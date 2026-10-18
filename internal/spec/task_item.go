package spec

import (
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

func (t *Task) UnmarshalYAML(node ast.Node) error {
	if err := rejectTaskFields(node); err != nil {
		return err
	}
	if err := rejectRemovedTaskKeys(node); err != nil {
		return err
	}
	type plainTask Task
	if err := yaml.NodeToValue(node, (*plainTask)(t), yaml.Strict()); err != nil {
		return err
	}
	return nil
}

// TaskItem is one item of an agent's `tasks:` list. A mapping defines a task and
// says when this agent runs it; a bare string names a task another agent defines,
// so two agents can offer the same task without either owning a copy of it:
//
//	tasks:
//	  - name: verify_customer
//	    when: the caller has not been identified
//	    instructions: ...
//	  - look_up_order
type TaskItem struct {
	Ref  string
	Task *Task
}

// UnmarshalYAML accepts both shapes.
//
// goccy's InterfaceUnmarshaler is the deliberate choice here, the same one
// Regions makes: its callback re-enters the decoder, so a mapping with a typo in
// it is refused with goccy's own line, column and caret rather than a sentence of
// ours with no position.
func (t *TaskItem) UnmarshalYAML(unmarshal func(any) error) error {
	var ref string
	if err := unmarshal(&ref); err == nil {
		t.Ref = ref
		return nil
	}
	var task Task
	if err := unmarshal(&task); err != nil {
		return err
	}
	t.Task = &task
	return nil
}

// MarshalYAML writes a bare name back as the bare name the author wrote, so a
// console round-trip does not reshape their file.
func (t TaskItem) MarshalYAML() (any, error) {
	if t.Task != nil {
		return t.Task, nil
	}
	return t.Ref, nil
}

func rejectTaskFields(node ast.Node) error {
	var entries []*ast.MappingValueNode
	switch n := node.(type) {
	case *ast.MappingNode:
		entries = n.Values
	case *ast.MappingValueNode:
		entries = []*ast.MappingValueNode{n}
	}

	for _, entry := range entries {
		key := entry.Key.String()
		if key == "result" || key == "expect" || key == "requires" {
			return &PairError{Line: entry.Key.GetToken().Position.Line,
				Msg: key + ": is retired. Declare typed variables and save them with assign:. Use messages history for earlier speech, or reference saved values in a reset prompt."}
		}
	}
	return nil
}

// removedTaskKeys are step keys that were removed, each with the sentence that
// says what replaces it. They are refused by name, so an author upgrading a
// file reads what to write instead of a bare "unknown field".
var removedTaskKeys = map[string]string{
	"inputs": "inputs: is removed. A prompt reads a State value where it names {{state.x}}: write that in the step's prompt file. Save a prefetch result to State first if the step needs it.",
	"via":    "via: is removed. The agent starts a step through the step's when: sentence or a flow.",
}

func rejectRemovedTaskKeys(node ast.Node) error {
	mapping, ok := node.(*ast.MappingNode)
	if !ok {
		return nil
	}
	for _, entry := range mapping.Values {
		if msg, removed := removedTaskKeys[entry.Key.String()]; removed {
			return &PairError{Line: entry.Key.GetToken().Position.Line, Msg: msg}
		}
	}
	return nil
}

func (h *Handoff) UnmarshalYAML(node ast.Node) error {
	if err := rejectTaskFields(node); err != nil {
		return err
	}
	type plainHandoff Handoff
	if err := yaml.NodeToValue(node, (*plainHandoff)(h), yaml.Strict()); err != nil {
		return err
	}
	return nil
}
