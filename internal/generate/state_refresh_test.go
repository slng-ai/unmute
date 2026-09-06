package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// The conversation state block has to hold what a step just wrote, in the
// prompt of the agent that sent the step in.
//
// LiveKit renders an agent's prompt in `on_enter`, and an agent is entered once
// per call, so the block froze at whatever it held before the first step
// finished. Its steps looked right the whole time, which is what made this hard
// to see: a step is entered per visit and renders on the way in. On a live call
// (trace 798550937, 2026-09-04) `verify_customer` finished at 15:05:03 and the
// concierge's own prompt still read "Customer: none recorded yet" at 15:06:14,
// seventy seconds and two saved bookings later. It corrected only because a
// handoff built a new agent instance.
//
// The comment that recorded the assumption said "C11: rendered at session
// start, never re-rendered", which was true when a prompt could only name a
// call variable. A call variable does not change mid-call. A declared value
// does, and both reach the prompt through the same placeholders.
func TestLiveKitRefreshesTheOwnerPromptAfterAStepWritesState(t *testing.T) {
	t.Parallel()

	got := emitted(t, loadExample(t, "salon-concierge-v2"), ir.ProviderLiveKit)

	// Every agent that owns an assigning step declares the method, and the
	// method re-renders rather than doing something else.
	if strings.Count(got, "async def _refresh_prompt(self) -> None:") != 2 {
		t.Errorf("want _refresh_prompt on both agents, got %d",
			strings.Count(got, "async def _refresh_prompt(self) -> None:"))
	}

	// Every assign site calls it. Counting the calls against the assign sites
	// is what catches a new step added without one.
	assigns := strings.Count(got, "_append_entry(ctx.userdata.") + strings.Count(got, "ctx.userdata.customer = result[")
	if assigns == 0 {
		t.Fatal("no assign sites found; this gate is testing nothing")
	}
	if calls := strings.Count(got, "await self._refresh_prompt()"); calls != 4 {
		t.Errorf("got %d refresh calls for %d assign sites, want one per step that assigns", calls, assigns)
	}

	// The call comes after the writes, not before: refreshing first renders the
	// old value and is the bug with extra steps.
	for _, step := range []string{"ctx.userdata.customer = result[", "_append_entry(ctx.userdata.appointments"} {
		write := strings.Index(got, step)
		if write < 0 {
			t.Fatalf("assign site %q is gone", step)
		}
		refresh := strings.Index(got[write:], "await self._refresh_prompt()")
		if refresh < 0 {
			t.Errorf("no refresh after %q", step)
			continue
		}
		// Nothing else may write state between the two, or that write is lost
		// from the prompt until the next step runs.
		between := got[write : write+refresh]
		if strings.Count(between, "return result") > 0 {
			t.Errorf("the step returns before refreshing the prompt, after %q", step)
		}
	}
}

// The other half, and the reason this is one gate rather than a note: the spec
// asks for the block to refresh at the same rate on both targets. Pipecat reads
// live state per request through build_chat_completion_params, so it never had
// this defect, and a change that made LiveKit refresh less often than a step
// visit would put the two back out of step.
func TestBothTargetsRefreshDeclaredStateWithoutWaitingForAnEntry(t *testing.T) {
	t.Parallel()

	livekit := emitted(t, loadExample(t, "salon-concierge-v2"), ir.ProviderLiveKit)
	pipecat := emitted(t, loadExample(t, "salon-concierge-v2"), ir.ProviderPipecat)

	// LiveKit: the render call reaches the owner outside on_enter.
	enter := strings.Index(livekit, "async def on_enter(self) -> None:")
	if enter < 0 {
		t.Fatal("livekit emits no on_enter")
	}
	if !strings.Contains(livekit, "await self._refresh_prompt()") {
		t.Error("livekit renders the owner prompt only on entry, so a value a step wrote is not in it")
	}

	// Pipecat re-renders the owner's prompt when a step returns, and it does so
	// because it has to: the step replaced the system instruction, so the owner's
	// has to be put back, and putting it back reads live state through _render.
	// That is why the two targets behaved differently from one shared block.
	// LiveKit's step return had nothing to restore, so nothing re-rendered.
	//
	// So the assertion is per owner and not global: each agent's prompt is
	// rendered more than once, which is the same guarantee _refresh_prompt now
	// gives LiveKit.
	for _, prompt := range []string{"CONCIERGE_PROMPT", "COMPLAINT_SPECIALIST_PROMPT"} {
		renders := strings.Count(pipecat, "system_instruction=_render("+prompt)
		if renders < 2 {
			t.Errorf("pipecat renders %s %d time(s); an owner prompt rendered once per entry goes stale "+
				"the moment a step writes state", prompt, renders)
		}
	}
}

// A package whose steps write no declared state emits exactly what it did
// before, method and call alike. The refresh is wired from the owner, so this
// is what proves the wiring is conditional and not just present.
func TestNoRefreshEmittedForAPackageWhoseStepsAssignNothing(t *testing.T) {
	t.Parallel()

	for _, pkg := range []string{"salon-concierge", "salon-concierge-single-prompt"} {
		got := emitted(t, loadExample(t, pkg), ir.ProviderLiveKit)
		if strings.Contains(got, "_refresh_prompt") {
			t.Errorf("%s emits _refresh_prompt; its steps assign nothing so nothing can go stale", pkg)
		}
	}
}
