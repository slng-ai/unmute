package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/slng-ai/unmute/internal/devmetrics"
	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/style"
	"github.com/slng-ai/unmute/internal/target"
	"github.com/spf13/cobra"
)

func newDevCmd() *cobra.Command {
	var uiPort, botPort, targetName, publicURL, to string
	var noOpen, verbose, console, telephony, carrier, noWebhook bool
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
			if !telephony && carrier {
				return errors.New("dev: --carrier requires --telephony")
			}
			// Both of these exist only to manage a public origin, and only a
			// carrier run has one: the default loop never leaves the machine.
			// So they name --carrier rather than --telephony, which is the mode
			// that can actually use the value.
			if !carrier && publicURL != "" {
				return errors.New("dev: --public-url requires --carrier (a local telephony run has no public origin)")
			}
			if !carrier && noWebhook {
				return errors.New("dev: --no-webhook requires --carrier (a local telephony run never touches your carrier's number)")
			}
			if !telephony && to != "" {
				return errors.New("dev: --to requires --telephony")
			}
			if to != "" {
				if err := validateDialTarget(to); err != nil {
					return err
				}
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
			if console {
				return errors.New("dev: --console was removed. " +
					"Run `unmute dev " + root + "` to talk to the agent in your browser")
			}
			if telephony {
				return runDevTelephony(cmd, root, selected, devTelephonyOptions{
					publicValue: publicURL, botPort: botPort, to: to,
					carrier: carrier, noWebhook: noWebhook, verbose: verbose,
				})
			}
			// Default local mode: start the selected target's WebRTC runtime and
			// serve one web UI for both Pipecat and LiveKit.
			return runDevWeb(cmd, root, selected, uiPort, botPort, noOpen, verbose)
		},
	}

	cmd.Flags().StringVar(&uiPort, "port", "8765", "port for the local dev UI")
	cmd.Flags().StringVar(&botPort, "bot-port", "7860", "host port for the local agent runtime (with Compose, UNMUTE_DEV_PORT or UNMUTE_TELEPHONY_PORT)")
	cmd.Flags().StringVar(&targetName, "target", "", "target instance name (required without a TTY when multiple exist)")
	cmd.Flags().StringArrayVar(&vars, "var", nil, "seed an input variable for this session: --var name=value (repeatable; the local stand-in for the dispatch payload)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open the browser automatically")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "follow container/agent logs on stderr (default: write to the log file only)")
	// Registered, hidden, and rejected on use. The flag is gone, but it is in
	// shell history and in older documentation, and cobra's bare "unknown flag"
	// would leave an author guessing whether they misremembered the name or the
	// mode itself went away.
	cmd.Flags().BoolVar(&console, "console", false, "removed: use the browser dev loop")
	_ = cmd.Flags().MarkHidden("console")
	cmd.Flags().BoolVar(&telephony, "telephony", false, "run the selected target's resolved telephony route (no browser UI)")
	cmd.Flags().BoolVar(&carrier, "carrier", false, "reach the route through your own carrier: managed tunnel, webhook rewrite, restore on exit (requires --telephony)")
	cmd.Flags().StringVar(&publicURL, "public-url", "", "exact public HTTPS origin for routes with carrier callbacks (requires --carrier)")
	cmd.Flags().StringVar(&to, "to", "", "E.164 number to dial for an outbound telephony test (requires --telephony and an outbound-capable target)")
	cmd.Flags().BoolVar(&noWebhook, "no-webhook", false, "do not touch the carrier number's webhook configuration (requires --carrier; point it at the printed public URL yourself)")
	return cmd
}

