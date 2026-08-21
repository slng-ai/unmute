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
	"strconv"
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
	run = run.withStream()
	portProbe, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", run.botPort))
	if err != nil {
		return fmt.Errorf("dev %s: local agent port %s is already in use: %w", run.root, run.botPort, err)
	}
	_ = portProbe.Close()

	logSink, closeLog, err := openDevLog(cmd, run)
	if err != nil {
		return err
	}
	defer closeLog()

	web, err := startDevWebServer(cmd, run)
	if err != nil {
		return err
	}
	defer web.close()

	spin := startSpinner(cmd.ErrOrStderr(), "starting the local Pipecat agent")
	agent, err := startPipecatWebAgent(ctx, outDir, run.botPort, run.env, logSink)
	if err != nil {
		spin.Stop()
		return web.holdForFailure(ctx, cmd, run, fmt.Errorf("dev %s: %w", run.root, err))
	}
	defer stopBot(agent.cmd, agent.done)
	if err := pipecatWebAgentReady(ctx, run.botPort, agent.done); err != nil {
		spin.Stop()
		if ctx.Err() != nil {
			return nil
		}
		return web.holdForFailure(ctx, cmd, run,
			fmt.Errorf("dev %s: local Pipecat agent not ready: %w (logs: %s)", run.root, err, run.logPath))
	}
	spin.Stop()
	run.stream.SetState(devStateReady)
	return web.wait(ctx, cmd, run, agent.done)
}

// openDevLog opens the run's log and returns the sink both runners write to. The
// file still receives every byte the process printed, measurement lines included:
// a log that does not match the process makes the measurement path itself
// undebuggable. The stream is a tee, not a filter.
func openDevLog(cmd *cobra.Command, run devWebRun) (io.Writer, func(), error) {
	logFile, err := os.Create(run.logPath)
	if err != nil {
		return nil, nil, fmt.Errorf("dev %s: open log: %w", run.root, err)
	}
	writers := []io.Writer{logFile, run.stream}
	if run.verbose {
		writers = append(writers, cmd.ErrOrStderr())
	}
	return io.MultiWriter(writers...), func() { _ = logFile.Close() }, nil
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
	// stream carries the run's output to the page. Both runners tee their log
	// sink into it, so startup output reaches the browser before there is a
	// runtime to talk to.
	stream *devStream
}

// withStream fills in the stream if the caller did not. Each runner establishes
// this itself rather than trusting every call site to remember, because the only
// symptom of forgetting is a nil dereference deep inside a startup path.
func (r devWebRun) withStream() devWebRun {
	if r.stream == nil {
		r.stream = newDevStream()
	}
	return r
}

// devWebServer is the page, alive before the runtime it describes. Serving first
// is the whole point of the inversion: a build that never finishes used to leave
// the author with a terminal error and no browser, which is precisely when the
// output is worth reading.
type devWebServer struct {
	srv   *http.Server
	errCh chan error
}

// startDevWebServer binds, serves, prints the banner, and opens the browser. It
// no longer waits for a runtime, so a port collision is still caught here but a
// failing build is not.
func startDevWebServer(cmd *cobra.Command, run devWebRun) (*devWebServer, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", run.uiPort))
	if err != nil {
		return nil, fmt.Errorf("dev %s: dev server: %w", run.root, err)
	}
	liveKitURL := "ws://127.0.0.1:7880"
	if port := envValue(run.env, "LIVEKIT_HOST_PORT"); port != "" {
		liveKitURL = "ws://127.0.0.1:" + port
	}
	srv := &http.Server{Handler: devWebMux(run.provider, run.agentName, run.botPort, liveKitURL, run.stream)}
	web := &devWebServer{srv: srv, errCh: make(chan error, 1)}
	go func() { web.errCh <- srv.Serve(ln) }()

	uiURL := fmt.Sprintf("http://localhost:%s/?agent=%s", run.uiPort, url.QueryEscape(run.agentName))
	fmt.Fprintf(cmd.OutOrStdout(), "\n  \033[1;32m▸\033[0m %s\n    ctrl-c to stop  ·  logs: %s\n\n", uiURL, run.logPath)
	if !run.noOpen {
		openBrowser(uiURL)
	}
	return web, nil
}

func (w *devWebServer) close() {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = w.srv.Shutdown(shutdownCtx)
}

// wait blocks until the run ends: an interrupt, the runtime stopping, or the
// server itself failing.
func (w *devWebServer) wait(ctx context.Context, cmd *cobra.Command, run devWebRun, runtimeDone <-chan error) error {
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
	case err := <-w.errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			returnErr = fmt.Errorf("dev %s: dev server: %w", run.root, err)
		}
	}
	return returnErr
}

// holdForFailure keeps a failed run readable. The terminal still gets the error
// and the log path and the command still exits non-zero: the page showing the
// failure is additional, never a replacement for failing loudly.
func (w *devWebServer) holdForFailure(ctx context.Context, cmd *cobra.Command, run devWebRun, cause error) error {
	run.stream.SetState(devStateFailed)
	fmt.Fprintf(cmd.ErrOrStderr(), "\n%v\n", cause)
	fmt.Fprintf(cmd.ErrOrStderr(), "the browser is still showing the output; ctrl-c to stop\n")
	<-ctx.Done()
	return cause
}

