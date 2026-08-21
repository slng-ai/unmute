package cli

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slng-ai/unmute/internal/target"
)

// The media websocket plane's gates, M1 to M6 in contracts/local-planes.md.
//
// The fake agent below is the whole reason these are worth running: it accepts
// the upgrade, checks the signature, speaks the protocol back, and can be told
// to barge in. A stand-in tested only against its own assertions would prove
// that the code agrees with itself.

// --- the known answer -------------------------------------------------------

// The signature the emitted agent validates is computed by twilio-python's
// RequestValidator, so the only test worth having is against that
// implementation's output. These two vectors were produced by twilio 9.11.0
// (`RequestValidator.compute_signature`) on 2026-08-20 and are pasted verbatim.
// If twilioSignature ever disagrees with them it disagrees with the library the
// agent calls, and every call would 403.
func TestTwilioSignatureMatchesTheLibraryTheAgentCalls(t *testing.T) {
	const token = "plane-token-abc123"
	form := url.Values{
		"CallSid":    {"CA0123456789abcdef"},
		"From":       {"+15550000001"},
		"To":         {"+15550000002"},
		"Direction":  {"inbound"},
		"CallStatus": {"ringing"},
	}
	if got, want := twilioSignature("http://127.0.0.1:7860/telephony/inbound", form, token),
		"d2UiDKFGt+7k7YG4vq6ns5SdhkI="; got != want {
		t.Errorf("form signature = %q, want %q (twilio-python 9.11.0)", got, want)
	}
	// The stream is signed with no parameters at all, over the wss:// form.
	if got, want := twilioSignature("wss://127.0.0.1:7860/telephony/ws/tok-xyz", nil, token),
		"D59Pm82ec1+L+rEXfKWabjw4WRs="; got != want {
		t.Errorf("stream signature = %q, want %q (twilio-python 9.11.0)", got, want)
	}
}

// The handshake constant, against the vector RFC 6455 publishes in section 1.3.
//
// This test exists because its absence cost a live debugging session. wsGUID was
// first transcribed from memory with two characters wrong, and the fake agent
// below shares the same constant, so the client and the double agreed with each
// other and every test passed. The failure only appeared against uvicorn, as a
// mismatched Sec-WebSocket-Accept with nothing to say about why.
//
// A shared constant cannot be checked by two things that both read it. It needs
// an external answer, and the specification supplies one.
func TestWebsocketAcceptMatchesTheRFCVector(t *testing.T) {
	const key = "dGhlIHNhbXBsZSBub25jZQ=="
	const want = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	digest := sha1.Sum([]byte(key + wsGUID))
	if got := base64.StdEncoding.EncodeToString(digest[:]); got != want {
		t.Errorf("accept for the RFC 6455 example key is %q, want %q: wsGUID is wrong, and every "+
			"handshake against a real server will be refused", got, want)
	}
}

// --- gate M1 ----------------------------------------------------------------

// M1: the stand-in speaks exactly the surface the emitted agent uses, and no
// more. The surface is the pinned SDK's, because the emitted code hands the
// socket to the SDK rather than parsing anything itself, so this asserts against
// what was read out of that version and names the version it came from.
//
// Drift fails in both directions on purpose. A message the stand-in sends that
// the agent cannot read is a broken call; a message it does not send that the
// agent needs is a call that never starts; and a message it implements that
// nothing sends is code with no reason to exist.
func TestMediaCarrierSpeaksExactlyTheAgentsProtocolSurface(t *testing.T) {
	if mediaCarrierSDKVersion != "1.7.0" {
		t.Fatalf("the protocol surface in this file was read from pipecat-ai 1.7.0 but the code "+
			"now claims %s. Re-read parse_telephony_websocket and the carrier serializers, then "+
			"update both.", mediaCarrierSDKVersion)
	}
	// Read from pipecat-ai 1.7.0: TwilioFrameSerializer.deserialize handles
	// exactly `media` and `dtmf`; serialize emits `media` and, on an
	// interruption, `clear`. `connected` and `start` are consumed above it by
	// parse_telephony_websocket, and `stop` is what a carrier sends at the end.
	wantToAgent := map[string]bool{"connected": true, "start": true, "media": true, "dtmf": true, "stop": true}
	wantFromAgent := map[string]bool{"media": true, "clear": true}

	for _, event := range mediaCarrierEvents.ToAgent {
		if !wantToAgent[event] {
			t.Errorf("the stand-in sends %q, which the pinned agent does not read", event)
		}
		delete(wantToAgent, event)
	}
	for event := range wantToAgent {
		t.Errorf("the pinned agent reads %q and the stand-in never sends it", event)
	}
	for _, event := range mediaCarrierEvents.FromAgent {
		if !wantFromAgent[event] {
			t.Errorf("the stand-in handles %q, which the pinned agent never sends", event)
		}
		delete(wantFromAgent, event)
	}
	for event := range wantFromAgent {
		t.Errorf("the pinned agent sends %q and the stand-in ignores it", event)
	}
	// `mark` is the one the carrier's own documentation lists and this serializer
	// does not use. It stays out until something sends one.
	for _, event := range append(mediaCarrierEvents.ToAgent, mediaCarrierEvents.FromAgent...) {
		if event == "mark" {
			t.Error("the stand-in implements `mark`, which the pinned serializer neither sends nor reads")
		}
	}
}

