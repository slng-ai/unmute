//go:build smoke

package generate

import (
	"strings"
	"testing"
)

// The request a task group removes, proved against the real framework.
//
// A group's whole claim is that the owner does not spend a model request
// deciding to enter the second step. That claim went untested for a month and
// was false on Pipecat the entire time: the first node of a flow carried its
// opening line as a Flows `tts_say` pre-action, which holds the node until the
// ActionFinishedFrame behind it reaches the worker sink, and a frame this
// worker queues does not move until the tool call building the flow returns. So
// the group said nothing, asked the model nothing, and held the caller until
// they hung up (live Pipecat call 2026-09-16, trace 5a330c65). Every check on
// the shape was a text or golden assertion on emitted Python, and emitted
// Python that deadlocks looks exactly like emitted Python that works.
//
// This drives the shipped salon through a real FlowManager with a stand-in for
// the model, and reads the system instruction of each request. Three runs:
//
//  1. a caller nobody has verified: concierge, verification, booking, and no
//     second concierge request between the last two;
//  2. a caller already verified: the group skips verification and opens on
//     booking, and verification is not re-run;
//  3. a step that cannot serve the request: the group stops and hands the owner
//     an unserved status rather than reading the diary for a question nobody
//     asked;
//  4. a lookup that refuses the number: not a success, so the step stays open
//     and booking is never entered.
//
// Before the fix run 1 times out at `Setting node: verify_customer`.
func TestSmokePipecatGroupChainsWithoutAnOwnerRequest(t *testing.T) {
	out := runPipecatSmokeScript(t, "salon-concierge", nil, nil, pipecatGroupChainSmokeScript)
	if !strings.Contains(string(out), "group chain smoke ok") {
		t.Fatalf("the group chain smoke did not reach its own last line:\n%s", out)
	}
}

