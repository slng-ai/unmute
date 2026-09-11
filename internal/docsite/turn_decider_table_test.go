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
	for vendor, detector := range detectors {
		cells, ok := rows[detector.Class]
		if !ok {
			t.Errorf("models/turn-detection.mdx has no row for %s (%s)", vendor, detector.Class)
			continue
		}
		binding, ceiling, eager := cells[0], cells[1], cells[2]
		for _, want := range []string{"`provider: " + vendor + "`", "`" + detector.ExampleModel + "`"} {
			if !strings.Contains(binding, want) {
				t.Errorf("%s row binding cell %q does not name %s", vendor, strings.TrimSpace(binding), want)
			}
		}
		if !strings.Contains(ceiling, "`"+detector.CeilingArg+"`") {
			t.Errorf("%s row ceiling cell %q does not name %s", vendor, strings.TrimSpace(ceiling), detector.CeilingArg)
		}
		if want := "yes"; detector.Eager != (strings.TrimSpace(eager) == want) {
			t.Errorf("%s row says early answer %q; the table says %v", vendor, strings.TrimSpace(eager), detector.Eager)
		}
	}
	for class := range rows {
		found := false
		for _, detector := range detectors {
			found = found || detector.Class == class
		}
		if !found {
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
