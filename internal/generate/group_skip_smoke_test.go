//go:build smoke

package generate

import "testing"

// What a group actually does with the three exits, against the real framework:
// a step that succeeds hands over, a step that ends unserved stops the flow,
// and a handoff called beside a tool that ends the step waits for that tool.
//
// The three branches of that last one are the ones no unit test can settle:
// success commits and then hands off, a tool failure keeps the step open with
// no handoff, and a save failure keeps the step open with the repair
// instruction in front of the model.
func TestSmokeGroupExitsLiveKit(t *testing.T) {
	runLiveKitSmokeScript(t, "terminal_step", nil, nil, groupSmokeScript("agent", "Userdata()"))
}

func TestSmokeGroupExitsPipecat(t *testing.T) {
	runPipecatSmokeScript(t, "terminal_step", nil, nil, groupSmokeScript("bot", "build_state()"))
}

func groupSmokeScript(module, stateExpr string) string {
	return `"""Smoke check: what a group does with each of its exits."""
# ruff: noqa: E402 - the environment has to be seeded before the module imports
import asyncio
import json
import os
import time
from types import SimpleNamespace

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import ` + module + ` as generated

BOOKED = {
    "status": "booked",
    "summary": "Booking saved.",
    "booking": {"reference": "bkg_0001", "service": "haircut", "action": "create"},
}


def verified_state():
    fresh = generated.` + stateExpr + `
    generated._save_result("verify", fresh, {"customer_phone": "+15550101010", "customer_status": "existing"})
    return fresh


def check_skip_decision():
    """The plan a group starts with, over the same predicate both targets read."""
    fresh = generated.` + stateExpr + `
    steps = (("verify", "customer_phone"), ("book", ""))
    plan = [name for name, c in steps if not (c and generated._is_confirmed(fresh, c))]
    assert plan == ["verify", "book"], plan
    generated._save_result("verify", fresh, {"customer_phone": "+15550101010", "customer_status": "existing"})
    plan = [name for name, c in steps if not (c and generated._is_confirmed(fresh, c))]
    assert plan == ["book"], plan
    # And the last step of that plan is the one that owes the caller a reply.
    assert plan[-1] == "book"


def check_unserved_stops_the_group():
    """A step that ends unserved saves nothing and reads as unserved."""
    fresh = verified_state()
    values = generated._save_result("book", fresh, {"unserved_request": "they want a refund"})
    assert values["unserved_request"] == "they want a refund"
    assert fresh.booking is None
    assert generated._task_status(values) == {"status": "unserved"}
    assert generated._group_status({"verify": {}, "book": values}) == {"status": "unserved"}
    assert generated._group_status({"verify": {}, "book": {"booking": {}}}) == {"status": "completed"}


async def check_livekit_handoff_branches():
    fresh = verified_state()
    ctx = SimpleNamespace(
        userdata=fresh,
        session=SimpleNamespace(),
        function_call=SimpleNamespace(call_id="tool-1", name="book_it"),
        speech_handle=SimpleNamespace(chat_items=[]),
    )
    # One response, two calls: the model asked to book and to hand the caller
    # over in the same breath. Each handler sees the other's call.
    booking_call = SimpleNamespace(type="function_call", call_id="tool-1", name="book_it")
    transfer_call = SimpleNamespace(type="function_call", call_id="tool-2", name="to_care")
    batch = SimpleNamespace(chat_items=[booking_call, transfer_call])
    ctx.speech_handle = batch
    transfer_ctx = SimpleNamespace(
        userdata=fresh, session=SimpleNamespace(), function_call=transfer_call, speech_handle=batch
    )
    step = generated.Book()
    # The terminal tool commits and leaves the ending to the transfer.
    assert await step._end_on_book_it(ctx, BOOKED) is None
    assert not step.done()
    assert step._finish_call_id == "tool-1"
    assert fresh.booking["reference"] == "bkg_0001"
    # The transfer sees the settled success and moves the caller.
    moved = await step.to_care(transfer_ctx)
    assert moved is None
    assert step.done()
    # A tool failure keeps the step open and the transfer refuses to move. The
    # failure settles the wait itself: the handoff answers promptly rather than
    # sitting out the timeout.
    failing = verified_state()
    ctx.userdata = failing
    transfer_ctx.userdata = failing
    held = generated.Book()
    held._begin_terminal()
    started = time.monotonic()
    not_confirmed = {"status": "not_confirmed", "summary": "The caller has not agreed yet."}
    failed, answer = await asyncio.gather(
        held._end_on_book_it(ctx, not_confirmed), held.to_care(transfer_ctx)
    )
    assert failed == not_confirmed
    assert isinstance(answer, str) and "was not moved" in answer, answer
    assert time.monotonic() - started < 5, "the handoff waited for the timeout, not the settlement"
    assert not held.done()
    # A handoff that arrives in a later response while the mutation is still
    # running waits for it, and then owns the ending: the booking is committed
    # and the caller is moved, once.
    later = verified_state()
    waiting = generated.Book()
    slow_ctx = SimpleNamespace(
        userdata=later, session=SimpleNamespace(), function_call=booking_call,
        speech_handle=SimpleNamespace(chat_items=[booking_call]),
    )
    alone_ctx = SimpleNamespace(
        userdata=later, session=SimpleNamespace(), function_call=transfer_call,
        speech_handle=SimpleNamespace(chat_items=[transfer_call]),
    )
    original_tool = generated.tools.book_it.book_it

    async def slow_book_it(*args, **kwargs):
        await asyncio.sleep(0.1)
        return original_tool(*args, **kwargs)

    generated.tools.book_it.book_it = slow_book_it
    try:
        ended, moved = await asyncio.gather(
            waiting.book_it(slow_ctx, confirmed=True, service="haircut"),
            waiting.to_care(alone_ctx),
        )
    finally:
        generated.tools.book_it.book_it = original_tool
    assert ended is None and moved is None, (ended, moved)
    assert later.booking["reference"] == "bkg_0001"
    assert waiting.done()
    # A save failure is the same answer, and the result stays in front of the
    # model with the repair instruction.
    refusing = generated.Book()
    broken = dict(BOOKED, booking={"reference": "bkg_0001", "service": "haircut", "action": "invented"})
    out = await refusing._end_on_book_it(ctx, broken)
    assert "Not recorded" in out["refused"]
    answer = await refusing.to_care(transfer_ctx)
    assert isinstance(answer, str) and "was not moved" in answer, answer
    assert not refusing.done()


check_skip_decision()
check_unserved_stops_the_group()
if generated.__name__ == "agent":
    asyncio.run(check_livekit_handoff_branches())
print("group smoke passed")
`
}
