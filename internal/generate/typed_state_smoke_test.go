//go:build smoke

package generate

import "testing"

// The unit tests hold what the emitted declared-state block says. These hold
// what it does, against the real Pydantic in each emitted project: the classes
// construct, the validators accept a legal value and refuse an illegal one, an
// appended entry lands beside the one before it, and a refused value leaves the
// previous contents exactly as they were.
//
// One scripted conversation, run against both emitted modules, ending with one
// expected state written out once and asserted by both. That is what SC-003
// asks for and what a comparison of a state handed in cannot give: the state
// here is the state the conversation produced.
//
// Nothing reaches a provider. The script drives the module's own state
// machinery, which is where every claim in this feature actually lands.

func TestSmokeTypedStateLiveKit(t *testing.T) {
	runLiveKitSmokeScript(t, "typed_state", nil, nil, typedStateSmokeScript("agent", "Userdata()"))
}

func TestSmokeTypedStatePipecat(t *testing.T) {
	runPipecatSmokeScript(t, "typed_state", nil, nil, typedStateSmokeScript("bot", "build_state()"))
}

// typedStateExpectedState is the declared state the scripted conversation ends
// with, written once and asserted by both targets. Identical state on both is
// therefore a property of this one literal rather than of two scripts kept in
// step by hand.
const typedStateExpectedState = `{
  "appointments": [
    {"appointment_type": "haircut", "scheduled_date": "2026-03-19", "scheduled_time": "09:30"},
    {"appointment_type": "dry_cut", "scheduled_date": "2026-03-26", "scheduled_time": "14:00"}
  ],
  "caller_phone": "+34600111222",
  "caller_reason": ["create_booking", "cancel_booking"]
}`

// typedStateSmokeScript is the whole conversation, parameterised only by the
// module's name and how that target builds its state object. Everything the
// script asserts comes out of the shared block, so the two runs differ in
// nothing that matters.
func typedStateSmokeScript(module, stateExpr string) string {
	return `"""Smoke check: the scripted conversation, over the emitted state."""
# ruff: noqa: E402 - the environment has to be seeded before the module imports
import json
import os

# Placeholders for the startup check one target runs at import. Nothing here
# reaches a provider: the script drives the module's own state machinery.
for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import ` + module + ` as generated

EXPECTED = json.loads(r"""` + typedStateExpectedState + `""")

state = generated.` + stateExpr + `

# A declared list starts empty rather than absent, so an append never has to
# create it, and a step reading it is told so in words.
assert state.appointments == [], state.appointments
assert state.caller_reason == [], state.caller_reason
assert generated._state_text("appointments", state.appointments) == "none recorded yet."
assert generated._state_text("caller_reason", state.caller_reason) == "none recorded yet."

# The step that reads the caller's number back and gets a yes.
values = generated._typed_result(
    "confirm_number", {"caller_phone": "+34600111222", "summary": "read back and agreed"}
)
state.caller_phone = values["caller_phone"]

# The caller books, and then books again, and then changes their mind about the
# second one. Two appointments and two reasons, in the order they gave them.
booked = [
    ("2026-03-19", "09:30", "haircut", "create_booking"),
    ("2026-03-26", "14:00", "dry_cut", "cancel_booking"),
]
for day, at, service, reason in booked:
    values = generated._typed_result(
        "book",
        {
            "appointment": {
                "scheduled_date": day,
                "scheduled_time": at,
                "appointment_type": service,
            },
            "reason": reason,
            "summary": "recorded",
        },
    )
    # Plain data, not a model: one framework refuses a BaseModel outright and
    # drops the whole tool result, the other cannot serialise one at all.
    assert isinstance(values["appointment"], dict), type(values["appointment"])
    state.appointments.append(values["appointment"])
    state.caller_reason.append(values["reason"])

assert len(state.appointments) == 2, state.appointments
assert state.appointments[0]["scheduled_date"] == "2026-03-19", state.appointments

# A value outside the declared set. Refused where it enters, the message names
# the field and lists what was allowed, and the previous contents survive.
before = json.dumps(
    {
        "appointments": state.appointments,
        "caller_phone": state.caller_phone,
        "caller_reason": state.caller_reason,
    },
    sort_keys=True,
)
for bad, field, allowed in (
    (
        {
            "appointment": {
                "scheduled_date": "2026-04-02",
                "scheduled_time": "10:00",
                "appointment_type": "haircut",
            },
            "reason": "sell_me_a_car",
            "summary": "x",
        },
        "reason",
        "create_booking",
    ),
    (
        {
            "appointment": {
                "scheduled_date": "2026-04-02",
                "scheduled_time": "10:00",
                "appointment_type": "a_perm",
            },
            "reason": "create_booking",
            "summary": "x",
        },
        "appointment.appointment_type",
        "haircut",
    ),
    (
        {
            "appointment": {
                "scheduled_date": "the second of April",
                "scheduled_time": "10:00",
                "appointment_type": "haircut",
            },
            "reason": "create_booking",
            "summary": "x",
        },
        "appointment.scheduled_date",
        "year-month-day",
    ),
):
    try:
        generated._typed_result("book", bad)
    except generated._StateRefused as refused:
        assert field in refused.message, (field, refused.message)
        assert allowed in refused.message, (allowed, refused.message)
    else:
        raise AssertionError("a value outside its declared type entered the state: " + repr(bad))

after = json.dumps(
    {
        "appointments": state.appointments,
        "caller_phone": state.caller_phone,
        "caller_reason": state.caller_reason,
    },
    sort_keys=True,
)
assert after == before, "a refused value changed the state"

# A phone number of the wrong shape, refused the same way and with its shape
# named. The shape is checked here and never written into the schema.
try:
    generated._typed_result("confirm_number", {"caller_phone": "600 111 222", "summary": "x"})
except generated._StateRefused as refused:
    assert "caller_phone" in refused.message, refused.message
    assert "E.164" in refused.message, refused.message
else:
    raise AssertionError("a phone number of the wrong shape entered the state")

# The state as a prompt reads it: compact JSON, never a Python repr.
rendered = generated._state_text("appointments", state.appointments)
assert rendered.startswith('[{"'), rendered
assert "'" not in rendered, rendered
assert "None" not in rendered, rendered
assert generated._state_text("caller_reason", state.caller_reason) == '["create_booking","cancel_booking"]'

# The guard. An empty list is unmet, because "nothing booked yet" is exactly the
# state a guard exists to wait for. A zero and a false are not: they are real
# answers a caller can give.
empty = generated.` + stateExpr + `
assert generated._unmet_prerequisites(empty, ["appointments"]) == ["appointments"]
assert generated._unmet_prerequisites(state, ["appointments"]) == []
empty.a_zero, empty.a_false = 0, False
assert generated._unmet_prerequisites(empty, ["a_zero", "a_false"]) == []

# A value awaiting the caller's agreement satisfies no guard through any path
# into it, so naming a field one level down cannot escape the mark.
empty._unconfirmed = {"caller_phone"}
empty.caller_phone = "+34600111222"
assert generated._unmet_prerequisites(empty, ["caller_phone"]) == ["caller_phone"]

# And the whole declared state, field for field, against the one expectation
# both targets read.
final = {
    "appointments": state.appointments,
    "caller_phone": state.caller_phone,
    "caller_reason": state.caller_reason,
}
assert final == EXPECTED, json.dumps(final, indent=2, sort_keys=True)
print("typed state: the scripted conversation ends with the expected state")
`
}
