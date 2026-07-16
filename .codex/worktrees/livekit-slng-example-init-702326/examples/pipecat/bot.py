import logging
import os
from collections.abc import Callable

from pipecat.adapters.schemas.tools_schema import ToolsSchema
from pipecat.audio.vad.silero import SileroVADAnalyzer
from pipecat.frames.frames import TTSSpeakFrame
from pipecat.pipeline.pipeline import Pipeline
from pipecat.pipeline.worker import PipelineParams, PipelineWorker
from pipecat.processors.aggregators.llm_context import LLMContext
from pipecat.processors.aggregators.llm_response_universal import (
	LLMContextAggregatorPair,
	LLMUserAggregatorParams,
)
from pipecat.runner.types import RunnerArguments
from pipecat.runner.utils import create_transport
from pipecat.services.openai.llm import OpenAILLMService
from pipecat.transcriptions.language import Language
from pipecat.transports.base_transport import BaseTransport, TransportParams
from pipecat.workers.runner import WorkerRunner
from pipecat_slng import SlngSTTService, SlngTTSService

from tools.weather import WEATHER_SCHEMA, get_current_weather

logger = logging.getLogger(__name__)

# Using webrtc for web application testing, for prod we switch to twilio
transport_params: dict[str, Callable[..., TransportParams]] = {
	"webrtc": lambda: TransportParams(
		audio_in_enabled=True,
		audio_out_enabled=True,
	),
}

# Available trasports
# "daily": lambda: DailyParams(
#     audio_in_enabled=True,
#     audio_out_enabled=True,
# ),
# "webrtc": lambda: TransportParams(
#     audio_in_enabled=True,
#     audio_out_enabled=True,
# ),
# "twilio": lambda: FastAPIWebsocketParams(
#     audio_in_enabled=True,
#     audio_out_enabled=True,
#     # add_wav_header and serializer will be set automatically
# ),


async def run_bot(transport: BaseTransport, runner_args: RunnerArguments):
	logging.basicConfig(filename="myapp.log", level=logging.INFO)
	logger.info("Starting bot")
	slng_api_key = os.environ["SLNG_API_KEY"]
	slng_provider_key = os.environ["GRADIUM_API_KEY"]

	runner = WorkerRunner(handle_sigint=runner_args.handle_sigint)

	stt = SlngSTTService(
		api_key=slng_api_key,
		model="slng/deepgram/nova:3-en",
		language=Language.EN,
		enable_vad=True,
		enable_partials=True,
		# region_override="eu-north-1",  # uncomment to pin to a datacenter
	)

	llm = OpenAILLMService(
		api_key=os.environ["OPENAI_API_KEY"],
		settings=OpenAILLMService.Settings(
			system_instruction="You are a helpful voice assistant. Keep responses brief.",
		),
	)

	tts = SlngTTSService(
		api_key=slng_api_key,
		model="gradium/tts:default",
		voice="QETTJoT4n_WmpL3w",
		provider_key=slng_provider_key,
	)

	# Advertise the tool schema to the LLM, then wire the handler that runs
	# when the model calls it.
	llm.register_function("get_current_weather", get_current_weather)

	context = LLMContext(tools=ToolsSchema(standard_tools=[WEATHER_SCHEMA]))
	aggregators = LLMContextAggregatorPair(
		context,
		user_params=LLMUserAggregatorParams(vad_analyzer=SileroVADAnalyzer()),
	)

	pipeline = Pipeline(
		[
			transport.input(),
			stt,
			aggregators.user(),
			llm,
			tts,
			transport.output(),
			aggregators.assistant(),
		]
	)

	agent_1 = PipelineWorker(
		pipeline,
		name="assistant",
		params=PipelineParams(
			audio_in_sample_rate=16000,
			audio_out_sample_rate=24000,
			enable_metrics=True,
			enable_usage_metrics=True,
		),
	)

	# on_client_ready is an RTVI event, not a transport event. The worker
	# auto-creates the RTVI processor; register on agent_1.rtvi. Fires when the
	# client finished connecting and sent "client-ready".
	@agent_1.rtvi.event_handler("on_client_ready")
	async def on_client_ready(rtvi):
		await agent_1.queue_frames(
			[
				TTSSpeakFrame(
					"Hi! I am a voice assistant here to help you or chat with you. How can I help you today?"
				)
			]
		)

	@transport.event_handler("on_client_connected")
	async def on_client_connected(transport, client):
		logger.info("Client connected")

	@transport.event_handler("on_client_disconnected")
	async def on_client_disconnected(transport, client):
		await runner.cancel()

	await runner.add_workers(agent_1)
	await runner.run()


async def bot(runner_args: RunnerArguments):
	transport = await create_transport(runner_args, transport_params)
	await run_bot(transport, runner_args)


if __name__ == "__main__":
	from pipecat.runner.run import main

	main()
