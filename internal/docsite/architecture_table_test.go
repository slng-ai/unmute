package docsite

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/target"
)

// The "What compiles where today" table on the architecture page, held against
// the capability table.
//
// This gate exists because the table was wrong the day it needed to be right.
// `architecture: live` landed on LiveKit and the page went on saying "not yet",
// which is the worst direction for this particular table to rot in: a reader
// who believes their target cannot do it writes the other architecture, or the
// other target, and never finds out. Nothing caught it, because prose about
// which target supports what is prose.
//
// Only three of the four cell states are distinguishable from the table alone,
// so the gate reads the refusal note too:
//
//	yes      the target emits it            Core
//	not yet  a driver gap, refused for now  Gated, and the note says "yet"
//	no       a shape fact about the target  Gated, and it does not
//
// The last two are the distinction the page's own paragraph draws under the
// table, so keeping them apart here is what stops that paragraph becoming a
// sentence nobody can act on.
var architectureTableRows = []struct {
	Name  string
	Field target.Field
}{
	{"`realtime`", target.FieldRealtimeModel},
	{"`live`", target.FieldLiveModel},
}

// The columns, in the order the header lists them.
var architectureTableColumns = []target.Provider{target.LiveKit, target.Pipecat, target.Slng}

var architectureRowPattern = regexp.MustCompile("^\\|\\s*(`[a-z]+`)\\s*\\|(.*)\\|\\s*$")

func TestArchitectureTableMatchesTheCapabilityTable(t *testing.T) {
	page, err := os.ReadFile(filepath.Join(siteRoot, "build", "architecture", "overview.mdx"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(page), "## What compiles where")
	if !ok {
		t.Fatal("the architecture overview has no \"What compiles where\" section; this gate went blind")
	}
	if head, _, cut := strings.Cut(section, "\n## "); cut {
		section = head
	}

	table := target.Default()
	seen := map[string]bool{}
	for _, line := range strings.Split(section, "\n") {
		match := architectureRowPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		row, ok := architectureRow(match[1])
		if !ok {
			// `cascade` has no capability field of its own: it is what a
			// package compiles to when it declares nothing, so there is no row
			// to hold it against.
			continue
		}
		seen[match[1]] = true
		cells := strings.Split(strings.TrimSuffix(match[2], "|"), "|")
		if len(cells) != len(architectureTableColumns) {
			t.Fatalf("row %s has %d cells, want %d: the table's shape changed and this gate went blind",
				match[1], len(cells), len(architectureTableColumns))
		}
		for i, provider := range architectureTableColumns {
			want := architectureCell(table.Capability(row.Field, provider))
			if got := strings.TrimSpace(cells[i]); got != want {
				t.Errorf("build/architecture/overview.mdx says %s on %s is %q; the capability table says %q",
					match[1], provider, got, want)
			}
		}
	}
	for _, row := range architectureTableRows {
		if !seen[row.Name] {
			t.Errorf("build/architecture/overview.mdx has no row for %s, which %s declares", row.Name, row.Field)
		}
	}
}

func architectureRow(name string) (struct {
	Name  string
	Field target.Field
}, bool) {
	for _, row := range architectureTableRows {
		if row.Name == name {
			return row, true
		}
	}
	return struct {
		Name  string
		Field target.Field
	}{}, false
}

func architectureCell(capability target.Capability) string {
	if capability.Tag == target.Core || capability.Tag == target.Warn {
		return "yes"
	}
	if strings.Contains(capability.Note, " yet") {
		return "not yet"
	}
	return "no"
}
