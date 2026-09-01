package spec

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// Pair is one item of a pair list, authored as `- key: value`:
//
//	assign:
//	  - customer_phone: result.value
//	  - customer_name: result.name
//
// so the authoring surface reads like a mapping while the Go type carries no map
// (CLAUDE.md, no dictionaries in the authoring surface). A map has no order a
// reader can see, and no place to put a per-entry comment where anybody will
// find it.
//
// Value is `any` because the two pair lists differ in what a value may be, not
// in how it decodes: an `args:` value is a scalar of the tool's own input type,
// while an `assign:` value is always a `result.<field>` string. Build validates
// each in its own place.
type Pair struct {
	Key   string
	Value any
}

// PairError is a decode refusal that knows its line but not its file. The file
// name belongs to whoever asked for the decode, so Package.decode joins the two
// and prints `agent.yaml:22: ...` the way every other refusal in the tree does.
type PairError struct {
	Line int
	Msg  string
}

func (e *PairError) Error() string { return fmt.Sprintf("%d: %s", e.Line, e.Msg) }

// UnmarshalYAML decodes one `- key: value` item.
//
// goccy's NodeUnmarshaler is the deliberate choice here, rather than the
// InterfaceUnmarshaler that Regions uses: the AST node carries the line, and the
// line is most of what makes this refusal useful. A dropped indent is the
// mistake this catches, and it produces an item holding two keys:
//
//	assign:
//	  - customer_name: result.name
//	    customer_id: result.id
//
// which is one item with two pairs rather than two items. A
// map[string]string would have accepted it silently and lost the author's
// intent, which is the second reason this type exists at all.
func (p *Pair) UnmarshalYAML(node ast.Node) error {
	line := 0
	if token := node.GetToken(); token != nil {
		line = token.Position.Line
	}
	mapping, ok := node.(*ast.MappingNode)
	if !ok {
		// A one-key mapping parses as a MappingValueNode rather than a
		// MappingNode, which is the shape every correctly written item has.
		value, single := node.(*ast.MappingValueNode)
		if !single {
			return &PairError{Line: line, Msg: fmt.Sprintf(
				"a pair list item must be written %q, one key to one value, and this item is a %s",
				"- key: value", node.Type())}
		}
		return p.set(value, line)
	}
	switch len(mapping.Values) {
	case 1:
		return p.set(mapping.Values[0], line)
	case 0:
		return &PairError{Line: line, Msg: `an empty pair list item. Write it as "- name: value"`}
	default:
		keys := make([]string, 0, len(mapping.Values))
		for _, value := range mapping.Values {
			keys = append(keys, value.Key.String())
		}
		return &PairError{Line: line, Msg: fmt.Sprintf(
			"a pair list item holding %d keys (%s). One item, one pair: give each its own %q line",
			len(keys), strings.Join(keys, ", "), "- ")}
	}
}

// set fills the pair from a single key-value node, decoding the value as a bare
// scalar so a nested mapping or list is refused here rather than reaching Build
// as something no pair value can be.
func (p *Pair) set(value *ast.MappingValueNode, line int) error {
	p.Key = value.Key.String()
	switch scalar := value.Value.(type) {
	case *ast.StringNode:
		p.Value = scalar.Value
	case *ast.IntegerNode:
		p.Value = scalar.Value
	case *ast.FloatNode:
		p.Value = scalar.Value
	case *ast.BoolNode:
		p.Value = scalar.Value
	case *ast.NullNode:
		p.Value = nil
	default:
		return &PairError{Line: line, Msg: fmt.Sprintf(
			"pair %q holds a %s, and a pair value is one scalar", p.Key, value.Value.Type())}
	}
	return nil
}

// MarshalYAML writes the pair back as the one-key mapping the author wrote, so a
// console round-trip does not reshape their file.
func (p Pair) MarshalYAML() (any, error) {
	return map[string]any{p.Key: p.Value}, nil
}
