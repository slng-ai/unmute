package ir

import (
	"slices"
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
}

// The four capability rows that used to gate Vapi and Deepgram on their target's
// carrier lost that condition when `carrier` left the target, because no author
// can write one where those rows would see it and a refusal naming an impossible
// fix is worse than no condition at all (spec FR-001a, research R11).
//
// safe_core only ever reached the Deepgram cold-transfer row. The other three
// are unreachable from any shipped package, so without this they would change
// untested. The Twilio requirement each row records survives as a comment in
// internal/target/table.go for whoever builds those drivers.
func TestDriverlessProvidersResolveTransfersWithNoCarrier(t *testing.T) {
	agent := safeAgent(t)
	phone := testTelephonyChannel()
	outbound := true
	phone.Outbound = &outbound
	agent.Channels["phone"] = phone

	for _, provider := range []Provider{ProviderVapi, ProviderDeepgram} {
		target := targetFor(agent, provider)
		if target.Carrier != "" {
			t.Fatalf("%s carries a carrier %q; this feature removed the field from every target", provider, target.Carrier)
		}
		for _, control := range []targetcap.TelephonyControl{
			targetcap.ColdTransfer, targetcap.WarmTransfer, targetcap.VoicemailDetection,
		} {
			resolved := targetcap.Default().Control(control, targetcap.Provider(provider), target.Transport, target.Carrier)
			if resolved.Tag == targetcap.Gated && strings.Contains(resolved.Note, "carrier") {
				t.Errorf("%s %s is gated on a carrier no author can write: %s", provider, control, resolved.Note)
			}
		}
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
	// The carrier-websocket routes carry no transfers (SPEC C1, V1), so the
	// provisional-route fixture must not declare one: drop the control, its
	// tool reference, and the channel's cold_transfer requirement.
	delete(pkg.Agent.Controls, "to_human")
	billing := pkg.Agent.Agents["billing"]
	billing.Tools = slices.DeleteFunc(slices.Clone(billing.Tools), func(name string) bool { return name == "to_human" })
	pkg.Agent.Agents["billing"] = billing
	phone := pkg.Agent.Channels["phone"]
	phone.RequiredControls = []string{"hangup"}
	pkg.Agent.Channels["phone"] = phone
	routeTarget(pkg, "pipecat", "primary_phone", "carrier-websocket", "twilio")
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
	routeTarget(pkg, "pipecat", "primary_phone", "carrier-websocket", "twilio")
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
	routeTarget(pkg, "pipecat", "primary_phone", "carrier-websocket", "twilio")
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
	routeTarget(pkg, "pipecat", "primary_phone", "carrier-websocket", "twilio")
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

// Exotel still fails closed, and now it fails one stage earlier and says more.
// Its two rows carry a real environment vocabulary and an empty feature map, so
// they are in the catalog but not selectable; naming one in a connection is
// refused at build with the routes the provider does support, and that list
// never suggests Exotel back (spec FR-011a, research R6).
func TestValidateExotelTelephonyFailsClosedWithoutAuthenticatedWebSocket(t *testing.T) { // telephony T9, V4-V6
	pkg := loadSafeCore(t)
	enableTelephony(pkg)
	routeTarget(pkg, "pipecat", "primary_phone", "carrier-websocket", "exotel")
	connection := pkg.Connections["primary_phone"]
	connection.Environment = map[string]string{
		"api_key": "EXOTEL_API_KEY", "api_token": "EXOTEL_API_TOKEN",
		"account_sid": "EXOTEL_ACCOUNT_SID", "subdomain": "EXOTEL_SUBDOMAIN",
		"from_number": "EXOTEL_PHONE_NUMBER", "app_id": "EXOTEL_APP_ID",
	}
	pkg.Connections["primary_phone"] = connection
	_, err := Build(pkg)
	if err == nil {
		t.Fatal("an Exotel connection compiled; the route has no feature the emitter can honour")
	}
	for _, want := range []string{`carrier "exotel" is not a route`, "pipecat supports:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal is missing %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "with exotel") {
		t.Errorf("the refusal suggests exotel, which is the refusal the author just hit: %v", err)
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

// bearerAuth and apiKeyAuth are the two schemes with their Build defaults
// already applied, as validateToolAuth sees them.
func bearerAuth() *ToolAuth {
	return &ToolAuth{Type: ToolAuthBearer, TokenEnv: "LOOKUP_CUSTOMER_TOKEN"}
}

func apiKeyAuth() *ToolAuth {
	return &ToolAuth{Type: ToolAuthAPIKey, TokenEnv: "LOOKUP_CUSTOMER_KEY", Header: DefaultAPIKeyHeader}
}

// withToolAuth puts an auth block on the fixture's webhook tool.
func withToolAuth(t *testing.T, auth *ToolAuth) *Agent {
	t.Helper()
	agent := safeAgent(t)
	tool := agent.Tools["lookup_customer"] // webhook in the fixture
	tool.Auth = auth
	agent.Tools["lookup_customer"] = tool
	return agent
}

// TestValidateWebhookAuthSchemes covers both schemes (SCHEMA §5.3): each valid
// form passes, and a missing token, a literal secret, a cross-scheme field, an
// unknown scheme, and a missing type are all errors.
func TestValidateWebhookAuthSchemes(t *testing.T) {
	for name, auth := range map[string]*ToolAuth{"bearer": bearerAuth(), "api_key": apiKeyAuth()} {
		t.Run(name+" passes", func(t *testing.T) {
			agent := withToolAuth(t, auth)
			if report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default()); err != nil {
				t.Fatalf("%s webhook must validate on LiveKit: %v\n%#v", name, err, report.PerTarget)
			}
		})
	}

	literalToken := bearerAuth()
	literalToken.TokenEnv = "sk-live-1234"
	crossScheme := bearerAuth()
	crossScheme.Header = "X-API-Key"
	apiKeyNoHeader := apiKeyAuth()
	apiKeyNoHeader.Header = ""

	for _, tc := range []struct {
		name string
		auth *ToolAuth
		want string
	}{
		{"bearer without token_env", &ToolAuth{Type: ToolAuthBearer}, "token_env is required"},
		{"literal token", literalToken, "never a secret value"},
		{"bearer carrying an api_key field", crossScheme, "header is not a bearer field"},
		{"api_key without header", apiKeyNoHeader, "header is required for api_key"},
		{"no type", &ToolAuth{TokenEnv: "LOOKUP_CUSTOMER_TOKEN"}, "type is required"},
		{"hmac is not a scheme", &ToolAuth{Type: "hmac", TokenEnv: "LOOKUP_CUSTOMER_TOKEN"}, "type must be bearer or api_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := withToolAuth(t, tc.auth)
			report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
			if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), tc.want) {
				t.Fatalf("want error containing %q: err=%v report=%#v", tc.want, err, report.PerTarget)
			}
		})
	}

	// Auth belongs to the two blocks that make a request of their own: webhook
	// and mcp (N40). A local handler owns its own credential in Python.
	t.Run("auth on a local tool", func(t *testing.T) {
		agent := safeAgent(t)
		tool := agent.Tools["lookup_customer"]
		tool.Execution, tool.URLEnv = ToolLocal, ""
		tool.Handler, tool.Auth = "tools/lookup_customer.py", bearerAuth()
		agent.Tools["lookup_customer"] = tool
		report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
		if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "webhook and mcp execution only") {
			t.Fatalf("auth outside a webhook or mcp tool must be rejected: err=%v report=%#v", err, report.PerTarget)
		}
	})

	t.Run("gated on Vapi in provider words", func(t *testing.T) {
		agent := withToolAuth(t, bearerAuth())
		report, err := Validate(agent, []Target{targetFor(agent, ProviderVapi)}, targetcap.Default())
		if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "Vapi") {
			t.Fatalf("webhook auth on Vapi must gate in Vapi vocabulary: err=%v report=%#v", err, report.PerTarget)
		}
	})
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

