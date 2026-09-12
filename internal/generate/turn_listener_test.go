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
	// Read from where the settings block opens to where it closes, because the
	// mistake this guards against renders the flag *last* among the settings:
	// matching the first settings line could never have caught it.
	settings := bot[strings.Index(bot, "settings=DeepgramFluxSTTService.Settings("):]
	if end := strings.Index(settings, "\n        )"); end > 0 {
		settings = settings[:end]
	}
	if strings.Contains(settings, "enable_eager_end_of_turn") {
		t.Errorf("enable_eager_end_of_turn landed inside Settings; it is a constructor argument:\n%s", settings)
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
	// And it does not say the opposite two lines above. The advisory note is
	// true of the on-device pair and false here, where the binding is what
	// selected the transcriber's own turn service.
	if strings.Contains(report, "its binding is advisory") {
		t.Error("compile-report.json calls the turn binding advisory while that binding is what chose the service")
	}
	// The date beside the class is that service's own, not the ordinary
	// transcriber's: they are different rows, checked on different days.
	if !strings.Contains(report, "DeepgramFluxSTTService (pipecat-ai[deepgram], verified 2026-09-12)") {
		t.Errorf("compile-report.json does not carry the turn service's own verification date:\n%s", report)
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

// TestGradiumDecidesTheTurnThroughItsOwnClass is the second switch: the vendor
// keeps its ordinary class and takes one flat constructor argument. Nothing is
// swapped, so the mistake this guards against is a compiler that emits the
// argument on a class it also renamed, or renames nothing and emits no argument
// at all, which reads as a working package whose turns never change hands.
func TestGradiumDecidesTheTurnThroughItsOwnClass(t *testing.T) {
	artifact := generateFor(t, "turn_listener_gradium", ir.ProviderPipecat)
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"from pipecat.services.gradium.stt import GradiumSTTService",
		"return GradiumSTTService(",
		"enable_turn_detection=True,",
		"vad_analyzer=SileroVADAnalyzer(),",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py is missing %q", want)
		}
	}
	for _, absent := range []string{
		// No second class, and none of the local pair.
		"GradiumTurnsSTTService", "LocalSmartTurnAnalyzerV3", "SmartTurnParams", "VADParams",
		"user_turn_strategies=", "EagerUserTurnStrategies", "enable_eager_end_of_turn",
		// And no ceiling: the service exposes no end-of-turn timeout, so a
		// keyword here would raise at construction on the first call.
		"eot_timeout_ms", "turn_end_timeout_ms", "eot_horizon_s",
	} {
		if strings.Contains(bot, absent) {
			t.Errorf("bot.py carries %q, which gradium's turn detection neither needs nor has", absent)
		}
	}
	// The flat argument is a constructor argument, not a setting, the same way
	// the eager flag is. Read the settings block rather than the first line,
	// because a misplacement renders it among the settings and a prefix match
	// would pass.
	settings := bot[strings.Index(bot, "settings=GradiumSTTService.Settings("):]
	if end := strings.Index(settings, "\n        )"); end > 0 {
		settings = settings[:end]
	}
	if strings.Contains(settings, "enable_turn_detection") {
		t.Errorf("enable_turn_detection landed inside Settings; it is a constructor argument:\n%s", settings)
	}
	// The emitted comment cannot name a ceiling field this vendor does not have.
	// Before the template branched, it rendered "()=1600" and named no class.
	if strings.Contains(bot, "=1600, from pace:") {
		t.Error("bot.py claims a pace ceiling landed somewhere on a vendor with no end-of-turn timeout")
	}
	if !strings.Contains(bot, "It exposes no end-of-turn timeout") {
		t.Error("bot.py does not say why no ceiling was written")
	}
	// The report says who decides without crediting a ceiling nobody took.
	report := artifactFile(t, artifact, "compile-report.json")
	if !strings.Contains(report, "turn gradium decides (its own end-of-turn timing, no ceiling of ours") {
		t.Errorf("compile-report.json does not say gradium decides on its own timing:\n%s", report)
	}
	if strings.Contains(report, "closes at") {
		t.Error("compile-report.json names a closing time on a vendor that takes no ceiling")
	}
}

