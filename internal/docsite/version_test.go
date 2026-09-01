package docsite

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// No page tells a reader which release the site describes. Releases come faster
// than pages get re-read, so a stated version is stale the day after it is
// written and nothing about the page changes when it goes stale. A reader who
// wants to know what they have runs `unmute --version`; a reader who wants to
// know what changed reads the changelog.
//
// The marker in docs-site/snippets/unmute-version.mdx stays, because the
// release automation uses it as its own cursor: scripts/render_changelog.py
// reads it to decide whether a tag is newer than the page already knows about,
// and moves it when it adds an entry. It is not a sentence anybody reads.
//
// So this file holds two halves: the marker is well formed, and no page states
// a version by any route, neither a typed literal nor by rendering the marker.

const (
	versionSnippet = "snippets/unmute-version.mdx"
	versionVar     = "unmuteVersion"
)

// semver is the shape the marker holds: a leading v and three numeric parts.
// The CLI stamps its version at link time from a tag of this shape, so a marker
// that does not match cannot name a release that exists.
var semver = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// versionLiteral finds a version literal anywhere in prose. Deliberately not
// anchored, because the point is to catch one buried mid-sentence.
var versionLiteral = regexp.MustCompile(`\bv\d+\.\d+\.\d+\b`)

// markerLine matches the one line the snippet carries. The release automation
// rewrites this exact line, so its shape is part of the contract.
var markerLine = regexp.MustCompile(`(?m)^export const ` + versionVar + ` = '([^']*)';$`)

// siteVersion reads the marker. changelog_test.go starts here too, so a missing
// or malformed marker fails once with a clear message instead of many times.
func siteVersion(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(siteRoot, filepath.FromSlash(versionSnippet)))
	if err != nil {
		t.Fatalf("read the version marker: %v; docs-site/%s is the release automation's cursor over the changelog", err, versionSnippet)
	}
	hits := markerLine.FindAllStringSubmatch(string(raw), -1)
	if len(hits) != 1 {
		t.Fatalf("docs-site/%s has %d lines matching `export const %s = '...';`, want exactly 1; the release automation rewrites that one line", versionSnippet, len(hits), versionVar)
	}
	return hits[0][1]
}

// TestVersionMarkerIsSemver holds the value itself.
func TestVersionMarkerIsSemver(t *testing.T) {
	version := siteVersion(t)
	if !semver.MatchString(version) {
		t.Errorf("docs-site/%s holds %q, which is not a version of the shape v1.2.3; releases are tagged that way and the automation compares tags", versionSnippet, version)
	}
}

// TestNoPageStatesAVersion is the half that catches drift, and it refuses both
// routes to the same failure: a typed literal, and an import of the marker that
// puts the same number on the page with a build step in front of it.
//
// Two files are exempt and both for a stated reason: the marker is the value,
// and the changelog is a list of releases, where a literal per entry is the
// content. Widening this exemption list is how the rule dies.
func TestNoPageStatesAVersion(t *testing.T) {
	siteVersion(t) // fail early and once if the marker is broken

	exempt := map[string]bool{
		versionSnippet:  true,
		"changelog.mdx": true,
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
		content := string(raw)
		checked++
		for _, hit := range versionLiteral.FindAllString(content, -1) {
			t.Errorf("%s writes the version literal %q; no page names a release, because releases move faster than pages. Point the reader at `unmute --version` and /changelog instead", page, hit)
		}
		if strings.Contains(content, versionVar) {
			t.Errorf("%s reads %s from /%s; that marker is the release automation's cursor, not a sentence for a reader. Point the reader at `unmute --version` and /changelog instead", page, versionVar, versionSnippet)
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
