package cli

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/target"
)

// Turning a hosted name into a checked published version.
//
// The other half of preflight.go, and the same rule holds: it decides, and it
// runs no subprocess. Every read it needs arrives as an argument.
//
// The reason this exists at all is that a name is not an answer. `unmute deploy`
// promises to attach the version it validated, and to keep that promise it has
// to name three things a name does not: which record of that name, which
// published version of that record, and what that version's contract says. The
// listing answers the first, and only if the answer is unique.

// resolvedTool is one hosted reference, resolved and checked.
type resolvedTool struct {
	// Requirement is the reference as the compiler emitted it: Name is the
	// hosted name the account was asked for and Where is the package file that
	// asked, which are two different strings whenever a `slng:` scalar aliases.
	Requirement generate.Requirement
	ToolID      string
	Version     int
	ContentHash string
	Snapshot    slngPublishedSnapshot
	// finding says whether the rest of this value is usable. Unexported,
	// because a caller reads it through the resolution's Findings list rather
	// than per reference: the report groups by kind, and one list is what keeps
	// "3 of 5 satisfied" countable.
	finding finding
}

// usable reports whether this reference was resolved to a version the deploy may
// attach.
func (t resolvedTool) usable() bool {
	return t.finding.State == satisfied && t.ToolID != "" && t.Version > 0
}

// resolvedMCP is one selected server tool, with the exact snapshot it was
// checked against.
type resolvedMCP struct {
	Requirement generate.Requirement
	ServerID    string
	SchemaHash  string
}

// resolution is what a deployment checked. Findings carries one entry per
// requirement, the same contract comparePreflight keeps, so a blocked
// resolution renders through the existing report rather than a second one.
type resolution struct {
	Tools []resolvedTool
	// Builtins are the curated references, resolved to an id so the staged body
	// carries one for every reference. They are kept apart from Tools because
	// only Tools have a published contract to check a binding or a vault
	// requirement against.
	Builtins []resolvedTool
	MCP      []resolvedMCP
	// Servers is each server's record as it was checked, keyed by name, so the
	// preview can name a refresh that happened and the staging can carry the
	// hash that was checked.
	Servers map[string]slngMCPRecord
}

// eligibleTools are the records of one name this account owns and could attach.
//
// Scope, and only organisation scope. A curated capability appears in the same
// listing under an ordinary name, and `tool_list.json` holds `end_call` at both
// scopes at once: without this filter, `slng: end_call` and `builtin: end_call`
// would resolve to the same record and one of the two would be attaching the
// wrong thing. `builtin:` is how a curated capability is reached, and it keeps
// its own name-based check.
func eligibleTools(name string, catalogue []slngAccountTool) []slngAccountTool {
	var found []slngAccountTool
	for _, tool := range catalogue {
		if tool.Name == name && tool.Scope == "organisation" {
			found = append(found, tool)
		}
	}
	return found
}

// resolveHosted turns each hosted reference into a checked published version.
//
// It stops per reference rather than per run: a package with one unpublished
// tool and four good ones reports all five, because the whole reason for asking
// the account before writing is to learn everything wrong in one go.
func resolveHosted(
	runner *voiceaiRunner, cache *resolveCache,
	requires generate.Requirements, resources slngResources,
) []resolvedTool {
	toolsChecked := checked(resources.Unchecked, target.SlngToolList)
	catalogue := make([]string, 0, len(resources.Tools))
	for _, tool := range resources.Tools {
		catalogue = append(catalogue, tool.Name)
	}

	resolved := make([]resolvedTool, 0, len(requires.Hosted))
	for _, requirement := range requires.Hosted {
		resolved = append(resolved, resolveOneHosted(runner, cache, requirement, resources, catalogue, toolsChecked))
	}
	return resolved
}

