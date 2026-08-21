package cli

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/slng-ai/unmute/internal/target"
)

// The media websocket plane: a stand-in for the carrier on the three routes
// whose carrier streams call audio over a WebSocket. It posts the agent's own
// inbound endpoint, reads the stream address out of the markup the agent
// answers with, opens the stream, and speaks the carrier's protocol over
// loopback. No tunnel, no carrier account, nothing leaving the machine.
//
// Everything here is measured against the versions internal/target/driver.go
// pins, not against the carrier's wire documentation, because the emitted agent
// does not parse the protocol itself: it hands the socket to pipecat-ai, which
// owns every byte. The R7 addendum in the feature's research.md records the
// readings and mediaCarrierSDKVersion below pins the version they came from.

// mediaCarrierSDKVersion is the SDK release the handshake and event surface in
// this file were read from. Gate M1 asserts the surface against this, so a
// version bump that moves the protocol fails a test here rather than failing a
// call at runtime.
const mediaCarrierSDKVersion = "1.7.0"

// The frame cadence a carrier sends and expects. callAudioFrameSamples in
// dev_call_audio.go is the same 20 ms expressed in samples, which in mu-law is
// also bytes.
const mediaFrameInterval = 20 * time.Millisecond

// mediaCarrierEvents is the protocol surface the emitted agent actually uses,
// which is smaller than the carrier's published protocol. Gate M1 is the rule
// that this list and the stand-in agree, in both directions.
//
// Sent to the agent: the two handshake messages, then audio. `dtmf` is here
// because the serializer deserializes it; `stop` because a carrier sends it
// when the call ends and the framework tolerates it.
//
// Read from the agent: audio, and the barge-in signal. Deliberately absent is
// `mark`: the pinned serializer never generates one and never reads one, so
// implementing it would be building for a message that cannot arrive.
var mediaCarrierEvents = struct {
	ToAgent   []string
	FromAgent []string
}{
	ToAgent:   []string{"connected", "start", "media", "dtmf", "stop"},
	FromAgent: []string{"media", "clear"},
}

// twilioSignature is Twilio's request signature, which the emitted agent
// validates on both the inbound webhook and the stream. Without it the agent
// answers 403 and there is no call to test.
//
// The algorithm, from the twilio-python RequestValidator the emitted code
// calls: the URI, then for each parameter name in sorted order the name
// followed by each of its values in sorted order, HMAC-SHA1 under the auth
// token, base64. The validator tries the URI both with and without an explicit
// port, so signing over the agent's own computed URL is enough.
func twilioSignature(uri string, params url.Values, token string) string {
	payload := uri
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		values := append([]string(nil), params[name]...)
		sort.Strings(values)
		for _, value := range values {
			payload += name + value
		}
	}
	mac := hmac.New(sha1.New, []byte(token))
	mac.Write([]byte(payload))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// mediaHandshake is the two messages that open a stream, per carrier. The
// framework detects the carrier from the *shape* of these messages rather than
// from any name in them, so the key spellings here are load-bearing: change
// `streamSid` to `stream_sid` and the same call is detected as a different
// carrier, or as none.
//
// Read from pipecat-ai's _detect_transport_type_from_message. The second
// message is what carries the call, the first is the carrier saying hello;
// providers that send only one are tolerated by the framework with a warning,
// and we always send both because every real carrier on these routes does.
func mediaHandshake(carrier, streamID, callID, from, to string) ([]map[string]any, error) {
	connected := map[string]any{"event": "connected", "protocol": "Call", "version": "1.0.0"}
	switch carrier {
	case "twilio":
		return []map[string]any{connected, {
			"event":          "start",
			"sequenceNumber": "1",
			"streamSid":      streamID,
			"start": map[string]any{
				"streamSid":   streamID,
				"callSid":     callID,
				"accountSid":  "AC" + strings.Repeat("0", 32),
				"tracks":      []string{"inbound"},
				"mediaFormat": map[string]any{"encoding": "audio/x-mulaw", "sampleRate": callAudioRate, "channels": 1},
				// The caller and callee reach the agent as stream parameters on
				// this route, not as fields of their own: the framework reads
				// them from start.customParameters.
				"customParameters": map[string]any{"from_number": from, "to_number": to},
			},
		}}, nil
	case "telnyx":
		return []map[string]any{connected, {
			"event":     "start",
			"stream_id": streamID,
			"start": map[string]any{
				"call_control_id": callID,
				"from":            from,
				"to":              to,
				// The framework passes this straight to the serializer as its
				// outbound encoding, so a wrong value here is a wrong codec on
				// the wire rather than a rejected handshake.
				"media_format": map[string]any{"encoding": "PCMU", "sample_rate": callAudioRate, "channels": 1},
			},
		}}, nil
	case "plivo":
		return []map[string]any{connected, {
			"event": "start",
			"start": map[string]any{
				"streamId": streamID,
				"callId":   callID,
				"from":     from,
				"to":       to,
				"mediaFormat": map[string]any{
					"encoding": "audio/x-mulaw", "sampleRate": callAudioRate, "channels": 1,
				},
			},
		}}, nil
	}
	// Exotel is the fourth shape the framework detects and the one route this
	// repository refuses at validation, so it cannot reach here. Naming it beats
	// a silent empty handshake if that ever changes.
	return nil, fmt.Errorf("carrier %q has no media websocket handshake; the stand-in speaks twilio, telnyx and plivo", carrier)
}

