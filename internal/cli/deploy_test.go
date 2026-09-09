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
	return deployFixture(t, "slng_tools", input, script, args...)
}

// deployFixture is deployWithInput over a named package, for a check that needs
// a different one. The hosted-tool findings need the hosted fixture, because
// slng_tools declares no `slng:` tool and so produces no hosted requirement to
// compare.
func deployFixture(t *testing.T, fixture, input, script string, args ...string) (dir, out, errOut string, err error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub voiceai is a POSIX shell script")
	}
	dir = t.TempDir()
	source := filepath.Join("..", "testdata", fixture)
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

// contractStub is the answer a compatible `voiceai` gives to the support probe,
// and every stub below carries it.
//
// It is a separate constant rather than a line repeated eight times because it
// is the one thing a stub cannot omit: `unmute deploy` reads
// `resolution_contract` before it writes, and a stub that answers `[]` is an
// older CLI as far as the run is concerned. That refusal is correct, and it is
// what TestDeployRefusesAnIncompatiblePushTool holds.
//
// The `--require-resolved` case has to come before the plain `agents push` case
// in a shell `case`, because the first match wins and every guarded push
// contains both strings.
const contractStub = `  *"agents push"*"--require-resolved"*) printf '{"ok":true,"dry_run":true,"resolution_contract":1,"organisation":{"id":"org-1","name":"Example"},"agent":{"id":"agent-1","action":"create"}}' ;;`

// resolvedToolStub answers the two ID-addressed reads a hosted reference needs:
// the metadata that says whose tool it is, and the immutable published version
// that carries the contract a binding is checked against.
//
// One tool per call, so a fixture with two hosted references composes two of
// these. The id is derived from the name so a test can read the argv log and
// see which tool was asked about.
func resolvedToolStub(name, id, version, schema string) string {
	return resolvedToolStubIn("org-1", name, id, version, schema)
}

// resolvedToolStubIn is resolvedToolStub for an organisation other than org-1.
// The organisation matters: a record belonging to one account and a run
// resolving another is a refusal, so a stub whose ids disagree with its own
// `whoami` refuses every reference, which is correct and confusing to debug.
func resolvedToolStubIn(organisation, name, id, version, schema string) string {
	return `  *"tool get ` + id + ` --id"*) printf '{"id":"` + id + `","organisation_id":"` + organisation + `","name":"` + name + `","tool_type":"code","source":"org","latest_version":` + version + `}' ;;
  *"tool get ` + id + ` --version"*) printf '{"tool_id":"` + id + `","version_number":` + version + `,"content_hash":"h-` + id + `","published_at":"2026-09-01T00:00:00Z","snapshot_json":{"name":"` + name + `","tool_type":"code","description":"Published description of ` + name + `.","declared_secrets":[],"argument_schema":` + schema + `}}' ;;`
}

// openSchema is a published parameter schema that accepts the injected
// arguments the fixtures use.
const openSchema = `{"type":"object","properties":{"query":{"type":"string"},"channel":{"type":"string"},"order_number":{"type":"string"}},"additionalProperties":false}`

// A stub whose account has nothing the fixture needs.
//
// It still answers the support probe, because an incompatible CLI and an empty
// account are two different refusals and this test is about the second.
const emptyAccountStub = `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"org-1","org_name":"Example"}}' ;;
` + contractStub + `
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

// TestDeployPreflightDegrades. A read that could not be made is never counted
// as satisfied, and whether it stops the run depends on one thing: whether the
// push would check it again.
//
// That split is what spec 007 changed, and it changed it because the guarded
// push does not re-resolve. This test holds the tolerant half. The vault is the
// case where the old rule still holds: the push checks every vault entry itself
// and returns a `vault_missing` blocker, so an unreadable `secret list` costs
// nothing but a warning, and refusing here would refuse a run that would have
// been refused later with a worse message.
//
// TestDeployRefusesWhenAResolvedCheckCannotBeMade holds the other half.
func TestDeployPreflightDegrades(t *testing.T) {
	// Everything the package needs is resolvable, except that the vault cannot
	// be listed. The run must warn, reach the push, and say what it skipped.
	stub := `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"org-1","org_name":"Example"}}' ;;
  *"secret list"*) printf 'error: insufficient scope\n' >&2; exit 1 ;;
  *"tool list"*) printf '[{"id":"t-end_call","scope":"global","name":"end_call","tool_type":"end_call","latest_version":1},{"id":"t-check_order","scope":"organisation","name":"check_order","tool_type":"code","latest_version":1},{"id":"t-refund","scope":"organisation","name":"refund","tool_type":"api_request","latest_version":2}]' ;;
` + resolvedToolStub("check_order", "t-check_order", "1", openSchema) + `
` + resolvedToolStub("refund", "t-refund", "2", openSchema) + `
  *"mcp list"*) printf '[{"id":"s-internal_docs","name":"internal_docs","capability_status":"healthy"}]' ;;
  *"mcp get s-internal_docs"*) printf '{"id":"s-internal_docs","name":"internal_docs","capability_status":"healthy","capability_observed_at":"2026-09-08T10:00:00Z","next_refresh_at":"2099-01-01T00:00:00Z","capabilities":{"truncated":false,"tools":[{"name":"search_docs","schema_hash":"h-search"},{"name":"read_doc","schema_hash":"h-read"}]}}' ;;
  *"mcp tools"*) printf '[{"name":"search_docs"},{"name":"read_doc"}]' ;;
` + contractStub + `
  *) printf '[]' ;;
