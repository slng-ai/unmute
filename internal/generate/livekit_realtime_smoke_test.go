//go:build smoke

package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// TestSmokeLiveKitRealtime drives the emitted realtime_model LiveKit project
// through the real RealtimeModel with only its socket replaced. The emitted
// entrypoint runs unchanged: it warms the VAD, builds the model, builds the
// session, starts the agent and installs its own handlers, and what it built
// then talks to an in-process stand-in for the Realtime API, which records every
// client event the plugin sends and replays the server's side of one call.
//
// What it proves, which is spec 024 SC-002 read against this framework:
//   - the session is built with llm= and the emitted extras, so the absences
//     architecture: realtime promises are absences in the object the framework
//     receives and not only in the emitted text;
//   - the session.update events that reach the wire carry the package: the model
//     id, the voice, the rendered prompt and the agent's two tools;
//   - turn_detection: local sends the model no turn-detection argument, which is
//     the one lowering here whose correct emission is silence, and the framework
//     then switches the server's own detection off on the wire;
//   - the greeting reaches the model as an instruction, and speaking it was
//     never a route this session could have taken;
//   - the model's words and the caller's words both reach the session history
//     with no transcriber and no synthesizer in the pipeline;
//   - one tool call runs the emitted handler, its result is answered by call id,
//     and the model is asked to continue only once the answer is in the context.
//
// One thing the harness supplies, because a smoke has no LiveKit server: the
// room is dropped from session.start(). Nothing else is replaced or shortened;
// the ten-second generate_reply deadline (realtime_model.py:1731) is met by
// answering rather than by moving it.
func TestSmokeLiveKitRealtime(t *testing.T) {
	runLiveKitRealtimeSmokeScript(t, livekitRealtimeSmokeScript)
}

// runLiveKitRealtimeSmokeScript is runLiveKitSmokeScript for a package under
// internal/testdata that examplePackagePath does not name. It is written here
// rather than added to that list because the realtime fixture is this file's
// alone: the Pipecat half of architecture: realtime carries its own.
func runLiveKitRealtimeSmokeScript(t *testing.T, script string) []byte {
	t.Helper()
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	artifact := generateFor(t, "realtime_model", ir.ProviderLiveKit)

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

	cmd := uvCommand("run", "python", "smoke_check.py")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("smoke check failed:\n%s", out)
	} else if strings.Contains(string(out), "--- Logging error ---") {
		t.Fatalf("smoke check logged an internal formatting error:\n%s", out)
	}
	t.Logf("%s", out)
	return out
}

