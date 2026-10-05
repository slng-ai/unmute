package generate

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/stateschema"
	"github.com/slng-ai/unmute/internal/target"
)

// typedStateMarkers is every distinctive line the shared call-state module
// emits. Exhaustive on purpose: the gate below asserts that a package with no
// state.py and no step carries none of them, so a marker missing from this
// list is a hole in that gate.
var typedStateMarkers = []string{
	"# --- call state",
	"class StateRefused",
	"class CallState(",
	"class StepResult(",
	"STEP_RESULTS",
	"ASSIGNMENTS",
	"PrivateAttr(",
	"create_model(",
	"from state import State",
	"to_jsonable_python",
}

func loadTypedState(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "typed_state"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := buildWithState(t, pkg)
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
	agent, err := buildWithState(t, pkg)
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
	return artifactFile(t, artifact, agentSource)
}

// TestTypedStateEmitsNothingForAPackageThatDeclaresNone is FR-015, and it is
// the only real protection a package with no state has from this feature.
//
// A package with no state.py and no step must emit exactly what it emitted
// before, so call_state.py, state.py and every import they need appear only
// when something is authored. The golden files hold the byte comparison; this
// holds the reason a byte would change.
func TestTypedStateEmitsNothingForAPackageThatDeclaresNone(t *testing.T) {
	agent := loadShapeless(t)
	block, err := TypedState(agent, "")
	if err != nil {
		t.Fatal(err)
	}
	if block.Source != "" {
		t.Errorf("a package declaring no state rendered a block:\n%s", block.Source)
	}
	if len(block.Values) != 0 || len(block.Deps()) != 0 {
		t.Errorf("a package declaring no state named values %v and dependencies %v", block.Values, block.Deps())
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range artifact.Files {
			if file.Path == "state.py" {
				t.Errorf("%s writes a state.py for a package that has none", provider)
			}
		}
		module := artifactFile(t, artifact, agentSource)
		for _, marker := range typedStateMarkers {
			if strings.Contains(module, marker) {
				t.Errorf("%s emits %q for a package declaring no state", provider, marker)
			}
		}
		// The import lines the block needs must not appear either: an unused
		// import is a byte that changed, and on this tree it is also a lint
		// failure in the emitted project.
		for _, unwanted := range []string{"from pydantic_core import", "ConfigDict", "field_validator"} {
			if strings.Contains(module, unwanted) {
				t.Errorf("%s emits %q for a package declaring no state", provider, unwanted)
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
			"# --- call state",
			"class StateRefused(Exception):",
			"class CallState(State):",
			"class StepResult(BaseModel):",
			"STEP_RESULTS = {step: StepResult.for_step(step) for step in CallState.RESULTS}",
			"from state import State",
			// The step tables the author's assign: lines compile to.
			`("appointments", "appointment", True)`,
			`("caller_phone", "caller_phone", False)`,
		} {
			if !strings.Contains(module, want) {
				t.Errorf("%s does not emit %q", provider, want)
			}
		}
		// The call state holds no type of its own: every one is read off the
		// author's State, so none of the shapes this fixture declares is
		// re-declared in generated code.
		for _, redeclared := range []string{"class Appointment", "class Contact", "Phone = Annotated"} {
			if strings.Contains(module, redeclared) {
				t.Errorf("%s re-declares %q in generated code; the author's state.py is the only place a type is written",
					provider, redeclared)
			}
		}
	}
}

// TestStateFileIsCopiedVerbatim holds the contract of the one file the author
// writes: the bytes in the project are the bytes in the package, flagged so the
// write path formats them no more than it would edit them.
func TestStateFileIsCopiedVerbatim(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "typed_state"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := buildWithState(t, pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, file := range artifact.Files {
			if file.Path != "state.py" {
				continue
			}
			found = true
			if !file.Verbatim {
				t.Errorf("%s does not mark state.py verbatim, so the write path may reformat the author's file", provider)
			}
			if string(file.Content) != string(pkg.StateSource) {
				t.Errorf("%s changed state.py on its way to the project", provider)
			}
		}
		if !found {
			t.Errorf("%s writes no state.py, so call_state.py has nothing to subclass", provider)
		}
		for _, field := range agent.VariableOrder {
			if description := agent.Variables[field].Description; description != "" &&
				!strings.Contains(string(pkg.StateSource), strings.Split(description, "\n")[0]) {
				t.Errorf("%s: the description of %s is not in state.py, which is where the model reads it from", provider, field)
			}
		}
	}
}

// TestTypedStatePutsNoShapeKeywordInAnEmittedSchema is research section 20,
// and it fails in no other check.
//
// A `format` or a `pattern` in the schema the model is sent survives one
// target's strict converter, which is that target's default, and the provider
// rejects it. So it passes every local check and fails on the first real call.
// The one place a tool schema is built drops both keywords and says them in
// words, and both targets send only what that function returns.
func TestTypedStatePutsNoShapeKeywordInAnEmittedSchema(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		if !strings.Contains(module, `if key not in ("format", "pattern", "$defs")`) {
			t.Errorf("%s does not drop format and pattern from the schema the model is sent", provider)
		}
		if got := strings.Count(withoutComments(module), "model_json_schema("); got != 1 {
			t.Errorf("%s calls model_json_schema() %d times, want 1 (tool_parameters): a second is a schema going "+
				"out without the keywords stripped", provider, got)
		}
	}
	// Every format the author's types use has its words, or the model is told
	// nothing about the shape until a value is refused mid-call.
	module := withoutComments(emitted(t, agent, ir.ProviderPipecat))
	walkTypes(agent, func(typ *stateschema.Type) {
		if typ.Format != "" && !strings.Contains(module, `"`+typ.Format+`": "`) {
			t.Errorf("the type format %q has no words in _FORMAT_WORDS, so the model is never told its shape", typ.Format)
		}
	})
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

// TestCallStateIsTheSameOnBothTargets is FR-006 where it is cheapest to hold:
// call_state.py is rendered once, in call_state.go, and inserted into both
// modules verbatim, with only the target's own members filled in. Rendering it
// twice is how the two targets would drift, and this is what notices.
func TestCallStateIsTheSameOnBothTargets(t *testing.T) {
	agent := loadTypedState(t)
	block, err := TypedState(agent, "")
	if err != nil {
		t.Fatal(err)
	}
	if block.Source == "" {
		t.Fatal("the fixture rendered no block, so this gate proves nothing")
	}
	// The target's own members are the one seam, so the block is checked on
	// either side of it.
	head, tail, found := strings.Cut(block.Source, "\n\n    def initial_value(")
	if !found {
		t.Fatal("the block has no initial_value, so this gate cannot find the members seam")
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		if !strings.Contains(module, head) || !strings.Contains(module, "\n\n    def initial_value("+tail) {
			t.Errorf("%s does not carry the rendered block verbatim, so the two targets can differ", provider)
		}
	}
}

// TestCallStateMembersMatchTheTemplate holds ir.CallStateMembers, the names a
// State field may not take, to the methods the template really defines. A
// method the list does not know is a field the compiler would let overwrite it.
func TestCallStateMembersMatchTheTemplate(t *testing.T) {
	want := slices.Clone(ir.CallStateMembers)
	slices.Sort(want)
	if got := CallStateMethods(); !slices.Equal(got, want) {
		t.Errorf("CallState defines %v, but ir.CallStateMembers lists %v", got, want)
	}
}

// TestLiveKitFinishParameterIsTheGeneratedClass is research section 13, and it
// is the silent gap this closes.
//
// A bare dict annotation carries no field names, no types and no descriptions,
// so the model was asked for an object and told nothing about what belongs in
// it. Nothing failed. It just did not work. The finish tool now sends the
// schema of the step's own result model, raw, so nothing is rebuilt from a
// signature that could lose a field.
func TestLiveKitFinishParameterIsTheGeneratedClass(t *testing.T) {
	agent := loadTypedState(t)
	module := emitted(t, agent, ir.ProviderLiveKit)
	finish := functionBody(t, module, "    async def finish(")
	if finish == "" {
		t.Fatal("no finish handler emitted")
	}
	for _, want := range []string{
		"@function_tool(raw_schema={",
		`"parameters": STEP_RESULTS["book"].tool_parameters(),`,
		"async def finish(self, raw_arguments: dict[str, object], ctx: RunContext[CallState])",
	} {
		if !strings.Contains(module, want) {
			t.Errorf("the livekit finish tool does not carry %q:\n%s", want, finish)
		}
	}
	// And no bare dict anywhere a structured result is annotated.
	for _, forbidden := range []string{"appointment: dict", "appointment: Any", "appointment: object"} {
		if strings.Contains(module, forbidden) {
			t.Errorf("livekit annotates a structured result as %q, which tells the model nothing", forbidden)
		}
	}
	// A Literal result field keeps its closed set because the field type is
	// read off State, so the model is told what it may hand back.
	if !strings.Contains(module, "annotation = _item_type(_field_type(name)) if append else _field_type(name)") {
		t.Error("a step result field is not typed from State's own annotation")
	}
}

// TestPipecatFinishPropertiesComeFromTheStepResult is the same gate for the
// other target, which sends each property as one tool property.
func TestPipecatFinishPropertiesComeFromTheStepResult(t *testing.T) {
	agent := loadTypedState(t)
	module := emitted(t, agent, ir.ProviderPipecat)
	if !strings.Contains(module, `STEP_RESULTS["book"].tool_parameters()["properties"]`) {
		t.Errorf("pipecat's finish properties do not come from the step's result model:\n%s", module)
	}
}

// TestDottedAssignWalksIntoAStructuredResultAtEmission is gap 1 of the scoped
// variables feature, proven at the emitted seam: a step's result is a model
// whose declared fields StepResult has validated, nested models included. So a
// dotted assign field walks the path one part at a time and an absent or null
// parent reads as None rather than raising.
func TestDottedAssignWalksIntoAStructuredResultAtEmission(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := buildWithState(t, pkg)
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
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		got := emitted(t, agent, provider)
		if !strings.Contains(got, want) || !strings.Contains(got, `for part in path.split("."):`) {
			t.Errorf("%s does not walk the dotted assign path:\n%s", provider, got)
		}
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
			// Both authored append destinations reach the one staged writer.
			`("appointments", "appointment", True)`,
			`("caller_reason", "reason", True)`,
			"value = _appended(getattr(self, name), value)",
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
	// The list defaults live on the author's State, a Pydantic model, which
	// copies a field's default for each instance. A shared mutable default
	// would be one call's state leaking into the next. The smoke suite measures
	// the copy.
	if got := agent.Variables["appointments"].Default; got == nil {
		t.Error("appointments declares no default, so a step has no list to append to")
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
		// Refused where it enters: the finish is parsed, and the batch is
		// validated whole, before anything is saved.
		save := between(t, module, "    def save_result(", "    def save_batch(")
		if strings.Index(save, "STEP_RESULTS[step].parse(raw)") > strings.Index(save, "self.save_batch(") {
			t.Errorf("%s saves a result before parsing it, so a refused value is already in the state", provider)
		}
		batch := between(t, module, "    def save_batch(", "    def save_call_start(")
		validate := strings.Index(batch, "type(self).model_validate(values)")
		if validate < 0 || validate > strings.Index(batch, "setattr(self, name, value)") {
			t.Errorf("%s writes a value before validating the batch, so the previous contents do not survive a refusal:\n%s",
				provider, batch)
		}
		// The previous contents survive, because the refusal is an exception and
		// the save is the statement after it.
		if !strings.Contains(module, "except StateRefused as refused:") {
			t.Errorf("%s does not catch the refusal, so a bad value ends the step rather than the turn", provider)
		}
		// The message names the field and what was allowed.
		if !strings.Contains(module, "refused.message") {
			t.Errorf("%s discards the refusal message, so the model is never told which field or what was allowed",
				provider)
		}
		if !strings.Contains(module, "Ask again, then call finish with a value that fits.") {
			t.Errorf("%s does not tell the model what to do next after a refusal", provider)
		}
	}
	// And the one refusal names the field and carries Pydantic's own message,
	// which for a Literal lists every allowed entry.
	block, err := TypedState(agent, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`where = ".".join(str(part) for part in (field, *first["loc"]) if part != "")`,
		`return cls(f"{where}: {first['msg']}")`,
		"raise StateRefused.from_error(error) from None",
	} {
		if !strings.Contains(block.Source, want) {
			t.Errorf("the shared refusal does not name the field: %q missing", want)
		}
	}
}

// TestFinishSchemaResolvesEveryRef is the gate under the one thing no unit test
// could settle, now that a real request has settled it.
//
// Pydantic emits $defs and a $ref for a model that contains another model.
// Measured against the provider: one target nests the schema inside one tool
// property and sends no strict flag, and a $ref there comes back 200 with the
// model inventing field names for the nested object, so every result would be
// refused where it entered. The refs inlined, it fills the model's own fields.
// The other target hoists its $defs to the parameters root and sends strict on,
// and a $defs anywhere but that root is a 400 naming the pointer. So every ref
// is inlined and no $defs is sent, and this is what notices if that stops.
func TestFinishSchemaResolvesEveryRef(t *testing.T) {
	agent := loadTypedState(t)
	block, err := TypedState(agent, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"def _tool_schema(node: object, defs: dict) -> object:",
		`target = node.get("$ref")`,
		"siblings = {key: _tool_schema(value, defs) for key, value in node.items() if key != \"$ref\"}",
		`if key not in ("format", "pattern", "$defs")`,
	} {
		if !strings.Contains(block.Source, want) {
			t.Errorf("the resolver is incomplete: %q missing", want)
		}
	}
	// The sibling keys survive the resolution. A `$ref` beside a description is
	// how a nullable nested field arrives, and dropping the description would
	// take away the one thing telling the model what the field is.
	if !strings.Contains(block.Source, "return {**found, **siblings} if isinstance(found, dict) else found") {
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
		// The guard is in one place rather than at each site, so it cannot be
		// present at one append and missing at the next.
		for _, want := range []string{
			"            if append:\n                if value is None:\n                    continue\n",
			"def _appended(entries: list, value: object) -> list:",
			"    if isinstance(value, (BaseModel, dict, list)) and value in entries:\n        return list(entries)\n",
		} {
			if !strings.Contains(module, want) {
				t.Errorf("%s does not emit %q, so an absent entry appends None and the state holds a booking nobody made",
					provider, want)
			}
		}
		// And no append reaches the list without going through it.
		if strings.Contains(module, ".appointments.append(") || strings.Contains(module, ".caller_reason.append(") {
			t.Errorf("%s appends straight onto a declared list, so a step re-entered mid-call adds the "+
				"entry it read through an explicit prompt reference", provider)
		}
	}
	// And the type is what makes it legal: the element type with its
	// nullability is what an append is checked against.
	booking, ok := agent.Controls["manage_booking"].(*ir.Delegate)
	if !ok {
		t.Fatalf("manage_booking = %#v, want a delegate", agent.Controls["manage_booking"])
	}
	field := agent.Tasks[booking.Task].Result["appointment"]
	if field.Type == nil || !field.Append {
		t.Errorf("the booking step's appointment result is %v (append %v), want an appended one",
			field.Type, field.Append)
	}
}

// An empty string is no value yet, and every finish field reads it that way.
//
// Empty is not a wrong value: it is what a declared variable holds before
// anything fills it, and what a model sends for a field no tool gave it. Refusing
// it deadlocked a live call on both targets and did so differently: LiveKit
// logged a generic "error parsing arguments for finish" with no field and no
// expectation, while Pipecat's refusal reached the model, which asked the caller
// out loud for an identifier that no tool in the package returns. The model had
// nothing else to send, so every retry was refused the same way and the step
// never finished.
//
// A wrong value is still refused. That half is proven by
// TestAValueOutsideALiteralSetIsRefusedWhereItEnters and, on a running module,
// by the L4 smoke.
func TestABlankResultValueReadsAsAbsent(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		source := emitted(t, agent, provider)
		for _, want := range []string{
			`@field_validator("*", mode="before")`,
			`if value == "" and info.field_name not in cls.BLANK_OK:`,
			// Text keeps an empty string, because that is what a tool returns for
			// a customer it has no name for.
			"model.BLANK_OK = frozenset(blank_ok)",
			// Every field is optional in the schema, so a step that cannot serve
			// the caller never has to invent values, and a served finish is then
			// held to REQUIRED.
			"fields[field] = (annotation | None, Field(None, description=description))",
			"missing = next((name for name in sorted(cls.REQUIRED) if getattr(result, name) is None), None)",
		} {
			if !strings.Contains(source, want) {
				t.Errorf("%s does not emit %q; empty is how a declared value says nothing yet and the "+
					"model has nothing else to send for a field no tool fills", provider, want)
			}
		}
	}
}

