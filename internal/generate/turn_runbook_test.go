package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// The emitted runbook is where an author reads what their package actually does,
// and FR-009a, FR-009b and FR-010 are all prose. Prose with no gate is a wish,
// so this file is the gate.
//
// The section used to be wrapped in `{{if .EndpointingDelay}}`, which was right
// while a duration was the only thing that could move a turn. It is wrong now:
// every package gets a pace, so a package that authored nothing had two numbers
// governing its calls and a runbook that mentioned neither.

func turnSection(t *testing.T, provider ir.Provider, delay ir.Duration, pace ir.Pace) string {
	t.Helper()
	readme := generatedFile(t, loadTurnFixture(t, delay, pace), provider, "README.md")
	_, after, ok := strings.Cut(readme, "## Turn taking")
	if !ok {
		t.Fatalf("emitted %s README has no \"## Turn taking\" section", provider)
	}
	section, _, _ := strings.Cut(after, "\n## ")
	return section
}

// TestTurnRunbookExistsWithNothingAuthored is FR-010 plus the FR-015 corollary:
// a package that says nothing still waits two specific lengths of time, so the
// runbook still has to say what they are.
func TestTurnRunbookExistsWithNothingAuthored(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		t.Run(string(provider), func(t *testing.T) {
			section := turnSection(t, provider, "", "")
			// The balanced row, which is what an unset package gets.
			for _, want := range []string{"balanced", "1.6"} {
				if !strings.Contains(section, want) {
					t.Errorf("the turn section does not name %q with nothing authored:\n%s", want, section)
				}
			}
		})
	}
}

// TestTurnRunbookNamesTheFloorAndTheCeilingSeparately is FR-009a. One number is
// not enough: lowering the floor alone does not shorten a turn, and a runbook
// that reports a single "delay" teaches the reader the thing that already cost
// this repository a whole measurement effort.
func TestTurnRunbookNamesTheFloorAndTheCeilingSeparately(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		// The resolved numbers for endpointing_delay 400ms + pace snappy.
		wantFloor   string
		wantCeiling string
	}{
		{ir.ProviderLiveKit, "0.4", "1.2"},
		{ir.ProviderPipecat, "0.4", "1.2"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			section := turnSection(t, tc.provider, "400ms", ir.PaceSnappy)
			for _, want := range []string{
				tc.wantFloor,   // the floor
				tc.wantCeiling, // the ceiling
				"floor",
				"ceiling",
				"snappy",
			} {
				if !strings.Contains(section, want) {
					t.Errorf("the turn section does not name %q:\n%s", want, section)
				}
			}
			// The trap this whole feature exists to correct: a reader who thinks
			// the silence window is the wait.
			if !strings.Contains(section, "does not") && !strings.Contains(section, "not shorten") {
				t.Errorf("the turn section does not say that lowering the floor alone does not shorten a turn:\n%s", section)
			}
		})
	}
}

// TestPipecatRunbookDistinguishesTheTwoStopSecs is FR-009a's Pipecat half. Both
// fields are spelled stop_secs. A reader grepping the emitted bot.py finds two
// and has no way to tell which is which unless the runbook says.
func TestPipecatRunbookDistinguishesTheTwoStopSecs(t *testing.T) {
	section := turnSection(t, ir.ProviderPipecat, "", ir.PaceBalanced)
	for _, want := range []string{"VADParams", "SmartTurnParams", "stop_secs"} {
		if !strings.Contains(section, want) {
			t.Errorf("the Pipecat turn section does not name %q, so the two stop_secs fields are indistinguishable:\n%s", want, section)
		}
	}
}

// TestTurnRunbookStatesWhetherTheWaitAdapts is FR-009b: the two targets are not
// equally capable and the difference belongs in each runbook rather than in a
// comparison table nobody emits. LiveKit can adapt inside the window; Pipecat
// cannot, and has to say so rather than staying quiet about it.
func TestTurnRunbookStatesWhetherTheWaitAdapts(t *testing.T) {
	livekit := turnSection(t, ir.ProviderLiveKit, "", ir.PaceBalanced)
	if !strings.Contains(livekit, "dynamic") {
		t.Errorf("the LiveKit turn section does not name the endpointing mode:\n%s", livekit)
	}
	pipecat := turnSection(t, ir.ProviderPipecat, "", ir.PaceBalanced)
	if !strings.Contains(pipecat, "adapt") {
		t.Errorf("the Pipecat turn section does not say the wait does not adapt:\n%s", pipecat)
	}
}

// TestPipecatRunbookRecordsWhyItsFloorDoesNotMove keeps the measurement next to
// the reader most likely to want to change it. Someone who sees LiveKit's floor
// move with the pace and Pipecat's stay put will otherwise "fix" the
// inconsistency and make some turns a second slower.
func TestPipecatRunbookRecordsWhyItsFloorDoesNotMove(t *testing.T) {
	section := turnSection(t, ir.ProviderPipecat, "", ir.PaceBalanced)
	for _, want := range []string{"1.0", "transcript"} {
		if !strings.Contains(section, want) {
			t.Errorf("the Pipecat turn section does not record the flat-second cliff (%q):\n%s", want, section)
		}
	}
}

// TestBothTargetsReportTheEndpointingWaitUnderOneKey is FR-011a. The wait this
// feature changes has to be readable on both targets or half the change is
// unmeasurable, and it has to be the *same key* or the dev loop has to know
// which target it is looking at.
//
// This was a coverage gap in the plan and it closed itself on the rebase: main's
// dev_metrics work already emits `user_turn` on Pipecat from
// LatencyBreakdown.user_turn_secs, which the pinned pipecat 1.8.0 documents as
// running "from when the user actually stopped speaking to when the turn was
// released", including VAD silence, STT finalisation and the turn analyzer wait
// (observers/user_bot_latency_observer.py:97-102). LiveKit reports the same thing
// from end_of_turn_delay. So the gate is that they stay agreed, not that either
// gets built.
func TestBothTargetsReportTheEndpointingWaitUnderOneKey(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		source   string
	}{
		{ir.ProviderLiveKit, "end_of_turn_delay"},
		{ir.ProviderPipecat, "user_turn_secs"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			metrics := generatedFile(t, loadTurnFixture(t, "", ir.PaceBalanced), tc.provider, "dev_metrics.py")
			if !strings.Contains(metrics, `"user_turn"`) {
				t.Errorf("emitted dev_metrics.py does not report a \"user_turn\" key, so the endpointing wait is bundled with the rest of the turn")
			}
			if !strings.Contains(metrics, tc.source) {
				t.Errorf("emitted dev_metrics.py does not read %s, so \"user_turn\" is measuring something else", tc.source)
			}
		})
	}
}
