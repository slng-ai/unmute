package generate

import (
	"path/filepath"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
)

// `semantic_endpointing` was validated, reported, and documented on four
// docs-site pages, three skill reference files and the agent-yaml table, and it
// reached nothing in any emitted project. Both drivers marked it advisory and no
// template read it.
//
// A field that looks authored and changes no output is what Principle II
// forbids, and it mattered here because `pace` lands right beside it: adding a
// second field naming the same subject while the first stayed dead would put two
// owners on one surface with only one of them working.
//
// These are the gates that make it load-bearing.

// TestTurnFieldsSurviveAPerTargetOverride is the gate on a bug this feature
// found twice, in two fields, for the same reason.
//
// A per-target `models:` override replaces the whole ModelDef rather than merging
// into it, so any field the override does not restate is dropped.
// `endpointing_delay` and `prompt_suffix` already had explicit carry-forwards in
// resolveBindings for exactly this. `pace` and `semantic_endpointing` did not.
//
// `semantic_endpointing`'s version was latent: the field reached no emitted
// project, so an override silently dropping it changed nothing observable. It
// surfaced the moment the field became load-bearing, on the flagship example,
// which overrides its turn binding on both targets — so `off` on the base binding
// reached neither.
//
// This runs against examples/salon-concierge deliberately. safe_core has no turn
// override, so a test that configures the resolved binding directly skips the
// merge and cannot see this class of bug at all.
func TestTurnFieldsSurviveAPerTargetOverride(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
	if err != nil {
		t.Fatal(err)
	}
	// Set all three fields on the base turn binding, as an author would. They
	// are set here rather than read off the file so this proves the
	// carry-forward whatever the example happens to author: it used to take
	// endpointing_delay from salon-concierge's own base binding, so dropping
	// that line from the package silently turned one of these three
	// assertions into a no-op.
	eager := true
	for name, def := range pkg.Agent.Models.Turn {
		def.Pace = string(ir.PaceSnappy)
		def.SemanticEndpointing = string(ir.SemanticEndpointingOff)
		def.EndpointingDelay = "400ms"
		// The fourth field, and the one that was missing from this loop while
		// the carry-forward was missing from Build: an override that says
		// nothing about the early answer compiled a bot without it, with no
		// line anywhere saying the feature had been turned off.
		def.Eager = &eager
		pkg.Agent.Models.Turn[name] = def
	}

	agent, err := buildWithState(t, pkg)
	if err != nil {
		t.Fatal(err)
	}
	overridden := 0
	for name, resolved := range agent.Targets {
		if resolved.Models.Turn == nil {
			continue
		}
		overridden++
		if got := resolved.Models.Turn.Pace; got != ir.PaceSnappy {
			t.Errorf("target %q resolved pace %q, want %q: a per-target override dropped it", name, got, ir.PaceSnappy)
		}
		if got := resolved.Models.Turn.SemanticEndpointing; got != ir.SemanticEndpointingOff {
			t.Errorf("target %q resolved semantic_endpointing %q, want %q: a per-target override dropped it", name, got, ir.SemanticEndpointingOff)
		}
		// The one that already had a carry-forward, asserted here too so all
		// four live in one place.
		if resolved.Models.Turn.EndpointingDelay == "" {
			t.Errorf("target %q lost its endpointing_delay", name)
		}
		if !resolved.Models.Turn.Eager {
			t.Errorf("target %q resolved eager false: a per-target override dropped the early answer", name)
		}
	}
	if overridden == 0 {
		t.Fatal("the salon example has no resolved turn binding, so this test proves nothing")
	}
}
