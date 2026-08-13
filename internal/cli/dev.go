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
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/slng/unmute/internal/generate"
	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/style"
	"github.com/slng/unmute/internal/target"
	"github.com/spf13/cobra"
)

func newDevCmd() *cobra.Command {
	var uiPort, botPort, targetName, publicURL, to string
	var noOpen, verbose, console, telephony, noWebhook bool
	var vars []string

	cmd := &cobra.Command{
		Use:   "dev <agent-dir>",
		Short: "Compile, run the agent locally, and talk to it in the browser or terminal (--console).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := args[0]
			if !telephony && publicURL != "" {
				return errors.New("dev: --public-url requires --telephony")
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
				if telephony {
					return errors.New("dev: --console and --telephony cannot be used together")
				}
				return runDevConsole(cmd, root, selected)
			}
			if telephony {
				return runDevTelephony(cmd, root, selected, publicURL, botPort, to, noWebhook, verbose)
			}
			// Default local mode: build and run the deployable container, served
			// through one WebRTC web UI for both pipecat and livekit (SPEC V1).
			return runDevWeb(cmd, root, selected, uiPort, botPort, noOpen, verbose)
		},
	}

	cmd.Flags().StringVar(&uiPort, "port", "8765", "port for the local dev UI")
	cmd.Flags().StringVar(&botPort, "bot-port", "7860", "host port the container publishes the agent on (Compose UNMUTE_DEV_PORT; with --telephony, UNMUTE_TELEPHONY_PORT)")
	cmd.Flags().StringVar(&targetName, "target", "", "target instance name (required without a TTY when multiple exist)")
	cmd.Flags().StringArrayVar(&vars, "var", nil, "seed an input variable for this session: --var name=value (repeatable; the local stand-in for the dispatch payload)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open the browser automatically")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "follow container/agent logs on stderr (default: write to the log file only)")
	cmd.Flags().BoolVar(&console, "console", false, "talk to the agent in the terminal over the local mic/speaker (no browser or dev server; --port/--bot-port/--no-open are ignored)")
	cmd.Flags().BoolVar(&telephony, "telephony", false, "run the selected target's resolved telephony route (no browser UI)")
	cmd.Flags().StringVar(&publicURL, "public-url", "", "exact public HTTPS origin for routes with carrier callbacks (requires --telephony)")
	cmd.Flags().StringVar(&to, "to", "", "E.164 number to dial for an outbound telephony test (requires --telephony and an outbound-capable target)")
	cmd.Flags().BoolVar(&noWebhook, "no-webhook", false, "do not touch the carrier number's webhook configuration (requires --telephony; point it at the printed public URL yourself)")
	return cmd
}