esac`
	_, out, errOut, err := deployWithStub(t, stub, "--dry-run")
	if err != nil {
		t.Fatalf("an unreadable vault listing failed the deploy: %v\n%s", err, errOut)
	}
	if !strings.Contains(errOut, "insufficient scope") {
		t.Errorf("the skipped read is not reported:\n%s", errOut)
	}
	// What covers the gap is said per requirement, not as a blanket clause on
	// the warning line: the guarded push re-checks the package's own declared
	// vault names against a fresh read and nothing else, so a sentence claiming
	// it covers every unread listing was untrue of most of them.
	if !strings.Contains(out+errOut, "still refused there") {
		t.Errorf("nothing says what covers this gap:\n%s\n%s", out, errOut)
	}
	if !strings.Contains(out, "dry run") {
		t.Errorf("the run did not reach the push:\n%s", out)
	}
}

// TestDeployRefusesWhenAResolvedCheckCannotBeMade is the inverse of the test
// above, and the two have to be read together.
//
// `unmute deploy` promises to attach the published version it validated, and it
// keeps that promise by handing the push an explicit tool id and version under
// `--require-resolved`. The push then attaches exactly that and checks nothing
// for itself. So a listing this run could not read is not an early warning that
// the push covers: it is an attachment nobody verified, and there is no later
// step that would catch it.
//
// This used to warn and reach the push, which is how a deployment could report
// success having verified nothing. The tolerance it kept is the one above.
func TestDeployRefusesWhenAResolvedCheckCannotBeMade(t *testing.T) {
	for _, tc := range []struct {
		name, stub string
		want       []string
	}{
		{
			name: "the tool listing could not be read",
			stub: `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"org-1","org_name":"Example"}}' ;;
  *"tool list"*) printf 'error: insufficient scope\n' >&2; exit 1 ;;
  *"secret list"*) printf '[]' ;;
` + contractStub + `
  *) printf '[]' ;;
esac`,
			want: []string{"Cannot deploy", "hosted tool", "could not be listed", "no version was checked"},
		},
		{
			name: "one published version could not be read",
			stub: `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"org-1","org_name":"Example"}}' ;;
  *"tool list"*) printf '[{"id":"t-check_order","scope":"organisation","name":"check_order","tool_type":"code","latest_version":1},{"id":"t-search","scope":"organisation","name":"search_places_text","tool_type":"api_request","latest_version":3}]' ;;
  *"tool get t-check_order --version"*) printf 'error: gateway timeout\n' >&2; exit 1 ;;
  *"tool get"*"--id"*) printf '{"id":"t-check_order","organisation_id":"org-1","name":"check_order","source":"org","latest_version":1}' ;;
  *"secret list"*) printf '[]' ;;
` + contractStub + `
  *) printf '[]' ;;
esac`,
			want: []string{"Cannot deploy", "published contract was not checked"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, _, errOut, err := deployFixture(t, "slng_hosted", "", tc.stub, "--dry-run")
			if err == nil {
				t.Fatal("a reference whose version could not be checked reached the push, which would attach it unverified")
			}
			for _, want := range tc.want {
				if !strings.Contains(errOut, want) {
					t.Errorf("the refusal does not say %q:\n%s", want, errOut)
				}
			}
			if _, statErr := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(statErr) {
				t.Error("a run refused over an unchecked reference still wrote a build directory")
			}
		})
	}
}

// The two hosted-tool checks the deploy makes, and the fact they are two
// different severities is the whole point.
//
// The name has to exist: unmute creates no tool, so a reference the
// organisation cannot resolve stops the run, because the push would refuse
// anyway. The version does not: the agent calls the platform's copy either way,
// so a moved tool is a package whose mirror no longer describes what runs, not a
// deploy that will fail. Conflating them would either wave through a name that
// cannot resolve or block a deploy over a stale note.

// hostedAccountStub answers with an organisation that holds both hosted tools
// and every vault entry the fixture needs. version is a parameter, so one stub
// serves the matching and the moved cases.
func hostedAccountStub(version string) string {
	return `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"org-1","org_name":"Example"}}' ;;
  *"tool list"*) printf '[{"id":"t-end_call","scope":"global","name":"end_call","tool_type":"end_call","latest_version":1},{"id":"t-check_order","scope":"organisation","name":"check_order","tool_type":"code","latest_version":1},{"id":"t-search","scope":"organisation","name":"search_places_text","tool_type":"api_request","latest_version":` + version + `}]' ;;
` + resolvedToolStub("check_order", "t-check_order", "1", openSchema) + `
` + resolvedToolStub("search_places_text", "t-search", version, openSchema) + `
  *"secret list"*) printf '[{"name":"SLNG_TOOL_RENDER","kind":"secret","has_value":true},{"name":"OPENAI_API_KEY","kind":"secret","has_value":true},{"name":"DEEPGRAM_API_KEY","kind":"secret","has_value":true},{"name":"SLNG_API_KEY","kind":"secret","has_value":true}]' ;;
` + contractStub + `
  *) printf '[]' ;;
esac`
}

// TestHostedDriftWarnsAndDoesNotBlock: the committed mirror was taken from
// version 3 and the organisation is at 4.
func TestHostedDriftWarnsAndDoesNotBlock(t *testing.T) {
	_, out, errOut, err := deployFixture(t, "slng_hosted", "", hostedAccountStub("4"), "--dry-run")
	if err != nil {
		t.Fatalf("a moved hosted tool failed the deploy, and it must not: %v\n%s", err, errOut)
	}
	for _, want := range []string{
		"warning:",
		// search_places_text is the tool the stub moved: its mirror was taken
		// from version 3 and the organisation is at 4. check_order is at 1 in
		// both, which is what the next test checks stays quiet.
		"search_places_text",
		"version 4",
		"version 3",
		"run `unmute pull`",
		// The second clause is the important half: the deploy is going ahead,
		// so the reader needs to know what they are choosing.
		"the agent will call the organisation's version",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("the drift warning does not say %q:\n%s", want, errOut)
		}
	}
	if !strings.Contains(out, "dry run") {
		t.Errorf("the run did not reach the push:\n%s", out)
	}
}

// TestHostedVersionMatchWarnsAboutNothing is the other half, and it matters as
// much: a check that warns on a package that is up to date is a check people
// learn to ignore.
func TestHostedVersionMatchWarnsAboutNothing(t *testing.T) {
	_, out, errOut, err := deployFixture(t, "slng_hosted", "", hostedAccountStub("3"), "--dry-run")
	if err != nil {
		t.Fatalf("deploy failed on a package whose mirrors match: %v\n%s", err, errOut)
	}
	if strings.Contains(errOut, "version") {
		t.Errorf("a matching mirror still produced a version warning:\n%s", errOut)
	}
	if !strings.Contains(out, "satisfied") {
		t.Errorf("the preflight did not report the hosted references as satisfied:\n%s", out)
	}
}

// TestHostedReferenceTheOrganisationLacksBlocks. unmute creates no tool, so this
// is the case that has to stop rather than warn, and the message has to say the
// dashboard is where a tool is born.
func TestHostedReferenceTheOrganisationLacksBlocks(t *testing.T) {
	stub := `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"org-1","org_name":"Example"}}' ;;
  *"tool list"*) printf '[{"name":"end_call","tool_type":"end_call","latest_version":1},{"name":"check_order_v2","tool_type":"code","latest_version":1}]' ;;
  *) printf '[]' ;;