// resolveOneHosted is the whole decision for one reference. It always returns a
// resolvedTool: the Finding on it says whether the rest of it is usable.
func resolveOneHosted(
	runner *voiceaiRunner, cache *resolveCache,
	requirement generate.Requirement, resources slngResources,
	catalogue []string, toolsChecked bool,
) resolvedTool {
	out := resolvedTool{Requirement: requirement}
	// Required: in the guarded mode the push attaches exactly the version this
	// run resolved and checks nothing for itself, so a read that did not happen
	// means an unverified attachment and nothing downstream would catch it.
	// This is the rule that changed with spec 007. It used to be a warning,
	// which meant an unreadable tool listing produced a deployment that
	// reported success and had verified nothing.
	found := finding{Requirement: requirement, Kind: "hosted tool", Required: true}

	if !toolsChecked {
		found.State = notChecked
		found.Detail = "the account's tools could not be listed, so this reference was not resolved and no version was checked"
		out.finding = found
		return out
	}

	candidates := eligibleTools(requirement.Name, resources.Tools)
	switch {
	case len(candidates) == 0:
		found.State = absent
		found.NearMiss = nearMiss(requirement.Name, catalogue)
		found.Detail = absentHostedDetail(requirement, catalogue, found.NearMiss)
		out.finding = found
		return out
	case len(candidates) > 1:
		// Two organisation-owned records of one name. Nothing local can choose
		// between them and picking the first is what the old name-only push did,
		// which is how a deployment attached a tool nobody meant.
		ids := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			ids = append(ids, candidate.ID)
		}
		sort.Strings(ids)
		found.State = absent
		found.Detail = fmt.Sprintf(
			"this organisation has %d tools with this name (%s), and nothing in the package says which: rename one of them in the SLNG dashboard, or point this reference at a name only one tool has",
			len(candidates), strings.Join(ids, ", "))
		out.finding = found
		return out
	}

	chosen := candidates[0]
	if chosen.LatestVersion < 1 {
		// Created and never published. It is not absent, and saying so would
		// send the author looking for a tool that is sitting in their dashboard.
		found.State = absent
		found.Detail = "this organisation has the tool and has never published a version of it: publish it in the SLNG dashboard, because an agent calls a published version and there is none to attach"
		out.finding = found
		return out
	}

	// The metadata read, for the two fields the version envelope does not carry.
	identity, err := readToolIdentity(runner, cache, chosen.ID)
	if err != nil {
		found.State = notChecked
		found.Detail = fmt.Sprintf("%v, so this reference's version and contract were not checked", err)
		out.finding = found
		return out
	}
	if identity.Source == "curated" {
		found.State = wrongKind
		found.Detail = fmt.Sprintf(
			"this is a capability SLNG curates rather than a tool this organisation owns, and it has no published definition to attach by version: reach it with `builtin: %s` instead, which resolves by name and needs no version",
			requirement.Name)
		out.finding = found
		return out
	}
	if account := resources.Account.Account.OrgID; account != "" && identity.OrganisationID != "" && identity.OrganisationID != account {
		// The listing and the record disagree about whose tool this is. Reading
		// on would validate a contract belonging to another organisation.
		found.State = wrongKind
		found.Detail = fmt.Sprintf(
			"the tool of this name belongs to organisation %s and this run resolved %s: the checks and the push must use one account, so select the right profile or key and run again",
			identity.OrganisationID, account)
		out.finding = found
		return out
	}

	snapshot, err := readPublishedVersion(runner, cache, chosen.ID, chosen.LatestVersion)
	if err != nil {
		found.State = notChecked
		found.Detail = fmt.Sprintf("%v, so this reference's published contract was not checked", err)
		out.finding = found
		return out
	}

	out.ToolID, out.Version = chosen.ID, snapshot.Version
	out.ContentHash, out.Snapshot = snapshot.ContentHash, snapshot.Snapshot
	found.State = satisfied
	out.finding = found
	return out
}

// absentHostedDetail says what to do, and it says a different thing for a
// scalar reference than for a legacy one, because the fix is different: an
// alias is corrected on the `slng:` line and a legacy reference by renaming the
// file. Which one this is, is readable from the requirement's own Where, which
// the compiler wrote to name the line the author edits.
func absentHostedDetail(requirement generate.Requirement, catalogue []string, near string) string {
	fix := fmt.Sprintf("correct the name on the `slng:` line in %s", requirementFile(requirement))
	if !strings.Contains(requirement.Where, "`slng: ") {
		fix = fmt.Sprintf("write the hosted tool's name on the `slng:` line in %s, which is what resolves the reference now, or rename the file", requirementFile(requirement))
	}
	if near != "" {
		return fmt.Sprintf("this organisation has %q, and names are matched exactly and are case-sensitive: %s", near, fix)
	}
	return fmt.Sprintf("this organisation has no tool of this name (it has %s). Either %s, or create the tool in the SLNG dashboard: a deploy creates none",
		joinNames(catalogue), fix)
}

