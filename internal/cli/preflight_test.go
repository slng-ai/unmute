package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// The comparison is pure, so these are table-driven with no subprocess and no
// network. That is deliberate: the rules that make this feature safe rather than
// merely useful are all decisions, not I/O, and a rule held behind a stub is a
// rule nobody rewrites a test for.

// account is a stock set of resources standing in for a provisioned
// organisation. Modelled on the captured fixtures: three curated tools, a vault
// holding one of each kind, one healthy MCP server and one whose probe failed.
func account() slngResources {
	return slngResources{
		Account: slngAccount{OK: true, Profile: "default"},
		Tools: []slngAccountTool{
			{Name: "end_call", ToolType: "end_call"},
			{Name: "transfer_call", ToolType: "transfer_call"},
			{Name: "current_datetime", ToolType: "current_datetime"},
		},
		Vault: []slngVaultEntry{
			{Name: "ACME_API_KEY", Kind: "secret", HasValue: true},
			{Name: "BRAND_NAME", Kind: "variable", HasValue: true},
			{Name: "HALF_MADE_TOKEN", Kind: "secret", HasValue: false},
			{Name: "FIRECRAWL_API_KEY", Kind: "variable", HasValue: true},
		},
		MCPServer: []slngMCPServer{
			{Name: "firecrawl-mcp", CapabilityStatus: "healthy"},
			{Name: "flaky-mcp", CapabilityStatus: "error", CapabilityError: "probe timed out"},
		},
		MCPTools: map[string][]slngMCPTool{
			"firecrawl-mcp": {{Name: "firecrawl_scrape"}, {Name: "firecrawl_search"}},
			"flaky-mcp":     {},
		},
	}
}

func need(name string) generate.Requirement {
	return generate.Requirement{Name: name, Where: "tools/" + name + ".yaml asked for it"}
}

func onlyFinding(t *testing.T, requires generate.Requirements, resources slngResources) finding {
	t.Helper()
	report := comparePreflight(requires, resources)
	if len(report.Findings) != 1 {
		t.Fatalf("expected one finding, got %d: %+v", len(report.Findings), report.Findings)
	}
	return report.Findings[0]
}

// TestPreflightStates walks every state a requirement can end in. The states are
// the whole model: get one of them wrong and the report either refuses a deploy
// it should have allowed or allows one it should have refused.
func TestPreflightStates(t *testing.T) {
	for _, tc := range []struct {
		name     string
		requires generate.Requirements
		resource func(slngResources) slngResources
		want     findingState
		says     string
	}{
		{
			name:     "a builtin the account has",
			requires: generate.Requirements{Builtins: []generate.Requirement{need("end_call")}},
			want:     satisfied,
		},
		{
			name:     "a builtin the account does not have",
			requires: generate.Requirements{Builtins: []generate.Requirement{need("send_sms")}},
			want:     absent,
			says:     "SLNG dashboard",
		},
		{
			name:     "a secret that is present and populated",
			requires: generate.Requirements{Secrets: []generate.Requirement{need("ACME_API_KEY")}},
			want:     satisfied,
		},
		{
			name:     "a secret the vault does not hold",
			requires: generate.Requirements{Secrets: []generate.Requirement{need("REFUND_API_TOKEN")}},
			want:     absent,
			says:     "voiceai secret create REFUND_API_TOKEN",
		},
		{
			name:     "a secret that exists with no value",
			requires: generate.Requirements{Secrets: []generate.Requirement{need("HALF_MADE_TOKEN")}},
			want:     empty,
			says:     "--overwrite",
		},
		{
			name:     "a secret held under the other kind",
			requires: generate.Requirements{Secrets: []generate.Requirement{need("FIRECRAWL_API_KEY")}},
			want:     wrongKind,
			says:     "holds this name as a variable",
		},
		{
			name:     "a variable held as a secret",
			requires: generate.Requirements{Variables: []generate.Requirement{need("ACME_API_KEY")}},
			want:     wrongKind,
			says:     "needs a variable",
		},
		{
			name:     "a missing variable is created as a variable",
			requires: generate.Requirements{Variables: []generate.Requirement{need("GREETING_NAME")}},
			want:     absent,
			says:     "--kind variable",
		},
		{
			name:     "an MCP server the account has",
			requires: generate.Requirements{MCPServers: []generate.Requirement{need("firecrawl-mcp")}},
			want:     satisfied,
		},
		{
			name:     "an MCP server whose stored probe failed",
			requires: generate.Requirements{MCPServers: []generate.Requirement{need("flaky-mcp")}},
			want:     unhealthy,
			says:     "not a live call",
		},
		{
			name:     "an MCP server the account does not have",
			requires: generate.Requirements{MCPServers: []generate.Requirement{need("absent-mcp")}},
			want:     absent,
			says:     "attached in the SLNG dashboard",
		},
		{
			name: "an MCP tool the server offers",
			requires: generate.Requirements{MCPTools: []generate.Requirement{
				{Name: "firecrawl_scrape", Server: "firecrawl-mcp", Where: "tools/search.yaml exposes it"},
			}},
			want: satisfied,
		},
		{
			name: "an MCP tool the server does not offer",
			requires: generate.Requirements{MCPTools: []generate.Requirement{
				{Name: "firecrawl_crawl", Server: "firecrawl-mcp", Where: "tools/search.yaml exposes it"},
			}},
			want: absent,
			// FR-008: naming what the server does offer is the difference between
			// "wrong" and "here is the list to pick from".
			says: "firecrawl_search",
		},
		{
			name:     "an account with no MCP servers at all",
			requires: generate.Requirements{MCPServers: []generate.Requirement{need("firecrawl-mcp")}},
			resource: func(r slngResources) slngResources { r.MCPServer = nil; return r },
			want:     absent,
			says:     "no MCP servers attached at all",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resources := account()
			if tc.resource != nil {
				resources = tc.resource(resources)
			}
			found := onlyFinding(t, tc.requires, resources)
			if found.State != tc.want {
				t.Errorf("state is %v, want %v (detail: %s)", found.State, tc.want, found.Detail)
			}
			if tc.says != "" && !strings.Contains(found.Detail, tc.says) {
				t.Errorf("the detail %q does not say %q", found.Detail, tc.says)
			}
			if found.State != satisfied && strings.TrimSpace(found.Detail) == "" {
				t.Error("a finding that is not satisfied says nothing about what to do")
			}
		})
	}
}

