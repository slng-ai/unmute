package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateCommandPrintsEveryTargetAndWarnings(t *testing.T) { // V16, V18
	stdout, stderr, err := runValidateCommand(t, filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"✓ livekit (livekit)", "✓ pipecat (pipecat)"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "Warnings:\n") || strings.Contains(stderr, "Errors:\n") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestValidateCommandFiltersTargetInstances(t *testing.T) { // V18
	stdout, _, err := runValidateCommand(t, "--target", "pipecat", filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "✓ pipecat (pipecat)") || strings.Contains(stdout, "livekit") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestValidateCommandReturnsErrorForGatedTarget(t *testing.T) { // V16
	dir := filepath.Join(t.TempDir(), "agent")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "safe_core"))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A gate a remaining driver actually has: Pipecat does not emit per-task
	// models. This used to use Vapi's thinking-audio gate, which retired with
	// the target. The property under test is unchanged — a gated target prints
	// ✗ on stdout and its reason under Errors on stderr, and exits non-zero.
	content = bytes.Replace(content, []byte("  max_duration: 20m"), []byte("  max_duration: 20m\n  thinking_audio: subtle"), 1)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runValidateCommand(t, "--target", "pipecat", dir)
	if err == nil || !strings.Contains(stdout, "✗ pipecat (pipecat)") || !strings.Contains(stderr, "Errors:\n") {
		t.Fatalf("err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
}

// US3: an account feature the provider grants on request must be named before an
// author spends money proving it is missing. Cold transfer on the Daily route
// dials the destination, which needs Daily dial-out enabled on the domain.
//
// Exit code stays 0. A prerequisite is a fact about the route, not a defect in
// the package: unmute compiles it perfectly and cannot know what the author's
// Daily account is allowed to do. Failing here would refuse correct packages.
func TestValidateNamesTheDailyDialOutPrerequisite(t *testing.T) {
	stdout, stderr, err := runValidateCommand(t, "--target", "pipecat",
		filepath.Join("..", "testdata", "daily_carrier"))
	if err != nil {
		t.Fatalf("a prerequisite must not fail validation: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "✓ pipecat (pipecat)") {
		t.Errorf("stdout = %q, want the target still passing", stdout)
	}
	for _, want := range []string{"dial-out", "daily_dialout"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q, so the author learns about it from a failed call:\n%s", want, stderr)
		}
	}
	// The author has to be able to act on it, so the page it came from is named.
	if !strings.Contains(stderr, "https://docs.pipecat.ai/") {
		t.Errorf("stderr names no documentation for the prerequisite:\n%s", stderr)
	}
}

// The other half of the rule, and the easy half to get wrong. A prerequisite
// that prints on every Daily compile is a banner, and a banner trains authors to
// ignore stderr.
func TestValidateOmitsPrerequisiteWithoutTheCapabilityThatNeedsIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "daily_carrier"))); err != nil {
		t.Fatal(err)
	}
	// Same Daily route, no transfer and no outbound: nothing dials out.
	path := filepath.Join(dir, "agent.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Cut the escalation, the list that attaches it, and the destination it
	// resolves to. Written as whole-block substitutions rather than byte offsets,
	// because an offset into a fixture is a stack trace waiting for whoever edits
	// the fixture next.
	trimmed := content
	for _, block := range []string{
		"    escalations:\n      - send_to_billing\n",
		"escalations:\n  send_to_billing:\n    when: The caller asks about an invoice or a refund.\n    cold:\n      destination: billing_line\n\n",
		"destinations:\n  billing_line: BILLING_PHONE_NUMBER\n\n",
	} {
		if !bytes.Contains(trimmed, []byte(block)) {
			t.Fatalf("fixture no longer contains the block this test removes:\n%s", block)
		}
		trimmed = bytes.Replace(trimmed, []byte(block), nil, 1)
	}
	trimmed = bytes.Replace(trimmed, []byte("    outbound: true\n"), []byte("    outbound: false\n"), 1)
	if err := os.WriteFile(path, trimmed, 0o600); err != nil {
		t.Fatal(err)
	}
	// The carrier route remains inbound, but nothing in the package dials out.
	_, stderr, err := runValidateCommand(t, "--target", "pipecat", dir)
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, stderr)
	}
	for _, forbidden := range []string{"dial-out", "daily_dialout"} {
		if strings.Contains(stderr, forbidden) {
			t.Errorf("stderr names %q for a package that never dials out:\n%s", forbidden, stderr)
		}
	}
}

