package scaffold

import (
	"slices"
	"testing"

	targetcap "github.com/slng-ai/unmute/internal/target"
)

// Selecting twilio must scaffold a package the real compiler accepts, with no
// browser channel, no SLNG binding and no framework pin.
func TestTwilioScaffoldPassesPreflight(t *testing.T) {
	var data Data
	data.Name = "relay"
	data.SetTarget(string(targetcap.Twilio))
	report, err := Preflight(data)
	if err != nil {
		t.Fatalf("a fresh twilio package failed preflight: %v", err)
	}
	if len(report.Warnings) > 0 {
		t.Errorf("a fresh twilio package warns: %v", report.Warnings)
	}
	for _, want := range []string{"OPENAI_API_KEY", "TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PUBLIC_URL"} {
		if !slices.Contains(report.RequiredEnv, want) {
			t.Errorf("required env %v lacks %s", report.RequiredEnv, want)
		}
	}
	if data.TargetVersion != "" || data.Pins != "" {
		t.Errorf("twilio starter carries a framework version or pins: %q %q", data.TargetVersion, data.Pins)
	}
	// Leaving twilio drops its phone channel with it.
	data.SetTarget(string(targetcap.Pipecat))
	if len(data.Channels) > 0 {
		t.Errorf("switching away from twilio kept its channel: %v", data.Channels)
	}
}
