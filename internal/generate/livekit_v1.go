package generate

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/slng/unmute/internal/ir"
	targetcap "github.com/slng/unmute/internal/target"
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

// beta.workflows (TaskGroup), AgentTask, and inference (LLM/TurnDetector) are all
// present from livekit-agents 1.5.x. Range: >=1.5, <2.0 (verified against the
// reference venv's 1.5.x, driver-livekit C7). The warm-transfer prebuilt is
// beta and its surface moved inside the 1.x line (WorkflowInstructions became
// InstructionParts / extra_instructions), so warm packages pin the minor
// series the import was verified against: 1.6.4, checked in the reference
// checkout on 2026-08-11 (SPEC V10).
const (
	livekitVersionMajor      = 1
	livekitVersionMinMinor   = 5
	livekitWarmVerifiedMinor = 6
)

var livekitVersionPattern = regexp.MustCompile(`^(\d+)(?:\.(\d+))?(?:\.(\d+))?`)

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
	// ToExpr is the Python expression for the destination: a quoted literal,
	// or os.environ["NAME"] when the target defers it to an env var (N26).
	ToExpr string
	Warm   bool
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
	Method    string
	When      string
	Task      *livekitSingleTask // set for a single-task delegate; Steps empty
	Steps     []livekitStep
	Isolated  bool   // context_scope: isolated — standalone-AgentTask sequence, no TaskGroup (C3)
	Then      string // "return" | "transfer" | "end"
	ThenClass string // target Agent class, set only for then: transfer
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
	Instructions     string // builtin end_call closing message → end_instructions
	EndsConversation bool   // effect: ends_conversation — shutdown after the call
}

// livekitMCPServer is one MCP server an agent or task mounts (B3): the tools
// sharing a url_env collapse to one MCPServerHTTP with allowed_tools (D8).
type livekitMCPServer struct {
	URLEnv string
	Tools  []string
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

type livekitData struct {
	Project          string
	Version          string
	DeploymentRegion string
	AgentName        string
	EntryAgent       string
	EntryClass       string
	STT              livekitChain
	SessionLLM       livekitChain
	SessionTTS       livekitService
	TurnVersion      string
	Agents           []livekitAgent
	Tasks            []livekitTask
	Vars             []livekitVar
	CallStartVars    []livekitCallStartVar // dispatched input variables (I.dispatch)
	Capture          *livekitCapture       // generated update_variables tool; nil without conversation variables
	Secrets          []string              // declared secrets, for .env.example (V11)
	ExtraEnv         []string              // env the route needs that the package never declared
	RequiredSecrets  []string              // required secrets: a startup check refuses to run without them (V12)
	LocalTools       []livekitLocalTool    // copied handler files (tools/<name>.py)
	Pins             map[string]string     // plugin pins (C6): raise dep floors
	Prompts          []livekitPrompt
	PluginModules    []string // merged `from livekit.plugins import ...` names
	Deps             []string
	RequiredEnv      []string
	DevEnv           []string // provider creds the web dev image needs (LIVEKIT_* are hardcoded in compose.dev.yaml)
	DevOptionalEnv   []string // passed through when the host sets it, never required (UNMUTE_CALL_START)
	Notes            []string
	InferenceUses    []string // bindings routed through LiveKit Inference (console needs cloud creds, C2/C7)
	Tracing          bool

	NeedsTasks         bool        // AgentTask import
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
	HasVars            bool        // Userdata dataclass + session userdata
	NeedsLastN         bool        // the _last_n history helper
	NeedsSummarize     bool        // the _summarize history helper
	NeedsAsyncio       bool        // inactivity end / max_duration timers
	NeedsInspect       bool        // local tool wrappers (isawaitable)
	NeedsMCP           bool        // mcp import (MCPServerHTTP)
	NeedsEndCallTool   bool        // beta.tools EndCallTool import (prebuilt end_call)
	HasColdTransfer    bool        // get_job_context import
	HasWarmTransfer    bool        // WarmTransferTask import + trunk env + room_options (B14)
	Outbound           *livekitOutbound
	Telephony          *livekitTelephony

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
	targetcap.FieldFallback:              true, // llm.FallbackAdapter (V4)
	targetcap.FieldListenFallback:        true, // stt.FallbackAdapter (T16)
	targetcap.FieldTask:                  true, // AgentTask; single delegate awaits it (T12)
	targetcap.FieldTaskModel:             true, // AgentTask(llm=...) (T14, B1)
	targetcap.FieldTaskNestedResult:      true, // dict finish arg
	targetcap.FieldTaskGroup:             true, // beta.workflows TaskGroup (warn: experimental)
	targetcap.FieldTaskGroupReturn:       true, // N13 snapshot/restore + task_results
	targetcap.FieldContextIsolated:       true, // standalone-AgentTask sequence (T13)
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
	targetcap.FieldToolMCP:               true, // mcp.MCPServerHTTP mounts (B3)
	targetcap.FieldToolAuth:              true, // _bearer Authorization header off token_env
	targetcap.FieldToolInterruption:      true, // warn: runs to completion
	targetcap.FieldOutbound:              true, // SIP dial-out off job metadata
	targetcap.FieldVoicemail:             true, // AMD machine-vm branches (N6)
	targetcap.FieldTracingLangfuse:       true,
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
		Kind:             CodeTarget,
		Files:            files,
		Notes:            GenerateReport{Notes: data.Notes},
		LiveKitInference: data.InferenceUses,
	}, nil
}

