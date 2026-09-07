"""Generated Pipecat agent for salon-concierge-v2.

Compiled by `unmute`; do not edit by hand. Prompts, model routes, and the agent
graph are baked in from the package. Secret values are read from the environment
and never written here. The agency model uses the Pipecat workers API: a main
PipelineWorker owns the transport + STT, each agent is an LLMWorker with its own
LLM and voice, and agent_transfer is activate_worker(). Tasks and task groups
run as Pipecat Flows on the owning agent: a delegate tool snapshots the shared
context, a FlowManager walks the steps as nodes, and control returns with only
the typed results.
"""

from __future__ import annotations

import asyncio
import copy
import functools
import inspect
import json
import os
import re
import sys
from dataclasses import dataclass, field
from datetime import datetime
from typing import Annotated, Literal
from urllib.parse import quote
from zoneinfo import ZoneInfo

import aiohttp
import knowledge
import tools.cancel_booking
import tools.check_availability
import tools.create_booking
import tools.find_or_create_customer
import tools.list_bookings
import tools.look_up_customer
import tools.modify_booking
import tools.record_complaint
from dotenv import load_dotenv
from loguru import logger
from pydantic import AfterValidator, BaseModel, Field, TypeAdapter, ValidationError

from pipecat.audio.turn.smart_turn.base_smart_turn import SmartTurnParams
from pipecat.audio.turn.smart_turn.local_smart_turn_v3 import LocalSmartTurnAnalyzerV3
from pipecat.audio.vad.silero import SileroVADAnalyzer
from pipecat.audio.vad.vad_analyzer import VADParams
from pipecat.bus import BusBridgeProcessor
from pipecat.flows import (
    ContextStrategy,
    ContextStrategyConfig,
    FlowManager,
    FlowsFunctionSchema,
    NodeConfig,
    NO_RESPONSE,
)
from pipecat.frames.frames import (
    EndFrame,
    FunctionCallResultProperties,
    LLMMessagesAppendFrame,
    LLMUpdateSettingsFrame,
    TTSSpeakFrame,
)
from pipecat.pipeline.pipeline import Pipeline
from pipecat.pipeline.worker import PipelineParams, PipelineWorker
from pipecat.processors.aggregators.llm_context import LLMContext
from pipecat.processors.aggregators.llm_response_universal import (
    LLMContextAggregatorPair,
    LLMUserAggregatorParams,
)
from pipecat.runner.types import RunnerArguments
from pipecat.runner.utils import create_transport
from pipecat.services.llm_service import FunctionCallParams
from pipecat.services.settings import LLMSettings
from pipecat.transcriptions.language import Language
from pipecat.transports.base_transport import BaseTransport, TransportParams
from pipecat.transports.websocket.fastapi import FastAPIWebsocketParams
from pipecat.turns.user_stop import TurnAnalyzerUserTurnStopStrategy
from pipecat.turns.user_turn_strategies import UserTurnStrategies
from pipecat.turns.user_mute import MuteUntilFirstBotCompleteUserMuteStrategy
from pipecat.workers.llm import LLMWorkerActivationArgs, tool
from pipecat.workers.runner import WorkerRunner

from dev_metrics import dev_metrics_observer
from tracing import (
    TRACE_NAME,
    TracedLLMWorker,
    enable_agent_tracing,
    flush_tracing,
    setup_langfuse_tracing,
)

from pipecat.services.openai.llm import OpenAILLMService
from pipecat_slng import SlngSTTService
from pipecat_slng import SlngTTSService

load_dotenv()

_LOGGING_CONFIGURED = False


def _configure_logging() -> None:
    global _LOGGING_CONFIGURED
    if _LOGGING_CONFIGURED:
        return
    logger.remove()
    logger.add(sys.stderr, level=os.getenv("UNMUTE_LOG_LEVEL", "INFO").upper())
    _LOGGING_CONFIGURED = True


MAIN_NAME = "main"

# Read, split and embed every knowledge base at module import, which is once per
# container rather than once per session.
#
# Pipecat Cloud imports this module and then calls bot() for each session, and it
# keeps a warm container for about five minutes after one ends, so this cost is
# amortised across every session that lands on it. It is deliberately not inside
# bot(): there it would run per session and a caller would wait for it.
#
# A failure here raises during import, so the container never reaches ready and
# the platform refuses the start. That is the intent: an agent that cannot read
# its documents should not take calls.
_configure_logging()
knowledge.build_indexes()
# What every session needs, whatever channel it arrives on. What a *phone call*
# adds is CALL_REQUIRED_ENV below, checked separately, so this package still runs
# in the browser with nothing but model keys (V10/B3).
REQUIRED_ENV = [
    "LANGFUSE_BASE_URL",
    "LANGFUSE_PUBLIC_KEY",
    "LANGFUSE_SECRET_KEY",
    "OPENAI_API_KEY",
    "SLNG_API_KEY",
]


def require_env() -> None:
    missing = [name for name in REQUIRED_ENV if not os.getenv(name)]
    if missing:
        raise RuntimeError(
            f"Missing required environment variables: {', '.join(missing)}"
        )


def _direct_tool(
    fn=None, *, cancel_on_interruption=True, timeout_secs=None, announce=None
):
    """Keep every direct function call terminal, even on malformed input."""

    def decorate(handler):
        signature = inspect.signature(handler)
        declared = set(signature.parameters) - {"self", "params"}

        @functools.wraps(handler)
        async def guarded(*args, **kwargs):
            params = kwargs.get("params")
            if params is None:
                params_index = 1 if "self" in signature.parameters else 0
                params = args[params_index]

            unexpected = sorted(set(kwargs) - declared - {"params"})
            if unexpected:
                allowed = ", ".join(sorted(declared)) or "none"
                await params.result_callback(
                    {
                        "error": (
                            f"Unexpected arguments for {params.function_name}: "
                            f"{', '.join(unexpected)}. Allowed arguments: {allowed}. "
                            "Retry with only allowed arguments."
                        )
                    }
                )
                return

            if announce:
                # Cover the wait: TTSSpeakFrame is a DataFrame, so it is queued
                # and speech starts while the handler body runs. Nothing waits
                # for playout here, which is the point of the line. Queued after
                # the argument check, so a rejected call stays silent, and an
                # interruption discards it in line with cancel_on_interruption.
                await params.llm.push_frame(TTSSpeakFrame(announce))

            resolved = False
            original_result_callback = params.result_callback

            async def resolve(result, **callback_kwargs):
                nonlocal resolved
                resolved = True
                return await original_result_callback(result, **callback_kwargs)

            params.result_callback = resolve
            try:
                return await handler(*args, **kwargs)
            except Exception:
                logger.exception(
                    "tool failed before completing: {}", params.function_name
                )
                if not resolved:
                    await original_result_callback(
                        {
                            "error": (
                                f"{params.function_name} failed before completing. "
                                "Do not claim success; retry only with corrected input."
                            )
                        }
                    )
                    return
                raise
            finally:
                params.result_callback = original_result_callback

        return tool(
            cancel_on_interruption=cancel_on_interruption,
            timeout_secs=timeout_secs,
        )(guarded)

    if fn is not None:
        return decorate(fn)
    return decorate


# Checked at import, which is what makes the container refuse to start rather
# than start and go quiet.
#
# It used to be checked only inside run_bot, once per session. The container
# then reported healthy, the platform marked the deployment ready, the browser
# got a valid answer to its offer — and the failure happened in a background
# task where only the log saw it. A caller heard silence. That is the exact
# trade Principle II calls the worst one available, and two documentation pages
# already promised the opposite: "the container starts, checks the keys the
# agent needs, and stops with the names it did not find" (Wave C, 2026-08-15).
#
# run_bot still calls it, because a session that somehow starts without them
# should fail before the caller hears anything either.
require_env()


# --- the carrier stream ------------------------------------------------------
# Your carrier streams the call's audio straight to Pipecat Cloud, which starts
# this agent. Nothing of yours is in the path, so there is nothing here that
# answers a webhook or holds a call open. The runner has already parsed the
# carrier's handshake by the time run_bot starts; everything below reads that
# parsed result and never re-parses it.

# What a phone call adds to REQUIRED_ENV above, checked only on a phone call: a
# browser or console session on this package reads none of these and must not be
# asked for them. The check runs before the caller hears anything, so a missing
# value can never first show up as a failed transfer on a call somebody is
# paying for.
CALL_REQUIRED_ENV = [
    "MANAGER_PHONE_NUMBER",
    "TWILIO_ACCOUNT_SID",
    "TWILIO_AUTH_TOKEN",
    "TWILIO_PHONE_NUMBER",
]


def _phone_session(runner_args: RunnerArguments) -> dict | None:
    """The parsed carrier handshake, or None when this is not a phone call.

    The runner sets transport_type and call_data while building the transport, so
    this reads facts rather than deriving them. A browser or console session has
    neither and must behave exactly as it does on a package with no telephony.
    """
    if getattr(runner_args, "transport_type", None) != "twilio":
        return None
    missing = [name for name in CALL_REQUIRED_ENV if not os.getenv(name)]
    if missing:
        raise RuntimeError(
            f"Missing environment for a phone call: {', '.join(missing)}"
        )
    call = dict(getattr(runner_args, "call_data", None) or {})
    direction = (call.get("body") or {}).get("direction", "inbound")
    logger.info("phone call {} ({})", call.get("call_id"), direction)
    return call


def _pipeline_audio_rates(phone_call: dict | None) -> dict:
    """Run the pipeline at the carrier's own sample rate, on a phone call only.

    Twilio Media Streams is 8 kHz mono in both directions. Pipecat's defaults are
    16 kHz in and 24 kHz out, so without this every inbound frame is upsampled and
    every outbound frame is downsampled, for the whole call, in exchange for no
    information at all: there is nothing above 4 kHz in the audio to recover. The
    transport's own guide asks for these two values for exactly that reason.

    A browser or console session on the same package keeps the defaults, where
    8 kHz would throw away quality those channels really have.
    """
    if not phone_call:
        return {}
    return {"audio_in_sample_rate": 8000, "audio_out_sample_rate": 8000}


def _transfer_twiml(destination: str, ring_timeout: int) -> str:
    """The whole transfer, as one document your carrier will run in order.

    Announce, dial, then end the original call when the destination leg ends.
    Twilio moves to the next verb after a completed, declined, or unanswered
    dial, so the same final hangup covers every outcome without starting a new
    agent that has none of this call's context.
    """
    return (
        "<Response>"
        "<Say>Connecting you to a colleague now.</Say>"
        f'<Dial answerOnBridge="true" timeout="{ring_timeout}">{destination}</Dial>'
        "<Hangup/>"
        "</Response>"
    )


async def _update_carrier_call(call_id: str, twiml: str) -> None:
    """Replace the live call's document at your carrier, by its own call id.

    This is the one request in the whole project made in the carrier's own words,
    and it is call control rather than provisioning: the call already exists. One
    HTTPS request, so no carrier SDK.
    """
    account_sid = os.environ["TWILIO_ACCOUNT_SID"]
    base = "https://api.twilio.com"
    url = f"{base}/2010-04-01/Accounts/{account_sid}/Calls/{call_id}.json"
    auth = aiohttp.BasicAuth(account_sid, os.environ["TWILIO_AUTH_TOKEN"])
    async with aiohttp.ClientSession() as session:
        async with session.post(url, auth=auth, data={"Twiml": twiml}) as response:
            if response.status < 200 or response.status >= 300:
                raise RuntimeError(
                    f"carrier call update failed with {response.status}: {await response.text()}"
                )


# --- declared state ----------------------------------------------------------
# Generated from the `shapes:` and the typed `variables:` in agent.yaml. Both
# target frameworks already depend on Pydantic, so nothing here adds one.
#
# Emitted from one place in the compiler for both targets, so the classes, the
# checks and the refusal wording cannot differ between them.

_SHAPE_PHONE = re.compile(r"^\+[1-9]\d{6,14}$")


def _shape_phone(value: str) -> str:
    # Empty is not a wrong value, it is no value yet. It is what a declared
    # variable holds before anything fills it, what the state block renders as
    # words, and what a tool hands back for a field it could not fill. Refusing
    # it here deadlocked a live call on both targets: the model had nothing else
    # to send, so every retry was refused the same way and the step never
    # finished. A wrong value is still refused; an absent one is not wrong.
    if value and not _SHAPE_PHONE.match(value):
        raise ValueError(
            "expected a phone number in E.164, one leading plus and 7 to 15 digits, like +34600111222"
        )
    return value


# AfterValidator and never a pattern= constraint: a pattern reaches the schema
# the model is sent, one target's strict converter keeps it, and the provider
# rejects it. So the schema says str and the shape is checked here.
#
# The description is how the format reaches the model at all, and it is the only
# keyword that can: it travels as prose, so no strict converter strips it. A
# field that said nothing about its shape was learned from a refusal mid-call,
# which cost a model round trip on every value the prompt spells one way and
# this type another. A field carrying its own description keeps that one; the
# emitter appends this phrase to it.
Phone = Annotated[
    str,
    AfterValidator(_shape_phone),
    Field(
        description="a phone number in E.164, one leading plus and 7 to 15 digits, like +34600111222"
    ),
]

_SHAPE_DATE = re.compile(r"^\d{4}-\d{2}-\d{2}$")


def _shape_date(value: str) -> str:
    # Empty is not a wrong value, it is no value yet. It is what a declared
    # variable holds before anything fills it, what the state block renders as
    # words, and what a tool hands back for a field it could not fill. Refusing
    # it here deadlocked a live call on both targets: the model had nothing else
    # to send, so every retry was refused the same way and the step never
    # finished. A wrong value is still refused; an absent one is not wrong.
    if value and not _SHAPE_DATE.match(value):
        raise ValueError("expected a day written year-month-day, like 2026-03-19")
    return value


# AfterValidator and never a pattern= constraint: a pattern reaches the schema
# the model is sent, one target's strict converter keeps it, and the provider
# rejects it. So the schema says str and the shape is checked here.
#
# The description is how the format reaches the model at all, and it is the only
# keyword that can: it travels as prose, so no strict converter strips it. A
# field that said nothing about its shape was learned from a refusal mid-call,
# which cost a model round trip on every value the prompt spells one way and
# this type another. A field carrying its own description keeps that one; the
# emitter appends this phrase to it.
Date = Annotated[
    str,
    AfterValidator(_shape_date),
    Field(description="a day written year-month-day, like 2026-03-19"),
]

_SHAPE_TIME = re.compile(r"^([01]\d|2[0-3]):[0-5]\d$")


def _shape_time(value: str) -> str:
    # Empty is not a wrong value, it is no value yet. It is what a declared
    # variable holds before anything fills it, what the state block renders as
    # words, and what a tool hands back for a field it could not fill. Refusing
    # it here deadlocked a live call on both targets: the model had nothing else
    # to send, so every retry was refused the same way and the step never
    # finished. A wrong value is still refused; an absent one is not wrong.
    if value and not _SHAPE_TIME.match(value):
        raise ValueError(
            "expected a time of day on the 24-hour clock, like 09:30 or 17:45"
        )
    return value


# AfterValidator and never a pattern= constraint: a pattern reaches the schema
# the model is sent, one target's strict converter keeps it, and the provider
# rejects it. So the schema says str and the shape is checked here.
#
# The description is how the format reaches the model at all, and it is the only
# keyword that can: it travels as prose, so no strict converter strips it. A
# field that said nothing about its shape was learned from a refusal mid-call,
# which cost a model round trip on every value the prompt spells one way and
# this type another. A field carrying its own description keeps that one; the
# emitter appends this phrase to it.
Time = Annotated[
    str,
    AfterValidator(_shape_time),
    Field(description="a time of day on the 24-hour clock, like 09:30 or 17:45"),
]

_SHAPE_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$")


def _shape_id(value: str) -> str:
    # Empty is not a wrong value, it is no value yet. It is what a declared
    # variable holds before anything fills it, what the state block renders as
    # words, and what a tool hands back for a field it could not fill. Refusing
    # it here deadlocked a live call on both targets: the model had nothing else
    # to send, so every retry was refused the same way and the step never
    # finished. A wrong value is still refused; an absent one is not wrong.
    if value and not _SHAPE_ID.match(value):
        raise ValueError(
            "expected an identifier: letters, digits, and then any of dot, dash, underscore or colon"
        )
    return value


