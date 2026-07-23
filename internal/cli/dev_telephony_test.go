package cli

import (
	"bytes"
	"context"
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
	body := "#!/bin/sh\nprintf '%s | UNMUTE_PUBLIC_URL=%s | TRUNKS=%s/%s\\n' \"$*\" \"$UNMUTE_PUBLIC_URL\" \"$LIVEKIT_SIP_INBOUND_TRUNK\" \"$LIVEKIT_SIP_OUTBOUND_TRUNK\" >> \"$TRACE_FILE\"\ncase \"$*\" in *' logs '*) kill -INT $$;; esac\n"
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
	body := "#!/bin/sh\nprintf '%s | TRUNKS=%s/%s\\n' \"$*\" \"$LIVEKIT_SIP_INBOUND_TRUNK\" \"$LIVEKIT_SIP_OUTBOUND_TRUNK\" >> \"$TRACE_FILE\"\ncase \"$*\" in *' logs '*) kill -INT $$;; esac\n"
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
			env = setChildEnv(env, "LIVEKIT_SIP_INBOUND_TRUNK", "ST_in")
			return setChildEnv(env, "LIVEKIT_SIP_OUTBOUND_TRUNK", "ST_out"), nil
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
	if !strings.Contains(lines[0], "--wait redis livekit_server livekit_sip") || !strings.Contains(lines[0], "TRUNKS=/") {
		t.Fatalf("first up was not trunk-free infra: %q", lines[0])
	}
	full := lines[1]
	if !strings.HasSuffix(strings.Split(full, " | ")[0], "--wait") || !strings.Contains(full, "TRUNKS=ST_in/ST_out") {
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
