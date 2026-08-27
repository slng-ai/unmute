package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
)

// This file holds the whole turn-timing contract on both code drivers: the floor
// (how long silence has to last before the caller counts as finished) and the
// ceiling (how long the runtime waits before closing the turn regardless).
//
// It changed direction on 2026-08-27, and the history matters because the
// earlier direction was right on the evidence it had.
//
// The first version asserted `endpointing={"min_delay": X}` on LiveKit. That was
// wrong: min_delay cannot fire before the VAD reports end of speech, and Silero's
// default window is 0.55s, so every authored value under that was inert.
// Measured live 2026-08-21 the user_turn floor sat at 0.577s regardless of what
// was authored. The fix moved the window to the prewarmed VAD and this file grew
// a test forbidding the endpointing key outright, whose comment said "nothing
// should set it until something is measured to need it".
//
// Something was then measured to need it. On a live LiveKit call two turns spent
// 2.5s in endpointing alone, which is `max_delay` — a ceiling in the same dict,
// which the VAD window has no say in at all. The old reasoning covered min_delay
// and was silently generalised to the whole key. So the forbidding test is gone
// and this one asserts the opposite, on its own evidence.
//
// The floor and the ceiling are now separate things with separate owners:
// endpointing_delay sets the floor, pace sets the ceiling.

// loadTurnFixture returns safe_core, the one fixture declaring both code
// targets, with the turn binding configured.
func loadTurnFixture(t *testing.T, delay ir.Duration, pace ir.Pace) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if delay == "" && pace == "" {
		return agent
	}
	// The pace lives on the base model, not on a target override: Validate
	// refuses a per-target pace, so a fixture that set only the resolved binding
	// would be testing the refusal rather than the feature.
	if pace != "" && agent.Turn != "" {
		base := agent.Models[agent.Turn]
		base.Pace = pace
		agent.Models[agent.Turn] = base
	}
	for name, resolved := range agent.Targets {
		if resolved.Models.Turn == nil {
			t.Fatalf("target %q has no turn binding to configure", name)
		}
		turn := *resolved.Models.Turn
		if delay != "" {
			turn.EndpointingDelay = delay
		}
		if pace != "" {
			turn.Pace = pace
		}
		resolved.Models.Turn = &turn
		agent.Targets[name] = resolved
	}
	return agent
}

// TestEndpointingDelayReachesTheFloorOnBothDrivers is the original gate, intact:
// one authored duration means one thing on both drivers, and it is the silence
// window, not anything above it.
func TestEndpointingDelayReachesTheFloorOnBothDrivers(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		file     string
		want     string
	}{
		{ir.ProviderLiveKit, "agent.py", "silero.VAD.load(min_silence_duration=1.5)"},
		{ir.ProviderPipecat, "bot.py", "vad_analyzer=SileroVADAnalyzer(params=VADParams(stop_secs=1.5)),"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			got := generatedFile(t, loadTurnFixture(t, "1500ms", ""), tc.provider, tc.file)
			if !strings.Contains(got, tc.want) {
				t.Errorf("emitted %s does not carry the authored window: want %s", tc.file, tc.want)
			}
		})
	}
}

// TestAuthoredDelayWinsTheFloorAndThePaceStillMovesTheCeiling is FR-004 read
// exactly. The promise is that an authored duration is never overridden — not
// that the output is unchanged. Asserting the latter would lock in the opposite
// of FR-015, so both halves are asserted together here.
func TestAuthoredDelayWinsTheFloorAndThePaceStillMovesTheCeiling(t *testing.T) {
	for _, tc := range []struct {
		provider    ir.Provider
		file        string
		wantFloor   string
		wantCeiling string
	}{
		{
			provider: ir.ProviderLiveKit, file: "agent.py",
			// 1.5s authored, snappy's floor is 0.25: the authored value survives.
			wantFloor:   "silero.VAD.load(min_silence_duration=1.5)",
			wantCeiling: `"max_delay": 1.2`,
		},
		{
			provider: ir.ProviderPipecat, file: "bot.py",
			wantFloor:   "VADParams(stop_secs=1.5)",
			wantCeiling: "SmartTurnParams(stop_secs=1.2)",
		},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			got := generatedFile(t, loadTurnFixture(t, "1500ms", ir.PaceSnappy), tc.provider, tc.file)
			if !strings.Contains(got, tc.wantFloor) {
				t.Errorf("the authored floor did not survive the pace: want %s", tc.wantFloor)
			}
			if !strings.Contains(got, tc.wantCeiling) {
				t.Errorf("the pace did not reach the ceiling: want %s", tc.wantCeiling)
			}
		})
	}
}

