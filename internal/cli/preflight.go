package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/target"
)

// Comparing what a package needs against what the account has.
//
// Nothing in this file runs a subprocess or opens a socket. voiceai.go asks the
// questions; this decides what the answers mean. The split is the reason every
// rule below can be tested table-driven, with no stub and no network, which is
// where the rules that matter actually get held.

// findingState is what one requirement turned out to be.
type findingState int

const (
	// satisfied: present, populated, and the right kind.
	satisfied findingState = iota
	// absent: the account positively does not have it. This stops a deploy,
	// because the push would refuse anyway and refusing here costs no compile.
	absent
	// empty: the entry exists and holds no value. A different sentence from
	// absent, and a different fix: the name is taken, so creating it again fails.
	empty
	// wrongKind: present under the other vault kind. Also not "missing", and
	// pointedly not offered a fill, because creating it again does not help.
	wrongKind
	// unhealthy: an MCP server the account has, whose last stored probe failed.
	// A warning and not a refusal: the probe is not a live call, so it is no more
	// trustworthy as bad news than as good.
	unhealthy
	// stale: the organisation holds this tool, and at a version the committed
	// mirror was not taken from.
	//
	// A warning and not a refusal, and blocks() below is what makes that true by
	// construction rather than by remembering. The agent calls the platform's
	// copy either way, so the risk is a package whose mirror no longer describes
	// what runs, not a deploy that will fail.
	stale
	// notChecked: the read could not be made. Never counted as satisfied, and
	// never a refusal. This is the state that keeps an old `voiceai` or a
	// restricted network from becoming a deploy outage.
	//
	// Spelled differently from the *unchecked error type in voiceai.go on
	// purpose, because they are different things: that is one read that failed,
	// this is one requirement left unanswered by it. One failed read produces
	// many of these.
	notChecked
)

// blocks reports whether this state stops the run before the push.
func (s findingState) blocks() bool { return s == absent || s == empty || s == wrongKind }

// finding is one requirement, compared.
type finding struct {
	Requirement generate.Requirement
	// Kind groups the report and names the thing in a sentence: "secret",
	// "mcp server", "builtin tool".
	Kind  string
	State findingState
	// Detail is the account's own words where it had any, and unmute's
	// otherwise. It is the line that says what to do.
	Detail string
	// NearMiss is an account name that matches except for case. Names are matched
	// exactly and are case-sensitive everywhere, and case is the mistake the two
	// naming conventions in play actually produce: uppercase vault names beside
	// lower snake tool names.
	NearMiss string
}

// preflightReport is what one deploy learned before writing anything.
type preflightReport struct {
	Account   slngAccount
	Findings  []finding
	Unchecked []*unchecked
	Notes     []string
}

// blocked is every finding that stops the run.
func (r preflightReport) blocked() []finding {
	var out []finding
	for _, f := range r.Findings {
		if f.State.blocks() {
			out = append(out, f)
		}
	}
	return out
}

func (r preflightReport) satisfiedCount() int {
	count := 0
	for _, f := range r.Findings {
		if f.State == satisfied {
			count++
		}
	}
	return count
}

// comparePreflight compares every requirement against the account.
//
// Every requirement produces exactly one finding, including the satisfied ones,
// so the report can say "3 of 5" rather than only listing problems. And every
// requirement is compared: the run does not stop at the first gap, because the
// whole point of asking the account before pushing is to learn everything wrong
// in one go rather than across four refused pushes.
func comparePreflight(requires generate.Requirements, resources slngResources) preflightReport {
	report := preflightReport{
		Account:   resources.Account,
		Unchecked: resources.Unchecked,
		Notes:     resources.Notes,
	}

	toolsChecked := checked(resources.Unchecked, target.SlngToolList)
	vaultChecked := checked(resources.Unchecked, target.SlngSecretList)
	mcpChecked := checked(resources.Unchecked, target.SlngMCPList)

	accountTools := make([]string, 0, len(resources.Tools))
	for _, tool := range resources.Tools {
		accountTools = append(accountTools, tool.Name)
	}
	for _, requirement := range requires.Builtins {
		report.Findings = append(report.Findings, compareBuiltin(requirement, accountTools, toolsChecked))
	}
	for _, requirement := range requires.Hosted {
		report.Findings = append(report.Findings, compareHosted(requirement, resources.Tools, toolsChecked))
	}

	servers := make([]string, 0, len(resources.MCPServer))
	for _, server := range resources.MCPServer {
		servers = append(servers, server.Name)
	}
	for _, requirement := range requires.MCPServers {
		report.Findings = append(report.Findings, compareMCPServer(requirement, resources.MCPServer, servers, mcpChecked))
	}
	for _, requirement := range requires.MCPTools {
		report.Findings = append(report.Findings, compareMCPTool(requirement, resources, mcpChecked))
	}

	for _, requirement := range requires.Secrets {
		report.Findings = append(report.Findings, compareVault(requirement, "secret", resources.Vault, vaultChecked))
	}
	for _, requirement := range requires.Variables {
		report.Findings = append(report.Findings, compareVault(requirement, "variable", resources.Vault, vaultChecked))
	}
	return report
}