// GATE. A builtin reference is checked by the tool *file's* name, not by the
// builtin id it selects.
//
// slngTools writes {"tool": <package tool name>}, so tools/hang_up.yaml
// declaring `builtin: {id: end_call}` emits a reference to "hang_up". Nothing
// refuses that at validate. If the preflight checked the id it would find
// end_call on the account, report everything fine, and let the push fail anyway,
// which is worse than not checking at all. The fix an author needs is to rename
// the file, so the report has to say that.
func TestPreflightChecksTheToolFileNameNotTheBuiltinID(t *testing.T) {
	found := onlyFinding(t, generate.Requirements{Builtins: []generate.Requirement{{
		Name:  "hang_up",
		Where: "tools/hang_up.yaml is a builtin",
	}}}, account())

	if found.State != absent {
		t.Fatalf("a builtin named for what it does rather than for the capability it selects was accepted (state %v)", found.State)
	}
	if !strings.Contains(found.Detail, "tool file's own name") {
		t.Errorf("the detail %q does not explain that the reference is the file name", found.Detail)
	}
}

// And the near-miss form of the same mistake, which is the one an author is most
// likely to make: right capability, wrong case.
func TestPreflightNamesTheCloseMatchItFound(t *testing.T) {
	found := onlyFinding(t, generate.Requirements{Builtins: []generate.Requirement{need("End_Call")}}, account())
	if found.NearMiss != "end_call" {
		t.Errorf("near miss is %q, want end_call", found.NearMiss)
	}
	if !strings.Contains(found.Detail, "rename tools/End_Call.yaml to tools/end_call.yaml") {
		t.Errorf("the detail %q does not name the rename that fixes it", found.Detail)
	}

	// Vault names are uppercase by convention, so the same trap sits there.
	found = onlyFinding(t, generate.Requirements{Secrets: []generate.Requirement{need("acme_api_key")}}, account())
	if found.NearMiss != "ACME_API_KEY" {
		t.Errorf("near miss is %q, want ACME_API_KEY", found.NearMiss)
	}
	if !strings.Contains(found.Detail, "case-sensitive") {
		t.Errorf("the detail %q does not say why an exact-looking name missed", found.Detail)
	}
}

// GATE. A name the account holds at two scopes is named once in a refusal.
//
// A tool name is unique per *scope*, not per organisation, so an account
// carrying SLNG's curated `end_call` and a second one somebody made in the
// dashboard lists that name twice. Relayed verbatim, the refusal reads "this
// organisation has `end_call`, `end_call`", which looks like a broken tool to
// the one reader who is already stuck. Live organisation, 2026-09-03: both
// `end_call` and `transfer_call` existed at global and organisation scope, which
// is why the captured fixture now carries both pairs.
func TestPreflightNamesADuplicatedAccountToolOnce(t *testing.T) {
	resources := account()
	resources.Tools = append(resources.Tools,
		slngAccountTool{Name: "end_call", ToolType: "end_call"},
		slngAccountTool{Name: "transfer_call", ToolType: "transfer_call"},
	)
	// A name that near-misses nothing, so the refusal takes the branch that lists
	// everything the account has.
	found := onlyFinding(t, generate.Requirements{Builtins: []generate.Requirement{need("hang_up")}}, resources)
	for _, name := range []string{"`end_call`", "`transfer_call`"} {
		if count := strings.Count(found.Detail, name); count != 1 {
			t.Errorf("the detail names %s %d times, want once: %s", name, count, found.Detail)
		}
	}
}

// GATE, inverted deliberately and in the same change as the contract it
// encodes, which is what the ratchet rule requires of a gate that starts
// failing for a good reason.
//
// It used to assert the opposite: a code or webhook tool was never reported as
// missing, because the push created it, so its absence was the expected state
// of a first deploy. That is no longer true. The slng target creates no tool:
// the platform CLI's `tool` group is list, get and run, with no create, so a
// name the organisation does not hold is the end of the road until somebody
// makes one in the dashboard. Reporting it early costs no compile and saves a
// refused push.
//
// Held at the derivation, so the gate compiles a real package rather than
// asserting over a hand-built value that could be wrong in the same way twice.
func TestPreflightReportsAHostedToolTheOrganisationLacks(t *testing.T) {
	requires := slngToolsRequirements(t)
	if len(requires.Hosted) != 2 {
		t.Fatalf("the fixture no longer references two hosted tools, so this gate proves nothing: %+v", requires.Hosted)
	}

	// An account with the curated capability and neither hosted tool.
	report := comparePreflight(requires, account())
	reported := map[string]findingState{}
	for _, found := range report.Findings {
		reported[found.Requirement.Name] = found.State
	}
	for _, name := range []string{"check_order", "refund"} {
		state, ok := reported[name]
		if !ok {
			t.Errorf("%q was not reported at all, and unmute creates no tool, so an absent one is a refusal", name)
			continue
		}
		if state != absent {
			t.Errorf("%q is %v, want absent", name, state)
		}
		if !state.blocks() {
			t.Errorf("%q is %v, which does not stop the run; the push would refuse it anyway", name, state)
		}
	}

	// The other half, and the one that keeps this from crying wolf: an account
	// that holds them is satisfied, with no warning about a version when the
	// mirrors match.
	held := account()
	held.Tools = append(held.Tools,
		slngAccountTool{Name: "check_order", ToolType: "code", LatestVersion: 1},
		slngAccountTool{Name: "refund", ToolType: "api_request", LatestVersion: 2},
	)
	for _, found := range comparePreflight(requires, held).Findings {
		if found.Kind != "hosted tool" {
			continue
		}
		if found.State != satisfied {
			t.Errorf("%q is %v against an account that holds it at the pinned version, want satisfied: %s",
				found.Requirement.Name, found.State, found.Detail)
		}
	}

	// And a moved one warns rather than blocks, because the agent calls the
	// platform's copy either way.
	moved := account()
	moved.Tools = append(moved.Tools,
		slngAccountTool{Name: "check_order", ToolType: "code", LatestVersion: 9},
		slngAccountTool{Name: "refund", ToolType: "api_request", LatestVersion: 2},
	)
	for _, found := range comparePreflight(requires, moved).Findings {
		if found.Requirement.Name != "check_order" || found.Kind != "hosted tool" {
			continue
		}
		if found.State != stale {
			t.Errorf("a moved hosted tool is %v, want stale", found.State)
		}
		if found.State.blocks() {
			t.Error("a moved hosted tool stops the deploy, and it must not: the agent calls the platform's copy either way")
		}
		if !strings.Contains(found.Detail, "version 9") || !strings.Contains(found.Detail, "version 1") {
			t.Errorf("the drift detail does not name both versions: %s", found.Detail)
		}
	}
}