esac`
	dir, _, errOut, err := deployFixture(t, "slng_hosted", "", stub, "--dry-run")
	if err == nil {
		t.Fatal("a hosted reference the organisation does not hold reached the push")
	}
	for _, want := range []string{
		"Cannot deploy",
		"hosted tool",
		"check_order",
		// The fix names the line the author edits. For a legacy block that is
		// the `slng:` line to write the hosted name on, or the file to rename;
		// for a scalar it is the name on that line. It is no longer "the tool
		// file's own name", because the file's name is not what resolves a
		// reference any more.
		"`slng:` line",
		"create the tool in the SLNG dashboard",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, errOut)
		}
	}
	// The near miss, which is the most common way this goes wrong.
	if !strings.Contains(errOut, "check_order_v2") {
		t.Errorf("the refusal does not name the near miss:\n%s", errOut)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(statErr) {
		t.Error("a refused preflight still wrote a build directory")
	}
}

// TestHostedDriftDegradesWhenToolsCannotBeListed was replaced by
// TestDeployRefusesWhenAResolvedCheckCannotBeMade above.
//
// It held that an unreadable tool listing must warn and reach the push, on the
// grounds that the push was the authority on which version to attach. Under
// spec 007 it is not: the push attaches the version this run resolved and
// checks none of it, so tolerating that read means attaching a version nobody
// looked at. The fact the old test protected, that an unmade read is never
// counted as satisfied, is still held: it is now a refusal rather than a pass.

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

// provisionedContract is every ID-addressed read the slng_tools fixture needs,
// for one organisation: the two hosted tools' identities and published
// versions, the MCP server's capability record, and the live agent.
//
// Shared, because three stubs need the same eight lines and a stub that is
// missing one refuses the reference it belongs to with a message about the
// account rather than about the stub.
func provisionedContract(organisation string) string {
	return resolvedToolStubIn(organisation, "check_order", "t-check_order", "1", openSchema) + `
` + resolvedToolStubIn(organisation, "refund", "t-refund", "2", openSchema) + `
  *"mcp get s-internal_docs"*) printf '{"id":"s-internal_docs","name":"internal_docs","transport":"streamable_http","capability_status":"healthy","capability_observed_at":"2026-09-08T10:00:00Z","next_refresh_at":"2099-01-01T00:00:00Z","capabilities":{"truncated":false,"tools":[{"name":"search_docs","schema_hash":"h-search"},{"name":"read_doc","schema_hash":"h-read"}]}}' ;;
`
}

// provisionedAgent is a live agent with nothing attached, which is what a first
// deployment reads. It is separate from provisionedContract because a stub that
// wants a POPULATED agent has to answer `agents get` itself, and a shell case
// takes the first match: leaving an empty answer in the shared block would
// silently win over the case that test wrote.
const provisionedAgent = `  *"agents get"*) printf '{"id":"agent-1","organisation_id":"o","name":"slng-tools-fixture-slng","tool_refs":[],"mcp_refs":[]}' ;;`

// provisionedCatalogue is the tool listing the same fixture needs, with the ids
// and scopes resolution reads.
const provisionedCatalogue = `  *"tool list"*) printf '[{"id":"t-end_call","scope":"global","name":"end_call","tool_type":"end_call","latest_version":1},{"id":"t-check_order","scope":"organisation","name":"check_order","tool_type":"code","latest_version":1},{"id":"t-refund","scope":"organisation","name":"refund","tool_type":"api_request","latest_version":2}]' ;;
  *"mcp list"*) printf '[{"id":"s-internal_docs","name":"internal_docs","capability_status":"healthy"}]' ;;
  *"mcp tools"*) printf '[{"name":"search_docs"},{"name":"read_doc"}]' ;;
  *"secret list"*) printf '[{"name":"REFUND_API_TOKEN","has_value":true},{"name":"ACME_BRAND","has_value":true}]' ;;`

// A stub whose account holds everything the slng_tools fixture needs, so the run
// reaches the push and the steps after it. `trunks` is left to each caller.
func provisionedStub(trunks string) string {
	return `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"o","org_name":"Example"}}' ;;
  *"agents push"*"--dry-run"*) printf '{"ok":true,"dry_run":true,"resolution_contract":1,"organisation":{"id":"o","name":"Example"},"agent":{"id":"agent-1","action":"create"}}' ;;
  *"agents push"*"--require-resolved"*) printf '{"ok":true,"resolution_contract":1,"organisation":{"id":"o","name":"Example"},"agent":{"id":"agent-1","action":"create"},"version":"unchanged"}' ;;
  *"tool list"*) printf '[{"id":"t-end_call","scope":"global","name":"end_call","tool_type":"end_call","latest_version":1},{"id":"t-check_order","scope":"organisation","name":"check_order","tool_type":"code","latest_version":1},{"id":"t-refund","scope":"organisation","name":"refund","tool_type":"api_request","latest_version":2}]' ;;
