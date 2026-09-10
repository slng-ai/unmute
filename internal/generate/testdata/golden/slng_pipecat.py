"""Generated Pipecat agent for safe-core-fixture.

Compiled by `unmute`; do not edit by hand. Prompts, model routes, and the agent
graph are baked in from the package. Secret values are read from the environment
and never written here. The agency model uses the Pipecat workers API: a main
PipelineWorker owns the transport + STT, each agent is an LLMWorker with its own
LLM and voice, and agent_transfer is activate_worker(). Tasks and task groups
run as Pipecat Flows on the owning agent: a delegate tool snapshots the shared
context, a FlowManager walks the steps as nodes, and control returns with only
a completed or unserved status.
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
import uuid
from dataclasses import dataclass
from urllib.parse import quote

import httpx
from openai import AsyncOpenAI, DefaultAsyncHttpxClient
from dotenv import load_dotenv
from loguru import logger
from pydantic import BaseModel, TypeAdapter, ValidationError

from pipecat.audio.turn.smart_turn.base_smart_turn import SmartTurnParams
from pipecat.audio.turn.smart_turn.local_smart_turn_v3 import LocalSmartTurnAnalyzerV3
from pipecat.audio.vad.silero import SileroVADAnalyzer
from pipecat.audio.vad.vad_analyzer import VADParams
from pipecat.bus import BusBridgeProcessor
from pipecat.flows import ContextStrategy, ContextStrategyConfig, FlowManager, FlowsFunctionSchema, NodeConfig, NO_RESPONSE
from pipecat.frames.frames import EndFrame, FunctionCallResultProperties, LLMMessagesAppendFrame, LLMRunFrame, LLMUpdateSettingsFrame, TTSSpeakFrame
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
from pipecat.transports.base_transport import BaseTransport, TransportParams
from pipecat.turns.user_start import MinWordsUserTurnStartStrategy
from pipecat.turns.user_stop import TurnAnalyzerUserTurnStopStrategy
from pipecat.turns.user_turn_strategies import UserTurnStrategies
from pipecat.workers.llm import LLMWorker, LLMWorkerActivationArgs, tool
from pipecat.workers.runner import WorkerRunner

from dev_metrics import install_dev_metrics

from pipecat.services.deepgram.stt import DeepgramSTTService
from pipecat.services.openai.llm import OpenAILLMService
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
# Provider credentials only. The telephony route's environment (Redis, carrier
# keys, the public URL) is required by telephony.py, not here, so a telephony
# package still runs in the browser with nothing but model keys (V10/B3).
REQUIRED_ENV = [
    "DEEPGRAM_API_KEY",
    "GET_INVOICE_URL",
    "LOOKUP_CUSTOMER_URL",
    "OPENAI_API_KEY",
    "SLNG_API_KEY",
]
IGNORE_PHRASES = ["okay", "right", "uh-huh"]


def require_env() -> None:
    missing = [name for name in REQUIRED_ENV if not os.getenv(name)]
    if missing:
        raise RuntimeError(f"Missing required environment variables: {', '.join(missing)}")

# --- SLNG Context Router ----------------------------------------------------
# The router answers a repeated turn from its cache instead of calling the model.
# That is why a router-bound system prompt below keeps its placeholders instead of
# being rendered here: the router substitutes them from template_variables, so the
# prompt it sees is identical on every call, which is what lets a turn repeat. A
# first turn never caches, and the router decides which later turns are
# repeatable, so a repeat served by the model is expected rather than a fault.
#
# The agent id header scopes that cache, and it is one value per prompt rather
# than one for the package: each agent and each task sends the authored agent_id,
# a colon, then its own name. The cache key is the last exchange and carries no
# system prompt, so two prompts under one scope get served each other's answers.
# A task runs on its owner's service here, so entering one swaps its scope in and
# every way out swaps the owner's back.


def _slng_config_fast_reasoning() -> dict:
    """The model configuration, sent inline in the body of every think request.

    Credentials are read here rather than at import, so a missing one is reported
    by require_env() alongside every other name instead of raising a KeyError
    before anything has checked. No value is written into this file: each is the
    name of an environment variable.
    """
    return {"tiers": {"1": [{"endpoint": {"api_key": os.environ["OPENAI_API_KEY"], "url": "https://api.openai.com/v1"}, "model": "gpt-5.6-luna", "weight": 100}]}}

async def _slng_log_provenance(response) -> None:
    """Say where this answer came from, once per think request.

    The router states this only in response headers, and neither framework hands
    them to us, so a hook on the client is the one place that sees them. Without
    it the question an operator actually asks about a cache, whether it is
    working, has no answer in this run's own log.

    Three rules, from httpx's own event-hooks documentation, each of which would
    be a live-call defect if broken. It has to be async, because a sync callable
    on an AsyncClient is never awaited. It reads headers only: the hook runs
    before the body is read, so touching the body would consume the stream the
    framework is about to iterate. And it cannot raise, because a raising
    response hook fails the request it was only meant to describe.

    It also logs only a router think request. The scope header is what lets the
    line name a scope at all, so a request without one is a request this hook
    could not describe.
    """
    try:
        scope = response.request.headers.get("X-Slng-Agent-Id")
        if not scope:
            return
        # Field order is the contract, and the gate reads it off this line.
        fields = ["scope=" + scope]
        fields.append("source=" + response.headers.get("x-slng-response-source", "unknown"))
        if response.headers.get("x-slng-cache-layer"):
            fields.append("layer=" + response.headers["x-slng-cache-layer"])
        if response.headers.get("x-slng-model"):
            fields.append("model=" + response.headers["x-slng-model"])
        fields.append("request_id=" + response.headers.get("x-slng-request-id", "unknown"))
        logger.info("slng router: " + " ".join(fields))
    except Exception:  # noqa: BLE001 - a log line must never end a call
        logger.debug("could not read the router's provenance headers", exc_info=True)



class _SlngRouterLLMService(OpenAILLMService):
    """The router's LLM service: a response hook, and per-request variables.

    Two overrides, two different seams, and the second one is why this class
    holds any state.

    create_client is the only seam for reading the response headers: the service
    builds its own AsyncOpenAI and hands us neither the raw response nor its
    headers, and the router states where an answer came from only in a header.
    The connection limits restate the base class's own (pipecat
    services/openai/base_llm.py create_client at the pinned version). Restating
    them is deliberate. Anything different here would change connection reuse,
    which is a latency change nobody asked for and nothing would report.

    build_chat_completion_params is the seam for the request body. The base
    class's last statement before returning is params.update(self._settings.extra),
    which reads a settings snapshot mutated only by an explicit update: nothing
    re-evaluates the expression that filled it, so a value written part way
    through a step reached the model one turn late. This override reads the live
    state instead, on the streaming path and the one-shot path both, which is
    what makes the two targets refresh at the same point.
    """

    def __init__(self, *args, slng_state=None, **kwargs):
        super().__init__(*args, **kwargs)
        self._slng_state = slng_state

    def build_chat_completion_params(self, params_from_context) -> dict:
        params = super().build_chat_completion_params(params_from_context)
        scope = (params.get("extra_headers") or {}).get("X-Slng-Agent-Id", "")
        body = dict(params.get("extra_body") or {})
        body["template_variables"] = _slng_template_variables(
            self._slng_state, _SLNG_TEMPLATE_PATHS.get(scope, ()), scope=scope
        )
        params["extra_body"] = body
        return params

    def create_client(self, api_key=None, base_url=None, **kwargs):
        return AsyncOpenAI(
            api_key=api_key,
            base_url=base_url,
            http_client=DefaultAsyncHttpxClient(
                limits=httpx.Limits(
                    max_keepalive_connections=100,
                    max_connections=1000,
                    keepalive_expiry=None,
                ),
                event_hooks={"response": [_slng_log_provenance]},
            ),
        )



_SLNG_VARIABLE_LIMIT = 4000
_SLNG_TEMPLATE_PATHS = {"safe-core-router-v3:billing": ["customer_id", "caller_alias"], "safe-core-router-v3:intake": ["customer_id", "caller_alias"], "safe-core-router-v3:task.collect": ["customer_id"], "safe-core-router-v3:task.confirm": []}
_SLNG_SCOPE_SITES = {"safe-core-router-v3:task.collect": "task:collect", "safe-core-router-v3:task.confirm": "task:confirm"}


def _slng_template_variables(state, names, *, scope="") -> dict:
    """The values the router substitutes into the prompt's placeholders.

    A name with no value yet sends the empty string, never None and never the text
    "None". A value over the router's limit is truncated with a warning rather
    than dropped: an over-long value must not end a live call.
    """
    values = {}
    for name in names:
        # The bound is measured on the JSON rendering, not on the repr of the
        # object: they are different lengths, and the one the router receives is
        # the one that has to fit. _state_text carries its own bound and its own
        # warning, so a declared value is already shortened by here. A path
        # arrives as one flat name, and _state_lookup walks it.
        text = _state_text(*_prompt_value(state, name, _SLNG_SCOPE_SITES.get(scope, "")))
        if len(text) > _SLNG_VARIABLE_LIMIT:
            logger.warning(
                "template variable {} is {} characters; truncating to {} for the router",
                name,
                len(text),
                _SLNG_VARIABLE_LIMIT,
            )
            text = text[:_SLNG_VARIABLE_LIMIT]
        values[name] = text
    return values


def _direct_tool(fn=None, *, cancel_on_interruption=True, timeout_secs=None):
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
                await params.result_callback({
                    "error": (
                        f"Unexpected arguments for {params.function_name}: "
                        f"{', '.join(unexpected)}. Allowed arguments: {allowed}. "
                        "Retry with only allowed arguments."
                    )
                })
                return

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
                logger.exception("tool failed before completing: {}", params.function_name)
                if not resolved:
                    await original_result_callback({
                        "error": (
                            f"{params.function_name} failed before completing. "
                            "Do not claim success; retry only with corrected input."
                        )
                    })
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



# --- declared state ----------------------------------------------------------
# Generated from the `shapes:` and the typed `variables:` in agent.yaml. Both
# target frameworks already depend on Pydantic, so nothing here adds one. The one
# exception is EmailStr, which is checked by email-validator: declaring it puts
# that package in this project's pyproject.toml, and declaring no email type
# leaves both the import and the dependency out.
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
    "collect": {
        "tier": TypeAdapter(str),
    },
    "confirm": {
        "confirmed": TypeAdapter(bool),
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
    "caller_alias": TypeAdapter(str),
    "customer_id": TypeAdapter(str),
    "verified": TypeAdapter(bool),
}
_TASK_ASSIGNMENTS = {
    "collect": [
    ],
    "confirm": [
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


@dataclass
class State:
    """Typed call variables (SCHEMA 4.4), shared across agents."""

    caller_alias: str | None = None  # What the caller says to call them.
    customer_id: str | None = None
    verified: bool = False


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
    if "caller_alias" in call_start:
        setattr(state, "caller_alias", call_start["caller_alias"])
    if "customer_id" in call_start:
        setattr(state, "customer_id", call_start["customer_id"])
    if "verified" in call_start:
        setattr(state, "verified", call_start["verified"])
    if missing:
        raise RuntimeError(f"Missing call context fields: {', '.join(missing)}")
    return state

_TEMPLATE = re.compile(r"\{\{\s*([a-z_][a-z0-9_]*)\s*\}\}")


def _render(text: str, state, *, quote_values: bool = False, site: str = "") -> str:
    """Substitute each variable token from the call state (SCHEMA 4.4 templates).
    Only substituted values are URL-encoded, never the surrounding literal."""
    if state is None:
        return text

    def _one(match: re.Match[str]) -> str:
        # Through _state_lookup, which walks a path emitted as one flat name, and
        # _state_text, so a declared value renders as compact JSON and an empty
        # one as words. Anything not declared structured comes back exactly as
        # str() gave it.
        value = _state_text(*_prompt_value(state, match.group(1), site))
        return quote(value, safe="") if quote_values else value

    return _TEMPLATE.sub(_one, text)

async def _end_after(worker: PipelineWorker, timeout_secs: float) -> None:
    await asyncio.sleep(timeout_secs)
    await worker.queue_frame(EndFrame())

# --- prompts ----------------------------------------------------------------
# Agent system instructions as module constants: one copy each, referenced by
# the LLM builder and any Flow restore (V2).
BILLING_PROMPT = """# Billing agent (placeholder prompt)

