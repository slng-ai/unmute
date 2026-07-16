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
	if strings.Contains(text, "return-to-prior-assistant") {
		t.Fatalf("task-group steps must not trigger the single-task gate: %q", text)
	}
}

func TestValidateWarnsWhenElevenLabsTaskReturns(t *testing.T) {
	agent := safeAgent(t)
	agent.Tasks["collect"] = Task{
		Instructions: "collect", Result: map[string]ResultField{"done": {Type: PrimitiveBoolean}},
		Context: TaskContext{History: HistoryFull},
	}
	agent.Controls["collect"] = &Delegate{Kind: ControlDelegate, Task: "collect"}
	report, err := Validate(agent, []Target{targetFor(agent, ProviderElevenLabs)}, targetcap.Default())
	if err != nil || !strings.Contains(strings.Join(report.PerTarget[0].Warnings, "\n"), "running transcript") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateElevenLabsBriefingMessageIsTwilioOnly(t *testing.T) { // driver-elevenlabs V8
	warm := &HumanTransfer{Kind: ControlHumanTransfer, Destination: "billing_line", Mode: TransferWarm, Briefing: BriefingMessage}
	sip := targetFor(safeAgent(t), ProviderElevenLabs) // no carrier -> SIP-style transfer
	twilio := targetFor(safeAgent(t), ProviderElevenLabs)
	twilio.Carrier = "twilio"

	agent := safeAgent(t)
	agent.Controls["to_human"] = warm
	report, err := Validate(agent, []Target{sip}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "native Twilio integration") {
		t.Fatalf("expected SIP briefing:message gate; err=%v report=%#v", err, report.PerTarget)
	}
	if report, err := Validate(agent, []Target{twilio}, targetcap.Default()); err != nil {
		t.Fatalf("carrier twilio must pass briefing:message; err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateTaskGroupOverridesMemberContext(t *testing.T) {
	agent := safeAgent(t)
	agent.Models["group_only_summarizer"] = ModelProfile{Placement: PlacementAPI}
	agent.Tasks["collect"] = Task{
		Instructions: "collect", Result: map[string]ResultField{"done": {Type: PrimitiveBoolean}},
		Context: TaskContext{History: HistorySummary, Summarizer: "group_only_summarizer"},
	}
	agent.TaskGroups["collect_then_end"] = TaskGroup{
		Steps: []string{"collect"}, ContextScope: ContextShared, Then: GroupEnd, Merge: GroupMergeResults,
	}
	report, err := Validate(agent, []Target{targetFor(agent, ProviderElevenLabs)}, targetcap.Default())
	if err != nil || strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "missing reason binding") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateWarnsWhenElevenLabsTaskGroupReturns(t *testing.T) {
	agent := safeAgent(t)
	agent.Tasks["collect"] = Task{
		Instructions: "collect", Result: map[string]ResultField{"done": {Type: PrimitiveBoolean}},
		Context: TaskContext{History: HistoryFull},
	}
	agent.TaskGroups["collect_then_return"] = TaskGroup{
		Steps: []string{"collect"}, ContextScope: ContextShared, Then: GroupReturn, Merge: GroupMergeResults,
	}
	report, err := Validate(agent, []Target{targetFor(agent, ProviderElevenLabs)}, targetcap.Default())
	if err != nil || !strings.Contains(strings.Join(report.PerTarget[0].Warnings, "\n"), "task-group turns") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
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

func TestValidateOpenTurnBindingIsIndependentOfPipelineBlock(t *testing.T) {
	agent := safeAgent(t)
	agent.Pipeline.Turn = nil
	target := targetFor(agent, ProviderLiveKit)
	target.Models.Turn = nil
	report, err := Validate(agent, []Target{target}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "missing open turn binding") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateSpeakProviderAndEndpointSlots(t *testing.T) {
	agent := safeAgent(t)
	target := targetFor(agent, ProviderElevenLabs)
	binding := target.Models.Speak["front_desk"]
	binding.Provider = "cartesia"
	binding.EndpointEnv = "CUSTOM_TTS_URL"
	target.Models.Speak["front_desk"] = binding
	report, err := Validate(agent, []Target{target}, targetcap.Default())
	if err == nil {
		t.Fatal("expected speak slot errors")
	}
	errors := strings.Join(report.PerTarget[0].Errors, "\n")
	for _, want := range []string{"custom speak endpoints have no slot", `speak binding provider "cartesia" has no slot`} {
		if !strings.Contains(errors, want) {
			t.Errorf("missing %q in %q", want, errors)
		}
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
	target.Carrier = ""
	report, err = Validate(agent, []Target{target}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "carrier Twilio AMD") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateRejectsVoicemailPolicyWithoutOutbound(t *testing.T) {
	agent := safeAgent(t)
	phone := agent.Channels["phone"]
	phone.OnVoicemail = VoicemailHangup
	agent.Channels["phone"] = phone
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "on_voicemail requires outbound: true") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateRequiresInterruptionEnabled(t *testing.T) {
	agent := safeAgent(t)
	agent.Conversation.Interruption.Enabled = nil
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "interruption.enabled is required") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateResolvesRequiredControlsAgainstTargetRoute(t *testing.T) {
	agent := safeAgent(t)
	phone := agent.Channels["phone"]
	phone.RequiredControls = append(phone.RequiredControls, "dtmf_send")
	agent.Channels["phone"] = phone
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "proven only for carrier Twilio") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateRejectsLiteralWebhookURL(t *testing.T) {
	agent := safeAgent(t)
	tool := agent.Tools["lookup_customer"]
	tool.Execution = ToolWebhook
	tool.Handler = ""
	tool.URLEnv = "https://example.com/hook"
	agent.Tools["lookup_customer"] = tool
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "environment variable name") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateNestedResultChecksEveryConfiguredTarget(t *testing.T) {
	agent := safeAgent(t)
	agent.Tasks["nested"] = Task{
		Instructions: "nested", Result: map[string]ResultField{"payload": {Schema: map[string]any{"type": "object"}}},
		Context: TaskContext{History: HistoryFull},
	}
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), `configured target "vapi-prod"`) {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateNestedResultRejectsUnknownConfiguredProvider(t *testing.T) {
	agent := safeAgent(t)
	agent.Tasks["nested"] = Task{
		Instructions: "nested", Result: map[string]ResultField{"payload": {Schema: map[string]any{"type": "object"}}},
		Context: TaskContext{History: HistoryFull},
	}
	livekit := targetFor(agent, ProviderLiveKit)
	agent.Targets = map[string]Target{"unknown": {Name: "unknown", Provider: "other"}}
	report, err := Validate(agent, []Target{livekit}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), `configured target "unknown" has unknown provider "other"`) {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateFallbackUsesConfiguredSlotKind(t *testing.T) {
	agent := safeAgent(t)
	profile := agent.Models["fast_reasoning"]
	profile.Fallback = []string{"careful_reasoning"}
	agent.Models["fast_reasoning"] = profile
	target := targetFor(agent, ProviderVapi)
	binding := target.Models.Reason["careful_reasoning"]
	binding.Provider = "anthropic"
	target.Models.Reason["careful_reasoning"] = binding

	caps := targetcap.Default()
	report, err := Validate(agent, []Target{target}, caps)
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "stay within one provider") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
	caps.FallbackSlots[targetcap.Vapi] = targetcap.FallbackProvider
	report, err = Validate(agent, []Target{target}, caps)
	if err != nil || strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "stay within one provider") {
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
	if len(report.Sizing) != 30 {
		t.Fatalf("sizing lines = %d", len(report.Sizing))
	}
	for _, line := range report.Sizing {
		if line.Status != "unbenchmarked" || !strings.Contains(line.Basis, "channels=realtime_audio,telephony") {
			t.Fatalf("sizing line = %#v", line)
		}
	}
	usageValue := ""
	for _, line := range report.Sizing {
		if line.Metric == "provider_session_time_quota.telephony" {
			usageValue = line.Value
			break
		}
	}
	if usageValue != "4h0m0s" {
		t.Fatalf("telephony session-time quota = %q", usageValue)
	}
	delete(agent.Channels, "web")
	report, err = Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err != nil || len(report.Sizing) != 4 {
		t.Fatalf("telephony-only sizing: err=%v lines=%#v", err, report.Sizing)
	}
	for _, line := range report.Sizing {
		if strings.Contains(line.Metric, "realtime_audio") {
			t.Fatalf("telephony-only sizing includes realtime audio: %#v", line)
		}
	}
}

func TestValidatePipecatMaturityGates(t *testing.T) { // driver-pipecat T1, C9
	tests := []struct {
		name   string
		mutate func(*Agent)
		want   string
	}{
		{"fallback", func(a *Agent) {
			profile := a.Models["fast_reasoning"]
			profile.Fallback = []string{"careful_reasoning"}
			a.Models["fast_reasoning"] = profile
		}, "does not emit generated fallback yet"},
		{"thinking_audio", func(a *Agent) { a.Conversation.ThinkingAudio = ThinkingSubtle }, "does not emit thinking audio yet"},
		{"local_tool", func(a *Agent) {
			tool := a.Tools["lookup_customer"]
			tool.Execution, tool.URLEnv, tool.Handler = ToolLocal, "", "tools/lookup_customer.py"
			a.Tools["lookup_customer"] = tool
		}, "does not emit local tool handlers yet"},
		{"mcp_tool", func(a *Agent) {
			tool := a.Tools["lookup_customer"]
			tool.Execution, tool.URLEnv = ToolMCP, ""
			a.Tools["lookup_customer"] = tool
		}, "does not emit MCP tools yet"},
		{"outbound", func(a *Agent) {
			phone := a.Channels["phone"]
			outbound := true
			phone.Outbound, phone.OnVoicemail = &outbound, VoicemailLeaveMessage
			a.Channels["phone"] = phone
		}, "does not emit outbound calling yet"},
		{"transfer_history", func(a *Agent) {
			a.Controls["to_billing"].(*AgentTransfer).Context.History = HistoryMessages
		}, "history: full only"},
		{"transfer_variable_subset", func(a *Agent) {
			a.Controls["to_billing"].(*AgentTransfer).Context.Variables = VariableSelection{Names: []string{"customer_id"}}
		}, "variables subset"},
		{"transfer_no_tool_calls", func(a *Agent) {
			no := false
			a.Controls["to_billing"].(*AgentTransfer).Context.IncludeToolCalls = &no
		}, "include_tool_calls"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			agent := safeAgent(t)
			test.mutate(agent)
			report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
			if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), test.want) {
				t.Fatalf("err=%v report=%#v", err, report.PerTarget)
			}
		})
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