// requirementFile is the package path an author edits.
//
// From the Source field the compiler set, not from the Where sentence. Reading
// it out of prose worked and was one reword away from silently returning
// nothing, which would have left every injected argument unchecked and the
// deploy reporting success.
func requirementFile(requirement generate.Requirement) string {
	if requirement.Source == "" {
		return "the tool file"
	}
	return "tools/" + requirement.Source + ".yaml"
}

// vaultNeed is one entry a published contract needs, and the kind it needs it
// as. The kind matters: a secret and a variable are different things in the
// vault, created differently, and an author looking at a flat list cannot tell
// which one a name is.
type vaultNeed struct {
	Name  string
	Kind  string
	Where string
}

// publishedVaultNeeds are the vault entries a checked published version reads.
//
// Discovered from the platform rather than declared in the package, which is
// FR-010: a hosted tool's credentials are the platform's, and asking an author
// to repeat them creates two owners and one of them goes stale. Names and kinds
// only. No value, no auth block and no config payload leaves this function.
func publishedVaultNeeds(resolved resolvedTool) []vaultNeed {
	var needs []vaultNeed
	seen := map[string]bool{}
	add := func(name, kind, where string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		needs = append(needs, vaultNeed{Name: name, Kind: kind, Where: where})
	}
	origin := requirementFile(resolved.Requirement)
	because := fmt.Sprintf("the published version %d of `%s` reads it, and %s references that tool",
		resolved.Version, resolved.Requirement.Name, origin)

	for _, name := range resolved.Snapshot.DeclaredSecrets {
		add(name, "secret", because)
	}
	// The auth block and the header list. A request tool commonly declares no
	// secret and names one here instead, which is the shape
	// `tool_get_api_request.json` captured: `declared_secrets` empty and
	// `config.auth.secret_name` set. Reading only the first list would report
	// nothing missing for a tool that cannot authenticate.
	config := resolved.Snapshot.Config
	if auth, ok := config["auth"].(map[string]any); ok {
		if name, ok := auth["secret_name"].(string); ok {
			add(name, "secret", because)
		}
	}
	if headers, ok := config["headers"].([]any); ok {
		for _, entry := range headers {
			header, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if name, ok := header["secret_name"].(string); ok {
				add(name, "secret", because)
			}
		}
	}
	// A vault *variable* rather than a secret, and the two are not
	// interchangeable: SLNG substitutes a variable into text and holds a secret
	// for a credential. A URL template and an argument default are the two
	// places a token like this appears.
	if url, ok := config["url"].(string); ok {
		for _, name := range templateVaultNames(url) {
			add(name, "variable", because)
		}
	}
	for _, key := range sortedMapKeys(resolved.Snapshot.ArgumentDefaults) {
		if text, ok := resolved.Snapshot.ArgumentDefaults[key].(string); ok {
			for _, name := range templateVaultNames(text) {
				add(name, "variable", because+", as the default for `"+key+"`")
			}
		}
	}
	return needs
}

// mcpVaultNeeds are the vault entries a hosted MCP server's own connection
// reads. Its address, its transport and its credential are SLNG's, so these are
// read off the server record and never off the package: a code target's
// `url_env` and `auth:` are that target's, and reporting them here would ask an
// author to put a code target's environment into the SLNG vault.
func mcpVaultNeeds(record slngMCPRecord, origin string) []vaultNeed {
	var needs []vaultNeed
	for _, name := range record.vaultNames() {
		kind := "secret"
		// A token inside the URL template is a variable: SLNG substitutes it
		// into the address rather than sending it as a credential.
		if slices.Contains(templateVaultNames(record.URLTemplate), name) {
			kind = "variable"
		}
		needs = append(needs, vaultNeed{
			Name: name, Kind: kind,
			Where: fmt.Sprintf("the hosted MCP server `%s` uses it to connect, and %s selects tools from that server", record.Name, origin),
		})
	}
	return needs
}

// compareDiscoveredVault checks discovered needs against the account, reusing
// the same comparison the package's declared names go through. One predicate,
// because a second one would drift and the two would disagree about what
// "empty" means.
func compareDiscoveredVault(needs []vaultNeed, resources slngResources) []finding {
	vaultChecked := checked(resources.Unchecked, target.SlngSecretList)
	var findings []finding
	for _, need := range needs {
		findings = append(findings, compareVault(
			generate.Requirement{Name: need.Name, Where: need.Where}, need.Kind, resources.Vault, vaultChecked, true))
	}
	return findings
}

