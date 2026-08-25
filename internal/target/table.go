package target

type Provider string

const (
	LiveKit Provider = "livekit"
	Pipecat Provider = "pipecat"
)

// Providers is every target a package may name. A provider earns a place here
// by having a driver that emits a runnable project, not before: `vapi` and
// `deepgram` sat here for a while validating and then failing at compile with
// "driver is not implemented", which taught an author nothing until the last
// step. Retired 2026-08-24 (constitution 5.0.0).
var Providers = []Provider{LiveKit, Pipecat}

// Retired names a provider this repository used to accept as a target. The
// value is what to tell the author, because "unknown provider" is true and
// unhelpful for a word that used to work.
//
// `deepgram` is here as a *target*. It remains a perfectly good model vendor —
// slng/deepgram/nova:3-en and friends — which is the distinction the
// constitution's targets-and-vendors bullet exists to protect.
var Retired = map[Provider]string{
	"vapi":     "the vapi target never emitted a runnable project and was retired on 2026-08-24",
	"deepgram": "the deepgram target never emitted a runnable project and was retired on 2026-08-24; deepgram remains available as a model vendor, for example slng/deepgram/nova:3-en",
}

func IsCode(provider Provider) bool {
	return provider == LiveKit || provider == Pipecat
}

// EmitsProject reports whether a provider's driver writes a runnable project.
// Every provider in Providers does, which is now the entry requirement; the
// function stays because internal/generate and internal/skill both ask, and one
// list beats two hand-copied ones.
func EmitsProject(provider Provider) bool {
	return provider == LiveKit || provider == Pipecat
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
	FieldEndpointingDelay      Field = "pipeline.turn.endpointing_delay"
	FieldFallback              Field = "models.fallback"
	FieldListenFallback        Field = "models.listen.fallback"
	FieldTask                  Field = "tasks"
	FieldTaskModel             Field = "tasks.model"
	FieldTaskNestedResult      Field = "tasks.result.nested"
	FieldTaskGroup             Field = "task_groups"
	FieldTaskGroupReturn       Field = "task_groups.then.return"
	FieldContextIsolated       Field = "task_groups.context_scope.isolated"
	FieldTransferAnnounce      Field = "controls.agent_transfer.announce"
	FieldTransferRequires      Field = "controls.agent_transfer.requires"
	FieldContextNoToolCalls    Field = "context.include_tool_calls.false"
	FieldContextVariableSubset Field = "context.variables.list"
	FieldTransferBriefing      Field = "controls.human_transfer.warm.briefing"
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
	FieldToolMCPTask           Field = "tasks.tools.execution.mcp"
	FieldToolClient            Field = "tools.execution.client"
	FieldToolProviderHosted    Field = "tools.execution.provider_hosted"
	FieldToolBuiltin           Field = "tools.execution.builtin"
	FieldToolAuth              Field = "tools.auth"
	FieldToolInterruption      Field = "tools.interruption.non_default"
	FieldToolAnnounce          Field = "tools.announce"
	FieldToolAnnounceTask      Field = "tasks.tools.announce"
	FieldOutbound              Field = "channels.telephony.outbound"
	FieldVoicemail             Field = "channels.telephony.on_voicemail"
	FieldDeploymentMultiRegion Field = "deployment_region.multiple"
	FieldTracingLangfuse       Field = "tracing.provider.langfuse"
	FieldTracingCoval          Field = "tracing.provider.coval"
	FieldVariableConversation  Field = "variables.source.conversation"
	FieldToolInject            Field = "tools.inject"
	FieldWebhookPath           Field = "tools.webhook.path"
	FieldTemplates             Field = "templates.session_start"
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
			FieldListenLocal: field(),
			FieldSpeakLocal:  field(),
			// Same answer as its two siblings, and for the same reason:
			// placement is forwarded, not lowered. A code driver emits the same
			// service call either way and runs wherever the author runs it, so
			// there is nothing for a local reason model to be gated on.
			FieldReasonLocal: field(),
			FieldSpeakEndpoint: field(
				deny(LiveKit, "LiveKit has no OpenAI-compatible speak wildcard: its openai plugin TTS carries no language slot (N14), so a custom speak endpoint cannot be catalogued"),
			),
			FieldTurnPlacement: field(
				warn(LiveKit, "LiveKit turn placement is a preference"),
			),
			FieldSemanticEndpointing: field(
				warn(LiveKit, "LiveKit semantic endpointing depends on the bound model"),
				warn(Pipecat, "Pipecat semantic endpointing depends on the bound model"),
			),
			// One window, two places to set it: LiveKit's prewarmed Silero VAD
			// (min_silence_duration, floored at 250ms because the turn detector
			// raises below that) and Pipecat's VADParams stop_secs. Not
			// interchangeable defaults, which matters when a package moves
			// between the two: LiveKit's Silero defaults to 0.55s and Pipecat's
			// stop_secs to 0.2s, so the same unset package hears a different
			// agent. LiveKit's turn_handling endpointing min_delay is
			// deliberately NOT the destination; it cannot fire before the VAD
			// reports end of speech, so anything under the window is inert
			// there. The hosted stacks own turn taking, so the value has
			// nowhere to land.
			FieldEndpointingDelay: field(),
			FieldFallback:         field(deny(Pipecat, "the Pipecat driver does not emit generated fallback yet")),
			// Listen (STT) fallback: native on LiveKit (stt.FallbackAdapter,
			// verified in livekit-agents source 2026-07-19). Gated elsewhere
			// until a slot is verified: no documented transcriber fallback on
			// Vapi, and Deepgram's agent.listen takes a single provider (unlike
			// think's array).
			FieldListenFallback: field(
				deny(Pipecat, "the Pipecat driver does not emit listen fallback yet"),
			),
			FieldTask: field(),
			// Verified 2026-07-16: an LLMSwitcher inside an LLMWorker pipeline
			// stalls all flow frames on pipecat-ai 1.5.0, so per-task model has
			// no working lowering there yet (driver-pipecat B7 spike).
			FieldTaskModel:        field(deny(Pipecat, "the Pipecat driver does not emit per-task model yet (LLMSwitcher stalls inside an LLMWorker)")),
			FieldTaskNestedResult: field(),
			FieldTaskGroup:        field(warn(LiveKit, "LiveKit TaskGroup is experimental")),
			FieldTaskGroupReturn:  field(),
			FieldContextIsolated:  field(),
			FieldTransferAnnounce: field(),
			FieldTransferRequires: field(),
			FieldContextNoToolCalls: field(
				deny(Pipecat, "the Pipecat driver does not shape transfer context (include_tool_calls) yet"),
			),
			FieldContextVariableSubset: field(
				deny(Pipecat, "the Pipecat driver does not shape transfer context (variables subset) yet"),
			),
			// SCHEMA N25: `briefing` is free text, so there is no per-value row
			// to resolve. It rides the warm_transfer control row, which already
			// says which routes can carry a private consultation leg at all.
			FieldTransferBriefing: field(
				deny(Pipecat, "this project emits no warm transfer on any Pipecat route yet, so a briefing has nothing to lower onto; warm transfer compiles on (livekit, sip) trunks today (SPEC C4)"),
			),
			FieldGreetingUserFirst:    field(),
			FieldGreetingModelWritten: field(),
			FieldGreetingAbsent: field(
				warn(LiveKit, "LiveKit has no greeting block: the agent opens with a model-written line"),
				warn(Pipecat, "Pipecat has no greeting block: the agent opens with a model-written line"),
			),
			FieldInterruptionMinWords: field(),
			FieldInterruptionIgnore:   field(),
			FieldInactivity: field(
				warn(LiveKit, "LiveKit driver must range-check inactivity durations"),
				warn(Pipecat, "Pipecat driver must range-check inactivity durations"),
			),
			FieldMaxDuration: field(
				warn(LiveKit, "LiveKit driver must verify a max-duration cap"),
				warn(Pipecat, "Pipecat driver must verify a max-duration cap"),
			),
			FieldThinkingAudio: field(
				deny(Pipecat, "the Pipecat driver does not emit thinking audio yet"),
			),
			// Vapi-only on purpose, even though no driver enforces output today
			// (grep `.Output` across internal/generate: no hits). The tag
			// vocabulary has no slot for "declared, inert, legal everywhere":
			// `warn` is defined as "works on all four" and every other use of it
			// means works-with-a-caveat, while the honest tag for "not proven on
			// any target yet" is `provisional`, which fails validation
			// everywhere and would reject every package that declares an output.
			// Choosing between implementing enforcement and taking that break is
			// a maintainer call, so the gap is recorded in SCHEMA.md N22 rather
			// than encoded here as a redefined tag.
			FieldToolOutput: field(),
			FieldToolLocal:  field(),
			FieldToolMCP:    field(),
			// Scope, not kind: Pipecat emits MCP sources on an agent, but a
			// Flows node advertises only the function schemas it lists, and
			// pipecat's MCPClient exposes no per-tool handler to wrap in one.
			// So a source listed on a task fails by name instead of quietly
			// being offered everywhere or nowhere (N40).
			FieldToolMCPTask: field(
				deny(Pipecat, "the Pipecat driver cannot scope an MCP tool source to a task: list it on the agent instead"),
			),
			FieldToolClient: field(
				deny(LiveKit, "LiveKit client tools are not proven by its driver"),
				deny(Pipecat, "Pipecat client tools are not proven by its driver"),
			),
			FieldToolProviderHosted: field(
				deny(LiveKit, "LiveKit provider-hosted tools are not proven by its driver"),
				deny(Pipecat, "Pipecat provider-hosted tools are not proven by its driver"),
			),
			FieldToolBuiltin: field(
			// LiveKit + Pipecat host the end_call prebuilt; the rest still lack
			// a lowering.
			),
			FieldToolAuth: field(
			// The code drivers own the request, so they can send the header;
			// a managed target configures its own tool auth provider-side.
			),
			FieldToolInterruption: field(
				warn(LiveKit, "LiveKit runs tool executions to completion; a per-tool interruption preference is not enforced"),
			),
			FieldToolAnnounce: field(),
			// Scope, not kind: Pipecat emits an agent tool as a decorated
			// function that holds FunctionCallParams, but a task tool as a flows
			// handler, which holds a FlowManager instead. Both have a seam:
			// FlowManager.worker is the documented way to queue a frame from
			// inside a handler, verified on pipecat-ai 1.7.0, the pinned version,
			// where flows ships bundled as pipecat.flows rather than the
			// standalone pipecat_flows package.
			//
			// This row used to read deny(Pipecat, "cannot announce a tool listed
			// on a task: list it on the agent instead"). That was a gap in this
			// compiler stated as a limit of the provider, and it blocked a
			// feature that worked. The scope stays a named field because the two
			// drivers reach the seam differently and a third might not have one,
			// so it keeps somewhere to say so.
			FieldToolAnnounceTask: field(),
			FieldOutbound: field(
				deny(Pipecat, "the Pipecat driver does not emit outbound calling yet"),
			),
			FieldVoicemail: field(
				deny(Pipecat, "the Pipecat driver does not emit voicemail handling yet"),
			),
			FieldTracingLangfuse: field(),
			// Coval tracing needs a process the driver owns: it installs an
			// OpenTelemetry provider and reads a per-call simulation ID off the
			// inbound call. A managed target exposes neither, so both rows deny
			// for the same reason their Langfuse rows do.
			FieldTracingCoval: field(),
			// Several regions in one deployment_region (N32). LiveKit creates
			// one deployment per region from one build directory; every other
			// provider is gated, each in its own words. Verified 2026-08-12.
			FieldDeploymentMultiRegion: field(
				deny(Pipecat, "Pipecat Cloud agent names are globally unique across regions, so a second region needs a differently named agent: declare one region here and deploy the second with `pipecat cloud deploy <name>-<region> --region <region>`"),
			),
			// Variables and secrets (variable_secrets_specs.md V5). The code
			// drivers own the session state and the request, so they can capture
			// a value mid-call and merge hidden parameters; a managed target can
			// only do what its own API exposes, and the Deepgram driver is
			// unwritten. Each row lifts when its provider mechanism is
			// doc-verified (the verify table in that spec).
			FieldVariableConversation: field(),
			FieldToolInject:           field(),
			FieldWebhookPath:          field(),
			FieldTemplates:            field(),
		},
		Controls: map[TelephonyControl]map[Provider]ControlCapability{
			ColdTransfer: controls(
				control(),
				controlDeny("Pipecat cold transfer requires an active channels.phone Connection: a web-only session has no existing SIP sessionId or phone leg to transfer; use daily-sip or cloud-websocket with Twilio"),
			),
			WarmTransfer: controls(
				control(),
				// Says which of two things it means, per N34: Daily documents warm,
				// this driver has not built it (feature 005); the carrier websocket
				// transports have no transfer control at all. Writing either as the
				// other is the defect FR-032 exists to stop.
				controlDeny("this driver does not emit warm transfer yet; Daily documents the pattern but it needs the bot to own the call audio, and Pipecat's websocket transports have no transfer control at all. Warm compiles on (livekit, sip) today (SPEC C1, C4)"),
			),
			DTMFSend:           routedControls("dtmf_send"),
			DTMFReceive:        routedControls("dtmf_receive"),
			Hold:               routedControls("hold"),
			Hangup:             controls(control(), control()),
			VoicemailDetection: controls(control(), control()),
			IVRNavigation:      routedControls("ivr_navigation"),
		},
		Conditions: map[Field]map[Provider]ValueCondition{
			FieldToolMCP: {
				LiveKit: {Value: "python", Note: "LiveKit MCP tools require sdk_language: python"},
			},
		},
		Roles: map[Role]map[Provider]RoleKind{
			Listen: role(Open, Open),
			Turn:   role(Open, Open),
			Speak:  role(Open, Open),
			Reason: role(Open, Open),
		},
		History: map[History]map[Provider]HistorySupport{
			// Pipecat driver v1 emits history: full only; other values are a
			// maturity gate (the workers handoff carries the running context and
			// fine-grained shaping is not emitted yet, C9).
			HistoryFull:     history(HistoryOK, HistoryOK),
			HistoryMessages: history(HistoryOK, HistoryFail),
			HistoryLastN:    history(HistoryOK, HistoryFail),
			HistorySummary:  history(HistoryGenerated, HistoryFail),
			HistoryReset:    history(HistoryOK, HistoryFail),
		},
		FallbackSlots: map[Provider]FallbackSlot{
			LiveKit: FallbackComponent, Pipecat: FallbackGenerated,
		},
	}
}

