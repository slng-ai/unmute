package spec

import (
	"fmt"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// ToolEntry is one item of a `tools:` list in agent.yaml: a bare tool name, or
// a block saying what happens around the tool. The tool file says what the
// tool is; this says what the caller hears and what the call remembers.
//
//	tools:
//	  - look_up_salon_info
//	  - name: book_appointment
//	    confirm:
//	      - Shall I book your {{input.service}} for {{input.date}}?
//	    outcomes:
//	      - if: [status: booked]
//	        save: [appointment: result.appointment]
//	        then: done
type ToolEntry struct {
	Name string `json:"name" yaml:"name"`
	// Save writes State on every result of the tool, whatever its outcome.
	// Package scope only: it is the tool's own save, the same on every agent.
	Save []Pair `json:"save,omitempty" yaml:"save,omitempty" since:"2"`
	// Confirm is read back by code before the tool runs, and the tool runs on
	// the caller's yes. A step's tool only, beside an outcome that ends it.
	Confirm []string `json:"confirm,omitempty" yaml:"confirm,omitempty" since:"2"`
	// Outcomes say what each result does, tried top to bottom.
	Outcomes []Outcome `json:"outcomes,omitempty" yaml:"outcomes,omitempty"`
	// Start is what is said as the tool starts. Package scope only.
	Start *ToolStart `json:"start,omitempty" yaml:"start,omitempty" since:"2"`
	// Hidden names result fields the model never reads. Package scope only.
	Hidden []string `json:"hidden,omitempty" yaml:"hidden,omitempty" since:"2"`
}

// Bare reports an entry that names a tool and says nothing else about it.
func (t ToolEntry) Bare() bool {
	return len(t.Save) == 0 && len(t.Confirm) == 0 && len(t.Outcomes) == 0 && t.Start == nil && len(t.Hidden) == 0
}

// UnmarshalYAML accepts a bare name or a block, the way TaskItem does.
func (t *ToolEntry) UnmarshalYAML(unmarshal func(any) error) error {
	var name string
	if err := unmarshal(&name); err == nil {
		t.Name = name
		return nil
	}
	type plainEntry ToolEntry
	return unmarshal((*plainEntry)(t))
}

// MarshalYAML writes a bare entry back as the bare name, so a console
// round-trip does not reshape the author's file.
func (t ToolEntry) MarshalYAML() (any, error) {
	if t.Bare() {
		return t.Name, nil
	}
	type plainEntry ToolEntry
	return plainEntry(t), nil
}

// ToolNames are the names of a tools: list, in written order.
func ToolNames(entries []ToolEntry) []string {
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name
	}
	return names
}

// Outcome is one item of a tool's `outcomes:`. It starts with `if:`, a typed
// match on the result, or `else:`, which catches every result no `if:` above it
// matched. Then it may save State, say lines, and end the step.
type Outcome struct {
	If []SuccessPair `json:"if,omitempty" yaml:"if,omitempty"`
	// ElseKey holds the written `else:` key, which has no value. Else is what
	// the decoder found: whether the key is there.
	ElseKey any  `json:"else,omitempty" yaml:"else,omitempty"`
	Else    bool `json:"-" yaml:"-"`
	// Save writes State, as `- appointment: result.appointment`.
	Save []Pair   `json:"save,omitempty" yaml:"save,omitempty"`
	Say  []string `json:"say,omitempty" yaml:"say,omitempty"`
	// Then ends the step: `done` completes it, `failed` fails it. Omitted, the
	// step goes on and the model decides.
	Then string `json:"then,omitempty" yaml:"then,omitempty"`
}

// UnmarshalYAML records whether `else:` was written. Its value is empty, so the
// decoder alone cannot tell a written key from a missing one.
func (o *Outcome) UnmarshalYAML(node ast.Node) error {
	mapping, ok := node.(*ast.MappingNode)
	if !ok {
		if single, isPair := node.(*ast.MappingValueNode); isPair {
			mapping = &ast.MappingNode{Values: []*ast.MappingValueNode{single}}
		} else {
			line := 0
			if token := node.GetToken(); token != nil {
				line = token.Position.Line
			}
			return &PairError{Line: line, Msg: "an outcome is a block that starts with if: or else:"}
		}
	}
	for _, entry := range mapping.Values {
		if entry.Key.String() != "else" {
			continue
		}
		if _, empty := entry.Value.(*ast.NullNode); !empty {
			return &PairError{Line: entry.Key.GetToken().Position.Line,
				Msg: "else: takes no value. Write else: on its own line, then save:, say: or then: under it"}
		}
		o.Else = true
	}
	type plainOutcome Outcome
	if err := yaml.NodeToValue(node, (*plainOutcome)(o), yaml.Strict()); err != nil {
		return err
	}
	o.ElseKey = nil
	return nil
}

