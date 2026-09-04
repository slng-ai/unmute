package generate

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/slng-ai/unmute/internal/ir"
)

// The declared shapes, and the one place their emitted Python is written.
//
// Following guard.go and prefetch.go: one file owns the text, both drivers
// render what it produces, and the two targets agree by construction rather
// than by a test that has to notice they stopped agreeing. The test still
// exists, because construction can be undone.
//
// Everything here is rendered once and inserted into both modules verbatim, so
// "the same on both targets" (FR-006) is a property of this file rather than of
// two templates kept in step by hand.

// shapedPattern is the check each text type carries, and the phrase that says
// what it wants. The pattern lives in an AfterValidator in the emitted code and
// never in the annotation: a `pattern=` constraint reaches the schema the model
// is sent, one target's strict converter strips neither `format` nor `pattern`,
// strict is that target's default, and the provider rejects both.
//
// One phrase, used twice: a refusal prints "expected " and it, and the emitted
// alias carries it as its description. Two strings would be two things to keep
// in step, and the day they drift the model is told one format and refused
// against another.
var shapedPatterns = map[ir.ShapedText]struct {
	regex  string
	phrase string
}{
	ir.ShapedPhone: {
		`^\+[1-9]\d{6,14}$`,
		"a phone number in E.164, one leading plus and 7 to 15 digits, like +34600111222",
	},
	ir.ShapedDate: {
		`^\d{4}-\d{2}-\d{2}$`,
		"a day written year-month-day, like 2026-03-19",
	},
	ir.ShapedTime: {
		`^([01]\d|2[0-3]):[0-5]\d$`,
		"a time of day on the 24-hour clock, like 09:30 or 17:45",
	},
	ir.ShapedID: {
		`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`,
		"an identifier: letters, digits, and then any of dot, dash, underscore or colon",
	},
}

// ShapedPhrase is what a text type wants, as the emitted description says it.
// Exported for the gate that reads it out of the emitted module: the sentence
// the model is shown and the sentence a refusal prints have to be the same one.
func ShapedPhrase(kind ir.ShapedText) string { return shapedPatterns[kind].phrase }

// pyRaw writes a pattern as a Python raw string. Not pyQuote: that escapes
// every backslash, and a regex is mostly backslashes, so the emitted pattern
// would match a literal backslash instead of a digit. Every pattern above is
// checked to hold no quote and to end in no backslash, which is what makes the
// raw form safe.
func pyRaw(pattern string) string { return `r"` + pattern + `"` }

