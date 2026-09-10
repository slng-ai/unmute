import asyncio
import json
import logging
import os
import uuid
import re
from dataclasses import dataclass
from typing import Annotated
from urllib.parse import quote
import httpx
from openai import AsyncOpenAI
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
    stt,
)
from livekit.agents.voice import MetricsCollectedEvent
from livekit.plugins import deepgram, elevenlabs, openai, silero

import dev_metrics
from dev_metrics import dev_llm_node, dev_say, install_dev_metrics


logger = logging.getLogger("safe-core-fixture")
logger.setLevel(logging.INFO)

load_dotenv()


# --- prompts ---------------------------------------------------------------

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

COLLECT_PROMPT = """Ask for the caller's email and confirm the account for {{customer_id}}.

When this step is complete, call `finish` with: tier.

`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish` with their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that status and takes the caller from there."""

CONFIRM_PROMPT = """Read the booking back and ask the caller to confirm.

When this step is complete, call `finish` with: confirmed.

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
    "DEEPGRAM_API_KEY",
    "ELEVEN_API_KEY",
    "GET_INVOICE_URL",
    "LIVEKIT_API_KEY",
    "LIVEKIT_API_SECRET",
    "LIVEKIT_URL",
    "LOOKUP_CUSTOMER_URL",
    "OPENAI_API_KEY",
    "SLNG_API_KEY",
]


def require_env() -> None:
    missing = [name for name in REQUIRED_ENV if not os.getenv(name)]
    if missing:
        raise RuntimeError("Missing required environment variables: " + ", ".join(missing))


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



def _slng_router_client() -> AsyncOpenAI:
    """The router client, built here so a response hook can read its headers.

    The plugin builds its own client when it is given none, and it exposes no
    hook, no raw response and no header callback. Passing one in is the only
    supported seam (livekit-plugins-openai llm.py, the `client` argument), so the
    provenance line above costs this function.

    Every value restates the plugin's own default at the pinned version
    (llm.py:161-176): retries off, and that exact httpx timeout and limit set.
    Restating them is the point. Anything different here would be a change to
    retry or connection behaviour that nobody asked for and nothing would report.

    Passing a client also means owning it: the plugin closes only a client it
    built itself (`_owns_client`), so the entrypoint closes this one on shutdown.
    """
    return AsyncOpenAI(
        api_key=os.environ["SLNG_API_KEY"],
        base_url="https://eu.context-router.slng.ai/v1",
        max_retries=0,
        http_client=httpx.AsyncClient(
            timeout=httpx.Timeout(connect=15.0, read=5.0, write=5.0, pool=5.0),
            follow_redirects=True,
            limits=httpx.Limits(
                max_connections=50,
                max_keepalive_connections=50,
                keepalive_expiry=120,
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
                "template variable %s is %d characters; truncating to %d for the router",
                name,
                len(text),
                _SLNG_VARIABLE_LIMIT,
            )
            text = text[:_SLNG_VARIABLE_LIMIT]
        values[name] = text
    return values


# --- templates ---------------------------------------------------------------
_TEMPLATE = re.compile(r"\{\{\s*([a-z_][a-z0-9_]*)\s*\}\}")


def _render(text: str, userdata, *, quote_values: bool = False, site: str = "") -> str:
    """Substitute each variable token from the session userdata (SCHEMA 4.4).
    Only substituted values are URL-encoded, never the surrounding literal."""
    if userdata is None:
        return text

    def _one(match: re.Match[str]) -> str:
        # Through _state_lookup, which walks a path emitted as one flat name, and
        # _state_text, so a declared value renders as compact JSON and an empty
        # one as words. Anything not declared structured comes back exactly as
        # str() gave it.
        value = _state_text(*_prompt_value(userdata, match.group(1), site))
        return quote(value, safe="") if quote_values else value

    return _TEMPLATE.sub(_one, text)


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


# --- shared state ------------------------------------------------------------
# Typed session state (SCHEMA 4.4): tasks assign into it, transfers read it.
@dataclass
class Userdata:
    caller_alias: str | None = None  # What the caller says to call them.
    customer_id: str | None = None
    verified: bool | None = False
    # One value per call, set where the call begins. It groups this call's think
    # requests for support and scopes nothing: the cache scope is the agent id
    # header, so this may differ freely between calls. It lives here rather than
    # in an entrypoint local because the header set now travels per request, so
    # every agent and task class has to be able to reach it from a method body.
    slng_session_id: str = ""
    # And the router client, for the same reason: the summarizer is built inside
    # an agent method, where the entrypoint's local is out of scope. One client
    # per call, so the pool and its response hook are the call's own.
    slng_client: AsyncOpenAI | None = None


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
    if "caller_alias" in values:
        value = values["caller_alias"]
        if not (isinstance(value, str)):
            raise RuntimeError("call_start.caller_alias must be string")
    if "customer_id" in values:
        value = values["customer_id"]
        if not (isinstance(value, str)):
            raise RuntimeError("call_start.customer_id must be string")
    if "verified" in values:
        value = values["verified"]
        if not (isinstance(value, bool)):
            raise RuntimeError("call_start.verified must be boolean")
    if missing:
        raise RuntimeError("Missing call_start fields: " + ", ".join(missing))
    return values


def _hydrate_call_start(userdata, values: dict) -> None:
    if "caller_alias" in values:
        userdata.caller_alias = values["caller_alias"]
    if "customer_id" in values:
        userdata.customer_id = values["customer_id"]
    if "verified" in values:
        userdata.verified = values["verified"]
    return None


# --- interruption shaping ----------------------------------------------------
IGNORE_PHRASES = ["okay", "right", "uh-huh"]


class IgnorePhrasesMixin:
    """interruption.ignore_phrases (generated): matching final transcripts are
    dropped before turn handling, so they neither interrupt nor reach the LLM."""

    def stt_node(self, audio, model_settings):
        async def _filtered():
            async for event in Agent.default.stt_node(self, audio, model_settings):
                if (
                    event.type == stt.SpeechEventType.FINAL_TRANSCRIPT
                    and event.alternatives
                    and event.alternatives[0].text.strip().lower().strip(" .,!?") in IGNORE_PHRASES
                ):
                    continue
                yield event

        return _filtered()


# --- router cache scope ------------------------------------------------------
async def _slng_llm_node(agent, chat_ctx, tools, model_settings):
    """Agent.default.llm_node, plus everything this request's own scope and
    values.

    One model object serves every agent and task in this session, so neither the
    scope nor the variable values can be a constructor value. The scope would be
    the same for all of them, which is the collision this prevents; the values
    would be the ones the call started with, which is never the ones that matter.

    Both dicts are written whole rather than by key, and both are written here
    rather than at construction, for one reason read out of the plugin: it copies
    the per-request extra_kwargs first and then overwrites extra_body and
    extra_headers from its own constructor options (livekit-plugins-openai
    llm.py:961-968, read at the pinned version). So a constructor value does not
    merely win, it wins in silence. That is why no router model here is built
    with either field.

    The body restates the framework's own default (livekit-agents 1.6.10,
    agents/voice/agent.py:524-545), because that default passes no per-request
    extras and ModelSettings carries only tool_choice, so there is no supported
    seam short of the node. The version pin is exact, floor equal to ceiling, so
    a framework bump is already a deliberate step; checking this against the new
    default is part of it.

    A module function rather than only a mixin method, because two things need
    it: the agent classes, through _SlngScoped below, and the task retry mixin,
    which overrides llm_node for its own reasons and calls the default itself. A
    second copy of this body is how one of those two would come to send the
    wrong scope.

    agent.session is read rather than stored: llm_node is only ever called by the
    running activity, so the session exists by the time this runs. That holds for
    an AgentTask too, which is an Agent.
    """
    session = agent.session
    # The activity resolves a per-class override against the session default the
    # same way (agent_activity.py:4628-4630). isinstance covers both the not-given and
    # the None case without reaching for a private helper.
    activity_llm = agent.llm if isinstance(agent.llm, llm.LLM) else session.llm
    tool_choice = model_settings.tool_choice if model_settings else NOT_GIVEN
    async with activity_llm.chat(
        chat_ctx=chat_ctx,
        tools=tools,
        tool_choice=tool_choice,
        conn_options=session.conn_options.llm_conn_options,
        extra_kwargs={
            "extra_headers": {
                "X-Slng-Agent-Id": agent._slng_scope,
                "X-Slng-Session-Id": session.userdata.slng_session_id,
            },
            # Rebuilt for every request, which is the point: a value this call
            # learns partway through reaches the model from the next turn on.
            "extra_body": {"reasoning_effort": "none", "slng_config": _slng_config_fast_reasoning(), "template_variables": _slng_template_variables(session.userdata, _SLNG_TEMPLATE_PATHS.get(agent._slng_scope, ()), scope=agent._slng_scope)},
        },
    ) as stream:
        async for chunk in stream:
            yield chunk


class _SlngScoped:
    """Route an agent class's think requests through its own cache scope.

    Agents only. A task carries _slng_scope as a plain class attribute and gets
    here through _RetryEmptyTaskResponseMixin instead, because that mixin
    overrides llm_node too and this one would shadow it.
    """

    _slng_scope: str

    def llm_node(self, chat_ctx, tools, model_settings):
        return dev_llm_node(self, _slng_llm_node, chat_ctx, tools, model_settings)


# --- agents ----------------------------------------------------------------

class Billing(_SlngScoped, IgnorePhrasesMixin, Agent):
    def tts_node(self, text, model_settings):
        return dev_metrics.dev_tts_node(self, text, model_settings)

    # This agent's own cache scope, sent by _SlngScoped on every request.
    _slng_scope = "safe-core-router-v3:billing"

    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(
            instructions=BILLING_PROMPT,
            chat_ctx=chat_ctx,
            tts=elevenlabs.TTS(api_key=os.environ["ELEVEN_API_KEY"], voice_id="EXAVITQu4vr4xnSDxMaL"),
        )
        if isinstance(chat_ctx, llm.ChatContext):
            self._chat_ctx = chat_ctx.copy()

    async def on_enter(self) -> None:
        # This agent took over via handoff; let its own instructions drive the
        # opening (the prompt already says not to re-greet).
        self.session.generate_reply()
    @function_tool
    async def get_invoice(self, ctx: RunContext, customer_id: Annotated[str, Field(description="The customer id from lookup_customer")]) -> dict:
        """Fetch the most recent invoice for a customer id. Returns the invoice total and status."""
        async with httpx.AsyncClient() as client:
            resp = await client.post(
                os.environ["GET_INVOICE_URL"],
                json={"customer_id": customer_id},
            )
            resp.raise_for_status()
            return resp.json()



class Intake(_SlngScoped, IgnorePhrasesMixin, Agent):
    def tts_node(self, text, model_settings):
        return dev_metrics.dev_tts_node(self, text, model_settings)

    # This agent's own cache scope, sent by _SlngScoped on every request.
    _slng_scope = "safe-core-router-v3:intake"

    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN, initial: bool = False) -> None:
        self._initial = initial
        super().__init__(
            instructions=INTAKE_PROMPT,
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
            self.session.generate_reply(tools=[t.id for t in self.tools if t.id not in {"to_billing"}])
            return
        await dev_say(self.session, "Hi, you have reached Acme Support. How can I help you today?")
    @function_tool
    async def lookup_customer(self, ctx: RunContext, email: Annotated[str, Field(description="Caller email address")], phone: Annotated[str, Field(description="Caller phone number in E.164 form")]) -> dict:
        """Look up a customer record by phone number or email. Returns the customer id and name."""
        async with httpx.AsyncClient() as client:
            resp = await client.post(
                os.environ["LOOKUP_CUSTOMER_URL"],
                json={"email": email, "phone": phone},
            )
            resp.raise_for_status()
            return resp.json()


    @function_tool
    async def to_billing(self, ctx: RunContext):
        """Caller asks about billing, an invoice, or a refund."""
        return Billing(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_config_update=True, exclude_handoff=True))

    @function_tool
    async def run_collect(self, ctx: RunContext) -> dict:
        """Collect the caller's account details. When this flow finishes it returns a status. Continue with the caller. Do not run this flow again for the same request. A completed status means the step finished. Read any saved values through your own prompt references. An unserved status means the step could not help. Ask the caller what they need, then use your tools or a handoff."""
        owner_ctx = self.chat_ctx.copy()
        try:
            result = await Collect(chat_ctx=owner_ctx.copy(exclude_instructions=True, exclude_config_update=True, exclude_handoff=True))
        finally:
            await self.update_chat_ctx(owner_ctx, exclude_invalid_function_calls=False)
        dev_metrics.dev_task_returned(ctx, result)
        return _task_status(result)

    @function_tool
    async def run_triage(self, ctx: RunContext) -> dict:
        """Run the triage group. When this flow finishes it returns a status. Continue with the caller. Do not run this flow again for the same request. A completed status means the step finished. Read any saved values through your own prompt references. An unserved status means the step could not help. Ask the caller what they need, then use your tools or a handoff."""
        owner_ctx = self.chat_ctx.copy()
        try:
            task_results = {}
            task_results["collect"] = await Collect(chat_ctx=llm.ChatContext())
            task_results["confirm"] = await Confirm(chat_ctx=llm.ChatContext())
        finally:
            await self.update_chat_ctx(owner_ctx, exclude_invalid_function_calls=False)
        dev_metrics.dev_task_returned(ctx, *task_results.values())
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

    def tts_node(self, text, model_settings):
        return dev_metrics.dev_tts_node(self, text, model_settings)

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
            # A router-bound task asks under its own cache scope, not its owner's.
            # Dispatched here because this mixin owns llm_node for every task, and
            # a package can mix a router-bound task with one that is not.
            node = (
                _slng_llm_node
                if getattr(self, "_slng_scope", None)
                else Agent.default.llm_node
            )
            async for chunk in dev_llm_node(
                self, node, request_chat_ctx, request_tools, model_settings
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


class Collect(_RetryEmptyTaskResponseMixin, IgnorePhrasesMixin, AgentTask[dict]):
    # This task's own cache scope. Read by _RetryEmptyTaskResponseMixin rather
    # than by a mixin of its own, because that mixin already owns llm_node here.
    _slng_scope = "safe-core-router-v3:task.collect"

    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(instructions=COLLECT_PROMPT, chat_ctx=chat_ctx)
        if isinstance(chat_ctx, llm.ChatContext):
            self._chat_ctx = chat_ctx.copy()
        self._response_tool_call_ids: set[str] = set()

    async def on_enter(self) -> None:
        await self.update_chat_ctx(self.chat_ctx.copy(exclude_instructions=True, exclude_config_update=True, exclude_handoff=True))
        # The task's own instructions describe this step; let them drive the opening.
        self.session.generate_reply()

    @function_tool
    async def lookup_customer(self, ctx: RunContext, email: Annotated[str, Field(description="Caller email address")], phone: Annotated[str, Field(description="Caller phone number in E.164 form")]) -> dict:
        """Look up a customer record by phone number or email. Returns the customer id and name."""
        async with httpx.AsyncClient() as client:
            resp = await client.post(
                os.environ["LOOKUP_CUSTOMER_URL"],
                json={"email": email, "phone": phone},
            )
            resp.raise_for_status()
            return resp.json()

    @function_tool
    async def finish(self, ctx: RunContext, tier: str | None = None, unserved_request: Annotated[str, Field(description="Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.")] = "") -> str | None:
        """Save this step's values and finish, or return unserved without saving."""
        if self.done():
            return
        try:
            _values = _save_result("collect", ctx.userdata, {"tier": tier, "unserved_request": unserved_request})
        except _StateRefused as refused:
            logger.warning("finish %s: %s", "collect", refused.message)
            return f"Not recorded: {refused.message}. Ask again, then call finish with a value that fits."
        dev_metrics.dev_task_finished(ctx, _values)
        self.complete(_values)

class Confirm(_RetryEmptyTaskResponseMixin, IgnorePhrasesMixin, AgentTask[dict]):
    # This task's own cache scope. Read by _RetryEmptyTaskResponseMixin rather
    # than by a mixin of its own, because that mixin already owns llm_node here.
    _slng_scope = "safe-core-router-v3:task.confirm"

    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(instructions=CONFIRM_PROMPT, chat_ctx=chat_ctx)
        if isinstance(chat_ctx, llm.ChatContext):
            self._chat_ctx = chat_ctx.copy()
        self._response_tool_call_ids: set[str] = set()

    async def on_enter(self) -> None:
        await self.update_chat_ctx(self.chat_ctx.copy(exclude_instructions=True, exclude_config_update=True, exclude_handoff=True))
        # The task's own instructions describe this step; let them drive the opening.
        self.session.generate_reply()

    @function_tool
    async def finish(self, ctx: RunContext, confirmed: bool | None = None, unserved_request: Annotated[str, Field(description="Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.")] = "") -> str | None:
        """Save this step's values and finish, or return unserved without saving."""
        if self.done():
            return
        try:
            _values = _save_result("confirm", ctx.userdata, {"confirmed": confirmed, "unserved_request": unserved_request})
        except _StateRefused as refused:
            logger.warning("finish %s: %s", "confirm", refused.message)
            return f"Not recorded: {refused.message}. Ask again, then call finish with a value that fits."
        dev_metrics.dev_task_finished(ctx, _values)
        self.complete(_values)


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


@server.rtc_session(agent_name="safe-core-fixture-livekit")
async def entrypoint(ctx: JobContext) -> None:
    require_env()
    # The call state is a local here as well as the session's user data, so the
    # router's template variable snapshot can read it beside the llm kwarg.
    #
    # It also carries one SLNG Context Router session id per call, created here
    # where the call begins, so one worker process serving several jobs keeps them
    # apart. It rides the state object rather than a local of its own because
    # every agent and task now builds its own header set at request time and has
    # to be able to reach this from a method body. Not named session_id, because
    # this file already writes a "session_id" into the telephony call context and
    # that is a different thing.
    slng_state = Userdata(slng_session_id=str(uuid.uuid4()))
    # This client is ours, so closing it is ours too: the plugin closes only a
    # client it built itself. Without this a worker leaks one connection pool per
    # call it serves.
    slng_client = _slng_router_client()
    slng_state.slng_client = slng_client

    async def _close_slng_client() -> None:
        await slng_client.close()

    ctx.add_shutdown_callback(_close_slng_client)
    session = AgentSession[Userdata](
        userdata=slng_state,
        stt=deepgram.STT(api_key=os.environ["DEEPGRAM_API_KEY"], model="nova-3"),
        llm=openai.LLM(client=slng_state.slng_client, model="gpt-5.6-luna"),
        tts=elevenlabs.TTS(api_key=os.environ["ELEVEN_API_KEY"], voice_id="cgSgspJ2msm6clMCkdW9"),
        turn_handling=TurnHandlingOptions(
            turn_detection=inference.TurnDetector(version="v1-mini"),
            # The CEILING, and the shortest wait the runtime will consider.
            # pace: balanced. Without this the streaming defaults apply and
            # max_delay is 2.5s, which no package could reach.
            endpointing={"mode": "dynamic", "min_delay": 0.3, "max_delay": 1.6},
            interruption={"enabled": True, "min_words": 2},
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
    await session.start(agent=Intake(initial=True), room=ctx.room)
    await ctx.connect()

    async def _max_duration() -> None:
        await asyncio.sleep(1200)
        session.shutdown()  # conversation.max_duration

    asyncio.create_task(_max_duration())


# No __main__ block: this module is started through livekit-agents' supported
# CLI, `python -m livekit.agents start agent.py`, which imports it and finds the
# `server` above. The older per-script entry point goes through a CLI upstream
# has deprecated and will remove.
