package target

type Provider string

const (
	LiveKit    Provider = "livekit"
	Pipecat    Provider = "pipecat"
	Vapi       Provider = "vapi"
	ElevenLabs Provider = "elevenlabs"
	Deepgram   Provider = "deepgram"
)

var Providers = []Provider{LiveKit, Pipecat, Vapi, ElevenLabs, Deepgram}

func IsCode(provider Provider) bool {
	return provider == LiveKit || provider == Pipecat || provider == Deepgram
}

type Tag string

const (
	Core        Tag = "core"
	Warn        Tag = "warn"
	Gated       Tag = "gated"
	Provisional Tag = "provisional"
)

type Field string

const (
	FieldListenLocal           Field = "pipeline.listen.placement.local"
	FieldSpeakLocal            Field = "pipeline.speak.placement.local"
	FieldSpeakEndpoint         Field = "bindings.speak.endpoint_env"
	FieldReasonLocal           Field = "models.placement.local"
	FieldTurnPlacement         Field = "pipeline.turn.placement"
	FieldSemanticEndpointing   Field = "pipeline.turn.semantic_endpointing"
	FieldFallback              Field = "models.fallback"
	FieldListenFallback        Field = "models.listen.fallback"
	FieldTask                  Field = "tasks"
	FieldTaskModel             Field = "tasks.model"
	FieldTaskNestedResult      Field = "tasks.result.nested"
	FieldTaskGroup             Field = "task_groups"
	FieldTaskGroupReturn       Field = "task_groups.then.return"
	FieldContextIsolated       Field = "task_groups.context_scope.isolated"
	FieldTransferRequires      Field = "controls.agent_transfer.requires"
	FieldContextNoToolCalls    Field = "context.include_tool_calls.false"
	FieldContextVariableSubset Field = "context.variables.list"
	FieldBriefingSummary       Field = "controls.human_transfer.briefing.summary"
	FieldBriefingMessage       Field = "controls.human_transfer.briefing.message"
	FieldBriefingWait          Field = "controls.human_transfer.briefing.wait"
	FieldGreetingUserFirst     Field = "conversation.greeting.user"
	FieldGreetingModelWritten  Field = "conversation.greeting.model_written"
	FieldGreetingAbsent        Field = "conversation.greeting.absent"
	FieldInterruptionMinWords  Field = "conversation.interruption.minimum_words"
	FieldInterruptionIgnore    Field = "conversation.interruption.ignore_phrases"
	FieldInactivity            Field = "conversation.inactivity"
	FieldMaxDuration           Field = "conversation.max_duration"
	FieldThinkingAudio         Field = "conversation.thinking_audio"
	FieldToolOutput            Field = "tools.output"
	FieldToolLocal             Field = "tools.execution.local"
	FieldToolMCP               Field = "tools.execution.mcp"
	FieldToolClient            Field = "tools.execution.client"
	FieldToolProviderHosted    Field = "tools.execution.provider_hosted"
	FieldToolBuiltin           Field = "tools.execution.builtin"
	FieldToolInterruption      Field = "tools.interruption.non_default"
	FieldOutbound              Field = "channels.telephony.outbound"
	FieldVoicemail             Field = "channels.telephony.on_voicemail"
	FieldTracingLangfuse       Field = "tracing.provider.langfuse"
	FieldFutureProvisional     Field = "future.provisional"
)

type Capability struct {
	Tag  Tag
	Note string
}

type TelephonyControl string

const (
	ColdTransfer       TelephonyControl = "cold_transfer"
	WarmTransfer       TelephonyControl = "warm_transfer"
	DTMFSend           TelephonyControl = "dtmf_send"
	DTMFReceive        TelephonyControl = "dtmf_receive"
	Hold               TelephonyControl = "hold"
	Hangup             TelephonyControl = "hangup"
	VoicemailDetection TelephonyControl = "voicemail_detection"
	IVRNavigation      TelephonyControl = "ivr_navigation"
)

type ControlCapability struct {
	Capability
	Carrier       string
	Transport     string
	ConditionNote string
}

type ValueCondition struct {
	Value    string
	NonEmpty bool
	Note     string
}