// TestPaceReachesTheCeilingOnBothDrivers is the core of User Story 1. The two
// targets spell the ceiling differently, which is the reason a portable word
// exists rather than a duration.
func TestPaceReachesTheCeilingOnBothDrivers(t *testing.T) {
	for _, tc := range []struct {
		pace        ir.Pace
		livekit     []string
		pipecat     string
		notLiveKit  string
		description string
	}{
		{
			pace:        ir.PaceSnappy,
			livekit:     []string{`"mode": "dynamic"`, `"min_delay": 0.25`, `"max_delay": 1.2`},
			pipecat:     "SmartTurnParams(stop_secs=1.2)",
			notLiveKit:  "2.5",
			description: "snappy",
		},
		{
			pace:        ir.PaceBalanced,
			livekit:     []string{`"mode": "dynamic"`, `"min_delay": 0.3`, `"max_delay": 1.6`},
			pipecat:     "SmartTurnParams(stop_secs=1.6)",
			description: "balanced",
		},
		{
			pace: ir.PacePatient,
			// patient reproduces the pinned framework defaults, so selecting it
			// changes nothing for a package that already has today's behaviour.
			livekit:     []string{`"mode": "fixed"`, `"min_delay": 0.5`, `"max_delay": 2.5`},
			pipecat:     "SmartTurnParams(stop_secs=3)",
			description: "patient",
		},
	} {
		t.Run(tc.description, func(t *testing.T) {
			agentPy := generatedFile(t, loadTurnFixture(t, "", tc.pace), ir.ProviderLiveKit, "agent.py")
			for _, want := range tc.livekit {
				if !strings.Contains(agentPy, want) {
					t.Errorf("emitted agent.py does not carry %s for pace %s", want, tc.pace)
				}
			}
			if tc.notLiveKit != "" && strings.Contains(agentPy, `"max_delay": `+tc.notLiveKit) {
				t.Errorf("emitted agent.py still carries max_delay %s for pace %s", tc.notLiveKit, tc.pace)
			}
			botPy := generatedFile(t, loadTurnFixture(t, "", tc.pace), ir.ProviderPipecat, "bot.py")
			if !strings.Contains(botPy, tc.pipecat) {
				t.Errorf("emitted bot.py does not carry %s for pace %s", tc.pipecat, tc.pace)
			}
		})
	}
}

// TestUnsetPaceEmitsTheBalancedRowRatherThanNoKey is FR-015, and it is the
// reason golden files move on both targets in this change. A package that says
// nothing gets the faster behaviour, which means the keys are present and carry
// the balanced numbers rather than being absent and inheriting a slower default
// nobody chose.
func TestUnsetPaceEmitsTheBalancedRowRatherThanNoKey(t *testing.T) {
	agentPy := generatedFile(t, loadTurnFixture(t, "", ""), ir.ProviderLiveKit, "agent.py")
	for _, want := range []string{
		`endpointing={"mode": "dynamic", "min_delay": 0.3, "max_delay": 1.6}`,
		`silero.VAD.load(min_silence_duration=0.3)`,
	} {
		if !strings.Contains(agentPy, want) {
			t.Errorf("emitted agent.py does not carry %s with nothing authored", want)
		}
	}
	// The bare load is what the previous contract required. It is gone on
	// purpose: 0.55s was a default nobody chose and the turn detector wants less.
	if strings.Contains(agentPy, `silero.VAD.load()`) {
		t.Error("emitted agent.py prewarms a bare silero.VAD.load(), which inherits Silero's 0.55s rather than the balanced floor")
	}

	botPy := generatedFile(t, loadTurnFixture(t, "", ""), ir.ProviderPipecat, "bot.py")
	for _, want := range []string{
		"VADParams(stop_secs=0.2)",
		"SmartTurnParams(stop_secs=1.6)",
	} {
		if !strings.Contains(botPy, want) {
			t.Errorf("emitted bot.py does not carry %s with nothing authored", want)
		}
	}
}

