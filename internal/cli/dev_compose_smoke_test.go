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
			if err := writeArtifactFiles(nil, outDir, artifact.Files); err != nil {
				t.Fatal(err)
			}
			composeFile := filepath.Join(outDir, "compose.telephony.yaml")
			project := composeProjectName(outDir, tc.name)
			env := placeholderArtifactEnvironment(os.Environ(), artifactFileContent(t, artifact, ".env.example"))
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
				// `--wait` reports only "container X is unhealthy", which says
				// nothing about why. Dump the service logs so a failure here is
				// diagnosable from CI output alone.
				logs, _ := run("logs", "--no-color")
				t.Fatalf("compose up: %v\n%s\n--- service logs ---\n%s", err, output, logs)
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
	// The carrier-websocket routes carry media only: SCHEMA N31 made human
	// transfers native-route-only, so safe_core's cold transfer cannot ride this
	// one and asking for it fails the build. This test is about the local Compose
	// graph, not transfers, so the fixture drops the transfer and asks the route
	// for nothing it does not have.
	dropHumanTransfers(pkg)
	enableSmokeTelephony(pkg, "hangup")
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
	// The SIP route has the cold-transfer primitive (N31), so this one keeps
	// safe_core's transfer and requires the control.
	enableSmokeTelephony(pkg, "cold_transfer", "hangup")
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

// enableSmokeTelephony puts an inbound phone channel on the package. The
// required controls are per route, because a route that cannot do one fails the
// build rather than degrading (SCHEMA N31).
func enableSmokeTelephony(pkg *spec.Package, requiredControls ...string) {
	inbound, outbound := true, false
	pkg.Agent.Channels["phone"] = spec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
		RequiredControls: requiredControls,
	}
}

// dropHumanTransfers removes safe_core's cold transfer, for a route that has no
// transfer primitive. The control is attached to an agent's tool list, so both
// sides go, otherwise the reference dangles.
func dropHumanTransfers(pkg *spec.Package) {
	for name, control := range pkg.Agent.Controls {
		if control.Kind != "human_transfer" {
			continue
		}
		delete(pkg.Agent.Controls, name)
		for agentName, agent := range pkg.Agent.Agents {
			agent.Tools = slices.DeleteFunc(agent.Tools, func(tool string) bool { return tool == name })
			pkg.Agent.Agents[agentName] = agent
		}
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

// placeholderArtifactEnvironment overrides every name the generated
// `.env.example` lists, so an ambient credential in the developer's shell can
// never make this test pass for the wrong reason.
//
// It sets placeholders rather than blanks. `/readyz` answers 503 while any
// REQUIRED_ENV name is empty, so a blanked environment can never report the
// topology ready: the healthcheck spends its thirty retries on 503s and every
// subtest fails with an opaque "container is unhealthy". Presence is what the
// readiness contract asks for, and presence is what this supplies.
func placeholderArtifactEnvironment(env []string, example string) []string {
	for _, line := range strings.Split(example, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if name, _, ok := strings.Cut(line, "="); ok && name != "" {
			env = setChildEnv(env, name, smokeEnvValue(name))
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
