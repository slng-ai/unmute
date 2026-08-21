package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
	"github.com/spf13/cobra"
)

// fakeTelephonyRoot builds a package root with a .env and returns it. The
// TRACE_FILE entry routes every fake docker call into one trace file.
func fakeTelephonyRoot(t *testing.T, env string) (root, trace string) {
	t.Helper()
	root = t.TempDir()
	trace = filepath.Join(root, "trace.log")
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TRACE_FILE="+trace+"\n"+env), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, trace
}

// allowHeldPorts turns off the host-port probe for a test that binds a port
// itself in place of the container runtime. Not a weakening of the check: those
// tests *are* the thing that would be listening, so the probe is asking about
// them. TestDevRefusesAPortAnotherRunIsHolding holds the check itself.
func allowHeldPorts(t *testing.T) {
	t.Helper()
	restore := hostPortCheck
	hostPortCheck = func([]hostPort) error { return nil }
	t.Cleanup(func() { hostPortCheck = restore })
}

// fakeDocker traces `docker <args>` plus selected env values, and turns the
// logs follow into an immediate clean interrupt so the run returns.
func fakeDocker(t *testing.T, dir string) {
	t.Helper()
	script := filepath.Join(dir, "docker")
	body := "#!/bin/sh\nprintf '%s | UNMUTE_PUBLIC_URL=%s | HOOK=%s/%s\\n' \"$*\" \"$UNMUTE_PUBLIC_URL\" \"$UNMUTE_TEST_HOOK_ONE\" \"$UNMUTE_TEST_HOOK_TWO\" >> \"$TRACE_FILE\"\ncase \"$*\" in *' logs '*) kill -INT $$;; esac\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	restoreCmd, restorePreflight := composeCommand, composePreflight
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, script, args...)
	}
	composePreflight = func(context.Context, []string) error { return nil }
	t.Cleanup(func() { composeCommand, composePreflight = restoreCmd, restorePreflight })
	// Standing in for the container runtime means standing in for the thing that
	// binds the ports too: nothing here publishes anything, so a real probe would
	// only report on whatever else happens to be running on the machine. Left
	// real, every test using this fake passes or fails by what is up in Docker at
	// the time, which is how a container holding 7880 turned six of them red.
	allowHeldPorts(t)
}

func telephonyTestCommand(t *testing.T) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	return cmd, &out
}

func pipecatTwilioPlan() *generate.TelephonyRuntimePlan {
	return &generate.TelephonyRuntimePlan{
		Route: ir.TelephonyKey{Provider: ir.ProviderPipecat, Transport: "carrier-websocket", Carrier: "twilio"},
		PublicEndpoints: []generate.TelephonyEndpoint{
			{Name: "inbound", Method: "POST", Path: "/telephony/inbound"},
			{Name: "media", Method: "WS", Path: "/telephony/ws/{token}"},
		},
		RequiredEnv:  []string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PHONE_NUMBER", "UNMUTE_PUBLIC_URL"},
		Environment:  map[string]string{"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN", "from_number": "TWILIO_PHONE_NUMBER"},
		Evidence:     []ir.TelephonyFeatureEvidence{{Feature: "inbound", Tag: "core"}},
		Services:     []string{"application", "redis"},
		Coordination: "shared",
		LocalPlane:   string(target.LocalPlaneMediaWebsocket),
	}
}

// pipecatTwilioOutboundPlan is the inbound plan plus the outbound feature,
// endpoint, and the UNMUTE_OUTBOUND_TOKEN the CLI must supply itself.
func pipecatTwilioOutboundPlan() *generate.TelephonyRuntimePlan {
	plan := pipecatTwilioPlan()
	plan.PublicEndpoints = append(plan.PublicEndpoints, generate.TelephonyEndpoint{Name: "outbound", Method: "POST", Path: "/telephony/outbound"})
	plan.RequiredEnv = append(plan.RequiredEnv, "UNMUTE_OUTBOUND_TOKEN")
	plan.Evidence = append(plan.Evidence, ir.TelephonyFeatureEvidence{Feature: "outbound", Tag: "core"})
	return plan
}

// pipecatTwilioOutboundOnlyPlan is a package that dials out and answers nothing,
// which is what examples/outbound-reminder is: `channels.phone inbound: false`.
// Its own shape, rather than the inbound fixture with a flag flipped, because the
// endpoint list and the evidence both have to lose their inbound entry or the
// plan describes an agent that does not exist.
func pipecatTwilioOutboundOnlyPlan() *generate.TelephonyRuntimePlan {
	plan := pipecatTwilioOutboundPlan()
	endpoints := plan.PublicEndpoints[:0:len(plan.PublicEndpoints)]
	for _, endpoint := range plan.PublicEndpoints {
		if endpoint.Name != "inbound" {
			endpoints = append(endpoints, endpoint)
		}
	}
	plan.PublicEndpoints = endpoints
	evidence := plan.Evidence[:0:len(plan.Evidence)]
	for _, item := range plan.Evidence {
		if item.Feature != "inbound" {
			evidence = append(evidence, item)
		}
	}
	plan.Evidence = evidence
	return plan
}

// livekitConnectorPlan is the LiveKit Twilio connector shape: the Twilio
// account trio like Pipecat, the LiveKit dev pair supplied locally, and the
// HTTP dial-out token the CLI mints. No Redis, no SIP trunks.
func livekitConnectorPlan() *generate.TelephonyRuntimePlan {
	return &generate.TelephonyRuntimePlan{
		Route: ir.TelephonyKey{Provider: ir.ProviderLiveKit, Transport: "connector", Carrier: "twilio"},
		PublicEndpoints: []generate.TelephonyEndpoint{
			{Name: "inbound", Method: "POST", Path: "/telephony/inbound"},
			{Name: "media", Method: "WS", Path: "/telephony/ws/{token}"},
			{Name: "outbound", Method: "POST", Path: "/telephony/outbound"},
		},
		RequiredEnv: []string{
			"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PHONE_NUMBER",
			"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET",
			"UNMUTE_PUBLIC_URL", "UNMUTE_OUTBOUND_TOKEN",
		},
		LocalEnvironment: []string{"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"},
		Environment:      map[string]string{"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN", "from_number": "TWILIO_PHONE_NUMBER"},
		Evidence:         []ir.TelephonyFeatureEvidence{{Feature: "inbound", Tag: "provisional"}, {Feature: "outbound", Tag: "provisional"}},
		Services:         []string{"application", "livekit_server"},
		Coordination:     "shared",
		LocalPlane:       string(target.LocalPlaneMediaWebsocket),
	}
}

