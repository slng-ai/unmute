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
	runLiveKitSmokeScript(t, "typed_state", nil, nil, typedStateSmokeScript("agent", "CallState()"))
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
    {"appointment_type": "haircut", "scheduled_date": "2026-03-19", "scheduled_time": "09:30:00"},
    {"appointment_type": "dry_cut", "scheduled_date": "2026-03-26", "scheduled_time": "14:00:00"}
  ],
  "booked_for": {"email": "fred.bloggs@example.com", "name": "Fred Bloggs"},
  "caller_phone": "+34600111222",
  "caller_reason": ["create_booking", "cancel_booking"],
  "reminder_email": "fred.bloggs@example.com"
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

generated = _project("` + module + `")

EXPECTED = json.loads(r"""` + typedStateExpectedState + `""")

import asyncio
from types import SimpleNamespace

from pydantic_core import to_jsonable_python

def check_candidate_visibility():
    fresh = generated.` + stateExpr + `
    fresh.caller_phone = "+34600111222"
    assert generated._render("Phone {{caller_phone}}",fresh) == "Phone none recorded yet."
    assert generated._render("Phone {{caller_phone}}",fresh,site="task:confirm_number") == "Phone +34600111222"
    assert generated._refusal("probe",fresh,[("caller_phone","verify first")])
    fresh.save_result("confirm_number", {"caller_phone":"+34600111222"})
    assert generated._render("Phone {{caller_phone}}",fresh) == "Phone +34600111222"
    assert not generated._refusal("probe",fresh,[("caller_phone","verify first")])

check_candidate_visibility()

async def check_finish_handler():
    fresh = generated.` + stateExpr + `
    # session, because the emitted finish reports to the dev reporter through
    # ctx.session. It has no reporter attached, which is the case a real run
    # outside unmute dev is in.
    ctx = SimpleNamespace(userdata=fresh, session=SimpleNamespace(),
        function_call=SimpleNamespace(call_id="finish-check"))
    if generated.__name__ == "agent":
        task = generated.ConfirmNumber()
        assert await task.finish({"caller_phone": "wrong"}, ctx)
        assert not task.done()
        assert not fresh.caller_phone
        await task.finish({"caller_phone": "+34600111222"}, ctx)
        assert task.done()
        await task.finish({"caller_phone": "+34600999888"}, ctx)
        assert fresh.caller_phone == "+34600111222"
        escaped = generated.ConfirmNumber()
        await escaped.finish({"unserved_request": "another request"}, ctx)
        assert escaped.done()
        assert fresh.caller_phone == "+34600111222"
        cancelled = generated.ConfirmNumber()
        cancelled.cancel()
        await cancelled.finish({"caller_phone": "+34600999888"}, ctx)
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
    node=await generated.DeskAgent._confirm_number_node_confirm_number(worker)
    old=next(fn.handler for fn in node["functions"] if fn.name=="finish_confirm_number_confirm_number")
    worker._confirm_number_visit=object()
    result,_=await old({"caller_phone":"+34600111222"},None)
    assert result=={"status":"already handled"} and not fresh.caller_phone
    node=await generated.DeskAgent._confirm_number_node_confirm_number(worker)
    current=next(fn.handler for fn in node["functions"] if fn.name=="finish_confirm_number_confirm_number")
    await current({"caller_phone":"+34600111222"},None)
    await current({"caller_phone":"+34600999888"},None)
    assert fresh.caller_phone=="+34600111222"

asyncio.run(check_stale_visit())

