package cli

import (
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

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