const livekitRealtimeSmokeScript = `"""Smoke check: the emitted realtime session against the real RealtimeModel."""
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
from livekit.agents import AgentSession, inference, io
from livekit.plugins.openai.realtime import RealtimeModel, realtime_model
from openai.types.realtime import realtime_audio_input_turn_detection as openai_realtime

# One 10 ms frame of something audible. The model's words are carried by its
# audio, so a reply with a transcript and no sound never reaches the output.
SPEECH = base64.b64encode(struct.pack("<240h", *([9000, -9000] * 120))).decode()

GREETING = "Hi, this is Sage and Stone Salon. How can I help?"


class FakeSocket:
    """The Realtime API's end of the websocket, in the shape aiohttp hands over.

    The plugin's own send and receive loops read it exactly as they read a real
    one: send_str, receive() returning a typed message, close()
    (realtime_model.py:1111-1180).
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
    (realtime_model.py:1068-1079). Replacing the session rather than
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
realtime_model.RealtimeModel._ensure_http_session = lambda self: FakeHTTP(probe)


class Speaker(io.AudioOutput):
    """Somewhere for the model's voice to land.

    Without one the playout never finishes and the agent never returns to
    listening, so the reply after the tool would never settle.
    """

    def __init__(self):
        super().__init__(
            label="realtime-smoke-output",
            capabilities=io.AudioOutputCapabilities(pause=False),
        )
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
    """The emitted session, with the one thing a smoke cannot have.

    The room is dropped, because there is no LiveKit server to join. Everything
    the emitted entrypoint passes is recorded first, so the substitution is
    visible to the assertions rather than hidden behind them.
    """

    def __init__(self, **kwargs):
        probe.kwargs = dict(kwargs)
        super().__init__(**kwargs)
        probe.session = self

    async def start(self, *args, **kwargs):
        kwargs.pop("room", None)
        self.output.audio = Speaker()
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


async def speak(socket, response_id, item_id, transcript):
    """One reply, the way the service sends one: an item, its part, sound, words."""
    await socket.serve({
        "type": "response.output_item.added", "response_id": response_id, "output_index": 0,
        "item": {"id": item_id, "type": "message", "role": "assistant",
                 "status": "in_progress", "content": []},
    })
    await socket.serve({
        "type": "conversation.item.added", "previous_item_id": None,
        "item": {"id": item_id, "type": "message", "role": "assistant",
                 "status": "in_progress", "content": []},
    })
    # part.type is what decides the message's modalities
    # (realtime_model.py:1939-1956), so an audio reply says so here.
    await socket.serve({
        "type": "response.content_part.added", "response_id": response_id, "item_id": item_id,
        "output_index": 0, "content_index": 0, "part": {"type": "audio", "transcript": ""},
    })
    for _ in range(4):
        await socket.serve({
            "type": "response.output_audio.delta", "response_id": response_id,
            "item_id": item_id, "output_index": 0, "content_index": 0, "delta": SPEECH,
        })
    await socket.serve({
        "type": "response.output_audio_transcript.delta", "response_id": response_id,
        "item_id": item_id, "output_index": 0, "content_index": 0, "delta": transcript,
    })
    await socket.serve({
        "type": "response.output_item.done", "response_id": response_id, "output_index": 0,
        "item": {"id": item_id, "type": "message", "role": "assistant",
                 "status": "completed", "content": []},
    })


async def finish(socket, response_id):
    """response.done is what moves a reply's transcript onto the chat item.

    The transcript is appended to the remote item only here
    (realtime_model.py:2199), so a reply that has not finished is not yet in the
    session history however many deltas arrived.
    """
    await socket.serve({
        "type": "response.done",
        "response": {"id": response_id, "status": "completed", "output": []},
    })


async def main():
    proc = SimpleNamespace(userdata={})
    agent.prewarm(proc)
    ctx = SimpleNamespace(
        room=SimpleNamespace(name="livekit-realtime-smoke"),
        proc=proc,
        connect=lambda: asyncio.sleep(0),
    )
    await agent.entrypoint(ctx)
    session = probe.session
    assert session is not None, "the emitted entrypoint built no session"
    try:
        # 1. The session is the one model, its retry cap and the framework's
        # own turn taking. A transcriber, a synthesizer or a second reasoning
        # model would be a keyword here.
        assert set(probe.kwargs) == {"llm", "conn_options", "turn_handling", "vad", "user_away_timeout"}, probe.kwargs
        model = probe.kwargs["llm"]
        assert isinstance(model, RealtimeModel), model
        assert isinstance(
            probe.kwargs["turn_handling"]["turn_detection"], inference.TurnDetector
        ), probe.kwargs["turn_handling"]
        # One retry for the reasoning model, not the framework's three: a
        # caller cannot wait out 4.1 seconds of sleep between the attempts. The
        # other two roles keep the default.
        assert probe.kwargs["conn_options"].llm_conn_options.max_retry == 1, probe.kwargs["conn_options"]

        await until(lambda: probe.socket is not None, "the emitted session never opened a socket")
        socket = probe.socket
        assert socket.url == "wss://api.openai.com/v1/realtime?model=gpt-realtime", socket.url
        assert socket.headers["Authorization"] == "Bearer " + os.environ["OPENAI_API_KEY"], (
            "the key the session presents is not the one the package declared"
        )

        # 2. The session.update events carry the package. There are three: the
        # options, queued before the socket exists (realtime_model.py:908), the
        # instructions (realtime_model.py:1648) and the tools
        # (realtime_model.py:1633).
        await until(
            lambda: len(socket.events("session.update")) >= 3,
            "the package never reached the wire",
        )
        options, instructions, tools = (
            event["session"] for event in socket.events("session.update")[:3]
        )
        assert options["model"] == "gpt-realtime", options
        assert options["audio"]["output"]["voice"] == "marin", options
        assert options["output_modalities"] == ["audio"], options
        assert "Sage and Stone" in instructions["instructions"], instructions
        assert sorted(tool["name"] for tool in tools["tools"]) == [
            "check_availability",
            "lookup_customer",
        ], tools["tools"]
        assert tools["tools"][1]["parameters"]["required"] == ["phone"], tools["tools"][1]

        # 3. turn_detection: local sends the model no turn-detection argument,
        # and this is the assertion the whole file exists for.
        #
        # can_disable_turn_detection is "not is_given(turn_detection)"
        # (realtime_model.py:484), so a value of any kind, the vendor's own
        # default included, would cost the framework the right to decide the
        # turn. The control below is the same model built the way
        # turn_detection: semantic lowers, so the True above is earned by the
        # absence rather than true of every model.
        assert model.capabilities.can_disable_turn_detection is True, (
            "the emitted module passed a turn-detection argument, so the framework "
            "can no longer take the turn decision back"
        )
        pinned = RealtimeModel(
            api_key="control",
            turn_detection=openai_realtime.SemanticVad(type="semantic_vad"),
        )
        assert pinned.capabilities.can_disable_turn_detection is False, (
            "a pinned turn_detection no longer costs the framework the decision; "
            "the assertion above has stopped discriminating"
        )

        # And the framework then does take it: with a VAD loaded and a client-side
        # detector given (agent_activity.py:349-392) it opens the session with
        # turn_detection_disabled (agent_activity.py:1097-1101), whose session copy
        # is turn_detection=None (realtime_model.py:891) and reaches the wire as
        # null (realtime_model.py:1327). Null is the opt-out, and it is a stronger
        # statement than an absent key: absent is the server's own default, which
        # is semantic VAD.
        assert options["audio"]["input"]["turn_detection"] is None, (
            "server-side turn detection is still on: " + repr(options["audio"]["input"])
        )

        # 4. The greeting is put to the model as an instruction, and speaking it
        # was never a route: the model reports supports_say=False, and say()
        # refuses on a session with no synthesizer (agent_activity.py:1555-1565).
        await until(lambda: socket.events("response.create"), "the greeting was never delivered")
        opening = socket.events("response.create")[0]
        assert GREETING in opening["response"]["instructions"], opening
        assert "Sage and Stone appointment desk" in opening["response"]["instructions"], (
            "the response instructions replace the session ones, so the prompt has to ride along"
        )
        assert model.capabilities.supports_say is False, model.capabilities
        try:
            session.say(GREETING)
        except RuntimeError:
            pass
        else:
            raise AssertionError("session.say() worked; the greeting could have been read out")

        # 5. The model speaks. response.created has to echo the event id within
        # ten seconds or the future fails (realtime_model.py:1695-1744), so the
        # deadline is met by answering rather than by moving it.
        await socket.serve({
            "type": "response.created",
            "response": {"id": "resp_1", "status": "in_progress",
                         "metadata": {"client_event_id": opening["event_id"]}},
        })
        await speak(socket, "resp_1", "item_1", GREETING)
        await finish(socket, "resp_1")
        await until(lambda: spoken(session), "the model's words never reached the session history")
        assert spoken(session) == [GREETING], spoken(session)

        # 6. The caller speaks, and the model reports it: no transcriber ran.
        # The transcript lands on the item the server already announced
        # (realtime_model.py:2032-2041).
        await socket.serve({
            "type": "conversation.item.added", "previous_item_id": "item_1",
            "item": {"id": "item_2", "type": "message", "role": "user", "status": "completed",
                     "content": [{"type": "input_audio", "transcript": None}]},
        })
        await socket.serve({
            "type": "conversation.item.input_audio_transcription.completed",
            "item_id": "item_2", "content_index": 0, "transcript": "My number is 555 010 101",
        })
        await until(lambda: heard(session), "the caller's words never reached the session history")
        assert heard(session) == ["My number is 555 010 101"], heard(session)

        # 7. The model calls a tool: the emitted handler runs, its output is
        # answered by call id, and the model is asked to continue only once the
        # answer is in the context.
        await socket.serve({
            "type": "response.created",
            "response": {"id": "resp_2", "status": "in_progress"},
        })
        await socket.serve({
            "type": "response.output_item.done", "response_id": "resp_2", "output_index": 0,
            "item": {"id": "fc_1", "type": "function_call", "status": "completed",
                     "call_id": "call_1", "name": "lookup_customer",
                     "arguments": json.dumps({"phone": "+1555010101"})},
        })
        await finish(socket, "resp_2")
        await until(
            lambda: socket.events("conversation.item.create"),
            "the tool's output was not answered to the API",
        )
        answer = socket.events("conversation.item.create")[0]["item"]
        assert answer["call_id"] == "call_1", answer
        assert answer["type"] == "function_call_output", answer
        # The framework hands a dict back as its repr, so the real tool's own
        # values are what is asserted rather than the spelling.
        for fragment in ("'found': True", "'customer_id': 'cus_1001'", "'name': 'Alex Morgan'"):
            assert fragment in answer["output"], answer
        assert len(socket.events("response.create")) == 1, (
            "the model was asked to continue before its answer was acknowledged"
        )
        await socket.serve({
            "type": "conversation.item.added",
            "previous_item_id": socket.events("conversation.item.create")[0]["previous_item_id"],
            "item": dict(answer, status="completed"),
        })
        await until(
            lambda: len(socket.events("response.create")) > 1,
            "the model was never asked to answer with the tool's result",
        )
        follow = socket.events("response.create")[1]
        await socket.serve({
            "type": "response.created",
            "response": {"id": "resp_3", "status": "in_progress",
                         "metadata": {"client_event_id": follow["event_id"]}},
        })
        await speak(socket, "resp_3", "item_3", "You are in our records, Alex.")
        await finish(socket, "resp_3")
        await until(lambda: len(spoken(session)) > 1, "the model never answered after the tool")
        assert spoken(session)[1] == "You are in our records, Alex.", spoken(session)

        # Nothing else went to the API: no audio was pushed, because no
        # transcriber and no caller microphone are part of this session.
        assert not socket.events("input_audio_buffer.append"), "the session pushed audio of its own"
    finally:
        await session.aclose()


asyncio.run(main())
print("LiveKit realtime: one model carries the package, the framework keeps the turn, and a native tool answers")
`