You are the billing specialist for Acme Support. This is a phone call, so keep every answer to one or two short sentences.

- The caller was handed to you because they have a billing question. The conversation so far is in your context.
- Use `get_invoice` to look up the caller's invoices. It takes the customer id, which the earlier lookup already established.
- Explain charges calmly and clearly, one item at a time.
- If the caller is not satisfied, explain what a human support team would need to review.


The caller is {{customer_id}}, who goes by {{caller_alias}}."""
INTAKE_PROMPT = """# Intake agent (placeholder prompt)

You are the front desk voice agent for Acme Support. This is a phone call, so keep every answer to one or two short sentences.

- Greet the caller and find out what they need.
- When they give a phone number or email, use `lookup_customer` to find their record.
- If the caller asks about billing, an invoice, or a refund, hand off to the billing agent with `to_billing`.
- Never guess account details. If you cannot find the customer, say so and ask again.


The caller is {{customer_id}}, who goes by {{caller_alias}}."""


# --- agents -----------------------------------------------------------------



def build_billing_llm(state=None, *, slng_session_id):
    return _SlngRouterLLMService(
        api_key=os.environ["SLNG_API_KEY"],
        base_url="https://eu.context-router.slng.ai/v1",
        slng_state=state,
        settings=OpenAILLMService.Settings(
            model="gpt-5.6-luna",
            system_instruction=BILLING_PROMPT,
            extra={"extra_body": {"reasoning_effort": "none", "slng_config": _slng_config_fast_reasoning()}, "extra_headers": {"X-Slng-Agent-Id": "safe-core-router-v3:billing", "X-Slng-Session-Id": slng_session_id}},
        ),
    )


def build_billing_tts():
    return SlngTTSService(
        api_key=os.environ["SLNG_API_KEY"],
        voice="aura-2-orion-en",
        model="slng/deepgram/aura:2-en",
    )


class BillingAgent(LLMWorker):
    """Agent: billing."""

    def __init__(self, state=None, context=None, call_context=None, dev_metrics=None, *, slng_session_id) -> None:
        self.state = state
        if context is not None:
            self.context = context
        # Kept on self because a task's headers are swapped in from a method
        # body, where the constructor's parameter is out of scope, and a whole
        # header dict has to carry this along with the scope.
        self._slng_session_id = slng_session_id

        llm = build_billing_llm(state, slng_session_id=slng_session_id)
        tts = build_billing_tts()
        self._dev = dev_metrics
        if self._dev:
            self._dev.observe_llm(llm)
            self._dev.observe_tts(tts)
        super().__init__("billing", llm=llm, pipeline=Pipeline([llm, tts]), bridged=())

    async def queue_frame(self, frame, *args, **kwargs) -> None:
        if self._dev:
            self._dev.stamp(frame)
        await super().queue_frame(frame, *args, **kwargs)

    async def on_activated(self, args) -> None:
        token = self._dev.enter_activation(args) if self._dev else None
        try:
            await self.queue_frame(LLMUpdateSettingsFrame(
                delta=LLMSettings(system_instruction=BILLING_PROMPT,
                    extra={"extra_headers": {"X-Slng-Agent-Id": "safe-core-router-v3:billing", "X-Slng-Session-Id": self._slng_session_id}}),
            ))
            await super().on_activated(args)
            # Pipecat 1.8 only runs on activation when messages are nonempty.
            # A handoff already shaped the shared context; request the reply
            # without adding a synthetic message or waiting for the caller.
            if args and args.get("run_llm") and not args.get("messages"):
                await self.queue_frame(LLMRunFrame())
        finally:
            if self._dev:
                self._dev.exit_activation(token)



    @_direct_tool
    async def get_invoice(self, params: FunctionCallParams, customer_id: str):
        """Fetch the most recent invoice for a customer id. Returns the invoice total and status.

        Args:
            customer_id (str): The customer id from lookup_customer
        """
        async with httpx.AsyncClient() as client:
            response = await client.post(
                os.environ["GET_INVOICE_URL"],
                json={"customer_id": customer_id},
                timeout=30.0,
            )
            response.raise_for_status()
            await params.result_callback(response.json())



def build_intake_llm(state=None, *, slng_session_id):
    return _SlngRouterLLMService(
        api_key=os.environ["SLNG_API_KEY"],
        base_url="https://eu.context-router.slng.ai/v1",
        slng_state=state,
        settings=OpenAILLMService.Settings(
            model="gpt-5.6-luna",
            system_instruction=INTAKE_PROMPT,
            extra={"extra_body": {"reasoning_effort": "none", "slng_config": _slng_config_fast_reasoning()}, "extra_headers": {"X-Slng-Agent-Id": "safe-core-router-v3:intake", "X-Slng-Session-Id": slng_session_id}},
        ),
    )


def build_intake_tts():
    return SlngTTSService(
        api_key=os.environ["SLNG_API_KEY"],
        voice="aura-2-thalia-en",
        model="slng/deepgram/aura:2-en",
    )


class IntakeAgent(LLMWorker):
    """Agent: intake."""

    def __init__(self, state=None, context=None, call_context=None, dev_metrics=None, *, slng_session_id) -> None:
        self.state = state
        if context is not None:
            self.context = context
        # Kept on self because a task's headers are swapped in from a method
        # body, where the constructor's parameter is out of scope, and a whole
        # header dict has to carry this along with the scope.
        self._slng_session_id = slng_session_id

        llm = build_intake_llm(state, slng_session_id=slng_session_id)
        tts = build_intake_tts()
        self._dev = dev_metrics
        if self._dev:
            self._dev.observe_llm(llm)
            self._dev.observe_tts(tts)
        super().__init__("intake", llm=llm, pipeline=Pipeline([llm, tts]), bridged=())

    async def queue_frame(self, frame, *args, **kwargs) -> None:
        if self._dev:
            self._dev.stamp(frame)
        await super().queue_frame(frame, *args, **kwargs)

    async def on_activated(self, args) -> None:
        token = self._dev.enter_activation(args) if self._dev else None
        try:
            await self.queue_frame(LLMUpdateSettingsFrame(
                delta=LLMSettings(system_instruction=INTAKE_PROMPT,
                    extra={"extra_headers": {"X-Slng-Agent-Id": "safe-core-router-v3:intake", "X-Slng-Session-Id": self._slng_session_id}}),
            ))
            await super().on_activated(args)
            # Pipecat 1.8 only runs on activation when messages are nonempty.
            # A handoff already shaped the shared context; request the reply
            # without adding a synthetic message or waiting for the caller.
            if args and args.get("run_llm") and not args.get("messages"):
                await self.queue_frame(LLMRunFrame())
        finally:
            if self._dev:
                self._dev.exit_activation(token)



    @_direct_tool(cancel_on_interruption=False)
    async def to_billing(self, params: FunctionCallParams):
        """Caller asks about billing, an invoice, or a refund."""
        # context.history on this handoff. One LLMContext is shared for the whole
        # call, so the receiver is given the shaped list rather than a copy.
        self.context.set_messages([dict(m) for m in self.context.get_messages() if m.get("role") in ("user", "assistant", "tool")])
        await self.activate_worker(
            "billing",
            args=LLMWorkerActivationArgs(
                metadata=self._dev.activation_metadata() if self._dev else None,
                messages=[],
                run_llm=True,
            ),
            deactivate_self=True,
            result_callback=params.result_callback,
        )

    @_direct_tool
    async def lookup_customer(self, params: FunctionCallParams, email: str = "", phone: str = ""):
        """Look up a customer record by phone number or email. Returns the customer id and name.

        Args:
            email (str): Caller email address
            phone (str): Caller phone number in E.164 form
        """
        async with httpx.AsyncClient() as client:
            response = await client.post(
                os.environ["LOOKUP_CUSTOMER_URL"],
                json={"email": email, "phone": phone},
                timeout=30.0,
            )
            response.raise_for_status()
            await params.result_callback(response.json())

    @_direct_tool
    async def run_collect(self, params: FunctionCallParams):
        """Collect the caller's account details."""
        self._run_collect_visit = object()
        self._run_collect_results = {}
        self._run_collect_active_step = "collect"
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
            {"status": "running the collect task"},
            properties=FunctionCallResultProperties(run_llm=False),
        )
        # V2/B2: drain the resolved owner call before snapshotting. Otherwise
        # restoration erases that call and the unchanged request delegates again.
        await self.flush_pipeline()
        self._run_collect_snapshot = (copy.deepcopy(self.context.get_messages()), self.context.tools)
        # The step's own cache scope, queued before the node is entered so it is
        # ahead of the frame that triggers the step's first completion. That
        # first request is the one that collided: it asks with the task's prompt,
        # so it has to ask under the task's scope, not this agent's.
        await self.queue_frame(LLMUpdateSettingsFrame(
            delta=LLMSettings(extra={"extra_headers": {"X-Slng-Agent-Id": "safe-core-router-v3:task.collect", "X-Slng-Session-Id": self._slng_session_id}}),
        ))
        # context.history on this task. Shaped after the snapshot above, so the
        # finish path restores the owner's own context whatever this step saw.
        self.context.set_messages([dict(m) for m in self.context.get_messages() if m.get("role") in ("user", "assistant", "tool")])
        await flow.initialize(self._run_collect_node_collect())

    def _run_collect_node_collect(self) -> NodeConfig:
        self.context.set_messages([dict(m) for m in self.context.get_messages() if m.get("role") in ("user", "assistant", "tool")])
        return NodeConfig(
            name="collect",
            role_message="Ask for the caller's email and confirm the account for {{customer_id}}.\n\nWhen this step is complete, call `finish_run_collect_collect` with: tier.\n\n`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish_run_collect_collect` with their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that status and takes the caller from there.",
            task_messages=[{"role": "developer", "content": "Begin this step."}],
            functions=[
                FlowsFunctionSchema(
                    name="lookup_customer",
                    description="Look up a customer record by phone number or email. Returns the customer id and name.",
                    properties={"email": {"description": "Caller email address", "type": "string"}, "phone": {"description": "Caller phone number in E.164 form", "type": "string"}},
                    required=[],
                    handler=_flow_tool_lookup_customer,
                ),
                FlowsFunctionSchema(
                    name="finish_run_collect_collect",
                    description="Record the result of this step and finish.",
                    properties={"unserved_request": {"description": "Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.", "type": "string"}, "tier": _schema(TypeAdapter(str | None))},
                    required=[],
                    handler=_flow_visit(self, "run_collect", self._run_collect_finish_collect),
                ),
            ],
        )

    async def _run_collect_finish_collect(self, args, flow_manager):
        if self._run_collect_active_step != "collect":
            return {"status": "already handled"}, NO_RESPONSE
        # Validated before anything is recorded: a value that does not fit its
        # declared type never enters the state, the previous contents stand, and
        # the message goes back to the model so it can correct itself on the next
        # turn instead of the step recording something wrong. This framework
        # validates no tool argument itself, so without this the two targets
        # would behave differently on the same package.
        try:
            _values = _save_result("collect", self.state, dict(args))
        except _StateRefused as refused:
            logger.warning("finish {}: {}", "collect", refused.message)
            return {"refused": f"Not recorded: {refused.message}. Ask again, then call finish with a value that fits."}, None
        self._run_collect_results["collect"] = _values
        self._run_collect_active_step = None
        # then: return — restore the owner's pre-flow context (messages and
        # tools); only a completed or unserved status crosses back.
        messages, tools = self._run_collect_snapshot
        _settle_task_call(messages, "run_collect", _group_status(self._run_collect_results))
        await self.queue_frame(LLMUpdateSettingsFrame(
            delta=LLMSettings(system_instruction=INTAKE_PROMPT,
                # The owner's cache scope goes back with its prompt. A leaked
                # task scope is the same defect pointing the other way.
                extra={"extra_headers": {"X-Slng-Agent-Id": "safe-core-router-v3:intake", "X-Slng-Session-Id": self._slng_session_id}}),
        ))
        await self.flush_pipeline()
        self.context.set_messages(messages + [{
            "role": "developer",
            "content": json.dumps(_group_status(self._run_collect_results)),
        }])
        self.context.set_tools(tools)
        return {"status": "ok"}, None

    @_direct_tool
    async def run_triage(self, params: FunctionCallParams):
        """Run the triage group."""
        self._run_triage_visit = object()
        self._run_triage_results = {}
        self._run_triage_active_step = "collect"
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
            {"status": "running the triage flow"},
            properties=FunctionCallResultProperties(run_llm=False),
        )
        # V2/B2: drain the resolved owner call before snapshotting. Otherwise
        # restoration erases that call and the unchanged request delegates again.
        await self.flush_pipeline()
        self._run_triage_snapshot = (copy.deepcopy(self.context.get_messages()), self.context.tools)
        # The step's own cache scope, queued before the node is entered so it is
        # ahead of the frame that triggers the step's first completion. That
        # first request is the one that collided: it asks with the task's prompt,
        # so it has to ask under the task's scope, not this agent's.
        await self.queue_frame(LLMUpdateSettingsFrame(
            delta=LLMSettings(extra={"extra_headers": {"X-Slng-Agent-Id": "safe-core-router-v3:task.collect", "X-Slng-Session-Id": self._slng_session_id}}),
        ))
        # context.history on this task. Shaped after the snapshot above, so the
        # finish path restores the owner's own context whatever this step saw.
        self.context.set_messages([dict(m) for m in self.context.get_messages() if m.get("role") in ("user", "assistant", "tool")])
        await flow.initialize(self._run_triage_node_collect())

    def _run_triage_node_collect(self) -> NodeConfig:
        self.context.set_messages([])
        return NodeConfig(
            name="collect",
            role_message="Ask for the caller's email and confirm the account for {{customer_id}}.\n\nWhen this step is complete, call `finish_run_triage_collect` with: tier.\n\n`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish_run_triage_collect` with their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that status and takes the caller from there.",
            task_messages=[{"role": "developer", "content": "Begin this step."}],
            functions=[
                FlowsFunctionSchema(
                    name="lookup_customer",
                    description="Look up a customer record by phone number or email. Returns the customer id and name.",
                    properties={"email": {"description": "Caller email address", "type": "string"}, "phone": {"description": "Caller phone number in E.164 form", "type": "string"}},
                    required=[],
                    handler=_flow_tool_lookup_customer,
                ),
                FlowsFunctionSchema(
                    name="finish_run_triage_collect",
                    description="Record the result of this step and finish.",
                    properties={"unserved_request": {"description": "Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.", "type": "string"}, "tier": _schema(TypeAdapter(str | None))},
                    required=[],
                    handler=_flow_visit(self, "run_triage", self._run_triage_finish_collect),
                ),
            ],
            context_strategy=ContextStrategyConfig(strategy=ContextStrategy.RESET),
        )

    async def _run_triage_finish_collect(self, args, flow_manager):
        if self._run_triage_active_step != "collect":
            return {"status": "already handled"}, NO_RESPONSE
        # Validated before anything is recorded: a value that does not fit its
        # declared type never enters the state, the previous contents stand, and
        # the message goes back to the model so it can correct itself on the next
        # turn instead of the step recording something wrong. This framework
        # validates no tool argument itself, so without this the two targets
        # would behave differently on the same package.
        try:
            _values = _save_result("collect", self.state, dict(args))
        except _StateRefused as refused:
            logger.warning("finish {}: {}", "collect", refused.message)
            return {"refused": f"Not recorded: {refused.message}. Ask again, then call finish with a value that fits."}, None
        self._run_triage_results["collect"] = _values
        self._run_triage_active_step = "confirm"
        # The next step is a different prompt site, so it asks under a different
        # cache scope. Queued before the node is handed back, for the same reason
        # the first step's was.
        await self.queue_frame(LLMUpdateSettingsFrame(
            delta=LLMSettings(extra={"extra_headers": {"X-Slng-Agent-Id": "safe-core-router-v3:task.confirm", "X-Slng-Session-Id": self._slng_session_id}}),
        ))
        return _task_status(self._run_triage_results["collect"]), self._run_triage_node_confirm()

    def _run_triage_node_confirm(self) -> NodeConfig:
        self.context.set_messages([])
        return NodeConfig(
            name="confirm",
            role_message="Read the booking back and ask the caller to confirm.\n\nWhen this step is complete, call `finish_run_triage_confirm` with: confirmed.\n\n`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish_run_triage_confirm` with their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that status and takes the caller from there.",
            task_messages=[{"role": "developer", "content": "Begin this step."}],
            functions=[
                FlowsFunctionSchema(
                    name="finish_run_triage_confirm",
                    description="Record the result of this step and finish.",
                    properties={"unserved_request": {"description": "Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.", "type": "string"}, "confirmed": _schema(TypeAdapter(bool | None))},
                    required=[],
                    handler=_flow_visit(self, "run_triage", self._run_triage_finish_confirm),
                ),
            ],
            context_strategy=ContextStrategyConfig(strategy=ContextStrategy.RESET),
        )

    async def _run_triage_finish_confirm(self, args, flow_manager):
        if self._run_triage_active_step != "confirm":
            return {"status": "already handled"}, NO_RESPONSE
        # Validated before anything is recorded: a value that does not fit its
        # declared type never enters the state, the previous contents stand, and
        # the message goes back to the model so it can correct itself on the next
        # turn instead of the step recording something wrong. This framework
        # validates no tool argument itself, so without this the two targets
        # would behave differently on the same package.
        try:
            _values = _save_result("confirm", self.state, dict(args))
        except _StateRefused as refused:
            logger.warning("finish {}: {}", "confirm", refused.message)
            return {"refused": f"Not recorded: {refused.message}. Ask again, then call finish with a value that fits."}, None
        self._run_triage_results["confirm"] = _values
        self._run_triage_active_step = None
        # then: return — restore the owner's pre-flow context (messages and
        # tools); only a completed or unserved status crosses back.
        messages, tools = self._run_triage_snapshot
        _settle_task_call(messages, "run_triage", _group_status(self._run_triage_results))
        await self.queue_frame(LLMUpdateSettingsFrame(
            delta=LLMSettings(system_instruction=INTAKE_PROMPT,
                # The owner's cache scope goes back with its prompt. A leaked
                # task scope is the same defect pointing the other way.
                extra={"extra_headers": {"X-Slng-Agent-Id": "safe-core-router-v3:intake", "X-Slng-Session-Id": self._slng_session_id}}),
        ))
        await self.flush_pipeline()
        self.context.set_messages(messages + [{
            "role": "developer",
            "content": json.dumps(_group_status(self._run_triage_results)),
        }])
        self.context.set_tools(tools)
        return {"status": "ok"}, None


