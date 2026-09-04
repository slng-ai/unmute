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
			"verify_customer": &ir.Delegate{Assign: []ir.AssignTo{{Var: "customer_phone", Field: "customer_phone"}}},
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
			"zulu":  &ir.Delegate{Assign: []ir.AssignTo{{Var: "phone", Field: "phone"}}},
			"alpha": &ir.Delegate{Assign: []ir.AssignTo{{Var: "phone", Field: "phone"}}},
			"mike":  &ir.Delegate{Assign: []ir.AssignTo{{Var: "phone", Field: "phone"}}},
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
	variables := map[string]ir.Variable{"customer_phone": {Type: ir.PrimitiveString}}

	if got := ForwardDeclaration(nil, suppliers, variables); got != "" {
		t.Errorf("an unguarded control gets no sentence, got %q", got)
	}

	got := ForwardDeclaration([]string{"customer_phone"}, suppliers, variables)
	for _, want := range []string{"customer_phone", "verify_customer"} {
		if !strings.Contains(got, want) {
			t.Errorf("forward declaration %q omits %q", got, want)
		}
	}

	// Where nothing supplies the value, because it arrives from source:, from
	// --var or from the carrier, the sentence names the requirement and stops
	// rather than pointing at a control that cannot help.
	got = ForwardDeclaration([]string{"caller_number"}, suppliers, variables)
	if !strings.Contains(got, "caller_number") {
		t.Errorf("forward declaration %q omits the requirement", got)
	}
	if strings.Contains(got, "call ") {
		t.Errorf("forward declaration %q names a supplier for a variable that has none", got)
	}
}

// The sentence is a claim, and it used to be made unconditionally. These are the
// two cases from research.md R7, and they pull in opposite directions, which is
// why the fix is a condition rather than a deletion.
func TestForwardDeclarationDropsAClauseTheRuntimeWillNeverNeed(t *testing.T) {
	suppliers := map[string]string{"customer_phone": "verify_customer"}

	t.Run("settled: the clause goes", func(t *testing.T) {
		// The value arrives from the carrier and needs no confirmation, so the
		// agent already has it, usable, before it speaks. Telling the model to
		// call verify_customer here is an instruction to re-verify a caller it
		// already knows, and the model obeys.
		variables := map[string]ir.Variable{
			"customer_phone": {Type: ir.PrimitiveString, Source: ir.VariableSourceFromNumber},
		}
		if got := ForwardDeclaration([]string{"customer_phone"}, suppliers, variables); got != "" {
			t.Errorf("a settled value keeps a sentence that is false: %q", got)
		}
	})

	t.Run("proposed: the clause stays", func(t *testing.T) {
		// The value is present and unusable until the caller agrees, so the
		// sentence is exactly right and has to survive. This is the half a
		// blanket deletion would have got wrong.
		variables := map[string]ir.Variable{
			"customer_phone": {Type: ir.PrimitiveString, Confirm: "verify_customer"},
		}
		got := ForwardDeclaration([]string{"customer_phone"}, suppliers, variables)
		if !strings.Contains(got, "verify_customer") {
			t.Errorf("an unconfirmed value loses the step that confirms it: %q", got)
		}
	})

	t.Run("a pre-fetched value keeps its clause", func(t *testing.T) {
		// A prefetch entry may skip, so the value may be at its default. "May
		// hold" is not "does hold", and the sentence has to stay for the case
		// where it does not.
		variables := map[string]ir.Variable{"customer_phone": {Type: ir.PrimitiveString, Default: ""}}
		got := ForwardDeclaration([]string{"customer_phone"}, suppliers, variables)
		if !strings.Contains(got, "customer_phone") {
			t.Errorf("a value that may be skipped loses its sentence: %q", got)
		}
	})

	t.Run("one settled name among two leaves the other", func(t *testing.T) {
		variables := map[string]ir.Variable{
			"customer_phone": {Type: ir.PrimitiveString, Source: ir.VariableSourceFromNumber},
			"account_id":     {Type: ir.PrimitiveString},
		}
		got := ForwardDeclaration([]string{"customer_phone", "account_id"}, suppliers, variables)
		if strings.Contains(got, "customer_phone") {
			t.Errorf("the settled name survived: %q", got)
		}
		if !strings.Contains(got, "account_id") {
			t.Errorf("the unsettled name was dropped with it: %q", got)
		}
	})
}

