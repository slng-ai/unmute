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

// smokeCheckScript imports the emitted bot and instantiates every service
// builder with placeholder env values. Importing alone proves the imports and
// dependency set; calling the builders proves the constructor kwargs against
// the real installed services — the drift class py_compile can never see
// (driver-pipecat B6).
const smokeCheckScript = `"""Smoke check: import the generated bot and instantiate every service."""
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import bot  # noqa: E402  (module import already constructs the agent workers)

builders = sorted(n for n in vars(bot) if n.startswith("build_") and callable(getattr(bot, n)))
assert builders, "no service builders found in bot.py"
for name in builders:
    getattr(bot, name)()
print("smoke ok:", ", ".join(builders))
`

// TestSmokePipecatV1ServicesInstantiate proves the safe_core emission end to
// end (V9, L4): uv resolves the emitted pyproject (network), bot.py imports,
// and every emitted service constructor accepts its emitted kwargs
// (deepgram Settings-style STT, slng flat-kwargs TTS, openai Settings LLM).
// Opt-in (`make smoke` / -tags smoke), never in the default suite.
func TestSmokePipecatV1ServicesInstantiate(t *testing.T) {
	runPipecatSmoke(t, nil, nil)
}

// TestSmokePipecatV1MultiVendorInstantiates covers the remaining official
// entries in one venv: assemblyai listen, elevenlabs + cartesia speak.
func TestSmokePipecatV1MultiVendorInstantiates(t *testing.T) {
	runPipecatSmoke(t, func(tgt *ir.Target) {
		tgt.Models.Listen = &ir.Binding{Provider: "assemblyai", Model: "universal-3-5-pro"}
		tgt.Models.Speak["front_desk"] = ir.Binding{
			Provider: "elevenlabs", Model: "eleven_multilingual_v2", Voice: "21m00Tcm4TlvDq8ikWAM",
		}
		tgt.Models.Speak["specialist"] = ir.Binding{
			Provider: "cartesia", Model: "sonic-3", Voice: "f786b574-daa5-4673-aa0c-cbe3e8534c02",
		}
	}, nil)
}

// TestSmokePipecatV1LocalToolInstantiates proves the local-tool lowering (T14,
// V13) against real pipecat-ai: importing bot constructs the agent workers, so
// the @tool wrapper class-collects and `import tools.fetch_notes` resolves the
// copied handler file inside the venv.
func TestSmokePipecatV1LocalToolInstantiates(t *testing.T) {
	runPipecatSmoke(t, nil, func(agent *ir.Agent) {
		agent.Tools["fetch_notes"] = ir.Tool{
			Description: "Fetch the caller's saved notes.",
			Input:       map[string]any{"type": "object", "properties": map[string]any{"topic": map[string]any{"type": "string"}}, "required": []any{"topic"}},
			Execution:   ir.ToolLocal, Handler: "tools/fetch_notes.py",
			HandlerSource: "def fetch_notes(topic):\n    return {\"notes\": []}\n",
			Interruption:  ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
		}
		intake := agent.Agents["intake"]
		intake.Tools = append(intake.Tools, "fetch_notes")
		agent.Agents["intake"] = intake
	})
}

func runPipecatSmoke(t *testing.T, mutate func(*ir.Target), mutateAgent func(*ir.Agent)) {
	t.Helper()
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
	if mutateAgent != nil {
		mutateAgent(agent)
	}
	tgt := targetByProvider(t, agent, ir.ProviderPipecat)
	if mutate != nil {
		mutate(&tgt)
	}
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	for _, file := range artifact.Files {
		out := filepath.Join(dir, file.Path)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, file.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "smoke_check.py"), []byte(smokeCheckScript), 0o644); err != nil {
		t.Fatal(err)
	}

	// uv resolves the emitted pyproject into a project venv (shared uv cache,
	// so repeat runs are fast) and runs the check inside it.
	cmd := exec.Command("uv", "run", "python", "smoke_check.py")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("smoke check failed:\n%s", out)
	} else {
		t.Logf("%s", out)
	}
}
