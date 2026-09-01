package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/target"
)

// fixture reads one captured `voiceai --json` document. The captures live in
// testdata/voiceai with a README saying which parts are real and which are
// synthesised, following the precedent in deploy_test.go: the fields that matter
// are the ones the tool actually emits, and a struct that guesses at a shape
// reads zero out of a real account and reports success.
func fixture(t *testing.T, name string, out any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "voiceai", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("fixture %s does not decode into %T: %v", name, out, err)
	}
}

// stubVoiceai writes a shell script standing in for the CLI. Each call is
// appended to a log file, so a test can assert not only what was passed but how
// many times, which is what the read-once gate needs.
//
// The stub is a shell script, which is why these skip on Windows. Tests run on
// Linux in CI and on macOS locally; the release build is the only Windows target
// and it runs no tests.
func stubVoiceai(t *testing.T, script string) (bin, log string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub voiceai is a POSIX shell script")
	}
	dir := t.TempDir()
	bin = filepath.Join(dir, "voiceai-stub")
	log = filepath.Join(dir, "calls.log")
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + log + "\n" + script
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func calls(t *testing.T, log string) []string {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// TestVoiceaiDecodesEveryCapturedShape. Each of these was captured from a live
// organisation, and each one has at least one field that a hand-written struct
// would plausibly get wrong: whoami nests the organisation under `account`, a
// vault entry's populated bit is `has_value` and not `value`, and a trunk's
// `in_use_by` is a single name rather than the list its plural reading suggests.
func TestVoiceaiDecodesEveryCapturedShape(t *testing.T) {
	var account slngAccount
	fixture(t, "whoami.json", &account)
	if !account.OK || account.Account.OrgName == "" || account.Account.OrgID == "" {
		t.Errorf("whoami decoded to %+v, losing the organisation", account)
	}
	if !strings.Contains(account.String(), "profile default") {
		t.Errorf("the account line %q does not name the profile it resolved", account)
	}

	var vault []slngVaultEntry
	fixture(t, "secret_list.json", &vault)
	var sawSecret, sawVariable, sawEmpty bool
	for _, entry := range vault {
		switch {
		case entry.Kind == "secret" && entry.HasValue:
			sawSecret = true
		case entry.Kind == "variable":
			sawVariable = true
		}
		if !entry.HasValue {
			sawEmpty = true
		}
	}
	if !sawSecret || !sawVariable || !sawEmpty {
		t.Errorf("the vault fixture does not cover secret, variable and empty: %+v", vault)
	}

	var tools []slngAccountTool
	fixture(t, "tool_list.json", &tools)
	// The fact the whole builtin check rests on: curated capabilities are ordinary
	// tools in this listing. If that stops being true, the check has to become
	// inconclusive rather than start reporting every builtin as missing.
	var sawEndCall bool
	for _, tool := range tools {
		if tool.Name == "end_call" && tool.ToolType == "end_call" {
			sawEndCall = true
		}
	}
	if !sawEndCall {
		t.Error("tool list no longer carries end_call as a curated tool, so a builtin reference cannot be checked positively")
	}

	var trunks []slngTrunk
	fixture(t, "trunks_list.json", &trunks)
	var sawAttached, sawUnusable bool
	for _, trunk := range trunks {
		if trunk.InUseBy != "" {
			sawAttached = true
		}
		if !trunk.Usable && trunk.UnavailableReason != "" {
			sawUnusable = true
		}
	}
	if !sawAttached || !sawUnusable {
		t.Errorf("the trunk fixture does not cover both an attached and an unusable trunk: %+v", trunks)
	}
}

// TestVoiceaiPutsTheProfileBeforeTheSubcommand. `--profile` is a root option. A
// run that passes it after the subcommand does not fail loudly; it is an unknown
// flag, or it resolves a different account from the one the push writes to,
// which would make every finding a statement about somewhere else.
func TestVoiceaiPutsTheProfileBeforeTheSubcommand(t *testing.T) {
	runner := newVoiceaiRunner("voiceai", nil, "work")
	got := strings.Join(runner.argv(target.SlngSecretList), " ")
	if want := "--profile work secret list --json"; got != want {
		t.Errorf("argv is %q, want %q", got, want)
	}

	bare := newVoiceaiRunner("voiceai", nil, "")
	got = strings.Join(bare.argv(target.SlngSecretList), " ")
	if want := "secret list --json"; got != want {
		t.Errorf("a run with no profile built %q, want %q", got, want)
	}
}

// TestVoiceaiReadsEachResourceKindOnce is the cost gate, and it counts a whole
// deploy rather than the preflight alone: the trunk read happens after the push
// and is organisation-wide, enumerating every agent, so leaving it outside the
// count would leave the expensive half ungoverned.
func TestVoiceaiReadsEachResourceKindOnce(t *testing.T) {
	bin, log := stubVoiceai(t, `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"p","account":{"org_id":"o","org_name":"n"}}' ;;
  *"mcp tools"*) printf '[{"name":"firecrawl_scrape"}]' ;;
  *"mcp list"*) printf '[{"name":"firecrawl-mcp","capability_status":"healthy"}]' ;;
  *) printf '[]' ;;
esac`)

	runner := newVoiceaiRunner(bin, nil, "")
	// Two tools on one server, and the same server named twice, because a package
	// may reference one server from several tools. Neither may cost a second read.
	if _, err := readResources(runner, []string{"firecrawl-mcp"}); err != nil {
		t.Fatalf("readResources: %v", err)
	}
	if _, _, err := readTrunks(runner); err != nil {
		t.Fatalf("readTrunks: %v", err)
	}

	for command, count := range runner.reads {
		if count != 1 {
			t.Errorf("`voiceai %s` ran %d times; each resource kind is read once per deploy", command, count)
		}
	}
	if got := len(calls(t, log)); got != 6 {
		t.Errorf("a full deploy made %d account reads, want 6 (whoami, secret, tool, mcp, mcp tools, trunks)", got)
	}
}

// TestVoiceaiTreatsAFailedReadAsUnchecked. The safety argument for this whole
// feature is that "the account does not have this" and "I could not find out"
// are different answers. A stub that exits non-zero, one that prints nothing,
// and one that prints prose where a document belongs are all the second.
func TestVoiceaiTreatsAFailedReadAsUnchecked(t *testing.T) {
	for _, tc := range []struct {
		name, script, want string
	}{
		{"a non-zero exit", `printf 'error: insufficient scope\n' >&2; exit 1`, "insufficient scope"},
		{"no output at all", `exit 3`, "exit status 3"},
		{"prose where a document belongs", `printf 'Killed: 9\n' >&2; exit 137`, "Killed: 9"},
		{"an unknown subcommand", `printf "error: unknown command 'mcp'\n" >&2; exit 1`, "unknown command"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, _ := stubVoiceai(t, tc.script)
			runner := newVoiceaiRunner(bin, nil, "")
			var out []slngVaultEntry
			err := runner.read(target.SlngSecretList, &out)

			var missed *unchecked
			if !asUnchecked(err, &missed) {
				t.Fatalf("a failed read returned %T (%v); it must be *unchecked so it cannot be read as an absence", err, err)
			}
			if !strings.Contains(missed.Reason, tc.want) {
				t.Errorf("the reason %q hides what the tool said (%q)", missed.Reason, tc.want)
			}
			if !strings.Contains(missed.Error(), "voiceai secret list") {
				t.Errorf("the message %q does not name the command that failed", missed.Error())
			}
		})
	}
}

