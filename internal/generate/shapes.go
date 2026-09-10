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
//
// A kind carries exactly one of two checks: a `regex`, which the emitter wraps
// in the shared pattern body, or a `body`, which is the whole emitted function
// body for a check no pattern can make. An email address is the second case.
// Writing one as a regex is a well known way to reject real addresses, and the
// library that gets it right is the one Pydantic itself reaches for, so the
// emitted project asks for `email-validator` when a package declares the type
// and never otherwise.
//
// Neither and both are refused by TestEveryShapedKindCarriesOneCheck, because a
// kind with neither reads a zero-value row: it would emit `re.compile(r"")` and
// a refusal saying only "expected ".
var shapedPatterns = map[ir.ShapedText]struct {
	regex  string
	body   string
	phrase string
}{
	ir.ShapedPhone: {
		regex:  `^\+[1-9]\d{6,14}$`,
		phrase: "a phone number in E.164, one leading plus and 7 to 15 digits, like +34600111222",
	},
	ir.ShapedDate: {
		regex:  `^\d{4}-\d{2}-\d{2}$`,
		phrase: "a day written year-month-day, like 2026-03-19",
	},
	ir.ShapedTime: {
		regex:  `^([01]\d|2[0-3]):[0-5]\d$`,
		phrase: "a time of day on the 24-hour clock, like 09:30 or 17:45",
	},
	ir.ShapedID: {
		regex:  `^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`,
		phrase: "an identifier: letters, digits, and then any of dot, dash, underscore or colon",
	},
	ir.ShapedEmail: {
		body:   emailShapeBody,
		phrase: "an email address, like name@example.com",
	},
}

// emailShapeBody is the emitted body for EmailStr. It is written out here rather
// than assembled from parts because it is Python somebody has to be able to
// read.
//
// check_deliverability=False is not a preference. The library's default is True,
// which sends DNS queries for the domain's mail servers, and this validator runs
// on the voice path: a slow resolver would hold the caller in silence, and a
// caller giving an address the compiler cannot post to today is still giving the
// address they have.
//
// The normalized form is what gets saved, which is what makes the value plain
// text a later tool can use without normalizing it again.
const emailShapeBody = `    # Empty is not a wrong value, it is no value yet, exactly as for the
    # pattern-checked types. Refusing it deadlocked a live call on both targets.
    if not value:
        return value
    try:
        info = validate_email(value, check_deliverability=False)
    except EmailNotValidError as exc:
        # The library's own sentence names what it stopped at, which is more than
        # the phrase can, so it rides along after it and the model can correct
        # itself from one refusal instead of guessing.
        raise ValueError(` + shapedExpected + ` + ": " + str(exc)) from exc
    return info.normalized`

// shapedExpected is the token a validator body writes where the refusal's
// "expected <phrase>" belongs. One phrase reaches the model as the alias
// description and the refusal as this text, so the two cannot drift.
const shapedExpected = "__SHAPED_EXPECTED__"

