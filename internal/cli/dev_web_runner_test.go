package cli

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/spf13/cobra"
)

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
	cmd.SetContext(recordedCtx(t))
	return cmd, &out
}
