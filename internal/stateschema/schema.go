// Package stateschema reads the author's state.py through the JSON Schema
// Pydantic writes for it, and answers the questions the compiler asks of a type:
// which fields a value has, whether one value fits where another is saved, and
// how to name a type in a message.
//
// The compiler never writes or parses a Python type. The model is the author's,
// Pydantic says what it is, and this package reads what Pydantic said.
package stateschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Kind is a JSON Schema type word. Pydantic writes exactly one per schema node,
// and a nullable value as anyOf with null, never as a list of words.
type Kind string

// The kinds a state value can have.
const (
	KindString  Kind = "string"
	KindInteger Kind = "integer"
	KindNumber  Kind = "number"
	KindBoolean Kind = "boolean"
	KindObject  Kind = "object"
	KindArray   Kind = "array"
)

// Model is the author's State class: its fields in the order the class declares
// them, and the digest of the file it was read from.
type Model struct {
	Fields []Field `json:"fields"`
	// Digest names the state.py and the pins it was read with. A recorded
	// fixture is filed under it, so an edited file has no fixture until it is
	// recorded again.
	Digest string `json:"digest"`
	// ExtraTypes lists the pydantic_extra_types modules state.py imports, so the
	// emitted project installs what the model needs and nothing more.
	ExtraTypes []string `json:"extra_types,omitempty"`
}

// Field is one field of a model.
type Field struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Type        *Type  `json:"type"`
	// Required means the field has no default. A top-level State field must
	// have one, because a call starts from State() with nothing filled in.
	Required bool `json:"required,omitempty"`
	// Default is the JSON Pydantic wrote for the default: "null" for `= None`,
	// absent for a default_factory, and verbatim otherwise.
	Default json.RawMessage `json:"default,omitempty"`
}

// HasValue reports whether the field starts the call holding something a
// prompt could show: a default that is not None or an empty string, or a
// factory.
func (f Field) HasValue() bool {
	if f.Required {
		return false
	}
	text := string(f.Default)
	return text != "null" && text != `""`
}

// DefaultValue is the default decoded to plain Go data, or nil.
func (f Field) DefaultValue() any {
	if len(f.Default) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(f.Default, &value) != nil {
		return nil
	}
	return value
}

// Type is one JSON Schema node with its references resolved.
type Type struct {
	Kind     Kind `json:"kind"`
	Nullable bool `json:"nullable,omitempty"`
	// Enum is a Literal or an Enum class, as its string members.
	Enum    []string `json:"enum,omitempty"`
	Format  string   `json:"format,omitempty"`
	Pattern string   `json:"pattern,omitempty"`
	// Model names the BaseModel or Enum class a $ref pointed at.
	Model  string  `json:"model,omitempty"`
	Fields []Field `json:"fields,omitempty"`
	Items  *Type   `json:"items,omitempty"`
}

// Field returns the named field of an object type.
func (t *Type) Field(name string) (Field, bool) {
	for _, field := range t.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return Field{}, false
}

// Field returns the named top-level field.
func (m *Model) Field(name string) (Field, bool) {
	for _, field := range m.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return Field{}, false
}

// Plain reports whether the type is one scalar with nothing Pydantic would
// check beyond its kind: no closed set, no format and no pattern.
func (t *Type) Plain() bool {
	switch t.Kind {
	case KindObject, KindArray:
		return false
	}
	return len(t.Enum) == 0 && t.Format == "" && t.Pattern == ""
}

// IsList reports whether the type is a list.
func (t *Type) IsList() bool { return t != nil && t.Kind == KindArray }

