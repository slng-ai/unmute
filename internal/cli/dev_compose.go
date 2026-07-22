package cli

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/slng/unmute/internal/generate"
)

var (
	composeLookPath  = exec.LookPath
	composeCommand   = exec.CommandContext
	composePreflight = preflightCompose
	composeSlug      = regexp.MustCompile(`[^a-z0-9_-]+`)
)

func preflightCompose(ctx context.Context, env []string) error {
	if _, err := composeLookPath("docker"); err != nil {
		return errors.New("docker compose is required for --telephony; install Docker Desktop or Docker Engine with the Compose plugin")
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

func externalTelephonyEnv(plan *generate.TelephonyRuntimePlan) []string {
	supplied := make(map[string]bool, len(plan.LocalEnvironment)+len(plan.DevSuppliedEnv))
	for _, name := range plan.LocalEnvironment {
		supplied[name] = true
	}
	for _, name := range plan.DevSuppliedEnv {
		supplied[name] = true
	}
	result := make([]string, 0, len(plan.RequiredEnv))
	for _, name := range plan.RequiredEnv {
		if !supplied[name] {
			result = append(result, name)
		}
	}
	return result
}

func rejectLocalTopologyConflicts(plan *generate.TelephonyRuntimePlan, env []string) error {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			values[name] = value
		}
	}
	for _, name := range plan.LocalEnvironment {
		if values[name] != "" {
			return fmt.Errorf("%s conflicts with the generated local LiveKit SIP topology; unset it for `unmute dev --telephony`", name)
		}
	}
	for _, name := range plan.DevSuppliedEnv {
		if values[name] != "" {
			return fmt.Errorf("%s is supplied by `unmute dev --telephony` itself; unset it for local runs", name)
		}
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

func runTelephonyCompose(
	ctx context.Context,
	dir, file, project string,
	env []string,
	output, stdout, stderr io.Writer,
	logPath string,
) error {
	cleanup := func() error {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		down := composeCommand(cleanupCtx, "docker", composeArgs(file, project, "down", "--remove-orphans", "--timeout", "30")...)
		down.Dir, down.Env, down.Stdout, down.Stderr = dir, env, output, output
		return down.Run()
	}

	up := composeCommand(ctx, "docker", composeArgs(file, project, "up", "--build", "--detach", "--remove-orphans", "--wait")...)
	up.Dir, up.Env, up.Stdout, up.Stderr = dir, env, output, output
	spin := startSpinner(stderr, "starting telephony Compose services")
	err := up.Run()
	spin.Stop()
	if err != nil {
		_ = cleanup()
		if composeWasInterrupted(ctx, err) {
			fmt.Fprintln(stderr, "\nstopping...")
			return nil
		}
		fmt.Fprintf(stderr, "telephony Compose failed to start. logs: %s\n", logPath)
		return fmt.Errorf("start Docker Compose topology: %w", err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			fmt.Fprintf(stderr, "warning: stop telephony Compose project %s: %v\n", project, err)
		}
	}()

	fmt.Fprintf(stdout, "\n  \033[1;32m▸\033[0m telephony route ready\n    compose project: %s  ·  ctrl-c to stop  ·  logs: %s\n\n", project, logPath)
	logsCtx, cancelLogs := context.WithCancel(ctx)
	defer cancelLogs()
	logs := composeCommand(logsCtx, "docker", composeArgs(file, project, "logs", "--follow", "--no-color")...)
	logs.Dir, logs.Env, logs.Stdout, logs.Stderr = dir, env, output, output
	if err := logs.Start(); err != nil {
		return fmt.Errorf("follow Docker Compose logs: %w", err)
	}
	logsDone := make(chan error, 1)
	go func() { logsDone <- logs.Wait() }()

	select {
	case <-ctx.Done():
		fmt.Fprintln(stderr, "\nstopping...")
		cancelLogs()
		<-logsDone
		return nil
	case err := <-logsDone:
		if composeWasInterrupted(ctx, err) {
			fmt.Fprintln(stderr, "\nstopping...")
			return nil
		}
		if err == nil {
			return errors.New("docker compose log stream exited before shutdown")
		}
		if err != nil {
			return fmt.Errorf("docker compose log stream exited: %w", err)
		}
		return nil
	}
}
