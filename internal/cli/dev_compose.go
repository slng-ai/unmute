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
	"slices"
	"strings"
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
	local := map[string]bool{"REDIS_URL": true}
	if slices.Contains(plan.Services, "livekit_server") {
		local["LIVEKIT_URL"] = true
		local["LIVEKIT_API_KEY"] = true
		local["LIVEKIT_API_SECRET"] = true
	}
	result := make([]string, 0, len(plan.RequiredEnv))
	for _, name := range plan.RequiredEnv {
		if !local[name] {
			result = append(result, name)
		}
	}
	return result
}

func rejectLocalTopologyConflicts(plan *generate.TelephonyRuntimePlan, env []string) error {
	if !slices.Contains(plan.Services, "livekit_server") {
		return nil
	}
	values := make(map[string]string, len(env))
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			values[name] = value
		}
	}
	for _, name := range []string{"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "REDIS_URL"} {
		if values[name] != "" {
			return fmt.Errorf("%s conflicts with the generated local LiveKit SIP topology; unset it for `unmute dev --telephony`", name)
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
		if ctx.Err() == nil && err == nil {
			return errors.New("docker compose log stream exited before shutdown")
		}
		if err != nil && ctx.Err() == nil {
			return fmt.Errorf("docker compose log stream exited: %w", err)
		}
		return nil
	}
}
