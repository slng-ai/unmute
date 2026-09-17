package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/manifest"
)

func TestEditorWords(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{`code --wait`, []string{"code", "--wait"}},
		{`"C:\Program Files\Editor\code.exe" --wait`, []string{`C:\Program Files\Editor\code.exe`, "--wait"}},
		{`"/Applications/My Editor" --wait 'two words'`, []string{"/Applications/My Editor", "--wait", "two words"}},
		{`editor escaped\ word`, []string{"editor", "escaped word"}},
	} {
		got, err := editorWords(tc.in)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: %v %v", tc.in, got, err)
		}
	}
	for _, input := range []string{"", `"open`, `editor\`} {
		if _, err := editorWords(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestManifestCreateAndUse(t *testing.T) {
	store := manifest.Store{Root: t.TempDir()}
	old := manifestStore
	manifestStore = func() (manifest.Store, error) { return store, nil }
	t.Cleanup(func() { manifestStore = old })
	editor := filepath.Join(t.TempDir(), "fake editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\n[ \"$1\" = '--wait' ] || exit 2\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", `"`+editor+`" --wait`)
	t.Setenv("EDITOR", "missing-editor")
	run := func(args ...string) (string, error) {
		cmd := newRootCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader("yes\n"))
		if len(args) > 1 && args[1] == "create" {
			args = append(args, "--editor")
		}
		cmd.SetArgs(args)
		err := cmd.Execute()
		return out.String(), err
	}
	if _, err := run("manifest", "create", "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("manifest", "create", "second"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Default()
	if err != nil || got.Name != "second" {
		t.Fatalf("default: %v %v", got, err)
	}
	if _, err := run("manifest", "use", "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("manifest", "create", "acme"); err == nil {
		t.Fatal("accepted duplicate")
	}
	if _, err := run("manifest", "create", "123"); err != nil {
		t.Fatal(err)
	}
	numeric, err := store.Load("123")
	if err != nil || numeric.Rules.Name != "123" {
		t.Fatalf("numeric company name: %v %v", numeric, err)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if _, err := run("manifest", "create", "third"); err == nil || !strings.Contains(err.Error(), "EDITOR") {
		t.Fatalf("missing editor: %v", err)
	}
}

func TestManifestInvalidEditorDraftIsKept(t *testing.T) {
	store := manifest.Store{Root: t.TempDir()}
	old := manifestStore
	manifestStore = func() (manifest.Store, error) { return store, nil }
	t.Cleanup(func() { manifestStore = old })
	editor := filepath.Join(t.TempDir(), "editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nprintf 'manifest: invalid\\nversion: 0\\n' > \"$1\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", editor)
	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"manifest", "create", "invalid", "--editor"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "draft kept at ") {
		t.Fatalf("invalid draft: %v", err)
	}
	path := strings.Split(strings.Split(err.Error(), "draft kept at ")[1], ":")[0]
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if got, err := store.Default(); err != nil || got != nil {
		t.Fatalf("default changed: %v %v", got, err)
	}
}

func TestManifestEditorFailureKeepsDraft(t *testing.T) {
	store := manifest.Store{Root: t.TempDir()}
	old := manifestStore
	manifestStore = func() (manifest.Store, error) { return store, nil }
	t.Cleanup(func() { manifestStore = old })
	editor := filepath.Join(t.TempDir(), "editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editor)
	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"manifest", "create", "acme", "--editor"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "editor failed; draft kept at ") {
		t.Fatalf("editor failure: %v", err)
	}
	path := strings.Split(strings.Split(err.Error(), "draft kept at ")[1], ":")[0]
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if got, err := store.Default(); err != nil || got != nil {
		t.Fatalf("default changed: %v %v", got, err)
	}
}

func TestGuidedManifestCreateEditAndCopies(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	store := manifest.Store{Root: t.TempDir()}
	old := manifestStore
	manifestStore = func() (manifest.Store, error) { return store, nil }
	t.Cleanup(func() { manifestStore = old })
	run := func(input string, args ...string) (string, error) {
		cmd := newRootCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader(input))
		cmd.SetArgs(args)
		err := cmd.Execute()
		return out.String(), err
	}
	if out, err := run("acme\n1\n8\n2\n", "manifest", "create"); err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	original, err := store.Load("acme")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(original.Data), "#") || original.Rules.Version != 1 {
		t.Fatalf("unexpected starter: %s", original.Data)
	}
	packageCopy := filepath.Join(t.TempDir(), "agent")
	// Picker, deployment target, Save, confirm.
	if out, err := run("1\n1\n7\n\n", "init", packageCopy, "--from-manifest"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	copied, err := os.ReadFile(filepath.Join(packageCopy, "manifest"))
	if err != nil {
		t.Fatal(err)
	}
	// Change only company name; revision stays manual.
	if out, err := run("1\nAcme Europe\n\n1\n8\n2\n", "manifest", "edit", "acme"); err != nil {
		t.Fatalf("edit: %v\n%s", err, out)
	} else if !strings.Contains(out, "backup ") {
		t.Fatal("backup path missing")
	}
	updated, err := store.Load("acme")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Rules.Name != "Acme Europe" || updated.Rules.Version != 1 {
		t.Fatal("wrong identity or automatic revision")
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(updated.Path), "manifest.backup-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups: %v %v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backup, original.Data) {
		t.Fatal("backup is not exact")
	}
	kept, err := os.ReadFile(filepath.Join(packageCopy, "manifest"))
	if err != nil || !bytes.Equal(kept, copied) {
		t.Fatal("editing changed package copy")
	}
	if _, err := run("8\n2\n", "manifest", "edit", "acme"); err != nil {
		t.Fatal(err)
	}
	backups, err = filepath.Glob(filepath.Join(filepath.Dir(updated.Path), "manifest.backup-*"))
	if err != nil || len(backups) != 1 {
		t.Fatal("unchanged edit created backup")
	}
	if _, err := run("", "manifest", "create", "acme"); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := run("9\n2\n", "manifest", "create", "cancelled"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("cancelled"); err == nil {
		t.Fatal("cancel wrote file")
	}
	if _, err := run("8\n1\n3\n", "manifest", "create", "second"); err != nil {
		t.Fatal(err)
	}
	selected, err := store.Default()
	if err != nil || selected.Name != "second" {
		t.Fatal("default selection failed")
	}
}

func TestExternalEditorRepairsInvalidManifest(t *testing.T) {
	store := manifest.Store{Root: t.TempDir()}
	old := manifestStore
	manifestStore = func() (manifest.Store, error) { return store, nil }
	t.Cleanup(func() { manifestStore = old })
	path, err := store.Create("acme", []byte("manifest: acme\nversion: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	broken := []byte("manifest: [broken")
	if err := os.WriteFile(path, broken, 0600); err != nil {
		t.Fatal(err)
	}
	editor := filepath.Join(t.TempDir(), "repair")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nprintf 'manifest: acme\\nversion: 2\\n' > \"$1\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", editor)
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"manifest", "edit", "acme", "--editor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("repair: %v\n%s", err, out.String())
	}
	saved, err := store.Load("acme")
	if err != nil || saved.Rules.Version != 2 {
		t.Fatal("repair failed")
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(path), "manifest.backup-*"))
	if err != nil || len(backups) != 1 {
		t.Fatal("no repair backup")
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backup, broken) {
		t.Fatal("repair lost original")
	}
}