// TestVoiceaiKeepsReadingAfterOneKindFails. A preflight with three answers and
// one gap is worth reading. Aborting on the first failure would hide the two
// findings the author could still act on.
func TestVoiceaiKeepsReadingAfterOneKindFails(t *testing.T) {
	bin, _ := stubVoiceai(t, `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"p","account":{"org_id":"o","org_name":"n"}}' ;;
  *"tool list"*) printf 'error: forbidden\n' >&2; exit 1 ;;
  *) printf '[]' ;;
esac`)

	runner := newVoiceaiRunner(bin, nil, "")
	resources, err := readResources(runner, nil)
	if err != nil {
		t.Fatalf("one failed listing aborted the whole read: %v", err)
	}
	if len(resources.Unchecked) != 1 {
		t.Fatalf("recorded %d unchecked reads, want 1: %v", len(resources.Unchecked), resources.Unchecked)
	}
	if !strings.Contains(resources.Unchecked[0].Command, "tool list") {
		t.Errorf("the unchecked read is %q, want the tool listing", resources.Unchecked[0].Command)
	}
	if runner.reads["secret list"] != 1 || runner.reads["mcp list"] != 1 {
		t.Errorf("the other listings were skipped: %v", runner.reads)
	}
}

// TestVoiceaiStopsWhenItCannotNameTheAccount. Every finding is a statement about
// one organisation. A run that cannot say which one has nothing to report, so
// this is the single fatal read.
func TestVoiceaiStopsWhenItCannotNameTheAccount(t *testing.T) {
	bin, _ := stubVoiceai(t, `printf 'error: invalid api key\n' >&2; exit 1`)
	runner := newVoiceaiRunner(bin, nil, "")
	if _, err := readResources(runner, nil); err == nil {
		t.Fatal("a run that could not identify the account carried on checking it")
	} else if !strings.Contains(err.Error(), "which SLNG organisation") {
		t.Errorf("the error %q does not say what could not be determined", err)
	}
}

