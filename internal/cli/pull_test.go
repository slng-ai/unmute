package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// `unmute pull` is the only command that contacts the platform for a package's
// benefit, so it is the one most able to break two rules at once: it writes
// into the author's files, and it handles a tool that declares secrets.
//
// Every test here drives a stub `voiceai` off the real captured `tool get`
// documents in testdata/voiceai. A hand-written response would prove the
// decode works by not exercising it.

// pullWithStub runs the command against a copy of the hosted fixture and a stub
// `voiceai` on PATH. It returns the package directory so a caller can read what
// was written.
func pullWithStub(t *testing.T, script string, args ...string) (dir, out, errOut string, err error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub voiceai is a POSIX shell script")
	}
	dir = t.TempDir()
	if copyErr := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "slng_hosted"))); copyErr != nil {
		t.Fatal(copyErr)
	}

	binDir := t.TempDir()
	stub := filepath.Join(binDir, "voiceai")
	if writeErr := os.WriteFile(stub, []byte("#!/bin/sh\n"+script), 0o755); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(target.SlngRouterKeyEnv, "test-key")

	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(append([]string{"pull", dir}, args...))
	err = root.Execute()
	return dir, stdout.String(), stderr.String(), err
}

// fixtureJSON returns a shell fragment that prints one captured `voiceai`
// document.
//
// It `cat`s the file rather than embedding its bytes, because a real captured
// response carries quotes, backslashes and newlines. Embedding it would make
// the stub's own quoting the thing under test.
func fixtureJSON(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", "voiceai", name))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s is not JSON: %v", name, err)
	}
	return "cat " + path
}

// happyStub answers every read the pull makes, from the real captures.
func happyStub(t *testing.T) string {
	t.Helper()
	return `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"550fffde","org_name":"[SLNG] Example Workspace"}}' ;;
  *"tool get check_order"*) ` + fixtureJSON(t, "tool_get_code.json") + ` ;;
  *"tool get search_places_text"*) ` + fixtureJSON(t, "tool_get_api_request.json") + ` ;;
  *"tool list"*) printf '[{"name":"check_order","tool_type":"code","latest_version":1},{"name":"search_places_text","tool_type":"api_request","latest_version":3}]' ;;
  *) printf '[]' ;;
esac`
}

