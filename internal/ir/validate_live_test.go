package ir

import (
	"path/filepath"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// liveAgent builds the live_model fixture: one agent on a live model
// backed by an OpenAI think entry, two tools, a greeting and an idle nudge.
func liveAgent(t *testing.T) *Agent {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "live_model"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// TestValidateLiveAcceptsOneAgentOnALiveModel is the accepted shape, and
// what it resolves to: a live binding carrying its backend, no open listen
// or turn role demanded, and the OpenAI key on the startup check.
func TestValidateLiveAcceptsOneAgentOnALiveModel(t *testing.T) {
	agent := liveAgent(t)
	if agent.Agents["desk"].Live != "voice" || agent.Agents["desk"].Model != "" || agent.Agents["desk"].Voice != "" {
		t.Fatalf("agent resolved to %+v", agent.Agents["desk"])
	}
	if agent.Models["voice"].Kind != KindLive || agent.Models["voice"].Backend != "fast" {
		t.Fatalf("live model resolved to %+v", agent.Models["voice"])
	}
	tgt := targetFor(agent, ProviderPipecat)
	binding, ok := tgt.Models.Live["voice"]
	if !ok || binding.Backend != "fast" || binding.Voice != "marin" || binding.Model != "gpt-live-1" {
		t.Fatalf("live binding resolved to %+v", tgt.Models.Live)
	}
	if _, ok := tgt.Models.Reason["fast"]; !ok {
		t.Fatal("the think entry the live model names is not a used reason binding")
	}
	if tgt.Models.Listen != nil || tgt.Models.Turn != nil || len(tgt.Models.Speak) != 0 {
		t.Fatalf("a live package resolved listen, turn or speak bindings: %+v", tgt.Models)
	}
	row := validateOne(t, agent, tgt)
	if len(row.Errors) != 0 {
		t.Fatalf("unexpected errors:\n%s", strings.Join(row.Errors, "\n"))
	}
}

// liveRefusal is one row of the live refusal table in
// specs/021 and specs/023 contracts/authoring.md.
//
// One table, read by two tests. The parity test below used to carry its own
// copy of a subset of these rows, which meant the contract's claim that no row
// differs between the two code targets was checked for half the rows it makes
// the claim about, and adding a row here bought no parity coverage at all. A
// second list is the thing that drifts.
type liveRefusal struct {
	name   string
	mutate func(agent *Agent, tgt *Target)
	want   []string
	// namesTheTarget marks a refusal whose text legitimately opens with the
	// target's own name. The parity check compares those with the name masked,
	// because "livekit ..." against "pipecat ..." is the two targets saying the
	// same thing, not saying different things.
	namesTheTarget bool
}

func liveRefusals() []liveRefusal {
	return []liveRefusal{
		{
			name: "a second agent",
			mutate: func(agent *Agent, _ *Target) {
				agent.Agents["billing"] = AgentDef{Instructions: "Billing.", Live: "voice"}
			},
			want:           []string{`architecture: live serves one agent: "voice" is bound by "billing" and the package also declares agent desk`, "architecture: realtime is one model"},
			namesTheTarget: true,
		},
		{
			name: "a task",
			mutate: func(agent *Agent, _ *Target) {
				agent.Tasks = map[string]Task{"verify": {Instructions: "Verify."}}
			},
			want:           []string{"architecture: live has no place for tasks", "a live session fixes them when it starts"},
			namesTheTarget: true,
		},
		{
			name: "a listen section",
			mutate: func(agent *Agent, tgt *Target) {
				agent.Listen = "transcriber"
				agent.Models["transcriber"] = ModelDef{Kind: KindListen, Provider: "deepgram", Model: "nova-3"}
				tgt.Models.Listen = &Binding{Provider: "deepgram", Model: "nova-3"}
			},
			want: []string{`models.listen is not used under architecture: live: agent "desk" binds "voice", which listens itself`},
		},
		{
			name: "a turn section",
			mutate: func(agent *Agent, tgt *Target) {
				agent.Turn = "detector"
				agent.Models["detector"] = ModelDef{Kind: KindTurn, Provider: "local", Model: "silero"}
				tgt.Models.Turn = &Binding{Provider: "local", Model: "silero"}
			},
			want: []string{`models.turn is not used under architecture: live: agent "desk" binds "voice", which decides the turn itself`},
		},
		{
			name: "a speak section",
			mutate: func(agent *Agent, _ *Target) {
				agent.Models["voice"] = ModelDef{Kind: KindSpeak, Provider: "cartesia", Voice: "x"}
			},
			want: []string{"models.speak is not used under architecture: live: agent \"desk\" binds \"voice\", which speaks itself"},
		},
		{
			name: "interruption settings",
			mutate: func(agent *Agent, _ *Target) {
				on := true
				agent.Conversation.Interruption = &Interruption{Enabled: &on}
			},
			want: []string{"conversation.interruption reaches nothing under architecture: live: the model handles being talked over itself"},
		},
		{
			name: "call state",
			mutate: func(agent *Agent, _ *Target) {
				agent.Variables = map[string]Variable{"caller": {}}
			},
			want: []string{"variables and prefetch are not emitted under architecture: live in this version"},
		},
		{
			name: "tracing",
			mutate: func(agent *Agent, _ *Target) {
				agent.Tracing = &Tracing{Provider: "langfuse"}
			},
			want: []string{"tracing is not emitted under architecture: live in this version"},
		},
		{
			name: "a telephony connection",
			mutate: func(_ *Agent, tgt *Target) {
				tgt.Connection = "twilio_voice"
			},
			want: []string{`architecture: live compiles for the browser route in this version: connection "twilio_voice"`},
		},
		{
			name: "a backend that is not at OpenAI",
			mutate: func(agent *Agent, tgt *Target) {
				backend := tgt.Models.Reason["fast"]
				backend.Provider = "anthropic"
				tgt.Models.Reason["fast"] = backend
			},
			want: []string{`live model "voice" backend "fast" is provider "anthropic": the handover happens inside the live session itself`},
		},
		{
			name: "tools with no backend",
			mutate: func(agent *Agent, tgt *Target) {
				live := agent.Models["voice"]
				live.Backend = ""
				agent.Models["voice"] = live
				binding := tgt.Models.Live["voice"]
				binding.Backend = ""
				tgt.Models.Live["voice"] = binding
			},
			want: []string{`live model "voice" is bound by agent "desk", which has tools, and names no backend`},
		},
		{
			// Nothing else catches this: the backend is skipped by both binding
			// loops, so an entry with no model id validated clean and compiled
			// to Settings(model=""). The console produced exactly this shape by
			// rewriting the backend from its name alone.
			name: "a backend with no model",
			mutate: func(_ *Agent, tgt *Target) {
				backend := tgt.Models.Reason["fast"]
				backend.Model = ""
				tgt.Models.Reason["fast"] = backend
			},
			want: []string{`live model "voice" backend "fast" names no model`, "write model: <the backend model's id>"},
		},
		{
			// The rows below had no case at all. Each is named in contracts §2,
			// and until the parity test read this table there was nothing to
			// notice their absence: the table was the contract's only reader.
			name: "a task group",
			mutate: func(agent *Agent, _ *Target) {
				agent.TaskGroups = map[string]TaskGroup{"intake": {}}
			},
			want:           []string{"architecture: live has no place for tasks", "Remove the tasks and task groups"},
			namesTheTarget: true,
		},
		{
			name: "a handoff to another agent",
			mutate: func(agent *Agent, _ *Target) {
				agent.Controls = map[string]Control{"to_billing": &AgentTransfer{}}
			},
			want:           []string{`control "to_billing" hands the call to another agent or a task`, "architecture: realtime is one model"},
			namesTheTarget: true,
		},
		{
			name: "an escalation",
			mutate: func(agent *Agent, _ *Target) {
				agent.Controls = map[string]Control{"to_a_person": &HumanTransfer{}}
			},
			want: []string{`escalation "to_a_person" is not emitted under architecture: live`, "architecture: cascade"},
		},
		{
			name: "a prefetch entry",
			mutate: func(agent *Agent, _ *Target) {
				agent.Prefetch = []Prefetch{{}}
			},
			want: []string{"variables and prefetch are not emitted under architecture: live", "architecture: cascade"},
		},
		{
			name:           "a vendor with no live service",
			namesTheTarget: true,
			mutate: func(agent *Agent, tgt *Target) {
				binding := tgt.Models.Live["voice"]
				binding.Provider = "anthropic"
				tgt.Models.Live["voice"] = binding
			},
			want: []string{`live binding provider "anthropic" has no slot`},
		},
	}
}

// TestValidateLiveRefusesEveryShapeItCannotHonour holds every row of the
// live refusal table in specs/021 contracts/authoring.md.
func TestValidateLiveRefusesEveryShapeItCannotHonour(t *testing.T) {
	for _, tc := range liveRefusals() {
		t.Run(tc.name, func(t *testing.T) {
			agent := liveAgent(t)
			tgt := targetFor(agent, ProviderPipecat)
			tc.mutate(agent, &tgt)
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

// TestValidateLiveIsRefusedOnSlngAlone: both code targets emit this
// architecture, so slng is the only target that denies the binding by name. It
// says what to write instead, and nothing else about the package is reported,
// because the denial is the whole answer there.
//
// This used to assert LiveKit refused too. That assertion was correct until the
// livekit driver grew the pipeline, and the reason it is gone is that the
// driver emits one now, not that the rule was relaxed.
func TestValidateLiveIsRefusedOnSlngAlone(t *testing.T) {
	agent := liveAgent(t)
	for _, tc := range []struct {
		provider Provider
		want     string
	}{
		{ProviderSlng, "slng target binds listen, think and speak by name and has no slot for one model that does all three"},
	} {
		tgt := targetFor(agent, ProviderPipecat)
		tgt.Provider, tgt.Name = tc.provider, string(tc.provider)
		if tc.provider == ProviderLiveKit {
			tgt.Version = "1.8.1"
		} else {
			tgt.Version = ""
		}
		row := validateOne(t, agent, tgt)
		text := strings.Join(row.Errors, "\n")
		if !strings.Contains(text, tc.want) {
			t.Errorf("%s: want %q in:\n%s", tc.provider, tc.want, text)
		}
		if strings.Contains(text, "missing open listen binding") || strings.Contains(text, "is missing live binding") {
			t.Errorf("%s: the denial should stand alone, got:\n%s", tc.provider, text)
		}
	}
}

// TestBuildRefusesAnAgentNamingBothFormsOrNeither: the authored shape is one of
// think and speak, or live; Build says which is missing and names the fix.
func TestBuildRefusesAnAgentNamingBothFormsOrNeither(t *testing.T) {
	load := func(t *testing.T, mutate func(pkg *packagespec.Package)) error {
		t.Helper()
		pkg, err := packagespec.Load(filepath.Join("..", "testdata", "live_model"))
		if err != nil {
			t.Fatal(err)
		}
		mutate(pkg)
		_, err = Build(pkg)
		return err
	}
	if err := load(t, func(pkg *packagespec.Package) {
		def := pkg.Agent.Agents["desk"]
		def.Think = "fast"
		pkg.Agent.Agents["desk"] = def
	}); err == nil || !strings.Contains(err.Error(), `names live "voice" and also think or speak`) {
		t.Errorf("both forms: %v", err)
	}
	if err := load(t, func(pkg *packagespec.Package) {
		def := pkg.Agent.Agents["desk"]
		def.Live = ""
		pkg.Agent.Agents["desk"] = def
	}); err == nil || !strings.Contains(err.Error(), "names no live model") {
		t.Errorf("neither form: %v", err)
	}
	if err := load(t, func(pkg *packagespec.Package) {
		def := pkg.Agent.Agents["desk"]
		def.Live = "fast"
		pkg.Agent.Agents["desk"] = def
	}); err == nil || !strings.Contains(err.Error(), `live "fast" is a think model, not a live model`) {
		t.Errorf("wrong kind: %v", err)
	}
	if err := load(t, func(pkg *packagespec.Package) {
		pkg.Agent.Models.Live[0].Backend = "voice"
	}); err == nil || !strings.Contains(err.Error(), "is a live model, not a think model") {
		t.Errorf("backend of the wrong kind: %v", err)
	}
	if err := load(t, func(pkg *packagespec.Package) {
		pkg.Agent.Models.Live = append(pkg.Agent.Models.Live, pkg.Agent.Models.Live[0])
	}); err == nil || !strings.Contains(err.Error(), `models.live declares "voice" twice`) {
		t.Errorf("duplicate name: %v", err)
	}
	// A clash with another section is the other mistake, and says so.
	if err := load(t, func(pkg *packagespec.Package) {
		pkg.Agent.Models.Live[0].Name = "fast"
	}); err == nil || !strings.Contains(err.Error(), `model name "fast" appears in both think and live`) {
		t.Errorf("name taken by a think entry: %v", err)
	}
	if err := load(t, func(pkg *packagespec.Package) {
		tgt := pkg.Targets["pipecat"]
		tgt.Models = map[string]packagespec.ModelDef{"voice": {Provider: "openai", Model: "other"}}
		pkg.Targets["pipecat"] = tgt
	}); err == nil || !strings.Contains(err.Error(), `overrides live model "voice"; a live entry takes no per-target override`) {
		t.Errorf("per-target override: %v", err)
	}
}

// TestValidateLiveRefusesTheSameOnBothCodeTargets: every refusal a live package
// can receive reads identically on LiveKit and on Pipecat.
//
// This exists because the feature that taught LiveKit this architecture was
// planned around a difference that turned out not to be real. The plan had an
// agent with tools and no backend refused on LiveKit and accepted on Pipecat,
// reasoning from Pipecat's client delegation being able to run a package's
// tools. It can; this compiler never wires a backend worker for it, so with no
// backend the tools cannot run on either target, and both already refused the
// shape in the same words.
//
// So the rule this pins is: a package moves between the two code targets by
// changing one line, and it is never told a different story about what it
// TestValidateLiveRefusesTheSameOnBothCodeTargets holds the bold claim at the
// foot of specs/023 contracts/authoring.md §2: **no row differs between the two
// targets**. Every row of liveRefusals() is put to both, and the two answers
// have to be the same text.
//
// It reads the shared table rather than a list of its own, which is the point.
// The earlier version named seven rows by hand, so six of the rows the contract
// makes the claim about were asserted on Pipecat alone, and a new row joined the
// refusal table without ever being checked for parity.
//
// A refusal that opens with the target's own name is compared with the name
// masked. That is not a loophole: the sentence after it still has to match word
// for word, and FR-006 allows a refusal to name the target only where a
// difference between the runtimes is real, which a vendor list is.
func TestValidateLiveRefusesTheSameOnBothCodeTargets(t *testing.T) {
	for _, tc := range liveRefusals() {
		t.Run(tc.name, func(t *testing.T) {
			said := map[Provider]string{}
			for _, provider := range []Provider{ProviderLiveKit, ProviderPipecat} {
				agent := liveAgent(t)
				tgt := targetFor(agent, provider)
				tc.mutate(agent, &tgt)
				row := validateOne(t, agent, tgt)
				if len(row.Errors) == 0 {
					t.Fatalf("%s accepted a shape a live package cannot carry", provider)
				}
				text := strings.Join(row.Errors, "\n")
				if tc.namesTheTarget {
					text = strings.ReplaceAll(text, string(provider), "<target>")
				}
				said[provider] = text
			}
			if said[ProviderLiveKit] != said[ProviderPipecat] {
				t.Errorf("the two code targets tell different stories about the same package:\nlivekit:\n%s\npipecat:\n%s",
					said[ProviderLiveKit], said[ProviderPipecat])
			}
		})
	}
}

// TestNoRefusalSendsAnAuthorToAShapeThatCompilesNowhere is the rule the
// realtimeAdvice helper exists to keep.
//
// A live refusal ends by naming the other speech-to-speech architecture, which
// is only useful where that architecture compiles. While no driver emits it, an
// author who follows the advice spends a round to hit a second refusal. The
// sentence is therefore derived from the capability table rather than written
// out, and this holds it both ways: it must not promise realtime while the
// table denies it, and it must stop hedging the day the table allows it.
//
// Both directions matter. The first is today's bug. The second is what stops a
// hedge outliving its reason, which is the harder one to notice: nothing fails
// when a refusal is merely out of date.
func TestNoRefusalSendsAnAuthorToAShapeThatCompilesNowhere(t *testing.T) {
	for _, provider := range []Provider{ProviderLiveKit, ProviderPipecat} {
		t.Run(string(provider), func(t *testing.T) {
			agent := liveAgent(t)
			agent.Tasks = map[string]Task{"verify": {Instructions: "V.", Context: TaskContext{History: HistoryMessages}}}
			tgt := targetFor(agent, provider)
			row := validateOne(t, agent, tgt)
			text := strings.Join(row.Errors, "\n")
			if !strings.Contains(text, "architecture: realtime") {
				t.Fatalf("the tasks refusal stopped naming realtime at all:\n%s", text)
			}

			// The question is NOT whether realtime compiles here. It is whether
			// an author sent there could write the thing they are being refused
			// for. Those came apart the moment realtime started compiling while
			// still carrying no tasks, and this assertion asked the first
			// question until then, so the day the driver landed it demanded the
			// refusal start promising a shape that would refuse the author again.
			// Watching it fail that way is what found the split.
			takesTasks := targetTable().Capability(realtimeTasksField(), targetProvider(provider)).Tag == coreTag()
			promises := strings.Contains(text, "write architecture: realtime")
			switch {
			case takesTasks && !promises:
				t.Errorf("%s carries tasks on architecture: realtime, and the refusal still hedges rather than telling the author to write it:\n%s", provider, text)
			case !takesTasks && promises:
				t.Errorf("%s does not carry tasks on architecture: realtime, and the refusal tells the author to write it anyway; they would spend a round to hit a second refusal:\n%s", provider, text)
			}
			if !takesTasks && !strings.Contains(text, "no tasks on "+string(provider)) {
				t.Errorf("%s does not carry tasks on architecture: realtime and the refusal does not say so:\n%s", provider, text)
			}
		})
	}
}

// Small indirections so the test above reads as one thought rather than four
// package-qualified lookups.
func targetTable() targetcap.Table                        { return targetcap.Default() }
func realtimeTasksField() targetcap.Field                 { return targetcap.FieldRealtimeTasks }
func targetProvider(provider Provider) targetcap.Provider { return targetcap.Provider(provider) }
func coreTag() targetcap.Tag                              { return targetcap.Core }

// TestABackendCarryingParamsWarnsThatTheyReachNothing.
//
// A live backend is configured through the session's delegation, and this
// compiler sends the model id alone. Every other key on that think entry is
// dropped. That was silent, and worse than silent: compile-report.json recorded
// the params as forwarded, so the one file an author checks to see what the
// compiler resolved told them the setting had arrived.
//
// Found while writing examples/takeaway-orders, by an author reaching for
// reasoning_effort on the backend and reading the emitted module afterwards.
// A warning rather than a refusal, the way a turn binding carrying a field that
// reaches nothing warns: the entry is legal and its model id does real work.
func TestABackendCarryingParamsWarnsThatTheyReachNothing(t *testing.T) {
	for _, provider := range []Provider{ProviderLiveKit, ProviderPipecat} {
		t.Run(string(provider), func(t *testing.T) {
			agent := liveAgent(t)
			tgt := targetFor(agent, provider)
			backend := tgt.Models.Reason["fast"]
			backend.Params = map[string]any{"reasoning_effort": "none"}
			tgt.Models.Reason["fast"] = backend

			row := validateOne(t, agent, tgt)
			if len(row.Errors) != 0 {
				t.Fatalf("a backend carrying params is legal, not refused:\n%s", strings.Join(row.Errors, "\n"))
			}
			warnings := strings.Join(row.Warnings, "\n")
			for _, want := range []string{
				`backend "fast" carries params reasoning_effort`,
				"which reach nothing",
				// The fix, which is the half a bare "reaches nothing" leaves out.
				"architecture: cascade",
			} {
				if !strings.Contains(warnings, want) {
					t.Errorf("the warning is missing %q:\n%s", want, warnings)
				}
			}
		})
	}

	// The control. A backend with no params says nothing, because a warning that
	// fires on every package is one nobody reads.
	agent := liveAgent(t)
	row := validateOne(t, agent, targetFor(agent, ProviderLiveKit))
	if strings.Contains(strings.Join(row.Warnings, "\n"), "reach nothing") {
		t.Errorf("a backend carrying no params warned anyway:\n%s", strings.Join(row.Warnings, "\n"))
	}
}
