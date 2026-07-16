package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	// The scaffold must validate clean on its pipecat target.
	vout, err := run(t, "validate", dir)
	if err != nil {
		t.Fatalf("validate scaffold: %v\n%s", err, vout)
	}
	if !strings.Contains(vout, "pipecat-dev") || !strings.Contains(vout, "pass") {
		t.Fatalf("scaffold did not validate clean:\n%s", vout)
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

func TestInit_wizardScaffoldsFromScriptedInput(t *testing.T) {
	t.Chdir(t.TempDir())
	// 1=create, name, 9=Create agent, confirm.
	out, err := runWithInput(t, "1\nwiz-agent\n9\n\n", "init")
	if err != nil {
		t.Fatalf("init wizard: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join("wiz-agent", "agent.yaml")); err != nil {
		t.Fatalf("wizard did not scaffold agent.yaml: %v\n%s", err, out)
	}
}