type Role string

const (
	Listen Role = "listen"
	Turn   Role = "turn"
	Speak  Role = "speak"
	Reason Role = "reason"
)

type RoleKind string

const (
	Open       RoleKind = "open"
	Integrated RoleKind = "integrated"
)

type History string

const (
	HistoryFull     History = "full"
	HistoryMessages History = "messages"
	HistoryLastN    History = "last_n"
	HistorySummary  History = "summary"
	HistoryReset    History = "reset"
)

type HistoryKind string

const (
	HistoryOK        HistoryKind = "ok"
	HistoryFail      HistoryKind = "fail"
	HistoryGenerated HistoryKind = "generated"
)

type HistorySupport struct {
	Kind HistoryKind
	Note string
}

type FallbackSlot string

const (
	FallbackComponent    FallbackSlot = "component"
	FallbackGenerated    FallbackSlot = "generated"
	FallbackSameProvider FallbackSlot = "same_provider_model"
	FallbackModelID      FallbackSlot = "model_id"
	FallbackProvider     FallbackSlot = "provider_entry"
)

// Per-role vendor allowlists live in the provider catalogue (catalog_*.go);
// validation checks bindings with Catalog.CheckVendor, not this table.
type Table struct {
	Fields        map[Field]map[Provider]Capability
	Controls      map[TelephonyControl]map[Provider]ControlCapability
	Conditions    map[Field]map[Provider]ValueCondition
	Roles         map[Role]map[Provider]RoleKind
	History       map[History]map[Provider]HistorySupport
	FallbackSlots map[Provider]FallbackSlot
}

func (t Table) Capability(field Field, provider Provider) Capability {
	return t.Fields[field][provider]
}

func (t Table) Control(control TelephonyControl, provider Provider, transport, carrier string) Capability {
	support := t.Controls[control][provider]
	if support.Tag == Gated || support.Tag == Provisional {
		return support.Capability
	}
	conditionFailed := false
	switch {
	case support.Carrier != "" && support.Transport != "":
		conditionFailed = support.Carrier != carrier || support.Transport != transport
	case support.Carrier != "":
		conditionFailed = support.Carrier != carrier
	case support.Transport != "":
		conditionFailed = support.Transport != transport
	}
	if conditionFailed {
		return Capability{Tag: Gated, Note: support.ConditionNote}
	}
	return support.Capability
}

func (t Table) CapabilityForValue(field Field, provider Provider, value string) Capability {
	capability := t.Capability(field, provider)
	condition := t.Conditions[field][provider]
	conditionFailed := condition.Value != "" && condition.Value != value || condition.NonEmpty && value == ""
	if (capability.Tag == Core || capability.Tag == Warn) && conditionFailed {
		return Capability{Tag: Gated, Note: condition.Note}
	}
	return capability
}

func (t Table) Role(role Role, provider Provider) RoleKind {
	return t.Roles[role][provider]
}

func (t Table) HistorySupport(history History, provider Provider) HistorySupport {
	return t.History[history][provider]
}

