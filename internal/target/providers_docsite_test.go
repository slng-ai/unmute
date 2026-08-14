package target

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestProvidersDocsiteMatchesCatalog binds the public providers page to the
// catalogue, the same way providers_doc_test.go binds the internal reference.
// The page states a fact the code already owns, so it gets an agreement test:
// a vendor added or removed in catalog_pipecat.go / catalog_livekit.go fails
// here until the page is updated, and a vendor the page invents fails too.
//
// It also holds the one editorial rule the page carries (SC-010): SLNG is
// listed first in every role it serves.
func TestProvidersDocsiteMatchesCatalog(t *testing.T) {
	path := filepath.Join("..", "..", "docs-site", "reference", "providers.mdx")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	sections := map[string]Provider{"## Pipecat": Pipecat, "## LiveKit Agents": LiveKit}
	// The page names the roles the way an author writes them (N15 calls the
	// reasoning kind "think"); the catalogue keeps the internal name "reason".
	roles := map[string]Role{"listen": Listen, "speak": Speak, "think": Reason}
	roleHeading := regexp.MustCompile(`^### (Listen|Speak|Think)\b`)
	row := regexp.MustCompile("^\\| `([a-z_]+)` \\|")

	type key struct {
		fw   Provider
		role Role
	}
	documented := map[key][]string{}
	var current Provider
	var role Role
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "## ") {
			current, role = sections[strings.TrimSpace(line)], ""
			continue
		}
		if m := roleHeading.FindStringSubmatch(line); m != nil {
			role = roles[strings.ToLower(m[1])]
			continue
		}
		if current == "" || role == "" {
			continue
		}
		if m := row.FindStringSubmatch(line); m != nil {
			documented[key{current, role}] = append(documented[key{current, role}], m[1])
		}
	}
	if len(documented) != 6 {
		t.Fatalf("parsed %d provider tables from %s, want 6 (two targets x three roles) — heading or table format changed? update this parser", len(documented), path)
	}

	cat := DefaultCatalog()
	for k, vendors := range documented {
		catalogued := cat.Vendors(k.fw, k.role)
		for _, vendor := range vendors {
			if !contains(catalogued, vendor) {
				t.Errorf("providers.mdx lists %s %s %q, which the catalogue does not have", k.fw, k.role, vendor)
			}
		}
		for _, vendor := range catalogued {
			if !contains(vendors, vendor) {
				t.Errorf("catalogue entry %s/%s/%s is missing from providers.mdx", k.fw, k.role, vendor)
			}
		}
		if contains(catalogued, "slng") && vendors[0] != "slng" {
			t.Errorf("%s %s lists %q first; slng leads every list it appears in", k.fw, k.role, vendors[0])
		}
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