// MarshalYAML writes the outcome back the way an author writes it: if: or a
// bare else:, then save:, say: and then:, each only when set.
func (o Outcome) MarshalYAML() (any, error) {
	out := yaml.MapSlice{}
	if o.Else {
		out = append(out, yaml.MapItem{Key: "else", Value: nil})
	} else {
		out = append(out, yaml.MapItem{Key: "if", Value: o.If})
	}
	if len(o.Save) > 0 {
		out = append(out, yaml.MapItem{Key: "save", Value: o.Save})
	}
	if len(o.Say) > 0 {
		out = append(out, yaml.MapItem{Key: "say", Value: o.Say})
	}
	if o.Then != "" {
		out = append(out, yaml.MapItem{Key: "then", Value: o.Then})
	}
	return out, nil
}

// TaskStart is a step's `start:`: the line said as it opens, whether it then
// waits for the caller, and a tool code runs first.
type TaskStart struct {
	Say    []string `json:"say,omitempty" yaml:"say,omitempty"`
	Listen bool     `json:"listen,omitempty" yaml:"listen,omitempty"`
	Call   string   `json:"call,omitempty" yaml:"call,omitempty"`
	// With passes State to the call, as `- date: state.requested_day?`. A `?`
	// marks a value the call runs without.
	With []Pair `json:"with,omitempty" yaml:"with,omitempty"`
}

// HandoffStart is a handoff's `start:`: a tool of the next agent, run in code
// as it takes the call.
type HandoffStart struct {
	Call string `json:"call" yaml:"call"`
	With []Pair `json:"with,omitempty" yaml:"with,omitempty"`
}

// ToolStart is a package tool entry's `start:`: the line said as the tool
// starts, so a slow call is not silence.
type ToolStart struct {
	Say []string `json:"say,omitempty" yaml:"say,omitempty"`
	// Wait runs the tool once the line has played, so a goodbye is heard in
	// full before a tool that ends the call runs.
	Wait bool `json:"wait,omitempty" yaml:"wait,omitempty"`
}

// removedKeys refuses keys that moved, each with the sentence naming what to
// write instead, so an author upgrading a file reads the fix and not "unknown
// field".
func removedKeys(node ast.Node, moved map[string]string) error {
	mapping, ok := node.(*ast.MappingNode)
	if !ok {
		return nil
	}
	for _, entry := range mapping.Values {
		if msg, gone := moved[entry.Key.String()]; gone {
			return &PairError{Line: entry.Key.GetToken().Position.Line, Msg: msg}
		}
	}
	return nil
}

// stepToolKeys moved from a step to its tool entries and its start: block.
var stepToolKeys = map[string]string{
	"finish":     "finish: moved under the step's tools:. Write the tool as - name: <tool>, then outcomes: with an if: and then: done",
	"replies":    "replies: moved under the step's tools:. Write the tool as - name: <tool>, then outcomes: with an if: and say:",
	"assign":     "assign: is called save: now. A save that only one tool result makes goes on that outcome, as save: under it",
	"opening":    "opening: listen moved into start:. Write start: with listen: true",
	"announce":   "announce: moved into start:. Write start: with say:",
	"opens_with": "opens_with: moved into start:. Write start: with call: <tool> and with:",
}

// handoffToolKeys moved from a handoff to its start: block.
var handoffToolKeys = map[string]string{
	"opens_with": "opens_with: moved into start:. Write start: with call: <tool> and with:",
}

// toolBehaviourMoved explains a tool-file key that moved to agent.yaml.
func toolBehaviourMoved(key, where string) string {
	return fmt.Sprintf("%s moved to agent.yaml: write the tool under tools: as - name: <tool>, then %s", key, where)
}
