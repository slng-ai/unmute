package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSafeCore(t *testing.T) { // V3, V14
	pkg, err := Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Agent.EntryAgent != "intake" || len(pkg.Targets) != 4 {
		t.Fatalf("unexpected package: entry=%q targets=%d", pkg.Agent.EntryAgent, len(pkg.Targets))
	}
	if _, ok := pkg.Tools["lookup_customer"]; !ok {
		t.Fatal("tool filename was not used as its name")
	}
	if got := pkg.Connections["primary_phone"].Environment["auth_token"]; got != "TWILIO_AUTH_TOKEN" {
		t.Fatalf("connection auth_token = %q", got)
	}
	if !strings.Contains(pkg.Markdown["instructions.md"], "front desk") {
		t.Fatal("instructions.md was not loaded by path")
	}
}

func TestLoadConnectionsStrictAndPathSafe(t *testing.T) { // telephony V1, V3
	newPackage := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "connections"), 0o700); err != nil {
			t.Fatal(err)
		}
		for name, content := range map[string]string{
			"agent.yaml":   "version: 1\nentry_agent: intake\nmodels: {}\nagents: {}\nchannels: {}\n",
			"targets.yaml": "targets: {}\n",
		} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}

	t.Run("strict fields", func(t *testing.T) {
		dir := newPackage(t)
		path := filepath.Join(dir, "connections", "primary.yaml")
		if err := os.WriteFile(path, []byte("kind: telephony\nprovider: twilio\nenvironment: {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(dir)
		if err == nil || !strings.Contains(err.Error(), "connections/primary.yaml") || !strings.Contains(err.Error(), "provider") {
			t.Fatalf("want positioned unknown-field error, got %v", err)
		}
	})

	t.Run("symlink escape", func(t *testing.T) {
		dir := newPackage(t)
		outside := filepath.Join(t.TempDir(), "outside.yaml")
		if err := os.WriteFile(outside, []byte("kind: telephony\nenvironment: {token: TOKEN}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "connections", "escape.yaml")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "escape.yaml") {
			t.Fatalf("want path escape error, got %v", err)
		}
	})
}

func TestLoadRejectsUnknownFieldWithPosition(t *testing.T) { // V3
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("version: 1\nentry_agent: intake\nmisspelled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "agent.yaml") || !strings.Contains(err.Error(), "3:1") {
		t.Fatalf("want filename and line:col, got %v", err)
	}
}

func TestLoadRejectsUnknownTracingField(t *testing.T) { // V25
	dir := t.TempDir()
	yaml := "version: 1\nentry_agent: intake\ntracing:\n  provider: langfuse\n  sample_rate: 1\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "sample_rate") || !strings.Contains(err.Error(), "5:3") {
		t.Fatalf("want unknown tracing field with position, got %v", err)
	}
}

func TestLoadRejectsRetiredPipelineBlock(t *testing.T) { // V3, V22 (N15)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("version: 1\nentry_agent: intake\npipeline:\n  listen: { placement: api }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "agent.yaml") || !strings.Contains(err.Error(), "pipeline") {
		t.Fatalf("want old pipeline block rejected with position, got %v", err)
	}
}

// TestV24_AgentRefsAreThinkAndSpeakOnly freezes the agent shape (B5): listen
// and turn are per-call plumbing selected at package level, never per agent.
// If AgentDef ever grows such a field, this strict-decode rejection vanishes
// and the B5 decision must be consciously revisited in SCHEMA.md first.
func TestV24_AgentRefsAreThinkAndSpeakOnly(t *testing.T) {
	dir := t.TempDir()
	yaml := "version: 1\nentry_agent: a\nagents:\n  a:\n    instructions: i.md\n    model: m\n    voice: v\n    listen: transcriber\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "agent.yaml") || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("want agent-level listen rejected with position, got %v", err)
	}
}

func TestSchemaIsDerivedFromPackage(t *testing.T) {
	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	if schema.Properties["agent"] == nil || schema.Properties["connections"] == nil || schema.Properties["targets"] == nil {
		t.Fatal("derived authoring schema is missing package files")
	}
	content, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"tracing"`) {
		t.Fatal("derived authoring schema is missing tracing")
	}
}

// writeToolPackage lays down the smallest package whose only tool is the given
// file body, so a tool-shape error is the only thing Load can report.
func writeToolPackage(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"agent.yaml": "version: 1\nentry_agent: intake\n" +
			"models:\n  think:\n    m: { provider: openai, model: gpt-4o-mini }\n" +
			"  speak:\n    v: { provider: slng, model: \"slng/deepgram/aura:2-en\", voice: aura-2-thalia-en }\n" +
			"agents:\n  intake:\n    instructions: instructions.md\n    model: m\n    voice: v\n    tools: [probe]\n" +
			"tools: [probe]\nchannels:\n  web: { kind: realtime_audio }\n",
		"instructions.md":  "Be brief.\n",
		"targets.yaml":     "targets:\n  livekit:\n    provider: livekit\n    version: \"1.5.2\"\n    sdk_language: python\n",
		"tools/probe.yaml": body,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestLoadToolShape covers the execution-block shape checks (SCHEMA §5.2): the
// retired flat keys read as migration instructions, an empty or duplicated block
// is named with its line, and every legal single-block form loads.
func TestLoadToolShape(t *testing.T) {
	const head = "description: Probe.\ninput: { type: object }\n"
	for _, tc := range []struct {
		name, body, want string
	}{
		{"flat execution", head + "\nexecution: webhook\nurl_env: PROBE_URL\n", "no longer a top-level field"},
		{"flat url_env", head + "\nwebhook:\n  url_env: PROBE_URL\nurl_env: PROBE_URL\n", "move url_env inside"},
		{"flat token_env", head + "\nwebhook:\n  url_env: PROBE_URL\ntoken_env: PROBE_TOKEN\n", "move token_env under"},
		{"scalar builtin", "description: Probe.\n\nbuiltin: end_call\n", "`builtin:` is a block now"},
		{"no block", head, "no execution block"},
		{"two blocks", head + "\nwebhook:\n  url_env: PROBE_URL\nlocal:\n  handler: tools/probe.py\n", "two execution blocks"},
		{"empty webhook block", head + "\nwebhook:\n", "block is empty"},
		{"bare client block", head + "\nclient:\n", "needs an explicit empty body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeToolPackage(t, tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
			if !strings.Contains(err.Error(), "tools/probe.yaml") {
				t.Errorf("error must name the file: %v", err)
			}
		})
	}

	for _, tc := range []struct {
		name, body string
		kind       string
	}{
		{"webhook", head + "\nwebhook:\n  url_env: PROBE_URL\n", "webhook"},
		{"webhook with auth", head + "\nwebhook:\n  url_env: PROBE_URL\n  auth:\n    type: bearer\n    token_env: PROBE_TOKEN\n", "webhook"},
		{"quoted block key", head + "\n\"webhook\":\n  url_env: PROBE_URL\n", "webhook"},
		{"inline builtin block", "description: Probe.\n\nbuiltin: { id: end_call }\n", "builtin"},
		{"explicit empty client", head + "\nclient: {}\n", "client"},
		{"mcp", head + "\nmcp:\n  url_env: PROBE_MCP_URL\n", "mcp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg, err := Load(writeToolPackage(t, tc.body))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := pkg.Tools["probe"].ExecutionKind(); got != tc.kind {
				t.Errorf("execution kind = %q, want %q", got, tc.kind)
			}
		})
	}
}