// mcpAgent turns the safe core's lookup_customer into an MCP tool source, so
// each case below changes exactly the one field it is about (N40).
func mcpAgent(t *testing.T, mutate func(*Tool)) *Agent {
	t.Helper()
	agent := safeAgent(t)
	tool := Tool{
		Execution: ToolMCP, URLEnv: "FIRECRAWL_MCP_URL",
		Interruption: ToolProviderDefault, Effect: ToolReturnsData,
	}
	mutate(&tool)
	agent.Tools["lookup_customer"] = tool
	agent.Secrets = append(agent.Secrets, "FIRECRAWL_MCP_URL", "FIRECRAWL_API_KEY")
	return agent
}

// TestValidateMCPToolSource covers the block's own rules: the transport is one
// of two names, the selection is a list of distinct non-empty names, and auth
// is legal here and holds an env name rather than a secret (N40, SC-005).
func TestValidateMCPToolSource(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Tool)
		want   string
	}{
		{"unknown transport", func(tool *Tool) { tool.MCPTransport = "websocket" },
			`transport must be sse or streamable_http, not "websocket"`},
		{"literal token", func(tool *Tool) {
			tool.Auth = &ToolAuth{Type: ToolAuthBearer, TokenEnv: "fc-live-not-a-real-key"}
		}, "never a secret value"},
		{"repeated selection", func(tool *Tool) {
			tool.MCPTools = []string{"firecrawl_search", "firecrawl_search"}
		}, `tools names "firecrawl_search" twice`},
		{"empty selection entry", func(tool *Tool) { tool.MCPTools = []string{"firecrawl_search", " "} },
			"tools has an empty entry"},
		{"contract field", func(tool *Tool) { tool.Description = "Search the web." },
			"takes no description, input, or output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := mcpAgent(t, tc.mutate)
			report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
			if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), tc.want) {
				t.Fatalf("want error containing %q: err=%v report=%#v", tc.want, err, report.PerTarget)
			}
		})
	}

	// The whole block, spelled the way the example spells it, validates and
	// puts both env names in the report with the site that names them (FR-009).
	t.Run("full block", func(t *testing.T) {
		agent := mcpAgent(t, func(tool *Tool) {
			tool.MCPTransport = MCPTransportStreamableHTTP
			tool.MCPTools = []string{"firecrawl_search"}
			tool.Auth = &ToolAuth{Type: ToolAuthBearer, TokenEnv: "FIRECRAWL_API_KEY"}
		})
		if _, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default()); err != nil {
			t.Fatalf("the full mcp block must validate: %v", err)
		}
		sites := EnvReferenceSites(agent)
		for env, want := range map[string]string{
			"FIRECRAWL_MCP_URL": "tools/lookup_customer.yaml mcp.url_env",
			"FIRECRAWL_API_KEY": "tools/lookup_customer.yaml mcp.auth.token_env",
		} {
			if got := sites[env]; len(got) != 1 || got[0] != want {
				t.Errorf("reference site for %s = %v, want [%s]", env, got, want)
			}
		}
	})
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
		// local tools lifted 2026-07-17 (driver-pipecat C9/T14), mcp tool
		// sources 2026-08-14 (N40) — neither is gated. What is still gated is
		// the scope: an mcp source listed on a task, below.
		{"mcp_task_scope", func(a *Agent) {
			a.Tools["lookup_customer"] = Tool{
				Execution: ToolMCP, URLEnv: "BOOKINGS_MCP_URL",
				Interruption: ToolProviderDefault, Effect: ToolReturnsData,
			}
			// The scope is what the gate is about, so the task holds nothing
			// else; other errors from the bare task are not what is asserted.
			a.Tasks["confirm_booking"] = Task{Tools: []string{"lookup_customer"}}
		}, "cannot scope an MCP tool source to a task"},
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
			// is wrong often enough to mislead, so the matrix lives in N21.
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

