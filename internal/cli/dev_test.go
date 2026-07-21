package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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
	path := filepath.Join(dir, ".env.local")
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
	for _, want := range []string{"--no-open", "--bot-port", "--target", "--telephony", "--public-url", "talk to it"} {
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
		ManualSteps: []string{"configure signed callbacks"}, Coordination: "local",
	}
	var out bytes.Buffer
	printDevTelephonyPlan(&out, "phone", plan, public)
	for _, want := range []string{
		"provider=pipecat transport=carrier-websocket carrier=twilio coordination=local",
		"POST https://voice.example.com/unmute/telephony/inbound",
		"WS wss://voice.example.com/unmute/telephony/ws/{token}",
		"setup: configure signed callbacks",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("plan missing %q:\n%s", want, out.String())
		}
	}
	if got := strings.Join(devTelephonyCommand([]string{"uv", "run", "uvicorn", "telephony:app", "--port", "7860"}, "9000"), " "); got != "uv run uvicorn telephony:app --port 9000" {
		t.Fatalf("dev command = %q", got)
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

func TestWaitHTTPReadyRequiresSuccessfulReadiness(t *testing.T) { // telephony V16
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	done := make(chan error)
	if err := waitHTTPReady(context.Background(), server.URL, done, time.Second); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	exited <- errors.New("boom")
	if err := waitHTTPReady(context.Background(), "http://127.0.0.1:1/readyz", exited, time.Second); err == nil || !strings.Contains(err.Error(), "exited before ready") {
		t.Fatalf("got %v", err)
	}
}

func TestDevTelephonyPreflightStopsBeforeCredentialsOrProvisionalRoute(t *testing.T) { // telephony V11, V17
	dir := copySafeCore(t)
	targetsPath := filepath.Join(dir, "targets.yaml")
	raw, err := os.ReadFile(targetsPath)
	if err != nil {
		t.Fatal(err)
	}
	configured := strings.Replace(string(raw),
		"    transport: daily-sip        # cold_transfer needs Daily SIP on Pipecat",
		"    transport: carrier-websocket\n    carrier: twilio\n    connection: primary_phone", 1)
	if configured == string(raw) {
		t.Fatal("pipecat target fixture did not change")
	}
	if err := os.WriteFile(targetsPath, []byte(configured), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PHONE_NUMBER"} {
		t.Setenv(name, "")
	}
	out, err := run(t, "dev", dir, "--target", "pipecat", "--telephony", "--public-url", "https://voice.example.com")
	if err == nil || !strings.Contains(err.Error(), "TWILIO_ACCOUNT_SID") || !strings.Contains(err.Error(), "TELEPHONY.md#credentials") {
		t.Fatalf("preflight error = %v", err)
	}
	if !strings.Contains(out, "wss://voice.example.com/telephony/ws/{token}") || !strings.Contains(out, "Twilio Console account dashboard") {
		t.Fatalf("setup output =\n%s", out)
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

func TestCompileTargetForDevUsesExactInstance(t *testing.T) {
	dir := copySafeCore(t)
	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	outDir, err := compileTargetForDev(cmd, dir, "pipecat")
	if err != nil {
		t.Fatal(err)
	}
	if outDir != filepath.Join(dir, "build", "pipecat") {
		t.Fatalf("outDir = %q", outDir)
	}
	if _, err := os.Stat(filepath.Join(outDir, "bot.py")); err != nil {
		t.Fatal(err)
	}
}

func TestDevSelectedTargetReportsProviderSpecificRunner(t *testing.T) {
	dir := copySafeCore(t)
	// livekit web with no URL now tries the local dev-server fallback; force
	// both probes negative (no server on :7880, no binary) so the machine's
	// real state can't change the branch (V10), and force the ambient creds
	// empty. Expect the C7 install prompt pointing at --console.
	t.Setenv("LIVEKIT_URL", "")
	t.Setenv("LIVEKIT_API_KEY", "")
	t.Setenv("LIVEKIT_API_SECRET", "")
	restorePort, restoreLook := liveKitPortProbe, liveKitLookPath
	liveKitPortProbe = func(string) bool { return false }
	liveKitLookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { liveKitPortProbe, liveKitLookPath = restorePort, restoreLook })

	_, err := run(t, "dev", dir, "--target", "livekit")
	if err == nil || !strings.Contains(err.Error(), "brew install livekit") || !strings.Contains(err.Error(), "--console") {
		t.Fatalf("livekit web install-prompt error = %v", err)
	}
	_, err = run(t, "dev", dir, "--target", "elevenlabs")
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