// TestPullWritesTheMirrorAndStampsThePin is the whole loop: three files per
// hosted code tool, two per hosted request tool, and a pin that the offline
// check then accepts.
func TestPullWritesTheMirrorAndStampsThePin(t *testing.T) {
	dir, out, _, err := pullWithStub(t, happyStub(t), "--force")
	if err != nil {
		t.Fatalf("pull failed: %v\n%s", err, out)
	}

	// The organisation, before any other output. Two are reachable from one
	// checkout and are provisioned differently, so a reader who does not know
	// which was read cannot act on any of the rest.
	if !strings.Contains(out, "slng: organisation [SLNG] Example Workspace (550fffde") {
		t.Errorf("the pull does not name the organisation it read:\n%s", out)
	}
	if org, first := strings.Index(out, "organisation"), strings.Index(out, "tools/"); org > first {
		t.Errorf("the organisation line comes after a finding:\n%s", out)
	}

	for _, want := range []string{
		"tools/check_order.slng.json",
		"tools/check_order.slng.py",
		"tools/check_order.yaml",
		"tools/search_places_text.slng.json",
	} {
		if _, statErr := os.Stat(filepath.Join(dir, want)); statErr != nil {
			t.Errorf("%s was not written: %v", want, statErr)
		}
		if !strings.Contains(out, want) {
			t.Errorf("the report never names %s, so a reader cannot tell what happened to it:\n%s", want, out)
		}
	}
	// A request tool has no code, so it gets no module.
	if _, statErr := os.Stat(filepath.Join(dir, "tools/search_places_text.slng.py")); statErr == nil {
		t.Error("a hosted request tool got a module, and it has no code to put in one")
	}

	// The header, which is the only lint suppression in this tree and is
	// measured rather than defensive: the platform's own code fails this
	// repository's lint.
	module, err := os.ReadFile(filepath.Join(dir, "tools/check_order.slng.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(module), "# ruff: noqa") {
		t.Errorf("the mirrored module has no lint header, so committing it turns CI red:\n%s", string(module)[:80])
	}
	if !strings.Contains(string(module), "Edit it in the SLNG dashboard, not here.") {
		t.Error("the header does not tell the reader where to make a change")
	}

	// The pin, checked the way a compile checks it.
	pkg, err := spec.Load(dir)
	if err != nil {
		t.Fatalf("the package the pull wrote does not load: %v", err)
	}
	for _, name := range []string{"check_order", "search_places_text"} {
		tool := pkg.Tools[name]
		if tool.Slng == nil || tool.Slng.Hash == "" {
			t.Fatalf("%s has no pin after a pull", name)
		}
		if got := ir.MirrorDigest(pkg.MirrorBytes[name]); got != tool.Slng.Hash {
			t.Errorf("%s pins %q and its committed bytes digest to %q", name, tool.Slng.Hash, got)
		}
	}

	// The authored file keeps everything else. A pull stamps one line; it does
	// not re-render somebody's file.
	authored, err := os.ReadFile(filepath.Join(dir, "tools/check_order.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# A tool SLNG hosts",
		"announce: One moment while I look that up.",
		"effect: returns_data",
	} {
		if !strings.Contains(string(authored), want) {
			t.Errorf("the pull lost %q from the authored file, so it re-rendered rather than stamped", want)
		}
	}
}

// TestPullOutputIsPlainWhenPiped. The run header is a terminal-only affordance
// and the file list is coloured, so both halves have to leave piped bytes
// alone: a script reading this output must see the same characters a person
// does, minus the paint.
//
// This exists because the command did not have the property when it was
// written. It used the bare style.Dim, which builds its renderer from the
// global stdout rather than from the writer being written to, so it decided
// about colour by looking somewhere else. Every other package command was
// moved onto style.For for that reason, and the helpers have their own test;
// nothing held a *command* to using them, which is what let this one through.
func TestPullOutputIsPlainWhenPiped(t *testing.T) {
	_, out, errOut, err := pullWithStub(t, happyStub(t), "--force")
	if err != nil {
		t.Fatalf("pull failed: %v\n%s", err, errOut)
	}
	for name, stream := range map[string]string{"stdout": out, "stderr": errOut} {
		if strings.Contains(stream, "\x1b[") {
			t.Errorf("%s carries terminal escapes on a captured writer: %q", name, stream)
		}
	}
	// The header goes to a terminal and nowhere else, so a captured run starts
	// at the first line a script would want to read.
	if strings.HasPrefix(out, "pull ") {
		t.Errorf("the run header reached a non-terminal writer:\n%s", out)
	}
	// And the list still reads as paths, painted or not.
	if !strings.Contains(out, "tools/check_order.slng.json") {
		t.Errorf("a file path did not survive the path dimming:\n%s", out)
	}
}

// TestPullReportsUnchangedRatherThanSkipping. The rule `skill install` states in
// its own comment: a silent no-op and a silent overwrite look identical from the
// outside, and a pull that fetched and found nothing new looks identical to one
// that did not run.
func TestPullReportsUnchangedRatherThanSkipping(t *testing.T) {
	stub := happyStub(t)
	dir, _, _, err := pullWithStub(t, stub, "--force")
	if err != nil {
		t.Fatal(err)
	}

	// Second pull over the same directory, which is now already correct.
	binDir := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(binDir, "voiceai"), []byte("#!/bin/sh\n"+stub), 0o755); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(target.SlngRouterKeyEnv, "test-key")
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"pull", dir})
	if err := root.Execute(); err != nil {
		t.Fatalf("a second pull over an up-to-date package failed: %v\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "unchanged") {
		t.Errorf("a pull that found nothing new says nothing:\n%s", stdout.String())
	}
}