// mediaStreamAddress pulls the stream URL out of whatever markup the agent
// answered the inbound webhook with. All three carriers answer with XML
// carrying one stream element, and they disagree about its name and about
// whether the address is an attribute or the text: Twilio and Plivo use
// `<Stream url=...>` and `<Stream>wss://...</Stream>` respectively.
//
// Parsed by walking tokens rather than by unmarshalling into a struct, because
// the three documents share no schema and the only thing wanted from any of
// them is the one address.
func mediaStreamAddress(markup string) (string, error) {
	decoder := xml.NewDecoder(strings.NewReader(markup))
	inStream := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("the agent's answer is not valid XML: %w", err)
		}
		switch element := token.(type) {
		case xml.StartElement:
			if !strings.EqualFold(element.Name.Local, "Stream") {
				continue
			}
			for _, attr := range element.Attr {
				if strings.EqualFold(attr.Name.Local, "url") && attr.Value != "" {
					return attr.Value, nil
				}
			}
			inStream = true
		case xml.CharData:
			if inStream {
				if address := strings.TrimSpace(string(element)); address != "" {
					return address, nil
				}
			}
		case xml.EndElement:
			if strings.EqualFold(element.Name.Local, "Stream") {
				inStream = false
			}
		}
	}
	return "", errors.New("the agent's answer carries no stream address")
}

// --- the websocket client -------------------------------------------------
//
// Written on the standard library rather than added as a dependency, which is
// the decision research R7 records: the peer is one known server, the messages
// are small JSON text frames, and the protocol has not changed since 2011.
//
// ponytail: no fragmentation, no compression, no client-side close handshake
// beyond sending the frame. uvicorn does not fragment a small text message and
// does not negotiate permessage-deflate unless asked. If a peer ever does
// either, reach for a real websocket library rather than growing this.

// wsGUID is the constant RFC 6455 mixes into the key to prove the server
// understood the upgrade rather than merely returning 101.
//
// Copy this from the specification, never from memory. The first version of this
// line read ...95CA-5AB0DC85B11F, which drops the last group's leading C and
// pads the end with an F, and every handshake against a real server failed on a
// mismatched accept. The test double carried the same typo, so client and double
// agreed and the test passed: the bug only appeared against uvicorn.
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type wsConn struct {
	conn   net.Conn
	reader *bufio.Reader
	// writes are serialised because the audio pump and the close path can both
	// reach this from different goroutines.
	writeMu sync.Mutex
}

// wsDial opens a websocket to a plain-HTTP address. dialURL is where the socket
// actually goes, which on this plane is always loopback.
func wsDial(ctx context.Context, dialURL string, header http.Header) (*wsConn, error) {
	parsed, err := url.Parse(dialURL)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "ws" {
		return nil, fmt.Errorf("wsDial: scheme %q is not supported; the local plane dials plain ws over loopback", parsed.Scheme)
	}
	host := parsed.Host
	if parsed.Port() == "" {
		host = net.JoinHostPort(host, "80")
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		_ = conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	path := parsed.RequestURI()
	request := &strings.Builder{}
	fmt.Fprintf(request, "GET %s HTTP/1.1\r\nHost: %s\r\n", path, parsed.Host)
	fmt.Fprintf(request, "Upgrade: websocket\r\nConnection: Upgrade\r\n")
	fmt.Fprintf(request, "Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n", key)
	for name, values := range header {
		for _, value := range values {
			fmt.Fprintf(request, "%s: %s\r\n", name, value)
		}
	}
	request.WriteString("\r\n")
	if _, err := conn.Write([]byte(request.String())); err != nil {
		_ = conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		// A 403 here is the signature check, which is the most likely failure
		// and the least obvious, so it is named rather than left as a number.
		if response.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("the agent refused the stream with 403: the request signature did not match, "+
				"which means the stand-in and the agent disagree about the public URL or the auth token (dialled %s)", dialURL)
		}
		return nil, fmt.Errorf("the agent answered %s to the stream upgrade", response.Status)
	}
	digest := sha1.Sum([]byte(key + wsGUID))
	// Both values in the message, because "wrong key" alone is unactionable: an
	// empty one means the peer answered 101 without completing the handshake,
	// and a different one means it hashed something other than what was sent.
	if want, got := base64.StdEncoding.EncodeToString(digest[:]), response.Header.Get("Sec-WebSocket-Accept"); got != want {
		_ = conn.Close()
		return nil, fmt.Errorf("the peer accepted the upgrade but answered Sec-WebSocket-Accept %q "+
			"for key %q, want %q, so it is not completing the websocket handshake", got, key, want)
	}
	return &wsConn{conn: conn, reader: reader}, nil
}

