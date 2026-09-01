package generate

import (
	"strconv"
	"strings"

	"github.com/slng-ai/unmute/internal/ir"
)

// The prerequisite guard, and the one place its words are written.
//
// A control can declare `requires:`, naming variables that must hold a value
// before it runs. Both code drivers refuse such a control while a name is
// unmet. Before this file the two drivers wrote that refusal in their own
// words: LiveKit said "Cannot transfer yet; missing required information:" and
// Pipecat said "Cannot transfer yet; still need:". Nothing made them agree, so
// they did not, and an author reading one target's behaviour learned something
// slightly untrue about the other.
//
// So the wording lives here, once, and both templates render what this file
// produces. The refusal goes to the model and never to the caller: the caller
// hears the model's next natural question instead.

// PrerequisiteLimit is how many times one control may be refused before the
// agent stops recovering silently and asks the caller out loud.
//
// Silent recovery is right while the model can still plausibly fetch the value
// itself. It stops being right once the model has tried and failed several
// times, because from there the silence is just a conversation that has stopped
// making progress. Five is high enough that an ordinary recovery never reaches
// it and low enough that a caller is not left waiting through a loop.
//
// It lives in emitted code rather than in prompt text. A model can be talked out
// of an instruction; it cannot be talked out of a counter.
const PrerequisiteLimit = 5

// SupplierIndex maps a variable name to the control that fills it.
//
// Only a delegate can fill a variable, through its `assign:` block, so that is
// the whole search. Where two controls assign the same variable, the first in
// sorted control order wins, which makes the emitted output deterministic rather
// than dependent on map order.
func SupplierIndex(controls map[string]ir.Control) map[string]string {
	index := map[string]string{}
	for _, name := range sortedKeys(controls) {
		delegate, ok := controls[name].(*ir.Delegate)
		if !ok {
			continue
		}
		for _, variable := range sortedKeys(delegate.Assign) {
			if _, taken := index[variable]; !taken {
				index[variable] = name
			}
		}
	}
	return index
}

// prerequisiteClause names one unmet variable and, where one exists, the control
// that fills it. Where nothing fills it, because the value arrives from
// `source:`, from `--var` or from the carrier, the clause names the requirement
// and stops rather than pointing at a control that cannot help.
//
// The same "(call X to get it)" phrasing appears in the generated Python below.
// That is two sentences for two readers, not one sentence written twice: this
// one goes on the tool description before the model gets there, the other goes
// in the refusal after it did. They are free to diverge, and sharing a fragment
// across the language boundary would cost more indirection than it saves.
func prerequisiteClause(variable string, suppliers map[string]string) string {
	if supplier, ok := suppliers[variable]; ok {
		return variable + " (call " + supplier + " to get it)"
	}
	return variable
}

// ForwardDeclaration is the sentence appended to a guarded control's
// model-facing description.
//
// This is the mechanism; the guard is only the net. A model that can see the
// requirement before it reaches the step collects the value during the earlier
// turns, so the guard is rarely reached at all and the caller never notices
// there was one. A model that only discovers the requirement by being refused
// has already spent a turn.
// The sentence is also a claim, and it can be false. A variable that always
// holds a value by session start and needs no confirmation is one the agent
// already has, so telling the model to go and fetch it is an instruction to
// re-verify a caller it already knows, and the model obeys. So such a variable
// is dropped from the list, and when that empties the list the sentence is empty.
//
// A variable awaiting confirmation is the opposite case and keeps its clause:
// there the sentence is exactly right, because the value is present and unusable
// until the naming step has heard the caller agree.
func ForwardDeclaration(requires []string, suppliers map[string]string, variables map[string]ir.Variable) string {
	if len(requires) == 0 {
		return ""
	}
	clauses := make([]string, 0, len(requires))
	for _, variable := range requires {
		if settledAtSessionStart(variables, variable) {
			continue
		}
		clauses = append(clauses, prerequisiteClause(variable, suppliers))
	}
	if len(clauses) == 0 {
		return ""
	}
	return " Before this can run you need: " + strings.Join(clauses, ", ") + "."
}

