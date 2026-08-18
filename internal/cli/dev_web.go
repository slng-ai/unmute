package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
	"github.com/slng-ai/unmute/internal/web"
	"github.com/spf13/cobra"
)

// The containerized dev livekit-server runs `--dev`, whose placeholder key pair
// is devkey/secret (doc-verified 2026-07-22, docs.livekit.io self-hosting). The
// browser reaches the published port on the host; the worker reaches it by the
// compose network name (see compose.dev.yaml). Never production values.
const (
	liveKitDevKey    = "devkey"
	liveKitDevSecret = "secret"
)

// devWebMissingDockerHint mirrors the cloudflared install hint's shape: what is
// missing and how to install it. LiveKit needs its local server stack, so its
// browser path still runs through Compose.
const devWebMissingDockerHint = "docker compose is required to run `unmute dev`; " + composeInstallHint

// runDevWeb is the default `unmute dev` runner (SPEC V1): generate the
// deployable project, start the target's local runtime, then serve one
// standardized WebRTC web UI. Pipecat runs on the host because a browser cannot
// reach the ephemeral UDP ICE candidates gathered inside Docker Desktop;
// LiveKit still needs its local server stack in Compose.
func runDevWeb(cmd *cobra.Command, root, targetName, uiPort, botPort string, noOpen, verbose bool) error {
	agent, targets, err := loadPackage(root, []string{targetName})
	if err != nil {
		return fmt.Errorf("dev %s: %w", root, err)
	}
	resolved := targets[0]
	switch resolved.Provider {
	case ir.ProviderPipecat, ir.ProviderLiveKit:
		// code targets: run the deployable container below.
	default:
		return fmt.Errorf("dev %s: target %q uses %s; its dev runner is not implemented", root, resolved.Name, resolved.Provider)
	}

	artifact, err := generate.Generate(agent, resolved, target.Default())
	if err != nil {
		return fmt.Errorf("dev %s: %w", root, err)
	}
	for _, warning := range artifact.Notes.Warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
	}
	outDir := filepath.Join(root, "build", resolved.Name)
	if err := writeArtifactFiles(cmd.ErrOrStderr(), outDir, artifact.Files); err != nil {
		return fmt.Errorf("dev %s: %w", root, err)
	}
	composeFile := filepath.Join(outDir, "compose.dev.yaml")
	if _, err := os.Stat(composeFile); err != nil {
		return fmt.Errorf("dev %s: generated project has no compose.dev.yaml: %w", root, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "compiled %s\n", outDir)

	childEnv := devChildEnv(root, cmd.ErrOrStderr())
	// compose.dev.yaml publishes the bot on ${UNMUTE_DEV_PORT}; --bot-port sets it.
	childEnv = setChildEnv(childEnv, "UNMUTE_DEV_PORT", botPort)

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	run := devWebRun{
		root:        root,
		provider:    resolved.Provider,
		agentName:   resolved.Name,
		composeFile: composeFile,
		project:     composeProjectName(root, resolved.Name),
		env:         childEnv,
		logPath:     filepath.Join(outDir, "dev.log"),
		uiPort:      uiPort,
		botPort:     botPort,
		noOpen:      noOpen,
		verbose:     verbose,
	}
	if resolved.Provider == ir.ProviderPipecat {
		if _, err := pipecatLookPath("uv"); err != nil {
			return fmt.Errorf("dev %s: uv not found on PATH; Pipecat WebRTC runs locally with uv (https://docs.astral.sh/uv/)", root)
		}
		return runDevPipecat(ctx, cmd, outDir, run)
	}

	if err := preflightComposeCore(ctx, childEnv, devWebMissingDockerHint); err != nil {
		return fmt.Errorf("dev %s: %w", root, err)
	}
	return runDevCompose(ctx, cmd, run)
}

var startPipecatWebAgent = func(ctx context.Context, dir, port string, env []string, sink io.Writer) (*localAgent, error) {
	return startLocalPipecatAgent(ctx, dir, env, sink,
		"-t", "webrtc", "--host", "0.0.0.0", "--port", port)
}

var pipecatWebAgentReady = waitForLocalAgentReady
var pipecatLookPath = exec.LookPath