// writeFrame writes one final, masked frame. Every client frame must be masked;
// a server that receives an unmasked one is required to close the connection.
func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	header := []byte{0x80 | opcode}
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, byte(0x80|length))
	case length < 1<<16:
		header = append(header, 0x80|126, 0, 0)
		binary.BigEndian.PutUint16(header[2:4], uint16(length))
	default:
		header = append(header, 0x80|127)
		header = append(header, make([]byte, 8)...)
		binary.BigEndian.PutUint64(header[2:10], uint64(length))
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	header = append(header, mask[:]...)
	masked := make([]byte, length)
	for i, b := range payload {
		masked[i] = b ^ mask[i%4]
	}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(masked)
	return err
}

// WriteJSON sends one protocol message.
func (c *wsConn) WriteJSON(message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return c.writeFrame(0x1, payload)
}

// ReadJSON returns the next protocol message, answering pings and treating a
// close frame as the end of the stream. Returns io.EOF when the peer closed.
func (c *wsConn) ReadJSON() (map[string]any, error) {
	for {
		opcode, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		switch opcode {
		case 0x1, 0x2:
			var message map[string]any
			if err := json.Unmarshal(payload, &message); err != nil {
				return nil, fmt.Errorf("the agent sent a frame that is not JSON: %w", err)
			}
			return message, nil
		case 0x8:
			return nil, io.EOF
		case 0x9:
			if err := c.writeFrame(0xA, payload); err != nil {
				return nil, err
			}
		case 0xA:
			// a pong we did not ask for; nothing to do
		}
	}
}

