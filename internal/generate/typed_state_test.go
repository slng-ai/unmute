package generate

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// typedStateMarkers is every distinctive line the shared declared-state code emits.
// Exhaustive on purpose: the byte-identical gate below asserts that a package
// declaring nothing structured carries none of them, so a marker missing from
// this list is a hole in that gate.
var typedStateMarkers = []string{
	"# --- declared state",
	"class _StateRefused",
	"def _typed(",
	"def _plain(",
	"def _typed_result(",
	"def _state_text(",
	"def _state_lookup(",
	"_FINISH_TYPES",
	"_STATE_STRUCTURED",
	"_STATE_EMPTY",
	"TypeAdapter(",
	"AfterValidator(",
	"BaseModel",
	"_SHAPE_PHONE",
	"_SHAPE_DATE",
	"_SHAPE_TIME",
	"_SHAPE_ID",
	"_shape_emailstr",
	"class NameEmail",
	"validate_email",
	"email_validator",
	"email-validator",
	"model_validator",
	"field(default_factory=list)",
}

func loadTypedState(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "typed_state"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func loadShapeless(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "simple-prompt"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// emitted returns the module both drivers write, so a test asserts on the same
// question twice rather than once per target.
func emitted(t *testing.T, agent *ir.Agent, provider ir.Provider) string {
	t.Helper()
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatalf("generate %s: %v", provider, err)
	}
	if provider == ir.ProviderLiveKit {
		return artifactFile(t, artifact, "agent.py")
	}
	return artifactFile(t, artifact, "bot.py")
}

// TestTypedStateEmitsNothingForAPackageThatDeclaresNone is FR-015, and it is
// the only real protection every shipped example has from this feature.
//
// A package declaring no shape and no structured type must emit exactly what it
// emitted before, so the block, its constants, its imports and the list default
// appear only when something is authored. The golden files hold the byte
// comparison; this holds the reason a byte would change.
func TestTypedStateEmitsNothingForAPackageThatDeclaresNone(t *testing.T) {
	agent := loadShapeless(t)
	block, err := TypedState(agent)
	if err != nil {
		t.Fatal(err)
	}
	if block.Source != "" {
		t.Errorf("a package declaring nothing structured rendered a block:\n%s", block.Source)
	}
	if len(block.Structured) != 0 {
		t.Errorf("a package declaring nothing structured named %v as structured", block.Structured)
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		for _, marker := range typedStateMarkers {
			if strings.Contains(module, marker) {
				t.Errorf("%s emits %q for a package declaring nothing structured", provider, marker)
			}
		}
		// The import lines the block needs must not appear either: an unused
		// import is a byte that changed, and on this tree it is also a lint
		// failure in the emitted project.
		for _, unwanted := range []string{"from pydantic import AfterValidator", "dataclass, field"} {
			if strings.Contains(module, unwanted) {
				t.Errorf("%s emits %q for a package declaring nothing structured", provider, unwanted)
			}
		}
	}
}

// TestTypedStateEmitsTheBlockWhenAuthored is the other half, and it is what
// stops the gate above passing because nothing is ever emitted.
func TestTypedStateEmitsTheBlockWhenAuthored(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		for _, want := range []string{
			"# --- declared state",
			"class Appointment(BaseModel):",
			"class _StateRefused(Exception):",
			"def _typed_result(step, values):",
			"_STATE_STRUCTURED = {",
			"Phone = Annotated[\n    str,\n    AfterValidator(_shape_phone),\n",
			"field(default_factory=list)",
		} {
			if !strings.Contains(module, want) {
				t.Errorf("%s does not emit %q", provider, want)
			}
		}
		// The shape's own fields, in declaration order, and the description the
		// model reads.
		if !strings.Contains(module, "scheduled_date: Date") {
			t.Errorf("%s does not annotate scheduled_date with its shaped type", provider)
		}
		// A text type nothing declares emits no alias and no pattern.
		if strings.Contains(module, "_SHAPE_ID") {
			t.Errorf("%s emits the Id alias, which this package never declares", provider)
		}
	}
}

