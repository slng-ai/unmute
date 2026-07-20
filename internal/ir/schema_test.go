package ir

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSchemaDerivesUnionEnumsAndNameReferences(t *testing.T) { // V2
	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{
		`"oneOf"`,
		`"const":"delegate"`,
		`"enum":["string","number","boolean","integer"]`,
		`"fallback"`,
		`"items":{"type":"string"}`,
		`"destinations"`,
		`"url_env"`,
		`"tracing"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("derived schema missing %s", want)
		}
	}
}

func TestResultFieldSchemaAcceptsEveryResultShape(t *testing.T) {
	schema, err := resultFieldSchema()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{
		map[string]any{"type": "boolean"},
		map[string]any{"type": "string", "enum": []any{"a", "b"}},
		map[string]any{"schema": map[string]any{"type": "object"}},
	} {
		if err := resolved.Validate(value); err != nil {
			t.Errorf("%v: %v", value, err)
		}
	}
}
