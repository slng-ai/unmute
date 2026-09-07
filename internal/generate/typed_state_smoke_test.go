//go:build smoke

package generate

import "testing"

// The unit tests hold what the emitted declared-state code says. These hold
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

import asyncio
from types import SimpleNamespace

def check_candidate_visibility():
    fresh = generated.` + stateExpr + `
    fresh.caller_phone = "+34600111222"
    assert generated._render("Phone {{caller_phone}}",fresh) == "Phone none recorded yet."
    assert generated._render("Phone {{caller_phone}}",fresh,site="task:confirm_number") == "Phone +34600111222"
    assert generated._refusal("probe",fresh,[("caller_phone","verify first")])
    generated._save_result("confirm_number",fresh,{"caller_phone":"+34600111222"})
    assert generated._render("Phone {{caller_phone}}",fresh) == "Phone +34600111222"
    assert not generated._refusal("probe",fresh,[("caller_phone","verify first")])

check_candidate_visibility()

async def check_finish_handler():
    fresh = generated.` + stateExpr + `
    ctx = SimpleNamespace(userdata=fresh, function_call=SimpleNamespace(call_id="finish-check"))
    if generated.__name__ == "agent":
        task = generated.ConfirmNumber()
        assert await task.finish(ctx, caller_phone="wrong")
        assert not task.done()
        assert not fresh.caller_phone
        await task.finish(ctx, caller_phone="+34600111222")
        assert task.done()
        await task.finish(ctx, caller_phone="+34600999888")
        assert fresh.caller_phone == "+34600111222"
        escaped = generated.ConfirmNumber()
        await escaped.finish(ctx, unserved_request="another request")
        assert escaped.done()
        assert fresh.caller_phone == "+34600111222"
        cancelled = generated.ConfirmNumber()
        cancelled.cancel()
        await cancelled.finish(ctx, caller_phone="+34600999888")
        assert fresh.caller_phone == "+34600111222"
    else:
        async def ignore(*args, **kwargs): pass
        worker = SimpleNamespace(state=fresh, context=generated.LLMContext(),
            queue_frame=ignore, flush_pipeline=ignore,
            _confirm_number_active_step="confirm_number", _confirm_number_results={},
            _confirm_number_snapshot=([], []), _slng_session_id="test-session")
        finish = generated.DeskAgent._confirm_number_finish_confirm_number
        result, _ = await finish(worker, {"caller_phone":"wrong"}, None)
        assert "refused" in result
        assert worker._confirm_number_active_step == "confirm_number"
        assert not fresh.caller_phone
        await finish(worker, {"caller_phone":"+34600111222"}, None)
        assert fresh.caller_phone == "+34600111222"
        await finish(worker, {"caller_phone":"+34600999888"}, None)
        assert fresh.caller_phone == "+34600111222"
        worker._confirm_number_active_step = "confirm_number"
        await finish(worker, {"unserved_request":"another request"}, None)
        assert fresh.caller_phone == "+34600111222"

async def check_stale_visit():
    if generated.__name__ != "bot": return
    from functools import partial
    fresh=generated.` + stateExpr + `
    async def ignore(*args,**kwargs): pass
    worker=SimpleNamespace(state=fresh, context=generated.LLMContext(), queue_frame=ignore,flush_pipeline=ignore,
        _confirm_number_active_step="confirm_number", _confirm_number_results={},
        _confirm_number_snapshot=([],[]), _confirm_number_visit=object(), _slng_session_id="test")
    worker._confirm_number_finish_confirm_number=partial(generated.DeskAgent._confirm_number_finish_confirm_number,worker)
    node=generated.DeskAgent._confirm_number_node_confirm_number(worker)
    old=next(fn.handler for fn in node["functions"] if fn.name=="finish_confirm_number_confirm_number")
    worker._confirm_number_visit=object()
    result,_=await old({"caller_phone":"+34600111222"},None)
    assert result=={"status":"already handled"} and not fresh.caller_phone
    node=generated.DeskAgent._confirm_number_node_confirm_number(worker)
    current=next(fn.handler for fn in node["functions"] if fn.name=="finish_confirm_number_confirm_number")
    await current({"caller_phone":"+34600111222"},None)
    await current({"caller_phone":"+34600999888"},None)
    assert fresh.caller_phone=="+34600111222"

asyncio.run(check_stale_visit())

async def check_injection():
    fresh = generated.` + stateExpr + `
    fresh.accepted = False
    fresh.count = 0
    fresh.last_appointment = {"scheduled_date":"2026-09-11", "scheduled_time":"14:00", "appointment_type":"haircut"}
    if generated.__name__ == "agent":
        import inspect
        task = generated.Book()
        assert list(inspect.signature(task.inspect_state).parameters) == ["ctx", "note"]
        from livekit.agents.llm.utils import build_legacy_openai_schema
        schema = build_legacy_openai_schema(task.inspect_state)["function"]["parameters"]
        assert set(schema["properties"]) == {"note"} and schema["required"] == ["note"],schema
        result = await task.inspect_state(SimpleNamespace(userdata=fresh), note="authored")
    else:
        worker = SimpleNamespace(context=generated.LLMContext(),state=fresh,_bind_state=lambda handler:handler,_book_finish_book=lambda *args:None)
        node = generated.DeskAgent._book_node_book(worker)
        schema = next(tool for tool in node["functions"] if tool.name == "inspect_state")
        assert set(schema.properties) == {"note"} and schema.required == ["note"],schema
        result = await generated._flow_tool_inspect_state({"note":"authored"}, None, fresh)
    assert result == {"note":"authored", "appointment":fresh.last_appointment,
        "date":"2026-09-11", "accepted":False, "count":0, "label":"Date 2026-09-11"}, result
    assert result["accepted"] is False and type(result["count"]) is int