// TestTypedStatePutsNoShapeKeywordInAnEmittedSchema is research section 20,
// and it fails in no other check.
//
// A `format` or a `pattern` in the schema the model is sent survives one
// target's strict converter, which is that target's default, and the provider
// rejects it. So it passes every local check and fails on the first real call.
// The shape lives in an AfterValidator, which contributes nothing to
// model_json_schema(), and that is what this holds.
func TestTypedStatePutsNoShapeKeywordInAnEmittedSchema(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		// Comment lines are dropped first, the way the colour-literal gate reads
		// through the AST: the block's own comment explains why a pattern= is
		// never written, and a gate that could not tell an explanation from an
		// instance would forbid saying so.
		module := withoutComments(emitted(t, agent, provider))
		for _, keyword := range []string{`"format"`, `"pattern"`, "StringConstraints", "pattern=", "format="} {
			if strings.Contains(module, keyword) {
				t.Errorf("%s emits %s; one target's strict converter keeps it and the provider rejects it",
					provider, keyword)
			}
		}
	}
	// And the patterns themselves are raw-string safe, because a pattern that
	// needs escaping would compile and then match the wrong thing.
	for kind, pattern := range ShapedPatterns() {
		if !RawStringSafe(pattern) {
			t.Errorf("the %s pattern %q cannot be written as a Python raw string", kind, pattern)
		}
	}
}

// Pydantic has its own EmailStr and NameEmail, and reaching for either is the
// one mistake the gate above cannot catch.
//
// pydantic.EmailStr publishes `{"type": "string", "format": "email"}` and
// NameEmail publishes `format: name-email`, which is exactly the keyword the
// shaped-text design exists to keep off the wire. The emitted _schema() helper
// resolves $ref and $defs and strips no keyword, and the gate above greps the
// module's own source, so a Pydantic-native import would read clean there and
// still send `format` to the provider. Only the first real call would say so.
//
// The types this compiler emits carry the same names on purpose, because those
// are the names an author already knows. That is what makes the mistake easy,
// and it is why this refuses the import by name rather than the annotation.
func TestNoEmittedModuleImportsPydanticsOwnEmailTypes(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := withoutComments(emitted(t, agent, provider))
		for _, line := range strings.Split(module, "\n") {
			if !strings.HasPrefix(line, "from pydantic import ") {
				continue
			}
			for _, name := range []string{"EmailStr", "NameEmail"} {
				if !slices.Contains(strings.Split(strings.TrimPrefix(line, "from pydantic import "), ", "), name) {
					continue
				}
				t.Errorf("%s imports Pydantic's own %s: it publishes a format keyword the "+
					"provider rejects, and no local check would say so. Emit the alias or the "+
					"class this compiler generates instead", provider, name)
			}
		}
		// The generated ones are what the module has to be using.
		if !strings.Contains(module, "AfterValidator(_shape_emailstr)") {
			t.Errorf("%s does not emit this compiler's own EmailStr alias", provider)
		}
		if !strings.Contains(module, "class NameEmail(BaseModel):") {
			t.Errorf("%s does not emit this compiler's own NameEmail class", provider)
		}
	}
}

// Every shaped kind carries exactly one check: a pattern the shared body wraps,
// or a whole body for something no pattern can do.
//
// Neither is the failure worth a gate. shapedPatterns is a map, so a kind with
// no row reads a zero value: the module would emit `re.compile(r"")`, which
// matches everything, and a refusal saying only "expected " with no format after
// it. Both is a kind whose emitted check depends on which branch the emitter
// happens to test first.
func TestEveryShapedKindCarriesOneCheck(t *testing.T) {
	kinds := ir.ShapedTextOrder()
	if len(kinds) == 0 {
		t.Fatal("no shaped kinds, so this gate proves nothing")
	}
	patterns := ShapedPatterns()
	var withPattern, withBody int
	for _, kind := range kinds {
		pattern, hasPattern := patterns[kind]
		body := ShapedBody(kind)
		switch {
		case hasPattern && body != "":
			t.Errorf("%s carries both a pattern and a body; the emitter would use one and the "+
				"other would be a check nobody runs", kind)
		case !hasPattern && body == "":
			t.Errorf("%s carries no check at all: it would emit an empty pattern, which matches "+
				"every value, and a refusal that names no format", kind)
		case hasPattern:
			withPattern++
			if pattern == "" {
				t.Errorf("%s carries an empty pattern", kind)
			}
		default:
			withBody++
			// The refusal token is what carries the one shared phrase into a
			// hand-written body. Without it the body raises a sentence the model
			// was never shown, which is the drift the single phrase prevents.
			if !strings.Contains(body, shapedExpected) {
				t.Errorf("%s carries a body that names no expected format, so its refusal and the "+
					"description the model reads can say different things", kind)
			}
		}
		if ShapedPhrase(kind) == "" {
			t.Errorf("%s tells the model nothing about its format", kind)
		}
	}
	// Both forms have to be exercised, or this gate passes on a tree where one
	// of the two branches is dead.
	if withPattern == 0 || withBody == 0 {
		t.Errorf("%d pattern-checked and %d body-checked kinds; this gate needs both to mean anything",
			withPattern, withBody)
	}
}