// runDevCompose builds and starts the stack, waits for readiness, serves the UI,
// and tears the project-scoped stack down on every exit path (SPEC V5). Data
// volumes are preserved: `down` never passes --volumes. It reuses the same
// compose seams telephony uses, so L2 tests stay hermetic.
func runDevCompose(ctx context.Context, cmd *cobra.Command, run devWebRun) error {
	run = run.withStream()
	logSink, closeLog, err := openDevLog(cmd, run)
	if err != nil {
		return err
	}
	defer closeLog()

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

	// Serve before building. The build is the part that fails, and its output is
	// exactly what the author needs to see when it does.
	web, err := startDevWebServer(cmd, run)
	if err != nil {
		return err
	}
	defer web.close()

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
		return web.holdForFailure(ctx, cmd, run,
			fmt.Errorf("dev %s: docker compose up: %w (logs: %s)", run.root, upErr, run.logPath))
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
			// The stack died during startup. That is a failure the page should
			// show, but the log stream has ended, so there is nothing more to
			// wait for: report it and exit rather than holding an idle page.
			run.stream.SetState(devStateFailed)
			return fmt.Errorf("dev %s: compose stack stopped before the worker registered (logs: %s)", run.root, run.logPath)
		case <-time.After(3 * time.Minute):
			spin.Stop()
			return web.holdForFailure(ctx, cmd, run,
				fmt.Errorf("dev %s: agent worker not ready after 3m (logs: %s)", run.root, run.logPath))
		}
	}

	run.stream.SetState(devStateReady)
	return web.wait(ctx, cmd, run, logsDone)
}

// devWebMux is the one host dev server for both targets. The only per-target
// routing is the transport wiring: pipecat reverse-proxies the WebRTC offer to
// the local bot; livekit serves the vendored SDK. Both share the same
// static page, the /api/session bootstrap (SPEC V6) and the event stream.
func devWebMux(provider ir.Provider, agentName, botPort, liveKitURL string, stream *devStream) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/session", devSessionHandler(provider, agentName, liveKitURL, stream))
	mux.HandleFunc("/api/events", devEventsHandler(stream))
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
// Since the page is served before the runtime exists, this has to be able to
// answer that there is nothing to connect to yet. `ready: false` is the page's
// instruction to keep the call control unavailable rather than offer a call that
// cannot be placed; a ready answer carries exactly what it always did.
func devSessionHandler(provider ir.Provider, agentName, liveKitURL string, stream *devStream) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if stream != nil && stream.State() != devStateReady {
			_ = json.NewEncoder(w).Encode(map[string]any{"kind": providerSessionKind(provider), "ready": false})
			return
		}
		switch provider {
		case ir.ProviderPipecat:
			_ = json.NewEncoder(w).Encode(map[string]any{"kind": "webrtc-offer", "offerUrl": "/api/offer", "ready": true})
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
			token, err := mintLiveKitToken(liveKitDevKey, liveKitDevSecret, room, identity, agentName, covalDispatchMetadata(r), time.Now(), 30*time.Minute)
			if err != nil {
				http.Error(w, fmt.Sprintf("mint token: %v", err), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"kind": "livekit", "url": liveKitURL, "token": token, "room": room, "ready": true})
		default:
			http.Error(w, "unsupported transport", http.StatusInternalServerError)
		}
	}
}

// providerSessionKind is the transport name the page switches on. It is answered
// even before readiness so the page knows which transport it will be joining.
func providerSessionKind(provider ir.Provider) string {
	if provider == ir.ProviderLiveKit {
		return "livekit"
	}
	return "webrtc-offer"
}

// devEventsHandler streams the run to the page as server-sent events. One stream
// carries every kind, because output lines and measurement records come off the
// same writer and the page wants them in the writer's order.
//
// SSE rather than a WebSocket: the standard library has no WebSocket and the
// dependency rules forbid adding one for this. An http.Flusher is enough, and
// the browser's own reconnect carries Last-Event-ID for free.
func devEventsHandler(stream *devStream) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok || stream == nil {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		// Localhost only, but the page is the only intended reader either way.
		w.Header().Set("X-Accel-Buffering", "no")

		after := 0
		if raw := r.Header.Get("Last-Event-ID"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil {
				after = n
			}
		}
		backlog, live, cancel := stream.Subscribe(after)
		defer cancel()

		for _, ev := range backlog {
			if !writeDevEvent(w, ev) {
				return
			}
		}
		flusher.Flush()

		for {
			select {
			case <-r.Context().Done():
				return
			case ev, open := <-live:
				if !open {
					return
				}
				if !writeDevEvent(w, ev) {
					return
				}
				flusher.Flush()
			}
		}
	}
}

func writeDevEvent(w http.ResponseWriter, ev devEvent) bool {
	payload, err := json.Marshal(ev)
	if err != nil {
		return true // skip one unencodable event rather than dropping the stream
	}
	_, err = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.Seq, payload)
	return err == nil
}

// covalSimulationHeader is the header Coval sets on a token request when it
// drives a LiveKit agent. A deployed agent uses the operator's own token server,
// so this handler is only the local half; the emitted README states what a
// production token server has to forward.
const covalSimulationHeader = "X-Coval-Simulation-Id"

// covalDispatchMetadata turns a Coval simulation header on the token request
// into the dispatch metadata the emitted agent reads back as ctx.job.metadata.
// Empty when the header is absent, which is every ordinary browser session.
func covalDispatchMetadata(r *http.Request) string {
	simulation := r.Header.Get(covalSimulationHeader)
	if simulation == "" {
		return ""
	}
	// The key matches the participant attribute the emitted inbound trunk maps,
	// so the agent's resolver reads one name whichever route the call took.
	metadata, err := json.Marshal(map[string]string{"coval.simulation_id": simulation})
	if err != nil {
		return ""
	}
	return string(metadata)
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
