package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	for _, want := range []string{"--no-open", "--bot-port", "--target", "talk to it"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dev --help missing %q; got:\n%s", want, out.String())
		}
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
	if name != "livekit-dev" {
		t.Fatalf("selected target = %q", name)
	}
}

func TestSelectDevTargetRequiresNameForMultipleWithoutTTY(t *testing.T) {
	dir := copySafeCore(t)
	cmd := newRootCmd()
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&bytes.Buffer{})
	_, err := selectDevTarget(cmd, dir, "")
	if err == nil || !strings.Contains(err.Error(), "multiple targets declared; pass --target <name>") || !strings.Contains(err.Error(), "pipecat-dev (pipecat)") {
		t.Fatalf("selectDevTarget() error = %v", err)
	}
}

func TestCompileTargetForDevUsesExactInstance(t *testing.T) {
	dir := copySafeCore(t)
	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	outDir, err := compileTargetForDev(cmd, dir, "pipecat-dev")
	if err != nil {
		t.Fatal(err)
	}
	if outDir != filepath.Join(dir, "build", "pipecat-dev") {
		t.Fatalf("outDir = %q", outDir)
	}
	if _, err := os.Stat(filepath.Join(outDir, "bot.py")); err != nil {
		t.Fatal(err)
	}
}

func TestDevSelectedTargetReportsProviderSpecificRunner(t *testing.T) {
	dir := copySafeCore(t)
	// livekit web mode now runs, gated on LiveKit creds: with none present it
	// fails the preflight and points at --console (C7). Force the ambient creds
	// empty so the machine's real env can't satisfy the preflight.
	t.Setenv("LIVEKIT_URL", "")
	t.Setenv("LIVEKIT_API_KEY", "")
	t.Setenv("LIVEKIT_API_SECRET", "")
	_, err := run(t, "dev", dir, "--target", "livekit-dev")
	if err == nil || !strings.Contains(err.Error(), "livekit web mode needs") || !strings.Contains(err.Error(), "--console") {
		t.Fatalf("livekit web preflight error = %v", err)
	}
	_, err = run(t, "dev", dir, "--target", "elevenlabs-prod")
	if err == nil || !strings.Contains(err.Error(), `target "elevenlabs-prod" uses managed ElevenLabs`) || !strings.Contains(err.Error(), "unmute apply") {
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
	_, err := run(t, "dev", dir, "--target", "elevenlabs-prod", "--console")
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
	_, err := run(t, "dev", dir, "--target", "vapi-prod", "--console", "--port", "1", "--no-open")
	if err == nil || !strings.Contains(err.Error(), "console mode is not implemented") {
		t.Fatalf("vapi console route error = %v", err)
	}
	// The web path for the same target uses the other wording.
	_, err = run(t, "dev", dir, "--target", "vapi-prod")
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
	tgtPath := filepath.Join(dir, "targets.yaml")
	raw, err := os.ReadFile(tgtPath)
	if err != nil {
		t.Fatal(err)
	}
	flipped := strings.ReplaceAll(string(raw), "provider: openai", "provider: livekit")
	if flipped == string(raw) {
		t.Fatal("expected an openai reason binding to flip to livekit inference")
	}
	if err := os.WriteFile(tgtPath, []byte(flipped), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = run(t, "dev", dir, "--target", "livekit-dev", "--console")
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
