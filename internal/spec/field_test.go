package spec

import (
	"errors"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// decodeFields decodes a `fields:` list the way Load decodes agent.yaml: one
// call site, strict, so a test cannot pass by being decoded more loosely than
// the compiler decodes.
func decodeFields(t *testing.T, source string) ([]Field, error) {
	t.Helper()
	var out struct {
		Fields []Field `yaml:"fields"`
	}
	err := yaml.UnmarshalWithOptions([]byte(source), &out, yaml.Strict())
	return out.Fields, err
}

// TestFieldDecodesBothForms holds FR-002b: a field is one line, and only a
// field wanting a description grows a block.
func TestFieldDecodesBothForms(t *testing.T) {
	fields, err := decodeFields(t, `fields:
  - scheduled_date: Date
  - appointment_type: Literal["haircut", "dry_cut"]
  - name: calling_reason
    type: Literal["create_booking", "cancel_booking"]
    description: Why this particular appointment is being touched.
`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Field{
		{Name: "scheduled_date", Type: "Date"},
		{Name: "appointment_type", Type: `Literal["haircut", "dry_cut"]`},
		{
			Name:        "calling_reason",
			Type:        `Literal["create_booking", "cancel_booking"]`,
			Description: "Why this particular appointment is being touched.",
		},
	}
	if len(fields) != len(want) {
		t.Fatalf("decoded %d fields, want %d: %+v", len(fields), len(want), fields)
	}
	for i := range want {
		if fields[i] != want[i] {
			t.Errorf("field %d = %+v, want %+v", i, fields[i], want[i])
		}
	}
}

// TestFieldRefusesTheShapesADroppedIndentMakes is why Field carries its own
// decoder rather than being a map: a map would have accepted every one of these
// and lost what the author meant.
func TestFieldRefusesTheShapesADroppedIndentMakes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		line   int
		phrase string
	}{
		{
			name: "two short fields written as one item",
			source: `fields:
  - scheduled_date: Date
    scheduled_time: Time
`,
			line:   2,
			phrase: "holding 2 keys",
		},
		{
			name: "an item with no keys",
			source: `fields:
  - {}
`,
			line:   2,
			phrase: "an empty field",
		},
		{
			name: "a bare name with no type",
			source: `fields:
  - scheduled_date
`,
			line:   2,
			phrase: "must be written",
		},
		{
			name: "a long form with no type",
			source: `fields:
  - name: scheduled_date
    description: The day.
`,
			line:   2,
			phrase: "declares no type",
		},
		{
			name: "a nested value where a type belongs",
			source: `fields:
  - scheduled_date:
      format: iso
`,
			line:   2,
			phrase: "with the type on the same line",
		},
		{
			name: "an unknown key",
			source: `fields:
  - name: scheduled_date
    type: Date
    pattern: '\d{4}'
`,
			line:   4,
			phrase: `unknown key "pattern"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeFields(t, tc.source)
			if err == nil {
				t.Fatalf("decoded, want a refusal")
			}
			var refusal *PairError
			if !errors.As(err, &refusal) {
				t.Fatalf("err = %v, want a *PairError so Load can print the file with the line", err)
			}
			if refusal.Line != tc.line {
				t.Errorf("refused at line %d, want line %d (%s)", refusal.Line, tc.line, refusal.Msg)
			}
			if !strings.Contains(refusal.Msg, tc.phrase) {
				t.Errorf("said %q, want it to contain %q", refusal.Msg, tc.phrase)
			}
		})
	}
}

// TestFieldRefusesConfirmByName is the second half of FR-008a, and it stands
// nowhere else.
//
// `confirm:` belongs to a variable. Written on a field it would let a guard on
// an unconfirmed value be escaped by naming the field one level down, which is
// the whole point of keeping the mark on the value. And a field decoder that
// swallowed the key would make that escape silent, because the custom
// unmarshaler intercepts the decode and the yaml.Strict() set in load.go never
// sees the node.
func TestFieldRefusesConfirmByName(t *testing.T) {
	_, err := decodeFields(t, `fields:
  - name: customer_phone
    type: Phone
    confirm: verify_customer
`)
	if err == nil {
		t.Fatal("a field declaring confirm: decoded, and a guard on the value it sits in would be escapable")
	}
	var refusal *PairError
	if !errors.As(err, &refusal) {
		t.Fatalf("err = %v, want a *PairError", err)
	}
	if refusal.Line != 4 {
		t.Errorf("refused at line %d, want the confirm: line, 4", refusal.Line)
	}
	for _, phrase := range []string{"confirm:", "belongs to the variable", "one level down"} {
		if !strings.Contains(refusal.Msg, phrase) {
			t.Errorf("said %q, want it to contain %q", refusal.Msg, phrase)
		}
	}
}

// TestFieldWritesTheShortFormBack keeps the console round-trip from growing
// every one-line field into a three-line block.
func TestFieldWritesTheShortFormBack(t *testing.T) {
	short, err := yaml.Marshal([]Field{{Name: "scheduled_date", Type: "Date"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(short)); got != "- scheduled_date: Date" {
		t.Errorf("marshalled %q, want %q", got, "- scheduled_date: Date")
	}
	long, err := yaml.Marshal([]Field{{Name: "reason", Type: "str", Description: "Why."}})
	if err != nil {
		t.Fatal(err)
	}
	// name before the type before the description, which is the order the
	// author reads and the order a map would not have kept.
	wantOrder := []string{"name: reason", "type: str", "description: Why."}
	at := -1
	for _, line := range wantOrder {
		next := strings.Index(string(long), line)
		if next <= at {
			t.Fatalf("marshalled the long form as %q, want %v in that order", string(long), wantOrder)
		}
		at = next
	}
}