` + resolvedToolStubIn("o", "check_order", "t-check_order", "1", openSchema) + `
` + resolvedToolStubIn("o", "refund", "t-refund", "2", openSchema) + `
  *"secret list"*) printf '[{"name":"REFUND_API_TOKEN","has_value":true},{"name":"ACME_BRAND","has_value":true}]' ;;
  *"mcp list"*) printf '[{"id":"s-internal_docs","name":"internal_docs","capability_status":"healthy"}]' ;;
  *"mcp get s-internal_docs"*) printf '{"id":"s-internal_docs","name":"internal_docs","transport":"streamable_http","capability_status":"healthy","capability_observed_at":"2026-09-08T10:00:00Z","next_refresh_at":"2099-01-01T00:00:00Z","capabilities":{"truncated":false,"tools":[{"name":"search_docs","schema_hash":"h-search"},{"name":"read_doc","schema_hash":"h-read"}]}}' ;;
  *"mcp tools"*) printf '[{"name":"search_docs"},{"name":"read_doc"}]' ;;
` + provisionedAgent + `
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
  *"agents push"*"--dry-run"*) printf '{"ok":true,"dry_run":true,"resolution_contract":1,"organisation":{"id":"org-CHECKED","name":"Checked"},"agent":{"id":"agent-1","action":"create"}}' ;;
  *"agents push"*) printf '{"ok":true,"resolution_contract":1,"organisation":{"id":"org-WRITTEN","name":"Written"},"agent":{"id":"agent-1","name":"slng-tools-slng","action":"create"},"version":"unchanged"}' ;;
` + provisionedCatalogue + `
` + provisionedContract("org-CHECKED") + `
  *"agents get"*) printf '{"id":"agent-1","organisation_id":"org-CHECKED","name":"slng-tools-fixture-slng","tool_refs":[],"mcp_refs":[]}' ;;
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

// TestDryRunMakesNoRemoteChange is the read-only promise, held by refusing
// every mutating command rather than by inspecting output.
//
// The recorder answers reads and exits non-zero on anything that writes,
// discovers, fills a secret, dispatches a call or runs a tool. So a dry run
// that reached any of those fails here with the command it tried, which is a
// better failure than an assertion about a line that happened not to be
// printed.
//
// Two paths matter most, because both are writes that happen while checking
// rather than while deploying: the vault fill and the MCP capability refresh.
// A preview must reach neither, and a terminal being available must not change
// that.
func TestDryRunMakesNoRemoteChange(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	// Every read the fixture needs, and a refusal for everything else. The
	// catch-all comes last, so an unexpected command is a failure rather than
	// an empty list that decodes.
	recorder := `printf '%s\n' "$*" >> ` + log + `
case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"o","org_name":"Example"}}' ;;
  *"agents push"*"--dry-run"*) printf '{"ok":true,"dry_run":true,"resolution_contract":1,"organisation":{"id":"o","name":"Example"},"agent":{"id":"agent-1","action":"update"}}' ;;
` + provisionedCatalogue + `
` + provisionedContract("o") + `
` + provisionedAgent + `
  *"mcp run"*) printf 'a dry run connected to a server\n' >&2; exit 1 ;;
  *"secret create"*) printf 'a dry run created a vault entry\n' >&2; exit 1 ;;
  *"agents calls dispatch"*) printf 'a dry run placed a call\n' >&2; exit 1 ;;
  *"tool run"*) printf 'a dry run ran a tool\n' >&2; exit 1 ;;
  *"agents update"*) printf 'a dry run changed an agent\n' >&2; exit 1 ;;
  *"agents push"*) printf 'a dry run pushed without the guard\n' >&2; exit 1 ;;
  *"trunks list"*) printf '[]' ;;
  *) printf 'a dry run ran an unexpected command\n' >&2; exit 1 ;;
esac`
	// A terminal's worth of input, so a run that would have asked to fill a
	// secret gets a yes. It must not ask.
	_, out, errOut, err := deployWithInput(t, "y\ny\ny\n", recorder, "--dry-run", "--run-samples")
	if err != nil {
		t.Fatalf("the dry run failed: %v\n%s", err, errOut)
	}

	raw, readErr := os.ReadFile(log)
	if readErr != nil {
		t.Fatalf("the recorder logged nothing: %v", readErr)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		for _, forbidden := range []string{
			"mcp run", "secret create", "tool run", "agents update", "agents calls dispatch",
		} {
			if strings.Contains(line, forbidden) {
				t.Errorf("a dry run ran %q, which changes remote state: %s", forbidden, line)
			}
		}
		// Every push a dry run makes carries both guards. Without --dry-run it
		// would write; without --require-resolved it would resolve names again.
		if strings.Contains(line, "agents push") {
			for _, needed := range []string{"--dry-run", "--require-resolved"} {
				if !strings.Contains(line, needed) {
					t.Errorf("a push under --dry-run is missing %s: %s", needed, line)
				}
			}
		}
	}
	if !strings.Contains(out, "dry run") {
		t.Errorf("the run did not reach the preview:\n%s", out)
	}
	// --run-samples was passed and must have stayed read-only: it is forwarded
	// to the push, which keeps it inert under --dry-run, and it never becomes a
	// `tool run` here.
	if strings.Contains(string(raw), "confirm-side-effects") {
		t.Error("--run-samples under --dry-run executed a tool against real dependencies")
	}
}

