package ir

import "slices"

// Variables is the canonical shape of agent/variables.yaml.
//
// User variables are caller/dispatcher-supplied template values. System
// variables are runtime-supplied session context. Values do not live here.
type Variables struct {
	User   map[string]UserVariable   `json:"user,omitempty" yaml:"user,omitempty"`
	System map[string]SystemVariable `json:"system,omitempty" yaml:"system,omitempty"`
}

// UserVariable declares one {{variable}} that can be supplied by an author
// default or by call/session arguments.
type UserVariable struct {
	Description string `json:"description" yaml:"description"`
	Default     string `json:"default,omitempty" yaml:"default,omitempty"`
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
}

// SystemVariable declares one runtime-supplied {{variable}}.
type SystemVariable struct {
	Source      SystemVariableSource `json:"source" yaml:"source"`
	Description string               `json:"description" yaml:"description"`
	Config      map[string]string    `json:"config,omitempty" yaml:"config,omitempty"`
}

// SystemVariableSource identifies the runtime context value that feeds a
// system variable.
type SystemVariableSource string

const (
	SystemVariableSourceCallID             SystemVariableSource = "call_id"
	SystemVariableSourceRoomName           SystemVariableSource = "room_name"
	SystemVariableSourceJobID              SystemVariableSource = "job_id"
	SystemVariableSourceAgentID            SystemVariableSource = "agent_id"
	SystemVariableSourceAgentName          SystemVariableSource = "agent_name"
	SystemVariableSourcePhoneNumber        SystemVariableSource = "phone_number"
	SystemVariableSourceFirstUserMessage   SystemVariableSource = "first_user_message"
	SystemVariableSourceCallEndReason      SystemVariableSource = "call_end_reason"
	SystemVariableSourceTranscriptMessages SystemVariableSource = "transcript_messages"
	SystemVariableSourceCurrentDatetime    SystemVariableSource = "current_datetime"
)

// SystemVariableSources lists supported v1 system sources in stable order for
// schema generation, docs, and validation.
var SystemVariableSources = []SystemVariableSource{
	SystemVariableSourceCallID,
	SystemVariableSourceRoomName,
	SystemVariableSourceJobID,
	SystemVariableSourceAgentID,
	SystemVariableSourceAgentName,
	SystemVariableSourcePhoneNumber,
	SystemVariableSourceFirstUserMessage,
	SystemVariableSourceCallEndReason,
	SystemVariableSourceTranscriptMessages,
	SystemVariableSourceCurrentDatetime,
}

// IsValid reports whether s is one of the v1 runtime/system context sources.
func (s SystemVariableSource) IsValid() bool {
	return slices.Contains(SystemVariableSources, s)
}
