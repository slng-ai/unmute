package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

// safeCoreWithGreeting rebuilds safe_core with one greeting shape so each case
// starts from a clean IR rather than a mutated leftover.
func safeCoreWithGreeting(t *testing.T, greeting *ir.Greeting) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Conversation.Greeting = greeting
	return agent
}

// TestGreetingDefaultIsModelWrittenOnEveryDriver pins the whole point of the
// compiler: one package must not open a call differently depending on which
// target you compiled. With no conversation.greeting block, LiveKit and Pipecat
// both emit the model-written opening, with the same instruction, so the two
// cannot drift apart again (SCHEMA.md 4.8 / N20).
//
// "every driver" is literally the two that emit an opening. Vapi is managed and
// keeps its native default; Deepgram is a code target whose driver is unwritten,
// so a third case appears here only when that generator does.
func TestGreetingDefaultIsModelWrittenOnEveryDriver(t *testing.T) {
	const instruction = "Greet the caller and offer to help."

	// The default is not "some opening": it is exactly what an explicit
	// `speaks_first: agent` with no `text` produces. Comparing the whole file
	// says so far more strongly than grepping for a marker.
	t.Run("pipecat absent block equals explicit model-written", func(t *testing.T) {
		absent := safeCoreWithGreeting(t, nil)
		explicit := safeCoreWithGreeting(t, &ir.Greeting{SpeaksFirst: ir.SpeaksFirstAgent})

		absentBot := generatedFile(t, absent, ir.ProviderPipecat, "bot.py")
		explicitBot := generatedFile(t, explicit, ir.ProviderPipecat, "bot.py")
		if absentBot != explicitBot {
			t.Error("bot.py for an absent greeting block differs from an explicit model-written greeting")
		}
	})

	t.Run("both drivers use the same opening instruction", func(t *testing.T) {
		agent := safeCoreWithGreeting(t, nil)

		bot := generatedFile(t, agent, ir.ProviderPipecat, "bot.py")
		for _, want := range []string{`"content": ` + `"` + instruction + `"`, "run_llm=True"} {
			if !strings.Contains(bot, want) {
				t.Errorf("pipecat bot.py missing %q", want)
			}
		}
		if strings.Contains(bot, "TTSSpeakFrame") {
			t.Error("pipecat bot.py speaks a fixed line for an absent greeting block")
		}

		py := generatedFile(t, agent, ir.ProviderLiveKit, "agent.py")
		if want := `generate_reply(instructions="` + instruction + `")`; !strings.Contains(py, want) {
			t.Errorf("livekit agent.py missing %q", want)
		}
	})
}

// generatedFile compiles one agent for one provider and returns a single
// emitted file.
func generatedFile(t *testing.T, agent *ir.Agent, provider ir.Provider, path string) string {
	t.Helper()
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifactFile(t, artifact, path)
}