# AfterValidator and never a pattern= constraint: a pattern reaches the schema
# the model is sent, one target's strict converter keeps it, and the provider
# rejects it. So the schema says str and the shape is checked here.
#
# The description is how the format reaches the model at all, and it is the only
# keyword that can: it travels as prose, so no strict converter strips it. A
# field that said nothing about its shape was learned from a refusal mid-call,
# which cost a model round trip on every value the prompt spells one way and
# this type another. A field carrying its own description keeps that one; the
# emitter appends this phrase to it.
Id = Annotated[
    str,
    AfterValidator(_shape_id),
    Field(
        description="an identifier: letters, digits, and then any of dot, dash, underscore or colon"
    ),
]


class Appointment(BaseModel):
    """One thing being booked, moved or cancelled."""

    scheduled_date: Date
    scheduled_time: Time
    appointment_type: Annotated[
        Literal["haircut", "haircolor", "haircut_and_haircolor", "dry_cut"],
        Field(
            description="The service the caller asked for, in the salon's own words."
        ),
    ]
    action: Annotated[
        Literal["create", "modify", "cancel"],
        Field(description="What this call did to this appointment."),
    ]
    booking_id: Annotated[
        Id | None,
        Field(
            description="The diary's own id for the booking, once one exists. Absent while the caller is still choosing a time. Expected an identifier: letters, digits, and then any of dot, dash, underscore or colon."
        ),
    ]


class Complaint(BaseModel):
    """One thing the caller is unhappy about."""

    complaint_id: Id
    reason: Annotated[
        Literal["service_quality", "waiting_time", "price", "staff", "other"],
        Field(
            description="What the complaint is about, in the salon's own categories."
        ),
    ]
    about: Annotated[
        Appointment | None,
        Field(
            description="The appointment the complaint concerns, when it concerns one. A complaint about the salon in general has none."
        ),
    ]
    resolution: Annotated[
        Literal["refund_offered", "rebooking_offered", "escalated", "noted"],
        Field(
            description="What was offered or done about it on this call. Three of these are offers and not outcomes: a refund offered is not a refund approved, a rebooking offered is not a rebooking confirmed, and escalated means somebody will look, not that they agreed. Only what a tool did is done. Say this value out loud in the tense it is written in."
        ),
    ]


class Customer(BaseModel):
    """Who the caller is, as the salon's records have them."""

    phone_number: Phone
    status: Annotated[
        Literal["existing", "created", "invalid"],
        Field(
            description="What the lookup found for the confirmed number: existing for a record that was already there, created for one written during this call, and invalid for a number the lookup could not use."
        ),
    ]


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

    A step re-entered mid-call can read a value out of its own state block and
    hand it straight back, which is not a second thing happening. One live call
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
    "handle_complaint": {
        "complaint": TypeAdapter(Complaint),
        "reason": TypeAdapter(
            Literal[
                "create_booking",
                "modify_booking",
                "cancel_booking",
                "request_informations",
                "complain",
            ]
        ),
    },
    "manage_booking": {
        "appointment": TypeAdapter(Appointment | None),
        "reason": TypeAdapter(
            Literal[
                "create_booking",
                "modify_booking",
                "cancel_booking",
                "request_informations",
                "complain",
            ]
        ),
    },
    "verify_customer": {
        "customer": TypeAdapter(Customer),
        "customer_phone": TypeAdapter(Phone),
    },
}


def _typed_result(step, values):
    """Validate a step's declared results where they enter the state.

    Refused here rather than carried into a later step that assumes it is
    right, and refused on both targets rather than on the one whose framework
    happens to validate tool arguments: one of them validates through Pydantic
    and lets the model self-correct, the other splats raw JSON into the handler.
    """
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


_STATE_STRUCTURED = {
    "appointments",
    "booking_date",
    "caller_reason",
    "complaints",
    "customer",
    "customer_phone",
}
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
    if name in _STATE_STRUCTURED:
        if value is None or value == "" or value == [] or value == {}:
            return _STATE_EMPTY
        if not isinstance(value, str):
            value = json.dumps(_plain(value), separators=(",", ":"), ensure_ascii=False)
    text = "" if value is None else str(value)
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


@dataclass
class State:
    """Typed call variables (SCHEMA 4.4), shared across agents."""

    appointments: list[Appointment] = field(
        default_factory=list
    )  # What this call booked, moved or cancelled, in the order the caller gave them. A list because a caller can do two things in one call, and the booking step appends rather than replacing, so the second does not erase the first. It starts empty, which is why a guard naming it waits: an empty list is nothing recorded yet, not a decision the caller made.
    booking_date: Date = ""  # Today's date in the salon's own timezone, YYYY-MM-DD. Read once from the clock before the greeting, so a caller saying "tomorrow" costs one model request instead of two chained tool calls. A call that crosses midnight keeps the day it started on, which is deliberate: a date changing underneath a conversation would leave the caller and the agent disagreeing about what "tomorrow" means halfway through.
    caller_reason: list[
        Literal[
            "create_booking",
            "modify_booking",
            "cancel_booking",
            "request_informations",
            "complain",
        ]
    ] = field(
        default_factory=list
    )  # Why the caller rang. A list, because one call can do more than one thing: somebody who books and also complains has two reasons, and each step appends the one it heard rather than replacing what the last step recorded.
    complaints: list[Complaint] = field(
        default_factory=list
    )  # What the caller was unhappy about, one entry per thing, each with what was offered about it. Appended by the complaint step for the same reason the appointments are.
    customer: Customer | None = (
        None  # Who the caller is, once the verification step has heard them agree to the number and looked the record up. Absent until then, and that absence is load-bearing: the booking step's guard names customer.status, so with no value the step waits rather than starting on a caller nobody looked up. No `default:` for the same reason a default was wrong on the flat status value it replaced. A default is a value the variable holds before the first word, so a defaulted customer satisfies the guard on an empty record.
    )
    customer_name: str = ""  # The name on the record the caller's number belongs to, looked up before the greeting. Inherits `customer_phone`'s confirming step, because a name found from a number nobody has agreed to is exactly as unconfirmed as that number was: greeting a stranger by the account holder's name is the worst thing this feature could do, and the compiler refuses the prompt that would.
    customer_phone: Phone = ""  # The caller's phone number in E.164: a plus sign, then digits, with no spaces, brackets or dashes. One shape for every phone number in this package, the MANAGER_PHONE_NUMBER transfer destination included, so no prompt and no tool has to guess which shape it is holding. No `source:` here on purpose. The prefetch block below reads the carrier's fact and this variable receives it, which leaves the per-route refusal for a variable naming a source its target cannot supply exactly as strict as it is: on a route with no caller ID the entry skips and this holds its default. Offered to the caller for a yes, never acted on unasked. Somebody may be ringing from a friend's phone, or may hold a second account, so until the verification step has heard them agree this value satisfies no `requires:` guard and appears in no prompt but that step's own. An earlier version of this note said no prompt ever reads the number back. That is now false rather than merely out of date: reading it back is the whole saving, and it replaced twelve spoken digits with one yes. The read-back turn does not cache, and that trade was made deliberately.


def _dispatched_call_start(call_context: dict | None) -> dict:
    """Input variables arrive with the dispatch: the call context on a telephony
    route, or UNMUTE_CALL_START for a local `unmute dev --var` session."""
    values = dict((call_context or {}).get("call_start", {}))
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
    return values


def build_state(call_context: dict | None = None) -> State:
    state = State()
    missing = []
    call_start = _dispatched_call_start(call_context)
    if "appointments" in call_start:
        setattr(state, "appointments", call_start["appointments"])
    if "booking_date" in call_start:
        setattr(state, "booking_date", call_start["booking_date"])
    if "caller_reason" in call_start:
        setattr(state, "caller_reason", call_start["caller_reason"])
    if "complaints" in call_start:
        setattr(state, "complaints", call_start["complaints"])
    if "customer" in call_start:
        setattr(state, "customer", call_start["customer"])
    if "customer_name" in call_start:
        setattr(state, "customer_name", call_start["customer_name"])
    if "customer_phone" in call_start:
        setattr(state, "customer_phone", call_start["customer_phone"])
    if missing:
        raise RuntimeError(f"Missing call context fields: {', '.join(missing)}")
    return state


_TEMPLATE = re.compile(r"\{\{\s*([a-z_][a-z0-9_]*)\s*\}\}")


def _render(text: str, state, *, quote_values: bool = False) -> str:
    """Substitute each variable token from the call state (SCHEMA 4.4 templates).
    Only substituted values are URL-encoded, never the surrounding literal."""
    if state is None:
        return text

    def _one(match: re.Match[str]) -> str:
        value = getattr(state, match.group(1), None)
        # Through _state_text, so a declared value renders as compact JSON and
        # an empty one as words. Anything not declared structured comes back
        # exactly as str() gave it.
        value = _state_text(match.group(1), value)
        return quote(value, safe="") if quote_values else value

    return _TEMPLATE.sub(_one, text)


def _refusal(tool: str, state, needed: list[tuple[str, str]]) -> str:
    """Refuse a call whose injected variables are still unset (V4): the model is
    told what to ask for, and no half-formed request is ever sent."""
    unset = [
        (name, hint)
        for name, hint in needed
        if getattr(state, name, None) in (None, "")
        # A value the caller has not agreed to is present and not usable. Without
        # this, a pre-fetched number would reach somebody else's record: the
        # request would go out against a proposal rather than a fact.
        or name in getattr(state, "_unconfirmed", ())
    ]
    if not unset:
        return ""
    names = ", ".join(name for name, _ in unset)
    hints = " ".join(f"{name}: {hint}" for name, hint in unset if hint)
    return f"cannot call {tool} yet: {names} not set. Ask the caller first. {hints}".strip()


# Prerequisite guard, generated by internal/generate/guard.go.
#
# A control that declares requires: is held back until every named variable
# holds a value. The refusal below goes to the model, never to the caller: it
# names what is missing and which control supplies it, so the model fetches the
# value and retries within the same turn. The caller hears the model's next
# natural question and never learns a guard fired.
#
# Both emitted targets render this same block, so their wording cannot drift.
_PREREQUISITE_LIMIT = 5
_PREREQUISITE_SUPPLIER = {
    "appointments": "manage_booking",
    "caller_reason": "handle_complaint",
    "complaints": "handle_complaint",
    "customer": "verify_customer",
    "customer_phone": "verify_customer",
}

# Every name declared as a list. An empty one means nothing has been
# recorded yet, which is exactly the state a guard exists to wait for, so it
# is unmet. Tested by the declared type rather than by truthiness, because 0,
# False and 0.0 are real answers a caller can give and treating them as
# missing is the bug this predicate was written to avoid.
_PREREQUISITE_LISTS = {"appointments", "caller_reason", "complaints"}


def _unmet_prerequisites(state, names):
    unmet = []
    for name in names:
        root, _, path = name.partition(".")
        # A value awaiting the caller's agreement satisfies no guard through any
        # path into it: the mark is on the value, so naming a field one level
        # down cannot escape it. getattr with a default, because the set is
        # created by the pre-fetch and a path here may never have run one.
        if root in getattr(state, "_unconfirmed", ()):
            unmet.append(name)
            continue
        value = getattr(state, root, None)
        for step in path.split(".") if path else ():
            if value is None:
                break
            value = (
                value.get(step)
                if isinstance(value, dict)
                else getattr(value, step, None)
            )
        if value is None or value == "":
            unmet.append(name)
        elif not path and root in _PREREQUISITE_LISTS and len(value) == 0:
            unmet.append(name)
    return unmet


def _prerequisite_refusal(names, at_limit):
    wants = ", ".join(
        name + " (call " + _PREREQUISITE_SUPPLIER[name] + " to get it)"
        if name in _PREREQUISITE_SUPPLIER
        else name
        for name in names
    )
    if at_limit:
        return (
            "Not started. Still missing: "
            + wants
            + ". Do not say any of this out loud. You have tried several times"
            + " without it, so ask the caller for it directly now, in your own"
            + " plain words, and stay in the conversation."
        )
    return (
        "Not started. Missing: "
        + wants
        + ". Do not say any of this out loud. Get the missing value now, then"
        + " call this again in the same turn."
    )


# One counter per guarded control per session. Held on the module rather than the
# worker because activating another worker replaces the object and a caller who
# keeps refusing across a handoff has not started over.
_prerequisite_refusals: dict[str, int] = {}


# Pre-fetch, generated by internal/generate/prefetch.go.
#
# Facts that are knowable before the greeting are resolved here, once per call,
# so the model is never asked to discover them. An entry whose inputs are empty
# is skipped and the values keep their declared defaults, which is what makes a
# package that pre-fetches a carrier fact still work on a route that supplies
# none. Nothing here can fail a call.
_PREFETCH_BUDGET_S = 2.0
_PREFETCH_VALUE_MAX = 512
_PREFETCH_TZ = ZoneInfo("Europe/Madrid")


def _prefetch_call_facts(call_context):
    """The call's own facts, with the local seed filling only what the carrier did not.

    UNMUTE_CALL_FACTS is what `unmute dev --source name=value` sets. The
    carrier wins wherever it supplied a value: a seed stands in for a fact it
    could not supply, and never overrides one it did, so a stale value in a .env
    cannot quietly reshape a real call.

    Merging here rather than at each call site is deliberate. This driver starts a
    session from four different places, and a merge written four times is a merge
    that disagrees with itself in one of them.
    """
    facts = dict(call_context or {})
    seeded = os.environ.get("UNMUTE_CALL_FACTS")
    if not seeded:
        return facts
    try:
        values = json.loads(seeded)
        for name in values:
            if not facts.get(name):
                facts[name] = values[name]
    except (ValueError, TypeError):
        logger.warning(
            "UNMUTE_CALL_FACTS is not a JSON object of call facts; ignoring it"
        )
    return facts


def _prefetch_bounded(name, value):
    """Bound one pre-fetched value, and say so when it is shortened.

    The length is only knowable here, at run time, so this cannot be a
    compile-time refusal. Silence is the one thing it must not be.
    """
    text = "" if value is None else str(value)
    if len(text) <= _PREFETCH_VALUE_MAX:
        return text
    logger.warning(
        f"prefetch: {name} was {len(text)} characters and is cut to "
        f"{_PREFETCH_VALUE_MAX}; a value this long stops the prompt being cached"
    )
    return text[:_PREFETCH_VALUE_MAX]


async def _prefetch(state, call_context) -> None:
    call_context = _prefetch_call_facts(call_context)
    # Every value awaiting the caller's agreement. The prerequisite guard reads
    # this set, so an unconfirmed value satisfies no step, and each generated
    # assign write discards its own name as the caller settles it.
    state._unconfirmed = set()
    # Entries run in the order agent.yaml lists them. Nothing here is sorted or
    # reordered: an entry reading a value a later entry assigns was refused at
    # compile time, so by here the order is known good.

    # today: clock -> booking_date
    state.booking_date = datetime.now(_PREFETCH_TZ).date().isoformat()
    logger.info(f"prefetch today: resolved booking_date={state.booking_date}")

    # caller: source from_number -> customer_phone
    _value = (call_context or {}).get("from_number") or ""
    if not _value:
        logger.info("prefetch caller: skipped, the call carries no from_number")
    else:
        state.customer_phone = _prefetch_bounded("customer_phone", _value)
        state._unconfirmed.add("customer_phone")
        logger.info("prefetch caller: resolved customer_phone, awaiting confirmation")

    # profile: look_up_customer(phone={{customer_phone}}) -> customer_name
    if not state.customer_phone:
        logger.info("prefetch profile: skipped, customer_phone is empty")
    else:
        try:
            async with asyncio.timeout(_PREFETCH_BUDGET_S):
                result = tools.look_up_customer.look_up_customer(
                    phone=state.customer_phone
                )
                if inspect.isawaitable(result):
                    result = await result
            state.customer_name = _prefetch_bounded(
                "customer_name", (result or {}).get("name")
            )
            state._unconfirmed.add("customer_name")
            logger.info(
                "prefetch profile: resolved customer_name, awaiting confirmation"
            )
        except TimeoutError:
            logger.warning(
                f"prefetch profile: gave up after {_PREFETCH_BUDGET_S}s; customer_name keeps its default"
            )
        except Exception:
            logger.exception(
                "prefetch profile: failed; customer_name keeps its default"
            )


