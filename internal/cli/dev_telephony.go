package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
	"github.com/spf13/cobra"
)

// randomOutboundToken mints the dev-only shared secret the CLI and the
// container's dial-out endpoint use to authenticate a local outbound trigger.
func randomOutboundToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// devTelephonyOptions carries the dev command flags into the post-gate core.
type devTelephonyOptions struct {
	publicValue string
	botPort     string
	to          string // --to: E.164 to dial once the outbound-capable graph is healthy
	noWebhook   bool   // --no-webhook: leave the carrier's number configuration alone
	verbose     bool
}

// e164Pattern matches an E.164 number, the same shape the generated
// _valid_destination accepts (leading +, no leading zero, 8-15 digits).
var e164Pattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

// validateDialTarget rejects a --to value that is not a bare E.164 number.
func validateDialTarget(number string) error {
	if !e164Pattern.MatchString(number) {
		return fmt.Errorf("dev: --to must be an E.164 number like +15551234567, got %q", number)
	}
	return nil
}

// execDevTelephony runs everything after the fail-closed gate: env checks,
// the managed tunnel, the Compose graph (infrastructure first for routes
// whose trunk records the dev command creates itself), carrier webhook
// configuration, the call line, log streaming, and teardown. It never
// validates routes; runDevTelephony's gate already did (SPEC V5).
func execDevTelephony(cmd *cobra.Command, root, targetName string, plan *generate.TelephonyRuntimePlan, files []generate.File, opts devTelephonyOptions) error {
	// TELEPHONY.md step 2: the provider-neutral plan prints before env
	// validation; the endpoint URLs print at step 6, once the origin exists.
	printDevTelephonyPlan(cmd.OutOrStdout(), targetName, plan, nil)
	childEnv := devChildEnv(root, cmd.ErrOrStderr())
	if err := rejectLocalTopologyConflicts(plan, childEnv); err != nil {
		return err
	}
	childEnv = setChildEnv(childEnv, "UNMUTE_TELEPHONY_PORT", opts.botPort)
	required := externalTelephonyEnv(plan)
	// UNMUTE_PUBLIC_URL is dev-supplied here: --public-url or the managed
	// tunnel injects it after this check, so it is not demanded from .env.
	if opts.publicValue != "" || len(plan.PublicEndpoints) > 0 {
		required = slices.DeleteFunc(required, func(name string) bool { return name == "UNMUTE_PUBLIC_URL" })
	}
	// UNMUTE_OUTBOUND_TOKEN is a dev-supplied secret for the HTTP dial-out routes
	// (Pipecat carrier-websocket and the LiveKit connector): the CLI mints it,
	// injects it so the container's dial-out endpoint can authenticate the
	// trigger and pass readiness, and reuses it to place the call. It is never
	// demanded from .env and never printed. Mint it exactly when the route's
	// runtime lists it (SPEC V5): LiveKit SIP dials out by agent dispatch and
	// needs no token, so it must not receive a dead injection.
	outboundToken := ""
	if slices.Contains(plan.RequiredEnv, "UNMUTE_OUTBOUND_TOKEN") {
		token, err := randomOutboundToken()
		if err != nil {
			return fmt.Errorf("mint outbound token: %w", err)
		}
		outboundToken = token
		childEnv = setChildEnv(childEnv, "UNMUTE_OUTBOUND_TOKEN", outboundToken)
		required = slices.DeleteFunc(required, func(name string) bool { return name == "UNMUTE_OUTBOUND_TOKEN" })
	}
	if missing := missingEnvironment(required, childEnv); len(missing) > 0 {
		return fmt.Errorf("missing telephony credentials/configuration: %s; see TELEPHONY.md#credentials for where to obtain them", strings.Join(missing, ", "))
	}
	if err := composePreflight(cmd.Context(), childEnv); err != nil {
		return err
	}
	outDir := filepath.Join(root, "build", targetName)
	if err := writeArtifactFiles(cmd.ErrOrStderr(), outDir, files); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "compiled %s\n", outDir)
	composePath := filepath.Join(outDir, "compose.telephony.yaml")
	if _, err := os.Stat(composePath); err != nil {
		return fmt.Errorf("generated telephony Compose file: %w", err)
	}
	// Compose runs with its working directory set to the build dir, so the
	// --file path must be absolute: a path relative to the process cwd doubles
	// the build-dir prefix and vanishes when root is a relative package dir.
	composePath, err := filepath.Abs(composePath)
	if err != nil {
		return fmt.Errorf("resolve telephony Compose path: %w", err)
	}

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

	// --public-url wins and skips all tunnel management; otherwise routes
	// with carrier callbacks get a managed cloudflared quick tunnel (V1).
	var public *url.URL
	switch {
	case opts.publicValue != "":
		public, err = parseTelephonyPublicURL(opts.publicValue)
		if err != nil {
			return err
		}
	case len(plan.PublicEndpoints) > 0:
		tunnel, err := startQuickTunnel(ctx, opts.botPort, processOut)
		if err != nil {
			return err
		}
		defer tunnel.Stop()
		public, err = parseTelephonyPublicURL(tunnel.URL)
		if err != nil {
			return fmt.Errorf("managed tunnel returned an unusable origin: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s: managed tunnel %s (quick tunnel URLs rotate on every run)\n", targetName, public)
	}
	if public != nil {
		childEnv = setChildEnv(childEnv, "UNMUTE_PUBLIC_URL", public.String())
	}
	printDevTelephonyEndpoints(cmd.OutOrStdout(), targetName, plan, public)

	run := telephonyComposeRun{
		dir: filepath.Dir(composePath), file: composePath, project: composeProjectName(root, targetName),
		env: childEnv, output: processOut,
		stdout: cmd.OutOrStdout(), stderr: cmd.ErrOrStderr(), logPath: logPath,
	}
	if planCreatesLiveKitSIPRecords(plan) {
		// LiveKit SIP: infrastructure first, then trunk and dispatch records
		// against the local server, then the application (V4). This is the gate
		// for the whole two-phase startup, not a display detail: without it the
		// application starts before any record exists and an inbound call has
		// nowhere to land.
		run.infraServices = telephonyInfraServices(plan)
		run.beforeApp = func(ctx context.Context, env []string) ([]string, error) {
			if err := ensureLiveKitSIPRecords(ctx, cmd.OutOrStdout(), targetName, plan, env); err != nil {
				return nil, err
			}
			return env, nil
		}
	}
	// Set by onReady, read by onStop: both close over it, so the restore
	// survives into the shutdown path (V14).
	var restoreWebhook func(context.Context) error
	run.onStop = func(ctx context.Context) error {
		if restoreWebhook == nil {
			return nil
		}
		return restoreWebhook(ctx)
	}
	run.onReady = func(ctx context.Context) error {
		// The webhook is reconfigured on every start: quick tunnel URLs
		// rotate per run, and the previous value is printed for restore (V3).
		// --no-webhook opts out entirely, for a number this run must not touch.
		if plan.AutoWebhookEndpoint != "" && public != nil && !opts.noWebhook {
			restore, err := autoConfigureCarrierWebhook(ctx, cmd.OutOrStdout(), targetName, plan, public, childEnv)
			if err != nil {
				return err
			}
			restoreWebhook = restore
		}
		if opts.noWebhook && plan.AutoWebhookEndpoint != "" && public != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "%s: --no-webhook, carrier number left untouched; this run is reachable at %s\n",
				targetName, strings.TrimSuffix(public.String(), "/"))
		}
		printDevCallLine(cmd.OutOrStdout(), plan, childEnv)
		// Outbound-capable route: --to places one call now that the graph is
		// healthy; without --to, print how to place one and do nothing (T5).
		// The direction guard in runDevTelephony ensures opts.to is only set for
		// an outbound-capable plan. Placement differs by route: LiveKit SIP has
		// no HTTP dial-out endpoint, so it dispatches the agent on the local
		// server; carrier-websocket and the connector POST to the bot's own
		// /telephony/outbound (I.trigger, I.sipdial).
		if planHasTelephonyFeature(plan, "outbound") {
			if opts.to != "" {
				if plan.Route.Transport == "sip" {
					if err := placeLiveKitDispatch(ctx, cmd.OutOrStdout(), targetName, opts.to, childEnv); err != nil {
						return err
					}
				} else if err := placeOutboundCall(ctx, cmd.OutOrStdout(), targetName, opts.botPort, outboundToken, opts.to, childEnv); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: dial-out ready; re-run with --to <E.164> to place a call\n", targetName)
			}
		}
		return nil
	}
	return runTelephonyCompose(ctx, run)
}