// patternShapeBody is the shared body for a text type checked by a pattern.
func patternShapeBody(upper string) string {
	return `    # Empty is not a wrong value, it is no value yet. It is what a declared
    # variable holds before anything fills it, what an empty prompt reference
    # renders as words, and what a tool hands back for a field it could not fill. Refusing
    # it here deadlocked a live call on both targets: the model had nothing else
    # to send, so every retry was refused the same way and the step never
    # finished. A wrong value is still refused; an absent one is not wrong.
    if value and not _SHAPE_` + upper + `.match(value):
        raise ValueError(` + shapedExpected + `)
    return value`
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

// ShapedPatterns is every **pattern-checked** text type's regex, for that gate.
// A kind checked by a library call is absent, because it has no pattern to hold
// to the raw-string rule; ShapedBody is how the gate sees it instead.
func ShapedPatterns() map[ir.ShapedText]string {
	out := make(map[ir.ShapedText]string, len(shapedPatterns))
	for kind, row := range shapedPatterns {
		if row.regex != "" {
			out[kind] = row.regex
		}
	}
	return out
}

// ShapedBody is the emitted validator body for a text type checked by something
// other than a pattern, empty for the pattern-checked ones. Exported so one gate
// can hold every kind to carrying exactly one of the two.
func ShapedBody(kind ir.ShapedText) string { return shapedPatterns[kind].body }

// shapedOrder fixes the order the aliases are emitted in, so the same package
// renders the same bytes twice. It reads internal/ir's list rather than keeping
// its own, because a kind missing here emits no alias at all while PyAnno still
// writes its name into an annotation, which is a NameError at import.
var shapedOrder = ir.ShapedTextOrder()

// TypedStateBlock is what a driver renders: the Python, plus what each
// template's import block needs to know without re-deriving it.
type TypedStateBlock struct {
	Source string
	// Structured names values that explicit references render as JSON, sorted.
	Structured []string
	// Values is every declared value in authoring order, with its authored type.
	// The runbook prints the declarations without claiming every prompt sees them.
	Values []TypedStateValue
	// Empty is what a value with no contents renders as, so the runbook quotes
	// the string rather than paraphrasing it.
	Empty string
	// NeedsRe says a **pattern-checked** text type is used, so the module
	// compiles a regex. EmailStr is checked by a library call and compiles none,
	// which is why this is no longer "any shaped type is used": an unused
	// `import re` fails the ruff gate the emitted README promises.
	NeedsRe bool
	// NeedsTerminal says a task ends on its own tool, which is the one thing
	// that emits the terminal helpers. False for a package writing no `finish:`,
	// so that package emits the bytes it emitted before the key existed.
	NeedsTerminal bool
	// NeedsWithdrawal says some group step carries `skip_when_confirmed:`, which
	// is the one thing that emits the confirmation helpers.
	NeedsWithdrawal bool
	// NeedsShaped says any text type with a validated shape is used, which is
	// what needs AfterValidator and typing.Annotated.
	NeedsShaped    bool
	NeedsJSON      bool
	NeedsAnnotated bool
	NeedsField     bool
	NeedsLiteral   bool
	// NeedsModelValidator says a compiler-supplied class carries a parser, which
	// is the one place this block reaches for Pydantic's model_validator.
	NeedsModelValidator bool
	// NeedsEmailValidator says the block calls email-validator, which is the one
	// thing here that is not stdlib or Pydantic. It drives both the import in
	// each template and the dependency in each emitted pyproject.toml, so those
	// three cannot disagree: an import with no dependency is a startup
	// ImportError, and a dependency with no import is a package the image
	// installs for nothing.
	NeedsEmailValidator bool
}

// builtinClassBody is the extra body a compiler-supplied class carries after its
// fields, empty for an authored shape and for a built-in that needs none.
//
// Keyed by name rather than by a field on ir.Shape holding Python, because the
// emitted Python belongs in the file that emits Python. A built-in with no entry
// here emits a plain class, which is a class with no parser rather than a
// broken one, and TestBuiltinShapesAllEmitAParser refuses that silence.
func builtinClassBody(class ir.Shape) string {
	if !class.Builtin {
		return ""
	}
	return builtinClassBodies[class.Name]
}

// nameEmailParser lets one string stand in for the pair.
//
// The model fills the two fields, which is why the schema stays an object and
// carries no format keyword. This is for the other way in: a tool that returns
// "Fred Bloggs <fred@example.com>" or a bare address, a seeded call-start value,
// and a dotted assign that picks one string out of a result. Without it those
// all fail validation on a value that is perfectly readable.
//
// allow_display_name is what reads the name out of the angle-bracket form, and
// it needs email-validator 2.2 or newer. With no name to read, the local part
// stands in, which is Pydantic's own answer for the same input.
const nameEmailParser = `    @model_validator(mode="before")
    @classmethod
    def _read_one_string(cls, value):
        if not isinstance(value, str):
            return value
        if not value.strip():
            # No value yet, not a wrong one. Both fields stay empty and every
            # prompt naming them renders the words a missing value renders.
            return {"name": "", "email": ""}
        try:
            info = validate_email(
                value, check_deliverability=False, allow_display_name=True
            )
        except EmailNotValidError as exc:
            raise ValueError(
                "expected an email address, on its own or with a name in front of"
                " it, like Fred Bloggs <fred@example.com>: " + str(exc)
            ) from exc
        return {
            "name": info.display_name or info.local_part,
            "email": info.normalized,
        }
`

var builtinClassBodies = map[string]string{"NameEmail": nameEmailParser}

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
	for _, task := range agent.Tasks {
		if len(task.Finish) > 0 {
			block.NeedsTerminal = true
		}
		if task.Withdraws {
			block.NeedsWithdrawal = true
		}
	}
	used := usedShapedText(agent)
	if len(classes) == 0 && len(agent.Variables) == 0 && len(finish) == 0 && len(agent.Tasks) == 0 {
		return TypedStateBlock{}, nil
	}
	block.Empty = ir.StateEmptyText()
	// Per used kind and not "any kind is used": EmailStr is checked by a library
	// call and compiles no pattern, so a package declaring only that one has no
	// `re` to import and the emitted ruff gate refuses an unused one.
	for kind := range used {
		block.NeedsShaped = true
		block.NeedsRe = block.NeedsRe || shapedPatterns[kind].regex != ""
		block.NeedsEmailValidator = block.NeedsEmailValidator || kind == ir.ShapedEmail
	}
	for _, class := range classes {
		if builtinClassBody(class) == "" {
			continue
		}
		block.NeedsModelValidator = true
		// A supplied parser reads the address itself, so it needs the library
		// whether or not any declared value is an EmailStr on its own.
		block.NeedsEmailValidator = true
	}
	block.NeedsJSON = true
	block.NeedsLiteral = declaresLiteral(agent)
	// A shaped text type is itself an Annotated alias, so using one needs the
	// import even in a package that declares no literal and no description. It
	// was missing, and the package that would have found it does not exist yet:
	// every shipped one declaring a shaped type also declares one of the other
	// two, so the import arrived for another reason.
	block.NeedsField = fieldCarriesDescription(agent, classes)
	block.NeedsAnnotated = block.NeedsShaped || block.NeedsField

	var b strings.Builder
	b.WriteString(`# --- declared state ----------------------------------------------------------
# Generated from the ` + "`shapes:`" + ` and the typed ` + "`variables:`" + ` in agent.yaml. Both
# target frameworks already depend on Pydantic, so nothing here adds one. The one
# exception is EmailStr, which is checked by email-validator: declaring it puts
# that package in this project's pyproject.toml, and declaring no email type
# leaves both the import and the dependency out.
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
		upper := strings.ToUpper(lower)
		check, body := "", row.body
		if row.regex != "" {
			check = "_SHAPE_" + upper + " = re.compile(" + pyRaw(row.regex) + ")\n\n\n"
			body = patternShapeBody(upper)
		}
		// The phrase is substituted rather than formatted in, because a body is
		// Python and a stray percent sign in one would otherwise be read as a
		// verb. One token, one owner: shapedExpected.
		body = strings.ReplaceAll(body, shapedExpected, pyQuote("expected "+row.phrase))
		fmt.Fprintf(&b, `
%sdef _shape_%s(value: str) -> str:
%s


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
			check, lower, body, string(kind), lower, pyQuote(row.phrase))
	}
	for _, class := range classes {
		b.WriteString("\n\nclass " + class.Name + "(BaseModel):\n")
		if class.Description != "" {
			b.WriteString("    " + pyTriple(class.Description) + "\n\n")
		}
		for _, field := range class.Fields {
			b.WriteString("    " + field.Name + ": " + pyFieldAnno(field) + "\n")
		}
		if body := builtinClassBody(class); body != "" {
			b.WriteString("\n" + body)
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

    A step re-entered mid-call can read a value through an explicit prompt
    reference and hand it straight back, which is not a second thing happening. One live call
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


def _task_status(values):
    return {"status": "unserved" if values.get("unserved_request") else "completed"}


def _group_status(results):
    return {"status": "unserved" if any(value.get("unserved_request") for value in results.values()) else "completed"}


def _typed_result(step, values):
    """Validate a step's declared results where they enter the state.

    Refused here rather than carried into a later step that assumes it is
    right, and refused on both targets rather than on the one whose framework
    happens to validate tool arguments: one of them validates through Pydantic
    and lets the model self-correct, the other splats raw JSON into the handler.
    """
    if values.get("unserved_request"):
        return {"unserved_request": values["unserved_request"]}
    adapters = _FINISH_TYPES.get(step)
    if not adapters:
        return values
    out = dict(values)
    for name, adapter in adapters.items():
        # Absent goes through the adapter too, rather than being skipped. A
        # field the model left out is a field with no value, and that is what a
        # prompt telling it to leave one out asks for: a value that may be
        # absent validates as None and the append drops it, and a value that
        # may not is refused here with the message that lets the model correct
        # itself. Skipping an absent field instead left the key missing from
        # the result, and the assignment that reads it by name raised a
        # KeyError inside the finish handler on the target whose framework
        # validates no argument of its own.
        out[name] = _plain(_typed(name, adapter, out.get(name)))
    return out
`)
	b.WriteString("\n\n_STATE_TYPES = {\n")
	for _, name := range sortedKeys(agent.Variables) {
		variable := agent.Variables[name]
		anno := pyType(variable.Type)
		if variable.Shape != nil {
			anno = PyAnno(variable.Shape)
		}
		fmt.Fprintf(&b, "    %s: TypeAdapter(%s),\n", pyQuote(name), anno)
	}
	b.WriteString("}\n_TASK_ASSIGNMENTS = {\n")
	for _, name := range sortedKeys(agent.Tasks) {
		fmt.Fprintf(&b, "    %s: [\n", pyQuote(name))
		for _, entry := range agent.Tasks[name].Assign {
			fmt.Fprintf(&b, "        (%s, %s, %s),\n", pyQuote(entry.Var), pyQuote(entry.Field), pyLiteral(entry.Append))
		}
		b.WriteString("    ],\n")
	}
	b.WriteString("}\n_STATE_CONFIRM = {\n")
	for _, name := range sortedKeys(agent.Variables) {
		if confirmer := agent.Variables[name].Confirm; confirmer != "" {
			fmt.Fprintf(&b, "    %s: %s,\n", pyQuote(name), pyQuote(confirmer))
		}
	}
	b.WriteString("}\n_STATE_DEPENDENCIES = {\n")
	for _, entry := range agent.Prefetch {
		var roots []string
		for _, input := range entry.Inputs {
			root := ir.PathRoot(input)
			if !slices.Contains(roots, root) {
				roots = append(roots, root)
			}
		}
		for _, pair := range entry.Assign {
			if agent.Variables[pair.Key].ConfirmInherited {
				fmt.Fprintf(&b, "    %s: %s,\n", pyQuote(pair.Key), pyLiteral(anyStrings(roots)))
			}
		}
	}
	b.WriteString(`}


def _save_result(step, state, values):
    """Validate all assignments before changing any call state."""
    values = _typed_result(step, values)
    if values.get("unserved_request"):
        return values
    pending = {}
    for name, path, append in _TASK_ASSIGNMENTS.get(step, ()):
        value = values
        for part in path.split("."):
            value = value.get(part) if isinstance(value, dict) else None
        if append:
            if value is None:
                continue
            entries = list(getattr(state, name, None) or [])
            _append_entry(entries, value)
            value = entries
        pending[name] = _plain(_typed(name, _STATE_TYPES[name], value))
    _save_batch(state, pending, step=step)
    return values


def _save_batch(state, values, *, step=None, inputs=None):
    """Commit a validated batch and invalidate older results of changed inputs."""
    if not values:
        return
    pending = {name: _plain(_typed(name, _STATE_TYPES[name], value)) for name, value in values.items()}
    unconfirmed: set[str] = set(getattr(state, "_unconfirmed", ()))
    provenance = dict(getattr(state, "_prefetch_provenance", {}))
    affected = {name for name, value in pending.items() if getattr(state, name, None) != value}
    while True:
        more = {name for name, reads in provenance.items() if set(reads) & affected} - affected
        if not more:
            break
        affected.update(more)
    invalidated = affected - pending.keys()
    for name in invalidated:
        provenance.pop(name, None)
    for name, value in pending.items():
        if inputs is not None:
            provenance[name] = tuple(inputs)
        else:
            provenance.pop(name, None)
        if name in _STATE_CONFIRM:
            if _STATE_CONFIRM[name] == step and value is not None and value != "":
                unconfirmed.discard(name)
            else:
                unconfirmed.add(name)
    def current(name):
        return None if name in invalidated else pending.get(name, getattr(state, name, None))
    # Dependencies are acyclic: prefetch can only read earlier entries.
    for _ in range(len(_STATE_DEPENDENCIES) + 1):
        before = set(unconfirmed)
        for name, reads in _STATE_DEPENDENCIES.items():
            if current(name) is not None and current(name) != "" and all(
                source not in unconfirmed and current(source) is not None and current(source) != "" for source in reads
            ):
                unconfirmed.discard(name)
            else:
                unconfirmed.add(name)
        if before == unconfirmed:
            break
    for name in invalidated:
        setattr(state, name, None)
    for name, value in pending.items():
        setattr(state, name, value)
    if hasattr(state, "_unconfirmed"):
        state._unconfirmed = unconfirmed
    if inputs is not None or hasattr(state, "_prefetch_provenance"):
        state._prefetch_provenance = provenance

`)
	if block.NeedsTerminal {
		b.WriteString(`
def _success_word(value):
    """One result value as a success pair reads it.

    The pairs come out of YAML as text, so a boolean has to read as the word the
    author wrote rather than as Python's own spelling of it: True never matches
    the word true, and the step would silently never end.
    """
    if isinstance(value, bool):
        return "true" if value else "false"
    return "" if value is None else str(value)


def _terminal_success(result, success):
    """Whether a tool result means this step is done.

    Every field is required and its value has to be one of the listed ones.
    Anything else is an ordinary result and goes back to the model, which is
    what keeps a failed booking a conversation rather than a saved one.
    """
    if not isinstance(result, dict):
        return False
    # The loop variable is name, not field: a package declaring a list imports
    # dataclasses' own field, and a loop variable of that name shadows it.
    for name, values in success.items():
        if _success_word(result.get(name)) not in values:
            return False
    return True


def _merge_retained(step, args, retained):
    """The model's finish arguments with the tool's own values put back.

    Reached only when a save was refused and the model is repairing it. A field
    the tool returned validly is the authoritative one: the model cannot
    manufacture a booking reference, and a repair that retyped one would record
    a booking nobody made. A retained value that does not validate is left to
    the model, because refusing here would leave the step with no way out.
    """
    out = dict(args)
    for name, adapter in _FINISH_TYPES.get(step, {}).items():
        if name not in retained:
            continue
        try:
            _typed(name, adapter, retained[name])
        except _StateRefused:
            continue
        out[name] = retained[name]
    return out


`)
	}
	if block.NeedsWithdrawal {
		b.WriteString(`
def _is_confirmed(state, name):
    """Whether a value is confirmed right now.

    Both halves matter: a value nobody has agreed to is unconfirmed, and so is
    one that was withdrawn. A group reads this once, as it starts, to decide
    whether the step that confirms it has to run.
    """
    value = getattr(state, name, None)
    return name not in getattr(state, "_unconfirmed", ()) and value is not None and value != ""


def _withdraw_confirmation(state, step):
    """Withdraw what this step confirms, because this step is about to run again.

    A step a group may skip cannot be trusted to have confirmed anything once it
    is entered: the caller is correcting the value, or the step is running
    because the confirmation had already lapsed. Values derived from a withdrawn
    one follow it, through the same dependency pass a save runs.

    Scoped to the steps a group names with skip_when_confirmed:, so a package
    that names none behaves exactly as it did.
    """
    withdrawn = {name for name, owner in _STATE_CONFIRM.items() if owner == step}
    if not withdrawn:
        return
    unconfirmed = set(getattr(state, "_unconfirmed", ())) | withdrawn
    # Dependencies are acyclic, so one pass per entry settles them. The same
    # loop _save_batch runs, deliberately not shared with it: sharing would mean
    # editing a function every package emits, and every package that writes none
    # of this has to keep emitting exactly what it emitted.
    for _ in range(len(_STATE_DEPENDENCIES) + 1):
        before = set(unconfirmed)
        for name, reads in _STATE_DEPENDENCIES.items():
            if any(source in unconfirmed for source in reads):
                unconfirmed.add(name)
        if before == unconfirmed:
            break
    if hasattr(state, "_unconfirmed"):
        state._unconfirmed = unconfirmed
    logger.info("withdrew confirmation on entering " + step)


`)
	}
	b.WriteString(`

_STATE_STRUCTURED = {`)
	for i, name := range block.Structured {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(pyQuote(name))
	}
	b.WriteString(`}
_STATE_EMPTY = ` + pyQuote(ir.StateEmptyText()) + `
`)
	b.WriteString(`# The bound on one rendered value, in characters. The same number the router
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
    if value is None or value == "":
        return _STATE_EMPTY
    if not isinstance(value, str):
        value = json.dumps(_plain(value), separators=(",", ":"), ensure_ascii=False)
    text = str(value)
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


def _prompt_value(state, name, site=""):
    root, value = _state_lookup(state, name)
    if root in getattr(state, "_unconfirmed", ()) and site != "task:" + _STATE_CONFIRM.get(root, ""):
        return root, None
    return root, value


def _state_lookup(state, name):
    """The value a placeholder names, and the declared name it belongs to.

    A path is authored {{customer.status}} and emitted {{customer__status}}: one
    flat name, because the router substitutes flat names only and both render
    paths have to agree. Everything before the first "__" is the declared value;
    each "__" after it starts a field, read as a dict key or an attribute and as
    None past an absent link, so a field of a record nobody has filled renders
    as the empty words and never raises. The root's name comes back with the
    value because the words for an empty value belong to the root variable.
    """
    root, _, path = name.partition("__")
    value = getattr(state, root, None) if state is not None else None
    for part in path.split("__") if path else ():
        if value is None:
            break
        value = value.get(part) if isinstance(value, dict) else getattr(value, part, None)
    return root, value
`)
	block.Source = b.String()
	return block, nil
}

// finishStep is one task's derived finish fields in emitted order.
type finishStep struct {
	Task   string
	Fields []finishField
}

type finishField struct {
	Name string
	Anno string
}

// finishTypes validates every domain field, including primitive outputs.
func finishTypes(agent *ir.Agent) []finishStep {
	var out []finishStep
	for _, name := range sortedKeys(agent.Tasks) {
		step := finishStep{Task: name}
		for _, field := range sortedKeys(agent.Tasks[name].Result) {
			value := agent.Tasks[name].Result[field]
			step.Fields = append(step.Fields, finishField{Name: field, Anno: pyAnno(resultPyType(value), value.Enum, value.Description)})
		}
		out = append(out, step)
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
	// The second net behind ir's reservedShapeName, which refuses the
	// declaration. This one catches a name that reaches the catalog some other
	// way, and it names the built-in rather than the framework, so the refusal
	// says something the author can act on.
	for _, name := range ir.BuiltinShapeNames() {
		if _, ok := agent.Shapes[name]; ok && agent.Shapes[name].Builtin {
			continue
		}
		taken[name] = "a shape this compiler supplies"
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
	for _, task := range agent.Tasks {
		for _, field := range task.Result {
			if len(field.Enum) > 0 {
				return true
			}
		}
	}
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
	for _, task := range agent.Tasks {
		for _, field := range task.Result {
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
func PydanticImports(needsField bool, typed *TypedStateBlock) string {
	var names []string
	if typed != nil {
		if typed.NeedsShaped {
			names = append(names, "AfterValidator")
		}
		if typed.NeedsModelValidator {
			names = append(names, "model_validator")
		}
		names = append(names, "BaseModel", "TypeAdapter", "ValidationError")
		needsField = needsField || typed.NeedsField
	}
	if needsField {
		names = append(names, "Field")
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}
