package ir

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

func TestValidateSafeCorePerTarget(t *testing.T) { // V5, V18
	agent := safeAgent(t)
	report, err := Validate(agent, allTargets(agent), targetcap.Default())
	if err != nil {
		t.Fatalf("%v: %#v", err, report.PerTarget)
	}
	if len(report.PerTarget) != 2 {
		t.Fatalf("target rows = %d", len(report.PerTarget))
	}
	for _, provider := range []Provider{ProviderLiveKit, ProviderPipecat} {
		if row := reportFor(report, provider); len(row.Errors) != 0 {
			t.Fatalf("%s row has errors: %#v", provider, row)
		}
	}
}

func TestValidateAgentTransferAnnouncePerTarget(t *testing.T) {
	agent := safeAgent(t)
	agent.Controls["to_billing"].(*AgentTransfer).Announce = "I’ll connect you with billing."
	for provider, wantError := range map[Provider]bool{
		ProviderLiveKit: false,
		ProviderPipecat: false,
	} {
		report, err := Validate(agent, []Target{targetFor(agent, provider)}, targetcap.Default())
		if (err != nil) != wantError {
			t.Errorf("%s: err=%v report=%#v", provider, err, report.PerTarget)
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

func TestValidateTaskRequiresResultAndHistory(t *testing.T) {
	agent := safeAgent(t)
	agent.Tasks["incomplete"] = Task{Instructions: "incomplete"}
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err == nil {
		t.Fatal("task with no result or history must fail validation")
	}
	errors := strings.Join(report.PerTarget[0].Errors, "\n")
	for _, want := range []string{`task "incomplete" result must not be empty`, `"incomplete" context.history is required`} {
		if !strings.Contains(errors, want) {
			t.Errorf("validation errors missing %q:\n%s", want, errors)
		}
	}
}

// A task may attach a handoff, and nothing else. Only the legal half is still
// checkable here: a task definition has `tools:` and `handoffs:` and no other
// key, so a delegate or an escalation on a task is no longer a package Validate
// can be handed. The illegal halves moved to
// TestBuildRefusesAFieldFromAnotherKind, which proves the file is refused at
// decode with a line and a column.
func TestValidateTaskMayAttachAHandoff(t *testing.T) {
	agent := safeAgent(t)
	agent.Tasks["routing"] = Task{
		Instructions: "Route the caller.",
		Tools:        []string{"to_billing"},
		Result:       map[string]ResultField{"done": {Type: PrimitiveBoolean}},
		Context:      TaskContext{History: HistoryFull},
	}
	report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err != nil {
		t.Fatalf("handoff on a task rejected: %v %#v", err, report.PerTarget)
	}
}

// Every tracing provider is gated per target the same way, and a new provider
// must not arrive ungated on a target that cannot lower it.
func TestValidateTracingByTargetForEveryProvider(t *testing.T) { // V25
	for _, name := range TracingProviders {
		agent := safeAgent(t)
		agent.Tracing = &Tracing{Provider: name}
		for provider, wantError := range map[Provider]bool{
			ProviderLiveKit: false, ProviderPipecat: false,
		} {
			report, err := Validate(agent, []Target{targetFor(agent, provider)}, targetcap.Default())
			if (err != nil) != wantError {
				t.Errorf("%s/%s: err=%v report=%#v", name, provider, err, report.PerTarget)
			}
		}
	}
}

// An unknown provider fails before generation rather than silently tracing
// nothing.
func TestValidateRejectsAnUnknownTracingProvider(t *testing.T) {
	agent := safeAgent(t)
	agent.Tracing = &Tracing{Provider: "not-a-provider"}
	if _, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default()); err == nil {
		t.Fatal("an unknown tracing provider validated")
	}
}

func TestValidateLangfuseTracingByTarget(t *testing.T) { // V25
	agent := safeAgent(t)
	agent.Tracing = &Tracing{Provider: "langfuse"}
	for provider, wantError := range map[Provider]bool{
		ProviderLiveKit: false, ProviderPipecat: false,
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

func TestValidateTurnModelRejectsSpeakAndThinkFields(t *testing.T) { // V22
	agent := safeAgent(t)
	def := agent.Models["vad"]
	def.Voice = "not-a-turn-field"
	temperature := 0.4
	def.Temperature = &temperature
	agent.Models["vad"] = def

	report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "turn model; voice, speed, and sampling fields do not apply") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

// N16: a language set on a vendor whose integration has no language slot must
// fail at VALIDATE, not just generate (C6: gate before any artifact). gemini
// warm_standby_enabled is real on livekit and inert on pipecat: pipecat-slng
// 0.5.0 deferred it, so the kwarg is absorbed by **kwargs and discarded with no
// error. A params: block is forwarded verbatim by design, so nothing else in the
// compiler can notice that the package is promising a held-open socket and
// getting none. A warning rather than a refusal, because a package that ships to
// both targets authors one speak binding for both.
func TestValidateWarmStandbyIsPipecatInert(t *testing.T) {
	withStandby := func(t *testing.T, provider Provider) []string {
		t.Helper()
		agent := safeAgent(t)
		target := targetFor(agent, provider)
		b := target.Models.Speak["front_desk"]
		b.Params = map[string]any{"warm_standby_enabled": true}
		target.Models.Speak["front_desk"] = b
		report, err := Validate(agent, []Target{target}, targetcap.Default())
		if err != nil {
			t.Fatalf("an inert param must warn, never fail: %v %#v", err, report.PerTarget[0].Errors)
		}
		return report.PerTarget[0].Warnings
	}

	warnings := strings.Join(withStandby(t, ProviderPipecat), "\n")
	if !strings.Contains(warnings, "warm_standby_enabled") {
		t.Errorf("pipecat did not warn that the param reaches nothing:\n%s", warnings)
	}
	// The warning has to say what to do instead, or it is just noise.
	if !strings.Contains(warnings, "livekit") {
		t.Errorf("the warning does not name the target where the param works:\n%s", warnings)
	}

	// On livekit the plugin implements it, so there is nothing to say.
	if got := strings.Join(withStandby(t, ProviderLiveKit), "\n"); strings.Contains(got, "warm_standby_enabled") {
		t.Errorf("warned about a param that works on this target:\n%s", got)
	}

	// And an unset param is silent everywhere.
	quiet := safeAgent(t)
	report, _ := Validate(quiet, []Target{targetFor(quiet, ProviderPipecat)}, targetcap.Default())
	if got := strings.Join(report.PerTarget[0].Warnings, "\n"); strings.Contains(got, "warm_standby_enabled") {
		t.Errorf("warned with no param authored:\n%s", got)
	}
}

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

// V4/V11's "a gate speaks the failing target's vocabulary" used to be shown
// here with Vapi's thinking-audio and Squad-return gates. Both belonged to a
// retired target. The property still holds and is covered wherever a live
// driver gates something — see TestValidateBuiltinUnknownIDRejected and the
// per-target rows in TestCompilerGolden.

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

// The V6 provisional-tag behaviour used to be exercised here through a
// synthetic capability field (`future.provisional`) that existed only so this
// test could reach the tag. The field is gone; the behaviour it stood in for is
// covered end to end by TestValidateTelephonyProvisionalRouteIsUsableAndQuiet
// below, against a real route. Do not reintroduce a synthetic field to "restore
// coverage" that was never missing.

// A history value its target cannot shape is refused, and the refusal names
// what that target does shape (V8).
//
// `summary` rather than `messages`, because Pipecat shapes `messages` now. The
// pair is the whole of the check: the value refused, and the value accepted
// next to it, so a table that refuses everything would not pass either.
func TestValidateContextPolicy(t *testing.T) { // V8
	agent := safeAgent(t)
	transfer := agent.Controls["to_billing"].(*AgentTransfer)
	transfer.Context.History = HistorySummary
	report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "does not summarize a context yet") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
	transfer.Context.History = HistoryMessages
	report, err = Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err != nil {
		t.Fatalf("history: messages is shaped on pipecat and must validate: err=%v report=%#v", err, report.PerTarget)
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
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "capacity.peak_starts_per_second must be positive for telephony") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

// V13's outbound-variable-satisfiability check used to run here against the
// deepgram target. It needed a target that is neither LiveKit nor Pipecat,
// because validateChannels errors and skips the rest of the channel when either
// of those has a telephony channel with no resolved Connection plan. With both
// managed targets retired there is no such target, so the scenario cannot be
// constructed and the branch it exercised is now unreachable for a telephony
// channel. Removing that branch is a separate change from retiring a provider.

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

	for _, provider := range []Provider{ProviderLiveKit, ProviderPipecat} {
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

// A misspelled protect entry would otherwise lower to no strategy at all: the
// author asks for a protected greeting, compile stays green, and the agent
// interrupts itself on the first phone call.
func TestValidateRejectsUnknownInterruptionProtect(t *testing.T) {
	agent := safeAgent(t)
	agent.Conversation.Interruption.Protect = []InterruptionProtect{"greetings"}
	report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), `protect has unknown entry "greetings"`) {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
}

// enabled: false already mutes the whole call, so a protect list beside it is a
// contradiction. Honouring one and dropping the other silently is the worse
// outcome either way, so it is refused.
func TestValidateRejectsInterruptionProtectWhenDisabled(t *testing.T) {
	agent := safeAgent(t)
	off := false
	agent.Conversation.Interruption.Enabled = &off
	agent.Conversation.Interruption.Protect = []InterruptionProtect{ProtectGreeting}
	report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "protect has no meaning when enabled is false") {
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
	phone := pkg.Agent.Channels["phone"]
	phone.RequiredControls = []string{"hangup"}
	pkg.Agent.Channels["phone"] = phone
	routeTarget(pkg, "pipecat", "primary_phone", "cloud-websocket", "twilio")
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
	routeTarget(pkg, "pipecat", "primary_phone", "cloud-websocket", "twilio")
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
	routeTarget(pkg, "pipecat", "primary_phone", "cloud-websocket", "twilio")
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
	routeTarget(pkg, "pipecat", "primary_phone", "cloud-websocket", "twilio")
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

// The two Pipecat routes that deployed to no managed platform are gone, and a
// package that names one is refused before generation (spec FR-008).
//
// No new refusal was written for this. validateRoute already refuses anything
// absent from the selectable route table and lists what the provider does have,
// so deleting the rows is the whole change. What this test holds is that the
// refusal keeps *naming the transport* and keeps *suggesting only routes that
// deploy*: a suggestion list that offered one of these back would send an author
// to a route they cannot run anywhere.
func TestValidateRefusesTheRoutesThatDeployNowhere(t *testing.T) {
	for _, transport := range []string{"carrier-websocket", "sip"} {
		t.Run(transport, func(t *testing.T) {
			pkg := loadSafeCore(t)
			enableTelephony(pkg)
			routeTarget(pkg, "pipecat", "primary_phone", transport, "twilio")
			_, err := Build(pkg)
			if err == nil {
				t.Fatalf("(pipecat, %s, twilio) compiled; it deploys to no managed platform and has no local loop", transport)
			}
			for _, want := range []string{transport, "pipecat supports:"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal is missing %q: %v", want, err)
				}
			}
			// The surviving Pipecat phone routes, and nothing else, may be offered.
			// Matched on the whole suggestion list rather than by substring: the
			// surviving `daily-sip` ends in "sip", so a naive check for "sip with"
			// reports a route that is fine.
			_, suggestions, ok := strings.Cut(err.Error(), "pipecat supports: ")
			if !ok {
				t.Fatalf("the refusal carries no suggestion list: %v", err)
			}
			if got := strings.TrimSuffix(suggestions, "."); got != "cloud-websocket with twilio; daily-sip with twilio" {
				t.Errorf("suggestions = %q; only the routes that deploy to Pipecat Cloud may be offered", got)
			}
		})
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

	// A "gated on Vapi, in Vapi's words" subtest stood here. Webhook auth is
	// supported on both remaining drivers, so there is no denial to word.
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

// Builtin tools were gated on Vapi, in Vapi's words. That target is retired and
// both remaining drivers support builtins, so there is no denial left to word.

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

// The "every configured target is checked, not just the one being validated"
// property used to be shown by a nested result gated on the vapi instance in
// the fixture. TestValidateNestedResultRejectsUnknownConfiguredProvider below
// shows the same property with a provider name that cannot go stale.

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
		ProviderPipecat: "does not emit listen fallback yet",
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
	target := targetFor(agent, ProviderLiveKit)
	binding := target.Models.Reason["careful_reasoning"]
	binding.Provider = "anthropic"
	target.Models.Reason["careful_reasoning"] = binding

	// The gate is a property of the slot kind, not of a provider name. It used
	// to be shown on a target whose configured slot was same-provider; both such
	// targets are retired, so the slot is set here instead, which is what the
	// test was always about.
	caps := targetcap.Default()
	caps.FallbackSlots[targetcap.Provider(target.Provider)] = targetcap.FallbackSameProvider
	report, err := Validate(agent, []Target{target}, caps)
	if err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "stay within one provider") {
		t.Fatalf("err=%v report=%#v", err, report.PerTarget)
	}
	caps.FallbackSlots[targetcap.Provider(target.Provider)] = targetcap.FallbackProvider
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
	// 2 targets x (listen + turn + 2 speak + 2 reason): the package-wide turn
	// preference reaches every target (warned, not dropped).
	if len(report.ForwardedBindings) != 12 {
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
	// 2 targets x (workers + gpus + realtime_audio concurrency + session-time) = 8.
	if len(report.Sizing) != 8 {
		t.Fatalf("sizing lines = %d", len(report.Sizing))
	}
	for _, line := range report.Sizing {
		if line.Status != "unbenchmarked" || !strings.Contains(line.Basis, "channels=realtime_audio") {
			t.Fatalf("sizing line = %#v", line)
		}
	}
	// Sizing is reported alongside errors, not instead of them. A bare telephony
	// channel with no resolved Connection plan is itself an error on every
	// remaining target, so the error is expected here; what is under test is
	// that the sizing lines still switch to the telephony basis.
	agent.Channels = map[string]Channel{"phone": testTelephonyChannel()}
	report, _ = Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if len(report.Sizing) != 5 {
		t.Fatalf("telephony-only sizing lines=%#v", report.Sizing)
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
		// summary, because it is the last history value Pipecat refuses: the
		// driver shapes messages, last_n and reset now.
		{"transfer_history", func(a *Agent) {
			a.Controls["to_billing"].(*AgentTransfer).Context.History = HistorySummary
		}, "does not summarize a context yet"},
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

// TestValueChecksFailAtValidate walks all eight former generator-only value checks. Each one
// let a package exit 0 from validate and 1 from compile, with a bare message
// carrying no target prefix and no position, after validate had already said the
// package was fine. The generators keep their own errors as a backstop; what
// moves is the fact each one checks against (research D5).
func TestValueChecksFailAtValidate(t *testing.T) {
	for _, test := range []struct {
		name     string
		provider string
		mutate   func(*packagespec.Package)
		want     string
	}{
		{
			name:     "turn detector model",
			provider: "livekit",
			mutate: func(pkg *packagespec.Package) {
				override := pkg.Targets["livekit"].Models["vad"]
				override.Model = "silero"
				pkg.Targets["livekit"].Models["vad"] = override
			},
			want: `turn model "silero" is not recognized; use turn-detector-mini (local) or turn-detector (LiveKit Cloud)`,
		},
		{
			name:     "sdk_language",
			provider: "livekit",
			mutate: func(pkg *packagespec.Package) {
				setTargetField(pkg, "livekit", func(t *packagespec.Target) { t.SDKLanguage = "node" })
			},
			want: `livekit driver emits python projects only; sdk_language "node" has no templates yet`,
		},
		{
			name:     "pin key",
			provider: "livekit",
			mutate: func(pkg *packagespec.Package) {
				setTargetField(pkg, "livekit", func(t *packagespec.Target) {
					t.Pins = map[string]string{"livekit-plugins-banana": "1.6.1"}
				})
			},
			want: `livekit pin "livekit-plugins-banana" is not a pinnable package; known: `,
		},
		{
			name:     "pin value is not a version",
			provider: "livekit",
			mutate: func(pkg *packagespec.Package) {
				setTargetField(pkg, "livekit", func(t *packagespec.Target) {
					t.Pins = map[string]string{"livekit-plugins-silero": "banana"}
				})
			},
			want: `livekit pin livekit-plugins-silero: "banana" is not a semantic version`,
		},
		{
			name:     "pin value is below the floor",
			provider: "livekit",
			mutate: func(pkg *packagespec.Package) {
				setTargetField(pkg, "livekit", func(t *packagespec.Target) {
					t.Pins = map[string]string{"livekit-plugins-silero": "0.0.1"}
				})
			},
			want: `livekit pin livekit-plugins-silero "0.0.1" is below the catalogue floor >=1.6.1`,
		},
		{
			name:     "version is not a version, livekit",
			provider: "livekit",
			mutate: func(pkg *packagespec.Package) {
				setTargetField(pkg, "livekit", func(t *packagespec.Target) { t.Version = "banana" })
			},
			want: `livekit version "banana" is not a semantic version`,
		},
		{
			name:     "version is above the supported ceiling, pipecat",
			provider: "pipecat",
			mutate: func(pkg *packagespec.Package) {
				setTargetField(pkg, "pipecat", func(t *packagespec.Target) { t.Version = "9.9.9" })
			},
			want: `pipecat version "9.9.9" is newer than this unmute supports (exactly 1.8.0); a newer unmute may support it`,
		},
		{
			name:     "version is below the exact supported version, pipecat",
			provider: "pipecat",
			mutate: func(pkg *packagespec.Package) {
				setTargetField(pkg, "pipecat", func(t *packagespec.Target) { t.Version = "1.6.9" })
			},
			want: `pipecat version "1.6.9" is outside the supported range (exactly 1.8.0)`,
		},
		{
			name:     "version names only two parts, livekit",
			provider: "livekit",
			mutate: func(pkg *packagespec.Package) {
				setTargetField(pkg, "livekit", func(t *packagespec.Target) { t.Version = "1.6" })
			},
			want: `livekit version "1.6" must be three numbers, for example "1.6.10"`,
		},
		{
			// The sharpest of the eight: `voice` is required on every speak entry,
			// so authoring a deepgram speak model
			// necessarily produced a package that validated green and could not
			// compile.
			name:     "speak voice with no slot",
			provider: "livekit",
			mutate: func(pkg *packagespec.Package) {
				override := pkg.Targets["livekit"].Models["front_desk"]
				override.Provider, override.Model, override.Voice = "deepgram", "aura-2-thalia-en", "aura-2-thalia-en"
				pkg.Targets["livekit"].Models["front_desk"] = override
			},
			want: `livekit speak binding provider "deepgram": voice has no slot here`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			pkg := loadSafeCore(t)
			only := pkg.Targets[test.provider]
			pkg.Targets = map[string]packagespec.Target{test.provider: only}
			test.mutate(pkg)
			agent, err := Build(pkg)
			if err != nil {
				t.Fatalf("this is a value check, not a shape check: Build must still succeed: %v", err)
			}
			report, verr := Validate(agent, []Target{agent.Targets[test.provider]}, targetcap.Default())
			if verr == nil {
				t.Fatalf("validate exited 0; compile then fails on %q, after validate said the package was fine", test.want)
			}
			joined := strings.Join(report.PerTarget[0].Errors, "\n")
			if !strings.Contains(joined, test.want) {
				t.Fatalf("errors =\n%s\nwant one containing %q", joined, test.want)
			}
		})
	}
}

// setTargetField edits one target in place. packagespec.Target is a value in a
// map, so a field write needs the read-modify-write these cases would otherwise
// each repeat.
func setTargetField(pkg *packagespec.Package, name string, set func(*packagespec.Target)) {
	target := pkg.Targets[name]
	set(&target)
	pkg.Targets[name] = target
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
		addColdHumanTransfer(pkg)
		enableTelephony(pkg)
		phone := pkg.Agent.Channels["phone"]
		phone.Outbound = &outbound
		pkg.Agent.Channels["phone"] = phone
		// The shipped cold transfer, made warm: same tool wiring, same
		// destination, only the shape block differs.
		human := pkg.Agent.Escalations["to_human"]
		human.Cold, human.Warm = nil, &packagespec.WarmTransfer{Destination: "billing_line"}
		pkg.Agent.Escalations["to_human"] = human
		routeTarget(pkg, "pipecat", "primary_phone", "cloud-websocket", "twilio")
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
	// Non-telephony Pipecat target: the control row.
	pkg := loadSafeCore(t)
	addColdHumanTransfer(pkg)
	human := pkg.Agent.Escalations["to_human"]
	human.Cold, human.Warm = nil, &packagespec.WarmTransfer{Destination: "billing_line"}
	pkg.Agent.Escalations["to_human"] = human
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
	addColdHumanTransfer(pkg)
	enableTelephony(pkg)
	outbound := true
	phone := pkg.Agent.Channels["phone"]
	phone.Outbound = &outbound
	pkg.Agent.Channels["phone"] = phone
	human = pkg.Agent.Escalations["to_human"]
	human.Cold, human.Warm = nil, &packagespec.WarmTransfer{Destination: "billing_line"}
	pkg.Agent.Escalations["to_human"] = human
	routeTarget(pkg, "pipecat", "primary_phone", "cloud-websocket", "twilio")
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
	human = pkg.Agent.Escalations["send_to_billing"]
	human.Cold, human.Warm = nil, &packagespec.WarmTransfer{Destination: "billing_line"}
	pkg.Agent.Escalations["send_to_billing"] = human
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

func dailyCarrierPackage(t *testing.T) *packagespec.Package {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "daily_carrier"))
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestBuildRejectsCarrierlessPipecatDaily(t *testing.T) {
	pkg := loadSafeCore(t)
	addColdHumanTransfer(pkg)
	pipecat := pkg.Targets["pipecat"]
	pipecat.Connection = "daily_provisioned"
	pkg.Targets = map[string]packagespec.Target{"pipecat": pipecat}
	pkg.Connections = map[string]packagespec.Connection{
		"daily_provisioned": {Transport: "daily-sip"},
	}
	_, err := Build(pkg)
	if err == nil {
		t.Fatal("carrierless Daily remained an accepted authoring route")
	}
	for _, want := range []string{"connections/daily_provisioned.yaml", `transport "daily-sip" declares no carrier`, "daily-sip with twilio"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("build error missing %q: %v", want, err)
		}
	}
}

func TestValidatePipecatDailyColdTransferNeedsPhoneLeg(t *testing.T) {
	pkg := loadSafeCore(t)
	addColdHumanTransfer(pkg)
	pipecat := pkg.Targets["pipecat"]
	pipecat.Connection = "twilio_sip_daily"
	pkg.Targets = map[string]packagespec.Target{"pipecat": pipecat}
	pkg.Connections = map[string]packagespec.Connection{"twilio_sip_daily": {
		Transport: "daily-sip", Carrier: "twilio", Environment: map[string]string{
			"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN",
			"sip_address": "SIP_TRUNK_HOSTNAME", "from_number": "SIP_FROM_NUMBER",
		},
	}}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}

	report, err := Validate(agent, []Target{agent.Targets["pipecat"]}, targetcap.Default())
	if err == nil {
		t.Fatal("web-only Daily accepted a cold transfer without an existing SIP phone leg")
	}
	joined := strings.Join(report.PerTarget[0].Errors, "\n")
	for _, want := range []string{"channels.phone", "sessionId"} {
		if !strings.Contains(joined, want) {
			t.Errorf("validation error missing %q:\n%s", want, joined)
		}
	}
}

func TestValidatePipecatDailyColdTransferNeedsActivePhoneDirection(t *testing.T) {
	pkg := dailyCarrierPackage(t)
	disabled := false
	phone := pkg.Agent.Channels["phone"]
	phone.Inbound, phone.Outbound = &disabled, &disabled
	pkg.Agent.Channels["phone"] = phone
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}

	report, err := Validate(agent, []Target{agent.Targets["pipecat"]}, targetcap.Default())
	if err == nil {
		t.Fatal("cold transfer accepted a phone channel that can neither receive nor place a call")
	}
	if joined := strings.Join(report.PerTarget[0].Errors, "\n"); !strings.Contains(joined, "enable inbound or outbound") {
		t.Fatalf("validation error did not name the missing phone direction:\n%s", joined)
	}
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

	// A route that does require it still does. The contrast used to be a Pipecat
	// carrier-websocket target; that route is gone and no surviving Pipecat route
	// runs a coordination store, so the contrast is LiveKit sip, which does.
	cwPkg := loadSafeCore(t)
	enableTelephony(cwPkg)
	routeTarget(cwPkg, "livekit", "primary_phone", "sip", "twilio")
	cwPkg.Connections["primary_phone"] = packagespec.Connection{
		Transport: "sip", Carrier: "twilio", Environment: map[string]string{
			"sip_address": "TWILIO_SIP_ADDRESS", "sip_username": "TWILIO_SIP_USERNAME",
			"sip_password": "TWILIO_SIP_PASSWORD", "from_number": "TWILIO_PHONE_NUMBER",
		},
	}
	cwAgent, err := Build(cwPkg)
	if err != nil {
		t.Fatal(err)
	}
	cwTarget := cwAgent.Targets["livekit"]
	cwPlan := *cwTarget.Telephony
	cwPlan.Services = []string{"application"}
	cwTarget.Telephony = &cwPlan
	report, _ = Validate(cwAgent, []Target{cwTarget}, targetcap.Default())
	if joined = strings.Join(report.PerTarget[0].Errors, "\n"); !strings.Contains(joined, "telephony service redis is required") {
		t.Fatalf("a route that runs a coordination store must still require redis, got:\n%s", joined)
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
			pkg.Agent.Variables = map[string]packagespec.Variable{}
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
			// Where it does work. No Pipecat route fills call sources, so the
			// only useful answer is a LiveKit one, and the message has to give it
			// rather than leaving the author to go looking.
			if !strings.Contains(joined, "livekit, connector") {
				t.Errorf("the refusal must name where the source does work, got:\n%s", joined)
			}

			// The same declaration passes where the fill path exists. That used to
			// be the Pipecat carrier-websocket adapter. With that route gone, no
			// Pipecat route fills call sources at all, so the contrast is the
			// LiveKit Twilio connector: the surviving route whose bridge reads the
			// carrier's stream and writes these attributes.
			cwPkg := loadSafeCore(t)
			enableTelephony(cwPkg)
			cwPkg.Agent.Variables = map[string]packagespec.Variable{}
			cwPkg.Agent.Variables["caller_fact"] = packagespec.Variable{Type: "string", Source: string(source)}
			routeTarget(cwPkg, "livekit", "primary_phone", "connector", "twilio")
			cwPkg.Connections["primary_phone"] = packagespec.Connection{
				Transport: "connector", Carrier: "twilio", Environment: map[string]string{
					"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN",
					"from_number": "TWILIO_PHONE_NUMBER",
				},
			}
			cwAgent, err := Build(cwPkg)
			if err != nil {
				t.Fatal(err)
			}
			report, _ = Validate(cwAgent, []Target{cwAgent.Targets["livekit"]}, targetcap.Default())
			if joined = strings.Join(report.PerTarget[0].Errors, "\n"); strings.Contains(joined, "source."+string(source)) {
				t.Fatalf("the connector route fills %s, so it must not be refused there:\n%s", source, joined)
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
	addColdHumanTransfer(pkg)
	human := pkg.Agent.Escalations["to_human"]
	human.Cold, human.Warm = nil, &packagespec.WarmTransfer{Destination: "billing_line"}
	pkg.Agent.Escalations["to_human"] = human
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

// TestColdTransferNeedsARoute is the cold sibling of the warm gate above, and
// it replaces a test that asserted the opposite. Cold does not dial, so it needs
// no SIP credentials — but it does need a **leg**, and a session that never
// arrived by phone has none. LiveKit used to compile the transfer anyway and
// then explain in a generated comment why it could not work (reproduction.md B).
//
// Pipecat already refused, in its own vocabulary. That must not change, and the
// generic message must never append on top of it: one error, one voice.
func TestColdTransferNeedsARoute(t *testing.T) {
	browserOnly := func(t *testing.T, provider string) (*Agent, Target) {
		t.Helper()
		pkg := loadSafeCore(t)
		addColdHumanTransfer(pkg)
		only := pkg.Targets[provider]
		only.Connection = "" // browser only: no route of any kind
		pkg.Targets = map[string]packagespec.Target{provider: only}
		agent, err := Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		return agent, agent.Targets[provider]
	}

	t.Run("livekit refuses as the sibling of the warm message", func(t *testing.T) {
		agent, resolved := browserOnly(t, "livekit")
		report, verr := Validate(agent, []Target{resolved}, targetcap.Default())
		if verr == nil {
			t.Fatal("a browser-only package must not compile a transfer that hands over a phone leg it never had")
		}
		joined := strings.Join(report.PerTarget[0].Errors, "\n")
		for _, want := range []string{
			"cold transfer needs a telephony Connection",
			"it hands the caller's own phone leg to the destination",
			"has no leg to hand over",
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("error must say %q, got:\n%s", want, joined)
			}
		}
	})

	t.Run("pipecat keeps its own words, alone", func(t *testing.T) {
		agent, resolved := browserOnly(t, "pipecat")
		report, verr := Validate(agent, []Target{resolved}, targetcap.Default())
		if verr == nil {
			t.Fatal("pipecat already refused this package; it must keep refusing")
		}
		errs := report.PerTarget[0].Errors
		if len(errs) != 1 {
			t.Fatalf("want exactly one error in the provider's own vocabulary, got %d:\n%s", len(errs), strings.Join(errs, "\n"))
		}
		if !strings.Contains(errs[0], "Pipecat cold transfer requires an active channels.phone Connection") {
			t.Errorf("error = %q, want Pipecat's own wording", errs[0])
		}
	})
}

// FR-006 and SC-008: the four SIP values are required by the route itself, so a
// Connection that omits one fails before any artifact is written. With the
// stored trunk gone this is fatal rather than a fallback, so it is pinned here
// rather than assumed.
func TestSIPRouteRequiresEveryConnectionValue(t *testing.T) {
	for _, missing := range []string{"sip_address", "sip_username", "sip_password", "from_number"} {
		pkg := loadSafeCore(t)
		addColdHumanTransfer(pkg)
		enableTelephony(pkg)
		human := pkg.Agent.Escalations["to_human"]
		human.Cold, human.Warm = nil, &packagespec.WarmTransfer{Destination: "billing_line"}
		pkg.Agent.Escalations["to_human"] = human
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
	addColdHumanTransfer(pkg)
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
	control := pkg.Agent.Escalations["to_human"]
	control.Cold.OnUnavailable = string(OnUnavailableHangup)
	pkg.Agent.Escalations["to_human"] = control
	return pkg
}

func TestValidatePipecatCloudWebsocketRequiresHangupOnUnavailable(t *testing.T) {
	for _, test := range []struct {
		name        string
		policy      string
		wantFailure bool
	}{
		{name: "omitted default", wantFailure: true},
		{name: "explicit return", policy: string(OnUnavailableReturn), wantFailure: true},
		{name: "explicit hangup", policy: string(OnUnavailableHangup)},
	} {
		t.Run(test.name, func(t *testing.T) {
			pkg := cloudWebsocketPackage(t)
			control := pkg.Agent.Escalations["to_human"]
			control.Cold.OnUnavailable = test.policy
			pkg.Agent.Escalations["to_human"] = control
			agent, err := Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			human := agent.Controls["to_human"].(*HumanTransfer)
			if test.policy == "" && human.OnUnavailable != OnUnavailableReturn {
				t.Fatalf("omitted policy resolved to %q, want %q", human.OnUnavailable, OnUnavailableReturn)
			}
			report, err := Validate(agent, []Target{agent.Targets["pipecat"]}, targetcap.Default())
			if !test.wantFailure {
				if err != nil {
					t.Fatalf("hangup policy was rejected: %v\n%s", err, strings.Join(report.PerTarget[0].Errors, "\n"))
				}
				return
			}
			if err == nil {
				t.Fatal("return_to_caller passed validation on a route that cannot reconnect the caller")
			}
			joined := strings.Join(report.PerTarget[0].Errors, "\n")
			for _, want := range []string{"return_to_caller", "cannot reconnect the original media stream", "on_unavailable: hangup"} {
				if !strings.Contains(joined, want) {
					t.Errorf("validation error missing %q:\n%s", want, joined)
				}
			}
		})
	}
}

func TestValidatePipecatCloudWebsocketTransferNeedsPhoneSession(t *testing.T) {
	pkg := cloudWebsocketPackage(t)
	delete(pkg.Agent.Channels, "phone")
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	resolved := agent.Targets["pipecat"]
	if resolved.Telephony != nil {
		t.Fatal("web-only fixture unexpectedly has a telephony plan")
	}
	report, err := Validate(agent, []Target{resolved}, targetcap.Default())
	if err == nil {
		t.Fatal("web-only cloud-websocket transfer passed without a live carrier call")
	}
	joined := strings.Join(report.PerTarget[0].Errors, "\n")
	for _, want := range []string{"cloud-websocket", "CallSid", "media stream", "channels.phone"} {
		if !strings.Contains(joined, want) {
			t.Errorf("validation error missing %q:\n%s", want, joined)
		}
	}
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
	// The other direction: on a route that does host a process, an empty process
	// list is still an error. The contrast used to be carrier-websocket and is now
	// LiveKit sip, the surviving route that runs the operator's own container.
	cwPkg := loadSafeCore(t)
	enableTelephony(cwPkg)
	routeTarget(cwPkg, "livekit", "primary_phone", "sip", "twilio")
	cwPkg.Connections["primary_phone"] = packagespec.Connection{
		Transport: "sip", Carrier: "twilio", Environment: map[string]string{
			"sip_address": "TWILIO_SIP_ADDRESS", "sip_username": "TWILIO_SIP_USERNAME",
			"sip_password": "TWILIO_SIP_PASSWORD", "from_number": "TWILIO_PHONE_NUMBER",
		},
	}
	cwAgent, err := Build(cwPkg)
	if err != nil {
		t.Fatal(err)
	}
	cwTarget := cwAgent.Targets["livekit"]
	cwPlan := *cwTarget.Telephony
	cwPlan.Processes = nil
	cwTarget.Telephony = &cwPlan
	report, _ = Validate(cwAgent, []Target{cwTarget}, targetcap.Default())
	if joined := strings.Join(report.PerTarget[0].Errors, "\n"); !strings.Contains(joined, "no runtime process") {
		t.Fatalf("a process-free plan on a route that hosts one must still fail, got:\n%s", joined)
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
	for _, want := range []string{"source.from_number", "cloud-websocket", "livekit, connector"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the refusal is missing %q, got:\n%s", want, joined)
		}
	}
}

// TestValidateToolAnnounceLegalExecutionOnly: the line needs a body to speak
// before, so it follows inject's rule. Every other execution kind is refused by
// tool name, and a webhook, local, knowledge or slng tool passes (FR-002,
// FR-029 for the third, which reuses this field rather than adding one, and the
// fourth for the same reason: a hosted tool runs somewhere, and how long it
// takes is exactly what an author cannot control).
func TestValidateToolAnnounceLegalExecutionOnly(t *testing.T) {
	for _, tc := range []struct {
		name      string
		execution ToolExecution
		wantError bool
	}{
		{"webhook", ToolWebhook, false},
		{"local", ToolLocal, false},
		{"knowledge", ToolKnowledge, false},
		{"slng", ToolSlngHosted, false},
		{"builtin", ToolBuiltin, true},
		{"client", ToolClient, true},
		{"provider_hosted", ToolProviderHosted, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := safeAgent(t)
			tool := agent.Tools["lookup_customer"]
			tool.Announce = "Let me look that up."
			tool.Execution = tc.execution
			if tc.execution == ToolLocal {
				tool.Handler, tool.URLEnv = "tools/lookup_customer.py", ""
			}
			if tc.execution == ToolSlngHosted {
				// A hosted tool with no mirror is refused for that, which is a
				// different error; this case is about announce alone.
				tool.URLEnv, tool.Input, tool.Output = "", nil, nil
				tool.MirrorPin = MirrorDigest(nil)
				tool.Mirror = &packagespec.Mirror{Name: "lookup_customer", ToolType: "code", Source: "org"}
			}
			if tc.execution == ToolKnowledge {
				tool.URLEnv, tool.Input, tool.Output = "", nil, nil
				tool.KnowledgeBase = "refunds"
				agent.Knowledge = map[string]KnowledgeBase{
					"refunds": {Name: "refunds", Documents: "knowledge/refunds", Embed: "openai", Files: []string{"policy.pdf"},
						Mode: DefaultKnowledgeMode, ChunkSize: DefaultChunkSize, ChunkOverlap: DefaultChunkOverlap, TopK: DefaultTopK},
				}
				agent.Secrets = append(agent.Secrets, "OPENAI_API_KEY")
			}
			agent.Tools["lookup_customer"] = tool
			report, _ := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
			joined := strings.Join(report.PerTarget[0].Errors, "\n")
			const want = `tool "lookup_customer" announce is legal for webhook, local, knowledge and slng execution only`
			if got := strings.Contains(joined, want); got != tc.wantError {
				t.Errorf("announce refused = %v, want %v: %#v", got, tc.wantError, report.PerTarget[0].Errors)
			}
		})
	}
}

// TestValidateToolAnnounceRejectsTemplates: a fixed sentence, same rule as the
// transfer announcement. A rendered line would need its variable in scope at the
// moment the tool fires, which this field does not promise (FR-004).
func TestValidateToolAnnounceRejectsTemplates(t *testing.T) {
	agent := safeAgent(t)
	tool := agent.Tools["lookup_customer"]
	tool.Announce = "Checking on {{customer_id}} now."
	agent.Tools["lookup_customer"] = tool
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err == nil {
		t.Fatal("a templated announce must be refused")
	}
	if !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), `tool "lookup_customer" announce does not support templates`) {
		t.Errorf("error must name the tool and the reason: %#v", report.PerTarget[0].Errors)
	}
}

// TestValidateToolAnnouncePerTarget: only the two code drivers emit the line, so
// a managed provider fails with its own note rather than dropping it (FR-007).
func TestValidateToolAnnouncePerTarget(t *testing.T) {
	agent := safeAgent(t)
	tool := agent.Tools["lookup_customer"]
	tool.Announce = "Let me look that up."
	agent.Tools["lookup_customer"] = tool
	for provider, wantError := range map[Provider]bool{
		ProviderLiveKit: false,
		ProviderPipecat: false,
	} {
		report, err := Validate(agent, []Target{targetFor(agent, provider)}, targetcap.Default())
		if (err != nil) != wantError {
			t.Errorf("%s: err=%v report=%#v", provider, err, report.PerTarget)
		}
	}
}

// TestValidateToolAnnounceOnTaskScopePerTarget: scope, not kind. Both code
// drivers reach a task tool's speech seam, they just reach it differently.
// LiveKit lowers agent tools and task tools through one path and emits the same
// session.say either way; Pipecat emits a task tool as a flows handler and
// queues the frame through FlowManager.worker.
//
// This used to assert that Pipecat refused with "cannot announce a tool listed
// on a task: list it on the agent instead". That was a gap in this compiler
// written as a limit of the provider, and asserting it kept a working feature
// switched off. Now neither driver refuses, and the assertion is that neither
// does.
func TestValidateToolAnnounceOnTaskScopePerTarget(t *testing.T) {
	for _, provider := range []Provider{ProviderLiveKit, ProviderPipecat} {
		agent := safeAgent(t)
		tool := agent.Tools["lookup_customer"]
		tool.Announce = "Let me look that up."
		agent.Tools["lookup_customer"] = tool
		agent.Tasks["verify_caller"] = Task{
			Instructions: "Confirm who is calling.",
			Tools:        []string{"lookup_customer"},
			Result:       map[string]ResultField{"confirmed": {Type: PrimitiveBoolean}},
			Context:      TaskContext{History: HistoryFull},
		}

		report, err := Validate(agent, []Target{targetFor(agent, provider)}, targetcap.Default())
		if err != nil {
			t.Fatalf("%s: announcing a task tool must validate: %v %#v", provider, err, report.PerTarget[0].Errors)
		}
		joined := strings.Join(report.PerTarget[0].Errors, "\n")
		if strings.Contains(joined, "list it on the agent instead") {
			t.Errorf("%s: the old task-scope refusal is back: %#v", provider, report.PerTarget[0].Errors)
		}
	}
}

// --- SLNG Context Router -----------------------------------------------------

// routerAgent points safe_core's fast_reasoning think profile at the router and
// rebuilds, because the resolved per-target bindings are fixed in Build. Its
// sibling careful_reasoning stays on openai, so every case here is also a mixed
// package: one router profile beside one direct one.
func routerAgent(t *testing.T, mutate func(*packagespec.ModelDef)) *Agent {
	t.Helper()
	pkg := loadSafeCore(t)
	pkg.Agent.Secrets = append(pkg.Agent.Secrets, "SLNG_API_KEY")
	def := packagespec.ModelDef{
		Provider: "slng", Model: "gpt-5.6-luna", AgentID: "safe-core-v1",
		Upstream: &packagespec.Upstream{Provider: "openai"},
		Params:   map[string]any{"world_part_override": "eu", "reasoning_effort": "none"},
	}
	if mutate != nil {
		mutate(&def)
	}
	pkg.Agent.Models.Think["fast_reasoning"] = def
	agent, err := Build(pkg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return agent
}

// routerErrors validates a router package on one code target and returns its
// errors joined, so a case can assert on the words rather than the count.
func routerErrors(t *testing.T, agent *Agent, provider Provider) string {
	t.Helper()
	report, _ := Validate(agent, []Target{targetFor(agent, provider)}, targetcap.Default())
	return strings.Join(report.PerTarget[0].Errors, "\n")
}

func TestValidateSlngRouterAcceptsTheSmallestLegalBinding(t *testing.T) {
	agent := routerAgent(t, nil)
	for _, provider := range []Provider{ProviderPipecat, ProviderLiveKit} {
		if got := routerErrors(t, agent, provider); got != "" {
			t.Errorf("%s refused a valid router binding:\n%s", provider, got)
		}
	}
}

// FR-003 and FR-005. The four regions are the router's own set, and `na` copied
// off the regional infrastructure page is the likely mistake, so the refusal has
// to say which vocabulary is which.
func TestValidateSlngRouterRegion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params map[string]any
		wants  []string
	}{
		{"missing", map[string]any{}, []string{"world_part_override", "eu, us, india, indonesia"}},
		{"speech world part", map[string]any{"world_part_override": "na"}, []string{"na", "eu, us, india, indonesia", "speech"}},
		{"unknown", map[string]any{"world_part_override": "atlantis"}, []string{"atlantis", "eu, us, india, indonesia"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := routerAgent(t, func(def *packagespec.ModelDef) { def.Params = tc.params })
			got := routerErrors(t, agent, ProviderPipecat)
			for _, want := range tc.wants {
				if !strings.Contains(got, want) {
					t.Errorf("refusal does not say %q:\n%s", want, got)
				}
			}
		})
	}
}

// FR-007, at the validation layer. The shape rules themselves are held in
// internal/target; this is the half that proves a bad id reaches a refusal.
func TestValidateSlngRouterAgentID(t *testing.T) {
	for _, tc := range []struct {
		name, id, wants string
	}{
		{"missing", "", "agent_id is required"},
		{"whitespace", "safe core v1", "printable ASCII"},
		{"over long", strings.Repeat("v", targetcap.SlngAgentIDMaxLen+1), "the bound is"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := routerAgent(t, func(def *packagespec.ModelDef) { def.AgentID = tc.id })
			got := routerErrors(t, agent, ProviderPipecat)
			if !strings.Contains(got, tc.wants) {
				t.Errorf("refusal does not say %q:\n%s", tc.wants, got)
			}
			if !strings.Contains(got, "think.fast_reasoning") {
				t.Errorf("refusal does not name the profile:\n%s", got)
			}
		})
	}
}

// The bound moved from the authored value to the derived one, so an id that
// passes on its own can still be refused once a site name is added to it. That
// refusal is the point: the whole value becomes the header value, and a
// truncation would let two sites collapse onto one scope silently.
func TestValidateSlngRouterScopeOverBound(t *testing.T) {
	// Long enough that the shortest site name pushes it over, short enough that
	// the authored-value rule still accepts it. safe_core's shortest agent name
	// is `intake`, six characters, plus one separator.
	id := strings.Repeat("v", targetcap.SlngAgentIDMaxLen-len("intake"))
	if err := targetcap.ValidateSlngAgentID(id); err != nil {
		t.Fatalf("fixture must pass the authored-value rule first: %v", err)
	}
	agent := routerAgent(t, func(def *packagespec.ModelDef) { def.AgentID = id })
	got := routerErrors(t, agent, ProviderPipecat)
	// The author has two names to choose between, so the refusal has to name
	// the site as well as the profile and the bound.
	for _, want := range []string{
		"think.fast_reasoning", "agent intake", "the bound is 128", targetcap.SlngAgentIDHeader, "Shorten",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("refusal does not say %q:\n%s", want, got)
		}
	}
	// And a package whose *longest* site still fits compiles, so the bound is
	// the bound and not a margin. safe_core's longest suffix is seven
	// characters: `billing`, and the summarizer's `summary` matches it.
	shorter := strings.Repeat("v", targetcap.SlngAgentIDMaxLen-len("billing")-len(targetcap.SlngScopeSeparator))
	if got := routerErrors(t, routerAgent(t, func(def *packagespec.ModelDef) { def.AgentID = shorter }), ProviderPipecat); got != "" {
		t.Errorf("a scope of exactly the bound was refused:\n%s", got)
	}
}

