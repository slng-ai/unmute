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