func compareBuiltin(requirement generate.Requirement, accountTools []string, wasChecked bool) finding {
	found := finding{Requirement: requirement, Kind: "builtin tool"}
	if !wasChecked {
		found.State, found.Detail = notChecked, "the account's tools could not be listed, so the push decides this one"
		return found
	}
	for _, name := range accountTools {
		if name == requirement.Name {
			found.State = satisfied
			return found
		}
	}
	found.State = absent
	found.NearMiss = nearMiss(requirement.Name, accountTools)
	switch {
	case found.NearMiss != "":
		// The most common way this goes wrong: the reference is the tool *file's*
		// name, so a package that named the file for what it does rather than for
		// the capability it selects emits a name the account has never heard of.
		found.Detail = fmt.Sprintf("this organisation has %q, and a builtin reference is the tool file's own name: rename tools/%s.yaml to tools/%s.yaml",
			found.NearMiss, requirement.Name, found.NearMiss)
	default:
		found.Detail = fmt.Sprintf("this organisation has no tool of this name (it has %s). A builtin reference is the tool file's own name, so either rename the file to a capability SLNG offers, or create the tool in the SLNG dashboard",
			joinNames(accountTools))
	}
	return found
}

// compareHosted checks a `slng:` reference twice over: the organisation has to
// hold the name at all, and it has to still hold what the mirror was taken
// from. The two answers are different states with different severities.
//
// The absent half reuses the builtin near-miss remedy, because the mistake is
// the same one: a hosted reference is the tool *file's* own name, so a package
// that named the file for what it does rather than for the tool it references
// emits a name the account has never heard of.
func compareHosted(requirement generate.Requirement, accountTools []slngAccountTool, wasChecked bool) finding {
	found := finding{Requirement: requirement, Kind: "hosted tool"}
	if !wasChecked {
		found.State, found.Detail = notChecked, "the account's tools could not be listed, so hosted tool versions were not checked; the push decides what it would have covered"
		return found
	}
	names := make([]string, 0, len(accountTools))
	for _, tool := range accountTools {
		names = append(names, tool.Name)
	}
	for _, tool := range accountTools {
		if tool.Name != requirement.Name {
			continue
		}
		// The version comparison costs nothing extra: this listing carries
		// latest_version, so one read answers both "does the name exist" and
		// "has it moved". A read per hosted tool on every deploy was the
		// alternative.
		//
		// The second clause is the important half of the message. The deploy is
		// going ahead either way, because the agent always calls the platform's
		// copy, so the risk is not a broken deploy but a package whose mirror no
		// longer describes what runs.
		if requirement.Version > 0 && tool.LatestVersion > 0 && tool.LatestVersion != requirement.Version {
			found.State = stale
			found.Detail = fmt.Sprintf("this tool is at version %d in this organisation and the committed mirror was taken from version %d: "+
				"run `unmute pull` to update it, or deploy knowing the agent will call the organisation's version and not the one in this package",
				tool.LatestVersion, requirement.Version)
			return found
		}
		found.State = satisfied
		return found
	}
	found.State = absent
	found.NearMiss = nearMiss(requirement.Name, names)
	switch {
	case found.NearMiss != "":
		found.Detail = fmt.Sprintf("this organisation has %q, and a hosted reference is the tool file's own name: rename tools/%s.yaml to tools/%s.yaml",
			found.NearMiss, requirement.Name, found.NearMiss)
	default:
		found.Detail = fmt.Sprintf("this organisation has no tool of this name (it has %s). A hosted reference is the tool file's own name, so either rename the file to a tool the organisation has, or create the tool in the SLNG dashboard",
			joinNames(names))
	}
	return found
}

