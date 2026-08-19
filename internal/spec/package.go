package spec

import (
	"fmt"
	"strings"
)

// Package is the decoded, unresolved v1 package assembled from its files.
type Package struct {
	Agent       AgentFile             `json:"agent" yaml:"agent"`
	Tools       map[string]Tool       `json:"tools,omitempty" yaml:"tools,omitempty"`
	Connections map[string]Connection `json:"connections,omitempty" yaml:"connections,omitempty"`
	Targets     map[string]Target     `json:"targets" yaml:"targets"`

	Root     string            `json:"-" yaml:"-"`
	Markdown map[string]string `json:"-" yaml:"-"`
	Handlers map[string]string `json:"-" yaml:"-"` // local tool handler sources, by path
	files    map[string][]byte
}

// Location returns the first source line containing token in a package file.
func (p *Package) Location(file, token string) string {
	for i, line := range strings.Split(string(p.files[file]), "\n") {
		if strings.Contains(line, token) {
			return fmt.Sprintf("%s:%d", file, i+1)
		}
	}
	return file
}

type AgentFile struct {
	Version    int           `json:"version" yaml:"version"`
	EntryAgent string        `json:"entry_agent" yaml:"entry_agent"`
	Models     ModelSections `json:"models" yaml:"models"`
	// Listen/Turn select one entry of the matching models section by name.
	// Optional when the section has at most one entry (the sole entry selects
	// itself); required with 2+ entries (N15 palette).
	Listen    string              `json:"listen,omitempty" yaml:"listen,omitempty"`
	Turn      string              `json:"turn,omitempty" yaml:"turn,omitempty"`
	Variables map[string]Variable `json:"variables,omitempty" yaml:"variables,omitempty"`
	Secrets   []string            `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	// Destinations maps a symbolic name a control escalates to onto the name of
	// an environment variable holding the number. A destination is who this agent
	// escalates to, which is the same desk whichever carrier reaches it, so it
	// lives here rather than on a target.
	Destinations map[string]string    `json:"destinations,omitempty" yaml:"destinations,omitempty"`
	Agents       map[string]AgentDef  `json:"agents" yaml:"agents"`
	Tasks        map[string]Task      `json:"tasks,omitempty" yaml:"tasks,omitempty"`
	TaskGroups   map[string]TaskGroup `json:"task_groups,omitempty" yaml:"task_groups,omitempty"`
	Controls     map[string]Control   `json:"controls,omitempty" yaml:"controls,omitempty"`
	Tools        []string             `json:"tools,omitempty" yaml:"tools,omitempty"`
	Conversation *Conversation        `json:"conversation,omitempty" yaml:"conversation,omitempty"`
	Tracing      *Tracing             `json:"tracing,omitempty" yaml:"tracing,omitempty"`
	Channels     map[string]Channel   `json:"channels" yaml:"channels"`
	Capacity     *Capacity            `json:"capacity,omitempty" yaml:"capacity,omitempty"`
}

type Tracing struct {
	Provider string `json:"provider" yaml:"provider"`
}

// ModelSections is the central models map, grouped by kind (N15): the section
// an entry sits in IS its kind. Names share one namespace across sections.
// Entries are a palette: unreferenced ones are legal swappable alternates.
type ModelSections struct {
	Think  map[string]ModelDef `json:"think,omitempty" yaml:"think,omitempty"`
	Speak  map[string]ModelDef `json:"speak,omitempty" yaml:"speak,omitempty"`
	Listen map[string]ModelDef `json:"listen,omitempty" yaml:"listen,omitempty"`
	Turn   map[string]ModelDef `json:"turn,omitempty" yaml:"turn,omitempty"`
}

// ModelDef is the unified model definition (N15): one shape for every models
// section entry and for per-target overrides. Which fields are legal is decided
// by the section (kind). provider+model carry the same pairing the old target
// bindings used; the typed generation fields are folded into the forwarded
// params at Build time.
type ModelDef struct {
	Provider            string   `json:"provider,omitempty" yaml:"provider,omitempty"`
	Model               string   `json:"model,omitempty" yaml:"model,omitempty"`
	Voice               string   `json:"voice,omitempty" yaml:"voice,omitempty"`
	Speed               *float64 `json:"speed,omitempty" yaml:"speed,omitempty"`
	Language            string   `json:"language,omitempty" yaml:"language,omitempty"`
	Temperature         *float64 `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	TopP                *float64 `json:"top_p,omitempty" yaml:"top_p,omitempty"`
	TopK                *int     `json:"top_k,omitempty" yaml:"top_k,omitempty"`
	EndpointEnv         string   `json:"endpoint_env,omitempty" yaml:"endpoint_env,omitempty"`
	Placement           string   `json:"placement,omitempty" yaml:"placement,omitempty"`
	SemanticEndpointing string   `json:"semantic_endpointing,omitempty" yaml:"semantic_endpointing,omitempty"`
	// AgentID scopes the SLNG Context Router's cache. One stable value per
	// package, authored by a human, carrying a version suffix they own and bump
	// after a prompt change they judge meaningful. Never composed, never
	// derived, never hashed: a split id splits the cache.
	AgentID string `json:"agent_id,omitempty" yaml:"agent_id,omitempty"`
	// Upstream says where the router actually calls the model and whose
	// credentials pay for it. Required on a router think binding, because the
	// configuration travels inline on every request.
	Upstream    *Upstream      `json:"upstream,omitempty" yaml:"upstream,omitempty"`
	Params      map[string]any `json:"params,omitempty" yaml:"params,omitempty"`
	Fallback    []string       `json:"fallback,omitempty" yaml:"fallback,omitempty"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
}

// Upstream is the model behind a SLNG Context Router think binding: the
// provider kind, its endpoint, and the environment variable names holding its
// credentials.
//
// One struct holds the union of every provider's fields; which subset is legal
// is decided by provider, and that per-provider truth lives in the table beside
// the other provider facts (internal/target/slng_router.go). A new provider is
// then a table row and a test, not a new type and a new branch.
//
// Every credential is named, never written: a *_env field holds an environment
// variable name, the name has to appear in secrets:, and no field mixes a
// literal with an environment value.
type Upstream struct {
	// Provider is one of openai, openai-compat, azure, vertex, bedrock.
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`
	// URL is the upstream endpoint. Defaults on openai. On azure it is the
	// resource root rather than the deployment URL.
	URL string `json:"url,omitempty" yaml:"url,omitempty"`
	// KeyEnv names the variable holding the provider key.
	KeyEnv string `json:"key_env,omitempty" yaml:"key_env,omitempty"`
	// AuthHeader is a header *name*, for an openai-compat host that wants the
	// key somewhere other than Authorization: Bearer. The value still comes
	// from KeyEnv.
	AuthHeader string `json:"auth_header,omitempty" yaml:"auth_header,omitempty"`
	// Deployment and APIVersion are azure's.
	Deployment string `json:"deployment,omitempty" yaml:"deployment,omitempty"`
	APIVersion string `json:"api_version,omitempty" yaml:"api_version,omitempty"`
	// CredentialsEnv names the variable holding a GCP service-account key, as
	// JSON, as base64 of that JSON, or as a path to the key file. The generated
	// agent works out which at startup.
	CredentialsEnv string `json:"credentials_env,omitempty" yaml:"credentials_env,omitempty"`
	// Location and Project are vertex's; Project defaults to the key's own.
	Location string `json:"location,omitempty" yaml:"location,omitempty"`
	Project  string `json:"project,omitempty" yaml:"project,omitempty"`
	// The bedrock set. SessionTokenEnv is for temporary credentials, and
	// ModelID is the Bedrock model id, which differs from the entry label.
	AccessKeyIDEnv     string `json:"access_key_id_env,omitempty" yaml:"access_key_id_env,omitempty"`
	SecretAccessKeyEnv string `json:"secret_access_key_env,omitempty" yaml:"secret_access_key_env,omitempty"`
	SessionTokenEnv    string `json:"session_token_env,omitempty" yaml:"session_token_env,omitempty"`
	Region             string `json:"region,omitempty" yaml:"region,omitempty"`
	ModelID            string `json:"model_id,omitempty" yaml:"model_id,omitempty"`
}

