package generate

import (
	"bytes"
	"slices"
	"strings"
	"text/template"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/stateschema"
)

// The call state the code targets emit is two modules. state.py is the
// author's own file, copied as written. call_state.py is generated, and it is
// the same text on both targets: CallState subclasses the author's State and
// adds what a call needs around it, StepResult turns a step's finish into one
// validated model, and nothing in either writes a Python type. Every type comes
// from State's own annotations, read at import.

// TypedStateBlock is what a driver renders: the generated module, plus what
// each template's import block and runbook need without re-deriving it.
type TypedStateBlock struct {
	Source string
	// Values is every declared value in the order State declares it, with its
	// type named for the runbook.
	Values []TypedStateValue
	// Empty is what a value with no contents renders as, so the runbook quotes
	// the string rather than paraphrasing it.
	Empty string
	// HasState says the package has a state.py. Without one, call_state.py
	// declares an empty State so a step still has somewhere to save.
	HasState bool
	// NeedsEmailValidator says State holds an email type, which Pydantic checks
	// through email-validator: it puts that package in pyproject.toml.
	NeedsEmailValidator bool
	// ExtraTypes are the pydantic_extra_types modules state.py imports, each of
	// which needs its own extra installed.
	ExtraTypes []string
}

// TypedStateValue is one declared value as the runbook names it.
type TypedStateValue struct {
	Name string
	Type string
	// Confirm is the step that must hear the caller agree before anything acts
	// on this value, empty when it is settled on arrival.
	Confirm string
}

// callStateData fills callStateTemplate.
type callStateData struct {
	HasState     bool
	Members      string
	Assignments  string
	Results      string
	Confirm      string
	Dependencies string
	CallStart    string
	Empty        string
	ValueMax     int
	Unserved     string
}

// TypedState renders call_state.py for a package that keeps state, runs a
// step, or has target members to carry, and nothing otherwise. members is the
// target's own class body, added to CallState after the shared members.
func TypedState(agent *ir.Agent, members string) (TypedStateBlock, error) {
	if agent.State == nil && len(agent.Tasks) == 0 && members == "" {
		return TypedStateBlock{}, nil
	}
	block := TypedStateBlock{Empty: ir.StateEmptyText(), HasState: agent.State != nil}
	if agent.State != nil {
		block.ExtraTypes = agent.State.ExtraTypes
		for _, name := range agent.VariableOrder {
			variable := agent.Variables[name]
			block.Values = append(block.Values, TypedStateValue{Name: name, Type: variable.Schema.String(), Confirm: variable.Confirm})
			block.NeedsEmailValidator = block.NeedsEmailValidator || reachesEmail(variable.Schema)
		}
	}
	data := callStateData{
		HasState: block.HasState, Members: members,
		Assignments: assignmentsTable(agent), Results: resultsTable(agent),
		Confirm: confirmTable(agent), Dependencies: dependenciesTable(agent),
		CallStart: "(" + pyTuple(callStartNames(agent)) + ")",
		Empty:     pyQuote(ir.StateEmptyText()), ValueMax: slngVariableLimit,
		Unserved: pyQuote(ir.UnservedResultDescription),
	}
	var b bytes.Buffer
	if err := callStateTemplate.Execute(&b, data); err != nil {
		return TypedStateBlock{}, err
	}
	block.Source = b.String()
	return block, nil
}

// reachesEmail reports whether a type holds an email anywhere in it.
func reachesEmail(typ *stateschema.Type) bool {
	if typ == nil {
		return false
	}
	if typ.Format == "email" || typ.Format == "name-email" {
		return true
	}
	if reachesEmail(typ.Items) {
		return true
	}
	for _, field := range typ.Fields {
		if reachesEmail(field.Type) {
			return true
		}
	}
	return false
}

