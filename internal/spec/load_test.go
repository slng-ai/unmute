package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSafeCore(t *testing.T) { // V3, V14
	pkg, err := Load(filepath.Join("..", "..", "examples", "safe_core"))
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

func TestSchemaIsDerivedFromPackage(t *testing.T) {
	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	if schema.Properties["agent"] == nil || schema.Properties["targets"] == nil {
		t.Fatal("derived authoring schema is missing package files")
	}
}
