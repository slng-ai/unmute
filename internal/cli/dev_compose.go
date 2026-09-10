package cli

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

var (
	composeLookPath = exec.LookPath
	composeCommand  = exec.CommandContext
	composeSlug     = regexp.MustCompile(`[^a-z0-9_-]+`)
)

// composeInstallHint names what to install; both entry points word their own
// missing-docker sentence around it.
const composeInstallHint = "install Docker Desktop or Docker Engine with the Compose plugin"

// preflightComposeCore runs the docker/compose/daemon checks. missingHint is the
// full error returned when the docker binary is absent, so each entry point can
// name its own mode and escape hatch.
func preflightComposeCore(ctx context.Context, env []string, missingHint string) error {
	if _, err := composeLookPath("docker"); err != nil {
		return errors.New(missingHint)
	}
	check := composeCommand(ctx, "docker", "compose", "version")
	check.Env = env
	if output, err := check.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose is unavailable: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	daemon := composeCommand(ctx, "docker", "info", "--format", "{{.ServerVersion}}")
	daemon.Env = env
	if output, err := daemon.CombinedOutput(); err != nil {
		return fmt.Errorf("docker daemon is unavailable: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func composeProjectName(root, target string) string {
	absolute, err := filepath.Abs(root)
	if err != nil {
		absolute = root
	}
	digest := sha256.Sum256([]byte(absolute))
	base := strings.ToLower(filepath.Base(filepath.Clean(root)) + "-" + target)
	base = strings.Trim(composeSlug.ReplaceAllString(base, "-"), "-_")
	if base == "" {
		base = "agent"
	}
	return fmt.Sprintf("unmute-%s-%x", base, digest[:4])
}

func composeArgs(file, project string, command ...string) []string {
	args := []string{"compose", "--file", file, "--project-name", project}
	return append(args, command...)
}

// liveKitDevBasePort is the first host port set `unmute dev` tries for the dev
// livekit-server: signalling on base, TCP fallback on base+1, UDP media on
// base+2. A var so a test can point it at ports it holds itself.
var liveKitDevBasePort = 7880

// freeLiveKitPorts returns the first base whose three ports are all free on this
// host, stepping by ten so two stacks never straddle. Every `unmute dev` session
// is its own Compose project, because the name hashes the package path, so two
// worktrees of one agent used to fight over 7880 and the second one lost with
// "port is already allocated".
//
// ponytail: a host probe sees a Docker-published port on Docker Desktop and on
// Linux with docker-proxy, which is the default. With userland-proxy off it does
// not, and `up` then fails the way it always did.
func freeLiveKitPorts() int {
	for base := liveKitDevBasePort; base < liveKitDevBasePort+100; base += 10 {
		if portFree("tcp", base) && portFree("tcp", base+1) && portFree("udp", base+2) {
			return base
		}
	}
	return liveKitDevBasePort
}

func portFree(network string, port int) bool {
	addr := net.JoinHostPort("", strconv.Itoa(port))
	if network == "udp" {
		conn, err := net.ListenPacket("udp", addr)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// devPIDLabel is the container label naming the `unmute dev` process that
// started a stack; the emitted compose.dev.yaml sets it from UNMUTE_DEV_PID.
const devPIDLabel = "unmute.dev.pid"

// reapAbandonedStacks stops every dev stack whose `unmute dev` process is gone.
// The teardown in runDevCompose is a defer, and a SIGKILL or a closed terminal
// skips a defer, so a stack outlived its session often enough to hold 7880
// against the next one. `--all` also clears the stopped leftovers a Docker
// restart leaves behind. No --file: the worktree that wrote the compose file may
// be gone, and Compose resolves the project from the containers' own labels
// (checked with --dry-run on Compose v5.1.4).
//
// ponytail: a reused PID keeps a stack alive until that PID dies too. A stack
// started by hand carries an empty label and is never touched.
func reapAbandonedStacks(ctx context.Context, env []string, logSink, errW io.Writer) {
	list := composeCommand(ctx, "docker", "ps", "--all", "--filter", "label="+devPIDLabel,
		"--format", `{{.Label "`+devPIDLabel+`"}} {{.Label "com.docker.compose.project"}}`)
	list.Env = env
	output, err := list.Output()
	if err != nil {
		return // nothing listed, nothing reaped; a held port still fails `up` loudly
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || seen[fields[1]] {
			continue
		}
		seen[fields[1]] = true
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid == os.Getpid() || processAlive(pid) {
			continue
		}
		project := fields[1]
		down := composeCommand(ctx, "docker", "compose", "--project-name", project, "down", "--remove-orphans", "--timeout", "5")
		down.Env = env
		down.Stdout, down.Stderr = logSink, logSink
		if err := down.Run(); err != nil {
			warnDownFailed(errW, project)
			continue
		}
		notef(errW, "stopped %s, left by an earlier run whose process is gone\n", project)
	}
}

// processAlive reports whether pid is running, through the documented idiom: a
// signal 0 on the os.Process, since FindProcess always succeeds on Unix. EPERM
// means alive under another user. Windows cannot send a signal 0, so there
// every pid reads as alive and nothing is ever reaped, which is the safe side.
// (syscall.Kill itself is not defined on Windows and broke the release build.)
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM) || runtime.GOOS == "windows"
}

// warnDownFailed names the one thing the reader has to do when a stack could not
// be stopped: the containers hold their ports until somebody runs this.
func warnDownFailed(w io.Writer, project string) {
	warnf(w, "could not stop %s; run: docker compose --project-name %s down\n", project, project)
}

func composeWasInterrupted(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return true
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && (status.Signal() == syscall.SIGINT || status.Signal() == syscall.SIGTERM)
}
