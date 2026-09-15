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

// Real pinned native classes send through a recording HTTP transport. Mixing
// locations in each module catches an adapter that accidentally shares a route.
func TestSmokeNativeGeminiLocations(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	dir := t.TempDir()
	args := []string{"run", "--python", "3.12"}
	var packages []string
	for i, locations := range [][2]string{{"us", "eu"}, {"global", "us-central1"}, {"europe-west4", "unsupported-location"}, {"", ""}} {
		pkg := filepath.Join(dir, fmt.Sprint(i))
		packages = append(packages, pkg)
		for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
			agent := loadCompilerAgent(t)
			tgt := targetByProvider(t, agent, provider)
			for j, name := range []string{"fast_reasoning", "careful_reasoning"} {
				params := map[string]any{"thinking_config": map[string]any{"thinking_level": "minimal"}, "temperature": 0.2}
				if locations[j] != "" {
					params["vertexai"], params["location"] = true, locations[j]
				}
				tgt.Models.Reason[name] = ir.Binding{Provider: "google", Model: "gemini-3.5-flash-lite", Params: params}
			}
			artifact, err := Generate(agent, tgt, target.Default())
			if err != nil {
				t.Fatal(err)
			}
			module, dependency := "bot.py", "pipecat-ai"
			if provider == ir.ProviderLiveKit {
				module, dependency = "agent.py", "livekit-agents"
			}
			path := filepath.Join(pkg, "build", string(provider))
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, module), []byte(artifactFile(t, artifact, module)), 0o644); err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				args = append(args, "--with", fmt.Sprintf("%s[google]==%s", dependency, tgt.Version))
			}
		}
	}
	script, err := filepath.Abs("../../scripts/check_gemini.py")
	if err != nil {
		t.Fatal(err)
	}
	args = append(args, "python", script)
	args = append(args, packages...)
	cmd := exec.Command("uv", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native Gemini check failed: %v\n%s", err, out)
	} else {
		t.Logf("%s", out)
	}
}
