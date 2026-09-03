package cli

import (
	"strings"
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

// --source is the local stand-in for a caller ID, and it is a separate flag from
// --var on purpose. This is the gate on that: --var writes a variable, --source
// writes a call fact, and the pre-fetch reads the fact. Seeding the variable
// directly would skip the pre-fetch, mark nothing as awaiting confirmation, and
// let a local run book an appointment without ever reading a number back, which
// is a local path passing where a real call fails (research R8).
func TestCallFactsPayload(t *testing.T) {
	t.Run("a call fact is accepted", func(t *testing.T) {
		got, err := callFactsPayload([]string{"from_number=+34600111222"})
		if err != nil {
			t.Fatalf("callFactsPayload: %v", err)
		}
		if got != `{"from_number":"+34600111222"}` {
			t.Errorf("payload = %s", got)
		}
	})

	t.Run("no flags is no payload", func(t *testing.T) {
		if got, err := callFactsPayload(nil); err != nil || got != "" {
			t.Errorf("callFactsPayload(nil) = %q, %v; want an empty payload and no error", got, err)
		}
	})

	for _, tc := range []struct {
		name string
		flag string
		want string
	}{
		{"not name=value", "from_number", "must be name=value"},
		{"not a call fact", "caller_name=Ada", "not a fact a call carries"},
		{"the model's own", "conversation=x", "the model saves mid-call"},
		{"the dispatch payload", "call_start=x", "seed it with --var"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := callFactsPayload([]string{tc.flag})
			if err == nil {
				t.Fatalf("--source %s was accepted", tc.flag)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not say %q: %v", tc.want, err)
			}
		})
	}
}

// --var seeds the dispatch payload, and the set it accepts is the set the two
// drivers hydrate from it: `source: call_start`, and a variable declaring no
// source at all. Both emitted runbooks print a `--var <name>=...` line for
// every one of them, so a name the runbook prints and the flag refuses is a
// runbook teaching a command that does not work. The refusal for a sourceless
// variable also named `update_variables`, which is generated over
// `source: conversation` only, so it described a mechanism that could not have
// written the value either.
func TestCallStartPayload(t *testing.T) {
	agent := &ir.Agent{Variables: map[string]ir.Variable{
		"dispatched":     {Type: ir.PrimitiveString, Source: ir.VariableSourceCallStart},
		"sourceless":     {Type: ir.PrimitiveString},
		"mid_call":       {Type: ir.PrimitiveString, Source: ir.VariableSourceConversation},
		"from_the_route": {Type: ir.PrimitiveString, Source: ir.VariableSourceFromNumber},
	}}

	for _, name := range []string{"dispatched", "sourceless"} {
		got, err := callStartPayload(agent, []string{name + "=Ada"})
		if err != nil {
			t.Errorf("--var %s: %v; both drivers hydrate this variable off the dispatch payload and both runbooks print the flag for it", name, err)
			continue
		}
		if got != `{"`+name+`":"Ada"}` {
			t.Errorf("--var %s payload = %s", name, got)
		}
	}

	for _, tc := range []struct {
		name string
		flag string
		want string
	}{
		{"not name=value", "dispatched", "must be name=value"},
		{"undeclared", "nobody=Ada", "no variable"},
		{"the model's own", "mid_call=Ada", ir.CaptureToolName},
		{"the route's own", "from_the_route=Ada", "the runtime supplies it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := callStartPayload(agent, []string{tc.flag})
			if err == nil {
				t.Fatalf("--var %s was accepted", tc.flag)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not say %q: %v", tc.want, err)
			}
			// A refusal that reaches this point has a source to name, so it can
			// never render the empty one the sourceless branch used to print.
			if strings.Contains(err.Error(), "has source ,") {
				t.Errorf("the refusal names an empty source: %v", err)
			}
		})
	}
}

// Every fact --source accepts is one the compiler agrees is a call fact. Two
// lists of the same eight names is two lists that drift.
func TestCallFactNamesMatchTheCompiler(t *testing.T) {
	for _, name := range callFactNames() {
		if !ir.IsSystemSource(ir.VariableSource(name)) {
			t.Errorf("--source offers %q, which the compiler does not treat as a call fact", name)
		}
	}
	if len(callFactNames()) != 8 {
		t.Errorf("--source offers %d facts; the compiler has eight runtime-owned sources", len(callFactNames()))
	}
}