// A derived scope is unique per site only because the separator appears in it
// exactly once, and what guarantees that is checkNames, written for Python
// identifiers long before anything composed a header value out of a name.
//
// So the guarantee is gated here, at the rule that actually holds it. Loosening
// namePattern to admit a colon would let two sites derive one scope and bring
// back the collision this whole feature exists to kill, with nothing else in the
// tree noticing. The plan for this change expected to need a second refusal in
// Validate; it does not, and this is the check that keeps that true.
func TestNamePatternExcludesTheScopeSeparator(t *testing.T) {
	for _, name := range []string{
		"billing" + targetcap.SlngScopeSeparator + "eu",
		targetcap.SlngScopeSeparator + "billing",
		"billing" + targetcap.SlngScopeSeparator,
	} {
		if namePattern.MatchString(name) {
			t.Errorf("namePattern admits %q, so two prompt sites could derive one cache scope", name)
		}
	}
	// And the shape a scope is actually built from still passes, so the rule is
	// refusing the separator rather than everything.
	for _, name := range []string{"billing", "customer_verification", "chat_with_me"} {
		if !namePattern.MatchString(name) {
			t.Errorf("namePattern refuses %q, which the shipped example uses", name)
		}
	}
}

// FR-010, the rule with the worst silent cost: a second id splits one package's
// cache and nothing fails, the agent is just never fast. The refusal names both
// profiles and both values because the author has to know which line to change.
func TestValidateSlngRouterOneAgentIDPerPackage(t *testing.T) {
	pkg := loadSafeCore(t)
	pkg.Agent.Secrets = append(pkg.Agent.Secrets, "SLNG_API_KEY")
	router := func(id string) packagespec.ModelDef {
		return packagespec.ModelDef{
			Provider: "slng", Model: "gpt-5.6-luna", AgentID: id,
			Upstream: &packagespec.Upstream{Provider: "openai"},
			Params:   map[string]any{"world_part_override": "eu", "reasoning_effort": "none"},
		}
	}
	pkg.Agent.Models.Think["fast_reasoning"] = router("safe-core-v1")
	pkg.Agent.Models.Think["careful_reasoning"] = router("safe-core-careful-v1")
	agent, err := Build(pkg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got := routerErrors(t, agent, ProviderPipecat)
	for _, want := range []string{
		"fast_reasoning", "careful_reasoning", "safe-core-v1", "safe-core-careful-v1", "one package sends one agent id",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("refusal does not say %q:\n%s", want, got)
		}
	}
	// An *unused* second profile is refused too. It emits nothing today, so the
	// per-target view never sees it, but pointing one agent at it is a one-word
	// edit and the cost of finding out then is a latency graph rather than a
	// compile error. Found by walking the quickstart refusals by hand.
	pkg.Agent.Models.Think["careful_reasoning"] = router("safe-core-careful-v1")
	pkg.Agent.Agents["billing"] = func() packagespec.AgentDef {
		def := pkg.Agent.Agents["billing"]
		def.Think = "fast_reasoning"
		return def
	}()
	unused, err := Build(pkg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := routerErrors(t, unused, ProviderPipecat); !strings.Contains(got, "one package sends one agent id") {
		t.Errorf("an unused second router profile with a different id was accepted:\n%s", got)
	}

	// The same two profiles agreeing is the whole point of the rule, so it has
	// to pass: this test cannot be satisfied by refusing every second profile.
	pkg.Agent.Models.Think["careful_reasoning"] = router("safe-core-v1")
	agreed, err := Build(pkg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := routerErrors(t, agreed, ProviderPipecat); got != "" {
		t.Errorf("two profiles carrying the same id were refused:\n%s", got)
	}
}

// FR-034: no block, no provider inside it, or an unknown one, and every refusal
// names the five accepted providers.
func TestValidateSlngRouterUpstreamProvider(t *testing.T) {
	for _, tc := range []struct {
		name     string
		upstream *packagespec.Upstream
		wants    string
	}{
		{"absent", nil, "needs an upstream block"},
		{"no provider", &packagespec.Upstream{}, "needs a provider"},
		{"unknown", &packagespec.Upstream{Provider: "anthropic"}, "not one the router accepts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := routerAgent(t, func(def *packagespec.ModelDef) { def.Upstream = tc.upstream })
			got := routerErrors(t, agent, ProviderPipecat)
			if !strings.Contains(got, tc.wants) {
				t.Errorf("refusal does not say %q:\n%s", tc.wants, got)
			}
			for _, provider := range targetcap.SlngUpstreamProviders() {
				if !strings.Contains(got, provider) {
					t.Errorf("refusal does not name %q:\n%s", provider, got)
				}
			}
		})
	}
}

// FR-034c: a missing required field and a field belonging to another provider are
// both refused by name, because an unknown endpoint key comes back as a 400 on
// every think request.
func TestValidateSlngRouterUpstreamFields(t *testing.T) {
	azure := func() *packagespec.Upstream {
		return &packagespec.Upstream{
			Provider: "azure", URL: "https://r.cognitiveservices.azure.com/",
			KeyEnv: "AZURE_OPENAI_API_KEY", Deployment: "gpt-4o-deploy", APIVersion: "2024-12-01-preview",
		}
	}
	for _, tc := range []struct {
		name     string
		upstream func() *packagespec.Upstream
		wants    []string
	}{
		{"missing api_version", func() *packagespec.Upstream {
			u := azure()
			u.APIVersion = ""
			return u
		}, []string{"missing api_version", "azure requires"}},
		{"another provider's field", func() *packagespec.Upstream {
			u := azure()
			u.Location = "europe-west4"
			return u
		}, []string{"location belongs to provider vertex", "azure requires"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := routerAgent(t, func(def *packagespec.ModelDef) {
				def.Upstream = tc.upstream()
			})
			got := routerErrors(t, agent, ProviderPipecat)
			for _, want := range tc.wants {
				if !strings.Contains(got, want) {
					t.Errorf("refusal does not say %q:\n%s", want, got)
				}
			}
		})
	}
}

// FR-034a. The value is never echoed: what lands in a *_env field when it is
// wrong is usually a pasted credential, and a refusal that quotes it puts the
// secret in a terminal, a CI log and a bug report.
func TestValidateSlngRouterCredentialIsANameNotAValue(t *testing.T) {
	const pasted = "sk-live-9aa0not-a-real-key"
	agent := routerAgent(t, func(def *packagespec.ModelDef) {
		def.Upstream = &packagespec.Upstream{Provider: "openai", KeyEnv: pasted}
	})
	got := routerErrors(t, agent, ProviderPipecat)
	if !strings.Contains(got, "upstream key_env is not an environment variable name") {
		t.Errorf("refusal does not say what the field takes:\n%s", got)
	}
	if strings.Contains(got, pasted) {
		t.Errorf("the refusal echoes the value back:\n%s", got)
	}
	report, _ := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if warnings := strings.Join(report.PerTarget[0].Warnings, "\n"); strings.Contains(warnings, pasted) {
		t.Errorf("a warning echoes the value back:\n%s", warnings)
	}
}

// FR-034b, both halves. A name the author wrote has to be declared, and a name
// the compiler supplied is never demanded, which is the same boundary
// TestSecretsCrossCheckNeverAsksForDriverSuppliedNames holds for provider keys.
func TestValidateSlngRouterCredentialMustBeDeclared(t *testing.T) {
	agent := routerAgent(t, func(def *packagespec.ModelDef) {
		def.Upstream = &packagespec.Upstream{
			Provider: "openai-compat", URL: "https://host/v1", KeyEnv: "HOST_LLM_KEY",
		}
	})
	got := routerErrors(t, agent, ProviderPipecat)
	for _, want := range []string{"upstream key_env names HOST_LLM_KEY", "secrets:"} {
		if !strings.Contains(got, want) {
			t.Errorf("refusal does not say %q:\n%s", want, got)
		}
	}

	// The other half: provider openai supplies OPENAI_API_KEY itself, so the
	// author is never asked to declare it and never warned about it either.
	supplied := routerAgent(t, nil)
	if got := routerErrors(t, supplied, ProviderPipecat); strings.Contains(got, "upstream key_env") {
		t.Errorf("the build asks for a name the compiler supplied:\n%s", got)
	}
	report, _ := Validate(supplied, []Target{targetFor(supplied, ProviderPipecat)}, targetcap.Default())
	for _, warning := range report.PerTarget[0].Warnings {
		if strings.Contains(warning, "upstream.key_env") {
			t.Errorf("the cross-check names a compiler-supplied upstream credential: %s", warning)
		}
	}
}

// D2: two owners for one endpoint is one owner too many.
func TestValidateSlngRouterRejectsEndpointEnv(t *testing.T) {
	agent := routerAgent(t, func(def *packagespec.ModelDef) { def.EndpointEnv = "ROUTER_BASE_URL" })
	got := routerErrors(t, agent, ProviderPipecat)
	if !strings.Contains(got, "endpoint_env") || !strings.Contains(got, "world_part_override") {
		t.Errorf("refusal does not name both owners:\n%s", got)
	}
}

// The two router fields are the router's. On any other binding they have no slot,
// and a slotless field is refused rather than dropped.
func TestValidateSlngRouterFieldsHaveNoSlotElsewhere(t *testing.T) {
	for _, tc := range []struct {
		name  string
		set   func(*packagespec.ModelDef)
		wants string
	}{
		{"agent_id", func(def *packagespec.ModelDef) { def.AgentID = "safe-core-v1" }, "sets agent_id"},
		{"upstream", func(def *packagespec.ModelDef) { def.Upstream = &packagespec.Upstream{Provider: "openai"} }, "sets upstream"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := loadSafeCore(t)
			def := pkg.Agent.Models.Think["fast_reasoning"]
			tc.set(&def)
			pkg.Agent.Models.Think["fast_reasoning"] = def
			agent, err := Build(pkg)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			got := routerErrors(t, agent, ProviderPipecat)
			if !strings.Contains(got, tc.wants) {
				t.Errorf("refusal does not say %q:\n%s", tc.wants, got)
			}
		})
	}
}

