package ir

import (
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// loadLive loads the live_model fixture and lets a test mutate the authored
// package before Build reads it, so each case is the file an author would
// actually have written rather than a hand-built IR.
func loadLive(t *testing.T, mutate func(pkg *packagespec.Package)) error {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "live_model"))
	if err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(pkg)
	}
	_, err = Build(pkg)
	return err
}

// TestArchitectureDecidesWhichSectionsAreLegal is the whole point of the key
// being written rather than derived. Each case is a package that says one thing
// with the key and another with its sections or its bindings, and each refusal
// has to name the key, because that is the fact the author can act on.
//
// The derived form reported these as a run of missing-binding errors further
// down, none of which named the cause. That is what this replaces.
func TestArchitectureDecidesWhichSectionsAreLegal(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(pkg *packagespec.Package)
		want   []string
	}{
		{
			name: "a value outside the three",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Architecture = "speech_to_speech"
			},
			want: []string{`architecture "speech_to_speech" is not one this compiler knows`, "cascade, realtime, live"},
		},
		{
			name: "a live section under cascade",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Architecture = packagespec.ArchitectureCascade
			},
			want: []string{"models.live belongs to architecture: live, but this package is architecture: cascade", "Write architecture: live, or remove the section"},
		},
		{
			name: "a realtime section under live",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Models.Realtime = []packagespec.RealtimeDef{{
					Name: "other", Provider: "openai", Model: "gpt-realtime", Voice: "marin",
				}}
			},
			want: []string{"models.realtime belongs to architecture: realtime, but this package is architecture: live"},
		},
		{
			name: "an architecture with no section to run on",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Architecture = packagespec.ArchitectureRealtime
				pkg.Agent.Models.Live = nil
			},
			want: []string{"architecture: realtime needs a models.realtime section naming the model to run on"},
		},
		{
			name: "an agent binding the other architecture's word",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Models.Realtime = []packagespec.RealtimeDef{{
					Name: "voice", Provider: "openai", Model: "gpt-realtime", Voice: "marin",
				}}
				pkg.Agent.Models.Live = nil
				pkg.Agent.Architecture = packagespec.ArchitectureRealtime
				def := pkg.Agent.Agents["desk"]
				def.Live, def.Realtime = "voice", ""
				pkg.Agent.Agents["desk"] = def
			},
			want: []string{`agent "desk" names live:, which belongs to architecture: live, but this package is architecture: realtime`},
		},
		{
			name: "a cascaded agent binding a speech to speech word",
			mutate: func(pkg *packagespec.Package) {
				pkg.Agent.Architecture = packagespec.ArchitectureCascade
				pkg.Agent.Models.Live = nil
				pkg.Agent.Models.Realtime = []packagespec.RealtimeDef{{
					Name: "voice", Provider: "openai", Model: "gpt-realtime", Voice: "marin",
				}}
			},
			// The section is refused first: it is the earlier and more specific
			// fact, and fixing it is what the author would do anyway.
			want: []string{"models.realtime belongs to architecture: realtime"},
		},
		{
			// The architecture check has to run before the think and speak kind
			// checks, not just before the live and realtime ones. Below them it
			// told an author writing think: under architecture: live that their
			// live entry was the wrong kind of model, which is true and is not
			// the mistake.
			name: "a live agent also naming think",
			mutate: func(pkg *packagespec.Package) {
				def := pkg.Agent.Agents["desk"]
				def.Think = "voice"
				pkg.Agent.Agents["desk"] = def
			},
			want: []string{`agent "desk" names live "voice" and also think or speak`, "named by that entry's own backend key, not by the agent"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := loadLive(t, tc.mutate)
			if err == nil {
				t.Fatal("the package was accepted; the architecture decided nothing")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("want %q in:\n%v", want, err)
				}
			}
		})
	}
}

// TestAnArchitectureRefusalPointsAtTheKey: the line has to be the key's own,
// not the first line that happens to contain the word. The live fixture's
// opening comment describes the live architecture and sits on line one, so
// every refusal about the key pointed there until KeyLocation existed. A line
// number that is confidently wrong is worse than none: it sends the reader to a
// comment and tells them the compiler cannot read their file.
func TestAnArchitectureRefusalPointsAtTheKey(t *testing.T) {
	err := loadLive(t, func(pkg *packagespec.Package) {
		pkg.Agent.Architecture = "speech_to_speech"
	})
	if err == nil {
		t.Fatal("an unknown architecture was accepted")
	}
	if strings.Contains(err.Error(), "agent.yaml:1:") || strings.Contains(err.Error(), "agent.yaml:1 ") {
		t.Errorf("the refusal points at line 1, which is the fixture's comment, not its key: %v", err)
	}
	if !strings.Contains(err.Error(), "agent.yaml:") {
		t.Errorf("the refusal names no line at all: %v", err)
	}
}