func compareMCPServer(requirement generate.Requirement, servers []slngMCPServer, names []string, wasChecked bool) finding {
	found := finding{Requirement: requirement, Kind: "mcp server"}
	if !wasChecked {
		found.State, found.Detail = notChecked, "the account's MCP servers could not be listed, so the push decides this one"
		return found
	}
	for _, server := range servers {
		if server.Name != requirement.Name {
			continue
		}
		if status := strings.ToLower(server.CapabilityStatus); status != "" && status != "healthy" && status != "ok" {
			found.State = unhealthy
			found.Detail = fmt.Sprintf("the account has this server and its last stored probe says %q%s. That probe is not a live call, so this is a warning rather than a refusal",
				server.CapabilityStatus, parenthesised(server.CapabilityError))
			return found
		}
		found.State = satisfied
		return found
	}
	found.State = absent
	found.NearMiss = nearMiss(requirement.Name, names)
	switch {
	case found.NearMiss != "":
		found.Detail = fmt.Sprintf("the account has %q, and names are matched exactly: correct the spelling in the package", found.NearMiss)
	case len(names) == 0:
		found.Detail = "this organisation has no MCP servers attached at all. An MCP server is attached in the SLNG dashboard; unmute cannot create one"
	default:
		found.Detail = fmt.Sprintf("this organisation has %s. An MCP server is attached in the SLNG dashboard; unmute cannot create one", joinNames(names))
	}
	return found
}

func compareMCPTool(requirement generate.Requirement, resources slngResources, wasChecked bool) finding {
	found := finding{Requirement: requirement, Kind: "mcp tool"}
	if !wasChecked {
		found.State, found.Detail = notChecked, "the account's MCP servers could not be listed, so the push decides this one"
		return found
	}
	offered, known := resources.MCPTools[requirement.Server]
	if !known {
		// Either the server is absent, which is already its own finding, or its
		// tool list could not be read. Reporting a second problem caused by the
		// first would double the noise and halve the signal.
		found.State = notChecked
		found.Detail = fmt.Sprintf("server %q was not readable, so its tools were not checked", requirement.Server)
		return found
	}
	names := make([]string, 0, len(offered))
	for _, tool := range offered {
		names = append(names, tool.Name)
	}
	for _, name := range names {
		if name == requirement.Name {
			found.State = satisfied
			return found
		}
	}
	found.State = absent
	found.NearMiss = nearMiss(requirement.Name, names)
	found.Detail = fmt.Sprintf("server %q does not offer this tool. It offers %s. These names come from the server's last stored probe, not a live call",
		requirement.Server, joinNames(names))
	if found.NearMiss != "" {
		found.Detail = fmt.Sprintf("server %q offers %q, and names are matched exactly: correct the spelling in the package",
			requirement.Server, found.NearMiss)
	}
	return found
}

func compareVault(requirement generate.Requirement, want string, vault []slngVaultEntry, wasChecked bool) finding {
	found := finding{Requirement: requirement, Kind: want}
	if !wasChecked {
		found.State, found.Detail = notChecked, "the vault could not be listed, so the push decides this one"
		return found
	}
	names := make([]string, 0, len(vault))
	for _, entry := range vault {
		names = append(names, entry.Name)
	}
	for _, entry := range vault {
		if entry.Name != requirement.Name {
			continue
		}
		switch {
		case entry.Kind != want:
			// Not "missing". Creating it again would be refused as a duplicate, and
			// the author would have followed the advice and got nowhere.
			found.State = wrongKind
			found.Detail = fmt.Sprintf("the vault holds this name as a %s and the package needs a %s. Two different things share the name, so delete or rename one of them in the SLNG dashboard",
				entry.Kind, want)
		case !entry.HasValue:
			found.State = empty
			found.Detail = fmt.Sprintf("the entry exists and holds no value. Supply one with `%s --overwrite`, because the name is taken and a plain create is refused",
				target.SlngSecretCreate.With(requirement.Name))
		default:
			found.State = satisfied
		}
		return found
	}
	found.State = absent
	found.NearMiss = nearMiss(requirement.Name, names)
	// The whole command inside one pair of backticks. Closing before the flag
	// gives an author something they can copy and that then creates the wrong
	// kind of entry.
	create := target.SlngSecretCreate.With(requirement.Name)
	if want == "variable" {
		create = create.With("--kind", "variable")
	}
	found.Detail = fmt.Sprintf("the vault holds no entry with this name. Create it: `%s`", create)
	if found.NearMiss != "" {
		found.Detail = fmt.Sprintf("the vault holds %q, and names are matched exactly and are case-sensitive: either correct the package or create %q",
			found.NearMiss, requirement.Name)
	}
	return found
}

// nearMiss finds an account name that differs only by case.
//
// Case folding and nothing more. Every listing matches names exactly and
// case-sensitively, and the two conventions in play are uppercase vault names
// and lower snake tool names, so case is the mistake those conventions actually
// produce. An edit-distance search would be more code for a typo class nobody
// has reported.
func nearMiss(want string, have []string) string {
	for _, candidate := range have {
		if candidate != want && strings.EqualFold(candidate, want) {
			return candidate
		}
	}
	return ""
}

