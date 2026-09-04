package ir

import (
	"strings"
	"testing"
)

// A step is only entered when it offers the model something the agent above it
// does not already have. When it does not, the agent finishes the job itself and
// the step's `assign:` never runs, so declared state records nothing.
//
// Found on a live call: `record_complaint` sat on a complaint specialist as well
// as on its `handle_complaint` step. The specialist recorded the complaint,
// answered the caller sensibly, and the state block still read
// "Complaints: none recorded yet" at the end of the call. The specialist's own
// prompt said "then run the complaint step in the same turn, silently", so this
// is not a prompt that was missing a rule. A tool within reach beat the prompt.
func TestWarnsWhenAnAgentHoldsEveryToolOfItsStep(t *testing.T) {
	t.Parallel()

	build := func(agentTools, stepTools []string) *Agent {
		return &Agent{
			Agents: map[string]AgentDef{
				"specialist": {Tools: append(append([]string{}, agentTools...), "run_step")},
			},
			Tasks: map[string]Task{
				"handle_complaint": {Tools: stepTools},
			},
			Controls: map[string]Control{
				"run_step": &Delegate{Kind: ControlDelegate, Task: "handle_complaint"},
			},
			Tools: map[string]Tool{
				"record_complaint":      {},
				"look_up_refund_policy": {},
			},
		}
	}

	t.Run("every tool shared warns and names the tool to move", func(t *testing.T) {
		t.Parallel()
		warnings := warnOnStepsThatOfferNothing(build(
			[]string{"look_up_refund_policy", "record_complaint"},
			[]string{"record_complaint"},
		))
		if len(warnings) != 1 {
			t.Fatalf("got %d warnings, want 1: %v", len(warnings), warnings)
		}
		// The message has to carry all four, because the fix is a move and the
		// author needs to know what to move and where from.
		for _, want := range []string{"specialist", "handle_complaint", "record_complaint", "leave them on the step"} {
			if !strings.Contains(warnings[0], want) {
				t.Errorf("warning does not name %q: %s", want, warnings[0])
			}
		}
	})

	t.Run("one tool the agent lacks is a reason to enter, so no warning", func(t *testing.T) {
		t.Parallel()
		// The shape the fix produces: the read stays on the agent, the write is
		// the step's alone, so the step holds the only route to it.
		if warnings := warnOnStepsThatOfferNothing(build(
			[]string{"look_up_refund_policy"},
			[]string{"record_complaint"},
		)); len(warnings) != 0 {
			t.Errorf("warned on a step that adds a tool: %v", warnings)
		}
	})

	t.Run("a step declaring no tools is left alone", func(t *testing.T) {
		t.Parallel()
		// Its prompt, its history scope and its typed result are all reasons to
		// enter it that have nothing to do with tools, so an empty list is not
		// the subset this warning is about.
		if warnings := warnOnStepsThatOfferNothing(build(
			[]string{"record_complaint"},
			nil,
		)); len(warnings) != 0 {
			t.Errorf("warned on a step that declares no tools: %v", warnings)
		}
	})

	t.Run("a tool on a different agent is not this agent's short route", func(t *testing.T) {
		t.Parallel()
		agent := build([]string{"look_up_refund_policy"}, []string{"record_complaint"})
		agent.Agents["concierge"] = AgentDef{Tools: []string{"record_complaint"}}
		if warnings := warnOnStepsThatOfferNothing(agent); len(warnings) != 0 {
			t.Errorf("warned because another agent holds the tool: %v", warnings)
		}
	})
}

// The warning has to reach a caller of Validate, not just its own function.
// A check whose result is collected and dropped reads as wired up and reaches
// nobody, which has happened in this file's history before.
func TestTheStepWarningReachesTheValidationReport(t *testing.T) {
	t.Parallel()

	agent := &Agent{
		Agents: map[string]AgentDef{
			"specialist": {Tools: []string{"record_complaint", "run_step"}},
		},
		Tasks:    map[string]Task{"handle_complaint": {Tools: []string{"record_complaint"}}},
		Controls: map[string]Control{"run_step": &Delegate{Kind: ControlDelegate, Task: "handle_complaint"}},
		Tools:    map[string]Tool{"record_complaint": {}},
	}

	_, warnings := validateStructure(agent)
	var found bool
	for _, warning := range warnings {
		if strings.Contains(warning, "holds every tool of its step") {
			found = true
		}
	}
	if !found {
		t.Errorf("validateStructure dropped the warning; got %v", warnings)
	}
}
