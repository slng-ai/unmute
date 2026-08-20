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
	"regexp"
	"strings"
	"testing"
)

// hexColour matches a CSS hex colour, which is the form a stray literal takes.
var hexColour = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)

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

func TestTurnsCarryTheirOwnTimings(t *testing.T) {
	source := page(t)
	for _, want := range []string{
		`function attachRecord(rec)`,
		`if (botTurnEl) renderTurnTiming(botTurnEl, rec)`, // nearest in time, no id plumbed through
		`else heldRecord = rec`,                           // a record can beat its transcript row
		`claimHeldRecord(botTurnEl)`,
		`heldRecord = null`,                       // a new conversation drops a stale one
		`turn.parentNode.insertBefore(row, turn)`, // tools sit above the reply they preceded
		`amount.textContent = secs(value)`,        // unreported renders as a dash
	} {
		if !strings.Contains(source, want) {
			t.Errorf("per-turn timings missing %q", want)
		}
	}
	// secs() is the single place a number becomes text, and it must never invent
	// a zero for something a target did not report.
	if !strings.Contains(source, `function secs(v){ return v == null ? "—" : v.toFixed(2) + "s"; }`) {
		t.Error("secs() no longer renders an absent value as a dash")
	}
	// A session record describes the run, not a turn, so it must not land on one.
	if !strings.Contains(source, `if (!rec || rec.kind !== "turn") return;`) {
		t.Error("attachRecord no longer rejects non-turn records")
	}
}

// The design constraints for this page are enforceable, so they are enforced:
// it ships inside the binary and has to work with every external request blocked.
func TestPageStaysSelfContained(t *testing.T) {
	source := page(t)
	for _, forbidden := range []string{
		"https://",
		"http://cdn",
		"@import url(",
		"fonts.googleapis.com",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("page reaches outside the binary: %q", forbidden)
		}
	}
	// Colour lives in the token block at the top. New elements reuse it rather
	// than introducing literals further down.
	tokens := strings.Index(source, ":root{")
	end := strings.Index(source, "*{box-sizing:border-box}")
	if tokens < 0 || end < 0 {
		t.Fatal("token block moved; update this test deliberately")
	}
	body := source[end:]
	if hits := hexColour.FindAllString(body, -1); len(hits) > 0 {
		t.Errorf("colour literals outside the token block: %v", hits)
	}
	for _, fn := range []string{"rgb(", "rgba(", "hsl("} {
		if strings.Contains(body, fn) {
			t.Errorf("colour function %q outside the token block", fn)
		}
	}
	// One oklch() survives below the tokens, the accent hover. It predates this
	// feature; the count is pinned so a second one has to be a decision rather
	// than a drift.
	if got := strings.Count(body, "oklch("); got != 1 {
		t.Errorf("found %d oklch() literals below the token block, want the 1 known hover", got)
	}
}