type Variable struct {
	Type        string `json:"type" yaml:"type"`
	Default     any    `json:"default,omitempty" yaml:"default,omitempty"`
	Source      string `json:"source,omitempty" yaml:"source,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Secret declares one runtime environment value the package needs. The map key
// IS the environment variable name (UPPER_SNAKE), so there is no field a value
// could ever be written into (variable_secrets_specs.md C3, V9).
type AgentDef struct {
	Instructions string   `json:"instructions" yaml:"instructions"`
	Model        string   `json:"model" yaml:"model"`
	Voice        string   `json:"voice" yaml:"voice"`
	Tools        []string `json:"tools,omitempty" yaml:"tools,omitempty"`
}

type Task struct {
	Instructions string         `json:"instructions" yaml:"instructions"`
	Tools        []string       `json:"tools,omitempty" yaml:"tools,omitempty"`
	Model        string         `json:"model,omitempty" yaml:"model,omitempty"`
	Result       map[string]any `json:"result" yaml:"result"`
	Context      TaskContext    `json:"context" yaml:"context"`
}

type TaskGroup struct {
	Steps        []string `json:"steps" yaml:"steps"`
	ContextScope string   `json:"context_scope" yaml:"context_scope"`
	Then         string   `json:"then" yaml:"then"`
	ThenTarget   string   `json:"then_target,omitempty" yaml:"then_target,omitempty"`
	Merge        string   `json:"merge,omitempty" yaml:"merge,omitempty"`
}

type TaskContext struct {
	History          string `json:"history" yaml:"history"`
	MaxMessages      int    `json:"max_messages,omitempty" yaml:"max_messages,omitempty"`
	Summarizer       string `json:"summarizer,omitempty" yaml:"summarizer,omitempty"`
	IncludeToolCalls *bool  `json:"include_tool_calls,omitempty" yaml:"include_tool_calls,omitempty"`
}

type TransferContext struct {
	History          string `json:"history" yaml:"history"`
	MaxMessages      int    `json:"max_messages,omitempty" yaml:"max_messages,omitempty"`
	Summarizer       string `json:"summarizer,omitempty" yaml:"summarizer,omitempty"`
	IncludeToolCalls *bool  `json:"include_tool_calls,omitempty" yaml:"include_tool_calls,omitempty"`
	Variables        any    `json:"variables" yaml:"variables"`
}

// Control is the strict superset decoded before Build selects the kind.
type Control struct {
	Kind     string            `json:"kind" yaml:"kind"`
	When     string            `json:"when,omitempty" yaml:"when,omitempty"`
	Task     *string           `json:"task,omitempty" yaml:"task,omitempty"`
	Group    *string           `json:"group,omitempty" yaml:"group,omitempty"`
	Assign   map[string]string `json:"assign,omitempty" yaml:"assign,omitempty"`
	To       *string           `json:"to,omitempty" yaml:"to,omitempty"`
	Announce *string           `json:"announce,omitempty" yaml:"announce,omitempty"`
	Requires []string          `json:"requires,omitempty" yaml:"requires,omitempty"`
	Context  *TransferContext  `json:"context,omitempty" yaml:"context,omitempty"`
	// A human_transfer names its shape with a block, never a `mode:` field, so a
	// warm-only field on a cold transfer is unwritable rather than rejected by a
	// cross-field rule (SCHEMA N25, the N19 argument applied to controls). The
	// block carries every parameter of the transfer, `destination` included, so
	// there is no such thing as an empty shape block (SCHEMA N27).
	Cold *ColdTransfer `json:"cold,omitempty" yaml:"cold,omitempty"`
	Warm *WarmTransfer `json:"warm,omitempty" yaml:"warm,omitempty"`
}

// ColdTransfer is the `cold:` block: hand the caller to the destination and drop
// out.
type ColdTransfer struct {
	Destination   string `json:"destination" yaml:"destination"`
	RingTimeout   string `json:"ring_timeout,omitempty" yaml:"ring_timeout,omitempty"`
	OnUnavailable string `json:"on_unavailable,omitempty" yaml:"on_unavailable,omitempty"`
}

// WarmTransfer is the `warm:` block: hold the caller, ring the person, brief
// them, then bridge the two. `briefing` is free text (SCHEMA N25); the drivers
// pass the call transcript alongside it on their own.
type WarmTransfer struct {
	Destination   string `json:"destination" yaml:"destination"`
	Briefing      string `json:"briefing,omitempty" yaml:"briefing,omitempty"`
	RingTimeout   string `json:"ring_timeout,omitempty" yaml:"ring_timeout,omitempty"`
	OnUnavailable string `json:"on_unavailable,omitempty" yaml:"on_unavailable,omitempty"`
}

// TransferDestination reports the destination the selected shape block names.
func (c Control) TransferDestination() string {
	switch {
	case c.Cold != nil && c.Warm != nil:
		return ""
	case c.Cold != nil:
		return c.Cold.Destination
	case c.Warm != nil:
		return c.Warm.Destination
	}
	return ""
}

// TransferShape reports the shape block the control selects, or "" when it
// carries neither. Build rejects zero and two, so a built package always
// answers with exactly one.
func (c Control) TransferShape() string {
	switch {
	case c.Cold != nil && c.Warm != nil:
		return ""
	case c.Cold != nil:
		return "cold"
	case c.Warm != nil:
		return "warm"
	}
	return ""
}

// Tool is one tools/<name>.yaml. The top level is the contract with the model
// (description/input/output) plus the two conversation scalars; how the tool
// runs lives in exactly one execution-keyed block, so a field belonging to
// another execution kind is unwritable rather than merely rejected (SCHEMA §5.2).
type Tool struct {
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	Input       map[string]any `json:"input,omitempty" yaml:"input,omitempty"`
	Output      map[string]any `json:"output,omitempty" yaml:"output,omitempty"`

	// Inject is a flat map of request key to scalar, merged into the call and
	// never advertised to the model: a value may carry {{variable}} tokens.
	// Legal on webhook and local only: an mcp server owns its own call shape,
	// so there is nothing here to merge into (validate.go, SCHEMA N40).
	Inject map[string]any `json:"inject,omitempty" yaml:"inject,omitempty"`

	Webhook        *ToolWebhook  `json:"webhook,omitempty" yaml:"webhook,omitempty"`
	Local          *ToolLocal    `json:"local,omitempty" yaml:"local,omitempty"`
	MCP            *ToolMCP      `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	Builtin        *ToolBuiltin  `json:"builtin,omitempty" yaml:"builtin,omitempty"`
	Client         *ToolNoFields `json:"client,omitempty" yaml:"client,omitempty"`
	ProviderHosted *ToolNoFields `json:"provider_hosted,omitempty" yaml:"provider_hosted,omitempty"`

	Interruption string `json:"interruption,omitempty" yaml:"interruption,omitempty"`
	Effect       string `json:"effect,omitempty" yaml:"effect,omitempty"`

	// Announce is one fixed sentence the agent speaks as the tool starts, so a
	// slow call is not silence. Legal on webhook and local only: every other
	// kind has no body to speak before (validate.go).
	//
	// ponytail: a plain string, not a pointer, because both execution-block
	// agreement tests read every pointer field on Tool as an execution block.
	// A pointer here would claim a seventh block that does not exist.
	Announce string `json:"announce,omitempty" yaml:"announce,omitempty"`
}