// V12/B5: a warm transfer dials its destination, so a package containing one
// must declare an outbound phone direction. examples/livekit-human-transfer shipped
// with `outbound: false`, validated green, and then had `--to` refused as if
// the direction had been removed from the driver (B5).
func TestV12_WarmTransferRequiresOutboundDirection(t *testing.T) {
	for _, outbound := range []bool{false, true} {
		pkg := loadSafeCore(t)
		enableTelephony(pkg)
		phone := pkg.Agent.Channels["phone"]
		phone.Outbound = &outbound
		pkg.Agent.Channels["phone"] = phone
		// The shipped cold transfer, made warm: same tool wiring, same
		// destination, only the shape block differs.
		human := pkg.Agent.Controls["to_human"]
		human.Cold, human.Warm = nil, &packagespec.WarmTransfer{Destination: "billing_line"}
		pkg.Agent.Controls["to_human"] = human
		routeTarget(pkg, "pipecat", "primary_phone", "carrier-websocket", "twilio")
		agent, err := Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		report, _ := Validate(agent, []Target{agent.Targets["pipecat"]}, targetcap.Default())
		joined := strings.Join(report.PerTarget[0].Errors, "\n")
		if got := strings.Contains(joined, "needs outbound: true"); got == outbound {
			t.Fatalf("outbound=%v: direction error present=%v, want %v:\n%s", outbound, got, !outbound, joined)
		}
	}
}

