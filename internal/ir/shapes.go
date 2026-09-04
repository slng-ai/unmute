package ir

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	packagespec "github.com/slng-ai/unmute/internal/spec"
)

// Shapes and type expressions, resolved.
//
// internal/spec owns the grammar and reports the column of anything malformed
// inside an expression. This file owns the **vocabulary**: which names are
// types, which are refused and what to write instead, and which name a shape
// the package declared. That split is why every name reaches here intact,
// carrying its column, and why one refusal can say both `agent.yaml:112` and
// `column 6`.
//
// Called from Build rather than from Validate, and that is not a filing
// mistake: Build cannot construct a TypeRef for a name it cannot resolve, so a
// refusal here is the alternative to a half-built IR. Validate keeps what is
// genuinely per-target, which is the slng refusal and the capability rows.

// primitiveSpellings is both vocabularies for the four primitives: the JSON
// Schema words every package written before this feature uses, and Python's own
// words, which is what an author reading a type expression expects to write.
// One resolved PrimitiveType either way, so `type: string` and `type: str` are
// the same declaration and every package that compiles today is untouched.
var primitiveSpellings = map[string]PrimitiveType{
	"string":  PrimitiveString,
	"str":     PrimitiveString,
	"integer": PrimitiveInteger,
	"int":     PrimitiveInteger,
	"number":  PrimitiveNumber,
	"float":   PrimitiveNumber,
	"boolean": PrimitiveBoolean,
	"bool":    PrimitiveBoolean,
}

// shapedSpellings is the closed set of text-with-a-shape types.
var shapedSpellings = map[string]ShapedText{
	"Phone": ShapedPhone,
	"Date":  ShapedDate,
	"Time":  ShapedTime,
	"Id":    ShapedID,
}

// refusedTypes is every name a person reaches for that this scope does not
// take, each with the sentence saying what to write instead. Refused by name so
// the message can give the reason, which for the temporal and identifier types
// is the one that matters: they put `format` into the schema the model is sent,
// one target's strict converter keeps it, and the provider rejects it. That
// failure appears on the first real call and in no local check.
var refusedTypes = map[string]string{
	"datetime": `write "Date" and "Time" as two fields, which are text with a shape checked where the ` +
		"value enters the state and never written into the schema the model is sent",
	"date":              `write "Date", which is text with a validated shape`,
	"time":              `write "Time", which is text with a validated shape`,
	"UUID":              `write "Id", which is text with a validated shape`,
	"SecretStr":         "a secret never travels through state: it reaches a tool through that tool's own *_env field",
	"PaymentCardNumber": "card numbers are outside the declared type scope, and nothing in this compiler should carry one",
	"dict":              `declare the fields as a shape under "shapes:" and name that shape here`,
	"Dict":              `declare the fields as a shape under "shapes:" and name that shape here`,
	"set":               `write "list[...]", which is the one collection this scope takes`,
	"tuple":             `write "list[...]", which is the one collection this scope takes`,
	"List":              `write "list[...]", in lower case, which is Python's own spelling now`,
	"Optional":          `write "T | None"`,
	"Union":             `write "A | B", and only "| None" is meaningful in this scope`,
	"Any":               "name the type: a value the compiler cannot describe is a value no prompt can render and no guard can test",
	"BaseModel":         `name one of the shapes declared under "shapes:"`,
}

