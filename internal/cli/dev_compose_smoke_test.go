//go:build smoke

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/generate"
	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
)

// TestSmokeTelephonyComposeTopologies is credential-free: it builds each
// generated application, waits for its exact local graph, proves stopping
// Redis makes the graph incomplete, and then proves a clean restart. It does
// not exercise or promote a carrier route.
func TestSmokeTelephonyComposeTopologies(t *testing.T) { // telephony V26
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("docker is not installed")
	}
	if output, err := exec.Command(docker, "info").CombinedOutput(); err != nil {
		t.Skipf("docker daemon is unavailable: %v (%s)", err, strings.TrimSpace(string(output)))
	}

	for _, tc := range []struct {
		name     string
		artifact func(*testing.T) generate.Artifact
		services []string
	}{
		{name: "pipecat", artifact: smokePipecatTelephonyArtifact, services: []string{"application", "redis"}},
		{name: "livekit_sip", artifact: smokeLiveKitSIPArtifact, services: []string{"application", "livekit_server", "livekit_sip", "redis"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outDir := filepath.Join(t.TempDir(), tc.name)
			artifact := tc.artifact(t)
			if err := writeArtifactFiles(outDir, artifact.Files); err != nil {
				t.Fatal(err)
			}
			composeFile := filepath.Join(outDir, "compose.telephony.yaml")
			project := composeProjectName(outDir, tc.name)
			env := scrubArtifactEnvironment(os.Environ(), artifactFileContent(t, artifact, ".env.example"))
			env = setChildEnv(env, "UNMUTE_TELEPHONY_PORT", "0")
			run := func(args ...string) ([]byte, error) {
				cmd := exec.Command(docker, composeArgs(composeFile, project, args...)...)
				cmd.Dir, cmd.Env = outDir, env
				return cmd.CombinedOutput()
			}
			t.Cleanup(func() {
				_, _ = run("down", "--remove-orphans", "--volumes")
			})

			if output, err := run("up", "--build", "--detach", "--wait"); err != nil {
				t.Fatalf("compose up: %v\n%s", err, output)
			}
			assertComposeServices(t, run, tc.services)

			if output, err := run("stop", "redis"); err != nil {
				t.Fatalf("stop Redis: %v\n%s", err, output)
			}
			running, err := composeRunningServices(run)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(running, "redis") || slices.Equal(running, tc.services) {
				t.Fatalf("topology remained ready without Redis: %v", running)
			}

			if output, err := run("up", "--detach", "--wait"); err != nil {
				t.Fatalf("clean restart: %v\n%s", err, output)
			}
			assertComposeServices(t, run, tc.services)
		})
	}
}

func smokePipecatTelephonyArtifact(t *testing.T) generate.Artifact {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	enableSmokeTelephony(pkg)
	target := pkg.Targets["pipecat"]
	target.Transport, target.Carrier, target.Connection = "carrier-websocket", "twilio", "primary_phone"
	pkg.Targets = map[string]spec.Target{"pipecat": target}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := generate.GeneratePipecat(agent, agent.Targets["pipecat"], nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func smokeLiveKitSIPArtifact(t *testing.T) generate.Artifact {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	enableSmokeTelephony(pkg)
	target := pkg.Targets["livekit"]
	target.Transport, target.Carrier, target.Connection = "sip", "twilio", "primary_phone"
	pkg.Targets = map[string]spec.Target{"livekit": target}
	connection := pkg.Connections["primary_phone"]
	connection.Environment = map[string]string{
		"sip_address": "TWILIO_SIP_ADDRESS", "sip_username": "TWILIO_SIP_USERNAME",
		"sip_password": "TWILIO_SIP_PASSWORD", "from_number": "TWILIO_PHONE_NUMBER",
	}
	pkg.Connections["primary_phone"] = connection
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := generate.GenerateLiveKit(agent, agent.Targets["livekit"], nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func enableSmokeTelephony(pkg *spec.Package) {
	inbound, outbound := true, false
	pkg.Agent.Channels["phone"] = spec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
		RequiredControls: []string{"cold_transfer", "hangup"},
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

func scrubArtifactEnvironment(env []string, example string) []string {
	for _, line := range strings.Split(example, "\n") {
		name, _, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && name != "" {
			env = setChildEnv(env, name, "")
		}
	}
	return env
}

func composeRunningServices(run func(...string) ([]byte, error)) ([]string, error) {
	output, err := run("ps", "--status", "running", "--services")
	if err != nil {
		return nil, err
	}
	services := strings.Fields(string(output))
	slices.Sort(services)
	return services, nil
}

func assertComposeServices(t *testing.T, run func(...string) ([]byte, error), want []string) {
	t.Helper()
	got, err := composeRunningServices(run)
	if err != nil {
		t.Fatal(err)
	}
	want = slices.Clone(want)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("running services = %v, want %v", got, want)
	}
}
