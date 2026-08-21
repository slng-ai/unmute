package docsite

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/target"
)

// The transfer table on the local-telephony page, held against the route table.
//
// That table is the first thing a reader looks at when a transfer does not
// happen, and it is prose, so it can rot silently while the code moves. It rots
// in the worst direction too: a row saying a shape compiles when the compiler
// refuses it sends somebody to debug their package over a route that was never
// going to work.
//
// Which is exactly what happened. A route's transfer support was reported from
// memory, twice, and both times it was wrong.

// transferRow is one row of the page's table, as written.
type transferRow struct {
	Provider  target.Provider
	Transport string
	Cold      string
	Warm      string
}

// rowPattern reads `| LiveKit `sip` | compiles · ... | **refused at compile** |`.
var rowPattern = regexp.MustCompile(
	"^\\|\\s*(LiveKit|Pipecat)\\s+`([a-z-]+)`\\s*\\|([^|]*)\\|([^|]*)\\|\\s*$")

func TestTransferTableMatchesTheRouteTable(t *testing.T) {
	page, err := os.ReadFile(filepath.Join(siteRoot, "dev", "local-telephony.mdx"))
	if err != nil {
		t.Fatal(err)
	}
	// Only the transfer table, not the plane table further down, which has the
	// same leading two columns and a different meaning.
	section := transferSection(t, string(page))

	var rows []transferRow
	for _, line := range strings.Split(section, "\n") {
		match := rowPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		provider := target.LiveKit
		if match[1] == "Pipecat" {
			provider = target.Pipecat
		}
		rows = append(rows, transferRow{
			Provider: provider, Transport: match[2],
			Cold: strings.TrimSpace(match[3]), Warm: strings.TrimSpace(match[4]),
		})
	}
	if len(rows) == 0 {
		t.Fatal("no transfer rows found on the page; the table's shape changed and this gate went blind")
	}

	// Every row is claimed of a route that exists, and claims what that route
	// actually declares.
	routes := target.TelephonyRoutes()
	seen := map[target.TelephonyKey]bool{}
	for _, row := range rows {
		key := target.TelephonyKey{Provider: row.Provider, Transport: row.Transport, Carrier: "twilio"}
		route, ok := routes[key]
		if !ok {
			t.Errorf("the page has a row for (%s, %s), which is not a route", row.Provider, row.Transport)
			continue
		}
		seen[key] = true
		check := func(shape string, control target.TelephonyControl, written string) {
			_, declared := route.Features[target.TelephonyFeature(control)]
			claimed := !strings.Contains(written, "refused")
			if declared != claimed {
				verb := map[bool]string{true: "compiles", false: "is refused"}
				t.Errorf("(%s, %s) %s transfer %s, and the page says %q",
					row.Provider, row.Transport, shape, verb[declared], written)
			}
		}
		check("cold", target.ColdTransfer, row.Cold)
		check("warm", target.WarmTransfer, row.Warm)
	}

	// And no route with a transfer capability is missing from the table. A route
	// that can transfer and is not listed is the omission a reader cannot see.
	for key, route := range routes {
		if key.Carrier != "twilio" || seen[key] {
			continue
		}
		for _, control := range []target.TelephonyControl{target.ColdTransfer, target.WarmTransfer} {
			if _, declared := route.Features[target.TelephonyFeature(control)]; declared {
				t.Errorf("(%s, %s) emits %s transfer and the page's table does not mention it",
					key.Provider, key.Transport, control)
			}
		}
	}
}

// transferSection is the table under the transfers heading and nothing else.
func transferSection(t *testing.T, page string) string {
	t.Helper()
	const heading = "## Transfers"
	start := strings.Index(page, heading)
	if start < 0 {
		t.Fatalf("the page has no %q section, so a reader looking for transfer support finds nothing", heading)
	}
	rest := page[start+len(heading):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		return rest[:end]
	}
	return rest
}
