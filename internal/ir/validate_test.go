package ir

import (
	"strings"
	"testing"

	targetcap "github.com/slng/unmute/internal/target"
)

func TestValidateSafeCorePerTarget(t *testing.T) { // V5, V18
	agent := safeAgent(t)
	report, err := Validate(agent, allTargets(agent), targetcap.Default())
	if err != nil {
		t.Fatalf("%v: %#v", err, report.PerTarget)
	}
	if len(report.PerTarget) != 5 {
		t.Fatalf("target rows = %d", len(report.PerTarget))
	}
	elevenlabs := reportFor(report, ProviderElevenLabs)
	if len(elevenlabs.Warnings) == 0 || len(elevenlabs.Errors) != 0 {
		t.Fatalf("ElevenLabs row = %#v", elevenlabs)
	}
}

func TestValidateUsesProviderVocabularyForGates(t *testing.T) { // V4, V11
	agent := safeAgent(t)
	agent.Conversation.ThinkingAudio = ThinkingSubtle
	agent.Tasks["collect"] = Task{
		Instructions: "collect", Result: map[string]ResultField{"done": {Type: PrimitiveBoolean}},
		Context: TaskContext{History: HistoryFull},
	}
	agent.TaskGroups["collect_then_return"] = TaskGroup{
		Steps: []string{"collect"}, ContextScope: ContextShared, Then: GroupReturn, Merge: GroupMergeResults,
	}
	report, err := Validate(agent, []Target{targetFor(agent, ProviderVapi)}, targetcap.Default())
	if err == nil {
		t.Fatal("expected gated validation error")
	}
	text := strings.Join(report.PerTarget[0].Errors, "\n")
	for _, want := range []string{"Vapi has no faithful thinking-audio lowering", "Vapi state-preserving Squad return is unverified"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
}

func TestValidateProvisionalFailsEveryTarget(t *testing.T) { // V6
	caps := targetcap.Default()
	for _, provider := range targetcap.Providers {
		row := TargetValidation{Provider: Provider(provider)}
		applyCapability(caps, targetcap.FieldFutureProvisional, provider, &row)
		if len(row.Errors) != 1 {
			t.Errorf("%s provisional errors = %v", provider, row.Errors)
		}
	}
}

func TestValidateContextPolicy(t *testing.T) { // V8
	agent := safeAgent(t)
	transfer := agent.Controls["to_billing"].(*AgentTransfer)
	transfer.Context.History = HistoryMessages
	report, err := Validate(agent, []Target{targetFor(agent, ProviderElevenLabs)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "ElevenLabs always keeps the full transcript") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateRequiresCompleteBindings(t *testing.T) { // V9
	agent := safeAgent(t)
	target := targetFor(agent, ProviderLiveKit)
	delete(target.Models.Reason, "fast_reasoning")
	report, err := Validate(agent, []Target{target}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "missing reason binding") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateCapacity(t *testing.T) { // V12
	agent := safeAgent(t)
	agent.Capacity.PeakSessions = agent.Capacity.MaxSessions + 1
	report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "must not exceed") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateOutboundRequiresSatisfiableVariablesAndWarnsOnDeepgram(t *testing.T) { // V13
	agent := safeAgent(t)
	phone := agent.Channels["phone"]
	outbound := true
	phone.Outbound = &outbound
	phone.OnVoicemail = VoicemailLeaveMessage
	agent.Channels["phone"] = phone
	agent.Variables["campaign_id"] = Variable{Type: PrimitiveString, Source: VariableSourceCallStart}
	target := targetFor(agent, ProviderDeepgram)
	report, err := Validate(agent, []Target{target}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "not satisfiable") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
	variable := agent.Variables["campaign_id"]
	variable.Default = "campaign"
	agent.Variables["campaign_id"] = variable
	report, err = Validate(agent, []Target{target}, targetcap.Default())
	if err != nil || !strings.Contains(strings.Join(report.PerTarget[0].Warnings, "\n"), "carrier-conditional") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateReportsForwardedBindingsAndUnbenchmarkedSizing(t *testing.T) { // V15
	agent := safeAgent(t)
	report, err := Validate(agent, allTargets(agent), targetcap.Default())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.ForwardedBindings) != 27 {
		t.Fatalf("forwarded bindings = %d", len(report.ForwardedBindings))
	}
	foundTemperature := false
	for _, binding := range report.ForwardedBindings {
		for _, param := range binding.Params {
			if binding.Role == "reason" && binding.Profile == "fast_reasoning" && param.Name == "temperature" {
				foundTemperature = true
			}
		}
	}
	if !foundTemperature {
		t.Fatal("forwarded temperature param is missing")
	}
	if len(report.Sizing) != 15 {
		t.Fatalf("sizing lines = %d", len(report.Sizing))
	}
	for _, line := range report.Sizing {
		if line.Status != "unbenchmarked" || line.Basis == "" {
			t.Fatalf("sizing line = %#v", line)
		}
	}
}

func safeAgent(t *testing.T) *Agent {
	t.Helper()
	agent, err := Build(loadSafeCore(t))
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func allTargets(agent *Agent) []Target {
	names := sortedKeys(agent.Targets)
	result := make([]Target, 0, len(names))
	for _, name := range names {
		result = append(result, agent.Targets[name])
	}
	return result
}

func targetFor(agent *Agent, provider Provider) Target {
	for _, target := range agent.Targets {
		if target.Provider == provider {
			return target
		}
	}
	panic("target not found: " + provider)
}

func reportFor(report ValidateReport, provider Provider) TargetValidation {
	for _, row := range report.PerTarget {
		if row.Provider == provider {
			return row
		}
	}
	panic("report not found: " + provider)
}