// SPEC V1/C1: a transfer compiles only where the platform ships the
// primitive, and the refusal names the routes that do. Warm on any Pipecat
// target is refused by the control row; warm on a Pipecat carrier telephony
// route is refused by the route table, with the supported routes named.
func TestV1_PipecatWarmTransferFailsWithSupportedRoutesNamed(t *testing.T) {
	// Non-telephony Pipecat target (safe_core's daily-sip): the control row.
	pkg := loadSafeCore(t)
	human := pkg.Agent.Controls["to_human"]
	human.Cold, human.Warm = nil, &packagespec.WarmTransfer{Destination: "billing_line"}
	pkg.Agent.Controls["to_human"] = human
	pipecatTarget := pkg.Targets["pipecat"]
	pkg.Targets = map[string]packagespec.Target{"pipecat": pipecatTarget}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	report, _ := Validate(agent, []Target{agent.Targets["pipecat"]}, targetcap.Default())
	joined := strings.Join(report.PerTarget[0].Errors, "\n")
	if !strings.Contains(joined, "does not emit warm transfer yet") || !strings.Contains(joined, "(livekit, sip)") {
		t.Fatalf("warm on pipecat daily must fail naming (livekit, sip), got:\n%s", joined)
	}
	// N34 / FR-032: the refusal must not claim the platform cannot do it. Daily
	// documents warm; this driver has not built it. Saying the first when you mean
	// the second sends an author looking for a different platform.
	if strings.Contains(joined, "no native warm transfer") || strings.Contains(joined, "Pipecat has no warm") {
		t.Errorf("the refusal states a platform limitation Daily's own docs contradict:\n%s", joined)
	}

	// Telephony route (carrier-websocket): the route table names the fix too.
	pkg = loadSafeCore(t)
	enableTelephony(pkg)
	outbound := true
	phone := pkg.Agent.Channels["phone"]
	phone.Outbound = &outbound
	pkg.Agent.Channels["phone"] = phone
	human = pkg.Agent.Controls["to_human"]
	human.Cold, human.Warm = nil, &packagespec.WarmTransfer{Destination: "billing_line"}
	pkg.Agent.Controls["to_human"] = human
	routeTarget(pkg, "pipecat", "primary_phone", "carrier-websocket", "twilio")
	agent, err = Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	report, _ = Validate(agent, []Target{agent.Targets["pipecat"]}, targetcap.Default())
	joined = strings.Join(report.PerTarget[0].Errors, "\n")
	if !strings.Contains(joined, "warm transfer compiles on (livekit, sip) trunks") {
		t.Fatalf("warm on the carrier route must name the supported routes, got:\n%s", joined)
	}

	// T016b / spec FR-006: the Daily carrier leg changes nothing about warm. The
	// route grants no warm feature, so the refusal must still arrive, and it must
	// still say which thing it means.
	pkg = dailyCarrierPackage(t)
	human = pkg.Agent.Controls["to_human"]
	human.Cold, human.Warm = nil, &packagespec.WarmTransfer{Destination: "billing_line"}
	pkg.Agent.Controls["to_human"] = human
	phone = pkg.Agent.Channels["phone"]
	phone.Outbound = &outbound
	pkg.Agent.Channels["phone"] = phone
	agent, err = Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	report, _ = Validate(agent, []Target{agent.Targets["pipecat"]}, targetcap.Default())
	joined = strings.Join(report.PerTarget[0].Errors, "\n")
	if !strings.Contains(joined, "does not emit warm transfer yet") || !strings.Contains(joined, "(livekit, sip)") {
		t.Fatalf("warm on the Daily carrier leg must still fail naming (livekit, sip), got:\n%s", joined)
	}
	if strings.Contains(joined, "no native warm transfer") || strings.Contains(joined, "Pipecat has no warm") {
		t.Errorf("the refusal states a platform limitation Daily's own docs contradict:\n%s", joined)
	}
}

