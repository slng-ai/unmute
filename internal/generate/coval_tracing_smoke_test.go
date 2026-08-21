//go:build smoke

package generate

import "testing"

// The unit tests hold what the emitted Coval module says. These hold what it
// does, against the real OpenTelemetry and framework packages: spans are held
// until a simulation ID arrives, the flush carries Coval's two headers, and a
// call no simulation owns exports nothing instead of failing.
//
// A local HTTP sink stands in for api.coval.dev. Nothing here reaches Coval, so
// the check needs no credential beyond a fake one.

func TestSmokeCovalTracingPipecat(t *testing.T) {
	runPipecatSmokeScript(t, "salon-concierge", nil, enableCoval, covalPipecatTracingSmokeScript)
}

func TestSmokeCovalTracingLiveKit(t *testing.T) {
	runLiveKitSmokeScript(t, "salon-concierge", nil, enableCoval, covalLiveKitTracingSmokeScript)
}

// covalTracingSmokeSink is the shared half: a local OTLP endpoint that records
// what it was sent, so the check can assert on real exported batches.
const covalTracingSmokeSink = `
import http.server
import json
import os
import threading
import types

RECEIVED = []


SUBMITTED = []


class _Sink(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length)
        if self.path.endswith("conversations:submit"):
            SUBMITTED.append(
                {
                    "headers": {k.lower(): v for k, v in self.headers.items()},
                    "body": json.loads(body),
                }
            )
            payload = json.dumps(
                {"conversation": {"conversation_id": "conv-from-submit", "status": "IN_QUEUE"}}
            ).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return
        RECEIVED.append({k.lower(): v for k, v in self.headers.items()})
        self.send_response(200)
        self.send_header("Content-Type", "application/x-protobuf")
        self.end_headers()
        self.wfile.write(b"")

    def log_message(self, *a):
        pass


_server = http.server.HTTPServer(("127.0.0.1", 0), _Sink)
threading.Thread(target=_server.serve_forever, daemon=True).start()
_BASE = f"http://127.0.0.1:{_server.server_address[1]}"
_ENDPOINT = _BASE + "/v1/traces"
_SUBMIT_ENDPOINT = _BASE + "/v1/conversations:submit"

os.environ["COVAL_API_KEY"] = "smoke-key-not-a-real-secret"
os.environ.pop("COVAL_SIMULATION_ID", None)

import tracing

tracing.COVAL_TRACES_ENDPOINT = _ENDPOINT
tracing.COVAL_CONVERSATIONS_ENDPOINT = _SUBMIT_ENDPOINT
`

