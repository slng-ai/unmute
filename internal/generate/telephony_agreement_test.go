package generate

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/target"
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
			return pipecatEmittedTelephonyFeaturesFor(key.Carrier)
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

// TestTwilioTransferCarrierAgreement is connector-transfers V6: the Pipecat
// carrier-WebSocket route and the LiveKit connector route sit on the same
// Twilio product, so they must make the same carrier moves. A cold transfer is
// a REST redirect of the caller's call on both; a warm transfer creates a
// second streamed call on both. If these ever diverge it is a bug in one of
// them, not a design difference.
func TestTwilioTransferCarrierAgreement(t *testing.T) {
	pipecat := readTemplate(t, "pipecat_v1", "telephony_twilio.py.tmpl")
	livekit := readTemplate(t, "livekit_v1", "telephony_bridge.py.tmpl")
	for _, want := range []struct{ what, needle string }{
		{"cold redirects the caller's call", "_dial_twiml("},
		{"warm creates a second streamed call", "_stream_twiml("},
		{"the second call rides the outbound create path", "calls.create"},
	} {
		if !strings.Contains(pipecat, want.needle) {
			t.Errorf("pipecat twilio template lost %s (%q)", want.what, want.needle)
		}
		if !strings.Contains(livekit, want.needle) {
			t.Errorf("livekit connector bridge lost %s (%q)", want.what, want.needle)
		}
	}
	// Both must redirect via the call resource, never hang the caller up and
	// redial, which would drop the conversation.
	for name, tmpl := range map[string]string{"pipecat": pipecat, "livekit": livekit} {
		if !strings.Contains(tmpl, "twiml=_dial_twiml(") {
			t.Errorf("%s cold transfer must update the live call with dial TwiML", name)
		}
	}
}

func readTemplate(t *testing.T, driver, name string) string {
	t.Helper()
	body, err := os.ReadFile("templates/" + driver + "/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
