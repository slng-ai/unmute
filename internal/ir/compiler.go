package ir

import (
	"slices"

	targetcap "github.com/slng-ai/unmute/internal/target"
)

// Agent is the resolved v1 package. References remain names so the graph is
// acyclic and schema derivation does not recurse through agent handoffs.
type Agent struct {
	Version    int    `json:"version" yaml:"version"`
	EntryAgent string `json:"entry_agent" yaml:"entry_agent"`
	// Models flattens the four authoring sections into one name-keyed map; each
	// entry's Kind records its section (names are one namespace, N15).
	Models map[string]ModelDef `json:"models" yaml:"models"`
	// Listen/Turn are the resolved selection names into Models ("" = none).
	Listen    string              `json:"listen,omitempty" yaml:"listen,omitempty"`
	Turn      string              `json:"turn,omitempty" yaml:"turn,omitempty"`
	Variables map[string]Variable `json:"variables,omitempty" yaml:"variables,omitempty"`
	Secrets   []string            `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	// Knowledge is the resolved knowledge: section, one entry per declared base.
	Knowledge map[string]KnowledgeBase `json:"knowledge,omitempty" yaml:"knowledge,omitempty"`
	// Documents holds each knowledge base document, keyed by its artifact path
	// (knowledge/<base>/<file>). Excluded from both derived schemas, the same way
	// Tool.HandlerSource is: it is file content travelling to the artifact
	// writer, not a field an author writes or a debug reader wants to see.
	Documents    map[string][]byte     `json:"-" yaml:"-"`
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

// KnowledgeBase is one resolved knowledge base: a folder of documents, the
// service that embeds them, and the files found in it.
//
// There is no Passages field and no Passage type. Splitting happens at startup
// inside the emitted project, so the compiler holds no opinion about passages;
// precomputing them here would be a second source of truth that can disagree
// with the documents it came from.
type KnowledgeBase struct {
	// Name is the authored name, which is also the artifact subfolder name and the
	// key into the emitted module's settings. Validation has proven it is legal.
	Name string `json:"name" yaml:"name"`
	// Documents is the authored folder path, kept for the compile report.
	Documents string `json:"documents" yaml:"documents"`
	// Embed is the resolved embedding service, never empty after Build.
	Embed string `json:"embed" yaml:"embed"`
	// Files are the document file names found, sorted, so the report and the
	// golden read the same way twice.
	Files []string `json:"files,omitempty" yaml:"files,omitempty"`
	// ChunkSize, ChunkOverlap and TopK are the resolved retrieval settings, never
	// zero after Build applies the defaults. Held per base rather than globally
	// because a price list and a prose policy want different passage sizes, which
	// is the whole reason they are authorable.
	ChunkSize    int `json:"chunk_size" yaml:"chunk_size"`
	ChunkOverlap int `json:"chunk_overlap" yaml:"chunk_overlap"`
	TopK         int `json:"top_k" yaml:"top_k"`
	// Mode is the resolved retrieval mode, never empty after Build.
	Mode KnowledgeMode `json:"mode" yaml:"mode"`
	// MinScore is the authored cutoff, or 0 for none. Zero and absent mean the
	// same thing here on purpose: a cutoff of 0 filters nothing, because every
	// score is above it.
	MinScore float64 `json:"min_score,omitempty" yaml:"min_score,omitempty"`
}

// The retrieval defaults, and the one place they are written.
//
// 90 tokens is roughly 360 characters or 65 words, so three of them is about 200
// tokens of retrieved text per lookup. LlamaIndex's own default is 1024, which
// would put a small essay in the model's context per result.
//
// These are defaults now rather than constants: measurement found that a price
// list splits badly at 90 tokens, ranking an opening-hours passage above the
// price a caller asked for. Recall at 3 still held, but that is luck at three
// results rather than design, so the author can now say what their documents
// look like.
const (
	DefaultChunkSize    = 90
	DefaultChunkOverlap = 20
	DefaultTopK         = 3

	// MaxChunkSize and MaxTopK are sanity bounds, not provider limits. The real
	// cost is what reaches the model: TopK * ChunkSize tokens per lookup, on a
	// voice call where the whole latency budget is a few hundred milliseconds.
	MaxChunkSize = 2048
	MaxTopK      = 20
	// TokenBudgetWarn is where a package earns a warning for how much retrieved
	// text it sends the model per lookup. Not an error: a big budget is a real
	// choice for a dense reference document, it is just rarely what someone meant.
	TokenBudgetWarn = 1500
)

// KnowledgeMode is how a lookup searches.
//
// Three, because measurement found no single winner. Recall across five question
// sets on the example corpus and a 200-page synthetic one:
//
//	                      adversarial  normal  paraphrase  big/verbatim  big/described
//	KnowledgeMeaning        13/15       10/10    8/10         1/8           0/8
//	KnowledgeKeyword        15/15       10/10    6/10         8/8           3/8
//	KnowledgeHybrid         14/15       10/10    8/10         8/8           1/8
//
// Meaning wins paraphrase, which is what it is for. Keyword wins rare tokens and
// scale, and costs no network call at all. Hybrid is best-or-tied on four of the
// five, which is why it is the default.
type KnowledgeMode string

const (
	// KnowledgeMeaning is dense vector search: it matches what a question means,
	// so it finds a passage whose words the caller never said.
	KnowledgeMeaning KnowledgeMode = "meaning"
	// KnowledgeKeyword is BM25 over the passages: it matches the words themselves,
	// ranked. It needs no embedding service, no credential and no network, which
	// makes it the only mode that runs entirely offline.
	KnowledgeKeyword KnowledgeMode = "keyword"
	// KnowledgeHybrid runs both and interleaves them, so each gets slots the other
	// cannot crowd out.
	KnowledgeHybrid KnowledgeMode = "hybrid"

	DefaultKnowledgeMode = KnowledgeHybrid
)

// KnowledgeModes is every legal value, in the order an error message lists them.
var KnowledgeModes = []KnowledgeMode{KnowledgeMeaning, KnowledgeKeyword, KnowledgeHybrid}

// Embeds reports whether the mode needs an embedding service at all. Keyword-only
// retrieval does not, so such a package needs no credential, makes no network call
// and installs no embeddings package.
func (m KnowledgeMode) Embeds() bool { return m != KnowledgeKeyword }

// Scores reports whether results carry a relevance score, which only the
// meaning-based half produces. A min_score on a keyword-only base would filter
// nothing, so validation says so rather than letting it look effective.
func (m KnowledgeMode) Scores() bool { return m.Embeds() }

// Keywords reports whether the mode ranks words directly, which is what needs
// BM25. Only meaning-based retrieval does not, and such a package should neither
// import the retriever nor install it.
func (m KnowledgeMode) Keywords() bool { return m != KnowledgeMeaning }

// MinScoreWarn is where a relevance cutoff starts costing real answers.
//
// Re-measured 2026-08-26 against SimpleVectorStore, on the salon corpus: 18
// answerable questions against 5 off-topic ones. Everything up to and including
// 0.25 keeps all 17 answers the corpus can actually produce; 0.30 loses two, 0.40
// loses four, 0.50 loses seven. On mode: meaning, 0.25 also silences all 5
// off-topic questions, which makes it the one value that costs nothing and buys
// something. Observed similarities ran 0.211 to 0.623.
//
// So the useful band sits at or below this line. A warning rather than a limit,
// because the band is a property of the author's corpus and not of the feature —
// but a value above it is far more often a misunderstanding of the scale than a
// deliberate choice.
const MinScoreWarn = 0.25

// TracingProviders is the allowlist both Build and Validate read, so the two
// cannot drift into disagreeing about which providers exist.
var TracingProviders = []string{"coval", "langfuse"}

func validTracingProvider(provider string) bool {
	return slices.Contains(TracingProviders, provider)
}

// TracingSecrets is the env each provider needs at run time. It is the one place
// the required-secret notes and the drivers read, so a new provider cannot ship
// with its credentials missing from the compile report.
var TracingSecrets = map[string][]string{
	"coval":    {"COVAL_API_KEY"},
	"langfuse": {"LANGFUSE_BASE_URL", "LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY"},
}

// tracingCapability maps a provider onto the capability row that gates it, so a
// target denies the provider the package actually named rather than whichever
// one happened to be gated first.
func tracingCapability(provider string) targetcap.Field {
	if provider == "coval" {
		return targetcap.FieldTracingCoval
	}
	return targetcap.FieldTracingLangfuse
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
	// EndpointingDelay is the turn model's silence window: how long the caller
	// has to stay quiet before the runtime treats them as finished. It is the
	// floor on every turn's wait. Turn models only; LiveKit floors it at 250ms.
	EndpointingDelay Duration `json:"endpointing_delay,omitempty" yaml:"endpointing_delay,omitempty"`
	// AgentID and Upstream are the SLNG Context Router's two authored fields,
	// carried verbatim: the id scopes the router's cache and the block says
	// which upstream serves the model. Neither folds into Params, because params
	// reach the SDK verbatim and these two are consumed by the compiler.
	AgentID     string         `json:"agent_id,omitempty" yaml:"agent_id,omitempty"`
	Upstream    *Upstream      `json:"upstream,omitempty" yaml:"upstream,omitempty"`
	Params      map[string]any `json:"params,omitempty" yaml:"params,omitempty"`
	Fallback    []string       `json:"fallback,omitempty" yaml:"fallback,omitempty"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
}

