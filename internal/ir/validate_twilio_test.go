package ir

import (
	"path/filepath"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// twilioAgent builds the passing baseline every refusal below starts from, so a
// test that breaks one thing knows the error came from the thing it broke.
func twilioAgent(t *testing.T) *Agent {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "twilio_relay"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func validateTwilio(t *testing.T, agent *Agent, resolved Target) TargetValidation {
	t.Helper()
	report, _ := Validate(agent, []Target{resolved}, targetcap.Default())
	return reportFor(report, ProviderTwilio)
}

func TestTwilioBaselineValidatesClean(t *testing.T) {
	agent := twilioAgent(t)
	row := validateTwilio(t, agent, targetFor(agent, ProviderTwilio))
	if len(row.Errors) > 0 || len(row.Warnings) > 0 {
		t.Fatalf("the twilio baseline must be clean: errors=%#v warnings=%#v", row.Errors, row.Warnings)
	}
}

// Each row breaks one thing the first release does not run and reads the
// refusal. Every twilio-worded message must also name what to do instead.
func TestTwilioRefusesWhatItDoesNotRun(t *testing.T) {
	yes := true
	for _, tc := range []struct {
		name   string
		mutate func(*Agent, *Target)
		want   string
	}{
		{"second agent", func(a *Agent, _ *Target) {
			a.Agents["other"] = a.Agents["desk"]
		}, "runs one agent"},
		{"handoff control", func(a *Agent, _ *Target) {
			a.Controls = map[string]Control{"to_sales": &AgentTransfer{}}
		}, "emits no handoff"},
		{"variable", func(a *Agent, _ *Target) {
			a.Variables = map[string]Variable{"caller": {Type: PrimitiveString}}
		}, "has no session state"},
		{"outbound channel", func(a *Agent, _ *Target) {
			a.Channels["phone"] = Channel{Kind: ChannelTelephony, Inbound: &yes, Outbound: &yes}
		}, "answers inbound calls only"},
		{"browser channel", func(a *Agent, _ *Target) {
			a.Channels["web"] = Channel{Kind: ChannelRealtimeAudio}
		}, "answers one inbound phone channel"},
		{"no greeting", func(a *Agent, _ *Target) {
			a.Conversation.Greeting = nil
		}, "twilio target needs an explicit opening"},
		{"model-written greeting", func(a *Agent, _ *Target) {
			a.Conversation.Greeting = &Greeting{SpeaksFirst: SpeaksFirstAgent}
		}, "has no model-written opening"},
		{"protect tool calls", func(a *Agent, _ *Target) {
			a.Conversation.Interruption = &Interruption{Enabled: &yes, Protect: []InterruptionProtect{ProtectToolCalls}}
		}, "can protect only the greeting"},
		{"inactivity", func(a *Agent, _ *Target) {
			a.Conversation.Inactivity = &Inactivity{EndAfter: "30s"}
		}, "emits no inactivity timer"},
		{"minimum words", func(a *Agent, _ *Target) {
			a.Conversation.Interruption = &Interruption{Enabled: &yes, MinimumWords: 2}
		}, "no slot for a minimum word count"},
		{"webhook tool", func(a *Agent, _ *Target) {
			a.Tools["lookup"] = Tool{Execution: ToolWebhook, URLEnv: "LOOKUP_URL", Input: map[string]any{"type": "object"}}
			addTool(a, "lookup")
		}, "webhook tool \"lookup\" is not emitted"},
		{"mcp tool", func(a *Agent, _ *Target) {
			a.Tools["crm"] = Tool{Execution: ToolMCP, URLEnv: "CRM_URL"}
			addTool(a, "crm")
		}, "an MCP tool source is not emitted"},
		{"local tool ending the call", func(a *Agent, _ *Target) {
			a.Tools["bye"] = Tool{Execution: ToolLocal, Effect: ToolEndsConversation, Handler: "tools/bye.py", HandlerSource: "def bye():\n    return {}\n"}
			addTool(a, "bye")
		}, "only through the builtin end_call"},
		{"end_call closing instructions", func(a *Agent, _ *Target) {
			tool := a.Tools["end_call"]
			tool.Instructions = "Say goodbye."
			a.Tools["end_call"] = tool
		}, "speaks no closing line"},
		{"nested tool input", func(a *Agent, _ *Target) {
			a.Tools["find"] = Tool{Execution: ToolLocal, Handler: "tools/find.py", HandlerSource: "def find(q):\n    return {}\n", Input: map[string]any{
				"type": "object", "properties": map[string]any{"q": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
			}}
			addTool(a, "find")
		}, `field "q" has type array`},
		{"unrepresented keyword", func(a *Agent, _ *Target) {
			a.Tools["find"] = Tool{Execution: ToolLocal, Handler: "tools/find.py", HandlerSource: "def find(q):\n    return {}\n", Input: map[string]any{
				"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string", "pattern": "^a"}},
			}}
			addTool(a, "find")
		}, `field "q" keyword "pattern" is not represented`},
		{"tool announce", func(a *Agent, _ *Target) {
			a.Tools["find"] = Tool{Execution: ToolLocal, Handler: "tools/find.py", HandlerSource: "def find():\n    return {}\n", Announce: []string{"One moment."}}
			addTool(a, "find")
		}, "announce"},
		{"tool cancel", func(a *Agent, _ *Target) {
			a.Tools["find"] = Tool{Execution: ToolLocal, Handler: "tools/find.py", HandlerSource: "def find():\n    return {}\n", Interruption: ToolCancel}
			addTool(a, "find")
		}, "never cancels one"},
		{"tracing", func(a *Agent, _ *Target) {
			a.Tracing = &Tracing{Provider: "langfuse"}
		}, "emits no trace exporter"},
		{"version", func(_ *Agent, r *Target) { r.Version = "1.0.0" }, "has no framework version"},
		{"pins", func(_ *Agent, r *Target) { r.Pins = map[string]string{"openai": "3.19.2"} }, "pins its own dependencies"},
		{"deployment region", func(_ *Agent, r *Target) { r.DeploymentRegions = []string{"us-east"} }, "with no deployment region"},
		{"warm instances", func(_ *Agent, r *Target) { r.WarmInstances = 1 }, "no pool to keep warm"},
		{"sdk language", func(_ *Agent, r *Target) { r.SDKLanguage = "node" }, "emits python projects only"},
		{"turn model identity", func(_ *Agent, r *Target) {
			r.Models.Turn = &Binding{Provider: "local", Model: "silero"}
		}, "carries settings only"},
		{"unknown turn setting", func(_ *Agent, r *Target) {
			r.Models.Turn = &Binding{Params: map[string]any{"eotThreshold": 0.7}}
		}, `no turn setting "eotThreshold"`},
		{"speech timeout out of range", func(_ *Agent, r *Target) {
			r.Models.Turn = &Binding{Params: map[string]any{"speechTimeout": 200}}
		}, "from 600 to 5000"},
		{"suffixed voice", func(_ *Agent, r *Target) {
			speak := r.Models.Speak["voice"]
			speak.Voice = "UgBBYS2sOqTuMpoF3BR0-flash_v2_5"
			r.Models.Speak["voice"] = speak
		}, "not a bare ElevenLabs voice id"},
		{"listen params", func(_ *Agent, r *Target) {
			r.Models.Listen.Params = map[string]any{"smart_format": true}
		}, "forwards no listen params"},
		{"unsupported listen vendor", func(_ *Agent, r *Target) {
			r.Models.Listen.Provider = "slng"
		}, "slng"},
		{"app-owned think param", func(_ *Agent, r *Target) {
			think := r.Models.Reason["reasoning"]
			think.Params = map[string]any{"tools": []any{}}
			r.Models.Reason["reasoning"] = think
		}, "which the app sets itself"},
		{"gemini param on openai", func(_ *Agent, r *Target) {
			think := r.Models.Reason["reasoning"]
			think.Params = map[string]any{"thinking_config": map[string]any{"thinking_level": "LOW"}}
			r.Models.Reason["reasoning"] = think
		}, "belongs to the other think provider"},
		{"openai param on gemini", func(_ *Agent, r *Target) {
			r.Models.Reason["reasoning"] = Binding{Provider: "google", Model: "gemini-3.1-flash-lite", Placement: PlacementAPI,
				Params: map[string]any{"reasoning_effort": "none"}}
		}, "belongs to the other think provider"},
		{"vertex without a location", func(_ *Agent, r *Target) {
			r.Models.Reason["reasoning"] = Binding{Provider: "gemini", Model: "gemini-3.1-flash-lite", Placement: PlacementAPI,
				Params: map[string]any{"vertexai": true}}
		}, "location"},
		{"custom endpoint", func(_ *Agent, r *Target) {
			think := r.Models.Reason["reasoning"]
			think.EndpointEnv = "MY_LLM_URL"
			r.Models.Reason["reasoning"] = think
		}, "endpoint"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := twilioAgent(t)
			resolved := targetFor(agent, ProviderTwilio)
			tc.mutate(agent, &resolved)
			row := validateTwilio(t, agent, resolved)
			joined := strings.Join(row.Errors, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("no error contains %q; got:\n%s", tc.want, joined)
			}
			for _, message := range row.Errors {
				if strings.HasPrefix(message, "twilio target") && !strings.Contains(message, ": ") {
					t.Errorf("twilio error names no alternative: %q", message)
				}
			}
		})
	}
}

