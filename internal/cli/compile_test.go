package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/generate"
	"github.com/slng/unmute/internal/ir"
)

// TestWriteArtifactFilesFormatsPython: the write path runs a best-effort
// `ruff format` over emitted .py so the on-disk project is format-stable, and
// leaves non-Python files untouched (SPEC V2). Skips when ruff is absent.
func TestWriteArtifactFilesFormatsPython(t *testing.T) {
	if _, err := exec.LookPath("ruff"); err != nil {
		t.Skip("ruff not installed")
	}
	dir := t.TempDir()
	ugly := "x  =  {  'a':1 }\n\n\n\ndef f( ):\n    return   x\n"
	readme := "# keep  this  as-is\n"
	files := []generate.File{
		{Path: "bot.py", Content: []byte(ugly)},
		{Path: "README.md", Content: []byte(readme)},
	}
	if err := writeArtifactFiles(nil, dir, files); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "bot.py"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == ugly {
		t.Error("bot.py was written unformatted (ruff format pass did not run)")
	}
	// Format-stable: a second `ruff format` produces no diff.
	cmd := exec.Command("ruff", "format", "--diff", "-")
	cmd.Stdin = bytes.NewReader(got)
	if out, _ := cmd.CombinedOutput(); len(bytes.TrimSpace(out)) != 0 {
		t.Errorf("written bot.py is not ruff-format-stable:\n%s", out)
	}
	// Non-Python files are copied verbatim.
	if md, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(md) != readme {
		t.Errorf("README.md was modified: %q", md)
	}
}

// copySafeCore copies the example package into a temp dir so compile can write
// its build/ output without polluting the repo.
func copySafeCore(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "agent")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "safe_core"))); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPrintTelephonyPlanUsesArtifactWithoutCarrierDispatch(t *testing.T) {
	plan := &generate.TelephonyRuntimePlan{
		Route:       ir.TelephonyKey{Provider: ir.ProviderPipecat, Transport: "carrier-websocket", Carrier: "twilio"},
		RequiredEnv: []string{"TWILIO_AUTH_TOKEN"}, Coordination: "local", AdmissionOwner: "generated_runtime",
	}
	var out bytes.Buffer
	printTelephonyPlan(&out, "phone", plan)
	for _, want := range []string{"provider=pipecat", "transport=carrier-websocket", "carrier=twilio", "required env TWILIO_AUTH_TOKEN"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %s", want, out.String())
		}
	}
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
	stdout, _, err := runCompileCommand(t, "--target", "pipecat", dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"pipecat: binding listen provider=deepgram model=nova-3 (forwarded as-is, not validated)",
		"pipecat:   param temperature=0.4",
		"pipecat: sizing workers=60 [unbenchmarked]",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}

	report, err := os.ReadFile(filepath.Join(dir, "build", "pipecat", "compile-report.json"))
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
	_, _, err = runCompileCommand(t, "--target", "vapi", dir)
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
