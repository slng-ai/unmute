package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
)

// TestEndpointingDelayReachesBothDrivers is the gate on the knob the framework's
// own warning asks for: a transcriber slower than the default lands its final
// text after the turn is committed, and the agent answers half a sentence
// (B: fragmented STT, 2026-08-20). Each driver has one real place to put it.
func TestEndpointingDelayReachesBothDrivers(t *testing.T) {
	// The drivers read the resolved target binding, so the delay is set there:
	// safe_core is the one fixture that declares both code targets.
	load := func(t *testing.T, delay ir.Duration) *ir.Agent {
		t.Helper()
		pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
		if err != nil {
			t.Fatal(err)
		}
		agent, err := ir.Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		if delay == "" {
			return agent
		}
		for name, resolved := range agent.Targets {
			if resolved.Models.Turn == nil {
				t.Fatalf("target %q has no turn binding to configure", name)
			}
			turn := *resolved.Models.Turn
			turn.EndpointingDelay = delay
			resolved.Models.Turn = &turn
			agent.Targets[name] = resolved
		}
		return agent
	}

	for _, tc := range []struct {
		provider ir.Provider
		file     string
		set      string
		unset    string
	}{
		{
			provider: ir.ProviderLiveKit, file: "agent.py",
			set:   `endpointing={"min_delay": 1.5},`,
			unset: "endpointing={",
		},
		{
			provider: ir.ProviderPipecat, file: "bot.py",
			set:   "vad_analyzer=SileroVADAnalyzer(params=VADParams(stop_secs=1.5)),",
			unset: "VADParams",
		},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			with := generatedFile(t, load(t, "1500ms"), tc.provider, tc.file)
			if !strings.Contains(with, tc.set) {
				t.Errorf("emitted %s does not carry the authored delay: want %s", tc.file, tc.set)
			}
			// Unset must leave the runtime default alone rather than restate it.
			without := generatedFile(t, load(t, ""), tc.provider, tc.file)
			if strings.Contains(without, tc.unset) {
				t.Errorf("emitted %s mentions %q with no delay authored", tc.file, tc.unset)
			}
		})
	}
}