// Upstream is the resolved model behind a router think binding. The union of
// every provider's fields lives in one struct; which subset is legal is decided
// by Provider through the table in internal/target.
type Upstream struct {
	Provider           string `json:"provider,omitempty" yaml:"provider,omitempty"`
	URL                string `json:"url,omitempty" yaml:"url,omitempty"`
	KeyEnv             string `json:"key_env,omitempty" yaml:"key_env,omitempty"`
	AuthHeader         string `json:"auth_header,omitempty" yaml:"auth_header,omitempty"`
	Deployment         string `json:"deployment,omitempty" yaml:"deployment,omitempty"`
	APIVersion         string `json:"api_version,omitempty" yaml:"api_version,omitempty"`
	CredentialsEnv     string `json:"credentials_env,omitempty" yaml:"credentials_env,omitempty"`
	Location           string `json:"location,omitempty" yaml:"location,omitempty"`
	Project            string `json:"project,omitempty" yaml:"project,omitempty"`
	AccessKeyIDEnv     string `json:"access_key_id_env,omitempty" yaml:"access_key_id_env,omitempty"`
	SecretAccessKeyEnv string `json:"secret_access_key_env,omitempty" yaml:"secret_access_key_env,omitempty"`
	SessionTokenEnv    string `json:"session_token_env,omitempty" yaml:"session_token_env,omitempty"`
	Region             string `json:"region,omitempty" yaml:"region,omitempty"`
	ModelID            string `json:"model_id,omitempty" yaml:"model_id,omitempty"`
}

