package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

// devArtifact compiles safe_core to the named provider. safe_core carries both
// code targets, so one fixture covers their shared dev invariants.
func devArtifact(t *testing.T, provider ir.Provider) Artifact {
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
	return artifact
}

func devComposeArtifact(t *testing.T, provider ir.Provider) string {
	t.Helper()
	return artifactFile(t, devArtifact(t, provider), "compose.dev.yaml")
}

func TestV13CodeTargetsUseDotenv(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		t.Run(string(provider), func(t *testing.T) {
			artifact := devArtifact(t, provider)
			for _, file := range artifact.Files {
				if strings.Contains(string(file.Content), ".env.local") {
					t.Errorf("%s contains .env.local; local dotenv filename is .env", file.Path)
				}
			}
			entrypoint := map[ir.Provider]string{ir.ProviderPipecat: "bot.py", ir.ProviderLiveKit: "agent.py"}[provider]
			if source := artifactFile(t, artifact, entrypoint); !strings.Contains(source, "load_dotenv()") {
				t.Errorf("%s does not load .env with load_dotenv()", entrypoint)
			}
		})
	}
}

func TestV15CodeTargetsExcludeDotenvFromDockerBuild(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		t.Run(string(provider), func(t *testing.T) {
			dockerignore := artifactFile(t, devArtifact(t, provider), ".dockerignore")
			if !strings.Contains("\n"+dockerignore, "\n.env\n") {
				t.Fatalf(".dockerignore does not exclude .env:\n%s", dockerignore)
			}
		})
	}
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
