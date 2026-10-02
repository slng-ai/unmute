package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// between returns the text from one marker up to the next, so a test asserts
// on the method it means rather than on the whole class.
func between(t *testing.T, source, from, to string) string {
	t.Helper()
	start := strings.Index(source, from)
	if start < 0 {
		t.Fatalf("the module emits no %q", from)
	}
	end := strings.Index(source[start:], to)
	if end < 0 {
		t.Fatalf("the module has no %q after %q", to, from)
	}
	return source[start : start+end]
}

// TestStateBlockRendersJSONAndNotARepr is the half of FR-005 that lives in the
// emitted module, and the half of Fail Loud that a truncation could break in
// silence.
//
// A prompt that stringified with str() would print a Python repr for a
// structured value: single quotes, None rather than null, and nothing a
// provider ever produced. The bound is measured on the JSON rendering, because
// the JSON is what the model receives and the two lengths differ.
func TestStateBlockRendersJSONAndNotARepr(t *testing.T) {
	agent := loadTypedState(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		module := emitted(t, agent, provider)
		for _, want := range []string{
			"text = value if isinstance(value, str) else to_json(value).decode()",
			"VALUE_MAX: ClassVar[int] = ",
			"CallState.render(",
		} {
			if !strings.Contains(module, want) {
				t.Errorf("%s does not emit %q, so a declared value reaches a prompt as a Python repr",
					provider, want)
			}
		}
		// The bound is measured after the JSON rendering.
		body := between(t, module, "    def render(", "    def save_result(")
		dumps := strings.Index(body, "to_json(value)")
		bound := strings.Index(body, "len(text) > cls.VALUE_MAX")
		if dumps < 0 || bound < 0 || bound < dumps {
			t.Errorf("%s measures the bound before rendering the JSON, so a structured value is bounded by "+
				"the length of its repr:\n%s", provider, body)
		}
		// Shortened, never dropped, and never in silence.
		if !strings.Contains(body, "logger.warning(") {
			t.Errorf("%s shortens a value with no warning, which is the hidden downgrade Fail Loud forbids:\n%s",
				provider, body)
		}
		if !strings.Contains(body, "text[: cls.VALUE_MAX]") {
			t.Errorf("%s does not shorten the value it warned about:\n%s", provider, body)
		}
	}
}

// TestStateTextRendersEmptyNotNoneForABarePrimitive is what checkTemplates'
// deleted read restriction used to make moot: once an agent prompt or a task
// prompt may name any declared variable, a plain str/int/bool/float variable
// with nothing in it yet is a realistic render, not only a structured one.
// render's own fallback covers it unconditionally. A bare primitive holding
// Python None renders the shared missing-value words and never the word
// "None".
func TestStateTextRendersEmptyNotNoneForABarePrimitive(t *testing.T) {
	agent := loadTypedState(t)
	block, err := TypedState(agent, "")
	if err != nil {
		t.Fatal(err)
	}
	body := between(t, block.Source, "    def render(", "    def save_result(")
	if !strings.Contains(body, "\n        if value is None or value == \"\":\n            return cls.EMPTY_TEXT\n") {
		t.Errorf("render does not unconditionally render a missing value as the shared empty text:\n%s", body)
	}
	if !strings.Contains(body, "if state is None:\n            return cls.EMPTY_TEXT") {
		t.Errorf("render does not read before the call has begun as empty:\n%s", body)
	}
}

// TestStateBlockWarningCarriesNoLibrarySpecificPlaceholder is the same rule the
// pre-fetch log lines carry, for the same reason: this warning is emitted into
// both modules from one place, LiveKit logs through stdlib `logging` and Pipecat
// through loguru, and either style prints literally on the other target.
func TestStateBlockWarningCarriesNoLibrarySpecificPlaceholder(t *testing.T) {
	agent := loadTypedState(t)
	block, err := TypedState(agent, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(block.Source, "\n") {
		if !strings.Contains(line, "declared state:") && !strings.Contains(line, "VALUE_MAX}") {
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
		if variable.Structured() {
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
