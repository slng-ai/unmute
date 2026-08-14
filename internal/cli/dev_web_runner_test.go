package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slng-ai/unmute/internal/ir"
)

// fakeDevDocker installs a docker stand-in that traces every call (with a few
// env values) to trace, turns `logs` into a clean self-interrupt so the run
// returns, and makes the preflight `version`/`info` checks pass. Returns the
// trace path.
func fakeDevDocker(t *testing.T) (script, trace string) {
	t.Helper()
	dir := t.TempDir()
	script = filepath.Join(dir, "docker")
	trace = filepath.Join(dir, "trace.log")
	body := "#!/bin/sh\n" +
		"printf '%s | UNMUTE_DEV_PORT=%s | OPENAI_API_KEY=%s\\n' \"$*\" \"$UNMUTE_DEV_PORT\" \"$OPENAI_API_KEY\" >> \"$TRACE_FILE\"\n" +
		"case \"$*\" in *' logs '*) kill -INT $$;; esac\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	restoreCmd, restoreLook := composeCommand, composeLookPath
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, script, args...)
	}
	composeLookPath = func(string) (string, error) { return script, nil }
	t.Cleanup(func() { composeCommand, composeLookPath = restoreCmd, restoreLook })
	return script, trace
}

// TestDevWebRunsComposeAndPassesEnv exercises the default runner end to end
// with a fake docker: the exact up/logs/down sequence, a project-scoped name,
// volumes preserved, and host env (a provider cred + UNMUTE_DEV_PORT) forwarded
// to Compose (SPEC V1, V5, V9).
func TestDevWebRunsComposeAndPassesEnv(t *testing.T) {
	dir := copySafeCore(t)
	_, trace := fakeDevDocker(t)
	if err := os.WriteFile(filepath.Join(dir, ".env"),
		[]byte("TRACE_FILE="+trace+"\nOPENAI_API_KEY=sk-test-xyz\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "dev", dir, "--target", "pipecat", "--port", "0", "--bot-port", "7862", "--no-open")
	if err != nil {
		t.Fatalf("dev web run: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		"up --build --detach --remove-orphans --wait",
		"logs --follow --no-color",
		"down --remove-orphans --timeout 30",
		"--project-name unmute-",
		"UNMUTE_DEV_PORT=7862",
		"OPENAI_API_KEY=sk-test-xyz",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("compose trace missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "--volumes") {
		t.Errorf("down must never pass --volumes (data volumes survive):\n%s", s)
	}
}

// TestDevWebMissingDockerFailsWithInstallHint: with no docker binary the run
// fails in preflight with the dev install message (Docker Desktop/Engine + the
// Compose plugin) and points at --console; no compose command runs (SCHEMA §5.3: no dead code).
func TestDevWebMissingDockerFailsWithInstallHint(t *testing.T) {
	dir := copySafeCore(t)
	restore := composeLookPath
	composeLookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { composeLookPath = restore })

	_, err := run(t, "dev", dir, "--target", "pipecat", "--port", "0", "--no-open")
	if err == nil ||
		!strings.Contains(err.Error(), "docker compose is required to run") ||
		!strings.Contains(err.Error(), "Docker Desktop") ||
		!strings.Contains(err.Error(), "Compose plugin") ||
		!strings.Contains(err.Error(), "--console") {
		t.Fatalf("missing docker error = %v", err)
	}
}

// TestDevComposeTearsDownOnInterrupt: a ctrl-c mid-run (ctx cancel) still runs
// the project-scoped down (SPEC V5, "on every exit path including interrupt").
func TestDevComposeTearsDownOnInterrupt(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	trace := filepath.Join(dir, "trace.log")
	// up returns immediately; logs blocks so the run parks in its select until
	// the context is cancelled.
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TRACE_FILE\"\ncase \"$*\" in *' logs '*) sleep 30;; esac\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	restore := composeCommand
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, script, args...)
	}
	t.Cleanup(func() { composeCommand = restore })

	cmd, out := telephonyTestCommand(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	err := runDevCompose(ctx, cmd, devWebRun{
		root: dir, provider: ir.ProviderPipecat, agentName: "pipecat",
		composeFile: filepath.Join(dir, "compose.dev.yaml"), project: "unmute-interrupt-test",
		env:     []string{"TRACE_FILE=" + trace},
		logPath: filepath.Join(dir, "dev.log"), uiPort: "0", botPort: "7860", noOpen: true,
	})
	if err != nil {
		t.Fatalf("runDevCompose: %v\n%s", err, out.String())
	}
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "down --remove-orphans --timeout 30") {
		t.Fatalf("interrupt did not tear the stack down:\n%s", raw)
	}
}
