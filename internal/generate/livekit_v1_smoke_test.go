//go:build smoke

package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// livekitSmokeScript imports the emitted agent module and instantiates every
// generated Agent/AgentTask class. Importing proves the imports and dependency
// set against the real installed packages; instantiating runs the per-agent
// llm=/tts= constructor kwargs (the drift class py_compile can never see).
// Session-level constructors run inside the entrypoint and need a JobContext,
// so they stay runtime-checked; the catalogue resolution golden pins their
// kwargs (driver-livekit T8/V10).
const livekitSmokeScript = `"""Smoke check: import the generated agent and instantiate every Agent class."""
import asyncio
import inspect
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import agent as agent_module  # noqa: E402

from livekit.agents import Agent  # noqa: E402

classes = sorted(
    (name for name, obj in vars(agent_module).items()
     if inspect.isclass(obj) and issubclass(obj, Agent) and obj.__module__ == "agent"),
)
assert classes, "no Agent classes found in agent.py"


async def _instantiate() -> None:  # AgentTask.__init__ needs a running loop
    for name in classes:
        getattr(agent_module, name)()


asyncio.run(_instantiate())
print("smoke ok:", ", ".join(classes))
`

const livekitSLNGIdentitySmokeScript = `"""Capture SLNG request identity through the pinned LiveKit Responses adapter."""
import asyncio
import copy
import json
import os
import uuid
from types import SimpleNamespace

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import agent as generated  # noqa: E402

from livekit.agents import NOT_GIVEN, llm  # noqa: E402

captured = []


class CapturedRequest(dict):
    def _process_event(self, event):
        return event


def capture_base_chat(self, *, chat_ctx, extra_kwargs=NOT_GIVEN, **kwargs):
    assert extra_kwargs is not NOT_GIVEN
    instructions = next(
        (
            item.text_content
            for item in chat_ctx.items
            if item.type == "message" and item.id == "lk.agent_task.instructions"
        ),
        next(item.text_content for item in chat_ctx.items if item.type == "message"),
    )
    record = {
        "headers": copy.deepcopy(extra_kwargs["extra_headers"]),
        "body": copy.deepcopy(extra_kwargs["extra_body"]),
        "instructions": instructions,
        "messages": [
            (item.role, item.text_content)
            for item in chat_ctx.items
            if item.type == "message"
        ],
        "item_types": [item.type for item in chat_ctx.items],
        "kwargs": kwargs,
    }
    captured.append(record)
    return CapturedRequest(record)


generated.openai.responses.LLM.chat = capture_base_chat
router = generated._SLNGResponsesLLM(
    api_key=os.environ["SLNG_API_KEY"],
    base_url="https://eu.llm-router.slng.ai/v1",
    model="slng/auto",
    use_websocket=False,
    store=False,
    slng_config={"cache_enabled": True, "tiers": {}},
)
assert captured == []


def context(rendered_prompt):
    chat_ctx = llm.ChatContext()
    chat_ctx.add_message(
        id="lk.agent_task.instructions",
        role="system",
        content=rendered_prompt,
    )
    chat_ctx.add_message(role="user", content="hello")
    return chat_ctx


class FakeSession:
    def __init__(self, userdata):
        self.userdata = userdata
        self.replies = 0
        self.spoken = []

    def generate_reply(self, *args, **kwargs):
        self.replies += 1

    async def say(self, text, *args, **kwargs):
        self.spoken.append(text)


async def enter(instance, session):
    # The pinned SDK's session property reads the active AgentActivity. A tiny
    # activity lets this smoke execute the generated on_enter call sites.
    instance._activity = SimpleNamespace(session=session)
    await instance.on_enter()


def without_session(request):
    stable = copy.deepcopy(dict(request))
    session_id = stable["headers"].pop("X-Slng-Session-Id")
    return session_id, stable


def capture_cache_request(session_id):
    before = len(captured)
    generated._SLNG_SESSION_ID.set(session_id)
    generated._SLNG_ACTIVE_SCOPE.set([None])
    values = generated.Userdata(caller_name="Ada", request_topic="hours")
    generated._activate_slng_scope(
        "router-fixture-v1--agent--front_desk",
        generated.FRONT_DESK_PROMPT,
        generated._slng_snapshot(values, ["caller_name"]),
    )
    assert len(captured) == before

    request = router.chat(chat_ctx=context("locally rendered cache prompt"))
    assert len(captured) == before + 1
    return request


def verify_cache_seed_probe():
    seed = capture_cache_request(str(uuid.uuid4()))
    probe = capture_cache_request(str(uuid.uuid4()))
    seed_session, stable_seed = without_session(seed)
    probe_session, stable_probe = without_session(probe)

    assert uuid.UUID(seed_session).version == 4
    assert uuid.UUID(probe_session).version == 4
    assert seed_session != probe_session
    assert stable_seed == stable_probe
    assert stable_seed["instructions"] == generated.FRONT_DESK_PROMPT
    assert "{{caller_name}}" in stable_seed["instructions"]
    assert stable_seed["headers"] == {
        "X-Slng-Agent-Id": "router-fixture-v1--agent--front_desk"
    }
    assert stable_seed["body"]["template_variables"] == {"caller_name": "Ada"}
    assert stable_seed["messages"][-1] == ("user", "hello")


async def one_call():
    capture_start = len(captured)
    session_id = str(uuid.uuid4())
    generated._SLNG_SESSION_ID.set(session_id)
    generated._SLNG_ACTIVE_SCOPE.set([None])
    values = generated.Userdata(caller_name="Ada", request_topic="hours")
    generated._activate_slng_scope(
        "router-fixture-v1--agent--front_desk",
        generated.FRONT_DESK_PROMPT,
        generated._slng_snapshot(values, ["caller_name"]),
    )
    values.caller_name = "Grace"
    ordinary = {
        "temperature": 0.2,
        "extra_headers": {"X-Caller": "kept"},
        "extra_body": {"trace": "kept"},
    }
    before = copy.deepcopy(ordinary)
    first = router.chat(chat_ctx=context("rendered for Ada"), extra_kwargs=ordinary)
    retry = router.chat(chat_ctx=context("rendered for Grace"), extra_kwargs=ordinary)
    assert ordinary == before
    assert first["headers"]["X-Caller"] == "kept"
    assert first["body"]["trace"] == "kept"
    assert first["body"]["template_variables"] == {"caller_name": "Ada"}
    assert retry["body"]["template_variables"] == {"caller_name": "Ada"}
    assert first["instructions"] == generated.FRONT_DESK_PROMPT
    assert retry["instructions"] == generated.FRONT_DESK_PROMPT

    # Exercise the generated activation call sites, not just their helper.
    fake_session = FakeSession(values)
    for instance, expected_id, prompt in [
        (
            generated.FrontDesk(initial=True),
            "router-fixture-v1--agent--front_desk",
            generated.FRONT_DESK_PROMPT,
        ),
        (
            generated.Specialist(),
            "router-fixture-v1--agent--specialist",
            generated.SPECIALIST_PROMPT,
        ),
        (
            generated.Standalone(),
            "router-fixture-v1--task--standalone",
            generated.STANDALONE_PROMPT,
        ),
        (
            generated.GroupStep(),
            "router-fixture-v1--task--group_step",
            generated.GROUP_STEP_PROMPT,
        ),
    ]:
        await enter(instance, fake_session)
        activated = router.chat(chat_ctx=context("locally rendered"))
        assert activated["headers"]["X-Slng-Agent-Id"] == expected_id
        assert activated["instructions"] == prompt

    # Tool continuations preserve the same call identity and frozen snapshot.
    tool_ctx = context("rendered for Grace")
    tool_ctx.insert(
        [
            llm.FunctionCall(call_id="weather-1", name="weather", arguments="{}"),
            llm.FunctionCallOutput(
                call_id="weather-1", name="weather", output="sunny", is_error=False
            ),
        ]
    )
    generated._activate_slng_scope(
        "router-fixture-v1--agent--front_desk",
        generated.FRONT_DESK_PROMPT,
        {"caller_name": "Ada"},
    )
    tool_turn = router.chat(chat_ctx=tool_ctx)
    assert "function_call" in tool_turn["item_types"]
    assert "function_call_output" in tool_turn["item_types"]
    assert tool_turn["headers"]["X-Slng-Session-Id"] == session_id
    assert tool_turn["body"]["template_variables"] == {"caller_name": "Ada"}

    # A summarizer has its own system message. It reuses the active owner scope
    # and call UUID, but must not replace that message with the raw agent prompt.
    summary_ctx = llm.ChatContext()
    summary_ctx.add_message(role="system", content="Summarize this conversation.")
    summary_ctx.add_message(role="user", content="user: hello")
    summary = router.chat(chat_ctx=summary_ctx)
    assert summary["headers"]["X-Slng-Agent-Id"].endswith("--agent--front_desk")
    assert summary["headers"]["X-Slng-Session-Id"] == session_id
    assert summary["instructions"] == "Summarize this conversation."

    for scope_id, prompt, names in [
        (
            "router-fixture-v1--agent--specialist",
            generated.SPECIALIST_PROMPT,
            ["account_tier"],
        ),
        (
            "router-fixture-v1--task--standalone",
            generated.STANDALONE_PROMPT,
            ["request_topic"],
        ),
        (
            "router-fixture-v1--task--group_step",
            generated.GROUP_STEP_PROMPT,
            ["request_topic", "caller_name"],
        ),
    ]:
        generated._activate_slng_scope(
            scope_id,
            prompt,
            generated._slng_snapshot(values, names),
        )
        record = router.chat(chat_ctx=context("locally rendered"))
        assert record["headers"]["X-Slng-Agent-Id"] == scope_id
        assert record["headers"]["X-Slng-Session-Id"] == session_id

    # Reusing one task in another group keeps the same stable task identity.
    generated._activate_slng_scope(
        "router-fixture-v1--task--group_step",
        generated.GROUP_STEP_PROMPT,
        generated._slng_snapshot(values, ["request_topic", "caller_name"]),
    )
    reused = router.chat(chat_ctx=context("locally rendered"))
    assert reused["headers"]["X-Slng-Agent-Id"].endswith("--task--group_step")
    assert {
        item["headers"]["X-Slng-Session-Id"] for item in captured[capture_start:]
    } == {session_id}
    return session_id


async def isolated_job(label):
    generated._SLNG_SESSION_ID.set(str(uuid.uuid4()))
    generated._SLNG_ACTIVE_SCOPE.set([None])
    generated._activate_slng_scope(label, "raw {{caller_name}}", {"caller_name": label})
    await asyncio.sleep(0)
    return router.chat(chat_ctx=context("rendered"))


async def main():
    verify_cache_seed_probe()
    await one_call()

    # LiveKit creates inference tasks before later agent/task activations. The
    # task must see mutations to this job's shared carrier, not its old tuple.
    generated._SLNG_SESSION_ID.set(str(uuid.uuid4()))
    generated._SLNG_ACTIVE_SCOPE.set([None])
    generated._activate_slng_scope(
        "router-fixture-v1--agent--front_desk",
        generated.FRONT_DESK_PROMPT,
        {"caller_name": "Ada"},
    )
    release = asyncio.Event()

    async def existing_inference_task():
        await release.wait()
        return router.chat(chat_ctx=context("locally rendered"))

    inference = asyncio.create_task(existing_inference_task())
    await asyncio.sleep(0)
    generated._activate_slng_scope(
        "router-fixture-v1--task--standalone",
        generated.STANDALONE_PROMPT,
        {"request_topic": "hours"},
    )
    release.set()
    inherited = await inference
    assert inherited["headers"]["X-Slng-Agent-Id"].endswith("--task--standalone")
    assert inherited["body"]["template_variables"] == {"request_topic": "hours"}

    left, right = await asyncio.gather(
        isolated_job("router-fixture-v1--agent--left"),
        isolated_job("router-fixture-v1--agent--right"),
    )
    assert left["headers"]["X-Slng-Session-Id"] != right["headers"]["X-Slng-Session-Id"]
    assert left["headers"]["X-Slng-Agent-Id"].endswith("--left")
    assert right["headers"]["X-Slng-Agent-Id"].endswith("--right")
    await router.aclose()
    print("livekit slng identity smoke ok")


asyncio.run(main())
`

