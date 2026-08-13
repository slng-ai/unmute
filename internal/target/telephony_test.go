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

// The Daily carrier route's row, in full (SCHEMA N37). The key set is the one
// fact this route adds to the telephony vocabulary, and the two absences are as
// deliberate as the four presences.
func TestPipecatDailyCarrierRouteRow(t *testing.T) {
	key := TelephonyKey{Provider: Pipecat, Transport: "daily-sip", Carrier: "twilio"}
	route, ok := TelephonyRoutes()[key]
	if !ok {
		t.Fatal("the Daily carrier route has no row, so nothing on it can validate")
	}
	if got := strings.Join(route.RequiredEnvironment, ","); got != "account_sid,auth_token,sip_address,from_number" {
		t.Fatalf("required environment = %q, want account_sid,auth_token,sip_address,from_number in that order", got)
	}
	// Daily's dial-out carries no SIP credential auth on any documented surface,
	// so a key here would promise authentication nothing performs (research F3).
	for _, refused := range []string{"sip_username", "sip_password"} {
		if slices.Contains(route.RequiredEnvironment, refused) || slices.Contains(route.OptionalEnvironment, refused) {
			t.Errorf("route accepts %q; carrier termination on this route authenticates by IP allow-list", refused)
		}
	}
	// The CLI never writes a carrier webhook here: the helper's public URL is the
	// operator's to choose, so pointing the number at it is a dictated step.
	if route.AutoWebhookEndpoint != "" {
		t.Errorf("auto-webhook endpoint = %q, want none", route.AutoWebhookEndpoint)
	}
	// RuntimeEnvironment is what the *deployed agent* reads. No trunk ID, and no
	// Redis: this route keeps no shared control record.
	for _, rule := range route.RuntimeEnvironment {
		if rule.Name != "DAILY_API_KEY" {
			t.Errorf("runtime environment carries %q; only the deployed agent's own names belong here", rule.Name)
		}
	}
	if len(route.Processes) != 1 || route.Processes[0].Name != "telephony-helper" {
		t.Fatalf("processes = %#v, want the one operator-run helper", route.Processes)
	}
	if len(route.ManualSteps) == 0 {
		t.Error("the carrier steps are dictated, so the row must summarise them")
	}
}

// No route asks the operator for a trunk ID in either direction (SCHEMA N33 for
// outbound, N36 for inbound). Dialling out carries the carrier's trunk settings
// inline with every call. Inbound cannot work that way, because an unsolicited
// call arrives with no request of ours for configuration to travel with, so it
// keeps its two platform records, but the emitted telephony-setup.sh resolves
// them by phone number at provisioning time. An environment name that carried an
// ID is the thing this feature retired, so the table must never grow one back.
func TestTelephonyRuntimeEnvironmentCarriesNoTrunkIDs(t *testing.T) {
	for key, route := range TelephonyRoutes() {
		for _, rule := range route.RuntimeEnvironment {
			if strings.Contains(rule.Name, "TRUNK") {
				t.Errorf("route %v runtime environment carries the trunk ID %s", key, rule.Name)
			}
		}
		for _, name := range route.LocallySuppliedEnvironment {
			if strings.Contains(name, "TRUNK") {
				t.Errorf("route %v locally supplied environment carries the trunk ID %s", key, name)
			}
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
