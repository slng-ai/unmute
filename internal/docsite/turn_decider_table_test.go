package docsite

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/target"
)

// The "Let the transcriber decide" table on the turn-detection page, held
// against internal/target/turn_listener.go both ways: every vendor that can
// decide the turn has a row naming its class, its binding, its example model and
// the field the pace ceiling lands in, and no row names a class the table does
// not have. Same reason as the transfer table: a reader who is told a vendor
// decides turns when the compiler refuses it debugs the wrong thing.
//
// The rows deliberately do not start with a backtick vendor cell, because
// TestTurnDetectionDocsiteHasNoVendorList refuses provider-style rows on this
// page: the turn role has no catalogue vendors, and this table is not one.
var deciderRow = regexp.MustCompile("(?m)^\\|\\s*[A-Z][^|]*\\(`([A-Za-z]+STTService)`\\)\\s*\\|([^|]*)\\|([^|]*)\\|([^|]*)\\|")

// deciderClass is the class a reader will see in the emitted bot for a vendor
// that decides the turn. Two vendors are reached through a class of their own;
// the other two keep their ordinary transcriber and are switched on by an
// argument, so their class comes from the catalogue.
func deciderClass(t *testing.T, detector target.ListenTurnDetector) string {
	t.Helper()
	if detector.SwapsClass() {
		return detector.Class
	}
	entry, ok := target.DefaultCatalog().Lookup(target.Pipecat, target.Listen, detector.Vendor)
	if !ok {
		t.Fatalf("%s decides turns and has no pipecat listen entry to take its class from", detector.Vendor)
	}
	return entry.Call.Class
}

func TestTurnDeciderTableMatchesTheTurnListenerTable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(siteRoot, "models", "turn-detection.mdx"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	rows := map[string][]string{}
	for _, m := range deciderRow.FindAllStringSubmatch(page, -1) {
		rows[m[1]] = m[2:]
	}
	detectors := target.ListenTurnDetectors(target.Pipecat)
	if len(rows) != len(detectors) {
		t.Errorf("the page has %d decider rows and the table has %d", len(rows), len(detectors))
	}
	classes := map[string]bool{}
	for vendor, detector := range detectors {
		class := deciderClass(t, detector)
		classes[class] = true
		cells, ok := rows[class]
		if !ok {
			t.Errorf("models/turn-detection.mdx has no row for %s (%s)", vendor, class)
			continue
		}
		binding, ceiling, eager := cells[0], cells[1], cells[2]
		for _, want := range []string{"`provider: " + vendor + "`", "`" + detector.ExampleModel + "`"} {
			if !strings.Contains(binding, want) {
				t.Errorf("%s row binding cell %q does not name %s", vendor, strings.TrimSpace(binding), want)
			}
		}
		// A vendor whose service exposes no end-of-turn timeout has nowhere for
		// the ceiling to land, and `pace` is refused on it. The cell has to say
		// so rather than quoting a field that does not exist: a reader who is
		// told where pace lands, and then has pace refused, debugs the refusal.
		if detector.HasCeiling() {
			if !strings.Contains(ceiling, "`"+detector.CeilingArg+"`") {
				t.Errorf("%s row ceiling cell %q does not name %s", vendor, strings.TrimSpace(ceiling), detector.CeilingArg)
			}
		} else {
			// It must not borrow another vendor's field, which is what a row
			// written by copying the one above it does.
			for _, other := range detectors {
				if other.HasCeiling() && strings.Contains(ceiling, other.CeilingArg) {
					t.Errorf("%s row ceiling cell %q names %s, which is %s's field; this service exposes no end-of-turn timeout",
						vendor, strings.TrimSpace(ceiling), other.CeilingArg, other.Vendor)
				}
			}
			for _, want := range []string{"no ceiling", "refused"} {
				if !strings.Contains(ceiling, want) {
					t.Errorf("%s row ceiling cell %q does not say %q; pace is refused on this vendor", vendor, strings.TrimSpace(ceiling), want)
				}
			}
		}
		if want := "yes"; detector.Eager != (strings.TrimSpace(eager) == want) {
			t.Errorf("%s row says early answer %q; the table says %v", vendor, strings.TrimSpace(eager), detector.Eager)
		}
	}
	for class := range rows {
		if !classes[class] {
			t.Errorf("models/turn-detection.mdx names %s, which no turn-detecting listener row has", class)
		}
	}
	// The three ceilings the page quotes are the pace table's, in milliseconds.
	for _, pace := range target.PaceValues() {
		ms := int(target.ResolvePace(target.Pipecat, pace).TurnCeiling*1000 + 0.5)
		if !strings.Contains(page, pace+"` is "+strconv.Itoa(ms)) && !strings.Contains(page, pace+"` "+strconv.Itoa(ms)) {
			t.Errorf("the page does not quote %s as %d ms", pace, ms)
		}
	}
}