// String names the type the way a Python author would read it, for messages.
func (t *Type) String() string {
	if t == nil {
		return "nothing"
	}
	var name string
	switch {
	case t.Model != "":
		name = t.Model
	case len(t.Enum) > 0:
		quoted := make([]string, len(t.Enum))
		for i, value := range t.Enum {
			quoted[i] = fmt.Sprintf("%q", value)
		}
		name = "Literal[" + strings.Join(quoted, ", ") + "]"
	case t.Kind == KindArray:
		name = "list[" + t.Items.String() + "]"
	case t.Kind == KindObject:
		name = "an object"
	default:
		name = pythonNames[t.Kind]
		if t.Format != "" {
			name += " (" + t.Format + ")"
		}
	}
	if t.Nullable {
		name += " | None"
	}
	return name
}

var pythonNames = map[Kind]string{
	KindString: "str", KindInteger: "int", KindNumber: "float", KindBoolean: "bool",
}

// Path walks field names into an object type, and returns the type at the end.
// A value that may be None anywhere along the way may be None at the end.
func (t *Type) Path(fields []string) (*Type, error) {
	current := t
	nullable := false
	for i, name := range fields {
		where := strings.Join(fields[:i], ".")
		if current.Kind == KindArray {
			return nil, fmt.Errorf("%s is a list, so it has no field %q; name the list itself", orField(where), name)
		}
		if current.Kind != KindObject {
			return nil, fmt.Errorf("%s is %s, which has no fields to name", orField(where), current)
		}
		field, ok := current.Field(name)
		if !ok {
			return nil, fmt.Errorf("%s has no field %q; it has %s", orField(where), name, fieldNames(current.Fields))
		}
		nullable = nullable || current.Nullable
		current = field.Type
	}
	if nullable && !current.Nullable {
		copied := *current
		copied.Nullable = true
		current = &copied
	}
	return current, nil
}

func fieldNames(fields []Field) string {
	names := make([]string, len(fields))
	for i, field := range fields {
		names[i] = field.Name
	}
	return strings.Join(names, ", ")
}

// Fits says whether a value of type src can be saved where dst is declared. It
// is the one rule a step's assign:, a pre-fetch and a terminal tool are held to.
// It checks shape and closed sets, the parts a mistake would make wrong on
// every call. A format or a pattern is the runtime's to check, on the value.
func Fits(src, dst *Type) error {
	if src.Nullable && !dst.Nullable {
		return fmt.Errorf("the value may be None and the destination is %s", dst)
	}
	switch dst.Kind {
	case KindArray:
		if src.Kind != KindArray {
			return fmt.Errorf("the value is %s and the destination is %s", src, dst)
		}
		return Fits(src.Items, dst.Items)
	case KindObject:
		if src.Kind != KindObject {
			return fmt.Errorf("the value is %s and the destination is %s", src, dst)
		}
		if src.Model != "" && src.Model == dst.Model {
			return nil
		}
		for _, want := range dst.Fields {
			have, ok := src.Field(want.Name)
			if !ok {
				if want.Required {
					return fmt.Errorf("the value has no %s, which %s requires", want.Name, dst)
				}
				continue
			}
			if err := Fits(have.Type, want.Type); err != nil {
				return fmt.Errorf("%s: %w", want.Name, err)
			}
		}
		return nil
	case KindNumber:
		if src.Kind != KindNumber && src.Kind != KindInteger {
			return fmt.Errorf("the value is %s and the destination is %s", src, dst)
		}
		return nil
	}
	if src.Kind != dst.Kind {
		return fmt.Errorf("the value is %s and the destination is %s", src, dst)
	}
	if len(dst.Enum) == 0 {
		return nil
	}
	if len(src.Enum) == 0 {
		return fmt.Errorf("the destination allows only %s, and the value is any %s; declare the same set on the value",
			strings.Join(dst.Enum, ", "), pythonNames[src.Kind])
	}
	for _, value := range src.Enum {
		if !slices.Contains(dst.Enum, value) {
			return fmt.Errorf("the value can be %s, which the destination does not allow; it allows %s",
				value, strings.Join(dst.Enum, ", "))
		}
	}
	return nil
}

