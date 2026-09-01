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
	// Documents holds each knowledge base document, keyed by the path it takes
	// inside the artifact (knowledge/<base>/<file>). []byte, not string, because
	// a PDF is binary, and read but never parsed: the compiler has no PDF parser
	// and needs none, because the emitted project reads the original at startup.
	Documents map[string][]byte `json:"-" yaml:"-"`
	files     map[string][]byte
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
	Version int `json:"version" yaml:"version"`
	// Name is what this agent is called wherever it is deployed under a name
	// rather than an id. It lives here, in the package, because that is the only
	// thing about it that is stable: a target instance is named for where the
	// package goes (`slng:`, which every package following the docs writes), and
	// the folder on disk is named by whoever cloned the repository. Neither is an
	// identity, and on SLNG the name IS the identity: names are unique per
	// organisation and a push resolves the agent to update by matching one, so a
	// name that two packages share is one live agent that two packages overwrite.
	Name       string        `json:"name,omitempty" yaml:"name,omitempty"`
	EntryAgent string        `json:"entry_agent" yaml:"entry_agent"`
	Models     ModelSections `json:"models" yaml:"models"`
	// Listen/Turn select one entry of the matching models section by name.
	// Optional when the section has at most one entry (the sole entry selects
	// itself); required with 2+ entries (N15 palette).
	Listen    string              `json:"listen,omitempty" yaml:"listen,omitempty"`
	Turn      string              `json:"turn,omitempty" yaml:"turn,omitempty"`
	Variables map[string]Variable `json:"variables,omitempty" yaml:"variables,omitempty"`
	// Timezone is the IANA zone every clock reading in this package is taken in.
	// Required before a clock can be pre-fetched, and required rather than
	// defaulted because the wrong answer here is silent: a container clock is
	// UTC, so a salon in Spain taking a booking for "tomorrow" at 23:30 local
	// would confidently name the wrong day. Package-level, beside Variables,
	// because it is a fact about the business rather than about one target.
	Timezone string `json:"timezone,omitempty" yaml:"timezone,omitempty"`
	// Prefetch names the facts resolved once per call, before the greeting, and
	// where each one lands. An ordered list: entries resolve top to bottom, so
	// the order the file shows is the order the agent uses, and an entry reading
	// a value a later entry assigns is refused rather than quietly reordered
	// (CLAUDE.md, no dictionaries in the authoring surface).
	Prefetch []Prefetch `json:"prefetch,omitempty" yaml:"prefetch,omitempty"`
	Secrets  []string   `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	// Destinations maps a symbolic name a control escalates to onto the name of
	// an environment variable holding the number. A destination is who this agent
	// escalates to, which is the same desk whichever carrier reaches it, so it
	// lives here rather than on a target.
	Destinations map[string]string `json:"destinations,omitempty" yaml:"destinations,omitempty"`
	// Knowledge maps a name onto a folder of documents the agents read from.
	// Package-level, beside Models and Destinations, because a corpus belongs to
	// the package: two tools over one folder is normal, and putting the folder on
	// the tool would re-index the same documents once per tool reading them.
	Knowledge map[string]KnowledgeDef `json:"knowledge,omitempty" yaml:"knowledge,omitempty"`
	Agents    map[string]AgentDef     `json:"agents" yaml:"agents"`
	// The three catalogs. Every agent-level list has a same-named top-level
	// catalog, and the block an entry is written in IS its kind, so there is no
	// `kind:` field that could disagree with where the entry sits. Declared here,
	// after Agents and before Tasks, because struct order is what the derived
	// schema publishes and the canonical section order starts from the reader.
	Delegates    map[string]Delegate   `json:"delegates,omitempty" yaml:"delegates,omitempty"`
	Handoffs     map[string]Handoff    `json:"handoffs,omitempty" yaml:"handoffs,omitempty"`
	Escalations  map[string]Escalation `json:"escalations,omitempty" yaml:"escalations,omitempty"`
	Tasks        map[string]Task       `json:"tasks,omitempty" yaml:"tasks,omitempty"`
	TaskGroups   map[string]TaskGroup  `json:"task_groups,omitempty" yaml:"task_groups,omitempty"`
	Tools        []string              `json:"tools,omitempty" yaml:"tools,omitempty"`
	Conversation *Conversation         `json:"conversation,omitempty" yaml:"conversation,omitempty"`
	Tracing      *Tracing              `json:"tracing,omitempty" yaml:"tracing,omitempty"`
	Channels     map[string]Channel    `json:"channels" yaml:"channels"`
	Capacity     *Capacity             `json:"capacity,omitempty" yaml:"capacity,omitempty"`
}

type Tracing struct {
	Provider string `json:"provider" yaml:"provider"`
}

// KnowledgeDef is one knowledge base: a folder of documents, the service that
// embeds them, and how the documents are cut up and searched.
//
// The three retrieval fields are pointers so an absent field is distinguishable
// from a zero one: `chunk_overlap: 0` is a legal choice and must not read as
// "unset, use the default".
//
// Still absent, and still on purpose: no embedding model (the service pins one),
// no relevance threshold (measurement found a genuine hit at 0.291 against an
// off-topic question at 0.293, so no cutoff separates them), and no retrieval
// mode (every lookup searches the same way).
type KnowledgeDef struct {
	// Documents is a folder path relative to the package root. A folder, never a
	// single file, so adding a second document needs no authoring change.
	Documents string `json:"documents" yaml:"documents"`
	// Embed names an embedding service. Empty resolves to openai at Build.
	Embed string `json:"embed,omitempty" yaml:"embed,omitempty"`
	// ChunkSize is the passage size in TOKENS, not characters. Absent resolves to
	// ir.DefaultChunkSize.
	ChunkSize *int `json:"chunk_size,omitempty" yaml:"chunk_size,omitempty"`
	// ChunkOverlap is how many tokens consecutive passages share, so a sentence
	// split across a boundary is still whole in one of them. Absent resolves to
	// ir.DefaultChunkOverlap.
	ChunkOverlap *int `json:"chunk_overlap,omitempty" yaml:"chunk_overlap,omitempty"`
	// TopK is how many passages a lookup returns. Absent resolves to
	// ir.DefaultTopK.
	TopK *int `json:"top_k,omitempty" yaml:"top_k,omitempty"`
	// Mode is how a lookup searches: meaning, keyword, or hybrid. Absent resolves
	// to ir.DefaultKnowledgeMode.
	Mode string `json:"mode,omitempty" yaml:"mode,omitempty"`
	// MinScore drops results scoring below it, where higher is closer. Absent
	// means no filtering, which is the default and the measured behaviour.
	//
	// A pointer, and absent by default, because measurement says most values hurt:
	// on the salon corpus 0.20 costs nothing and removes one off-topic question,
	// 0.25 already loses a real answer, and 0.40 loses nine of twelve. The scores
	// are similarities in roughly the 0.15 to 0.45 band, not probabilities, so a
	// value like 0.9 returns nothing at all.
	MinScore *float64 `json:"min_score,omitempty" yaml:"min_score,omitempty"`
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
	// Pace is how quickly the agent decides the caller has finished: snappy,
	// balanced or patient. It is the portable form of a wait, and each target
	// maps it onto its own floor and ceiling, because the two frameworks do not
	// spell either the same way.
	//
	// Empty means balanced, which is deliberately not "leave the runtime alone".
	// The runtime defaults are slower than the turn detector needs, so a package
	// that says nothing still gets the faster behaviour.
	//
	// Turn bindings only, and it takes no per-target override: a per-target pace
	// is a duration in disguise, and EndpointingDelay already exists for that.
	Pace string `json:"pace,omitempty" yaml:"pace,omitempty"`
	// EndpointingDelay is the window of silence that has to pass before the
	// runtime treats the caller as finished. It is the floor on every turn, so
	// shortening it shortens every answer and lengthening it gives a caller who
	// pauses mid-sentence more room. LiveKit will not accept less than 250ms.
	EndpointingDelay string `json:"endpointing_delay,omitempty" yaml:"endpointing_delay,omitempty"`
	// AgentID scopes the SLNG Context Router's cache. One stable value per
	// package, authored by a human, carrying a version suffix they own and bump
	// after a prompt change they judge meaningful. Never composed, never
	// derived, never hashed: a split id splits the cache.
	AgentID string `json:"agent_id,omitempty" yaml:"agent_id,omitempty"`
	// Upstream says where the router actually calls the model and whose
	// credentials pay for it. Required on a router think binding, because the
	// configuration travels inline on every request.
	Upstream *Upstream `json:"upstream,omitempty" yaml:"upstream,omitempty"`
	// PromptSuffix is literal text the compiler appends to every system prompt
	// this binding sends: each agent's, each task's, and the summarizer's where
	// one is emitted.
	//
	// It exists because some models take instructions no parameter can carry.
	// Qwen3 is a hybrid thinking model, and on 2026-08-27 three spellings of the
	// thinking-off parameter were sent to three of its hosts, nine requests: all
	// accepted, all ignored, hundreds of reasoning tokens each time. Its own
	// `/no_think` directive in the prompt is the only thing that worked, and it
	// worked from the system prompt, mid-prompt, with tools, and through the
	// router.
	//
	// On the model rather than on an agent, because it is a fact about the model.
	// An agent that needs different wording has its own prompt file. The compiler
	// does not know `/no_think` from any other string and must not learn: the next
	// model's directive will be different and this field should already work for
	// it.
	PromptSuffix string `json:"prompt_suffix,omitempty" yaml:"prompt_suffix,omitempty"`

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
	Type    string `json:"type" yaml:"type"`
	Default any    `json:"default,omitempty" yaml:"default,omitempty"`
	Source  string `json:"source,omitempty" yaml:"source,omitempty"`
	// Confirm names the step that must hear the caller agree before anything acts
	// on this value. Until then the value satisfies no prerequisite and renders
	// only in that step's own prompt. Empty means the value is settled the moment
	// it arrives, which is true of every variable that existed before this field.
	Confirm     string `json:"confirm,omitempty" yaml:"confirm,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Prefetch is one entry under `prefetch:`. Exactly one source key is present:
