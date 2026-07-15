package spec

import "github.com/google/jsonschema-go/jsonschema"

// Schema derives the authoring-package schema at runtime.
func Schema() (*jsonschema.Schema, error) {
	return jsonschema.For[Package](nil)
}
