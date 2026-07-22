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

	"github.com/slng/unmute/internal/generate"
	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/scaffold"
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

func TestDevChildEnv_readsDotenv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SLNG_API_KEY=sk-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := devChildEnv(dir, &bytes.Buffer{})
	if !contains(env, "SLNG_API_KEY=sk-secret") {
		t.Errorf(".env value not passed to the child env; env = %v", env)
	}
}

func TestV14DevChildEnvReadsWorkingDirectoryThenPackageDotenv(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "examples", "agent")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte(
		"UNMUTE_TEST_REPO_ENV=repo\nUNMUTE_TEST_SHARED_ENV=repo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(
		"UNMUTE_TEST_PACKAGE_ENV=package\nUNMUTE_TEST_SHARED_ENV=package\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	env := devChildEnv(root, &bytes.Buffer{})
	for _, want := range []string{
		"UNMUTE_TEST_REPO_ENV=repo",
		"UNMUTE_TEST_PACKAGE_ENV=package",
		"UNMUTE_TEST_SHARED_ENV=package",
	} {
		if !contains(env, want) {
			t.Errorf("dev child env missing %q", want)
		}
	}
}

func TestDevChildEnv_missingFileIsFine(t *testing.T) {
	var warn bytes.Buffer
	env := devChildEnv(t.TempDir(), &warn) // no .env present
	if len(env) == 0 {
		t.Fatal("expected the ambient environment when .env is absent")
	}
	if warn.Len() != 0 {
		t.Errorf("a missing .env must not warn; got %q", warn.String())
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

// Trunk IDs are supplied by the dev command itself (SPEC V4): they are never
// demanded from the user and a user-set value is rejected, not overridden.
func TestComposeDevSuppliedEnvironmentIsNeverDemandedAndRejectsOverrides(t *testing.T) {
	plan := &generate.TelephonyRuntimePlan{
		RequiredEnv:      []string{"LIVEKIT_SIP_INBOUND_TRUNK", "LIVEKIT_SIP_OUTBOUND_TRUNK", "LIVEKIT_URL", "TWILIO_SIP_PASSWORD"},
		LocalEnvironment: []string{"LIVEKIT_URL"},
		DevSuppliedEnv:   []string{"LIVEKIT_SIP_INBOUND_TRUNK", "LIVEKIT_SIP_OUTBOUND_TRUNK"},
	}
	if got := externalTelephonyEnv(plan); strings.Join(got, ",") != "TWILIO_SIP_PASSWORD" {
		t.Fatalf("external env = %v", got)
	}
	err := rejectLocalTopologyConflicts(plan, []string{"LIVEKIT_SIP_INBOUND_TRUNK=ST_stale"})
	if err == nil || !strings.Contains(err.Error(), "LIVEKIT_SIP_INBOUND_TRUNK is supplied by `unmute dev --telephony` itself") {
		t.Fatalf("dev-supplied override = %v", err)
	}
	if err := rejectLocalTopologyConflicts(plan, []string{"LIVEKIT_SIP_OUTBOUND_TRUNK="}); err != nil {
		t.Fatalf("empty dev-supplied override should not conflict: %v", err)
	}
}

func TestDevTelephonyReportsProvisionalRouteBeforeConfiguration(t *testing.T) { // telephony V11, V17, B12
	restore := composePreflight
	preflightCalled := false
	composePreflight = func(context.Context, []string) error {
		preflightCalled = true
		return errors.New("Docker must not be checked for a validation-red route")
	}
	t.Cleanup(func() { composePreflight = restore })
	dir := copySafeCore(t)
	targetsPath := filepath.Join(dir, "targets.yaml")
	raw, err := os.ReadFile(targetsPath)
	if err != nil {
		t.Fatal(err)
	}
	configured := strings.Replace(string(raw),
		"    transport: daily-sip        # cold_transfer needs Daily SIP on Pipecat",
		"    transport: carrier-websocket\n    carrier: twilio\n    connection: primary_phone", 1)
	configured = strings.Replace(configured,
		"    sdk_language: python\n    models:",
		"    sdk_language: python\n    transport: connector\n    carrier: twilio\n    connection: primary_phone\n    models:", 1)
	if configured == string(raw) {
		t.Fatal("pipecat target fixture did not change")
	}
	if err := os.WriteFile(targetsPath, []byte(configured), 0o600); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(dir, "agent.yaml")
	agentRaw, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	agentConfigured := strings.Replace(string(agentRaw),
		"channels:\n  web: { kind: realtime_audio }",
		"channels:\n  web: { kind: realtime_audio }\n  phone:\n    kind: telephony\n    inbound: true\n    outbound: false\n    required_controls: [cold_transfer, hangup]", 1)
	if agentConfigured == string(agentRaw) {
		t.Fatal("agent channel fixture did not change")
	}
	if err := os.WriteFile(agentPath, []byte(agentConfigured), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PHONE_NUMBER"} {
		t.Setenv(name, "")
	}
	out, err := run(t, "dev", dir, "--target", "pipecat", "--telephony")
	if err == nil || !strings.Contains(err.Error(), "has not passed its credentialed smoke") {
		t.Fatalf("route error = %v", err)
	}
	for _, laterGate := range []string{"--public-url", "TWILIO_ACCOUNT_SID", "Docker"} {
		if strings.Contains(err.Error(), laterGate) {
			t.Errorf("route error reached later gate %q: %v", laterGate, err)
		}
	}
	if preflightCalled {
		t.Fatal("Docker preflight ran before telephony route validation")
	}
	if out != "" {
		t.Fatalf("validation-red route printed an executable plan:\n%s", out)
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

// TestDevConsoleAndTelephonyRejected: console (native, host audio) and
// telephony (containerized) are mutually exclusive and refused up front,
// before any generation or Docker (SPEC V7).
func TestDevConsoleAndTelephonyRejected(t *testing.T) {
	dir := copySafeCore(t)
	_, err := run(t, "dev", dir, "--target", "pipecat", "--console", "--telephony")
	if err == nil || !strings.Contains(err.Error(), "--console and --telephony cannot be used together") {
		t.Fatalf("console+telephony error = %v", err)
	}
}

// TestDevWebRejectsManagedProvider: a managed provider has no local dev runner
// and is refused before generation or any Docker preflight (SPEC I.dev).
func TestDevWebRejectsManagedProvider(t *testing.T) {
	dir := copySafeCore(t)
	_, err := run(t, "dev", dir, "--target", "elevenlabs")
	if err == nil || !strings.Contains(err.Error(), `target "elevenlabs" uses managed ElevenLabs`) || !strings.Contains(err.Error(), "unmute apply") {
		t.Fatalf("elevenlabs dev error = %v", err)
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

func TestConsolePlan(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		want     string // space-joined uv args
		errSub   string
	}{
		{ir.ProviderPipecat, "run --extra console bot.py console", ""},
		{ir.ProviderLiveKit, "run agent.py console", ""},
		{ir.ProviderElevenLabs, "", "unmute apply"},
		{ir.ProviderVapi, "", "not implemented"},
	} {
		got, err := consolePlan(tc.provider)
		if tc.errSub != "" {
			if err == nil || !strings.Contains(err.Error(), tc.errSub) {
				t.Errorf("consolePlan(%s) err = %v, want contains %q", tc.provider, err, tc.errSub)
			}
			continue
		}
		if err != nil {
			t.Fatalf("consolePlan(%s): %v", tc.provider, err)
		}
		if strings.Join(got, " ") != tc.want {
			t.Errorf("consolePlan(%s) = %v, want %q", tc.provider, got, tc.want)
		}
	}
}

func TestRequireInferenceCreds(t *testing.T) {
	// Hermetic: force the ambient LiveKit creds empty so the machine's real
	// env can't mask the missing case.
	t.Setenv("LIVEKIT_API_KEY", "")
	t.Setenv("LIVEKIT_API_SECRET", "")
	uses := []string{`reason provider "livekit"`}

	dir := t.TempDir()
	err := requireInferenceCreds(dir, uses)
	if err == nil || !strings.Contains(err.Error(), "LIVEKIT_API_KEY") ||
		!strings.Contains(err.Error(), "LIVEKIT_API_SECRET") || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("missing-creds error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, ".env"),
		[]byte("LIVEKIT_API_KEY=k\nLIVEKIT_API_SECRET=s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireInferenceCreds(dir, uses); err != nil {
		t.Errorf("creds present in .env, want nil; got %v", err)
	}
}

func TestDevConsoleRefusesManaged(t *testing.T) {
	dir := copySafeCore(t)
	_, err := run(t, "dev", dir, "--target", "elevenlabs", "--console")
	if err == nil || !strings.Contains(err.Error(), "managed ElevenLabs") || !strings.Contains(err.Error(), "unmute apply") {
		t.Fatalf("elevenlabs console error = %v", err)
	}
}

// TestDevConsoleRoutesRegardlessOfWebFlags: --console takes over the dispatch,
// and the web-only flags are inert. A vapi target gives the console-specific
// "console mode is not implemented" (not the web path's "dev runner is not
// implemented"), even with --port passed, proving the route and the inertness.
func TestDevConsoleRoutesRegardlessOfWebFlags(t *testing.T) {
	dir := copySafeCore(t)
	_, err := run(t, "dev", dir, "--target", "vapi", "--console", "--port", "1", "--no-open")
	if err == nil || !strings.Contains(err.Error(), "console mode is not implemented") {
		t.Fatalf("vapi console route error = %v", err)
	}
	// The web path for the same target uses the other wording.
	_, err = run(t, "dev", dir, "--target", "vapi")
	if err == nil || !strings.Contains(err.Error(), "its dev runner is not implemented") {
		t.Fatalf("vapi web route error = %v", err)
	}
}

// TestDevConsoleLiveKitInferenceRequiresCreds (V7, C7): a livekit console target
// that routes a role through LiveKit Inference fails the preflight naming the
// missing creds and the reason, before the TUI launches. Flips a scaffolded
// agent's reason binding to provider: livekit (the Inference wildcard).
func TestDevConsoleLiveKitInferenceRequiresCreds(t *testing.T) {
	t.Setenv("LIVEKIT_API_KEY", "")
	t.Setenv("LIVEKIT_API_SECRET", "")
	data := scaffold.Data{Name: "agent"}
	data.SetTarget("livekit")
	dir := filepath.Join(t.TempDir(), "agent")
	if _, err := scaffold.Write(dir, data); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(dir, "agent.yaml")
	raw, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	flipped := strings.ReplaceAll(string(raw), "provider: openai", "provider: livekit")
	if flipped == string(raw) {
		t.Fatal("expected an openai reason binding to flip to livekit inference")
	}
	if err := os.WriteFile(agentPath, []byte(flipped), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = run(t, "dev", dir, "--target", "livekit", "--console")
	if err == nil || !strings.Contains(err.Error(), "LIVEKIT_API_KEY") || !strings.Contains(err.Error(), "Inference") {
		t.Fatalf("console inference preflight error = %v", err)
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
