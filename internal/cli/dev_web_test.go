package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/web"
)

// TestDevSessionHandlerPipecat: the bootstrap contract for pipecat is the
// webrtc-offer kind pointing at the proxied offer URL (SPEC I.session, V6).
func TestDevSessionHandlerPipecat(t *testing.T) {
	rr := httptest.NewRecorder()
	devSessionHandler(ir.ProviderPipecat, "pipecat", "").ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/session", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["kind"] != "webrtc-offer" || body["offerUrl"] != "/api/offer" {
		t.Errorf("pipecat session = %v", body)
	}
}

// TestDevSessionHandlerLiveKit: the livekit kind carries the dev server URL and
// a fresh-room token whose agent dispatch names the target (SPEC I.session, V6).
func TestDevSessionHandlerLiveKit(t *testing.T) {
	liveKitURL := "ws://127.0.0.1:7883"
	decode := func() map[string]string {
		t.Helper()
		rr := httptest.NewRecorder()
		devSessionHandler(ir.ProviderLiveKit, "remy-dev", liveKitURL).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/session", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d", rr.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
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

	pipecat := devWebMux(ir.ProviderPipecat, "pipecat", "7860", "")
	if rr := get(pipecat, "/"); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "unmute dev") {
		t.Errorf("pipecat GET / = %d, body missing brand", rr.Code)
	}
	if rr := get(pipecat, "/api/session"); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "webrtc-offer") {
		t.Errorf("pipecat GET /api/session = %d: %s", rr.Code, rr.Body.String())
	}

	livekit := devWebMux(ir.ProviderLiveKit, "remy-dev", "7860", "ws://127.0.0.1:7880")
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

func TestDevWebAssetsEmbedded(t *testing.T) {
	for _, name := range []string{"index.html", "logo.png", "livekit-client.umd.js"} {
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
