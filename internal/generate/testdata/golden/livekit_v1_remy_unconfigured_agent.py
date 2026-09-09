import asyncio
import json
import logging
import os
from dataclasses import dataclass
from typing import Annotated
import httpx
from dotenv import load_dotenv

from pydantic import BaseModel, Field, TypeAdapter, ValidationError
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

from dev_metrics import dev_llm_node, dev_say, install_dev_metrics


logger = logging.getLogger("remy-fixture")
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


When this step is complete, call `finish`.

`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish` with their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that status and takes the caller from there."""

FIND_SLOT_PROMPT = """# Find a table

You are handling only the table search for this caller. Work one question per turn.

1. Ask for the date and rough time they want.
2. Ask how many people.
3. Call `check_availability` with the date and party size, and offer the open times that come back. Present at most three, as plain spoken options.
4. When the caller picks one, record the date, time, and party size and finish.

Never promise a table that check_availability did not return. If nothing is open, say so plainly and offer the nearest alternatives it returned.


When this step is complete, call `finish`.

`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish` with their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that status and takes the caller from there."""

QUALIFY_EVENT_PROMPT = """# Qualify a private event

You are handling only the event details for this caller. Work one question per turn.

1. Ask what the occasion is.
2. Ask roughly how many guests.
3. Ask the date they have in mind.

When you have all three, record them and finish. Do not quote prices, menus, or availability; the events team handles that after the call.


When this step is complete, call `finish`.

