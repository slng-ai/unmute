//go:build smoke

package generate

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

// consoleCheckScript proves the console extra (T8, V8): after `uv run --extra
// console`, pyaudio is installed (importing the local-audio transport runs its
// `import pyaudio` guard), bot.py imports, and console_main is present. It does
// not construct the transport — that opens the audio subsystem, which a headless
// runner has no device for; resolve + import is what V8 asks.
const consoleCheckScript = `"""Smoke check: the console extra resolves and bot.py imports with it."""
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from pipecat.transports.local.audio import LocalAudioTransport  # noqa: E402,F401  (imports pyaudio)

assert callable(bot.console_main), "console_main missing from bot.py"
print("console extra ok")
`

// TestSmokePipecatV1ConsoleExtraResolves (T8, V8): `uv run --extra console`
// resolves the emitted pyproject including pipecat-ai[local] (pyaudio) and the
// bot imports with the local-audio transport. Skips cleanly when portaudio is
// absent (pyaudio can't build) — the console prerequisite, not a driver bug.
func TestSmokePipecatV1ConsoleExtraResolves(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, file := range artifact.Files {
		out := filepath.Join(dir, file.Path)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, file.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "console_check.py"), []byte(consoleCheckScript), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("uv", "run", "--extra", "console", "python", "console_check.py")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		lower := strings.ToLower(string(out))
		if strings.Contains(lower, "portaudio") || strings.Contains(lower, "pyaudio") {
			t.Skipf("console extra needs portaudio (pyaudio could not build); skipping:\n%s", out)
		}
		t.Fatalf("console extra smoke failed:\n%s", out)
	}
	if !bytes.Contains(out, []byte("console extra ok")) {
		t.Fatalf("unexpected console smoke output:\n%s", out)
	}
}

// smokeCheckScript imports the emitted bot and instantiates every service
// builder with placeholder env values. Importing alone proves the imports and
// dependency set; calling the builders proves the constructor kwargs against
// the real installed services — the drift class py_compile can never see
// (driver-pipecat B6).
const smokeCheckScript = `"""Smoke check: import the generated bot and instantiate every service."""
import asyncio
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402  (module import already constructs the agent workers)

for name in ("LANGFUSE_SECRET_KEY", "LANGFUSE_PUBLIC_KEY", "LANGFUSE_BASE_URL"):
    os.environ.pop(name, None)
assert bot.setup_langfuse_tracing() is False
os.environ["LANGFUSE_PUBLIC_KEY"] = "partial"
try:
    bot.setup_langfuse_tracing()
except ValueError:
    pass
else:
    raise AssertionError("partial Langfuse configuration must fail")
finally:
    os.environ.pop("LANGFUSE_PUBLIC_KEY")

builders = sorted(n for n in vars(bot) if n.startswith("build_") and callable(getattr(bot, n)))
assert builders, "no service builders found in bot.py"


async def _run() -> None:  # some services construct an aiohttp session
    for name in builders:
        getattr(bot, name)()


asyncio.run(_run())
print("smoke ok:", ", ".join(builders))
`

