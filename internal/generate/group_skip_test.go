package generate

import (
	"strings"
	"testing"
)

// A group step whose confirmation already holds is not entered. Decided once,
// as the group starts, which is what the key says: a confirmation that lapses
// mid-flow is the next invocation's question.
func TestAGroupSkipsAConfirmedStep(t *testing.T) {
	livekit := terminalModule(t, "livekit", "agent.py")
	for _, want := range []string{
		// The shared group decides per step as it is built.
		`if _is_confirmed(ctx.userdata, "customer_phone"):`,
		`logger.info("skipped verify: customer_phone is already confirmed")`,
		// The isolated sequence decides once, as a plan it then walks.
		`if not (confirmed and _is_confirmed(ctx.userdata, confirmed))`,
		`("verify", "customer_phone"),`,
		`if "verify" not in _plan:`,
		"for _id in _plan:",
	} {
		if !strings.Contains(livekit, want) {
			t.Errorf("livekit agent.py missing %q", want)
		}
	}
	pipecat := terminalModule(t, "pipecat", "bot.py")
	for _, want := range []string{
		"self._do_book_plan = [",
		`("verify", "customer_phone"),`,
		"if not (confirmed and _is_confirmed(self.state, confirmed))",
		"def _do_book_next(self, name):",
	} {
		if !strings.Contains(pipecat, want) {
			t.Errorf("pipecat bot.py missing %q", want)
		}
	}
}

// A step that some group may skip withdraws what it confirms whenever it is
// entered, standalone entry included, so a skip is never decided on a
// confirmation the same step is about to replace.
func TestAWithdrawingTaskUnconfirmsOnEntry(t *testing.T) {
	livekit := terminalModule(t, "livekit", "agent.py")
	if !strings.Contains(livekit, `_withdraw_confirmation(self.session.userdata, "verify")`) {
		t.Error("livekit agent.py does not withdraw on entering the skippable step")
	}
	// Before the prompt renders, or the step reads a confirmation it is about
	// to replace.
	withdrawAt := strings.Index(livekit, `_withdraw_confirmation(self.session.userdata, "verify")`)
	promptAt := strings.Index(livekit, "await self.update_instructions(")
	if withdrawAt < 0 || (promptAt >= 0 && promptAt < withdrawAt) {
		t.Errorf("withdrawal must come before the prompt: withdraw=%d prompt=%d", withdrawAt, promptAt)
	}
	pipecat := terminalModule(t, "pipecat", "bot.py")
	if !strings.Contains(pipecat, `_withdraw_confirmation(self.state, "verify")`) {
		t.Error("pipecat bot.py does not withdraw on entering the skippable step")
	}
	nodeAt := strings.Index(pipecat, "def _do_book_node_verify(self)")
	withdrawAt = strings.Index(pipecat[nodeAt:], `_withdraw_confirmation(self.state, "verify")`)
	roleAt := strings.Index(pipecat[nodeAt:], "role_message=")
	if nodeAt < 0 || withdrawAt < 0 || roleAt < withdrawAt {
		t.Errorf("withdrawal must come before role_message: node=%d withdraw=%d role=%d", nodeAt, withdrawAt, roleAt)
	}
}

// Values that derive from a withdrawn one follow it, through the same
// dependency pass a save runs.
func TestWithdrawalClearsDerivedValuesToo(t *testing.T) {
	for name, module := range terminalModules(t) {
		body := blockAfter(t, module, "def _withdraw_confirmation(state, step):")
		for _, want := range []string{
			"_STATE_CONFIRM.items()",
			"for name, reads in _STATE_DEPENDENCIES.items():",
			"if any(source in unconfirmed for source in reads):",
			"state._unconfirmed = unconfirmed",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: _withdraw_confirmation missing %q", name, want)
			}
		}
	}
}

// A step that ends unserved stops the group: running the next one would answer
// a question nobody asked, and the owner is handed the unserved status.
func TestAGroupStopsOnUnserved(t *testing.T) {
	livekit := terminalModule(t, "livekit", "agent.py")
	for _, want := range []string{
		"class _GroupStop(Exception):",
		`if isinstance(event.result, dict) and event.result.get("unserved_request"):`,
		`raise _GroupStop(dict(flow["results"]))`,
		"except _GroupStop as stop:",
		"task_results = stop.results",
	} {
		if !strings.Contains(livekit, want) {
			t.Errorf("livekit agent.py missing %q", want)
		}
	}
	pipecat := terminalModule(t, "pipecat", "bot.py")
	for _, want := range []string{
		`if self._do_book_results["verify"].get("unserved_request"):`,
		`logger.info("group stopped: verify ended unserved")`,
		"_next = None",
	} {
		if !strings.Contains(pipecat, want) {
			t.Errorf("pipecat bot.py missing %q", want)
		}
	}
}

// A handoff called beside a tool that ends the step waits for that tool to
// settle, and moves the caller only once the step's own work is recorded.
func TestAHandoffWaitsForATerminalCallToSettle(t *testing.T) {
	livekit := terminalModule(t, "livekit", "agent.py")
	for _, want := range []string{
		// Waits on a mutation in flight from any response, or one called
		// beside it in this response and not started yet.
		`if self._terminal_pending or _sibling_call(ctx, {"book_it", "cancel_it"}):`,
		"await asyncio.wait_for(self._terminal_settled.wait(), timeout=30.0)",
		"if self._finish_call_id is None:",
		"The action did not complete, so the caller was not moved.",
	} {
		if !strings.Contains(livekit, want) {
			t.Errorf("livekit agent.py missing %q", want)
		}
	}
	// The wait comes before the transfer claims the step, or the terminal call
	// finds the step already closed.
	waitAt := strings.Index(livekit, "await asyncio.wait_for(self._terminal_settled.wait()")
	claimAt := strings.Index(livekit, "if not self._claim_terminal():")
	if waitAt < 0 || claimAt < waitAt {
		t.Errorf("the handoff must wait before it claims: wait=%d claim=%d", waitAt, claimAt)
	}
	pipecat := terminalModule(t, "pipecat", "bot.py")
	for _, want := range []string{
		`if self._do_book_pending or any(name in {"book_it", "cancel_it"} for name in self._response_calls):`,
		"await asyncio.wait_for(self._do_book_settled.wait(), timeout=30.0)",
		`if "book" not in self._do_book_results:`,
		`return {"status": "not transferred: the action did not complete"}, None`,
	} {
		if !strings.Contains(pipecat, want) {
			t.Errorf("pipecat bot.py missing %q", want)
		}
	}
	// Both handlers of one response run on this framework, so the terminal tool
	// records its result before handing the ending to the transfer.
	if !strings.Contains(pipecat, `self._do_book_results["book"] = _values`) {
		t.Error("pipecat bot.py does not record the terminal result before the handoff reads it")
	}
}
