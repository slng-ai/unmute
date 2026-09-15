package target

import "fmt"

// TurnDetectionProfile is what one authored `turn_detection:` becomes on one
// target, for an `architecture: realtime` package.
//
// One table read by both drivers, for the reason paceProfiles is one: who
// decides the turn is a target fact, and a second copy is the thing that drifts.
//
// The field that carries the weight here is Omit, and it is not a convenience.
// livekit-plugins-openai 1.8.1 computes
//
//	can_disable_turn_detection=not is_given(turn_detection)
//
// at realtime_model.py:486. So passing *any* turn_detection argument, including
// one identical to the vendor's own default, costs the framework the right to
// hand turn taking to the client. `turn_detection: local` therefore lowers to
// emitting nothing at all, and the framework's own detector takes over exactly
// as it does for a cascaded package. Emitting `turn_detection=None` instead
// would read as the obvious lowering and would turn detection off everywhere
// with nothing taking its place.
//
// This is the only lowering in the tree where the correct emission is silence,
// which is why it gets a field of its own rather than an empty Expr that a
// reader would take for "not filled in yet".
//
// The two targets take different routes and arrive at the same place, which is
// worth knowing before anybody makes them match. On the wire both send
// `turn_detection: null`, which is the API's own way of saying nobody on the
// server decides. LiveKit gets there by being passed nothing: the framework
// then sees it may take the decision, and its session copy sets None
// (realtime_model.py:891). Pipecat gets there from the literal False, which its
// SessionUpdateEvent.model_dump rewrites to null on the way out, deliberately
// and with a comment saying so (services/openai/realtime/events.py:390-411).
//
// So neither route is the other's, and swapping either one breaks it: passing
// None on LiveKit costs the framework the decision before it is offered, and
// passing None on Pipecat is dropped by exclude_none, which leaves the vendor's
// own detector running. Both smokes assert the wire, not just the driver.
type TurnDetectionProfile struct {
	// Omit says to emit no turn-detection argument at all. See above.
	Omit bool
	// Expr is the Python expression the driver emits when Omit is false. It is
	// rendered verbatim, so it carries its own imports' module alias.
	Expr string
	// Imports is every module the expression names, so a driver can register
	// them without parsing Expr.
	Imports []string
}

// The three answers, as untyped string constants so internal/spec owns the
// spelling and this package owns what each becomes.
const (
	TurnDetectionServerVAD = "server_vad"
	TurnDetectionSemantic  = "semantic"
	TurnDetectionLocal     = "local"
)

// turnDetectionProfiles maps an authored value onto each target.
//
// Every row is deliberate, including the two Omit rows, which look identical and
// are not: `local` omits so the framework can take over, and an absent value is
// not in this table at all, because a package that says nothing sends nothing.
var turnDetectionProfiles = map[Provider]map[string]TurnDetectionProfile{
	LiveKit: {
		// openai.types.realtime.realtime_audio_input_turn_detection.ServerVad.
		// Its own defaults are the server's, so nothing is spelled out here: a
		// field written in would be a setting the author did not ask for.
		TurnDetectionServerVAD: {
			Expr:    `openai_realtime.ServerVad(type="server_vad")`,
			Imports: []string{"from openai.types.realtime import realtime_audio_input_turn_detection as openai_realtime"},
		},
		// SemanticVad. This is also what the vendor uses when nothing is given
		// (utils.py:36-41), so writing it is how an author pins today's default
		// against a future change rather than a no-op.
		TurnDetectionSemantic: {
			Expr:    `openai_realtime.SemanticVad(type="semantic_vad")`,
			Imports: []string{"from openai.types.realtime import realtime_audio_input_turn_detection as openai_realtime"},
		},
		TurnDetectionLocal: {Omit: true},
	},
	Pipecat: {
		// pipecat.services.openai.realtime.events. The package's own __init__ is
		// empty in 1.10.0, so the import is the full module path.
		TurnDetectionServerVAD: {
			Expr:    `realtime_events.TurnDetection()`,
			Imports: []string{"from pipecat.services.openai.realtime import events as realtime_events"},
		},
		TurnDetectionSemantic: {
			Expr:    `realtime_events.SemanticTurnDetection()`,
			Imports: []string{"from pipecat.services.openai.realtime import events as realtime_events"},
		},
		// Literal False, which pipecat holds apart from None on purpose:
		// llm.py:527-540 says None keeps the vendor's default and False is the
		// opt-out that puts the service in manual mode. Manual mode is what lets
		// this project's own turn settings decide, which is what `local` means.
		TurnDetectionLocal: {Expr: "False"},
	},
}

// ResolveTurnDetection answers what an authored value becomes on one target.
//
// It refuses rather than falling back, because every fallback here is a wrong
// answer a caller would hear: a missing row that resolved to the zero value
// would emit no argument and read as `local` on LiveKit, silently moving the
// turn decision.
func ResolveTurnDetection(provider Provider, value string) (TurnDetectionProfile, error) {
	rows, ok := turnDetectionProfiles[provider]
	if !ok {
		return TurnDetectionProfile{}, fmt.Errorf("no turn detection table for %s: architecture: realtime is not emitted there", provider)
	}
	profile, ok := rows[value]
	if !ok {
		return TurnDetectionProfile{}, fmt.Errorf("turn_detection %q has no lowering on %s", value, provider)
	}
	return profile, nil
}

// TurnDetectionValues is every legal value, in the order refusals and docs name
// them. One ordered list, so a value added to the table above reaches the
// message that lists the legal ones.
func TurnDetectionValues() []string {
	return []string{TurnDetectionServerVAD, TurnDetectionSemantic, TurnDetectionLocal}
}
