//go:build smoke

package generate

import "testing"

// What a listening step actually does, against the real framework.
//
// This used to build the NodeConfig and assert on the dict, which is the check
// that let the Pipecat deadlock ship: a node that is never set looks perfect as
// a dict. The line was a Flows `tts_say` pre-action, and a pre-action holds the
// node until the ActionFinishedFrame behind it reaches the worker sink, while a
// frame this worker queues does not move until the tool call building the flow
// returns. So the step said nothing, set nothing and waited forever.
//
// So the node is set now, through a FlowManager, on a running worker. The line
// reaching the TTS and the step's prompt reaching the LLM are the two halves of
// `_set_node` having finished at all.
//
// Nothing here reads the NodeConfig: that is TestListeningOpeningMakesNoRequest
// and TestListeningOpeningSurvivesAReset, which hold the seeded turn, the reset
// and the absence of a pre-action on the emitted text. This one only runs it.
func TestSmokeListeningOpening(t *testing.T) {
	runPipecatSmokeScript(t, "terminal_step", nil, nil, `"""Smoke check: a listening step speaks, sets its node, and asks nothing."""
import asyncio
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402
from pipecat.bus import BusBridgeProcessor  # noqa: E402
from pipecat.frames.frames import FunctionCallFromLLM  # noqa: E402
from pipecat.pipeline.pipeline import Pipeline  # noqa: E402
from pipecat.pipeline.worker import PipelineWorker  # noqa: E402
from pipecat.processors.aggregators.llm_context import LLMContext  # noqa: E402
from pipecat.processors.aggregators.llm_response_universal import LLMContextAggregatorPair  # noqa: E402
from pipecat.processors.frame_processor import FrameProcessor  # noqa: E402
from pipecat.workers.llm import LLMWorkerActivationArgs  # noqa: E402
from pipecat.workers.runner import WorkerRunner  # noqa: E402

LINE = "What would you like me to pass on?"


class Passthrough(FrameProcessor):
    spoken = []

    async def process_frame(self, frame, direction):
        await super().process_frame(frame, direction)
        if type(frame).__name__ == "TTSSpeakFrame":
            Passthrough.spoken.append((frame.text, frame.append_to_context))
        await self.push_frame(frame, direction)


async def main():
    requests = []
    bot.build_desk_tts = Passthrough
    context = LLMContext(messages=[{"role": "user", "content": "Leave a note for my stylist."}])
    owner = bot.DeskAgent(state=bot.build_state(), context=context)

    async def complete(ctx):
        requests.append(owner.llm._settings.system_instruction or "")
        if len(requests) == 1:
            await owner.llm.run_function_calls([FunctionCallFromLLM(
                function_name="take_note", tool_call_id="probe-1",
                arguments={}, context=ctx,
            )])

    owner.llm._process_context = complete
    runner = WorkerRunner()
    user, assistant = LLMContextAggregatorPair(context)
    main_worker = PipelineWorker(Pipeline([
        user, BusBridgeProcessor(bus=runner.bus, worker_name="main"), assistant,
    ]), name="main")
    ready = asyncio.Event()

    @runner.event_handler("on_ready")
    async def on_ready(runner):
        ready.set()

    @main_worker.event_handler("on_pipeline_started")
    async def on_started(worker, frame):
        await ready.wait()
        await runner.add_workers(owner)
        await main_worker.activate_worker(owner.name, args=LLMWorkerActivationArgs(messages=[], run_llm=True))

    await runner.add_workers(main_worker)
    running = asyncio.create_task(runner.run())
    try:
        # The step's line has to be heard. A pre-action never got this far:
        # nothing was spoken and the node was never set.
        for _ in range(200):
            if Passthrough.spoken:
                break
            await asyncio.sleep(0.05)
        assert Passthrough.spoken == [(LINE, False)], Passthrough.spoken
        # And the node was set: the step's prompt is the LLM's instruction now.
        for _ in range(100):
            if (owner.llm._settings.system_instruction or "").startswith("# Take a note"):
                break
            await asyncio.sleep(0.05)
        assert (owner.llm._settings.system_instruction or "").startswith("# Take a note"), \
            owner.llm._settings.system_instruction
    finally:
        await runner.cancel(reason="probe complete")
        await running

    # One request: the owner's, which chose the step. The step itself opens by
    # listening, and that is the request this key removes.
    assert len(requests) == 1, requests
    print("listening opening smoke ok: spoken once, node set, no request of its own")


asyncio.run(main())
`)
}