// settledAtSessionStart reports whether a variable is one the agent certainly
// holds, usable, before the first spoken word.
//
// "Certainly" is the load-bearing word, and it is why a pre-fetched value does
// not count: an entry whose inputs are empty is skipped, so the value may be
// there or may be at its default, and the sentence has to stay for the case where
// it is not. A dispatched or runtime-owned value is different: if it were
// missing, the session would have failed at start rather than reached this
// description.
func settledAtSessionStart(variables map[string]ir.Variable, name string) bool {
	variable, declared := variables[name]
	if !declared || variable.Confirm != "" {
		return false
	}
	return variable.Source == ir.VariableSourceCallStart || ir.IsSystemSource(variable.Source)
}

// guardHelperSource is the Python the drivers share.
//
// Rendering one generated block into both templates is what makes the two
// targets agree by construction rather than by a test that has to notice they
// stopped agreeing. The test still exists, because construction can be undone.
//
// getattr with a default reads state on both targets: LiveKit keeps variables on
// a userdata dataclass and Pipecat on a call-state object, and both are plain
// attribute lookups.
func guardHelperSource(suppliers map[string]string, unconfirmed bool) string {
	var b strings.Builder
	b.WriteString(`# Prerequisite guard, generated by internal/generate/guard.go.
#
# A control that declares requires: is held back until every named variable
# holds a value. The refusal below goes to the model, never to the caller: it
# names what is missing and which control supplies it, so the model fetches the
# value and retries within the same turn. The caller hears the model's next
# natural question and never learns a guard fired.
#
# Both emitted targets render this same block, so their wording cannot drift.
_PREREQUISITE_LIMIT = ` + strconv.Itoa(PrerequisiteLimit) + `
_PREREQUISITE_SUPPLIER = {
`)
	for _, variable := range sortedKeys(suppliers) {
		b.WriteString("    " + pyQuote(variable) + ": " + pyQuote(suppliers[variable]) + ",\n")
	}
	b.WriteString("}\n\n")
	if unconfirmed {
		// A value the caller has not agreed to is present but not usable. Holding
		// it back here rather than in prompt text is the same argument as the
		// counter above: a model can be talked out of an instruction, not out of a
		// set membership test. getattr with a default keeps the helper working
		// before the pre-fetch has run.
		b.WriteString(`
def _unmet_prerequisites(state, names):
    return [
        name
        for name in names
        if getattr(state, name, None) in (None, "")
        or name in getattr(state, "_unconfirmed", ())
    ]
`)
	} else {
		b.WriteString(`
def _unmet_prerequisites(state, names):
    return [name for name in names if getattr(state, name, None) in (None, "")]
`)
	}
	b.WriteString(`

def _prerequisite_refusal(names, at_limit):
    wants = ", ".join(
        name + " (call " + _PREREQUISITE_SUPPLIER[name] + " to get it)"
        if name in _PREREQUISITE_SUPPLIER
        else name
        for name in names
    )
    if at_limit:
        return (
            "Not started. Still missing: "
            + wants
            + ". Do not say any of this out loud. You have tried several times"
            + " without it, so ask the caller for it directly now, in your own"
            + " plain words, and stay in the conversation."
        )
    return (
        "Not started. Missing: "
        + wants
        + ". Do not say any of this out loud. Get the missing value now, then"
        + " call this again in the same turn."
    )
`)
	return b.String()
}

// PrerequisiteGuard returns the Python guard block for a package, and whether
// the package needs one at all.
//
// A package where nothing declares `requires:` gets no block and no behaviour
// change: its emitted output is byte-for-byte what it was before this existed.
// That is the point of returning the flag rather than always emitting a helper
// nothing calls.
func PrerequisiteGuard(agent *ir.Agent) (string, bool) {
	guarded := false
	for _, control := range agent.Controls {
		switch control := control.(type) {
		case *ir.Delegate:
			guarded = guarded || len(control.Requires) > 0
		case *ir.AgentTransfer:
			guarded = guarded || len(control.Requires) > 0
		}
	}
	if !guarded {
		return "", false
	}
	return guardHelperSource(SupplierIndex(agent.Controls), PrefetchUnconfirmed(agent)), true
}

// delegateForwardDeclaration is the sentence appended to one delegate's
// description, resolved against the package it lives in.
func delegateForwardDeclaration(agent *ir.Agent, c *ir.Delegate) string {
	return ForwardDeclaration(c.Requires, SupplierIndex(agent.Controls), agent.Variables)
}