const covalPipecatTracingSmokeScript = covalTracingSmokeSink + `
provider = tracing.setup_coval_tracing()

# Before any simulation ID exists, a span must be held rather than sent.
provider.get_tracer("smoke").start_span("llm").end()
provider.force_flush()
assert RECEIVED == [], f"exported before activation: {RECEIVED}"

# Every documented Pipecat route resolves, and reports itself.
ws = types.SimpleNamespace(
    websocket=types.SimpleNamespace(headers={"X-Coval-Simulation-Id": "sim-ws"}),
    body={},
    call_data=None,
)
assert tracing.resolve_simulation_id(ws) == ("sim-ws", "websocket_header")

dialin = types.SimpleNamespace(
    websocket=None,
    body={"dialin_settings": {"sip_headers": {"x-coval-simulation-id": "sim-sip"}}},
    call_data=None,
)
assert tracing.resolve_simulation_id(dialin) == ("sim-sip", "sip_header")

param = types.SimpleNamespace(
    websocket=None,
    body={},
    call_data=types.SimpleNamespace(body={"X-Coval-Simulation-Id": "sim-param"}),
)
assert tracing.resolve_simulation_id(param) == ("sim-param", "carrier_parameter")

# A browser session carries nothing, and that is not an error.
bare = types.SimpleNamespace(websocket=None, body={}, call_data=None)
assert tracing.resolve_simulation_id(bare) == (None, "none")

# A call no simulation claimed still reaches Coval: flush_tracing registers it
# as a conversation, built from the transcript already on the held spans, and
# exports the same spans against the conversation ID. This is what puts a local
# unmute dev run in Trace Search.
heard = provider.get_tracer("smoke").start_span("stt")
heard.set_attribute("transcript", "i would like to book a haircut")
heard.end()
said = provider.get_tracer("smoke").start_span("tts")
said.set_attribute("text", "Sure, what day works?")
said.end()
provider.force_flush()
assert RECEIVED == [], f"exported with no correlation ID at all: {RECEIVED}"

tracing.flush_tracing(provider, "local-session-1")
assert len(SUBMITTED) == 1, SUBMITTED
submit = SUBMITTED[0]
assert submit["headers"].get("x-api-key") == "smoke-key-not-a-real-secret", submit["headers"]
assert submit["body"]["transcript"] == [
    {"role": "user", "content": "i would like to book a haircut"},
    {"role": "assistant", "content": "Sure, what day works?"},
], submit["body"]["transcript"]
assert submit["body"]["external_conversation_id"] == "local-session-1", submit["body"]
assert RECEIVED, "the conversation ID did not flush the held spans"
conv_export = RECEIVED[0]
# Exactly one correlation header, and it is the conversation one.
assert conv_export.get("x-conversation-id") == "conv-from-submit", conv_export
assert "x-simulation-id" not in conv_export, conv_export

# A second flush must not register the same call twice.
tracing.flush_tracing(provider, "local-session-1")
assert len(SUBMITTED) == 1, SUBMITTED

RECEIVED.clear()

# Activating flushes the span that existed before the ID did.
tracing._router.simulation_id = None
tracing._router._exporter = None
assert tracing.activate_simulation(ws) == "sim-ws"
provider.force_flush()
assert RECEIVED, "activation did not flush the held span"
first = RECEIVED[0]
assert first.get("x-api-key") == "smoke-key-not-a-real-secret", first
assert first.get("x-simulation-id") == "sim-ws", first
# The simulation route must never carry the conversation header.
assert "x-conversation-id" not in first, first

print(
    f"coval pipecat tracing ok: {len(RECEIVED)} export(s) after activation, "
    f"{len(SUBMITTED)} conversation(s) registered"
)
`

