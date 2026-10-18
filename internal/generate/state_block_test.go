package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// TestStateBlockPromptsReadWholeWithEveryValueEmpty is FR-005a, extending the
// gate the pre-fetched values already have.
//
// A declared value with no contents renders as words, so every sentence naming
// it reads whole. The failure this prevents reaches a caller as speech: "your
// appointments are None".
func TestStateBlockPromptsReadWholeWithEveryValueEmpty(t *testing.T) {
	agent := loadTypedState(t)
	var declared []string
	for name, variable := range agent.Variables {
		if variable.Structured() {
			declared = append(declared, name)
		}
	}
	if len(declared) == 0 {
		t.Fatal("the fixture declares nothing structured, so this gate proves nothing")
	}
	// Exactly the state a call renders at its first turn: every declared value
	// empty, so every one of them renders as words.
	prompts := append(mapValues(promptBodies(agent.Agents)), mapValues(taskBodies(agent.Tasks))...)
	for _, prompt := range prompts {
		rendered := prompt
		for _, name := range declared {
			rendered = strings.ReplaceAll(rendered, "{{"+name+"}}", ir.StateEmptyText())
		}
		if rendered == prompt {
			continue // this prompt names none of them
		}
		for _, line := range strings.Split(rendered, "\n") {
			if strings.Contains(line, "None") {
				t.Errorf("a prompt renders the word None where a declared value was empty: %q", line)
			}
			if strings.Contains(line, "[]") || strings.Contains(line, "null") {
				t.Errorf("a prompt renders an empty structure where a declared value was empty, and a step "+
					"cannot tell that from a decision the caller made: %q", line)
			}
			for _, dangling := range []string{" is .", " on .", " for .", " at .", " to .", ": .", " is ?"} {
				if strings.Contains(line, dangling) {
					t.Errorf("a prompt has a dangling %q where a declared value was empty: %q",
						strings.TrimSpace(dangling), line)
				}
			}
		}
	}
}
