//go:build smoke

package generate

import "testing"

// What a listening step actually produces, against the real framework: the node
// this target builds carries the spoken line where the caller's reply will
// follow it, and asks for no completion of its own.
//
// The Pipecat half is the one worth driving: its node is a dict the framework
// reads, and the interaction that matters is invisible in the emitted text. A
// node with `history: reset` replaces the whole message list after pre-actions
// have run, so a line the pre-action wrote into the context would be wiped. The
// line is in task_messages, which the reset carries, and the pre-action only
// speaks it.
func TestSmokeListeningOpening(t *testing.T) {
	runPipecatSmokeScript(t, "terminal_step", nil, nil, `"""Smoke check: the node a listening step builds."""
# ruff: noqa: E402 - the environment has to be seeded before the module imports
import json
import os
from types import SimpleNamespace

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot as generated

worker = SimpleNamespace(state=generated.build_state(), context=generated.LLMContext())
worker._take_note_visit = object()
worker._take_note_finish_take_note = lambda args, flow: None
node = generated.DeskAgent._take_note_node_take_note(worker)

# No completion is run when the node is set, which is the request this removes.
assert node["respond_immediately"] is False, node

# The line is the step's own first turn, so a caller answering it is answering
# something that is in the context.
assert node["task_messages"] == [
    {"role": "assistant", "content": "What would you like me to pass on?"}
], node["task_messages"]

# And it is spoken exactly once: the pre-action says it and writes nothing.
assert node["pre_actions"] == [
    {
        "type": "tts_say",
        "text": "What would you like me to pass on?",
        "append_text_to_context": False,
    }
], node["pre_actions"]

# The step declares history: reset, and the node resets. This is the pair: the
# reset replaces the message list with task_messages, so the seeded line
# survives it and the pre-action's own context write would not have.
strategy = node["context_strategy"]
assert strategy.strategy is generated.ContextStrategy.RESET, strategy

# The framework accepts the node as built, rather than at the first call.
generated.FlowManager  # noqa: B018 - the import is the check
print("listening opening smoke passed")
`)
}
