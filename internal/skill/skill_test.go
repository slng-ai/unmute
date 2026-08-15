package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// bundle builds a Bundle over an in-memory FS shaped like assets/, so the
// decision-table tests do not move when the real content does.
func bundle(t *testing.T, version string, files map[string]string) Bundle {
	t.Helper()
	mem := fstest.MapFS{}
	for name, content := range files {
		mem[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return Bundle{FS: mem, Version: version}
}

func twoFileBundle(t *testing.T, version string) Bundle {
	t.Helper()
	return bundle(t, version, map[string]string{
		"assets/SKILL.md":            "entry " + version + "\n",
		"assets/references/tools.md": "tools\n",
		"assets/pointer/SKILL.md":    "pointer\n",
	})
}

func TestHashIsLowercaseHexSHA256(t *testing.T) {
	// The empty string's SHA-256, so a change of algorithm or encoding fails
	// here rather than silently invalidating every manifest in the wild.
	if got, want := Hash(nil), "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"; got != want {
		t.Errorf("Hash(nil) = %q, want %q", got, want)
	}
	if Hash([]byte("a")) == Hash([]byte("b")) {
		t.Error("different content hashed the same")
	}
}

func TestManifestRoundTripsAcrossSeparators(t *testing.T) {
	dir := t.TempDir()
	want := Manifest{Version: "1.2.3", Files: map[string]string{
		"SKILL.md":            Hash([]byte("a")),
		"references/tools.md": Hash([]byte("b")),
	}}
	if err := writeManifest(dir, want); err != nil {
		t.Fatal(err)
	}

	// Forward slashes on disk, whatever the platform, so a manifest written on
	// Windows still matches on macOS.
	raw, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `\\`) {
		t.Errorf("manifest carries a backslash separator:\n%s", raw)
	}
	var onDisk Manifest
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if _, ok := onDisk.Files["references/tools.md"]; !ok {
		t.Errorf("manifest keys are not forward-slashed: %v", onDisk.Files)
	}

	got, ok, err := readManifest(dir)
	if err != nil || !ok {
		t.Fatalf("readManifest: ok=%v err=%v", ok, err)
	}
	if got.Version != want.Version || len(got.Files) != len(want.Files) {
		t.Errorf("round trip lost data: %+v", got)
	}
}

func TestManifestNeverListsItself(t *testing.T) {
	dir := t.TempDir()
	if err := writeManifest(dir, Manifest{Version: "1", Files: map[string]string{manifestName: "x", "SKILL.md": "y"}}); err != nil {
		t.Fatal(err)
	}
	got, _, err := readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, listed := got.Files[manifestName]; listed {
		t.Error("the manifest lists itself")
	}
}

func TestMissingManifestIsNotAnError(t *testing.T) {
	got, ok, err := readManifest(t.TempDir())
	if err != nil {
		t.Fatalf("a missing manifest must read as absent, not as an error: %v", err)
	}
	if ok {
		t.Error("reported a manifest that is not there")
	}
	if got.Files == nil {
		t.Error("Files must be usable without a nil check")
	}
}

func TestCorruptManifestFailsLoud(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readManifest(dir); err == nil {
		t.Error("a corrupt manifest read clean; it must fail rather than be treated as absent")
	}
}

// actions flattens a plan into path -> action, for the table below.
func actions(plan DestinationPlan) map[string]Action {
	out := map[string]Action{}
	for _, file := range plan.Files {
		out[file.Path] = file.Action
	}
	return out
}

// TestInstallDecisionTable walks every row of the table in data-model.md. This
// is the whole behaviour of the command, so every row gets a case.
func TestInstallDecisionTable(t *testing.T) {
	t.Run("absent on disk, absent from manifest, written", func(t *testing.T) {
		project := t.TempDir()
		plan, err := twoFileBundle(t, "1.0.0").Plan(project, Canonical, false)
		if err != nil {
			t.Fatal(err)
		}
		if got := actions(plan)["SKILL.md"]; got != ActionWritten {
			t.Errorf("got %q, want %q", got, ActionWritten)
		}
	})

	t.Run("same version, same bytes, left alone", func(t *testing.T) {
		project := t.TempDir()
		b := twoFileBundle(t, "1.0.0")
		install(t, b, project)
		plan, err := b.Plan(project, Canonical, false)
		if err != nil {
			t.Fatal(err)
		}
		for path, action := range actions(plan) {
			if action != ActionCurrent {
				t.Errorf("%s: got %q, want %q", path, action, ActionCurrent)
			}
		}
		if plan.Changed() {
			t.Error("a second install of the same version wants to change something")
		}
	})

	t.Run("same version, different bytes, updated", func(t *testing.T) {
		project := t.TempDir()
		install(t, twoFileBundle(t, "1.0.0"), project)
		// Same version, different content: a rebuilt binary at the same tag.
		next := bundle(t, "1.0.0", map[string]string{
			"assets/SKILL.md":            "entry rewritten\n",
			"assets/references/tools.md": "tools\n",
			"assets/pointer/SKILL.md":    "pointer\n",
		})
		plan, err := next.Plan(project, Canonical, false)
		if err != nil {
			t.Fatal(err)
		}
		if got := actions(plan)["SKILL.md"]; got != ActionUpdated {
			t.Errorf("SKILL.md: got %q, want %q", got, ActionUpdated)
		}
		if got := actions(plan)["references/tools.md"]; got != ActionCurrent {
			t.Errorf("references/tools.md: got %q, want %q", got, ActionCurrent)
		}
	})

	t.Run("version differs, upgraded and reported from", func(t *testing.T) {
		project := t.TempDir()
		install(t, twoFileBundle(t, "1.0.0"), project)
		plan, err := twoFileBundle(t, "2.0.0").Plan(project, Canonical, false)
		if err != nil {
			t.Fatal(err)
		}
		if plan.FromVersion != "1.0.0" {
			t.Errorf("FromVersion = %q, want 1.0.0", plan.FromVersion)
		}
		if got := actions(plan)["references/tools.md"]; got != ActionUpgraded {
			t.Errorf("got %q, want %q even though the bytes match: the version is what moved", got, ActionUpgraded)
		}
	})

	t.Run("hash differs from manifest, refused", func(t *testing.T) {
		project := t.TempDir()
		b := twoFileBundle(t, "1.0.0")
		install(t, b, project)
		edit(t, project, "SKILL.md", "hand edited\n")

		plan, err := b.Plan(project, Canonical, false)
		if err != nil {
			t.Fatal(err)
		}
		if got := actions(plan)["SKILL.md"]; got != ActionRefused {
			t.Errorf("got %q, want %q", got, ActionRefused)
		}
		refused := plan.Refused()
		if len(refused) != 1 || !strings.HasSuffix(refused[0], "SKILL.md") {
			t.Errorf("Refused() = %v, want the one changed file named", refused)
		}
		if err := b.Apply(plan); err == nil {
			t.Fatal("Apply overwrote a locally changed file without --force")
		}
		if got := read(t, project, "SKILL.md"); got != "hand edited\n" {
			t.Errorf("the local edit was lost: %q", got)
		}
	})

	t.Run("present with no manifest, refused", func(t *testing.T) {
		project := t.TempDir()
		dir := Canonical.Dir(project)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("someone else's\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		plan, err := twoFileBundle(t, "1.0.0").Plan(project, Canonical, false)
		if err != nil {
			t.Fatal(err)
		}
		if got := actions(plan)["SKILL.md"]; got != ActionRefused {
			t.Errorf("got %q, want %q: a directory Unmute did not write is someone else's", got, ActionRefused)
		}
	})

	t.Run("absent on disk but in the manifest, restored", func(t *testing.T) {
		project := t.TempDir()
		b := twoFileBundle(t, "1.0.0")
		install(t, b, project)
		if err := os.Remove(filepath.Join(Canonical.Dir(project), "SKILL.md")); err != nil {
			t.Fatal(err)
		}
		plan, err := b.Plan(project, Canonical, false)
		if err != nil {
			t.Fatal(err)
		}
		if got := actions(plan)["SKILL.md"]; got != ActionRestored {
			t.Errorf("got %q, want %q", got, ActionRestored)
		}
	})

	t.Run("force overwrites a changed file", func(t *testing.T) {
		project := t.TempDir()
		b := twoFileBundle(t, "1.0.0")
		install(t, b, project)
		edit(t, project, "SKILL.md", "hand edited\n")

		plan, err := b.Plan(project, Canonical, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Refused()) != 0 {
			t.Fatalf("--force still refused %v", plan.Refused())
		}
		if err := b.Apply(plan); err != nil {
			t.Fatal(err)
		}
		if got := read(t, project, "SKILL.md"); got != "entry 1.0.0\n" {
			t.Errorf("--force did not overwrite: %q", got)
		}
	})
}

// TestPlanNamesEveryRefusalAtOnce holds the rule that the whole decision set is
// computed before anything is written, so a user fixes every file in one pass.
func TestPlanNamesEveryRefusalAtOnce(t *testing.T) {
	project := t.TempDir()
	b := twoFileBundle(t, "1.0.0")
	install(t, b, project)
	edit(t, project, "SKILL.md", "one\n")
	edit(t, project, filepath.Join("references", "tools.md"), "two\n")

	plan, err := b.Plan(project, Canonical, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Refused()) != 2 {
		t.Errorf("Refused() = %v, want both files named in one message", plan.Refused())
	}
	err = b.Apply(plan)
	if err == nil {
		t.Fatal("Apply succeeded on two refusals")
	}
	for _, want := range []string{"SKILL.md", "tools.md", "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q:\n%s", want, err)
		}
	}
}