// FR-024, FR-029 and the byte-identical promise, in one place: the guard reads
// the unconfirmed set only when a package has one.
func TestGuardConsultsUnconfirmedOnlyWhenAPackageHasAny(t *testing.T) {
	with := guardHelperSource(map[string]string{"customer_phone": "verify_customer"}, true, nil, false)
	if !strings.Contains(with, `name in getattr(state, "_unconfirmed", ())`) {
		t.Errorf("an unconfirmed value satisfies a gate it should not:\n%s", with)
	}

	without := guardHelperSource(map[string]string{"customer_phone": "verify_customer"}, false, nil, false)
	if strings.Contains(without, "_unconfirmed") {
		t.Errorf("a package with no confirm: gained the set, so its emitted output moved:\n%s", without)
	}
	// getattr with a default, not a bare attribute read: the guard has to work on
	// a call where the pre-fetch has not run yet.
	if !strings.Contains(with, `getattr(state, "_unconfirmed", ())`) {
		t.Error("the set is read without a default, so a call before the pre-fetch raises")
	}
}

// The generated block is the single owner of the refusal wording. Both drivers
// render this same text, which is why the two targets cannot drift: there is
// only one string to change.
func TestGuardHelperSource(t *testing.T) {
	src := guardHelperSource(map[string]string{"customer_phone": "verify_customer"}, false, nil, false)

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

// TestEmptyListDoesNotSatisfyAGuard is research section 14, and the naive fix
// for it regresses the case the predicate was written to protect.
//
// `appointments: []` means nothing has been booked yet, which is exactly the
// state a guard exists to wait for, so it is unmet. But `0`, `False` and `0.0`
// are real answers a caller can give, so the arm has to test the declared type
// rather than truthiness: a blanket `if not value` would hold a step back on a
// caller who answered zero.
func TestEmptyListDoesNotSatisfyAGuard(t *testing.T) {
	agent := loadTypedState(t)
	block, ok := PrerequisiteGuard(agent)
	if !ok {
		t.Fatal("the fixture declares no requires:, so this gate proves nothing")
	}
	// The set is the declared lists and nothing else, so a primitive cannot
	// reach the emptiness arm at all.
	if !strings.Contains(block, `_PREREQUISITE_LISTS = {"appointments", "caller_reason"}`) {
		t.Errorf("the guard does not name the declared lists:\n%s", block)
	}
	// The emptiness arm is reached only through that set. Written as one line so
	// the membership test and the length test cannot be separated by an edit
	// without this failing.
	if !strings.Contains(block, "elif not path and root in _PREREQUISITE_LISTS and len(value) == 0:") {
		t.Errorf("the guard tests emptiness outside the declared-list set:\n%s", block)
	}
	// And the regression the naive fix produces is not there.
	for _, forbidden := range []string{"if not value", "if not getattr(state", "not value:"} {
		if strings.Contains(block, forbidden) {
			t.Errorf("the guard uses truthiness (%q), which holds a step back on a caller who answered 0 or False:\n%s",
				forbidden, block)
		}
	}
	// A package declaring no list keeps the predicate it had, byte for byte, so
	// nothing that compiles today changes.
	plain, ok := PrerequisiteGuard(loadExample(t, "salon-concierge"))
	if !ok {
		t.Fatal("the salon package declares no requires:, so the control half proves nothing")
	}
	if strings.Contains(plain, "_PREREQUISITE_LISTS") {
		t.Errorf("a package declaring no list grew the list-aware predicate:\n%s", plain)
	}
}

// TestAPathIntoAnUnconfirmedValueSatisfiesNothing is FR-008a's other half: the
// mark is on the value, so naming a field one level down must not escape it.
func TestAPathIntoAnUnconfirmedValueSatisfiesNothing(t *testing.T) {
	agent := loadTypedState(t)
	block, ok := PrerequisiteGuard(agent)
	if !ok {
		t.Fatal("the fixture declares no requires:, so this gate proves nothing")
	}
	// The root is what is tested against the set, before any path is walked.
	root := strings.Index(block, `if root in getattr(state, "_unconfirmed", ())`)
	walk := strings.Index(block, "for step in path.split")
	if root < 0 {
		t.Fatalf("the guard does not test the path's root against the unconfirmed set:\n%s", block)
	}
	if walk >= 0 && walk < root {
		t.Errorf("the guard walks the path before testing its root, so a field one level down escapes the mark:\n%s",
			block)
	}
}
