package spec

import (
	"fmt"
	"strings"
)

// Package is the decoded, unresolved v1 package assembled from its files.
type Package struct {
	Agent   AgentFile         `json:"agent" yaml:"agent"`
	Tools   map[string]Tool   `json:"tools,omitempty" yaml:"tools,omitempty"`
	Targets map[string]Target `json:"targets" yaml:"targets"`

	Root     string            `json:"-" yaml:"-"`
	Markdown map[string]string `json:"-" yaml:"-"`
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
	Version      int                     `json:"version" yaml:"version"`
	EntryAgent   string                  `json:"entry_agent" yaml:"entry_agent"`
	Pipeline     Pipeline                `json:"pipeline" yaml:"pipeline"`
	Models       map[string]ModelProfile `json:"models" yaml:"models"`
	Voices       map[string]VoiceProfile `json:"voices" yaml:"voices"`
	Variables    map[string]Variable     `json:"variables,omitempty" yaml:"variables,omitempty"`
	Agents       map[string]AgentDef     `json:"agents" yaml:"agents"`
	Tasks        map[string]Task         `json:"tasks,omitempty" yaml:"tasks,omitempty"`
	TaskGroups   map[string]TaskGroup    `json:"task_groups,omitempty" yaml:"task_groups,omitempty"`
	Controls     map[string]Control      `json:"controls,omitempty" yaml:"controls,omitempty"`
	Tools        []string                `json:"tools,omitempty" yaml:"tools,omitempty"`
	Conversation *Conversation           `json:"conversation,omitempty" yaml:"conversation,omitempty"`
	Channels     map[string]Channel      `json:"channels" yaml:"channels"`
	Capacity     *Capacity               `json:"capacity,omitempty" yaml:"capacity,omitempty"`
}

type Pipeline struct {
	Listen PipelineRole `json:"listen" yaml:"listen"`
	Turn   *TurnRole    `json:"turn,omitempty" yaml:"turn,omitempty"`
	Speak  PipelineRole `json:"speak" yaml:"speak"`
}

type PipelineRole struct {
	Placement string `json:"placement" yaml:"placement"`
}

type TurnRole struct {
	Placement           string `json:"placement" yaml:"placement"`
	SemanticEndpointing string `json:"semantic_endpointing,omitempty" yaml:"semantic_endpointing,omitempty"`
}

type ModelProfile struct {
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Placement   string   `json:"placement" yaml:"placement"`
	Fallback    []string `json:"fallback,omitempty" yaml:"fallback,omitempty"`
}

type VoiceProfile struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
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
	Task        string            `json:"task,omitempty" yaml:"task,omitempty"`
	Group       string            `json:"group,omitempty" yaml:"group,omitempty"`
	Assign      map[string]string `json:"assign,omitempty" yaml:"assign,omitempty"`
	To          string            `json:"to,omitempty" yaml:"to,omitempty"`
	Requires    []string          `json:"requires,omitempty" yaml:"requires,omitempty"`
	Context     *TransferContext  `json:"context,omitempty" yaml:"context,omitempty"`
	Destination string            `json:"destination,omitempty" yaml:"destination,omitempty"`
	Mode        string            `json:"mode,omitempty" yaml:"mode,omitempty"`
	Briefing    string            `json:"briefing,omitempty" yaml:"briefing,omitempty"`
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
	Enabled       bool     `json:"enabled" yaml:"enabled"`
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
	PeakSessions       int    `json:"peak_sessions" yaml:"peak_sessions"`
	MaxSessions        int    `json:"max_sessions" yaml:"max_sessions"`
	AvgSessionDuration string `json:"avg_session_duration" yaml:"avg_session_duration"`
}

type TargetsFile struct {
	Targets map[string]Target `json:"targets" yaml:"targets"`
}

type Target struct {
	Provider     string            `json:"provider" yaml:"provider"`
	Version      string            `json:"version,omitempty" yaml:"version,omitempty"`
	Pins         map[string]string `json:"pins,omitempty" yaml:"pins,omitempty"`
	SDKLanguage  string            `json:"sdk_language,omitempty" yaml:"sdk_language,omitempty"`
	Transport    string            `json:"transport,omitempty" yaml:"transport,omitempty"`
	Carrier      string            `json:"carrier,omitempty" yaml:"carrier,omitempty"`
	Region       string            `json:"region,omitempty" yaml:"region,omitempty"`
	Edition      string            `json:"edition,omitempty" yaml:"edition,omitempty"`
	Models       Bindings          `json:"models" yaml:"models"`
	Destinations map[string]string `json:"destinations,omitempty" yaml:"destinations,omitempty"`
}

type Bindings struct {
	Listen *Binding           `json:"listen,omitempty" yaml:"listen,omitempty"`
	Turn   *Binding           `json:"turn,omitempty" yaml:"turn,omitempty"`
	Speak  map[string]Binding `json:"speak,omitempty" yaml:"speak,omitempty"`
	Reason map[string]Binding `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type Binding struct {
	Provider    string         `json:"provider,omitempty" yaml:"provider,omitempty"`
	Model       string         `json:"model,omitempty" yaml:"model,omitempty"`
	Voice       string         `json:"voice,omitempty" yaml:"voice,omitempty"`
	VoiceID     string         `json:"voice_id,omitempty" yaml:"voice_id,omitempty"`
	EndpointEnv string         `json:"endpoint_env,omitempty" yaml:"endpoint_env,omitempty"`
	Placement   string         `json:"placement,omitempty" yaml:"placement,omitempty"`
	Params      map[string]any `json:"params,omitempty" yaml:"params,omitempty"`
}
