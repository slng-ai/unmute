package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/generate"
)

// The attachment comparison, on explicit old and new state.
//
// A push REPLACES, so the interesting half of a preview is what the agent has
// and the package does not: a description typed into the dashboard, an
// invocation switched to `system`, a `call_start` trigger. Those are what
// disappear, and a comparison that walked only the package side would never see
// them.
//
// Every case here builds both sides by hand rather than through a stub, because
// the property under test is the diff and not the reads.

// liveWith is a live agent holding one attachment, edited as the case needs.
func liveWith(edit func(*slngLiveTool)) slngLiveAgent {
	attached := slngLiveTool{
		AttachmentID: "att-1", ToolID: "t-1", Version: 2, Invocation: "model",
		Arguments: map[string]any{"query": "{{customer_name}}"},
	}
	edit(&attached)
	return slngLiveAgent{ID: "agent-1", Name: "acme-support-slng", ToolRefs: []slngLiveTool{attached}}
}

// resolvedAt is the package side: the same tool, at the version this run
// resolved.
func resolvedAt(version int) resolvedTool {
	return resolvedTool{
		Requirement: generate.Requirement{
			Name: "search_places_text",
			// Source is what deployment keys the authored `inject:` values and
			// the authored description by, so a helper that left it empty would
			// silently check nothing. That is the failure mode the field
			// replaced a prose scan to prevent, and leaving it out here makes
			// these tests fail rather than pass vacuously.
			Source: "search_places_text",
			Where:  "tools/search_places_text.yaml references a tool SLNG hosts, so SLNG must already have one of that name",
		},
		ToolID: "t-1", Version: version,
		Snapshot: slngPublishedSnapshot{Name: "search_places_text", Description: "Search for places."},
		finding:  finding{State: satisfied},
	}
}

var suppliesQuery = map[string]map[string]any{"search_places_text": {"query": "{{customer_name}}"}}

