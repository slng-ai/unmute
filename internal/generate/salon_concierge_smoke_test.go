//go:build smoke

package generate

import "testing"

// Target-wide smoke tests already exercise LiveKit TaskGroup and Pipecat
// FlowManager dispatch. These journeys keep the salon-specific contract: the
// generated adapters, one real SQLite store, and the exact saved outcomes.
func TestSmokeSalonConciergeLiveKitJourneys(t *testing.T) {
	runLiveKitSmokeScript(t, "salon-concierge", nil, nil, salonLiveKitJourneysSmokeScript)
}

func TestSmokeSalonConciergePipecatJourneys(t *testing.T) {
	runPipecatSmokeScript(t, "salon-concierge", nil, nil, salonPipecatJourneysSmokeScript)
}

const salonStaleStoreSmokePrelude = `
stale_database_path = (
    Path(tempfile.gettempdir())
    / "unmute-salon-concierge"
    / str(os.getpid())
    / "salon.db"
)
stale_database_path.parent.mkdir(parents=True, exist_ok=True)
for suffix in ("", "-journal", "-wal", "-shm"):
    stale_database_path.with_name(stale_database_path.name + suffix).write_bytes(b"stale")
`

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
database_dir = tempfile.TemporaryDirectory()
database_path = Path(database_dir.name) / "salon.db"
tool_modules = {
    name: importlib.import_module(f"tools.{name}") for name in tool_names
}
assert {module._DB_PATH for module in tool_modules.values()} == {stale_database_path}
for suffix in ("", "-journal", "-wal", "-shm"):
    assert not stale_database_path.with_name(stale_database_path.name + suffix).exists()
for module in tool_modules.values():
    module._DB_PATH = database_path

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
    with sqlite3.connect(database_path) as database:
        return database.execute(
            "SELECT booking_id, customer_id, service, slot_id, status FROM bookings"
        ).fetchall()


def complaint_rows():
    with sqlite3.connect(database_path) as database:
        return database.execute(
            "SELECT customer_id, summary, requested_resolution FROM complaints"
        ).fetchall()
`

const salonLiveKitJourneysSmokeScript = `"""Smoke check: salon journeys on LiveKit."""
import asyncio
import importlib
import json
import os
import sqlite3
import tempfile
from datetime import date, timedelta
from importlib.metadata import version
from pathlib import Path
from types import SimpleNamespace

assert version("livekit-agents") == "1.6.10"

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

` + salonStaleStoreSmokePrelude + `
import agent  # noqa: E402
from livekit.agents import llm  # noqa: E402
` + salonStoreSmokePrelude + `

def recording_task(task_type, chat_ctx=None):
    class RecordingTask(task_type):
        def __init__(self):
            kwargs = {} if chat_ctx is None else {"chat_ctx": chat_ctx}
            super().__init__(**kwargs)
            self.completions = []

        def complete(self, result):
            self.completions.append(result)

    return RecordingTask()


def run_context(userdata, call_id):
    return SimpleNamespace(
        userdata=userdata,
        function_call=SimpleNamespace(call_id=call_id),
    )


async def confirm(userdata, draft, question, answer):
    chat_ctx = llm.ChatContext.empty()
    chat_ctx.add_message(role="assistant", content=question)
    chat_ctx.add_message(role="user", content=answer)
    messages = [item for item in chat_ctx.items if isinstance(item, llm.ChatMessage)]
    assert [(item.role, item.raw_text_content) for item in messages] == [
        ("assistant", question),
        ("user", answer),
    ], "confirmation must be a fresh caller turn after the full question"
    task = recording_task(agent.ConfirmBooking, chat_ctx)
    result = {**draft, "confirmed": True}
    await task.finish(run_context(userdata, "confirm-finish"), **result)
    assert task.completions == [result]
    return result


async def apply(userdata, confirmed):
    task = recording_task(agent.ApplyBooking)
    ctx = run_context(userdata, "apply-finish")
    if confirmed["action"] == "create":
        result = await task.create_booking(
            ctx,
            confirmed=confirmed["confirmed"],
            service=confirmed["service"],
            slot_id=confirmed["slot_id"],
        )
    else:
        result = await task.cancel_booking(
            ctx,
            booking_id=confirmed["booking_id"],
            confirmed=confirmed["confirmed"],
        )
    applied = {
        "action": confirmed["action"],
        "booking_id": result["booking_id"],
        "status": result["status"],
        "summary": result["summary"],
    }
    await task.finish(ctx, **applied)
    assert task.completions == [applied]
    return applied