// dedupeNeeds keeps the first mention of each name and kind, so two tools
// reading one credential produce one finding rather than two identical ones.
func dedupeNeeds(needs []vaultNeed) []vaultNeed {
	var out []vaultNeed
	seen := map[string]bool{}
	for _, need := range needs {
		key := need.Kind + "\x00" + need.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, need)
	}
	return out
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// resolveMCP checks each hosted MCP server and each tool the package selected
// from it.
//
// refresh is what separates a preview from a deployment, and it is the one
// remote write on this path. A stored capability snapshot goes stale, and the
// only way to renew it is to make the server's own discovery handshake, which
// is a change to remote state. So a dry run reports a stale record and refreshes
// nothing, and a real deploy refreshes each unusable record at most once and
// then rechecks it. Discovery is not execution: it asks the server what it
// offers and runs none of the tools the package selected.
//
// Every finding here is Required. A selected MCP tool the deploy could not check
// is a tool that would be attached unverified, and the whole promise is that it
// would not be.
func resolveMCP(
	runner *voiceaiRunner, cache *resolveCache,
	requires generate.Requirements, resources slngResources, refresh bool,
) (map[string]slngMCPRecord, []resolvedMCP, []finding) {
	// One moment for the whole run, so two servers are judged against the same
	// clock and a slow deploy cannot call the first fresh and the second stale.
	return resolveMCPAt(runner, cache, requires, resources, refresh, time.Now())
}

func resolveMCPAt(
	runner *voiceaiRunner, cache *resolveCache,
	requires generate.Requirements, resources slngResources, refresh bool, now time.Time,
) (map[string]slngMCPRecord, []resolvedMCP, []finding) {
	mcpChecked := checked(resources.Unchecked, target.SlngMCPList)
	records := map[string]slngMCPRecord{}
	var findings []finding
	// refreshed names the servers this run actually probed, so a later failure
	// can still say what was changed. Reporting "nothing changed" after a real
	// discovery would be false.
	refreshed := map[string]bool{}

	names := make([]string, 0, len(resources.MCPServer))
	for _, server := range resources.MCPServer {
		names = append(names, server.Name)
	}

	for _, requirement := range requires.MCPServers {
		found := finding{Requirement: requirement, Kind: "mcp server", Required: true}
		if !mcpChecked {
			found.State = notChecked
			found.Detail = "the account's MCP servers could not be listed, so this server's capabilities were not checked and its selected tools cannot be attached unverified"
			findings = append(findings, found)
			continue
		}
		var chosen slngMCPServer
		for _, server := range resources.MCPServer {
			if server.Name == requirement.Name {
				chosen = server
			}
		}
		if chosen.ID == "" {
			found.State = absent
			found.NearMiss = nearMiss(requirement.Name, names)
			found.Detail = absentServerDetail(names, found.NearMiss, requirement.Name)
			findings = append(findings, found)
			continue
		}

		record, err := readMCPRecord(runner, cache, chosen.ID)
		if err != nil {
			found.State = notChecked
			found.Detail = fmt.Sprintf("%v, so this server's capabilities were not checked", err)
			findings = append(findings, found)
			continue
		}
		if !record.usableAt(now) && refresh {
			probed, probeErr := refreshMCPRecord(runner, cache, chosen.ID)
			refreshed[requirement.Name] = true
			if probeErr != nil {
				found.State = notChecked
				found.Detail = fmt.Sprintf("its stored capability snapshot was not usable (%s), and connecting to refresh it did not work: %v",
					recordState(record, now), probeErr)
				findings = append(findings, found)
				continue
			}
			record = probed
		}
		records[requirement.Name] = record

		switch {
		case !record.usableAt(now) && !refresh:
			found.State = notChecked
			found.Detail = fmt.Sprintf("its stored capability snapshot is %s. A dry run does not connect to a server, so this was reported and not refreshed: a real deploy refreshes it once and checks it again",
				recordState(record, now))
		case !record.usableAt(now):
			found.State = notChecked
			found.Detail = fmt.Sprintf("its capability snapshot is still %s after a refresh, so the tools this package selects from it were not checked", recordState(record, now))
		default:
			found.State = satisfied
			if refreshed[requirement.Name] {
				found.Detail = "its stored capability snapshot was refreshed by connecting to the server, which changed that record on SLNG"
			}
		}
		findings = append(findings, found)
	}

	var resolved []resolvedMCP
	for _, requirement := range requires.MCPTools {
		found := finding{Requirement: requirement, Kind: "mcp tool", Required: true}
		record, ok := records[requirement.Server]
		if !ok {
			found.State = notChecked
			found.Detail = fmt.Sprintf("server %q was not checked, so this tool was not either", requirement.Server)
			findings = append(findings, found)
			continue
		}
		entry, present := record.Tool(requirement.Name)
		switch {
		case present && entry.SchemaHash == "":
			// Present and hashless. The hash is what a resolved push attaches,
			// and there is nothing to attach.
			found.State = notChecked
			found.Detail = fmt.Sprintf("server %q lists this tool and recorded no schema hash for it, which is the value an attachment carries: refresh the server's capabilities and deploy again", requirement.Server)
		case present:
			found.State = satisfied
			resolved = append(resolved, resolvedMCP{Requirement: requirement, ServerID: record.ID, SchemaHash: entry.SchemaHash})
		case !record.usableAt(now):
			// Cannot say it is absent. An incomplete probe found nothing about
			// it, which is a different fact from the server not offering it.
			found.State = notChecked
			found.Detail = fmt.Sprintf("server %q's capability snapshot is %s, so it cannot establish whether this tool exists", requirement.Server, recordState(record, now))
		default:
			offered := make([]string, 0, len(record.Capabilities.Tools))
			for _, tool := range record.Capabilities.Tools {
				offered = append(offered, tool.Name)
			}
			found.State = absent
			found.NearMiss = nearMiss(requirement.Name, offered)
			found.Detail = fmt.Sprintf("server %q does not offer this tool. It offers %s", requirement.Server, joinNames(offered))
			if found.NearMiss != "" {
				found.Detail = fmt.Sprintf("server %q offers %q, and names are matched exactly: correct the spelling under `mcp.tools`", requirement.Server, found.NearMiss)
			}
		}
		findings = append(findings, found)
	}
	return records, resolved, findings
}

