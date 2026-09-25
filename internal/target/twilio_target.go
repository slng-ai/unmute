package target

import (
	"fmt"
	"slices"
	"strings"
)

// The Twilio *target*'s own facts: a generated Python web app that Twilio
// ConversationRelay calls. Twilio is also a carrier on the LiveKit and Pipecat
// routes, which is why every diagnostic this target produces opens with
// "twilio target", the same way the slng target's do.
//
// One home for the platform facts a package is checked against, so the driver
// in internal/generate and ir.Validate cannot disagree about them.
//
// Official contracts, read 2026-09-25:
//   - https://www.twilio.com/docs/voice/twiml/connect/conversationrelay
//   - https://www.twilio.com/docs/voice/conversationrelay/websocket-messages
//   - https://www.twilio.com/docs/usage/security

// TwilioTargetVerified is when the facts below were last read against the
// official docs and the pinned packages. Re-read before relying on any of them.
const TwilioTargetVerified = "2026-09-25"

// TwilioTransport is the connection transport this target serves. It is the
// only transport the target takes, and the carrier is always twilio.
const TwilioTransport = "conversation-relay"

// TwilioRegions are the Twilio Regions a connection's `region:` may name,
// default first. ConversationRelay is documented in all three. Each region
// keeps its own copy of a number's voice configuration and its own Auth Token,
// and only calls routed to that region use it:
// https://www.twilio.com/docs/global-infrastructure/understanding-twilio-regions
var TwilioRegions = []string{"us1", "ie1", "au1"}

// TwilioPython is the interpreter line the emitted project and its container
// use. Session 1 measured the providers on CPython 3.12.13.
const TwilioPython = "3.12"

// TwilioPins are the exact versions the emitted project installs. They are
// internal pins and not an authoring surface: a package cannot move them, and
// `pins:` on a twilio target is refused. aiohttp is already a dependency of the
// twilio package; it is named because app.py imports it directly.
//
// The two model SDKs are listed here, but a project installs only the one its
// think binding selects.
var TwilioPins = map[string]string{
	"aiohttp":      "3.14.3",
	"google-genai": "2.25.0",
	"openai":       "3.19.2",
	"pydantic":     "2.13.5",
	"twilio":       "9.11.1",
}

// TwilioRuntimeDeps is the dependency set every twilio project installs, before
// the one model SDK.
var TwilioRuntimeDeps = []string{"aiohttp", "pydantic", "twilio"}

// TwilioModelSDK names the package a think vendor needs.
func TwilioModelSDK(vendor string) string {
	if vendor == "google" || vendor == "gemini" {
		return "google-genai"
	}
	return "openai"
}

// TwilioThinkEvidence is a think path run end to end against the real API with
// scripts/text_run_twilio.py --real: a streamed reply, a local tool follow-up,
// and the next turn after an interrupt. Path is "chat", "developer" or the
// Vertex location. A binding off this list compiles, and its compile report
// says it has no real-model evidence.
type TwilioThinkEvidence struct {
	Vendor, Model, Path, Date string
}

// TwilioVerifiedThink is every think path with that evidence.
var TwilioVerifiedThink = []TwilioThinkEvidence{
	{Vendor: "openai", Model: "gpt-5.6-luna", Path: "chat", Date: "2026-09-25"},
	{Vendor: "google", Model: "gemini-3.1-flash-lite", Path: "eu", Date: "2026-09-25"},
}

// The runtime limits are internal tested defaults, not authored fields.
const (
	// TwilioMaxToolRounds bounds the model and tool round trips in one caller
	// turn.
	TwilioMaxToolRounds = 8
	// TwilioToolDeadlineSeconds bounds how long a turn waits for one tool. The
	// handler is never cancelled: after the deadline its outcome is unknown.
	TwilioToolDeadlineSeconds = 30
)

// TwilioSpeechProviders maps a catalogue vendor to the value ConversationRelay
// takes in transcriptionProvider or ttsProvider. Other providers exist in
// ConversationRelay; they are outside this slice, not refused by Twilio.
var TwilioSpeechProviders = map[Role]map[string]string{
	Listen: {"deepgram": "Deepgram"},
	Speak:  {"elevenlabs": "ElevenLabs"},
}

// TwilioTurnParam is one ConversationRelay turn setting an author may write
// under a settings-only models.turn entry. The name is the TwiML attribute.
type TwilioTurnParam struct {
	Name   string
	Kind   string // "int", "enum" or "bool"
	Min    int
	Max    int
	Values []string
}

