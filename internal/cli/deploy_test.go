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
  "agent": { "name": "acme-support-slng", "action": "create" },
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
  "agent": { "id": "01998a7c", "name": "acme-support-slng", "action": "update" },
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
	if plan.Agent.Name != "acme-support-slng" || plan.Agent.Action != "create" {
		t.Errorf("plan agent = %+v, want name acme-support-slng action create", plan.Agent)
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
	if blocked.Agent.Name != "" {
		t.Errorf("a blocked check names no agent, got %q", blocked.Agent.Name)
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
	if err := printPushResult(&out, &errOut, "slng", "build/slng", target.SlngRouterKeyEnv, decodePush(t, pushOutcomeJSON)); err != nil {
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
		err := printPushResult(&out, &errOut, "slng", "build/slng", "", decodePush(t, raw))
		if err == nil {
			t.Errorf("%s: printPushResult returned nil, so unmute would exit 0 after printing:\n%s", name, errOut.String())
		}
	}
	var out, errOut bytes.Buffer
	if err := printPushResult(&out, &errOut, "slng", "build/slng", target.SlngRouterKeyEnv, decodePush(t, pushPlanJSON)); err != nil {
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