// FR-001 and FR-014: a target with no slng reason row refuses in its own
// vocabulary rather than falling through to whatever its wildcard row is.
func TestValidateSlngRouterNeedsARowOnTheTarget(t *testing.T) {
	// Both remaining targets carry an slng reason row, which is the case this
	// used to prove by contrast against the two retired targets that did not.
	// Assert the positive: the row exists, so the binding is accepted.
	agent := routerAgent(t, nil)
	for _, provider := range []Provider{ProviderLiveKit, ProviderPipecat} {
		if got := routerErrors(t, agent, provider); got != "" {
			t.Errorf("%s has an slng reason row but refused the binding: %s", provider, got)
		}
	}
}

// FR-037: a warning, on stderr, exit 0. Not a refusal, because the compiler
// cannot know the upstream model family for certain.
func TestValidateSlngRouterWarnsOnToolsWithoutReasoningEffort(t *testing.T) {
	agent := routerAgent(t, func(def *packagespec.ModelDef) {
		def.Params = map[string]any{"world_part_override": "eu"}
	})
	report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err != nil {
		t.Fatalf("the missing param must warn, never fail: %v %#v", err, report.PerTarget[0].Errors)
	}
	warnings := strings.Join(report.PerTarget[0].Warnings, "\n")
	if !strings.Contains(warnings, "reasoning_effort") || !strings.Contains(warnings, "400") {
		t.Errorf("the warning does not name the failure it prevents:\n%s", warnings)
	}
	// With the param set there is nothing to say.
	quiet := routerAgent(t, nil)
	report, _ = Validate(quiet, []Target{targetFor(quiet, ProviderPipecat)}, targetcap.Default())
	if got := strings.Join(report.PerTarget[0].Warnings, "\n"); strings.Contains(got, "reasoning_effort") {
		t.Errorf("warned about a param that is set:\n%s", got)
	}
	// And nothing to say on an upstream that does not serve OpenAI's models,
	// where the advice is not merely unnecessary but wrong: qwen/qwen3-32b on
	// Nebius answers reasoning_effort with a 400 (measured 2026-08-27). Firing
	// here would tell every OpenRouter author, on every compile, to set the one
	// param that breaks their agent.
	compat := routerAgent(t, func(def *packagespec.ModelDef) {
		def.Params = map[string]any{"world_part_override": "eu"}
		def.Upstream = &packagespec.Upstream{
			Provider: "openai-compat",
			URL:      "https://openrouter.ai/api/v1",
			KeyEnv:   "OPENROUTER_API_KEY",
		}
	})
	// The upstream credential has to be declared or a different gate refuses the
	// package before this one gets a word in.
	compat.Secrets = append(compat.Secrets, "OPENROUTER_API_KEY")
	report, err = Validate(compat, []Target{targetFor(compat, ProviderPipecat)}, targetcap.Default())
	if err != nil {
		t.Fatalf("an openai-compat upstream must validate: %v %#v", err, report.PerTarget[0].Errors)
	}
	if got := strings.Join(report.PerTarget[0].Warnings, "\n"); strings.Contains(got, "reasoning_effort") {
		t.Errorf("advised reasoning_effort on an upstream whose host answers it with a 400:\n%s", got)
	}
}

