package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copySafeCore copies the example package into a temp dir so compile can write
// its build/ output without polluting the repo.
func copySafeCore(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "agent")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "..", "examples", "safe_core"))); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runCompileCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := newRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"compile"}, args...))
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// gap #2: forwarded bindings + derived sizing reach compile stdout and the
// compile-report.json on disk — SCHEMA.md §6.2 rule 6, §5.1 ("the contract").
func TestCompilePrintsBindingsAndSizing(t *testing.T) {
	dir := copySafeCore(t)
	stdout, _, err := runCompileCommand(t, "--target", "pipecat-dev", dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"pipecat-dev: binding listen provider=deepgram model=nova-3 (forwarded as-is, not validated)",
		"pipecat-dev:   param temperature=0.4",
		"pipecat-dev: sizing workers=60 [unbenchmarked]",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}

	report, err := os.ReadFile(filepath.Join(dir, "build", "pipecat-dev", "compile-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"bindings"`, `"sizing"`, `"nova-3"`, `"unbenchmarked"`} {
		if !strings.Contains(string(report), want) {
			t.Errorf("compile-report.json missing %q:\n%s", want, report)
		}
	}
}

// gap #1: a gated target surfaces the provider-vocabulary diagnostic on the
// compile path, not just "validation failed for N target(s)".
func TestCompileSurfacesPerTargetDiagnostics(t *testing.T) {
	dir := copySafeCore(t)
	path := filepath.Join(dir, "agent.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = bytes.Replace(content, []byte("  max_duration: 20m"), []byte("  max_duration: 20m\n  thinking_audio: subtle"), 1)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = runCompileCommand(t, "--target", "vapi-prod", dir)
	if err == nil || !strings.Contains(err.Error(), "Vapi has no faithful thinking-audio lowering") {
		t.Fatalf("err = %v", err)
	}
}

// gap #3: a package with zero target instances fails the same way validate does.
func TestCompileFailsWithNoTargets(t *testing.T) {
	dir := copySafeCore(t)
	if err := os.WriteFile(filepath.Join(dir, "targets.yaml"), []byte("targets: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := runCompileCommand(t, dir)
	if err == nil || !strings.Contains(err.Error(), "no targets selected") {
		t.Fatalf("err = %v", err)
	}
}
