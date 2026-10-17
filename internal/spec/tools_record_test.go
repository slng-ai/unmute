package spec

import (
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/stateschema"
)

var updateToolRecordings = flag.Bool("update", false, "re-record every version 2 package's tool types through uv")

var versionTwo = regexp.MustCompile(`(?m)^version:\s*2\s*$`)

// v2Packages is every version 2 package in the tree that has a local tool,
// loaded, with the tool refs its read is filed under.
func v2Packages(t *testing.T) map[string]*Package {
	t.Helper()
	out := map[string]*Package{}
	for _, top := range []string{"examples", "internal/testdata", "internal/voice-agents-tests"} {
		err := filepath.WalkDir(filepath.Join("..", "..", top), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && (entry.Name() == "build" || strings.HasPrefix(entry.Name(), ".")) {
				return filepath.SkipDir
			}
			if entry.IsDir() || entry.Name() != "agent.yaml" {
				return nil
			}
			source, err := os.ReadFile(path)
			if err != nil || !versionTwo.Match(source) {
				return err
			}
			pkg, err := Load(filepath.Dir(path))
			if err != nil {
				t.Errorf("%s: %v", path, err)
				return nil
			}
			if len(pkg.localToolRefs()) > 0 {
				out[filepath.Dir(path)] = pkg
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// TestRecordTools re-records every version 2 package's tool types through uv.
// It runs only with -update, because it needs uv and the network the first
// time. It lives here rather than in stateschema because finding a package's
// tools needs Load, and stateschema cannot import this package.
func TestRecordTools(t *testing.T) {
	if !*updateToolRecordings {
		t.Skip("run with -update to re-record through uv")
	}
	dir := stateschema.RecordedToolsInRepo()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, pkg := range v2Packages(t) {
		refs := pkg.localToolRefs()
		out, err := stateschema.UV{}.RunTools(t.Context(), pkg.Root, pkg.StateSource != nil, refs)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		rel, _ := filepath.Rel(filepath.Join("..", ".."), path)
		digest := stateschema.ToolDigest(pkg.StateSource, refs)
		data, err := json.MarshalIndent(map[string]any{"source": filepath.ToSlash(rel), "digest": digest, "report": json.RawMessage(out)}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, digest+".json"), append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestEveryV2ToolIsRecorded fails when a handler, a precheck or state.py was
// edited, or a pin moved, without recording again. It needs no Python.
func TestEveryV2ToolIsRecorded(t *testing.T) {
	recorded := stateschema.Recorded{Dir: stateschema.RecordedInRepo().Dir}
	for path, pkg := range v2Packages(t) {
		if _, err := recorded.ReadTools(t.Context(), path, pkg.StateSource, pkg.localToolRefs()); err != nil {
			t.Error(err)
		}
	}
}

// TestNoOrphanToolRecording fails on a recording no package matches any more.
func TestNoOrphanToolRecording(t *testing.T) {
	live := map[string]bool{}
	for _, pkg := range v2Packages(t) {
		live[stateschema.ToolDigest(pkg.StateSource, pkg.localToolRefs())] = true
	}
	recorded, err := filepath.Glob(filepath.Join(stateschema.RecordedToolsInRepo(), "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range recorded {
		if digest := strings.TrimSuffix(filepath.Base(path), ".json"); !live[digest] {
			t.Errorf("%s matches no package; run `go test ./internal/spec -run TestRecordTools -update`", path)
		}
	}
}
