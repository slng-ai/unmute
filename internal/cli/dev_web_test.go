package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/slng-ai/unmute/internal/devmetrics"
	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
	"github.com/slng-ai/unmute/internal/web"
)

func TestDevSessionCallIDs(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		t.Run(string(provider), func(t *testing.T) {
			stream := readyDevStream(t)
			previous := ""
			for range 2 {
				rr := httptest.NewRecorder()
				devSessionHandler(provider, "agent", "ws://127.0.0.1:7880", stream)(rr, httptest.NewRequest(http.MethodGet, "/api/session", nil))
				var body map[string]any
				if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				id, _ := body["call_id"].(string)
				if id == "" || id == previous || body["dev_events_version"] != float64(2) {
					t.Fatalf("bootstrap identity = %q, previous = %q, version = %v", id, previous, body["dev_events_version"])
				}
				if provider == ir.ProviderLiveKit && id != body["room"] {
					t.Fatalf("call ID %q does not match native room %v", id, body["room"])
				}
				previous = id
			}
		})
	}
}

// A real net.Pipe write cannot complete without a reader or a deadline. This
// proves the response-controller guard releases a stalled write, not just that
// the event loop notices cancellation while it is already waiting for input.
type stalledDevWriter struct {
	conn   net.Conn
	header http.Header
}

func (w *stalledDevWriter) Header() http.Header         { return w.header }
func (w *stalledDevWriter) WriteHeader(int)             {}
func (w *stalledDevWriter) Flush()                      {}
func (w *stalledDevWriter) Write(p []byte) (int, error) { return w.conn.Write(p) }
func (w *stalledDevWriter) SetWriteDeadline(deadline time.Time) error {
	return w.conn.SetWriteDeadline(deadline)
}

func TestDevEventsBoundsAStalledWrite(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()
	w := &stalledDevWriter{conn: server, header: make(http.Header)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		devEventsHandler(newDevStream())(w, httptest.NewRequest(http.MethodGet, "/api/events", nil))
	}()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("SSE handler stayed blocked on an unread connection")
	}
}

type deadlineDevWriter struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (w *deadlineDevWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func TestDevEventsClearsDeadlineAfterEachFlush(t *testing.T) {
	w := &deadlineDevWriter{ResponseRecorder: httptest.NewRecorder()}
	for _, event := range []devEvent{
		{T: devEventState, State: devStateReady},
		{T: devEventLog, Seq: 1, StreamID: "stream", Text: "arrived"},
	} {
		if !writeDevEvent(w, event) {
			t.Fatal("could not write the event")
		}
		if len(w.deadlines) != 2 || w.deadlines[0].IsZero() || !w.deadlines[1].IsZero() {
			t.Fatalf("write deadlines = %v; want a bound followed by a cleared idle deadline", w.deadlines)
		}
		if !w.Flushed {
			t.Fatal("event was not flushed immediately")
		}
		w.deadlines = nil
		w.Flushed = false
	}
}

type pausedDevWriter struct {
	*deadlineDevWriter
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *pausedDevWriter) Flush() {
	w.ResponseRecorder.Flush()
	w.once.Do(func() {
		close(w.entered)
		<-w.release
	})
}

func TestDevEventsOverflowEndsWithoutDrainingItsQueue(t *testing.T) {
	stream := newDevStream()
	w := &pausedDevWriter{
		deadlineDevWriter: &deadlineDevWriter{ResponseRecorder: httptest.NewRecorder()},
		entered:           make(chan struct{}),
		release:           make(chan struct{}),
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		devEventsHandler(stream)(w, httptest.NewRequest(http.MethodGet, "/api/events", nil))
	}()
	<-w.entered // initial state flushed; hold the handler while its queue fills
	for range devStreamQueue + 1 {
		_, _ = stream.Write([]byte("queued\n"))
	}
	close(w.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("overflowed handler waited for another producer event")
	}
	if count := strings.Count(w.Body.String(), "data: "); count != 1 {
		t.Fatalf("handler drained stale queue after overflow: %d writes, want only initial state", count)
	}
}

func TestDevEventsRejectsMalformedCursor(t *testing.T) {
	srv := httptest.NewServer(devEventsHandler(newDevStream()))
	defer srv.Close()
	for _, cursor := range []string{"2", "stream:-1", "stream:0", ":1", "stream:1:2", "stream:+1", "stream:99999999999999999999999999999"} {
		t.Run(cursor, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Last-Event-ID", cursor)
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("cursor %q status = %d, want 400", cursor, resp.StatusCode)
			}
		})
	}
}