func control() ControlCapability {
	return ControlCapability{Capability: Capability{Tag: Core}}
}

func controlDeny(note string) ControlCapability {
	return ControlCapability{Capability: Capability{Tag: Gated, Note: note}}
}

// controlNamedCarrier stood here: a control gated on the target's carrier
// alone. Its four users were the Vapi and Deepgram transfer rows, and they lost
// the condition when `carrier` left the target — after that no author could
// write a value those rows would see, so the condition could only ever produce
// a refusal naming an impossible fix (spec FR-001a, research R11). The Carrier
// field it set is still read by routedControls below, which pairs it with a
// transport.

func routedControls(name string) map[Provider]ControlCapability {
	note := "required control " + name + " is proven only for the exact carrier Twilio and transport Daily SIP route"
	value := ControlCapability{Capability: Capability{Tag: Core}, Carrier: "twilio", Transport: "daily-sip", ConditionNote: note}
	return controls(value, value)
}

func controls(livekit, pipecat ControlCapability) map[Provider]ControlCapability {
	return map[Provider]ControlCapability{LiveKit: livekit, Pipecat: pipecat}
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

func role(livekit, pipecat RoleKind) map[Provider]RoleKind {
	return map[Provider]RoleKind{LiveKit: livekit, Pipecat: pipecat}
}

func history(livekit, pipecat HistoryKind) map[Provider]HistorySupport {
	values := map[Provider]HistorySupport{
		LiveKit: {Kind: livekit}, Pipecat: {Kind: pipecat},
	}
	if pipecat == HistoryFail {
		value := values[Pipecat]
		value.Note = "the Pipecat driver emits history: full only; other values are not shaped yet"
		values[Pipecat] = value
	}
	return values
}
