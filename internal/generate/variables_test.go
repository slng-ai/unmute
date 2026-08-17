package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The worked example is the fixture for input, system, and conversation
// variables on both shipped drivers (variable_secrets_specs.md T6, T7).
func loadOutboundReminder(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "outbound-reminder"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func TestSLNGPromptLoweringKeepsRawPromptAndExactSnapshot(t *testing.T) {
	body := "Welcome {{name}}. Your slot is {{slot}}. Repeat: {{name}}."
	for _, test := range []struct {
		target, state, snapshot string
	}{
		{target: "pipecat", state: "state", snapshot: `_slng_snapshot(state, ["name", "slot"])`},
		{target: "livekit", state: "self.session.userdata", snapshot: `_slng_snapshot(self.session.userdata, ["name", "slot"])`},
	} {
		t.Run(test.target, func(t *testing.T) {
			got := lowerSLNGPrompt("WELCOME_PROMPT", body, test.state)
			if got.PromptExpr != "WELCOME_PROMPT" {
				t.Fatalf("SLNG prompt expression = %q, want raw prompt constant", got.PromptExpr)
			}
			if got.SnapshotExpr != test.snapshot {
				t.Fatalf("SLNG snapshot expression = %q", got.SnapshotExpr)
			}
			if strings.Contains(got.SnapshotExpr, "unrelated") {
				t.Fatalf("SLNG snapshot must contain only referenced variables: %s", got.SnapshotExpr)
			}
		})
	}
	task := lowerSLNGPrompt(pyQuote(body), body, "self.state")
	if task.PromptExpr != `"Welcome {{name}}. Your slot is {{slot}}. Repeat: {{name}}."` {
		t.Fatalf("SLNG task prompt lost its raw placeholders: %s", task.PromptExpr)
	}
}

func TestSLNGPromptLoweringWithNoReferencesDoesNotReadState(t *testing.T) {
	for _, state := range []string{"state", "self.session.userdata"} {
		got := lowerSLNGPrompt("WELCOME_PROMPT", "Welcome to the salon.", state)
		if got.PromptExpr != "WELCOME_PROMPT" {
			t.Fatalf("SLNG prompt expression = %q, want raw prompt constant", got.PromptExpr)
		}
		if got.SnapshotExpr != "{}" {
			t.Errorf("zero-reference SLNG prompt reads %q, want an empty snapshot", got.SnapshotExpr)
		}
	}
}

func TestSLNGLoweringLeavesEveryLocalRenderSiteUnchanged(t *testing.T) {
	if got := promptExpr("WELCOME_PROMPT", "Welcome {{name}}.", "state"); got != `_render(WELCOME_PROMPT, state)` {
		t.Fatalf("non-SLNG prompt expression = %q", got)
	}
	if got := injectExpr("customer/{{customer_id}}", "state"); got != `_render("customer/{{customer_id}}", state)` {
		t.Fatalf("tool injection expression = %q", got)
	}
	tool := ir.Tool{URLEnv: "TOOL_URL", Path: "/customers/{{customer_id}}"}
	if got := urlExpr(tool, "state"); got != `os.environ["TOOL_URL"].rstrip("/") + _render("/customers/{{customer_id}}", state, quote_values=True)` {
		t.Fatalf("webhook path expression = %q", got)
	}
}

func TestVariablesLowerOnBothDrivers(t *testing.T) {
	agent := loadOutboundReminder(t)
	cases := []struct {
		target string
		file   string
		want   []string
	}{
		{
			target: "pipecat",
			file:   "bot.py",
			want: []string{
				// The capture tool exists, is attached, and writes the state (V6).
				`async def update_variables(`,
				`self.state.reschedule_to = reschedule_to`,
				// A whole-token value keeps its declared type (V14).
				`customer_id=state.customer_id`,
				`new_time=state.reschedule_to`,
				// A literal inject value rides through untouched.
				`channel="phone"`,
				// The refusal precedes the local call (V4).
				`refusal = _refusal(`,
				`{"refused": refusal}`,
				`tools.confirm_appointment.confirm_appointment(`,
				`tools.reschedule_appointment.reschedule_appointment(`,
				// The prompt and greeting render from the call state (V15).
				`system_instruction=_render(REMINDER_PROMPT, state)`,
				`_render("Hi {{name}}! This is Sage and Stone Salon calling about your appointment {{appointment_time}}. Does that time still work for you?", state)`,
				`_dispatched_call_start(call_context)`,
			},
		},
		{
			target: "livekit",
			file:   "agent.py",
			want: []string{
				`async def update_variables(`,
				`ctx.userdata.reschedule_to = reschedule_to`,
				`customer_id=ctx.userdata.customer_id`,
				`new_time=ctx.userdata.reschedule_to`,
				`channel="phone"`,
				`refusal = _refusal(`,
				`return {"refused": refusal}`,
				`tools.confirm_appointment.confirm_appointment(`,
				`tools.reschedule_appointment.reschedule_appointment(`,
				`await self.update_instructions(_render(REMINDER_PROMPT, self.session.userdata))`,
				`_render("Hi {{name}}! This is Sage and Stone Salon calling about your appointment {{appointment_time}}. Does that time still work for you?", session.userdata)`,
				`_hydrate_call_start(session.userdata`,
				// A required secret stops the session before it answers (V12).
				`require_env()`,
				`"OPENAI_API_KEY",`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			artifact, err := Generate(agent, agent.Targets[tc.target], target.Default())
			if err != nil {
				t.Fatal(err)
			}
			source := artifactFile(t, artifact, tc.file)
			for _, want := range tc.want {
				if !strings.Contains(source, want) {
					t.Errorf("%s missing %q", tc.file, want)
				}
			}
			// The raw template must never reach the runtime unrendered: every
			// remaining token has to sit inside a _render call or a prompt constant.
			for _, line := range strings.Split(source, "\n") {
				if !strings.Contains(line, "{{") || strings.Contains(line, "_TEMPLATE") {
					continue
				}
				if !strings.Contains(line, "_render(") && !strings.Contains(line, "{{customer_id}}") && !strings.Contains(line, "{{name}}") && !strings.Contains(line, "{{appointment_time}}") {
					t.Errorf("%s has an unrendered template outside a render call: %s", tc.file, strings.TrimSpace(line))
				}
			}
		})
	}
}

// A system variable never gates a tool call: the model cannot be asked for the
// number the caller dialed (B2).
func TestSystemVariableDoesNotGateToolCall(t *testing.T) {
	agent := loadOutboundReminder(t)
	tool := agent.Tools["confirm_appointment"]
	if len(tool.Inject) == 0 {
		t.Fatal("fixture must inject values")
	}
	needed := neededVars(tool, agent.Variables)
	for _, variable := range needed {
		if agent.Variables[variable.Name].Source == ir.VariableSourceToNumber {
			t.Fatalf("system variable %q must not gate the call", variable.Name)
		}
	}
	// The variable the caller can actually supply still does.
	found := false
	for _, variable := range needed {
		if variable.Name == "customer_id" {
			found = true
		}
	}
	if !found {
		t.Fatal("a dispatched variable with no default must still gate the call")
	}
}

// .env.example is one sorted list of bare names, and every one of them is the
// reader's to fill in (V11, FR-018). A secret has no description to carry: the
// name is the whole declaration.
//
// The "required by the target or a connection" section this used to assert is
// gone with the names that were in it: they were the route's, not the author's.
func TestEnvExampleDocumentsSecrets(t *testing.T) {
	agent := loadOutboundReminder(t)
	for _, name := range []string{"pipecat", "livekit"} {
		artifact, err := Generate(agent, agent.Targets[name], target.Default())
		if err != nil {
			t.Fatal(err)
		}
		env := artifactFile(t, artifact, ".env.example")
		for _, want := range []string{"OPENAI_API_KEY=\nSLNG_API_KEY=", "TWILIO_ACCOUNT_SID="} {
			if !strings.Contains(env, want) {
				t.Errorf("%s .env.example missing %q", name, want)
			}
		}
		if strings.Contains(env, "secret") && strings.Contains(env, "=sk-") {
			t.Errorf("%s .env.example must never carry a value", name)
		}
	}
}

// A package with no variables keeps its previous output: none of the machinery
// appears (V16).
func TestNoVariablesEmitsNoMachinery(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "twilio-telephony-hello"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if len(agent.Variables) != 0 {
		t.Skip("fixture now declares variables")
	}
	for _, name := range []string{"pipecat", "livekit"} {
		artifact, err := Generate(agent, agent.Targets[name], target.Default())
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range artifact.Files {
			source := string(file.Content)
			for _, forbidden := range []string{"_render(", "_refusal(", "update_variables", "_dispatched_call_start", "UNMUTE_CALL_START"} {
				if strings.Contains(source, forbidden) {
					t.Errorf("%s/%s must not carry %q when the package declares no variables", name, file.Path, forbidden)
				}
			}
		}
	}
}