// The one target-shaped limit this feature adds, gated because a rule with no
// gate is a wish. LiveKit builds an agent's or a task's own LLM inside its
// constructor, where neither the call's session id nor the call state is in
// scope, and it constructs agents from ten places. So a router profile that is
// not the entry agent's has nowhere to put its per-call values there. Pipecat
// builds one LLM per agent from a single place and has no such limit, which is
// why the same package is legal on one target and not the other.
func TestValidateSlngRouterSecondProfileIsLiveKitOnlyRefusal(t *testing.T) {
	pkg := loadSafeCore(t)
	pkg.Agent.Secrets = append(pkg.Agent.Secrets, "SLNG_API_KEY")
	router := packagespec.ModelDef{
		Provider: "slng", Model: "gpt-5.6-luna", AgentID: "safe-core-v1",
		Upstream: &packagespec.Upstream{Provider: "openai"},
		Params:   map[string]any{"world_part_override": "eu", "reasoning_effort": "none"},
	}
	// Both profiles are router bindings carrying the same id, so FR-010 is happy.
	// careful_reasoning is the billing agent's, not the entry agent's.
	pkg.Agent.Models.Think["fast_reasoning"] = router
	pkg.Agent.Models.Think["careful_reasoning"] = router
	agent, err := Build(pkg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	got := routerErrors(t, agent, ProviderLiveKit)
	for _, want := range []string{"careful_reasoning", "entry agent", "intake", "fast_reasoning"} {
		if !strings.Contains(got, want) {
			t.Errorf("the livekit refusal does not say %q:\n%s", want, got)
		}
	}
	// The refusal names the target, because the same package compiles on the
	// other one. An author who cannot see which target refused has to guess.
	if !strings.Contains(got, "livekit") {
		t.Errorf("the refusal does not name the target:\n%s", got)
	}
	if got := routerErrors(t, agent, ProviderPipecat); got != "" {
		t.Errorf("pipecat refused a second router profile it can serve:\n%s", got)
	}
}

// The drivers add UnservedResultField to every task's finish themselves, so a
// task result claiming the name would collide with the generated argument.
func TestValidateRejectsReservedTaskResultField(t *testing.T) {
	agent := safeAgent(t)
	agent.Tasks["collect"] = Task{
		Instructions: "collect",
		Result: map[string]ResultField{
			"done":              {Type: PrimitiveBoolean},
			UnservedResultField: {Type: PrimitiveString},
		},
		Context: TaskContext{History: HistoryFull},
	}
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err == nil {
		t.Fatal("expected a validation error for the reserved result field")
	}
	text := strings.Join(report.PerTarget[0].Errors, "\n")
	if !strings.Contains(text, UnservedResultField) || !strings.Contains(text, "is reserved") {
		t.Errorf("error does not name the reserved field: %q", text)
	}
}

// endpointing_delay is the window of silence that has to pass before the runtime
// treats the caller as finished, and it only means anything on a turn model.
func TestValidateEndpointingDelayIsATurnFieldAndPositive(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  ModelKind
		value Duration
		want  string
	}{
		{name: "turn model takes it", kind: KindTurn, value: "1s"},
		{name: "listen model does not", kind: KindListen, value: "1s", want: "endpointing_delay is a turn-model field"},
		{name: "zero is refused", kind: KindTurn, value: "0s", want: "endpointing_delay must be a positive Go duration"},
		{name: "garbage is refused", kind: KindTurn, value: "soon", want: "endpointing_delay must be a positive Go duration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := validateModelKind("slow_stt", ModelDef{Kind: tc.kind, EndpointingDelay: tc.value})
			text := strings.Join(got, "\n")
			if tc.want == "" {
				if text != "" {
					t.Fatalf("unexpected errors: %s", text)
				}
				return
			}
			if !strings.Contains(text, tc.want) {
				t.Fatalf("want %q, got %q", tc.want, text)
			}
		})
	}
}

