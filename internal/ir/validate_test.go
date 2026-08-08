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
	if len(report.PerTarget) != 4 {
		t.Fatalf("target rows = %d", len(report.PerTarget))
	}
	for _, provider := range []Provider{ProviderLiveKit, ProviderPipecat, ProviderVapi, ProviderDeepgram} {
		if row := reportFor(report, provider); len(row.Errors) != 0 {
			t.Fatalf("%s row has errors: %#v", provider, row)
		}
	}
}

func TestValidateLanguage(t *testing.T) { // N16: language is validated per model
	agent := safeAgent(t)
	def := agent.Models["front_desk"]
	def.Language = "not_a_language"
	agent.Models["front_desk"] = def
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
		ProviderVapi: true, ProviderDeepgram: true,
	} {
		report, err := Validate(agent, []Target{targetFor(agent, provider)}, targetcap.Default())
		if (err != nil) != wantError {
			t.Errorf("%s: err=%v report=%#v", provider, err, report.PerTarget)
		}
	}
}

// N16: language belongs on speak/listen models only; on a think/turn model it
// would be silently dropped at generate, so validate rejects it.
func TestValidateLanguageOnThinkModelRejected(t *testing.T) {
	agent := safeAgent(t)
	def := agent.Models["fast_reasoning"] // a think model
	def.Language = "es"
	agent.Models["fast_reasoning"] = def
	report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "language applies only to speak and listen") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

// N16: a language set on a vendor whose integration has no language slot must
// fail at VALIDATE, not just generate (C6: gate before any artifact). gemini
// TTS is NoLanguage on livekit.
func TestValidateLanguageSlotGate(t *testing.T) {
	agent := safeAgent(t)
	target := targetFor(agent, ProviderLiveKit)
	target.Models.Speak["front_desk"] = Binding{Provider: "gemini", Voice: "Kore", Language: "es"}
	report, err := Validate(agent, []Target{target}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "has no language slot") {
		t.Fatalf("expected a validate-time language-slot gate; err=%v report=%#v", err, report.PerTarget)
	}
}

