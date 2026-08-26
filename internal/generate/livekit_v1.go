package generate

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"text/template"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// The LiveKit driver lowers the resolved IR into a runnable LiveKit Agents
// project (Python). Each agent is a livekit.agents.Agent; agent_transfer is a
// @function_tool returning the next Agent (native handoff); a task is an
// AgentTask[dict]; a task_group is a beta.workflows.TaskGroup. listen/speak
// resolve through the provider catalogue (internal/target/catalog_livekit.go):
// SLNG is the scaffold default, per-vendor plugins bind when the user picks
// them; reason lowers to LiveKit Inference. Python is emitted only through
// these templates (C1/ADR-0002).
//
//go:embed templates/livekit_v1/*.tmpl
var livekitV1Templates embed.FS

// The supported livekit-agents version lives in target.Window(LiveKit). This
// driver emits the author's declared version as an exact pin. The `mcp` extra
// is separate from that version and still added here: without it
// livekit/agents/llm/mcp.py raises ImportError on import (N40, research R2).

// livekitService is one resolved binding: the rendered constructor plus its
// catalogue entry (imports/deps) and vendor (report labeling).
type livekitService struct {
	Call   ServiceCall
	Entry  targetcap.Entry
	Vendor string
}

// livekitChain is a resolved model plus its fallback chain (V4/T16): a
// non-empty Chain renders as a FallbackAdapter of the matching role module
// (llm.FallbackAdapter for think, stt.FallbackAdapter for listen).
type livekitChain struct {
	Primary livekitService
	Chain   []livekitService
}

func (l livekitChain) services() []livekitService {
	return append([]livekitService{l.Primary}, l.Chain...)
}

type livekitAgent struct {
	Name           string
	Class          string
	PromptConst    string
	PromptExpr     string // render call when the prompt is templated, else ""
	IsEntry        bool
	LLM            *livekitChain   // set only when it differs from the session default
	TTS            *livekitService // set only when it differs from the session default
	Greeting       *livekitGreeting
	Tools          []livekitTool
	Prebuilt       []livekitTool // execution: builtin tools, rendered into super().__init__(tools=...)
	MCPServers     []livekitMCPServer
	Transfers      []livekitTransfer
	HumanTransfers []livekitHumanTransfer
	Delegates      []livekitDelegate
	// SlngScope is this agent's own router cache scope, empty unless its think
	// profile is a router binding.
	//
	// It is a per-class constant rather than a constructor value because one
	// model object serves the whole session on this target: the scope has to
	// travel with the request, and the class is what knows whose request it is.
	SlngScope string
}

// livekitGreeting drives the entry agent's on_enter: a fixed line, a
// model-written opening, or silence until the caller speaks.
type livekitGreeting struct {
	Say string
	// Templated marks a line naming variables known at call start (C11). The two
	// emission sites reach the session state by different names, so each renders
	// its own call rather than sharing one prebuilt expression.
	Templated bool
	RunLLM    bool
	Silent    bool
}

// livekitTransfer carries the shaped context of an agent_transfer (V5): a
// prebuilt Python expression for the handed-over ChatContext ("" = history:
// reset, the target starts fresh), an optional generated summarizer, and the
// userdata fields the transfer does not carry (context.variables subset).
type livekitTransfer struct {
	Method      string
	When        string
	TargetClass string
	Announce    string
	Requires    []string      // guard: refuse until these userdata fields are set (V7)
	CtxExpr     string        // Python expr for chat_ctx=; "" = reset
	Summary     *livekitChain // set for history: summary — _summarize before handoff
	ResetVars   []livekitVar
}

// livekitHumanTransfer lowers a human_transfer control (V6, SCHEMA N25): cold is
// a SIP REFER through the job context; warm awaits the prebuilt
// WarmTransferTask (beta.workflows, Beta on Python), which dials the person,
// plays hold music, briefs them from the chat context, and merges the calls.
type livekitHumanTransfer struct {
	Method string
	When   string
	// ToExpr is the cold-transfer destination as a SIP REFER **URI** expression:
	// a quoted `tel:`/`sip:` literal, or _refer_uri(os.environ["NAME"]) when the
	// target defers it to an env var (N26) whose value is only known on the call.
	ToExpr string
	// DialExpr is the same destination as a number to dial, for the warm path:
	// a quoted literal or os.environ["NAME"], with no URI scheme, because
	// WarmTransferTask's sip_call_to takes a phone number.
	DialExpr string
	Warm     bool
	// Briefing is the free-text `warm.briefing`, lowered as the extra slot of
	// the prebuilt's instructions. Empty means the prebuilt's own persona.
	Briefing string
	// RingTimeout is the emitted seconds value ("30.0"), empty when unset so the
	// platform default applies.
	RingTimeout string
	// Hangup is on_unavailable: hangup. False is return_to_caller, which hands
	// the model a refusal string and leaves the caller with the agent.
	Hangup bool
}

