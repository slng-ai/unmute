package cli

import (
	"encoding/base64"
	"encoding/json"
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
		defer res.Body.Close()
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