// TestPreviewNamesWhatAReplacementWouldChange is the case list this comparison
// exists for. Each row is a setting an author can lose without being told.
func TestPreviewNamesWhatAReplacementWouldChange(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*slngLiveTool)
		want string
	}{
		{
			name: "a version the organisation has moved past",
			edit: func(*slngLiveTool) {},
			want: "version, from 2 to 4",
		},
		{
			// The half FR-004 turns on. The package authored no description, so
			// the attachment inherits the published one; the agent has an
			// explicit override, and a replacement removes it.
			name: "a description written in the dashboard",
			edit: func(a *slngLiveTool) { a.Description = "Edited on the platform." },
			want: "description, which the agent has now and this package does not declare",
		},
		{
			// The platform keeps a trigger under `system.triggers`, and this
			// read a top-level `trigger` field it does not have. A real
			// preview reported the invocation changing to `system` and said
			// nothing about the `call_start` trigger that made it one, so the
			// shape here is the platform's own.
			name: "a system trigger the package cannot declare",
			edit: func(a *slngLiveTool) {
				a.Invocation = "system"
				a.System = &slngLiveSystem{Triggers: []slngLiveTrigger{{Event: "call_start"}}}
			},
			want: "trigger on `call_start`, which the agent has now and this package cannot declare",
		},
		{
			name: "a system argument the package cannot declare",
			edit: func(a *slngLiveTool) {
				a.Invocation = "system"
				a.System = &slngLiveSystem{
					Triggers:  []slngLiveTrigger{{Event: "call_start"}},
					Arguments: []slngLiveArg{{Name: "order_number", Type: "string"}},
				}
			},
			want: "system arguments `order_number`",
		},
		{
			// The package below declares no `announce:`, so this one IS a
			// removal. The shape is the platform's own: a segmented text, not
			// the bare string this fixture used to carry, which is the same
			// mistake as the `trigger` field above and would have let a correct
			// reader find no sentence at all.
			name: "a pre-action message the package does not declare",
			edit: func(a *slngLiveTool) { a.ExecutionPolicy = livePreAction("One moment.", false, true) },
			want: `execution_policy.pre_action_message, the sentence the agent speaks before this tool runs ("One moment."), which the agent has now and this package does not declare`,
		},
		{
			// A field this preview does not read is still a field a replacement
			// removes, and naming it is the only way an author sees it.
			name: "a policy setting this preview does not read",
			edit: func(a *slngLiveTool) { a.ExecutionPolicy = map[string]any{"retry_after_seconds": float64(3)} },
			want: "execution_policy.retry_after_seconds, which the agent has now and this package cannot declare",
		},
		{
			name: "an attachment's own configuration of a curated capability",
			edit: func(a *slngLiveTool) {
				a.ConfigOverrides = map[string]any{"type": "end_call", "prompt": "Say goodbye warmly."}
			},
			want: "config_overrides, this attachment's own settings for the tool",
		},
		{
			name: "an invocation switched away from the model",
			edit: func(a *slngLiveTool) { a.Invocation = "system" },
			want: `invocation, which is "system" on the agent now`,
		},
		{
			name: "an argument override the package does not supply",
			edit: func(a *slngLiveTool) { a.Arguments["trace_id"] = "abc" },
			want: "argument_overrides.trace_id, which the agent has now and this package does not supply",
		},
		{
			name: "an argument override whose value differs",
			edit: func(a *slngLiveTool) { a.Arguments["query"] = "a fixed string" },
			want: "argument_overrides.query, which differs from what this package supplies",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := compareAttachments(
				[]resolvedTool{resolvedAt(4)}, proposedSettings{Arguments: suppliesQuery},
				liveWith(tc.edit), true, nil,
			)
			if len(rows) != 1 {
				t.Fatalf("rows = %+v", rows)
			}
			joined := strings.Join(rows[0].Changes, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Errorf("the preview does not say %q:\n%s", tc.want, joined)
			}
			// Both versions, every time. An author deciding whether to deploy
			// needs the one they have and the one they would get.
			if rows[0].PreviousVersion == nil || *rows[0].PreviousVersion != 2 {
				t.Errorf("the previous version is %v, want 2", rows[0].PreviousVersion)
			}
			if rows[0].ProposedVersion != 4 {
				t.Errorf("the proposed version is %d, want 4", rows[0].ProposedVersion)
			}
		})
	}
}

// TestPreviewInventsNoPreviousVersion: a first deployment has no baseline, and
// reporting one would be an invention. FR-013 says so and this is the case that
// would produce it.
func TestPreviewInventsNoPreviousVersion(t *testing.T) {
	rows := compareAttachments(
		[]resolvedTool{resolvedAt(4)}, proposedSettings{Arguments: suppliesQuery},
		slngLiveAgent{ID: "agent-1"}, true, nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].PreviousVersion != nil {
		t.Errorf("a new attachment reports a previous version of %d", *rows[0].PreviousVersion)
	}
	if !rows[0].New {
		t.Error("a new attachment is not marked new, so the preview cannot tell it from one whose agent could not be read")
	}
	// And the change list stays empty: every entry in it is read as "would
	// change <this>", so a sentence about being new does not belong there.
	if len(rows[0].Changes) != 0 {
		t.Errorf("a new attachment carries a change list: %v", rows[0].Changes)
	}
}

// TestPreviewSaysNothingWhenTheAgentCouldNotBeRead.
//
// The distinction that makes this comparison honest. An unreadable agent and a
// brand new one are opposite situations, and collapsing them would produce a
// preview reporting no removals for an agent full of them.
func TestPreviewSaysNothingWhenTheAgentCouldNotBeRead(t *testing.T) {
	rows := compareAttachments(
		[]resolvedTool{resolvedAt(4)}, proposedSettings{Arguments: suppliesQuery},
		slngLiveAgent{}, false, nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].PreviousVersion != nil {
		t.Error("an unreadable agent produced a previous version out of nothing")
	}
	if len(rows[0].Changes) != 0 {
		t.Errorf("an unreadable agent produced a change list: %v", rows[0].Changes)
	}
	// And no removals, because "I could not see it" is not "there is nothing
	// there". The report's own flag is what tells the reader.
	if removals := attachmentRemovals(slngLiveAgent{}, false, nil, nil); len(removals) != 0 {
		t.Errorf("an unreadable agent produced removals: %+v", removals)
	}
}

