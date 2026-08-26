package generate

import (
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

// TestPublicExamplesEmitLintCleanPython runs `ruff check` over every shipped
// example's emitted Python on both code drivers. It exists because of B4: a
// helper referenced `quote` while its import rode a different condition, so the
// emitted module was a NameError waiting for the first call that took that
// branch. Nothing else would have caught it — the module imports fine, so the
// L4 smoke passes, and `ruff format` (which compile runs) reports layout, not
// undefined names.
//
// It is in the default suite, not the smoke tag, because the whole point is to
// catch this on every run. `ruff check` needs only the ruff binary: no venv, no
// project dependencies, no Python execution, so the zero-Python promise of
// L1–L3 still holds in spirit. It skips cleanly when ruff is absent.
func TestPublicExamplesEmitLintCleanPython(t *testing.T) {
	ruff, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("ruff not installed")
	}
	root := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "agent.yaml")); err != nil {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join(root, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range sortedTargetNames(agent) {
				resolved := agent.Targets[name]
				if !target.IsCode(target.Provider(resolved.Provider)) {
					continue
				}
				artifact, err := Generate(agent, resolved, target.Default())
				if err != nil {
					t.Fatalf("generate %q: %v", name, err)
				}
				dir := t.TempDir()
				var python []string
				for _, file := range artifact.Files {
					if !strings.HasSuffix(file.Path, ".py") {
						continue
					}
					path := filepath.Join(dir, file.Path)
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, file.Content, 0o644); err != nil {
						t.Fatal(err)
					}
					python = append(python, path)
				}
				if len(python) == 0 {
					continue
				}
				if output, err := ruffCheck(ruff, python...); err != nil {
					t.Errorf("%s/%s emitted Python fails ruff check:\n%s", entry.Name(), name, output)
				}
			}
		})
	}
}

func sortedTargetNames(agent *ir.Agent) []string {
	names := make([]string, 0, len(agent.Targets))
	for name := range agent.Targets {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// A shape no shipped example has, held to the same ruff standard.
//
// TestPublicExamplesEmitLintCleanPython above only sees `examples/`, and nothing
// there declares a task whose tools include an agent transfer. So the emitted
// rollback path for that shape had no lint coverage at all, and an
// `except BaseException as error:` whose body never read `error` (ruff F841)
// shipped in it. The only thing that caught it was TestSmokePipecatV1TaskTransferStopsFlow,
// which needs Python, a venv and 18 seconds, and is not the PR gate.
//
// Emitted from the same fixture the smoke test uses, so the two cannot drift.
func TestTaskTransferEmitsLintCleanPython(t *testing.T) {
	ruff, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("ruff not installed")
	}
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	addPipecatTaskTransferFixture(agent)
	artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "bot.py")
	if err := os.WriteFile(path, []byte(artifactFile(t, artifact, "bot.py")), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := ruffCheck(ruff, path); err != nil {
		t.Errorf("a task that transfers emits Python that fails ruff check:\n%s", output)
	}
}

// ruffCheck runs the one ruff invocation both gates above use. --isolated
// ignores any ruff config above the temp dir (the stray-config trap
// compile_ruff_test.go already documents), and E501 is line length, which
// emitted prompts legitimately exceed.
func ruffCheck(ruff string, paths ...string) ([]byte, error) {
	args := append([]string{"check", "--isolated", "--select", "E,F,W", "--ignore", "E501"}, paths...)
	return exec.Command(ruff, args...).CombinedOutput()
}
