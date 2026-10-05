package ir

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/slng-ai/unmute/internal/stateschema"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// What SLNG's task contract refuses that the capability table cannot say,
// because each one is a value and not a feature. The contract is
// app/schemas/agent_tasks.py in slng_backend (read at 66caa2a3, 2026-10-02),
// and its own docstring lists what it does not accept yet.

// The contract's counts (agent_tasks.py MAX_TASKS, MAX_TASK_GROUPS,
// MAX_GROUP_STEPS).
const (
	slngMaxTasks      = 32
	slngMaxGroups     = 16
	slngMaxGroupSteps = 16
)

// slngIDPattern is the one constrained text SLNG has a word for, Id. Any other
// pattern on a state field has no SLNG spelling.
const slngIDPattern = `^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`

// slngFormats names each string format SLNG's type grammar spells. date-time
// is missing on purpose: SLNG refuses datetime.
var slngFormats = map[string]string{
	"phone": "Phone", "email": "EmailStr", "name-email": "NameEmail", "date": "Date", "time": "Time",
}

var slngScalars = map[stateschema.Kind]string{
	stateschema.KindString: "str", stateschema.KindInteger: "int",
	stateschema.KindNumber: "float", stateschema.KindBoolean: "bool",
}

// SlngType writes a state type in SLNG's variable type grammar:
// str, int, float, bool, Phone, EmailStr, NameEmail, Date, Time, Id,
// Literal["a", "b"], list[T], a shape name, and "| None".
func SlngType(t *stateschema.Type) (string, error) {
	var name string
	switch {
	case len(t.Enum) > 0:
		quoted := make([]string, len(t.Enum))
		for i, value := range t.Enum {
			quoted[i] = fmt.Sprintf("%q", value)
		}
		name = "Literal[" + strings.Join(quoted, ", ") + "]"
	case t.Kind == stateschema.KindArray:
		item, err := SlngType(t.Items)
		if err != nil {
			return "", err
		}
		name = "list[" + item + "]"
	case t.Kind == stateschema.KindObject:
		if t.Model == "" {
			return "", fmt.Errorf("%s has no fields SLNG can name; declare a BaseModel for it", t)
		}
		// A shape's fields are written in the same grammar, so a field SLNG has
		// no word for refuses the model that holds it.
		for _, field := range t.Fields {
			if _, err := SlngType(field.Type); err != nil {
				return "", fmt.Errorf("%s.%s: %w", t.Model, field.Name, err)
			}
		}
		name = t.Model
	case t.Format != "":
		name = slngFormats[t.Format]
		if name == "" {
			return "", fmt.Errorf("%s has no SLNG type; use str, or one of Phone, EmailStr, NameEmail, date or time", t)
		}
	case t.Pattern != "":
		if t.Pattern != slngIDPattern {
			return "", fmt.Errorf("a pattern other than the Id one has no SLNG type; drop the pattern, or use the Id alias")
		}
		name = "Id"
	default:
		name = slngScalars[t.Kind]
	}
	if t.Nullable {
		name += " | None"
	}
	return name, nil
}

// SlngRuntimeVariables names the variables SLNG holds as runtime variables:
// every value a task saves, and every value the model records itself
// (source: conversation). Every other declared variable is a template
// variable the dispatch fills.
func SlngRuntimeVariables(agent *Agent) map[string]bool {
	out := map[string]bool{}
	for _, task := range agent.Tasks {
		for _, name := range AssignedVars(task.Assign) {
			out[name] = true
		}
	}
	for name, variable := range agent.Variables {
		if variable.Source == VariableSourceConversation {
			out[name] = true
		}
	}
	return out
}

func validateSlngTasks(agent *Agent, row *TargetValidation) {
	fail := func(format string, args ...any) {
		row.Errors = add(row.Errors, targetcap.SlngDiagnostic(format, args...))
	}
	if len(agent.Tasks) > slngMaxTasks {
		fail("has %d tasks, and SLNG holds at most %d on one agent: merge some, or compile to livekit or pipecat", len(agent.Tasks), slngMaxTasks)
	}
	if len(agent.TaskGroups) > slngMaxGroups {
		fail("has %d task groups, and SLNG holds at most %d on one agent: merge some, or compile to livekit or pipecat", len(agent.TaskGroups), slngMaxGroups)
	}
	for _, name := range slices.Sorted(maps.Keys(agent.Tasks)) {
		task := agent.Tasks[name]
		if name == "finish" || name == "go_back_to_step" {
			fail("task %q has a name SLNG keeps for its own tool: rename the task", name)
		}
		if len(task.Announce) > 1 {
			fail("task %q has %d announce lines, and SLNG speaks one: keep one line", name, len(task.Announce))
		}
		for _, assign := range task.Assign {
			if assign.Optional {
				fail("task %q assigns result.%s? and SLNG has no optional marker: drop the ?, or compile to livekit or pipecat", name, assign.Field)
			}
		}
		for _, ref := range task.Tools {
			if _, isControl := agent.Controls[ref]; isControl {
				fail("task %q lists %q, and a SLNG task cannot start another task or hand the call over: move it to the agent's tools", name, ref)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(agent.TaskGroups)) {
		group := agent.TaskGroups[name]
		if name == "finish" || name == "go_back_to_step" {
			fail("task group %q has a name SLNG keeps for its own tool: rename the group", name)
		}
		if group.Then != GroupReturn {
			fail("task group %q ends with then: %s, and SLNG only returns to the agent: write then: return, or compile to livekit or pipecat", name, group.Then)
		}
		if len(group.Steps) > slngMaxGroupSteps {
			fail("task group %q has %d steps, and SLNG runs at most %d: split it", name, len(group.Steps), slngMaxGroupSteps)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(agent.Controls)) {
		if delegate, ok := agent.Controls[name].(*Delegate); ok && len(delegate.Announce) > 1 {
			fail("%q has %d announce lines, and SLNG speaks one: keep one line", name, len(delegate.Announce))
		}
	}

	runtime := SlngRuntimeVariables(agent)
	started := map[string]bool{}
	for _, control := range agent.Controls {
		if delegate, ok := control.(*Delegate); ok && delegate.Task != "" {
			started[delegate.Task] = true
		}
	}
	for _, name := range slices.Sorted(maps.Keys(agent.Variables)) {
		variable := agent.Variables[name]
		if variable.Confirm != "" && !started[variable.Confirm] {
			fail("variable %q is confirmed by %q, and SLNG starts a confirming task from its own when: sentence: list the task on the agent with a when:", name, variable.Confirm)
		}
		if variable.Schema == nil {
			continue
		}
		if !runtime[name] {
			// A dispatch value reaches SLNG as text, so a type on it would be
			// checked by nothing.
			if !variable.Schema.Plain() {
				fail("variable %q is %s and is filled by the dispatch, which SLNG holds as plain text: make it str, or save it with a task's assign:", name, variable.Schema)
			}
			continue
		}
		if variable.Description == "" {
			fail("variable %q has no description, and SLNG needs one for every value a task or the model saves: add description= to its Field in state.py", name)
		}
		if _, err := SlngType(variable.Schema); err != nil {
			fail("variable %q: %v", name, err)
		}
	}
}