// TestDryRunTouchesNoAuthoredFileOrMirror: the other half of read-only. A
// preview writes into build/ and nowhere else, so every authored byte and every
// committed mirror is identical afterwards.
func TestDryRunTouchesNoAuthoredFileOrMirror(t *testing.T) {
	stub := provisionedStub(`printf '[]'`)
	dir, _, errOut, err := deployWithStub(t, stub, "--dry-run")
	if err != nil {
		t.Fatalf("the dry run failed: %v\n%s", err, errOut)
	}
	before := map[string][]byte{}
	source := filepath.Join("..", "testdata", "slng_tools")
	entries, readErr := os.ReadDir(source)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(source, entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		before[entry.Name()] = content
	}
	tools, readErr := os.ReadDir(filepath.Join(source, "tools"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range tools {
		content, readErr := os.ReadFile(filepath.Join(source, "tools", entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		before[filepath.Join("tools", entry.Name())] = content
	}
	for name, want := range before {
		got, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			t.Errorf("%s is gone after a dry run: %v", name, readErr)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("%s changed during a dry run, and a preview edits nothing an author wrote", name)
		}
	}
}

// TestDeployWritesTheReportAndNamesTheVersionsItAttached: the two things a run
// leaves behind. The report is the detail, and stdout names the versions,
// because "it deployed" without a version is not an answer to "which one is
// running".
func TestDeployWritesTheReportAndNamesTheVersionsItAttached(t *testing.T) {
	stub := provisionedStub(`printf '[]'`)
	dir, out, errOut, err := deployWithStub(t, stub)
	if err != nil {
		t.Fatalf("deploy: %v\n%s", err, errOut)
	}
	for _, want := range []string{"attached check_order v1", "attached refund v2"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout does not say %q, so a reader cannot tell which version is running:\n%s", want, out)
		}
	}

	raw, readErr := os.ReadFile(filepath.Join(dir, "build", "slng", "deploy-report.json"))
	if readErr != nil {
		t.Fatalf("no deployment report was written: %v", readErr)
	}
	var report deployReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("the report is not readable: %v", err)
	}
	if report.Outcome != "deployed" {
		t.Errorf("outcome = %q, want deployed", report.Outcome)
	}
	if report.DryRun {
		t.Error("a real deploy wrote a report marked as a dry run")
	}
	if report.AgentID == "" {
		t.Error("the report does not name the agent that was written")
	}
	byTool := map[string]int{}
	for _, row := range report.Tools {
		byTool[row.Tool] = row.ProposedVersion
	}
	if byTool["check_order"] != 1 || byTool["refund"] != 2 {
		t.Errorf("the report does not carry the versions attached: %+v", report.Tools)
	}
	// Every check has a state, so "how much of this was actually verified" is
	// answerable from the file.
	if len(report.Checks) == 0 {
		t.Error("the report names no checks, so a reader cannot tell what was verified")
	}
	for _, check := range report.Checks {
		if check.State == "" {
			t.Errorf("check %q has no state", check.Name)
		}
	}
}

// TestDeployRefusesAnIncompatiblePushTool. The installed CLI at the time this
// feature was written does not implement the guarded contract, and a run that
// silently fell back to a name-only push would resolve every name again and
// attach whatever was newest, having reported a different version as checked.
//
// So an unsupported tool is a refusal with upgrade guidance, before any write.
func TestDeployRefusesAnIncompatiblePushTool(t *testing.T) {
	// Everything resolvable, and a push that answers the way 0.1.16 does: it
	// rejects the unknown option before doing anything.
	stub := `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"o","org_name":"Example"}}' ;;
` + provisionedCatalogue + `
` + provisionedContract("o") + `
` + provisionedAgent + `
  *"agents push"*"--require-resolved"*) printf "error: unknown option '--require-resolved'\n" >&2; exit 1 ;;
  *) printf '[]' ;;
esac`
	dir, _, _, err := deployWithStub(t, stub, "--dry-run")
	if err == nil {
		t.Fatal("an incompatible push tool was accepted, so the run would attach a version it never checked")
	}
	for _, want := range []string{"--require-resolved", "not the version this run checked", "brew install"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
	// And it refused before writing: the probe runs after the account's own
	// gaps are reported and before the build directory is created.
	if _, statErr := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(statErr) {
		t.Error("a run refused over an incompatible push tool still wrote a build directory")
	}
}

// TestDeployReadsTheAgentBeforeItReplacesIt.
//
// The bug this exists to stop was in the first version of this flow: the live
// agent was read after the real push, so the "previous version" in every report
// was the version that push had just attached, and a preview of an update
// showed no change at all.
//
// So a real deploy makes two pushes. One dry, which changes nothing and returns
// the agent it would replace; then the read; then the real one. The recorder
// below answers `agents get` with an agent at v1 and only ONCE, before the
// writing push, so a read afterwards would see the stub's post-push answer.
func TestDeployReadsTheAgentBeforeItReplacesIt(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	stub := `printf '%s\n' "$*" >> ` + log + `
case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"o","org_name":"Example"}}' ;;
  *"agents push"*"--dry-run"*) printf '{"ok":true,"dry_run":true,"resolution_contract":1,"organisation":{"id":"o","name":"Example"},"agent":{"id":"agent-1","action":"update"}}' ;;
  *"agents push"*) printf '{"ok":true,"resolution_contract":1,"organisation":{"id":"o","name":"Example"},"agent":{"id":"agent-1","action":"update"},"version":{"number":9,"label":"l"}}' ;;
` + provisionedCatalogue + `
` + provisionedContract("o") + `
  *"agents get"*) printf '{"id":"agent-1","organisation_id":"o","name":"slng-tools-fixture-slng","tool_refs":[{"attachment_id":"att-1","tool_id":"t-check_order","version":1,"invocation":"system","description":"Edited in the dashboard.","system":{"triggers":[{"event":"call_start"}],"arguments":[{"name":"caller","type":"string"}]}}],"mcp_refs":[]}' ;;
  *"trunks list"*) printf '[]' ;;
  *) printf '[]' ;;
esac`
	dir, out, errOut, err := deployWithStub(t, stub)
	if err != nil {
		t.Fatalf("deploy: %v\n%s", err, errOut)
	}

	raw, readErr := os.ReadFile(log)
	if readErr != nil {
		t.Fatalf("the recorder logged nothing: %v", readErr)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var readAt, wroteAt = -1, -1
	for i, line := range lines {
		if strings.Contains(line, "agents get") && readAt < 0 {
			readAt = i
		}
		// The writing push: guarded and not a dry run.
		if strings.Contains(line, "agents push") && strings.Contains(line, "--require-resolved") &&
			!strings.Contains(line, "--dry-run") {
			wroteAt = i
		}
	}
	if readAt < 0 {
		t.Fatalf("the agent was never read, so the report's previous versions are invented:\n%s", raw)
	}
	if wroteAt < 0 {
		t.Fatalf("no writing push was made:\n%s", raw)
	}
	if readAt > wroteAt {
		t.Errorf("the agent was read after it was replaced, so every previous version in the report is the version just attached:\n%s", raw)
	}

	var report deployReport
	content, readErr := os.ReadFile(filepath.Join(dir, "build", "slng", "deploy-report.json"))
	if readErr != nil {
		t.Fatalf("no report: %v", readErr)
	}
	if err := json.Unmarshal(content, &report); err != nil {
		t.Fatal(err)
	}
	var row deployReportTool
	for _, candidate := range report.Tools {
		if candidate.Tool == "check_order" {
			row = candidate
		}
	}
	if row.PreviousVersion == nil {
		t.Fatalf("the report carries no previous version for an attachment the agent already had: %+v", report.Tools)
	}
	if *row.PreviousVersion != 1 {
		t.Errorf("the previous version is %d, want 1: the agent had v1 before this run", *row.PreviousVersion)
	}
	// And the two dashboard settings a replacement destroys are named, which is
	// the whole reason the baseline is read at all.
	joined := strings.Join(row.Changes, "\n")
	for _, want := range []string{"description", "trigger", "invocation"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the report does not say a replacement removes the %s the agent has now:\n%s", want, joined)
		}
	}
	if !strings.Contains(out, "attached check_order v1") {
		t.Errorf("stdout does not name the version attached:\n%s", out)
	}
}

// TestRealDeployRefreshesAStaleSnapshotOnceAndSaysSo.
//
// A stored MCP capability snapshot goes stale, and the only way to renew it is
// the server's own discovery handshake, which changes remote state. So the rule
// has three parts and each is a separate way to get it wrong: a real deploy
// refreshes, it refreshes at most once per server, and it says it did.
//
// The last part is the one that would rot silently. A run that refreshed and
// then failed a later check must not report that nothing changed, because
// something on SLNG is different.
func TestRealDeployRefreshesAStaleSnapshotOnceAndSaysSo(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	// The first `mcp get` answers stale, the connect succeeds, and the reread
	// answers fresh. A counter file is how the stub tells the two gets apart.
	counter := filepath.Join(t.TempDir(), "gets")
	stale := `{"id":"s-internal_docs","name":"internal_docs","capability_status":"healthy","capability_observed_at":"2020-01-01T00:00:00Z","next_refresh_at":"2020-01-01T00:05:00Z","capabilities":{"truncated":false,"tools":[{"name":"search_docs","schema_hash":"h-old"},{"name":"read_doc","schema_hash":"h-old"}]}}`
	fresh := `{"id":"s-internal_docs","name":"internal_docs","capability_status":"healthy","capability_observed_at":"2099-01-01T00:00:00Z","next_refresh_at":"2099-01-01T00:05:00Z","capabilities":{"truncated":false,"tools":[{"name":"search_docs","schema_hash":"h-new"},{"name":"read_doc","schema_hash":"h-new"}]}}`
	stub := `printf '%s\n' "$*" >> ` + log + `
case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"o","org_name":"Example"}}' ;;
  *"agents push"*"--dry-run"*) printf '{"ok":true,"dry_run":true,"resolution_contract":1,"organisation":{"id":"o","name":"Example"},"agent":{"id":"agent-1","action":"create"}}' ;;
  *"agents push"*) printf '{"ok":true,"resolution_contract":1,"organisation":{"id":"o","name":"Example"},"agent":{"id":"agent-1","action":"create"},"version":"unchanged"}' ;;
` + provisionedCatalogue + `
` + resolvedToolStubIn("o", "check_order", "t-check_order", "1", openSchema) + `
` + resolvedToolStubIn("o", "refund", "t-refund", "2", openSchema) + `
  *"mcp get s-internal_docs"*)
    if [ -f ` + counter + ` ]; then printf '%s' '` + fresh + `'; else touch ` + counter + `; printf '%s' '` + stale + `'; fi ;;
  *"mcp run s-internal_docs"*) printf '{"status":"connected"}' ;;
` + provisionedAgent + `
  *"trunks list"*) printf '[]' ;;
  *) printf '[]' ;;
esac`
	dir, _, errOut, err := deployWithStub(t, stub)
	if err != nil {
		t.Fatalf("a stale snapshot failed the deploy instead of being refreshed: %v\n%s", err, errOut)
	}

	raw, readErr := os.ReadFile(log)
	if readErr != nil {
		t.Fatalf("the recorder logged nothing: %v", readErr)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var runs int
	for _, line := range lines {
		if strings.Contains(line, "mcp run") {
			runs++
		}
	}
	if runs == 0 {
		t.Error("a real deploy did not refresh a stale snapshot, so the push would refuse it")
	}
	if runs > 1 {
		t.Errorf("a real deploy connected to one server %d times; the budget is one", runs)
	}
	// Every read is by id, so a rename or a name reused for a second server
	// between the check and the write cannot redirect it.
	for _, line := range lines {
		if strings.Contains(line, "mcp run") || strings.Contains(line, "mcp get") {
			if !strings.Contains(line, "s-internal_docs") || !strings.Contains(line, "--id") {
				t.Errorf("an MCP read is addressed by name rather than by id: %s", line)
			}
		}
	}

	// The refreshed hash is what reached the report, not the stale one it was
	// checked against first.
	content, readErr := os.ReadFile(filepath.Join(dir, "build", "slng", "deploy-report.json"))
	if readErr != nil {
		t.Fatalf("no report: %v", readErr)
	}
	var report deployReport
	if err := json.Unmarshal(content, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.MCP) == 0 {
		t.Fatal("the report carries no MCP selection")
	}
	for _, row := range report.MCP {
		if row.SchemaHash != "h-new" {
			t.Errorf("%s carries hash %q, want the refreshed h-new: attaching the stale one is what the guarded push refuses", row.Tool, row.SchemaHash)
		}
		if !row.Refreshed {
			t.Errorf("%s is not marked as refreshed, so a later failure could claim nothing changed", row.Tool)
		}
	}
	// And the run says out loud that it changed something on SLNG.
	var said bool
	for _, effect := range report.SideEffects {
		if strings.Contains(effect, "internal_docs") && strings.Contains(effect, "refreshed") {
			said = true
		}
	}
	if !said {
		t.Errorf("the report does not record the refresh as a side effect: %v", report.SideEffects)
	}
}

// TestDeployReadsEachResourceOnce is the cost gate for the reads this feature
// added, and it is the reason resolution caches inside the run.
//
// The listings were already held to one read per kind. Resolution reads per
// resource instead: one identity and one published version per tool, one record
// per server, one agent.
//
// The overlap it has to survive is resolution and the baseline wanting the same
// published version: resolution reads the version it is about to attach, and the
// baseline reads the version the agent has now, which are the same read whenever
// a package is deployed twice with nothing changed. That is the case below.
//
// This used to prove the cache with an alias, two package files naming one
// hosted tool. That is now refused, and rightly: an attachment is keyed by tool
// id, so two references to one id cannot keep two sets of settings. A caching
// test is not the place to settle what a package may declare, so the alias moved
// to TestDeployRefusesTwoReferencesToOneHostedTool and this one keeps the reads.
func TestDeployReadsEachResourceOnce(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "slng_tools"))); err != nil {
		t.Fatal(err)
	}

	log := filepath.Join(t.TempDir(), "calls.log")
	// An agent already holding check_order at the version this package
	// resolves, so resolution and the baseline ask for one published version.
	attached := `  *"agents get"*) printf '{"id":"agent-1","organisation_id":"o","name":"slng-tools-fixture-slng","tool_refs":[{"attachment_id":"a-1","tool_id":"t-check_order","version":1}],"mcp_refs":[]}' ;;`
	stub := `printf '%s\n' "$*" >> ` + log + `
case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"o","org_name":"Example"}}' ;;
  *"agents push"*"--dry-run"*) printf '{"ok":true,"dry_run":true,"resolution_contract":1,"organisation":{"id":"o","name":"Example"},"agent":{"id":"agent-1","action":"update"}}' ;;
` + provisionedCatalogue + `
` + provisionedContract("o") + `
` + attached + `
  *) printf '[]' ;;
esac`
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "voiceai"), []byte("#!/bin/sh\n"+stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(target.SlngRouterKeyEnv, "test-key")

	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"deploy", dir, "--dry-run"})
	if err := root.Execute(); err != nil {
		t.Fatalf("deploy: %v\n%s", err, stderr.String())
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the recorder logged nothing: %v", err)
	}
	counts := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		counts[line]++
	}
	for line, count := range counts {
		if count > 1 {
			t.Errorf("%q ran %d times; every read in a deployment is made once", line, count)
		}
	}
	// And the overlap really happened, or the cache was never asked anything.
	var versionReads int
	for line := range counts {
		if strings.Contains(line, "tool get t-check_order") && strings.Contains(line, "--version 1") {
			versionReads++
		}
	}
	if versionReads != 1 {
		t.Errorf("check_order v1 was read %d times, and resolution and the baseline both want it", versionReads)
	}
}

