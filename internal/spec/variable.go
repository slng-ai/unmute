package spec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// Variable is one entry under `variables:`. A state value's type, default and
// description live on State in state.py; this entry says only how the value
// behaves, which no Python type can:
//
//	variables:
//	  - name: caller_phone
//	    confirm: verify_contact
//
// A field needs an entry only when it carries `source:` or `confirm:`.
type Variable struct {
	// Name is the State field this entry is about.
	Name   string `json:"name" yaml:"name"`
	Source string `json:"source,omitempty" yaml:"source,omitempty"`
	// Confirm names the step that must hear the caller agree before anything acts
	// on this value. Until then the value stays marked unconfirmed and renders
	// only in that step's own prompt. Empty means the value is settled the moment
	// it arrives.
	Confirm string `json:"confirm,omitempty" yaml:"confirm,omitempty"`
}

// Variables is the `variables:` list.
type Variables []Variable

// movedToState are the keys a variable entry carried before its type lived in
// state.py. Each is refused with the place it moved to, rather than with a
// strict decoder's "unknown field".
var movedToState = []string{"type", "default", "description"}

// UnmarshalYAML decodes the list, and refuses the name-keyed form every package
// wrote before state.py with the sentence that says what to do instead.
func (v *Variables) UnmarshalYAML(node ast.Node) error {
	line := 0
	if token := node.GetToken(); token != nil {
		line = token.Position.Line
	}
	sequence, ok := node.(*ast.SequenceNode)
	if !ok {
		return &PairError{Line: line, Msg: "variables: is a list now. Types, defaults and descriptions live on " +
			"State in state.py; list here only the values that need source: or confirm:, as `- name: caller_phone`"}
	}
	for _, item := range sequence.Values {
		itemLine := line
		if token := item.GetToken(); token != nil {
			itemLine = token.Position.Line
		}
		if mapping, ok := item.(*ast.MappingNode); ok {
			for _, entry := range mapping.Values {
				if key := entry.Key.String(); slices.Contains(movedToState, key) {
					return &PairError{Line: itemLine, Msg: fmt.Sprintf(
						"a variables: entry has no %s: any more; declare it on the field in state.py", key)}
				}
			}
		}
		var variable Variable
		if err := yaml.NodeToValue(item, &variable, yaml.Strict()); err != nil {
			return &PairError{Line: itemLine, Msg: strings.TrimSpace(err.Error())}
		}
		*v = append(*v, variable)
	}
	return nil
}

// Named returns the entry for one State field, if there is one.
func (v Variables) Named(name string) (Variable, bool) {
	for _, variable := range v {
		if variable.Name == name {
			return variable, true
		}
	}
	return Variable{}, false
}
