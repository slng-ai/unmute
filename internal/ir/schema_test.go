package ir

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	targetcap "github.com/slng-ai/unmute/internal/target"
)

// TestProviderEnumMatchesTargetSet closes the one surface in this repository
// where a stale provider list produced a wrong artifact rather than wrong prose.
//
// enumOptions hand-writes the Provider enum for the debug schema, and until this
// test existed nothing compared it to target.Providers. Forgetting to add a
// provider here produced a derived schema that rejected the target the compiler
// had just accepted, on a green CI run, because no other check reads this list.
// Principle III asks for an agreement test wherever a fact has a second owner.
func TestProviderEnumMatchesTargetSet(t *testing.T) {
	schema := enumOptions().TypeSchemas[reflect.TypeFor[Provider]()]
	if schema == nil {
		t.Fatal("the Provider type has no enum schema, so the debug schema takes any string")
	}
	var got []string
	for _, value := range schema.Enum {
		name, ok := value.(Provider)
		if !ok {
			t.Fatalf("enum entry %#v is not an ir.Provider", value)
		}
		got = append(got, string(name))
	}
	var want []string
	for _, provider := range targetcap.Providers {
		want = append(want, string(provider))
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("debug schema provider enum = %v, target.Providers = %v", got, want)
	}
}

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
		`"enum":["bearer","api_key"]`, // webhook auth scheme (C2: every enum type gets a TypeSchemas override)
		`"fallback"`,
		`"items":{"type":"string"}`,
		`"destinations"`,
		`"url_env"`,
		`"tracing"`,
		`"connections"`,
		`"peak_starts_per_second"`,
		`"session_id"`,
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