// raw is one JSON Schema node as Pydantic writes it, or as an author writes a
// tool's output: property.
type raw struct {
	Type        json.RawMessage            `json:"type"`
	Format      string                     `json:"format"`
	Pattern     string                     `json:"pattern"`
	Description string                     `json:"description"`
	Ref         string                     `json:"$ref"`
	Enum        []any                      `json:"enum"`
	Const       json.RawMessage            `json:"const"`
	AnyOf       []json.RawMessage          `json:"anyOf"`
	OneOf       []json.RawMessage          `json:"oneOf"`
	AllOf       []json.RawMessage          `json:"allOf"`
	Items       json.RawMessage            `json:"items"`
	PrefixItems json.RawMessage            `json:"prefixItems"`
	Properties  orderedProps               `json:"properties"`
	Required    []string                   `json:"required"`
	Additional  json.RawMessage            `json:"additionalProperties"`
	Default     json.RawMessage            `json:"default"`
	Defs        map[string]json.RawMessage `json:"$defs"`
}

// orderedProps keeps a properties object in the order it was written, which is
// the order the class declares its fields.
type orderedProps []namedRaw

type namedRaw struct {
	name string
	raw  json.RawMessage
}

func (p *orderedProps) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return errors.New("properties is not an object")
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		*p = append(*p, namedRaw{name: token.(string), raw: value})
	}
	return nil
}

// resolver turns raw nodes into types, following $ref into $defs.
type resolver struct {
	defs    map[string]json.RawMessage
	walking []string
	// lenient reads a tool's output: the way the compiler always has, where an
	// untyped property is text. A state.py schema is read strictly.
	lenient bool
}

// Parse reads the schema State.model_json_schema() wrote.
func Parse(schema []byte) (*Model, error) {
	var top raw
	if err := json.Unmarshal(schema, &top); err != nil {
		return nil, fmt.Errorf("the State schema is not JSON: %w", err)
	}
	r := &resolver{defs: top.Defs}
	fields, err := r.fields(top)
	if err != nil {
		return nil, err
	}
	return &Model{Fields: fields}, nil
}

// ParseProperty reads one property of a tool's output: schema, as the author
// wrote it in YAML. An untyped property is read as text, as it always was.
func ParseProperty(property map[string]any) (*Type, error) {
	data, err := json.Marshal(property)
	if err != nil {
		return nil, err
	}
	r := &resolver{lenient: true}
	return r.node(data, "")
}

func (r *resolver) fields(object raw) ([]Field, error) {
	out := make([]Field, 0, len(object.Properties))
	for _, prop := range object.Properties {
		typ, err := r.node(prop.raw, prop.name)
		if err != nil {
			return nil, err
		}
		var meta raw
		_ = json.Unmarshal(prop.raw, &meta)
		out = append(out, Field{
			Name: prop.name, Description: meta.Description, Type: typ,
			Required: slices.Contains(object.Required, prop.name), Default: meta.Default,
		})
	}
	return out, nil
}

func (r *resolver) node(data json.RawMessage, where string) (*Type, error) {
	var node raw
	if err := json.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("%s: %w", orField(where), err)
	}
	if node.Ref != "" {
		return r.ref(node.Ref, where)
	}
	if len(node.AllOf) == 1 {
		return r.node(node.AllOf[0], where)
	}
	if branches := append(node.AnyOf, node.OneOf...); len(branches) > 0 {
		return r.union(branches, where)
	}
	kind, nullable, err := r.kind(node, where)
	if err != nil {
		return nil, err
	}
	typ := &Type{Kind: kind, Nullable: nullable, Format: node.Format, Pattern: node.Pattern}
	if len(node.Const) > 0 {
		node.Enum = []any{nil}
		_ = json.Unmarshal(node.Const, &node.Enum[0])
	}
	for _, value := range node.Enum {
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s: a closed set holds text here, and %v is not text; use Literal of strings", orField(where), value)
		}
		typ.Enum = append(typ.Enum, text)
	}
	switch kind {
	case KindArray:
		if len(node.PrefixItems) > 0 {
			return nil, fmt.Errorf("%s is a tuple; use list[...] or a BaseModel", orField(where))
		}
		if len(node.Items) == 0 {
			if !r.lenient {
				return nil, fmt.Errorf("%s is a list of anything; name the item type, as list[str]", orField(where))
			}
			typ.Items = &Type{Kind: KindString}
			break
		}
		if typ.Items, err = r.node(node.Items, where+"[]"); err != nil {
			return nil, err
		}
	case KindObject:
		if len(node.Additional) > 0 && string(node.Additional) != "false" && !r.lenient {
			return nil, fmt.Errorf("%s is a dict; declare its fields as a BaseModel instead", orField(where))
		}
		if typ.Fields, err = r.fields(node); err != nil {
			return nil, err
		}
	}
	return typ, nil
}