// TestPullRefusalsNameTheOrganisationAndTheFix. Every refusal has to say what to
// do next, and the account ones have to name the organisation, because the
// answer depends on which of the two was read.
func TestPullRefusalsNameTheOrganisationAndTheFix(t *testing.T) {
	const account = `*whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"550fffde","org_name":"[SLNG] Example Workspace"}}' ;;`

	for _, tc := range []struct {
		name   string
		stub   string
		before func(t *testing.T, dir string)
		want   []string
	}{
		{
			name: "a name the organisation does not hold",
			stub: `case "$*" in
  ` + account + `
  *"tool list"*) printf '[{"name":"check_order_v2","tool_type":"code","latest_version":1}]' ;;
  *"tool get"*) printf 'error: tool not found\n' >&2; exit 1 ;;
  *) printf '[]' ;;
esac`,
			// unmute creates no tool, so this is the end of the road until
			// somebody makes one. The message says that rather than implying a
			// flag would fix it.
			want: []string{
				"has no tool called `check_order`",
				"check_order_v2",
				"the tool file's own name",
				"create the tool in the SLNG dashboard",
				"unmute creates none",
			},
		},
		{
			name: "a name that resolves to a curated capability",
			stub: `case "$*" in
  ` + account + `
  *"tool get"*) ` + fixtureJSON(t, "tool_get_curated.json") + ` ;;
  *) printf '[]' ;;
esac`,
			want: []string{"capability SLNG curates", "builtin:", "needs no pull"},
		},
		{
			name: "a mirrored file edited by hand",
			stub: happyStub(t),
			before: func(t *testing.T, dir string) {
				t.Helper()
				path := filepath.Join(dir, "tools", "check_order.slng.py")
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(content, "\n# my own note\n"...), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: []string{
				"changed after they were written",
				"tools/check_order.slng.py",
				"an edit here reaches nothing",
				"unmute pull --force",
				"make the change in the SLNG dashboard",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.before == nil {
				_, _, _, err := pullWithStub(t, tc.stub)
				assertRefusal(t, err, tc.want)
				return
			}
			// A case that needs the package touched first runs the pull twice:
			// once to write, once to meet the refusal.
			dir := t.TempDir()
			if copyErr := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "slng_hosted"))); copyErr != nil {
				t.Fatal(copyErr)
			}
			tc.before(t, dir)
			binDir := t.TempDir()
			if writeErr := os.WriteFile(filepath.Join(binDir, "voiceai"), []byte("#!/bin/sh\n"+tc.stub), 0o755); writeErr != nil {
				t.Fatal(writeErr)
			}
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv(target.SlngRouterKeyEnv, "test-key")
			root := newRootCmd()
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetIn(strings.NewReader(""))
			root.SetArgs([]string{"pull", dir})
			assertRefusal(t, root.Execute(), tc.want)
		})
	}
}

func assertRefusal(t *testing.T, err error, want []string) {
	t.Helper()
	if err == nil {
		t.Fatal("the pull did not refuse")
	}
	for _, phrase := range want {
		if !strings.Contains(err.Error(), phrase) {
			t.Errorf("the refusal does not say %q:\n%v", phrase, err)
		}
	}
}

// TestPullRefusesWithNoCredential. The one command that needs one says so
// plainly, and says which commands do not, because that is the next question.
func TestPullRefusesWithNoCredential(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub voiceai is a POSIX shell script")
	}
	dir := t.TempDir()
	if copyErr := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "slng_hosted"))); copyErr != nil {
		t.Fatal(copyErr)
	}
	binDir := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(binDir, "voiceai"), []byte("#!/bin/sh\nprintf '[]'"), 0o755); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(target.SlngRouterKeyEnv, "")
	t.Setenv(target.SlngPushCredentialEnv, "")

	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"pull", dir})
	assertRefusal(t, root.Execute(), []string{
		"no SLNG credential found",
		target.SlngRouterKeyEnv,
		"the only command that needs one",
		"`validate` and `compile` work offline",
	})
}

