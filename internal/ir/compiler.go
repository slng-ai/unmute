package ir

// Agent is the resolved v1 package. References remain names so the graph is
// acyclic and schema derivation does not recurse through agent handoffs.
type Agent struct {
	Version    int    `json:"version" yaml:"version"`
	EntryAgent string `json:"entry_agent" yaml:"entry_agent"`
	// Models flattens the four authoring sections into one name-keyed map; each
	// entry's Kind records its section (names are one namespace, N15).
	Models map[string]ModelDef `json:"models" yaml:"models"`
	// Listen/Turn are the resolved selection names into Models ("" = none).
	Listen       string                `json:"listen,omitempty" yaml:"listen,omitempty"`
	Turn         string                `json:"turn,omitempty" yaml:"turn,omitempty"`
	Variables    map[string]Variable   `json:"variables,omitempty" yaml:"variables,omitempty"`
	Secrets      map[string]Secret     `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Agents       map[string]AgentDef   `json:"agents" yaml:"agents"`
	Tasks        map[string]Task       `json:"tasks,omitempty" yaml:"tasks,omitempty"`
	TaskGroups   map[string]TaskGroup  `json:"task_groups,omitempty" yaml:"task_groups,omitempty"`
	Controls     map[string]Control    `json:"controls,omitempty" yaml:"controls,omitempty"`
	Tools        map[string]Tool       `json:"tools,omitempty" yaml:"tools,omitempty"`
	Conversation *Conversation         `json:"conversation,omitempty" yaml:"conversation,omitempty"`
	Tracing      *Tracing              `json:"tracing,omitempty" yaml:"tracing,omitempty"`
	Channels     map[string]Channel    `json:"channels" yaml:"channels"`
	Capacity     *Capacity             `json:"capacity,omitempty" yaml:"capacity,omitempty"`
	Connections  map[string]Connection `json:"connections,omitempty" yaml:"connections,omitempty"`
	Targets      map[string]Target     `json:"targets" yaml:"targets"`
}

type Tracing struct {
	Provider string `json:"provider" yaml:"provider"`
}

// ModelKind is resolved from a model's reference site in Build (N15).
type ModelKind string

const (
	KindThink  ModelKind = "think"
	KindSpeak  ModelKind = "speak"
	KindListen ModelKind = "listen"
	KindTurn   ModelKind = "turn"
)

// ModelDef is the resolved unified model definition (N15). provider+model carry
// the vendor pairing; Placement is derived from provider unless set explicitly;
// Kind is fixed in Build from where the model is referenced.
type ModelDef struct {
	Kind                ModelKind           `json:"kind" yaml:"kind"`
	Provider            string              `json:"provider,omitempty" yaml:"provider,omitempty"`
	Model               string              `json:"model,omitempty" yaml:"model,omitempty"`
	Voice               string              `json:"voice,omitempty" yaml:"voice,omitempty"`
	Speed               *float64            `json:"speed,omitempty" yaml:"speed,omitempty"`
	Language            string              `json:"language,omitempty" yaml:"language,omitempty"`
	Temperature         *float64            `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	TopP                *float64            `json:"top_p,omitempty" yaml:"top_p,omitempty"`
	TopK                *int                `json:"top_k,omitempty" yaml:"top_k,omitempty"`
	EndpointEnv         string              `json:"endpoint_env,omitempty" yaml:"endpoint_env,omitempty"`
	Placement           Placement           `json:"placement" yaml:"placement"`
	SemanticEndpointing SemanticEndpointing `json:"semantic_endpointing,omitempty" yaml:"semantic_endpointing,omitempty"`
	Params              map[string]any      `json:"params,omitempty" yaml:"params,omitempty"`
	Fallback            []string            `json:"fallback,omitempty" yaml:"fallback,omitempty"`
	Description         string              `json:"description,omitempty" yaml:"description,omitempty"`
}

type Placement string

const (
	PlacementAPI   Placement = "api"
	PlacementLocal Placement = "local"
)

type SemanticEndpointing string

const (
	SemanticEndpointingRequired  SemanticEndpointing = "required"
	SemanticEndpointingPreferred SemanticEndpointing = "preferred"
	SemanticEndpointingOff       SemanticEndpointing = "off"
)

