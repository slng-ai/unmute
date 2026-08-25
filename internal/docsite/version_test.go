package docsite

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The site states which release of the CLI it describes. That statement has one
// owner, docs-site/snippets/unmute-version.mdx, and the release automation is
// the only thing that moves it. This file holds both halves: the marker is
// well formed, and no page has quietly grown a second copy of it.
//
// A second copy is the failure that matters. A version typed into a page reads
// as authoritative and goes stale silently, because nothing about it changes
// when a release ships.

const (
	versionSnippet = "snippets/unmute-version.mdx"
	versionVar     = "unmuteVersion"
)

// versionPages import the marker and render it. Adding a page here is how a new
// surface joins the contract.
var versionPages = []string{
	"start/installation.mdx",
	"start/quickstart.mdx",
}

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

// siteVersion reads the marker. Every test below starts here, so a missing or
// malformed marker fails once with a clear message instead of many times.
func siteVersion(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(siteRoot, filepath.FromSlash(versionSnippet)))
	if err != nil {
		t.Fatalf("read the version marker: %v; docs-site/%s is where the site says which release it describes", err, versionSnippet)
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
		t.Errorf("docs-site/%s holds %q, which is not a version of the shape v1.2.3; releases are tagged that way and the docs name a release", versionSnippet, version)
	}
}

// TestVersionPagesReadTheMarker holds the reading end. A page that states the
// version has to import it, because a page that types the number is the drift
// this file exists to prevent.
func TestVersionPagesReadTheMarker(t *testing.T) {
	siteVersion(t) // fail early and once if the marker is broken

	for _, page := range versionPages {
		raw, err := os.ReadFile(filepath.Join(siteRoot, filepath.FromSlash(page)))
		if err != nil {
			t.Errorf("read %s: %v", page, err)
			continue
		}
		content := string(raw)

		want := "import { " + versionVar + " } from '/" + versionSnippet + "';"
		if !strings.Contains(content, want) {
			t.Errorf("%s does not carry %q; a page that states the version reads it from the marker", page, want)
		}
		if !strings.Contains(content, "{"+versionVar+"}") {
			t.Errorf("%s imports %s but never renders {%s}; an unused import states nothing to a reader", page, versionVar, versionVar)
		}
	}
}

// TestNoPageHardcodesAVersion is the half that catches drift. Two files are
// exempt and both for a stated reason: the marker is the value, and the
// changelog is a list of releases, where a literal per entry is the content.
//
// Widening this exemption list is how the rule dies. A page that wants to name
// a version imports the marker.
func TestNoPageHardcodesAVersion(t *testing.T) {
	version := siteVersion(t)
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
		checked++
		for _, hit := range versionLiteral.FindAllString(string(raw), -1) {
			t.Errorf("%s writes the version literal %q; import %s from /%s instead, so the release automation can move it (the marker currently says %s)", page, hit, versionVar, versionSnippet, version)
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
