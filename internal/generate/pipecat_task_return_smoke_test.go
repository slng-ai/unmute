//go:build smoke

package generate

import (
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// The think binding is pinned to OpenAI for this one smoke, and the reason is
// worth keeping next to it.
//
// What this test is about is the context an owner gets back when a task
// returns: which messages are there, which tools it advertises, and that no
// task-private value leaked. It reads those off the request body, and the body
// it knows how to read is the OpenAI chat-completions shape: `messages` with a
// `role`, `tools` with a `function.name`, a `tool_call_id` per call.
//
// The salon example's own think vendor is not part of that subject, and it
// moved: it was OpenAI when this was written and is native Gemini on Vertex
// now. A Google service builds its request differently and has no
// `build_chat_completion_params`, so the stand-in below raised on it and the
// whole suite failed for a reason that had nothing to do with task return.
//
// Pinning the vendor keeps the assertions meaningful and stops this test from
// breaking every time the example changes model. Whether the emitted bot works
// on Vertex is a different question, and the other salon smokes compile and run
// the example as it actually ships.
func TestSmokePipecatTaskReturnSettlesOriginalCall(t *testing.T) {
	openAIThink := func(tgt *ir.Target) {
		binding, ok := tgt.Models.Reason["reasoning"]
		if !ok {
			t.Fatal("salon-concierge has no think entry named reasoning to pin")
		}
		binding.Provider, binding.Model = "openai", "gpt-5.6-luna"
		binding.Params = map[string]any{"reasoning_effort": "none"}
		tgt.Models.Reason["reasoning"] = binding
	}
	runPipecatSmokeScript(t, "salon-concierge", openAIThink, nil, pipecatTaskReturnSmokeScript)
}

const pipecatTaskReturnSmokeScript = `"""A completed verification cannot remain running in the owner's context."""
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


class Passthrough(FrameProcessor):
    async def process_frame(self, frame, direction):
        await super().process_frame(frame, direction)
        await self.push_frame(frame, direction)


async def main():
    requests = []
    returned = asyncio.Event()
    bot.build_concierge_tts = Passthrough
    context = LLMContext(messages=[{"role": "user", "content": "Book a haircut tomorrow afternoon."}])
    state = bot.build_state()
    state.customer_phone = "+15005550006"
    owner = bot.ConciergeAgent(state=state, context=context)
    # Two calls, not three. The step ends on its lookup, so there is no finish
    # call to script: the request that used to make it is the one this feature
    # removes, and a model that made it anyway would be calling a function the
    # node no longer advertises.
    scripted = [
        ("verify_customer", {}),
        ("find_or_create_customer", {"phone": "+15005550006"}),
    ]

    async def complete(ctx):
        llm = owner.llm
        params = llm.build_chat_completion_params(llm.get_llm_adapter().get_llm_invocation_params(
            ctx, system_instruction=llm._settings.system_instruction,
            convert_developer_to_user=not llm.supports_developer_role,
        ))
        requests.append({key: params[key] for key in ("messages", "tools")})
        if len(requests) <= len(scripted):
            name, args = scripted[len(requests) - 1]
            await llm.run_function_calls([FunctionCallFromLLM(
                function_name=name, tool_call_id=f"probe-{len(requests)}",
                arguments=args, context=ctx,
            )])
        else:
            returned.set()

    # Only model decisions and audio are replaced. The bus, tool dispatch,
    # FlowManager, finish validation and owner restoration all run for real.
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
        await asyncio.wait_for(returned.wait(), timeout=10)
        # The task's save landed and its confirmation cleared: that is what the
        # owner resuming has to see. There is no verification marker to read out
        # of the owner's prompt any more, because the number is confirm-gated and
        # the concierge prompt deliberately holds none. The leak check below is
        # the other half of that same rule.
        assert state.customer_phone == "+15005550006", state.customer_phone
        assert "customer_phone" not in state._unconfirmed, state._unconfirmed
        request = requests[-1]
        # The booking step runs inside the book group now, so what the owner
        # advertises is the flow rather than the step.
        assert "book" in [tool["function"]["name"] for tool in request["tools"]]
        result = next(message for message in request["messages"]
                      if message.get("role") == "tool" and message.get("tool_call_id") == "probe-1")
        assert json.loads(result["content"]) == {"status": "completed"}, result
        assert "+15005550006" not in json.dumps(request["messages"]), "task-private values leaked"
    finally:
        await runner.cancel(reason="probe complete")
        await running

    # Re-entry settles only the latest invocation, for either terminal status.
    for status in ("completed", "unserved"):
        messages = [
            {"role": "assistant", "tool_calls": [{"id": "old", "function": {"name": "verify_customer"}}]},
            {"role": "tool", "tool_call_id": "old", "content": "earlier result"},
            {"role": "assistant", "tool_calls": [{"id": "new", "function": {"name": "verify_customer"}}]},
            {"role": "tool", "tool_call_id": "new", "content": "running"},
        ]
        bot._settle_task_call(messages, "verify_customer", {"status": status})
        assert messages[1]["content"] == "earlier result"
        assert json.loads(messages[3]["content"]) == {"status": status}
    print("task return smoke ok: completed call, saved state, owner tools, no private values")


asyncio.run(main())
`
