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
	// A pace that differs from the package's own is a DIFFERENT refusal (pace
	// takes no per-target override), and it would fire before the ones these
	// cases are about. So the base is kept in step with whatever the case sets,
	// which is also the only shape an author can actually write.
	if def, ok := agent.Models[agent.Turn]; ok && def.Pace != turn.Pace {
		def.Pace = turn.Pace
		agent.Models[agent.Turn] = def
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

// TestValidateListenDeciderAcceptsTheTwoVendors10AddedIsWhatUS3 buys: the same
// `turn: provider: listen` an author already writes now takes two more
// transcribers. Neither names a model family, because neither has a second class
// with its own model list to get wrong.
func TestValidateListenDeciderAcceptsTheTwoNewVendors(t *testing.T) {
	for _, tc := range []struct{ vendor, model string }{
		{"gradium", "default"},
		{"speechmatics", "linden-1"},
	} {
		agent, tgt := listenDeciderTarget(t, ProviderPipecat, tc.vendor, tc.model, func(turn *Binding) {
			// Neither takes an early answer or a ceiling, so the accepted shape
			// writes neither. The two cases below prove each is refused.
			turn.Eager = false
			turn.Pace = ""
		})
		row := validateOne(t, agent, tgt)
		if len(row.Errors) != 0 {
			t.Errorf("%s %s: unexpected errors: %s", tc.vendor, tc.model, strings.Join(row.Errors, "\n"))
		}
	}
}

// TestValidateRefusesEagerOnAVendorThatPredictsNothing: the refusal was written
// in spec 021 against no real vendor, because both rows in the table then said
// they could predict. 1.10.0 supplies two that cannot: both report a turn that
// has already ended.
//
// Without this, the emitted constructor passes enable_eager_end_of_turn to a
// service that has no such keyword, and the author finds out from a traceback on
// the first call.
func TestValidateRefusesEagerOnAVendorThatPredictsNothing(t *testing.T) {
	for _, tc := range []struct{ vendor, model string }{
		{"gradium", "default"},
		{"speechmatics", "linden-1"},
	} {
		agent, tgt := listenDeciderTarget(t, ProviderPipecat, tc.vendor, tc.model, func(turn *Binding) {
			turn.Eager = true
			turn.Pace = ""
		})
		row := validateOne(t, agent, tgt)
		found := ""
		for _, err := range row.Errors {
			if strings.Contains(err, "eager") {
				found = err
			}
		}
		if found == "" {
			t.Fatalf("%s: eager: true was accepted on a vendor that predicts nothing; errors: %s", tc.vendor, strings.Join(row.Errors, "\n"))
		}
		if !strings.Contains(found, tc.vendor) {
			t.Errorf("%s: the refusal does not name the vendor: %s", tc.vendor, found)
		}
		// It names what to do instead, and the alternatives are read from the
		// table rather than written out beside it, so a vendor that gains
		// prediction later reaches this sentence with nobody remembering.
		for _, want := range []string{"Remove eager", "deepgram", "cartesia"} {
			if !strings.Contains(found, want) {
				t.Errorf("%s: the refusal does not name %q: %s", tc.vendor, want, found)
			}
		}
		// And it does not offer a vendor that cannot predict either.
		if strings.Contains(found, "gradium") && strings.Contains(found, "such as default") {
			t.Errorf("%s: the refusal offers a vendor that predicts nothing as the fix: %s", tc.vendor, found)
		}
	}
}

// TestValidateRefusesPaceWithoutACeiling: under a listening decider the pace
// ceiling IS that service's own end-of-turn timeout. Flux and Turns each expose
// one field for it; gradium and speechmatics expose none.
//
// Refused rather than mapped onto the nearest-looking setting. Gradium's
// eot_horizon_s is which prediction horizon to READ, in seconds, not how long to
// wait, so writing the ceiling there would make one authored word mean two
// different things per vendor and the agent would wait a length nobody asked
// for.
func TestValidateRefusesPaceWithoutACeiling(t *testing.T) {
	for _, tc := range []struct{ vendor, model string }{
		{"gradium", "default"},
		{"speechmatics", "linden-1"},
	} {
		agent, tgt := listenDeciderTarget(t, ProviderPipecat, tc.vendor, tc.model, func(turn *Binding) {
			turn.Eager = false
			turn.Pace = PaceSnappy
		})
		row := validateOne(t, agent, tgt)
		found := ""
		for _, err := range row.Errors {
			if strings.Contains(err, "pace") {
				found = err
			}
		}
		if found == "" {
			t.Fatalf("%s: pace was accepted on a vendor with no end-of-turn timeout; errors: %s", tc.vendor, strings.Join(row.Errors, "\n"))
		}
		for _, want := range []string{"pace: snappy", tc.vendor, "no end-of-turn timeout", "Remove pace"} {
			if !strings.Contains(found, want) {
				t.Errorf("%s: the refusal does not name %q: %s", tc.vendor, want, found)
			}
		}
		// The alternatives come from the table, the same way the eager ones do.
		for _, want := range []string{"deepgram", "cartesia"} {
			if !strings.Contains(found, want) {
				t.Errorf("%s: the refusal does not offer %q, which does take a ceiling: %s", tc.vendor, want, found)
			}
		}
	}
}

// TestValidatePaceIsStillTakenWhereItLands: the inverse, and the reason the
// refusal is keyed on the row rather than on a list of vendor names. A vendor
// whose service takes a ceiling still takes a pace, and adding two vendors that
// do not must not have made `pace` mean nothing everywhere.
func TestValidatePaceIsStillTakenWhereItLands(t *testing.T) {
	for _, tc := range []struct{ vendor, model string }{
		{"deepgram", "flux-general-en"},
		{"cartesia", "ink-2"},
	} {
		agent, tgt := listenDeciderTarget(t, ProviderPipecat, tc.vendor, tc.model, func(turn *Binding) {
			turn.Pace = PaceSnappy
		})
		row := validateOne(t, agent, tgt)
		for _, err := range row.Errors {
			if strings.Contains(err, "pace") {
				t.Errorf("%s: pace was refused on a vendor whose service takes one: %s", tc.vendor, err)
			}
		}
	}
}
