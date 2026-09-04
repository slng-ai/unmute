package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// typedInputMarkers is every distinctive line the typed-inputs feature emits.
// Exhaustive on purpose: the byte-identical gate below asserts that a package
// handing nothing in carries none of them, so a marker missing from this list
// is a hole in that gate.
var typedInputMarkers = []string{
	"_INPUT_TYPES",
	"_INPUT_REQUIRED",
	"def _typed_inputs(",
	"_INPUT_NAMES",
	"_INPUT_EMPTY",
	ir.InputBlockHeading + "\n",
	ir.InputBlockNote,
	"_previous",
	"Not started:",
	// The explicit schema, not the Flows one every task node already carries.
	": FunctionSchema(",
	"import FunctionSchema",
	"## Request block",
}

func loadTypedInputs(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "typed_inputs"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// emittedText returns every text file every declared target of a package
// writes, keyed by "<target>/<path>", so one gate reads a whole compile.
func emittedText(t *testing.T, agent *ir.Agent) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, tgt := range agent.Targets {
		artifact, err := Generate(agent, tgt, target.Default())
		if err != nil {
			t.Fatalf("generate %s: %v", name, err)
		}
		for _, file := range artifact.Files {
			if strings.HasSuffix(file.Path, ".py") || strings.HasSuffix(file.Path, ".md") {
				out[name+"/"+file.Path] = string(file.Content)
			}
		}
	}
	return out
}

// after is the module from the first occurrence of marker, so an assertion
// about one method reads that method and not an earlier one of the same name.
func after(t *testing.T, module, marker string) string {
	t.Helper()
	at := strings.Index(module, marker)
	if at < 0 {
		t.Fatalf("the module emits no %q", marker)
	}
	return module[at:]
}

// method is one emitted method, from its signature to the next method or class,
// so an assertion cannot drift into a later method that happens to carry the
// same fragment.
func method(t *testing.T, module, signature string) string {
	t.Helper()
	text := after(t, module, signature)
	end := len(text)
	for _, next := range []string{"\n    @", "\n    async def ", "\n    def ", "\nclass ", "\n\n\n"} {
		if at := strings.Index(text[len(signature):], next); at >= 0 && at+len(signature) < end {
			end = at + len(signature)
		}
	}
	return text[:end]
}

// constant is one emitted triple-quoted module constant, opening line to
// closing quotes.
func constant(t *testing.T, module, name string) string {
	t.Helper()
	text := after(t, module, name+` = """`)
	end := strings.Index(text[len(name)+6:], `"""`)
	if end < 0 {
		t.Fatalf("%s never closes", name)
	}
	return text[:len(name)+6+end]
}

// ordered asserts the fragments appear in this order, each after the previous.
func ordered(t *testing.T, where, text string, fragments ...string) {
	t.Helper()
	from := 0
	for _, fragment := range fragments {
		at := strings.Index(text[from:], fragment)
		if at < 0 {
			t.Errorf("%s: %q does not follow the previous fragment (or is absent):\n%s", where, fragment, text[:min(len(text), 1200)])
			return
		}
		from += at + len(fragment)
	}
}

// TestTypedInputsEmitNothingForAPackageThatDeclaresNone is FR-011, and it is
// the only real protection every shipped example has from this feature. The
// golden files hold the byte comparison; this holds the reason a byte would
// change, over every shipped package and the frozen control.
func TestTypedInputsEmitNothingForAPackageThatDeclaresNone(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	names = append(names, "salon-concierge-v2")
	for _, name := range names {
		agent := loadExample(t, name)
		for _, task := range agent.Tasks {
			if len(task.Inputs) > 0 {
				t.Fatalf("%s declares an input, so it cannot be the control for this gate", name)
			}
		}
		for path, text := range emittedText(t, agent) {
			for _, marker := range typedInputMarkers {
				if strings.Contains(text, marker) {
					t.Errorf("%s: %s emits %q for a package handing nothing in", name, path, marker)
				}
			}
		}
	}
	// And the composer itself: nothing for a package with no input anywhere.
	shapeless := loadShapeless(t)
	block, err := TypedState(shapeless)
	if err != nil {
		t.Fatal(err)
	}
	if len(block.Inputs) != 0 || len(InputStateFields(shapeless)) != 0 {
		t.Errorf("a package handing nothing in resolved inputs: %+v", block.Inputs)
	}
}

