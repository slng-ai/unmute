package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/slng/unmute/internal/generate"
	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/target"
	"github.com/slng/unmute/internal/web"
	"github.com/spf13/cobra"
)

func newDevCmd() *cobra.Command {
	var uiPort, botPort, targetName string
	var noOpen, verbose bool

	cmd := &cobra.Command{
		Use:   "dev <agent-dir>",
		Short: "Compile, run the agent locally, and talk to it in the browser.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := args[0]

			selected, err := selectDevTarget(cmd, root, targetName)
			if err != nil {
				return err
			}
			outDir, err := compileTargetForDev(cmd, root, selected)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "compiled %s\n", outDir)

			childEnv := devChildEnv(root, cmd.ErrOrStderr())
			if _, err := exec.LookPath("uv"); err != nil {
				return fmt.Errorf("dev %s: uv not found on PATH; install uv to run the agent locally (https://docs.astral.sh/uv/)", root)
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			logPath := filepath.Join(outDir, "bot.log")
			logFile, err := os.Create(logPath)
			if err != nil {
				return fmt.Errorf("dev %s: open log: %w", root, err)
			}
			defer func() { _ = logFile.Close() }()
			var botOut io.Writer = logFile
			if verbose {
				botOut = io.MultiWriter(logFile, cmd.ErrOrStderr())
			}

			bot := exec.Command("uv", "run", "bot.py", "--host", "127.0.0.1", "--port", botPort)
			bot.Dir = outDir
			bot.Env = childEnv
			bot.Stdout = botOut
			bot.Stderr = botOut
			bot.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // own group so we can kill children
			if err := bot.Start(); err != nil {
				return fmt.Errorf("dev %s: start agent: %w", root, err)
			}
			defer killGroup(bot)

			botDone := make(chan error, 1)
			go func() { botDone <- bot.Wait() }()

			spin := startSpinner(cmd.ErrOrStderr(), "starting agent")
			waitErr := waitPort(ctx, net.JoinHostPort("127.0.0.1", botPort), botDone, 3*time.Minute)
			spin.Stop()
			if waitErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "agent failed to start. logs: %s\n", logPath)
				return fmt.Errorf("dev %s: %w", root, waitErr)
			}

			target, _ := url.Parse("http://" + net.JoinHostPort("127.0.0.1", botPort))
			mux := http.NewServeMux()
			mux.Handle("/api/", httputil.NewSingleHostReverseProxy(target)) // WebRTC offer → Pipecat runner
			mux.Handle("/", http.FileServer(http.FS(web.FS)))

			// Bind eagerly so a port collision fails here, before the banner.
			ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", uiPort))
			if err != nil {
				return fmt.Errorf("dev %s: dev server: %w", root, err)
			}
			srv := &http.Server{Handler: mux}
			srvErr := make(chan error, 1)
			go func() { srvErr <- srv.Serve(ln) }()

			uiURL := fmt.Sprintf("http://localhost:%s/?agent=%s", uiPort, url.QueryEscape(filepath.Base(root)))
			fmt.Fprintf(cmd.OutOrStdout(), "\n  \033[1;32m▸\033[0m %s\n    ctrl-c to stop  ·  logs: %s\n\n", uiURL, logPath)
			if !noOpen {
				openBrowser(uiURL)
			}

			select {
			case <-ctx.Done():
				fmt.Fprintln(cmd.ErrOrStderr(), "\nstopping...")
			case err := <-botDone:
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
			stopBot(bot, botDone)
			return nil
		},
	}

	cmd.Flags().StringVar(&uiPort, "port", "8765", "port for the local dev UI")
	cmd.Flags().StringVar(&botPort, "bot-port", "7860", "port the Pipecat runner listens on")
	cmd.Flags().StringVar(&targetName, "target", "", "target instance name (required without a TTY when multiple exist)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open the browser automatically")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "stream agent logs to stderr (default: write to bot.log only)")
	return cmd
}

// spinner draws a single-line braille spinner on a TTY and is a no-op printer
// elsewhere (tests, CI, piped output). Stdlib only.
type spinner struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

func startSpinner(w io.Writer, msg string) *spinner {
	s := &spinner{stop: make(chan struct{}), done: make(chan struct{})}
	if !isTTY(w) {
		fmt.Fprintln(w, msg+"...")
		close(s.done)
		return s
	}
	go func() {
		defer close(s.done)
		frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		i := 0
		for {
			select {
			case <-s.stop:
				fmt.Fprint(w, "\r\033[2K")
				return
			case <-t.C:
				fmt.Fprintf(w, "\r\033[2K \033[33m%c\033[0m %s", frames[i%len(frames)], msg)
				i++
			}
		}
	}()
	return s
}

func (s *spinner) Stop() {
	s.once.Do(func() { close(s.stop) })
	<-s.done
}

