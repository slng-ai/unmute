package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The supplier index is what lets an inject refusal say more than "something is
// missing". Naming the step that fills the gap is the difference between a
// model that recovers on the same turn and one that asks the caller a question
// a step, not the caller, was going to answer.
func TestSupplierIndex(t *testing.T) {
	t.Run("one supplier", func(t *testing.T) {
		index := SupplierIndex(map[string]ir.Control{
			"verify_customer": &ir.Delegate{Assign: []ir.AssignTo{{Var: "customer_phone", Field: "customer_phone"}}},
			"manage_booking":  &ir.Delegate{},
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

// TestNeededHintNamesTheRightStep is the Go-level unit test for the per-value
// advice a refusal gives: a confirm value always names its own confirming
// step, a plain assigned value names its supplier, and anything else falls
// back to asking the caller, followed by its description.
func TestNeededHintNamesTheRightStep(t *testing.T) {
	suppliers := map[string]string{"assigned_value": "book"}

	if got := neededHint("confirmed_value", ir.Variable{Confirm: "verify_customer"}, suppliers); got != "run verify_customer first." {
		t.Errorf("neededHint(confirm) = %q, want it to name the confirming step", got)
	}
	// A confirm value that also happens to have a supplier still names its own
	// confirming step: that is the step that clears the mark, so it is the one
	// to run, whichever task's assign: also happens to write it.
	if got := neededHint("confirmed_value", ir.Variable{Confirm: "verify_customer"}, map[string]string{"confirmed_value": "book"}); got != "run verify_customer first." {
		t.Errorf("neededHint(confirm+supplier) = %q, want the confirming step to win", got)
	}
	if got := neededHint("assigned_value", ir.Variable{}, suppliers); got != "run book first." {
		t.Errorf("neededHint(assigned, no confirm) = %q, want it to name the assigning step", got)
	}
	if got := neededHint("plain_value", ir.Variable{Description: "The plain value's description."}, suppliers); got != "ask the caller for it. The plain value's description." {
		t.Errorf("neededHint(plain) = %q, want it to say to ask the caller, then the description", got)
	}
	if got := neededHint("undescribed_value", ir.Variable{}, suppliers); got != "ask the caller for it." {
		t.Errorf("neededHint(no description) = %q, want the bare instruction with no trailing text", got)
	}
}

// TestRefusalNamesTheSupplyingStepAndAsksForTheRest is the emitted-Python half
// of the same rule: a confirm value's refusal names its confirming step
// (never "ask the caller", which is the mistake confirm: exists to prevent —
// the salon's own phone number would be read out by the caller for a value
// the agent is about to use to look them up), and a plain value's refusal
// tells the model to ask the caller and gives it the value's description.
func TestRefusalNamesTheSupplyingStepAndAsksForTheRest(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	// Two steps: one confirms a value, the other only assigns one, so the
	// fixture carries one of each case the hint distinguishes and the two
	// emitted hints cannot read alike by accident.
	agent.Tasks["verify_customer"] = ir.Task{
		Instructions: "Confirm who is calling.",
		Result:       map[string]ir.ResultField{"confirmed_value": {Type: ir.PrimitiveString}},
		Context:      ir.TaskContext{History: ir.HistoryFull},
	}
	agent.Tasks["record_note"] = ir.Task{
		Instructions: "Record a note.",
		Result:       map[string]ir.ResultField{"assigned_value": {Type: ir.PrimitiveString}},
		Context:      ir.TaskContext{History: ir.HistoryFull},
	}
	agent.Controls["verify_customer"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Task: "verify_customer", When: "Identify the caller.",
		Assign: []ir.AssignTo{{Var: "confirmed_value", Field: "confirmed_value"}},
	}
	agent.Controls["record_note"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Task: "record_note", When: "Record a note.",
		Assign: []ir.AssignTo{{Var: "assigned_value", Field: "assigned_value"}},
	}
	agent.Variables["confirmed_value"] = ir.Variable{Type: ir.PrimitiveString, Confirm: "verify_customer"}
	agent.Variables["assigned_value"] = ir.Variable{Type: ir.PrimitiveString}
	agent.Variables["plain_value"] = ir.Variable{Type: ir.PrimitiveString, Description: "The plain value's description."}
	tool := agent.Tools["get_invoice"]
	tool.Inject = map[string]any{
		"confirmed": "{{confirmed_value}}",
		"assigned":  "{{assigned_value}}",
		"plain":     "{{plain_value}}",
	}
	agent.Tools["get_invoice"] = tool
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "verify_customer", "record_note")
	agent.Agents["intake"] = intake

	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		file := "agent.py"
		if provider == ir.ProviderPipecat {
			file = "bot.py"
		}
		artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
		if err != nil {
			t.Fatalf("%s: generate: %v", provider, err)
		}
		py := artifactFile(t, artifact, file)
		for _, want := range []string{
			`("confirmed_value", "run verify_customer first.")`,
			`("assigned_value", "run record_note first.")`,
			`("plain_value", "ask the caller for it. The plain value's description.")`,
		} {
			if !strings.Contains(py, want) {
				t.Errorf("%s: get_invoice's NeededLiteral omits %s", provider, want)
			}
		}
		if strings.Contains(py, "Ask the caller first.") {
			t.Errorf("%s: _refusal still carries the old blanket advice", provider)
		}
	}
}
