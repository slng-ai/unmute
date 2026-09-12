package target

import (
	"strings"
	"testing"
)

// TestEveryListenTurnDetectorRowIsComplete holds the table the way pace_test
// holds the pace rows: a zero field here emits a service that cannot connect (no
// import), cannot take the ceiling (no Settings field) or refuses with an empty
// example.
//
// What "complete" means now depends on the row's switch, which is the change
// spec 022 made. A class-swap row has to carry a class and an import; a row
// switched on by an argument or a setting has to carry that key and its value,
// and must NOT carry a class, because a half-filled row would emit both and the
// second would win silently. The fields that genuinely do not apply everywhere,
// the ceiling and the early prediction, are checked by their own refusals rather
// than required here.
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
			"ExampleModel": row.ExampleModel, "Verified": row.Verified,
		} {
			if value == "" {
				t.Errorf("%s: %s is empty", vendor, name)
			}
		}
		switch row.Switch {
		case SwitchClass:
			if row.Class == "" || row.Import == "" {
				t.Errorf("%s: a class switch with no class or import emits the ordinary transcriber and never decides a turn: %+v", vendor, row)
			}
			if row.EnableArg != "" || row.EnableValue != "" {
				t.Errorf("%s: a class switch also writes %s=%s; the class is the switch, so the argument reaches a constructor that has no such keyword", vendor, row.EnableArg, row.EnableValue)
			}
			if !strings.Contains(row.Import, row.Class) {
				t.Errorf("%s: import %q does not provide %q", vendor, row.Import, row.Class)
			}
			if len(row.ModelPrefixes) == 0 {
				t.Errorf("%s: a second class serves its own model family, and this row names none, so a model it refuses would reach it", vendor)
			}
		case SwitchArg, SwitchSetting:
			if row.EnableArg == "" || row.EnableValue == "" {
				t.Errorf("%s: a %s switch with no key or value emits the ordinary service with nothing asking it to decide: %+v", vendor, row.Switch, row)
			}
			if row.Class != "" || row.Import != "" {
				t.Errorf("%s: a %s switch also names class %q; the vendor keeps its ordinary class, so this would swap in one that does not exist", vendor, row.Switch, row.Class)
			}
		default:
			t.Errorf("%s: switch %q is not one the driver can emit", vendor, row.Switch)
		}
		// A LOCAL value pins a default back on the path where the vendor's own
		// detection is off. Only a setting has a default to pin: a flat argument
		// left off is off, and a class not swapped in is not built.
		if row.LocalValue != "" && row.Switch != SwitchSetting {
			t.Errorf("%s: a %s switch carries a local value %q, which nothing writes on the local path", vendor, row.Switch, row.LocalValue)
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
	if got := ListenTurnDetectorVendors(Pipecat); strings.Join(got, ",") != "cartesia,deepgram,gradium,speechmatics" {
		t.Errorf("vendors = %v, want the four sorted", got)
	}
}

// TestListenTurnDetectorServesItsFamilyOnly is the model-id check this table
// exists for: a Nova model on the Flux service is a connection Deepgram refuses,
// and ink-whisper is the ordinary Cartesia transcriber's model.
//
// The two vendors switched on by a keyword have no second class and so no second
// model list. Every model they serve can decide, which is why they serve one
// here rather than being carved out.
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
		{"gradium", "default", true},
		{"speechmatics", "linden-1", true},
		{"speechmatics", "enhanced", true},
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

// TestEveryListenTurnDetectorRowIsRead: the rows carry a `CeilingArg` and an
// `Eager` answer, and a row whose answer is no has to reach a refusal rather
// than an emitted keyword. Both were written against no real vendor when the
// table held two rows that said yes to both; 1.10.0 supplies two that say no to
// both, so this now asserts the split rather than logging it.
func TestEveryListenTurnDetectorRowIsRead(t *testing.T) {
	predicts := map[string]bool{"deepgram": true, "cartesia": true, "gradium": false, "speechmatics": false}
	ceilings := map[string]bool{"deepgram": true, "cartesia": true, "gradium": false, "speechmatics": false}
	for _, vendor := range ListenTurnDetectorVendors(Pipecat) {
		detector, ok := LookupListenTurnDetector(Pipecat, vendor)
		if !ok {
			t.Fatalf("pipecat names %q and the lookup does not have it", vendor)
		}
		want, known := predicts[vendor]
		if !known {
			t.Errorf("%s is a new row: say here whether it predicts a turn early and whether it has a ceiling, because two refusals read those", vendor)
			continue
		}
		if detector.Eager != want {
			t.Errorf("%s Eager = %v, want %v; validate refuses eager: true where this is false", vendor, detector.Eager, want)
		}
		if detector.HasCeiling() != ceilings[vendor] {
			t.Errorf("%s HasCeiling = %v, want %v; validate refuses pace where this is false", vendor, detector.HasCeiling(), ceilings[vendor])
		}
		if detector.SwapsClass() != (detector.Switch == SwitchClass) {
			t.Errorf("%s SwapsClass disagrees with its own switch %q", vendor, detector.Switch)
		}
	}
}

// TestSpeechmaticsPinsTheTurnModeBothWays is the one row written in two
// directions, and the reason is worth keeping next to the assertion: the
// upstream default flipped from EXTERNAL to VAD in 1.10.0. Without the local
// value, a package that binds this vendor and says nothing about turns changes
// who ends the caller's turn on a version bump, with nobody asking.
func TestSpeechmaticsPinsTheTurnModeBothWays(t *testing.T) {
	row, ok := LookupListenTurnDetector(Pipecat, "speechmatics")
	if !ok {
		t.Fatal("speechmatics has no row")
	}
	if row.EnableValue == row.LocalValue {
		t.Fatalf("both directions write %q, so the turn binding decides nothing", row.EnableValue)
	}
	if !strings.HasSuffix(row.LocalValue, ".EXTERNAL") {
		t.Errorf("local value = %q, want the external mode so pipecat's own detector keeps deciding", row.LocalValue)
	}
	if !strings.HasSuffix(row.EnableValue, ".VAD") {
		t.Errorf("listening value = %q, want the service's own mode", row.EnableValue)
	}
	// The enum is re-exported on the service class, so the emitted module needs
	// no second import. A value spelled any other way would need one.
	entry, ok := DefaultCatalog().Lookup(Pipecat, Listen, "speechmatics")
	if !ok {
		t.Fatal("speechmatics has no pipecat listen entry")
	}
	for _, value := range []string{row.EnableValue, row.LocalValue} {
		if !strings.HasPrefix(value, entry.Call.Class+".") {
			t.Errorf("%q is not reached through %s, so the emitted module would need an import nothing adds", value, entry.Call.Class)
		}
	}
}
