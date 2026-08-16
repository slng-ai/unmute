import asyncio
import json
import logging
import os
from dataclasses import dataclass
from typing import Annotated
import httpx
from dotenv import load_dotenv
from pydantic import Field
from livekit.agents import (
    NOT_GIVEN,
    Agent,
    AgentTask,
    AgentServer,
    AgentSession,
    JobContext,
    JobProcess,
    NotGivenOr,
    RunContext,
    TurnHandlingOptions,
    function_tool,
    inference,
    llm,
    metrics,
)
from livekit.agents.beta.workflows import TaskGroup
from livekit.agents.voice import MetricsCollectedEvent
from livekit.plugins import openai, silero, slng


logger = logging.getLogger("livekit")
logger.setLevel(logging.INFO)

load_dotenv()


# --- prompts ---------------------------------------------------------------

EVENTS_PROMPT = """# Private events

You are Remy, now helping the caller plan a private event. Use what they have already said. Keep every turn to one or two short sentences.

- When you are ready to take the details, call `do_event`. It runs the events flow: qualifying the event, then confirming the details.
- When the flow returns, tell the caller the events team will follow up, and ask if there is anything else.
- If the caller actually wants a normal table, or wants to start over, use `back_to_greeter`.

Do not greet again or re-introduce yourself.
"""

GREETER_PROMPT = """# Remy, the greeter

You are Remy, the phone concierge for Fern and Oak, a small restaurant group. This is a voice call, so keep every turn to one or two short sentences and ask one thing at a time.

Your only job is to greet the caller and send them to the right place.

- If they want to book a table for a normal visit, use `to_reservations`.
- If they want a private event, a party, or a large group, use `to_events`.
- If it is unclear, ask one short question: "Is this for a table, or a private event?"

Do not take dates, names, or numbers yourself. Hand off as soon as the intent is clear, in one natural line, without telling the caller they are being transferred. They stay with Remy for the whole call.
"""

RESERVATIONS_PROMPT = """# Reservations

You are Remy, now helping the caller book a table. Use what they have already said. Keep every turn to one or two short sentences.

- When you are ready to take the booking, call `do_reserve`. It runs the reservation flow: finding a time, then confirming the details.
- When the flow returns, close warmly in one line and ask if there is anything else.
- If the caller actually wants a private event, or wants to start over, use `back_to_greeter`.

Do not greet again or re-introduce yourself.
"""

CONFIRM_BOOKING_PROMPT = """# Confirm and send

You are handling only the confirmation for this caller. Work one question per turn.

1. Ask for the name the booking should be under.
2. Confirm the phone number for the text. If one is already on file, read it back digit by digit and ask if it is right; otherwise ask for one.
3. Ask for a clear yes before sending anything, and wait for it. Never send in the same turn you ask.
4. Only after an explicit yes, call `send_confirmation` with the name, phone, and a one-line summary of the booking.

If the caller declines, send nothing and finish. Do not promise anything beyond the text message.


When this step is complete, call `finish` with: sent."""

FIND_SLOT_PROMPT = """# Find a table

You are handling only the table search for this caller. Work one question per turn.

1. Ask for the date and rough time they want.
2. Ask how many people.
3. Call `check_availability` with the date and party size, and offer the open times that come back. Present at most three, as plain spoken options.
4. When the caller picks one, record the date, time, and party size and finish.

Never promise a table that check_availability did not return. If nothing is open, say so plainly and offer the nearest alternatives it returned.


When this step is complete, call `finish` with: date, party_size, time."""

QUALIFY_EVENT_PROMPT = """# Qualify a private event

You are handling only the event details for this caller. Work one question per turn.

1. Ask what the occasion is.
2. Ask roughly how many guests.
3. Ask the date they have in mind.

When you have all three, record them and finish. Do not quote prices, menus, or availability; the events team handles that after the call.


When this step is complete, call `finish` with: date, headcount, occasion."""

