package generate

import (
	"strconv"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// paceView is one resolved target.PaceProfile rendered as the literals a
// template writes. The rendering lives here rather than in each driver because
// both drivers need the same numbers formatted the same way, and a Python float
// spelled two ways in two files is a diff nobody can read.
//
// The profile itself comes from internal/target, which owns the mapping. This
// type owns nothing but the strings.
type paceView struct {
	// Name is the resolved word: an unset pace reads "balanced" here, never "".
	// The runbook and the compile report both name it, so an author can see what
	// their silence became without opening the generated code.
	Name string
	// Floor is the silence window actually emitted, in seconds. It is the
	// authored endpointing_delay when the package set one, and the pace's own
	// floor otherwise. Never empty: leaving it to the framework meant inheriting
	// Silero's 0.55s on LiveKit, which is a default nobody chose.
	Floor string
	// FloorAuthored says the floor came from endpointing_delay rather than from
	// the pace. The runbook says which, because "0.4s" alone does not tell an
	// author whether changing the pace will move it.
	FloorAuthored bool
	// Ceiling is the longest the runtime waits before closing the turn
	// regardless, in seconds. LiveKit spells it max_delay; Pipecat spells it the
	// analyzer's own stop_secs.
	Ceiling string
	// MinDelay and Mode are LiveKit's endpointing dict. Pipecat has no separate
	// equivalent for either, and its runbook says so rather than pretending.
	MinDelay string
	Mode     string
}

// resolvePaceView resolves a target's turn binding into the literals its
// template needs. An unset pace reads as balanced rather than empty, because the
// name reaches the runbook and the compile report and "" would tell an author
// their agent waits on nothing.
func resolvePaceView(provider targetcap.Provider, binding *ir.Binding) paceView {
	pace := targetcap.PaceBalanced
	if binding != nil && binding.Pace != "" {
		pace = string(binding.Pace)
	}
	profile := targetcap.ResolvePace(provider, pace)
	view := paceView{
		Name:     pace,
		Floor:    seconds(profile.VADSilence),
		Ceiling:  seconds(profile.TurnCeiling),
		MinDelay: seconds(profile.MinDelay),
		Mode:     profile.Mode,
	}
	if binding != nil && binding.EndpointingDelay != "" {
		// FR-004: an authored duration is never overridden. It wins the floor and
		// leaves the ceiling to the pace, which is why they are separate fields.
		if authored := durationSeconds(binding.EndpointingDelay); authored != "" {
			view.Floor = authored
			view.FloorAuthored = true
		}
	}
	return view
}

// seconds renders a profile value the way Python reads it: 0.3, 1.6, 3. Trailing
// zeros are dropped because a ceiling written 3.0 in one file and 3 in another
// makes a golden diff about formatting instead of behaviour.
func seconds(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// note is the compile-report line. It names the pace and both numbers, because
// the whole point of one authored word is that the author can still see what it
// became, and "pace: snappy" alone does not tell anyone how long the agent waits.
func (p paceView) note() string {
	floor := "silence " + p.Floor + "s from the pace"
	if p.FloorAuthored {
		floor = "silence " + p.Floor + "s authored"
	}
	return "turn pace " + p.Name + " (" + floor + ", closes at " + p.Ceiling + "s)"
}

// semanticEndpointingOff reports whether the package asked for no semantic
// end-of-turn model.
//
// Only `off` changes anything. `preferred` and `required` both emit the model,
// and so does an unset field: every shipped code target can provide one, so
// there is nothing for `required` to assert that is not already true. The
// distinction is kept in the authored field rather than collapsed, because it
// documents intent for a target that might one day not be able to.
func semanticEndpointingOff(binding *ir.Binding) bool {
	return binding != nil && binding.SemanticEndpointing == ir.SemanticEndpointingOff
}
