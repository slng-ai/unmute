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
import sys
from dataclasses import dataclass

import httpx
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
    "LANGFUSE_BASE_URL",
    "LANGFUSE_PUBLIC_KEY",
    "LANGFUSE_SECRET_KEY",
    "LOOKUP_CUSTOMER_URL",
    "OPENAI_API_KEY",
    "SLNG_API_KEY",
]
IGNORE_PHRASES = ["okay", "right", "uh-huh"]


def require_env() -> None:
    missing = [name for name in REQUIRED_ENV if not os.getenv(name)]
    if missing:
        raise RuntimeError(f"Missing required environment variables: {', '.join(missing)}")


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
"""
INTAKE_PROMPT = """# Intake agent (placeholder prompt)

You are the front desk voice agent for Acme Support. This is a phone call, so keep every answer to one or two short sentences.

- Greet the caller and find out what they need.
- When they give a phone number or email, use `lookup_customer` to find their record.
- If the caller asks about billing, an invoice, or a refund, hand off to the billing agent with `to_billing`.
- Never guess account details. If you cannot find the customer, say so and ask again.
"""


# --- agents -----------------------------------------------------------------



def build_billing_llm(state=None):
    return OpenAILLMService(
        api_key=os.environ["OPENAI_API_KEY"],
        settings=OpenAILLMService.Settings(
            model="gpt-4o",
            system_instruction=BILLING_PROMPT,
        ),
    )


def build_billing_tts():
    return SlngTTSService(
        api_key=os.environ["SLNG_API_KEY"],
        voice="aura-2-orion-en",
        model="slng/deepgram/aura:2-en",
    )


class BillingAgent(TracedLLMWorker):
    """Agent: billing."""

    def __init__(self, state=None, context=None, call_context=None) -> None:
        self.state = state
        self.context = context

        llm = build_billing_llm(state)
        super().__init__("billing", llm=llm, pipeline=Pipeline([llm, build_billing_tts()]), bridged=())



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



def build_intake_llm(state=None):
    return OpenAILLMService(
        api_key=os.environ["OPENAI_API_KEY"],
        settings=OpenAILLMService.Settings(
            model="gpt-4o-mini",
            system_instruction=INTAKE_PROMPT,
            temperature=0.4,
        ),
    )


def build_intake_tts():
    return SlngTTSService(
        api_key=os.environ["SLNG_API_KEY"],
        voice="aura-2-thalia-en",
        model="slng/deepgram/aura:2-en",
    )


class IntakeAgent(TracedLLMWorker):
    """Agent: intake."""

    def __init__(self, state=None, context=None, call_context=None) -> None:
        self.state = state
        self.context = context

        llm = build_intake_llm(state)
        super().__init__("intake", llm=llm, pipeline=Pipeline([llm, build_intake_tts()]), bridged=())

    async def on_activated(self, args) -> None:
        # A Flow replaces this worker's system instruction with its task role.
        # Re-entry restores the owning agent before tools/messages run.
        await self.queue_frame(LLMUpdateSettingsFrame(
            delta=LLMSettings(system_instruction=INTAKE_PROMPT),
        ))
        await super().on_activated(args)



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
        await flow.initialize(self._run_collect_node_collect())

    def _run_collect_node_collect(self) -> NodeConfig:
        return NodeConfig(
            name="collect",
            role_message="Ask for the caller's email, look them up, and confirm their account tier.\n\nWhen this step is complete, call `finish_run_collect_collect` with: tier, verified_flag.\n\n`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish_run_collect_collect` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there.",
            task_messages=[{"role": "developer", "content": "Begin this step."}],
            functions=[
                FlowsFunctionSchema(
                    name="lookup_customer",
                    description="Look up a customer record by phone number or email. Returns the customer id and name.",
                    properties={"email": {"description": "Caller email address", "type": "string"}, "phone": {"description": "Caller phone number in E.164 form", "type": "string"}},
                    required=[],
                    handler=self._trace_flow_tool("lookup_customer", _flow_tool_lookup_customer),
                ),
                FlowsFunctionSchema(
                    name="finish_run_collect_collect",
                    description="Record the result of this step and finish.",
                    properties={"tier": {"enum": ["free", "pro"], "type": "string"}, "unserved_request": {"description": "Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.", "type": "string"}, "verified_flag": {"type": "boolean"}},
                    required=["tier", "verified_flag"],
                    handler=self._trace_flow_tool("finish_run_collect_collect", self._run_collect_finish_collect),
                ),
            ],
        )

    async def _run_collect_finish_collect(self, args, flow_manager):
        self._run_collect_results["collect"] = dict(args)
        self.state.verified = self._run_collect_results["collect"]["verified_flag"]
        # then: return — restore the owner's pre-flow context (messages and
        # tools); only the typed results cross back (merge: results, N13).
        messages, tools = self._run_collect_snapshot
        await self.queue_frame(LLMUpdateSettingsFrame(
            delta=LLMSettings(system_instruction=INTAKE_PROMPT),
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
        await flow.initialize(self._run_triage_node_collect())

    def _run_triage_node_collect(self) -> NodeConfig:
        return NodeConfig(
            name="collect",
            role_message="Ask for the caller's email, look them up, and confirm their account tier.\n\nWhen this step is complete, call `finish_run_triage_collect` with: tier, verified_flag.\n\n`unserved_request` is for a request this step cannot serve. Do this step's own work first, and never use it to skip that work: the caller's original reason for being here is not an unserved request. If a handoff here covers what they want, call that handoff instead. Only when no tool and no handoff here can serve what the caller is asking, call `finish_run_triage_collect` with the closest result you have and their request in `unserved_request`, in their own words, rather than refusing or explaining what you cannot do here. The agent that owns this step reads that field and takes the caller from there.",
            task_messages=[{"role": "developer", "content": "Begin this step."}],
            functions=[
                FlowsFunctionSchema(
                    name="lookup_customer",
                    description="Look up a customer record by phone number or email. Returns the customer id and name.",
                    properties={"email": {"description": "Caller email address", "type": "string"}, "phone": {"description": "Caller phone number in E.164 form", "type": "string"}},
                    required=[],
                    handler=self._trace_flow_tool("lookup_customer", _flow_tool_lookup_customer),
                ),
                FlowsFunctionSchema(
                    name="finish_run_triage_collect",
                    description="Record the result of this step and finish.",
                    properties={"tier": {"enum": ["free", "pro"], "type": "string"}, "unserved_request": {"description": "Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it.", "type": "string"}, "verified_flag": {"type": "boolean"}},
                    required=["tier", "verified_flag"],
                    handler=self._trace_flow_tool("finish_run_triage_collect", self._run_triage_finish_collect),
                ),
            ],
            context_strategy=ContextStrategyConfig(strategy=ContextStrategy.RESET),
        )

    async def _run_triage_finish_collect(self, args, flow_manager):
        self._run_triage_results["collect"] = dict(args)
        # then: return — restore the owner's pre-flow context (messages and
        # tools); only the typed results cross back (merge: results, N13).
        messages, tools = self._run_triage_snapshot
        await self.queue_frame(LLMUpdateSettingsFrame(
            delta=LLMSettings(system_instruction=INTAKE_PROMPT),
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

    trace_provider = setup_langfuse_tracing()
    trace_attributes = {"langfuse.trace.name": TRACE_NAME}
    if runner_args.session_id is not None:
        trace_attributes["langfuse.session.id"] = runner_args.session_id

    runner = WorkerRunner(handle_sigint=runner_args.handle_sigint)

    # turn: local — end-of-turn detection runs on-device (Silero VAD). No API
    # key, no network hop; the turn binding in targets.yaml is advisory.
    context = LLMContext()
    state = build_state(call_context)
    agents = [BillingAgent(state=state, context=context, call_context=call_context), IntakeAgent(state=state, context=context, call_context=call_context)]
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

        conversation_id=runner_args.session_id,
        enable_tracing=True,
        additional_span_attributes=trace_attributes,

        params=PipelineParams(enable_metrics=True, enable_usage_metrics=True),
    )

    enable_agent_tracing(main, agents)


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
