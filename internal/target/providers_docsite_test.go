package target

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	sections := map[string]Provider{"## Pipecat": Pipecat, "## LiveKit": LiveKit}
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

// TestLiveDocsiteMatchesCatalog holds the fifth Models page. A live model
// has a vendor table on Pipecat alone: the other two targets refuse the binding
// by name, so the page carries one `## Pipecat` table and no LiveKit one, and
// that table is the catalogue's live row.
func TestLiveDocsiteMatchesCatalog(t *testing.T) {
	path := filepath.Join("..", "..", "docs-site", "models", "live.mdx")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Only the target section is a vendor table. The page's other tables list
	// the entry's fields and what a live package cannot carry, in the same
	// backticked shape, so the section is what tells them apart.
	row := regexp.MustCompile("^\\| `([a-z_]+)` \\|")
	documented := map[string][]string{}
	var section string
	var sections []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(line)
			sections = append(sections, section)
			continue
		}
		if m := row.FindStringSubmatch(line); m != nil {
			if _, ok := headingFramework[section]; ok {
				documented[section] = append(documented[section], m[1])
			}
		}
	}
	cat := DefaultCatalog()
	// Each code target has its own vendor table, and each is held against the
	// catalogue both ways. Before this architecture compiled on two targets the
	// page had one section and this test refused a second; now a framework with
	// catalogued vendors and no section is what fails, which is the same rule
	// pointed at the current truth rather than the old one.
	for heading, fw := range headingFramework {
		if !slices.Contains(sections, heading) {
			t.Fatalf("models/live.mdx has no %q section, so the %s vendor table cannot be found", heading, fw)
		}
		catalogued := cat.Vendors(fw, Live)
		for _, vendor := range documented[heading] {
			if !contains(catalogued, vendor) {
				t.Errorf("models/live.mdx lists %s live %q, which the catalogue does not have", fw, vendor)
			}
		}
		for _, vendor := range catalogued {
			if !contains(documented[heading], vendor) {
				t.Errorf("catalogue entry %s/live/%s is missing from models/live.mdx", fw, vendor)
			}
		}
	}
	for _, heading := range sections {
		if heading == "## slng" {
			t.Errorf("models/live.mdx has a %q section; the slng target binds three roles by name and has no slot for a live model", heading)
		}
	}
	if vendors := cat.Vendors(Slng, Live); len(vendors) != 0 {
		t.Errorf("the catalogue now has slng live vendors %v: models/live.mdx must grow a section and this test a parser for it", vendors)
	}
	if !strings.Contains(string(raw), "slng target refuses") {
		t.Error("models/live.mdx does not say the slng target refuses a live model")
	}
}

// headingFramework maps each vendor-table heading on models/live.mdx onto the
// framework whose catalogue rows it must match. Only these headings are read as
// vendor tables: the page's other tables list the entry's fields and what a live
// package cannot carry, in the same backticked shape.
var headingFramework = map[string]Provider{
	"## Pipecat":        Pipecat,
	"## LiveKit Agents": LiveKit,
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// TestEmbeddingServicesDocsiteMatchesTable holds the embedding-service table
// against the two surfaces that repeat it: the public page an author lands on,
// and the skill a coding agent reads before it writes a package.
//
// Both directions, because both failures are real. A service in the table and not
// on the page is a feature nobody can find; a service on the page and not in the
// table is a compile error the page told the author to write.
//
// The credential is checked too. A page naming the wrong variable sends the author
// to set something the agent never reads, and the symptom is a startup failure
// that names a variable they believe they already set.
func TestEmbeddingServicesDocsiteMatchesTable(t *testing.T) {
	surfaces := map[string]string{
		"docs-site page": filepath.Join("..", "..", "docs-site", "build", "tools", "knowledge.mdx"),
		"skill":          filepath.Join("..", "skill", "assets", "references", "tools.md"),
	}
	for label, path := range surfaces {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		page := string(raw)
		for _, name := range EmbeddingServiceNames() {
			service, _ := LookupEmbeddingService(name)
			if !strings.Contains(page, "`"+name+"`") {
				t.Errorf("%s does not name embedding service %q", label, name)
			}
			if service.CredentialEnv != "" && !strings.Contains(page, service.CredentialEnv) {
				t.Errorf("%s names %q without its credential %s", label, name, service.CredentialEnv)
			}
		}
		// The other direction: an `embed:` value the page invents.
		row := regexp.MustCompile("(?m)^\\| `([a-z0-9_]+)`(?: \\*\\(default\\)\\*)? \\| `?([A-Z_]+)`? \\|")
		for _, match := range row.FindAllStringSubmatch(page, -1) {
			service, ok := LookupEmbeddingService(match[1])
			if !ok {
				continue // some other table on the page; the loop above is the binding one
			}
			if service.CredentialEnv != match[2] {
				t.Errorf("%s says %q needs %s, the table says %s", label, match[1], match[2], service.CredentialEnv)
			}
		}
	}
}