func runDevPipecat(ctx context.Context, cmd *cobra.Command, outDir string, run devWebRun) error {
	portProbe, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", run.botPort))
	if err != nil {
		return fmt.Errorf("dev %s: local agent port %s is already in use: %w", run.root, run.botPort, err)
	}
	_ = portProbe.Close()

	logFile, err := os.Create(run.logPath)
	if err != nil {
		return fmt.Errorf("dev %s: open log: %w", run.root, err)
	}
	defer func() { _ = logFile.Close() }()
	var logSink io.Writer = logFile
	if run.verbose {
		logSink = io.MultiWriter(logFile, cmd.ErrOrStderr())
	}

	spin := startSpinner(cmd.ErrOrStderr(), "starting the local Pipecat agent")
	agent, err := startPipecatWebAgent(ctx, outDir, run.botPort, run.env, logSink)
	if err != nil {
		spin.Stop()
		return fmt.Errorf("dev %s: %w", run.root, err)
	}
	defer stopBot(agent.cmd, agent.done)
	if err := pipecatWebAgentReady(ctx, run.botPort, agent.done); err != nil {
		spin.Stop()
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("dev %s: local Pipecat agent not ready: %w (logs: %s)", run.root, err, run.logPath)
	}
	spin.Stop()
	return serveDevWeb(ctx, cmd, run, agent.done)
}

type devWebRun struct {
	root        string
	provider    ir.Provider
	agentName   string
	composeFile string
	project     string
	env         []string
	logPath     string
	uiPort      string
	botPort     string
	noOpen      bool
	verbose     bool
}

// runDevCompose builds and starts the stack, waits for readiness, serves the UI,
// and tears the project-scoped stack down on every exit path (SPEC V5). Data
// volumes are preserved: `down` never passes --volumes. It reuses the same
// compose seams telephony uses, so L2 tests stay hermetic.
func runDevCompose(ctx context.Context, cmd *cobra.Command, run devWebRun) error {
	logFile, err := os.Create(run.logPath)
	if err != nil {
		return fmt.Errorf("dev %s: open log: %w", run.root, err)
	}
	defer func() { _ = logFile.Close() }()
	var logSink io.Writer = logFile
	if run.verbose {
		logSink = io.MultiWriter(logFile, cmd.ErrOrStderr())
	}

	// Teardown runs on every path, even a ctrl-c mid-build: a background context
	// so cancellation of ctx cannot abort the `down` itself.
	defer func() {
		downCtx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		down := composeCommand(downCtx, "docker", composeArgs(run.composeFile, run.project, "down", "--remove-orphans", "--timeout", "30")...)
		down.Env = run.env
		down.Stdout, down.Stderr = logSink, logSink
		_ = down.Run()
	}()

	spin := startSpinner(cmd.ErrOrStderr(), "building and starting the container")
	up := composeCommand(ctx, "docker", composeArgs(run.composeFile, run.project, "up", "--build", "--detach", "--remove-orphans", "--wait")...)
	up.Env = run.env
	up.Stdout, up.Stderr = logSink, logSink
	upErr := up.Run()
	spin.Stop()
	if upErr != nil {
		if composeWasInterrupted(ctx, upErr) {
			return nil
		}
		return fmt.Errorf("dev %s: docker compose up: %w (logs: %s)", run.root, upErr, run.logPath)
	}

	// Stream compose logs to dev.log for the whole run. For livekit, watch for
	// the worker's "registered worker" line so the browser opens only once agent
	// dispatch will fire; pipecat is ready as soon as `up --wait` returns.
	ready := make(chan struct{})
	logsSink := logSink
	if run.provider == ir.ProviderLiveKit {
		logsSink = &readyWatcher{w: logSink, marker: []byte("registered worker"), fire: func() { close(ready) }}
	} else {
		close(ready)
	}
	logs := composeCommand(ctx, "docker", composeArgs(run.composeFile, run.project, "logs", "--follow", "--no-color")...)
	logs.Env = run.env
	logs.Stdout, logs.Stderr = logsSink, logsSink
	if err := logs.Start(); err != nil {
		return fmt.Errorf("dev %s: stream logs: %w", run.root, err)
	}
	logsDone := make(chan error, 1)
	go func() { logsDone <- logs.Wait() }()

	if run.provider == ir.ProviderLiveKit {
		spin := startSpinner(cmd.ErrOrStderr(), "waiting for the agent worker to register")
		select {
		case <-ready:
			spin.Stop()
		case <-ctx.Done():
			spin.Stop()
			return nil
		case <-logsDone:
			spin.Stop()
			return fmt.Errorf("dev %s: compose stack stopped before the worker registered (logs: %s)", run.root, run.logPath)
		case <-time.After(3 * time.Minute):
			spin.Stop()
			return fmt.Errorf("dev %s: agent worker not ready after 3m (logs: %s)", run.root, run.logPath)
		}
	}

	return serveDevWeb(ctx, cmd, run, logsDone)
}

