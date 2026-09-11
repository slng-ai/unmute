package ir

import (
	"strings"
	"testing"

	targetcap "github.com/slng-ai/unmute/internal/target"
)

// listenDeciderTarget returns the safe_core package resolved for one provider
// with its turn binding handing the decision to the transcriber and its listen
// binding set as the case needs. The base package binds slng listening and a
// local turn, so every case below states the whole shape it tests.
func listenDeciderTarget(t *testing.T, provider Provider, listenVendor, listenModel string, mutate func(turn *Binding)) (*Agent, Target) {
	t.Helper()
	agent := safeAgent(t)
	tgt := targetFor(agent, provider)
	if tgt.Models.Turn == nil || tgt.Models.Listen == nil {
		t.Fatal("fixture has no turn or listen binding to configure")
	}
	turn := *tgt.Models.Turn
	turn.Provider, turn.Model = targetcap.TurnDeciderListen, ""
	// The fixture writes the two fields a listening decider refuses, so each case
	// starts without them and adds back the one it is about.
	turn.SemanticEndpointing = ""
	if agent.Conversation != nil && agent.Conversation.Interruption != nil {
		interruption := *agent.Conversation.Interruption
		interruption.MinimumWords = 0
		agent.Conversation.Interruption = &interruption
	}
	if mutate != nil {
		mutate(&turn)
	}
	tgt.Models.Turn = &turn
	listen := *tgt.Models.Listen
	listen.Provider, listen.Model, listen.Params = listenVendor, listenModel, nil
	tgt.Models.Listen = &listen
	return agent, tgt
}

func validateOne(t *testing.T, agent *Agent, tgt Target) TargetValidation {
	t.Helper()
	// Validate reports a refusal as an error as well as in the row; the row is
	// what these tests read, so the error is not a harness failure here.
	report, _ := Validate(agent, []Target{tgt}, targetcap.Default())
	if len(report.PerTarget) != 1 {
		t.Fatalf("want one target row, got %d", len(report.PerTarget))
	}
	return report.PerTarget[0]
}

// TestValidateListenDeciderAcceptsAFluxOrTurnsListener is the accepted shape:
// the transcriber decides, no local model is named, and the pace still applies.
func TestValidateListenDeciderAcceptsAFluxOrTurnsListener(t *testing.T) {
	for _, tc := range []struct{ vendor, model string }{
		{"deepgram", "flux-general-en"},
		{"deepgram", "flux-general-multi"},
		{"cartesia", "ink-2"},
	} {
		agent, tgt := listenDeciderTarget(t, ProviderPipecat, tc.vendor, tc.model, func(turn *Binding) {
			turn.Eager = true
		})
		row := validateOne(t, agent, tgt)
		if len(row.Errors) != 0 {
			t.Errorf("%s %s: unexpected errors: %s", tc.vendor, tc.model, strings.Join(row.Errors, "\n"))
		}
	}
}

// TestValidateListenDeciderRefusesEveryShapeItCannotHonour holds every row of
// the refusal table in specs/021 contracts/authoring.md: each message names
// what to do instead.
func TestValidateListenDeciderRefusesEveryShapeItCannotHonour(t *testing.T) {
	for _, tc := range []struct {
		name          string
		provider      Provider
		vendor, model string
		mutate        func(turn *Binding)
		minWords      int
		want          []string
	}{
		{
			name: "a nova model on the deepgram flux service", provider: ProviderPipecat,
			vendor: "deepgram", model: "nova-3",
			want: []string{`deepgram model "nova-3" does not`, "flux-general-en", "set the turn provider back to local"},
		},
		{
			name: "the ordinary cartesia transcriber", provider: ProviderPipecat,
			vendor: "cartesia", model: "ink-whisper",
			want: []string{`cartesia model "ink-whisper" does not`, "ink-2"},
		},
		{
			name: "a vendor that detects turns but cannot predict one", provider: ProviderPipecat,
			vendor: "assemblyai", model: "universal-3-5-pro",
			want: []string{"cartesia (ink- models such as ink-2) and deepgram (flux- models such as flux-general-en)", `listening model is provider "assemblyai"`, "Set the turn provider to local"},
		},
		{
			name: "the slng bridge", provider: ProviderPipecat,
			vendor: "slng", model: "deepgram/nova:3",
			want: []string{`listening model is provider "slng"`},
		},
		{
			name: "endpointing_delay under a listening decider", provider: ProviderPipecat,
			vendor: "deepgram", model: "flux-general-en",
			mutate: func(turn *Binding) { turn.EndpointingDelay = "300ms" },
			want:   []string{"endpointing_delay 300ms reaches nothing when the transcriber decides the turn", "Remove it"},
		},
		{
			name: "semantic_endpointing under a listening decider", provider: ProviderPipecat,
			vendor: "deepgram", model: "flux-general-en",
			mutate: func(turn *Binding) { turn.SemanticEndpointing = SemanticEndpointingOff },
			want:   []string{"semantic_endpointing off reaches nothing when the transcriber decides the turn"},
		},
		{
			name: "minimum_words under a listening decider", provider: ProviderPipecat,
			vendor: "deepgram", model: "flux-general-en", minWords: 2,
			want: []string{"conversation.interruption.minimum_words reaches nothing when the transcriber decides the turn"},
		},
		{
			name: "livekit has no listening decider", provider: ProviderLiveKit,
			vendor: "deepgram", model: "flux-general-en",
			mutate: func(turn *Binding) { turn.Eager = true },
			want:   []string{`turn provider "listen" is not available on livekit`, "turn-detector-mini", "eager is not available on livekit"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent, tgt := listenDeciderTarget(t, tc.provider, tc.vendor, tc.model, tc.mutate)
			if tc.minWords > 0 {
				if agent.Conversation == nil {
					agent.Conversation = &Conversation{}
				}
				enabled := true
				agent.Conversation.Interruption = &Interruption{Enabled: &enabled, MinimumWords: tc.minWords}
			}
			row := validateOne(t, agent, tgt)
			text := strings.Join(row.Errors, "\n")
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("want %q in:\n%s", want, text)
				}
			}
		})
	}
}