// dailyCarrierPackage is safe_core on (pipecat, daily-sip, twilio): the Daily
// route with a carrier leg (SCHEMA N37), with the four Connection keys that
// route accepts and nothing else.
func dailyCarrierPackage(t *testing.T) *packagespec.Package {
	t.Helper()
	pkg := loadSafeCore(t)
	enableTelephony(pkg)
	target := pkg.Targets["pipecat"]
	target.Connection = "twilio_sip_daily"
	pkg.Targets = map[string]packagespec.Target{"pipecat": target}
	pkg.Connections = map[string]packagespec.Connection{"twilio_sip_daily": {
		Transport: "daily-sip", Carrier: "twilio", Environment: map[string]string{
			"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN",
			"sip_address": "SIP_TRUNK_HOSTNAME", "from_number": "SIP_FROM_NUMBER",
		},
	}}
	return pkg
}

// T016: the Daily carrier route validates Redis-free, and a plan that declares
// Redis anyway fails. Both halves matter: the second proves T010 relaxed the
// right branch, and the carrier-websocket check below proves it did not relax
// the wrong one.
func TestValidatePipecatDailyCarrierServiceSet(t *testing.T) {
	agent, err := Build(dailyCarrierPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	resolved := agent.Targets["pipecat"]
	report, _ := Validate(agent, []Target{resolved}, targetcap.Default())
	if errs := report.PerTarget[0].Errors; len(errs) != 0 {
		t.Fatalf("the Daily carrier route must validate cleanly, got:\n%s", strings.Join(errs, "\n"))
	}

	// A fabricated plan declaring redis on this route fails by name.
	withRedis := resolved
	plan := *resolved.Telephony
	plan.Services = []string{"application", "redis"}
	withRedis.Telephony = &plan
	report, _ = Validate(agent, []Target{withRedis}, targetcap.Default())
	joined := strings.Join(report.PerTarget[0].Errors, "\n")
	if !strings.Contains(joined, `telephony route declares unexpected service "redis"`) {
		t.Fatalf("a redis service on the Daily carrier route must fail by name, got:\n%s", joined)
	}

	// The carrier-websocket routes still require it.
	cwPkg := loadSafeCore(t)
	enableTelephony(cwPkg)
	routeTarget(cwPkg, "pipecat", "primary_phone", "carrier-websocket", "twilio")
	cwAgent, err := Build(cwPkg)
	if err != nil {
		t.Fatal(err)
	}
	cwTarget := cwAgent.Targets["pipecat"]
	cwPlan := *cwTarget.Telephony
	cwPlan.Services = []string{"application"}
	cwTarget.Telephony = &cwPlan
	report, _ = Validate(cwAgent, []Target{cwTarget}, targetcap.Default())
	if joined = strings.Join(report.PerTarget[0].Errors, "\n"); !strings.Contains(joined, "telephony service redis is required") {
		t.Fatalf("the carrier-websocket routes must still require redis, got:\n%s", joined)
	}
}

// T016a / FR-004, research D11: the route grants no telephony call sources,
// because the code that fills them is the carrier-websocket adapter this route
// does not emit. The refusal is the feature, and it has to name where the source
// does work, so an author learns the fix rather than only the no.
func TestValidatePipecatDailyCarrierRefusesCallSources(t *testing.T) {
	for _, source := range []VariableSource{
		VariableSourceFromNumber, VariableSourceToNumber,
		VariableSourceCallID, VariableSourceDirection,
	} {
		t.Run(string(source), func(t *testing.T) {
			pkg := dailyCarrierPackage(t)
			pkg.Agent.Variables["caller_fact"] = packagespec.Variable{Type: "string", Source: string(source)}
			agent, err := Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			report, _ := Validate(agent, []Target{agent.Targets["pipecat"]}, targetcap.Default())
			joined := strings.Join(report.PerTarget[0].Errors, "\n")
			if !strings.Contains(joined, "source."+string(source)) {
				t.Fatalf("a %s variable must fail by the source's name on this route, got:\n%s", source, joined)
			}
			if !strings.Contains(joined, "daily-sip") {
				t.Errorf("the refusal must name the route, got:\n%s", joined)
			}
			if !strings.Contains(joined, "carrier-websocket") {
				t.Errorf("the refusal must name where the source does work, got:\n%s", joined)
			}

			// The same declaration passes where the fill path exists.
			cwPkg := dailyCarrierPackage(t)
			cwPkg.Agent.Variables["caller_fact"] = packagespec.Variable{Type: "string", Source: string(source)}
			cw := cwPkg.Targets["pipecat"]
			cw.Connection = "primary_phone"
			cwPkg.Targets = map[string]packagespec.Target{"pipecat": cw}
			cwPkg.Connections = map[string]packagespec.Connection{"primary_phone": {
				Transport: "carrier-websocket", Carrier: "twilio", Environment: map[string]string{
					"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN",
					"from_number": "TWILIO_PHONE_NUMBER",
				},
			}}
			cwAgent, err := Build(cwPkg)
			if err != nil {
				t.Fatal(err)
			}
			report, _ = Validate(cwAgent, []Target{cwAgent.Targets["pipecat"]}, targetcap.Default())
			if joined = strings.Join(report.PerTarget[0].Errors, "\n"); strings.Contains(joined, "source."+string(source)) {
				t.Fatalf("the carrier-websocket route fills %s, so it must not be refused there:\n%s", source, joined)
			}
		})
	}
}

// deployment_region takes one region or several (N32). Several is LiveKit only:
// every other provider is gated in its own words, before any artifact exists.
func TestValidateDeploymentRegions(t *testing.T) { // N32
	for _, tc := range []struct {
		name     string
		provider Provider
		regions  []string
		want     string // "" means the package must validate cleanly
	}{
		{"one on pipecat", ProviderPipecat, []string{"us-west"}, ""},
		{"one on livekit", ProviderLiveKit, []string{"us-east"}, ""},
		{"several on livekit", ProviderLiveKit, []string{"us-east", "eu-central"}, ""},
		{"none", ProviderLiveKit, nil, ""},
		{"several on pipecat", ProviderPipecat, []string{"us-west", "us-east"}, "globally unique across regions"},
		{"several on vapi", ProviderVapi, []string{"us-west", "us-east"}, "Vapi has no per-region deployment"},
		{"duplicate", ProviderLiveKit, []string{"us-east", "us-east"}, `lists "us-east" twice`},
		{"empty entry", ProviderLiveKit, []string{"us-east", ""}, "empty entry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := safeAgent(t)
			target := targetFor(agent, tc.provider)
			target.DeploymentRegions = tc.regions
			report, err := Validate(agent, []Target{target}, targetcap.Default())
			text := strings.Join(report.PerTarget[0].Errors, "\n")
			if tc.want == "" {
				if err != nil {
					t.Fatalf("want a clean package, got %v: %q", err, text)
				}
				return
			}
			if err == nil {
				t.Fatalf("want an error mentioning %q, got a clean package", tc.want)
			}
			if !strings.Contains(text, tc.want) {
				t.Fatalf("missing %q in %q", tc.want, text)
			}
			if !strings.Contains(text, "deployment_region") && !strings.Contains(text, "region") {
				t.Fatalf("error does not name the field: %q", text)
			}
		})
	}
}

