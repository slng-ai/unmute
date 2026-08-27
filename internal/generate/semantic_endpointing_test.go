package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
)

// `semantic_endpointing` was validated, reported, and documented on four
// docs-site pages, three skill reference files and the agent-yaml table, and it
// reached nothing in any emitted project. Both drivers marked it advisory and no
// template read it.
//
// A field that looks authored and changes no output is what Principle II
// forbids, and it mattered here because `pace` lands right beside it: adding a
// second field naming the same subject while the first stayed dead would put two
// owners on one surface with only one of them working.
//
// These are the gates that make it load-bearing.

func semanticFixture(t *testing.T, value ir.SemanticEndpointing) *ir.Agent {
	t.Helper()
	agent := loadTurnFixture(t, "", "")
	if agent.Turn != "" {
		base := agent.Models[agent.Turn]
		base.SemanticEndpointing = value
		agent.Models[agent.Turn] = base
	}
	for name, resolved := range agent.Targets {
		if resolved.Models.Turn == nil {
			t.Fatalf("target %q has no turn binding", name)
		}
		turn := *resolved.Models.Turn
		turn.SemanticEndpointing = value
		resolved.Models.Turn = &turn
		agent.Targets[name] = resolved
	}
	return agent
}

// TestSemanticEndpointingOffRemovesTheModelOnBothTargets. The two targets spell
// "no turn model" differently, which is the whole reason one authored word maps
// per target rather than being forwarded.
func TestSemanticEndpointingOffRemovesTheModelOnBothTargets(t *testing.T) {
	agentPy := generatedFile(t, semanticFixture(t, ir.SemanticEndpointingOff), ir.ProviderLiveKit, "agent.py")
	if !strings.Contains(agentPy, `turn_detection="vad"`) {
		t.Error("off did not reach LiveKit: expected turn_detection=\"vad\"")
	}
	if strings.Contains(agentPy, "inference.TurnDetector(") {
		t.Error("off left the LiveKit turn detector in place")
	}
	// The endpointing dict is independent of the detector, so the ceiling stays.
	if !strings.Contains(agentPy, `"max_delay": 1.6`) {
		t.Error("off cost LiveKit its ceiling; the endpointing dict does not depend on the detector")
	}

	botPy := generatedFile(t, semanticFixture(t, ir.SemanticEndpointingOff), ir.ProviderPipecat, "bot.py")
	if !strings.Contains(botPy, "SpeechTimeoutUserTurnStopStrategy()") {
		t.Error("off did not reach Pipecat: expected the speech-timeout stop strategy")
	}
	if strings.Contains(botPy, "LocalSmartTurnAnalyzerV3") {
		t.Error("off left the Pipecat analyzer in place")
	}
	// The import has to follow the branch or ruff flags an unused name.
	if strings.Contains(botPy, "import SmartTurnParams") || strings.Contains(botPy, "import LocalSmartTurnAnalyzerV3") {
		t.Error("off left an analyzer import behind, which ruff reads as unused")
	}
}

// TestSemanticEndpointingPreferredAndRequiredKeepTheModel. Only `off` changes
// anything: every shipped code target can provide a semantic model, so there is
// nothing for `required` to assert that is not already true.
func TestSemanticEndpointingPreferredAndRequiredKeepTheModel(t *testing.T) {
	for _, value := range []ir.SemanticEndpointing{
		ir.SemanticEndpointingPreferred,
		ir.SemanticEndpointingRequired,
		"", // unset
	} {
		name := string(value)
		if name == "" {
			name = "unset"
		}
		t.Run(name, func(t *testing.T) {
			agentPy := generatedFile(t, semanticFixture(t, value), ir.ProviderLiveKit, "agent.py")
			if !strings.Contains(agentPy, "inference.TurnDetector(") {
				t.Error("LiveKit lost its turn detector")
			}
			if strings.Contains(agentPy, `turn_detection="vad"`) {
				t.Error("LiveKit fell back to VAD-only turn detection")
			}
			botPy := generatedFile(t, semanticFixture(t, value), ir.ProviderPipecat, "bot.py")
			if !strings.Contains(botPy, "LocalSmartTurnAnalyzerV3") {
				t.Error("Pipecat lost its end-of-turn analyzer")
			}
			if strings.Contains(botPy, "SpeechTimeoutUserTurnStopStrategy") {
				t.Error("Pipecat fell back to the speech timeout")
			}
		})
	}
}