// refuseTunnel is gate P3: a default run starts no tunnel, so the lookup seam
// fails the test if anything reaches for one. Every default-loop test calls
// this, which is what keeps the gate honest as the loop grows: a default path
// that starts tunnelling fails here rather than in a reader's terminal.
func refuseTunnel(t *testing.T) {
	t.Helper()
	restore := tunnelLookPath
	tunnelLookPath = func(string) (string, error) {
		t.Error("a default telephony run must not start a tunnel; that is --carrier's job")
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { tunnelLookPath = restore })
}

// planForRoute builds the plan a default run of this route gets, from the route
// record itself rather than a copy of it, so the gate below reads the table. It
// carries no feature evidence on purpose: that keeps every route on the same
// plain compose path, since the point here is the printed output, not the
// route's own startup shape.
func planForRoute(route target.TelephonyRoute) *generate.TelephonyRuntimePlan {
	return &generate.TelephonyRuntimePlan{
		Route: ir.TelephonyKey{
			Provider: ir.Provider(route.Key.Provider), Transport: route.Key.Transport, Carrier: route.Key.Carrier,
		},
		RequiredEnv: route.RequiredEnvironment,
		ManualSteps: route.ManualSteps,
		LocalPlane:  string(route.LocalPlane),
		Services:    []string{"application"},
	}
}

// P5: every default run prints the carrier steps its route dictates. This is
// the answer to "does a healthy local run mean I can go live": it does not, on
// any route, and the run has to say so every time. The steps come from the
// route table, so a step added there is covered here without an edit.
func TestDefaultRunPrintsEveryDictatedCarrierStep(t *testing.T) {
	planes := 0
	for _, route := range target.SelectableTelephonyRoutes() {
		if route.LocalPlane == target.LocalPlaneNone {
			continue
		}
		planes++
		name := fmt.Sprintf("%s_%s_%s", route.Key.Provider, route.Key.Transport, route.Key.Carrier)
		t.Run(name, func(t *testing.T) {
			if len(route.ManualSteps) == 0 {
				t.Skipf("route %s dictates no carrier steps", name)
			}
			var env strings.Builder
			for _, key := range route.RequiredEnvironment {
				fmt.Fprintf(&env, "%s=value\n", key)
			}
			root, _ := fakeTelephonyRoot(t, env.String())
			fakeDocker(t, root)
			refuseTunnel(t)

			cmd, out := telephonyTestCommand(t)
			if err := execDevTelephony(cmd, root, "phone", planForRoute(route), composeFiles, devTelephonyOptions{botPort: "7899"}); err != nil {
				t.Fatalf("default run on %s: %v\n%s", name, err, out.String())
			}
			printed := out.String()
			for _, step := range route.ManualSteps {
				if !strings.Contains(printed, step) {
					t.Errorf("the default run never printed the carrier step %q:\n%s", step, printed)
				}
			}
			if !strings.Contains(printed, "no carrier involved") {
				t.Errorf("a default run must name its mode on its first lines:\n%s", printed)
			}
		})
	}
	// A table that grows a plane but loses this gate's coverage is the failure
	// this counts against.
	if planes == 0 {
		t.Fatal("no selectable route declares a local plane; the gate covered nothing")
	}
}

// fakeTunnel puts a cloudflared on the lookup seam that reports one quick
// tunnel origin and then sleeps, which is what every carrier-mode test needs
// and none of them needs to spell out.
func fakeTunnel(t *testing.T, dir string) {
	t.Helper()
	script := filepath.Join(dir, "cloudflared")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'INF |  https://fake-zero.trycloudflare.com  |'\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	restore := tunnelLookPath
	tunnelLookPath = func(string) (string, error) { return script, nil }
	t.Cleanup(func() { tunnelLookPath = restore })
}

// FR-016: carrier mode states what its audio delay means, and the default loop
// does not, because it has no such delay to explain. Reaching a laptop from the
// public network is slow; without these lines a carrier run reads as evidence
// that a deployed agent is sluggish, which is the wrong conclusion drawn from a
// real measurement.
func TestCarrierModeStatesWhatItsDelayMeans(t *testing.T) {
	disclaimer := []string{
		"--carrier: this places a real call through your carrier.",
		"artifact of reaching your machine from",
		"not the delay a deployed agent has",
	}

	root, _ := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\n")
	fakeDocker(t, root)
	allowHeldPorts(t)
	fakeTunnel(t, root)
	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", pipecatTwilioPlan(), composeFiles, devTelephonyOptions{botPort: "7864", carrier: true}); err != nil {
		t.Fatalf("carrier run: %v\n%s", err, out.String())
	}
	for _, want := range disclaimer {
		if !strings.Contains(out.String(), want) {
			t.Errorf("carrier mode never said %q:\n%s", want, out.String())
		}
	}

	// The same lines under a default run would be a lie: nothing reaches this
	// machine from outside it.
	newFakeSIPAdmin(t)
	localRoot, _ := fakeTelephonyRoot(t, strings.Join(sipTestEnv(), "\n"))
	fakeDocker(t, localRoot)
	refuseTunnel(t)
	localCmd, localOut := telephonyTestCommand(t)
	if err := execDevTelephony(localCmd, localRoot, "phone", livekitSIPPlan(), composeFiles, devTelephonyOptions{botPort: "8082"}); err != nil {
		t.Fatalf("default run: %v\n%s", err, localOut.String())
	}
	for _, forbidden := range disclaimer {
		if strings.Contains(localOut.String(), forbidden) {
			t.Errorf("a default run claimed a carrier delay it does not have (%q):\n%s", forbidden, localOut.String())
		}
	}
}

