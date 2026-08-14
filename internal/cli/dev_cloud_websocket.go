package cli

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/spf13/cobra"
)

// The local phone path for the (pipecat, cloud-websocket) route.
//
// Production on this route hosts nothing, so there is no Compose graph and no
// helper to run: the whole flow is the compiled agent on this machine, a tunnel
// so the carrier can reach it, and the number pointed at that tunnel for the
// length of the session.
//
// The agent runs in pipecat's own local telephony mode (`bot.py -t twilio -x
// <host>`), which answers the carrier's webhook itself with markup naming its own
// `/ws`. Verified against the installed pipecat-ai 1.5.0 on 2026-08-13 (research
// F12): the flags are `-t/--transport` and `-x/--proxy`, the proxy is a hostname
// with no scheme, and the webhook it answers is `POST /`. So no TwiML Bin is
// created locally and the production Bin is never touched.

// devCloudWebsocketWebhookPath is where the local runner answers the carrier.
// Not a route endpoint: this route has none, which is why it is a constant here
// rather than a field on the plan.
const devCloudWebsocketWebhookPath = "/"

// execDevCloudWebsocket runs the local phone path and blocks until the session
// ends. The number's previous voice configuration is restored on every exit path,
// interrupt included: a dev session that dies without restoring has left a real
// phone line pointing at a dead tunnel.
func execDevCloudWebsocket(cmd *cobra.Command, root, targetName string, plan *generate.TelephonyRuntimePlan, files []generate.File, opts devTelephonyOptions) error {
	printDevTelephonyPlan(cmd.OutOrStdout(), targetName, plan, nil)
	childEnv := devChildEnv(root, cmd.ErrOrStderr())
	// Everything the local run needs, by name. The carrier credentials are needed
	// here even though a deployed pure-inbound agent needs none: pointing the
	// number at this session is a request to the carrier's API in the operator's
	// name, so the CLI cannot do it without them.
	required := []string{plan.Environment["account_sid"], plan.Environment["auth_token"], plan.Environment["from_number"]}
	required = append(required, externalTelephonyEnv(plan)...)
	required = trimEmptyAndDevSupplied(required)
	if missing := missingEnvironment(required, childEnv); len(missing) > 0 {
		return fmt.Errorf("missing telephony credentials/configuration: %s. This route hosts nothing in production, "+
			"but pointing your number at this local session is a request to your carrier in your name, so the CLI needs "+
			"them here; see TELEPHONY.md#credentials for where to obtain them", strings.Join(missing, ", "))
	}
	if _, err := exec.LookPath("uv"); err != nil {
		return fmt.Errorf("uv not found on PATH; the local phone path runs the compiled agent with uv (https://docs.astral.sh/uv/)")
	}
	outDir := filepath.Join(root, "build", targetName)
	if err := writeArtifactFiles(cmd.ErrOrStderr(), outDir, files); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "compiled %s\n", outDir)

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logPath := filepath.Join(outDir, "telephony.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	var processOut io.Writer = logFile
	if opts.verbose {
		processOut = io.MultiWriter(logFile, cmd.ErrOrStderr())
	}

	// --public-url brings your own tunnel and skips all tunnel management, exactly
	// as on every other route.
	var public *url.URL
	switch {
	case opts.publicValue != "":
		public, err = parseTelephonyPublicURL(opts.publicValue)
		if err != nil {
			return err
		}
	default:
		tunnel, err := startQuickTunnel(ctx, opts.botPort, processOut)
		if err != nil {
			return err
		}
		defer tunnel.Stop()
		public, err = parseTelephonyPublicURL(tunnel.URL)
		if err != nil {
			return fmt.Errorf("managed tunnel returned an unusable origin: %w", err)
		}
	}

	agent, err := startLocalCarrierAgent(ctx, outDir, opts.botPort, public.Host, childEnv, processOut)
	if err != nil {
		return err
	}
	defer stopBot(agent.cmd, agent.done)

	// Restore is deferred before the number is ever touched, so every exit path
	// after this point puts the number back: clean exit, an error below, or the
	// interrupt the context is watching (constitution V's borrowed-state rule).
	var restore func(context.Context) error
	defer func() {
		if restore == nil {
			return
		}
		// A fresh context: the session's own is already cancelled by the interrupt
		// that got us here, and a cancelled context cannot make an HTTPS request.
		if err := restore(context.WithoutCancel(cmd.Context())); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not restore the number's voice configuration: %v\n", err)
		}
	}()
	if !opts.noWebhook {
		restore, err = setDevCarrierWebhook(ctx, cmd.OutOrStdout(), targetName, plan, public, childEnv)
		if err != nil {
			return err
		}
	}
	printDevCloudWebsocketSession(cmd.OutOrStdout(), targetName, plan, public, childEnv, opts)

	// Nothing left to orchestrate: wait for the agent to exit or for the session to
	// be interrupted. The deferred stops and the restore run either way.
	select {
	case err := <-agent.done:
		if err != nil && ctx.Err() == nil {
			return fmt.Errorf("local agent exited: %w (see %s)", err, logPath)
		}
	case <-ctx.Done():
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: session ended\n", targetName)
	return nil
}