// M1, second half: the handshake carries each carrier's own discriminator keys.
// The framework detects the carrier from the shape of these two messages, so a
// renamed key is a call detected as the wrong carrier or as none at all. The
// key names come from _detect_transport_type_from_message.
func TestMediaHandshakeCarriesEachCarriersDiscriminator(t *testing.T) {
	cases := []struct {
		carrier string
		// probe returns the value the framework's detector reads, so a rename
		// anywhere on the path fails rather than just a missing top-level key.
		probe func(start map[string]any, message map[string]any) []any
	}{
		{"twilio", func(start, message map[string]any) []any {
			return []any{message["event"], start["streamSid"], start["callSid"]}
		}},
		{"telnyx", func(start, message map[string]any) []any {
			return []any{message["stream_id"], start["call_control_id"]}
		}},
		{"plivo", func(start, message map[string]any) []any {
			return []any{start["streamId"], start["callId"]}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.carrier, func(t *testing.T) {
			messages, err := mediaHandshake(tc.carrier, "MZstream", "CAcall", "+15550000001", "+15550000002")
			if err != nil {
				t.Fatal(err)
			}
			if len(messages) != 2 {
				t.Fatalf("handshake is %d messages, want 2: the framework reads two and warns on one", len(messages))
			}
			if messages[0]["event"] != "connected" {
				t.Errorf("first message is %v, want the connected event", messages[0]["event"])
			}
			start, ok := messages[1]["start"].(map[string]any)
			if !ok {
				t.Fatalf("second message carries no start object: %v", messages[1])
			}
			for i, value := range tc.probe(start, messages[1]) {
				if value == nil || value == "" {
					t.Errorf("discriminator %d is empty, so the framework cannot detect %s", i, tc.carrier)
				}
			}
		})
	}
	// Exotel is detected by the framework and refused by this repository, so the
	// stand-in must say so rather than sending a handshake nothing recognises.
	if _, err := mediaHandshake("exotel", "s", "c", "f", "t"); err == nil {
		t.Error("mediaHandshake accepted exotel, which has no emitted adapter here")
	}
}

// --- gate M2 ----------------------------------------------------------------

// M2: mu-law at 8000 Hz mono in 20 ms frames, round-tripped against a fixture.
// A stand-in that ships anything else is not standing in for a carrier.
func TestMediaAudioIsMulaw8kIn20msFrames(t *testing.T) {
	samples := callFixtureSamples(1)
	if len(samples) != callAudioRate {
		t.Fatalf("one second of fixture is %d samples, want %d", len(samples), callAudioRate)
	}
	payload := pcmToMulaw(samples)
	if len(payload) != len(samples) {
		t.Fatalf("mu-law payload is %d bytes for %d samples; one sample is one byte", len(payload), len(samples))
	}
	frames := mulawFrames(payload)
	if want := callAudioRate / callAudioFrameSamples; len(frames) != want {
		t.Fatalf("one second is %d frames, want %d at 20 ms", len(frames), want)
	}
	for i, frame := range frames {
		if len(frame) != callAudioFrameSamples {
			t.Fatalf("frame %d is %d bytes, want %d (20 ms of mu-law)", i, len(frame), callAudioFrameSamples)
		}
	}
	// The round trip is lossy by one quantisation step, which is the codec, not
	// a bug. What must survive is the signal: a loud fixture stays loud.
	restored := mulawToPCM(payload)
	var peak int
	for _, sample := range restored {
		if value := int(sample); value > peak {
			peak = value
		}
	}
	if peak < 8000 {
		t.Errorf("round-tripped fixture peaks at %d, which is too quiet to tell audio from silence", peak)
	}
	if mediaFrameInterval != 20*time.Millisecond {
		t.Errorf("frame interval is %s, want 20ms", mediaFrameInterval)
	}
}

// --- the fake agent ---------------------------------------------------------

// fakeAgent is an agent-shaped peer: it validates the signature the emitted
// agent validates, answers the inbound webhook with the markup the emitted
// agent answers with, and speaks the protocol on the stream.
type fakeAgent struct {
	token string
	// reply is the audio the agent sends once the stream opens, in frames.
	replyFrames int
	// bargeInAfter sends a clear once this many frames have arrived from the
	// caller. Zero never barges in.
	bargeInAfter int
	// markup overrides the answer to the inbound webhook, for the failure cases.
	markup string
	// rejectSignature makes the agent behave like the real one does when the
	// signature is wrong, which is the failure most worth being able to see.
	rejectSignature bool
	// transferAfter posts a cold transfer document once this many caller frames
	// have arrived, the way the emitted agent does when the model calls the
	// tool. Zero never transfers. Needs control set, because a transfer on this
	// route is an HTTP request to the carrier and not a message on the stream.
	transferAfter int
	control       *mediaControl

	server *httptest.Server
	// observed is what the agent received, for the assertions.
	observedStart chan map[string]any
	// observedCallID is this call's id as the carrier gave it, which is what a
	// transfer has to be addressed to. Written from the stream goroutine and
	// read from it, never from the test's own goroutine.
	observedCallID string
	// streamEnded is when the agent's media stream closed. The only way to see
	// a transfer actually cut the caller's leg: a run that merely records having
	// cut it looks identical from the outside, and did, until this existed.
	streamEnded chan time.Time
	framesIn    chan int
}

func newFakeAgent(t *testing.T, agent *fakeAgent) *fakeAgent {
	t.Helper()
	agent.observedStart = make(chan map[string]any, 1)
	agent.framesIn = make(chan int, 1)
	agent.streamEnded = make(chan time.Time, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/telephony/inbound", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if agent.rejectSignature || r.Header.Get("X-Twilio-Signature") == "" {
			http.Error(w, "invalid carrier signature", http.StatusForbidden)
			return
		}
		markup := agent.markup
		if markup == "" {
			// The shape the emitted _stream_twiml produces: a wss:// address
			// built from the agent's own public URL, which is not where the
			// stand-in will dial.
			markup = fmt.Sprintf(`<Response><Connect><Stream url="wss://%s/telephony/ws/tok-xyz" /></Connect></Response>`,
				"agent.example:443")
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, markup)
	})
	mux.HandleFunc("/telephony/ws/", func(w http.ResponseWriter, r *http.Request) {
		if agent.rejectSignature {
			http.Error(w, "invalid carrier signature", http.StatusForbidden)
			return
		}
		conn, reader, err := upgradeFake(w, r)
		if err != nil {
			t.Errorf("fake agent could not accept the upgrade: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		agent.serveStream(conn, reader)
	})
	agent.server = httptest.NewServer(mux)
	t.Cleanup(agent.server.Close)
	return agent
}

// serveStream reads the handshake, then answers audio and optionally barges in.
func (a *fakeAgent) serveStream(conn net.Conn, reader *bufio.Reader) {
	sent := false
	frames := 0
	for {
		payload, err := readFakeFrame(reader)
		if err != nil {
			select {
			case a.framesIn <- frames:
			default:
			}
			select {
			case a.streamEnded <- time.Now():
			default:
			}
			return
		}
		var message map[string]any
		if json.Unmarshal(payload, &message) != nil {
			continue
		}
		switch message["event"] {
		case "start":
			if start, ok := message["start"].(map[string]any); ok {
				if id, ok := start["callSid"].(string); ok {
					a.observedCallID = id
				}
			}
			select {
			case a.observedStart <- message:
			default:
			}
		case "media":
			frames++
			if !sent {
				sent = true
				// Answer with audio, all at once, the way a TTS burst arrives.
				for i := 0; i < a.replyFrames; i++ {
					frame := pcmToMulaw(callFixtureSamples(1)[:callAudioFrameSamples])
					_ = writeFakeFrame(conn, map[string]any{
						"event":     "media",
						"streamSid": "MZstream",
						"media":     map[string]any{"payload": base64.StdEncoding.EncodeToString(frame)},
					})
				}
			}
			if a.bargeInAfter > 0 && frames == a.bargeInAfter {
				_ = writeFakeFrame(conn, map[string]any{"event": "clear", "streamSid": "MZstream"})
			}
			if a.transferAfter > 0 && frames == a.transferAfter && a.control != nil {
				// The emitted agent's own move: replace the live call's document
				// at the carrier. Its call id came from the start message, which
				// is where the real one reads it too.
				a.postTransfer(a.callIDFrom(message))
			}
		}
	}
}

// callIDFrom finds this call's id the way the emitted agent does: out of the
// start message the carrier sent, which the agent has kept since.
func (a *fakeAgent) callIDFrom(map[string]any) string {
	start := a.observedCallID
	if start == "" {
		return "CAunknown"
	}
	return start
}

// postTransfer is the emitted cold transfer, made from the double so the
// stand-in is exercised by something shaped like its real client: one form POST
// to the carrier's call-control endpoint, and nothing on the media stream.
func (a *fakeAgent) postTransfer(callID string) {
	document := `<Response>` +
		`<Say>Connecting you to a colleague now.</Say>` +
		`<Dial answerOnBridge="true" timeout="30">+15551234567</Dial>` +
		`<Hangup/>` +
		`</Response>`
	form := url.Values{"Twiml": {document}}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		a.control.base+carrierCallPath+"ACtest/Calls/"+callID+".json",
		strings.NewReader(form.Encode()))
	if err != nil {
		return
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// The stand-in authenticates, so the double presents the run's credentials
	// the way the SDK and the raw call-control request both do.
	request.SetBasicAuth(testControlSID, testControlToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return
	}
	_ = response.Body.Close()
}

// upgradeFake completes the server side of the websocket handshake by hijacking
// the connection. Hand-rolled for the same reason the client is: one known
// peer, small text frames, no dependency worth adding for a test double.
func upgradeFake(w http.ResponseWriter, r *http.Request) (net.Conn, *bufio.Reader, error) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer cannot be hijacked")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, nil, fmt.Errorf("no Sec-WebSocket-Key")
	}
	conn, buffered, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}
	digest := sha1.Sum([]byte(key + wsGUID))
	response := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(digest[:]) + "\r\n\r\n"
	if _, err := conn.Write([]byte(response)); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return conn, buffered.Reader, nil
}