// TestPipecatFloorDoesNotMoveWithThePace is the emitted half of the measured
// cliff. internal/target/pace_test.go gates the table; this gates the output, so
// a template that reached for the wrong field would fail here.
//
// Above the transcript's arrival time Pipecat pays a flat 1.0s safety net and
// observed transcripts run 0.27s and up, so a wider window is slower. The pace
// reaches this target through the ceiling alone. research.md §5a.
func TestPipecatFloorDoesNotMoveWithThePace(t *testing.T) {
	for _, pace := range []ir.Pace{ir.PaceSnappy, ir.PaceBalanced, ir.PacePatient} {
		botPy := generatedFile(t, loadTurnFixture(t, "", pace), ir.ProviderPipecat, "bot.py")
		if !strings.Contains(botPy, "VADParams(stop_secs=0.2)") {
			t.Errorf("pace %s moved the Pipecat VAD window; it must stay at 0.2 for every pace (research.md §5a)", pace)
		}
	}
}

// TestPipecatNamesTheTwoStopSecsFieldsSeparately is FR-009a at the emitted
// level. Both fields are spelled stop_secs and they mean different things: one is
// the VAD's silence window and one is the analyzer's ceiling. If a future edit
// crossed them, every other assertion here would still pass.
func TestPipecatNamesTheTwoStopSecsFieldsSeparately(t *testing.T) {
	botPy := generatedFile(t, loadTurnFixture(t, "400ms", ir.PaceBalanced), ir.ProviderPipecat, "bot.py")
	if !strings.Contains(botPy, "VADParams(stop_secs=0.4)") {
		t.Error("the VAD's stop_secs did not take the authored duration")
	}
	if !strings.Contains(botPy, "SmartTurnParams(stop_secs=1.6)") {
		t.Error("the analyzer's stop_secs did not take the pace's ceiling")
	}
	if strings.Contains(botPy, "SmartTurnParams(stop_secs=0.4)") {
		t.Error("the authored silence window reached the analyzer's ceiling; the two stop_secs fields have been crossed")
	}
}

// TestLiveKitEmitsEndpointingOnEveryBranch guards the shape of the template
// rather than one rendering of it. TurnHandlingOptions is constructed at two
// sites, one per interruption branch, and a key added to one of them only would
// pass every test above on whichever fixture happened to take that branch.
func TestLiveKitEmitsEndpointingOnEveryBranch(t *testing.T) {
	agentPy := generatedFile(t, loadTurnFixture(t, "", ir.PaceBalanced), ir.ProviderLiveKit, "agent.py")
	handling := strings.Count(agentPy, "turn_handling=TurnHandlingOptions(")
	endpointing := strings.Count(agentPy, "endpointing={")
	if handling == 0 {
		t.Fatal("emitted agent.py constructs no TurnHandlingOptions")
	}
	if endpointing != handling {
		t.Errorf("emitted agent.py builds %d TurnHandlingOptions but sets endpointing %d times; every branch has to carry it", handling, endpointing)
	}
}

// TestPipecatEmitsTheAnalyzerOnEveryAggregator is the same guard for Pipecat,
// which builds LLMUserAggregatorParams at two sites.
func TestPipecatEmitsTheAnalyzerOnEveryAggregator(t *testing.T) {
	botPy := generatedFile(t, loadTurnFixture(t, "", ir.PaceBalanced), ir.ProviderPipecat, "bot.py")
	aggregators := strings.Count(botPy, "vad_analyzer=SileroVADAnalyzer(")
	analyzers := strings.Count(botPy, "LocalSmartTurnAnalyzerV3(")
	if aggregators == 0 {
		t.Fatal("emitted bot.py constructs no user aggregator")
	}
	if analyzers != aggregators {
		t.Errorf("emitted bot.py builds %d aggregators but constructs %d analyzers; every one has to carry the ceiling", aggregators, analyzers)
	}
}
