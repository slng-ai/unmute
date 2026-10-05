package stateschema

import (
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var updateRecorded = flag.Bool("update", false, "re-record every state.py report through uv")

const recordedDir = "testdata/recorded"

// repoRoot is two levels up from this package.
const repoRoot = "../.."

// stateFiles is every state.py the tree holds, plus the reader's own test
// models, relative to the repo root. Each needs a recorded report, because
// every test that loads one reads it through Recorded.
func stateFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, top := range []string{"examples", "internal/testdata", "internal/voice-agents-tests"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, top), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && (entry.Name() == "build" || strings.HasPrefix(entry.Name(), ".")) {
				return filepath.SkipDir
			}
			if !entry.IsDir() && entry.Name() == FileName {
				rel, _ := filepath.Rel(repoRoot, path)
				out = append(out, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	models, err := filepath.Glob("testdata/models/*.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range models {
		out = append(out, "internal/stateschema/"+filepath.ToSlash(model))
	}
	slices.Sort(out)
	return out
}

func readSource(t *testing.T, rel string) []byte {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatal(err)
	}
	return source
}

// TestRecord re-records every report through uv. It runs only with -update,
// because it needs uv and the network the first time.
func TestRecord(t *testing.T) {
	if !*updateRecorded {
		t.Skip("run with -update to re-record through uv")
	}
	if err := os.RemoveAll(recordedDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(recordedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range stateFiles(t) {
		path, err := filepath.Abs(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatal(err)
		}
		out, err := UV{}.Run(t.Context(), path)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		source := readSource(t, rel)
		data, err := json.MarshalIndent(recording{Source: rel, Digest: Digest(source), Report: out}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(recordedDir, Digest(source)+".json"), append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestUVRefusesWhatOnlyPythonSees runs with -update too: the cases no report
// can carry, because state.py fails before a report is printed.
func TestUVRefusesWhatOnlyPythonSees(t *testing.T) {
	if !*updateRecorded {
		t.Skip("run with -update; it needs uv")
	}
	for _, tc := range []struct{ name, source, want string }{
		{"shadow", "from pydantic import BaseModel\n\n\nclass State(BaseModel):\n    json: str = \"\"\n",
			`state.py:4: does not import: UserWarning: Field name "json" in "State" shadows an attribute in parent "BaseModel"`},
		{"no state", "x = 1\n", "state.py defines no class State"},
		{"sibling import", "import helpers\n", "No module named 'helpers'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, FileName), []byte(tc.source), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := UV{}.Read(t.Context(), dir, []byte(tc.source))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want it to say %q", err, tc.want)
			}
		})
	}
}

// TestEveryStatePyIsRecorded fails when a state.py was edited, or a pin moved,
// without recording again. It needs no Python, which is the point: the digest
// changes, so the recording goes missing.
func TestEveryStatePyIsRecorded(t *testing.T) {
	for _, rel := range stateFiles(t) {
		digest := Digest(readSource(t, rel))
		if _, err := os.Stat(filepath.Join(recordedDir, digest+".json")); err != nil {
			t.Errorf("%s has no recorded report; run `go test ./internal/stateschema -run TestRecord -update` (needs uv)", rel)
		}
	}
}

// TestEveryPackageStateReads holds every package's state.py to the rules a
// call depends on. The reader's own test models are left out, because most of
// them exist to be refused.
func TestEveryPackageStateReads(t *testing.T) {
	for _, rel := range stateFiles(t) {
		if strings.HasPrefix(rel, "internal/stateschema/") {
			continue
		}
		model, err := Recorded{Dir: recordedDir}.Read(t.Context(), filepath.Dir(rel), readSource(t, rel))
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		for _, field := range model.Fields {
			if field.Required {
				t.Errorf("%s: State.%s has no default", rel, field.Name)
			}
		}
	}
}

// TestNoOrphanRecording fails on a recording no file matches any more.
func TestNoOrphanRecording(t *testing.T) {
	live := map[string]bool{}
	for _, rel := range stateFiles(t) {
		live[Digest(readSource(t, rel))] = true
	}
	recorded, err := filepath.Glob(filepath.Join(recordedDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range recorded {
		if digest := strings.TrimSuffix(filepath.Base(path), ".json"); !live[digest] {
			t.Errorf("%s matches no state.py; run `go test ./internal/stateschema -run TestRecord -update`", path)
		}
	}
}
