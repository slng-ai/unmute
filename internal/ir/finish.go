package ir

import (
	"fmt"
	"slices"
	"strings"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/stateschema"
)

// TerminalTool is one resolved `finish:` entry: a tool of the task, and the
// result values that mean the step is done. When the tool returns a result
// meeting every pair, the step saves its `assign:` from that result and ends,
// with no model request between the tool returning and the continuation.
type TerminalTool struct {
	Tool string `json:"tool" yaml:"tool"`
	// Success maps an output field to the values that count as success. Every
	// field is required; several values on one field are alternatives. A map
	// here and a list in the authoring surface is the usual split: this is the
	// resolved shape, and every reader wants it by field name.
	Success map[string][]string `json:"success" yaml:"success"`
}

// EndsOnTools names the tools this task ends on, sorted, for the report and the
// runbook. Derived rather than stored: two copies of one fact drift. One tool
// appears once, because terminalTools refuses a second entry naming it.
func (t Task) EndsOnTools() []string {
	names := make([]string, 0, len(t.Finish))
	for _, entry := range t.Finish {
		names = append(names, entry.Tool)
	}
	slices.Sort(names)
	return names
}

// terminalTools lowers a task's `finish:` and refuses everything the emitted
// code could not honour: a tool the task does not hold, a success field the tool
// does not declare a closed set for, a value outside that set, and a result
// field the tool does not return in a shape the step can save.
//
// Every refusal names the tool, what is wrong, and what to write instead,
// because a compile-time refusal is the only place an author finds out: at
// runtime the step would simply never end on its own, which reads as the model
// being slow rather than as a package being wrong.
func terminalTools(taskName string, raw packagespec.Task, agent *Agent, result map[string]ResultField) ([]TerminalTool, error) {
	if len(raw.Finish) == 0 {
		return nil, nil
	}
	out := make([]TerminalTool, 0, len(raw.Finish))
	named := map[string]bool{}
	for _, entry := range raw.Finish {
		// One entry per tool. A second one used to be accepted and then reduced
		// to whichever was written last, because the generator keys its success
		// conditions by tool: a package naming booked, moved and cancelled in
		// three entries ended its step on cancelled alone, and the two dropped
		// values failed nothing and appeared nowhere.
		if named[entry.Tool] {
			return nil, fmt.Errorf("finish names %q twice in %q; write one entry for it and list the alternatives under the field, as `- status:` with a value per line",
				entry.Tool, taskName)
		}
		named[entry.Tool] = true
		if !slices.Contains(raw.Tools, entry.Tool) {
			return nil, fmt.Errorf("finish names %q, which %q does not list under tools:; add it there or name one it has",
				entry.Tool, taskName)
		}
		tool, ok := agent.Tools[entry.Tool]
		if !ok {
			return nil, fmt.Errorf("finish names %q, which is not a tool of this package", entry.Tool)
		}
		properties, _ := tool.Output["properties"].(map[string]any)
		success := map[string][]string{}
		for _, pair := range entry.Success {
			declared, err := declaredEnum(entry.Tool, properties, pair.Field)
			if err != nil {
				return nil, err
			}
			for _, value := range pair.Values {
				if !slices.Contains(declared, value) {
					return nil, fmt.Errorf("%s never returns %s: %s; it declares %s",
						entry.Tool, pair.Field, value, strings.Join(declared, ", "))
				}
			}
			// Same reason as the per-tool check above: a second pair on one
			// field overwrote the first rather than widening it.
			if _, twice := success[pair.Field]; twice {
				return nil, fmt.Errorf("finish on %q names %s twice; list its alternatives under one `- %s:` instead, a value per line",
					entry.Tool, pair.Field, pair.Field)
			}
			success[pair.Field] = pair.Values
		}
		if len(success) == 0 {
			return nil, fmt.Errorf("finish names %q with no success:; a step cannot end on a result nothing checks", entry.Tool)
		}
		for _, field := range sortedKeys(result) {
			if err := terminalResultFits(entry.Tool, properties, field, result[field]); err != nil {
				return nil, err
			}
		}
		out = append(out, TerminalTool{Tool: entry.Tool, Success: success})
	}
	return out, nil
}

