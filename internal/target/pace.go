package target

// The three paces, as untyped string constants so internal/ir can define its
// typed Pace from them and there is one spelling of each word in the tree.
const (
	PaceSnappy   = "snappy"
	PaceBalanced = "balanced"
	PacePatient  = "patient"
)

// PaceValues is every legal pace, in the order an author should think about
// them. The validator's refusal reads it, so a value added to the table below
// reaches the message that lists the legal ones.
func PaceValues() []string {
	return []string{PaceSnappy, PaceBalanced, PacePatient}
}

// PaceProfile is what one authored pace becomes on one target.
//
// Every field carries a deliberate value, never a zero meaning "unset": a target
// that reads a zero here emits a faster agent than anyone asked for. pace_test.go
// is the gate for that.
type PaceProfile struct {
	// VADSilence is the floor: the silence the detector waits before it reports
	// that speech stopped. An authored endpointing_delay replaces it.
	VADSilence float64
	// TurnCeiling is the longest the runtime waits before closing the turn
	// regardless. LiveKit spells it max_delay; Pipecat spells it the analyzer's
	// own stop_secs, which is a different field from the VAD's stop_secs despite
	// the shared name.
	TurnCeiling float64
	// MinDelay is LiveKit's min_delay. Pipecat has no separate equivalent, so it
	// is unused there rather than approximated.
	MinDelay float64
	// Mode is LiveKit's endpointing mode: "dynamic" adapts inside
	// [MinDelay, TurnCeiling] from the pauses a caller actually leaves, "fixed"
	// does not. Pipecat can do neither, reads neither field, and its runbook
	// says so.
	Mode string
}

// paceProfiles maps an authored pace onto each target's own floor and ceiling.
// Same shape as liveKitTurnModels in catalog_livekit.go, and here for the same
// reason: a floor and a ceiling are target facts, and Principle III puts target
// facts in this package.
//
// The numbers are a proposal until the paired measurement in User Story 3
// confirms them. Do not quote them anywhere as measured. One exception, called
// out in the row itself: the Pipecat floor.
var paceProfiles = map[Provider]map[string]PaceProfile{
	// LiveKit: the pace reaches both the prewarmed VAD's silence window and the
	// endpointing dict, so both the floor and the ceiling move. patient is the
	// pinned framework default (_STREAMING_ENDPOINTING_DEFAULTS, voice/turn.py:142)
	// so selecting it changes nothing.
	LiveKit: {
		PaceSnappy:   {VADSilence: 0.25, MinDelay: 0.25, TurnCeiling: 1.2, Mode: "dynamic"},
		PaceBalanced: {VADSilence: 0.3, MinDelay: 0.3, TurnCeiling: 1.6, Mode: "dynamic"},
		PacePatient:  {VADSilence: 0.4, MinDelay: 0.5, TurnCeiling: 2.5, Mode: "fixed"},
	},
	// Pipecat: the floor does NOT vary with the pace, and that is measured rather
	// than chosen. pipecat-slng asks the bridge to finalise when VAD reports the
	// caller stopped; a final that already arrived finds no request outstanding,
	// so the frame goes out unfinalized and Pipecat waits out a flat 1.0s safety
	// net. Over eight turns at 400ms every turn whose transcript beat the window
	// paid a flat 1.000s, and observed transcripts run 0.27s and up. A wider
	// window is therefore SLOWER, so a patient row at 0.3 would have made a
	// patient agent a second slower on some turns.
	//
	// The pace reaches this target through the ceiling alone. Do not make this
	// column vary again because it looks inconsistent with LiveKit's: the
	// inconsistency is the finding. research.md §5a, and
	// examples/salon-concierge/targets.yaml carries the measurement.
	Pipecat: {
		PaceSnappy:   {VADSilence: 0.2, TurnCeiling: 1.2},
		PaceBalanced: {VADSilence: 0.2, TurnCeiling: 1.6},
		PacePatient:  {VADSilence: 0.2, TurnCeiling: 3.0},
	},
}

// ResolvePace returns the profile for an authored pace on one target. An empty
// pace resolves to balanced, because a package that authored nothing still gets
// the faster behaviour rather than whatever the framework defaulted to.
//
// Total rather than error-returning, because three gates already stand between
// this and a miss: ir.Validate refuses a pace no table names, the capability
// table denies FieldPace on a target with no rows, and pace_test.go holds every
// emitting target to a complete set. A miss would be a compiler bug, and
// pace_test.go is where it surfaces.
func ResolvePace(provider Provider, pace string) PaceProfile {
	if pace == "" {
		pace = PaceBalanced
	}
	return paceProfiles[provider][pace]
}
