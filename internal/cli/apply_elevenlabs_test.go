package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/slng/unmute/internal/ir"
)

// TestApplyElevenLabsPushesBranchAwarePlan drives `unmute apply` against a mock
// ElevenLabs config plane and verifies the ordered, credential-authenticated,
// id-capturing push (driver-elevenlabs T6/T7, C5, V10). No network, no secrets
// in the body.
func TestApplyElevenLabsPushesBranchAwarePlan(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	var branches []string
	var authed bool
	count := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		if r.Header.Get("xi-api-key") == "test-key" {
			authed = true
		}
		count++
		id := "agent_" + string(rune('0'+count))
		bodies = append(bodies, body)
		branches = append(branches, r.URL.Query().Get("branch_id"))
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"agent_id": id})
	}))
	defer srv.Close()

	// Point the driver's config-plane base at the mock; restore after.
	prev := providerBaseURL[ir.ProviderElevenLabs]
	providerBaseURL[ir.ProviderElevenLabs] = srv.URL
	defer func() { providerBaseURL[ir.ProviderElevenLabs] = prev }()

	t.Setenv("ELEVENLABS_API_KEY", "test-key")
	t.Setenv("LOOKUP_CUSTOMER_URL", "https://hooks.example/lookup")
	t.Setenv("GET_INVOICE_URL", "https://hooks.example/invoice")

	safe := filepath.Join("..", "..", "examples", "safe_core")
	out, err := run(t, "apply", safe, "--target", "elevenlabs-prod")
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}

	if !authed {
		t.Fatal("credential was not sent in the xi-api-key header")
	}
	if len(bodies) != 2 {
		t.Fatalf("want 2 create calls, got %d\n%s", len(bodies), out)
	}
	// billing is created before intake (its captured id is referenced there).
	if name := bodies[0]["name"]; name != "elevenlabs-prod-billing" {
		t.Fatalf("first create was %v, want billing first", name)
	}
	if name := bodies[1]["name"]; name != "elevenlabs-prod-intake" {
		t.Fatalf("second create was %v, want intake", name)
	}

	// intake's transfer_to_agent must carry billing's captured id, not a placeholder.
	rule := transferRule(t, bodies[1], "transfer_to_agent")
	if got := rule["agent_id"]; got != "agent_1" {
		t.Fatalf("transfer_to_agent agent_id = %v, want captured agent_1", got)
	}

	// {{env:...}} in a webhook tool url resolves at apply time.
	billingTool := webhookTool(t, bodies[0], "get_invoice")
	if url := billingTool["api_schema"].(map[string]any)["url"]; url != "https://hooks.example/invoice" {
		t.Fatalf("env placeholder not resolved: %v", url)
	}

	// No branch pin -> main branch (empty branch_id query param).
	for i, b := range branches {
		if b != "" {
			t.Fatalf("step %d targeted branch %q, want main (no pin)", i+1, b)
		}
	}
}

func transferRule(t *testing.T, body map[string]any, toolName string) map[string]any {
	t.Helper()
	for _, raw := range promptTools(t, body) {
		tool := raw.(map[string]any)
		if tool["type"] == "system" && tool["name"] == toolName {
			return tool["params"].(map[string]any)["transfers"].([]any)[0].(map[string]any)
		}
	}
	t.Fatalf("no %s system tool in %v", toolName, body)
	return nil
}

func webhookTool(t *testing.T, body map[string]any, name string) map[string]any {
	t.Helper()
	for _, raw := range promptTools(t, body) {
		tool := raw.(map[string]any)
		if tool["name"] == name && tool["type"] == "webhook" {
			return tool
		}
	}
	t.Fatalf("no webhook tool %q in %v", name, body)
	return nil
}

func promptTools(t *testing.T, body map[string]any) []any {
	t.Helper()
	cc := body["conversation_config"].(map[string]any)
	ab := cc["agent"].(map[string]any)
	p := ab["prompt"].(map[string]any)
	tools, _ := p["tools"].([]any)
	return tools
}
