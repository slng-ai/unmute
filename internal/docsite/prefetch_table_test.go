package docsite

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/target"
)

// The call-fact table on the pre-fetch page, held against the route table.
//
// It is the page a reader lands on to answer one question: will this route give
// me the caller's number. Getting that answer from prose means the answer can
// rot while the code moves, and it rots in the worst direction: a reader who
// believes a fact arrives writes a prompt that reads it back to the caller, and
// finds out on a live call that it was empty.
//
// So the table is checked both ways. A cell claiming a fact the route does not
// supply fails, and so does a route supplying one the table does not show.

// The four routes the table has a column for, in the order the header lists
// them. `livekit sip` stands for the three carrier rows, which grant the same
// set; exotel grants nothing and is named in prose rather than given a column.
var prefetchTableRoutes = []struct {
	Header string
	Key    target.TelephonyKey
}{
	{"livekit sip", target.TelephonyKey{Provider: target.LiveKit, Transport: "sip", Carrier: "twilio"}},
	{"livekit connector", target.TelephonyKey{Provider: target.LiveKit, Transport: "connector", Carrier: "twilio"}},
	{"pipecat daily-sip", target.TelephonyKey{Provider: target.Pipecat, Transport: "daily-sip", Carrier: "twilio"}},
	{"pipecat cloud-websocket", target.TelephonyKey{Provider: target.Pipecat, Transport: "cloud-websocket", Carrier: "twilio"}},
}

// prefetchRowPattern reads `| `from_number` | in, out | in, out | in | in |`.
var prefetchRowPattern = regexp.MustCompile("^\\|\\s*`(source\\.[a-z_]+)`\\s*\\|(.*)\\|\\s*$")

func TestPrefetchCallFactTableMatchesTheRouteTable(t *testing.T) {
	page, err := os.ReadFile(filepath.Join(siteRoot, "build", "prefetch.mdx"))
	if err != nil {
		t.Fatal(err)
	}
	section := prefetchWhereItWorksSection(t, string(page))

	rows := map[target.TelephonyFeature][]string{}
	for _, line := range strings.Split(section, "\n") {
		match := prefetchRowPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		cells := strings.Split(strings.TrimSuffix(match[2], "|"), "|")
		if len(cells) != len(prefetchTableRoutes) {
			t.Fatalf("row %q has %d route cells, want %d: the table's shape changed and this gate went blind",
				match[1], len(cells), len(prefetchTableRoutes))
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		rows[target.TelephonyFeature(match[1])] = cells
	}
	if len(rows) == 0 {
		t.Fatal("no call-fact rows found on the page; the table's shape changed and this gate went blind")
	}

	// Every fact the page claims, checked against what the route grants.
	for feature, cells := range rows {
		for i, route := range prefetchTableRoutes {
			want := prefetchCell(route.Key, feature)
			if got := cells[i]; got != want {
				t.Errorf("%s under %s reads %q, and the route table says %q",
					feature, route.Header, got, want)
			}
		}
	}

	// And every fact a route grants, checked against what the page shows. This is
	// the half that catches a new grant nobody documented, which is how a table
	// starts understating what works.
	for _, route := range prefetchTableRoutes {
		for feature := range target.TelephonyRoutes()[route.Key].Features {
			if !strings.HasPrefix(string(feature), target.TelephonySourcePrefix) {
				continue
			}
			if _, shown := rows[feature]; !shown {
				t.Errorf("%s is supplied on %s and the page has no row for it", feature, route.Header)
			}
		}
	}
}

// prefetchCell is what one cell must read: the directions the route supplies the
// fact in, or a dash when it supplies none.
func prefetchCell(key target.TelephonyKey, feature target.TelephonyFeature) string {
	evidence := target.ResolveTelephonyFeature(key, feature)
	if evidence.Tag == target.Gated {
		return ""
	}
	if len(evidence.Directions) == 0 {
		return "in, out"
	}
	ways := make([]string, 0, len(evidence.Directions))
	for _, direction := range evidence.Directions {
		switch direction {
		case target.TelephonyInbound:
			ways = append(ways, "in")
		case target.TelephonyOutbound:
			ways = append(ways, "out")
		default:
			ways = append(ways, string(direction))
		}
	}
	return strings.Join(ways, ", ")
}

// prefetchWhereItWorksSection isolates the table, so a `source.` row somewhere
// else on the page cannot be read as one of its rows.
func prefetchWhereItWorksSection(t *testing.T, page string) string {
	t.Helper()
	const heading = "## Where it works"
	start := strings.Index(page, heading)
	if start < 0 {
		t.Fatalf("docs-site/build/prefetch.mdx has no %q heading; this gate reads the table under it", heading)
	}
	rest := page[start+len(heading):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		return rest[:end]
	}
	return rest
}

// The header row has to name the four routes in the order the cells are read,
// or every assertion above is checking the wrong column. Asserted separately so
// a reordered header fails by saying so rather than by reporting four wrong
// cells.
func TestPrefetchCallFactTableHeaderNamesItsRoutes(t *testing.T) {
	page, err := os.ReadFile(filepath.Join(siteRoot, "build", "prefetch.mdx"))
	if err != nil {
		t.Fatal(err)
	}
	section := prefetchWhereItWorksSection(t, string(page))
	var header string
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "| Fact ") {
			header = line
			break
		}
	}
	if header == "" {
		t.Fatal("the table has no `| Fact |` header row; this gate reads the columns off it")
	}
	position := -1
	for _, route := range prefetchTableRoutes {
		at := strings.Index(header, fmt.Sprintf("`%s`", route.Header))
		if at < 0 {
			t.Errorf("the header does not name the route `%s`", route.Header)
			continue
		}
		if at <= position {
			t.Errorf("the header names `%s` out of order; the cells are read left to right", route.Header)
		}
		position = at
	}
}
