package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/scaffold"
)

// run executes a fresh command tree (rule 1) and returns captured output + err.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func runWithInput(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestInitWithoutTTYRequiresName(t *testing.T) {
	_, err := run(t, "init")
	if err == nil || err.Error() != "agent name required" {
		t.Fatalf("init error = %v, want agent name required", err)
	}
}

func TestInit_scaffoldsValidV1Package(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	out, err := run(t, "init", dir)
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	for _, name := range []string{"agent.yaml", "instructions.md", "targets.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing scaffolded %s: %v", name, err)
		}
	}
	// The scaffold must validate clean on its default target.
	vout, err := run(t, "validate", dir)
	if err != nil {
		t.Fatalf("validate scaffold: %v\n%s", err, vout)
	}
	if !strings.Contains(vout, "✓ livekit (livekit)") {
		t.Fatalf("scaffold did not validate clean:\n%s", vout)
	}
}

func TestInit_seedsEndCallByDefault(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	out, err := run(t, "init", dir)
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	tool, err := os.ReadFile(filepath.Join(dir, "tools", "end_call.yaml"))
	if err != nil {
		t.Fatalf("init did not scaffold the default end_call tool: %v", err)
	}
	for _, want := range []string{"builtin:", "  id: end_call"} {
		if !strings.Contains(string(tool), want) {
			t.Errorf("end_call.yaml missing %q:\n%s", want, tool)
		}
	}
	agent, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agent), "- end_call") {
		t.Errorf("entry agent must reference end_call:\n%s", agent)
	}
	// Still compiles clean on the default target.
	vout, err := run(t, "validate", dir)
	if err != nil {
		t.Fatalf("validate scaffold with default end_call: %v\n%s", err, vout)
	}
}

func TestInit_refusesExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := run(t, "init", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "init", dir); err == nil {
		t.Fatal("expected refusal to overwrite an existing directory")
	}
}

func TestV30InitEntersCreate(t *testing.T) {
	t.Chdir(t.TempDir())
	// init enters Create directly: name, 7=Create agent, confirm.
	out, err := runWithInput(t, "wiz-agent\n7\n\n", "init")
	if err != nil {
		t.Fatalf("init wizard: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join("wiz-agent", "agent.yaml")); err != nil {
		t.Fatalf("wizard did not scaffold agent.yaml: %v\n%s", err, out)
	}
}

func TestV30BareRootNonTTYPrintsHelp(t *testing.T) {
	out, err := run(t)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "Available Commands:") {
		t.Fatalf("bare non-TTY output is not help:\n%s", out)
	}
}

func TestV30BareRootTTYLaunchesConsole(t *testing.T) {
	device, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = device.Close() }()
	if !shouldRunConsole(device, device) {
		t.Fatal("character-device stdin/stdout did not select console")
	}
	var output bytes.Buffer
	if shouldRunConsole(strings.NewReader(""), &output) {
		t.Fatal("scripted stdin/stdout selected console")
	}
}

func TestInitWizardDeclineWritesNothing(t *testing.T) {
	t.Chdir(t.TempDir())
	// Decline review, then back out of the editor.
	if _, err := runWithInput(t, "agent\n7\n2\n8\n", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("agent"); !os.IsNotExist(err) {
		t.Fatalf("declined wizard wrote destination: %v", err)
	}
}

func TestConsoleActionUsesCommandPaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent"}
	data.SetTarget(scaffold.DefaultTarget)
	if _, err := scaffold.Write(dir, data); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"validate", "compile"} {
		t.Run(action, func(t *testing.T) {
			var output bytes.Buffer
			if err := consoleAction(action, dir, &output); err != nil {
				t.Fatalf("consoleAction(%q): %v\n%s", action, err, output.String())
			}
			if output.Len() == 0 {
				t.Fatalf("consoleAction(%q) produced no report", action)
			}
		})
	}
	if err := consoleAction("unknown", dir, &bytes.Buffer{}); err == nil || err.Error() != fmt.Sprintf("unknown console action %q", "unknown") {
		t.Fatalf("unknown action error = %v", err)
	}
}

// `unmute init` writes no `name:` it had to guess at, so a folder that cannot be
// one is refused before anything is written, and refused with a message about
// the argument the author typed rather than about a field they never wrote.
func TestInitRefusesAFolderThatCannotBeAnAgentName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "it")
	cmd := newRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"init", dir})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("init wrote a package whose name it could not derive")
	}
	for _, want := range []string{`"it" cannot be an agent name`, "3 to 64 characters", "add a `name:`"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not contain %q: %v", want, err)
		}
	}
	if _, statErr := os.Stat(dir); statErr == nil {
		t.Error("init refused and still created the directory")
	}
}

// The usual case: a folder becomes a legal name, and the package it writes is
// one the compiler accepts.
func TestInitWritesAPackageThatValidates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Acme_Support")
	cmd := newRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"init", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "name: acme-support\n") {
		t.Errorf("agent.yaml does not carry the name derived from the folder:\n%s", written)
	}
	if _, _, err := loadPackage(dir, nil); err != nil {
		t.Errorf("the scaffolded package does not build: %v", err)
	}
}
