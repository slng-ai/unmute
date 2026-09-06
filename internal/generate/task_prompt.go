package generate

import (
	"strings"

	"github.com/slng-ai/unmute/internal/ir"
)

// unservedOwnerRule is the other half of the escape, read by the agent that owns
// the step: LiveKit appends it to the delegate tool's docstring, Pipecat to the
// developer message that hands the results back. Without it the field arrives in
// a result nobody was told to read.
const unservedOwnerRule = "A result carrying `" + ir.UnservedResultField + "` means a step could not serve that request and handed it back. " +
	"The caller is still owed it: after one short line about the result, act on that request in the same turn, with your own tools, a handoff, " +
	"or the same flow again. It is a new request, so running the flow for it is not running it again for the one that just finished. " +
	"Never end the turn without acting on it, and never tell the caller you cannot."

// taskFinishContract is the compiler's own tail on every task prompt. The
// authored instructions describe the step; this names the one call that ends it,
// and covers the one case package authors almost never write down.
//
// A task node advertises its declared tools plus finish, and nothing else. When
// the caller asks for something none of those cover, a locked-down step has no
// route out: the model refuses, the caller presses, and it refuses again until
// the call dies, because nothing ever tells it that finishing is how it hands
// control back. Reproduced on a mutation step that deliberately carries no
// handoff: "book it, and also record my complaint" ran the mutation and then
// looped on "please contact the salon directly" to a caller already on the phone
// with the salon (B: salon compound request, 2026-08-20). The parent agent owns
// the handoffs, so every task is told the escape here instead of each package
// repeating the instruction.
//
// finishName differs per target: LiveKit tasks expose a plain `finish`, a
// Pipecat flow node a per-step `finish_<delegate>_<step>`.
func taskFinishContract(finishName string, resultNames []string) string {
	call := "`" + finishName + "`"
	tail := "\n\nWhen this step is complete, call " + call
	if len(resultNames) > 0 {
		tail += " with: " + strings.Join(resultNames, ", ")
	}
	return tail + ".\n\n`" + ir.UnservedResultField + "` is for a request this step " +
		"cannot serve. Do this step's own work first, and never use it to skip that " +
		"work: the caller's original reason for being here is not an unserved request. " +
		"If a handoff here covers what they want, call that handoff instead. Only when " +
		"no tool and no handoff here can serve what the caller is asking, call " + call +
		" with the closest result you have and their request in `" +
		ir.UnservedResultField + "`, in their own words, rather than refusing or " +
		"explaining what you cannot do here. The agent that owns this step reads that " +
		"field and takes the caller from there."
}
