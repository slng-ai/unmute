package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/slng/unmute/internal/ir"
)

// TestApplyElevenLabsPushesToolThenAgentPlan drives `unmute apply` against a
// mock ElevenLabs config plane and verifies the ordered, credential-authenticated
// push: standalone workspace tools first (POST /v1/convai/tools), then agents
// (POST /v1/convai/agents/create) whose prompt.tool_ids and
// built_in_tools.transfer_to_agent reference the captured ids. No network, no
// secrets in the body (driver-elevenlabs T6/T7, C5, C9, V10).
func TestApplyElevenLabsPushesToolThenAgentPlan(t *testing.T) {
	var mu sync.Mutex
	toolConfigs := map[string]map[string]any{} // tool name -> tool_config
	agentBodies := map[string]map[string]any{} // agent name -> body
	var agentOrder []string                    // agent create order
	var authed bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("xi-api-key") == "test-key" {
			authed = true
		}
		if strings.Contains(r.URL.Path, "/convai/tools") {
			cfg := body["tool_config"].(map[string]any)
			name := cfg["name"].(string)
			toolConfigs[name] = cfg
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "toolid_" + name})
			return
		}
		name, _ := body["name"].(string)
		agentBodies[name] = body
		agentOrder = append(agentOrder, name)
		_ = json.NewEncoder(w).Encode(map[string]string{"agent_id": "agentid_" + name})
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

	// Webhook tools are created as standalone workspace resources.
	if len(toolConfigs) != 2 {
		t.Fatalf("want 2 tool creates, got %d: %v", len(toolConfigs), toolConfigs)
	}
	invoice, ok := toolConfigs["get_invoice"]
	if !ok {
		t.Fatalf("get_invoice tool not created: %v", toolConfigs)
	}
	// {{env:...}} resolves in the tool's api_schema.url at apply time.
	if url := invoice["api_schema"].(map[string]any)["url"]; url != "https://hooks.example/invoice" {
		t.Fatalf("env placeholder not resolved in tool url: %v", url)
	}

	// billing is created before intake (its captured id is referenced there).
	if len(agentOrder) != 2 || agentOrder[0] != "elevenlabs-prod-billing" || agentOrder[1] != "elevenlabs-prod-intake" {
		t.Fatalf("agent create order = %v, want billing then intake", agentOrder)
	}

	// intake's transfer_to_agent carries billing's captured agent id, not a placeholder.
	rule := transferRule(t, agentBodies["elevenlabs-prod-intake"], "transfer_to_agent")
	if got := rule["agent_id"]; got != "agentid_elevenlabs-prod-billing" {
		t.Fatalf("transfer_to_agent agent_id = %v, want captured billing id", got)
	}

	// prompt.tool_ids reference the captured workspace tool ids, not tool names.
	if ids := toolIDs(t, agentBodies["elevenlabs-prod-billing"]); len(ids) != 1 || ids[0] != "toolid_get_invoice" {
		t.Fatalf("billing tool_ids = %v, want [toolid_get_invoice]", ids)
	}
	if ids := toolIDs(t, agentBodies["elevenlabs-prod-intake"]); len(ids) != 1 || ids[0] != "toolid_lookup_customer" {
		t.Fatalf("intake tool_ids = %v, want [toolid_lookup_customer]", ids)
	}
}

func promptBlock(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	cc := body["conversation_config"].(map[string]any)
	ab := cc["agent"].(map[string]any)
	return ab["prompt"].(map[string]any)
}

func transferRule(t *testing.T, body map[string]any, toolName string) map[string]any {
	t.Helper()
	bi, _ := promptBlock(t, body)["built_in_tools"].(map[string]any)
	tool, ok := bi[toolName].(map[string]any)
	if !ok {
		t.Fatalf("no %s built-in tool in %v", toolName, bi)
	}
	return tool["params"].(map[string]any)["transfers"].([]any)[0].(map[string]any)
}

func toolIDs(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, _ := promptBlock(t, body)["tool_ids"].([]any)
	ids := make([]string, len(raw))
	for i, v := range raw {
		ids[i] = v.(string)
	}
	return ids
}
