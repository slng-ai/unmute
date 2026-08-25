package docsite

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// docs-site/changelog.mdx is written by scripts/render_changelog.py from the
// GitHub Release that GoReleaser publishes. Nobody edits it by hand, which is
// exactly why it needs a gate: a renderer defect produces a page that still
// looks plausible, and the release it got wrong has already shipped.
//
// Everything here reads two files. No network, no git, no gh. That matters
// because this runs in the default suite, and the default suite has none of
// those.

const changelogPage = "changelog.mdx"

// entryOpen matches one Update block's opening tag and pulls out the three
// attributes the page contract fixes.
var entryOpen = regexp.MustCompile(`(?m)^<Update\b([^>]*)>$`)

var (
	attrLabel       = regexp.MustCompile(`\blabel="([^"]*)"`)
	attrDescription = regexp.MustCompile(`\bdescription="([^"]*)"`)
	attrTags        = regexp.MustCompile(`\btags=\{\[([^\]]*)\]\}`)
)

// releaseLink is the last line of every entry. The tag in the URL has to match
// the entry's own description, because a link to the wrong release is worse
// than no link: it reads as authoritative.
var releaseLink = regexp.MustCompile(`https://github\.com/slng-ai/unmute/releases/tag/(v[0-9.]+)`)

// commitHash catches the generated commit list surviving into the page. The
// renderer cuts everything from the "## Changelog" marker down, so a 40
// character hash on the page means that cut failed.
var commitHash = regexp.MustCompile(`\b[0-9a-f]{40}\b`)

// headingLine is any markdown heading. The renderer turns every heading inside
// an entry into bold text, because release bodies start at different levels
// from each other and TestPagesHaveCleanStructure forbids both a level one
// heading and a jump between levels. One heading on this page means a body
// reached it unconverted.
var headingLine = regexp.MustCompile(`(?m)^[ \t]*#{1,6}[ \t]`)

type entry struct {
	label       string
	description string
	tags        string
	body        string
	line        int
}

func changelog(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(siteRoot, changelogPage))
	if err != nil {
		t.Fatalf("read the changelog: %v", err)
	}
	return string(raw)
}

// changelogEntries slices the page into entries, keeping the line number so a
// failure points at the block rather than at the file.
func changelogEntries(t *testing.T, page string) []entry {
	t.Helper()
	matches := entryOpen.FindAllStringSubmatchIndex(page, -1)
	if len(matches) == 0 {
		t.Fatal("docs-site/changelog.mdx carries no <Update> entries, so every check below would pass for the wrong reason")
	}

	entries := make([]entry, 0, len(matches))
	for index, match := range matches {
		attrs := page[match[2]:match[3]]
		end := len(page)
		if index+1 < len(matches) {
			end = matches[index+1][0]
		}
		entries = append(entries, entry{
			label:       first(attrLabel, attrs),
			description: first(attrDescription, attrs),
			tags:        first(attrTags, attrs),
			body:        page[match[1]:end],
			line:        strings.Count(page[:match[0]], "\n") + 1,
		})
	}
	return entries
}

func first(pattern *regexp.Regexp, text string) string {
	if hit := pattern.FindStringSubmatch(text); hit != nil {
		return hit[1]
	}
	return ""
}

// order turns v1.2.3 into a comparable slice, for slices.Compare. Comparing the
// parts one at a time by hand is where this went wrong first: component by
// component, v0.1.5 does not look larger than v0.2.0, so a page with those two
// in the wrong order passed.
func order(version string) ([]int, bool) {
	if !semver.MatchString(version) {
		return nil, false
	}
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		out = append(out, value)
	}
	return out, true
}

// TestChangelogEntriesAreWellFormed holds the three attributes. label is what
// the reader sees in the table of contents, description is the version and the
// key the renderer's idempotence depends on, and tags draws the sidebar filter.
func TestChangelogEntriesAreWellFormed(t *testing.T) {
	for _, item := range changelogEntries(t, changelog(t)) {
		where := changelogPage + ":" + strconv.Itoa(item.line)

		if item.label == "" {
			t.Errorf("%s has an <Update> with no label; the label is its table of contents entry and its anchor", where)
		}
		if item.description == "" {
			t.Errorf("%s has an <Update> with no description; the description carries the version, and the renderer keys idempotence on it", where)
			continue
		}
		if !semver.MatchString(item.description) {
			t.Errorf("%s has description %q, which is not a version of the shape v1.2.3", where, item.description)
		}
		if item.tags != `"CLI"` {
			t.Errorf("%s has tags {[%s]}, want {[\"CLI\"]}; the tag draws the sidebar filter", where, item.tags)
		}
		if hit := releaseLink.FindStringSubmatch(item.body); hit == nil {
			t.Errorf("%s (%s) does not link to its release; the linked release carries the binaries and the full commit list", where, item.description)
		} else if hit[1] != item.description {
			t.Errorf("%s says it is %s but links to the %s release", where, item.description, hit[1])
		}
	}
}