// pipecatRequestTracingSmokeScript drives the generated worker/bus topology
// through deterministic STT, LLM, and TTS services. V17 requires all three
// request spans to share the conversation trace.
const pipecatRequestTracingSmokeScript = `"""Smoke check: a real worker turn emits nested speech and LLM spans."""
import asyncio
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from opentelemetry import trace  # noqa: E402
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter  # noqa: E402
from pipecat.bus import BusBridgeProcessor  # noqa: E402
from pipecat.frames.frames import (  # noqa: E402
    Frame,
    InputAudioRawFrame,
    LLMContextFrame,
    LLMFullResponseEndFrame,
    LLMFullResponseStartFrame,
    LLMTextFrame,
    TTSAudioRawFrame,
    TTSStoppedFrame,
    TranscriptionFrame,
)
from pipecat.pipeline.pipeline import Pipeline  # noqa: E402
from pipecat.pipeline.worker import PipelineParams, PipelineWorker  # noqa: E402
from pipecat.processors.aggregators.llm_context import LLMContext  # noqa: E402
from pipecat.processors.aggregators.llm_response_universal import LLMContextAggregatorPair  # noqa: E402
from pipecat.processors.frame_processor import FrameDirection, FrameProcessor  # noqa: E402
from pipecat.services.llm_service import LLMService  # noqa: E402
from pipecat.services.settings import LLMSettings, STTSettings, TTSSettings  # noqa: E402
from pipecat.services.stt_service import STTService  # noqa: E402
from pipecat.services.tts_service import TTSService  # noqa: E402
from pipecat.utils.tracing.service_decorators import traced_llm, traced_stt, traced_tts  # noqa: E402
from pipecat.utils.tracing.setup import setup_tracing  # noqa: E402
from pipecat.workers.llm import LLMWorkerActivationArgs  # noqa: E402
from pipecat.workers.runner import WorkerRunner  # noqa: E402


class FakeLLM(LLMService):
    def __init__(self) -> None:
        super().__init__(
            settings=LLMSettings(
                model="probe-model",
                system_instruction=None,
                temperature=None,
                max_tokens=None,
                top_p=None,
                top_k=None,
                frequency_penalty=None,
                presence_penalty=None,
                seed=None,
                filter_incomplete_user_turns=False,
                user_turn_completion_config=None,
            )
        )

    @traced_llm
    async def _process_context(self, context: LLMContext) -> None:
        await self.push_frame(LLMTextFrame("traced."))

    async def process_frame(self, frame: Frame, direction: FrameDirection) -> None:
        await super().process_frame(frame, direction)
        if isinstance(frame, LLMContextFrame):
            await self.push_frame(LLMFullResponseStartFrame())
            await self._process_context(frame.context)
            await self.push_frame(LLMFullResponseEndFrame())
        else:
            await self.push_frame(frame, direction)


class FakeSTT(STTService):
    def __init__(self) -> None:
        super().__init__(
            audio_passthrough=False,
            sample_rate=16000,
            settings=STTSettings(model="probe-stt", language="en"),
        )

    @traced_stt
    async def run_stt(self, audio: bytes):
        yield TranscriptionFrame(
            "trace this request",
            user_id="probe-user",
            timestamp="2026-07-20T00:00:00Z",
            finalized=True,
        )


class FakeTTS(TTSService):
    def __init__(self) -> None:
        super().__init__(
            push_start_frame=True,
            push_stop_frames=True,
            push_text_frames=False,
            sample_rate=16000,
            settings=TTSSettings(model="probe-tts", voice="probe-voice", language="en"),
        )

    @traced_tts
    async def run_tts(self, text: str, context_id: str):
        yield TTSAudioRawFrame(
            audio=b"\x00\x00" * 160,
            sample_rate=16000,
            num_channels=1,
            context_id=context_id,
        )


class Passthrough(FrameProcessor):
    async def process_frame(self, frame: Frame, direction: FrameDirection) -> None:
        await super().process_frame(frame, direction)
        await self.push_frame(frame, direction)


class StopAfterSpeech(Passthrough):
    def __init__(self, runner: WorkerRunner) -> None:
        super().__init__()
        self.runner = runner

    async def process_frame(self, frame: Frame, direction: FrameDirection) -> None:
        await super().process_frame(frame, direction)
        if isinstance(frame, TTSStoppedFrame):
            asyncio.create_task(self.runner.cancel(reason="speech traced"))


async def main() -> None:
    memory = InMemorySpanExporter()
    assert setup_tracing(exporter=memory)
    provider = trace.get_tracer_provider()

    bot.build_welcome_agent_llm = FakeLLM
    bot.build_welcome_agent_tts = FakeTTS

    runner = WorkerRunner()
    context = LLMContext()
    user_aggregator, assistant_aggregator = LLMContextAggregatorPair(context)
    main_worker = PipelineWorker(
        Pipeline(
            [
                FakeSTT(),
                user_aggregator,
                BusBridgeProcessor(bus=runner.bus, worker_name="trace-main"),
                StopAfterSpeech(runner),
                assistant_aggregator,
            ]
        ),
        name="trace-main",
        enable_tracing=True,
        params=PipelineParams(enable_metrics=True),
    )
    request_agent = bot.WelcomeAgentAgent()
    bot._enable_agent_tracing(main_worker, [request_agent])

    @main_worker.event_handler("on_pipeline_started")
    async def on_pipeline_started(worker, frame):
        await main_worker.queue_frame(
            InputAudioRawFrame(
                audio=b"\x00\x00" * 160,
                sample_rate=16000,
                num_channels=1,
            )
        )
        await main_worker.activate_worker(
            request_agent.name,
            args=LLMWorkerActivationArgs(
                messages=[{"role": "user", "content": "trace this request"}],
                run_llm=True,
            ),
        )

    await runner.add_workers(main_worker, request_agent)
    await asyncio.wait_for(runner.run(), timeout=10)
    provider.force_flush()

    spans = memory.get_finished_spans()
    conversation = next(span for span in spans if span.name == "conversation")
    requests = {span.name: span for span in spans if span.name in {"stt", "llm", "tts"}}
    assert requests.keys() == {"stt", "llm", "tts"}
    assert requests["stt"].attributes["gen_ai.request.model"] == "probe-stt"
    assert requests["llm"].attributes["gen_ai.request.model"] == "probe-model"
    assert requests["tts"].attributes["gen_ai.request.model"] == "probe-tts"
    assert all(span.context.trace_id == conversation.context.trace_id for span in requests.values())
    assert all(span.end_time > span.start_time for span in requests.values())
    print("pipecat speech tracing smoke ok")


asyncio.run(main())
`

// TestSmokePipecatV1ServicesInstantiate proves the safe_core emission end to
// end (V9, L4): uv resolves the emitted pyproject (network), bot.py imports,
// and every emitted service constructor accepts its emitted kwargs
// (deepgram Settings-style STT, slng flat-kwargs TTS, openai Settings LLM).
// Opt-in (`make smoke` / -tags smoke), never in the default suite.
func TestSmokePipecatV1ServicesInstantiate(t *testing.T) {
	runPipecatSmoke(t, "safe_core", nil, nil)
}