// livekitOutbound is the telephony outbound + AMD voicemail lowering (V8/N6).
type livekitOutbound struct {
	LeaveMessage bool // on_voicemail: leave_message (false = hangup)
}

// livekitTelephony is the exact self-hosted SIP route selected by Build. It
// contains environment-variable names and normalized variable mappings only;
// secret values never enter generation.
type livekitTelephony struct {
	// Transport is "sip" (carrier SIP trunk) or "connector" (our Twilio Media
	// Streams bridge). It selects the agent.py call path and the emitted files.
	Transport      string
	Carrier        string
	Connection     string
	ProviderDocs   string
	CredentialHint string
	SIPAddressEnv  string
	SIPUsernameEnv string
	SIPPasswordEnv string
	FromNumberEnv  string
	// Connector-only: the bridge's Twilio client and webhook signature check.
	AccountSIDEnv string
	AuthTokenEnv  string
	HasInbound    bool
	HasOutbound   bool
	HasWarm       bool
	Greeting      *livekitGreeting
	SystemSources []livekitSystemSource
	CallStart     []livekitCallStart
	// Plane and the three fields under it are the route's carrier-free
	// development plane, derived in internal/ir. The Compose file assigns the
	// addresses and the dev command dials them, so neither may compute them:
	// they read the one plan.
	Plane           string
	PlaneSubnet     string
	PlaneSIPAddress string
	PlaneServices   []planeService
}

type livekitSystemSource struct {
	Variable string
	Source   string
}

type livekitCallStart struct {
	Name      string
	Type      string
	TypeCheck string
	Required  bool
}

// livekitDelegate lowers a delegate control. A single task awaits its
// AgentTask directly (typed-result-only return, C4) and applies `assign`; a
// group builds a TaskGroup, awaits it, and hands control on per the group's
// `then`: return the typed results to the owner (merge: results), transfer to
// another agent, or end the call. N13: the flow's own turns never land in the
// owner's context regardless of which path is taken.
type livekitDelegate struct {
	Method          string
	When            string
	Task            *livekitSingleTask // set for a single-task delegate; Steps empty
	Steps           []livekitStep
	Isolated        bool   // context_scope: isolated — standalone-AgentTask sequence, no TaskGroup (C3)
	Then            string // "return" | "transfer" | "end"
	ThenClass       string // target Agent class, set only for then: transfer
	CanTaskTransfer bool   // one member task can hand the caller directly to another agent
}

// livekitSingleTask is the task side of a single-task delegate: the AgentTask
// class to await plus the `assign` writes into the typed userdata (N5). The
// task's own context (N12) shapes what the AgentTask sees on entry.
type livekitSingleTask struct {
	Class   string
	ID      string
	Assign  []livekitAssign
	CtxExpr string        // Python expr for chat_ctx=; "" = reset (fresh task)
	Summary *livekitChain // set for history: summary
}

type livekitAssign struct {
	Var   string
	Field string
}

// livekitVar is one typed shared-state field on the generated Userdata
// dataclass (SCHEMA 4.4; LiveKit session userdata).
type livekitVar struct {
	Name        string
	PyType      string
	Default     string // Python literal; "None" when the spec declares none
	Description string
}

// livekitCapture is the generated update_variables tool (V6): one optional
// argument per conversation variable, writing the session userdata.
type livekitCapture struct {
	Name        string
	Description string
	Args        []livekitArg
	Fields      []string
}

// livekitCallStartVar is one dispatched input variable, hydrated from the job
// metadata or the dev UNMUTE_CALL_START payload before the greeting.
type livekitCallStartVar struct {
	Name      string
	Type      string
	TypeCheck string
	Required  bool
}

