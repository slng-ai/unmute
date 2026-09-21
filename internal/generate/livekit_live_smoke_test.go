//go:build smoke

package generate

import "testing"

// TestSmokeLiveKitLiveModel drives the emitted live_model LiveKit project
// through the real GPTLiveModel with only its socket replaced. The emitted
// entrypoint runs unchanged: it builds the model, builds the session, starts the
// agent and installs its own handlers, and what it built then talks to an
// in-process stand-in for the Live API, which records every client event the
// plugin sends and replays the server's side of one call.
//
// What it proves, which is the Pipecat side's list (TestSmokeLiveModel) read
// against this framework:
//   - the session is constructed with the model and nothing else, so the four
//     absences the architecture promises are absences in the object the
//     framework receives and not only in the emitted text;
//   - session.start carries the package: the rendered prompt, the voice, the
//     backend model and the agent's two tools, with no startup history;
//   - the greeting is delivered as an instruction the model paraphrases, and
//     session.say() is not a route it could have taken;
//   - the model's words and the caller's words both reach the session history;
//   - a delegated function call runs the registered handler, its output is
//     answered to the API by call id, and the response is continued only once it
//     has finished;
//   - the caller going quiet puts the nudge in as another instruction.
//
// Two things the harness supplies, because a smoke has no LiveKit server and no
// twenty seconds to wait: the room is dropped from session.start(), and
// user_away_timeout is shortened. Both are recorded on the way past, so the
// emitted value is asserted rather than replaced silently.
func TestSmokeLiveKitLiveModel(t *testing.T) {
	runLiveKitSmokeScript(t, "live_model", nil, nil, livekitLiveModelScript)
}

