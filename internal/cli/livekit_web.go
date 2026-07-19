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
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/slng/unmute/internal/generate"
	"github.com/slng/unmute/internal/target"
	"github.com/slng/unmute/internal/web"
	"github.com/spf13/cobra"
)

// liveKitCreds are the LiveKit server credentials the web dev client needs. URL
// is the room server; the key/secret sign the participant token (C5). Console
// mode reads none of these unless a binding routes through Inference (C2).
type liveKitCreds struct {
	URL       string
	APIKey    string
	APISecret string
}

// liveKitCredsFromEnv reads the creds from a merged env slice (ambient + .env,
// as devChildEnv builds it). Last value wins so a repo .env overrides a stale
// ambient value.
func liveKitCredsFromEnv(env []string) liveKitCreds {
	get := func(key string) string {
		val := ""
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, key+"="); ok {
				val = v
			}
		}
		return val
	}
	return liveKitCreds{
		URL:       get("LIVEKIT_URL"),
		APIKey:    get("LIVEKIT_API_KEY"),
		APISecret: get("LIVEKIT_API_SECRET"),
	}
}

// missing lists which required creds are empty, for a preflight error (C7).
func (c liveKitCreds) missing() []string {
	var out []string
	if c.URL == "" {
		out = append(out, "LIVEKIT_URL")
	}
	if c.APIKey == "" {
		out = append(out, "LIVEKIT_API_KEY")
	}
	if c.APISecret == "" {
		out = append(out, "LIVEKIT_API_SECRET")
	}
	return out
}

// runLiveKitWeb runs a livekit target in the browser (the pipecat web mode's
// counterpart, V5): compile, preflight the LiveKit creds, start the agent
// worker (`uv run agent.py dev`), wait for it to register, then serve the
// livekit dev client and open the browser. The browser joins a LiveKit room
// directly (no local proxy); the worker joins the same room via token dispatch.
func runLiveKitWeb(cmd *cobra.Command, root, targetName, uiPort string, noOpen, verbose bool) error {
	agent, targets, err := loadPackage(root, []string{targetName})
	if err != nil {
		return fmt.Errorf("dev %s: %w", root, err)
	}
	resolved := targets[0]

	artifact, err := generate.Generate(agent, resolved, target.Default())
	if err != nil {
		return fmt.Errorf("dev %s: %w", root, err)
	}
	for _, warning := range artifact.Notes.Warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
	}
	outDir := filepath.Join(root, "build", resolved.Name)
	if err := writeArtifactFiles(outDir, artifact.Files); err != nil {
		return fmt.Errorf("dev %s: %w", root, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "compiled %s\n", outDir)

	childEnv := devChildEnv(root, cmd.ErrOrStderr())
	creds := liveKitCredsFromEnv(childEnv)
	if missing := creds.missing(); len(missing) > 0 {
		return fmt.Errorf("dev %s: livekit web mode needs %s in the environment or .env (LiveKit Cloud or a self-hosted server); the browser client connects to that server. To talk without any LiveKit credentials, use the terminal instead: `unmute dev %s --console`",
			root, strings.Join(missing, ", "), root)
	}
	if _, err := exec.LookPath("uv"); err != nil {
		return fmt.Errorf("dev %s: uv not found on PATH; install uv to run the agent locally (https://docs.astral.sh/uv/)", root)
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logPath := filepath.Join(outDir, "agent.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("dev %s: open log: %w", root, err)
	}
	defer func() { _ = logFile.Close() }()
	var sink io.Writer = logFile
	if verbose {
		sink = io.MultiWriter(logFile, cmd.ErrOrStderr())
	}
	ready := make(chan struct{})
	watch := &readyWatcher{w: sink, marker: []byte("registered worker"), fire: func() { close(ready) }}

	worker := exec.Command("uv", "run", "agent.py", "dev")
	worker.Dir = outDir
	worker.Env = childEnv
	worker.Stdout = watch
	worker.Stderr = watch
	worker.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // own group so we can kill children
	if err := worker.Start(); err != nil {
		return fmt.Errorf("dev %s: start agent: %w", root, err)
	}
	defer killGroup(worker)

	workerDone := make(chan error, 1)
	go func() { workerDone <- worker.Wait() }()

	spin := startSpinner(cmd.ErrOrStderr(), "starting agent worker")
	select {
	case <-ready:
		spin.Stop()
	case err := <-workerDone:
		spin.Stop()
		fmt.Fprintf(cmd.ErrOrStderr(), "agent failed to start. logs: %s\n", logPath)
		if err != nil {
			return fmt.Errorf("dev %s: agent exited before ready: %w", root, err)
		}
		return fmt.Errorf("dev %s: agent exited before ready", root)
	case <-ctx.Done():
		spin.Stop()
		stopBot(worker, workerDone)
		return nil
	case <-time.After(3 * time.Minute):
		spin.Stop()
		fmt.Fprintf(cmd.ErrOrStderr(), "agent worker not ready after 3m. logs: %s\n", logPath)
		return fmt.Errorf("dev %s: agent worker not ready", root)
	}

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", uiPort))
	if err != nil {
		return fmt.Errorf("dev %s: dev server: %w", root, err)
	}
	srv := &http.Server{Handler: liveKitDevMux(creds, resolved.Name)}
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Serve(ln) }()

	uiURL := fmt.Sprintf("http://localhost:%s/", uiPort)
	fmt.Fprintf(cmd.OutOrStdout(), "\n  \033[1;32m▸\033[0m %s\n    ctrl-c to stop  ·  logs: %s\n\n", uiURL, logPath)
	if !noOpen {
		openBrowser(uiURL)
	}

	select {
	case <-ctx.Done():
		fmt.Fprintln(cmd.ErrOrStderr(), "\nstopping...")
	case err := <-workerDone:
		_ = srv.Close()
		if err != nil {
			return fmt.Errorf("dev %s: agent exited: %w", root, err)
		}
		return nil
	case err := <-srvErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("dev %s: dev server: %w", root, err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	stopBot(worker, workerDone)
	return nil
}

// liveKitDevMux routes the livekit dev server: the token endpoint plus the
// static client (livekit.html at /, the vendored UMD next to it). Split out so
// the routing is unit-testable without a running worker.
func liveKitDevMux(creds liveKitCreds, agentName string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/token", liveKitTokenHandler(creds, agentName))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.ServeFileFS(w, r, web.FS, "livekit.html")
		case "/livekit-client.umd.js":
			http.ServeFileFS(w, r, web.FS, "livekit-client.umd.js")
		default:
			http.NotFound(w, r)
		}
	})
	return mux
}

// readyWatcher tees a child's output to w while watching for a marker line; it
// fires once when the marker first appears (the agent worker's "registered
// worker" log). stdout and stderr share one watcher, so Write is guarded.
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

// liveKitTokenHandler serves GET /api/token → {url, token, room}. Each request
// mints a fresh room so the token's agent dispatch fires at room creation (C5,
// V5); the browser connects to url with token and the agent joins that room.
func liveKitTokenHandler(creds liveKitCreds, agentName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		token, err := mintLiveKitToken(creds.APIKey, creds.APISecret, room, identity, agentName, time.Now(), 30*time.Minute)
		if err != nil {
			http.Error(w, fmt.Sprintf("mint token: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"url":   creds.URL,
			"token": token,
			"room":  room,
		})
	}
}