// TestDeployRefusesTwoReferencesToOneHostedTool.
//
// SLNG keys an attachment by tool id. Two package files naming one hosted tool
// therefore cannot hold two descriptions, two invocations or two `inject:` sets:
// the push reuses one attachment and whichever reference is written second
// decides all of it, with the other's settings gone and nothing said.
//
// So it is refused before anything is staged, and the refusal names both files,
// because "which two of my tools are the same tool" is the only question the
// author has and only the filenames answer it.
func TestDeployRefusesTwoReferencesToOneHostedTool(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "slng_tools"))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tools", "order_status.yaml"),
		[]byte("slng: check_order\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(dir, "agent.yaml")
	raw, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	// The agent's own list is indented six spaces and the package catalogue two,
	// so the deeper one is replaced first and the shallower replacement is
	// anchored on its own indent.
	body := strings.Replace(string(raw), "      - check_order\n", "      - check_order\n      - order_status\n", 1)
	body = strings.Replace(body, "\ntools:\n  - check_order\n", "\ntools:\n  - check_order\n  - order_status\n", 1)
	if strings.Count(body, "order_status") != 2 {
		t.Fatalf("the fixture's shape changed, so this test no longer attaches the alias:\n%s", body)
	}
	if err := os.WriteFile(agentPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	log := filepath.Join(t.TempDir(), "calls.log")
	stub := `printf '%s\n' "$*" >> ` + log + `
case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"o","org_name":"Example"}}' ;;
  *"agents push"*"--dry-run"*) printf '{"ok":true,"dry_run":true,"resolution_contract":1,"organisation":{"id":"o","name":"Example"},"agent":{"id":"agent-1","action":"update"}}' ;;
` + provisionedCatalogue + `
` + provisionedContract("o") + `
` + provisionedAgent + `
  *) printf '[]' ;;
esac`
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "voiceai"), []byte("#!/bin/sh\n"+stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(target.SlngRouterKeyEnv, "test-key")

	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"deploy", dir, "--dry-run"})
	err = root.Execute()
	if err == nil {
		t.Fatalf("two references to one hosted tool were accepted, so one file's settings would be dropped silently:\n%s", stdout.String())
	}
	said := stdout.String() + stderr.String() + err.Error()
	for _, want := range []string{"tools/check_order.yaml", "tools/order_status.yaml", "check_order"} {
		if !strings.Contains(said, want) {
			t.Errorf("the refusal does not name %q, so the author cannot tell which two files collide:\n%s", want, said)
		}
	}
	// Refused before staging: no push of any kind was attempted.
	logged, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the recorder logged nothing: %v", err)
	}
	if strings.Contains(string(logged), "agents push") {
		t.Errorf("the run reached a push after refusing the collision:\n%s", logged)
	}
}

