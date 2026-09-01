package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// The comparison is pure, so these are table-driven with no subprocess and no
// network. That is deliberate: the rules that make this feature safe rather than
// merely useful are all decisions, not I/O, and a rule held behind a stub is a
// rule nobody rewrites a test for.

// account is a stock set of resources standing in for a provisioned
// organisation. Modelled on the captured fixtures: six curated tools, a vault
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

// GATE. A code or webhook tool is never reported as missing.
//
// The push creates those, so their absence is the expected state of a first
// deploy. Reporting them would put two false problems in front of every new
// author, and a report that cries wolf on a first run is a report nobody reads
// on the run that matters.
//
// This is held at the derivation, so the gate compiles a real package rather
// than asserting over a hand-built value that could be wrong in the same way.
func TestPreflightNeverReportsAToolThePushCreates(t *testing.T) {
	requires := slngToolsRequirements(t)
	report := comparePreflight(requires, account())
	for _, found := range report.Findings {
		for _, created := range []string{"check_order", "refund"} {
			if found.Requirement.Name == created {
				t.Errorf("%q is written by the push and was reported as %v", created, found.State)
			}
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
	if !strings.Contains(errOut.String(), "the push decides") {
		t.Errorf("the warning does not say what covers the gap: %q", errOut.String())
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
  *"agents push"*) printf '{"ok":true,"organisation":{"id":"o","name":"n"},"agent":{"id":"a1","name":"a","action":"create"},"version":"unchanged"}' ;;
  *"tool list"*) printf '[{"name":"end_call","tool_type":"end_call"}]' ;;
  *"secret list"*) printf '[{"name":"REFUND_API_TOKEN","kind":"secret","has_value":true},{"name":"ACME_BRAND","kind":"variable","has_value":true}]' ;;
  *"mcp list"*) printf '[{"name":"internal_docs","capability_status":"healthy"}]' ;;
  *"mcp tools"*) printf '[{"name":"search_docs"},{"name":"read_doc"}]' ;;
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
