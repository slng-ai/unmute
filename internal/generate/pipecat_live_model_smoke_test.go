//go:build smoke

package generate

import (
	"strings"
	"testing"
)

// TestSmokeLiveModel drives the emitted realtime_live bot through the real
// OpenAILiveLLMService with only the socket replaced. The emitted
// build_desk_live() runs unchanged and connects to an in-process stand-in for
// the Live API, which records every client event the service sends and replays
// the server's side of one call.
//
// What it proves: the session starts with the rendered prompt, the voice, the
// backend model and the agent's tools, and the opening line is lifted out of the
// startup history; the greeting is delivered as spoken commentary once the
// session is up; the model's own transcript reaches the context and the dev feed
// as its reply, marked as transcribed speech; a caller transcript reaches both as
// the caller's words; a delegated function call runs the registered handler, its
// output is answered to the API and the response is continued only once it has
// finished; and the idle nudge goes in as commentary the model speaks in its own
// words.
func TestSmokeLiveModel(t *testing.T) {
	harness, _, ok := strings.Cut(devStreamingPipecatScript, "async def exercise(enabled):")
	if !ok {
		t.Fatal("Pipecat streaming harness has no exercise entry point")
	}
	checkStreamingOutput(t, runPipecatSmokeScript(t, "realtime_live", nil, nil, harness+pipecatLiveModelScript))
}

