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
	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/scaffold"
)

func TestLLMModelsMatchCatalog(t *testing.T) { // V4
	want := append(ir.LLMCatalogModels(), otherLLM)
	if got := llmModels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("llmModels() = %v, want %v", got, want)
	}
}

func TestRunCreateDefaults(t *testing.T) { // V1, V5, V15
	t.Chdir(t.TempDir())
	input := strings.NewReader("1\nagent\n8\n\n")
	var output bytes.Buffer
	got, err := Run(input, &output, true, []string{"pipecat", "slng"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	agent := Agent{
		Path: "agent",
		Data: scaffold.Data{
			Name:     "agent",
			Greeting: scaffold.DefaultGreeting,
			Language: scaffold.DefaultLanguage,
			LLMModel: scaffold.DefaultLLMModel,
			STTModel: scaffold.DefaultSTTModel,
			TTSModel: scaffold.DefaultTTSModel,
			TTSVoice: scaffold.DefaultTTSVoice,
		},
	}
	want := Result{Agent: agent, Original: agent, Create: true, Confirmed: true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Run() = %#v, want %#v", got, want)
	}
	if _, err := os.Stat("agent"); !os.IsNotExist(err) {
		t.Fatalf("TUI wrote agent directory: %v", err)
	}
	for _, label := range []string{"Agent prompt", "LLM", "TTS", "STT", "Greeting", "Language", "← Back"} {
		if !strings.Contains(output.String(), label) {
			t.Errorf("menu missing %q:\n%s", label, output.String())
		}
	}
	if !strings.Contains(output.String(), "Agent name") || !strings.Contains(output.String(), agentNameHelp) {
		t.Fatalf("name field missing guidance:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "Type :back to return.") || strings.Contains(output.String(), "Esc = Back") {
		t.Fatalf("accessible navigation guidance is wrong:\n%s", output.String())
	}
}

func TestRunEditsExistingAgent(t *testing.T) { // V14, V16
	agent := Agent{
		Path: "/tmp/local-agent",
		Data: scaffold.Data{
			Name:     "Local agent",
			Greeting: "Old greeting",
			Language: "en",
			LLMModel: "openai/gpt-4.1",
			STTModel: "old/stt",
			TTSModel: "old/tts",
			TTSVoice: "old-voice",
		},
		Prompt: "Old prompt",
	}
	var output bytes.Buffer
	got, err := Run(strings.NewReader("2\n5\nNew greeting\n7\n\n"), &output, true, nil, []Agent{agent})
	if err != nil {
		t.Fatal(err)
	}
	want := agent
	want.Data.Greeting = "New greeting"
	if !got.Confirmed || got.Create || !reflect.DeepEqual(got.Original, agent) || !reflect.DeepEqual(got.Agent, want) {
		t.Fatalf("existing edit = %#v", got)
	}
	if !strings.Contains(output.String(), "Local agent") || !strings.Contains(output.String(), "Save changes") {
		t.Fatalf("existing agent missing from menus:\n%s", output.String())
	}
}

func TestRunBackAndNoWritePaths(t *testing.T) { // V2, V14, V16
	t.Chdir(t.TempDir())
	for name, input := range map[string]string{
		"name back":   "1\n:back\n2\n",
		"prompt back": "1\nagent\n1\n:back\n9\n2\n",
		"summary no":  "1\nagent\n8\nn\n9\n2\n",
	} {
		t.Run(name, func(t *testing.T) {
			result, err := Run(strings.NewReader(input), &bytes.Buffer{}, true, []string{"pipecat", "slng"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.Confirmed {
				t.Fatalf("navigation returned confirmed result: %#v", result)
			}
			if _, err := os.Stat("agent"); !os.IsNotExist(err) {
				t.Fatalf("navigation wrote agent directory: %v", err)
			}
		})
	}
}

func TestRunCompileDefaultsToAllTargets(t *testing.T) { // C9, V11, V15
	result, err := Run(
		strings.NewReader("1\nagent\n7\n2\n0\n8\n\n"),
		&bytes.Buffer{},
		true,
		[]string{"pipecat", "slng"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Compile || !reflect.DeepEqual(result.Targets, []string{"pipecat", "slng"}) {
		t.Fatalf("compile result = %#v", result)
	}
}

func TestRunEOFAborts(t *testing.T) { // C3, C8
	t.Chdir(t.TempDir())
	_, err := Run(strings.NewReader("1\nagent\n"), &bytes.Buffer{}, true, []string{"pipecat", "slng"}, nil)
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

func TestValidateNameRefusesExistingDirectory(t *testing.T) { // V6
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

func TestValidateBasicRejectsTemplateBreakers(t *testing.T) { // V8
	for _, value := range []string{`bad"value`, "bad\nvalue", "bad\rvalue"} {
		if err := validateBasic(value); err == nil {
			t.Errorf("validateBasic(%q) accepted", value)
		}
	}
}
