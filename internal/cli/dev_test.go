package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/scaffold"
	"github.com/slng-ai/unmute/internal/spec"
)

func TestParseDotenv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := strings.Join([]string{
		"# a comment",
		"",
		"SLNG_API_KEY=sk-slng",
		`OPENAI_API_KEY="sk-openai"`,
		"export REGION='eu-central'",
		"MALFORMED",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := parseDotenv(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"SLNG_API_KEY":   "sk-slng",
		"OPENAI_API_KEY": "sk-openai",
		"REGION":         "eu-central",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("parseDotenv[%q] = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["MALFORMED"]; ok {
		t.Error("malformed line should be skipped")
	}
}

func TestDevChildEnv_readsDotenvLocal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env.local"), []byte("SLNG_API_KEY=sk-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := devChildEnv(dir, &bytes.Buffer{})
	if !contains(env, "SLNG_API_KEY=sk-secret") {
		t.Errorf(".env.local value not passed to the child env; env = %v", env)
	}
}

func TestV14DevChildEnvReadsWorkingDirectoryThenPackageDotenv(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "examples", "agent")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte(
		"UNMUTE_TEST_REPO_ENV=repo\nUNMUTE_TEST_CWD_LOCAL_WINS=repo-env\nUNMUTE_TEST_SHARED_ENV=repo-env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".env.local"), []byte(
		"UNMUTE_TEST_REPO_LOCAL_ENV=repo-local\nUNMUTE_TEST_CWD_LOCAL_WINS=repo-local\nUNMUTE_TEST_PACKAGE_WINS=repo-local\nUNMUTE_TEST_SHARED_ENV=repo-local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(
		"UNMUTE_TEST_PACKAGE_ENV=package\nUNMUTE_TEST_PACKAGE_LOCAL_WINS=package-env\nUNMUTE_TEST_PACKAGE_WINS=package-env\nUNMUTE_TEST_SHARED_ENV=package-env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env.local"), []byte(
		"UNMUTE_TEST_PACKAGE_LOCAL_ENV=package-local\nUNMUTE_TEST_PACKAGE_LOCAL_WINS=package-local\nUNMUTE_TEST_SHARED_ENV=package-local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNMUTE_TEST_SHARED_ENV", "shell")
	t.Chdir(repo)

	env := devChildEnv(root, &bytes.Buffer{})
	for _, want := range []string{
		"UNMUTE_TEST_REPO_ENV=repo",
		"UNMUTE_TEST_REPO_LOCAL_ENV=repo-local",
		"UNMUTE_TEST_CWD_LOCAL_WINS=repo-local",
		"UNMUTE_TEST_PACKAGE_ENV=package",
		"UNMUTE_TEST_PACKAGE_LOCAL_ENV=package-local",
		"UNMUTE_TEST_PACKAGE_WINS=package-env",
		"UNMUTE_TEST_PACKAGE_LOCAL_WINS=package-local",
		"UNMUTE_TEST_SHARED_ENV=package-local",
	} {
		if !contains(env, want) {
			t.Errorf("dev child env missing %q", want)
		}
	}
}

func TestDevChildEnv_missingFilesAreFine(t *testing.T) {
	var warn bytes.Buffer
	env := devChildEnv(t.TempDir(), &warn) // no .env or .env.local present
	if len(env) == 0 {
		t.Fatal("expected the ambient environment when .env is absent")
	}
	if warn.Len() != 0 {
		t.Errorf("missing dotenv files must not warn; got %q", warn.String())
	}
}

func TestDevChildEnv_readsPackageRootFilesOnce(t *testing.T) {
	dir := t.TempDir()
	tooLong := strings.Repeat("x", 64*1024+1)
	for _, name := range []string{".env", ".env.local"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(tooLong), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)

	var warn bytes.Buffer
	devChildEnv(dir, &warn)
	for _, name := range []string{".env", ".env.local"} {
		prefix := "warning: reading " + filepath.Join(dir, name) + ":"
		if got := strings.Count(warn.String(), prefix); got != 1 {
			t.Errorf("%s warned %d times, want once:\n%s", name, got, warn.String())
		}
	}
}

