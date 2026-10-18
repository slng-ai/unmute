package spec

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// statePrefix is how a with: value names a State value, as state.customer_phone.
const statePrefix = "state."

// lowerToolBehaviour turns what an author writes about a tool in agent.yaml
// (tool entries with outcomes:, confirm:, hidden: and start:, a step's start:,
// a handoff's start:) into the fields the compiler reads: a step's Finish,
// Replies, Assign, Announce, Opening and OpensWith, and a tool's Assign,
// Replies, Announce, AnnounceWait and Hidden.
//
// Done here, before anything reads a task, so the compiler keeps one shape to
// check and to emit. The checks that need a tool's types (a value it never
// returns, a parameter it does not take) stay in ir, where the types are.
func (p *Package) lowerToolBehaviour() error {
	for name, tool := range p.Tools {
		for i, pair := range tool.Inject {
			if text, ok := pair.Value.(string); ok && strings.HasPrefix(text, statePrefix) {
				tool.Inject[i].Value = "{{" + text + "}}"
			}
		}
		p.Tools[name] = tool
	}
	seen := map[string]bool{}
	for _, entry := range p.Agent.Tools {
		where := p.Location("agent.yaml", "name: "+entry.Name)
		if seen[entry.Name] {
			return fmt.Errorf("%s: tools: lists %s twice; keep one entry", where, entry.Name)
		}
		seen[entry.Name] = true
		if entry.Bare() {
			continue
		}
		tool := p.Tools[entry.Name]
		if len(entry.Confirm) > 0 {
			return fmt.Errorf("%s: confirm: reads a change back before a step ends on it, so it goes on the tool's entry under that step's tools", where)
		}
		if entry.Start != nil {
			tool.Announce, tool.AnnounceWait = entry.Start.Say, entry.Start.Wait
		}
		tool.Hidden, tool.Assign = entry.Hidden, entry.Save
		replies, err := packageOutcomes(entry)
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		tool.Replies = replies
		p.Tools[entry.Name] = tool
	}
	for _, agent := range slices.Sorted(maps.Keys(p.Agent.Agents)) {
		for _, item := range p.Agent.Agents[agent].Tasks {
			if item.Task == nil {
				continue
			}
			if err := p.lowerTask(item.Task); err != nil {
				return fmt.Errorf("%s: task %q: %w", p.Location("agent.yaml", "name: "+item.Task.Name), item.Task.Name, err)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(p.Agent.Handoffs)) {
		handoff := p.Agent.Handoffs[name]
		if handoff.Start == nil {
			continue
		}
		opens, err := p.startCall(handoff.Start.Call, handoff.Start.With)
		if err != nil {
			return fmt.Errorf("%s: handoff %q: %w", p.Location("agent.yaml", name+":"), name, err)
		}
		handoff.OpensWith = opens
		p.Agent.Handoffs[name] = handoff
	}
	return nil
}

// packageOutcomes lowers the outcomes of a tool listed at the top of
// agent.yaml. They run on every agent, outside any step, so none can end one.
func packageOutcomes(entry ToolEntry) ([]Reply, error) {
	if err := checkOutcomeOrder(entry); err != nil {
		return nil, err
	}
	var replies []Reply
	for _, outcome := range entry.Outcomes {
		if outcome.Then != "" {
			return nil, fmt.Errorf("an outcome of %s says then: %s, and a tool listed at the top of agent.yaml runs outside any step, so it has no step to end. "+
				"Move the outcome under the step's tools list", entry.Name, outcome.Then)
		}
		if len(outcome.Say) == 0 {
			return nil, fmt.Errorf("an outcome of %s has no say:. To save on every result, write save: on the tool's entry, beside its outcomes", entry.Name)
		}
		replies = append(replies, Reply{When: outcome.If, Say: outcome.Say, Save: outcome.Save})
	}
	return replies, nil
}

// lowerTask lowers a step's start: and its tool entries.
func (p *Package) lowerTask(task *Task) error {
	task.Tools = ToolNames(task.ToolEntries)
	if start := task.Start; start != nil {
		task.Announce = start.Say
		if start.Listen {
			task.Opening = "listen"
		}
		switch {
		case start.Call != "":
			opens, err := p.startCall(start.Call, start.With)
			if err != nil {
				return err
			}
			task.OpensWith = opens
		case len(start.With) > 0:
			return fmt.Errorf("start: has with: and no call:; with: passes State to the tool call: names")
		}
	}
	saved := map[string]bool{}
	for _, pair := range task.Save {
		saved[pair.Key] = true
		task.Assign = append(task.Assign, pair)
	}
	var doneSaves, doneBare []string
	for _, entry := range task.ToolEntries {
		if entry.Start != nil || len(entry.Hidden) > 0 || len(entry.Save) > 0 {
			return fmt.Errorf("start:, hidden: and save: on %s are the same on every agent, so they go on the tool's entry in the top-level tools list. "+
				"A save that only one result makes goes under that outcome", entry.Name)
		}
		if err := checkOutcomeOrder(entry); err != nil {
			return err
		}
		ends := false
		for _, outcome := range entry.Outcomes {
			if outcome.Then == "" {
				if len(outcome.Say) == 0 {
					return fmt.Errorf("an outcome of %s neither says a line nor ends the step; add say:, or then: done", entry.Name)
				}
				task.Replies = append(task.Replies, Reply{Tool: entry.Name, When: outcome.If, Say: outcome.Say, Save: outcome.Save})
				continue
			}
			finish := FinishEntry{Tool: entry.Name, Success: outcome.If, Confirm: entry.Confirm, Say: outcome.Say, Save: outcome.Save, Ends: "completed"}
			if outcome.Then == "failed" {
				finish.Ends = "failed"
			} else {
				ends = true
				if len(outcome.Save) > 0 {
					doneSaves = append(doneSaves, entry.Name)
				} else {
					doneBare = append(doneBare, entry.Name)
				}
			}
			task.Finish = append(task.Finish, finish)
			for _, pair := range outcome.Save {
				if !saved[pair.Key] {
					saved[pair.Key] = true
					task.Assign = append(task.Assign, pair)
				}
			}
		}
		if len(entry.Confirm) > 0 && !ends {
			return fmt.Errorf("%s has confirm: and no outcome with then: done. A read-back asks before the change that ends the step, so add that outcome", entry.Name)
		}
	}
	// A done outcome with no save: of its own saves the step's save: rows from
	// the tool's result, which is unmute-core's rule for a rule without rows.
	// Beside an outcome that does save, that would also save the other
	// outcome's values, which nobody means, so the mix is refused.
	if len(doneSaves) > 0 && len(doneBare) > 0 {
		return fmt.Errorf("an outcome of %s ends the step with then: done and no save:, while one of %s has a save: of its own. "+
			"An outcome with no save: saves every value of the step, so give this one a save: too", doneBare[0], doneSaves[0])
	}
	return nil
}

// startCall lowers a start: block's call: and with:. Each with: value names
// State as state.<name>, and a trailing ? marks one the call runs without.
//
// It sets no line. The call's result goes through the tool's outcomes like any
// other call, and unmute-core says the line they pick (0.18.1 on a step, and
// always on a handoff).
func (p *Package) startCall(call string, with []Pair) (*OpensWith, error) {
	if call == "" {
		return nil, fmt.Errorf("start: needs call: naming the tool to run")
	}
	opens := &OpensWith{Tool: call}
	for _, pair := range with {
		text, _ := pair.Value.(string)
		name, optional := strings.CutSuffix(strings.TrimSpace(text), "?")
		name, ok := strings.CutPrefix(name, statePrefix)
		if !ok {
			return nil, fmt.Errorf("with: passes %s as %q; it reads State as state.<name>, as - %s: state.%s", pair.Key, text, pair.Key, pair.Key)
		}
		opens.Arguments = append(opens.Arguments, Pair{Key: pair.Key, Value: name})
		if optional {
			opens.Optional = append(opens.Optional, pair.Key)
		}
	}
	return opens, nil
}

// checkOutcomeOrder holds a tool's outcomes to the order unmute-core runs them
// in. Core tries every rule that ends the step before any line that does not,
// so the ones with then: are written first, then the ones without, then else:.
// Written that way, the order on the page is the order that runs.
func checkOutcomeOrder(entry ToolEntry) error {
	withoutThen := false
	for i, outcome := range entry.Outcomes {
		if outcome.Else == (len(outcome.If) > 0) {
			return fmt.Errorf("an outcome of %s starts with if: or else:, and this one has %s", entry.Name, map[bool]string{true: "both", false: "neither"}[outcome.Else])
		}
		switch outcome.Then {
		case "", "done", "failed":
		default:
			return fmt.Errorf("an outcome of %s says then: %s; write done or failed, or leave then: out", entry.Name, outcome.Then)
		}
		if outcome.Else {
			switch {
			case i != len(entry.Outcomes)-1:
				return fmt.Errorf("else: is the last outcome of %s: it catches every result no if: above it matched", entry.Name)
			case outcome.Then == "done":
				return fmt.Errorf("else: on %s says then: done. A step completes only on a result an if: names; end else: with then: failed, or leave then: out", entry.Name)
			case outcome.Then == "failed" && withoutThen:
				return fmt.Errorf("else: on %s says then: failed after an outcome without then:. unmute-core tries every outcome that ends the step first, so this else: would hide that one. "+
					"Remove then: from else:, or end the other outcome with then: too", entry.Name)
			}
			continue
		}
		if outcome.Then == "" {
			withoutThen = true
		} else if withoutThen {
			return fmt.Errorf("an outcome of %s with then: comes after one without. Write the outcomes that end the step first: unmute-core tries them before any other", entry.Name)
		}
	}
	return nil
}
