package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/devmetrics"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The measurement producers are the one place where a Go constant and generated
// Python have to agree on a literal string. Nothing else in the tree can catch a
// disagreement: the Python is emitted, `examples/*/build/` is gitignored, and a
// producer reading the wrong switch name is not a crash, it is a feature that
// silently prints nothing while every other test stays green. So the switch name
// and the sentinel are asserted here, against the constants that own them.
func TestDevMetricsProducerAgreesWithTheGoContract(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pkg      string
		provider ir.Provider
		entry    string
		wiring   string
	}{
		{"pipecat", "safe_core", ir.ProviderPipecat, "bot.py", "observers=[dev_metrics_observer()]"},
		{"livekit", "remy", ir.ProviderLiveKit, "agent.py", "install_dev_metrics(session)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			artifact := generateFor(t, tc.pkg, tc.provider)

			producer := artifactFile(t, artifact, "dev_metrics.py")
			if !strings.Contains(producer, `METRICS_ENV = "`+devmetrics.Env+`"`) {
				t.Errorf("producer does not read %s, so the dev loop cannot switch it on", devmetrics.Env)
			}
			if !strings.Contains(producer, `SENTINEL = "`+strings.TrimSpace(devmetrics.Sentinel)+`"`) {
				t.Errorf("producer does not print the sentinel %q the decoder looks for", devmetrics.Sentinel)
			}
			// The framing has to survive a container prefix, which means the
			// sentinel and the payload are separated by exactly one space.
			if !strings.Contains(producer, `f"{SENTINEL} {json.dumps(record, separators=(',', ':'))}"`) {
				t.Error("producer does not emit the sentinel followed by one space and compact JSON")
			}
			if !strings.Contains(producer, "flush=True") {
				t.Error("producer does not flush, so records can arrive long after the turn")
			}

			entry := artifactFile(t, artifact, tc.entry)
			if !strings.Contains(entry, tc.wiring) {
				t.Errorf("%s does not wire the producer: want %q", tc.entry, tc.wiring)
			}
		})
	}
}

// Emission must not depend on configuration. A producer emitted only when some
// feature is on would make build/<target>/ depend on which command last ran, so
// the dev loop would stop exercising the file that ships.
func TestDevMetricsIsEmittedEvenWhenNothingElseIs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pkg      string
		provider ir.Provider
	}{
		{"livekit unconfigured", "remy", ir.ProviderLiveKit},
		{"pipecat", "safe_core", ir.ProviderPipecat},
	} {
		t.Run(tc.name, func(t *testing.T) {
			artifact := generateFor(t, tc.pkg, tc.provider)
			if !artifactHasFile(artifact, "dev_metrics.py") {
				t.Error("artifact does not carry dev_metrics.py")
			}
		})
	}
}

func generateFor(t *testing.T, pkgName string, provider ir.Provider) Artifact {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", pkgName))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

// The switch reaching the producer is a second, separate contract. A LiveKit
// worker runs inside a container and a Pipecat phone route can too, so setting
// the variable on the child process is not enough: a container receives only
// what its compose service declares. This shipped once with the producer wired
// correctly and the variable never arriving, which looks exactly like a target
// that reports nothing, so the compose files are pinned here.
func TestEveryComposeThatRunsAnAgentForwardsTheSwitch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pkg      string
		provider ir.Provider
		composes []string
	}{
		{"pipecat", "safe_core", ir.ProviderPipecat, []string{"compose.dev.yaml"}},
		{"livekit", "remy", ir.ProviderLiveKit, []string{"compose.dev.yaml"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			artifact := generateFor(t, tc.pkg, tc.provider)
			for _, name := range tc.composes {
				compose := artifactFile(t, artifact, name)
				// Bare name, no value: compose forwards the host's value when it
				// is set and omits the variable entirely when it is not, which is
				// what keeps a deployed artifact silent.
				if !strings.Contains(compose, "\n      - "+devmetrics.Env+"\n") {
					t.Errorf("%s does not forward %s, so the producer inside the container stays inert", name, devmetrics.Env)
				}
				if strings.Contains(compose, devmetrics.Env+"=") {
					t.Errorf("%s pins a value for %s; it must pass through so it is absent when unset", name, devmetrics.Env)
				}
			}
		})
	}
}