func TestBrowserCommand(t *testing.T) {
	for _, tc := range []struct {
		goos string
		name string
	}{
		{"darwin", "open"},
		{"linux", "xdg-open"},
	} {
		if name, _ := browserCommand(tc.goos, "http://x"); name != tc.name {
			t.Errorf("browserCommand(%q) = %q, want %q", tc.goos, name, tc.name)
		}
	}
}

func TestDev_help(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"dev", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--no-open", "--bot-port", "--target", "--telephony", "--public-url", "UNMUTE_TELEPHONY_PORT", "talk to it"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dev --help missing %q; got:\n%s", want, out.String())
		}
	}
}

// FR-028: `--telephony` on the Daily route has nothing to offer, so it refuses.
//
// The refusal is the interesting part. The carrierless Daily form dials out and
// cannot receive calls, so there is no local topology to run, but the author can
// still talk to this agent in the browser. A silent no-op here would be the flag
// that does nothing which Principle II forbids.
func TestDevTelephonyRefusesOnTheDailyRouteAndNamesWhatWorks(t *testing.T) {
	cmd := newRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	root := filepath.Join("..", "testdata", "safe_core")
	cmd.SetArgs([]string{"dev", "--telephony", "--target", "pipecat", root})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("--telephony on the Daily route must refuse, got exit 0")
	}
	message := err.Error()
	for _, want := range []string{
		"daily-sip",      // names the route
		"outbound-only",  // names what the shape can do
		"cannot receive", // names why --telephony cannot run it
		"browser",        // names the mode that does work
		"safe_core",      // names the package the command received
	} {
		if !strings.Contains(message, want) {
			t.Errorf("refusal missing %q:\n%s", want, message)
		}
	}
	// It must not claim the route has no telephony. It is the only Pipecat
	// telephony route there is.
	for _, forbidden := range []string{"carries the phone call", "no resolved telephony route", "not supported", "unsupported"} {
		if strings.Contains(message, forbidden) {
			t.Errorf("refusal says %q, which is false for this route:\n%s", forbidden, message)
		}
	}
}

// The carrier form of the Daily route refuses too, and it has to refuse for a
// different reason, in different words. It *does* have a local telephony
// topology: the emitted helper. What it does not have is a way for this command
// to run that helper somewhere the operator's carrier can reach, which is the
// whole point of the tunnel the README dictates.
//
// The no-carrier message would be false here ("no local telephony topology to
// run"), and the generic "no executable telephony topology" line would be false
// too, since this route resolves a plan with a process in it.
func TestDevTelephonyRefusesOnTheDailyCarrierFormAndNamesTheHelper(t *testing.T) {
	cmd := newRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	// This is a fixture, not a public example: the route keeps its guards without
	// recommending the carrier form to readers.
	root := filepath.Join("..", "testdata", "daily_carrier")
	cmd.SetArgs([]string{"dev", "--telephony", "--target", "pipecat", root})
	if err := cmd.Execute(); err == nil {
		t.Fatal("--telephony on the Daily carrier form must refuse, got exit 0")
	} else {
		message := err.Error()
		for _, want := range []string{
			"telephony_helper.py", // names the artifact that does the work
			"README",              // names where the two commands are written out
			"tunnel",              // names the local test path rather than denying one
			"twilio",              // names the carrier the target declared
			"browser",             // still names a mode that works right now
		} {
			if !strings.Contains(message, want) {
				t.Errorf("refusal missing %q:\n%s", want, message)
			}
		}
		for _, forbidden := range []string{
			"no local telephony topology",      // false: the helper is one
			"no executable telephony topology", // false: the plan has a process
			"not supported", "unsupported",
		} {
			if strings.Contains(message, forbidden) {
				t.Errorf("refusal says %q, which is false for this route:\n%s", forbidden, message)
			}
		}
	}
}