// GATE. A check that could not be made warns and never counts as satisfied.
//
// This is the rule that stops the feature from becoming a new outage. An old
// `voiceai`, a restricted CI network or a missing read scope must not turn into
// a refused deploy, and must equally not turn into a silent pass.
func TestPreflightTreatsAnUnmadeCheckAsNeitherPassNorFail(t *testing.T) {
	resources := account()
	resources.Vault = nil
	resources.Unchecked = []*unchecked{{
		Command: target.SlngSecretList.String(),
		Reason:  "insufficient scope",
	}}

	report := comparePreflight(generate.Requirements{
		Secrets:  []generate.Requirement{need("ACME_API_KEY")},
		Builtins: []generate.Requirement{need("end_call")},
	}, resources)

	var vaultFinding, toolFinding finding
	for _, found := range report.Findings {
		switch found.Kind {
		case "secret":
			vaultFinding = found
		case "builtin tool":
			toolFinding = found
		}
	}
	if vaultFinding.State != notChecked {
		t.Errorf("a requirement from a listing that was never read is %v; an unread listing is not an absence", vaultFinding.State)
	}
	if report.satisfiedCount() != 1 || toolFinding.State != satisfied {
		t.Error("a failed vault read suppressed the tool check, which was answered")
	}
	if len(report.blocked()) != 0 {
		t.Error("an unmade check stopped the deploy; only a positive absence may do that")
	}

	// And it has to be visible. A skipped check that prints nothing is the report
	// claiming a check it never made.
	var out, errOut bytes.Buffer
	if err := renderPreflight(&out, &errOut, "slng", report); err != nil {
		t.Fatalf("render returned an error for a report with no blocking finding: %v", err)
	}
	if !strings.Contains(errOut.String(), "insufficient scope") {
		t.Errorf("the skipped read is not reported: %q", errOut.String())
	}
	// And what covers the gap is said per requirement rather than as one
	// blanket clause on the warning line. It used to end "the push decides what
	// it would have covered", about every unread listing, which was untrue of
	// most of them: the guarded push re-checks the package's own declared vault
	// names and nothing else. The finding is where the answer belongs, because
	// it is a different answer per requirement.
	said := out.String() + errOut.String()
	if !strings.Contains(said, "still refused there") {
		t.Errorf("nothing says what covers this gap: %q", said)
	}
	if strings.Contains(said, "the push decides") {
		t.Errorf("the blanket claim is back, and it is untrue of most reads: %q", said)
	}
}

// GATE. The report never lists a vault entry the package did not name.
//
// The verified organisation holds 44 entries, 42 of them platform-managed. A
// report that enumerated the vault would bury the two lines an author has to act
// on, and would leak the shape of an organisation into a package's build output.
func TestPreflightReportsOnlyWhatThePackageAskedFor(t *testing.T) {
	report := comparePreflight(generate.Requirements{
		Secrets: []generate.Requirement{need("REFUND_API_TOKEN")},
	}, account())

	var out, errOut bytes.Buffer
	err := renderPreflight(&out, &errOut, "slng", report)
	if err == nil {
		t.Fatal("a missing secret did not stop the run")
	}
	printed := out.String() + errOut.String()
	for _, unrelated := range []string{"ACME_API_KEY", "BRAND_NAME", "HALF_MADE_TOKEN", "FIRECRAWL_API_KEY"} {
		if strings.Contains(printed, unrelated) {
			t.Errorf("the report names %q, which this package never asked for", unrelated)
		}
	}
	if !strings.Contains(printed, "REFUND_API_TOKEN") {
		t.Error("the report does not name the secret the package did ask for")
	}
}

// FR-014 and SC-001. Four gaps across four resource kinds produce one report
// naming all four. Stopping at the first would be the behaviour this whole
// feature exists to replace: a sequence of refusals, one per attempt.
func TestPreflightReportsEveryGapInOnePass(t *testing.T) {
	report := comparePreflight(generate.Requirements{
		Builtins:   []generate.Requirement{need("send_sms")},
		MCPServers: []generate.Requirement{need("absent-mcp")},
		Secrets:    []generate.Requirement{need("REFUND_API_TOKEN")},
		Variables:  []generate.Requirement{need("GREETING_NAME")},
	}, account())

	if got := len(report.blocked()); got != 4 {
		t.Fatalf("%d blocking findings, want 4: %+v", got, report.blocked())
	}

	var out, errOut bytes.Buffer
	if err := renderPreflight(&out, &errOut, "slng", report); err == nil {
		t.Fatal("four missing things did not stop the run")
	}
	printed := errOut.String()
	for _, name := range []string{"send_sms", "absent-mcp", "REFUND_API_TOKEN", "GREETING_NAME"} {
		if !strings.Contains(printed, name) {
			t.Errorf("the report does not name %q, so the author would find it on the next attempt instead", name)
		}
	}
	// Grouped, so four problems do not read as one wall.
	for _, kind := range []string{"builtin tool", "mcp server", "secret", "variable"} {
		if !strings.Contains(printed, kind+" (1)") {
			t.Errorf("the report does not group %q with a count", kind)
		}
	}
	if !strings.Contains(printed, "nothing was compiled, created or changed") {
		t.Error("the report does not say the account is unchanged, which is the thing an author most needs to know after a refusal")
	}
}