// TestChangelogIsNewestFirst holds the order. The renderer inserts at the top,
// so descending order is what a correct run produces and any other order means
// somebody backfilled out of sequence or edited the page by hand.
func TestChangelogIsNewestFirst(t *testing.T) {
	entries := changelogEntries(t, changelog(t))

	previous, ok := order(entries[0].description)
	if !ok {
		t.Fatalf("the first entry's description %q is not a version", entries[0].description)
	}
	above := entries[0].description
	for _, item := range entries[1:] {
		current, ok := order(item.description)
		if !ok {
			t.Errorf("%s:%d has description %q, which is not a version", changelogPage, item.line, item.description)
			continue
		}
		// Strictly descending, so a duplicate entry for one release fails here
		// too rather than only in the renderer.
		if slices.Compare(current, previous) >= 0 {
			t.Errorf("%s:%d puts %s below %s; entries run newest first, strictly", changelogPage, item.line, item.description, above)
		}
		previous, above = current, item.description
	}
}

// TestNewestEntryMatchesTheVersionMarker joins the two halves of the release
// automation. One run adds the entry and moves the marker, so the two
// disagreeing means one of them was written without the other.
func TestNewestEntryMatchesTheVersionMarker(t *testing.T) {
	entries := changelogEntries(t, changelog(t))
	marker := siteVersion(t)

	if entries[0].description != marker {
		t.Errorf("the newest changelog entry is %s but docs-site/%s says the site describes %s; one release was added without moving the marker, or the other way round", entries[0].description, versionSnippet, marker)
	}
}

// TestChangelogObeysTheSiteWritingRules holds the four rules the renderer's
// transforms exist to satisfy. Each one had a real release body behind it.
//
// The dash rule is scoped to this page rather than the whole site on purpose.
// 35 em dashes already live on 7 other pages, so a site-wide check would fail
// on arrival, and the ratchet rule says the code gets fixed rather than the
// gate loosened. This page is written end to end by the renderer, so it can be
// held to the rule from its first line. Widening the rule to the rest of the
// site is a separate change that fixes those 35 first.
func TestChangelogObeysTheSiteWritingRules(t *testing.T) {
	page := changelog(t)

	for _, dash := range []struct{ name, value string }{
		{"em dash", "—"},
		{"en dash", "–"},
	} {
		if count := strings.Count(page, dash.value); count > 0 {
			t.Errorf("docs-site/%s carries %d %s; the site is written without them, and render_changelog.py normalises the ones release notes contain", changelogPage, count, dash.name)
		}
	}
	if hit := commitHash.FindString(page); hit != "" {
		t.Errorf("docs-site/%s carries the commit hash %s; the renderer cuts the generated commit list, so this means that cut failed", changelogPage, hit)
	}
	if headingLine.MatchString(page) {
		t.Errorf("docs-site/%s carries a markdown heading; the renderer turns headings inside an entry into bold text, because release bodies start at different levels and the page's heading hierarchy is checked", changelogPage)
	}
}

// TestChangelogKeepsTheInsertMarker holds the one line the renderer needs. The
// marker sits above every entry so the page's hand written lead paragraph stays
// on top. Delete it and the next release fails to render, on a run nobody is
// watching.
func TestChangelogKeepsTheInsertMarker(t *testing.T) {
	page := changelog(t)
	const marker = "{/* changelog:entries */}"

	if count := strings.Count(page, marker); count != 1 {
		t.Fatalf("docs-site/%s contains %q %d times, want exactly 1; scripts/render_changelog.py inserts each entry directly after it", changelogPage, marker, count)
	}
	entries := changelogEntries(t, page)
	if strings.Index(page, marker) > strings.Index(page, "<Update") {
		t.Errorf("the insert marker sits below the first entry, so the next release would land in the middle of the page instead of on top")
	}
	if got := strings.Count(page, "<Update"); got != len(entries) {
		t.Errorf("found %d <Update> tags but parsed %d entries; an entry is formatted in a way this gate cannot read, so it is not being checked", got, len(entries))
	}
}