// TestInputsReachBothDelegateSchemas is FR-003, FR-004 and FR-009 in the
// emitted modules: the step's tool takes one parameter per input, required
// before optional, each carrying its description; on Pipecat the schema is
// explicit because the framework reads one off a signature and a signature
// cannot express a Literal or a declared class; and a step with no inputs
// keeps the tool it always had.
func TestInputsReachBothDelegateSchemas(t *testing.T) {
	agent := loadTypedInputs(t)
	livekit := emitted(t, agent, ir.ProviderLiveKit)
	pipecat := emitted(t, agent, ir.ProviderPipecat)

	delegate := method(t, livekit, "    async def do_thing(")
	ordered(t, "livekit do_thing signature", delegate,
		`kind: Literal["a", "b"],`,
		"note: Annotated[", "str | None,", "Anything the caller added", "] = None,",
		"thing: Annotated[", "Thing | None,", "The thing the caller named", "] = None",
		") -> dict")
	if !strings.Contains(livekit, "    async def check(self, ctx: RunContext) -> dict:") {
		t.Error("livekit: a step with no inputs no longer takes a bare RunContext")
	}

	ordered(t, "pipecat do_thing schema", after(t, pipecat, "    def build_tools(self) -> list:"),
		`"do_thing": FunctionSchema(`, `name="do_thing",`,
		`"kind": _schema(_INPUT_TYPES["do_thing"]["kind"])`,
		`"note": _schema(_INPUT_TYPES["do_thing"]["note"])`,
		`"thing": _schema(_INPUT_TYPES["do_thing"]["thing"])`,
		`required=["kind"],`,
		`"to_specialist": FunctionSchema(`, `required=["problem"],`,
		`return [explicit.get(getattr(tool, "__name__", None), tool) for tool in tools]`)
	// The handler still carries the parameters, because the framework invokes a
	// direct function with the model's arguments as keywords.
	ordered(t, "pipecat do_thing handler", method(t, pipecat, "    async def do_thing("),
		"params: FunctionCallParams,", `kind: Literal["a", "b"],`, "note: Annotated[", "] = None,", "thing: Annotated[", "] = None")
	if !strings.Contains(pipecat, "    async def check(self, params: FunctionCallParams):") {
		t.Error("pipecat: a step with no inputs no longer takes a bare FunctionCallParams")
	}
	if !strings.Contains(pipecat, "from pipecat.adapters.schemas.function_schema import FunctionSchema") {
		t.Error("pipecat: FunctionSchema is used and not imported")
	}
	// The one type table serves both, keyed by site, with the description in
	// the annotation so the schema the model is sent carries it.
	for name, module := range map[string]string{"livekit": livekit, "pipecat": pipecat} {
		ordered(t, name+" input table", after(t, module, "_INPUT_TYPES = {"),
			`"do_thing": {`, `"kind": TypeAdapter(Literal["a", "b"]),`, `"note": TypeAdapter(`, "Anything the caller added",
			`"to_front": {`, `"outcome": TypeAdapter(str),`,
			`"to_specialist": {`, `"problem": TypeAdapter(str),`,
			"_INPUT_REQUIRED = {", `"do_thing": {"kind"},`, `"to_front": {"outcome"},`, `"to_specialist": {"problem"},`)
	}
}

// TestAnInputOutsideItsTypeIsRefusedBeforeTheStepIsConstructed is FR-007 on
// both targets: validation precedes the task's construction, the announcement
// and every write, and the refusal names what the contract says.
func TestAnInputOutsideItsTypeIsRefusedBeforeTheStepIsConstructed(t *testing.T) {
	agent := loadTypedInputs(t)
	livekit := method(t, emitted(t, agent, ir.ProviderLiveKit), "    async def do_thing(")
	ordered(t, "livekit", livekit,
		`_typed_inputs(`, `"do_thing", {"kind": kind, "note": note, "thing": thing}`,
		"except _StateRefused as refused:",
		`"refused": f"Not started: {refused.message}. Ask the caller, then run the step again with a value that fits."`,
		"setattr(ctx.userdata, _name, _value)",
		"await DoThing(")
	pipecat := method(t, emitted(t, agent, ir.ProviderPipecat), "    async def do_thing(")
	ordered(t, "pipecat", pipecat,
		`_typed_inputs("do_thing", params.arguments)`,
		"except _StateRefused as refused:",
		`"refused": f"Not started: {refused.message}. Ask the caller, then run the step again with a value that fits."`,
		"setattr(self.state, _name, _value)",
		"flow.initialize(")
	// The helper refuses a required value the model left out or left empty,
	// naming the field, and validates an optional one as absent.
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		body := functionBody(t, emitted(t, agent, provider), "def _typed_inputs(site, values):")
		for _, want := range []string{
			"required = _INPUT_REQUIRED.get(site, set())",
			`if name in required and (value is None or value == ""):`,
			`raise _StateRefused(f"{name}: required, and nothing was given")`,
			"out[name] = _plain(_typed(name, adapter, value))",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: _typed_inputs does not carry %q:\n%s", provider, want, body)
			}
		}
	}
}