async def _end_after(worker: PipelineWorker, timeout_secs: float) -> None:
    await asyncio.sleep(timeout_secs)
    await worker.queue_frame(EndFrame())


# --- context shaping ---------------------------------------------------------


def _speech_only(messages: list[dict]) -> list[dict]:
    """history: messages, what was said out loud and no tool record.

    A tool record cannot be half-dropped. LLMContext holds provider-shaped
    dicts, so a tool call is a `tool_calls` key on an assistant message while
    only its reply carries role "tool". Filtering by role alone therefore keeps
    every call and drops everything answering it, and the provider rejects that
    request: "An assistant message with 'tool_calls' must be followed by tool
    messages responding to each 'tool_call_id'". Measured on a live call, mid
    step, on a package whose every unit test passed.

    So the call goes with its reply. An assistant turn that both spoke and
    called a tool keeps what it said, because that half is what this value is
    for. LiveKit needs none of this: .messages() holds no function-call items,
    so its filter drops both halves already.
    """
    kept: list[dict] = []
    for message in messages:
        if message.get("role") not in ("user", "assistant"):
            continue
        if not message.get("tool_calls"):
            kept.append(message)
            continue
        spoken = {key: value for key, value in message.items() if key != "tool_calls"}
        if spoken.get("content"):
            kept.append(spoken)
    return kept


# --- prompts ----------------------------------------------------------------
# Agent system instructions as module constants: one copy each, referenced by
# the LLM builder and any Flow restore (V2).
COMPLAINT_SPECIALIST_PROMPT = """# Sage and Stone customer care

You are still Robin, the same person the caller has been talking to. Nothing
about the call changed for them, so nothing about you changes either. You listen
to the complaint, acknowledge the impact, record the useful facts, and give a
clear next step. A human manager is available to inbound phone callers through
the manager transfer.

## How you speak

A text to speech voice reads out everything you write, exactly as you write it.
So write speech, not text.

- Whole sentences in ordinary capitalization. No markdown, no asterisks, no
  bullet points, no headings, no emoji, no symbols: the voice reads them out
  loud.
- Never send a bare fragment. A number or an amount sits inside a sentence.
- Capitals are read letter by letter, so use them only when that is what you
  want, like ATM.
- Write money, dates, times and numbers the plain written way and let the voice
  say them: 3:00 PM, Friday the 12th, 28 euros, 20 percent. Where the refund
  policy writes an amount or a deadline out in words, quote it as written.
- Write a phone number as a plus sign and the usual digit groups. Never put
  commas between digits and never break a number into separate words: the voice
  drops everything after the first comma.
- One or two short sentences a turn, one question at a time.
- Never say agent names, tool names, result keys, or raw results.

## How you sound

Calm, unhurried, and on the caller's side. Someone is telling you something
went wrong, so the warmth matters more here than anywhere else in the call.

- Use contractions, and vary your opener. "Right, ...", "Okay, ...", "Mhm,
  ...", "Ah, ...", "I see, ...", or no opener at all.
- A short filler at the front of a turn sounds like a person thinking, and it
  rides at the front of a turn that also does its job.
- A genuine apology is the one place to let the tone drop. "Oh, that's not
  okay, I'm sorry." Do not perform it and do not repeat it.
- Never gush, never say "I completely understand", and never thank the caller
  for their patience.

## What you never do

- You join a conversation that is already running. Continue it: never open with
  a greeting, an introduction, or a question already answered.
- Run a handoff or an escalation silently, and never mention one.
- Keep complaint IDs silent. Never promise a refund, credit, callback time, or
  policy that is not in the conversation.
- Never ask the caller for their phone number and never say one back. This
  prompt holds no number on purpose: if a step needs it, it already has it.
- Never claim a complaint was recorded or a transfer started unless the matching
  action ran in the same turn and succeeded.

## Escalation comes first

Call the manager transfer immediately when the caller asks for a manager,
supervisor, owner or human, or is clearly and strongly frustrated: repeated
anger after an attempted resolution, hostile language, or refusing to continue
with an agent. Do not verify anyone first, because reaching a person is never
gated on identifying yourself.

Ordinary disappointment, a firm tone, or one negative adjective is not strong
frustration. This is a conversation judgment, not a sentiment score.

If there is no active phone leg, say a direct transfer needs an inbound phone
call. If a real call reaches the carrier but the manager cannot be connected,
call it a carrier failure rather than a browser limitation, and never promise
the caller will stay connected.

## Complaint workflow

Listen first. Identify last, and only because a record needs an owner.

1. Acknowledge the problem without admitting facts the caller did not state.
2. Ask only for the missing service or visit detail and what they would like
   done. Quote the refund policy from the documents freely here: none of it
   depends on knowing who is calling.
3. A complaint needs a number to attach to. Read the conversation info at the
   end of this prompt: once it names a customer, verification has already
   succeeded, so say nothing about it and go straight to recording. Otherwise
   run verification, saying why in one short sentence.
4. Then run the complaint step in the same turn, silently. It records what the
   caller told you and hands back what happened.
5. When it hands its result back, give the smallest useful next step in one
   short sentence, without repeating what it already said. Offer a manager when
   the request needs a person with authority.

   Say the resolution it recorded, in the tense it recorded it. A refund or a
   redo that was offered is offered, so "a free redo can be arranged" and "I
   can have a manager confirm that" are both true, and "your appointment
   tomorrow will be used as the redo" is not: nothing in this call approved it,
   and a caller who hangs up believing it arrives expecting a free visit.
6. If the caller changes to booking help or another topic, hand back to the
   concierge immediately and silently.

Conversation info:
What this call has established so far. Read it rather than re-reading the conversation, and never ask for something already recorded here.
1. Booking date: {{booking_date}}
2. Caller reason: {{caller_reason}}
3. Customer: {{customer}}
4. Appointments: {{appointments}}
5. Complaints: {{complaints}}"""
CONCIERGE_PROMPT = """# Sage and Stone concierge

You are Robin, on the front desk at Sage and Stone. You are the person the
caller talks to for the whole call. You confirm who is calling, run the booking
step yourself, answer what you can, and hand over only for the one thing you do
not own: complaints and refunds, which customer care handles because it holds
the refund policy and the complaint record and you must not.

## How you speak

A text to speech voice reads out everything you write, exactly as you write it.
So write speech, not text.

- Whole sentences in ordinary capitalization. No markdown, no asterisks, no
  bullet points, no headings, no emoji, no symbols: the voice reads them out
  loud.
- Never send a bare fragment. A number or an amount sits inside a sentence.
- Capitals are read letter by letter, so use them only when that is what you
  want, like ATM.
- Write money, dates, times and numbers the plain written way and let the voice
  say them: 3:00 PM, Friday the 12th, 28 euros, 20 percent. Where the salon's
  own documents write an amount out in words, quote them as written.
- Write a phone number the way it is written on a phone: a plus sign, then the
  country code, then the rest in groups of two to four digits. Never put commas
  between digits and never break a number into separate words: the voice drops
  everything after the first comma.
- One or two short sentences a turn, one question at a time.
- Never say agent names, tool names, result keys, or raw results.

## How you sound

Relaxed, warm, and quick. You have worked this desk for years, you are talking
to one person, and you are not reading a script.

- Use contractions, and start a sentence with And, But or So when it fits.
- Vary your opener, and never open two turns in a row the same way. "Right,
  ...", "Okay, so ...", "Mhm, ...", "Ah, ...", "Lovely, ...", or no opener at
  all.
- A small filler at the front of a turn sounds like a person thinking. After a
  standalone "um", follow it with "so". A filler rides at the front of a turn
  that also does its job: never send a turn that is only a filler.
- One short line while a lookup runs is fine and human. "Let me check." Never
  ask the caller to hold, and never say the same line twice in a row.
- If a better phrasing lands mid sentence, drop the first one and carry on with
  the second, without apologising for it.
- When you did not catch something, say so plainly. "Sorry, I missed that, say
  it again?"

## What you never do

- Run a handoff or an escalation silently. Never mention a handoff, a
  specialist, or a routing step: just move.
- Keep internal IDs silent, and never say the caller's phone number. The
  verification step is the only place a number is spoken, and it is the only
  prompt that holds one.
- Never claim something happened unless the matching action ran in this turn
  and succeeded.
- Never invent salon policy, availability, or customer details.
- Never say the same thing twice in a call unless the caller asks you to.

## Workflow

1. If they ask for a manager or a person, or they are clearly and strongly
   frustrated, escalate on this turn. Do not verify first and do not ask for a
   number: somebody who wants a person should not be interviewed to get one.
   Say what actually happened. If there is no active phone leg, say a direct
   transfer needs an inbound phone call, and never tell the caller to phone the
   salon: on a real call they already have. If a phone call reaches the carrier
   but the manager cannot be connected, call it a carrier failure rather than a
   browser limitation, and never promise the caller will stay connected.
2. Otherwise work out whether they need booking help, have a complaint, or want
   to chat. Ask only if it is unclear, and never ask something they already
   said.
3. A complaint goes to customer care straight away.
4. Booking needs verification first: it reads the number back and needs a yes,
   and the booking step will not start without it. Once verification succeeds,
   run the booking step in the same turn, silently. If it does not succeed, say
   what the practical problem is once and offer to try again.
5. When the booking step hands its result back, confirm it in one short
   sentence that names nothing. "You're all set." "That's booked." "Done, it's
   in the diary." The step already said the service, the day and the time, so
   naming them again is the same news twice, and naming them from the
   conversation info is worse: those are the bookings this call already made,
   not the one that just happened.
6. Send the booking step back in for every booking change the caller wants
   making, including a second one in the same call: each visit records one
   booking, so two bookings is two visits. A step handing back an unserved
   request has given you a job, not an excuse, so act on it in the same turn
   and never apologise for it. What does not go back to the step is a question
   about a booking already on the conversation info: that is yours to answer
   from the info, in one sentence, with no step and no tool.

Verification happens once per call, and keeping it to once is your job rather
than the step's: the step runs with no conversation in front of it, so it
cannot tell that it already ran. Read the conversation info at the end of this
prompt. Once it names a customer, verification has already succeeded, so carry
on with what it found and never run the step again unless the caller says the
number is wrong.

## Answering things yourself

Anything that is not a booking and not a complaint, you handle here. Prices,
services and opening hours come from the salon's own documents, so look them up
rather than remembering them. For anything outside those documents, say plainly
that you cannot check it. Never claim to have searched or browsed a live
source.

Read the conversation info below rather than re-reading the call. It is the
record of what this call has already established, and it is why you never need
to ask again for something already on it.

Conversation info:
What this call has established so far. Read it rather than re-reading the conversation, and never ask for something already recorded here.
1. Booking date: {{booking_date}}
2. Caller reason: {{caller_reason}}
3. Customer: {{customer}}
4. Appointments: {{appointments}}
5. Complaints: {{complaints}}"""


# --- agents -----------------------------------------------------------------


def build_complaint_specialist_llm(state=None):
    return OpenAILLMService(
        api_key=os.environ["OPENAI_API_KEY"],
        settings=OpenAILLMService.Settings(
            model="gpt-5.6-luna",
            system_instruction=_render(COMPLAINT_SPECIALIST_PROMPT, state),
            extra={"reasoning_effort": "none"},
        ),
    )


def build_complaint_specialist_tts():
    return SlngTTSService(
        api_key=os.environ["SLNG_API_KEY"],
        voice="62ae83ad-4f6a-430b-af41-a9bede9286ca",
        model="cartesia/sonic:3.5",
        language=Language("en"),
        warm_standby_enabled=True,
        world_part_override="eu",
    )