// TestPreviewReportsAContractChangeAndClaimsNothingAboutBehaviour.
//
// Two published versions differing in their parameters is a fact. "The tool now
// works differently" is a guess, and this report must never make it: a schema
// comparison cannot see a code change that kept the same signature.
func TestPreviewReportsAContractChangeAndClaimsNothingAboutBehaviour(t *testing.T) {
	old := map[string]slngPublishedVersion{
		snapshotKey("t-1", 2): {ToolID: "t-1", Version: 2, Snapshot: slngPublishedSnapshot{
			Description: "Search for places.",
			ArgumentSchema: map[string]any{"type": "object", "properties": map[string]any{
				"query": map[string]any{"type": "string"},
			}},
		}},
	}
	current := resolvedAt(4)
	current.Snapshot.Description = "Search for places, near a location."
	current.Snapshot.ArgumentSchema = map[string]any{"type": "object", "properties": map[string]any{
		"query": map[string]any{"type": "string"},
		"near":  map[string]any{"type": "string"},
	}}

	rows := compareAttachments([]resolvedTool{current}, proposedSettings{Arguments: suppliesQuery}, liveWith(func(*slngLiveTool) {}), true, old)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if !rows[0].PublishedParametersChanged {
		t.Error("a renamed and added parameter was not reported as a contract change")
	}
	if !rows[0].PublishedDescriptionChanged {
		t.Error("a changed published description was not reported")
	}

	// The unchanged case, so the flags mean something.
	same := resolvedAt(2)
	same.Snapshot = old[snapshotKey("t-1", 2)].Snapshot
	rows = compareAttachments([]resolvedTool{same}, proposedSettings{Arguments: suppliesQuery}, liveWith(func(*slngLiveTool) {}), true, old)
	if rows[0].PublishedParametersChanged || rows[0].PublishedDescriptionChanged {
		t.Error("an unchanged contract was reported as changed")
	}
}

