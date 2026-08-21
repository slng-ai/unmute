package cli

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/target"
)

var (
	composeLookPath  = exec.LookPath
	composeCommand   = exec.CommandContext
	composePreflight = preflightCompose
	composeSlug      = regexp.MustCompile(`[^a-z0-9_-]+`)
)

// composeInstallHint names what to install; both entry points word their own
// missing-docker sentence around it.
const composeInstallHint = "install Docker Desktop or Docker Engine with the Compose plugin"

// preflightCompose is the --telephony gate. Its message is unchanged on purpose
// (telephony behaviour is byte-for-byte stable, SPEC V10).
func preflightCompose(ctx context.Context, env []string) error {
	return preflightComposeCore(ctx, env, "docker compose is required for --telephony; "+composeInstallHint)
}

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

func externalTelephonyEnv(plan *generate.TelephonyRuntimePlan) []string {
	supplied := make(map[string]bool, len(plan.LocalEnvironment))
	for _, name := range plan.LocalEnvironment {
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
		// The names this command mints are locally supplied too, and it simply
		// overwrites them: only the generated topology's own values conflict.
		if slices.Contains(devMintedEnvironment, name) {
			continue
		}
		if values[name] != "" {
			return fmt.Errorf("%s conflicts with the generated local LiveKit SIP topology; unset it for `unmute dev --telephony`", name)
		}
	}
	return nil
}

// hostPort is one port this run will publish, and the one thing to change when
// it is already taken.
type hostPort struct {
	// Port is the host port, as a string because that is how every one of these
	// arrives: from a flag, or from an environment variable, or from a default.
	Port string
	// What names it in the message. Not the container's word for it: the reader
	// is looking at their own machine.
	What string
	// MovedBy is the flag or variable that puts it somewhere else. A message that
	// says a port is busy and not how to move it makes the reader go looking.
	MovedBy string
}

// telephonyHostPorts is what this run will publish, per plane.
//
// Only the ports this command knows by name. Parsing them out of the generated
// Compose file would cover more, and would mean expanding shell defaults out of
// a YAML string, for ports whose collisions Docker already reports clearly. The
// one it does not report clearly is the agent's, which is the reason this exists.
func telephonyHostPorts(plan *generate.TelephonyRuntimePlan, botPort string, env []string) []hostPort {
	ports := []hostPort{{Port: botPort, What: "the local agent", MovedBy: "--bot-port"}}
	if plan == nil || plan.LocalPlane != string(target.LocalPlaneSIP) {
		return ports
	}
	// The SIP plane publishes a control port and a signalling port too, and both
	// have their own variable. The RTP range is deliberately not checked: it is a
	// hundred ports, and probing a hundred listeners on every start to catch a
	// collision Docker names accurately is not worth the wait.
	return append(ports,
		hostPort{Port: envOrDefault(env, "LIVEKIT_HOST_PORT", "7880"), What: "LiveKit Server", MovedBy: "LIVEKIT_HOST_PORT"},
		hostPort{Port: envOrDefault(env, "LIVEKIT_SIP_HOST_PORT", "5060"), What: "LiveKit SIP", MovedBy: "LIVEKIT_SIP_HOST_PORT"},
	)
}

// envOrDefault reads a name out of the child environment, falling back to the
// same default the generated Compose file uses. The two have to agree, which is
// why the defaults are written beside the names that carry them.
func envOrDefault(env []string, name, fallback string) string {
	if value := envValue(env, name); value != "" {
		return value
	}
	return fallback
}

// rejectOccupiedHostPorts stops a run whose ports are already taken, naming the
// port and what moves it.
//
// Two failures, and the quiet one is worse. A published port that collides makes
// Docker refuse with its own message, which is at least a message. The **agent**
// port does not collide loudly: a second run reaches the first run's agent, gets
// an answer meant for a different package, and reports something that looks like
// a bug in the caller's own code. Found by hitting it: a 404 from a route the
// emitted agent plainly defines, because the port belonged to another run.
// hostPortCheck is the seam, in the same shape as composePreflight beside it: a
// test that stands in for the container runtime is also standing in for the
// thing that binds these ports, so it has to be able to turn this off. Its own
// gate is TestDevRefusesAPortAnotherRunIsHolding, which calls the function.
var hostPortCheck = rejectOccupiedHostPorts

