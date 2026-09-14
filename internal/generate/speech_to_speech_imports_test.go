package generate

import (
	"regexp"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// The emitted project promises `ruff check .` passes, and its README tells the
// reader to run it. An imported name nothing calls is an F401, so a shape that
// emits one hands the reader a project that fails its own instructions on the
// first command, with a traceback-free error about a line the compiler wrote.
//
// Three shapes did, and each one is a combination rather than a feature:
//
//   - a live package whose caller speaks first, because the idle nudge moved to
//     a client event and the greeting is the frame's only other reader;
//   - a realtime package setting minimum_words, because the strategy it names
//     is built only where this project's own aggregator decides the turn;
//   - a half cascade with no greeting line, because dev_say's import followed
//     "is there a synthesizer" and its one remaining call site is the greeting.
//
// So this walks shapes rather than checking one import: the next combination is
// the one nobody thought of either.
var pythonImportLine = regexp.MustCompile(`(?m)^from ([\w.]+) import (.+)$`)

// unusedImports reports every name a module imports and never mentions again.
// A used name appears at least twice: once on its import line and once where it
// is called. Parenthesised and aliased forms are not emitted by these templates.
func unusedImports(module string) []string {
	var unused []string
	for _, match := range pythonImportLine.FindAllStringSubmatch(module, -1) {
		if match[1] == "__future__" {
			continue // a compiler directive, used by being present
		}
		for _, name := range strings.Split(match[2], ",") {
			name = strings.TrimSpace(name)
			if name == "" || strings.Contains(name, "(") || strings.Contains(name, " as ") {
				continue
			}
			if strings.Count(module, name) < 2 {
				unused = append(unused, name)
			}
		}
	}
	return unused
}

// silentCaller makes the caller speak first: the greeting stops being a line
// this project writes, which is what took the last reader off two imports.
func silentCaller(agent *ir.Agent, _ *ir.Target) {
	if agent.Conversation != nil && agent.Conversation.Greeting != nil {
		agent.Conversation.Greeting.SpeaksFirst = "user"
		agent.Conversation.Greeting.Text = ""
	}
}

func TestSpeechToSpeechImportsOnlyWhatItUses(t *testing.T) {
	for _, shape := range []struct {
		name    string
		module  func(*testing.T, func(*ir.Agent, *ir.Target)) string
		mutate  func(*ir.Agent, *ir.Target)
		fixture string
	}{
		{name: "livekit realtime, caller first", module: realtimeModule, mutate: silentCaller},
		{name: "livekit half cascade, caller first", module: realtimeModule, mutate: func(a *ir.Agent, tg *ir.Target) {
			bindHalfCascadeVoice(a, tg)
			silentCaller(a, tg)
		}},
		{name: "pipecat realtime, caller first", module: realtimePipecatBot, mutate: silentCaller},
		{name: "pipecat half cascade, caller first", module: realtimePipecatBot, mutate: func(a *ir.Agent, tg *ir.Target) {
			halfCascade(a, tg)
			silentCaller(a, tg)
		}},
	} {
		t.Run(shape.name, func(t *testing.T) {
			if unused := unusedImports(shape.module(t, shape.mutate)); len(unused) > 0 {
				t.Errorf("imported and never used: %s. The emitted project's own ruff gate fails on this, "+
					"so the reader's first command after compiling reports an error the compiler wrote",
					strings.Join(unused, ", "))
			}
		})
	}
}

// TestLiveImportsOnlyWhatItUses is the same question for the live architecture,
// which has its own fixture and no mutate-driven helper.
func TestLiveImportsOnlyWhatItUses(t *testing.T) {
	agent := agentFor(t, "live_model")
	silentCaller(agent, nil)
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		resolved := targetByProvider(t, agent, provider)
		artifact, err := Generate(agent, resolved, targetcap.Default())
		if err != nil {
			t.Fatal(err)
		}
		name := "bot.py"
		if provider == ir.ProviderLiveKit {
			name = "agent.py"
		}
		if unused := unusedImports(artifactFile(t, artifact, name)); len(unused) > 0 {
			t.Errorf("%s %s imports and never uses: %s", provider, name, strings.Join(unused, ", "))
		}
	}
}

// TestSpeechToSpeechResolvesTheReplyBeforeTheToolEvent is an ordering gate, and
// ordering is the whole defect: a live or realtime reply opens with no context
// frame in front of it, so every frame that follows carries no scope of its own
// and the open reply has to be looked up. The lookup sat below the tool event,
// so a tool call recorded an empty cause and the dev page filed it under
// unassigned activity, sitting beside the reply that had called it. Both lines
// were present and the module read fine; only their order was wrong, which is
// why nothing but their order can be asserted here.
func TestSpeechToSpeechResolvesTheReplyBeforeTheToolEvent(t *testing.T) {
	agent := agentFor(t, "live_model")
	resolved := targetByProvider(t, agent, ir.ProviderPipecat)
	artifact, err := Generate(agent, resolved, targetcap.Default())
	if err != nil {
		t.Fatal(err)
	}
	module := artifactFile(t, artifact, "dev_metrics.py")
	lookup := strings.Index(module, "self._live_turn in self._requests")
	tool := strings.Index(module, "self._tool_event(llm, frame, scope)")
	if lookup < 0 || tool < 0 {
		t.Fatalf("the live reply lookup (%d) or the tool event (%d) is not in the emitted module", lookup, tool)
	}
	if lookup > tool {
		t.Error("the open reply is resolved after the tool event, so every tool call on a speech to " +
			"speech package records no reply to hang off and renders as unassigned activity")
	}
}
