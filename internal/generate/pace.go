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
	// analyzer's own stop_secs, or the transcriber's end-of-turn timeout when
	// the transcriber decides the turn.
	Ceiling string
	// CeilingMillis is the same ceiling in whole milliseconds, which is how the
	// turn-detecting transcribers spell their timeout (eot_timeout_ms,
	// turn_end_timeout_ms).
	CeilingMillis string
	// MinDelay and Mode are LiveKit's endpointing dict. Pipecat has no separate
	// equivalent for either, and its runbook says so rather than pretending.
	MinDelay string
	Mode     string
	// ByListener says the transcriber decides the turn (`turn: provider:
	// listen`): no local silence window is consulted and no local analyzer is
	// built, so the floor above is not emitted and the ceiling reaches the
	// transcriber's own timeout instead. Pipecat only; validation refuses the
	// decider elsewhere before a driver sees it.
	ByListener bool
	// Eager says the transcriber's predicted end of turn is answered before it
	// is confirmed. Meaningful only with ByListener, which validation holds.
	Eager bool
	// Listener is the turn-detecting service row when ByListener is set, so the
	// runbook can name the class and the field the ceiling landed in.
	Listener targetcap.ListenTurnDetector
	// LocalPinArg and LocalPinValue are a setting written on the LOCAL path, to
	// hold a listening vendor to the turn behaviour the package already had.
	//
	// One vendor needs this. Speechmatics' turn_detection_mode defaulted to
	// EXTERNAL in pipecat 1.9.0, meaning the caller drives turns, which is
	// Pipecat's own detector; in 1.10.0 it defaults to VAD, meaning the service
	// closes turns itself. Left implicit, a package that binds it and says
	// nothing about turns changes who ends the caller's turn on a version bump.
	// So the old answer is written out.
	//
	// Empty for every other vendor and on the listening path, where EnableValue
	// is written instead.
	LocalPinArg   string
	LocalPinValue string
}

// resolvePaceView resolves a target's turn binding into the literals its
// template needs. An unset pace reads as balanced rather than empty, because the
// name reaches the runbook and the compile report and "" would tell an author
// their agent waits on nothing.
//
// listen is the target's listening binding, read only when the turn binding
// hands it the decision, to name the service that took it.
func resolvePaceView(provider targetcap.Provider, binding *ir.Binding, listen *ir.Binding) paceView {
	pace := targetcap.PaceBalanced
	if binding != nil && binding.Pace != "" {
		pace = string(binding.Pace)
	}
	profile := targetcap.ResolvePace(provider, pace)
	view := paceView{
		Name:          pace,
		Floor:         seconds(profile.VADSilence),
		Ceiling:       seconds(profile.TurnCeiling),
		CeilingMillis: strconv.Itoa(int(profile.TurnCeiling*1000 + 0.5)),
		MinDelay:      seconds(profile.MinDelay),
		Mode:          profile.Mode,
	}
	if binding != nil && binding.EndpointingDelay != "" {
		// FR-004: an authored duration is never overridden. It wins the floor and
		// leaves the ceiling to the pace, which is why they are separate fields.
		if authored := durationSeconds(binding.EndpointingDelay); authored != "" {
			view.Floor = authored
			view.FloorAuthored = true
		}
	}
	if listen == nil {
		return view
	}
	detector, ok := targetcap.LookupListenTurnDetector(provider, listenVendor(*listen))
	if !ok {
		return view
	}
	if binding != nil && binding.Provider == targetcap.TurnDeciderListen {
		view.ByListener = true
		view.Eager = binding.Eager
		view.Listener = detector
		return view
	}
	// The local detector decides. A vendor whose own turn detection is off by
	// default needs nothing said; one whose default turns it ON has to be told
	// not to, or the package's turn taking changes without the author asking.
	if detector.LocalValue != "" {
		view.LocalPinArg, view.LocalPinValue = detector.EnableArg, detector.LocalValue
	}
	return view
}

// listenVendor is the vendor a listening binding resolves to, with the same
// default spelling resolveService uses for an unnamed provider.
func listenVendor(listen ir.Binding) string {
	if listen.Provider == "" {
		return "openai"
	}
	return listen.Provider
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
	if p.ByListener {
		eager := "answers the confirmed turn"
		if p.Eager {
			eager = "answers the predicted turn early"
		}
		// A vendor with no end-of-turn timeout is told plainly that it kept its
		// own, rather than being credited with a ceiling it never took. `pace`
		// is refused on such a vendor, so the name here is always the default.
		if !p.Listener.HasCeiling() {
			return "turn " + p.Listener.Vendor + " decides (its own end-of-turn timing, no ceiling of ours, " + eager + ")"
		}
		return "turn pace " + p.Name + " (the transcriber decides, " + p.Listener.Class + " closes at " + p.Ceiling + "s via " + p.Listener.CeilingArg + ", " + eager + ")"
	}
	floor := "silence " + p.Floor + "s from the pace"
	if p.FloorAuthored {
		floor = "silence " + p.Floor + "s authored"
	}
	note := "turn pace " + p.Name + " (" + floor + ", closes at " + p.Ceiling + "s)"
	if p.LocalPinArg != "" {
		// Say it, because it is a setting the author did not write and it is the
		// answer to "what decides my turn" for the one vendor whose default
		// moved. Left unsaid, an author reading the report would have to open
		// the emitted bot to find out.
		note += "; the listening service is held to " + p.LocalPinArg + "=" + p.LocalPinValue + " so it does not close turns itself"
	}
	return note
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
