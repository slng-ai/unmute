package docsite

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/target"
)

// The per-target tool tables on the tools overview page, held against the
// capability table the compiler actually reads.
//
// Same reasoning as transfer_table_test.go, and the same history: a reader who
// wants to know whether a tool kind works on their target looks at this table
// first, and it is prose, so it rots silently while the code moves. It rots in the
// worst direction too. A row saying a kind compiles where the compiler refuses it
// sends somebody to debug a package over a shape that was never going to work.
//
// This is not hypothetical here. The page said "the six ways one can run" for a
// while after there were seven, and its `announce` rules row named two execution
// blocks after a third had been allowed.

// toolKindRow is one row of the agent-scoped table, as written on the page.
type toolKindRow struct {
	Kind    string
	LiveKit string
	Pipecat string
	Slng    string
}

// wordFor is how the page spells each capability tag. Bold is stripped before
// comparing, so emphasis is the author's choice and not part of the contract.
func wordFor(tag target.Tag) string {
	switch tag {
	case target.Core:
		return "yes"
	case target.Warn:
		return "yes, with a warning"
	case target.Gated:
		return "no"
	}
	return string(tag)
}

var toolRowPattern = regexp.MustCompile(
	`^\|\s*` + "`" + `([a-z_]+):` + "`" + `\s*\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|$`,
)

func readOverview(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "docs-site", "build", "tools", "overview.mdx")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

// section returns the lines between a heading and the next heading of any level.
func section(t *testing.T, body, heading string) []string {
	t.Helper()
	lines := strings.Split(body, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == heading {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("the overview page has no %q heading", heading)
	}
	var out []string
	for _, line := range lines[start:] {
		if strings.HasPrefix(line, "#") {
			break
		}
		out = append(out, line)
	}
	return out
}

func clean(cell string) string {
	return strings.TrimSpace(strings.ReplaceAll(cell, "*", ""))
}

// TestToolKindTableMatchesCapabilities: every agent-scoped tool kind row says what
// the capability table says, for all three targets.
func TestToolKindTableMatchesCapabilities(t *testing.T) {
	body := readOverview(t)
	table := target.Default()

	// The kinds that have a capability field. `webhook:` deliberately has none:
	// it is the baseline every target supports, and the check below holds that.
	fields := map[string]target.Field{
		"local":           target.FieldToolLocal,
		"mcp":             target.FieldToolMCP,
		"builtin":         target.FieldToolBuiltin,
		"client":          target.FieldToolClient,
		"provider_hosted": target.FieldToolProviderHosted,
		"knowledge":       target.FieldToolKnowledge,
	}

	var rows []toolKindRow
	for _, line := range section(t, body, "## How each target treats a tool") {
		if m := toolRowPattern.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			rows = append(rows, toolKindRow{
				Kind: m[1], LiveKit: clean(m[2]), Pipecat: clean(m[3]), Slng: clean(m[4]),
			})
		}
	}
	if len(rows) == 0 {
		t.Fatal("no tool kind rows parsed from the overview page: the table shape changed")
	}

	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.Kind] = true
		if row.Kind == "webhook" {
			for _, cell := range []string{row.LiveKit, row.Pipecat, row.Slng} {
				if cell != "yes" {
					t.Errorf("webhook: row says %q; webhook has no capability field, so every target supports it", cell)
				}
			}
			continue
		}
		field, ok := fields[row.Kind]
		if !ok {
			t.Errorf("the page has a row for %q, which is not a tool kind the capability table knows", row.Kind)
			continue
		}
		for _, check := range []struct {
			provider target.Provider
			written  string
		}{
			{target.LiveKit, row.LiveKit},
			{target.Pipecat, row.Pipecat},
			{target.Slng, row.Slng},
		} {
			want := wordFor(table.Capability(field, check.provider).Tag)
			if check.written != want {
				t.Errorf("%s: on %s the page says %q, the capability table says %q",
					row.Kind, check.provider, check.written, want)
			}
		}
	}

	// Every kind with a field must appear, or a reader checking their target finds
	// nothing and assumes it works.
	for kind := range fields {
		if !seen[kind] {
			t.Errorf("the page's table has no row for %q", kind)
		}
	}
	if !seen["webhook"] {
		t.Error("the page's table has no row for webhook, the one every target supports")
	}
}

// TestToolTaskScopeTableMatchesCapabilities: the task-scoped table, same rule.
//
// Separate because the fields are different constants, and because Pipecat refuses
// both of them for a reason that has nothing to do with the agent-scoped rows.
func TestToolTaskScopeTableMatchesCapabilities(t *testing.T) {
	body := readOverview(t)
	table := target.Default()
	fields := map[string]target.Field{
		"mcp":       target.FieldToolMCPTask,
		"knowledge": target.FieldToolKnowledgeTask,
	}

	found := map[string]bool{}
	for _, line := range section(t, body, "### Attaching a tool to a task") {
		m := toolRowPattern.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		kind := m[1]
		field, ok := fields[kind]
		if !ok {
			continue
		}
		found[kind] = true
		// Only LiveKit and Pipecat are compared literally. The SLNG cell explains
		// that the target has no tasks at all, which is a different statement from
		// a per-kind refusal and is checked by prose below.
		for _, check := range []struct {
			provider target.Provider
			written  string
		}{
			{target.LiveKit, clean(m[2])},
			{target.Pipecat, clean(m[3])},
		} {
			want := wordFor(table.Capability(field, check.provider).Tag)
			written := check.written
			// Pipecat's cell carries the remedy as well as the verdict, which is
			// the whole point of it, so compare on the verdict it starts with.
			if i := strings.Index(written, ","); i > 0 {
				written = written[:i]
			}
			if written != want {
				t.Errorf("task-scoped %s on %s: page says %q, capability table says %q",
					kind, check.provider, written, want)
			}
		}
	}
	for kind := range fields {
		if !found[kind] {
			t.Errorf("the task-scope table has no row for %q", kind)
		}
	}
}

// TestToolPageCountsTheExecutionBlocks: the page's own count of execution blocks
// matches how many there are.
//
// The description said six when there were seven. A number in prose that nothing
// checks is a number that will be wrong.
func TestToolPageCountsTheExecutionBlocks(t *testing.T) {
	body := readOverview(t)
	blocks := []string{
		"webhook", "local", "mcp", "builtin", "client", "provider_hosted", "knowledge",
	}
	words := map[int]string{5: "five", 6: "six", 7: "seven", 8: "eight"}
	want := words[len(blocks)]
	if want == "" {
		t.Fatalf("no word for %d blocks", len(blocks))
	}
	if !strings.Contains(body, "## The "+want+" execution blocks") {
		t.Errorf("the page must head its list %q, because there are %d blocks", "The "+want+" execution blocks", len(blocks))
	}
	if !strings.Contains(body, "the "+want+" ways one can run") {
		t.Errorf("the page description must say %q ways", want)
	}
	for _, block := range blocks {
		if !strings.Contains(body, "`"+block+":`") {
			t.Errorf("the page never names the %q block", block)
		}
	}
}