# --- required environment ----------------------------------------------------
# Everything this agent needs to run: the model providers' keys, the connection
# to the orchestrator, every address and token a tool or MCP source names, and
# anything else the package declared. Derived from what the compiler knows it
# requires rather than from the author's `secrets:` block, so a package that
# declares nothing still refuses to start without them. A missing one fails the
# session before the agent answers, rather than at the first tool call.
#
# The phone route's own credentials are deliberately absent: one file serves
# every channel, and demanding carrier credentials would refuse a browser
# session on a phone package. They are listed in .env.example and the runbook.
REQUIRED_ENV = [
    "CHECK_AVAILABILITY_URL",
    "LIVEKIT_API_KEY",
    "LIVEKIT_API_SECRET",
    "LIVEKIT_URL",
    "OPENAI_API_KEY",
    "SEND_CONFIRMATION_URL",
    "SLNG_API_KEY",
]


def require_env() -> None:
    missing = [name for name in REQUIRED_ENV if not os.getenv(name)]
    if missing:
        raise RuntimeError("Missing required environment variables: " + ", ".join(missing))


# --- shared state ------------------------------------------------------------
# Typed session state (SCHEMA 4.4): tasks assign into it, transfers read it.
@dataclass
class Userdata:
    caller_phone: str | None = None


# --- job metadata ------------------------------------------------------------
def _livekit_job_metadata(raw: str) -> dict:
    if not raw:
        return {}
    try:
        metadata = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise RuntimeError("LiveKit job metadata must be valid JSON") from exc
    if not isinstance(metadata, dict):
        raise RuntimeError("LiveKit job metadata must be a JSON object")
    return metadata


# --- dispatched input variables ----------------------------------------------
def _dispatched_call_start(metadata: dict | None = None) -> dict:
    """Input variables arrive with the dispatch: the job metadata in production,
    or UNMUTE_CALL_START for a local `unmute dev --var` session."""
    values = dict((metadata or {}).get("call_start", {}))
    raw = os.getenv("UNMUTE_CALL_START")
    if raw:
        try:
            supplied = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise RuntimeError("UNMUTE_CALL_START must be valid JSON") from exc
        if not isinstance(supplied, dict):
            raise RuntimeError("UNMUTE_CALL_START must be a JSON object")
        # The dispatch wins: env is the local stand-in for it.
        for name, value in supplied.items():
            values.setdefault(name, value)
    missing = []
    if "caller_phone" in values:
        value = values["caller_phone"]
        if not (isinstance(value, str)):
            raise RuntimeError("call_start.caller_phone must be string")
    if missing:
        raise RuntimeError("Missing call_start fields: " + ", ".join(missing))
    return values


def _hydrate_call_start(userdata, values: dict) -> None:
    if "caller_phone" in values:
        userdata.caller_phone = values["caller_phone"]
    return None


# --- agents ----------------------------------------------------------------

class Events(Agent):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(
            instructions=EVENTS_PROMPT,
            chat_ctx=chat_ctx,
            tts=slng.TTS(api_key=os.environ["SLNG_API_KEY"], voice="aura-2-orion-en", model="slng/deepgram/aura:2-en"),
        )

    async def on_enter(self) -> None:
        # This agent took over via handoff; let its own instructions drive the
        # opening (the prompt already says not to re-greet).
        self.session.generate_reply()

    @function_tool(flags=llm.tool_context.ToolFlag.IGNORE_ON_ENTER)
    async def back_to_greeter(self, ctx: RunContext):
        """Caller wants something else, or to start over."""
        return Greeter(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True))

    @function_tool
    async def do_event(self, ctx: RunContext) -> dict:
        """The caller is ready to plan their event; run the events flow. When this flow finishes it returns its result to you. That result is the final outcome for this request: relay it to the caller and continue. Do not run this flow again for the same request."""
        group = TaskGroup(
            chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True),
            summarize_chat_ctx=False,
        )
        group.add(
            lambda: QualifyEvent(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True)),
            id="qualify_event",
            description="qualify event",
        )
        group.add(
            lambda: ConfirmBooking(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True)),
            id="confirm_booking",
            description="confirm booking",
        )
        owner_ctx = self.chat_ctx.copy()
        result = await group
        # N13: the flow's own turns must not land in the owner's context. The
        # TaskGroup merges them back on completion, so restore the pre-flow
        # context and hand the owner only the typed results (merge: results).
        await self.update_chat_ctx(owner_ctx)
        return result.task_results


