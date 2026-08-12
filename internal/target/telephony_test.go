package target

import (
	"slices"
	"strings"
	"testing"
)

// The auto-webhook fact is carrier data backed by a CLI implementation. The
// only implementation is Twilio (internal/cli/dev_twilio.go), so exactly the
// Twilio routes carry it (Pipecat carrier-websocket and the LiveKit connector),
// each naming a declared public endpoint. A fact on any non-Twilio route means
// someone added data without a CLI implementation (SPEC V3, C5).
func TestTelephonyAutoWebhookIsATwilioFactOnly(t *testing.T) {
	routes := TelephonyRoutes()
	twilioRoutes := map[TelephonyKey]bool{
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "twilio"}: true,
		{Provider: LiveKit, Transport: "connector", Carrier: "twilio"}:         true,
	}
	for key, route := range routes {
		if route.AutoWebhookEndpoint == "" {
			continue
		}
		if !twilioRoutes[key] {
			t.Fatalf("route %v carries auto-webhook fact %q without a CLI implementation", key, route.AutoWebhookEndpoint)
		}
		if route.AutoWebhookEndpoint != "inbound" {
			t.Fatalf("route %v auto-webhook endpoint = %q, want inbound", key, route.AutoWebhookEndpoint)
		}
		if !slices.ContainsFunc(route.PublicEndpoints, func(rule TelephonyEndpointRule) bool {
			return rule.Name == route.AutoWebhookEndpoint
		}) {
			t.Fatalf("route %v auto-webhook fact names no declared endpoint: %#v", key, route.PublicEndpoints)
		}
	}
	for key := range twilioRoutes {
		if routes[key].AutoWebhookEndpoint != "inbound" {
			t.Fatalf("Twilio route %v must carry the auto-webhook fact", key)
		}
	}
}

// The dev command supplies the LiveKit SIP trunk ID itself for local runs
// (compiler.md V36): every SIP route declares exactly that one name, backed by
// a runtime environment rule, and no other route declares any.
//
// Inbound only, since 2026-08-12 (SCHEMA N33). Dialling out carries the
// carrier's trunk settings inline, so there is no outbound trunk to register
// locally or in a deployment, and local now uses the same mechanism production
// does. Inbound cannot work that way: an unsolicited call arrives with no
// request of ours for configuration to travel with.
func TestTelephonyDevSuppliedEnvironmentIsSIPTrunkIDs(t *testing.T) {
	for key, route := range TelephonyRoutes() {
		if key.Provider == LiveKit && key.Transport == "sip" && len(route.Features) > 0 {
			if got := strings.Join(route.DevSuppliedEnvironment, ","); got != "LIVEKIT_SIP_INBOUND_TRUNK" {
				t.Fatalf("route %v dev-supplied environment = %q", key, got)
			}
			for _, name := range route.DevSuppliedEnvironment {
				if !slices.ContainsFunc(route.RuntimeEnvironment, func(rule TelephonyEnvironmentRule) bool {
					return rule.Name == name
				}) {
					t.Fatalf("route %v dev-supplied %s has no runtime environment rule", key, name)
				}
			}
			continue
		}
		if len(route.DevSuppliedEnvironment) != 0 {
			t.Fatalf("route %v unexpectedly declares dev-supplied environment %v", key, route.DevSuppliedEnvironment)
		}
	}
}

// FR-016 draws a line the 2026-08-12 rename must not cross. The four SIP values
// are standard SIP trunk settings, so they lost their carrier prefix and the
// same emitted code dials through any carrier with them. A carrier's REST
// account credentials are genuinely that one carrier's, so they keep theirs.
// The route keys themselves never move: renaming one breaks a written package.
func TestTelephonyRouteEnvironmentKeysHoldTheRenameLine(t *testing.T) {
	routes := TelephonyRoutes()
	want := map[TelephonyKey][]string{
		{Provider: LiveKit, Transport: "sip", Carrier: "twilio"}:               {"sip_address", "sip_username", "sip_password", "from_number"},
		{Provider: LiveKit, Transport: "sip", Carrier: "telnyx"}:               {"sip_address", "sip_username", "sip_password", "from_number"},
		{Provider: LiveKit, Transport: "sip", Carrier: "plivo"}:                {"sip_address", "sip_username", "sip_password", "from_number"},
		{Provider: LiveKit, Transport: "connector", Carrier: "twilio"}:         {"account_sid", "auth_token", "from_number"},
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "twilio"}: {"account_sid", "auth_token", "from_number"},
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "telnyx"}: {"api_key", "public_key", "connection_id", "from_number"},
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "plivo"}:  {"auth_id", "auth_token", "from_number"},
	}
	for key, expected := range want {
		route, ok := routes[key]
		if !ok {
			t.Errorf("route %v is missing from the table", key)
			continue
		}
		if got := strings.Join(route.RequiredEnvironment, ","); got != strings.Join(expected, ",") {
			t.Errorf("route %v required environment = %q, want %q", key, got, strings.Join(expected, ","))
		}
	}
}