func (c *wsConn) readFrame() (opcode byte, payload []byte, err error) {
	var head [2]byte
	if _, err := io.ReadFull(c.reader, head[:]); err != nil {
		return 0, nil, err
	}
	opcode = head[0] & 0x0F
	masked := head[1]&0x80 != 0
	length := int(head[1] & 0x7F)
	switch length {
	case 126:
		var extended [2]byte
		if _, err := io.ReadFull(c.reader, extended[:]); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint16(extended[:]))
	case 127:
		var extended [8]byte
		if _, err := io.ReadFull(c.reader, extended[:]); err != nil {
			return 0, nil, err
		}
		size := binary.BigEndian.Uint64(extended[:])
		// A frame this large is not something any of these carriers sends, and
		// refusing it beats allocating whatever a confused peer asked for.
		if size > 1<<24 {
			return 0, nil, fmt.Errorf("the agent sent a %d byte frame, which is past anything this protocol carries", size)
		}
		length = int(size)
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.reader, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload = make([]byte, length)
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

// Close sends a close frame and drops the connection. On these routes closing
// the stream is what ends the call, so this is the hangup.
func (c *wsConn) Close() error {
	_ = c.writeFrame(0x8, []byte{0x03, 0xE8}) // 1000, normal closure
	return c.conn.Close()
}

// --- who the caller is ------------------------------------------------------

// callerAudio is where the caller's speech comes from and where the agent's
// speech goes. Two implementations: a fixture, which is what makes the
// unattended check possible, and a person, which is what makes a judgement
// about delay possible. The spec allows both and defaults to the fixture.
type callerAudio struct {
	// next is the caller's next 20 ms of mu-law, and false when there is no
	// more. A fixture runs out; a microphone does not.
	next func() ([]byte, bool)
	// play hands the agent's audio to the speaker. nil when nobody is listening.
	play func([]byte)
	// close releases whatever was opened.
	close func()
}

// fixtureCaller plays synthesised speech. Deterministic, so a recording of it
// can be compared against what was sent, and no audio device is touched.
func fixtureCaller(seconds int) *callerAudio {
	frames := mulawFrames(pcmToMulaw(callFixtureSamples(seconds)))
	index := 0
	return &callerAudio{
		next: func() ([]byte, bool) {
			if index >= len(frames) {
				return nil, false
			}
			frame := frames[index]
			index++
			return frame, true
		},
		close: func() {},
	}
}

// soxCallerCommands is what the person-as-caller mode runs. Named here so the
// error message and the documentation cannot drift from the invocation.
//
// Both directions are raw 8 kHz mono mu-law, which is the call's own format, so
// nothing resamples on our side and a fault is a fault in the call rather than
// in a conversion. `-q` because sox otherwise draws a meter over the run's
// output.
var (
	soxRecordArgs = []string{"-q", "-t", "raw", "-r", "8000", "-e", "u-law", "-b", "8", "-c", "1", "-"}
	soxPlayArgs   = []string{"-q", "-t", "raw", "-r", "8000", "-e", "u-law", "-b", "8", "-c", "1", "-"}
)

// personCaller wires the machine's microphone and speaker into the call through
// sox. An external tool rather than a library: CGO is off, so this binary cannot
// open an audio device itself, and the SIP plane already asks the author to
// install a softphone for the same reason.
func personCaller(ctx context.Context) (*callerAudio, error) {
	for _, tool := range []string{"rec", "play"} {
		if _, err := exec.LookPath(tool); err != nil {
			return nil, fmt.Errorf("talking to the agent needs %q on PATH, which comes with sox "+
				"(`brew install sox`). Without it the caller is the built-in fixture, which is what "+
				"a default run uses", tool)
		}
	}
	record := exec.CommandContext(ctx, "rec", soxRecordArgs...)
	microphone, err := record.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// sox writes its own diagnostics to stderr; they belong in the run's log,
	// not interleaved with the call.
	record.Stderr = nil
	if err := record.Start(); err != nil {
		return nil, fmt.Errorf("start the microphone: %w", err)
	}

	speaker := exec.CommandContext(ctx, "play", soxPlayArgs...)
	speakerIn, err := speaker.StdinPipe()
	if err != nil {
		return nil, err
	}
	speaker.Stderr = nil
	if err := speaker.Start(); err != nil {
		return nil, fmt.Errorf("start the speaker: %w", err)
	}

	return &callerAudio{
		next: func() ([]byte, bool) {
			frame := make([]byte, callAudioFrameSamples)
			// A short read is a device hiccup, not the end of the call: a
			// microphone has no end. Reading the full frame keeps the pump on
			// the call's own clock.
			if _, err := io.ReadFull(microphone, frame); err != nil {
				return nil, false
			}
			return frame, true
		},
		play: func(frame []byte) {
			// A failed write means the speaker went away. Nothing to do about
			// it mid-call, and the recording still gets everything.
			_, _ = speakerIn.Write(frame)
		},
		close: func() {
			_ = speakerIn.Close()
			_ = record.Process.Kill()
			_ = speaker.Wait()
			_ = record.Wait()
		},
	}, nil
}

// --- the call ---------------------------------------------------------------

// mediaCarrierRun is one stand-in, configured for one agent. The auth token is
// whatever the plane told the agent to expect: the stand-in signs with the same
// value, so the pair agree without either of them being a real credential.
type mediaCarrierRun struct {
	carrier string
	// publicURL is what the agent believes its own address is, and therefore
	// what it signs over. The stand-in must sign over the same string even
	// though it dials somewhere else, which is why this is separate from dial.
	publicURL string
	// dial is where the socket and the webhook actually go: loopback.
	dial      string
	authToken string
	callDir   string
	// inboundPath is where this route's agent answers the carrier's incoming
	// call. It differs per route and is not guessable: the carrier-websocket and
	// connector routes serve /telephony/inbound, and the cloud-websocket route's
	// local runner serves "/". Hardcoding one of them answers 404 on the other,
	// which is how this was found.
	inboundPath string
	// fixtureSeconds is how long the caller talks for. A call that outlasts its
	// audio is a call that goes quiet, so this bounds the useful part of a run.
	fixtureSeconds int
	// forceFixture keeps the machine's audio devices out of it. Set by the
	// unattended check, which has nobody to talk to it and must not depend on an
	// audio device existing. A person running the command never sets it: they get
	// the microphone when the machine has one, and the fixture when it does not.
	forceFixture bool
	// control is the carrier's call-control endpoint for this run, which is how
	// a cold transfer arrives: not over the media stream, but as a replacement
	// document posted to the carrier's REST API. Nil leaves a call unable to be
	// transferred, which is what a test that is not about transfers wants.
	control *mediaControl
	// outbound, when set, is a call the agent asked the carrier to place: the
	// stream address is already known, so there is no inbound webhook to post
	// and the call starts at the socket. Everything after that is one call
	// either way, which is why this is a field and not a second call loop.
	outbound *outboundRequest
}

// mediaPlaneBridgeSeconds is how long a fixture-driven call stays bridged to
// the transfer destination. Long enough that both legs carry audible audio and
// an empty recording means something, short enough that a default run is not
// something you wait through. A person's call is not bounded here: it ends when
// they stop it.
const mediaPlaneBridgeSeconds = 4

// fixtureOnly reports whether this call will play a recording rather than carry
// a person's voice.
func (r mediaCarrierRun) fixtureOnly() bool {
	return r.fixtureReason() != ""
}

// fixtureReason is why this call cannot carry a person's voice, or "" when it
// can. A sentence, because it is printed to somebody who expected to talk.
func (r mediaCarrierRun) fixtureReason() string {
	if r.forceFixture {
		return "this run asked for one"
	}
	for _, tool := range []string{"rec", "play"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Sprintf("%q is not on your PATH, and it comes with sox (`brew install sox`)", tool)
		}
	}
	return ""
}

