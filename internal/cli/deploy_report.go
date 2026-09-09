package cli

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
)

// What a deployment would change, or did.
//
// A push REPLACES. The agent that comes out has exactly what the package
// declares, and everything else on it is gone: an attachment somebody edited in
// the dashboard, a `call_start` trigger, a description written on the platform
// side. Those are the things an author most needs to see before agreeing, and
// they are the things the push's own report is coarsest about.
//
// So this compares the agent as it stands against what this run resolved, and
// names the difference per attachment. It says nothing about behaviour: two
// versions of a tool having different parameters is a fact, and "the tool now
// works differently" is a guess, so only the first is reported.
//
// Three rules about what does NOT go in here.
//
// A secret value never does. Nor does an auth block, a header list, a URL
// template, or any other raw provider payload: the report is names and
// metadata, because it is written to disk beside the build and an author may
// well commit it by accident.
//
// A previous state that could not be read is written as unknown and never as
// empty. "I could not see what is there" and "there is nothing there" produce
// opposite previews, and only one of them is honest about an agent full of
// attachments nobody could enumerate.
//
// And nothing reads this file back. It is output. A future deploy resolves the
// latest published versions anew, so treating yesterday's report as a version
// pin would quietly turn it into the lockfile this feature exists to remove.

// deployReportVersion is the report's own format version, so a reader can tell
// which shape it is holding. It is bumped when a field's meaning changes, never
// when one is added.
const deployReportVersion = 1

type deployReport struct {
	ReportVersion int    `json:"report_version"`
	Target        string `json:"target"`
	Provider      string `json:"provider"`
	// Organisation is the account every read and write in this run used. It is
	// here because an exported key and a stored profile can resolve to
	// different ones, and a report that did not say which would be unreadable a
	// week later.
	Organisation string `json:"organisation"`
	// Agent is the deployed name, which is the package's `name:` joined to this
	// target, and the id when one was established.
	Agent   string `json:"agent"`
	AgentID string `json:"agent_id,omitempty"`
	// Action is "create" or "update" where the push could establish it.
	Action string `json:"action,omitempty"`
	DryRun bool   `json:"dry_run"`
	// Outcome is what happened: "previewed", "blocked", "deployed", or
	// "partial" when something was written and something else then failed.
	Outcome string `json:"outcome"`

	Tools []deployReportTool `json:"tools,omitempty"`
	MCP   []deployReportMCP  `json:"mcp,omitempty"`
	// Removals are attachments the live agent has and this package does not
	// name, so a replacement detaches them.
	Removals []deployReportRemoval `json:"removals,omitempty"`
	Checks   []deployReportCheck   `json:"checks,omitempty"`
	// SideEffects are remote changes this run definitely made before it
	// finished, in order. It exists so a failed run cannot report that nothing
	// happened after it refreshed an MCP snapshot or created a vault entry.
	SideEffects []string `json:"side_effects,omitempty"`
	// PreviousStateUnknown is set when a required baseline read did not
	// succeed, so every "previous" below is absent rather than empty.
	PreviousStateUnknown bool     `json:"previous_state_unknown,omitempty"`
	Notes                []string `json:"notes,omitempty"`
}

type deployReportTool struct {
	// Source is the package tool file's own name and Tool is the hosted name.
	// Both, because a `slng:` scalar lets them differ and a reader tracing a
	// finding needs the file.
	Source string `json:"source"`
	Tool   string `json:"tool"`
	ToolID string `json:"tool_id,omitempty"`
	// PreviousVersion is what the agent has attached now. Absent when there was
	// no earlier attachment, or when the live agent could not be read.
	PreviousVersion *int `json:"previous_version,omitempty"`
	// ProposedVersion is the version this run checked and would attach, and on
	// a successful deploy it is the version actually attached.
	ProposedVersion int `json:"proposed_version,omitempty"`
	// New marks an attachment the agent does not have yet. It is separate from
	// an empty PreviousVersion, because that is also what an unreadable agent
	// produces, and those are different facts.
	New bool `json:"new,omitempty"`
	// Changes are the attachment settings a replacement would alter, as paths
	// an author can find: "description", "argument_overrides.query",
	// "invocation", "trigger on `call_start`",
	// "execution_policy.pre_action_message".
	Changes []string `json:"changes,omitempty"`
	// PublishedDescriptionChanged and PublishedParametersChanged compare the two
	// published versions, and they say only that the contract differs. Neither
	// says the tool behaves the same or differently: a schema comparison cannot
	// know that, and claiming it could is the one thing this report must not do.
	PublishedDescriptionChanged bool `json:"published_description_changed,omitempty"`
	PublishedParametersChanged  bool `json:"published_parameters_changed,omitempty"`
}

