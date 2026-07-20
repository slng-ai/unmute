package target

import "testing"

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
		{"DTMF carrier route", DTMFSend, LiveKit, "", "twilio", Core},
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

func TestMCPResolvesSDKLanguageFromTable(t *testing.T) {
	table := Default()
	if got := table.CapabilityForValue(FieldToolMCP, LiveKit, "go"); got.Tag != Gated {
		t.Fatalf("Go MCP capability = %#v", got)
	}
	if got := table.CapabilityForValue(FieldToolMCP, LiveKit, "python"); got.Tag != Core {
		t.Fatalf("Python MCP capability = %#v", got)
	}
}