// checkLiveKitPins validates plugin pins (C6): a pin key must be a catalogued
// standalone package or the silero VAD plugin, its value must be a semantic
// version at or above the catalogue floor. The pin then raises the dep floor
// in livekitDeps. Unknown keys fail loud — a typo must not silently drop.
func checkLiveKitPins(pins map[string]string) error {
	floors := defaultCatalog.Packages(targetcap.LiveKit)
	floors["livekit-plugins-silero"] = ">=1.6.1" // always emitted (session VAD)
	names := make([]string, 0, len(pins))
	for name := range pins {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		floor, ok := floors[name]
		if !ok {
			known := make([]string, 0, len(floors))
			for k := range floors {
				known = append(known, k)
			}
			sort.Strings(known)
			return fmt.Errorf("livekit pin %q is not a pinnable package; known: %s", name, strings.Join(known, ", "))
		}
		pinned, ok := parseLiveKitVersion(pins[name])
		if !ok {
			return fmt.Errorf("livekit pin %s: %q is not a semantic version", name, pins[name])
		}
		min, ok := parseLiveKitVersion(strings.TrimPrefix(floor, ">="))
		if ok && lessLiveKitVersion(pinned, min) {
			return fmt.Errorf("livekit pin %s %q is below the catalogue floor %s", name, pins[name], floor)
		}
	}
	return nil
}

func parseLiveKitVersion(v string) ([3]int, bool) {
	match := livekitVersionPattern.FindStringSubmatch(v)
	if match == nil {
		return [3]int{}, false
	}
	var out [3]int
	for i, part := range match[1:] {
		out[i], _ = strconv.Atoi(part)
	}
	return out, true
}

