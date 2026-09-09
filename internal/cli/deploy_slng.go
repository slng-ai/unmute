package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/style"
	"github.com/slng-ai/unmute/internal/target"
)

// One target's remote run, in order, with the write last.
//
// The order is the promise. Everything that reads happens before anything that
// writes, and the agent is changed only after every check it needs has an
// answer. A dry run stops at the preview, and it stops before the two paths
// that change remote state as a side effect of checking: the vault fill and the
// MCP capability refresh.
//
//	local validation → account and contract support → resolve → bindings and
//	vault → preview → guarded push → outcome
//
// Nothing here decides. resolveHosted, resolveMCP, checkBindings, compareVault
// and compareAttachments decide, and none of them runs a subprocess; this
// function is the order they run in and the writer they report through.

// slngDeployment is what one target's run established.
type slngDeployment struct {
	Account    slngAccount
	Resolution resolution
	Bindings   []bindingCheck
	// Proposed is what the package declares for each attachment, so the preview
	// can compare it against what the agent has now without walking the IR a
	// second time.
	Proposed proposedSettings
	// Live is the agent as it stands, and LiveKnown says whether reading it
	// worked. An unreadable agent is not an empty one: the second would produce
	// a preview claiming there is nothing to lose.
	Live      slngLiveAgent
	LiveKnown bool
	// Previous holds the published snapshots the live attachments point at, so
	// the preview can say what changed in the contract and not only in the
	// version number.
	Previous map[string]slngPublishedVersion
	// Refreshed names the servers this run probed, and SideEffects everything
	// it definitely changed. Both exist so a run that fails afterwards cannot
	// report that nothing happened.
	Refreshed   map[string]bool
	SideEffects []string
}

// deployResolution runs the read half: the account, the contract support probe,
// every resolution, and the checks derived from them.
//
// It writes nothing remote except under the two conditions its caller has
// already agreed to: the existing opt-in vault fill, and one MCP capability
// refresh per stale server on a real deploy. Both are recorded in SideEffects
// as they happen.
func deployResolution(
	cmd *cobra.Command, runner *voiceaiRunner, cache *resolveCache,
	name string, artifact generate.Artifact, agent *ir.Agent,
	env []string, dryRun bool,
) (slngDeployment, preflightReport, error) {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	deployment := slngDeployment{Refreshed: map[string]bool{}, Previous: map[string]slngPublishedVersion{}}

	resources, err := readResources(runner, artifact.Requires.ServerNames())
	if err != nil {
		return deployment, preflightReport{}, err
	}
	deployment.Account = resources.Account
	fmt.Fprintf(out, "%s: organisation %s\n", name, resources.Account)

	// The builtins and the package's own declared vault names go through the
	// existing name-based comparison. The hosted references and the MCP
	// selections do not: they are resolved to ids and versions below, and
	// letting both paths report on them would produce two findings per
	// reference saying different things.
	declared := artifact.Requires
	declared.Hosted, declared.MCPServers, declared.MCPTools = nil, nil, nil
	report := comparePreflight(declared, resources)

	deployment.Resolution.Tools = resolveHosted(runner, cache, artifact.Requires, resources)
	for _, reference := range deployment.Resolution.Tools {
		report.Findings = append(report.Findings, hostedDriftWarning(reference))
	}
	// Builtins carry an identity too, and no extra finding: comparePreflight
	// above already reported each by name, and a second row per builtin would be
	// counted twice in the satisfied tally.
	deployment.Resolution.Builtins = resolveBuiltins(artifact.Requires, resources)

	// Two references arriving at one hosted tool are refused here, which is
	// before anything is staged and before the push is asked to reconcile them.
	// It reads the builtins too, so a scope collision on one name is caught.
	report.Findings = append(report.Findings,
		duplicateHostedFindings(deployment.Resolution.Tools, deployment.Resolution.Builtins)...)

	records, selections, mcpFindings := resolveMCP(runner, cache, artifact.Requires, resources, !dryRun)
	deployment.Resolution.Servers, deployment.Resolution.MCP = records, selections
	report.Findings = append(report.Findings, mcpFindings...)
	for _, found := range mcpFindings {
		if strings.Contains(found.Detail, "was refreshed by connecting") {
			deployment.Refreshed[found.Requirement.Name] = true
			deployment.SideEffects = append(deployment.SideEffects,
				fmt.Sprintf("connected to MCP server %q, which refreshed the capability snapshot SLNG stores for it", found.Requirement.Name))
		}
	}

	// The vault entries a published contract reads, discovered from the account
	// rather than declared in the package. This is the half a mirror used to
	// supply, and it went stale the moment somebody changed the tool.
	var needs []vaultNeed
	for _, reference := range deployment.Resolution.Tools {
		if reference.usable() {
			needs = append(needs, publishedVaultNeeds(reference)...)
		}
	}
	for _, requirement := range artifact.Requires.MCPServers {
		if record, ok := records[requirement.Name]; ok {
			needs = append(needs, mcpVaultNeeds(record, requirementFile(requirement))...)
		}
	}
	// These are the DISCOVERED needs, and compareVault marks them required for
	// that reason: an unreadable listing blocks here rather than warning.
	//
	// The distinction is the push's own vault check, which reads
	// `requiredSecretNames`. That is the `{{$NAME}}` tokens in the package's
	// prompts plus the credential names on tool bodies physically present in
	// the package. A name discovered from a published contract or from a hosted
	// MCP server's credentials is in neither, so it never reaches that check
	// and nothing downstream would catch it.
	//
	// This is a correction. It used to warn, on the belief that the push
	// re-checks every vault entry itself, and the push re-checks the package's
	// own declared names against a fresh read and no more than that. A
	// discovered name is the same case as a published version or an MCP schema
	// hash: the guarded push attaches what this run resolved and verifies none
	// of it, so a read that did not happen here happens nowhere.
	//
	// And what neither run establishes, either way, is whether an entry holds a
	// value: the push compares a name and a kind and never reads `has_value`.
	report.Findings = append(report.Findings, compareDiscoveredVault(dedupeNeeds(needs), resources)...)

	// The arguments the package supplies, against the contract that was
	// actually resolved.
	deployment.Proposed = proposedSettings{
		Arguments:   generate.SlngInjectedArguments(agent),
		Description: generate.SlngAuthoredDescriptions(agent),
		Announce:    generate.SlngAuthoredAnnouncements(agent),
		Config:      generate.SlngAuthoredConfig(agent),
	}
	deployment.Bindings = checkBindings(deployment.Resolution.Tools, deployment.Proposed.Arguments)
	report.Findings = append(report.Findings, bindingFindings(deployment.Bindings)...)

	// The vault fill is a write, so it belongs to a real run only. A dry run
	// bypasses the path entirely rather than declining inside it, because the
	// promise is that a preview creates nothing and "it asked and I said no" is
	// not that promise.
	if !dryRun {
		in := cmd.InOrStdin()
		before := countSatisfied(report)
		offerToFill(in, out, errOut, runner, &report, env, interactiveTerminal(in))
		if created := countSatisfied(report) - before; created > 0 {
			deployment.SideEffects = append(deployment.SideEffects,
				fmt.Sprintf("created %s in the SLNG vault", plural(created, "entry")))
		}
	}

	return deployment, report, nil
}