// TestValidateEndpointingDelayLiveKitFloor is the compile-time half of a runtime
// crash. livekit-agents refuses a VAD silence window under 250ms when a
// streaming turn detector is bound: _check_vad_silence_requirement raises
// ValueError (voice/audio_recognition.py:885-903, against
// MIN_SILENCE_DURATION_MS = 200 plus 50 in inference/eot/base.py). A package
// that authors 200ms builds fine and then dies when the worker takes its first
// call, which is the worst possible moment to find out.
//
// The floor is unconditional on LiveKit because the driver already refuses a
// target with no turn binding ("missing open turn binding"), so the streaming
// detector the SDK checks against is always there. Pipecat has no documented
// floor and is left alone.
func TestValidateEndpointingDelayLiveKitFloor(t *testing.T) {
	for _, tc := range []struct {
		name      string
		provider  Provider
		value     Duration
		wantError bool
	}{
		{name: "at the floor is allowed", provider: ProviderLiveKit, value: "250ms"},
		{name: "above the floor is allowed", provider: ProviderLiveKit, value: "300ms"},
		{name: "the runtime default is allowed", provider: ProviderLiveKit, value: "550ms"},
		{name: "below the floor is refused", provider: ProviderLiveKit, value: "200ms", wantError: true},
		{name: "far below is refused", provider: ProviderLiveKit, value: "10ms", wantError: true},
		{name: "pipecat has no floor to enforce", provider: ProviderPipecat, value: "10ms"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := safeAgent(t)
			tgt := targetFor(agent, tc.provider)
			if tgt.Models.Turn == nil {
				t.Fatalf("%s target has no turn binding to configure", tc.provider)
			}
			turn := *tgt.Models.Turn
			turn.EndpointingDelay = tc.value
			tgt.Models.Turn = &turn
			report, err := Validate(agent, []Target{tgt}, targetcap.Default())
			if (err != nil) != tc.wantError {
				t.Fatalf("err = %v, wantError = %v: errors=%#v", err, tc.wantError, reportFor(report, tc.provider).Errors)
			}
			if !tc.wantError {
				return
			}
			text := strings.Join(reportFor(report, tc.provider).Errors, "\n")
			// The message has to carry both numbers, because the author needs to
			// know what they wrote and what the runtime will accept.
			for _, want := range []string{string(tc.value), "250ms"} {
				if !strings.Contains(text, want) {
					t.Errorf("error does not name %q: %q", want, text)
				}
			}
		})
	}
}

