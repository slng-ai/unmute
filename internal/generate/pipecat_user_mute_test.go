package generate

import (
	"regexp"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// muteArgs matches the emitted aggregator argument and returns the class names
// inside it.
var muteArgs = regexp.MustCompile(`user_mute_strategies=\[([^\]]*)\]`)

// A phone leg has no echo cancellation, so an unprotected greeting is heard by
// the agent's own transcriber, reported as caller speech, and used to interrupt
// the agent one second into the call. The garbled turn then stays in the model's
// context for the whole conversation.
//
// That shipped, and it was invisible to every existing test: the pipecat golden
// is a web route, so the telephony path had no coverage of this argument at all.
// These rows are the telephony path.
func TestPipecatProtectsTheGreetingOnAPhoneRoute(t *testing.T) {
	on, off := true, false
	for _, tc := range []struct {
		name   string
		pkg    string
		mutate func(*ir.Agent)
		want   []string
	}{
		{
			// The default, and the reason this test exists.
			name: "telephony route protects the greeting by default",
			pkg:  "daily_carrier",
			want: []string{muteUntilFirstBotComplete},
		},
		{
			// A browser gets echo cancellation from the platform, so a web route
			// needs no protection and full barge-in stays the default.
			name: "web route protects nothing",
			pkg:  "safe_core",
			want: nil,
		},
		{
			name: "an explicit protect list wins over the route default",
			pkg:  "daily_carrier",
			mutate: func(a *ir.Agent) {
				setInterruption(a, &ir.Interruption{Enabled: &on, Protect: []ir.InterruptionProtect{ir.ProtectToolCalls}})
			},
			want: []string{muteFunctionCall},
		},
		{
			name: "both stretches lower to both classes",
			pkg:  "daily_carrier",
			mutate: func(a *ir.Agent) {
				setInterruption(a, &ir.Interruption{Enabled: &on, Protect: []ir.InterruptionProtect{ir.ProtectGreeting, ir.ProtectToolCalls}})
			},
			want: []string{muteUntilFirstBotComplete, muteFunctionCall},
		},
		{
			// An empty list is the author saying "none", which is the only way to
			// turn the route default off. It has to survive as distinct from an
			// absent key all the way from YAML to here.
			name: "an empty protect list suppresses the route default",
			pkg:  "daily_carrier",
			mutate: func(a *ir.Agent) {
				setInterruption(a, &ir.Interruption{Enabled: &on, Protect: []ir.InterruptionProtect{}})
			},
			want: nil,
		},
		{
			name: "enabled false still mutes the whole call",
			pkg:  "daily_carrier",
			mutate: func(a *ir.Agent) {
				setInterruption(a, &ir.Interruption{Enabled: &off})
			},
			want: []string{muteAlways},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := agentFor(t, tc.pkg)
			if tc.mutate != nil {
				tc.mutate(agent)
			}
			artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			bot := artifactFile(t, artifact, "bot.py")

			match := muteArgs.FindStringSubmatch(bot)
			if len(tc.want) == 0 {
				if match != nil {
					t.Fatalf("bot.py mutes the caller when it should not: %q", match[0])
				}
				return
			}
			if match == nil {
				t.Fatalf("bot.py emits no user_mute_strategies, so the caller can talk over %v", tc.want)
			}
			var want []string
			for _, class := range tc.want {
				want = append(want, class+"()")
			}
			if got := strings.Join(want, ", "); match[1] != got {
				t.Errorf("user_mute_strategies = [%s], want [%s]", match[1], got)
			}
			// The name in the argument and the name in the import have to agree.
			// A class used but not imported is a container that raises before it
			// ever answers a call, which is exactly how dev_metrics shipped.
			for _, class := range tc.want {
				if !strings.Contains(bot, "from pipecat.turns.user_mute import ") || !importsClass(bot, class) {
					t.Errorf("bot.py names %s but does not import it", class)
				}
			}
		})
	}
}

// importsClass reports whether the emitted user_mute import line names class.
func importsClass(bot, class string) bool {
	for _, line := range strings.Split(bot, "\n") {
		if strings.HasPrefix(line, "from pipecat.turns.user_mute import ") && strings.Contains(line, class) {
			return true
		}
	}
	return false
}

// setInterruption replaces the interruption block, creating the conversation if
// the fixture has none.
func setInterruption(a *ir.Agent, interruption *ir.Interruption) {
	if a.Conversation == nil {
		a.Conversation = &ir.Conversation{}
	}
	a.Conversation.Interruption = interruption
}
