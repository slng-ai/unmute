//go:build smoke

package generate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// Real pinned native classes send through a recording HTTP transport. No key,
// model request or audio is needed to catch a constructor or EU-routing drift.
func TestSmokeNativeGeminiEU(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	agent := loadCompilerAgent(t)
	dir := t.TempDir()
	args := []string{"run", "--python", "3.12"}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		tgt := targetByProvider(t, agent, provider)
		for name := range tgt.Models.Reason {
			tgt.Models.Reason[name] = ir.Binding{
				Provider: "google", Model: "gemini-3.5-flash-lite",
				Params: map[string]any{"vertexai": true, "location": "eu", "thinking_config": map[string]any{"thinking_level": "minimal"}},
			}
		}
		artifact, err := Generate(agent, tgt, target.Default())
		if err != nil {
			t.Fatal(err)
		}
		module, dependency := "bot.py", "pipecat-ai"
		if provider == ir.ProviderLiveKit {
			module, dependency = "agent.py", "livekit-agents"
		}
		path := filepath.Join(dir, "build", string(provider))
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, module), []byte(artifactFile(t, artifact, module)), 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, "--with", fmt.Sprintf("%s[google]==%s", dependency, tgt.Version))
	}
	script, err := filepath.Abs("../../scripts/check_gemini_eu.py")
	if err != nil {
		t.Fatal(err)
	}
	args = append(args, "python", script, dir)
	cmd := exec.Command("uv", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native Gemini check failed: %v\n%s", err, out)
	} else {
		t.Logf("%s", out)
	}
}
