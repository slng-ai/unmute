package spec

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// decodeFinish runs one `finish:` block through the real decoder, for the same
// reason decodePairs does: this is the path a package file takes, and it is what
// proves a PairError survives goccy's own wrapping.
func decodeFinish(t *testing.T, source string) ([]FinishEntry, error) {
	t.Helper()
	var out struct {
		Finish []FinishEntry `yaml:"finish"`
	}
	err := yaml.UnmarshalWithOptions([]byte(source), &out, yaml.Strict())
	return out.Finish, err
}

func TestFinishEntryDecodes(t *testing.T) {
	entries, err := decodeFinish(t, `finish:
  - tool: create_booking
    success:
      - status: booked
  - tool: cancel_booking
    success:
      - status: [cancelled, already_cancelled]
      - refunded: true
`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}
	if entries[0].Tool != "create_booking" {
		t.Errorf("first tool = %q", entries[0].Tool)
	}
	if len(entries[0].Success) != 1 || entries[0].Success[0].Field != "status" {
		t.Fatalf("first success = %+v", entries[0].Success)
	}
	// One scalar reads as a one-item list, so the emitted check has one shape.
	if got := entries[0].Success[0].Values; len(got) != 1 || got[0] != "booked" {
		t.Errorf("first success values = %v, want [booked]", got)
	}
	// Several values on one field are alternatives, in the authored order.
	if got := entries[1].Success[0].Values; len(got) != 2 || got[0] != "cancelled" || got[1] != "already_cancelled" {
		t.Errorf("alternatives = %v, want [cancelled already_cancelled]", got)
	}
	// A non-string scalar is kept as its written form; the enum comparison is
	// ir.Build's job and it reads the tool's declared values as strings.
	if got := entries[1].Success[1].Values; len(got) != 1 || got[0] != "true" {
		t.Errorf("bool value = %v, want [true]", got)
	}
}

// A dropped indent produces one item holding two keys, which is the mistake this
// decoder exists to catch. The line is most of what makes the refusal useful.
func TestSuccessPairRefusesTwoKeysWithItsLine(t *testing.T) {
	_, err := decodeFinish(t, `finish:
  - tool: create_booking
    success:
      - status: booked
        refunded: false
`)
	if err == nil {
		t.Fatal("a success item holding two keys must be refused")
	}
	var pair *PairError
	if !errors.As(err, &pair) {
		t.Fatalf("want a PairError carrying the line, got %T: %v", err, err)
	}
	if pair.Line != 4 {
		t.Errorf("line = %d, want 4", pair.Line)
	}
	for _, want := range []string{"status", "refunded", "One item, one field"} {
		if !strings.Contains(pair.Msg, want) {
			t.Errorf("message %q does not name %q", pair.Msg, want)
		}
	}
}

func TestSuccessPairRefusesTheShapesThatCannotBeChecked(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "empty item",
			source: "finish:\n  - tool: t\n    success:\n      - {}\n",
			want:   "empty success item",
		},
		{
			name:   "empty list",
			source: "finish:\n  - tool: t\n    success:\n      - status: []\n",
			want:   "lists no value",
		},
		{
			name:   "nested mapping",
			source: "finish:\n  - tool: t\n    success:\n      - status:\n          code: booked\n",
			want:   "one scalar or a list of scalars",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodeFinish(t, tc.source); err == nil {
				t.Fatal("want a refusal")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("message %q does not say %q", err, tc.want)
			}
		})
	}
}

// The console rewrites agent.yaml from what it decoded, so a value written as a
// bare scalar has to come back as one. A list that came back as a one-item list
// would reshape every file the console touched.
func TestSuccessPairWritesBackWhatWasAuthored(t *testing.T) {
	entries, err := decodeFinish(t, "finish:\n  - tool: t\n    success:\n      - status: booked\n      - kind: [a, b]\n")
	if err != nil {
		t.Fatal(err)
	}
	out, err := yaml.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "status: booked") {
		t.Errorf("a single value came back as a list:\n%s", out)
	}
	if !strings.Contains(string(out), "- a") {
		t.Errorf("a list of values did not come back as a list:\n%s", out)
	}
}

// The derived authoring schema is what a coding agent and the editor read, so a
// key that compiles and is not published is a key nobody discovers.
//
// It walks lists as well as mappings, which searchSchema does not: a task is
// published under a `oneOf`, because a task item is a bare name or a mapping,
// and a walk that stops at a list never reaches a single task field.
func TestAuthoringSchemaPublishesFinish(t *testing.T) {
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
	finish := searchAnywhere(decoded, "finish")
	if finish == nil {
		t.Fatal("the derived authoring schema publishes no finish property")
	}
	for _, name := range []string{"tool", "success"} {
		if searchAnywhere(finish, name) == nil {
			t.Errorf("the derived authoring schema publishes no %q property under finish", name)
		}
	}
}

func searchAnywhere(node any, name string) any {
	switch value := node.(type) {
	case map[string]any:
		if properties, ok := value["properties"].(map[string]any); ok {
			if found, ok := properties[name]; ok {
				return found
			}
		}
		for _, child := range value {
			if found := searchAnywhere(child, name); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range value {
			if found := searchAnywhere(child, name); found != nil {
				return found
			}
		}
	}
	return nil
}

// decodeSteps runs one `steps:` block through the real decoder.
func decodeSteps(t *testing.T, source string) ([]StepItem, error) {
	t.Helper()
	var out struct {
		Steps []StepItem `yaml:"steps"`
	}
	err := yaml.UnmarshalWithOptions([]byte(source), &out, yaml.Strict())
	return out.Steps, err
}

// A bare name is a step that always runs; an item says how the group treats it.
// Both shapes, so a group written before this key reads exactly as it did.
func TestStepItemTakesABareNameOrAnItem(t *testing.T) {
	steps, err := decodeSteps(t, `steps:
  - task: verify_customer
    skip_when_confirmed: customer_phone
  - manage_booking
`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("got %d steps, want 2: %+v", len(steps), steps)
	}
	if steps[0].Task != "verify_customer" || steps[0].SkipWhenConfirmed != "customer_phone" {
		t.Errorf("first step = %+v", steps[0])
	}
	if steps[1].Task != "manage_booking" || steps[1].SkipWhenConfirmed != "" {
		t.Errorf("second step = %+v", steps[1])
	}
	// The console rewrites agent.yaml from what it decoded, so a bare name has
	// to come back bare or every group file it touches is reshaped.
	out, err := yaml.Marshal(steps)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "- manage_booking") {
		t.Errorf("a bare step came back as an item:\n%s", out)
	}
}

func TestStepItemRefusesAnItemWithNoTask(t *testing.T) {
	_, err := decodeSteps(t, "steps:\n  - skip_when_confirmed: customer_phone\n")
	if err == nil {
		t.Fatal("an item naming no task must be refused")
	}
	var pair *PairError
	if !errors.As(err, &pair) {
		t.Fatalf("want a PairError carrying the line, got %T: %v", err, err)
	}
	if pair.Line != 2 {
		t.Errorf("line = %d, want 2", pair.Line)
	}
	if !strings.Contains(pair.Msg, "task name or an item") {
		t.Errorf("message %q does not say what to write", pair.Msg)
	}
}
