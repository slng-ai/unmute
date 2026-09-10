package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// TestPlaceholderPathIsWalkedOnBothTargets is spec 005 US1 and US4 at emission:
// the fixture's prompt names {{last_appointment.appointment_type}}, and both
// modules carry it as one flat name, walk it through the shared lookup in the
// local renderer, and send the same flat name to the router with its live value.
func TestPlaceholderPathIsWalkedOnBothTargets(t *testing.T) {
	agent := loadTypedState(t)
	if got := agent.Agents["desk"].Instructions; !strings.Contains(got, "{{last_appointment__appointment_type}}") {
		t.Fatalf("the IR does not carry the path flat:\n%s", got)
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		for _, want := range []string{
			"def _state_lookup(state, name):",
			`root, _, path = name.partition("__")`,
			"_state_text(*_prompt_value(",
			"{{last_appointment__appointment_type}}",
			`"last_appointment__appointment_type"`,
			// A path into a shape the compiler supplies flattens and walks
			// exactly like one into a declared shape. It only can because
			// internal/ir seeds the shape into the catalog: a reference with no
			// entry there is refused at build with "which has no fields to
			// name", so a prompt could never read one part of the pair.
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
		body := functionBody(t, module, "def _state_lookup(state, name):")
		if !strings.Contains(body, "if value is None:") || !strings.Contains(body, "isinstance(value, dict)") {
			t.Errorf("%s: the walk does not stop at an absent link:\n%s", provider, body)
		}
	}
}

// TestInjectOfAPathLowersToTheLookup is spec 005 US3: a single-token inject
// naming a part reads it through the lookup, keeping the part's own type, and
// the unset guard names the whole record.
func TestInjectOfAPathLowersToTheLookup(t *testing.T) {
	if got := injectExpr("{{customer__status}}", "ctx.userdata"); got != `_state_lookup(ctx.userdata, "customer__status")[1]` {
		t.Errorf("injectExpr(path) = %s", got)
	}
	if got := injectExpr("{{customer}}", "ctx.userdata"); got != "ctx.userdata.customer" {
		t.Errorf("injectExpr(whole value) = %s, which is not the attribute read it always was", got)
	}
	needed := neededVars(ir.Tool{Inject: map[string]any{"status": "{{customer__status}}", "phone": "{{customer__phone_number}}"}},
		map[string]ir.Variable{"customer": {Description: "The record the lookup returned."}}, nil)
	if len(needed) != 2 || needed[0].Name != "customer__phone_number" || needed[1].Name != "customer__status" {
		t.Errorf("neededVars = %+v, want the two exact referenced fields", needed)
	}
}