type livekitStep struct {
	Class string
	ID    string
	Desc  string
}

type livekitTask struct {
	Name        string
	Class       string
	PromptConst string
	PromptExpr  string        // render call when the prompt is templated, else ""
	LLM         *livekitChain // per-task model override (B1); nil = session LLM
	Result      []livekitArg  // finish() args + the completed result dict
	Tools       []livekitTool
	Prebuilt    []livekitTool // execution: builtin tools, rendered into super().__init__(tools=...)
	MCPServers  []livekitMCPServer
	Transfers   []livekitTransfer
	// SlngScope is this task's own router cache scope, empty unless its think
	// profile is a router binding. A task's prompt is not its owner's, so its
	// scope is not its owner's either.
	SlngScope string
}

type livekitTool struct {
	Method           string
	Description      string
	URLEnv           string
	URLExpr          string          // request URL expression (webhook.path renders into it)
	Inject           []injectedValue // hidden request values, never advertised to the model
	Needed           []neededVar     // unset ones refuse the call before it is sent (V4)
	NeededLiteral    string          // Needed as a Python list of (name, hint) pairs
	JSONBody         string          // full Python dict literal for the webhook body
	CallKwargs       string          // full kwargs string for a local handler call
	Auth             *webhookAuth    // nil = unauthenticated POST
	Args             []livekitArg
	Local            bool   // execution: local — call the copied handler module
	Builtin          string // execution: builtin — prebuilt registry id (renders into tools=, not a method)
	KnowledgeBase    string // execution: knowledge — the base this tool searches
	Instructions     string // builtin end_call closing message → end_instructions
	EndsConversation bool   // effect: ends_conversation — shutdown after the call
	Announce         string // one fixed sentence spoken as the tool starts; "" = silent
}

// livekitMCPServer is one MCP tool source an agent or task mounts (N40): one
// MCPToolset on the tools surface, identified by the source name. An empty
// field emits no argument at all, so the SDK's own default stands: no
// transport_type means it reads the URL, no allowed_tools means every tool the
// server exposes, no headers means an unauthenticated connection.
type livekitMCPServer struct {
	Name      string
	URLEnv    string
	Transport string
	Tools     []string
	Auth      *webhookAuth
	AuthEnv   string // the token's env name, for the startup check
}

// livekitLocalTool is a copied handler file: tools/<name>.py in the project.
type livekitLocalTool struct {
	Name   string
	Source string
}

// livekitInterruption is the conversation.interruption block (V16): enabled
// and min_words lower to the session's InterruptionOptions; ignore phrases
// lower to the generated stt_node filter mixin.
type livekitInterruption struct {
	Enabled  bool
	MinWords int
}

type livekitArg struct {
	Name     string
	PyType   string
	Required bool
	Enum     []string // input `enum` → Literal[...] (V2)
	Desc     string   // per-property `description` → Annotated[..., Field(description=...)] (V2)
	Anno     string   // rendered Python annotation (PyType, Literal[...], or Annotated[...])
}

type livekitPrompt struct {
	Const string
	Body  string
}

// livekitDeploy is one row of the README's deploy commands: one per declared
// region, or a single region-less row when the package declares none.
type livekitDeploy struct {
	Region string
	// ConfigFile is empty for a single deployment, so its commands use the
	// platform's default file name; several regions get the platform's own
	// per-region naming (livekit.<region>.toml).
	ConfigFile string
}