func TestTelephonyDevPlanUsesExactPublicURLAndResolvedArtifact(t *testing.T) { // telephony V11, V17
	public, err := parseTelephonyPublicURL("https://voice.example.com/unmute/")
	if err != nil {
		t.Fatal(err)
	}
	plan := &generate.TelephonyRuntimePlan{
		Route: ir.TelephonyKey{Provider: ir.ProviderPipecat, Transport: "carrier-websocket", Carrier: "twilio"},
		PublicEndpoints: []generate.TelephonyEndpoint{
			{Name: "inbound", Method: "POST", Path: "/telephony/inbound"},
			{Name: "media", Method: "WS", Path: "/telephony/ws/{token}"},
		},
		ManualSteps: []string{"configure signed callbacks"}, Services: []string{"application", "redis"}, Coordination: "shared",
		Reasons: []ir.TelephonyCoordinationReason{{Name: "call_correlation", Consumers: []string{"application"}}},
	}
	var out bytes.Buffer
	printDevTelephonyPlan(&out, "phone", plan, public)
	for _, want := range []string{
		"provider=pipecat transport=carrier-websocket carrier=twilio coordination=shared",
		"POST https://voice.example.com/unmute/telephony/inbound",
		"WS wss://voice.example.com/unmute/telephony/ws/{token}",
		"setup: configure signed callbacks",
		"local services: application, redis",
		"coordination reason call_correlation -> application",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("plan missing %q:\n%s", want, out.String())
		}
	}
}

func TestLiveKitSIPDevPlanNeedsNoHTTPSCallbackURL(t *testing.T) { // telephony T10, V17
	plan := &generate.TelephonyRuntimePlan{
		Route:       ir.TelephonyKey{Provider: ir.ProviderLiveKit, Transport: "sip", Carrier: "twilio"},
		ManualSteps: []string{"expose SIP signaling and RTP"}, Coordination: "shared",
	}
	var out bytes.Buffer
	printDevTelephonyPlan(&out, "phone", plan, nil)
	if !strings.Contains(out.String(), "transport=sip") || !strings.Contains(out.String(), "expose SIP signaling and RTP") {
		t.Fatalf("LiveKit SIP plan =\n%s", out.String())
	}
}

func TestTelephonyDevRejectsUnsafePublicURLAndNamesMissingCredentials(t *testing.T) { // telephony V11
	for _, value := range []string{"", "http://voice.example.com", "https://user@example.com", "https://voice.example.com?host=wrong"} {
		if _, err := parseTelephonyPublicURL(value); err == nil {
			t.Errorf("parseTelephonyPublicURL(%q) passed", value)
		}
	}
	env := setChildEnv([]string{"TWILIO_ACCOUNT_SID=account", "TWILIO_AUTH_TOKEN="}, "UNMUTE_PUBLIC_URL", "https://voice.example.com")
	missing := missingEnvironment([]string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "UNMUTE_PUBLIC_URL"}, env)
	if strings.Join(missing, ",") != "TWILIO_AUTH_TOKEN" {
		t.Fatalf("missing environment = %v", missing)
	}
}

func TestComposePreflightFailsClearlyWhenDockerIsMissing(t *testing.T) { // telephony V24
	restore := composeLookPath
	composeLookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { composeLookPath = restore })
	err := preflightCompose(context.Background(), os.Environ())
	if err == nil || !strings.Contains(err.Error(), "docker compose is required") || !strings.Contains(err.Error(), "Docker Desktop") {
		t.Fatalf("preflight error = %v", err)
	}
}

func TestComposePreflightFailsClearlyWhenDaemonIsUnavailable(t *testing.T) { // telephony V24
	dir := t.TempDir()
	fake := filepath.Join(dir, "docker")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nif [ \"$1\" = fail ]; then echo daemon-down; exit 1; fi\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	restoreLook, restoreCommand := composeLookPath, composeCommand
	composeLookPath = func(string) (string, error) { return fake, nil }
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		mode := "ok"
		if len(args) > 0 && args[0] == "info" {
			mode = "fail"
		}
		return exec.CommandContext(ctx, fake, mode)
	}
	t.Cleanup(func() { composeLookPath, composeCommand = restoreLook, restoreCommand })
	err := preflightCompose(context.Background(), os.Environ())
	if err == nil || !strings.Contains(err.Error(), "docker daemon is unavailable") || !strings.Contains(err.Error(), "daemon-down") {
		t.Fatalf("preflight error = %v", err)
	}
}

func TestComposePlanIsProjectScopedAndPreservesVolumes(t *testing.T) { // telephony V24-V25
	project := composeProjectName("/tmp/My Agent!", "LiveKit Main")
	if !strings.HasPrefix(project, "unmute-my-agent--livekit-main-") {
		t.Fatalf("project = %q", project)
	}
	up := strings.Join(composeArgs("compose.telephony.yaml", project, "up", "--build", "--detach", "--wait"), " ")
	if !strings.Contains(up, "--project-name "+project) || !strings.Contains(up, "up --build --detach --wait") {
		t.Fatalf("up args = %q", up)
	}
	down := strings.Join(composeArgs("compose.telephony.yaml", project, "down", "--remove-orphans"), " ")
	if strings.Contains(down, "--volumes") || !strings.Contains(down, "--project-name "+project) {
		t.Fatalf("down args = %q", down)
	}
}

