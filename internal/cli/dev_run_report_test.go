package cli

import (
	"strings"
	"testing"
)

// P8: the outcome distinguishes all seven states, and the order between them is
// enforced. The state that matters most is not_acted_on: a caller that accepts
// a transfer request and then does nothing with it is the case the old code
// reported as a success, which is how a transfer that silently failed looked
// healthy in the output.
func TestTransferOutcomeStateMachineAllowsOnlyRealSequences(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape transferShape
		steps []transferOutcome
		wantN int // how many steps must be accepted before one is refused
	}{
		{"cold ends at accepted", coldTransfer,
			[]transferOutcome{transferAccepted, transferDestinationReached}, 2},
		{"cold cannot merge", coldTransfer,
			[]transferOutcome{transferAccepted, transferDestinationReached, transferMerged}, 2},
		{"warm merges", warmTransfer,
			[]transferOutcome{transferAccepted, transferDestinationReached, transferMerged}, 3},
		{"destination did not answer, caller came back", warmTransfer,
			[]transferOutcome{transferAccepted, transferUnavailableReturn}, 2},
		{"destination did not answer, call ended", coldTransfer,
			[]transferOutcome{transferAccepted, transferUnavailableHangup}, 2},
		{"caller never acted on it", coldTransfer,
			[]transferOutcome{transferAccepted, transferNotActedOn}, 2},
		{"acceptance cannot be skipped", warmTransfer,
			[]transferOutcome{transferDestinationReached}, 0},
		{"a finished transfer does not continue", warmTransfer,
			[]transferOutcome{transferAccepted, transferNotActedOn, transferDestinationReached}, 2},
		{"a request is not repeated", coldTransfer,
			[]transferOutcome{transferRequested}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := transferRecord{Shape: tc.shape, Destination: "supervisor_line", Outcome: transferRequested}
			accepted := 0
			for _, step := range tc.steps {
				if err := record.advance(step); err != nil {
					break
				}
				accepted++
			}
			if accepted != tc.wantN {
				t.Fatalf("accepted %d of %d steps, want %d (ended at %s)", accepted, len(tc.steps), tc.wantN, record.Outcome)
			}
		})
	}
}

// The refusal has to say which move was impossible, because it is a bug in the
// code that called it, and "invalid transition" sends the reader to a debugger.
func TestTransferAdvanceRefusalNamesTheMove(t *testing.T) {
	record := transferRecord{Shape: coldTransfer, Destination: "billing_line", Outcome: transferAccepted}
	if err := record.advance(transferMerged); err == nil {
		t.Fatal("a cold transfer must not merge")
	} else {
		for _, want := range []string{"billing_line", "warm", "cold"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal never names %q: %v", want, err)
			}
		}
	}
	if record.Outcome != transferAccepted {
		t.Errorf("a refused move changed the outcome to %s", record.Outcome)
	}
}

// Every outcome the contract names is reachable, so none of the seven is a
// value the code can never produce.
func TestEveryTransferOutcomeIsReachable(t *testing.T) {
	reached := map[transferOutcome]bool{transferRequested: true}
	for from, targets := range transferAdvance {
		if !reached[from] && len(targets) == 0 {
			continue
		}
		for _, to := range targets {
			reached[to] = true
		}
	}
	for _, outcome := range []transferOutcome{
		transferRequested, transferAccepted, transferDestinationReached, transferMerged,
		transferUnavailableReturn, transferUnavailableHangup, transferNotActedOn,
	} {
		if !reached[outcome] {
			t.Errorf("outcome %s is unreachable, so nothing can ever report it", outcome)
		}
	}
}

// The recorder is what gate P2 asserts against, so it has to record before the
// request rather than after: a write that fails halfway still changed something
// at the carrier.
func TestCarrierWriteRecordsEveryRequest(t *testing.T) {
	var report runReport
	report.carrierWrite("twilio: point %s at this run", "+15550001111")
	report.carrierWrite("twilio: restore %s", "+15550001111")
	if len(report.CarrierWrites) != 2 {
		t.Fatalf("recorded %d writes, want 2: %v", len(report.CarrierWrites), report.CarrierWrites)
	}
	if !strings.Contains(report.CarrierWrites[0], "+15550001111") {
		t.Errorf("the record does not name what was written: %q", report.CarrierWrites[0])
	}
	// A nil report is what a code path with no report looks like, and it must
	// not panic: the recorder is on the path that talks to a real carrier.
	var absent *runReport
	absent.carrierWrite("no report here")
}