// knowledgeAgent is a package with one knowledge base and one tool searching it,
// which every case below then breaks one way.
func knowledgeAgent(t *testing.T) *Agent {
	t.Helper()
	agent := safeAgent(t)
	agent.Knowledge = map[string]KnowledgeBase{
		"refunds": {Name: "refunds", Documents: "knowledge/refunds", Embed: "openai", Files: []string{"policy.pdf"},
			Mode: DefaultKnowledgeMode, ChunkSize: DefaultChunkSize, ChunkOverlap: DefaultChunkOverlap, TopK: DefaultTopK},
	}
	agent.Secrets = append(agent.Secrets, "OPENAI_API_KEY")
	slices.Sort(agent.Secrets)
	tool := agent.Tools["lookup_customer"]
	tool.Execution, tool.KnowledgeBase = ToolKnowledge, "refunds"
	tool.URLEnv, tool.Input, tool.Output, tool.Handler = "", nil, nil, ""
	tool.Description = "Look up the salon's refund policy."
	agent.Tools["lookup_customer"] = tool
	return agent
}

// TestValidateKnowledgeCompileErrors walks every compile-time message in
// contracts/authoring.md. Each case asserts the message names the offending value
// and, where there is a legal set, lists it: an error that says only "invalid"
// makes the author guess, which is the failure Fail Loud exists to prevent.
func TestValidateKnowledgeCompileErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		break_ func(*Agent)
		want   []string
	}{
		{
			// FR-012. The declared list is in the message so the author can see
			// the name they meant to type.
			name: "undeclared base",
			break_: func(a *Agent) {
				tool := a.Tools["lookup_customer"]
				tool.KnowledgeBase = "refund"
				a.Tools["lookup_customer"] = tool
			},
			want: []string{`"lookup_customer"`, `"refund"`, "not declared", "declared: refunds"},
		},
		{
			name:   "no knowledge section at all",
			break_: func(a *Agent) { a.Knowledge = nil },
			want:   []string{`"refunds"`, "no knowledge: section is declared"},
		},
		{
			// FR-009. Build leaves Files empty for a folder that is missing or
			// holds nothing readable, so both arrive here as the same case.
			name:   "folder holds nothing readable",
			break_: func(a *Agent) { base := a.Knowledge["refunds"]; base.Files = nil; a.Knowledge["refunds"] = base },
			want:   []string{`"refunds"`, `"knowledge/refunds"`, "no file of a supported type", ".pdf"},
		},
		{
			name:   "documents missing",
			break_: func(a *Agent) { base := a.Knowledge["refunds"]; base.Documents = ""; a.Knowledge["refunds"] = base },
			want:   []string{`"refunds"`, "documents is required"},
		},
		{
			// FR-011, and the message lists what is supported because the author
			// cannot be expected to guess the spelling of a service.
			name:   "unknown embed",
			break_: func(a *Agent) { base := a.Knowledge["refunds"]; base.Embed = "cohere"; a.Knowledge["refunds"] = base },
			want:   []string{`"refunds"`, `"cohere"`, "supported: bedrock, gemini, huggingface, openai"},
		},
		{
			// FR-016. The name is printed; the value never is, because there is
			// no value here to print.
			name: "credential not declared",
			break_: func(a *Agent) {
				a.Secrets = slices.DeleteFunc(a.Secrets, func(s string) bool { return s == "OPENAI_API_KEY" })
			},
			want: []string{`"refunds"`, `"openai"`, "OPENAI_API_KEY in secrets:"},
		},
		{
			// A two-character name is refused for readability: it names a folder,
			// a settings key and every log line about the base. It was Chroma's
			// own rule before that store was dropped, and the bound outlived it.
			name: "name too short",
			break_: func(a *Agent) {
				a.Knowledge = map[string]KnowledgeBase{"hr": {Name: "hr", Documents: "knowledge/hr", Embed: "openai", Files: []string{"a.md"},
					Mode: DefaultKnowledgeMode, ChunkSize: DefaultChunkSize, ChunkOverlap: DefaultChunkOverlap, TopK: DefaultTopK}}
				tool := a.Tools["lookup_customer"]
				tool.KnowledgeBase = "hr"
				a.Tools["lookup_customer"] = tool
			},
			want: []string{`"hr"`, "3 to 64 characters of [a-z0-9_]"},
		},
		{
			name: "name has illegal characters",
			break_: func(a *Agent) {
				a.Knowledge = map[string]KnowledgeBase{"Refund Policy": {Name: "Refund Policy", Documents: "kb", Embed: "openai", Files: []string{"a.md"},
					Mode: DefaultKnowledgeMode, ChunkSize: DefaultChunkSize, ChunkOverlap: DefaultChunkOverlap, TopK: DefaultTopK}}
				tool := a.Tools["lookup_customer"]
				tool.KnowledgeBase = "Refund Policy"
				a.Tools["lookup_customer"] = tool
			},
			want: []string{`"Refund Policy"`, "3 to 64 characters of [a-z0-9_]"},
		},
		{
			// spec.Load refuses these with a line number, so reaching validation
			// means the IR was built in code. The message still has to be right.
			name: "input on a knowledge tool",
			break_: func(a *Agent) {
				tool := a.Tools["lookup_customer"]
				tool.Input = map[string]any{"type": "object"}
				a.Tools["lookup_customer"] = tool
			},
			want: []string{`"lookup_customer"`, "takes no input or output", "owns its own schema"},
		},
		{
			name: "base on a webhook tool",
			break_: func(a *Agent) {
				tool := a.Tools["escalate"]
				tool.KnowledgeBase = "refunds"
				a.Tools["escalate"] = tool
			},
			want: []string{`"escalate"`, "base is legal for knowledge execution only"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := knowledgeAgent(t)
			tc.break_(agent)
			report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
			if err == nil {
				t.Fatalf("want a validation failure, got none: %#v", report.PerTarget[0].Errors)
			}
			joined := strings.Join(report.PerTarget[0].Errors, "\n")
			for _, want := range tc.want {
				if !strings.Contains(joined, want) {
					t.Errorf("message must contain %q, got:\n%s", want, joined)
				}
			}
		})
	}
}