// checked reports whether a given read succeeded, so a finding derived from a
// listing that was never read is reported as unchecked rather than as an
// absence. Getting this backwards is the one bug in this file that would make
// the feature worse than not having it.
func checked(missed []*unchecked, command target.SlngCommand) bool {
	for _, entry := range missed {
		if entry.Command == command.String() {
			return false
		}
	}
	return true
}

// joinNames renders account names for a refusal: sorted, and each named once.
//
// The dedupe is load-bearing rather than tidy. A name is unique per *scope*, not
// per organisation, so an account that has SLNG's curated `end_call` and a
// second one somebody made in the dashboard lists the name twice. Read back
// verbatim that says "this organisation has `end_call`, `end_call`", which reads
// like a broken tool to the one reader who is already stuck. Verified against a
// live organisation on 2026-09-03, where both `end_call` and `transfer_call`
// existed at global and organisation scope.
func joinNames(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	return "`" + strings.Join(slices.Compact(sorted), "`, `") + "`"
}

// renderPreflight writes the report.
//
// Facts to stdout in the `name: fact` form the rest of the CLI uses; anything
// the author has to act on to stderr, so the exit code matches what was printed.
// It returns an error when the run must stop, which the caller wraps.
func renderPreflight(out, errOut io.Writer, name string, report preflightReport) error {
	// Advisories the account printed alongside its data. Relayed as prose,
	// because that is what they are.
	for _, note := range report.Notes {
		notef(errOut, "%s: %s\n", name, note)
	}
	// A read that could not be made is a warning and never a refusal, and it says
	// which question went unanswered. Silence here would be the report claiming a
	// check it never made.
	for _, missed := range report.Unchecked {
		warnf(errOut, "%s: %s; the push decides what it would have covered\n", name, missed)
	}

	if len(report.Findings) == 0 {
		fmt.Fprintf(out, "%s: this package needs nothing from the account\n", name)
		return nil
	}

	blocked := report.blocked()
	if len(blocked) == 0 {
		fmt.Fprintf(out, "%s: %s satisfied\n", name, plural(report.satisfiedCount(), "requirement"))
		// stale joins unhealthy here rather than getting its own loop: both are
		// facts about something the account has, neither stops the run, and a
		// second loop would print them out of the order they were derived in.
		for _, found := range report.Findings {
			if found.State == unhealthy || found.State == stale {
				warnf(errOut, "%s: %s %s: %s\n", name, found.Kind, found.Requirement.Name, found.Detail)
			}
		}
		return nil
	}

	fmt.Fprintf(errOut, "\nCannot deploy %s. %s the account does not have:\n", name, plural(len(blocked), "thing"))
	// Grouped by kind, and within a kind in the order the requirements were
	// derived, which is sorted. Two runs of the same package read identically.
	for _, kind := range kindOrder(blocked) {
		group := findingsOfKind(blocked, kind)
		fmt.Fprintf(errOut, "\n  %s (%d)\n", kind, len(group))
		for _, found := range group {
			fmt.Fprintf(errOut, "    %s\n", found.Requirement.Name)
			fmt.Fprintf(errOut, "      %s\n", found.Requirement.Where)
			fmt.Fprintf(errOut, "      %s\n", found.Detail)
		}
	}
	fmt.Fprintln(errOut, "\n  nothing was compiled, created or changed.")
	return fmt.Errorf("slng target %q: the account is missing %s", name, plural(len(blocked), "thing"))
}

// kindOrder is the kinds present, in a fixed order rather than a map's. The
// order runs from what an author fixes in the dashboard to what they can fix
// from here, so the list ends with the ones `deploy` can offer to close.
func kindOrder(findings []finding) []string {
	order := []string{"builtin tool", "hosted tool", "mcp server", "mcp tool", "secret", "variable"}
	var present []string
	for _, kind := range order {
		if len(findingsOfKind(findings, kind)) > 0 {
			present = append(present, kind)
		}
	}
	return present
}

func findingsOfKind(findings []finding, kind string) []finding {
	var out []finding
	for _, found := range findings {
		if found.Kind == kind {
			out = append(out, found)
		}
	}
	return out
}

// --- filling what unmute is allowed to fill ---------------------------------
//
// Secrets are the only resource kind the CLI can write. A missing tool, MCP
// server or trunk is created in the SLNG dashboard, so for those the report ends
// at saying so. For a vault entry it can end at the entry existing.