// buildShapes resolves the authored `shapes:` list into the resolved catalog.
//
// Two passes, because a shape may name another shape declared below it: the
// first collects the names, the second resolves the fields against them. The
// cycle check runs last, over the resolved references.
func buildShapes(pkg *packagespec.Package) (map[string]Shape, error) {
	declared := make(map[string]bool, len(pkg.Agent.Shapes))
	for _, shape := range pkg.Agent.Shapes {
		where := pkg.Location("agent.yaml", "name: "+shape.Name)
		if strings.TrimSpace(shape.Name) == "" {
			return nil, fmt.Errorf("%s: a shape with no name. Every shape carries a name:, "+
				"which is what a type: refers to", pkg.Location("agent.yaml", "shapes:"))
		}
		if declared[shape.Name] {
			return nil, fmt.Errorf("%s: shape %q is declared twice. One shape, one name: merge the two "+
				"or give the second its own name", where, shape.Name)
		}
		if reason := reservedShapeName(shape.Name); reason != "" {
			return nil, fmt.Errorf("%s: shape %q cannot be called that, because %s. Give it a name of its "+
				"own, written in CapWords", where, shape.Name, reason)
		}
		if len(shape.Fields) == 0 {
			return nil, fmt.Errorf("%s: shape %q declares no fields. A shape is a group of fields, and one "+
				"with none tells the model nothing", where, shape.Name)
		}
		declared[shape.Name] = true
	}
	out := make(map[string]Shape, len(pkg.Agent.Shapes))
	for _, shape := range pkg.Agent.Shapes {
		where := pkg.Location("agent.yaml", "name: "+shape.Name)
		resolved := Shape{
			Name:        shape.Name,
			Description: strings.TrimSpace(shape.Description),
			Fields:      make([]Field, 0, len(shape.Fields)),
		}
		seen := make(map[string]bool, len(shape.Fields))
		for _, field := range shape.Fields {
			if seen[field.Name] {
				return nil, fmt.Errorf("%s: shape %q declares field %q twice. One field, one name: the second "+
					"would silently replace the first in the generated class", where, shape.Name, field.Name)
			}
			seen[field.Name] = true
			ref, err := resolveType(field.Type, declared)
			if err != nil {
				return nil, fmt.Errorf("%s: shape %q field %q: %w",
					locateType(pkg, field.Type, "name: "+shape.Name), shape.Name, field.Name, err)
			}
			resolved.Fields = append(resolved.Fields, Field{
				Name: field.Name, Type: ref, Description: strings.TrimSpace(field.Description),
			})
		}
		out[shape.Name] = resolved
	}
	if err := checkShapeCycles(pkg, out); err != nil {
		return nil, err
	}
	return out, nil
}

// reservedShapeName says why a name is unavailable, or "" when it is free.
func reservedShapeName(name string) string {
	if _, ok := primitiveSpellings[name]; ok {
		return "it is one of the primitive types"
	}
	if _, ok := shapedSpellings[name]; ok {
		return "it is one of the text types with a validated shape"
	}
	if _, ok := refusedTypes[name]; ok {
		return "a type expression cannot use that name"
	}
	switch name {
	case "Literal", "list", "None":
		return "it is part of the type grammar"
	}
	return ""
}

// resolveType turns one authored type expression into a resolved TypeRef.
//
// The returned ref is never nil for a legal expression, including a bare
// primitive: whether to keep it is the caller's decision, and a variable
// declaring a bare primitive keeps nil so a package with nothing structured in
// it resolves byte-identically (FR-015).
func resolveType(expr string, declared map[string]bool) (*TypeRef, error) {
	parsed, err := packagespec.ParseType(expr)
	if err != nil {
		return nil, err
	}
	return resolveParsed(parsed, declared)
}