// ToolWebhook is the `webhook:` block: an HTTP endpoint named by env var, with
// optional authentication.
type ToolWebhook struct {
	URLEnv string `json:"url_env" yaml:"url_env"`
	// Path is appended to the env base URL; it may carry {{variable}} tokens,
	// whose rendered values are URL-encoded (variable_secrets_specs.md I.authoring.tool).
	Path string    `json:"path,omitempty" yaml:"path,omitempty"`
	Auth *ToolAuth `json:"auth,omitempty" yaml:"auth,omitempty"`
}

// ToolLocal is the `local:` block: a Python handler in the package.
type ToolLocal struct {
	Handler string `json:"handler,omitempty" yaml:"handler,omitempty"`
}

// ToolMCP is the `mcp:` block: one remote MCP server used as a tool source
// (SCHEMA N40). The server owns each tool's name, description, and parameters,
// so the file only says how to reach the server and which of its tools to
// expose. Every address and secret is an environment variable name, never a
// value.
type ToolMCP struct {
	URLEnv string `json:"url_env" yaml:"url_env"`
	// Transport is `sse` or `streamable_http`. Empty means the platform's own
	// default for the URL (a path ending in /mcp is streamable HTTP).
	Transport string `json:"transport,omitempty" yaml:"transport,omitempty"`
	// Auth is the same shape webhook auth uses (bearer, api_key).
	Auth *ToolAuth `json:"auth,omitempty" yaml:"auth,omitempty"`
	// Tools selects server tool names to expose. Empty means every tool the
	// server offers.
	Tools []string `json:"tools,omitempty" yaml:"tools,omitempty"`
}