// kind reads the type word. A Pydantic schema always has one, apart from a
// Literal written as a bare enum. A tool's output may write a list of words.
func (r *resolver) kind(node raw, where string) (Kind, bool, error) {
	var words []string
	if len(node.Type) > 0 {
		var one string
		if json.Unmarshal(node.Type, &one) == nil {
			words = []string{one}
		} else if json.Unmarshal(node.Type, &words) != nil {
			return "", false, fmt.Errorf("%s: type is neither a word nor a list of words", orField(where))
		}
	}
	nullable := slices.Contains(words, "null")
	words = slices.DeleteFunc(words, func(word string) bool { return word == "null" })
	switch {
	case len(words) == 1:
		kind := Kind(words[0])
		if _, ok := pythonNames[kind]; ok || kind == KindObject || kind == KindArray {
			return kind, nullable, nil
		}
		return "", false, fmt.Errorf("%s has type %q, which is not a JSON Schema type", orField(where), words[0])
	case len(words) > 1:
		return "", false, fmt.Errorf("%s may be %s; a state value has one type, so use one or a BaseModel", orField(where), strings.Join(words, " or "))
	case len(node.Enum) > 0 || len(node.Const) > 0:
		return KindString, nullable, nil
	case len(node.Properties) > 0:
		return KindObject, nullable, nil
	case r.lenient:
		return KindString, nullable, nil
	}
	return "", false, fmt.Errorf("%s is Any; give it a type", orField(where))
}

// union reads anyOf. The only union a state value may be is one type or None.
func (r *resolver) union(branches []json.RawMessage, where string) (*Type, error) {
	var kept []json.RawMessage
	nullable := false
	for _, branch := range branches {
		var node raw
		if json.Unmarshal(branch, &node) == nil && string(node.Type) == `"null"` {
			nullable = true
			continue
		}
		kept = append(kept, branch)
	}
	if len(kept) != 1 {
		return nil, fmt.Errorf("%s is a union of %d types; a state value is one type, or one type | None", orField(where), len(kept))
	}
	typ, err := r.node(kept[0], where)
	if err != nil {
		return nil, err
	}
	if nullable && !typ.Nullable {
		copied := *typ
		copied.Nullable = true
		typ = &copied
	}
	return typ, nil
}

func (r *resolver) ref(ref, where string) (*Type, error) {
	name, ok := strings.CutPrefix(ref, "#/$defs/")
	if !ok {
		return nil, fmt.Errorf("%s refers to %s, which is not in this schema", orField(where), ref)
	}
	if slices.Contains(r.walking, name) {
		return nil, fmt.Errorf("%s holds itself through %s; a state value cannot be recursive", orField(where), name)
	}
	data, ok := r.defs[name]
	if !ok {
		return nil, fmt.Errorf("%s refers to %s, which the schema does not define", orField(where), name)
	}
	r.walking = append(r.walking, name)
	defer func() { r.walking = r.walking[:len(r.walking)-1] }()
	typ, err := r.node(data, where)
	if err != nil {
		return nil, err
	}
	typ.Model = name
	return typ, nil
}

func orField(where string) string {
	if where == "" {
		return "the value"
	}
	return where
}
