package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// A task's `announce:` is spoken as the step is entered, which is what the key
// has always said it does and what it did not do: as a task group step with the
// default opening the line reached nobody, and the salon package carried its
// verification line on a tool instead as the workaround.
//
// The half that matters as much is where it is *not* emitted. A task the owner
// calls directly says its line at the delegate seam, before the class is even
// built, so the step must not say it again. Saying it twice is the failure this
// holds shut, and it is one `and` in each template.
//
// Held against internal/testdata/remy, which runs two groups and nothing else,
// so a line emitted per delegate rather than per step shows up as a count.
//
// On Pipecat the line is a queued TTSSpeakFrame and not a `tts_say` pre-action,
// which it was until 2026-09-21. A pre-action holds the node until its
// ActionFinishedFrame reaches the worker sink, and a frame queued from inside
// the tool call that builds the flow does not move until that call returns, so
// the first node of every flow deadlocked: the group said nothing, asked the
// model nothing, and held the caller (trace 5a330c65).
func TestAGroupStepSpeaksItsOwnAnnounceOnce(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		file     string
		want     string
	}{
		{ir.ProviderLiveKit, "agent.py", "if self._speak_opening:"},
		{ir.ProviderPipecat, "bot.py", `await self.queue_frame(TTSSpeakFrame(_open, append_to_context=False))`},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			agent := loadExample(t, "remy")
			artifact, err := Generate(agent, targetByProvider(t, agent, tc.provider), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			py := artifactFile(t, artifact, tc.file)
			if got := strings.Count(py, tc.want); got != 1 {
				t.Fatalf("the step opening is emitted %d times, want exactly 1", got)
			}
			// The step's line reaches the model as well as the caller. On
			// Pipecat a TTSSpeakFrame never enters the context, and a live call
			// answered the agent's own announcement with "Got it.".
			if tc.provider == ir.ProviderPipecat && !strings.Contains(py, `{"role": "assistant", "content": _open}`) {
				t.Error("the spoken line is not seeded into the step's own context")
			}
		})
	}
}

// A task nothing runs as a step grows none of the machinery. prefetch_core's
// verify_caller is called by its owner and says its line at the seam, so the
// class takes no keyword and its bytes do not move. This is what
// TestPackagesWritingNoNewKeyEmitTheSameBytes would otherwise catch a week
// later, with the whole feature already built on top of it.
func TestATaskNoGroupRunsGrowsNoStepOpening(t *testing.T) {
	agent := announcingAgent(t)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if py := artifactFile(t, artifact, "agent.py"); strings.Contains(py, "speak_opening") {
		t.Error("a task no group runs took the step-opening keyword")
	}
}

// Alternatives lower to the _announce helper, one line lowers to the literal it
// always was. The second half is the whole compatibility story: `announce` is
// not in the new-key exemption in compat_test.go, so a scalar that stopped
// emitting its old bytes fails that gate rather than this one.
func TestAnnounceAlternativesLowerToTheHelperAndOneLineDoesNot(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		file     string
	}{
		{ir.ProviderLiveKit, "agent.py"},
		{ir.ProviderPipecat, "bot.py"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			agent := salonAgent(t)
			artifact, err := Generate(agent, targetByProvider(t, agent, tc.provider), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			py := artifactFile(t, artifact, tc.file)
			for _, want := range []string{
				"import random",
				"def _announce(key: str, lines: list[str]) -> str:",
				// The key is per call site, so one site's history never
				// silences another's, and the salon has both kinds: the diary
				// line belongs to a step of the `book` flow, and verification
				// is also a delegate customer care calls on its own. Neither
				// sits on a tool since 2026-09-16, because a tool's line is
				// emitted inside the tool body and fires once per call, so a
				// model that read the diary twice in one turn spoke twice
				// (trace 917975e9).
				`"delegate:verify_customer",`,
				`"task:manage_booking",`,
				`"Let me have a look at the diary.",`,
			} {
				if !strings.Contains(py, want) {
					t.Errorf("the alternatives package is missing %q", want)
				}
			}

			// One line, same package, same target: the record_complaint tool
			// writes a scalar and must still emit a bare literal.
			if !strings.Contains(py, `"Let me get that written down."`) {
				t.Error("a scalar announce stopped emitting its literal")
			}
			if strings.Contains(py, `_announce("tool:record_complaint"`) {
				t.Error("a scalar announce went through the chooser")
			}
		})
	}
}

// A package writing only scalars emits neither the helper nor its import, so a
// `random` nothing calls is never an F401 from the emitted project's own ruff
// gate and no existing golden moves.
func TestAPackageWithNoAlternativesEmitsNoChooser(t *testing.T) {
	agent := announcingAgent(t)
	for _, tc := range []struct {
		provider ir.Provider
		file     string
	}{
		{ir.ProviderLiveKit, "agent.py"},
		{ir.ProviderPipecat, "bot.py"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			artifact, err := Generate(agent, targetByProvider(t, agent, tc.provider), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			py := artifactFile(t, artifact, tc.file)
			for _, unwanted := range []string{"import random", "def _announce("} {
				if strings.Contains(py, unwanted) {
					t.Errorf("a scalar-only package emitted %q", unwanted)
				}
			}
		})
	}
}