// TestSpeechmaticsTurnModeFollowsTheBinding is the third switch and the one that
// matters most on this bump.
//
// turn_detection_mode is not new; its DEFAULT flipped in pipecat 1.10.0, from
// EXTERNAL (the caller drives turns, meaning Pipecat's own detector) to VAD (the
// service closes turns itself). So the mode is written in both directions:
// without the local half, a package that binds this vendor and says nothing
// about turns changes who ends the caller's turn on a version bump.
func TestSpeechmaticsTurnModeFollowsTheBinding(t *testing.T) {
	local := artifactFile(t, generateFor(t, "speechmatics_local", ir.ProviderPipecat), "bot.py")
	listen := artifactFile(t, generateFor(t, "speechmatics_listen", ir.ProviderPipecat), "bot.py")

	// Same class both ways: this vendor is switched by a setting, not a swap.
	for name, bot := range map[string]string{"local": local, "listen": listen} {
		if !strings.Contains(bot, "return SpeechmaticsSTTService(") {
			t.Errorf("%s: bot.py does not build the ordinary speechmatics class", name)
		}
	}
	if !strings.Contains(local, "turn_detection_mode=SpeechmaticsSTTService.TurnDetectionMode.EXTERNAL,") {
		t.Error("the local decider does not hold speechmatics to the external mode, so the service would close turns beside pipecat's own detector")
	}
	if !strings.Contains(listen, "turn_detection_mode=SpeechmaticsSTTService.TurnDetectionMode.VAD,") {
		t.Error("the listening decider does not put speechmatics in its own turn mode")
	}
	// The local package still builds the local pair; the listening one does not.
	for _, want := range []string{"LocalSmartTurnAnalyzerV3(", "VADParams(stop_secs=", "user_turn_strategies=UserTurnStrategies("} {
		if !strings.Contains(local, want) {
			t.Errorf("the local decider lost %q, so pinning the mode changed more than the mode", want)
		}
	}
	for _, absent := range []string{"LocalSmartTurnAnalyzerV3(", "user_turn_strategies="} {
		if strings.Contains(listen, absent) {
			t.Errorf("the listening decider still carries %q", absent)
		}
	}
	// The enum is reached through the service class, so no second import is
	// needed. An import nothing adds is a NameError at worker startup.
	if strings.Contains(local, "from pipecat.services.speechmatics.stt import TurnDetectionMode") {
		t.Error("the mode is reached through the service class; a second import would be one the emitted module has to add and nothing does")
	}
	// The report tells the author which component decides, on the path where
	// they wrote nothing about it (SC-005).
	report := artifactFile(t, generateFor(t, "speechmatics_local", ir.ProviderPipecat), "compile-report.json")
	if !strings.Contains(report, "held to turn_detection_mode=SpeechmaticsSTTService.TurnDetectionMode.EXTERNAL so it does not close turns itself") {
		t.Errorf("compile-report.json does not name the setting written on the author's behalf:\n%s", report)
	}
}

// TestAVendorWithNoSwitchEmitsNothingOfOurs: every listening vendor that is NOT
// in the turn-decider table keeps the bytes it had. The table grew by two on
// this bump, and a lookup keyed too loosely would start writing a turn keyword
// into services that have none.
func TestAVendorWithNoSwitchEmitsNothingOfOurs(t *testing.T) {
	bot := artifactFile(t, generateFor(t, "simple-prompt", ir.ProviderPipecat), "bot.py")
	for _, absent := range []string{"enable_turn_detection", "turn_detection_mode"} {
		if strings.Contains(bot, absent) {
			t.Errorf("a package binding a vendor with no turn-decider row gained %q", absent)
		}
	}
}