// `clock:` reads the package clock, `source:` reads a fact the call itself
// carries, and `tool:` runs one already-declared read-only tool. `args:` belongs
// to a `tool:` entry and nowhere else.
//
// The three source keys are plain strings rather than pointers, matching
// Handoff.To: each carries its own value, so an absent key is the empty string
// and Build refuses zero or more than one. Delegate.Task needs a pointer only
// because it must tell an absent key from one written empty; nothing here does.
//
// Source is the field that keeps a variable from ever having to declare a
// `source:` its target cannot supply, which is what leaves the existing
// per-route refusal exactly as strict as it is today.
type Prefetch struct {
	// Name is a field, not a map key, and that is the whole reason this block has
	// an order a reader can see. It is also what makes a per-entry comment land
	// somewhere a reader will find it, and what lets a refusal or a log line say
	// which entry it means.
	Name   string `json:"name" yaml:"name"`
	Clock  string `json:"clock,omitempty" yaml:"clock,omitempty"`
	Source string `json:"source,omitempty" yaml:"source,omitempty"`
	Tool   string `json:"tool,omitempty" yaml:"tool,omitempty"`
	Args   []Pair `json:"args,omitempty" yaml:"args,omitempty"`
	Assign []Pair `json:"assign,omitempty" yaml:"assign,omitempty"`
}