type livekitData struct {
	Project           string
	Version           string
	DeploymentRegions []string
	Deploys           []livekitDeploy
	AgentName         string
	EntryAgent        string
	EntryClass        string
	STT               livekitChain
	SessionLLM        livekitChain
	SessionTTS        livekitService
	TurnVersion       string
	// EndpointingDelay is the authored VAD silence window in seconds, "" when
	// the package leaves the runtime default alone. It renders into the
	// prewarmed Silero VAD, not into turn_handling's endpointing: min_delay
	// cannot fire before the VAD reports end of speech, so a value below the
	// window is unreachable there.
	EndpointingDelay string
	Agents           []livekitAgent
	Tasks            []livekitTask
	Vars             []livekitVar
	CallStartVars    []livekitCallStartVar // dispatched input variables (I.dispatch)
	Capture          *livekitCapture       // generated update_variables tool; nil without conversation variables
	Secrets          []string              // declared secrets, for .env.example (V11)
	ExtraEnv         []string              // env the route needs that the package never declared
	RequiredSecrets  []string              // required secrets: a startup check refuses to run without them (V12)
	CallRequiredEnv  []string              // cold-transfer destinations checked only for a real phone call
	LocalTools       []livekitLocalTool    // copied handler files (tools/<name>.py)
	MCPServers       []livekitMCPServer    // unique mounted sources, used by the shared constructor + startup preflight
	Pins             map[string]string     // plugin pins (C6): raise dep floors
	Prompts          []livekitPrompt
	EntryPromptExpr  string   // templated entry prompt rendered from session.userdata after outbound SIP hydration
	PluginModules    []string // merged `from livekit.plugins import ...` names
	// Slng is the SLNG Context Router's module-level helpers. Empty on a package
	// with no router think binding, and then none of it is emitted (FR-019).
	Slng        slngHelpers
	Deps        []string
	RequiredEnv []string
	// Knowledge is the shared knowledge-module data: the declared bases and the
	// deduplicated embedding imports across them.
	Knowledge knowledgeData
	// AuthorEnv is the half of RequiredEnv the author supplies. Everything else
	// — the route's own values, which `unmute dev` sets locally and a platform
	// or operator sets at deploy time — is absent from every author-facing file
	// (FR-018). RequiredEnv stays the complete list, and the compile report
	// keeps it, so hiding is not deleting.
	AuthorEnv []string
	// SuppliedForYou is the route's locally-supplied set: names `unmute dev`
	// sets locally and the platform or operator sets at deploy time. They are
	// absent from .env.example (FR-018), so a startup check that hits one has to
	// say where the value comes from rather than only that it is missing, or the
	// operator is told to set a variable no file ever mentioned (research D14).
	SuppliedForYou       []string
	DevEnv               []string // provider creds the web dev image needs (LIVEKIT_* are hardcoded in compose.dev.yaml)
	HandoffControls      []string // control names dev_metrics.py must not report as tools
	DevOptionalEnv       []string // passed through when the host sets it, never required (UNMUTE_CALL_START)
	Notes                []string
	Tracing              bool
	TracingProvider      string
	OpenAIResponses      bool
	NeedsOpenAIReasoning bool

	NeedsTasks         bool        // AgentTask import
	Unserved           livekitArg  // the reserved finish arg: a request the step could not serve
	NeedsTaskGroups    bool        // beta.workflows TaskGroup import
	NeedsFunctionTools bool        // RunContext + function_tool imports
	TypingImports      string      // `from typing import ...` names (Annotated/Literal), "" if none (V2)
	NeedsField         bool        // `from pydantic import Field` — any tool arg carries a description (V2)
	SingleAgentMinimal bool        // one agent, never a handoff target: drop the chat_ctx ctor plumbing (F3)
	NeedsLLM           bool        // the `llm` module import (chat_ctx param, fallback chains, or history helpers)
	NeedsHTTPX         bool        // any webhook tool
	NeedsRender        bool        // any template site: the _render helper + re import
	NeedsRefusal       bool        // any tool whose injected variables can be unset (V4)
	AuthKinds          authKindSet // webhook auth schemes in use: helpers + imports per scheme
	HasVars            bool        // session userdata carries the package's variables
	// HasUserdata says the Userdata object is emitted at all. Variables need it,
	// and so does a router package with none: the per-call session id lives on
	// it, because the header set now travels per request and every agent and
	// task class has to be able to reach the value from a method body.
	HasUserdata      bool
	NeedsLastN       bool // the _last_n history helper
	NeedsSummarize   bool // the _summarize history helper
	NeedsAsyncio     bool // inactivity end / max_duration timers
	NeedsInspect     bool // local tool wrappers (isawaitable)
	NeedsMCP         bool // mcp import (MCPServerHTTP)
	NeedsEndCallTool bool // beta.tools EndCallTool import (prebuilt end_call)
	HasColdTransfer  bool // get_job_context import
	HasWarmTransfer  bool // WarmTransferTask import + trunk env + room_options (B14)
	HasTaskTransfers bool // _TaskTransfer sentinel + task delegate catch paths
	// HasToolAnnouncements gates the README section only: the emitted speech is
	// per-tool and needs no import, so nothing in agent.py reads this.
	HasToolAnnouncements bool
	Outbound             *livekitOutbound
	// CarrierSteps are the route's dictated carrier actions, rendered verbatim
	// into the runbook. internal/target owns this text: a runbook that restates
	// it in its own prose has made a second copy of a one-owner fact, and the
	// copy is the one that goes stale. Gate C5 asserts every step survives.
	CarrierSteps []string
	Telephony    *livekitTelephony

	// Conversation shaping (V16).
	ThinkingAudio          bool // subtle → BackgroundAudioPlayer thinking sound
	Interruption           *livekitInterruption
	IgnorePhrases          []string // generated stt_node filter mixin
	InactivityNudgeSecs    int
	InactivityEndSecs      int
	InactivityEndDeltaSecs int // end_after minus nudge_after, floored at 1s
	MaxDurationSecs        int
	MaxSessions            int // telephony worker admission cap
	DrainTimeoutSecs       int // bound graceful shutdown by the longest session
}

