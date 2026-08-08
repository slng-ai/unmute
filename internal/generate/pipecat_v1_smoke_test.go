//go:build smoke

package generate

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

const telephonyDeepSmokeScript = `"""Exercise generated carrier ingress, serializer selection, and Redis admission."""
import asyncio
import base64
import json
import os
import time
from urllib.parse import urlencode

carrier = os.environ["UNMUTE_SMOKE_CARRIER"]
os.environ["UNMUTE_PUBLIC_URL"] = "https://voice.example"
os.environ["UNMUTE_OUTBOUND_TOKEN"] = "outbound-smoke"
for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

telnyx_private_key = None
if carrier == "telnyx":
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

    telnyx_private_key = Ed25519PrivateKey.generate()
    public_key = telnyx_private_key.public_key().public_bytes(
        serialization.Encoding.Raw, serialization.PublicFormat.Raw
    )
    os.environ["TELNYX_PUBLIC_KEY"] = base64.b64encode(public_key).decode()

import bot
import telephony as adapter
from pipecat.runner.types import WebSocketRunnerArguments
from pipecat.runner.utils import create_transport
from starlette.requests import Request
from telephony_state import TelephonyState


class FakeWebSocket:
    def __init__(self, messages):
        self.messages = messages
        self.headers = {}

    def iter_text(self):
        async def iterate():
            for message in self.messages:
                yield json.dumps(message)
        return iterate()


def request(path, body, headers):
    sent = False

    async def receive():
        nonlocal sent
        if sent:
            return {"type": "http.disconnect"}
        sent = True
        return {"type": "http.request", "body": body, "more_body": False}

    scope = {
        "type": "http",
        "http_version": "1.1",
        "method": "POST",
        "scheme": "https",
        "path": path,
        "raw_path": path.encode(),
        "query_string": b"",
        "headers": [(name.lower().encode(), value.encode()) for name, value in headers.items()],
        "server": ("voice.example", 443),
        "client": ("127.0.0.1", 1),
    }
    return Request(scope, receive)


async def signed_fixture():
    path = "/telephony/inbound"
    if carrier == "twilio":
        from twilio.request_validator import RequestValidator

        form = {"CallSid": "CA-smoke", "From": "+14155550100", "To": "+14155550101"}
        body = urlencode(form).encode()
        signature = RequestValidator(os.environ["TWILIO_AUTH_TOKEN"]).compute_signature(
            "https://voice.example" + path, form
        )
        parsed = await adapter._signed_form(request(path, body, {"X-Twilio-Signature": signature}))
        assert parsed["CallSid"] == "CA-smoke"
    elif carrier == "telnyx":
        body = json.dumps({"data": {"id": "event-smoke", "event_type": "test"}}, separators=(",", ":")).encode()
        timestamp = str(int(time.time()))
        signature = base64.b64encode(
            telnyx_private_key.sign(timestamp.encode() + b"|" + body)
        ).decode()
        parsed = await adapter._signed_event(request(path, body, {
            "Telnyx-Signature-Ed25519": signature,
            "Telnyx-Timestamp": timestamp,
        }))
        assert parsed["id"] == "event-smoke"
    else:
        from plivo.utils.signature_v3 import construct_post_url, get_signature_v3

        form = {"CallUUID": "plivo-smoke", "From": "+14155550100", "To": "+14155550101"}
        body = urlencode(form).encode()
        nonce = "nonce-smoke"
        base_url = construct_post_url("https://voice.example" + path, dict(form)).decode()
        signature = get_signature_v3(
            os.environ["PLIVO_AUTH_TOKEN"].encode(), base_url, nonce
        ).decode()
        parsed = await adapter._signed_form(request(path, body, {
            "X-Plivo-Signature-V3": signature,
            "X-Plivo-Signature-V3-Nonce": nonce,
        }))
        assert parsed["CallUUID"] == "plivo-smoke"


async def serializer_fixture():
    fixtures = {
        "twilio": {"event": "start", "start": {"streamSid": "MZ-smoke", "callSid": "CA-smoke"}},
        "telnyx": {"stream_id": "stream-smoke", "start": {
            "call_control_id": "call-smoke", "media_format": {"encoding": "PCMU"}
        }},
        "plivo": {"start": {"streamId": "stream-smoke", "callId": "call-smoke"}},
    }
    websocket = FakeWebSocket([fixtures[carrier]])
    runner_args = WebSocketRunnerArguments(websocket=websocket)
    transport = await create_transport(runner_args, bot.transport_params)
    expected = {
        "twilio": "TwilioFrameSerializer",
        "telnyx": "TelnyxFrameSerializer",
        "plivo": "PlivoFrameSerializer",
    }[carrier]
    assert transport._params.serializer.__class__.__name__ == expected


async def admission_fixture():
    state = TelephonyState(os.environ["REDIS_URL"], "deep-smoke-" + carrier, 1, 30)
    assert await state.ping()
    assert await state.admit("first")
    assert await state.admit("first")
    assert not await state.admit("second")
    await state.release("first")
    assert await state.admit("second")
    await state.release("second")
    await state.close()


async def main():
    await signed_fixture()
    await serializer_fixture()
    await admission_fixture()
    await adapter.STATE.close()
    print("telephony deep smoke ok:", carrier)


asyncio.run(main())
`

