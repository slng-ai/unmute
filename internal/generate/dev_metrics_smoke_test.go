//go:build smoke

package generate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/slng-ai/unmute/internal/devmetrics"
	"github.com/slng-ai/unmute/internal/ir"
)

// Run the actual native sessions/workers with fake providers and held stages.
// Decode the exact producer output, binding these checks to the Go wire owner.
func checkStreamingOutput(t *testing.T, out []byte) {
	t.Helper()
	count := 0
	var captured bytes.Buffer
	for _, line := range bytes.Split(out, []byte("\n")) {
		record, found, err := devmetrics.Extract(line)
		if !found {
			continue
		}
		if err != nil {
			t.Fatalf("emitted invalid record: %v\n%s", err, line)
		}
		if record.Version != 2 {
			t.Fatalf("legacy producer emission: %s", line)
		}
		count++
		captured.Write(line)
		captured.WriteByte('\n')
	}
	if count == 0 {
		t.Fatal("native run emitted no identified streaming records")
	}
	// Optional local evidence destination; ordinary smoke runs leave only Go output.
	if dir := os.Getenv("UNMUTE_DEV_STREAM_EVIDENCE"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(t.Name())+".jsonl"), captured.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("decoded %d native streaming records", count)
}

func TestSmokeLiveKitDevStreaming(t *testing.T) {
	checkStreamingOutput(t, runLiveKitSmokeScript(t, "remy", nil, nil, devStreamingLiveKitScript))
}

func TestSmokePipecatDevStreaming(t *testing.T) {
	for _, fixture := range []string{"simple-prompt", "safe_core"} {
		t.Run(fixture, func(t *testing.T) {
			checkStreamingOutput(t, runPipecatSmokeScript(t, fixture, nil, func(agent *ir.Agent) {
				agent.Tracing = nil
				agent.Prefetch = nil
				agent.Conversation = &ir.Conversation{Greeting: &ir.Greeting{SpeaksFirst: ir.SpeaksFirstAgent, Text: "Welcome."}}
			}, devStreamingPipecatScript))
		})
	}
}