// A warm transfer dials the destination itself, and since 2026-08-12 (SCHEMA
// N33) it does that with the carrier's own SIP credentials passed inline. A
// LiveKit target with no telephony Connection therefore has nothing to dial
// with, and that is now a gated error naming the four values it needs.
//
// It used to validate green and then emit an agent that read
// LIVEKIT_SIP_OUTBOUND_TRUNK, a LiveKit-assigned id the package never mentioned
// and nobody was told to create: the exact defect this feature removed. Cold is
// unaffected, because it acts on the caller's existing leg and dials nobody.
func TestWarmTransferWithoutAConnectionIsGated(t *testing.T) {
	pkg := loadSafeCore(t)
	human := pkg.Agent.Controls["to_human"]
	human.Cold, human.Warm = nil, &packagespec.WarmTransfer{Destination: "billing_line"}
	pkg.Agent.Controls["to_human"] = human
	livekitTarget := pkg.Targets["livekit"]
	pkg.Targets = map[string]packagespec.Target{"livekit": livekitTarget}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	report, verr := Validate(agent, []Target{agent.Targets["livekit"]}, targetcap.Default())
	if verr == nil {
		t.Fatal("warm transfer with no telephony Connection must fail validation")
	}
	joined := strings.Join(report.PerTarget[0].Errors, "\n")
	for _, want := range []string{"warm transfer needs a telephony Connection", "sip_address", "sip_username", "sip_password", "from_number"} {
		if !strings.Contains(joined, want) {
			t.Errorf("error must name %q, got:\n%s", want, joined)
		}
	}
}

