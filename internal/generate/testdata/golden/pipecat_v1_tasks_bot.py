"""Generated Pipecat agent for pipecat.

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
import json
import os
from dataclasses import dataclass

import httpx
from dotenv import load_dotenv
from loguru import logger

from pipecat.audio.vad.silero import SileroVADAnalyzer
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
from pipecat.transports.daily.transport import DailyParams, DailyTransport
from pipecat.turns.user_start import MinWordsUserTurnStartStrategy
from pipecat.turns.user_stop import SpeechTimeoutUserTurnStopStrategy
from pipecat.turns.user_turn_strategies import UserTurnStrategies
from pipecat.workers.llm import LLMWorkerActivationArgs, tool
from pipecat.workers.runner import WorkerRunner

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

MAIN_NAME = "main"
# Provider credentials only. The telephony route's environment (Redis, carrier
# keys, the public URL) is required by telephony.py, not here, so a telephony
# package still runs in the browser with nothing but model keys (V10/B3).
REQUIRED_ENV = [
    "DAILY_API_KEY",
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

# Set once the transport exists, so an agent's cold-transfer @tool can reach it.
_TRANSPORT: BaseTransport | None = None
# One call gets one transfer attempt. Holds the first attempt's result so a
# caller who asks twice replays it instead of firing a second REFER, and so a
# transfer that failed can never come back to the model as a success.
#
# ponytail: a module-level value, not the shared control store the carrier
# routes use for this. That store is deliberately absent here: this route
# declares no Redis, and adding one to remember a fact that never outlives the
# call would be an idle service. On this route one process serves one call, so
# module scope is call scope. If that ever stops being true, this has to become
# per-session state keyed on the call id.
_TRANSFER_RESULT: dict | None = None


def require_env() -> None:
    missing = [name for name in REQUIRED_ENV if not os.getenv(name)]
    if missing:
        raise RuntimeError(f"Missing required environment variables: {', '.join(missing)}")



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
- If the caller is not satisfied or asks for a person, transfer them with `to_human`.
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


    @tool
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

    @tool
    async def to_human(self, params: FunctionCallParams):
        """Transfer the caller to a human."""
        logger.info("human transfer fired: to_human (cold)")
        global _TRANSFER_RESULT
        if _TRANSFER_RESULT is not None:
            # Asked twice. Replay the first answer rather than dial again.
            await params.result_callback(_TRANSFER_RESULT)
            return
        # Claimed before the primitive runs, not after: a request arriving while
        # the REFER is still in flight must not slip past the guard.
        _TRANSFER_RESULT = {"in_progress": "A transfer is already under way; do not start another."}
        # Daily SIP transfers via REFER, which keeps the bot on the call
        # until the transfer completes, so the bot speaks the announcement
        # itself (the official daily-pstn-cold-transfer pattern).
        await params.llm.push_frame(
            LLMMessagesAppendFrame(
                [{"role": "developer", "content": "Tell the caller you are transferring them to a colleague now and to please hold, then wait."}],
                run_llm=True,
            )
        )
        if isinstance(_TRANSPORT, DailyTransport):
            error = await _TRANSPORT.sip_call_transfer({"toEndPoint": "+14155550123"})
        else:
            # No phone call, so nothing to hand over: the browser and console
            # transports carry no transfer primitive. Failing here by name beats
            # raising, and beats telling the model the caller was transferred.
            error = "this session is not a phone call, so it cannot be transferred"
        if error is not None:
            # A failed transfer is a return value, not an exception, on this
            # transport (T5). Ignoring it would tell the model the caller was
            # transferred while they are still right here. on_unavailable
            # decides what the caller gets instead.
            logger.warning("cold transfer failed: {}", error)
            _TRANSFER_RESULT = {"failed": f"The transfer could not be completed ({error}). Tell the caller and keep helping them."}
            await params.result_callback(_TRANSFER_RESULT)
            return
        _TRANSFER_RESULT = {"transferred": True}
        await params.result_callback(_TRANSFER_RESULT)



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


    @tool(cancel_on_interruption=False)
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

    @tool
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

    @tool
    async def run_collect(self, params: FunctionCallParams):
        """Collect the caller's account details."""
        self._run_collect_results = {}
        # Snapshot the owner's context; the flow rewrites it per node and the
        # final step restores it (merge: results, N13).
        self._run_collect_snapshot = ([dict(m) for m in self.context.get_messages()], self.context.tools)
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
        await flow.initialize(self._run_collect_node_collect())

    def _run_collect_node_collect(self) -> NodeConfig:
        return NodeConfig(
            name="collect",
            role_message="Ask for the caller's email, look them up, and confirm their account tier.",
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
                    properties={"tier": {"enum": ["free", "pro"], "type": "string"}, "verified_flag": {"type": "boolean"}},
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
            "content": "Task results: " + json.dumps(self._run_collect_results) + " Continue with the caller in one short line.",
        }])
        self.context.set_tools(tools)
        return {"status": "ok"}, None

    @tool
    async def run_triage(self, params: FunctionCallParams):
        """Run the triage group."""
        self._run_triage_results = {}
        # Snapshot the owner's context; the flow rewrites it per node and the
        # final step restores it (merge: results, N13).
        self._run_triage_snapshot = ([dict(m) for m in self.context.get_messages()], self.context.tools)
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
        await flow.initialize(self._run_triage_node_collect())

    def _run_triage_node_collect(self) -> NodeConfig:
        return NodeConfig(
            name="collect",
            role_message="Ask for the caller's email, look them up, and confirm their account tier.",
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
                    properties={"tier": {"enum": ["free", "pro"], "type": "string"}, "verified_flag": {"type": "boolean"}},
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
            "content": "Task results: " + json.dumps(self._run_triage_results) + " Continue with the caller in one short line.",
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
    "daily": lambda: DailyParams(audio_in_enabled=True, audio_out_enabled=True),

}