// serveDevWeb owns the target-neutral HTTP page once its runtime is ready.
func serveDevWeb(ctx context.Context, cmd *cobra.Command, run devWebRun, runtimeDone <-chan error) error {
	// Bind eagerly so a port collision fails here, before the banner.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", run.uiPort))
	if err != nil {
		return fmt.Errorf("dev %s: dev server: %w", run.root, err)
	}
	liveKitURL := "ws://127.0.0.1:7880"
	if port := envValue(run.env, "LIVEKIT_HOST_PORT"); port != "" {
		liveKitURL = "ws://127.0.0.1:" + port
	}
	srv := &http.Server{Handler: devWebMux(run.provider, run.agentName, run.botPort, liveKitURL)}
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Serve(ln) }()

	uiURL := fmt.Sprintf("http://localhost:%s/?agent=%s", run.uiPort, url.QueryEscape(run.agentName))
	fmt.Fprintf(cmd.OutOrStdout(), "\n  \033[1;32m▸\033[0m %s\n    ctrl-c to stop  ·  logs: %s\n\n", uiURL, run.logPath)
	if !run.noOpen {
		openBrowser(uiURL)
	}

	var returnErr error
	select {
	case <-ctx.Done():
		fmt.Fprintln(cmd.ErrOrStderr(), "\nstopping...")
	case err := <-runtimeDone:
		if err != nil && ctx.Err() == nil && !composeWasInterrupted(ctx, err) {
			returnErr = fmt.Errorf("dev %s: local runtime stopped: %w (logs: %s)", run.root, err, run.logPath)
		}
		if ctx.Err() == nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "\nlocal runtime stopped; see %s\n", run.logPath)
		}
	case err := <-srvErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			returnErr = fmt.Errorf("dev %s: dev server: %w", run.root, err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	return returnErr
}

// devWebMux is the one host dev server for both targets. The only per-target
// routing is the transport wiring: pipecat reverse-proxies the WebRTC offer to
// the local bot; livekit serves the vendored SDK. Both share the same
// static page and the /api/session bootstrap (SPEC V6).
func devWebMux(provider ir.Provider, agentName, botPort, liveKitURL string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/session", devSessionHandler(provider, agentName, liveKitURL))
	switch provider {
	case ir.ProviderPipecat:
		// The offer (and any other bot API) proxies to the selected bot port.
		// /api/session is more specific, so it wins over this prefix.
		bot, _ := url.Parse("http://" + net.JoinHostPort("127.0.0.1", botPort))
		mux.Handle("/api/", httputil.NewSingleHostReverseProxy(bot))
	case ir.ProviderLiveKit:
		mux.HandleFunc("/livekit-client.umd.js", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFileFS(w, r, web.FS, "livekit-client.umd.js")
		})
	}
	mux.Handle("/", http.FileServer(http.FS(web.FS)))
	return mux
}

// devSessionHandler answers GET /api/session with the transport contract the
// one web page switches on (SPEC I.session, V6). Pipecat gets the offer URL to
// POST its WebRTC offer to; livekit gets the dev server URL and a fresh token
// (a new room per request so agent dispatch fires at room creation).
func devSessionHandler(provider ir.Provider, agentName, liveKitURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch provider {
		case ir.ProviderPipecat:
			_ = json.NewEncoder(w).Encode(map[string]string{"kind": "webrtc-offer", "offerUrl": "/api/offer"})
		case ir.ProviderLiveKit:
			room, err := randomRoomName("unmute")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			identity, err := randomRoomName("user")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			token, err := mintLiveKitToken(liveKitDevKey, liveKitDevSecret, room, identity, agentName, time.Now(), 30*time.Minute)
			if err != nil {
				http.Error(w, fmt.Sprintf("mint token: %v", err), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"kind": "livekit", "url": liveKitURL, "token": token, "room": room})
		default:
			http.Error(w, "unsupported transport", http.StatusInternalServerError)
		}
	}
}

// readyWatcher tees a stream to w while watching for a marker line; it fires
// once when the marker first appears (the agent worker's "registered worker"
// log). Concurrent-safe because compose logs stdout and stderr share it.
type readyWatcher struct {
	mu     sync.Mutex
	w      io.Writer
	marker []byte
	buf    []byte
	done   bool
	once   sync.Once
	fire   func()
}

func (rw *readyWatcher) Write(p []byte) (int, error) {
	rw.mu.Lock()
	if !rw.done {
		rw.buf = append(rw.buf, p...)
		if bytes.Contains(rw.buf, rw.marker) {
			rw.done = true
			rw.buf = nil
			rw.mu.Unlock()
			rw.once.Do(rw.fire)
			return rw.w.Write(p)
		}
		if len(rw.buf) > 8192 { // bound memory; keep a tail in case the marker splits a write
			rw.buf = rw.buf[len(rw.buf)-len(rw.marker):]
		}
	}
	rw.mu.Unlock()
	return rw.w.Write(p)
}