// recordState says why a capability snapshot cannot settle a question, in the
// platform's own words where it had any. It names the distinction rather than
// collapsing it, because "failed" and "incomplete" send an author to different
// places.
func recordState(record slngMCPRecord, now time.Time) string {
	switch {
	case !record.healthy() && record.StatusError != "":
		return fmt.Sprintf("%s (%s)", record.Status, firstLine(record.StatusError))
	case !record.healthy():
		return record.Status
	case record.Capabilities.Truncated:
		return fmt.Sprintf("truncated after %d page(s), so it lists some of the server's tools and not all of them", record.Capabilities.PagesFetched)
	// Each way a snapshot can fail to be current reads differently, because
	// they send an author to different places. A rendered deadline of "" told
	// nobody anything, and it was the commonest of the three.
	case record.ObservedAt == "":
		return "incomplete: it records no observation time, so nothing says how old the snapshot is and the platform refuses one it cannot date"
	case record.NextRefreshAt == "":
		return fmt.Sprintf("of unknown age: it was observed at %s and declares no refresh schedule, so nothing on the record says whether the platform still counts it, and a push refuses a snapshot older than its own window",
			record.ObservedAt)
	case !record.fresh(now):
		return fmt.Sprintf("stale: it was observed at %s and the platform stops counting it at %s, so a push would refuse it",
			record.ObservedAt, record.NextRefreshAt)
	}
	return "usable"
}

func absentServerDetail(names []string, near, want string) string {
	if near != "" {
		return fmt.Sprintf("the account has %q, and names are matched exactly: correct the `server:` name in the package", near)
	}
	if len(names) == 0 {
		return "this organisation has no MCP servers attached at all. An MCP server is attached in the SLNG dashboard; unmute cannot create one"
	}
	return fmt.Sprintf("this organisation has %s. An MCP server is attached in the SLNG dashboard; unmute cannot create one", joinNames(names))
}

