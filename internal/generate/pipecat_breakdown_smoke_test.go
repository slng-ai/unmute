//go:build smoke

package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// TestSmokeWhereTheReplysTimeWent drives the emitted Pipecat bot through the
// framework's own latency observer and its own function-call runner, with no
// provider behind either.
//
// What it proves, on the real framework rather than on frames written by hand:
//   - a reply produces a breakdown whose parts are the ones the framework names
//     and whose durations add up to the total it measured, which is what the Go
//     decoder refuses to accept otherwise;
//   - the bot's speech is timed from the transport's own speaking frames, which
//     is the only place a reply's length appears;
//   - a handler that raises ends its row `failed` carrying the error text, and
//     one that runs past its deadline ends `timed_out`, told apart by what the
//     framework sets rather than by log wording.
func TestSmokeWhereTheReplysTimeWent(t *testing.T) {
	harness, _, ok := strings.Cut(devStreamingPipecatScript, "async def exercise(enabled):")
	if !ok {
		t.Fatal("Pipecat streaming harness has no exercise entry point")
	}
	checkStreamingOutput(t, runPipecatSmokeScript(t, "simple-prompt", nil, func(agent *ir.Agent) {
		agent.Tracing = nil
		agent.Prefetch = nil
	}, harness+pipecatBreakdownScript))
}

const pipecatBreakdownScript = `
from pipecat.frames.frames import (  # noqa: E402
    BotStartedSpeakingFrame,
    BotStoppedSpeakingFrame,
    FunctionCallFromLLM,
    VADUserStartedSpeakingFrame,
    VADUserStoppedSpeakingFrame,
)


async def exercise_breakdown():
    os.environ["UNMUTE_DEV_METRICS"] = "1"
    probe = Probe()
    probe.context = None
    patch_providers(probe)
    original_process = FakeLLM.process_frame

    async def remember_context(self, frame, direction):
        # The context the aggregator pushes is what a function call belongs to.
        if isinstance(frame, LLMContextFrame):
            probe.context = frame.context
        await original_process(self, frame, direction)

    FakeLLM.process_frame = remember_context
    capture = Capture()
    call_id = "pipecat-breakdown"
    args = RunnerArguments(body={"unmute_dev_call_id": call_id}, session_id=call_id)
    try:
        with redirect_stdout(capture):
            task = asyncio.create_task(bot.run_bot(FakeTransport(probe), args))
            try:
                await asyncio.wait_for(probe.started.wait(), timeout=5)
                await probe.main.rtvi.set_client_ready()
                await until(lambda: probe.syntheses, "greeting reached TTS")
                probe.syntheses[0][1].set()
                await until(lambda: probe.audio, "greeting played")

                # One reply, from the caller falling silent to the bot speaking.
                # The observer anchors on the VAD frames, so those are what the
                # transport would push here.
                await probe.stt.push_frame(VADUserStartedSpeakingFrame())
                await probe.stt.push_frame(UserStartedSpeakingFrame())
                final = transcript("Book me a haircut")
                await probe.stt.push_frame(final)
                # Turn-stop is a SystemFrame and can overtake queued transcript
                # data, so the turn is only ended once the words have landed.
                await until(lambda: final in probe.transcripts_processed, "the caller's words reached the aggregator")
                await probe.stt.push_frame(VADUserStoppedSpeakingFrame())
                await probe.stt.push_frame(UserStoppedSpeakingFrame())
                await until(lambda: len(probe.requests) == 1, "the turn reached the model")
                request = probe.requests[0]
                await asyncio.sleep(0.05)
                request.first.set()
                request.rest.set()
                await asyncio.wait_for(request.finished.wait(), timeout=5)
                await until(lambda: len(probe.syntheses) == 2, "the reply reached TTS")
                probe.syntheses[1][1].set()
                await probe.stt.push_frame(BotStartedSpeakingFrame())
                await asyncio.sleep(0.05)
                await probe.stt.push_frame(BotStoppedSpeakingFrame())

                await until(lambda: any(r["kind"] == "breakdown" for r in capture.records()),
                            "the framework's breakdown reached the dev feed", timeout=5)
                breakdown = [r for r in capture.records() if r["kind"] == "breakdown"][-1]["breakdown"]
                assert breakdown["measured_from"] in ("user_silence", "client_connected"), breakdown
                assert breakdown["parts"], "a breakdown with no parts should not have been emitted"
                total = sum(part["duration_secs"] for part in breakdown["parts"])
                assert abs(total - breakdown["total_secs"]) <= 1e-6, breakdown
                for part in breakdown["parts"]:
                    assert part["owner_kind"] in ("service", "setting", "bot", "pipeline"), part
                    assert part["key"] and part["label"] and part["owner"], part

                def spoken():
                    return [r for r in capture.latest("measurement")
                            if r["measurement"]["metric"] == "speech_duration"]

                await until(spoken, "the bot's speech was never timed", timeout=5)
                assert spoken()[-1]["measurement"]["state"] == "measured", spoken()[-1]

                # A handler that raises, and one that runs past its deadline.
                llm = probe.llms[0]
                await until(lambda: probe.context is not None, "the model saw a context")

                async def raises(params):
                    raise KeyError("customer_id")

                async def never_returns(params):
                    await asyncio.sleep(5)

                llm.register_function("boom", raises)
                llm.register_function("slow", never_returns, timeout_secs=0.05)
                await llm.run_function_calls([
                    FunctionCallFromLLM(context=probe.context, tool_call_id="call-boom",
                                        function_name="boom", arguments={}),
                    FunctionCallFromLLM(context=probe.context, tool_call_id="call-slow",
                                        function_name="slow", arguments={}),
                ])

                def outcome(name):
                    rows = [r["operation"] for r in capture.latest("operation")
                            if r["operation"]["name"] == name]
                    return rows[-1] if rows else None

                await until(lambda: outcome("boom") and outcome("boom")["state"] == "failed",
                            "the raising handler did not end failed", timeout=5)
                await until(lambda: outcome("slow") and outcome("slow")["state"] == "timed_out",
                            "the handler that ran past its deadline did not end timed out", timeout=5)
                # The type, and never the message beside it: an exception's
                # text quotes whatever the handler was working on.
                assert outcome("boom")["reason"] == "The handler raised KeyError.", outcome("boom")
                assert "customer_id" not in capture.getvalue(), "the handler's message reached the dev feed"
                assert outcome("slow")["reason"], outcome("slow")
            finally:
                if not task.done():
                    for _text, release in probe.syntheses:
                        release.set()
                    for request in probe.requests:
                        request.first.set()
                        request.rest.set()
                    await probe.runner.cancel(reason="breakdown smoke cleanup")
                    await asyncio.wait_for(task, timeout=5)
    finally:
        FakeLLM.process_frame = original_process
        for line in capture.getvalue().splitlines():
            if line.startswith(SENTINEL):
                print(line + "\n", end="")


asyncio.run(exercise_breakdown())
print("Pipecat breakdown: the parts add up, the speech is timed, and a tool says why it produced nothing")
`