func lessLiveKitVersion(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// checkLiveKitVersion rejects a framework version outside the templates' range.
func checkLiveKitVersion(version string) error {
	if version == "" {
		return fmt.Errorf("livekit target requires a framework version")
	}
	match := livekitVersionPattern.FindStringSubmatch(version)
	if match == nil {
		return fmt.Errorf("livekit version %q is not a semantic version", version)
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	if major != livekitVersionMajor || minor < livekitVersionMinMinor {
		return fmt.Errorf("livekit version %q is outside the driver's template-compatible range (>=%d.%d, <%d.0)", version, livekitVersionMajor, livekitVersionMinMinor, livekitVersionMajor+1)
	}
	return nil
}

func renderLiveKitFiles(data livekitData) ([]File, error) {
	outputs := []struct{ tmpl, path string }{
		{"agent.py", "agent.py"},
		{"pyproject.toml", "pyproject.toml"},
		{"README.md", "README.md"},
		{"env.example", ".env.example"},
		{"Dockerfile", "Dockerfile"},
		{"compose.dev.yaml", "compose.dev.yaml"},
		{"livekit.toml", "livekit.toml"},
	}
	if data.Tracing {
		outputs = append(outputs, struct{ tmpl, path string }{"tracing.py", "tracing.py"})
	}
	connector := data.Telephony != nil && data.Telephony.Transport == "connector"
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
		}
	}
	var files []File
	for _, o := range outputs {
		content, err := renderLiveKitV1(o.tmpl, data)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: o.path, Content: content})
	}
	files = append(files, File{Path: ".dockerignore", Content: []byte(".env\n")})
	// SIP trunk JSON inputs are for the SIP route only; the connector uses no
	// SIP trunks.
	if !connector {
		sipFiles, err := livekitSIPFiles(data)
		if err != nil {
			return nil, err
		}
		files = append(files, sipFiles...)
	}
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

// livekitSIPFiles emits provisioner inputs rather than provisioning external
// state. UNVERIFIED: recheck the JSON shapes with the LiveKit docs MCP before
// promoting this route; they were verified against docs.livekit.io on
// 2026-07-20 because the MCP server was unavailable.
func livekitSIPFiles(data livekitData) ([]File, error) {
	telephony := data.Telephony
	if telephony == nil {
		return nil, nil
	}
	placeholder := func(name string) string { return "${" + name + "}" }
	encode := func(path string, value any) (File, error) {
		content, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return File{}, fmt.Errorf("encode %s: %w", path, err)
		}
		return File{Path: path, Content: append(content, '\n')}, nil
	}
	var files []File
	if telephony.HasInbound {
		file, err := encode("sip-inbound-trunk.json", map[string]any{
			"trunk": map[string]any{
				"name":    data.Project + " " + telephony.Carrier + " inbound",
				"numbers": []string{placeholder(telephony.FromNumberEnv)},
			},
		})
		if err != nil {
			return nil, err
		}
		files = append(files, file)
		file, err = encode("sip-dispatch-rule.json", map[string]any{
			"dispatch_rule": map[string]any{
				"name":      data.Project + " inbound",
				"trunk_ids": []string{placeholder("LIVEKIT_SIP_INBOUND_TRUNK")},
				"rule": map[string]any{
					"dispatchRuleIndividual": map[string]any{"roomPrefix": "call-"},
				},
				"roomConfig": map[string]any{
					"agents": []map[string]any{{
						"agentName": data.AgentName,
						"metadata":  `{"direction":"inbound"}`,
					}},
				},
			},
		})
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if telephony.HasOutbound || telephony.HasWarm {
		file, err := encode("sip-outbound-trunk.json", map[string]any{
			"trunk": map[string]any{
				"name":    data.Project + " " + telephony.Carrier + " outbound",
				"address": placeholder(telephony.SIPAddressEnv),
				"numbers": []string{placeholder(telephony.FromNumberEnv)},
			},
		})
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func renderLiveKitV1(name string, data livekitData) ([]byte, error) {
	raw, err := livekitV1Templates.ReadFile("templates/livekit_v1/" + name + ".tmpl")
	if err != nil {
		return nil, fmt.Errorf("livekit template %s: %w", name, err)
	}
	tmpl, err := template.New(name).Funcs(template.FuncMap{
		"pyq":    pyQuote,
		"join":   strings.Join,
		"triple": pyTriple,
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
	EntryAgent  string                `json:"entry_agent"`
	Agents      []string              `json:"agents"`
	Tasks       []string              `json:"tasks,omitempty"`
	Files       []string              `json:"generated_files"`
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
		Target: data.Project, Provider: "livekit", Version: data.Version, EntryAgent: data.EntryClass,
		Agents: agents, Tasks: tasks, Files: generated, RequiredEnv: data.RequiredEnv,
		Bindings: bindings, Sizing: sizing,
		Variables: reportVariables(agent), Secrets: reportSecrets(agent),
		Notes: data.Notes,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
