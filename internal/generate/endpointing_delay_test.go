package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
)

// TestEndpointingDelayReachesBothDrivers is the gate on one authored field
// meaning one thing on both code drivers: the window of silence that has to pass
// before the runtime treats the caller as finished.
//
// It used to assert `endpointing={"min_delay": X}` on LiveKit, which was wrong in
// a way no test could see. LiveKit's endpointing min_delay cannot fire before the
// VAD reports end of speech, and Silero's default window is 0.55s, so every
// authored value under that was silently inert. Measured live 2026-08-21, the
// user_turn floor sat at 0.577s on every run regardless of what was authored.
// Pipecat's VADParams(stop_secs=X) was always the real window, so the same YAML
// worked on one driver and did nothing on the other.
//
// The window lives in one place on LiveKit: the prewarmed Silero VAD.
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
			set:   "silero.VAD.load(min_silence_duration=1.5)",
			unset: "min_silence_duration",
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
				t.Errorf("emitted %s does not carry the authored window: want %s", tc.file, tc.set)
			}
			// Unset must leave the runtime default alone rather than restate it.
			without := generatedFile(t, load(t, ""), tc.provider, tc.file)
			if strings.Contains(without, tc.unset) {
				t.Errorf("emitted %s mentions %q with no delay authored", tc.file, tc.unset)
			}
		})
	}
}

// TestEndpointingDelayLeavesNoLiveKitEndpointingKey pins the other half of the
// fix. Emitting `endpointing={"min_delay": X}` alongside the VAD window would
// look like belt and braces and is worse than that: the key reads as though it
// were doing the work, which is what sent a whole measurement effort after the
// wrong knob. Nothing should set it until something is measured to need it.
func TestEndpointingDelayLeavesNoLiveKitEndpointingKey(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for name, resolved := range agent.Targets {
		if resolved.Models.Turn == nil {
			continue
		}
		turn := *resolved.Models.Turn
		turn.EndpointingDelay = "1500ms"
		resolved.Models.Turn = &turn
		agent.Targets[name] = resolved
	}
	got := generatedFile(t, agent, ir.ProviderLiveKit, "agent.py")
	if strings.Contains(got, "endpointing=") {
		t.Error("emitted agent.py sets a turn_handling endpointing key: min_delay cannot fire before the VAD window, so it does nothing but mislead")
	}
}

// TestEndpointingDelayUnsetEmitsBareVADLoad is the no-drift guarantee. A package
// that authors nothing must keep the runtime's own default, which means a bare
// silero.VAD.load() and no argument restating 0.55s.
func TestEndpointingDelayUnsetEmitsBareVADLoad(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	got := generatedFile(t, agent, ir.ProviderLiveKit, "agent.py")
	if !strings.Contains(got, `proc.userdata["vad"] = silero.VAD.load()`) {
		t.Error("emitted agent.py does not prewarm a bare silero.VAD.load() with no window authored")
	}
}
