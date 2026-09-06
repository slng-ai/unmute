package ir

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// inputsPackage is the typed-inputs fixture with one edit applied to its
// agent.yaml, loaded from a real directory so every refusal is checked against
// a file with positions rather than a struct with none. An edit that changes
// nothing loads the fixture as it is.
func inputsPackage(t *testing.T, edit func(yaml string) string) *packagespec.Package {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "testdata", "typed_inputs"))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "agent.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := edit(string(raw))
	if edited == string(raw) && edit != nil {
		// A case whose edit did not land tests the fixture, not the refusal.
		t.Fatalf("the edit changed nothing in agent.yaml")
	}
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, err := packagespec.Load(root)
	if err != nil {
		t.Fatalf("the edited fixture does not load: %v", err)
	}
	return pkg
}

func inputsAgent(t *testing.T) *Agent {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "typed_inputs"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func replaceOnce(t *testing.T, old, new string) func(string) string {
	t.Helper()
	return func(yaml string) string {
		if !strings.Contains(yaml, old) {
			t.Fatalf("the fixture holds no %q", old)
		}
		return strings.Replace(yaml, old, new, 1)
	}
}

func inputNames(inputs []InputField) []string {
	names := make([]string, 0, len(inputs))
	for _, input := range inputs {
		names = append(names, input.Name)
	}
	return names
}

// TestBuildResolvesInputsInAuthoredOrder is FR-001, FR-002 and FR-004 at the
// resolved layer: every input carries its resolved type, its optionality from
// `| None`, its description and its authored position, and a receiving agent's
// brief is the union of the handoffs that target it.
func TestBuildResolvesInputsInAuthoredOrder(t *testing.T) {
	agent := inputsAgent(t)
	step := agent.Tasks["do_thing"].Inputs
	if got, want := inputNames(step), []string{"kind", "note", "thing"}; !slices.Equal(got, want) {
		t.Fatalf("do_thing inputs = %v, want %v in authored order", got, want)
	}
	if kind := step[0]; kind.Optional || !slices.Equal(kind.Type.Literal, []string{"a", "b"}) {
		t.Errorf("kind = %+v, want a required Literal of a and b", kind)
	}
	if note := step[1]; !note.Optional || note.Type.Primitive != PrimitiveString || note.Description == "" {
		t.Errorf("note = %+v, want an optional str carrying its description", note)
	}
	if thing := step[2]; !thing.Optional || thing.Type.Shape != "Thing" {
		t.Errorf("thing = %+v, want an optional Thing", thing)
	}
	if got := inputNames(agent.Tasks["check"].Inputs); len(got) != 0 {
		t.Errorf("check declares no input and resolved %v", got)
	}
	handoff := agent.Controls["to_specialist"].(*AgentTransfer)
	if got, want := inputNames(handoff.Inputs), []string{"problem", "thing"}; !slices.Equal(got, want) {
		t.Errorf("to_specialist inputs = %v, want %v", got, want)
	}
	if got, want := inputNames(agent.Agents["specialist"].Inputs), []string{"problem", "thing"}; !slices.Equal(got, want) {
		t.Errorf("specialist brief = %v, want %v, the union of the handoffs that target it", got, want)
	}
	if got, want := inputNames(agent.Agents["front"].Inputs), []string{"outcome"}; !slices.Equal(got, want) {
		t.Errorf("front brief = %v, want %v", got, want)
	}
}

// TestBuildRefusesEveryCollidingInputName is FR-008 and decision 2 of the task
// list. Each refusal names both things that clash and says what to do, because
// at run time the name is a field on the shared call state and a collision
// would overwrite the other value for the visit.
func TestBuildRefusesEveryCollidingInputName(t *testing.T) {
	const kind = `- kind: Literal["a", "b"]`
	for _, tc := range []struct {
		name string
		edit func(string) string
		want []string
	}{
		{"a declared variable", replaceOnce(t, kind, "- notes: str"), []string{`task "do_thing" expects "notes"`, "a declared variable", "rename it"}},
		{"a word of the type grammar", replaceOnce(t, kind, "- str: str"), []string{`expects "str"`, "a word of the type grammar"}},
		{"the reserved result field", replaceOnce(t, kind, "- unserved_request: str"), []string{`expects "unserved_request"`, "reserved result field"}},
		{"not a placeholder name", replaceOnce(t, kind, "- Kind: str"), []string{`expects "Kind"`, "lowercase words joined by underscores"}},
		{"declared twice on one site", replaceOnce(t, kind, kind+"\n          - kind: str"), []string{`expects "kind" twice`}},
		// Tasks resolve before handoffs, so the handoff is where the second
		// spelling is found and refused, naming the task that spelled it first.
		{"one name, two types across the package", replaceOnce(t, kind, kind+"\n          - problem: int"), []string{`handoff "to_specialist" expects "problem" as str`, `task "do_thing" expects it as int`, "one name has one type"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := inputsPackage(t, tc.edit)
			_, err := Build(pkg)
			if err == nil {
				t.Fatal("built, want a refusal")
			}
			for _, fragment := range tc.want {
				if !strings.Contains(err.Error(), fragment) {
					t.Errorf("refusal does not say %q:\n%v", fragment, err)
				}
			}
			if !strings.Contains(err.Error(), "agent.yaml:") {
				t.Errorf("refusal names no line:\n%v", err)
			}
		})
	}
}

// TestBuildRefusesInputOnAGroupedTask is research section 9: a group is entered
// by one call that cannot say which step a value is for.
func TestBuildRefusesInputOnAGroupedTask(t *testing.T) {
	pkg := inputsPackage(t, func(yaml string) string {
		yaml = strings.Replace(yaml, "    handoffs:\n      - to_specialist\n\n  specialist:", "    handoffs:\n      - to_specialist\n    task_groups:\n      - both\n\n  specialist:", 1)
		return strings.Replace(yaml, "handoffs:\n  to_specialist:", "task_groups:\n  both:\n    when: The caller wants both.\n    steps:\n      - do_thing\n      - check\n    context_scope: shared\n    then: return\n    merge: results\n\nhandoffs:\n  to_specialist:", 1)
	})
	_, err := Build(pkg)
	if err == nil {
		t.Fatal("built, want a refusal")
	}
	for _, fragment := range []string{`task "do_thing" declares expect:`, `task group "both"`, "a grouped task expects nothing"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("refusal does not say %q:\n%v", fragment, err)
		}
	}
}

