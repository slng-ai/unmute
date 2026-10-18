package scaffold

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/stateschema"
)

// HandlerRef is a local tool's handler in the file:function form,
// `tools/<name>.py:<name>`. A handler naming a file alone gets the tool's name
// as its function.
func (t Tool) HandlerRef() string {
	file, function := spec.SplitHandler(t.Handler)
	if file == "" {
		file = "tools/" + t.Name + ".py"
	}
	if function == "" {
		function = t.Name
	}
	return file + ":" + function
}

// stubParam is one parameter of a handler stub, read from the console's input
// schema.
type stubParam struct {
	name, pyType, description string
	required                  bool
	schema                    map[string]any
}

// stubParams reads the console's input schema: required parameters first, each
// group in name order, the order Python needs.
func (t Tool) stubParams() []stubParam {
	var schema struct {
		Properties map[string]map[string]any `json:"properties"`
		Required   []string                  `json:"required"`
	}
	_ = json.Unmarshal([]byte(t.Input), &schema) // the console validated it; an unreadable one is no parameters
	var params []stubParam
	for name, property := range schema.Properties {
		kind, _ := property["type"].(string)
		description, _ := property["description"].(string)
		params = append(params, stubParam{
			name: name, pyType: cmp.Or(stubTypes[kind], "str"), description: description,
			required: slices.Contains(schema.Required, name), schema: property,
		})
	}
	slices.SortFunc(params, func(a, b stubParam) int {
		if a.required != b.required {
			if a.required {
				return -1
			}
			return 1
		}
		return strings.Compare(a.name, b.name)
	})
	return params
}

// stubTypes are the JSON Schema types the console writes, as Python spells them.
var stubTypes = map[string]string{
	"string": "str", "number": "float", "integer": "int", "boolean": "bool", "array": "list", "object": "dict",
}

// resultClass is the stub's output model.
func (t Tool) resultClass() string {
	var b strings.Builder
	for part := range strings.SplitSeq(t.Name, "_") {
		if part != "" {
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return b.String() + "Result"
}

// handlerStub is the handler file the console writes for a new local tool: a
// typed function whose signature is the tool's input and whose return type is
// its output, as agent.yaml reads them. The author replaces its body.
func (t Tool) handlerStub() string {
	params := t.stubParams()
	described := slices.ContainsFunc(params, func(p stubParam) bool { return p.description != "" })
	var b strings.Builder
	fmt.Fprintf(&b, "\"\"\"The %s tool. Its signature is its input, its return type its output.\"\"\"\n\n", t.Name)
	if described {
		b.WriteString("from typing import Annotated\n\nfrom pydantic import BaseModel, Field\n\n\n")
	} else {
		b.WriteString("from pydantic import BaseModel\n\n\n")
	}
	fmt.Fprintf(&b, "class %s(BaseModel):\n    \"\"\"What %s returns.\"\"\"\n\n    result: str = \"\"\n\n\n", t.resultClass(), t.Name)
	args := make([]string, 0, len(params))
	for _, p := range params {
		var anno string
		switch description := strconv.Quote(p.description); {
		case p.description != "" && p.required:
			anno = fmt.Sprintf("Annotated[%s, Field(description=%s)]", p.pyType, description)
		case p.description != "":
			anno = fmt.Sprintf("Annotated[%s | None, Field(description=%s)] = None", p.pyType, description)
		case p.required:
			anno = p.pyType
		default:
			anno = p.pyType + " | None = None"
		}
		args = append(args, p.name+": "+anno)
	}
	_, function := spec.SplitHandler(t.HandlerRef())
	fmt.Fprintf(&b, "def %s(%s) -> %s:\n", function, strings.Join(args, ", "), t.resultClass())
	fmt.Fprintf(&b, "    \"\"\"%s\"\"\"\n", cmp.Or(oneLineDoc(t.Description), "Describe what this tool does."))
	fmt.Fprintf(&b, "    return %s(result=\"Replace this with your own code.\")\n", t.resultClass())
	return b.String()
}

// oneLineDoc keeps a description on one docstring line.
func oneLineDoc(text string) string {
	return strings.ReplaceAll(strings.Join(strings.Fields(text), " "), `"""`, `'''`)
}

// stubTypes is what Python would read from handlerStub, known without running
// it, so the wizard's preflight needs no uv.
func (t Tool) stubTypes() stateschema.ToolTypes {
	properties := map[string]any{}
	required := []any{}
	for _, p := range t.stubParams() {
		property := map[string]any{"type": p.schema["type"]}
		if p.schema["type"] == nil {
			property["type"] = "string"
		}
		if p.description != "" {
			property["description"] = p.description
		}
		properties[p.name] = property
		if p.required {
			required = append(required, p.name)
		}
	}
	input := map[string]any{"type": "object", "title": t.Name + "_arguments", "properties": properties}
	if len(required) > 0 {
		input["required"] = required
	}
	return stateschema.ToolTypes{
		Description: cmp.Or(oneLineDoc(t.Description), "Describe what this tool does."),
		Input:       input,
		Output: map[string]any{
			"type": "object", "title": t.resultClass(), "description": "What " + t.Name + " returns.",
			"properties": map[string]any{"result": map[string]any{"type": "string", "default": "", "title": "Result"}},
		},
	}
}