class ComplaintSpecialistAgent(TracedLLMWorker):
    """Agent: complaint_specialist."""

    def __init__(self, state=None, context=None, call_context=None) -> None:
        self.state = state
        self.context = context
        self.call_context = call_context if call_context is not None else {}

        llm = build_complaint_specialist_llm(state)
        super().__init__(
            "complaint_specialist",
            llm=llm,
            pipeline=Pipeline([llm, build_complaint_specialist_tts()]),
            bridged=(),
        )

    async def on_activated(self, args) -> None:
        # A Flow replaces this worker's system instruction with its task role.
        # Re-entry restores the owning agent before tools/messages run.
        await self.queue_frame(
            LLMUpdateSettingsFrame(
                delta=LLMSettings(
                    system_instruction=_render(COMPLAINT_SPECIALIST_PROMPT, self.state)
                ),
            )
        )
        await super().on_activated(args)

    def _bind_state(self, handler):
        """Give a module-level flows handler this worker's call state, so a task
        tool can read injected variables the same way an agent tool does."""

        async def _bound(args, flow_manager):
            return await handler(args, flow_manager, state=self.state)

        return _bound

    @_direct_tool(cancel_on_interruption=False)
    async def to_concierge(self, params: FunctionCallParams):
        """The caller changes to a request owned by another specialist."""
        # context.history on this handoff. One LLMContext is shared for the whole
        # call, so the receiver is given the shaped list rather than a copy.
        self.context.set_messages(_speech_only(self.context.get_messages()))
        await self.activate_worker(
            "concierge",
            args=LLMWorkerActivationArgs(
                messages=[
                    {
                        "role": "developer",
                        "content": "The caller changes to a request owned by another specialist.",
                    }
                ],
                run_llm=True,
            ),
            deactivate_self=True,
            result_callback=params.result_callback,
        )

    @_direct_tool
    async def look_up_refund_policy(self, params: FunctionCallParams, query: str):
        """Look up the salon's refund and complaints policy. Use this before you state any refund, redo, timescale, or goodwill offer, so you quote the policy instead of guessing it. It covers the redo window, the three refund tiers, colour corrections, retail returns, and how a complaint is handled.

        The results may not answer the question. They are ordered best first, and each carries a relevance score where a higher number means a closer match. If nothing returned actually answers what was asked, say you do not have that information rather than offering the closest result.

                Args:
                    query (str): What to look up. Use the caller's own words.
        """
        await params.result_callback(await knowledge.look_up("refunds", query))

    @_direct_tool(cancel_on_interruption=False)
    async def to_manager(self, params: FunctionCallParams):
        """The caller explicitly asks for a manager or is clearly and strongly frustrated."""
        logger.info("human transfer fired: to_manager (cold)")
        transfer_result = self.call_context.get("_transfer_result")
        if transfer_result is not None:
            # Asked twice. Replay the first answer rather than dial again.
            await params.result_callback(transfer_result)
            return
        phone_call = self.call_context.get("_phone_call")
        if not phone_call or not phone_call.get("call_id"):
            # No phone call, so nothing to hand over: the browser and console
            # transports have no call for your carrier to redirect. Saying so beats
            # raising, and beats telling the model the caller was transferred.
            self.call_context["_transfer_result"] = {
                "failed": "this session is not a phone call, so it cannot be transferred"
            }
            await params.result_callback(self.call_context["_transfer_result"])
            return
        # Claimed before any transfer work awaits: a request arriving while
        # the first one is in flight must not slip past the guard.
        self.call_context["_transfer_result"] = {
            "in_progress": "A transfer is already under way; do not start another."
        }

        # The announcement is the first verb of the document below, spoken by your
        # carrier rather than by this agent. Applying the update replaces the call's
        # document, which tears this media stream down, so a line the agent started
        # speaking would be cut off by its own transfer.
        try:
            await _update_carrier_call(
                phone_call["call_id"],
                _transfer_twiml(os.environ["MANAGER_PHONE_NUMBER"], 30),
            )
        except Exception as exc:
            # The request failed, so the call is untouched and the caller is still
            # here listening. Tell them, and never report a transfer that did not
            # happen.
            logger.warning("cold transfer failed: {}", exc)
            self.call_context["_transfer_result"] = {
                "failed": "The transfer could not be completed; the call is ending."
            }
            failure_error = None
            try:
                await params.llm.push_frame(
                    LLMMessagesAppendFrame(
                        [
                            {
                                "role": "developer",
                                "content": "Tell the caller nobody can take the call right now, apologize, and say goodbye.",
                            }
                        ],
                        run_llm=True,
                    )
                )
                await params.result_callback(self.call_context["_transfer_result"])
            except BaseException as exc:
                failure_error = exc
            try:
                await params.llm.push_frame(EndFrame())
            except BaseException:
                if failure_error is None:
                    raise
                logger.exception("failed to end call after transfer failure")
            if failure_error is not None:
                raise failure_error
            return
        # The stream ends as your carrier applies the document. Recorded first so a
        # request that arrives in that window replays this rather than re-firing.
        self.call_context["_transfer_result"] = {"transfer_started": True}
        await params.result_callback(self.call_context["_transfer_result"])

    @_direct_tool
    async def verify_customer(self, params: FunctionCallParams):
        """Confirm who the caller is before any specialist handoff. The task reads the phone number back and needs a yes before it looks anyone up."""
        self._verify_customer_results = {}
        flow = FlowManager(
            llm=self.llm,
            context_aggregator=LLMContextAggregatorPair(self.context),
            worker=self,
        )
        # Resolve this tool call so it never dangles (V3/B1), but with
        # run_llm=False: the flow's first node (respond_immediately) is the sole
        # responder, so the owner never runs a second completion and the caller
        # hears the task's opening line once, not twice (V7/B4).
        await params.result_callback(
            {"status": "running the verify_customer task"},
            properties=FunctionCallResultProperties(run_llm=False),
        )
        # V2/B2: drain the resolved owner call before snapshotting. Otherwise
        # restoration erases that call and the unchanged request delegates again.
        await self.flush_pipeline()
        self._verify_customer_snapshot = (
            copy.deepcopy(self.context.get_messages()),
            self.context.tools,
        )
        await flow.initialize(self._verify_customer_node_verify_customer())

    def _verify_customer_node_verify_customer(self) -> NodeConfig:
        return NodeConfig(
            name="verify_customer",
            role_message=_render(
                '# Verify the customer\n\nYou confirm who you are speaking to, and you have two ways in.\n\n**When you already have a number**, which is most inbound calls: the number is\n`{{customer_phone}}` and the name on that record is `{{customer_name}}`. Read\nthe number back, ask for a yes, and stop. Never ask for a number you already\nhave.\n\n**When you have nothing**, because the caller withheld their number or the\nroute does not carry one, both come through blank and you ask for a number.\n\n**You are handed no conversation.** This step runs with the history reset, so\nyou have this prompt, the values above and the conversation info at the end.\nNothing the caller said is in front of you, and neither is anything you did on\nan earlier run. So you cannot tell whether verification already happened: Robin\ndecides that and Robin can see it.\n\nYou also cannot tell why the caller rang, and you do not need to. **Never ask\nwhat they are calling about.** They have already said it, and the step that\nacts on their request reads the conversation and records the reason itself.\n\n## How you speak\n\nA text to speech voice reads out everything you write, exactly as you write it.\nSo write speech, not text.\n\n- Whole sentences in ordinary capitalization. No markdown, no asterisks, no\n  emoji, no symbols: the voice reads them out loud.\n- Never send a bare fragment. A number sits inside a short question, never as\n  digits on their own.\n- Capitals are read letter by letter, so use them only when that is what you\n  want.\n- Write a phone number the way it is written on a phone: a plus sign, then the\n  country code, then the rest in groups of two to four digits. The voice\n  recognises that shape.\n- Never break a number into separate words and never put commas between digits.\n  A comma inside a run of digits stops the voice: "plus 3 4, 1 1 1, 1 1 1" came\n  out as "plus three four" and the caller heard nothing to check.\n- One short sentence, one question. Never say tool names or result keys.\n\n## How you sound\n\nSame person the caller has been talking to, still relaxed. Reading a number\nback is the dullest moment of the call, so keep it light and keep it moving.\nUse contractions, vary how you open, and never say the same sentence twice in\nthis step. No apologies for the process and no thanking them for their\npatience.\n\n## Workflow\n\n1. The call is already running and the caller is waiting on you, so your first\n   response always speaks: either read back the number above or ask for one.\n   Never open with silence and never open by asking what they wanted.\n2. **If `{{customer_phone}}` holds a number, read it back.** Do not say where it\n   came from and never say the name: a caller ringing from a friend\'s phone\n   would hear a stranger\'s name, which is the worst thing this step can do. If\n   it is empty, ask for the number, keeping any digits they already gave.\n3. Read every digit back once, written as a phone number, and ask if that is\n   right. Group the digits yourself in the usual groups of two to four, and\n   never copy the pauses out of what you heard: a caller who trails off\n   mid-number is transcribed as "111 11 1", and reading that back makes a whole\n   number look one digit short. Never invent a country code.\n4. Agreement is a yes however it arrives: "yes", "that\'s right", "sounds about\n   right", or agreement followed by the caller moving straight on to what they\n   came for. Only a correction or a plain no is not a yes.\n5. On a no, take the new digits and read back again in different words. A caller\n   who hears their own question repeated word for word thinks the line broke. A\n   no to a number you were handed is not a mistake: somebody on a friend\'s phone\n   says no here and is right to, so drop it and ask for the one they want.\n6. You never decide whether a number is long enough. The lookup does, and a\n   number it cannot use comes back with an invalid status. So on a yes, call the\n   lookup with the digits you are holding, whatever shape they are in. Most of\n   the world\'s numbers are not three, three and four, and one that looks wrong\n   to you is almost always whole.\n7. If the lookup still returns invalid after one retry, or the caller will not\n   confirm, finish with an empty phone number and a customer record whose status\n   is invalid.\n8. On a yes and a usable number you are done. Finish.\n\n## What you return\n\n**The confirmed number.** In E.164 and no other shape: a plus sign, then the\ndigits, with nothing between them, no spaces, no brackets and no dashes. Copy\nwhat the lookup returned character for character: do not regroup it, do not pretty it\nup, do not drop the plus. This value is data, not something to say out loud; the\nreadback in step 3 is the only place a number is spoken, and it is spoken in the\nspaced shape.\n\n**The customer record.** The confirmed number and the status the lookup gave\nyou, existing, created, or invalid. The number is the identity here, so the\nrecord carries no name and no id: never add either. On an invalid number still\nreturn a record, with the invalid status.\n\n**The summary.** One short line for whoever reads this next: confirmed and\nlooked up, confirmed but invalid, or not confirmed. Plain words, not something\nyou would say out loud.\n\nConversation info:\nWhat this call has established so far. Read it rather than re-reading the conversation, and never ask for something already recorded here.\n1. Booking date: {{booking_date}}\n2. Caller reason: {{caller_reason}}\n3. Customer: {{customer}}\n4. Appointments: {{appointments}}\n5. Complaints: {{complaints}}\n6. Customer phone: {{customer_phone}}\n\nWhen this step is complete, call `finish_verify_customer_verify_customer` with: customer, customer_phone, summary.\n\n`unserved_request` is for a request this step cannot serve. Do this step\'s own work first, and never use it to skip that work: the caller\'s original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish_verify_customer_verify_customer` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there.',
                self.state,
            ),
            task_messages=[{"role": "developer", "content": "Begin this step."}],
            functions=[
                FlowsFunctionSchema(
                    name="find_or_create_customer",
                    description="Look up or create one salon customer from the exact confirmed phone number. Use only after the digit readback got a clear yes. Reuse the record that already owns the number, or create one for a number that is new. Never guess a number or pass one the caller has not confirmed.",
                    properties={
                        "phone": {
                            "description": "Exact confirmed phone; 10 to 15 digits with no inferred country code",
                            "type": "string",
                        }
                    },
                    required=["phone"],
                    handler=self._trace_flow_tool(
                        "find_or_create_customer", _flow_tool_find_or_create_customer
                    ),
                ),
                FlowsFunctionSchema(
                    name="finish_verify_customer_verify_customer",
                    description="Record the result of this step and finish.",
                    properties={
                        **{
                            "summary": {"type": "string"},
                            "unserved_request": {
                                "description": "Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.",
                                "type": "string",
                            },
                        },
                        "customer": _schema(
                            _FINISH_TYPES["verify_customer"]["customer"]
                        ),
                        "customer_phone": _schema(
                            _FINISH_TYPES["verify_customer"]["customer_phone"]
                        ),
                    },
                    required=["customer", "customer_phone", "summary"],
                    handler=self._trace_flow_tool(
                        "finish_verify_customer_verify_customer",
                        self._verify_customer_finish_verify_customer,
                    ),
                ),
            ],
            context_strategy=ContextStrategyConfig(strategy=ContextStrategy.RESET),
        )

    async def _verify_customer_finish_verify_customer(self, args, flow_manager):
        # Validated before anything is recorded: a value that does not fit its
        # declared type never enters the state, the previous contents stand, and
        # the message goes back to the model so it can correct itself on the next
        # turn instead of the step recording something wrong. This framework
        # validates no tool argument itself, so without this the two targets
        # would behave differently on the same package.
        try:
            _values = _typed_result("verify_customer", dict(args))
        except _StateRefused as refused:
            logger.warning("finish {}: {}", "verify_customer", refused.message)
            return {
                "refused": f"Not recorded: {refused.message}. Ask again, then call finish with a value that fits."
            }, None
        self._verify_customer_results["verify_customer"] = _values
        self.state.customer = self._verify_customer_results["verify_customer"][
            "customer"
        ]
        self.state.customer_phone = self._verify_customer_results["verify_customer"][
            "customer_phone"
        ]
        # The step that confirms this value has just assigned it, so it is settled
        # now and the guard stops holding it back. Discarded here rather than in the
        # step's prompt: a model can be talked out of an instruction, not out of a
        # set membership test. getattr with a default, matching the guard: this step
        # can be reached on a path where the pre-fetch never ran, and a bare read
        # there is an AttributeError inside a finish handler.
        getattr(self.state, "_unconfirmed", set()).discard("customer_phone")
        # then: return — restore the owner's pre-flow context (messages and
        # tools); only the typed results cross back (merge: results, N13).
        messages, tools = self._verify_customer_snapshot
        await self.queue_frame(
            LLMUpdateSettingsFrame(
                delta=LLMSettings(
                    system_instruction=_render(COMPLAINT_SPECIALIST_PROMPT, self.state)
                ),
            )
        )
        await self.flush_pipeline()
        self.context.set_messages(
            messages
            + [
                {
                    "role": "developer",
                    "content": "Task results: "
                    + json.dumps(self._verify_customer_results)
                    + " Continue with the caller in one short line. A result carrying `unserved_request` means a step could not serve that request and handed it back. The caller is still owed it: after one short line about the result, act on that request in the same turn, with your own tools, a handoff, or the same flow again. It is a new request, so running the flow for it is not running it again for the one that just finished. Never end the turn without acting on it, and never tell the caller you cannot.",
                }
            ]
        )
        self.context.set_tools(tools)
        return {"status": "ok"}, None

    @_direct_tool
    async def handle_complaint(self, params: FunctionCallParams):
        """The verified caller is unhappy about something and wants it recorded. Before this can run you need: customer.status."""
        _unmet = _unmet_prerequisites(self.state, ["customer.status"])
        if _unmet:
            # requires guard (machine-checked): no flow starts, so nothing else
            # in this process is going to speak. The call is resolved WITHOUT
            # run_llm=False on purpose. The success path below sets it because
            # the flow's first node is the sole responder; copying that here
            # would leave a live call silent with nothing in the trace, because
            # there is no first node to respond.
            _tries = _prerequisite_refusals.get("handle_complaint", 0) + 1
            _prerequisite_refusals["handle_complaint"] = _tries
            _at_limit = _tries >= _PREREQUISITE_LIMIT
            if _at_limit:
                logger.info(
                    "prerequisite guard: step {} refused, unmet {}, retry limit reached; asking the caller".format(
                        "handle_complaint", ", ".join(_unmet)
                    )
                )
            else:
                logger.info(
                    "prerequisite guard: step {} refused, unmet {}".format(
                        "handle_complaint", ", ".join(_unmet)
                    )
                )
            await params.result_callback(
                {"refused": _prerequisite_refusal(_unmet, _at_limit)}
            )
            return
        _prerequisite_refusals["handle_complaint"] = 0
        self._handle_complaint_results = {}
        flow = FlowManager(
            llm=self.llm,
            context_aggregator=LLMContextAggregatorPair(self.context),
            worker=self,
        )
        # Resolve this tool call so it never dangles (V3/B1), but with
        # run_llm=False: the flow's first node (respond_immediately) is the sole
        # responder, so the owner never runs a second completion and the caller
        # hears the task's opening line once, not twice (V7/B4).
        await params.result_callback(
            {"status": "running the handle_complaint task"},
            properties=FunctionCallResultProperties(run_llm=False),
        )
        # V2/B2: drain the resolved owner call before snapshotting. Otherwise
        # restoration erases that call and the unchanged request delegates again.
        await self.flush_pipeline()
        self._handle_complaint_snapshot = (
            copy.deepcopy(self.context.get_messages()),
            self.context.tools,
        )
        # context.history on this task. Shaped after the snapshot above, so the
        # finish path restores the owner's own context whatever this step saw.
        self.context.set_messages(_speech_only(self.context.get_messages()))
        await flow.initialize(self._handle_complaint_node_handle_complaint())

    def _handle_complaint_node_handle_complaint(self) -> NodeConfig:
        return NodeConfig(
            name="handle_complaint",
            role_message=_render(
                "# Record one complaint\n\nYou write one complaint down, with what has already been offered about it.\n\nThe specialist has the refund policy and has already quoted it. You do not: the\nonly tool you have here records the complaint.\n\n## How you speak\n\nA text to speech voice reads out everything you write, exactly as you write it.\nSo write speech, not text.\n\n- Whole sentences in ordinary capitalization. No markdown, no asterisks, no\n  bullet points, no emoji, no symbols: the voice reads them out loud.\n- Never send a bare fragment. A number or an amount sits inside a sentence.\n- Capitals are read letter by letter, so use them only when that is what you\n  want.\n- Write money, dates and times the plain written way and let the voice say\n  them: 28 euros, 20 percent, Friday the 12th. Where the policy already writes\n  an amount or a deadline out in words, quote it exactly as written.\n- One or two short sentences a turn, one question at a time. Never say tool\n  names or result keys, and keep the complaint id silent.\n\n## How you sound\n\nSame person the caller has been talking to, still calm and on their side.\nNothing about the call changed for them when this step started, so nothing\nabout you changes either. Use contractions and vary your opener. A genuine\napology is the one place to let the tone drop: do not perform it and do not\nrepeat it. Never gush, never say \"I completely understand\", and never thank the\ncaller for their patience.\n\n## What you are handed\n\nBoth sides of the conversation so far, tool records left out, so whatever the\ncaller told the specialist about what went wrong is already in front of you.\nNever ask them to repeat it. The conversation info at the end of this prompt\nholds the caller's record and any appointment this call already booked, moved,\nor cancelled.\n\n## What you never do\n\n- The caller is already verified. Never ask for their name or number.\n- Never promise a refund, credit, callback time, or policy that is not in the\n  conversation.\n- Never say a complaint is recorded unless the matching tool ran in this turn\n  and said so.\n\n## Workflow\n\n1. The caller has already described what went wrong, so your first response\n   records it. Never open by asking what happened, and only ask a question if\n   something you genuinely need is missing.\n2. Read back through the conversation for what is settled: what they are\n   unhappy about, which visit it concerns, and what the specialist already\n   offered. Never invent an offer and never quote a policy: you do not have it\n   here. Where nothing has been offered yet, the resolution is that it has been\n   noted, which is a real answer.\n3. Record the complaint with that resolution, then say in one short sentence\n   that it is written down. If it ever comes back saying the record failed, say\n   plainly that it was not recorded rather than implying it was.\n4. Give the smallest useful next step in one short sentence. Offer a manager\n   when the request needs a person with authority.\n\n## What you return\n\n**The complaint.** Built from what you just recorded:\n\n- `complaint_id`: from what the tool returned.\n- `reason`: sorted into the salon's own categories: service quality, waiting\n  time, price, staff, or other.\n- `about`: the appointment the complaint concerns, matched to one the\n  conversation info already shows, action and all. Leave it out for anything\n  else, including an older visit this call never recorded.\n- `resolution`: what has been offered or done on this call. `noted` is for when\n  nothing more specific was offered, not for when recording failed. The other\n  three are offers, so record the offer you actually made: `rebooking_offered`\n  when you said a redo can be arranged, never when you told the caller a\n  booking they already hold is now that redo. Nothing here approves anything.\n\n**The reason they rang.** `complain`, always.\n\n**The summary.** One short line for whoever reads this next: recorded with what\nwas offered, or not recorded and why. Plain words, not something you would say\nout loud.\n\nConversation info:\nWhat this call has established so far. Read it rather than re-reading the conversation, and never ask for something already recorded here.\n1. Booking date: {{booking_date}}\n2. Caller reason: {{caller_reason}}\n3. Customer: {{customer}}\n4. Appointments: {{appointments}}\n5. Complaints: {{complaints}}\n\nWhen this step is complete, call `finish_handle_complaint_handle_complaint` with: complaint, reason, summary.\n\n`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish_handle_complaint_handle_complaint` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there.",
                self.state,
            ),
            task_messages=[{"role": "developer", "content": "Begin this step."}],
            functions=[
                FlowsFunctionSchema(
                    name="record_complaint",
                    description="Record the verified customer's complaint after enough facts are known. This does not replace a requested manager transfer.",
                    properties={
                        "requested_resolution": {
                            "description": "What the customer wants the salon to do, or an empty string",
                            "type": "string",
                        },
                        "summary": {
                            "description": "Short factual summary of the complaint",
                            "type": "string",
                        },
                    },
                    required=["summary", "requested_resolution"],
                    handler=self._trace_flow_tool(
                        "record_complaint",
                        self._bind_state(_flow_tool_record_complaint),
                    ),
                ),
                FlowsFunctionSchema(
                    name="finish_handle_complaint_handle_complaint",
                    description="Record the result of this step and finish.",
                    properties={
                        **{
                            "summary": {"type": "string"},
                            "unserved_request": {
                                "description": "Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.",
                                "type": "string",
                            },
                        },
                        "complaint": _schema(
                            _FINISH_TYPES["handle_complaint"]["complaint"]
                        ),
                        "reason": _schema(_FINISH_TYPES["handle_complaint"]["reason"]),
                    },
                    required=["complaint", "reason", "summary"],
                    handler=self._trace_flow_tool(
                        "finish_handle_complaint_handle_complaint",
                        self._handle_complaint_finish_handle_complaint,
                    ),
                ),
            ],
        )

    async def _handle_complaint_finish_handle_complaint(self, args, flow_manager):
        # Validated before anything is recorded: a value that does not fit its
        # declared type never enters the state, the previous contents stand, and
        # the message goes back to the model so it can correct itself on the next
        # turn instead of the step recording something wrong. This framework
        # validates no tool argument itself, so without this the two targets
        # would behave differently on the same package.
        try:
            _values = _typed_result("handle_complaint", dict(args))
        except _StateRefused as refused:
            logger.warning("finish {}: {}", "handle_complaint", refused.message)
            return {
                "refused": f"Not recorded: {refused.message}. Ask again, then call finish with a value that fits."
            }, None
        self._handle_complaint_results["handle_complaint"] = _values
        # An entry added, not the value replaced: only the step producing it knows
        # whether the caller added an intent or swapped one, and the list starts
        # empty so this never has to create it. The helper drops an absent entry
        # and a structured one already on the list, so neither a step that
        # concluded nothing nor a step re-entered mid-call grows it.
        _append_entry(
            self.state.caller_reason,
            self._handle_complaint_results["handle_complaint"]["reason"],
        )
        # An entry added, not the value replaced: only the step producing it knows
        # whether the caller added an intent or swapped one, and the list starts
        # empty so this never has to create it. The helper drops an absent entry
        # and a structured one already on the list, so neither a step that
        # concluded nothing nor a step re-entered mid-call grows it.
        _append_entry(
            self.state.complaints,
            self._handle_complaint_results["handle_complaint"]["complaint"],
        )
        # then: return — restore the owner's pre-flow context (messages and
        # tools); only the typed results cross back (merge: results, N13).
        messages, tools = self._handle_complaint_snapshot
        await self.queue_frame(
            LLMUpdateSettingsFrame(
                delta=LLMSettings(
                    system_instruction=_render(COMPLAINT_SPECIALIST_PROMPT, self.state)
                ),
            )
        )
        await self.flush_pipeline()
        self.context.set_messages(
            messages
            + [
                {
                    "role": "developer",
                    "content": "Task results: "
                    + json.dumps(self._handle_complaint_results)
                    + " Continue with the caller in one short line. A result carrying `unserved_request` means a step could not serve that request and handed it back. The caller is still owed it: after one short line about the result, act on that request in the same turn, with your own tools, a handoff, or the same flow again. It is a new request, so running the flow for it is not running it again for the one that just finished. Never end the turn without acting on it, and never tell the caller you cannot.",
                }
            ]
        )
        self.context.set_tools(tools)
        return {"status": "ok"}, None