// The one thing an email check must never do on the voice path is ask DNS.
//
// email-validator's check_deliverability defaults to true, which sends MX
// queries for the domain. This validator runs where a value enters the state,
// which is inside a turn: a slow or unreachable resolver would hold the caller
// in silence, and it would refuse a real address whose mail server is having a
// bad day. A caller giving an address nothing can post to today is still giving
// the address they have.
//
// Asserted per call site rather than once, because the module has two: the alias
// and the supplied pair's parser.
func TestTheEmittedEmailCheckNeverAsksDNS(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := withoutComments(emitted(t, agent, provider))
		// Counted on the open paren, so the import line, which names the
		// function without calling it, is not one of these.
		calls := strings.Count(module, "validate_email(")
		if calls < 2 {
			t.Fatalf("%s makes %d email checks, want the alias and the pair's parser", provider, calls)
		}
		if got := strings.Count(module, "check_deliverability=False"); got != calls {
			t.Errorf("%s makes %d email checks and %d of them skip the DNS lookup; all of them "+
				"have to, because this runs on the voice path and a resolver that hangs holds the "+
				"caller in silence", provider, calls, got)
		}
	}
}

// TestTypedStateCarriesEveryDeclaredDescription is FR-014, which nothing else
// asserts: a field's description reaches the model, so the model knows what the
// field means without the author repeating it in prose.
func TestTypedStateCarriesEveryDeclaredDescription(t *testing.T) {
	agent := loadTypedState(t)
	var wanted []string
	for _, shape := range agent.Shapes {
		for _, field := range shape.Fields {
			if field.Description != "" {
				wanted = append(wanted, field.Description)
			}
		}
	}
	if len(wanted) == 0 {
		t.Fatal("the fixture declares no field description, so this gate proves nothing")
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		for _, description := range wanted {
			if !strings.Contains(module, description) {
				t.Errorf("%s does not carry the field description %q into the schema", provider, description)
			}
			if !strings.Contains(module, "Field(description=") {
				t.Errorf("%s carries no Field(description=...), so no description reaches the model", provider)
			}
		}
	}
	// A shape's own description reaches the model as the class docstring.
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		if !strings.Contains(emitted(t, agent, provider), `"""One thing being booked, moved or cancelled."""`) {
			t.Errorf("%s drops the shape's own description", provider)
		}
	}
}

// TestTypedStateBlockIsByteIdenticalOnBothTargets is FR-006 where it is
// cheapest to hold: the declared-state code is rendered once, in shapes.go,
// and inserted into both modules verbatim. Rendering it twice is how the two
// targets would drift, and this is what notices.
func TestTypedStateBlockIsByteIdenticalOnBothTargets(t *testing.T) {
	agent := loadTypedState(t)
	block, err := TypedState(agent)
	if err != nil {
		t.Fatal(err)
	}
	if block.Source == "" {
		t.Fatal("the fixture rendered no block, so this gate proves nothing")
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		if !strings.Contains(emitted(t, agent, provider), block.Source) {
			t.Errorf("%s does not carry the rendered block verbatim, so the two targets can differ", provider)
		}
	}
}

