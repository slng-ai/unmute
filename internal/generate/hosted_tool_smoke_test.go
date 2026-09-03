//go:build smoke

package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// The mirrored SLNG module, actually run, and the emitted call actually made.
//
// This is the layer that can catch the one thing goldens cannot see about a
// hosted code tool: whether the platform's contract is what we think it is. The
// emitted call is `handler(Input(**kwargs))`, recovered from a real platform
// response rather than from documentation, and every other test in this feature
// only checks that we wrote those words. This one checks that they run.
//
// It also proves the second-order thing the header exists for: the module is
// another system's source, it carries a lint suppression, and it still has to
// import and execute as Python.
//
// No credential and no network. The module answers from a table, which is what
// makes it safe to run: SLNG code tools have no network access at all, so a
// hosted tool that reached one would not be a `code` tool.
func TestSmokeHostedCodeToolRunsThroughThePlatformContract(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		t.Run(string(provider), func(t *testing.T) {
			agent := buildHosted(t, hostedCodeFixture)
			artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			for _, file := range artifact.Files {
				path := filepath.Join(dir, file.Path)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, file.Content, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "smoke_check.py"), []byte(hostedToolSmokeScript), 0o644); err != nil {
				t.Fatal(err)
			}
			// pydantic only. The mirrored module imports it, and it is already a
			// hard dependency of both frameworks, so this adds nothing an
			// emitted project does not already install.
			cmd := exec.Command("uv", "run", "--with", "pydantic", "python", "smoke_check.py")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("smoke check failed:\n%s", out)
			} else {
				t.Logf("%s", out)
			}
		})
	}
}

const hostedToolSmokeScript = `
import inspect
import re
from pathlib import Path

import tools.check_order as mirrored

source = Path("tools/check_order.py").read_text()

# The header the pull writes, still on the file the container will import.
assert source.startswith("# ruff: noqa"), source[:120]
assert "Edit it in the SLNG dashboard, not here." in source

# SLNG's contract, which is what the emitted call reaches for. Recovered from a
# real platform response: the module defines an Input model and a handler taking
# one, so a lowering that called a function named after the tool would work by
# luck on some modules and fail on others.
assert hasattr(mirrored, "handler"), dir(mirrored)
assert hasattr(mirrored, "Input"), dir(mirrored)
signature = inspect.signature(mirrored.handler)
assert len(signature.parameters) == 1, signature

# The call the generated agent makes, made.
result = mirrored.handler(mirrored.Input(order_number="A-1001"))
dumped = result.model_dump() if hasattr(result, "model_dump") else result
assert isinstance(dumped, dict), dumped
assert dumped["status"] == "shipped", dumped
assert dumped["delivers_on"] == "2026-09-02", dumped

# An unknown order still answers, because the module handles it. A raise here
# would reach the model as a tool failure rather than as an answer.
unknown = mirrored.handler(mirrored.Input(order_number="nope")).model_dump()
assert unknown["status"] == "unknown", unknown

# And the emitted agent module names the same two symbols this file just used.
# Both drivers write the call, so whichever one built this directory has to have
# written it the same way.
agent_source = ""
for candidate in ("agent.py", "bot.py"):
    if Path(candidate).exists():
        agent_source = Path(candidate).read_text()
        break
assert agent_source, "neither agent.py nor bot.py was emitted"
assert re.search(r"tools\.check_order\.handler\(", agent_source), "the emitted call does not go through handler()"
assert re.search(r"tools\.check_order\.Input\(", agent_source), "the emitted call does not build Input()"

print("hosted code tool: handler(Input(...)) ran and answered")
`