// mediaCallResult is what one call is worth reporting: where the recordings
// landed, and whether the agent ever asked for a barge-in. The counts are what
// tells a connected-but-silent call apart from one that carried audio.
type mediaCallResult struct {
	CallerHeard string // path to what the caller heard, which is the agent talking
	CallerSpoke string // path to what the caller sent, which is the fixture
	// Counted in mu-law bytes of audio, which at 8 kHz is one byte per sample,
	// not in protocol messages and not in whole frames. Both alternatives lie:
	// the agent batches its audio, so measured on a live call 3.30 seconds
	// arrived in 24 messages, and rounding each message down to whole 20 ms
	// frames lost a further 0.4 seconds. Bytes are exact.
	BytesToAgent   int
	BytesFromAgent int
	// FramesHeard is what actually reached the caller, which is what a barge-in
	// changes. Filled by whoever reads the recording back, not by the call.
	FramesHeard int
	Clears      int
	// Transfer is what happened when the agent asked the carrier to move this
	// call, and nil on the calls where it never asked. A pointer rather than a
	// bool and five zero values, so "no transfer" cannot be mistaken for "a
	// transfer that did nothing".
	Transfer *mediaTransfer
}

// mediaTransfer is a cold transfer as this plane can witness it: the shared
// record of how far it got, plus the two things only the carrier knows.
//
// Deliberately not "did the third party pick up". The plane is the carrier, so
// it knows the destination was dialled and it knows what the destination's leg
// carried; a human deciding whether to answer a real phone is exactly the part
// a local run cannot prove, which is why gate S9 asks cold for requested,
// accepted and the caller's leg leaving the agent, and not for completion.
type mediaTransfer struct {
	// The shared vocabulary from dev_run_report.go, so this plane's transfers
	// and the SIP plane's are the same thing said once. Its state machine is
	// also the guard that a cold transfer never reports a merge.
	transferRecord
	// Announcement is what the carrier was told to say to the caller first,
	// which on this route is the agent's whole announcement: it is spoken by
	// the carrier and not by the agent, and hearing it is how a caller knows a
	// transfer started at all.
	Announcement string
	// StreamCut records the caller's leg leaving the agent. The carrier tears
	// the media stream down when it applies the new document, so a transfer
	// where this is false is a transfer the agent only announced.
	StreamCut bool
	// DestinationHeard is the path to what the caller said after the handoff,
	// which is the destination's leg. Written even when empty, because an empty
	// destination leg is a finding.
	DestinationHeard   string
	BytesToDestination int
	// Ended is whether the document ended the caller's leg after the dial. The
	// plane must not hang up a call the document did not ask it to.
	Ended bool
}

// inboundCall posts the agent's own inbound webhook the way a carrier would and
// returns the stream address out of the answer. This is the step that proves the
// webhook, the signature check and the markup, before any audio exists.
func (r mediaCarrierRun) inboundCall(ctx context.Context, from, to, callID string) (string, error) {
	form := url.Values{
		"CallSid":    {callID},
		"From":       {from},
		"To":         {to},
		"Direction":  {"inbound"},
		"CallStatus": {"ringing"},
	}
	// The agent computes the URL it signs from its own configured public
	// address, never from the request, so this is signed over publicURL even
	// though the request goes to dial.
	signed := r.publicURL + r.inboundPath
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.dial+r.inboundPath, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if r.carrier == "twilio" {
		request.Header.Set("X-Twilio-Signature", twilioSignature(signed, form, r.authToken))
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the agent answered %s to the inbound webhook: %s",
			response.Status, strings.TrimSpace(string(body)))
	}
	return mediaStreamAddress(string(body))
}

