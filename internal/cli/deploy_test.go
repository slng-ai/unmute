package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/target"
)

// The four documents `voiceai agents push --json` produces, captured from
// voiceai 0.1.14 on 2026-08-27 by running it against examples/slng-support and
// internal/testdata/slng_tools.
//
// They are the contract this command reads, and they live here rather than being
// hand-written because the fields that matter are the ones the tool actually
// emits: `version` is an object in one shape and the string "unchanged" in
// another, a blocked check carries no `agent` at all, and a plan's `dry_run` is
// absent from every other shape. A struct that guesses at any of those reads
// zero out of a real push and reports success.
const (
	pushPlanJSON = `{
  "ok": true,
  "dry_run": true,
  "organisation": { "id": "550fffde", "name": "[SLNG] Example Workspace" },
  "package": "/pkg/build/slng/agent.json",
  "agent": { "action": "create" },
  "tools": [
    { "name": "check_order", "action": "create", "toolType": "code", "needsGreenRun": true, "hasSample": true, "willRun": true }
  ],
  "refs": [
    { "name": "end_call", "tool_id": "fd25f5c5", "version": 3, "attachment_id": "097b571f", "reused": false, "shadowed": "curated" }
  ],
  "removals": [ { "name": "refund", "attachment_id": "aaaa1111", "tool_id": "bbbb2222" } ],
  "overwrites": [ "system_prompt" ],
  "blockers": []
}`

	pushBlockedJSON = `{
  "ok": false,
  "changed": false,
  "organisation": { "id": "550fffde", "name": "[SLNG] Example Workspace" },
  "blockers": [
    {
      "kind": "vault_missing",
      "items": [ "ACME_BRAND", "REFUND_API_TOKEN" ],
      "detail": "create them, then push again.",
      "url": "https://app.slng.ai/vault/secrets"
    },
    {
      "kind": "sample_missing",
      "items": [ "check_order (code)" ],
      "detail": "a code or api_request tool cannot publish until one successful run proves it."
    }
  ]
}`

	pushOutcomeJSON = `{
  "ok": true,
  "organisation": { "id": "550fffde", "name": "[SLNG] Example Workspace" },
  "tools": [
    { "name": "check_order", "created": true, "introspected": true, "ran": "succeeded", "published": 1 }
  ],
  "agent": { "id": "01998a7c", "action": "update" },
  "version": { "number": 4, "label": "slng 2026-08-27T10:00:00Z" }
}`

	pushErrorJSON = `{
  "ok": false,
  "changed": false,
  "error": "no VOICEAI_API_KEY set. Run ` + "`voiceai login`" + `, or set VOICEAI_API_KEY in your env."
}`
)

func decodePush(t *testing.T, raw string) pushResult {
	t.Helper()
	var result pushResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decoding a real push document: %v", err)
	}
	return result
}

// TestPushDocumentsDecode holds the field-by-field reading of each shape. The
// interesting assertions are the ones a plausible struct gets wrong.
func TestPushDocumentsDecode(t *testing.T) {
	plan := decodePush(t, pushPlanJSON)
	if !plan.OK || !plan.DryRun {
		t.Errorf("plan: ok=%t dry_run=%t, want both true", plan.OK, plan.DryRun)
	}
	if plan.Agent.Action != "create" {
		t.Errorf("plan agent = %+v, want action create", plan.Agent)
	}
	if len(plan.Tools) != 1 || !plan.Tools[0].WillRun || plan.Tools[0].ToolType != "code" {
		t.Errorf("plan tools = %+v, want one code tool that will run its sample", plan.Tools)
	}
	// The two destructive halves of an update. A struct that misses either turns
	// a dry run into a report that says nothing is at risk.
	if len(plan.Removals) != 1 || plan.Removals[0].Name != "refund" {
		t.Errorf("plan removals = %+v, want refund", plan.Removals)
	}
	if len(plan.Overwrites) != 1 || plan.Overwrites[0] != "system_prompt" {
		t.Errorf("plan overwrites = %+v, want [system_prompt]", plan.Overwrites)
	}

	blocked := decodePush(t, pushBlockedJSON)
	if blocked.OK || len(blocked.Blockers) != 2 {
		t.Fatalf("blocked: ok=%t blockers=%d, want false and 2", blocked.OK, len(blocked.Blockers))
	}
	if got := blocked.Blockers[0]; got.Kind != "vault_missing" || len(got.Items) != 2 || got.URL == "" {
		t.Errorf("first blocker = %+v, want vault_missing with 2 items and a url", got)
	}
	if blocked.Agent.ID != "" {
		t.Errorf("a blocked check names no agent, got %q", blocked.Agent.ID)
	}

	outcome := decodePush(t, pushOutcomeJSON)
	if !outcome.OK || outcome.DryRun {
		t.Errorf("outcome: ok=%t dry_run=%t, want true and false", outcome.OK, outcome.DryRun)
	}
	if outcome.Agent.ID != "01998a7c" || outcome.Agent.Action != "update" {
		t.Errorf("outcome agent = %+v, want id 01998a7c action update", outcome.Agent)
	}
	if got := versionLine(outcome.Version); !strings.Contains(got, "version 4") {
		t.Errorf("versionLine = %q, want it to name version 4", got)
	}

	failed := decodePush(t, pushErrorJSON)
	if failed.OK || failed.Changed || !strings.Contains(failed.Error, "VOICEAI_API_KEY") {
		t.Errorf("error document = %+v, want ok=false changed=false and an error naming the key", failed)
	}
}

