package spec

import (
	"fmt"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// FinishEntry is one item under a task's `finish:`: a tool of that task, and the
// result values that mean it succeeded.
//
//	finish:
//	  - tool: create_booking
//	    success:
//	      - status: booked
//
// When that tool returns a result meeting every success pair, the step saves its
// `assign:` from the result and ends, with no model request in between. The
// result never reaches the model.
type FinishEntry struct {
	Tool    string        `json:"tool" yaml:"tool"`
	Success []SuccessPair `json:"success" yaml:"success"`
}

// SuccessPair is one `field: value` or `field: [value, value]` item under
// `success:`. Items on different fields are all required; several values on one
// field are alternatives.
//
// It is its own type rather than a Pair because a Pair value is one scalar, and
// a success value is one scalar or a list of them. Everything else about it is
// Pair's contract, refusal for refusal: an item holding two keys or none is
// refused at decode with its line, because that is what a dropped indent
// produces and what a map[string][]string would have swallowed.
type SuccessPair struct {
	Field  string
	Values []string
}

func (s *SuccessPair) UnmarshalYAML(node ast.Node) error {
	line := 0
	if token := node.GetToken(); token != nil {
		line = token.Position.Line
	}
	mapping, ok := node.(*ast.MappingNode)
	if !ok {
		value, single := node.(*ast.MappingValueNode)
		if !single {
			return &PairError{Line: line, Msg: fmt.Sprintf(
				"a success item must be written %q or %q, and this item is a %s",
				"- field: value", "- field: [value, value]", node.Type())}
		}
		return s.set(value, line)
	}
	switch len(mapping.Values) {
	case 1:
		return s.set(mapping.Values[0], line)
	case 0:
		return &PairError{Line: line, Msg: `an empty success item. Write it as "- field: value"`}
	default:
		keys := make([]string, 0, len(mapping.Values))
		for _, value := range mapping.Values {
			keys = append(keys, value.Key.String())
		}
		return &PairError{Line: line, Msg: fmt.Sprintf(
			"a success item holding %d keys (%s). One item, one field: give each its own %q line",
			len(keys), joinKeys(keys), "- ")}
	}
}

// set fills the pair from a single key-value node. The value is one scalar or a
// list of scalars; anything else is refused here rather than reaching Build as
// something no success value can be. Values are kept as strings, and the
// comparison against the tool's declared enum happens in ir.Build.
func (s *SuccessPair) set(value *ast.MappingValueNode, line int) error {
	s.Field = value.Key.String()
	switch node := value.Value.(type) {
	case *ast.SequenceNode:
		if len(node.Values) == 0 {
			return &PairError{Line: line, Msg: fmt.Sprintf(
				"success %q lists no value, so nothing can meet it. Name at least one the tool declares", s.Field)}
		}
		for _, item := range node.Values {
			scalar, err := successScalar(s.Field, item, line)
			if err != nil {
				return err
			}
			s.Values = append(s.Values, scalar)
		}
		return nil
	default:
		scalar, err := successScalar(s.Field, value.Value, line)
		if err != nil {
			return err
		}
		s.Values = []string{scalar}
		return nil
	}
}

func successScalar(field string, node ast.Node, line int) (string, error) {
	switch scalar := node.(type) {
	case *ast.StringNode:
		return scalar.Value, nil
	case *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode:
		return scalar.String(), nil
	default:
		return "", &PairError{Line: line, Msg: fmt.Sprintf(
			"success %q holds a %s, and a success value is one scalar or a list of scalars", field, node.Type())}
	}
}

// MarshalYAML writes the item back as the author wrote it: a bare value where
// there is one, a list where there were several, so a console round-trip does
// not reshape their file.
func (s SuccessPair) MarshalYAML() (any, error) {
	if len(s.Values) == 1 {
		return map[string]any{s.Field: s.Values[0]}, nil
	}
	return map[string]any{s.Field: s.Values}, nil
}

func joinKeys(keys []string) string {
	out := ""
	for i, key := range keys {
		if i > 0 {
			out += ", "
		}
		out += key
	}
	return out
}

// StepItem is one item of a group's `steps:`. A bare string names a task the
// group always runs; a mapping names it and says how the group treats it:
//
//	steps:
//	  - task: verify_customer
//	    skip_when_confirmed: customer_phone
//	  - manage_booking
//
// The two shapes are decoded the way TaskItem decodes its two, so a group that
// never needed the mapping keeps reading exactly as it did.
type StepItem struct {
	Task string `json:"task" yaml:"task"`
	// SkipWhenConfirmed names a variable. The group skips this step when that
	// variable is confirmed at the moment the group starts, and runs it
	// otherwise. Empty means the step always runs.
	SkipWhenConfirmed string `json:"skip_when_confirmed,omitempty" yaml:"skip_when_confirmed,omitempty"`
}

// UnmarshalYAML accepts both shapes. A NodeUnmarshaler rather than the
// InterfaceUnmarshaler TaskItem uses, because the one refusal this has of its
// own, an item naming no task, is only useful with its line.
func (s *StepItem) UnmarshalYAML(node ast.Node) error {
	line := 0
	if token := node.GetToken(); token != nil {
		line = token.Position.Line
	}
	if scalar, ok := node.(*ast.StringNode); ok {
		s.Task = scalar.Value
		return nil
	}
	type plainStep StepItem
	if err := yaml.NodeToValue(node, (*plainStep)(s), yaml.Strict()); err != nil {
		return err
	}
	if s.Task == "" {
		return &PairError{Line: line, Msg: `a group step is a task name or an item with "task:"`}
	}
	return nil
}

// MarshalYAML writes a bare name back as the bare name the author wrote, so a
// console round-trip does not reshape their file.
func (s StepItem) MarshalYAML() (any, error) {
	if s.SkipWhenConfirmed == "" {
		return s.Task, nil
	}
	return map[string]any{"task": s.Task, "skip_when_confirmed": s.SkipWhenConfirmed}, nil
}