func Default() Table {
	return Table{
		Fields: map[Field]map[Provider]Capability{
			FieldListenLocal: field(
				deny(Vapi, "Vapi cannot run a local listen model"),
				deny(ElevenLabs, "ElevenLabs uses its integrated ASR"),
				deny(Deepgram, "Deepgram has no slot for an outside STT model"),
			),
			FieldSpeakLocal: field(
				deny(Vapi, "Vapi cannot run a local voice model"),
				deny(ElevenLabs, "ElevenLabs cannot run a local voice model"),
				deny(Deepgram, "Deepgram cannot run a local voice model"),
			),
			FieldSpeakEndpoint: field(
				deny(LiveKit, "LiveKit has no OpenAI-compatible speak wildcard: its openai plugin TTS carries no language slot (N14), so a custom speak endpoint cannot be catalogued"),
				deny(ElevenLabs, "ElevenLabs custom speak endpoints have no slot"),
			),
			FieldReasonLocal: field(deny(Vapi, "Vapi custom local LLM endpoints are unverified")),
			FieldTurnPlacement: field(
				warn(LiveKit, "LiveKit turn placement is a preference"),
				warn(Vapi, "Vapi integrates turn detection; placement is ignored"),
				warn(ElevenLabs, "ElevenLabs integrates turn detection; placement is ignored"),
				warn(Deepgram, "Deepgram integrates turn detection into listen; placement is ignored"),
			),
			FieldSemanticEndpointing: field(
				warn(LiveKit, "LiveKit semantic endpointing depends on the bound model"),
				warn(Pipecat, "Pipecat semantic endpointing depends on the bound model"),
				warn(Vapi, "Vapi semantic endpointing is forwarded as a preference"),
				warn(ElevenLabs, "ElevenLabs semantic endpointing is forwarded as a preference"),
				warn(Deepgram, "Deepgram semantic endpointing depends on the bound listen model"),
			),
			FieldFallback: field(deny(Pipecat, "the Pipecat driver does not emit generated fallback yet")),
			// Listen (STT) fallback: native on LiveKit (stt.FallbackAdapter,
			// verified in livekit-agents source 2026-07-19). Gated elsewhere
			// until a slot is verified: no documented transcriber fallback on
			// Vapi, listen is integrated on ElevenLabs, and Deepgram's
			// agent.listen takes a single provider (unlike think's array).
			FieldListenFallback: field(
				deny(Pipecat, "the Pipecat driver does not emit listen fallback yet"),
				deny(Vapi, "Vapi has no documented transcriber fallback slot"),
				deny(ElevenLabs, "ElevenLabs listen is integrated; there is no STT fallback slot"),
				deny(Deepgram, "Deepgram agent.listen takes a single provider; there is no fallback slot"),
			),
			FieldTask: field(
				deny(Vapi, "Vapi return-to-prior-assistant is unverified"),
				warn(ElevenLabs, "ElevenLabs keeps task turns in the owner's running transcript"),
			),
			// Verified 2026-07-16: an LLMSwitcher inside an LLMWorker pipeline
			// stalls all flow frames on pipecat-ai 1.5.0, so per-task model has
			// no working lowering there yet (driver-pipecat B7 spike).
			FieldTaskModel: field(deny(Pipecat, "the Pipecat driver does not emit per-task model yet (LLMSwitcher stalls inside an LLMWorker)")),
			FieldTaskNestedResult: field(
				deny(Vapi, "Vapi cannot enforce nested task results"),
				deny(ElevenLabs, "ElevenLabs cannot enforce nested task results"),
			),
			FieldTaskGroup: field(warn(LiveKit, "LiveKit TaskGroup is experimental")),
			FieldTaskGroupReturn: field(
				deny(Vapi, "Vapi state-preserving Squad return is unverified"),
				warn(ElevenLabs, "ElevenLabs keeps task-group turns in the owner's running transcript"),
			),
			FieldContextIsolated: field(
				deny(Vapi, "Vapi cannot isolate task-group context"),
				deny(ElevenLabs, "ElevenLabs keeps one running transcript"),
			),
			FieldTransferRequires: field(
				deny(Vapi, "Vapi has no machine-checked transfer guard"),
				deny(ElevenLabs, "ElevenLabs has no machine-checked transfer guard"),
			),
			FieldContextNoToolCalls: field(
				deny(Pipecat, "the Pipecat driver does not shape transfer context (include_tool_calls) yet"),
				deny(Vapi, "Vapi cannot exclude tool calls from transfer context"),
				deny(ElevenLabs, "ElevenLabs always keeps the full transcript"),
			),
			FieldContextVariableSubset: field(
				deny(Pipecat, "the Pipecat driver does not shape transfer context (variables subset) yet"),
				deny(Vapi, "Vapi accepts transfer variables: all only"),
				deny(ElevenLabs, "ElevenLabs accepts transfer variables: all only"),
			),
			FieldBriefingSummary: field(
				deny(Pipecat, "Pipecat has no summary briefing lowering"),
				deny(ElevenLabs, "ElevenLabs supports message briefing only"),
				deny(Deepgram, "Deepgram has no summary briefing lowering"),
			),
			FieldBriefingMessage: field(
				deny(LiveKit, "LiveKit supports summary briefing only"),
				deny(Pipecat, "Pipecat has no message briefing lowering"),
				deny(Deepgram, "Deepgram has no message briefing lowering"),
			),
			FieldBriefingWait: field(
				deny(LiveKit, "LiveKit has no wait briefing lowering"),
				deny(Pipecat, "Pipecat has no wait briefing lowering"),
				deny(ElevenLabs, "ElevenLabs has no wait briefing lowering"),
				deny(Deepgram, "Deepgram has no wait briefing lowering"),
			),
			FieldGreetingUserFirst: field(
				warn(Deepgram, "Deepgram silence for an omitted greeting is undocumented"),
			),
			FieldGreetingModelWritten: field(
				warn(Deepgram, "Deepgram generates the opening with a synthetic turn"),
			),
			FieldGreetingAbsent: field(
				warn(LiveKit, "LiveKit default greeting behavior applies"),
				warn(Pipecat, "Pipecat default greeting behavior applies"),
				warn(Vapi, "Vapi default greeting behavior applies"),
				warn(ElevenLabs, "ElevenLabs default greeting behavior applies"),
				warn(Deepgram, "Deepgram default greeting behavior applies"),
			),
			FieldInterruptionMinWords: field(
				warn(ElevenLabs, "ElevenLabs has no minimum-word interruption knob"),
				warn(Deepgram, "Deepgram interruption minimum words is lossy"),
			),
			FieldInterruptionIgnore: field(
				warn(Deepgram, "Deepgram drops interruption ignore phrases"),
			),
			FieldInactivity: field(
				warn(LiveKit, "LiveKit driver must range-check inactivity durations"),
				warn(Pipecat, "Pipecat driver must range-check inactivity durations"),
				warn(Vapi, "Vapi driver must range-check inactivity durations"),
				warn(ElevenLabs, "ElevenLabs driver must range-check inactivity durations"),
				warn(Deepgram, "Deepgram driver must range-check inactivity durations"),
			),
			FieldMaxDuration: field(
				warn(LiveKit, "LiveKit driver must verify a max-duration cap"),
				warn(Pipecat, "Pipecat driver must verify a max-duration cap"),
				warn(Vapi, "Vapi driver must verify a max-duration cap"),
				warn(ElevenLabs, "ElevenLabs driver must verify a max-duration cap"),
				warn(Deepgram, "Deepgram driver must verify a max-duration cap"),
			),
			FieldThinkingAudio: field(
				deny(Pipecat, "the Pipecat driver does not emit thinking audio yet"),
				deny(Vapi, "Vapi has no faithful thinking-audio lowering"),
				deny(Deepgram, "Deepgram has no faithful thinking-audio lowering"),
			),
			FieldToolOutput: field(
				warn(Vapi, "Vapi cannot enforce tool output schemas"),
				warn(ElevenLabs, "ElevenLabs cannot enforce tool output schemas"),
			),
			FieldToolLocal: field(
				deny(Vapi, "Vapi cannot host local tool code"),
				deny(ElevenLabs, "ElevenLabs cannot host local tool code"),
			),
			FieldToolMCP: field(
				deny(Pipecat, "the Pipecat driver does not emit MCP tools yet"),
				deny(Deepgram, "Deepgram has no runtime MCP client"),
			),
			FieldToolClient: field(
				deny(LiveKit, "LiveKit client tools are not proven by its driver"),
				deny(Pipecat, "Pipecat client tools are not proven by its driver"),
				deny(Vapi, "Vapi client tools are not proven by its driver"),
				deny(ElevenLabs, "ElevenLabs client tools are not proven by its driver"),
				deny(Deepgram, "Deepgram client tools are not proven by its driver"),
			),
			FieldToolProviderHosted: field(
				deny(LiveKit, "LiveKit provider-hosted tools are not proven by its driver"),
				deny(Pipecat, "Pipecat provider-hosted tools are not proven by its driver"),
				deny(Vapi, "Vapi provider-hosted tools are not proven by its driver"),
				deny(ElevenLabs, "ElevenLabs provider-hosted tools are not proven by its driver"),
				deny(Deepgram, "Deepgram provider-hosted tools are not proven by its driver"),
			),
			FieldToolBuiltin: field(
				deny(LiveKit, "LiveKit builtin tools are not proven by its driver"),
				deny(Pipecat, "Pipecat builtin tools are not proven by its driver"),
				deny(Vapi, "Vapi builtin tools are not proven by its driver"),
				deny(ElevenLabs, "ElevenLabs builtin tools are not proven by its driver"),
				deny(Deepgram, "Deepgram builtin tools are not proven by its driver"),
			),
			FieldToolInterruption: field(
				warn(LiveKit, "LiveKit runs tool executions to completion; a per-tool interruption preference is not enforced"),
				warn(Vapi, "Vapi uses provider-default tool interruption"),
				warn(ElevenLabs, "ElevenLabs uses provider-default tool interruption"),
			),
			FieldOutbound: field(
				deny(Pipecat, "the Pipecat driver does not emit outbound calling yet"),
				warn(Deepgram, "Deepgram outbound calling uses carrier-conditional generated AMD"),
			),
			FieldVoicemail: field(
				deny(Pipecat, "the Pipecat driver does not emit voicemail handling yet"),
				warn(Deepgram, "Deepgram voicemail handling uses carrier-conditional generated AMD"),
			),
			FieldTracingLangfuse: field(
				deny(Vapi, "Vapi has no Langfuse tracing lowering"),
				deny(ElevenLabs, "ElevenLabs has no Langfuse tracing lowering"),
				deny(Deepgram, "the Deepgram driver does not emit Langfuse tracing"),
			),
			FieldFutureProvisional: provisional(),
		},
		Controls: map[TelephonyControl]map[Provider]ControlCapability{
			ColdTransfer: controls(
				control(),
				controlTransport("daily-sip", "Pipecat cold transfer requires Daily SIP transport"),
				control(),
				control(),
				controlNamedCarrier("twilio", "Deepgram transfer requires carrier Twilio in the generated bridge"),
			),
			WarmTransfer: controls(
				control(),
				controlDeny("Pipecat warm transfer ships upstream but this driver does not emit it yet"),
				controlNamedCarrier("twilio", "Vapi warm transfer requires carrier Twilio"),
				control(),
				controlNamedCarrier("twilio", "Deepgram transfer requires carrier Twilio in the generated bridge"),
			),
			DTMFSend:           routedControls("dtmf_send"),
			DTMFReceive:        routedControls("dtmf_receive"),
			Hold:               routedControls("hold"),
			Hangup:             controls(control(), control(), control(), control(), control()),
			VoicemailDetection: controls(control(), control(), control(), control(), controlNamedCarrier("twilio", "Deepgram voicemail detection requires carrier Twilio AMD in the generated bridge")),
			IVRNavigation:      routedControls("ivr_navigation"),
		},
		Conditions: map[Field]map[Provider]ValueCondition{
			FieldToolMCP: {
				LiveKit: {Value: "python", Note: "LiveKit MCP tools require sdk_language: python"},
			},
			FieldReasonLocal: {
				ElevenLabs: {NonEmpty: true, Note: "ElevenLabs local reason models require a custom endpoint"},
			},
			// V8: a warm-transfer briefing message is read to the operator only for
			// numbers imported via the native Twilio integration; SIP transfers do
			// not support it (verified against ElevenLabs docs 2026-07-15).
			FieldBriefingMessage: {
				ElevenLabs: {Value: "twilio", Note: "ElevenLabs warm-transfer briefing messages require carrier twilio (native Twilio integration); SIP transfers do not support them"},
			},
		},
		Roles: map[Role]map[Provider]RoleKind{
			Listen: role(Open, Open, Open, Integrated, Open),
			Turn:   role(Open, Open, Integrated, Integrated, Integrated),
			Speak:  role(Open, Open, Open, Open, Open),
			Reason: role(Open, Open, Open, Open, Open),
		},
		History: map[History]map[Provider]HistorySupport{
			// Pipecat driver v1 emits history: full only; other values are a
			// maturity gate (the workers handoff carries the running context and
			// fine-grained shaping is not emitted yet, C9).
			HistoryFull:     history(HistoryOK, HistoryOK, HistoryOK, HistoryOK, HistoryOK),
			HistoryMessages: history(HistoryOK, HistoryFail, HistoryOK, HistoryFail, HistoryOK),
			HistoryLastN:    history(HistoryOK, HistoryFail, HistoryOK, HistoryFail, HistoryOK),
			HistorySummary:  history(HistoryGenerated, HistoryFail, HistoryFail, HistoryFail, HistoryGenerated),
			HistoryReset:    history(HistoryOK, HistoryFail, HistoryOK, HistoryFail, HistoryOK),
		},
		FallbackSlots: map[Provider]FallbackSlot{
			LiveKit: FallbackComponent, Pipecat: FallbackGenerated, Vapi: FallbackSameProvider,
			ElevenLabs: FallbackModelID, Deepgram: FallbackProvider,
		},
	}
}

