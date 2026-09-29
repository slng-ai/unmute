package tui

import (
	"bytes"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/scaffold"
	"github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

func twilioStarter() scaffold.Data {
	data := scaffold.Data{Name: "relay", Tools: scaffold.DefaultTools()}
	data.SetTarget(string(targetcap.Twilio))
	return data
}

// Every twilio shape the console can author comes back from maintain with the
// speech bindings, the phone-only channel, the greeting either way round and
// either think provider as written. The only loss allowed is the one every
// target's fresh scaffold reports, the regenerated starter prompt.
func TestTwilioPackagesRoundTripThroughMaintain(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*scaffold.Data)
	}{
		{"openai starter", func(*scaffold.Data) {}},
		{"gemini starter", func(d *scaffold.Data) { d.Reason = scaffold.TwilioReasonStarter("google") }},
		{"gemini on vertex", func(d *scaffold.Data) {
			d.Reason = scaffold.Binding{Provider: "google", Model: scaffold.TwilioGeminiModel,
				Params: "vertexai: true\nlocation: eu\nthinking_config:\n  thinking_level: MINIMAL"}
		}},
		{"gemini alias", func(d *scaffold.Data) { d.Reason = scaffold.TwilioReasonStarter("gemini") }},
		{"caller speaks first", func(d *scaffold.Data) { d.SpeaksFirst, d.Greeting = "user", "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := twilioStarter()
			tc.edit(&data)
			if _, err := scaffold.Preflight(data); err != nil {
				t.Fatalf("preflight: %v", err)
			}
			dir := filepath.Join(t.TempDir(), "relay")
			if _, err := scaffold.Write(dir, data); err != nil {
				t.Fatal(err)
			}
			agent, err := loadMaintained(dir)
			if err != nil {
				t.Fatal(err)
			}
			if losses := slices.DeleteFunc(slices.Clone(agent.losses), func(l string) bool { return l == "instructions.md: content" }); len(losses) > 0 {
				t.Errorf("maintain would lose %v", losses)
			}
			got := agent.data
			if got.Target != "twilio" || got.Reason.Provider != data.Reason.Provider || got.Reason.Model != data.Reason.Model || got.Listen != data.Listen || got.Speak != data.Speak ||
				got.Transport != targetcap.TwilioTransport || got.Carrier != "twilio" || len(got.AllChannels()) != 1 {
				t.Errorf("read back %+v\nwrote %+v", got, data)
			}
		})
	}
}

// Switching a twilio think provider loads that vendor's measured binding: the
// other vendor's model and params would reach a request that cannot take them.
// Picking the brand already bound keeps the binding, alias included.
func TestTwilioThinkProviderSwitchLoadsTheStarter(t *testing.T) {
	edit := func(start scaffold.Binding, script string) scaffold.Binding {
		t.Helper()
		runner := newRunner(strings.NewReader(script), &bytes.Buffer{}, true)
		if err := editBindingFor(runner, string(targetcap.Twilio), targetcap.Reason, &start); err != nil {
			t.Fatal(err)
		}
		return start
	}
	// Provider, then 1 google or 2 openai, then Back.
	if got := edit(scaffold.TwilioReasonStarter("openai"), "1\n1\n4\n"); got != scaffold.TwilioReasonStarter("google") {
		t.Errorf("openai -> google gave %+v", got)
	}
	if got := edit(scaffold.TwilioReasonStarter("google"), "1\n2\n4\n"); got != scaffold.TwilioReasonStarter("openai") {
		t.Errorf("google -> openai gave %+v", got)
	}
	mine := scaffold.Binding{Provider: "gemini", Model: "gemini-custom", Params: "vertexai: true\nlocation: eu"}
	if got := edit(mine, "1\n1\n4\n"); got != mine {
		t.Errorf("re-picking google changed %+v to %+v", mine, got)
	}
}

// The console offers sections a twilio package cannot carry. It refuses them at
// preflight, with the target's reason, rather than writing a package that does
// not compile.
func TestTwilioPreflightRefusesWhatTheTargetDoesNot(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*scaffold.Data)
		want string
	}{
		{"a task", func(d *scaffold.Data) {
			d.Tasks = []scaffold.Task{{Name: "collect", Instructions: "Ask for the day.", Agent: "assistant", When: "the caller asks about a day"}}
		}, "task"},
		{"a second agent", func(d *scaffold.Data) {
			d.Agents = []scaffold.Agent{{Name: "second", Instructions: "Help.", Reason: d.Reason, Speak: d.Speak}}
			d.Handoffs = []scaffold.Handoff{{Name: "to_second", Source: "assistant", To: "second"}}
		}, "agent"},
		{"a browser channel", func(d *scaffold.Data) {
			d.Channels = append(d.Channels, scaffold.Channel{Name: "web", Kind: "realtime_audio"})
		}, "channel"},
		{"tracing", func(d *scaffold.Data) { d.Tracing = &spec.Tracing{Provider: "langfuse"} }, "tracing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := twilioStarter()
			tc.edit(&data)
			_, err := scaffold.Preflight(data)
			if err == nil || !strings.Contains(err.Error(), "twilio") || !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Fatalf("preflight err = %v, want a twilio refusal naming %q", err, tc.want)
			}
		})
	}
}
