//go:build smoke

package generate

import "testing"

// Target-wide smoke tests already exercise LiveKit TaskGroup and Pipecat
// FlowManager dispatch. These journeys keep the salon-specific contract: the
// generated adapters, one shared in-process store, and the exact saved
// outcomes.
func TestSmokeSalonConciergeLiveKitJourneys(t *testing.T) {
	runLiveKitSmokeScript(t, "salon-concierge", nil, nil, salonLiveKitJourneysSmokeScript)
}

func TestSmokeSalonConciergePipecatJourneys(t *testing.T) {
	runPipecatSmokeScript(t, "salon-concierge", nil, nil, salonPipecatJourneysSmokeScript)
}

// The tools/salon.py handler is copied once per tool (see its own module
// docstring), so a module-level dict would give each copy a private store.
// The product's fix is one state module parked in sys.modules that every
// copy reaches through sys.modules.setdefault. This prelude asserts that
// property directly, by identity, rather than reaching for a private
// attribute by name: the last time this rotted (2026-08-24), the store moved
// from a per-module `_DB_PATH` SQLite file to this in-memory module, and a
// test pinned to the old name broke instead of catching the real thing worth
// keeping, which is that every handler still shares one store.
const salonStoreSmokePrelude = `
tool_names = (
    "cancel_booking",
    "check_availability",
    "create_booking",
    "find_or_create_customer",
    "get_current_date",
    "list_bookings",
    "modify_booking",
    "record_complaint",
)
tool_modules = {
    name: importlib.import_module(f"tools.{name}") for name in tool_names
}

state_objects = {}
for _tool_name, _module in tool_modules.items():
    assert hasattr(_module, "_state"), (
        f"tools.{_tool_name} has no _state attribute; found {sorted(vars(_module))!r}"
    )
    state_objects[_tool_name] = _module._state

distinct_state_ids = {id(state) for state in state_objects.values()}
assert len(distinct_state_ids) == 1, (
    "expected every tool handler to share one in-process state object, "
    f"found distinct objects: {state_objects!r}"
)
shared_state = next(iter(state_objects.values()))

registered_state = sys.modules.get("unmute_salon_state")
assert registered_state is shared_state, (
    "the shared state every tool handler resolved to is not the module "
    f"registered under sys.modules['unmute_salon_state']; found {registered_state!r}"
)
for _attribute in ("customers", "bookings", "complaints", "lock"):
    assert hasattr(shared_state, _attribute), (
        f"the shared state module has no {_attribute!r} attribute; "
        f"found {sorted(vars(shared_state))!r}"
    )

actions = []
for tool_name in (
    "find_or_create_customer",
    "get_current_date",
    "check_availability",
    "create_booking",
    "list_bookings",
    "cancel_booking",
    "record_complaint",
):
    module = tool_modules[tool_name]
    original = getattr(module, tool_name)

    def recorded(*args, _name=tool_name, _original=original, **kwargs):
        result = _original(*args, **kwargs)
        actions.append((_name, dict(kwargs), result))
        return result

    setattr(module, tool_name, recorded)


def booking_rows():
    return sorted(
        (
            booking_id,
            booking["customer_id"],
            booking["service"],
            booking["slot_id"],
            booking["status"],
        )
        for booking_id, booking in shared_state.bookings.items()
    )


def complaint_rows():
    return [
        (
            complaint["customer_id"],
            complaint["summary"],
            complaint["requested_resolution"],
        )
        for complaint in shared_state.complaints.values()
    ]
`

