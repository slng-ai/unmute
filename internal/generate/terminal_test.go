package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// terminalModule compiles the spec 010 fixture for one target and returns the
// emitted driver module. One fixture for the whole feature, because the three
// keys interact: a group that skips a step whose confirmation holds, a step that
// ends on its own tool, and a handoff out of that step.
func terminalModule(t *testing.T, targetName, file string) string {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "terminal_step"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, agent.Targets[targetName], target.Default())
	if err != nil {
		t.Fatalf("generate %s: %v", targetName, err)
	}
	return artifactFile(t, artifact, file)
}

func terminalModules(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"livekit": terminalModule(t, "livekit", "agent.py"),
		"pipecat": terminalModule(t, "pipecat", "bot.py"),
	}
}

// The shared helpers are rendered once, above both drivers, so the two targets
// cannot answer "did this tool succeed" differently.
func TestTerminalHelpersAreByteIdenticalOnBothTargets(t *testing.T) {
	modules := terminalModules(t)
	for _, marker := range []string{"def _terminal_success(", "def _merge_retained(", "def _success_word("} {
		livekit := blockAfter(t, modules["livekit"], marker)
		pipecat := blockAfter(t, modules["pipecat"], marker)
		if livekit != pipecat {
			t.Errorf("%s differs between targets:\n--- livekit ---\n%s\n--- pipecat ---\n%s", marker, livekit, pipecat)
		}
	}
}

// LiveKit logs through stdlib logging and Pipecat through loguru, so a shared
// line carrying either style prints literally on the other target.
func TestTerminalHelpersCarryNoLibrarySpecificPlaceholder(t *testing.T) {
	for name, module := range terminalModules(t) {
		for _, line := range strings.Split(module, "\n") {
			if !strings.Contains(line, "logger.") {
				continue
			}
			if !strings.Contains(line, "terminal") && !strings.Contains(line, "withdrew") && !strings.Contains(line, "carried") && !strings.Contains(line, "skipped") && !strings.Contains(line, "group stopped") && !strings.Contains(line, "handoff held") && !strings.Contains(line, "step ") {
				continue
			}
			if strings.Contains(line, "%s") || strings.Contains(line, ".format(") {
				t.Errorf("%s: a shared log line carries a library-specific placeholder: %s", name, strings.TrimSpace(line))
			}
		}
	}
}

// A step that names a tool under finish: ends on it: the tool's own method hands
// the result to the step's ending, and the ending completes without a reply.
func TestTerminalToolEndsTheStepOnBothTargets(t *testing.T) {
	livekit := terminalModule(t, "livekit", "agent.py")
	for _, want := range []string{
		"return await self._end_on_book_it(ctx, result)",
		"return await self._end_on_cancel_it(ctx, result)",
		"return await self._end_on_look_up(ctx, result)",
		`_values = _save_result("book", ctx.userdata, result)`,
		"self.complete(_values)",
		// None, so livekit-agents sets reply_required False and makes no
		// request. This one line is the feature.
		"        return None",
	} {
		if !strings.Contains(livekit, want) {
			t.Errorf("livekit agent.py missing %q", want)
		}
	}
	pipecat := terminalModule(t, "pipecat", "bot.py")
	for _, want := range []string{
		"async def _do_book_terminal_book_book_it(self, args, flow_manager):",
		"result = await _flow_tool_book_it(args, flow_manager, state=self.state)",
		`_values = _save_result("book", self.state, result)`,
		// The wrapper saves once and hands the saved values to the finish
		// handler's tail. Going through the finish handler saved twice, and a
		// second save is a second entry on every list an assign appends to.
		"return await self._do_book_advance_book(_values)",
		"async def _do_book_advance_book(self, _values):",
	} {
		if !strings.Contains(pipecat, want) {
			t.Errorf("pipecat bot.py missing %q", want)
		}
	}
}

// A result that is not a success is an ordinary result: it goes back to the
// model and the step stays open, which is what keeps a failed booking a
// conversation rather than a saved one.
func TestTerminalToolLeavesANonSuccessToTheModel(t *testing.T) {
	for name, module := range terminalModules(t) {
		if !strings.Contains(module, `_terminal_success(result, {"status": ("booked",)})`) {
			t.Errorf("%s does not check the success values the package declared", name)
		}
		if !strings.Contains(module, "return result") {
			t.Errorf("%s does not hand a non-success back to the model", name)
		}
	}
}