// FR-006b. A control produces no finding, because the body carries no reference
// to it and there is nothing an account read could confirm or deny.
func TestPreflightIgnoresControls(t *testing.T) {
	// slng_tools declares a transfer control; the emitted body references only
	// the four tools. Whatever the account has, no finding may mention it.
	requires := slngToolsRequirements(t)
	for _, found := range comparePreflight(requires, account()).Findings {
		if found.Kind == "builtin tool" && found.Requirement.Name != "end_call" {
			t.Errorf("a control produced a %q finding for %q", found.Kind, found.Requirement.Name)
		}
	}
}

// A fully provisioned account gets one line, not five empty lists, because an
// empty list reads like a bug in the compiler.
func TestPreflightSaysSoWhenEverythingIsThere(t *testing.T) {
	report := comparePreflight(generate.Requirements{
		Builtins: []generate.Requirement{need("end_call")},
		Secrets:  []generate.Requirement{need("ACME_API_KEY")},
	}, account())

	var out, errOut bytes.Buffer
	if err := renderPreflight(&out, &errOut, "slng", report); err != nil {
		t.Fatalf("a provisioned account was refused: %v", err)
	}
	if !strings.Contains(out.String(), "2 requirements satisfied") {
		t.Errorf("stdout is %q, want a one-line count", out.String())
	}
	if strings.Contains(errOut.String(), "Cannot deploy") {
		t.Errorf("a provisioned account produced a refusal: %q", errOut.String())
	}
}

// A package needing nothing is told so, rather than shown an empty report.
func TestPreflightSaysWhenAPackageNeedsNothing(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := renderPreflight(&out, &errOut, "slng", comparePreflight(generate.Requirements{}, account())); err != nil {
		t.Fatalf("a package needing nothing was refused: %v", err)
	}
	if !strings.Contains(out.String(), "needs nothing from the account") {
		t.Errorf("stdout is %q", out.String())
	}
}

// slngToolsRequirements compiles the one fixture that exercises every
// requirement kind, so the two gates that are really about the *derivation*
// assert against a real package rather than against a hand-built value that
// could be wrong in the same way the code is.
func slngToolsRequirements(t *testing.T) generate.Requirements {
	t.Helper()
	dir := filepath.Join("..", "testdata", "slng_tools")
	agent, selected, err := loadPackage(dir, nil)
	if err != nil {
		t.Fatalf("load %s: %v", dir, err)
	}
	for _, resolved := range selected {
		if resolved.Provider != ir.ProviderSlng {
			continue
		}
		artifact, err := generate.Generate(agent, resolved, target.Default())
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		return artifact.Requires
	}
	t.Fatalf("%s declares no slng target", dir)
	return generate.Requirements{}
}

// --- filling secrets --------------------------------------------------------

// fillRunner returns a runner backed by a stub that logs every argv, plus the
// log path, so a test can prove what was and was not passed on a command line.
func fillRunner(t *testing.T, script string) (*voiceaiRunner, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub voiceai is a POSIX shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "voiceai")
	log := filepath.Join(dir, "calls.log")
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + log + "\n" + script
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return newVoiceaiRunner(bin, nil, ""), log
}

func fillReport(kinds ...finding) preflightReport {
	return preflightReport{Findings: kinds}
}

// GATE. No secret value reaches argv, a file unmute writes, or either output
// stream.
//
// The CLI has no --value flag precisely because argv lands in shell history and
// is visible in `ps`. Reintroducing one here would undo that on unmute's side,
// silently, for every author who lets deploy create a secret.
func TestFillNeverPutsAValueOnACommandLineOrInOutput(t *testing.T) {
	const value = "sk-super-secret-value"
	runner, log := fillRunner(t, "cat > /dev/null\nexit 0")

	report := fillReport(finding{
		Requirement: generate.Requirement{Name: "ACME_API_KEY", Where: "tools/x.yaml authenticates with it"},
		Kind:        "secret",
		State:       absent,
	})
	var out, errOut bytes.Buffer
	offerToFill(strings.NewReader("y\n"), &out, &errOut, runner, &report,
		[]string{"ACME_API_KEY=" + value}, true)

	if report.Findings[0].State != satisfied {
		t.Fatalf("the entry was not created: %+v", report.Findings[0])
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the stub logged nothing: %v", err)
	}
	if strings.Contains(string(raw), value) {
		t.Errorf("the value reached the command line: %q", raw)
	}
	if strings.Contains(out.String()+errOut.String(), value) {
		t.Errorf("the value was printed:\nstdout: %q\nstderr: %q", out.String(), errOut.String())
	}
	// It must still say where the value came from, or the author cannot tell
	// which of several .env files was used.
	if !strings.Contains(out.String(), "set in this package's environment") {
		t.Errorf("the prompt does not name the source of the value: %q", out.String())
	}
}