// Secret declares one runtime environment value the package needs. The map key
// IS the environment variable name (UPPER_SNAKE), so there is no field a value
// could ever be written into (variable_secrets_specs.md C3, V9).
// AgentDef is one entry under `agents:`. The four lists are what this agent can
// do, one list per kind, each naming entries in the same-named catalog. All four
// are optional: an agent with no delegates omits the key.
type AgentDef struct {
	Instructions string   `json:"instructions" yaml:"instructions"`
	Model        string   `json:"model" yaml:"model"`
	Voice        string   `json:"voice" yaml:"voice"`
	Tools        []string `json:"tools,omitempty" yaml:"tools,omitempty"`
	Delegates    []string `json:"delegates,omitempty" yaml:"delegates,omitempty"`
	Handoffs     []string `json:"handoffs,omitempty" yaml:"handoffs,omitempty"`
	Escalations  []string `json:"escalations,omitempty" yaml:"escalations,omitempty"`
}

// Task is one entry under `tasks:`. It has `tools:` and `handoffs:` and no
// `delegates:` and no `escalations:`, which is how "a task may attach handoffs
// only" stops being a validation rule and becomes structure: there is no key to
// write the illegal thing in.
type Task struct {
	Instructions string         `json:"instructions" yaml:"instructions"`
	Tools        []string       `json:"tools,omitempty" yaml:"tools,omitempty"`
	Handoffs     []string       `json:"handoffs,omitempty" yaml:"handoffs,omitempty"`
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

// Delegate is one entry under `delegates:`. It runs a task or a task group and
// then control comes back to the agent that ran it.
//
// Task and Group stay pointers because exactly one of them must be present,
// which no struct can express, so the check survives in Build and needs to tell
// an absent key from one written empty.
type Delegate struct {
	Task  *string `json:"task,omitempty" yaml:"task,omitempty"`
	Group *string `json:"group,omitempty" yaml:"group,omitempty"`
	When  string  `json:"when,omitempty" yaml:"when,omitempty"`
	// Announce is one fixed sentence the agent speaks as the step is entered, so
	// the two model requests it takes to enter one are not silence. Not spoken
	// when the step is refused for unmet prerequisites: the caller hearing "let
	// me pull up the diary" and then being asked for a phone number is worse than
	// hearing nothing.
	//
	// A plain string, matching Tool.Announce. Handoff.Announce is a *string for
	// its own historical reason and is not the model to copy here.
	Announce string            `json:"announce,omitempty" yaml:"announce,omitempty"`
	Requires []string          `json:"requires,omitempty" yaml:"requires,omitempty"`
	Assign   map[string]string `json:"assign,omitempty" yaml:"assign,omitempty"`
}

// Handoff is one entry under `handoffs:`. The conversation becomes another
// agent and never comes back.
//
// To is a plain string, not a pointer: a handoff always has a `to:`, so a
// missing one is the empty string and is refused by the same check that refuses
// a `to:` naming an agent that does not exist.
type Handoff struct {
	To       string           `json:"to" yaml:"to"`
	When     string           `json:"when,omitempty" yaml:"when,omitempty"`
	Announce *string          `json:"announce,omitempty" yaml:"announce,omitempty"`
	Requires []string         `json:"requires,omitempty" yaml:"requires,omitempty"`
	Context  *TransferContext `json:"context,omitempty" yaml:"context,omitempty"`
}

// Escalation is one entry under `escalations:`. The caller goes through to a
// person.
//
// The shape is named by a block, never by a `mode:` field, so a warm-only field
// on a cold transfer is unwritable rather than rejected by a cross-field rule
// (SCHEMA N25, the N19 argument applied to controls). The block carries every
// parameter of the transfer, `destination` included, so there is no such thing
// as an empty shape block (SCHEMA N27).
type Escalation struct {
	When string        `json:"when,omitempty" yaml:"when,omitempty"`
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
func (e Escalation) TransferDestination() string {
	switch {
	case e.Cold != nil && e.Warm != nil:
		return ""
	case e.Cold != nil:
		return e.Cold.Destination
	case e.Warm != nil:
		return e.Warm.Destination
	}
	return ""
}

// TransferShape reports the shape block the escalation selects, or "" when it
// carries neither. Build rejects zero and two, so a built package always answers
// with exactly one.
func (e Escalation) TransferShape() string {
	switch {
	case e.Cold != nil && e.Warm != nil:
		return ""
	case e.Cold != nil:
		return "cold"
	case e.Warm != nil:
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

	Webhook        *ToolWebhook   `json:"webhook,omitempty" yaml:"webhook,omitempty"`
	Local          *ToolLocal     `json:"local,omitempty" yaml:"local,omitempty"`
	MCP            *ToolMCP       `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	Builtin        *ToolBuiltin   `json:"builtin,omitempty" yaml:"builtin,omitempty"`
	Client         *ToolNoFields  `json:"client,omitempty" yaml:"client,omitempty"`
	ProviderHosted *ToolNoFields  `json:"provider_hosted,omitempty" yaml:"provider_hosted,omitempty"`
	Knowledge      *ToolKnowledge `json:"knowledge,omitempty" yaml:"knowledge,omitempty"`

	Interruption string `json:"interruption,omitempty" yaml:"interruption,omitempty"`
	Effect       string `json:"effect,omitempty" yaml:"effect,omitempty"`

	// ReadOnly is the author's promise that this tool writes nothing. Required
	// before a prefetch entry may run it, because a prefetch runs unasked on
	// every call: a tool that writes would write on every call, wrong numbers
	// included.
	//
	// Deliberately distinct from Effect, which describes what the tool does to
	// the conversation rather than what it does to data. The compiler cannot
	// check either claim, so this is a declaration and not a guarantee, and both
	// the docs and the skill say so in those words.
	//
	// ponytail: a plain bool, not a pointer, for the same reason Announce is a
	// plain string: both execution-block agreement tests read every pointer field
	// on Tool as an execution block.
	ReadOnly bool `json:"read_only,omitempty" yaml:"read_only,omitempty"`

	// Announce is one fixed sentence the agent speaks as the tool starts, so a
	// slow call is not silence. Legal on webhook and local only: every other
	// kind has no body to speak before (validate.go).
	//
	// ponytail: a plain string, not a pointer, because both execution-block
	// agreement tests read every pointer field on Tool as an execution block.
	// A pointer here would claim a seventh block that does not exist.
	Announce string `json:"announce,omitempty" yaml:"announce,omitempty"`
}

// ToolWebhook is the `webhook:` block: an HTTP endpoint named by env var or by
// a literal base URL, with optional authentication.
type ToolWebhook struct {
	// URLEnv names an environment variable holding the whole base URL. The code
	// drivers read it at run time, which is how a webhook host stays out of the
	// package.
	URLEnv string `json:"url_env,omitempty" yaml:"url_env,omitempty"`
	// BaseURL is the literal base, for a target whose platform stores the URL
	// rather than reading it from the agent's environment. SLNG's URL validator
	// requires a literal hostname and rejects a template token in the scheme or
	// the authority, so a name is not a shape it can take.
	//
	// It belongs on the tool rather than on the target because two webhook tools
	// in one package can point at two different hosts.
	//
	// A tool needs at least one of the two, and which one is required is a
	// question about the target: the code drivers read url_env and ignore this,
	// and the slng target does the reverse.
	BaseURL string `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	// Path is appended to the base URL; it may carry {{variable}} tokens,
	// whose rendered values are URL-encoded (variable_secrets_specs.md I.authoring.tool).
	Path string    `json:"path,omitempty" yaml:"path,omitempty"`
	Auth *ToolAuth `json:"auth,omitempty" yaml:"auth,omitempty"`
}

// ToolLocal is the `local:` block: a Python handler in the package.
type ToolLocal struct {
	Handler string `json:"handler,omitempty" yaml:"handler,omitempty"`
	// Dependencies are exact `name==version` pins the handler imports, for a
	// target that installs a per-tool environment. Each is an exact pin because a
	// range is not reproducible: the platform stores a canonical, sorted list and
	// a body that does not match it is not the body that exists.
	//
	// The code drivers build their project's dependency list from the provider
	// catalogue and read nothing per tool, so this field is refused there rather
	// than dropped. FieldToolDependencies is the row that says so.
	Dependencies []string `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
}

// ToolKnowledge is the `knowledge:` block: a lookup over one knowledge base
// declared in the package's knowledge: section. One field, because the tool owns
// its own schema (one string, the caller's question) and its own result shape
// (passages with sources and scores), so input: and output: have nowhere to go.
type ToolKnowledge struct {
	Base string `json:"base" yaml:"base"`
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
	case t.Knowledge != nil:
		return "knowledge"
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
	Enabled *bool `json:"enabled" yaml:"enabled"`
	// Protect names the stretches of the call the caller cannot talk over while
	// barge-in stays on everywhere else: "greeting", "tool_calls". An empty list
	// is not the same as an absent one — it means protect nothing, and it is how
	// an author turns off a default the target would otherwise apply.
	Protect       []string `json:"protect,omitempty" yaml:"protect,omitempty"`
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
	WarmInstances    int                 `json:"warm_instances,omitempty" yaml:"warm_instances,omitempty"`       // instances the platform holds ready, so a call is not waiting on a cold container
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