// TestBuildRefusesAnInputTypeOutsideTheGrammarWithItsColumn is FR-001's last
// clause: the refusal joins the file, the line and the column inside the
// expression, the way a variable's does.
func TestBuildRefusesAnInputTypeOutsideTheGrammarWithItsColumn(t *testing.T) {
	pkg := inputsPackage(t, replaceOnce(t, `- kind: Literal["a", "b"]`, `- kind: list[Literal["a", "b"], Nothing]`))
	_, err := Build(pkg)
	if err == nil {
		t.Fatal("built, want a refusal")
	}
	line := lineOf(t, pkg, `Nothing`)
	for _, fragment := range []string{"agent.yaml:" + strconv.Itoa(line), `task "do_thing" expects "kind"`, "column"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("refusal does not say %q:\n%v", fragment, err)
		}
	}
}

var inputPlaceholder = regexp.MustCompile(`\{\{([a-z_]+)\}\}`)

// blockOf is the text of the request block in one prompt, or "" when the
// prompt carries none.
func blockOf(prompt string) string {
	at := strings.Index(prompt, InputBlockHeading+"\n")
	if at < 0 {
		return ""
	}
	return prompt[at:]
}

// TestInputBlockReachesTheReceivingPromptAfterTheStateBlockInAuthoredOrder is
// FR-005 and SC-004: the block is at the end of the receiving prompt, after
// the state block, numbers the inputs in the order they were written, and no
// prompt file was edited to put it there.
func TestInputBlockReachesTheReceivingPromptAfterTheStateBlockInAuthoredOrder(t *testing.T) {
	agent := inputsAgent(t)
	for _, tc := range []struct {
		where, prompt string
		want          []string
	}{
		{"task do_thing", agent.Tasks["do_thing"].Instructions, []string{"kind", "note", "thing"}},
		{"agent specialist", agent.Agents["specialist"].Instructions, []string{"problem", "thing"}},
		{"agent front", agent.Agents["front"].Instructions, []string{"outcome"}},
	} {
		block := blockOf(tc.prompt)
		if block == "" {
			t.Errorf("%s carries no request block:\n%s", tc.where, tc.prompt)
			continue
		}
		if !strings.Contains(block, InputBlockNote) {
			t.Errorf("%s carries the block with no line saying what it is", tc.where)
		}
		state := strings.Index(tc.prompt, StateBlockHeading)
		if state < 0 || state > strings.Index(tc.prompt, InputBlockHeading) {
			t.Errorf("%s does not place the request block after the state block", tc.where)
		}
		var order []string
		for _, line := range strings.Split(block, "\n") {
			if !numberedLine.MatchString(line) {
				continue
			}
			order = append(order, inputPlaceholder.FindStringSubmatch(line)[1])
		}
		if !slices.Equal(order, tc.want) {
			t.Errorf("%s numbers %v, want %v", tc.where, order, tc.want)
		}
		if !strings.Contains(block, "1. Kind: {{kind}}") && tc.where == "task do_thing" {
			t.Errorf("the label is not the name as a sentence reads it:\n%s", block)
		}
	}
}

