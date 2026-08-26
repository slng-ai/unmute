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
		"plain Pipecat Cloud": plainPipecatArtifact(t),
		"Daily with carrier":  dailyCarrierArtifact(t, "twilio", true),
		"cloud-websocket":     cloudWebsocketArtifact(t, cloudWebsocketOptions{inbound: true}),
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

// Gate C2. Every telephony route deploys to a managed platform now, so this no
// longer follows a per-route field: every Pipecat route emits the manifest, and
// no LiveKit route does.
//
// Still scoped by provider, and that is the load-bearing half. pcc-deploy.toml is
// a Pipecat artifact: an unscoped "every route that deploys emits a manifest"
// would demand a Pipecat manifest from LiveKit and fail on a working example. So
// the LiveKit half is asserted in the opposite direction.
func TestCloudIsolationManifestIsEmittedByEveryPipecatRouteAndNoLiveKitRoute(t *testing.T) {
	routes := target.SelectableTelephonyRoutes()
	pipecatSeen, livekitSeen := 0, 0
	for _, key := range sortedRouteKeys(routes) {
		artifact, err := telephonyRouteArtifact(t, key)
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		emitted := slices.Contains(artifactPaths(artifact), "pcc-deploy.toml")
		switch key.Provider {
		case target.Pipecat:
			pipecatSeen++
			if !emitted {
				t.Errorf("Pipecat route %+v emits no pcc-deploy.toml. Every surviving "+
					"telephony route deploys to a managed platform, so a Pipecat route "+
					"without its deployment manifest cannot be deployed at all", key)
			}
		case target.LiveKit:
			livekitSeen++
			if emitted {
				t.Errorf("LiveKit route %+v emits pcc-deploy.toml, which is a Pipecat "+
					"artifact. Every LiveKit route deploys, and still none emits this "+
					"manifest", key)
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
func TestCloudIsolationEveryPipecatRouteBuildsOnThePlatformBaseImage(t *testing.T) {
	const platformBase = "dailyco/pipecat-base"
	routes := target.SelectableTelephonyRoutes()
	seen := 0
	for _, key := range sortedRouteKeys(routes) {
		if key.Provider != target.Pipecat {
			continue
		}
		artifact, err := telephonyRouteArtifact(t, key)
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		seen++
		dockerfile := artifactFile(t, artifact, "Dockerfile")
		// The self-hosted-on-plain-Python half of this gate went with the two
		// routes that deployed nowhere. Every Pipecat route left builds for the
		// platform, so the assertion is now one-directional.
		if !strings.Contains(dockerfile, platformBase) {
			t.Errorf("Pipecat route %+v does not build on %s. Every surviving Pipecat "+
				"route deploys to Pipecat Cloud, which builds from the platform's own "+
				"base image:\n%s", key, platformBase, dockerfile)
		}
	}
	if seen == 0 {
		t.Fatal("no Pipecat route inspected: the filter is inverted")
	}
}
