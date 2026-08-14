package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
)

// --var is the local stand-in for the dispatch payload: values are parsed
// against their declared type, and an undeclared name is refused rather than
// accepted and dropped (variable_secrets_specs.md V13).
func TestCallStartPayload(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "outbound-reminder"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("declared names encode as JSON", func(t *testing.T) {
		got, err := callStartPayload(agent, []string{"name=Ada", "customer_id=cus_1042"})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"name":"Ada"`, `"customer_id":"cus_1042"`} {
			if !strings.Contains(got, want) {
				t.Errorf("payload %s missing %s", got, want)
			}
		}
	})

	t.Run("no flags means no payload", func(t *testing.T) {
		got, err := callStartPayload(agent, nil)
		if err != nil || got != "" {
			t.Fatalf("got %q, %v; want empty", got, err)
		}
	})

	for _, tc := range []struct {
		name, flag, want string
	}{
		{"undeclared name", "nickname=Ada", `no variable "nickname" is declared`},
		{"missing equals", "name", "must be name=value"},
		{"runtime-owned source", "dialed_number=+15551234567", "the runtime supplies it"},
		{"conversation source", "reschedule_to=friday at 3 pm", "saves it mid-call through update_variables"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := callStartPayload(agent, []string{tc.flag})
			if err == nil {
				t.Fatalf("--var %s must be refused", tc.flag)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// Each declared type parses to its JSON counterpart, so a bot expecting a number
// never receives a quoted string.
func TestParseVarValue(t *testing.T) {
	for _, tc := range []struct {
		kind ir.PrimitiveType
		raw  string
		want any
	}{
		{ir.PrimitiveString, "Ada", "Ada"},
		{ir.PrimitiveInteger, "42", 42},
		{ir.PrimitiveNumber, "1.5", 1.5},
		{ir.PrimitiveBoolean, "true", true},
	} {
		got, err := parseVarValue(tc.kind, tc.raw)
		if err != nil || got != tc.want {
			t.Errorf("parseVarValue(%s, %q) = %v, %v; want %v", tc.kind, tc.raw, got, err, tc.want)
		}
	}
	if _, err := parseVarValue(ir.PrimitiveInteger, "many"); err == nil {
		t.Error("a non-integer must be refused for an integer variable")
	}
}
