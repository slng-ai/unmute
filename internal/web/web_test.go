// The dev page is one embedded file with no test runner, and adding one would
// introduce a JavaScript toolchain this repository does not have anywhere else.
// So the tests below assert that the shipped page still contains the code that
// implements a behaviour; they cannot prove that pressing a key does the right
// thing. That limit is a recorded decision, not an oversight: see the Assumptions
// section of specs/016-dev-ui-metrics-logs/spec.md, which makes the manual
// walkthrough in that feature's quickstart.md the gate for interface behaviour.
// Treat these as tripwires against silent deletion, and walk the page by hand.
package web

import (
	"strings"
	"testing"
)

// page returns the shipped dev page, which is what every test here inspects.
func page(t *testing.T) string {
	t.Helper()
	raw, err := FS.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestV16PipecatUsesRTVI2SegmentUpdates(t *testing.T) {
	source := page(t)
	for _, want := range []string{
		`type:"client-ready"`,
		`version:"2.0.0"`,
		`new Map()`,
		`t === "bot-output"`,
		`d.segment_id`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("web client missing %q", want)
		}
	}
	if strings.Contains(source, `t === "bot-transcription"`) {
		t.Error("web client still renders deprecated bot-transcription frames")
	}
}

func TestV17LiveKitUpdatesTranscriptionSegment(t *testing.T) {
	source := page(t)
	if !strings.Contains(source, `setBotSegment(seg.id, text)`) {
		t.Error("LiveKit remote transcription does not update by segment id")
	}
	if strings.Contains(source, `else pushTurn("bot", text)`) {
		t.Error("LiveKit remote transcription still appends every update")
	}
}

func TestControlsLabelTheStateNotTheAction(t *testing.T) {
	source := page(t)
	for _, want := range []string{
		`MIC LIVE`,                       // capturing
		`MIC MUTED`,                      // not capturing
		`MIC —`,                          // no call, or permission refused
		`el.mute.dataset.mic = micState`, // one state drives the appearance
		`el.mute.disabled = micState === "unavailable"`, // inert until a call exists
		`el.connect.textContent = "end call"`,           // the primary button names the next transition
	} {
		if !strings.Contains(source, want) {
			t.Errorf("control states missing %q", want)
		}
	}
	// A verb on the microphone control is the defect this replaced: the label has
	// to say what is true, and let styling carry what a click would do.
	for _, forbidden := range []string{
		`el.mute.textContent = "mic on"`,
		`muted ? "muted" : "mic on"`,
		`el.connect.textContent = "disconnect"`,
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("control still labelled with an action: %q", forbidden)
		}
	}
}

func TestMicrophoneWorksOnBothTransports(t *testing.T) {
	source := page(t)
	for _, want := range []string{
		`switchActiveDevice("audioinput", selectedDeviceId)`, // livekit input selection
		`getTrackPublication(LK.Track.Source.Microphone)`,    // livekit local track
		`reconnectMic(new MediaStream([track]))`,             // feeds the shared analyser
		`micNode.connect(micAnalyser)`,                       // mic-only tap for the level
		`el.microw.hidden = devices.length < 2`,              // offer a choice only when there is one
	} {
		if !strings.Contains(source, want) {
			t.Errorf("microphone wiring missing %q", want)
		}
	}
	// livekit used to hide the picker outright, justified by a comment claiming the
	// SDK owns device selection. It does not: it exposes switchActiveDevice.
	for _, forbidden := range []string{
		`el.microw.hidden = true`,
		`the SDK owns mic selection`,
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("livekit still refuses input selection: %q", forbidden)
		}
	}
}

func TestHoldToTalkCannotBeSwallowedByAFocusedButton(t *testing.T) {
	source := page(t)
	// SPACE activates a focused button and scrolls the page. Both would fire while
	// holding to talk, and the first one ends the call. The handler must be at the
	// document, must preventDefault, must ignore auto-repeat, and clicks must not
	// leave focus on a button in the first place.
	for _, want := range []string{
		`document.addEventListener("keydown"`,
		`document.addEventListener("keyup"`,
		`e.preventDefault()`,
		`e.repeat`,
		`typingTarget(e.target)`,
		`el.connect.blur()`,
		`el.mute.blur()`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("hold-to-talk guard missing %q", want)
		}
	}
}

func TestPipecatWaitsForARealConnectionAndStartsFresh(t *testing.T) {
	source := page(t)
	for _, want := range []string{
		`pcId = null;`,
		`await waitForPeer(pc);`,
		`clientReadyTimer = setInterval(sendClientReady, 500)`,
		`t === "bot-ready"`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("Pipecat reconnect/ready contract missing %q", want)
		}
	}
}

func TestPageShowsTheRunBeforeThereIsACall(t *testing.T) {
	source := page(t)
	for _, want := range []string{
		`new EventSource("/api/events")`,                // the stream, not polling
		`ev.t === "state"`,                              // lifecycle drives the view
		`ev.t === "metric"`,                             // records are not littered through the log
		`session.ready === false`,                       // answer, not error, before a runtime
		`el.connect.disabled = true`,                    // no call to offer yet
		`if (logRows.length > LOG_MAX) logRows.shift()`, // oldest end, like the server's buffer
		`document.querySelector(".kind[aria-pressed='true']")`,
		`el.flagcount`, // count visible from either view
	} {
		if !strings.Contains(source, want) {
			t.Errorf("log view missing %q", want)
		}
	}
	// The views are one client-side state rendered two ways. A router or a second
	// document would tear down the page, which ends the call.
	for _, forbidden := range []string{
		`location.hash`,
		`window.location =`,
		`history.pushState`,
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("view switching became navigation: %q", forbidden)
		}
	}
}

func TestPageKeepsTheControlsOutsideBothViews(t *testing.T) {
	source := page(t)
	// The controls have to sit outside the view container: a control that
	// disappears when you switch view is the same defect as one labelled with its
	// action, it makes the reader hold state the interface should show.
	// The views container closes before the controls open, so the controls are its
	// sibling rather than a child of either view.
	if !strings.Contains(source, "      </div>\n    </div>\n\n    <footer class=\"controls\">") {
		t.Error("the session controls are no longer a sibling of the views container")
	}
	if !strings.Contains(source, `id="view-conversation"`) || !strings.Contains(source, `id="view-logs"`) {
		t.Error("expected exactly the two views")
	}
	if strings.Count(source, `class="view"`) != 2 {
		t.Errorf("found %d views, want 2", strings.Count(source, `class="view"`))
	}
}
