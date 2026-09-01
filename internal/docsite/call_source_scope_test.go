package docsite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/target"
)

// Every page that teaches a call-source variable says which routes supply one.
//
// A system source is filled by an emitted carrier adapter, and only the LiveKit
// routes emit one, so `source: from_number` on a Pipecat target is refused at
// validation. Six surfaces teach that field now. Two of them used to say only "on a
// phone call", which reads as "any phone route", and a Pipecat author following
// them wrote a package the compiler would not build (2026-08-27).
//
// The claim is pinned in both directions. If a Pipecat route ever does grant a
// call source, the first half fails and names the pages that would then be wrong
// the other way, instead of leaving those pages quietly understating what works.
func TestCallSourcePagesScopeThemToLiveKit(t *testing.T) {
	// The pages that name a system source, wherever they live. The skill is in
	// here too: a coding agent reads it before it writes a package, so a scope it
	// does not know is a package it writes wrong.
	pages := []string{
		filepath.Join(siteRoot, "reference", "variables.mdx"),
		filepath.Join(siteRoot, "build", "variables.mdx"),
		filepath.Join(siteRoot, "build", "prefetch.mdx"),
		filepath.Join(siteRoot, "optimization", "prefetch.mdx"),
		filepath.Join(siteRoot, "telephony", "outbound-calls.mdx"),
		filepath.Join("..", "skill", "assets", "references", "variables.md"),
	}

	var granting []target.TelephonyKey
	for key, route := range target.TelephonyRoutes() {
		if key.Provider != target.Pipecat {
			continue
		}
		for feature := range route.Features {
			if strings.HasPrefix(string(feature), target.TelephonySourcePrefix) {
				granting = append(granting, key)
				break
			}
		}
	}
	if len(granting) > 0 {
		t.Fatalf("a Pipecat route now grants a call source (%v), so these pages understate it and need rewriting: %v", granting, pages)
	}

	for _, page := range pages {
		body, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !strings.Contains(text, "from_number") {
			t.Errorf("%s no longer teaches a call source, so it does not belong in this list", page)
			continue
		}
		// The rest of the wording differs per page on purpose. This phrase is the
		// claim itself, and "Pipecat" alone would not do: every one of these pages
		// names the target somewhere else, so the check would pass on all four
		// while saying nothing.
		if !strings.Contains(strings.ToLower(text), "no pipecat route") {
			t.Errorf("%s teaches a call source without saying no Pipecat route supplies one, so a Pipecat author reads it as available", page)
		}
	}
}
