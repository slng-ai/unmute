//go:build smoke

package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The real pinned SDK serializes requests into an HTTP mock. No Agora account,
// microphone or paid provider call is used; the browser conversation is still owed.
func TestSmokeAgoraRuntime(t *testing.T) {
	uv, err := exec.LookPath("uv")
	if err != nil {
		t.Skip("uv is required for the pinned Agora smoke environment")
	}
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "agora-voice"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, agent.Targets["agora"], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, file := range artifact.Files {
		if err := os.WriteFile(filepath.Join(dir, file.Path), file.Content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "test_agora_runtime.py"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(uv, "run", "--no-project", "--with", "agora-agents=="+target.AgoraSDKVersion, "python", script, filepath.Join(dir, "server.py"))
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Agora runtime smoke: %v\n%s", err, output)
	}
}