// TestSmokePipecatV1TaskGroupsInstantiate imports the generated FlowManager
// lowering used by both standalone tasks and task groups.
func TestSmokePipecatV1TaskGroupsInstantiate(t *testing.T) {
	runPipecatSmoke(t, "task-groups", nil, nil)
}

// TestSmokePipecatV1MultiVendorInstantiates covers the remaining official
// entries in one venv: assemblyai listen, elevenlabs + cartesia speak.
func TestSmokePipecatV1MultiVendorInstantiates(t *testing.T) {
	runPipecatSmoke(t, "safe_core", func(tgt *ir.Target) {
		tgt.Models.Listen = &ir.Binding{Provider: "assemblyai", Model: "universal-3-5-pro"}
		tgt.Models.Speak["front_desk"] = ir.Binding{
			Provider: "elevenlabs", Model: "eleven_multilingual_v2", Voice: "21m00Tcm4TlvDq8ikWAM",
		}
		tgt.Models.Speak["specialist"] = ir.Binding{
			Provider: "cartesia", Model: "sonic-3", Voice: "f786b574-daa5-4673-aa0c-cbe3e8534c02",
		}
	}, nil)
}

// TestSmokePipecatV1RestoredVendorsInstantiates covers the riskiest of the
// T13-restored entries in one venv: soniox listen, inworld + rime speak, and
// anthropic reason (the workers driver injects system_instruction into its
// Settings). Constructor kwargs are checked against the real packages.
// (speechmatics speak was smoke-rejected here 2026-07-17: its service demands
// a caller-supplied aiohttp_session, impossible at module import — T13.)
func TestSmokePipecatV1RestoredVendorsInstantiates(t *testing.T) {
	runPipecatSmoke(t, "safe_core", func(tgt *ir.Target) {
		tgt.Models.Listen = &ir.Binding{Provider: "soniox", Model: "stt-rt-v5"}
		tgt.Models.Speak["front_desk"] = ir.Binding{
			Provider: "inworld", Model: "inworld-tts-2", Voice: "Ashley",
		}
		tgt.Models.Speak["specialist"] = ir.Binding{
			Provider: "rime", Model: "mistv2", Voice: "cove",
		}
		tgt.Models.Reason["fast_reasoning"] = ir.Binding{
			Provider: "anthropic", Model: "claude-sonnet-4-6",
		}
	}, nil)
}

// TestSmokePipecatV1LocalToolInstantiates proves the local-tool lowering (T14,
// V13) against real pipecat-ai: importing bot constructs the agent workers, so
// the @tool wrapper class-collects and `import tools.fetch_notes` resolves the
// copied handler file inside the venv.
func TestSmokePipecatV1LocalToolInstantiates(t *testing.T) {
	runPipecatSmoke(t, "safe_core", nil, func(agent *ir.Agent) {
		agent.Tools["fetch_notes"] = ir.Tool{
			Description: "Fetch the caller's saved notes.",
			Input:       map[string]any{"type": "object", "properties": map[string]any{"topic": map[string]any{"type": "string"}}, "required": []any{"topic"}},
			Execution:   ir.ToolLocal, Handler: "tools/fetch_notes.py",
			HandlerSource: "def fetch_notes(topic):\n    return {\"notes\": []}\n",
			Interruption:  ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
		}
		intake := agent.Agents["intake"]
		intake.Tools = append(intake.Tools, "fetch_notes")
		agent.Agents["intake"] = intake
	})
}

// TestSmokeV17PipecatSpeechTracing proves the 1.5 worker compatibility shim
// sends STT, LLM, and TTS requests into the main conversation trace.
func TestSmokeV17PipecatSpeechTracing(t *testing.T) {
	runPipecatSmokeScript(t, "simple-prompt", nil, nil, pipecatRequestTracingSmokeScript)
}

func runPipecatSmoke(t *testing.T, example string, mutate func(*ir.Target), mutateAgent func(*ir.Agent)) {
	t.Helper()
	runPipecatSmokeScript(t, example, mutate, mutateAgent, smokeCheckScript)
}

func runPipecatSmokeScript(t *testing.T, example string, mutate func(*ir.Target), mutateAgent func(*ir.Agent), script string) {
	t.Helper()
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", example))
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
	tgt := targetByProvider(t, agent, ir.ProviderPipecat)
	if mutate != nil {
		mutate(&tgt)
	}
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	for _, file := range artifact.Files {
		out := filepath.Join(dir, file.Path)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, file.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "smoke_check.py"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	// uv resolves the emitted pyproject into a project venv (shared uv cache,
	// so repeat runs are fast) and runs the check inside it.
	cmd := exec.Command("uv", "run", "python", "smoke_check.py")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("smoke check failed:\n%s", out)
	} else {
		t.Logf("%s", out)
	}
}
