package skill

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite the installed-tree golden")

var goldenPath = filepath.Join("testdata", "golden", "skill_install.txt")

// TestInstalledTreeMatchesGolden pins what a fresh install puts on disk. The
// contents of each file move constantly while the bundle is being written; the
// shape of the tree is the thing that must not move by accident, because it is
// what the assistants read and what the documentation describes.
//
// Re-capture after an intentional change:
//
//	go test ./internal/skill -run TestInstalledTreeMatchesGolden -update
func TestInstalledTreeMatchesGolden(t *testing.T) {
	project := t.TempDir()
	bundle := New("test")
	for _, dest := range []Destination{Canonical, Pointer} {
		plan, err := bundle.Plan(project, dest, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := bundle.Apply(plan); err != nil {
			t.Fatal(err)
		}
	}

	var got strings.Builder
	err := filepath.WalkDir(project, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(project, path)
		if err != nil {
			return err
		}
		fmt.Fprintln(&got, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != string(want) {
		t.Errorf("the installed tree no longer matches %s.\nRe-capture with:\n\tgo test ./internal/skill -run TestInstalledTreeMatchesGolden -update\n\ngot:\n%s\nwant:\n%s", goldenPath, got.String(), want)
	}
}
