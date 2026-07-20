package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
)

func TestApply_codeTargetSaysUseCompile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := run(t, "init", dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	out, err := run(t, "apply", dir) // scaffold has only a pipecat (code) target
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if !strings.Contains(out, "use `unmute compile`") {
		t.Fatalf("expected code-target guidance; got:\n%s", out)
	}
}

func TestApply_managedTargetNotImplementedYet(t *testing.T) {
	safe := filepath.Join("..", "testdata", "safe_core")
	out, err := run(t, "apply", safe, "--target", "vapi")
	if err == nil {
		t.Fatalf("expected managed driver not-implemented error; got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "vapi driver is not implemented") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestElevenLabsResidencyBase covers region -> regional base mapping (item 4):
// residency keys authenticate against a regional base, an unknown region does
// not resolve (caller warns + falls back), and the empty region uses the default.
func TestElevenLabsResidencyBase(t *testing.T) {
	known := map[string]string{
		"eu":        "https://api.eu.residency.elevenlabs.io",
		"IN":        "https://api.in.residency.elevenlabs.io",
		"singapore": "https://api.sg.residency.elevenlabs.io",
		"us":        "https://api.us.elevenlabs.io",
	}
	for region, want := range known {
		if got, ok := elevenLabsResidencyBase(region); !ok || got != want {
			t.Errorf("region %q -> %q (ok=%v), want %q", region, got, ok, want)
		}
	}
	if _, ok := elevenLabsResidencyBase("mars"); ok {
		t.Error("unknown region should not resolve")
	}
	if got, ok := elevenLabsResidencyBase(""); !ok || got != providerBaseURL[ir.ProviderElevenLabs] {
		t.Errorf("empty region -> %q (ok=%v), want default base", got, ok)
	}
}
