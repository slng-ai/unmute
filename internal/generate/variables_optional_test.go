package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// TestAnOmittedVariablesCompilesToWhatAllProduced is SC-006 end to end: the
// same package with both `variables: all` lines removed generates every byte
// the original generates, on both targets.
func TestAnOmittedVariablesCompilesToWhatAllProduced(t *testing.T) {
	source := filepath.Join("..", "testdata", "typed_inputs")
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "agent.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "      variables: all\n") != 2 {
		t.Fatal("the fixture no longer writes variables: all on both handoffs, so this gate proves nothing")
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(raw), "      variables: all\n", "")), 0o644); err != nil {
		t.Fatal(err)
	}
	build := func(dir string) map[string]string {
		pkg, err := spec.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		agent, err := ir.Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for name, tgt := range agent.Targets {
			artifact, err := Generate(agent, tgt, target.Default())
			if err != nil {
				t.Fatalf("generate %s: %v", name, err)
			}
			for _, file := range artifact.Files {
				out[name+"/"+file.Path] = string(file.Content)
			}
		}
		return out
	}
	with, without := build(source), build(root)
	if len(with) == 0 || len(with) != len(without) {
		t.Fatalf("the two compiles wrote %d and %d files", len(with), len(without))
	}
	for path, content := range with {
		if without[path] != content {
			t.Errorf("%s differs once variables: all is left out", path)
		}
	}
}
