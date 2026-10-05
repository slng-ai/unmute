package spec

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func decodeVariables(source string) (Variables, error) {
	var out struct {
		Variables Variables `yaml:"variables"`
	}
	err := yaml.UnmarshalWithOptions([]byte(source), &out, yaml.Strict())
	return out.Variables, err
}

func TestVariablesDecodeAsAnOrderedList(t *testing.T) {
	got, err := decodeVariables("variables:\n  - name: caller_phone\n    source: from_number\n    confirm: verify\n  - name: today\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (Variable{Name: "caller_phone", Source: "from_number", Confirm: "verify"}) || got[1].Name != "today" {
		t.Fatalf("variables = %+v", got)
	}
	if v, ok := got.Named("caller_phone"); !ok || v.Confirm != "verify" {
		t.Errorf("Named(caller_phone) = %+v, %v", v, ok)
	}
}

// The name-keyed form and the keys that moved to state.py are refused with the
// place to put them, not with a strict decoder's "unknown field".
func TestVariablesRefuseTheOldFormsByName(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"the map form", "variables:\n  caller_phone:\n    type: str\n", "variables: is a list now"},
		{"type", "variables:\n  - name: a\n    type: str\n", "no type: any more"},
		{"default", "variables:\n  - name: a\n    default: x\n", "no default: any more"},
		{"description", "variables:\n  - name: a\n    description: x\n", "no description: any more"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeVariables(tc.source)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one saying %q", err, tc.want)
			}
		})
	}
}