// TestVersionLineReadsTheStringForm is the other half of the two-typed field.
// SLNG writes no version when a body matches what is already live, and the tool
// reports that as a bare string rather than an object.
func TestVersionLineReadsTheStringForm(t *testing.T) {
	got := versionLine(json.RawMessage(`"unchanged"`))
	if !strings.Contains(got, "unchanged") {
		t.Errorf("versionLine(%q) = %q, want it to say the agent is unchanged", "unchanged", got)
	}
	if strings.Contains(got, "version 0") {
		t.Errorf("versionLine printed a version number for the string form: %q", got)
	}
}

// TestPushBlockersCarryEveryFix is the whole point of relaying the tool's own
// report: an author reading this has to see what is missing, why, and where to
// go, without opening another tool.
func TestPushBlockersCarryEveryFix(t *testing.T) {
	result := decodePush(t, pushBlockedJSON)
	var errOut bytes.Buffer
	printPushBlockers(&errOut, "slng", filepath.Join("pkg", "build", "slng"), result.Blockers)
	got := errOut.String()

	for _, want := range []string{
		"ACME_BRAND", "REFUND_API_TOKEN", // every item
		"create them, then push again.",                  // the tool's own detail
		"https://app.slng.ai/vault/secrets",              // the page that fixes it
		"vault missing",                                  // the kind, humanised
		"check_order (code)",                             // the second blocker's item
		"nothing was created or changed.",                // the reassurance
		filepath.Join("pkg", "build", "slng", "samples"), // unmute's own addition
		"--run-samples",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the blocker report does not mention %q:\n%s", want, got)
		}
	}
	// An underscored kind reaching an author is the tool's internal spelling.
	if strings.Contains(got, "vault_missing") {
		t.Errorf("the report prints a raw kind:\n%s", got)
	}
}

// TestBlockerHintOnlyAddsWhatTheToolCannotKnow. The push tool owns the wording
// of every blocker, so unmute adds a line only where it knows something the tool
// does not: the path it compiled to, and its own flag.
func TestBlockerHintOnlyAddsWhatTheToolCannotKnow(t *testing.T) {
	for _, kind := range []string{"vault_missing", "tool_unresolved", "mcp_unsupported", "tool_type_immutable", "singleton_exists"} {
		if hint := blockerHint(kind, "build/slng"); hint != "" {
			t.Errorf("blockerHint(%q) = %q, want nothing: the tool's detail already says it", kind, hint)
		}
	}
	for _, kind := range []string{"sample_missing", "samples_not_enabled", "agent_ambiguous"} {
		if blockerHint(kind, "build/slng") == "" {
			t.Errorf("blockerHint(%q) is empty, but unmute knows the path or the flag for it", kind)
		}
	}
}

// TestPushResultWarnsThatAnUpdateReplaces. Pushing replaces rather than merges,
// and which agent it replaces is decided by the name in the body. A silent
// replace is the failure mode, so the warning is the gate.
//
// It has to quote the *agent's* name, not this target's. The two were the same
// string until a package started naming its own deployments, so the warning
// read from the wrong one and told the author to rename the wrong thing.
func TestPushResultWarnsThatAnUpdateReplaces(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := printPushResult(&out, &errOut, "slng", "acme-support-slng", "build/slng", target.SlngRouterKeyEnv, slngAccount{}, decodePush(t, pushOutcomeJSON)); err != nil {
		t.Fatalf("a successful outcome returned %v", err)
	}
	warned := errOut.String()
	for _, want := range []string{"replaces", "01998a7c", "--agent-id", `"acme-support-slng"`, "`name:` in agent.yaml"} {
		if !strings.Contains(warned, want) {
			t.Errorf("the replace warning does not mention %q:\n%s", want, warned)
		}
	}
	if strings.Contains(warned, "rename the target") {
		t.Errorf("the warning sends the author to rename the target, which no longer decides the agent's name:\n%s", warned)
	}
	// The closing line has to be runnable, agent id included: the web-session
	// command's id is not optional, which is what target.SlngWebSessionCommand
	// and its own gate exist to hold.
	if got := out.String(); !strings.Contains(got, "01998a7c --file session.json") {
		t.Errorf("the outcome does not name a runnable web-session command:\n%s", got)
	}
}

// TestPushResultReturnsAnErrorWheneverItPrintedOne. Exit code and output have to
// agree: a command that prints "cannot deploy" and exits 0 is worse than either.
func TestPushResultReturnsAnErrorWheneverItPrintedOne(t *testing.T) {
	for name, raw := range map[string]string{"blocked": pushBlockedJSON, "errored": pushErrorJSON} {
		var out, errOut bytes.Buffer
		err := printPushResult(&out, &errOut, "slng", "acme-support-slng", "build/slng", "", slngAccount{}, decodePush(t, raw))
		if err == nil {
			t.Errorf("%s: printPushResult returned nil, so unmute would exit 0 after printing:\n%s", name, errOut.String())
		}
	}
	var out, errOut bytes.Buffer
	if err := printPushResult(&out, &errOut, "slng", "acme-support-slng", "build/slng", target.SlngRouterKeyEnv, slngAccount{}, decodePush(t, pushPlanJSON)); err != nil {
		t.Errorf("a dry run that found nothing wrong returned %v", err)
	}
}

// TestPushFailureNamesTheKeyOnlyWhenThereWasNone. With a key in the environment
// the failure is something else, and pointing at the key would send the reader
// to the one thing that is already right.
func TestPushFailureNamesTheKeyOnlyWhenThereWasNone(t *testing.T) {
	result := decodePush(t, pushErrorJSON)

	var withoutKey bytes.Buffer
	_ = pushFailure(&withoutKey, "slng", "", result)
	if !strings.Contains(withoutKey.String(), target.SlngRouterKeyEnv) {
		t.Errorf("with no key resolved, the failure does not name %s:\n%s", target.SlngRouterKeyEnv, withoutKey.String())
	}

	var withKey bytes.Buffer
	_ = pushFailure(&withKey, "slng", target.SlngRouterKeyEnv, result)
	if strings.Contains(withKey.String(), "export "+target.SlngRouterKeyEnv) {
		t.Errorf("a key was resolved, so the failure should not tell the reader to export one:\n%s", withKey.String())
	}
}

