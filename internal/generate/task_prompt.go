package generate

import (
	"strings"

	"github.com/slng-ai/unmute/internal/ir"
)

// unservedOwnerRule is the other half of the escape, read by the agent that owns
// the step: LiveKit appends it to the delegate tool's docstring, Pipecat to the
// developer message that hands the results back. Without it the field arrives in
// a result nobody was told to read.
const unservedOwnerRule = "A completed status means the step finished. Read any saved values through your own prompt references. " +
	"An unserved status means the step could not help. Ask the caller what they need, then use your tools or a handoff."

// terminalOwnerRule is added to that when a step of the flow ends on its own
// tool. Such a step consumes the caller's turn and the owner is handed it back
// immediately before the status, so the owner has to be told what it is looking
// at: the words above the status are what the step answered, not a new request.
const terminalOwnerRule = " The caller's words right above the status are what the step answered; it served the part it owns, so act only on what remains."

// joinWords is a list as a sentence reads it: "a", "a or b", "a, b or c".
func joinWords(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	default:
		return strings.Join(words[:len(words)-1], ", ") + " or " + words[len(words)-1]
	}
}

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
	return taskFinishContractFor(finishName, resultNames, nil)
}

// taskFinishContractFor is the same tail with the sentences a step that ends on
// its own tool needs. Told out loud because the model otherwise calls `finish`
// straight after the tool, which is the request the step just avoided: it has
// been instructed to finish since the first version of this contract, and a
// tool ending the step is the exception.
//
// The list is the step's terminal tools, sorted. Empty gives exactly the tail
// every package had before this feature, byte for byte.
func taskFinishContractFor(finishName string, resultNames, endsOn []string) string {
	call := "`" + finishName + "`"
	tail := "\n\nWhen this step is complete, call " + call
	if len(resultNames) > 0 {
		tail += " with: " + strings.Join(resultNames, ", ")
	}
	tail += "."
	if len(endsOn) > 0 {
		quoted := make([]string, len(endsOn))
		for i, name := range endsOn {
			quoted[i] = "`" + name + "`"
		}
		tail += "\n\nWhen " + joinWords(quoted) + " returns a successful result, this step ends by " +
			"itself and saves it; do not call " + call + " after it. " + call + " is for a request this " +
			"step cannot serve, for values you already hold, and for recording a result whose save was refused."
	}
	return tail + "\n\n`" + ir.UnservedResultField + "` is for a request this step " +
		"cannot serve. Do this step's own work first, and never use it to skip that " +
		"work: the caller's original reason for being here is not an unserved request. " +
		"If a handoff here covers what they want, call that handoff instead. Only when " +
		"no tool and no handoff here can serve what the caller is asking, call " + call +
		" with their request in `" +
		ir.UnservedResultField + "`, in their own words, rather than refusing or " +
		"explaining what you cannot do here. The agent that owns this step reads that " +
		"status and takes the caller from there."
}
