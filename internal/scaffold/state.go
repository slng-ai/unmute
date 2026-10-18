package scaffold

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/stateschema"
)

// statePy is the state.py a package is written with. An author's own file is
// copied as written, because it is theirs and the console cannot edit Python.
// A package with no file of its own gets one made from the console's simple
// variables: a plain field each, with its default and its description. With
// no variables it is an empty State, ready for the author's first field.
func statePy(d Data) []byte {
	if d.State != nil {
		return d.State
	}
	// Twilio and SLNG run their own runtime, which keeps no State.
	if len(d.Variables) == 0 && d.Target != "livekit" && d.Target != "pipecat" {
		return nil
	}
	described := slices.ContainsFunc(d.Variables, func(v Variable) bool { return v.Description != "" })
	var b strings.Builder
	b.WriteString(`"""The call state: every value the agents and tasks of one call share."""` + "\n\n")
	if described {
		b.WriteString("from pydantic import BaseModel, Field\n\n\n")
	} else {
		b.WriteString("from pydantic import BaseModel\n\n\n")
	}
	b.WriteString("class State(BaseModel):\n")
	b.WriteString(`    """What one call knows, shared by every agent and task in it."""` + "\n")
	if len(d.Variables) > 0 {
		b.WriteString("\n")
	}
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

// readState gives a loaded package its state model and its local tools' types.
// What the console wrote itself is known without running Python, so the wizard
// stays free of uv: its state.py and the handler stubs it made. An author's own
// state.py or handler is read through reader.
func (d Data) readState(ctx context.Context, pkg *spec.Package, reader stateschema.Reader) error {
	return pkg.ReadState(ctx, consoleReader{data: d, next: reader})
}

// consoleReader answers for the files the console wrote, and asks next about
// the rest.
type consoleReader struct {
	data Data
	next stateschema.Reader
}

func (r consoleReader) Read(ctx context.Context, dir string, source []byte) (*stateschema.Model, error) {
	console := r.data
	console.State = nil
	if r.data.State == nil || bytes.Equal(r.data.State, statePy(console)) {
		return r.data.stateModel(), nil
	}
	return r.next.Read(ctx, dir, source)
}

func (r consoleReader) ReadTools(ctx context.Context, dir string, state []byte, refs []stateschema.ToolRef) (map[string]stateschema.ToolTypes, error) {
	stubs := map[string]Tool{}
	for _, tool := range r.data.Tools {
		if tool.ExecutionKind() == "local" && (tool.HandlerSource == "" || tool.HandlerSource == tool.handlerStub()) {
			stubs[tool.Name] = tool
		}
	}
	types := make(map[string]stateschema.ToolTypes, len(refs))
	for _, ref := range refs {
		tool, ok := stubs[ref.Name]
		if !ok {
			// One handler the console did not write: read them all through
			// Python, the stubs too, so every tool is read the same way.
			return r.next.ReadTools(ctx, dir, state, refs)
		}
		types[ref.Name] = tool.stubTypes()
	}
	return types, nil
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
