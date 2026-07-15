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
	} {
		if !strings.Contains(text, want) {
			t.Errorf("derived schema missing %s", want)
		}
	}
}