// pipecatInlineSmokeScript proves the inline single-agent emission (F3) against
// real pipecat-ai 1.5.0 + pipecat-slng: bot.py imports, has no bus scaffolding,
// its service builders construct, and its module-level tool functions register
// on LLMContext (the inline tool path). Construction-level, like the other
// instantiate smokes.
const pipecatInlineSmokeScript = `"""Smoke check: the inline single-agent bot imports and constructs, no bus."""
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from pipecat.processors.aggregators.llm_context import LLMContext  # noqa: E402

src = open("bot.py").read()
assert "BusBridgeProcessor" not in src, "inline bot must not build the bus"
assert "activate_worker(" not in src, "inline bot must not activate workers"

assert type(bot.build_stt()).__name__ == "SlngSTTService"
assert type(bot.build_appointment_desk_llm()).__name__ == "OpenAILLMService"
assert type(bot.build_appointment_desk_tts()).__name__ == "SlngTTSService"

# The generated module-level tool functions are valid direct functions.
LLMContext(
    tools=[
        bot.lookup_customer,
        bot.create_customer,
        bot.check_availability,
        bot.book_appointment,
        bot.cancel_appointment,
    ]
)
print("inline instantiation ok")
`

// TestSmokePipecatV1InlineInstantiates proves the inline single-agent shape (F3)
// end to end on real pipecat-ai: no bus, LLM inline, tools as direct functions
// on LLMContext. simple-prompt ships tracing, which the inline path excludes, so
// the smoke clears it.
func TestSmokePipecatV1InlineInstantiates(t *testing.T) {
	runPipecatSmokeScript(t, "simple-prompt", nil, func(agent *ir.Agent) {
		agent.Tracing = nil
	}, pipecatInlineSmokeScript)
}

// TestSmokePipecatV1BuiltinEndCall proves the emitted bodyless end_call @tool
// imports and constructs in a real venv (prebuilt-tools T11).
func TestSmokePipecatV1BuiltinEndCall(t *testing.T) {
	runPipecatSmokeScript(t, "simple-prompt", nil, func(agent *ir.Agent) {
		agent.Tracing = nil
		addBuiltinEndCall(agent)
	}, pipecatInlineSmokeScript)
}

func examplePackagePath(name string) string {
	if name == "remy" || name == "safe_core" {
		return filepath.Join("..", "testdata", name)
	}
	return filepath.Join("..", "..", "examples", name)
}

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
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
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

func TestSmokePipecatTwilioTemplatesCompileWithoutCredentials(t *testing.T) { // telephony V20
	testSmokePipecatTelephonyTemplatesCompileWithoutCredentials(t, "twilio", map[string]string{
		"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN", "from_number": "TWILIO_PHONE_NUMBER",
	})
}

