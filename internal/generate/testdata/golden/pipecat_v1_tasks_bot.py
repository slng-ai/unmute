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
import base64
import functools

import os
import json
from dataclasses import asdict, dataclass
from datetime import datetime, timezone

import httpx
from dotenv import load_dotenv

from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

from pipecat.audio.vad.silero import SileroVADAnalyzer
from pipecat.bus import BusBridgeProcessor
from pipecat.flows import ContextStrategy, ContextStrategyConfig, FlowManager, FlowsFunctionSchema, NodeConfig
from pipecat.frames.frames import EndFrame, LLMMessagesAppendFrame
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
from pipecat.transports.base_transport import BaseTransport, TransportParams
from pipecat.turns.user_start import MinWordsUserTurnStartStrategy
from pipecat.turns.user_stop import SpeechTimeoutUserTurnStopStrategy
from pipecat.turns.user_turn_strategies import UserTurnStrategies
from pipecat.workers.llm import LLMWorker, LLMWorkerActivationArgs, tool
from pipecat.workers.runner import WorkerRunner

from pipecat.utils.tracing.setup import setup_tracing

from pipecat.services.deepgram.stt import DeepgramSTTService
from pipecat.services.openai.llm import OpenAILLMService
from pipecat_slng import SlngTTSService

load_dotenv()

MAIN_NAME = "main"

TRACE_NAME = "intake" + "-" + "pipecat"

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

# Set once the shared context exists, so delegate flows can snapshot/restore it.
_CONTEXT: LLMContext | None = None


def require_env() -> None:
    missing = [name for name in REQUIRED_ENV if not os.getenv(name)]
    if missing:
        raise RuntimeError(f"Missing required environment variables: {', '.join(missing)}")



def _patch_pipecat_speech_tracing() -> None:
    """Expose Pipecat's speech payloads and metrics to Langfuse."""
    from pipecat.frames.frames import TTSAudioRawFrame
    from pipecat.services.tts_service import TTSService
    from pipecat.utils.tracing import service_decorators

    if getattr(service_decorators.add_stt_span_attributes, "__langfuse_patch__", False):
        return

    def patch(original, kind):
        def patched(span, *args, **kwargs):
            set_attribute = span.set_attribute

            # ponytail: Pipecat 1.5 emits speech data under generic keys that
            # Langfuse does not map. Remove when Pipecat emits Langfuse I/O.
            def mapped_set_attribute(key, value):
                set_attribute(key, value)
                encoded = json.dumps(value, default=str)
                if kind == "stt" and key == "transcript":
                    set_attribute("langfuse.observation.output", encoded)
                    set_attribute("langfuse.trace.input", encoded)
                elif kind == "tts" and key == "text":
                    set_attribute("langfuse.observation.input", encoded)
                    set_attribute("langfuse.trace.output", encoded)
                elif key == "metrics.ttfb":
                    set_attribute("langfuse.observation.metadata.ttfb_seconds", value)
                    set_attribute(
                        "langfuse.observation.completion_start_time",
                        datetime.now(timezone.utc).isoformat(),
                    )
                elif kind == "tts" and key == "metrics.character_count":
                    set_attribute("langfuse.observation.metadata.character_count", value)
                    set_attribute(
                        "langfuse.observation.usage_details",
                        json.dumps({"characters": value}),
                    )

            span.set_attribute = mapped_set_attribute
            original(span, *args, **kwargs)
            mapped_set_attribute(
                "langfuse.observation.input" if kind == "stt" else "langfuse.observation.output",
                json.dumps("audio"),
            )

        patched.__langfuse_patch__ = True
        return patched

    service_decorators.add_stt_span_attributes = patch(
        service_decorators.add_stt_span_attributes, "stt"
    )
    service_decorators.add_tts_span_attributes = patch(
        service_decorators.add_tts_span_attributes, "tts"
    )

    original_append_to_audio_context = TTSService.append_to_audio_context

    async def append_to_audio_context(self, context_id, frame):
        entry = getattr(self, "_tts_spans", {}).get(context_id)
        if isinstance(frame, TTSAudioRawFrame) and entry and not entry.get("ttfb_recorded"):
            started_at = getattr(getattr(self, "_metrics", None), "_start_ttfb_time", 0)
            # ponytail: Pipecat 1.5 clears some TTS contexts while pushing the
            # metric frame. Record TTFB at the actual first audio frame instead.
            if started_at:
                entry["span"].set_attribute(
                    "metrics.ttfb", datetime.now(timezone.utc).timestamp() - started_at
                )
                entry["ttfb_recorded"] = True
        return await original_append_to_audio_context(self, context_id, frame)

    append_to_audio_context.__langfuse_patch__ = True
    TTSService.append_to_audio_context = append_to_audio_context


