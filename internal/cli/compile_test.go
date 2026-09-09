package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/target"
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

// TestCompileNeedsNoCredential is the requirement the whole hosted-tool design
// rests on, and it is checked by removing the credential from the environment
// rather than by unsetting a profile.
//
// Nothing in CI has an SLNG credential and nothing ever will, so a package that
// names a tool the platform hosts still has to compile there. That is why the
// definition is fetched once by hand, committed, and read off disk from then on.
//
// The distinction matters: a stored `voiceai` profile on a developer's machine
// would satisfy a fetch nobody meant to make, and the test would pass while the
// property it claims to hold was false.
func TestCompileNeedsNoCredential(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		target  string
	}{
		{"slng_hosted", "slng"},
		{"slng_hosted_code", "livekit"},
		{"slng_hosted_code", "pipecat"},
		// The scalar counterpart: a code target's pin lives in generated
		// tools/<name>.slng.meta.json rather than the tool file's own `hash:`
		// line. slng_hosted_code_scalar declares no slng target of its own;
		// TestSlngNeedsNoScalarMetadataEither (internal/ir/hosted_test.go)
		// covers slng needing no pin at all on the scalar form.
		{"slng_hosted_code_scalar", "livekit"},
		{"slng_hosted_code_scalar", "pipecat"},
	} {
		t.Run(tc.fixture+"/"+tc.target, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", tc.fixture))); err != nil {
				t.Fatal(err)
			}
			// Every name a credential could arrive under, emptied. Setenv with
			// an empty value beats Unsetenv here: an empty string is what a
			// caller reads, and it proves the code does not fall back to a
			// stored profile either.
			t.Setenv(target.SlngRouterKeyEnv, "")
			t.Setenv(target.SlngPushCredentialEnv, "")
			// PATH too, so `voiceai` is not even reachable. If compile grew a
			// fetch, this is what would catch it.
			t.Setenv("PATH", t.TempDir())

			out, errOut, err := runCompileCommand(t, dir, "--target", tc.target)
			if err != nil {
				t.Fatalf("compile needed a credential: %v\n%s\n%s", err, out, errOut)
			}
			if !strings.Contains(out, "agent.json") && !strings.Contains(out, "agent.py") && !strings.Contains(out, "bot.py") {
				t.Errorf("compile wrote no agent module:\n%s", out)
			}
		})
	}
}