// TestPreviewNamesEveryRemovalWithWhatGoesWithIt: an attachment the package no
// longer names is detached, and the settings on it go too. Naming the settings
// is what makes a `call_start` trigger visible as a thing being lost rather
// than one word in a list.
func TestPreviewNamesEveryRemovalWithWhatGoesWithIt(t *testing.T) {
	live := slngLiveAgent{
		ID: "agent-1",
		ToolRefs: []slngLiveTool{
			{AttachmentID: "att-1", ToolID: "t-1", Version: 2},
			{AttachmentID: "att-2", ToolID: "t-GONE", Version: 5, Invocation: "system",
				Description: "Written in the dashboard.",
				System: &slngLiveSystem{
					Triggers:  []slngLiveTrigger{{Event: "call_start"}},
					Arguments: []slngLiveArg{{Name: "caller", Type: "string"}},
				},
				ExecutionPolicy: map[string]any{"pre_action_message": map[string]any{"enabled": true}},
				Arguments:       map[string]any{"order_number": "MY_RANDOM_VALUE"}},
		},
		MCPRefs: []slngLiveMCPRef{
			{AttachmentID: "m-1", ServerID: "s-1", ToolName: "kept"},
			{AttachmentID: "m-2", ServerID: "s-1", ToolName: "dropped",
				Description: "Written in the dashboard.",
				Arguments:   map[string]any{"limit": float64(5)}},
		},
	}
	removals := attachmentRemovals(live, true,
		[]resolvedTool{resolvedAt(4)},
		[]resolvedMCP{{Requirement: generate.Requirement{Name: "kept", Server: "docs"}, ServerID: "s-1", SchemaHash: "h"}},
	)
	if len(removals) != 2 {
		t.Fatalf("removals = %+v, want the detached tool and the dropped MCP selection", removals)
	}
	var tool deployReportRemoval
	for _, removal := range removals {
		if removal.Kind == "tool" {
			tool = removal
		}
	}
	if tool.Name != "t-GONE" {
		t.Errorf("the detached tool is %q, want t-GONE", tool.Name)
	}
	joined := strings.Join(tool.Settings, ",")
	for _, want := range []string{
		"description", "invocation system", "trigger on call_start",
		"system argument caller", "execution_policy", "argument_overrides.order_number",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the removal does not name %q, so an author cannot see what goes with it: %s", want, joined)
		}
	}

	// An MCP selection carries its settings too. Comparing the schema hash
	// alone said a server's tool had changed shape and stayed silent about a
	// description and an argument override going in the same push.
	var mcp deployReportRemoval
	for _, removal := range removals {
		if removal.Kind == "mcp tool" {
			mcp = removal
		}
	}
	joinedMCP := strings.Join(mcp.Settings, ",")
	for _, want := range []string{"description", "argument_overrides.limit"} {
		if !strings.Contains(joinedMCP, want) {
			t.Errorf("the detached MCP selection does not name %q: %s", want, joinedMCP)
		}
	}
}

// TestReportCarriesNoSecretAndNoRawPayload.
//
// The report goes to disk beside the build and an author may commit it. So the
// rule is names and metadata: no value, no auth block, no header list, no URL.
func TestReportCarriesNoSecretAndNoRawPayload(t *testing.T) {
	reference := resolvedAt(4)
	reference.Snapshot.Config = map[string]any{
		"auth":    map[string]any{"type": "bearer", "secret_name": "PLACES_TOKEN"},
		"url":     "https://places.example.invalid/search",
		"headers": []any{map[string]any{"name": "X-Region", "secret_name": "REGION"}},
	}
	report := deployReport{
		Target: "slng", Organisation: "Example (org-1)", Agent: "acme-support-slng",
		Outcome: "previewed", DryRun: true,
		Tools: compareAttachments([]resolvedTool{reference}, proposedSettings{Arguments: suppliesQuery}, liveWith(func(*slngLiveTool) {}), true, nil),
		Checks: reportChecks([]finding{{
			Kind: "secret", State: absent,
			Requirement: generate.Requirement{Name: "PLACES_TOKEN", Where: "the published version reads it"},
			Detail:      "the vault holds no entry with this name",
		}}, nil),
	}
	raw, err := marshalDeployReport(report)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	// The NAME of a credential is exactly what the report is for; the shape of
	// the request it authenticates is not.
	if !strings.Contains(text, "PLACES_TOKEN") {
		t.Error("the report does not name the credential it says is missing, which is the whole point of it")
	}
	for _, forbidden := range []string{"bearer", "https://places", "X-Region", "argument_schema", "code_src"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("the report carries %q, which is a raw provider payload rather than a name", forbidden)
		}
	}
	// And it says which format it is, so a reader knows what they are holding.
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("the report is not JSON: %v", err)
	}
	if decoded["report_version"] != float64(deployReportVersion) {
		t.Errorf("report_version = %v, want %d", decoded["report_version"], deployReportVersion)
	}
	if decoded["provider"] != "slng" {
		t.Errorf("provider = %v", decoded["provider"])
	}
}