const livekitSLNGResponsesSmokeScript = `"""Exercise the generated wrapper through LiveKit's pinned HTTP Responses stream."""
import asyncio
import json
import os
from types import SimpleNamespace

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import agent as generated  # noqa: E402

from livekit.agents import APIStatusError, function_tool, llm  # noqa: E402
from openai.types.responses import (  # noqa: E402
    ResponseCompletedEvent,
    ResponseCreatedEvent,
    ResponseFailedEvent,
    ResponseIncompleteEvent,
    ResponseOutputItemDoneEvent,
    ResponseTextDeltaEvent,
)


class FakeEventStream:
    def __init__(self, events):
        self.events = events

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_args):
        return None

    async def __aiter__(self):
        for event in self.events:
            yield event


class FakeResponses:
    def __init__(self):
        self.requests = []
        self.streams = []

    async def create(self, **request):
        self.requests.append(request)
        return FakeEventStream(self.streams.pop(0))


class FakeClient:
    def __init__(self):
        self.responses = FakeResponses()
        self._base_url = SimpleNamespace(netloc=b"router.test")


def created(response_id):
    return ResponseCreatedEvent.model_construct(
        response=SimpleNamespace(id=response_id),
        sequence_number=0,
        type="response.created",
    )


def completed(response_id, usage=None):
    return ResponseCompletedEvent.model_construct(
        response=SimpleNamespace(
            id=response_id,
            output=[],
            service_tier="default",
            usage=usage,
        ),
        sequence_number=9,
        type="response.completed",
    )


def text_delta(text, sequence):
    return ResponseTextDeltaEvent.model_construct(
        content_index=0,
        delta=text,
        item_id="message-1",
        logprobs=[],
        output_index=0,
        sequence_number=sequence,
        type="response.output_text.delta",
    )


def context():
    chat_ctx = llm.ChatContext()
    chat_ctx.add_message(
        id="lk.agent_task.instructions",
        role="system",
        content="rendered for Ada",
    )
    chat_ctx.add_message(role="user", content="Weather in Pune? Use the tool.")
    return chat_ctx


@function_tool
async def get_weather(city: str) -> str:
    """Get the weather for one city."""
    return f"sunny in {city}"


async def collect(router, chat_ctx, *, tools=None):
    chunks = []
    async with router.chat(chat_ctx=chat_ctx, tools=tools) as stream:
        async for chunk in stream:
            chunks.append(chunk)
    return chunks


async def expect_error(router, events, contains):
    router._client.responses.streams.append(events)
    try:
        await collect(router, context())
    except APIStatusError as error:
        assert contains in str(error), error
        assert not error.retryable
    else:
        raise AssertionError(f"{contains} stream ended without an error")


async def main():
    client = FakeClient()
    router = generated._SLNGResponsesLLM(
        client=client,
        model="slng/auto",
        use_websocket=False,
        store=False,
        slng_config={"cache_enabled": True, "tiers": {}},
    )
    assert router._opts.use_websocket is False
    assert router._ws is None
    generated._SLNG_SESSION_ID.set("00000000-0000-4000-8000-000000000001")
    generated._activate_slng_scope(
        "router-fixture-v1--agent--front_desk",
        generated.FRONT_DESK_PROMPT,
        {"caller_name": "Ada"},
    )

    client.responses.streams.append(
        [
            created("response-tool"),
            ResponseOutputItemDoneEvent.model_construct(
                item=SimpleNamespace(
                    type="function_call",
                    arguments='{"city":"Pune"}',
                    name="get_weather",
                    call_id="weather-1",
                ),
                output_index=0,
                sequence_number=1,
                type="response.output_item.done",
            ),
            completed("response-tool"),
        ]
    )
    history = context()
    tool_chunks = await collect(router, history, tools=[get_weather])
    calls = [call for chunk in tool_chunks if chunk.delta for call in chunk.delta.tool_calls]
    assert len(calls) == 1
    assert calls[0].name == "get_weather"
    assert calls[0].arguments == '{"city":"Pune"}'
    assert calls[0].call_id == "weather-1"

    history.insert(
        [
            llm.FunctionCall(
                call_id="weather-1",
                name="get_weather",
                arguments='{"city":"Pune"}',
            ),
            llm.FunctionCallOutput(
                call_id="weather-1",
                name="get_weather",
                output="31C, humid",
                is_error=False,
            ),
        ]
    )
    usage = SimpleNamespace(
        input_tokens=20,
        input_tokens_details=SimpleNamespace(cached_tokens=7),
        output_tokens=3,
        total_tokens=23,
    )
    client.responses.streams.append(
        [
            created("response-text"),
            text_delta("31C, ", 1),
            text_delta("humid", 2),
            completed("response-text", usage),
        ]
    )
    text_chunks = await collect(router, history, tools=[get_weather])
    assert "".join(
        chunk.delta.content for chunk in text_chunks if chunk.delta and chunk.delta.content
    ) == "31C, humid"
    terminal_usage = [chunk.usage for chunk in text_chunks if chunk.usage]
    assert len(terminal_usage) == 1
    assert terminal_usage[0].prompt_tokens == 20
    assert terminal_usage[0].prompt_cached_tokens == 7
    assert terminal_usage[0].completion_tokens == 3
    assert terminal_usage[0].total_tokens == 23

    first_request, replay_request = client.responses.requests[:2]
    assert first_request["stream"] is True
    assert first_request["store"] is False
    assert first_request["tools"][0]["type"] == "function"
    assert first_request["extra_headers"]["X-Slng-Agent-Id"].endswith(
        "--agent--front_desk"
    )
    assert first_request["extra_body"]["template_variables"] == {"caller_name": "Ada"}
    assert len(replay_request["input"]) == 4
    replay_types = [item.get("type") for item in replay_request["input"]]
    assert replay_types[-2:] == ["function_call", "function_call_output"]
    assert "previous_response_id" not in replay_request

    await expect_error(
        router,
        [
            ResponseFailedEvent.model_construct(
                response=SimpleNamespace(error=SimpleNamespace(message="provider failed")),
                sequence_number=1,
                type="response.failed",
            )
        ],
        "provider failed",
    )
    await expect_error(
        router,
        [
            ResponseIncompleteEvent.model_construct(
                response=SimpleNamespace(
                    incomplete_details=SimpleNamespace(reason="max_output_tokens")
                ),
                sequence_number=1,
                type="response.incomplete",
            )
        ],
        "response.incomplete: max_output_tokens",
    )
    await router.aclose()
    print("livekit slng responses smoke ok")


asyncio.run(main())
`

