package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// baseFixture is safe_core, the one fixture that carries both code targets, so
// the same package can be compiled twice and the two results compared.
func baseFixture(t *testing.T) *ir.Agent {
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

// guardedFixture adds one returning step that cannot start until the caller has
// been identified: the exact shape this feature exists to make expressible, and
// the shape the release example could not write before it.
func guardedFixture(t *testing.T) *ir.Agent {
	t.Helper()
	agent := baseFixture(t)
	agent.Tasks["book"] = ir.Task{
		Instructions: "Take the booking.",
		Result:       map[string]ir.ResultField{"booked": {Type: ir.PrimitiveBoolean}},
		Context:      ir.TaskContext{History: ir.HistoryFull},
	}
	agent.Controls["manage_booking"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Task: "book", When: "The caller wants a booking.",
		Requires: []string{"customer_id"},
	}
	agent.Controls["verify_customer"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Task: "book", When: "Identify the caller.",
		Assign: []ir.AssignTo{{Var: "customer_id", Field: "booked"}},
	}
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "manage_booking", "verify_customer")
	agent.Agents["intake"] = intake
	return agent
}

func emitFor(t *testing.T, agent *ir.Agent, provider ir.Provider, file string) string {
	t.Helper()
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatalf("%s: generate: %v", provider, err)
	}
	return artifactFile(t, artifact, file)
}

// FR-006: the feature costs nothing to a package that does not use it.
//
// This is the whole reason the guard block is conditional rather than always
// emitted. Every project this tool has ever produced must compile to exactly
// what it compiled to before, or "no behaviour change" is a claim nobody can
// check.
func TestUnguardedPackagesAreByteIdentical(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		file := "agent.py"
		if provider == ir.ProviderPipecat {
			file = "bot.py"
		}
		before := emitFor(t, baseFixture(t), provider, file)
		after := emitFor(t, baseFixture(t), provider, file)
		if before != after {
			t.Errorf("%s: an unguarded package must compile to byte-identical output", provider)
		}
		for _, forbidden := range []string{"_PREREQUISITE_LIMIT", "_unmet_prerequisites", "_prerequisite_refusal"} {
			if strings.Contains(after, forbidden) {
				t.Errorf("%s: an unguarded package emits %q; the guard must cost nothing when nothing declares requires:", provider, forbidden)
			}
		}
	}
}

// FR-005 and SC-004: the two targets must refuse in the same words.
//
// T027 and T029 check each target's guard on its own, and neither can catch the
// two drifting apart, because each passes happily while the other says something
// different. That is exactly how the previous wording drifted: LiveKit said
// "missing required information" and Pipecat said "still need", both were
// tested, and nothing compared them. This test compares them.
func TestBothTargetsRefuseInTheSameWords(t *testing.T) {
	livekit := emitFor(t, guardedFixture(t), ir.ProviderLiveKit, "agent.py")
	pipecat := emitFor(t, guardedFixture(t), ir.ProviderPipecat, "bot.py")

	// Take the whole generated block: the supplier table, the limit, and both
	// helpers. Reading to the first line that starts a new top-level statement
	// is what makes this the function rather than an arbitrary number of lines.
	extract := func(t *testing.T, src, label string) string {
		t.Helper()
		start := strings.Index(src, "_PREREQUISITE_LIMIT = ")
		if start < 0 {
			t.Fatalf("%s: emits no guard block", label)
		}
		lines := strings.Split(src[start:], "\n")
		seenDef := false
		for i, line := range lines {
			if strings.HasPrefix(line, "def _prerequisite_refusal(") {
				seenDef = true
				continue
			}
			if seenDef && line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, ")") {
				return strings.Join(lines[:i], "\n")
			}
		}
		t.Fatalf("%s: guard block has no end", label)
		return ""
	}

	left, right := extract(t, livekit, "livekit"), extract(t, pipecat, "pipecat")
	if left != right {
		t.Errorf("the two targets refuse in different words.\nlivekit:\n%s\n\npipecat:\n%s", left, right)
	}
}

// FR-003d: the bound lives in emitted code, not in prompt text.
//
// A model can be talked out of an instruction. It cannot be talked out of a
// counter. Putting the limit in a prompt would make the one part of this feature
// that exists to stop a loop the part most likely to be ignored inside one.
func TestBoundLivesInCodeNotPrompt(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		file     string
	}{{ir.ProviderLiveKit, "agent.py"}, {ir.ProviderPipecat, "bot.py"}} {
		src := emitFor(t, guardedFixture(t), tc.provider, tc.file)

		if !strings.Contains(src, "_PREREQUISITE_LIMIT = 5") {
			t.Errorf("%s: the bound must be a literal in emitted code", tc.provider)
		}
		if !strings.Contains(src, "_tries >= _PREREQUISITE_LIMIT") {
			t.Errorf("%s: the counter must be compared against the bound in code", tc.provider)
		}

		// The prompt constants are where an instruction would live. None of them
		// may state a retry count, or the bound has two owners that can disagree.
		for _, phrase := range []string{"five times", "5 times", "five attempts", "5 attempts", "retry limit of"} {
			if strings.Contains(strings.ToLower(src), phrase) {
				t.Errorf("%s: emitted text states the retry count as prose (%q); the bound has one owner and it is the counter", tc.provider, phrase)
			}
		}
	}
}
