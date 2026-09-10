package ir

import (
	"fmt"
	"slices"
	"strings"

	packagespec "github.com/slng-ai/unmute/internal/spec"
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
// runbook. Derived rather than stored: two copies of one fact drift.
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
	for _, entry := range raw.Finish {
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
			success[pair.Field] = pair.Values
		}
		if len(success) == 0 {
			return nil, fmt.Errorf("finish names %q with no success:; a step cannot end on a result nothing checks", entry.Tool)
		}
		for _, field := range sortedKeys(result) {
			if err := terminalResultFits(entry.Tool, properties, field, result[field], agent); err != nil {
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
func terminalResultFits(tool string, properties map[string]any, field string, want ResultField, agent *Agent) error {
	// The reserved escape is the model's own, never a tool's.
	if field == UnservedResultField {
		return nil
	}
	property, ok := properties[field].(map[string]any)
	if !ok {
		return fmt.Errorf("%s returns no %s; every tool under finish: has to return what assign: saves", tool, field)
	}
	return terminalPropertyFits(tool, field, property, want.Shape, want.Type, agent)
}

// terminalPropertyFits walks a declared shape against the tool's own JSON
// Schema, field by field. assignableInto answers the plain cases and is reused
// rather than copied; the walk is the part it has no vocabulary for, because a
// tool's `object` property has no name to match a shape by.
func terminalPropertyFits(tool, field string, property map[string]any, want *TypeRef, wantPrimitive PrimitiveType, agent *Agent) error {
	if want != nil && want.List != nil {
		items, ok := property["items"].(map[string]any)
		if word, _ := property["type"].(string); word != "array" || !ok {
			return fmt.Errorf("%s returns %s as %s; the variable is %s, so the tool has to return a list of them",
				tool, field, describeProperty(property), want.String())
		}
		return terminalPropertyFits(tool, field, items, want.List, want.List.Primitive, agent)
	}
	if want != nil && want.Shape != "" {
		shape, ok := agent.Shapes[want.Shape]
		if !ok {
			return fmt.Errorf("%s is declared as %s, which no shapes: entry declares", field, want.Shape)
		}
		nested, _ := property["properties"].(map[string]any)
		if word, _ := property["type"].(string); word != "object" || nested == nil {
			return fmt.Errorf("%s returns %s as %s; the shape %s is an object, so the tool has to return one with a property per field",
				tool, field, describeProperty(property), want.Shape)
		}
		for _, member := range shape.Fields {
			child, ok := nested[member.Name].(map[string]any)
			if !ok {
				return fmt.Errorf("%s returns %s without %s; the shape %s declares it, and every tool under finish: has to return what assign: saves",
					tool, field, member.Name, want.Shape)
			}
			if err := terminalPropertyFits(tool, field+"."+member.Name, child, member.Type, member.Type.Primitive, agent); err != nil {
				return err
			}
		}
		return nil
	}
	// A closed set the tool declares that sits inside the one the variable
	// allows is assignable, which is where this parts company with a step's own
	// `assign:`. There the model produces the value and has to be told the whole
	// set; here the tool produces it, and a tool that can only ever return
	// `create` fits a variable allowing create, modify or cancel. Requiring the
	// same set would make one mutation per action impossible to declare.
	if want != nil && len(want.Literal) > 0 {
		declared := propertyResultField(property)
		if len(declared.Enum) == 0 {
			return fmt.Errorf("%s returns %s as %s; the variable allows %s, so the tool has to declare its own enum",
				tool, field, describeProperty(property), strings.Join(want.Literal, ", "))
		}
		for _, value := range declared.Enum {
			if !slices.Contains(want.Literal, value) {
				return fmt.Errorf("%s can return %s as %s, which the variable does not allow; it allows %s",
					tool, field, value, strings.Join(want.Literal, ", "))
			}
		}
		return nil
	}
	// A shape field's type is always a TypeRef, so a bare primitive one arrives
	// here wrapped. assignableInto reads a non-nil target as a structured type
	// and would refuse `str` against `str`, so the wrapper comes off first.
	if want != nil && want.Shape == "" && want.List == nil && want.Shaped == "" && len(want.Literal) == 0 {
		want, wantPrimitive = nil, want.Primitive
	}
	// Everything from here down holds one plain value. propertyResultField
	// types an output as text unless the schema says integer, number or
	// boolean, so an object or an array read as text and passed: the mismatch
	// surfaced only after the business tool had already run.
	if word, _ := property["type"].(string); word == "object" || word == "array" {
		return fmt.Errorf("%s returns %s as %s; the destination holds one plain value, so the tool has to return one",
			tool, field, word)
	}
	// Plain: the same predicate a step's assign: and a pre-fetch are held to, so
	// three checks cannot drift into three answers.
	if err := assignableInto(want, wantPrimitive, propertyResultField(property)); err != nil {
		return fmt.Errorf("%s returns %s as %s: %w", tool, field, describeProperty(property), err)
	}
	return nil
}

// propertyResultField types one output property the way prefetchResultField
// types one pre-fetch field: plain text unless the schema says otherwise, with a
// closed set carried through so a Literal destination can be matched.
func propertyResultField(property map[string]any) ResultField {
	out := ResultField{Type: PrimitiveString}
	if word, ok := property["type"].(string); ok {
		switch PrimitiveType(word) {
		case PrimitiveInteger, PrimitiveNumber, PrimitiveBoolean:
			out.Type = PrimitiveType(word)
		}
	}
	if values, err := stringSlice(property["enum"]); err == nil {
		out.Enum = values
	}
	return out
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
