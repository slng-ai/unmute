package target

import "testing"

// The pace table is a set of numbers with reasons behind them, and a number with
// its reason left in a comment is a number the next person tidies away. These
// are the reasons, as assertions.
//
// These tests are also what lets ResolvePace be total: a missing row reads as a
// zero profile there rather than an error, and this is where that surfaces.

func TestEveryPaceHasACompleteRowForEveryTargetThatReadsOne(t *testing.T) {
	for _, provider := range Providers {
		if !EmitsProject(provider) {
			// Slng owns its own turn taking, so it reads no profile. The capability
			// row denies FieldPace, which is the gate for that.
			continue
		}
		for _, pace := range PaceValues() {
			profile := ResolvePace(provider, pace)
			if profile.VADSilence <= 0 {
				t.Errorf("%s/%s: VADSilence is %v; a zero here emits a faster agent than anyone asked for", provider, pace, profile.VADSilence)
			}
			if profile.TurnCeiling <= 0 {
				t.Errorf("%s/%s: TurnCeiling is %v, so the turn never closes on the ceiling", provider, pace, profile.TurnCeiling)
			}
			if profile.TurnCeiling <= profile.VADSilence {
				t.Errorf("%s/%s: ceiling %v is not above floor %v, so the ceiling is unreachable", provider, pace, profile.TurnCeiling, profile.VADSilence)
			}
		}
	}
}

func TestLiveKitRowsCarryEveryFieldThatTargetReads(t *testing.T) {
	// LiveKit reads MinDelay and Mode; Pipecat reads neither. A zero MinDelay
	// emits min_delay 0 rather than inheriting a default, and an empty Mode emits
	// `"mode": ""`, which LiveKit does not accept.
	for _, pace := range PaceValues() {
		profile := ResolvePace(LiveKit, pace)
		if profile.MinDelay <= 0 {
			t.Errorf("livekit/%s: MinDelay is %v, and LiveKit emits it verbatim", pace, profile.MinDelay)
		}
		if profile.MinDelay > profile.TurnCeiling {
			t.Errorf("livekit/%s: min_delay %v exceeds max_delay %v, which LiveKit reads as a window that cannot open", pace, profile.MinDelay, profile.TurnCeiling)
		}
		if profile.Mode != "dynamic" && profile.Mode != "fixed" {
			t.Errorf("livekit/%s: Mode is %q, and LiveKit takes only \"dynamic\" or \"fixed\"", pace, profile.Mode)
		}
	}
}

func TestEveryLiveKitFloorClearsTheTurnDetectorHardFloor(t *testing.T) {
	// The LiveKit audio turn detector raises at session start below 0.25s
	// (research.md §3). A row under it compiles and then fails to start.
	const hardFloor = 0.25
	for _, pace := range PaceValues() {
		profile := ResolvePace(LiveKit, pace)
		if profile.VADSilence < hardFloor {
			t.Errorf("livekit/%s: VADSilence %v is below the %v hard floor; the turn detector raises there", pace, profile.VADSilence, hardFloor)
		}
	}
}

func TestEveryPipecatFloorIsExactlyTheMeasuredValue(t *testing.T) {
	// This is the one number in the table with measurement behind it, and it is
	// the one most likely to be "tidied" into symmetry with LiveKit's column.
	//
	// pipecat-slng asks the bridge to finalise when VAD reports the caller
	// stopped. A final that already arrived finds no request outstanding, so the
	// frame goes out unfinalized and Pipecat waits out a flat 1.0s safety net.
	// Measured over eight turns at 400ms: every turn whose transcript beat the
	// window paid a flat 1.000s. Observed transcripts run 0.27s and up.
	//
	// So the window is a cliff, not a dial, and raising it for a patient agent
	// would make that agent a second SLOWER on some turns. The pace reaches
	// Pipecat through the ceiling instead. See research.md §5a and the
	// per-target override in examples/salon-concierge/targets.yaml.
	const measured = 0.2
	for _, pace := range PaceValues() {
		profile := ResolvePace(Pipecat, pace)
		if profile.VADSilence != measured {
			t.Errorf("pipecat/%s: VADSilence is %v, want exactly %v. Do not vary this column with the pace: above the transcript's arrival time Pipecat pays a flat 1.0s, so a wider window is slower. research.md §5a", pace, profile.VADSilence, measured)
		}
	}
}

func TestUnsetPaceResolvesToBalancedRatherThanZero(t *testing.T) {
	for _, provider := range Providers {
		if !EmitsProject(provider) {
			continue
		}
		if unset, balanced := ResolvePace(provider, ""), ResolvePace(provider, PaceBalanced); unset != balanced {
			t.Errorf("%s: an unset pace resolved to %+v, want the balanced row %+v. FR-015: a package that says nothing gets the faster behaviour", provider, unset, balanced)
		}
	}
}

func TestFasterPacesAreActuallyFaster(t *testing.T) {
	// A table where patient is snappier than snappy is a table someone typed in
	// the wrong order, and nothing else here would catch it.
	for _, provider := range Providers {
		if !EmitsProject(provider) {
			continue
		}
		snappy := ResolvePace(provider, PaceSnappy)
		balanced := ResolvePace(provider, PaceBalanced)
		patient := ResolvePace(provider, PacePatient)
		if snappy.TurnCeiling >= balanced.TurnCeiling || balanced.TurnCeiling >= patient.TurnCeiling {
			t.Errorf("%s: ceilings run %v, %v, %v for snappy, balanced, patient; they must increase", provider, snappy.TurnCeiling, balanced.TurnCeiling, patient.TurnCeiling)
		}
	}
}

func TestPatientReproducesTheFrameworkCeiling(t *testing.T) {
	// patient is the escape hatch, and contracts/authoring.md promises that
	// selecting it changes nothing for a package that has today's behaviour.
	// These are the pinned framework defaults: LiveKit
	// _STREAMING_ENDPOINTING_DEFAULTS max_delay 2.5 (voice/turn.py:142) and
	// Pipecat SmartTurnParams stop_secs 3 (base_smart_turn.py:32).
	for provider, want := range map[Provider]float64{LiveKit: 2.5, Pipecat: 3.0} {
		if got := ResolvePace(provider, PacePatient).TurnCeiling; got != want {
			t.Errorf("%s/patient: ceiling is %v, want the framework default %v so patient changes nothing", provider, got, want)
		}
	}
}