// livekitRequestTracingSmokeScript drives a real AgentSession through fake
// STT, LLM, and TTS services. The in-memory exporter must receive all three
// speech-pipeline observations under the framework's session trace (V21/V22).
const livekitRequestTracingSmokeScript = `"""Smoke check: a real agent session traces STT, LLM, and TTS."""
import asyncio
import time

import agent
import tracing
from livekit import rtc
from livekit.agents import (
    Agent,
    AgentSession,
    DEFAULT_API_CONNECT_OPTIONS,
    NOT_GIVEN,
    io,
    llm,
    stt,
    tts,
)
from livekit.agents.telemetry import set_tracer_provider
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter


class FakeStream(llm.LLMStream):
    async def _run(self) -> None:
        self._event_ch.send_nowait(
            llm.ChatChunk(
                id="probe",
                delta=llm.ChoiceDelta(role="assistant", content="traced"),
            )
        )


class FakeLLM(llm.LLM):
    @property
    def model(self) -> str:
        return "probe-model"

    @property
    def provider(self) -> str:
        return "probe-provider"

    def chat(
        self,
        *,
        chat_ctx,
        tools=None,
        conn_options=DEFAULT_API_CONNECT_OPTIONS,
        **kwargs,
    ) -> llm.LLMStream:
        return FakeStream(
            self,
            chat_ctx=chat_ctx,
            tools=tools or [],
            conn_options=conn_options,
        )


class FakeRecognizeStream(stt.RecognizeStream):
    async def _run(self) -> None:
        async for item in self._input_ch:
            if not isinstance(item, rtc.AudioFrame):
                continue
            self._event_ch.send_nowait(
                stt.SpeechEvent(
                    type=stt.SpeechEventType.FINAL_TRANSCRIPT,
                    request_id="stt-probe",
                    alternatives=[stt.SpeechData(language="en", text="trace this request")],
                )
            )
            self._event_ch.send_nowait(
                stt.SpeechEvent(
                    type=stt.SpeechEventType.RECOGNITION_USAGE,
                    request_id="stt-probe",
                    recognition_usage=stt.RecognitionUsage(audio_duration=item.duration),
                )
            )
            await asyncio.Future()


class FakeSTT(stt.STT):
    def __init__(self) -> None:
        super().__init__(capabilities=stt.STTCapabilities(streaming=True, interim_results=False))

    @property
    def model(self) -> str:
        return "probe-stt-model"

    @property
    def provider(self) -> str:
        return "probe-provider"

    async def _recognize_impl(self, buffer, *, language, conn_options):
        raise NotImplementedError

    def stream(
        self,
        *,
        language=NOT_GIVEN,
        conn_options=DEFAULT_API_CONNECT_OPTIONS,
    ) -> stt.RecognizeStream:
        return FakeRecognizeStream(stt=self, conn_options=conn_options)


class FakeChunkedStream(tts.ChunkedStream):
    async def _run(self, output_emitter) -> None:
        output_emitter.initialize(
            request_id="tts-probe",
            sample_rate=16000,
            num_channels=1,
            mime_type="audio/pcm",
        )
        output_emitter.push(b"\x00\x00" * 160)


class FakeTTS(tts.TTS):
    def __init__(self) -> None:
        super().__init__(
            capabilities=tts.TTSCapabilities(streaming=False),
            sample_rate=16000,
            num_channels=1,
        )

    @property
    def model(self) -> str:
        return "probe-tts-model"

    @property
    def provider(self) -> str:
        return "probe-provider"

    def synthesize(
        self,
        text,
        *,
        conn_options=DEFAULT_API_CONNECT_OPTIONS,
    ) -> tts.ChunkedStream:
        return FakeChunkedStream(tts=self, input_text=text, conn_options=conn_options)


class FakeAudioInput(io.AudioInput):
    def __init__(self) -> None:
        super().__init__(label="probe-input")
        self._queue = asyncio.Queue()

    def push(self, frame: rtc.AudioFrame) -> None:
        self._queue.put_nowait(frame)

    async def __anext__(self) -> rtc.AudioFrame:
        return await self._queue.get()


class FakeAudioOutput(io.AudioOutput):
    def __init__(self) -> None:
        super().__init__(
            label="probe-output",
            capabilities=io.AudioOutputCapabilities(pause=False),
        )
        self._playback_position = 0.0

    async def capture_frame(self, frame: rtc.AudioFrame) -> None:
        first = self._playback_position == 0.0
        await super().capture_frame(frame)
        self._playback_position += frame.duration
        if first:
            self.on_playback_started(created_at=time.time())

    def flush(self) -> None:
        super().flush()
        if self._playback_position:
            position = self._playback_position
            self._playback_position = 0.0
            self.on_playback_finished(playback_position=position, interrupted=False)

    def clear_buffer(self) -> None:
        self._playback_position = 0.0


class ProbeAgent(Agent):
    def __init__(self) -> None:
        super().__init__(instructions="Trace the probe turn.")


async def main() -> None:
    memory = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(memory))
    set_tracer_provider(provider)

    audio_input = FakeAudioInput()
    audio_output = FakeAudioOutput()
    async with AgentSession(
        stt=FakeSTT(),
        llm=FakeLLM(),
        tts=FakeTTS(),
        turn_handling={"turn_detection": "manual"},
    ) as session:
        session.input.audio = audio_input
        session.output.audio = audio_output

        pending_stt_metrics = []
        pending_tts_metrics = []

        @session.on("metrics_collected")
        def _trace_speech(ev) -> None:
            if isinstance(ev.metrics, tracing.STTMetrics):
                pending_stt_metrics.append(ev.metrics)
            elif isinstance(ev.metrics, tracing.TTSMetrics):
                pending_tts_metrics.append(ev.metrics)

        @session.on("conversation_item_added")
        def _trace_speech_item(ev) -> None:
            text = getattr(ev.item, "raw_text_content", None)
            role = getattr(ev.item, "role", None)
            if role == "user" and text and pending_stt_metrics:
                tracing.trace_speech_metrics(
                    provider,
                    pending_stt_metrics,
                    input_value="audio",
                    output_value=text,
                    ended_at=ev.created_at,
                )
                pending_stt_metrics.clear()
            elif role == "assistant" and text and pending_tts_metrics:
                tracing.trace_speech_metrics(
                    provider,
                    pending_tts_metrics,
                    input_value=text,
                    output_value="audio",
                    ended_at=ev.created_at,
                )
                pending_tts_metrics.clear()

        await session.start(ProbeAgent())
        audio_input.push(
            rtc.AudioFrame(
                data=b"\x00\x00" * 160,
                sample_rate=16000,
                num_channels=1,
                samples_per_channel=160,
            )
        )

        for _ in range(100):
            if pending_stt_metrics:
                break
            await asyncio.sleep(0.01)
        else:
            raise AssertionError("STT service path did not emit metrics")

        await session.run(user_input="trace this request")

    spans = memory.get_finished_spans()
    by_name = {span.name: span for span in spans}
    speech = {
        name: [span for span in spans if span.name == name]
        for name in ("stt", "tts")
    }
    required = {"agent_session", "stt", "llm_request", "llm_node", "tts", "tts_node"}
    assert required <= by_name.keys(), required - by_name.keys()
    assert len(speech["stt"]) == 1, len(speech["stt"])
    assert len(speech["tts"]) == 1, len(speech["tts"])
    assert by_name["stt"].attributes["gen_ai.request.model"] == "probe-stt-model"
    assert by_name["llm_request"].attributes["gen_ai.request.model"] == "probe-model"
    assert by_name["tts"].attributes["gen_ai.request.model"] == "probe-tts-model"
    assert by_name["stt"].attributes["metrics.audio_duration"] > 0
    assert by_name["tts"].attributes["metrics.audio_duration"] > 0
    assert by_name["tts"].attributes["metrics.ttfb"] >= 0
    assert by_name["stt"].attributes["langfuse.observation.input"] == "audio"
    assert by_name["stt"].attributes["langfuse.observation.output"] == "trace this request"
    assert by_name["tts"].attributes["langfuse.observation.input"] == "traced"
    assert by_name["tts"].attributes["langfuse.observation.output"] == "audio"
    session_trace = by_name["agent_session"].context.trace_id
    assert all(by_name[name].context.trace_id == session_trace for name in required)
    print("livekit speech tracing smoke ok")


asyncio.run(main())
`

