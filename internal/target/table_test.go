package target

import (
	"strings"
	"testing"
)

func TestDefaultTableIsCompleteAndTyped(t *testing.T) {
	table := Default()
	for field, providers := range table.Fields {
		for _, provider := range Providers {
			if providers[provider].Tag == "" {
				t.Errorf("%s missing %s tag", field, provider)
			}
		}
	}
	for control, providers := range table.Controls {
		for _, provider := range Providers {
			if providers[provider].Tag == "" {
				t.Errorf("%s missing %s tag", control, provider)
			}
		}
	}
	if table.Role(Turn, Vapi) != Integrated {
		t.Fatal("Vapi turn role must be integrated")
	}
	if err := DefaultCatalog().CheckVendor(LiveKit, Speak, "elevenlabs", false); err != nil {
		t.Fatalf("livekit speak vendor elevenlabs must validate: %v", err)
	}
	if got := table.HistorySupport(HistorySummary, Vapi); got.Kind != HistoryFail || got.Note == "" {
		t.Fatalf("Vapi summary history = %#v", got)
	}
	for _, provider := range Providers {
		if table.Capability(FieldFutureProvisional, provider).Tag != Provisional {
			t.Errorf("provisional field passed on %s", provider)
		}
	}
	if table.Capability(FieldInactivity, LiveKit).Tag != Warn || table.Capability(FieldMaxDuration, Deepgram).Tag != Warn {
		t.Fatal("warn-tagged lifecycle fields must produce target warnings")
	}
	if table.Capability(FieldTracingLangfuse, LiveKit).Tag != Core || table.Capability(FieldTracingLangfuse, Pipecat).Tag != Core {
		t.Fatal("Langfuse tracing must pass on code drivers")
	}
	for _, provider := range []Provider{Vapi, Deepgram} {
		if table.Capability(FieldTracingLangfuse, provider).Tag != Gated {
			t.Errorf("Langfuse tracing passed on %s", provider)
		}
	}
}

func TestBuiltinToolsPassOnCodeDriversOnly(t *testing.T) {
	table := Default()
	if table.Capability(FieldToolBuiltin, LiveKit).Tag != Core || table.Capability(FieldToolBuiltin, Pipecat).Tag != Core {
		t.Fatal("builtin tools must pass on LiveKit and Pipecat")
	}
	for _, provider := range []Provider{Vapi, Deepgram} {
		if table.Capability(FieldToolBuiltin, provider).Tag != Gated {
			t.Errorf("builtin tools passed on %s", provider)
		}
	}
}

func TestTelephonyControlsResolveCarrierAndTransport(t *testing.T) {
	table := Default()
	tests := []struct {
		name      string
		control   TelephonyControl
		provider  Provider
		transport string
		carrier   string
		want      Tag
	}{
		{"pipecat cold missing Daily", ColdTransfer, Pipecat, "twilio", "", Gated},
		{"pipecat cold Daily", ColdTransfer, Pipecat, "daily-sip", "", Core},
		{"vapi warm wrong carrier", WarmTransfer, Vapi, "", "telnyx", Gated},
		{"vapi warm Twilio", WarmTransfer, Vapi, "", "twilio", Core},
		{"DTMF missing route", DTMFSend, LiveKit, "", "", Gated},
		{"DTMF carrier only", DTMFSend, LiveKit, "", "twilio", Gated},
		{"DTMF exact route", DTMFSend, LiveKit, "daily-sip", "twilio", Core},
		{"DTMF unknown carrier", DTMFSend, LiveKit, "", "made-up", Gated},
		{"Deepgram voicemail missing carrier", VoicemailDetection, Deepgram, "", "", Gated},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := table.Control(test.control, test.provider, test.transport, test.carrier); got.Tag != test.want {
				t.Fatalf("capability = %#v", got)
			}
		})
	}
}

func TestTelephonyControlRequiresExactCarrierAndTransport(t *testing.T) {
	table := Table{Controls: map[TelephonyControl]map[Provider]ControlCapability{
		ColdTransfer: {
			Pipecat: {
				Capability: Capability{Tag: Core}, Carrier: "twilio", Transport: "carrier-websocket",
				ConditionNote: "exact route required",
			},
		},
	}}
	for _, route := range []struct{ transport, carrier string }{
		{"carrier-websocket", "telnyx"},
		{"sip", "twilio"},
	} {
		if got := table.Control(ColdTransfer, Pipecat, route.transport, route.carrier); got.Tag != Gated {
			t.Fatalf("partial route match passed: %#v", got)
		}
	}
}