// withoutComments drops every full-line Python comment, so a gate over emitted
// code does not fire on emitted prose about that code.
func withoutComments(module string) string {
	var kept []string
	for _, line := range strings.Split(module, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// TestLiveKitFinishParameterIsTheGeneratedClass is research section 13, and it
// is the silent gap this closes.
//
// resultPyType returned "dict" for anything nested, and a bare dict annotation
// carries no field names, no types and no descriptions, so the pydantic
// conversion had nothing to turn into properties: the model was asked for an
// object and told nothing about what belongs in it. Nothing failed. It just did
// not work.
func TestLiveKitFinishParameterIsTheGeneratedClass(t *testing.T) {
	agent := loadTypedState(t)
	module := emitted(t, agent, ir.ProviderLiveKit)
	finish := functionBody(t, module, "    async def finish(")
	if finish == "" {
		t.Fatal("no finish handler emitted")
	}
	if !strings.Contains(module, "appointment: Annotated[Appointment | None,") {
		t.Errorf("the finish parameter for a shaped result is not the generated class:\n%s", finish)
	}
	// And no bare dict anywhere a shaped result is annotated.
	for _, forbidden := range []string{"appointment: dict", "appointment: Any", "appointment: object"} {
		if strings.Contains(module, forbidden) {
			t.Errorf("livekit annotates a shaped result as %q, which tells the model nothing", forbidden)
		}
	}
	// A Literal result field keeps its closed set on the parameter too, so the
	// model is told what it may hand back.
	if !strings.Contains(module, `reason: Annotated[Literal["create_booking", "cancel_booking"] | None,`) {
		t.Errorf("the finish parameter for a Literal result is not the closed set:\n%s", finish)
	}
}

// TestDottedAssignWalksIntoAShapedResultAtEmission is gap 1 of the scoped
// variables feature, proven at the emitted seam: a step's result is a plain
// dict by the time either driver assigns from it, because _typed_result's
// _plain (shapes.go) has already dumped every declared shape out of its
// Pydantic model, nested shapes included. So a dotted assign field walks a
// chain of dict .get() calls rather than a single subscript, every link but the
// last wrapped in `or {}` so an absent or null parent reads as None rather than
// raising.
//
// A bare (undotted) field keeps the single subscript this always rendered:
// TestLiveKitV1SingleTaskDelegate already holds that byte for byte, so this
// only adds the dotted case.
func TestDottedAssignWalksIntoAShapedResultAtEmission(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	assignedTask := agent.Tasks["find_slot"]
	assignedTask.Assign = []ir.AssignTo{{Var: "caller_phone", Field: "appointment.scheduled_date"}}
	agent.Tasks["find_slot"] = assignedTask
	agent.Controls["do_find"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Task: "find_slot",
		When: "The caller only wants to check for a slot, not book yet.",
	}
	def := agent.Agents["reservations"]
	def.Tools = append(def.Tools, "do_find")
	agent.Agents["reservations"] = def

	const want = `("caller_phone", "appointment.scheduled_date", False)`
	livekit, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate livekit: %v", err)
	}
	if got := artifactFile(t, livekit, "agent.py"); !strings.Contains(got, want) || !strings.Contains(got, `for part in path.split("."):`) {
		t.Errorf("livekit does not walk the dotted assign path:\n%s", got)
	}

	pipecat, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatalf("generate pipecat: %v", err)
	}
	if got := artifactFile(t, pipecat, "bot.py"); !strings.Contains(got, want) || !strings.Contains(got, `for part in path.split("."):`) {
		t.Errorf("pipecat does not walk the dotted assign path:\n%s", got)
	}
}

// TestTwoAppendedEntriesAreBothRecorded is FR-009a and SC-006 at the emitted
// seam: an append adds an entry, and the list it adds to starts empty, so a
// second call of the same step cannot overwrite the first.
//
// One overwriting the other is what a replace would do, and it is what the
// authored `+` exists to prevent. Driven for real, over a whole conversation,
// by the smoke test.
func TestTwoAppendedEntriesAreBothRecorded(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		for _, want := range []string{
			// Both authored append destinations reach the shared staged writer.
			`("appointments", "appointment", True)`,
			`("caller_reason", "reason", True)`,
			"_append_entry(entries, value)",
			// And the list is there to append to before the first step runs.
			"appointments: list[Appointment] = field(default_factory=list)",
		} {
			if !strings.Contains(module, want) {
				t.Errorf("%s does not emit %q, so the second entry replaces the first", provider, want)
			}
		}
		// The replace form must be gone for the appended values: one line of
		// each, and neither is an assignment.
		for _, forbidden := range []string{".appointments = result[", ".appointments = self._"} {
			if strings.Contains(module, forbidden) {
				t.Errorf("%s still replaces appointments, so a caller booking twice ends with one: %q",
					provider, forbidden)
			}
		}
	}
	// A shared mutable default is one call's state leaking into the next, which
	// is why the list arrives through a factory rather than as a literal.
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		if strings.Contains(emitted(t, agent, provider), "appointments: list[Appointment] = []") {
			t.Errorf("%s shares one list between calls", provider)
		}
	}
}

