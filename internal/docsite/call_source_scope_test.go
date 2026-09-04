package docsite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/target"
)

// Every page that teaches a call-source variable says where one is supplied.
//
// This test used to assert the opposite of what it asserts now, and the way it
// changed is the point. A system source was filled only by LiveKit's carrier
// adapter, so six pages said so, and the test held them to it and additionally
// refused to pass the day a Pipecat route granted one: a tripwire, so that a new
// grant could not quietly leave those pages understating what worked.
//
// The tripwire fired. Two Pipecat Twilio routes supply call facts now, so the
// pages say that instead, and this checks the new claim the same way: per route,
// and in both directions.
func TestCallSourcePagesNameWhereEachFactIsSupplied(t *testing.T) {
	// The pages that name a system source, wherever they live. The skill is in
	// here too: a coding agent reads it before it writes a package, so a scope it
	// does not know is a package it writes wrong.
	pages := []string{
		filepath.Join(siteRoot, "reference", "variables.mdx"),
		filepath.Join(siteRoot, "build", "variables.mdx"),
		filepath.Join(siteRoot, "build", "prefetch.mdx"),
		filepath.Join(siteRoot, "optimization", "prefetch.mdx"),
		filepath.Join(siteRoot, "optimization", "overview.mdx"),
		filepath.Join(siteRoot, "telephony", "outbound-calls.mdx"),
		filepath.Join("..", "skill", "assets", "references", "variables.md"),
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
		// The old claim, which is now false. A page still carrying it tells a
		// Pipecat author their package will not build when it will.
		if strings.Contains(strings.ToLower(text), "no pipecat route") {
			t.Errorf("%s still says no Pipecat route supplies a call source, and two of them do now", page)
		}
	}
}

// The pages that teach the caller's number say it is best effort, because on
// every route that supplies one it can still arrive empty.
//
// A caller who withholds their number is ordinary, not exceptional, and it does
// not arrive as nothing: Twilio's own policy is that it arrives as the word
// `anonymous`, and where an upstream carrier sent a word instead, as keypad
// digits shaped exactly like a real number. A page that teaches the field
// without teaching that is a page somebody builds an identification step on.
func TestCallerNumberPagesSayItIsBestEffort(t *testing.T) {
	pages := []string{
		filepath.Join(siteRoot, "build", "prefetch.mdx"),
		filepath.Join(siteRoot, "reference", "variables.mdx"),
		filepath.Join("..", "skill", "assets", "references", "variables.md"),
	}
	for _, page := range pages {
		body, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(body))
		if !strings.Contains(text, "anonymous") {
			t.Errorf("%s teaches the caller's number without saying a withheld one arrives as the word "+
				"anonymous, so a reader builds on a value that is not a number", page)
		}
		if !strings.Contains(text, "best effort") && !strings.Contains(text, "best-effort") {
			t.Errorf("%s teaches the caller's number without saying it is best effort", page)
		}
	}
}

// A route that supplies a fact one way only is a route whose pages have to say
// which way. There are three such cells today and all three are the two Pipecat
// Twilio rows' phone numbers, which is not an omission: a TwiML Bin is attached
// to one number, so the number being called is a constant there and only the
// caller's is worth carrying.
func TestDirectionLimitedFactsAreDocumented(t *testing.T) {
	var limited []string
	for key, route := range target.TelephonyRoutes() {
		for feature, evidence := range route.Features {
			if len(evidence.Directions) > 0 {
				limited = append(limited, string(key.Provider)+" "+key.Transport+" "+string(feature))
			}
		}
	}
	if len(limited) == 0 {
		t.Skip("no route limits a fact to one direction, so there is nothing to document")
	}
	page, err := os.ReadFile(filepath.Join(siteRoot, "build", "prefetch.mdx"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(page)
	// The table's own vocabulary. Cells read `in` or `out` rather than both, and
	// the page has to explain what a one-way cell means, or the reader takes a
	// blank half-row for an oversight.
	for _, want := range []string{"inbound", "outbound"} {
		if !strings.Contains(strings.ToLower(text), want) {
			t.Errorf("docs-site/build/prefetch.mdx does not use the word %q, and %d facts are supplied one way only: %v",
				want, len(limited), limited)
		}
	}
}
