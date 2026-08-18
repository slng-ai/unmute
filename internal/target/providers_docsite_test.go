package target

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestTargetVersionDocsiteMatchesWindows(t *testing.T) {
	path := filepath.Join("..", "..", "docs-site", "reference", "targets-yaml.mdx")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for provider, window := range Windows() {
		want := fmt.Sprintf("`%s` %s", FrameworkPackage(provider), window.Ceiling)
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s supported version drifted from %s: missing %q", provider, path, want)
		}
	}
}

// TestProvidersDocsiteMatchesCatalog binds the public Models pages to the
// catalogue, the same way providers_doc_test.go binds the internal reference.
// The pages state a fact the code already owns, so it gets an agreement test:
// a vendor added or removed in catalog_pipecat.go / catalog_livekit.go fails
// here until the page is updated, and a vendor a page invents fails too.
//
// It also holds the one editorial rule the pages carry (SC-010): SLNG is
// listed first in every role it serves.
//
// Retargeted 2026-08-14 (feature 009): the single reference/providers.mdx page
// retired into one page per role under docs-site/models/.
func TestProvidersDocsiteMatchesCatalog(t *testing.T) {
	// The pages name the roles the way an author writes them (N15 calls the
	// reasoning kind "think"); the catalogue keeps the internal name "reason".
	pages := map[string]Role{"stt": Listen, "tts": Speak, "llm": Reason}
	sections := map[string]Provider{"## Pipecat": Pipecat, "## LiveKit Agents": LiveKit}
	row := regexp.MustCompile("^\\| `([a-z_]+)` \\|")

	for page, role := range pages {
		path := filepath.Join("..", "..", "docs-site", "models", page+".mdx")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		documented := map[Provider][]string{}
		var current Provider
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "## ") {
				current = sections[strings.TrimSpace(line)]
				continue
			}
			if current == "" {
				continue
			}
			if m := row.FindStringSubmatch(line); m != nil {
				documented[current] = append(documented[current], m[1])
			}
		}
		if len(documented) != 2 {
			t.Fatalf("parsed %d provider tables from %s, want 2 (one per target) — heading or table format changed? update this parser", len(documented), path)
		}

		cat := DefaultCatalog()
		for fw, vendors := range documented {
			catalogued := cat.Vendors(fw, role)
			for _, vendor := range vendors {
				if !contains(catalogued, vendor) {
					t.Errorf("models/%s.mdx lists %s %s %q, which the catalogue does not have", page, fw, role, vendor)
				}
			}
			for _, vendor := range catalogued {
				if !contains(vendors, vendor) {
					t.Errorf("catalogue entry %s/%s/%s is missing from models/%s.mdx", fw, role, vendor, page)
				}
			}
			if contains(catalogued, "slng") && vendors[0] != "slng" {
				t.Errorf("models/%s.mdx %s lists %q first; slng leads every list it appears in", page, fw, vendors[0])
			}
		}
	}
}

// TestTurnDetectionDocsiteHasNoVendorList holds the fourth Models page. The turn
// role has no catalogue entries at all, which is exactly why that page explains a
// mechanism per target instead of listing vendors. If the catalogue ever gains a
// turn vendor, this fails and the page has to grow a list the parser above can
// read.
func TestTurnDetectionDocsiteHasNoVendorList(t *testing.T) {
	path := filepath.Join("..", "..", "docs-site", "models", "turn-detection.mdx")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cat := DefaultCatalog()
	for _, fw := range []Provider{Pipecat, LiveKit} {
		if vendors := cat.Vendors(fw, Turn); len(vendors) != 0 {
			t.Errorf("the catalogue now has %s turn vendors %v: models/turn-detection.mdx must list them", fw, vendors)
		}
	}
	row := regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\|")
	if m := row.FindStringSubmatch(string(raw)); m != nil {
		t.Errorf("%s carries a provider-style row for %q, but the turn role has no catalogue vendors", path, m[1])
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
