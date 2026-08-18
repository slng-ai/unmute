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

const livekitRegionalSmokeScript = `"""Smoke check: the installed SLNG plugin accepts both regional controls."""
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import agent  # noqa: F401, E402
from livekit.plugins import slng  # noqa: E402

stt = slng.STT(
    api_key=os.environ["SLNG_API_KEY"],
    model="slng/deepgram/nova:3-multi",
    world_part_override="eu",
)
tts = slng.TTS(
    api_key=os.environ["SLNG_API_KEY"],
    model="slng/deepgram/aura:2-en",
    voice="aura-2-thalia-en",
    region_override="eu-north-1",
    world_part_override="eu",
)
assert type(stt).__name__ == "STT"
assert type(tts).__name__ == "TTS"
print("regional SLNG smoke ok")
`

// This runs against the exact livekit-agents pin emitted by the generator. It
// proves the two SDK contracts the task lowering depends on: TaskGroup merges
// one task's native finish call/output before starting the next task, and
// propagates an exception result without starting later tasks.
const livekitTaskGroupContractSmokeScript = `"""Smoke check: task-group result sharing and transfer propagation."""
import asyncio
import json
import os
import subprocess
from types import SimpleNamespace

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

subprocess.run(["ruff", "check", "."], check=True)
subprocess.run(["ty", "check", "agent.py"], check=True)

import agent  # noqa: E402
from livekit.agents import llm  # noqa: E402
from livekit.agents.beta.workflows import TaskGroup  # noqa: E402


class FakeTask:
    def __init__(self, result=None, error=None, observed=None):
        self.result = result
        self.error = error
        self.observed = observed
        self.chat_ctx = llm.ChatContext.empty()
        self.tools = []

    async def update_chat_ctx(self, chat_ctx):
        self.chat_ctx = chat_ctx
        if self.observed is not None:
            self.observed.extend(chat_ctx.items)

    async def update_tools(self, tools):
        self.tools = tools

    def __await__(self):
        async def run():
            if self.error is not None:
                raise self.error
            if self.result is not None:
                exact = json.dumps(self.result, sort_keys=True)
                call_id = "task-result-" + str(id(self))
                self.chat_ctx.insert(
                    [
                        llm.FunctionCall(
                            call_id=call_id,
                            name="finish",
                            arguments=exact,
                        ),
                        llm.FunctionCallOutput(
                            call_id=call_id,
                            name="finish",
                            output=exact,
                            is_error=False,
                        ),
                    ]
                )
            return self.result

        return run().__await__()


class RecordingFindSlot(agent.FindSlot):
    def __init__(self):
        super().__init__()
        self.completions = []

    def complete(self, result):
        self.completions.append(result)


class RecordingTaskGroup(TaskGroup):
    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        self.completions = []

    def complete(self, result):
        self.completions.append(result)


class BlockingSession:
    def __init__(self):
        self.started = asyncio.Event()
        self.release = asyncio.Event()
        self.announcements = 0

    async def say(self, *args, **kwargs):
        self.announcements += 1
        self.started.set()
        await self.release.wait()


class FailingSession:
    async def say(self, *args, **kwargs):
        raise RuntimeError("announcement failed")


async def main():
    class RefuseSession:
        async def say(self, *args, **kwargs):
            raise AssertionError("an empty required value reached the announcement")

    guard_task = RecordingFindSlot()
    guard_ctx = SimpleNamespace(
        userdata=SimpleNamespace(caller_phone=""),
        session=RefuseSession(),
    )
    refusal = await guard_task.back_to_greeter(guard_ctx)
    assert refusal == "Cannot transfer yet; missing required information: caller_phone"
    assert not guard_task.completions
    await guard_task.finish(
        guard_ctx,
        date="2026-08-18",
        party_size=2,
        time="15:00",
    )
    assert guard_task.completions == [
        {"date": "2026-08-18", "party_size": 2, "time": "15:00"}
    ]

    transfer_session = BlockingSession()
    transfer_ctx = SimpleNamespace(
        userdata=SimpleNamespace(caller_phone="+15551234567"),
        session=transfer_session,
    )
    transfer_first = RecordingFindSlot()
    pending_transfer = asyncio.create_task(
        transfer_first.back_to_greeter(transfer_ctx)
    )
    await transfer_session.started.wait()
    await transfer_first.finish(
        transfer_ctx,
        date="2026-08-18",
        party_size=2,
        time="15:00",
    )
    assert not transfer_first.completions
    transfer_session.release.set()
    await pending_transfer
    assert len(transfer_first.completions) == 1
    assert isinstance(transfer_first.completions[0], agent._TaskTransfer)

    finish_session = BlockingSession()
    finish_ctx = SimpleNamespace(
        userdata=SimpleNamespace(caller_phone="+15551234567"),
        session=finish_session,
    )
    finish_first = RecordingFindSlot()
    await finish_first.finish(
        finish_ctx,
        date="2026-08-18",
        party_size=2,
        time="15:00",
    )
    await finish_first.back_to_greeter(finish_ctx)
    assert finish_first.completions == [
        {"date": "2026-08-18", "party_size": 2, "time": "15:00"}
    ]
    assert finish_session.announcements == 0

    failed_ctx = SimpleNamespace(
        userdata=SimpleNamespace(caller_phone="+15551234567"),
        session=FailingSession(),
    )
    failed_transfer = RecordingFindSlot()
    try:
        await failed_transfer.back_to_greeter(failed_ctx)
    except RuntimeError as error:
        assert str(error) == "announcement failed"
    else:
        raise AssertionError("failed announcement did not escape")
    await failed_transfer.finish(
        failed_ctx,
        date="2026-08-18",
        party_size=2,
        time="15:00",
    )
    assert failed_transfer.completions == [
        {"date": "2026-08-18", "party_size": 2, "time": "15:00"}
    ]

    observed = []

    group = TaskGroup(
        chat_ctx=llm.ChatContext.empty(),
        summarize_chat_ctx=False,
    )
    group.add(lambda: FakeTask({"time": "15:00", "date": "2026-08-18"}), id="find_slot", description="Find slot")
    group.add(lambda: FakeTask({"confirmed": True}, observed=observed), id="confirm", description="Confirm")
    await group.on_enter()
    exact = "{\"date\": \"2026-08-18\", \"time\": \"15:00\"}"
    assert any(
        isinstance(item, llm.FunctionCall)
        and item.name == "finish"
        and item.arguments == exact
        for item in observed
    )
    assert any(
        isinstance(item, llm.FunctionCallOutput)
        and item.name == "finish"
        and item.output == exact
        for item in observed
    )
    assert all(
        not isinstance(item, llm.ChatMessage)
        or item.role not in ("developer", "system")
        for item in observed
    )

    second_started = []
    transfer = agent._TaskTransfer(agent.Greeter())
    stopped = RecordingTaskGroup(
        chat_ctx=llm.ChatContext.empty(),
        summarize_chat_ctx=False,
    )
    stopped.add(lambda: FakeTask(error=transfer), id="transfer", description="Transfer")
    stopped.add(lambda: second_started.append(True) or FakeTask({}), id="later", description="Must not start")
    await stopped.on_enter()
    assert not second_started
    assert stopped.completions == [transfer]

    print("livekit task-group contract smoke ok")


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

func TestSmokeLiveKitRegionalInfrastructureInstantiates(t *testing.T) {
	runLiveKitSmokeScript(t, "regional-infrastructure", nil, nil, livekitRegionalSmokeScript)
}

func addLiveKitTaskTransfer(agent *ir.Agent) {
	transfer := agent.Controls["back_to_greeter"].(*ir.AgentTransfer)
	transfer.Announce = "I will take you back to Remy."
	transfer.Requires = []string{"caller_phone"}
	task := agent.Tasks["find_slot"]
	task.Tools = append(task.Tools, "back_to_greeter")
	agent.Tasks["find_slot"] = task
}

func TestSmokeLiveKitV1TaskGroupContracts(t *testing.T) {
	runLiveKitSmokeScript(t, "remy", func(target *ir.Target) {
		target.Version = "1.6.10"
	}, addLiveKitTaskTransfer, livekitTaskGroupContractSmokeScript)
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
	pkg, err := spec.Load(examplePackagePath(example))
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
