// These Go-only checks guard the shipped page's protocol and asset boundaries.
// They do not execute JavaScript. TestSmokeDevUIStreaming runs the unchanged
// page in a real browser behind the smoke tag; the normal suite needs no Node.
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

func TestPipecatKeepsReadinessAndForwardsTheCallID(t *testing.T) {
	source := page(t)
	for _, want := range []string{
		`type:"client-ready"`,
		`version:"2.0.0"`,
		`request_data`,
		`unmute_dev_call_id`,
		`session.call_id`,
		`session.dev_events_version`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("dev call bootstrap/readiness missing %q", want)
		}
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
	// document, must preventDefault and must ignore auto-repeat. Native transcript
	// controls keep keyboard activation; the browser smoke checks both paths.
	for _, want := range []string{
		`document.addEventListener("keydown"`,
		`document.addEventListener("keyup"`,
		`e.preventDefault()`,
		`e.repeat`,
		`typingTarget(e.target)`,
		`.transcript summary,.transcript button`,
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
		`await waitForPeer(peer);`,
		`clientReadyTimer=setInterval(sendClientReady,500)`,
		`msg.type === "bot-ready"`,
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

func TestTextSegmentsCarryTheirOwnFinality(t *testing.T) {
	source := page(t)
	for _, want := range []string{
		`message_id`,
		`separator_before`,
		`dataset.state`,
		`provisional`,
		`incomplete`,
		`textContent`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("identified text/finality wire missing %q", want)
		}
	}
	for _, retired := range []string{`mergeFinished`, `userFinished`, `closeUserTurn`} {
		if strings.Contains(source, retired) {
			t.Errorf("transcript still depends on word guesses or agent-driven finality: %q", retired)
		}
	}
}

func TestDevRecordsUseIdentityInsteadOfTheNewestReply(t *testing.T) {
	source := page(t)
	for _, want := range []string{`new Map()`, `call_id`, `exchange_id`, `revision`} {
		if !strings.Contains(source, want) {
			t.Errorf("dev record identity wire missing %q", want)
		}
	}
	for _, retired := range []string{`heldRecord`, `claimHeldRecord`, `if (botTurnEl) renderTurnTiming`} {
		if strings.Contains(source, retired) {
			t.Errorf("measurements still attach by newest reply: %q", retired)
		}
	}
}

// The design constraints for this page are enforceable, so they are enforced:
// it ships inside the binary and has to work with every external request blocked.
func TestPageStaysSelfContained(t *testing.T) {
	source := page(t)
	// A user-opened guide is navigation, not a runtime dependency.
	guide := `<a class="metrics-guide" href="https://unmute.ai/optimization/latency" target="_blank" rel="noopener">How to read latency ↗</a>`
	if !strings.Contains(source, guide) {
		t.Fatal("latency guide link is missing")
	}
	source = strings.Replace(source, guide, "", 1)
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
