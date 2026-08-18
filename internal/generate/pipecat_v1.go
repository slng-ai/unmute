package generate

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// The Pipecat driver lowers the resolved IR into a runnable project via two
// mechanisms, each on Pipecat's recommended path (C8): a main PipelineWorker
// owns transport + STT, each agent is an LLMWorker with its own LLM + TTS,
// agent_transfer is activate_worker(), and tasks/task_groups run as Pipecat
// Flows (FlowManager) on the owning worker. Python is emitted only through
// these templates (C1/ADR-0002).
//
//go:embed templates/pipecat_v1/*.tmpl
var pipecatV1Templates embed.FS

// The driver's templates target the Pipecat workers model (LLMWorker /
// activate_worker) + Flows-in-core. The exact supported SDK version lives in
// internal/target/driver.go so ir.Validate reads the same contract.

// pyName turns a snake/kebab identifier into a safe Python class-name stem.
func pyName(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' })
	for i, part := range parts {
		if part != "" {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "")
}

// pipecatService is a resolved provider binding: the rendered constructor
// (Call) plus its catalogue entry (imports/install), and the raw identity the
// task job-workers need to drive the OpenAI SDK directly. Model/voice/params
// are forwarded verbatim (C11); the catalogue only picks the code slot.
type pipecatService struct {
	Call      ServiceCall
	Entry     targetcap.Entry
	Vendor    string // resolved binding provider (report labeling)
	APIKeyEnv string
	BaseURL   string // env var name for base_url, empty if native
	Model     string
}

type pyKV struct {
	Key   string
	Value string // already a Python literal
}

// pipecatAgent is one LLMWorker: its class, worker name, LLM, TTS, prompt, and
// the tools/transfers/delegates it exposes as @tool methods.
type pipecatAgent struct {
	Name        string // worker name (the agent's snake_case id)
	Class       string // Python class name
	Prompt      string
	PromptConst string // module constant holding Prompt (dedup: builder + restore, V2)
	PromptExpr  string // constructor expression: PromptConst, or _render(..., state)
	// RuntimePromptExpr is the same prompt inside an agent method, where call
	// state is reached through self rather than the constructor-local name.
	RuntimePromptExpr string
	LLM               pipecatService
	TTS               pipecatService
	Tools             []pipecatTool
	Transfers         []pipecatTransfer
	Delegates         []pipecatDelegate
	MCPSources        []pipecatMCPSource
	FlowFunctionNames []string // task-node handlers sharing this worker's LLM registry
}

// pipecatMCPSource is one MCP tool source this agent carries (N40): one
// MCPClient, started before the agent is activated and closed on shutdown. The
// server names its own tools, so nothing here describes them; Var is the
// Python name the emitted client and its tools schema share.
type pipecatMCPSource struct {
	Name      string
	Var       string // <name>_mcp
	URLEnv    string
	Transport string       // "" = pick the parameter class from the URL at startup
	Tools     []string     // empty = every tool the server exposes
	Auth      *webhookAuth // nil = no headers argument
	AuthEnv   string       // the token's env name, for the startup check
}

// ParamsClass is the mcp parameter class this source constructs, or "" when the
// transport is unstated and the bot chooses at startup.
func (s pipecatMCPSource) ParamsClass() string {
	switch s.Transport {
	case ir.MCPTransportSSE:
		return "SseServerParameters"
	case ir.MCPTransportStreamableHTTP:
		return "StreamableHttpParameters"
	}
	return ""
}

// pipecatTask is one guided conversational step lowered to a Flow node (C8, B7):
// its instructions, tools, and a uniquely named finish function derived from the
// result schema (V1). Nodes are emitted inline in the owning delegate's methods.
type pipecatTask struct {
	Name           string // node id (the task's snake_case id)
	FinishName     string // LLM-visible "finish_<delegate>_<task>" — unique so a sticky handler registration can never run a stale step (V1)
	NextName       string // next step's node in this delegate's chain; "" on the last step
	Prompt         string
	PromptExpr     string // the node's role_message: the quoted prompt, or a render call when it names a variable
	Tools          []pipecatTool
	Transfers      []pipecatTransfer
	ResultProps    string // Python literal: JSON-schema properties for finish args
	ResultRequired string // Python literal: list of required finish arg names
}

// pipecatDelegate is a delegate control: run a task or an ordered group of tasks
// as a Flow on the owning worker, then return / transfer / end (C8, V2).
type pipecatDelegate struct {
	MethodName   string
	When         string
	Task         string          // single-task delegate; "" if a group
	Assign       []pipecatAssign // result.<field> -> variable
	Group        string          // group delegate; "" if a single task
	StepTasks    []pipecatTask   // resolved ordered steps (a single task is one step)
	Then         string          // "return" | "transfer" | "end"
	ThenTarget   string          // target agent for then: transfer
	Isolated     bool            // context_scope: isolated (per-node context RESET)
	HasTransfers bool            // a step can abort the remaining Flow and hand off
}

type pipecatAssign struct {
	Var   string
	Field string
}

// pipecatTool is a webhook or local tool exposed as an @tool method: webhook
// POSTs to url_env, local awaits the user's handler from tools/<name>.py (V13).
// Inside a Flow node the same tool is instead a module-level flows handler; the
// InputProps/InputRequired literals carry its schema onto the FlowsFunctionSchema.
type pipecatTool struct {
	Name            string
	MethodName      string
	Description     string
	URLEnv          string
	URLExpr         string          // request URL expression (webhook.path renders into it)
	Inject          []injectedValue // hidden request values, never advertised to the model
	Needed          []neededVar     // unset ones refuse the call before it is sent (V4)
	NeededLiteral   string          // Needed as a Python list of (name, hint) pairs
	NeedsState      bool            // reads the call state: inside a Flow node it must be bound in
	JSONBody        string          // full Python dict literal for the webhook body
	CallKwargs      string          // full kwargs string for a local handler call
	Auth            *webhookAuth    // nil = unauthenticated POST
	Local           bool            // execution: local — body imports + awaits tools/<name>.py (V13)
	HandlerSource   string          // local handler file content, copied into the artifact
	Args            []pipecatArg
	InputProps      string // Python literal: the input schema's properties object
	InputRequired   string // Python literal: the input schema's required list
	Builtin         string // execution: builtin — prebuilt registry id (bodyless end tool)
	Instructions    string // builtin end_call goodbye → developer message before EndFrame
	EndsCall        bool
	Interruption    string // "cancel" | "continue" | "" (provider default)
	ColdDestination string // set for a cold human_transfer: the resolved number/SIP URI (Daily SIP only)
	// RingTimeoutSecs is the cold block's ring_timeout in seconds, or the route
	// default. It reaches the carrier document, so a declared value is honoured.
	RingTimeoutSecs int
	// HangupOnUnavailable is the cold block's on_unavailable: hangup — a failed
	// transfer says a goodbye and ends the call instead of returning the model
	// a failure string (T5).
	HangupOnUnavailable bool
}

// pipecatLocalTool is a copied handler file: tools/<name>.py in the project.
type pipecatLocalTool struct {
	Name   string
	Source string
}

// pipecatArg is one agent-level @tool parameter. Pipecat derives the tool schema
// from the method signature + Google-style docstring, so the declared type,
// per-property description, and enum all ride across (V1): PyType/PyDefault shape
// the signature; Description + Enum become the `Args:` docstring lines (Pipecat's
// direct-function generator does not map Literal→JSON enum, so enums are prose).
type pipecatArg struct {
	Name        string
	PyType      string // signature annotation: str/int/float/bool/list/dict (+ " | None" for optional complex)
	PyDefault   string // default literal for an optional arg (rendered as ` = <PyDefault>`); unused when Required
	Required    bool
	Description string
	Enum        []string
}

// pipecatTransfer is an agent_transfer control lowered to activate_worker.
type pipecatTransfer struct {
	MethodName string
	To         string // target worker name
	When       string
	Announce   string   // optional source-worker speech before activation
	Reason     string   // developer message injected on activation
	Requires   []string // variables that must be set before the handoff (guard)
}

type pipecatVariable struct {
	Name        string
	PyType      string
	Default     string // Python literal
	Source      string
	Description string
}

// pipecatCapture is the generated update_variables tool (V6): one optional
// argument per conversation variable, writing the shared State.
type pipecatCapture struct {
	Name        string
	Description string
	Args        []pipecatArg
	Fields      []string // conversation variable names, in schema order
}

// pipecatCallStartVar is one dispatched input variable, hydrated from the call
// context or the dev UNMUTE_CALL_START payload before the greeting.
type pipecatCallStartVar struct {
	Name     string
	Type     string
	Required bool
}

type pipecatTelephony struct {
	Carrier       string
	Connection    string
	MaxSessions   int
	SessionTTL    int
	AccountSIDEnv string
	AuthIDEnv     string
	AuthTokenEnv  string
	APIKeyEnv     string
	PublicKeyEnv  string
	ConnectionEnv string
	FromNumberEnv string
	HasInbound    bool
	HasOutbound   bool
	SystemSources []pipecatSystemSource
	CallStart     []pipecatCallStart
}

// pipecatDailyCarrier is the (pipecat, daily-sip, twilio) data group: the
// carrier leg on the Daily route (SCHEMA N37).
//
// It is deliberately a *separate* field from pipecatTelephony rather than a
// widening of it. Twenty-two emitted sites read `.Telephony` as "this is a
// carrier-websocket route" — nine in README.md.tmpl, eleven in bot.py.tmpl, one
// each in Dockerfile.tmpl and pyproject.toml.tmpl — plus four in the driver's Go
// (pipecat_v1.go's artifact branch, pipecat_v1_build.go's carrier deps,
// buildPipecatTelephony, and inlineEligible). Giving this route a
// pipecatTelephony would arm every one of them, and a missed narrowing fails
// quietly: a carrier build would gain the whole carrier-websocket artifact set
// and lose its deploy manifest. A second field cannot arm any of them, so the
// trap never exists (research item 1, task T011a).
type pipecatDailyCarrier struct {
	Carrier    string
	Connection string
	// Connection env names (SCHEMA N37's key set). No sip_username or
	// sip_password: Daily's dial-out carries no SIP credential auth on any
	// documented surface, so termination authenticates by IP allow-list.
	AccountSIDEnv string
	AuthTokenEnv  string
	SIPAddressEnv string
	FromNumberEnv string
	HasInbound    bool
	HasOutbound   bool
	// AgentEnv and HelperEnv split the required environment by who reads it. The
	// deployed agent reads AgentEnv; the operator-run helper reads HelperEnv and
	// OptionalEnv where they run it. The carrier auth token is deliberately in
	// AgentEnv because both the bot and helper read that shared route credential.
	AgentEnv  []string
	HelperEnv []string
	// CallEnv is what a *phone call* adds to the agent's own environment: the
	// carrier names, and nothing the process already checks at startup. A browser
	// or console session on this package reads none of them, so asking for them
	// unconditionally would break the two modes that work with no phone at all.
	CallEnv     []string
	OptionalEnv []string
}

// pipecatCloudWebsocket is the (pipecat, cloud-websocket, twilio) data group:
// the carrier streams the call straight to Pipecat Cloud and the operator hosts
// nothing (SCHEMA N38).
//
// A third separate field, for the reason pipecatDailyCarrier is a second one:
// every site that reads `.Telephony` means "carrier-websocket, with a helper
// process and a Redis", and every site that reads `.DailyCarrier` means "a SIP
// leg into a Daily room". This route is neither, and a group that half-matches
// either would arm emitted code that cannot work here. Three narrow fields
// cannot make that mistake; one widened field would only have to avoid it.
type pipecatCloudWebsocket struct {
	Carrier    string
	Connection string
	// Connection env names. All three are empty on a pure-inbound package, which
	// declares no connection at all because the platform receives the call without
	// credentials (research F4, D4).
	AccountSIDEnv string
	AuthTokenEnv  string
	FromNumberEnv string
	// OrganizationEnv completes the service host for outbound markup; the compiler
	// knows the agent name and cannot know the organization (research D5).
	OrganizationEnv string
	HasInbound      bool
	HasOutbound     bool
	// StreamURL is the platform's carrier endpoint, regional when the target
	// declares a region. Computed once, in one place, and read by the Bin and the
	// outbound command, so the two cannot disagree about where the audio goes.
	StreamURL string
	// CallEnv is what a *phone call* adds to the process environment: nothing on a
	// pure-inbound package, the carrier names and any outbound organization otherwise. A
	// browser or console session on the same package reads none of them.
	CallEnv []string
}

type pipecatSystemSource struct {
	Variable string
	Source   string
}

type pipecatCallStart struct {
	Name     string
	Type     string
	Required bool
}

// pipecatTransportParams is the parameter object one transport key constructs,
// plus the import that class needs. The two travel as one unit so an emitted
// class structurally cannot lose its import — the same invariant the provider
// catalogue holds for service classes (data-model TransportParamsClass).
type pipecatTransportParams struct {
	Class string
	// Transport is the transport class the route's transfer primitive lives on,
	// set only when the package emits a transfer. The tool narrows to it before
	// calling the primitive, because the primitive is not on BaseTransport.
	Transport string
	Import    string
}

type pipecatData struct {
	Project          string
	Version          string
	DeploymentRegion string
	// SecretSet names the platform secret set the README tells the operator to
	// create; empty when the package declares no secrets. The manifest names it
	// so a deploy that skipped that step fails at deploy time rather than on a
	// live call.
	SecretSet           string
	MainName            string
	EntryAgent          string
	EntryClass          string
	STT                 pipecatService
	Agents              []pipecatAgent
	FlowTools           []pipecatTool      // deduped task tools, emitted as module-level flows handlers
	LocalTools          []pipecatLocalTool // copied handler files (tools/<name>.py, V13)
	Variables           []pipecatVariable
	CallStartVars       []pipecatCallStartVar // dispatched input variables (I.dispatch)
	Capture             *pipecatCapture       // generated update_variables tool; nil without conversation variables
	Secrets             []string              // declared secrets, for .env.example (V11)
	ExtraEnv            []string              // env the route needs that the package never declared
	GreetingExpr        string                // Python expression for the fixed greeting line
	GreetingText        string
	GreetingInstruction string
	GreetingRunLLM      string // "True" or "False"
	Interrupt           *pipecatInterrupt
	Inactivity          *pipecatInactivity
	MaxDurationSecs     int
	// NeedsEndAfter emits the _end_after helper, which the max-duration cap and
	// the inactivity hangup share.
	NeedsEndAfter bool
	// NeedsUserMute emits the always-mute strategy for interruption.enabled:
	// false, which is how Pipecat 1.5 expresses "the caller cannot barge in".
	NeedsUserMute   bool
	HasColdTransfer bool
	Transport       string
	// DailyParams is the parameter object the "daily" transport key constructs on
	// the Daily route, where the generic one cannot work. Nil everywhere else, so
	// no other route churns and no package carries a Daily import it never uses.
	DailyParams  *pipecatTransportParams
	FrameImports []string // pipecat.frames.frames names, merged into one import (V2)
	Imports      []string
	Extras       []string
	Deps         []string // standalone pip deps for plugin services (e.g. pipecat-slng)
	RequiredEnv  []string
	// AuthorEnv is the half of RequiredEnv the author supplies; see the LiveKit
	// driver's field of the same name.
	AuthorEnv []string
	// SuppliedForYou is the route's locally-supplied set: names `unmute dev`
	// sets locally and the platform or operator sets at deploy time. They are
	// absent from .env.example (FR-018), so a startup check that hits one has to
	// say where the value comes from rather than only that it is missing, or the
	// operator is told to set a variable no file ever mentioned (research D14).
	SuppliedForYou []string
	DevEnv         []string // provider creds the web dev image needs, without telephony/coordination env (compose.dev.yaml)
	DevOptionalEnv []string // passed through when the host sets it, never required
	Notes          []string
	Tracing        bool
	// Telephony means the carrier-websocket route, and every site that reads it
	// means that. The Daily carrier leg is DailyCarrier; see its doc comment.
	Telephony    *pipecatTelephony
	DailyCarrier *pipecatDailyCarrier
	// CloudWebsocket is the platform-terminated carrier stream (SCHEMA N38): the
	// one route where the operator hosts nothing. See its type's doc comment for
	// why it is a third field rather than a widening of either other one.
	CloudWebsocket *pipecatCloudWebsocket
	// Prerequisites are the route's account features the provider grants on
	// request, read from the rulebook in internal/target and never restated here.
	// Present only when this package uses something that needs one.
	Prerequisites []targetcap.RouteAccountPrerequisite

	// Import needs: keep bot.py free of unused imports (only what a given spec
	// actually exercises), so the emitted pipeline reads clean.
	NeedsInspect             bool        // any local tool (isawaitable on the user handler, V13)
	NeedsRender              bool        // any template site: the _render helper + re import
	NeedsStateBind           bool        // any flow tool reading state (inject inside a task)
	NeedsRefusal             bool        // any tool whose injected variables can be unset (V4)
	NeedsHTTPX               bool        // any webhook tool (agent @tool or flows handler)
	AuthKinds                authKindSet // webhook auth schemes in use: helpers + imports per scheme
	NeedsFunctionCalls       bool        // any @tool/transfer/delegate (FunctionCallParams)
	NeedsTurnStrategies      bool        // interruption min-words strategy
	NeedsEndFrame            bool
	NeedsAppendFrame         bool
	HasFlows                 bool // any delegate (tasks run as Flows on the owner, C8)
	HasTaskTransfers         bool // a task can transfer; imports the public NO_RESPONSE sentinel
	HasTransferAnnouncements bool // target worker owns exact handoff speech before its reply
	HasIsolated              bool // any isolated group (ContextStrategy RESET import)
	NeedsLanguage            bool // any emitted service sets a language kwarg (Language enum import, N16)
	Inline                   bool // single agent, no bus: LLM inline in the pipeline (F3)
	NeedsMCP                 bool // any mcp tool source: MCPClient import + lifecycle (N40)
	// MCPParamsImports are the mcp.client.session_group parameter classes the
	// emitted bot actually constructs, so the import lists neither less nor
	// more than the file uses.
	MCPParamsImports []string
	// NeedsMCPChooser means at least one source stated no transport, so the bot
	// picks the parameter class from the URL at startup (research R5).
	NeedsMCPChooser bool
}

// Provider → service facts (class, import, extra/dep, key env, constructor
// shape) live in the provider catalogue (internal/target/catalog_pipecat.go).
// V11's exactly-one-install and import-per-class rules are catalogue
// invariants now (TestCatalogInvariants).

type pipecatInterrupt struct {
	Enabled      bool
	MinWords     int
	IgnorePhrase []string
}

type pipecatInactivity struct {
	NudgeSecs int
	EndSecs   int
}

// pipecatEmittedFields declares every capability field the Pipecat emitter has a
// real code path for. It MUST equal the table's non-gated Pipecat rows — the T12
// agreement test enforces it, so a field can never be validate-green while the
// emitter silently drops it (compiler V19). Add a row here only with the code.
var pipecatEmittedFields = map[targetcap.Field]bool{
	targetcap.FieldListenLocal:          true, // placement forwarded (code target runs it locally)
	targetcap.FieldSpeakLocal:           true,
	targetcap.FieldReasonLocal:          true,
	targetcap.FieldSpeakEndpoint:        true, // base_url on the TTS service
	targetcap.FieldTurnPlacement:        true, // advisory (VAD/smart-turn supplied)
	targetcap.FieldSemanticEndpointing:  true, // advisory
	targetcap.FieldTask:                 true, // Flow node on the owning worker (C8)
	targetcap.FieldTaskNestedResult:     true, // forwarded json_schema properties
	targetcap.FieldTaskGroup:            true, // linear dynamic-flow chain
	targetcap.FieldTaskGroupReturn:      true, // snapshot/restore + results injection
	targetcap.FieldContextIsolated:      true, // per-node ContextStrategy RESET
	targetcap.FieldTransferRequires:     true, // guard before activate_worker
	targetcap.FieldTransferAnnounce:     true, // native source messages before activation
	targetcap.FieldGreetingUserFirst:    true,
	targetcap.FieldGreetingModelWritten: true,
	targetcap.FieldGreetingAbsent:       true,
	targetcap.FieldInterruptionMinWords: true, // MinWordsUserTurnStartStrategy
	targetcap.FieldInterruptionIgnore:   true, // IGNORE_PHRASES
	targetcap.FieldInactivity:           true, // user_idle_timeout
	targetcap.FieldMaxDuration:          true, // asyncio EndFrame timer
	targetcap.FieldToolOutput:           true, // tool returns response.json()
	targetcap.FieldToolLocal:            true, // @tool awaiting tools/<name>.py (T14, V13)
	targetcap.FieldToolMCP:              true, // one MCPClient per source, started at setup (N40)
	targetcap.FieldToolBuiltin:          true, // prebuilt end_call → bodyless end tool
	targetcap.FieldToolAuth:             true, // _bearer Authorization header off token_env
	targetcap.FieldToolInterruption:     true, // cancel_on_interruption
	targetcap.FieldTracingLangfuse:      true,
	targetcap.FieldVariableConversation: true, // generated update_variables @tool writing State
	targetcap.FieldToolInject:           true, // hidden request values merged from State
	targetcap.FieldWebhookPath:          true, // rendered, URL-encoded path on the base URL
	targetcap.FieldTemplates:            true, // _render over prompts and the greeting at session start
}

var pipecatEmittedTelephonyFeatures = map[targetcap.TelephonyFeature]bool{
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

// pipecatDailyCarrierEmittedTelephonyFeatures is the (pipecat, daily-sip,
// twilio) half of the same agreement. Hand-written, so it holds only what the
// emitter can keep: no `source.*` entries, because the fill path for those lives
// in the carrier-websocket adapter this route does not emit (research D11/R14).
var pipecatDailyCarrierEmittedTelephonyFeatures = map[targetcap.TelephonyFeature]bool{
	targetcap.TelephonyRouteSelected:                   true,
	targetcap.TelephonyInbound:                         true,
	targetcap.TelephonyOutbound:                        true,
	targetcap.TelephonyFeature(targetcap.ColdTransfer): true,
	targetcap.TelephonyFeature(targetcap.Hangup):       true,
}

// pipecatCloudWebsocketEmittedTelephonyFeatures is the (pipecat,
// cloud-websocket, twilio) half of the emitter agreement. Hand-written like the
// Daily carrier's, and holding the same five features the row grants: no
// `source.*` entries, because the call-source table is filled by the
// carrier-websocket adapter this route does not emit. The Bin's from_number and
// to_number parameters reach the bot's call_data, which is a different surface
// from a bound spec variable.
var pipecatCloudWebsocketEmittedTelephonyFeatures = map[targetcap.TelephonyFeature]bool{
	targetcap.TelephonyRouteSelected:                   true,
	targetcap.TelephonyInbound:                         true,
	targetcap.TelephonyOutbound:                        true,
	targetcap.TelephonyFeature(targetcap.ColdTransfer): true,
	targetcap.TelephonyFeature(targetcap.Hangup):       true,
}

// GeneratePipecat lowers a validated agent + pipecat target into a project.
// The socket runs Validate(caps) first (V17), so this reads only agent+target.
func GeneratePipecat(agent *ir.Agent, target ir.Target, bindings []ir.ForwardedBinding, sizing []ir.Sizing) (Artifact, error) {
	if err := checkPipecatVersion(target.Version); err != nil {
		return Artifact{}, err
	}
	data, err := buildPipecatData(agent, target)
	if err != nil {
		return Artifact{}, err
	}

	files, err := renderPipecatFiles(data)
	if err != nil {
		return Artifact{}, err
	}
	report, err := pipecatReport(agent, data, files, bindings, sizing)
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

// checkPipecatVersion rejects a target version outside the templates' range (V8).
func checkPipecatVersion(version string) error {
	return targetcap.CheckVersion(targetcap.Pipecat, version)
}

func renderPipecatFiles(data pipecatData) ([]File, error) {
	// tmpl → output path (decoupled so .env.example can't be a dotfile template,
	// which Go's embed would skip).
	outputs := []struct{ tmpl, path string }{
		{"bot.py", "bot.py"},
		{"pyproject.toml", "pyproject.toml"},
		{"Dockerfile", "Dockerfile"},
		{"compose.dev.yaml", "compose.dev.yaml"},
		{"README.md", "README.md"},
		{"env.example", ".env.example"},
	}
	if data.Tracing {
		outputs = append(outputs, struct{ tmpl, path string }{"tracing.py", "tracing.py"})
	}
	if data.Telephony != nil {
		templateName := "telephony_twilio.py"
		switch data.Telephony.Carrier {
		case "telnyx":
			templateName = "telephony_telnyx.py"
		case "plivo":
			templateName = "telephony_plivo.py"
		}
		outputs = append(outputs, struct{ tmpl, path string }{templateName, "telephony.py"})
		outputs = append(outputs, struct{ tmpl, path string }{"telephony_shared.py", "telephony_shared.py"})
		outputs = append(outputs, struct{ tmpl, path string }{"telephony_state.py", "telephony_state.py"})
		outputs = append(outputs, struct{ tmpl, path string }{"compose.telephony.yaml", "compose.telephony.yaml"})
	} else {
		outputs = append(outputs, struct{ tmpl, path string }{"pcc-deploy.toml", "pcc-deploy.toml"})
	}
	// The Daily carrier route deploys to Pipecat Cloud, so it keeps the manifest
	// the branch above just emitted and adds the one artifact the
	// operator runs themselves.
	if data.DailyCarrier != nil {
		outputs = append(outputs, struct{ tmpl, path string }{"telephony_helper.py", "telephony_helper.py"})
	}
	var files []File
	for _, o := range outputs {
		content, err := renderPipecatV1(o.tmpl, data)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: o.path, Content: content})
	}
	// Same set as the LiveKit driver's: secrets never reach the image, and a local
	// `uv run` in this directory leaves a virtualenv behind that would otherwise
	// be uploaded as build context on every deploy.
	files = append(files, File{Path: ".dockerignore", Content: []byte(".env\n.env.*\n.venv/\n__pycache__/\n")})
	// Local tool handlers are copied verbatim from the source package (SCHEMA
	// §5: code targets host the handler; V13).
	if len(data.LocalTools) > 0 {
		files = append(files, File{Path: "tools/__init__.py", Content: []byte("")})
		for _, lt := range data.LocalTools {
			files = append(files, File{Path: "tools/" + lt.Name + ".py", Content: []byte(lt.Source)})
		}
	}
	return files, nil
}

func renderPipecatV1(name string, data pipecatData) ([]byte, error) {
	raw, err := pipecatV1Templates.ReadFile("templates/pipecat_v1/" + name + ".tmpl")
	if err != nil {
		return nil, fmt.Errorf("pipecat template %s: %w", name, err)
	}
	tmpl, err := template.New(name).Funcs(template.FuncMap{"pyq": pyQuote, "pytriple": pyTriple, "join": strings.Join, "mcpTimeout": func() int { return mcpTimeoutSeconds }}).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("pipecat template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("pipecat template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// pyQuote renders a Go string as a Python string literal.
func pyQuote(s string) string { return strconv.Quote(s) }

// promptConstName is the module constant that holds an agent's system prompt
// (referenced by its LLM builder and its Flow restore handler, dedup per V2).
func promptConstName(agent string) string {
	return strings.ToUpper(strings.ReplaceAll(agent, "-", "_")) + "_PROMPT"
}

type pipecatReportJSON struct {
	Target      string                `json:"target"`
	Provider    string                `json:"provider"`
	Version     string                `json:"version"`
	Supported   *reportSupported      `json:"supported,omitempty"`
	EntryAgent  string                `json:"entry_agent"`
	Agents      []string              `json:"agents"`
	Files       []string              `json:"generated_files"`
	Regions     []string              `json:"deployment_regions,omitempty"`
	RequiredEnv []string              `json:"required_env"`
	Bindings    []ir.ForwardedBinding `json:"bindings,omitempty"`
	Sizing      []ir.Sizing           `json:"sizing,omitempty"`
	Variables   []reportVariable      `json:"variables,omitempty"`
	Secrets     []reportSecret        `json:"secrets,omitempty"`
	// Prerequisites are inspectable for the same reason the forwarded region is:
	// a fact the compiler acted on has to be readable back out.
	Prerequisites []targetcap.RouteAccountPrerequisite `json:"route_prerequisites,omitempty"`
	Notes         []string                             `json:"notes,omitempty"`
}

func pipecatReport(agent *ir.Agent, data pipecatData, files []File, bindings []ir.ForwardedBinding, sizing []ir.Sizing) ([]byte, error) {
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
	out, err := json.MarshalIndent(pipecatReportJSON{
		Target: data.Project, Provider: "pipecat", Version: data.Version,
		Supported: supportedRange(targetcap.Pipecat), EntryAgent: data.EntryAgent,
		Agents: agents, Files: generated,
		// Forwarded without checking, so it must be readable back (constitution).
		// A list of one on this target: several regions never reach generate.
		Regions: regionList(data.DeploymentRegion), RequiredEnv: data.RequiredEnv,
		Bindings: bindings, Sizing: sizing,
		Variables: reportVariables(agent), Secrets: reportSecrets(agent),
		Prerequisites: data.Prerequisites,
		Notes:         data.Notes,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