// TestSmokeLiveKitV1RemyInstantiates proves the Remy emission end to end (V10,
// L4): uv resolves the emitted pyproject (network), agent.py imports, and
// every generated Agent/AgentTask instantiates against real livekit-agents +
// livekit-plugins-slng. Opt-in (`make smoke` / -tags smoke) only.
func TestSmokeLiveKitV1RemyInstantiates(t *testing.T) {
	runLiveKitSmoke(t, "remy", nil)
}

func TestSmokeLiveKitSLNGIdentityAndConcurrentJobs(t *testing.T) {
	runLiveKitSmokeScript(t, "slng-context-router", nil, nil, livekitSLNGIdentitySmokeScript)
}

func TestSmokeLiveKitSLNGResponsesStreamAndTools(t *testing.T) {
	runLiveKitSmokeScript(t, "slng-context-router", nil, nil, livekitSLNGResponsesSmokeScript)
}

// TestSmokeLiveKitV1MultiVendorInstantiates covers the per-vendor plugin
// entries in one venv: safe_core's deepgram listen + elevenlabs speak, with
// one voice rebound to cartesia (per-agent tts= overrides run at
// instantiation, checking those constructor kwargs).
func TestSmokeLiveKitV1MultiVendorInstantiates(t *testing.T) {
	runLiveKitSmoke(t, "safe_core", func(tgt *ir.Target) {
		tgt.Models.Speak["specialist"] = ir.Binding{
			Provider: "cartesia", Model: "sonic-3", Voice: "f786b574-daa5-4673-aa0c-cbe3e8534c02",
		}
	})
}