func TestComposeExecutorRunsUpLogsAndProjectScopedDown(t *testing.T) { // telephony V24
	dir := t.TempDir()
	trace := filepath.Join(dir, "trace.log")
	fake := filepath.Join(dir, "docker")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TRACE_FILE\"\ncase \"$*\" in *' logs '*) while :; do sleep 1; done;; esac\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	restore := composeCommand
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, fake, args...)
	}
	t.Cleanup(func() { composeCommand = restore })
	var output bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(time.Second, cancel)
	err := runTelephonyCompose(ctx, telephonyComposeRun{
		dir: dir, file: filepath.Join(dir, "compose.telephony.yaml"), project: "unmute-test",
		env: []string{"TRACE_FILE=" + trace}, output: &output, stdout: &output, stderr: &output,
		logPath: filepath.Join(dir, "telephony.log"),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	commands := string(raw)
	for _, want := range []string{
		"--project-name unmute-test up --build --detach --remove-orphans --wait",
		"--project-name unmute-test logs --follow --no-color",
		"--project-name unmute-test down --remove-orphans --timeout 30",
	} {
		if !strings.Contains(commands, want) {
			t.Errorf("commands missing %q:\n%s", want, commands)
		}
	}
	if strings.Contains(commands, "--volumes") {
		t.Fatalf("cleanup removed volumes:\n%s", commands)
	}
}

func TestComposeExecutorTreatsStartupInterruptAsCleanStop(t *testing.T) { // telephony V24
	dir := t.TempDir()
	fake := filepath.Join(dir, "docker")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\ncase \"$*\" in *' up '*) while :; do sleep 1; done;; esac\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	restore := composeCommand
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, fake, args...)
	}
	t.Cleanup(func() { composeCommand = restore })
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	var output bytes.Buffer
	err := runTelephonyCompose(ctx, telephonyComposeRun{
		dir: dir, file: filepath.Join(dir, "compose.telephony.yaml"), project: "unmute-test",
		output: &output, stdout: &output, stderr: &output,
		logPath: filepath.Join(dir, "telephony.log"),
	})
	if err != nil {
		t.Fatalf("startup interrupt returned an error: %v", err)
	}
	if !strings.Contains(output.String(), "stopping...") {
		t.Fatalf("startup interrupt did not report a clean stop: %s", output.String())
	}
}

func TestComposeExecutorTreatsLogInterruptAsCleanStop(t *testing.T) { // telephony V24
	dir := t.TempDir()
	fake := filepath.Join(dir, "docker")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\ncase \"$*\" in *' logs '*) kill -INT $$;; esac\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	restore := composeCommand
	composeCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, fake, args...)
	}
	t.Cleanup(func() { composeCommand = restore })
	var output bytes.Buffer
	err := runTelephonyCompose(context.Background(), telephonyComposeRun{
		dir: dir, file: filepath.Join(dir, "compose.telephony.yaml"), project: "unmute-test",
		output: &output, stdout: &output, stderr: &output,
		logPath: filepath.Join(dir, "telephony.log"),
	})
	if err != nil {
		t.Fatalf("log interrupt returned an error: %v", err)
	}
	if !strings.Contains(output.String(), "stopping...") {
		t.Fatalf("log interrupt did not report a clean stop: %s", output.String())
	}
}