func TestTaskFinishAllowsEscapeWithoutDomainArguments(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		source := emitted(t, agent, provider)
		for _, want := range []string{
			// Unserved bypasses validation and saves no domain values.
			"        if result.unserved_request:\n            return {\"unserved_request\": result.unserved_request}\n",
			`if raw.get("unserved_request"):`,
		} {
			if !strings.Contains(source, want) {
				t.Errorf("%s does not emit %q; an unserved exit must not invent a value", provider, want)
			}
		}
	}
}

// The email checker and the phone library are the two things a state.py can
// reach for that neither stdlib nor Pydantic supplies, so the project's
// pyproject.toml has to ask for them exactly when state.py's types need them.
//
// An import with no dependency is an ImportError at worker startup, which the
// operator sees as an agent that never answers. A dependency nothing imports is
// a package every image installs for nothing. Both targets, and a package with
// the types beside one without, so a driver that simply stopped asking could
// not pass this.
func TestStateDependenciesMatchTheTypes(t *testing.T) {
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
				pyproject := artifactFile(t, artifact, "pyproject.toml")
				wantEmail := agent.State != nil && slices.ContainsFunc(agent.VariableOrder, func(name string) bool {
					return reachesEmail(agent.Variables[name].Schema)
				})
				wantPhone := agent.State != nil && slices.Contains(agent.State.ExtraTypes, "phone_numbers")

				if got := strings.Contains(pyproject, `"email-validator`); got != wantEmail {
					t.Errorf("pyproject declares the email checker = %v but state.py needs it = %v", got, wantEmail)
				}
				if got := strings.Contains(pyproject, `pydantic-extra-types[phonenumbers]`); got != wantPhone {
					t.Errorf("pyproject declares the phone library = %v but state.py needs it = %v", got, wantPhone)
				}
				withTypes = withTypes || wantEmail
				withoutTypes = withoutTypes || !wantEmail
			})
		}
	}
	if !withTypes || !withoutTypes {
		t.Errorf("covered a package that needs the email checker = %v and one that does not = %v; "+
			"this agreement needs both", withTypes, withoutTypes)
	}
}

