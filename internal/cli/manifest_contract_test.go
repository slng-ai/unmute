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
			_, rest, ok := strings.Cut(string(page), "```yaml manifest\n")
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
	if err := os.WriteFile(agentPath, append([]byte("manifest: manifest\n"), agent...), 0o644); err != nil {
		t.Fatal(err)
	}
	// A violation on the unselected Pipecat target must block LiveKit output too.
	if err := os.WriteFile(filepath.Join(dir, "manifest"), []byte("manifest: acme-corp\nversion: 1\ntargets:\n  allow:\n    - livekit\n"), 0o644); err != nil {
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

func TestInitUsesSavedDefaultAndPickerDoesNotChangeIt(t *testing.T) {
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
	for _, tc := range []struct {
		name, input, want string
		named             bool
		pick              bool
	}{
		{"default-agent", "7\n\n", contract, false, false},
		{"other-agent", "2\n7\n\n", other, false, true},
		{"named-agent", "7\n\n", other, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), tc.name)
			cmd := newRootCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetIn(strings.NewReader(tc.input))
			args := []string{"init", dir}
			if tc.named {
				args = append(args, "--manifest", "other")
			}
			if tc.pick {
				args = append(args, "--from-manifest")
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("init: %v\n%s", err, out.String())
			}
			got, err := os.ReadFile(filepath.Join(dir, "manifest"))
			if err != nil || string(got) != tc.want {
				t.Fatalf("manifest copy: %v, %q\n%s", err, got, out.String())
			}
			if _, stderr, err := runValidateCommand(t, dir); err != nil {
				t.Fatalf("created agent is invalid: %v\n%s", err, stderr)
			}
		})
	}
	if saved, err := store.Default(); err != nil || saved.Name != "acme" {
		t.Fatalf("picker changed the default: %v, %+v", err, saved)
	}
}

func TestInitRefusesBrokenDefaultWithoutWriting(t *testing.T) {
	store := manifest.Store{Root: t.TempDir()}
	original := manifestStore
	manifestStore = func() (manifest.Store, error) { return store, nil }
	t.Cleanup(func() { manifestStore = original })
	if err := os.WriteFile(filepath.Join(store.Root, "default-manifest"), []byte("missing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "agent")
	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"init", dir})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "configured default") {
		t.Fatalf("broken default silently ignored: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("broken default wrote destination: %v", err)
	}
}
