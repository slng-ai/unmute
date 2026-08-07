//go:build smoke

package generate

import (
	"fmt"
	"testing"

	"github.com/slng/unmute/internal/ir"
	targetcap "github.com/slng/unmute/internal/target"
)

// The runtime half of N19. The catalogue's generation slots were read off
// vendor docs and plugin sources by hand, and nothing so far has proven a
// single one of those kwarg names is real: a wrong name emits Python that
// validates, compiles and formats, then raises TypeError on the first call.
// Rime alone spells it two ways (speedAlpha on Pipecat, speed_alpha on
// LiveKit), sarvam calls it pace and inworld speaking_rate.
//
// These two tests bind every probed entry into one package per framework, so
// one venv per framework constructs them all against the real pipecat-ai and
// livekit-agents packages. A rejected kwarg fails here. When it does, the
// catalogue entry is what is wrong, not the probe.

// TestSmokePipecatV1GenerationSlotsInstantiate constructs every Pipecat entry
// that declares a generation slot with that slot filled (V36, L4). The emitted
// builders run against real pipecat-ai, so a Settings field the service does
// not have fails here.
func TestSmokePipecatV1GenerationSlotsInstantiate(t *testing.T) {
	mutateAgent, mutateTarget := generationProbeMutators(t, targetcap.Pipecat)
	runPipecatSmokeScript(t, "safe_core", mutateTarget, mutateAgent, smokeCheckScript)
}

// TestSmokeLiveKitV1GenerationSlotsInstantiate is the LiveKit twin. Only
// per-agent llm=/tts= constructors run at import (session-level ones need a
// JobContext), which is exactly why each probe gets its own agent.
func TestSmokeLiveKitV1GenerationSlotsInstantiate(t *testing.T) {
	mutateAgent, mutateTarget := generationProbeMutators(t, targetcap.LiveKit)
	runLiveKitSmokeScript(t, "safe_core", mutateTarget, mutateAgent, livekitSmokeScript)
}

// generationProbeMutators turns the framework's probe list into the pair of
// callbacks the smoke runners take: one adds a throwaway agent per probe, the
// other binds that agent's speak and reason profiles. The agents are
// unreachable by design — they exist so the driver emits one constructor per
// probe, and nothing else in the package refers to them.
func generationProbeMutators(t *testing.T, framework targetcap.Provider) (func(*ir.Agent), func(*ir.Target)) {
	t.Helper()
	probes := generationProbes[framework]
	if len(probes) == 0 {
		t.Fatalf("no generation probes declared for %s", framework)
	}

	mutateAgent := func(agent *ir.Agent) {
		base := agent.Agents[agent.EntryAgent]
		for i, probe := range probes {
			def := base
			def.Tools = nil // a probe agent constructs services; it never runs a turn
			if probe.speak.Provider != "" {
				def.Voice = probeProfile(i, targetcap.Speak)
			}
			if probe.reason.Provider != "" {
				def.Model = probeProfile(i, targetcap.Reason)
			}
			agent.Agents[fmt.Sprintf("probe_%d", i)] = def
		}
	}

	mutateTarget := func(tgt *ir.Target) {
		for i, probe := range probes {
			if probe.speak.Provider != "" {
				tgt.Models.Speak[probeProfile(i, targetcap.Speak)] = probeBinding(t, framework, targetcap.Speak, probe.speak)
			}
			if probe.reason.Provider != "" {
				tgt.Models.Reason[probeProfile(i, targetcap.Reason)] = probeBinding(t, framework, targetcap.Reason, probe.reason)
			}
		}
	}
	return mutateAgent, mutateTarget
}

func probeProfile(i int, role targetcap.Role) string {
	return fmt.Sprintf("probe_%d_%s", i, role)
}

// probeBinding fills the entry's declared slots, marking them as compiler-folded
// so they lower through the catalogue instead of forwarding verbatim (N19). The
// values come from the entry itself, so a slot added later is probed with no
// edit here; the author's own params, where a probe sets any, are left alone.
func probeBinding(t *testing.T, framework targetcap.Provider, role targetcap.Role, binding ir.Binding) ir.Binding {
	t.Helper()
	entry, ok := defaultCatalog.Lookup(framework, role, binding.Provider)
	if !ok {
		t.Fatalf("%s %s provider %q is not catalogued", framework, role, binding.Provider)
	}
	params := map[string]any{}
	for name, value := range binding.Params {
		params[name] = value
	}
	generation := map[string]any{}
	for name := range entry.Call.ParamSlots() {
		value, ok := generationProbeValues[name]
		if !ok {
			t.Fatalf("generation param %q has no probe value", name)
		}
		params[name], generation[name] = value, value
	}
	binding.Params, binding.Generation = params, generation
	return binding
}