func control() ControlCapability {
	return ControlCapability{Capability: Capability{Tag: Core}}
}

func controlDeny(note string) ControlCapability {
	return ControlCapability{Capability: Capability{Tag: Gated, Note: note}}
}

func controlTransport(transport, note string) ControlCapability {
	return ControlCapability{Capability: Capability{Tag: Core}, Transport: transport, ConditionNote: note}
}

func controlNamedCarrier(carrier, note string) ControlCapability {
	return ControlCapability{Capability: Capability{Tag: Core}, Carrier: carrier, ConditionNote: note}
}

func routedControls(name string) map[Provider]ControlCapability {
	note := "required control " + name + " is proven only for the exact carrier Twilio and transport Daily SIP route"
	value := ControlCapability{Capability: Capability{Tag: Core}, Carrier: "twilio", Transport: "daily-sip", ConditionNote: note}
	return controls(value, value, value, value, value)
}

func controls(livekit, pipecat, vapi, elevenlabs, deepgram ControlCapability) map[Provider]ControlCapability {
	return map[Provider]ControlCapability{
		LiveKit: livekit, Pipecat: pipecat, Vapi: vapi, ElevenLabs: elevenlabs, Deepgram: deepgram,
	}
}

type override struct {
	provider Provider
	value    Capability
}

