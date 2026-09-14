package ir

import (
	"os"
	"path/filepath"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
)

// TestRealtimeOverrideKeepsTheTurnDecision: a per-target `models:` override
// replaces the vendor selection, and a handful of fields are carried forward
// because an override cannot mean to drop them. `turn_detection:` is the
// hardest of the set to lose, because an author cannot write it into an
// override at all: packagespec.ModelDef has no such field, so an override that
// changes nothing but the model id used to compile a bot with the provider's
// own detector standing where the authored one was, with nothing on stdout, in
// the compile report or in the runbook saying a model had been swapped out.
//
// A live entry is refused a per-target override outright for the same family of
// reason. A realtime entry is not, because it compiles on both targets and a
// per-target vendor swap is a thing an author legitimately wants.
func TestRealtimeOverrideKeepsTheTurnDecision(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "testdata", "realtime_model"))); err != nil {
		t.Fatal(err)
	}
	// The narrowest override there is: one model id, nothing else.
	targets := `targets:
  livekit:
    provider: livekit
    version: "1.8.1"
    models:
      voice:
        provider: openai
        model: gpt-realtime-mini
        voice: marin
`
	if err := os.WriteFile(filepath.Join(root, "targets.yaml"), []byte(targets), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, err := packagespec.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	binding, ok := targetFor(agent, ProviderLiveKit).Models.Realtime["voice"]
	if !ok {
		t.Fatal("the overridden realtime entry resolved to no binding")
	}
	if binding.Model != "gpt-realtime-mini" {
		t.Errorf("model = %q, want the override's gpt-realtime-mini", binding.Model)
	}
	if binding.TurnDetection != "local" {
		t.Errorf("turn_detection = %q, want the authored local: the override dropped the turn decision, "+
			"so this target answers on the provider's detector and says so nowhere", binding.TurnDetection)
	}
}

// TestRealtimeReachesTheReportAndTheSecretCheck: a realtime package references
// no think, listen or speak binding, so a walk that reads only those three sees
// nothing at all. Two readers depend on that walk, and both went quiet:
//
//   - compile-report.json, which is where every binding, param and sizing
//     figure moved when `compile` stopped printing them, showed a realtime
//     package no bindings whatsoever;
//   - the undeclared-secret warning, which is the only thing between an author
//     and a worker that starts, answers the phone and 401s on the first word.
func TestRealtimeReachesTheReportAndTheSecretCheck(t *testing.T) {
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "realtime_model"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	tgt := targetFor(agent, ProviderLiveKit)

	var realtime []ForwardedBinding
	for _, binding := range forwardedBindings(tgt) {
		if binding.Role == "realtime" {
			realtime = append(realtime, binding)
		}
	}
	if len(realtime) != 1 || realtime[0].Binding.Model != "gpt-realtime" {
		t.Errorf("the report carries %+v, want the one realtime binding: the one file a reader is "+
			"sent to for what the compiler resolved showed a realtime package nothing", realtime)
	}

	var named bool
	for _, want := range providerKeyEnvNames(agent, tgt) {
		if want.name == "OPENAI_API_KEY" {
			named = true
		}
	}
	if !named {
		t.Error("the realtime model's provider key reaches the secret check nowhere, " +
			"so a package declaring no secrets is warned about none")
	}
}
