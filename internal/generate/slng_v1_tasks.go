package generate

import (
	"maps"
	"slices"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/stateschema"
)

// The task half of the SLNG body: tasks, task_groups and shapes, in the shape
// of slng_backend app/schemas/agent_tasks.py (read at 66caa2a3). Every key a
// default covers is left out, because the backend drops it when it stores the
// document and a body carrying it would read as a change on every push.
//
// A task names its tools by the names the body's tool_refs and mcp_refs use.
// SLNG wants attachment ids there, and `voiceai agents push` swaps them in
// after it has chosen them, the same way it fills tool_refs.

type slngTask struct {
	Name         string `json:"name"`
	Instructions string `json:"instructions"`
	When         string `json:"when,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	// Each entry holds one key, the variable (with a trailing + to append), and
	// its result path.
	Assign   []map[string]string `json:"assign,omitempty"`
	Announce string              `json:"announce,omitempty"`
	Opening  string              `json:"opening,omitempty"`
	Context  *slngTaskContext    `json:"context,omitempty"`
}

type slngTaskContext struct {
	History     string `json:"history"`
	MaxMessages int    `json:"max_messages,omitempty"`
}

type slngGroup struct {
	Name string `json:"name"`
	When string `json:"when,omitempty"`
	// A bare task name, or a slngStep when the step can be skipped.
	Steps        []any  `json:"steps"`
	Announce     string `json:"announce,omitempty"`
	ContextScope string `json:"context_scope"`
	Then         string `json:"then"`
}

type slngStep struct {
	Task              string `json:"task"`
	SkipWhenConfirmed string `json:"skip_when_confirmed"`
}

type slngShape struct {
	Name   string           `json:"name"`
	Fields []slngShapeField `json:"fields"`
}

type slngShapeField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// slngTaskBody fills the three task lists and types the runtime variables.
// toolNames maps a package tool to the names its references carry in the
// body, more than one for an MCP source.
func slngTaskBody(agent *ir.Agent, body *slngBody, toolNames map[string][]string) error {
	delegates := map[string]*ir.Delegate{}
	for _, control := range agent.Controls {
		if delegate, ok := control.(*ir.Delegate); ok {
			delegates[delegate.Task+delegate.Group] = delegate
		}
	}

	for _, name := range slices.Sorted(maps.Keys(agent.Tasks)) {
		task := agent.Tasks[name]
		out := slngTask{Name: name, Instructions: ir.DottedPaths(task.Instructions)}
		if delegate := delegates[name]; delegate != nil {
			out.When = delegate.When
		}
		for _, tool := range task.Tools {
			out.Tools = append(out.Tools, toolNames[tool]...)
		}
		for _, assign := range task.Assign {
			key := assign.Var
			if assign.Append {
				key += "+"
			}
			out.Assign = append(out.Assign, map[string]string{key: "result." + assign.Field})
		}
		// ir.Validate refused a second line on this target.
		if len(task.Announce) > 0 {
			out.Announce = task.Announce[0]
		}
		if task.Opening == ir.OpeningListen {
			out.Opening = string(task.Opening)
		}
		if history := task.Context.History; history != "" && history != ir.HistoryMessages {
			out.Context = &slngTaskContext{History: string(history), MaxMessages: task.Context.MaxMessages}
		}
		body.Tasks = append(body.Tasks, out)
	}

	for _, name := range slices.Sorted(maps.Keys(agent.TaskGroups)) {
		group := agent.TaskGroups[name]
		out := slngGroup{Name: name, ContextScope: string(group.ContextScope), Then: string(group.Then)}
		if delegate := delegates[name]; delegate != nil {
			out.When = delegate.When
			if len(delegate.Announce) > 0 {
				out.Announce = delegate.Announce[0]
			}
		}
		for _, step := range group.Steps {
			if step.SkipWhenConfirmed == "" {
				out.Steps = append(out.Steps, step.Task)
				continue
			}
			out.Steps = append(out.Steps, slngStep{Task: step.Task, SkipWhenConfirmed: step.SkipWhenConfirmed})
		}
		body.TaskGroups = append(body.TaskGroups, out)
	}

	seen := map[string]bool{}
	for i, variable := range body.RuntimeVars {
		declared := agent.Variables[variable.Name]
		body.RuntimeVars[i].Confirm = declared.Confirm
		if declared.Schema == nil {
			continue
		}
		typ, err := ir.SlngType(declared.Schema)
		if err != nil {
			return err // ir.Validate refused it already; never emit a guess
		}
		if typ != "str" {
			body.RuntimeVars[i].Type = typ
		}
		if err := slngShapes(declared.Schema, seen, &body.Shapes); err != nil {
			return err
		}
	}
	return nil
}

// slngShapes adds one shape per BaseModel a type reaches, each once, nested
// models before the model that holds them so a reader meets a name after its
// definition.
func slngShapes(t *stateschema.Type, seen map[string]bool, out *[]slngShape) error {
	if t.Items != nil {
		return slngShapes(t.Items, seen, out)
	}
	if t.Kind != stateschema.KindObject || len(t.Enum) > 0 || seen[t.Model] {
		return nil
	}
	seen[t.Model] = true
	shape := slngShape{Name: t.Model}
	for _, field := range t.Fields {
		if err := slngShapes(field.Type, seen, out); err != nil {
			return err
		}
		typ, err := ir.SlngType(field.Type)
		if err != nil {
			return err
		}
		shape.Fields = append(shape.Fields, slngShapeField{Name: field.Name, Type: typ, Description: field.Description})
	}
	*out = append(*out, shape)
	return nil
}