func addTool(agent *Agent, name string) {
	desk := agent.Agents["desk"]
	desk.Tools = append(desk.Tools, name)
	agent.Agents["desk"] = desk
}

// A flat schema of the four primitives, with enums and descriptions, is what
// the app validates, and it must pass.
func TestFlatSchemaAcceptsWhatTheAppValidates(t *testing.T) {
	schema := map[string]any{
		"type": "object", "additionalProperties": false, "required": []any{"day"},
		"properties": map[string]any{
			"day":   map[string]any{"type": "string", "enum": []any{"monday", "friday"}, "description": "which day"},
			"count": map[string]any{"type": "integer", "enum": []any{1, 2.0}},
			"price": map[string]any{"type": "number"},
			"open":  map[string]any{"type": "boolean"},
		},
	}
	if problems := flatSchemaProblems(schema); len(problems) > 0 {
		t.Fatalf("a supported schema was refused: %v", problems)
	}
	if problems := flatSchemaProblems(nil); len(problems) > 0 {
		t.Fatalf("no schema means no arguments and must pass: %v", problems)
	}
}

// Turn params are dead on the framework targets and forwarded on twilio, so a
// package carrying them warns on livekit and not on twilio.
func TestTwilioTurnParamsDoNotWarn(t *testing.T) {
	agent := twilioAgent(t)
	resolved := targetFor(agent, ProviderTwilio)
	resolved.Models.Turn = &Binding{Params: map[string]any{"speechTimeout": 800, "interruptSensitivity": "low", "ignoreBackchannel": true}}
	agent.Models["relay"] = ModelDef{Kind: KindTurn, Params: resolved.Models.Turn.Params}
	agent.Turn = "relay"
	row := validateTwilio(t, agent, resolved)
	if len(row.Errors) > 0 || len(row.Warnings) > 0 {
		t.Fatalf("valid turn settings must be clean: errors=%v warnings=%v", row.Errors, row.Warnings)
	}
}

// The number SID is a deploy value: the plan keeps it out of what the running
// app is asked for, and says where it went.
func TestTwilioPlanKeepsTheNumberSIDDeployOnly(t *testing.T) {
	plan := targetFor(twilioAgent(t), ProviderTwilio).Telephony
	if plan == nil {
		t.Fatal("the twilio target has no telephony plan")
	}
	for _, name := range plan.RequiredEnvironment {
		if name == "TWILIO_PHONE_NUMBER_SID" {
			t.Errorf("the number SID is in the runtime environment: %v", plan.RequiredEnvironment)
		}
	}
	if len(plan.DeployEnvironment) != 1 || plan.DeployEnvironment[0] != "TWILIO_PHONE_NUMBER_SID" {
		t.Errorf("deploy environment = %v, want the number SID", plan.DeployEnvironment)
	}
	if plan.Coordination != "in_process" || len(plan.Services) != 1 || plan.Services[0] != "application" {
		t.Errorf("topology = %s %v, want one in-process application", plan.Coordination, plan.Services)
	}
}
