package cli

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// routeSafeCore puts the pipecat and livekit targets on real carrier routes.
//
// It writes one connection file per transport, because a connection declares
// its own transport and these two targets never share one. That is the whole
// authoring change in miniature: the targets each name a file, and the files
// carry the route (spec FR-001, FR-008a).
func routeSafeCore(t *testing.T, dir, pipecatTransport, livekitTransport string) {
	t.Helper()
	write := func(name, transport string, environment map[string]string) {
		body := "transport: " + transport + "\ncarrier: twilio\nenvironment:\n"
		for _, key := range slices.Sorted(maps.Keys(environment)) {
			body += "  " + key + ": " + environment[key] + "\n"
		}
		path := filepath.Join(dir, "connections", name+".yaml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	twilioAPI := map[string]string{
		"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN",
		"from_number": "TWILIO_PHONE_NUMBER",
	}
	write("primary_phone", pipecatTransport, twilioAPI)
	// The LiveKit instance already names twilio_sip, because cold transfer needs
	// a route there (SCHEMA N31). Rewriting that file rather than adding a second
	// one keeps the fixture free of a connection no target names, which is its
	// own warning.
	write("twilio_sip", livekitTransport, twilioAPI)

	path := filepath.Join(dir, "targets.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	configured := mustReplace(t, string(raw),
		"    connection: daily_provisioned   # cold_transfer needs Daily SIP on Pipecat",
		"    connection: primary_phone")
	if err := os.WriteFile(path, []byte(configured), 0o600); err != nil {
		t.Fatal(err)
	}
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

func TestCompileSLNGRouterReportsEnvironmentNamesWithoutSecretValues(t *testing.T) {
	t.Setenv("SLNG_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	const routerValue = "router-secret-must-not-leak"
	const upstreamValue = "upstream-secret-must-not-leak"
	for _, target := range []string{"livekit", "pipecat"} {
		t.Run(target, func(t *testing.T) {
			dir := copySLNGRouter(t)
			if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(
				"SLNG_API_KEY="+routerValue+"\nOPENAI_API_KEY="+upstreamValue+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := runCompileCommand(t, "--target", target, dir)
			if err != nil {
				t.Fatalf("compile: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
			}
			buildDir := filepath.Join(dir, "build", target)
			envExample, err := os.ReadFile(filepath.Join(buildDir, ".env.example"))
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"SLNG_API_KEY=", "OPENAI_API_KEY="} {
				if !strings.Contains(string(envExample), name) {
					t.Errorf(".env.example missing %q:\n%s", name, envExample)
				}
			}
			report, err := os.ReadFile(filepath.Join(buildDir, "compile-report.json"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{`"provider": "slng"`, `"region": "eu"`, `"api_key_env": "OPENAI_API_KEY"`} {
				if !strings.Contains(string(report), want) {
					t.Errorf("compile report missing %q:\n%s", want, report)
				}
			}

			visible := stdout + stderr + string(report)
			err = filepath.Walk(buildDir, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil || info.IsDir() {
					return walkErr
				}
				content, readErr := os.ReadFile(path)
				if readErr != nil {
					return readErr
				}
				visible += string(content)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{routerValue, upstreamValue} {
				if strings.Contains(visible, secret) {
					t.Fatalf("compile output leaked secret value %q", secret)
				}
			}
		})
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
	_, _, err = runCompileCommand(t, "--target", "vapi", dir)
	if err == nil || !strings.Contains(err.Error(), "Vapi has no faithful thinking-audio lowering") {
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