// resolveParsed is the same resolution over an expression that is already
// parsed, which is how a nested argument keeps its column: re-parsing the
// argument's own text would start counting from one again, and the column is
// the whole reason a refusal about a nested type is useful.
func resolveParsed(parsed packagespec.TypeExpr, declared map[string]bool) (*TypeRef, error) {
	var real []packagespec.TypeAtom
	optional := false
	for _, atom := range parsed.Union {
		if !atom.Quoted && atom.Name == "None" {
			if atom.Bracket {
				return nil, &packagespec.TypeError{Col: atom.Col, Msg: `"None" takes no brackets`}
			}
			if optional {
				return nil, &packagespec.TypeError{Col: atom.Col, Msg: `"None" written twice. ` +
					`One "| None" is what says a value may be absent`}
			}
			optional = true
			continue
		}
		real = append(real, atom)
	}
	switch len(real) {
	case 1:
	case 0:
		return nil, &packagespec.TypeError{Col: firstAtomCol(parsed), Msg: `"None" and nothing else. A value that ` +
			"can only be absent is not a value: name the type it holds when it is present"}
	default:
		names := make([]string, 0, len(real))
		for _, atom := range real {
			names = append(names, atom.String())
		}
		return nil, &packagespec.TypeError{Col: real[1].Col, Msg: fmt.Sprintf(
			"a union of %s. Only %q is meaningful in this scope: a value holding either of two real types "+
				"cannot be rendered into a prompt or tested by a guard",
			strings.Join(names, " and "), "| None")}
	}
	ref, err := resolveAtom(real[0], declared)
	if err != nil {
		return nil, err
	}
	if optional && ref.List != nil {
		return nil, &packagespec.TypeError{Col: real[0].Col, Msg: fmt.Sprintf(
			"a list that may be absent. A declared list starts empty, so it is never absent: drop the %q, and "+
				"an empty list is how the state says nothing has been recorded", "| None")}
	}
	ref.Optional = optional
	return ref, nil
}

func resolveAtom(atom packagespec.TypeAtom, declared map[string]bool) (*TypeRef, error) {
	if atom.Quoted {
		return nil, &packagespec.TypeError{Col: atom.Col, Msg: fmt.Sprintf(
			"the entry %q where a type belongs. A quoted entry belongs inside %s",
			atom.Entry, `Literal["a", "b"]`)}
	}
	switch atom.Name {
	case "Literal":
		if !atom.Bracket || len(atom.Args) == 0 {
			return nil, &packagespec.TypeError{Col: atom.Col, Msg: fmt.Sprintf(
				"%s with no entries. A Literal is a closed set, so name at least one: %s",
				"Literal", `Literal["yes", "no"]`)}
		}
		entries := make([]string, 0, len(atom.Args))
		for _, arg := range atom.Args {
			if len(arg.Union) != 1 || !arg.Union[0].Quoted {
				return nil, &packagespec.TypeError{Col: firstCol(arg, atom.Col), Msg: fmt.Sprintf(
					"%s is not a quoted entry. Every entry of a Literal is text in double quotes",
					arg.String())}
			}
			entry := arg.Union[0].Entry
			if slices.Contains(entries, entry) {
				return nil, &packagespec.TypeError{Col: arg.Union[0].Col, Msg: fmt.Sprintf(
					"the entry %q twice. A Literal is a set, so a repeat says nothing the first did not", entry)}
			}
			entries = append(entries, entry)
		}
		return &TypeRef{Literal: entries}, nil
	case "list":
		if !atom.Bracket || len(atom.Args) == 0 {
			return nil, &packagespec.TypeError{Col: atom.Col, Msg: fmt.Sprintf(
				"a list with no element type. Name what it holds: %s or %s",
				"list[Appointment]", `list[Literal["yes", "no"]]`)}
		}
		if len(atom.Args) > 1 {
			return nil, &packagespec.TypeError{Col: firstCol(atom.Args[1], atom.Col), Msg: fmt.Sprintf(
				"a list of %d types. A list holds one type: %s", len(atom.Args), "list[Appointment]")}
		}
		element, err := resolveParsed(atom.Args[0], declared)
		if err != nil {
			return nil, err
		}
		if element.List != nil {
			return nil, &packagespec.TypeError{Col: atom.Col, Msg: "a list of lists. Nothing renders that into a " +
				"prompt a step can read: declare a shape holding the inner list and make it a list of that"}
		}
		return &TypeRef{List: element}, nil
	case "None":
		// Reached only from inside brackets; the union above handles the rest.
		return nil, &packagespec.TypeError{Col: atom.Col, Msg: `"None" on its own inside a type. ` +
			`Write it beside a type, as "T | None"`}
	}
	// The refused names come first, brackets or not: `dict[str, str]` is a
	// dictionary whichever way it is written, and "takes no brackets" would be
	// true and useless.
	if instead, ok := refusedTypes[atom.Name]; ok {
		return nil, &packagespec.TypeError{Col: atom.Col, Msg: fmt.Sprintf(
			"%q is not a type this compiler takes: %s", atom.Name, instead)}
	}
	if atom.Bracket {
		return nil, &packagespec.TypeError{Col: atom.Col, Msg: fmt.Sprintf(
			"%q takes no brackets. Only %q and %q do", atom.Name, "list", "Literal")}
	}
	if primitive, ok := primitiveSpellings[atom.Name]; ok {
		return &TypeRef{Primitive: primitive}, nil
	}
	if shaped, ok := shapedSpellings[atom.Name]; ok {
		return &TypeRef{Shaped: shaped}, nil
	}
	if declared[atom.Name] {
		return &TypeRef{Shape: atom.Name}, nil
	}
	return nil, &packagespec.TypeError{Col: atom.Col, Msg: fmt.Sprintf(
		"no shape named %q is declared. Declare it under %q, or write one of %s",
		atom.Name, "shapes:", strings.Join(scopeNames(declared), ", "))}
}