func TestComposeLocalEnvironmentAndLiveKitConflicts(t *testing.T) { // telephony V24-V25
	plan := &generate.TelephonyRuntimePlan{
		Services:         []string{"application", "redis", "livekit_server", "livekit_sip"},
		RequiredEnv:      []string{"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "REDIS_URL", "TWILIO_SIP_PASSWORD"},
		LocalEnvironment: []string{"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "REDIS_URL"},
	}
	if got := externalTelephonyEnv(plan); strings.Join(got, ",") != "TWILIO_SIP_PASSWORD" {
		t.Fatalf("external env = %v", got)
	}
	if err := rejectLocalTopologyConflicts(plan, []string{"LIVEKIT_URL=wss://cloud.example"}); err == nil || !strings.Contains(err.Error(), "LIVEKIT_URL conflicts") {
		t.Fatalf("conflict = %v", err)
	}
	if err := rejectLocalTopologyConflicts(plan, []string{"LIVEKIT_URL="}); err != nil {
		t.Fatalf("empty local override should not conflict: %v", err)
	}
	pipecat := &generate.TelephonyRuntimePlan{
		RequiredEnv: []string{"REDIS_URL", "TWILIO_AUTH_TOKEN"}, LocalEnvironment: []string{"REDIS_URL"},
	}
	if got := externalTelephonyEnv(pipecat); strings.Join(got, ",") != "TWILIO_AUTH_TOKEN" {
		t.Fatalf("Pipecat external environment = %v", got)
	}
	if err := rejectLocalTopologyConflicts(pipecat, []string{"REDIS_URL=redis://external"}); err == nil || !strings.Contains(err.Error(), "REDIS_URL conflicts") {
		t.Fatalf("Pipecat local Redis conflict = %v", err)
	}
}

// No environment name carries a trunk ID any more (SCHEMA N36), so nothing is
// dev-supplied on this route: the dev command creates the records and the local
// LiveKit SIP service reads them itself. A stale retired value left in a `.env`
// is now simply ignored, which is what the README's retirement sentence tells the
// operator. The local topology guard still fails loud for the names the generated
// Compose owns, which is the case that would otherwise point a local run at
// someone's real deployment.
func TestComposeIgnoresRetiredTrunkNamesAndStillGuardsLocalTopology(t *testing.T) {
	plan := &generate.TelephonyRuntimePlan{
		RequiredEnv:      []string{"LIVEKIT_URL", "TWILIO_SIP_PASSWORD"},
		LocalEnvironment: []string{"LIVEKIT_URL"},
	}
	if got := externalTelephonyEnv(plan); strings.Join(got, ",") != "TWILIO_SIP_PASSWORD" {
		t.Fatalf("external env = %v", got)
	}
	for _, stale := range []string{"LIVEKIT_SIP_INBOUND_TRUNK=ST_stale", "LIVEKIT_SIP_OUTBOUND_TRUNK=ST_stale"} {
		if err := rejectLocalTopologyConflicts(plan, []string{stale}); err != nil {
			t.Fatalf("a retired name must be ignored, not reported: %v", err)
		}
	}
	err := rejectLocalTopologyConflicts(plan, []string{"LIVEKIT_URL=wss://production.example"})
	if err == nil || !strings.Contains(err.Error(), "LIVEKIT_URL conflicts") {
		t.Fatalf("local topology conflict = %v", err)
	}
}

// A provisional route is usable now: dev proceeds past validation instead of
// failing closed on the credentialed-smoke gate. It stops later (here on
// missing model credentials), never on the gate. Standalone --public-url still
// requires --telephony.
func TestDevTelephonyProvisionalRouteDoesNotFailClosed(t *testing.T) {
	dir := copySafeCore(t)
	routeSafeCore(t, dir, "carrier-websocket", "connector")
	agentPath := filepath.Join(dir, "agent.yaml")
	agentRaw, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	// The trailing blank line is part of the anchor: it pins `kind` as the last
	// key under `web:`, so adding a sibling key there fails this test loudly
	// instead of splicing `phone:` into the middle of the `web:` mapping.
	agentConfigured := mustReplace(t, string(agentRaw),
		"channels:\n  web:\n    kind: realtime_audio\n\n",
		"channels:\n  web:\n    kind: realtime_audio\n  phone:\n    kind: telephony\n    inbound: true\n    outbound: false\n    required_controls:\n      - cold_transfer\n      - hangup\n\n")
	if err := os.WriteFile(agentPath, []byte(agentConfigured), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PHONE_NUMBER"} {
		t.Setenv(name, "value")
	}
	_, err = run(t, "dev", dir, "--target", "pipecat", "--telephony", "--public-url", "https://voice.example.com")
	if err == nil || strings.Contains(err.Error(), "credentialed smoke") {
		t.Fatalf("provisional route must no longer fail closed at validation, got %v", err)
	}
	if _, err := run(t, "dev", dir, "--target", "pipecat", "--public-url", "https://voice.example.com"); err == nil || !strings.Contains(err.Error(), "requires --telephony") {
		t.Fatalf("standalone --public-url error = %v", err)
	}
}

func TestSelectDevTargetAutoSelectsSoleInstance(t *testing.T) {
	data := scaffold.Data{Name: "agent"}
	data.SetTarget("livekit")
	dir := filepath.Join(t.TempDir(), "agent")
	if _, err := scaffold.Write(dir, data); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCmd()
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&bytes.Buffer{})
	name, err := selectDevTarget(cmd, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "livekit" {
		t.Fatalf("selected target = %q", name)
	}
}

func TestSelectDevTargetRequiresNameForMultipleWithoutTTY(t *testing.T) {
	dir := copySafeCore(t)
	cmd := newRootCmd()
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&bytes.Buffer{})
	_, err := selectDevTarget(cmd, dir, "")
	if err == nil || !strings.Contains(err.Error(), "multiple targets declared; pass --target <name>") || !strings.Contains(err.Error(), "pipecat (pipecat)") {
		t.Fatalf("selectDevTarget() error = %v", err)
	}
}

// TestDevConsoleRemoved: the flag is gone, and passing it explains itself.
// It stays registered and hidden precisely so this message is possible: an
// author with `--console` in their shell history gets told the mode moved,
// not cobra's bare "unknown flag".
func TestDevConsoleRemoved(t *testing.T) {
	dir := copySafeCore(t)
	_, err := run(t, "dev", dir, "--target", "pipecat", "--console")
	if err == nil {
		t.Fatal("--console must fail; it was removed")
	}
	for _, want := range []string{"--console was removed", "Docker", "browser"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("removed-flag error %q must mention %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("removed-flag error fell through to cobra: %v", err)
	}
}

// T3/V3: --to requires --telephony and a valid E.164 value; both fail up front,
// before target selection.
func TestDevToFlagGuards(t *testing.T) {
	dir := copySafeCore(t)
	if _, err := run(t, "dev", dir, "--target", "pipecat", "--to", "+15551234567"); err == nil || !strings.Contains(err.Error(), "--to requires --telephony") {
		t.Fatalf("--to without --telephony error = %v", err)
	}
	if _, err := run(t, "dev", dir, "--target", "pipecat", "--telephony", "--to", "not-a-number"); err == nil || !strings.Contains(err.Error(), "E.164") {
		t.Fatalf("malformed --to error = %v", err)
	}
}

// T3/V6: --to on an inbound-only target errors before generate and any child
// process, naming the outbound requirement rather than the provisional gate.
func TestDevToRejectsInboundOnlyTarget(t *testing.T) {
	dir := copySafeCore(t)
	routeSafeCore(t, dir, "carrier-websocket", "connector")
	agentPath := filepath.Join(dir, "agent.yaml")
	agentRaw, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	// The trailing blank line is part of the anchor: it pins `kind` as the last
	// key under `web:`, so adding a sibling key there fails this test loudly
	// instead of splicing `phone:` into the middle of the `web:` mapping.
	agentConfigured := mustReplace(t, string(agentRaw),
		"channels:\n  web:\n    kind: realtime_audio\n\n",
		"channels:\n  web:\n    kind: realtime_audio\n  phone:\n    kind: telephony\n    inbound: true\n    outbound: false\n    required_controls:\n      - cold_transfer\n      - hangup\n\n")
	if err := os.WriteFile(agentPath, []byte(agentConfigured), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = run(t, "dev", dir, "--target", "pipecat", "--telephony", "--to", "+15551234567")
	if err == nil || !strings.Contains(err.Error(), "outbound-capable") {
		t.Fatalf("--to on inbound-only error = %v", err)
	}
}

// TestDevWebRejectsManagedProvider: a managed provider has no local dev runner
// and is refused before generation or any Docker preflight (SPEC I.dev).
func TestDevWebRejectsManagedProvider(t *testing.T) {
	dir := copySafeCore(t)
	_, err := run(t, "dev", dir, "--target", "vapi")
	if err == nil || !strings.Contains(err.Error(), "its dev runner is not implemented") {
		t.Fatalf("vapi dev error = %v", err)
	}
}

func TestSelectDevTargetRejectsUnknownInstance(t *testing.T) {
	dir := copySafeCore(t)
	cmd := newRootCmd()
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&bytes.Buffer{})
	_, err := selectDevTarget(cmd, dir, "missing")
	if err == nil || !strings.Contains(err.Error(), `target instance "missing" is not declared`) {
		t.Fatalf("selectDevTarget() error = %v", err)
	}
}

func contains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

// The route reaches the local flow rather than a refusal. Every other Pipecat
// carrier form refuses `--telephony` for its own true reason, so this asserts the
// absence of all of those messages as well as the presence of the real one: the
// failure an operator with no credentials should see is the credential list, not
// "this route cannot be run locally".
func TestDevTelephonyOnTheCloudWebsocketRouteReachesTheLocalFlow(t *testing.T) {
	dir := copySafeCore(t)
	// The phone channel below is package-wide, so the LiveKit target needs a route
	// too or the build refuses before this command's dispatch is ever reached.
	routeSafeCore(t, dir, "cloud-websocket", "connector")
	agentPath := filepath.Join(dir, "agent.yaml")
	agentRaw, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	agentConfigured := mustReplace(t, string(agentRaw),
		"channels:\n  web:\n    kind: realtime_audio\n\n",
		"channels:\n  web:\n    kind: realtime_audio\n  phone:\n    kind: telephony\n    inbound: true\n    outbound: false\n    required_controls:\n      - cold_transfer\n      - hangup\n\n")
	if err := os.WriteFile(agentPath, []byte(agentConfigured), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = run(t, "dev", dir, "--target", "pipecat", "--telephony", "--public-url", "https://voice.example.com")
	if err == nil {
		t.Fatal("with no carrier credentials the local flow must refuse")
	}
	message := err.Error()
	// The real failure, named.
	for _, want := range []string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PHONE_NUMBER"} {
		if !strings.Contains(message, want) {
			t.Errorf("the refusal does not name %q:\n%s", want, message)
		}
	}
	// Every message that would mean the command never got as far as trying.
	for _, forbidden := range []string{
		"no resolved telephony route", "no executable telephony topology",
		"no local telephony topology", "cannot run it for you", "telephony_helper.py",
		"credentialed smoke", "not supported", "unsupported",
	} {
		if strings.Contains(message, forbidden) {
			t.Errorf("the route refused instead of running: %q appears in\n%s", forbidden, message)
		}
	}
}

// FR-018a. `channels.web` is not required for the browser path, and the two
// phone-only examples prove it: neither declares one, and neither has to.
// Without this, "make the browser path work" could be satisfied by telling
// every author to add a channel they do not otherwise need.
func TestBrowserPathNeedsNoWebChannel(t *testing.T) {
	for _, example := range []string{"livekit-human-transfer", "pipecat-human-transfer-twilio"} {
		t.Run(example, func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join("..", "..", "examples", example))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := pkg.Agent.Channels["web"]; ok {
				t.Fatalf("%s declares channels.web, so it no longer holds this line", example)
			}
			var telephony bool
			for _, channel := range pkg.Agent.Channels {
				if channel.Kind == "telephony" {
					telephony = true
				}
			}
			if !telephony {
				t.Fatalf("%s declares no telephony channel, so it is the wrong fixture", example)
			}
			if _, err := ir.Build(pkg); err != nil {
				t.Fatalf("a phone-only package must build for the browser path: %v", err)
			}
		})
	}
}

// dev takes the same optional package directory as validate and compile
// (contracts C7 and C8). These prove the resolution without starting Docker:
// both assertions land on checks that run immediately after the directory is
// resolved, so reaching them at all means the zero-argument form worked.

func TestDevWithNoArgumentResolvesTheCurrentDirectory(t *testing.T) {
	t.Chdir(copyPackage(t, "remy"))
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	// --public-url without --telephony is rejected just after the directory is
	// resolved, so this error means dev accepted the zero-argument form.
	cmd.SetArgs([]string{"dev", "--public-url", "https://example.test"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--public-url requires --telephony") {
		t.Fatalf("dev with no directory did not reach its flag checks: %v", err)
	}
}

func TestDevWithNoArgumentOutsideAPackageExplainsItself(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"dev"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("dev outside a package must fail")
	}
	if !strings.Contains(err.Error(), "agent.yaml") || strings.Contains(err.Error(), "accepts 1 arg") {
		t.Fatalf("dev must explain itself, not print the cobra arity error: %v", err)
	}
}