def build_concierge_llm(state=None):
    return OpenAILLMService(
        api_key=os.environ["OPENAI_API_KEY"],
        settings=OpenAILLMService.Settings(
            model="gpt-5.6-luna",
            system_instruction=_render(CONCIERGE_PROMPT, state),
            extra={"reasoning_effort": "none"},
        ),
    )


def build_concierge_tts():
    return SlngTTSService(
        api_key=os.environ["SLNG_API_KEY"],
        voice="62ae83ad-4f6a-430b-af41-a9bede9286ca",
        model="cartesia/sonic:3.5",
        language=Language("en"),
        warm_standby_enabled=True,
        world_part_override="eu",
    )


class ConciergeAgent(TracedLLMWorker):
    """Agent: concierge."""

    def __init__(self, state=None, context=None, call_context=None) -> None:
        self.state = state
        self.context = context
        self.call_context = call_context if call_context is not None else {}

        llm = build_concierge_llm(state)
        super().__init__(
            "concierge",
            llm=llm,
            pipeline=Pipeline([llm, build_concierge_tts()]),
            bridged=(),
        )

    async def on_activated(self, args) -> None:
        # A Flow replaces this worker's system instruction with its task role.
        # Re-entry restores the owning agent before tools/messages run.
        await self.queue_frame(
            LLMUpdateSettingsFrame(
                delta=LLMSettings(
                    system_instruction=_render(CONCIERGE_PROMPT, self.state)
                ),
            )
        )
        await super().on_activated(args)

    def _bind_state(self, handler):
        """Give a module-level flows handler this worker's call state, so a task
        tool can read injected variables the same way an agent tool does."""

        async def _bound(args, flow_manager):
            return await handler(args, flow_manager, state=self.state)

        return _bound

    @_direct_tool(cancel_on_interruption=False)
    async def to_complaints(self, params: FunctionCallParams):
        """The verified caller has a complaint or service problem."""
        # context.history on this handoff. One LLMContext is shared for the whole
        # call, so the receiver is given the shaped list rather than a copy.
        self.context.set_messages(_speech_only(self.context.get_messages()))
        await self.activate_worker(
            "complaint_specialist",
            args=LLMWorkerActivationArgs(
                messages=[
                    {
                        "role": "developer",
                        "content": "The verified caller has a complaint or service problem.",
                    }
                ],
                run_llm=True,
            ),
            deactivate_self=True,
            result_callback=params.result_callback,
        )

    @_direct_tool
    async def look_up_salon_info(self, params: FunctionCallParams, query: str):
        """Look up anything about the salon itself: services and prices, how long things take, opening hours, the stylists and what each one does, booking, deposits, cancellation and lateness terms, payment methods, parking, and accessibility. Use this whenever the caller asks a question about how the salon works, not only about price. Quote the document rather than estimating.
        Prefer this over asking the caller for anything. A question about the salon is not a booking request: look it up and answer it, and only start verification if the caller actually wants to make, move, or cancel an appointment.

        The results may not answer the question. They are ordered best first, and each carries a relevance score where a higher number means a closer match. If nothing returned actually answers what was asked, say you do not have that information rather than offering the closest result.

                Args:
                    query (str): What to look up. Use the caller's own words.
        """
        await params.result_callback(await knowledge.look_up("services", query))

    @_direct_tool(cancel_on_interruption=False)
    async def to_manager(self, params: FunctionCallParams):
        """The caller explicitly asks for a manager or is clearly and strongly frustrated."""
        logger.info("human transfer fired: to_manager (cold)")
        transfer_result = self.call_context.get("_transfer_result")
        if transfer_result is not None:
            # Asked twice. Replay the first answer rather than dial again.
            await params.result_callback(transfer_result)
            return
        phone_call = self.call_context.get("_phone_call")
        if not phone_call or not phone_call.get("call_id"):
            # No phone call, so nothing to hand over: the browser and console
            # transports have no call for your carrier to redirect. Saying so beats
            # raising, and beats telling the model the caller was transferred.
            self.call_context["_transfer_result"] = {
                "failed": "this session is not a phone call, so it cannot be transferred"
            }
            await params.result_callback(self.call_context["_transfer_result"])
            return
        # Claimed before any transfer work awaits: a request arriving while
        # the first one is in flight must not slip past the guard.
        self.call_context["_transfer_result"] = {
            "in_progress": "A transfer is already under way; do not start another."
        }

        # The announcement is the first verb of the document below, spoken by your
        # carrier rather than by this agent. Applying the update replaces the call's
        # document, which tears this media stream down, so a line the agent started
        # speaking would be cut off by its own transfer.
        try:
            await _update_carrier_call(
                phone_call["call_id"],
                _transfer_twiml(os.environ["MANAGER_PHONE_NUMBER"], 30),
            )
        except Exception as exc:
            # The request failed, so the call is untouched and the caller is still
            # here listening. Tell them, and never report a transfer that did not
            # happen.
            logger.warning("cold transfer failed: {}", exc)
            self.call_context["_transfer_result"] = {
                "failed": "The transfer could not be completed; the call is ending."
            }
            failure_error = None
            try:
                await params.llm.push_frame(
                    LLMMessagesAppendFrame(
                        [
                            {
                                "role": "developer",
                                "content": "Tell the caller nobody can take the call right now, apologize, and say goodbye.",
                            }
                        ],
                        run_llm=True,
                    )
                )
                await params.result_callback(self.call_context["_transfer_result"])
            except BaseException as exc:
                failure_error = exc
            try:
                await params.llm.push_frame(EndFrame())
            except BaseException:
                if failure_error is None:
                    raise
                logger.exception("failed to end call after transfer failure")
            if failure_error is not None:
                raise failure_error
            return
        # The stream ends as your carrier applies the document. Recorded first so a
        # request that arrives in that window replays this rather than re-firing.
        self.call_context["_transfer_result"] = {"transfer_started": True}
        await params.result_callback(self.call_context["_transfer_result"])

    @_direct_tool
    async def verify_customer(self, params: FunctionCallParams):
        """Confirm who the caller is before any specialist handoff. The task reads the phone number back and needs a yes before it looks anyone up."""
        self._verify_customer_results = {}
        flow = FlowManager(
            llm=self.llm,
            context_aggregator=LLMContextAggregatorPair(self.context),
            worker=self,
        )
        # Resolve this tool call so it never dangles (V3/B1), but with
        # run_llm=False: the flow's first node (respond_immediately) is the sole
        # responder, so the owner never runs a second completion and the caller
        # hears the task's opening line once, not twice (V7/B4).
        await params.result_callback(
            {"status": "running the verify_customer task"},
            properties=FunctionCallResultProperties(run_llm=False),
        )
        # V2/B2: drain the resolved owner call before snapshotting. Otherwise
        # restoration erases that call and the unchanged request delegates again.
        await self.flush_pipeline()
        self._verify_customer_snapshot = (
            copy.deepcopy(self.context.get_messages()),
            self.context.tools,
        )
        await flow.initialize(self._verify_customer_node_verify_customer())

    def _verify_customer_node_verify_customer(self) -> NodeConfig:
        return NodeConfig(
            name="verify_customer",
            role_message=_render(
                '# Verify the customer\n\nYou confirm who you are speaking to, and you have two ways in.\n\n**When you already have a number**, which is most inbound calls: the number is\n`{{customer_phone}}` and the name on that record is `{{customer_name}}`. Read\nthe number back, ask for a yes, and stop. Never ask for a number you already\nhave.\n\n**When you have nothing**, because the caller withheld their number or the\nroute does not carry one, both come through blank and you ask for a number.\n\n**You are handed no conversation.** This step runs with the history reset, so\nyou have this prompt, the values above and the conversation info at the end.\nNothing the caller said is in front of you, and neither is anything you did on\nan earlier run. So you cannot tell whether verification already happened: Robin\ndecides that and Robin can see it.\n\nYou also cannot tell why the caller rang, and you do not need to. **Never ask\nwhat they are calling about.** They have already said it, and the step that\nacts on their request reads the conversation and records the reason itself.\n\n## How you speak\n\nA text to speech voice reads out everything you write, exactly as you write it.\nSo write speech, not text.\n\n- Whole sentences in ordinary capitalization. No markdown, no asterisks, no\n  emoji, no symbols: the voice reads them out loud.\n- Never send a bare fragment. A number sits inside a short question, never as\n  digits on their own.\n- Capitals are read letter by letter, so use them only when that is what you\n  want.\n- Write a phone number the way it is written on a phone: a plus sign, then the\n  country code, then the rest in groups of two to four digits. The voice\n  recognises that shape.\n- Never break a number into separate words and never put commas between digits.\n  A comma inside a run of digits stops the voice: "plus 3 4, 1 1 1, 1 1 1" came\n  out as "plus three four" and the caller heard nothing to check.\n- One short sentence, one question. Never say tool names or result keys.\n\n## How you sound\n\nSame person the caller has been talking to, still relaxed. Reading a number\nback is the dullest moment of the call, so keep it light and keep it moving.\nUse contractions, vary how you open, and never say the same sentence twice in\nthis step. No apologies for the process and no thanking them for their\npatience.\n\n## Workflow\n\n1. The call is already running and the caller is waiting on you, so your first\n   response always speaks: either read back the number above or ask for one.\n   Never open with silence and never open by asking what they wanted.\n2. **If `{{customer_phone}}` holds a number, read it back.** Do not say where it\n   came from and never say the name: a caller ringing from a friend\'s phone\n   would hear a stranger\'s name, which is the worst thing this step can do. If\n   it is empty, ask for the number, keeping any digits they already gave.\n3. Read every digit back once, written as a phone number, and ask if that is\n   right. Group the digits yourself in the usual groups of two to four, and\n   never copy the pauses out of what you heard: a caller who trails off\n   mid-number is transcribed as "111 11 1", and reading that back makes a whole\n   number look one digit short. Never invent a country code.\n4. Agreement is a yes however it arrives: "yes", "that\'s right", "sounds about\n   right", or agreement followed by the caller moving straight on to what they\n   came for. Only a correction or a plain no is not a yes.\n5. On a no, take the new digits and read back again in different words. A caller\n   who hears their own question repeated word for word thinks the line broke. A\n   no to a number you were handed is not a mistake: somebody on a friend\'s phone\n   says no here and is right to, so drop it and ask for the one they want.\n6. You never decide whether a number is long enough. The lookup does, and a\n   number it cannot use comes back with an invalid status. So on a yes, call the\n   lookup with the digits you are holding, whatever shape they are in. Most of\n   the world\'s numbers are not three, three and four, and one that looks wrong\n   to you is almost always whole.\n7. If the lookup still returns invalid after one retry, or the caller will not\n   confirm, finish with an empty phone number and a customer record whose status\n   is invalid.\n8. On a yes and a usable number you are done. Finish.\n\n## What you return\n\n**The confirmed number.** In E.164 and no other shape: a plus sign, then the\ndigits, with nothing between them, no spaces, no brackets and no dashes. Copy\nwhat the lookup returned character for character: do not regroup it, do not pretty it\nup, do not drop the plus. This value is data, not something to say out loud; the\nreadback in step 3 is the only place a number is spoken, and it is spoken in the\nspaced shape.\n\n**The customer record.** The confirmed number and the status the lookup gave\nyou, existing, created, or invalid. The number is the identity here, so the\nrecord carries no name and no id: never add either. On an invalid number still\nreturn a record, with the invalid status.\n\n**The summary.** One short line for whoever reads this next: confirmed and\nlooked up, confirmed but invalid, or not confirmed. Plain words, not something\nyou would say out loud.\n\nConversation info:\nWhat this call has established so far. Read it rather than re-reading the conversation, and never ask for something already recorded here.\n1. Booking date: {{booking_date}}\n2. Caller reason: {{caller_reason}}\n3. Customer: {{customer}}\n4. Appointments: {{appointments}}\n5. Complaints: {{complaints}}\n6. Customer phone: {{customer_phone}}\n\nWhen this step is complete, call `finish_verify_customer_verify_customer` with: customer, customer_phone, summary.\n\n`unserved_request` is for a request this step cannot serve. Do this step\'s own work first, and never use it to skip that work: the caller\'s original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish_verify_customer_verify_customer` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there.',
                self.state,
            ),
            task_messages=[{"role": "developer", "content": "Begin this step."}],
            functions=[
                FlowsFunctionSchema(
                    name="find_or_create_customer",
                    description="Look up or create one salon customer from the exact confirmed phone number. Use only after the digit readback got a clear yes. Reuse the record that already owns the number, or create one for a number that is new. Never guess a number or pass one the caller has not confirmed.",
                    properties={
                        "phone": {
                            "description": "Exact confirmed phone; 10 to 15 digits with no inferred country code",
                            "type": "string",
                        }
                    },
                    required=["phone"],
                    handler=self._trace_flow_tool(
                        "find_or_create_customer", _flow_tool_find_or_create_customer
                    ),
                ),
                FlowsFunctionSchema(
                    name="finish_verify_customer_verify_customer",
                    description="Record the result of this step and finish.",
                    properties={
                        **{
                            "summary": {"type": "string"},
                            "unserved_request": {
                                "description": "Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.",
                                "type": "string",
                            },
                        },
                        "customer": _schema(
                            _FINISH_TYPES["verify_customer"]["customer"]
                        ),
                        "customer_phone": _schema(
                            _FINISH_TYPES["verify_customer"]["customer_phone"]
                        ),
                    },
                    required=["customer", "customer_phone", "summary"],
                    handler=self._trace_flow_tool(
                        "finish_verify_customer_verify_customer",
                        self._verify_customer_finish_verify_customer,
                    ),
                ),
            ],
            context_strategy=ContextStrategyConfig(strategy=ContextStrategy.RESET),
        )

    async def _verify_customer_finish_verify_customer(self, args, flow_manager):
        # Validated before anything is recorded: a value that does not fit its
        # declared type never enters the state, the previous contents stand, and
        # the message goes back to the model so it can correct itself on the next
        # turn instead of the step recording something wrong. This framework
        # validates no tool argument itself, so without this the two targets
        # would behave differently on the same package.
        try:
            _values = _typed_result("verify_customer", dict(args))
        except _StateRefused as refused:
            logger.warning("finish {}: {}", "verify_customer", refused.message)
            return {
                "refused": f"Not recorded: {refused.message}. Ask again, then call finish with a value that fits."
            }, None
        self._verify_customer_results["verify_customer"] = _values
        self.state.customer = self._verify_customer_results["verify_customer"][
            "customer"
        ]
        self.state.customer_phone = self._verify_customer_results["verify_customer"][
            "customer_phone"
        ]
        # The step that confirms this value has just assigned it, so it is settled
        # now and the guard stops holding it back. Discarded here rather than in the
        # step's prompt: a model can be talked out of an instruction, not out of a
        # set membership test. getattr with a default, matching the guard: this step
        # can be reached on a path where the pre-fetch never ran, and a bare read
        # there is an AttributeError inside a finish handler.
        getattr(self.state, "_unconfirmed", set()).discard("customer_phone")
        # then: return — restore the owner's pre-flow context (messages and
        # tools); only the typed results cross back (merge: results, N13).
        messages, tools = self._verify_customer_snapshot
        await self.queue_frame(
            LLMUpdateSettingsFrame(
                delta=LLMSettings(
                    system_instruction=_render(CONCIERGE_PROMPT, self.state)
                ),
            )
        )
        await self.flush_pipeline()
        self.context.set_messages(
            messages
            + [
                {
                    "role": "developer",
                    "content": "Task results: "
                    + json.dumps(self._verify_customer_results)
                    + " Continue with the caller in one short line. A result carrying `unserved_request` means a step could not serve that request and handed it back. The caller is still owed it: after one short line about the result, act on that request in the same turn, with your own tools, a handoff, or the same flow again. It is a new request, so running the flow for it is not running it again for the one that just finished. Never end the turn without acting on it, and never tell the caller you cannot.",
                }
            ]
        )
        self.context.set_tools(tools)
        return {"status": "ok"}, None

    @_direct_tool
    async def manage_booking(self, params: FunctionCallParams):
        """The caller wants to create, modify, or cancel a booking. Before this can run you need: customer_phone (call verify_customer to get it), customer.status."""
        _unmet = _unmet_prerequisites(self.state, ["customer_phone", "customer.status"])
        if _unmet:
            # requires guard (machine-checked): no flow starts, so nothing else
            # in this process is going to speak. The call is resolved WITHOUT
            # run_llm=False on purpose. The success path below sets it because
            # the flow's first node is the sole responder; copying that here
            # would leave a live call silent with nothing in the trace, because
            # there is no first node to respond.
            _tries = _prerequisite_refusals.get("manage_booking", 0) + 1
            _prerequisite_refusals["manage_booking"] = _tries
            _at_limit = _tries >= _PREREQUISITE_LIMIT
            if _at_limit:
                logger.info(
                    "prerequisite guard: step {} refused, unmet {}, retry limit reached; asking the caller".format(
                        "manage_booking", ", ".join(_unmet)
                    )
                )
            else:
                logger.info(
                    "prerequisite guard: step {} refused, unmet {}".format(
                        "manage_booking", ", ".join(_unmet)
                    )
                )
            await params.result_callback(
                {"refused": _prerequisite_refusal(_unmet, _at_limit)}
            )
            return
        _prerequisite_refusals["manage_booking"] = 0
        self._manage_booking_results = {}
        self._manage_booking_active_step = "manage_booking"
        flow = FlowManager(
            llm=self.llm,
            context_aggregator=LLMContextAggregatorPair(self.context),
            worker=self,
        )
        # Resolve this tool call so it never dangles (V3/B1), but with
        # run_llm=False: the flow's first node (respond_immediately) is the sole
        # responder, so the owner never runs a second completion and the caller
        # hears the task's opening line once, not twice (V7/B4).
        await params.result_callback(
            {"status": "running the manage_booking task"},
            properties=FunctionCallResultProperties(run_llm=False),
        )
        # V2/B2: drain the resolved owner call before snapshotting. Otherwise
        # restoration erases that call and the unchanged request delegates again.
        await self.flush_pipeline()
        self._manage_booking_snapshot = (
            copy.deepcopy(self.context.get_messages()),
            self.context.tools,
        )
        # context.history on this task. Shaped after the snapshot above, so the
        # finish path restores the owner's own context whatever this step saw.
        self.context.set_messages(_speech_only(self.context.get_messages()))
        await flow.initialize(self._manage_booking_node_manage_booking())

    def _manage_booking_node_manage_booking(self) -> NodeConfig:
        return NodeConfig(
            name="manage_booking",
            role_message=_render(
                '# Handle one booking change\n\nYou take one booking request from start to finish: work out what the caller\nwants, get one clear yes, then save it.\n\n## How you speak\n\nA text to speech voice reads out everything you write, exactly as you write it.\nSo write speech, not text.\n\n- Whole sentences in ordinary capitalization. No markdown, no asterisks, no\n  bullet points, no emoji, no symbols: the voice reads them out loud.\n- Never send a bare fragment. A time sits inside a sentence: "Friday at 11:30 AM\n  works.", never "11:30." on its own.\n- Capitals are read letter by letter, so use them only when that is what you\n  want.\n- Write dates and times the plain written way and let the voice say them:\n  11:30 AM, 3:00 PM, tomorrow, Friday the 12th.\n- Name a day once, and the way the caller named it. If they said tomorrow, say\n  tomorrow.\n- Say `haircolor` as "hair color", `haircut_and_haircolor` as "a haircut and a\n  hair color", and `dry_cut` as "a dry cut".\n- Offer times the way a person does: "I\'ve got 9:00 AM, 11:30, or 3:00 in the\n  afternoon." Never read out a list.\n- One or two short sentences a turn, one question at a time. Never say tool\n  names or result keys, and keep booking IDs silent.\n\n## How you sound\n\nSame person the caller has been talking to. Quick, warm, and a bit pleased when\na booking lands. Use contractions, vary your opener, and never say the same\ninformation twice.\n\n## What you are handed\n\nToday is `{{booking_date}}`, in the salon\'s own timezone. You get what was said\nout loud on this call plus the conversation info at the end of this prompt. No\ntool result anybody ran before you is in front of you, so call the tool\nyourself for availability, a booking list or a price.\n\n## What you never do\n\n- The caller is already verified. Never ask for their name or number.\n- Use only the bookings and slots a tool returned. Never invent an ID and never\n  improvise a time nobody offered.\n- Never say a booking is saved, moved, or cancelled unless the matching tool ran\n  in this turn and said so.\n\n## Workflow\n\n1. The caller is waiting on you, so your first response always speaks. Read what\n   they have already told you and ask only for what is genuinely missing. A\n   caller who said "a haircut tomorrow afternoon" has given you the service, the\n   day and the part of the day, so ask nothing and go straight to availability.\n2. Work out create, modify, or cancel from what they said. Ask only if it is\n   unclear. This is the `action` you record at the end.\n3. To modify or cancel, list their bookings first, unless the record was created\n   during this call: a new record has nothing on it, so say there is nothing\n   booked yet and offer to make one. Do the same if an existing record\'s list\n   comes back empty. If more than one booking fits, name them by service and\n   time and let the caller pick.\n4. To create or modify, work the day out from the date above rather than asking\n   a tool what day it is, then check availability and offer up to three of the\n   times it returned, narrowed to the part of the day they asked for. A caller\n   who said afternoon does not want to hear about 9:00 AM. Never ask which time\n   suits them and then read out the times: if the next thing you do is check\n   availability, check it.\n5. Say the whole thing back in one sentence and ask one yes-or-no question: the\n   service, the day, the time. "Tomorrow at 3:00 PM for a haircut, shall I book\n   it?" When only one time fits, that is the same sentence as the offer, not a\n   second one. Nothing said before that question counts as a yes.\n6. On a clear yes, save it in the same turn with `confirmed` set to true, then\n   say it landed in one short sentence. "That\'s booked." is the whole turn: the\n   caller heard the day, the time and the service in your own question and said\n   yes to them.\n7. On a no, ask what they would like instead and offer again. Do not record an\n   appointment for something that did not happen.\n8. Finish once the booking you were asked for is saved, or once there is truly\n   nothing left this step can do. There is no "still working" finish: while the\n   conversation is live, speak instead. And never finish having done nothing:\n   you were sent in because the caller wants a booking change, so that change\n   is your job, however far into the call it arrives.\n\n## What you return\n\n**The appointment.** The one booking you saved in this visit, built from the\ntool that just ran: `scheduled_date` and `scheduled_time`, the\n`appointment_type` in the salon\'s own words, the `action`, and the `booking_id`\nthe tool returned.\n\nLeave it out entirely when you saved nothing this visit. A caller who asked and\nthen changed their mind leaves nothing to record, and an appointment already on\nthe conversation info was recorded by an earlier visit: handing it back again\nis the same booking counted twice, not a new one. Only ever return a booking a\ntool saved for you in this visit.\n\n**The reason they rang.** `create_booking`, `modify_booking`, or\n`cancel_booking`, from what they asked you. Never ask for it and never say it\nout loud. Return it even when nothing was saved.\n\n**The summary.** One short line for whoever reads this next: booked, moved,\ncancelled, or not confirmed. Plain words, not something you would say out loud.\n\n## Leaving this step\n\n**They raise a complaint or ask for a person.** Call `to_complaints` on the same\nturn and save nothing. This is the only handoff you hold. If they ask for a\nmanager, customer care reaches one; you cannot.\n\n**They ask for something a booking tool cannot do, once a booking is saved.**\nA price, an opening time, anything that is not a booking change. Put it in\n`unserved_request` when you finish, in their own words, and Robin takes it from\nthere.\n\nA second booking is not that. It is a booking change, so it is yours: do it.\nWhat you never do is save two of them in one visit, because you hand back one\nappointment and the other would go unrecorded. So save one, say it landed,\nfinish, and Robin sends you straight back in for the next one.\n\nNever reach for a handoff or `unserved_request` because you are unsure what to\nsay. Ask them instead.\n\nConversation info:\nWhat this call has established so far. Read it rather than re-reading the conversation, and never ask for something already recorded here.\n1. Booking date: {{booking_date}}\n2. Caller reason: {{caller_reason}}\n3. Customer: {{customer}}\n4. Appointments: {{appointments}}\n5. Complaints: {{complaints}}\n\nWhen this step is complete, call `finish_manage_booking_manage_booking` with: appointment, reason, summary.\n\n`unserved_request` is for a request this step cannot serve. Do this step\'s own work first, and never use it to skip that work: the caller\'s original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish_manage_booking_manage_booking` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there.',
                self.state,
            ),
            task_messages=[{"role": "developer", "content": "Begin this step."}],
            functions=[
                FlowsFunctionSchema(
                    name="list_bookings",
                    description="List the verified customer's active bookings. The phone number comes from verification and is never supplied or guessed by the model.",
                    properties={},
                    required=[],
                    handler=self._trace_flow_tool(
                        "list_bookings", self._bind_state(_flow_tool_list_bookings)
                    ),
                ),
                FlowsFunctionSchema(
                    name="check_availability",
                    description="List currently open salon times for one supported service and date.",
                    properties={
                        "date": {
                            "description": "Preferred date in YYYY-MM-DD form",
                            "type": "string",
                        },
                        "service": {
                            "enum": ["haircut", "hair-color", "blowout"],
                            "type": "string",
                        },
                    },
                    required=["service", "date"],
                    handler=self._trace_flow_tool(
                        "check_availability", _flow_tool_check_availability
                    ),
                ),
                FlowsFunctionSchema(
                    name="create_booking",
                    description="Create one confirmed booking for the verified customer using an exact slot returned by availability. Never invent a slot or a phone number.",
                    properties={
                        "confirmed": {
                            "description": "Exact true value from the shared confirm_booking result",
                            "type": "boolean",
                        },
                        "service": {
                            "enum": ["haircut", "hair-color", "blowout"],
                            "type": "string",
                        },
                        "slot_id": {
                            "description": "Exact slot ID returned by check_availability",
                            "type": "string",
                        },
                    },
                    required=["service", "slot_id", "confirmed"],
                    handler=self._trace_flow_tool(
                        "create_booking", self._bind_state(_flow_tool_create_booking)
                    ),
                ),
                FlowsFunctionSchema(
                    name="modify_booking",
                    description="Atomically move one confirmed active booking owned by the verified customer to an exact available slot.",
                    properties={
                        "booking_id": {
                            "description": "Exact booking ID returned by list_bookings",
                            "type": "string",
                        },
                        "confirmed": {
                            "description": "Exact true value from the shared confirm_booking result",
                            "type": "boolean",
                        },
                        "service": {
                            "enum": ["haircut", "hair-color", "blowout"],
                            "type": "string",
                        },
                        "slot_id": {
                            "description": "Exact slot ID returned by check_availability",
                            "type": "string",
                        },
                    },
                    required=["booking_id", "service", "slot_id", "confirmed"],
                    handler=self._trace_flow_tool(
                        "modify_booking", self._bind_state(_flow_tool_modify_booking)
                    ),
                ),
                FlowsFunctionSchema(
                    name="cancel_booking",
                    description="Cancel one confirmed booking owned by the verified customer. Use an exact booking ID returned by list_bookings.",
                    properties={
                        "booking_id": {
                            "description": "Exact booking ID returned by list_bookings",
                            "type": "string",
                        },
                        "confirmed": {
                            "description": "Exact true value from the shared confirm_booking result",
                            "type": "boolean",
                        },
                    },
                    required=["booking_id", "confirmed"],
                    handler=self._trace_flow_tool(
                        "cancel_booking", self._bind_state(_flow_tool_cancel_booking)
                    ),
                ),
                FlowsFunctionSchema(
                    name="to_complaints",
                    description="The verified caller has a complaint or service problem.",
                    properties={},
                    required=[],
                    handler=self._trace_flow_tool(
                        "to_complaints",
                        self._manage_booking_transfer_manage_booking_to_complaints,
                    ),
                ),
                FlowsFunctionSchema(
                    name="finish_manage_booking_manage_booking",
                    description="Record the result of this step and finish.",
                    properties={
                        **{
                            "summary": {"type": "string"},
                            "unserved_request": {
                                "description": "Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.",
                                "type": "string",
                            },
                        },
                        "appointment": _schema(
                            _FINISH_TYPES["manage_booking"]["appointment"]
                        ),
                        "reason": _schema(_FINISH_TYPES["manage_booking"]["reason"]),
                    },
                    required=["appointment", "reason", "summary"],
                    handler=self._trace_flow_tool(
                        "finish_manage_booking_manage_booking",
                        self._manage_booking_finish_manage_booking,
                    ),
                ),
            ],
        )

    async def _manage_booking_transfer_manage_booking_to_complaints(
        self, args, flow_manager
    ):
        if self._manage_booking_active_step != "manage_booking":
            return {"status": "already handled"}, NO_RESPONSE
        self._manage_booking_active_step = None
        flow_messages = None
        flow_tools = None
        try:
            flow_messages = [dict(message) for message in self.context.get_messages()]
            flow_tools = self.context.tools
            messages, tools = self._manage_booking_snapshot
            task_start = (
                len(messages) if flow_messages[: len(messages)] == messages else 0
            )
            task_messages = [
                dict(message)
                for message in flow_messages[task_start:]
                if message.get("role") in {"user", "assistant", "tool"}
            ]
            self.context.set_messages(messages + task_messages)
            self.context.set_tools(tools)
            await self.activate_worker(
                "complaint_specialist",
                args=LLMWorkerActivationArgs(
                    messages=[
                        {
                            "role": "developer",
                            "content": "The verified caller has a complaint or service problem.",
                        }
                    ],
                    run_llm=True,
                ),
                deactivate_self=True,
            )
        except BaseException:
            self._manage_booking_active_step = "manage_booking"
            if flow_messages is not None:
                self.context.set_messages(flow_messages)
            if flow_tools is not None:
                self.context.set_tools(flow_tools)
            raise
        return {"transferred": True}, NO_RESPONSE

    async def _manage_booking_finish_manage_booking(self, args, flow_manager):
        if self._manage_booking_active_step != "manage_booking":
            return {"status": "already handled"}, NO_RESPONSE
        self._manage_booking_active_step = None
        # Validated before anything is recorded: a value that does not fit its
        # declared type never enters the state, the previous contents stand, and
        # the message goes back to the model so it can correct itself on the next
        # turn instead of the step recording something wrong. This framework
        # validates no tool argument itself, so without this the two targets
        # would behave differently on the same package.
        try:
            _values = _typed_result("manage_booking", dict(args))
        except _StateRefused as refused:
            logger.warning("finish {}: {}", "manage_booking", refused.message)
            return {
                "refused": f"Not recorded: {refused.message}. Ask again, then call finish with a value that fits."
            }, None
        self._manage_booking_results["manage_booking"] = _values
        flow_messages = None
        flow_tools = None
        completion_started = False
        try:
            flow_messages = [dict(message) for message in self.context.get_messages()]
            flow_tools = self.context.tools
            completion_started = True
            return await self._manage_booking_complete_manage_booking()
        except BaseException as error:
            self._manage_booking_active_step = "manage_booking"
            self._manage_booking_results.pop("manage_booking", None)
            try:
                if flow_messages is not None:
                    self.context.set_messages(flow_messages)
                if flow_tools is not None:
                    self.context.set_tools(flow_tools)
                if completion_started:
                    await self.queue_frame(
                        LLMUpdateSettingsFrame(
                            delta=LLMSettings(
                                system_instruction=_render(
                                    '# Handle one booking change\n\nYou take one booking request from start to finish: work out what the caller\nwants, get one clear yes, then save it.\n\n## How you speak\n\nA text to speech voice reads out everything you write, exactly as you write it.\nSo write speech, not text.\n\n- Whole sentences in ordinary capitalization. No markdown, no asterisks, no\n  bullet points, no emoji, no symbols: the voice reads them out loud.\n- Never send a bare fragment. A time sits inside a sentence: "Friday at 11:30 AM\n  works.", never "11:30." on its own.\n- Capitals are read letter by letter, so use them only when that is what you\n  want.\n- Write dates and times the plain written way and let the voice say them:\n  11:30 AM, 3:00 PM, tomorrow, Friday the 12th.\n- Name a day once, and the way the caller named it. If they said tomorrow, say\n  tomorrow.\n- Say `haircolor` as "hair color", `haircut_and_haircolor` as "a haircut and a\n  hair color", and `dry_cut` as "a dry cut".\n- Offer times the way a person does: "I\'ve got 9:00 AM, 11:30, or 3:00 in the\n  afternoon." Never read out a list.\n- One or two short sentences a turn, one question at a time. Never say tool\n  names or result keys, and keep booking IDs silent.\n\n## How you sound\n\nSame person the caller has been talking to. Quick, warm, and a bit pleased when\na booking lands. Use contractions, vary your opener, and never say the same\ninformation twice.\n\n## What you are handed\n\nToday is `{{booking_date}}`, in the salon\'s own timezone. You get what was said\nout loud on this call plus the conversation info at the end of this prompt. No\ntool result anybody ran before you is in front of you, so call the tool\nyourself for availability, a booking list or a price.\n\n## What you never do\n\n- The caller is already verified. Never ask for their name or number.\n- Use only the bookings and slots a tool returned. Never invent an ID and never\n  improvise a time nobody offered.\n- Never say a booking is saved, moved, or cancelled unless the matching tool ran\n  in this turn and said so.\n\n## Workflow\n\n1. The caller is waiting on you, so your first response always speaks. Read what\n   they have already told you and ask only for what is genuinely missing. A\n   caller who said "a haircut tomorrow afternoon" has given you the service, the\n   day and the part of the day, so ask nothing and go straight to availability.\n2. Work out create, modify, or cancel from what they said. Ask only if it is\n   unclear. This is the `action` you record at the end.\n3. To modify or cancel, list their bookings first, unless the record was created\n   during this call: a new record has nothing on it, so say there is nothing\n   booked yet and offer to make one. Do the same if an existing record\'s list\n   comes back empty. If more than one booking fits, name them by service and\n   time and let the caller pick.\n4. To create or modify, work the day out from the date above rather than asking\n   a tool what day it is, then check availability and offer up to three of the\n   times it returned, narrowed to the part of the day they asked for. A caller\n   who said afternoon does not want to hear about 9:00 AM. Never ask which time\n   suits them and then read out the times: if the next thing you do is check\n   availability, check it.\n5. Say the whole thing back in one sentence and ask one yes-or-no question: the\n   service, the day, the time. "Tomorrow at 3:00 PM for a haircut, shall I book\n   it?" When only one time fits, that is the same sentence as the offer, not a\n   second one. Nothing said before that question counts as a yes.\n6. On a clear yes, save it in the same turn with `confirmed` set to true, then\n   say it landed in one short sentence. "That\'s booked." is the whole turn: the\n   caller heard the day, the time and the service in your own question and said\n   yes to them.\n7. On a no, ask what they would like instead and offer again. Do not record an\n   appointment for something that did not happen.\n8. Finish once the booking you were asked for is saved, or once there is truly\n   nothing left this step can do. There is no "still working" finish: while the\n   conversation is live, speak instead. And never finish having done nothing:\n   you were sent in because the caller wants a booking change, so that change\n   is your job, however far into the call it arrives.\n\n## What you return\n\n**The appointment.** The one booking you saved in this visit, built from the\ntool that just ran: `scheduled_date` and `scheduled_time`, the\n`appointment_type` in the salon\'s own words, the `action`, and the `booking_id`\nthe tool returned.\n\nLeave it out entirely when you saved nothing this visit. A caller who asked and\nthen changed their mind leaves nothing to record, and an appointment already on\nthe conversation info was recorded by an earlier visit: handing it back again\nis the same booking counted twice, not a new one. Only ever return a booking a\ntool saved for you in this visit.\n\n**The reason they rang.** `create_booking`, `modify_booking`, or\n`cancel_booking`, from what they asked you. Never ask for it and never say it\nout loud. Return it even when nothing was saved.\n\n**The summary.** One short line for whoever reads this next: booked, moved,\ncancelled, or not confirmed. Plain words, not something you would say out loud.\n\n## Leaving this step\n\n**They raise a complaint or ask for a person.** Call `to_complaints` on the same\nturn and save nothing. This is the only handoff you hold. If they ask for a\nmanager, customer care reaches one; you cannot.\n\n**They ask for something a booking tool cannot do, once a booking is saved.**\nA price, an opening time, anything that is not a booking change. Put it in\n`unserved_request` when you finish, in their own words, and Robin takes it from\nthere.\n\nA second booking is not that. It is a booking change, so it is yours: do it.\nWhat you never do is save two of them in one visit, because you hand back one\nappointment and the other would go unrecorded. So save one, say it landed,\nfinish, and Robin sends you straight back in for the next one.\n\nNever reach for a handoff or `unserved_request` because you are unsure what to\nsay. Ask them instead.\n\nConversation info:\nWhat this call has established so far. Read it rather than re-reading the conversation, and never ask for something already recorded here.\n1. Booking date: {{booking_date}}\n2. Caller reason: {{caller_reason}}\n3. Customer: {{customer}}\n4. Appointments: {{appointments}}\n5. Complaints: {{complaints}}\n\nWhen this step is complete, call `finish_manage_booking_manage_booking` with: appointment, reason, summary.\n\n`unserved_request` is for a request this step cannot serve. Do this step\'s own work first, and never use it to skip that work: the caller\'s original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish_manage_booking_manage_booking` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there.',
                                    self.state,
                                )
                            ),
                        )
                    )
                    await self.flush_pipeline()
            except BaseException as rollback_error:
                raise error from rollback_error
            raise

    async def _manage_booking_complete_manage_booking(self):
        # then: return — restore the owner's pre-flow context (messages and
        # tools); only the typed results cross back (merge: results, N13).
        messages, tools = self._manage_booking_snapshot
        await self.queue_frame(
            LLMUpdateSettingsFrame(
                delta=LLMSettings(
                    system_instruction=_render(CONCIERGE_PROMPT, self.state)
                ),
            )
        )
        await self.flush_pipeline()
        self.context.set_messages(
            messages
            + [
                {
                    "role": "developer",
                    "content": "Task results: "
                    + json.dumps(self._manage_booking_results)
                    + " Continue with the caller in one short line. A result carrying `unserved_request` means a step could not serve that request and handed it back. The caller is still owed it: after one short line about the result, act on that request in the same turn, with your own tools, a handoff, or the same flow again. It is a new request, so running the flow for it is not running it again for the one that just finished. Never end the turn without acting on it, and never tell the caller you cannot.",
                }
            ]
        )
        self.context.set_tools(tools)
        # An entry added, not the value replaced: only the step producing it knows
        # whether the caller added an intent or swapped one, and the list starts
        # empty so this never has to create it. The helper drops an absent entry
        # and a structured one already on the list, so neither a step that
        # concluded nothing nor a step re-entered mid-call grows it.
        _append_entry(
            self.state.appointments,
            self._manage_booking_results["manage_booking"]["appointment"],
        )
        # An entry added, not the value replaced: only the step producing it knows
        # whether the caller added an intent or swapped one, and the list starts
        # empty so this never has to create it. The helper drops an absent entry
        # and a structured one already on the list, so neither a step that
        # concluded nothing nor a step re-entered mid-call grows it.
        _append_entry(
            self.state.caller_reason,
            self._manage_booking_results["manage_booking"]["reason"],
        )
        return {"status": "ok"}, None