func TestDevEventsSignalsRestartBeforeRetainedData(t *testing.T) {
	stream := newDevStream()
	_, _ = stream.Write([]byte("retained\n"))
	srv := httptest.NewServer(devEventsHandler(stream))
	defer srv.Close()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", "previous-stream:1")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	r := bufio.NewReader(resp.Body)
	_, _ = readSSE(t, r) // current state
	if id, ev := readSSE(t, r); ev.T != "gap" || id != "" {
		t.Fatalf("restart did not signal an ID-less gap before data: id=%q event=%+v", id, ev)
	}
	if _, ev := readSSE(t, r); ev.Text != "retained" {
		t.Fatalf("restart did not replay retained data: %+v", ev)
	}
}

func TestDevSessionPipecatCallIDSurvivesTheOfferProxy(t *testing.T) {
	const callID = "unmute-dev-offer"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var offer map[string]any
		if err := json.NewDecoder(r.Body).Decode(&offer); err != nil {
			t.Error(err)
		}
		data, _ := offer["request_data"].(map[string]any)
		if data["unmute_dev_call_id"] != callID {
			t.Errorf("offer request_data = %v", data)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	port := strings.TrimPrefix(upstream.URL, "http://127.0.0.1:")
	page := httptest.NewServer(devWebMux(ir.ProviderPipecat, "agent", port, "", readyDevStream(t)))
	defer page.Close()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, page.URL+"/api/offer", strings.NewReader(`{"sdp":"offer","type":"offer","pc_id":null,"request_data":{"unmute_dev_call_id":"`+callID+`"}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := page.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("offer proxy status = %d", resp.StatusCode)
	}
}

// readyDevStream is a stream already past startup, so the session handler answers
// with the real transport contract rather than "not ready yet".
func readyDevStream(t *testing.T) *devStream {
	t.Helper()
	s := newDevStream()
	s.SetState(devStateReady)
	return s
}

// TestDevSessionHandlerPipecat: the bootstrap contract for pipecat is the
// webrtc-offer kind pointing at the proxied offer URL (SPEC I.session, V6).
func TestDevSessionHandlerPipecat(t *testing.T) {
	rr := httptest.NewRecorder()
	devSessionHandler(ir.ProviderPipecat, "pipecat", "", readyDevStream(t)).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/session", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["kind"] != "webrtc-offer" || body["offerUrl"] != "/api/offer" || body["ready"] != true {
		t.Errorf("pipecat session = %v", body)
	}
}

// TestDevSessionHandlerLiveKit: the livekit kind carries the dev server URL and
// a fresh-room token whose agent dispatch names the target (SPEC I.session, V6).
func TestDevSessionHandlerLiveKit(t *testing.T) {
	liveKitURL := "ws://127.0.0.1:7883"
	ready := readyDevStream(t)
	decode := func() map[string]string {
		t.Helper()
		rr := httptest.NewRecorder()
		devSessionHandler(ir.ProviderLiveKit, "remy-dev", liveKitURL, ready).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/session", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d", rr.Code)
		}
		var raw map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		if raw["ready"] != true {
			t.Fatalf("ready = %v, want true", raw["ready"])
		}
		body := map[string]string{}
		for k, v := range raw {
			if s, ok := v.(string); ok {
				body[k] = s
			}
		}
		return body
	}

	body := decode()
	if body["kind"] != "livekit" {
		t.Errorf("kind = %q, want livekit", body["kind"])
	}
	if body["url"] != liveKitURL {
		t.Errorf("url = %q, want %q", body["url"], liveKitURL)
	}
	if !strings.HasPrefix(body["room"], "unmute-") {
		t.Errorf("room = %q, want unmute- prefix", body["room"])
	}
	parts := strings.Split(body["token"], ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments", len(parts))
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims lkClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Video.Room != body["room"] {
		t.Errorf("token room %q != response room %q", claims.Video.Room, body["room"])
	}
	if len(claims.RoomConfig.Agents) != 1 || claims.RoomConfig.Agents[0].AgentName != "remy-dev" {
		t.Errorf("agent dispatch = %+v", claims.RoomConfig.Agents)
	}
	// Fresh room per request (dispatch fires at room creation).
	if second := decode(); second["room"] == body["room"] {
		t.Errorf("two session requests reused room %q", body["room"])
	}
}

// TestDevWebMuxServesOnePageBothTargets: both targets serve the one branded
// page at / and answer /api/session; livekit additionally serves the vendored
// SDK. The per-target difference is only the transport wiring (SPEC V6).
func TestDevWebMuxServesOnePageBothTargets(t *testing.T) {
	get := func(h http.Handler, path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		return rr
	}

	pipecat := devWebMux(ir.ProviderPipecat, "pipecat", "7860", "", readyDevStream(t))
	if rr := get(pipecat, "/"); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "unmute dev") {
		t.Errorf("pipecat GET / = %d, body missing brand", rr.Code)
	}
	if rr := get(pipecat, "/api/session"); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "webrtc-offer") {
		t.Errorf("pipecat GET /api/session = %d: %s", rr.Code, rr.Body.String())
	}

	livekit := devWebMux(ir.ProviderLiveKit, "remy-dev", "7860", "ws://127.0.0.1:7880", readyDevStream(t))
	if rr := get(livekit, "/"); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "unmute dev") {
		t.Errorf("livekit GET / = %d, body missing brand", rr.Code)
	}
	if rr := get(livekit, "/livekit-client.umd.js"); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "LivekitClient") {
		t.Errorf("livekit GET SDK = %d", rr.Code)
	}
	if rr := get(livekit, "/api/session"); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"livekit"`) {
		t.Errorf("livekit GET /api/session = %d: %s", rr.Code, rr.Body.String())
	}
}

// TestReadyWatcher: fires once when the marker appears (even split across
// writes) and passes every byte through to the underlying writer.
func TestReadyWatcher(t *testing.T) {
	var sink bytes.Buffer
	count := 0
	rw := &readyWatcher{w: &sink, marker: []byte("registered worker"), fire: func() { count++ }}

	_, _ = rw.Write([]byte("booting up\n"))
	if count != 0 {
		t.Fatalf("fired before marker; count = %d", count)
	}
	_, _ = rw.Write([]byte("... registered wor")) // marker split across writes
	_, _ = rw.Write([]byte("ker id=xyz\n"))
	if count != 1 {
		t.Fatalf("fire count = %d, want 1", count)
	}
	_, _ = rw.Write([]byte("another registered worker line\n"))
	if count != 1 {
		t.Fatalf("fire must be once; count = %d", count)
	}
	if s := sink.String(); !strings.Contains(s, "booting up") || !strings.Contains(s, "id=xyz") {
		t.Errorf("passthrough lost data: %q", s)
	}
}

func TestWaitForLocalAgentReadyRequiresReadyStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}))
	defer server.Close()
	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	if err := waitForLocalAgentReady(t.Context(), port, make(chan error)); err != nil {
		t.Fatalf("waitForLocalAgentReady: %v", err)
	}

	done := make(chan error, 1)
	done <- errors.New("boom")
	if err := waitForLocalAgentReady(t.Context(), "1", done); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("early process exit = %v", err)
	}
}

