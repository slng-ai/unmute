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
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/devmetrics"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/web"
)

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
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	cmd, _ := devTestCommand(t)
	err = runDevPipecat(t.Context(), cmd, t.TempDir(), devWebRun{root: "pkg", botPort: port})
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("busy port error = %v", err)
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
func readSSE(t *testing.T, r *bufio.Reader) (int, devEvent) {
	t.Helper()
	var id int
	var ev devEvent
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("reading event stream: %v", err)
		}
		switch {
		case strings.HasPrefix(line, "id: "):
			if _, err := fmt.Sscanf(strings.TrimSpace(line), "id: %d", &id); err != nil {
				t.Fatalf("bad id line %q: %v", line, err)
			}
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

	if _, ev := readSSE(t, r); ev.T != devEventState || ev.State != devStateStarting {
		t.Fatalf("first event = %+v, want the latched state", ev)
	}
	if _, ev := readSSE(t, r); ev.T != devEventLog || ev.Text != "built image" {
		t.Fatalf("second event = %+v", ev)
	}
	id, ev := readSSE(t, r)
	if ev.T != devEventMetric || ev.Record == nil || ev.Record.Seq != 1 {
		t.Fatalf("third event = %+v", ev)
	}
	if id != ev.Seq {
		t.Errorf("id %d does not match seq %d, so Last-Event-ID cannot resume", id, ev.Seq)
	}

	// Now live, on the same connection.
	stream.SetState(devStateReady)
	if _, ev := readSSE(t, r); ev.T != devEventState || ev.State != devStateReady {
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
	req.Header.Set("Last-Event-ID", "2") // "one" is seq 1, "two" is seq 2
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
		})
	}
}
