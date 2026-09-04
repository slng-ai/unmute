package spec

import (
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// VariableOrder is the order the author declared the variables in.
//
// `variables:` is a name-keyed block and stays one: it is on the dictionary
// debt list and nothing about it moves in this feature. But a map has no order,
// and the composed state block numbers its lines in declaration order, because
// sorting is what makes a numbered list move under a reader who adds a field.
//
// So the order is read off the authored file, once, from the same bytes the
// decoder read. Through goccy's parser rather than a line scan: comments,
// quoting and indentation style all vary, and the AST already knows the
// difference between a key and a line that looks like one.
//
// Derived, never authored, which is why it carries the same `json:"-"` tag as
// Tasks and Callables: it is not a field anybody writes.
func (p *Package) VariableOrder() []string { return p.variableOrder }

// readVariableOrder fills variableOrder from agent.yaml's own bytes. A file
// that will not parse here is not reported: the decoder has already read the
// same bytes and its refusal is the better one, so this returns nothing and
// every reader falls back to sorted names.
func (p *Package) readVariableOrder() {
	file, err := parser.ParseBytes(p.files["agent.yaml"], 0)
	if err != nil || len(file.Docs) == 0 {
		return
	}
	root, ok := file.Docs[0].Body.(*ast.MappingNode)
	if !ok {
		return
	}
	for _, entry := range root.Values {
		if entry.Key.String() != "variables" {
			continue
		}
		switch block := entry.Value.(type) {
		case *ast.MappingValueNode:
			p.variableOrder = []string{block.Key.String()}
		case *ast.MappingNode:
			for _, variable := range block.Values {
				p.variableOrder = append(p.variableOrder, variable.Key.String())
			}
		}
		return
	}
}
