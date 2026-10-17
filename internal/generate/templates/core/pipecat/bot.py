"""This agent on Pipecat, on unmute-core. Written by `unmute compile`.

spec.py builds the agent from agent.json. This file builds the pipeline around it, and
each call runs the same way:

1. Read what the line carries: a carrier's handshake, or nothing on a browser call.
2. ``spec.new_call`` builds the call's state, with its prefetch read in, from the
   ``call_start`` object of the runner's request body. A request that lacks a required
   value, or holds a wrong one, gets no pipeline, and the log says which value.
3. The pipeline gets the speech and the model agent.yaml names, and its turn pace.
4. ``UnmuteFlows`` runs the entry agent's ``AgentSpec`` on the pipeline's context.
5. When the caller connects, the greeting plays and the call's longest length starts.

    uv run bot.py -t webrtc   # then open http://localhost:7860
    uv run bot.py -t twilio   # a Twilio Media Streams phone call
"""

import dataclasses
import logging
from collections.abc import Callable
from typing import Any

from dotenv import load_dotenv
from google import genai
from pipecat.adapters.schemas.function_schema import FunctionSchema
from pipecat.audio.turn.smart_turn.local_smart_turn_v3 import LocalSmartTurnAnalyzerV3
from pipecat.audio.vad.silero import SileroVADAnalyzer
from pipecat.frames.frames import LLMUpdateSettingsFrame
from pipecat.pipeline.pipeline import Pipeline
from pipecat.pipeline.worker import PipelineParams, PipelineWorker
from pipecat.processors.aggregators.llm_context import LLMContext
from pipecat.processors.aggregators.llm_response_universal import (
    LLMContextAggregatorPair,
    LLMUserAggregatorParams,
)
from pipecat.runner.types import RunnerArguments
from pipecat.runner.utils import create_transport
from pipecat.services.google.llm import GoogleLLMService
from pipecat.services.llm_service import FunctionCallParams, LLMService
from pipecat.services.openai.llm import OpenAILLMService
from pipecat.services.settings import LLMSettings
from pipecat.transcriptions.language import Language
from pipecat.transports.base_transport import BaseTransport, TransportParams
from pipecat.transports.websocket.fastapi import FastAPIWebsocketParams
from pipecat.turns.user_stop import TurnAnalyzerUserTurnStopStrategy
from pipecat.workers.runner import WorkerRunner
from pipecat_slng import SlngSTTService, SlngTTSService
from unmute_core.call import CallStartError
from unmute_core.model import model_name
from unmute_core.pipecat import (
    IdleNudgeObserver,
    ModelCallLLMService,
    TelemetryObserver,
    TwilioLine,
    UnmuteFlows,
    end_call_tool,
    greeting_frames,
    interruption,
    max_duration,
    smart_turn_params,
    telephony_call_info,
    vad_params,
)
from unmute_core.settings import env_value
from unmute_core.slng import TrackingIds, tracking_ids
from unmute_core.state import CallInfo, CallState, call_phone_region, use_phone_region
from unmute_core.telemetry import Langfuse, Telemetry
from unmute_core.turn import turn_window

import spec
import speech

load_dotenv()
spec.start_knowledge()
logger = logging.getLogger("bot")
MODELS: dict[str, Any] = spec.SPEC["models"]
WINDOW = turn_window(MODELS["turn"].get("pace", "balanced"), "pipecat")

transport_params: dict[str, Callable[..., Any]] = {
    "webrtc": lambda: TransportParams(audio_in_enabled=True, audio_out_enabled=True),
    "twilio": lambda: FastAPIWebsocketParams(audio_in_enabled=True, audio_out_enabled=True),
}
"""The transports ``uv run bot.py -t ...`` may pick."""


def required(name: str) -> str:
    """Return an environment variable the agent cannot run without."""
    value = env_value(name, str)
    if not value:
        raise RuntimeError(f"Set {name} in the environment or in .env")
    return value


def vertex_client(api_key: str, location: str) -> genai.Client:
    """Return a GenAI client pinned to one Vertex location, with an API key.

    Pipecat's Google service reaches Vertex only with Google Cloud credentials. This client
    uses an API key instead, on the location's own host, so EU requests stay in the EU.
    """
    if location in ("us", "eu"):
        host = f"aiplatform.{location}.rep.googleapis.com"
    elif location == "global":
        host = "aiplatform.googleapis.com"
    else:
        host = f"{location}-aiplatform.googleapis.com"
    return genai.Client(
        vertexai=True,
        api_key=api_key,
        location=location,
        http_options=genai.types.HttpOptions(
            base_url=f"https://{host}/v1beta1",
            base_url_resource_scope=genai.types.ResourceScope.COLLECTION,
            api_version="v1beta1",
            headers={"X-Vertex-AI-LLM-Shared-Request-Type": "priority"},
        ),
    )