// livekitEmittedFields declares every capability field the LiveKit emitter has
// a real code path for. It MUST equal the table's non-gated LiveKit rows — the
// V15 agreement test enforces it, so a field can never be validate-green while
// the emitter silently drops it (B1). Add a row here only with the code.
var livekitEmittedFields = map[targetcap.Field]bool{
	targetcap.FieldListenLocal:           true, // placement forwarded (code target runs it locally)
	targetcap.FieldSpeakLocal:            true,
	targetcap.FieldReasonLocal:           true,
	targetcap.FieldTurnPlacement:         true, // advisory (Inference turn detection supplied)
	targetcap.FieldSemanticEndpointing:   true, // advisory
	targetcap.FieldEndpointingDelay:      true, // prewarmed VAD min_silence_duration
	targetcap.FieldFallback:              true, // llm.FallbackAdapter (V4)
	targetcap.FieldListenFallback:        true, // stt.FallbackAdapter (T16)
	targetcap.FieldTask:                  true, // AgentTask; single delegate awaits it (T12)
	targetcap.FieldTaskModel:             true, // AgentTask(llm=...) (T14, B1)
	targetcap.FieldTaskNestedResult:      true, // dict finish arg
	targetcap.FieldTaskGroup:             true, // beta.workflows TaskGroup (warn: experimental)
	targetcap.FieldTaskGroupReturn:       true, // N13 snapshot/restore + task_results
	targetcap.FieldContextIsolated:       true, // standalone-AgentTask sequence (T13)
	targetcap.FieldTransferAnnounce:      true, // awaited outgoing reply before handoff (N44)
	targetcap.FieldTransferRequires:      true, // generated guard naming unmet vars (V7)
	targetcap.FieldContextNoToolCalls:    true, // copy(exclude_function_call=True)
	targetcap.FieldContextVariableSubset: true, // uncarried userdata fields reset (D7)
	targetcap.FieldTransferBriefing:      true, // WarmTransferTask instructions extra (N25)
	targetcap.FieldGreetingUserFirst:     true,
	targetcap.FieldGreetingModelWritten:  true,
	targetcap.FieldGreetingAbsent:        true,
	targetcap.FieldInterruptionMinWords:  true, // TurnHandlingOptions interruption min_words
	targetcap.FieldInterruptionIgnore:    true, // generated stt_node filter mixin
	targetcap.FieldInactivity:            true, // user_away_timeout + away handler
	targetcap.FieldMaxDuration:           true, // asyncio shutdown timer
	targetcap.FieldThinkingAudio:         true, // BackgroundAudioPlayer thinking sound
	targetcap.FieldToolOutput:            true, // tool returns response.json()
	targetcap.FieldToolLocal:             true, // handler copied + wrapped
	targetcap.FieldToolBuiltin:           true, // prebuilt end_call → beta EndCallTool
	targetcap.FieldToolKnowledge:         true, // knowledge.py + one @function_tool per lookup
	targetcap.FieldToolKnowledgeTask:     true, // the same method on an AgentTask's tools surface
	targetcap.FieldToolMCP:               true, // mcp.MCPToolset mounts on the tools surface (N40)
	targetcap.FieldToolMCPTask:           true, // the same mount on an AgentTask's tools surface
	targetcap.FieldToolAuth:              true, // _bearer Authorization header off token_env
	targetcap.FieldToolInterruption:      true, // warn: runs to completion
	targetcap.FieldToolAnnounce:          true, // unawaited session.say() before the request
	targetcap.FieldToolAnnounceTask:      true, // task tools share method_tool, so the same line
	targetcap.FieldOutbound:              true, // SIP dial-out off job metadata
	targetcap.FieldVoicemail:             true, // AMD machine-vm branches (N6)
	targetcap.FieldTracingLangfuse:       true,
	targetcap.FieldTracingCoval:          true, // tracing.py exports to Coval off the SIP simulation ID
	targetcap.FieldDeploymentMultiRegion: true, // one README deploy row per declared region, own config file
	targetcap.FieldVariableConversation:  true, // generated update_variables @function_tool writing userdata
	targetcap.FieldToolInject:            true, // hidden request values merged from userdata
	targetcap.FieldWebhookPath:           true, // rendered, URL-encoded path on the base URL
	targetcap.FieldTemplates:             true, // update_instructions/_render at session start
}

