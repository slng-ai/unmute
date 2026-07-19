package target

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestProvidersDocMatchesCatalog is compiler.md V20 (B4): every vendor row in
// docs/user/reference/providers.md resolves through Catalog.Lookup, and every
// non-wildcard code-target entry appears in that reference. Either direction
// of drift — a silent catalogue deletion (B4's revert) or an undocumented
// addition — is a red build, never a quiet narrowing of user-visible breadth.
func TestProvidersDocMatchesCatalog(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "user", "reference", "providers.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	sections := map[string]Provider{"## Pipecat": Pipecat, "## LiveKit": LiveKit}
	// N15 calls the author-facing model kind "think" while the resolved
	// catalogue deliberately keeps the internal role name "reason".
	roles := map[string]Role{"listen": Listen, "speak": Speak, "think": Reason}
	row := regexp.MustCompile("^\\| (listen|speak|think) \\| `([a-z_]+)`")

	type key struct {
		fw     Provider
		role   Role
		vendor string
	}
	documented := map[key]bool{}
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
			documented[key{current, roles[m[1]], m[2]}] = true
		}
	}
	if len(documented) == 0 {
		t.Fatal("no provider rows parsed from providers.md — table format changed? update this parser")
	}

	cat := DefaultCatalog()
	for k := range documented {
		if _, ok := cat.Lookup(k.fw, k.role, k.vendor); !ok {
			t.Errorf("providers.md documents %s %s %q but Catalog.Lookup does not resolve it", k.fw, k.role, k.vendor)
		}
	}
	for _, e := range cat.Entries() {
		if e.Wildcard() || (e.Framework != Pipecat && e.Framework != LiveKit) {
			continue
		}
		if !documented[key{e.Framework, e.Role, e.Vendor}] {
			t.Errorf("catalogue entry %s/%s/%s is missing from providers.md", e.Framework, e.Role, e.Vendor)
		}
	}
}