type Variable struct {
	Type        PrimitiveType  `json:"type" yaml:"type"`
	Default     any            `json:"default,omitempty" yaml:"default,omitempty"`
	Source      VariableSource `json:"source,omitempty" yaml:"source,omitempty"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
}

// Secret is a declared runtime environment value. Its name is the map key in
// Agent.Secrets; Required is settled in Build (default true), so every consumer
// reads a decided value.
type Secret struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool   `json:"required" yaml:"required"`
}

type PrimitiveType string

const (
	PrimitiveString  PrimitiveType = "string"
	PrimitiveNumber  PrimitiveType = "number"
	PrimitiveBoolean PrimitiveType = "boolean"
	PrimitiveInteger PrimitiveType = "integer"
)

type VariableSource string

const (
	VariableSourceCallStart  VariableSource = "call_start"
	VariableSourceSessionID  VariableSource = "session_id"
	VariableSourceCarrier    VariableSource = "carrier"
	VariableSourceConnection VariableSource = "connection"
	VariableSourceCallID     VariableSource = "call_id"
	VariableSourceStreamID   VariableSource = "stream_id"
	VariableSourceDirection  VariableSource = "direction"
	VariableSourceFromNumber VariableSource = "from_number"
	VariableSourceToNumber   VariableSource = "to_number"
	// VariableSourceConversation marks a value the model saves mid-call through
	// the generated update_variables tool (variable_secrets_specs.md N23).
	VariableSourceConversation VariableSource = "conversation"
)

type AgentDef struct {
	Instructions string   `json:"instructions" yaml:"instructions"`
	Model        string   `json:"model" yaml:"model"`
	Voice        string   `json:"voice" yaml:"voice"`
	Tools        []string `json:"tools,omitempty" yaml:"tools,omitempty"`
}

type Task struct {
	Instructions string                 `json:"instructions" yaml:"instructions"`
	Tools        []string               `json:"tools,omitempty" yaml:"tools,omitempty"`
	Model        string                 `json:"model,omitempty" yaml:"model,omitempty"`
	Result       map[string]ResultField `json:"result" yaml:"result"`
	Context      TaskContext            `json:"context" yaml:"context"`
}

type ResultField struct {
	Type   PrimitiveType  `json:"type,omitempty" yaml:"type,omitempty"`
	Enum   []string       `json:"enum,omitempty" yaml:"enum,omitempty"`
	Schema map[string]any `json:"schema,omitempty" yaml:"schema,omitempty"`
}

type TaskGroup struct {
	Steps        []string     `json:"steps" yaml:"steps"`
	ContextScope ContextScope `json:"context_scope" yaml:"context_scope"`
	Then         GroupThen    `json:"then" yaml:"then"`
	ThenTarget   string       `json:"then_target,omitempty" yaml:"then_target,omitempty"`
	Merge        GroupMerge   `json:"merge,omitempty" yaml:"merge,omitempty"`
}

type ContextScope string

const (
	ContextShared   ContextScope = "shared"
	ContextIsolated ContextScope = "isolated"
)

type GroupThen string

const (
	GroupReturn   GroupThen = "return"
	GroupTransfer GroupThen = "transfer"
	GroupEnd      GroupThen = "end"
)

type GroupMerge string

const GroupMergeResults GroupMerge = "results"

type TaskContext struct {
	History          History `json:"history" yaml:"history"`
	MaxMessages      int     `json:"max_messages,omitempty" yaml:"max_messages,omitempty"`
	Summarizer       string  `json:"summarizer,omitempty" yaml:"summarizer,omitempty"`
	IncludeToolCalls *bool   `json:"include_tool_calls,omitempty" yaml:"include_tool_calls,omitempty"`
}

type TransferContext struct {
	TaskContext
	Variables VariableSelection `json:"variables" yaml:"variables"`
}

type VariableSelection struct {
	All   bool     `json:"all,omitempty" yaml:"all,omitempty"`
	Names []string `json:"names,omitempty" yaml:"names,omitempty"`
}

type History string

const (
	HistoryFull     History = "full"
	HistoryMessages History = "messages"
	HistoryLastN    History = "last_n"
	HistorySummary  History = "summary"
	HistoryReset    History = "reset"
)

type Control interface {
	control()
}

type Delegate struct {
	Kind   ControlKind       `json:"kind" yaml:"kind"`
	When   string            `json:"when,omitempty" yaml:"when,omitempty"`
	Task   string            `json:"task,omitempty" yaml:"task,omitempty"`
	Group  string            `json:"group,omitempty" yaml:"group,omitempty"`
	Assign map[string]string `json:"assign,omitempty" yaml:"assign,omitempty"`
}

func (*Delegate) control() {}

type AgentTransfer struct {
	Kind     ControlKind     `json:"kind" yaml:"kind"`
	When     string          `json:"when,omitempty" yaml:"when,omitempty"`
	To       string          `json:"to" yaml:"to"`
	Requires []string        `json:"requires,omitempty" yaml:"requires,omitempty"`
	Context  TransferContext `json:"context" yaml:"context"`
}

func (*AgentTransfer) control() {}

type HumanTransfer struct {
	Kind        ControlKind  `json:"kind" yaml:"kind"`
	When        string       `json:"when,omitempty" yaml:"when,omitempty"`
	Destination string       `json:"destination" yaml:"destination"`
	Mode        TransferMode `json:"mode" yaml:"mode"`
	Briefing    Briefing     `json:"briefing,omitempty" yaml:"briefing,omitempty"`
}

func (*HumanTransfer) control() {}

type ControlKind string

const (
	ControlDelegate      ControlKind = "delegate"
	ControlAgentTransfer ControlKind = "agent_transfer"
	ControlHumanTransfer ControlKind = "human_transfer"
)

type TransferMode string

const (
	TransferCold TransferMode = "cold"
	TransferWarm TransferMode = "warm"
)

type Briefing string

const (
	BriefingSummary Briefing = "summary"
	BriefingMessage Briefing = "message"
	BriefingWait    Briefing = "wait"
)

type Tool struct {
	Description   string         `json:"description,omitempty" yaml:"description,omitempty"`
	Input         map[string]any `json:"input,omitempty" yaml:"input,omitempty"`
	Output        map[string]any `json:"output,omitempty" yaml:"output,omitempty"`
	Execution     ToolExecution  `json:"execution" yaml:"execution"`
	Builtin       string         `json:"builtin,omitempty" yaml:"builtin,omitempty"` // prebuilt registry id (builtin only)
	Instructions  string         `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	Handler       string         `json:"handler,omitempty" yaml:"handler,omitempty"`
	HandlerSource string         `json:"-" yaml:"-"` // local handler file content, loaded by spec.Load
	URLEnv        string         `json:"url_env,omitempty" yaml:"url_env,omitempty"`
	// Path is the webhook path appended to URLEnv's base URL; may hold templates.
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
	// Inject holds hidden request values, merged into the call and never shown to
	// the model. String values may hold {{variable}} tokens.
	Inject       map[string]any   `json:"inject,omitempty" yaml:"inject,omitempty"`
	Auth         *ToolAuth        `json:"auth,omitempty" yaml:"auth,omitempty"` // webhook auth; nil = unauthenticated
	Interruption ToolInterruption `json:"interruption,omitempty" yaml:"interruption,omitempty"`
	Effect       ToolEffect       `json:"effect,omitempty" yaml:"effect,omitempty"`
}