class Greeter(Agent):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN, initial: bool = False) -> None:
        self._initial = initial
        super().__init__(
            instructions=GREETER_PROMPT,
            chat_ctx=chat_ctx,
        )

    async def on_enter(self) -> None:
        if not self._initial:
            self.session.generate_reply()
            return
        await self.session.say("Hi, this is Remy at Fern and Oak. Are you booking a table, or planning a private event?")

    @function_tool(flags=llm.tool_context.ToolFlag.IGNORE_ON_ENTER)
    async def to_reservations(self, ctx: RunContext):
        """Caller wants to book a table for a normal dine-in visit."""
        return Reservations(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True))

    @function_tool(flags=llm.tool_context.ToolFlag.IGNORE_ON_ENTER)
    async def to_events(self, ctx: RunContext):
        """Caller wants to plan a private event, party, or large group booking."""
        return Events(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True))


class Reservations(Agent):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(
            instructions=RESERVATIONS_PROMPT,
            chat_ctx=chat_ctx,
            tts=slng.TTS(api_key=os.environ["SLNG_API_KEY"], voice="aura-2-orion-en", model="slng/deepgram/aura:2-en"),
        )

    async def on_enter(self) -> None:
        # This agent took over via handoff; let its own instructions drive the
        # opening (the prompt already says not to re-greet).
        self.session.generate_reply()

    @function_tool(flags=llm.tool_context.ToolFlag.IGNORE_ON_ENTER)
    async def back_to_greeter(self, ctx: RunContext):
        """Caller wants something else, or to start over."""
        return Greeter(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True))

    @function_tool
    async def do_reserve(self, ctx: RunContext) -> dict:
        """The caller is ready to book a table; run the reservation flow. When this flow finishes it returns its result to you. That result is the final outcome for this request: relay it to the caller and continue. Do not run this flow again for the same request."""
        group = TaskGroup(
            chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True),
            summarize_chat_ctx=False,
        )
        group.add(
            lambda: FindSlot(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True)),
            id="find_slot",
            description="find slot",
        )
        group.add(
            lambda: ConfirmBooking(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True)),
            id="confirm_booking",
            description="confirm booking",
        )
        owner_ctx = self.chat_ctx.copy()
        result = await group
        # N13: the flow's own turns must not land in the owner's context. The
        # TaskGroup merges them back on completion, so restore the pre-flow
        # context and hand the owner only the typed results (merge: results).
        await self.update_chat_ctx(owner_ctx)
        return result.task_results


# --- tasks -----------------------------------------------------------------

class ConfirmBooking(AgentTask[dict]):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(instructions=CONFIRM_BOOKING_PROMPT, chat_ctx=chat_ctx)

    async def on_enter(self) -> None:
        # The task's own instructions describe this step; let them drive the opening.
        self.session.generate_reply()

    @function_tool
    async def send_confirmation(self, ctx: RunContext, name: Annotated[str, Field(description="The name the booking is under")], phone: Annotated[str, Field(description="Caller phone number in E.164 form")], summary: Annotated[str, Field(description="One-line summary of the booking to include")]) -> dict:
        """Send the caller a confirmation text for their booking. Call only after the caller has agreed to receive it."""
        async with httpx.AsyncClient() as client:
            resp = await client.post(
                os.environ["SEND_CONFIRMATION_URL"],
                json={"name": name, "phone": phone, "summary": summary},
            )
            resp.raise_for_status()
            return resp.json()

    @function_tool
    async def finish(self, ctx: RunContext, sent: bool) -> None:
        """Record the result of this step and finish. complete() is the sole
        resolution; do not relay anything after it."""
        self.complete({"sent": sent})

