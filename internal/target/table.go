package target

type Provider string

const (
	LiveKit Provider = "livekit"
	Pipecat Provider = "pipecat"
	Slng    Provider = "slng"
)

// Providers is every target a package may name. A provider earns a place here by
// having a driver that owns its whole output, not before: `vapi` and `deepgram`
// sat here for a while validating and then failing at compile with "driver is
// not implemented", which taught an author nothing until the last step. Retired
// 2026-08-24 (constitution 5.0.0).
//
// Owning the output does not mean emitting Python. LiveKit and Pipecat emit a
// runnable project; slng emits a deployment body for a platform that runs the
// agent, which is a complete output of a different shape (constitution 6.0.0).
// What is still forbidden is a provider that validates and produces nothing.
var Providers = []Provider{LiveKit, Pipecat, Slng}

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
// It stopped agreeing with Providers in constitution 6.0.0: slng is a target and
// emits no project, so this is now the question "is there something to run, pin
// and version here", which is what every caller was really asking. It is what
// separates a target that has a `dev`, a framework version and dependency pins
// from one that has none of the three.
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
	FieldPace                  Field = "pipeline.turn.pace"
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
	FieldDelegateRequires      Field = "controls.delegate.requires"
	FieldContextNoToolCalls    Field = "context.include_tool_calls.false"
	FieldContextVariableSubset Field = "context.variables.list"
	FieldTransferBriefing      Field = "controls.human_transfer.warm.briefing"
	FieldGreetingUserFirst     Field = "conversation.greeting.user"
	FieldGreetingModelWritten  Field = "conversation.greeting.model_written"
	FieldGreetingAbsent        Field = "conversation.greeting.absent"
	FieldInterruptionMinWords  Field = "conversation.interruption.minimum_words"
	FieldInterruptionIgnore    Field = "conversation.interruption.ignore_phrases"
	FieldInterruptionProtect   Field = "conversation.interruption.protect"
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
	FieldToolSlngHosted        Field = "tools.execution.slng"
	FieldToolKnowledge         Field = "tools.execution.knowledge"
	FieldToolKnowledgeTask     Field = "tasks.tools.execution.knowledge"
	FieldToolAuth              Field = "tools.auth"
	FieldToolInterruption      Field = "tools.interruption.non_default"
	FieldToolAnnounce          Field = "tools.announce"
	FieldToolAnnounceTask      Field = "tasks.tools.announce"
	FieldOutbound              Field = "channels.telephony.outbound"
	FieldVoicemail             Field = "channels.telephony.on_voicemail"
	FieldDeploymentMultiRegion Field = "deployment_region.multiple"
	FieldWarmInstances         Field = "warm_instances"
	FieldTracingLangfuse       Field = "tracing.provider.langfuse"
	FieldTracingCoval          Field = "tracing.provider.coval"
	FieldVariableConversation  Field = "variables.source.conversation"
	FieldPrefetch              Field = "prefetch"
	FieldVariableConfirm       Field = "variables.confirm"
	FieldDelegateAnnounce      Field = "controls.delegate.announce"
	FieldToolInject            Field = "tools.inject"
	FieldWebhookPath           Field = "tools.webhook.path"
	FieldToolDependencies      Field = "tools.local.dependencies"
	FieldTemplates             Field = "templates.session_start"
	FieldTypedState            Field = "variables.type.shape"
	FieldShapedText            Field = "variables.type.shaped"
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
			FieldListenLocal: field(deny(Slng, slngNoPlacement("listen"))),
			FieldSpeakLocal:  field(deny(Slng, slngNoPlacement("speak"))),
			// Same answer as its two siblings on the code drivers, and for the same
			// reason: placement is forwarded, not lowered. A code driver emits the
			// same service call either way and runs wherever the author runs it, so
			// there is nothing for a local reason model to be gated on. On a hosted
			// target there is no "wherever the author runs it", which is why the
			// slng arm denies where those two pass.
			FieldReasonLocal: field(deny(Slng, slngNoPlacement("reason"))),
			FieldSpeakEndpoint: field(
				deny(LiveKit, "LiveKit has no OpenAI-compatible speak wildcard: its openai plugin TTS carries no language slot (N14), so a custom speak endpoint cannot be catalogued"),
				deny(Slng, "slng target takes a speak model name and a voice with no slot for a custom endpoint: bind a speak model SLNG hosts, or compile to pipecat which emits the endpoint_env lookup"),
			),
			// The three turn rows deny for one reason: SLNG owns turn taking and its
			// create body has no turn section at all, so a bound turn model, a
			// semantic endpointing choice and a silence window all reach nothing.
			// Dropping them quietly would change how the agent hears a caller
			// without saying so, which is the downgrade Principle II forbids.
			//
			// LiveKit places the detector itself, which is a fact about LiveKit
			// and not a problem with the package, so it warns nowhere: the
			// emitted runbook and the turn-taking page are where that is taught.
			FieldTurnPlacement: field(
				deny(Slng, "slng target owns its own turn taking and its create body carries no turn section: remove the turn binding, or compile to livekit or pipecat which place the turn detector themselves"),
			),
			// Both code targets warned here until 2026-08-27, and the warning was
			// honest about the wrong thing. It said the effect depended on the
			// bound model. The real situation was that the value reached no
			// emitted project at all: both drivers marked it advisory and no
			// template read it. It is load-bearing now — `off` removes the turn
			// model on LiveKit and the end-of-turn analyzer on Pipecat — so it is
			// an ordinary supported field.
			FieldSemanticEndpointing: field(
				deny(Slng, "slng target owns its own turn taking, so a semantic endpointing choice reaches nothing: remove it, or compile to livekit or pipecat where the bound turn model decides"),
			),
			// One window, two places to set it: LiveKit's prewarmed Silero VAD
			// (min_silence_duration, floored at 250ms because the turn detector
			// raises below that) and Pipecat's VADParams stop_secs. Not
			// interchangeable defaults, which matters when a package moves
			// between the two: LiveKit's Silero defaults to 0.55s and Pipecat's
			// stop_secs to 0.2s, so the same unset package hears a different
			// agent. The hosted stacks own turn taking, so the value has
			// nowhere to land.
			//
			// This field is the FLOOR only. It was the whole story until
			// 2026-08-27, and the note that used to sit here said LiveKit's
			// endpointing min_delay was deliberately not a destination. That is
			// now false, and deliberately: `pace` reaches min_delay and max_delay,
			// which is where a 2.5s turn actually came from. The reasoning was
			// also only half right — min_delay cannot fire before the VAD reports
			// end of speech, so a min_delay under the window is inert, but
			// max_delay is a ceiling the VAD window has no say in at all.
			FieldEndpointingDelay: field(
				deny(Slng, "slng target owns its own turn taking, so an endpointing delay reaches nothing: remove it, or compile to livekit or pipecat, which set the silence window on the VAD"),
			),
			// The ceiling, and the floor when no duration was authored. One
			// authored word per binding, mapped per target in pace.go, because a
			// floor and a ceiling are target facts and the two frameworks spell
			// neither the same way. No per-target override: a per-target pace is
			// a duration in disguise and endpointing_delay already is one.
			FieldPace: field(
				deny(Slng, "slng target owns its own turn taking, so a pace reaches nothing: remove it, or compile to livekit or pipecat, which set the turn window themselves"),
			),
			// SLNG has a real fallback slot per component: fallbacks.stt and
			// fallbacks.llm take model strings, fallbacks.tts takes model and voice
			// pairs (voice_agent.py:194-225, read 2026-08-25). The platform holds the
			// list, so nothing is generated and nothing is pinned to one vendor.
			FieldFallback: field(
				deny(Pipecat, "the Pipecat driver does not emit generated fallback yet"),
				allow(Slng),
			),
			// Listen (STT) fallback: native on LiveKit (stt.FallbackAdapter,
			// verified in livekit-agents source 2026-07-19). Gated elsewhere
			// until a slot is verified: no documented transcriber fallback on
			// Vapi, and Deepgram's agent.listen takes a single provider (unlike
			// think's array).
			FieldListenFallback: field(
				deny(Pipecat, "the Pipecat driver does not emit listen fallback yet"),
				allow(Slng),
			),
			FieldTask: field(deny(Slng, slngNoTasks("a task"))),
			// Verified 2026-07-16: an LLMSwitcher inside an LLMWorker pipeline
			// stalls all flow frames on pipecat-ai 1.5.0, so per-task model has
			// no working lowering there yet (driver-pipecat B7 spike).
			FieldTaskModel: field(
				deny(Pipecat, "the Pipecat driver does not emit per-task model yet (LLMSwitcher stalls inside an LLMWorker)"),
				deny(Slng, slngNoTasks("a per-task model")),
			),
			FieldTaskNestedResult: field(deny(Slng, slngNoTasks("a nested task result"))),
			FieldTaskGroup: field(
				deny(Slng, slngNoTasks("a task group")),
			),
			FieldTaskGroupReturn:  field(deny(Slng, slngNoTasks("a task group return step"))),
			FieldContextIsolated:  field(deny(Slng, slngNoTasks("an isolated task context"))),
			FieldTransferAnnounce: field(deny(Slng, slngNoHandoff("a transfer announcement"))),
			FieldTransferRequires: field(deny(Slng, slngNoHandoff("a transfer requirement"))),
			// A step requirement is refused for the task reason, not the handoff
			// one: the slng target has no separate step to hold back, so there is
			// nothing for the guard to guard.
			FieldDelegateRequires: field(deny(Slng, slngNoTasks("a step requirement"))),
			// A step announcement is refused for the task reason too: with one
			// agent and no steps there is no entry to speak over.
			FieldDelegateAnnounce: field(deny(Slng, slngNoTasks("a step announcement"))),
			// Prefetch needs a seam between the call arriving and the agent
			// greeting, and both code drivers have one: LiveKit between hydration
			// and session.start, Pipecat between build_state and the agent
			// construction. The SLNG platform owns session start, so there is no
			// seam of ours to resolve a fact in.
			//
			// The clock case is genuinely reachable there through the router's
			// own template_variables, and is left undone rather than half done:
			// half of a prefetch block working on one target is worse than none of
			// it, because the package would compile and behave differently.
			FieldPrefetch: field(deny(Slng, "slng target compiles no session-start hook of its own, so a prefetch has no seam to run in: "+
				"fold the value into the agent's instructions, or compile to livekit or pipecat which emit the block")),
			// Confirmation holds a value back from a gate, and the gate is the
			// prerequisite guard on a step. No steps, no guard, nothing to hold.
			FieldVariableConfirm: field(deny(Slng, slngNoTasks("a value awaiting confirmation"))),
			// Declared state is a generated Pydantic class in a module the two
			// code drivers write. The slng target writes a spec and emits no
			// module, so there is nowhere for the class, the validator or the
			// composed state block to be.
			FieldTypedState: field(deny(Slng, slngNoModule("a value with a declared shape"))),
			FieldShapedText: field(deny(Slng, slngNoModule("a value whose text has a validated shape"))),
			FieldContextNoToolCalls: field(
				deny(Pipecat, "the Pipecat driver does not shape transfer context (include_tool_calls) yet"),
				deny(Slng, slngNoHandoff("include_tool_calls: false")),
			),
			FieldContextVariableSubset: field(
				deny(Pipecat, "the Pipecat driver does not shape transfer context (variables subset) yet"),
				deny(Slng, slngNoHandoff("a variables subset")),
			),
			// SCHEMA N25: `briefing` is free text, so there is no per-value row
			// to resolve. It rides the warm_transfer control row, which already
			// says which routes can carry a private consultation leg at all.
			FieldTransferBriefing: field(
				deny(Pipecat, "this project emits no warm transfer on any Pipecat route yet, so a briefing has nothing to lower onto; warm transfer compiles on (livekit, sip) trunks today (SPEC C4)"),
				deny(Slng, "slng target has no warm transfer to brief: SLNG's curated transfer_call places one blind transfer with no consultation leg, so drop the briefing and use mode: cold, or compile to livekit on a sip route which carries the private leg"),
			),
			// SLNG requires a greeting on every agent and speaks the string it is
			// given (voice_agent.py:951, read 2026-08-25). So all three greeting
			// shapes that are not "the author wrote the line" deny, and each says
			// which line to write.
			FieldGreetingUserFirst: field(
				deny(Slng, "slng target requires a greeting on every agent, so it cannot wait for the caller to speak first: write conversation.greeting.text, or compile to livekit or pipecat which can open silent"),
			),
			FieldGreetingModelWritten: field(
				deny(Slng, "slng target speaks the greeting it is given rather than composing one: write conversation.greeting.text, or compile to livekit or pipecat which let the model open the call"),
			),
			FieldGreetingAbsent: field(
				deny(Slng, "slng target requires a greeting and cannot invent one: add a conversation.greeting with a text, or compile to livekit or pipecat where the agent opens with a model-written line"),
			),
			FieldInterruptionMinWords: field(
				deny(Slng, "slng target takes interruptions as on or off with no minimum word count: remove minimum_words, or compile to livekit or pipecat which set the threshold"),
			),
			FieldInterruptionIgnore: field(
				deny(Slng, "slng target takes interruptions as on or off and reads no phrase list: remove ignore_phrases, or compile to livekit or pipecat which match them"),
			),
			// Pipecat carries this natively: the aggregator takes one mute
			// strategy per protected stretch, so "greeting" and "tool_calls" each
			// lower to a class from pipecat.turns.user_mute.
			//
			// LiveKit could honour "greeting" by passing allow_interruptions=False
			// to the say() that opens the call, but not a model-written opening,
			// which goes through generate_reply and takes no such argument. Half a
			// field is worse than none, so it is refused by name until both
			// openings are wired.
			FieldInterruptionProtect: field(
				deny(LiveKit, "the LiveKit driver does not protect named stretches of a call yet: drop conversation.interruption.protect and use enabled: false to stop barge-in for the whole call, or compile to pipecat which mutes per stretch"),
				deny(Slng, "slng target takes interruptions as on or off with no way to protect one stretch of a call: drop conversation.interruption.protect, or compile to pipecat which mutes per stretch"),
			),
			FieldInactivity: field(
				// SLNG's idle nudges take three spoken texts, each 1 to 500 characters
				// (voice_agent.py:232-243), and a package carries two durations and no
				// texts. There is no honest mapping, so the field is refused by name and
				// the body leaves idle_nudges out, which lets SLNG apply its own.
				deny(Slng, "slng target cannot carry an inactivity window: SLNG's idle nudges need three spoken texts a package does not declare, so remove conversation.inactivity and let SLNG use its own, or compile to livekit or pipecat which take the durations"),
			),
			FieldMaxDuration: field(
				deny(Slng, "slng target's create body has no maximum call duration: cap the call in the SLNG dashboard, or compile to livekit or pipecat which enforce it inside the agent"),
			),
			FieldThinkingAudio: field(
				deny(Pipecat, "the Pipecat driver does not emit thinking audio yet"),
				deny(Slng, "slng target's create body has no thinking-audio setting: remove conversation.thinking_audio, or compile to livekit which emits it"),
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
			// Denied on slng since reference-only. This row was allowed there
			// *because* SLNG derives a code tool's result schema by
			// introspecting the Output class in the code_src unmute wrote
			// (tool.py:353, read 2026-08-25). unmute now writes no code_src, so
			// there is nothing to introspect and the field reaches nothing.
			// Refused rather than dropped, and the message says which fact
			// changed.
			FieldToolOutput: field(
				deny(Slng, "slng target derives a tool's result shape from the tool SLNG hosts, not from the package: `output:` reaches nothing here, so remove it, or compile to livekit or pipecat which read it"),
			),
			// The refusal that carries reference-only. It has to answer "then
			// how do I get my tool onto SLNG", because the answer is no longer
			// "unmute does it for you": the `tool` group of the platform CLI is
			// list, get and run, with no create, verified at 0.1.16 on
			// 2026-09-03.
			FieldToolLocal: field(
				deny(Slng, "slng target does not create tools: a `local:` handler would have to be uploaded, and SLNG owns a tool's code, version and gate pipeline: create the tool in the SLNG dashboard and reference it with `slng:`, or compile to livekit or pipecat which run your handler themselves"),
			),
			// Core since voiceai 0.1.16, verified against a live deploy 2026-09-01.
			// unmute writes {server, tool} by name; the push resolves the name to
			// server_id and copies each tool's observed_schema_hash out of the
			// platform's OWN stored capability snapshot, so nothing connects to the
			// server. This row warned for a year that the push had to reach the
			// server and that a package with an mcp: tool could not deploy. Both
			// were wrong, and the second was wrong in the direction that stops
			// people trying.
			//
			// What remains is a freshness window, not a compile-time fact: the
			// backend refuses a snapshot older than mcp_capability_ttl_seconds
			// (300s) or one whose probe failed. Neither is knowable from a package,
			// and both are visible in the account, so the preflight owns them.
			FieldToolMCP: field(allow(Slng)),
			// Scope, not kind: Pipecat emits MCP sources on an agent, but a
			// Flows node advertises only the function schemas it lists, and
			// pipecat's MCPClient exposes no per-tool handler to wrap in one.
			// So a source listed on a task fails by name instead of quietly
			// being offered everywhere or nowhere (N40).
			//
			// slng stays core here for the reason the announce row below gives: the
			// FieldTask row already refuses the task, so a second message about its
			// tools would name a consequence instead of the cause.
			FieldToolMCPTask: field(
				deny(Pipecat, "the Pipecat driver cannot scope an MCP tool source to a task: list it on the agent instead"),
				allow(Slng),
			),
			FieldToolClient: field(
				deny(LiveKit, "LiveKit client tools are not proven by its driver"),
				deny(Pipecat, "Pipecat client tools are not proven by its driver"),
				deny(Slng, "slng target has no client tool shape: all eight SLNG tool types run server-side, so move the work into a local: or webhook: tool"),
			),
			FieldToolProviderHosted: field(
				deny(LiveKit, "LiveKit provider-hosted tools are not proven by its driver"),
				deny(Pipecat, "Pipecat provider-hosted tools are not proven by its driver"),
				deny(Slng, "slng target has no provider-hosted tool shape: SLNG curates its own builtins, so use builtin: end_call to hang up, or move the work into a local: or webhook: tool"),
			),
			FieldToolBuiltin: field(
				// LiveKit + Pipecat host the end_call prebuilt; SLNG curates its own
				// end_call, which is one of the five reserved names, so a builtin
				// reference is written by name and needs no tool body.
				allow(Slng),
			),
			// Allowed everywhere, which is the whole point of the block: naming a
			// tool SLNG hosts must cost no portability.
			//
			// It works on the code targets because the mirror `unmute pull`
			// commits carries the platform's own introspected schema and, for a
			// code tool, its module. So livekit and pipecat build a real tool out
			// of a definition they did not have to parse Python to recover, and
			// slng references the platform's own copy by name.
			//
			// The one limit is not here: a hosted tool that declares Python
			// dependencies is refused on the code targets by FieldToolDependencies
			// below, in the words that row already uses, because a mirrored pin
			// and an authored one reach nothing there for the same reason.
			FieldToolSlngHosted: field(allow(Slng)),
			// Core on both since 2026-08-25, when the Pipecat lowering landed.
			// It started denied on Pipecat on purpose: constitution 5.0.0 retired
			// the vapi and deepgram targets for the inverse, validating and then
			// failing at compile with "driver is not implemented", which taught an
			// author nothing until the last step.
			// internal/generate/knowledge_agreement_test.go asserts Core on both
			// and asserts both drivers emit the same contract, so this row and the
			// two lowerings cannot drift apart.
			// The two code drivers emit the module that reads the documents. The
			// slng target emits a README and pushes a spec, so there is no image to
			// carry the folder and nothing to build an index in.
			FieldToolKnowledge: field(
				deny(Slng, slngNoKnowledge("a knowledge base")),
			),
			// Scope, not kind, and the same seam FieldToolMCPTask records: a
			// Pipecat task tool is a flows handler holding a FlowManager, not a
			// decorated function holding FunctionCallParams.
			FieldToolKnowledgeTask: field(
				deny(Pipecat, "the Pipecat driver cannot scope a knowledge tool to a task: list it on the agent instead"),
				deny(Slng, slngNoKnowledge("a task-scoped knowledge base")),
			),
			FieldToolAuth: field(
				// The code drivers own the request, so they can send the header.
				// SLNG takes auth in the tool body instead: ApiRequestConfig.auth
				// is none, bearer or hmac with a Vault secret_name
				// (api_request.py:138). That was the reason this was allowed on
				// slng, and reference-only removed it: unmute writes no tool body,
				// so there is nowhere for the block to land.
				//
				// A hosted request tool still authenticates. Its credential is
				// the platform's, named in the mirror's config.auth.secret_name,
				// and it reaches the runbook and the deploy preflight from there.
				deny(Slng, "slng target does not create tools, so there is no tool body for an `auth:` block to land in: a hosted tool keeps its own credential, which SLNG stores, so reference it with `slng:`, or compile to livekit or pipecat which send the header themselves"),
			),
			FieldToolInterruption: field(
				warn(LiveKit, "LiveKit runs tool executions to completion; a per-tool interruption preference is not enforced"),
				deny(Slng, "slng target has no per-tool interruption setting: leave the tool at the provider default, or compile to pipecat which enforces the preference"),
			),
			FieldToolAnnounce: field(allow(Slng)),
			// Scope, not kind: Pipecat emits an agent tool as a decorated
			// function that holds FunctionCallParams, but a task tool as a flows
			// handler, which holds a FlowManager instead. Both have a seam:
			// FlowManager.worker is the documented way to queue a frame from
			// inside a handler, verified on pipecat-ai 1.8.0, the pinned version,
			// where flows ships bundled as pipecat.flows rather than the
			// standalone pipecat_flows package.
			//
			// This row used to read deny(Pipecat, "cannot announce a tool listed
			// on a task: list it on the agent instead"). That was a gap in this
			// compiler stated as a limit of the provider, and it blocked a
			// feature that worked. The scope stays a named field because the two
			// drivers reach the seam differently and a third might not have one,
			// so it keeps somewhere to say so.
			FieldToolAnnounceTask: field(allow(Slng)),
			FieldOutbound: field(
				deny(Pipecat, "the Pipecat driver does not emit outbound calling yet"),
				deny(Slng, "slng target writes no trunk and dials nothing from a package: place outbound calls from the SLNG dashboard or its API against a trunk configured there, or compile to livekit which dials from the agent"),
			),
			FieldVoicemail: field(
				deny(Pipecat, "the Pipecat driver does not emit voicemail handling yet"),
				// SLNG has a curated voicemail_detection capability; this driver does
				// not attach it. Saying so is the difference between naming a gap in
				// this compiler and inventing a limit of the platform.
				deny(Slng, "slng target does not emit voicemail handling yet; SLNG curates a voicemail_detection capability and this driver does not attach it: add it to the agent in the SLNG dashboard, or compile to livekit which emits the handling"),
			),
			// Tracing needs a process the driver owns: an exporter is installed at
			// startup, and Coval additionally reads a per-call simulation ID off the
			// inbound call. A hosted target gives unmute neither, which is what this
			// comment always said and what the slng rows now act on.
			FieldTracingLangfuse: field(
				deny(Slng, "slng target instruments no process of yours, so it cannot install a Langfuse exporter: read the traces in the SLNG dashboard, or compile to livekit or pipecat which emit the exporter"),
			),
			FieldTracingCoval: field(
				deny(Slng, "slng target instruments no process of yours and sees no inbound call, so it can neither install the Coval exporter nor read a simulation ID: run the evaluation against the SLNG agent from Coval, or compile to livekit or pipecat which emit the exporter"),
			),
			// Several regions in one deployment_region (N32). LiveKit creates
			// one deployment per region from one build directory; every other
			// provider is gated, each in its own words. Verified 2026-08-12.
			FieldDeploymentMultiRegion: field(
				deny(Pipecat, "Pipecat Cloud agent names are globally unique across regions, so a second region needs a differently named agent: declare one region here and deploy the second with `pipecat cloud deploy <name>-<region> --region <region>`"),
				deny(Slng, "slng target takes exactly one deployment region: name one of any, us-east, eu-central or ap-south, where any lets SLNG route the call itself"),
			),
			// Instances held ready (warm_instances). Only Pipecat Cloud takes the
			// number in a file this compiler writes: `[scaling] min_agents` in
			// pcc-deploy.toml, so an authored value survives every recompile
			// instead of living on a deploy flag somebody has to remember.
			// LiveKit Cloud has no such key. livekit.toml carries the project
			// subdomain and the agent id and nothing else, and whether production
			// keeps a warm replica is a property of the billing plan
			// (docs.livekit.io/deploy/agents/managing-deployments, checked
			// 2026-08-27). SLNG runs the agent itself and exposes no pool.
			FieldWarmInstances: field(
				deny(LiveKit, "livekit.toml holds only the project subdomain and the agent id, so no warm pool size can be compiled into it: on LiveKit Cloud a paid plan keeps production warm and the free plan scales to zero, so drop warm_instances and choose the plan, or compile to pipecat which writes the number into pcc-deploy.toml"),
				deny(Slng, "slng target deploys a hosted agent and exposes no instance pool of yours to keep warm: drop warm_instances, or compile to pipecat which writes the number into pcc-deploy.toml"),
			),
			// Variables and secrets (variable_secrets_specs.md V5). The code
			// drivers own the session state and the request, so they can capture
			// a value mid-call and merge hidden parameters; a managed target can
			// only do what its own API exposes, and the Deepgram driver is
			// unwritten. Each row lifts when its provider mechanism is
			// doc-verified (the verify table in that spec).
			FieldVariableConversation: field(
				deny(Slng, "slng target declares variables and their defaults but has no slot for one captured during the call: supply the value when the call is dispatched, or compile to livekit or pipecat which capture it mid-call"),
			),
			FieldToolInject: field(allow(Slng)),
			// Same reason as FieldToolAuth: the path was written into a tool
			// body, or into the attachment's config override when it carried a
			// template. unmute writes neither now.
			FieldWebhookPath: field(
				deny(Slng, "slng target does not create tools, so there is no tool URL for a `path:` to be appended to: SLNG stores a hosted tool's whole URL, so reference it with `slng:`, or compile to livekit or pipecat which build the request themselves"),
			),
			// SLNG installs a per-tool environment from an exact pin list, with a
			// locked snapshot and a verification probe
			// (app/services/code_dependency_manifest.py). The code drivers build one
			// dependency list for the whole project, out of the provider catalogue's
			// install specs, and read nothing per tool — so a per-tool pin there
			// would reach no artifact at all. Refused rather than dropped.
			FieldToolDependencies: field(
				deny(LiveKit, "the LiveKit driver builds one dependency list for the whole generated project from the provider catalogue and reads no per-tool pins: add the package to build/<target>/pyproject.toml after compiling, or compile to slng which installs a per-tool environment"),
				deny(Pipecat, "the Pipecat driver builds one dependency list for the whole generated project from the provider catalogue and reads no per-tool pins: add the package to build/<target>/pyproject.toml after compiling, or compile to slng which installs a per-tool environment"),
				allow(Slng),
			),
			// SLNG renders the same session-start substitution unmute already writes
			// for the other two: template_variable_options declares the names,
			// template_defaults holds the values, and both a prompt and a greeting
			// may carry one (agent_runtime_compiler.py:991-1007).
			FieldTemplates: field(allow(Slng)),
		},
		Controls: map[TelephonyControl]map[Provider]ControlCapability{
			ColdTransfer: controls(
				control(),
				controlDeny("Pipecat cold transfer requires an active channels.phone Connection: a web-only session has no existing SIP sessionId or phone leg to transfer; use daily-sip or cloud-websocket with Twilio"),
				// A gap in this driver, not a limit of SLNG, and the note says which:
				// transfer_call is one of SLNG's eight curated tool types
				// (shared_tool_contract.py:31) and this driver does not attach it yet.
				controlDeny("slng target does not emit a transfer control yet; SLNG curates a transfer_call capability and this driver does not attach it: add transfer_call to the agent in the SLNG dashboard, or compile to livekit or pipecat which emit the transfer themselves"),
			),
			WarmTransfer: controls(
				control(),
				// Says which of two things it means, per N34: Daily documents warm,
				// this driver has not built it (feature 005); the carrier websocket
				// transports have no transfer control at all. Writing either as the
				// other is the defect FR-032 exists to stop.
				controlDeny("this driver does not emit warm transfer yet; Daily documents the pattern but it needs the bot to own the call audio, and Pipecat's websocket transports have no transfer control at all. Warm compiles on (livekit, sip) today (SPEC C1, C4)"),
				controlDeny("slng target has no warm transfer: SLNG's curated transfer_call places one blind transfer with no consultation leg to hold the caller on, so use mode: cold, or compile to livekit on a sip route"),
			),
			DTMFSend:    routedControls("dtmf_send"),
			DTMFReceive: routedControls("dtmf_receive"),
			Hold:        routedControls("hold"),
			// Hangup and voicemail detection are curated SLNG capabilities
			// (end_call, voicemail_detection), so the control resolves. Whether the
			// driver emits handling for a detected voicemail is a separate question,
			// answered by FieldVoicemail above; that is the same split Pipecat sits
			// in, where this control is core and the field denies.
			Hangup:             controls(control(), control(), control()),
			VoicemailDetection: controls(control(), control(), control()),
			IVRNavigation:      routedControls("ivr_navigation"),
		},
		// Audited for slng 2026-08-25 and deliberately left with no entry. The one
		// condition here reads resolved.SDKLanguage, and a slng target refuses
		// sdk_language outright, so any condition written for it could only fail
		// against an empty value and name a fix no author could write.
		Conditions: map[Field]map[Provider]ValueCondition{
			FieldToolMCP: {
				LiveKit: {Value: "python", Note: "LiveKit MCP tools require sdk_language: python"},
			},
		},
		Roles: map[Role]map[Provider]RoleKind{
			// SLNG binds stt, llm, tts and tts_voice by name (voice_agent.py:144), so
			// three roles are open. Turn is integrated: SLNG owns turn taking and its
			// create body has no turn section, so an author binds nothing there.
			Listen: role(Open, Open, Open),
			Turn:   role(Open, Open, Integrated),
			Speak:  role(Open, Open, Open),
			Reason: role(Open, Open, Open),
		},
		History: map[History]map[Provider]HistorySupport{
			// The Pipecat driver shapes four of the five. `summary` needs an awaited
			// model turn and a summarizer prompt of its own, which is its own change
			// with its own verification; LiveKit's lives as a literal in the LiveKit
			// template. Every slng value fails, because a context history shapes a
			// crossing and slng writes one agent; both notes live in history()
			// rather than five times over.
			HistoryFull:     history(HistoryOK, HistoryOK, HistoryFail),
			HistoryMessages: history(HistoryOK, HistoryOK, HistoryFail),
			HistoryLastN:    history(HistoryOK, HistoryOK, HistoryFail),
			HistorySummary:  history(HistoryGenerated, HistoryFail, HistoryFail),
			HistoryReset:    history(HistoryOK, HistoryOK, HistoryFail),
		},
		// Read unconditionally at the top of every target validation
		// (ir.validateFallbacks), so a missing key breaks every package on that
		// target with "target has no fallback slot kind". SLNG holds a real list per
		// component and generates nothing, which is the component slot; it is not
		// FallbackSameProvider, because nothing in SLNG's fallback rules pins the
		// chain to one vendor.
		FallbackSlots: map[Provider]FallbackSlot{
			LiveKit: FallbackComponent, Pipecat: FallbackGenerated, Slng: FallbackComponent,
		},
	}
}

