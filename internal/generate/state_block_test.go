package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// TestStateBlockRendersJSONAndNotARepr is the half of FR-005 that lives in the
// emitted module, and the half of Fail Loud that a truncation could break in
// silence.
//
// Both render paths stringified with str(), which prints a Python repr for a
// structured value: single quotes, None rather than null, and nothing a
// provider ever produced. The bound is measured on the JSON rendering, because
// the JSON is what the model receives and the two lengths differ.
func TestStateBlockRendersJSONAndNotARepr(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		for _, want := range []string{
			`json.dumps(_plain(value), separators=(",", ":"), ensure_ascii=False)`,
			"_STATE_VALUE_MAX",
			"value = _state_text(*_state_lookup(",
			"text = _state_text(*_state_lookup(state, name))",
		} {
			if !strings.Contains(module, want) {
				t.Errorf("%s does not emit %q, so a declared value reaches a prompt as a Python repr",
					provider, want)
			}
		}
		// The bound is measured after the JSON rendering, which is what
		// _state_text does: the length check is inside it, below the dumps.
		body := functionBody(t, module, "def _state_text(name, value):")
		dumps := strings.Index(body, "json.dumps")
		bound := strings.Index(body, "len(text) > _STATE_VALUE_MAX")
		if dumps < 0 || bound < 0 || bound < dumps {
			t.Errorf("%s measures the bound before rendering the JSON, so a structured value is bounded by "+
				"the length of its repr:\n%s", provider, body)
		}
		// Shortened, never dropped, and never in silence.
		if !strings.Contains(body, "logger.warning(") {
			t.Errorf("%s shortens a value with no warning, which is the hidden downgrade Fail Loud forbids:\n%s",
				provider, body)
		}
		if !strings.Contains(body, "text[:_STATE_VALUE_MAX]") {
			t.Errorf("%s does not shorten the value it warned about:\n%s", provider, body)
		}
	}
}

// TestStateTextRendersEmptyNotNoneForABarePrimitive is what checkTemplates'
// deleted read restriction used to make moot: once an agent prompt or a task
// prompt may name any declared variable (gaps 2 and 3 of the scoped variables
// feature), a plain str/int/bool/float variable with nothing in it yet is a
// realistic render, not only a structured one. _state_text's own fallback
// already covers it unconditionally, for a name outside _STATE_STRUCTURED as
// much as for one inside it: `text = "" if value is None else str(value)` runs
// last regardless, so a bare primitive holding Python None renders as an empty
// string and never the word "None". This holds it so a future change to the
// structured branch cannot silently reintroduce a repr for the bare case.
func TestStateTextRendersEmptyNotNoneForABarePrimitive(t *testing.T) {
	agent := loadTypedState(t)
	block, err := TypedState(agent)
	if err != nil {
		t.Fatal(err)
	}
	body := functionBody(t, block.Source, "def _state_text(name, value):")
	if body == "" {
		t.Fatal("no _state_text emitted, so this gate proves nothing")
	}
	// At the function's own indent, not nested inside `if name in
	// _STATE_STRUCTURED:`, so it runs whether or not the name is declared
	// structured.
	if !strings.Contains(body, "\n    text = \"\" if value is None else str(value)\n") {
		t.Errorf("_state_text does not unconditionally fall back to an empty string for None:\n%s", body)
	}
}

// TestStateBlockWarningCarriesNoLibrarySpecificPlaceholder is the same rule the
// pre-fetch log lines carry, for the same reason: this warning is emitted into
// both modules from one place, LiveKit logs through stdlib `logging` and Pipecat
// through loguru, and either style prints literally on the other target.
func TestStateBlockWarningCarriesNoLibrarySpecificPlaceholder(t *testing.T) {
	agent := loadTypedState(t)
	block, err := TypedState(agent)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(block.Source, "\n") {
		if !strings.Contains(line, "declared state:") && !strings.Contains(line, "_STATE_VALUE_MAX}") {
			continue
		}
		if strings.Contains(line, "%s") || strings.Contains(line, "%d") {
			t.Errorf("the shared warning uses a stdlib placeholder, which loguru prints literally: %q", line)
		}
		if strings.Contains(line, ".format(") || strings.Contains(line, "{}") {
			t.Errorf("the shared warning uses a loguru placeholder, which stdlib logging prints literally: %q", line)
		}
	}
}

// TestStateBlockPromptsReadWholeWithEveryValueEmpty is FR-005a, extending the
// gate the pre-fetched values already have.
//
// A declared value with no contents renders as words, so every sentence naming
// it reads whole. The failure this prevents reaches a caller as speech: "your
// appointments are None".
func TestStateBlockPromptsReadWholeWithEveryValueEmpty(t *testing.T) {
	agent := loadTypedState(t)
	var declared []string
	for name, variable := range agent.Variables {
		if variable.Shape != nil {
			declared = append(declared, name)
		}
	}
	if len(declared) == 0 {
		t.Fatal("the fixture declares nothing structured, so this gate proves nothing")
	}
	// Exactly the state a call renders at its first turn: every declared value
	// empty, so every one of them renders as words.
	prompts := append(mapValues(promptBodies(agent.Agents)), mapValues(taskBodies(agent.Tasks))...)
	for _, prompt := range prompts {
		rendered := prompt
		for _, name := range declared {
			rendered = strings.ReplaceAll(rendered, "{{"+name+"}}", ir.StateEmptyText())
		}
		if rendered == prompt {
			continue // this prompt names none of them
		}
		for _, line := range strings.Split(rendered, "\n") {
			if strings.Contains(line, "None") {
				t.Errorf("a prompt renders the word None where a declared value was empty: %q", line)
			}
			if strings.Contains(line, "[]") || strings.Contains(line, "null") {
				t.Errorf("a prompt renders an empty structure where a declared value was empty, and a step "+
					"cannot tell that from a decision the caller made: %q", line)
			}
			for _, dangling := range []string{" is .", " on .", " for .", " at .", " to .", ": .", " is ?"} {
				if strings.Contains(line, dangling) {
					t.Errorf("a prompt has a dangling %q where a declared value was empty: %q",
						strings.TrimSpace(dangling), line)
				}
			}
		}
	}
}

// functionBody returns one emitted function, from its `def` line to the next
// top-level statement, so a test asserts on the function it means rather than
// on the whole module.
func functionBody(t *testing.T, module, signature string) string {
	t.Helper()
	at := strings.Index(module, signature)
	if at < 0 {
		t.Fatalf("the module emits no %q", signature)
	}
	rest := module[at:]
	lines := strings.Split(rest, "\n")
	var body []string
	for i, line := range lines {
		if i > 0 && line != "" && !strings.HasPrefix(line, " ") {
			break
		}
		body = append(body, line)
	}
	return strings.Join(body, "\n")
}
