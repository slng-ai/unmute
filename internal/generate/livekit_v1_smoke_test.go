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

for name in ("LANGFUSE_SECRET_KEY", "LANGFUSE_PUBLIC_KEY", "LANGFUSE_BASE_URL"):
    os.environ.pop(name, None)
assert agent_module.setup_langfuse() is None
os.environ["LANGFUSE_PUBLIC_KEY"] = "partial"
try:
    agent_module.setup_langfuse()
except ValueError:
    pass
else:
    raise AssertionError("partial Langfuse configuration must fail")
finally:
    os.environ.pop("LANGFUSE_PUBLIC_KEY")

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

// livekitRequestTracingSmokeScript drives a real AgentSession turn with a
// deterministic fake LLM. The in-memory exporter must receive the framework's
// request spans; a hand-made root span would not satisfy V21.
const livekitRequestTracingSmokeScript = `"""Smoke check: a real agent turn emits request spans."""
import asyncio

import agent
from livekit.agents import Agent, AgentSession, DEFAULT_API_CONNECT_OPTIONS, llm
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


class ProbeAgent(Agent):
    def __init__(self) -> None:
        super().__init__(instructions=agent.WELCOME_AGENT_PROMPT)


async def main() -> None:
    memory = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(memory))
    set_tracer_provider(provider)

    async with AgentSession(llm=FakeLLM()) as session:
        await session.start(ProbeAgent())
        await session.run(user_input="trace this request")

    spans = memory.get_finished_spans()
    request = next(span for span in spans if span.name == "llm_request")
    node = next(span for span in spans if span.name == "llm_node")
    assert request.attributes["gen_ai.request.model"] == "probe-model"
    assert request.context.trace_id == node.context.trace_id
    print("livekit request tracing smoke ok")


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

// TestSmokeV21LiveKitRequestTracing proves request instrumentation, not only
// exporter connectivity: AgentSession.run must emit the LLM node and request.
func TestSmokeV21LiveKitRequestTracing(t *testing.T) {
	runLiveKitSmokeScript(t, "simple-prompt", nil, livekitRequestTracingSmokeScript)
}

func runLiveKitSmoke(t *testing.T, example string, mutate func(*ir.Target)) {
	t.Helper()
	runLiveKitSmokeScript(t, example, mutate, livekitSmokeScript)
}

func runLiveKitSmokeScript(t *testing.T, example string, mutate func(*ir.Target), script string) {
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