// The action happened and the save did not. The result stays, the refusal says
// how to record it, and the step stays open.
func TestARefusedSaveKeepsTheResultAndSaysHowToRepair(t *testing.T) {
	for name, module := range terminalModules(t) {
		if !strings.Contains(module, "except _StateRefused as refused:") {
			t.Errorf("%s does not catch a refused save in its terminal path", name)
		}
		for _, want := range []string{
			`"refused": "Not recorded: "`,
			"recover a missing one with a read tool of this step",
			"finish with unserved_request saying the action happened",
		} {
			if !strings.Contains(module, want) {
				t.Errorf("%s: the repair instruction is missing %q", name, want)
			}
		}
		if !strings.Contains(module, "**result,") {
			t.Errorf("%s: the refusal drops the tool's own result", name)
		}
	}
}

// After one mutation of a step succeeds, none of that step's mutations runs
// again in the same invocation. Different arguments do not prove a new request:
// during a save repair the model could change a time and book twice.
func TestASucceededMutationIsClosedForTheRestOfTheInvocation(t *testing.T) {
	livekit := terminalModule(t, "livekit", "agent.py")
	for _, want := range []string{
		"if self._terminal is not None:",
		"book_it already succeeded in this step",
		"cancel_it already succeeded in this step",
	} {
		if !strings.Contains(livekit, want) {
			t.Errorf("livekit agent.py missing the closure %q", want)
		}
	}
	// The guard sits before the handler body, not after it: a second booking
	// that ran and was then refused is a second booking.
	guardAt := strings.Index(livekit, "if self._terminal is not None:")
	callAt := strings.Index(livekit, "result = tools.book_it.book_it(")
	if guardAt < 0 || callAt < guardAt {
		t.Errorf("the closure must be checked before the tool runs: guard=%d call=%d", guardAt, callAt)
	}
	pipecat := terminalModule(t, "pipecat", "bot.py")
	if !strings.Contains(pipecat, "if self._do_book_terminal is not None:") {
		t.Error("pipecat bot.py does not close the step's mutations")
	}
	guardAt = strings.Index(pipecat, "if self._do_book_terminal is not None:")
	callAt = strings.Index(pipecat, "result = await _flow_tool_book_it(")
	if guardAt < 0 || callAt < guardAt {
		t.Errorf("pipecat must check the closure before the tool runs: guard=%d call=%d", guardAt, callAt)
	}
}

// The step consumed a caller turn the owner never saw, so the owner is handed it
// back immediately before the status, once.
func TestTheCarriedTurnReachesTheOwnerOnce(t *testing.T) {
	livekit := terminalModule(t, "livekit", "agent.py")
	for _, want := range []string{
		"def _newest_caller_turn(chat_ctx):",
		"def _insert_carried_turn(owner_ctx, carried):",
		"if owner_ctx.get_by_id(item.id) is None:",
		"_insert_carried_turn(owner_ctx, _carried)",
	} {
		if !strings.Contains(livekit, want) {
			t.Errorf("livekit agent.py missing %q", want)
		}
	}
	pipecat := terminalModule(t, "pipecat", "bot.py")
	for _, want := range []string{
		"def _newest_caller_message(messages):",
		// Decided where the turn is captured, by counting the caller's turns
		// against the owner's snapshot: matching on text dropped a caller who
		// said the same word twice.
		"def _caller_turns(messages):",
		"self._do_book_snapshot_turns = _caller_turns(self.context.get_messages())",
		"if _caller_turns(_messages) > self._do_book_snapshot_turns:",
		"_carried_messages(self._do_book_carried_turn)",
	} {
		if !strings.Contains(pipecat, want) {
			t.Errorf("pipecat bot.py missing %q", want)
		}
	}
	// The owner is told what it is reading, or the words above the status look
	// like a fresh request and the flow runs again.
	for name, module := range terminalModules(t) {
		if !strings.Contains(module, "The caller's words right above the status are what the step answered") {
			t.Errorf("%s: the owner is not told what the carried turn is", name)
		}
	}
}

// blockAfter is the emitted definition starting at marker, up to the next one,
// so two targets can be compared and one node asserted on without matching
// whole files. It stops at whichever comes first: the next method, the next
// class, or a module-level blank-line terminator.
func blockAfter(t *testing.T, module, marker string) string {
	t.Helper()
	at := strings.Index(module, marker)
	if at < 0 {
		t.Fatalf("module does not define %s", marker)
	}
	rest := module[at+len(marker):]
	end := len(rest)
	for _, boundary := range []string{"\n    def ", "\n    async def ", "\n    @", "\nclass ", "\ndef ", "\n\n\n"} {
		if found := strings.Index(rest, boundary); found >= 0 && found < end {
			end = found
		}
	}
	return marker + rest[:end]
}