def _settle_task_call(messages, name, status):
    """Replace this invocation's running reply before restoring the owner."""
    for message in reversed(messages):
        for call in message.get("tool_calls", []):
            if call.get("function", {}).get("name") == name:
                for reply in messages:
                    if reply.get("role") == "tool" and reply.get("tool_call_id") == call["id"]:
                        reply["content"] = json.dumps(status)
                return


def _flow_visit(worker, delegate, handler):
    visit = getattr(worker, "_" + delegate + "_visit", None)
    async def invoke(args, flow_manager):
        if getattr(worker, "_" + delegate + "_visit", None) is not visit:
            return {"status": "already handled"}, NO_RESPONSE
        return await handler(args, flow_manager)
    return invoke


# --- task tools (flows handlers) ----------------------------------------------
# Tools available inside task steps; stable module-level handlers so a
# re-registered function name always resolves to the same callable.


async def _flow_tool_lookup_customer(args, flow_manager):
    """Look up a customer record by phone number or email. Returns the customer id and name."""
    async with httpx.AsyncClient() as client:
        response = await client.post(os.environ["LOOKUP_CUSTOMER_URL"], json={**dict(args)}, timeout=30.0)
        response.raise_for_status()
        return response.json()


# --- transport & run --------------------------------------------------------
transport_params: dict = {
    "webrtc": lambda: TransportParams(audio_in_enabled=True, audio_out_enabled=True),
    # The runner assigns an inbound call's dial-in settings and Daily credentials
    # onto whatever this returns, so on the Daily route it has to be the params
    # class that declares them. The generic one rejects the assignment.
    "daily": lambda: TransportParams(audio_in_enabled=True, audio_out_enabled=True),

}



