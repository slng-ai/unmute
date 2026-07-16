// Package ir holds the canonical shape of an agent spec. For now that is only
// the prompt fragment set; typed structs + JSON Schema derivation arrive with
// `unmute validate` (deferred — see SPEC §C).
package ir

// PromptFragments is the canonical system-prompt fragment set, in compile order
// (ADR-0004). `unmute init` scaffolds exactly these; a later `compile` will
// concatenate them in this order, appending any unknown extra *.md
// alphabetically. Single source of truth for both commands.
var PromptFragments = []string{
	"identity",
	"output-rules",
	"personality",
	"goals",
	"guardrails",
	"user-information",
}
