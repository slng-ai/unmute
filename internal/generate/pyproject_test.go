package generate

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// Every code target ends its pyproject.toml with the one checker block, and
// that block keeps the rules the readability bar depends on. The rules
// themselves run in `make lint-emitted` and `make smoke`; this is the part a
// test with no Python can hold: nobody drops a rule family, and no target
// forgets the block or tells ty a Python version its requires-python does not
// allow.
func TestEveryCodeTargetDeclaresTheCheckerBlock(t *testing.T) {
	for _, family := range []string{`"PERF"`, `"SIM"`, `"C4"`, `"FURB"`, `"BLE"`, `"D"`, `"ANN"`, `convention = "google"`} {
		if !strings.Contains(pythonCheckersFormat, family) {
			t.Errorf("pythonCheckers no longer selects %s", family)
		}
	}

	floor := regexp.MustCompile(`requires-python = ">=([0-9.]+)`)
	simple := filepath.Join("..", "testdata", "simple-prompt")
	for _, tc := range []struct {
		name     string
		artifact func(t *testing.T) Artifact
	}{
		{"livekit", func(t *testing.T) Artifact { return providerArtifact(t, simple, ir.ProviderLiveKit) }},
		{"pipecat", func(t *testing.T) Artifact { return providerArtifact(t, simple, ir.ProviderPipecat) }},
		{"twilio", func(t *testing.T) Artifact {
			return twilioArtifact(t, filepath.Join("..", "testdata", "twilio"), "twilio")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pyproject := artifactFile(t, tc.artifact(t), "pyproject.toml")
			match := floor.FindStringSubmatch(pyproject)
			if match == nil {
				t.Fatalf("pyproject.toml has no requires-python floor:\n%s", pyproject)
			}
			if !strings.HasSuffix(pyproject, pythonCheckers(match[1])) {
				t.Errorf("pyproject.toml does not end with the checker block for Python %s:\n%s", match[1], pyproject)
			}
		})
	}
}

// providerArtifact compiles a package for the first target instance on one
// provider.
func providerArtifact(t *testing.T, dir string, provider ir.Provider) Artifact {
	t.Helper()
	pkg, err := spec.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return artifact
}