// TestStaleFileIsRemoved holds the one decision the contract does not list: a
// reference that a later version dropped would otherwise stay on disk and be
// read as current.
func TestStaleFileIsRemoved(t *testing.T) {
	project := t.TempDir()
	install(t, twoFileBundle(t, "1.0.0"), project)

	next := bundle(t, "2.0.0", map[string]string{
		"assets/SKILL.md":         "entry 2.0.0\n",
		"assets/pointer/SKILL.md": "pointer\n",
	})
	plan, err := next.Plan(project, Canonical, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := actions(plan)["references/tools.md"]; got != ActionRemoved {
		t.Errorf("got %q, want %q", got, ActionRemoved)
	}
	if err := next.Apply(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(Canonical.Dir(project), "references", "tools.md")); !os.IsNotExist(err) {
		t.Error("a reference this version no longer carries is still on disk")
	}
}

// TestApplyRollsBack proves the whole-or-nothing guarantee. A directory sitting
// where a file must go makes the write fail part way through.
func TestApplyRollsBack(t *testing.T) {
	project := t.TempDir()
	b := twoFileBundle(t, "1.0.0")
	dir := Canonical.Dir(project)
	// references/tools.md cannot be written because a directory is in its place.
	if err := os.MkdirAll(filepath.Join(dir, "references", "tools.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan, err := b.Plan(project, Canonical, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Apply(plan); err == nil {
		t.Fatal("Apply succeeded with a directory in a file's place")
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); !os.IsNotExist(err) {
		t.Error("a failed install left SKILL.md behind: it must look like no install, not a good one")
	}
	if _, err := os.Stat(filepath.Join(dir, manifestName)); !os.IsNotExist(err) {
		t.Error("a failed install left a manifest behind")
	}
}

func TestDestinationsResolvesAndDeduplicates(t *testing.T) {
	cases := []struct {
		name  string
		in    []string
		want  []string
		fails bool
	}{
		{name: "default is all", in: nil, want: []string{"canonical", "pointer"}},
		{name: "all", in: []string{"all"}, want: []string{"canonical", "pointer"}},
		{name: "claude is the pointer only", in: []string{"claude"}, want: []string{"pointer"}},
		{name: "codex is the canonical only", in: []string{"codex"}, want: []string{"canonical"}},
		{name: "three names, one directory", in: []string{"codex", "cursor", "copilot"}, want: []string{"canonical"}},
		{name: "both kinds", in: []string{"claude", "codex"}, want: []string{"pointer", "canonical"}},
		{name: "case and space tolerated", in: []string{" Codex "}, want: []string{"canonical"}},
		{name: "unknown fails", in: []string{"emacs"}, fails: true},
		{name: "unknown fails even beside a known one", in: []string{"codex", "emacs"}, fails: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Destinations(tc.in)
			if tc.fails {
				if err == nil {
					t.Fatal("wanted an error")
				}
				for _, name := range AssistantNames() {
					if !strings.Contains(err.Error(), name) {
						t.Errorf("the error does not list the supported name %q:\n%s", name, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, dest := range got {
				names = append(names, dest.Name)
			}
			if strings.Join(names, ",") != strings.Join(tc.want, ",") {
				t.Errorf("got %v, want %v", names, tc.want)
			}
		})
	}
}

func TestPointerTakesOnlyItsOwnFile(t *testing.T) {
	project := t.TempDir()
	b := twoFileBundle(t, "1.0.0")
	plan, err := b.Plan(project, Pointer, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 1 || plan.Files[0].Path != "SKILL.md" {
		t.Fatalf("the pointer destination carries %d files, want just SKILL.md: %+v", len(plan.Files), plan.Files)
	}
	if err := b.Apply(plan); err != nil {
		t.Fatal(err)
	}
	if got := read(t, project, filepath.Join("..", "..", "..", ".claude", "skills", "unmute", "SKILL.md")); got != "pointer\n" {
		t.Errorf("the pointer got the canonical body: %q", got)
	}
}

func TestCanonicalExcludesThePointerSubtree(t *testing.T) {
	plan, err := twoFileBundle(t, "1.0.0").Plan(t.TempDir(), Canonical, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range plan.Files {
		if strings.HasPrefix(file.Path, "pointer/") {
			t.Errorf("the canonical destination carries %s, which belongs to the pointer", file.Path)
		}
	}
}

func TestVersionTokenIsSubstituted(t *testing.T) {
	project := t.TempDir()
	b := bundle(t, "9.9.9", map[string]string{
		"assets/SKILL.md":         "version: " + versionToken + "\n",
		"assets/pointer/SKILL.md": "version: " + versionToken + "\n",
	})
	install(t, b, project)
	got := read(t, project, "SKILL.md")
	if strings.Contains(got, versionToken) {
		t.Errorf("the version token survived the install: %q", got)
	}
	if !strings.Contains(got, "9.9.9") {
		t.Errorf("the version did not land: %q", got)
	}
}

// TestRealBundleInstalls is the one test that touches the shipped content
// rather than a fixture, so an embed directive that stops matching the tree
// fails here.
func TestRealBundleInstalls(t *testing.T) {
	project := t.TempDir()
	b := New("test")
	for _, dest := range []Destination{Canonical, Pointer} {
		plan, err := b.Plan(project, dest, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Files) == 0 {
			t.Fatalf("%s destination is empty: the embed directive no longer matches the tree", dest.Name)
		}
		if err := b.Apply(plan); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(Canonical.Dir(project), "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	// A second run must be a no-op, which is what makes the command safe to
	// re-run and what the golden test depends on.
	plan, err := b.Plan(project, Canonical, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Changed() {
		t.Errorf("a second install of the same bundle is not a no-op: %+v", plan.Files)
	}
}

func install(t *testing.T, b Bundle, project string) {
	t.Helper()
	for _, dest := range []Destination{Canonical, Pointer} {
		plan, err := b.Plan(project, dest, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Apply(plan); err != nil {
			t.Fatal(err)
		}
	}
}

func edit(t *testing.T, project, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(Canonical.Dir(project), rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, project, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(Canonical.Dir(project), rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