// LiveKit has no OpenAI-compatible speak wildcard (N14): a speak binding with
// an endpoint_env is gated. Restores coverage dropped with the ElevenLabs sweep.
func TestValidateLiveKitSpeakEndpointGate(t *testing.T) {
	agent := safeAgent(t)
	target := targetFor(agent, ProviderLiveKit)
	b := target.Models.Speak["front_desk"]
	b.EndpointEnv = "CUSTOM_TTS_URL"
	target.Models.Speak["front_desk"] = b
	report, err := Validate(agent, []Target{target}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "no OpenAI-compatible speak wildcard") {
		t.Fatalf("expected the livekit speak-endpoint gate; err=%v report=%#v", err, report.PerTarget)
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
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err != nil || strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "missing reason binding") {
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
	report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "history: full only") {
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

func TestValidateCapacity(t *testing.T) { // V12
	agent := safeAgent(t)
	agent.Capacity.PeakSessions = agent.Capacity.MaxSessions + 1
	report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "must not exceed") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateTelephonyRequiresPeakStartRateWithoutConnection(t *testing.T) {
	agent := safeAgent(t)
	agent.Channels["phone"] = testTelephonyChannel()
	agent.Capacity.PeakStartsPerSecond = 0
	report, err := Validate(agent, []Target{targetFor(agent, ProviderVapi)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "capacity.peak_starts_per_second must be positive for telephony") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateOutboundRequiresSatisfiableVariablesAndWarnsOnDeepgram(t *testing.T) { // V13
	agent := safeAgent(t)
	phone := testTelephonyChannel()
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
	phone := testTelephonyChannel()
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

func TestValidateCodeTelephonyRequiresResolvedPlan(t *testing.T) {
	agent := safeAgent(t)
	agent.Channels["phone"] = testTelephonyChannel()
	target := targetFor(agent, ProviderLiveKit)
	report, err := Validate(agent, []Target{target}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "telephony channel requires a resolved Connection plan") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

// A provisional telephony route (real adapter, no credentialed smoke) is
// usable and validates silently: no error and no telephony warning. The
// provisional status lives in the compile report, not the user's console. Only
// Gated routes (no adapter) stay hard errors.
func TestValidateTelephonyProvisionalRouteIsUsableAndQuiet(t *testing.T) {
	pkg := loadSafeCore(t)
	enableTelephony(pkg)
	target := pkg.Targets["pipecat"]
	target.Transport = "carrier-websocket"
	target.Carrier = "twilio"
	target.Connection = "primary_phone"
	pkg.Targets = map[string]packagespec.Target{"pipecat": target}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	resolved := agent.Targets["pipecat"]
	report, err := Validate(agent, []Target{resolved}, targetcap.Default())
	if err != nil {
		t.Fatalf("provisional route must not block, got errors=%#v", report.PerTarget[0].Errors)
	}
	for _, w := range report.PerTarget[0].Warnings {
		if strings.Contains(w, "telephony") {
			t.Fatalf("provisional route must not print a telephony warning, got %q", w)
		}
	}
}

// T1/V1: a Pipecat carrier-websocket target may be outbound-capable without
// on_voicemail. No voicemail error of any kind is raised when it is omitted.
func TestValidateOutboundWithoutVoicemailRaisesNoVoicemailError(t *testing.T) {
	pkg := loadSafeCore(t)
	enableTelephony(pkg)
	phone := pkg.Agent.Channels["phone"]
	outbound := true
	phone.Outbound = &outbound
	pkg.Agent.Channels["phone"] = phone
	target := pkg.Targets["pipecat"]
	target.Transport, target.Carrier, target.Connection = "carrier-websocket", "twilio", "primary_phone"
	pkg.Targets = map[string]packagespec.Target{"pipecat": target}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	report, _ := Validate(agent, []Target{agent.Targets["pipecat"]}, targetcap.Default())
	joined := strings.Join(report.PerTarget[0].Errors, "\n")
	if strings.Contains(joined, "voicemail") {
		t.Fatalf("outbound without on_voicemail must raise no voicemail error, got:\n%s", joined)
	}
}

// T1/V2: setting on_voicemail on Pipecat still fails, because the Pipecat route
// cannot detect voicemail. Decoupling outbound from voicemail never enables it.
func TestValidatePipecatOnVoicemailStillErrors(t *testing.T) {
	pkg := loadSafeCore(t)
	enableTelephony(pkg)
	phone := pkg.Agent.Channels["phone"]
	outbound := true
	phone.Outbound = &outbound
	phone.OnVoicemail = "hangup"
	pkg.Agent.Channels["phone"] = phone
	target := pkg.Targets["pipecat"]
	target.Transport, target.Carrier, target.Connection = "carrier-websocket", "twilio", "primary_phone"
	pkg.Targets = map[string]packagespec.Target{"pipecat": target}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Validate(agent, []Target{agent.Targets["pipecat"]}, targetcap.Default())
	joined := strings.Join(report.PerTarget[0].Errors, "\n")
	if err == nil || !strings.Contains(joined, "voicemail") {
		t.Fatalf("pipecat + on_voicemail must still error on voicemail support, err=%v:\n%s", err, joined)
	}
}

func TestValidateTelephonyPlanRejectsOrphanRedisAndUnknownConsumers(t *testing.T) { // telephony V13, V23
	pkg := loadSafeCore(t)
	enableTelephony(pkg)
	target := pkg.Targets["pipecat"]
	target.Transport, target.Carrier, target.Connection = "carrier-websocket", "twilio", "primary_phone"
	pkg.Targets = map[string]packagespec.Target{"pipecat": target}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	resolved := agent.Targets["pipecat"]
	reasons := append([]TelephonyCoordinationReason(nil), resolved.Telephony.CoordinationReasons...)

	resolved.Telephony.CoordinationReasons = nil
	report, err := Validate(agent, []Target{resolved}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "Redis service has no coordination consumer") {
		t.Fatalf("orphan Redis: err=%v report=%#v", err, report.PerTarget)
	}

	resolved.Telephony.CoordinationReasons = reasons
	resolved.Telephony.CoordinationReasons[0].Consumers = []string{"undeclared"}
	report, err = Validate(agent, []Target{resolved}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), `undeclared consumer "undeclared"`) {
		t.Fatalf("unknown consumer: err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateExotelTelephonyFailsClosedWithoutAuthenticatedWebSocket(t *testing.T) { // telephony T9, V4-V6
	pkg := loadSafeCore(t)
	enableTelephony(pkg)
	target := pkg.Targets["pipecat"]
	target.Transport = "carrier-websocket"
	target.Carrier = "exotel"
	target.Connection = "primary_phone"
	pkg.Targets = map[string]packagespec.Target{"pipecat": target}
	connection := pkg.Connections["primary_phone"]
	connection.Environment = map[string]string{
		"api_key": "EXOTEL_API_KEY", "api_token": "EXOTEL_API_TOKEN",
		"account_sid": "EXOTEL_ACCOUNT_SID", "subdomain": "EXOTEL_SUBDOMAIN",
		"from_number": "EXOTEL_PHONE_NUMBER", "app_id": "EXOTEL_APP_ID",
	}
	pkg.Connections["primary_phone"] = connection
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	resolved := agent.Targets["pipecat"]
	report, err := Validate(agent, []Target{resolved}, targetcap.Default())
	errors := strings.Join(report.PerTarget[0].Errors, "\n")
	if err == nil || !strings.Contains(errors, "does not support route") {
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

// makeBuiltin rewrites an existing safe_core tool into a clean end_call
// builtin, keeping its references intact.
func makeBuiltin(agent *Agent, name string) {
	tool := agent.Tools[name]
	tool.Execution = ToolBuiltin
	tool.Builtin = "end_call"
	tool.Input, tool.Output, tool.URLEnv, tool.Handler = nil, nil, "", ""
	tool.Effect = ToolEndsConversation
	tool.Description = "End the call."
	agent.Tools[name] = tool
}

func TestValidateBuiltinEndCallPassesOnLiveKit(t *testing.T) {
	agent := safeAgent(t)
	makeBuiltin(agent, "lookup_customer")
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err != nil {
		t.Fatalf("end_call builtin must validate on LiveKit: %v\n%#v", err, report.PerTarget)
	}
}

func TestValidateBuiltinDeniedOnVapiInProviderWords(t *testing.T) {
	agent := safeAgent(t)
	makeBuiltin(agent, "lookup_customer")
	report, err := Validate(agent, []Target{targetFor(agent, ProviderVapi)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "Vapi") {
		t.Fatalf("builtin on Vapi must gate in Vapi vocabulary: err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateBuiltinUnknownIDRejected(t *testing.T) {
	agent := safeAgent(t)
	makeBuiltin(agent, "lookup_customer")
	tool := agent.Tools["lookup_customer"]
	tool.Builtin = "teleport"
	agent.Tools["lookup_customer"] = tool
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "teleport") {
		t.Fatalf("unknown builtin id must be rejected: err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateBuiltinRejectsWebhookFields(t *testing.T) {
	agent := safeAgent(t)
	makeBuiltin(agent, "lookup_customer")
	tool := agent.Tools["lookup_customer"]
	tool.URLEnv = "SOME_URL"
	agent.Tools["lookup_customer"] = tool
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "lookup_customer") {
		t.Fatalf("builtin tool with url_env must be rejected: err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateBuiltinFieldsRejectedOnNonBuiltin(t *testing.T) {
	agent := safeAgent(t)
	tool := agent.Tools["lookup_customer"] // stays webhook
	tool.Instructions = "goodbye"
	agent.Tools["lookup_customer"] = tool
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "instructions") {
		t.Fatalf("instructions on a non-builtin tool must be rejected: err=%v report=%#v", err, report.PerTarget)
	}
}

func TestValidateBuiltinRejectsConflictingEffect(t *testing.T) {
	agent := safeAgent(t)
	makeBuiltin(agent, "lookup_customer")
	tool := agent.Tools["lookup_customer"]
	tool.Effect = ToolReturnsData // conflicts with end_call's ends_conversation
	agent.Tools["lookup_customer"] = tool
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "effect") {
		t.Fatalf("conflicting effect on a builtin tool must be rejected: err=%v report=%#v", err, report.PerTarget)
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
		ProviderPipecat:  "does not emit listen fallback yet",
		ProviderVapi:     "no documented transcriber fallback slot",
		ProviderDeepgram: "single provider; there is no fallback slot",
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
	// 4 targets x (listen + turn + 2 speak + 2 reason): the package-wide turn
	// preference now reaches integrated-turn targets too (warned, not dropped).
	if len(report.ForwardedBindings) != 24 {
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
	// 4 targets x (workers + gpus + realtime_audio concurrency + session-time) = 16.
	if len(report.Sizing) != 16 {
		t.Fatalf("sizing lines = %d", len(report.Sizing))
	}
	for _, line := range report.Sizing {
		if line.Status != "unbenchmarked" || !strings.Contains(line.Basis, "channels=realtime_audio") {
			t.Fatalf("sizing line = %#v", line)
		}
	}
	agent.Channels = map[string]Channel{"phone": testTelephonyChannel()}
	report, err = Validate(agent, []Target{targetFor(agent, ProviderVapi)}, targetcap.Default())
	if err != nil || len(report.Sizing) != 5 {
		t.Fatalf("telephony-only sizing: err=%v lines=%#v", err, report.Sizing)
	}
	for _, line := range report.Sizing {
		if strings.Contains(line.Metric, "realtime_audio") {
			t.Fatalf("telephony-only sizing includes realtime audio: %#v", line)
		}
	}
	if got := report.Sizing[len(report.Sizing)-1]; got.Metric != "provider_call_start_rate.telephony" || got.Value != "4" {
		t.Fatalf("telephony call-start sizing = %#v", got)
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

func testTelephonyChannel() Channel {
	inbound, outbound := true, false
	return Channel{
		Kind: ChannelTelephony, Inbound: &inbound, Outbound: &outbound,
		RequiredControls: []string{"cold_transfer", "hangup"},
	}
}

// TestValidateToolSchemaReportsYAMLAccidents: tool input/output are
// map[string]any, so strict YAML decoding stops at that boundary and a bad key
// travels into the emitted tool contract (Pipecat serializes the whole
// properties map verbatim). A valueless spaced key is what an unquoted comma in
// a YAML flow mapping leaves behind, and it is how a fixture shipped
// `description: The requested date, e.g. 2026-08-14` as a truncated description
// plus a null-valued key `e.g. 2026-08-14`.
//
// It is reported, never rejected. `{"e.g. 2026-08-14": null}` is a valid schema
// because unknown keywords must be ignored whatever their value, so failing on
// it would fail something N10 and D10 permit. Turning silence into a named,
// path-precise warning is the whole win available here.
func TestValidateToolSchemaReportsYAMLAccidents(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(tool *Tool)
		want   string
	}{
		{
			name:   "top level",
			mutate: func(tool *Tool) { tool.Input["oops a space"] = nil },
			want:   `has an empty schema key "oops a space" at input`,
		},
		{
			name: "inside a property, the shape that shipped",
			mutate: func(tool *Tool) {
				prop := tool.Input["properties"].(map[string]any)["phone"].(map[string]any)
				prop["e.g. 2026-08-14"] = nil
			},
			want: `has an empty schema key "e.g. 2026-08-14" at input.properties.phone`,
		},
		{
			name: "inside items",
			mutate: func(tool *Tool) {
				prop := tool.Input["properties"].(map[string]any)["phone"].(map[string]any)
				prop["items"] = map[string]any{"type": "string", "and more": nil}
			},
			want: `has an empty schema key "and more" at input.properties.phone.items`,
		},
		{
			name: "inside draft-07 dependencies, which is a subschema position",
			mutate: func(tool *Tool) {
				tool.Input["dependencies"] = map[string]any{
					"phone": map[string]any{"required": []any{"email"}, "and more": nil},
				}
			},
			want: `has an empty schema key "and more" at input.dependencies.phone`,
		},
		{
			name: "in output as well as input",
			mutate: func(tool *Tool) {
				tool.Output = map[string]any{"type": "object", "trailing text": nil}
			},
			want: `has an empty schema key "trailing text" at output`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := safeAgent(t)
			tool := agent.Tools["lookup_customer"]
			tc.mutate(&tool)
			agent.Tools["lookup_customer"] = tool
			report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
			if err != nil {
				t.Fatalf("a legal schema must not fail: %v %#v", err, report.PerTarget)
			}
			if !strings.Contains(strings.Join(report.PerTarget[0].Warnings, "\n"), tc.want) {
				t.Fatalf("want warning %q, got %#v", tc.want, report.PerTarget[0].Warnings)
			}
		})
	}
}

// TestValidateToolSchemaAcceptsValidSchemas: JSON Schema requires unknown
// keywords to be ignored, and SCHEMA.md N10 inherits that by calling tool input
// "a JSON Schema object" without narrowing it. So nothing valid may fail: not a
// keyword the drivers never read, not a vendor extension, and not author data
// under `default`/`examples`, which must never be descended into as if it were
// a subschema.
func TestValidateToolSchemaAcceptsValidSchemas(t *testing.T) {
	agent := safeAgent(t)
	tool := agent.Tools["lookup_customer"]
	prop := tool.Input["properties"].(map[string]any)["phone"].(map[string]any)
	prop["minLength"] = 12
	prop["pattern"] = `^\+[0-9]+$`
	prop["format"] = "phone"
	prop["discriminator"] = "openapi dialect"
	prop["x-vendor-hint"] = "vendor extension"
	prop["default"] = map[string]any{"not_a_schema_key": true}
	prop["examples"] = []any{map[string]any{"also_not_a_schema_key": true}}
	agent.Tools["lookup_customer"] = tool

	report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err != nil {
		t.Fatalf("valid schema rejected: %v %#v", err, report.PerTarget)
	}
	if joined := strings.Join(report.PerTarget[0].Warnings, "\n"); strings.Contains(joined, "x-vendor-hint") ||
		strings.Contains(joined, "discriminator") || strings.Contains(joined, "not_a_schema_key") {
		t.Fatalf("valid schema warned: %q", joined)
	}
}

// TestValidateToolSchemaWarnsOnUnrecognisedKey: a plain typo is legal JSON
// Schema (unknown keywords are ignored), so it cannot fail the build. It still
// reaches the provider unread, so it earns a warning: stderr, exit 0.
//
// The second case guards the boundary of the accident rule. A member name may
// legally contain spaces, so whitespace alone must not fail; only the valueless
// form does. A spaced key that carries a value is something a human wrote.
func TestValidateToolSchemaWarnsOnUnrecognisedKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		set  any
		want string
	}{
		{
			name: "typo",
			key:  "descriptoin",
			set:  "typo for description",
			want: `has unrecognised schema key "descriptoin" at input.properties.phone`,
		},
		{
			name: "spaced key that carries a value is legal, not an accident",
			key:  "my annotation",
			set:  "deliberate",
			want: `has unrecognised schema key "my annotation" at input.properties.phone`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := safeAgent(t)
			tool := agent.Tools["lookup_customer"]
			prop := tool.Input["properties"].(map[string]any)["phone"].(map[string]any)
			prop[tc.key] = tc.set
			agent.Tools["lookup_customer"] = tool

			report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
			if err != nil {
				t.Fatalf("an unrecognised keyword must not fail: %v", err)
			}
			joined := strings.Join(report.PerTarget[0].Warnings, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("want warning %q, got %#v", tc.want, report.PerTarget[0].Warnings)
			}
			// The warning must make no claim about the key surviving into the
			// emitted project. That answer needs three axes: LiveKit drops
			// unread keys everywhere; Pipecat keeps them only on the Flow-node
			// path, where the properties map is serialised into bot.py verbatim;
			// and a top-level input key is dropped by both. Any one-line summary
			// is wrong often enough to mislead, so the matrix lives in N19.
			for _, promise := range []string{"forwarded to the provider", "depends on the driver", "surviv", "reaches the provider"} {
				if strings.Contains(joined, promise) {
					t.Fatalf("warning makes an unsupportable survival claim %q: %q", promise, joined)
				}
			}
		})
	}
}

// TestValidateTaskResultSchemaKeysReported: a nested task result field carries a
// raw schema (build.go stashes any unrecognised map as ResultField.Schema) and
// the Pipecat driver serialises it through resultProperties/pyLiteral exactly as
// it serialises tool properties. Same unvalidated surface, so the same walk has
// to reach it, named for the field rather than for a tool.
func TestValidateTaskResultSchemaKeysReported(t *testing.T) {
	agent := safeAgent(t)
	agent.Tasks["collect"] = Task{
		Instructions: "Collect the caller's account details.",
		Result: map[string]ResultField{"details": {Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"city": map[string]any{"type": "string", "descriptoin": "typo"}},
		}}},
		Context: TaskContext{History: HistoryFull},
	}
	agent.Controls["run_collect"] = &Delegate{Kind: ControlDelegate, Task: "collect", When: "Collect details."}
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "run_collect")
	agent.Agents["intake"] = intake

	// safe_core also declares Vapi, which gates nested task results, so the run
	// fails for that unrelated reason. What matters here is that the schema key
	// lands in Warnings and never in Errors.
	report, _ := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	want := `task "collect" result "details" has unrecognised schema key "descriptoin" at schema.properties.city`
	if !strings.Contains(strings.Join(report.PerTarget[0].Warnings, "\n"), want) {
		t.Fatalf("want warning %q, got %#v", want, report.PerTarget[0].Warnings)
	}
	if strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "schema key") {
		t.Fatalf("a schema key must never become an error: %#v", report.PerTarget[0].Errors)
	}
}

func reportFor(report ValidateReport, provider Provider) TargetValidation {
	for _, row := range report.PerTarget {
		if row.Provider == provider {
			return row
		}
	}
	panic("report not found: " + provider)
}