// TestARealDeployPrintsNoRoutineAdvisory.
//
// A deferred check is a fact, not an action: "this value arrives when a call
// starts" names nothing the reader has to do, and it is true of every deploy of
// every package that injects a variable. Printing it on a real run is the shape
// this repository's output rule exists to stop, where twenty lines of
// nothing-to-do bury the two that matter.
//
// So a real run prints what it did, and the dry run describes what would happen.
// Both put the detail in the report either way.
func TestARealDeployPrintsNoRoutineAdvisory(t *testing.T) {
	stub := provisionedStub(`printf '[]'`)
	dir, out, errOut, err := deployWithStub(t, stub)
	if err != nil {
		t.Fatalf("deploy: %v\n%s", err, errOut)
	}
	if strings.Contains(errOut, "which SLNG resolves when a call starts") {
		t.Errorf("a real deploy printed a deferred-check note:\n%s", errOut)
	}
	if strings.Contains(out, "would change") {
		t.Errorf("a real deploy printed a preview:\n%s", out)
	}
	// It printed what it did, which is the version of each tool it attached.
	if !strings.Contains(out, "attached") {
		t.Errorf("a real deploy did not say what it attached:\n%s", out)
	}

	// The dry run does print it, because describing what would happen is its
	// whole job.
	_, dryOut, dryErr, err := deployWithStub(t, stub, "--dry-run")
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, dryErr)
	}
	if strings.Contains(dryOut, "attached") {
		t.Errorf("a dry run claimed to have attached something:\n%s", dryOut)
	}

	// And either way every check reaches the report with its state, which is
	// what makes keeping the note off a real run's output lossless. Which
	// states appear depends on the package; that the report carries them at all
	// is what this asserts, and TestReportChecksSeparateSatisfiedFromDeferredFromBlocked
	// holds the three apart.
	content, readErr := os.ReadFile(filepath.Join(dir, "build", "slng", "deploy-report.json"))
	if readErr != nil {
		t.Fatalf("no report: %v", readErr)
	}
	var report deployReport
	if err := json.Unmarshal(content, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Checks) == 0 {
		t.Error("the report of a real deploy names no check at all, so the detail the output leaves out is nowhere")
	}
	for _, check := range report.Checks {
		if check.State == "" || check.Source == "" {
			t.Errorf("check %q reaches the report with no state or no source: %+v", check.Name, check)
		}
	}
}