// declaredEnum is the success grammar: a success value has to be one the tool
// declares, because a compiler that cannot read the set cannot tell a typo from
// a value, and a typo means a step that never ends by itself.
func declaredEnum(tool string, properties map[string]any, field string) ([]string, error) {
	property, ok := properties[field].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s declares no %s in its output:; a success field has to be one the tool returns", tool, field)
	}
	values, err := stringSlice(property["enum"])
	if err != nil || len(values) == 0 {
		return nil, fmt.Errorf("%s on %s has no enum; a success value has to be one the tool declares, so give the output property an enum: of its own",
			field, tool)
	}
	return values, nil
}

// terminalResultFits holds one of the task's result fields against one terminal
// tool's output. The step saves `assign:` from that output without the model in
// between, so a field the tool does not return is a save that silently records
// nothing, and a field of the wrong type is a save the validator refuses on a
// live call.
//
// The fit rule is the one a step's assign: and a pre-fetch are held to. It
// reads the tool's output as JSON Schema and the destination as the schema
// Pydantic wrote for it, so the two compare as like with like. A closed set
// the tool declares fits inside a larger one the variable allows: a tool that
// only ever returns `create` fits a variable allowing create, modify or
// cancel.
func terminalResultFits(tool string, properties map[string]any, field string, want ResultField) error {
	// The reserved escape is the model's own, never a tool's.
	if field == UnservedResultField {
		return nil
	}
	property, ok := properties[field].(map[string]any)
	if !ok {
		return fmt.Errorf("%s returns no %s; every tool under finish: has to return what assign: saves", tool, field)
	}
	returned, err := stateschema.ParseProperty(property)
	if err == nil {
		err = stateschema.Fits(returned, want.Type)
	}
	if err != nil {
		return fmt.Errorf("%s returns %s as %s: %w", tool, field, describeProperty(property), err)
	}
	return nil
}

// describeProperty names a schema property the way its author wrote it, so a
// refusal points at the tool file rather than at a Go type name.
func describeProperty(property map[string]any) string {
	word, _ := property["type"].(string)
	if word == "" {
		return "an untyped property"
	}
	if values, err := stringSlice(property["enum"]); err == nil && len(values) > 0 {
		return word + " of " + strings.Join(values, ", ")
	}
	return word
}

// taskOpening reads the `opening:` key. An omitted key is `generate`, which is
// what every package written before this key compiled to.
func taskOpening(word string) (TaskOpening, error) {
	switch TaskOpening(word) {
	case "", OpeningGenerate:
		return OpeningGenerate, nil
	case OpeningListen:
		return OpeningListen, nil
	default:
		return "", fmt.Errorf("opening: is %s or %s, and this says %q", OpeningGenerate, OpeningListen, word)
	}
}

// checkSkipWhenConfirmed holds the one thing a skip decision reads: a
// confirmation, made by the step being skipped.
//
// A variable with no `confirm:` is refused because nothing would ever set the
// flag the skip reads, so the step would run forever or never depending on
// whether the value happens to be empty. A variable confirmed by another task is
// refused because skipping this step would then turn on somebody else's work,
// which is how a caller ends up booked without being identified.
func checkSkipWhenConfirmed(step packagespec.StepItem, agent *Agent) error {
	name := step.SkipWhenConfirmed
	if name == "" {
		return nil
	}
	variable, ok := agent.Variables[name]
	if !ok {
		return fmt.Errorf("skip_when_confirmed names %q, which no variables: entry declares", name)
	}
	switch variable.Confirm {
	case "":
		return fmt.Errorf("%s carries no confirm:; skipping reads confirmation, so name a variable a step confirms", name)
	case step.Task:
		return nil
	default:
		return fmt.Errorf("%s is confirmed by %s, not by %s; put skip_when_confirmed: on that step",
			name, variable.Confirm, step.Task)
	}
}
