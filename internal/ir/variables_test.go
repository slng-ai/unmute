package ir

import (
	"strings"
	"testing"

	packagespec "github.com/slng/unmute/internal/spec"
	targetcap "github.com/slng/unmute/internal/target"
)

// The variables-and-secrets surface is checked at Build time, where file:line is
// available (variable_secrets_specs.md V1, V2, V3, V7, V8).
func TestBuildRejectsBadTemplatesAndSecrets(t *testing.T) {
	cases := []struct {
		name  string
		mutet func(*packagespec.Package)
		want  string
	}{
		{
			name: "unknown token in the greeting",
			mutet: func(pkg *packagespec.Package) {
				pkg.Agent.Conversation.Greeting.Text = "Hi {{custmer_id}}"
			},
			want: `references {{custmer_id}}, which is not a declared variable`,
		},
		{
			name: "a secret may never flow through a template",
			mutet: func(pkg *packagespec.Package) {
				pkg.Agent.Conversation.Greeting.Text = "Token {{SALON_API_TOKEN}}"
			},
			want: "secrets never flow through templates",
		},
		{
			name: "a conversation variable has no value when the prompt is built",
			mutet: func(pkg *packagespec.Package) {
				pkg.Agent.Variables["reschedule_to"] = packagespec.Variable{Type: "string", Source: "conversation"}
				pkg.Agent.Conversation.Greeting.Text = "Hi {{reschedule_to}}"
			},
			want: "has no value when the prompt is built",
		},
		{
			name: "the same variable is fine once it has a default",
			mutet: func(pkg *packagespec.Package) {
				pkg.Agent.Variables["reschedule_to"] = packagespec.Variable{Type: "string", Source: "conversation", Default: "later"}
				pkg.Agent.Conversation.Greeting.Text = "Hi {{reschedule_to}}"
			},
			want: "",
		},
		{
			name: "a call-time site may name a conversation variable with no default",
			mutet: func(pkg *packagespec.Package) {
				pkg.Agent.Variables["reschedule_to"] = packagespec.Variable{Type: "string", Source: "conversation"}
				tool := pkg.Tools["lookup_customer"]
				tool.Inject = map[string]any{"slot": "{{reschedule_to}}"}
				pkg.Tools["lookup_customer"] = tool
			},
			want: "",
		},
		{
			name: "an injected key cannot double as a model parameter",
			mutet: func(pkg *packagespec.Package) {
				tool := pkg.Tools["lookup_customer"]
				tool.Inject = map[string]any{"phone": "{{customer_id}}"}
				pkg.Tools["lookup_customer"] = tool
			},
			want: "which is also an input property",
		},
		{
			name: "inject has no lowering on an mcp tool",
			mutet: func(pkg *packagespec.Package) {
				tool := pkg.Tools["lookup_customer"]
				tool.Webhook, tool.MCP = nil, &packagespec.ToolMCP{URLEnv: "MCP_URL"}
				tool.Inject = map[string]any{"caller": "{{customer_id}}"}
				pkg.Tools["lookup_customer"] = tool
			},
			want: "inject is legal on webhook and local tools",
		},
		{
			name: "a webhook path must start with a slash",
			mutet: func(pkg *packagespec.Package) {
				tool := pkg.Tools["lookup_customer"]
				tool.Webhook.Path = "customers"
				pkg.Tools["lookup_customer"] = tool
			},
			want: "webhook.path must start with /",
		},
		{
			name: "a secret key is an environment variable name",
			mutet: func(pkg *packagespec.Package) {
				pkg.Agent.Secrets = []string{"salon_token"}
			},
			want: "must be an UPPER_SNAKE environment variable name",
		},
		{
			name: "the capture tool name is reserved",
			mutet: func(pkg *packagespec.Package) {
				pkg.Agent.Tools = append(pkg.Agent.Tools, CaptureToolName)
				pkg.Tools[CaptureToolName] = pkg.Tools["lookup_customer"]
			},
			want: "is reserved",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg := loadSafeCore(t)
			// Every case needs a variable to point at and a secret to confuse it with.
			pkg.Agent.Variables["customer_id"] = packagespec.Variable{Type: "string", Source: "call_start", Default: "cus_1"}
			pkg.Agent.Secrets = []string{"SALON_API_TOKEN"}
			if pkg.Agent.Conversation == nil {
				pkg.Agent.Conversation = &packagespec.Conversation{}
			}
			if pkg.Agent.Conversation.Greeting == nil {
				pkg.Agent.Conversation.Greeting = &packagespec.Greeting{SpeaksFirst: "agent"}
			}
			tc.mutet(pkg)
			_, err := Build(pkg)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("Build failed, want success: %v", err)
			case tc.want == "":
			case err == nil:
				t.Fatalf("Build succeeded, want error containing %q", tc.want)
			case !strings.Contains(err.Error(), tc.want):
				t.Fatalf("Build error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// A source: conversation variable and a template are gated on the two targets
// whose drivers cannot honor them (V5).
func TestValidateGatesVariableFeatures(t *testing.T) {
	pkg := loadSafeCore(t)
	pkg.Agent.Variables["reschedule_to"] = packagespec.Variable{Type: "string", Source: "conversation"}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		provider Provider
		wantErr  bool
	}{
		{ProviderLiveKit, false},
		{ProviderPipecat, false},
		{ProviderVapi, true},
		{ProviderDeepgram, true},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			target := targetFor(agent, tc.provider)
			report, _ := Validate(agent, []Target{target}, targetcap.Default())
			gated := false
			for _, row := range report.PerTarget {
				for _, message := range row.Errors {
					if strings.Contains(message, "variable") || strings.Contains(message, "capture") {
						gated = true
					}
				}
			}
			if gated != tc.wantErr {
				t.Fatalf("%s gated = %v, want %v (errors %v)", tc.provider, gated, tc.wantErr, report.PerTarget[0].Errors)
			}
		})
	}
}

// The env cross-check is a warning, never an error: declaring secrets is opt-in
// and a package that declares some but not all still compiles (V10, C7).
func TestUndeclaredSecretIsWarningOnly(t *testing.T) {
	pkg := loadSafeCore(t)
	pkg.Agent.Secrets = []string{"SALON_API_TOKEN"}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	warning := undeclaredSecretWarning(agent)
	if warning == "" {
		t.Fatal("expected a warning naming the env vars the tools reference but secrets: does not declare")
	}
	if !strings.Contains(warning, "referenced but not declared") {
		t.Fatalf("warning = %q", warning)
	}
	target := targetFor(agent, ProviderLiveKit)
	report, err := Validate(agent, []Target{target}, targetcap.Default())
	if err != nil {
		t.Fatalf("an undeclared env name must not fail validation: %v", err)
	}
	found := false
	for _, row := range report.PerTarget {
		for _, message := range row.Warnings {
			if strings.Contains(message, "referenced but not declared") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("the cross-check warning must reach the per-target report")
	}
}

// A local handler owns its own request, so its credential is read in Python and
// never appears in YAML. The cross-check reads the handler source, so an
// undeclared name is still named instead of failing on the first call (V10).
func TestHandlerEnvReadsAreCrossChecked(t *testing.T) {
	agent := &Agent{
		Secrets: []string{"SALON_API_URL"},
		Tools: map[string]Tool{
			"cancel_appointment": {
				Execution: ToolLocal,
				Handler:   "tools/cancel_appointment.py",
				HandlerSource: `import os
url = os.environ["SALON_API_URL"]
key = os.environ.get('SALON_API_SIGNING_KEY')
mode = os.getenv("SALON_API_MODE", "live")
flag = os.getenv("debug")
`,
			},
		},
	}
	refs := referencedEnvNames(agent)
	for _, name := range []string{"SALON_API_URL", "SALON_API_SIGNING_KEY", "SALON_API_MODE"} {
		if refs[name] != "tools/cancel_appointment.py os.environ" {
			t.Errorf("%s site = %q, want the handler file", name, refs[name])
		}
	}
	if _, ok := refs["debug"]; ok {
		t.Error("a lowercase lookup is not an environment name reference")
	}
	warning := undeclaredSecretWarning(agent)
	if !strings.Contains(warning, "SALON_API_SIGNING_KEY (tools/cancel_appointment.py os.environ)") {
		t.Fatalf("warning must name the undeclared read and its file, got %q", warning)
	}
	if strings.Contains(warning, "SALON_API_URL") {
		t.Fatalf("a declared secret must not be reported, got %q", warning)
	}
}

func TestTemplateParsing(t *testing.T) {
	if got := TemplateRefs("Hi {{name}}, your slot is {{ slot }} ({{name}})"); len(got) != 2 || got[0] != "name" || got[1] != "slot" {
		t.Fatalf("TemplateRefs = %v", got)
	}
	// A whole-value token keeps its declared type; anything mixed renders to text.
	if got := TemplateVar("{{ customer_id }}"); got != "customer_id" {
		t.Fatalf("TemplateVar = %q, want customer_id", got)
	}
	if got := TemplateVar("id-{{customer_id}}"); got != "" {
		t.Fatalf("TemplateVar = %q, want empty for a mixed value", got)
	}
	if HasTemplate("no tokens here") {
		t.Fatal("HasTemplate must be false without a token")
	}
}
