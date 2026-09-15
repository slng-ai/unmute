package ir

import (
	"strings"
	"testing"
)

// TestAClosedSetRefusalListsTheSet.
//
// A refusal for a value outside a closed set names what was written and what is
// allowed. Naming only the first leaves an author guessing at a list that
// exists, is short, and is right there in the compiler.
//
// Both of these did exactly that until an author writing examples/pharmacy-refills
// reached for `effect: writes_data`, which reads like a third member of a set
// whose other two are `returns_data` and `ends_conversation`. The refusal said
// the value was invalid and stopped.
//
// The list is read from the ordered accessor rather than written out here, so a
// value added to the set reaches this test and the refusal together. A copy in
// the test is the thing that goes stale first, because nothing fails when it
// does.
func TestAClosedSetRefusalListsTheSet(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(tool *Tool)
		allowed []string
	}{
		{"effect", func(tool *Tool) { tool.Effect = "writes_data" }, values(ToolEffectValues())},
		{"interruption", func(tool *Tool) { tool.Interruption = "pause" }, values(ToolInterruptionValues())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := liveAgent(t)
			tool := agent.Tools["lookup_customer"]
			tc.mutate(&tool)
			agent.Tools["lookup_customer"] = tool

			row := validateOne(t, agent, targetFor(agent, ProviderLiveKit))
			text := strings.Join(row.Errors, "\n")
			if !strings.Contains(text, "lookup_customer") {
				t.Fatalf("the refusal does not name the tool:\n%s", text)
			}
			for _, allowed := range tc.allowed {
				if !strings.Contains(text, allowed) {
					t.Errorf("the refusal does not offer %q, so the author has to find the set themselves:\n%s", allowed, text)
				}
			}
		})
	}
}

func values[T ~string](in []T) []string {
	out := make([]string, 0, len(in))
	for _, value := range in {
		out = append(out, string(value))
	}
	return out
}

// TestToolInterruptionWarnsOnlyOnARealDifference.
//
// CLAUDE.md allows the capability table exactly one Warn row, and requires it to
// name a difference an author can act on. This one named a setting instead.
//
// LiveKit runs a tool execution to completion, which is what `continue` asks
// for. Warning on `continue` told an author their preference was not enforced
// while the call behaved exactly as they wrote it, and a package targeting both
// runtimes is the normal shape, so it printed on every validate forever. The
// example that found it wrote `provider_default` purely to keep the output
// quiet, which is the wrong lesson for a shipped package to teach.
//
// `cancel` on LiveKit is a real difference and still warns.
func TestToolInterruptionWarnsOnlyOnARealDifference(t *testing.T) {
	warned := func(t *testing.T, provider Provider, value ToolInterruption) bool {
		t.Helper()
		agent := liveAgent(t)
		tool := agent.Tools["lookup_customer"]
		tool.Interruption = value
		agent.Tools["lookup_customer"] = tool
		row := validateOne(t, agent, targetFor(agent, provider))
		return strings.Contains(strings.Join(row.Warnings, "\n"), "tool executions to completion")
	}

	if warned(t, ProviderLiveKit, ToolContinue) {
		t.Error("livekit warned about `continue`, which is what it does anyway; the author cannot act on that")
	}
	if !warned(t, ProviderLiveKit, ToolCancel) {
		t.Error("livekit did not warn about `cancel`, which it genuinely does not do")
	}
	// The control: the runtime that reads the preference says nothing either way,
	// so a two-target package can state what it wants and stay quiet.
	for _, value := range []ToolInterruption{ToolContinue, ToolCancel} {
		if warned(t, ProviderPipecat, value) {
			t.Errorf("pipecat warned about %q, which it enforces", value)
		}
	}
}