// TestPipecatEmitsNoSpeechTimeoutUnlessAsked is the named regression guard for
// the defect this change removes, and it is the one test here that would have
// failed before it.
//
// The `interruption.minimum_words` branch used to pass
// `stop=[SpeechTimeoutUserTurnStopStrategy()]`, which replaced Pipecat's own
// end-of-turn analyzer with a plain silence timer. Nothing said so: not the
// package, not the compile report, not the emitted runbook. A package asking for
// a minimum interruption word count silently got worse turn taking.
//
// The timer is still reachable, but only by asking for it with
// `semantic_endpointing: off`. That is the difference between a choice and a
// downgrade.
func TestPipecatEmitsNoSpeechTimeoutUnlessAsked(t *testing.T) {
	agent := loadTurnFixture(t, "", ir.PaceBalanced)
	// The word count is what used to trigger the downgrade. `enabled` is
	// required alongside it, which is why it is set here too.
	if agent.Conversation == nil {
		agent.Conversation = &ir.Conversation{}
	}
	enabled := true
	agent.Conversation.Interruption = &ir.Interruption{Enabled: &enabled, MinimumWords: 3}

	botPy := generatedFile(t, agent, ir.ProviderPipecat, "bot.py")
	if !strings.Contains(botPy, "MinWordsUserTurnStartStrategy(min_words=3)") {
		t.Fatal("the fixture did not reach the minimum-words branch, so this test proves nothing")
	}
	if strings.Contains(botPy, "SpeechTimeoutUserTurnStopStrategy") {
		t.Error("the minimum-words branch emits a speech timeout again: it silently replaces the end-of-turn analyzer with a weaker timer")
	}
	if !strings.Contains(botPy, "LocalSmartTurnAnalyzerV3") {
		t.Error("the minimum-words branch lost the end-of-turn analyzer")
	}
	if !strings.Contains(botPy, "SmartTurnParams(stop_secs=1.6)") {
		t.Error("the minimum-words branch lost the pace's ceiling")
	}
}

// TestTurnFieldsSurviveAPerTargetOverride is the gate on a bug this feature
// found twice, in two fields, for the same reason.
//
// A per-target `models:` override replaces the whole ModelDef rather than merging
// into it, so any field the override does not restate is dropped.
// `endpointing_delay` and `prompt_suffix` already had explicit carry-forwards in
// resolveBindings for exactly this. `pace` and `semantic_endpointing` did not.
//
// `semantic_endpointing`'s version was latent: the field reached no emitted
// project, so an override silently dropping it changed nothing observable. It
// surfaced the moment the field became load-bearing, on the flagship example,
// which overrides its turn binding on both targets — so `off` on the base binding
// reached neither.
//
// This runs against examples/salon-concierge deliberately. safe_core has no turn
// override, so a test that configures the resolved binding directly skips the
// merge and cannot see this class of bug at all.
func TestTurnFieldsSurviveAPerTargetOverride(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
	if err != nil {
		t.Fatal(err)
	}
	// Set both fields on the base turn binding, as an author would.
	for name, def := range pkg.Agent.Models.Turn {
		def.Pace = string(ir.PaceSnappy)
		def.SemanticEndpointing = string(ir.SemanticEndpointingOff)
		pkg.Agent.Models.Turn[name] = def
	}

	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	overridden := 0
	for name, resolved := range agent.Targets {
		if resolved.Models.Turn == nil {
			continue
		}
		overridden++
		if got := resolved.Models.Turn.Pace; got != ir.PaceSnappy {
			t.Errorf("target %q resolved pace %q, want %q: a per-target override dropped it", name, got, ir.PaceSnappy)
		}
		if got := resolved.Models.Turn.SemanticEndpointing; got != ir.SemanticEndpointingOff {
			t.Errorf("target %q resolved semantic_endpointing %q, want %q: a per-target override dropped it", name, got, ir.SemanticEndpointingOff)
		}
		// The one that already had a carry-forward, asserted here too so all
		// three live in one place.
		if resolved.Models.Turn.EndpointingDelay == "" {
			t.Errorf("target %q lost its endpointing_delay", name)
		}
	}
	if overridden == 0 {
		t.Fatal("the salon example has no resolved turn binding, so this test proves nothing")
	}
}