func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// selectDevTarget chooses by exact instance name. A single target needs no
// prompt; multiple targets never fall back to map or provider ordering.
func selectDevTarget(cmd *cobra.Command, root, requested string) (string, error) {
	names := []string(nil)
	if requested != "" {
		names = []string{requested}
	}
	_, targets, err := loadPackage(root, names)
	if err != nil {
		return "", fmt.Errorf("dev %s: %w", root, err)
	}
	if len(targets) == 0 {
		return "", fmt.Errorf("dev %s: no targets declared in targets.yaml", root)
	}
	if requested != "" || len(targets) == 1 {
		return targets[0].Name, nil
	}
	if !isCharDevice(cmd.InOrStdin()) || !isCharDevice(cmd.OutOrStdout()) {
		choices := make([]string, 0, len(targets))
		for _, candidate := range targets {
			choices = append(choices, fmt.Sprintf("%s (%s)", candidate.Name, candidate.Provider))
		}
		return "", fmt.Errorf("dev %s: multiple targets declared; pass --target <name>: %s", root, strings.Join(choices, ", "))
	}
	selected := targets[0].Name
	options := make([]huh.Option[string], 0, len(targets))
	for _, candidate := range targets {
		options = append(options, huh.NewOption(fmt.Sprintf("%s  ·  %s", candidate.Name, candidate.Provider), candidate.Name))
	}
	if err := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("Target to run").Options(options...).Value(&selected))).
		WithInput(cmd.InOrStdin()).WithOutput(cmd.OutOrStdout()).Run(); err != nil {
		return "", fmt.Errorf("dev %s: select target: %w", root, err)
	}
	return selected, nil
}

// compileTargetForDev dispatches the chosen instance. The bundled browser
// runner speaks Pipecat WebRTC today; other shipped targets fail with their
// specific next command instead of being silently replaced.
func compileTargetForDev(cmd *cobra.Command, root, targetName string) (string, error) {
	agent, targets, err := loadPackage(root, []string{targetName})
	if err != nil {
		return "", fmt.Errorf("dev %s: %w", root, err)
	}
	resolved := targets[0]
	if resolved.Provider != ir.ProviderPipecat {
		switch resolved.Provider {
		case ir.ProviderLiveKit:
			return "", fmt.Errorf("dev %s: target %q uses livekit; no LiveKit local browser runner is shipped—use `unmute compile %s --target %s`", root, resolved.Name, root, resolved.Name)
		case ir.ProviderElevenLabs:
			return "", fmt.Errorf("dev %s: target %q uses managed ElevenLabs; no local runner exists—use `unmute apply %s --target %s`", root, resolved.Name, root, resolved.Name)
		default:
			return "", fmt.Errorf("dev %s: target %q uses %s; its dev runner is not implemented", root, resolved.Name, resolved.Provider)
		}
	}
	artifact, err := generate.Generate(agent, resolved, target.Default())
	if err != nil {
		return "", fmt.Errorf("dev %s: %w", root, err)
	}
	for _, warning := range artifact.Notes.Warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
	}
	outDir := filepath.Join(root, "build", resolved.Name)
	if err := writeArtifactFiles(outDir, artifact.Files); err != nil {
		return "", fmt.Errorf("dev %s: %w", root, err)
	}
	return outDir, nil
}

// devChildEnv builds the bot subprocess environment: the ambient env plus any
// KEY=VALUE pairs from a .env at the package root. The bot also calls
// load_dotenv(), and its require_env() fails loudly if a value is missing.
func devChildEnv(root string, warn io.Writer) []string {
	env := os.Environ()
	vals, err := parseDotenv(filepath.Join(root, ".env"))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(warn, "warning: reading .env: %v\n", err)
		}
		return env
	}
	for name, value := range vals {
		if value != "" {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// parseDotenv reads a minimal KEY=VALUE dotenv file. It is not a full dotenv
// parser: no interpolation, just trims surrounding quotes and an `export` prefix.
func parseDotenv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = unquote(strings.TrimSpace(v))
	}
	return out, sc.Err()
}

// unquote strips one matched pair of surrounding quotes, leaving inner quotes
// intact (so a value like pa"ss is not mangled).
func unquote(v string) string {
	if len(v) >= 2 {
		if c := v[0]; (c == '"' || c == '\'') && v[len(v)-1] == c {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// waitPort blocks until addr accepts a connection, the bot exits, the context is
// cancelled, or the timeout elapses.
func waitPort(ctx context.Context, addr string, botDone <-chan error, timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-botDone:
			if err != nil {
				return fmt.Errorf("agent exited before ready: %w", err)
			}
			return errors.New("agent exited before ready")
		case <-deadline:
			return fmt.Errorf("agent not ready after %s", timeout)
		case <-ticker.C:
		}
	}
}

// stopBot asks the bot's process group to exit, then hard-kills it if it does
// not within the grace period. The negative pid targets the whole group, so
// uv's child python is reaped too.
func stopBot(c *exec.Cmd, done <-chan error) {
	if c.Process == nil {
		return
	}
	_ = syscall.Kill(-c.Process.Pid, syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
}

// killGroup is the deferred backstop: hard-kill the group on any return path.
func killGroup(c *exec.Cmd) {
	if c == nil || c.Process == nil {
		return
	}
	_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
}

func openBrowser(target string) {
	name, args := browserCommand(runtime.GOOS, target)
	_ = exec.Command(name, args...).Start()
}

func browserCommand(goos, target string) (string, []string) {
	if goos == "darwin" {
		return "open", []string{target}
	}
	return "xdg-open", []string{target} // POSIX-only command; no Windows branch
}