// TestAStepsInputsAreSavedAndRestoredAroundTheVisit is FR-006: the values
// hold for the visit and are put back after it, never cleared, so an owner
// whose own brief shares a name keeps it; and the restore does not run on the
// in-task handoff path, where the receiver's brief was just written.
func TestAStepsInputsAreSavedAndRestoredAroundTheVisit(t *testing.T) {
	agent := loadTypedInputs(t)
	livekit := method(t, emitted(t, agent, ir.ProviderLiveKit), "    async def do_thing(")
	ordered(t, "livekit", livekit,
		"_previous = {", `"kind": ctx.userdata.kind,`, `"note": ctx.userdata.note,`, `"thing": ctx.userdata.thing}`,
		"await DoThing(",
		"except _TaskTransfer as transfer:", "return transfer.agent",
		"for _name, _value in _previous.items():", "setattr(ctx.userdata, _name, _value)",
		"await self.update_chat_ctx(owner_ctx)")
	for _, clear := range []string{"ctx.userdata.kind = None", "ctx.userdata.note = None", "ctx.userdata.thing = None"} {
		if strings.Contains(livekit, clear) {
			t.Errorf("livekit: the delegate clears %q rather than restoring it, which erases an owner's brief of that name", clear)
		}
	}

	pipecat := emitted(t, agent, ir.ProviderPipecat)
	ordered(t, "pipecat delegate", method(t, pipecat, "    async def do_thing("),
		"self._do_thing_previous = {", `"kind": self.state.kind,`, "setattr(self.state, _name, _value)", "flow.initialize(")
	// Restored where the step returns to its owner, before the owner's prompt
	// is rendered again, so that prompt reads the owner's own values.
	ordered(t, "pipecat completion", method(t, pipecat, "    async def _do_thing_complete_do_thing(self):"),
		"for _name, _value in self._do_thing_previous.items():", "setattr(self.state, _name, _value)",
		"system_instruction=")
	// And not on the handoff out of the step.
	transfer := method(t, pipecat, "    async def _do_thing_transfer_do_thing_to_specialist(self, args, flow_manager):")
	if strings.Contains(transfer, "_do_thing_previous") {
		t.Error("pipecat: the in-task handoff restores the step's inputs over the brief it just wrote")
	}
}

// TestRequestBlockReachesTheStepPromptOnBothTargets is FR-005 and FR-010 in
// the emitted modules: the block ends the receiving prompt, identically on
// both targets, and no other prompt carries it.
func TestRequestBlockReachesTheStepPromptOnBothTargets(t *testing.T) {
	agent := loadTypedInputs(t)
	stepBlock := "Request:\nWhat you were handed for this visit. It does not change while you run.\n1. Kind: {{kind}}\n2. Note: {{note}}\n3. Thing: {{thing}}"
	want := map[string]string{
		"SPECIALIST_PROMPT": "Request:\nWhat you were handed for this visit. It does not change while you run.\n1. Problem: {{problem}}\n2. Thing: {{thing}}",
		"FRONT_PROMPT":      "Request:\nWhat you were handed for this visit. It does not change while you run.\n1. Outcome: {{outcome}}",
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		for name, block := range want {
			prompt := constant(t, module, name)
			if !strings.HasSuffix(prompt, block) {
				t.Errorf("%s: %s does not end with its request block:\n%s", provider, name, prompt)
			}
			if strings.Index(prompt, ir.StateBlockHeading) > strings.Index(prompt, ir.InputBlockHeading) {
				t.Errorf("%s: %s places the request block before the state block", provider, name)
			}
		}
		if strings.Contains(constant(t, module, "FRONT_PROMPT"), "{{kind}}") {
			t.Errorf("%s: the front desk's prompt names the step's input", provider)
		}
		// A task prompt is a module constant on LiveKit and a quoted node
		// message on Pipecat; the block is the same text in both spellings.
		if provider == ir.ProviderLiveKit {
			if prompt := constant(t, module, "DO_THING_PROMPT"); !strings.Contains(prompt, stepBlock) {
				t.Errorf("livekit: DO_THING_PROMPT carries no request block:\n%s", prompt)
			}
			if strings.Contains(constant(t, module, "CHECK_PROMPT"), ir.InputBlockHeading) {
				t.Error("livekit: a step with no inputs carries a request block")
			}
		} else {
			node := method(t, module, "    def _do_thing_node_do_thing(self) -> NodeConfig:")
			if !strings.Contains(node, strings.TrimSuffix(strings.TrimPrefix(pyQuote(stepBlock), `"`), `"`)) {
				t.Errorf("pipecat: the do_thing node's role message carries no request block:\n%s", node)
			}
			if strings.Contains(method(t, module, "    def _check_node_check(self) -> NodeConfig:"), "Request:") {
				t.Error("pipecat: a step with no inputs carries a request block")
			}
		}
	}
}