// TestReportChecksSeparateSatisfiedFromDeferredFromBlocked.
//
// The three states an author has to be able to tell apart, and the reason the
// report exists: a clean compile of a hosted reference is not a checked one,
// and a run that could not read something is not a run that found nothing.
func TestReportChecksSeparateSatisfiedFromDeferredFromBlocked(t *testing.T) {
	rows := reportChecks([]finding{
		{Kind: "hosted tool", State: satisfied, Requirement: generate.Requirement{Name: "ok"}},
		{Kind: "hosted tool", State: stale, Requirement: generate.Requirement{Name: "behind"}, Detail: "run `unmute pull`"},
		{Kind: "hosted tool", State: absent, Requirement: generate.Requirement{Name: "gone"}, Detail: "create it in the dashboard"},
		{Kind: "mcp tool", State: notChecked, Required: true, Requirement: generate.Requirement{Name: "unknown"}, Detail: "the snapshot was truncated"},
	}, []string{"query arrives when a call starts"})

	want := map[string]string{"ok": "satisfied", "behind": "deferred", "gone": "blocked", "unknown": "blocked"}
	got := map[string]string{}
	for _, row := range rows {
		if row.Name != "" {
			got[row.Name] = row.State
		}
	}
	for name, state := range want {
		if got[name] != state {
			t.Errorf("%s = %q, want %q", name, got[name], state)
		}
	}
	// A required check nobody could make is blocked, not deferred. That is the
	// rule spec 007 changed, and getting it backwards is how a deployment
	// reports success having verified nothing.
	var deferred int
	for _, row := range rows {
		if row.Name == "" && row.State == "deferred" {
			deferred++
		}
	}
	if deferred != 1 {
		t.Errorf("the deferred binding note is not in the report: %+v", rows)
	}
}

// TestPreviewNamesAnMCPSnapshotThatMovedOn: a server tool that changed shape
// since the attachment was made is a change an author should see, because the
// model's argument list for it changes with it.
func TestPreviewNamesAnMCPSnapshotThatMovedOn(t *testing.T) {
	live := slngLiveAgent{ID: "agent-1", MCPRefs: []slngLiveMCPRef{
		{AttachmentID: "m-1", ServerID: "s-1", ToolName: "search_docs", SchemaHash: "old-hash"},
	}}
	rows := compareMCPAttachments(
		[]resolvedMCP{{
			Requirement: generate.Requirement{Name: "search_docs", Server: "internal_docs"},
			ServerID:    "s-1", SchemaHash: "new-hash",
		}},
		map[string]slngMCPRecord{"internal_docs": {ID: "s-1", Name: "internal_docs"}},
		map[string]bool{"internal_docs": true},
		live, true,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].PreviousSchemaHash != "old-hash" || rows[0].SchemaHash != "new-hash" {
		t.Errorf("both hashes are not reported: %+v", rows[0])
	}
	if !strings.Contains(strings.Join(rows[0].Changes, ""), "observed_schema_hash") {
		t.Errorf("the change is not named: %v", rows[0].Changes)
	}
	// And the refresh is reported whether or not the run goes on to succeed,
	// because it changed a record on SLNG.
	if !rows[0].Refreshed {
		t.Error("a server whose snapshot this run refreshed is not marked, so a later failure could claim nothing changed")
	}
}

