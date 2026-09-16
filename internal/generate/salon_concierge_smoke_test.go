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

func TestSmokePipecatHandoffActivationRunsWithoutNewMessages(t *testing.T) {
	runPipecatSmokeScript(t, "salon-concierge", nil, nil, pipecatHandoffActivationSmokeScript)
}

const pipecatHandoffActivationSmokeScript = `"""An empty handoff payload still starts the receiving worker's reply."""
import asyncio
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from pipecat.frames.frames import (  # noqa: E402
    LLMMessagesAppendFrame,
    LLMRunFrame,
    LLMSetToolsFrame,
    LLMUpdateSettingsFrame,
)
from pipecat.processors.aggregators.llm_context import LLMContext  # noqa: E402


async def main():
    # Exercise the generated override AND the real SDK superclass. The old
    # handoff test replaced activate_worker and only checked its arguments.
    for worker_type in (bot.ConciergeAgent, bot.ComplaintSpecialistAgent):
        worker = worker_type(state=bot.build_state(), context=LLMContext())
        for messages, run_llm, expected in (
            ([], True, 1),
            (None, True, 1),
            ([], False, 0),
            ([], None, 0),
            ([{"role": "developer", "content": "completed"}], True, 1),
            ([{"role": "developer", "content": "completed"}], False, 0),
        ):
            frames = []

            async def capture(frame, *_args, **_kwargs):
                frames.append(frame)

            worker.queue_frame = capture
            await worker.on_activated({"messages": messages, "run_llm": run_llm})
            requests = [f for f in frames if isinstance(f, LLMRunFrame) or (
                isinstance(f, LLMMessagesAppendFrame) and f.run_llm
            )]
            assert len(requests) == expected, (worker_type, messages, run_llm, frames)
            if requests:
                before = frames[:frames.index(requests[0])]
                assert any(isinstance(f, LLMUpdateSettingsFrame) for f in before)
                assert any(isinstance(f, LLMSetToolsFrame) for f in before)
            if not messages:
                assert not any(isinstance(f, LLMMessagesAppendFrame) for f in frames), frames
        await worker.on_activated(None)


asyncio.run(main())
print("handoff activation smoke ok: both workers, empty/nonempty payloads, enabled/disabled replies")
`

// Replay the v3 call's failure through the real finish validator, saved state,
// and injected tool arguments. A slot is opaque text: changing its separators
// to satisfy an Id validator makes the booking tool reject it.
func TestSmokeSalonV3RescheduleLiveKit(t *testing.T) {
	runLiveKitSmokeScript(t, "salon-concierge-v3", nil, nil, salonV3RescheduleScript("agent", "Userdata()"))
}

func TestSmokeSalonV3ReschedulePipecat(t *testing.T) {
	runPipecatSmokeScript(t, "salon-concierge-v3", nil, nil, salonV3RescheduleScript("bot", "build_state()"))
}