var livekitEmittedTelephonyFeatures = map[targetcap.TelephonyFeature]bool{
	targetcap.TelephonyRouteSelected:                         true,
	targetcap.TelephonyInbound:                               true,
	targetcap.TelephonyOutbound:                              true,
	targetcap.TelephonyFeature(targetcap.Hangup):             true,
	targetcap.TelephonyFeature(targetcap.ColdTransfer):       true,
	targetcap.TelephonyFeature(targetcap.WarmTransfer):       true,
	targetcap.TelephonyFeature(targetcap.VoicemailDetection): true,
	"source.session_id":                                      true,
	"source.carrier":                                         true,
	"source.connection":                                      true,
	"source.call_id":                                         true,
	"source.direction":                                       true,
	"source.from_number":                                     true,
	"source.to_number":                                       true,
}

// The LiveKit Twilio connector emits inbound, outbound, and hangup only;
// transfers ride the platform's native SIP primitives, which need a SIP
// participant and an outbound trunk, and this route has neither (SPEC C1).
// Voicemail detection has no AMD lowering here yet. It carries a Twilio
// stream id (source.stream_id) since it rides Twilio Media Streams.
var livekitConnectorEmittedTelephonyFeatures = map[targetcap.TelephonyFeature]bool{
	targetcap.TelephonyRouteSelected:             true,
	targetcap.TelephonyInbound:                   true,
	targetcap.TelephonyOutbound:                  true,
	targetcap.TelephonyFeature(targetcap.Hangup): true,
	"source.session_id":                          true,
	"source.carrier":                             true,
	"source.connection":                          true,
	"source.call_id":                             true,
	"source.stream_id":                           true,
	"source.direction":                           true,
	"source.from_number":                         true,
	"source.to_number":                           true,
}

// GenerateLiveKit lowers a validated agent + livekit target into a project. The
// socket runs Validate(caps) first (V17), so this reads only agent+target.
func GenerateLiveKit(agent *ir.Agent, target ir.Target, bindings []ir.ForwardedBinding, sizing []ir.Sizing) (Artifact, error) {
	if err := checkLiveKitVersion(target.Version); err != nil {
		return Artifact{}, err
	}
	data, err := buildLiveKitData(agent, target)
	if err != nil {
		return Artifact{}, err
	}

	files, err := renderLiveKitFiles(data)
	if err != nil {
		return Artifact{}, err
	}
	report, err := livekitReport(agent, data, files, bindings, sizing)
	if err != nil {
		return Artifact{}, err
	}
	files = append(files, File{Path: "compile-report.json", Content: report})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	return Artifact{
		Kind:  CodeTarget,
		Files: files,
		Notes: GenerateReport{Notes: data.Notes},
	}, nil
}

// checkLiveKitPins validates plugin pins (C6). The pinnable set, the floors, and
// the comparison all live in internal/target now, because ir.Validate asks the
// same three questions and used to ask none of them: a bad pin validated green
// and failed compile. This stays as the driver's backstop, and the pin still
// raises the dep floor in livekitDeps.
func checkLiveKitPins(pins map[string]string) error {
	return targetcap.CheckPins(targetcap.LiveKit, pins)
}