// P2: a default run performs no write outside this machine, and this is how
// SC-004 is measured. The recorder sits on the carrier path itself, so this
// asserts what the code did rather than what it printed, and the second half
// asserts the recorder is not simply inert: the same path under --carrier has
// to record, or an empty field would prove nothing.
func TestDefaultRunWritesNothingToACarrier(t *testing.T) {
	newFakeSIPAdmin(t)
	root, _ := fakeTelephonyRoot(t, strings.Join(sipTestEnv(), "\n"))
	fakeDocker(t, root)
	allowHeldPorts(t)
	refuseTunnel(t)
	var local runReport
	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", livekitSIPPlan(), composeFiles,
		devTelephonyOptions{botPort: "8083", report: &local}); err != nil {
		t.Fatalf("default run: %v\n%s", err, out.String())
	}
	if len(local.CarrierWrites) != 0 {
		t.Errorf("a default run wrote to a carrier: %v", local.CarrierWrites)
	}
	if local.Plane != target.LocalPlaneSIP {
		t.Errorf("the report records plane %q, want %q", local.Plane, target.LocalPlaneSIP)
	}

	var updates url.Values
	fakeTwilioAPI(t, "sekrit-auth-77", "https://old.example/hook", &updates)
	carrierRoot, _ := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=sekrit-auth-77\nTWILIO_PHONE_NUMBER=+15550001111\n")
	fakeDocker(t, carrierRoot)
	fakeTunnel(t, carrierRoot)
	plan := pipecatTwilioPlan()
	plan.AutoWebhookEndpoint = "inbound"
	var carrier runReport
	carrierCmd, carrierOut := telephonyTestCommand(t)
	if err := execDevTelephony(carrierCmd, carrierRoot, "phone", plan, composeFiles,
		devTelephonyOptions{botPort: "7865", carrier: true, report: &carrier}); err != nil {
		t.Fatalf("carrier run: %v\n%s", err, carrierOut.String())
	}
	if len(carrier.CarrierWrites) == 0 {
		t.Fatal("carrier mode recorded no write, so the default loop's empty record proves nothing")
	}
	// The set and its restore are two writes, and the restore is the one that
	// matters most: a run that sets and never restores leaves a real number
	// pointing at a dead tunnel.
	joined := strings.Join(carrier.CarrierWrites, "\n")
	for _, want := range []string{"+15550001111", "restore"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the recorded writes never mention %q:\n%s", want, joined)
		}
	}
}

var composeFiles = []generate.File{{Path: "compose.telephony.yaml", Content: []byte("services: {}\n")}}

