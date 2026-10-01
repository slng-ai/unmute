package generate

import (
	"os"
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

// A from-import past the line limit comes out the way ruff's import sorter
// writes it, and everything else is left alone: a short line, an already
// wrapped one, a plain import and an indented one.
func TestWrapLongImportsWrapsOnlyWhatRuffWould(t *testing.T) {
	long := "from livekit.agents.voice import Agent, AgentSession, ModelSettings, RunContext, SpeechHandle"
	commented := long + "  # the voice API"
	src := strings.Join([]string{
		"from os import path",
		long,
		commented,
		"from x import (\n    a,\n)",
		"import " + strings.Repeat("a.", 45) + "b",
		"    " + long,
	}, "\n")
	want := strings.Join([]string{
		"from os import path",
		"from livekit.agents.voice import (\n    Agent,\n    AgentSession,\n    ModelSettings,\n    RunContext,\n    SpeechHandle,\n)",
		"from livekit.agents.voice import (  # the voice API\n    Agent,\n    AgentSession,\n    ModelSettings,\n    RunContext,\n    SpeechHandle,\n)",
		"from x import (\n    a,\n)",
		"import " + strings.Repeat("a.", 45) + "b",
		"    " + long,
	}, "\n")
	if got := string(wrapLongImports([]byte(src))); got != want {
		t.Errorf("wrapLongImports:\n%s\nwant:\n%s", got, want)
	}
}

// The emitted-python CI job installs ruff so compile formats with it, and that
// has to be the ruff every generated project pins, or the job checks layout a
// user's project would never have.
func TestCIFormatsWithThePinnedRuff(t *testing.T) {
	pin := regexp.MustCompile(`"ruff==([0-9.]+)"`).FindStringSubmatch(pythonCheckersFormat)
	if pin == nil {
		t.Fatal("pythonCheckers pins no ruff version")
	}
	ci, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ci), "ruff=="+pin[1]) {
		t.Errorf("ci.yml does not install ruff==%s, the version generated projects pin", pin[1])
	}
}