`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish` with their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that status and takes the caller from there."""

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


# --- declared state ----------------------------------------------------------
# Generated from the `shapes:` and the typed `variables:` in agent.yaml. Both
# target frameworks already depend on Pydantic, so nothing here adds one.
#
# Emitted from one place in the compiler for both targets, so the classes, the
# checks and the refusal wording cannot differ between them.


class _StateRefused(Exception):
    """A value that does not fit its declared type, refused where it enters.

    Carried as an exception rather than a return so the write cannot happen by
    accident: the previous contents stay exactly as they were, and the message
    goes back to the model, which is what lets it correct itself on the next
    turn instead of the step recording something wrong.
    """

    def __init__(self, message: str) -> None:
        super().__init__(message)
        self.message = message


def _typed(field, adapter, value):
    """Validate one value entering the declared state."""
    try:
        return adapter.validate_python(value)
    except ValidationError as error:
        first = error.errors()[0]
        where = ".".join(str(part) for part in first["loc"])
        named = f"{field}.{where}" if where else field
        raise _StateRefused(f"{named}: {first['msg']}") from None


def _append_entry(entries, value):
    """One entry onto a declared list, unless it is already on it.

    A step re-entered mid-call can read a value through an explicit prompt
    reference and hand it straight back, which is not a second thing happening. One live call
    entered the booking step four times and finished three of them immediately,
    each with the same appointment it had recorded on the first, so one booking
    became four entries and the caller's recap listed a booking four times.

    An object carries its own identity, so an identical one is the same thing
    reported twice. A plain value is not: two bookings really do give two
    reasons of "create_booking", and both of those count. So the skip is for
    structured entries only.

    Nothing absent is added either, which is how a step that concluded nothing
    this time finishes without inventing an entry.
    """
    if value is None:
        return
    if isinstance(value, (dict, list)) and value in entries:
        return
    entries.append(value)


def _plain(value):
    """A validated value as plain data.

    Plain data is the only shape both frameworks accept back from a tool: one
    refuses a BaseModel outright and drops the whole tool result with a log
    line, the other cannot serialise one at all.
    """
    if isinstance(value, BaseModel):
        return value.model_dump(mode="json")
    if isinstance(value, list):
        return [_plain(entry) for entry in value]
    if isinstance(value, dict):
        return {key: _plain(entry) for key, entry in value.items()}
    return value


def _schema(adapter):
    """One declared type's schema, with every $ref resolved into place.

    Pydantic emits $defs and a $ref for a shape that contains another shape, and
    this is not a formatting preference. Measured on one real request to the
    provider, three ways:

    - the schema as Pydantic emits it, nested inside one tool property with no
      strict flag: accepted with a 200, and the model invented field names for
      the nested object because it never read the definition. Every result would
      then have been refused where it entered, on every call.
    - the same schema with the refs inlined: accepted, and the model filled the
      shape's own fields exactly, the nullable one included.
    - the shape the other target sends, with the $defs hoisted to the
      parameters root and strict on: accepted, and correct. A $defs anywhere but
      that root is a 400 naming the pointer.

    This target nests the schema inside one property and sends no strict flag,
    so it is the first case unless the refs are resolved here.
    """
    schema = adapter.json_schema()
    defs = schema.pop("$defs", {})

    def resolve(node):
        if isinstance(node, list):
            return [resolve(item) for item in node]
        if not isinstance(node, dict):
            return node
        target = node.get("$ref")
        if isinstance(target, str) and target.startswith("#/$defs/"):
            found = defs.get(target.rsplit("/", 1)[1], {})
            siblings = {key: value for key, value in node.items() if key != "$ref"}
            return {**resolve(found), **siblings}
        return {key: resolve(value) for key, value in node.items()}

    return resolve(schema)


_FINISH_TYPES = {
    "confirm_booking": {
    },
    "find_slot": {
    },
    "qualify_event": {
    },
}


def _task_status(values):
    return {"status": "unserved" if values.get("unserved_request") else "completed"}


def _group_status(results):
    return {"status": "unserved" if any(value.get("unserved_request") for value in results.values()) else "completed"}


def _typed_result(step, values):
    """Validate a step's declared results where they enter the state.

    Refused here rather than carried into a later step that assumes it is
    right, and refused on both targets rather than on the one whose framework
    happens to validate tool arguments: one of them validates through Pydantic
    and lets the model self-correct, the other splats raw JSON into the handler.
    """
    if values.get("unserved_request"):
        return {"unserved_request": values["unserved_request"]}
    adapters = _FINISH_TYPES.get(step)
    if not adapters:
        return values
    out = dict(values)
    for name, adapter in adapters.items():
        # Absent goes through the adapter too, rather than being skipped. A
        # field the model left out is a field with no value, and that is what a
        # prompt telling it to leave one out asks for: a value that may be
        # absent validates as None and the append drops it, and a value that
        # may not is refused here with the message that lets the model correct
        # itself. Skipping an absent field instead left the key missing from
        # the result, and the assignment that reads it by name raised a
        # KeyError inside the finish handler on the target whose framework
        # validates no argument of its own.
        out[name] = _plain(_typed(name, adapter, out.get(name)))
    return out


_STATE_TYPES = {
    "caller_phone": TypeAdapter(str),
}
_TASK_ASSIGNMENTS = {
    "confirm_booking": [
    ],
    "find_slot": [
    ],
    "qualify_event": [
    ],
}
_STATE_CONFIRM = {
}
_STATE_DEPENDENCIES = {
}


def _save_result(step, state, values):
    """Validate all assignments before changing any call state."""
    values = _typed_result(step, values)
    if values.get("unserved_request"):
        return values
    pending = {}
    for name, path, append in _TASK_ASSIGNMENTS.get(step, ()):
        value = values
        for part in path.split("."):
            value = value.get(part) if isinstance(value, dict) else None
        if append:
            if value is None:
                continue
            entries = list(getattr(state, name, None) or [])
            _append_entry(entries, value)
            value = entries
        pending[name] = _plain(_typed(name, _STATE_TYPES[name], value))
    _save_batch(state, pending, step=step)
    return values


def _save_batch(state, values, *, step=None, inputs=None):
    """Commit a validated batch and invalidate older results of changed inputs."""
    if not values:
        return
    pending = {name: _plain(_typed(name, _STATE_TYPES[name], value)) for name, value in values.items()}
    unconfirmed: set[str] = set(getattr(state, "_unconfirmed", ()))
    provenance = dict(getattr(state, "_prefetch_provenance", {}))
    affected = {name for name, value in pending.items() if getattr(state, name, None) != value}
    while True:
        more = {name for name, reads in provenance.items() if set(reads) & affected} - affected
        if not more:
            break
        affected.update(more)
    invalidated = affected - pending.keys()
    for name in invalidated:
        provenance.pop(name, None)
    for name, value in pending.items():
        if inputs is not None:
            provenance[name] = tuple(inputs)
        else:
            provenance.pop(name, None)
        if name in _STATE_CONFIRM:
            if _STATE_CONFIRM[name] == step and value is not None and value != "":
                unconfirmed.discard(name)
            else:
                unconfirmed.add(name)
    def current(name):
        return None if name in invalidated else pending.get(name, getattr(state, name, None))
    # Dependencies are acyclic: prefetch can only read earlier entries.
    for _ in range(len(_STATE_DEPENDENCIES) + 1):
        before = set(unconfirmed)
        for name, reads in _STATE_DEPENDENCIES.items():
            if current(name) is not None and current(name) != "" and all(
                source not in unconfirmed and current(source) is not None and current(source) != "" for source in reads
            ):
                unconfirmed.discard(name)
            else:
                unconfirmed.add(name)
        if before == unconfirmed:
            break
    for name in invalidated:
        setattr(state, name, None)
    for name, value in pending.items():
        setattr(state, name, value)
    if hasattr(state, "_unconfirmed"):
        state._unconfirmed = unconfirmed
    if inputs is not None or hasattr(state, "_prefetch_provenance"):
        state._prefetch_provenance = provenance



_STATE_STRUCTURED = {}
_STATE_EMPTY = "none recorded yet."
# The bound on one rendered value, in characters. The same number the router
# bounds a template variable by, because this is the same value travelling the
# same way, and one number cannot be two.
_STATE_VALUE_MAX = 4000


def _state_text(name, value):
    """One value as a prompt reads it.

    Compact JSON for anything declared structured, never a Python repr: a repr
    writes single quotes and None, which is not JSON and is not what any
    provider produced. Words for a declared value with no contents, so a step
    cannot mistake "not yet known" for "known to be nothing".

    A value that was never declared structured renders exactly as it did before
    this existed, which is what keeps every package written before it unchanged.
    """
    if value is None or value == "":
        return _STATE_EMPTY
    if not isinstance(value, str):
        value = json.dumps(_plain(value), separators=(",", ":"), ensure_ascii=False)
    text = str(value)
    if len(text) > _STATE_VALUE_MAX:
        # The length is only knowable here, at run time, so this cannot be a
        # compile-time refusal. What it must not be is silent: a shortened value
        # is a value the model reads as complete. An f-string rather than a
        # placeholder, because this line is emitted into two modules that log
        # through two different libraries and either style prints literally on
        # the other one.
        logger.warning(
            f"declared state: {name} rendered {len(text)} characters and is shortened to "
            f"{_STATE_VALUE_MAX}; a value this long also stops the prompt being cached"
        )
        text = text[:_STATE_VALUE_MAX]
    return text


def _prompt_value(state, name, site=""):
    root, value = _state_lookup(state, name)
    if root in getattr(state, "_unconfirmed", ()) and site != "task:" + _STATE_CONFIRM.get(root, ""):
        return root, None
    return root, value


def _state_lookup(state, name):
    """The value a placeholder names, and the declared name it belongs to.

    A path is authored {{customer.status}} and emitted {{customer__status}}: one
    flat name, because the router substitutes flat names only and both render
    paths have to agree. Everything before the first "__" is the declared value;
    each "__" after it starts a field, read as a dict key or an attribute and as
    None past an absent link, so a field of a record nobody has filled renders
    as the empty words and never raises. The root's name comes back with the
    value because the words for an empty value belong to the root variable.
    """
    root, _, path = name.partition("__")
    value = getattr(state, root, None) if state is not None else None
    for part in path.split("__") if path else ():
        if value is None:
            break
        value = value.get(part) if isinstance(value, dict) else getattr(value, part, None)
    return root, value


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
                    _task_status(event.result),
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
    def llm_node(self, chat_ctx, tools, model_settings):
        return dev_llm_node(self, Agent.default.llm_node, chat_ctx, tools, model_settings)

    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(
            instructions=EVENTS_PROMPT,
            chat_ctx=chat_ctx,
            tts=slng.TTS(api_key=os.environ["SLNG_API_KEY"], voice="aura-2-orion-en", model="slng/deepgram/aura:2-en"),
        )
        if isinstance(chat_ctx, llm.ChatContext):
            self._chat_ctx = chat_ctx.copy()

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
        return Greeter(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_config_update=True, exclude_handoff=True))

    @function_tool
    async def do_event(self, ctx: RunContext) -> dict:
        """The caller is ready to plan their event; run the events flow. When this flow finishes it returns a status. Continue with the caller. Do not run this flow again for the same request. A completed status means the step finished. Read any saved values through your own prompt references. An unserved status means the step could not help. Ask the caller what they need, then use your tools or a handoff."""
        owner_ctx = self.chat_ctx.copy()
        try:
            group = TaskGroup(
                summarize_chat_ctx=False,
                on_task_completed=lambda event: _share_task_result(group, event),
            )
            await group.update_chat_ctx(owner_ctx.copy(exclude_instructions=True, exclude_config_update=True, exclude_handoff=True), exclude_invalid_function_calls=False)
            group.add(lambda: QualifyEvent(), id="qualify_event", description="qualify event")
            group.add(lambda: ConfirmBooking(), id="confirm_booking", description="confirm booking")
            result = await group
            task_results = result.task_results
        finally:
            await self.update_chat_ctx(owner_ctx, exclude_invalid_function_calls=False)
        return _group_status(task_results)


class Greeter(Agent):
    def llm_node(self, chat_ctx, tools, model_settings):
        return dev_llm_node(self, Agent.default.llm_node, chat_ctx, tools, model_settings)

    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN, initial: bool = False) -> None:
        self._initial = initial
        super().__init__(
            instructions=GREETER_PROMPT,
            chat_ctx=chat_ctx,
        )
        if isinstance(chat_ctx, llm.ChatContext):
            self._chat_ctx = chat_ctx.copy()

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
        await dev_say(self.session, "Hi, this is Remy at Fern and Oak. Are you booking a table, or planning a private event?")

    @function_tool
    async def to_reservations(self, ctx: RunContext):
        """Caller wants to book a table for a normal dine-in visit."""
        return Reservations(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_config_update=True, exclude_handoff=True))

    @function_tool
    async def to_events(self, ctx: RunContext):
        """Caller wants to plan a private event, party, or large group booking."""
        return Events(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_config_update=True, exclude_handoff=True))


class Reservations(Agent):
    def llm_node(self, chat_ctx, tools, model_settings):
        return dev_llm_node(self, Agent.default.llm_node, chat_ctx, tools, model_settings)

    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(
            instructions=RESERVATIONS_PROMPT,
            chat_ctx=chat_ctx,
            tts=slng.TTS(api_key=os.environ["SLNG_API_KEY"], voice="aura-2-orion-en", model="slng/deepgram/aura:2-en"),
        )
        if isinstance(chat_ctx, llm.ChatContext):
            self._chat_ctx = chat_ctx.copy()

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
        return Greeter(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_config_update=True, exclude_handoff=True))

    @function_tool
    async def do_reserve(self, ctx: RunContext) -> dict:
        """The caller is ready to book a table; run the reservation flow. When this flow finishes it returns a status. Continue with the caller. Do not run this flow again for the same request. A completed status means the step finished. Read any saved values through your own prompt references. An unserved status means the step could not help. Ask the caller what they need, then use your tools or a handoff."""
        owner_ctx = self.chat_ctx.copy()
        try:
            group = TaskGroup(
                summarize_chat_ctx=False,
                on_task_completed=lambda event: _share_task_result(group, event),
            )
            await group.update_chat_ctx(owner_ctx.copy(exclude_instructions=True, exclude_config_update=True, exclude_handoff=True), exclude_invalid_function_calls=False)
            group.add(lambda: FindSlot(), id="find_slot", description="find slot")
            group.add(lambda: ConfirmBooking(), id="confirm_booking", description="confirm booking")
            result = await group
            task_results = result.task_results
        finally:
            await self.update_chat_ctx(owner_ctx, exclude_invalid_function_calls=False)
        return _group_status(task_results)


# --- tasks -----------------------------------------------------------------
def _task_result(values: dict, unserved_request: str) -> dict:
    """A step that could not serve a request names it on the way out, so the
    agent that owns the step reads it off the result and takes it from there."""
    if not unserved_request:
        return values
    return {**values, "unserved_request": unserved_request}


class _RetryEmptyTaskResponseMixin:
    _response_tool_call_ids: set[str]

    async def update_chat_ctx(self, chat_ctx, *, exclude_invalid_function_calls=False):
        # History policy owns old tool records, including tools this task cannot run.
        assert isinstance(self, Agent)
        await Agent.update_chat_ctx(self, chat_ctx, exclude_invalid_function_calls=exclude_invalid_function_calls)

    async def update_tools(self, tools):
        assert isinstance(self, Agent)
        history = self.chat_ctx.copy()
        await Agent.update_tools(self, tools)
        await self.update_chat_ctx(history, exclude_invalid_function_calls=False)

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
            async for chunk in dev_llm_node(
                self, Agent.default.llm_node, request_chat_ctx, request_tools, model_settings
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
        if isinstance(chat_ctx, llm.ChatContext):
            self._chat_ctx = chat_ctx.copy()
        self._response_tool_call_ids: set[str] = set()
        self._finish_call_id: str | None = None

    async def on_enter(self) -> None:
        await self.update_chat_ctx(self.chat_ctx.copy(exclude_instructions=True, exclude_config_update=True, exclude_handoff=True))
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
    async def finish(self, ctx: RunContext, unserved_request: Annotated[str, Field(description="Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.")] = "") -> str | None:
        """Save this step's values and finish, or return unserved without saving."""
        if self.done():
            return
        try:
            _values = _save_result("confirm_booking", ctx.userdata, {"unserved_request": unserved_request})
        except _StateRefused as refused:
            logger.warning("finish %s: %s", "confirm_booking", refused.message)
            return f"Not recorded: {refused.message}. Ask again, then call finish with a value that fits."
        self.complete(_values)
        self._finish_call_id = ctx.function_call.call_id