// TestDeployCredentialPrefersTheSlngKey. One SLNG key serves every SLNG role, so
// SLNG_API_KEY is the name a package's .env already carries; VOICEAI_API_KEY is
// the name the push tool itself reads and keeps a voiceai-shaped shell working.
func TestDeployCredentialPrefersTheSlngKey(t *testing.T) {
	both := []string{target.SlngRouterKeyEnv + "=  slng-key  ", target.SlngPushCredentialEnv + "=voiceai-key"}
	if key, source := deployCredential(both); key != "slng-key" || source != target.SlngRouterKeyEnv {
		t.Errorf("deployCredential() = %q, %q; want the trimmed SLNG key from %s", key, source, target.SlngRouterKeyEnv)
	}

	only := []string{target.SlngPushCredentialEnv + "=voiceai-key"}
	if key, source := deployCredential(only); key != "voiceai-key" || source != target.SlngPushCredentialEnv {
		t.Errorf("with only the push tool's own key set, deployCredential() = %q, %q", key, source)
	}

	if key, source := deployCredential(nil); key != "" || source != "" {
		t.Errorf("with neither set, deployCredential() = %q, %q; want empty so the tool falls back to its profile", key, source)
	}
}

// TestDeployReadsTheKeyFromADotenv. `unmute dev` reads a package's .env, so an
// author who put SLNG_API_KEY there and ran dev expects deploy to see the same
// line. Reading only the process environment made that a silent failure.
func TestDeployReadsTheKeyFromADotenv(t *testing.T) {
	t.Setenv(target.SlngRouterKeyEnv, "")
	t.Setenv(target.SlngPushCredentialEnv, "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(target.SlngRouterKeyEnv+"=from-dotenv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	key, source := deployCredential(packageEnv(dir, io.Discard))
	if key != "from-dotenv" || source != target.SlngRouterKeyEnv {
		t.Errorf("deployCredential() = %q, %q; want the key out of the package .env", key, source)
	}
}

// TestDeployRefusesAPackageWithNoSlngTarget. `deploy` pushes to SLNG and nowhere
// else, so the refusal has to name what the package does declare and the block
// that would make it deployable — not just say no.
func TestDeployRefusesAPackageWithNoSlngTarget(t *testing.T) {
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"deploy", filepath.Join("..", "..", "examples", "salon-concierge")})
	err := root.Execute()
	if err == nil {
		t.Fatal("deploying a package with no slng target succeeded")
	}
	for _, want := range []string{"no slng target", "livekit", "pipecat", "provider: slng", "unmute compile"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q:\n%s", want, err.Error())
		}
	}
}

// TestCompilePreservesToolSamples. Samples live in build/<target>/samples/,
// which is inside the directory a rewrite deletes. Without the preserved
// pattern, writing a sample and re-running deploy removes it and reports the
// same sample_missing blocker forever.
func TestCompilePreservesToolSamples(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "build", "slng")
	sample := filepath.Join(outDir, "samples", "check_order.json")
	if err := os.MkdirAll(filepath.Dir(sample), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sample, []byte(`{"order_id":"A1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "agent.json"), []byte(`{"name":"stale"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	files := []generate.File{{Path: "agent.json", Content: []byte(`{"name":"fresh"}`)}}
	if err := writeArtifactFiles(nil, outDir, files); err != nil {
		t.Fatalf("writeArtifactFiles: %v", err)
	}

	kept, err := os.ReadFile(sample)
	if err != nil {
		t.Fatalf("the sample did not survive a recompile: %v", err)
	}
	if string(kept) != `{"order_id":"A1"}` {
		t.Errorf("the sample survived with the wrong content: %s", kept)
	}
	rewritten, err := os.ReadFile(filepath.Join(outDir, "agent.json"))
	if err != nil || string(rewritten) != `{"name":"fresh"}` {
		t.Errorf("agent.json = %s, %v; preserving samples must not stop the rewrite", rewritten, err)
	}
}

// TestRunPushForwardsEveryFlag. A typo in one of these is a quiet wrong answer:
// a dropped --run-samples makes the push refuse with `sample_missing` on a
// package that has its samples, and a dropped --dry-run writes to a live
// organisation. So the stub echoes back what it was handed.
//
// The stub is a shell script, which is why this skips on Windows. Tests run on
// Linux in CI and on macOS locally; the release build is the only Windows target
// and it runs no tests.
func TestRunPushForwardsEveryFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub push tool is a POSIX shell script")
	}
	stub := filepath.Join(t.TempDir(), "voiceai-stub")
	script := "#!/bin/sh\n" +
		`printf '{"ok":true,"dry_run":true,"error":"%s","organisation":{"id":"x"},"agent":{"name":"a","action":"create"}}' "$*"` + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	opts := deployOptions{dryRun: true, runSamples: true, agentID: "an-id", label: "a label"}
	result, err := runPush(stub, "build/slng", []string{target.SlngPushCredentialEnv + "=k"}, "k", opts)
	if err != nil {
		t.Fatalf("runPush: %v", err)
	}
	// The stub echoes its argv into `error`, which is the one string field that
	// survives a decode regardless of shape.
	for _, want := range []string{
		"agents push build/slng --json",
		"--dry-run", "--run-samples", "--agent-id an-id", "--label a label",
	} {
		if !strings.Contains(result.Error, want) {
			t.Errorf("the push was not handed %q; argv was %q", want, result.Error)
		}
	}

	// And a run with no options adds none of them.
	bare, err := runPush(stub, "build/slng", nil, "", deployOptions{})
	if err != nil {
		t.Fatalf("runPush: %v", err)
	}
	for _, unwanted := range []string{"--dry-run", "--run-samples", "--agent-id", "--label"} {
		if strings.Contains(bare.Error, unwanted) {
			t.Errorf("a plain deploy passed %q; argv was %q", unwanted, bare.Error)
		}
	}
}