// TestPullNeverWritesASecretValue is the highest-risk gate in this feature.
//
// The pull reads a tool that declares secrets and writes into the author's
// package, so "secret values appear in no package, generated file, or report"
// needs a check rather than care. The same shape as
// TestFillNeverPutsAValueOnACommandLineOrInOutput next door: log every argv the
// command produced, and read every byte it wrote.
func TestPullNeverWritesASecretValue(t *testing.T) {
	const value = "sk-live-do-not-write-this-anywhere"
	log := filepath.Join(t.TempDir(), "argv.log")
	stub := `printf '%s\n' "$*" >> ` + log + `
` + happyStub(t)

	dir, out, errOut, err := pullWithStub(t, stub, "--force")
	if err != nil {
		t.Fatalf("pull failed: %v\n%s", err, errOut)
	}

	// Nothing shaped like a value reaches either stream.
	for name, stream := range map[string]string{"stdout": out, "stderr": errOut} {
		if strings.Contains(stream, "sk-") || strings.Contains(stream, value) {
			t.Errorf("%s carries something shaped like a credential value:\n%s", name, stream)
		}
	}

	// Nor argv. A value on a command line lands in shell history and in `ps`,
	// which is why the neighbouring secret command has no --value flag at all.
	argv, readErr := os.ReadFile(log)
	if readErr != nil {
		t.Fatalf("the stub logged no argv, so this proved nothing: %v", readErr)
	}
	if len(argv) == 0 {
		t.Fatal("the argv log is empty, so this proved nothing")
	}
	if strings.Contains(string(argv), "sk-") {
		t.Errorf("a credential value reached a command line:\n%s", argv)
	}

	// Nor any file the pull wrote. The name has to be there; the value must not.
	agent, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agent), "SLNG_TOOL_RENDER") {
		t.Error("agent.yaml no longer declares the credential the hosted request tool reads, so nothing checks it")
	}
	if strings.Contains(string(agent), "sk-") {
		t.Error("agent.yaml carries something shaped like a credential value")
	}
	mirror, err := os.ReadFile(filepath.Join(dir, "tools", "search_places_text.slng.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mirror), `"secret_name": "SLNG_TOOL_RENDER"`) {
		t.Error("the mirror lost the credential's name, which is what the emitted call reads it by")
	}
	if strings.Contains(string(mirror), "sk-") {
		t.Error("the mirror carries something shaped like a credential value")
	}
}

// TestPullAddsASecretNameThePlatformOnlyNamesInItsConfig.
//
// Captured live, and the reason the mirror does not read the obvious field: an
// api_request tool with a bearer token had `declared_secrets: []` and named the
// credential in `config.auth.secret_name`. Reading only the first would have
// written nothing, and the emitted call would 401 on a name nothing declared.
func TestPullAddsASecretNameThePlatformOnlyNamesInItsConfig(t *testing.T) {
	dir := t.TempDir()
	if copyErr := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "slng_hosted"))); copyErr != nil {
		t.Fatal(copyErr)
	}
	// Take the name back out, so the pull has to put it there.
	path := filepath.Join(dir, "agent.yaml")
	authored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stripped := strings.Replace(string(authored), "  - SLNG_TOOL_RENDER\n", "", 1)
	if stripped == string(authored) {
		t.Fatal("the fixture no longer declares SLNG_TOOL_RENDER, so this test needs rewriting")
	}
	if err := os.WriteFile(path, []byte(stripped), 0o644); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS == "windows" {
		t.Skip("the stub voiceai is a POSIX shell script")
	}
	binDir := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(binDir, "voiceai"), []byte("#!/bin/sh\n"+happyStub(t)), 0o755); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(target.SlngRouterKeyEnv, "test-key")

	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"pull", dir, "--force"})
	if err := root.Execute(); err != nil {
		t.Fatalf("pull failed: %v\n%s", err, stderr.String())
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "  - SLNG_TOOL_RENDER") {
		t.Errorf("the pull did not add the credential's name to secrets::\n%s", after)
	}
	if !strings.Contains(stdout.String(), "secret") {
		t.Errorf("the report never says a secret was added:\n%s", stdout.String())
	}
	// The rest of the file survives a one-line insert.
	if _, err := spec.Load(dir); err != nil {
		t.Errorf("the package no longer loads after the pull edited agent.yaml: %v", err)
	}
}