// A variable is created as a variable, and an entry that already exists is
// filled rather than added: the CLI refuses a silent overwrite, so consent is
// given in advance or the create fails.
func TestFillPassesTheKindAndTheOverwrite(t *testing.T) {
	for _, tc := range []struct {
		name  string
		found finding
		want  []string
		avoid []string
	}{
		{
			name:  "a missing secret",
			found: finding{Requirement: generate.Requirement{Name: "TOKEN"}, Kind: "secret", State: absent},
			want:  []string{"secret create TOKEN"},
			avoid: []string{"--kind variable", "--overwrite"},
		},
		{
			name:  "a missing variable",
			found: finding{Requirement: generate.Requirement{Name: "BRAND"}, Kind: "variable", State: absent},
			want:  []string{"secret create BRAND", "--kind variable"},
			avoid: []string{"--overwrite"},
		},
		{
			name:  "an entry that exists with no value",
			found: finding{Requirement: generate.Requirement{Name: "HALF_MADE"}, Kind: "secret", State: empty},
			want:  []string{"secret create HALF_MADE", "--overwrite"},
			avoid: []string{"--kind variable"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner, log := fillRunner(t, "cat > /dev/null\nexit 0")
			report := fillReport(tc.found)
			var out, errOut bytes.Buffer
			offerToFill(strings.NewReader("y\n"), &out, &errOut, runner, &report, []string{tc.found.Requirement.Name + "=v"}, true)

			raw, err := os.ReadFile(log)
			if err != nil {
				t.Fatalf("nothing was run: %v", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(string(raw), want) {
					t.Errorf("argv %q does not carry %q", raw, want)
				}
			}
			for _, avoid := range tc.avoid {
				if strings.Contains(string(raw), avoid) {
					t.Errorf("argv %q carries %q, which this case must not pass", raw, avoid)
				}
			}
		})
	}
}

// A wrong-kind entry is never offered a fill. The name is taken by something
// else, so a create is refused as a duplicate and an author who followed the
// advice would have done the work and got nowhere.
func TestFillNeverOffersToFixAWrongKind(t *testing.T) {
	runner, log := fillRunner(t, "exit 0")
	report := fillReport(finding{
		Requirement: generate.Requirement{Name: "FIRECRAWL_API_KEY"},
		Kind:        "secret",
		State:       wrongKind,
	})
	var out, errOut bytes.Buffer
	offerToFill(strings.NewReader("y\n"), &out, &errOut, runner, &report, nil, true)

	if _, err := os.ReadFile(log); err == nil {
		t.Error("a wrong-kind entry was offered a create, which the account would refuse as a duplicate")
	}
	if report.Findings[0].State != wrongKind {
		t.Errorf("the finding changed state to %v without anything being done", report.Findings[0].State)
	}
}

// FR-020. A run with no terminal prompts for nothing and prints the commands
// that would fix each. A CI run that stopped to ask would hang until it was
// killed.
func TestFillDoesNotPromptWithoutATerminal(t *testing.T) {
	runner, log := fillRunner(t, "exit 0")
	report := fillReport(
		finding{Requirement: generate.Requirement{Name: "TOKEN"}, Kind: "secret", State: absent},
		finding{Requirement: generate.Requirement{Name: "BRAND"}, Kind: "variable", State: empty},
	)
	var out, errOut bytes.Buffer
	offerToFill(strings.NewReader("y\ny\n"), &out, &errOut, runner, &report, nil, false)

	if _, err := os.ReadFile(log); err == nil {
		t.Error("a non-interactive run created a secret without being asked to")
	}
	for _, want := range []string{"voiceai secret create TOKEN", "voiceai secret create BRAND --kind variable --overwrite"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("the report does not print %q:\n%s", want, errOut.String())
		}
	}
	for _, found := range report.Findings {
		if found.State == satisfied {
			t.Error("a non-interactive run marked an entry satisfied without creating it")
		}
	}
}

// Declining leaves the entry alone and the run refused. "No" has to mean no, or
// the prompt is theatre.
func TestFillRespectsARefusal(t *testing.T) {
	runner, log := fillRunner(t, "exit 0")
	report := fillReport(finding{Requirement: generate.Requirement{Name: "TOKEN"}, Kind: "secret", State: absent})
	var out, errOut bytes.Buffer
	offerToFill(strings.NewReader("n\n"), &out, &errOut, runner, &report, []string{"TOKEN=v"}, true)

	if _, err := os.ReadFile(log); err == nil {
		t.Error("answering no still created the secret")
	}
	if report.Findings[0].State != absent {
		t.Errorf("a declined entry is %v, want it left absent", report.Findings[0].State)
	}
}

// A create that fails leaves the finding where it was, so the run still refuses.
// Marking it satisfied on a failed create would push a body the account cannot
// serve.
func TestFillLeavesTheFindingAloneWhenTheCreateFails(t *testing.T) {
	runner, _ := fillRunner(t, "printf 'error: name already exists\\n' >&2\nexit 1")
	report := fillReport(finding{Requirement: generate.Requirement{Name: "TOKEN"}, Kind: "secret", State: absent})
	var out, errOut bytes.Buffer
	offerToFill(strings.NewReader("y\n"), &out, &errOut, runner, &report, []string{"TOKEN=v"}, true)

	if report.Findings[0].State != absent {
		t.Errorf("a failed create marked the entry %v", report.Findings[0].State)
	}
	if !strings.Contains(errOut.String(), "was not created") {
		t.Errorf("the failure is not reported: %q", errOut.String())
	}
}

