package ir

import (
	"path/filepath"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
)

// realtimeAgent builds the realtime_live fixture: one agent on a live model
// backed by an OpenAI think entry, two tools, a greeting and an idle nudge.
func realtimeAgent(t *testing.T) *Agent {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "realtime_live"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// TestValidateRealtimeAcceptsOneAgentOnALiveModel is the accepted shape, and
// what it resolves to: a realtime binding carrying its backend, no open listen
// or turn role demanded, and the OpenAI key on the startup check.
func TestValidateRealtimeAcceptsOneAgentOnALiveModel(t *testing.T) {
	agent := realtimeAgent(t)
	if agent.Agents["desk"].Realtime != "live" || agent.Agents["desk"].Model != "" || agent.Agents["desk"].Voice != "" {
		t.Fatalf("agent resolved to %+v", agent.Agents["desk"])
	}
	if agent.Models["live"].Kind != KindRealtime || agent.Models["live"].Think != "fast" {
		t.Fatalf("realtime model resolved to %+v", agent.Models["live"])
	}
	tgt := targetFor(agent, ProviderPipecat)
	binding, ok := tgt.Models.Realtime["live"]
	if !ok || binding.Think != "fast" || binding.Voice != "marin" || binding.Model != "gpt-live-1" {
		t.Fatalf("realtime binding resolved to %+v", tgt.Models.Realtime)
	}
	if _, ok := tgt.Models.Reason["fast"]; !ok {
		t.Fatal("the think entry the realtime model names is not a used reason binding")
	}
	if tgt.Models.Listen != nil || tgt.Models.Turn != nil || len(tgt.Models.Speak) != 0 {
		t.Fatalf("a realtime package resolved listen, turn or speak bindings: %+v", tgt.Models)
	}
	row := validateOne(t, agent, tgt)
	if len(row.Errors) != 0 {
		t.Fatalf("unexpected errors:\n%s", strings.Join(row.Errors, "\n"))
	}
}

// TestValidateRealtimeRefusesEveryShapeItCannotHonour holds every row of the
// realtime refusal table in specs/021 contracts/authoring.md.
func TestValidateRealtimeRefusesEveryShapeItCannotHonour(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(agent *Agent, tgt *Target)
		want   []string
	}{
		{
			name: "a second agent",
			mutate: func(agent *Agent, _ *Target) {
				agent.Agents["billing"] = AgentDef{Instructions: "Billing.", Realtime: "live"}
			},
			want: []string{`a realtime model serves one agent: "live" is bound by "billing" and the package also declares agent desk`, "cascaded pipeline"},
		},
		{
			name: "a task",
			mutate: func(agent *Agent, _ *Target) {
				agent.Tasks = map[string]Task{"verify": {Instructions: "Verify."}}
			},
			want: []string{"a realtime model serves one agent with no tasks", "a live session fixes them when it starts"},
		},
		{
			name: "a listen section",
			mutate: func(agent *Agent, tgt *Target) {
				agent.Listen = "transcriber"
				agent.Models["transcriber"] = ModelDef{Kind: KindListen, Provider: "deepgram", Model: "nova-3"}
				tgt.Models.Listen = &Binding{Provider: "deepgram", Model: "nova-3"}
			},
			want: []string{`models.listen is not used when agent "desk" binds realtime model "live": the live model listens itself`},
		},
		{
			name: "a turn section",
			mutate: func(agent *Agent, tgt *Target) {
				agent.Turn = "detector"
				agent.Models["detector"] = ModelDef{Kind: KindTurn, Provider: "local", Model: "silero"}
				tgt.Models.Turn = &Binding{Provider: "local", Model: "silero"}
			},
			want: []string{`models.turn is not used when agent "desk" binds realtime model "live": the live model decides the turn itself`},
		},
		{
			name: "a speak section",
			mutate: func(agent *Agent, _ *Target) {
				agent.Models["voice"] = ModelDef{Kind: KindSpeak, Provider: "cartesia", Voice: "x"}
			},
			want: []string{"models.speak is not used when agent \"desk\" binds realtime model \"live\": the live model speaks itself"},
		},
		{
			name: "interruption settings",
			mutate: func(agent *Agent, _ *Target) {
				on := true
				agent.Conversation.Interruption = &Interruption{Enabled: &on}
			},
			want: []string{"conversation.interruption reaches nothing on a live model: it handles being talked over itself"},
		},
		{
			name: "call state",
			mutate: func(agent *Agent, _ *Target) {
				agent.Variables = map[string]Variable{"caller": {}}
			},
			want: []string{"variables and prefetch are not emitted for a live model in this version"},
		},
		{
			name: "tracing",
			mutate: func(agent *Agent, _ *Target) {
				agent.Tracing = &Tracing{Provider: "langfuse"}
			},
			want: []string{"tracing is not emitted for a live model in this version"},
		},
		{
			name: "a telephony connection",
			mutate: func(_ *Agent, tgt *Target) {
				tgt.Connection = "twilio_voice"
			},
			want: []string{`a live model compiles for the browser route in this version: connection "twilio_voice"`},
		},
		{
			name: "a backend that is not at OpenAI",
			mutate: func(agent *Agent, tgt *Target) {
				backend := tgt.Models.Reason["fast"]
				backend.Provider = "anthropic"
				tgt.Models.Reason["fast"] = backend
			},
			want: []string{`realtime model "live" think "fast" is provider "anthropic": the live model's backend runs at OpenAI`},
		},
		{
			name: "tools with no backend",
			mutate: func(agent *Agent, tgt *Target) {
				live := agent.Models["live"]
				live.Think = ""
				agent.Models["live"] = live
				binding := tgt.Models.Realtime["live"]
				binding.Think = ""
				tgt.Models.Realtime["live"] = binding
			},
			want: []string{`realtime model "live" is bound by agent "desk", which has tools, and names no think entry`},
		},
		{
			name: "a vendor with no live service",
			mutate: func(agent *Agent, tgt *Target) {
				binding := tgt.Models.Realtime["live"]
				binding.Provider = "anthropic"
				tgt.Models.Realtime["live"] = binding
			},
			want: []string{`realtime binding provider "anthropic" has no slot`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := realtimeAgent(t)
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

// TestValidateRealtimeIsRefusedOffPipecat: the other two targets deny the
// binding by name and say where it compiles, and nothing else about the
// package is reported, because the denial is the whole answer there.
func TestValidateRealtimeIsRefusedOffPipecat(t *testing.T) {
	agent := realtimeAgent(t)
	for _, tc := range []struct {
		provider Provider
		want     string
	}{
		{ProviderLiveKit, "a realtime model compiles on pipecat only: the livekit driver emits no pipeline"},
		{ProviderSlng, "slng target binds listen, think and speak by name and has no slot for a realtime model"},
	} {
		tgt := targetFor(agent, ProviderPipecat)
		tgt.Provider, tgt.Name = tc.provider, string(tc.provider)
		if tc.provider == ProviderLiveKit {
			tgt.Version = "1.6.10"
		} else {
			tgt.Version = ""
		}
		row := validateOne(t, agent, tgt)
		text := strings.Join(row.Errors, "\n")
		if !strings.Contains(text, tc.want) {
			t.Errorf("%s: want %q in:\n%s", tc.provider, tc.want, text)
		}
		if strings.Contains(text, "missing open listen binding") || strings.Contains(text, "is missing realtime binding") {
			t.Errorf("%s: the denial should stand alone, got:\n%s", tc.provider, text)
		}
	}
}

// TestBuildRefusesAnAgentNamingBothFormsOrNeither: the authored shape is one of
// think and speak, or realtime; Build says which is missing and names the fix.
func TestBuildRefusesAnAgentNamingBothFormsOrNeither(t *testing.T) {
	load := func(t *testing.T, mutate func(pkg *packagespec.Package)) error {
		t.Helper()
		pkg, err := packagespec.Load(filepath.Join("..", "testdata", "realtime_live"))
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
	}); err == nil || !strings.Contains(err.Error(), `names realtime "live" and also think or speak`) {
		t.Errorf("both forms: %v", err)
	}
	if err := load(t, func(pkg *packagespec.Package) {
		def := pkg.Agent.Agents["desk"]
		def.Realtime = ""
		pkg.Agent.Agents["desk"] = def
	}); err == nil || !strings.Contains(err.Error(), "names no think model") {
		t.Errorf("neither form: %v", err)
	}
	if err := load(t, func(pkg *packagespec.Package) {
		def := pkg.Agent.Agents["desk"]
		def.Realtime = "fast"
		pkg.Agent.Agents["desk"] = def
	}); err == nil || !strings.Contains(err.Error(), `realtime "fast" is a think model, not a realtime model`) {
		t.Errorf("wrong kind: %v", err)
	}
	if err := load(t, func(pkg *packagespec.Package) {
		pkg.Agent.Models.Realtime[0].Think = "live"
	}); err == nil || !strings.Contains(err.Error(), "is a realtime model, not a think model") {
		t.Errorf("backend of the wrong kind: %v", err)
	}
	if err := load(t, func(pkg *packagespec.Package) {
		pkg.Agent.Models.Realtime = append(pkg.Agent.Models.Realtime, pkg.Agent.Models.Realtime[0])
	}); err == nil || !strings.Contains(err.Error(), `models.realtime declares "live" twice`) {
		t.Errorf("duplicate name: %v", err)
	}
	// A clash with another section is the other mistake, and says so.
	if err := load(t, func(pkg *packagespec.Package) {
		pkg.Agent.Models.Realtime[0].Name = "fast"
	}); err == nil || !strings.Contains(err.Error(), `model name "fast" appears in both think and realtime`) {
		t.Errorf("name taken by a think entry: %v", err)
	}
	if err := load(t, func(pkg *packagespec.Package) {
		tgt := pkg.Targets["pipecat"]
		tgt.Models = map[string]packagespec.ModelDef{"live": {Provider: "openai", Model: "other"}}
		pkg.Targets["pipecat"] = tgt
	}); err == nil || !strings.Contains(err.Error(), `overrides realtime model "live"; a realtime entry takes no per-target override`) {
		t.Errorf("per-target override: %v", err)
	}
}
