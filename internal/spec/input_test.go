package spec

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// decodeTaskInput decodes a task carrying `input:` the way Load decodes
// agent.yaml: strict, so a test cannot pass by being decoded more loosely than
// the compiler decodes.
func decodeTaskInput(t *testing.T, source string) (Task, error) {
	t.Helper()
	var task Task
	err := yaml.UnmarshalWithOptions([]byte(source), &task, yaml.Strict())
	return task, err
}

// TestInputDecodesBothFormsAndRefusesAMalformedItem is FR-001 and FR-002 at the
// decoder: `input:` on a task and on a handoff takes the two forms a shape's
// `fields:` take, through the one decoder that exists, so every refusal that
// decoder carries reaches the new key with no new code.
func TestInputDecodesBothFormsAndRefusesAMalformedItem(t *testing.T) {
	task, err := decodeTaskInput(t, `name: manage_booking
instructions: tasks/booking.md
input:
  - action: Literal["create", "modify", "cancel"]
  - name: service
    type: Literal["haircut", "haircolor"] | None
    description: Only what the caller said.
result:
  summary: string
`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Field{
		{Name: "action", Type: `Literal["create", "modify", "cancel"]`},
		{Name: "service", Type: `Literal["haircut", "haircolor"] | None`, Description: "Only what the caller said."},
	}
	if len(task.Input) != len(want) {
		t.Fatalf("decoded %d inputs, want %d: %+v", len(task.Input), len(want), task.Input)
	}
	for i := range want {
		if task.Input[i] != want[i] {
			t.Errorf("input %d = %+v, want %+v", i, task.Input[i], want[i])
		}
	}

	var handoff Handoff
	if err := yaml.UnmarshalWithOptions([]byte("to: specialist\ninput:\n  - problem: str\n"), &handoff, yaml.Strict()); err != nil {
		t.Fatal(err)
	}
	if len(handoff.Input) != 1 || handoff.Input[0] != (Field{Name: "problem", Type: "str"}) {
		t.Errorf("handoff input = %+v, want problem: str", handoff.Input)
	}

	for _, tc := range []struct {
		name, source, want string
		line               int
	}{
		{
			name:   "two keys and no name is a dropped indent",
			source: "name: t\ninstructions: t.md\ninput:\n  - action: str\n    service: str\nresult:\n  summary: string\n",
			want:   "holding 2 keys", line: 4,
		},
		{
			name:   "an empty item",
			source: "name: t\ninstructions: t.md\ninput:\n  - {}\nresult:\n  summary: string\n",
			want:   "an empty field", line: 4,
		},
		{
			name:   "confirm belongs to a variable",
			source: "name: t\ninstructions: t.md\ninput:\n  - name: phone\n    type: Phone\n    confirm: verify\nresult:\n  summary: string\n",
			want:   `declares "confirm:"`, line: 6,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeTaskInput(t, tc.source)
			if err == nil {
				t.Fatal("decoded, want a refusal")
			}
			var refusal *PairError
			if !errors.As(err, &refusal) {
				t.Fatalf("error is %T, want *PairError carrying the line: %v", err, err)
			}
			if !strings.Contains(refusal.Msg, tc.want) {
				t.Errorf("refusal %q does not say %q", refusal.Msg, tc.want)
			}
			if refusal.Line != tc.line {
				t.Errorf("refusal names line %d, want %d", refusal.Line, tc.line)
			}
		})
	}
}

// TestInputIsOptionalInTheDerivedSchema holds the schema side: `input` is
// published on a task and on a handoff, in the two forms fieldSchema
// publishes, and neither struct requires it, because a step or a handoff with
// no input is the common case.
func TestInputIsOptionalInTheDerivedSchema(t *testing.T) {
	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	// The two structs are found by the keys only they carry, wherever the
	// deriver placed them, so this does not depend on how definitions are laid
	// out. A task is the object with `result` and `input`; a handoff the one
	// with `to` and `input`.
	found := map[string]bool{}
	var walk func(node any)
	walk = func(node any) {
		switch node := node.(type) {
		case []any:
			for _, item := range node {
				walk(item)
			}
		case map[string]any:
			properties, _ := node["properties"].(map[string]any)
			if _, input := properties["input"]; input {
				kind := ""
				if _, ok := properties["result"]; ok {
					kind = "Task"
				} else if _, ok := properties["to"]; ok {
					kind = "Handoff"
				}
				if kind != "" {
					found[kind] = true
					if required, _ := node["required"].([]any); slices.Contains(required, any("input")) {
						t.Errorf("%s requires input, and a step with none is the common case", kind)
					}
				}
			}
			for _, value := range node {
				walk(value)
			}
		}
	}
	walk(decoded)
	for _, kind := range []string{"Task", "Handoff"} {
		if !found[kind] {
			t.Errorf("the derived schema publishes no input on %s", kind)
		}
	}
}
