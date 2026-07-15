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
	if table.Role(Listen, ElevenLabs) != Integrated {
		t.Fatal("ElevenLabs listen role must be integrated")
	}
	if got := table.HistorySupport(HistoryMessages, ElevenLabs); got.Kind != HistoryFail || got.Note == "" {
		t.Fatalf("ElevenLabs messages history = %#v", got)
	}
	for _, provider := range Providers {
		if table.Capability(FieldFutureProvisional, provider).Tag != Provisional {
			t.Errorf("provisional field passed on %s", provider)
		}
	}
}