// runDevTelephony is the fail-closed gate: loading and generation reject
// every provisional or gated route before any tunnel, Docker, or carrier
// call (SPEC V5). The post-gate orchestration lives in execDevTelephony.
func runDevTelephony(cmd *cobra.Command, root, targetName string, opts devTelephonyOptions) error {
	agent, targets, err := loadPackage(root, []string{targetName})
	if err != nil {
		return fmt.Errorf("dev %s: %w", root, err)
	}
	resolved := targets[0]
	// The Daily carrier route has a plan, so it would otherwise fall through to the
	// generic "no executable telephony topology" line below, which is false: it has
	// a topology, it just is not one this command can run for you.
	if resolved.Provider == ir.ProviderPipecat && resolved.Transport == "daily-sip" && resolved.Carrier != "" {
		return fmt.Errorf("dev %s: target %q reaches its phone calls through your own carrier (%s) on the Pipecat Daily route, "+
			"and the piece that answers the carrier is the emitted telephony_helper.py, which you run yourself; "+
			"this command cannot run it for you because your carrier has to be able to reach it. "+
			"Run `unmute compile %s` and follow the Telephony setup section of the emitted README, which is the helper "+
			"beside a tunnel, two commands. To talk to this agent right now with no phone at all, "+
			"use `unmute dev %s` in the browser",
			root, resolved.Name, resolved.Carrier, root, root)
	}
	// The platform-terminated carrier route runs the phone path locally with one
	// command. It gets its own orchestration rather than the Compose graph below,
	// because production on this route hosts nothing: there is no compose file, no
	// helper, and no endpoint of ours, so the local session is the compiled agent,
	// a tunnel, and the number borrowed for the length of it.
	if devCloudWebsocketRoute(resolved) {
		plan := generate.TelephonyRuntimePlanFor(resolved)
		if plan == nil {
			return fmt.Errorf("dev %s: target %q has no resolved telephony route", root, resolved.Name)
		}
		if opts.to != "" && !planHasTelephonyFeature(plan, "outbound") {
			return fmt.Errorf("dev %s: --to needs an outbound-capable target; %q has no outbound direction (set channels.phone outbound: true)", root, resolved.Name)
		}
		artifact, err := generate.Generate(agent, resolved, target.Default())
		if err != nil {
			return fmt.Errorf("dev %s: %w", root, err)
		}
		if err := execDevCloudWebsocket(cmd, root, resolved.Name, artifact.Telephony, artifact.Files, opts); err != nil {
			return fmt.Errorf("dev %s: %w", root, err)
		}
		return nil
	}
	plan := generate.TelephonyRuntimePlanFor(resolved)
	if plan == nil {
		return fmt.Errorf("dev %s: target %q has no resolved telephony route", root, resolved.Name)
	}
	// --to only makes sense for an outbound-capable target; reject before
	// generate or any child process (SPEC V3, V6).
	if opts.to != "" && !planHasTelephonyFeature(plan, "outbound") {
		return fmt.Errorf("dev %s: --to needs an outbound-capable target; %q has no outbound direction (set channels.phone outbound: true)", root, resolved.Name)
	}
	artifact, err := generate.Generate(agent, resolved, target.Default())
	if err != nil {
		return fmt.Errorf("dev %s: %w", root, err)
	}
	if artifact.Telephony == nil || len(artifact.Telephony.Services) == 0 {
		return fmt.Errorf("dev %s: target %q has no executable telephony topology", root, resolved.Name)
	}
	if err := execDevTelephony(cmd, root, resolved.Name, artifact.Telephony, artifact.Files, opts); err != nil {
		return fmt.Errorf("dev %s: %w", root, err)
	}
	return nil
}

func parseTelephonyPublicURL(value string) (*url.URL, error) {
	if value == "" {
		return nil, errors.New("--public-url must be an HTTPS origin with an optional path, got an empty value")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("--public-url must be an HTTPS origin with an optional path, got %q", value)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return parsed, nil
}

func printDevTelephonyPlan(out io.Writer, name string, plan *generate.TelephonyRuntimePlan, public *url.URL) {
	fmt.Fprintf(out, "%s: telephony route provider=%s transport=%s carrier=%s coordination=%s\n", name, plan.Route.Provider, plan.Route.Transport, plan.Route.Carrier, plan.Coordination)
	printDevTelephonyEndpoints(out, name, plan, public)
	for _, step := range plan.ManualSteps {
		fmt.Fprintf(out, "%s: setup: %s\n", name, step)
	}
	fmt.Fprintf(out, "%s: local services: %s\n", name, strings.Join(plan.Services, ", "))
	for _, reason := range plan.Reasons {
		fmt.Fprintf(out, "%s: coordination reason %s -> %s\n", name, reason.Name, strings.Join(reason.Consumers, ", "))
	}
}

// printDevTelephonyEndpoints prints the exact public callback URLs once the
// origin is known (TELEPHONY.md step 6); a nil public prints nothing.
func printDevTelephonyEndpoints(out io.Writer, name string, plan *generate.TelephonyRuntimePlan, public *url.URL) {
	if public == nil {
		return
	}
	for _, endpoint := range plan.PublicEndpoints {
		base := strings.TrimSuffix(public.String(), "/")
		if endpoint.Method == "WS" {
			base = "wss" + strings.TrimPrefix(base, "https")
		}
		fmt.Fprintf(out, "%s: %s %s %s%s\n", name, endpoint.Name, endpoint.Method, base, endpoint.Path)
	}
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

func missingEnvironment(names, env []string) []string {
	values := make(map[string]bool, len(env))
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if ok && value != "" {
			values[name] = true
		}
	}
	var missing []string
	for _, name := range names {
		if !values[name] {
			missing = append(missing, name)
		}
	}
	return missing
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

// devChildEnv builds the bot subprocess environment from the ambient env, the
// current directory's .env and .env.local, then the package-root pair. Later
// files win, so a package can override shared repository credentials.
func devChildEnv(root string, warn io.Writer) []string {
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
