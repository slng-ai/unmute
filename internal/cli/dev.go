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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/slng-ai/unmute/internal/devmetrics"
	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/style"
	"github.com/slng-ai/unmute/internal/tui"
	"github.com/spf13/cobra"
)

func newDevCmd() *cobra.Command {
	var uiPort, botPort, targetName, regionName string
	var noOpen, verbose bool
	var vars, sources []string

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

			// The facts a carrier would supply ride their own payload, read by the
			// emitted pre-fetch only where nothing else filled them. This is what
			// makes the whole identification path reproducible with no phone: the
			// value goes through the pre-fetch, so it is marked as awaiting
			// confirmation and read back exactly as a real one is.
			if len(sources) > 0 {
				payload, err := callFactsPayload(sources)
				if err != nil {
					return fmt.Errorf("dev %s: %w", root, err)
				}
				if err := os.Setenv(generate.LocalCallFactsEnv, payload); err != nil {
					return fmt.Errorf("dev %s: %w", root, err)
				}
			}

			agent, selected, err := selectDevTarget(cmd, root, targetName, regionName)
			if err != nil {
				return err
			}
			// Default local mode: start the selected target's WebRTC runtime and
			// serve one web UI for both Pipecat and LiveKit.
			return runDevWeb(cmd, root, agent, selected, uiPort, botPort, noOpen, verbose)
		},
	}

	cmd.Flags().StringVar(&uiPort, "port", "8765", "port for the local dev UI")
	cmd.Flags().StringVar(&botPort, "bot-port", "7860", "host port for the local agent runtime (with Compose, UNMUTE_DEV_PORT)")
	cmd.Flags().StringVar(&targetName, "target", "", "target instance name (required without a TTY when multiple exist)")
	cmd.Flags().StringVar(&regionName, "region", "", "deployment region to run, for a target that names several (required without a TTY)")
	cmd.Flags().StringArrayVar(&vars, "var", nil, "seed an input variable for this session: --var name=value (repeatable; the local stand-in for the dispatch payload)")
	cmd.Flags().StringArrayVar(&sources, "source", nil, "seed a fact the call carries: --source from_number=+34600111222 (repeatable; the local stand-in for a caller ID, read by prefetch)")
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
	u := style.For(w)
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
				fmt.Fprintf(w, "\r\033[2K %s %s", u.Accent(string(frames[i%len(frames)])), msg)
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

// selectDevTarget picks the one build `dev` will run. A single build needs no
// prompt, and several never fall back to map or provider ordering.
//
// A target that names
// several regions is several builds, each with its own models, so the region is
// part of the choice and not a detail of it.
//
// It returns the target rather than its name because a name no longer
// identifies one build.
func selectDevTarget(cmd *cobra.Command, root, requested, region string) (*ir.Agent, ir.Target, error) {
	names := []string(nil)
	if requested != "" {
		names = []string{requested}
	}
	agent, targets, err := loadPackage(root, names)
	if err != nil {
		return nil, ir.Target{}, fmt.Errorf("dev %s: %w", root, err)
	}
	if region != "" {
		kept := make([]ir.Target, 0, len(targets))
		for _, candidate := range targets {
			if candidate.Region == region || (candidate.Region == "" && slices.Contains(candidate.DeploymentRegions, region)) {
				kept = append(kept, candidate)
			}
		}
		if len(kept) == 0 {
			return nil, ir.Target{}, fmt.Errorf("dev %s: no target deploys to region %q; this package declares %s",
				root, region, devRegionChoices(targets))
		}
		targets = kept
	}
	if len(targets) == 0 {
		return nil, ir.Target{}, fmt.Errorf("dev %s: no targets declared in targets.yaml", root)
	}
	if len(targets) == 1 {
		return agent, targets[0], nil
	}
	if !isTTY(cmd.InOrStdin()) || !isTTY(cmd.OutOrStdout()) {
		return nil, ir.Target{}, fmt.Errorf("dev %s: several builds to choose from; pass --target <name> or --region <region>: %s",
			root, devRegionChoices(targets))
	}
	options := make([]tui.Option, 0, len(targets))
	for _, candidate := range targets {
		options = append(options, tui.Option{
			Label: fmt.Sprintf("%s  ·  %s", candidate.Label(), candidate.Provider),
			Value: strconv.Itoa(len(options)),
		})
	}
	selected, err := tui.SelectOne(cmd.InOrStdin(), cmd.OutOrStdout(), "Build to run", options)
	if err != nil {
		return nil, ir.Target{}, fmt.Errorf("dev %s: select target: %w", root, err)
	}
	index, err := strconv.Atoi(selected)
	if err != nil || index < 0 || index >= len(targets) {
		return nil, ir.Target{}, fmt.Errorf("dev %s: select target: %q is not one of the offered builds", root, selected)
	}
	return agent, targets[index], nil
}

// devRegionChoices names every build on offer, the way the picker labels them.
func devRegionChoices(targets []ir.Target) string {
	choices := make([]string, 0, len(targets))
	for _, candidate := range targets {
		choices = append(choices, fmt.Sprintf("%s (%s)", candidate.Label(), candidate.Provider))
	}
	return strings.Join(choices, ", ")
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
				warnf(warn, "reading %s: %v\n", file, err)
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
	// Same placement, same reason: telephony dev runs need it too, and the dev
	// loop must win over a stale value on disk. This is what makes a local run
	// show up in Coval as `<name>-local` while the same build deployed shows up
	// as `<name>`.
	env = setChildEnv(env, generate.LocalRunEnv, "1")
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
