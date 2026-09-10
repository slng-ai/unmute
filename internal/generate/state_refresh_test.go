package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// Explicit prompt references have to hold what a task just wrote in the prompt
// of the agent that sent the task in.
//
// LiveKit renders an agent's prompt in `on_enter`, and an agent is entered once
// per call, so the rendered reference froze at whatever it held before the first step
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

	got := emitted(t, loadExample(t, "salon-concierge-v3"), ir.ProviderLiveKit)

	// The owner whose prompt reads saved state declares the method.
	if strings.Count(got, "async def _refresh_prompt(self) -> None:") != 2 {
		t.Errorf("want both owners to refresh their prompts, got %d",
			strings.Count(got, "async def _refresh_prompt(self) -> None:"))
	}

	// Every assign site calls it. Counting the calls against the assign sites
	// is what catches a new step added without one.
	if calls := strings.Count(got, "await self._refresh_prompt()"); calls != 4 {
		t.Errorf("got %d refresh calls, want one for each assigning task on both owners", calls)
	}

	// The call comes after the writes, not before: refreshing first renders the
	// old value and is the bug with extra steps.
	if save := strings.Index(got, `result = await ManageBooking(`); save < 0 || !strings.Contains(got[save:], "await self._refresh_prompt()") {
		t.Error("manage_booking returns without refreshing the owner's prompt")
	}
}

// The other half, and the reason this is one gate rather than a note: the spec
// asks for the block to refresh at the same rate on both targets. Pipecat reads
// live state per request through build_chat_completion_params, so it never had
// this defect, and a change that made LiveKit refresh less often than a step
// visit would put the two back out of step.
func TestBothTargetsRefreshDeclaredStateWithoutWaitingForAnEntry(t *testing.T) {
	t.Parallel()

	livekit := emitted(t, loadExample(t, "salon-concierge-v3"), ir.ProviderLiveKit)
	pipecat := emitted(t, loadExample(t, "salon-concierge-v3"), ir.ProviderPipecat)

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
	for _, prompt := range []string{"CONCIERGE_PROMPT"} {
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

	for _, pkg := range []string{"salon-concierge-single-prompt"} {
		got := emitted(t, loadExample(t, pkg), ir.ProviderLiveKit)
		if strings.Contains(got, "_refresh_prompt") {
			t.Errorf("%s emits _refresh_prompt; its steps assign nothing so nothing can go stale", pkg)
		}
	}
}

func TestPromptsContainOnlyAuthoredStateReferences(t *testing.T) {
	agent := loadTypedState(t)
	for name, task := range agent.Tasks {
		if strings.Contains(task.Instructions, "Conversation info:") || strings.Contains(task.Instructions, "Request:") {
			t.Errorf("task %s includes an automatic sharing block", name)
		}
	}
}

func TestTaskReturnsOnlyNeutralStatus(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		got := emitted(t, loadTypedState(t), provider)
		if provider == ir.ProviderLiveKit && !strings.Contains(got, "return _task_status(result)") {
			t.Error("LiveKit delegate returns raw result")
		}
		if strings.Contains(got, "Task results: ") {
			t.Error("Pipecat puts private results in owner context")
		}
		if !strings.Contains(got, `"status": "unserved" if values.get("unserved_request") else "completed"`) {
			t.Error("missing neutral outcome")
		}
	}
}
