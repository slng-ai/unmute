//go:build smoke

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/slng/unmute/internal/ir"
)

// TestSmokeElevenLabsAgentRoundTrip creates a throwaway agent in the live
// ElevenLabs workspace, fetches it back, and deletes it — proving the corrected
// wire shapes work against the real config plane (create at
// POST /v1/convai/agents/create; turn.silence_end_call_timeout /
// turn.interruption_ignore_terms). Opt-in (`make smoke` / -tags smoke), skipped
// without ELEVENLABS_API_KEY; never in the default suite or PR gate (L4).
//
// Set ELEVENLABS_BASE_URL to a residency base (e.g. https://api.eu.residency.elevenlabs.io)
// when the key belongs to a regional workspace.
func TestSmokeElevenLabsAgentRoundTrip(t *testing.T) {
	key := os.Getenv("ELEVENLABS_API_KEY")
	if key == "" {
		t.Skip("ELEVENLABS_API_KEY not set")
	}
	base := providerBaseURL[ir.ProviderElevenLabs]
	if b := os.Getenv("ELEVENLABS_BASE_URL"); b != "" {
		base = b
	}
	client := &http.Client{Timeout: 30 * time.Second}

	do := func(method, path string, body []byte) (int, []byte) {
		t.Helper()
		var r io.Reader
		if body != nil {
			r = bytes.NewReader(body)
		}
		req, err := http.NewRequest(method, base+path, r)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("xi-api-key", key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		payload, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, payload
	}

	createBody, err := json.Marshal(map[string]any{
		"name": "unmute-smoke-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		"conversation_config": map[string]any{
			"agent": map[string]any{
				"prompt": map[string]any{"prompt": "Throwaway smoke-test agent. Delete on sight."},
			},
			// The item-5 fix: these turn-level fields must sit under `turn`.
			"turn": map[string]any{
				"silence_end_call_timeout":  30,
				"interruption_ignore_terms": []string{"okay"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	status, payload := do("POST", "/v1/convai/agents/create", createBody)
	if status >= 300 {
		t.Fatalf("create agent: status %d: %s", status, payload)
	}
	var created struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(payload, &created); err != nil || created.AgentID == "" {
		t.Fatalf("create response has no agent_id: %s", payload)
	}

	// Always clean up, even if the GET assertion below fails.
	t.Cleanup(func() {
		if status, payload := do("DELETE", "/v1/convai/agents/"+created.AgentID, nil); status >= 300 {
			t.Errorf("delete agent %s: status %d: %s", created.AgentID, status, payload)
		}
	})

	if status, payload := do("GET", "/v1/convai/agents/"+created.AgentID, nil); status >= 300 {
		t.Fatalf("get agent %s: status %d: %s", created.AgentID, status, payload)
	}
}