// streamPath turns the address the agent answered with into the loopback URL to
// dial, and the URL the agent will sign over. The agent builds a wss:// address
// from its own public URL; the socket goes to loopback instead, and the
// signature has to be over the agent's version.
func (r mediaCarrierRun) streamPath(address string) (dialURL, signedURL string, err error) {
	parsed, err := url.Parse(address)
	if err != nil {
		return "", "", fmt.Errorf("the agent's stream address is not a URL: %w", err)
	}
	if parsed.Path == "" {
		return "", "", fmt.Errorf("the agent's stream address %q carries no path", address)
	}
	dialBase, err := url.Parse(r.dial)
	if err != nil {
		return "", "", err
	}
	dialURL = "ws://" + dialBase.Host + parsed.Path
	return dialURL, address, nil
}

// call runs one whole call: webhook, stream, audio both ways, then hangs up by
// closing the socket, which is what ends a call on these routes.
func (r mediaCarrierRun) call(ctx context.Context, from, to string, duration time.Duration) (mediaCallResult, error) {
	var result mediaCallResult
	// Identifiers a carrier would have minted. The agent stores and echoes them
	// but never checks their shape, so random text with the carrier's prefix is
	// enough, and randomCredentialText is already the repository's one source of
	// unguessable text.
	callSuffix, err := randomCredentialText(16)
	if err != nil {
		return result, err
	}
	streamSuffix, err := randomCredentialText(16)
	if err != nil {
		return result, err
	}
	callID, streamID := "CA"+callSuffix, "MZ"+streamSuffix
	if r.outbound != nil {
		// The carrier already minted this call's id when it accepted the request
		// to place it, and the agent is holding that id: a second one here would
		// make its transfer document unaddressable.
		callID = r.outbound.CallID
	}
	// Watched before the webhook, not after: the agent can post a transfer
	// document as soon as it has the call id, and a document that arrives
	// before anyone is listening is answered 404 and reported to the caller as
	// a failed transfer.
	var instructions <-chan transferInstruction
	if r.control != nil {
		watched, stop := r.control.watch(callID)
		defer stop()
		instructions = watched
	}
	// An outbound call has no webhook to post: the agent handed the carrier its
	// stream address in the document it asked the call to be placed with.
	address := ""
	if r.outbound != nil {
		address = r.outbound.Address
	} else {
		posted, err := r.inboundCall(ctx, from, to, callID)
		if err != nil {
			return result, err
		}
		address = posted
	}
	dialURL, signedURL, err := r.streamPath(address)
	if err != nil {
		return result, err
	}
	header := http.Header{}
	if r.carrier == "twilio" {
		// The stream is signed too, over the wss:// form and with no parameters.
		header.Set("X-Twilio-Signature", twilioSignature(signedURL, nil, r.authToken))
	}
	conn, err := wsDial(ctx, dialURL, header)
	if err != nil {
		return result, err
	}
	// Closing the socket is the hangup on this route, and there is nothing to do
	// about a failure to hang up a call that is already over.
	defer func() { _ = conn.Close() }()

	handshake, err := mediaHandshake(r.carrier, streamID, callID, from, to)
	if err != nil {
		return result, err
	}
	for _, message := range handshake {
		if err := conn.WriteJSON(message); err != nil {
			return result, fmt.Errorf("the handshake did not reach the agent: %w", err)
		}
	}

	// What the caller heard, assembled as it is played out rather than as it
	// arrives: a barge-in drops audio the agent had queued but the caller had
	// not reached yet, and a recording built on arrival could not show that.
	var (
		mu             sync.Mutex
		pending        []byte
		heard          []int16
		bytesFromAgent int
		clears         int
	)
	// settled is result with the reader goroutine's own two counters folded in,
	// under the lock that goroutine writes them behind. They are the only fields
	// two goroutines touch, and returning result directly reported whatever this
	// goroutine happened to see: a barge-in the agent really did send came back
	// as no barge-in at all, which is the one thing gate M4 exists to catch.
	settled := func() mediaCallResult {
		mu.Lock()
		defer mu.Unlock()
		result.BytesFromAgent = bytesFromAgent
		result.Clears = clears
		return result
	}
	readDone := make(chan error, 1)
	go func() {
		for {
			message, err := conn.ReadJSON()
			if err != nil {
				readDone <- err
				return
			}
			switch message["event"] {
			case "media":
				payload, _ := message["media"].(map[string]any)
				encoded, _ := payload["payload"].(string)
				frame, decodeErr := base64.StdEncoding.DecodeString(encoded)
				if decodeErr != nil {
					readDone <- fmt.Errorf("the agent sent audio that is not base64: %w", decodeErr)
					return
				}
				mu.Lock()
				pending = append(pending, frame...)
				bytesFromAgent += len(frame)
				mu.Unlock()
			case "clear":
				// Barge-in: the agent is telling the carrier to throw away
				// anything not yet played. Honouring it is what makes local
				// turn-taking behave like a real call rather than better than
				// one (gate M4).
				mu.Lock()
				pending = nil
				clears++
				mu.Unlock()
			}
		}
	}()

	// The caller's own audio, and the play-out of the agent's, both on the same
	// 20 ms clock a carrier uses.
	caller := fixtureCaller(r.fixtureSeconds)
	if !r.fixtureOnly() {
		person, err := personCaller(ctx)
		if err != nil {
			return settled(), err
		}
		caller = person
	}
	defer caller.close()

	ticker := time.NewTicker(mediaFrameInterval)
	defer ticker.Stop()
	// A duration of zero runs until the context is cancelled, which is what a
	// person talking needs: a conversation does not have a length decided in
	// advance.
	deadline := time.Now().Add(duration)
	var spoke []int16
	for duration == 0 || time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return r.finish(settled(), spoke, heard, nil)
		case err := <-readDone:
			if errors.Is(err, io.EOF) {
				return r.finish(settled(), spoke, heard, nil)
			}
			return settled(), err
		case instruction := <-instructions:
			// The carrier applies the document, and applying it is what ends the
			// media stream: the agent's own comment says a line it had started
			// speaking is cut off by its own transfer. So the stream goes first,
			// before any of the dialling, exactly as it does at Twilio.
			transfer := &mediaTransfer{
				transferRecord: transferRecord{
					Shape:       coldTransfer,
					Destination: instruction.Destination,
					Outcome:     transferRequested,
				},
				Announcement: instruction.Say,
			}
			// Accepted the moment the document was taken, which is what the
			// handler already answered 200 to. Recorded here rather than there
			// so the record and the caller's leg move together.
			if err := transfer.advance(transferAccepted); err != nil {
				return settled(), err
			}
			transfer.StreamCut = conn.Close() == nil
			result.Transfer = transfer
			return r.bridge(ctx, settled(), instruction, caller, spoke, heard)
		case <-ticker.C:
		}
		if frame, ok := caller.next(); ok {
			if err := conn.WriteJSON(map[string]any{
				"event":     "media",
				"streamSid": streamID,
				"media": map[string]any{
					"payload": base64.StdEncoding.EncodeToString(frame),
				},
			}); err != nil {
				// The agent closing mid-call is an outcome, not a failure of
				// the stand-in: report what was recorded up to that point.
				break
			}
			spoke = append(spoke, mulawToPCM(frame)...)
			result.BytesToAgent += len(frame)
		}
		mu.Lock()
		if len(pending) >= callAudioFrameSamples {
			played := pending[:callAudioFrameSamples]
			heard = append(heard, mulawToPCM(played)...)
			if caller.play != nil {
				caller.play(played)
			}
			pending = pending[callAudioFrameSamples:]
		}
		mu.Unlock()
	}
	return r.finish(settled(), spoke, heard, nil)
}