// TwilioTurnParams is every turn setting this target forwards. Anything else is
// refused, because a setting that reaches nothing changes nothing the author
// can hear. Ranges are the documented ones.
var TwilioTurnParams = []TwilioTurnParam{
	{Name: "speechTimeout", Kind: "int", Min: 600, Max: 5000},
	{Name: "interruptSensitivity", Kind: "enum", Values: []string{"high", "medium", "low"}},
	{Name: "ignoreBackchannel", Kind: "bool"},
}

// CheckTwilioTurnParam checks one authored turn setting against its rule.
func CheckTwilioTurnParam(name string, value any) error {
	i := slices.IndexFunc(TwilioTurnParams, func(p TwilioTurnParam) bool { return p.Name == name })
	if i < 0 {
		known := make([]string, 0, len(TwilioTurnParams))
		for _, p := range TwilioTurnParams {
			known = append(known, p.Name)
		}
		return fmt.Errorf("twilio target has no turn setting %q: write one of %s, or remove it", name, strings.Join(known, ", "))
	}
	param := TwilioTurnParams[i]
	switch param.Kind {
	case "int":
		n, ok := wholeNumber(value)
		if !ok || n < param.Min || n > param.Max {
			return fmt.Errorf("twilio target turn setting %s is %v: write a whole number of milliseconds from %d to %d, or remove it", name, value, param.Min, param.Max)
		}
	case "enum":
		s, ok := value.(string)
		if !ok || !slices.Contains(param.Values, s) {
			return fmt.Errorf("twilio target turn setting %s is %v: write one of %s", name, value, strings.Join(param.Values, ", "))
		}
	case "bool":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("twilio target turn setting %s is %v: write true or false", name, value)
		}
	}
	return nil
}

// wholeNumber reads a YAML number as an int only when it has no fraction.
func wholeNumber(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case uint64:
		return int(v), true
	case float64:
		if v == float64(int(v)) {
			return int(v), true
		}
	}
	return 0, false
}

// TwilioDocLinks are the official pages each emitted step links to.
type TwilioDocLinks struct {
	ConversationRelay string
	Messages          string
	Security          string
	Connect           string
	Hangup            string
	Onboarding        string
	Numbers           string
	Regions           string
}

// TwilioDocs is the one copy of those links.
var TwilioDocs = TwilioDocLinks{
	ConversationRelay: "https://www.twilio.com/docs/voice/twiml/connect/conversationrelay",
	Messages:          "https://www.twilio.com/docs/voice/conversationrelay/websocket-messages",
	Security:          "https://www.twilio.com/docs/usage/security",
	Connect:           "https://www.twilio.com/docs/voice/twiml/connect",
	Hangup:            "https://www.twilio.com/docs/voice/twiml/hangup",
	Onboarding:        "https://www.twilio.com/docs/voice/conversationrelay/onboarding",
	Numbers:           "https://www.twilio.com/docs/phone-numbers",
	Regions:           "https://www.twilio.com/docs/global-infrastructure/understanding-twilio-regions",
}

// twilioOnlyOne is the reason shared by every row that needs a second agent,
// a task or a crossing between them.
func twilioOnlyOne(what string) string {
	return "twilio target runs one agent with one prompt, so " + what + " has nowhere to go: fold the step into the agent's instructions, or compile to livekit or pipecat which emit multi-task agents"
}

// twilioSpeech is the reason shared by every turn and speech-timing row:
// ConversationRelay does the listening and speaking, and the app sees text.
func twilioSpeech(what string) string {
	return "twilio target hands speech and turn taking to ConversationRelay, which has no slot for " + what + ": remove it (the settings ConversationRelay takes go on a settings-only models.turn entry), or compile to livekit or pipecat"
}

func twilioNoTool(what string) string {
	return "twilio target runs only local handlers and the builtin end_call, so " + what + " is not emitted: move the work into a local: tool, or compile to livekit or pipecat"
}