// TestDeployReportsNoPhantomRemovalOfAnAuthoredAnnouncement.
//
// The whole-run half of the announcement comparison, and the shape a real
// guarded dry run got wrong: it said a replacement would remove "the message
// the agent speaks before this tool runs, which the agent has now and this
// package cannot declare" about a package whose own tool file declares that
// sentence. `announce:` is an authoring key, it compiles to the attachment's
// execution_policy.pre_action_message, and the preview had the live policy with
// nothing to compare it against, so every one read as a removal.
//
// The fixture's check_order.yaml declares `announce: One moment while I look
// that up.`, so the agent below carries exactly that and the run must say
// nothing about it. The second half changes one word, because a preview that
// stopped reading the policy at all would pass the first half on its own.
func TestDeployReportsNoPhantomRemovalOfAnAuthoredAnnouncement(t *testing.T) {
	attachedSaying := func(spoken string) string {
		return `  *"agents get"*) printf '{"id":"agent-1","organisation_id":"o","name":"slng-tools-fixture-slng","tool_refs":[` +
			`{"attachment_id":"att-1","tool_id":"t-check_order","version":1,"invocation":"model",` +
			`"execution_policy":{"pre_action_message":{"enabled":true,"wait":false,"text":{"segments":[{"type":"literal","value":"` +
			spoken + `"}]}}}}],"mcp_refs":[]}' ;;`
	}
	changesFor := func(t *testing.T, spoken string) string {
		t.Helper()
		stub := `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"o","org_name":"Example"}}' ;;
  *"agents push"*"--dry-run"*) printf '{"ok":true,"dry_run":true,"resolution_contract":1,"organisation":{"id":"o","name":"Example"},"agent":{"id":"agent-1","action":"update"}}' ;;
` + provisionedCatalogue + `
` + provisionedContract("o") + `
` + attachedSaying(spoken) + `
  *"trunks list"*) printf '[]' ;;
  *) printf '[]' ;;
esac`
		dir, _, errOut, err := deployWithStub(t, stub, "--dry-run")
		if err != nil {
			t.Fatalf("deploy --dry-run: %v\n%s", err, errOut)
		}
		content, readErr := os.ReadFile(filepath.Join(dir, "build", "slng", "deploy-report.json"))
		if readErr != nil {
			t.Fatalf("no report: %v", readErr)
		}
		var report deployReport
		if err := json.Unmarshal(content, &report); err != nil {
			t.Fatal(err)
		}
		for _, row := range report.Tools {
			if row.Tool == "check_order" {
				return strings.Join(row.Changes, "\n")
			}
		}
		t.Fatalf("the report carries no row for check_order: %+v", report.Tools)
		return ""
	}

	if changes := changesFor(t, "One moment while I look that up."); strings.Contains(changes, "pre_action_message") {
		t.Errorf("a replacement writing this package's own `announce:` back was reported as removing it:\n%s", changes)
	}
	changed := changesFor(t, "Hold on.")
	if !strings.Contains(changed, `execution_policy.pre_action_message, from "Hold on." to this package's `+"`announce:`") {
		t.Errorf("a different sentence on the agent was not reported as a change:\n%s", changed)
	}
}