// runDevTelephony is the fail-closed gate: loading and generation reject
// every provisional or gated route before any tunnel, Docker, or carrier
// call (SPEC V5). The post-gate orchestration lives in execDevTelephony.
func runDevTelephony(cmd *cobra.Command, root, targetName, publicValue, botPort, to string, noWebhook, verbose bool) error {
	agent, targets, err := loadPackage(root, []string{targetName})
	if err != nil {
		return fmt.Errorf("dev %s: %w", root, err)
	}
	resolved := targets[0]
	// Both Daily forms refuse, and the two refusals say different true things.
	// The carrier form has a plan, so it would otherwise fall through to the
	// generic "no executable telephony topology" line below, which is false: it has
	// a topology, it just is not one this command can run for you.
	if resolved.Provider == ir.ProviderPipecat && resolved.Transport == "daily-sip" && resolved.Carrier != "" {
		return fmt.Errorf("dev %s: target %q reaches its phone calls through your own carrier (%s) on the Pipecat Daily route, "+
			"and the piece that answers the carrier is the emitted telephony_helper.py, which you run yourself; "+
			"this command cannot run it for you because your carrier has to be able to reach it. "+
			"Run `unmute compile %s` and follow the Telephony setup section of the emitted README, which is the helper "+
			"beside a tunnel, two commands. To talk to this agent right now with no phone at all, "+
			"use `unmute dev %s` in the browser or `unmute dev --console %s` in the terminal",
			root, resolved.Name, resolved.Carrier, root, root, root)
	}
	plan := generate.TelephonyRuntimePlanFor(resolved)
	if plan == nil {
		// The Daily route is telephony, so the generic message would be false. It
		// simply has nothing to run locally: Daily's own infrastructure carries the
		// call to a deployed agent. Name the route, name the two modes that do work
		// on it right now, and point at how a real phone call happens (FR-028).
		if resolved.Transport == "daily-sip" {
			return fmt.Errorf("dev %s: target %q runs on the Pipecat Daily route (transport daily-sip), "+
				"where Daily carries the phone call to a deployed agent, so there is no local telephony "+
				"topology to run; talk to this agent now with `unmute dev %s` in the browser or "+
				"`unmute dev --console %s` in the terminal, and for a real phone call run "+
				"`unmute compile %s` and follow the Deploy and Phone calls sections of the emitted README",
				root, resolved.Name, root, root, root)
		}
		return fmt.Errorf("dev %s: target %q has no resolved telephony route", root, resolved.Name)
	}
	// --to only makes sense for an outbound-capable target; reject before
	// generate or any child process (SPEC V3, V6).
	if to != "" && !planHasTelephonyFeature(plan, "outbound") {
		return fmt.Errorf("dev %s: --to needs an outbound-capable target; %q has no outbound direction (set channels.phone outbound: true)", root, resolved.Name)
	}
	artifact, err := generate.Generate(agent, resolved, target.Default())
	if err != nil {
		return fmt.Errorf("dev %s: %w", root, err)
	}
	if artifact.Telephony == nil || len(artifact.Telephony.Services) == 0 {
		return fmt.Errorf("dev %s: target %q has no executable telephony topology", root, resolved.Name)
	}
	opts := devTelephonyOptions{publicValue: publicValue, botPort: botPort, to: to, noWebhook: noWebhook, verbose: verbose}
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

// runDevConsole compiles the selected target and hands the terminal to its
// native console mode: pipecat's LocalAudioTransport (via the `console` extra) or
// livekit's `agent.py console`. Talk over the local mic/speaker — no dev
// server, no browser, no log file (C6). livekit console needs LiveKit creds
// only when a binding routes through Inference (C2/C7); the artifact carries
// that fact, so this never re-derives it.
func runDevConsole(cmd *cobra.Command, root, targetName string) error {
	agent, targets, err := loadPackage(root, []string{targetName})
	if err != nil {
		return fmt.Errorf("dev %s: %w", root, err)
	}
	resolved := targets[0]
	uvArgs, err := consolePlan(resolved.Provider)
	if err != nil {
		return fmt.Errorf("dev %s: target %q %w", root, resolved.Name, err)
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
	fmt.Fprintf(cmd.OutOrStdout(), "compiled %s\n", outDir)

	if len(artifact.LiveKitInference) > 0 {
		if err := requireInferenceCreds(root, artifact.LiveKitInference); err != nil {
			return fmt.Errorf("dev %s: %w", root, err)
		}
	}
	if _, err := exec.LookPath("uv"); err != nil {
		return fmt.Errorf("dev %s: uv not found on PATH; install uv to run the agent locally (https://docs.astral.sh/uv/)", root)
	}
	return execConsole(outDir, uvArgs, devChildEnv(root, cmd.ErrOrStderr()))
}

// consolePlan is the `uv` invocation for a target's console mode. Pure and
// exhaustive so the dispatch is unit-tested without running uv (V7). Managed
// and unimplemented targets refuse with their next command.
func consolePlan(p ir.Provider) ([]string, error) {
	switch p {
	case ir.ProviderPipecat:
		// The `console` extra installs pyaudio (LocalAudioTransport); the
		// `console` argv selects console_main() in bot.py (T1).
		return []string{"run", "--extra", "console", "bot.py", "console"}, nil
	case ir.ProviderLiveKit:
		return []string{"run", "agent.py", "console"}, nil
	default:
		return nil, fmt.Errorf("uses %s; console mode is not implemented", p)
	}
}

// requireInferenceCreds fails before launching the console TUI when a binding
// routes through LiveKit Inference but the LiveKit creds are absent (C2/C7):
// console never connects to a room, but Inference is billed through LiveKit
// Cloud. A scaffold-default agent (native providers + local turn) hits neither
// key, so this never fires for it.
func requireInferenceCreds(root string, uses []string) error {
	env := devChildEnv(root, io.Discard)
	has := func(key string) bool {
		for _, kv := range env {
			if strings.HasPrefix(kv, key+"=") && len(kv) > len(key)+1 {
				return true
			}
		}
		return false
	}
	var missing []string
	for _, key := range []string{"LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"} {
		if !has(key) {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("console mode needs %s: this agent routes through LiveKit Inference (%s); set them in .env, or bind native providers + local turn to run console with no LiveKit credentials",
		strings.Join(missing, " and "), strings.Join(uses, ", "))
}

// execConsole hands the terminal to the console child and blocks until it
// exits. Real stdio is inherited so the child owns the TTY (livekit draws a
// full-screen UI); the child stays in our process group so terminal Ctrl-C
// reaches it directly, and we suppress SIGINT in the parent so we wait for the
// child's graceful shutdown instead of dying first (C6).
func execConsole(dir string, uvArgs, env []string) error {
	child := exec.Command("uv", uvArgs...)
	child.Dir = dir
	child.Env = env
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	if err := child.Run(); err != nil {
		return fmt.Errorf("console: %w", err)
	}
	return nil
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

// devChildEnv builds the bot subprocess environment from the ambient env, the
// current directory's .env, then the package-root .env. Later files win, so a
// package can override shared repository credentials.
func devChildEnv(root string, warn io.Writer) []string {
	env := os.Environ()
	packageEnv := filepath.Join(root, ".env")
	files := []string{}
	if cwd, err := os.Getwd(); err == nil {
		files = append(files, filepath.Join(cwd, ".env"))
		if absolute, err := filepath.Abs(packageEnv); err == nil {
			packageEnv = absolute
		}
	}
	if len(files) == 0 || files[0] != packageEnv {
		files = append(files, packageEnv)
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
	_ = syscall.Kill(-c.Process.Pid, syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
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
