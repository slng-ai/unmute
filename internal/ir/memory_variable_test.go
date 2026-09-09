package ir

import (
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// A `source: conversation` variable is a value the model records mid-call. SLNG
// has a slot for it, a runtime variable; the code drivers do not, and a task's
// `assign:` is the spelling there. These hold the source to that split and to
// the two facts the platform requires of a runtime variable: no default, and a
// description the model reads.

func conversationVariable() Variable {
	return Variable{Type: "string", Source: VariableSourceConversation,
		Description: "The caller's mobile number, recorded after the caller confirmed it."}
}

func TestSlngTakesAConversationVariable(t *testing.T) {
	agent := slngAgent(t)
	agent.Variables["caller_phone"] = conversationVariable()
	row := validateSlng(t, agent)
	if len(row.Errors) > 0 {
		t.Fatalf("a conversation variable must validate on slng, got:\n%s", strings.Join(row.Errors, "\n"))
	}
}

func TestConversationVariableTakesNoDefaultAndNeedsADescription(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Variable)
		want string
	}{
		{"a default", func(v *Variable) { v.Default = "+447700900123" }, "takes no default"},
		{"no description", func(v *Variable) { v.Description = "  " }, "needs a description"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := slngAgent(t)
			variable := conversationVariable()
			tc.edit(&variable)
			agent.Variables["caller_phone"] = variable
			row := validateSlng(t, agent)
			wantSlngError(t, row, tc.want)
		})
	}
}

func TestCodeTargetsRefuseAConversationVariableAndNameTheTask(t *testing.T) {
	agent := safeAgent(t)
	agent.Variables["caller_phone"] = conversationVariable()
	for _, provider := range []Provider{ProviderLiveKit, ProviderPipecat} {
		report, err := Validate(agent, []Target{targetFor(agent, provider)}, targetcap.Default())
		if err == nil {
			t.Fatalf("%s accepted a conversation variable", provider)
		}
		joined := strings.Join(reportFor(report, provider).Errors, "\n")
		for _, want := range []string{"no value a model records mid-call", "task's `assign:`", "compile to slng"} {
			if !strings.Contains(joined, want) {
				t.Errorf("%s refusal does not say %q:\n%s", provider, want, joined)
			}
		}
	}
}

// send_sms is the one builtin with a setting a package pins, and the platform
// is strict about it: the sender is required on the attachment and must be a
// literal E.164 number. Each way of getting that wrong is refused at validate,
// with the rule, rather than at the push.

func sendSmsTool(inject map[string]any) Tool {
	return Tool{
		Description: "Text the caller.", Execution: ToolBuiltin, Builtin: "send_sms",
		Effect: ToolReturnsData, Interruption: ToolProviderDefault, Inject: inject,
	}
}

func TestBuiltinSendSmsSenderRules(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inject map[string]any
		want   string
	}{
		{"no inject at all", nil, "needs `inject:` with one `from_number`"},
		{"a second key", map[string]any{"from_number": "+447700900123", "body": "hi"}, "takes only the sender"},
		{"a template reference", map[string]any{"from_number": "{{customer_phone}}"}, "not a literal number"},
		{"a national number", map[string]any{"from_number": "07700900123"}, "not a literal number"},
		{"a number", map[string]any{"from_number": 447700900123}, "not a literal number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := slngAgent(t)
			agent.Tools["send_sms"] = sendSmsTool(tc.inject)
			row := validateSlng(t, agent)
			wantSlngError(t, row, tc.want)
		})
	}
	t.Run("a literal E.164 sender passes", func(t *testing.T) {
		agent := slngAgent(t)
		agent.Tools["send_sms"] = sendSmsTool(map[string]any{"from_number": "+447700900123"})
		row := validateSlng(t, agent)
		for _, message := range row.Errors {
			if strings.Contains(message, "send_sms") {
				t.Errorf("a correct sender was refused: %s", message)
			}
		}
	})
}

func TestCodeTargetsRefuseSendSmsByName(t *testing.T) {
	agent := safeAgent(t)
	makeBuiltin(agent, "lookup_customer")
	tool := agent.Tools["lookup_customer"]
	tool.Builtin, tool.Effect = "send_sms", ToolReturnsData
	tool.Inject = map[string]any{"from_number": "+447700900123"}
	agent.Tools["lookup_customer"] = tool
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err == nil {
		t.Fatal("livekit accepted builtin send_sms")
	}
	joined := strings.Join(reportFor(report, ProviderLiveKit).Errors, "\n")
	for _, want := range []string{"hosts only the end_call prebuilt", "compile this package to slng"} {
		if !strings.Contains(joined, want) {
			t.Errorf("refusal does not say %q:\n%s", want, joined)
		}
	}
}

// The build-time inject rule opens for send_sms alone. end_call keeps refusing
// an inject, and the message now names the one builtin that takes one.
func TestBuildRefusesInjectOnEndCall(t *testing.T) {
	pkg := loadSlngCore(t)
	tool := pkg.Tools["end_call"]
	tool.Inject = []packagespec.Pair{{Key: "from_number", Value: "+447700900123"}}
	pkg.Tools["end_call"] = tool
	_, err := Build(pkg)
	if err == nil || !strings.Contains(err.Error(), "on builtin send_sms for its sender") {
		t.Fatalf("inject on end_call must be refused naming send_sms as the exception, got: %v", err)
	}
}