// ToolBuiltin is the `builtin:` block: a prebuilt-tool registry id plus its
// optional closing line.
type ToolBuiltin struct {
	ID           string `json:"id" yaml:"id"`
	Instructions string `json:"instructions,omitempty" yaml:"instructions,omitempty"`
}

// ToolNoFields is an execution kind with nothing to configure. Both kinds using
// it stay gated on every target, so the empty block (`client: {}`) is only ever
// written to hit that gate.
type ToolNoFields struct{}

// ToolAuth authenticates a webhook call. Type selects the scheme; the token is
// always an env var name, and `header` belongs to api_key alone.
type ToolAuth struct {
	Type string `json:"type" yaml:"type"`
	// bearer, api_key: the token's env var.
	TokenEnv string `json:"token_env,omitempty" yaml:"token_env,omitempty"`
	// api_key: header name (default X-API-Key).
	Header string `json:"header,omitempty" yaml:"header,omitempty"`
}

// ExecutionKind reports the execution kind the file's block selects, or "" when
// no block is present. Load rejects zero and two-or-more blocks, so a loaded
// package always answers with exactly one kind.
func (t Tool) ExecutionKind() string {
	switch {
	case t.Webhook != nil:
		return "webhook"
	case t.Local != nil:
		return "local"
	case t.MCP != nil:
		return "mcp"
	case t.Builtin != nil:
		return "builtin"
	case t.Client != nil:
		return "client"
	case t.ProviderHosted != nil:
		return "provider_hosted"
	}
	return ""
}