# --- task tools (flows handlers) ----------------------------------------------
# Tools available inside task steps; stable module-level handlers so a
# re-registered function name always resolves to the same callable.


async def _flow_tool_cancel_booking(args, flow_manager, state=None):
    """Cancel one confirmed booking owned by the verified customer. Use an exact booking ID returned by list_bookings."""
    refusal = _refusal(
        "cancel_booking",
        state,
        [
            (
                "customer_phone",
                "The caller's phone number in E.164: a plus sign, then digits, with no spaces, brackets or dashes. One shape for every phone number in this package, the MANAGER_PHONE_NUMBER transfer destination included, so no prompt and no tool has to guess which shape it is holding.\nNo `source:` here on purpose. The prefetch block below reads the carrier's fact and this variable receives it, which leaves the per-route refusal for a variable naming a source its target cannot supply exactly as strict as it is: on a route with no caller ID the entry skips and this holds its default.\nOffered to the caller for a yes, never acted on unasked. Somebody may be ringing from a friend's phone, or may hold a second account, so until the verification step has heard them agree this value satisfies no `requires:` guard and appears in no prompt but that step's own.\nAn earlier version of this note said no prompt ever reads the number back. That is now false rather than merely out of date: reading it back is the whole saving, and it replaced twelve spoken digits with one yes. The read-back turn does not cache, and that trade was made deliberately.",
            )
        ],
    )
    if refusal:
        return {"refused": refusal}
    # Cover the wait, same contract as _direct_tool: TTSSpeakFrame is a
    # DataFrame, so it is queued and speech starts while the handler body runs.
    # A flows handler holds a FlowManager rather than FunctionCallParams, and
    # FlowManager.worker is the documented seam for queueing a frame from inside
    # a handler (pipecat.flows.manager, pipecat-ai 1.8.0). Queued after the
    # refusal check, so a refused call stays silent.
    await flow_manager.worker.queue_frame(TTSSpeakFrame("Cancelling that now."))
    result = tools.cancel_booking.cancel_booking(
        **dict(args), customer_phone=state.customer_phone
    )
    if inspect.isawaitable(result):
        result = await result
    return result