// TestInputBlockReachesNoOtherPrompt is the other half of FR-005: a step with
// no inputs gets no block, and an agent's block holds its own brief and never a
// step's request.
func TestInputBlockReachesNoOtherPrompt(t *testing.T) {
	agent := inputsAgent(t)
	if block := blockOf(agent.Tasks["check"].Instructions); block != "" {
		t.Errorf("check declares no input and carries a block:\n%s", block)
	}
	if agent.InputBlock(TaskPromptSite("check")) != "" || InputBlock(nil) != "" {
		t.Error("a site with no inputs composes a block")
	}
	front := agent.Agents["front"].Instructions
	for _, stray := range []string{"{{kind}}", "{{note}}", "{{thing}}", "{{problem}}"} {
		if strings.Contains(front, stray) {
			t.Errorf("the front desk's prompt names %s, which is somebody else's input", stray)
		}
	}
	if agent.InputBlock(AgentPromptSite("front")) != blockOf(front) {
		t.Error("the agent's InputBlock and the composed prompt disagree")
	}
}

// TestInputBlockNamesNoDottedPath is the state block's rule, for the same
// reason: the emitted substitution regex tokenises flat identifiers only.
func TestInputBlockNamesNoDottedPath(t *testing.T) {
	agent := inputsAgent(t)
	for name, task := range agent.Tasks {
		for _, match := range regexp.MustCompile(`\{\{[^}]*\}\}`).FindAllString(blockOf(task.Instructions), -1) {
			if strings.Contains(match, ".") {
				t.Errorf("task %s block carries %s, which the runtime cannot substitute", name, match)
			}
		}
	}
}

// TestCheckTemplatesRefusesAPromptNamingAnInputItWasNotHanded is the
// compile-time half of FR-005, and nothing else keeps a step's request out of
// its parent's prompt: at run time the value is on the shared call state.
func TestCheckTemplatesRefusesAPromptNamingAnInputItWasNotHanded(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "testdata", "typed_inputs"))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "instructions.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, []byte("\nThe caller wants kind {{kind}}.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, err := packagespec.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Build(pkg)
	if err == nil {
		t.Fatal("built with the front desk reading the step's input")
	}
	for _, fragment := range []string{`agent "front" instructions references {{kind}}`, `task "do_thing" instructions expects to be handed`, "Only the prompt that expects it may read it"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("refusal does not say %q:\n%v", fragment, err)
		}
	}
}

// TestToolInjectMayReadARequiredInputOfEverySiteItIsAttachedTo is the rule for
// a hidden tool argument. The fixture's tool injects the step's required input
// and passes. A tool attached to a site with no such input is refused naming
// that site, and an optional input is refused because an injected value cannot
// be asked for.
func TestToolInjectMayReadARequiredInputOfEverySiteItIsAttachedTo(t *testing.T) {
	agent := inputsAgent(t)
	if _, ok := agent.Tools["note_thing"]; !ok {
		t.Fatal("the fixture no longer attaches note_thing, so this gate proves nothing")
	}
	for _, tc := range []struct {
		name string
		edit func(string) string
		want []string
	}{
		{
			"attached to an agent handed no such input",
			replaceOnce(t, "    instructions: instructions.md\n    think: reasoning\n    speak: voice\n    tasks:",
				"    instructions: instructions.md\n    think: reasoning\n    speak: voice\n    tools:\n      - note_thing\n    tasks:"),
			[]string{`tool "note_thing" injects {{kind}}`, `attached to agent "front", which is handed no kind`},
		},
		{
			"an optional input",
			func(yaml string) string {
				yaml = strings.Replace(yaml, "- kind: Literal[\"a\", \"b\"]", "- name: kind\n            type: Literal[\"a\", \"b\"] | None\n            description: Which kind, if said.", 1)
				return yaml
			},
			[]string{`tool "note_thing" injects {{kind}}`, `task "do_thing" declares as optional`, "make the value required"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := inputsPackage(t, tc.edit)
			_, err := Build(pkg)
			if err == nil {
				t.Fatal("built, want a refusal")
			}
			for _, fragment := range tc.want {
				if !strings.Contains(err.Error(), fragment) {
					t.Errorf("refusal does not say %q:\n%v", fragment, err)
				}
			}
		})
	}
}

// TestSlngRefusesAnInput is FR-010's hosted clause: the slng target emits no
// module, so there is nothing to hand a value to, and the row says so and
// names the alternative. The two code targets validate the same package clean.
func TestSlngRefusesAnInput(t *testing.T) {
	agent := inputsAgent(t)
	hosted := targetFor(agent, ProviderLiveKit)
	hosted.Name, hosted.Provider = "slng", ProviderSlng
	report, _ := Validate(agent, []Target{hosted}, targetcap.Default())
	row := reportFor(report, ProviderSlng)
	joined := strings.Join(row.Errors, "\n")
	for _, fragment := range []string{"slng target", "expect: list", "compile to livekit or pipecat"} {
		if !strings.Contains(joined, fragment) {
			t.Errorf("no slng error says %q; got:\n%s", fragment, joined)
		}
	}
	for _, provider := range []Provider{ProviderLiveKit, ProviderPipecat} {
		report, _ := Validate(agent, []Target{targetFor(agent, provider)}, targetcap.Default())
		if row := reportFor(report, provider); len(row.Errors) > 0 {
			t.Errorf("%s refuses the fixture: %v", provider, row.Errors)
		}
	}
}