func rejectOccupiedHostPorts(ports []hostPort) error {
	for _, port := range ports {
		if port.Port == "" {
			continue
		}
		// Both addresses, because neither one alone sees every holder. Measured on
		// darwin: a wildcard bind *succeeds* while loopback is held, so probing
		// 0.0.0.0 alone misses another `unmute dev`, whose Compose publishes on
		// loopback by default. Probing 127.0.0.1 alone misses a container
		// published on the wildcard, which is how Docker publishes and is the
		// collision people actually hit: another run's LiveKit Server on 7880
		// sailed past a loopback-only probe and failed later as Compose's own
		// bind error, buried in the log file.
		for _, host := range []string{"127.0.0.1", "0.0.0.0"} {
			listener, err := net.Listen("tcp", net.JoinHostPort(host, port.Port))
			if err != nil {
				return fmt.Errorf("port %s is already in use, and this run needs it for %s. Another "+
					"`unmute dev` or container is probably still up: stop it, or move this one with %s. "+
					"Left alone, this run would reach the other one's agent and report its answers as yours",
					port.Port, port.What, port.MovedBy)
			}
			// Closed at once. This is a probe, not a reservation: holding it would
			// be holding the port away from the thing about to publish it.
			if err := listener.Close(); err != nil {
				return fmt.Errorf("release the probe on port %s: %w", port.Port, err)
			}
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

// telephonyComposeRun is one Compose execution. The optional hooks exist for
// the zero-step flow: infraServices+beforeApp bring up the route's
// infrastructure first so the dev command can create LiveKit trunk records
// and extend the environment before the application starts; onReady runs
// after the full graph is healthy (carrier webhook config, call line).
type telephonyComposeRun struct {
	dir, file, project string
	env                []string
	output             io.Writer
	stdout, stderr     io.Writer
	logPath            string
	infraServices      []string
	beforeApp          func(ctx context.Context, env []string) ([]string, error)
	onReady            func(ctx context.Context) error
	// onStop undoes what onReady did to the outside world. It runs on every
	// exit path after startup, including ctrl-c, with its own context: the
	// run's ctx is already cancelled by then (V14).
	onStop func(ctx context.Context) error
}

func runTelephonyCompose(ctx context.Context, run telephonyComposeRun) (returnErr error) {
	dir, file, project := run.dir, run.file, run.project
	env, output, stdout, stderr, logPath := run.env, run.output, run.stdout, run.stderr, run.logPath
	cleanup := func() error {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		down := composeCommand(cleanupCtx, "docker", composeArgs(file, project, "down", "--remove-orphans", "--timeout", "30")...)
		down.Dir, down.Env, down.Stdout, down.Stderr = dir, env, output, output
		return down.Run()
	}
	upServices := func(services ...string) error {
		args := append(composeArgs(file, project, "up", "--build", "--detach", "--remove-orphans", "--wait"), services...)
		up := composeCommand(ctx, "docker", args...)
		up.Dir, up.Env, up.Stdout, up.Stderr = dir, env, output, output
		return up.Run()
	}
	failStartup := func(err error) error {
		_ = cleanup()
		if composeWasInterrupted(ctx, err) {
			fmt.Fprintln(stderr, "\nstopping...")
			return nil
		}
		fmt.Fprintf(stderr, "telephony Compose failed to start. logs: %s\n", logPath)
		return fmt.Errorf("start Docker Compose topology: %w", err)
	}

	if len(run.infraServices) > 0 {
		spin := startSpinner(stderr, "starting telephony infrastructure services")
		err := upServices(run.infraServices...)
		spin.Stop()
		if err != nil {
			return failStartup(err)
		}
		if run.beforeApp != nil {
			extended, err := run.beforeApp(ctx, env)
			if err != nil {
				_ = cleanup()
				return err
			}
			env = extended
		}
	}
	spin := startSpinner(stderr, "starting telephony Compose services")
	err := upServices()
	spin.Stop()
	if err != nil {
		return failStartup(err)
	}
	defer func() {
		// Undo the outward-facing changes before the containers go, and on a
		// fresh context: ctrl-c has already cancelled ctx by this point (V14).
		if run.onStop != nil {
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := run.onStop(stopCtx); err != nil {
				restoreErr := fmt.Errorf("restore telephony carrier state: %w", err)
				fmt.Fprintf(stderr, "warning: %v\n", restoreErr)
				returnErr = errors.Join(returnErr, restoreErr)
			}
		}
		if err := cleanup(); err != nil {
			fmt.Fprintf(stderr, "warning: stop telephony Compose project %s: %v\n", project, err)
		}
	}()

	fmt.Fprintf(stdout, "\n  \033[1;32m▸\033[0m telephony route ready\n    compose project: %s  ·  ctrl-c to stop  ·  logs: %s\n\n", project, logPath)
	if run.onReady != nil {
		if err := run.onReady(ctx); err != nil {
			return err
		}
	}
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
		// Print only once the log stream is gone: `compose logs --follow` copies
		// into the same writer from its own goroutine, so printing first is an
		// unsynchronised concurrent write (caught by `go test -race`).
		cancelLogs()
		<-logsDone
		fmt.Fprintln(stderr, "\nstopping...")
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