// Fields keys the authored values by the name an author writes, which is what
// the provider table in internal/target is written against. Empty values are
// dropped, so a caller sees exactly the keys the author set. One method here
// keeps the authored spelling in one place instead of a switch per consumer.
func (u *Upstream) Fields() map[string]string {
	if u == nil {
		return nil
	}
	out := map[string]string{}
	for key, value := range map[string]string{
		"url": u.URL, "key_env": u.KeyEnv, "auth_header": u.AuthHeader,
		"deployment": u.Deployment, "api_version": u.APIVersion,
		"credentials_env": u.CredentialsEnv, "location": u.Location, "project": u.Project,
		"access_key_id_env": u.AccessKeyIDEnv, "secret_access_key_env": u.SecretAccessKeyEnv,
		"session_token_env": u.SessionTokenEnv, "region": u.Region, "model_id": u.ModelID,
	} {
		if value != "" {
			out[key] = value
		}
	}
	return out
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

// Agent.Secrets is the declared environment names, sorted in Build. A secret has
// no fields: the name is the whole declaration, and every declared name is
// required.

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
	Announce string          `json:"announce,omitempty" yaml:"announce,omitempty"`
	Requires []string        `json:"requires,omitempty" yaml:"requires,omitempty"`
	Context  TransferContext `json:"context" yaml:"context"`
}

func (*AgentTransfer) control() {}

