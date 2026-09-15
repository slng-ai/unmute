//go:build smoke

package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// TestSmokeListenerDecidesTheTurn drives the emitted turn_listener bot through
// the framework's real eager path with a stand-in for Deepgram Flux built on the
// same mixin the real service uses. The emitted constructor call is what runs:
// the stand-in records the flag and the ceiling the bot passed, then plays a
// turn the way Flux reports one (proposed start, predicted end, resumed speech,
// predicted end again, committed end).
//
// What it proves: the speculative reply is generated on the prediction, held
// inside the LLM service, dropped when the caller resumes, and released once the
// committed transcript matches; one reply is spoken for the turn and no second
// inference runs for it; the withdrawn prediction never reaches the caller or the
// dev feed as caller text.
func TestSmokeListenerDecidesTheTurn(t *testing.T) {
	harness, _, ok := strings.Cut(devStreamingPipecatScript, "async def exercise(enabled):")
	if !ok {
		t.Fatal("Pipecat streaming harness has no exercise entry point")
	}
	checkStreamingOutput(t, runPipecatSmokeScript(t, "turn_listener", nil, func(agent *ir.Agent) {
		agent.Tracing = nil
		agent.Prefetch = nil
	}, harness+pipecatListenerDecidesScript))
}

const pipecatListenerDecidesScript = `
from pipecat.frames.frames import (  # noqa: E402
    ProposedUserStartedSpeakingFrame,
    ProposedUserStoppedSpeakingFrame,
)
from pipecat.services.deepgram.flux.stt import DeepgramFluxSTTService  # noqa: E402
from pipecat.turns.eager_end_of_turn_mixin import EagerEndOfTurnSTTServiceMixin  # noqa: E402


class FakeFlux(EagerEndOfTurnSTTServiceMixin, STTService):
    """Deepgram Flux without the socket: the same mixin, the same turn frames."""

    def __init__(self, probe, *, enable_eager_end_of_turn=False, settings=None, **kwargs):
        super().__init__(
            enable_eager_end_of_turn=enable_eager_end_of_turn,
            audio_passthrough=False,
            sample_rate=16000,
            settings=STTSettings(model="flux-general-en", language="en"),
        )
        probe.stt = self
        probe.flux = {"eager": enable_eager_end_of_turn, "settings": settings, **kwargs}

    def service_metadata_frame(self):
        frame = super().service_metadata_frame()
        frame.user_turn_strategies = self.recommended_user_turn_strategies(enable_interruptions=True)
        return frame

    async def run_stt(self, audio):
        if False:
            yield None

    # Flux's four turn events, in the order and shape stt_base.py sends them.
    async def start_of_turn(self):
        await self.broadcast_frame(ProposedUserStartedSpeakingFrame)

    async def predict_end_of_turn(self, text):
        await self._push_eager_end_of_turn(text, user_id="caller")

    async def turn_resumed(self):
        await self._cancel_eager_end_of_turn()

    async def end_of_turn(self, text):
        self._clear_eager_end_of_turn()
        await self.push_frame(
            TranscriptionFrame(text, "caller", "2026-09-11T00:00:00Z", finalized=True)
        )
        await self.broadcast_frame(ProposedUserStoppedSpeakingFrame)


class FluxFactory:
    """Stands where the emitted module names DeepgramFluxSTTService.

    The emitted build_stt() runs unchanged: it reads .Settings off this and calls
    it with the real keyword arguments, which is how the flag and the ceiling the
    compiler wrote are asserted below rather than assumed.
    """

    Settings = DeepgramFluxSTTService.Settings

    def __init__(self, probe):
        self.probe = probe

    def __call__(self, **kwargs):
        return FakeFlux(self.probe, **kwargs)


async def exercise_listener():
    os.environ["UNMUTE_DEV_METRICS"] = "1"
    probe = Probe()
    probe.flux = None
    original_build_stt = bot.build_stt
    original_params = bot.LLMUserAggregatorParams
    patch_providers(probe)
    # The shared patch replaces the transcriber and the aggregator's strategies;
    # both are what this test is about, so the emitted ones are put back.
    bot.build_stt = original_build_stt
    bot.LLMUserAggregatorParams = original_params
    bot.DeepgramFluxSTTService = FluxFactory(probe)
    capture = Capture()
    call_id = "pipecat-listener-decides"
    args = RunnerArguments(body={"unmute_dev_call_id": call_id}, session_id=call_id)
    try:
        with redirect_stdout(capture):
            task = asyncio.create_task(bot.run_bot(FakeTransport(probe), args))
            try:
                await asyncio.wait_for(probe.started.wait(), timeout=5)
                assert probe.flux is not None, "the emitted build_stt did not construct the Flux service"
                assert probe.flux["eager"] is True, probe.flux
                assert probe.flux["settings"].eot_timeout_ms == 1200, probe.flux["settings"]
                await probe.main.rtvi.set_client_ready()
                await until(lambda: probe.syntheses, "greeting reached TTS")
                probe.syntheses[0][1].set()
                await until(lambda: probe.audio, "greeting played")

                # 1. A prediction the caller talks past.
                await probe.stt.start_of_turn()
                await probe.stt.predict_end_of_turn("Book me a haircut")
                await until(lambda: len(probe.requests) == 1, "the prediction started a speculative model request")
                withdrawn = probe.requests[0]
                withdrawn.first.set()
                withdrawn.rest.set()
                await asyncio.wait_for(withdrawn.finished.wait(), timeout=5)
                await asyncio.sleep(0.05)
                assert len(probe.syntheses) == 1, "a speculative reply reached TTS before the turn was confirmed"
                await probe.stt.turn_resumed()
                await asyncio.sleep(0.05)
                assert len(probe.syntheses) == 1, "a withdrawn prediction's reply reached TTS"

                # 2. A prediction the committed transcript confirms.
                await probe.stt.predict_end_of_turn("Book me a haircut for tomorrow")
                await until(lambda: len(probe.requests) == 2, "the second prediction started a speculative request")
                held = probe.requests[1]
                held.first.set()
                held.rest.set()
                await asyncio.wait_for(held.finished.wait(), timeout=5)
                await asyncio.sleep(0.05)
                assert len(probe.syntheses) == 1, "the held reply reached TTS before the turn was confirmed"
                # Flux commits with punctuation the prediction lacked; NormalizedMatch keeps the reply.
                await probe.stt.end_of_turn("Book me a haircut for tomorrow.")
                await until(lambda: len(probe.syntheses) == 2, "the confirmed turn did not release the held reply", timeout=5)
                assert probe.syntheses[1][0].startswith("Hello"), probe.syntheses[1]
                probe.syntheses[1][1].set()
                await asyncio.sleep(0.1)
                assert len(probe.requests) == 2, "the confirmed turn ran a new inference instead of releasing the held one"
                assert len(probe.syntheses) == 2, "more than one reply was spoken for one turn"

                users = [r["text"]["text"] for r in capture.text("user")]
                assert any(text.startswith("Book me a haircut for tomorrow") for text in users), users
                assert "Book me a haircut" not in users, "the withdrawn prediction was recorded as caller text"
            finally:
                if not task.done():
                    for _text, release in probe.syntheses:
                        release.set()
                    for request in probe.requests:
                        request.first.set()
                        request.rest.set()
                    await probe.runner.cancel(reason="listener smoke cleanup")
                    await asyncio.wait_for(task, timeout=5)
    finally:
        for line in capture.getvalue().splitlines():
            if line.startswith(SENTINEL):
                print(line + "\n", end="")


asyncio.run(exercise_listener())
print("Pipecat listener decides: the prediction is answered early, dropped when withdrawn, spoken once when confirmed")
`
