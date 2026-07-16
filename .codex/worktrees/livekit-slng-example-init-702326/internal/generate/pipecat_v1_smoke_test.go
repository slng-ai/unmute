//go:build smoke

package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

// TestSmokePipecatV1BotPyCompiles proves the emitted bot.py is valid Python
// (V9, L4). Opt-in (`make smoke` / -tags smoke), never in the default suite.
func TestSmokePipecatV1BotPyCompiles(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatal(err)
	}

	var bot []byte
	for _, file := range artifact.Files {
		if file.Path == "bot.py" {
			bot = file.Content
		}
	}
	if bot == nil {
		t.Fatal("bot.py not generated")
	}
	botPath := filepath.Join(t.TempDir(), "bot.py")
	if err := os.WriteFile(botPath, bot, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("uv", "run", "--no-project", "python", "-m", "py_compile", botPath)
	cmd.Env = append(os.Environ(), "UV_CACHE_DIR="+filepath.Join(t.TempDir(), "uv-cache"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bot.py failed py_compile:\n%s", out)
	}
}