// GATE (FR-021). Secrets are the only thing unmute writes. Nothing in a deploy
// may create, change or delete a tool, an MCP server or a trunk. It is true
// today only because nobody wrote that code, which is exactly the kind of fact
// that needs a test rather than a habit.
func TestDeployWritesNothingButSecrets(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	stub := `printf '%s\n' "$*" >> ` + log + `
case "$*" in
  *whoami*) printf '{"ok":true,"profile":"default","account":{"org_id":"o","org_name":"n"}}' ;;
  *"agents push"*"--dry-run"*) printf '{"ok":true,"dry_run":true,"resolution_contract":1,"organisation":{"id":"o","name":"n"},"agent":{"id":"a1","action":"create"}}' ;;
  *"agents push"*) printf '{"ok":true,"resolution_contract":1,"organisation":{"id":"o","name":"n"},"agent":{"id":"a1","name":"a","action":"create"},"version":"unchanged"}' ;;
` + provisionedCatalogue + `
` + provisionedContract("o") + `
` + provisionedAgent + `
  *) printf '[]' ;;
esac`
	if _, _, _, err := deployWithStub(t, stub); err != nil {
		t.Fatalf("a fully provisioned account was refused: %v", err)
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the stub logged nothing: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		for _, verb := range []string{"tool create", "tool update", "tool delete",
			"mcp create", "mcp update", "mcp delete",
			"trunks create", "trunks update", "trunks delete",
			"agents delete", "agents replace"} {
			if strings.Contains(line, verb) {
				t.Errorf("a deploy ran `voiceai %s`; the only write unmute makes is a secret create", line)
			}
		}
	}
}

// mcpFixtureRecord loads one of the spec 007 MCP fixtures.
func mcpFixtureRecord(t *testing.T, name string) slngMCPRecord {
	t.Helper()
	var record slngMCPRecord
	fixture(t, name, &record)
	return record
}

// TestMCPRecordStatesAreReadTogether is the property FR-012 turns on: no single
// field of a capability snapshot says whether a selected tool can be attached.
//
// A healthy status with a week-old observation is stale. A fresh observation on
// a failed probe found nothing. A truncated record listing two tools does not
// say a third is absent, it says the probe stopped. Each of the four fixtures
// is one of those, and the point is that they are four different answers rather
// than "healthy" and "not healthy".
func TestMCPRecordStatesAreReadTogether(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		usable  bool
		// state is a fragment the reason has to carry, so the message tells the
		// author which of the four they are looking at.
		state string
	}{
		{fixture: "mcp_get_healthy.json", usable: true, state: "usable"},
		// Stale is the case a status alone gets wrong: it still says healthy,
		// and the window the platform keeps the observation for has passed. The
		// platform refuses such a snapshot at push time, so calling it usable
		// would produce a deployment that checked a record and then could not
		// attach it.
		{fixture: "mcp_get_stale.json", usable: false, state: "stale"},
		{fixture: "mcp_get_failed.json", usable: false, state: "connection_failed"},
		{fixture: "mcp_get_truncated.json", usable: false, state: "truncated"},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			record := mcpFixtureRecord(t, tc.fixture)
			// A fixed moment, just after the healthy fixture's observation, so
			// the answers are about the fixtures and not about when the suite
			// happens to run.
			now := time.Date(2026, 9, 8, 10, 5, 0, 0, time.UTC)
			if record.usableAt(now) != tc.usable {
				t.Errorf("usable = %v, want %v (status %q, truncated %v, next refresh %q)",
					record.usableAt(now), tc.usable, record.Status, record.Capabilities.Truncated, record.NextRefreshAt)
			}
			if got := recordState(record, now); !strings.Contains(got, tc.state) {
				t.Errorf("the state reads %q, want it to say %q", got, tc.state)
			}
		})
	}

	// The window.
	//
	// The platform expires a snapshot on the age of its observation, against a
	// TTL that is a server setting and is not knowable from here.
	// `next_refresh_at` is the platform's own background-discovery schedule,
	// which falls at 80-90% of that TTL, so it always lands BEFORE the expiry
	// and being inside it means the observation is inside the window with room
	// to spare. Reading it that way is conservative in the one safe direction:
	// a refresh slightly early costs one connection.
	healthy := mcpFixtureRecord(t, "mcp_get_healthy.json")
	if !healthy.fresh(time.Date(2026, 9, 8, 10, 5, 0, 0, time.UTC)) {
		t.Error("a record inside its own schedule is not fresh")
	}
	if healthy.fresh(time.Date(2026, 9, 8, 10, 8, 0, 0, time.UTC)) {
		t.Error("a record past its own next_refresh_at is still called fresh, and a push would refuse it")
	}

	// A record declaring no schedule used to be treated as fresh, and that was
	// the defect: with no schedule and no knowable TTL, nothing on the record
	// says how old the observation may be, so a six-year-old snapshot read as
	// current and the deploy attached it. An unknown window is not a fresh one.
	undated := healthy
	undated.NextRefreshAt = ""
	if undated.fresh(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("a record declaring no refresh schedule is treated as fresh, so an observation of any age would be attached")
	}
	if got := recordState(undated, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)); !strings.Contains(got, "unknown age") {
		t.Errorf("the state does not say the age is unknown, and it rendered an empty deadline before: %q", got)
	}

	// The platform refuses a snapshot it cannot date, and its own schema
	// forbids a healthy record from carrying no observation time.
	unobserved := healthy
	unobserved.ObservedAt = ""
	if unobserved.fresh(time.Date(2026, 9, 8, 10, 5, 0, 0, time.UTC)) {
		t.Error("a record with no observation time is treated as fresh")
	}

	stopped := healthy
	stopped.NextRefreshAt = "not a timestamp"
	if stopped.fresh(time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)) {
		t.Error("an unparseable window is treated as fresh; refreshing is the safe direction")
	}

	// A failed probe carries its reason, and the reason is what an author acts
	// on: a DNS failure and a refused credential are different problems.
	failed := mcpFixtureRecord(t, "mcp_get_failed.json")
	moment := time.Date(2026, 9, 8, 10, 5, 0, 0, time.UTC)
	if !strings.Contains(recordState(failed, moment), "no such host") {
		t.Errorf("a failed probe does not carry the server's own reason: %s", recordState(failed, moment))
	}
}