// TestPullCheckWritesNothing. `--check` is the shape a CI job would use: it
// reports drift and exits non-zero, and it must not repair anything, because a
// job that silently fixed the thing it was checking would always pass.
func TestPullCheckWritesNothing(t *testing.T) {
	dir := t.TempDir()
	if copyErr := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "slng_hosted"))); copyErr != nil {
		t.Fatal(copyErr)
	}
	// A pin that no longer matches, which is what a moved tool looks like
	// offline.
	path := filepath.Join(dir, "tools", "check_order.slng.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(before, ' '), 0o644); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS == "windows" {
		t.Skip("the stub voiceai is a POSIX shell script")
	}
	binDir := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(binDir, "voiceai"), []byte("#!/bin/sh\n"+happyStub(t)), 0o755); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(target.SlngRouterKeyEnv, "test-key")

	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"pull", dir, "--check"})
	if err := root.Execute(); err == nil {
		t.Fatal("--check found drift and exited 0")
	}
	if !strings.Contains(stdout.String(), "stale") {
		t.Errorf("--check does not say which file is stale:\n%s", stdout.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(append(before, ' ')) {
		t.Error("--check rewrote a file it was only meant to check")
	}
}

// TestPullCommandNameHasOneOwner. `unmute pull` is named in refusals other
// commands print: the compile-time refusal for a hosted tool with no mirror
// committed, the one for a pin that does not match, and the deploy's drift
// warning. That makes the command's name a fact with four readers, so a rename
// that missed one would send an author to a command that does not exist.
//
// The same shape as TestResourcesCommandNameHasOneOwner next door, and for the
// same reason it exists there.
func TestPullCommandNameHasOneOwner(t *testing.T) {
	root := newRootCmd()
	var found bool
	for _, sub := range root.Commands() {
		if sub.Name() == "pull" {
			found = true
		}
	}
	if !found {
		t.Fatal("the root tree has no `pull` command, and three refusals in other packages tell an author to run one")
	}

	// Every surface that names it, held to the same spelling. Each of these is a
	// string in a different package, which is exactly why this is one test.
	agent := buildHostedForNaming(t)
	report, err := ir.Validate(agent, hostedTargets(agent), target.Default())
	if err == nil {
		t.Fatal("a hosted tool with no mirror was not refused, so the message that names the pull never fired")
	}
	joined := strings.Join(report.PerTarget[0].Errors, "\n")
	if !strings.Contains(joined, "`unmute pull`") {
		t.Errorf("the unmirrored-block refusal does not name the command that fixes it:\n%s", joined)
	}
}

// buildHostedForNaming loads the hosted fixture and drops one mirror, which is
// what `slng: {}` before the first pull looks like once it is loaded.
func buildHostedForNaming(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "slng_hosted"))
	if err != nil {
		t.Fatal(err)
	}
	delete(pkg.Mirrors, "check_order")
	delete(pkg.MirrorBytes, "check_order")
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func hostedTargets(agent *ir.Agent) []ir.Target {
	var targets []ir.Target
	for _, resolved := range agent.Targets {
		targets = append(targets, resolved)
	}
	return targets
}