// TestRequestBlockIsByteIdenticalOnBothTargets and the machinery gate below
// are FR-010 held by construction and then checked: one composer and one
// emitter above both drivers, and the two modules carry the same text.
func TestRequestBlockIsByteIdenticalOnBothTargets(t *testing.T) {
	agent := loadTypedInputs(t)
	livekit := emitted(t, agent, ir.ProviderLiveKit)
	pipecat := emitted(t, agent, ir.ProviderPipecat)
	for _, name := range []string{"SPECIALIST_PROMPT", "FRONT_PROMPT"} {
		lk, pc := constant(t, livekit, name), constant(t, pipecat, name)
		if lk[strings.Index(lk, ir.InputBlockHeading):] != pc[strings.Index(pc, ir.InputBlockHeading):] {
			t.Errorf("%s request block differs between targets:\n%s\n---\n%s", name, lk, pc)
		}
	}
	// The step's block is composed once in ir.Build; both drivers carry that
	// one string, LiveKit verbatim and Pipecat quoted.
	block := agent.Tasks["do_thing"].Instructions
	block = block[strings.Index(block, ir.InputBlockHeading):]
	if !strings.Contains(livekit, block) || !strings.Contains(pipecat, strings.Trim(pyQuote(block), `"`)) {
		t.Errorf("the composed step block does not reach both modules:\n%s", block)
	}
}

func TestTypedInputMachineryIsByteIdenticalOnBothTargets(t *testing.T) {
	agent := loadTypedInputs(t)
	livekit := emitted(t, agent, ir.ProviderLiveKit)
	pipecat := emitted(t, agent, ir.ProviderPipecat)
	for _, region := range []string{"_INPUT_TYPES = {", "_INPUT_REQUIRED = {", "def _typed_inputs(site, values):", "_INPUT_NAMES = {", "def _state_text(name, value):"} {
		lk := functionBody(t, livekit, region)
		pc := functionBody(t, pipecat, region)
		if lk != pc {
			t.Errorf("%s differs between targets:\n%s\n---\n%s", region, lk, pc)
		}
	}
	block, err := TypedState(agent)
	if err != nil {
		t.Fatal(err)
	}
	if block.InputEmpty != "not given." || !strings.Contains(livekit, `_INPUT_EMPTY = "not given."`) {
		t.Errorf("an input with no value does not render as the words the runbook quotes: %q", block.InputEmpty)
	}
	if !strings.Contains(livekit, "return _INPUT_EMPTY if name in _INPUT_NAMES else _STATE_EMPTY") {
		t.Error("an empty input renders as the state block's words rather than its own")
	}
	if !strings.Contains(livekit, "_INPUT_NAMES = {\"kind\", \"note\", \"outcome\", \"problem\", \"thing\"}") {
		t.Error("the worded set does not hold every input name")
	}
}