func TestCompileSlngPreservesAuthoredFalseAndNumbers(t *testing.T) {
	dir := copyPackage(t, "slng_hosted")
	tool := "slng: check_order\ninject:\n  - enabled: false\n  - limit: 0\n  - count: 42\n  - offset: -1\n  - ratio: 0.5\n"
	if err := os.WriteFile(filepath.Join(dir, "tools", "check_order.yaml"), []byte(tool), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := runCompileCommand(t, dir, "--target", "slng")
	if err != nil {
		t.Fatalf("authored scalars refused: %v\n%s\n%s", err, out, errOut)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "build", "slng", "agent.json"))
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Refs []struct {
			Tool      string         `json:"tool"`
			Arguments map[string]any `json:"argument_overrides"`
		} `json:"tool_refs"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	for _, ref := range body.Refs {
		if ref.Tool != "check_order" {
			continue
		}
		for key, want := range map[string]any{"enabled": false, "limit": float64(0), "count": float64(42), "offset": float64(-1), "ratio": 0.5} {
			if got := ref.Arguments[key]; got != want {
				t.Errorf("%s = %#v, want %#v", key, got, want)
			}
		}
		return
	}
	t.Fatal("missing check_order attachment")
}

// TestCompileSelectsMixedTargetsInEitherOrder is the compile-command half of
// TestSelectingTargetsInEitherOrderGivesTheSameResult (internal/ir/hosted_test.go):
// selecting two targets in one compile must not let their order change what
// either one produces, which is what a check that wrote a finding onto the
// shared IR rather than onto its own target's row would get wrong. No
// package here mixes slng with a code target: they share a turn: model, and
// slng refuses one outright (a pre-existing, orthogonal gap the fixtures'
// own comments already flag), so this proves order-independence with the two
// code targets slng_hosted_code_scalar already declares together.
func TestCompileSelectsMixedTargetsInEitherOrder(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "slng_hosted_code_scalar"))); err != nil {
		t.Fatal(err)
	}
	t.Setenv(target.SlngRouterKeyEnv, "")
	t.Setenv(target.SlngPushCredentialEnv, "")
	t.Setenv("PATH", t.TempDir())

	forward, errOut, err := runCompileCommand(t, dir, "--target", "livekit", "--target", "pipecat")
	if err != nil {
		t.Fatalf("livekit-then-pipecat failed: %v\n%s\n%s", err, forward, errOut)
	}
	reverse, errOut, err := runCompileCommand(t, dir, "--target", "pipecat", "--target", "livekit")
	if err != nil {
		t.Fatalf("pipecat-then-livekit failed: %v\n%s\n%s", err, reverse, errOut)
	}
	forwardLines, reverseLines := strings.Split(strings.TrimSpace(forward), "\n"), strings.Split(strings.TrimSpace(reverse), "\n")
	sort.Strings(forwardLines)
	sort.Strings(reverseLines)
	if strings.Join(forwardLines, "\n") != strings.Join(reverseLines, "\n") {
		t.Errorf("compile wrote a different file list depending on target order:\n  livekit,pipecat: %v\n  pipecat,livekit: %v", forwardLines, reverseLines)
	}
}

// TestCompileSlngIgnoresASiblingCodeTargetsMissingMirror is FR-006/acceptance
// scenario 2: a target declared but not selected must not block slng, even
// when that target is missing something only it needs. slng_hosted already
// carries a committed mirror for every hosted tool; deleting it here without
// declaring a code target still proves nothing, so a livekit target instance
// is declared beside slng specifically so there is a target for the missing
// mirror to matter to, and then never selected.
func TestCompileSlngIgnoresASiblingCodeTargetsMissingMirror(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "slng_hosted"))); err != nil {
		t.Fatal(err)
	}
	// slng_hosted's own turn: is absent, which is exactly what lets a code
	// target instance be declared beside slng without the two colliding over
	// one: a resolved target with no turn model at all offends neither
	// slng's refusal of one nor (were it ever selected, which this test
	// never does) livekit's requirement for one.
	targetsPath := filepath.Join(dir, "targets.yaml")
	targetsContent, err := os.ReadFile(targetsPath)
	if err != nil {
		t.Fatal(err)
	}
	withLivekit := append(append([]byte{}, targetsContent...), []byte(
		"\n  livekit:\n    provider: livekit\n    version: \"1.6.10\"\n    sdk_language: python\n")...)
	if err := os.WriteFile(targetsPath, withLivekit, 0o600); err != nil {
		t.Fatal(err)
	}
	// check_order's mirror is what livekit would need and slng never reads.
	for _, mirrored := range []string{"tools/check_order.slng.json", "tools/check_order.slng.py"} {
		if err := os.Remove(filepath.Join(dir, mirrored)); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv(target.SlngRouterKeyEnv, "")
	t.Setenv(target.SlngPushCredentialEnv, "")
	t.Setenv("PATH", t.TempDir())

	slngOut, errOut, err := runCompileCommand(t, dir, "--target", "slng")
	if err != nil {
		t.Fatalf("slng alone failed even though the missing mirror belongs to a sibling target that was only declared, not selected: %v\n%s\n%s", err, slngOut, errOut)
	}
	if !strings.Contains(slngOut, "agent.json") {
		t.Errorf("compile wrote no slng agent.json:\n%s", slngOut)
	}

	// The other half: selecting the code target that actually needs the
	// mirror still refuses, naming the fix.
	_, _, err = runCompileCommand(t, dir, "--target", "livekit")
	if err == nil {
		t.Fatal("livekit compiled with no committed mirror for check_order")
	}
	if !strings.Contains(err.Error(), "unmute pull") {
		t.Errorf("the livekit refusal does not name the fix: %v", err)
	}
}

// Forwarded bindings and derived sizing reach compile-report.json, which is now
// the only place they are written. compile's stdout is the generated file list
// and nothing else, so this file is where somebody goes to read what the
// compiler decided about a binding it forwards without checking.
func TestCompileWritesBindingsAndSizingToTheReport(t *testing.T) {
	dir := copySafeCore(t)
	stdout, _, err := runCompileCommand(t, "--target", "pipecat", dir)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing but the generated file list. A binding, param, sizing or driver
	// note back on stdout is the noise this command was quietened to remove.
	for _, unwanted := range []string{"forwarded as-is", "binding ", "param ", "sizing ", "telephony "} {
		if strings.Contains(stdout, unwanted) {
			t.Errorf("stdout carries %q, which belongs only in compile-report.json:\n%s", unwanted, stdout)
		}
	}

	report, err := os.ReadFile(filepath.Join(dir, "build", "pipecat", "compile-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"bindings"`, `"sizing"`, `"nova-3"`, `"unbenchmarked"`, `"temperature"`, `"workers"`,
	} {
		if !strings.Contains(string(report), want) {
			t.Errorf("compile-report.json missing %q:\n%s", want, report)
		}
	}
}

// FR-011. A prefetch entry whose author said it writes is named where the rest
// of what the compiler acted on is named, and nowhere else.
//
// Both halves matter, and the second is the one that keeps stdout readable. The
// key is required, so every tool entry carries an answer: a warning for the
// `true` ones would fire on every compile of every package that legitimately
// writes, forever, which is the noise this command was quietened to remove.
func TestCompileNamesAWritingPrefetchEntryInTheReportAndNotOnStdout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "prefetch_core"))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := mustReplace(t, string(raw), "    writes: false\n", "    writes: true\n")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runCompileCommand(t, "--target", "pipecat", dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range []struct{ name, text string }{{"stdout", stdout}, {"stderr", stderr}} {
		if strings.Contains(stream.text, "writes") {
			t.Errorf("%s mentions writes:, and a required key is not news:\n%s", stream.name, stream.text)
		}
	}

	report, err := os.ReadFile(filepath.Join(dir, "build", "pipecat", "compile-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"prefetch_writes"`, `"profile"`, `"lookup_customer"`} {
		if !strings.Contains(string(report), want) {
			t.Errorf("compile-report.json missing %q:\n%s", want, report)
		}
	}

	// And an entry that reads is named nowhere: the report lists what writes, not
	// every entry with the answer printed beside it.
	if err := os.WriteFile(path, []byte(mustReplace(t, source, "    writes: true\n", "    writes: false\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCompileCommand(t, "--target", "pipecat", dir); err != nil {
		t.Fatal(err)
	}
	report, err = os.ReadFile(filepath.Join(dir, "build", "pipecat", "compile-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(report), `"prefetch_writes"`) {
		t.Errorf("the report lists prefetch_writes for a package where nothing writes:\n%s", report)
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
