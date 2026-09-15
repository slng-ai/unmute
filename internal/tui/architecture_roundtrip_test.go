package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/scaffold"
	packagespec "github.com/slng-ai/unmute/internal/spec"
)

// TestMaintainKeepsTheArchitecture is the console half of the architecture key.
// `unmute maintain` rewrites agent.yaml from scaffold.Data, so a key that struct
// does not carry is a key the console drops. Dropping this one is quieter than
// dropping most: the rewritten file still compiles, it just compiles a cascaded
// pipeline where the author wrote a speech to speech one, and nothing about the
// result says a model was swapped out.
//
// Written in the shape every round-trip test in this package uses: build the
// data, write it, read it back, and check what came back.
func TestMaintainKeepsTheArchitecture(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-desk", EntryAgent: "assistant",
		Architecture: "live",
		Live: []scaffold.SpeechModel{{
			Name: "voice", Provider: "openai", Model: "gpt-live-1",
			Voice: "marin", Backend: "fast", Description: "the front desk",
		}},
		Backends: []scaffold.SpeechBackend{{
			Name: "fast", Binding: scaffold.Binding{Provider: "openai", Model: "gpt-5.6-terra"},
		}},
	}
	data.SetTarget("pipecat")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	if agent.data.Architecture != "live" {
		t.Errorf("architecture = %q, want live; the console turned a live package into a cascaded one", agent.data.Architecture)
	}
	if len(agent.data.Live) != 1 {
		t.Fatalf("live entries = %d, want 1: %+v", len(agent.data.Live), agent.data.Live)
	}
	got := agent.data.Live[0]
	want := scaffold.SpeechModel{
		Name: "voice", Provider: "openai", Model: "gpt-live-1",
		Voice: "marin", Backend: "fast", Description: "the front desk",
	}
	if got != want {
		t.Errorf("live entry = %+v, want %+v", got, want)
	}
	// Carrying the keys is half of it. The rewrite has to leave a package that
	// still builds: a live entry naming a backend no think entry declares reads
	// back fine and is refused by the next `unmute validate`.
	pkg, err := packagespec.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ir.Build(pkg); err != nil {
		t.Errorf("the console wrote a live package that no longer builds: %v", err)
	}
	// And the backend has to come back whole. Writing its name and inventing the
	// rest builds, validates and compiles, with the author's model id gone and
	// an empty one in the emitted session: the quietest way this can break, and
	// what it did until the entry was carried rather than derived.
	backend, ok := pkg.Agent.Models.Think["fast"]
	if !ok {
		t.Fatalf("the backend think entry is gone; models.think = %+v", pkg.Agent.Models.Think)
	}
	if backend.Model != "gpt-5.6-terra" || backend.Provider != "openai" {
		t.Errorf("the backend came back as %+v, want the authored provider and model", backend)
	}
}

// TestMaintainKeepsARealtimeModel is the same for the other speech to speech
// section, including turn_detection, which is the field an author is most
// likely to have tuned and least likely to notice going missing.
func TestMaintainKeepsARealtimeModel(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-desk", EntryAgent: "assistant",
		Architecture: "realtime",
		Realtime: []scaffold.SpeechModel{{
			Name: "voice", Provider: "openai", Model: "gpt-realtime",
			Voice: "marin", Turn: "semantic",
		}},
	}
	data.SetTarget("pipecat")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	if agent.data.Architecture != "realtime" {
		t.Errorf("architecture = %q, want realtime", agent.data.Architecture)
	}
	if len(agent.data.Realtime) != 1 || agent.data.Realtime[0].Turn != "semantic" {
		t.Errorf("realtime entries = %+v, want one carrying turn_detection semantic", agent.data.Realtime)
	}
}

// TestMaintainWritesNoArchitectureForACascade: the key is omitted, not written
// as `architecture: cascade`. A cascaded package's file has to come back out of
// the console exactly as it went in, and that includes not gaining a key the
// author never wrote.
func TestMaintainWritesNoArchitectureForACascade(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{Name: "pkg", AgentName: "acme-desk", EntryAgent: "assistant"}
	data.SetTarget("pipecat")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(root, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(written), "architecture:") {
		t.Errorf("a cascaded package gained an architecture: key it never wrote:\n%s", written)
	}
}

