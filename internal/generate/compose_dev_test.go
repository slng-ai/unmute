package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

// devComposeArtifact compiles safe_core to the named provider and returns the
// emitted compose.dev.yaml. safe_core carries both a pipecat and a livekit
// target, so one fixture covers both dev-compose goldens.
func devComposeArtifact(t *testing.T, provider ir.Provider) string {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
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
	return artifactFile(t, artifact, "compose.dev.yaml")
}

// TestPipecatDevComposeGolden locks the pipecat web dev topology: one built
// application service, no coordination store (the web path uses none), env by
// name only. SPEC T1, V2, V3, V11. Zero Python, zero Docker.
func TestPipecatDevComposeGolden(t *testing.T) {
	compose := devComposeArtifact(t, ir.ProviderPipecat)
	assertValidYAML(t, compose)
	assertGoldenFile(t, filepath.Join("testdata", "golden", "pipecat_v1_dev_compose.yaml"), compose, *updatePipecatV1)

	for _, want := range []string{
		"build:\n      context: .",
		`command: ["python", "bot.py", "--host", "0.0.0.0", "--port", "7860"]`,
		"${UNMUTE_DEV_PORT:-7860}:7860",
		"- OPENAI_API_KEY",
		"healthcheck:",
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("pipecat compose.dev.yaml missing %q:\n%s", want, compose)
		}
	}
	// The web path never runs a coordination store (no-idle-sidecar rule); and
	// no secret value is ever written into the file (env by name only).
	for _, forbidden := range []string{"image: valkey/valkey:", "image: redis:", "REDIS_URL", "=sk-", "livekit_server"} {
		if strings.Contains(compose, forbidden) {
			t.Errorf("pipecat compose.dev.yaml contains %q:\n%s", forbidden, compose)
		}
	}
}

// TestLiveKitDevComposeGolden locks the livekit web dev topology: a pinned
// single-node dev server publishing the three browser-WebRTC ports plus the
// built worker, no coordination store. SPEC T1, V2, V4, V11. Zero Python,
// zero Docker.
func TestLiveKitDevComposeGolden(t *testing.T) {
	compose := devComposeArtifact(t, ir.ProviderLiveKit)
	assertValidYAML(t, compose)
	assertGoldenFile(t, filepath.Join("testdata", "golden", "livekit_v1_dev_compose.yaml"), compose, *updateLiveKitV1)

	for _, want := range []string{
		"image: livekit/livekit-server:v1.13.4",
		`command: ["--dev", "--bind", "0.0.0.0", "--node-ip", "127.0.0.1", "--udp-port", "7882"]`,
		"7881:7881",
		"7882:7882/udp",
		`command: ["python", "agent.py", "dev"]`,
		"LIVEKIT_URL=ws://livekit_server:7880",
		"LIVEKIT_API_SECRET=secret",
		"condition: service_healthy",
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("livekit compose.dev.yaml missing %q:\n%s", want, compose)
		}
	}
	// Single node needs no external store; the hardcoded dev key/secret must be
	// the LiveKit --dev placeholder, never a real value.
	for _, forbidden := range []string{"image: valkey/valkey:", "image: redis:", "--redis-host", "devsecret-local-only"} {
		if strings.Contains(compose, forbidden) {
			t.Errorf("livekit compose.dev.yaml contains %q:\n%s", forbidden, compose)
		}
	}
}
