package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/spf13/cobra"
)

// fakeDevDocker installs a docker stand-in that traces every call (with a few
// env values) to trace, turns `logs` into a clean exit so the run
// returns, and makes the preflight `version`/`info` checks pass. Returns the
// trace path.
func fakeDevDocker(t *testing.T) (script, trace string) {
	t.Helper()
	dir := t.TempDir()
	script = filepath.Join(dir, "docker")
	trace = filepath.Join(dir, "trace.log")
	body := "#!/bin/sh\n" +
		"printf '%s | UNMUTE_DEV_PORT=%s | OPENAI_API_KEY=%s | UNMUTE_DEV_PID=%s | LIVEKIT_HOST_PORT=%s\\n' \"$*\" \"$UNMUTE_DEV_PORT\" \"$OPENAI_API_KEY\" \"$UNMUTE_DEV_PID\" \"$LIVEKIT_HOST_PORT\" >> \"$TRACE_FILE\"\n" +
		"case \"$*\" in *' logs '*) echo 'registered worker'; exit 0;; esac\n" +
		// `ps` lists three stacks: one whose process is dead, one alive, one
		// started by hand (no pid). Only the first may be stopped.
		"case \"$*\" in 'ps '*) printf '%s unmute-dead-test\\n%s unmute-live-test\\n unmute-hand-test\\n' \"$DEAD_PID\" \"$LIVE_PID\";; esac\n"
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
	// A process that has already exited is the stack an earlier session left
	// behind; this test's own pid is the stack another live session still uses.
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"),
		[]byte("TRACE_FILE="+trace+"\nOPENAI_API_KEY=sk-test-xyz\n"+
			"DEAD_PID="+strconv.Itoa(dead.ProcessState.Pid())+"\nLIVE_PID="+strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "dev", dir, "--target", "livekit", "--port", "0", "--bot-port", "7862", "--no-open")
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
		"down --remove-orphans --timeout 5",
		"--project-name unmute-",
		"UNMUTE_DEV_PORT=7862",
		"OPENAI_API_KEY=sk-test-xyz",
		// The stack is labelled with this process, and the LiveKit ports are
		// chosen here rather than left to the template's 7880 default.
		"UNMUTE_DEV_PID=" + strconv.Itoa(os.Getpid()),
		"LIVEKIT_HOST_PORT=7",
		// The dead session's stack is stopped by name alone, no --file.
		"compose --project-name unmute-dead-test down --remove-orphans --timeout 5",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("compose trace missing %q:\n%s", want, s)
		}
	}
	for _, forbidden := range []string{"--volumes", "unmute-live-test down", "unmute-hand-test down"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("compose trace contains %q (volumes survive, and a live or hand-started stack is never stopped):\n%s", forbidden, s)
		}
	}
	if !strings.Contains(out, "stopped unmute-dead-test") {
		t.Errorf("stderr does not say which abandoned stack was stopped:\n%s", out)
	}
	// `--port 0` binds an ephemeral port; the URL has to name the bound one.
	if !strings.Contains(out, "http://localhost:") || strings.Contains(out, "http://localhost:0/") {
		t.Errorf("the URL does not carry the port actually bound:\n%s", out)
	}
}

// TestFreeLiveKitPortsSkipsABusySet: a held port in the first set moves the
// whole set, stepping by ten, to one where all three ports are free.
func TestFreeLiveKitPortsSkipsABusySet(t *testing.T) {
	var held net.Listener
	var base int
	for _, candidate := range []int{17880, 27880, 37880, 47880} {
		ln, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(candidate)))
		if err == nil {
			held, base = ln, candidate
			break
		}
	}
	if held == nil {
		t.Skip("no candidate base port free on this host")
	}
	defer func() { _ = held.Close() }()
	restore := liveKitDevBasePort
	liveKitDevBasePort = base
	t.Cleanup(func() { liveKitDevBasePort = restore })

	got := freeLiveKitPorts()
	if got == base || (got-base)%10 != 0 {
		t.Fatalf("freeLiveKitPorts() = %d with %d held; want a later set stepped by ten", got, base)
	}
	if !portFree("tcp", got) || !portFree("tcp", got+1) || !portFree("udp", got+2) {
		t.Fatalf("the set at %d is not free", got)
	}
}

