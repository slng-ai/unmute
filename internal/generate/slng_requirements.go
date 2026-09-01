package generate

import (
	"maps"
	"slices"

	"github.com/slng-ai/unmute/internal/ir"
)

// What a compiled slng body needs the account to already have.
//
// One owner, for two readers. The emitted runbook prints these so an author
// standing in the build directory can see what to create, and `unmute deploy`
// checks them against the account before it writes anything. Those two must be
// the same set or the runbook is lying, and the only way to guarantee that
// without an agreement test over prose is to make them the same value.
//
// Nothing here is a value. Unmute has no secret to write and would refuse to
// write one; every field below is a name.

// Requirements is everything the account must already hold for this body to
// push. What the push *creates* is deliberately absent: a code or webhook tool
// does not exist until the push writes it, so reporting one as missing would be
// noise on every first deploy, and noise teaches authors to stop reading.
type Requirements struct {
	// Builtins are tool references the push resolves rather than creates. Each is
	// a curated capability the organisation already holds.
	Builtins []Requirement
	// MCPServers is one entry per distinct server named by an mcp: tool.
	MCPServers []Requirement
	// MCPTools is one entry per exposed server tool, with Server naming the
	// server that must offer it.
	MCPTools []Requirement
	// Secrets are credentials a tool authenticates with, stored in the vault
	// under the same name the package uses for the environment variable.
	Secrets []Requirement
	// Variables are {{$NAME}} tokens SLNG substitutes at run time. They are a
	// different vault kind from a secret, created differently, and an author
	// looking at a flat list cannot tell which is which.
	Variables []Requirement
}

// Requirement is one thing the account must hold.
type Requirement struct {
	// Name is exact and case-sensitive. Every one of the account's listings
	// matches on exact name, so a near miss is a miss.
	Name string
	// Server is set on MCPTools alone, naming the server that must offer Name.
	Server string
	// Where is the line of the package that asked for this. It is mandatory:
	// "create ACME_TOKEN" is only actionable beside "because check_order
	// authenticates with it", and a finding an author cannot trace is a finding
	// they cannot act on.
	Where string
}

// Empty reports whether the package needs nothing from the account at all. Such
// a package is told so in one line rather than shown five empty lists, because
// an empty list reads like a bug in the compiler.
func (r Requirements) Empty() bool { return r.Count() == 0 }

// Count is how many names this package needs the account to hold.
func (r Requirements) Count() int {
	return len(r.Builtins) + len(r.MCPServers) + len(r.MCPTools) + len(r.Secrets) + len(r.Variables)
}

// ServerNames is the distinct MCP servers, for a caller that has to ask each one
// what tools it offers. Sorted, so the reads happen in a stable order.
func (r Requirements) ServerNames() []string {
	names := make([]string, 0, len(r.MCPServers))
	for _, server := range r.MCPServers {
		names = append(names, server.Name)
	}
	return names
}

// slngRequirements derives what one compiled body needs.
//
// It takes the built artifacts rather than the package alone because two of the
// sources only exist after lowering: the vault tokens in the resolved system
// prompt and greeting, and the tool and MCP references the body actually
// carries. Deriving those from the package again would be a second owner of the
// same fact.
func slngRequirements(agent *ir.Agent, built slngArtifacts) Requirements {
	var requirements Requirements

	// Builtins, from the references the body carries.
	//
	// The name is the *package tool's* name, not the builtin id it selects, and
	// that is not a detail. slngTools writes {"tool": <package tool name>}, so a
	// package with tools/hang_up.yaml declaring `builtin: {id: end_call}` emits a
	// reference to "hang_up". Nothing refuses that at validate, the account has
	// no tool called "hang_up", and the push fails. The fix is to rename the
	// file, which is only sayable if the name checked is the one the ref carries.
	//
	// A control contributes nothing here. slngTools skips it entirely, because a
	// control reaches SLNG as a curated capability attached to the agent in the
	// dashboard and the body carries no reference to it. There is no name to
	// resolve, so there is nothing an account read could confirm or deny.
	for _, ref := range built.Body.ToolRefs {
		if agent.Tools[ref.Tool].Execution != ir.ToolBuiltin {
			continue
		}
		requirements.Builtins = append(requirements.Builtins, Requirement{
			Name:  ref.Tool,
			Where: "tools/" + ref.Tool + ".yaml is a builtin, so SLNG must already have a tool of that name",
		})
	}

	// MCP servers and their tools. The body carries one reference per exposed
	// tool, so the servers are the distinct half of that.
	seenServer := map[string]string{}
	for _, ref := range built.Body.MCPRefs {
		if seenServer[ref.Server] == "" {
			seenServer[ref.Server] = "tools/" + ref.Server + ".yaml names this MCP server"
		}
		requirements.MCPTools = append(requirements.MCPTools, Requirement{
			Name:   ref.Tool,
			Server: ref.Server,
			Where:  "tools/" + ref.Server + ".yaml exposes it under mcp.tools",
		})
	}
	for _, name := range slices.Sorted(maps.Keys(seenServer)) {
		requirements.MCPServers = append(requirements.MCPServers, Requirement{Name: name, Where: seenServer[name]})
	}

	requirements.Secrets, requirements.Variables = slngVaultRequirements(agent, built)
	return requirements
}

// slngVaultRequirements collects every SLNG Vault name the package needs,
// grouped by where it came from. Two kinds, because they are created differently
// and an author looking at a flat list cannot tell which is which:
//
//   - a secret is a credential a tool authenticates with, named in the package
//     as an environment variable and stored in the Vault under the same name;
//   - a variable is a {{$NAME}} token SLNG substitutes at run time.
//
// Only names. Unmute has no value to write and would refuse to write one.
func slngVaultRequirements(agent *ir.Agent, built slngArtifacts) (secrets, variables []Requirement) {
	seenSecret, seenVariable := map[string]string{}, map[string]string{}
	addSecret := func(name, where string) {
		if name != "" && seenSecret[name] == "" {
			seenSecret[name] = where
		}
	}
	addVariable := func(value, where string) {
		for _, ref := range ir.TemplateRefs(value) {
			if name, ok := ir.VaultToken(ref); ok && seenVariable[name] == "" {
				seenVariable[name] = where
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(agent.Tools)) {
		tool := agent.Tools[name]
		if tool.Auth != nil {
			addSecret(tool.Auth.TokenEnv, "tools/"+name+".yaml authenticates with it")
		}
		addVariable(tool.Description, "tools/"+name+".yaml description")
		addVariable(tool.BaseURL, "tools/"+name+".yaml webhook base_url")
		addVariable(tool.Path, "tools/"+name+".yaml webhook path")
		for _, key := range slices.Sorted(maps.Keys(tool.Inject)) {
			if text, ok := tool.Inject[key].(string); ok {
				addVariable(text, "tools/"+name+".yaml inject."+key)
			}
		}
	}
	addVariable(built.Body.SystemPrompt, "the entry agent's instructions")
	addVariable(built.Body.Greeting, "conversation.greeting.text")
	for _, name := range slices.Sorted(maps.Keys(seenSecret)) {
		secrets = append(secrets, Requirement{Name: name, Where: seenSecret[name]})
	}
	for _, name := range slices.Sorted(maps.Keys(seenVariable)) {
		variables = append(variables, Requirement{Name: name, Where: seenVariable[name]})
	}
	return secrets, variables
}