class FindSlot(_RetryEmptyTaskResponseMixin, AgentTask[dict]):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(instructions=FIND_SLOT_PROMPT, chat_ctx=chat_ctx)
        if isinstance(chat_ctx, llm.ChatContext):
            self._chat_ctx = chat_ctx.copy()
        self._response_tool_call_ids: set[str] = set()
        self._finish_call_id: str | None = None

    async def on_enter(self) -> None:
        await self.update_chat_ctx(self.chat_ctx.copy(exclude_instructions=True, exclude_config_update=True, exclude_handoff=True))
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
    async def finish(self, ctx: RunContext, unserved_request: Annotated[str, Field(description="Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.")] = "") -> str | None:
        """Save this step's values and finish, or return unserved without saving."""
        if self.done():
            return
        try:
            _values = _save_result("find_slot", ctx.userdata, {"unserved_request": unserved_request})
        except _StateRefused as refused:
            logger.warning("finish %s: %s", "find_slot", refused.message)
            return f"Not recorded: {refused.message}. Ask again, then call finish with a value that fits."
        self.complete(_values)
        self._finish_call_id = ctx.function_call.call_id

class QualifyEvent(_RetryEmptyTaskResponseMixin, AgentTask[dict]):
    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(instructions=QUALIFY_EVENT_PROMPT, chat_ctx=chat_ctx)
        if isinstance(chat_ctx, llm.ChatContext):
            self._chat_ctx = chat_ctx.copy()
        self._response_tool_call_ids: set[str] = set()
        self._finish_call_id: str | None = None

    async def on_enter(self) -> None:
        await self.update_chat_ctx(self.chat_ctx.copy(exclude_instructions=True, exclude_config_update=True, exclude_handoff=True))
        # The task's own instructions describe this step; let them drive the opening.
        self.session.generate_reply()

    @function_tool
    async def finish(self, ctx: RunContext, unserved_request: Annotated[str, Field(description="Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.")] = "") -> str | None:
        """Save this step's values and finish, or return unserved without saving."""
        if self.done():
            return
        try:
            _values = _save_result("qualify_event", ctx.userdata, {"unserved_request": unserved_request})
        except _StateRefused as refused:
            logger.warning("finish %s: %s", "qualify_event", refused.message)
            return f"Not recorded: {refused.message}. Ask again, then call finish with a value that fits."
        self.complete(_values)
        self._finish_call_id = ctx.function_call.call_id


