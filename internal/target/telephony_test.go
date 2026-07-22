package target

import (
	"slices"
	"strings"
	"testing"
)

// The auto-webhook fact is carrier data: exactly one route carries it in v1
// (Pipecat carrier-websocket with Twilio) and it must name a declared public
// endpoint. A fact on any other route means someone added data without a CLI
// implementation (SPEC V3, C5).
func TestTelephonyAutoWebhookIsATwilioPipecatFactOnly(t *testing.T) {
	routes := TelephonyRoutes()
	twilio := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "twilio"}
	for key, route := range routes {
		if key == twilio {
			continue
		}
		if route.AutoWebhookEndpoint != "" {
			t.Fatalf("route %v carries auto-webhook fact %q without a CLI implementation", key, route.AutoWebhookEndpoint)
		}
	}
	route := routes[twilio]
	if route.AutoWebhookEndpoint != "inbound" {
		t.Fatalf("Twilio auto-webhook endpoint = %q, want inbound", route.AutoWebhookEndpoint)
	}
	if !slices.ContainsFunc(route.PublicEndpoints, func(rule TelephonyEndpointRule) bool {
		return rule.Name == route.AutoWebhookEndpoint
	}) {
		t.Fatalf("auto-webhook fact names no declared endpoint: %#v", route.PublicEndpoints)
	}
}

// The dev command supplies the LiveKit SIP trunk IDs itself for local runs
// (SPEC V4): every SIP route declares exactly those two names, each backed by
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