async def check_owner_return():
    fresh = generated.` + stateExpr + `
    if generated.__name__ == "agent":
        from livekit.agents import llm
        class Owner(generated.Desk):
            @property
            def session(self): return SimpleNamespace(userdata=fresh)
        owner = Owner()
        original = llm.ChatContext()
        original.add_message(role="user",content="OWNER_OLD_SPEECH")
        await owner.update_chat_ctx(original)
        real_task = generated.ConfirmNumber
        class Task:
            def __init__(self, **kwargs): pass
            def __await__(self):
                async def run():
                    private = llm.ChatContext()
                    private.add_message(role="user",content="PRIVATE_TASK_SPEECH")
                    await owner.update_chat_ctx(private)
                    return {"caller_phone":"PRIVATE_RESULT", "unserved_request":"PRIVATE_UNSERVED"}
                return run().__await__()
        generated.ConfirmNumber = Task
        try:
            result = await owner.confirm_number(SimpleNamespace(userdata=fresh))
        finally:
            generated.ConfirmNumber = real_task
        visible = json.dumps({"messages":owner.chat_ctx.to_dict(),"result":result})
        assert result == {"status":"unserved"},result
        assert "OWNER_OLD_SPEECH" in visible and "PRIVATE_" not in visible,visible
    else:
        async def ignore(*args, **kwargs):pass
        worker=SimpleNamespace(state=fresh,context=generated.LLMContext(messages=[{"role":"user","content":"PRIVATE_TASK_SPEECH"}]),
            queue_frame=ignore,flush_pipeline=ignore,_confirm_number_active_step="confirm_number",_confirm_number_results={},
            _confirm_number_snapshot=([{"role":"user","content":"OWNER_OLD_SPEECH"}],[]),_slng_session_id="test")
        await generated.DeskAgent._confirm_number_finish_confirm_number(worker,{"unserved_request":"PRIVATE_UNSERVED"},None)
        visible=json.dumps(worker.context.get_messages())
        assert "OWNER_OLD_SPEECH" in visible and "PRIVATE_" not in visible,visible
        assert json.loads(worker.context.get_messages()[-1]["content"]) == {"status":"unserved"}

asyncio.run(check_owner_return())

asyncio.run(check_injection())

asyncio.run(check_finish_handler())

state = generated.` + stateExpr + `

from copy import deepcopy
before_escape = deepcopy(vars(state))
assert generated._save_result("confirm_number", state, {"unserved_request": "another request"}) == {"unserved_request": "another request"}
assert vars(state) == before_escape

assert generated._save_result("no_output", state, {}) == {}
assert vars(state) == before_escape
try:
    generated._save_result("record_flags", state, {"count": 4, "accepted": "not-a-boolean"})
except generated._StateRefused:
    pass
else:
    raise AssertionError("invalid primitive result was accepted")
assert vars(state) == before_escape
generated._save_result("record_flags", state, {"count": 0, "accepted": False})
assert state.count == 0 and state.accepted is False

assert generated._state_text("appointments", []) == "[]"
assert generated._state_text("caller_phone", None) == "none recorded yet."
assert generated._state_text("caller_reason", False) == "false"
assert generated._state_text("caller_reason", 0) == "0"

# A later invalid destination cannot leave an earlier append behind. A retry
# uses the same call state and only commits once all destinations fit.
from copy import deepcopy
from pydantic import TypeAdapter

original_adapter = generated._STATE_TYPES["last_appointment"]
generated._STATE_TYPES["last_appointment"] = TypeAdapter(int)
before_batch = deepcopy(vars(state))
try:
    generated._save_result("book", state, {
        "reason": "create_booking",
        "appointment": {"scheduled_date": "2026-03-19", "scheduled_time": "09:30",
                        "appointment_type": "haircut"},
        "summary": "recorded",
    })
except generated._StateRefused:
    pass
else:
    raise AssertionError("invalid final destination was saved")
assert vars(state) == before_batch, (vars(state), before_batch)
generated._STATE_TYPES["last_appointment"] = original_adapter
generated._save_result("book", state, {
    "reason": "create_booking",
    "appointment": {"scheduled_date": "2026-03-19", "scheduled_time": "09:30",
                    "appointment_type": "haircut"},
    "summary": "recorded",
})
assert len(state.appointments) == 1
assert state.caller_reason == ["create_booking"]
state = generated.` + stateExpr + `

# A declared list starts empty rather than absent, so an append never has to
# create it, and a step reading it is told so in words.
assert state.appointments == [], state.appointments
assert state.caller_reason == [], state.caller_reason
assert generated._state_text("appointments", state.appointments) == "[]"
assert generated._state_text("caller_reason", state.caller_reason) == "[]"

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

# The finish schema goes out with no $ref and no $defs left in it. Measured
# against the provider: a $ref inside one tool property comes back 200 with the
# model inventing field names for the nested object, so every result would be
# refused where it entered and nothing would say why.
nested = generated.TypeAdapter(list[generated.Appointment])
assert "$ref" in json.dumps(nested.json_schema()), "the fixture stopped producing a $ref to resolve"
resolved = generated._schema(nested)
assert "$ref" not in json.dumps(resolved), json.dumps(resolved)
assert "$defs" not in resolved, sorted(resolved)
# And the resolution put the referenced object's own fields in place.
assert resolved["items"]["properties"]["scheduled_date"]["type"] == "string", resolved
for adapters in generated._FINISH_TYPES.values():
    for name, adapter in adapters.items():
        one = generated._schema(adapter)
        assert "$ref" not in json.dumps(one), (name, one)
        assert "$defs" not in one, (name, sorted(one))

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
