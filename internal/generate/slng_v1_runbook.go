package generate

import (
	"fmt"
	"maps"
	"slices"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// What the runbook prints. It is a value rather than the artifacts themselves so
// the template has no way to reach into the body and restate a field: whatever a
// reader needs is named here on purpose.
type slngRunbook struct {
	Target string
	// AgentName is the name the body claims, taken from the package rather than
	// the target instance. The runbook prints it because it is the one thing here
	// that reaches outside this directory: SLNG names are unique per
	// organisation, and a push under a name someone else's package already uses
	// replaces their agent rather than adding one.
	AgentName string
	Region    string
	// CredentialEnv is the push tool's key, and it is not the Context Router's
	// key. Naming the wrong one costs an afternoon, so the runbook names this one
	// and says which it is not.
	CredentialEnv string
	RouterKeyEnv  string
	LoginCommand  string
	// ListCommand is how an author checks a name is free before the first push.
	ListCommand string
	// WebSessionCommand takes the agent id the create step returned. Leaving it
	// off is an error, not a shorter spelling, so the runbook prints the real one.
	WebSessionCommand string
	// VaultSecrets and VaultVariables are names only. Unmute can list what a
	// package needs and can create none of them, and it never sees a value.
	VaultSecrets   []Requirement
	VaultVariables []Requirement
	NameShape      string
	// HostedRefs and MCPRefs drive the two "this body is not postable as
	// written" sections. A package with neither gets neither section.
	//
	// HostedRefs replaced ToolFiles, which listed the tool bodies the push would
	// create. It creates none: each entry here is a tool the organisation must
	// already hold, with the version the committed mirror was taken from, so the
	// section says what to check rather than what will be made.
	HostedRefs []string
	MCPRefs    []string
	Builtins   []string
	// ToolCount is every tool reference in the body. A builtin and a hosted tool
	// both need their tool_id resolved by the push.
	ToolCount int
	// ToolRefNames are the tool references in the body, in the order they appear
	// there, so the worked example in the runbook can name a real one from this
	// package rather than an invented tool.
	ToolRefNames []string
	// VariableNames is what a session's `arguments` may set. They are the
	// package's own declared variables, which is the connection an author most
	// often misses: a variable declared in agent.yaml is a value supplied per
	// call, not a constant.
	VariableNames []string
}

// NeedsVault reports whether the package needs any Vault entry at all. A package
// that needs none is told so, rather than shown an empty list, because an empty
// list reads like a bug in the compiler.
func (r slngRunbook) NeedsVault() bool { return len(r.VaultSecrets)+len(r.VaultVariables) > 0 }

// NeedsPushResolution reports whether the emitted body carries a name where the
// API wants an identifier.
//
// It counts *every* reference, including a builtin, and that correction came
// from reading the schema rather than from a live push. ToolAttachment
// (shared_tool_contract.py:481) is extra: forbid and requires attachment_id,
// tool_id and version, all three. A builtin reference is written as
// {"tool": "end_call", ...}, so it fails twice over: `tool` is not a field, and
// the three required ones are absent. McpAttachment is the same shape.
//
// This runbook used to tell a builtins-only author their body was postable as
// written. It is not. The only body that posts unchanged is one with no
// references at all, which is what ToolCount == 0 means below.
func (r slngRunbook) NeedsPushResolution() bool { return r.ToolCount+len(r.MCPRefs) > 0 }

func slngRunbookFor(agent *ir.Agent, tgt ir.Target, built slngArtifacts) slngRunbook {
	runbook := slngRunbook{
		Target:            tgt.Name,
		AgentName:         built.Body.Name,
		Region:            built.Body.Region,
		CredentialEnv:     targetcap.SlngPushCredentialEnv,
		RouterKeyEnv:      targetcap.SlngRouterKeyEnv,
		LoginCommand:      targetcap.SlngLoginCommand,
		ListCommand:       targetcap.SlngListCommand,
		WebSessionCommand: targetcap.SlngWebSessionCommand,
		NameShape:         targetcap.SlngVaultNamePattern,
	}
	for _, ref := range built.Body.MCPRefs {
		runbook.MCPRefs = append(runbook.MCPRefs, ref.Server+" · "+ref.Tool)
	}
	// Derived from Requires rather than from the body, because Requires is what
	// the deploy preflight checks: the runbook printing a different list from
	// the one the deploy reads is the failure the vault table's own agreement
	// gate exists to prevent, and this list has the same two readers.
	for _, hosted := range built.Requires.Hosted {
		runbook.HostedRefs = append(runbook.HostedRefs,
			fmt.Sprintf("`%s`, mirrored from version %d", hosted.Name, hosted.Version))
	}
	runbook.ToolCount = len(built.Body.ToolRefs)
	for _, ref := range built.Body.ToolRefs {
		runbook.ToolRefNames = append(runbook.ToolRefNames, ref.Tool)
		if agent.Tools[ref.Tool].Execution == ir.ToolBuiltin {
			runbook.Builtins = append(runbook.Builtins, ref.Tool)
		}
	}
	runbook.VariableNames = slices.Sorted(maps.Keys(built.Body.Variables))
	// The same value `unmute deploy` checks against the account. The runbook
	// prints what the preflight looks for, so a table here that omitted a name
	// would be a table that lies about what the push needs.
	runbook.VaultSecrets = built.Requires.Secrets
	runbook.VaultVariables = built.Requires.Variables
	return runbook
}

// DeployCommand is the one command the runbook tells an author to run. It
// validates, compiles and pushes, so a reader standing in the package needs
// nothing else.
//
// It carries no credential at all, not even an expansion of one. The key is
// exported in the line above it in the runbook, so the command that ends up in a
// shell history, a CI log or a screenshot has nothing in it worth reading.
func (r slngRunbook) DeployCommand() string {
	return fmt.Sprintf(targetcap.SlngDeployCommand, ".")
}

// PushCommand is what DeployCommand runs underneath, named so an author who
// would rather drive the push tool directly can. It takes the compiled directory
// this file sits in.
func (r slngRunbook) PushCommand() string {
	return fmt.Sprintf(targetcap.SlngPushPackageCommand, "build/"+r.Target)
}