func TestRunDevPipecatRejectsBusyAgentPort(t *testing.T) {
	// Match Python's --host 0.0.0.0. On macOS a generic tcp listener can
	// bind IPv6 while this IPv4 port is occupied by an earlier worker.
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	cmd, _ := devTestCommand(t)
	// A port the author typed is refused when busy.
	err = runDevPipecat(t.Context(), cmd, t.TempDir(), devWebRun{root: "pkg", botPort: port, botPortPinned: true})
	if err == nil || !strings.Contains(err.Error(), "already in use") || !strings.Contains(err.Error(), "--bot-port") {
		t.Fatalf("busy port error = %v", err)
	}
	// The default, left unset, gives way to a free port: the run gets past the
	// probe and fails later, on the empty log path this bare run carries.
	err = runDevPipecat(t.Context(), cmd, t.TempDir(), devWebRun{root: "pkg", botPort: port})
	if err == nil || strings.Contains(err.Error(), "already in use") || !strings.Contains(err.Error(), "open log") {
		t.Fatalf("unpinned busy port error = %v, want the run to move past the probe", err)
	}
}

func TestDevWebAssetsEmbedded(t *testing.T) {
	for _, name := range []string{"index.html", "logo.svg", "livekit-client.umd.js"} {
		info, err := fs.Stat(web.FS, name)
		if err != nil {
			t.Errorf("web asset %q not embedded: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("web asset %q is empty", name)
		}
	}
}

// readSSE reads one server-sent event: an id line, a data line, a blank line.
func readSSE(t *testing.T, r *bufio.Reader) (string, devEvent) {
	t.Helper()
	var id string
	var ev devEvent
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("reading event stream: %v", err)
		}
		switch {
		case strings.HasPrefix(line, "id: "):
			id = strings.TrimSpace(strings.TrimPrefix(line, "id: "))
		case strings.HasPrefix(line, "data: "):
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
				t.Fatalf("bad data line %q: %v", line, err)
			}
		case strings.TrimSpace(line) == "":
			return id, ev
		}
	}
}

