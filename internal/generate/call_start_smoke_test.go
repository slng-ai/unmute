//go:build smoke

package generate

import (
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// L4 smoke for what arrives before the first word: a dispatched value and a
// carrier fact are saved through the declared type, on the real SDK, the same
// way a step's finish is. customer-intake declares an object, a list and a
// Phone that a dispatch may fill; addCallerNumberFact adds the one thing it does
// not declare, a Phone the carrier fills.

func TestSmokeLiveKitCallStartIsTyped(t *testing.T) {
	runLiveKitSmokeScript(t, "customer-intake", nil, addCallerNumberFact, callStartScript("agent", "Userdata()", livekitFromDispatch))
}

func TestSmokePipecatCallStartIsTyped(t *testing.T) {
	runPipecatSmokeScript(t, "customer-intake", nil, addCallerNumberFact, callStartScript("bot", "build_state()", pipecatFromDispatch))
}

func addCallerNumberFact(agent *ir.Agent) {
	agent.Variables["caller_number"] = ir.Variable{
		Type: ir.PrimitiveString, Shape: &ir.TypeRef{Shaped: ir.ShapedPhone},
		Source: ir.VariableSourceFromNumber, Default: "",
	}
	agent.VariableOrder = append(agent.VariableOrder, "caller_number")
}

// Each target reads the dispatch through its own entry point, so the script
// drives that one rather than the shared helper alone.
const (
	livekitFromDispatch = `def from_dispatch():
    state = generated.Userdata()
    generated._hydrate_call_start(state, generated._dispatched_call_start({}))
    return state
`
	pipecatFromDispatch = `def from_dispatch():
    return generated.build_state()
`
)

func callStartScript(module, fresh, fromDispatch string) string {
	return `"""Smoke check: call-start values and carrier facts are typed."""
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")
generated = _project("` + module + `")


` + fromDispatch + `

def field(state, name, key):
    """Read one field of a saved object, whatever form the state holds it in."""
    return json.loads(generated._state_text(name, getattr(state, name)))[key]


# An object, a list and a Phone are saved. LiveKit used to refuse the first two
# as "must be string" and never checked the third.
os.environ["UNMUTE_CALL_START"] = json.dumps({
    "record": {"record_id": "R1", "opened_on": "2026-10-02", "enquiry": "other"},
    "notes": ["called before"],
    "caller_phone": "+34600111222",
})
state = from_dispatch()
assert field(state, "record", "record_id") == "R1", state.record
# Stored as the model its type names, not as the dict that arrived.
assert type(state.record).__name__ == "CustomerRecord", type(state.record)
assert state.record.record_id == "R1", state.record
assert state.notes == ["called before"], state.notes
assert state.caller_phone == "+34600111222", state.caller_phone

# A Phone outside E.164 stops the call start, names the field, and saves
# nothing of the batch it arrived in.
os.environ["UNMUTE_CALL_START"] = json.dumps({"caller_phone": "600111222", "notes": ["kept?"]})
try:
    from_dispatch()
except RuntimeError as error:
    assert str(error).startswith("call_start: caller_phone"), error
else:
    raise AssertionError("a Phone outside E.164 was saved at call start")
del os.environ["UNMUTE_CALL_START"]
partial = generated.` + fresh + `
try:
    generated._save_call_start(partial, {"caller_phone": "600111222", "notes": ["kept?"]}, ("caller_phone", "notes"))
except RuntimeError:
    pass
assert partial.notes == [], "a refused batch saved part of itself"

# A carrier fact that does not fit is left unsaved and reported, so the caller
# who withholds their number is treated as one with no number, not hung up on.
fact = generated.` + fresh + `
assert generated._save_fact(fact, "caller_number", "anonymous") is False
assert fact.caller_number == "", fact.caller_number
assert generated._save_fact(fact, "caller_number", "+34600111333") is True
assert fact.caller_number == "+34600111333", fact.caller_number

# The list default is written [], which is safe only because Pydantic copies it
# for each instance: one call's notes never appear on the next call.
one, two = generated.` + fresh + `, generated.` + fresh + `
one.notes.append("only on the first call")
assert two.notes == [], two.notes
print("call start typed ok")
`
}
