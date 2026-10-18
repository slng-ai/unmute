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
		`MIC OFF`,                        // no call yet
		`NO MIC · TEXT ONLY`,             // a text-only call, on purpose
		`NO MIC · BLOCKED`,               // permission refused
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

func TestPageKeepsTheControlsOutsideEveryView(t *testing.T) {
	source := page(t)
	// The controls have to sit outside the view container: a control that
	// disappears when you switch view is the same defect as one labelled with its
	// action, it makes the reader hold state the interface should show.
	// The controls close right before the views container opens, so they are its
	// sibling rather than a child of any view.
	controls := strings.Index(source, `<section class="controls"`)
	views := strings.Index(source, `<div class="views" id="views">`)
	if controls < 0 || views < 0 || controls > views ||
		strings.Count(source[controls:views], "<section") != 1 ||
		!strings.HasSuffix(source[:views], "    </section>\n\n    ") {
		t.Error("the session controls are no longer a sibling of the views container")
	}
	for _, view := range []string{`id="view-conversation"`, `id="view-metrics"`, `id="view-logs"`} {
		if !strings.Contains(source, view) {
			t.Errorf("view missing: %s", view)
		}
	}
	if strings.Count(source, `class="view"`) != 3 {
		t.Errorf("found %d views, want 3", strings.Count(source, `class="view"`))
	}
}

// The call's state reaches the page as records the agent prints and fields the
// session names. The smoke replay drives both through a browser; this holds the
// wiring in place for the suite that needs no Node.
func TestPageShowsTheCallState(t *testing.T) {
	source := page(t)
	for _, want := range []string{
		`session.state_fields`,                // every declared field, in state.py's order
		`record.kind==='state'`,               // each change is its own record
		`id="state-panel"`,                    // the whole state, in the inspector
		`row.dataset.kind='state'`,            // and each change, where it happened
		`if (rec.state) return prefix+'state`, // one log line per change
	} {
		if !strings.Contains(source, want) {
			t.Errorf("state view missing %q", want)
		}
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
	for _, fn := range []string{"rgb(", "rgba(", "hsl(", "oklch("} {
		if strings.Contains(body, fn) {
			t.Errorf("colour function %q outside the token block", fn)
		}
	}
}

func TestTextChatReachesTheBotThroughTheTransportsOwnChannel(t *testing.T) {
	source := page(t)
	for _, want := range []string{
		`id="chatform"`,
		`id="chatinput"`,
		`id="chatsend"`,
		`id="textonly"`,
		`type:"send-text"`,                 // the RTVI client message Pipecat reads
		`run_immediately:true`,             // the typed line is the caller's turn now
		`audio_response:true`,              // the bot answers by voice
		`sendText(text,{topic:"lk.chat"})`, // the LiveKit agent's chat topic
		`const sent = stream || silentStream();`, // text only sends silence: a live model answers nothing without audio
	} {
		if !strings.Contains(source, want) {
			t.Errorf("text chat missing %q", want)
		}
	}
	// The echo is a placeholder that the bot's own record replaces.
	if !strings.Contains(source, "function dropEcho(record)") || !strings.Contains(source, "pendingEchoes") {
		t.Error("a typed line's echo must give way to a matching record")
	}
	// A text-only call must not reach for the microphone: LiveKit publishes silence instead.
	if !strings.Contains(source, "if (textOnly) {") || !strings.Contains(source, "publishTrack(track,{source:LK.Track.Source.Microphone})") {
		t.Error("LiveKit text only must publish silence instead of enabling the microphone")
	}
}

// While the agent starts, the conversation view is a start screen named in words
// that fit both targets, the typed line sits under the conversation rather than
// in the call bar, and a reply the model made more than once is not rated.
func TestPageStartsOnAStartScreenAndTypesUnderTheConversation(t *testing.T) {
	source := page(t)
	for _, want := range []string{
		`setRunState(ev.state, ev.step)`, // the CLI names the step
		`const BOOT_STEP = {1:"Starting the agent", 2:"Waiting for the agent to answer"}`,
		`function unratedReply(ops)`,
		`reply_latency:      {slow:900,  bad:1200`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("page missing %q", want)
		}
	}
	boot := source[strings.Index(source, `<section class="boot"`):strings.Index(source, `<section class="stage">`)]
	for _, framework := range []string{"LiveKit", "Pipecat", "worker", "container"} {
		if strings.Contains(boot, framework) {
			t.Errorf("start screen names one target's internals: %q", framework)
		}
	}
	conversation := strings.Index(source, `id="view-conversation"`)
	metrics := strings.Index(source, `id="view-metrics"`)
	form := strings.Index(source, `<form id="chatform"`)
	if form < conversation || form > metrics {
		t.Error("the typed line must sit inside the conversation view")
	}
}