const livekitLiveModelScript = `"""Smoke check: the emitted live session against the real GPT-Live model."""
# ruff: noqa: E402 - the environment has to be seeded before the module imports
import asyncio
import base64
import json
import os
import struct
from types import SimpleNamespace

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import aiohttp
import agent
from livekit import rtc
from livekit.agents import AgentSession, io
from livekit.plugins.openai.realtime import gpt_live_model

# Short enough to wait for, and asserted against the emitted 20 before it is
# applied, so shortening it cannot hide the emitted value changing.
AWAY_AFTER = 0.4

# One 10 ms frame of something audible. The adapter attaches the model's words
# to the sound that carries them and only closes a message when the sound stops
# (llm/duplex_adapter.py:373-397), so a transcript with no audio never becomes a
# message. FixedGate opens three times above the plugin's declared silence of
# 0.0006 (realtime/gpt_live_model.py:47, llm/duplex_adapter.py:92-95).
SPEECH = base64.b64encode(struct.pack("<240h", *([9000, -9000] * 120))).decode()

GREETING = "Hi, this is Sage and Stone Salon. How can I help?"


class FakeSocket:
    """The Live API's end of the websocket, in the shape aiohttp hands over.

    The plugin's own send and receive loops read it exactly as they read a real
    one: send_str, receive() returning a typed message, close()
    (realtime/gpt_live_model.py:471-575).
    """

    def __init__(self, url, headers):
        self.url = url
        self.headers = headers
        self.sent = []
        self.inbound = asyncio.Queue()

    async def send_str(self, message):
        self.sent.append(json.loads(message))

    async def receive(self):
        message = await self.inbound.get()
        if message is None:
            return SimpleNamespace(type=aiohttp.WSMsgType.CLOSED, data=None)
        return SimpleNamespace(type=aiohttp.WSMsgType.TEXT, data=json.dumps(message))

    async def close(self):
        await self.inbound.put(None)

    async def serve(self, event):
        await self.inbound.put(event)

    def events(self, kind):
        return [event for event in self.sent if event.get("type") == kind]


class FakeHTTP:
    """Stands in for the plugin's aiohttp session, and only for its ws_connect.

    This is the seam: _create_ws_conn builds the url and the headers itself and
    then calls _ensure_http_session().ws_connect(url=..., headers=...)
    (realtime/gpt_live_model.py:445-463). Replacing the session rather than
    _create_ws_conn leaves the url and the Authorization header real, which is
    why the script can assert them.
    """

    def __init__(self, probe):
        self.probe = probe

    async def ws_connect(self, *, url, headers):
        socket = FakeSocket(url, headers)
        self.probe.socket = socket
        return socket


probe = SimpleNamespace(socket=None, session=None, kwargs=None)
gpt_live_model.GPTLiveModel._ensure_http_session = lambda self: FakeHTTP(probe)


class Microphone(io.AudioInput):
    """The caller's open microphone, sending silence.

    Their turn ends on their own audio rather than on a timer: push_audio counts
    the quiet since the last transcript fragment and closes the message at
    _MIN_SILENCE_MS (realtime/gpt_live_model.py:906-911).
    """

    def __init__(self):
        super().__init__(label="live-smoke-mic")

    async def __anext__(self):
        await asyncio.sleep(0.02)
        return rtc.AudioFrame(
            data=b"\x00\x00" * 2400, sample_rate=24000, num_channels=1, samples_per_channel=2400
        )


class Speaker(io.AudioOutput):
    """Somewhere for the model's voice to land.

    Without one the playout never finishes, the agent never returns to
    listening, and the away timer is never armed: it is set only while both
    sides are listening (voice/agent_session.py:2026-2029, 2069-2072).
    """

    def __init__(self):
        super().__init__(label="live-smoke-output", capabilities=io.AudioOutputCapabilities(pause=False))
        self.position = 0.0

    async def capture_frame(self, frame):
        first = self.position == 0.0
        await super().capture_frame(frame)
        self.position += frame.duration
        if first:
            self.on_playback_started(created_at=asyncio.get_running_loop().time())

    def flush(self):
        super().flush()
        if self.position:
            position, self.position = self.position, 0.0
            self.on_playback_finished(playback_position=position, interrupted=False)

    def clear_buffer(self):
        self.position = 0.0


class HarnessSession(AgentSession):
    """The emitted session, with the two things a smoke cannot have.

    The room is dropped, because there is no LiveKit server to join, and the
    away window is shortened. Everything the emitted entrypoint passes is
    recorded first, so both substitutions are visible to the assertions rather
    than hidden behind them.
    """

    def __init__(self, **kwargs):
        probe.kwargs = dict(kwargs)
        super().__init__(**{**kwargs, "user_away_timeout": AWAY_AFTER})
        probe.session = self

    async def start(self, *args, **kwargs):
        kwargs.pop("room", None)
        self.output.audio = Speaker()
        self.input.audio = Microphone()
        return await super().start(*args, **kwargs)


agent.AgentSession = HarnessSession


async def until(predicate, message, timeout=15):
    async with asyncio.timeout(timeout):
        while not predicate():
            await asyncio.sleep(0.01)
    assert predicate(), message


def spoken(session):
    return [
        item.text_content
        for item in session.history.items
        if getattr(item, "role", None) == "assistant" and item.text_content
    ]


def heard(session):
    return [
        item.text_content
        for item in session.history.items
        if getattr(item, "role", None) == "user" and item.text_content
    ]


async def speak(socket, transcript):
    """One utterance, the way the service sends one: sound, words, sound."""
    for _ in range(6):
        await socket.serve({"type": "session.output_audio.delta", "delta": SPEECH})
    await socket.serve({"type": "session.output_transcript.delta", "delta": transcript})
    for _ in range(6):
        await socket.serve({"type": "session.output_audio.delta", "delta": SPEECH})


async def main():
    ctx = SimpleNamespace(
        room=SimpleNamespace(name="livekit-live-smoke"),
        connect=lambda: asyncio.sleep(0),
    )
    await agent.entrypoint(ctx)
    session = probe.session
    assert session is not None, "the emitted entrypoint built no session"
    try:
        # 1. The session is the model, its retry cap and nothing else. A
        # transcriber, a synthesizer, a turn setting or a detector would be a
        # keyword here.
        assert set(probe.kwargs) == {"llm", "conn_options", "user_away_timeout"}, probe.kwargs
        assert isinstance(probe.kwargs["llm"], gpt_live_model.GPTLiveModel), probe.kwargs["llm"]
        assert probe.kwargs["user_away_timeout"] == 20, probe.kwargs
        # One retry for the reasoning model, not the framework's three: a
        # caller cannot wait out 4.1 seconds of sleep between the attempts. The
        # other two roles keep the default.
        assert probe.kwargs["conn_options"].llm_conn_options.max_retry == 1, probe.kwargs["conn_options"]

        await until(lambda: probe.socket is not None, "the emitted session never opened a socket")
        socket = probe.socket
        assert socket.url == "wss://api.openai.com/v1/live/sessions", socket.url
        assert socket.headers["Authorization"] == "Bearer " + os.environ["OPENAI_API_KEY"], (
            "the key the session presents is not the one the package declared"
        )

        # 2. session.start carries the package: prompt, voice, backend, tools.
        await until(lambda: socket.events("session.start"), "no session.start reached the API")
        config = socket.events("session.start")[0]["session"]
        assert config["model"] == "gpt-live-1", config
        assert config["audio"]["output"]["voice"] == "marin", config
        assert "Sage and Stone" in config["instructions"], config["instructions"]
        assert config["delegation"]["type"] == "responses", config["delegation"]
        responses = config["delegation"]["responses"]
        assert responses["model"] == "gpt-5.6-terra", responses
        assert sorted(tool["name"] for tool in responses["tools"]) == [
            "check_availability",
            "lookup_customer",
        ], responses["tools"]
        assert not config.get("input"), "a call that has not started yet has history: %r" % (config.get("input"),)

        # 3. The greeting is an instruction, and speaking it was never a route:
        # the adapter declares supports_say=False (llm/duplex_adapter.py:268) and
        # say() refuses on a session with no synthesizer
        # (voice/agent_activity.py:1557-1565).
        await socket.serve({"type": "session.started", "session": {"id": "sess_smoke", "status": "active", "model": "gpt-live-1"}})
        await until(lambda: socket.events("session.commentary.append"), "the greeting was not delivered as an instruction")
        opening = socket.events("session.commentary.append")[0]
        assert opening["delegation_id"] is None, opening
        assert GREETING in opening["content"], opening
        try:
            session.say("Hi, this is Sage and Stone Salon.")
        except RuntimeError:
            pass
        else:
            raise AssertionError("session.say() worked; the greeting could have been spoken word for word")

        # 4. The model speaks, and its words are its turn in the history.
        await speak(socket, "Hi, this is Sage and Stone Salon.")
        await until(lambda: spoken(session), "the model's words never reached the session history")
        assert spoken(session) == ["Hi, this is Sage and Stone Salon."], spoken(session)

        # 5. The caller speaks, and the model reports it: no transcriber ran.
        await socket.serve({"type": "session.input_transcript.delta", "delta": "My number is 555 010 101", "start_ms": 0, "end_ms": 1000})
        await until(lambda: heard(session), "the caller's words never reached the session history")
        assert heard(session) == ["My number is 555 010 101"], heard(session)

        # 6. The backend delegates a function call: the emitted handler runs, its
        # output is answered by call id, and the response is continued only once
        # it has finished asking.
        await socket.serve({"type": "session.delegation.created", "delegation": {"id": "dlg_1", "target": "responses", "response_id": "resp_1"}})
        await socket.serve({"type": "response.event", "delegation_id": "dlg_1", "event": {"type": "response.created", "response": {"id": "resp_1"}}})
        await socket.serve({"type": "response.event", "delegation_id": "dlg_1", "event": {"type": "response.output_item.done", "item": {
            "id": "fc_1", "type": "function_call", "status": "completed", "call_id": "call_1",
            "name": "lookup_customer", "arguments": json.dumps({"phone": "+1 555 010 101"}),
        }}})
        await until(lambda: socket.events("response.item.create"), "the tool's output was not answered to the API")
        answer = socket.events("response.item.create")[0]["item"]
        assert answer["call_id"] == "call_1", answer
        # The framework hands a dict back as its repr, so the real tool's own
        # values are what is asserted rather than the spelling.
        for fragment in ("'found': True", "'customer_id': 'cus_1001'", "'name': 'Alex Morgan'"):
            assert fragment in answer["output"], answer
        assert not socket.events("response.create"), "the response was continued before it had finished"
        await socket.serve({"type": "response.event", "delegation_id": "dlg_1", "event": {"type": "response.completed", "response": {"id": "resp_1", "status": "completed"}}})
        await until(lambda: socket.events("response.create"), "the finished response was not continued")

        # 7. The model answers the caller, both sides fall quiet, and the nudge
        # goes in as another instruction rather than as a line to read out.
        before = len(socket.events("session.commentary.append"))
        await speak(socket, "You are in our records, Alex.")
        await until(lambda: session.agent_state == "listening", "the agent never finished answering")
        await until(lambda: len(socket.events("session.commentary.append")) > before, "the idle nudge was never sent")
        nudge = socket.events("session.commentary.append")[-1]
        assert nudge["delegation_id"] is None, nudge
        assert "still there" in nudge["content"], nudge
    finally:
        await session.aclose()


asyncio.run(main())
print("LiveKit live model: the session is the model alone, the greeting and the nudge are instructions, and a delegated tool answers")
`