// callStartNames are the variables a dispatch may fill, sorted so the emitted
// tuple does not move when a field is declared in a different place.
func callStartNames(agent *ir.Agent) []string {
	var names []string
	for _, name := range agent.VariableOrder {
		if source := agent.Variables[name].Source; source == "" || source == ir.VariableSourceCallStart {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// assignmentsTable is step -> what its finish saves: (state field, result path,
// append). Every step has a row, so a step that saves nothing reads as that.
func assignmentsTable(agent *ir.Agent) string {
	var b strings.Builder
	b.WriteString("{")
	for _, name := range sortedKeys(agent.Tasks) {
		b.WriteString("\n        " + pyQuote(name) + ": (")
		for _, entry := range agent.Tasks[name].Assign {
			b.WriteString("(" + pyQuote(entry.Var) + ", " + pyQuote(entry.Field) + ", " + pyLiteral(entry.Append) + "), ")
		}
		b.WriteString("),")
	}
	if len(agent.Tasks) > 0 {
		b.WriteString("\n    ")
	}
	b.WriteString("}")
	return b.String()
}

// resultsTable is step -> its finish fields: (field, state field, append,
// required). StepResult builds each step's model from it.
func resultsTable(agent *ir.Agent) string {
	var b strings.Builder
	b.WriteString("{")
	for _, name := range sortedKeys(agent.Tasks) {
		b.WriteString("\n        " + pyQuote(name) + ": (")
		result := agent.Tasks[name].Result
		for _, field := range sortedKeys(result) {
			entry := result[field]
			b.WriteString("(" + pyQuote(field) + ", " + pyQuote(entry.Var) + ", " +
				pyLiteral(entry.Append) + ", " + pyLiteral(entry.Required) + "), ")
		}
		b.WriteString("),")
	}
	if len(agent.Tasks) > 0 {
		b.WriteString("\n    ")
	}
	b.WriteString("}")
	return b.String()
}

// confirmTable is value -> the step that must hear the caller agree to it.
func confirmTable(agent *ir.Agent) string {
	pairs := map[string]any{}
	for _, name := range sortedKeys(agent.Variables) {
		if step := agent.Variables[name].Confirm; step != "" {
			pairs[name] = step
		}
	}
	return pyLiteral(pairs)
}

// dependenciesTable is pre-fetched value -> the values it was looked up from,
// for each one whose confirmation it inherits.
func dependenciesTable(agent *ir.Agent) string {
	reads := map[string][]string{}
	for _, entry := range agent.Prefetch {
		var roots []string
		for _, input := range entry.Inputs {
			if root := ir.PathRoot(input); !slices.Contains(roots, root) {
				roots = append(roots, root)
			}
		}
		for _, pair := range entry.Assign {
			if agent.Variables[pair.Key].ConfirmInherited {
				reads[pair.Key] = roots
			}
		}
	}
	entries := make([]string, 0, len(reads))
	for _, name := range sortedKeys(reads) {
		entries = append(entries, pyQuote(name)+": ("+pyTuple(reads[name])+")")
	}
	return "{" + strings.Join(entries, ", ") + "}"
}

// PydanticImports is the `from pydantic import ...` line the modules draw on.
// A superset is fine: each module keeps only the names it uses.
func PydanticImports(needsField bool, typed *TypedStateBlock) string {
	var names []string
	if typed != nil {
		names = append(names, "BaseModel", "ConfigDict", "Field", "PrivateAttr", "ValidationError",
			"TypeAdapter", "ValidationInfo", "create_model", "field_validator")
	}
	if needsField {
		names = append(names, "Field")
	}
	slices.Sort(names)
	return strings.Join(slices.Compact(names), ", ")
}

// CallStateMethods are the names CallState defines beside the author's fields,
// for the test that holds ir.CallStateMembers to them: the methods
// callStateTemplate defines, and the session id field the LiveKit target adds.
func CallStateMethods() []string {
	names := []string{livekitSessionIDField}
	inClass := false
	for _, line := range strings.Split(callStateSource, "\n") {
		switch {
		case strings.HasPrefix(line, "class CallState("):
			inClass = true
		case inClass && strings.HasPrefix(line, "class "):
			inClass = false
		case inClass && strings.HasPrefix(line, "    def "):
			name, _, _ := strings.Cut(strings.TrimPrefix(line, "    def "), "(")
			if !strings.HasPrefix(name, "_") {
				names = append(names, name)
			}
		}
	}
	slices.Sort(names)
	return names
}

var callStateTemplate = template.Must(template.New("call_state").Parse(callStateSource))

// callStateSource is call_state.py. Shared by both targets, so the save, the
// refusal wording and the prompt rendering cannot differ between them.
const callStateSource = `# --- call state --------------------------------------------------------------
# State is the author's own class, in state.py. CallState adds what one call
# needs around it: the one way a value is saved, which values still wait for the
# caller to confirm them, and how a value reads in a prompt. StepResult checks
# what a step hands back when it finishes. Neither writes a type of its own:
# every type here is read off State's annotations.


class StateRefused(Exception):
    """A value that does not fit its declared type, refused where it enters.

    Carried as an exception rather than a return so the write cannot happen by
    accident: the previous contents stay exactly as they were, and the message
    goes back to the model, which is what lets it correct itself on the next
    turn instead of the step recording something wrong.
    """

    def __init__(self, message: str) -> None:
        """Keep the message the model will be shown.

        Args:
            message: What was wrong with the value, worded for the model.
        """
        super().__init__(message)
        self.message = message

    @classmethod
    def from_error(cls, error: ValidationError, field: str = "") -> StateRefused:
        """Word the first problem Pydantic found the way the model is shown it.

        Args:
            error: What Pydantic refused.
            field: The value the error is about, when its location does not
                already start with it.

        Returns:
            The refusal, as "record.record_id: Field required".
        """
        first = error.errors()[0]
        where = ".".join(str(part) for part in (field, *first["loc"]) if part != "")
        return cls(f"{where}: {first['msg']}")
{{if not .HasState}}


class State(BaseModel):
    """This package has no state.py, so a call keeps no values of its own."""
{{- end}}


class CallState(State):
    """The state of one call: the author's State, and how the call keeps it."""

    # A misspelt name is refused, never saved somewhere nothing reads it. A save
    # validates the batch once, as a whole, so assignment is never checked.
    model_config = ConfigDict(extra="forbid", frozen=False, validate_assignment=False)

    # step -> what its finish saves: (state field, result path, append).
    ASSIGNMENTS: ClassVar[dict[str, tuple[tuple[str, str, bool], ...]]] = {{.Assignments}}
    # step -> its finish fields: (field, state field, append, required).
    RESULTS: ClassVar[dict[str, tuple[tuple[str, str, bool, bool], ...]]] = {{.Results}}
    # value -> the step that must hear the caller agree to it.
    CONFIRM: ClassVar[dict[str, str]] = {{.Confirm}}
    # pre-fetched value -> the values it was looked up from.
    DEPENDENCIES: ClassVar[dict[str, tuple[str, ...]]] = {{.Dependencies}}
    # The values a dispatch may fill when the call starts.
    CALL_START: ClassVar[tuple[str, ...]] = {{.CallStart}}
    # What a value with nothing in it reads as in a prompt.
    EMPTY_TEXT: ClassVar[str] = {{.Empty}}
    # The bound on one rendered value, in characters. The same number the router
    # bounds a template variable by, because this is the same value travelling
    # the same way, and one number cannot be two.
    VALUE_MAX: ClassVar[int] = {{.ValueMax}}

    # Facts about the values, not values: which ones still wait for the caller
    # to confirm them, and which inputs each pre-fetched one was derived from.
    # Private, so no save, prompt or dispatch payload can ever set them.
    _unconfirmed: set[str] = PrivateAttr(default_factory=lambda: set(CallState.CONFIRM))
    _prefetch_provenance: dict[str, tuple[str, ...]] = PrivateAttr(default_factory=dict)
{{- .Members}}

    def initial_value(self, name: str) -> object:
        """Read the value a field starts the call with.

        Args:
            name: Name of the field.

        Returns:
            A fresh copy of the field's default.
        """
        return type(self).model_fields[name].get_default(call_default_factory=True)

    def lookup(self, flat: str) -> tuple[str, object]:
        """Find the value a placeholder names, and the field it belongs to.

        A path is authored {{"{{"}}state.customer.status{{"}}"}} and emitted
        customer__status: one flat name, because the router substitutes flat
        names only. Everything before the first "__" is the field; each "__"
        after it reads one attribute, and is None past an absent link, so a
        field of a record nobody has filled reads as empty and never raises.

        Args:
            flat: The flat placeholder name.

        Returns:
            The field's name and the value found at the end of the path.
        """
        root, _, path = flat.partition("__")
        value = getattr(self, root, None)
        for part in path.split("__") if path else ():
            if value is None:
                break
            value = getattr(value, part, None)
        return root, value

    def plain(self, flat: str) -> Any:
        """Read the value a placeholder names as plain JSON data.

        The one way a value leaves the state for a tool or a prompt. Dumped by
        the field's own serializer, so a date reads as 2026-10-02 and a name
        with an email as "Jo <jo@example.com>", never as a Python object.

        Args:
            flat: The flat placeholder name.

        Returns:
            The value as JSON data, or None past an absent link.
        """
        root, _, path = flat.partition("__")
        value = self.model_dump(mode="json", include={root}).get(root)
        for part in path.split("__") if path else ():
            value = value.get(part) if isinstance(value, dict) else None
        return value

    def is_unconfirmed(self, flat: str) -> bool:
        """Say whether the value a placeholder names still waits for the caller.

        Args:
            flat: The flat placeholder name.

        Returns:
            True while the caller has not agreed to the value.
        """
        return flat.partition("__")[0] in self._unconfirmed

    @classmethod
    def render(cls, state: CallState | None, flat: str, site: str = "") -> str:
        """Render one placeholder the way a prompt reads it.

        Compact JSON for anything structured, never a Python repr. Words for a
        value with no contents, so a step cannot mistake "not yet known" for
        "known to be nothing". A value still waiting for the caller reads as
        empty everywhere but in the step that confirms it.

        Args:
            state: The call's state, or None before the call has begun.
            flat: The flat placeholder name.
            site: The prompt reading it, so the confirming step still sees it.

        Returns:
            The text, cut to the bound when it is longer.
        """
        if state is None:
            return cls.EMPTY_TEXT
        root = flat.partition("__")[0]
        if state.is_unconfirmed(root) and site != "task:" + cls.CONFIRM.get(root, ""):
            return cls.EMPTY_TEXT
        value = state.plain(flat)
        if value is None or value == "":
            return cls.EMPTY_TEXT
        text = value if isinstance(value, str) else to_json(value).decode()
        if len(text) > cls.VALUE_MAX:
            # The length is only knowable here, at run time, so this cannot be
            # a compile-time refusal. What it must not be is silent: a shortened
            # value is a value the model reads as complete.
            logger.warning(
                f"declared state: {root} rendered {len(text)} characters and is shortened to "
                f"{cls.VALUE_MAX}; a value this long also stops the prompt being cached"
            )
            text = text[: cls.VALUE_MAX]
        return text

    def save_result(self, step: str, raw: dict) -> dict:
        """Check a step's finish values, then save what it assigns, all or nothing.

        Args:
            step: Name of the step whose result is being saved.
            raw: The finish arguments as the model or the tool sent them.

        Returns:
            The result as plain data, or the unserved request alone when the
            step could not help.

        Raises:
            StateRefused: If a value does not fit its declared type.
        """
        result = STEP_RESULTS[step].parse(raw)
        if result.unserved_request:
            return {"unserved_request": result.unserved_request}
        pending = {}
        for name, path, append in self.ASSIGNMENTS.get(step, ()):
            # A dotted path walks the result's fields, and is None past any
            # missing link.
            value: object = result
            for part in path.split("."):
                if value is None:
                    break
                value = getattr(value, part, None)
            if append:
                if value is None:
                    continue
                value = _appended(getattr(self, name), value)
            pending[name] = value
        self.save_batch(pending, step=step)
        # Plain data for the framework: one refuses a model in a tool result and
        # drops the whole result, the other cannot serialise one at all.
        return result.model_dump(mode="json")

    def save_batch(self, values: dict, *, step: str | None = None, inputs: list[str] | None = None) -> None:
        """Validate one batch as a whole, then commit it and invalidate what it changed.

        The batch is built into one fresh model, so each value is checked by
        its own field's annotation, and nothing is saved until every value has
        passed. A field the batch leaves out takes its default, which Pydantic
        does not validate, so it never refuses a save of something else.

        Args:
            values: Field names mapped to what to save.
            step: The step saving them, which decides whether it confirms them.
            inputs: Fields a pre-fetch read to produce them, kept as provenance
                so a later change to one invalidates the result.

        Raises:
            StateRefused: If a value does not fit its declared type.
        """
        if not values:
            return
        try:
            checked = type(self).model_validate(values)
        except ValidationError as error:
            raise StateRefused.from_error(error) from None
        pending = {name: getattr(checked, name) for name in values}
        unconfirmed = set(self._unconfirmed)
        provenance = dict(self._prefetch_provenance)
        affected = {name for name, value in pending.items() if getattr(self, name, None) != value}
        # Grow the changed set with everything derived from it, until nothing new joins.
        while more := {name for name, reads in provenance.items() if set(reads) & affected} - affected:
            affected |= more
        invalidated = affected - pending.keys()
        provenance = {name: reads for name, reads in provenance.items() if name not in invalidated}
        for name, value in pending.items():
            if inputs is not None:
                provenance[name] = tuple(inputs)
            else:
                provenance.pop(name, None)
            if name in self.CONFIRM:
                if self.CONFIRM[name] == step and value is not None and value != "":
                    unconfirmed.discard(name)
                else:
                    unconfirmed.add(name)

        def current(name: str) -> object:
            """Read a value as it will stand once this batch lands.

            Args:
                name: Name of the field.

            Returns:
                The pending value, the starting value if it was invalidated,
                else what the state holds.
            """
            return self.initial_value(name) if name in invalidated else pending.get(name, getattr(self, name, None))

        # Dependencies are acyclic: a pre-fetch can only read earlier entries.
        # Each pass reads what the previous one settled, so this runs to a fixed
        # point.
        for _ in range(len(self.DEPENDENCIES) + 1):
            before = set(unconfirmed)
            for name, reads in self.DEPENDENCIES.items():
                if current(name) is not None and current(name) != "" and all(
                    source not in unconfirmed and current(source) is not None and current(source) != ""
                    for source in reads
                ):
                    unconfirmed.discard(name)
                else:
                    unconfirmed.add(name)
            if before == unconfirmed:
                break
        # An invalidated value goes back to what the call started with: None for
        # a value with no default, empty for a list.
        for name in invalidated:
            setattr(self, name, self.initial_value(name))
        for name, value in pending.items():
            setattr(self, name, value)
        self._unconfirmed = unconfirmed
        self._prefetch_provenance = provenance

    def save_call_start(self, values: dict) -> None:
        """Save the values the dispatch sent, each checked against its type.

        A value that does not fit stops the call before it starts. The dispatch
        is the author's own payload, so a wrong value in it is a defect to fix,
        and a call started anyway would run on a value nobody declared. Nothing
        is saved when one value is refused.

        Args:
            values: The dispatched values, by field name.

        Raises:
            RuntimeError: If a dispatched value does not fit its type.
        """
        try:
            self.save_batch({name: values[name] for name in self.CALL_START if name in values})
        except StateRefused as refused:
            raise RuntimeError(f"call_start: {refused.message}") from None

    def save_fact(self, name: str, value: object) -> bool:
        """Save one fact the carrier sent, unless it does not fit its type.

        A caller who withholds their number arrives as "anonymous", which is not
        a phone number and is nobody's mistake. Ending the call for it would
        hang up on the caller, so a fact that does not fit is logged, left
        unsaved, and treated as a fact that never arrived. Each fact is saved
        on its own, so one odd value never drops the others.

        Args:
            name: The field the fact fills.
            value: The fact as the carrier sent it.

        Returns:
            Whether the fact was saved.
        """
        try:
            self.save_batch({name: value})
        except StateRefused as refused:
            logger.warning(f"call fact {name}: {refused.message}; treated as missing")
            return False
        return True

    def is_confirmed(self, name: str) -> bool:
        """Say whether a value is confirmed right now.

        Both halves matter: a value nobody has agreed to is unconfirmed, and so
        is one that was withdrawn. A group reads this once, as it starts, to
        decide whether the step that confirms it has to run.

        Args:
            name: Name of the field.

        Returns:
            True when the value is filled and nobody has withdrawn its
            confirmation.
        """
        value = getattr(self, name, None)
        return name not in self._unconfirmed and value is not None and value != ""

    def withdraw_confirmation(self, step: str) -> None:
        """Withdraw what this step confirms, because the step is about to run again.

        A step a group may skip cannot be trusted to have confirmed anything
        once it is entered: the caller is correcting the value, or the step is
        running because the confirmation had already lapsed. Values derived
        from a withdrawn one follow it.

        Args:
            step: Name of the step being entered.
        """
        withdrawn = {name for name, owner in self.CONFIRM.items() if owner == step}
        if not withdrawn:
            return
        unconfirmed = self._unconfirmed | withdrawn
        # Dependencies are acyclic, so one pass per entry settles them.
        for _ in range(len(self.DEPENDENCIES) + 1):
            before = set(unconfirmed)
            for name, reads in self.DEPENDENCIES.items():
                if any(source in unconfirmed for source in reads):
                    unconfirmed.add(name)
            if before == unconfirmed:
                break
        self._unconfirmed = unconfirmed
        logger.info("withdrew confirmation on entering " + step)


def _task_status(values: dict) -> dict[str, str]:
    """Say whether a step served the caller or could not.

    Args:
        values: The step's finish values.

    Returns:
        A one-key status dict for the agent that owns the step.
    """
    return {"status": "unserved" if values.get("unserved_request") else "completed"}


def _group_status(results: dict) -> dict[str, str]:
    """Say whether a group of steps served the caller or one of them could not.

    Args:
        results: Each step's finish values, by step name.

    Returns:
        A one-key status dict for the agent that owns the group.
    """
    unserved = any(value.get("unserved_request") for value in results.values())
    return {"status": "unserved" if unserved else "completed"}


def _success_word(value: object) -> str:
    """Spell one result value the way a success pair reads it.

    The pairs come out of YAML as text, so a boolean has to read as the word the
    author wrote rather than as Python's own spelling of it: True never matches
    the word true, and the step would silently never end.

    Args:
        value: One field of a tool result.

    Returns:
        The value as text, with booleans as true or false and none as empty.
    """
    if isinstance(value, bool):
        return "true" if value else "false"
    return "" if value is None else str(value)


def _terminal_success(result: object, success: dict) -> bool:
    """Say whether a tool result means this step is done.

    Every field is required and its value has to be one of the listed ones.
    Anything else is an ordinary result and goes back to the model, which is
    what keeps a failed booking a conversation rather than a saved one.

    Args:
        result: What the tool returned.
        success: Each required result field mapped to the values that count.

    Returns:
        True when every required field holds one of its listed values.
    """
    if not isinstance(result, dict):
        return False
    return all(_success_word(result.get(name)) in values for name, values in success.items())


def _appended(entries: list, value: object) -> list:
    """Add one entry onto a list, unless it is already on it.

    A step re-entered mid-call can read a value through an explicit prompt
    reference and hand it straight back, which is not a second thing happening.
    One live call entered the booking step four times and finished three of
    them immediately, each with the same appointment, so one booking became
    four entries. An object carries its own identity, so an identical one is
    the same thing reported twice. A plain value is not: two bookings really do
    give two reasons of "create_booking", and both count.

    Args:
        entries: The list as it stands.
        value: The entry to add.

    Returns:
        A new list with the entry on the end, or the same entries.
    """
    if isinstance(value, (BaseModel, dict, list)) and value in entries:
        return list(entries)
    return [*entries, value]


def _field_type(name: str) -> Any:
    """The annotation one CallState field validates against, constraints included.

    Args:
        name: Name of the field.

    Returns:
        The annotation, wrapped back in Annotated when the field carries
        constraints, because FieldInfo keeps those apart from the annotation.
    """
    info = CallState.model_fields[name]
    return Annotated[(info.annotation, *info.metadata)] if info.metadata else info.annotation


def _item_type(annotation: Any) -> Any:
    """The entry type of a list annotation, through Annotated and | None.

    Args:
        annotation: A list annotation, possibly wrapped.

    Returns:
        The annotation one entry is checked against.
    """
    for candidate in (annotation, *get_args(annotation)):
        if get_origin(candidate) is list:
            return get_args(candidate)[0]
    return annotation


def _accepts_blank(annotation: Any) -> bool:
    """Say whether an empty string is a legal value of a type.

    Args:
        annotation: The annotation a step result field is checked against.

    Returns:
        True for text, where empty is a value like any other; False for a date,
        a number, a closed set or a model, where empty can only mean no value.
    """
    try:
        TypeAdapter(annotation).validate_python("")
    except ValidationError:
        return False
    return True


# Words for the keywords a provider refuses in a tool schema. One target's
# strict converter keeps format and pattern and the provider rejects both, so
# each travels as prose instead: the model still reads the shape it is asked
# for, and the value is still checked where it enters the state.
_FORMAT_WORDS = {
    "date": "a date written YYYY-MM-DD",
    "date-time": "a date and time written YYYY-MM-DDTHH:MM",
    "email": "an email address",
    "name-email": "a name and an email address, written Jo Bloggs <jo@example.com>",
    "phone": "a phone number with its country code, written +34600111222",
    "time": "a time of day written HH:MM on the 24-hour clock",
}


def _tool_schema(node: object, defs: dict) -> object:
    """Shape one schema node for a provider: refs inlined, format and pattern as words.

    Pydantic emits $defs and a $ref for a model that holds another model. A
    nested $ref the model cannot resolve is still accepted by the provider, and
    the model then invents the nested field names, so every ref is inlined.

    Args:
        node: A schema node, or a list or scalar found inside one.
        defs: The schema's $defs.

    Returns:
        The node with no $ref, no format and no pattern left in it.
    """
    if isinstance(node, list):
        return [_tool_schema(item, defs) for item in node]
    if not isinstance(node, dict):
        return node
    target = node.get("$ref")
    if isinstance(target, str) and target.startswith("#/$defs/"):
        found = _tool_schema(defs.get(target.rsplit("/", 1)[1], {}), defs)
        siblings = {key: _tool_schema(value, defs) for key, value in node.items() if key != "$ref"}
        return {**found, **siblings} if isinstance(found, dict) else found
    out = {key: _tool_schema(value, defs) for key, value in node.items() if key not in ("format", "pattern", "$defs")}
    words = []
    if node.get("format") in _FORMAT_WORDS:
        words.append(_FORMAT_WORDS[node["format"]])
    if isinstance(node.get("pattern"), str):
        words.append("text matching " + node["pattern"])
    if words:
        sentences = [str(out.get("description", "")), "Expected " + "; ".join(words) + "."]
        out["description"] = " ".join(sentence for sentence in sentences if sentence)
    return out


class StepResult(BaseModel):
    """What one step hands back when it finishes.

    Each step has its own subclass, built from CallState's own field types, so
    the model is asked for exactly what the save will accept. Every field is
    optional in the schema, so a step that cannot serve the caller never has to
    invent values; a served finish is then held to REQUIRED.
    """

    # A terminal tool's result carries more than the step assigns. Kept, so the
    # result handed back is the whole of what came in.
    model_config = ConfigDict(extra="allow")
    # Fields a served finish must carry: the whole values the step assigns,
    # less the ones its assign: marks with ?.
    REQUIRED: ClassVar[frozenset[str]] = frozenset()
    # Fields whose type takes an empty string as a value, so it is kept.
    BLANK_OK: ClassVar[frozenset[str]] = frozenset()

    unserved_request: str = Field("", description={{.Unserved}})

    @field_validator("*", mode="before")
    @classmethod
    def _blank_is_absent(cls, value: object, info: ValidationInfo) -> object:
        """Read an empty string as no value, where the type has no empty value.

        Empty is what the model sends for a field it has nothing for. Refusing
        it as a wrong value deadlocked a live call: the model had nothing else
        to send, so every retry was refused the same way. Text keeps it, because
        an empty name is what a tool returns for a customer it has no name for.

        Args:
            value: The raw value.
            info: Which field it is.

        Returns:
            None for an empty string, else the value; an empty request for none.
        """
        if info.field_name == "unserved_request":
            # Nothing to report may arrive as null, and reads as no request.
            return "" if value is None else value
        if value == "" and info.field_name not in cls.BLANK_OK:
            return None
        return value

    @classmethod
    def for_step(cls, step: str) -> type[StepResult]:
        """Build the result model for one step from CallState's field types.

        Args:
            step: Name of the step.

        Returns:
            The step's own subclass.
        """
        fields: dict[str, Any] = {}
        required = set()
        blank_ok = set()
        for field, name, append, needed in CallState.RESULTS.get(step, ()):
            annotation = _item_type(_field_type(name)) if append else _field_type(name)
            description = CallState.model_fields[name].description
            fields[field] = (annotation | None, Field(None, description=description))
            if needed:
                required.add(field)
            if _accepts_blank(annotation):
                blank_ok.add(field)
        title = "".join(part.title() for part in step.split("_")) + "Result"
        model = create_model(title, __base__=cls, __module__=__name__, **fields)
        model.REQUIRED = frozenset(required)
        model.BLANK_OK = frozenset(blank_ok)
        return model

    @classmethod
    def parse(cls, raw: dict) -> Self:
        """Validate a finish where it enters: the one place a step's result is checked.

        Args:
            raw: The finish arguments as sent.

        Returns:
            The validated result.

        Raises:
            StateRefused: If a field does not fit its type, or a required one is
                missing.
        """
        if raw.get("unserved_request"):
            return cls(unserved_request=str(raw["unserved_request"]))
        try:
            result = cls.model_validate(raw)
        except ValidationError as error:
            raise StateRefused.from_error(error) from None
        missing = next((name for name in sorted(cls.REQUIRED) if getattr(result, name) is None), None)
        if missing is not None:
            raise StateRefused(f"{missing}: Field required")
        return result

    @classmethod
    def tool_parameters(cls) -> dict:
        """The parameters schema the model is sent for this step's finish.

        Returns:
            A JSON Schema object with one property per field and every $ref
            inlined. None of it is required, for the reason the class says.
        """
        schema = cls.model_json_schema()
        defs = schema.get("$defs", {})
        properties = {name: _tool_schema(node, defs) for name, node in schema.get("properties", {}).items()}
        return {"type": "object", "properties": properties, "required": []}

    @classmethod
    def retain(cls, args: dict, retained: dict) -> dict:
        """Put a tool's own values back into the model's finish arguments.

        Reached only when a save was refused and the model is repairing it. A
        field the tool returned validly is the authoritative one: the model
        cannot manufacture a booking reference, and a repair that retyped one
        would record a booking nobody made. A retained value that does not
        validate is left to the model, because refusing here would leave the
        step with no way out.

        Args:
            args: The model's finish arguments.
            retained: The values the tool returned.

        Returns:
            The arguments with each validly retained value restored.
        """
        out = dict(args)
        for name in cls.model_fields:
            if name not in retained or name == "unserved_request":
                continue
            try:
                cls.model_validate({name: retained[name]})
            except ValidationError:
                continue
            out[name] = retained[name]
        return out


# One result model per step, built once at import.
STEP_RESULTS = {step: StepResult.for_step(step) for step in CallState.RESULTS}
`

// callStateTypingNames are the typing names call_state.py reads.
var callStateTypingNames = []string{"Annotated", "Any", "ClassVar", "Self", "get_args", "get_origin"}

// withTypingNames adds names to a `from typing import` list, in the order
// ruff's import sorter keeps: classes first, then functions, each sorted.
func withTypingNames(current string, names ...string) string {
	all := slices.Clone(names)
	if current != "" {
		all = append(all, strings.Split(current, ", ")...)
	}
	slices.SortFunc(all, func(a, b string) int {
		lowerA, lowerB := a[:1] == strings.ToLower(a[:1]), b[:1] == strings.ToLower(b[:1])
		if lowerA != lowerB {
			if lowerA {
				return 1
			}
			return -1
		}
		return strings.Compare(a, b)
	})
	return strings.Join(slices.Compact(all), ", ")
}

// Deps are the packages the project installs for state.py, beyond Pydantic,
// which both frameworks already depend on: email-validator for an email type,
// and pydantic-extra-types with the extra each imported module needs.
func (b *TypedStateBlock) Deps() []string {
	if b == nil {
		return nil
	}
	var deps, extras []string
	if b.NeedsEmailValidator {
		deps = append(deps, "email-validator>=2.2,<3")
	}
	for _, module := range b.ExtraTypes {
		extra := "pycountry"
		if module == "phone_numbers" {
			extra = "phonenumbers"
		}
		if !slices.Contains(extras, extra) {
			extras = append(extras, extra)
		}
	}
	if len(extras) > 0 {
		slices.Sort(extras)
		deps = append(deps, "pydantic-extra-types["+strings.Join(extras, ",")+"]>=2.11,<3")
	}
	return deps
}
