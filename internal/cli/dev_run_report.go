package cli

import (
	"fmt"
	"time"

	"github.com/slng-ai/unmute/internal/target"
)

// What a telephony run produced. The printed output and the automated check
// read this one structure, so a run cannot print something the check does not
// see, or pass a check about output nobody printed.

// transferOutcome is how far a transfer got. It is an enumeration because "it
// worked" is several different things, and two of them used to be conflated:
// a caller that accepts a transfer request and then does nothing with it read
// as a success, which is how a silently failed transfer looked healthy.
type transferOutcome string

const (
	transferRequested          transferOutcome = "requested"
	transferAccepted           transferOutcome = "accepted"
	transferDestinationReached transferOutcome = "destination_reached"
	transferMerged             transferOutcome = "merged"
	transferUnavailableReturn  transferOutcome = "unavailable_returned"
	transferUnavailableHangup  transferOutcome = "unavailable_hangup"
	// transferNotActedOn: the plane accepted the request and the caller never
	// acted on it. For a cold transfer the product's responsibility ends at
	// acceptance, so this names the caller as the likely cause rather than
	// reporting a failure the product did not cause.
	transferNotActedOn transferOutcome = "not_acted_on"
)

// transferShape is cold or warm. Only a warm transfer can merge.
type transferShape string

const (
	coldTransfer transferShape = "cold"
	warmTransfer transferShape = "warm"
)

// transferRecord is one transfer and how far it got.
type transferRecord struct {
	Shape       transferShape
	Destination string
	Outcome     transferOutcome
}

// transferAdvance is the only legal move out of each outcome. Acceptance is
// where a cold transfer's product responsibility ends, so everything after it
// is about what the caller and the destination did, and a merge belongs to a
// warm transfer alone.
var transferAdvance = map[transferOutcome][]transferOutcome{
	// A request can fail before anything accepts it: a cold transfer whose
	// REFER is refused logs its failure immediately after going out, with no
	// acceptance in between. Corrected 2026-08-20, when reading the emitted
	// agent's own log vocabulary showed the sequence this model had ruled out.
	// The two that still require acceptance are the ones that mean somebody
	// acted: a destination cannot be reached, and a caller cannot fail to act,
	// on a request nothing ever took.
	transferRequested: {transferAccepted, transferUnavailableReturn, transferUnavailableHangup},
	transferAccepted: {
		transferDestinationReached, transferUnavailableReturn,
		transferUnavailableHangup, transferNotActedOn,
	},
	transferDestinationReached: {transferMerged},
	transferMerged:             nil,
	transferUnavailableReturn:  nil,
	transferUnavailableHangup:  nil,
	transferNotActedOn:         nil,
}

// advance moves a transfer to its next outcome, refusing a move the shape or
// the order does not allow. A refusal is a bug in the caller, not a call that
// went badly, which is why it returns an error rather than recording one.
func (record *transferRecord) advance(next transferOutcome) error {
	if next == transferMerged && record.Shape != warmTransfer {
		return fmt.Errorf("transfer to %s: only a warm transfer can merge, this one is %s", record.Destination, record.Shape)
	}
	for _, allowed := range transferAdvance[record.Outcome] {
		if allowed == next {
			record.Outcome = next
			return nil
		}
	}
	return fmt.Errorf("transfer to %s: cannot go from %s to %s", record.Destination, record.Outcome, next)
}

// callLeg is one leg of one call and how it ended.
type callLeg struct {
	Direction string // "inbound" or "outbound", the same words the route uses
	Endpoint  string
	Start     time.Time
	End       time.Time
	EndedBy   string
}

// runReport is what one run produced.
type runReport struct {
	Plane           target.TelephonyLocalPlane
	DialInstruction string
	// DialCredential is generated per run and printed, because the developer
	// has to type it into a softphone. It is not a secret in the sense the
	// constitution protects: it authorises one call to a plane listening on
	// this machine, it dies with the run, and its disclosure has no
	// consequence anywhere else. It reaches no emitted file.
	DialCredential string
	Calls          []callLeg
	Transfers      []transferRecord
	Recordings     []string
	// CarrierWrites is every request this run made to a carrier, recorded by
	// the code that makes them. It exists so that "nothing left this machine"
	// is an assertion instead of a claim: the default loop must leave it empty.
	CarrierWrites []string
}

// carrierWrite records a request to a carrier. It is called before the request
// is made, so a write that fails halfway is recorded too: the point is that no
// write is silent, not that every write succeeded.
func (report *runReport) carrierWrite(format string, args ...any) {
	if report == nil {
		return
	}
	report.CarrierWrites = append(report.CarrierWrites, fmt.Sprintf(format, args...))
}
