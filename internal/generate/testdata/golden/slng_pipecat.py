"""Generated Pipecat agent for safe-core-fixture.

Compiled by `unmute`; do not edit by hand. Prompts, model routes, and the agent
graph are baked in from the spec. Secret values are read from the environment
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
import uuid
from dataclasses import dataclass
from urllib.parse import quote

import httpx
from openai import AsyncOpenAI, DefaultAsyncHttpxClient
from dotenv import load_dotenv
from loguru import logger

from pipecat.audio.turn.smart_turn.base_smart_turn import SmartTurnParams
from pipecat.audio.turn.smart_turn.local_smart_turn_v3 import LocalSmartTurnAnalyzerV3
from pipecat.audio.vad.silero import SileroVADAnalyzer
from pipecat.audio.vad.vad_analyzer import VADParams
from pipecat.bus import BusBridgeProcessor
from pipecat.flows import ContextStrategy, ContextStrategyConfig, FlowManager, FlowsFunctionSchema, NodeConfig
from pipecat.frames.frames import EndFrame, FunctionCallResultProperties, LLMMessagesAppendFrame, LLMUpdateSettingsFrame, TTSSpeakFrame
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

from dev_metrics import dev_metrics_observer

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
    """The router's LLM service, plus a response hook on the client it builds.

    Overriding create_client is the only seam this framework offers for it: the
    service builds its own AsyncOpenAI and hands us neither the raw response nor
    its headers, and the router states where an answer came from only in headers.

    The connection limits restate the base class's own (pipecat
    services/openai/base_llm.py create_client at the pinned version). Restating
    them is deliberate. Anything different here would change connection reuse,
    which is a latency change nobody asked for and nothing would report.
    """

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
    if "customer_id" in call_start:
        setattr(state, "customer_id", call_start["customer_id"])
    if "verified" in call_start:
        setattr(state, "verified", call_start["verified"])
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
        value = "" if value is None else str(value)
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
        settings=OpenAILLMService.Settings(
            model="gpt-5.6-luna",
            system_instruction=BILLING_PROMPT,
            extra={"extra_body": {"reasoning_effort": "none", "slng_config": _slng_config_fast_reasoning(), "template_variables": _slng_template_variables(state, ("caller_alias", "customer_id"))}, "extra_headers": {"X-Slng-Agent-Id": "safe-core-router-v3:billing", "X-Slng-Session-Id": slng_session_id}},
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

    def __init__(self, state=None, context=None, call_context=None, *, slng_session_id) -> None:
        self.state = state
        self.context = context
        # Kept on self because a task's headers are swapped in from a method
        # body, where the constructor's parameter is out of scope, and a whole
        # header dict has to carry this along with the scope.
        self._slng_session_id = slng_session_id

        llm = build_billing_llm(state, slng_session_id=slng_session_id)
        super().__init__("billing", llm=llm, pipeline=Pipeline([llm, build_billing_tts()]), bridged=())


    @_direct_tool(cancel_on_interruption=False)
    async def update_variables(self, params: FunctionCallParams, caller_alias: str | None = None):
        """Save details the caller gives you, as soon as you learn them. caller_alias: What the caller says to call them.

        Args:
            caller_alias (str | None): What the caller says to call them.
        """
        saved = []
        if caller_alias is not None:
            self.state.caller_alias = caller_alias
            saved.append("caller_alias")
        if saved:
            # The third place this call writes a variable, and the one that made
            # the other two insufficient: a value the caller offers arrives here,
            # not at a task result. The router substitutes these into the prompt's
            # placeholders, so the value has to reach it before the next turn
            # asks. Body only, so the speaking site's cache scope stays put.
            await self.queue_frame(LLMUpdateSettingsFrame(
                delta=LLMSettings(extra={"extra_body": {"reasoning_effort": "none", "slng_config": _slng_config_fast_reasoning(), "template_variables": _slng_template_variables(self.state, ("caller_alias", "customer_id"))}}),
            ))
        await params.result_callback({"saved": saved})


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
        settings=OpenAILLMService.Settings(
            model="gpt-5.6-luna",
            system_instruction=INTAKE_PROMPT,
            extra={"extra_body": {"reasoning_effort": "none", "slng_config": _slng_config_fast_reasoning(), "template_variables": _slng_template_variables(state, ("caller_alias", "customer_id"))}, "extra_headers": {"X-Slng-Agent-Id": "safe-core-router-v3:intake", "X-Slng-Session-Id": slng_session_id}},
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

    def __init__(self, state=None, context=None, call_context=None, *, slng_session_id) -> None:
        self.state = state
        self.context = context
        # Kept on self because a task's headers are swapped in from a method
        # body, where the constructor's parameter is out of scope, and a whole
        # header dict has to carry this along with the scope.
        self._slng_session_id = slng_session_id

        llm = build_intake_llm(state, slng_session_id=slng_session_id)
        super().__init__("intake", llm=llm, pipeline=Pipeline([llm, build_intake_tts()]), bridged=())

    async def on_activated(self, args) -> None:
        # A Flow replaces this worker's system instruction with its task role.
        # Re-entry restores the owning agent before tools/messages run.
        # And its cache scope with it: a task that handed the conversation on
        # rather than returning left this service holding the task's scope, so
        # re-entry has to take it back or this agent would answer as the task.
        # Only the extra_headers key travels: a settings update merges the extra
        # dict key by key, so the inline configuration and the reasoning setting
        # survive untouched.
        await self.queue_frame(LLMUpdateSettingsFrame(
            delta=LLMSettings(
                system_instruction=INTAKE_PROMPT,
                extra={"extra_headers": {"X-Slng-Agent-Id": "safe-core-router-v3:intake", "X-Slng-Session-Id": self._slng_session_id}},
            ),
        ))
        await super().on_activated(args)


    @_direct_tool(cancel_on_interruption=False)
    async def update_variables(self, params: FunctionCallParams, caller_alias: str | None = None):
        """Save details the caller gives you, as soon as you learn them. caller_alias: What the caller says to call them.

        Args:
            caller_alias (str | None): What the caller says to call them.
        """
        saved = []
        if caller_alias is not None:
            self.state.caller_alias = caller_alias
            saved.append("caller_alias")
        if saved:
            # The third place this call writes a variable, and the one that made
            # the other two insufficient: a value the caller offers arrives here,
            # not at a task result. The router substitutes these into the prompt's
            # placeholders, so the value has to reach it before the next turn
            # asks. Body only, so the speaking site's cache scope stays put.
            await self.queue_frame(LLMUpdateSettingsFrame(
                delta=LLMSettings(extra={"extra_body": {"reasoning_effort": "none", "slng_config": _slng_config_fast_reasoning(), "template_variables": _slng_template_variables(self.state, ("caller_alias", "customer_id"))}}),
            ))
        await params.result_callback({"saved": saved})


    @_direct_tool(cancel_on_interruption=False)
    async def to_billing(self, params: FunctionCallParams):
        """Caller asks about billing, an invoice, or a refund."""
        await self.activate_worker(
            "billing",
            args=LLMWorkerActivationArgs(
                messages=[{"role": "developer", "content": "Caller asks about billing, an invoice, or a refund."}],
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
        self._run_collect_results = {}
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
        await flow.initialize(self._run_collect_node_collect())

    def _run_collect_node_collect(self) -> NodeConfig:
        return NodeConfig(
            name="collect",
            role_message="Ask for the caller's email and confirm the account for {{customer_id}}.\n\nWhen this step is complete, call `finish_run_collect_collect` with: tier.\n\n`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish_run_collect_collect` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there.",
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
                    properties={"tier": {"type": "string"}, "unserved_request": {"description": "Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.", "type": "string"}},
                    required=["tier"],
                    handler=self._run_collect_finish_collect,
                ),
            ],
        )

    async def _run_collect_finish_collect(self, args, flow_manager):
        self._run_collect_results["collect"] = dict(args)
        # then: return — restore the owner's pre-flow context (messages and
        # tools); only the typed results cross back (merge: results, N13).
        messages, tools = self._run_collect_snapshot
        await self.queue_frame(LLMUpdateSettingsFrame(
            delta=LLMSettings(system_instruction=INTAKE_PROMPT,
                # The owner's cache scope goes back with its prompt. A leaked
                # task scope is the same defect pointing the other way.
                extra={"extra_headers": {"X-Slng-Agent-Id": "safe-core-router-v3:intake", "X-Slng-Session-Id": self._slng_session_id}}),
        ))
        await self.flush_pipeline()
        self.context.set_messages(messages + [{
            "role": "developer",
            "content": "Task results: " + json.dumps(self._run_collect_results) + " Continue with the caller in one short line. A result carrying `unserved_request` means a step could not serve that request and handed it back. The caller is still owed it: after one short line about the result, act on that request in the same turn with your own tools or a handoff. Never end the turn without it and never tell the caller you cannot.",
        }])
        self.context.set_tools(tools)
        return {"status": "ok"}, None

    @_direct_tool
    async def run_triage(self, params: FunctionCallParams):
        """Run the triage group."""
        self._run_triage_results = {}
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
        await flow.initialize(self._run_triage_node_collect())

    def _run_triage_node_collect(self) -> NodeConfig:
        return NodeConfig(
            name="collect",
            role_message="Ask for the caller's email and confirm the account for {{customer_id}}.\n\nWhen this step is complete, call `finish_run_triage_collect` with: tier.\n\n`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish_run_triage_collect` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there.",
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
                    properties={"tier": {"type": "string"}, "unserved_request": {"description": "Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.", "type": "string"}},
                    required=["tier"],
                    handler=self._run_triage_finish_collect,
                ),
            ],
            context_strategy=ContextStrategyConfig(strategy=ContextStrategy.RESET),
        )

    async def _run_triage_finish_collect(self, args, flow_manager):
        self._run_triage_results["collect"] = dict(args)
        # The next step is a different prompt site, so it asks under a different
        # cache scope. Queued before the node is handed back, for the same reason
        # the first step's was.
        await self.queue_frame(LLMUpdateSettingsFrame(
            delta=LLMSettings(extra={"extra_headers": {"X-Slng-Agent-Id": "safe-core-router-v3:task.confirm", "X-Slng-Session-Id": self._slng_session_id}}),
        ))
        return {"status": "ok", "result": self._run_triage_results["collect"]}, self._run_triage_node_confirm()

    def _run_triage_node_confirm(self) -> NodeConfig:
        return NodeConfig(
            name="confirm",
            role_message="Read the booking back and ask the caller to confirm.\n\nWhen this step is complete, call `finish_run_triage_confirm` with: confirmed.\n\n`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish_run_triage_confirm` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there.",
            task_messages=[{"role": "developer", "content": "Begin this step."}],
            functions=[
                FlowsFunctionSchema(
                    name="finish_run_triage_confirm",
                    description="Record the result of this step and finish.",
                    properties={"confirmed": {"type": "boolean"}, "unserved_request": {"description": "Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.", "type": "string"}},
                    required=["confirmed"],
                    handler=self._run_triage_finish_confirm,
                ),
            ],
            context_strategy=ContextStrategyConfig(strategy=ContextStrategy.RESET),
        )

    async def _run_triage_finish_confirm(self, args, flow_manager):
        self._run_triage_results["confirm"] = dict(args)
        # then: return — restore the owner's pre-flow context (messages and
        # tools); only the typed results cross back (merge: results, N13).
        messages, tools = self._run_triage_snapshot
        await self.queue_frame(LLMUpdateSettingsFrame(
            delta=LLMSettings(system_instruction=INTAKE_PROMPT,
                # The owner's cache scope goes back with its prompt. A leaked
                # task scope is the same defect pointing the other way.
                extra={"extra_headers": {"X-Slng-Agent-Id": "safe-core-router-v3:intake", "X-Slng-Session-Id": self._slng_session_id}}),
        ))
        await self.flush_pipeline()
        self.context.set_messages(messages + [{
            "role": "developer",
            "content": "Task results: " + json.dumps(self._run_triage_results) + " Continue with the caller in one short line. A result carrying `unserved_request` means a step could not serve that request and handed it back. The caller is still owed it: after one short line about the result, act on that request in the same turn with your own tools or a handoff. Never end the turn without it and never tell the caller you cannot.",
        }])
        self.context.set_tools(tools)
        return {"status": "ok"}, None


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


async def run_bot(transport: BaseTransport, runner_args: RunnerArguments) -> None:
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
    agents = [BillingAgent(state=state, context=context, call_context=call_context, slng_session_id=slng_session_id), IntakeAgent(state=state, context=context, call_context=call_context, slng_session_id=slng_session_id)]
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



async def bot(runner_args: RunnerArguments) -> None:
    _configure_logging()

    transport = await create_transport(runner_args, transport_params)
    await run_bot(transport, runner_args)


if __name__ == "__main__":
    from pipecat.runner.run import main

    main()
