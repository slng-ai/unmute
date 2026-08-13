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
		},
	})
}
