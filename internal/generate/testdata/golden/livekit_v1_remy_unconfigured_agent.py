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
from livekit.agents.beta.workflows import TaskCompletedEvent, TaskGroup
from livekit.agents.voice import MetricsCollectedEvent
from livekit.plugins import openai, silero, slng

from dev_metrics import install_dev_metrics


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


When this step is complete, call `finish` with: sent.

`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there."""

FIND_SLOT_PROMPT = """# Find a table

You are handling only the table search for this caller. Work one question per turn.

1. Ask for the date and rough time they want.
2. Ask how many people.
3. Call `check_availability` with the date and party size, and offer the open times that come back. Present at most three, as plain spoken options.
4. When the caller picks one, record the date, time, and party size and finish.

Never promise a table that check_availability did not return. If nothing is open, say so plainly and offer the nearest alternatives it returned.


When this step is complete, call `finish` with: date, party_size, time.

`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there."""

QUALIFY_EVENT_PROMPT = """# Qualify a private event

You are handling only the event details for this caller. Work one question per turn.

1. Ask what the occasion is.
2. Ask roughly how many guests.
3. Ask the date they have in mind.

When you have all three, record them and finish. Do not quote prices, menus, or availability; the events team handles that after the call.


When this step is complete, call `finish` with: date, headcount, occasion.