type ToolExecution string

const (
	ToolLocal          ToolExecution = "local"
	ToolClient         ToolExecution = "client"
	ToolWebhook        ToolExecution = "webhook"
	ToolProviderHosted ToolExecution = "provider_hosted"
	ToolBuiltin        ToolExecution = "builtin"
	ToolMCP            ToolExecution = "mcp"
)

// ToolAuth is a resolved webhook authentication scheme (SCHEMA §5). Defaults
// are applied in Build, so every generator reads settled values; the token only
// ever appears here as an environment variable name.
type ToolAuth struct {
	Type     ToolAuthType `json:"type" yaml:"type"`
	TokenEnv string       `json:"token_env,omitempty" yaml:"token_env,omitempty"`
	Header   string       `json:"header,omitempty" yaml:"header,omitempty"`
}

type ToolAuthType string

const (
	ToolAuthBearer ToolAuthType = "bearer"
	ToolAuthAPIKey ToolAuthType = "api_key"
)

// DefaultAPIKeyHeader is the api_key header name applied in Build when the
// block omits one.
const DefaultAPIKeyHeader = "X-API-Key"

type ToolInterruption string

const (
	ToolContinue        ToolInterruption = "continue"
	ToolCancel          ToolInterruption = "cancel"
	ToolProviderDefault ToolInterruption = "provider_default"
)

type ToolEffect string

const (
	ToolReturnsData      ToolEffect = "returns_data"
	ToolEndsConversation ToolEffect = "ends_conversation"
)

type Conversation struct {
	Greeting      *Greeting     `json:"greeting,omitempty" yaml:"greeting,omitempty"`
	Interruption  *Interruption `json:"interruption,omitempty" yaml:"interruption,omitempty"`
	Inactivity    *Inactivity   `json:"inactivity,omitempty" yaml:"inactivity,omitempty"`
	MaxDuration   Duration      `json:"max_duration,omitempty" yaml:"max_duration,omitempty"`
	ThinkingAudio ThinkingAudio `json:"thinking_audio,omitempty" yaml:"thinking_audio,omitempty"`
}

