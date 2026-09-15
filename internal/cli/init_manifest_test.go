package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/slng-ai/unmute/internal/manifest"
	"github.com/slng-ai/unmute/internal/scaffold"
	"github.com/slng-ai/unmute/internal/spec"
)

type noDraftInput struct{ t *testing.T }

func (r noDraftInput) Read([]byte) (int, error) { r.t.Fatal("draft read stdin"); return 0, io.EOF }

func draftStore(t *testing.T) manifest.Store {
	t.Helper()
	previous := manifestStore
	store := manifest.Store{Root: t.TempDir()}
	manifestStore = func() (manifest.Store, error) { return store, nil }
	t.Cleanup(func() { manifestStore = previous })
	return store
}
func initManifestCommand(t *testing.T, in io.Reader, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(in)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}
func TestInitManifestDraftNeverPromptsOrGuesses(t *testing.T) {
	store := draftStore(t)
	raw := []byte("# Exact company bytes.\nmanifest: Company\nversion: 2\nmodels:\n  speak:\n    - provider: slng\n")
	if _, err := store.Create("chosen", raw); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Root, "default-manifest"), []byte("missing-default\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "draft-agent")
	out, err := initManifestCommand(t, noDraftInput{t}, "init", dir, "--manifest", "chosen", "--draft")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "unmute validate") {
		t.Fatal("missing completion instruction")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 6 {
		t.Fatalf("unexpected files: %v", entries)
	}
	for _, name := range []string{"agent.yaml", "targets.yaml", "instructions.md", ".gitignore", ".env.example", "manifest"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	copied, err := os.ReadFile(filepath.Join(dir, "manifest"))
	if err != nil || !bytes.Equal(copied, raw) {
		t.Fatal("manifest bytes changed")
	}
	pkg, err := spec.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Agent.Manifest != "manifest" || pkg.Agent.Name != "draft-agent" || pkg.Agent.Agents[pkg.Agent.EntryAgent].Instructions != "instructions.md" {
		t.Fatal("draft identity/link missing")
	}
	if len(pkg.Agent.Models.Listen)+len(pkg.Agent.Models.Speak)+len(pkg.Agent.Models.Think)+len(pkg.Agent.Channels)+len(pkg.Targets)+len(pkg.Agent.Secrets) != 0 || pkg.Agent.Tracing != nil {
		t.Fatal("draft guessed configuration")
	}
	for _, command := range []string{"validate", "compile"} {
		if _, err := initManifestCommand(t, noDraftInput{t}, command, dir); err == nil {
			t.Fatalf("unfinished draft passed %s", command)
		}
	}
	if _, err := initManifestCommand(t, noDraftInput{t}, "init", dir, "--manifest", "chosen", "--draft"); err == nil {
		t.Fatal("occupied directory accepted")
	}
	unchanged, _ := os.ReadFile(filepath.Join(store.Root, "default-manifest"))
	if string(unchanged) != "missing-default\n" {
		t.Fatal("default changed")
	}
	saved, err := store.Load("chosen")
	if err != nil || !bytes.Equal(saved.Data, raw) {
		t.Fatal("saved manifest changed")
	}
}
func TestInitManifestFlagErrorsWriteNothing(t *testing.T) {
	store := draftStore(t)
	if _, err := store.Create("chosen", []byte("manifest: Company\nversion: 1\n")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--draft"}, "--manifest"},
		{[]string{"--manifest", "", "--draft"}, "manifest"},
		{[]string{"--manifest", "absent", "--draft"}, "absent"},
		{[]string{"--manifest", "../chosen", "--draft"}, "manifest name"},
		{[]string{"--manifest", "chosen", "--from-manifest"}, "--manifest"},
		{[]string{"--draft", "--from-manifest"}, "--from-manifest"},
		{[]string{"--from-manifest", "chosen"}, "--manifest"},
	} {
		dir := filepath.Join(t.TempDir(), "agent")
		args := append([]string{"init", dir}, tc.args...)
		_, err := initManifestCommand(t, noDraftInput{t}, args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: %v", args, err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatal("error wrote destination")
		}
	}
	for _, args := range [][]string{{"init", "--manifest", "chosen", "--draft"}, {"init", "", "--manifest", "chosen", "--draft"}} {
		if _, err := initManifestCommand(t, noDraftInput{t}, args...); err == nil || !strings.Contains(err.Error(), "name") {
			t.Fatalf("%v: %v", args, err)
		}
	}
	path, _ := store.Path("chosen")
	if err := os.WriteFile(path, []byte("invalid: ["), 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "agent")
	if _, err := initManifestCommand(t, noDraftInput{t}, "init", dir, "--manifest", "chosen", "--draft"); err == nil {
		t.Fatal("invalid manifest accepted")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("invalid manifest wrote destination")
	}
}

func TestNamedManifestIgnoresBrokenDefaultAndCancelWritesNothing(t *testing.T) {
	store := draftStore(t)
	raw := []byte("manifest: chosen\nversion: 1\ntargets:\n  allow: [livekit]\n")
	if _, err := store.Create("chosen", raw); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Root, "default-manifest"), []byte("missing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, save := range []bool{true, false} {
		dir := filepath.Join(t.TempDir(), "agent")
		input := "8\n" // Cancel on the main agent menu.
		if save {
			input = "7\n\n"
		}
		out, err := initManifestCommand(t, strings.NewReader(input), "init", dir, "--manifest", "chosen")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if save {
			b, err := os.ReadFile(filepath.Join(dir, "manifest"))
			if err != nil || !bytes.Equal(b, raw) {
				t.Fatal("wrong contract")
			}
		} else if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatal("cancel wrote package")
		}
	}
}

