package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The worked example is the fixture for the whole surface: input, system, and
// conversation variables plus declared secrets, on both shipped drivers
// (variable_secrets_specs.md T6, T7).
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
				`"customer_id": state.customer_id`,
				`"new_time": state.reschedule_to`,
				// A literal inject value rides through untouched.
				`"channel": "phone"`,
				// The path renders with its values URL-encoded (V14).
				`os.environ["SALON_API_URL"].rstrip("/")`,
				`quote_values=True`,
				// The refusal precedes the request (V4).
				`refusal = _refusal(`,
				`{"refused": refusal}`,
				// The prompt and greeting render from the call state (V15).
				`system_instruction=_render(REMINDER_PROMPT, state)`,
				`_dispatched_call_start(call_context)`,
			},
		},
		{
			target: "livekit",
			file:   "agent.py",
			want: []string{
				`async def update_variables(`,
				`ctx.userdata.reschedule_to = reschedule_to`,
				`"customer_id": ctx.userdata.customer_id`,
				`"new_time": ctx.userdata.reschedule_to`,
				`"channel": "phone"`,
				`os.environ["SALON_API_URL"].rstrip("/")`,
				`quote_values=True`,
				`refusal = _refusal(`,
				`return {"refused": refusal}`,
				`await self.update_instructions(_render(REMINDER_PROMPT, self.session.userdata))`,
				`_hydrate_call_start(session.userdata`,
				// A required secret stops the session before it answers (V12).
				`require_env()`,
				`"SALON_API_TOKEN",`,
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

// Declared secrets drive .env.example, one bare name per line, with
// referenced-but-undeclared names labeled (V11). A secret has no description to
// carry: the name is the whole declaration.
func TestEnvExampleDocumentsSecrets(t *testing.T) {
	agent := loadOutboundReminder(t)
	for _, name := range []string{"pipecat", "livekit"} {
		artifact, err := Generate(agent, agent.Targets[name], target.Default())
		if err != nil {
			t.Fatal(err)
		}
		env := artifactFile(t, artifact, ".env.example")
		for _, want := range []string{
			"SALON_API_TOKEN=\nSALON_API_URL=",
			"# required by the target or a connection\n",
			"TWILIO_ACCOUNT_SID=",
		} {
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