// TestDevEventsReplaysThenStreams: output produced before the browser finishes
// loading is the most interesting output there is, so the backlog comes first and
// live lines follow on the same connection.
func TestDevEventsReplaysThenStreams(t *testing.T) {
	stream := newDevStream()
	_, _ = stream.Write([]byte("built image\n"))
	_, _ = stream.Write([]byte(devmetrics.Sentinel + `{"kind":"turn","seq":1,"e2e":0.9}` + "\n"))

	srv := httptest.NewServer(devEventsHandler(stream))
	defer srv.Close()
	resp, err := http.Get(srv.URL) //nolint:noctx // test client, closed below
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q", got)
	}
	r := bufio.NewReader(resp.Body)

	if id, ev := readSSE(t, r); id != "" || ev.T != devEventState || ev.State != devStateStarting {
		t.Fatalf("first event = %+v, want the latched state", ev)
	}
	if _, ev := readSSE(t, r); ev.T != devEventLog || ev.Text != "built image" {
		t.Fatalf("second event = %+v", ev)
	}
	id, ev := readSSE(t, r)
	if ev.T != devEventMetric || ev.Record == nil || ev.Record.Seq != 1 {
		t.Fatalf("third event = %+v", ev)
	}
	if prefix, sequence, ok := strings.Cut(id, ":"); !ok || prefix == "" || sequence != fmt.Sprint(ev.Seq) {
		t.Errorf("id %q does not carry stream identity and seq %d", id, ev.Seq)
	}

	// Now live, on the same connection.
	stream.SetState(devStateReady)
	if id, ev := readSSE(t, r); id != "" || ev.T != devEventState || ev.State != devStateReady {
		t.Fatalf("live state event = %+v", ev)
	}
	_, _ = stream.Write([]byte("registered worker\n"))
	if _, ev := readSSE(t, r); ev.Text != "registered worker" {
		t.Fatalf("live log event = %+v", ev)
	}
}

func TestDevEventsHonoursLastEventID(t *testing.T) {
	stream := newDevStream()
	_, _ = stream.Write([]byte("one\ntwo\nthree\n"))

	srv := httptest.NewServer(devEventsHandler(stream))
	defer srv.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", stream.id+":2") // "one" is seq 1, "two" is seq 2
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	r := bufio.NewReader(resp.Body)

	if _, ev := readSSE(t, r); ev.T != devEventState {
		t.Fatalf("first event = %+v, want the latched state", ev)
	}
	if _, ev := readSSE(t, r); ev.Text != "three" {
		t.Fatalf("resumed at %+v, want three", ev)
	}
}

// TestDevSessionAnswersBeforeTheRuntimeExists: the page is served first now, so
// /api/session has to answer while there is nothing to connect to. It must say
// so rather than hand back a token for a room no worker will join.
func TestDevSessionAnswersBeforeTheRuntimeExists(t *testing.T) {
	for name, tc := range map[string]struct {
		provider ir.Provider
		kind     string
	}{
		"livekit": {ir.ProviderLiveKit, "livekit"},
		"pipecat": {ir.ProviderPipecat, "webrtc-offer"},
	} {
		provider, wantKind := tc.provider, tc.kind
		t.Run(name, func(t *testing.T) {
			stream := newDevStream() // still starting
			rr := httptest.NewRecorder()
			devSessionHandler(provider, "agent", "ws://127.0.0.1:7880", stream).
				ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/session", nil))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d", rr.Code)
			}
			var body map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["ready"] != false {
				t.Errorf("ready = %v, want false", body["ready"])
			}
			if body["kind"] != wantKind {
				t.Errorf("kind = %v, want %q even while not ready", body["kind"], wantKind)
			}
			if _, ok := body["token"]; ok {
				t.Error("handed out a token before a worker could join the room")
			}
			if _, ok := body["call_id"]; ok {
				t.Error("created a call identity before the runtime was ready")
			}
		})
	}
}

// `unmute dev` mints a browser token whose dispatch entry names one agent, and
// the emitted worker registers under another name entirely unless the two read
// the same source.
//
// They did not. The token named the target instance and the worker registered
// the package's deploy name, so the room opened, no worker joined it, and the
// browser loop went silent with nothing logged as wrong. This holds the token's
// name to the string the emitted agent.py actually registers.
func TestDevDispatchNameMatchesTheEmittedWorker(t *testing.T) {
	agent, targets, err := loadPackage(filepath.Join("..", "testdata", "safe_core"), []string{"livekit"})
	if err != nil {
		t.Fatal(err)
	}
	resolved := targets[0]
	artifact, err := generate.Generate(agent, resolved, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	var botpy string
	for _, file := range artifact.Files {
		if file.Path == "agent.py" {
			botpy = string(file.Content)
		}
	}
	if botpy == "" {
		t.Fatal("no agent.py emitted")
	}
	dispatched := devDispatchName(agent, resolved)
	if !strings.Contains(botpy, `@server.rtc_session(agent_name="`+dispatched+`")`) {
		t.Errorf("dev dispatches to %q, which the emitted worker does not register", dispatched)
	}
	if dispatched == resolved.Name {
		t.Fatal("this test needs the dispatch name to differ from the target name")
	}
}