// TestSmokeLiveKitV1RestoredVendorsInstantiates covers the riskiest of the
// T17-restored entries in one venv: soniox listen (model nests in
// params=soniox.STTOptions), deepgram speak (model-only aura id, no voice
// kwarg), gemini speak (google.beta.GeminiTTS classpath), and anthropic reason
// (native plugin instead of Inference). Per-agent tts=/llm= constructors run
// at instantiation; the listen binding proves import + dependency resolution.
func TestSmokeLiveKitV1RestoredVendorsInstantiates(t *testing.T) {
	runLiveKitSmoke(t, "safe_core", func(tgt *ir.Target) {
		tgt.Models.Listen = &ir.Binding{Provider: "soniox", Model: "stt-rt-v5"}
		tgt.Models.Speak["front_desk"] = ir.Binding{
			Provider: "deepgram", Model: "aura-2-andromeda-en",
		}
		tgt.Models.Speak["specialist"] = ir.Binding{
			Provider: "gemini", Voice: "Kore",
		}
		tgt.Models.Reason["fast_reasoning"] = ir.Binding{
			Provider: "anthropic", Model: "claude-sonnet-4-6",
		}
	})
}

// TestSmokeV22LiveKitSpeechTracing proves request instrumentation, not only
// exporter connectivity: one AgentSession must trace its STT, LLM, and TTS paths.
func TestSmokeV22LiveKitSpeechTracing(t *testing.T) {
	runLiveKitSmokeScript(t, "simple-prompt", nil, nil, livekitRequestTracingSmokeScript)
}