async def create_then_cancel(userdata):
    ctx = run_context(userdata, "prepare-finish")
    prepare = recording_task(agent.PrepareBooking)
    current = await prepare.get_current_date(ctx)
    requested = (date.fromisoformat(current["date"]) + timedelta(days=1)).isoformat()
    available = await prepare.check_availability(
        ctx, date=requested, service="haircut"
    )
    slot_id = available["slots"][-1]["slot_id"]
    draft = {
        "action": "create",
        "booking_id": "",
        "service": "haircut",
        "slot_id": slot_id,
    }
    await prepare.finish(ctx, **draft)
    assert prepare.completions == [draft]
    confirmed = await confirm(
        userdata,
        draft,
        "Would you like me to book that haircut tomorrow at three o'clock?",
        "yes",
    )
    created = await apply(userdata, confirmed)

    prepare = recording_task(agent.PrepareBooking)
    listed = await prepare.list_bookings(ctx)
    assert [item["booking_id"] for item in listed["bookings"]] == [
        created["booking_id"]
    ]
    draft = {
        "action": "cancel",
        "booking_id": created["booking_id"],
        "service": "haircut",
        "slot_id": "",
    }
    await prepare.finish(ctx, **draft)
    assert prepare.completions == [draft]
    confirmed = await confirm(
        userdata,
        draft,
        "Would you like me to cancel that haircut appointment?",
        "cancel it",
    )
    cancelled = await apply(userdata, confirmed)
    assert cancelled["booking_id"] == created["booking_id"]
    return created["booking_id"], slot_id


async def split_verification_then_intent_change():
    chat_ctx = llm.ChatContext.empty()
    chat_ctx.add_message(role="user", content="Jordan Lee, 303555")
    chat_ctx.add_message(role="user", content="0199")
    fragments = [
        item.raw_text_content
        for item in chat_ctx.items
        if isinstance(item, llm.ChatMessage) and item.role == "user"
    ]
    assert fragments == ["Jordan Lee, 303555", "0199"]

    userdata = agent.Userdata()
    verification = recording_task(agent.CustomerVerification, chat_ctx)
    ctx = run_context(userdata, "verification-finish")
    verified = await verification.find_or_create_customer(
        ctx, name="Jordan Lee", phone="3035550199"
    )
    await verification.finish(ctx, **verified)
    assert verification.completions == [verified]
    userdata.customer_id = verified["customer_id"]
    userdata.customer_name = verified["customer_name"]

    complaint = "Actually, I need to complain about my last visit."
    chat_ctx.add_message(role="user", content=complaint)
    interrupted = recording_task(agent.PrepareBooking, chat_ctx)
    await interrupted.to_complaints(run_context(userdata, "intent-change"))
    assert len(interrupted.completions) == 1
    transfer = interrupted.completions[0]
    assert isinstance(transfer, agent._TaskTransfer)
    assert isinstance(transfer.agent, agent.ComplaintSpecialist)
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
        "Maya Tess", "2025550187"
    )
    assert customer["status"] == "created"
    actions.clear()
    userdata = agent.Userdata(
        customer_id=customer["customer_id"],
        customer_name=customer["customer_name"],
    )
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
    assert intent_actions[0][1] == {
        "name": "Jordan Lee",
        "phone": "3035550199",
    }
    assert verified["customer_name"] == "Jordan Lee"
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
database_dir.cleanup()
print("livekit salon journeys smoke ok")
`

const salonPipecatJourneysSmokeScript = `"""Smoke check: salon journeys on Pipecat."""
import asyncio
import importlib
import json
import os
import sqlite3
import tempfile
from datetime import date, timedelta
from importlib.metadata import version
from pathlib import Path

assert version("pipecat-ai") == "1.7.0"

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

` + salonStaleStoreSmokePrelude + `
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