// bridge carries the transfer document out, after the agent's stream has been
// cut. The caller is still on the line: they now talk to the destination
// instead of to the agent, which is the whole of what a cold transfer is.
//
// The destination answers immediately and plays the same fixture the caller
// does. A real destination is a person who may not pick up, and that is exactly
// the part of a transfer no local run can prove: gate S9 asks this plane for
// requested, accepted, and the caller's leg leaving the agent, and deliberately
// not for third-leg completion.
func (r mediaCarrierRun) bridge(ctx context.Context, result mediaCallResult,
	instruction transferInstruction, caller *callerAudio, spoke, heard []int16,
) (mediaCallResult, error) {
	// One destination is defined as never answering, so a run can take the
	// route's unavailable path without a real number that really does not pick
	// up. Handled before any audio moves, because nothing is bridged on a call
	// nobody took.
	if instruction.Destination == target.LocalPlaneNoAnswerNumber {
		return r.ringOut(ctx, result, instruction, spoke, heard)
	}
	// The destination's own audio, so the caller's leg is not silent after the
	// handoff and a recording that went quiet means the bridge failed rather
	// than the fixture running out.
	destination := fixtureCaller(r.fixtureSeconds)
	defer destination.close()

	ticker := time.NewTicker(mediaFrameInterval)
	defer ticker.Stop()
	// A fixture call is bounded so an unattended run ends. A person's call is
	// not: they are talking to the destination now and will stop when they are
	// done.
	deadline := time.Time{}
	if r.fixtureOnly() {
		deadline = time.Now().Add(mediaPlaneBridgeSeconds * time.Second)
	}
	var toDestination []int16
	for deadline.IsZero() || time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return r.finish(result, spoke, heard, toDestination)
		case <-ticker.C:
		}
		frame, ok := caller.next()
		if !ok {
			// The caller has nothing left to say. On a fixture run that is the
			// end of the useful part of the bridge.
			break
		}
		toDestination = append(toDestination, mulawToPCM(frame)...)
		result.Transfer.BytesToDestination += len(frame)
		if reply, ok := destination.next(); ok {
			heard = append(heard, mulawToPCM(reply)...)
			if caller.play != nil {
				caller.play(reply)
			}
		}
	}
	// <Hangup/> after the dial is how the document ends the original call, and
	// a document without it leaves the caller where they are. Recorded rather
	// than acted on, because the stand-in has already stopped carrying the
	// caller's audio anywhere by the time this returns.
	result.Transfer.Ended = instruction.Hangup
	// The destination was dialled and it answered, which is as far as a cold
	// transfer goes: only a warm one can merge, and the record refuses that move
	// on this shape. A destination leg that carried nothing does not advance,
	// so a bridge that failed cannot report a destination that was reached.
	if result.Transfer.BytesToDestination > 0 {
		if err := result.Transfer.advance(transferDestinationReached); err != nil {
			return result, err
		}
	}
	return r.finish(result, spoke, heard, toDestination)
}