// TestValidateEagerNeedsTheListeningDecider: `eager` is a flag the local
// detector cannot act on, so it is refused there, on every other model kind, and
// on an effective per-target binding that lost the listening provider.
func TestValidateEagerNeedsTheListeningDecider(t *testing.T) {
	on := true
	for _, tc := range []struct {
		name  string
		model ModelDef
		want  string
	}{
		{name: "listen decider takes it", model: ModelDef{Kind: KindTurn, Provider: targetcap.TurnDeciderListen, Eager: &on}},
		{name: "false is legal anywhere on a turn model", model: ModelDef{Kind: KindTurn, Provider: "local", Eager: new(bool)}},
		{name: "local detector refuses it", model: ModelDef{Kind: KindTurn, Provider: "local", Eager: &on}, want: "eager needs turn provider listen: the local detector cannot predict a turn before it ends"},
		{name: "think model refuses it", model: ModelDef{Kind: KindThink, Eager: &on}, want: "eager is a turn-model field"},
		{name: "listen model refuses it", model: ModelDef{Kind: KindListen, Eager: &on}, want: "eager is a turn-model field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := strings.Join(eagerErrors("detector", tc.model), "\n")
			if tc.want == "" && text != "" {
				t.Fatalf("unexpected errors: %s", text)
			}
			if tc.want != "" && !strings.Contains(text, tc.want) {
				t.Fatalf("want %q, got %q", tc.want, text)
			}
		})
	}

	agent := safeAgent(t)
	tgt := targetFor(agent, ProviderPipecat)
	turn := *tgt.Models.Turn
	turn.Eager = true
	tgt.Models.Turn = &turn
	row := validateOne(t, agent, tgt)
	if text := strings.Join(row.Errors, "\n"); !strings.Contains(text, "turn binding sets eager without provider listen") {
		t.Errorf("an effective local binding with eager must be refused, got:\n%s", text)
	}
}

// TestValidateLocalDeciderIsUntouched: the shape every shipped package writes
// keeps validating exactly as before this feature.
func TestValidateLocalDeciderIsUntouched(t *testing.T) {
	agent := safeAgent(t)
	for _, provider := range []Provider{ProviderPipecat, ProviderLiveKit} {
		row := validateOne(t, agent, targetFor(agent, provider))
		if len(row.Errors) != 0 {
			t.Errorf("%s: %s", provider, strings.Join(row.Errors, "\n"))
		}
	}
}

// TestValidateRefusesEagerOnAnotherRolesOverride: a per-target `models:` block
// replaces a binding whole, so a turn field written inside an override of a
// listening or thinking entry never passes through the authored-model check
// that refuses it everywhere else. It would resolve onto a binding nothing
// reads and compile without a word.
func TestValidateRefusesEagerOnAnotherRolesOverride(t *testing.T) {
	for _, tc := range []struct {
		role  string
		apply func(*Target)
		want  string
	}{
		{"listen", func(tgt *Target) { tgt.Models.Listen.Eager = true },
			"the listen binding sets eager, which is a turn-model field"},
		{"think", func(tgt *Target) {
			for name, binding := range tgt.Models.Reason {
				binding.Eager = true
				tgt.Models.Reason[name] = binding
				break
			}
		}, "sets eager, which is a turn-model field"},
	} {
		t.Run(tc.role, func(t *testing.T) {
			agent, tgt := listenDeciderTarget(t, ProviderPipecat, "deepgram", "flux-general-en", nil)
			tc.apply(&tgt)
			row := validateOne(t, agent, tgt)
			if text := strings.Join(row.Errors, "\n"); !strings.Contains(text, tc.want) {
				t.Errorf("want %q in:\n%s", tc.want, text)
			}
		})
	}
}
