package spec

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

// Schema derives the authoring-package schema at runtime.
func Schema() (*jsonschema.Schema, error) {
	return jsonschema.For[Package](&jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{
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
		},
	})
}

// ptr is the one-line address-of that jsonschema's *int fields need. Nothing in
// this package needed one before.
func ptr[T any](value T) *T { return &value }