func deny(provider Provider, note string) override {
	return override{provider, Capability{Tag: Gated, Note: note}}
}

func warn(provider Provider, note string) override {
	return override{provider, Capability{Tag: Warn, Note: note}}
}

func field(overrides ...override) map[Provider]Capability {
	values := make(map[Provider]Capability, len(Providers))
	for _, provider := range Providers {
		values[provider] = Capability{Tag: Core}
	}
	for _, override := range overrides {
		values[override.provider] = override.value
	}
	return values
}

func provisional() map[Provider]Capability {
	values := make(map[Provider]Capability, len(Providers))
	for _, provider := range Providers {
		values[provider] = Capability{Tag: Provisional, Note: "field is provisional on every target"}
	}
	return values
}

func role(livekit, pipecat, vapi, elevenlabs, deepgram RoleKind) map[Provider]RoleKind {
	return map[Provider]RoleKind{
		LiveKit: livekit, Pipecat: pipecat, Vapi: vapi, ElevenLabs: elevenlabs, Deepgram: deepgram,
	}
}

func history(livekit, pipecat, vapi, elevenlabs, deepgram HistoryKind) map[Provider]HistorySupport {
	values := map[Provider]HistorySupport{
		LiveKit: {Kind: livekit}, Pipecat: {Kind: pipecat}, Vapi: {Kind: vapi},
		ElevenLabs: {Kind: elevenlabs}, Deepgram: {Kind: deepgram},
	}
	if pipecat == HistoryFail {
		value := values[Pipecat]
		value.Note = "the Pipecat driver emits history: full only; other values are not shaped yet"
		values[Pipecat] = value
	}
	if elevenlabs == HistoryFail {
		value := values[ElevenLabs]
		value.Note = "ElevenLabs always keeps the full transcript"
		values[ElevenLabs] = value
	}
	if vapi == HistoryFail {
		value := values[Vapi]
		value.Note = "Vapi has no summary context mode"
		values[Vapi] = value
	}
	return values
}
