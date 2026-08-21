package generate

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The runbook and the artifact set have to agree about where a build deploys.
// `pcc-deploy.toml` is emitted exactly for the routes with a managed-platform
// path, so a README that teaches `pipecat cloud deploy` without one sends the
// reader to a manifest that was never written. That is what shipped: every
// (pipecat, carrier-websocket, *) build carried the whole **Deploy to Pipecat
// Cloud** section, manifest sentence included, while emitting no manifest and
// building from a plain Python base image.
func TestPipecatReadmeTeachesCloudDeployExactlyWhenTheManifestIsEmitted(t *testing.T) {
	for name, artifact := range map[string]Artifact{
		"plain Pipecat Cloud":        plainPipecatArtifact(t),
		"Daily with carrier":         dailyCarrierArtifact(t, "twilio", true),
		"cloud-websocket":            cloudWebsocketArtifact(t, cloudWebsocketOptions{inbound: true}),
		"carrier-websocket (twilio)": carrierWebsocketArtifact(t, "twilio"),
		"carrier-websocket (telnyx)": carrierWebsocketArtifact(t, "telnyx"),
		"carrier-websocket (plivo)":  carrierWebsocketArtifact(t, "plivo"),
	} {
		manifest := slices.Contains(artifactPaths(artifact), "pcc-deploy.toml")
		readme := artifactFile(t, artifact, "README.md")
		switch teaches := strings.Contains(readme, "pipecat cloud deploy"); {
		case manifest && !teaches:
			t.Errorf("%s emits pcc-deploy.toml but its README never says how to deploy it", name)
		case !manifest && teaches:
			t.Errorf("%s emits no pcc-deploy.toml but its README teaches `pipecat cloud deploy`", name)
		}
		// The self-hosted route still has to say where it runs, or the runbook
		// simply stops after the environment list.
		if !manifest && !strings.Contains(readme, "## Deploy it yourself") {
			t.Errorf("%s has no managed-platform path and no self-hosted deploy section", name)
		}
	}
}

// Cloud go-live isolation, gates C2 to C4 of contracts/local-planes.md.
//
// Going live on a managed platform always requires real carrier configuration,
// and nothing in the carrier-free local telephony work may soften that. These
// tests land before any emission change so they catch a regression the moment it
// appears rather than after it ships.
//
// Why this file is in package generate and not in package target: it asserts
// what is *emitted*, and internal/generate imports internal/target. A test in
// package target that reached for the generator would be an import cycle.
//
// Why every table reads SelectableTelephonyRoutes(): the two Exotel rows in
// TelephonyRoutes() carry empty feature maps that ResolveTelephonyFeature
// refuses, so they compile to nothing and there is no artifact to assert on.

// planeEnvironmentPrefix is the reserved prefix for an environment name that
// exists only for a local plane.
//
// A reserved prefix rather than a hand-maintained list of names, because gate C4
// has to keep working as the planes grow. A list would need editing every time a
// plane learns a new name, and the edit nobody makes is exactly how a plane-only
// name reaches a deployment manifest. Every plane-only name starts with this.
const planeEnvironmentPrefix = "UNMUTE_PLANE_"

