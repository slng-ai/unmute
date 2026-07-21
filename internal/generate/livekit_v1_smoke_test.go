//go:build smoke

package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
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
	runLiveKitSmokeScript(t, "simple-prompt", nil, livekitRequestTracingSmokeScript)
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
		{name: "single-task"},
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
			for _, args := range [][]string{{"run", "ruff", "check", "."}, {"run", "ty", "check", "."}} {
				cmd := exec.Command("uv", args...)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("uv %v failed:\n%s", args, out)
				}
			}
		})
	}
}

func runLiveKitSmoke(t *testing.T, example string, mutate func(*ir.Target)) {
	t.Helper()
	runLiveKitSmokeScript(t, example, mutate, livekitSmokeScript)
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

func runLiveKitSmokeScript(t *testing.T, example string, mutate func(*ir.Target), script string) {
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