func TestSmokePipecatTelnyxTemplatesCompileWithoutCredentials(t *testing.T) { // telephony T8, V20
	testSmokePipecatTelephonyTemplatesCompileWithoutCredentials(t, "telnyx", map[string]string{
		"api_key": "TELNYX_API_KEY", "public_key": "TELNYX_PUBLIC_KEY",
		"connection_id": "TELNYX_CONNECTION_ID", "from_number": "TELNYX_PHONE_NUMBER",
	})
}

func TestSmokePipecatPlivoTemplatesCompileWithoutCredentials(t *testing.T) { // telephony T9, V20
	testSmokePipecatTelephonyTemplatesCompileWithoutCredentials(t, "plivo", map[string]string{
		"auth_id": "PLIVO_AUTH_ID", "auth_token": "PLIVO_AUTH_TOKEN", "from_number": "PLIVO_PHONE_NUMBER",
	})
}

func testSmokePipecatTelephonyTemplatesCompileWithoutCredentials(t *testing.T, carrier string, environment map[string]string) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	enablePackageTelephony(pkg)
	configured := pkg.Targets["pipecat"]
	configured.Transport = "carrier-websocket"
	configured.Carrier = carrier
	configured.Connection = "primary_phone"
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	connection := pkg.Connections["primary_phone"]
	connection.Environment = environment
	pkg.Connections["primary_phone"] = connection
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := GeneratePipecat(agent, agent.Targets["pipecat"], nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var paths []string
	for _, name := range []string{"bot.py", "telephony.py", "telephony_shared.py", "telephony_state.py"} {
		content := []byte(artifactFile(t, artifact, name))
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	args := append([]string{"-m", "py_compile"}, paths...)
	if out, err := exec.Command(python, args...).CombinedOutput(); err != nil {
		t.Fatalf("generated telephony syntax: %v\n%s", err, out)
	}
}

func TestSmokePipecatTelephonyRuntimeContracts(t *testing.T) { // telephony V20
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	redisURL := startSmokeRedis(t)
	carriers := map[string]map[string]string{
		"twilio": {
			"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN", "from_number": "TWILIO_PHONE_NUMBER",
		},
		"telnyx": {
			"api_key": "TELNYX_API_KEY", "public_key": "TELNYX_PUBLIC_KEY",
			"connection_id": "TELNYX_CONNECTION_ID", "from_number": "TELNYX_PHONE_NUMBER",
		},
		"plivo": {
			"auth_id": "PLIVO_AUTH_ID", "auth_token": "PLIVO_AUTH_TOKEN", "from_number": "PLIVO_PHONE_NUMBER",
		},
	}
	for _, carrier := range []string{"twilio", "telnyx", "plivo"} {
		t.Run(carrier, func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
			if err != nil {
				t.Fatal(err)
			}
			enablePackageTelephony(pkg)
			configured := pkg.Targets["pipecat"]
			configured.Transport, configured.Carrier, configured.Connection = "carrier-websocket", carrier, "primary_phone"
			pkg.Targets = map[string]spec.Target{"pipecat": configured}
			connection := pkg.Connections["primary_phone"]
			connection.Environment = carriers[carrier]
			pkg.Connections["primary_phone"] = connection
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := GeneratePipecat(agent, agent.Targets["pipecat"], nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			for _, file := range artifact.Files {
				path := filepath.Join(dir, file.Path)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, file.Content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "telephony_deep_smoke.py"), []byte(telephonyDeepSmokeScript), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("uv", "run", "python", "telephony_deep_smoke.py")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "REDIS_URL="+redisURL, "UNMUTE_SMOKE_CARRIER="+carrier)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s telephony runtime smoke: %v\n%s", carrier, err, out)
			}
			if !bytes.Contains(out, []byte("telephony deep smoke ok: "+carrier)) {
				t.Fatalf("unexpected %s telephony smoke output:\n%s", carrier, out)
			}
		})
	}
}