// fillable reports whether this finding is one `deploy` can close.
//
// wrongKind is deliberately excluded. The name is already taken by something
// else, so a create would be refused as a duplicate and an author who followed
// the advice would have done the work and got nowhere. That one is resolved by
// deleting or renaming in the dashboard.
func (f finding) fillable() bool {
	return (f.Kind == "secret" || f.Kind == "variable") && (f.State == absent || f.State == empty)
}

// offerToFill walks the vault findings a run can close, and closes the ones the
// author agrees to.
//
// A successful create marks the finding satisfied rather than re-reading the
// vault. The create either succeeded or returned an error, so a second listing
// would only confirm what the exit status already said, and it would break the
// one-read-per-kind budget to do it.
func offerToFill(in io.Reader, out, errOut io.Writer, runner *voiceaiRunner, report *preflightReport, env []string, interactive bool) {
	var pending []int
	for index, found := range report.Findings {
		if found.fillable() {
			pending = append(pending, index)
		}
	}
	if len(pending) == 0 {
		return
	}

	if !interactive {
		// No terminal, so no prompt: a run in CI that stopped to ask would hang
		// until it was killed. The commands that would fix each are printed
		// instead, which is the whole of what the prompt would have done.
		fmt.Fprintf(errOut, "\n  %s can be created from here, and this run has no terminal to ask on:\n",
			plural(len(pending), "vault entry"))
		for _, index := range pending {
			fmt.Fprintf(errOut, "    %s\n", fillCommand(report.Findings[index]))
		}
		return
	}

	reader := bufio.NewReader(in)
	fmt.Fprintf(out, "\n%s missing from the vault, and unmute can create them.\n",
		plural(len(pending), "entry"))
	for _, index := range pending {
		found := report.Findings[index]
		if !fillOne(reader, out, errOut, runner, found, env) {
			continue
		}
		report.Findings[index].State = satisfied
		report.Findings[index].Detail = ""
	}
}

// fillOne creates one entry, and reports whether it now exists.
func fillOne(reader *bufio.Reader, out, errOut io.Writer, runner *voiceaiRunner, found finding, env []string) bool {
	name := found.Requirement.Name
	fmt.Fprintf(out, "\n  %s (%s)\n    %s\n", name, found.Kind, found.Requirement.Where)
	if found.State == empty {
		fmt.Fprintf(out, "    this name already exists with no value, so filling it replaces the entry rather than adding one\n")
	}

	// A value already sitting in the package's environment is the common case,
	// and retyping a key that is two feet away is how a typo gets into a vault.
	// The value is named by its source and never printed: `deploy` already reads
	// these files, so this adds a destination for a value it had, not a source.
	fromEnv := strings.TrimSpace(envValue(env, name))
	prompt := "    create it? [y/N] "
	if fromEnv != "" {
		prompt = fmt.Sprintf("    a value for %s is set in this package's environment. Use it? [y/N] ", name)
	}
	fmt.Fprint(out, prompt)

	answer, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(answer) == "" {
		// Nothing to read. Redirecting from /dev/null gets here rather than through
		// the non-interactive path above, because /dev/null is a character device
		// and so passes for a terminal; there is no way to tell the two apart
		// without a platform syscall. So the answer is the same either way: print
		// the command that would fix it and move on, never block.
		fmt.Fprintf(out, "\n    no answer to read, so %s was left alone. Create it with:\n      %s\n",
			name, fillCommand(found))
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(answer), "y") {
		fmt.Fprintf(out, "    left alone.\n")
		return false
	}

	// With a value in hand it goes on stdin, because the CLI has no --value flag
	// and argv would put it in shell history. With none, the terminal is handed
	// to `voiceai secret create`, which masks its own prompt: the value never
	// enters this process at all.
	if err := runner.createSecret(name, found.Kind, fromEnv, found.State == empty, os.Stdin, out, errOut); err != nil {
		fmt.Fprintf(errOut, "    %s was not created: %v\n", name, err)
		return false
	}
	fmt.Fprintf(out, "    %s created.\n", name)
	return true
}

// fillCommand is what a non-interactive run prints instead of asking.
func fillCommand(found finding) string {
	command := target.SlngSecretCreate.With(found.Requirement.Name)
	if found.Kind == "variable" {
		command = command.With("--kind", "variable")
	}
	if found.State == empty {
		command = command.With("--overwrite")
	}
	return command.String()
}

// interactiveTerminal reports whether there is a person to ask.
//
// A character device on standard input means a terminal; a pipe or a file means
// a script. Checked with os.Stat rather than a terminal library, because that is
// the whole of the question and the alternative is a dependency for one bit.
func interactiveTerminal(in io.Reader) bool {
	file, ok := in.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
