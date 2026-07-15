package ir

// AgentConfig is the shape of agent/agent.yaml.
type AgentConfig struct {
	Greeting string `json:"greeting" yaml:"greeting"`
	Language string `json:"language,omitempty" yaml:"language,omitempty"`
}

// ComplianceConfig is the shape of agent/compliance.yaml.
//
// The compliance file currently preserves only the declared region boundary.
type ComplianceConfig struct {
	Region string `json:"region" yaml:"region"`
}

// IdleNudgesConfig is the shape of agent/overrides/idle.yaml.
type IdleNudgesConfig struct {
	Enabled                 bool   `json:"enabled" yaml:"enabled"`
	FirstNudgeDelaySeconds  int    `json:"first_nudge_delay_seconds" yaml:"first_nudge_delay_seconds"`
	SecondNudgeDelaySeconds int    `json:"second_nudge_delay_seconds" yaml:"second_nudge_delay_seconds"`
	HangupDelaySeconds      int    `json:"hangup_delay_seconds" yaml:"hangup_delay_seconds"`
	FirstNudgeText          string `json:"first_nudge_text" yaml:"first_nudge_text"`
	SecondNudgeText         string `json:"second_nudge_text" yaml:"second_nudge_text"`
	FinalHangupText         string `json:"final_hangup_text" yaml:"final_hangup_text"`
}

// InterruptionConfig is the shape of agent/overrides/interruption.yaml.
type InterruptionConfig struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// VoiceAgentsRuntime is the Voice Agents API runtime/config object produced
// from agent.yaml, compliance.yaml, and overrides. Field names match the API
// contract, not the authoring filenames.
type VoiceAgentsRuntime struct {
	Language            string            `json:"language,omitempty" yaml:"language,omitempty"`
	Region              string            `json:"region,omitempty" yaml:"region,omitempty"`
	IdleNudges          *IdleNudgesConfig `json:"idle_nudges,omitempty" yaml:"idle_nudges,omitempty"`
	EnableInterruptions *bool             `json:"enable_interruptions,omitempty" yaml:"enable_interruptions,omitempty"`
}

// ToVoiceAgentsRuntime maps authoring runtime files to Voice Agents API-shaped
// fields. Full compliance enforcement stays in deploy/resolve.
func ToVoiceAgentsRuntime(
	agent AgentConfig,
	compliance ComplianceConfig,
	idle IdleNudgesConfig,
	interruption InterruptionConfig,
) VoiceAgentsRuntime {
	enableInterruptions := interruption.Enabled
	idleCopy := idle

	return VoiceAgentsRuntime{
		Language:            agent.Language,
		Region:              compliance.Region,
		IdleNudges:          &idleCopy,
		EnableInterruptions: &enableInterruptions,
	}
}