// TestTypedStateDepsFollowTheTypes holds the rule at its source: an email
// anywhere in a type, however deep, asks for the checker, and each imported
// pydantic_extra_types module asks for its own extra.
func TestTypedStateDepsFollowTheTypes(t *testing.T) {
	email := &stateschema.Type{Kind: stateschema.KindString, Format: "email", Nullable: true}
	nested := &stateschema.Type{Kind: stateschema.KindObject, Model: "Contact", Fields: []stateschema.Field{
		{Name: "name", Type: &stateschema.Type{Kind: stateschema.KindString}},
		{Name: "email", Type: email},
	}}
	for name, schema := range map[string]*stateschema.Type{
		"a bare address":                 email,
		"an address in a model":          nested,
		"an address in a list":           {Kind: stateschema.KindArray, Items: email},
		"an address in a list of models": {Kind: stateschema.KindArray, Items: nested},
	} {
		agent := &ir.Agent{
			State:         &stateschema.Model{},
			Variables:     map[string]ir.Variable{"contact": {Schema: schema}},
			VariableOrder: []string{"contact"},
		}
		block, err := TypedState(agent, "")
		if err != nil {
			t.Fatal(err)
		}
		if !block.NeedsEmailValidator || !slices.Equal(block.Deps(), []string{"email-validator>=2.2,<3"}) {
			t.Errorf("%s: deps = %v, want the email checker alone", name, block.Deps())
		}
	}
	plain := &ir.Agent{
		State:         &stateschema.Model{ExtraTypes: []string{"phone_numbers"}},
		Variables:     map[string]ir.Variable{"count": {Schema: &stateschema.Type{Kind: stateschema.KindInteger}}},
		VariableOrder: []string{"count"},
	}
	block, err := TypedState(plain, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pydantic-extra-types[phonenumbers]>=2.11,<3"}; !slices.Equal(block.Deps(), want) {
		t.Errorf("deps = %v, want %v and no email checker", block.Deps(), want)
	}
}