// TestInputPromptsReadWholeWithEveryValueEmpty extends the state block's rule
// to inputs: every prompt naming one reads as a sentence with that value
// rendered as words, which is exactly the state a step is entered in when the
// agent left an optional input out.
func TestInputPromptsReadWholeWithEveryValueEmpty(t *testing.T) {
	agent := loadTypedInputs(t)
	var names []string
	for _, field := range InputStateFields(agent) {
		names = append(names, field.Name)
	}
	prompts := append(mapValues(promptBodies(agent.Agents)), mapValues(taskBodies(agent.Tasks))...)
	checked := 0
	for _, prompt := range prompts {
		rendered := prompt
		for _, name := range names {
			rendered = strings.ReplaceAll(rendered, "{{"+name+"}}", ir.InputEmptyText())
		}
		if rendered == prompt {
			continue
		}
		checked++
		for _, line := range strings.Split(rendered, "\n") {
			if strings.Contains(line, "None") || strings.Contains(line, "[]") || strings.Contains(line, "null") {
				t.Errorf("a prompt renders a null where an input was left out: %q", line)
			}
			for _, dangling := range []string{" is .", " on .", " for .", " at .", " to .", ": .", " is ?", " kind ."} {
				if strings.Contains(line, dangling) {
					t.Errorf("a prompt has a dangling %q where an input was left out: %q", strings.TrimSpace(dangling), line)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no prompt names an input, so this gate proves nothing")
	}
}

// TestAHandoffWritesTheBriefBeforeTheReceiverExists is US2 on both targets and
// on both seams a handoff can sit on: the brief is validated, the receiver's
// every input is reset, this handoff's values are written, and only then is
// the receiver built or activated.
func TestAHandoffWritesTheBriefBeforeTheReceiverExists(t *testing.T) {
	agent := loadTypedInputs(t)
	livekit := emitted(t, agent, ir.ProviderLiveKit)
	front := after(t, livekit, "class Front(")
	ordered(t, "livekit agent handoff", method(t, front, "    async def to_specialist("),
		"problem: str,", "thing: Annotated[", "] = None",
		`_typed_inputs(`, `"to_specialist", {"problem": problem, "thing": thing}`,
		`"refused": f"Not started: {refused.message}. Ask the caller, then hand over again with a value that fits."`,
		"ctx.userdata.problem = None", "ctx.userdata.thing = None",
		"for _name, _value in _inputs.items():", "setattr(ctx.userdata, _name, _value)",
		"return Specialist(")
	ordered(t, "livekit in-task handoff", method(t, after(t, livekit, "class DoThing("), "    async def to_specialist("),
		"problem: str,",
		"if not self._claim_terminal():",
		`_typed_inputs(`, "self._terminal_claimed = False",
		"ctx.userdata.problem = None", "setattr(ctx.userdata, _name, _value)",
		"self.complete(_TaskTransfer(Specialist(")
	ordered(t, "livekit return handoff", method(t, after(t, livekit, "class Specialist("), "    async def to_front("),
		"outcome: str", `_typed_inputs(`, "ctx.userdata.outcome = None", "setattr(ctx.userdata, _name, _value)", "return Front(")

	pipecat := emitted(t, agent, ir.ProviderPipecat)
	ordered(t, "pipecat agent handoff", method(t, after(t, pipecat, "class FrontAgent("), "    async def to_specialist("),
		"problem: str,",
		`_typed_inputs("to_specialist", params.arguments)`,
		`"refused": f"Not started: {refused.message}. Ask the caller, then hand over again with a value that fits."`,
		"self.state.problem = None", "self.state.thing = None",
		"setattr(self.state, _name, _value)",
		"await self.activate_worker(", `"specialist",`)
	ordered(t, "pipecat in-task handoff", method(t, pipecat, "    async def _do_thing_transfer_do_thing_to_specialist(self, args, flow_manager):"),
		`_typed_inputs("to_specialist", dict(args))`,
		"self.state.problem = None", "setattr(self.state, _name, _value)",
		"await self.activate_worker(", `"specialist",`)
	// The in-task handoff's node schema carries the brief too.
	ordered(t, "pipecat in-task schema", method(t, pipecat, "    def _do_thing_node_do_thing(self) -> NodeConfig:"),
		`name="to_specialist",`, `"problem": _schema(_INPUT_TYPES["to_specialist"]["problem"])`, `required=["problem"],`)
}

// TestAReceivingAgentsBriefSurvivesItsOwnStep is research section 10: the
// owner's re-render paths read the shared state, where the brief still is,
// and nothing on the step's return path clears it. This fixture thinks through
// the router, so on LiveKit the owner's prompt is substituted per request from
// the state object and there is no local re-render to check; the local path is
// held over the verification package, whose owner re-renders on its own steps.
func TestAReceivingAgentsBriefSurvivesItsOwnStep(t *testing.T) {
	agent := loadTypedInputs(t)
	livekit := emitted(t, agent, ir.ProviderLiveKit)
	front := after(t, livekit, "class Front(")
	front = front[:strings.Index(front, "class Specialist(")]
	if strings.Contains(front, "ctx.userdata.outcome = None") {
		t.Error("livekit: the front desk clears its own brief on a path that is not a handoff")
	}
	if strings.Contains(method(t, front, "    async def do_thing("), "ctx.userdata.outcome") {
		t.Error("livekit: the front desk's step touches the front desk's brief")
	}
	pipecat := emitted(t, agent, ir.ProviderPipecat)
	frontWorker := after(t, pipecat, "class FrontAgent(")
	frontWorker = frontWorker[:strings.Index(frontWorker, "class SpecialistAgent(")]
	if strings.Contains(frontWorker, "self.state.outcome = None") {
		t.Error("pipecat: the front desk clears its own brief on a path that is not a handoff")
	}
	// On Pipecat the owner's prompt is set again when its step completes, after
	// the step's inputs were put back; on the router path the prompt is the
	// constant and the router reads the state per request.
	ordered(t, "pipecat completion", method(t, frontWorker, "    async def _do_thing_complete_do_thing(self):"),
		"self._do_thing_previous.items()", "system_instruction=")
}

// TestPipecatWorkerWithABriefAndNoStepsRerendersOnActivation is research
// section 11: the specialist has no steps, and before this feature that meant
// no on_activated at all, so its prompt would have been the one rendered at
// construction, before any brief existed.
func TestPipecatWorkerWithABriefAndNoStepsRerendersOnActivation(t *testing.T) {
	agent := loadTypedInputs(t)
	if len(agent.Agents["specialist"].Tools) != 1 {
		t.Fatalf("the specialist should hold one handoff and no step, got %v", agent.Agents["specialist"].Tools)
	}
	pipecat := emitted(t, agent, ir.ProviderPipecat)
	specialist := after(t, pipecat, "class SpecialistAgent(")
	if end := strings.Index(specialist[1:], "\nclass "); end >= 0 {
		specialist = specialist[:end+1]
	}
	ordered(t, "pipecat specialist", specialist,
		"async def on_activated(self, args) -> None:",
		"a worker with a brief renders it here",
		"system_instruction=SPECIALIST_PROMPT")
}

// TestSlngRouterCarriesInputNamesOnBothTargets is research section 8: the
// input placeholders are in the built prompts, so they join the names the
// router is given on both targets, and a name outside its visit renders empty.
func TestSlngRouterCarriesInputNamesOnBothTargets(t *testing.T) {
	agent := loadTypedInputs(t)
	pipecat := emitted(t, agent, ir.ProviderPipecat)
	if !strings.Contains(pipecat, `slng_variable_names=("kind", "note", "notes", "outcome", "problem", "thing"),`) {
		t.Error("pipecat: the per-worker template-variable names do not hold every input")
	}
	livekit := emitted(t, agent, ir.ProviderLiveKit)
	body := after(t, livekit, `"template_variables": _slng_template_variables(`)
	for _, name := range []string{`"kind"`, `"note"`, `"outcome"`, `"problem"`, `"thing"`} {
		if !strings.Contains(body[:400], name) {
			t.Errorf("livekit: the router request body does not carry %s", name)
		}
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		helper := functionBody(t, emitted(t, agent, provider), "def _slng_template_variables(state, names) -> dict:")
		if !strings.Contains(helper, "getattr(state, name, None)") || !strings.Contains(helper, "_state_text(name, value)") {
			t.Errorf("%s: the router path does not read the state object through _state_text", provider)
		}
	}
}

// TestRunbookNamesTheRequestBlockPerReceiver is the first of the four
// documentation surfaces: the emitted runbook shows the block and names what
// each seam is handed, on both targets, and a package with none says nothing.
func TestRunbookNamesTheRequestBlockPerReceiver(t *testing.T) {
	agent := loadTypedInputs(t)
	for path, text := range emittedText(t, agent) {
		if !strings.HasSuffix(path, "README.md") {
			continue
		}
		for _, want := range []string{
			"## Request block",
			"# task do_thing\nRequest:\nWhat you were handed for this visit. It does not change while you run.\n1. Kind: {{kind}}",
			"# handoff to_specialist, read by specialist\nRequest:",
			"- `do_thing` (step): `kind` (Literal[\"a\", \"b\"]) `note` (str | None, optional) `thing` (Thing | None, optional)",
			"- `to_specialist` (handoff, read by `specialist`): `problem` (str) `thing` (Thing | None, optional)",
			"renders as `not given.`",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not say %q", path, want)
			}
		}
	}
	for path, text := range emittedText(t, loadTypedState(t)) {
		if strings.HasSuffix(path, "README.md") && strings.Contains(text, "## Request block") {
			t.Errorf("%s documents a request block for a package handing nothing in", path)
		}
	}
}