// TestAValueOutsideALiteralSetIsRefusedWhereItEnters is FR-004, FR-013a and
// SC-005, none of which the validation mechanism alone delivers.
//
// Three separate claims, and the third is the one that is easy to lose: the
// value is refused where it enters, the previous contents survive, and the
// message names the field and lists what was allowed. Driven for real by the
// smoke test; this holds the branch that makes it possible.
func TestAValueOutsideALiteralSetIsRefusedWhereItEnters(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		// Refused where it enters: the validating call comes before anything is
		// recorded, on both targets.
		validate := strings.Index(module, "_typed_result(")
		if validate < 0 {
			t.Fatalf("%s validates no finish argument, so a value outside a declared set enters the state",
				provider)
		}
		record := recordIndex(t, module, provider)
		if record < validate {
			t.Errorf("%s records the result before validating it, so a refused value is already in the state",
				provider)
		}
		// The previous contents survive, because the refusal is an exception and
		// the record is the statement after it.
		if !strings.Contains(module, "except _StateRefused as refused:") {
			t.Errorf("%s does not catch the refusal, so a bad value ends the step rather than the turn", provider)
		}
		// The message names the field and what was allowed. The field comes from
		// the shared helper's own naming and the allowed set from Pydantic's own
		// literal message, which lists the entries.
		if !strings.Contains(module, "refused.message") {
			t.Errorf("%s discards the refusal message, so the model is never told which field or what was allowed",
				provider)
		}
		if !strings.Contains(module, "Ask again, then call finish with a value that fits.") {
			t.Errorf("%s does not tell the model what to do next after a refusal", provider)
		}
	}
	// And the shared helper is what names the field and carries Pydantic's own
	// message, which for a Literal lists every allowed entry.
	block, err := TypedState(agent)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`named = f"{field}.{where}" if where else field`,
		`raise _StateRefused(f"{named}: {first[`,
	} {
		if !strings.Contains(block.Source, want) {
			t.Errorf("the shared refusal does not name the field: %q missing", want)
		}
	}
}

// recordIndex is where a target writes the step's result into the state, so a
// test can say the validation came first.
func recordIndex(t *testing.T, module string, provider ir.Provider) int {
	t.Helper()
	marker := "self.complete(_values)"
	if provider == ir.ProviderPipecat {
		marker = "_results[\"book\"] = _values"
	}
	at := strings.Index(module, marker)
	if at < 0 {
		t.Fatalf("%s emits no %q, so this test cannot say when the result is recorded", provider, marker)
	}
	return at
}

// TestPipecatFinishSchemaResolvesEveryRef is the gate under the one thing no
// unit test could settle, now that a real request has settled it.
//
// Pydantic emits $defs and a $ref for a shape that contains another shape.
// Measured against the provider three ways: this target nests the schema inside
// one tool property and sends no strict flag, and a $ref there comes back 200
// with the model inventing field names for the nested object, so every result
// would be refused where it entered. The refs inlined, it fills the shape's own
// fields. The other target hoists its $defs to the parameters root and sends
// strict on, which works, and a $defs anywhere but that root is a 400 naming
// the pointer.
//
// So the emitted schema goes through the resolver, and this is what notices if
// it stops.
func TestPipecatFinishSchemaResolvesEveryRef(t *testing.T) {
	agent := loadTypedState(t)
	module := emitted(t, agent, ir.ProviderPipecat)
	// One call, and it is the resolver's own. A second is a schema going out
	// with its refs unresolved.
	if got := strings.Count(module, ".json_schema()"); got != 1 {
		t.Errorf("pipecat calls json_schema() %d times, want 1 (the resolver's own): a $ref inside one tool "+
			"property is a 200 the model answers with invented field names", got)
	}
	if !strings.Contains(module, "_schema(TypeAdapter(") {
		t.Errorf("pipecat's finish schema does not go through the resolver:\n%s", module)
	}
	block, err := TypedState(agent)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"def _schema(adapter):",
		`defs = schema.pop("$defs", {})`,
		`target = node.get("$ref")`,
		"siblings = {key: value for key, value in node.items()",
	} {
		if !strings.Contains(block.Source, want) {
			t.Errorf("the resolver is incomplete: %q missing", want)
		}
	}
	// The sibling keys survive the resolution. A `$ref` beside a description is
	// how a nullable nested field arrives, and dropping the description would
	// take away the one thing telling the model what the field is.
	if !strings.Contains(block.Source, "{**resolve(found), **siblings}") {
		t.Error("the resolver drops the keys beside a $ref, so a nested field loses its description")
	}
}