// TestOmittedArchitectureIsCascade: every package written before this key
// existed has to keep compiling, and keep compiling to the same shape. The
// resolved value is never empty, so no reader downstream has to treat an absent
// key as a fourth case.
func TestOmittedArchitectureIsCascade(t *testing.T) {
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "simple-prompt"))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Agent.Architecture != "" {
		t.Fatalf("the fixture writes architecture %q; this test needs one that writes none", pkg.Agent.Architecture)
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if agent.Architecture != ArchitectureCascade {
		t.Errorf("architecture resolved to %q, want cascade", agent.Architecture)
	}
	for _, target := range agent.Targets {
		if target.Models.Realtime != nil || target.Models.Live != nil {
			t.Errorf("a cascaded package resolved a speech to speech binding: %+v", target.Models)
		}
	}
}

// realtimeAgent builds a package on architecture: realtime, from the live
// fixture, so the realtime checks have something to run against.
func realtimeAgent(t *testing.T, mutate func(entry *packagespec.RealtimeDef, agent *packagespec.AgentDef)) *Agent {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "live_model"))
	if err != nil {
		t.Fatal(err)
	}
	entry := packagespec.RealtimeDef{
		Name: "voice", Provider: "openai", Model: "gpt-realtime",
		Voice: "marin", TurnDetection: packagespec.TurnDetectionSemantic,
	}
	// A speak entry for the half cascade cases to name. Declared always, because
	// an unreferenced entry is a legal palette alternate and costs the cases
	// that do not use it nothing.
	pkg.Agent.Models.Speak = map[string]packagespec.ModelDef{
		"warm": {Provider: "cartesia", Model: "sonic-3", Voice: "a-voice-id"},
	}
	def := pkg.Agent.Agents["desk"]
	def.Live = ""
	def.Realtime = "voice"
	if mutate != nil {
		mutate(&entry, &def)
	}
	pkg.Agent.Architecture = packagespec.ArchitectureRealtime
	pkg.Agent.Models.Live = nil
	pkg.Agent.Models.Realtime = []packagespec.RealtimeDef{entry}
	pkg.Agent.Agents["desk"] = def
	agent, err := Build(pkg)
	if err != nil {
		t.Fatalf("the realtime package does not build: %v", err)
	}
	return agent
}

// realtimeEnabled is the capability table with architecture: realtime turned on
// for Pipecat. No target emits a realtime pipeline yet, so the shipped table
// denies the field everywhere and validateRealtime never runs against it.
//
// Without this the realtime checks would be forty lines that no test executes:
// every one of them would read as covered, because the denial fires first and
// the row it produces looks like a refusal. That is exactly how untested
// validation ships. These tests turn the field on and hold the checks
// themselves; TestRealtimeIsRefusedOnEveryTargetToday holds the denial.
func realtimeEnabled() targetcap.Table {
	table := targetcap.Default()
	fields := maps.Clone(table.Fields)
	row := maps.Clone(fields[targetcap.FieldRealtimeModel])
	row[targetcap.Pipecat] = targetcap.Capability{Tag: targetcap.Core}
	fields[targetcap.FieldRealtimeModel] = row
	table.Fields = fields
	return table
}

// TestRealtimeIsRefusedOnSlngAlone: both code targets emit this architecture, so
// slng is the only one that denies the binding by name, and its refusal says
// what to write instead. A denial with no note is an error an author cannot act
// on.
//
// This asserted every target refused it until the drivers landed. The reason it
// changed is that two drivers emit a pipeline now, not that a rule was relaxed:
// the slng row still denies, and for a shape reason rather than a driver gap.
func TestRealtimeIsRefusedOnSlngAlone(t *testing.T) {
	agent := realtimeAgent(t, nil)
	for _, provider := range []Provider{ProviderPipecat, ProviderLiveKit, ProviderSlng} {
		tgt := targetFor(agent, ProviderPipecat)
		tgt.Provider, tgt.Name = provider, string(provider)
		switch provider {
		case ProviderLiveKit:
			tgt.Version = "1.8.1"
		case ProviderSlng:
			tgt.Version = ""
		}
		row := validateOne(t, agent, tgt)
		text := strings.Join(row.Errors, "\n")
		if provider != ProviderSlng {
			continue // the code targets are held by their own driver tests
		}
		if !strings.Contains(text, "architecture: realtime") {
			t.Errorf("%s: the refusal does not name the architecture:\n%s", provider, text)
		}
		if !strings.Contains(text, "architecture: cascade") {
			t.Errorf("%s: the refusal names nothing to write instead:\n%s", provider, text)
		}
	}
}

