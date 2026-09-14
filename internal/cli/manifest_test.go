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
	cmd.SetArgs([]string{"manifest", "create", "invalid"})
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
	cmd.SetArgs([]string{"manifest", "create", "acme"})
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