type Conversation struct {
	Greeting      *Greeting     `json:"greeting,omitempty" yaml:"greeting,omitempty"`
	Interruption  *Interruption `json:"interruption,omitempty" yaml:"interruption,omitempty"`
	Inactivity    *Inactivity   `json:"inactivity,omitempty" yaml:"inactivity,omitempty"`
	MaxDuration   string        `json:"max_duration,omitempty" yaml:"max_duration,omitempty"`
	ThinkingAudio string        `json:"thinking_audio,omitempty" yaml:"thinking_audio,omitempty"`
}

type Greeting struct {
	SpeaksFirst string `json:"speaks_first" yaml:"speaks_first"`
	Text        string `json:"text,omitempty" yaml:"text,omitempty"`
}

type Interruption struct {
	Enabled       *bool    `json:"enabled" yaml:"enabled"`
	MinimumWords  int      `json:"minimum_words,omitempty" yaml:"minimum_words,omitempty"`
	IgnorePhrases []string `json:"ignore_phrases,omitempty" yaml:"ignore_phrases,omitempty"`
}

type Inactivity struct {
	NudgeAfter string `json:"nudge_after,omitempty" yaml:"nudge_after,omitempty"`
	EndAfter   string `json:"end_after,omitempty" yaml:"end_after,omitempty"`
}

type Channel struct {
	Kind             string   `json:"kind" yaml:"kind"`
	Inbound          *bool    `json:"inbound,omitempty" yaml:"inbound,omitempty"`
	Outbound         *bool    `json:"outbound,omitempty" yaml:"outbound,omitempty"`
	RequiredControls []string `json:"required_controls,omitempty" yaml:"required_controls,omitempty"`
	OnVoicemail      string   `json:"on_voicemail,omitempty" yaml:"on_voicemail,omitempty"`
}

type Capacity struct {
	PeakSessions        int     `json:"peak_sessions" yaml:"peak_sessions"`
	MaxSessions         int     `json:"max_sessions" yaml:"max_sessions"`
	PeakStartsPerSecond float64 `json:"peak_starts_per_second,omitempty" yaml:"peak_starts_per_second,omitempty"`
	AvgSessionDuration  string  `json:"avg_session_duration" yaml:"avg_session_duration"`
}

// Connection is one phone route: the mechanism, the carrier that hands over the
// call, and the environment variable names holding that account's credentials.
// It is readable on its own — a target names it and declares nothing else about
// how a call arrives.
type Connection struct {
	Transport   string            `json:"transport,omitempty" yaml:"transport,omitempty"`
	Carrier     string            `json:"carrier,omitempty" yaml:"carrier,omitempty"`
	Kind        string            `json:"kind" yaml:"kind"`
	Environment map[string]string `json:"environment" yaml:"environment"`
}

type TargetsFile struct {
	Targets map[string]Target `json:"targets" yaml:"targets"`
}

type Target struct {
	Provider         string              `json:"provider" yaml:"provider"`
	Version          string              `json:"version,omitempty" yaml:"version,omitempty"`
	Pins             map[string]string   `json:"pins,omitempty" yaml:"pins,omitempty"`
	SDKLanguage      string              `json:"sdk_language,omitempty" yaml:"sdk_language,omitempty"`
	Connection       string              `json:"connection,omitempty" yaml:"connection,omitempty"`
	DeploymentRegion Regions             `json:"deployment_region,omitempty" yaml:"deployment_region,omitempty"` // where the platform deploys the agent: one region or several (N18, widened by N32)
	Models           map[string]ModelDef `json:"models,omitempty" yaml:"models,omitempty"`                       // per-target overrides (N15), keyed by model name / listen / turn

	// Moved fields, kept on the decode struct so a package written the old way
	// still parses and can be refused with a message naming the new home rather
	// than a bare "unknown field" (Principle II). `json:"-"` keeps them out of
	// the derived authoring schema, which authoring_surface_test.go asserts.
	//
	// Transport and Carrier now live in connections/<name>.yaml; Destinations at
	// the top level of agent.yaml.
	Transport    string            `json:"-" yaml:"transport,omitempty"`
	Carrier      string            `json:"-" yaml:"carrier,omitempty"`
	Destinations map[string]string `json:"-" yaml:"destinations,omitempty"`
}