func TestTelephonyRouteEvidenceIsExactAndProvisionalWithoutSmoke(t *testing.T) {
	exact := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "twilio"}
	if got := ResolveTelephonyFeature(exact, TelephonyInbound); got.Tag != Provisional || got.Docs == "" || got.Verified == "" || got.Smoke {
		t.Fatalf("exact route evidence = %#v", got)
	}
	for _, key := range []TelephonyKey{
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "telnyx"},
		{Provider: Pipecat, Transport: "sip", Carrier: "twilio"},
	} {
		if got := ResolveTelephonyFeature(key, TelephonyFeature(WarmTransfer)); got.Tag != Gated {
			t.Fatalf("partial or unsupported route passed: %#v", got)
		}
	}
	// The LiveKit Twilio connector is a usable route (its own open-source
	// bridge): inbound, outbound, and hangup are provisional like the other
	// live routes; transfers and voicemail stay gated. A transfer needs a
	// platform primitive and this route has none (SPEC C1, V1).
	connector := TelephonyKey{Provider: LiveKit, Transport: "connector", Carrier: "twilio"}
	for _, feature := range []TelephonyFeature{TelephonyRouteSelected, TelephonyInbound, TelephonyOutbound, TelephonyFeature(Hangup)} {
		if got := ResolveTelephonyFeature(connector, feature); got.Tag != Provisional {
			t.Fatalf("connector feature %s = %#v, want provisional", feature, got)
		}
	}
	for _, feature := range []TelephonyFeature{TelephonyFeature(WarmTransfer), TelephonyFeature(ColdTransfer), TelephonyFeature(VoicemailDetection)} {
		if got := ResolveTelephonyFeature(connector, feature); got.Tag != Gated {
			t.Fatalf("connector unsupported feature %s = %#v, want gated", feature, got)
		}
	}
	required, optional, ok := TelephonyEnvironment(connector)
	if !ok || len(optional) != 0 || strings.Join(required, ",") != "account_sid,auth_token,from_number" {
		t.Fatalf("connector environment vocabulary = required %v optional %v ok %v", required, optional, ok)
	}
	if route := TelephonyRoutes()[connector]; len(route.Processes) != 1 || len(route.PublicEndpoints) != 4 || route.AutoWebhookEndpoint != "inbound" {
		t.Fatalf("connector must advertise runtime facts: %#v", route)
	}
	required, optional, ok = TelephonyEnvironment(exact)
	if !ok || len(optional) != 0 || strings.Join(required, ",") != "account_sid,auth_token,from_number" {
		t.Fatalf("exact environment vocabulary = required %v optional %v ok %v", required, optional, ok)
	}
	runtime := TelephonyRoutes()[exact]
	if len(runtime.Processes) != 1 || len(runtime.PublicEndpoints) != 4 || len(runtime.ManualSteps) == 0 {
		t.Fatalf("Pipecat route runtime facts = %#v", runtime)
	}
	if strings.Join(runtime.LocallySuppliedEnvironment, ",") != "REDIS_URL" {
		t.Fatalf("Pipecat locally supplied environment = %v", runtime.LocallySuppliedEnvironment)
	}
	telnyx := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "telnyx"}
	required, optional, ok = TelephonyEnvironment(telnyx)
	if !ok || len(optional) != 0 || strings.Join(required, ",") != "api_key,public_key,connection_id,from_number" {
		t.Fatalf("Telnyx environment vocabulary = required %v optional %v ok %v", required, optional, ok)
	}
	plivo := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "plivo"}
	required, optional, ok = TelephonyEnvironment(plivo)
	if !ok || len(optional) != 0 || strings.Join(required, ",") != "auth_id,auth_token,from_number" {
		t.Fatalf("Plivo environment vocabulary = required %v optional %v ok %v", required, optional, ok)
	}
	for _, carrier := range []string{"twilio", "telnyx", "plivo"} {
		livekitSIP := TelephonyKey{Provider: LiveKit, Transport: "sip", Carrier: carrier}
		required, optional, ok = TelephonyEnvironment(livekitSIP)
		if !ok || len(optional) != 0 || strings.Join(required, ",") != "sip_address,sip_username,sip_password,from_number" {
			t.Fatalf("LiveKit SIP %s environment vocabulary = required %v optional %v ok %v", carrier, required, optional, ok)
		}
		if got := ResolveTelephonyFeature(livekitSIP, TelephonyFeature(WarmTransfer)); got.Tag != Provisional || got.Smoke || !strings.Contains(got.Docs, carrier) {
			t.Fatalf("LiveKit SIP %s warm transfer evidence = %#v", carrier, got)
		}
	}
	exotel := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "exotel"}
	if got := ResolveTelephonyFeature(exotel, TelephonyRouteSelected); got.Tag != Gated || !strings.Contains(got.Note, "does not support route") {
		t.Fatalf("Exotel unauthenticated WebSocket route = %#v", got)
	}
	if got := ResolveTelephonyFeature(TelephonyKey{Provider: LiveKit, Transport: "sip", Carrier: "exotel"}, TelephonyRouteSelected); got.Tag != Gated {
		t.Fatalf("unproven Exotel SIP route = %#v", got)
	}
	// No transfers on the carrier-websocket routes, either shape: a transfer
	// needs a platform primitive and Pipecat's websocket transports have none
	// (SPEC C1, V1). Every carrier refuses both by name.
	for _, carrier := range []string{"twilio", "telnyx", "plivo"} {
		key := TelephonyKey{Provider: Pipecat, Transport: "carrier-websocket", Carrier: carrier}
		for _, feature := range []TelephonyFeature{TelephonyFeature(ColdTransfer), TelephonyFeature(WarmTransfer)} {
			if got := ResolveTelephonyFeature(key, feature); got.Tag != Gated {
				t.Fatalf("Pipecat %s %s = %#v, want gated", carrier, feature, got)
			}
		}
	}
}

