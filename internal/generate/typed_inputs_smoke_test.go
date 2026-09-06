//go:build smoke

package generate

import "testing"

// The unit tests hold what the emitted typed-inputs machinery says. These hold
// what it does, against the real Pydantic in each emitted project: a value
// outside an input's type is refused naming the field, a required one left out
// or left empty is refused, an optional one left out validates as absent and
// renders as words, a shaped one lands as plain data and renders as compact
// JSON, and the explicit schema a delegate advertises carries the enum, the
// descriptions and the resolved object, with no $ref left in it.
//
// One scripted hand-in, run against both emitted modules, ending with one
// expected state written out once and asserted by both (SC-003). Nothing
// reaches a provider, and nothing here drives the framework's own tool loop:
// the delegate method needs a live session, so the save and restore around a
// visit are held by the unit gates over the emitted text and by the real calls
// the quickstart describes.

func TestSmokeTypedInputsLiveKit(t *testing.T) {
	runLiveKitSmokeScript(t, "typed_inputs", nil, nil, typedInputsSmokeScript("agent", "Userdata()", false))
}

func TestSmokeTypedInputsPipecat(t *testing.T) {
	runPipecatSmokeScript(t, "typed_inputs", nil, nil, typedInputsSmokeScript("bot", "build_state()", true))
}

// typedInputsExpectedState is the state both modules hold after the script,
// written once so identical state on both is a property of one literal.
const typedInputsExpectedState = `{
  "kind": "b",
  "note": null,
  "notes": [],
  "outcome": null,
  "problem": null,
  "thing": {"label": "chair", "count": 2}
}`

func typedInputsSmokeScript(module, stateExpr string, pipecat bool) string {
	explicit := ""
	if pipecat {
		explicit = `
# The explicit schema this target advertises in place of the signature-derived
# one: the class is importable and the method that swaps the schema in exists.
assert generated.FunctionSchema is not None
assert callable(getattr(generated.FrontAgent, "build_tools"))
`
	}
	return `"""Smoke check: what a step is handed, over the emitted machinery."""
# ruff: noqa: E402 - the environment has to be seeded before the module imports
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import ` + module + ` as generated

EXPECTED = json.loads(r"""` + typedInputsExpectedState + `""")

state = generated.` + stateExpr + `

# An input has no value outside its visit, whatever its type.
for name in ("kind", "note", "thing", "problem", "outcome"):
    assert getattr(state, name) is None, (name, getattr(state, name))
    assert generated._state_text(name, None) == "not given.", name
assert generated._state_text("notes", state.notes) == "none recorded yet."

# A value outside the Literal: refused naming the field and what was allowed,
# and nothing is written.
try:
    generated._typed_inputs("do_thing", {"kind": "z"})
except generated._StateRefused as refused:
    assert "kind" in refused.message, refused.message
    assert "'a'" in refused.message, refused.message
else:
    raise AssertionError("a value outside the input's set was accepted")

# A required input left out, and one left empty.
for values in ({}, {"kind": ""}, {"kind": None, "note": "x"}):
    try:
        generated._typed_inputs("do_thing", values)
    except generated._StateRefused as refused:
        assert refused.message.startswith("kind: required"), refused.message
    else:
        raise AssertionError("a required input left out was accepted: " + repr(values))
try:
    generated._typed_inputs("to_front", {"outcome": ""})
except generated._StateRefused as refused:
    assert refused.message.startswith("outcome: required"), refused.message
else:
    raise AssertionError("an empty required brief was accepted")

# An optional input left out validates as absent and renders as words. The
# first visit: kind a, nothing else said.
values = generated._typed_inputs("do_thing", {"kind": "a"})
assert values == {"kind": "a", "note": None, "thing": None}, values
for name, value in values.items():
    setattr(state, name, value)
assert generated._state_text("note", state.note) == "not given."
assert generated._state_text("kind", state.kind) == "a"

# A shaped input outside its shape is refused naming the nested field.
try:
    generated._typed_inputs("do_thing", {"kind": "b", "thing": {"label": "chair", "count": "many"}})
except generated._StateRefused as refused:
    assert "thing.count" in refused.message, refused.message
else:
    raise AssertionError("a shaped input with a wrong field was accepted")

# A second visit with its own values: the shaped one lands as plain data and
# renders as compact JSON, never a repr.
values = generated._typed_inputs("do_thing", {"kind": "b", "thing": {"label": "chair", "count": 2}})
assert isinstance(values["thing"], dict), type(values["thing"])
for name, value in values.items():
    setattr(state, name, value)
rendered = generated._state_text("thing", state.thing)
assert rendered == '{"label":"chair","count":2}', rendered
assert "'" not in rendered and "None" not in rendered, rendered

# The brief a handoff carries validates the same way, and a shaped one it
# shares a name with is one field.
brief = generated._typed_inputs("to_specialist", {"problem": "the chair broke"})
assert brief == {"problem": "the chair broke", "thing": None}, brief

# The schema the model is sent: the enum, the descriptions, the resolved object,
# and no $ref or $defs anywhere, on every site.
for site, adapters in generated._INPUT_TYPES.items():
    for name, adapter in adapters.items():
        one = generated._schema(adapter)
        assert "$ref" not in json.dumps(one), (site, name, one)
        assert "$defs" not in one, (site, name, sorted(one))
kind = generated._schema(generated._INPUT_TYPES["do_thing"]["kind"])
assert kind.get("enum") == ["a", "b"], kind
note = generated._schema(generated._INPUT_TYPES["do_thing"]["note"])
assert "Anything the caller added" in json.dumps(note), note
thing = generated._schema(generated._INPUT_TYPES["do_thing"]["thing"])
assert "label" in json.dumps(thing) and "count" in json.dumps(thing), thing
assert generated._INPUT_REQUIRED == {"do_thing": {"kind"}, "to_front": {"outcome"}, "to_specialist": {"problem"}}, generated._INPUT_REQUIRED
` + explicit + `
final = {
    "kind": state.kind,
    "note": state.note,
    "notes": state.notes,
    "outcome": state.outcome,
    "problem": state.problem,
    "thing": state.thing,
}
assert final == EXPECTED, json.dumps(final, indent=2, sort_keys=True)
print("typed inputs: the scripted hand-in ends with the expected state")
`
}
