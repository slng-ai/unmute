package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateCommandPrintsEveryTargetAndWarnings(t *testing.T) { // V16, V18
	stdout, stderr, err := runValidateCommand(t, filepath.Join("..", "..", "examples", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"TARGET\tPROVIDER\tRESULT", "livekit-dev\tlivekit\tpass", "deepgram-dev\tdeepgram\tpass"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "warning:") || strings.Contains(stderr, "error:") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestValidateCommandFiltersTargetInstances(t *testing.T) { // V18
	stdout, _, err := runValidateCommand(t, "--target", "vapi-prod", filepath.Join("..", "..", "examples", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "vapi-prod\tvapi\tpass") || strings.Contains(stdout, "livekit-dev") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestValidateCommandReturnsErrorForGatedTarget(t *testing.T) { // V16
	dir := filepath.Join(t.TempDir(), "agent")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "..", "examples", "safe_core"))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = bytes.Replace(content, []byte("  max_duration: 20m"), []byte("  max_duration: 20m\n  thinking_audio: subtle"), 1)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runValidateCommand(t, "--target", "vapi-prod", dir)
	if err == nil || !strings.Contains(stdout, "vapi-prod\tvapi\tfail") || !strings.Contains(stderr, "Vapi has no faithful thinking-audio lowering") {
		t.Fatalf("err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
}

func runValidateCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := newRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"validate"}, args...))
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}