const pipecatGroupChainSmokeScript = `"""A task group runs its second step without asking the owner which one."""
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
from tools import find_slots as salon  # noqa: E402 - the shared in-process store


class Passthrough(FrameProcessor):
    """Stands in for TTS. Spoken frames still travel the pipeline."""

    spoken = []

    async def process_frame(self, frame, direction):
        await super().process_frame(frame, direction)
        if type(frame).__name__ == "TTSSpeakFrame":
            Passthrough.spoken.append(frame.text)
        await self.push_frame(frame, direction)


# Run 1 verifies and books this caller; run 2 is the same caller moving that
# booking, which is the shape a second booking on one call actually has. Run 3
# never reaches the diary, so it uses a number nobody has heard of.
CALLER = "+15005550006"
STRANGER = "+15005550008"
TOMORROW = (salon._booking_today() + salon.timedelta(days=1)).isoformat()


def whose_prompt(text):
    """Which prompt a request asked under, by its first heading."""
    if not text:
        return "<none>"
    return text.strip().splitlines()[0].lstrip("# ").strip()


def a_free_slot(phone):
    """A slot the diary will actually accept, read from the store itself."""
    free = salon.find_slots(phone, date=TOMORROW, service="haircut")
    assert free["slots"], free
    return free["slots"][0]["slot_id"]


def advertised(ctx):
    tools = getattr(ctx, "tools", None)
    return sorted(getattr(tool, "name", str(tool)) for tool in getattr(tools, "standard_tools", []) or [])


def caller_lines(ctx):
    return [m.get("content") for m in ctx.get_messages() if isinstance(m, dict) and m.get("role") == "user"]


async def run(scripted, verified, phone):
    """Drive one booking turn and return what each model request asked under."""
    Passthrough.spoken = []
    asked = []
    seen = []
    finished = asyncio.Event()
    bot.build_concierge_tts = Passthrough
    context = LLMContext(messages=[{"role": "user", "content": "Book a haircut tomorrow afternoon."}])
    state = bot.build_state()
    state.customer_phone = phone
    state.booking_date = TOMORROW
    state.salon_local_time = "10:00"
    if verified:
        # What the verification step leaves behind: the caller agreed to this
        # number, so the group has nothing to ask them.
        state._unconfirmed = set(state._unconfirmed) - {"customer_phone"}
    owner = bot.ConciergeAgent(state=state, context=context)

    async def complete(ctx):
        asked.append(whose_prompt(owner.llm._settings.system_instruction))
        seen.append({"tools": advertised(ctx), "callers": caller_lines(ctx)})
        if len(asked) > len(scripted):
            finished.set()
            return
        name, args = scripted[len(asked) - 1]
        await owner.llm.run_function_calls([FunctionCallFromLLM(
            function_name=name, tool_call_id=f"probe-{len(asked)}",
            arguments=args, context=ctx,
        )])

    # Only the model's decisions and the audio are replaced. The bus, tool
    # dispatch, FlowManager, the finish validation and the owner's restoration
    # all run for real.
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
        # A deadlocked flow never makes its second request, so this is the
        # assertion and not just a guard: the whole defect is that nothing
        # happens at all.
        await asyncio.wait_for(finished.wait(), timeout=20)
    finally:
        await runner.cancel(reason="probe complete")
        await running
    return asked, state, owner, seen


async def main():
    # 1. Nobody verified yet.
    asked, state, owner, seen = await run([
        ("book", {}),
        ("find_or_create_customer", {"phone": CALLER}),
        ("find_slots", {"date": TOMORROW, "service": "haircut"}),
        ("save_booking", {"action": "book", "slot_id": a_free_slot(CALLER), "confirmed": True}),
    ], verified=False, phone=CALLER)
    assert asked[:4] == [
        "Sage and Stone concierge",
        "Verify the customer",
        "Handle one booking change",
        "Handle one booking change",
    ], asked
    # The one this feature removes. Between verification ending and booking
    # opening the concierge is never asked anything: a second entry of its
    # prompt anywhere in the first three requests is that request coming back.
    assert asked[1:3].count("Sage and Stone concierge") == 0, asked
    assert state.customer_phone == CALLER, state.customer_phone
    assert "customer_phone" not in state._unconfirmed, state._unconfirmed
    # Saved once, from the tool's own result, with no model request in between.
    assert state.appointment is not None, "the booking step saved nothing"
    assert state.appointment["action"] == "book", state.appointment
    # Each step spoke its own line as it was entered, which is what the
    # pre-action could not do.
    assert len(Passthrough.spoken) == 2, Passthrough.spoken
    assert owner._book_plan == ["verify_customer", "manage_booking"], owner._book_plan
    # The step the group opened by itself got its own tools and its own way out,
    # and none of the owner's.
    assert seen[2]["tools"] == [
        "find_slots", "finish_book_manage_booking", "save_booking", "to_complaints",
    ], seen[2]["tools"]
    # And the caller's own words, because the group shares context and the step
    # keeps the default spoken-message history.
    assert "Book a haircut tomorrow afternoon." in seen[2]["callers"], seen[2]["callers"]

    # 2. The same caller, already verified, moving what they just booked. This
    # is the turn the skip exists for: on a booking after the first the caller
    # hears the diary line and is never asked for their number again.
    booked = state.appointment["booking_id"]
    asked, state, owner, seen = await run([
        ("book", {}),
        ("find_slots", {"date": TOMORROW, "service": "haircut"}),
        ("save_booking", {
            "action": "move", "booking_id": booked,
            "slot_id": a_free_slot(CALLER), "confirmed": True,
        }),
    ], verified=True, phone=CALLER)
    assert asked[:3] == [
        "Sage and Stone concierge",
        "Handle one booking change",
        "Handle one booking change",
    ], asked
    assert owner._book_plan == ["manage_booking"], owner._book_plan
    assert state.appointment is not None, ("the verified caller's move did not save", owner._book_results, asked)
    assert state.appointment["action"] == "move", state.appointment
    # One line, because a step that does not run says nothing.
    assert len(Passthrough.spoken) == 1, Passthrough.spoken

    # 3. Verification cannot serve the request: the diary is never read.
    asked, state, owner, seen = await run([
        ("book", {}),
        ("finish_book_verify_customer", {"unserved_request": "they want to speak to Amara"}),
    ], verified=False, phone=STRANGER)
    _ = seen
    assert asked[:2] == [
        "Sage and Stone concierge",
        "Verify the customer",
    ], asked
    # Back with the owner, on its own prompt, and booking never opened.
    assert asked[2] == "Sage and Stone concierge", asked
    assert "Handle one booking change" not in asked, asked
    assert state.appointment is None, state.appointment

    # 4. The lookup refuses the number: not a success, so the step does not end
    # and booking is never opened. The result goes back to the model, under the
    # verification prompt, for it to ask again.
    asked, state, owner, seen = await run([
        ("book", {}),
        ("find_or_create_customer", {"phone": "123"}),
    ], verified=False, phone=STRANGER)
    assert asked[:3] == [
        "Sage and Stone concierge",
        "Verify the customer",
        "Verify the customer",
    ], asked
    assert owner._book_active_step == "verify_customer", owner._book_active_step
    assert state.appointment is None, state.appointment

    print("group chain smoke ok: verification runs into booking, skips when confirmed, "
          "stops when unserved, and stays open when the lookup refuses")


asyncio.run(main())
`
