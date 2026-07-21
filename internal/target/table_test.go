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
	if table.Role(Listen, ElevenLabs) != Integrated {
		t.Fatal("ElevenLabs listen role must be integrated")
	}
	if err := DefaultCatalog().CheckVendor(ElevenLabs, Speak, "elevenlabs", false); err != nil {
		t.Fatalf("ElevenLabs speak provider elevenlabs must validate: %v", err)
	}
	if got := table.HistorySupport(HistoryMessages, ElevenLabs); got.Kind != HistoryFail || got.Note == "" {
		t.Fatalf("ElevenLabs messages history = %#v", got)
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
	for _, provider := range []Provider{Vapi, ElevenLabs, Deepgram} {
		if table.Capability(FieldTracingLangfuse, provider).Tag != Gated {
			t.Errorf("Langfuse tracing passed on %s", provider)
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
		if got := ResolveTelephonyFeature(key, TelephonyBriefingMessage); got.Tag != Gated {
			t.Fatalf("partial or unsupported route passed: %#v", got)
		}
	}
	connector := TelephonyKey{Provider: LiveKit, Transport: "connector", Carrier: "twilio"}
	for _, feature := range []TelephonyFeature{TelephonyRouteSelected, TelephonyInbound, TelephonyOutbound, TelephonyFeature(Hangup), TelephonyFeature(WarmTransfer)} {
		got := ResolveTelephonyFeature(connector, feature)
		if got.Tag != Gated {
			t.Fatalf("connector route-recognition-only feature %s = %#v", feature, got)
		}
	}
	required, optional, ok := TelephonyEnvironment(connector)
	if !ok || len(optional) != 0 || strings.Join(required, ",") != "account_sid,auth_token,from_number" {
		t.Fatalf("connector environment vocabulary = required %v optional %v ok %v", required, optional, ok)
	}
	required, optional, ok = TelephonyEnvironment(exact)
	if !ok || len(optional) != 0 || strings.Join(required, ",") != "account_sid,auth_token,from_number" {
		t.Fatalf("exact environment vocabulary = required %v optional %v ok %v", required, optional, ok)
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
	for _, feature := range []TelephonyFeature{TelephonyFeature(WarmTransfer), TelephonyBriefingSummary} {
		if got := ResolveTelephonyFeature(exact, feature); got.Tag != Gated {
			t.Fatalf("Pipecat unemitted feature %s = %#v", feature, got)
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