// TestAnAbsentEntryAppendsNothing is the case a step that concluded nothing
// needs, and it is the one an append would otherwise force a model to invent.
//
// A caller who asks about a booking and then changes their mind leaves the step
// with nothing to add. Without this the result field would have to be required,
// so the model would produce an appointment to have something to hand back, and
// the state would record a booking nobody made.
func TestAnAbsentEntryAppendsNothing(t *testing.T) {
	agent := loadExample(t, "salon-concierge-v2")
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		appends := strings.Count(module, "_append_entry(")
		if appends == 0 {
			t.Fatalf("%s appends nothing to appointments, so this gate proves nothing", provider)
		}
		// The guard is in the helper rather than at each site, so it cannot be
		// present at one append and missing at the next.
		if !strings.Contains(module, "def _append_entry(entries, value):") {
			t.Errorf("%s emits %d appends and no _append_entry, so an absent entry appends None and the "+
				"state holds a booking nobody made", provider, appends)
		}
		for _, want := range []string{
			"    if value is None:\n        return\n",
			"    if isinstance(value, (dict, list)) and value in entries:\n        return\n",
		} {
			if !strings.Contains(module, want) {
				t.Errorf("%s does not emit %q in _append_entry", provider, want)
			}
		}
		// And no append reaches the list without going through it.
		if strings.Contains(module, ".appointments.append(") || strings.Contains(module, ".caller_reason.append(") {
			t.Errorf("%s appends straight onto a declared list, so a step re-entered mid-call adds the "+
				"entry it read through an explicit prompt reference", provider)
		}
	}
	// And the type is what makes it legal: the element type with its
	// nullability dropped is what an append is checked against.
	booking, ok := agent.Controls["manage_booking"].(*ir.Delegate)
	if !ok {
		t.Fatalf("manage_booking = %#v, want a delegate", agent.Controls["manage_booking"])
	}
	field := agent.Tasks[booking.Task].Result["appointment"]
	if field.Shape == nil || !field.Shape.Optional {
		t.Errorf("the booking step's appointment result is %v, want one that may be absent", field.Shape)
	}
}

// TestShapedTextAcceptsNoValueYet holds the one loosening in the shape checks,
// and the reason it is not a hole.
//
// Empty is not a wrong value, it is no value yet: it is what a declared
// variable holds before anything fills it, what an empty reference renders as
// words, and what a tool hands back for a field it could not fill. Refusing it
// deadlocked a live call on both targets and did so differently, which is why
// the assertion is on the emitted text rather than on one framework's
// behaviour: LiveKit logged a generic "error parsing arguments for finish" with
// no field and no expectation, while Pipecat's refusal reached the model, which
// asked the caller out loud for an identifier that no tool in the package
// returns. The model had nothing else to send, so every retry was refused the
// same way and the step never finished.
//
// A wrong value is still refused. That half is proven by
// TestAValueOutsideALiteralSetIsRefusedWhereItEnters and, on a running module,
// by the L4 smoke.
func TestShapedTextAcceptsNoValueYet(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		source := emitted(t, agent, provider)
		checks := strings.Count(source, "def _shape_")
		if checks == 0 {
			t.Fatalf("%s emits no shaped-text check, so this gate proves nothing", provider)
		}
		// Two forms let empty through, one per kind of check: a pattern-checked
		// type only matches a value it has, and a library-checked one returns
		// before it calls out. Counting one form alone said 3 of 4 the day
		// EmailStr arrived, so the count is over both and the requirement is
		// still that every emitted check carries one of them.
		passes := strings.Count(source, "if value and not _SHAPE_") +
			strings.Count(source, "if not value:\n        return value")
		if passes != checks {
			t.Errorf("%s emits %d shaped-text checks and %d of them pass an empty value through; "+
				"all of them have to, because empty is how a declared value says nothing yet and the "+
				"model has nothing else to send for a field no tool fills", provider, checks, passes)
		}
		if strings.Contains(source, "if not _SHAPE_") {
			t.Errorf("%s refuses an empty shaped value; that is what deadlocked a live call on both targets", provider)
		}
		// The supplied pair is the other place a value can enter, and it takes
		// the empty string the same way rather than refusing it.
		if !strings.Contains(source, `if not value.strip():`) {
			t.Errorf("%s emits a NameEmail parser that refuses an empty string; a declared pair with "+
				"nothing in it yet has to validate, or every prompt naming it raises instead of "+
				"rendering the words a missing value renders", provider)
		}
	}
}

// A shaped text type is the one type whose value the model has to spell a
// particular way, and the only keyword that can tell it so is the description:
// a `format` or a `pattern` is stripped or refused (see the gate above). Left
// off, the format was learned from a refusal mid-call, which is a wasted model
// round trip on every value the prompt spells one way and the type another. A
// live call spent one on `"11:30 AM"` against a `Time`.
func TestShapedTextTellsTheModelItsFormat(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		source := emitted(t, agent, provider)
		// The shared list, not a copy: a type this loop does not visit is a type
		// whose description nobody checks, and the copy that used to live here
		// would have gone on naming four.
		for _, kind := range ir.ShapedTextOrder() {
			alias := string(kind) + " = Annotated["
			if !strings.Contains(source, alias) {
				continue
			}
			phrase := ShapedPhrase(kind)
			if !strings.Contains(source, "AfterValidator(_shape_"+strings.ToLower(string(kind))+"),\n    Field(description="+strconv.Quote(phrase)) {
				t.Errorf("%s emits %s with no description carrying %q, so the model is told nothing "+
					"about the shape until a value is refused mid-call", provider, kind, phrase)
			}
			// One phrase, so the sentence the model is shown and the sentence a
			// refusal prints cannot drift into naming two different formats.
			if !strings.Contains(source, strconv.Quote("expected "+phrase)) {
				t.Errorf("%s refuses a wrong %s with wording that is not %q", provider, kind, "expected "+phrase)
			}
		}
	}
	// A field that documents itself keeps its own sentence, because Pydantic
	// takes the closest description and drops the alias's. So the format is
	// appended to it: documenting a field is otherwise the one thing that stops
	// the model being told what shape the value takes.
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		source := emitted(t, agent, provider)
		want := "The time the caller agreed to. Expected " + ShapedPhrase(ir.ShapedTime) + "."
		if !strings.Contains(source, strconv.Quote(want)) {
			t.Errorf("%s does not emit %q; a described shaped field keeps its own sentence and loses "+
				"the alias's, so the format has to be appended to it", provider, want)
		}
	}
}

