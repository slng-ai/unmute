package ir

import (
	"strings"
	"testing"

	packagespec "github.com/slng/unmute/internal/spec"
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

func TestValidateLanguage(t *testing.T) {
	agent := safeAgent(t)
	agent.Language = "not_a_language"
	report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "BCP-47") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateLangfuseTracingByTarget(t *testing.T) { // V25
	agent := safeAgent(t)
	agent.Tracing = &Tracing{Provider: "langfuse"}
	for provider, wantError := range map[Provider]bool{
		ProviderLiveKit: false, ProviderPipecat: false,
		ProviderVapi: true, ProviderElevenLabs: true, ProviderDeepgram: true,
	} {
		report, err := Validate(agent, []Target{targetFor(agent, provider)}, targetcap.Default())
		if (err != nil) != wantError {
			t.Errorf("%s: err=%v report=%#v", provider, err, report.PerTarget)
		}
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
	agent.Models["group_only_summarizer"] = ModelDef{Kind: KindThink, Placement: PlacementAPI}
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

func TestValidateOpenTurnBindingRequiredWhenAbsent(t *testing.T) {
	agent := safeAgent(t)
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

// TestValidateSpeakRequiredFieldsFromCatalog: entry-declared arity fires at
// validate time — an ElevenLabs speak binding needs a voice, an SLNG one a
// model — matching what generate-time resolution would reject anyway.
func TestValidateSpeakRequiredFieldsFromCatalog(t *testing.T) {
	agent := safeAgent(t)
	target := targetFor(agent, ProviderLiveKit)
	target.Models.Speak["front_desk"] = Binding{Provider: "elevenlabs", Model: "eleven_multilingual_v2"} // voice missing
	target.Models.Speak["specialist"] = Binding{Provider: "slng", Voice: "aura-2-orion-en"}              // model missing
	report, err := Validate(agent, []Target{target}, targetcap.Default())
	if err == nil {
		t.Fatal("expected speak required-field errors")
	}
	errors := strings.Join(report.PerTarget[0].Errors, "\n")
	for _, want := range []string{
		`speak.front_desk binding provider "elevenlabs" is missing a voice`,
		`speak.specialist binding provider "slng" is missing a model`,
	} {
		if !strings.Contains(errors, want) {
			t.Errorf("missing %q in %q", want, errors)
		}
	}
}

func TestValidateManagedSpeakRequiredFieldsFromCatalog(t *testing.T) {
	agent := safeAgent(t)
	target := targetFor(agent, ProviderElevenLabs)
	target.Models.Speak["front_desk"] = Binding{Provider: "elevenlabs", Model: "eleven_turbo_v2_5"}
	report, err := Validate(agent, []Target{target}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "missing a voice") {
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
	target := targetFor(agent, ProviderLiveKit)
	target.Carrier = ""
	report, err := Validate(agent, []Target{target}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "exact carrier Twilio") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateTelephonyPlanFailsClosedWithoutRouteSmoke(t *testing.T) { // telephony V4-V6
	pkg := loadSafeCore(t)
	target := pkg.Targets["pipecat"]
	target.Transport = "carrier-websocket"
	target.Carrier = "twilio"
	target.Connection = "primary_phone"
	pkg.Targets["pipecat"] = target
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	resolved := agent.Targets["pipecat"]
	report, err := Validate(agent, []Target{resolved}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "has not passed its credentialed smoke") {
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
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), `configured target "vapi"`) {
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

func TestT16_ListenFallbackGatesPerTarget(t *testing.T) {
	build := func(t *testing.T) *Agent {
		t.Helper()
		pkg := loadSafeCore(t)
		pkg.Agent.Models.Listen["backup_stt"] = packagespec.ModelDef{Provider: "soniox", Model: "stt-rt-v5"}
		primary := pkg.Agent.Models.Listen["transcriber"]
		primary.Fallback = []string{"backup_stt"}
		pkg.Agent.Models.Listen["transcriber"] = primary
		pkg.Agent.Listen = "transcriber"
		agent, err := Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		return agent
	}
	agent := build(t)
	// LiveKit is the one target with a native slot (stt.FallbackAdapter).
	// Soniox is not in the LiveKit catalogue, so rebind the fallback there.
	livekit := targetFor(agent, ProviderLiveKit)
	livekit.Models.ListenFallbacks[0].Binding = Binding{Provider: "deepgram", Model: "nova-2", Placement: PlacementAPI}
	if report, err := Validate(agent, []Target{livekit}, targetcap.Default()); err != nil {
		t.Fatalf("livekit must accept listen fallback; err=%v report=%#v", err, report.PerTarget)
	}
	for provider, want := range map[Provider]string{
		ProviderPipecat:    "does not emit listen fallback yet",
		ProviderVapi:       "no documented transcriber fallback slot",
		ProviderElevenLabs: "no STT fallback slot",
		ProviderDeepgram:   "single provider; there is no fallback slot",
	} {
		report, err := Validate(agent, []Target{targetFor(agent, provider)}, targetcap.Default())
		if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), want) {
			t.Errorf("%s: want %q gate, err=%v report=%#v", provider, want, err, report.PerTarget)
		}
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
	// 5 targets x (listen + turn + 2 speak + 2 reason): the package-wide turn
	// preference now reaches integrated-turn targets too (warned, not dropped).
	if len(report.ForwardedBindings) != 30 {
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
		// local tools lifted 2026-07-17 (driver-pipecat C9/T14) — no longer gated.
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
