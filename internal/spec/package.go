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
	Language   string        `json:"language,omitempty" yaml:"language,omitempty"`
	EntryAgent string        `json:"entry_agent" yaml:"entry_agent"`
	Models     ModelSections `json:"models" yaml:"models"`
	// Listen/Turn select one entry of the matching models section by name.
	// Optional when the section has at most one entry (the sole entry selects
	// itself); required with 2+ entries (N15 palette).
	Listen       string               `json:"listen,omitempty" yaml:"listen,omitempty"`
	Turn         string               `json:"turn,omitempty" yaml:"turn,omitempty"`
	Variables    map[string]Variable  `json:"variables,omitempty" yaml:"variables,omitempty"`
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
	Provider            string         `json:"provider,omitempty" yaml:"provider,omitempty"`
	Model               string         `json:"model,omitempty" yaml:"model,omitempty"`
	Voice               string         `json:"voice,omitempty" yaml:"voice,omitempty"`
	Speed               *float64       `json:"speed,omitempty" yaml:"speed,omitempty"`
	Language            string         `json:"language,omitempty" yaml:"language,omitempty"`
	Temperature         *float64       `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	TopP                *float64       `json:"top_p,omitempty" yaml:"top_p,omitempty"`
	TopK                *int           `json:"top_k,omitempty" yaml:"top_k,omitempty"`
	EndpointEnv         string         `json:"endpoint_env,omitempty" yaml:"endpoint_env,omitempty"`
	Placement           string         `json:"placement,omitempty" yaml:"placement,omitempty"`
	SemanticEndpointing string         `json:"semantic_endpointing,omitempty" yaml:"semantic_endpointing,omitempty"`
	Params              map[string]any `json:"params,omitempty" yaml:"params,omitempty"`
	Fallback            []string       `json:"fallback,omitempty" yaml:"fallback,omitempty"`
	Description         string         `json:"description,omitempty" yaml:"description,omitempty"`
}

type Variable struct {
	Type    string `json:"type" yaml:"type"`
	Default any    `json:"default,omitempty" yaml:"default,omitempty"`
	Source  string `json:"source,omitempty" yaml:"source,omitempty"`
}

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
	Kind        string            `json:"kind" yaml:"kind"`
	When        string            `json:"when,omitempty" yaml:"when,omitempty"`
	Task        *string           `json:"task,omitempty" yaml:"task,omitempty"`
	Group       *string           `json:"group,omitempty" yaml:"group,omitempty"`
	Assign      map[string]string `json:"assign,omitempty" yaml:"assign,omitempty"`
	To          *string           `json:"to,omitempty" yaml:"to,omitempty"`
	Requires    []string          `json:"requires,omitempty" yaml:"requires,omitempty"`
	Context     *TransferContext  `json:"context,omitempty" yaml:"context,omitempty"`
	Destination *string           `json:"destination,omitempty" yaml:"destination,omitempty"`
	Mode        *string           `json:"mode,omitempty" yaml:"mode,omitempty"`
	Briefing    *string           `json:"briefing,omitempty" yaml:"briefing,omitempty"`
}

type Tool struct {
	Description  string         `json:"description" yaml:"description"`
	Input        map[string]any `json:"input" yaml:"input"`
	Output       map[string]any `json:"output,omitempty" yaml:"output,omitempty"`
	Execution    string         `json:"execution" yaml:"execution"`
	Handler      string         `json:"handler,omitempty" yaml:"handler,omitempty"`
	URLEnv       string         `json:"url_env,omitempty" yaml:"url_env,omitempty"`
	Interruption string         `json:"interruption,omitempty" yaml:"interruption,omitempty"`
	Effect       string         `json:"effect,omitempty" yaml:"effect,omitempty"`
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

type Connection struct {
	Kind        string            `json:"kind" yaml:"kind"`
	Environment map[string]string `json:"environment" yaml:"environment"`
}

type TargetsFile struct {
	Targets map[string]Target `json:"targets" yaml:"targets"`
}

type Target struct {
	Provider     string              `json:"provider" yaml:"provider"`
	Version      string              `json:"version,omitempty" yaml:"version,omitempty"`
	Pins         map[string]string   `json:"pins,omitempty" yaml:"pins,omitempty"`
	SDKLanguage  string              `json:"sdk_language,omitempty" yaml:"sdk_language,omitempty"`
	Transport    string              `json:"transport,omitempty" yaml:"transport,omitempty"`
	Carrier      string              `json:"carrier,omitempty" yaml:"carrier,omitempty"`
	Connection   string              `json:"connection,omitempty" yaml:"connection,omitempty"`
	Region       string              `json:"region,omitempty" yaml:"region,omitempty"`
	Edition      string              `json:"edition,omitempty" yaml:"edition,omitempty"`
	Models       map[string]ModelDef `json:"models,omitempty" yaml:"models,omitempty"` // per-target overrides (N15), keyed by model name / listen / turn
	Destinations map[string]string   `json:"destinations,omitempty" yaml:"destinations,omitempty"`
}