// The same fixture with a cold transfer still validates, on the same
// non-telephony target. Cold needs no trunk of either kind, so gating warm must
// not have caught it.
func TestColdTransferWithoutAConnectionStillValidates(t *testing.T) {
	pkg := loadSafeCore(t)
	livekitTarget := pkg.Targets["livekit"]
	pkg.Targets = map[string]packagespec.Target{"livekit": livekitTarget}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	report, verr := Validate(agent, []Target{agent.Targets["livekit"]}, targetcap.Default())
	if verr != nil {
		t.Fatalf("cold transfer on a non-telephony LiveKit target must still validate: %v\n%v", verr, report.PerTarget[0].Errors)
	}
}

// FR-006 and SC-008: the four SIP values are required by the route itself, so a
// Connection that omits one fails before any artifact is written. With the
// stored trunk gone this is fatal rather than a fallback, so it is pinned here
// rather than assumed.
func TestSIPRouteRequiresEveryConnectionValue(t *testing.T) {
	for _, missing := range []string{"sip_address", "sip_username", "sip_password", "from_number"} {
		pkg := loadSafeCore(t)
		enableTelephony(pkg)
		human := pkg.Agent.Controls["to_human"]
		human.Cold, human.Warm = nil, &packagespec.WarmTransfer{Destination: "billing_line"}
		pkg.Agent.Controls["to_human"] = human
		routeTarget(pkg, "livekit", "primary_phone", "sip", "twilio")
		connection := pkg.Connections["primary_phone"]
		connection.Environment = map[string]string{
			"sip_address": "SIP_TRUNK_HOSTNAME", "sip_username": "SIP_AUTH_USERNAME",
			"sip_password": "SIP_AUTH_PASSWORD", "from_number": "SIP_FROM_NUMBER",
		}
		delete(connection.Environment, missing)
		pkg.Connections["primary_phone"] = connection
		agent, err := Build(pkg)
		if err != nil {
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("omitting %s failed without naming it: %v", missing, err)
			}
			continue
		}
		report, verr := Validate(agent, []Target{agent.Targets["livekit"]}, targetcap.Default())
		if verr == nil {
			t.Errorf("omitting %s from the Connection must fail validation", missing)
			continue
		}
		if joined := strings.Join(report.PerTarget[0].Errors, "\n"); !strings.Contains(joined, missing) {
			t.Errorf("omitting %s must be named in the error, got:\n%s", missing, joined)
		}
	}
}

// cloudWebsocketPackage is safe_core on (pipecat, cloud-websocket, twilio) in the
// shape that needs a connection: a cold transfer, which places a call.
func cloudWebsocketPackage(t *testing.T) *packagespec.Package {
	t.Helper()
	pkg := loadSafeCore(t)
	enableTelephony(pkg)
	target := pkg.Targets["pipecat"]
	target.Connection = "twilio_voice"
	pkg.Targets = map[string]packagespec.Target{"pipecat": target}
	pkg.Connections = map[string]packagespec.Connection{"twilio_voice": {
		Transport: "cloud-websocket", Carrier: "twilio", Environment: map[string]string{
			"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN",
			"from_number": "TWILIO_PHONE_NUMBER",
		},
	}}
	return pkg
}