def build_stt():
    return DeepgramSTTService(
        api_key=os.environ["DEEPGRAM_API_KEY"],
        settings=DeepgramSTTService.Settings(
            model="nova-3",
        ),
    )


async def _run_bot(transport: BaseTransport, runner_args: RunnerArguments, dev) -> None:
    require_env()
    call_context = {}
    # One SLNG Context Router session id per call, passed as an argument from here
    # on. It groups this call's think requests for support and scopes nothing:
    # the agent id is what scopes the router's cache, so this may differ freely
    # between calls. Not named session_id, because runner_args.session_id is a
    # different thing this file also reads.
    slng_session_id = str(uuid.uuid4())

    runner = WorkerRunner(handle_sigint=runner_args.handle_sigint)

    # turn: local — end-of-turn detection runs on-device (Silero VAD). No API
    # key, no network hop; the turn binding in targets.yaml is advisory.
    context = LLMContext()
    state = build_state(call_context)
    agents = [BillingAgent(state=state, context=context, call_context=call_context, dev_metrics=dev, slng_session_id=slng_session_id), IntakeAgent(state=state, context=context, call_context=call_context, dev_metrics=dev, slng_session_id=slng_session_id)]
    user_aggregator, assistant_aggregator = LLMContextAggregatorPair(
        context,
        user_params=LLMUserAggregatorParams(
            # The FLOOR: how long silence has to last before speech counts as
            # stopped. Pipecat's own default, and it does not move with the pace.
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
                # A minimum word count replaces the default start pair on purpose:
                # gating turn start on words is the whole point, and leaving VAD
                # start in place would open the turn before the words arrive.
                start=[MinWordsUserTurnStartStrategy(min_words=2)],
                stop=[
                    TurnAnalyzerUserTurnStopStrategy(
                        turn_analyzer=LocalSmartTurnAnalyzerV3(
                            params=SmartTurnParams(stop_secs=1.6)
                        )
                    )
                ],

            ),
            user_idle_timeout=15,
        ),
    )
    bridge = BusBridgeProcessor(bus=runner.bus, worker_name=MAIN_NAME, name=f"{MAIN_NAME}::BusBridge")
    dev.observe_aggregators(user_aggregator, assistant_aggregator)
    pipeline = Pipeline(
        [
            transport.input(),
            dev.observe_stt(build_stt()),
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
        observers=dev.observers(),

        params=PipelineParams(enable_metrics=True, enable_usage_metrics=True),
    )


    @user_aggregator.event_handler("on_user_turn_idle")
    async def on_user_turn_idle(aggregator):
        await aggregator.push_frame(
            LLMMessagesAppendFrame(
                [{"role": "developer", "content": "The caller has gone quiet. Politely check if they are still there."}],
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
            _idle_end = asyncio.create_task(
                _end_after(main, 45 - 15)
            )

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
            "intake",
            args=LLMWorkerActivationArgs(run_llm=False),
        )
        await next(agent for agent in agents if agent.name == "intake").queue_frame(
            TTSSpeakFrame("Hi, you have reached Acme Support. How can I help you today?")
        )

        asyncio.create_task(_end_after(main, 1200))

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

    @main.rtvi.event_handler("on_client_ready")
    async def on_client_ready(rtvi):
        # Wait for both client media readiness and main's StartFrame before
        # activating tools or emitting the greeting (SPEC V2).
        await pipeline_started.wait()
        await activate_entry()

    @transport.event_handler("on_client_disconnected")
    async def on_client_disconnected(transport, client):
        await runner.cancel()

    await runner.add_workers(main)

    await runner.run()
    if worker_start_error is not None:
        raise worker_start_error



async def run_bot(transport: BaseTransport, runner_args: RunnerArguments) -> None:
    dev = install_dev_metrics(runner_args)
    try:
        await _run_bot(transport, runner_args, dev)
    except Exception:
        dev.finish(error=True)
        raise
    finally:
        dev.finish()


async def bot(runner_args: RunnerArguments) -> None:
    _configure_logging()

    transport = await create_transport(runner_args, transport_params)
    await run_bot(transport, runner_args)


if __name__ == "__main__":
    from pipecat.runner.run import main

    main()