// TestRunPushReportsUnreadableOutput. The push tool prints JSON on success and on
// failure, so stdout that will not parse means the tool itself broke — and that
// has to be an error rather than a zero-valued result read as "nothing wrong".
func TestRunPushReportsUnreadableOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub push tool is a POSIX shell script")
	}
	stub := filepath.Join(t.TempDir(), "voiceai-broken")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho 'Killed: 9' >&2\nexit 137\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := runPush(stub, "build/slng", nil, "", deployOptions{})
	if err == nil {
		t.Fatal("unreadable push output was accepted as a result")
	}
	if !strings.Contains(err.Error(), "Killed: 9") {
		t.Errorf("the error hides what the tool said: %v", err)
	}
}

// deployWithStub runs a real `deploy` against a stub `voiceai` on PATH, over a
// copy of the slng_tools fixture so a refused run can be checked for having
// written nothing.
func deployWithStub(t *testing.T, script string, args ...string) (dir, out, errOut string, err error) {
	t.Helper()
	// No stdin, so nothing prompts. Left implicit, cmd.InOrStdin() returns
	// os.Stdin, which under `go test` is /dev/null: a character device, so it
	// passes for a terminal and the run starts asking questions nobody answers.
	return deployWithInput(t, "", script, args...)
}

// deployWithInput is deployWithStub with a person at the keyboard.
func deployWithInput(t *testing.T, input, script string, args ...string) (dir, out, errOut string, err error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub voiceai is a POSIX shell script")
	}
	dir = t.TempDir()
	source := filepath.Join("..", "testdata", "slng_tools")
	if copyErr := os.CopyFS(dir, os.DirFS(source)); copyErr != nil {
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
	root.SetIn(strings.NewReader(input))
	root.SetArgs(append([]string{"deploy", dir}, args...))
	err = root.Execute()
	return dir, stdout.String(), stderr.String(), err
}

// A stub whose account has nothing the fixture needs.
const emptyAccountStub = `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"org-1","org_name":"Example"}}' ;;
  *) printf '[]' ;;
esac`

// TestDeployPreflightRefusesBeforeWritingAnything.
//
// This is the promise the whole feature rests on. Generate returns an artifact
// and writeArtifactFiles is a separate step, so a run refused between them has
// touched neither the build directory nor the organisation. If the preflight
// ever moved after the write, a refused deploy would start leaving a stale
// build/slng behind and the "nothing was changed" line would become a lie.
func TestDeployPreflightRefusesBeforeWritingAnything(t *testing.T) {
	dir, out, errOut, err := deployWithStub(t, emptyAccountStub)
	if err == nil {
		t.Fatal("an account holding none of the package's requirements was deployed to")
	}

	if _, statErr := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(statErr) {
		t.Errorf("a refused preflight left a build directory behind (%v)", statErr)
	}
	if strings.Contains(out, "compiled") {
		t.Errorf("the run reported compiling despite refusing:\n%s", out)
	}
	if !strings.Contains(errOut, "nothing was compiled, created or changed") {
		t.Errorf("the refusal does not say the account is untouched:\n%s", errOut)
	}
	// Every gap, not the first: this package needs a builtin, an MCP server, two
	// MCP tools and a credential, and an author should learn all of it once.
	for _, want := range []string{"end_call", "internal_docs", "REFUND_API_TOKEN"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("the refusal does not name %q:\n%s", want, errOut)
		}
	}
}

// TestDeployNamesTheOrganisationBeforeChecking. A finding is a statement about
// one account, and an environment key and a stored profile can belong to
// different ones. Printing the organisation after the findings would mean an
// author reads four problems before learning which organisation has them.
func TestDeployNamesTheOrganisationBeforeChecking(t *testing.T) {
	_, out, _, _ := deployWithStub(t, emptyAccountStub)
	if !strings.Contains(out, "organisation Example (org-1), profile default") {
		t.Errorf("the run does not name the account it resolved:\n%s", out)
	}
}