// V1: without --public-url the managed tunnel supplies UNMUTE_PUBLIC_URL to
// the Compose environment, the plan prints tunnel-derived endpoint URLs, the
// call line names the configured number, and the tunnel group dies with the
// run.
func TestExecDevTelephonyManagedTunnelInjectsPublicURLAndTearsDown(t *testing.T) {
	root, trace := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\n")
	fakeDocker(t, root)
	allowHeldPorts(t)
	cloudflared := filepath.Join(root, "cloudflared")
	if err := os.WriteFile(cloudflared, []byte("#!/bin/sh\necho 'INF |  https://fake-zero.trycloudflare.com  |' 1>&2\necho $$ > \""+root+"/tunnel.pid\"\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) { return cloudflared, nil }
	t.Cleanup(func() { tunnelLookPath = restoreLook })

	cmd, out := telephonyTestCommand(t)
	err := execDevTelephony(cmd, root, "phone", pipecatTwilioPlan(), composeFiles, devTelephonyOptions{botPort: "7861", carrier: true})
	if err != nil {
		t.Fatalf("execDevTelephony: %v\n%s", err, out.String())
	}
	printed := out.String()
	for _, want := range []string{
		"managed tunnel https://fake-zero.trycloudflare.com",
		"inbound POST https://fake-zero.trycloudflare.com/telephony/inbound",
		"media WS wss://fake-zero.trycloudflare.com/telephony/ws/{token}",
		"call +15550001111",
	} {
		if !strings.Contains(printed, want) {
			t.Errorf("output missing %q:\n%s", want, printed)
		}
	}
	// TELEPHONY.md step order: plan facts print before the tunnel starts,
	// endpoint URLs print after the origin is known.
	if plan, tunnel := strings.Index(printed, "telephony route provider="), strings.Index(printed, "managed tunnel"); plan == -1 || plan > tunnel {
		t.Fatalf("plan facts did not print before the tunnel:\n%s", printed)
	}
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "up --build --detach --remove-orphans --wait | UNMUTE_PUBLIC_URL=https://fake-zero.trycloudflare.com") {
		t.Fatalf("compose up did not receive the tunnel origin:\n%s", raw)
	}
	pidRaw, err := os.ReadFile(filepath.Join(root, "tunnel.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid := strings.TrimSpace(string(pidRaw))
	deadline := time.After(5 * time.Second)
	for {
		if err := exec.Command("kill", "-0", pid).Run(); err != nil {
			break // tunnel process is gone
		}
		select {
		case <-deadline:
			t.Fatal("tunnel child still alive after the run finished")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// V1: --public-url bypasses tunnel management entirely; the tunnel seam is
// never consulted and the given origin is used unchanged.
func TestExecDevTelephonyPublicURLSkipsTunnelManagement(t *testing.T) {
	root, trace := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\n")
	fakeDocker(t, root)
	allowHeldPorts(t)
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) {
		t.Error("tunnel management must be skipped when --public-url is set")
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { tunnelLookPath = restoreLook })

	cmd, out := telephonyTestCommand(t)
	opts := devTelephonyOptions{publicValue: "https://mine.example.dev", botPort: "7860", carrier: true}
	if err := execDevTelephony(cmd, root, "phone", pipecatTwilioPlan(), composeFiles, opts); err != nil {
		t.Fatalf("execDevTelephony: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "inbound POST https://mine.example.dev/telephony/inbound") {
		t.Fatalf("plan did not use the provided public URL:\n%s", out.String())
	}
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "UNMUTE_PUBLIC_URL=https://mine.example.dev") {
		t.Fatalf("compose up did not receive the provided origin:\n%s", raw)
	}
}

// V2: with neither --public-url nor cloudflared installed, the run fails
// before Compose with install instructions.
func TestExecDevTelephonyMissingCloudflaredFailsBeforeCompose(t *testing.T) {
	root, trace := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\n")
	fakeDocker(t, root)
	allowHeldPorts(t)
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { tunnelLookPath = restoreLook })

	cmd, out := telephonyTestCommand(t)
	err := execDevTelephony(cmd, root, "phone", pipecatTwilioPlan(), composeFiles, devTelephonyOptions{botPort: "7860", carrier: true})
	if err == nil || !strings.Contains(err.Error(), "brew install cloudflared") || !strings.Contains(err.Error(), "--public-url") {
		t.Fatalf("missing cloudflared error = %v\n%s", err, out.String())
	}
	if raw, err := os.ReadFile(trace); err == nil && strings.Contains(string(raw), " up ") {
		t.Fatalf("compose ran despite the missing tunnel client:\n%s", raw)
	}
}

// V4/V5 plumbing: infra services come up first, the beforeApp hook extends
// the environment, and the full graph sees the extension.
func TestComposeExecutorRunsInfraServicesThenHookThenFullGraph(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "trace.log")
	fake := filepath.Join(dir, "docker")
	body := "#!/bin/sh\nprintf '%s | HOOK=%s/%s\\n' \"$*\" \"$UNMUTE_TEST_HOOK_ONE\" \"$UNMUTE_TEST_HOOK_TWO\" >> \"$TRACE_FILE\"\ncase \"$*\" in *' logs '*) kill -INT $$;; esac\n"
	if err := os.WriteFile(fake, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	restore := composeCommand
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, fake, args...)
	}
	t.Cleanup(func() { composeCommand = restore })

	var output bytes.Buffer
	hookRan := false
	err := runTelephonyCompose(context.Background(), telephonyComposeRun{
		dir: dir, file: filepath.Join(dir, "compose.telephony.yaml"), project: "unmute-test",
		env: []string{"TRACE_FILE=" + trace}, output: &output, stdout: &output, stderr: &output,
		logPath:       filepath.Join(dir, "telephony.log"),
		infraServices: []string{"redis", "livekit_server", "livekit_sip"},
		beforeApp: func(_ context.Context, env []string) ([]string, error) {
			hookRan = true
			env = setChildEnv(env, "UNMUTE_TEST_HOOK_ONE", "one")
			return setChildEnv(env, "UNMUTE_TEST_HOOK_TWO", "two"), nil
		},
	})
	if err != nil || !hookRan {
		t.Fatalf("run = %v, hook ran = %v", err, hookRan)
	}
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 3 {
		t.Fatalf("trace too short:\n%s", raw)
	}
	if !strings.Contains(lines[0], "--wait redis livekit_server livekit_sip") || !strings.Contains(lines[0], "HOOK=/") {
		t.Fatalf("first up ran before the hook, so it must not see the hook's environment: %q", lines[0])
	}
	full := lines[1]
	if !strings.HasSuffix(strings.Split(full, " | ")[0], "--wait") || !strings.Contains(full, "HOOK=one/two") {
		t.Fatalf("full up did not carry hook-extended env: %q", full)
	}
}

func TestComposeExecutorReturnsOnStopError(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "docker")
	body := "#!/bin/sh\ncase \"$*\" in *' logs '*) kill -INT $$;; esac\n"
	if err := os.WriteFile(fake, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	restoreCommand := composeCommand
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, fake, args...)
	}
	t.Cleanup(func() { composeCommand = restoreCommand })

	var output bytes.Buffer
	err := runTelephonyCompose(context.Background(), telephonyComposeRun{
		dir: dir, file: filepath.Join(dir, "compose.telephony.yaml"), project: "unmute-test",
		output: &output, stdout: &output, stderr: &output,
		logPath: filepath.Join(dir, "telephony.log"),
		onStop: func(context.Context) error {
			return errors.New("Twilio restore refused")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "Twilio restore refused") {
		t.Fatalf("onStop error = %v, output:\n%s", err, output.String())
	}
}

// Regression: a relative package dir must still let Compose find its file.
// Compose runs with its working directory set to the build dir, so a --file
// path relative to the process cwd doubles the prefix and vanishes. The fake
// docker resolves --file from its own cwd and fails if it is not there.
func TestExecDevTelephonyRelativeRootLocatesComposeFile(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	if err := os.MkdirAll("pkg", 0o755); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(parent, "trace.log")
	if err := os.WriteFile(filepath.Join("pkg", ".env"),
		[]byte("TRACE_FILE="+trace+"\nTWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// composeArgs puts the compose file at arg 3 (compose --file <file> ...).
	// From the command's working dir, that file must exist.
	script := filepath.Join(parent, "docker")
	body := "#!/bin/sh\nif [ \"$2\" = \"--file\" ] && [ ! -f \"$3\" ]; then printf 'FILE_MISSING:%s\\n' \"$3\" >> \"$TRACE_FILE\"; exit 1; fi\ncase \"$*\" in *' logs '*) kill -INT $$;; esac\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	restoreCmd, restorePreflight := composeCommand, composePreflight
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, script, args...)
	}
	composePreflight = func(context.Context, []string) error { return nil }
	t.Cleanup(func() { composeCommand, composePreflight = restoreCmd, restorePreflight })

	cloudflared := filepath.Join(parent, "cloudflared")
	if err := os.WriteFile(cloudflared, []byte("#!/bin/sh\necho 'INF |  https://fake-zero.trycloudflare.com  |'\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) { return cloudflared, nil }
	t.Cleanup(func() { tunnelLookPath = restoreLook })

	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, "pkg", "phone", pipecatTwilioPlan(), composeFiles, devTelephonyOptions{botPort: "7862", carrier: true}); err != nil {
		t.Fatalf("execDevTelephony with relative root: %v\n%s", err, out.String())
	}
	if raw, err := os.ReadFile(trace); err == nil && strings.Contains(string(raw), "FILE_MISSING") {
		t.Fatalf("compose could not locate its file from the build dir:\n%s", raw)
	}
}

// T2/V4: an outbound-capable target gets UNMUTE_OUTBOUND_TOKEN from the CLI,
// not from .env (which lacks it), and the token is injected into the container
// env yet never printed to the user.
func TestExecDevTelephonyOutboundSuppliesTokenWithoutEnvOrPrint(t *testing.T) {
	root, trace := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\n")
	script := filepath.Join(root, "docker")
	body := "#!/bin/sh\nprintf '%s | OUTBOUND=[%s]\\n' \"$*\" \"$UNMUTE_OUTBOUND_TOKEN\" >> \"$TRACE_FILE\"\ncase \"$*\" in *' logs '*) kill -INT $$;; esac\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	restoreCmd, restorePreflight := composeCommand, composePreflight
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, script, args...)
	}
	composePreflight = func(context.Context, []string) error { return nil }
	t.Cleanup(func() { composeCommand, composePreflight = restoreCmd, restorePreflight })

	fakeTunnel(t, root)

	cmd, out := telephonyTestCommand(t)
	// The .env has no UNMUTE_OUTBOUND_TOKEN; a clean run proves the CLI supplied it.
	if err := execDevTelephony(cmd, root, "phone", pipecatTwilioOutboundPlan(), composeFiles, devTelephonyOptions{botPort: "7861", carrier: true}); err != nil {
		t.Fatalf("execDevTelephony (outbound): %v\n%s", err, out.String())
	}
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "OUTBOUND=[]") {
		t.Fatalf("UNMUTE_OUTBOUND_TOKEN was not injected into the container env:\n%s", raw)
	}
	if strings.Contains(out.String(), "OUTBOUND_TOKEN") {
		t.Fatalf("the outbound token must never be printed:\n%s", out.String())
	}
}