// slngNoPlacement, slngNoTasks and slngNoHandoff each carry one reason shared by
// several rows. They are here rather than inline because the rows deny for
// exactly the same reason, and three copies of a sentence drift into three
// slightly different sentences.

func slngNoPlacement(role string) string {
	return "slng target runs the whole pipeline on SLNG's own infrastructure, so a locally placed " + role + " model has no machine to run on: drop the placement and let SLNG host the model, or compile to livekit or pipecat and run it yourself"
}

func slngNoTasks(what string) string {
	return "slng target writes one agent with one prompt, so " + what + " has nowhere to go: fold the step into the agent's instructions, or compile to livekit or pipecat which emit multi-task agents"
}

// slngNoKnowledge is why the slng target refuses a knowledge base.
//
// The two code drivers emit knowledge.py and copy the documents beside it, so the
// index is built inside the image they produce. The slng target emits a README and
// pushes a spec: there is no image, nothing to copy the folder into, and no process
// of ours to build an index in.
func slngNoKnowledge(what string) string {
	return "slng target pushes a spec and emits no runtime of its own, so " + what +
		" has nowhere to be read or searched: drop the knowledge: tool and put the " +
		"facts in the agent's instructions, or compile to livekit or pipecat which " +
		"emit the search module and carry the documents in the image"
}