const pipecatLiveModelScript = `
from pipecat.frames.frames import BotStoppedSpeakingFrame  # noqa: E402
from pipecat.services.openai.live import llm as live_llm  # noqa: E402


class FakeLiveSocket:
    """The Live API's end of the socket.

    Records what the service sends, as parsed JSON, and replays what the server
    would send; the service's own receive loop reads it exactly as it reads the
    real one.
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


def operations(capture, kind):
    return [record["operation"] for record in capture.latest("operation") if record["operation"]["type"] == kind]


async def exercise_live():
    os.environ["UNMUTE_DEV_METRICS"] = "1"
    probe = Probe()
    probe.socket = None
    probe.aggregators = None
    original_params = bot.LLMUserAggregatorParams
    patch_providers(probe)
    # The shared patch replaces the aggregator's strategies with explicit ones.
    # A live model recommends its own and the emitted call passes none, which is
    # the point, so the emitted parameters go back with the idle timer shortened.
    bot.LLMUserAggregatorParams = lambda **kwargs: original_params(**{**kwargs, "user_idle_timeout": 0.3})
    inner_pair = bot.LLMContextAggregatorPair

    def capture_pair(*args, **kwargs):
        pair = inner_pair(*args, **kwargs)
        probe.aggregators = pair
        return pair

    bot.LLMContextAggregatorPair = capture_pair

    async def connect(*, uri, additional_headers):
        assert uri.startswith("wss://api.openai.com/v1/live"), uri
        return FakeLiveSocket(probe, additional_headers)

    live_llm.websocket_connect = connect
    capture = Capture()
    call_id = "pipecat-live-model"
    args = RunnerArguments(body={"unmute_dev_call_id": call_id}, session_id=call_id)
    transport = FakeTransport(probe)
    try:
        with redirect_stdout(capture):
            task = asyncio.create_task(bot.run_bot(transport, args))
            try:
                await asyncio.wait_for(probe.started.wait(), timeout=5)
                await until(lambda: probe.socket is not None, "the emitted service did not open its socket")
                socket = probe.socket
                assert socket.headers["Authorization"] == "Bearer " + os.environ["OPENAI_API_KEY"], socket.headers

                # 1. The session starts from the first context frame, configured
                # from the package: prompt, voice, backend, tools.
                await probe.main.rtvi.set_client_ready()
                await until(lambda: socket.events("session.start"), "no session.start reached the API")
                session = socket.events("session.start")[0]["session"]
                assert session["model"] == "gpt-live-1", session
                assert session["audio"]["output"]["voice"] == "marin", session
                assert "Sage and Stone" in session["instructions"], session["instructions"]
                assert session["delegation"]["type"] == "responses", session["delegation"]
                responses = session["delegation"]["responses"]
                assert responses["model"] == "gpt-5.6-terra", responses
                assert sorted(tool["name"] for tool in responses["tools"]) == ["check_availability", "lookup_customer"], responses["tools"]
                assert not session.get("input"), "the opening instruction stayed in the startup history: %r" % (session.get("input"),)

                # 2. The greeting is delivered as spoken commentary once the session is up.
                await socket.serve({"type": "session.started", "session": {"id": "sess_smoke", "status": "active", "model": "gpt-live-1"}})
                await until(lambda: socket.events("session.commentary.append"), "the greeting was not delivered as commentary")
                opening = socket.events("session.commentary.append")[0]
                assert opening["delegation_id"] is None, opening
                assert "Hi, this is Sage and Stone Salon" in opening["content"], opening

                # 3. The model speaks: its transcript is its reply on the dev feed,
                # marked as transcribed speech, and closes when the turn does.
                await socket.serve({"type": "session.output_transcript.delta", "delta": "Hi, this is Sage and Stone Salon."})
                await until(lambda: any(r["text"]["text"].startswith("Hi, this is Sage") for r in capture.text("assistant")), "the model's words did not reach the dev feed", timeout=5)
                reply = [r for r in capture.text("assistant") if r["text"]["text"].startswith("Hi, this is Sage")][0]
                assert reply["text"]["origin"] == "transcription", reply
                await until(lambda: any(r["text"]["state"] == "final" for r in capture.text("assistant")), "the model's turn did not close", timeout=5)
                assert [op["state"] for op in operations(capture, "llm")] == ["ended"], operations(capture, "llm")

                # 4. The caller speaks: interim then final words on the dev feed, and
                # the turn the service closes writes them into the context.
                await socket.serve({"type": "session.input_transcript.delta", "delta": "My number is 555 010 101"})
                await until(lambda: capture.text("user"), "the caller's words did not reach the dev feed", timeout=5)
                await until(lambda: any(r["text"]["state"] == "final" and r["text"]["text"] == "My number is 555 010 101" for r in capture.text("user")), "the caller's turn did not close", timeout=5)
                user_aggregator, _assistant = probe.aggregators
                await until(lambda: any(m.get("role") == "user" for m in user_aggregator.context.messages), "the caller's words did not reach the context", timeout=5)
                await until(lambda: any(r["exchange"]["role"] == "input" and r["exchange"]["state"] == "ended" for r in capture.latest("exchange")), "the caller's input exchange did not end when the turn did", timeout=5)

                # 5. The backend delegates a function call: the registered handler
                # runs, its output is answered to the API, and the response is
                # continued only once it has finished.
                await socket.serve({"type": "session.delegation.created", "delegation": {"id": "dlg_1", "target": "responses", "response_id": "resp_1"}})
                await socket.serve({"type": "response.event", "delegation_id": "dlg_1", "event": {"type": "response.created", "response": {"id": "resp_1"}}})
                await socket.serve({"type": "response.event", "delegation_id": "dlg_1", "event": {"type": "response.output_item.done", "item": {
                    "id": "fc_1", "type": "function_call", "status": "completed", "call_id": "call_1",
                    "name": "lookup_customer", "arguments": json.dumps({"phone": "+1 555 010 101"}),
                }}})
                await until(lambda: socket.events("response.item.create"), "the tool's output was not answered to the API", timeout=5)
                answer = socket.events("response.item.create")[0]["item"]
                assert answer["call_id"] == "call_1", answer
                assert json.loads(answer["output"]) == {"found": True, "customer_id": "cus_1001", "name": "Alex Morgan"}, answer
                assert not socket.events("response.create"), "the response was continued before it had finished"
                await socket.serve({"type": "response.event", "delegation_id": "dlg_1", "event": {"type": "response.completed", "response": {"id": "resp_1", "status": "completed"}}})
                await until(lambda: socket.events("response.create"), "the finished response was not continued", timeout=5)
                await until(lambda: [op["state"] for op in operations(capture, "tool")] == ["returned"], "the tool row did not end as returned")
                assert [op["name"] for op in operations(capture, "tool")] == ["lookup_customer"], operations(capture, "tool")

                # 6. The idle nudge goes in as commentary once the bot has stopped
                # speaking and the caller has stayed quiet for the timeout.
                before = len(socket.events("session.commentary.append"))
                await transport.input().push_frame(BotStoppedSpeakingFrame())
                await until(lambda: len(socket.events("session.commentary.append")) > before, "the idle nudge was not sent", timeout=5)
                nudge = socket.events("session.commentary.append")[-1]
                assert nudge["delegation_id"] is None and "still there" in nudge["content"], nudge
            finally:
                if not task.done():
                    await probe.runner.cancel(reason="live model smoke cleanup")
                    await asyncio.wait_for(task, timeout=5)
            # The call has ended: the model's reply counts as completed on the
            # strength of its final transcript, since no synthesis runs after it.
            replies = [r["exchange"] for r in capture.latest("exchange") if r["exchange"]["role"] == "response"]
            assert replies and all(r["state"] == "ended" for r in replies), replies
    finally:
        for line in capture.getvalue().splitlines():
            if line.startswith(SENTINEL):
                print(line + "\n", end="")


asyncio.run(exercise_live())
print("Pipecat live model: the session carries the package, the greeting is spoken, a delegated tool answers, and the nudge is commentary")
`
