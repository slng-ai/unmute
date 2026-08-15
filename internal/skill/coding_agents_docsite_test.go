package skill

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The coding agents page restates one fact this package owns: which assistants
// the skill supports. Constitution III says a fact stated twice gets an
// agreement test, and FR-018 of feature 012 asks for this one by name. The
// failure always names the page, because the page is what has to change.

const codingAgentsPage = "docs-site/start/coding-agents.mdx"

func codingAgents(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(codingAgentsPage)))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// TestCodingAgentsPageNamesEveryAssistant holds the assistant set. The page
// quotes the summary line the install prints, which is built from Assistants(),
// so adding or removing one changes that line and this test goes red. Quoting
// generated output rather than retyping the list is what makes the page's copy
// derived instead of remembered.
func TestCodingAgentsPageNamesEveryAssistant(t *testing.T) {
	page := codingAgents(t)
	summary := "Installed the Unmute skill for " + strings.Join(Assistants(), ", ") + "."

	if !strings.Contains(page, summary) {
		t.Errorf("%s does not quote the install summary %q; the assistants it shows and the ones the CLI supports have drifted apart", codingAgentsPage, summary)
	}
}

// TestCodingAgentsTableCoversEveryAssistant holds the other half. The summary
// line above is one string, so a page could quote it and still show a table
// that has gone stale. This counts the rows and checks each one names a
// directory the install actually writes.
func TestCodingAgentsTableCoversEveryAssistant(t *testing.T) {
	page := codingAgents(t)

	row := regexp.MustCompile("(?m)^\\| ([A-Za-z][A-Za-z ]*) \\| `(\\.[a-z]+/skills/unmute/)` \\|$")
	rows := row.FindAllStringSubmatch(page, -1)

	if want := len(Assistants()); len(rows) != want {
		t.Fatalf("%s lists %d assistants in its table, but the CLI supports %d; add or remove the row", codingAgentsPage, len(rows), want)
	}

	dirs := map[string]bool{
		Canonical.Rel() + "/": true,
		Pointer.Rel() + "/":   true,
	}
	for _, hit := range rows {
		if !dirs[filepath.ToSlash(hit[2])] {
			t.Errorf("%s says %s reads %q, which is not a directory the install writes", codingAgentsPage, hit[1], hit[2])
		}
	}
}

// TestCodingAgentsPageProofPromptMatchesTheSkill holds the check the page tells
// a reader to run. The page claims a right answer names the four execution
// blocks a target actually emits, so those four have to still be the four the
// bundle teaches. A fifth becoming usable makes the page's check wrong.
func TestCodingAgentsPageProofPromptMatchesTheSkill(t *testing.T) {
	page := codingAgents(t)

	for _, block := range []string{"webhook:", "local:", "mcp:", "builtin:"} {
		if !strings.Contains(page, "`"+block+"`") {
			t.Errorf("%s does not name `%s` in its proof answer; the four kinds the page checks for must be the four the bundle teaches", codingAgentsPage, block)
		}
	}
	if !strings.Contains(bundleFile(t, "references/tools.md"), "## The six execution blocks") {
		t.Errorf("references/tools.md no longer has six execution blocks, so the four the page asks about may have changed; re-check %s", codingAgentsPage)
	}
}
