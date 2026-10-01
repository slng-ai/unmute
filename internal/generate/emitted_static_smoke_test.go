//go:build smoke

package generate

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// emittedEntryModule is the module each code target runs, and so the one an
// import check loads.
var emittedEntryModule = map[ir.Provider]string{
	ir.ProviderLiveKit: "agent",
	ir.ProviderPipecat: "bot",
	ir.ProviderTwilio:  "app",
}

// staticCheckFixtures are the internal packages that reach code no example
// does: tasks and groups (remy, terminal_step), typed session state
// (typed_state) and the Daily carrier transport (daily_carrier).
var staticCheckFixtures = []string{"remy", "terminal_step", "typed_state", "daily_carrier"}

// TestSmokeEmittedProjectsPassTheirOwnGate holds every example, on every code
// target it declares, to the bar its generated pyproject.toml sets, using the
// commands a user would run in that project:
//
//  1. `uv run ruff check .` with the rules the project selects.
//  2. `uv run ty check .` with its SDKs installed.
//  3. `ruff format` leaves it stable. The generator never formats; `unmute
//     compile` does, so this is the write-path check.
//  4. The entry module imports under `uv run python`, with placeholder values
//     for the environment variables compile-report.json names.
//
// It replaces the per-driver example lists that used to run a subset of these
// on a subset of the examples.
func TestSmokeEmittedProjectsPassTheirOwnGate(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	entries, err := os.ReadDir(filepath.Join("..", "..", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	packages := slices.Clone(staticCheckFixtures)
	for _, entry := range entries {
		if entry.IsDir() {
			packages = append(packages, entry.Name())
		}
	}
	for _, name := range packages {
		pkg, err := spec.Load(examplePackagePath(name))
		if err != nil {
			t.Fatal(err)
		}
		agent, err := ir.Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		for _, instance := range slices.Sorted(maps.Keys(agent.Targets)) {
			tgt := agent.Targets[instance]
			module, ok := emittedEntryModule[tgt.Provider]
			if !ok {
				continue // the slng target writes no Python
			}
			t.Run(name+"/"+instance, func(t *testing.T) {
				artifact, err := Generate(agent, tgt, target.Default())
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
				for _, args := range [][]string{
					{"run", "ruff", "check", "."},
					{"run", "ty", "check", "."},
					{"run", "ruff", "format", "."},
					{"run", "ruff", "format", "--check", "."},
				} {
					runIn(t, dir, args...)
				}
				// Written after the checks, so they see only what a user compiles.
				if err := os.WriteFile(filepath.Join(dir, "smoke_import.py"), []byte(withKnowledgeStub(importCheckScript(module))), 0o644); err != nil {
					t.Fatal(err)
				}
				runIn(t, dir, "run", "python", "smoke_import.py")
			})
		}
	}
}

// importCheckScript imports one emitted module with every required variable
// set to a placeholder.
func importCheckScript(module string) string {
	return `"""Smoke check: the entry module imports."""
import json
import os

for name in json.load(open("compile-report.json"))["required_env"]:
    os.environ.setdefault(name, "smoke-placeholder")

import ` + module + ` as _entry  # noqa: E402

print("imported", _entry.__name__)
`
}

// runIn runs one uv command in dir and fails the test with its output.
func runIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := uvCommand(args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("uv %s failed:\n%s", strings.Join(args, " "), out)
	}
}
