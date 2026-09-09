package generate

import (
	"fmt"

	"github.com/slng-ai/unmute/internal/ir"
)

// The temporary body a guarded push consumes.
//
// Ordinary compiled output is name-based and account-independent, and it has to
// stay that way: `build/<target>/agent.json` is checked into nothing, compiled
// with no credential, and identical whichever organisation an author happens to
// be logged into. An id in it would make the file mean something different on
// two machines.
//
// But `unmute deploy` promises to attach the version it checked, and it cannot
// keep that promise through a push that resolves names again: the push would
// float every reference to whatever is newest, which by then may be a version
// nothing validated. So deployment stages a second copy carrying the ids and
// versions it resolved, hands that to a push running in the guarded mode, and
// deletes it on the way out.
//
// This lives in the generator rather than in internal/cli deliberately. The
// body's shape has one owner, and a second JSON model over in the CLI would be
// a second definition of the same document, free to drift from the one that is
// actually tested against the platform's contract fixtures.

// SlngResolvedTool is one checked tool reference: which package file it came
// from, and the exact identity to attach.
type SlngResolvedTool struct {
	// Source is the package tool file's own name, which is how a resolution is
	// matched back to the reference it belongs to. The hosted name is not usable
	// as a key: two files may alias the same hosted tool, and the whole reason
	// this feature keeps both names is that they are allowed to differ.
	Source  string
	ToolID  string
	Version int
}

// SlngResolvedMCP is one checked MCP selection.
type SlngResolvedMCP struct {
	// Source is the package tool file, and Server and Tool identify the
	// selection within it, because one file selects several tools from one
	// server.
	Source     string
	Server     string
	Tool       string
	ServerID   string
	SchemaHash string
}

// SlngResolvedBody renders the staged body.
//
// It refuses rather than emits a partial document. A reference with no
// resolution would reach the push unresolved, and an unresolved reference in
// guarded mode is a refusal there too, so refusing here names the tool and the
// file instead of relaying a message about a body.
func SlngResolvedBody(agent *ir.Agent, tgt ir.Target, tools []SlngResolvedTool, mcp []SlngResolvedMCP) ([]byte, error) {
	built, err := buildSlng(agent, tgt)
	if err != nil {
		return nil, err
	}

	byTool := make(map[string]SlngResolvedTool, len(tools))
	for _, resolved := range tools {
		byTool[resolved.Source] = resolved
	}
	byMCP := make(map[string]SlngResolvedMCP, len(mcp))
	for _, resolved := range mcp {
		byMCP[resolved.Server+"\x00"+resolved.Tool] = resolved
	}

	for i := range built.Body.ToolRefs {
		ref := &built.Body.ToolRefs[i]
		resolved, ok := byTool[ref.origin]
		if !ok {
			return nil, fmt.Errorf("tool %q was not resolved to a published version, so a guarded push has nothing to attach: this is a bug, because deployment refuses before staging when a reference is unresolved", ref.origin)
		}
		if resolved.ToolID == "" || resolved.Version < 1 {
			return nil, fmt.Errorf("tool %q resolved to id %q version %d, which is not an attachable identity",
				ref.origin, resolved.ToolID, resolved.Version)
		}
		ref.ToolID, ref.Version = resolved.ToolID, resolved.Version
	}
	for i := range built.Body.MCPRefs {
		ref := &built.Body.MCPRefs[i]
		resolved, ok := byMCP[ref.Server+"\x00"+ref.Tool]
		if !ok {
			return nil, fmt.Errorf("tool %q selects %q from MCP server %q and that selection was not checked, so a guarded push has nothing to attach",
				ref.origin, ref.Tool, ref.Server)
		}
		if resolved.ServerID == "" || resolved.SchemaHash == "" {
			return nil, fmt.Errorf("MCP server %q resolved to id %q with schema hash %q for tool %q, which is not an attachable identity",
				ref.Server, resolved.ServerID, resolved.SchemaHash, ref.Tool)
		}
		ref.ServerID, ref.SchemaHash = resolved.ServerID, resolved.SchemaHash
	}
	return marshalSlng(built.Body)
}

// SlngInjectedArguments is each package tool's authored `inject:` map, keyed by
// the tool file's own name.
//
// Read off the IR rather than off the emitted body, so a deployment refusal
// names the value the author wrote and the file it is in, rather than a
// generated artifact they did not.
func SlngInjectedArguments(agent *ir.Agent) map[string]map[string]any {
	out := map[string]map[string]any{}
	for name, tool := range agent.Tools {
		if len(tool.Inject) == 0 {
			continue
		}
		arguments := make(map[string]any, len(tool.Inject))
		for key, value := range tool.Inject {
			arguments[key] = value
		}
		out[name] = arguments
	}
	return out
}

// SlngAuthoredAnnouncements is each package tool file's authored `announce:`,
// keyed by the tool file's own name.
//
// Every tool, not just a hosted one: `announce:` compiles to the attachment's
// execution_policy.pre_action_message on whatever kind of reference carries it.
//
// This exists because a preview cannot compare an announcement it was never
// given. Without it the deploy report read the agent's live policy, had nothing
// to compare it against, and reported every one of them as a setting "this
// package cannot declare, so a replacement removes it" — including a sentence
// the package itself was about to write back word for word.
func SlngAuthoredAnnouncements(agent *ir.Agent) map[string]string {
	out := map[string]string{}
	for name, tool := range agent.Tools {
		if tool.Announce != "" {
			out[name] = tool.Announce
		}
	}
	return out
}

// SlngAuthoredDescriptions is each package tool file's authored `description:`,
// keyed by the tool file's own name, and only where the author wrote one.
//
// Hosted tools emit only authored overrides; builtins emit their description
// directly. Match both branches of slngTools so the preview compares the same
// values the deployment writes, without taking descriptions from old mirrors.
func SlngAuthoredDescriptions(agent *ir.Agent) map[string]string {
	out := map[string]string{}
	for name, tool := range agent.Tools {
		if tool.Execution == ir.ToolBuiltin || (tool.Execution == ir.ToolSlngHosted && tool.DescriptionAuthored) {
			out[name] = tool.Description
		}
	}
	return out
}