type deployReportMCP struct {
	Source string `json:"source,omitempty"`
	Server string `json:"server"`
	Tool   string `json:"tool_name"`
	// SchemaHash is the snapshot this run checked. A push in the guarded mode
	// attaches this exact value or refuses.
	SchemaHash string `json:"observed_schema_hash,omitempty"`
	// PreviousSchemaHash is what the agent has attached now, when it could be
	// read.
	PreviousSchemaHash string   `json:"previous_schema_hash,omitempty"`
	Changes            []string `json:"changes,omitempty"`
	// Refreshed marks a server whose stored snapshot this run renewed, which is
	// a change to remote state and is reported whether or not the run went on
	// to succeed.
	Refreshed bool `json:"refreshed,omitempty"`
}

type deployReportRemoval struct {
	Kind string `json:"kind"`
	// Name is the best identity available. A tool the package no longer names
	// may only be knowable by its id, and saying so is more use than omitting
	// the row.
	Name string `json:"name"`
	// Settings are what goes with it, so a dashboard-only trigger is visible
	// rather than being one word in a list.
	Settings []string `json:"settings,omitempty"`
}

type deployReportCheck struct {
	// Kind and Name are the finding's own, so this report and the printed
	// refusal name the same thing.
	Kind string `json:"kind"`
	Name string `json:"name"`
	// State is "satisfied", "deferred", or "blocked".
	State string `json:"state"`
	// Source is the package line that asked for it.
	Source string `json:"source,omitempty"`
	// Remedy is what to do, where it is known.
	Remedy string `json:"remedy,omitempty"`
}

// compareAttachments builds the tool rows by comparing what this run resolved
// against what the agent has now.
//
// live is the agent as read, and liveKnown says whether that read succeeded. The
// second argument is not optional politeness: without it an unreadable agent and
// a brand new one produce the same rows, and the first is a preview that hides
// every removal.
// proposedSettings is what the package declares for each attachment, keyed by
// the tool file's own name.
//
// One value rather than three maps threaded through two signatures, and that is
// not tidiness: two of the three are `map[string]string`, so as positional
// arguments they are interchangeable to the compiler and a swap would be a
// preview that reports the wrong setting with no build error.
//
// Announce is the field this type was made for. Without it the preview had the
// agent's live announcement and nothing to compare it against, so it reported
// every one as a setting "this package cannot declare, so a replacement removes
// it" — including a sentence the package was about to write back word for word.
type proposedSettings struct {
	// Arguments is each tool file's authored `inject:` map.
	Arguments map[string]map[string]any
	// Description is each hosted tool file's own `description:`, where the
	// author wrote one, so the preview can tell an override the package
	// declares from one only the agent has.
	Description map[string]string
	// Announce is each tool file's own `announce:`, which compiles to the
	// attachment's execution_policy.pre_action_message.
	Announce map[string]string
	// Config is each tool file's own config override, which today is a builtin
	// send_sms's sender, so the preview compares it rather than calling every
	// config override a setting the package cannot declare.
	Config map[string]map[string]any
}

func compareAttachments(
	resolved []resolvedTool, proposed proposedSettings,
	live slngLiveAgent, liveKnown bool,
	previous map[string]slngPublishedVersion,
) []deployReportTool {
	rows := make([]deployReportTool, 0, len(resolved))
	for _, reference := range resolved {
		source := reference.Requirement.Source
		row := deployReportTool{
			Source: source, Tool: reference.Requirement.Name,
			ToolID: reference.ToolID, ProposedVersion: reference.Version,
		}
		if !liveKnown {
			rows = append(rows, row)
			continue
		}
		attached, found := liveAttachment(live, reference.ToolID)
		if !found {
			// A new attachment. There is no previous version, and inventing one
			// would be the invention FR-013 forbids.
			//
			// Changes stays empty rather than carrying a sentence, because every
			// entry in it is read as "would change <this>": a row saying it
			// would change "attached for the first time" is not English. The
			// absent PreviousVersion is what says it is new, and the preview
			// prints that.
			row.New = true
			rows = append(rows, row)
			continue
		}
		version := attached.Version
		row.PreviousVersion = &version
		row.Changes = attachmentChanges(attached, reference, proposed, source)
		// Drift between the version attached and the version being attached,
		// and only where BOTH contracts were actually read.
		//
		// The proposed side is the half that used to be assumed. A builtin
		// resolves to an id and a version and no snapshot, because a curated
		// capability has no published contract this deploy checks a binding
		// against, so comparing the baseline against its empty snapshot
		// reported that every builtin's description and parameters had changed.
		// A comparison missing one side is left out rather than guessed at,
		// which is the same rule the absent baseline already followed.
		if old, ok := previous[snapshotKey(reference.ToolID, attached.Version)]; ok && reference.Snapshot.read() {
			row.PublishedDescriptionChanged = old.Snapshot.Description != reference.Snapshot.Description
			row.PublishedParametersChanged = !reflect.DeepEqual(old.Snapshot.ArgumentSchema, reference.Snapshot.ArgumentSchema)
		}
		rows = append(rows, row)
	}
	return rows
}