// TestMaintainKeepsAHalfCascade is the round-trip for the shape where the
// speech to speech model listens and thinks and a synthesizer of the author's
// choosing speaks. Three things used to go missing here, and each one leaves a
// file that reads fine:
//
//   - the agent's `speak:` binding, and the `models.speak` entry it names, so
//     the rewritten package no longer validates at all;
//   - the entry the agent binds, taken as the first in the section rather than
//     read, so a package listing an alternate first came back pointing at it;
//   - a backend named by two entries, written twice under one `think:` key, so
//     the rewritten file stopped parsing and the console could not reopen it.
func TestMaintainKeepsAHalfCascade(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-desk", EntryAgent: "assistant",
		Architecture: "realtime",
		Realtime: []scaffold.SpeechModel{
			// The alternate is first on purpose: the agent binds the second one.
			{Name: "cheap", Provider: "openai", Model: "gpt-realtime-mini", Turn: "semantic"},
			{Name: "voice", Provider: "openai", Model: "gpt-realtime", Turn: "semantic"},
		},
		SpeechBound: "voice",
		SpeechSpeak: scaffold.SpeechBackend{
			Name:    "warm",
			Binding: scaffold.Binding{Provider: "cartesia", Model: "sonic-3", Voice: "a0e99841-438c-4a64-b679-ae501e7d6091"},
		},
	}
	data.SetTarget("pipecat")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	pkg, err := packagespec.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	def, ok := pkg.Agent.Agents["assistant"]
	if !ok {
		t.Fatalf("the agent is gone; agents = %+v", pkg.Agent.Agents)
	}
	if def.Realtime != "voice" {
		t.Errorf("the agent binds %q, want voice: the console repointed it at another entry", def.Realtime)
	}
	if def.Speak != "warm" {
		t.Errorf("the agent's speak binding = %q, want warm", def.Speak)
	}
	speak, ok := pkg.Agent.Models.Speak["warm"]
	if !ok {
		t.Fatalf("the synthesizer entry is gone; models.speak = %+v", pkg.Agent.Models.Speak)
	}
	if speak.Provider != "cartesia" || speak.Model != "sonic-3" {
		t.Errorf("the synthesizer came back as %+v, want the authored provider and model", speak)
	}
	// A half cascade that no longer validates is the failure an author meets,
	// so hold the whole build rather than the keys alone.
	if _, err := ir.Build(pkg); err != nil {
		t.Errorf("the console wrote a half cascade that no longer builds: %v", err)
	}
	// Reading it back through the console has to give the same answer, because
	// that is the loop an author repeats.
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	if agent.data.SpeechBound != "voice" || agent.data.SpeechSpeak.Name != "warm" {
		t.Errorf("read back bound=%q speak=%q, want voice and warm", agent.data.SpeechBound, agent.data.SpeechSpeak.Name)
	}
}

// TestMaintainWritesOneBackendPerName: two live entries may name one backend,
// which is an ordinary palette and not a strange package. The backends render
// as mapping keys under `think:`, so writing one per entry wrote a duplicate
// key, and goccy refuses the file the console just produced.
func TestMaintainWritesOneBackendPerName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	data := scaffold.Data{
		Name: "pkg", AgentName: "acme-desk", EntryAgent: "assistant",
		Architecture: "live",
		Live: []scaffold.SpeechModel{
			{Name: "voice", Provider: "openai", Model: "gpt-live-1", Voice: "marin", Backend: "fast"},
			{Name: "spare", Provider: "openai", Model: "gpt-live-1", Voice: "cedar", Backend: "fast"},
		},
		SpeechBound: "voice",
		Backends: []scaffold.SpeechBackend{{
			Name: "fast", Binding: scaffold.Binding{Provider: "openai", Model: "gpt-5.6-terra"},
		}},
	}
	data.SetTarget("pipecat")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	// Loading is the assertion: a duplicate mapping key is a parse error here,
	// not something that surfaces later.
	pkg, err := packagespec.Load(root)
	if err != nil {
		t.Fatalf("the console wrote a file it cannot read back: %v", err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(agent.data.Backends) != 1 {
		t.Errorf("backends = %+v, want one entry for the one name both models share", agent.data.Backends)
	}
	if _, err := ir.Build(pkg); err != nil {
		t.Errorf("the console wrote a live package that no longer builds: %v", err)
	}
}