// TestDevComposeReportsAFailedDown: a teardown that fails names the project and
// the command to run, instead of leaving the ports held in silence.
func TestDevComposeReportsAFailedDown(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	body := "#!/bin/sh\ncase \"$*\" in *' logs '*) sleep 30;; *' down '*) exit 1;; esac\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	restore := composeCommand
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, script, args...)
	}
	t.Cleanup(func() { composeCommand = restore })

	cmd, out := devTestCommand(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	err := runDevCompose(ctx, cmd, devWebRun{
		root: dir, provider: ir.ProviderPipecat, agentName: "pipecat",
		composeFile: filepath.Join(dir, "compose.dev.yaml"), project: "unmute-stuck-test",
		env:     []string{"PATH=" + os.Getenv("PATH")},
		logPath: filepath.Join(dir, "dev.log"), uiPort: "0", botPort: "7860", noOpen: true,
	})
	if err != nil {
		t.Fatalf("runDevCompose: %v\n%s", err, out.String())
	}
	for _, want := range []string{"could not stop unmute-stuck-test", "docker compose --project-name unmute-stuck-test down"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("a failed down did not print %q:\n%s", want, out.String())
		}
	}
}

// TestDevWebTearsDownOnHangup: closing the terminal sends SIGHUP. It has to be
// caught like ctrl-c, or the process dies before the deferred teardown and the
// stack keeps its ports. Were it not caught, this test binary would die here.
func TestDevWebTearsDownOnHangup(t *testing.T) {
	dir := copySafeCore(t)
	tmp := t.TempDir()
	script := filepath.Join(tmp, "docker")
	trace := filepath.Join(tmp, "trace.log")
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TRACE_FILE\"\ncase \"$*\" in *' logs '*) sleep 30;; esac\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	restoreCmd, restoreLook := composeCommand, composeLookPath
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, script, args...)
	}
	composeLookPath = func(string) (string, error) { return script, nil }
	t.Cleanup(func() { composeCommand, composeLookPath = restoreCmd, restoreLook })
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("TRACE_FILE="+trace+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	go func() {
		// Hang up only once the run is parked on `logs`, which is after the
		// signal handler is installed.
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			raw, _ := os.ReadFile(trace)
			if strings.Contains(string(raw), " logs ") {
				_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	out, err := run(t, "dev", dir, "--target", "livekit", "--port", "0", "--bot-port", "0", "--no-open")
	if err != nil {
		t.Fatalf("dev web run: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "down --remove-orphans --timeout 5") {
		t.Fatalf("a hang-up did not tear the stack down:\n%s", raw)
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

	_, err := run(t, "dev", dir, "--target", "livekit", "--port", "0", "--no-open")
	if err == nil ||
		!strings.Contains(err.Error(), "docker compose is required to run") ||
		!strings.Contains(err.Error(), "Docker Desktop") ||
		!strings.Contains(err.Error(), "Compose plugin") {
		t.Fatalf("missing docker error = %v", err)
	}
}

func TestDevWebPipecatRunsOnHostWithoutDocker(t *testing.T) {
	dir := copySafeCore(t)
	portListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, botPort, err := net.SplitHostPort(portListener.Addr().String())
	_ = portListener.Close()
	if err != nil {
		t.Fatal(err)
	}
	started := false
	restoreStart, restoreReady, restoreLook := startPipecatWebAgent, pipecatWebAgentReady, pipecatLookPath
	pipecatLookPath = func(string) (string, error) { return "/fake/uv", nil }
	startPipecatWebAgent = func(_ context.Context, gotDir, port string, env []string, _ io.Writer) (*localAgent, error) {
		started = true
		if gotDir != filepath.Join(dir, "build", "pipecat") || port != botPort || envValue(env, "OPENAI_API_KEY") != "sk-test-xyz" {
			t.Errorf("local start = dir %q port %q key %q", gotDir, port, envValue(env, "OPENAI_API_KEY"))
		}
		done := make(chan error, 1)
		go func() {
			time.Sleep(20 * time.Millisecond)
			done <- nil
		}()
		return &localAgent{cmd: &exec.Cmd{}, done: done}, nil
	}
	pipecatWebAgentReady = func(context.Context, string, <-chan error) error { return nil }
	t.Cleanup(func() {
		startPipecatWebAgent, pipecatWebAgentReady, pipecatLookPath = restoreStart, restoreReady, restoreLook
	})
	restoreComposeLook := composeLookPath
	composeLookPath = func(string) (string, error) {
		t.Error("Pipecat browser dev must not inspect Docker")
		return "", errors.New("unexpected Docker lookup")
	}
	t.Cleanup(func() { composeLookPath = restoreComposeLook })
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OPENAI_API_KEY=sk-test-xyz\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "dev", dir, "--target", "pipecat", "--port", "0", "--bot-port", botPort, "--no-open")
	if err != nil {
		t.Fatalf("dev web run: %v\n%s", err, out)
	}
	if !started || !strings.Contains(out, "build/pipecat") {
		t.Fatalf("local Pipecat agent was not started:\n%s", out)
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

	cmd, out := devTestCommand(t)
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
	if !strings.Contains(string(raw), "down --remove-orphans --timeout 5") {
		t.Fatalf("interrupt did not tear the stack down:\n%s", raw)
	}
}

// TestDevComposeHoldsAFailedBuildOpen: the whole point of serving before the
// build is that a failed build is readable in the browser. So a failing `up`
// must not exit immediately, and must still exit non-zero with the log path when
// the author eventually stops it. Failing loudly is not negotiable; the page is
// additional.
func TestDevComposeHoldsAFailedBuildOpen(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	trace := filepath.Join(dir, "trace.log")
	// up fails the way a missing secret or a bad dependency fails.
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TRACE_FILE\"\n" +
		"case \"$*\" in *' up '*) echo 'ERROR: failed to solve' >&2; exit 17;; esac\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	restore := composeCommand
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, script, args...)
	}
	t.Cleanup(func() { composeCommand = restore })

	cmd, out := devTestCommand(t)
	stream := newDevStream()
	ctx, cancel := context.WithCancel(context.Background())
	released := make(chan struct{})
	go func() {
		// Only cancel once the failure has actually been published, which proves
		// the run was still alive and serving rather than already returned.
		for stream.State() != devStateFailed {
			time.Sleep(5 * time.Millisecond)
		}
		close(released)
		cancel()
	}()

	err := runDevCompose(ctx, cmd, devWebRun{
		root: dir, provider: ir.ProviderLiveKit, agentName: "livekit",
		composeFile: filepath.Join(dir, "compose.dev.yaml"), project: "unmute-failed-build-test",
		env:     []string{"TRACE_FILE=" + trace},
		logPath: filepath.Join(dir, "dev.log"), uiPort: "0", botPort: "7860", noOpen: true,
		stream: stream,
	})

	select {
	case <-released:
	default:
		t.Fatal("run returned before publishing the failure, so the page never showed it")
	}
	if err == nil {
		t.Fatal("a failed build must still exit non-zero")
	}
	if !strings.Contains(err.Error(), "docker compose up") || !strings.Contains(err.Error(), "dev.log") {
		t.Errorf("error text lost its cause or the log path: %v", err)
	}
	if stream.State() != devStateFailed {
		t.Errorf("state = %q, want %q", stream.State(), devStateFailed)
	}
	// Teardown still runs, and the build output still reached the log.
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "down --remove-orphans") {
		t.Errorf("a failed build did not tear the stack down:\n%s", raw)
	}
	logged, err := os.ReadFile(filepath.Join(dir, "dev.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logged), "failed to solve") {
		t.Errorf("build failure missing from the log:\n%s\n%s", logged, out.String())
	}
}

// devTestCommand is a cobra command wired to one buffer, for the dev helpers
// that take a *cobra.Command only to write to it.
func devTestCommand(t *testing.T) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(t.Context())
	return cmd, &out
}