// TestPreviewSaysNothingAboutAnUnchangedDescription.
//
// The false positive the authored description exists to stop. An author who
// deliberately wrote an override, and whose agent already carries exactly those
// words, has changed nothing, and telling them a replacement would remove it is
// a preview they cannot act on.
//
// The three other combinations are changes, and each says which one it is,
// because "removed", "added" and "different" send an author to different edits.
func TestPreviewSaysNothingAboutAnUnchangedDescription(t *testing.T) {
	const words = "Search for places near the caller."
	for _, tc := range []struct {
		name     string
		onAgent  string
		authored string
		want     string
	}{
		{name: "the same words on both sides", onAgent: words, authored: words, want: ""},
		{name: "neither side declares one", onAgent: "", authored: "", want: ""},
		{name: "platform materialized the inherited description", onAgent: "Search for places.", want: ""},
		{name: "only the agent has one", onAgent: words, authored: "",
			want: "the agent has now and this package does not declare"},
		{name: "only the package has one", onAgent: "", authored: words,
			want: "this package declares and the agent does not have"},
		{name: "both, and they differ", onAgent: words, authored: "Different words.",
			want: "differs from the one the agent has now"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The same version on both sides, so a description is the only thing
			// that could be reported.
			rows := compareAttachments(
				[]resolvedTool{resolvedAt(2)}, proposedSettings{
					Arguments:   suppliesQuery,
					Description: map[string]string{"search_places_text": tc.authored},
				},
				liveWith(func(a *slngLiveTool) { a.Description = tc.onAgent }), true, nil,
			)
			joined := strings.Join(rows[0].Changes, "\n")
			if tc.want == "" {
				if strings.Contains(joined, "description") {
					t.Errorf("an unchanged description was reported as a change:\n%s", joined)
				}
				return
			}
			if !strings.Contains(joined, tc.want) {
				t.Errorf("the preview does not say %q:\n%s", tc.want, joined)
			}
		})
	}
}

func TestPreviewKeepsAnInheritedMCPDescription(t *testing.T) {
	var record slngMCPRecord
	if err := json.Unmarshal([]byte(`{"id":"s-1","name":"docs","capabilities":{"tools":[{"name":"search_docs","schema_hash":"h-search","description":"Search the docs."}]}}`), &record); err != nil {
		t.Fatal(err)
	}
	rows := compareMCPAttachments(
		[]resolvedMCP{{Requirement: generate.Requirement{Name: "search_docs", Server: "docs"}, ServerID: "s-1", SchemaHash: "h-search"}},
		map[string]slngMCPRecord{"docs": record}, nil,
		slngLiveAgent{MCPRefs: []slngLiveMCPRef{{ServerID: "s-1", ToolName: "search_docs", SchemaHash: "h-search", Description: "Search the docs."}}}, true,
	)
	if len(rows) != 1 || len(rows[0].Changes) != 0 {
		t.Fatalf("unchanged inherited MCP description reported as changed: %+v", rows)
	}
}

// livePreAction is the platform's own shape for the sentence an agent speaks
// before a tool runs: an enabled flag, a wait flag, and a segmented text.
//
// A helper rather than a literal at each call site, because the literal is what
// went wrong: a fixture carrying `"text": "One moment."` matched a substring
// assertion and told nobody that the platform nests the words two levels down.
func livePreAction(spoken string, wait, enabled bool) map[string]any {
	return map[string]any{"pre_action_message": map[string]any{
		"enabled": enabled, "wait": wait,
		"text": map[string]any{"segments": []any{
			map[string]any{"type": "literal", "value": spoken},
		}},
	}}
}