func TestSmokeV26LiveKitExamplesStaticCheck(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	cases := []struct {
		name     string
		toolFree bool
		tracing  bool
	}{
		{name: "simple-prompt"},
		{name: "multi-task"},
		{name: "task-groups"},
		{name: "subagents"},
		{name: "simple-prompt-tool-free-unconfigured", toolFree: true},
		{name: "simple-prompt-tool-free-tracing", toolFree: true, tracing: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			example := tc.name
			if tc.toolFree {
				example = "simple-prompt"
			}
			pkg, err := spec.Load(examplePackagePath(example))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			if tc.toolFree {
				entry := agent.Agents[agent.EntryAgent]
				entry.Tools = nil
				agent.Agents[agent.EntryAgent] = entry
			}
			if tc.tracing {
				enableLangfuse(agent)
			} else if tc.toolFree {
				agent.Tracing = nil
			}
			artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			for _, file := range artifact.Files {
				path := filepath.Join(dir, file.Path)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, file.Content, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			// dl§V26: the raw generator output is ruff-check + ty-clean.
			for _, args := range [][]string{{"run", "ruff", "check", "."}, {"run", "ty", "check", "."}} {
				cmd := exec.Command("uv", args...)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("uv %v failed:\n%s", args, out)
				}
			}
			// V3 (F4): the CLI write path applies `ruff format`; assert it lays the
			// project out cleanly (write-path parity) and that the result is
			// format-stable (a second pass leaves no diff). Byte-format-stability
			// of the raw generator output stays out of scope (C1: the generator
			// never formats).
			format := exec.Command("uv", "run", "ruff", "format", ".")
			format.Dir = dir
			if out, err := format.CombinedOutput(); err != nil {
				t.Fatalf("uv run ruff format failed:\n%s", out)
			}
			diff := exec.Command("uv", "run", "ruff", "format", "--diff", ".")
			diff.Dir = dir
			if out, err := diff.CombinedOutput(); err != nil {
				t.Fatalf("emitted project is not ruff-format-stable:\n%s", out)
			}
		})
	}
}