// telephonyRouteArtifact compiles a package on one route.
//
// Deliberately generic: the environment map is derived from the route's own
// RequiredEnvironment, and the only control asked for is hangup, which every
// selectable route grants. So a new route joins these gates without this helper
// being touched.
func telephonyRouteArtifact(t *testing.T, key target.TelephonyKey) (Artifact, error) {
	t.Helper()
	route := target.TelephonyRoutes()[key]

	fixture := "safe_core"
	if key.Transport == "daily-sip" {
		// This route's package needs the platform key the helper reads, which
		// safe_core does not declare.
		fixture = "daily_carrier"
	}
	pkg, err := spec.Load(filepath.Join("..", "testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}

	instance := "pipecat"
	if key.Provider == target.LiveKit {
		instance = "livekit"
	}
	configured, ok := pkg.Targets[instance]
	if !ok {
		t.Fatalf("fixture %s has no %s target", fixture, instance)
	}

	// The directions the route under test actually supports, read from its own
	// row. Hardcoding both was fine while every route dialled out; it stopped
	// being fine when `(pipecat, sip)` dropped the outbound claim it had no code
	// for, and a fixture that asks for a capability the route refuses tests the
	// refusal instead of the route.
	inbound := route.Features[target.TelephonyInbound].Feature != ""
	outbound := route.Features[target.TelephonyOutbound].Feature != ""
	pkg.Agent.Channels["phone"] = spec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
		RequiredControls: []string{"hangup"},
	}

	environment := make(map[string]string, len(route.RequiredEnvironment))
	for _, name := range route.RequiredEnvironment {
		environment[name] = "UNMUTE_TEST_" + strings.ToUpper(name)
	}
	pkg.Connections = map[string]spec.Connection{"route_under_test": {
		Transport: key.Transport, Carrier: key.Carrier, Environment: environment,
	}}
	configured.Connection = "route_under_test"
	pkg.Targets = map[string]spec.Target{instance: configured}

	agent, err := ir.Build(pkg)
	if err != nil {
		return Artifact{}, fmt.Errorf("build %+v: %w", key, err)
	}
	artifact, err := Generate(agent, agent.Targets[instance], target.Default())
	if err != nil {
		return Artifact{}, fmt.Errorf("generate %+v: %w", key, err)
	}
	return artifact, nil
}

// sortedRouteKeys keeps failures in a stable order.
func sortedRouteKeys(routes map[target.TelephonyKey]target.TelephonyRoute) []target.TelephonyKey {
	keys := slices.Collect(maps.Keys(routes))
	slices.SortFunc(keys, func(a, b target.TelephonyKey) int {
		return strings.Compare(
			fmt.Sprintf("%s/%s/%s", a.Provider, a.Transport, a.Carrier),
			fmt.Sprintf("%s/%s/%s", b.Provider, b.Transport, b.Carrier),
		)
	})
	return keys
}

// Gate C2. Two scoping decisions, both load-bearing.
//
// Scoped to Pipecat because pcc-deploy.toml is a Pipecat artifact. CloudDeploys
// is true on every LiveKit route, so an unscoped predicate would demand a
// Pipecat manifest from LiveKit and fail on a working example. The LiveKit half
// is asserted separately and in the opposite direction: no LiveKit route emits
// that manifest at all.
func TestCloudIsolationManifestFollowsCloudDeploys(t *testing.T) {
	routes := target.SelectableTelephonyRoutes()
	pipecatSeen, livekitSeen := 0, 0
	for _, key := range sortedRouteKeys(routes) {
		route := routes[key]
		artifact, err := telephonyRouteArtifact(t, key)
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		emitted := slices.Contains(artifactPaths(artifact), "pcc-deploy.toml")
		switch key.Provider {
		case target.Pipecat:
			pipecatSeen++
			if emitted != route.CloudDeploys {
				t.Errorf("route %+v emits pcc-deploy.toml = %v but CloudDeploys = %v: "+
					"the deployment manifest must be emitted exactly when the route has "+
					"a managed-platform deployment path", key, emitted, route.CloudDeploys)
			}
		case target.LiveKit:
			livekitSeen++
			if emitted {
				t.Errorf("LiveKit route %+v emits pcc-deploy.toml, which is a Pipecat "+
					"artifact. CloudDeploys is true on every LiveKit route and still no "+
					"LiveKit route emits this manifest", key)
			}
		}
	}
	if pipecatSeen == 0 || livekitSeen == 0 {
		t.Fatalf("covered %d Pipecat and %d LiveKit routes: this gate must see both providers",
			pipecatSeen, livekitSeen)
	}
}

// Gate C3. Scoped to Pipecat for the same reason as C2: the managed platform's
// base image is a Pipecat artifact. LiveKit emits its own container definition
// and is held by C2's no-Pipecat-manifest half.
func TestCloudIsolationBaseImageFollowsCloudDeploys(t *testing.T) {
	const platformBase = "dailyco/pipecat-base"
	routes := target.SelectableTelephonyRoutes()
	seen := 0
	for _, key := range sortedRouteKeys(routes) {
		if key.Provider != target.Pipecat {
			continue
		}
		route := routes[key]
		artifact, err := telephonyRouteArtifact(t, key)
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		seen++
		dockerfile := artifactFile(t, artifact, "Dockerfile")
		usesPlatformBase := strings.Contains(dockerfile, platformBase)
		if usesPlatformBase != route.CloudDeploys {
			t.Errorf("route %+v uses %s = %v but CloudDeploys = %v: a route with a "+
				"managed-platform deployment path builds on the platform's own base "+
				"image, and a self-hosted route builds on a plain Python image",
				key, platformBase, usesPlatformBase, route.CloudDeploys)
		}
		if !route.CloudDeploys && !strings.Contains(dockerfile, "FROM python:") {
			t.Errorf("self-hosted route %+v does not build on a plain Python image:\n%s",
				key, dockerfile)
		}
	}
	if seen == 0 {
		t.Fatal("no Pipecat route inspected: the filter is inverted")
	}
}

// Gate C4. A name that exists only so a local plane can run has no business in a
// deployment manifest or in the environment file an author fills in for
// production. Reads the reserved prefix rather than a list of names, so a plane
// that grows a new name is covered without this test being edited.
func TestCloudIsolationNoPlaneEnvironmentReachesManagedPlatformArtifacts(t *testing.T) {
	routes := target.SelectableTelephonyRoutes()
	seen := 0
	for _, key := range sortedRouteKeys(routes) {
		route := routes[key]
		if key.Provider != target.Pipecat || !route.CloudDeploys {
			continue
		}
		artifact, err := telephonyRouteArtifact(t, key)
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		seen++
		for _, path := range []string{"pcc-deploy.toml", ".env.example"} {
			if !slices.Contains(artifactPaths(artifact), path) {
				t.Errorf("route %+v emits no %s, so this gate is inspecting nothing", key, path)
				continue
			}
			content := artifactFile(t, artifact, path)
			if strings.Contains(content, planeEnvironmentPrefix) {
				t.Errorf("route %+v: %s carries a plane-only environment name "+
					"(prefix %s). A local plane's names must never reach a managed-"+
					"platform artifact:\n%s", key, path, planeEnvironmentPrefix, content)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no Pipecat route with a managed-platform path inspected")
	}
}