// The booking flow used to be a three-task group (draft, confirm, apply); it
// is now one Booking task/step that calls the mutation tool directly with
// confirmed=True once it has a yes (2026-08-21, "Cut the salon concierge's
// LLM round trips per turn"). Customer verification takes a phone number
// only — no name, no customer_name variable. These journeys follow that
// shape.
const salonLiveKitJourneysSmokeScript = `"""Smoke check: salon journeys on LiveKit."""
import asyncio
import importlib
import json
import os
import sys
from datetime import date, timedelta
from importlib.metadata import version
from types import SimpleNamespace

assert version("livekit-agents") == "1.6.10"

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import agent  # noqa: E402
from livekit.agents import llm  # noqa: E402
` + salonStoreSmokePrelude + `

def quiet_activity():
    """A stand-in for the AgentActivity a real session would attach. Every
    tool below speaks an announcement through self.session.say before doing
    its work (2026-08-23, "Speak before a tool runs, on both code drivers"),
    and that property raises without a live activity. Setting _activity is
    the documented seam AgentTask itself uses (agent.py: self._activity),
    not a private hack around it.
    """
    return SimpleNamespace(session=SimpleNamespace(say=lambda *_a, **_k: None))


def recording_task(task_type, chat_ctx=None):
    class RecordingTask(task_type):
        def __init__(self):
            kwargs = {} if chat_ctx is None else {"chat_ctx": chat_ctx}
            super().__init__(**kwargs)
            self.completions = []
            self._activity = quiet_activity()

        def complete(self, result):
            self.completions.append(result)

    return RecordingTask()


def run_context(userdata, call_id):
    return SimpleNamespace(
        userdata=userdata,
        function_call=SimpleNamespace(call_id=call_id),
    )


async def create_then_cancel(userdata):
    ctx = run_context(userdata, "booking-finish")
    task = recording_task(agent.Booking)
    current = await task.get_current_date(ctx)
    requested = (date.fromisoformat(current["date"]) + timedelta(days=1)).isoformat()
    available = await task.check_availability(ctx, date=requested, service="haircut")
    slot_id = available["slots"][-1]["slot_id"]
    created = await task.create_booking(
        ctx, confirmed=True, service="haircut", slot_id=slot_id
    )
    assert created["status"] == "booked", created
    applied = {
        "action": "create",
        "booking_id": created["booking_id"],
        "status": created["status"],
        "summary": created["summary"],
    }
    await task.finish(ctx, **applied)
    assert task.completions == [applied]

    task = recording_task(agent.Booking)
    listed = await task.list_bookings(ctx)
    assert [item["booking_id"] for item in listed["bookings"]] == [
        applied["booking_id"]
    ]
    cancelled = await task.cancel_booking(
        ctx, booking_id=applied["booking_id"], confirmed=True
    )
    assert cancelled["status"] == "cancelled", cancelled
    finished = {
        "action": "cancel",
        "booking_id": cancelled["booking_id"],
        "status": cancelled["status"],
        "summary": cancelled["summary"],
    }
    await task.finish(ctx, **finished)
    assert task.completions == [finished]
    return applied["booking_id"], slot_id


async def split_verification_then_intent_change():
    chat_ctx = llm.ChatContext.empty()
    chat_ctx.add_message(role="user", content="303")
    chat_ctx.add_message(role="user", content="5550199")
    fragments = [
        item.raw_text_content
        for item in chat_ctx.items
        if isinstance(item, llm.ChatMessage) and item.role == "user"
    ]
    assert fragments == ["303", "5550199"], (
        "confirmation must be a fresh caller turn joining both fragments, "
        f"got {fragments!r}"
    )

    userdata = agent.Userdata()
    verification = recording_task(agent.CustomerVerification, chat_ctx)
    ctx = run_context(userdata, "verification-finish")
    verified = await verification.find_or_create_customer(ctx, phone="3035550199")
    finish_result = {
        "customer_id": verified["customer_id"],
        "customer_phone": "".join(fragments),
        "status": verified["status"],
        "summary": verified["summary"],
    }
    await verification.finish(ctx, **finish_result)
    assert verification.completions == [finish_result]
    userdata.customer_id = verified["customer_id"]

    complaint = "Actually, I need to complain about my last visit."
    chat_ctx.add_message(role="user", content=complaint)
    interrupted = recording_task(agent.Booking, chat_ctx)
    await interrupted.to_complaints(run_context(userdata, "intent-change"))
    assert len(interrupted.completions) == 1
    transfer = interrupted.completions[0]
    assert isinstance(transfer, agent._TaskTransfer)
    assert isinstance(transfer.agent, agent.ComplaintSpecialist)
    transfer.agent._activity = quiet_activity()
    assert userdata.customer_id == verified["customer_id"]
    complaint_messages = [
        item.raw_text_content
        for item in transfer.agent.chat_ctx.items
        if isinstance(item, llm.ChatMessage)
        and item.role == "user"
        and item.raw_text_content == complaint
    ]
    assert complaint_messages == [complaint]
    recorded = await transfer.agent.record_complaint(
        run_context(userdata, "record-complaint"),
        requested_resolution="A manager callback",
        summary="The last visit did not meet expectations.",
    )
    assert recorded["status"] == "recorded"
    return verified


async def main():
    customer = tool_modules["find_or_create_customer"].find_or_create_customer(
        "2025550187"
    )
    assert customer["status"] == "created"
    actions.clear()
    userdata = agent.Userdata(customer_id=customer["customer_id"])
    booking_id, slot_id = await create_then_cancel(userdata)
    booking_actions = [name for name, _, _ in actions]
    assert booking_actions == [
        "get_current_date",
        "check_availability",
        "create_booking",
        "list_bookings",
        "cancel_booking",
    ]
    assert booking_rows() == [
        (booking_id, customer["customer_id"], "haircut", slot_id, "cancelled")
    ]

    verified = await split_verification_then_intent_change()
    intent_actions = actions[len(booking_actions):]
    assert [name for name, _, _ in intent_actions] == [
        "find_or_create_customer",
        "record_complaint",
    ]
    assert intent_actions[0][1] == {"phone": "3035550199"}
    assert verified["status"] == "created"
    assert complaint_rows() == [
        (
            verified["customer_id"],
            "The last visit did not meet expectations.",
            "A manager callback",
        )
    ]
    assert booking_rows() == [
        (booking_id, customer["customer_id"], "haircut", slot_id, "cancelled")
    ]


asyncio.run(main())
print("livekit salon journeys smoke ok")
`

