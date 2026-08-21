package cli

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/slng-ai/unmute/internal/generate"
)

// Carrier mode: everything `dev --telephony --carrier` does that a local run
// does not. It lives in one file because that is what makes the default loop
// auditable: a carrier write is a request to the author's own carrier account
// in their name, so the code that makes one has to be somewhere a test can
// point at and say "the default loop never reaches this".
//
// Nothing here is new behaviour. It is what every telephony run did before
// carrier mode became opt-in.

// printCarrierDisclaimer says what carrier mode is doing and what its audio
// delay means. Reaching a laptop from the public network is slow, and that
// delay belongs to the tunnel, not to the agent: without this, a carrier run
// reads as evidence that a deployed agent will be sluggish.
func printCarrierDisclaimer(out io.Writer, targetName string) {
	fmt.Fprintf(out, "%s: --carrier: this places a real call through your carrier.\n", targetName)
	fmt.Fprintf(out, "%s: audio delay in this mode is an artifact of reaching your machine from\n", targetName)
	fmt.Fprintf(out, "%s: the public network. It is not the delay a deployed agent has.\n", targetName)
}

// carrierPublicOrigin resolves the origin the carrier reaches this run through:
// the author's own with --public-url, otherwise a managed cloudflared quick
// tunnel.
//
// Whether a tunnel is wanted at all is the caller's to decide, because it
// differs by route: the Compose routes need one only when the route publishes
// carrier callbacks, while the route whose platform terminates the carrier's
// stream publishes none of ours and still needs one for every carrier run.
//
// The returned stop is never nil, so a caller can defer it without a guard. A
// nil origin with a nil error means no origin was wanted, which is not a
// failure.
func carrierPublicOrigin(ctx context.Context, out io.Writer, targetName string, plan *generate.TelephonyRuntimePlan, opts devTelephonyOptions, processOut io.Writer, wantTunnel bool) (*url.URL, func(), error) {
	noop := func() {}
	switch {
	case opts.publicValue != "":
		public, err := parseTelephonyPublicURL(opts.publicValue)
		if err != nil {
			return nil, noop, err
		}
		return public, noop, nil
	case wantTunnel:
		tunnel, err := startQuickTunnel(ctx, opts.botPort, processOut)
		if err != nil {
			return nil, noop, err
		}
		public, err := parseTelephonyPublicURL(tunnel.URL)
		if err != nil {
			tunnel.Stop()
			return nil, noop, fmt.Errorf("managed tunnel returned an unusable origin: %w", err)
		}
		fmt.Fprintf(out, "%s: managed tunnel %s (quick tunnel URLs rotate on every run)\n", targetName, public)
		return public, tunnel.Stop, nil
	}
	return nil, noop, nil
}

// armCarrierWebhook points the carrier's number at this run and returns the
// restore for whatever it pointed at before. A nil restore means nothing was
// touched, which covers three cases: the route has no webhook endpoint, there
// is no public origin to point at, or --no-webhook asked for the number to be
// left alone.
//
// The webhook is reconfigured on every start rather than once, because quick
// tunnel URLs rotate per run.
func armCarrierWebhook(ctx context.Context, out io.Writer, targetName string, plan *generate.TelephonyRuntimePlan, public *url.URL, env []string, noWebhook bool, report *runReport) (func(context.Context) error, error) {
	if plan.AutoWebhookEndpoint == "" || public == nil {
		return nil, nil
	}
	if noWebhook {
		fmt.Fprintf(out, "%s: --no-webhook, carrier number left untouched; this run is reachable at %s\n",
			targetName, strings.TrimSuffix(public.String(), "/"))
		return nil, nil
	}
	number := envValue(env, plan.Environment["from_number"])
	report.carrierWrite("%s: point %s at this run", plan.Route.Carrier, number)
	restore, err := autoConfigureCarrierWebhook(ctx, out, targetName, plan, public, env)
	if err != nil {
		return nil, err
	}
	return func(restoreCtx context.Context) error {
		report.carrierWrite("%s: restore %s to what it pointed at before", plan.Route.Carrier, number)
		return restore(restoreCtx)
	}, nil
}