// A declared result field the model left out reaches the state as None, not as
// a missing key.
//
// Found by reading the emitted module against the frameworks' own docs. The
// booking prompt tells the model to leave the appointment out when it saved
// nothing, and leaving it out is legal: the field may be absent. Skipping an
// absent field during validation left the key missing from the result, and the
// assignment that reads it by name raised a KeyError inside the finish handler
// on Pipecat, whose framework validates no tool argument of its own. LiveKit
// never saw it, because its own argument parsing fills every declared
// parameter first. So the one target with no framework validation was the one
// that crashed.
func TestAnOmittedResultFieldValidatesRatherThanVanishing(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		source := emitted(t, agent, provider)
		if !strings.Contains(source, "out[name] = _plain(_typed(name, adapter, out.get(name)))") {
			t.Errorf("%s does not put an absent declared field through its adapter; the key stays missing "+
				"and the assignment that reads it raises a KeyError inside the finish handler", provider)
		}
		if strings.Contains(source, "if name in out:") {
			t.Errorf("%s skips validation for a field the model left out, so an absent one vanishes "+
				"instead of validating as None", provider)
		}
	}
}

func TestTaskFinishAllowsEscapeWithoutDomainArguments(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		source := emitted(t, agent, provider)
		if provider == ir.ProviderLiveKit && !regexp.MustCompile(`caller_phone: [^\n]*Phone \| None[^\n]* = None`).MatchString(source) {
			t.Error("finish must accept an unserved exit without inventing a phone number")
		}
		if !strings.Contains(source, `values.get("unserved_request")`) {
			t.Error("unserved must bypass validation and save no domain values")
		}
	}
}

// The email checker is the one thing the declared-state block reaches for that
// is neither stdlib nor Pydantic, so three facts about each emitted project have
// to agree: the module imports it, the module calls it, and the project's
// pyproject.toml asks for it.
//
// Each disagreement is its own failure and none of them is visible locally. An
// import with no dependency is an ImportError at worker startup, which the
// operator sees as an agent that never answers. A dependency with no import is a
// package every image installs for nothing. And an import with no use fails the
// ruff gate the emitted README tells the operator to run.
//
// Both targets, and a package with the types beside one without, so a driver
// that simply stopped emitting the import could not pass this. Modelled on
// TestPipecatHTTPXImportMatchesItsUseAndItsDependency, which holds the same
// three-way agreement for httpx.
func TestEmailValidatorImportMatchesItsUseAndItsDependency(t *testing.T) {
	withTypes, withoutTypes := false, false
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		for _, pkg := range []struct {
			name  string
			agent func(*testing.T) *ir.Agent
		}{
			{"typed_state", loadTypedState},
			{"simple-prompt", loadShapeless},
		} {
			t.Run(string(provider)+"/"+pkg.name, func(t *testing.T) {
				agent := pkg.agent(t)
				artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
				if err != nil {
					t.Fatalf("generate: %v", err)
				}
				name := "bot.py"
				if provider == ir.ProviderLiveKit {
					name = "agent.py"
				}
				source := artifactFile(t, artifact, name)
				pyproject := artifactFile(t, artifact, "pyproject.toml")

				imported := strings.Contains(source, "from email_validator import ")
				// On the open paren, so the import line, which names the
				// function without calling it, is not a use.
				used := strings.Contains(source, "validate_email(")
				declared := strings.Contains(pyproject, `"email-validator`)

				if imported != used {
					t.Errorf("%s imports the email checker = %v but calls it = %v: an unused import "+
						"fails the emitted project's ruff gate, and a call with no import is a "+
						"NameError on the first value that enters the state", name, imported, used)
				}
				if imported != declared {
					t.Errorf("%s imports the email checker = %v but pyproject declares it = %v: an "+
						"import with no dependency is an ImportError at worker startup, and a "+
						"dependency with no import is a package every image installs for nothing",
						name, imported, declared)
				}
				withTypes = withTypes || imported
				withoutTypes = withoutTypes || !imported
			})
		}
	}
	if !withTypes || !withoutTypes {
		t.Errorf("covered a package that needs the email checker = %v and one that does not = %v; "+
			"this agreement needs both", withTypes, withoutTypes)
	}
}

