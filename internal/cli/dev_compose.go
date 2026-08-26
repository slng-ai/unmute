package cli

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
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
