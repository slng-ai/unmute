package spec

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

// Schema derives the authoring-package schema at runtime.
func Schema() (*jsonschema.Schema, error) {
	options := &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		// Reflection sees Regions as a list and cannot express the scalar
		// form N32 keeps valid, so the library's own TypeSchemas hook says
		// it instead. Same hook internal/ir/schema.go uses for its unions.
		reflect.TypeFor[Regions](): {OneOf: []*jsonschema.Schema{
			{Type: "string"},
			{Type: "array", Items: &jsonschema.Schema{Type: "string"}},
		}},
		// Pair is the same kind of mismatch. Reflection sees Key and Value and
		// would publish `{key: ..., value: ...}`, which is not what anybody
		// writes: a pair is authored as a one-key mapping, `- phone: "+34..."`,
		// and UnmarshalYAML is what turns that into the struct. So the schema
		// says the authored shape, and says the "exactly one" part too, which
		// is the refusal a dropped indent hits.
		reflect.TypeFor[Pair](): {
			Type:                 "object",
			MinProperties:        ptr(1),
			MaxProperties:        ptr(1),
			AdditionalProperties: &jsonschema.Schema{},
			Description: "One pair, written as `- key: value`. Exactly one key per list item: " +
				"an item holding two is a dropped indent and is refused with its line.",
		},
	}}
	// Both hooks below are derived before they are registered, because each one
	// publishes a shape that includes the reflected struct: registering first
	// would make the derivation read its own hook.
	field, err := fieldSchema(options)
	if err != nil {
		return nil, err
	}
	options.TypeSchemas[reflect.TypeFor[Field]()] = field
	item, err := taskItemSchema(options)
	if err != nil {
		return nil, err
	}
	options.TypeSchemas[reflect.TypeFor[TaskItem]()] = item
	return jsonschema.For[Package](options)
}

// fieldSchema publishes the two shapes one item of a shape's `fields:` may
// take. Reflection sees Name, Type and Description and would publish only the
// long form, and the long form is the one an author almost never writes: a
// field is `- scheduled_date: Date` unless it wants a description.
func fieldSchema(options *jsonschema.ForOptions) (*jsonschema.Schema, error) {
	long, err := jsonschema.For[Field](options)
	if err != nil {
		return nil, err
	}
	return &jsonschema.Schema{OneOf: []*jsonschema.Schema{
		{
			Type:                 "object",
			MinProperties:        ptr(1),
			MaxProperties:        ptr(1),
			AdditionalProperties: &jsonschema.Schema{Type: "string"},
			Description: "One field, written `- scheduled_date: Date`. Exactly one key per list item, " +
				"the field's name to its type expression: an item holding two is a dropped indent " +
				"and is refused with its line.",
		},
		long,
	}}, nil
}

// taskItemSchema publishes the two shapes an agent's `tasks:` item may take.
// Reflection sees Ref and Task and would publish neither, because both are what
// UnmarshalYAML fills rather than what anybody writes.
func taskItemSchema(options *jsonschema.ForOptions) (*jsonschema.Schema, error) {
	task, err := jsonschema.For[Task](options)
	if err != nil {
		return nil, err
	}
	return &jsonschema.Schema{OneOf: []*jsonschema.Schema{
		{Type: "string", Description: "The name of a task another agent defines."},
		task,
	}}, nil
}

// ptr is the one-line address-of that jsonschema's *int fields need. Nothing in
// this package needed one before.
func ptr[T any](value T) *T { return &value }
