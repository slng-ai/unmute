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
    # A success saves, ends the step, and asks for the one reply it owes.
    step = generated.Book()
    assert await step._end_on_book_it(ctx, BOOKED) == {"status": "completed"}
    assert step.done()
    assert fresh.booking["reference"] == "bkg_0001"
    assert step._terminal is not None
    # A step with another one after it hands over silently.
    quiet = generated.Book(ends_flow=False)
    assert await quiet._end_on_book_it(ctx, BOOKED) is None
    assert quiet.done()
    # A non-success is an ordinary result and the step stays open.
    open_step = generated.Book()
    assert await open_step._end_on_book_it(ctx, NOT_CONFIRMED) == NOT_CONFIRMED
    assert not open_step.done()
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
        _do_book_active_step="book",
        _do_book_results={},
        _do_book_terminal=None,
        _do_book_settled=asyncio.Event(),
        _do_book_carried_turn=None,
        _do_book_plan=["verify", "book"],
        _do_book_snapshot=([], []),
    )
    # The flow's own methods, bound to the stand-in worker: the wrapper hands
    # the advance to the finish handler, which is the point of the shape.
    async def ignore(*args, **kwargs):
        return None

    worker.queue_frame = ignore
    worker.flush_pipeline = ignore
    worker._do_book_next = lambda name: generated.DeskAgent._do_book_next(worker, name)
    worker._do_book_node = lambda name: generated.DeskAgent._do_book_node(worker, name)
    worker._do_book_finish_book = lambda args, flow: generated.DeskAgent._do_book_finish_book(worker, args, flow)
    worker._do_book_complete_book = lambda: generated.DeskAgent._do_book_complete_book(worker)
    worker.context.set_messages([{"role": "user", "content": "book it"}])
    wrapper = generated.DeskAgent._do_book_terminal_book_book_it
    # A non-success is an ordinary result and the step stays open.
    result, node = await wrapper(worker, {"confirmed": False, "service": "haircut"}, None)
    assert result["status"] == "not_confirmed" and node is None
    assert worker._do_book_terminal is None
    # A success saves without the model in between.
    worker._do_book_active_step = "book"
    result, node = await wrapper(worker, {"confirmed": True, "service": "haircut"}, None)
    assert fresh.booking["reference"] == "bkg_0001"
    assert worker._do_book_terminal is not None
    assert worker._do_book_settled.is_set()
    # A second mutation after that one does not run.
    worker._do_book_active_step = "book"
    refused, _ = await wrapper(worker, {"confirmed": True, "service": "haircut"}, None)
    assert "already succeeded" in refused["refused"]


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
