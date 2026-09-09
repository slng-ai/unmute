package spec

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// slngShapeAdvice is the sentence every wrong `slng:` shape gets, and it is one
// sentence on purpose: a scalar, a number, a list, an empty block and a `name:`
// written one level too deep are all the same mistake from the author's side,
// which is not knowing what the one legal line looks like. Showing it is worth
// more than five ways of saying "that is not it".
const slngShapeAdvice = "`slng:` names the tool SLNG already hosts: write `slng: check_order`, " +
	"one line, the hosted tool's exact name"

// UnmarshalYAML accepts the scalar form and the legacy block.
//
// goccy's NodeUnmarshaler, the choice Pair and Field make rather than the
// InterfaceUnmarshaler TaskItem makes, and for Field's reason: a custom
// unmarshaler intercepts the decode, so the yaml.Strict() set once at load.go's
// single call site never sees this node and never refuses an unknown key inside
// it. The block arm below therefore counts and names its own keys, because the
// key that has to be refused is a specific one:
//
//	slng:
//	  name: check_order     # refused, by name
//
// That is the scalar written one level too deep. Accepting it would make three
// forms where the contract has two, and the third would be the only one whose
// hosted name a reader could not see on the `slng:` line itself.
func (s *ToolSlng) UnmarshalYAML(node ast.Node) error {
	line := 0
	if token := node.GetToken(); token != nil {
		line = token.Position.Line
	}
	if name, ok := node.(*ast.StringNode); ok {
		if !validHostedName(name.Value) {
			return &PairError{Line: line, Msg: slngShapeAdvice}
		}
		s.Name = name.Value
		return nil
	}
	var entries []*ast.MappingValueNode
	switch shape := node.(type) {
	case *ast.MappingValueNode:
		entries = []*ast.MappingValueNode{shape}
	case *ast.MappingNode:
		// Zero values is `slng: {}`, which is what a package authored before its
		// first pull carries. It stays legal: ir.Validate is what refuses it on
		// a target that needs the mirror, naming the pull.
		entries = shape.Values
	default:
		return &PairError{Line: line, Msg: slngShapeAdvice}
	}
	for _, entry := range entries {
		key := entry.Key.String()
		keyLine := line
		if token := entry.Key.GetToken(); token != nil {
			keyLine = token.Position.Line
		}
		switch key {
		case "hash":
			text, ok := fieldText(entry.Value)
			if !ok {
				return &PairError{Line: keyLine, Msg: fmt.Sprintf(
					"`slng:` holds a %s under `hash:`, and a pin is one line of text `unmute pull` wrote", entry.Value.Type())}
			}
			s.Hash = text
		case "name":
			return &PairError{Line: keyLine, Msg: slngShapeAdvice}
		default:
			return &PairError{Line: keyLine, Msg: fmt.Sprintf(
				"`slng:` declares unknown key %q. %s, and the block form beside it takes `hash:` alone", key, slngShapeAdvice)}
		}
	}
	return nil
}

// MarshalYAML writes back the form the author wrote, so a console round trip
// does not turn one line into a block or a block into one line.
func (s ToolSlng) MarshalYAML() (any, error) {
	if s.Name != "" {
		return s.Name, nil
	}
	if s.Hash != "" {
		return yaml.MapSlice{{Key: "hash", Value: s.Hash}}, nil
	}
	return map[string]any{}, nil
}

// validHostedName is the whole local check on a hosted name: it is not empty,
// it carries no padding, and it holds no control character. Whether the
// organisation has a tool called this is a question only the account can answer,
// and deployment is where it is asked.
func validHostedName(name string) bool {
	if name == "" || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