// TestVoiceaiSkipsToolsForAServerItDoesNotHave. Asking a server that is already
// a finding reports a second problem caused by the first, and costs a read to
// do it.
func TestVoiceaiSkipsToolsForAServerItDoesNotHave(t *testing.T) {
	bin, _ := stubVoiceai(t, `case "$*" in
  *whoami*) printf '{"ok":true,"profile":"p","account":{"org_id":"o","org_name":"n"}}' ;;
  *) printf '[]' ;;
esac`)

	runner := newVoiceaiRunner(bin, nil, "")
	resources, err := readResources(runner, []string{"absent-mcp"})
	if err != nil {
		t.Fatalf("readResources: %v", err)
	}
	if runner.reads["mcp tools"] != 0 {
		t.Error("a server the account does not have was interrogated for its tools")
	}
	if len(resources.Unchecked) != 0 {
		t.Errorf("skipping an absent server recorded an unchecked read: %v", resources.Unchecked)
	}
}

// TestVoiceaiKeepsAdvisoriesOutOfTheDocument. The trunk commands print a caveat
// on the error stream saying what the platform withholds. It is worth relaying
// and it is not data: parsing it as part of the JSON is how a capture becomes
// invalid.
func TestVoiceaiKeepsAdvisoriesOutOfTheDocument(t *testing.T) {
	bin, _ := stubVoiceai(t, `printf 'note: a trunk attached to no agent is not visible here.\n' >&2
printf '[{"direction":"inbound","name":"1_inbound","numbers":["+441234567890"],"usable":true}]'`)

	runner := newVoiceaiRunner(bin, nil, "")
	trunks, notes, err := readTrunks(runner)
	if err != nil {
		t.Fatalf("an advisory on stderr made the read fail: %v", err)
	}
	if len(trunks) != 1 || trunks[0].Name != "1_inbound" {
		t.Errorf("the document decoded to %+v", trunks)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "not visible here") {
		t.Errorf("the advisory was dropped: %v", notes)
	}
}

// asUnchecked is errors.As under a name that reads as prose at the call site.
// It is errors.As and not a type assertion because the rest of this tree matches
// errors that way and errorlint holds it there; a read wrapped on its way up
// would otherwise stop being recognised and start reading as an absence, which
// is the exact confusion the type exists to prevent.
func asUnchecked(err error, out **unchecked) bool {
	return errors.As(err, out)
}
