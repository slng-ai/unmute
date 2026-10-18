package generate

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/slng-ai/unmute/internal/ir"
)

// slngCodeTool is the tool body `voiceai agents push` and `voiceai tool create`
// write for a code tool (voiceai 0.1.23, cli/src/commands/tool.ts create): the
// Python source SLNG builds, introspects, runs and publishes.
type slngCodeTool struct {
	Name           string             `json:"name"`
	ToolType       string             `json:"tool_type"`
	Description    string             `json:"description"`
	CodeSrc        string             `json:"code_src"`
	Config         slngCodeToolConfig `json:"config"`
	DeclaredSecret []string           `json:"declared_secrets"`
	Dependencies   []string           `json:"dependencies"`
}

type slngCodeToolConfig struct {
	Type         string         `json:"type"`
	ImportProbes []string       `json:"import_probes"`
	Egress       map[string]any `json:"egress"`
}

// stateImport is a handler's import of its package's state.py. SLNG runs one
// file, so the state classes are written above the handler and the import goes.
var stateImport = regexp.MustCompile(`(?m)^from state import [^\n]*\n|^import state\n`)

// slngCodeToolNames are the names SLNG reads from a code tool's file. A handler
// module that defines one of them would be shadowed by the adapter.
var slngCodeToolNames = []string{"Input", "Output", "handler"}

// slngCodeToolFiles writes one code tool body and its sample per
// local tool: build/slng/tools/<name>.json and build/slng/samples/<name>.json,
// where `voiceai agents push` reads them.
func slngCodeToolFiles(agent *ir.Agent) ([]File, error) {
	var files []File
	for _, name := range slices.Sorted(maps.Keys(agent.Tools)) {
		tool := agent.Tools[name]
		if tool.Execution != ir.ToolLocal {
			continue
		}
		source, err := slngCodeSource(agent, tool)
		if err != nil {
			return nil, fmt.Errorf("tool %q: %w", name, err)
		}
		body, err := marshalSlng(slngCodeTool{
			Name: name, ToolType: "code", Description: tool.Description, CodeSrc: source,
			Config:         slngCodeToolConfig{Type: "code", ImportProbes: []string{}, Egress: map[string]any{}},
			DeclaredSecret: []string{}, Dependencies: append([]string{}, tool.Dependencies...),
		})
		if err != nil {
			return nil, fmt.Errorf("tool %q: %w", name, err)
		}
		files = append(files, File{Path: "tools/" + name + ".json", Content: body})
		if len(tool.Sample) > 0 {
			files = append(files, File{Path: "samples/" + name + ".json", Content: tool.Sample})
		}
	}
	return files, nil
}

// slngCodeSource is the one Python file SLNG runs for a typed handler: the
// package's state.py when the handler imports it, the handler module, and an
// adapter giving SLNG the three names it reads. Input is built from the
// handler's signature and Output is its return model, so the tool's types are
// the handler's types on every target.
func slngCodeSource(agent *ir.Agent, tool ir.Tool) (string, error) {
	function := tool.HandlerFunction
	module := tool.HandlerSource
	for _, reserved := range slngCodeToolNames {
		if regexp.MustCompile(`(?m)^(def|class) ` + reserved + `\b|^` + reserved + ` *=`).MatchString(module) {
			return "", fmt.Errorf("%s defines %s, which SLNG reads from a code tool: rename it", tool.Handler, reserved)
		}
	}
	var out strings.Builder
	if stateImport.MatchString(module) {
		out.WriteString("# state.py\n")
		out.WriteString(agent.StateSource)
		out.WriteString("\n\n# " + tool.Handler + "\n")
		module = stateImport.ReplaceAllString(module, "")
	}
	out.WriteString(module)
	fmt.Fprintf(&out, `

# What SLNG reads from a code tool: Input, Output and handler(input) -> Output.
# Written by unmute from the handler's own signature.
import inspect as _inspect  # noqa: E402
import typing as _typing  # noqa: E402

from pydantic import create_model as _create_model  # noqa: E402

_hints = _typing.get_type_hints(%[1]s, include_extras=True)
_parameters = _inspect.signature(%[1]s).parameters
Input = _create_model(
    "Input",
    **{
        name: (_hints[name], ... if parameter.default is parameter.empty else parameter.default)
        for name, parameter in _parameters.items()
    },
)
Output = _hints["return"]


def handler(input: Input) -> Output:
    return %[1]s(**{name: getattr(input, name) for name in _parameters})
`, function)
	return out.String(), nil
}