// attachmentChanges is what a replacement would alter on one attachment, as
// paths.
//
// The comparison runs from the live side as well as the package side, because
// the settings that matter most here are the ones the package does NOT declare:
// a description typed into the dashboard, an invocation switched to `system`, a
// `call_start` trigger. Those disappear under a replacement and only a
// live-side walk sees them.
func attachmentChanges(attached slngLiveTool, reference resolvedTool, proposed proposedSettings, source string) []string {
	var changes []string
	injected, authored := proposed.Arguments[source], proposed.Description[source]
	// SLNG stores the inherited description on the attachment too. Compare
	// the words the next deployment will use, not just the authored override.
	switch {
	case attached.Description == authored:
	case authored == "" && reference.Snapshot.read() && attached.Description == reference.Snapshot.Description:
	case attached.Description != "" && authored == "":
		changes = append(changes, "description, which the agent has now and this package does not declare, so a replacement removes it and the published description is used instead")
	case attached.Description == "":
		changes = append(changes, "description, which this package declares and the agent does not have, so a replacement overrides the published one")
	default:
		changes = append(changes, "description, which differs from the one the agent has now")
	}
	if attached.Version != reference.Version {
		changes = append(changes, fmt.Sprintf("version, from %d to %d", attached.Version, reference.Version))
	}
	if attached.Invocation != "" && attached.Invocation != "model" {
		changes = append(changes, fmt.Sprintf("invocation, which is %q on the agent now and %q in this package", attached.Invocation, "model"))
	}
	// The trigger, from where the platform actually keeps it. This read a
	// top-level `trigger` field the platform does not have, so a real preview
	// reported the invocation changing to `system` and said nothing about the
	// `call_start` trigger that made it one.
	if events := attached.triggers(); len(events) > 0 {
		changes = append(changes, fmt.Sprintf("trigger on %s, which the agent has now and this package cannot declare, so a replacement removes it and the tool is called by the model instead",
			joinNames(events)))
	}
	if names := attached.systemArguments(); len(names) > 0 {
		changes = append(changes, fmt.Sprintf("system arguments %s, which the agent supplies to this tool now and this package cannot declare, so a replacement removes them",
			joinNames(names)))
	}
	// The announcement, compared against the one this package proposes rather
	// than reported as a removal on sight. `announce:` IS an authoring key and
	// compiles to exactly this field, so a preview that read the live policy and
	// had nothing to compare it with told an author their own authored sentence
	// would be deleted by writing it.
	//
	// Four separate facts, each its own line, because they are separately
	// actionable: the sentence, whether it is switched on, whether the agent
	// waits for it, and anything in the policy this preview does not read.
	spoken, wait, enabled := attached.preAction()
	announced := proposed.Announce[source]
	switch {
	case spoken == announced:
	case announced == "":
		changes = append(changes, fmt.Sprintf("execution_policy.pre_action_message, the sentence the agent speaks before this tool runs (%q), which the agent has now and this package does not declare, so a replacement removes it", spoken))
	case spoken == "":
		changes = append(changes, "execution_policy.pre_action_message, the sentence this package's `announce:` speaks before this tool runs, which the agent does not have now")
	default:
		changes = append(changes, fmt.Sprintf("execution_policy.pre_action_message, from %q to this package's `announce:`", spoken))
	}
	if announced != "" && spoken != "" && !enabled {
		changes = append(changes, "execution_policy.pre_action_message.enabled, which is off on the agent now and this package's `announce:` turns on")
	}
	if wait {
		changes = append(changes, "execution_policy.pre_action_message.wait, which makes the agent wait for that sentence before the tool runs and this package cannot declare, so a replacement clears it")
	}
	changes = append(changes, attached.unreadPolicySettings()...)
	proposedConfig, declaresConfig := proposed.Config[source]
	switch {
	case !declaresConfig && len(attached.ConfigOverrides) > 0:
		changes = append(changes, "config_overrides, this attachment's own settings for the tool, which the agent has now and this package cannot declare, so a replacement removes them")
	case declaresConfig:
		// The one config a package declares is a send_sms sender. Compare that
		// field and say so; any other key the agent holds is one this package
		// cannot write back.
		want, _ := proposedConfig["from_number"].(string)
		have, _ := attached.ConfigOverrides["from_number"].(string)
		switch have {
		case want:
		case "":
			changes = append(changes, "config_overrides.from_number, the sender this package's `inject:` pins, which the agent does not have now")
		default:
			changes = append(changes, fmt.Sprintf("config_overrides.from_number, from %q to this package's `inject:`", have))
		}
		for _, key := range sortedMapKeys(attached.ConfigOverrides) {
			if key != "from_number" && key != "type" {
				changes = append(changes, fmt.Sprintf("config_overrides.%s, which the agent has now and this package cannot declare, so a replacement removes it", key))
			}
		}
	}
	for _, key := range sortedMapKeys(attached.Arguments) {
		want, supplied := injected[key]
		switch {
		case !supplied:
			changes = append(changes, fmt.Sprintf("argument_overrides.%s, which the agent has now and this package does not supply, so a replacement removes it and the model supplies the argument", key))
		case !sameArgument(attached.Arguments[key], want):
			changes = append(changes, fmt.Sprintf("argument_overrides.%s, which differs from what this package supplies", key))
		}
	}
	for _, key := range sortedMapKeys(injected) {
		if _, present := attached.Arguments[key]; !present {
			changes = append(changes, fmt.Sprintf("argument_overrides.%s, which this package supplies and the agent does not have", key))
		}
	}
	slices.Sort(changes)
	return changes
}

