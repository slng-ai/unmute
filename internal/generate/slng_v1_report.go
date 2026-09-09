package generate

import (
	"encoding/json"
	"slices"
)

// The slng target's compile report.
//
// It exists for one sentence the runbook cannot say honestly: which checks this
// compile did not make. A hosted reference names a tool somebody else published,
// so an offline compile can check the reference's shape and nothing about the
// tool: not that the organisation has it, not that an injected argument fits its
// parameters, not that its credentials are in the vault. Those are deployment's,
// and an author who cannot see the difference between "checked" and "not checked
// yet" reads a clean compile as a promise it never made.
//
// It is a report and not a warning, because a deferred check fires on every
// correct package. A line on stdout has to name something the reader has to do,
// and "deployment will check this" is not that. The compile report is next to
// the body on disk, which is where a reader who wants the detail looks.

type slngReportJSON struct {
	Target   string `json:"target"`
	Provider string `json:"provider"`
	// Region is forwarded as declared, so it must be readable back.
	Region string   `json:"region"`
	Agent  string   `json:"agent"`
	Files  []string `json:"files"`
	// ToolRefs pairs each emitted reference with the package file it came from.
	// The two names differ whenever a `slng:` scalar names a tool the file is not
	// called after, and a reader tracing a deployment finding back to a file
	// needs both.
	ToolRefs []slngReportRef `json:"tool_refs"`
	MCPRefs  []slngReportMCP `json:"mcp_refs,omitempty"`
	// Requires is what the account must already hold: the same value the runbook
	// prints and `unmute deploy` checks, so the three cannot disagree.
	Requires slngReportRequires `json:"requires"`
	// Deferred names each check this compile could not make and who makes it.
	Deferred []slngReportDeferred `json:"deferred_checks,omitempty"`
	Notes    []string             `json:"notes,omitempty"`
	Warnings []string             `json:"warnings,omitempty"`
}

type slngReportRef struct {
	// Source is the package tool file's own name, which is what an agent's
	// `tools:` list and every diagnostic uses.
	Source string `json:"source"`
	// Tool is the name written into the body, which is the hosted one for a
	// `slng:` reference.
	Tool   string `json:"tool"`
	Hosted bool   `json:"hosted,omitempty"`
	// Description is present only when the package authored one, because an
	// attachment description is an override and an inherited one is not written.
	Description string `json:"description,omitempty"`
	// Arguments are the `inject:` values, which the model never sees. Names and
	// authored values only: a template is still a template here, resolved when a
	// call starts.
	Arguments map[string]any `json:"argument_overrides,omitempty"`
	Announce  string         `json:"announce,omitempty"`
}

type slngReportMCP struct {
	Server string `json:"server"`
	Tool   string `json:"tool_name"`
}

type slngReportRequires struct {
	Builtins   []Requirement `json:"builtins,omitempty"`
	Hosted     []Requirement `json:"hosted,omitempty"`
	MCPServers []Requirement `json:"mcp_servers,omitempty"`
	MCPTools   []Requirement `json:"mcp_tools,omitempty"`
	Secrets    []Requirement `json:"secrets,omitempty"`
	Variables  []Requirement `json:"variables,omitempty"`
}

type slngReportDeferred struct {
	// Check is what was not established, in the words the author would use.
	Check string `json:"check"`
	// By is the command that establishes it.
	By string `json:"by"`
	// Refs are the references it applies to, so a package with one hosted tool
	// and eight builtins does not read as if all nine were unchecked.
	Refs []string `json:"refs,omitempty"`
}

// slngReport renders the report beside the body. Files lists what was written
// including the report itself, the way both code targets' reports do.
func slngReport(built slngArtifacts, files []File) ([]byte, error) {
	generated := make([]string, 0, len(files)+1)
	for _, file := range files {
		generated = append(generated, file.Path)
	}
	generated = append(generated, "compile-report.json")
	slices.Sort(generated)

	refs := make([]slngReportRef, 0, len(built.Body.ToolRefs))
	for _, ref := range built.Body.ToolRefs {
		row := slngReportRef{
			Source: ref.origin, Tool: ref.Tool, Hosted: ref.hosted,
			Description: ref.Description, Arguments: ref.Arguments,
		}
		if ref.Policy != nil && ref.Policy.PreActionMessage != nil {
			for _, segment := range ref.Policy.PreActionMessage.Text.Segments {
				row.Announce = segment.Value
			}
		}
		refs = append(refs, row)
	}
	mcp := make([]slngReportMCP, 0, len(built.Body.MCPRefs))
	for _, ref := range built.Body.MCPRefs {
		mcp = append(mcp, slngReportMCP{Server: ref.Server, Tool: ref.Tool})
	}

	out, err := json.MarshalIndent(slngReportJSON{
		Target: built.Runbook.Target, Provider: "slng", Region: built.Body.Region,
		Agent: built.Body.Name, Files: generated,
		ToolRefs: refs, MCPRefs: mcp,
		Requires: slngReportRequires{
			Builtins: built.Requires.Builtins, Hosted: built.Requires.Hosted,
			MCPServers: built.Requires.MCPServers, MCPTools: built.Requires.MCPTools,
			Secrets: built.Requires.Secrets, Variables: built.Requires.Variables,
		},
		Deferred: slngDeferredChecks(built),
		Notes:    built.Notes, Warnings: built.Warnings,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// slngDeferredChecks is what this compile did not establish, and it is derived
// from what the package actually references rather than listed unconditionally:
// a package with no hosted tool defers no argument contract, and one with no MCP
// server defers no capability refresh.
func slngDeferredChecks(built slngArtifacts) []slngReportDeferred {
	var deferred []slngReportDeferred
	names := func(requirements []Requirement) []string {
		out := make([]string, 0, len(requirements))
		for _, requirement := range requirements {
			out = append(out, requirement.Name)
		}
		return out
	}

	if hosted := names(built.Requires.Hosted); len(hosted) > 0 {
		deferred = append(deferred,
			slngReportDeferred{
				Check: "that the organisation publishes a tool of this name, and which published version is the latest",
				By:    "unmute deploy", Refs: hosted,
			},
			slngReportDeferred{
				Check: "that each injected argument is a parameter of the published version, and that a fixed value is of the type it declares",
				By:    "unmute deploy", Refs: hosted,
			},
			slngReportDeferred{
				Check: "which vault entries the published version needs, which are read from the organisation rather than declared here",
				By:    "unmute deploy", Refs: hosted,
			})
	}
	if builtins := names(built.Requires.Builtins); len(builtins) > 0 {
		deferred = append(deferred, slngReportDeferred{
			Check: "that the organisation holds a curated capability of this name",
			By:    "unmute deploy", Refs: builtins,
		})
	}
	if servers := names(built.Requires.MCPServers); len(servers) > 0 {
		deferred = append(deferred, slngReportDeferred{
			Check: "that the server is registered, that its stored capability snapshot is usable, and that it offers every selected tool",
			By:    "unmute deploy", Refs: servers,
		})
	}
	return deferred
}