// HumanTransfer is the resolved `cold:`/`warm:` control (SCHEMA N25). Mode is
// the resolved shape, not an authored field: the authoring surface is the block
// name. Briefing is free text and is warm-only. OnUnavailable is resolved to its
// default at Build time, so no driver ever reads an empty value.
type HumanTransfer struct {
	Kind          ControlKind   `json:"kind" yaml:"kind"`
	When          string        `json:"when,omitempty" yaml:"when,omitempty"`
	Destination   string        `json:"destination" yaml:"destination"`
	Mode          TransferMode  `json:"mode" yaml:"mode"`
	Briefing      string        `json:"briefing,omitempty" yaml:"briefing,omitempty"`
	RingTimeout   Duration      `json:"ring_timeout,omitempty" yaml:"ring_timeout,omitempty"`
	OnUnavailable OnUnavailable `json:"on_unavailable" yaml:"on_unavailable"`
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

// OnUnavailable is what happens when the person does not take the call. One
// value covers every way that can happen (no answer within RingTimeout, an
// explicit decline, voicemail, or a failed dial), because that is the shape the
// platforms report: LiveKit's WarmTransferTask surfaces all four as one
// ToolError (SCHEMA N25).
type OnUnavailable string

const (
	OnUnavailableReturn OnUnavailable = "return_to_caller"
	OnUnavailableHangup OnUnavailable = "hangup"
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
	// Dependencies are the local handler's exact `name==version` pins, as the
	// author wrote them. Canonicalisation happens where they are emitted.
	Dependencies []string `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	URLEnv       string   `json:"url_env,omitempty" yaml:"url_env,omitempty"`
	// BaseURL is a webhook's literal base URL, for a target that stores the URL
	// rather than reading it from the agent's environment. Empty on every tool a
	// code driver emits, which is why they never read it.
	BaseURL string `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	// Path is the webhook path appended to the base URL; may hold templates.
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
	// Inject holds hidden request values, merged into the call and never shown to
	// the model. String values may hold {{variable}} tokens.
	Inject map[string]any `json:"inject,omitempty" yaml:"inject,omitempty"`
	Auth   *ToolAuth      `json:"auth,omitempty" yaml:"auth,omitempty"` // webhook or mcp auth; nil = unauthenticated
	// MCPTransport is "", "sse", or "streamable_http" (mcp only). Empty leaves
	// the choice to the platform's own rule for the URL (SCHEMA N40).
	MCPTransport string `json:"mcp_transport,omitempty" yaml:"mcp_transport,omitempty"`
	// MCPTools selects server tool names to expose (mcp only); empty = all.
	MCPTools     []string         `json:"mcp_tools,omitempty" yaml:"mcp_tools,omitempty"`
	Interruption ToolInterruption `json:"interruption,omitempty" yaml:"interruption,omitempty"`
	Effect       ToolEffect       `json:"effect,omitempty" yaml:"effect,omitempty"`
	// Announce is one fixed sentence spoken as the tool starts, so a slow call
	// is not silence. Webhook, local and knowledge only; blank means no
	// announcement, so no driver has to interpret whitespace.
	Announce string `json:"announce,omitempty" yaml:"announce,omitempty"`
	// KnowledgeBase names the base this tool searches (knowledge only).
	// Validation has proven the name is declared.
	KnowledgeBase string `json:"knowledge_base,omitempty" yaml:"knowledge_base,omitempty"`
}

type ToolExecution string

const (
	ToolLocal          ToolExecution = "local"
	ToolClient         ToolExecution = "client"
	ToolWebhook        ToolExecution = "webhook"
	ToolProviderHosted ToolExecution = "provider_hosted"
	ToolBuiltin        ToolExecution = "builtin"
	ToolMCP            ToolExecution = "mcp"
	ToolKnowledge      ToolExecution = "knowledge"
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

// The two remote MCP transports (SCHEMA N40). Local stdio servers are out of
// scope, so a transport is always one of these or absent.
const (
	MCPTransportSSE            = "sse"
	MCPTransportStreamableHTTP = "streamable_http"
)

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
	Name              string            `json:"name" yaml:"name"`
	Provider          Provider          `json:"provider" yaml:"provider"`
	Version           string            `json:"version,omitempty" yaml:"version,omitempty"`
	Pins              map[string]string `json:"pins,omitempty" yaml:"pins,omitempty"`
	SDKLanguage       string            `json:"sdk_language,omitempty" yaml:"sdk_language,omitempty"`
	Transport         string            `json:"transport,omitempty" yaml:"transport,omitempty"`
	Carrier           string            `json:"carrier,omitempty" yaml:"carrier,omitempty"`
	Connection        string            `json:"connection,omitempty" yaml:"connection,omitempty"`
	DeploymentRegions []string          `json:"deployment_regions,omitempty" yaml:"deployment_regions,omitempty"`
	Models            Bindings          `json:"models" yaml:"models"`
	Destinations      map[string]string `json:"destinations,omitempty" yaml:"destinations,omitempty"`
	Telephony         *TelephonyPlan    `json:"telephony,omitempty" yaml:"telephony,omitempty"`
}

type Provider string

// These mirror target.Providers. The list is short enough that two hand-written
// copies were cheaper than a dependency edge, and TestProviderEnumMatchesTargets
// in schema_test.go fails when they disagree.
const (
	ProviderLiveKit Provider = "livekit"
	ProviderPipecat Provider = "pipecat"
	ProviderSlng    Provider = "slng"
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
	// EndpointingDelay is the turn model's silence window: how long the caller
	// has to stay quiet before the runtime treats them as finished. It is the
	// floor on every turn's wait. Turn models only; LiveKit floors it at 250ms.
	EndpointingDelay Duration `json:"endpointing_delay,omitempty" yaml:"endpointing_delay,omitempty"`
	// AgentID and Upstream are set only on a SLNG Context Router think binding.
	AgentID  string         `json:"agent_id,omitempty" yaml:"agent_id,omitempty"`
	Upstream *Upstream      `json:"upstream,omitempty" yaml:"upstream,omitempty"`
	Params   map[string]any `json:"params,omitempty" yaml:"params,omitempty"`
}

// Router reports whether this binding selects the SLNG Context Router, which is
// the one question every driver and every validation site asks.
func (b Binding) Router() bool { return b.Provider == ProviderSlngRouter }

// ProviderSlngRouter is the provider spelling that selects the router. It is the
// same word the listen and speak roles use, because one SLNG key serves all
// three roles; what differs is the accepted region set and this role's own
// upstream block.
const ProviderSlngRouter = "slng"
