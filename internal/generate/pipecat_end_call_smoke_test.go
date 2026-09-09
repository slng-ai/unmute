//go:build smoke

package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

func TestSmokePipecatEndCallDrainsOneGoodbye(t *testing.T) {
	harness, _, ok := strings.Cut(devStreamingPipecatScript, "async def exercise(enabled):")
	if !ok {
		t.Fatal("Pipecat streaming harness has no exercise entry point")
	}
	for _, fixture := range []string{"simple-prompt", "safe_core"} {
		t.Run(fixture, func(t *testing.T) {
			checkStreamingOutput(t, runPipecatSmokeScript(t, fixture, nil, func(agent *ir.Agent) {
				agent.Tracing = nil
				agent.Prefetch = nil
				agent.Conversation = &ir.Conversation{Greeting: &ir.Greeting{SpeaksFirst: ir.SpeaksFirstAgent, Text: "Welcome."}}
				addBuiltinEndCall(agent)
			}, harness+pipecatEndCallScript))
		})
	}
}

const pipecatEndCallScript = `
from pipecat.frames.frames import CancelFrame, EndFrame, FunctionCallFromLLM

GOODBYE = "Have a lovely day, and goodbye!"


class EndCallLLM(FakeLLM):
    async def process_frame(self, frame, direction):
        await LLMService.process_frame(self, frame, direction)
        if not isinstance(frame, LLMContextFrame):
            await self.push_frame(frame, direction)
            return
        self.probe.model_calls += 1
        await self.push_frame(LLMFullResponseStartFrame())
        await self.start_processing_metrics()
        try:
            if self.probe.model_calls == 1:
                # The real registered tool owns its result and shutdown. This
                # only replaces the provider's native function-call response.
                await self.run_function_calls([FunctionCallFromLLM(
                    context=frame.context, tool_call_id="end-call",
                    function_name="end_call", arguments={},
                )])
            else:
                self.probe.goodbye_started.set()
                await self.probe.goodbye_release.wait()
                await self.push_frame(LLMTextFrame(GOODBYE))
        finally:
            await self.stop_processing_metrics()
            await self.push_frame(LLMFullResponseEndFrame())


class EndCallTTS(FakeTTS):
    async def stop(self, frame):
        self.probe.tts_stops.append(len(self.probe.audio))
        await super().stop(frame)

    async def cancel(self, frame):
        self.probe.tts_stops.append(len(self.probe.audio))
        await super().cancel(frame)


class HeldOutput(PassThrough):
    async def process_frame(self, frame, direction):
        if isinstance(frame, TTSAudioRawFrame) and self.probe.audio:
            self.probe.output_started.set()
            await self.probe.output_release.wait()
        await super().process_frame(frame, direction)


async def exercise_end_call(enabled, cut_at=None):
    os.environ["UNMUTE_DEV_METRICS"] = "1" if enabled else "0"
    probe = Probe()
    probe.model_calls = 0
    probe.tts_stops = []
    probe.goodbye_started, probe.goodbye_release = asyncio.Event(), asyncio.Event()
    probe.output_started, probe.output_release = asyncio.Event(), asyncio.Event()
    patch_providers(probe)
    for name in vars(bot).copy():
        if name.startswith("build_") and name.endswith("_llm"):
            setattr(bot, name, lambda *_args, **_kwargs: EndCallLLM(probe))
        if name.startswith("build_") and name.endswith("_tts"):
            setattr(bot, name, lambda *_args, **_kwargs: EndCallTTS(probe))
    transport = FakeTransport(probe)
    transport._output = HeldOutput(probe, output=True)
    ended = []

    @transport.output().event_handler("on_before_process_frame")
    async def transport_frame(_processor, frame):
        if isinstance(frame, (EndFrame, CancelFrame)):
            ended.append(len(probe.audio))

    capture = Capture()
    call_id = f"pipecat-end-call-{enabled}-{cut_at or 'complete'}"
    args = RunnerArguments(body={"unmute_dev_call_id": call_id}, session_id=call_id)

    async def disconnect(task):
        await probe.runner.cancel(reason=f"caller left during {cut_at}")
        await asyncio.wait_for(task, timeout=5)
        responses = [r["exchange"] for r in capture.latest("exchange") if r["exchange"]["role"] == "response"]
        assert responses[-1]["state"] in ("interrupted", "incomplete"), "A cut goodbye was reported complete"

    try:
        with redirect_stdout(capture):
            task = asyncio.create_task(bot.run_bot(transport, args))
            try:
                await asyncio.wait_for(probe.started.wait(), timeout=5)
                await probe.main.rtvi.set_client_ready()
                await until(lambda: probe.syntheses, "End-call greeting reached TTS")
                probe.syntheses[0][1].set()
                await until(lambda: probe.audio, "End-call greeting played")
                greeting_frames = len(probe.audio)
                await probe.stt.push_frame(UserStartedSpeakingFrame())
                final = transcript("Awesome, thank you very much.")
                await probe.stt.push_frame(final)
                await until(lambda: final in probe.transcripts_processed, "End-call input reached native aggregator")
                await probe.stt.push_frame(UserStoppedSpeakingFrame())
                await until(lambda: probe.goodbye_started.is_set() or probe.tts_stops, "end_call produced neither model follow-up nor shutdown")
                assert not probe.tts_stops and not ended and not task.done(), "end_call closed while the goodbye model was still running"
                assert len(probe.syntheses) == 1 and len(probe.audio) == greeting_frames
                if enabled:
                    assert capture.latest("call")[0]["call"]["state"] == "open"
                if cut_at == "model":
                    await disconnect(task)
                    return
                probe.goodbye_release.set()
                await until(lambda: len(probe.syntheses) > 1 or probe.tts_stops, "end_call produced neither goodbye nor shutdown")
                assert not probe.tts_stops, "end_call stopped TTS before its goodbye"
                assert len(probe.syntheses) == 2 and probe.syntheses[1][0] == GOODBYE
                await asyncio.sleep(0.05)
                assert probe.model_calls == 2, "end_call requested more than one goodbye"
                assert not ended and not task.done() and len(probe.audio) == greeting_frames
                if enabled:
                    assert capture.latest("call")[0]["call"]["state"] == "open"
                    assert assistant_words(capture).count(GOODBYE) == 1
                if cut_at == "synthesis":
                    await disconnect(task)
                    return
                probe.syntheses[1][1].set()
                await until(probe.output_started.is_set, "Goodbye synthesis did not reach the held output")
                assert not ended and not task.done() and len(probe.audio) == greeting_frames, "Session ended before goodbye output drained"
                if enabled:
                    assert capture.latest("call")[0]["call"]["state"] == "open"
                probe.output_release.set()
                await until(task.done, "end_call left the runner and transport open", timeout=5)
                await task
                assert len(probe.audio) == greeting_frames + 1, "Goodbye audio was lost or repeated"
                assert ended and all(count == len(probe.audio) for count in ended), "Transport ended before goodbye audio drained"
                assert probe.tts_stops and all(count == len(probe.audio) for count in probe.tts_stops), "TTS stopped before goodbye audio drained"
                assert probe.model_calls == 2 and len(probe.syntheses) == 2
                if enabled:
                    assert capture.latest("call")[0]["call"]["state"] == "ended"
                    tools = [r["operation"] for r in capture.latest("operation") if r["operation"]["type"] == "tool"]
                    assert len(tools) == 1 and tools[0]["name"] == "end_call" and tools[0]["state"] == "returned"
                    assert all(r["operation"]["state"] != "running" for r in capture.latest("operation")), "Shutdown left a running operation"
                    responses = [r["exchange"] for r in capture.latest("exchange") if r["exchange"]["role"] == "response"]
                    assert all(response["state"] == "ended" for response in responses), "Completed greeting or goodbye was marked incomplete"
                    assert all(response["playback"] == "unknown" for response in responses), "TTS completion invented native playback evidence"
                else:
                    assert not capture.records(), "Disabled metrics emitted records"
            finally:
                if not task.done():
                    probe.goodbye_release.set()
                    probe.output_release.set()
                    for _text, release in probe.syntheses:
                        release.set()
                    await probe.runner.cancel(reason="end-call smoke cleanup")
                    await asyncio.wait_for(task, timeout=5)
    finally:
        for line in capture.getvalue().splitlines():
            if line.startswith(SENTINEL):
                print(line)


async def main():
    await exercise_end_call(True)
    await exercise_end_call(False)
    await exercise_end_call(True, cut_at="model")
    await exercise_end_call(True, cut_at="synthesis")


asyncio.run(main())
print("Pipecat end_call: one goodbye drains before TTS, transport and runner stop")
`