// twilioFields is every capability row's answer for the twilio target, in one
// place. field() does not seed this provider, so a row missing here fails
// TestDefaultTableIsCompleteAndTyped: a new field is a decision somebody makes
// for this target, never a silent grant.
func twilioFields() map[Field]Capability {
	no := func(note string) Capability { return Capability{Tag: Gated, Note: note} }
	yes := Capability{Tag: Core}
	return map[Field]Capability{
		FieldListenLocal:         no("twilio target has ConversationRelay transcribe the call on Twilio's side, so a locally placed listen model has no machine to run on: drop the placement, or compile to livekit or pipecat"),
		FieldSpeakLocal:          no("twilio target has ConversationRelay speak on Twilio's side, so a locally placed speak model has no machine to run on: drop the placement, or compile to livekit or pipecat"),
		FieldSpeakEndpoint:       no("twilio target has ConversationRelay speak with its own providers, so a custom speak endpoint reaches nothing: bind elevenlabs, or compile to pipecat which emits the endpoint_env lookup"),
		FieldReasonLocal:         no("twilio target calls OpenAI or Gemini directly and forwards no custom endpoint: bind provider openai or google, or compile to livekit or pipecat"),
		FieldTurnPlacement:       yes, // a settings-only entry; identity is refused in ir
		FieldSemanticEndpointing: no(twilioSpeech("a semantic endpointing choice")),
		FieldEndpointingDelay:    no(twilioSpeech("an endpointing delay")),
		FieldPace:                no(twilioSpeech("a pace")),
		FieldTurnByListener:      no(twilioSpeech("turn provider listen")),
		FieldTurnEager:           no(twilioSpeech("eager")),
		FieldLiveModel:           no("twilio target has ConversationRelay listen and speak and the app think, so one model doing all three has no slot: write architecture: cascade"),
		FieldRealtimeModel:       no("twilio target has ConversationRelay listen and speak and the app think, so architecture: realtime has no slot: write architecture: cascade"),
		FieldRealtimeTasks:       no("twilio target has no slot for architecture: realtime at all: write architecture: cascade"),
		FieldFallback:            no("twilio target calls one think model and emits no fallback chain: remove fallback, or compile to livekit which emits one"),
		FieldListenFallback:      no("twilio target has ConversationRelay transcribe with one provider and no fallback: remove the listen fallback, or compile to livekit"),
		FieldTask:                no(twilioOnlyOne("a task")),
		FieldTaskModel:           no(twilioOnlyOne("a per-task model")),
		FieldTaskNestedResult:    no(twilioOnlyOne("a nested task result")),
		FieldTaskFinish:          no(twilioOnlyOne("a step that ends on its tool")),
		FieldTaskOpening:         no(twilioOnlyOne("a step opening")),
		FieldTaskGroup:           no(twilioOnlyOne("a task group")),
		FieldTaskGroupReturn:     no(twilioOnlyOne("a task group return step")),
		FieldGroupSkip:           no(twilioOnlyOne("a skippable group step")),
		FieldContextIsolated:     no(twilioOnlyOne("an isolated task context")),
		FieldTransferAnnounce:    no(twilioOnlyOne("a transfer announcement")),
		FieldContextNoToolCalls:  no(twilioOnlyOne("include_tool_calls: false")),
		FieldTransferBriefing:    no("twilio target emits no transfer, so a briefing has nothing to brief: remove it, or compile to livekit on a sip route"),
		FieldDelegateAnnounce:    no(twilioOnlyOne("a step announcement")),
		// The two greeting shapes ConversationRelay can say: a fixed line in
		// welcomeGreeting, or nothing while the caller speaks first.
		FieldGreetingUserFirst:    yes,
		FieldGreetingModelWritten: no("twilio target speaks a fixed welcomeGreeting and has no model-written opening yet: write conversation.greeting.text, or set speaks_first: user"),
		FieldGreetingAbsent:       no("twilio target needs an explicit opening: add conversation.greeting with a text, or with speaks_first: user"),
		FieldInterruptionMinWords: no(twilioSpeech("a minimum word count")),
		FieldInterruptionIgnore:   no(twilioSpeech("a phrase list")),
		FieldInterruptionProtect:  yes, // greeting only; ir refuses tool_calls
		FieldInactivity:           no("twilio target emits no inactivity timer yet: remove conversation.inactivity, or compile to livekit or pipecat which take the durations"),
		FieldMaxDuration:          no("twilio target emits no call length cap yet: cap the call with Twilio's own timeLimit outside this package, or compile to livekit or pipecat"),
		FieldThinkingAudio:        no("twilio target has ConversationRelay play audio and has no thinking sound: remove conversation.thinking_audio, or compile to livekit"),
		FieldToolOutput:           yes,
		FieldToolLocal:            yes,
		FieldToolMCP:              no(twilioNoTool("an MCP tool source")),
		FieldToolMCPTask:          no(twilioNoTool("an MCP tool source")),
		FieldToolClient:           no(twilioNoTool("a client tool")),
		FieldToolProviderHosted:   no(twilioNoTool("a provider-hosted tool")),
		FieldToolBuiltin:          yes, // end_call only, held by ir
		FieldToolSlngHosted:       no(twilioNoTool("a tool SLNG hosts")),
		FieldToolKnowledge:        no(twilioNoTool("a knowledge base")),
		FieldToolKnowledgeTask:    no(twilioNoTool("a knowledge base")),
		FieldToolAuth:             no(twilioNoTool("a webhook's auth block")),
		FieldToolInterruption:     no("twilio target runs every tool to completion and never cancels one, so interruption: cancel is not honoured: remove it or write continue, or compile to pipecat which cancels"),
		FieldToolAnnounce:         no("twilio target speaks no announcement before a tool yet: remove announce and let the model say a short line before the call, or compile to livekit or pipecat"),
		// Core for the reason the slng row gives: FieldTask already refuses
		// the task, so a second message would name a consequence, not the cause.
		FieldToolAnnounceTask:      yes,
		FieldToolInject:            no("twilio target has no session values to inject into a tool: remove inject, or compile to livekit or pipecat"),
		FieldWebhookPath:           no(twilioNoTool("a webhook path")),
		FieldToolDependencies:      yes,
		FieldOutbound:              no("twilio target answers inbound calls only: set outbound: false, or compile to livekit which dials from the agent"),
		FieldVoicemail:             no("twilio target answers inbound calls only and handles no voicemail: remove on_voicemail, or compile to livekit"),
		FieldDeploymentMultiRegion: no("twilio target is one process you host, with no deployment region: remove deployment_region and run it where you choose"),
		FieldWarmInstances:         no("twilio target is one process you host, so there is no pool to keep warm: remove warm_instances"),
		FieldTracingLangfuse:       no("twilio target emits no trace exporter yet: remove tracing, or compile to livekit or pipecat which emit it"),
		FieldTracingCoval:          no("twilio target emits no trace exporter yet: remove tracing, or compile to livekit or pipecat which emit it"),
		FieldPrefetch:              no("twilio target has no session state, so a prefetch has nowhere to put its value: fold the value into the instructions, or compile to livekit or pipecat"),
		FieldVariableConfirm:       no(twilioOnlyOne("a value awaiting confirmation")),
		FieldVariableConversation:  no("twilio target has no session state for a model to record into: remove the variable, or compile to slng"),
		FieldTemplates:             no("twilio target renders no session-start values into the prompt: remove the {{placeholder}}, or compile to livekit or pipecat"),
		FieldTypedState:            no("twilio target has no session state, so a declared shape has nothing to check: remove the variable, or compile to livekit or pipecat"),
		FieldShapedText:            no("twilio target has no session state, so a shaped value has nothing to check: remove the variable, or compile to livekit or pipecat"),
	}
}