type Greeting struct {
	SpeaksFirst SpeaksFirst `json:"speaks_first" yaml:"speaks_first"`
	Text        string      `json:"text,omitempty" yaml:"text,omitempty"`
}

type SpeaksFirst string

const (
	SpeaksFirstAgent SpeaksFirst = "agent"
	SpeaksFirstUser  SpeaksFirst = "user"
)

type Interruption struct {
	Enabled       *bool    `json:"enabled" yaml:"enabled"`
	MinimumWords  int      `json:"minimum_words,omitempty" yaml:"minimum_words,omitempty"`
	IgnorePhrases []string `json:"ignore_phrases,omitempty" yaml:"ignore_phrases,omitempty"`
}

type Inactivity struct {
	NudgeAfter Duration `json:"nudge_after,omitempty" yaml:"nudge_after,omitempty"`
	EndAfter   Duration `json:"end_after,omitempty" yaml:"end_after,omitempty"`
}

type Duration string

type ThinkingAudio string

const (
	ThinkingNone   ThinkingAudio = "none"
	ThinkingSubtle ThinkingAudio = "subtle"
)

type Channel struct {
	Kind             ChannelKind     `json:"kind" yaml:"kind"`
	Inbound          *bool           `json:"inbound,omitempty" yaml:"inbound,omitempty"`
	Outbound         *bool           `json:"outbound,omitempty" yaml:"outbound,omitempty"`
	RequiredControls []string        `json:"required_controls,omitempty" yaml:"required_controls,omitempty"`
	OnVoicemail      VoicemailAction `json:"on_voicemail,omitempty" yaml:"on_voicemail,omitempty"`
}

type ChannelKind string

const (
	ChannelRealtimeAudio ChannelKind = "realtime_audio"
	ChannelTelephony     ChannelKind = "telephony"
)

type VoicemailAction string

const (
	VoicemailHangup       VoicemailAction = "hangup"
	VoicemailLeaveMessage VoicemailAction = "leave_message"
)

type Capacity struct {
	PeakSessions        int      `json:"peak_sessions" yaml:"peak_sessions"`
	MaxSessions         int      `json:"max_sessions" yaml:"max_sessions"`
	PeakStartsPerSecond float64  `json:"peak_starts_per_second,omitempty" yaml:"peak_starts_per_second,omitempty"`
	AvgSessionDuration  Duration `json:"avg_session_duration" yaml:"avg_session_duration"`
}

type Connection struct {
	Kind        string            `json:"kind" yaml:"kind"`
	Environment map[string]string `json:"environment" yaml:"environment"`
}

type TelephonyPlan struct {
	Channels            []string                      `json:"channels" yaml:"channels"`
	Connection          string                        `json:"connection" yaml:"connection"`
	Key                 TelephonyKey                  `json:"key" yaml:"key"`
	Environment         map[string]string             `json:"environment" yaml:"environment"`
	Destinations        map[string]string             `json:"destinations,omitempty" yaml:"destinations,omitempty"`
	SystemSources       map[string]VariableSource     `json:"system_sources,omitempty" yaml:"system_sources,omitempty"`
	Evidence            []TelephonyFeatureEvidence    `json:"evidence" yaml:"evidence"`
	Processes           []TelephonyProcess            `json:"processes" yaml:"processes"`
	PublicEndpoints     []TelephonyEndpoint           `json:"public_endpoints,omitempty" yaml:"public_endpoints,omitempty"`
	RequiredEnvironment []string                      `json:"required_environment" yaml:"required_environment"`
	LocalEnvironment    []string                      `json:"locally_supplied_environment" yaml:"locally_supplied_environment"`
	DevEnvironment      []string                      `json:"dev_supplied_environment,omitempty" yaml:"dev_supplied_environment,omitempty"`
	AutoWebhookEndpoint string                        `json:"auto_webhook_endpoint,omitempty" yaml:"auto_webhook_endpoint,omitempty"`
	ManualSteps         []string                      `json:"manual_steps,omitempty" yaml:"manual_steps,omitempty"`
	Services            []string                      `json:"services" yaml:"services"`
	Coordination        string                        `json:"coordination" yaml:"coordination"`
	CoordinationReasons []TelephonyCoordinationReason `json:"coordination_reasons" yaml:"coordination_reasons"`
	AdmissionOwner      string                        `json:"admission_owner" yaml:"admission_owner"`
}

