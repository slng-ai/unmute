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
    stt,
)
from livekit.agents.voice import MetricsCollectedEvent
from livekit.plugins import deepgram, elevenlabs, openai, silero

from dev_metrics import install_dev_metrics


logger = logging.getLogger("livekit")
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

`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there."""

CONFIRM_PROMPT = """Read the booking back and ask the caller to confirm.

When this step is complete, call `finish` with: confirmed.

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


def _slng_template_variables(state, names) -> dict:
    """The values the router substitutes into the prompt's placeholders.

    A name with no value yet sends the empty string, never None and never the text
    "None". A value over the router's limit is truncated with a warning rather
    than dropped: an over-long value must not end a live call.
    """
    values = {}
    for name in names:
        value = getattr(state, name, None) if state is not None else None
        text = "" if value is None else str(value)
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


def _render(text: str, userdata, *, quote_values: bool = False) -> str:
    """Substitute each variable token from the session userdata (SCHEMA 4.4).
    Only substituted values are URL-encoded, never the surrounding literal."""
    if userdata is None:
        return text

    def _one(match: re.Match[str]) -> str:
        value = getattr(userdata, match.group(1), None)
        value = "" if value is None else str(value)
        return quote(value, safe="") if quote_values else value

    return _TEMPLATE.sub(_one, text)


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
    llm.py:956-962, read at the pinned version). So a constructor value does not
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
    # same way (agent_activity.py:4627). isinstance covers both the not-given and
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
            "extra_body": {"slng_config": _slng_config_fast_reasoning(), "template_variables": _slng_template_variables(session.userdata, ("caller_alias", "customer_id"))},
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
        return _slng_llm_node(self, chat_ctx, tools, model_settings)


# --- agents ----------------------------------------------------------------

class Billing(_SlngScoped, IgnorePhrasesMixin, Agent):
    # This agent's own cache scope, sent by _SlngScoped on every request.
    _slng_scope = "safe-core-router-v3:billing"

    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(
            instructions=BILLING_PROMPT,
            chat_ctx=chat_ctx,
            tts=elevenlabs.TTS(api_key=os.environ["ELEVEN_API_KEY"], voice_id="EXAVITQu4vr4xnSDxMaL"),
        )

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

    @function_tool
    async def update_variables(self, ctx: RunContext, caller_alias: str | None = None) -> str:
        """Save details the caller gives you, as soon as you learn them. caller_alias: What the caller says to call them."""
        saved = []
        if caller_alias is not None:
            ctx.userdata.caller_alias = caller_alias
            saved.append("caller_alias")
        return "Saved: " + ", ".join(saved) if saved else "Nothing new to save."



class Intake(_SlngScoped, IgnorePhrasesMixin, Agent):
    # This agent's own cache scope, sent by _SlngScoped on every request.
    _slng_scope = "safe-core-router-v3:intake"

    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN, initial: bool = False) -> None:
        self._initial = initial
        super().__init__(
            instructions=INTAKE_PROMPT,
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
            self.session.generate_reply(tools=[t.id for t in self.tools if t.id not in {"to_billing"}])
            return
        await self.session.say("Hi, you have reached Acme Support. How can I help you today?")
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
    async def update_variables(self, ctx: RunContext, caller_alias: str | None = None) -> str:
        """Save details the caller gives you, as soon as you learn them. caller_alias: What the caller says to call them."""
        saved = []
        if caller_alias is not None:
            ctx.userdata.caller_alias = caller_alias
            saved.append("caller_alias")
        return "Saved: " + ", ".join(saved) if saved else "Nothing new to save."


    @function_tool
    async def to_billing(self, ctx: RunContext):
        """Caller asks about billing, an invoice, or a refund."""
        return Billing(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True))

    @function_tool
    async def run_collect(self, ctx: RunContext) -> dict:
        """Collect the caller's account details. When this flow finishes it returns its result to you. That result is the final outcome for this request: relay it to the caller and continue. Do not run this flow again for the same request. A result carrying `unserved_request` means a step could not serve that request and handed it back. The caller is still owed it: after one short line about the result, act on that request in the same turn with your own tools or a handoff. Never end the turn without it and never tell the caller you cannot."""
        # N13: snapshot before the task, restore after. An awaited AgentTask
        # merges its own turns into this agent's context when it returns
        # (livekit/agents/voice/agent.py, merge on handoff-return), so without
        # this the follow-up prompt ends on the task's last assistant line with
        # no tool record of the work. That reads as unfinished, and the model
        # runs the same flow again (B: multi-task delegated twice and booked
        # nothing, 2026-08-15). The task-group branch below always did this.
        owner_ctx = self.chat_ctx.copy()
        result = await Collect(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True))
        await self.update_chat_ctx(owner_ctx)
        # C4/N13: the task's turns are not propagated back; the typed result is
        # the only return.
        return result

    @function_tool
    async def run_triage(self, ctx: RunContext) -> dict:
        """Run the triage group. When this flow finishes it returns its result to you. That result is the final outcome for this request: relay it to the caller and continue. Do not run this flow again for the same request. A result carrying `unserved_request` means a step could not serve that request and handed it back. The caller is still owed it: after one short line about the result, act on that request in the same turn with your own tools or a handoff. Never end the turn without it and never tell the caller you cannot."""
        # context_scope: isolated — each step is a standalone AgentTask starting
        # fresh (C3/C4); the grouped form always shares context, so it is not used.
        # Starting fresh says nothing about coming back: an AgentTask merges its
        # own turns into this agent on return whatever context it began with, so
        # the owner is snapshotted here and restored below, exactly as the
        # single-task and grouped branches do.
        owner_ctx = self.chat_ctx.copy()
        task_results: dict = {}
        task_results["collect"] = await Collect()
        task_results["confirm"] = await Confirm()
        # N13: standalone tasks never merge their turns back; hand the owner the
        # typed results only (merge: results).
        await self.update_chat_ctx(owner_ctx)
        return task_results


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
            # A router-bound task asks under its own cache scope, not its owner's.
            # Dispatched here because this mixin owns llm_node for every task, and
            # a package can mix a router-bound task with one that is not.
            node = (
                _slng_llm_node
                if getattr(self, "_slng_scope", None)
                else Agent.default.llm_node
            )
            async for chunk in node(
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
                        "response now; call finish only if the task is complete."
                        if attempt == 0
                        else "The finish retry was also empty. This is the second "
                        "retry. Use the existing tool result without repeating any "
                        "operation. Produce the task's next valid response now; call "
                        "finish only if the task is complete."
                    )
                else:
                    recovery = (
                        "The previous response was empty. Follow the current task "
                        "instructions and produce its next valid response now."
                        if attempt == 0
                        else "The prior retry was also empty. This is the second "
                        "retry. Follow the current task instructions and produce "
                        "its next non-empty valid response now."
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
        self._response_tool_call_ids: set[str] = set()

    async def on_enter(self) -> None:
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
    async def update_variables(self, ctx: RunContext, caller_alias: str | None = None) -> str:
        """Save details the caller gives you, as soon as you learn them. caller_alias: What the caller says to call them."""
        saved = []
        if caller_alias is not None:
            ctx.userdata.caller_alias = caller_alias
            saved.append("caller_alias")
        return "Saved: " + ", ".join(saved) if saved else "Nothing new to save."

    @function_tool
    async def finish(self, ctx: RunContext, tier: str, unserved_request: Annotated[str, Field(description="Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.")] = "") -> None:
        """Record the result of this step and finish. complete() is the sole
        resolution; do not relay anything after it."""
        self.complete(_task_result({"tier": tier}, unserved_request))

class Confirm(_RetryEmptyTaskResponseMixin, IgnorePhrasesMixin, AgentTask[dict]):
    # This task's own cache scope. Read by _RetryEmptyTaskResponseMixin rather
    # than by a mixin of its own, because that mixin already owns llm_node here.
    _slng_scope = "safe-core-router-v3:task.confirm"

    def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:
        super().__init__(instructions=CONFIRM_PROMPT, chat_ctx=chat_ctx)
        self._response_tool_call_ids: set[str] = set()

    async def on_enter(self) -> None:
        # The task's own instructions describe this step; let them drive the opening.
        self.session.generate_reply()

    @function_tool
    async def update_variables(self, ctx: RunContext, caller_alias: str | None = None) -> str:
        """Save details the caller gives you, as soon as you learn them. caller_alias: What the caller says to call them."""
        saved = []
        if caller_alias is not None:
            ctx.userdata.caller_alias = caller_alias
            saved.append("caller_alias")
        return "Saved: " + ", ".join(saved) if saved else "Nothing new to save."

    @function_tool
    async def finish(self, ctx: RunContext, confirmed: bool, unserved_request: Annotated[str, Field(description="Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.")] = "") -> None:
        """Record the result of this step and finish. complete() is the sole
        resolution; do not relay anything after it."""
        self.complete(_task_result({"confirmed": confirmed}, unserved_request))


# --- session ---------------------------------------------------------------
def prewarm(proc: JobProcess) -> None:
    proc.userdata["vad"] = silero.VAD.load()
server = AgentServer()
server.setup_fnc = prewarm


@server.rtc_session(agent_name="livekit")
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
    slng_state.slng_client = _slng_router_client()

    async def _close_slng_client() -> None:
        await slng_state.slng_client.close()

    ctx.add_shutdown_callback(_close_slng_client)
    session = AgentSession[Userdata](
        userdata=slng_state,
        stt=deepgram.STT(api_key=os.environ["DEEPGRAM_API_KEY"], model="nova-3"),
        llm=openai.LLM(client=slng_state.slng_client, model="gpt-5.6-luna", reasoning_effort="none"),
        tts=elevenlabs.TTS(api_key=os.environ["ELEVEN_API_KEY"], voice_id="cgSgspJ2msm6clMCkdW9"),
        turn_handling=TurnHandlingOptions(
            turn_detection=inference.TurnDetector(version="v1-mini"),
            interruption={"enabled": True, "min_words": 2},
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