async def _flow_tool_check_availability(args, flow_manager):
    """List currently open salon times for one supported service and date."""
    # Cover the wait, same contract as _direct_tool: TTSSpeakFrame is a
    # DataFrame, so it is queued and speech starts while the handler body runs.
    # A flows handler holds a FlowManager rather than FunctionCallParams, and
    # FlowManager.worker is the documented seam for queueing a frame from inside
    # a handler (pipecat.flows.manager, pipecat-ai 1.8.0). Queued after the
    # refusal check, so a refused call stays silent.
    await flow_manager.worker.queue_frame(TTSSpeakFrame("Let me check."))
    result = tools.check_availability.check_availability(**dict(args))
    if inspect.isawaitable(result):
        result = await result
    return result


async def _flow_tool_create_booking(args, flow_manager, state=None):
    """Create one confirmed booking for the verified customer using an exact slot returned by availability. Never invent a slot or a phone number."""
    refusal = _refusal(
        "create_booking",
        state,
        [
            (
                "customer_phone",
                "The caller's phone number in E.164: a plus sign, then digits, with no spaces, brackets or dashes. One shape for every phone number in this package, the MANAGER_PHONE_NUMBER transfer destination included, so no prompt and no tool has to guess which shape it is holding.\nNo `source:` here on purpose. The prefetch block below reads the carrier's fact and this variable receives it, which leaves the per-route refusal for a variable naming a source its target cannot supply exactly as strict as it is: on a route with no caller ID the entry skips and this holds its default.\nOffered to the caller for a yes, never acted on unasked. Somebody may be ringing from a friend's phone, or may hold a second account, so until the verification step has heard them agree this value satisfies no `requires:` guard and appears in no prompt but that step's own.\nAn earlier version of this note said no prompt ever reads the number back. That is now false rather than merely out of date: reading it back is the whole saving, and it replaced twelve spoken digits with one yes. The read-back turn does not cache, and that trade was made deliberately.",
            )
        ],
    )
    if refusal:
        return {"refused": refusal}
    # Cover the wait, same contract as _direct_tool: TTSSpeakFrame is a
    # DataFrame, so it is queued and speech starts while the handler body runs.
    # A flows handler holds a FlowManager rather than FunctionCallParams, and
    # FlowManager.worker is the documented seam for queueing a frame from inside
    # a handler (pipecat.flows.manager, pipecat-ai 1.8.0). Queued after the
    # refusal check, so a refused call stays silent.
    await flow_manager.worker.queue_frame(TTSSpeakFrame("Booking that in."))
    result = tools.create_booking.create_booking(
        **dict(args), customer_phone=state.customer_phone
    )
    if inspect.isawaitable(result):
        result = await result
    return result