// V5: the LiveKit connector run mints and injects UNMUTE_OUTBOUND_TOKEN (its
// runtime lists it) and demands only the Twilio account trio from .env, exactly
// like the Pipecat route. LIVEKIT_URL and the key pair are supplied locally.
func TestExecDevTelephonyConnectorSuppliesTokenAndTwilioEnvOnly(t *testing.T) {
	root, trace := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\n")
	script := filepath.Join(root, "docker")
	body := "#!/bin/sh\nprintf '%s | OUTBOUND=[%s]\\n' \"$*\" \"$UNMUTE_OUTBOUND_TOKEN\" >> \"$TRACE_FILE\"\ncase \"$*\" in *' logs '*) kill -INT $$;; esac\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	restoreCmd, restorePreflight := composeCommand, composePreflight
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, script, args...)
	}
	composePreflight = func(context.Context, []string) error { return nil }
	t.Cleanup(func() { composeCommand, composePreflight = restoreCmd, restorePreflight })

	fakeTunnel(t, root)

	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", livekitConnectorPlan(), composeFiles, devTelephonyOptions{botPort: "7862", carrier: true}); err != nil {
		t.Fatalf("execDevTelephony (connector): %v\n%s", err, out.String())
	}
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "OUTBOUND=[]") {
		t.Fatalf("connector run did not inject UNMUTE_OUTBOUND_TOKEN:\n%s", raw)
	}
	if strings.Contains(out.String(), "OUTBOUND_TOKEN") {
		t.Fatalf("the outbound token must never be printed:\n%s", out.String())
	}
}

// V5: a LiveKit SIP run injects no UNMUTE_OUTBOUND_TOKEN — SIP dials out by
// agent dispatch, so the token would be a dead injection.
func TestExecDevTelephonySIPInjectsNoOutboundToken(t *testing.T) {
	// Own fake docker, so own port stub: see fakeDocker for why the probe has no
	// place in a test where nothing publishes.
	allowHeldPorts(t)
	newFakeSIPAdmin(t)
	root, trace := fakeTelephonyRoot(t, strings.Join(sipTestEnv(), "\n"))
	script := filepath.Join(root, "docker")
	body := "#!/bin/sh\nprintf '%s | OUTBOUND=[%s]\\n' \"$*\" \"$UNMUTE_OUTBOUND_TOKEN\" >> \"$TRACE_FILE\"\ncase \"$*\" in *' logs '*) kill -INT $$;; esac\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	restoreCmd, restorePreflight := composeCommand, composePreflight
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, script, args...)
	}
	composePreflight = func(context.Context, []string) error { return nil }
	t.Cleanup(func() { composeCommand, composePreflight = restoreCmd, restorePreflight })
	refuseTunnel(t)

	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", livekitSIPPlan(), composeFiles, devTelephonyOptions{botPort: "8081"}); err != nil {
		t.Fatalf("execDevTelephony (sip): %v\n%s", err, out.String())
	}
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "OUTBOUND=[]") {
		t.Fatalf("SIP run injected an outbound token it never uses:\n%s", raw)
	}
}

