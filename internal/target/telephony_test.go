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

// The dev command supplies the LiveKit SIP trunk IDs itself for local runs
// (compiler.md V36): every SIP route declares exactly those two names, each backed by
// a runtime environment rule, and no other route declares any.
func TestTelephonyDevSuppliedEnvironmentIsSIPTrunkIDs(t *testing.T) {
	for key, route := range TelephonyRoutes() {
		if key.Provider == LiveKit && key.Transport == "sip" && len(route.Features) > 0 {
			if got := strings.Join(route.DevSuppliedEnvironment, ","); got != "LIVEKIT_SIP_INBOUND_TRUNK,LIVEKIT_SIP_OUTBOUND_TRUNK" {
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