func runLiveKitSmoke(t *testing.T, example string, mutate func(*ir.Target)) {
	t.Helper()
	runLiveKitSmokeScript(t, example, mutate, nil, livekitSmokeScript)
}

// addBuiltinEndCall attaches a prebuilt end_call tool to the entry agent (the
// scaffold/init default, prebuilt-tools T9/T11).
func addBuiltinEndCall(agent *ir.Agent) {
	agent.Tools["end_call"] = ir.Tool{
		Execution: ir.ToolBuiltin, Builtin: "end_call",
		Description:  "End the call when the caller is finished.",
		Instructions: "Thank the caller and say goodbye.",
		Effect:       ir.ToolEndsConversation, Interruption: ir.ToolProviderDefault,
	}
	def := agent.Agents[agent.EntryAgent]
	def.Tools = append(def.Tools, "end_call")
	agent.Agents[agent.EntryAgent] = def
}

// TestSmokeLiveKitV1BuiltinEndCall proves the emitted beta EndCallTool import
// and construction resolve and instantiate in a real venv (prebuilt-tools T11).
func TestSmokeLiveKitV1BuiltinEndCall(t *testing.T) {
	runLiveKitSmokeScript(t, "simple-prompt", nil, addBuiltinEndCall, livekitSmokeScript)
}

