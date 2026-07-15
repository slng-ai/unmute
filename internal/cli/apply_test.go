package cli

import (
	"path/filepath"
	"strings"
	"testing"
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
	safe := filepath.Join("..", "..", "examples", "safe_core")
	out, err := run(t, "apply", safe, "--target", "vapi-prod")
	if err == nil {
		t.Fatalf("expected managed driver not-implemented error; got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "vapi driver is not implemented") {
		t.Fatalf("unexpected error: %v", err)
	}
}
