package ir

import (
	"reflect"
	"testing"
)

func TestToVoiceAgentsRuntime(t *testing.T) { // V24, V25, V26, V27, V28
	got := ToVoiceAgentsRuntime(
		AgentConfig{
			Greeting: "Hi, thanks for calling. How can I help you today?",
			Language: "en",
		},
		ComplianceConfig{
			Region: "ap-south",
		},
		IdleNudgesConfig{
			Enabled:                 true,
			FirstNudgeDelaySeconds:  15,
			SecondNudgeDelaySeconds: 30,
			HangupDelaySeconds:      15,
			FirstNudgeText:          "Are you still there?",
			SecondNudgeText:         "I'm still here. If you need a moment, just let me know.",
			FinalHangupText:         "I'll end the call now. Please feel free to call back when you're ready.",
		},
		InterruptionConfig{Enabled: true},
	)

	enableInterruptions := true
	wantIdle := &IdleNudgesConfig{
		Enabled:                 true,
		FirstNudgeDelaySeconds:  15,
		SecondNudgeDelaySeconds: 30,
		HangupDelaySeconds:      15,
		FirstNudgeText:          "Are you still there?",
		SecondNudgeText:         "I'm still here. If you need a moment, just let me know.",
		FinalHangupText:         "I'll end the call now. Please feel free to call back when you're ready.",
	}
	want := VoiceAgentsRuntime{
		Language:            "en",
		Region:              "ap-south",
		IdleNudges:          wantIdle,
		EnableInterruptions: &enableInterruptions,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToVoiceAgentsRuntime() mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestToVoiceAgentsRuntime_preservesFalseInterruption(t *testing.T) { // V27, V28
	got := ToVoiceAgentsRuntime(
		AgentConfig{},
		ComplianceConfig{},
		IdleNudgesConfig{},
		InterruptionConfig{Enabled: false},
	)

	if got.EnableInterruptions == nil {
		t.Fatal("enable_interruptions should be present even when false")
	}
	if *got.EnableInterruptions {
		t.Fatal("enable_interruptions should preserve false")
	}
}