// sameArgument compares two override values through JSON, so an int read from
// YAML and a float64 read from the platform's JSON are not reported as a
// difference nobody made.
func sameArgument(live, want any) bool {
	encode := func(value any) string {
		raw, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprint(value)
		}
		return string(raw)
	}
	return encode(live) == encode(want)
}

// compareMCPAttachments builds the MCP rows the same way.
func compareMCPAttachments(
	resolved []resolvedMCP, records map[string]slngMCPRecord, refreshed map[string]bool,
	live slngLiveAgent, liveKnown bool,
) []deployReportMCP {
	rows := make([]deployReportMCP, 0, len(resolved))
	for _, selection := range resolved {
		row := deployReportMCP{
			Server: selection.Requirement.Server, Tool: selection.Requirement.Name,
			SchemaHash: selection.SchemaHash, Refreshed: refreshed[selection.Requirement.Server],
		}
		if !liveKnown {
			rows = append(rows, row)
			continue
		}
		for _, attached := range live.MCPRefs {
			if attached.ServerID != selection.ServerID || attached.ToolName != selection.Requirement.Name {
				continue
			}
			row.PreviousSchemaHash = attached.SchemaHash
			if attached.SchemaHash != selection.SchemaHash {
				row.Changes = append(row.Changes,
					"observed_schema_hash, so the server's tool has changed shape since this attachment was made")
			}
			// SLNG materializes the inherited MCP description on save. Only
			// differing words change when the replacement inherits it again.
			entry, _ := records[selection.Requirement.Server].Tool(selection.Requirement.Name)
			if attached.Description != "" && attached.Description != entry.Description {
				row.Changes = append(row.Changes,
					"description, which differs from the server's current description that a replacement inherits")
			}
			for _, key := range sortedMapKeys(attached.Arguments) {
				row.Changes = append(row.Changes, fmt.Sprintf(
					"argument_overrides.%s, which the agent has now and a package cannot declare on an MCP selection, so a replacement removes it and the model supplies the argument", key))
			}
			if len(attached.ExecutionPolicy) > 0 {
				row.Changes = append(row.Changes,
					"execution_policy, the message the agent speaks before this tool runs, which a package cannot declare on an MCP selection, so a replacement removes it")
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// attachmentRemovals are the live agent's attachments this package does not
// name. Each carries the settings that go with it, so a `call_start` trigger is
// visible as a thing being lost rather than as one word in a list.
//
// resolved has to be every reference this deploy attaches, builtins included.
// It was handed the hosted references alone, so a package's `end_call` was
// listed as an existing attachment and as a detachment in the same preview:
// each is read as an instruction and they contradict. buildDeployReport now
// hands one slice to this and to compareAttachments so the two cannot disagree
// again.
func attachmentRemovals(
	live slngLiveAgent, liveKnown bool,
	resolved []resolvedTool, mcp []resolvedMCP,
) []deployReportRemoval {
	if !liveKnown {
		return nil
	}
	keep := map[string]bool{}
	for _, reference := range resolved {
		keep[reference.ToolID] = true
	}
	var removals []deployReportRemoval
	for _, attached := range live.ToolRefs {
		if keep[attached.ToolID] {
			continue
		}
		removal := deployReportRemoval{Kind: "tool", Name: attached.ToolID}
		if attached.Description != "" {
			removal.Settings = append(removal.Settings, "description")
		}
		if attached.Invocation != "" && attached.Invocation != "model" {
			removal.Settings = append(removal.Settings, "invocation "+attached.Invocation)
		}
		for _, event := range attached.triggers() {
			removal.Settings = append(removal.Settings, "trigger on "+event)
		}
		for _, name := range attached.systemArguments() {
			removal.Settings = append(removal.Settings, "system argument "+name)
		}
		if len(attached.ExecutionPolicy) > 0 {
			removal.Settings = append(removal.Settings, "execution_policy")
		}
		if len(attached.ConfigOverrides) > 0 {
			removal.Settings = append(removal.Settings, "config_overrides")
		}
		for _, key := range sortedMapKeys(attached.Arguments) {
			removal.Settings = append(removal.Settings, "argument_overrides."+key)
		}
		removals = append(removals, removal)
	}

	keepMCP := map[string]bool{}
	for _, selection := range mcp {
		keepMCP[selection.ServerID+"\x00"+selection.Requirement.Name] = true
	}
	for _, attached := range live.MCPRefs {
		if keepMCP[attached.ServerID+"\x00"+attached.ToolName] {
			continue
		}
		removal := deployReportRemoval{Kind: "mcp tool", Name: attached.ServerID + " " + attached.ToolName}
		if attached.Description != "" {
			removal.Settings = append(removal.Settings, "description")
		}
		for _, key := range sortedMapKeys(attached.Arguments) {
			removal.Settings = append(removal.Settings, "argument_overrides."+key)
		}
		if len(attached.ExecutionPolicy) > 0 {
			removal.Settings = append(removal.Settings, "execution_policy")
		}
		removals = append(removals, removal)
	}
	sort.Slice(removals, func(i, j int) bool {
		if removals[i].Kind != removals[j].Kind {
			return removals[i].Kind < removals[j].Kind
		}
		return removals[i].Name < removals[j].Name
	})
	return removals
}

// reportChecks turns findings and deferred bindings into the report's own check
// rows, so what the report says and what the terminal printed are the same set.
func reportChecks(findings []finding, deferred []string) []deployReportCheck {
	rows := make([]deployReportCheck, 0, len(findings)+len(deferred))
	for _, found := range findings {
		state := "satisfied"
		switch {
		case found.blocks():
			state = "blocked"
		case found.State != satisfied:
			state = "deferred"
		}
		rows = append(rows, deployReportCheck{
			Kind: found.Kind, Name: found.Requirement.Name, State: state,
			Source: found.Requirement.Where, Remedy: found.Detail,
		})
	}
	for _, note := range deferred {
		rows = append(rows, deployReportCheck{
			Kind: "tool argument", Name: "", State: "deferred", Remedy: note,
		})
	}
	return rows
}

// liveAttachment finds one attachment by tool id.
func liveAttachment(live slngLiveAgent, toolID string) (slngLiveTool, bool) {
	for _, attached := range live.ToolRefs {
		if attached.ToolID != "" && attached.ToolID == toolID {
			return attached, true
		}
	}
	return slngLiveTool{}, false
}

func snapshotKey(id string, version int) string { return fmt.Sprintf("%s@%d", id, version) }

// marshalDeployReport writes the report. Indented and newline-terminated,
// because a person reads it beside the build.
func marshalDeployReport(report deployReport) ([]byte, error) {
	report.ReportVersion, report.Provider = deployReportVersion, "slng"
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
