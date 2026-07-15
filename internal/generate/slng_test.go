package generate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/scaffold"
	"github.com/slng/unmute/internal/spec"
)

func TestGenerateSLNG_golden(t *testing.T) { // V47, V48, V50, V51, V53, V54
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := scaffold.Write(dir, scaffold.Data{Name: "Support Bot"}); err != nil {
		t.Fatal(err)
	}
	writeTool(t, dir, `description: Look up an order by its ID and return the current status.
parameters:
  type: object
  properties:
    order_id:
      type: string
      description: The customer's order ID.
  required: [order_id]
needs_approval: false
handler:
  type: http
  ref: https://tools.example.com/orders/lookup
`)

	result, err := GenerateSLNG(loadSLNGInput(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if result.Filename != "Support Bot.json" {
		t.Fatalf("filename = %q, want Support Bot.json", result.Filename)
	}
	if len(result.Warnings) != 0 || len(result.OmittedTools) != 0 {
		t.Fatalf("warnings = %v omitted = %v", result.Warnings, result.OmittedTools)
	}
	for _, forbidden := range []string{
		"organisation_id",
		"template_variables",
		"sip_inbound_trunk_id",
		"execution_policy",
		"auth",
	} {
		if bytes.Contains(result.Content, []byte(`"`+forbidden+`":`)) {
			t.Fatalf("SLNG payload contains deferred field %q:\n%s", forbidden, result.Content)
		}
	}

	golden := "testdata/golden/slng.json"
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, result.Content, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("missing golden; run: go test ./internal/generate -run TestGenerateSLNG_golden -update")
	}
	if !bytes.Equal(result.Content, want) {
		t.Error("slng golden drift; run: go test ./internal/generate -run TestGenerateSLNG_golden -update")
	}
}

func TestGenerateSLNG_propagatesSimplePromptPlaceholders(t *testing.T) { // V55, V61
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := scaffold.Write(dir, scaffold.Data{Name: "Support Bot"}); err != nil {
		t.Fatal(err)
	}

	result, err := GenerateSLNG(loadSLNGInput(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result.Content, []byte(`{{user_name}}`)) {
		t.Fatalf("SLNG payload missing simple user_name placeholder:\n%s", result.Content)
	}
	for _, invalid := range []string{`{{ user_name }}`, `{{ ... }}`} {
		if bytes.Contains(result.Content, []byte(invalid)) {
			t.Fatalf("SLNG payload contains backend-invalid placeholder %q:\n%s", invalid, result.Content)
		}
	}
}

func TestGenerateSLNG_rejectsInvalidProjectName(t *testing.T) { // V49
	for _, name := range []string{"", " ", "bad/name", `bad\name`} {
		_, err := GenerateSLNG(SLNGInput{Project: ir.ProjectConfig{Name: name}})
		if err == nil {
			t.Fatalf("expected invalid name error for %q", name)
		}
	}
}

func TestGenerateSLNG_warnsAndOmitsUnsupportedTools(t *testing.T) { // V52
	result, err := GenerateSLNG(SLNGInput{
		Project: ir.ProjectConfig{Name: "support-bot"},
		Tools: []ir.ToolFile{
			{
				Name: "relative_http",
				Declaration: ir.ToolDeclaration{
					Handler: ir.ToolHandler{Type: "http", Ref: "orders"},
				},
			},
			{
				Name: "mcp_tool",
				Declaration: ir.ToolDeclaration{
					Handler: ir.ToolHandler{Type: "mcp", Ref: "orders"},
				},
			},
			{
				Name: "python_tool",
				Declaration: ir.ToolDeclaration{
					Handler: ir.ToolHandler{Type: "python", Ref: "python_tool"},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.OmittedTools) != 3 || len(result.Warnings) != 3 {
		t.Fatalf("warnings = %v omitted = %v", result.Warnings, result.OmittedTools)
	}
	for _, name := range []string{"relative_http", "mcp_tool", "python_tool"} {
		if !slicesContains(result.OmittedTools, name) {
			t.Fatalf("omitted missing %s: %v", name, result.OmittedTools)
		}
	}
	if bytes.Contains(result.Content, []byte(`"tools"`)) {
		t.Fatalf("tools field should be omitted when no tool maps:\n%s", result.Content)
	}
}

func TestGenerateSLNG_rejectsInvalidWebhook(t *testing.T) { // V52, V53
	_, err := GenerateSLNG(SLNGInput{
		Project: ir.ProjectConfig{Name: "support-bot"},
		Tools: []ir.ToolFile{
			{
				Name: "lookup_order",
				Declaration: ir.ToolDeclaration{
					Description: "Look up an order.",
					Parameters: map[string]any{
						"type":       "object",
						"properties": map[string]any{},
						"required":   []string{"order_id"},
					},
					Handler: ir.ToolHandler{Type: "http", Ref: "https://tools.example.com/orders#frag"},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected invalid webhook error")
	}
	if !strings.Contains(err.Error(), "URL fragments are not supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateSLNG_rejectsInvalidParameters(t *testing.T) { // V53
	_, err := GenerateSLNG(SLNGInput{
		Project: ir.ProjectConfig{Name: "support-bot"},
		Tools: []ir.ToolFile{
			{
				Name: "lookup_order",
				Declaration: ir.ToolDeclaration{
					Description: "Look up an order.",
					Parameters: map[string]any{
						"type":       "object",
						"properties": map[string]any{},
						"required":   []string{"order_id"},
					},
					Handler: ir.ToolHandler{Type: "http", Ref: "https://tools.example.com/orders"},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected invalid parameters error")
	}
	if !strings.Contains(err.Error(), "parameters.required contains keys not present") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func loadSLNGInput(t *testing.T, dir string) SLNGInput {
	t.Helper()
	project, err := spec.LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := spec.LoadAgentConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	compliance, err := spec.LoadComplianceConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	idle, err := spec.LoadIdleNudgesConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	interruption, err := spec.LoadInterruptionConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	stt, llm, tts, err := spec.LoadModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	variables, err := spec.LoadVariables(dir)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := spec.ComposePrompt(dir)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := spec.LoadTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	return SLNGInput{
		Project:      project,
		Prompt:       prompt,
		Agent:        agent,
		Compliance:   compliance,
		Idle:         idle,
		Interruption: interruption,
		STT:          stt,
		LLM:          llm,
		TTS:          tts,
		Variables:    variables,
		Tools:        tools,
	}
}

func writeTool(t *testing.T, dir string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "agent", "tools", "lookup_order.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func slicesContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