// countSatisfied is how offerToFill's effect is measured: it marks a finding
// satisfied when it created the entry, so the difference is what was written.
func countSatisfied(report preflightReport) int {
	count := 0
	for _, found := range report.Findings {
		if found.State == satisfied {
			count++
		}
	}
	return count
}

// checkResolutionContract reads the support marker off a guarded dry run.
//
// Reading the marker is the whole check, and a decode that merely succeeded is
// not evidence of anything: 0.1.16's own dry-run document decodes fine and means
// something else entirely, so a decode-succeeded test would report support that
// is not there and the run would go on to attach an unchecked version.
//
// The dry run this reads is of the real staged body rather than of a synthetic
// probe. That costs nothing extra, because a real deploy has to make that dry
// run anyway to learn which agent it would replace, and it means the answer
// covers the actual references rather than an empty document.
func checkResolutionContract(planned pushResult) error {
	switch marker := planned.ResolutionContract; marker {
	case target.SlngResolutionContractVersion:
		return nil
	case 0:
		return errors.New(target.SlngUpgradeGuidance)
	default:
		return fmt.Errorf("the installed `%s` answers `%s: %d` and this run needs %d: upgrade it with `%s`",
			deployPushBinary, target.SlngResolutionContract, marker,
			target.SlngResolutionContractVersion, target.SlngPushInstall)
	}
}

// stageResolvedBody writes the temporary resolved copy and returns its
// directory. The caller deletes it on every exit.
func stageResolvedBody(agent *ir.Agent, resolved ir.Target, deployment slngDeployment, outDir string) (string, error) {
	references := append(append([]resolvedTool(nil), deployment.Resolution.Tools...), deployment.Resolution.Builtins...)
	tools := make([]generate.SlngResolvedTool, 0, len(references))
	for _, reference := range references {
		tools = append(tools, generate.SlngResolvedTool{
			Source:  reference.Requirement.Source,
			ToolID:  reference.ToolID,
			Version: reference.Version,
		})
	}
	mcp := make([]generate.SlngResolvedMCP, 0, len(deployment.Resolution.MCP))
	for _, selection := range deployment.Resolution.MCP {
		mcp = append(mcp, generate.SlngResolvedMCP{
			Server: selection.Requirement.Server, Tool: selection.Requirement.Name,
			ServerID: selection.ServerID, SchemaHash: selection.SchemaHash,
		})
	}
	body, err := generate.SlngResolvedBody(agent, resolved, tools, mcp)
	if err != nil {
		return "", err
	}
	staged, err := os.MkdirTemp("", "unmute-slng-resolved-")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(staged, "agent.json"), body, 0o600); err != nil {
		return "", errors.Join(err, os.RemoveAll(staged))
	}
	// The samples the push may be asked to run live beside the ordinary build,
	// and the staged directory is a different one, so they are linked across.
	// Copied rather than moved: the build directory is the author's and a deploy
	// does not empty it.
	if err := copySamples(outDir, staged); err != nil {
		return "", errors.Join(err, os.RemoveAll(staged))
	}
	return staged, nil
}