// localAgent is the compiled agent running on this machine in the carrier's
// stream mode.
type localAgent struct {
	cmd  *exec.Cmd
	done chan error
}

// startLocalCarrierAgent runs the emitted bot in pipecat's local telephony mode.
// The proxy argument is a bare hostname by the runner's own contract (research
// F12), so the tunnel's host is passed rather than its URL.
func startLocalCarrierAgent(ctx context.Context, dir, port, proxyHost string, env []string, sink io.Writer) (*localAgent, error) {
	child := exec.CommandContext(ctx, "uv", "run", "bot.py",
		"-t", "twilio", "-x", proxyHost, "--host", "0.0.0.0", "--port", port)
	child.Dir = dir
	child.Env = env
	child.Stdout, child.Stderr = sink, sink
	ownProcessGroup(child) // own group, so uv's python is reaped too
	if err := child.Start(); err != nil {
		return nil, fmt.Errorf("start the local agent: %w", err)
	}
	agent := &localAgent{cmd: child, done: make(chan error, 1)}
	go func() { agent.done <- child.Wait() }()
	return agent, nil
}

// setDevCarrierWebhook points the declared number at this session and returns the
// undo.
//
// It calls the carrier implementation directly rather than going through
// autoConfigureCarrierWebhook, because that function derives the URL from a route
// endpoint and this route deliberately has none: the path belongs to pipecat's
// local runner, not to anything this repository emits.
func setDevCarrierWebhook(ctx context.Context, out io.Writer, targetName string, plan *generate.TelephonyRuntimePlan, public *url.URL, env []string) (func(context.Context) error, error) {
	if plan.Route.Carrier != "twilio" {
		return nil, fmt.Errorf("carrier %q has no local webhook implementation", plan.Route.Carrier)
	}
	voiceURL := strings.TrimSuffix(public.String(), "/") + devCloudWebsocketWebhookPath
	accountSID := envValue(env, plan.Environment["account_sid"])
	authToken := envValue(env, plan.Environment["auth_token"])
	number := envValue(env, plan.Environment["from_number"])
	previous, err := configureTwilioVoiceWebhook(ctx, accountSID, authToken, number, voiceURL)
	if err != nil {
		return nil, fmt.Errorf("configure Twilio voice webhook: %w", err)
	}
	shown := previous
	if shown == "" {
		shown = "unset"
	}
	fmt.Fprintf(out, "%s: Twilio voice webhook for %s set to %s (was: %s)\n", targetName, number, voiceURL, shown)
	return func(restoreCtx context.Context) error {
		if _, err := configureTwilioVoiceWebhook(restoreCtx, accountSID, authToken, number, previous); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s: Twilio voice webhook for %s restored to %s\n", targetName, number, shown)
		return nil
	}, nil
}

// printDevCloudWebsocketSession states the session's facts once: what is running
// where, what was borrowed, and that it will be given back.
func printDevCloudWebsocketSession(out io.Writer, targetName string, plan *generate.TelephonyRuntimePlan, public *url.URL, env []string, opts devTelephonyOptions) {
	fmt.Fprintf(out, "%s: local agent on port %s, reachable at %s (quick tunnel URLs rotate on every run)\n",
		targetName, opts.botPort, strings.TrimSuffix(public.String(), "/"))
	number := envValue(env, plan.Environment["from_number"])
	if opts.noWebhook {
		fmt.Fprintf(out, "%s: --no-webhook, carrier number left untouched; point it at %s%s yourself\n",
			targetName, strings.TrimSuffix(public.String(), "/"), devCloudWebsocketWebhookPath)
	} else if number != "" {
		fmt.Fprintf(out, "%s: borrowed %s for this session; its previous voice configuration is restored on exit, interrupt included\n",
			targetName, number)
	}
	fmt.Fprintf(out, "%s: your deployed TwiML Bin is untouched; nothing was created at your carrier\n", targetName)
	if number != "" && planHasTelephonyFeature(plan, "inbound") {
		fmt.Fprintf(out, "\n  \033[1;32m▸\033[0m call %s  ·  ctrl-c to stop\n\n", number)
	}
	if opts.to != "" {
		// Outbound on this route is one request to the carrier with inline markup
		// naming the platform, so it reaches the *deployed* agent, not this local
		// one. Placing it from here would test the wrong thing quietly.
		fmt.Fprintf(out, "%s: --to is not placed locally on this route: an outbound call's markup names the deployed agent, "+
			"so a local session cannot answer it. Use the outbound command in the emitted README against the deployed agent\n", targetName)
	}
}

// trimEmptyAndDevSupplied drops blank names and the values `unmute dev` supplies
// itself, so the missing-environment check never asks for something it is about
// to inject.
func trimEmptyAndDevSupplied(names []string) []string {
	devSupplied := map[string]bool{"UNMUTE_PUBLIC_URL": true, "UNMUTE_OUTBOUND_TOKEN": true}
	var out []string
	for _, name := range names {
		if name == "" || devSupplied[name] {
			continue
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// devCloudWebsocketRoute reports whether a resolved target takes the local flow
// above. Named once so the dev command's dispatch and its tests agree.
func devCloudWebsocketRoute(resolved ir.Target) bool {
	return resolved.Provider == ir.ProviderPipecat && resolved.Transport == "cloud-websocket"
}
