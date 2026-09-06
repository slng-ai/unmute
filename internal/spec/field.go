package spec

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// Shape is one named group of fields, declared once under `shapes:` and
// referred to by name from a variable's `type:` or from another shape's field:
//
//	shapes:
//	  - name: Appointment
//	    description: One thing being booked, moved or cancelled.
//	    fields:
//	      - scheduled_date: Date
//	      - scheduled_time: Time
//	      - appointment_type: Literal["haircut", "haircolor", "dry_cut"]
type Shape struct {
	// Name is what a `type:` refers to, written in CapWords because it names a
	// generated class the author reads back in Pydantic's own vocabulary.
	Name string `json:"name" yaml:"name"`
	// Description reaches the model as the class docstring.
	Description string  `json:"description,omitempty" yaml:"description,omitempty"`
	Fields      []Field `json:"fields" yaml:"fields"`
}

// Field is one member of a shape. It decodes from either form, so the common
// case is one line and only a field wanting a description grows a block:
//
//   - scheduled_date: Date
//
//   - name: scheduled_date
//     type: Date
//     description: The day, as the caller gave it.
type Field struct {
	Name        string `json:"name" yaml:"name"`
	Type        string `json:"type" yaml:"type"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// fieldKeys is the whole of a field's long form. It is written out rather than
// derived from the struct because the refusal below prints it, and a reader
// hitting that refusal wants the list in the order the contract shows it.
var fieldKeys = []string{"name", "type", "description"}

// UnmarshalYAML decodes both forms.
//
// goccy's NodeUnmarshaler, the choice Pair makes rather than the
// InterfaceUnmarshaler TaskItem makes, and the difference is load-bearing here.
// A custom unmarshaler intercepts the decode, so the yaml.Strict() set once at
// load.go's single call site never sees this node and never refuses an unknown
// key in it. Pair compensates by counting its own keys; this counts and names
// them, because the key that has to be refused is a specific one:
//
//	fields:
//	  - name: customer_phone
//	    type: Phone
//	    confirm: verify_customer     # refused, by name
//
// `confirm:` belongs to a value and not to a field (FR-008a). A per-field mark
// would let a guard on an unconfirmed value be escaped by nesting one level
// down, so a field that quietly swallowed the key would leave the guard looking
// enforced and not be.
func (f *Field) UnmarshalYAML(node ast.Node) error {
	line := 0
	if token := node.GetToken(); token != nil {
		line = token.Position.Line
	}
	values, err := fieldEntries(node, line)
	if err != nil {
		return err
	}
	// One key that is not `name:` is the short form, `- scheduled_date: Date`.
	if len(values) == 1 && values[0].Key.String() != "name" {
		f.Name = values[0].Key.String()
		text, ok := fieldText(values[0].Value)
		if !ok {
			return &PairError{Line: line, Msg: fmt.Sprintf(
				"field %q holds a %s, and a field is written %q with the type on the same line",
				f.Name, values[0].Value.Type(), "- name: Type")}
		}
		f.Type = text
		return nil
	}
	for _, value := range values {
		key := value.Key.String()
		text, ok := fieldText(value.Value)
		if !ok {
			return &PairError{Line: value.Key.GetToken().Position.Line, Msg: fmt.Sprintf(
				"field key %q holds a %s, and every key of a field holds one line of text",
				key, value.Value.Type())}
		}
		switch key {
		case "name":
			f.Name = text
		case "type":
			f.Type = text
		case "description":
			f.Description = text
		case "confirm":
			return &PairError{Line: value.Key.GetToken().Position.Line, Msg: fmt.Sprintf(
				"field %q declares %q. A confirm: belongs to the variable, not to a field inside it: "+
					"put it on the variable whose type names this shape, so a guard cannot be escaped "+
					"by naming the field one level down", f.Name, "confirm:")}
		default:
			return &PairError{Line: value.Key.GetToken().Position.Line, Msg: fmt.Sprintf(
				"field %q declares unknown key %q. A field takes %s",
				f.Name, key, strings.Join(fieldKeys, ", "))}
		}
	}
	if f.Name == "" {
		return &PairError{Line: line, Msg: fmt.Sprintf(
			"a field with no name. Write it %q, or as a %q block", "- scheduled_date: Date", "name:")}
	}
	if f.Type == "" {
		return &PairError{Line: line, Msg: fmt.Sprintf(
			"field %q declares no type. Write %q, one line, in Pydantic's own words", f.Name, "type:")}
	}
	return nil
}

// fieldText reads one field key's value as text.
//
// Both scalar shapes, because a description is prose and prose is written as a
// folded block: goccy parses `>-` and `|` into a LiteralNode rather than a
// StringNode, and a decoder that took only the second would refuse every
// description long enough to want wrapping.
func fieldText(node ast.Node) (string, bool) {
	switch scalar := node.(type) {
	case *ast.StringNode:
		return scalar.Value, true
	case *ast.LiteralNode:
		return strings.TrimRight(scalar.Value.Value, "\n"), true
	}
	return "", false
}

// fieldEntries reads the item's key-value nodes, refusing the two shapes a
// dropped indent produces. A one-key item parses as a MappingValueNode and
// everything else as a MappingNode, the same split Pair reads.
func fieldEntries(node ast.Node, line int) ([]*ast.MappingValueNode, error) {
	switch shape := node.(type) {
	case *ast.MappingValueNode:
		return []*ast.MappingValueNode{shape}, nil
	case *ast.MappingNode:
		if len(shape.Values) == 0 {
			return nil, &PairError{Line: line, Msg: fmt.Sprintf(
				"an empty field. Write it %q", "- scheduled_date: Date")}
		}
		// Two or more keys and no `name:` is the dropped indent that a
		// map[string]string used to swallow, and it is two fields written as
		// one. A long form always carries `name:`, so its presence is what
		// tells the two apart. One key is always the short form, whatever it
		// is called.
		if len(shape.Values) > 1 && !hasKey(shape.Values, "name") {
			keys := make([]string, 0, len(shape.Values))
			for _, value := range shape.Values {
				keys = append(keys, value.Key.String())
			}
			return nil, &PairError{Line: line, Msg: fmt.Sprintf(
				"a field holding %d keys (%s) and no %q. One item, one field: give each its own %q line, "+
					"or write the long form with %q, %q and %q",
				len(keys), strings.Join(keys, ", "), "name:", "- ", "name:", "type:", "description:")}
		}
		return shape.Values, nil
	default:
		return nil, &PairError{Line: line, Msg: fmt.Sprintf(
			"a field must be written %q, or as a block carrying %q and %q, and this item is a %s",
			"- scheduled_date: Date", "name:", "type:", node.Type())}
	}
}

func hasKey(values []*ast.MappingValueNode, name string) bool {
	for _, value := range values {
		if value.Key.String() == name {
			return true
		}
	}
	return false
}

// MarshalYAML writes the short form back when the field has no description, so
// a console round-trip does not grow every one-line field into a block. The
// long form goes through MapSlice rather than a map, because a map would sort
// the three keys and print `description:` above the name it describes.
func (f Field) MarshalYAML() (any, error) {
	if f.Description == "" {
		return yaml.MapSlice{{Key: f.Name, Value: f.Type}}, nil
	}
	return yaml.MapSlice{
		{Key: "name", Value: f.Name},
		{Key: "type", Value: f.Type},
		{Key: "description", Value: f.Description},
	}, nil
}