const covalLiveKitTracingSmokeScript = covalTracingSmokeSink + `
from opentelemetry.trace import StatusCode


class _Room:
    def __init__(self):
        self.name = "call-smoke"
        self.remote_participants = {}
        self.handlers = {}

    def on(self, event, handler):
        self.handlers[event] = handler


class _ChatContext:
    def __init__(self, items):
        self.items = items

    def to_dict(self, **_kwargs):
        return {"items": self.items}


class _Agent:
    """Stands in for the Agent holding the floor, with the three things the
    module reads off it: its prompt, its history and its tool list."""

    label = "booking_specialist"
    instructions = "You book haircuts. Confirm the slot before writing."

    def __init__(self):
        self.chat_ctx = _ChatContext([])
        self.tools = [
            types.SimpleNamespace(name="check_availability"),
            types.SimpleNamespace(name="create_booking"),
        ]


class _Session:
    def __init__(self):
        self.handlers = {}
        self.current_agent = _Agent()

    def on(self, event):
        def register(fn):
            self.handlers[event] = fn
            return fn

        return register

    def fire(self, event, **fields):
        self.handlers[event](types.SimpleNamespace(**fields))


def message(role, at, text, **fields):
    return types.SimpleNamespace(
        type="message", role=role, created_at=at, text_content=text, **fields
    )


def model(name, provider):
    return types.SimpleNamespace(model_name=name, model_provider=provider)


shutdown_callbacks = []
participant = types.SimpleNamespace(attributes={})
room = _Room()
room.remote_participants["sip_0"] = participant
ctx = types.SimpleNamespace(
    job=types.SimpleNamespace(metadata=""),
    room=room,
    add_shutdown_callback=shutdown_callbacks.append,
)
session = _Session()

provider = tracing.setup_coval(ctx, session, metadata={"session.id": room.name})

# Everything the module exports passes through the router, so recording there
# captures the finished spans with their parents and attributes intact.
EXPORTED = []
_real_export = tracing._router.export


def _recording_export(spans):
    EXPORTED.extend(spans)
    return _real_export(spans)


tracing._router.export = _recording_export

# Nothing may leave before the call is claimed by a simulation.
provider.force_flush()
assert RECEIVED == [], f"exported before activation: {RECEIVED}"

# A SIP participant's attributes arrive after the job has already started.
participant.attributes = {"coval.simulation_id": "sim-livekit"}
room.handlers["participant_attributes_changed"](None, participant)

# Both attribute spellings resolve: the lowercase key LiveKit actually writes,
# and a capitalized one from a differently configured trunk.
assert tracing._simulation_from_attributes({"sip.h.x-coval-simulation-id": "a"}) == "a"
assert tracing._simulation_from_attributes({"sip.h.X-Coval-Simulation-Id": "b"}) == "b"
assert tracing._simulation_from_attributes({}) is None

# ── One scripted call, in the order LiveKit reports it ─────────────────────────
#
# Turn 1 is the greeting: the agent speaks before anyone has said anything.
# Turn 2 is a real exchange that runs two tools, one of which fails, and takes
# two LLM rounds to answer. Turn 3 has a streaming transcriber that reports no
# transcription delay at all.
T = 1_000_000.0

session.fire("conversation_item_added", item=message("assistant", T + 0.5, "Hi, salon here."))
session.fire(
    "metrics_collected",
    metrics=types.SimpleNamespace(
        type="tts_metrics",
        timestamp=T + 0.6,
        duration=0.4,
        ttfb=0.2,
        characters_count=15,
        audio_duration=1.1,
        metadata=model("sonic-2", "cartesia"),
    ),
)

# Preemptive generation finishes work for the next exchange before the user
# message that names it arrives: LiveKit starts the model while the caller is
# still speaking, and commits the user message only once the final transcript
# lands. This metric arrives while the greeting turn is still open, but the
# round started after the caller started speaking, so it belongs to the
# exchange that follows.
session.fire(
    "metrics_collected",
    metrics=types.SimpleNamespace(
        type="llm_metrics",
        request_id="req-pre",
        timestamp=T + 2.3,
        duration=0.2,
        ttft=0.1,
        prompt_tokens=90,
        completion_tokens=10,
        total_tokens=100,
        prompt_cached_tokens=0,
        cancelled=False,
        metadata=model("gpt-4o-mini", "openai"),
    ),
)

# LiveKit commits a transcript per pause, so one spoken sentence can arrive in
# two pieces. Both belong to the exchange the agent is about to answer.
session.fire(
    "conversation_item_added",
    item=message(
        "user",
        T + 2.4,
        "book a cut",
        transcript_confidence=0.91,
        metrics={
            "started_speaking_at": T + 2.0,
            "stopped_speaking_at": T + 2.3,
            "transcription_delay": 0.1,
            "end_of_turn_delay": 0.2,
        },
    ),
)
session.fire(
    "conversation_item_added",
    item=message(
        "user",
        T + 3.0,
        "for friday",
        transcript_confidence=0.94,
        metrics={
            "started_speaking_at": T + 2.6,
            "stopped_speaking_at": T + 2.8,
            "transcription_delay": 0.15,
            "end_of_turn_delay": 0.3,
        },
    ),
)
session.fire(
    "metrics_collected",
    metrics=types.SimpleNamespace(
        type="stt_metrics",
        timestamp=T + 3.0,
        duration=0.0,
        streamed=True,
        audio_duration=0.8,
        metadata=model("nova-3", "Deepgram"),
    ),
)
session.fire(
    "metrics_collected",
    metrics=types.SimpleNamespace(
        type="llm_metrics",
        timestamp=T + 3.5,
        duration=0.5,
        ttft=0.2,
        prompt_tokens=120,
        completion_tokens=18,
        total_tokens=138,
        prompt_cached_tokens=64,
        cancelled=False,
        metadata=model("gpt-4o-mini", "openai"),
    ),
)
ok_call = types.SimpleNamespace(name="check_availability", call_id="c1", arguments='{"day":"fri"}')
bad_call = types.SimpleNamespace(name="record_complaint", call_id="c2", arguments="{}")
session.fire(
    "function_tools_executed",
    created_at=T + 3.9,
    function_calls=[ok_call, bad_call],
    zipped=lambda: [
        (ok_call, types.SimpleNamespace(is_error=False, output='{"slots":["15:00"]}')),
        (bad_call, types.SimpleNamespace(is_error=True, output="upstream unavailable")),
    ],
)
# By the second round the tool call and its result are in the chat context, which
# is what makes the snapshot taken at metrics time the prompt that round ran on.
session.current_agent.chat_ctx = _ChatContext(
    [
        {"type": "message", "role": "system", "content": ["You book haircuts."]},
        {"type": "message", "role": "user", "content": ["book a cut for friday"]},
        {"type": "function_call", "name": "check_availability", "arguments": '{"day":"fri"}'},
        {"type": "function_call_output", "name": "check_availability", "output": "15:00"},
    ]
)
session.fire(
    "metrics_collected",
    metrics=types.SimpleNamespace(
        type="llm_metrics",
        request_id="req-llm-2",
        timestamp=T + 4.4,
        duration=0.4,
        ttft=0.15,
        prompt_tokens=180,
        completion_tokens=24,
        total_tokens=204,
        prompt_cached_tokens=120,
        cancelled=False,
        metadata=model("gpt-4o-mini", "openai"),
    ),
)
session.fire(
    "metrics_collected",
    metrics=types.SimpleNamespace(
        type="tts_metrics",
        request_id="req-tts-2",
        timestamp=T + 5.0,
        duration=0.5,
        ttfb=0.25,
        characters_count=42,
        audio_duration=2.0,
        metadata=model("sonic-2", "cartesia"),
    ),
)
session.fire(
    "conversation_item_added",
    item=message(
        "assistant",
        T + 5.1,
        "Friday at three is free.",
        interrupted=False,
        metrics={"e2e_latency": 1.2},
    ),
)

session.fire(
    "conversation_item_added",
    item=message(
        "user",
        T + 7.0,
        "that works",
        transcript_confidence=0.99,
        metrics={
            "started_speaking_at": T + 6.4,
            "stopped_speaking_at": T + 6.9,
            "transcription_delay": 0,
            "end_of_turn_delay": 0.2,
        },
    ),
)
session.fire(
    "metrics_collected",
    metrics=types.SimpleNamespace(
        type="stt_metrics",
        timestamp=T + 7.0,
        duration=0.0,
        streamed=True,
        audio_duration=0.5,
        metadata=model("nova-3", "Deepgram"),
    ),
)
# LiveKit can report a round with no usage at all, which happens while it swaps
# agents. Four zeros there would read as a free request and drag a token average
# down, so usage is left off, the same way a zero latency is.
session.fire(
    "metrics_collected",
    metrics=types.SimpleNamespace(
        type="llm_metrics",
        timestamp=T + 7.4,
        duration=0.1,
        ttft=0.0,
        prompt_tokens=0,
        completion_tokens=0,
        total_tokens=0,
        prompt_cached_tokens=0,
        cancelled=False,
        metadata=None,
    ),
)
session.fire("close", type="close")

provider.force_flush()
assert RECEIVED, "nothing was exported to the endpoint"
first = RECEIVED[0]
assert first.get("x-api-key") == "smoke-key-not-a-real-secret", first
assert first.get("x-simulation-id") == "sim-livekit", first

# ── What Coval was actually sent ───────────────────────────────────────────────
by_id = {s.get_span_context().span_id: s for s in EXPORTED}
names = {}
for span in EXPORTED:
    names[span.name] = names.get(span.name, 0) + 1


def parent_name(span):
    parent = span.parent
    return by_id[parent.span_id].name if parent and parent.span_id in by_id else None


def only(name):
    found = [s for s in EXPORTED if s.name == name]
    assert len(found) == 1, f"expected exactly one {name}, got {len(found)}"
    return found[0]


# LiveKit's own spans must not be in here. If the module ever hands its provider
# to livekit.agents.telemetry again, this fills up with agent_session, llm_node,
# start_agent_activity and about a hundred others.
assert names == {
    "conversation": 1,
    "turn": 3,
    "stt": 3,
    "vad": 3,
    "llm": 4,
    "tts": 2,
    "llm_tool_call": 2,
    "stt.provider.deepgram": 3,
}, names

# The shape Coval reads. The bug this replaced put stt next to turn rather
# than inside it, because LiveKit parents user_turn and agent_turn as
# siblings, so one exchange arrived as two unrelated rows.
conversation = only("conversation")
assert parent_name(conversation) is None, "conversation must be the only root"
for span in EXPORTED:
    if span is conversation:
        continue
    assert parent_name(span) is not None, f"{span.name} is a second root span"
for name in ("stt", "vad", "llm", "tts"):
    for span in [s for s in EXPORTED if s.name == name]:
        assert parent_name(span) == "turn", f"{name} hangs off {parent_name(span)}, not turn"
for span in [s for s in EXPORTED if s.name == "llm_tool_call"]:
    assert parent_name(span) == "llm", f"llm_tool_call hangs off {parent_name(span)}"
for span in [s for s in EXPORTED if s.name.startswith("stt.provider.")]:
    assert parent_name(span) == "stt", f"{span.name} hangs off {parent_name(span)}"

# One exchange is one turn, and it holds the speech that started it, the model
# call that answered it and the speech that was played back.
turns = {s.attributes["turn.index"]: s for s in EXPORTED if s.name == "turn"}
assert set(turns) == {1, 2, 3}, sorted(turns)
exchange = turns[2].get_span_context().span_id
held = sorted(s.name for s in EXPORTED if s.parent and s.parent.span_id == exchange)
assert held == ["llm", "llm", "llm", "stt", "stt", "tts", "vad", "vad"], held
# Two commits, one sentence, one turn.
assert turns[2].attributes["turn.user_transcript"] == "book a cut for friday"
assert turns[2].attributes["turn.agent_transcript"] == "Friday at three is free."
assert turns[2].attributes["metrics.e2e_latency"] == 1.2
assert turns[2].attributes["turn.tool.call.count"] == 2

# The preemptive round arrived before the user message that opened this turn,
# so it must have moved out of the greeting turn and into this one, and the
# greeting turn must end with its own goodbye rather than stretching until
# that early metric arrived.
stts = sorted(
    (s for s in EXPORTED if s.name == "stt" and s.parent.span_id == exchange),
    key=lambda s: s.start_time,
)
speech = stts[-1]
answers = sorted(
    (s for s in EXPORTED if s.name == "llm" and s.parent.span_id == exchange),
    key=lambda s: s.start_time,
)
assert len(answers) == 3, [a.start_time for a in answers]
assert answers[0].attributes["gen_ai.response.id"] == "req-pre", answers[0].attributes
assert turns[1].start_time == int((T + 0.2) * 1e9), turns[1].start_time
assert turns[1].end_time == int((T + 0.6) * 1e9), turns[1].end_time
assert turns[2].end_time == int((T + 5.1) * 1e9), turns[2].end_time

# The stt span covers the caller's speech, from when they started speaking to
# when the transcript landed, so a preemptive model round reads as overlapping
# the speech it jumped ahead of rather than as a reply that came first.
assert stts[0].start_time == int((T + 2.0) * 1e9), stts[0].start_time
assert stts[0].end_time == int((T + 2.4) * 1e9), stts[0].end_time

# The reply that was actually spoken starts only after the transcript landed.
# The preemptive round may overlap the caller's speech; that is what it is for.
assert speech.end_time <= answers[-1].start_time, "the reply starts before the transcript lands"
assert answers[0].end_time <= answers[1].start_time, "LLM rounds overlap"
assert answers[1].end_time <= answers[2].start_time, "LLM rounds overlap"
# A round that ran tools stretches over them, so no child ends after its parent.
assert answers[1].end_time == int((T + 3.9) * 1e9), answers[1].end_time

# Latency, tokens and text, under the names Coval reads.
assert speech.attributes["transcript"] == "for friday"
assert speech.attributes["stt.confidence"] == 0.94
assert speech.attributes["metrics.ttfb"] == 0.15
assert speech.attributes["stt.providerName"] == "Deepgram"
assert speech.attributes["gen_ai.request.model"] == "nova-3"

assert answers[1].attributes["metrics.ttfb"] == 0.2
assert answers[1].attributes["gen_ai.usage.input_tokens"] == 120
assert answers[1].attributes["gen_ai.usage.output_tokens"] == 18
assert answers[1].attributes["gen_ai.request.model"] == "gpt-4o-mini"
# The second round asked for tools, the third one answered.
assert answers[1].attributes["llm.finish_reason"] == "tool_calls", answers[1].attributes
assert answers[2].attributes["llm.finish_reason"] == "stop", answers[2].attributes

# The prompt the round ran on, which is the thing a failing turn is read
# against and cannot be recovered from a transcript once handoffs move the
# agent between system prompts.
answered = answers[2]
assert answered.attributes["gen_ai.system_instructions"].startswith("You book haircuts.")
assert answered.attributes["agent.label"] == "booking_specialist"
assert list(answered.attributes["tools"]) == [
    "check_availability",
    "create_booking",
]
assert answered.attributes["gen_ai.usage.input_tokens"] == 180, answered.attributes
assert answered.attributes["prompt.message_count"] == 4, answered.attributes
assert answered.attributes["prompt.messages_traced"] == 4, answered.attributes
history = json.loads(answered.attributes["input"])
assert [item.get("role") or item["type"] for item in history] == [
    "system",
    "user",
    "function_call",
    "function_call_output",
], history
# Only the round that spoke carries what was said. The preemptive round was
# superseded and the tool round answered with tool calls, so neither may claim
# the reply.
assert answered.attributes["output"] == "Friday at three is free."
assert answered.attributes["response.length"] == len("Friday at three is free.")
assert answered.attributes["tool_count"] == 2
for early in answers[:2]:
    assert "output" not in early.attributes, early.attributes
    assert "response.length" not in early.attributes, early.attributes

# A history too long for the budget keeps the newest of it and says how much
# there was, rather than sending the whole call on every model request.
long_ctx = _ChatContext(
    [{"type": "message", "role": "user", "content": ["x" * 400]} for _ in range(200)]
)
instructions, prompt, total, traced = tracing._prompt(
    types.SimpleNamespace(instructions="", chat_ctx=long_ctx)
)
assert total == 200, total
assert 0 < traced < total, traced
assert len(prompt) <= tracing.MAX_PROMPT_CHARS + 1, len(prompt)

# A workflow task leaves the instructions attribute empty and keeps its prompt
# as a system message in the context instead. That message is the prompt, so it
# survives truncation however old it gets, and it fills in for the attribute.
aged = _ChatContext(
    [{"type": "message", "role": "system", "content": ["Confirm the draft."]}]
    + [{"type": "message", "role": "user", "content": ["y" * 400]} for _ in range(200)]
)
instructions, prompt, total, traced = tracing._prompt(
    types.SimpleNamespace(instructions="", chat_ctx=aged)
)
assert instructions == "Confirm the draft.", instructions
assert json.loads(prompt)[0]["role"] == "system", "the system prompt was truncated away"
assert total == 201 and traced < total, (total, traced)

# A workflow task group carries LiveKit's placeholder in place of a prompt, and
# reconfigures itself through the context instead. The newest of those entries
# is the prompt in force, and it is what a reader needs to see.
grouped = _ChatContext(
    [
        {"type": "message", "role": "system", "content": [tracing.PLACEHOLDER_INSTRUCTIONS]},
        {"type": "agent_config_update", "instructions": "# Prepare a booking draft"},
        {"type": "agent_config_update", "instructions": "# Confirm one exact draft"},
    ]
)
instructions, _prompt_json, _total, _traced = tracing._prompt(
    types.SimpleNamespace(instructions=tracing.PLACEHOLDER_INSTRUCTIONS, chat_ctx=grouped)
)
assert instructions == "# Confirm one exact draft", instructions

spoken = [s for s in EXPORTED if s.name == "tts" and s.parent.span_id == exchange][0]
assert spoken.attributes["metrics.ttfb"] == 0.25
assert spoken.attributes["tts.characters_count"] == 42
# The provider's own id, so a slow span here can be found in the provider's logs.
assert spoken.attributes["tts.request_id"] == "req-tts-2"
assert answers[2].attributes["gen_ai.response.id"] == "req-llm-2"
assert spoken.attributes["transcript"] == "Friday at three is free."

# A failed tool has to be legible as a failure and as a number: Coval derives
# error and success rates from span status, and its rate metrics average an
# attribute. The result the agent acted on is on the span too.
tools = {s.attributes["function.name"]: s for s in EXPORTED if s.name == "llm_tool_call"}
assert tools["check_availability"].attributes["tool.error"] == 0
assert tools["check_availability"].attributes["function.arguments"] == '{"day":"fri"}'
assert tools["check_availability"].attributes["tool_call_id"] == "c1"
assert tools["check_availability"].attributes["tool.result"] == '{"slots":["15:00"]}'
assert tools["record_complaint"].attributes["tool.error"] == 1
assert tools["record_complaint"].status.status_code is StatusCode.ERROR, (
    "a failed tool call must carry ERROR status"
)
assert tools["record_complaint"].attributes["tool.latency_ms"] > 0

unmeasured = sorted((s for s in EXPORTED if s.name == "llm"), key=lambda s: s.start_time)[-1]
assert "gen_ai.usage.input_tokens" not in unmeasured.attributes, unmeasured.attributes
assert "metrics.ttfb" not in unmeasured.attributes, unmeasured.attributes

# A streaming transcriber has no per-request wait, so it reports exactly zero.
# Copying that would fill Coval's TTFB metric with zeros, which reads as an
# instant transcript rather than as no measurement.
quiet = [s for s in EXPORTED if s.name == "stt" and s.parent.span_id != exchange][0]
assert "metrics.ttfb" not in quiet.attributes, quiet.attributes
assert quiet.attributes["stt.transcription_delay"] == 0, quiet.attributes

# The call's own totals, which is what a run-level trace metric aggregates.
assert conversation.attributes["tool.call.count"] == 2
assert conversation.attributes["tool.failure.count"] == 1
assert conversation.attributes["transcript.turn.count"] == 3
assert conversation.attributes["transcript.user_turn.count"] == 2
assert conversation.attributes["session.id"] == "call-smoke"
assert conversation.attributes["coval.simulation_id"] == "sim-livekit"
assert conversation.attributes["call.duration_seconds"] >= 0
assert [e.name for e in conversation.events] == ["simulation_id_received"], conversation.events

# Free text goes out bounded, so one long tool result cannot push a batch past
# what Coval's ingest accepts.
assert len(tracing._clip("x" * (tracing.MAX_TEXT_CHARS + 500))) == tracing.MAX_TEXT_CHARS + 1

assert shutdown_callbacks, "no flush was registered for process shutdown"

# ── A second call, that no simulation ever claims ──────────────────────────────
#
# This is the local unmute dev case and the production case. The trace must
# still reach Coval: the call is registered as a conversation from the
# transcript already on its own spans, and the spans go out against that
# conversation ID. Nothing here is stubbed except the endpoint, so this drives
# the real shutdown callback, including its worker-thread submit.
import asyncio

RECEIVED.clear()
SUBMITTED.clear()

room2 = _Room()
room2.name = "call-local-dev"
ctx2 = types.SimpleNamespace(
    job=types.SimpleNamespace(metadata=""),
    room=room2,
    add_shutdown_callback=shutdown_callbacks.append,
)
session2 = _Session()
provider2 = tracing.setup_coval(ctx2, session2, metadata={"session.id": room2.name})
local = shutdown_callbacks[-1]

# A fresh call means a fresh router, so this one is recorded separately.
LOCAL_EXPORTED = []
_real_export2 = tracing._router.export


def _recording_export2(spans):
    LOCAL_EXPORTED.extend(spans)
    return _real_export2(spans)


tracing._router.export = _recording_export2

session2.fire(
    "conversation_item_added",
    item=message(
        "user",
        T + 20.0,
        "are you open on sunday",
        metrics={"started_speaking_at": T + 19.5, "stopped_speaking_at": T + 19.9},
    ),
)
session2.fire(
    "metrics_collected",
    metrics=types.SimpleNamespace(
        type="llm_metrics",
        timestamp=T + 20.4,
        duration=0.4,
        ttft=0.2,
        prompt_tokens=40,
        completion_tokens=8,
        total_tokens=48,
        prompt_cached_tokens=0,
        cancelled=False,
        metadata=model("gpt-4o-mini", "openai"),
    ),
)
session2.fire(
    "conversation_item_added",
    item=message("assistant", T + 20.9, "We are open Sunday from ten.", metrics={}),
)

# The session fires its own close before the job runs shutdown callbacks, which
# ends the root span. Firing it here is what makes this the real order: the
# route has to be recorded somewhere that is still writable afterwards.
session2.fire("close", type="close")

# Nothing may have left yet: no simulation claimed this call.
provider2.force_flush()
assert RECEIVED == [], f"exported with no correlation ID at all: {RECEIVED}"

asyncio.run(local())

assert len(SUBMITTED) == 1, SUBMITTED
submitted = SUBMITTED[0]
assert submitted["headers"].get("x-api-key") == "smoke-key-not-a-real-secret", submitted["headers"]
assert submitted["body"]["transcript"] == [
    {"role": "user", "content": "are you open on sunday"},
    {"role": "assistant", "content": "We are open Sunday from ten."},
], submitted["body"]["transcript"]
assert submitted["body"]["external_conversation_id"] == "call-local-dev", submitted["body"]

assert RECEIVED, "the conversation ID did not flush the held spans"
local_export = RECEIVED[0]
# Exactly one correlation header, and it is the conversation one.
assert local_export.get("x-conversation-id") == "conv-from-submit", local_export
assert "x-simulation-id" not in local_export, local_export

# The call still produced Coval's tree, plus the span that records the route.
local_names = sorted({s.name for s in LOCAL_EXPORTED})
assert local_names == ["conversation", "llm", "stt", "transport", "turn"], local_names

# The route is on the trace, so a reader can tell how it was correlated.
marker = [s for s in LOCAL_EXPORTED if s.name == "transport"][-1]
assert marker.attributes["coval.conversation_id"] == "conv-from-submit", marker.attributes
assert marker.attributes["coval.correlation.method"] == "conversation_submit", marker.attributes

print(
    f"coval livekit tracing ok: {len(EXPORTED)} spans, {json.dumps(names, sort_keys=True)}, "
    f"{len(SUBMITTED)} conversation(s) registered"
)
`