func TestMCPResolvesSDKLanguageFromTable(t *testing.T) {
	table := Default()
	if got := table.CapabilityForValue(FieldToolMCP, LiveKit, "go"); got.Tag != Gated {
		t.Fatalf("Go MCP capability = %#v", got)
	}
	if got := table.CapabilityForValue(FieldToolMCP, LiveKit, "python"); got.Tag != Core {
		t.Fatalf("Python MCP capability = %#v", got)
	}
}

// TestV1_TransfersCompileOnlyOnNativeRoutes is SPEC V1: a transfer control
// compiles only where the platform documents the primitive. LiveKit SIP
// trunks grant both shapes; every other telephony route refuses both, and the
// refusal names the routes that work (SPEC C1). On the non-telephony side,
// the Pipecat control rows allow cold on the Daily transport alone and deny
// warm everywhere.
func TestV1_TransfersCompileOnlyOnNativeRoutes(t *testing.T) {
	cold, warm := TelephonyFeature(ColdTransfer), TelephonyFeature(WarmTransfer)
	for _, carrier := range []string{"twilio", "telnyx", "plivo"} {
		sip := TelephonyKey{Provider: LiveKit, Transport: "sip", Carrier: carrier}
		for _, feature := range []TelephonyFeature{cold, warm} {
			if got := ResolveTelephonyFeature(sip, feature); got.Tag != Provisional {
				t.Errorf("livekit sip %s %s = %#v, want provisional", carrier, feature, got)
			}
		}
	}
	noPrimitive := []TelephonyKey{
		{Provider: LiveKit, Transport: "connector", Carrier: "twilio"},
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "twilio"},
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "telnyx"},
		{Provider: Pipecat, Transport: "carrier-websocket", Carrier: "plivo"},
	}
	for _, key := range noPrimitive {
		coldGot := ResolveTelephonyFeature(key, cold)
		if coldGot.Tag != Gated || !strings.Contains(coldGot.Note, "(livekit, sip)") || !strings.Contains(coldGot.Note, "daily-sip") {
			t.Errorf("%v cold = %#v, want gated with the supported routes named", key, coldGot)
		}
		warmGot := ResolveTelephonyFeature(key, warm)
		if warmGot.Tag != Gated || !strings.Contains(warmGot.Note, "(livekit, sip) trunks") {
			t.Errorf("%v warm = %#v, want gated with the supported routes named", key, warmGot)
		}
	}
	table := Default()
	if got := table.Control(ColdTransfer, Pipecat, "daily-sip", ""); got.Tag != Core {
		t.Errorf("pipecat cold on daily-sip = %#v, want core", got)
	}
	if got := table.Control(ColdTransfer, Pipecat, "carrier-websocket", "twilio"); got.Tag != Gated {
		t.Errorf("pipecat cold off daily-sip = %#v, want gated", got)
	}
	for _, transport := range []string{"daily-sip", "carrier-websocket", ""} {
		if got := table.Control(WarmTransfer, Pipecat, transport, ""); got.Tag != Gated || !strings.Contains(got.Note, "(livekit, sip)") {
			t.Errorf("pipecat warm on %q = %#v, want gated naming (livekit, sip)", transport, got)
		}
	}
	if got := table.Capability(FieldTransferBriefing, Pipecat); got.Tag != Gated {
		t.Errorf("pipecat briefing field = %#v, want gated (briefing rides the warm row)", got)
	}
}

