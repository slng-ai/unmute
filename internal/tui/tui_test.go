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
)

func TestRunCreateDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	// 1=create, name=agent, 4=Create agent, ""=confirm default (yes).
	got, err := Run(strings.NewReader("1\nagent\n4\n\n"), &output, true)
	if err != nil {
		t.Fatal(err)
	}
	agent := Agent{Path: "agent", Data: scaffold.Data{
		Name: "agent", Greeting: scaffold.DefaultGreeting, Instructions: scaffold.DefaultInstructions,
	}}
	want := Result{Agent: agent, Create: true, Confirmed: true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Run() = %#v, want %#v", got, want)
	}
	if _, err := os.Stat("agent"); !os.IsNotExist(err) {
		t.Fatalf("TUI wrote agent directory: %v", err)
	}
	for _, label := range []string{"Instructions", "Greeting", "Compile after create", "Create agent", "← Back"} {
		if !strings.Contains(output.String(), label) {
			t.Errorf("menu missing %q:\n%s", label, output.String())
		}
	}
	if !strings.Contains(output.String(), "Agent name") || !strings.Contains(output.String(), agentNameHelp) {
		t.Fatalf("name field missing guidance:\n%s", output.String())
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
	// 1=create, name, 3=toggle compile on, 4=Create agent, confirm.
	got, err := Run(strings.NewReader("1\nagent\n3\n4\n\n"), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Confirmed || !got.Compile {
		t.Fatalf("compile toggle result = %#v", got)
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
