//go:build smoke

package generate

import (
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

func TestSmokePipecatProviderHistory(t *testing.T) {
	lastN := pipecatMethodBody(t, pipecatHistoryBot(t), "def _last_n(", "\n\n\n")
	full, _ := pipecatCtxExpr(ir.TaskContext{History: ir.HistoryFull})
	runPipecatSmokeScript(t, "salon-concierge", nil, nil,
		pipecatProviderHistorySmokeScript+"\nexec("+pyQuote(lastN)+")\nfull_expr = "+pyQuote(full)+"\ncheck()\n")
}

const pipecatProviderHistorySmokeScript = `"""Real provider metadata at every context boundary, without a provider call."""
import ast
import copy
import json
from types import SimpleNamespace

from pipecat.processors.aggregators.llm_context import LLMContext, LLMSpecificMessage

module = ast.parse(open("bot.py").read())
for node in module.body:
    if isinstance(node, ast.FunctionDef) and node.name in (
        "_caller_turns", "_newest_caller_message", "_speech_only", "_settle_task_call",
    ):
        exec(compile(ast.Module(body=[node], type_ignores=[]), "bot.py", "exec"))


def check():
    signature = LLMSpecificMessage(llm="google", message={
        "type": "thought_signature", "signature": b"test-signature",
        "bookmark": {"function_call": "new"},
    })
    caller = {"role": "user", "content": "Book a haircut."}
    old_call = {"role": "assistant", "tool_calls": [{"id": "old", "function": {"name": "book"}}]}
    old_reply = {"role": "tool", "tool_call_id": "old", "content": "earlier result"}
    call = {"role": "assistant", "content": "Checking.", "tool_calls": [{"id": "new", "function": {"name": "book"}}]}
    reply = {"role": "tool", "tool_call_id": "new", "content": "running"}
    messages = [signature, caller, old_call, old_reply, signature, call, signature, reply, signature]
    assert _caller_turns(messages) == 1
    assert _newest_caller_message(messages) == caller
    assert _newest_caller_message([signature]) is None
    assert _speech_only(messages) == [caller, {"role": "assistant", "content": "Checking."}]
    for status in ("completed", "unserved"):
        snapshot = copy.deepcopy(messages)
        _settle_task_call(snapshot, "book", {"status": status})
        assert snapshot[3]["content"] == "earlier result"
        assert json.loads(snapshot[-2]["content"]) == {"status": status}
        assert snapshot[0] == signature and snapshot[0] is not signature
        assert messages[-2]["content"] == "running"

    self = SimpleNamespace(context=LLMContext(messages=[{"role": "system", "content": "old prompt"}] + messages))
    full = eval(full_expr)
    assert full == messages
    assert full[0] is not signature
    assert full[0].message["signature"] == b"test-signature"

    # Metadata at the front must not hide an orphaned tool reply. Metadata
    # within the retained window survives; the authored limit is unchanged.
    assert _last_n(messages, 0) == []
    assert _last_n([caller, signature, reply, signature, caller], 4) == [signature, signature, caller]
    assert _last_n([caller, call, signature, reply, signature], 4) == [call, signature, reply, signature]
    print("provider history ok: caller turns, speech, full history, bounded history and task settlement")
`
