package cli

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/slng/unmute/internal/generate"
	"github.com/spf13/cobra"
)

// devTelephonyOptions carries the dev command flags into the post-gate core.
type devTelephonyOptions struct {
	publicValue string
	botPort     string
	verbose     bool
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
		dir: outDir, file: composePath, project: composeProjectName(root, targetName),
		env: childEnv, output: processOut,
		stdout: cmd.OutOrStdout(), stderr: cmd.ErrOrStderr(), logPath: logPath,
	}
	if len(plan.DevSuppliedEnv) > 0 {
		// LiveKit SIP: infrastructure first, then trunk and dispatch records
		// against the local server, then the application with the IDs (V4).
		run.infraServices = telephonyInfraServices(plan)
		run.beforeApp = func(ctx context.Context, env []string) ([]string, error) {
			injected, err := ensureLiveKitSIPRecords(ctx, cmd.OutOrStdout(), targetName, plan, env)
			if err != nil {
				return nil, err
			}
			for _, name := range plan.DevSuppliedEnv {
				if injected[name] != "" {
					env = setChildEnv(env, name, injected[name])
				}
			}
			return env, nil
		}
	}
	run.onReady = func(ctx context.Context) error {
		// The webhook is reconfigured on every start: quick tunnel URLs
		// rotate per run, and the previous value is printed for restore (V3).
		if plan.AutoWebhookEndpoint != "" && public != nil {
			if err := autoConfigureCarrierWebhook(ctx, cmd.OutOrStdout(), targetName, plan, public, childEnv); err != nil {
				return err
			}
		}
		printDevCallLine(cmd.OutOrStdout(), plan, childEnv)
		return nil
	}
	return runTelephonyCompose(ctx, run)
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