// TestPreviewComparesTheAnnouncementItWouldWrite.
//
// `announce:` is an authoring key. It compiles to the attachment's
// execution_policy.pre_action_message, and the preview was given the agent's
// live policy and nothing to compare it against, so it reported every one as a
// setting "this package cannot declare, so a replacement removes it". A real
// guarded dry run said that about a package whose own tool file declares the
// sentence, and the compiled body carries it: the author was told writing their
// announcement would delete it.
//
// The unchanged case is the one that matters, and the other three are here so
// that case is not passing by reporting nothing at all.
func TestPreviewComparesTheAnnouncementItWouldWrite(t *testing.T) {
	const words = "One moment while I look that up."
	for _, tc := range []struct {
		name     string
		onAgent  string
		authored string
		want     string
	}{
		{name: "the same sentence on both sides", onAgent: words, authored: words, want: ""},
		{name: "neither side declares one", onAgent: "", authored: "", want: ""},
		{name: "only the agent has one", onAgent: words, authored: "",
			want: "the agent has now and this package does not declare"},
		{name: "only the package has one", onAgent: "", authored: words,
			want: "which the agent does not have now"},
		{name: "both, and they differ", onAgent: words, authored: "Checking that now.",
			want: `from "One moment while I look that up." to this package's ` + "`announce:`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := compareAttachments(
				[]resolvedTool{resolvedAt(2)}, proposedSettings{
					Arguments: suppliesQuery,
					Announce:  map[string]string{"search_places_text": tc.authored},
				},
				liveWith(func(a *slngLiveTool) {
					if tc.onAgent != "" {
						a.ExecutionPolicy = livePreAction(tc.onAgent, false, true)
					}
				}), true, nil,
			)
			joined := strings.Join(rows[0].Changes, "\n")
			if tc.want == "" {
				if strings.Contains(joined, "pre_action_message") {
					t.Errorf("an unchanged announcement was reported as a change:\n%s", joined)
				}
				return
			}
			if !strings.Contains(joined, tc.want) {
				t.Errorf("the preview does not say %q:\n%s", tc.want, joined)
			}
		})
	}

	// The two flags beside the sentence. Each is its own line because each is
	// separately actionable, and `wait` is a real dashboard-only setting: the
	// package writes the field as false, so a replacement clears it.
	for _, tc := range []struct {
		name         string
		wait, enable bool
		want         string
	}{
		{name: "switched off on the agent", enable: false,
			want: "execution_policy.pre_action_message.enabled, which is off on the agent now"},
		{name: "the agent waits for it", wait: true, enable: true,
			want: "execution_policy.pre_action_message.wait, which makes the agent wait"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := compareAttachments(
				[]resolvedTool{resolvedAt(2)}, proposedSettings{
					Arguments: suppliesQuery,
					Announce:  map[string]string{"search_places_text": words},
				},
				liveWith(func(a *slngLiveTool) {
					a.ExecutionPolicy = livePreAction(words, tc.wait, tc.enable)
				}), true, nil,
			)
			joined := strings.Join(rows[0].Changes, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Errorf("the preview does not say %q:\n%s", tc.want, joined)
			}
		})
	}
}

// TestPreviewNeverCallsOneAttachmentBothKeptAndDetached.
//
// buildDeployReport compared the rows against every reference it attaches,
// builtins included, and built the removals from the hosted references alone. So
// a package's `end_call` was listed as an existing attachment and, four lines
// later, as an attachment that would be detached. Both lines read as
// instructions and they contradict, which leaves an author with no reading of
// the preview that is true.
//
// The two are readings of one set, so they are handed one slice now. This
// asserts the property, not the plumbing: nothing that is being attached may
// also appear as a removal.
func TestPreviewNeverCallsOneAttachmentBothKeptAndDetached(t *testing.T) {
	// A curated capability: an id and a version, and no published contract,
	// which is the shape resolveBuiltins produces.
	builtin := resolvedTool{
		Requirement: generate.Requirement{Name: "end_call", Source: "end_call", Where: "the agent lists it"},
		ToolID:      "t-end_call", Version: 3,
		finding: finding{State: satisfied},
	}
	live := liveWith(func(*slngLiveTool) {})
	live.ToolRefs = append(live.ToolRefs, slngLiveTool{
		AttachmentID: "att-2", ToolID: "t-end_call", Version: 3, Invocation: "model",
	})

	report := buildDeployReport("slng", "acme-support-slng", "agent-1", "update", true, "dry run",
		slngDeployment{
			Resolution: resolution{Tools: []resolvedTool{resolvedAt(2)}, Builtins: []resolvedTool{builtin}},
			Live:       live, LiveKnown: true,
			Previous: map[string]slngPublishedVersion{},
		}, preflightReport{})

	attaching := map[string]bool{}
	for _, row := range report.Tools {
		attaching[row.ToolID] = true
	}
	if !attaching["t-end_call"] {
		t.Fatal("the builtin is not in the report's tool rows, so this test proves nothing")
	}
	for _, removal := range report.Removals {
		if attaching[removal.Name] {
			t.Errorf("%q is reported as attached and as detached in one preview: %+v", removal.Name, removal)
		}
	}
}

