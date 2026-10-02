package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/stateschema"
)

// TestPlaceholderPathIsWalkedOnBothTargets is spec 005 US1 and US4 at emission:
// the fixture's prompt names {{state.last_appointment.appointment_type}}, and
// both modules carry it as one flat name, walk it through CallState.lookup in
// the local renderer, and send the same flat name to the router with its live
// value.
func TestPlaceholderPathIsWalkedOnBothTargets(t *testing.T) {
	agent := loadTypedState(t)
	if got := agent.Agents["desk"].Instructions; !strings.Contains(got, "{{last_appointment__appointment_type}}") {
		t.Fatalf("the IR does not carry the path flat:\n%s", got)
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		for _, want := range []string{
			"    def lookup(self, flat: str) -> tuple[str, object]:",
			`root, _, path = flat.partition("__")`,
			"CallState.render(",
			"{{last_appointment__appointment_type}}",
			`"last_appointment__appointment_type"`,
			// A path into a model the author declares flattens and walks exactly
			// like any other. A reference to a field the model does not have is
			// refused at build, so a prompt can name one part of the pair.
			"{{booked_for__name}}",
			`"booked_for__name"`,
		} {
			if !strings.Contains(module, want) {
				t.Errorf("%s does not emit %q", provider, want)
			}
		}
		if strings.Contains(module, "{{last_appointment.appointment_type}}") {
			t.Errorf("%s emits the authored dotted token, which no render path substitutes", provider)
		}
		// The walk reads None past an absent link rather than raising, which is
		// what lets a prompt name a field of a record nobody has filled.
		body := functionBody(t, module, "    def lookup(self, flat: str)")
		if !strings.Contains(body, "if value is None:") || !strings.Contains(body, "getattr(value, part, None)") {
			t.Errorf("%s: the walk does not stop at an absent link:\n%s", provider, body)
		}
	}
}

// TestInjectOfAPathLowersToThePlainRead is spec 005 US3: a single-token inject
// naming a part reads it through state.plain, keeping the part's own type, and
// the unset guard names the whole record.
func TestInjectOfAPathLowersToThePlainRead(t *testing.T) {
	// The state holds an object as a model, so a part of one leaves as plain
	// data, and so does the whole object; a plain value is the attribute read
	// it always was.
	variables := map[string]ir.Variable{
		"customer":  {Schema: &stateschema.Type{Kind: stateschema.KindObject, Model: "Customer", Nullable: true}},
		"caller_id": {Type: ir.PrimitiveString, Schema: &stateschema.Type{Kind: stateschema.KindString}},
	}
	if got := injectExpr("{{customer__status}}", "ctx.userdata", variables); got != `ctx.userdata.plain("customer__status")` {
		t.Errorf("injectExpr(path) = %s", got)
	}
	if got := injectExpr("{{customer}}", "ctx.userdata", variables); got != `ctx.userdata.plain("customer")` {
		t.Errorf("injectExpr(whole object) = %s, which sends a model no JSON body accepts", got)
	}
	if got := injectExpr("{{caller_id}}", "ctx.userdata", variables); got != "ctx.userdata.caller_id" {
		t.Errorf("injectExpr(plain value) = %s, which is not the attribute read it always was", got)
	}
	needed := neededVars(ir.Tool{Inject: map[string]any{"status": "{{customer__status}}", "phone": "{{customer__phone_number}}"}},
		map[string]ir.Variable{"customer": {Description: "The record the lookup returned."}}, nil)
	if len(needed) != 2 || needed[0].Name != "customer__phone_number" || needed[1].Name != "customer__status" {
		t.Errorf("neededVars = %+v, want the two exact referenced fields", needed)
	}
}