// addTwilio writes the twilio answer into every provider-keyed structure of a
// table built for the other three.
func addTwilio(t Table) Table {
	for field, capability := range twilioFields() {
		if t.Fields[field] == nil {
			continue // a row that does not exist is caught by the completeness test
		}
		t.Fields[field][Twilio] = capability
	}
	for name, byProvider := range t.Controls {
		if name == Hangup {
			byProvider[Twilio] = control()
			continue
		}
		byProvider[Twilio] = controlDeny("twilio target answers inbound calls and ends them, and emits no " + string(name) + ": remove the required control, or compile to livekit or pipecat on a route that carries it")
	}
	t.Roles[Listen][Twilio] = Open
	t.Roles[Speak][Twilio] = Open
	t.Roles[Reason][Twilio] = Open
	// Integrated: ConversationRelay decides the turn, so the entry carries its
	// settings and never a model.
	t.Roles[Turn][Twilio] = Integrated
	for history, byProvider := range t.History {
		byProvider[Twilio] = HistorySupport{Kind: HistoryFail, Note: twilioOnlyOne("a context history")}
		t.History[history] = byProvider
	}
	// Fallback is denied above, so the slot kind is never read for a chain; it
	// has to exist because validateFallbacks reads it on every package.
	t.FallbackSlots[Twilio] = FallbackComponent
	return t
}
