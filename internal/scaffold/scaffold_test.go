package scaffold

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

var update = flag.Bool("update", false, "rewrite golden files")

// manifest renders the created tree to a single deterministic blob: every file
// sorted, prefixed by its relative path.
func manifest(t *testing.T, dir string, created []string) []byte {
	t.Helper()
	sort.Strings(created)
	var b bytes.Buffer
	for _, p := range created {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			t.Fatal(err)
		}
		c, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString("=== " + rel + " ===\n")
		b.Write(c)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func TestWrite_golden(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	created, err := Write(dir, Data{Name: "support-bot"})
	if err != nil {
		t.Fatal(err)
	}
	got := manifest(t, dir, created)

	golden := "testdata/golden/init.txt"
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("missing golden; run: go test ./internal/scaffold -update")
	}
	if !bytes.Equal(got, want) {
		t.Errorf("scaffold drift; run: go test ./internal/scaffold -update")
	}
}

func TestWrite_customData(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	data := Data{Name: "support-bot", Greeting: `Hello \ caller?`, Instructions: "Be brief and warm."}
	if _, err := Write(dir, data); err != nil {
		t.Fatal(err)
	}

	agentYAML, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(agentYAML, &parsed); err != nil {
		t.Fatal(err)
	}
	greeting := parsed["conversation"].(map[string]any)["greeting"].(map[string]any)
	if greeting["text"] != data.Greeting {
		t.Errorf("greeting.text = %v, want %q", greeting["text"], data.Greeting)
	}

	instructions, err := os.ReadFile(filepath.Join(dir, "instructions.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(instructions)) != data.Instructions {
		t.Errorf("instructions.md = %q, want %q", strings.TrimSpace(string(instructions)), data.Instructions)
	}
}

func TestWrite_deterministic(t *testing.T) {
	a := filepath.Join(t.TempDir(), "x")
	b := filepath.Join(t.TempDir(), "x")
	ca, err := Write(a, Data{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	cb, err := Write(b, Data{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(manifest(t, a, ca), manifest(t, b, cb)) {
		t.Error("two renders of the same name differ; output is not deterministic")
	}
}

func TestWrite_refusesNonEmpty(t *testing.T) {
	dir := t.TempDir() // exists and (after first write) non-empty
	if _, err := Write(dir, Data{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir, Data{Name: "x"}); err == nil {
		t.Fatal("expected refusal to overwrite a non-empty dir")
	}
}
