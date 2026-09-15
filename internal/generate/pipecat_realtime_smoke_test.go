//go:build smoke

package generate

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// TestSmokePipecatRealtime drives the emitted realtime bot through the real
// OpenAIRealtimeLLMService with only the socket replaced. The emitted
// build_desk_realtime() runs unchanged and connects to an in-process stand-in
// for the Realtime API, which records every client event the service sends and
// replays the server's side of one call.
//
// What it proves: the model id rides the socket URL rather than a session
// field; the session.update that configures the call carries the rendered
// prompt, the voice under audio.output, the agent's two tools and caller
// transcription under audio.input; the driver's literal turn_detection=False
// puts the session in manual mode, which is what hands the turn to this
// project's own detector rather than the vendor's; the greeting reaches the
// model as a conversation item off the seeded context, not through the append
// frame this library leaves unimplemented; and one tool call runs the emitted
// handler and answers its output back over the same socket.
func TestSmokePipecatRealtime(t *testing.T) {
	harness, _, ok := strings.Cut(devStreamingPipecatScript, "async def exercise(enabled):")
	if !ok {
		t.Fatal("Pipecat streaming harness has no exercise entry point")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	// Built here rather than through runPipecatSmokeScript, because the realtime
	// fixture is not one of the packages examplePackagePath resolves and the
	// LiveKit half of this architecture is being written in parallel: this test
	// owns no file but its own.
	pkg, err := spec.Load(filepath.Join("..", "testdata", "realtime_pipecat"))
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
	checkStreamingOutput(t, runGeneratedPipecatSmokeScript(t, artifact, harness+pipecatRealtimeScript))
}

const pipecatRealtimeScript = `
from pipecat.services.openai.realtime import llm as realtime_llm  # noqa: E402


class FakeRealtimeSocket:
    """The Realtime API's end of the socket.

    Records what the service sends, as parsed JSON, and replays what the server
    would send; the service's own receive loop reads it exactly as it reads the
    real one (llm.py:833 iterates the socket and parses each message).
    """

    def __init__(self, probe, headers):
        self.headers = headers
        self.sent = []
        self.inbound = asyncio.Queue()
        probe.socket = self

    async def send(self, message):
        self.sent.append(json.loads(message))

    async def close(self):
        await self.inbound.put(None)

    def __aiter__(self):
        return self

    async def __anext__(self):
        message = await self.inbound.get()
        if message is None:
            raise StopAsyncIteration
        return message

    async def serve(self, event):
        await self.inbound.put(json.dumps(event))

    def events(self, kind):
        return [event for event in self.sent if event["type"] == kind]


async def exercise_realtime():
    os.environ["UNMUTE_DEV_METRICS"] = "1"
    probe = Probe()
    probe.socket = None
    probe.uri = None
    probe.realtime = None
    patch_providers(probe)

    # The emitted builder runs for real; this only keeps the service it returns,
    # so the test can ask the framework itself what mode the session is in.
    inner_build = bot.build_desk_realtime

    def capture_service():
        probe.realtime = inner_build()
        return probe.realtime

    bot.build_desk_realtime = capture_service

    async def connect(*, uri, additional_headers):
        probe.uri = uri
        return FakeRealtimeSocket(probe, additional_headers)

    realtime_llm.websocket_connect = connect
    capture = Capture()
    call_id = "pipecat-realtime"
    args = RunnerArguments(body={"unmute_dev_call_id": call_id}, session_id=call_id)
    transport = FakeTransport(probe)
    try:
        with redirect_stdout(capture):
            task = asyncio.create_task(bot.run_bot(transport, args))
            try:
                await asyncio.wait_for(probe.started.wait(), timeout=10)
                # The service connects in setup(), at FrameProcessorSetup time,
                # before any StartFrame (llm.py:491-499).
                await until(lambda: probe.socket is not None, "the emitted service did not open its socket")
                socket = probe.socket
                assert socket.headers["Authorization"] == "Bearer " + os.environ["OPENAI_API_KEY"], socket.headers

                # 1. The model id rides the socket URL as a query parameter
                # (llm.py:334), which is why the emitted builder passes it as a
                # setting and not as a session property.
                assert "model=gpt-realtime-2.1" in probe.uri, probe.uri

                # Nothing the emitted module sends may ride the append frame:
                # its handler in this library logs an error and returns
                # (llm.py:693-694), so a greeting or a nudge queued that way is
                # silently dropped. Read before the call is driven, so the
                # reason is this line and not a later timeout.
                assert "LLMMessagesAppendFrame" not in open("bot.py").read(), "the emitted bot queues the append frame this library never implemented"

                # 2. The handshake. The service sends nothing on connect; it
                # waits for session.created, answers with session.update, and
                # only counts the session ready once the server says updated
                # (llm.py:879-890). A first response created before that is held
                # back (llm.py:1176-1179), so a fake that skips session.updated
                # hangs the call.
                await socket.serve({"event_id": "ev_created", "type": "session.created", "session": {}})
                await until(lambda: socket.events("session.update"), "the service did not configure the session")
                await socket.serve({"event_id": "ev_updated", "type": "session.updated", "session": {}})

                # 3. The call opens. The first run frame carries the seeded
                # context, and the service sets the conversation up, re-sends
                # the session with the context folded in, then asks for the
                # first response (llm.py:1186-1210).
                await probe.main.rtvi.set_client_ready()
                await until(lambda: socket.events("response.create"), "the first response was never created", timeout=5)

                # The last session.update is the configured one. The first,
                # sent on session.created, already carries instructions and
                # model because the service syncs its top-level settings into
                # the session properties at construction (llm.py:116-127), so
                # only the context-carrying update proves the tools reached the
                # API.
                session = socket.events("session.update")[-1]["session"]
                assert "Sage and Stone appointment desk" in session["instructions"], session["instructions"]
                assert session["audio"]["output"]["voice"] == "marin", session["audio"]
                assert sorted(tool["name"] for tool in session["tools"]) == ["check_availability", "lookup_customer"], session["tools"]

                # 4. turn_detection: local switches the vendor's own detector
                # off. The driver writes the literal False, which the service
                # holds apart from None: None leaves OpenAI's server VAD on,
                # False is the opt-out that puts the session in manual mode and
                # leaves the turn to the local detector (llm.py:527-540). Ask
                # the framework, not our reading of the wire.
                assert probe.realtime._is_turn_detection_disabled(), "the session kept the vendor's own turn detection"

                # On the wire that False is spelled null, on purpose:
                # SessionUpdateEvent.model_dump rewrites it after the dump
                # (events.py:390-411), which is also why it survives
                # exclude_none. So the key being PRESENT is the whole evidence:
                # a driver writing None instead would be dropped by
                # exclude_none and the key would be gone.
                audio_input = session["audio"]["input"]
                assert "turn_detection" in audio_input, audio_input
                assert audio_input["turn_detection"] is None, audio_input

                # 5. Caller transcription is configured, so the caller's words
                # reach the context at all. SessionProperties() leaves audio
                # unset, and without an input transcription the API sends no
                # transcript events for the caller.
                assert audio_input["transcription"]["model"], audio_input

                # 6. The greeting reaches the model as a conversation item off
                # the seeded context, sent before the response is asked for
                # (llm.py:1195-1200). LLMMessagesAppendFrame is a stub in this
                # library that logs an error and returns (llm.py:693-694), so
                # the emitted bot must not reach for it.
                greetings = [
                    event for event in socket.events("conversation.item.create")
                    if "Hi, this is Sage and Stone Salon" in json.dumps(event["item"])
                ]
                assert greetings, socket.events("conversation.item.create")
                assert socket.sent.index(greetings[0]) < socket.sent.index(socket.events("response.create")[0]), socket.sent

                # 7. One tool call round trip: the API announces the call, the
                # arguments complete, the emitted handler runs and its output
                # goes back as a function_call_output item (llm.py:1378-1385),
                # followed by the response the result is meant to continue.
                responses_before = len(socket.events("response.create"))
                await socket.serve({"event_id": "ev_item", "type": "conversation.item.added", "item": {
                    "id": "item_fc_1", "type": "function_call", "status": "in_progress",
                    "call_id": "call_1", "name": "lookup_customer", "arguments": "",
                }})
                await socket.serve({"event_id": "ev_args", "type": "response.function_call_arguments.done",
                    "response_id": "resp_1", "item_id": "item_fc_1", "output_index": 0,
                    "call_id": "call_1", "arguments": json.dumps({"phone": "+1 555 010 101"})})
                await until(
                    lambda: [e for e in socket.events("conversation.item.create") if e["item"]["type"] == "function_call_output"],
                    "the tool's output was not answered to the API",
                    timeout=5,
                )
                answer = [e for e in socket.events("conversation.item.create") if e["item"]["type"] == "function_call_output"][0]["item"]
                assert answer["call_id"] == "call_1", answer
                assert json.loads(answer["output"]) == {"found": True, "customer_id": "cus_1001", "name": "Alex Morgan"}, answer
                await until(lambda: len(socket.events("response.create")) > responses_before, "the answered tool did not continue the response", timeout=5)
            finally:
                if not task.done():
                    await probe.runner.cancel(reason="realtime smoke cleanup")
                    await asyncio.wait_for(task, timeout=10)
    finally:
        for line in capture.getvalue().splitlines():
            if line.startswith(SENTINEL):
                print(line + "\n", end="")


asyncio.run(exercise_realtime())
print("Pipecat realtime: the session carries the package, turn detection is off, the greeting is an item, and a tool answers")
`
