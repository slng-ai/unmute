package ir

import (
	"fmt"
	"slices"
	"strings"
)

// The composed state block, and the one place its words are written.
//
// Every agent prompt and every task prompt ends with a block naming the
// declared values and their current contents, so a step reads declared state
// instead of re-reading the transcript. No author writes it, which is what
// makes adding a field to a shape appear in every prompt with no prompt file
// edited.
//
// Composed here, in ir.Build, above both target drivers, for two reasons that
// are worth keeping separate. One: the block has to be identical on both
// targets, and one composer above them makes that a property of the code rather
// than of a test that has to notice they stopped agreeing. Two: the block is
// appended through the seam that already exists, before checkTemplates walks
// the built prompts, so its placeholders are collected into the snapshot of
// names the router is given. Appending it in a driver instead would put its
// placeholders after that scan and the request would come back 422 mid-call.
//
// Composed **per site**, not once, because one existing refusal makes a global
// block illegal: a value awaiting the caller's agreement renders in exactly one
// prompt, the one belonging to the step that confirms it. checkTemplates walks
// the raw authored markdown and never sees this block, so that rule is this
// composer's own responsibility and it has its own gate.

// StateBlockHeading opens the block. One line, so a reader of a generated
// prompt can find where the authored text stops.
const StateBlockHeading = "Conversation info:"

// StateBlockNote is the sentence after the heading. It tells the model what the
// lines below are for, which is the whole point of the feature: the state is
// the record, and re-asking for something already in it is the failure this
// removes.
const StateBlockNote = "What this call has established so far. Read it rather than re-reading the " +
	"conversation, and never ask for something already recorded here."

// StateBlock composes the block for one prompt site.
//
// site is the string checkTemplateSite uses, so the confirm filter here
// reproduces refusal 16 exactly rather than approximately: a value carrying
// `confirm:` is included only when the site is its confirming step's own task
// instructions.
//
// Returns "" when the package declares nothing structured, which is what keeps
// appendPromptSuffix returning the prompt byte for byte (FR-015).
func (a *Agent) StateBlock(site string) string {
	names := a.stateBlockNames(site)
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(StateBlockHeading)
	b.WriteString("\n")
	b.WriteString(StateBlockNote)
	b.WriteString("\n")
	for i, name := range names {
		// A placeholder naming the whole value, never a dotted path: the emitted
		// substitution regex tokenises flat identifiers only, so a dotted name
		// would survive into the prompt as literal text.
		fmt.Fprintf(&b, "%d. %s: {{%s}}\n", i+1, stateBlockLabel(name), name)
	}
	return strings.TrimRight(b.String(), "\n")
}

// stateBlockNames is the values one site may see, in declaration order.
//
// Declaration order and not sorted: sorting is what makes a numbered list move
// under a reader who adds a field, and the author can predict what they wrote.
func (a *Agent) stateBlockNames(site string) []string {
	var names []string
	for _, name := range a.VariableOrder {
		variable, ok := a.Variables[name]
		if !ok || variable.Shape == nil {
			// Only a declared shape joins the block. A primitive variable is
			// authored into whichever prompt wants it, exactly as before, and a
			// package with no shapes gets no block at all.
			continue
		}
		// A declared secret never appears in a block: the block goes to a model
		// and into a trace (FR-012). Secrets never flow through templates
		// anyway, and this says so where the block is built rather than relying
		// on that.
		if slices.Contains(a.Secrets, name) {
			continue
		}
		// Refusal 16, reproduced. A value the caller has not agreed to renders
		// only in the prompt of the step that confirms it. Everywhere else is a
		// place the model would read a number nobody has agreed to, and the
		// worst version is not a wrong booking, it is greeting a stranger by the
		// account holder's name.
		if step := variable.Confirm; step != "" && site != confirmingSite(step) {
			continue
		}
		names = append(names, name)
	}
	return names
}

// confirmingSite is the one site an unconfirmed value renders in, spelled the
// way checkTemplateSite spells it so the two cannot drift.
func confirmingSite(step string) string { return fmt.Sprintf("task %q instructions", step) }

// stateBlockLabel is the value's name as a sentence reads it: underscores out,
// first letter up. Derived rather than authored, because a second label field
// would be a second place the same fact is written and could disagree with the
// placeholder beside it.
func stateBlockLabel(name string) string {
	words := strings.ReplaceAll(name, "_", " ")
	if words == "" {
		return words
	}
	return strings.ToUpper(words[:1]) + words[1:]
}

// AgentPromptSite and TaskPromptSite are the two site strings, and they are the
// strings the template validator already uses. Written once here so the
// composer and the validator cannot spell the same site two ways.
func AgentPromptSite(name string) string { return fmt.Sprintf("agent %q instructions", name) }

func TaskPromptSite(name string) string { return fmt.Sprintf("task %q instructions", name) }

// StateEmptyText is what a declared value with no contents renders as. Held
// here rather than in the emitted-code package so a compile-time test of a
// prompt and the runtime rendering read one string, and neither can drift into
// rendering `[]` where the other renders words (FR-005a).
func StateEmptyText() string { return stateEmptyText }

const stateEmptyText = "none recorded yet."
