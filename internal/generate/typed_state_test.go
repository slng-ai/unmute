package generate

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// typedStateMarkers is every distinctive line the declared-state block emits.
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
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
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
			`Phone = Annotated[str, AfterValidator(_shape_phone)]`,
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
// cheapest to hold: the declared-state block is rendered once, in shapes.go,
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
	if !strings.Contains(module, "appointment: Appointment,") {
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
	if !strings.Contains(module, `reason: Literal["create_booking", "cancel_booking"],`) {
		t.Errorf("the finish parameter for a Literal result is not the closed set:\n%s", finish)
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
			// An entry added, never the value replaced.
			".appointments.append(",
			".caller_reason.append(",
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
	marker := "self.complete(_task_result("
	if provider == ir.ProviderPipecat {
		marker = "_results[\"book\"] = _values"
	}
	at := strings.Index(module, marker)
	if at < 0 {
		t.Fatalf("%s emits no %q, so this test cannot say when the result is recorded", provider, marker)
	}
	return at
}

// TestComposedStateBlockIsIdenticalOnBothTargets is FR-006 over the thing an
// author actually reads: the block inside each prompt.
//
// One composer above both drivers is what makes this true, and it is the
// property a second composer in a driver would break. The block is per site, so
// the comparison is per site too: a step whose block differs between targets is
// a step reading a different state on each.
func TestComposedStateBlockIsIdenticalOnBothTargets(t *testing.T) {
	agent := loadTypedState(t)
	livekit := stateBlocksIn(t, emitted(t, agent, ir.ProviderLiveKit))
	pipecat := stateBlocksIn(t, emitted(t, agent, ir.ProviderPipecat))
	if len(livekit) == 0 {
		t.Fatal("no composed block found in the emitted module, so this gate proves nothing")
	}
	// Compared as sets, because the two drivers write their prompt constants in
	// different orders and one of them escapes the newlines. Neither is a
	// difference in the block: what has to match is which blocks exist and what
	// each one says.
	slices.Sort(livekit)
	slices.Sort(pipecat)
	if !slices.Equal(livekit, pipecat) {
		t.Errorf("the composed blocks differ between the targets:\nlivekit: %q\npipecat: %q", livekit, pipecat)
	}
	// Every block also carries the heading and the note, so a reader of either
	// module finds the same thing in the same words.
	for _, block := range append(livekit, pipecat...) {
		if !strings.Contains(block, ir.StateBlockNote) {
			t.Errorf("a composed block carries no note saying what it is for: %q", block)
		}
	}
}

// stateBlocksIn is every composed block in an emitted module, in the order the
// module writes them.
func stateBlocksIn(t *testing.T, module string) []string {
	t.Helper()
	// One driver writes a prompt as a triple-quoted literal and the other as a
	// single-quoted one with escaped newlines. Unescaping first is what lets one
	// extractor read both.
	module = strings.ReplaceAll(module, `\n`, "\n")
	var blocks []string
	rest := module
	for {
		at := strings.Index(rest, ir.StateBlockHeading)
		if at < 0 {
			return blocks
		}
		rest = rest[at:]
		var lines []string
		for _, line := range strings.Split(rest, "\n") {
			// A block ends at the first line that is neither its heading, its
			// note nor one of its numbered values.
			if len(lines) > 0 && !strings.HasPrefix(line, ir.StateBlockNote) && !numberedStateLine.MatchString(line) {
				break
			}
			lines = append(lines, line)
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
		rest = rest[len(ir.StateBlockHeading):]
	}
}

// numberedStateLine matches one value's line in a composed block.
var numberedStateLine = regexp.MustCompile(`^\d+\. .*\{\{[a-z_]+\}\}`)

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
	if !strings.Contains(module, "_schema(_FINISH_TYPES[") {
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
		appends := 0
		for _, line := range strings.Split(module, "\n") {
			if !strings.Contains(line, ".appointments.append(") {
				continue
			}
			appends++
			if !strings.HasPrefix(strings.TrimSpace(line), "self.state.appointments.append(") &&
				!strings.HasPrefix(strings.TrimSpace(line), "ctx.userdata.appointments.append(") {
				t.Errorf("%s: unexpected append line %q", provider, line)
			}
		}
		if appends == 0 {
			t.Fatalf("%s appends nothing to appointments, so this gate proves nothing", provider)
		}
		// Every append of a value that may be absent is guarded, so the list
		// grows only when the step produced something.
		guards := strings.Count(module, `["appointment"] is not None:`)
		if guards < appends {
			t.Errorf("%s guards %d of %d appends; an absent entry would append None and the state would hold a "+
				"booking nobody made", provider, guards, appends)
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