func TestSmokePipecatTelephonyRuntimeScriptCompiles(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	path := filepath.Join(t.TempDir(), "telephony_deep_smoke.py")
	if err := os.WriteFile(path, []byte(telephonyDeepSmokeScript), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(python, "-m", "py_compile", path).CombinedOutput(); err != nil {
		t.Fatalf("telephony runtime smoke syntax: %v\n%s", err, out)
	}
}

func startSmokeRedis(t *testing.T) string {
	t.Helper()
	if configured := os.Getenv("UNMUTE_SMOKE_REDIS_URL"); configured != "" {
		return configured
	}
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server is unavailable and UNMUTE_SMOKE_REDIS_URL is not set")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd := exec.Command(binary,
		"--bind", "127.0.0.1", "--port", fmt.Sprint(port),
		"--save", "", "--appendonly", "no", "--dir", t.TempDir(),
	)
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start redis-server: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	address := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		connection, dialErr := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			return "redis://" + address + "/0"
		}
		select {
		case err := <-done:
			done <- err
			t.Fatalf("redis-server exited during startup: %v\n%s", err, output.String())
		default:
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("redis-server did not become ready:\n%s", output.String())
	return ""
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
from pathlib import Path

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402

if Path("tracing.py").exists():
    import tracing

    for name in ("LANGFUSE_SECRET_KEY", "LANGFUSE_PUBLIC_KEY", "LANGFUSE_BASE_URL"):
        os.environ.pop(name, None)
    try:
        tracing.setup_langfuse_tracing()
    except ValueError:
        pass
    else:
        raise AssertionError("configured Langfuse tracing requires all credentials")

builders = sorted(n for n in vars(bot) if n.startswith("build_") and callable(getattr(bot, n)))
assert builders, "no service builders found in bot.py"


async def _run() -> None:  # some services construct an aiohttp session
    for name in builders:
        getattr(bot, name)()


asyncio.run(_run())
print("smoke ok:", ", ".join(builders))
`

const pipecatTaskRoleSmokeScript = `"""Smoke check: task role replaces, then restores, the owner role."""
import asyncio
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from pipecat.flows import FlowManager  # noqa: E402
from pipecat.frames.frames import Frame  # noqa: E402
from pipecat.processors.aggregators.llm_context import LLMContext  # noqa: E402
from pipecat.processors.aggregators.llm_response_universal import LLMContextAggregatorPair  # noqa: E402
from pipecat.processors.frame_processor import FrameDirection, FrameProcessor  # noqa: E402
from pipecat.services.llm_service import LLMService  # noqa: E402
from pipecat.services.settings import LLMSettings  # noqa: E402
from pipecat.workers.runner import WorkerRunner  # noqa: E402

OWNER_PROMPT = None
original_owner_builder = bot.build_appointment_desk_llm


class FakeLLM(LLMService):
    def __init__(self) -> None:
        super().__init__(settings=LLMSettings(model="smoke", system_instruction=OWNER_PROMPT))

    async def process_frame(self, frame: Frame, direction: FrameDirection) -> None:
        await super().process_frame(frame, direction)
        await self.push_frame(frame, direction)


class Passthrough(FrameProcessor):
    async def process_frame(self, frame: Frame, direction: FrameDirection) -> None:
        await super().process_frame(frame, direction)
        await self.push_frame(frame, direction)


for name in list(vars(bot)):
    if name.startswith("build_") and name.endswith("_llm"):
        setattr(bot, name, FakeLLM)
    elif name.startswith("build_") and name.endswith("_tts"):
        setattr(bot, name, Passthrough)


async def main() -> None:
    global OWNER_PROMPT
    OWNER_PROMPT = original_owner_builder()._settings.system_instruction
    context = LLMContext()
    owner = bot.AppointmentDeskAgent(state=None, context=context, call_context=None)
    target = bot.AftercareAgent(state=None, context=context, call_context=None)
    runner = WorkerRunner(handle_sigint=False)
    await runner.add_workers(owner, target)
    run_task = asyncio.create_task(runner.run(auto_end=False))
    await asyncio.wait_for(owner._pipeline_start_event.wait(), timeout=5)
    await asyncio.wait_for(target._pipeline_start_event.wait(), timeout=5)

    owner._active = True
    owner._manage_appointment_results = {}
    owner._manage_appointment_snapshot = (
        [dict(message) for message in context.get_messages()],
        context.tools,
    )
    flow = FlowManager(
        llm=owner.llm,
        context_aggregator=LLMContextAggregatorPair(context),
        worker=owner,
    )
    node = owner._manage_appointment_node_identify_customer()
    await flow.initialize(node)
    await owner.flush_pipeline()
    assert owner.llm._settings.system_instruction == node["role_message"]
    assert owner.llm._settings.system_instruction != OWNER_PROMPT

    await owner._manage_appointment_finish_finalize_appointment(
        {"action": "book", "status": "ok", "appointment_id": "apt-smoke"},
        flow,
    )
    assert owner.llm._settings.system_instruction == OWNER_PROMPT
    for _ in range(100):
        if target.active:
            break
        await asyncio.sleep(0.01)
    assert target.active, "then: transfer did not activate the target worker"

    await runner.cancel("task role smoke complete")
    await asyncio.wait_for(run_task, timeout=5)
    print("task role smoke ok")


asyncio.run(main())
`

const pipecatStaticCheckScript = `"""Smoke check: the generated project passes ty."""
import subprocess

subprocess.run(["ty", "check", "."], check=True)
`

// pipecatRequestTracingSmokeScript drives the generated worker/bus topology
// through deterministic STT, LLM, and TTS services. V17 requires all three
// request spans to share the conversation trace.
const pipecatRequestTracingSmokeScript = `"""Smoke check: a real worker turn emits nested speech and LLM spans."""
import asyncio
import base64
import json
import os
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer


class OTLPReceiver(BaseHTTPRequestHandler):
    def do_POST(self) -> None:
        self.rfile.read(int(self.headers["Content-Length"]))
        self.server.requests.append((self.path, dict(self.headers)))
        self.send_response(200)
        self.end_headers()

    def log_message(self, format, *args) -> None:
        pass


receiver = HTTPServer(("127.0.0.1", 0), OTLPReceiver)
receiver.requests = []
threading.Thread(target=receiver.serve_forever, daemon=True).start()

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")
os.environ["LANGFUSE_PUBLIC_KEY"] = "pk-smoke"
os.environ["LANGFUSE_SECRET_KEY"] = "sk-smoke"
os.environ["LANGFUSE_BASE_URL"] = f"http://127.0.0.1:{receiver.server_port}"

import bot  # noqa: E402
import tracing as tracing_config  # noqa: E402
from opentelemetry import trace  # noqa: E402
from opentelemetry.sdk.trace.export import SimpleSpanProcessor  # noqa: E402
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter  # noqa: E402
from pipecat.bus import BusBridgeProcessor  # noqa: E402
from pipecat.frames.frames import (  # noqa: E402
    Frame,
    FunctionCallFromLLM,
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
from pipecat.workers.llm import LLMWorkerActivationArgs  # noqa: E402
from pipecat.workers.runner import WorkerRunner  # noqa: E402


class FakeLLM(LLMService):
    def __init__(self) -> None:
        super().__init__(
            settings=LLMSettings(
                model="probe-model",
                system_instruction="You are the tracing probe.",
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
        self.tool_called = False

    @traced_llm
    async def _process_context(self, context: LLMContext) -> None:
        if not self.tool_called:
            self.tool_called = True
            await self.run_function_calls(
                [
                    FunctionCallFromLLM(
                        function_name="lookup_customer",
                        tool_call_id="call-smoke",
                        arguments={"phone": "+1555010101"},
                        context=context,
                    )
                ]
            )
        else:
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

    def can_generate_metrics(self) -> bool:
        return True

    @traced_stt
    async def run_stt(self, audio: bytes):
        await self.start_ttfb_metrics()
        yield TranscriptionFrame(
            "trace this request",
            user_id="probe-user",
            timestamp="2026-07-20T00:00:00Z",
            language="en",
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

    def can_generate_metrics(self) -> bool:
        return True

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
    assert tracing_config.setup_langfuse_tracing()
    provider = trace.get_tracer_provider()
    provider.add_span_processor(SimpleSpanProcessor(memory))

    agent_names = sorted(
        name.removeprefix("build_").removesuffix("_llm")
        for name in vars(bot)
        if name.startswith("build_") and name.endswith("_llm")
    )
    assert len(agent_names) == 1, agent_names
    agent_name = agent_names[0]
    setattr(bot, f"build_{agent_name}_llm", FakeLLM)
    setattr(bot, f"build_{agent_name}_tts", FakeTTS)

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
        additional_span_attributes={"langfuse.trace.name": tracing_config.TRACE_NAME},
        params=PipelineParams(enable_metrics=True, enable_usage_metrics=True),
    )
    agent_types = [
        value
        for value in vars(bot).values()
        if isinstance(value, type)
        and issubclass(value, bot.LLMWorker)
        and value is not bot.LLMWorker
        and value.__module__ == "bot"
    ]
    assert len(agent_types) == 1, agent_types
    request_agent = agent_types[0]()
    tracing_config.enable_agent_tracing(main_worker, [request_agent])

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
    turn = next(span for span in spans if span.name == "turn")
    tool_call = next(span for span in spans if span.name == "tool:lookup_customer")
    requests = {span.name: span for span in spans if span.name in {"stt", "llm", "tts"}}
    assert requests.keys() == {"stt", "llm", "tts"}
    assert requests["stt"].attributes["gen_ai.request.model"] == "probe-stt"
    assert requests["llm"].attributes["gen_ai.request.model"] == "probe-model"
    assert requests["tts"].attributes["gen_ai.request.model"] == "probe-tts"
    assert requests["stt"].attributes["gen_ai.provider.name"] == "fakestt"
    assert requests["tts"].attributes["gen_ai.provider.name"] == "faketts"
    assert requests["stt"].attributes["transcript"] == "trace this request"
    assert requests["stt"].attributes["language"] == "en"
    assert requests["stt"].attributes["is_final"] is True
    assert requests["llm"].attributes["output"] == "traced."
    assert requests["llm"].attributes["gen_ai.system_instructions"] == "You are the tracing probe."
    llm_input = json.loads(requests["llm"].attributes["langfuse.observation.input"])
    assert llm_input[0] == {"role": "system", "content": "You are the tracing probe."}
    assert {"role": "user", "content": "trace this request"} in llm_input
    assert json.loads(requests["llm"].attributes["input"]) == llm_input
    assert requests["tts"].attributes["text"] == "traced."
    assert requests["tts"].attributes["voice_id"] == "probe-voice"
    assert requests["tts"].attributes["metrics.character_count"] == len("traced.")
    assert requests["stt"].attributes["metrics.ttfb"] >= 0
    assert requests["tts"].attributes["metrics.ttfb"] >= 0
    assert json.loads(requests["stt"].attributes["langfuse.observation.input"]) == "audio"
    assert json.loads(requests["stt"].attributes["langfuse.observation.output"]) == "trace this request"
    assert json.loads(requests["tts"].attributes["langfuse.observation.input"]) == "traced."
    assert json.loads(requests["tts"].attributes["langfuse.observation.output"]) == "audio"
    assert json.loads(requests["stt"].attributes["langfuse.trace.input"]) == "trace this request"
    assert json.loads(requests["tts"].attributes["langfuse.trace.output"]) == "traced."
    assert requests["stt"].attributes["langfuse.observation.metadata.ttfb_seconds"] >= 0
    assert requests["tts"].attributes["langfuse.observation.metadata.ttfb_seconds"] >= 0
    assert requests["stt"].attributes["langfuse.observation.completion_start_time"]
    assert requests["tts"].attributes["langfuse.observation.completion_start_time"]
    assert requests["tts"].attributes["langfuse.observation.metadata.character_count"] == len("traced.")
    assert json.loads(requests["tts"].attributes["langfuse.observation.usage_details"]) == {
        "characters": len("traced.")
    }
    assert conversation.attributes["langfuse.trace.name"] == tracing_config.TRACE_NAME
    assert conversation.resource.attributes["service.name"] == tracing_config.TRACE_NAME
    assert all(span.context.trace_id == conversation.context.trace_id for span in requests.values())
    assert tool_call.context.trace_id == conversation.context.trace_id
    assert tool_call.parent.span_id == turn.context.span_id
    assert json.loads(tool_call.attributes["langfuse.observation.input"]) == {"phone": "+1555010101"}
    assert json.loads(tool_call.attributes["langfuse.observation.output"])["customer_id"] == "cus_1001"
    assert tool_call.attributes["tool.function_name"] == "lookup_customer"
    assert tool_call.attributes["tool.call_id"] == "call-smoke"
    assert all(span.end_time > span.start_time for span in requests.values())
    assert receiver.requests
    path, headers = receiver.requests[0]
    headers = {name.lower(): value for name, value in headers.items()}
    auth = base64.b64encode(b"pk-smoke:sk-smoke").decode()
    assert path == "/api/public/otel/v1/traces"
    assert headers["authorization"] == f"Basic {auth}"
    assert headers["x-langfuse-ingestion-version"] == "4"
    receiver.shutdown()
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

// TestSmokePipecatV1TaskGroupsInstantiate runs the generated FlowManager on
// Pipecat 1.5.0 and observes task-role replacement, owner-role restoration,
// and transfer activation (V28).
func TestSmokePipecatV1TaskGroupsInstantiate(t *testing.T) {
	runPipecatSmokeScript(t, "task-groups", nil, func(agent *ir.Agent) {
		aftercare := agent.Agents["appointment_desk"]
		aftercare.Instructions = "You are the aftercare agent."
		aftercare.Tools = nil
		agent.Agents["aftercare"] = aftercare
		group := agent.TaskGroups["appointment_flow"]
		group.Then = ir.GroupTransfer
		group.ThenTarget = "aftercare"
		agent.TaskGroups["appointment_flow"] = group
	}, pipecatTaskRoleSmokeScript)
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
// V13) against real pipecat-ai: importing bot defines the call-local worker
// classes, so the @tool wrapper class-collects and `import tools.fetch_notes`
// resolves the copied handler file inside the venv.
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

// TestSmokeV17PipecatSpeechTracing proves the generated OTLP setup exports the
// native STT, LLM, and TTS tree under the named conversation trace (V21).
func TestSmokeV17PipecatSpeechTracing(t *testing.T) {
	runPipecatSmokeScript(t, "simple-prompt", nil, nil, pipecatRequestTracingSmokeScript)
}

func TestSmokeV24PipecatSimplePromptStaticCheck(t *testing.T) {
	runPipecatSmokeScript(t, "simple-prompt", nil, nil, pipecatStaticCheckScript)
}

// TestSmokeV24PipecatExamplesStaticCheck holds raw Pipecat output to the bar
// LiveKit has had since V26, over the same examples: `uv run ruff check .`, the
// exact command a user would run in a generated project. It closes the gap where
// a lint regression in a Pipecat template was caught on one driver and missed on
// the other, and it only became runnable once the emitted pyproject declared a
// pinned ruff of its own.
//
// ty stays on simple-prompt (TestSmokeV24PipecatSimplePromptStaticCheck) rather
// than widening here: run over multi-task and task-groups it reports real type
// errors in emitted task code (self.context is `Unknown | None` at the snapshot
// and aggregator call sites, self.state likewise where results are assigned).
// Those are driver bugs to fix in their own change, not something to widen the
// gate into and leave red.
func TestSmokeV24PipecatExamplesStaticCheck(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	for _, example := range []string{"simple-prompt", "multi-task", "task-groups", "subagents"} {
		t.Run(example, func(t *testing.T) {
			pkg, err := spec.Load(examplePackagePath(example))
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
			// Only the emitted project lands here: no smoke script alongside, so
			// ruff sees exactly what a user would compile.
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
			cmd := exec.Command("uv", "run", "ruff", "check", ".")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("uv run ruff check . failed:\n%s", out)
			}
		})
	}
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