// firstAtomCol is where an expression starts, for a refusal about the whole
// expression rather than about one token in it.
func firstAtomCol(parsed packagespec.TypeExpr) int {
	if len(parsed.Union) == 0 {
		return 1
	}
	return parsed.Union[0].Col
}

// firstCol is the column an argument starts at, for a refusal about the
// argument rather than about a token inside it.
func firstCol(arg packagespec.TypeExpr, fallback int) int {
	if len(arg.Union) == 0 {
		return fallback
	}
	return arg.Union[0].Col
}

// scopeNames is the whole vocabulary a refusal offers, in a fixed order so the
// message is the same every run.
func scopeNames(declared map[string]bool) []string {
	names := []string{"str", "int", "float", "bool", "Phone", "Date", "Time", "Id", "Literal[...]", "list[...]"}
	names = append(names, slices.Sorted(maps.Keys(declared))...)
	return names
}

// checkShapeCycles refuses a shape that refers to itself, directly or through
// another. A self-referential shape generates a Pydantic class that cannot be
// constructed, and the model would be asked for an infinitely nested object.
func checkShapeCycles(pkg *packagespec.Package, shapes map[string]Shape) error {
	state := make(map[string]int, len(shapes)) // 0 unseen, 1 on the stack, 2 done
	var stack []string
	var walk func(name string) error
	walk = func(name string) error {
		switch state[name] {
		case 1:
			at := slices.Index(stack, name)
			chain := append(slices.Clone(stack[at:]), name)
			return fmt.Errorf("%s: shape %q refers to itself, through %s. A shape that contains itself "+
				"generates a class that cannot be built, and the model would be asked for an object with no "+
				"bottom: break the chain, or hold the nested part as a list on the outer shape",
				pkg.Location("agent.yaml", "name: "+name), name, strings.Join(chain, " -> "))
		case 2:
			return nil
		}
		state[name] = 1
		stack = append(stack, name)
		for _, field := range shapes[name].Fields {
			for _, referenced := range referencedShapes(field.Type) {
				if _, ok := shapes[referenced]; !ok {
					continue
				}
				if err := walk(referenced); err != nil {
					return err
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = 2
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(shapes)) {
		if err := walk(name); err != nil {
			return err
		}
	}
	return nil
}

// referencedShapes names every shape a resolved type reaches, through a list or
// through nothing.
func referencedShapes(ref *TypeRef) []string {
	if ref == nil {
		return nil
	}
	if ref.Shape != "" {
		return []string{ref.Shape}
	}
	return referencedShapes(ref.List)
}

// String writes a resolved type back the way it was authored, which is what a
// refusal naming a type prints.
func (t *TypeRef) String() string {
	if t == nil {
		return ""
	}
	var text string
	switch {
	case t.Shape != "":
		text = t.Shape
	case t.Shaped != "":
		text = string(t.Shaped)
	case len(t.Literal) > 0:
		entries := make([]string, 0, len(t.Literal))
		for _, entry := range t.Literal {
			entries = append(entries, `"`+entry+`"`)
		}
		text = "Literal[" + strings.Join(entries, ", ") + "]"
	case t.List != nil:
		text = "list[" + t.List.String() + "]"
	default:
		text = pythonSpelling(t.Primitive)
	}
	if t.Optional {
		text += " | None"
	}
	return text
}

// pythonSpelling is a primitive in the words an author writes, which is what a
// message about a type expression has to use.
func pythonSpelling(primitive PrimitiveType) string {
	switch primitive {
	case PrimitiveInteger:
		return "int"
	case PrimitiveNumber:
		return "float"
	case PrimitiveBoolean:
		return "bool"
	default:
		return "str"
	}
}

// IsList reports whether the resolved type is a list, which is what an append
// assignment requires and what a `requires:` guard has to test for emptiness
// rather than for truthiness.
func (t *TypeRef) IsList() bool { return t != nil && t.List != nil }

// Structured reports whether a type is more than a bare primitive, which is
// what decides whether a variable carries a Shape at all.
func (t *TypeRef) Structured() bool {
	if t == nil {
		return false
	}
	return t.Shape != "" || t.Shaped != "" || len(t.Literal) > 0 || t.List != nil || t.Optional
}

// Equal reports whether two resolved types are the same declaration, which is
// what an assignment from a step's result has to satisfy.
func (t *TypeRef) Equal(other *TypeRef) bool {
	if t == nil || other == nil {
		return t == nil && other == nil
	}
	if t.Primitive != other.Primitive || t.Shaped != other.Shaped ||
		t.Shape != other.Shape || t.Optional != other.Optional {
		return false
	}
	if !slices.Equal(t.Literal, other.Literal) {
		return false
	}
	return t.List.Equal(other.List)
}

// FieldPath resolves a dotted path into a shape, returning the field's type.
// One field deep or many: `customer.customer_name` and a longer path both walk
// the same way, and a path through a list is refused, because a guard cannot
// say which entry it meant.
func FieldPath(shapes map[string]Shape, root *TypeRef, path []string) (*TypeRef, error) {
	at := root
	walked := make([]string, 0, len(path))
	for _, segment := range path {
		if at.IsList() {
			return nil, fmt.Errorf("%q is a list, and a path cannot name a field inside one: "+
				"nothing says which entry it means", strings.Join(walked, "."))
		}
		shape, ok := shapes[at.Shape]
		if !ok {
			return nil, fmt.Errorf("%q is %s, which has no fields to name", strings.Join(walked, "."), at.String())
		}
		index := slices.IndexFunc(shape.Fields, func(field Field) bool { return field.Name == segment })
		if index < 0 {
			names := make([]string, 0, len(shape.Fields))
			for _, field := range shape.Fields {
				names = append(names, field.Name)
			}
			return nil, fmt.Errorf("shape %q declares no field %q. It declares %s",
				shape.Name, segment, strings.Join(names, ", "))
		}
		at = shape.Fields[index].Type
		walked = append(walked, segment)
	}
	return at, nil
}

// locateType is the line a `type:` sits on, which is the line the author has to
// open. The type text is searched for first, because a refusal carrying a
// column inside an expression has to name the line that expression is on; the
// declaration's own name is the fallback, for a type written in a form the
// search cannot match, such as a quoted one.
func locateType(pkg *packagespec.Package, expr, fallback string) string {
	if at := pkg.Location("agent.yaml", "type: "+expr); at != "agent.yaml" {
		return at
	}
	if at := pkg.Location("agent.yaml", ": "+expr); at != "agent.yaml" {
		return at
	}
	return pkg.Location("agent.yaml", fallback)
}

// reaches reports whether a resolved type, or any type inside it, satisfies
// pick. One walk for every per-target question about a declared type, so the
// two capability rows cannot disagree about what "structured" means.
func reaches(ref *TypeRef, pick func(*TypeRef) bool) bool {
	if ref == nil {
		return false
	}
	return pick(ref) || reaches(ref.List, pick)
}
