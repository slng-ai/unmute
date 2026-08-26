package generate

import (
	"fmt"
	"maps"
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
	case key.Provider == target.Pipecat && key.Transport == "daily-sip":
		if key.Carrier == "twilio" {
			return pipecatDailyCarrierEmittedTelephonyFeatures
		}
	case key.Provider == target.Pipecat && key.Transport == "cloud-websocket":
		if key.Carrier == "twilio" {
			return pipecatCloudWebsocketEmittedTelephonyFeatures
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