// TestValidateKnowledgeAccepted: the shape in contracts/authoring.md compiles,
// including two bases where only one is searched by any one tool, and no
// credential is ever printed as a value.
func TestValidateKnowledgeAccepted(t *testing.T) {
	agent := knowledgeAgent(t)
	if _, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default()); err != nil {
		t.Fatalf("the documented shape must compile: %v", err)
	}
	if got := agent.Knowledge["refunds"].Embed; got != "openai" {
		t.Errorf("embed = %q, want the resolved default", got)
	}
}

// TestValidateKnowledgeUnusedWarns: declared and never searched is a warning and
// exit 0, not a failure. It is worth saying because every process start reads and
// embeds it, so the cost is real even though the package runs.
func TestValidateKnowledgeUnusedWarns(t *testing.T) {
	agent := knowledgeAgent(t)
	agent.Knowledge["archive"] = KnowledgeBase{Name: "archive", Documents: "knowledge/archive", Embed: "openai", Files: []string{"old.md"},
		Mode: DefaultKnowledgeMode, ChunkSize: DefaultChunkSize, ChunkOverlap: DefaultChunkOverlap, TopK: DefaultTopK}
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err != nil {
		t.Fatalf("an unused knowledge base must not fail the build: %v", err)
	}
	joined := strings.Join(report.PerTarget[0].Warnings, "\n")
	for _, want := range []string{"archive", "embedded at every start and never read"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warning must contain %q, got:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "refunds") {
		t.Errorf("the base a tool does use must not be warned about:\n%s", joined)
	}
}

// TestValidateKnowledgeOnBothTargets holds FR-013 from the validation side.
//
// Both targets now accept a knowledge tool, because both drivers emit one. The
// agent-scoped row was denied on Pipecat until 2026-08-25, which was the point:
// constitution 5.0.0 retired the vapi and deepgram targets for validating what
// could not compile, so this kind stayed closed on Pipecat until its lowering
// existed.
//
// The task-scoped row is still gated on Pipecat, for a different reason: a task
// tool there is a flows handler holding a FlowManager, not a decorated function.
// That refusal has to name the target and say why.
func TestValidateKnowledgeOnBothTargets(t *testing.T) {
	for _, provider := range []Provider{ProviderLiveKit, ProviderPipecat} {
		agent := knowledgeAgent(t)
		if _, err := Validate(agent, []Target{targetFor(agent, provider)}, targetcap.Default()); err != nil {
			t.Errorf("a knowledge tool must compile on %s: %v", provider, err)
		}
	}

	agent := knowledgeAgent(t)
	agent.Tasks = map[string]Task{
		"verify": {Instructions: "Verify the caller.", Tools: []string{"lookup_customer"}},
	}
	report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err == nil {
		t.Fatal("a knowledge tool scoped to a Pipecat task must be refused")
	}
	joined := strings.Join(report.PerTarget[0].Errors, "\n")
	if !strings.Contains(joined, "scope a knowledge tool to a task") {
		t.Errorf("the refusal must say what to do instead, got:\n%s", joined)
	}
}

// TestValidateKnowledgeRetrievalSettings: every bound here is something that
// actually breaks, not a matter of taste.
//
// SentenceSplitter refuses chunk_size <= 0 with a pydantic ValidationError, and
// refuses an overlap larger than the size with "Got a larger chunk overlap (25)
// than chunk size (20), should be smaller." Both measured against llama-index on
// 2026-08-26. Catching them at compile is the difference between an authoring
// error and a container that will not start.
func TestValidateKnowledgeRetrievalSettings(t *testing.T) {
	set := func(a *Agent, size, overlap, topK int) {
		base := a.Knowledge["refunds"]
		base.ChunkSize, base.ChunkOverlap, base.TopK = size, overlap, topK
		a.Knowledge["refunds"] = base
	}
	for _, tc := range []struct {
		name                string
		size, overlap, topK int
		want                []string
	}{
		{"chunk_size zero", 0, 0, 3, []string{"chunk_size must be at least 1 token", "got 0"}},
		{"chunk_size negative", -5, 0, 3, []string{"chunk_size must be at least 1 token"}},
		{"chunk_size too large", MaxChunkSize + 1, 20, 3, []string{"above the", "token limit"}},
		{"overlap negative", 90, -1, 3, []string{"chunk_overlap cannot be negative"}},
		// The splitter's own refusal, caught earlier.
		{"overlap larger than size", 90, 91, 3, []string{"chunk_overlap 91 is larger than chunk_size 90", "which the splitter refuses"}},
		{"top_k zero", 90, 20, 0, []string{"top_k must be at least 1", "no reason to exist"}},
		{"top_k too large", 90, 20, MaxTopK + 1, []string{"top_k", "above the limit"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := knowledgeAgent(t)
			set(agent, tc.size, tc.overlap, tc.topK)
			report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
			if err == nil {
				t.Fatalf("want a failure, got none: %#v", report.PerTarget[0].Errors)
			}
			joined := strings.Join(report.PerTarget[0].Errors, "\n")
			for _, want := range tc.want {
				if !strings.Contains(joined, want) {
					t.Errorf("message must contain %q, got:\n%s", want, joined)
				}
			}
		})
	}

	// Legal edges. Overlap equal to size is accepted by the splitter, so it is
	// accepted here: a bound that is stricter than the thing it protects turns a
	// working package into a refused one.
	for _, tc := range []struct {
		name                string
		size, overlap, topK int
	}{
		{"defaults", DefaultChunkSize, DefaultChunkOverlap, DefaultTopK},
		{"no overlap at all", 90, 0, 3},
		{"overlap equal to size", 40, 40, 1},
		{"a wide window for a price list", 220, 40, 3},
		{"the upper bounds", MaxChunkSize, 0, MaxTopK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := knowledgeAgent(t)
			set(agent, tc.size, tc.overlap, tc.topK)
			if _, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default()); err != nil {
				t.Errorf("chunk_size %d overlap %d top_k %d must compile: %v", tc.size, tc.overlap, tc.topK, err)
			}
		})
	}
}

// TestKnowledgeDefaultsAreAppliedAtBuild: an absent field resolves, and an
// authored zero survives.
//
// The pointer types exist for the second half. `chunk_overlap: 0` is a legal
// choice — no overlap at all — and a plain int would make it indistinguishable
// from "unset", so Build would silently replace it with 20.
func TestKnowledgeDefaultsAreAppliedAtBuild(t *testing.T) {
	pkg := loadSafeCore(t)
	zero := 0
	pkg.Agent.Knowledge = map[string]packagespec.KnowledgeDef{
		"defaulted": {Documents: "kb"},
		"explicit":  {Documents: "kb", ChunkOverlap: &zero},
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	got := agent.Knowledge["defaulted"]
	if got.ChunkSize != DefaultChunkSize || got.ChunkOverlap != DefaultChunkOverlap || got.TopK != DefaultTopK {
		t.Errorf("defaults not applied: %+v", got)
	}
	if explicit := agent.Knowledge["explicit"]; explicit.ChunkOverlap != 0 {
		t.Errorf("an authored chunk_overlap of 0 was replaced by the default: %+v", explicit)
	}
}

// TestKnowledgeBudgetWarns: top_k and chunk_size are two small numbers that
// multiply into the model's context on every lookup, during a phone call. A large
// product is a legitimate choice for a dense reference document, so this warns
// and the build succeeds.
func TestKnowledgeBudgetWarns(t *testing.T) {
	agent := knowledgeAgent(t)
	base := agent.Knowledge["refunds"]
	base.ChunkSize, base.TopK = 500, 8 // 4000 tokens a lookup
	agent.Knowledge["refunds"] = base
	report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
	if err != nil {
		t.Fatalf("a large budget is a choice, not an error: %v", err)
	}
	joined := strings.Join(report.PerTarget[0].Warnings, "\n")
	for _, want := range []string{"refunds", "top_k 8", "chunk_size 500", "4000 tokens"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warning must contain %q, got:\n%s", want, joined)
		}
	}
	// And the default package does not warn, or the warning is noise.
	plain := knowledgeAgent(t)
	quiet, err := Validate(plain, []Target{targetFor(plain, ProviderLiveKit)}, targetcap.Default())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(quiet.PerTarget[0].Warnings, "\n"), "retrieved text per lookup") {
		t.Error("the defaults must not warn")
	}
}

// TestValidateKnowledgeMinScore: the range is 0 to 1, and a value above the
// measured-useful band warns rather than failing.
//
// It warns because the failure is silent. A cutoff set too high produces no error
// and no empty-index message; the agent simply says it does not know, on every
// question. And the scale is not what most people expect: these are similarity
// scores, measured 0.207 to 0.446 on the example corpus, so a value like 0.9 —
// which reads like a confident probability — returns nothing at all.
func TestValidateKnowledgeMinScore(t *testing.T) {
	set := func(a *Agent, score float64) {
		base := a.Knowledge["refunds"]
		base.MinScore = score
		a.Knowledge["refunds"] = base
	}
	for _, score := range []float64{-0.1, 1.5} {
		agent := knowledgeAgent(t)
		set(agent, score)
		report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
		if err == nil {
			t.Fatalf("min_score %g must be refused", score)
		}
		if !strings.Contains(strings.Join(report.PerTarget[0].Errors, "\n"), "min_score must be between 0 and 1") {
			t.Errorf("min_score %g: %v", score, report.PerTarget[0].Errors)
		}
	}
	// Inside the useful band: accepted, and quiet.
	for _, score := range []float64{0, 0.15, 0.2, MinScoreWarn} {
		agent := knowledgeAgent(t)
		set(agent, score)
		report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
		if err != nil {
			t.Errorf("min_score %g must compile: %v", score, err)
			continue
		}
		if strings.Contains(strings.Join(report.PerTarget[0].Warnings, "\n"), "min_score above") {
			t.Errorf("min_score %g is inside the measured band and must not warn", score)
		}
	}
	// Above it: accepted, and loud. 0.9 is the value a reader who thinks these are
	// probabilities reaches for, so it is the case worth naming.
	for _, score := range []float64{0.4, 0.9} {
		agent := knowledgeAgent(t)
		set(agent, score)
		report, err := Validate(agent, []Target{targetFor(agent, ProviderLiveKit)}, targetcap.Default())
		if err != nil {
			t.Fatalf("min_score %g is the author's call, not an error: %v", score, err)
		}
		joined := strings.Join(report.PerTarget[0].Warnings, "\n")
		for _, want := range []string{"min_score above", "dropping real answers", "similarity scores, not probabilities"} {
			if !strings.Contains(joined, want) {
				t.Errorf("min_score %g: warning must contain %q, got:\n%s", score, want, joined)
			}
		}
	}
}

