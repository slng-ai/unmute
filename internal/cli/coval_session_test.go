package cli

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slng-ai/unmute/internal/ir"
)

// tokenPayload decodes the middle segment of a hand-minted LiveKit JWT.
func tokenPayload(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	return claims
}

func dispatchEntry(t *testing.T, claims map[string]any) map[string]any {
	t.Helper()
	roomConfig, ok := claims["roomConfig"].(map[string]any)
	if !ok {
		t.Fatalf("no roomConfig in %v", claims)
	}
	agents, ok := roomConfig["agents"].([]any)
	if !ok || len(agents) != 1 {
		t.Fatalf("want one agent dispatch, got %v", roomConfig["agents"])
	}
	entry, ok := agents[0].(map[string]any)
	if !ok {
		t.Fatalf("dispatch entry is %T", agents[0])
	}
	return entry
}

// Coval drives a LiveKit agent by calling the token endpoint with the simulation
// on a custom header. The dev server is that endpoint locally, so the header has
// to reach the agent as dispatch metadata or the documented route does nothing.
func TestDevSessionCarriesTheCovalSimulationIntoDispatch(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	request.Header.Set("X-Coval-Simulation-Id", "sim-abc123")
	recorder := httptest.NewRecorder()
	devSessionHandler(ir.ProviderLiveKit, "remy-dev", "ws://127.0.0.1:7880", readyDevStream(t))(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	// map[string]any, not map[string]string: the payload also carries a bool.
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	token, _ := body["token"].(string)
	entry := dispatchEntry(t, tokenPayload(t, token))
	metadata, _ := entry["metadata"].(string)
	if !strings.Contains(metadata, "sim-abc123") {
		t.Fatalf("dispatch metadata does not carry the simulation: %q", metadata)
	}
	// The key has to be the same one the emitted agent reads for a SIP call, so
	// one resolver covers both routes.
	if !strings.Contains(metadata, "coval.simulation_id") {
		t.Fatalf("dispatch metadata uses an unexpected key: %q", metadata)
	}
}

// An ordinary browser session sends no such header, and its token must stay
// exactly what it was before Coval existed.
func TestDevSessionOmitsDispatchMetadataWithoutTheHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	devSessionHandler(ir.ProviderLiveKit, "remy-dev", "ws://127.0.0.1:7880", readyDevStream(t))(
		recorder, httptest.NewRequest(http.MethodGet, "/api/session", nil))

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	token, _ := body["token"].(string)
	if _, present := dispatchEntry(t, tokenPayload(t, token))["metadata"]; present {
		t.Fatal("a plain browser session token carries dispatch metadata")
	}
}

func TestCovalDispatchMetadataIsEmptyWithoutTheHeader(t *testing.T) {
	if got := covalDispatchMetadata(httptest.NewRequest(http.MethodGet, "/", nil)); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

// The minted token must carry metadata only when asked, so an existing caller
// cannot start emitting a field it never emitted.
func TestMintLiveKitTokenOmitsEmptyDispatchMetadata(t *testing.T) {
	token, err := mintLiveKitToken("APIx", "sx", "room", "user", "agent", "", time.Unix(1_700_000_000, 0), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := dispatchEntry(t, tokenPayload(t, token))["metadata"]; present {
		t.Fatal("empty dispatch metadata was still encoded")
	}
	token, err = mintLiveKitToken("APIx", "sx", "room", "user", "agent", `{"a":1}`, time.Unix(1_700_000_000, 0), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got := dispatchEntry(t, tokenPayload(t, token))["metadata"]; got != `{"a":1}` {
		t.Fatalf("dispatch metadata = %v", got)
	}
}