type TelephonyProcess struct {
	Name      string   `json:"name" yaml:"name"`
	Command   []string `json:"command" yaml:"command"`
	Health    string   `json:"health,omitempty" yaml:"health,omitempty"`
	Readiness string   `json:"readiness,omitempty" yaml:"readiness,omitempty"`
}

type TelephonyEndpoint struct {
	Name   string `json:"name" yaml:"name"`
	Method string `json:"method" yaml:"method"`
	Path   string `json:"path" yaml:"path"`
}

type TelephonyCoordinationReason struct {
	Name      string   `json:"name" yaml:"name"`
	Consumers []string `json:"consumers" yaml:"consumers"`
}

type TelephonyKey struct {
	Provider  Provider `json:"provider" yaml:"provider"`
	Transport string   `json:"transport" yaml:"transport"`
	Carrier   string   `json:"carrier" yaml:"carrier"`
}

type TelephonyFeatureEvidence struct {
	Feature  string `json:"feature" yaml:"feature"`
	Tag      string `json:"tag" yaml:"tag"`
	Note     string `json:"note,omitempty" yaml:"note,omitempty"`
	Docs     string `json:"docs,omitempty" yaml:"docs,omitempty"`
	Verified string `json:"verified,omitempty" yaml:"verified,omitempty"`
	Smoke    bool   `json:"smoke" yaml:"smoke"`
}

type Target struct {
	Name             string            `json:"name" yaml:"name"`
	Provider         Provider          `json:"provider" yaml:"provider"`
	Version          string            `json:"version,omitempty" yaml:"version,omitempty"`
	Pins             map[string]string `json:"pins,omitempty" yaml:"pins,omitempty"`
	SDKLanguage      string            `json:"sdk_language,omitempty" yaml:"sdk_language,omitempty"`
	Transport        string            `json:"transport,omitempty" yaml:"transport,omitempty"`
	Carrier          string            `json:"carrier,omitempty" yaml:"carrier,omitempty"`
	Connection       string            `json:"connection,omitempty" yaml:"connection,omitempty"`
	DeploymentRegion string            `json:"deployment_region,omitempty" yaml:"deployment_region,omitempty"`
	Models           Bindings          `json:"models" yaml:"models"`
	Destinations     map[string]string `json:"destinations,omitempty" yaml:"destinations,omitempty"`
	Telephony        *TelephonyPlan    `json:"telephony,omitempty" yaml:"telephony,omitempty"`
}

type Provider string

const (
	ProviderLiveKit  Provider = "livekit"
	ProviderPipecat  Provider = "pipecat"
	ProviderVapi     Provider = "vapi"
	ProviderDeepgram Provider = "deepgram"
)

type Bindings struct {
	Listen *Binding `json:"listen,omitempty" yaml:"listen,omitempty"`
	// ListenFallbacks is the selected listen model's flattened fallback chain,
	// in order, resolved per target (N15/T16). Empty without listen fallback.
	ListenFallbacks []ListenFallback   `json:"listen_fallbacks,omitempty" yaml:"listen_fallbacks,omitempty"`
	Turn            *Binding           `json:"turn,omitempty" yaml:"turn,omitempty"`
	Speak           map[string]Binding `json:"speak,omitempty" yaml:"speak,omitempty"`
	Reason          map[string]Binding `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// ListenFallback pairs a chain entry's model name with its resolved binding.
type ListenFallback struct {
	Name    string  `json:"name" yaml:"name"`
	Binding Binding `json:"binding" yaml:"binding"`
}

// Binding is the resolved per-target view of one effective model (N15): the
// generators consume this, not the authoring ModelDef.
type Binding struct {
	Provider            string              `json:"provider,omitempty" yaml:"provider,omitempty"`
	Model               string              `json:"model,omitempty" yaml:"model,omitempty"`
	Voice               string              `json:"voice,omitempty" yaml:"voice,omitempty"`
	VoiceID             string              `json:"voice_id,omitempty" yaml:"voice_id,omitempty"`
	Language            string              `json:"language,omitempty" yaml:"language,omitempty"`
	EndpointEnv         string              `json:"endpoint_env,omitempty" yaml:"endpoint_env,omitempty"`
	Placement           Placement           `json:"placement,omitempty" yaml:"placement,omitempty"`
	SemanticEndpointing SemanticEndpointing `json:"semantic_endpointing,omitempty" yaml:"semantic_endpointing,omitempty"`
	Params              map[string]any      `json:"params,omitempty" yaml:"params,omitempty"`
}
