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

func TestInstalledSLNGSkillNamesCredentialsWithoutValues(t *testing.T) {
	const routerValue = "installed-router-secret-must-not-leak"
	const upstreamValue = "installed-upstream-secret-must-not-leak"
	t.Setenv("SLNG_API_KEY", routerValue)
	t.Setenv("OPENAI_API_KEY", upstreamValue)

	project := t.TempDir()
	bundle := New("test")
	plan, err := bundle.Plan(project, Canonical, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := bundle.Apply(plan); err != nil {
		t.Fatal(err)
	}
	var installed strings.Builder
	for _, name := range []string{"SKILL.md", filepath.Join("references", "models.md"), filepath.Join("references", "package.md")} {
		raw, err := os.ReadFile(filepath.Join(Canonical.Dir(project), name))
		if err != nil {
			t.Fatal(err)
		}
		installed.Write(raw)
	}
	for _, name := range []string{"SLNG_API_KEY", "OPENAI_API_KEY", "api_key_env"} {
		if !strings.Contains(installed.String(), name) {
			t.Errorf("installed skill does not name %s", name)
		}
	}
	for _, value := range []string{routerValue, upstreamValue} {
		if strings.Contains(installed.String(), value) {
			t.Error("installed skill contains an environment value")
		}
	}
}
