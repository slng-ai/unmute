//go:build smoke

package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/slng/unmute/internal/scaffold"
)

func TestSmokePipecatBotPyCompiles(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}

	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := scaffold.Write(dir, scaffold.Data{Name: "support-bot"}); err != nil {
		t.Fatal(err)
	}
	input := loadPipecatInput(t, dir)
	result, err := GeneratePipecat(input)
	if err != nil {
		t.Fatal(err)
	}

	var bot []byte
	for _, artifact := range result.Artifacts {
		if artifact.Path == "bot.py" {
			bot = artifact.Content
			break
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
