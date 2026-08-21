package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
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

var cloudWebsocketAgentReady = waitForLocalAgentReady

// execDevCloudWebsocket runs the local phone path and blocks until the session
// ends. The number's previous voice configuration is restored on every exit path,
// interrupt included: a dev session that dies without restoring has left a real
// phone line pointing at a dead tunnel.
func execDevCloudWebsocket(cmd *cobra.Command, root, targetName string, plan *generate.TelephonyRuntimePlan, files []generate.File, opts devTelephonyOptions) (returnErr error) {
	printDevTelephonyPlan(cmd.OutOrStdout(), targetName, plan, nil)
	if opts.carrier {
		printCarrierDisclaimer(cmd.OutOrStdout(), targetName)
	} else {
		printLocalPlaneMode(cmd.OutOrStdout(), targetName, target.TelephonyLocalPlane(plan.LocalPlane))
	}
	if opts.report != nil {
		opts.report.Plane = target.TelephonyLocalPlane(plan.LocalPlane)
	}
	childEnv := devChildEnv(root, cmd.ErrOrStderr())
	// Everything the local run needs, by name. The carrier credentials belong to
	// carrier mode only: a deployed pure-inbound agent on this route needs none
	// of them, and pointing the number at this session is a request to the
	// carrier's API in the operator's name, so the CLI cannot do that without
	// them. A default run points nothing anywhere, so it asks for none.
	var required []string
	if opts.carrier {
		required = []string{plan.Environment["account_sid"], plan.Environment["auth_token"], plan.Environment["from_number"]}
	}
	required = append(required, externalTelephonyEnv(plan)...)
	required = trimEmptyAndDevSupplied(required)
	if missing := missingEnvironment(required, childEnv); len(missing) > 0 {
		return fmt.Errorf("missing telephony credentials/configuration: %s. This route hosts nothing in production, "+
			"but pointing your number at this local session is a request to your carrier in your name, so the CLI needs "+
			"them here; fill the package .env from build/%s/.env.example", strings.Join(missing, ", "), targetName)
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

	// A default run on this route is a local-plane run: the stand-in is the
	// carrier, over loopback, and it supplies what a carrier would so the run
	// needs no account of yours (SC-004).
	// This route publishes one port, the agent's, and it is the one whose
	// collision is silent: a second run reaches the first run's agent and
	// reports its answers as its own (T103).
	if err := hostPortCheck(telephonyHostPorts(plan, opts.botPort, childEnv)); err != nil {
		return err
	}
	var mediaPlane *mediaPlaneRun
	if !opts.carrier && planeIsMediaWebsocket(plan) {
		started, planeErr := startMediaPlaneRun(plan, opts.botPort, false)
		if planeErr != nil {
			return planeErr
		}
		mediaPlane = started
		// The plane holds a listener for the carrier's own API. Released here so
		// a run that ends any way at all leaves no port behind (gate P8).
		defer mediaPlane.stop()
		// This route's local runner answers the incoming call at "/", not at the
		// path the other routes on this plane use.
		mediaPlane.inboundPath = devCloudWebsocketWebhookPath
		childEnv = mediaPlane.apply(childEnv)
		if _, planeErr = mediaPlane.prepare(outDir); planeErr != nil {
			return planeErr
		}
	}

	// This route publishes no callback of ours, so a carrier run always needs a
	// tunnel: --public-url brings your own, otherwise one is managed. A default
	// run has no carrier and wants neither.
	var public *url.URL
	stopTunnel := func() {}
	if opts.carrier {
		origin, stop, originErr := carrierPublicOrigin(ctx, cmd.OutOrStdout(), targetName, plan, opts, processOut, true)
		if originErr != nil {
			return originErr
		}
		public, stopTunnel = origin, stop
	}
	defer stopTunnel()

	// The runner takes a bare host to advertise to the carrier's media stream
	// (research F12). A default run advertises the loopback address the local
	// media plane reaches the agent on, because there is nothing else to reach
	// it from.
	proxyHost := net.JoinHostPort("127.0.0.1", opts.botPort)
	if public != nil {
		proxyHost = public.Host
	}
	agent, err := startLocalCarrierAgent(ctx, outDir, opts.botPort, proxyHost, childEnv, processOut)
	if err != nil {
		return err
	}
	defer stopBot(agent.cmd, agent.done)
	spin := startSpinner(cmd.ErrOrStderr(), "waiting for the local Pipecat agent")
	if err := cloudWebsocketAgentReady(ctx, opts.botPort, agent.done); err != nil {
		spin.Stop()
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("local agent not ready: %w (see %s)", err, logPath)
	}
	spin.Stop()

	// Restore is deferred before the number is ever touched, so every exit path
	// after this point puts the number back: clean exit, an error below, or the
	// interrupt the context is watching (constitution V's borrowed-state rule).
	var restore func(context.Context) error
	defer func() {
		if restore == nil {
			return
		}
		// A fresh bounded context: the session's own is already cancelled by the
		// interrupt that got us here, and a cancelled context cannot restore it.
		restoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := restore(restoreCtx); err != nil {
			restoreErr := fmt.Errorf("restore the number's voice configuration: %w", err)
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", restoreErr)
			returnErr = errors.Join(returnErr, restoreErr)
		}
	}()
	if opts.carrier && !opts.noWebhook {
		number := envValue(childEnv, plan.Environment["from_number"])
		opts.report.carrierWrite("%s: point %s at this run", plan.Route.Carrier, number)
		restore, err = setDevCarrierWebhook(ctx, cmd.OutOrStdout(), targetName, plan, public, childEnv)
		if err != nil {
			return err
		}
		borrowed := restore
		restore = func(restoreCtx context.Context) error {
			opts.report.carrierWrite("%s: restore %s to what it pointed at before", plan.Route.Carrier, number)
			return borrowed(restoreCtx)
		}
	}
	printDevCloudWebsocketSession(cmd.OutOrStdout(), targetName, plan, public, childEnv, opts)

	// The plane's own call, which on this route is the only thing that reaches
	// the agent at all: nothing of ours is published, so without this the run
	// prints a port and waits for a caller that cannot exist.
	if mediaPlane != nil {
		printMediaPlaneReady(cmd.OutOrStdout(), targetName, mediaPlane)
		placeMediaPlaneCall(ctx, cmd.OutOrStdout(), cmd.ErrOrStderr(), targetName, logPath, mediaPlane)
	}

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

// localAgent is a compiled Pipecat agent running on this machine.
type localAgent struct {
	cmd  *exec.Cmd
	done chan error
}

// startLocalCarrierAgent runs the emitted bot in pipecat's local telephony mode.
// The proxy argument is a bare hostname by the runner's own contract (research
// F12), so the tunnel's host is passed rather than its URL.
func startLocalCarrierAgent(ctx context.Context, dir, port, proxyHost string, env []string, sink io.Writer) (*localAgent, error) {
	return startLocalPipecatAgent(ctx, dir, env, sink,
		"-t", "twilio", "-x", proxyHost, "--host", "0.0.0.0", "--port", port)
}

func startLocalPipecatAgent(ctx context.Context, dir string, env []string, sink io.Writer, args ...string) (*localAgent, error) {
	child := exec.CommandContext(ctx, "uv", append([]string{"run", "bot.py"}, args...)...)
	child.Dir = dir
	child.Env = env
	child.Stdout, child.Stderr = sink, sink
	ownProcessGroup(child) // own group, so uv's python is reaped too
	if err := child.Start(); err != nil {
		return nil, fmt.Errorf("start the local agent: %w", err)
	}
	agent := &localAgent{cmd: child, done: make(chan error, 1)}
	go func() {
		agent.done <- child.Wait()
		close(agent.done)
	}()
	return agent, nil
}

// waitForLocalAgentReady prevents the browser or a carrier webhook from being
// pointed at a process that has started but is not yet accepting sessions.
func waitForLocalAgentReady(ctx context.Context, port string, done <-chan error) error {
	readyCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: time.Second}
	endpoint := "http://" + net.JoinHostPort("127.0.0.1", port) + "/status"
	for {
		select {
		case err := <-done:
			if err == nil {
				return errors.New("local agent exited before it was ready")
			}
			return fmt.Errorf("local agent exited before it was ready: %w", err)
		case <-ticker.C:
			request, err := http.NewRequestWithContext(readyCtx, http.MethodGet, endpoint, nil)
			if err != nil {
				return err
			}
			response, err := client.Do(request)
			if err != nil {
				continue
			}
			var status struct {
				Status string `json:"status"`
			}
			decodeErr := json.NewDecoder(response.Body).Decode(&status)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK && decodeErr == nil && status.Status == "ready" {
				return nil
			}
		case <-readyCtx.Done():
			return readyCtx.Err()
		}
	}
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
	shown := previous.URL
	if shown == "" {
		shown = "unset"
	}
	fmt.Fprintf(out, "%s: Twilio voice webhook for %s set to %s (was: %s)\n", targetName, number, voiceURL, shown)
	return func(restoreCtx context.Context) error {
		if err := restoreTwilioVoiceWebhook(restoreCtx, accountSID, authToken, previous); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s: Twilio voice webhook for %s restored to %s\n", targetName, number, shown)
		return nil
	}, nil
}

// printDevCloudWebsocketSession states the session's facts once: what is running
// where, what was borrowed, and that it will be given back.
func printDevCloudWebsocketSession(out io.Writer, targetName string, plan *generate.TelephonyRuntimePlan, public *url.URL, env []string, opts devTelephonyOptions) {
	if public == nil {
		// A default run borrowed nothing and is reachable from nowhere, so the
		// only fact left to state is where the agent is listening.
		fmt.Fprintf(out, "%s: local agent on port %s, reachable on this machine only\n", targetName, opts.botPort)
		return
	}
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
		// Outbound on this route is one request the *operator* makes, with inline
		// markup naming the deployed service host: the emitted agent publishes no
		// endpoint to ask it from, so there is nothing here for the local plane to
		// poke. Placing it from here would test the wrong thing quietly.
		//
		// The local plane does place outbound calls, on the carrier-websocket
		// routes, where the agent does publish that endpoint (T068).
		fmt.Fprintf(out, "%s: --to is not placed locally on this route: an outbound call's markup names the deployed agent "+
			"and this route's agent publishes no endpoint to ask for one, so a local session has nothing to place. "+
			"Use the outbound command in the emitted README against the deployed agent, or a carrier-websocket "+
			"route to exercise outbound on the local plane\n", targetName)
	}
}

// devMintedEnvironment is the values `unmute dev` mints itself, rather than
// reading them from the generated Compose graph or from the host. Both are in
// the route's locally-supplied set — which is why neither appears in
// .env.example — but they are supplied by this command, so a host that exports
// one is overridden rather than refused.
var devMintedEnvironment = []string{"UNMUTE_PUBLIC_URL", "UNMUTE_OUTBOUND_TOKEN"}

// trimEmptyAndDevSupplied drops blank names and the values `unmute dev` supplies
// itself, so the missing-environment check never asks for something it is about
// to inject.
func trimEmptyAndDevSupplied(names []string) []string {
	var out []string
	for _, name := range names {
		if name == "" || slices.Contains(devMintedEnvironment, name) {
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
