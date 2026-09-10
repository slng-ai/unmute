package generate

import (
	"strings"
	"testing"
)

// opening: listen speaks one fixed line and waits. No request is made to open
// the step, which is the third of the three this feature removes.
func TestListeningOpeningMakesNoRequest(t *testing.T) {
	livekit := terminalModule(t, "livekit", "agent.py")
	enter := livekitOnEnter(t, livekit, "class TakeNote(")
	if !strings.Contains(enter, `self.session.say("What would you like me to pass on?")`) {
		t.Errorf("the listening step does not speak its line:\n%s", enter)
	}
	if strings.Contains(enter, "generate_reply") {
		t.Errorf("the listening step still opens with a model request:\n%s", enter)
	}
	pipecat := terminalModule(t, "pipecat", "bot.py")
	node := blockAfter(t, pipecat, "def _take_note_node_take_note(self)")
	for _, want := range []string{
		`{"role": "assistant", "content": "What would you like me to pass on?"}`,
		"respond_immediately=False,",
		`"type": "tts_say",`,
		`"append_text_to_context": False,`,
	} {
		if !strings.Contains(node, want) {
			t.Errorf("the pipecat listening node is missing %q:\n%s", want, node)
		}
	}
}

// The line has to survive the step's own history policy. On Pipecat a node's
// RESET replaces the whole message list after pre-actions have run, so a line
// the pre-action wrote into the context would be wiped: it is seeded in
// task_messages, which the reset carries, and the pre-action only speaks it.
func TestListeningOpeningSurvivesAReset(t *testing.T) {
	pipecat := terminalModule(t, "pipecat", "bot.py")
	node := blockAfter(t, pipecat, "def _take_note_node_take_note(self)")
	seedAt := strings.Index(node, `{"role": "assistant", "content": "What would you like me to pass on?"}`)
	actionAt := strings.Index(node, `"append_text_to_context": False,`)
	if seedAt < 0 || actionAt < 0 {
		t.Fatalf("the listening node is not built the way a reset survives:\n%s", node)
	}
	if !strings.Contains(node, "ContextStrategy.RESET") {
		t.Errorf("the fixture's listening step declares history: reset and the node does not reset:\n%s", node)
	}
	livekit := terminalModule(t, "livekit", "agent.py")
	enter := livekitOnEnter(t, livekit, "class TakeNote(")
	resetAt := strings.Index(enter, "await self.update_chat_ctx(llm.ChatContext())")
	sayAt := strings.Index(enter, "self.session.say(")
	if resetAt < 0 || sayAt < resetAt {
		t.Errorf("the line must be said after the history policy is applied: reset=%d say=%d", resetAt, sayAt)
	}
}

// The default is what every package had before this key existed, so a step that
// writes nothing opens exactly as it did.
func TestOpeningGenerateIsByteIdenticalToToday(t *testing.T) {
	livekit := terminalModule(t, "livekit", "agent.py")
	for _, class := range []string{"class Verify(", "class Book("} {
		body := livekitOnEnter(t, livekit, class)
		if !strings.Contains(body, "self.session.generate_reply(") {
			t.Errorf("%s should still open with a model request", class)
		}
		if strings.Contains(body, "opening: listen") {
			t.Errorf("%s carries the listening comment and should not", class)
		}
	}
	pipecat := terminalModule(t, "pipecat", "bot.py")
	node := blockAfter(t, pipecat, "def _do_book_node_verify(self)")
	if !strings.Contains(node, `task_messages=[{"role": "developer", "content": "Begin this step."}]`) {
		t.Errorf("a generating step no longer seeds the developer turn it always seeded:\n%s", node)
	}
	if strings.Contains(node, "respond_immediately") {
		t.Errorf("a generating step must not carry respond_immediately:\n%s", node)
	}
}

// livekitOnEnter is one AgentTask class's on_enter body, which is where an
// opening is decided.
func livekitOnEnter(t *testing.T, module, class string) string {
	t.Helper()
	at := strings.Index(module, class)
	if at < 0 {
		t.Fatalf("module does not define %s", class)
	}
	rest := module[at:]
	start := strings.Index(rest, "async def on_enter")
	if start < 0 {
		t.Fatalf("%s has no on_enter", class)
	}
	rest = rest[start:]
	if end := strings.Index(rest, "\n    @"); end >= 0 {
		return rest[:end]
	}
	return rest
}
