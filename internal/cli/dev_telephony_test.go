package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slng/unmute/internal/generate"
	"github.com/slng/unmute/internal/ir"
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
	cloudflared := filepath.Join(root, "cloudflared")
	if err := os.WriteFile(cloudflared, []byte("#!/bin/sh\necho 'INF |  https://fake-zero.trycloudflare.com  |' 1>&2\necho $$ > \""+root+"/tunnel.pid\"\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) { return cloudflared, nil }
	t.Cleanup(func() { tunnelLookPath = restoreLook })

	cmd, out := telephonyTestCommand(t)
	err := execDevTelephony(cmd, root, "phone", pipecatTwilioPlan(), composeFiles, devTelephonyOptions{botPort: "7861"})
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
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) {
		t.Error("tunnel management must be skipped when --public-url is set")
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { tunnelLookPath = restoreLook })

	cmd, out := telephonyTestCommand(t)
	opts := devTelephonyOptions{publicValue: "https://mine.example.dev", botPort: "7860"}
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
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { tunnelLookPath = restoreLook })

	cmd, out := telephonyTestCommand(t)
	err := execDevTelephony(cmd, root, "phone", pipecatTwilioPlan(), composeFiles, devTelephonyOptions{botPort: "7860"})
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
	if err := execDevTelephony(cmd, "pkg", "phone", pipecatTwilioPlan(), composeFiles, devTelephonyOptions{botPort: "7862"}); err != nil {
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

	cloudflared := filepath.Join(root, "cloudflared")
	if err := os.WriteFile(cloudflared, []byte("#!/bin/sh\necho 'INF |  https://fake-zero.trycloudflare.com  |'\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) { return cloudflared, nil }
	t.Cleanup(func() { tunnelLookPath = restoreLook })

	cmd, out := telephonyTestCommand(t)
	// The .env has no UNMUTE_OUTBOUND_TOKEN; a clean run proves the CLI supplied it.
	if err := execDevTelephony(cmd, root, "phone", pipecatTwilioOutboundPlan(), composeFiles, devTelephonyOptions{botPort: "7861"}); err != nil {
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

	cloudflared := filepath.Join(root, "cloudflared")
	if err := os.WriteFile(cloudflared, []byte("#!/bin/sh\necho 'INF |  https://fake-zero.trycloudflare.com  |'\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) { return cloudflared, nil }
	t.Cleanup(func() { tunnelLookPath = restoreLook })

	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", livekitConnectorPlan(), composeFiles, devTelephonyOptions{botPort: "7862"}); err != nil {
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
func TestExecDevTelephonyPlacesOutboundCallAfterReady(t *testing.T) {
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

	root, _ := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\n")
	fakeDocker(t, root)
	cloudflared := filepath.Join(root, "cloudflared")
	if err := os.WriteFile(cloudflared, []byte("#!/bin/sh\necho 'INF |  https://fake-zero.trycloudflare.com  |'\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) { return cloudflared, nil }
	t.Cleanup(func() { tunnelLookPath = restoreLook })

	cmd, out := telephonyTestCommand(t)
	opts := devTelephonyOptions{botPort: parsed.Port(), to: "+15559998888"}
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
	if !strings.Contains(out.String(), "CA-test") {
		t.Fatalf("call id not printed:\n%s", out.String())
	}
}

// T5/V6: an outbound-capable target run without --to prints a dial-out hint and
// places no call.
func TestExecDevTelephonyOutboundWithoutToHintsAndPlacesNothing(t *testing.T) {
	root, _ := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\n")
	fakeDocker(t, root)
	cloudflared := filepath.Join(root, "cloudflared")
	if err := os.WriteFile(cloudflared, []byte("#!/bin/sh\necho 'INF |  https://fake-zero.trycloudflare.com  |'\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) { return cloudflared, nil }
	t.Cleanup(func() { tunnelLookPath = restoreLook })

	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", pipecatTwilioOutboundPlan(), composeFiles, devTelephonyOptions{botPort: "7861"}); err != nil {
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
	cloudflared := filepath.Join(root, "cloudflared")
	if err := os.WriteFile(cloudflared, []byte("#!/bin/sh\necho 'INF |  https://fake-zero.trycloudflare.com  |'\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) { return cloudflared, nil }
	t.Cleanup(func() { tunnelLookPath = restoreLook })

	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", pipecatTwilioPlan(), composeFiles, devTelephonyOptions{botPort: "7861"}); err != nil {
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
		devTelephonyOptions{botPort: "7861", publicValue: "https://voice.example.com"})
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

// The borrowed-state rule: a dev session takes a real phone line and must give it
// back on **every** exit path. All three are exercised, because they are three
// different code paths reaching one deferred restore, and the one that matters
// most in practice (ctrl-c) is the one a test is least likely to cover.
func TestExecDevCloudWebsocketRestoresTheNumberOnEveryExitPath(t *testing.T) {
	cases := []struct {
		name      string
		agentBody string
		cancel    bool
		wantErr   bool
	}{
		{name: "clean exit", agentBody: "exit 0\n"},
		{name: "the agent fails", agentBody: "echo boom 1>&2\nexit 3\n", wantErr: true},
		{name: "interrupted", agentBody: "sleep 30\n", cancel: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := fakeTelephonyRoot(t,
				"TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=token\nTWILIO_PHONE_NUMBER=+15550001111\nPIPECAT_CLOUD_ORGANIZATION=org\n")
			fakeUV(t, root, tc.agentBody)
			var updates url.Values
			var calls []string
			server := fakeTwilioAPIRecording(t, "token", "https://old.example/hook", &updates, &calls)
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
				devTelephonyOptions{botPort: "7861", publicValue: "https://voice.example.com"})
			if tc.wantErr && err == nil {
				t.Fatalf("a failing agent must surface an error:\n%s", out.String())
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("execDevCloudWebsocket: %v\n%s", err, out.String())
			}
			// Two updates: the borrow and the restore, in that order, and the last
			// value is what the number had before.
			if len(calls) != 2 {
				t.Fatalf("the number was updated %d time(s), want 2 (borrow and restore): %v", len(calls), calls)
			}
			if calls[0] != "https://voice.example.com/" {
				t.Errorf("the session pointed the number at %q, want the local runner's webhook path", calls[0])
			}
			if calls[1] != "https://old.example/hook" {
				t.Errorf("the number was left pointing at %q, not what it had before", calls[1])
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

// fakeTwilioAPIRecording is fakeTwilioAPI plus an ordered log of every VoiceUrl
// the session wrote, which is how the borrow-and-restore pair is asserted.
func fakeTwilioAPIRecording(t *testing.T, authToken, existingVoiceURL string, updates *url.Values, calls *[]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	current := existingVoiceURL
	mux.HandleFunc("GET /2010-04-01/Accounts/account/IncomingPhoneNumbers.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"incoming_phone_numbers":[{"sid":"PN123","voice_url":"` + current + `"}]}`))
	})
	mux.HandleFunc("POST /2010-04-01/Accounts/account/IncomingPhoneNumbers/PN123.json", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		*updates = r.PostForm
		current = r.PostForm.Get("VoiceUrl")
		*calls = append(*calls, current)
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
