package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/manifest"
	"github.com/slng-ai/unmute/internal/scaffold"
)

// Both reader-facing examples must compile as a real package. A copied package
// uses its own contract even when this computer's saved library is unavailable.
func TestDocumentedManifestCompilesWithoutComputerConfig(t *testing.T) {
	original := manifestStore
	manifestStore = func() (manifest.Store, error) {
		return manifest.Store{}, fmt.Errorf("computer config must not be read during validation or compilation")
	}
	t.Cleanup(func() { manifestStore = original })
	for _, source := range []string{
		"../skill/assets/references/manifests.md",
		"../../docs-site/reference/manifest.mdx",
	} {
		t.Run(source, func(t *testing.T) {
			page, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			_, rest, ok := strings.Cut(string(page), "```yaml manifest.yaml\n")
			if !ok {
				t.Fatal("no manifest example")
			}
			contract, _, ok := strings.Cut(rest, "```")
			if !ok {
				t.Fatal("unclosed manifest example")
			}
			dir := filepath.Join(t.TempDir(), "agent")
			data := scaffold.Data{Name: "manifest-example", Manifest: []byte(contract), Tools: scaffold.DefaultTools()}
			data.SetTarget(scaffold.DefaultTarget)
			data.Listen.Language, data.Speak.Language = "en", "en"
			data.Listen.Params, data.Speak.Params = "world_part: eu-north", "world_part: eu-north"
			data.DeploymentRegions = []string{"eu-central"}
			if _, err := scaffold.Write(dir, data); err != nil {
				t.Fatal(err)
			}
			if _, stderr, err := runValidateCommand(t, dir); err != nil {
				t.Fatalf("validate: %v\n%s", err, stderr)
			}
			if stdout, stderr, err := runCompileCommand(t, dir); err != nil {
				t.Fatalf("compile: %v\n%s\n%s", err, stdout, stderr)
			}
			report, err := os.ReadFile(filepath.Join(dir, "build", "livekit", "compile-report.json"))
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]json.RawMessage
			if err := json.Unmarshal(report, &decoded); err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(decoded["manifest"], []byte("acme-corp")) {
				t.Fatalf("report does not identify the company contract: %s", report)
			}
		})
	}
}

func TestManifestFailurePreservesExistingBuild(t *testing.T) {
	dir := copySafeCore(t)
	build := filepath.Join(dir, "build", "livekit")
	if err := os.MkdirAll(build, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(build, "agent.py")
	before := []byte("existing generated output\n")
	if err := os.WriteFile(marker, before, 0o644); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(dir, "agent.yaml")
	agent, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentPath, append([]byte("manifest: manifest.yaml\n"), agent...), 0o644); err != nil {
		t.Fatal(err)
	}
	// A violation on the unselected Pipecat target must block LiveKit output too.
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte("manifest: acme-corp\nversion: 1\ntargets:\n  allow:\n    - livekit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runCompileCommand(t, dir, "--target", "livekit")
	if err == nil || !strings.Contains(stderr+err.Error(), "pipecat") {
		t.Fatalf("unselected target violation was not reported: %v\n%s\n%s", err, stdout, stderr)
	}
	after, err := os.ReadFile(marker)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed compile changed existing output: %v, %q", err, after)
	}
}

// A saved contract reaches a new package only when the author asks for one.
// A default saved on this computer used to route every `init` into guided
// setup, which is how a first agent became unbuildable without answering for
// rules nobody had mentioned.
func TestInitIgnoresTheSavedDefaultUnlessAsked(t *testing.T) {
	store := manifest.Store{Root: t.TempDir()}
	original := manifestStore
	manifestStore = func() (manifest.Store, error) { return store, nil }
	t.Cleanup(func() { manifestStore = original })
	const contract = "# company copy\nmanifest: acme\nversion: 1\ntargets:\n  allow:\n    - livekit\n"
	if _, err := store.Create("acme", []byte(contract)); err != nil {
		t.Fatal(err)
	}
	other := strings.Replace(contract, "manifest: acme", "manifest: other", 1)
	if _, err := store.Create("other", []byte(other)); err != nil {
		t.Fatal(err)
	}
	t.Run("plain-agent", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "plain-agent")
		out, err := initManifestCommand(t, noInput{t}, "init", dir)
		if err != nil {
			t.Fatalf("init: %v\n%s", err, out)
		}
		if _, err := os.Stat(filepath.Join(dir, "manifest.yaml")); !os.IsNotExist(err) {
			t.Fatalf("the saved default reached a plain init: %v\n%s", err, out)
		}
		if _, stderr, err := runValidateCommand(t, dir); err != nil {
			t.Fatalf("scaffold is invalid: %v\n%s", err, stderr)
		}
	})
	t.Run("picked-agent", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "picked-agent")
		// 2 picks `other`, which the picker lists after the default `acme`.
		out, err := initManifestCommand(t, strings.NewReader("2\n7\n\n"), "init", dir, "--from-manifest")
		if err != nil {
			t.Fatalf("init: %v\n%s", err, out)
		}
		got, err := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
		if err != nil || string(got) != other {
			t.Fatalf("manifest copy: %v, %q\n%s", err, got, out)
		}
		if _, stderr, err := runValidateCommand(t, dir); err != nil {
			t.Fatalf("created agent is invalid: %v\n%s", err, stderr)
		}
	})
	if saved, err := store.Default(); err != nil || saved.Name != "acme" {
		t.Fatalf("picker changed the default: %v, %+v", err, saved)
	}
}

func TestBrokenDefaultStopsThePickerAndNotTheScaffold(t *testing.T) {
	store := manifest.Store{Root: t.TempDir()}
	original := manifestStore
	manifestStore = func() (manifest.Store, error) { return store, nil }
	t.Cleanup(func() { manifestStore = original })
	if _, err := store.Create("acme", []byte("manifest: acme\nversion: 1\n")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Root, "default-manifest"), []byte("missing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "picked-agent")
	if _, err := initManifestCommand(t, noInput{t}, "init", dir, "--from-manifest"); err == nil || !strings.Contains(err.Error(), "configured default") {
		t.Fatalf("broken default silently ignored: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("broken default wrote destination: %v", err)
	}
	dir = filepath.Join(t.TempDir(), "plain-agent")
	if out, err := initManifestCommand(t, noInput{t}, "init", dir); err != nil {
		t.Fatalf("broken default stopped the scaffold: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent.yaml")); err != nil {
		t.Fatalf("scaffold missing: %v", err)
	}
}