// readFakeFrame reads one masked client text frame.
func readFakeFrame(reader *bufio.Reader) ([]byte, error) {
	var head [2]byte
	if _, err := io.ReadFull(reader, head[:]); err != nil {
		return nil, err
	}
	if head[0]&0x0F == 0x8 {
		return nil, io.EOF
	}
	length := int(head[1] & 0x7F)
	switch length {
	case 126:
		var extended [2]byte
		if _, err := io.ReadFull(reader, extended[:]); err != nil {
			return nil, err
		}
		length = int(binary.BigEndian.Uint16(extended[:]))
	case 127:
		var extended [8]byte
		if _, err := io.ReadFull(reader, extended[:]); err != nil {
			return nil, err
		}
		length = int(binary.BigEndian.Uint64(extended[:]))
	}
	var mask [4]byte
	if head[1]&0x80 != 0 {
		if _, err := io.ReadFull(reader, mask[:]); err != nil {
			return nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	if head[1]&0x80 != 0 {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return payload, nil
}

// writeFakeFrame writes one unmasked server text frame, which is what a server
// must send.
func writeFakeFrame(conn net.Conn, message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	header := []byte{0x81}
	switch length := len(payload); {
	case length < 126:
		header = append(header, byte(length))
	default:
		header = append(header, 126, 0, 0)
		binary.BigEndian.PutUint16(header[2:4], uint16(length))
	}
	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err = conn.Write(payload)
	return err
}

// runFor builds a stand-in pointed at this agent.
func (a *fakeAgent) standIn(t *testing.T) mediaCarrierRun {
	t.Helper()
	return mediaCarrierRun{
		carrier: "twilio",
		// What the agent believes its address is, and therefore signs over. On
		// purpose not the dial address: that difference is the whole reason the
		// signature has to be computed over a URL nothing connects to.
		publicURL:      "http://agent.example",
		dial:           a.server.URL,
		authToken:      a.token,
		inboundPath:    "/telephony/inbound",
		callDir:        t.TempDir(),
		fixtureSeconds: 2,
		// Never the machine's microphone. A test that opened an audio device
		// would pass or fail on whether this machine has one and on what the
		// room sounds like, and would hold the device away from whoever is
		// using it. The unattended check sets this for the same reason.
		forceFixture: true,
	}
}

// --- gate M3 ----------------------------------------------------------------

// M3: the stand-in reaches an agent and completes a call, webhook through
// stream through audio in both directions. This is the gate that the plane
// exists at all.
func TestMediaCarrierCompletesACallAgainstAnAgent(t *testing.T) {
	agent := newFakeAgent(t, &fakeAgent{token: "plane-token", replyFrames: 5})
	run := agent.standIn(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := run.call(ctx, "+15550000001", "+15550000002", 400*time.Millisecond)
	if err != nil {
		t.Fatalf("the call did not complete: %v", err)
	}
	if result.BytesToAgent == 0 {
		t.Error("no audio reached the agent, so the caller was silent")
	}
	if result.BytesFromAgent == 0 {
		t.Error("no audio came back from the agent, so the caller heard nothing")
	}
	// The handshake has to have carried the call, or the agent has no context.
	select {
	case start := <-agent.observedStart:
		inner, _ := start["start"].(map[string]any)
		params, _ := inner["customParameters"].(map[string]any)
		if params["from_number"] != "+15550000001" {
			t.Errorf("the agent saw from_number %v, want the caller the stand-in dialled with", params["from_number"])
		}
	default:
		t.Error("the agent never received a start message, so no call was opened")
	}
	// Both legs are on disk, and the caller's own leg carried real audio.
	for _, path := range []string{result.CallerSpoke, result.CallerHeard} {
		if filepath.Dir(path) != run.callDir {
			t.Errorf("recording %s is not in the run's call directory", path)
		}
	}
	samples, rate, err := readWAV(result.CallerSpoke)
	if err != nil {
		t.Fatalf("the caller's recording is unreadable: %v", err)
	}
	if rate != callAudioRate {
		t.Errorf("recording is at %d Hz, want %d", rate, callAudioRate)
	}
	if len(samples) == 0 {
		t.Error("the caller's recording is empty even though frames were sent")
	}
}

// M3, the failure worth naming: a wrong signature is the most likely
// misconfiguration and the least obvious, so the stand-in has to say what it is
// rather than reporting a bare 403.
func TestMediaCarrierNamesASignatureRejection(t *testing.T) {
	agent := newFakeAgent(t, &fakeAgent{token: "plane-token", rejectSignature: true})
	run := agent.standIn(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := run.call(ctx, "+15550000001", "+15550000002", 100*time.Millisecond)
	if err == nil {
		t.Fatal("the call succeeded against an agent that rejects every signature")
	}
	if !strings.Contains(err.Error(), "signature") {
		t.Errorf("the error does not mention the signature, so the cause is hidden: %v", err)
	}
}

// M3: an agent that answers the webhook with markup carrying no stream address
// is a broken package, and the stand-in must say that rather than dialling
// nothing.
func TestMediaCarrierRefusesMarkupWithNoStreamAddress(t *testing.T) {
	agent := newFakeAgent(t, &fakeAgent{token: "plane-token", markup: `<Response><Say>no stream here</Say></Response>`})
	run := agent.standIn(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := run.call(ctx, "+15550000001", "+15550000002", 100*time.Millisecond)
	if err == nil {
		t.Fatal("the stand-in accepted markup with no stream address")
	}
	if !strings.Contains(err.Error(), "stream address") {
		t.Errorf("the error does not say what was missing: %v", err)
	}
}

// --- gate M4 ----------------------------------------------------------------

// M4: barge-in is honoured. When the agent sends `clear`, audio it had queued
// but the caller had not yet reached is dropped rather than played out.
//
// Without this the stand-in would be a *better* carrier than a real one, and
// turn-taking bugs, which is most of what a phone agent gets wrong, would not
// reproduce locally at all.
func TestMediaCarrierDropsQueuedAudioOnBargeIn(t *testing.T) {
	// Differential, and it has to be: a single run cannot tell a dropped queue
	// from a queue the call simply had no time to play out. The play-out is one
	// frame per 20 ms, so a short call always ends with audio still queued,
	// whether or not anything cleared it. The first version of this test
	// asserted "heard fewer frames than the agent sent" and passed with the
	// barge-in deleted, which is a gate that proves nothing.
	//
	// So the same call runs twice against the same agent, once told to barge in
	// and once not, and the barge-in run has to hear strictly less.
	// The agent answers in one synchronous burst, and it has to finish that
	// burst and read the caller's second frame before the call ends, or it never
	// gets to the point where it barges in. Both numbers are sized for that with
	// room to spare: a frame takes milliseconds to move under the race detector,
	// which is around ten times what it takes without one, and at 200 frames and
	// half a second this test spent the whole call still draining the burst and
	// reported a barge-in that never happened.
	const replyFrames = 60
	const duration = time.Second
	play := func(bargeInAfter int) mediaCallResult {
		t.Helper()
		agent := newFakeAgent(t, &fakeAgent{
			token: "plane-token", replyFrames: replyFrames, bargeInAfter: bargeInAfter,
		})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result, err := agent.standIn(t).call(ctx, "+15550000001", "+15550000002", duration)
		if err != nil {
			t.Fatalf("the call did not complete: %v", err)
		}
		heard, _, err := readWAV(result.CallerHeard)
		if err != nil {
			t.Fatalf("what the caller heard is unreadable: %v", err)
		}
		result.FramesHeard = len(heard) / callAudioFrameSamples
		return result
	}

	control := play(0)
	bargedIn := play(2)

	if bargedIn.Clears == 0 {
		t.Fatal("the stand-in never saw the clear event, so barge-in was not exercised")
	}
	if control.FramesHeard == 0 {
		t.Fatal("the caller heard nothing even without a barge-in, so this measures nothing")
	}
	if bargedIn.FramesHeard >= control.FramesHeard {
		t.Errorf("the caller heard %d frames with a barge-in and %d without: the clear dropped nothing",
			bargedIn.FramesHeard, control.FramesHeard)
	}
}

// --- gate M6 ----------------------------------------------------------------

// M6: the stand-in cannot make a route look more capable than it is. The
// carrier-websocket routes have no transfer control at all, which is a fact of
// the transport rather than a gap in the emitted code, so no transfer behaviour
// may be reachable for them.
func TestMediaCarrierOffersNoTransferOnARouteWithout(t *testing.T) {
	routes := target.TelephonyRoutes()
	for _, carrier := range []string{"twilio", "telnyx", "plivo"} {
		key := target.TelephonyKey{Provider: target.Pipecat, Transport: "carrier-websocket", Carrier: carrier}
		route, ok := routes[key]
		if !ok {
			t.Fatalf("the (pipecat, carrier-websocket, %s) route is gone from the table", carrier)
		}
		if route.LocalPlane != target.LocalPlaneMediaWebsocket {
			t.Fatalf("the (pipecat, carrier-websocket, %s) route is on the %s plane, so this gate is "+
				"asserting the wrong thing", carrier, route.LocalPlane)
		}
		for _, control := range []target.TelephonyControl{target.ColdTransfer, target.WarmTransfer} {
			if _, declared := route.Features[target.TelephonyFeature(control)]; declared {
				t.Errorf("the (pipecat, carrier-websocket, %s) route now declares %s. If that is real, "+
					"the stand-in needs a call-control route for it and this gate needs rewriting.",
					carrier, control)
			}
		}
	}
}

// The plane's values have to win over whatever is already in the author's
// environment. A developer testing this locally almost certainly has a real
// TWILIO_AUTH_TOKEN in .env, and the stand-in signs with the plane's token: if
// the agent were handed the real one instead, every call would 403 and the cause
// would be invisible. Same gate the SIP plane carries for the same reason.
func TestMediaPlaneOverridesRealCarrierValuesAlreadyInTheEnvironment(t *testing.T) {
	plan := planForRoute(target.TelephonyRoutes()[target.TelephonyKey{
		Provider: target.Pipecat, Transport: "carrier-websocket", Carrier: "twilio",
	}])
	// The connection's vocabulary, which is what a compiled package carries and
	// what the plane replaces. planForRoute leaves it empty, and an empty one is
	// the case the plane now refuses rather than running credential-free in name
	// only.
	plan.Environment = map[string]string{
		"account_sid": "TWILIO_ACCOUNT_SID",
		"auth_token":  "TWILIO_AUTH_TOKEN",
		"from_number": "TWILIO_PHONE_NUMBER",
	}
	run, err := startMediaPlaneRun(plan, "7860", false)
	if err != nil {
		t.Fatal(err)
	}
	// What a real .env looks like.
	env := []string{
		"TWILIO_ACCOUNT_SID=AC_the_authors_real_account",
		"TWILIO_AUTH_TOKEN=the_authors_real_token",
		"TWILIO_PHONE_NUMBER=+441234567890",
		"UNMUTE_PUBLIC_URL=https://tunnel.example",
	}
	applied := run.apply(env)
	values := map[string]string{}
	for _, entry := range applied {
		if name, value, ok := strings.Cut(entry, "="); ok {
			values[name] = value
		}
	}
	if values["TWILIO_AUTH_TOKEN"] != run.token {
		t.Errorf("the agent would validate with %q while the stand-in signs with the plane's token: "+
			"every call would be refused with 403", values["TWILIO_AUTH_TOKEN"])
	}
	for _, name := range []string{"TWILIO_ACCOUNT_SID", "TWILIO_PHONE_NUMBER"} {
		if strings.Contains(values[name], "real") || strings.Contains(values[name], "441234567890") {
			t.Errorf("%s is still the author's own value %q, so the run is not carrier-free", name, values[name])
		}
	}
	// The public URL must be loopback: a leftover tunnel address here means the
	// agent signs over a host the stand-in is not dialling.
	//
	// And it must be https, which is the emitted agent's own rule for a public
	// URL. Nothing serves it: the value only ever becomes the string both sides
	// sign over and the wss:// address the agent hands back, while the socket
	// goes to plain ws on loopback. With http:// here the agent refuses to become
	// healthy and the whole run tears itself down, which is how this was found.
	if !strings.HasPrefix(values["UNMUTE_PUBLIC_URL"], "https://127.0.0.1:") {
		t.Errorf("UNMUTE_PUBLIC_URL is %q, want an https loopback origin: the agent refuses "+
			"anything else and signs over this exact string", values["UNMUTE_PUBLIC_URL"])
	}
	// The selector carries the stand-in's address, which the agent reads twice:
	// set at all means build the carrier transport directly, and its value is
	// where a cold transfer's document is posted. Loopback on both counts, or a
	// transfer is a write leaving this machine (gate P2).
	if !strings.HasPrefix(values[target.LocalPlaneEnvName], "http://127.0.0.1:") {
		t.Errorf("%s is %q, want the stand-in's loopback address: the agent takes the framework's "+
			"transport path without it and posts call control to api.twilio.com",
			target.LocalPlaneEnvName, values[target.LocalPlaneEnvName])
	}
}

// The refusal half of the same rule: a package whose connection names a
// credential the plane has no value for must stop, not run with the author's own.
func TestMediaPlaneRefusesWhenItCannotReplaceACredential(t *testing.T) {
	plan := planForRoute(target.TelephonyRoutes()[target.TelephonyKey{
		Provider: target.Pipecat, Transport: "carrier-websocket", Carrier: "twilio",
	}])
	plan.Environment = map[string]string{
		"account_sid": "TWILIO_ACCOUNT_SID",
		"auth_token":  "TWILIO_AUTH_TOKEN",
		"from_number": "TWILIO_PHONE_NUMBER",
		// A key no plane value covers.
		"regional_edge_token": "TWILIO_EDGE_TOKEN",
	}
	_, err := startMediaPlaneRun(plan, "7860", false)
	if err == nil {
		t.Fatal("the plane started without being able to replace TWILIO_EDGE_TOKEN, so the run " +
			"would have used the author's own carrier credentials")
	}
	if !strings.Contains(err.Error(), "TWILIO_EDGE_TOKEN") {
		t.Errorf("the refusal does not name the credential it could not supply: %v", err)
	}
}
