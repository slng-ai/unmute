package ir

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// The variables-and-secrets surface is checked at Build time, where file:line is
// available (variable_secrets_specs.md V1, V2, V3, V7, V8).
func TestBuildRejectsBadTemplatesAndSecrets(t *testing.T) {
	cases := []struct {
		name  string
		mutet func(*packagespec.Package)
		want  string
		// redacts, when set, is text the refusal must NOT contain. A slot that
		// takes an environment variable name gets a pasted credential when it is
		// wrong, and repeating it puts the value in a terminal and a CI log.
		redacts string
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
			// The refusal does not quote the offending text back. What lands in
			// this slot when it is wrong is usually a pasted credential, and a
			// message that repeats it puts the value in a terminal and a CI log.
			name: "a secret key is an environment variable name",
			mutet: func(pkg *packagespec.Package) {
				pkg.Agent.Secrets = []string{"sk-live-pretend-key-value"}
			},
			want:    "not an UPPER_SNAKE environment variable name",
			redacts: "sk-live-pretend-key-value",
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
			if tc.redacts != "" && err != nil && strings.Contains(err.Error(), tc.redacts) {
				t.Errorf("the refusal repeats %q back; a field that takes a name must never print what was written there instead:\n%v", tc.redacts, err)
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

// FR-005c: a package is never asked to declare a name it does not write. The
// driver and the platform supply these, and requiring them would put the same
// block of boilerplate at the top of every phone package — which is how a
// warning stops being read.
//
// Without this the boundary has nothing holding it: the cross-check reports
// whatever referencedEnvNames collects, and that function is the one this
// feature widened.
func TestSecretsCrossCheckNeverAsksForDriverSuppliedNames(t *testing.T) {
	pkg := loadSafeCore(t)
	enableTelephony(pkg)
	routeTarget(pkg, "livekit", "primary_phone", "sip", "twilio")
	connection := pkg.Connections["primary_phone"]
	connection.Environment = map[string]string{
		"sip_address": "SIP_TRUNK_HOSTNAME", "sip_username": "SIP_AUTH_USERNAME",
		"sip_password": "SIP_AUTH_PASSWORD", "from_number": "SIP_FROM_NUMBER",
	}
	pkg.Connections["primary_phone"] = connection
	pkg.Agent.Secrets = []string{"OPENAI_API_KEY"}

	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	warning := undeclaredSecretWarning(agent)
	for _, supplied := range []string{
		"REDIS_URL", "UNMUTE_PUBLIC_URL", "UNMUTE_OUTBOUND_TOKEN",
		"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET",
		"DAILY_API_KEY", "PIPECAT_CLOUD_ORGANIZATION",
	} {
		if strings.Contains(warning, supplied) {
			t.Errorf("the cross-check asks for %q, which no author writes:\n%s", supplied, warning)
		}
	}
	// The other half: what the author *did* write is still reported, so this
	// test cannot pass by the check having stopped working.
	if !strings.Contains(warning, "SIP_TRUNK_HOSTNAME") {
		t.Errorf("a name the author wrote in a connection is not reported:\n%s", warning)
	}
}

// SC-008, asserted directly. The underlying check only warns, and a warning is
// easy to stop reading, so the shipped examples are held to zero.
func TestTelephonyExamplesDeclareEveryNameTheyWrite(t *testing.T) {
	for _, example := range []string{
		"twilio-telephony-hello", "livekit-human-transfer",
		"pipecat-human-transfer-twilio", "pipecat-human-transfer-daily", "outbound-reminder",
	} {
		t.Run(example, func(t *testing.T) {
			pkg, err := packagespec.Load(filepath.Join("..", "..", "examples", example))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			if len(agent.Secrets) == 0 {
				t.Fatal("this example declares no secrets, so the check below is vacuous")
			}
			if warning := undeclaredSecretWarning(agent); warning != "" {
				t.Errorf("an author-written name is missing from secrets: %s", warning)
			}
		})
	}
}

// TestSecretsCheckRunsWithNoBlock walks the three shapes in
// specs/013-first-five-minutes/reproduction.md section C. The guard tested the
// **declaration** list, so the package with the most to report — declares
// nothing, references eight names — took the same early return as the package
// with nothing to report. Its sibling, unusedConnectionWarning, guards on the
// subject set and is correct; the shape to copy was already in the file.
//
// Severity stays a warning at exit 0, exactly as docs/SCHEMA.md N24 fixes it.
func TestSecretsCheckRunsWithNoBlock(t *testing.T) {
	load := func(t *testing.T, mutate func(*packagespec.Package)) *Agent {
		t.Helper()
		pkg, err := packagespec.Load(filepath.Join("..", "..", "examples", "livekit-human-transfer"))
		if err != nil {
			t.Fatal(err)
		}
		mutate(pkg)
		agent, err := Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		return agent
	}

	t.Run("a: every name declared, nothing to report", func(t *testing.T) {
		if warning := undeclaredSecretWarning(load(t, func(*packagespec.Package) {})); warning != "" {
			t.Errorf("warning = %q, want none", warning)
		}
	})

	dropped := []string{"BILLING_PHONE_NUMBER", "SIP_AUTH_USERNAME", "SIP_TRUNK_HOSTNAME"}
	t.Run("b: three names missing, all three reported with their site", func(t *testing.T) {
		agent := load(t, func(pkg *packagespec.Package) {
			pkg.Agent.Secrets = slices.DeleteFunc(slices.Clone(pkg.Agent.Secrets), func(name string) bool {
				return slices.Contains(dropped, name)
			})
		})
		warning := undeclaredSecretWarning(agent)
		for _, want := range append(slices.Clone(dropped), "agent.yaml destinations", "connections/") {
			if !strings.Contains(warning, want) {
				t.Errorf("warning must name %q, got %q", want, warning)
			}
		}
	})

	t.Run("c: no block at all, the case most worth reporting", func(t *testing.T) {
		agent := load(t, func(pkg *packagespec.Package) { pkg.Agent.Secrets = nil })
		warning := undeclaredSecretWarning(agent)
		if warning == "" {
			t.Fatal("a package that declares nothing and references eight names must not be the silent case")
		}
		for _, want := range dropped {
			if !strings.Contains(warning, want) {
				t.Errorf("warning must name %q, got %q", want, warning)
			}
		}
	})

	// Removing the guard alone leaves the check vacuous where it matters most: a
	// fresh scaffold references only its model provider keys, and those were in
	// no *_env field, so referencedEnvNames never saw them (FR-005a).
	t.Run("provider key names are in the reference set", func(t *testing.T) {
		agent := load(t, func(pkg *packagespec.Package) { pkg.Agent.Secrets = nil })
		refs := referencedEnvNames(agent)
		// The site is the first models entry that chose the key, whichever
		// section that is: one key often serves several roles, and naming the
		// first one the author can go and look at is the point.
		for _, name := range []string{"OPENAI_API_KEY", "SLNG_API_KEY"} {
			site, ok := refs[name]
			if !ok {
				t.Errorf("%s is not in the reference set, so the check is vacuous for a scaffolded package", name)
				continue
			}
			if !strings.HasPrefix(site, "agent.yaml models ") {
				t.Errorf("%s site = %q, want it to name the models entry that chose it", name, site)
			}
		}
	})
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
