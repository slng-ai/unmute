//go:build smoke

package generate

import (
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// Three of the four listening turn deciders are switched on three different
// ways, and two of those ways arrived in pipecat 1.10.0. Whether a keyword
// exists on a real constructor is exactly the question no offline test can
// answer: the compiler writes a name, and the name is either a parameter of the
// installed class or a TypeError on the first call in a deployed container.
//
// smokeCheckScript imports the emitted bot and calls every service builder
// against the really installed services, so each of these runs the emitted
// build_stt() for real.

// TestSmokeGradiumTurnDetectionConstructs proves enable_turn_detection is a
// parameter of the installed GradiumSTTService, on its ordinary class, with no
// ceiling keyword beside it.
func TestSmokeGradiumTurnDetectionConstructs(t *testing.T) {
	runPipecatSmokeScript(t, "turn_listener_gradium", nil, func(agent *ir.Agent) {
		agent.Tracing = nil
	}, smokeCheckScript)
}

// TestSmokeSpeechmaticsTurnModeConstructs proves the same for the settings
// switch, in both directions.
//
// The pair matters more than either half. 1.10.0 flipped this service's
// turn_detection_mode default from EXTERNAL to VAD, so the local package is the
// one asserting that a mode the author never wrote is accepted by the installed
// Settings dataclass; the listening package asserts the other value is too. A
// wrong enum member is an AttributeError at construction, which is the failure
// this catches and the offline suite cannot.
func TestSmokeSpeechmaticsTurnModeConstructs(t *testing.T) {
	for _, fixture := range []string{"speechmatics_local", "speechmatics_listen"} {
		t.Run(fixture, func(t *testing.T) {
			runPipecatSmokeScript(t, fixture, nil, func(agent *ir.Agent) {
				agent.Tracing = nil
			}, smokeCheckScript)
		})
	}
}