// copySamples carries `samples/*.json` into the staged directory, because the
// push reads them from beside the body it was given.
func copySamples(from, to string) error {
	entries, err := os.ReadDir(filepath.Join(from, "samples"))
	if errors.Is(err, fs.ErrNotExist) {
		// No samples at all is the ordinary case: unmute emits none, and they
		// are written by an operator who wants `--run-samples` to have
		// something to run.
		return nil
	}
	if err != nil {
		return err
	}
	target := filepath.Join(to, "samples")
	if err := os.MkdirAll(target, 0o700); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(from, "samples", entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(target, entry.Name()), content, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// readBaseline reads the agent a push would replace, plus the published
// snapshots its attachments point at, so the preview can name a contract change
// and not only a version number.
//
// A failed read leaves LiveKnown false rather than producing an empty agent.
// The distinction is the difference between a preview that says "these five
// things will be detached" and one that says nothing at all.
func readBaseline(runner *voiceaiRunner, cache *resolveCache, deployment *slngDeployment, agentID string) {
	if agentID == "" {
		// No existing agent: this is a create, and there is no baseline to read
		// rather than an unreadable one.
		deployment.LiveKnown = true
		return
	}
	live, err := readLiveAgent(runner, cache, agentID)
	if err != nil {
		deployment.LiveKnown = false
		return
	}
	deployment.Live, deployment.LiveKnown = live, true
	for _, attached := range live.ToolRefs {
		if attached.ToolID == "" || attached.Version < 1 {
			continue
		}
		snapshot, err := readPublishedVersion(runner, cache, attached.ToolID, attached.Version)
		if err != nil {
			// Optional: it only sharpens the preview. Without it the version
			// change is still named, and the contract comparison is left out
			// rather than guessed at.
			continue
		}
		deployment.Previous[snapshotKey(attached.ToolID, attached.Version)] = snapshot
	}
}

// printResolvedPlan is the dry run's stdout: what would be attached, and what a
// replacement would change. Every line names something the reader has to decide
// about, which is the rule the rest of this CLI's output follows.
func printResolvedPlan(out io.Writer, name string, report deployReport) {
	u := style.For(out)
	for _, row := range report.Tools {
		version := fmt.Sprintf("v%d", row.ProposedVersion)
		if row.PreviousVersion != nil && *row.PreviousVersion != row.ProposedVersion {
			version = fmt.Sprintf("v%d, from v%d", row.ProposedVersion, *row.PreviousVersion)
		}
		label := row.Tool
		if row.Source != row.Tool {
			label = fmt.Sprintf("%s (%s)", row.Tool, row.Source)
		}
		if row.New {
			version += ", new"
		}
		fmt.Fprintf(out, "%s: %s %s\n", name, label, u.Dim(version))
		for _, change := range row.Changes {
			fmt.Fprintf(out, "%s:   would change %s\n", name, change)
		}
		if row.PublishedParametersChanged {
			fmt.Fprintf(out, "%s:   the published parameters differ between those two versions, which says the contract changed and nothing about how the tool behaves\n", name)
		}
		if row.PublishedDescriptionChanged {
			fmt.Fprintf(out, "%s:   the published description differs between those two versions\n", name)
		}
	}
	for _, row := range report.MCP {
		fmt.Fprintf(out, "%s: %s %s\n", name, row.Server+" "+row.Tool, u.Dim("checked snapshot"))
		for _, change := range row.Changes {
			fmt.Fprintf(out, "%s:   would change %s\n", name, change)
		}
	}
	for _, removal := range report.Removals {
		detail := ""
		if len(removal.Settings) > 0 {
			detail = " (" + strings.Join(removal.Settings, ", ") + ")"
		}
		fmt.Fprintf(out, "%s: would detach %s %s%s, which this package does not name\n", name, removal.Kind, removal.Name, detail)
	}
	if report.PreviousStateUnknown {
		fmt.Fprintf(out, "%s: the agent as it stands could not be read, so nothing above says what a replacement would remove\n", name)
	}
}

// deferredLines are the checks a run is not claiming to have made, for stderr.
// They are facts about the run rather than problems, so they go out once per
// run and are not repeated per reference.
func deferredLines(deployment slngDeployment) []string {
	lines := append([]string(nil), deferredBindings(deployment.Bindings)...)
	slices.Sort(lines)
	return lines
}