async def _flow_tool_find_or_create_customer(args, flow_manager):
    """Look up or create one salon customer from the exact confirmed phone number. Use only after the digit readback got a clear yes. Reuse the record that already owns the number, or create one for a number that is new. Never guess a number or pass one the caller has not confirmed."""
    result = tools.find_or_create_customer.find_or_create_customer(**dict(args))
    if inspect.isawaitable(result):
        result = await result
    return result


async def _flow_tool_list_bookings(args, flow_manager, state=None):
    """List the verified customer's active bookings. The phone number comes from verification and is never supplied or guessed by the model."""
    refusal = _refusal(
        "list_bookings",
        state,
        [
            (
                "customer_phone",
                "The caller's phone number in E.164: a plus sign, then digits, with no spaces, brackets or dashes. One shape for every phone number in this package, the MANAGER_PHONE_NUMBER transfer destination included, so no prompt and no tool has to guess which shape it is holding.\nNo `source:` here on purpose. The prefetch block below reads the carrier's fact and this variable receives it, which leaves the per-route refusal for a variable naming a source its target cannot supply exactly as strict as it is: on a route with no caller ID the entry skips and this holds its default.\nOffered to the caller for a yes, never acted on unasked. Somebody may be ringing from a friend's phone, or may hold a second account, so until the verification step has heard them agree this value satisfies no `requires:` guard and appears in no prompt but that step's own.\nAn earlier version of this note said no prompt ever reads the number back. That is now false rather than merely out of date: reading it back is the whole saving, and it replaced twelve spoken digits with one yes. The read-back turn does not cache, and that trade was made deliberately.",
            )
        ],
    )
    if refusal:
        return {"refused": refusal}
    # Cover the wait, same contract as _direct_tool: TTSSpeakFrame is a
    # DataFrame, so it is queued and speech starts while the handler body runs.
    # A flows handler holds a FlowManager rather than FunctionCallParams, and
    # FlowManager.worker is the documented seam for queueing a frame from inside
    # a handler (pipecat.flows.manager, pipecat-ai 1.8.0). Queued after the
    # refusal check, so a refused call stays silent.
    await flow_manager.worker.queue_frame(TTSSpeakFrame("Let me look."))
    result = tools.list_bookings.list_bookings(
        **dict(args), customer_phone=state.customer_phone
    )
    if inspect.isawaitable(result):
        result = await result
    return result


async def _flow_tool_modify_booking(args, flow_manager, state=None):
    """Atomically move one confirmed active booking owned by the verified customer to an exact available slot."""
    refusal = _refusal(
        "modify_booking",
        state,
        [
            (
                "customer_phone",
                "The caller's phone number in E.164: a plus sign, then digits, with no spaces, brackets or dashes. One shape for every phone number in this package, the MANAGER_PHONE_NUMBER transfer destination included, so no prompt and no tool has to guess which shape it is holding.\nNo `source:` here on purpose. The prefetch block below reads the carrier's fact and this variable receives it, which leaves the per-route refusal for a variable naming a source its target cannot supply exactly as strict as it is: on a route with no caller ID the entry skips and this holds its default.\nOffered to the caller for a yes, never acted on unasked. Somebody may be ringing from a friend's phone, or may hold a second account, so until the verification step has heard them agree this value satisfies no `requires:` guard and appears in no prompt but that step's own.\nAn earlier version of this note said no prompt ever reads the number back. That is now false rather than merely out of date: reading it back is the whole saving, and it replaced twelve spoken digits with one yes. The read-back turn does not cache, and that trade was made deliberately.",
            )
        ],
    )
    if refusal:
        return {"refused": refusal}
    # Cover the wait, same contract as _direct_tool: TTSSpeakFrame is a
    # DataFrame, so it is queued and speech starts while the handler body runs.
    # A flows handler holds a FlowManager rather than FunctionCallParams, and
    # FlowManager.worker is the documented seam for queueing a frame from inside
    # a handler (pipecat.flows.manager, pipecat-ai 1.8.0). Queued after the
    # refusal check, so a refused call stays silent.
    await flow_manager.worker.queue_frame(TTSSpeakFrame("Moving that now."))
    result = tools.modify_booking.modify_booking(
        **dict(args), customer_phone=state.customer_phone
    )
    if inspect.isawaitable(result):
        result = await result
    return result


async def _flow_tool_record_complaint(args, flow_manager, state=None):
    """Record the verified customer's complaint after enough facts are known. This does not replace a requested manager transfer."""
    refusal = _refusal(
        "record_complaint",
        state,
        [
            (
                "customer_phone",
                "The caller's phone number in E.164: a plus sign, then digits, with no spaces, brackets or dashes. One shape for every phone number in this package, the MANAGER_PHONE_NUMBER transfer destination included, so no prompt and no tool has to guess which shape it is holding.\nNo `source:` here on purpose. The prefetch block below reads the carrier's fact and this variable receives it, which leaves the per-route refusal for a variable naming a source its target cannot supply exactly as strict as it is: on a route with no caller ID the entry skips and this holds its default.\nOffered to the caller for a yes, never acted on unasked. Somebody may be ringing from a friend's phone, or may hold a second account, so until the verification step has heard them agree this value satisfies no `requires:` guard and appears in no prompt but that step's own.\nAn earlier version of this note said no prompt ever reads the number back. That is now false rather than merely out of date: reading it back is the whole saving, and it replaced twelve spoken digits with one yes. The read-back turn does not cache, and that trade was made deliberately.",
            )
        ],
    )
    if refusal:
        return {"refused": refusal}
    # Cover the wait, same contract as _direct_tool: TTSSpeakFrame is a
    # DataFrame, so it is queued and speech starts while the handler body runs.
    # A flows handler holds a FlowManager rather than FunctionCallParams, and
    # FlowManager.worker is the documented seam for queueing a frame from inside
    # a handler (pipecat.flows.manager, pipecat-ai 1.8.0). Queued after the
    # refusal check, so a refused call stays silent.
    await flow_manager.worker.queue_frame(TTSSpeakFrame("Noting that down."))
    result = tools.record_complaint.record_complaint(
        **dict(args), customer_phone=state.customer_phone
    )
    if inspect.isawaitable(result):
        result = await result
    return result


# --- transport & run --------------------------------------------------------
transport_params: dict = {
    "webrtc": lambda: TransportParams(audio_in_enabled=True, audio_out_enabled=True),
    # The runner assigns an inbound call's dial-in settings and Daily credentials
    # onto whatever this returns, so on the Daily route it has to be the params
    # class that declares them. The generic one rejects the assignment.
    "daily": lambda: TransportParams(audio_in_enabled=True, audio_out_enabled=True),
    # Your carrier's stream, terminated by Pipecat Cloud. The runner attaches the
    # carrier's frame serializer itself, reading the stream and call ids out of the
    # handshake, so this declares the audio directions and nothing else.
    "twilio": lambda: FastAPIWebsocketParams(
        audio_in_enabled=True, audio_out_enabled=True
    ),
}


def build_stt():
    return SlngSTTService(
        api_key=os.environ["SLNG_API_KEY"],
        model="soniox/speech-ai:rt-v5",
        language=Language("en"),
        world_part_override="eu",
    )


async def run_bot(transport: BaseTransport, runner_args: RunnerArguments) -> None:
    require_env()
    call_context = {}
    # Decided once, right after the transport exists: is this a phone call at all?
    # Everything a call needs is checked here, before the caller hears anything.
    # None means browser or console, which behave exactly as they do on a package
    # with no telephony.
    phone_call = _phone_session(runner_args)
    call_context["_phone_call"] = phone_call

    trace_provider = setup_langfuse_tracing()
    trace_attributes = {"langfuse.trace.name": TRACE_NAME}
    if runner_args.session_id is not None:
        trace_attributes["langfuse.session.id"] = runner_args.session_id

    runner = WorkerRunner(handle_sigint=runner_args.handle_sigint)

    # turn: local — end-of-turn detection runs on-device (Silero VAD). No API
    # key, no network hop; the turn binding in targets.yaml is advisory.
    context = LLMContext()
    state = build_state(call_context)
    # Facts knowable before the greeting, resolved once, here: after the state
    # exists so there is somewhere to put them, and before the agents are built
    # so every prompt they render already sees them.
    await _prefetch(state, call_context)
    agents = [
        ComplaintSpecialistAgent(
            state=state, context=context, call_context=call_context
        ),
        ConciergeAgent(state=state, context=context, call_context=call_context),
    ]
    user_aggregator, assistant_aggregator = LLMContextAggregatorPair(
        context,
        user_params=LLMUserAggregatorParams(
            # The FLOOR: how long silence has to last before speech counts as
            # stopped. Authored as endpointing_delay.
            #
            # Widening this window makes some turns SLOWER, not more patient.
            # pipecat-slng asks the bridge to finalise when VAD reports the caller
            # stopped, and a final that already arrived finds no request
            # outstanding, so the frame goes out unfinalized and Pipecat waits out
            # a flat 1.0s safety net. Observed transcripts arrive from 0.27s.
            vad_analyzer=SileroVADAnalyzer(params=VADParams(stop_secs=0.2)),
            # The CEILING, on the end-of-turn analyzer. pace: balanced.
            #
            # Note the two fields both spelled stop_secs. The one above is the
            # VAD's silence window; this one is how long the turn may run before
            # closing regardless. They are different fields and different numbers.
            #
            # The analyzer is Pipecat's own default stop strategy. It is
            # constructed explicitly here only so the ceiling is reachable: left
            # implicit it runs at SmartTurnParams' own 3s.
            user_turn_strategies=UserTurnStrategies(
                stop=[
                    TurnAnalyzerUserTurnStopStrategy(
                        turn_analyzer=LocalSmartTurnAnalyzerV3(
                            params=SmartTurnParams(stop_secs=1.6)
                        )
                    )
                ],
            ),
            # The stretches of the call the caller cannot talk over, lowered from
            # conversation.interruption. On a phone route the greeting is
            # protected by default: a phone leg has no echo cancellation, so
            # without this the agent hears its own opening line and cuts itself
            # off. Barge-in stays on everywhere else.
            user_mute_strategies=[MuteUntilFirstBotCompleteUserMuteStrategy()],
            user_idle_timeout=15,
        ),
    )
    bridge = BusBridgeProcessor(
        bus=runner.bus, worker_name=MAIN_NAME, name=f"{MAIN_NAME}::BusBridge"
    )
    pipeline = Pipeline(
        [
            transport.input(),
            build_stt(),
            user_aggregator,
            bridge,
            transport.output(),
            assistant_aggregator,
        ]
    )
    main = PipelineWorker(
        pipeline,
        name=MAIN_NAME,
        # The worker builds its own latency observer only when tracing is enabled,
        # and does not re-expose its events, so this one is always ours.
        observers=[dev_metrics_observer()],
        conversation_id=runner_args.session_id,
        enable_tracing=True,
        additional_span_attributes=trace_attributes,
        params=PipelineParams(
            enable_metrics=True,
            enable_usage_metrics=True,
            **_pipeline_audio_rates(phone_call),
        ),
    )

    enable_agent_tracing(main, agents)

    @user_aggregator.event_handler("on_user_turn_idle")
    async def on_user_turn_idle(aggregator):
        await aggregator.push_frame(
            LLMMessagesAppendFrame(
                [
                    {
                        "role": "developer",
                        "content": "The caller has gone quiet. Politely check if they are still there.",
                    }
                ],
                run_llm=True,
            )
        )

    # inactivity.end_after: the nudge above is the first half, and this is the
    # second. Without it an idle call is nudged forever and never hung up, which
    # on a phone line is a billed open call. The clock starts at the first idle
    # turn and is cancelled by the caller speaking again.
    _idle_end: asyncio.Task | None = None

    @user_aggregator.event_handler("on_user_turn_idle")
    async def on_user_turn_idle_end(aggregator):
        nonlocal _idle_end
        if _idle_end is None or _idle_end.done():
            _idle_end = asyncio.create_task(_end_after(main, 45 - 15))

    @user_aggregator.event_handler("on_user_turn_started")
    async def on_user_turn_started(aggregator, strategy):
        nonlocal _idle_end
        if _idle_end is not None and not _idle_end.done():
            _idle_end.cancel()
            _idle_end = None

    runner_ready = asyncio.Event()
    pipeline_started = asyncio.Event()
    worker_start_error = None
    entry_started = False

    async def activate_entry():
        nonlocal entry_started
        if entry_started or worker_start_error is not None:
            return
        entry_started = True
        await main.activate_worker(
            "concierge",
            args=LLMWorkerActivationArgs(run_llm=False),
        )
        await next(agent for agent in agents if agent.name == "concierge").queue_frame(
            TTSSpeakFrame(
                "Hi, you've reached Sage and Stone. Robin speaking, what can I do for you?"
            )
        )

        asyncio.create_task(_end_after(main, 900))

    @runner.event_handler("on_ready")
    async def on_runner_ready(runner):
        runner_ready.set()

    @main.event_handler("on_pipeline_started")
    async def on_pipeline_started(worker, frame):
        nonlocal worker_start_error
        await runner_ready.wait()
        try:
            await runner.add_workers(*agents)
        except Exception as error:
            worker_start_error = error
            # Logged here, where it is still known. Cancelling the runner makes
            # run() raise CancelledError, and that reaches the caller before the
            # re-raise below ever runs, so this error is destroyed on its way
            # out: measured 2026-08-21, a session died half a second in and
            # reported nothing but a failed trace flush.
            logger.exception("the agent workers failed to start")
            pipeline_started.set()
            await runner.cancel(reason="agent worker startup failed")
            return
        pipeline_started.set()

    @transport.event_handler("on_client_connected")
    async def on_client_connected(transport, client):
        # Telephony transports (Daily SIP, the platform's carrier stream) have
        # no RTVI client-ready handshake: the carrier opens the media stream and
        # the bot initiates once that connection is open and main's StartFrame
        # has traversed (Pipecat Twilio dial-in flow; SPEC V2).
        #
        # This is also all an outbound call needs. Your carrier opens the stream
        # when the person answers, so the greeting below meets a human rather
        # than ringback, and there is no ringing state to handle.
        await pipeline_started.wait()
        await activate_entry()

    @transport.event_handler("on_client_disconnected")
    async def on_client_disconnected(transport, client):
        await runner.cancel()

    try:
        await runner.add_workers(main)
        await runner.run()
        if worker_start_error is not None:
            raise worker_start_error
    finally:
        primary_error = sys.exception()
        if primary_error is not None:
            try:
                await asyncio.to_thread(flush_tracing, trace_provider)
            except BaseException as cleanup_error:
                logger.error(
                    "Tracing flush failed while preserving the primary error ({})",
                    type(cleanup_error).__name__,
                )
        else:
            await asyncio.to_thread(flush_tracing, trace_provider)


async def bot(runner_args: RunnerArguments) -> None:
    _configure_logging()

    transport = await create_transport(runner_args, transport_params)
    await run_bot(transport, runner_args)


if __name__ == "__main__":
    from pipecat.runner.run import main

    main()