def setup_langfuse_tracing() -> bool:
    public_key = os.getenv("LANGFUSE_PUBLIC_KEY")
    secret_key = os.getenv("LANGFUSE_SECRET_KEY")
    base_url = os.getenv("LANGFUSE_BASE_URL")
    values = (public_key, secret_key, base_url)
    if not all(values):
        raise ValueError(
            "LANGFUSE_PUBLIC_KEY, LANGFUSE_SECRET_KEY, and LANGFUSE_BASE_URL must all be set"
        )

    auth = base64.b64encode(f"{public_key}:{secret_key}".encode()).decode()
    os.environ["OTEL_EXPORTER_OTLP_ENDPOINT"] = f"{base_url.rstrip('/')}/api/public/otel"
    os.environ["OTEL_EXPORTER_OTLP_HEADERS"] = (
        f"Authorization=Basic%20{auth},x-langfuse-ingestion-version=4"
    )
    _patch_pipecat_speech_tracing()
    if not setup_tracing(service_name=TRACE_NAME, exporter=OTLPSpanExporter()):
        raise RuntimeError("Pipecat OpenTelemetry tracing is unavailable")
    return True


def _enable_agent_tracing(main: PipelineWorker, agents: list[LLMWorker]) -> None:
    # ponytail: Pipecat 1.5 LLMWorker exposes no tracing kwargs. Share the main
    # worker's context until its public constructor accepts them.
    for agent in agents:
        agent._enable_tracing = True
        agent._tracing_context = main._tracing_context


# ponytail: Pipecat 1.5 has no standard-tool tracing hook. Remove this
# subclass when ordinary LLMWorker tools gain native spans.
class _TracedLLMWorker(LLMWorker):
    """Add the tool spans missing from Pipecat's standard LLM tracing."""

    async def _trace_tool(self, name, arguments, call, tool_call_id=None):
        if not self._enable_tracing:
            return await call(None)
        parent = (
            self._tracing_context.get_turn_context()
            or self._tracing_context.get_conversation_context()
        )
        with trace.get_tracer("unmute.pipecat.tools").start_as_current_span(
            f"tool:{name}", context=parent
        ) as span:
            span.set_attribute("tool.function_name", name)
            span.set_attribute("langfuse.observation.input", json.dumps(arguments, default=str))
            if tool_call_id:
                span.set_attribute("tool.call_id", tool_call_id)
            try:
                result = await call(span)
            except Exception:
                span.set_attribute("tool.result_status", "error")
                raise
            if result is not None:
                span.set_attribute("langfuse.observation.output", json.dumps(result, default=str))
            span.set_attribute("tool.result_status", "completed")
            return result

    def _track_tool_call(self, method):
        tracked = super()._track_tool_call(method)

        @functools.wraps(tracked)
        async def wrapper(params, *args, **kwargs):
            async def call(span):
                if span:
                    result_callback = params.result_callback

                    async def traced_result_callback(result, **callback_kwargs):
                        span.set_attribute(
                            "langfuse.observation.output", json.dumps(result, default=str)
                        )
                        return await result_callback(result, **callback_kwargs)

                    params.result_callback = traced_result_callback
                return await tracked(params, *args, **kwargs)

            return await self._trace_tool(
                params.function_name,
                dict(params.arguments),
                call,
                tool_call_id=params.tool_call_id,
            )

        return wrapper

    def _trace_flow_tool(self, name, method):
        @functools.wraps(method)
        async def wrapper(args, flow_manager):
            async def call(_span):
                return await method(args, flow_manager)

            return await self._trace_tool(name, dict(args), call)

        return wrapper





@dataclass
class State:
    """Typed call variables (SCHEMA 4.4), shared across agents."""
    customer_id: str | None = None
    verified: bool = False


STATE = State()

async def _end_after(worker: PipelineWorker, timeout_secs: float) -> None:
    await asyncio.sleep(timeout_secs)
    await worker.queue_frame(EndFrame())