// checkLiveKitVersion rejects a framework version outside the templates' range.
func checkLiveKitVersion(version string) error {
	return targetcap.CheckVersion(targetcap.LiveKit, version)
}

// livekitDeploys turns declared regions into the README's deploy rows. No region
// declared is still one row: the commands are the same, minus the flag, and the
// README says the platform will ask which region to use. Several regions become
// one deployment each, named the platform's way so `create` does not refuse on
// the second one.
func livekitDeploys(regions []string) []livekitDeploy {
	if len(regions) == 0 {
		return []livekitDeploy{{}}
	}
	deploys := make([]livekitDeploy, 0, len(regions))
	for _, region := range regions {
		row := livekitDeploy{Region: region}
		if len(regions) > 1 {
			row.ConfigFile = "livekit." + region + ".toml"
		}
		deploys = append(deploys, row)
	}
	return deploys
}

func renderLiveKitFiles(data livekitData) ([]File, error) {
	outputs := []struct{ tmpl, path string }{
		{"agent.py", "agent.py"},
		// Always emitted, inert unless the dev loop sets devmetrics.Env. Emitting
		// it only for `dev` would make build/<target>/ depend on which command
		// last ran, so the dev loop would stop testing the file that ships.
		{"dev_metrics.py", "dev_metrics.py"},
		{"pyproject.toml", "pyproject.toml"},
		{"README.md", "README.md"},
		{"env.example", ".env.example"},
		{"Dockerfile", "Dockerfile"},
		{"compose.dev.yaml", "compose.dev.yaml"},
		// No livekit.toml: both of its values (project subdomain, CA_ agent id)
		// are assigned by LiveKit Cloud, and `lk agent create` refuses to run
		// when a file of that name already exists. The platform writes it on the
		// first deploy and unmute compile preserves it from then on.
	}
	if data.Tracing {
		outputs = append(outputs, struct{ tmpl, path string }{tracingTemplate(data.TracingProvider), "tracing.py"})
	}
	// Only when the package declares a knowledge base: the module imports
	// llama-index, which is only in .Deps for the same reason.
	connector := data.Telephony != nil && data.Telephony.Transport == "connector"
	planeFiles := false
	if data.Telephony != nil {
		// The connector runs the app plus a local LiveKit Server only (no Redis,
		// no SIP bridge); its Compose and the bridge process differ from SIP.
		if connector {
			outputs = append(outputs,
				struct{ tmpl, path string }{"compose.telephony.connector.yaml", "compose.telephony.yaml"},
				struct{ tmpl, path string }{"telephony_bridge.py", "telephony_bridge.py"},
			)
		} else {
			outputs = append(outputs, struct{ tmpl, path string }{"compose.telephony.yaml", "compose.telephony.yaml"})
			// The plane's endpoint image and the SIP configuration it carries.
			// Read the plane rather than the transport: the plane is the route
			// fact that says a carrier-free loop exists, and Phase C's Pipecat
			// SIP route gets these for the same reason without a second branch.
			// The plane's own files come from the plane, not from this driver:
			// the Pipecat SIP route runs the same endpoints and must not have a
			// second copy of them.
			if data.Telephony.Plane == string(targetcap.LocalPlaneSIP) {
				planeFiles = true
			}
		}
	}
	var files []File
	for _, o := range outputs {
		content, err := renderLiveKitV1(o.tmpl, data)
		if err != nil {
			return nil, err
		}
		if o.tmpl == "compose.telephony.yaml" {
			// The plane's own topology is shared with the Pipecat SIP route, so
			// this template holds only this driver's application service and the
			// plane wraps it. One owner for the services both routes run.
			content, err = renderSIPPlaneCompose(sipPlaneCompose{
				ApplicationService: string(content),
				PlaneSubnet:        data.Telephony.PlaneSubnet,
				PlaneServices:      data.Telephony.PlaneServices,
			})
			if err != nil {
				return nil, err
			}
		}
		files = append(files, File{Path: o.path, Content: content})
	}
	// The operator's one command plus the endpoint image: plane artifacts, so the
	// plane emits them. The script resolves the trunk by phone number and
	// substitutes the JSON inputs itself, so no record ID is ever transcribed.
	if planeFiles {
		emitted, err := sipPlaneFiles(sipPlaneSetup{
			Project: data.Project, AgentName: data.AgentName,
			Carrier: data.Telephony.Carrier, FromNumberEnv: data.Telephony.FromNumberEnv,
			// This driver's agent *is* a LiveKit worker, so the emitted rule
			// dispatches it.
			DispatchesWorker: true,
			TracingProvider:  data.TracingProvider,
		}, data.Telephony.HasInbound)
		if err != nil {
			return nil, err
		}
		files = append(files, emitted...)
	}
	// The knowledge module is the shared one, rendered from templates/knowledge/
	// rather than from this driver's tree: it has no framework in it, so a second
	// copy would be a second owner of one surface.
	if len(data.Knowledge.Knowledge) > 0 {
		content, err := renderKnowledgeModule(data.Knowledge)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: "knowledge.py", Content: content})
	}
	// Both clouds build from this directory, and LiveKit caps the uploaded
	// context at 1 GB, so local run leftovers are excluded too.
	files = append(files, File{Path: ".dockerignore", Content: []byte(".env\n.env.*\n.venv/\n__pycache__/\n")})
	// Local tool handlers are copied verbatim from the source package (SCHEMA
	// §5: code targets host the handler).
	if len(data.LocalTools) > 0 {
		files = append(files, File{Path: "tools/__init__.py", Content: []byte("")})
		for _, lt := range data.LocalTools {
			files = append(files, File{Path: "tools/" + lt.Name + ".py", Content: []byte(lt.Source)})
		}
	}
	return files, nil
}