`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there."""

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


async def _share_task_result(group: TaskGroup, event: TaskCompletedEvent) -> None:
    """Fill LiveKit's empty task tool output before the next shared task."""
    finish_call_id = getattr(event.agent_task, "_finish_call_id", None)
    if finish_call_id is None:
        raise RuntimeError("completed task has no successful finish call")
    task_output = next(
        (
            item
            for item in reversed(event.agent_task.chat_ctx.items)
            if isinstance(item, llm.FunctionCallOutput)
            and item.name == "finish"
            and not item.is_error
            and item.call_id == finish_call_id
        ),
        None,
    )
    if task_output is None:
        raise RuntimeError("completed task has no matching successful finish output")

    shared_ctx = group.chat_ctx.copy()
    shared_output = shared_ctx.get_by_id(task_output.id)
    if shared_output is None:
        raise RuntimeError("completed task finish output is absent from its group")
    if not isinstance(shared_output, llm.FunctionCallOutput):
        raise RuntimeError("completed task finish output has an invalid type")
    if (
        shared_output.name != task_output.name
        or shared_output.call_id != task_output.call_id
        or shared_output.is_error
    ):
        raise RuntimeError("completed task finish output changed identity")

    shared_ctx.remove(shared_output)
    shared_ctx.insert(
        shared_output.model_copy(
            update={
                "output": json.dumps(
                    {"task_id": event.task_id, "result": event.result},
                    sort_keys=True,
                )
            }
        )
    )

    await group.update_chat_ctx(
        shared_ctx,
        exclude_invalid_function_calls=False,
    )


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
        # This opening turn withholds the agent's own handoffs (B3: an agent that can
        # hand the call back before it has said anything ping-pongs). The framework's
        # on-enter tool flag would hide them for the rest of the call instead, because
        # its filter follows the context of everything this reply starts: a live call
        # offered one specialist nothing but its delegate for ten turns while the
        # caller asked for another one (B: salon handoffs, 2026-08-20).
        self.session.generate_reply(tools=[t.id for t in self.tools if t.id not in {"back_to_greeter"}])

    @function_tool
    async def back_to_greeter(self, ctx: RunContext):
        """Caller wants something else, or to start over."""
        return Greeter(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True))

    @function_tool
    async def do_event(self, ctx: RunContext) -> dict:
        """The caller is ready to plan their event; run the events flow. When this flow finishes it returns its result to you. That result is the final outcome for this request: relay it to the caller and continue. Do not run this flow again for the same request. A result carrying `unserved_request` means a step could not serve that request and handed it back. The caller is still owed it: after one short line about the result, act on that request in the same turn with your own tools or a handoff. Never end the turn without it and never tell the caller you cannot."""
        group = TaskGroup(
            chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True),
            summarize_chat_ctx=False,
            on_task_completed=lambda event: _share_task_result(group, event),
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
        # This opening turn withholds the agent's own handoffs (B3: an agent that can
        # hand the call back before it has said anything ping-pongs). The framework's
        # on-enter tool flag would hide them for the rest of the call instead, because
        # its filter follows the context of everything this reply starts: a live call
        # offered one specialist nothing but its delegate for ten turns while the
        # caller asked for another one (B: salon handoffs, 2026-08-20).
            self.session.generate_reply(tools=[t.id for t in self.tools if t.id not in {"to_reservations", "to_events"}])
            return
        await self.session.say("Hi, this is Remy at Fern and Oak. Are you booking a table, or planning a private event?")

    @function_tool
    async def to_reservations(self, ctx: RunContext):
        """Caller wants to book a table for a normal dine-in visit."""
        return Reservations(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True))

    @function_tool
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
        # This opening turn withholds the agent's own handoffs (B3: an agent that can
        # hand the call back before it has said anything ping-pongs). The framework's
        # on-enter tool flag would hide them for the rest of the call instead, because
        # its filter follows the context of everything this reply starts: a live call
        # offered one specialist nothing but its delegate for ten turns while the
        # caller asked for another one (B: salon handoffs, 2026-08-20).
        self.session.generate_reply(tools=[t.id for t in self.tools if t.id not in {"back_to_greeter"}])

    @function_tool
    async def back_to_greeter(self, ctx: RunContext):
        """Caller wants something else, or to start over."""
        return Greeter(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True))

    @function_tool
    async def do_reserve(self, ctx: RunContext) -> dict:
        """The caller is ready to book a table; run the reservation flow. When this flow finishes it returns its result to you. That result is the final outcome for this request: relay it to the caller and continue. Do not run this flow again for the same request. A result carrying `unserved_request` means a step could not serve that request and handed it back. The caller is still owed it: after one short line about the result, act on that request in the same turn with your own tools or a handoff. Never end the turn without it and never tell the caller you cannot."""
        group = TaskGroup(
            chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True),
            summarize_chat_ctx=False,
            on_task_completed=lambda event: _share_task_result(group, event),
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
def _task_result(values: dict, unserved_request: str) -> dict:
    """A step that could not serve a request names it on the way out, so the
    agent that owns the step reads it off the result and takes it from there."""
    if not unserved_request:
        return values
    return {**values, "unserved_request": unserved_request}


class _RetryEmptyTaskResponseMixin:
    _response_tool_call_ids: set[str]

    async def llm_node(self, chat_ctx, tools, model_settings):
        # Every generated user of this mixin is an AgentTask; narrow that
        # invariant here so the emitted project type-checks without a new base.
        assert isinstance(self, Agent)
        # ponytail: keyed on the SDK's own marker rather than the placeholder
        # wording, and the emitted-code test is what catches it changing.
        #
        # The delegate that started this task is still running, so the framework
        # injects that call plus a placeholder output reading "The tool call is
        # still in progress." into the context (voice/generation.py,
        # _inject_running_tool_calls). For the agent that made the call that is
        # right: it stops the model re-issuing a call already in flight. A task
        # is a different agent with a different prompt, and its opening turn
        # reads an unfinished tool call it never made: it then answers with
        # nothing, or apologises for a failure that did not happen, which the
        # caller hears as the agent breaking. Reproduced on 3 of 3 scripted
        # salon calls (B: task opened silent after delegate, 2026-08-21).
        running_placeholders = {
            item.call_id
            for item in chat_ctx.items
            if isinstance(item, llm.FunctionCall)
            and item.extra.get("__lk_running_placeholder__")
        }
        if running_placeholders:
            chat_ctx = chat_ctx.copy()
            chat_ctx.items = [
                item
                for item in chat_ctx.items
                if getattr(item, "call_id", None) not in running_placeholders
            ]
        completed_tool_call_ids = {
            item.call_id
            for item in chat_ctx.items
            if isinstance(item, llm.FunctionCallOutput)
            and item.call_id in self._response_tool_call_ids
        }
        post_tool = bool(completed_tool_call_ids)
        finish_tool = llm.ToolContext(tools).get_function_tool("finish")
        if finish_tool is None:
            raise RuntimeError("task retry has no finish tool")
        finish_only = False
        request_tools: list[llm.Tool] = tools
        request_chat_ctx = chat_ctx
        for attempt in range(3):
            has_response = False
            async for chunk in Agent.default.llm_node(
                self, request_chat_ctx, request_tools, model_settings
            ):
                if isinstance(chunk, str):
                    has_response = has_response or bool(chunk.strip())
                elif isinstance(chunk, llm.ChatChunk) and chunk.delta is not None:
                    delta = chunk.delta
                    tool_calls = delta.tool_calls or []
                    if finish_only and tool_calls:
                        allowed = [call for call in tool_calls if call.name == "finish"]
                        if len(allowed) != len(tool_calls):
                            logger.warning(
                                "task post-tool reply tried another non-finish tool; "
                                "ignoring it"
                            )
                            chunk = chunk.model_copy(
                                update={
                                    "delta": delta.model_copy(
                                        update={"tool_calls": allowed}
                                    )
                                }
                            )
                            delta = chunk.delta
                            if delta is None:
                                raise RuntimeError("task retry lost its response delta")
                            tool_calls = allowed
                    self._response_tool_call_ids.update(
                        call.call_id for call in tool_calls
                    )
                    has_response = has_response or bool(
                        tool_calls or (delta.content or "").strip()
                    )
                yield chunk
            if has_response:
                self._response_tool_call_ids.difference_update(
                    completed_tool_call_ids
                )
                return
            if attempt < 2:
                logger.warning("task response was empty; retrying %d/2", attempt + 1)

                if attempt == 0 and post_tool:
                    finish_only = True
                    request_tools = [finish_tool]

                # Copy the original each time. Responses API reuses a previous
                # response when the context is unchanged, so each retry needs
                # a distinct instruction as well as a fresh context object.
                request_chat_ctx = chat_ctx.copy()
                instructions_index = request_chat_ctx.index_by_id(
                    "lk.agent_task.instructions"
                )
                if instructions_index is None:
                    raise RuntimeError("task retry has no instruction message")
                instructions = request_chat_ctx.items[instructions_index]
                if not isinstance(instructions, llm.ChatMessage):
                    raise RuntimeError("task retry instruction has an invalid type")
                instruction_text = instructions.raw_text_content
                if not instruction_text:
                    raise RuntimeError("task retry instruction is empty")
                if finish_only:
                    recovery = (
                        "The response after the tool result was empty. Use the tool "
                        "result already in context. Do not repeat that operation or "
                        "call another operation. Produce the task's next valid "
                        "response now, using what the caller has already told you "
                        "rather than asking again; call finish only if the task is "
                        "complete."
                        if attempt == 0
                        else "The finish retry was also empty. This is the second "
                        "retry. Use the existing tool result without repeating any "
                        "operation. Produce the task's next valid response now; call "
                        "finish only if the task is complete."
                    )
                else:
                    # "Do not ask again" is the load-bearing half.
                    #
                    # Without it a retry points the model at the task instructions
                    # and it restarts the script: observed on a live call, the
                    # caller answered "tomorrow if possible", the response came
                    # back empty, and the retry asked "what day would you like?"
                    # again. The caller's turn is in the copied context the whole
                    # time; the instruction just has to say to use it.
                    recovery = (
                        "The previous response was empty. Follow the current task "
                        "instructions and produce its next valid response now. The "
                        "caller's turns are already in context: use what they have "
                        "already told you, and never ask again for something they "
                        "have answered."
                        if attempt == 0
                        else "The prior retry was also empty. This is the second "
                        "retry. Follow the current task instructions and produce "
                        "its next non-empty valid response now, using what the "
                        "caller has already said rather than asking again."
                    )
                request_chat_ctx.items[instructions_index] = instructions.model_copy(
                    update={
                        "content": [instruction_text + "\n\n" + recovery]
                    }
                )

        logger.warning("task response stayed empty after two retries")
        if finish_only:
            yield (
                "I couldn't finish after the last tool result. Please ask me to "
                "check the current state before trying again."
            )
        else:
            yield "Sorry, I couldn't complete that. Please try again."


class ConfirmBooking(_RetryEmptyTaskResponseMixin, AgentTask[dict]):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(instructions=CONFIRM_BOOKING_PROMPT, chat_ctx=chat_ctx)
        self._response_tool_call_ids: set[str] = set()
        self._finish_call_id: str | None = None

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
    async def finish(self, ctx: RunContext, sent: bool, unserved_request: Annotated[str, Field(description="Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.")] = "") -> None:
        """Record the result of this step and finish. complete() is the sole
        resolution; do not relay anything after it."""
        result = _task_result({"sent": sent}, unserved_request)
        self.complete(result)
        # Record only after complete() succeeds: a rejected duplicate cannot
        # replace the winner, and there is no await for the callback to race.
        self._finish_call_id = ctx.function_call.call_id

class FindSlot(_RetryEmptyTaskResponseMixin, AgentTask[dict]):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(instructions=FIND_SLOT_PROMPT, chat_ctx=chat_ctx)
        self._response_tool_call_ids: set[str] = set()
        self._finish_call_id: str | None = None

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
    async def finish(self, ctx: RunContext, date: str, party_size: int, time: str, unserved_request: Annotated[str, Field(description="Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.")] = "") -> None:
        """Record the result of this step and finish. complete() is the sole
        resolution; do not relay anything after it."""
        result = _task_result({"date": date, "party_size": party_size, "time": time}, unserved_request)
        self.complete(result)
        # Record only after complete() succeeds: a rejected duplicate cannot
        # replace the winner, and there is no await for the callback to race.
        self._finish_call_id = ctx.function_call.call_id

class QualifyEvent(_RetryEmptyTaskResponseMixin, AgentTask[dict]):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(instructions=QUALIFY_EVENT_PROMPT, chat_ctx=chat_ctx)
        self._response_tool_call_ids: set[str] = set()
        self._finish_call_id: str | None = None

    async def on_enter(self) -> None:
        # The task's own instructions describe this step; let them drive the opening.
        self.session.generate_reply()

    @function_tool
    async def finish(self, ctx: RunContext, date: str, headcount: int, occasion: str, unserved_request: Annotated[str, Field(description="Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.")] = "") -> None:
        """Record the result of this step and finish. complete() is the sole
        resolution; do not relay anything after it."""
        result = _task_result({"date": date, "headcount": headcount, "occasion": occasion}, unserved_request)
        self.complete(result)
        # Record only after complete() succeeds: a rejected duplicate cannot
        # replace the winner, and there is no await for the callback to race.
        self._finish_call_id = ctx.function_call.call_id


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


    # Inert unless the dev loop set UNMUTE_DEV_METRICS. Reads session events, so
    # it never touches the tracer provider an opt-in trace export would own.
    install_dev_metrics(session)

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