# --- agents -----------------------------------------------------------------


def build_billing_llm():
    return OpenAILLMService(
        api_key=os.environ["OPENAI_API_KEY"],
        settings=OpenAILLMService.Settings(
            model="gpt-4o",
            system_instruction="# Billing agent (placeholder prompt)\n\nYou are the billing specialist for Acme Support. This is a phone call, so keep every answer to one or two short sentences.\n\n- The caller was handed to you because they have a billing question. The conversation so far is in your context.\n- Use `get_invoice` to look up invoices for customer `{{customer_id}}`.\n- Explain charges calmly and clearly, one item at a time.\n- If the caller is not satisfied or asks for a person, transfer them with `to_human`.\n",
        ),
    )


def build_billing_tts():
    return SlngTTSService(
        api_key=os.environ["SLNG_API_KEY"],
        voice="aura-2-orion-en",
        model="slng/deepgram/aura:2-en",
        language="en",
    )


class BillingAgent(_TracedLLMWorker):
    """Agent: billing."""

    def __init__(self) -> None:
        llm = build_billing_llm()
        super().__init__("billing", llm=llm, pipeline=Pipeline([llm, build_billing_tts()]), bridged=())

    @tool()
    async def get_invoice(self, params: FunctionCallParams, customer_id: str):
        """Fetch the most recent invoice for a customer id. Returns the invoice total and status."""
        async with httpx.AsyncClient() as client:
            response = await client.post(
                os.environ["GET_INVOICE_URL"],
                json={ "customer_id": customer_id },
                timeout=30.0,
            )
            response.raise_for_status()
            await params.result_callback(response.json())

    @tool()
    async def to_human(self, params: FunctionCallParams):
        """Transfer the caller to a human."""
        await params.llm.push_frame(
            LLMMessagesAppendFrame(
                [{"role": "developer", "content": "Tell the caller you are connecting them now, then wait."}],
                run_llm=True,
            )
        )
        if _TRANSPORT is not None:
            await _TRANSPORT.sip_call_transfer({"toEndPoint": "+14155550123"})
        await params.result_callback({"transferred": True})



def build_intake_llm():
    return OpenAILLMService(
        api_key=os.environ["OPENAI_API_KEY"],
        settings=OpenAILLMService.Settings(
            model="gpt-4o-mini",
            system_instruction="# Intake agent (placeholder prompt)\n\nYou are the front desk voice agent for Acme Support. This is a phone call, so keep every answer to one or two short sentences.\n\n- Greet the caller and find out what they need.\n- When they give a phone number or email, use `lookup_customer` to find their record.\n- If the caller asks about billing, an invoice, or a refund, hand off to the billing agent with `to_billing`.\n- Never guess account details. If you cannot find the customer, say so and ask again.\n",
            temperature=0.4,
        ),
    )


def build_intake_tts():
    return SlngTTSService(
        api_key=os.environ["SLNG_API_KEY"],
        voice="aura-2-thalia-en",
        model="slng/deepgram/aura:2-en",
        language="en",
    )