# --- session ---------------------------------------------------------------
def prewarm(proc: JobProcess) -> None:
    # The FLOOR: how long silence has to last before the runtime treats the
    # caller as finished. From pace: balanced.
    #
    # Always explicit. Leaving it off inherited Silero's own 0.55s, which is
    # slower than the turn detector wants and was never anybody's choice. Hard
    # floor is 0.25s; the turn detector raises at session start below it.
    #
    # Lowering this alone does not shorten a turn. The ceiling is in the session's
    # turn_handling endpointing below, and that is where a 2.5s turn came from.
    proc.userdata["vad"] = silero.VAD.load(min_silence_duration=0.3)
server = AgentServer()
server.setup_fnc = prewarm
if os.getenv("UNMUTE_LOCAL_RUN") == "1":
    # One browser call needs one warm spare, not one index build per CPU.
    server.update_options(num_idle_processes=1, initialize_process_timeout=60.0)


@server.rtc_session(agent_name="remy-fixture-livekit")
async def entrypoint(ctx: JobContext) -> None:
    require_env()
    session = AgentSession[Userdata](
        userdata=Userdata(),
        stt=slng.STT(api_key=os.environ["SLNG_API_KEY"], model="slng/deepgram/nova:3-en"),
        llm=openai.LLM(api_key=os.environ["OPENAI_API_KEY"], model="gpt-4o-mini", temperature=0.4),
        tts=slng.TTS(api_key=os.environ["SLNG_API_KEY"], voice="aura-2-thalia-en", model="slng/deepgram/aura:2-en"),
        turn_handling=TurnHandlingOptions(
            turn_detection=inference.TurnDetector(version="v1-mini"),
            # The CEILING, and the shortest wait the runtime will consider.
            # pace: balanced. Without this the streaming defaults apply and
            # max_delay is 2.5s, which no package could reach.
            endpointing={"mode": "dynamic", "min_delay": 0.3, "max_delay": 1.6},
            interruption={"enabled": True},
            preemptive_generation={"enabled": True},
        ),
        vad=ctx.proc.userdata["vad"],
        user_away_timeout=15,
    )


    # Inert unless the dev loop set UNMUTE_DEV_METRICS. Reads session events, so
    # it never touches the tracer provider an opt-in trace export would own.
    install_dev_metrics(session, call_id=ctx.room.name)

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