// The Daily route has no telephony plan (ir/build.go builds one only for a
// declared connection), so the prerequisite lookup has to work off the route
// triple alone. This is the seam the whole feature reads.
func TestRouteAccountPrerequisitesAreReachableWithoutAPlan(t *testing.T) {
	got := RouteAccountPrerequisites(Pipecat, "daily-sip", "")
	if len(got) != 1 || got[0].Name != "daily_dialout" {
		t.Fatalf("pipecat daily-sip prerequisites = %+v, want one daily_dialout", got)
	}
	if !got[0].Needs([]TelephonyFeature{TelephonyFeature(ColdTransfer)}) {
		t.Error("daily_dialout must be needed by cold_transfer: a cold transfer dials the destination")
	}
	if !got[0].Needs([]TelephonyFeature{TelephonyOutbound}) {
		t.Error("daily_dialout must be needed by outbound calling")
	}
	// The other half of the rule: one that always applies is a banner.
	if got[0].Needs([]TelephonyFeature{TelephonyInbound}) {
		t.Error("daily_dialout must not apply to an inbound-only package")
	}
	for _, carrier := range []string{"twilio", "telnyx", "plivo"} {
		if got := RouteAccountPrerequisites(Pipecat, "carrier-websocket", carrier); len(got) != 0 {
			t.Errorf("pipecat carrier-websocket %s prerequisites = %+v, want none", carrier, got)
		}
	}
	if got := RouteAccountPrerequisites(LiveKit, "sip", "twilio"); len(got) != 0 {
		t.Errorf("livekit sip prerequisites = %+v, want none", got)
	}
}

// Every prerequisite is recorded the way every other provider claim in this
// rulebook is: with the page it came from and the date it was checked. An empty
// NeededBy would be a prerequisite nothing needs, which must not exist.
func TestRouteAccountPrerequisitesAreEvidenced(t *testing.T) {
	table := Default()
	for _, rule := range routePrerequisites {
		p := rule.prereq
		if p.Name == "" || p.Summary == "" {
			t.Errorf("prerequisite %+v needs a name and an actionable summary", p)
		}
		if len(p.NeededBy) == 0 {
			t.Errorf("prerequisite %q has an empty NeededBy: a prerequisite nothing needs must not exist", p.Name)
		}
		if p.Docs == "" || p.Verified == "" {
			t.Errorf("prerequisite %q = docs %q verified %q, want both set", p.Name, p.Docs, p.Verified)
		}
		// A prerequisite for a capability the route refuses is a prerequisite for
		// something no package can reach, so it could never be reported. Checked
		// against the control rows rather than restated, so the rulebook stays the
		// one place the route's support is described.
		for _, feature := range p.NeededBy {
			control := TelephonyControl(feature)
			if _, isControl := table.Controls[control]; !isControl {
				continue
			}
			if got := table.Control(control, rule.provider, rule.transport, rule.carrier); got.Tag == Gated {
				t.Errorf("prerequisite %q is needed by %q, which (%s, %s) refuses: %s",
					p.Name, feature, rule.provider, rule.transport, got.Note)
			}
		}
	}
}
