//go:build smoke

package generate

import "testing"

// The unit tests hold what the emitted terminal code says. These hold what it
// does, against the real framework in each emitted project: a success saves and
// ends the step, a non-success goes back to the model, a refused save keeps the
// result and says how to repair it, and a second mutation after a success does
// not run at all.
//
// One script, run against both emitted modules, so the two targets cannot
// diverge on the thing the whole feature turns on. Nothing reaches a provider.
func TestSmokeTerminalOutcomesLiveKit(t *testing.T) {
	runLiveKitSmokeScript(t, "terminal_step", nil, nil, terminalSmokeScript("agent", "Userdata()"))
}

func TestSmokeTerminalOutcomesPipecat(t *testing.T) {
	runPipecatSmokeScript(t, "terminal_step", nil, nil, terminalSmokeScript("bot", "build_state()"))
}

func terminalSmokeScript(module, stateExpr string) string {
	return `"""Smoke check: what a step that ends on its own tool actually does."""
# ruff: noqa: E402 - the environment has to be seeded before the module imports
import asyncio
import json
import os
from types import SimpleNamespace

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import ` + module + ` as generated

BOOKED = {
    "status": "booked",
    "summary": "Booking saved.",
    "booking": {"reference": "bkg_0001", "service": "haircut", "action": "create"},
}
NOT_CONFIRMED = {"status": "not_confirmed", "summary": "The caller has not agreed yet."}


def check_success_predicate():
    """Every field required, several values on one field alternatives."""
    assert generated._terminal_success(BOOKED, {"status": ("booked",)})
    assert not generated._terminal_success(NOT_CONFIRMED, {"status": ("booked",)})
    assert generated._terminal_success({"status": "created"}, {"status": ("existing", "created")})
    assert not generated._terminal_success({"status": "created"}, {"status": ("existing",)})
    # Two fields, both required.
    assert not generated._terminal_success(BOOKED, {"status": ("booked",), "refunded": ("true",)})
    assert generated._terminal_success({"status": "booked", "refunded": True}, {"refunded": ("true",)})
    # Anything that is not a dict is not a success.
    assert not generated._terminal_success("booked", {"status": ("booked",)})


def check_confirmation_helpers():
    fresh = generated.` + stateExpr + `
    assert not generated._is_confirmed(fresh, "customer_phone")
    generated._save_result("verify", fresh, {"customer_phone": "+15550101010", "customer_status": "existing"})
    assert generated._is_confirmed(fresh, "customer_phone")
    # Entering the step that confirms it withdraws it again, which is what stops
    # a group skipping a step that is about to replace the value.
    generated._withdraw_confirmation(fresh, "verify")
    assert not generated._is_confirmed(fresh, "customer_phone")
    assert fresh.customer_phone == "+15550101010"
    # A step that confirms nothing withdraws nothing.
    generated._save_result("verify", fresh, {"customer_phone": "+15550101010", "customer_status": "existing"})
    generated._withdraw_confirmation(fresh, "book")
    assert generated._is_confirmed(fresh, "customer_phone")


def check_repair_merge():
    """The tool's own value wins where it validates; the model's stands where it does not."""
    retained = {"booking": {"reference": "bkg_0001", "service": "haircut", "action": "create"}}
    merged = generated._merge_retained("book", {"booking": None}, retained)
    assert merged["booking"] == retained["booking"]
    # A retained value that does not validate is left to the model.
    broken = {"booking": {"reference": "bkg_0001", "service": "haircut", "action": "invented"}}
    kept = generated._merge_retained("book", {"booking": None}, broken)
    assert kept["booking"] is None


def verified_state():
    """A state the booking step can run in: the number is saved and confirmed.

    The booking tool injects it and refuses the call while it is unconfirmed,
    which is the guard this feature deliberately leaves alone.
    """
    fresh = generated.` + stateExpr + `
    generated._save_result("verify", fresh, {"customer_phone": "+15550101010", "customer_status": "existing"})
    return fresh


async def check_livekit():
    fresh = verified_state()
    ctx = SimpleNamespace(
        userdata=fresh,
        session=SimpleNamespace(),
        function_call=SimpleNamespace(call_id="tool-1", name="book_it"),
        speech_handle=SimpleNamespace(chat_items=[]),
    )
    # A success saves and ends the step, and returns nothing: the framework asks
    # this step for no reply, and the owner speaks when the delegate returns.
    step = generated.Book()
    step._begin_terminal()
    assert await step._end_on_book_it(ctx, BOOKED) is None
    assert step.done()
    assert fresh.booking["reference"] == "bkg_0001"
    assert step._terminal is not None
    assert step._terminal_settled.is_set() and not step._terminal_pending
    # Delivered again after completion: nothing is saved a second time.
    saves = []
    original_save = generated._save_result
    generated._save_result = lambda *args: saves.append(args) or original_save(*args)
    try:
        assert await step._end_on_book_it(ctx, BOOKED) is None
    finally:
        generated._save_result = original_save
    assert saves == [], saves
    # A non-success is an ordinary result, the step stays open, and the call
    # still settles: a handoff waiting beside it stops waiting.
    open_step = generated.Book()
    open_step._begin_terminal()
    assert await open_step._end_on_book_it(ctx, NOT_CONFIRMED) == NOT_CONFIRMED
    assert not open_step.done()
    assert open_step._terminal_settled.is_set() and not open_step._terminal_pending
    # A second mutation after a success does not run: the guard is in the tool
    # method, before the handler, so nothing is booked twice.
    closed = generated.Book()
    await closed._end_on_book_it(ctx, BOOKED)
    refused = await closed.book_it(ctx, confirmed=True, service="haircut")
    assert "already succeeded" in refused["refused"]
    # A refused save keeps the result and says how to record it.
    refusing = generated.Book()
    broken = dict(BOOKED, booking={"reference": "bkg_0001", "service": "haircut", "action": "invented"})
    out = await refusing._end_on_book_it(ctx, broken)
    assert out["status"] == "booked" and "Not recorded" in out["refused"]
    assert not refusing.done()
    assert refusing._terminal is not None
    assert refusing._terminal_settled.is_set()


async def check_pipecat():
    fresh = verified_state()
    worker = SimpleNamespace(
        state=fresh,
        context=generated.LLMContext(),
        _response_calls=(),
        _do_book_visit=object(),
        _do_book_active_step="book",
        _do_book_results={},
        _do_book_terminal=None,
        _do_book_pending=False,
        _do_book_settled=asyncio.Event(),
        _do_book_carried_turn=None,
        _do_book_snapshot_turns=0,
        _do_book_plan=["verify", "book"],
        _do_book_snapshot=([], []),
        _dev=None,
    )
    # The flow's own methods, bound to the stand-in worker: the wrapper saves
    # once and hands the saved values to the shared advance.
    async def ignore(*args, **kwargs):
        return None

    worker.queue_frame = ignore
    worker.flush_pipeline = ignore
    worker._do_book_settle = lambda: generated.DeskAgent._do_book_settle(worker)
    worker._do_book_next = lambda name: generated.DeskAgent._do_book_next(worker, name)
    worker._do_book_node = lambda name: generated.DeskAgent._do_book_node(worker, name)
    worker._do_book_advance_book = lambda values: generated.DeskAgent._do_book_advance_book(worker, values)
    worker._do_book_finish_book = lambda args, flow: generated.DeskAgent._do_book_finish_book(worker, args, flow)
    worker._do_book_complete_book = lambda: generated.DeskAgent._do_book_complete_book(worker)
    worker.context.set_messages([{"role": "user", "content": "book it"}])
    wrapper = generated.DeskAgent._do_book_terminal_book_book_it
    # A non-success is an ordinary result, the step stays open, and the call
    # still settles.
    result, node = await wrapper(worker, {"confirmed": False, "service": "haircut"}, None)
    assert result["status"] == "not_confirmed" and node is None
    assert worker._do_book_terminal is None
    assert worker._do_book_settled.is_set() and not worker._do_book_pending
    # A success saves once, without the model in between, and carries the
    # caller turn the owner never saw.
    saves = []
    original_save = generated._save_result
    generated._save_result = lambda *args: saves.append(args) or original_save(*args)
    try:
        result, node = await wrapper(worker, {"confirmed": True, "service": "haircut"}, None)
    finally:
        generated._save_result = original_save
    assert len(saves) == 1, saves
    assert fresh.booking["reference"] == "bkg_0001"
    assert worker._do_book_terminal is not None
    assert worker._do_book_settled.is_set() and not worker._do_book_pending
    assert worker._do_book_carried_turn == {"role": "user", "content": "book it"}
    assert worker._do_book_results["book"]["booking"]["reference"] == "bkg_0001"
    assert worker._do_book_active_step is None, "the flow did not return to the owner"
    # A second mutation after that one does not run.
    worker._do_book_active_step = "book"
    refused, _ = await wrapper(worker, {"confirmed": True, "service": "haircut"}, None)
    assert "already succeeded" in refused["refused"]
    # A repair after a refused save puts the tool's own values back before it
    # saves: the model cannot manufacture a reference.
    merges = []
    original_merge = generated._merge_retained
    generated._merge_retained = lambda *args: merges.append(args) or original_merge(*args)
    try:
        await worker._do_book_finish_book({"booking": None}, None)
    finally:
        generated._merge_retained = original_merge
    assert len(merges) == 1 and merges[0][2] is worker._do_book_terminal[1], merges
    # Entering a step leaves the previous step's ending behind: the salon's
    # Pipecat journeys prove that through the real delegate entry, where the
    # first live failure of this feature lived.
    # A handoff from a later response waits for the running mutation, and moves
    # the caller only once it is recorded.
    later = verified_state()
    worker.state = later
    worker._do_book_results = {}
    # What the node builder resets on entering a step, done by hand here.
    worker._do_book_terminal = None
    worker._do_book_pending = False
    worker._do_book_carried_turn = None
    worker.context.set_messages([{"role": "user", "content": "book it, and get me a manager"}])
    activations = []

    async def activate(name, *, args, deactivate_self):
        activations.append((name, deactivate_self))

    worker.activate_worker = activate
    worker._do_book_transfer_book_to_care = lambda args, flow: generated.DeskAgent._do_book_transfer_book_to_care(worker, args, flow)
    original_tool = generated._flow_tool_book_it

    async def slow_tool(*args, **kwargs):
        await asyncio.sleep(0.1)
        return await original_tool(*args, **kwargs)

    generated._flow_tool_book_it = slow_tool
    worker._do_book_active_step = "book"
    worker._do_book_settled = asyncio.Event()
    worker._response_calls = ("to_care",)
    try:
        ended, moved = await asyncio.gather(
            wrapper(worker, {"confirmed": True, "service": "haircut"}, None),
            worker._do_book_transfer_book_to_care({}, None),
        )
    finally:
        generated._flow_tool_book_it = original_tool
    assert later.booking["reference"] == "bkg_0001"
    assert ended == ({"status": "completed"}, generated.NO_RESPONSE), ended
    assert moved == ({"transferred": True}, generated.NO_RESPONSE), moved
    assert activations == [("care", True)], activations


check_success_predicate()
check_confirmation_helpers()
check_repair_merge()
if generated.__name__ == "agent":
    asyncio.run(check_livekit())
else:
    asyncio.run(check_pipecat())
print("terminal smoke passed")
`
}
