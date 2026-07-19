package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/web"
)

// TestLiveKitTokenHandler (V5): GET /api/token returns the server url and a
// fresh room, and the token is a valid JWT carrying that room's agent dispatch.
func TestLiveKitTokenHandler(t *testing.T) {
	creds := liveKitCreds{URL: "wss://x.livekit.cloud", APIKey: "APIk", APISecret: "sec"}
	srv := httptest.NewServer(liveKitTokenHandler(creds, "remy-dev"))
	defer srv.Close()

	decode := func() map[string]string {
		t.Helper()
		res, err := http.Get(srv.URL + "/api/token")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		var body map[string]string
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}

	body := decode()
	if body["url"] != creds.URL {
		t.Errorf("url = %q, want %q", body["url"], creds.URL)
	}
	if !strings.HasPrefix(body["room"], "unmute-") {
		t.Errorf("room = %q, want unmute- prefix", body["room"])
	}
	// The token's roomConfig must dispatch the right agent.
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

	// Fresh room per request (C5): dispatch only fires at room creation.
	if second := decode(); second["room"] == body["room"] {
		t.Errorf("two token requests reused room %q", body["room"])
	}
}

func TestLiveKitCredsFromEnv(t *testing.T) {
	if got := (liveKitCreds{}).missing(); len(got) != 3 {
		t.Errorf("empty creds missing = %v, want all three", got)
	}
	full := liveKitCredsFromEnv([]string{"X=1", "LIVEKIT_URL=wss://x", "LIVEKIT_API_KEY=k", "LIVEKIT_API_SECRET=s"})
	if got := full.missing(); len(got) != 0 {
		t.Errorf("full creds missing = %v", got)
	}
	// Last value wins (a repo .env is appended after the ambient env).
	over := liveKitCredsFromEnv([]string{"LIVEKIT_API_KEY=old", "LIVEKIT_API_KEY=new"})
	if over.APIKey != "new" {
		t.Errorf("APIKey = %q, want new (last wins)", over.APIKey)
	}
}

// TestLiveKitDevMux (V5): the dev server serves the livekit client at /, the
// vendored UMD next to it, the token endpoint, and 404s elsewhere.
func TestLiveKitDevMux(t *testing.T) {
	mux := liveKitDevMux(liveKitCreds{URL: "wss://x", APIKey: "k", APISecret: "s"}, "remy-dev")
	get := func(path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		return rr
	}
	for _, tc := range []struct{ path, want string }{
		{"/", "unmute dev"},
		{"/livekit-client.umd.js", "LivekitClient"},
		{"/api/token", `"room"`},
	} {
		rr := get(tc.path)
		if rr.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", tc.path, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), tc.want) {
			t.Errorf("GET %s body missing %q", tc.path, tc.want)
		}
	}
	if rr := get("/nope"); rr.Code != http.StatusNotFound {
		t.Errorf("GET /nope = %d, want 404", rr.Code)
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

// TestLiveKitServerPlan (V9/V10): explicit creds win and spawn nothing; no URL
// falls back to the local dev server — reuse an open :7880, else spawn the
// binary, else the install prompt. All probes faked, hermetic by construction.
func TestLiveKitServerPlan(t *testing.T) {
	closed := func(string) bool { return false }
	open := func(string) bool { return true }
	found := func(string) (string, error) { return "/opt/bin/livekit-server", nil }
	absent := func(string) (string, error) { return "", errors.New("not in PATH") }

	explicit := []string{"LIVEKIT_URL=wss://x.cloud", "LIVEKIT_API_KEY=k", "LIVEKIT_API_SECRET=s"}
	plan, err := liveKitServerPlan(explicit, closed, absent)
	if err != nil {
		t.Fatal(err)
	}
	if plan.creds.URL != "wss://x.cloud" || plan.inject || plan.spawnPath != "" || plan.reused {
		t.Errorf("explicit creds plan = %+v, want verbatim creds and nothing local", plan)
	}

	// URL set but secret missing: naming error, never the local fallback (C7).
	_, err = liveKitServerPlan([]string{"LIVEKIT_URL=wss://x.cloud", "LIVEKIT_API_KEY=k"}, open, found)
	if err == nil || !strings.Contains(err.Error(), "LIVEKIT_API_SECRET") {
		t.Errorf("partial creds error = %v", err)
	}

	// No URL, something already on :7880 → reuse with dev creds, no spawn.
	plan, err = liveKitServerPlan(nil, open, absent)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.reused || !plan.inject || plan.spawnPath != "" ||
		plan.creds.URL != liveKitLocalURL || plan.creds.APIKey != liveKitDevKey {
		t.Errorf("reuse plan = %+v", plan)
	}

	// No URL, port closed, binary present → spawn it.
	plan, err = liveKitServerPlan(nil, closed, found)
	if err != nil {
		t.Fatal(err)
	}
	if plan.spawnPath != "/opt/bin/livekit-server" || !plan.inject || plan.reused {
		t.Errorf("spawn plan = %+v", plan)
	}

	// No URL, port closed, no binary → the V10 install prompt.
	_, err = liveKitServerPlan(nil, closed, absent)
	if err == nil || !strings.Contains(err.Error(), "brew install livekit") ||
		!strings.Contains(err.Error(), "get.livekit.io") || !strings.Contains(err.Error(), "LIVEKIT_URL") {
		t.Errorf("install prompt = %v", err)
	}
}

func TestLiveKitWebAssetsEmbedded(t *testing.T) {
	for _, name := range []string{"livekit.html", "livekit-client.umd.js"} {
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
