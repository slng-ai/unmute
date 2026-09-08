package generate

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The Responses-only params are authored once, on the base think binding, and
// each target takes what it can act on.
//
// They used to need a livekit target override, because a per-target models:
// entry replaces the base entry rather than merging into it: the override had
// to repeat provider, model and reasoning_effort verbatim to keep them. That is
// a binding somebody edits on one side only, so the params moved to the base
// and the compiler took responsibility for the target that cannot use them.
//
// What makes this worth a gate is the failure it replaces. A params: block is
// forwarded verbatim by design, and Pipecat's OpenAI entry forwards through the
// settings overflow, which is the request body. So `api` and `use_websocket` on
// a shared binding put two keys OpenAI's chat completions endpoint does not
// define into every request of every turn. Dropping them one layer lower caught
// the constructor path and not the overflow one, and the emitted Python looked
// right on LiveKit while Pipecat shipped the junk.
func TestResponsesOnlyParamsReachLiveKitAndNoRequestBody(t *testing.T) {
	load := func(t *testing.T) *ir.Agent {
		t.Helper()
		pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
		if err != nil {
			t.Fatal(err)
		}
		agent, err := ir.Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		return agent
	}

	// Authored on the base binding, so every target resolves all three.
	agent := load(t)
	for _, name := range []string{"api", "use_websocket"} {
		if _, ok := agent.Models["reasoning"].Params[name]; !ok {
			t.Fatalf("the salon package no longer authors params.%s on its base think binding, so this test proves nothing", name)
		}
	}

	t.Run("livekit builds the Responses class", func(t *testing.T) {
		agent := load(t)
		tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
		artifact, err := Generate(agent, tgt, target.Default())
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		agentPy := artifactFile(t, artifact, "agent.py")
		for _, want := range []string{
			"openai.responses.LLM(",
			`reasoning=openai_types.Reasoning(effort="none")`,
			"use_websocket=True",
		} {
			if !strings.Contains(agentPy, want) {
				t.Errorf("agent.py is missing %q: the base binding's params did not reach the target that can act on them", want)
			}
		}
	})

	t.Run("pipecat sends neither in the request body", func(t *testing.T) {
		agent := load(t)
		tgt := targetByProvider(t, agent, ir.ProviderPipecat)
		artifact, err := Generate(agent, tgt, target.Default())
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		botPy := artifactFile(t, artifact, "bot.py")
		// Named per param rather than as one search for "extra={", because the
		// module carries several and the point is which keys are in them.
		for _, name := range ir.ResponsesOnlyParams {
			if strings.Contains(botPy, `"`+name+`"`) {
				t.Errorf("bot.py still carries %q: pipecat builds the chat completions service, so this rides the request body as a key OpenAI does not define", name)
			}
		}
		// The one that does belong there is still there, so the drop is
		// targeted rather than a params block that stopped being forwarded.
		if !strings.Contains(botPy, `extra={"reasoning_effort": "none"}`) {
			t.Error(`bot.py lost extra={"reasoning_effort": "none"}: the drop took a param the request needs`)
		}
	})
}

// A warning, never a refusal, and never silence.
//
// Silence is what made this a bug worth gating: the author wrote two params, the
// package validated clean, and one target sent them to an endpoint that has no
// such fields. Refusing is not the answer either, because one think binding
// serving both targets is the shape this change exists to allow.
func TestResponsesOnlyParamsWarnOnTheTargetThatCannotUseThem(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	var declared []ir.Target
	for _, name := range slices.Sorted(maps.Keys(agent.Targets)) {
		declared = append(declared, agent.Targets[name])
	}
	report, err := ir.Validate(agent, declared, target.Default())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	for _, row := range report.PerTarget {
		warnings := strings.Join(row.Warnings, "\n")
		if len(row.Errors) > 0 {
			t.Errorf("target %q reports errors, and these params must never refuse a package: %v", row.Name, row.Errors)
		}
		switch row.Provider {
		case ir.ProviderLiveKit:
			for _, name := range ir.ResponsesOnlyParams {
				if strings.Contains(warnings, "params."+name) {
					t.Errorf("target %q warns about params.%s, which it acts on", row.Name, name)
				}
			}
		case ir.ProviderPipecat:
			for _, name := range ir.ResponsesOnlyParams {
				if !strings.Contains(warnings, "params."+name) {
					t.Errorf("target %q does not warn that params.%s reaches nothing here: %v", row.Name, name, row.Warnings)
				}
			}
		}
	}
}
