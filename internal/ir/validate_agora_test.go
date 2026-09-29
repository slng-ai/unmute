package ir

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

func agoraExample(t *testing.T) (*Agent, Target) {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "agora-voice"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent, agent.Targets["agora"]
}

func TestAgoraExampleValidates(t *testing.T) {
	agent, tgt := agoraExample(t)
	report, err := Validate(agent, []Target{tgt}, targetcap.Default())
	if err != nil {
		t.Fatalf("%v: %+v", err, report)
	}
}

func TestAgoraRejectsUnsupportedFeatures(t *testing.T) {
	cases := []struct {
		name, want string
		mutate     func(*Agent, *Target)
	}{
		{"tools", "tools", func(a *Agent, _ *Target) { a.Tools = map[string]Tool{"x": {Execution: ToolMCP}} }},
		{"tasks", "tasks", func(a *Agent, _ *Target) { a.Tasks = map[string]Task{"x": {}} }},
		{"variables", "variables", func(a *Agent, _ *Target) { a.Variables = map[string]Variable{"x": {}} }},
		{"phone", "channels.web", func(a *Agent, _ *Target) { a.Channels["web"] = Channel{Kind: ChannelTelephony} }},
		{"turn", "turn", func(a *Agent, _ *Target) { a.Turn = "detector" }},
		{"local", "placement", func(_ *Agent, t *Target) { t.Models.Listen.Placement = PlacementLocal }},
		{"params", "params", func(_ *Agent, t *Target) { t.Models.Listen.Params = map[string]any{"test": true} }},
		{"capacity", "capacity", func(a *Agent, _ *Target) { a.Capacity = &Capacity{MaxSessions: 1} }},
		{"region", "deployment_regions", func(_ *Agent, t *Target) { t.DeploymentRegions = []string{"us"} }},
		{"duration", "max_duration", func(a *Agent, _ *Target) { a.Conversation.MaxDuration = "601s" }},
		{"fine interruption", "minimum_words", func(a *Agent, _ *Target) { a.Conversation.Interruption.MinimumWords = 2 }},
		{"vendor", "provider deepgram", func(_ *Agent, t *Target) { t.Models.Listen.Provider = "unknown" }},
		{"model", "model nova-3", func(_ *Agent, t *Target) { t.Models.Listen.Model = "unknown" }},
		{"pins", "pins", func(_ *Agent, t *Target) { t.Pins = map[string]string{"agora-agents": "9.0.0"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent, tgt := agoraExample(t)
			tc.mutate(agent, &tgt)
			report, err := Validate(agent, []Target{tgt}, targetcap.Default())
			if err == nil {
				t.Fatal("unsupported feature validated")
			}
			var errors []string
			for _, row := range report.PerTarget {
				errors = append(errors, row.Errors...)
			}
			if !strings.Contains(strings.Join(errors, "\n"), tc.want) {
				t.Fatalf("missing %q: %+v", tc.want, report)
			}
		})
	}
}