// slngNoModule is why the slng target refuses a declared shape.
//
// Not the tasks reason and not the knowledge reason, though it rhymes with
// both: what is missing here is the emitted Python module. A shape is a
// generated class with a validator, and the validation has to run where the
// value enters the state, which is inside a module this target never writes.
func slngNoModule(what string) string {
	return "slng target pushes a spec and emits no module of its own, so " + what +
		" has nowhere to be declared or checked: declare the value as one of the primitive types, " +
		"or compile to livekit or pipecat, which generate the class and validate the value where it enters"
}

func slngNoHandoff(what string) string {
	return "slng target writes one agent, so there is no handoff for " + what + " to shape: remove it, or compile to livekit or pipecat which emit multi-agent handoffs"
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

// routedControls covers the four controls that exist only on one proven route.
// SLNG is not on any route: unmute writes it no carrier and no transport, so the
// carrier-and-transport condition below could only ever produce a refusal naming
// a fix no author of a slng package can write. It gets a plain deny instead,
// which is the same reason controlNamedCarrier was deleted above.
func routedControls(name string) map[Provider]ControlCapability {
	note := "required control " + name + " is proven only for the exact carrier Twilio and transport Daily SIP route"
	value := ControlCapability{Capability: Capability{Tag: Core}, Carrier: "twilio", Transport: "daily-sip", ConditionNote: note}
	return controls(value, value, controlDeny("slng target emits no "+name+" control: SLNG's curated capabilities cover ending and transferring a call, and this driver attaches neither, so drop the required control or compile to livekit or pipecat on a (twilio, daily-sip) route"))
}

func controls(livekit, pipecat, slng ControlCapability) map[Provider]ControlCapability {
	return map[Provider]ControlCapability{LiveKit: livekit, Pipecat: pipecat, Slng: slng}
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

// allow states a supported answer out loud. It exists because field() stopped
// seeding Slng, so a supported slng row has to say so rather than inherit it.
func allow(provider Provider) override {
	return override{provider, Capability{Tag: Core}}
}

// field seeds the providers whose drivers emit a project and nothing else.
//
// It used to seed every provider in Providers with Core, which read as a
// convenience and was a trapdoor: the day a third provider joined the list, all
// 48 rows would have granted it every behaviour in silence, including rows whose
// own comments already described the managed answer. Twenty-four of them carry no
// override at all, so nothing would have looked wrong in this file.
//
// Leaving Slng out makes TestDefaultTableIsCompleteAndTyped, which already
// existed, fail once per undecided row until a person writes an answer. No new
// test buys 48 forced decisions.
func field(overrides ...override) map[Provider]Capability {
	values := make(map[Provider]Capability, len(Providers))
	for _, provider := range Providers {
		if !EmitsProject(provider) {
			continue
		}
		values[provider] = Capability{Tag: Core}
	}
	for _, override := range overrides {
		values[override.provider] = override.value
	}
	return values
}

func role(livekit, pipecat, slng RoleKind) map[Provider]RoleKind {
	return map[Provider]RoleKind{LiveKit: livekit, Pipecat: pipecat, Slng: slng}
}

func history(livekit, pipecat, slng HistoryKind) map[Provider]HistorySupport {
	values := map[Provider]HistorySupport{
		LiveKit: {Kind: livekit}, Pipecat: {Kind: pipecat}, Slng: {Kind: slng},
	}
	// One value fails on Pipecat now rather than four, so the note names what
	// the target does support. A refusal that only says what is missing leaves
	// the author guessing at the nearest thing that works.
	if pipecat == HistoryFail {
		value := values[Pipecat]
		value.Note = "the Pipecat driver does not summarize a context yet: it supports history: full, messages, last_n and reset"
		values[Pipecat] = value
	}
	// Every slng history value fails, and for one reason rather than five, so the
	// note is written once here instead of five times at the call sites. A context
	// history shapes what a task or a transfer carries across, and the slng target
	// writes one agent with one prompt, so there is no crossing to shape.
	if slng == HistoryFail {
		value := values[Slng]
		value.Note = "slng target writes one agent with one prompt, so no task or transfer exists for a context history to shape: fold the step into the agent's instructions, or compile to livekit or pipecat which carry context across a handoff"
		values[Slng] = value
	}
	return values
}