func renderLiveKitV1(name string, data livekitData) ([]byte, error) {
	raw, err := livekitV1Templates.ReadFile("templates/livekit_v1/" + name + ".tmpl")
	if err != nil {
		return nil, fmt.Errorf("livekit template %s: %w", name, err)
	}
	tmpl, err := template.New(name).Funcs(template.FuncMap{
		"pyq":        pyQuote,
		"join":       strings.Join,
		"triple":     pyTriple,
		"mcpTimeout": func() int { return mcpTimeoutSeconds },
	}).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("livekit template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("livekit template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// pyTriple renders a Go string as a Python triple-quoted string literal, safe
// for a multi-line prompt body.
func pyTriple(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"""`, `\"\"\"`)
	return `"""` + s + `"""`
}

type livekitReportJSON struct {
	Target      string                `json:"target"`
	Provider    string                `json:"provider"`
	Version     string                `json:"version"`
	Supported   *reportSupported      `json:"supported,omitempty"`
	EntryAgent  string                `json:"entry_agent"`
	Agents      []string              `json:"agents"`
	Tasks       []string              `json:"tasks,omitempty"`
	Files       []string              `json:"generated_files"`
	Regions     []string              `json:"deployment_regions,omitempty"`
	RequiredEnv []string              `json:"required_env"`
	Bindings    []ir.ForwardedBinding `json:"bindings,omitempty"`
	Sizing      []ir.Sizing           `json:"sizing,omitempty"`
	Variables   []reportVariable      `json:"variables,omitempty"`
	Secrets     []reportSecret        `json:"secrets,omitempty"`
	Notes       []string              `json:"notes,omitempty"`
}

func livekitReport(agent *ir.Agent, data livekitData, files []File, bindings []ir.ForwardedBinding, sizing []ir.Sizing) ([]byte, error) {
	generated := make([]string, 0, len(files)+1)
	for _, file := range files {
		generated = append(generated, file.Path)
	}
	generated = append(generated, "compile-report.json")
	slices.Sort(generated)
	agents := make([]string, 0, len(data.Agents))
	for _, a := range data.Agents {
		agents = append(agents, a.Name)
	}
	tasks := make([]string, 0, len(data.Tasks))
	for _, t := range data.Tasks {
		tasks = append(tasks, t.Name)
	}
	out, err := json.MarshalIndent(livekitReportJSON{
		Target: data.Project, Provider: "livekit", Version: data.Version,
		Supported: supportedRange(targetcap.LiveKit), EntryAgent: data.EntryClass,
		Agents: agents, Tasks: tasks, Files: generated,
		// Forwarded without checking, so it must be readable back (constitution).
		Regions: data.DeploymentRegions, RequiredEnv: data.RequiredEnv,
		Bindings: bindings, Sizing: sizing,
		Variables: reportVariables(agent), Secrets: reportSecrets(agent),
		Notes: data.Notes,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
