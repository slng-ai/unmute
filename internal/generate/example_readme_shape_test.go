package generate

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// An example README is the first thing a reader opens, and for a long time each
// one had a shape of its own: hotel-concierge opened on four paragraphs before
// naming a command, salon-concierge-single-prompt had two headings, and none of
// the four told a reader what to do when the run failed.
//
// So they now run the slots the docs-site guide pages run, adapted: a
// definition, a map of the page's own sections, a Quickstart that is the
// commands before the explanation, the parts, then Troubleshooting. The slot
// table lives in docs-site/README.md and is the same one; an example README is
// not an MDX page, so it states no keys with ParamField and uses <details> where
// a page uses an Accordion.
//
// The map is held against the page's real H2s rather than only asserted to
// exist, which is the half the docs-site gate cannot do for itself: a map that
// has drifted from the headings is worse than no map, because a reader trusts it
// and it sends them to a section that is not there.
var exampleReadmeNavHeadings = []string{"Where to go next", "Next"}

var (
	exampleH2      = regexp.MustCompile(`(?m)^## +(.+?)\s*$`)
	exampleMapItem = regexp.MustCompile(`(?m)^- +\[([^\]]+)\]\(#[^)]*\)`)
)

func TestExampleReadmesFollowThePageShape(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "agent.yaml")); err != nil {
			continue
		}
		checked++
		t.Run(entry.Name(), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(root, entry.Name(), "README.md"))
			if err != nil {
				t.Fatalf("an example package carries a README: %v", err)
			}
			body := string(raw)

			var headings []string
			for _, match := range exampleH2.FindAllStringSubmatch(body, -1) {
				headings = append(headings, normalizeExampleHeading(match[1]))
			}
			for _, want := range []string{"Quickstart", "Troubleshooting"} {
				if !slices.Contains(headings, want) {
					t.Errorf("no ## %s section: a reader has to be able to run this package, and to find out what to do when it will not run", want)
				}
			}
			// Code before prose. The first H2 is the runnable one, so a reader
			// who wants the commands never reads an argument first.
			if len(headings) > 0 && headings[0] != "Quickstart" {
				t.Errorf("the first section is %q, not Quickstart: an example opens on the commands that run it, not on an explanation", headings[0])
			}

			if !strings.Contains(body, "On this page:") {
				t.Fatal(`no "On this page:" list; a reader cannot see the shape of the page without scrolling it`)
			}
			var mapped []string
			for _, match := range exampleMapItem.FindAllStringSubmatch(body, -1) {
				mapped = append(mapped, normalizeExampleHeading(match[1]))
			}
			for _, item := range mapped {
				if !slices.Contains(headings, item) {
					t.Errorf("the map lists %q and the page has no such section; a map a reader cannot follow is worse than none", item)
				}
			}
			// Every section is in the map, except the closing navigation one:
			// the docs-site pages leave that out too, because it is where the
			// reader lands after the page rather than part of it.
			for _, heading := range headings {
				if slices.Contains(exampleReadmeNavHeadings, heading) || slices.Contains(mapped, heading) {
					continue
				}
				t.Errorf("section %q is not in the map, so a reader scanning the map never learns it is there", heading)
			}
		})
	}
	if checked == 0 {
		t.Fatal("found no example package, so this test would pass for the wrong reason")
	}
	t.Logf("held the page shape on %d example READMEs", checked)
}

// normalizeExampleHeading reads a heading the way a reader does, so a map item
// written with the section's own backticks still matches the section.
func normalizeExampleHeading(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "`", ""))
}
