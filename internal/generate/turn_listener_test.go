package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// TestListenDeciderEmitsTheTranscribersTurnService reads the emitted bot for the
// turn_listener fixture: the vendor's turn-detecting class stands in for its
// transcriber, the pace ceiling lands in that service's own timeout, the eager
// flag reaches both the service and the aggregator, and nothing of the local
// pair is imported or built. Every name here is one the smoke test drives.
func TestListenDeciderEmitsTheTranscribersTurnService(t *testing.T) {
	artifact := generateFor(t, "turn_listener", ir.ProviderPipecat)
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"from pipecat.services.deepgram.flux.stt import DeepgramFluxSTTService",
		"from pipecat.turns.user_turn_strategies import EagerUserTurnStrategies",
		"return DeepgramFluxSTTService(",
		"enable_eager_end_of_turn=True,",
		"settings=DeepgramFluxSTTService.Settings(",
		"eot_timeout_ms=1200,",
		"vad_analyzer=SileroVADAnalyzer(),",
		"user_turn_strategies=EagerUserTurnStrategies(),",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py is missing %q", want)
		}
	}
	for _, absent := range []string{
		"DeepgramSTTService(", "LocalSmartTurnAnalyzerV3", "SmartTurnParams", "VADParams",
		"TurnAnalyzerUserTurnStopStrategy", "SpeechTimeoutUserTurnStopStrategy",
		"import UserTurnStrategies", "user_turn_strategies=UserTurnStrategies(",
	} {
		if strings.Contains(bot, absent) {
			t.Errorf("bot.py still carries %q, which the listening decider replaces", absent)
		}
	}
	// A flat flag, not a setting: the service reads it once at construction.
	if strings.Contains(bot, "Settings(\n            enable_eager_end_of_turn") {
		t.Error("enable_eager_end_of_turn landed inside Settings; it is a constructor argument")
	}
	// The runbook names the decider, the field the ceiling landed in, and that
	// the early answer is on (FR-015).
	readme := artifactFile(t, artifact, "README.md")
	for _, want := range []string{
		"**The transcriber decides the turn.**", "`DeepgramFluxSTTService`",
		"`eot_timeout_ms=1200`", "**The reply starts before the turn is confirmed.**",
		"`endpointing_delay`\nis refused at validate",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md is missing %q", want)
		}
	}
	if strings.Contains(readme, "Both are spelled `stop_secs`") {
		t.Error("README.md still explains the two stop_secs fields, neither of which exists under a listening decider")
	}
	// The compile report names both halves of the decision in one line.
	report := artifactFile(t, artifact, "compile-report.json")
	if !strings.Contains(report, "the transcriber decides, DeepgramFluxSTTService closes at 1.2s via eot_timeout_ms, answers the predicted turn early") {
		t.Errorf("compile-report.json does not say who decides the turn:\n%s", report)
	}
	// The extra and the key come from the ordinary listen entry.
	if pyproject := artifactFile(t, artifact, "pyproject.toml"); !strings.Contains(pyproject, "deepgram,") {
		t.Errorf("pyproject.toml must carry the deepgram extra:\n%s", pyproject)
	}
	if env := artifactFile(t, artifact, ".env.example"); !strings.Contains(env, "DEEPGRAM_API_KEY") {
		t.Errorf(".env.example must name DEEPGRAM_API_KEY:\n%s", env)
	}
}

// TestListenDeciderWithoutEagerPassesNoStrategies: with eager off the aggregator
// gets no user_turn_strategies of ours, because passing any makes the framework
// ignore the external strategies the transcriber recommends (research R3), and
// the service is built without the flag.
func TestListenDeciderWithoutEagerPassesNoStrategies(t *testing.T) {
	agent := agentFor(t, "turn_listener")
	tgt := targetByProvider(t, agent, ir.ProviderPipecat)
	turn := *tgt.Models.Turn
	turn.Eager = false
	tgt.Models.Turn = &turn
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, absent := range []string{"user_turn_strategies=", "enable_eager_end_of_turn", "EagerUserTurnStrategies"} {
		if strings.Contains(bot, absent) {
			t.Errorf("bot.py carries %q with eager off", absent)
		}
	}
	if !strings.Contains(bot, "eot_timeout_ms=1200,") {
		t.Error("the ceiling still reaches the transcriber's timeout with eager off")
	}
	readme := artifactFile(t, artifact, "README.md")
	if !strings.Contains(readme, "The reply starts when the transcriber confirms the turn.") {
		t.Error("README.md does not say the reply waits for the confirmed turn")
	}
}

// TestLocalDeciderEmitsWhatItAlwaysDid: the shape every shipped package writes
// keeps its bot.py byte for byte (the compat digests hold the whole artifact;
// this names the two things a local package must not gain).
func TestLocalDeciderEmitsWhatItAlwaysDid(t *testing.T) {
	bot := artifactFile(t, generateFor(t, "simple-prompt", ir.ProviderPipecat), "bot.py")
	for _, absent := range []string{"enable_eager_end_of_turn", "EagerUserTurnStrategies", "DeepgramFluxSTTService"} {
		if strings.Contains(bot, absent) {
			t.Errorf("a local-decider package gained %q", absent)
		}
	}
	for _, want := range []string{"VADParams(stop_secs=", "LocalSmartTurnAnalyzerV3(", "user_turn_strategies=UserTurnStrategies("} {
		if !strings.Contains(bot, want) {
			t.Errorf("a local-decider package lost %q", want)
		}
	}
}