// TestRealtimeChecksRunWhenTheTargetTakesTheBinding holds validateRealtime
// itself, with the field turned on. Each case is a package the compiler must
// refuse once a driver emits this shape, and each refusal has to name the fix.
func TestRealtimeChecksRunWhenTheTargetTakesTheBinding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(entry *packagespec.RealtimeDef, agent *packagespec.AgentDef)
		want   []string
	}{
		{
			name: "a voice and a speak binding both",
			mutate: func(entry *packagespec.RealtimeDef, agent *packagespec.AgentDef) {
				agent.Speak = "warm"
			},
			want: []string{`has voice "marin" and agent "desk" also binds speak "warm"`, "Remove the entry's voice:"},
		},
		{
			name: "neither a voice nor a speak binding",
			mutate: func(entry *packagespec.RealtimeDef, _ *packagespec.AgentDef) {
				entry.Voice = ""
			},
			want: []string{"names no voice and agent \"desk\" binds no speak model, so nothing would speak"},
		},
		{
			name: "a turn_detection outside the grammar",
			mutate: func(entry *packagespec.RealtimeDef, _ *packagespec.AgentDef) {
				entry.TurnDetection = "vad"
			},
			want: []string{`turn_detection "vad" is not one this compiler knows`, "server_vad, semantic, local"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := realtimeAgent(t, tc.mutate)
			tgt := targetFor(agent, ProviderPipecat)
			report, _ := Validate(agent, []Target{tgt}, realtimeEnabled())
			text := strings.Join(report.PerTarget[0].Errors, "\n")
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("want %q in:\n%s", want, text)
				}
			}
		})
	}
}

// TestRealtimeAcceptsTheShapeItIsMeantTo: the accepted package, so the cases
// above are refusing something specific rather than everything.
func TestRealtimeAcceptsTheShapeItIsMeantTo(t *testing.T) {
	agent := realtimeAgent(t, nil)
	tgt := targetFor(agent, ProviderPipecat)
	report, _ := Validate(agent, []Target{tgt}, realtimeEnabled())
	if errs := report.PerTarget[0].Errors; len(errs) != 0 {
		t.Errorf("the shape realtime exists for is refused:\n%s", strings.Join(errs, "\n"))
	}
}

// TestRealtimeTakesTheHalfCascade: a realtime model with no voice of its own and
// a speak binding is the half cascade, and it is accepted. This is the shape the
// two refusals above sit either side of, so without it they could both be
// passing by refusing everything.
func TestRealtimeTakesTheHalfCascade(t *testing.T) {
	agent := realtimeAgent(t, func(entry *packagespec.RealtimeDef, def *packagespec.AgentDef) {
		entry.Voice = ""
		def.Speak = "warm"
	})
	tgt := targetFor(agent, ProviderPipecat)
	report, _ := Validate(agent, []Target{tgt}, realtimeEnabled())
	if errs := report.PerTarget[0].Errors; len(errs) != 0 {
		t.Errorf("the half cascade is refused:\n%s", strings.Join(errs, "\n"))
	}
}

// TestRealtimeRefusesEverythingLiveRefusesForTheSameReason.
//
// The two speech-to-speech architectures do not carry the same things for the
// same reasons — realtime takes a new prompt mid-call and live does not — but
// four of the limits ARE the same, because they are this release's, not the
// vendors'. Tracing, call state, an MCP tool and a phone connection are refused
// on both, and the sentence a reader gets should not depend on which one they
// picked.
//
// Written because they were accepted in silence on realtime until a driver
// existed to expose it. A package could declare `tracing:` and compile to a
// worker that exports nothing. Nothing failed: validateLive had the checks and
// validateRealtime, added later, did not, and no test compared the two.
func TestRealtimeRefusesEverythingLiveRefusesForTheSameReason(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(agent *Agent, tgt *Target)
		want   string
	}{
		{"tracing", func(a *Agent, _ *Target) { a.Tracing = &Tracing{Provider: "langfuse"} },
			"tracing is not emitted under architecture: %s in this version"},
		{"call state", func(a *Agent, _ *Target) { a.Variables = map[string]Variable{"seen": {Type: "str"}} },
			"variables and prefetch are not emitted under architecture: %s in this version"},
		{"a phone connection", func(_ *Agent, tgt *Target) { tgt.Connection = "twilio_voice" },
			"architecture: %s compiles for the browser route in this version"},
		{"an escalation", func(a *Agent, _ *Target) { a.Controls = map[string]Control{"person": &HumanTransfer{}} },
			`escalation "person" is not emitted under architecture: %s in this version`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, shape := range []struct {
				architecture string
				agent        func(*testing.T) *Agent
			}{
				{"live", func(t *testing.T) *Agent { return liveAgent(t) }},
				{"realtime", func(t *testing.T) *Agent { return realtimeAgent(t, nil) }},
			} {
				agent := shape.agent(t)
				tgt := targetFor(agent, ProviderPipecat)
				tc.mutate(agent, &tgt)
				row := validateOne(t, agent, tgt)
				text := strings.Join(row.Errors, "\n")
				want := fmt.Sprintf(tc.want, shape.architecture)
				if !strings.Contains(text, want) {
					t.Errorf("architecture: %s does not refuse %s\nwant: %s\ngot:\n%s",
						shape.architecture, tc.name, want, text)
				}
			}
		})
	}
}