async def check_injection():
    fresh = generated.` + stateExpr + `
    fresh.save_batch({"accepted": False, "count": 0, "last_appointment": {
        "scheduled_date": "2026-09-11", "scheduled_time": "14:00", "appointment_type": "haircut"}})
    if generated.__name__ == "agent":
        import inspect
        task = generated.Book()
        assert list(inspect.signature(task.inspect_state).parameters) == ["ctx", "note"]
        from livekit.agents.llm.utils import build_legacy_openai_schema
        schema = build_legacy_openai_schema(task.inspect_state)["function"]["parameters"]
        assert set(schema["properties"]) == {"note"} and schema["required"] == ["note"],schema
        result = await task.inspect_state(
            SimpleNamespace(userdata=fresh, session=SimpleNamespace()), note="authored")
    else:
        worker = SimpleNamespace(context=generated.LLMContext(),state=fresh,_bind_state=lambda handler:handler,_book_finish_book=lambda *args:None)
        node = await generated.DeskAgent._book_node_book(worker)
        schema = next(tool for tool in node["functions"] if tool.name == "inspect_state")
        assert set(schema.properties) == {"note"} and schema.required == ["note"],schema
        result = await generated._flow_tool_inspect_state({"note":"authored"}, None, fresh)
    assert result == {"note":"authored", "appointment":fresh.plain("last_appointment"),
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
            # A session on the context, because the emitted task return reports
            # to the dev reporter through ctx.session. Without it this script
            # stops here and every assertion below it, the email ones included,
            # is never reached on this target.
            result = await owner.confirm_number(
                SimpleNamespace(userdata=fresh, session=SimpleNamespace()))
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


def snapshot(s):
    """Everything a save may change: the fields, and the two private records."""
    return deepcopy((vars(s), s._unconfirmed, s._prefetch_provenance))


before_escape = snapshot(state)
assert state.save_result("confirm_number", {"unserved_request": "another request"}) == {"unserved_request": "another request"}
assert snapshot(state) == before_escape

assert not state.save_result("no_output", {}).get("unserved_request")
assert snapshot(state) == before_escape
try:
    state.save_result("record_flags", {"count": 4, "accepted": "not-a-boolean"})
except generated.StateRefused:
    pass
else:
    raise AssertionError("invalid primitive result was accepted")
assert snapshot(state) == before_escape
state.save_result("record_flags", {"count": 0, "accepted": False})
assert state.count == 0 and state.accepted is False

empty = generated.` + stateExpr + `
assert generated.CallState.render(empty, "appointments") == "[]"
assert generated.CallState.render(empty, "caller_phone") == "none recorded yet."
assert generated.CallState.render(empty, "caller_phone") == generated.CallState.EMPTY_TEXT
assert generated.CallState.render(None, "caller_phone") == generated.CallState.EMPTY_TEXT
# False and 0 are values, not the absence of one.
assert generated.CallState.render(state, "accepted") == "false"
assert generated.CallState.render(state, "count") == "0"

# A batch is all or nothing: a valid list beside a value its field refuses
# leaves nothing behind. A retry uses the same call state and only commits once
# every value fits.
before_batch = snapshot(state)
try:
    state.save_batch({
        "appointments": [{"scheduled_date": "2026-03-19", "scheduled_time": "09:30",
                          "appointment_type": "haircut"}],
        "count": "not-a-number",
    })
except generated.StateRefused as refused:
    assert refused.message.startswith("count: "), refused.message
else:
    raise AssertionError("a batch with one bad value saved the rest")
assert snapshot(state) == before_batch, (snapshot(state), before_batch)
state.save_result("book", {
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
assert generated.CallState.render(state, "appointments") == "[]"
assert generated.CallState.render(state, "caller_reason") == "[]"

# The step that reads the caller's number back and gets a yes.
state.save_result("confirm_number", {"caller_phone": "+34600111222", "summary": "read back and agreed"})

# The caller books, and then books again, and then changes their mind about the
# second one. Two appointments and two reasons, in the order they gave them.
booked = [
    ("2026-03-19", "09:30", "haircut", "create_booking"),
    ("2026-03-26", "14:00", "dry_cut", "cancel_booking"),
]
for day, at, service, reason in booked:
    state.save_result(
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
# The validated model, which is what the state holds. Plain data is made only
# where a value leaves for a framework, by save_result and plain.
assert type(state.appointments[0]).__name__ == "Appointment", type(state.appointments[0])

assert len(state.appointments) == 2, state.appointments
assert str(state.appointments[0].scheduled_date) == "2026-03-19", state.appointments

# A value outside the declared set. Refused where it enters, the message names
# the field and lists what was allowed, and the previous contents survive.
before = json.dumps(
    to_jsonable_python({
        "appointments": state.appointments,
        "caller_phone": state.caller_phone,
        "caller_reason": state.caller_reason,
    }),
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
        "valid date",
    ),
):
    try:
        generated.STEP_RESULTS["book"].parse(bad)
    except generated.StateRefused as refused:
        assert field in refused.message, (field, refused.message)
        assert allowed in refused.message, (allowed, refused.message)
    else:
        raise AssertionError("a value outside its declared type entered the state: " + repr(bad))

after = json.dumps(
    to_jsonable_python({
        "appointments": state.appointments,
        "caller_phone": state.caller_phone,
        "caller_reason": state.caller_reason,
    }),
    sort_keys=True,
)
assert after == before, "a refused value changed the state"

# A phone number of the wrong shape, refused the same way and with its shape
# named. The shape is checked here and never written into the schema.
try:
    generated.STEP_RESULTS["confirm_number"].parse({"caller_phone": "600 111 222", "summary": "x"})
except generated.StateRefused as refused:
    assert "caller_phone" in refused.message, refused.message
    assert "phone number" in refused.message, refused.message
else:
    raise AssertionError("a phone number of the wrong shape entered the state")

# The two email types, against the real email-validator in this project. Nothing
# below is reachable from a unit test: the library is what decides whether an
# address is one, and the display-name reading is a flag on its own call.
#
# The step that takes them. reminder_email is not a field the model fills: it
# comes off the pair through a dotted assign, so one answer fills both values and
# the model is never asked for the address twice.
state.save_result("take_contact", {"booked_for": {"name": "Fred Bloggs", "email": "Fred.Bloggs@EXAMPLE.com"},
     "summary": "recorded"},
)
# Normalized on the way in, which is what makes the saved value plain text a
# later tool can use as it stands.
assert state.reminder_email == "Fred.Bloggs@example.com", state.reminder_email
assert state.booked_for.email == "Fred.Bloggs@example.com", state.booked_for

# A wrong address is refused where it enters, naming the field and the format,
# with the library's own reason after it so the model can correct itself. And the
# previous contents survive.
kept = (state.reminder_email, to_jsonable_python(state.booked_for))
try:
    state.save_result("take_contact", {"booked_for": {"name": "Fred", "email": "fred dot bloggs at example dot com"},
         "summary": "x"},
    )
except generated.StateRefused as refused:
    assert "booked_for" in refused.message, refused.message
    assert "email address" in refused.message, refused.message
else:
    raise AssertionError("a value that is not an email address entered the state")
assert (state.reminder_email, to_jsonable_python(state.booked_for)) == kept

# Then the value the run ends with, so the expected state below is one the
# conversation produced.
state.save_result("take_contact", {"booked_for": {"name": "Fred Bloggs", "email": "fred.bloggs@example.com"},
     "summary": "recorded"},
)

# What the model actually writes when a caller says an address out loud, and
# what it gets back for each mistake. This is the layer the remaining risk lives
# in: the model hears "fred dot bloggs at example dot com" and has one turn to
# write it. A refusal that said only "expected an email address" would leave it
# guessing which part was wrong, so every one of these has to name the defect.
for said in (
    "fred dot bloggs at example dot com",
    "fred.bloggs at example.com",
    "fred.bloggs@example",
    "fred bloggs@example.com",
    "Fred Bloggs fred.bloggs@example.com",
    "fred.bloggs@example..com",
):
    try:
        state.save_batch({"reminder_email": said})
    except generated.StateRefused as refused:
        reason = refused.message
        assert reason.startswith("reminder_email: "), (said, reason)
        # The library's own sentence, which is the half that says what to fix.
        # Its wording is the library's, so this asserts that something specific
        # followed the field rather than matching any one message.
        assert "email address" in reason and len(reason.split(": ", 1)[1]) > 10, (said, reason)
    else:
        raise AssertionError("a spoken address written down wrong was accepted: " + repr(said))

# A blank result value is no value yet, not a wrong one: it reads as absent, so
# the refusal says the field is missing instead of calling "" a bad address.
try:
    generated.STEP_RESULTS["take_contact"].parse({"booked_for": "", "summary": "x"})
except generated.StateRefused as refused:
    assert refused.message == "booked_for: Field required", refused.message
else:
    raise AssertionError("a blank result value was accepted as a contact")

# The pair is a model the author wrote, so it is filled by its two fields.
pair = generated.Contact.model_validate({"name": "Fred Bloggs", "email": "fred.bloggs@example.com"})
assert (pair.name, pair.email) == ("Fred Bloggs", "fred.bloggs@example.com"), pair
try:
    generated.Contact.model_validate({"name": "Fred Bloggs", "email": "not-an-address"})
except Exception as refused:
    assert "email address" in str(refused), str(refused)
else:
    raise AssertionError("a pair holding no address was accepted")

# A prompt reads one part of the pair through the same flat walk a declared
# shape's field goes through.
probe = generated.` + stateExpr + `
probe.booked_for = pair
assert probe.lookup("booked_for__name")[1] == "Fred Bloggs"
assert generated._render("For {{booked_for__name}}", probe) == "For Fred Bloggs"
assert probe.lookup("booked_for__missing")[1] is None

# And neither type puts a format keyword in the schema the model is sent, which
# is the one thing a Pydantic-native EmailStr would have done.
def schema_keys(node):
    """Every key anywhere in a schema, so a keyword is found wherever it nests."""
    if isinstance(node, dict):
        for key, value in node.items():
            yield key
            yield from schema_keys(value)
    elif isinstance(node, list):
        for value in node:
            yield from schema_keys(value)


for step, result in generated.STEP_RESULTS.items():
    keys = set(schema_keys(result.tool_parameters()))
    assert not keys & {"format", "pattern"}, (step, keys)

# The state as a prompt reads it: compact JSON, never a Python repr.
rendered = generated.CallState.render(state, "appointments")
assert rendered.startswith('[{"'), rendered
assert "'" not in rendered, rendered
assert "None" not in rendered, rendered
assert generated.CallState.render(state, "caller_reason") == '["create_booking","cancel_booking"]'

# The finish schema goes out with no $ref and no $defs left in it. Measured
# against the provider: a $ref inside one tool property comes back 200 with the
# model inventing field names for the nested object, so every result would be
# refused where it entered and nothing would say why.
from pydantic import TypeAdapter

nested = TypeAdapter(list[generated.Appointment]).json_schema()
assert "$ref" in json.dumps(nested), "the fixture stopped producing a $ref to resolve"
resolved = generated._tool_schema(nested, nested.get("$defs", {}))
assert "$ref" not in json.dumps(resolved), json.dumps(resolved)
assert "$defs" not in resolved, sorted(resolved)
# And the resolution put the referenced object's own fields in place.
assert resolved["items"]["properties"]["scheduled_date"]["type"] == "string", resolved
for step, result in generated.STEP_RESULTS.items():
    one = result.tool_parameters()
    assert "$ref" not in json.dumps(one), (step, one)
    assert "$defs" not in json.dumps(one), (step, one)

# And the whole declared state, field for field, against the one expectation
# both targets read.
final = {
    "appointments": to_jsonable_python(state.appointments),
    "caller_phone": state.caller_phone,
    "caller_reason": state.caller_reason,
    "reminder_email": state.reminder_email,
    # The pair is a model, so it is compared the way a prompt renders it.
    "booked_for": to_jsonable_python(state.booked_for),
}
assert final == EXPECTED, json.dumps(final, indent=2, sort_keys=True)
print("typed state: the scripted conversation ends with the expected state")
`
}