// ringOut is the unavailable path, and it is the whole of FR-013 on this plane:
// the destination rang and nobody took the call, so the run reports which of the
// two things the document asked for actually happened.
//
// The document decides, not a setting read here. `<Hangup/>` after the dial ends
// the caller's original leg, and its absence leaves the caller where they were,
// which is the difference between the two outcomes the report can name. That is
// the same reading of the same document as the answered path, so a package whose
// on_unavailable is hangup cannot be reported as returning the caller.
func (r mediaCarrierRun) ringOut(ctx context.Context, result mediaCallResult,
	instruction transferInstruction, spoke, heard []int16,
) (mediaCallResult, error) {
	// Rung for as long as the document asked, because the caller really does
	// wait through it and a run that skipped the wait would hide how long their
	// own timeout makes somebody hold on. Capped so an unattended run ends: a
	// document asking for two minutes of ringing is not worth two minutes here.
	ring := instruction.RingTimeout
	if ring <= 0 || ring > mediaPlaneRingCap {
		ring = mediaPlaneRingCap
	}
	timer := time.NewTimer(ring)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
	outcome := transferUnavailableReturn
	if instruction.Hangup {
		outcome = transferUnavailableHangup
	}
	result.Transfer.Ended = instruction.Hangup
	if err := result.Transfer.advance(outcome); err != nil {
		return result, err
	}
	// No third leg is written, deliberately: an empty destination recording says
	// a leg connected and carried nothing, and here no leg was ever connected.
	return r.finish(result, spoke, heard, nil)
}

// mediaPlaneRingCap bounds how long the stand-in rings an unanswered
// destination, whatever the document asks for.
const mediaPlaneRingCap = 15 * time.Second

// finish writes the legs out. Every one is written even when empty, because an
// empty recording is a finding: it says the leg connected and carried nothing,
// which is a different fault from a leg that never connected at all.
func (r mediaCarrierRun) finish(result mediaCallResult, spoke, heard, toDestination []int16) (mediaCallResult, error) {
	result.CallerSpoke = filepath.Join(r.callDir, "caller.wav")
	result.CallerHeard = filepath.Join(r.callDir, "caller_heard.wav")
	if err := os.MkdirAll(r.callDir, 0o755); err != nil {
		return result, err
	}
	if err := writeWAV(result.CallerSpoke, spoke, callAudioRate); err != nil {
		return result, err
	}
	if err := writeWAV(result.CallerHeard, heard, callAudioRate); err != nil {
		return result, err
	}
	// The third leg exists only on a call that was transferred, and naming it
	// on a call that was not would invite a reader to go looking for it.
	if result.Transfer != nil {
		result.Transfer.DestinationHeard = filepath.Join(r.callDir, "destination_heard.wav")
		if err := writeWAV(result.Transfer.DestinationHeard, toDestination, callAudioRate); err != nil {
			return result, err
		}
	}
	return result, nil
}

// callFixtureSamples is the caller's audio as samples, for as long as asked.
func callFixtureSamples(seconds int) []int16 {
	if seconds <= 0 {
		return nil
	}
	samples := make([]int16, seconds*callAudioRate)
	for i := range samples {
		samples[i] = callFixtureSample(i)
	}
	return samples
}