// RawStringSafe reports whether a pattern can be written as a Python raw
// string: no quote to close it early, and no trailing backslash to escape the
// closing quote. Exported because the gate over every pattern in this file
// belongs in a test rather than in a panic, and because the failure it catches
// is a pattern that compiles and then matches the wrong thing.
func RawStringSafe(pattern string) bool {
	return !strings.Contains(pattern, `"`) && !strings.HasSuffix(pattern, `\`)
}

// ShapedPatterns is every text type's check, for that gate.
func ShapedPatterns() map[ir.ShapedText]string {
	out := make(map[ir.ShapedText]string, len(shapedPatterns))
	for kind, row := range shapedPatterns {
		out[kind] = row.regex
	}
	return out
}

// shapedOrder fixes the order the aliases are emitted in, so the same package
// renders the same bytes twice.
var shapedOrder = []ir.ShapedText{ir.ShapedPhone, ir.ShapedDate, ir.ShapedTime, ir.ShapedID}

// TypedStateBlock is what a driver renders: the Python, plus what each
// template's import block needs to know without re-deriving it.
type TypedStateBlock struct {
	Source string
	// Structured names every declared value the state block renders as JSON and
	// as words when empty, sorted. Emitted as a set the render path reads, so a
	// package declaring nothing structured gets an empty one and every existing
	// prompt renders exactly as it did.
	Structured []string
	// Values is every declared value the block renders, in the order the block
	// numbers them, with the type its author wrote. The runbook prints it, which
	// is the one place a reader finds out what the model is being told.
	Values []TypedStateValue
	// Preview is the block as it stands in the entry agent's own prompt, so the
	// runbook shows the real thing rather than a description of it. Composed by
	// the one composer every prompt goes through, which is why it cannot drift
	// from what the module actually carries.
	Preview string
	// Empty is what a value with no contents renders as, so the runbook quotes
	// the string rather than paraphrasing it.
	Empty          string
	NeedsRe        bool
	NeedsJSON      bool
	NeedsAnnotated bool
	NeedsLiteral   bool
}

// TypedStateValue is one declared value as the runbook names it.
type TypedStateValue struct {
	Name string
	Type string
	// Confirm is the step that must hear the caller agree before anything acts
	// on this value, empty when it is settled on arrival.
	Confirm string
}

// TypedState renders the declared shapes, their validators and the finish-time
// type table. The second return is false for a package that declares nothing
// structured, and that package's module emits none of this.
func TypedState(agent *ir.Agent) (TypedStateBlock, error) {
	var block TypedStateBlock
	for _, name := range agent.VariableOrder {
		variable := agent.Variables[name]
		if variable.Shape == nil {
			continue
		}
		block.Structured = append(block.Structured, name)
		block.Values = append(block.Values, TypedStateValue{
			Name: name, Type: variable.Shape.String(), Confirm: variable.Confirm,
		})
	}
	// The set the render path reads is sorted, because it is a membership test.
	// Values keeps the declaration order, because that is the order a reader of
	// the block sees.
	slices.Sort(block.Structured)
	classes, err := shapeOrder(agent)
	if err != nil {
		return TypedStateBlock{}, err
	}
	finish := finishTypes(agent)
	used := usedShapedText(agent)
	if len(classes) == 0 && len(block.Values) == 0 && len(finish) == 0 {
		return TypedStateBlock{}, nil
	}
	block.Preview = agent.StateBlock(ir.AgentPromptSite(agent.EntryAgent))
	block.Empty = ir.StateEmptyText()
	block.NeedsRe = len(used) > 0
	block.NeedsJSON = true
	block.NeedsLiteral = declaresLiteral(agent)
	// A shaped text type is itself an Annotated alias, so using one needs the
	// import even in a package that declares no literal and no description. It
	// was missing, and the package that would have found it does not exist yet:
	// every shipped one declaring a shaped type also declares one of the other
	// two, so the import arrived for another reason.
	block.NeedsAnnotated = block.NeedsRe || block.NeedsLiteral || fieldCarriesDescription(agent, classes)

	var b strings.Builder
	b.WriteString(`# --- declared state ----------------------------------------------------------
# Generated from the ` + "`shapes:`" + ` and the typed ` + "`variables:`" + ` in agent.yaml. Both
# target frameworks already depend on Pydantic, so nothing here adds one.
#
# Emitted from one place in the compiler for both targets, so the classes, the
# checks and the refusal wording cannot differ between them.
`)
	for _, kind := range shapedOrder {
		if !used[kind] {
			continue
		}
		row := shapedPatterns[kind]
		lower := strings.ToLower(string(kind))
		fmt.Fprintf(&b, `
_SHAPE_%s = re.compile(%s)


def _shape_%s(value: str) -> str:
    # Empty is not a wrong value, it is no value yet. It is what a declared
    # variable holds before anything fills it, what the state block renders as
    # words, and what a tool hands back for a field it could not fill. Refusing
    # it here deadlocked a live call on both targets: the model had nothing else
    # to send, so every retry was refused the same way and the step never
    # finished. A wrong value is still refused; an absent one is not wrong.
    if value and not _SHAPE_%s.match(value):
        raise ValueError(%s)
    return value


# AfterValidator and never a pattern= constraint: a pattern reaches the schema
# the model is sent, one target's strict converter keeps it, and the provider
# rejects it. So the schema says str and the shape is checked here.
#
# The description is how the format reaches the model at all, and it is the only
# keyword that can: it travels as prose, so no strict converter strips it. A
# field that said nothing about its shape was learned from a refusal mid-call,
# which cost a model round trip on every value the prompt spells one way and
# this type another. A field carrying its own description keeps that one; the
# emitter appends this phrase to it.
%s = Annotated[
    str,
    AfterValidator(_shape_%s),
    Field(description=%s),
]
`,
			strings.ToUpper(lower), pyRaw(row.regex), lower, strings.ToUpper(lower),
			pyQuote("expected "+row.phrase), string(kind), lower, pyQuote(row.phrase))
	}
	for _, class := range classes {
		b.WriteString("\n\nclass " + class.Name + "(BaseModel):\n")
		if class.Description != "" {
			b.WriteString("    " + pyTriple(class.Description) + "\n\n")
		}
		for _, field := range class.Fields {
			b.WriteString("    " + field.Name + ": " + pyFieldAnno(field) + "\n")
		}
	}
	b.WriteString(`

class _StateRefused(Exception):
    """A value that does not fit its declared type, refused where it enters.

    Carried as an exception rather than a return so the write cannot happen by
    accident: the previous contents stay exactly as they were, and the message
    goes back to the model, which is what lets it correct itself on the next
    turn instead of the step recording something wrong.
    """

    def __init__(self, message: str) -> None:
        super().__init__(message)
        self.message = message


def _typed(field, adapter, value):
    """Validate one value entering the declared state."""
    try:
        return adapter.validate_python(value)
    except ValidationError as error:
        first = error.errors()[0]
        where = ".".join(str(part) for part in first["loc"])
        named = f"{field}.{where}" if where else field
        raise _StateRefused(f"{named}: {first['msg']}") from None


def _append_entry(entries, value):
    """One entry onto a declared list, unless it is already on it.

    A step re-entered mid-call can read a value out of its own state block and
    hand it straight back, which is not a second thing happening. One live call
    entered the booking step four times and finished three of them immediately,
    each with the same appointment it had recorded on the first, so one booking
    became four entries and the caller's recap listed a booking four times.

    An object carries its own identity, so an identical one is the same thing
    reported twice. A plain value is not: two bookings really do give two
    reasons of "create_booking", and both of those count. So the skip is for
    structured entries only.

    Nothing absent is added either, which is how a step that concluded nothing
    this time finishes without inventing an entry.
    """
    if value is None:
        return
    if isinstance(value, (dict, list)) and value in entries:
        return
    entries.append(value)


def _plain(value):
    """A validated value as plain data.

    Plain data is the only shape both frameworks accept back from a tool: one
    refuses a BaseModel outright and drops the whole tool result with a log
    line, the other cannot serialise one at all.
    """
    if isinstance(value, BaseModel):
        return value.model_dump(mode="json")
    if isinstance(value, list):
        return [_plain(entry) for entry in value]
    if isinstance(value, dict):
        return {key: _plain(entry) for key, entry in value.items()}
    return value
`)
	b.WriteString(`

def _schema(adapter):
    """One declared type's schema, with every $ref resolved into place.

    Pydantic emits $defs and a $ref for a shape that contains another shape, and
    this is not a formatting preference. Measured on one real request to the
    provider, three ways:

    - the schema as Pydantic emits it, nested inside one tool property with no
      strict flag: accepted with a 200, and the model invented field names for
      the nested object because it never read the definition. Every result would
      then have been refused where it entered, on every call.
    - the same schema with the refs inlined: accepted, and the model filled the
      shape's own fields exactly, the nullable one included.
    - the shape the other target sends, with the $defs hoisted to the
      parameters root and strict on: accepted, and correct. A $defs anywhere but
      that root is a 400 naming the pointer.

    This target nests the schema inside one property and sends no strict flag,
    so it is the first case unless the refs are resolved here.
    """
    schema = adapter.json_schema()
    defs = schema.pop("$defs", {})

    def resolve(node):
        if isinstance(node, list):
            return [resolve(item) for item in node]
        if not isinstance(node, dict):
            return node
        target = node.get("$ref")
        if isinstance(target, str) and target.startswith("#/$defs/"):
            found = defs.get(target.rsplit("/", 1)[1], {})
            siblings = {key: value for key, value in node.items() if key != "$ref"}
            return {**resolve(found), **siblings}
        return {key: resolve(value) for key, value in node.items()}

    return resolve(schema)
`)
	b.WriteString("\n\n_FINISH_TYPES = {\n")
	for _, step := range finish {
		b.WriteString("    " + pyQuote(step.Task) + ": {\n")
		for _, field := range step.Fields {
			b.WriteString("        " + pyQuote(field.Name) + ": TypeAdapter(" + field.Anno + "),\n")
		}
		b.WriteString("    },\n")
	}
	b.WriteString(`}


def _typed_result(step, values):
    """Validate a step's declared results where they enter the state.

    Refused here rather than carried into a later step that assumes it is
    right, and refused on both targets rather than on the one whose framework
    happens to validate tool arguments: one of them validates through Pydantic
    and lets the model self-correct, the other splats raw JSON into the handler.
    """
    adapters = _FINISH_TYPES.get(step)
    if not adapters:
        return values
    out = dict(values)
    for name, adapter in adapters.items():
        if name in out:
            out[name] = _plain(_typed(name, adapter, out[name]))
    return out


_STATE_STRUCTURED = {`)
	for i, name := range block.Structured {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(pyQuote(name))
	}
	b.WriteString(`}
_STATE_EMPTY = ` + pyQuote(ir.StateEmptyText()) + `
# The bound on one rendered value, in characters. The same number the router
# bounds a template variable by, because this is the same value travelling the
# same way, and one number cannot be two.
_STATE_VALUE_MAX = ` + strconv.Itoa(slngVariableLimit) + `


def _state_text(name, value):
    """One value as a prompt reads it.

    Compact JSON for anything declared structured, never a Python repr: a repr
    writes single quotes and None, which is not JSON and is not what any
    provider produced. Words for a declared value with no contents, so a step
    cannot mistake "not yet known" for "known to be nothing".

    A value that was never declared structured renders exactly as it did before
    this existed, which is what keeps every package written before it unchanged.
    """
    if name in _STATE_STRUCTURED:
        if value is None or value == "" or value == [] or value == {}:
            return _STATE_EMPTY
        if not isinstance(value, str):
            value = json.dumps(_plain(value), separators=(",", ":"), ensure_ascii=False)
    text = "" if value is None else str(value)
    if len(text) > _STATE_VALUE_MAX:
        # The length is only knowable here, at run time, so this cannot be a
        # compile-time refusal. What it must not be is silent: a shortened value
        # is a value the model reads as complete. An f-string rather than a
        # placeholder, because this line is emitted into two modules that log
        # through two different libraries and either style prints literally on
        # the other one.
        logger.warning(
            f"declared state: {name} rendered {len(text)} characters and is shortened to "
            f"{_STATE_VALUE_MAX}; a value this long also stops the prompt being cached"
        )
        text = text[:_STATE_VALUE_MAX]
    return text
`)
	block.Source = b.String()
	return block, nil
}

// finishStep is one task's declared result fields that carry a type, in the
// order the finish table emits them.
type finishStep struct {
	Task   string
	Fields []finishField
}

type finishField struct {
	Name string
	Anno string
}

// finishTypes is every task result field declaring more than a bare primitive,
// which is exactly the set the finish handler has to validate.
func finishTypes(agent *ir.Agent) []finishStep {
	var out []finishStep
	for _, name := range sortedKeys(agent.Tasks) {
		task := agent.Tasks[name]
		step := finishStep{Task: name}
		for _, field := range sortedKeys(task.Result) {
			if ref := task.Result[field].Shape; ref != nil {
				step.Fields = append(step.Fields, finishField{Name: field, Anno: PyAnno(ref)})
			}
		}
		if len(step.Fields) > 0 {
			out = append(out, step)
		}
	}
	return out
}

// shapeOrder is every declared shape in dependency order, so a class naming
// another is emitted after it. Ties break on the name, so the same package
// emits the same bytes twice.
//
// The collision check lives here rather than in internal/ir because this is
// where the class names exist: a shape whose class name is one the module
// already defines would be silently replaced by whichever definition came
// second, and the first sign of it is a finish handler validating against the
// wrong class mid-call.
func shapeOrder(agent *ir.Agent) ([]ir.Shape, error) {
	taken := emittedClassNames(agent)
	for _, name := range sortedKeys(agent.Shapes) {
		if owner, ok := taken[name]; ok {
			return nil, fmt.Errorf("shape %q generates a class the module already defines for %s. "+
				"Rename the shape: two classes of one name means the second silently replaces the first",
				name, owner)
		}
	}
	var out []ir.Shape
	placed := map[string]bool{}
	var walk func(name string)
	walk = func(name string) {
		if placed[name] {
			return
		}
		placed[name] = true
		shape := agent.Shapes[name]
		for _, field := range shape.Fields {
			for _, referenced := range shapeRefs(field.Type) {
				if _, ok := agent.Shapes[referenced]; ok {
					walk(referenced)
				}
			}
		}
		out = append(out, shape)
	}
	// Cycles were refused at build, so this recursion terminates.
	for _, name := range sortedKeys(agent.Shapes) {
		walk(name)
	}
	return out, nil
}

// emittedClassNames maps every class name the module already defines onto what
// it belongs to, for the refusal above.
func emittedClassNames(agent *ir.Agent) map[string]string {
	taken := map[string]string{
		"Userdata": "the shared state", "State": "the shared state",
		"Agent": "the framework", "AgentTask": "the framework", "AgentSession": "the framework",
		"BaseModel": "Pydantic", "Field": "Pydantic", "TypeAdapter": "Pydantic",
		"AfterValidator": "Pydantic", "ValidationError": "Pydantic",
		"Annotated": "the typing module", "Literal": "the typing module",
		"NodeConfig": "the framework", "LLMWorker": "the framework",
	}
	for _, name := range sortedKeys(agent.Agents) {
		taken[pyName(name)] = fmt.Sprintf("agent %q", name)
	}
	for _, name := range sortedKeys(agent.Tasks) {
		taken[pyName(name)] = fmt.Sprintf("task %q", name)
	}
	return taken
}

func shapeRefs(ref *ir.TypeRef) []string {
	if ref == nil {
		return nil
	}
	if ref.Shape != "" {
		return []string{ref.Shape}
	}
	return shapeRefs(ref.List)
}

// PyAnno is a resolved type as a Python annotation. A declared shape lowers to
// its generated class, and a text type with a shape lowers to its alias, which
// is `str` plus a validator and never a schema keyword.
func PyAnno(ref *ir.TypeRef) string {
	if ref == nil {
		return "str"
	}
	var text string
	switch {
	case ref.Shape != "":
		text = ref.Shape
	case ref.Shaped != "":
		text = string(ref.Shaped)
	case len(ref.Literal) > 0:
		entries := make([]string, 0, len(ref.Literal))
		for _, entry := range ref.Literal {
			entries = append(entries, pyQuote(entry))
		}
		text = "Literal[" + strings.Join(entries, ", ") + "]"
	case ref.List != nil:
		text = "list[" + PyAnno(ref.List) + "]"
	default:
		text = pyType(ref.Primitive)
	}
	if ref.Optional {
		text += " | None"
	}
	return text
}

// pyFieldAnno is one class field's annotation, carrying its description so the
// model is told what the field means without the author repeating it in prose
// (FR-014).
//
// A field's own description replaces the one its shaped type carries rather
// than joining it, which is Pydantic's rule and not a choice made here. So the
// format is appended: documenting a field is otherwise the one thing that would
// stop the model being told what shape the value has to be.
func pyFieldAnno(field ir.Field) string {
	anno := PyAnno(field.Type)
	if field.Description == "" {
		return anno
	}
	description := field.Description
	if kind := shapedKind(field.Type); kind != "" {
		description = strings.TrimSuffix(description, " ") + " Expected " + shapedPatterns[kind].phrase + "."
	}
	return "Annotated[\n        " + anno + ",\n        Field(description=" +
		pyQuote(description) + "),\n    ]"
}

// shapedKind is the text type a field is declared with, through a list if it is
// a list of them, and empty for anything else.
func shapedKind(ref *ir.TypeRef) ir.ShapedText {
	if ref == nil {
		return ""
	}
	if ref.Shaped != "" {
		return ref.Shaped
	}
	return shapedKind(ref.List)
}

// usedShapedText is the set of text types the package actually declares, so a
// package using none emits no alias and no compiled pattern.
func usedShapedText(agent *ir.Agent) map[ir.ShapedText]bool {
	used := map[ir.ShapedText]bool{}
	walkTypeRefs(agent, func(ref *ir.TypeRef) {
		if ref.Shaped != "" {
			used[ref.Shaped] = true
		}
	})
	return used
}

func declaresLiteral(agent *ir.Agent) bool {
	found := false
	walkTypeRefs(agent, func(ref *ir.TypeRef) {
		if len(ref.Literal) > 0 {
			found = true
		}
	})
	return found
}

func fieldCarriesDescription(agent *ir.Agent, classes []ir.Shape) bool {
	for _, class := range classes {
		for _, field := range class.Fields {
			if field.Description != "" {
				return true
			}
		}
	}
	return false
}

// walkTypeRefs visits every resolved type the package declares: on a variable,
// on a shape's field, and on a task's result field.
func walkTypeRefs(agent *ir.Agent, visit func(*ir.TypeRef)) {
	var walk func(ref *ir.TypeRef)
	walk = func(ref *ir.TypeRef) {
		if ref == nil {
			return
		}
		visit(ref)
		walk(ref.List)
	}
	for _, name := range sortedKeys(agent.Variables) {
		walk(agent.Variables[name].Shape)
	}
	for _, name := range sortedKeys(agent.Shapes) {
		for _, field := range agent.Shapes[name].Fields {
			walk(field.Type)
		}
	}
	for _, name := range sortedKeys(agent.Tasks) {
		task := agent.Tasks[name]
		for _, field := range sortedKeys(task.Result) {
			walk(task.Result[field].Shape)
		}
	}
}

// stateField is one declared value as a shared-state dataclass declares it:
// the annotation, and the default.
//
// A declared list starts empty rather than absent, so an append never has to
// create it, and through a factory because a shared mutable default on a
// dataclass is one call's state leaking into the next.
//
// nullableWithDefault is the one place the two targets differ, and the
// divergence is older than this feature: LiveKit annotates every field
// `| None` whatever its default, while Pipecat annotates a field carrying a
// default with its bare type. Passed in rather than decided here, so this file
// does not quietly pick a winner and no package written before this feature
// emits a byte differently.
func stateField(variable ir.Variable, nullableWithDefault bool) (anno, def string) {
	anno = pyType(variable.Type)
	if variable.Shape != nil {
		anno = PyAnno(variable.Shape)
	}
	if variable.Shape.IsList() {
		return anno, "field(default_factory=list)"
	}
	def = "None"
	if variable.Default != nil {
		def = pyLiteral(variable.Default)
	}
	if variable.Default == nil || nullableWithDefault {
		if !strings.HasSuffix(anno, " | None") {
			anno += " | None"
		}
	}
	return anno, def
}

// StateNeedsDataclassField reports whether any declared value starts as an
// empty list, which is the one thing that needs `field` beside `dataclass`.
func StateNeedsDataclassField(agent *ir.Agent) bool {
	for _, variable := range agent.Variables {
		if variable.Shape.IsList() {
			return true
		}
	}
	return false
}

// PydanticImports is the `from pydantic import ...` line each module needs.
// One computed list rather than two conditional lines, because Field is wanted
// by a tool argument description as well as by a shape field and importing it
// twice is what a linter reads as a redefinition.
func PydanticImports(needsField bool, typed bool) string {
	if typed {
		return "AfterValidator, BaseModel, Field, TypeAdapter, ValidationError"
	}
	if needsField {
		return "Field"
	}
	return ""
}
