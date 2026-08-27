package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// The supplier index is what lets a refusal say more than "something is
// missing". Naming the control that fills the gap is the difference between a
// model that recovers on the same turn and one that asks the caller a question
// it did not need to ask.
func TestSupplierIndex(t *testing.T) {
	t.Run("one supplier", func(t *testing.T) {
		index := SupplierIndex(map[string]ir.Control{
			"verify_customer": &ir.Delegate{Assign: map[string]string{"customer_phone": "result.customer_phone"}},
			"manage_booking":  &ir.Delegate{Requires: []string{"customer_phone"}},
			"to_care":         &ir.AgentTransfer{},
		})
		if index["customer_phone"] != "verify_customer" {
			t.Errorf("supplier of customer_phone = %q, want verify_customer: %v", index["customer_phone"], index)
		}
		if len(index) != 1 {
			t.Errorf("index = %v; only a delegate's assign: fills a variable", index)
		}
	})

	// Two controls can legally assign the same variable. Whichever wins has to
	// win the same way every time, or the emitted Python changes between two
	// compiles of an unchanged package and every golden file becomes a coin
	// toss. Sorted control order is the tiebreak.
	t.Run("several suppliers resolve deterministically", func(t *testing.T) {
		controls := map[string]ir.Control{
			"zulu":  &ir.Delegate{Assign: map[string]string{"phone": "result.phone"}},
			"alpha": &ir.Delegate{Assign: map[string]string{"phone": "result.phone"}},
			"mike":  &ir.Delegate{Assign: map[string]string{"phone": "result.phone"}},
		}
		for range 20 {
			if got := SupplierIndex(controls)["phone"]; got != "alpha" {
				t.Fatalf("supplier of phone = %q, want alpha every time", got)
			}
		}
	})

	t.Run("no supplier when the value comes from elsewhere", func(t *testing.T) {
		index := SupplierIndex(map[string]ir.Control{"to_care": &ir.AgentTransfer{}})
		if _, ok := index["caller_number"]; ok {
			t.Errorf("a variable filled by source: or --var has no supplying control: %v", index)
		}
	})
}

// The forward declaration is the mechanism; the guard is only the net. A model
// that sees the requirement on the tool description collects the value during
// the earlier turns, so the guard is rarely reached and the caller never learns
// there was one.
func TestForwardDeclaration(t *testing.T) {
	suppliers := map[string]string{"customer_phone": "verify_customer"}

	if got := ForwardDeclaration(nil, suppliers); got != "" {
		t.Errorf("an unguarded control gets no sentence, got %q", got)
	}

	got := ForwardDeclaration([]string{"customer_phone"}, suppliers)
	for _, want := range []string{"customer_phone", "verify_customer"} {
		if !strings.Contains(got, want) {
			t.Errorf("forward declaration %q omits %q", got, want)
		}
	}

	// Where nothing supplies the value, because it arrives from source:, from
	// --var or from the carrier, the sentence names the requirement and stops
	// rather than pointing at a control that cannot help.
	got = ForwardDeclaration([]string{"caller_number"}, suppliers)
	if !strings.Contains(got, "caller_number") {
		t.Errorf("forward declaration %q omits the requirement", got)
	}
	if strings.Contains(got, "call ") {
		t.Errorf("forward declaration %q names a supplier for a variable that has none", got)
	}
}

// The generated block is the single owner of the refusal wording. Both drivers
// render this same text, which is why the two targets cannot drift: there is
// only one string to change.
func TestGuardHelperSource(t *testing.T) {
	src := guardHelperSource(map[string]string{"customer_phone": "verify_customer"})

	for _, want := range []string{
		`"customer_phone": "verify_customer"`,
		"_PREREQUISITE_LIMIT = 5",
		"def _unmet_prerequisites(",
		"def _prerequisite_refusal(",
		"Do not say any of this out loud",
		"call this again in the same turn",
		"ask the caller for it directly",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated guard omits %q:\n%s", want, src)
		}
	}
}

// PrerequisiteGuard returning false is what keeps every unguarded package
// byte-identical. A helper emitted into a package that never calls it would be
// dead Python in every project this tool has ever produced.
func TestPrerequisiteGuardOnlyForGuardedPackages(t *testing.T) {
	unguarded := &ir.Agent{Controls: map[string]ir.Control{
		"do_it":   &ir.Delegate{Task: "work"},
		"to_care": &ir.AgentTransfer{To: "care"},
	}}
	if src, needed := PrerequisiteGuard(unguarded); needed || src != "" {
		t.Errorf("a package with no requires: must emit no guard, got needed=%v src=%q", needed, src)
	}

	for name, guarded := range map[string]*ir.Agent{
		"delegate": {Controls: map[string]ir.Control{"do_it": &ir.Delegate{Requires: []string{"phone"}}}},
		"handoff":  {Controls: map[string]ir.Control{"to_care": &ir.AgentTransfer{Requires: []string{"phone"}}}},
	} {
		if _, needed := PrerequisiteGuard(guarded); !needed {
			t.Errorf("%s: a guarded control must pull in the guard block", name)
		}
	}
}