async def booking_flow(worker, context, *, action, booking_id=""):
    worker._manage_booking_results = {}
    worker._manage_booking_active_step = "prepare_booking"
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
            {"service": "haircut", "date": requested}, None
        )
        slot_id = available["slots"][-1]["slot_id"]
        draft = {
            "action": "create",
            "booking_id": "",
            "service": "haircut",
            "slot_id": slot_id,
        }
    else:
        listed = await bot._flow_tool_list_bookings({}, None, state=worker.state)
        assert [item["booking_id"] for item in listed["bookings"]] == [booking_id]
        slot_id = ""
        draft = {
            "action": "cancel",
            "booking_id": booking_id,
            "service": "haircut",
            "slot_id": slot_id,
        }

    prepared, next_node = await worker._manage_booking_finish_prepare_booking(
        draft, None
    )
    assert prepared == {"status": "ok", "result": draft}
    assert next_node is not None
    context.add_message({
        "role": "assistant",
        "content": (
            "Would you like me to book that haircut?"
            if action == "create"
            else "Would you like me to cancel that haircut appointment?"
        ),
    })
    context.add_message({
        "role": "user",
        "content": "yes" if action == "create" else "cancel it",
    })
    confirmation = {**draft, "confirmed": True}
    confirmed, next_node = await worker._manage_booking_finish_confirm_booking(
        confirmation, None
    )
    assert confirmed == {"status": "ok", "result": confirmation}
    assert next_node is not None

    if action == "create":
        result = await bot._flow_tool_create_booking(
            {
                "confirmed": confirmation["confirmed"],
                "service": confirmation["service"],
                "slot_id": confirmation["slot_id"],
            },
            None,
            state=worker.state,
        )
        booking_id = result["booking_id"]
    else:
        result = await bot._flow_tool_cancel_booking(
            {
                "booking_id": confirmation["booking_id"],
                "confirmed": confirmation["confirmed"],
            },
            None,
            state=worker.state,
        )
    applied = {
        "action": action,
        "booking_id": booking_id,
        "status": result["status"],
        "summary": result["summary"],
    }
    finished, next_node = await worker._manage_booking_finish_apply_booking(
        applied, None
    )
    assert finished == {"status": "ok"} and next_node is None
    assert worker._manage_booking_results == {
        "prepare_booking": draft,
        "confirm_booking": confirmation,
        "apply_booking": applied,
    }
    return booking_id, slot_id


async def split_verification_then_intent_change():
    context = LLMContext()
    context.add_message({"role": "user", "content": "Jordan Lee, 303555"})
    context.add_message({"role": "user", "content": "0199"})
    assert [message["content"] for message in context.get_messages()] == [
        "Jordan Lee, 303555",
        "0199",
    ]
    state = bot.State()
    concierge = bot.ConciergeAgent(state=state, context=context, call_context={})
    await quiet(concierge)
    concierge._verify_customer_results = {}
    concierge._verify_customer_snapshot = (
        [dict(message) for message in context.get_messages()],
        context.tools,
    )
    verified = await bot._flow_tool_find_or_create_customer(
        {"name": "Jordan Lee", "phone": "3035550199"}, None
    )
    finished, next_node = await concierge._verify_customer_finish_customer_verification(
        verified, None
    )
    assert finished == {"status": "ok"} and next_node is None
    assert state.customer_id == verified["customer_id"]
    assert state.customer_name == "Jordan Lee"

    complaint = "Actually, I need to complain about my last visit."
    snapshot = [dict(message) for message in context.get_messages()]
    context.add_message({"role": "user", "content": complaint})
    specialist = bot.BookingSpecialistAgent(
        state=state, context=context, call_context={}
    )
    await quiet(specialist)
    activations = []

    async def activate(name, *, args, deactivate_self):
        activations.append((name, args, deactivate_self))

    specialist.activate_worker = activate
    specialist._manage_booking_results = {}
    specialist._manage_booking_active_step = "prepare_booking"
    specialist._manage_booking_snapshot = (snapshot, context.tools)
    transferred, next_node = (
        await specialist._manage_booking_transfer_prepare_booking_to_complaints(
            {}, None
        )
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
        state=state, context=context, call_context={}
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
        "Maya Tess", "2025550187"
    )
    assert customer["status"] == "created"
    actions.clear()
    state = bot.State(
        customer_id=customer["customer_id"],
        customer_name=customer["customer_name"],
    )
    context = LLMContext()
    worker = bot.BookingSpecialistAgent(state=state, context=context, call_context={})
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
    assert intent_actions[0][1] == {
        "name": "Jordan Lee",
        "phone": "3035550199",
    }
    assert verified["customer_name"] == "Jordan Lee"
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
database_dir.cleanup()
print("pipecat salon journeys smoke ok")
`