// resolveBuiltins turns each curated reference into an id and a version.
//
// A guarded push attaches ids, and it attaches them for every reference: a
// builtin needs nothing *created*, and its `tool_id` still has to be filled in.
// So a curated capability is resolved here rather than left to the push, which
// is the change from the name-only path where the push looked it up itself.
//
// The findings stay comparePreflight's. This adds identities to references that
// already have a finding, and a second finding per builtin would be counted
// twice in "3 of 5 satisfied".
func resolveBuiltins(requires generate.Requirements, resources slngResources) []resolvedTool {
	resolved := make([]resolvedTool, 0, len(requires.Builtins))
	for _, requirement := range requires.Builtins {
		out := resolvedTool{Requirement: requirement, finding: finding{
			Requirement: requirement, Kind: "builtin tool", State: satisfied,
		}}
		// A name can be held at two scopes at once: SLNG publishes `end_call`
		// to everybody and an organisation can have its own. The
		// organisation's is the one its dashboard attaches, so it wins, and the
		// choice is visible in the deployment report rather than silent.
		var chosen slngAccountTool
		for _, tool := range resources.Tools {
			if tool.Name != requirement.Name {
				continue
			}
			if chosen.ID == "" || (chosen.Scope != "organisation" && tool.Scope == "organisation") {
				chosen = tool
			}
		}
		if chosen.ID == "" {
			// comparePreflight already reported this by name, so this only
			// stops the reference reaching the staging with no identity.
			out.finding.State = absent
			resolved = append(resolved, out)
			continue
		}
		version := chosen.LatestVersion
		if version < 1 {
			// A curated capability the listing reports no version for. One is
			// the platform's own floor for a published capability, and a
			// reference with no version reaches the push unresolved.
			version = 1
		}
		out.ToolID, out.Version = chosen.ID, version
		resolved = append(resolved, out)
	}
	return resolved
}

// hostedDriftWarning adds the stale-mirror note to a resolved reference.
//
// Only a package that also targets livekit or pipecat has a committed mirror,
// and only a mirror records the version it was taken from. When it disagrees
// with the version this run resolved, the mirror is behind: the agent SLNG runs
// calls the published version either way, so the risk is a stale code-target
// build rather than a broken deploy, and that makes it a warning.
//
// A scalar reference with no mirror records no version and gets no note. There
// is nothing to be out of step with, which is the point of the shape.
func hostedDriftWarning(reference resolvedTool) finding {
	found := reference.finding
	if found.State != satisfied || reference.Requirement.Version < 1 || reference.Version == reference.Requirement.Version {
		return found
	}
	found.State = stale
	found.Detail = fmt.Sprintf(
		"this tool is at version %d in this organisation and the committed mirror was taken from version %d: "+
			"run `unmute pull` to update it, or deploy knowing the agent will call the organisation's version and not the one in this package",
		reference.Version, reference.Requirement.Version)
	return found
}

// duplicateHostedFindings refuses two local references that resolved to one
// hosted tool.
//
// SLNG keys an attachment by tool id, so one id cannot hold two sets of
// attachment settings. A push handed both references reuses a single attachment
// for them, and whichever is written second decides the description, the
// invocation and every argument override; the other reference's settings are
// gone with nothing said. That is the one outcome worse than a refusal, so this
// refuses before anything is staged and names both files, because the author's
// question is "which two of my tools are the same tool" and only the filenames
// answer it.
//
// Builtins are included rather than compared separately: a name can be held at
// both scopes at once, so an organisation publishing its own `end_call` makes
// `slng: end_call` and `builtin: end_call` the same id.
//
// Aliasing is not the problem and is not refused. Two files may reference two
// different hosted tools under any local names they like; what cannot work is
// two files arriving at the same remote id.
func duplicateHostedFindings(tools, builtins []resolvedTool) []finding {
	byID := map[string][]resolvedTool{}
	var order []string
	for _, reference := range append(append([]resolvedTool(nil), tools...), builtins...) {
		if !reference.usable() {
			// An unresolved reference has no id to collide on, and its own
			// finding already says why it is not usable.
			continue
		}
		if _, seen := byID[reference.ToolID]; !seen {
			order = append(order, reference.ToolID)
		}
		byID[reference.ToolID] = append(byID[reference.ToolID], reference)
	}

	var findings []finding
	for _, id := range order {
		group := byID[id]
		if len(group) < 2 {
			continue
		}
		files := make([]string, 0, len(group))
		for _, reference := range group {
			files = append(files, requirementFile(reference.Requirement))
		}
		sort.Strings(files)
		findings = append(findings, finding{
			Requirement: group[0].Requirement,
			Kind:        "tool reference",
			State:       wrongKind,
			Detail: fmt.Sprintf("%s reference the same hosted tool %q (%s). SLNG attaches one hosted tool once, so the two cannot carry separate descriptions, invocations or `inject:` values: the push would keep one file's settings and drop the other's without saying which. Delete one of the files, or point one of them at a different hosted tool",
				joinNames(files), group[0].Requirement.Name, id),
		})
	}
	return findings
}