const devStreamingLiveKitScript = `"""Run inside an emitted Remy LiveKit project with the pinned SDK installed.

This exercises generated ordinary-agent, greeting and task-retry call sites.
Only the providers and audio endpoints are fake. stdout is captured during
each release barrier and every framed record is printed at exit for Go decoding.
"""

import asyncio
import io as stdio
import json
import os
import sys
import time
from contextlib import redirect_stdout
from importlib.metadata import version
from types import SimpleNamespace

import agent
import dev_metrics
from livekit import rtc
from livekit.agents import (
    DEFAULT_API_CONNECT_OPTIONS,
    NOT_GIVEN,
    Agent,
    AgentSession,
    io,
    llm,
    stt,
    tts,
)


class Capture(stdio.StringIO):
    def records(self, call_id=None):
        result = []
        for line in self.getvalue().splitlines():
            if not line.startswith("UNMUTE_METRIC "):
                continue
            record = json.loads(line.removeprefix("UNMUTE_METRIC "))
            assert record.get("version") == 2, record
            if call_id is None or record.get("call_id") == call_id:
                result.append(record)
        return result

    def latest(self, call_id, kind):
        latest = {}
        for record in self.records(call_id):
            if record["kind"] != kind:
                continue
            previous = latest.get(record["id"])
            if previous is None or record["revision"] > previous["revision"]:
                if previous:
                    assert previous["order"] == record["order"], record
                latest[record["id"]] = record
        return sorted(latest.values(), key=lambda record: record["order"])

    def messages(self, call_id, speaker):
        groups = {}
        for record in self.latest(call_id, "text"):
            segment = record["text"]
            if segment["speaker"] == speaker:
                groups.setdefault(segment["message_id"], []).append(segment)
        return [
            ("".join(part.get("separator_before", "") + part["text"] for part in parts), parts)
            for parts in groups.values()
        ]

    def has_text(self, call_id, speaker, expected, *, final=False):
        return any(
            text == expected and (not final or all(part["state"] == "final" for part in parts))
            for text, parts in self.messages(call_id, speaker)
        )


async def until(predicate, message, timeout=3):
    async with asyncio.timeout(timeout):
        while not predicate():
            await asyncio.sleep(0.005)
    assert predicate(), message


class Response:
    def __init__(self, tokens, *, held=False, request_id="provider-reused-id"):
        self.tokens = tokens
        self.held = held
        self.request_id = request_id
        self.started = asyncio.Event()
        self.finish = asyncio.Event()
        self.closed = asyncio.Event()
        self.permits = asyncio.Queue()
        self.sent = 0


class ModelStream(llm.LLMStream):
    def __init__(self, model, *, response, **kwargs):
        super().__init__(model, **kwargs)
        self.response = response

    async def _run(self):
        response = self.response
        try:
            for token in response.tokens:
                if response.held:
                    await response.permits.get()
                self._event_ch.send_nowait(
                    llm.ChatChunk(
                        id=response.request_id,
                        delta=llm.ChoiceDelta(role="assistant", content=token)
                        if token is not None
                        else None,
                    )
                )
                response.sent += 1
            if response.held:
                await response.finish.wait()
        finally:
            response.closed.set()


class Model(llm.LLM):
    def __init__(self, responses):
        super().__init__()
        self.responses = responses
        self.calls = 0

    @property
    def model(self):
        return "streaming-probe"

    @property
    def provider(self):
        return "fake-provider"

    def chat(self, *, chat_ctx, tools=None, conn_options=DEFAULT_API_CONNECT_OPTIONS, **kwargs):
        assert self.calls < len(self.responses), "Unexpected extra model invocation"
        response = self.responses[self.calls]
        self.calls += 1
        response.started.set()
        return ModelStream(
            self,
            response=response,
            chat_ctx=chat_ctx,
            tools=tools or [],
            conn_options=conn_options,
        )


class RecognizeStream(stt.RecognizeStream):
    async def _run(self):
        self._stt.ready.set()
        while True:
            self._event_ch.send_nowait(await self._stt.events.get())


class Recognizer(stt.STT):
    def __init__(self):
        super().__init__(capabilities=stt.STTCapabilities(streaming=True, interim_results=True))
        self.events = asyncio.Queue()
        self.ready = asyncio.Event()

    async def _recognize_impl(self, buffer, *, language, conn_options):
        raise AssertionError("Streaming STT must not call batch recognition")

    def stream(self, *, language=NOT_GIVEN, conn_options=DEFAULT_API_CONNECT_OPTIONS):
        return RecognizeStream(stt=self, conn_options=conn_options)

    def push(self, kind, text=""):
        self.events.put_nowait(
            stt.SpeechEvent(
                type=kind,
                request_id="stt-session-id",
                alternatives=[stt.SpeechData(language="en", text=text)],
            )
        )


class SpeechStream(tts.ChunkedStream):
    async def _run(self, output_emitter):
        output_emitter.initialize(
            request_id="tts-probe", sample_rate=16000, num_channels=1, mime_type="audio/pcm"
        )
        self._tts.started.set()
        await self._tts.release.wait()
        output_emitter.push(b"\x00\x00" * 160)


class Speaker(tts.TTS):
    def __init__(self):
        super().__init__(
            capabilities=tts.TTSCapabilities(streaming=False), sample_rate=16000, num_channels=1
        )
        self.started = asyncio.Event()
        self.release = asyncio.Event()

    def synthesize(self, text, *, conn_options=DEFAULT_API_CONNECT_OPTIONS):
        return SpeechStream(tts=self, input_text=text, conn_options=conn_options)


class AudioInput(io.AudioInput):
    def __init__(self):
        super().__init__(label="streaming-input")
        self.frames = asyncio.Queue()
        self.frames.put_nowait(
            rtc.AudioFrame(data=b"\x00\x00" * 160, sample_rate=16000, num_channels=1, samples_per_channel=160)
        )

    async def __anext__(self):
        return await self.frames.get()


class AudioOutput(io.AudioOutput):
    def __init__(self):
        super().__init__(label="streaming-output", capabilities=io.AudioOutputCapabilities(pause=False))
        self.frames = 0
        self.position = 0.0

    async def capture_frame(self, frame):
        first = self.position == 0.0
        await super().capture_frame(frame)
        self.frames += 1
        self.position += frame.duration
        if first:
            self.on_playback_started(created_at=time.time())

    def flush(self):
        super().flush()
        if self.position:
            position, self.position = self.position, 0.0
            self.on_playback_finished(playback_position=position, interrupted=False)

    def clear_buffer(self):
        self.position = 0.0


class QuietGreeter(agent.Greeter):
    async def on_enter(self):
        pass


def make_session(model, speaker, *, recognizer=None):
    session = AgentSession(
        userdata=agent.Userdata(),
        stt=recognizer,
        llm=model,
        tts=speaker,
        turn_handling={"turn_detection": "manual", "interruption": {"enabled": False}},
    )
    session.output.audio = AudioOutput()
    if recognizer:
        session.input.audio = AudioInput()
    return session


async def check_streaming(capture):
    call_id = "livekit-streaming"
    response = Response(["The", " table", " is ready."], held=True)
    model, speaker, recognizer = Model([response]), Speaker(), Recognizer()
    async with make_session(model, speaker, recognizer=recognizer) as session:
        assert dev_metrics.install_dev_metrics(session, call_id=call_id) is session
        handles = []
        session.on("speech_created", lambda ev: handles.append(ev.speech_handle))
        await session.start(QuietGreeter(initial=True))
        await asyncio.wait_for(recognizer.ready.wait(), 3)
        recognizer.push(stt.SpeechEventType.START_OF_SPEECH)
        recognizer.push(stt.SpeechEventType.INTERIM_TRANSCRIPT, "I knead")
        await until(lambda: capture.has_text(call_id, "user", "I knead"), "Interim STT was withheld")
        recognizer.push(stt.SpeechEventType.FINAL_TRANSCRIPT, "I need")
        await until(lambda: capture.has_text(call_id, "user", "I need", final=True), "Final STT was withheld")
        recognizer.push(stt.SpeechEventType.INTERIM_TRANSCRIPT, "a table")
        await until(lambda: capture.has_text(call_id, "user", "I need a table"), "New interim did not extend caller row")
        parts = capture.messages(call_id, "user")[0][1]
        assert parts[0]["state"] == "final" and parts[-1]["state"] == "provisional", parts
        recognizer.push(stt.SpeechEventType.FINAL_TRANSCRIPT, "a table.")
        recognizer.push(stt.SpeechEventType.END_OF_SPEECH)
        await until(lambda: capture.has_text(call_id, "user", "I need a table.", final=True), "Second final STT was withheld")
        await session.commit_user_turn(transcript_timeout=0, stt_flush_duration=0)
        await asyncio.wait_for(response.started.wait(), 3)
        await asyncio.sleep(5)
        assert capture.has_text(call_id, "user", "I need a table.", final=True)
        assert not capture.messages(call_id, "assistant"), "Answer appeared before model output"
        assert session.output.audio.frames == 0
        expected = ""
        for index, token in enumerate(response.tokens, 1):
            response.permits.put_nowait(None)
            expected += token
            await until(lambda: capture.has_text(call_id, "assistant", expected), "Generated model text was withheld")
            assert response.sent == index
            assert session.output.audio.frames == 0, "Audio escaped the held TTS stage"
        response.finish.set()
        await asyncio.wait_for(response.closed.wait(), 3)
        assert capture.has_text(call_id, "assistant", "The table is ready.")
        speaker.release.set()
        await until(lambda: bool(handles) and handles[-1].done(), "Native speech did not finish")
        assert handles[-1].exception() is None
        assert session.output.audio.frames > 0
        assert len(capture.messages(call_id, "assistant")) == 1, "Final transcript duplicated generated text"


async def check_generated_greeting(capture):
    call_id = "livekit-greeting"
    model, speaker = Model([]), Speaker()
    expected = "Hi, this is Remy at Fern and Oak. Are you booking a table, or planning a private event?"
    async with make_session(model, speaker) as session:
        dev_metrics.install_dev_metrics(session, call_id=call_id)
        handles = []
        session.on("speech_created", lambda ev: handles.append(ev.speech_handle))
        await session.start(agent.Greeter(initial=True))
        await until(lambda: capture.has_text(call_id, "assistant", expected), "Generated greeting call site did not stream say text")
        assert model.calls == 0 and session.output.audio.frames == 0
        assert len(handles) == 1
        response = capture.latest(call_id, "text")[0]["text"]["exchange_id"]
        assert response, "Greeting has no response identity"
        speaker.release.set()
        await asyncio.wait_for(handles[0].wait_for_playout(), 3)
        assert len(capture.messages(call_id, "assistant")) == 1
        native = dev_metrics.dev_say(session, "One more thing.", allow_interruptions=False)
        assert native is handles[-1], "say helper replaced the native speech handle"
        await asyncio.wait_for(native.wait_for_playout(), 3)


async def check_task_retry(capture):
    call_id = "livekit-task-retry"
    model = Model([Response([None]), Response([None]), Response(["Tomorrow works."])])
    speaker = Speaker()
    async with make_session(model, speaker) as session:
        dev_metrics.install_dev_metrics(session, call_id=call_id)
        handles = []
        session.on("speech_created", lambda ev: handles.append(ev.speech_handle))
        await session.start(agent.FindSlot())
        await until(lambda: capture.has_text(call_id, "assistant", "Tomorrow works."), "Generated task retries did not stream")
        assert model.calls == 3, model.calls
        operations = [
            record for record in capture.latest(call_id, "operation")
            if record["operation"]["type"] == "llm"
        ]
        assert len(operations) == 3, "Three concrete SDK invocations must not become one operation"
        assert len({record["operation"].get("exchange_id") for record in operations}) == 1
        assert operations[0]["operation"].get("exchange_id"), operations
        assert session.output.audio.frames == 0
        speaker.release.set()
        await until(lambda: bool(handles) and handles[-1].done(), "Task speech did not finish")
        assert handles[-1].exception() is None


async def check_passthrough(capture):
    call_id = "livekit-passthrough"
    speaker = Speaker()
    async with make_session(Model([]), speaker) as session:
        dev_metrics.install_dev_metrics(session, call_id=call_id)
        probe = QuietGreeter(initial=True)
        await session.start(probe)
        chunk = llm.ChatChunk(id="identity-check", delta=llm.ChoiceDelta(role="assistant", content="A fragment"))
        closed = []

        class ExpectedError(Exception):
            pass

        async def original():
            try:
                yield chunk
                raise ExpectedError("Pass this exact failure through")
            finally:
                closed.append(True)

        iterator = original()
        observed = dev_metrics.dev_llm_node(probe, lambda *_: iterator, llm.ChatContext(), [], None)
        assert await anext(observed) is chunk, "Observer replaced a yielded ChatChunk"
        try:
            await anext(observed)
        except ExpectedError:
            pass
        else:
            raise AssertionError("Observer swallowed the original iterator error")
        assert closed == [True], "Original iterator was not closed"


async def check_disabled(capture):
    os.environ.pop("UNMUTE_DEV_METRICS", None)
    before = len(capture.records())
    speaker = Speaker()
    speaker.release.set()
    async with make_session(Model([]), speaker) as session:
        assert dev_metrics.install_dev_metrics(session, call_id="livekit-disabled") is session
        probe = QuietGreeter(initial=True)
        await session.start(probe)

        async def original():
            yield "untouched"

        iterator = original()
        observed = dev_metrics.dev_llm_node(
            SimpleNamespace(session=session), lambda *_: iterator, llm.ChatContext(), [], None
        )
        assert observed is iterator, "Disabled observer replaced the original iterator"
        await iterator.aclose()
        handles = []
        session.on("speech_created", lambda ev: handles.append(ev.speech_handle))
        handle = dev_metrics.dev_say(session, "Disabled observation.")
        assert handle is handles[-1]
        await asyncio.wait_for(handle.wait_for_playout(), 3)
    assert len(capture.records()) == before, "Disabled dev reporter emitted records"


from livekit.agents import RunContext, function_tool
from livekit.agents.metrics import LLMMetrics, STTMetrics, TTSMetrics, EOUMetrics
from livekit.agents.voice.events import MetricsCollectedEvent, ToolCallUpdated, ToolExecutionUpdatedEvent


def measured(capture, call_id, metric, *, operation_id=None, exchange_id=None):
    return [record for record in capture.latest(call_id, "measurement")
            if record["measurement"]["metric"] == metric
            and (operation_id is None or record["measurement"].get("operation_id") == operation_id)
            and (exchange_id is None or record["measurement"].get("exchange_id") == exchange_id)]


async def collect_retry_metrics(capture):
    call_id = "livekit-request-metrics"
    model = Model([Response([None], request_id=""), Response([None]), Response(["Ready."])])
    speaker = Speaker()
    native = []
    async with make_session(model, speaker) as session:
        dev_metrics.install_dev_metrics(session, call_id=call_id)
        session.on("metrics_collected", lambda event: native.append(event.metrics))
        await session.start(agent.FindSlot())
        await until(lambda: len([item for item in native if isinstance(item, LLMMetrics)]) == 3,
                    "Real SDK did not report all three model invocations")
        before_audio = capture.latest(call_id, "measurement")
        assert model.calls == 3 and session.output.audio.frames == 0
        speaker.release.set()
        await until(lambda: bool([item for item in native if isinstance(item, TTSMetrics)]),
                    "Real SDK did not report synthesis metrics")
    return call_id, native, before_audio


async def collect_early_metrics(capture):
    call_id = "livekit-early-metrics"
    response = Response(["An early answer."])
    model, speaker, recognizer = Model([response]), Speaker(), Recognizer()
    session = AgentSession(
        userdata=agent.Userdata(), stt=recognizer, llm=model, tts=speaker,
        turn_handling={"turn_detection": "stt", "interruption": {"enabled": False}},
    )
    session.output.audio = AudioOutput()
    session.input.audio = AudioInput()
    native, speaking, assistant_items, handles = [], [], [], []

    def on_speaking(event):
        if event.new_state == "speaking":
            # Native listeners are an unordered set. Inspect after all listeners
            # handle this event, still before the held speech can finish.
            early = dict(getattr(session, "_early_assistant_metrics", {}))
            def snapshot():
                speaking.append({
                    "records": capture.latest(call_id, "measurement"),
                    "early": early,
                    "assistant_items": len(assistant_items),
                })
            asyncio.get_running_loop().call_soon(snapshot)

    async with session:
        installed = time.time()
        dev_metrics.install_dev_metrics(session, call_id=call_id)
        session.on("metrics_collected", lambda event: native.append(event.metrics))
        session.on("agent_state_changed", on_speaking)
        session.on("speech_created", lambda event: handles.append(event.speech_handle))
        session.on("conversation_item_added", lambda event: assistant_items.append(event.item)
                   if isinstance(event.item, llm.ChatMessage) and event.item.role == "assistant" else None)
        await session.start(QuietGreeter(initial=True))
        await asyncio.wait_for(recognizer.ready.wait(), 3)
        recognizer.push(stt.SpeechEventType.START_OF_SPEECH)
        await until(lambda: session.user_state == "speaking", "Native STT did not start the caller turn")
        recognizer.push(stt.SpeechEventType.FINAL_TRANSCRIPT, "Please check.")
        recognizer.push(stt.SpeechEventType.END_OF_SPEECH)
        await until(lambda: bool([item for item in native if isinstance(item, EOUMetrics)]),
                    "Native STT endpoint did not report EOU")
        await until(lambda: capture.has_text(call_id, "assistant", "An early answer."),
                    "Native answer did not generate")
        assert not speaking and session.output.audio.frames == 0
        speaker.release.set()
        await until(lambda: handles and handles[0].done(), "Native response did not finish")
        assert assistant_items and speaking and "e2e_latency" in speaking[0]["early"]
        reply_records = capture.latest(call_id, "measurement")
        native_say = dev_metrics.dev_say(session, "A separate announcement.")
        await asyncio.wait_for(native_say.wait_for_playout(), 3)
        assert len(speaking) == 2, "Native say did not emit its own speaking lifecycle"
    return call_id, native, speaking, assistant_items, reply_records, installed


class ToolModelStream(ModelStream):
    async def _run(self):
        response = self.response
        try:
            for chunk in response.tokens:
                self._event_ch.send_nowait(chunk)
                response.sent += 1
        finally:
            response.closed.set()


class ToolModel(Model):
    def chat(self, *, chat_ctx, tools=None, conn_options=DEFAULT_API_CONNECT_OPTIONS, **kwargs):
        assert self.calls < len(self.responses), "Unexpected tool follow-up model invocation"
        response = self.responses[self.calls]
        self.calls += 1
        response.started.set()
        return ToolModelStream(self, response=response, chat_ctx=chat_ctx, tools=tools or [], conn_options=conn_options)


async def collect_overlapping_tool_metrics(capture):
    call_id = "livekit-tool-ownership"
    entered, release = asyncio.Event(), asyncio.Event()
    native_tools = []

    class ToolsAgent(QuietGreeter):
        @function_tool
        async def slow_lookup(self, ctx: RunContext) -> str:
            """Read a lookup result after a deliberate test barrier."""
            entered.set()
            await release.wait()
            return "found"

    first = llm.ChatChunk(id="same-provider-id", delta=llm.ChoiceDelta(
        role="assistant", tool_calls=[llm.FunctionToolCall(
            call_id="old-tool-call", name="slow_lookup", arguments="{}")]))
    second = llm.ChatChunk(id="same-provider-id", delta=llm.ChoiceDelta(role="assistant", content="A newer reply."))
    model, speaker = ToolModel([Response([first]), Response([second])]), Speaker()
    speaker.release.set()
    async with make_session(model, speaker) as session:
        dev_metrics.install_dev_metrics(session, call_id=call_id)
        session.on("tool_execution_updated", lambda event: native_tools.append(event.update))
        session.on("function_tools_executed", lambda event: event.cancel_tool_reply())
        await session.start(ToolsAgent(initial=True))
        older = session.generate_reply(user_input="Run a slow lookup.")
        await asyncio.wait_for(entered.wait(), 3)
        newer = session.generate_reply(user_input="A different response.")
        await until(lambda: model.calls == 2, "New model work did not overlap the old tool")
        before_finish = capture.latest(call_id, "operation")
        # A progress event has no terminal status; use the real public event type.
        session.emit("tool_execution_updated", ToolExecutionUpdatedEvent(update=ToolCallUpdated(
            id="old-tool-call", call_id="old-tool-call", message="still reading")))
        after_progress = capture.latest(call_id, "operation")
        release.set()
        await until(lambda: older.done() and newer.done(), "Overlapping native speeches did not finish")
        assert older.exception() is None and newer.exception() is None
        assert any(item.type == "tool_call_ended" for item in native_tools)
    return call_id, before_finish, after_progress, native_tools


async def collect_unassigned_zero_metrics(capture):
    call_id = "livekit-unknown-metrics"
    async with make_session(Model([]), Speaker()) as session:
        dev_metrics.install_dev_metrics(session, call_id=call_id)
        await session.start(QuietGreeter(initial=True))
        # Provider callbacks with no native speech/request context are real SDK
        # metric objects; they must remain unassigned even if a reply exists.
        base = dict(label="same-label", request_id="same-provider-id", timestamp=time.time(),
                    cancelled=False, completion_tokens=0, prompt_tokens=0,
                    prompt_cached_tokens=0, total_tokens=0, tokens_per_second=0)
        session.emit("metrics_collected", MetricsCollectedEvent(metrics=LLMMetrics(
            **base, duration=0, ttft=0)))
        session.emit("metrics_collected", MetricsCollectedEvent(metrics=LLMMetrics(
            **{**base, "request_id": ""}, duration=0.12, ttft=-1)))
        session.emit("metrics_collected", MetricsCollectedEvent(metrics=STTMetrics(
            label="streaming-stt", request_id="stream", timestamp=time.time(),
            duration=0, audio_duration=12, streamed=True)))
    return call_id


async def check_livekit_metrics(capture):
    retries = await collect_retry_metrics(capture)
    early = await collect_early_metrics(capture)
    tools = await collect_overlapping_tool_metrics(capture)
    unknown_id = await collect_unassigned_zero_metrics(capture)

    call_id, native, before_audio = retries
    operations = [record for record in capture.latest(call_id, "operation") if record["operation"]["type"] == "llm"]
    llm_metrics = [item for item in native if isinstance(item, LLMMetrics)]
    assert len(operations) == len(llm_metrics) == 3
    for operation, metric in zip(operations, llm_metrics, strict=True):
        first = measured(capture, call_id, "first_response", operation_id=operation["id"])
        duration = measured(capture, call_id, "request_duration", operation_id=operation["id"])
        assert len(first) == len(duration) == 1, "A concrete SDK invocation lost its measurements"
        assert first[0]["id"] in {record["id"] for record in before_audio}, "LLM metric waited for audio"
        payload = first[0]["measurement"]
        if metric.ttft == -1:
            assert payload["state"] == "unavailable" and "value" not in payload
        else:
            assert payload["state"] == "measured" and payload["value"] == metric.ttft
        assert duration[0]["measurement"]["value"] == metric.duration
    assert not operations[0]["operation"].get("source_request_id")
    assert operations[1]["operation"]["source_request_id"] == operations[2]["operation"]["source_request_id"]
    assert len({record["id"] for record in operations}) == 3

    call_id, native, speaking, assistant_items, reply_records, installed = early
    at_speaking = [record for record in speaking[0]["records"] if record["measurement"]["metric"] == "reply_latency"]
    assert len(at_speaking) == 1 and speaking[0]["assistant_items"] == 0, "E2E waited for completed speech"
    assert at_speaking[0]["measurement"]["value"] == speaking[0]["early"]["e2e_latency"]
    final_e2e = measured(capture, call_id, "reply_latency")
    assert len(final_e2e) == 1 and final_e2e[0]["id"] == at_speaking[0]["id"], "Native final metric duplicated early E2E or say reused it"
    speech = measured(capture, call_id, "speech_duration")
    assert speech and all(record["measurement"]["scope"] == "response" for record in speech)
    first_speech = measured(capture, call_id, "first_speech")
    assert len(first_speech) == 1 and first_speech[0]["measurement"]["scope"] == "call"
    assert "reporter" in first_speech[0]["measurement"].get("reason", "").lower()
    started = assistant_items[0].metrics["started_speaking_at"]
    assert abs(first_speech[0]["measurement"]["value"] - (started - installed)) < 0.25
    for metric in [item for item in native if isinstance(item, TTSMetrics)]:
        assert any(record["measurement"]["value"] == metric.ttfb for record in measured(capture, call_id, "first_response")
                   if record["measurement"]["state"] == "measured")

    call_id, before_finish, after_progress, native_tools = tools
    old_requests = [record for record in before_finish if record["operation"]["type"] == "llm"]
    assert len(old_requests) == 2 and old_requests[0]["operation"]["exchange_id"] != old_requests[1]["operation"]["exchange_id"]
    before_tool = [record for record in before_finish if record["operation"]["type"] == "tool"]
    progress_tool = [record for record in after_progress if record["operation"]["type"] == "tool"]
    finished = [record for record in capture.latest(call_id, "operation") if record["operation"]["type"] == "tool"]
    assert len(before_tool) == len(progress_tool) == len(finished) == 1, "Tool lifecycle was missing or duplicated"
    assert before_tool[0]["operation"]["state"] == progress_tool[0]["operation"]["state"] == "running"
    assert finished[0]["id"] == before_tool[0]["id"] and finished[0]["operation"]["state"] == "returned"
    assert finished[0]["operation"]["parent_operation_id"] == old_requests[0]["id"]
    assert finished[0]["operation"]["exchange_id"] == old_requests[0]["operation"]["exchange_id"]
    assert len(measured(capture, call_id, "tool_duration", operation_id=finished[0]["id"])) == 1

    unassigned = capture.latest(unknown_id, "measurement")
    assert unassigned and all(not record["measurement"].get("operation_id") for record in unassigned)
    assert not [record for record in capture.latest(unknown_id, "operation") if record["operation"]["type"] == "llm"], "Aggregate metrics invented model calls"
    first = measured(capture, unknown_id, "first_response")
    assert len(first) == 2, "Missing/reused provider IDs merged unrelated aggregate metrics"
    assert any(record["measurement"]["state"] == "measured" and record["measurement"]["value"] == 0 for record in first)
    assert any(record["measurement"]["state"] == "unavailable" and "value" not in record["measurement"] for record in first)
    assert any(record["measurement"]["metric"] == "request_duration" and record["measurement"]["state"] == "unavailable"
               and "value" not in record["measurement"] for record in unassigned), "Streaming STT zero became a duration"


async def check_metrics_while_model_is_open(capture):
    call_id = "livekit-live-node-metrics"
    response = Response([
        "This is an early answer with enough words to synthesize. ",
        "Here is the start of another sentence.",
    ], held=True)
    model, speaker = Model([response]), Speaker()
    native_metrics, native_playback, speaking, assistant_items = [], [], [], []
    async with make_session(model, speaker) as session:
        before_install = time.time()
        dev_metrics.install_dev_metrics(session, call_id=call_id)
        after_install = time.time()
        session.on("metrics_collected", lambda event: native_metrics.append(event.metrics))
        session.output.audio.on("playback_started", lambda event: native_playback.append(event.created_at))
        session.on("conversation_item_added", lambda event: assistant_items.append(event.item)
                   if isinstance(event.item, llm.ChatMessage) and event.item.role == "assistant" else None)

        def on_speaking(event):
            if event.new_state != "speaking":
                return
            early = dict(getattr(session, "_early_assistant_metrics", {}))

            def snapshot():
                speaking.append((early, capture.latest(call_id, "measurement")))

            # Native EventEmitter listener order is not guaranteed.
            asyncio.get_running_loop().call_soon(snapshot)

        session.on("agent_state_changed", on_speaking)
        await session.start(QuietGreeter(initial=True))
        handle = session.generate_reply(user_input="Start answering now.")
        await asyncio.wait_for(response.started.wait(), 3)
        for _ in response.tokens:
            response.permits.put_nowait(None)
        await asyncio.wait_for(speaker.started.wait(), 3)
        speaker.release.set()
        await until(lambda: bool(speaking), "Native speech did not start with the model stream still open")
        assert not response.closed.is_set() and not handle.done()
        assert not assistant_items and not [item for item in native_metrics if isinstance(item, LLMMetrics)]
        assert native_playback
        early, records = speaking[0]
        assert all(name in early for name in ("llm_node_ttft", "tts_node_ttfb", "playback_latency")), early
        early_first_speech = [record for record in records if record["measurement"]["metric"] == "first_speech"]
        # Finish native work before asserting adapter behavior: an initial red
        # proves the fixture itself is executable, and closes every SDK task.
        response.finish.set()
        await until(handle.done, "Native model/audio did not finish after releasing the stream")
        assert handle.exception() is None and assistant_items
        assert assistant_items[0].metrics["started_speaking_at"] == native_playback[0]

    for field in ("llm_node_ttft", "tts_node_ttfb", "playback_latency"):
        matches = [record for record in records if field in record["measurement"]["source"]]
        assert len(matches) == 1, f"Available native {field} was withheld at speaking start"
        metric = matches[0]["measurement"]
        assert metric["scope"] == "response" and not metric.get("operation_id")
        assert metric["state"] == "measured" and metric["value"] == early[field]
        final = [record for record in capture.latest(call_id, "measurement") if record["id"] == matches[0]["id"]]
        assert len(final) == 1 and final[0]["measurement"]["value"] == assistant_items[0].metrics[field]

    assert len(early_first_speech) == 1, "Native first speech timestamp waited for the completed assistant item"
    metric = early_first_speech[0]["measurement"]
    assert metric["scope"] == "call" and metric["state"] == "measured"
    assert native_playback[0] - after_install <= metric["value"] <= native_playback[0] - before_install
    final_first_speech = measured(capture, call_id, "first_speech")
    assert len(final_first_speech) == 1 and final_first_speech[0]["id"] == early_first_speech[0]["id"]


from livekit.agents import vad
from livekit.agents.voice.agent_session import SessionConnectOptions
from livekit.agents.voice.events import AgentStateChangedEvent


async def collect_cancel_and_error(capture):
    call_id = "livekit-cancel-error"
    response = Response(["An unfinished answer."], held=True, request_id="repeated-request")
    model, speaker = Model([response]), Speaker()
    session = AgentSession(
        userdata=agent.Userdata(), llm=model, tts=speaker,
        conn_options=SessionConnectOptions(max_unrecoverable_errors=0),
        turn_handling={"turn_detection": "manual", "interruption": {"enabled": False}},
    )
    session.output.audio = AudioOutput()
    closes, native_errors = [], []
    async with session:
        dev_metrics.install_dev_metrics(session, call_id=call_id)
        reporter = getattr(session, "_unmute_dev_reporter")
        session.on("close", closes.append)
        session.on("error", native_errors.append)
        await session.start(QuietGreeter(initial=True))
        handle = session.generate_reply(user_input="Keep these caller words.")
        await asyncio.wait_for(response.started.wait(), 3)
        response.permits.put_nowait(None)
        await until(lambda: capture.has_text(call_id, "assistant", "An unfinished answer."), "Cancellation fixture produced no text")
        assert session.output.audio.frames == 0 and not response.closed.is_set()
        handle.interrupt(force=True)
        await until(handle.done, "Native interruption did not close the speech")
        assert handle.interrupted and response.closed.is_set() and session.output.audio.frames == 0
        before_close = capture.latest(call_id, "measurement")
        # A provider's real error event drives the native Session close path.
        model.emit("error", llm.LLMError(timestamp=time.time(), label="fake-provider", recoverable=False,
                                         error=RuntimeError("expected provider failure")))
        await until(lambda: bool(closes), "Unrecoverable native error did not close the session")
        await asyncio.sleep(0)
        assert closes[0].reason.value == "error" and closes[0].error is not None and native_errors
        assert getattr(session, "_unmute_dev_reporter", None) is None
        count = len(capture.records(call_id))
        # Already queued observer callbacks must be inert even after teardown.
        reporter.agent_state(AgentStateChangedEvent(old_state="thinking", new_state="speaking"))
        reporter.speech_done(handle)
        reporter.close(closes[0])
        assert len(capture.records(call_id)) == count
    return call_id, reporter, handle, before_close


async def collect_input_source_loss(capture):
    call_id = "livekit-missing-input-fields"
    recognizer, model, speaker = Recognizer(), Model([Response(["I heard you."])]), Speaker()
    speaker.release.set()
    original = dev_metrics._native_input
    # Only the reporter sees this view. Its existing guard reads a missing
    # buffer field; the actual SDK recognition activity is unchanged.
    activity = SimpleNamespace(_new_turns_blocked=False)
    activity._audio_recognition = SimpleNamespace(_hooks=activity)
    dev_metrics._native_input = lambda session: original(SimpleNamespace(_activity=activity))
    try:
        async with make_session(model, speaker, recognizer=recognizer) as session:
            dev_metrics.install_dev_metrics(session, call_id=call_id)
            handles = []
            session.on("speech_created", lambda event: handles.append(event.speech_handle))
            await session.start(QuietGreeter(initial=True))
            await asyncio.wait_for(recognizer.ready.wait(), 3)
            recognizer.push(stt.SpeechEventType.FINAL_TRANSCRIPT, "Known caller words.")
            await until(lambda: capture.has_text(call_id, "user", "Known caller words.", final=True), "Source loss withheld STT")
            await session.commit_user_turn(transcript_timeout=0, stt_flush_duration=0)
            await until(lambda: handles and handles[0].done(), "Source loss changed native call behavior")
            assert model.calls == 1 and handles[0].exception() is None and session.output.audio.frames > 0
    finally:
        dev_metrics._native_input = original
    return call_id


async def collect_speech_identity_recovery(capture):
    call_id = "livekit-missing-speech-fields"
    response = Response(["A source-limited answer."], held=True)
    model, speaker = Model([response]), Speaker()
    original = dev_metrics.agent_activity
    before_metrics = None
    async with make_session(model, speaker) as session:
        dev_metrics.install_dev_metrics(session, call_id=call_id)
        await session.start(QuietGreeter(initial=True))
        # Hide the reporter module's private-field view. The native SDK still
        # owns its original module and ContextVars, which supply later metrics.
        dev_metrics.agent_activity = SimpleNamespace()
        try:
            handle = session.generate_reply(user_input="Keep the identities honest.")
            await asyncio.wait_for(response.started.wait(), 3)
            response.permits.put_nowait(None)
            await until(lambda: capture.has_text(call_id, "assistant", "A source-limited answer."), "Missing identity withheld generated words")
            before_metrics = capture.latest(call_id, "operation")
            response.finish.set()
            await asyncio.wait_for(response.closed.wait(), 3)
            await until(lambda: any(record["measurement"]["source"] == "LiveKit LLMMetrics.duration"
                                   for record in capture.latest(call_id, "measurement")), "Native metric context was lost")
        finally:
            dev_metrics.agent_activity = original
        speaker.release.set()
        await until(handle.done, "Source recovery changed native playback")
        assert handle.exception() is None and model.calls == 1 and session.output.audio.frames > 0
    return call_id, before_metrics


async def collect_native_input_boundaries(capture):
    call_id = "livekit-input-boundaries"
    recognizer, model, speaker = Recognizer(), Model([Response(["Okay."]), Response(["Again."])]), Speaker()
    speaker.release.set()
    async with make_session(model, speaker, recognizer=recognizer) as session:
        dev_metrics.install_dev_metrics(session, call_id=call_id)
        handles = []
        session.on("speech_created", lambda event: handles.append(event.speech_handle))
        await session.start(QuietGreeter(initial=True))
        await asyncio.wait_for(recognizer.ready.wait(), 3)
        recognition = session._activity._audio_recognition

        async def vad_event(kind):
            # Drive the real pinned recognition handler, not display events.
            await recognition._on_vad_event(vad.VADEvent(type=kind, samples_index=0,
                timestamp=time.time(), speech_duration=0.01, silence_duration=0))

        await vad_event(vad.VADEventType.START_OF_SPEECH)
        first_anchor = recognition._speech_start_time
        recognizer.push(stt.SpeechEventType.FINAL_TRANSCRIPT, "same words")
        await until(lambda: capture.has_text(call_id, "user", "same words", final=True), "First native segment missing")
        await vad_event(vad.VADEventType.END_OF_SPEECH)
        await vad_event(vad.VADEventType.START_OF_SPEECH)
        assert recognition._speech_start_time != first_anchor and recognition._audio_transcript == "same words"
        recognizer.push(stt.SpeechEventType.FINAL_TRANSCRIPT, "same words")
        await until(lambda: recognition._audio_transcript == "same words same words", "SDK did not accumulate repeated segments")
        before_commit = capture.messages(call_id, "user")
        await vad_event(vad.VADEventType.END_OF_SPEECH)
        await session.commit_user_turn(transcript_timeout=0, stt_flush_duration=0)
        await until(lambda: handles and handles[0].done(), "Native committed turn did not finish")
        assert recognition._audio_transcript == ""
        recognizer.push(stt.SpeechEventType.FINAL_TRANSCRIPT, "same words")
        await until(lambda: recognition._audio_transcript == "same words", "Second input turn did not reach the native buffer")
        await session.commit_user_turn(transcript_timeout=0, stt_flush_duration=0)
        await until(lambda: len(handles) == 2 and handles[1].done(), "Second native input turn did not finish")
        assert model.calls == 2
    return call_id, before_commit


async def collect_accepted_speculation(capture):
    call_id = "livekit-accepted-speculation"
    response = Response(["An accepted speculative reply."], held=True)
    recognizer, model, speaker = Recognizer(), Model([response]), Speaker()
    temporary_ids, committed_ids, handles = [], [], []

    class SpeculativeGreeter(QuietGreeter):
        async def on_user_turn_completed(self, chat_ctx, new_message):
            temporary_ids.append(new_message.id)

    session = AgentSession(userdata=agent.Userdata(), stt=recognizer, llm=model, tts=speaker,
        turn_handling={"turn_detection": "stt", "preemptive_generation": {"enabled": True},
                       "interruption": {"enabled": False}})
    session.input.audio, session.output.audio = AudioInput(), AudioOutput()
    async with session:
        dev_metrics.install_dev_metrics(session, call_id=call_id)
        session.on("speech_created", lambda event: handles.append(event.speech_handle))
        session.on("conversation_item_added", lambda event: committed_ids.append(event.item.id)
                   if isinstance(event.item, llm.ChatMessage) and event.item.role == "user" else None)
        await session.start(SpeculativeGreeter(initial=True))
        await asyncio.wait_for(recognizer.ready.wait(), 3)
        recognizer.push(stt.SpeechEventType.START_OF_SPEECH)
        recognizer.push(stt.SpeechEventType.PREFLIGHT_TRANSCRIPT, "Book tomorrow.")
        await asyncio.wait_for(response.started.wait(), 3)
        speculative_id = session._activity._preemptive_generation.user_message.id
        response.permits.put_nowait(None)
        await until(lambda: capture.has_text(call_id, "assistant", "An accepted speculative reply."), "Speculative text did not stream")
        recognizer.push(stt.SpeechEventType.FINAL_TRANSCRIPT, "Book tomorrow.")
        recognizer.push(stt.SpeechEventType.END_OF_SPEECH)
        await until(lambda: bool(committed_ids), "Native speculation was not committed")
        assert committed_ids == [speculative_id] and temporary_ids and temporary_ids[0] != speculative_id
        response.finish.set()
        speaker.release.set()
        await until(lambda: handles and handles[0].done(), "Accepted speculative speech did not finish")
        assert len(handles) == model.calls == 1 and handles[0].exception() is None
    return call_id


async def collect_old_call_isolation(capture):
    sessions, reporters, terminal = [], [], []
    call_ids = ["livekit-reused-native-one", "livekit-reused-native-two"]

    class ReusedToolAgent(QuietGreeter):
        @function_tool
        async def same_tool(self) -> str:
            """Return a local value without side effects."""
            return "value"

    for call_id in call_ids:
        chunk = llm.ChatChunk(id="repeated-request", delta=llm.ChoiceDelta(role="assistant",
            tool_calls=[llm.FunctionToolCall(call_id="repeated-tool", name="same_tool", arguments="{}")]))
        session = make_session(ToolModel([Response([chunk])]), Speaker())
        sessions.append(session)
        async with session:
            dev_metrics.install_dev_metrics(session, call_id=call_id)
            reporters.append(getattr(session, "_unmute_dev_reporter"))
            session.on("function_tools_executed", lambda event: event.cancel_tool_reply())
            session.on("tool_execution_updated", lambda event: terminal.append(event)
                       if event.update.type == "tool_call_ended" else None)
            await session.start(ReusedToolAgent(initial=True))
            handle = session.generate_reply(user_input="Read the value.")
            await until(handle.done, "Reused native tool call did not complete")
            assert handle.exception() is None
            if len(sessions) == 2:
                old_count, new_count = len(capture.records(call_ids[0])), len(capture.records(call_ids[1]))
                sessions[0].emit("tool_execution_updated", terminal[0])
                reporters[0].tool(terminal[0])
                assert len(capture.records(call_ids[0])) == old_count and len(capture.records(call_ids[1])) == new_count
    assert len(terminal) == 3  # Two native endings plus the deliberate old-call replay.
    return call_ids


async def check_livekit_lifecycle(capture):
    closed = await collect_cancel_and_error(capture)
    source_loss = await collect_input_source_loss(capture)
    identity = await collect_speech_identity_recovery(capture)
    boundaries = await collect_native_input_boundaries(capture)
    speculation = await collect_accepted_speculation(capture)
    old_calls = await collect_old_call_isolation(capture)

    call_id, reporter, handle, before_close = closed
    call = capture.latest(call_id, "call")[0]["call"]
    assert call["state"] == "error" and "error" in call.get("reason", ""), "Native close error/reason was discarded"
    assert capture.has_text(call_id, "user", "Keep these caller words.", final=True)
    answer = capture.messages(call_id, "assistant")
    assert len(answer) == 1 and answer[0][0] == "An unfinished answer." and answer[0][1][0]["state"] == "incomplete"
    ops = [record for record in capture.latest(call_id, "operation") if record["operation"]["type"] == "llm"]
    assert len(ops) == 1 and ops[0]["operation"]["state"] == "cancelled"
    for record in before_close:
        assert record in capture.latest(call_id, "measurement"), "Closing changed a measured quantity"
    assert reporter.speech_done not in handle._done_callbacks, "Closed reporter remains attached to native speech handle"
    assert not [name for name, value in vars(reporter).items() if isinstance(value, (dict, set, list)) and value], "Closed reporter retained per-call state"
    assert reporter.audio_output is None and reporter.session is None

    assert capture.latest(source_loss, "call")[0]["call"]["input_boundaries"] == "partial"
    assert len(capture.messages(source_loss, "user")) == 1, "Final item repeated STT already shown with limited grouping"
    call_id, before = identity
    assert len(before) == 1 and not before[0]["operation"].get("exchange_id")
    assert capture.latest(call_id, "call")[0]["call"]["model_calls"] == "partial"
    operations = [record for record in capture.latest(call_id, "operation") if record["operation"]["type"] == "llm"]
    assert len(operations) == 1 and operations[0]["id"] == before[0]["id"] and operations[0]["operation"].get("exchange_id"), "Native metric proof did not recover ownership"
    assert len(capture.messages(call_id, "assistant")) == 1, "Source recovery repeated the generated answer"
    duration = measured(capture, call_id, "request_duration", operation_id=operations[0]["id"])
    assert len(duration) == 1 and duration[0]["measurement"]["exchange_id"] == operations[0]["operation"]["exchange_id"]
    call_id, before_commit = boundaries
    assert len(before_commit) == 1 and before_commit[0][0] == "same words same words", "Changing anchor split one native input buffer"
    assert [message for message, _ in capture.messages(call_id, "user")] == ["same words same words", "same words"], "Native commit failed to create the next input row"
    assert len(capture.messages(speculation, "user")) == len(capture.messages(speculation, "assistant")) == 1, "Accepted speculative temporary IDs duplicated conversation rows"
    for call_id in old_calls:
        tools = [record for record in capture.latest(call_id, "operation") if record["operation"]["type"] == "tool"]
        assert len(tools) == 1 and tools[0]["operation"]["state"] == "returned"


async def main(capture):
    assert version("livekit-agents") == "1.6.10"
    for name in ("install_dev_metrics", "dev_llm_node", "dev_say"):
        assert callable(getattr(dev_metrics, name, None)), f"Missing streaming helper: {name}"
    assert agent.Greeter.llm_node is not Agent.llm_node, "Ordinary generated agent bypasses streaming helper"
    os.environ["UNMUTE_DEV_METRICS"] = "1"
    await check_streaming(capture)
    await check_generated_greeting(capture)
    await check_task_retry(capture)
    await check_passthrough(capture)
    await check_livekit_metrics(capture)
    await check_metrics_while_model_is_open(capture)
    await check_livekit_lifecycle(capture)
    await check_disabled(capture)


if __name__ == "__main__":
    captured = Capture()
    try:
        with redirect_stdout(captured):
            asyncio.run(main(captured))
    finally:
        for output_line in captured.getvalue().splitlines():
            if output_line.startswith("UNMUTE_METRIC "):
                sys.stdout.write(output_line + "\n")
        sys.stdout.flush()
    print("LiveKit streaming smoke passed: finality 5s, pre-audio text, native greeting, task retries, pass-through, env off")
`

