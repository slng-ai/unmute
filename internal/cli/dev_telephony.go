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
	carrier     bool   // --carrier: reach the route through the author's own carrier
	noWebhook   bool   // --no-webhook: leave the carrier's number configuration alone
	verbose     bool
	// report is where the run records what it did, and it is what the checks
	// assert against. Optional: the recorder tolerates a nil, so a caller with
	// nothing to print or assert passes nothing.
	report *runReport
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
	// Which mode this run is in, decided once and stated before anything
	// starts: a local plane on this machine, or the author's own carrier. A
	// route with neither has nothing this command can run locally, and saying
	// so here costs one line instead of a failure further in.
	plane := target.TelephonyLocalPlane(plan.LocalPlane)
	if opts.report != nil {
		opts.report.Plane = plane
	}
	switch {
	case opts.carrier:
		printCarrierDisclaimer(cmd.OutOrStdout(), targetName)
	case plane == target.LocalPlaneSIP, plane == target.LocalPlaneMediaWebsocket:
		printLocalPlaneMode(cmd.OutOrStdout(), targetName, plane)
	default:
		return fmt.Errorf("target %q has no local telephony plane on the %s %s route, so there is nothing "+
			"this command can run on this machine; pass --carrier to reach the route through your own carrier account",
			targetName, plan.Route.Provider, plan.Route.Transport)
	}
	childEnv := devChildEnv(root, cmd.ErrOrStderr())
	if err := rejectLocalTopologyConflicts(plan, childEnv); err != nil {
		return err
	}
	childEnv = setChildEnv(childEnv, "UNMUTE_TELEPHONY_PORT", opts.botPort)
	// Before Docker, before the plane, before anything is created. A port this
	// run needs and cannot have is a run that should not start (T103).
	if err := hostPortCheck(telephonyHostPorts(plan, opts.botPort, childEnv)); err != nil {
		return err
	}
	required := externalTelephonyEnv(plan)
	// The SIP plane supplies for itself everything a carrier would have
	// supplied, so none of those names has to be in the author's .env. This is
	// what makes a default run credential-free, and it is the difference
	// between a loop an author can run before lunch and one that needs an
	// account first (SC-004).
	var sipPlane *planeRun
	if !opts.carrier && planeIsSIP(plan) {
		started, err := startPlaneRun(plan, childEnv)
		if err != nil {
			return err
		}
		sipPlane = started
		childEnv = sipPlane.apply(childEnv)
		required = slices.DeleteFunc(required, func(name string) bool {
			return slices.Contains(sipPlane.supplied, name)
		})
	}
	// The other plane, for the routes whose carrier streams media over a
	// WebSocket. Same reason, same shape: it supplies what a carrier would, so a
	// default run needs no account (SC-004).
	var mediaPlane *mediaPlaneRun
	if !opts.carrier && planeIsMediaWebsocket(plan) {
		started, err := startMediaPlaneRun(plan, opts.botPort, true)
		if err != nil {
			return err
		}
		mediaPlane = started
		// The plane holds a listener for the carrier's own API. Released here so
		// a run that ends any way at all leaves no port behind (gate P8).
		defer mediaPlane.stop()
		childEnv = mediaPlane.apply(childEnv)
		required = slices.DeleteFunc(required, func(name string) bool {
			return slices.Contains(mediaPlane.supplied, name)
		})
	}
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
		return fmt.Errorf("missing telephony credentials/configuration: %s; fill the package .env from build/%s/.env.example", strings.Join(missing, ", "), targetName)
	}
	if err := composePreflight(cmd.Context(), childEnv); err != nil {
		return err
	}
	outDir := filepath.Join(root, "build", targetName)
	if err := writeArtifactFiles(cmd.ErrOrStderr(), outDir, files); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "compiled %s\n", outDir)
	if sipPlane != nil {
		// Made here rather than left to Compose: a bind mount whose source is
		// missing is created by the daemon, owned by root, and the endpoint
		// then cannot write its recording into it.
		if err := sipPlane.prepare(outDir); err != nil {
			return err
		}
		childEnv = sipPlane.apply(childEnv)
	}
	if mediaPlane != nil {
		if _, err := mediaPlane.prepare(outDir); err != nil {
			return err
		}
	}
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
	// On the plane, the log stream is also where a transfer says what it did.
	// Watching it here means the seven outcomes reach the terminal as they
	// happen, instead of a developer reading a log file afterwards to find out
	// whether the transfer they just heard fail actually failed (gate P8).
	if sipPlane != nil {
		processOut = io.MultiWriter(processOut, &transferWatcher{
			out: cmd.OutOrStdout(), targetName: targetName, report: opts.report,
		})
	}

	// The public origin, the webhook write and its restore are carrier mode's
	// whole job, and they live in dev_carrier_check.go (V1). The default loop
	// never calls into that file, which is what makes "this run touched no
	// carrier" something a test can assert rather than something we assert.
	var public *url.URL
	stopTunnel := func() {}
	if opts.carrier {
		origin, stop, err := carrierPublicOrigin(ctx, cmd.OutOrStdout(), targetName, plan, opts, processOut, len(plan.PublicEndpoints) > 0)
		if err != nil {
			return err
		}
		public, stopTunnel = origin, stop
	}
	defer stopTunnel()
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
			if err := ensureLiveKitSIPRecords(ctx, cmd.OutOrStdout(), targetName, plan, env, sipPlane.credentialOrNone()); err != nil {
				return nil, err
			}
			return env, nil
		}
	}
	// Set by onReady, read by onStop: both close over it, so the restore
	// survives into the shutdown path (V14).
	var restoreWebhook func(context.Context) error
	run.onStop = func(ctx context.Context) error {
		printPlaneRecordings(cmd.OutOrStdout(), targetName, plan, sipPlane, opts.report)
		if restoreWebhook == nil {
			return nil
		}
		return restoreWebhook(ctx)
	}
	run.onReady = func(ctx context.Context) error {
		if opts.carrier {
			restore, err := armCarrierWebhook(ctx, cmd.OutOrStdout(), targetName, plan, public, childEnv, opts.noWebhook, opts.report)
			if err != nil {
				return err
			}
			restoreWebhook = restore
		}
		printDevCallLine(cmd.OutOrStdout(), targetName, plan, childEnv, sipPlane, opts.report)
		// On this plane the stand-in is the caller, so the run places the call
		// rather than waiting for a person to dial. Said before it happens, and
		// before the run blocks, because a plane that looked like it were
		// waiting would be a plane you waited at forever (gate P4).
		//
		// With --to the run places an outbound call instead, below: asking for
		// one shape and being given both would double the wait and leave two
		// call reports to tell apart.
		//
		// And only on a package that declares an inbound direction. Without one
		// the agent publishes no inbound endpoint, so the stand-in's call is
		// answered 404 and reported as a failure that reads like a broken plane.
		// Found by running an outbound-only package: the honest thing to say is
		// what to run instead.
		if mediaPlane != nil && opts.to == "" && !planHasTelephonyFeature(plan, "inbound") {
			fmt.Fprintf(cmd.OutOrStdout(), "%s: this package declares no inbound direction, so there is "+
				"no incoming call to place. Add --to <E.164> to exercise its outbound direction, or set "+
				"channels.phone inbound: true\n", targetName)
		} else if mediaPlane != nil && opts.to == "" {
			printMediaPlaneReady(cmd.OutOrStdout(), targetName, mediaPlane)
			placeMediaPlaneCall(ctx, cmd.OutOrStdout(), cmd.ErrOrStderr(), targetName, logPath, mediaPlane)
		}
		// Outbound-capable route: --to places one call now that the graph is
		// healthy; without --to, print how to place one and do nothing (T5).
		// The direction guard in runDevTelephony ensures opts.to is only set for
		// an outbound-capable plan. Placement differs by route: LiveKit SIP has
		// no HTTP dial-out endpoint, so it dispatches the agent on the local
		// server; carrier-websocket and the connector POST to the bot's own
		// /telephony/outbound (I.trigger, I.sipdial).
		if planHasTelephonyFeature(plan, "outbound") {
			if opts.to != "" {
				// On the plane the number reaches this machine, and a run that
				// did not say so would read as a real call having been placed.
				if sipPlane != nil {
					printPlaneLocalDial(cmd.OutOrStdout(), targetName, opts.to, plan)
				}
				switch {
				case mediaPlane != nil:
					// The stand-in is the carrier, so it has to be waiting for
					// the call the agent asks for before the agent is asked to
					// place one. That ordering is why this owns the trigger
					// rather than being called after it (T068).
					placeMediaPlaneOutboundCall(ctx, cmd.OutOrStdout(), cmd.ErrOrStderr(),
						targetName, logPath, opts.to, mediaPlane, func() error {
							// io.Discard, because this line announces the same
							// number and the same call id the stand-in's own
							// banner does, and it cannot help arriving second:
							// the agent only answers after the stand-in has taken
							// the call. Printed, it read as the call being
							// answered before it was made.
							return placeOutboundCall(ctx, io.Discard, targetName,
								opts.botPort, outboundToken, opts.to, childEnv)
						})
				case plan.Route.Transport == "sip":
					if err := placeLiveKitDispatch(ctx, cmd.OutOrStdout(), targetName, opts.to, childEnv); err != nil {
						return err
					}
				default:
					if err := placeOutboundCall(ctx, cmd.OutOrStdout(), targetName, opts.botPort, outboundToken, opts.to, childEnv); err != nil {
						return err
					}
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: dial-out ready; re-run with --to <E.164> to place a call\n", targetName)
			}
		}
		return nil
	}
	return runTelephonyCompose(ctx, run)
}