// TestMCPRecordFindsATool: the lookup, and the fact its bool means "the record
// contains it" rather than "the server offers it".
func TestMCPRecordFindsATool(t *testing.T) {
	record := mcpFixtureRecord(t, "mcp_get_healthy.json")
	entry, ok := record.Tool("firecrawl_search")
	if !ok {
		t.Fatal("a tool the fixture lists was not found")
	}
	if entry.SchemaHash == "" {
		t.Error("the schema hash is empty, and it is the value an attachment carries")
	}
	if _, ok := record.Tool("firecrawl_crawl"); ok {
		t.Error("a tool the fixture does not list was found")
	}

	// The same lookup on a truncated record. It answers "not in this record",
	// and resolveMCP is what refuses to read that as "not on the server".
	truncated := mcpFixtureRecord(t, "mcp_get_truncated.json")
	if _, ok := truncated.Tool("firecrawl_search"); ok {
		t.Error("a truncated record claims to hold a tool it never listed")
	}
	if truncated.usable() {
		t.Error("a truncated record is usable, so an absent tool would be reported as missing on evidence that cannot support it")
	}
}

// TestMCPVaultNeedsComeFromTheHostedServer.
//
// SLNG holds the server: its address, its transport and its credential are the
// platform's. So the credential names come off the server record, and a code
// target's own `url_env` and `auth:` are that target's business. Reporting
// those here would ask an author to put a code target's environment into the
// SLNG vault.
func TestMCPVaultNeedsComeFromTheHostedServer(t *testing.T) {
	record := mcpFixtureRecord(t, "mcp_get_healthy.json")
	needs := mcpVaultNeeds(record, "tools/web_search.yaml")

	kinds := map[string]string{}
	for _, need := range needs {
		kinds[need.Name] = need.Kind
		if !strings.Contains(need.Where, "tools/web_search.yaml") {
			t.Errorf("%s does not trace back to the file that selected the server: %s", need.Name, need.Where)
		}
	}
	if kinds["FIRECRAWL_API_KEY"] != "secret" {
		t.Errorf("the bearer credential is %q, want a secret", kinds["FIRECRAWL_API_KEY"])
	}
	if kinds["FIRECRAWL_WORKSPACE"] != "secret" {
		t.Errorf("the header credential is %q, want a secret", kinds["FIRECRAWL_WORKSPACE"])
	}
	// A token substituted into the address is a vault VARIABLE. The two kinds
	// are created differently, so the wrong one sends an author to make an entry
	// the platform then refuses as a duplicate.
	if kinds["FIRECRAWL_MCP_PATH"] != "variable" {
		t.Errorf("the URL-template token is %q, want a variable", kinds["FIRECRAWL_MCP_PATH"])
	}
	// And no URL, no transport and no auth type leaves the function.
	for _, need := range needs {
		for _, forbidden := range []string{"https://", "streamable_http", "bearer"} {
			if strings.Contains(need.Where, forbidden) {
				t.Errorf("%s's reason leaks %q from the server's configuration: %s", need.Name, forbidden, need.Where)
			}
		}
	}
}

// TestEligibleToolsSeparatesTheOrganisationsOwnFromTheCurated.
//
// `tool_list_scoped.json` holds `end_call` at both scopes at once, which is a
// real shape: SLNG publishes the capability to everybody and an organisation
// can have its own. Without the scope filter a `slng:` reference and a
// `builtin:` reference would resolve to the same record and one of the two
// would attach the wrong thing.
func TestEligibleToolsSeparatesTheOrganisationsOwnFromTheCurated(t *testing.T) {
	var catalogue []slngAccountTool
	fixture(t, "tool_list_scoped.json", &catalogue)

	if got := eligibleTools("end_call", catalogue); len(got) != 1 || got[0].Scope != "organisation" {
		t.Errorf("end_call resolves to %+v, want the one organisation-scoped record", got)
	}
	// Two organisation-owned records of one name. Nothing local can choose, and
	// picking the first is what the name-only push did.
	if got := eligibleTools("ambiguous_tool", catalogue); len(got) != 2 {
		t.Errorf("ambiguous_tool resolves to %d records, want the 2 the account holds", len(got))
	}
	// A curated-only name is not eligible for a `slng:` reference at all.
	if got := eligibleTools("user_phone_number", catalogue); len(got) != 0 {
		t.Errorf("a curated-only name is eligible for a hosted reference: %+v", got)
	}
}

// TestPublishedVersionIsNotTheDraft.
//
// The reason the immutable getter is an upstream prerequisite at all. The
// mutable record is whatever somebody last saved in the dashboard, and
// `tool_get_draft_divergent.json` is a real shape of that: it renames the one
// parameter the example injects and names a secret no published version needs.
//
// A binding validated against it would be validated against a contract nothing
// serves, and it would pass or fail for the wrong reason.
func TestPublishedVersionIsNotTheDraft(t *testing.T) {
	var draft map[string]any
	fixture(t, "tool_get_draft_divergent.json", &draft)
	var version slngPublishedVersion
	fixture(t, "tool_version_published.json", &version)

	if version.Snapshot.ArgumentSchema == nil {
		t.Fatal("the published snapshot's parameters did not decode: `argument_schema` is the published field and `arg_schema` is the draft's, and they are different fields")
	}
	published, _ := version.Snapshot.ArgumentSchema["properties"].(map[string]any)
	if _, ok := published["query"]; !ok {
		t.Error("the published contract does not declare `query`, which is what the example injects")
	}
	// The draft renamed it. Reading the draft would refuse a binding that the
	// published version accepts.
	drafted, _ := draft["arg_schema"].(map[string]any)
	draftedProperties, _ := drafted["properties"].(map[string]any)
	if _, ok := draftedProperties["query"]; ok {
		t.Fatal("the divergent-draft fixture no longer diverges, so it proves nothing")
	}
	// And the draft's own fields say it is not current, which is the evidence
	// nothing reads today.
	if draft["is_current_version"] != false || draft["schema_stale"] != true {
		t.Error("the draft fixture does not carry the fields that say it is not the published version")
	}
}

