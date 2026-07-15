"""Generated Pipecat agent for pipecat-dev.

Compiled by `unmute`; do not edit by hand. Prompts, model routes, and the agent
graph are baked in from the spec. Secret values are read from the environment
and never written here. The agency model uses the Pipecat workers API: a main
PipelineWorker owns the transport + STT, each agent is an LLMWorker with its own
LLM and voice, and agent_transfer is activate_worker().
"""

from __future__ import annotations

import asyncio
import os
import json
from dataclasses import asdict, dataclass

import httpx
from openai import AsyncOpenAI
from dotenv import load_dotenv
from pipecat.audio.vad.silero import SileroVADAnalyzer
from pipecat.bus import BusBridgeProcessor
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
from pipecat.bus.messages import BusJobRequestMessage
from pipecat.pipeline.base_worker import BaseWorker
from pipecat.pipeline.job_decorator import job
from pipecat.services.deepgram.stt import DeepgramSTTService
from pipecat.services.openai.llm import OpenAILLMService
from pipecat.services.openai.tts import OpenAITTSService

load_dotenv()

MAIN_NAME = "main"
REQUIRED_ENV = [
    "DAILY_API_KEY",
    "DEEPGRAM_API_KEY",
    "GET_INVOICE_URL",
    "LOOKUP_CUSTOMER_URL",
    "OPENAI_API_KEY",
    "SLNG_API_KEY",
    "SLNG_TTS_URL",
]
IGNORE_PHRASES = ["okay", "right", "uh-huh"]

# Set once the transport exists, so an agent's cold-transfer @tool can reach it.
_TRANSPORT: BaseTransport | None = None


def require_env() -> None:
    missing = [name for name in REQUIRED_ENV if not os.getenv(name)]
    if missing:
        raise RuntimeError(f"Missing required environment variables: {', '.join(missing)}")


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
    return OpenAITTSService(
        api_key=os.environ["SLNG_API_KEY"],
        base_url=os.environ["SLNG_TTS_URL"],
        voice="marco",
    )


class BillingAgent(LLMWorker):
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
    return OpenAITTSService(
        api_key=os.environ["SLNG_API_KEY"],
        base_url=os.environ["SLNG_TTS_URL"],
        voice="nova-it",
    )


class IntakeAgent(LLMWorker):
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
        async with self.job("collect", name="collect", payload=asdict(STATE)) as job_ctx:
            result = job_ctx.response
        STATE.verified = result["verified_flag"]
        await params.result_callback(result)

    @tool()
    async def run_triage(self, params: FunctionCallParams):
        """Run the triage group."""
        results: dict = {}
        payload = dict(asdict(STATE))
        async with self.job("collect", name="collect", payload=payload) as job_ctx:
            results["collect"] = job_ctx.response
        await params.result_callback(results)


class CollectTask(BaseWorker):
    """Task worker: collect (delegate-and-return, structured result)."""

    @job(name="collect")
    async def run(self, message: BusJobRequestMessage):
        client = AsyncOpenAI(api_key=os.environ["OPENAI_API_KEY"])
        completion = await client.chat.completions.create(
            model="gpt-4o",
            messages=[
                {"role": "system", "content": "Collect the caller's account tier from the conversation so far."},
                {"role": "user", "content": json.dumps(message.payload or {})},
            ],
            response_format={"json_schema": {"name": "result", "schema": {"additionalProperties": False, "properties": {"tier": {"enum": ["free", "pro"], "type": "string"}, "verified_flag": {"type": "boolean"}}, "required": ["tier", "verified_flag"], "type": "object"}, "strict": True}, "type": "json_schema"},
        )
        result = json.loads(completion.choices[0].message.content)
        await self.send_job_response(message.job_id, result)

# --- transport & run --------------------------------------------------------
transport_params: dict = {
    "webrtc": lambda: TransportParams(audio_in_enabled=True, audio_out_enabled=True),
    "daily": lambda: TransportParams(audio_in_enabled=True, audio_out_enabled=True),
}

AGENTS = [BillingAgent(), IntakeAgent()]
TASK_WORKERS = [CollectTask()]



def build_stt():
    return DeepgramSTTService(
        api_key=os.environ["DEEPGRAM_API_KEY"],
        model="nova-3",
    )


async def run_bot(transport: BaseTransport, runner_args: RunnerArguments) -> None:
    require_env()
    global _TRANSPORT
    _TRANSPORT = transport
    runner = WorkerRunner(handle_sigint=runner_args.handle_sigint)

    context = LLMContext()
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

    @transport.event_handler("on_dialout_answered")
    async def on_dialout_answered(transport, data):
        # Cold transfer: the human answered, so the bot leaves the call.
        await main.queue_frame(EndFrame())

    @transport.event_handler("on_client_connected")
    async def on_client_connected(transport, client):
        await main.activate_worker(
            "intake",
            args=LLMWorkerActivationArgs(
                messages=[{"role": "developer", "content": "Begin the conversation by saying, word for word: Hi, you have reached Acme Support. How can I help you today?"}],
                run_llm=True,
            ),
        )
        asyncio.create_task(_end_after(main, 1200))

    @transport.event_handler("on_client_disconnected")
    async def on_client_disconnected(transport, client):
        await runner.cancel()

    await runner.add_workers(main, *AGENTS, *TASK_WORKERS)
    await runner.run()


async def bot(runner_args: RunnerArguments) -> None:
    transport = await create_transport(runner_args, transport_params)
    await run_bot(transport, runner_args)


if __name__ == "__main__":
    from pipecat.runner.run import main

    main()