class FindSlot(AgentTask[dict]):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(instructions=FIND_SLOT_PROMPT, chat_ctx=chat_ctx)

    async def on_enter(self) -> None:
        # The task's own instructions describe this step; let them drive the opening.
        self.session.generate_reply()

    @function_tool
    async def check_availability(self, ctx: RunContext, date: Annotated[str, Field(description="The requested date, e.g. 2026-08-14")], party_size: Annotated[int, Field(description="Number of people")]) -> dict:
        """Find open table times for a date and party size. Returns the available time slots."""
        async with httpx.AsyncClient() as client:
            resp = await client.post(
                os.environ["CHECK_AVAILABILITY_URL"],
                json={"date": date, "party_size": party_size},
            )
            resp.raise_for_status()
            return resp.json()

    @function_tool
    async def finish(self, ctx: RunContext, date: str, party_size: int, time: str) -> None:
        """Record the result of this step and finish. complete() is the sole
        resolution; do not relay anything after it."""
        self.complete({"date": date, "party_size": party_size, "time": time})

class QualifyEvent(AgentTask[dict]):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(instructions=QUALIFY_EVENT_PROMPT, chat_ctx=chat_ctx)

    async def on_enter(self) -> None:
        # The task's own instructions describe this step; let them drive the opening.
        self.session.generate_reply()

    @function_tool
    async def finish(self, ctx: RunContext, date: str, headcount: int, occasion: str) -> None:
        """Record the result of this step and finish. complete() is the sole
        resolution; do not relay anything after it."""
        self.complete({"date": date, "headcount": headcount, "occasion": occasion})


# --- session ---------------------------------------------------------------
def prewarm(proc: JobProcess) -> None:
    proc.userdata["vad"] = silero.VAD.load()
server = AgentServer()
server.setup_fnc = prewarm


@server.rtc_session(agent_name="livekit")
async def entrypoint(ctx: JobContext) -> None:
    require_env()
    session = AgentSession[Userdata](
        userdata=Userdata(),
        stt=slng.STT(api_key=os.environ["SLNG_API_KEY"], model="slng/deepgram/nova:3-en"),
        llm=openai.LLM(api_key=os.environ["OPENAI_API_KEY"], model="gpt-4o-mini", temperature=0.4),
        tts=slng.TTS(api_key=os.environ["SLNG_API_KEY"], voice="aura-2-thalia-en", model="slng/deepgram/aura:2-en"),
        turn_handling=TurnHandlingOptions(
            turn_detection=inference.TurnDetector(version="v1-mini"),
            interruption={"enabled": True},
            preemptive_generation={"enabled": True},
        ),
        vad=ctx.proc.userdata["vad"],
        user_away_timeout=15,
    )


    @session.on("metrics_collected")
    def _on_metrics_collected(ev: MetricsCollectedEvent) -> None:
        metrics.log_metrics(ev.metrics)

    async def _end_if_still_away() -> None:
        await asyncio.sleep(30)  # inactivity.end_after
        if session.user_state == "away":
            session.shutdown()

    @session.on("user_state_changed")
    def _on_user_state_changed(ev) -> None:
        if ev.new_state != "away":
            return
        session.generate_reply(
            instructions="The caller went quiet. Briefly check whether they are still there."
        )
        asyncio.create_task(_end_if_still_away())

    # Input variables land before the session starts, so the greeting and the
    # prompts already see them (I.dispatch).
    _hydrate_call_start(session.userdata, _dispatched_call_start(_livekit_job_metadata(ctx.job.metadata)))
    await session.start(agent=Greeter(initial=True), room=ctx.room)
    await ctx.connect()

    async def _max_duration() -> None:
        await asyncio.sleep(1200)
        session.shutdown()  # conversation.max_duration

    asyncio.create_task(_max_duration())


# No __main__ block: this module is started through livekit-agents' supported
# CLI, `python -m livekit.agents start agent.py`, which imports it and finds the
# `server` above. The older per-script entry point goes through a CLI upstream
# has deprecated and will remove.