// The route where the operator hosts nothing has to validate with an **empty**
// process list, and that has to stay illegal everywhere else. Both halves are
// asserted, because a relaxed check that relaxed too far would look identical
// from the passing side (SCHEMA N38).
func TestValidatePipecatCloudWebsocketHostsNothing(t *testing.T) {
	agent, err := Build(cloudWebsocketPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	resolved := agent.Targets["pipecat"]
	if len(resolved.Telephony.Processes) != 0 {
		t.Fatalf("the plan declares %d process(es); this test no longer describes the route", len(resolved.Telephony.Processes))
	}
	report, _ := Validate(agent, []Target{resolved}, targetcap.Default())
	if errs := report.PerTarget[0].Errors; len(errs) != 0 {
		t.Fatalf("the Pipecat Cloud websocket route must validate cleanly, got:\n%s", strings.Join(errs, "\n"))
	}

	// A fabricated plan that gives this route a process contradicts the route, and
	// says so rather than passing quietly.
	withProcess := resolved
	plan := *resolved.Telephony
	plan.Processes = []TelephonyProcess{{Name: "agent", Command: []string{"uv", "run", "bot.py"}, Health: "/", Readiness: "/"}}
	withProcess.Telephony = &plan
	report, _ = Validate(agent, []Target{withProcess}, targetcap.Default())
	if joined := strings.Join(report.PerTarget[0].Errors, "\n"); !strings.Contains(joined, "runs no process of yours") {
		t.Fatalf("a process on this route must fail by name, got:\n%s", joined)
	}

	// An endpoint likewise: this route hosts none.
	withEndpoint := resolved
	plan = *resolved.Telephony
	plan.PublicEndpoints = []TelephonyEndpoint{{Name: "inbound", Method: "POST", Path: "/call"}}
	withEndpoint.Telephony = &plan
	report, _ = Validate(agent, []Target{withEndpoint}, targetcap.Default())
	if joined := strings.Join(report.PerTarget[0].Errors, "\n"); !strings.Contains(joined, "hosts no endpoint of yours") {
		t.Fatalf("an endpoint on this route must fail by name, got:\n%s", joined)
	}

	// Redis, and any coordination reason beyond admission, stay out.
	withRedis := resolved
	plan = *resolved.Telephony
	plan.Services = []string{"application", "redis"}
	withRedis.Telephony = &plan
	report, _ = Validate(agent, []Target{withRedis}, targetcap.Default())
	if joined := strings.Join(report.PerTarget[0].Errors, "\n"); !strings.Contains(joined, `unexpected service "redis"`) {
		t.Fatalf("a redis service on this route must fail by name, got:\n%s", joined)
	}
	withReason := resolved
	plan = *resolved.Telephony
	plan.CoordinationReasons = append(slices.Clone(plan.CoordinationReasons),
		TelephonyCoordinationReason{Name: "call_correlation", Consumers: []string{"application"}})
	withReason.Telephony = &plan
	report, _ = Validate(agent, []Target{withReason}, targetcap.Default())
	if joined := strings.Join(report.PerTarget[0].Errors, "\n"); !strings.Contains(joined, "coordinates only admission") {
		t.Fatalf("a second coordination reason on this route must fail, got:\n%s", joined)
	}

	// And the routes that do run something still require it: the relaxation is
	// keyed on this route, not on Pipecat.
	cwPkg := loadSafeCore(t)
	enableTelephony(cwPkg)
	routeTarget(cwPkg, "pipecat", "primary_phone", "carrier-websocket", "twilio")
	cwAgent, err := Build(cwPkg)
	if err != nil {
		t.Fatal(err)
	}
	cwTarget := cwAgent.Targets["pipecat"]
	cwPlan := *cwTarget.Telephony
	cwPlan.Processes = nil
	cwTarget.Telephony = &cwPlan
	report, _ = Validate(cwAgent, []Target{cwTarget}, targetcap.Default())
	if joined := strings.Join(report.PerTarget[0].Errors, "\n"); !strings.Contains(joined, "no runtime process") {
		t.Fatalf("a process-free carrier-websocket plan must still fail, got:\n%s", joined)
	}
}

// The route refuses telephony call sources by name, for the same reason the Daily
// carrier route does: the code that fills them is the carrier-websocket adapter
// neither route emits.
func TestValidatePipecatCloudWebsocketRefusesCallSources(t *testing.T) {
	pkg := cloudWebsocketPackage(t)
	pkg.Agent.Variables["caller_fact"] = packagespec.Variable{Type: "string", Source: string(VariableSourceFromNumber)}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	report, _ := Validate(agent, []Target{agent.Targets["pipecat"]}, targetcap.Default())
	joined := strings.Join(report.PerTarget[0].Errors, "\n")
	for _, want := range []string{"source.from_number", "cloud-websocket", "carrier-websocket"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the refusal is missing %q, got:\n%s", want, joined)
		}
	}
}
