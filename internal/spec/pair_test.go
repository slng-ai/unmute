package spec

import (
	"errors"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// decodePairs runs one `assign:` block through the real decoder, so these tests
// exercise the same path a package file takes rather than calling UnmarshalYAML
// directly. The wrapper is what proves PairError survives goccy's own wrapping,
// which is what Package.decode relies on to print a file and a line.
func decodePairs(t *testing.T, source string) ([]Pair, error) {
	t.Helper()
	var out struct {
		Assign []Pair `yaml:"assign"`
	}
	err := yaml.UnmarshalWithOptions([]byte(source), &out, yaml.Strict())
	return out.Assign, err
}

func TestPairDecodesOneKeyPerItem(t *testing.T) {
	pairs, err := decodePairs(t, "assign:\n  - customer_phone: result.value\n  - customer_name: result.name\n")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(pairs) != 2 {
		t.Fatalf("got %d pairs, want 2: %+v", len(pairs), pairs)
	}
	// Order is the authored order, which is the whole reason this is a list.
	if pairs[0].Key != "customer_phone" || pairs[0].Value != "result.value" {
		t.Errorf("first pair = %+v", pairs[0])
	}
	if pairs[1].Key != "customer_name" || pairs[1].Value != "result.name" {
		t.Errorf("second pair = %+v", pairs[1])
	}
}

// A scalar of each primitive type, because `args:` values carry the tool's own
// input types and a pair that only accepted strings would refuse a legal package.
func TestPairDecodesEveryScalarType(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		want   any
	}{
		{"string", "assign:\n  - a: hello\n", "hello"},
		{"quoted string", "assign:\n  - a: \"42\"\n", "42"},
		{"integer", "assign:\n  - a: 42\n", uint64(42)},
		{"negative integer", "assign:\n  - a: -7\n", int64(-7)},
		{"float", "assign:\n  - a: 1.5\n", 1.5},
		{"bool", "assign:\n  - a: true\n", true},
		{"null", "assign:\n  - a:\n", nil},
		{"template", "assign:\n  - phone: \"{{customer_phone}}\"\n", "{{customer_phone}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pairs, err := decodePairs(t, tc.source)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(pairs) != 1 {
				t.Fatalf("got %d pairs, want 1", len(pairs))
			}
			if pairs[0].Value != tc.want {
				t.Errorf("value = %#v, want %#v", pairs[0].Value, tc.want)
			}
		})
	}
}

// D1, the refusal a map[string]string could not give. A dropped indent produces
// one item holding two keys, and the point of the message is that it names both
// of them and says what to write instead.
func TestPairRefusesTwoKeysInOneItem(t *testing.T) {
	_, err := decodePairs(t, "assign:\n  - customer_name: result.name\n    customer_id: result.id\n")
	if err == nil {
		t.Fatal("want a refusal for an item holding two keys, got none")
	}
	var pair *PairError
	if !errors.As(err, &pair) {
		t.Fatalf("want a *PairError so the file and line can be joined, got %T: %v", err, err)
	}
	if pair.Line != 2 {
		t.Errorf("line = %d, want 2 (the item's own line)", pair.Line)
	}
	for _, want := range []string{"customer_name", "customer_id", "One item, one pair"} {
		if !strings.Contains(pair.Msg, want) {
			t.Errorf("message does not mention %q: %s", want, pair.Msg)
		}
	}
}

// D1's other half. An item with no key at all is the same class of mistake, and
// the message says what one looks like rather than only what is wrong.
func TestPairRefusesAnItemWithNoKey(t *testing.T) {
	_, err := decodePairs(t, "assign:\n  - {}\n")
	if err == nil {
		t.Fatal("want a refusal for an empty item, got none")
	}
	var pair *PairError
	if !errors.As(err, &pair) {
		t.Fatalf("want a *PairError, got %T: %v", err, err)
	}
	if !strings.Contains(pair.Msg, `- name: value`) {
		t.Errorf("message does not say what to write instead: %s", pair.Msg)
	}
}

// A pair value is one scalar. A nested mapping here is refused at decode rather
// than reaching Build as something no pair value can be.
func TestPairRefusesANestedValue(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"mapping", "assign:\n  - a:\n      b: c\n"},
		{"list", "assign:\n  - a:\n      - b\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodePairs(t, tc.source)
			if err == nil {
				t.Fatal("want a refusal, got none")
			}
			var pair *PairError
			if !errors.As(err, &pair) {
				t.Fatalf("want a *PairError, got %T: %v", err, err)
			}
			if !strings.Contains(pair.Msg, "one scalar") {
				t.Errorf("message does not say a pair value is one scalar: %s", pair.Msg)
			}
		})
	}
}

// The file name belongs to the caller, so Package.decode is what joins it to the
// line. This asserts the joined form, because that is what an author reads.
func TestPairErrorReachesTheAuthorWithAFileAndLine(t *testing.T) {
	pkg := &Package{files: map[string][]byte{}}
	var out struct {
		Assign []Pair `yaml:"assign"`
	}
	err := pkg.decode("agent.yaml", []byte("assign:\n  - a: x\n    b: y\n"), &out)
	if err == nil {
		t.Fatal("want a refusal, got none")
	}
	if !strings.HasPrefix(err.Error(), "agent.yaml:2: ") {
		t.Errorf("error does not start with the file and line: %s", err)
	}
}
