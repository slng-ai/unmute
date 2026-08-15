//go:build smoke

package cli

import (
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
			env := placeholderArtifactEnvironment(os.Environ(), artifact.Telephony, artifactFileContent(t, artifact, ".env.example"))
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
	target.Connection = "primary_phone"
	pkg.Targets = map[string]spec.Target{"pipecat": target}
	smokeConnectionRoute(pkg, "primary_phone", "carrier-websocket", "twilio")
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := generate.GeneratePipecat(agent, agent.Targets["pipecat"], nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// generate.Generate attaches this; the driver entry point does not, and the
	// test needs it to know which names the route supplies rather than the author.
	artifact.Telephony = generate.TelephonyRuntimePlanFor(agent.Targets["pipecat"])
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
	target.Connection = "primary_phone"
	pkg.Targets = map[string]spec.Target{"livekit": target}
	smokeConnectionRoute(pkg, "primary_phone", "sip", "twilio")
	connection := pkg.Connections["primary_phone"]
	// Carrier-prefixed names on purpose. The shipped example moved to the plain
	// SIP names on 2026-08-12 (SCHEMA N33), and the compiler knows none of
	// either set: it carries whatever a Connection declares through verbatim.
	// Keeping this fixture on the old names is what proves a package written
	// before that change still compiles and dials unchanged.
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
	artifact.Telephony = generate.TelephonyRuntimePlanFor(agent.Targets["livekit"])
	return artifact
}

// enableSmokeTelephony puts an inbound phone channel on the package. The
// smokeConnectionRoute declares a route in a connection, which is where a route
// lives: a target names one connection and says nothing else about how a call
// reaches it (spec FR-001).
func smokeConnectionRoute(pkg *spec.Package, connection, transport, carrier string) {
	conn := pkg.Connections[connection]
	conn.Transport, conn.Carrier = transport, carrier
	pkg.Connections[connection] = conn
}

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
// transfer primitive. It removes the transfer completely: the control, every
// attachment, and the destination it resolved to. All three have to go — a
// dangling reference was always an error, and a destination nothing resolves to
// is one now, because its environment name still reached .env.example and the
// generated startup check for a control nothing could call.
func dropHumanTransfers(pkg *spec.Package) {
	for name, control := range pkg.Agent.Controls {
		if control.Kind != "human_transfer" {
			continue
		}
		delete(pkg.Agent.Controls, name)
		delete(pkg.Agent.Destinations, control.TransferDestination())
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
// placeholderArtifactEnvironment gives every name the project requires a
// placeholder value.
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
