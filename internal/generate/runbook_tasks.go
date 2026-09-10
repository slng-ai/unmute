package generate

import (
	"github.com/slng-ai/unmute/internal/ir"
)

// The three runbook rows spec 010 adds. One shared shape per target, because the
// two runbooks say the same thing about the same package and a second copy of
// the sentence is the one that goes stale.

type runbookTerminalStep struct {
	Step  string
	Tools string // the tool names as a sentence reads them
}

type runbookSkippableStep struct {
	Group    string
	Step     string
	Variable string
}

type runbookListeningStep struct {
	Step string
}

func runbookTerminalSteps(agent *ir.Agent) []runbookTerminalStep {
	var out []runbookTerminalStep
	for _, name := range sortedKeys(agent.Tasks) {
		tools := agent.Tasks[name].EndsOnTools()
		if len(tools) == 0 {
			continue
		}
		quoted := make([]string, len(tools))
		for i, tool := range tools {
			quoted[i] = "`" + tool + "`"
		}
		out = append(out, runbookTerminalStep{Step: name, Tools: joinWords(quoted)})
	}
	return out
}

func runbookSkippableSteps(agent *ir.Agent) []runbookSkippableStep {
	var out []runbookSkippableStep
	for _, name := range sortedKeys(agent.TaskGroups) {
		for _, step := range agent.TaskGroups[name].Steps {
			if step.SkipWhenConfirmed == "" {
				continue
			}
			out = append(out, runbookSkippableStep{Group: name, Step: step.Task, Variable: step.SkipWhenConfirmed})
		}
	}
	return out
}

func runbookListeningSteps(agent *ir.Agent) []runbookListeningStep {
	var out []runbookListeningStep
	for _, name := range sortedKeys(agent.Tasks) {
		if agent.Tasks[name].Opening == ir.OpeningListen {
			out = append(out, runbookListeningStep{Step: name})
		}
	}
	return out
}
