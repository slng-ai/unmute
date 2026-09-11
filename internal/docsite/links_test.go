package docsite

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every internal link on the site resolves, to a page and to a heading.
//
// This gate exists because renaming one heading breaks links on pages nobody
// is editing, silently. Restructuring the Variables page on 2026-09-11 broke
// three inbound links from best-practices and optimization, and nothing failed:
// Mintlify renders a dead anchor as a link that scrolls nowhere, which reads to
// a visitor as the docs having lost the section rather than as a typo.
//
// Both halves matter. A dead page link is a 404 the reader can at least see. A
// dead anchor lands them on the top of a long page with no idea which part of
// it they were promised, which is worse on exactly the pages long enough to
// need anchors.

// internalLink matches a link target that stays on the site: a Markdown
// `](/page#anchor)` or `](#anchor)`, and a JSX `href="/page#anchor"`.
//
// The JSX half is not optional. Most navigation between pages here is a
// <Card href="...">, not a Markdown link, so a pattern reading only `](...)`
// walks the site and reports every link resolving while checking almost none
// of them. This gate did exactly that for its first ten minutes, and passed a
// deliberately broken card href.
var internalLink = regexp.MustCompile(`\]\((/[^)\s]*|#[^)\s]*)\)|href="(/[^"\s]*|#[^"\s]*)"`)

// anchorHeading matches an H2 or deeper. H1 is refused by structure_test.go.
var anchorHeading = regexp.MustCompile(`^#{2,6}\s+(.*)$`)

// punctuation is what GitHub-style anchor generation drops, after backticks.
var punctuation = regexp.MustCompile(`[^\w\s-]`)

func TestEveryInternalLinkResolves(t *testing.T) {
	pages := map[string]bool{}
	var files []string
	err := filepath.WalkDir(siteRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipSnippets(path, entry) {
			return fs.SkipDir
		}
		if entry.IsDir() || filepath.Ext(path) != ".mdx" {
			return nil
		}
		rel, err := filepath.Rel(siteRoot, path)
		if err != nil {
			return err
		}
		pages[strings.TrimSuffix(filepath.ToSlash(rel), ".mdx")] = true
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("walked no pages, so this test would pass for the wrong reason")
	}

	// A page's anchors are read once and reused, because a busy page is linked
	// to from many others.
	anchorsOf := map[string]map[string]bool{}
	checked := 0

	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(siteRoot, path)
		if err != nil {
			t.Fatal(err)
		}
		from := filepath.ToSlash(rel)
		self := strings.TrimSuffix(from, ".mdx")
		for _, match := range internalLink.FindAllStringSubmatch(string(raw), -1) {
			href := match[1]
			if href == "" {
				href = match[2]
			}
			target, fragment, _ := strings.Cut(href, "#")
			target = strings.Trim(target, "/")
			if target == "" {
				// A same-page anchor, "#keep-several-records". Checked against
				// this page's own headings rather than skipped: a link into the
				// page you are already on breaks the same way as any other.
				target = self
			}
			checked++
			if !pages[target] {
				t.Errorf("%s links to /%s, which is not a page", from, target)
				continue
			}
			if fragment == "" {
				continue
			}
			if _, ok := anchorsOf[target]; !ok {
				anchorsOf[target] = readAnchors(t, filepath.Join(siteRoot, target+".mdx"))
			}
			if !anchorsOf[target][fragment] {
				t.Errorf("%s links to /%s#%s, and that page has no such heading", from, target, fragment)
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no internal links at all; the link pattern stopped matching")
	}
}

// readAnchors returns the slugs Mintlify generates for a page's headings, which
// is the GitHub rule: lower case, punctuation dropped, spaces to hyphens.
// Backticks go first, so `## Every key an agent takes` and a heading holding
// `code` both slug the way the renderer slugs them.
func readAnchors(t *testing.T, path string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	inFence := false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		match := anchorHeading.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		text := strings.ReplaceAll(match[1], "`", "")
		text = punctuation.ReplaceAllString(text, "")
		text = strings.ToLower(strings.TrimSpace(text))
		found[strings.Join(strings.Fields(text), "-")] = true
	}
	return found
}