const devStreamingPipecatScript = `"""Drive the generated bot through real Pipecat 1.8 workers, without providers.

Run this same script in inline and multi-agent artifacts. The Go harness removes
tracing, prefetch and inactivity, and gives each artifact a fixed direct greeting.
Only providers, media I/O and turn-detection decisions are controlled here. The
generated run_bot still builds services, installs hooks and activates its workers.
"""

import asyncio
import io
import json
import os
import time
from contextlib import redirect_stdout

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from pipecat.frames.frames import (  # noqa: E402
    Frame,
    InterimTranscriptionFrame,
    InterruptionFrame,
    LLMContextFrame,
    LLMFullResponseEndFrame,
    LLMFullResponseStartFrame,
    LLMTextFrame,
    LLMThoughtTextFrame,
    TTSAudioRawFrame,
    TranscriptionFrame,
    UserStartedSpeakingFrame,
    UserStoppedSpeakingFrame,
)
from pipecat.pipeline.worker import PipelineWorker  # noqa: E402
from pipecat.processors.aggregators.llm_response_universal import (  # noqa: E402
    LLMContextAggregatorPair,
    LLMUserAggregatorParams,
)
from pipecat.processors.frame_processor import FrameDirection, FrameProcessor  # noqa: E402
from pipecat.runner.types import RunnerArguments  # noqa: E402
from pipecat.services.llm_service import LLMService  # noqa: E402
from pipecat.services.settings import LLMSettings, STTSettings, TTSSettings  # noqa: E402
from pipecat.services.stt_service import STTService  # noqa: E402
from pipecat.services.tts_service import TTSService  # noqa: E402
from pipecat.transports.base_transport import BaseTransport  # noqa: E402
from pipecat.turns.user_start.external_user_turn_start_strategy import (  # noqa: E402
    ExternalUserTurnStartStrategy,
)
from pipecat.turns.user_stop.external_user_turn_stop_strategy import (  # noqa: E402
    ExternalUserTurnStopStrategy,
)
from pipecat.turns.user_turn_strategies import UserTurnStrategies  # noqa: E402
from pipecat.workers.runner import WorkerRunner  # noqa: E402

SENTINEL = "UNMUTE_METRIC "
PRIVATE_THOUGHT = "private reasoning must never reach the dev feed"


class Capture(io.StringIO):
    def records(self):
        return [
            json.loads(line[len(SENTINEL) :])
            for line in self.getvalue().splitlines()
            if line.startswith(SENTINEL)
        ]

    def latest(self, kind):
        latest = {}
        for record in self.records():
            if record.get("kind") == kind:
                latest[record["id"]] = record
        return list(latest.values())

    def text(self, speaker):
        return [
            record for record in self.latest("text")
            if record["text"]["speaker"] == speaker
        ]


async def until(predicate, label, timeout=2):
    try:
        async with asyncio.timeout(timeout):
            while not predicate():
                await asyncio.sleep(0.005)
    except TimeoutError as error:
        raise AssertionError(label) from error


class Request:
    def __init__(self):
        self.first = asyncio.Event()
        self.rest = asyncio.Event()
        self.finished = asyncio.Event()
        self.cancelled = False


class FakeLLM(LLMService):
    def __init__(self, probe):
        super().__init__(settings=LLMSettings(
            model="streaming-probe", system_instruction="", temperature=None,
            max_tokens=None, top_p=None, top_k=None, frequency_penalty=None,
            presence_penalty=None, seed=None, filter_incomplete_user_turns=False,
            user_turn_completion_config=None,
        ))
        self.probe = probe
        probe.llms.append(self)

    def can_generate_metrics(self):
        return True

    async def process_frame(self, frame, direction):
        await super().process_frame(frame, direction)
        if not isinstance(frame, LLMContextFrame):
            await self.push_frame(frame, direction)
            return
        request = Request()
        self.probe.requests.append(request)
        await self.start_processing_metrics()
        await self.start_ttfb_metrics()
        await self.push_frame(LLMFullResponseStartFrame())
        try:
            await request.first.wait()
            await self.stop_ttfb_metrics()
            await self.push_frame(LLMThoughtTextFrame(PRIVATE_THOUGHT))
            await self.push_frame(LLMTextFrame("Hel"))
            await request.rest.wait()
            # Empty and punctuation chunks must not acquire invented spaces.
            for fragment in ["", "lo", " there", "."]:
                await self.push_frame(LLMTextFrame(fragment))
        except asyncio.CancelledError:
            request.cancelled = True
            raise
        finally:
            await self.stop_processing_metrics()
            await self.push_frame(LLMFullResponseEndFrame())
            request.finished.set()


class FakeSTT(STTService):
    def __init__(self, probe):
        super().__init__(
            audio_passthrough=False,
            sample_rate=16000,
            settings=STTSettings(model="streaming-stt", language="en"),
        )
        probe.stt = self

    async def run_stt(self, audio):
        # The test supplies native recognizer frames at the service boundary.
        if False:
            yield None


class FakeTTS(TTSService):
    def __init__(self, probe):
        super().__init__(
            push_start_frame=True,
            push_stop_frames=True,
            push_text_frames=False,
            sample_rate=16000,
            settings=TTSSettings(model="streaming-tts", voice="probe", language="en"),
        )
        self.probe = probe

    def can_generate_metrics(self):
        return True

    async def run_tts(self, text, context_id):
        release = asyncio.Event()
        self.probe.syntheses.append((text, release))
        await release.wait()
        yield TTSAudioRawFrame(
            audio=b"\x00\x00" * 160,
            sample_rate=16000,
            num_channels=1,
            context_id=context_id,
        )


class PassThrough(FrameProcessor):
    def __init__(self, probe, output=False):
        super().__init__()
        self.probe = probe
        self.output = output

    async def process_frame(self, frame: Frame, direction: FrameDirection):
        await super().process_frame(frame, direction)
        if self.output and isinstance(frame, TTSAudioRawFrame):
            self.probe.audio.append(frame)
        await self.push_frame(frame, direction)


class FakeTransport(BaseTransport):
    def __init__(self, probe):
        super().__init__()
        self._register_event_handler("on_client_disconnected")
        self._input = PassThrough(probe)
        self._output = PassThrough(probe, output=True)

    def input(self):
        return self._input

    def output(self):
        return self._output


class Probe:
    def __init__(self):
        self.requests = []
        self.syntheses = []
        self.audio = []
        self.llms = []
        self.stt = None
        self.runner = None
        self.main = None
        self.started = asyncio.Event()
        self.transcripts_processed = []


def patch_providers(probe):
    # No hosted constructor runs. Generated agent-local constructors still run.
    bot.build_stt = lambda: FakeSTT(probe)
    for name in vars(bot).copy():
        if name.startswith("build_") and name.endswith("_llm"):
            setattr(bot, name, lambda *_args, **_kwargs: FakeLLM(probe))
        if name.startswith("build_") and name.endswith("_tts"):
            setattr(bot, name, lambda *_args, **_kwargs: FakeTTS(probe))
    for name in ("SileroVADAnalyzer", "LocalSmartTurnAnalyzerV3", "TurnAnalyzerUserTurnStopStrategy"):
        if hasattr(bot, name):
            setattr(bot, name, lambda **_kwargs: None)
    bot.LLMUserAggregatorParams = lambda **_kwargs: LLMUserAggregatorParams(
        user_turn_strategies=UserTurnStrategies(
            start=[ExternalUserTurnStartStrategy()],
            stop=[ExternalUserTurnStopStrategy(wait_for_transcript=False)],
        ),
    )

    def capture_aggregators(*args, **kwargs):
        pair = LLMContextAggregatorPair(*args, **kwargs)
        user, _assistant = pair

        @user.event_handler("on_after_process_frame")
        async def processed(_processor, frame):
            if isinstance(frame, TranscriptionFrame):
                probe.transcripts_processed.append(frame)

        return pair

    bot.LLMContextAggregatorPair = capture_aggregators

    class CapturedRunner(WorkerRunner):
        def __init__(self, **kwargs):
            super().__init__(**kwargs)
            probe.runner = self

    def capture_main(*args, **kwargs):
        worker = PipelineWorker(*args, **kwargs)
        probe.main = worker

        @worker.event_handler("on_pipeline_started")
        async def started(_worker, _frame):
            probe.started.set()

        return worker

    bot.WorkerRunner = CapturedRunner
    bot.PipelineWorker = capture_main


def transcript(text, interim=False):
    cls = InterimTranscriptionFrame if interim else TranscriptionFrame
    kwargs = {} if interim else {"finalized": False}
    return cls(text, user_id="caller", timestamp="2026-09-09T00:00:00Z", **kwargs)


def assistant_words(capture):
    return "".join(
        item["text"]["separator_before"] + item["text"]["text"]
        for item in sorted(capture.text("assistant"), key=lambda item: item["order"])
    )


async def exercise(enabled):
    if enabled:
        os.environ["UNMUTE_DEV_METRICS"] = "1"
    else:
        os.environ.pop("UNMUTE_DEV_METRICS", None)
    probe = Probe()
    patch_providers(probe)
    capture = Capture()
    call_id = "pipecat-streaming-on" if enabled else "pipecat-streaming-off"
    args = RunnerArguments(body={"unmute_dev_call_id": call_id}, session_id=call_id)
    with redirect_stdout(capture):
        task = asyncio.create_task(bot.run_bot(FakeTransport(probe), args))
        try:
            await asyncio.wait_for(probe.started.wait(), timeout=5)
            await probe.main.rtvi.set_client_ready()
            await until(lambda: probe.syntheses, "direct greeting reached native TTS")
            greeting, release = probe.syntheses[0]
            if enabled:
                await until(lambda: greeting in assistant_words(capture), "direct greeting before audio")
                assert not probe.requests, "direct greeting invented a model request"
                assert not probe.audio, "greeting audio was not held"
            release.set()
            await until(lambda: probe.audio, "native greeting audio passed through")
            audio_before_answer = len(probe.audio)

            await probe.stt.push_frame(UserStartedSpeakingFrame())
            await probe.stt.push_frame(transcript("Turn lef", interim=True))
            if enabled:
                await until(lambda: any(r["text"]["text"] == "Turn lef" and r["text"]["state"] == "provisional" for r in capture.text("user")), "interim words")
            final = transcript("Turn left")
            observed_at = time.monotonic()
            await probe.stt.push_frame(final)
            assert final.finalized is False, "test must cover final segment before STT utterance finalization"
            # Turn-stop is a SystemFrame and can overtake queued transcript data.
            await until(lambda: final in probe.transcripts_processed, "native final processed")
            await probe.stt.push_frame(UserStoppedSpeakingFrame())
            await until(lambda: probe.requests, "model invoked by native user aggregator")
            request = probe.requests[0]
            if enabled:
                await until(lambda: any(r["text"]["text"] == "Turn left" and r["text"]["state"] == "final" for r in capture.text("user")), "final caller words while model held")
                assert time.monotonic() - observed_at < 0.25, "recognition finality was delayed"
                assert len(capture.text("user")) == 1, "interim correction duplicated the segment"
                # The requirement explicitly asks for a five-second held model.
                await asyncio.sleep(5)
                assert not request.first.is_set() and not request.finished.is_set()
                assert len(probe.audio) == audio_before_answer
            request.first.set()
            if enabled:
                await until(lambda: assistant_words(capture).endswith("Hel"), "raw answer fragment before TTS")
                assert len(probe.syntheses) == 1, "fragment unexpectedly reached sentence TTS"
                assert len(probe.audio) == audio_before_answer
            request.rest.set()
            await until(lambda: len(probe.syntheses) == 2, "native answer synthesis")
            answer, release = probe.syntheses[1]
            assert answer == "Hello there.", answer
            if enabled:
                await until(lambda: assistant_words(capture).endswith("Hello there."), "full answer while audio held")
                assert assistant_words(capture).count("Hello there.") == 1, "worker bridge duplicated answer text"
                assert PRIVATE_THOUGHT not in capture.getvalue(), "private thought leaked"
                assert len(probe.audio) == audio_before_answer
            release.set()
            await until(lambda: len(probe.audio) > audio_before_answer, "native answer audio")

            # Same words are a distinct final segment, with no prior interim.
            await probe.stt.push_frame(UserStartedSpeakingFrame())
            second_final = transcript("Turn left")
            await probe.stt.push_frame(second_final)
            await until(lambda: second_final in probe.transcripts_processed, "second native final processed")
            await probe.stt.push_frame(UserStoppedSpeakingFrame())
            await until(lambda: len(probe.requests) == 2, "second native model invocation")
            second = probe.requests[1]
            second.first.set()
            if enabled:
                await until(lambda: len([r for r in capture.text("user") if r["text"]["text"] == "Turn left" and r["text"]["state"] == "final"]) == 2, "identical distinct recognition segments")
                await until(lambda: assistant_words(capture).endswith("Hel"), "second answer begins")
            await probe.stt.push_frame(InterruptionFrame())
            await until(lambda: second.finished.is_set(), "native model cancellation")
            assert second.cancelled, "interruption did not cancel the real processor task"
            await probe.stt.push_frame(UserStartedSpeakingFrame())
            await probe.stt.push_frame(transcript("unfinished caller words", interim=True))
            if enabled:
                await until(lambda: any(r["text"]["text"] == "unfinished caller words" for r in capture.text("user")), "provisional words before disconnect")
        finally:
            if probe.runner:
                await probe.runner.cancel(reason="streaming smoke complete")
            await asyncio.wait_for(task, timeout=5)

    if enabled:
        records = capture.records()
        assert records and all(r.get("version") == 2 for r in records), records
        assert all(r["call_id"] == call_id for r in records), records
        assert any(r["text"]["text"] == "unfinished caller words" for r in capture.text("user")), "disconnect dropped visible caller words"
        assert PRIVATE_THOUGHT not in capture.getvalue()
        for line in capture.getvalue().splitlines():
            if line.startswith(SENTINEL):
                print(line)
    else:
        assert not capture.records(), "metrics disabled but frames were emitted"
        assert probe.requests[0].finished.is_set() and probe.audio


# Add this helper to devStreamingPipecatScript and call it from main() after
# exercise(False). It reuses that script's real generated run_bot infrastructure.
async def exercise_recognition_boundaries():
    for late_final in (False, True):
        os.environ["UNMUTE_DEV_METRICS"] = "1"
        probe = Probe()
        patch_providers(probe)
        # Real native stop strategy waits for the final recognizer segment.
        bot.LLMUserAggregatorParams = lambda **_kwargs: LLMUserAggregatorParams(
            user_turn_strategies=UserTurnStrategies(
                start=[ExternalUserTurnStartStrategy()],
                stop=[ExternalUserTurnStopStrategy(wait_for_transcript=True, timeout=0.05)],
            ),
        )
        stop_processed = asyncio.Event()
        original_pair = bot.LLMContextAggregatorPair

        def pair_with_stop_barrier(*args, **kwargs):
            pair = original_pair(*args, **kwargs)
            user, _assistant = pair

            @user.event_handler("on_after_process_frame")
            async def stop_seen(_processor, frame):
                if isinstance(frame, UserStoppedSpeakingFrame):
                    stop_processed.set()

            return pair

        bot.LLMContextAggregatorPair = pair_with_stop_barrier
        capture = Capture()
        call_id = "pipecat-late-final" if late_final else "pipecat-multi-final"
        args = RunnerArguments(body={"unmute_dev_call_id": call_id}, session_id=call_id)
        with redirect_stdout(capture):
            task = asyncio.create_task(bot.run_bot(FakeTransport(probe), args))
            try:
                await asyncio.wait_for(probe.started.wait(), timeout=5)
                await probe.main.rtvi.set_client_ready()
                await until(lambda: probe.syntheses, "boundary probe greeting")
                probe.syntheses[0][1].set()
                await until(lambda: probe.audio, "boundary probe greeting audio")
                await probe.stt.push_frame(UserStartedSpeakingFrame())
                if late_final:
                    await probe.stt.push_frame(UserStoppedSpeakingFrame())
                    await asyncio.wait_for(stop_processed.wait(), timeout=2)
                for words in ("I need", "a table"):
                    final = transcript(words)
                    await probe.stt.push_frame(final)
                    await until(lambda: final in probe.transcripts_processed, "boundary final consumed")
                rows = sorted(capture.text("user"), key=lambda r: r["order"])
                assert len(rows) == 2 and all(r["text"]["state"] == "final" for r in rows), rows
                assert len({r["text"]["message_id"] for r in rows}) == 1, rows
                assert len({r["text"].get("exchange_id") for r in rows}) == 1, rows
                assert rows[0]["text"].get("exchange_id"), rows
                assert "".join(r["text"]["separator_before"] + r["text"]["text"] for r in rows) == "I need a table", rows
                if not late_final:
                    await probe.stt.push_frame(UserStoppedSpeakingFrame())
                await until(lambda: probe.requests, "boundary probe native commit")
            finally:
                if probe.runner:
                    await probe.runner.cancel(reason="boundary smoke complete")
                await asyncio.wait_for(task, timeout=5)
        for line in capture.getvalue().splitlines():
            if line.startswith(SENTINEL):
                print(line)


# Append to devStreamingPipecatScript, then call await exercise_live_metrics()
# from main(). Reuses its generated bot, providers, captured stdout and barriers.
from pipecat.frames.frames import FunctionCallFromLLM, FunctionCallResultProperties, MetricsFrame
from pipecat.metrics.metrics import TTFBMetricsData


class ToolProbeLLM(FakeLLM):
    def __init__(self, probe):
        super().__init__(probe)
        self.tool_context = None
        self.called_tools = False

        async def tool(params):
            identity = params.tool_call_id
            probe.tool_started.add(identity)
            await params.result_callback(
                {"private_result": "tool-internal-progress"},
                properties=FunctionCallResultProperties(is_final=False, run_llm=False),
            )
            probe.tool_progress.add(identity)
            await probe.tool_release[identity].wait()
            await params.result_callback(
                {"private_result": "tool-internal-result"},
                properties=FunctionCallResultProperties(run_llm=False),
            )
            probe.tool_finished.add(identity)

        self.register_function("same_name_tool", tool, cancel_on_interruption=False)

        @self.event_handler("on_before_push_frame")
        async def call_tools(_processor, frame):
            if type(frame) is LLMTextFrame and not self.called_tools:
                self.called_tools = True
                await self.run_function_calls([
                    FunctionCallFromLLM(
                        function_name="same_name_tool", tool_call_id=identity,
                        arguments={"private_argument": "tool-internal-input"},
                        context=self.tool_context,
                    )
                    for identity in ("tool-a", "tool-b")
                ])

    async def process_frame(self, frame, direction):
        if isinstance(frame, LLMContextFrame):
            self.tool_context = frame.context
        await super().process_frame(frame, direction)


async def exercise_live_metrics(call_id="pipecat-live-metrics"):
    os.environ["UNMUTE_DEV_METRICS"] = "1"
    probe = Probe()
    probe.tool_started = set()
    probe.tool_progress = set()
    probe.tool_finished = set()
    probe.tool_release = {identity: asyncio.Event() for identity in ("tool-a", "tool-b")}
    patch_providers(probe)
    for name in vars(bot).copy():
        if name.startswith("build_") and name.endswith("_llm"):
            setattr(bot, name, lambda *_args, **_kwargs: ToolProbeLLM(probe))
    capture = Capture()
    args = RunnerArguments(body={"unmute_dev_call_id": call_id}, session_id=call_id)

    def measurements(metric):
        return [r["measurement"] for r in capture.latest("measurement") if r["measurement"]["metric"] == metric]

    def tool_operations():
        return [r for r in capture.latest("operation") if r["operation"]["type"] == "tool"]

    with redirect_stdout(capture):
        task = asyncio.create_task(bot.run_bot(FakeTransport(probe), args))
        try:
            await asyncio.wait_for(probe.started.wait(), timeout=5)
            await probe.main.rtvi.set_client_ready()
            await until(lambda: probe.syntheses, "metric probe native greeting")
            probe.syntheses[0][1].set()
            await until(lambda: probe.audio, "metric probe native greeting audio")
            await probe.stt.push_frame(UserStartedSpeakingFrame())
            final = transcript("Show work as it happens.")
            await probe.stt.push_frame(final)
            await until(lambda: final in probe.transcripts_processed, "metric probe caller final")
            await probe.stt.push_frame(UserStoppedSpeakingFrame())
            await until(lambda: probe.requests, "metric probe native model request")
            models = [r for r in capture.latest("operation") if r["operation"]["type"] == "llm"]
            assert len(models) == 1 and models[0]["operation"]["state"] == "running", models
            model_id = models[0]["id"]
            request = probe.requests[0]
            request.first.set()
            await until(lambda: len(probe.tool_progress) == 2, "two native same-name tools overlap after intermediate results")
            assert not request.rest.is_set() and not request.finished.is_set()
            # First expected red against US1: actual SDK TTFB is available now,
            # while model completion, both tool results and answer audio are held.
            await until(lambda: any(m.get("operation_id") == model_id and m["state"] == "measured" for m in measurements("first_response")), "native model first-response measurement before tools finish")
            assert not any(m.get("operation_id") == model_id and m["state"] == "measured" for m in measurements("request_duration"))
            await until(lambda: len(tool_operations()) == 2, "two identified native tool starts")
            tools = tool_operations()
            assert len({r["id"] for r in tools}) == 2, tools
            assert all(r["operation"]["name"] == "same_name_tool" and r["operation"]["state"] == "running" for r in tools), tools
            assert all(r["operation"].get("parent_operation_id") == model_id for r in tools), tools
            assert all(r["operation"].get("exchange_id") == models[0]["operation"]["exchange_id"] for r in tools), tools
            identities = {r["id"] for r in tools}
            probe.tool_release["tool-a"].set()
            await until(lambda: "tool-a" in probe.tool_finished, "first native result returned")
            await until(lambda: len([r for r in tool_operations() if r["operation"]["state"] == "returned"]) == 1, "first tool update before second tool returns")
            assert {r["id"] for r in tool_operations()} == identities
            assert any(m["state"] == "measured" for m in measurements("tool_duration"))
            assert "tool-b" not in probe.tool_finished
            probe.tool_release["tool-b"].set()
            await until(lambda: len(probe.tool_finished) == 2, "second native result returned")
            request.rest.set()
            await until(lambda: request.finished.is_set(), "native model ended")
            await until(lambda: any(m.get("operation_id") == model_id and m["state"] == "measured" for m in measurements("request_duration")), "native full model duration after first response")
            # All normal native timing APIs in this fixture elapsed some real time. The
            # worker's synthetic initialization zeros must not become measured values.
            assert not any(m["state"] == "measured" and m.get("value") == 0 for m in measurements("first_response"))
            first_value = next(m["value"] for m in measurements("first_response") if m.get("operation_id") == model_id and m["state"] == "measured")
            for ordinal in (2, 3):
                for _text, release in probe.syntheses:
                    release.set()
                await probe.stt.push_frame(UserStartedSpeakingFrame())
                final = transcript(f"Native request number {ordinal}.")
                await probe.stt.push_frame(final)
                await until(lambda: final in probe.transcripts_processed, "next caller segment consumed")
                await probe.stt.push_frame(UserStoppedSpeakingFrame())
                await until(lambda: len(probe.requests) == ordinal, "next native model invocation")
                models = sorted([r for r in capture.latest("operation") if r["operation"]["type"] == "llm"], key=lambda r: r["order"])
                assert len(models) == ordinal and len({r["id"] for r in models}) == ordinal, models
                next_model_id = models[-1]["id"]
                assert models[-1]["operation"]["state"] == "running"
                next_request = probe.requests[-1]
                next_request.first.set()
                await until(lambda: any(m.get("operation_id") == next_model_id and m["state"] == "measured" for m in measurements("first_response")), "next native first-response before completion")
                if ordinal == 3:
                    # This is an actual source push from a separate task with no captured
                    # invocation context, while a model request is still running. The
                    # measured zero must survive and must not attach to that latest call.
                    source = next(llm for llm in probe.llms if llm.name == models[-1]["operation"]["name"])
                    await source.push_frame(MetricsFrame(data=[TTFBMetricsData(processor=source.name, model="native-zero-probe", value=0.0)]))
                    await until(lambda: any(m.get("model") == "native-zero-probe" and m["state"] == "measured" for m in measurements("first_response")), "genuine source zero arrives")
                    zero = next(m for m in measurements("first_response") if m.get("model") == "native-zero-probe")
                    assert zero["value"] == 0 and zero["scope"] == "unassigned" and not zero.get("operation_id") and not zero.get("exchange_id"), zero
                next_request.rest.set()
                await until(lambda: next_request.finished.is_set(), "next native model completed")
                await until(lambda: any(m.get("operation_id") == next_model_id and m["state"] == "measured" for m in measurements("request_duration")), "next native full duration")
            assert next(m["value"] for m in measurements("first_response") if m.get("operation_id") == model_id and m["state"] == "measured") == first_value
            assert len([r for r in capture.latest("operation") if r["operation"]["type"] == "llm"]) == 3
            assert all(private not in capture.getvalue() for private in (PRIVATE_THOUGHT, "tool-internal-input", "tool-internal-progress", "tool-internal-result"))
        finally:
            for release in probe.tool_release.values():
                release.set()
            if probe.runner:
                await probe.runner.cancel(reason="metric smoke complete")
            await asyncio.wait_for(task, timeout=5)
    for line in capture.getvalue().splitlines():
        if line.startswith(SENTINEL):
            print(line)


import dev_metrics
from pipecat.frames.frames import ErrorFrame, TTSSpeakFrame


class HandoffProbeLLM(ToolProbeLLM):
    def __init__(self, probe):
        super().__init__(probe)
        self.handoff_requested = False

        @self.event_handler("on_before_push_frame")
        async def handoff(_processor, frame):
            if type(frame) is LLMTextFrame and not self.handoff_requested:
                self.handoff_requested = True
                await self.run_function_calls([FunctionCallFromLLM(
                    function_name="to_billing", tool_call_id="native-handoff-id",
                    arguments={}, context=self.tool_context,
                )])


async def exercise_native_handoff():
    if not hasattr(bot, "build_intake_llm"):
        return
    os.environ["UNMUTE_DEV_METRICS"] = "1"
    probe = Probe()
    probe.tool_started, probe.tool_progress, probe.tool_finished = set(), set(), set()
    probe.tool_release = {identity: asyncio.Event() for identity in ("tool-a", "tool-b")}
    patch_providers(probe)
    bot.build_intake_llm = lambda *_args, **_kwargs: HandoffProbeLLM(probe)
    capture = Capture()
    call_id = "pipecat-native-handoff"
    args = RunnerArguments(body={"unmute_dev_call_id": call_id}, session_id=call_id)
    with redirect_stdout(capture):
        task = asyncio.create_task(bot.run_bot(FakeTransport(probe), args))
        try:
            await asyncio.wait_for(probe.started.wait(), timeout=5)
            await probe.main.rtvi.set_client_ready()
            await until(lambda: probe.syntheses, "handoff greeting")
            probe.syntheses[0][1].set()
            await until(lambda: probe.audio, "handoff greeting audio")
            await probe.stt.push_frame(UserStartedSpeakingFrame())
            final = transcript("Please transfer to billing.")
            await probe.stt.push_frame(final)
            await until(lambda: final in probe.transcripts_processed, "handoff final consumed")
            await probe.stt.push_frame(UserStoppedSpeakingFrame())
            await until(lambda: probe.requests, "handoff originating model")
            original = next(r for r in capture.latest("operation") if r["operation"]["type"] == "llm")
            probe.requests[0].first.set()
            await until(lambda: len(probe.tool_progress) == 2, "held native old tools")
            old_tools = {r["id"]: r["operation"] for r in capture.latest("operation") if r["operation"]["type"] == "tool"}
            probe.requests[0].rest.set()
            await until(lambda: len(probe.syntheses) > 1, "handoff source response reaches TTS")
            for _text, release in probe.syntheses:
                release.set()
            # Invokes the actual generated @tool method through LLMService. No
            # worker context or activation method is replaced to make it pass.
            await until(lambda: any(r["operation"]["type"] == "llm" and r["operation"]["name"] != original["operation"]["name"] for r in capture.latest("operation")), "native generated to_billing activates receiver", timeout=5)
            models = [r for r in capture.latest("operation") if r["operation"]["type"] == "llm"]
            receiver = models[-1]
            assert receiver["id"] != original["id"]
            assert receiver["operation"]["name"] != original["operation"]["name"]
            assert receiver["operation"].get("exchange_id") == original["operation"]["exchange_id"], models
            assert len(old_tools) == 2 and all(t.get("parent_operation_id") == original["id"] for t in old_tools.values())
            # Both original native tasks finish only after another worker's
            # request is active. Their records must still update the old owner.
            for release in probe.tool_release.values():
                release.set()
            await until(lambda: len(probe.tool_finished) == 2, "late old-worker results return")
            tools = {r["id"]: r["operation"] for r in capture.latest("operation") if r["operation"]["type"] == "tool"}
            assert set(tools) == set(old_tools), tools
            for identity, previous in old_tools.items():
                assert tools[identity]["state"] == "returned"
                assert tools[identity].get("parent_operation_id") == previous.get("parent_operation_id")
                assert tools[identity].get("exchange_id") == previous.get("exchange_id")
            assert not any(r["operation"].get("name") == "to_billing" for r in capture.latest("operation"))
        finally:
            for release in probe.tool_release.values():
                release.set()
            if probe.runner:
                await probe.runner.cancel(reason="handoff smoke complete")
            await asyncio.wait_for(task, timeout=5)
    for line in capture.getvalue().splitlines():
        if line.startswith(SENTINEL):
            print(line)


def event_handlers(source, event):
    # Test-only SDK inspection: the production path uses public remove_event_handler.
    return list(source._event_handlers[event].handlers)


async def exercise_native_lifecycle():
    original_install = bot.install_dev_metrics
    for terminal in ("end", "cancel", "error", "cancel-task"):
        os.environ["UNMUTE_DEV_METRICS"] = "1"
        probe, capture = Probe(), Capture()
        patch_providers(probe)

        def install(args):
            probe.reporter = original_install(args)
            return probe.reporter

        bot.install_dev_metrics = install
        call_id = f"pipecat-lifecycle-{terminal}"
        args = RunnerArguments(body={"unmute_dev_call_id": call_id}, session_id=call_id)
        with redirect_stdout(capture):
            task = asyncio.create_task(bot.run_bot(FakeTransport(probe), args))
            try:
                await asyncio.wait_for(probe.started.wait(), timeout=5)
                await probe.main.rtvi.set_client_ready()
                await until(lambda: probe.syntheses, "lifecycle greeting")
                probe.syntheses[0][1].set()
                await until(lambda: probe.audio, "lifecycle greeting audio")
                await probe.stt.push_frame(UserStartedSpeakingFrame())
                final = transcript("Keep these final words.")
                await probe.stt.push_frame(final)
                await until(lambda: final in probe.transcripts_processed, "lifecycle final consumed")
                await probe.stt.push_frame(UserStoppedSpeakingFrame())
                await until(lambda: probe.requests, "lifecycle native request")
                probe.requests[0].first.set()
                await until(lambda: assistant_words(capture).endswith("Hel"), "lifecycle partial answer")
                measured_before = {r["id"]: r["measurement"] for r in capture.latest("measurement") if r["measurement"]["state"] == "measured"}
                late_callbacks = event_handlers(probe.stt, "on_before_push_frame")
                if terminal == "end":
                    probe.requests[0].rest.set()
                    await until(lambda: len(probe.syntheses) > 1, "normal end answer reaches TTS")
                    for _text, release in probe.syntheses:
                        release.set()
                    await probe.runner.end(reason="normal native end")
                elif terminal == "error":
                    await probe.stt.push_error("private-fatal-error-payload", fatal=True)
                elif terminal == "cancel-task":
                    task.cancel()
                else:
                    await probe.runner.cancel(reason="native cancel")
                try:
                    await asyncio.wait_for(task, timeout=5)
                except asyncio.CancelledError:
                    assert terminal == "cancel-task", "reporter changed native cancellation"
                # Native WorkerRunner.run catches cancellation, then closes its workers.
                if terminal == "cancel-task":
                    assert probe.requests[0].cancelled, "native model task was not cancelled"
                calls = capture.latest("call")
                assert calls[-1]["call"]["state"] == ("error" if terminal == "error" else "ended"), calls
                assert any(r["text"]["text"] == final.text and r["text"]["state"] == "final" for r in capture.text("user"))
                if terminal != "end":
                    assert any(r["text"]["text"] == "Hel" and r["text"]["state"] == "incomplete" for r in capture.text("assistant"))
                for identity, value in measured_before.items():
                    assert next(r["measurement"] for r in capture.latest("measurement") if r["id"] == identity) == value
                assert not any(r["operation"]["state"] == "running" for r in capture.latest("operation"))
                count = len(capture.records())
                # A callback scheduled before closure can still arrive afterwards.
                # Capturing it here is test instrumentation, not an adapter path.
                for callback in late_callbacks:
                    await callback(probe.stt, transcript("late-old-call-words", interim=True))
                probe.reporter.finish()
                assert len(capture.records()) == count, "closed reporter emitted late records"
                assert not probe.reporter._interim and not probe.reporter._text_spacing, "closed hook rebuilt call state"
                assert not any(callback in event_handlers(probe.stt, "on_before_push_frame") for callback in late_callbacks), "native hooks were not detached"
                assert not probe.reporter.observers(), "closed reporter retained its observer"
                assert probe.reporter._scope.get() is None, "closed reporter retained task-local cause"
                assert "private-fatal-error-payload" not in capture.getvalue()
            finally:
                if probe.runner:
                    await probe.runner.cancel(reason="lifecycle cleanup")
                if not task.done():
                    await asyncio.wait_for(task, timeout=5)
                bot.install_dev_metrics = original_install
        for line in capture.getvalue().splitlines():
            if line.startswith(SENTINEL):
                print(line)

    # Reuse native tool-call IDs on a new reporter/call, with a late callback
    # from the previous call still retained above. IDs are scoped by call_id.
    await exercise_live_metrics(call_id="pipecat-repeated-native-tool-ids")

    class BrokenOutput(io.StringIO):
        def write(self, _value):
            raise BrokenPipeError("private-output-error-payload")

    probe = Probe()
    patch_providers(probe)
    args = RunnerArguments(body={"unmute_dev_call_id": "pipecat-broken-output"})
    with redirect_stdout(BrokenOutput()):
        task = asyncio.create_task(bot.run_bot(FakeTransport(probe), args))
        try:
            await asyncio.wait_for(probe.started.wait(), timeout=5)
            await probe.main.rtvi.set_client_ready()
            await until(lambda: probe.syntheses, "broken dev output must not block native greeting")
            probe.syntheses[0][1].set()
            await until(lambda: probe.audio, "broken dev output must not block native audio")
        finally:
            if probe.runner:
                await probe.runner.cancel(reason="broken output cleanup")
            await asyncio.wait_for(task, timeout=5)


async def main():
    await exercise(True)
    await exercise(False)
    await exercise_recognition_boundaries()
    await exercise_live_metrics()
    await exercise_native_handoff()
    await exercise_native_lifecycle()


asyncio.run(main())
print("pipecat generated worker streaming smoke passed")
`
