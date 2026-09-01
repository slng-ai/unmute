package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// announcingAgent is the fixture with a cover line and a prerequisite on the same
// step, which is the only shape that can distinguish "spoken on entry" from
// "spoken whatever happens".
func announcingAgent(t *testing.T) *ir.Agent {
	t.Helper()
	agent := prefetchFixture(t)
	delegate, ok := agent.Controls["verify_caller"].(*ir.Delegate)
	if !ok {
		t.Fatalf("verify_caller is not a delegate: %T", agent.Controls["verify_caller"])
	}
	if delegate.Announce == "" {
		t.Fatal("the fixture stopped carrying a delegate announcement")
	}
	delegate.Requires = []string{"customer_id"}
	return agent
}

// FR-034. The line is spoken on entry and not when the step is refused: a caller
// who hears "one moment while I check" and is then asked for a phone number has
// been told something untrue. The two orderings differ by one line in a template,
// so this is the gate that holds them apart.
func TestDelegateAnnounceComesAfterTheGuard(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		file     string
		say      string
	}{
		{ir.ProviderLiveKit, "agent.py", `self.session.say("One moment while I check.")`},
		{ir.ProviderPipecat, "bot.py", `TTSSpeakFrame("One moment while I check.")`},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			agent := announcingAgent(t)
			artifact, err := Generate(agent, targetByProvider(t, agent, tc.provider), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			py := artifactFile(t, artifact, tc.file)
			if got := strings.Count(py, tc.say); got != 1 {
				t.Fatalf("the cover line is emitted %d times, want exactly 1", got)
			}
			// The guard returns before reaching the line, so a refused step is
			// silent. Read from the start of the delegate's own method, because the
			// module carries other guards and other announcements.
			method := delegateMethodOf(t, py, "verify_caller")
			assertBefore(t, method, "_unmet_prerequisites(", tc.say)
			// And the refusal really does return before the line.
			assertBefore(t, method, "_prerequisite_refusal(", tc.say)
		})
	}
}

// A delegate with no announce: emits no line and no blank line either, which is
// what keeps every existing golden file where it is. The blank-line half is not
// fussiness: the first version of this feature rendered its partial
// unconditionally and moved three goldens, one of them on the slng target, which
// has nothing to do with delegates at all.
func TestDelegateWithoutAnnounceEmitsNothing(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		file     string
	}{
		{ir.ProviderLiveKit, "agent.py"},
		{ir.ProviderPipecat, "bot.py"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			agent := prefetchFixture(t)
			delegate, ok := agent.Controls["verify_caller"].(*ir.Delegate)
			if !ok {
				t.Fatalf("verify_caller is not a delegate: %T", agent.Controls["verify_caller"])
			}
			delegate.Announce = ""
			artifact, err := Generate(agent, targetByProvider(t, agent, tc.provider), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			method := delegateMethodOf(t, artifactFile(t, artifact, tc.file), "verify_caller")
			if strings.Contains(method, "One moment while I check.") {
				t.Error("the line survived the field being cleared")
			}
			// Whether the partial leaves a blank line behind is held by the golden
			// files rather than here, and held better: TestLiveKitV1RemyGolden,
			// TestLiveKitV1UnconfiguredGolden and TestSlngRouterGolden all moved by
			// exactly one blank line the first time this shipped, which is how the
			// bug was found. Pipecat method bodies carry legitimate blank lines, so
			// a blanket check here would be noise rather than a gate.
			if strings.Contains(method, "session.say(") || strings.Contains(method, "TTSSpeakFrame(") {
				t.Errorf("the method speaks with no announce: declared:\n%s", method)
			}
		})
	}
}

// delegateMethodOf returns one delegate's generated method body, so an ordering
// assertion cannot pass on a guard or an announcement belonging to another step.
func delegateMethodOf(t *testing.T, py, name string) string {
	t.Helper()
	from := strings.Index(py, "def "+name+"(self")
	if from < 0 {
		// Pipecat spells the same thing as a module-scope handler on the worker.
		from = strings.Index(py, "async def "+name+"(self")
	}
	if from < 0 {
		t.Fatalf("no generated method for delegate %q", name)
	}
	rest := py[from:]
	// The method ends at whichever comes first: the next decorated method, the
	// next top-level definition, or a module-level blank run.
	end := len(rest)
	for _, terminator := range []string{"\n    @", "\nclass ", "\n\n\n", "\ndef ", "\nasync def "} {
		if at := strings.Index(rest, terminator); at > 0 && at < end {
			end = at
		}
	}
	return rest[:end]
}
