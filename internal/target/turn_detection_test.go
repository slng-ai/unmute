package target

import "testing"

// TestEveryTurnDetectionValueHasARowOnEveryCodeTarget. A missing row is not a
// missing feature, it is a wrong answer the caller hears: ResolveTurnDetection
// refuses rather than returning a zero value, and this is what keeps it from
// having to.
func TestEveryTurnDetectionValueHasARowOnEveryCodeTarget(t *testing.T) {
	for _, provider := range []Provider{LiveKit, Pipecat} {
		for _, value := range TurnDetectionValues() {
			profile, err := ResolveTurnDetection(provider, value)
			if err != nil {
				t.Errorf("%s has no row for turn_detection %q: %v", provider, value, err)
				continue
			}
			if !profile.Omit && profile.Expr == "" {
				t.Errorf("%s row for %q emits an argument and says nothing to put in it", provider, value)
			}
			if profile.Omit && profile.Expr != "" {
				t.Errorf("%s row for %q both omits the argument and carries an expression", provider, value)
			}
			for _, imp := range profile.Imports {
				if profile.Omit {
					t.Errorf("%s row for %q omits the argument and still asks for import %q", provider, value, imp)
				}
			}
		}
	}
	if _, err := ResolveTurnDetection(Slng, TurnDetectionServerVAD); err == nil {
		t.Error("the slng target resolved a turn detection value; it emits no realtime pipeline at all")
	}
	if _, err := ResolveTurnDetection(LiveKit, "whenever"); err == nil {
		t.Error("an unknown value resolved instead of being refused")
	}
}

// TestLiveKitLocalTurnDetectionEmitsNothing is the one row worth its own test.
//
// livekit-plugins-openai 1.8.1 sets can_disable_turn_detection from
// `not is_given(turn_detection)` (realtime_model.py:486). So the framework may
// only take the turn decision when the argument was never passed. Emitting
// `turn_detection=None` is the lowering a reader would expect and it is wrong:
// it turns detection off with nothing taking over. Emitting the vendor's own
// default explicitly is wrong in the same way, and looks even more harmless.
//
// This asserts the shape of the answer rather than the text of an emitted line,
// because the drivers read this table and the mistake would be made here.
func TestLiveKitLocalTurnDetectionEmitsNothing(t *testing.T) {
	profile, err := ResolveTurnDetection(LiveKit, TurnDetectionLocal)
	if err != nil {
		t.Fatal(err)
	}
	if !profile.Omit {
		t.Fatal("turn_detection: local emits an argument on livekit, which costs the framework the right to decide the turn")
	}

	// Pipecat is the control: there the same value is a literal False, which
	// that library holds apart from None on purpose. Two targets, two correct
	// answers, and neither is the other's.
	pipecat, err := ResolveTurnDetection(Pipecat, TurnDetectionLocal)
	if err != nil {
		t.Fatal(err)
	}
	if pipecat.Omit || pipecat.Expr != "False" {
		t.Errorf("pipecat turn_detection: local resolved to %+v, want the literal False", pipecat)
	}
}
