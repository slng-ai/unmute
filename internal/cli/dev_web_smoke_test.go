//go:build smoke

package cli

import (
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// requireDocker skips the test unless a working docker daemon is reachable.
func requireDocker(t *testing.T) string {
	t.Helper()
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("docker is not installed")
	}
	if output, err := exec.Command(docker, "info").CombinedOutput(); err != nil {
		t.Skipf("docker daemon is unavailable: %v (%s)", err, strings.TrimSpace(string(output)))
	}
	return docker
}

// smokeDevArtifact compiles safe_core to the named target for the dev (web)
// path — no telephony.
func smokeDevArtifact(t *testing.T, targetName string) (composeFile, outDir string, env []string) {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := generate.Generate(agent, agent.Targets[targetName], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	outDir = filepath.Join(t.TempDir(), targetName)
	if err := writeArtifactFiles(nil, outDir, artifact.Files); err != nil {
		t.Fatal(err)
	}
	env = placeholderArtifactEnvironment(os.Environ(), artifact.Telephony, artifactFileContent(t, artifact, ".env.example"))
	return filepath.Join(outDir, "compose.dev.yaml"), outDir, env
}

// TestSmokeDevComposeValidates proves real Docker accepts the emitted
// compose.dev.yaml for both code targets. Credential-free and build-free:
// `docker compose config` parses and resolves the file without pulling or
// building (SPEC T6 L4).
func TestSmokeDevComposeValidates(t *testing.T) {
	docker := requireDocker(t)
	for _, name := range []string{"pipecat", "livekit"} {
		t.Run(name, func(t *testing.T) {
			composeFile, outDir, env := smokeDevArtifact(t, name)
			env = setChildEnv(env, "UNMUTE_DEV_PORT", "0")
			cmd := exec.Command(docker, composeArgs(composeFile, "unmute-devsmoke-"+name, "config", "-q")...)
			cmd.Dir, cmd.Env = outDir, env
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("docker compose config (%s): %v\n%s", name, err, output)
			}
		})
	}
}

// TestSmokeDevPipecatImageReceivesEnvAndImportsWebRTC builds the emitted image,
// proves Compose passes a host env value, and imports its web transport inside
// that image (SPEC V12, V13).
func TestSmokeDevPipecatImageReceivesEnvAndImportsWebRTC(t *testing.T) {
	docker := requireDocker(t)
	composeFile, outDir, _ := smokeDevArtifact(t, "pipecat")
	values, err := parseDotenv(filepath.Join(outDir, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	for name := range values {
		t.Setenv(name, "")
	}
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte("OPENAI_API_KEY=unmute-env-smoke\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	env := packageEnv(outDir, io.Discard)
	project := composeProjectName(outDir, "pipecat")
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command(docker, composeArgs(composeFile, project, args...)...)
		cmd.Dir, cmd.Env = outDir, env
		return cmd.CombinedOutput()
	}
	t.Cleanup(func() { _, _ = run("down", "--remove-orphans") })

	if output, err := run("build", "application"); err != nil {
		t.Fatalf("build pipecat application: %v\n%s", err, output)
	}
	if output, err := run("run", "--rm", "--no-deps", "application", "python", "-c",
		"import os; assert os.environ['OPENAI_API_KEY'] == 'unmute-env-smoke'; from pipecat.transports.smallwebrtc.transport import SmallWebRTCTransport"); err != nil {
		t.Fatalf("verify environment and import SmallWebRTCTransport in pipecat image: %v\n%s", err, output)
	}
}

// TestSmokeDevLiveKitServerStarts brings up the single-node dev livekit-server
// alone (no worker, no creds) to prove the pinned image, --dev command, ports,
// and healthcheck are real (SPEC V4, T6 L4). The worker needs provider creds
// and a browser, so a full call stays a manual smoke.
func TestSmokeDevLiveKitServerStarts(t *testing.T) {
	docker := requireDocker(t)
	composeFile, outDir, env := smokeDevArtifact(t, "livekit")
	project := composeProjectName(outDir, "livekit")
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command(docker, composeArgs(composeFile, project, args...)...)
		cmd.Dir, cmd.Env = outDir, env
		return cmd.CombinedOutput()
	}
	t.Cleanup(func() { _, _ = run("down", "--remove-orphans", "--volumes") })

	if output, err := run("up", "--detach", "--wait", "livekit_server"); err != nil {
		t.Fatalf("livekit_server up: %v\n%s", err, output)
	}
	output, err := run("ps", "--status", "running", "--services")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(strings.Fields(string(output)), "livekit_server") {
		t.Fatalf("livekit_server not running: %s", output)
	}
}

func artifactFileContent(t *testing.T, artifact generate.Artifact, path string) string {
	t.Helper()
	for _, file := range artifact.Files {
		if file.Path == path {
			return string(file.Content)
		}
	}
	t.Fatalf("artifact missing %s", path)
	return ""
}

// smokeEnvValue is the placeholder one generated environment name gets. The
// values are deliberately fake and the public URL uses the reserved .invalid
// TLD (RFC 2606), so nothing here is a credential and no host is reachable.
func smokeEnvValue(name string) string {
	if name == "UNMUTE_PUBLIC_URL" {
		return "https://smoke.invalid" // must parse as an HTTPS origin
	}
	return "unmute-smoke-placeholder"
}

// placeholderArtifactEnvironment gives every name the project requires a
// placeholder value, so an ambient credential in the developer's shell can
// never make this test pass for the wrong reason. Placeholders rather than
// blanks: a name that is present but empty reads as unset to the generated
// startup check.
//
// It reads the **telephony plan's** required environment rather than
// `.env.example`. Those are two different lists on purpose: `.env.example` holds
// the names the author supplies, and the route's own values are deliberately
// absent from it (FR-018), because a to-do list whose entries are not the
// reader's to do is not a to-do list. `unmute dev` supplies them at run time;
// this test has to stand in for that, so it reads the complete list — the same
// one `compile-report.json` carries under `required_env`.
//
// Reading the env file here was what broke when the two lists diverged: the
// container started, `/readyz` demanded `UNMUTE_PUBLIC_URL`, and nothing had
// set it. That is the failure mode research D14 predicted for a human operator,
// arriving first in a test, which is where it should arrive.
func placeholderArtifactEnvironment(env []string, plan *generate.TelephonyRuntimePlan, example string) []string {
	names := map[string]bool{}
	if plan != nil {
		for _, name := range plan.RequiredEnv {
			names[name] = true
		}
	}
	for _, line := range strings.Split(example, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if name, _, ok := strings.Cut(line, "="); ok && name != "" {
			names[name] = true
		}
	}
	for _, name := range slices.Sorted(maps.Keys(names)) {
		env = setChildEnv(env, name, smokeEnvValue(name))
	}
	return env
}