// Every refusal prompt_suffix carries, each asserting on the reason and not only
// on the failure. A refusal that does not say what to do instead is a dead end,
// and the constitution asks for the reason by name.
func TestValidatePromptSuffixRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   ModelKind
		value  string
		reason string
	}{
		{
			name: "a speak binding has no system prompt to append to",
			kind: KindSpeak, value: "/no_think",
			reason: "only a think model sends",
		},
		{
			name: "a listen binding has none either",
			kind: KindListen, value: "/no_think",
			reason: "only a think model sends",
		},
		{
			name: "empty is a typo, and absent is how you mean off",
			kind: KindThink, value: "   ",
			reason: "leave the key out to mean off",
		},
		{
			name: "past the bound it stops being a suffix",
			kind: KindThink, value: strings.Repeat("x", PromptSuffixMaxLen+1),
			reason: "not a second prompt",
		},
		{
			name: "a placeholder would be sent with no value",
			kind: KindThink, value: "Answer as {{caller_alias}}.",
			reason: "422",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(promptSuffixErrors("probe", ModelDef{Kind: tc.kind, PromptSuffix: tc.value}), "\n")
			if got == "" {
				t.Fatalf("accepted prompt_suffix %q on a %s model", tc.value, tc.kind)
			}
			if !strings.Contains(got, tc.reason) {
				t.Errorf("the refusal does not say %q:\n%s", tc.reason, got)
			}
		})
	}
	// And the shape that is fine, so the rules above are not simply refusing
	// everything.
	if got := promptSuffixErrors("probe", ModelDef{Kind: KindThink, PromptSuffix: "/no_think"}); got != nil {
		t.Errorf("refused a legal directive: %v", got)
	}
	if got := promptSuffixErrors("probe", ModelDef{Kind: KindSpeak}); got != nil {
		t.Errorf("refused a binding that authors no directive: %v", got)
	}
}

// Off the router it works, so it warns rather than refusing. It is only prompt
// text, and an author may well have meant it; but the reason the field exists is
// a router-served model whose thinking no parameter reaches, so it is worth
// saying out loud.
func TestValidatePromptSuffixWarnsOffTheRouter(t *testing.T) {
	agent := routerAgent(t, func(def *packagespec.ModelDef) {
		def.Provider = "openai"
		def.Model = "gpt-4o"
		def.AgentID = ""
		def.Upstream = nil
		def.Params = nil
		def.PromptSuffix = "/no_think"
	})
	report, err := Validate(agent, []Target{targetFor(agent, ProviderPipecat)}, targetcap.Default())
	if err != nil {
		t.Fatalf("an off-router directive must warn, never fail: %v %#v", err, report.PerTarget[0].Errors)
	}
	warnings := strings.Join(report.PerTarget[0].Warnings, "\n")
	if !strings.Contains(warnings, "prompt_suffix") || !strings.Contains(warnings, "Check you meant this binding") {
		t.Errorf("the warning does not name the field or say what to check:\n%s", warnings)
	}
}

// A per-target override cannot name a different directive, because the prompts it
// would apply to are markdown files every target shares. Refused with the reason,
// rather than resolved to whichever target was walked last.
func TestValidatePromptSuffixCannotDifferPerTarget(t *testing.T) {
	agent := routerAgent(t, func(def *packagespec.ModelDef) {
		def.PromptSuffix = "/no_think"
	})
	// The override arrives already resolved, so this is the shape Build would have
	// produced from a targets.yaml naming a second value.
	resolved := targetFor(agent, ProviderPipecat)
	binding := resolved.Models.Reason["fast_reasoning"]
	binding.PromptSuffix = "/think"
	resolved.Models.Reason["fast_reasoning"] = binding

	report, _ := Validate(agent, []Target{resolved}, targetcap.Default())
	errors := strings.Join(report.PerTarget[0].Errors, "\n")
	if !strings.Contains(errors, "cannot differ per target") {
		t.Errorf("a divergent override was not refused with its reason:\n%s", errors)
	}
	// An override that leaves the field alone is carried forward by Build, so the
	// agreeing case must stay silent. A fresh build, because the mutation above
	// wrote into a map the target shares with the one it came from.
	agreeing := routerAgent(t, func(def *packagespec.ModelDef) {
		def.PromptSuffix = "/no_think"
	})
	report, _ = Validate(agreeing, []Target{targetFor(agreeing, ProviderPipecat)}, targetcap.Default())
	if got := strings.Join(report.PerTarget[0].Errors, "\n"); strings.Contains(got, "prompt_suffix") {
		t.Errorf("refused an override that agrees with the package:\n%s", got)
	}
}

// TestValidatePaceIsATurnFieldWithThreeValues holds both refusals. The legal
// values come from internal/target rather than being retyped here, so a value
// added to the table cannot pass this test while missing from the message.
func TestValidatePaceIsATurnFieldWithThreeValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  ModelKind
		value Pace
		want  string
	}{
		{name: "turn model takes snappy", kind: KindTurn, value: PaceSnappy},
		{name: "turn model takes balanced", kind: KindTurn, value: PaceBalanced},
		{name: "turn model takes patient", kind: KindTurn, value: PacePatient},
		{name: "unset is legal and means balanced", kind: KindTurn, value: ""},
		{name: "unknown value is refused", kind: KindTurn, value: "fast", want: `"fast" is not a pace`},
		{name: "think model does not take it", kind: KindThink, value: PaceSnappy, want: "pace is a turn-model field"},
		{name: "listen model does not take it", kind: KindListen, value: PaceSnappy, want: "pace is a turn-model field"},
		{name: "speak model does not take it", kind: KindSpeak, value: PaceSnappy, want: "pace is a turn-model field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := paceErrors("local_turn", ModelDef{Kind: tc.kind, Pace: tc.value})
			text := strings.Join(got, "\n")
			if tc.want == "" {
				if text != "" {
					t.Fatalf("unexpected errors: %s", text)
				}
				return
			}
			if !strings.Contains(text, tc.want) {
				t.Fatalf("want %q, got %q", tc.want, text)
			}
		})
	}
}

// The refusal has to name every legal value, because "not a pace" tells an
// author they are wrong and not what to write instead (Principle II).
func TestValidatePaceRefusalNamesEveryLegalValue(t *testing.T) {
	text := strings.Join(paceErrors("local_turn", ModelDef{Kind: KindTurn, Pace: "fast"}), "\n")
	for _, value := range targetcap.PaceValues() {
		if !strings.Contains(text, value) {
			t.Errorf("refusal does not name %q: %q", value, text)
		}
	}
}

// TestValidatePaceWithDelayCompilesAndRefusesNothing. Both fields together is
// the recommended configuration, not a mistake: examples/salon-concierge sets
// both because its floor is measured per target and no pace can express that.
//
// There is deliberately no warning. Two were written and both were wrong on that
// example: "you set both" fired on the recommended configuration, and "your
// duration equals the pace floor, delete it" advised deleting a measured
// per-target floor, which drops that target back onto the flat-second cliff.
// What the author needs
// is which value won which half, and the compile report says exactly that on
// every compile. internal/generate holds that gate, because it is the report's
// text rather than a validation outcome.
func TestValidatePaceWithDelayCompilesAndRefusesNothing(t *testing.T) {
	agent := safeAgent(t)
	tgt := targetFor(agent, ProviderLiveKit)
	if tgt.Models.Turn == nil {
		t.Fatal("fixture has no turn binding to configure")
	}
	turn := *tgt.Models.Turn
	turn.Pace = PaceBalanced
	turn.EndpointingDelay = "400ms"
	tgt.Models.Turn = &turn
	agent.Models[agent.Turn] = func() ModelDef {
		m := agent.Models[agent.Turn]
		m.Pace = PaceBalanced
		return m
	}()

	report, err := Validate(agent, []Target{tgt}, targetcap.Default())
	if err != nil {
		t.Fatalf("both fields set must compile: %v", err)
	}
	for _, warning := range reportFor(report, ProviderLiveKit).Warnings {
		if strings.Contains(warning, "pace") {
			t.Errorf("a pace warning fired on the recommended configuration: %q", warning)
		}
	}
}

// TestValidatePaceRefusesAPerTargetOverride. The field's whole point is that one
// word works on both targets. A value that differed per target would be a
// duration in disguise, and endpointing_delay already is one — so a per-target
// pace is refused rather than resolved to whichever won.
func TestValidatePaceRefusesAPerTargetOverride(t *testing.T) {
	agent := safeAgent(t)
	base := agent.Models[agent.Turn]
	base.Pace = PaceBalanced
	agent.Models[agent.Turn] = base

	tgt := targetFor(agent, ProviderLiveKit)
	turn := *tgt.Models.Turn
	turn.Pace = PaceSnappy // as if a target override had named a second value
	tgt.Models.Turn = &turn

	report, err := Validate(agent, []Target{tgt}, targetcap.Default())
	if err == nil {
		t.Fatal("a per-target pace must be refused, not resolved to whichever won")
	}
	text := strings.Join(reportFor(report, ProviderLiveKit).Errors, "\n")
	for _, want := range []string{"snappy", "balanced", "no per-target override", "endpointing_delay"} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal does not name %q: %q", want, text)
		}
	}
}

// TestValidateWarnsOnTurnFieldsThatReachNothing. Three fields were accepted on a
// turn binding in silence: an author could write `params: {alpha: 0.5}`, compile
// clean, and get an agent that ignored it with nothing in the report to say so.
//
// A warning rather than a refusal, because a package carrying one today is not
// broken — it is carrying a value that never did anything, and failing a compile
// that used to pass would be a worse trade than saying so.
func TestValidateWarnsOnTurnFieldsThatReachNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model ModelDef
		// want is a phrase the warning must carry, including what to do instead.
		want []string
	}{
		{
			name:  "params",
			model: ModelDef{Kind: KindTurn, Params: map[string]any{"alpha": 0.5}},
			want:  []string{"alpha", "no target reads", "pace", "endpointing_delay"},
		},
		{
			name:  "agent_id",
			model: ModelDef{Kind: KindTurn, AgentID: "turn-v1"},
			want:  []string{"agent_id", "no target reads", "think binding"},
		},
		{
			name:  "fallback",
			model: ModelDef{Kind: KindTurn, Fallback: []string{"other"}},
			want:  []string{"fallback", "no target reads", "think and listen"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := turnDeadFieldWarnings("detector", tc.model)
			if len(got) != 1 {
				t.Fatalf("want exactly one warning, got %d: %v", len(got), got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got[0], want) {
					t.Errorf("warning does not name %q: %q", want, got[0])
				}
			}
		})
	}
}

// The same fields on any other role are somebody else's business, so this must
// not fire there. params on a think binding is the whole passthrough mechanism.
func TestValidateDoesNotWarnOnTurnFieldsElsewhere(t *testing.T) {
	for _, kind := range []ModelKind{KindThink, KindSpeak, KindListen} {
		model := ModelDef{
			Kind:     kind,
			Params:   map[string]any{"temperature": 0.5},
			AgentID:  "agent-v1",
			Fallback: []string{"other"},
		}
		if got := turnDeadFieldWarnings("m", model); len(got) > 0 {
			t.Errorf("%s binding warned about turn-only dead fields: %v", kind, got)
		}
	}
}

// A turn binding carrying only fields that do reach something stays quiet.
func TestValidateTurnBindingWithLiveFieldsIsSilent(t *testing.T) {
	model := ModelDef{
		Kind: KindTurn, Provider: "local", Model: "silero",
		Pace: PaceBalanced, EndpointingDelay: "400ms",
		SemanticEndpointing: SemanticEndpointingPreferred,
	}
	if got := turnDeadFieldWarnings("detector", model); len(got) > 0 {
		t.Errorf("a turn binding using only live fields warned: %v", got)
	}
}