// TestPreviewClaimsNoContractChangeWithoutBothContracts.
//
// The published-contract comparison reads the version the agent has now against
// the version being attached. A builtin resolves to an id and a version and no
// snapshot, because a curated capability has no published contract this deploy
// checks a binding against, so the comparison ran a real baseline against an
// empty proposed snapshot and reported that the description and the parameters
// had both changed. That is a fact about a field nobody read, stated as a fact
// about the tool.
//
// The absent baseline was already left out rather than guessed at. This holds
// the other side of it to the same rule.
func TestPreviewClaimsNoContractChangeWithoutBothContracts(t *testing.T) {
	builtin := resolvedTool{
		Requirement: generate.Requirement{Name: "end_call", Source: "end_call", Where: "the agent lists it"},
		ToolID:      "t-end_call", Version: 3,
		finding: finding{State: satisfied},
	}
	live := slngLiveAgent{ID: "agent-1", ToolRefs: []slngLiveTool{
		{AttachmentID: "att-2", ToolID: "t-end_call", Version: 3, Invocation: "model"},
	}}
	// A baseline that WAS read, which is what made the one-sided comparison
	// look like a difference.
	previous := map[string]slngPublishedVersion{
		snapshotKey("t-end_call", 3): {Snapshot: slngPublishedSnapshot{
			Name: "end_call", Description: "End the call.",
			ArgumentSchema: map[string]any{"type": "object"},
		}},
	}

	report := buildDeployReport("slng", "acme-support-slng", "agent-1", "update", true, "dry run",
		slngDeployment{
			Resolution: resolution{Builtins: []resolvedTool{builtin}},
			Live:       live, LiveKnown: true, Previous: previous,
		}, preflightReport{})

	if len(report.Tools) != 1 {
		t.Fatalf("expected the builtin's row, got %d rows", len(report.Tools))
	}
	row := report.Tools[0]
	if row.PublishedDescriptionChanged || row.PublishedParametersChanged {
		t.Errorf("the preview reports a contract change for a reference whose proposed contract was never read: %+v", row)
	}
}

// TestPreviewNamesWhatAnMCPReplacementWouldRemove.
//
// The MCP comparison reported the schema hash and nothing else, so a push that
// dropped a dashboard description and an argument override announced only that
// the server's tool had changed shape. A package's MCP selection carries a
// server and a tool name, so every other setting on the attachment is lost
// unconditionally and the preview has to say so.
func TestPreviewNamesWhatAnMCPReplacementWouldRemove(t *testing.T) {
	live := slngLiveAgent{ID: "agent-1", MCPRefs: []slngLiveMCPRef{{
		AttachmentID: "m-1", ServerID: "s-1", ToolName: "search_docs",
		SchemaHash:  "h-search",
		Description: "Written in the dashboard.",
		Arguments:   map[string]any{"limit": float64(5)},
		ExecutionPolicy: map[string]any{
			"pre_action_message": map[string]any{"enabled": true, "text": "Let me look."},
		},
	}}}
	rows := compareMCPAttachments(
		[]resolvedMCP{{
			Requirement: generate.Requirement{Name: "search_docs", Server: "docs"},
			ServerID:    "s-1", SchemaHash: "h-search",
		}},
		nil, nil, live, true,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	joined := strings.Join(rows[0].Changes, "\n")
	for _, want := range []string{"description", "argument_overrides.limit", "execution_policy"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the preview does not name %q, so it disappears in a push that reported nothing:\n%s", want, joined)
		}
	}
	// The hash is unchanged here, so nothing may claim the tool changed shape.
	if strings.Contains(joined, "changed shape") {
		t.Errorf("the preview claims a schema change on a matching hash:\n%s", joined)
	}
}