// A package whose only shaped type is the email one compiles no pattern, so it
// must not import `re`.
//
// NeedsRe used to mean "any shaped type is used", which was the same thing until
// a type arrived whose check is a library call. Left alone it would emit an
// unused `import re`, which fails the ruff gate the emitted project runs, and it
// would fail it only for a package nobody has written yet.
func TestAnEmailOnlyPackageCompilesNoPattern(t *testing.T) {
	agent := loadTypedState(t)
	// Everything except the email pair, so nothing else pulls a pattern in.
	trimmed := *agent
	trimmed.Variables = map[string]ir.Variable{
		"reminder_email": {Type: ir.PrimitiveString, Shape: &ir.TypeRef{Shaped: ir.ShapedEmail}},
	}
	trimmed.VariableOrder = []string{"reminder_email"}
	trimmed.Shapes = map[string]ir.Shape{}
	trimmed.Tasks = map[string]ir.Task{}

	block, err := TypedState(&trimmed)
	if err != nil {
		t.Fatal(err)
	}
	if !block.NeedsShaped {
		t.Error("a package declaring EmailStr does not count as using a shaped type, so it emits no AfterValidator import")
	}
	if block.NeedsRe {
		t.Error("a package whose only shaped type is checked by a library still asks for `import re`, " +
			"which the emitted project's ruff gate refuses as unused")
	}
	if !block.NeedsEmailValidator {
		t.Error("a package declaring EmailStr does not ask for the checker, so the import is a NameError")
	}
	if strings.Contains(block.Source, "re.compile(") {
		t.Error("a package whose only shaped type is checked by a library still compiles a pattern")
	}
	// And the other direction: the pair alone needs the checker too, because its
	// own parser reads the address whether or not any value is a bare EmailStr.
	pair := *agent
	pair.Variables = map[string]ir.Variable{
		"booked_for": {Type: ir.PrimitiveString, Shape: &ir.TypeRef{Shape: "NameEmail"}},
	}
	pair.VariableOrder = []string{"booked_for"}
	pair.Shapes = map[string]ir.Shape{"NameEmail": agent.Shapes["NameEmail"]}
	pair.Tasks = map[string]ir.Task{}
	block, err = TypedState(&pair)
	if err != nil {
		t.Fatal(err)
	}
	if !block.NeedsEmailValidator || !block.NeedsModelValidator {
		t.Errorf("a package declaring only the pair asks for the checker = %v and model_validator = %v; "+
			"it needs both, because the class it emits carries a parser that calls one",
			block.NeedsEmailValidator, block.NeedsModelValidator)
	}
}

// The pair takes a value in three shapes, and two of them are the reason it
// exists at all.
//
// The model fills the two fields, which is why the emitted schema stays an
// object and carries no format keyword. But a tool that returns
// "Fred Bloggs <fred@example.com>", a seeded call-start value and a dotted
// assign that picks one string out of a result all hand over a single string. A
// class with no parser would refuse every one of those on a value that reads
// perfectly well, and would refuse it inside a finish handler, where the model
// has nothing better to send.
//
// The parser is asserted on the emitted text here and driven against the real
// library by the L4 smoke, which is the only place the display-name reading is
// actually exercised.
func TestTheEmittedPairReadsOneString(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		source := emitted(t, agent, provider)
		for _, want := range []string{
			// A string is read, anything else passes through to the fields.
			"if not isinstance(value, str):",
			// The display-name form is what needs the flag, and the flag is
			// what needs email-validator 2.2.
			"allow_display_name=True",
			// With no name in front, the local part stands in, which is the
			// answer Pydantic's own NameEmail gives for the same input.
			`"name": info.display_name or info.local_part,`,
			// The address is saved normalized, so a later tool does not have to
			// normalize it again.
			`"email": info.normalized,`,
		} {
			if !strings.Contains(source, want) {
				t.Errorf("%s emits a NameEmail parser missing %q, so a value that arrives as one "+
					"string is refused rather than read", provider, want)
			}
		}
	}
}
