package tui

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/slng/unmute/internal/scaffold"
	targetcap "github.com/slng/unmute/internal/target"
)

func TestRunCreateDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	// 1=create, name=agent, 7=Create agent, ""=confirm default (yes).
	got, err := Run(strings.NewReader("1\nagent\n7\n\n"), &output, true)
	if err != nil {
		t.Fatal(err)
	}
	data := scaffold.Data{Name: "agent", Language: scaffold.DefaultLanguage, Channel: scaffold.DefaultChannel, Greeting: scaffold.DefaultGreeting, Instructions: scaffold.DefaultInstructions}
	data.SetTarget(scaffold.DefaultTarget)
	agent := Agent{Path: "agent", Data: data}
	want := Result{Agent: agent, Create: true, Confirmed: true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Run() = %#v, want %#v", got, want)
	}
	if _, err := os.Stat("agent"); !os.IsNotExist(err) {
		t.Fatalf("TUI wrote agent directory: %v", err)
	}
	for _, label := range []string{"Target", "Language", "Models", "Instructions", "Greeting", "Compile after create", "Create agent", "← Back"} {
		if !strings.Contains(output.String(), label) {
			t.Errorf("menu missing %q:\n%s", label, output.String())
		}
	}
	if !strings.Contains(output.String(), "Agent name") || !strings.Contains(output.String(), agentNameHelp) {
		t.Fatalf("name field missing guidance:\n%s", output.String())
	}
	for _, label := range []string{"Required env:", "Forwarded bindings:"} {
		if !strings.Contains(output.String(), label) {
			t.Errorf("review missing %q:\n%s", label, output.String())
		}
	}
}

func TestRunQuit(t *testing.T) {
	got, err := Run(strings.NewReader("2\n"), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Confirmed {
		t.Fatalf("quit returned a confirmed result: %#v", got)
	}
}

func TestRunCompileToggle(t *testing.T) {
	t.Chdir(t.TempDir())
	// 1=create, name, 6=toggle compile on, 7=Create agent, confirm.
	got, err := Run(strings.NewReader("1\nagent\n6\n7\n\n"), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Confirmed || !got.Compile {
		t.Fatalf("compile toggle result = %#v", got)
	}
}

func TestRunSelectTarget(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	// Create, name, Target, LiveKit, Create agent, confirm.
	got, err := Run(strings.NewReader("1\nagent\n1\n2\n7\n\n"), &output, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Data.Target != "livekit" {
		t.Fatalf("target = %q", got.Agent.Data.Target)
	}
	if !strings.Contains(output.String(), "Vapi and Deepgram are unavailable") {
		t.Fatalf("missing unavailable-driver explanation:\n%s", output.String())
	}
}

func TestRunEditModels(t *testing.T) {
	t.Chdir(t.TempDir())
	// Create, name, Models, Speak, cartesia, model, voice, params, Back, Create, confirm.
	got, err := Run(strings.NewReader("1\nagent\n3\n3\n1\nsonic-3\nvoice-id\n{\"speed\":1}\n4\n7\n\n"), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Data.Speak != (scaffold.Binding{Provider: "cartesia", Model: "sonic-3", Voice: "voice-id", Params: `{"speed":1}`}) {
		t.Fatalf("speak binding = %#v", got.Agent.Data.Speak)
	}
}

func TestRunEditLanguage(t *testing.T) {
	t.Chdir(t.TempDir())
	got, err := Run(strings.NewReader("1\nagent\n2\nes-MX\n7\n\n"), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Data.Language != "es-MX" {
		t.Fatalf("language = %q", got.Agent.Data.Language)
	}
}

func TestProviderOptionsMirrorCatalog(t *testing.T) {
	for _, tc := range []struct {
		framework targetcap.Provider
		role      targetcap.Role
	}{
		{targetcap.Pipecat, targetcap.Listen},
		{targetcap.Pipecat, targetcap.Reason},
		{targetcap.Pipecat, targetcap.Speak},
		{targetcap.LiveKit, targetcap.Listen},
		{targetcap.LiveKit, targetcap.Reason},
		{targetcap.LiveKit, targetcap.Speak},
		{targetcap.ElevenLabs, targetcap.Speak},
	} {
		options := providerOptions(tc.framework, tc.role)
		vendors := targetcap.DefaultCatalog().Vendors(tc.framework, tc.role)
		if len(options) != len(vendors) {
			t.Fatalf("%s/%s options = %d, vendors = %d", tc.framework, tc.role, len(options), len(vendors))
		}
		for i := range vendors {
			if options[i].Value != vendors[i] {
				t.Errorf("%s/%s option %d = %q, want %q", tc.framework, tc.role, i, options[i].Value, vendors[i])
			}
		}
	}
}

func TestValidateParams(t *testing.T) {
	for _, value := range []string{"", `{}`, `{"temperature":0.2}`} {
		if err := validateParams(value); err != nil {
			t.Errorf("validateParams(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"[]", "nope", "null"} {
		if err := validateParams(value); err == nil {
			t.Errorf("validateParams(%q) accepted", value)
		}
	}
}

func TestValidateLanguage(t *testing.T) {
	for _, value := range []string{"en", "es-MX", "zh-Hans-CN"} {
		if err := validateLanguage(value); err != nil {
			t.Errorf("validateLanguage(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"", "not_a_language"} {
		if err := validateLanguage(value); err == nil {
			t.Errorf("validateLanguage(%q) accepted", value)
		}
	}
}

func TestRunEOFAborts(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := Run(strings.NewReader("1\nagent\n"), &bytes.Buffer{}, true)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("Run() error = %v, want ErrUserAborted", err)
	}
}

func TestBackKeyMapShowsFooterHint(t *testing.T) {
	help := backKeyMap().Input.Submit.Help()
	if !strings.Contains(help.Key, "esc back") || help.Desc != "submit" {
		t.Fatalf("input footer help = %#v", help)
	}
}

func TestValidateNameRefusesExistingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("taken", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("taken", "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateName("taken"); !errors.Is(err, scaffold.ErrExists) {
		t.Fatalf("validateName() = %v, want ErrExists", err)
	}
}

func TestValidateBasicRejectsTemplateBreakers(t *testing.T) {
	for _, value := range []string{`bad"value`, "bad\nvalue", "bad\rvalue"} {
		if err := validateBasic(value); err == nil {
			t.Errorf("validateBasic(%q) accepted", value)
		}
	}
}