class VertexLLM(GoogleLLMService):
    """Pipecat's Google LLM service, with a Vertex API-key client."""

    def __init__(self, *, location: str, **kwargs: Any) -> None:
        """Remember the location, then build the service."""
        self._vertex_location = location
        super().__init__(**kwargs)

    def create_client(self) -> None:
        """Build the Vertex client the service calls through."""
        self._client = vertex_client(self._api_key, self._vertex_location)


def build_llm(first: str, state: CallState) -> LLMService[Any]:
    """Return the first agent's model: the SLNG Context Router, OpenAI or Gemini.

    ``UnmuteFlows`` sets its prompt. An OpenAI binding's params that Pipecat's settings
    do not name, like ``reasoning_effort``, go in each request as they are.
    """
    binding = MODELS["think"][spec.SPEC["agents"][first]["model"]]
    if binding["provider"] == "slng":
        call = spec.router(binding, state.call)
        settings = OpenAILLMService.Settings(model=model_name(call), system_instruction="")
        return ModelCallLLMService(call, state=state, settings=settings)
    params = dict(binding.get("params", {}))
    if binding["provider"] == "openai":
        known = {field.name for field in dataclasses.fields(OpenAILLMService.Settings)}
        named = {key: value for key, value in params.items() if key in known}
        extra = {key: value for key, value in params.items() if key not in known}
        return OpenAILLMService(
            api_key=required("OPENAI_API_KEY"),
            settings=OpenAILLMService.Settings(
                model=binding["model"], system_instruction="", extra=extra, **named
            ),
        )
    thinking = params.pop("thinking_config", None)
    settings = GoogleLLMService.Settings(
        model=binding["model"],
        system_instruction="",
        thinking=GoogleLLMService.ThinkingConfig(**thinking) if thinking else None,
    )
    key = required("GOOGLE_API_KEY")
    if params.pop("vertexai", False):
        return VertexLLM(location=params.pop("location", "global"), api_key=key, settings=settings)
    return GoogleLLMService(api_key=key, settings=settings)


def line_call(runner_args: RunnerArguments) -> CallInfo | None:
    """Return what a phone call carries, from the carrier's handshake. None on a web call."""
    call_data = getattr(runner_args, "call_data", None)
    return telephony_call_info(call_data, agent_id=spec.AGENT_ID) if call_data else None


def dispatched(runner_args: RunnerArguments) -> dict[str, Any] | None:
    """Return the ``call_start`` object the call was dispatched with, or None.

    A web call carries it in the runner's request body. A phone call carries it in the
    body of the carrier's handshake, which ``create_transport`` reads.
    """
    for body in (
        getattr(runner_args, "body", None),
        (getattr(runner_args, "call_data", None) or {}).get("body"),
    ):
        if isinstance(body, dict) and isinstance(body.get("call_start"), dict):
            return body["call_start"]
    return None


def build_stt(ids: TrackingIds) -> Any:
    """Return the speech-to-text service: SLNG's with the call's tracking ids, else speech.py's."""
    if speech.LISTEN is not None:
        return speech.LISTEN()
    listen = MODELS["listen"]
    return SlngSTTService(
        api_key=required("SLNG_API_KEY"),
        model=listen["model"],
        language=Language(listen.get("language", "en")),
        world_part=listen.get("params", {}).get("world_part", "eu-north"),
        **ids,
    )


def build_tts(name: str, ids: TrackingIds) -> Any:
    """Return one voice's service: SLNG's with the call's tracking ids, else speech.py's."""
    if (make := speech.SPEAK.get(name)) is not None:
        return make()
    voice = MODELS["speak"][name]
    return SlngTTSService(
        api_key=required("SLNG_API_KEY"),
        model=voice["model"],
        voice=voice.get("voice", ""),
        language=Language(voice.get("language", "en")),
        warm_standby_enabled=True,
        world_part=voice.get("params", {}).get("world_part", "eu-north"),
        **ids,
    )


