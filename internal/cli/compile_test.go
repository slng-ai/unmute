package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
)

// TestWriteArtifactFilesFormatsPython: the write path runs a best-effort
// `ruff format` over emitted .py so the on-disk project is format-stable, and
// leaves non-Python files untouched (SPEC V2). Skips when ruff is absent.
func TestWriteArtifactFilesFormatsPython(t *testing.T) {
	if _, err := exec.LookPath("ruff"); err != nil {
		t.Skip("ruff not installed")
	}
	dir := t.TempDir()
	ugly := "x  =  {  'a':1 }\n\n\n\ndef f( ):\n    return   x\n"
	readme := "# keep  this  as-is\n"
	files := []generate.File{
		{Path: "bot.py", Content: []byte(ugly)},
		{Path: "README.md", Content: []byte(readme)},
	}
	if err := writeArtifactFiles(nil, dir, files); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "bot.py"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == ugly {
		t.Error("bot.py was written unformatted (ruff format pass did not run)")
	}
	// Format-stable: a second `ruff format` produces no diff.
	cmd := exec.Command("ruff", "format", "--diff", "-")
	cmd.Stdin = bytes.NewReader(got)
	if out, _ := cmd.CombinedOutput(); len(bytes.TrimSpace(out)) != 0 {
		t.Errorf("written bot.py is not ruff-format-stable:\n%s", out)
	}
	// Non-Python files are copied verbatim.
	if md, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(md) != readme {
		t.Errorf("README.md was modified: %q", md)
	}
}

func TestV15WriteArtifactFilesPreservesDotenv(t *testing.T) {
	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	const secret = "OPENAI_API_KEY=keep-me\n"
	if err := os.WriteFile(dotenv, []byte(secret), 0o660); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dotenv, 0o660); err != nil {
		t.Fatal(err)
	}

	if err := writeArtifactFiles(nil, dir, []generate.File{{Path: "README.md", Content: []byte("generated\n")}}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dotenv)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != secret {
		t.Fatalf(".env changed during artifact rewrite: %q", got)
	}
	if info, err := os.Stat(dotenv); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o660 {
		t.Fatalf(".env mode = %o, want 660", info.Mode().Perm())
	}
}

func TestV15WriteArtifactFilesRestoresDotenvAfterFailure(t *testing.T) {
	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	const secret = "OPENAI_API_KEY=keep-me\n"
	if err := os.WriteFile(dotenv, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}

	err := writeArtifactFiles(nil, dir, []generate.File{{Path: "bad\x00path", Content: []byte("generated\n")}})
	if err == nil {
		t.Fatal("artifact rewrite unexpectedly succeeded")
	}
	got, readErr := os.ReadFile(dotenv)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != secret {
		t.Fatalf(".env changed after failed artifact rewrite: %q", got)
	}
}

// copySafeCore copies the example package into a temp dir so compile can write
// its build/ output without polluting the repo.
func copySafeCore(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "agent")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "safe_core"))); err != nil {
		t.Fatal(err)
	}
	return dir
}

// mustReplace applies one fixture substitution and fails when the anchor is
// gone. Every call carries its own guard on purpose: a single guard covering
// several replacements passes as soon as any one of them lands, so a fixture
// edit that stales one anchor patches the package halfway and the test then
// asserts against a shape nobody intended.
//
// Anchors should be self-terminating (end on a blank line or a closing token)
// rather than a prefix of an open block. A prefix still matches after someone
// appends a key to that block, and the replacement then splices into the middle
// of it. Failing loudly here is always better than editing the wrong place.
func mustReplace(t *testing.T, src, old, new string) string {
	t.Helper()
	out := strings.Replace(src, old, new, 1)
	if out == src {
		t.Fatalf("fixture anchor not found, the fixture moved under this test: %q", old)
	}
	return out
}

func TestPrintTelephonyPlanUsesArtifactWithoutCarrierDispatch(t *testing.T) {
	plan := &generate.TelephonyRuntimePlan{
		Route:       ir.TelephonyKey{Provider: ir.ProviderPipecat, Transport: "carrier-websocket", Carrier: "twilio"},
		RequiredEnv: []string{"TWILIO_AUTH_TOKEN"}, Coordination: "local", AdmissionOwner: "generated_runtime",
	}
	var out bytes.Buffer
	printTelephonyPlan(&out, "phone", plan)
	for _, want := range []string{"provider=pipecat", "transport=carrier-websocket", "carrier=twilio", "required env TWILIO_AUTH_TOKEN"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %s", want, out.String())
		}
	}
}

func runCompileCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := newRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"compile"}, args...))
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// gap #2: forwarded bindings + derived sizing reach compile stdout and the
// compile-report.json on disk — SCHEMA.md §6.2 rule 6, §5.1 ("the contract").
func TestCompilePrintsBindingsAndSizing(t *testing.T) {
	dir := copySafeCore(t)
	stdout, _, err := runCompileCommand(t, "--target", "pipecat", dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"pipecat: binding listen provider=deepgram model=nova-3 (forwarded as-is, not validated)",
		"pipecat:   param temperature=0.4",
		"pipecat: sizing workers=60 [unbenchmarked]",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}

	report, err := os.ReadFile(filepath.Join(dir, "build", "pipecat", "compile-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"bindings"`, `"sizing"`, `"nova-3"`, `"unbenchmarked"`} {
		if !strings.Contains(string(report), want) {
			t.Errorf("compile-report.json missing %q:\n%s", want, report)
		}
	}
}

func TestPrintContractExplainsOpenAIResponsesLowering(t *testing.T) {
	notes := generate.GenerateReport{ForwardedBindings: []ir.ForwardedBinding{{
		Target:  "livekit",
		Role:    "reason",
		Profile: "reasoning",
		Binding: ir.Binding{
			Provider: "openai",
			Model:    "gpt-5.6-terra",
			Params: map[string]any{
				"api":              "responses",
				"reasoning_effort": "low",
				"use_websocket":    false,
			},
		},
		Params: []ir.ForwardedParam{
			{Name: "api", Value: "responses"},
			{Name: "reasoning_effort", Value: "low"},
			{Name: "use_websocket", Value: false},
		},
	}}}

	var out bytes.Buffer
	printContract(&out, "livekit", ir.ProviderLiveKit, notes)
	for _, want := range []string{
		"OpenAI Responses mode; api is consumed and reasoning_effort is lowered when present",
		"param api=responses (compiler directive)",
		"param reasoning_effort=low (lowered to reasoning.effort)",
		"param use_websocket=false (forwarded as-is, not validated)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "binding reason.reasoning provider=openai model=gpt-5.6-terra (forwarded as-is, not validated)") {
		t.Errorf("Responses binding is falsely reported as wholly forwarded:\n%s", out.String())
	}

	notes.ForwardedBindings[0].Binding.Params = map[string]any{"api": "responses"}
	notes.ForwardedBindings[0].Params = []ir.ForwardedParam{{Name: "api", Value: "responses"}}
	out.Reset()
	printContract(&out, "livekit", ir.ProviderLiveKit, notes)
	if !strings.Contains(out.String(), "reasoning_effort is lowered when present") || strings.Contains(out.String(), "param reasoning_effort") {
		t.Errorf("default reasoning report is inaccurate:\n%s", out.String())
	}
}

// gap #1: a gated target surfaces the provider-vocabulary diagnostic on the
// compile path, not just "validation failed for N target(s)".
func TestCompileSurfacesPerTargetDiagnostics(t *testing.T) {
	dir := copySafeCore(t)
	path := filepath.Join(dir, "agent.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = bytes.Replace(content, []byte("  max_duration: 20m"), []byte("  max_duration: 20m\n  thinking_audio: subtle"), 1)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	// The diagnostic this surfaced was Vapi's thinking-audio gate. That target
	// is retired; naming it now fails as an undeclared instance, which is still
	// compile reporting a per-target problem rather than a bare exit code.
	_, _, err = runCompileCommand(t, "--target", "vapi", dir)
	if err == nil || !strings.Contains(err.Error(), `target instance "vapi" is not declared`) {
		t.Fatalf("err = %v", err)
	}
}

// gap #3: a package with zero target instances fails the same way validate does.
func TestCompileFailsWithNoTargets(t *testing.T) {
	dir := copySafeCore(t)
	if err := os.WriteFile(filepath.Join(dir, "targets.yaml"), []byte("targets: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := runCompileCommand(t, dir)
	if err == nil || !strings.Contains(err.Error(), "no targets selected") {
		t.Fatalf("err = %v", err)
	}
}

// LiveKit Cloud writes livekit.toml into the build directory on the first
// deploy, naming the project and the assigned agent. A recompile that destroyed
// it would break `lk agent deploy` and push the operator back to
// `lk agent create`, which registers a second billable agent.
func TestWriteArtifactFilesPreservesPlatformConfig(t *testing.T) {
	dir := t.TempDir()
	written := map[string]string{
		".env":                    "OPENAI_API_KEY=keep-me\n",
		"livekit.toml":            "[project]\n  subdomain = \"my-project\"\n\n[agent]\n  id = \"CA_abc123\"\n",
		"livekit.us-east.toml":    "[project]\n  subdomain = \"my-project\"\n\n[agent]\n  id = \"CA_east\"\n",
		"livekit.eu-central.toml": "[project]\n  subdomain = \"my-project\"\n\n[agent]\n  id = \"CA_central\"\n",
	}
	for name, content := range written {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A file the emitter does own must still be replaced.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeArtifactFiles(nil, dir, []generate.File{{Path: "README.md", Content: []byte("generated\n")}}); err != nil {
		t.Fatal(err)
	}
	for name, want := range written {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s did not survive the rewrite: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s changed during the rewrite:\n%s", name, got)
		}
	}
	if got, err := os.ReadFile(filepath.Join(dir, "README.md")); err != nil || string(got) != "generated\n" {
		t.Errorf("README.md = %q (err %v), want the regenerated content", got, err)
	}
}

// FR-004 and the Principle II precedent the Responses branch set: a value the
// compiler consumes rather than forwards is named in the report, so nothing is
// substituted silently. Here that is two things, the region and the whole
// upstream block.
//
// The second half is the constitution's rule that no report holds a secret
// value: the upstream line names each credential *variable* and never reads it.
func TestPrintContractNamesTheRouterRegionAndUpstream(t *testing.T) {
	notes := generate.GenerateReport{ForwardedBindings: []ir.ForwardedBinding{{
		Target:  "pipecat",
		Role:    "reason",
		Profile: "reasoning",
		Binding: ir.Binding{
			Provider: "slng",
			Model:    "gpt-5.6-luna",
			AgentID:  "salon-concierge-v1",
			Upstream: &ir.Upstream{Provider: "openai"},
			Params: map[string]any{
				"world_part_override": "eu",
				"reasoning_effort":    "none",
			},
		},
		Params: []ir.ForwardedParam{
			{Name: "reasoning_effort", Value: "none"},
			{Name: "world_part_override", Value: "eu"},
		},
	}}}

	var out bytes.Buffer
	printContract(&out, "pipecat", ir.ProviderPipecat, notes)
	for _, want := range []string{
		"binding reason.reasoning provider=slng model=gpt-5.6-luna (SLNG Context Router; world_part_override is consumed into base_url=https://eu.context-router.slng.ai/v1)",
		"param world_part_override=eu (consumed as the router base URL)",
		"upstream openai url=https://api.openai.com/v1 api_key=OPENAI_API_KEY (env) (sent inline in the request body)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "(forwarded as-is, not validated)") {
		t.Errorf("a router binding is falsely reported as wholly forwarded:\n%s", out.String())
	}

	// An author-named credential is still named and never read. The test sets a
	// value in the environment so a report that read one would show it.
	t.Setenv("HOST_LLM_KEY", "sk-live-not-a-real-key")
	notes.ForwardedBindings[0].Binding.Upstream = &ir.Upstream{
		Provider: "openai-compat", URL: "https://host/v1", KeyEnv: "HOST_LLM_KEY",
	}
	out.Reset()
	printContract(&out, "pipecat", ir.ProviderPipecat, notes)
	if !strings.Contains(out.String(), "upstream openai-compat url=https://host/v1 api_key=HOST_LLM_KEY (env)") {
		t.Errorf("report does not name the author's credential variable:\n%s", out.String())
	}
	if strings.Contains(out.String(), "sk-live-not-a-real-key") {
		t.Errorf("the report read a credential value:\n%s", out.String())
	}
}
