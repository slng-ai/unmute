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
	if pkg.Agent.EntryAgent != "intake" || len(pkg.Targets) != 5 {
		t.Fatalf("unexpected package: entry=%q targets=%d", pkg.Agent.EntryAgent, len(pkg.Targets))
	}
	if _, ok := pkg.Tools["lookup_customer"]; !ok {
		t.Fatal("tool filename was not used as its name")
	}
	if !strings.Contains(pkg.Markdown["instructions.md"], "front desk") {
		t.Fatal("instructions.md was not loaded by path")
	}
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
	if schema.Properties["agent"] == nil || schema.Properties["targets"] == nil {
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