// T4/V5: with --to, the CLI POSTs the Bearer-authed dial-out trigger to the
// container's published bot port over loopback once the graph is healthy, and
// prints the returned call id.
func TestV4_ExecDevTelephonyPlacesOutboundCallWithCallStartAfterReady(t *testing.T) {
	var gotAuth, gotBody, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{"session_id":"s","call_id":"CA-test","status":"accepted"}`))
	}))
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	root, _ := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\nUNMUTE_CALL_START={\"name\":\"Ada\",\"attempts\":2}\n")
	fakeDocker(t, root)
	allowHeldPorts(t)
	fakeTunnel(t, root)
	// The httptest server above is standing in for the container that would
	// publish this port, so the probe would be refusing this test's own listener.
	allowHeldPorts(t)

	cmd, out := telephonyTestCommand(t)
	opts := devTelephonyOptions{botPort: parsed.Port(), to: "+15559998888", carrier: true}
	if err := execDevTelephony(cmd, root, "phone", pipecatTwilioOutboundPlan(), composeFiles, opts); err != nil {
		t.Fatalf("execDevTelephony (outbound --to): %v\n%s", err, out.String())
	}
	if gotPath != "/telephony/outbound" {
		t.Fatalf("dial-out POST path = %q", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") || len(gotAuth) <= len("Bearer ") {
		t.Fatalf("dial-out trigger missing Bearer token: %q", gotAuth)
	}
	if !strings.Contains(gotBody, `"to":"+15559998888"`) {
		t.Fatalf("dial-out body = %q", gotBody)
	}
	if !strings.Contains(gotBody, `"call_start":{"attempts":2,"name":"Ada"}`) {
		t.Fatalf("dial-out trigger dropped typed call_start: %q", gotBody)
	}
	if !strings.Contains(out.String(), "CA-test") {
		t.Fatalf("call id not printed:\n%s", out.String())
	}
}

// T5/V6: an outbound-capable target run without --to prints a dial-out hint and
// places no call.
func TestExecDevTelephonyOutboundWithoutToHintsAndPlacesNothing(t *testing.T) {
	root, _ := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\n")
	fakeDocker(t, root)
	allowHeldPorts(t)
	fakeTunnel(t, root)

	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", pipecatTwilioOutboundPlan(), composeFiles, devTelephonyOptions{botPort: "7861", carrier: true}); err != nil {
		t.Fatalf("outbound without --to: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "dial-out ready") {
		t.Fatalf("outbound-capable target without --to should hint at dial-out:\n%s", out.String())
	}
	if strings.Contains(out.String(), "calling ") {
		t.Fatalf("no call must be placed without --to:\n%s", out.String())
	}
}

// T5/V7: an inbound-only target prints its dial-in number and no dial-out text.
func TestExecDevTelephonyInboundOnlyHasNoDialOutText(t *testing.T) {
	root, _ := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\n")
	fakeDocker(t, root)
	allowHeldPorts(t)
	fakeTunnel(t, root)

	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", pipecatTwilioPlan(), composeFiles, devTelephonyOptions{botPort: "7861", carrier: true}); err != nil {
		t.Fatalf("inbound-only: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "dial-out") {
		t.Fatalf("inbound-only target must not mention dial-out:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "call +15550001111") {
		t.Fatalf("inbound-only target should print its dial-in number:\n%s", out.String())
	}
}

// cloudWebsocketPlan is the (pipecat, cloud-websocket, twilio) shape: the three
// carrier names, and **no processes and no public endpoints**, because the
// operator hosts nothing (SCHEMA N38).
func cloudWebsocketPlan() *generate.TelephonyRuntimePlan {
	return &generate.TelephonyRuntimePlan{
		Route:       ir.TelephonyKey{Provider: ir.ProviderPipecat, Transport: "cloud-websocket", Carrier: "twilio"},
		RequiredEnv: []string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PHONE_NUMBER", "PIPECAT_CLOUD_ORGANIZATION"},
		Environment: map[string]string{
			"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN", "from_number": "TWILIO_PHONE_NUMBER",
		},
		Evidence:     []ir.TelephonyFeatureEvidence{{Feature: "inbound", Tag: "provisional"}},
		Services:     []string{"application"},
		Coordination: "shared",
		ManualSteps:  []string{"create a TwiML Bin"},
		LocalPlane:   string(target.LocalPlaneMediaWebsocket),
	}
}

// fakeUV puts a `uv` on PATH that runs the given shell body. The local phone path
// starts the compiled agent with `uv run bot.py`, so this is what stands in for it.
func fakeUV(t *testing.T, dir, body string) {
	t.Helper()
	script := filepath.Join(dir, "uv")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// P3, on the route with its own dev entry point: a default run on
// cloud-websocket starts no tunnel and asks the carrier for nothing. This route
// needs its own case because it does not go through execDevTelephony at all —
// it has its own tunnel call and its own webhook write, and a gate that only
// covered the Compose path would have missed both.
func TestExecDevCloudWebsocketDefaultRunStartsNoTunnelAndTouchesNoCarrier(t *testing.T) {
	// The credentials are present because the route declares them, and what
	// the carrier-free media plane needs from them is not settled here. The
	// gate is that having them changes nothing: none of them is sent anywhere.
	root, _ := fakeTelephonyRoot(t,
		"TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\nPIPECAT_CLOUD_ORGANIZATION=org\n")
	fakeUV(t, root, "exit 0\n")
	refuseTunnel(t)
	var calls []string
	fakeTwilioAPIRecording(t, "https://old.example/hook", false, &calls)
	restoreReady := cloudWebsocketAgentReady
	cloudWebsocketAgentReady = func(context.Context, string, <-chan error) error { return nil }
	t.Cleanup(func() { cloudWebsocketAgentReady = restoreReady })

	cmd, out := telephonyTestCommand(t)
	if err := execDevCloudWebsocket(cmd, root, "phone", cloudWebsocketPlan(), cloudWebsocketFiles,
		devTelephonyOptions{botPort: "7863"}); err != nil {
		t.Fatalf("default run on cloud-websocket: %v\n%s", err, out.String())
	}
	if len(calls) != 0 {
		t.Errorf("a default run reached the carrier's API: %v", calls)
	}
	printed := out.String()
	if !strings.Contains(printed, "no carrier involved") {
		t.Errorf("a default run must name its mode:\n%s", printed)
	}
	for _, forbidden := range []string{"borrowed", "trycloudflare", "voice webhook"} {
		if strings.Contains(printed, forbidden) {
			t.Errorf("a default run printed %q, which belongs to carrier mode:\n%s", forbidden, printed)
		}
	}
}

var cloudWebsocketFiles = []generate.File{{Path: "bot.py", Content: []byte("# generated\n")}}

// The route has no Compose graph and no credentials of the dev command's own, so
// the one thing it must refuse by name is a missing carrier credential. Refusing
// by name matters more here than elsewhere: a deployed pure-inbound agent on this
// route needs none of them, so an operator can reasonably be surprised that a
// local session does.
func TestExecDevCloudWebsocketRefusesMissingCredentialsByName(t *testing.T) {
	root, _ := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\n")
	cmd, _ := telephonyTestCommand(t)
	err := execDevCloudWebsocket(cmd, root, "phone", cloudWebsocketPlan(), cloudWebsocketFiles,
		devTelephonyOptions{botPort: "7861", publicValue: "https://voice.example.com", carrier: true})
	if err == nil {
		t.Fatal("a session with no auth token and no number must refuse")
	}
	for _, want := range []string{"TWILIO_AUTH_TOKEN", "TWILIO_PHONE_NUMBER", "hosts nothing in production"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal is missing %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "TWILIO_ACCOUNT_SID") {
		t.Errorf("the refusal names a value that is set: %v", err)
	}
}

func TestExecDevCloudWebsocketDoesNotTouchTwilioBeforeAgentReady(t *testing.T) {
	root, _ := fakeTelephonyRoot(t,
		"TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\nPIPECAT_CLOUD_ORGANIZATION=org\n")
	fakeUV(t, root, "sleep 30\n")
	var calls []string
	fakeTwilioAPIRecording(t, "https://old.example/hook", false, &calls)
	restoreReady := cloudWebsocketAgentReady
	cloudWebsocketAgentReady = func(context.Context, string, <-chan error) error {
		return errors.New("not ready")
	}
	t.Cleanup(func() { cloudWebsocketAgentReady = restoreReady })

	cmd, _ := telephonyTestCommand(t)
	err := execDevCloudWebsocket(cmd, root, "phone", cloudWebsocketPlan(), cloudWebsocketFiles,
		devTelephonyOptions{botPort: "7861", publicValue: "https://voice.example.com", carrier: true})
	if err == nil || !strings.Contains(err.Error(), "local agent not ready") {
		t.Fatalf("readiness failure = %v, want local-agent error", err)
	}
	if len(calls) != 0 {
		t.Fatalf("Twilio was touched before readiness: %v", calls)
	}
}

// The borrowed-state rule: a dev session takes a real phone line and must give it
// back on **every** exit path. Clean, agent-error, ctrl-c, and restore-error
// paths all reach the same deferred restore.
func TestExecDevCloudWebsocketRestoresTheNumberOnEveryExitPath(t *testing.T) {
	restoreReady := cloudWebsocketAgentReady
	cloudWebsocketAgentReady = func(context.Context, string, <-chan error) error { return nil }
	t.Cleanup(func() { cloudWebsocketAgentReady = restoreReady })
	cases := []struct {
		name         string
		agentBody    string
		cancel       bool
		restoreFails bool
		wantErr      bool
	}{
		{name: "clean exit", agentBody: "exit 0\n"},
		{name: "the agent fails", agentBody: "echo boom 1>&2\nexit 3\n", wantErr: true},
		{name: "interrupted", agentBody: "sleep 30\n", cancel: true},
		{name: "restore fails", agentBody: "exit 0\n", restoreFails: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := fakeTelephonyRoot(t,
				"TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\nPIPECAT_CLOUD_ORGANIZATION=org\n")
			fakeUV(t, root, tc.agentBody)
			var calls []string
			server := fakeTwilioAPIRecording(t, "https://old.example/hook", tc.restoreFails, &calls)
			_ = server

			cmd, out := telephonyTestCommand(t)
			ctx, cancel := context.WithCancel(context.Background())
			cmd.SetContext(ctx)
			defer cancel()
			if tc.cancel {
				// The interrupt path: signal.NotifyContext derives from this context, so
				// cancelling it reaches exactly the branch a ctrl-c reaches.
				go func() {
					time.Sleep(300 * time.Millisecond)
					cancel()
				}()
			}
			err := execDevCloudWebsocket(cmd, root, "phone", cloudWebsocketPlan(), cloudWebsocketFiles,
				devTelephonyOptions{botPort: "7861", publicValue: "https://voice.example.com", carrier: true})
			if tc.wantErr && err == nil {
				t.Fatalf("expected command error:\n%s", out.String())
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("execDevCloudWebsocket: %v\n%s", err, out.String())
			}
			if tc.restoreFails && !strings.Contains(err.Error(), "restore") {
				t.Fatalf("restore failure was not returned: %v", err)
			}
			// Two updates: borrow with POST, then restore the exact prior GET
			// configuration on every exit path.
			wantCalls := []string{
				"POST https://voice.example.com/",
				"GET https://old.example/hook",
			}
			if !slices.Equal(calls, wantCalls) {
				t.Fatalf("Twilio voice configuration writes = %v, want %v", calls, wantCalls)
			}
			printed := out.String()
			for _, want := range []string{"restored on exit", "TwiML Bin is untouched", "borrowed +15550001111"} {
				if !strings.Contains(printed, want) {
					t.Errorf("the session did not state %q:\n%s", want, printed)
				}
			}
		})
	}
}

// fakeTwilioAPIRecording keeps the current voice configuration and records each
// URL+method write, so the borrow-and-restore pair is asserted as one value.
func fakeTwilioAPIRecording(t *testing.T, existingVoiceURL string, restoreFails bool, calls *[]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	current := existingVoiceURL
	currentMethod := http.MethodGet
	mux.HandleFunc("GET /2010-04-01/Accounts/account/IncomingPhoneNumbers.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"incoming_phone_numbers":[{"sid":"PN123","voice_url":"` + current + `","voice_method":"` + currentMethod + `"}]}`))
	})
	mux.HandleFunc("POST /2010-04-01/Accounts/account/IncomingPhoneNumbers/PN123.json", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		current = r.PostForm.Get("VoiceUrl")
		currentMethod = r.PostForm.Get("VoiceMethod")
		*calls = append(*calls, currentMethod+" "+current)
		if restoreFails && len(*calls) == 2 {
			http.Error(w, "restore refused", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"sid":"PN123"}`))
	})
	// Anything else is a request this route must never make: no call creation, no
	// markup, nothing at the platform.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the local session made an unexpected carrier request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	restore := twilioAPIBase
	twilioAPIBase = server.URL
	t.Cleanup(func() { twilioAPIBase = restore })
	return server
}

// T103: a port another run is holding stops this one, with a message naming the
// port and what moves it.
//
// The reason this exists is a real diagnosis cycle. A run reached an agent
// started by a *different* run in a different worktree, on the default port,
// and answered a 404 for a route the emitted agent plainly defines. Docker does
// name a published-port collision clearly; the agent port is the one that fails
// silently, by succeeding against the wrong agent.
func TestDevRefusesAPortAnotherRunIsHolding(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	port := strconv.Itoa(held.Addr().(*net.TCPAddr).Port)

	err = rejectOccupiedHostPorts([]hostPort{
		{Port: port, What: "the local agent", MovedBy: "--bot-port"},
	})
	if err == nil {
		t.Fatal("a run started on a port another process holds; it would reach that process's agent")
	}
	for _, want := range []string{port, "the local agent", "--bot-port", "report its answers as yours"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}

	// The holder that a loopback-only probe cannot see, and the one people
	// actually hit: Docker publishes on the wildcard, so another run's LiveKit
	// Server on 7880 shows up here and nowhere on 127.0.0.1. On darwin a
	// wildcard bind succeeds while loopback is held, which is why the probe has
	// to try both and why this case is its own.
	wildcard, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = wildcard.Close() }()
	wildPort := strconv.Itoa(wildcard.Addr().(*net.TCPAddr).Port)
	if err := rejectOccupiedHostPorts([]hostPort{
		{Port: wildPort, What: "LiveKit Server", MovedBy: "LIVEKIT_HOST_PORT"},
	}); err == nil {
		t.Error("a run started on a port a container publishes on the wildcard; the port error " +
			"would surface later as the container runtime's own message in the log file")
	}

	// And a free port is not refused, or nothing could ever start. The probe must
	// also release what it takes: a second call on the same port has to pass.
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	freePort := strconv.Itoa(free.Addr().(*net.TCPAddr).Port)
	if err := free.Close(); err != nil {
		t.Fatal(err)
	}
	ports := []hostPort{{Port: freePort, What: "the local agent", MovedBy: "--bot-port"}}
	for round := range 2 {
		if err := rejectOccupiedHostPorts(ports); err != nil {
			t.Fatalf("round %d refused a free port, so the probe is holding what it tested: %v", round, err)
		}
	}
}

// Which ports a run claims, per plane. The SIP plane publishes two more and each
// has its own variable, and a message naming the wrong variable sends the reader
// to change something that has no effect.
func TestTheRunKnowsWhichPortsItClaims(t *testing.T) {
	media := telephonyHostPorts(pipecatTwilioOutboundPlan(), "7860", nil)
	if len(media) != 1 || media[0].Port != "7860" {
		t.Errorf("the media-websocket plane claims %+v; it publishes the agent's port and no more", media)
	}

	sip := telephonyHostPorts(livekitSIPPlan(), "7860", nil)
	byMover := map[string]string{}
	for _, port := range sip {
		byMover[port.MovedBy] = port.Port
	}
	for mover, want := range map[string]string{
		"--bot-port":            "7860",
		"LIVEKIT_HOST_PORT":     "7880",
		"LIVEKIT_SIP_HOST_PORT": "5060",
	} {
		if byMover[mover] != want {
			t.Errorf("%s covers port %q, want %q; the defaults here and in the generated Compose file "+
				"have to agree or the probe checks a port nothing publishes", mover, byMover[mover], want)
		}
	}

	// And an override is read, or the probe checks the default while the run
	// publishes something else.
	moved := telephonyHostPorts(livekitSIPPlan(), "7860", []string{"LIVEKIT_SIP_HOST_PORT=5070"})
	found := false
	for _, port := range moved {
		if port.MovedBy == "LIVEKIT_SIP_HOST_PORT" {
			found = port.Port == "5070"
		}
	}
	if !found {
		t.Errorf("an overridden LIVEKIT_SIP_HOST_PORT was ignored: %+v", moved)
	}
}

// An outbound-only package has no inbound endpoint, so the plane must not place
// an inbound call at it. Found by running examples/outbound-reminder, whose
// channels declare `inbound: false`: the stand-in placed a call, the agent
// answered 404, and the run reported "the local call did not complete", which
// reads as a broken plane rather than as a package with one direction.
func TestThePlaneDoesNotCallAPackageWithNoInboundDirection(t *testing.T) {
	plan := pipecatTwilioOutboundOnlyPlan()
	if planHasTelephonyFeature(plan, "inbound") {
		t.Fatal("the outbound-only fixture declares inbound, so this test cannot reach the case it is about")
	}
	root, _ := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\n")
	fakeDocker(t, root)
	allowHeldPorts(t)
	refuseTunnel(t)

	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", plan, composeFiles, devTelephonyOptions{botPort: "8099"}); err != nil {
		t.Fatalf("execDevTelephony: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "declares no inbound direction") {
		t.Errorf("the run did not say why no call was placed:\n%s", out.String())
	}
	// And it must not report a failed call, because it never should have tried.
	if strings.Contains(out.String(), "did not complete") {
		t.Errorf("the run placed an inbound call at a package that answers none:\n%s", out.String())
	}
}