// addMCPSource attaches one fully specified MCP tool source to the entry agent
// (N40): the shape examples/mcp-example ships.
func addMCPSource(agent *ir.Agent) {
	agent.Tools["web_search"] = ir.Tool{
		Execution: ir.ToolMCP, URLEnv: "FIRECRAWL_MCP_URL",
		MCPTransport: ir.MCPTransportStreamableHTTP, MCPTools: []string{"firecrawl_search"},
		Auth:         &ir.ToolAuth{Type: ir.ToolAuthBearer, TokenEnv: "FIRECRAWL_API_KEY"},
		Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
	}
	def := agent.Agents[agent.EntryAgent]
	def.Tools = append(def.Tools, "web_search")
	agent.Agents[agent.EntryAgent] = def
}

// TestSmokeLiveKitV1MCPToolSourceInstantiates is the proof gate for N40 on this
// driver: uv must resolve the `mcp` extra the emitted pyproject now asks for,
// and the Agent's tools= surface must construct MCPToolset/MCPServerHTTP with
// exactly the kwargs the template writes. Both are drift py_compile cannot see.
func TestSmokeLiveKitV1MCPToolSourceInstantiates(t *testing.T) {
	runLiveKitSmokeScript(t, "simple-prompt", nil, addMCPSource, livekitSmokeScript)
}

func TestLiveKitSIPGeneratedPythonCompiles(t *testing.T) { // telephony T10, V20
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	agent, resolved := configuredLiveKitSIP(t)
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.py")
	if err := os.WriteFile(path, []byte(artifactFile(t, artifact, "agent.py")), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(python, "-m", "py_compile", path).CombinedOutput(); err != nil {
		t.Fatalf("generated LiveKit SIP Python does not compile: %v\n%s", err, output)
	}
}

func runLiveKitSmokeScript(t *testing.T, example string, mutate func(*ir.Target), mutateAgent func(*ir.Agent), script string) {
	t.Helper()
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	packagePath := examplePackagePath(example)
	pkg, err := spec.Load(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if mutateAgent != nil {
		mutateAgent(agent)
	}
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	if mutate != nil {
		mutate(&tgt)
	}
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	for _, file := range artifact.Files {
		path := filepath.Join(dir, file.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, file.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "smoke_check.py"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("uv", "run", "python", "smoke_check.py")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("smoke check failed:\n%s", out)
	} else {
		t.Logf("%s", out)
	}
}