// printLocalPlaneMode states which local plane this run is on. One line,
// before anything starts, because the mode decides whether anything leaves the
// machine and a reader should not have to infer that from what follows.
func printLocalPlaneMode(out io.Writer, targetName string, plane target.TelephonyLocalPlane) {
	fmt.Fprintf(out, "%s: local telephony plane=%s  ·  no carrier involved\n", targetName, plane)
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
func printDevCallLine(out io.Writer, targetName string, plan *generate.TelephonyRuntimePlan, env []string, plane *planeRun, report *runReport) {
	if !planHasTelephonyFeature(plan, "inbound") {
		return
	}
	// The line this replaced said the wiring was ready and that a real inbound
	// call needed publicly reachable SIP and RTP ingress. It was true, and it
	// was the whole problem: a healthy run that could not be called. Now the
	// run says how to call it.
	if plane != nil {
		printPlaneReady(out, targetName, plan, plane, report)
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
	// The plane, not the provider. These records exist because the local plane is
	// a LiveKit SIP plane and an inbound call has to land somewhere on it, which
	// is equally true of the route whose *agent* is a Pipecat bot: reading the
	// provider here left `(pipecat, sip)` with no trunk and no dispatch rule, so
	// its first inbound call was answered 486.
	return planeIsSIP(plan) && planHasTelephonyFeature(plan, string(target.TelephonyInbound))
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