def build_pipeline(
    state: CallState, transport: BaseTransport, telemetry: Telemetry | None
) -> tuple[PipelineWorker, UnmuteFlows]:
    """Return the call's worker and the flows that run the call's first agent on it."""
    first = spec.entry(state)
    context = LLMContext()
    aggregators = LLMContextAggregatorPair(
        context,
        user_params=LLMUserAggregatorParams(
            vad_analyzer=SileroVADAnalyzer(params=vad_params(WINDOW)),
            user_turn_strategies=interruption(
                "keep",
                stop=[
                    TurnAnalyzerUserTurnStopStrategy(
                        turn_analyzer=LocalSmartTurnAnalyzerV3(params=smart_turn_params(WINDOW))
                    )
                ],
            ),
        ),
    )
    llm = build_llm(first, state)
    entry = spec.SPEC["agents"][first]
    ids = tracking_ids(state.call)
    pipeline = Pipeline(
        [
            transport.input(),
            build_stt(ids),
            aggregators.user(),
            llm,
            build_tts(entry["voice"], ids),
            transport.output(),
            aggregators.assistant(),
        ]
    )
    worker = PipelineWorker(
        pipeline, params=PipelineParams(enable_metrics=True, enable_usage_metrics=True)
    )
    line = TwilioLine(
        state.call.carrier_call_id,
        account_sid=env_value("TWILIO_ACCOUNT_SID", str) or "",
        auth_token=env_value("TWILIO_AUTH_TOKEN", str) or "",
        worker=worker,
        serializer=None,
    )

    def tools_of(name: str) -> list[FunctionSchema]:
        """Return the handoff tools and the end-call tool one agent offers, as Pipecat tools."""
        handoffs = spec.SPEC["agents"][name].get("handoffs", ())
        tools = [handoff_tool(spec.HANDOFFS[handoff]) for handoff in handoffs]
        if (ending := spec.SPEC["agents"][name].get("end_call")) is not None:
            wording = "\n".join(part for part in (ending["description"], ending["instructions"]) if part)
            tools.append(end_call_tool(wording, None, lambda: flows.reply_text))
        return tools

    def handoff_tool(handoff: spec.Handoff) -> FunctionSchema:
        """Return one handoff as a Pipecat tool whose handler calls ``UnmuteFlows.hand_off``."""

        async def handler(params: FunctionCallParams) -> None:
            """Save what the handoff sets, then give the call to the next agent."""
            if handoff.saves:
                state.save(handoff.saves)
            await flows.hand_off(
                params,
                spec.agent_spec(handoff.to, line),
                native_tools=tools_of(handoff.to),
                announce=handoff.announce,
                context=handoff.context,
                opens_with=handoff.opens_with,
            )

        return FunctionSchema(handoff.name, handoff.description, {}, [], handler=handler)

    flows = UnmuteFlows(
        spec.agent_spec(first, line),
        llm=llm,
        context=context,
        aggregators=aggregators,
        state=state,
        telemetry=telemetry,
        native_tools=tools_of(first),
    )
    flows.install()
    if telemetry is not None:
        binding = MODELS["think"][entry["model"]]
        worker.add_observer(
            TelemetryObserver(
                telemetry, model=binding["model"], provider=binding["provider"], flows=flows
            )
        )
    if spec.IDLE is not None:
        worker.add_observer(IdleNudgeObserver(spec.IDLE, worker, llm=llm))
    return worker, flows


async def bot(runner_args: RunnerArguments) -> None:
    """Run one call. The Pipecat runner calls this for each session.

    A browser call has no caller ID. ``UNMUTE_CALL_START`` can simulate one.
    ``LANGFUSE_PUBLIC_KEY`` turns tracing on.
    """
    transport = await create_transport(runner_args, transport_params)
    try:
        state = await spec.new_call(line_call(runner_args), dispatched(runner_args))
    except CallStartError as error:
        logger.error("%s: the call does not start: %s", spec.AGENT_ID, error)
        return
    telemetry = None
    if env_value("LANGFUSE_PUBLIC_KEY", str):
        telemetry = Telemetry(
            Langfuse.from_env(), agent=spec.AGENT_ID, call_id=state.call.session_id
        )
    worker, flows = build_pipeline(state, transport, telemetry)
    end_reason = "ended"

    @transport.event_handler("on_client_connected")
    async def greet(transport: BaseTransport, client: Any) -> None:
        """Set the agent's prompt, greet, and start the call's clock."""
        await worker.queue_frames(
            [
                LLMUpdateSettingsFrame(
                    delta=LLMSettings(system_instruction=flows.main_instruction())
                ),
                *greeting_frames(spec.GREETING, state),
            ]
        )
        if spec.MAX_DURATION_S is not None:
            max_duration(worker, spec.MAX_DURATION_S)

    @transport.event_handler("on_client_disconnected")
    async def hang_up(transport: BaseTransport, client: Any) -> None:
        """Make the saves steps held for the call's end, then stop the call."""
        nonlocal end_reason
        end_reason = "caller_hung_up"
        await flows.settle()
        await worker.cancel()

    if telemetry is not None:
        telemetry.start_call()
    runner = WorkerRunner(handle_sigint=runner_args.handle_sigint)
    await runner.add_workers(worker)
    try:
        with use_phone_region(lambda: call_phone_region(state.call)):
            await runner.run()
    finally:
        await flows.settle()
        if telemetry is not None:
            await telemetry.close(state=state, reason=end_reason)


if __name__ == "__main__":
    from pipecat.runner.run import main

    main()
