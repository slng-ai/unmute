package generate

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/target"
)

// TestTelephonyRouteEmitterAgreement enforces SPEC V6 over the exact route
// table. Provisional features count as granted emitter obligations: promotion
// must never reveal that a listed route feature had no code path.
func TestTelephonyRouteEmitterAgreement(t *testing.T) {
	routes := target.TelephonyRoutes()
	keys := slices.Collect(maps.Keys(routes))
	slices.SortFunc(keys, func(a, b target.TelephonyKey) int {
		return strings.Compare(
			fmt.Sprintf("%s/%s/%s", a.Provider, a.Transport, a.Carrier),
			fmt.Sprintf("%s/%s/%s", b.Provider, b.Transport, b.Carrier),
		)
	})
	for _, key := range keys {
		route := routes[key]
		emitted := emittedTelephonyFeatures(key)
		features := maps.Clone(emitted)
		for feature := range route.Features {
			features[feature] = true
		}
		for _, feature := range slices.Sorted(maps.Keys(features)) {
			evidence, listed := route.Features[feature]
			granted := listed && evidence.Tag != target.Gated
			if emitted[feature] != granted {
				t.Errorf("route %s/%s/%s feature %q: emitter emits=%v, table grants=%v",
					key.Provider, key.Transport, key.Carrier, feature, emitted[feature], granted)
			}
			if granted && (evidence.Docs == "" || evidence.Verified == "") {
				t.Errorf("route %s/%s/%s feature %q lacks docs or verification date",
					key.Provider, key.Transport, key.Carrier, feature)
			}
		}
	}
}

func emittedTelephonyFeatures(key target.TelephonyKey) map[target.TelephonyFeature]bool {
	switch {
	case key.Provider == target.Pipecat && key.Transport == "carrier-websocket":
		if slices.Contains([]string{"twilio", "telnyx", "plivo"}, key.Carrier) {
			return pipecatEmittedTelephonyFeatures
		}
	case key.Provider == target.Pipecat && key.Transport == "daily-sip":
		if key.Carrier == "twilio" {
			return pipecatDailyCarrierEmittedTelephonyFeatures
		}
	case key.Provider == target.Pipecat && key.Transport == "cloud-websocket":
		if key.Carrier == "twilio" {
			return pipecatCloudWebsocketEmittedTelephonyFeatures
		}
	case key.Provider == target.Pipecat && key.Transport == "sip":
		if slices.Contains([]string{"twilio", "telnyx", "plivo"}, key.Carrier) {
			return pipecatSIPEmittedTelephonyFeatures
		}
	case key.Provider == target.LiveKit && key.Transport == "sip":
		if slices.Contains([]string{"twilio", "telnyx", "plivo"}, key.Carrier) {
			return livekitEmittedTelephonyFeatures
		}
	case key.Provider == target.LiveKit && key.Transport == "connector":
		if key.Carrier == "twilio" {
			return livekitConnectorEmittedTelephonyFeatures
		}
	}
	return map[target.TelephonyFeature]bool{}
}

// The local telephony stack is purely open source (SPEC V8, B3): the
// coordination store ships as Valkey (BSD-3-Clause), never a Redis image,
// because Redis images are source-available (RSALv2/SSPLv1) since 7.4. The
// service name and REDIS_URL keep the protocol's name on purpose.
func TestV8TelephonyComposeShipsOSILicensedCoordinationStore(t *testing.T) {
	for _, golden := range []string{
		"testdata/golden/livekit_v1_telephony_compose.yaml",
		"testdata/golden/pipecat_v1_telephony_compose.yaml",
	} {
		raw, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		content := string(raw)
		if strings.Contains(content, "image: redis:") {
			t.Errorf("%s pins a source-available Redis image", golden)
		}
		if !strings.Contains(content, "image: valkey/valkey:") {
			t.Errorf("%s does not pin the Valkey coordination store", golden)
		}
	}
}

// A row that declares no process must produce a build with no process artifact.
//
// The emitter agreement above proves every granted feature has code behind it,
// which is a claim about features. This is the other half for the Pipecat Cloud
// websocket route, whose whole point is a *shape*: the operator hosts nothing, so
// the build must carry nothing for them to run. A route row can promise that and
// an emitter can quietly break it, so both ends are asserted.
func TestRouteWithNoProcessesEmitsNoProcessArtifact(t *testing.T) {
	routes := target.TelephonyRoutes()
	key := target.TelephonyKey{Provider: target.Pipecat, Transport: "cloud-websocket", Carrier: "twilio"}
	if route := routes[key]; len(route.Processes) != 0 || len(route.PublicEndpoints) != 0 {
		t.Fatalf("route %v now declares a process or an endpoint; this test no longer describes it", key)
	}
	artifact := compileCloudWebsocketExample(t)
	// Every artifact any route emits for the operator to run, or for a runtime to
	// run on their behalf. None of them may appear here.
	for _, path := range []string{
		"telephony.py", "telephony_helper.py", "telephony_shared.py", "telephony_state.py",
		"compose.telephony.yaml", "telephony_bridge.py", "telephony-setup.sh",
	} {
		if hasArtifactFile(artifact, path) {
			t.Errorf("build emits %s; this route declares no process, so there is nothing for the operator to run", path)
		}
	}
}

// Gate C5 (contracts/local-planes.md). Every route's dictated carrier steps
// survive into the emitted runbook, verbatim.
//
// Verbatim is the point, and it is why this gate is worth having. internal/target
// owns a route's manual steps, so a runbook that paraphrases them has made a
// second copy of a fact with one owner, and the copy is what goes stale. Before
// this gate the LiveKit runbook paraphrased its steps in hand-written prose and
// the Pipecat carrier-websocket runbook carried no carrier section at all, so on
// that route the dictated steps reached the operator nowhere.
//
// This matters most for the carrier-free local loop. A local run that completes
// without a carrier is exactly the run that could read as proof that go-live is
// configured, and these steps are the standing correction. Pairs with gate P5,
// which covers the printed output of a run.
func TestTelephonyManualStepsSurviveIntoTheRunbook(t *testing.T) {
	routes := target.SelectableTelephonyRoutes()
	seen := 0
	for _, key := range sortedRouteKeys(routes) {
		route := routes[key]
		if len(route.ManualSteps) == 0 {
			t.Errorf("route %+v declares no manual step: going live needs real carrier "+
				"configuration on every route, so every route dictates something", key)
			continue
		}
		artifact, err := telephonyRouteArtifact(t, key)
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		readme := artifactFile(t, artifact, "README.md")
		seen++
		for _, step := range route.ManualSteps {
			if !strings.Contains(readme, step) {
				t.Errorf("route %+v: the runbook does not carry this dictated step verbatim.\n"+
					"  step: %s\n"+
					"  internal/target owns this text; the runbook must render it rather "+
					"than restate it, or the two drift and the operator reads the stale one.",
					key, step)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no route inspected: this gate is asserting nothing")
	}
}