// US4: a region unmute cannot honour fails before any artifact exists, naming the
// problem and the fix.
//
// Note what is deliberately absent. A region *code* is never checked against a
// list: codes are forwarded exactly as written, the platform CLI is the
// validator, and no list of codes lives in this
// repository, because both platforms change theirs without notice. So the
// refusals here are the three that are knowable without one: an empty entry, the
// same region twice, and more than one region on a platform whose agent names are
// globally unique across regions.
func TestValidateRefusesARegionItCannotHonour(t *testing.T) {
	for _, tc := range []struct {
		name    string
		region  string
		wantAny []string
	}{
		{"several regions", "[us-east, eu-central]", []string{"globally unique across regions"}},
		{"same region twice", "[us-east, us-east]", []string{`lists "us-east" twice`}},
		{"empty entry", `[""]`, []string{"empty entry"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeDailyPackage(t, tc.region)
			_, stderr, err := runValidateCommand(t, "--target", "pipecat", dir)
			if err == nil {
				t.Fatalf("a region unmute cannot honour must fail, got exit 0\n%s", stderr)
			}
			for _, want := range tc.wantAny {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr missing %q:\n%s", want, stderr)
				}
			}
		})
	}
}

// A region code unmute does not recognise still compiles, because the platform is
// the validator. Pinned as a test so nobody "helpfully" adds a region allow-list:
// the platforms add regions outside our release cycle, and a list here would
// refuse a package that is correct.
func TestValidateForwardsAnUnknownRegionCode(t *testing.T) {
	dir := writeDailyPackage(t, "moon-base-1")
	_, stderr, err := runValidateCommand(t, "--target", "pipecat", dir)
	if err != nil {
		t.Fatalf("an unrecognised region code must be forwarded, not refused: %v\n%s", err, stderr)
	}
}

// writeDailyPackage copies the internal Daily fixture and rewrites its region.
func writeDailyPackage(t *testing.T, region string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "agent")
	source := filepath.Join("..", "testdata", "daily_carrier")
	if err := os.CopyFS(dir, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "targets.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = bytes.Replace(content, []byte("    deployment_region: us-west\n"),
		[]byte("    deployment_region: "+region+"\n"), 1)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestValidateRefusesAPastedMCPSecret is SC-005 at the command: a token pasted
// into an mcp block instead of named by an environment variable fails
// validation, before any artifact exists, and the message says what is wrong
// without repeating the value back.
func TestValidateRefusesAPastedMCPSecret(t *testing.T) {
	const pasted = "fc-live-pretend-key"
	dir := filepath.Join(t.TempDir(), "agent")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "safe_core"))); err != nil {
		t.Fatal(err)
	}
	tool := "mcp:\n  url_env: FIRECRAWL_MCP_URL\n  transport: streamable_http\n" +
		"  auth:\n    type: bearer\n    token_env: " + pasted + "\n"
	if err := os.WriteFile(filepath.Join(dir, "tools", "web_search.yaml"), []byte(tool), 0o600); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(dir, "agent.yaml")
	content, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	// Declared at the top level and attached to the entry agent. Declaring it
	// without attaching it is now its own refusal, and it would fire first.
	content = bytes.Replace(content, []byte("tools:\n  - lookup_customer"), []byte("tools:\n  - web_search\n  - lookup_customer"), 1)
	content = bytes.Replace(content, []byte("      - lookup_customer"), []byte("      - web_search\n      - lookup_customer"), 1)
	if err := os.WriteFile(agentPath, content, 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runValidateCommand(t, "--target", "livekit", dir)
	if err == nil {
		t.Fatalf("a pasted secret must fail validation: stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stderr, "token_env must be an UPPER_SNAKE environment variable name") {
		t.Errorf("the message must say what the field takes:\n%s", stderr)
	}
	if strings.Contains(stdout, pasted) || strings.Contains(stderr, pasted) {
		t.Error("validation must not echo the pasted secret back")
	}
}

func runValidateCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := newRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"validate"}, args...))
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// FR-037. A warning, on stderr, exit 0: the compiler cannot know the upstream
// model family for certain, so it says what it measured rather than refusing.
// Warnings never become a silent downgrade, which is why this asserts the exit
// code as well as the text.
func TestValidateWarnsOnRouterToolsWithoutReasoningEffort(t *testing.T) {
	dir := copySafeCore(t)
	path := filepath.Join(dir, "agent.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// safe_core's entry agent already carries tools, which is the trap: the
	// package validates, deploys, and then fails on every tool turn.
	router := `  think:
    fast_reasoning:
      provider: slng
      model: gpt-5.6-luna
      agent_id: safe-core-router-v1
      upstream:
        provider: openai
      params:
        world_part_override: eu
    careful_reasoning:
`
	text := mustReplace(t, string(raw), `  think:
    fast_reasoning:
      description: cheap and quick, greeting and routing
      provider: openai
      model: gpt-4o-mini
      temperature: 0.4
    careful_reasoning:
`, router)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := runValidateCommand(t, "--target", "pipecat", dir)
	if err != nil {
		t.Fatalf("a missing reasoning_effort must warn, never fail: %v\n%s", err, stderr)
	}
	for _, want := range []string{
		"Warnings:\n", "reasoning_effort", "400", "Function tools with reasoning_effort are not supported",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the warning does not say %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "Errors:\n") {
		t.Errorf("the warning was raised to an error:\n%s", stderr)
	}
}