func TestManifestDraftCanBecomeAValidatedAgent(t *testing.T) {
	store := draftStore(t)
	for _, all := range []bool{false, true} {
		name := "selected"
		if all {
			name = "provider-wide"
		}
		d := scaffold.Data{Name: "support-agent"}
		d.SetTarget("livekit")
		d.Listen.Language, d.Speak.Language = "en", "en"
		d.Instructions = "You help customers with store opening hours. Ask which store they mean."
		contract := spec.Manifest{Name: name, Version: 1, Targets: &spec.ManifestAllow{Allow: []string{"livekit"}}, Languages: &spec.ManifestAllow{Allow: []string{"en"}}, Tools: &spec.ManifestTools{Kinds: &spec.ManifestAllow{Allow: []string{}}}}
		row := func(b scaffold.Binding) []spec.ManifestModel {
			r := spec.ManifestModel{Provider: b.Provider}
			if !all {
				r.Allow = []string{b.Model}
			}
			return []spec.ManifestModel{r}
		}
		contract.Models = spec.ManifestModels{Listen: row(d.Listen), Speak: row(d.Speak), Think: row(d.Reason)}
		raw, err := yaml.Marshal(contract)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Create(name, raw); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(t.TempDir(), "support-agent")
		if out, err := initManifestCommand(t, noDraftInput{t}, "init", dir, "--manifest", name, "--draft"); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		// Produce completed authored files, then apply them as an assistant would.
		completed := filepath.Join(t.TempDir(), "completed")
		d.Manifest = raw
		files, err := scaffold.Write(completed, d)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if filepath.Base(file) == "manifest" {
				continue
			}
			b, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, filepath.Base(file)), b, 0644); err != nil {
				t.Fatal(err)
			}
		}
		for _, command := range []string{"validate", "compile"} {
			if out, err := initManifestCommand(t, noDraftInput{t}, command, dir); err != nil {
				t.Fatalf("%s: %v\n%s", command, err, out)
			}
		}
		pkg, err := spec.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		original := pkg.Agent
		for _, violation := range []string{"provider", "model", "language", "tool"} {
			if all && violation == "model" {
				continue
			}
			candidate := original
			switch violation {
			case "provider", "model", "language":
				candidate.Models.Listen = make(map[string]spec.ModelDef)
				for key, binding := range original.Models.Listen {
					switch violation {
					case "provider":
						binding.Provider = "deepgram"
					case "model":
						binding.Model = "not-approved"
					case "language":
						binding.Language = "es"
					}
					candidate.Models.Listen[key] = binding
				}
			case "tool":
				candidate.Tools = []string{"extra"}
				candidate.Agents = make(map[string]spec.AgentDef)
				for key, agent := range original.Agents {
					agent.Tools = []string{"extra"}
					candidate.Agents[key] = agent
				}
				if err := os.MkdirAll(filepath.Join(dir, "tools"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "tools", "extra.yaml"), []byte("description: End call\nbuiltin:\n  id: end_call\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			b, err := yaml.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), b, 0644); err != nil {
				t.Fatal(err)
			}
			out, err := initManifestCommand(t, noDraftInput{t}, "validate", dir)
			if err == nil || !strings.Contains(out+err.Error(), "manifest") {
				t.Fatalf("%s escaped contract: %v %s", violation, err, out)
			}
		}
		copied, err := os.ReadFile(filepath.Join(dir, "manifest"))
		if err != nil || !bytes.Equal(copied, raw) {
			t.Fatal("completion changed contract")
		}
	}
}
