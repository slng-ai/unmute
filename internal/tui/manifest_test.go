package tui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/scaffold"
	"github.com/slng-ai/unmute/internal/spec"
)

// createFromContract drives creation under one saved contract: what
// `unmute init <name> --from-manifest` does once the picker has chosen one.
func createFromContract(in *strings.Reader, out *bytes.Buffer, path string, raw []byte) (Result, error) {
	return RunCreateWithManifests(in, out, true, path, []ManifestChoice{{Data: raw}}, "", false)
}

const manifestTestContract = `# Preserve this comment.
manifest: acme
version: 1
targets:
  allow:
    - livekit
models:
  listen:
    - provider: slng
      allow:
        - deepgram/nova:3
  think:
    - provider: openai
      allow:
        - ` + scaffold.DefaultReasonModel + `
  speak:
    - provider: slng
      allow:
        - cartesia/sonic:3.5
languages:
  allow:
    - en
regions:
  models:
    - role: listen
      provider: slng
      allow:
        - eu-north
  deployments:
    - provider: livekit
      allow:
        - eu-central
tools:
  kinds:
    allow: []
tracing:
  allow: []
`

func TestManifestCreateCopiesContractAndMaintainsIt(t *testing.T) {
	var out bytes.Buffer
	result, err := createFromContract(strings.NewReader("cartesia-voice\n7\n\n"), &out, "agent", []byte(manifestTestContract))
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !result.Confirmed {
		t.Fatal("not confirmed")
	}
	data := result.Agent.Data
	if data.Target != "livekit" || len(data.Tools) != 0 || data.Listen.Language != "en" || data.Speak.Language != "en" || !strings.Contains(data.Listen.Params, "eu-north") {
		t.Fatalf("wrong guided choices: %+v", data)
	}
	root := filepath.Join(t.TempDir(), "agent")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	pkg, err := spec.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Agent.Manifest != "manifest.yaml" || string(pkg.ManifestBytes) != manifestTestContract {
		t.Fatal("contract changed")
	}
	maintained, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	maintained.data.Greeting = "Hello again."
	candidate := filepath.Join(t.TempDir(), "candidate")
	if _, err := scaffold.Write(candidate, maintained.data); err != nil {
		t.Fatal(err)
	}
	if err := validateMaintained(candidate); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(candidate, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != manifestTestContract {
		t.Fatal("maintenance changed contract bytes")
	}
}

func TestManifestPickerKeepsAccessibleAnswers(t *testing.T) {
	var out bytes.Buffer
	choices := []ManifestChoice{{Name: "other", Data: []byte("manifest: other\nversion: 1\ntargets:\n  allow: []\n")}, {Name: "acme", Data: []byte(manifestTestContract)}}
	result, err := RunCreateWithManifests(strings.NewReader("\ncartesia-voice\n7\n\n"), &out, true, "agent", choices, "acme", true)
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !result.Confirmed || string(result.Agent.Data.Manifest) != manifestTestContract {
		t.Fatal("picker did not use default")
	}
}

func TestManifestCreateRefusesImpossibleTargetAndCanCancel(t *testing.T) {
	for _, tc := range []struct {
		raw, input string
		wantError  bool
	}{
		{"manifest: acme\nversion: 1\ntargets:\n  allow: []\n", "", true},
		{manifestTestContract, "cartesia-voice\n8\n", false},
	} {
		result, err := createFromContract(strings.NewReader(tc.input), &bytes.Buffer{}, "agent", []byte(tc.raw))
		if (err != nil) != tc.wantError {
			t.Fatalf("error %v", err)
		}
		if result.Confirmed {
			t.Fatal("unexpected confirmed result")
		}
	}
}

func TestManifestChoicesFilterUnsupportedTargetsAndPromptForVoice(t *testing.T) {
	rules, err := spec.ParseManifest([]byte(`manifest: acme
version: 1
targets:
  allow:
    - livekit
    - pipecat
models:
  speak:
    - provider: elevenlabs
      allow:
        - model-one
        - model-two
languages:
  allow:
    - en
    - es
`))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	runner := newRunner(strings.NewReader("2\nvoice-id\n2\n"), &out, true)
	runner.manifest = rules
	binding := scaffold.Binding{}
	if err := chooseManifestBinding(runner, "livekit", "speak", &binding); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if binding.Model != "model-two" || binding.Voice != "voice-id" || binding.Language != "es" {
		t.Fatalf("wrong choices: %+v", binding)
	}
	if len(manifestTargetOptions(runner, createTargetOptions())) == 0 {
		t.Fatal("no supported target offered")
	}
}

func TestManifestTracingSurvivesMaintenance(t *testing.T) {
	raw := strings.Replace(manifestTestContract, "tracing:\n  allow: []", "tracing:\n  allow:\n    - langfuse", 1)
	var out bytes.Buffer
	result, err := createFromContract(strings.NewReader("cartesia-voice\n2\n7\n\n"), &out, "agent", []byte(raw))
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	root := filepath.Join(t.TempDir(), "agent")
	if _, err := scaffold.Write(root, result.Agent.Data); err != nil {
		t.Fatal(err)
	}
	maintained, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	if maintained.data.Tracing == nil || maintained.data.Tracing.Provider != "langfuse" {
		t.Fatal("tracing lost")
	}
	env, err := os.ReadFile(filepath.Join(root, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(env, []byte("LANGFUSE_SECRET_KEY=")) {
		t.Fatal("tracing credentials missing")
	}
}

func TestManifestChangingModelClearsPreviousVoiceAndParams(t *testing.T) {
	rules, err := spec.ParseManifest([]byte(manifestTestContract))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	runner := newRunner(strings.NewReader("new-cartesia-voice\n"), &out, true)
	runner.manifest = rules
	binding := scaffold.Binding{Provider: "slng", Model: "deepgram/aura:2", Voice: "old-aura-voice", Params: `{"old_knob":true}`}
	if err := chooseManifestBinding(runner, "livekit", "speak", &binding); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if binding.Voice != "new-cartesia-voice" || binding.Params != "" {
		t.Fatalf("kept model-specific settings: %+v", binding)
	}
	if !strings.Contains(out.String(), "Voice") {
		t.Fatal("did not ask for a new voice")
	}
}

func TestManifestUnscaffoldableProvidersAreNotOffered(t *testing.T) {
	for _, provider := range []string{"custom-company-endpoint", "slng"} {
		if _, ok := manifestScaffoldEntry("pipecat", "reason", provider); ok {
			t.Fatalf("offered %s without required endpoint or router fields", provider)
		}
	}
}

func TestHomeCanQuitWithBrokenDefaultButCreationFails(t *testing.T) {
	broken := fmt.Errorf("saved default missing")
	if _, err := RunConsoleWithManifest(strings.NewReader("3\n"), &bytes.Buffer{}, true, nil, nil, broken); err != nil {
		t.Fatal(err)
	}
	if _, err := RunConsoleWithManifest(strings.NewReader("1\n"), &bytes.Buffer{}, true, nil, nil, broken); !errors.Is(err, broken) {
		t.Fatalf("create error: %v", err)
	}
}

func TestMaintainShowsContractWarnings(t *testing.T) {
	raw := []byte("manifest: acme\nversion: 1\nlanguages:\n  allow:\n    - en\n")
	data := scaffold.Data{Name: "agent", Manifest: raw}
	data.SetTarget("livekit")
	root := filepath.Join(t.TempDir(), "agent")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	warnings, err := maintainedWarnings(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) == 0 || !strings.Contains(maintenanceWarningText(warnings), "manifest") {
		t.Fatalf("missing warning: %v", warnings)
	}
}

func TestManifestInitializationAcceptsAllProviderModels(t *testing.T) {
	rules, err := spec.ParseManifest([]byte("manifest: acme\nversion: 1\nmodels:\n  listen:\n    - provider: slng\n"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	runner := newRunner(strings.NewReader("soniox/speech-ai:rt-v5\n"), &out, true)
	runner.manifest = rules
	if len(manifestTargetOptions(runner, []menuChoice{newChoice("LiveKit", "livekit")})) != 1 {
		t.Fatal("all-model provider hid target")
	}
	binding := scaffold.Binding{Provider: "slng"}
	if err := chooseManifestBinding(runner, "livekit", "listen", &binding); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if binding.Provider != "slng" || binding.Model != "soniox/speech-ai:rt-v5" {
		t.Fatalf("wrong binding: %+v", binding)
	}
}