class IntakeAgent(_TracedLLMWorker):
    """Agent: intake."""

    def __init__(self) -> None:
        llm = build_intake_llm()
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

    @tool()
    async def lookup_customer(self, params: FunctionCallParams, email: str = "", phone: str = ""):
        """Look up a customer record by phone number or email. Returns the customer id and name."""
        async with httpx.AsyncClient() as client:
            response = await client.post(
                os.environ["LOOKUP_CUSTOMER_URL"],
                json={ "email": email, "phone": phone },
                timeout=30.0,
            )
            response.raise_for_status()
            await params.result_callback(response.json())

    @tool()
    async def run_collect(self, params: FunctionCallParams):
        """Collect the caller's account details."""
        self._run_collect_results = {}
        # Snapshot the owner's context; the flow rewrites it per node and the
        # final step restores it (merge: results, N13).
        self._run_collect_snapshot = ([dict(m) for m in _CONTEXT.get_messages()], _CONTEXT.tools)
        flow = FlowManager(
            llm=self.llm,
            context_aggregator=LLMContextAggregatorPair(_CONTEXT),
            worker=self,
        )
        await flow.initialize(self._run_collect_node_collect())
        return {"status": "running the collect task"}

    def _run_collect_node_collect(self) -> NodeConfig:
        return NodeConfig(
            name="collect",
            task_messages=[{"role": "system", "content": "Ask for the caller's email, look them up, and confirm their account tier."}],
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
            respond_immediately=True,
        )

    async def _run_collect_finish_collect(self, args, flow_manager):
        self._run_collect_results["collect"] = dict(args)
        STATE.verified = self._run_collect_results["collect"]["verified_flag"]
        # then: return — restore the owner's pre-flow context (messages and
        # tools); only the typed results cross back (merge: results, N13).
        messages, tools = self._run_collect_snapshot
        _CONTEXT.set_messages(messages + [{
            "role": "developer",
            "content": "Task results: " + json.dumps(self._run_collect_results) + " Continue with the caller in one short line.",
        }])
        _CONTEXT.set_tools(tools)
        return {"status": "ok"}, None

    @tool()
    async def run_triage(self, params: FunctionCallParams):
        """Run the triage group."""
        self._run_triage_results = {}
        # Snapshot the owner's context; the flow rewrites it per node and the
        # final step restores it (merge: results, N13).
        self._run_triage_snapshot = ([dict(m) for m in _CONTEXT.get_messages()], _CONTEXT.tools)
        flow = FlowManager(
            llm=self.llm,
            context_aggregator=LLMContextAggregatorPair(_CONTEXT),
            worker=self,
        )
        await flow.initialize(self._run_triage_node_collect())
        return {"status": "running the triage flow"}

    def _run_triage_node_collect(self) -> NodeConfig:
        return NodeConfig(
            name="collect",
            task_messages=[{"role": "system", "content": "Ask for the caller's email, look them up, and confirm their account tier."}],
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
            respond_immediately=True,
        )

    async def _run_triage_finish_collect(self, args, flow_manager):
        self._run_triage_results["collect"] = dict(args)
        # then: return — restore the owner's pre-flow context (messages and
        # tools); only the typed results cross back (merge: results, N13).
        messages, tools = self._run_triage_snapshot
        _CONTEXT.set_messages(messages + [{
            "role": "developer",
            "content": "Task results: " + json.dumps(self._run_triage_results) + " Continue with the caller in one short line.",
        }])
        _CONTEXT.set_tools(tools)
        return {"status": "ok"}, None


# --- task tools (flows handlers) ----------------------------------------------
# Tools available inside task steps; stable module-level handlers so a
# re-registered function name always resolves to the same callable.


async def _flow_tool_lookup_customer(args, flow_manager):
    """Look up a customer record by phone number or email. Returns the customer id and name."""
    async with httpx.AsyncClient() as client:
        response = await client.post(os.environ["LOOKUP_CUSTOMER_URL"], json=dict(args), timeout=30.0)
        response.raise_for_status()
        return response.json()


# --- transport & run --------------------------------------------------------
transport_params: dict = {
    "webrtc": lambda: TransportParams(audio_in_enabled=True, audio_out_enabled=True),
    "daily": lambda: TransportParams(audio_in_enabled=True, audio_out_enabled=True),
}

AGENTS = [BillingAgent(), IntakeAgent()]



def build_stt():
    return DeepgramSTTService(
        api_key=os.environ["DEEPGRAM_API_KEY"],
        settings=DeepgramSTTService.Settings(
            model="nova-3",
            language="en",
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
    global _CONTEXT
    _CONTEXT = context
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
        _enable_agent_tracing(main, AGENTS)


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

    async def activate_entry():
        await main.activate_worker(
            "intake",
            args=LLMWorkerActivationArgs(
                messages=[{"role": "developer", "content": "Begin the conversation by saying, word for word: Hi, you have reached Acme Support. How can I help you today?"}],
                run_llm=True,
            ),
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
            # Activation frames (greeting, tools) are dropped by BusBridge until
            # main's StartFrame has traversed; SmallWebRTC only starts the
            # pipeline at connect, so gate on on_pipeline_started (B8/V14).
            await pipeline_started.wait()
            await activate_entry()

        @transport.event_handler("on_client_disconnected")
        async def on_client_disconnected(transport, client):
            await runner.cancel()

    await runner.add_workers(main, *AGENTS)

    try:
        await runner.run()
    finally:
        if tracing_enabled:
            trace.get_tracer_provider().force_flush()



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