// placeOutboundCall triggers the container's dial-out endpoint over loopback
// (SPEC I.trigger): the CLI, not the tunnel, reaches the published bot port,
// and the returned call id is printed. The Bearer token is the dev secret from
// randomOutboundToken; it is sent, never printed.
func placeOutboundCall(ctx context.Context, out io.Writer, targetName, botPort, token, to string, env []string) error {
	callStart, err := callStartFromEnv(env)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"to": to, "call_start": callStart})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%s/telephony/outbound", botPort)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	var result struct {
		CallID string `json:"call_id"`
	}
	if err := doTelephonyJSON(request, &result); err != nil {
		return fmt.Errorf("place outbound call to %s: %w", to, err)
	}
	fmt.Fprintf(out, "\n  \033[1;32m▸\033[0m calling %s  (call %s)  ·  ctrl-c to stop\n\n", to, result.CallID)
	return nil
}

// printDevCallLine prints the number to dial once an inbound route is live.
func printDevCallLine(out io.Writer, plan *generate.TelephonyRuntimePlan, env []string) {
	if !planHasTelephonyFeature(plan, "inbound") {
		return
	}
	number := envValue(env, plan.Environment["from_number"])
	if number == "" {
		return
	}
	fmt.Fprintf(out, "\n  \033[1;32m▸\033[0m call %s  ·  ctrl-c to stop\n\n", number)
}

// planCreatesLiveKitSIPRecords reports whether `unmute dev --telephony` creates
// the local inbound trunk and dispatch rule for this plan, which is also what
// switches the startup into two phases: infrastructure, then records, then the
// application. Only a LiveKit SIP route that accepts calls has those records.
// The connector and the Pipecat carrier routes carry the inbound feature too but
// have no SIP trunk at all, and an outbound-only package needs no record of
// either kind (SCHEMA N36, 2026-08-12).
func planCreatesLiveKitSIPRecords(plan *generate.TelephonyRuntimePlan) bool {
	return plan.Route.Provider == ir.ProviderLiveKit && plan.Route.Transport == "sip" &&
		planHasTelephonyFeature(plan, string(target.TelephonyInbound))
}

func planHasTelephonyFeature(plan *generate.TelephonyRuntimePlan, feature string) bool {
	for _, evidence := range plan.Evidence {
		if evidence.Feature == feature {
			return true
		}
	}
	return false
}

// envValue returns the value of name in a KEY=VALUE env slice ("" when the
// name is empty or unset).
func envValue(env []string, name string) string {
	if name == "" {
		return ""
	}
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, name+"="); ok {
			return value
		}
	}
	return ""
}