const salonPipecatJourneysSmokeScript = `"""Smoke check: salon journeys on Pipecat."""
import asyncio
import importlib
import json
import os
import sys
from datetime import date, timedelta
from importlib.metadata import version
from types import SimpleNamespace

assert version("pipecat-ai") == "1.7.0"

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from pipecat.flows import NO_RESPONSE  # noqa: E402
from pipecat.frames.frames import Frame  # noqa: E402
from pipecat.processors.aggregators.llm_context import LLMContext  # noqa: E402
from pipecat.processors.frame_processor import (  # noqa: E402
    FrameDirection,
    FrameProcessor,
)
from pipecat.services.llm_service import FunctionCallParams, LLMService  # noqa: E402
from pipecat.services.settings import LLMSettings  # noqa: E402


class FakeLLM(LLMService):
    def __init__(self, *_args, **_kwargs):
        super().__init__(settings=LLMSettings(model="salon-probe"))

    async def process_frame(self, frame: Frame, direction: FrameDirection):
        await super().process_frame(frame, direction)
        await self.push_frame(frame, direction)


class Passthrough(FrameProcessor):
    async def process_frame(self, frame: Frame, direction: FrameDirection):
        await super().process_frame(frame, direction)
        await self.push_frame(frame, direction)


for builder_name in list(vars(bot)):
    if builder_name.startswith("build_") and builder_name.endswith("_llm"):
        setattr(bot, builder_name, FakeLLM)
    elif builder_name.startswith("build_") and builder_name.endswith("_tts"):
        setattr(bot, builder_name, Passthrough)
` + salonStoreSmokePrelude + `

async def quiet(worker):
    async def no_op(*_args, **_kwargs):
        pass

    worker.queue_frame = no_op
    worker.flush_pipeline = no_op
    # _direct_tool's own announce (record_complaint's "Noting that down.")
    # goes straight through params.llm.push_frame rather than worker.queue_frame,
    # and this worker's FakeLLM never sees a StartFrame, so that push logs a
    # framework error and drops the frame. Harmless to the assertions below,
    # which never inspect spoken announcements, but quiet it the same way.
    if getattr(worker, "llm", None) is not None:
        worker.llm.push_frame = no_op


async def booking_flow(worker, context, *, action, booking_id=""):
    # Every mutation and lookup tool below speaks an announcement through
    # flow_manager.worker.queue_frame before doing its work (2026-08-23,
    # "Speak before a tool runs, on both code drivers"). worker.queue_frame is
    # already a no-op from quiet(worker), so a flow_manager stand-in exposing
    # just that worker is enough; get_current_date has no announce and never
    # touches flow_manager at all.
    flow_manager = SimpleNamespace(worker=worker)
    worker._manage_booking_results = {}
    worker._manage_booking_active_step = "booking"
    worker._manage_booking_snapshot = (
        [dict(message) for message in context.get_messages()],
        context.tools,
    )
    if action == "create":
        current = await bot._flow_tool_get_current_date({}, None)
        requested = (
            date.fromisoformat(current["date"]) + timedelta(days=1)
        ).isoformat()
        available = await bot._flow_tool_check_availability(
            {"service": "haircut", "date": requested}, flow_manager
        )
        slot_id = available["slots"][-1]["slot_id"]
        result = await bot._flow_tool_create_booking(
            {"confirmed": True, "service": "haircut", "slot_id": slot_id},
            flow_manager,
            state=worker.state,
        )
        booking_id = result["booking_id"]
    else:
        listed = await bot._flow_tool_list_bookings(
            {}, flow_manager, state=worker.state
        )
        assert [item["booking_id"] for item in listed["bookings"]] == [booking_id]
        slot_id = ""
        result = await bot._flow_tool_cancel_booking(
            {"booking_id": booking_id, "confirmed": True},
            flow_manager,
            state=worker.state,
        )

    expected_status = "booked" if action == "create" else "cancelled"
    assert result["status"] == expected_status, result
    applied = {
        "action": action,
        "booking_id": booking_id,
        "status": result["status"],
        "summary": result["summary"],
    }
    finished, next_node = await worker._manage_booking_finish_booking(applied, None)
    assert finished == {"status": "ok"} and next_node is None
    assert worker._manage_booking_results == {"booking": applied}
    return booking_id, slot_id


async def split_verification_then_intent_change():
    context = LLMContext()
    context.add_message({"role": "user", "content": "303"})
    context.add_message({"role": "user", "content": "5550199"})
    assert [message["content"] for message in context.get_messages()] == [
        "303",
        "5550199",
    ]
    state = bot.State()
    concierge = bot.ConciergeAgent(
        state=state, context=context, call_context={}, slng_session_id="smoke-session"
    )
    await quiet(concierge)
    concierge._verify_customer_results = {}
    concierge._verify_customer_snapshot = (
        [dict(message) for message in context.get_messages()],
        context.tools,
    )
    verified = await bot._flow_tool_find_or_create_customer(
        {"phone": "3035550199"}, SimpleNamespace(worker=concierge)
    )
    finished, next_node = await concierge._verify_customer_finish_customer_verification(
        {**verified, "customer_phone": "3035550199"}, None
    )
    assert finished == {"status": "ok"} and next_node is None
    assert state.customer_id == verified["customer_id"]
    assert state.customer_phone == "3035550199", (
        "the confirmed phone must reach the variable the package assigns it to, "
        f"got {state.customer_phone!r}"
    )

    complaint = "Actually, I need to complain about my last visit."
    snapshot = [dict(message) for message in context.get_messages()]
    context.add_message({"role": "user", "content": complaint})
    specialist = bot.BookingSpecialistAgent(
        state=state, context=context, call_context={}, slng_session_id="smoke-session"
    )
    await quiet(specialist)
    activations = []

    async def activate(name, *, args, deactivate_self):
        activations.append((name, args, deactivate_self))

    specialist.activate_worker = activate
    specialist._manage_booking_results = {}
    specialist._manage_booking_active_step = "booking"
    specialist._manage_booking_snapshot = (snapshot, context.tools)
    transferred, next_node = (
        await specialist._manage_booking_transfer_booking_to_complaints({}, None)
    )
    assert transferred == {"transferred": True} and next_node is NO_RESPONSE
    assert len(activations) == 1
    assert activations[0][0] == "complaint_specialist"
    assert activations[0][2] is True
    assert specialist.state is state and state.customer_id == verified["customer_id"]
    complaint_messages = [
        message.get("content")
        for message in context.get_messages()
        if message.get("role") == "user" and message.get("content") == complaint
    ]
    assert complaint_messages == [complaint]

    complaint_worker = bot.ComplaintSpecialistAgent(
        state=state, context=context, call_context={}, slng_session_id="smoke-session"
    )
    await quiet(complaint_worker)
    callbacks = []

    async def result_callback(result, **_kwargs):
        callbacks.append(result)

    await complaint_worker.record_complaint(
        FunctionCallParams(
            function_name="record_complaint",
            tool_call_id="record-complaint",
            arguments={
                "requested_resolution": "A manager callback",
                "summary": "The last visit did not meet expectations.",
            },
            llm=complaint_worker.llm,
            pipeline_worker=complaint_worker,
            context=context,
            result_callback=result_callback,
        ),
        requested_resolution="A manager callback",
        summary="The last visit did not meet expectations.",
    )
    assert len(callbacks) == 1 and callbacks[0]["status"] == "recorded"
    return verified


async def main():
    customer = tool_modules["find_or_create_customer"].find_or_create_customer(
        "2025550187"
    )
    assert customer["status"] == "created"
    actions.clear()
    state = bot.State(customer_id=customer["customer_id"])
    context = LLMContext()
    worker = bot.BookingSpecialistAgent(
        state=state, context=context, call_context={}, slng_session_id="smoke-session"
    )
    await quiet(worker)
    booking_id, slot_id = await booking_flow(worker, context, action="create")
    cancelled_id, _ = await booking_flow(
        worker, context, action="cancel", booking_id=booking_id
    )
    assert cancelled_id == booking_id
    booking_actions = [name for name, _, _ in actions]
    assert booking_actions == [
        "get_current_date",
        "check_availability",
        "create_booking",
        "list_bookings",
        "cancel_booking",
    ]
    assert booking_rows() == [
        (booking_id, customer["customer_id"], "haircut", slot_id, "cancelled")
    ]

    verified = await split_verification_then_intent_change()
    intent_actions = actions[len(booking_actions):]
    assert [name for name, _, _ in intent_actions] == [
        "find_or_create_customer",
        "record_complaint",
    ]
    assert intent_actions[0][1] == {"phone": "3035550199"}
    assert verified["status"] == "created"
    assert complaint_rows() == [
        (
            verified["customer_id"],
            "The last visit did not meet expectations.",
            "A manager callback",
        )
    ]
    assert booking_rows() == [
        (booking_id, customer["customer_id"], "haircut", slot_id, "cancelled")
    ]


asyncio.run(main())
print("pipecat salon journeys smoke ok")
`
