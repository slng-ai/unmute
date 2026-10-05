//go:build smoke

package generate

import "testing"

// The unit tests hold what the emitted Logfire backend says. This holds what it
// does, against the real OpenTelemetry packages: the write token picks the
// region, and a span reaches an OTLP endpoint carrying the raw token as its
// Authorization header. A local sink stands in for Logfire, so the check needs
// no credential beyond a fake one.

func TestSmokeLogfireTracingPipecat(t *testing.T) {
	runPipecatSmokeScript(t, "salon-concierge", nil, enableLogfire, logfireTracingSmokeScript)
}

func TestSmokeLogfireTracingLiveKit(t *testing.T) {
	runLiveKitSmokeScript(t, "salon-concierge", nil, enableLogfire, logfireTracingSmokeScript)
}

const logfireTracingSmokeScript = `
import http.server
import os
import threading

from utils import telemetry

RECEIVED = []


class _Sink(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length") or 0))
        RECEIVED.append({"path": self.path, **{k.lower(): v for k, v in self.headers.items()}})
        self.send_response(200)
        self.send_header("Content-Type", "application/x-protobuf")
        self.end_headers()

    def log_message(self, *a):
        pass


_server = http.server.HTTPServer(("127.0.0.1", 0), _Sink)
threading.Thread(target=_server.serve_forever, daemon=True).start()
_BASE = f"http://127.0.0.1:{_server.server_address[1]}"

# The region comes off the token, the way the Logfire SDK reads it.
regions = {
    "pylf_v1_eu_secret": "https://logfire-eu.pydantic.dev",
    "pylf_v1_us_secret": "https://logfire-us.pydantic.dev",
    "pylf_v2_eu_secret": "https://logfire-eu.pydantic.dev",
    # An older token names no region, and an unknown region is not guessed at.
    "legacy-token-with-no-region": "https://logfire-us.pydantic.dev",
    "pylf_v1_xx_secret": "https://logfire-us.pydantic.dev",
}
for token, want in regions.items():
    assert telemetry.Logfire(token).base_url == want, (token, telemetry.Logfire(token).base_url)

# A missing token fails at setup, before any call is traced to nowhere.
os.environ.pop("LOGFIRE_TOKEN", None)
try:
    telemetry.Logfire.from_env()
except ValueError:
    pass
else:
    raise AssertionError("Logfire tracing started with no LOGFIRE_TOKEN")


class _LocalLogfire(telemetry.Logfire):
    @property
    def base_url(self) -> str:
        return _BASE


collector = telemetry.Telemetry(_LocalLogfire("pylf_v1_eu_smoke"))
assert collector.provider.resource.attributes["service.name"] == telemetry.TRACE_NAME
collector.tracer("smoke").start_span("turn").end()
assert collector.flush(), "the flush timed out"
assert RECEIVED, "no span reached the Logfire endpoint"
assert RECEIVED[0]["path"] == "/v1/traces", RECEIVED[0]
# The raw token, with no Bearer prefix, is what Logfire authenticates.
assert RECEIVED[0]["authorization"] == "pylf_v1_eu_smoke", RECEIVED[0]

# Every row is labelled and every model call carries its messages, the way
# Logfire's own integrations shape theirs. Real spans, through the real
# exporter wrapper, into a memory exporter instead of the network.
import json

from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

memory = InMemorySpanExporter()
shaping = TracerProvider()
shaping.add_span_processor(SimpleSpanProcessor(telemetry.LogfireExporter(memory)))
tracer = shaping.get_tracer("smoke")
pipecat_input = json.dumps(
    [
        {"role": "system", "content": "Be brief."},
        {"role": "user", "content": "hi"},
        {"role": "assistant", "tool_calls": [{"id": "c1", "function": {"name": "lookup", "arguments": "{}"}}]},
        {"role": "tool", "tool_call_id": "c1", "content": "found"},
    ]
)
livekit_messages = json.dumps([{"role": "user", "parts": [{"type": "text", "content": "hi"}]}])
with tracer.start_as_current_span("conversation", attributes={"session.id": "s1"}):
    with tracer.start_as_current_span("turn", attributes={"input.value": json.dumps("hello there")}) as turn:
        turn.set_attributes(collector.backend.agent_run("intake"))
        with tracer.start_as_current_span(
            "llm",
            attributes={
                "gen_ai.operation.name": "chat",
                "gen_ai.request.model": "gpt-probe",
                "gen_ai.system_instructions": "Be brief.",
                "input": pipecat_input,
            },
        ) as llm:
            llm.set_attribute("output", "Hello!")
        tracer.start_span(
            "llm_request",
            attributes={
                "gen_ai.operation.name": "chat",
                "gen_ai.request.model": "gpt-probe",
                "gen_ai.input.messages": livekit_messages,
            },
        ).end()
        tracer.start_span(
            "function_tool",
            attributes={"gen_ai.operation.name": "execute_tool", "gen_ai.tool.name": "lookup"},
        ).end()
        tracer.start_span("agent_speaking").end()

shaped = {span.name: span for span in memory.get_finished_spans()}
labels = {name: span.attributes.get("logfire.msg") for name, span in shaped.items()}
assert labels == {
    "conversation": "call s1",
    "turn": "turn: hello there",
    "llm": "chat gpt-probe",
    "llm_request": "chat gpt-probe",
    "function_tool": "tool lookup",
    "agent_speaking": None,
}, labels

# Pipecat's own attributes become the GenAI messages, with the system prompt
# kept out of them because it has an attribute of its own.
llm = shaped["llm"].attributes
assert json.loads(llm["gen_ai.input.messages"]) == [
    {"role": "user", "parts": [{"type": "text", "content": "hi"}]},
    {"role": "assistant", "parts": [{"type": "tool_call", "name": "lookup", "arguments": "{}"}]},
    {"role": "tool", "parts": [{"type": "tool_call_response", "id": "c1", "response": "found"}]},
], llm["gen_ai.input.messages"]
assert json.loads(llm["gen_ai.output.messages"]) == [
    {"role": "assistant", "parts": [{"type": "text", "content": "Hello!"}]}
], llm["gen_ai.output.messages"]
assert json.loads(llm["gen_ai.system_instructions"]) == [{"type": "text", "content": "Be brief."}]
# A span that already speaks GenAI is not rewritten.
assert shaped["llm_request"].attributes["gen_ai.input.messages"] == livekit_messages
assert "gen_ai.output.messages" not in shaped["llm_request"].attributes
# Shaping copies a span; it moves nothing. Parents and IDs are the originals.
assert shaped["llm"].parent.span_id == shaped["turn"].context.span_id
assert shaped["turn"].parent.span_id == shaped["conversation"].context.span_id
# An agent run that is not a turn is labelled by its agent.
assert telemetry.logfire_message("agent_turn", collector.backend.agent_run("intake")) == "agent intake"
# The turn is still an agent run for the Agents view, whatever its label says.
assert shaped["turn"].attributes["gen_ai.operation.name"] == "invoke_agent"
print(f"logfire tracing ok: {len(regions)} tokens resolved, {len(RECEIVED)} export(s), {len(shaped)} spans shaped")
`
