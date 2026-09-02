package docsite

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docs-site/README.md states two rules for prose that, until this file, no test
// held: no em or en dash as punctuation, and nothing about how an author
// checked a fact. Both rotted the same way. A review on 2026-09-02 found 34
// prose dashes, 17 figures from offline measurements and 9 sections telling the
// reader how many calls to make before believing a number. Every one of them
// was a sentence a reader had to step over to reach the product.
//
// Fenced code is exempt because it quotes real program output, and a dash the
// CLI prints is a fact about the CLI. The changelog is exempt because it is
// derived from GitHub Releases and changelog_test.go already holds its shape.
// Widening either exemption is how the rule dies.

// fencedBlock matches a whole ``` fence so it can be blanked before the scan.
var fencedBlock = regexp.MustCompile("(?s)```.*?```")

// authorFacing lists the phrases that mark a sentence written for the author
// rather than the reader. Each one was on a live page when this test was
// written, and each is a claim about our own process, not about the product.
var authorFacing = []string{
	"we measured",
	"Measured on 20",
	"Verified on 20",
	"One call proves",
	"Where this page's facts come from",
	"this repository",
}

func TestProseCarriesNoDashAndNoAuthorNote(t *testing.T) {
	exempt := map[string]bool{
		"changelog.mdx": true,
		versionSnippet:  true,
	}

	checked := 0
	err := filepath.WalkDir(siteRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".mdx" {
			return nil
		}
		rel, err := filepath.Rel(siteRoot, path)
		if err != nil {
			return err
		}
		page := filepath.ToSlash(rel)
		if exempt[page] {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		prose := fencedBlock.ReplaceAllStringFunc(string(raw), func(block string) string {
			// Keep the line count so the numbers below still point at the page.
			return strings.Repeat("\n", strings.Count(block, "\n"))
		})
		for i, line := range strings.Split(prose, "\n") {
			if strings.ContainsAny(line, "—–") {
				t.Errorf("%s:%d uses an em or en dash as punctuation; write a period, a comma or a colon, and leave an empty table cell blank", page, i+1)
			}
			lower := strings.ToLower(line)
			for _, phrase := range authorFacing {
				if strings.Contains(lower, strings.ToLower(phrase)) {
					t.Errorf("%s:%d says %q, which tells the reader how the author checked a fact; state what the product does instead", page, i+1, phrase)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("checked no pages, so this test would pass for the wrong reason")
	}
}
