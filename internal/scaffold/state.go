package scaffold

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/stateschema"
)

// statePy is the state.py a package is written with. An author's own file is
// copied as written, because it is theirs and the console cannot edit Python.
// A package with no file of its own gets one made from the console's simple
// variables: a plain field each, with its default and its description.
func statePy(d Data) []byte {
	if d.State != nil {
		return d.State
	}
	if len(d.Variables) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(`"""The call state: every value the agents and tasks of one call share."""` + "\n\n")
	b.WriteString("from pydantic import BaseModel, Field\n\n\n")
	b.WriteString("class State(BaseModel):\n")
	b.WriteString(`    """What one call knows, shared by every agent and task in it."""` + "\n\n")
	for _, variable := range d.Variables {
		anno := pythonTypes[variable.Type]
		if anno == "" {
			anno = "str"
		}
		value := pythonDefault(variable.Default)
		if value == "None" {
			anno += " | None"
		}
		if variable.Description != "" {
			value = fmt.Sprintf("Field(%s, description=%s)", value, strconv.Quote(variable.Description))
		}
		fmt.Fprintf(&b, "    %s: %s = %s\n", variable.Name, anno, value)
	}
	return []byte(b.String())
}

// BehaviourVariables are the variables agent.yaml lists: the ones carrying
// source: or confirm:. Their types live in state.py.
func (d Data) BehaviourVariables() []Variable {
	var out []Variable
	for _, variable := range d.Variables {
		if variable.Source != "" || variable.Confirm != "" {
			out = append(out, variable)
		}
	}
	return out
}

// stateModel is what Pydantic would say about the state.py statePy makes. The
// console's variables are four plain types, so the answer is known without
// running Python, and the wizard stays free of uv. An author's own state.py is
// read through the reader instead.
func (d Data) stateModel() *stateschema.Model {
	model := &stateschema.Model{}
	for _, variable := range d.Variables {
		kind := stateschema.Kind(variable.Type)
		if pythonTypes[variable.Type] == "" {
			kind = stateschema.KindString
		}
		field := stateschema.Field{Name: variable.Name, Description: variable.Description, Type: &stateschema.Type{Kind: kind}}
		if pythonDefault(variable.Default) == "None" {
			field.Type.Nullable, field.Default = true, json.RawMessage("null")
		} else {
			field.Default = json.RawMessage(variable.Default)
		}
		model.Fields = append(model.Fields, field)
	}
	return model
}

// readState gives a loaded package its state model: the one stateModel knows
// for the console's own variables, or the author's file read through reader.
func (d Data) readState(ctx context.Context, pkg *spec.Package, reader stateschema.Reader) error {
	if d.State == nil && len(d.Variables) > 0 {
		pkg.State = d.stateModel()
		return nil
	}
	return pkg.ReadState(ctx, reader)
}

// pythonTypes are the console's four plain types as Python writes them.
var pythonTypes = map[string]string{"string": "str", "number": "float", "integer": "int", "boolean": "bool"}

// pythonDefault writes a console default, which is JSON text, as Python.
func pythonDefault(text string) string {
	var value any
	if text == "" || json.Unmarshal([]byte(text), &value) != nil || value == nil {
		return "None"
	}
	switch typed := value.(type) {
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case string:
		return strconv.Quote(typed)
	default:
		return text
	}
}
