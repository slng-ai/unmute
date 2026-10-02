package ir

import (
	"fmt"
	"slices"
	"strings"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/stateschema"
)

// CallStateMembers are the names the generated CallState class defines beside
// the author's fields. A field by one of these names would be overwritten by
// the method rather than refused by Pydantic, so the compiler refuses it.
// TestCallStateMembersMatchTheTemplate holds this list to the template.
var CallStateMembers = []string{
	"initial_value", "is_confirmed", "is_unconfirmed", "lookup", "plain", "render", "save_batch",
	"save_call_start", "save_fact", "save_result", "slng_session_id", "withdraw_confirmation",
}

// buildState turns the author's State class into the variables every reader of
// the IR has always read, and lays the `variables:` entries over them.
func buildState(pkg *packagespec.Package, out *Agent) error {
	if pkg.Agent.Shapes != nil {
		return fmt.Errorf("%s: shapes: is retired; declare each shape as a BaseModel in %s and use it as a field type on State",
			pkg.Location("agent.yaml", "shapes:"), packagespec.StateFile)
	}
	model := pkg.State
	if pkg.StateSource != nil && model == nil {
		return fmt.Errorf("%s was never read; call Package.ReadState before ir.Build", packagespec.StateFile)
	}
	if model == nil {
		if len(pkg.Agent.Variables) > 0 {
			name := pkg.Agent.Variables[0].Name
			return fmt.Errorf("%s: variables: names %q, and this package has no %s to declare it in. "+
				"Add one beside agent.yaml with a class State(BaseModel) holding the field",
				pkg.Location("agent.yaml", name), name, packagespec.StateFile)
		}
		return nil
	}
	if err := checkStateNames(model.Fields, "State", true); err != nil {
		return fmt.Errorf("%s: %w", packagespec.StateFile, err)
	}
	for _, field := range model.Fields {
		if field.Required {
			return fmt.Errorf("%s: State.%s has no default, and a call starts from State() with nothing filled in. "+
				"Give it one: `| None = None`, `= \"\"`, or `= []` for a list", packagespec.StateFile, field.Name)
		}
		out.Variables[field.Name] = Variable{
			Type: renderedAs(field.Type), Schema: field.Type, Default: defaultOf(field), Description: field.Description,
		}
		out.VariableOrder = append(out.VariableOrder, field.Name)
	}
	seen := map[string]bool{}
	for _, entry := range pkg.Agent.Variables {
		where := pkg.Location("agent.yaml", entry.Name)
		variable, ok := out.Variables[entry.Name]
		switch {
		case !ok:
			return fmt.Errorf("%s: variables: names %q, which State in %s does not declare. It declares %s",
				where, entry.Name, packagespec.StateFile, strings.Join(out.VariableOrder, ", "))
		case seen[entry.Name]:
			return fmt.Errorf("%s: variables: lists %q twice; give it one entry", where, entry.Name)
		case entry.Source == "" && entry.Confirm == "":
			return fmt.Errorf("%s: the variables: entry for %q says nothing. List a field here only to give it "+
				"source: or confirm:, and drop the entry otherwise", where, entry.Name)
		}
		seen[entry.Name] = true
		variable.Source, variable.Confirm = VariableSource(entry.Source), entry.Confirm
		out.Variables[entry.Name] = variable
	}
	out.State, out.StateSource = model, string(pkg.StateSource)
	return nil
}

// checkStateNames holds every field name, at every depth, to the one shape a
// flat placeholder can carry: lowercase words joined by single underscores.
// Two underscores are the emitted path separator, so a name holding them would
// read as a path. A top-level name is also held to the names the generated
// class keeps for itself.
func checkStateNames(fields []stateschema.Field, owner string, top bool) error {
	for _, field := range fields {
		if !namePattern.MatchString(field.Name) {
			return fmt.Errorf("%s.%s is not a name a prompt can carry; use lowercase words joined by single underscores",
				owner, field.Name)
		}
		if top && (stateReservedName(field.Name) || field.Name == "state" || slices.Contains(CallStateMembers, field.Name)) {
			return fmt.Errorf("the field State.%s has a name the generated state class keeps for itself; rename the field", field.Name)
		}
		for typ := field.Type; typ != nil; typ = typ.Items {
			if len(typ.Fields) > 0 {
				name := typ.Model
				if name == "" {
					name = owner + "." + field.Name
				}
				if err := checkStateNames(typ.Fields, name, false); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// renderedAs is the plain kind a value reaches a prompt as: its own scalar
// kind, or text for anything structured.
func renderedAs(typ *stateschema.Type) PrimitiveType {
	switch typ.Kind {
	case stateschema.KindInteger:
		return PrimitiveInteger
	case stateschema.KindNumber:
		return PrimitiveNumber
	case stateschema.KindBoolean:
		return PrimitiveBoolean
	}
	return PrimitiveString
}

// defaultOf is the field's default as plain data, nil for `= None` and for a
// field with no default.
func defaultOf(field stateschema.Field) any {
	if string(field.Default) == "null" {
		return nil
	}
	return field.DefaultValue()
}

// Structured reports whether a variable holds more than one plain scalar: a
// model, a list, a closed set, a checked text type, or a value that may be
// None. It is what decides that a value reaches a tool as JSON and a prompt as
// rendered text rather than as itself.
func (v Variable) Structured() bool {
	return v.Schema != nil && (!v.Schema.Plain() || v.Schema.Nullable)
}