// TestVaultEntryWithNoReportedKindIsNotAMismatch.
//
// An account's real `secret list` carries the name and has_value and no kind.
// Read as a kind of "", every populated entry became a mismatch against a
// wanted "secret", and the deploy told the author to delete or rename an entry
// that was correct and populated. An absent field is the absence of evidence,
// which is the opposite conclusion from evidence of a mismatch.
//
// The kind is still compared when the listing reports one, because two things
// sharing a name really is unfixable by creating a third.
func TestVaultEntryWithNoReportedKindIsNotAMismatch(t *testing.T) {
	requirement := generate.Requirement{Name: "SLNG_TOOL_RENDER", Where: "check_order reads it"}

	unreported := compareVault(requirement, "secret",
		[]slngVaultEntry{{Name: "SLNG_TOOL_RENDER", HasValue: true}}, true, false)
	if unreported.State != satisfied {
		t.Errorf("a populated entry whose kind the listing does not report is %v, and the listing never reports one: %s",
			unreported.State, unreported.Detail)
	}
	for _, forbidden := range []string{"delete", "rename"} {
		if strings.Contains(unreported.Detail, forbidden) {
			t.Errorf("the finding tells the author to %s a correct entry: %s", forbidden, unreported.Detail)
		}
	}

	// Still empty when it is empty: the name existing is not the value existing.
	hollow := compareVault(requirement, "secret",
		[]slngVaultEntry{{Name: "SLNG_TOOL_RENDER"}}, true, false)
	if hollow.State != empty {
		t.Errorf("an entry holding no value is %v, and a tool reading it gets nothing", hollow.State)
	}

	// And a reported kind that really differs is still a mismatch.
	mismatched := compareVault(requirement, "secret",
		[]slngVaultEntry{{Name: "SLNG_TOOL_RENDER", Kind: "variable", HasValue: true}}, true, false)
	if mismatched.State != wrongKind {
		t.Errorf("a reported kind of %q against a wanted secret is %v, so the real mismatch stopped being caught",
			"variable", mismatched.State)
	}
}

// TestEveryBlockedFindingIsPrinted.
//
// kindOrder used to be the filter as well as the order, so a finding whose kind
// was not on its hardcoded list was counted in "1 thing to fix" and printed
// nowhere: a refusal naming a number with nothing under it, and no test failed
// because every kind that existed was on the list. Adding one broke it.
//
// The kind is a label. A label nobody thought to add here is not a reason to
// hide the finding, so this asserts the property rather than the list.
func TestEveryBlockedFindingIsPrinted(t *testing.T) {
	report := preflightReport{Findings: []finding{{
		Kind:  "a kind nobody added to the order",
		State: wrongKind,
		Requirement: generate.Requirement{
			Name:  "the thing that is wrong",
			Where: "the line that asked for it",
		},
		Detail: "the sentence that says what to do about it",
	}}}

	var out, errOut bytes.Buffer
	if err := renderPreflight(&out, &errOut, "slng", report); err == nil {
		t.Fatal("a blocked finding did not refuse the run")
	}
	said := out.String() + errOut.String()
	for _, want := range []string{"the thing that is wrong", "the sentence that says what to do about it"} {
		if !strings.Contains(said, want) {
			t.Errorf("the refusal counts this finding and does not print %q:\n%s", want, said)
		}
	}
}

// TestAnUnreadableVaultBlocksADiscoveredNeedAndWarnsOnADeclaredOne.
//
// The two are not the same gap, and this used to treat them as one.
//
// The push's own vault check reads `requiredSecretNames`: the `{{$NAME}}`
// tokens in the package's prompts, plus the credential names on tool bodies
// physically present in the package. A name the package declares is therefore
// re-checked against a fresh read at push time, so an unreadable listing here
// can warn: the gap really is covered, and refusing would refuse a deployment
// that gets refused later with a worse message.
//
// A name DISCOVERED from a published contract or a hosted MCP server's
// credentials is in neither of those sources. It never reaches the push's
// check, so an unreadable listing leaves it unverified with nothing downstream
// to catch it, and that is the same case as a resolved version: it blocks.
//
// The warning also has to stop claiming more than it can. Neither run
// establishes whether an entry holds a value, because the push compares a name
// and a kind and never reads `has_value`.
func TestAnUnreadableVaultBlocksADiscoveredNeedAndWarnsOnADeclaredOne(t *testing.T) {
	requirement := generate.Requirement{Name: "FIRECRAWL_API_KEY", Where: "the hosted server reads it"}

	discovered := compareVault(requirement, "secret", nil, false, true)
	if !discovered.blocks() {
		t.Errorf("an unreadable listing leaves a discovered credential unverified and does not block: %+v", discovered)
	}
	if !strings.Contains(discovered.Detail, "the push does not check it either") {
		t.Errorf("the refusal does not say why nothing downstream covers it: %s", discovered.Detail)
	}

	declared := compareVault(requirement, "secret", nil, false, false)
	if declared.blocks() {
		t.Errorf("a name the push re-checks against a fresh read blocks here as well: %+v", declared)
	}
	if !strings.Contains(declared.Detail, "holds a value") {
		t.Errorf("the warning does not say what neither run establishes: %s", declared.Detail)
	}
}

// TestMCPHealthIsThePlatformsOwnWord.
//
// The platform's attachment validation refuses any status but "healthy". This
// accepted "ok" as well, which is not a status the platform has: a permissive
// alias can only ever pass something the push then refuses.
func TestMCPHealthIsThePlatformsOwnWord(t *testing.T) {
	for status, want := range map[string]bool{"healthy": true, "ok": false, "unknown": false, "connection_failed": false} {
		if got := (slngMCPRecord{Status: status}).healthy(); got != want {
			t.Errorf("status %q reads as healthy=%v, want %v", status, got, want)
		}
	}
}