func salonV3RescheduleScript(module, stateExpr string) string {
	return `"""Smoke check: preserve a salon slot through finish and a reset move."""
import asyncio
import json
import os
from datetime import timedelta
from functools import partial
from types import SimpleNamespace

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import ` + module + ` as generated  # noqa: E402
from tools import check_availability, create_booking, find_or_create_customer, list_bookings  # noqa: E402

` + livekitRunContextStandIn + `

async def main():
    state = generated.` + stateExpr + `
    customer = find_or_create_customer.find_or_create_customer("+15005550006")
    generated._save_result("verify_customer", state, customer)
    tomorrow = check_availability._booking_today() + timedelta(days=1)
    first = check_availability.check_availability("haircut", tomorrow.isoformat())["slots"][-1]
    booking = create_booking.create_booking(state.customer_phone, "haircut", first["slot_id"], True)
    assert booking["status"] == "booked", booking

    async def ignore(*args, **kwargs):
        pass

    ctx = run_context(state)
    worker = None
    if generated.__name__ == "bot":
        worker = SimpleNamespace(
            state=state, context=generated.LLMContext(), queue_frame=ignore, flush_pipeline=ignore,
            _manage_booking_snapshot=([], []),
        )
        worker._manage_booking_complete_manage_booking = partial(
            generated.ConciergeAgent._manage_booking_complete_manage_booking, worker
        )

    async def save_slot(slot):
        day, service, time = slot["slot_id"].split("|")
        values = dict(appointment_id=booking["booking_id"], appointment_date=day,
                      appointment_service=service, appointment_slot_id=slot["slot_id"],
                      appointment_time=time, unserved_request=None)
        if generated.__name__ == "agent":
            from livekit.agents.llm.utils import validated_arguments
            task = generated.ManageBooking()
            await task.finish(ctx, **validated_arguments(task.finish, values))
            assert task.done(), "finish did not save the exact tool result"
        else:
            worker._manage_booking_active_step = "manage_booking"
            worker._manage_booking_results = {}
            result, _ = await generated.ConciergeAgent._manage_booking_finish_manage_booking(worker, values, None)
            assert result == {"status": "ok"}, result
        assert state.appointment_slot_id == slot["slot_id"]
        prompt = generated._render(generated.CONCIERGE_PROMPT, state, site="agent:concierge")
        assert day in prompt and time in prompt and service in prompt, prompt

    await save_slot(first)
    second_day = (tomorrow + timedelta(days=1)).isoformat()
    second = check_availability.check_availability("haircut", second_day)["slots"][0]
    await save_slot(second)
    if generated.__name__ == "agent":
        task = generated.RescheduleBooking()
        task._activity = SimpleNamespace(session=SimpleNamespace(say=lambda *_a, **_k: None))
        result = await task.modify_booking(ctx, confirmed=True)
    else:
        result = await generated._flow_tool_modify_booking(
            {"confirmed": True}, SimpleNamespace(worker=worker), state
        )
    assert result["status"] == "modified", result
    rows = list_bookings.list_bookings(state.customer_phone)["bookings"]
    assert len(rows) == 1 and rows[0]["booking_id"] == booking["booking_id"], rows
    assert rows[0]["start_time"] == second["start_time"], rows
    print("Exact slot survived finish, state, and injection; booking moved.")


asyncio.run(main())
`
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
//
// Four tools now, not eight: check_availability, list_bookings,
// create_booking, modify_booking, cancel_booking and look_up_customer are
// gone, merged into find_slots (a read) and save_booking (the one mutation).
// customer_status stays a field the tools return; it is no longer a saved
// variable, so nothing here reads it off the state.
const salonStoreSmokePrelude = `
tool_names = (
    "find_or_create_customer",
    "find_slots",
    "record_complaint",
    "save_booking",
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
for tool_name in tool_names:
    module = tool_modules[tool_name]
    original = getattr(module, tool_name)

    def recorded(*args, _name=tool_name, _original=original, **kwargs):
        result = _original(*args, **kwargs)
        actions.append((_name, dict(kwargs), result))
        return result

    setattr(module, tool_name, recorded)


def digits(phone):
    """The store's key for a number the tool hands back in E.164.

    The caller identifier is the phone number. The tool returns it in E.164, one
    shape for every number in the package, and keys its store on the digits alone
    so a caller who says the number any other way reaches the one record. A
    fixture that confuses the two passes for the wrong reason.
    """
    return "".join(character for character in str(phone) if character.isdigit())


_WEEKDAYS = (
    "Monday",
    "Tuesday",
    "Wednesday",
    "Thursday",
    "Friday",
    "Saturday",
    "Sunday",
)


def appointment_value(action, booking_id, slot_id):
    day, service, time = slot_id.split("|")
    # Spelled out from the date rather than read off strftime("%A"), which
    # follows the container's locale: the tool fills the weekday the same way,
    # so a fixture that read it off the host clock would agree with the tool by
    # accident on an English host and disagree everywhere else.
    weekday = _WEEKDAYS[date.fromisoformat(day).weekday()]
    hour = int(time[:2])
    spoken = f"{weekday} at {(hour - 1) % 12 + 1}:{time[3:5]} {'AM' if hour < 12 else 'PM'}"
    return dict(
        booking_id=booking_id, service=service, date=day, spoken=spoken, time=time, action=action
    )


def check_saved_appointment(module, state, appointment):
    assert state.appointment == appointment, state.appointment
    for name, site in (("CONCIERGE_PROMPT", "agent:concierge"),
                       ("COMPLAINT_SPECIALIST_PROMPT", "agent:complaint_specialist")):
        prompt = module._render(getattr(module, name), state, site=site)
        # The spoken phrase too: it is the one field the agent reads out, so a
        # record that reached state without reaching the prompt would leave the
        # agent composing the sentence again, which is what it gets wrong.
        assert appointment["date"] in prompt and appointment["time"] in prompt, prompt
        assert appointment["spoken"] in prompt, prompt


def booking_rows():
    return sorted(
        (
            booking_id,
            booking["customer_phone"],
            booking["service"],
            booking["slot_id"],
            booking["status"],
        )
        for booking_id, booking in shared_state.bookings.items()
    )


def complaint_rows():
    return [
        (
            complaint["customer_phone"],
            complaint["summary"],
            complaint["requested_resolution"],
        )
        for complaint in shared_state.complaints.values()
    ]
`

// The booking flow is a two-step group: verify_customer (skipped once the
// caller's number is confirmed) and manage_booking, which calls the one
// merged save_booking tool directly with confirmed=True once it has a yes
// (2026-08-21, "Cut the salon concierge's LLM round trips per turn"; merged
// onto find_slots/save_booking later). Customer verification takes a phone
// number only — no name, no customer_name variable. These journeys follow
// that shape.
const salonLiveKitJourneysSmokeScript = `"""Smoke check: salon journeys on LiveKit."""
import asyncio
import importlib
import json
import os
import sys
from datetime import date, timedelta
from importlib.metadata import version
from types import SimpleNamespace

_report = json.load(open("compile-report.json"))
# The pin the compiler wrote, read rather than repeated. A literal here fails
# this whole suite on a framework bump, which is a version check dressed as a
# behaviour test: the bump is already held by internal/target, and the thing
# worth asserting here is that the venv installed what the project asked for.
assert version("livekit-agents") == _report["version"], (version("livekit-agents"), _report["version"])

for name in _report["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import agent  # noqa: E402
from livekit.agents import AgentServer, llm  # noqa: E402
import ast  # noqa: E402

source = ast.parse(open("agent.py").read())
local_options = next(node for node in source.body if isinstance(node, ast.If)
                     and "UNMUTE_LOCAL_RUN" in ast.unparse(node.test))
for local in ("", "0", "1"):
    server = AgentServer()
    before = (server._num_idle_processes, server._initialize_process_timeout)
    os.environ["UNMUTE_LOCAL_RUN"] = local
    exec(compile(ast.Module(body=[local_options], type_ignores=[]), "agent.py", "exec"),
         {"os": os, "server": server})
    after = (server._num_idle_processes, server._initialize_process_timeout)
    assert after == ((1, 60.0) if local == "1" else before), after
` + salonStoreSmokePrelude + livekitRunContextStandIn + `

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


async def create_then_cancel(userdata):
    ctx = run_context(userdata, "booking-finish")
    task = recording_task(agent.ManageBooking)
    # The date was pre-fetched before caller verification, as it is on a real call.
    assert userdata.booking_date, "the pre-fetch landed no booking_date"
    requested = (
        date.fromisoformat(userdata.booking_date) + timedelta(days=1)
    ).isoformat()
    available = await task.find_slots(ctx, date=requested, service="haircut")
    slot_id = available["slots"][-1]["slot_id"]
    # The tool ends the step: it saves the appointment it returned and hands the
    # owner a status, and there is no finish call in between. That missing call
    # is the request spec 010 removes.
    created = await task.save_booking(
        ctx, action="book", additional=False, booking_id="", confirmed=True, slot_id=slot_id
    )
    # Nothing comes back: the framework asks this step for no reply, and the
    # owner speaks once when the delegate returns. A value here made the step
    # speak first and the owner speak again.
    assert created is None, created
    # The step completed with the values the tool returned, and the model was
    # never asked for them: one completion, no finish call.
    assert len(task.completions) == 1, task.completions
    assert task.completions[0]["appointment"]["action"] == "book", task.completions
    saved = userdata.appointment
    assert saved["action"] == "book" and saved["service"] == "haircut", saved
    booking_id = saved["booking_id"]
    appointment = appointment_value("book", booking_id, slot_id)
    check_saved_appointment(agent, userdata, appointment)

    # A second book while the caller holds a booking is the tool's own
    # refusal, whatever the model meant by it: a live call answered "move it to
    # the day after tomorrow" with a second booking. An ordinary result, so the
    # step stays open for save_booking's move action.
    task = recording_task(agent.ManageBooking)
    requested = (date.fromisoformat(requested) + timedelta(days=1)).isoformat()
    available = await task.find_slots(ctx, date=requested, service="haircut")
    slot_id = available["slots"][0]["slot_id"]
    refused = await task.save_booking(
        ctx, action="book", additional=False, booking_id="", confirmed=True, slot_id=slot_id
    )
    assert refused["status"] == "has_booking", refused
    assert [row["booking_id"] for row in refused["existing"]] == [booking_id], refused
    assert task.completions == [], task.completions
    moved = await task.save_booking(
        ctx, action="move", additional=False, booking_id=booking_id, confirmed=True, slot_id=slot_id
    )
    assert moved is None, moved
    assert userdata.appointment["booking_id"] == booking_id
    appointment = appointment_value("move", booking_id, slot_id)
    check_saved_appointment(agent, userdata, appointment)

    task = recording_task(agent.ManageBooking)
    listed = await task.find_slots(ctx, date="", service="any")
    assert [item["booking_id"] for item in listed["bookings"]] == [booking_id]
    # A read tool does not end the step; only a mutation the package listed does.
    cancelled = await task.save_booking(
        ctx, action="cancel", additional=False, booking_id=booking_id, confirmed=True, slot_id=""
    )
    assert cancelled is None, cancelled
    appointment = appointment_value("cancel", booking_id, slot_id)
    check_saved_appointment(agent, userdata, appointment)
    return booking_id, slot_id


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
    verification = recording_task(agent.VerifyCustomer, chat_ctx)
    ctx = run_context(userdata, "verification-finish")
    # The lookup ends the step itself, so what comes back is the status, and the
    # values it saved are on the state rather than in a finish call.
    verified = await verification.find_or_create_customer(ctx, phone="3035550199")
    assert verified is None, verified
    assert len(verification.completions) == 1, verification.completions
    saved_phone = userdata.customer_phone
    assert saved_phone == "+3035550199", saved_phone
    # customer_status is not a saved variable: it is a field the lookup
    # returns, carried in the step's own completion rather than on the state.
    customer_status = verification.completions[0]["customer_status"]
    assert customer_status == "created", customer_status

    complaint = "Actually, I need to complain about my last visit."
    chat_ctx.add_message(role="user", content=complaint)
    interrupted = recording_task(agent.ManageBooking, chat_ctx)
    await interrupted.to_complaints(run_context(userdata, "intent-change"))
    assert len(interrupted.completions) == 1
    transfer = interrupted.completions[0]
    assert isinstance(transfer, agent._TaskTransfer)
    assert isinstance(transfer.agent, agent.ComplaintSpecialist)
    transfer.agent._activity = quiet_activity()
    assert userdata.customer_phone == saved_phone
    complaint_messages = [
        item.raw_text_content
        for item in transfer.agent.chat_ctx.items
        if isinstance(item, llm.ChatMessage)
        and item.role == "user"
        and item.raw_text_content == complaint
    ]
    assert complaint_messages == [complaint]

    # record_complaint sits directly on the complaint specialist agent now,
    # not behind a task: filing a complaint is one action, not a step with an
    # order, so there is no "step ended" contract to check, only the tool's
    # own returned result.
    recorded = await transfer.agent.record_complaint(
        run_context(userdata, "record-complaint"),
        requested_resolution="A manager callback",
        summary="The last visit did not meet expectations.",
    )
    assert recorded["status"] == "recorded", recorded
    assert recorded["complaint"]["summary"] == "The last visit did not meet expectations.", recorded
    assert recorded["complaint"]["requested_resolution"] == "A manager callback", recorded
    return {"customer_phone": saved_phone, "customer_status": customer_status}


async def main():
    customer = tool_modules["find_or_create_customer"].find_or_create_customer(
        "2025550187"
    )
    assert customer["customer_status"] == "created"
    actions.clear()
    userdata = agent.Userdata(customer_phone=customer["customer_phone"])
    await agent._prefetch(userdata, None)
    agent._save_result(
        "verify_customer", userdata, {"customer_phone": customer["customer_phone"], "customer_status": customer["customer_status"]}
    )
    booking_id, slot_id = await create_then_cancel(userdata)
    booking_actions = [name for name, _, _ in actions]
    # Pre-fetch calls no tool now: "today" is a clock reading and "caller" reads
    # the call's own from_number, so this journey (which sets a number on the
    # state before driving the block) sees no prefetch entry here at all, not
    # even a skipped one.
    assert booking_actions == [
        "find_slots",
        "save_booking",
        "find_slots",
        # The second book ran and was refused by the backend: has_booking.
        "save_booking",
        "save_booking",
        "find_slots",
        "save_booking",
    ], booking_actions
    assert booking_rows() == [
        (booking_id, digits(customer["customer_phone"]), "haircut", slot_id, "cancelled")
    ]

    verified = await split_verification_then_intent_change()
    intent_actions = actions[len(booking_actions):]
    assert [name for name, _, _ in intent_actions] == [
        "find_or_create_customer",
        "record_complaint",
    ]
    assert intent_actions[0][1] == {"phone": "3035550199"}
    assert verified["customer_status"] == "created"
    assert complaint_rows() == [
        (
            digits(verified["customer_phone"]),
            "The last visit did not meet expectations.",
            "A manager callback",
        )
    ]
    assert booking_rows() == [
        (booking_id, digits(customer["customer_phone"]), "haircut", slot_id, "cancelled")
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

_report = json.load(open("compile-report.json"))
# The pin the compiler wrote, read rather than repeated. A literal here fails
# this whole suite on a framework bump, which is a version check dressed as a
# behaviour test: the bump is already held by internal/target, and the thing
# worth asserting here is that the venv installed what the project asked for.
assert version("pipecat-ai") == _report["version"], (version("pipecat-ai"), _report["version"])

for name in _report["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from pipecat.flows import NO_RESPONSE  # noqa: E402
from pipecat.frames.frames import Frame  # noqa: E402
from pipecat.processors.aggregators.llm_context import LLMContext, LLMSpecificMessage  # noqa: E402
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
    # A direct tool's own announce (record_complaint's "Let me get that written
    # down.") goes straight through params.llm.push_frame rather than
    # worker.queue_frame, and this worker's FakeLLM never sees a StartFrame, so
    # that push logs a framework error and drops the frame. Harmless to the
    # assertions below, which never inspect spoken announcements, but quiet it
    # the same way.
    if getattr(worker, "llm", None) is not None:
        worker.llm.push_frame = no_op


class Flow:
    """Stands in for FlowManager and records the node the delegate started on."""

    nodes = []

    def __init__(self, **_kwargs):
        pass

    async def initialize(self, node):
        Flow.nodes.append(node)


bot.FlowManager = Flow


async def enter(worker, name):
    """Run the real delegate entry and return the node it opened on.

    Through the delegate rather than hand-seeded attributes: the first live
    failure of this feature lived in what the entry sets up and what a step
    transition leaves behind, and a stand-in that wrote those attributes
    itself could not have seen it.
    """
    async def resolved(*_args, **_kwargs):
        pass

    await getattr(worker, name)(SimpleNamespace(result_callback=resolved))
    return Flow.nodes[-1]


async def enter_book(worker):
    return await enter(worker, "book")


def handlers(node):
    """The handlers registered on a node, by function name."""
    return {function.name: function.handler for function in node["functions"]}


def owner_status(context):
    return json.loads(context.get_messages()[-1]["content"])


async def booking_flow(worker, context, *, action, booking_id=""):
    # Every mutation and lookup tool below speaks an announcement through
    # flow_manager.worker.queue_frame before doing its work. worker.queue_frame
    # is already a no-op from quiet(worker), so a flow_manager stand-in exposing
    # just that worker is enough.
    flow_manager = SimpleNamespace(worker=worker)
    node = await enter_book(worker)
    # The number is confirmed, so the plan skips verification and opens on the
    # booking step: the owner request between the two is the one this package
    # no longer makes.
    assert worker._book_plan == ["manage_booking"], worker._book_plan
    assert node["name"] == "manage_booking", node["name"]
    step = handlers(node)
    if action == "book":
        # Same as the LiveKit side: the date was pre-fetched before verification.
        assert worker.state.booking_date, "the pre-fetch landed no booking_date"
        requested = (
            date.fromisoformat(worker.state.booking_date) + timedelta(days=1)
        ).isoformat()
        available = await step["find_slots"](
            {"date": requested, "service": "haircut"}, flow_manager
        )
        slot_id = available["slots"][-1]["slot_id"]
        result, next_node = await step["save_booking"](
            {
                "action": "book",
                "additional": False,
                "booking_id": "",
                "confirmed": True,
                "slot_id": slot_id,
            },
            flow_manager,
        )
        booking_id = worker.state.appointment["booking_id"]
    elif action == "move":
        requested = (date.fromisoformat(worker.state.appointment["date"]) + timedelta(days=1)).isoformat()
        available = await step["find_slots"](
            {"date": requested, "service": "haircut"}, flow_manager)
        slot_id = available["slots"][0]["slot_id"]
        result, next_node = await step["save_booking"](
            {
                "action": "move",
                "additional": False,
                "booking_id": booking_id,
                "confirmed": True,
                "slot_id": slot_id,
            },
            flow_manager)
    else:
        listed = await step["find_slots"]({"date": "", "service": "any"}, flow_manager)
        assert [item["booking_id"] for item in listed["bookings"]] == [booking_id]
        slot_id = shared_state.bookings[booking_id]["slot_id"]
        result, next_node = await step["save_booking"](
            {
                "action": "cancel",
                "additional": False,
                "booking_id": booking_id,
                "confirmed": True,
                "slot_id": "",
            },
            flow_manager,
        )

    # The tool ended the step: the model was never asked for a finish call, the
    # values the tool returned are saved, and the flow is back with the owner.
    assert result == {"status": "ok"} and next_node is None, (result, next_node)
    appointment = appointment_value(action, booking_id, slot_id)
    assert worker._book_results["manage_booking"]["appointment"] == appointment, worker._book_results
    assert worker._book_active_step is None
    check_saved_appointment(bot, worker.state, appointment)
    assert owner_status(context) == {"status": "completed"}, context.get_messages()[-1]
    return booking_id, slot_id


async def verify_then_create():
    """An unverified caller books: verification ends on its lookup, the booking
    step opens, and its first mutation runs.

    Both Pipecat traces the review supplied failed here. The tool that ended
    verification had closed the booking step's mutations, and the caller's
    first booking was refused as if it were a second one.
    """
    state = bot.State()
    await bot._prefetch(state, None)
    context = LLMContext()
    context.add_message({"role": "user", "content": "A haircut tomorrow please, my number is 303 555 0199."})
    signature = LLMSpecificMessage(llm="google", message={"type": "thought_signature", "signature": b"salon-probe"})
    context.add_message(signature)
    worker = bot.ConciergeAgent(state=state, context=context, call_context={})
    await quiet(worker)
    flow_manager = SimpleNamespace(worker=worker)
    node = await enter_book(worker)
    assert worker._book_plan == ["verify_customer", "manage_booking"], worker._book_plan
    assert node["name"] == "verify_customer"
    assert worker._book_snapshot[0][-1] == signature
    assert worker._book_snapshot[0][-1] is not signature
    assert all(isinstance(message, dict) for message in context.get_messages())
    context.add_message(signature)
    status, node = await handlers(node)["find_or_create_customer"]({"phone": "3035550199"}, flow_manager)
    assert status == {"status": "completed"} and node["name"] == "manage_booking", (status, node)
    assert state.customer_phone == "+3035550199", state.customer_phone
    # Entering the booking step leaves verification's ending behind.
    assert worker._book_terminal is None and not worker._book_pending
    step = handlers(node)
    requested = (date.fromisoformat(state.booking_date) + timedelta(days=1)).isoformat()
    available = await step["find_slots"]({"date": requested, "service": "haircut"}, flow_manager)
    slot_id = available["slots"][0]["slot_id"]
    context.add_message({"role": "user", "content": "Yes, book it."})
    context.add_message(signature)
    result, next_node = await step["save_booking"](
        {
            "action": "book",
            "additional": False,
            "booking_id": "",
            "confirmed": True,
            "slot_id": slot_id,
        },
        flow_manager,
    )
    assert result == {"status": "ok"} and next_node is None, (result, next_node)
    booking_id = state.appointment["booking_id"]
    check_saved_appointment(bot, state, appointment_value("book", booking_id, slot_id))
    # The caller's yes arrived after the owner's snapshot, so it is carried back
    # once, right above the status, and the first line is not repeated.
    messages = context.get_messages()
    assert owner_status(context) == {"status": "completed"}, messages[-1]
    assert messages[-2] == {"role": "user", "content": "Yes, book it."}, messages[-3:]
    assert sum(1 for message in messages if isinstance(message, dict) and message.get("role") == "user") == 2, messages
    assert signature in messages, "return lost the owner's provider metadata"

    # A second book while the caller holds a booking is the tool's own
    # refusal, an ordinary result: the step stays open, and moving keeps
    # the booking's identity.
    node = await enter_book(worker)
    assert worker._book_plan == ["manage_booking"], worker._book_plan
    step = handlers(node)
    refused, next_node = await step["save_booking"](
        {
            "action": "book",
            "additional": False,
            "booking_id": "",
            "confirmed": True,
            "slot_id": slot_id,
        },
        flow_manager,
    )
    assert refused["status"] == "has_booking" and next_node is None, refused
    assert worker._book_active_step == "manage_booking" and worker._book_terminal is None
    moved_date = (date.fromisoformat(requested) + timedelta(days=1)).isoformat()
    available = await step["find_slots"]({"date": moved_date, "service": "haircut"}, flow_manager)
    moved_slot = available["slots"][0]["slot_id"]
    result, next_node = await step["save_booking"](
        {
            "action": "move",
            "additional": False,
            "booking_id": booking_id,
            "confirmed": True,
            "slot_id": moved_slot,
        },
        flow_manager,
    )
    assert result == {"status": "ok"} and next_node is None, (result, next_node)
    check_saved_appointment(bot, state, appointment_value("move", booking_id, moved_slot))
    return booking_id, moved_slot


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
        state=state, context=context, call_context={}
    )
    await quiet(concierge)
    flow_manager = SimpleNamespace(worker=concierge)
    node = await enter_book(concierge)
    assert node["name"] == "verify_customer"
    finished, next_node = await handlers(node)["find_or_create_customer"](
        {"phone": "3035550199"}, flow_manager
    )
    # Verification is the group's first step, so ending it hands over to the
    # booking node rather than returning to the owner. It is the owner request
    # between the two that this package no longer makes.
    assert finished == {"status": "completed"}, finished
    assert next_node is not None and next_node["name"] == "manage_booking", next_node
    assert concierge._book_active_step == "manage_booking"
    # customer_status is not a saved variable: it is a field the lookup
    # returns, carried in the flow's own result rather than on the state.
    customer_status = concierge._book_results["verify_customer"]["customer_status"]
    # E.164, not raw digits and not spoken groups. The step returns the number in
    # the one shape tasks/verify-customer.md requires and the customer_phone
    # variable documents, and it has to reach the variable unchanged: a second
    # shape here would put two phone formats back into a package with one.
    assert state.customer_phone == "+3035550199", (
        "the confirmed phone must reach the variable in the shape the step "
        f"returns it, got {state.customer_phone!r}"
    )

    complaint = "Actually, I need to complain about my last visit."
    context.add_message({"role": "user", "content": complaint})
    activations = []

    async def activate(name, *, args, deactivate_self):
        activations.append((name, args, deactivate_self))

    concierge.activate_worker = activate
    transferred, after = await handlers(next_node)["to_complaints"]({}, flow_manager)
    assert transferred == {"transferred": True} and after is NO_RESPONSE
    assert len(activations) == 1
    assert activations[0][0] == "complaint_specialist"
    assert activations[0][2] is True
    assert state.customer_phone == "+3035550199"
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
    # record_complaint sits directly on the complaint specialist worker now,
    # not behind a flow node: filing a complaint is one action, not a step
    # with an order, so there is no "step ended" contract to check here, only
    # the direct tool's own result, delivered through its result_callback.
    callbacks = []

    async def capture(result, **_kwargs):
        callbacks.append(result)

    params = FunctionCallParams(
        function_name="record_complaint",
        tool_call_id="record-complaint-smoke",
        arguments={
            "summary": "The last visit did not meet expectations.",
            "requested_resolution": "A manager callback",
        },
        llm=complaint_worker.llm,
        pipeline_worker=complaint_worker,
        context=context,
        result_callback=capture,
    )
    await complaint_worker.record_complaint(
        params,
        requested_resolution="A manager callback",
        summary="The last visit did not meet expectations.",
    )
    assert len(callbacks) == 1, callbacks
    recorded = callbacks[0]
    assert recorded["status"] == "recorded", recorded
    assert recorded["complaint"]["summary"] == "The last visit did not meet expectations.", recorded
    assert recorded["complaint"]["requested_resolution"] == "A manager callback", recorded
    return {"customer_phone": state.customer_phone, "customer_status": customer_status}


async def main():
    customer = tool_modules["find_or_create_customer"].find_or_create_customer(
        "2025550187"
    )
    assert customer["customer_status"] == "created"
    actions.clear()
    state = bot.State(customer_phone=customer["customer_phone"])
    await bot._prefetch(state, None)
    bot._save_result(
        "verify_customer", state, {"customer_phone": customer["customer_phone"], "customer_status": customer["customer_status"]}
    )
    context = LLMContext()
    worker = bot.ConciergeAgent(
        state=state, context=context, call_context={}
    )
    await quiet(worker)
    booking_id, slot_id = await booking_flow(worker, context, action="book")
    moved_id, slot_id = await booking_flow(worker, context, action="move", booking_id=booking_id)
    assert moved_id == booking_id
    cancelled_id, _ = await booking_flow(
        worker, context, action="cancel", booking_id=booking_id
    )
    assert cancelled_id == booking_id
    booking_actions = [name for name, _, _ in actions]
    # Pre-fetch calls no tool now: "today" is a clock reading and "caller" reads
    # the call's own from_number, so this journey sees no prefetch entry here
    # at all, not even a skipped one.
    assert booking_actions == [
        "find_slots",
        "save_booking",
        "find_slots",
        "save_booking",
        "find_slots",
        "save_booking",
    ]
    assert booking_rows() == [
        (booking_id, digits(customer["customer_phone"]), "haircut", slot_id, "cancelled")
    ]

    fresh_id, fresh_slot = await verify_then_create()
    fresh_actions = [name for name, _, _ in actions[len(booking_actions):]]
    assert fresh_actions == [
        "find_or_create_customer",
        "find_slots",
        "save_booking",
        "save_booking",
        "find_slots",
        "save_booking",
    ], fresh_actions

    verified = await split_verification_then_intent_change()
    intent_actions = actions[len(booking_actions) + len(fresh_actions):]
    assert [name for name, _, _ in intent_actions] == [
        "find_or_create_customer",
        "record_complaint",
    ]
    assert intent_actions[0][1] == {"phone": "3035550199"}
    assert verified["customer_status"] == "existing", verified
    assert complaint_rows() == [
        (
            digits(verified["customer_phone"]),
            "The last visit did not meet expectations.",
            "A manager callback",
        )
    ]
    # A move leaves the store's own status field alone (save_booking.py: _move
    # updates the slot and service but not status), so a booking that was only
    # booked and moved still reads "booked" here.
    assert set(booking_rows()) == {
        (booking_id, digits(customer["customer_phone"]), "haircut", slot_id, "cancelled"),
        (fresh_id, "3035550199", "haircut", fresh_slot, "booked"),
    }, booking_rows()

asyncio.run(main())
print("pipecat salon journeys smoke ok")
`