// TestDeployStopsWhenItCannotNameTheAccount. With no account, every finding
// would be about an organisation the run cannot identify, so there is nothing
// worth reporting.
func TestDeployStopsWhenItCannotNameTheAccount(t *testing.T) {
	dir, _, errOut, err := deployWithStub(t, `printf 'error: invalid api key\n' >&2; exit 1`)
	if err == nil {
		t.Fatal("a run that could not identify the account carried on")
	}
	if !strings.Contains(err.Error(), "which SLNG organisation") {
		t.Errorf("the error does not say what could not be determined: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(statErr) {
		t.Error("a run that never identified the account still wrote a build directory")
	}
	_ = errOut
}

// TestDeployPreflightDegrades. An old `voiceai`, a restricted CI network or a
// missing read scope must not become a deploy failure. The push is the
// authority; the preflight is an early warning that can be skipped.
func TestDeployPreflightDegrades(t *testing.T) {
	// Everything the package needs is present, except that the MCP listing cannot
	// be read at all. The run must warn, reach the push, and say what it skipped.
	stub := `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"org-1","org_name":"Example"}}' ;;
  *"mcp list"*) printf 'error: insufficient scope\n' >&2; exit 1 ;;
  *"tool list"*) printf '[{"name":"end_call","tool_type":"end_call"}]' ;;
  *"secret list"*) printf '[{"name":"REFUND_API_TOKEN","kind":"secret","has_value":true},{"name":"ACME_BRAND","kind":"variable","has_value":true}]' ;;
  *"agents push"*) printf '{"ok":true,"dry_run":true,"organisation":{"id":"org-1","name":"Example"},"agent":{"name":"a","action":"create"}}' ;;
  *) printf '[]' ;;
esac`
	_, out, errOut, err := deployWithStub(t, stub, "--dry-run")
	if err != nil {
		t.Fatalf("an unreadable MCP listing failed the deploy: %v\n%s", err, errOut)
	}
	if !strings.Contains(errOut, "insufficient scope") {
		t.Errorf("the skipped read is not reported:\n%s", errOut)
	}
	if !strings.Contains(errOut, "the push decides") {
		t.Errorf("the warning does not say what covers the gap:\n%s", errOut)
	}
	if !strings.Contains(out, "dry run") {
		t.Errorf("the run did not reach the push:\n%s", out)
	}
}

// TestDeployRunsThePreflightUnderDryRun. `--dry-run` is the flag most likely to
// be wired past a new check by accident, and the accident is silent: the run
// would report a clean plan for an account that cannot accept it.
func TestDeployRunsThePreflightUnderDryRun(t *testing.T) {
	dir, _, errOut, err := deployWithStub(t, emptyAccountStub, "--dry-run")
	if err == nil {
		t.Fatal("--dry-run skipped the preflight and reported a plan against an empty account")
	}
	if !strings.Contains(errOut, "Cannot deploy") {
		t.Errorf("--dry-run did not produce the preflight refusal:\n%s", errOut)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(statErr) {
		t.Error("--dry-run wrote a build directory")
	}
}

// TestDeployForwardsTheProfileAsARootOption. `--profile` is a root option on the
// CLI. Passed after the subcommand it is an unknown flag; worse, a run that
// silently resolved a different account from the one the preflight checked would
// make every finding a statement about somewhere else.
func TestDeployForwardsTheProfileAsARootOption(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	stub := `printf '%s\n' "$*" >> ` + log + `
case "$*" in
  *whoami*) printf '{"ok":true,"profile":"work","account":{"org_id":"org-1","org_name":"Example"}}' ;;
  *) printf '[]' ;;
esac`
	if _, _, _, err := deployWithStub(t, stub, "--profile", "work"); err == nil {
		t.Fatal("the empty-account stub did not refuse")
	}
	raw, readErr := os.ReadFile(log)
	if readErr != nil {
		t.Fatalf("the stub logged nothing: %v", readErr)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if !strings.HasPrefix(line, "--profile work ") {
			t.Errorf("`voiceai %s` does not lead with the profile, so it may resolve a different account", line)
		}
	}
}

// A stub whose account holds everything the slng_tools fixture needs, so the run
// reaches the push and the steps after it. `trunks` is left to each caller.
func provisionedStub(trunks string) string {
	return `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"o","org_name":"Example"}}' ;;
  *"agents push"*) printf '{"ok":true,"organisation":{"id":"o","name":"Example"},"agent":{"id":"agent-1","name":"slng-tools-slng","action":"create"},"version":"unchanged"}' ;;
  *"tool list"*) printf '[{"name":"end_call","tool_type":"end_call"}]' ;;
  *"secret list"*) printf '[{"name":"REFUND_API_TOKEN","kind":"secret","has_value":true},{"name":"ACME_BRAND","kind":"variable","has_value":true}]' ;;
  *"mcp list"*) printf '[{"name":"internal_docs","capability_status":"healthy"}]' ;;
  *"mcp tools"*) printf '[{"name":"search_docs"},{"name":"read_doc"}]' ;;
  *"trunks list"*) ` + trunks + ` ;;
  *) printf '[]' ;;
esac`
}

// TestDeployReportsTheNumberThatReachesTheAgent. Telephony is verified on a
// deployed agent against a real carrier, so the moment after a push is exactly
// when an author needs the number.
func TestDeployReportsTheNumberThatReachesTheAgent(t *testing.T) {
	stub := provisionedStub(`printf '[{"direction":"inbound","name":"2_inbound","numbers":["+447700900222"],"usable":true,"in_use_by":"slng-tools-fixture-slng"}]'`)
	_, out, _, err := deployWithStub(t, stub)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if !strings.Contains(out, "+447700900222") {
		t.Errorf("the run does not print the number that reaches the agent:\n%s", out)
	}
	if !strings.Contains(out, "inbound trunk 2_inbound") {
		t.Errorf("the run does not name the trunk and its direction:\n%s", out)
	}
}

// TestDeployNeverReadsTheAgentNameBackFromThePush.
//
// `voiceai agents push --json` reports the agent's id and action and no name;
// the name goes to its human stream alone. unmute decoded a Name field from it
// anyway, which yielded "" on every run, and "" is also what a FREE trunk's
// in_use_by decodes to. The equality that answers "which number reaches my
// agent" therefore matched every unattached trunk, so a first deploy printed
// "inbound trunk X reaches this agent" about a number that reached nothing, and
// returned early without ever offering to attach one. Verified against the live
// CLI 2026-09-01.
//
// The deployed name is one unmute already computed to build the body, so this
// holds that it comes from there and that an empty in_use_by is never a match.
func TestDeployNeverReadsTheAgentNameBackFromThePush(t *testing.T) {
	// A free trunk, exactly as the account reports one.
	stub := provisionedStub(`printf '[{"direction":"inbound","id":"trunk-1","name":"1_inbound","numbers":["+447700900111"],"usable":true,"in_use_by":null}]'`)
	_, out, _, err := deployWithStub(t, stub)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if strings.Contains(out, "trunk 1_inbound reaches this agent") {
		t.Errorf("a free trunk is reported as reaching the agent, so the name comparison matched two empty strings:\n%s", out)
	}
	if !strings.Contains(out, "no number reaches this agent yet") {
		t.Errorf("the run does not say the agent is unreachable:\n%s", out)
	}

	// And the push document itself may not carry a name for anything to read.
	if strings.Contains(pushOutcomeJSON, `"name"`) && strings.Contains(pushOutcomeJSON, `"agent"`) {
		var doc struct {
			Agent map[string]any `json:"agent"`
		}
		if err := json.Unmarshal([]byte(pushOutcomeJSON), &doc); err != nil {
			t.Fatal(err)
		}
		if _, ok := doc.Agent["name"]; ok {
			t.Error("the push fixture invents an agent name the real CLI does not send")
		}
	}
}

// TestDeploySaysWhenNoNumberReachesTheAgent. The ordinary state of a first
// deploy: every inbound trunk on the verified organisation was attached to
// nothing. Printing an empty list here reads as a failure to look.
func TestDeployAttachesTheChosenTrunk(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	stub := `printf '%s\n' "$*" >> ` + log + "\n" + provisionedStub(`printf '[{"direction":"inbound","id":"trunk-1","name":"1_inbound","numbers":["+447700900111"],"usable":true,"in_use_by":null}]'`)

	// Not a terminal, so the run must not prompt and must not attach: an
	// unattended deploy silently claiming a phone number is the failure this
	// guards against.
	_, out, _, err := deployWithStub(t, stub)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	raw, _ := os.ReadFile(log)
	if strings.Contains(string(raw), "agents update") {
		t.Error("an unattended deploy attached a trunk without being asked")
	}
	if !strings.Contains(out, "1 inbound trunk free") {
		t.Errorf("a non-interactive run does not name the trunks it could have used:\n%s", out)
	}
}

// And with a person at the keyboard, the chosen trunk is attached.
//
// offerTrunk is called directly, the way the secret-fill tests call offerToFill:
// whether there is a terminal is decided from the real os.Stdin, which no test
// can stand in for, so the decision is a parameter and this exercises the branch
// behind it.
func TestOfferTrunkAttachesTheOneChosen(t *testing.T) {
	runner, log := fillRunner(t, "cat > /dev/null\nexit 0")
	candidates := []slngTrunk{
		{ID: "trunk-1", Direction: "inbound", Name: "1_inbound", Numbers: []string{"+447700900111"}, Usable: true},
		{ID: "trunk-2", Direction: "inbound", Name: "2_inbound", Numbers: []string{"+447700900222"}, Usable: true},
	}

	var out, errOut bytes.Buffer
	offerTrunk(strings.NewReader("2\n"), &out, &errOut, runner, "slng", "agent-1", candidates, true)

	if !strings.Contains(out.String(), "[1] 1_inbound") || !strings.Contains(out.String(), "[2] 2_inbound") {
		t.Errorf("the choices are not offered by number:\n%s", out.String())
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("nothing was attached: %v", err)
	}
	if !strings.Contains(string(raw), "agents update agent-1") {
		t.Errorf("the chosen trunk was not attached:\n%s", raw)
	}
	if !strings.Contains(out.String(), "2_inbound attached") {
		t.Errorf("the run does not confirm which trunk answers:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Call +447700900222") {
		t.Errorf("the run does not print the number to call:\n%s", out.String())
	}
}

// Declining leaves it unattached. A number is somebody's phone bill, so silence,
// a zero, or an answer that is not a choice must never be read as consent.
func TestOfferTrunkAttachesNothingWhenDeclined(t *testing.T) {
	candidates := []slngTrunk{{ID: "trunk-1", Direction: "inbound", Name: "1_inbound", Numbers: []string{"+441"}, Usable: true}}

	for _, answer := range []string{"0\n", "\n", "banana\n", "2\n", "-1\n", ""} {
		t.Run("answer "+strings.TrimSpace(answer), func(t *testing.T) {
			runner, log := fillRunner(t, "exit 0")
			var out, errOut bytes.Buffer
			offerTrunk(strings.NewReader(answer), &out, &errOut, runner, "slng", "agent-1", candidates, true)
			if _, err := os.ReadFile(log); err == nil {
				t.Errorf("answer %q attached a trunk", answer)
			}
		})
	}
}

// A dry run created no agent, so there is nothing to attach a number to and
// nothing to ask about.
func TestOfferTrunkAsksNothingWithoutAnAgent(t *testing.T) {
	runner, log := fillRunner(t, "exit 0")
	var out, errOut bytes.Buffer
	offerTrunk(strings.NewReader("1\n"), &out, &errOut, runner, "slng", "",
		[]slngTrunk{{ID: "t", Direction: "inbound", Name: "n", Usable: true}}, true)
	if out.String() != "" {
		t.Errorf("a run with no agent id still asked: %q", out.String())
	}
	if _, err := os.ReadFile(log); err == nil {
		t.Error("a run with no agent id attached a trunk")
	}
}

// The attachment itself is a PATCH of one field, so it cannot disturb anything
// else on the agent, and the field it sets follows the trunk's direction.
func TestAttachTrunkPatchesOneFieldByDirection(t *testing.T) {
	for _, tc := range []struct {
		direction, field string
	}{
		{"inbound", "sip_inbound_trunk_id"},
		{"outbound", "sip_outbound_trunk_id"},
	} {
		t.Run(tc.direction, func(t *testing.T) {
			runner, log := fillRunner(t, "cat > /tmp/unmute-attach-body.json\nexit 0")
			err := runner.attachTrunk("agent-1", slngTrunk{ID: "trunk-9", Direction: tc.direction, Name: "t"})
			if err != nil {
				t.Fatalf("attachTrunk: %v", err)
			}
			raw, readErr := os.ReadFile(log)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !strings.Contains(string(raw), "agents update agent-1 --file -") {
				t.Errorf("argv is %q; the body must arrive on stdin, not on the command line", raw)
			}
			body, bodyErr := os.ReadFile("/tmp/unmute-attach-body.json")
			if bodyErr != nil {
				t.Fatal(bodyErr)
			}
			if !strings.Contains(string(body), `"`+tc.field+`":"trunk-9"`) {
				t.Errorf("the body is %q, want only %s set", body, tc.field)
			}
			// One field and nothing else: a read-modify-write would race with
			// anything else editing that agent.
			if strings.Count(string(body), ":") != 1 {
				t.Errorf("the PATCH body carries more than one field: %q", body)
			}
		})
	}
}

func TestDeploySaysWhenNoNumberReachesTheAgent(t *testing.T) {
	stub := provisionedStub(`printf '[{"direction":"inbound","name":"1_inbound","numbers":["+447700900111"],"usable":true,"in_use_by":null},{"direction":"inbound","name":"broken","numbers":[],"usable":false,"unavailable_reason":"no numbers assigned","in_use_by":null}]'`)
	_, out, _, err := deployWithStub(t, stub)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if !strings.Contains(out, "no number reaches this agent yet") {
		t.Errorf("the run does not say the agent is unreachable:\n%s", out)
	}
	if !strings.Contains(out, "1_inbound") {
		t.Errorf("the run does not name a trunk that could be attached:\n%s", out)
	}
}

// TestDeploySurvivesAnUnreadableTrunkListing. The agent is live either way. An
// unreadable listing says nothing about whether a call would connect, so it is a
// warning and never a deploy failure.
func TestDeploySurvivesAnUnreadableTrunkListing(t *testing.T) {
	stub := provisionedStub(`printf 'error: forbidden\n' >&2; exit 1`)
	_, out, errOut, err := deployWithStub(t, stub)
	if err != nil {
		t.Fatalf("an unreadable trunk listing failed a successful deploy: %v", err)
	}
	if !strings.Contains(out, "deployed") {
		t.Errorf("the deploy did not report success:\n%s", out)
	}
	if !strings.Contains(errOut, "could not be read") {
		t.Errorf("the run does not say the numbers are unknown:\n%s", errOut)
	}
	if strings.Contains(out, "no number reaches this agent yet") {
		t.Error("an unreadable listing was reported as an answer: there may well be a number")
	}
}

// TestDeployRelaysTheTrunkAdvisory. The platform withholds a trunk that is both
// unusable and attached to no agent, so a listing is never a complete inventory.
// The account says so on its error stream and the run relays it as prose.
func TestDeployRelaysTheTrunkAdvisory(t *testing.T) {
	stub := provisionedStub(`printf 'note: a trunk attached to no agent is not visible here.\n' >&2; printf '[]'`)
	_, _, errOut, err := deployWithStub(t, stub)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if !strings.Contains(errOut, "not visible here") {
		t.Errorf("the account's advisory was dropped:\n%s", errOut)
	}
}

// TestDeployPlacesNoCallUnlessAsked. A call costs money and rings a real phone,
// so a successful deploy must never imply one.
func TestDeployPlacesNoCallUnlessAsked(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	stub := `printf '%s\n' "$*" >> ` + log + "\n" + provisionedStub(`printf '[]'`)

	if _, _, _, err := deployWithStub(t, stub); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the stub logged nothing: %v", err)
	}
	if strings.Contains(string(raw), "calls dispatch") {
		t.Errorf("a plain deploy placed a phone call:\n%s", raw)
	}
}

// And with --call it rings, naming the agent id the push returned.
func TestDeployPlacesTheCallWhenAsked(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	stub := `printf '%s\n' "$*" >> ` + log + "\n" + provisionedStub(`printf '[]'`)

	_, out, _, err := deployWithStub(t, stub, "--call", "+447700900123")
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "agents calls dispatch agent-1 --phone +447700900123") {
		t.Errorf("the call was not dispatched to the pushed agent:\n%s", raw)
	}
	if !strings.Contains(out, "calling +447700900123") {
		t.Errorf("the run does not say it is calling:\n%s", out)
	}
}

// TestDeployKeepsASuccessfulDeployWhenTheCallFails. The agent deployed. A call
// that would not connect is worth saying and is not a reason to report the
// deploy as failed.
func TestDeployKeepsASuccessfulDeployWhenTheCallFails(t *testing.T) {
	stub := `case "$*" in
  *"calls dispatch"*) printf 'error: no outbound trunk\n' >&2; exit 1 ;;
esac
` + provisionedStub(`printf '[]'`)

	_, out, errOut, err := deployWithStub(t, stub, "--call", "+447700900123")
	if err != nil {
		t.Fatalf("a failed test call failed the deploy: %v", err)
	}
	if !strings.Contains(out, "deployed") {
		t.Errorf("the deploy did not report success:\n%s", out)
	}
	if !strings.Contains(errOut, "the agent deployed, but the test call") {
		t.Errorf("the failed call is not reported as separate from the deploy:\n%s", errOut)
	}
}

// A dry run created nothing, so there is nothing to reach and nothing to call.
func TestDeployDryRunReportsNoReachAndPlacesNoCall(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	stub := `printf '%s\n' "$*" >> ` + log + "\n" + provisionedStub(`printf '[]'`)

	_, out, _, err := deployWithStub(t, stub, "--dry-run", "--call", "+447700900123")
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	raw, _ := os.ReadFile(log)
	for _, unwanted := range []string{"calls dispatch", "trunks list"} {
		if strings.Contains(string(raw), unwanted) {
			t.Errorf("a dry run ran `voiceai %s` against an agent it did not create", unwanted)
		}
	}
	if strings.Contains(out, "no number reaches this agent yet") {
		t.Error("a dry run reported on the reachability of an agent that was not created")
	}
}

// GATE (FR-028). A trunk the account reports as unusable is never offered as a
// choice, and is still named with the reason the account gave.
//
// Both halves matter and this test exists because both were lost once: the
// assertion for the reason was dropped when the interactive chooser replaced the
// passive listing, and `candidates` never filtered on `usable` at all, so a
// broken trunk was offered as a real option. Attaching one gives you a number
// that does not ring, which is worse than being told there is none.
func TestDeployNeverOffersAnUnusableTrunk(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	stub := `printf '%s\n' "$*" >> ` + log + "\n" + provisionedStub(`printf '[{"direction":"inbound","id":"t-broken","name":"broken_inbound","numbers":[],"usable":false,"unavailable_reason":"no numbers assigned","in_use_by":null}]'`)

	_, out, _, err := deployWithStub(t, stub)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	// Named, with the account's own reason, so an author knows a trunk exists and
	// why it will not do.
	if !strings.Contains(out, "broken_inbound") {
		t.Errorf("an unusable free trunk was hidden entirely:\n%s", out)
	}
	if !strings.Contains(out, "no numbers assigned") {
		t.Errorf("the account's reason was dropped:\n%s", out)
	}
	// But not offered, and not counted as something to choose.
	if !strings.Contains(out, "no usable free inbound trunk") {
		t.Errorf("a broken trunk was counted as attachable:\n%s", out)
	}
	if raw, readErr := os.ReadFile(log); readErr == nil && strings.Contains(string(raw), "agents update") {
		t.Error("an unusable trunk was attached")
	}
}

// The same rule one level down, so it holds even if reportReach's filtering is
// ever rewritten: a candidate list is a list of things that would work.
func TestOfferTrunkShowsTheReasonOnAnythingItLists(t *testing.T) {
	runner, _ := fillRunner(t, "exit 0")
	var out, errOut bytes.Buffer
	// Non-interactive, which is the branch that used to drop the suffix.
	offerTrunk(strings.NewReader(""), &out, &errOut, runner, "slng", "agent-1",
		[]slngTrunk{{ID: "t", Direction: "inbound", Name: "odd_one", Usable: false, UnavailableReason: "carrier rejected it"}}, false)

	if !strings.Contains(out.String(), "carrier rejected it") {
		t.Errorf("the non-interactive listing drops the reason a trunk will not work:\n%s", out.String())
	}
	// A trunk with no numbers reads as a formatting bug unless it says so.
	if !strings.Contains(out.String(), "no number") {
		t.Errorf("a trunk with no numbers renders as an empty field:\n%s", out.String())
	}
}

// GATE (Constitution III). The `resources` command has one owner, so a rename
// cannot leave a diagnostic pointing at a command that does not exist.
func TestResourcesCommandNameHasOneOwner(t *testing.T) {
	if got, want := resourcesCommandName(), "unmute "+newResourcesCmd().Name(); got != want {
		t.Errorf("resourcesCommandName() = %q, want %q", got, want)
	}
	root := newRootCmd()
	var found bool
	for _, sub := range root.Commands() {
		if "unmute "+sub.Name() == resourcesCommandName() {
			found = true
		}
	}
	if !found {
		t.Errorf("%q is not a command the root tree has", resourcesCommandName())
	}
}

// GATE (FR-005). The organisation is named once per target.
//
// It was printed twice for a while: once by the preflight from `whoami`, once
// by the push from its own result. Two identical lines read as two accounts,
// which is the exact confusion FR-005 exists to prevent. T021 asked for this
// test and it was never written, so the duplicate shipped.
func TestDeployNamesTheOrganisationExactlyOnce(t *testing.T) {
	_, out, errOut, err := deployWithStub(t, provisionedStub(`printf '[]'`))
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if got := strings.Count(out+errOut, "organisation "); got != 1 {
		t.Errorf("the organisation is named %d times, want once:\n%s%s", got, out, errOut)
	}
}

// But a real difference is not a duplicate. The preflight reads with the
// resolved credential and the push runs as its own process; if those land in
// different organisations then everything just checked was about somewhere else,
// and saying nothing would be the worst possible outcome.
func TestDeployWarnsWhenThePushLandsElsewhere(t *testing.T) {
	stub := `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"org-CHECKED","org_name":"Checked"}}' ;;
  *"agents push"*) printf '{"ok":true,"organisation":{"id":"org-WRITTEN","name":"Written"},"agent":{"id":"agent-1","name":"slng-tools-slng","action":"create"},"version":"unchanged"}' ;;
  *"tool list"*) printf '[{"name":"end_call","tool_type":"end_call"}]' ;;
  *"secret list"*) printf '[{"name":"REFUND_API_TOKEN","kind":"secret","has_value":true},{"name":"ACME_BRAND","kind":"variable","has_value":true}]' ;;
  *"mcp list"*) printf '[{"name":"internal_docs","capability_status":"healthy"}]' ;;
  *"mcp tools"*) printf '[{"name":"search_docs"},{"name":"read_doc"}]' ;;
  *) printf '[]' ;;
esac`
	_, _, errOut, err := deployWithStub(t, stub)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if !strings.Contains(errOut, "different organisations") {
		t.Errorf("a push that landed in another organisation was not reported:\n%s", errOut)
	}
	if !strings.Contains(errOut, "not what was written") {
		t.Errorf("the warning does not say what the consequence is:\n%s", errOut)
	}
}