def build_stt():
    return DeepgramSTTService(
        api_key=os.environ["DEEPGRAM_API_KEY"],
        settings=DeepgramSTTService.Settings(
            model="nova-3",
        ),
    )


async def run_bot(transport: BaseTransport, runner_args: RunnerArguments, activate_on_start: bool = False) -> None:
    require_env()

    tracing_enabled = setup_langfuse_tracing()

    global _TRANSPORT
    _TRANSPORT = transport
    runner = WorkerRunner(handle_sigint=runner_args.handle_sigint)

    # turn: local — end-of-turn detection runs on-device (Silero VAD). No API
    # key, no network hop; the turn binding in targets.yaml is advisory.
    context = LLMContext()
    call_context = None
    state = build_state(call_context)
    agents = [BillingAgent(state=state, context=context, call_context=call_context), IntakeAgent(state=state, context=context, call_context=call_context)]
    user_aggregator, assistant_aggregator = LLMContextAggregatorPair(
        context,
        user_params=LLMUserAggregatorParams(
            vad_analyzer=SileroVADAnalyzer(),
            user_turn_strategies=UserTurnStrategies(
                start=[MinWordsUserTurnStartStrategy(min_words=2)],
                stop=[SpeechTimeoutUserTurnStopStrategy()],
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

        enable_tracing=tracing_enabled,
        additional_span_attributes={"langfuse.trace.name": TRACE_NAME},

        params=PipelineParams(enable_metrics=True, enable_usage_metrics=True),
    )

    if tracing_enabled:
        enable_agent_tracing(main, agents)


    @user_aggregator.event_handler("on_user_turn_idle")
    async def on_user_turn_idle(aggregator):
        await aggregator.push_frame(
            LLMMessagesAppendFrame(
                [{"role": "developer", "content": "The caller has gone quiet. Politely check if they are still there."}],
                run_llm=True,
            )
        )

    @transport.event_handler("on_dialout_answered")
    async def on_dialout_answered(transport, data):
        # Cold transfer: the human answered, so the bot leaves the call.
        await main.queue_frame(EndFrame())

    pipeline_started = asyncio.Event()
    entry_started = False

    async def activate_entry():
        nonlocal entry_started
        if entry_started:
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

    @main.event_handler("on_pipeline_started")
    async def on_pipeline_started(worker, frame):
        pipeline_started.set()
        if activate_on_start:
            # Local audio (console) has no client-connect event, so the
            # pipeline start is the session start. This fires after main's
            # StartFrame, so the B8/V14 activation gate is already satisfied.
            await activate_entry()

    if not activate_on_start:

        @transport.event_handler("on_client_connected")
        async def on_client_connected(transport, client):
            # Telephony transports (carrier WebSocket, Daily SIP) have no RTVI
            # client-ready handshake: the carrier opens a raw media WebSocket and
            # the bot initiates once that connection is open and main's StartFrame
            # has traversed (Pipecat Twilio dial-in flow; SPEC V2).
            await pipeline_started.wait()
            await activate_entry()


        @transport.event_handler("on_client_disconnected")
        async def on_client_disconnected(transport, client):
            await runner.cancel()

    await runner.add_workers(main, *agents)

    try:
        await runner.run()
    finally:
        if tracing_enabled:
            flush_tracing()



async def bot(runner_args: RunnerArguments) -> None:

    transport = await create_transport(runner_args, transport_params)
    await run_bot(transport, runner_args)


async def console_main() -> None:
    # Talk to the agent in the terminal over the local mic and speaker. The dev
    # runner has no local transport, so build one here. The import is lazy so a
    # default web run (`uv sync`) never needs pyaudio/portaudio (the `local`
    # extra installs them: `uv run --extra console bot.py console`).
    from pipecat.transports.local.audio import (
        LocalAudioTransport,
        LocalAudioTransportParams,
    )

    runner_args = RunnerArguments()
    runner_args.handle_sigint = True  # ctrl-c ends the session cleanly
    transport = LocalAudioTransport(
        LocalAudioTransportParams(audio_in_enabled=True, audio_out_enabled=True)
    )
    await run_bot(transport, runner_args, activate_on_start=True)


if __name__ == "__main__":
    import sys

    if "console" in sys.argv[1:]:
        asyncio.run(console_main())
    else:
        from pipecat.runner.run import main

        main()
