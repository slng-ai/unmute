package target

import (
	"strings"
	"testing"
)

// TestEveryListenTurnDetectorRowIsComplete holds the table the way pace_test
// holds the pace rows: a zero field here emits a service that cannot connect
// (no import), cannot take the ceiling (no Settings field) or refuses with an
// empty example, so every field carries a deliberate value.
func TestEveryListenTurnDetectorRowIsComplete(t *testing.T) {
	rows := ListenTurnDetectors(Pipecat)
	if len(rows) == 0 {
		t.Fatal("pipecat has no turn-detecting listeners; the listen decider would refuse everything")
	}
	catalog := DefaultCatalog()
	for vendor, row := range rows {
		if row.Vendor != vendor {
			t.Errorf("%s: row names vendor %q", vendor, row.Vendor)
		}
		for name, value := range map[string]string{
			"Class": row.Class, "Import": row.Import, "ExampleModel": row.ExampleModel,
			"CeilingArg": row.CeilingArg, "Verified": row.Verified,
		} {
			if value == "" {
				t.Errorf("%s: %s is empty", vendor, name)
			}
		}
		if len(row.ModelPrefixes) == 0 {
			t.Errorf("%s: no model family, so ServesModel refuses every model", vendor)
		}
		if !strings.Contains(row.Import, row.Class) {
			t.Errorf("%s: import %q does not provide %q", vendor, row.Import, row.Class)
		}
		if !row.ServesModel(row.ExampleModel) {
			t.Errorf("%s: the example model %q is refused by its own row", vendor, row.ExampleModel)
		}
		// The ordinary listen entry supplies the extra, the key and the language
		// slot, so a vendor here has to be one the catalogue already lists.
		if _, ok := catalog.Lookup(Pipecat, Listen, vendor); !ok {
			t.Errorf("%s decides turns but has no pipecat listen entry to take the key and the extra from", vendor)
		}
	}
	if got := ListenTurnDetectorVendors(Pipecat); strings.Join(got, ",") != "cartesia,deepgram" {
		t.Errorf("vendors = %v, want cartesia and deepgram sorted", got)
	}
}

// TestListenTurnDetectorServesItsFamilyOnly is the model-id check this table
// exists for: a Nova model on the Flux service is a connection Deepgram refuses,
// and ink-whisper is the ordinary Cartesia transcriber's model.
func TestListenTurnDetectorServesItsFamilyOnly(t *testing.T) {
	for _, tc := range []struct {
		vendor, model string
		want          bool
	}{
		{"deepgram", "flux-general-en", true},
		{"deepgram", "flux-general-multi", true},
		{"deepgram", "nova-3", false},
		{"deepgram", "", false},
		{"cartesia", "ink-2", true},
		{"cartesia", "ink-whisper", false},
		{"cartesia", "sonic-3", false},
	} {
		row, ok := LookupListenTurnDetector(Pipecat, tc.vendor)
		if !ok {
			t.Fatalf("%s has no row", tc.vendor)
		}
		if got := row.ServesModel(tc.model); got != tc.want {
			t.Errorf("%s serves %q = %v, want %v", tc.vendor, tc.model, got, tc.want)
		}
	}
	if _, ok := LookupListenTurnDetector(Pipecat, "assemblyai"); ok {
		t.Error("assemblyai detects turns but cannot predict one early; it has no row on purpose")
	}
	if _, ok := LookupListenTurnDetector(LiveKit, "deepgram"); ok {
		t.Error("livekit lets no transcriber decide the turn; its turn model runs beside the transcriber")
	}
}
