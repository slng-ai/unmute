package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/slng-ai/unmute/internal/devmetrics"
	"github.com/slng-ai/unmute/internal/style"
	"github.com/slng-ai/unmute/internal/tui"
	"github.com/spf13/cobra"
)

func newDevCmd() *cobra.Command {
	var uiPort, botPort, targetName string
	var noOpen, verbose bool
	var vars []string

	cmd := &cobra.Command{
		Use:   "dev [agent-dir]",
		Short: "Compile, run the agent locally, and talk to it in the browser.",
		Long: "Compile, run the agent locally, and talk to it in the browser.\n\n" +
			"With no agent-dir, the package is the current directory, so you can " +
			"cd into an agent and run this with no arguments.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := packageDir(cmd, args)
			if err != nil {
				return err
			}

			// Input variables ride one JSON payload the generated runtimes read
			// when no dispatch supplied them; setting it in this process's
			// environment reaches every run path, container or not (I.dispatch).
			if len(vars) > 0 {
				agent, _, err := loadPackage(root, nil)
				if err != nil {
					return fmt.Errorf("dev %s: %w", root, err)
				}
				payload, err := callStartPayload(agent, vars)
				if err != nil {
					return fmt.Errorf("dev %s: %w", root, err)
				}
				if err := os.Setenv(CallStartEnv, payload); err != nil {
					return fmt.Errorf("dev %s: %w", root, err)
				}
			}

			selected, err := selectDevTarget(cmd, root, targetName)
			if err != nil {
				return err
			}
			// Default local mode: start the selected target's WebRTC runtime and
			// serve one web UI for both Pipecat and LiveKit.
			return runDevWeb(cmd, root, selected, uiPort, botPort, noOpen, verbose)
		},
	}

	cmd.Flags().StringVar(&uiPort, "port", "8765", "port for the local dev UI")
	cmd.Flags().StringVar(&botPort, "bot-port", "7860", "host port for the local agent runtime (with Compose, UNMUTE_DEV_PORT)")
	cmd.Flags().StringVar(&targetName, "target", "", "target instance name (required without a TTY when multiple exist)")
	cmd.Flags().StringArrayVar(&vars, "var", nil, "seed an input variable for this session: --var name=value (repeatable; the local stand-in for the dispatch payload)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open the browser automatically")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "follow container/agent logs on stderr (default: write to the log file only)")
	return cmd
}

func setChildEnv(env []string, name, value string) []string {
	prefix := name + "="
	filtered := env[:0]
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, prefix+value)
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
	accent := lipgloss.NewRenderer(w).NewStyle().Foreground(lipgloss.Color(style.Accent))
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
				fmt.Fprintf(w, "\r\033[2K %s %s", accent.Render(string(frames[i%len(frames)])), msg)
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

// isTTY reports whether a stream is a terminal. It takes `any` because callers
// hand it both writers (spinner, banner) and readers (prompt gating), and the
// question is about the file underneath either one.
func isTTY(value any) bool {
	file, ok := value.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
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
	if !isTTY(cmd.InOrStdin()) || !isTTY(cmd.OutOrStdout()) {
		choices := make([]string, 0, len(targets))
		for _, candidate := range targets {
			choices = append(choices, fmt.Sprintf("%s (%s)", candidate.Name, candidate.Provider))
		}
		return "", fmt.Errorf("dev %s: multiple targets declared; pass --target <name>: %s", root, strings.Join(choices, ", "))
	}
	options := make([]tui.Option, 0, len(targets))
	for _, candidate := range targets {
		options = append(options, tui.Option{
			Label: fmt.Sprintf("%s  ·  %s", candidate.Name, candidate.Provider),
			Value: candidate.Name,
		})
	}
	selected, err := tui.SelectOne(cmd.InOrStdin(), cmd.OutOrStdout(), "Target to run", options)
	if err != nil {
		return "", fmt.Errorf("dev %s: select target: %w", root, err)
	}
	return selected, nil
}

// packageEnv builds a child process's environment from the ambient env, the
// current directory's .env and .env.local, then the package-root pair. Later
// files win, so a package can override shared repository credentials.
//
// `dev` and `deploy` both use it, and that is the point: an author who put
// SLNG_API_KEY in an example's .env and ran `unmute dev` would be baffled to
// find `unmute deploy` cannot see the same line.
func packageEnv(root string, warn io.Writer) []string {
	env := os.Environ()
	files := make([]string, 0, 4)
	if cwd, err := os.Getwd(); err == nil {
		for _, name := range []string{".env", ".env.local"} {
			files = append(files, filepath.Join(cwd, name))
		}
		if absolute, err := filepath.Abs(root); err == nil {
			root = absolute
		}
	}
	for _, name := range []string{".env", ".env.local"} {
		file := filepath.Join(root, name)
		if !slices.Contains(files, file) {
			files = append(files, file)
		}
	}
	for _, file := range files {
		vals, err := parseDotenv(file)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				fmt.Fprintf(warn, "warning: reading %s: %v\n", file, err)
			}
			continue
		}
		for name, value := range vals {
			if value != "" {
				env = setChildEnv(env, name, value)
			}
		}
	}
	// The emitted agent's measurement producers are inert unless this is set. It
	// goes here rather than in each runner so telephony gets it too, and after
	// the dotenv files so the dev loop wins over a stale value on disk. The name
	// is owned by devmetrics because two Python producers read the same string.
	env = setChildEnv(env, devmetrics.Env, "1")
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

// stopBot asks a child's process group to exit, then hard-kills it if it does
// not within the grace period. The negative pid targets the whole group, so
// uv's child python is reaped too. Used by the telephony tunnel child.
func stopBot(c *exec.Cmd, done <-chan error) {
	if c.Process == nil {
		return
	}
	signalGroup(c, syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		signalGroup(c, syscall.SIGKILL)
	}
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
