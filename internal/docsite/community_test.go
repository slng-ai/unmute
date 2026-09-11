package docsite

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// One invite, one guide.
//
// The Discord invite is the one link on a community page a reader clicks
// straight away and a maintainer never re-reads. It now sits on seven
// surfaces: the docs landing page, the community page, the contributing page,
// the site's navbar, the site's footer, the repository README, CONTRIBUTING.md
// and the issue template's contact links. Rotating the invite, or letting one
// copy go stale, fails silently everywhere except the one place somebody
// happens to click, and the person it fails for is by definition the person
// who had not arrived yet.
//
// So there is exactly one invite in the tree. That is a thing a test can hold,
// and this is it.
//
// The second half is the guide. The contributing page sends a reader to
// CONTRIBUTING.md by absolute URL, which no link check in this repository can
// follow, so a rename would be found by a contributor and by nobody else.
// Both documents also have to keep naming the five things a contribution
// carries, because dropping one from one of them is how the two surfaces start
// giving a reader different answers.

// repoRoot is the tree above docs-site. retired_output_test.go and
// evidence_line_test.go already reach out of the site this way.
var repoRoot = filepath.Join("..", "..")

// discordInvite is the invite. Every surface names this one and no other.
const discordInvite = "https://discord.gg/kxZactmWj"

// contributingGuide is where the long form lives, and the URL the site uses to
// reach it. The two have to agree or the page sends a reader to a 404.
const (
	contributingGuide = "CONTRIBUTING.md"
	contributingURL   = "https://github.com/slng-ai/unmute/blob/main/CONTRIBUTING.md"
)

// contributingPage is the site's short form of the guide.
var contributingPage = filepath.Join("community", "contributing.mdx")

// anyInvite matches an invite in either shape Discord hands out, so a second
// one written in the other form is still caught.
var anyInvite = regexp.MustCompile(`https://discord\.(?:gg|com/invite)/[A-Za-z0-9_-]+`)

// invitedSurfaces are the files somebody new lands on. Each one carries the
// invite, because a page that says to come and talk with no way to get there
// is worse than a page that says nothing.
var invitedSurfaces = []string{
	"README.md",
	contributingGuide,
	filepath.Join(".github", "ISSUE_TEMPLATE", "config.yml"),
	filepath.Join(".github", "pull_request_template.md"),
	filepath.Join("docs-site", "docs.json"),
	filepath.Join("docs-site", "index.mdx"),
	filepath.Join("docs-site", "community", "overview.mdx"),
	filepath.Join("docs-site", contributingPage),
}

// notScanned are directories that hold generated output, a local checkout's
// own tooling, or someone's ignored working files. Nothing under them is a
// surface a reader lands on.
var notScanned = map[string]bool{
	".git":         true,
	".agents":      true,
	".claude":      true,
	".codex":       true,
	".specify":     true,
	"bin":          true,
	"build":        true,
	"dist":         true,
	"node_modules": true,
	"specs":        true,
	"__pycache__":  true,
	".venv":        true,
}

// scanned are the extensions an invite could be written into.
var scanned = map[string]bool{
	".md":   true,
	".mdx":  true,
	".json": true,
	".yml":  true,
	".yaml": true,
	".go":   true,
	".py":   true,
	".txt":  true,
}

// TestOneDiscordInviteEverywhere is the half that catches drift. A second
// invite anywhere in the tree fails here, naming the file and the line.
func TestOneDiscordInviteEverywhere(t *testing.T) {
	checked := 0
	err := filepath.WalkDir(repoRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if notScanned[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !scanned[filepath.Ext(path)] {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		for number, line := range strings.Split(string(raw), "\n") {
			for _, hit := range anyInvite.FindAllString(line, -1) {
				if hit != discordInvite {
					t.Errorf("%s:%d names the Discord invite %s, and the one invite is %s; "+
						"a second invite rots on its own and nobody notices until a newcomer cannot get in",
						filepath.ToSlash(rel), number+1, hit, discordInvite)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("scanned no files, so this test would pass for the wrong reason")
	}
}

// TestEverySurfaceCarriesTheInvite is the half that catches removal. Each of
// these files is a place somebody arrives before they know anything else.
func TestEverySurfaceCarriesTheInvite(t *testing.T) {
	for _, surface := range invitedSurfaces {
		raw, err := os.ReadFile(filepath.Join(repoRoot, surface))
		if err != nil {
			t.Errorf("read %s: %v; it is one of the surfaces that invites somebody in", filepath.ToSlash(surface), err)
			continue
		}
		if !strings.Contains(string(raw), discordInvite) {
			t.Errorf("%s names no Discord invite; it is one of the places a newcomer lands, so it carries %s",
				filepath.ToSlash(surface), discordInvite)
		}
	}
}

// TestTheSiteOffersTheInviteOnEveryPage holds the two places in docs.json that
// render on every page rather than on one. Mintlify draws nothing that the
// configuration does not ask for, so the absence of either key is the absence
// of the link.
func TestTheSiteOffersTheInviteOnEveryPage(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(siteRoot, "docs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Navbar struct {
			Links []struct {
				Label string `json:"label"`
				Href  string `json:"href"`
			} `json:"links"`
		} `json:"navbar"`
		Footer struct {
			Socials map[string]string `json:"socials"`
		} `json:"footer"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}

	inNavbar := false
	for _, link := range config.Navbar.Links {
		if link.Href == discordInvite {
			inNavbar = true
		}
	}
	if !inNavbar {
		t.Errorf("docs-site/docs.json navbar.links has no link to %s; the navbar is the one invitation that reaches a reader who landed deep in the reference", discordInvite)
	}
	if got := config.Footer.Socials["discord"]; got != discordInvite {
		t.Errorf("docs-site/docs.json footer.socials.discord = %q, want %q", got, discordInvite)
	}
}

// contributionRequirements are the five things a contribution carries. The
// needles are deliberately weak: this catches one deleted outright, which is
// the failure that matters, and leaves every sentence around it free to change.
//
// The fifth is the one that would go first. An example and a video are
// obviously missing when they are missing; a docs page that was never written
// looks exactly like a change that needed no page.
var contributionRequirements = []struct {
	what   string
	needle string
}{
	{"an issue, opened before the code", "issue"},
	{"an example package that uses the new feature", "examples/"},
	{"a video of it working", "video"},
	{"a README for that example", "readme"},
	{"a docs page explaining how the feature works", "docs-site"},
}

// TestBothContributingSurfacesAskForTheSameFiveThings holds the pair. The
// guide on GitHub and the page on the site are read by different people at
// different moments, and a requirement that survives in only one of them is a
// requirement a contributor learns about in review.
func TestBothContributingSurfacesAskForTheSameFiveThings(t *testing.T) {
	surfaces := []string{
		contributingGuide,
		filepath.Join("docs-site", contributingPage),
	}
	for _, surface := range surfaces {
		raw, err := os.ReadFile(filepath.Join(repoRoot, surface))
		if err != nil {
			t.Errorf("read %s: %v", filepath.ToSlash(surface), err)
			continue
		}
		text := strings.ToLower(string(raw))
		for _, want := range contributionRequirements {
			if !strings.Contains(text, want.needle) {
				t.Errorf("%s never says %q, so it no longer asks for %s; both surfaces ask for all five or a contributor learns the missing one in review",
					filepath.ToSlash(surface), want.needle, want.what)
			}
		}
	}
}

// TestTheSitePointsAtTheGuideThatExists holds the one link on the site that no
// link checker here can follow: an absolute URL into the repository's own
// default branch.
func TestTheSitePointsAtTheGuideThatExists(t *testing.T) {
	if _, err := os.Stat(filepath.Join(repoRoot, contributingGuide)); err != nil {
		t.Fatalf("%s is missing: %v; the site links to it by absolute URL, so a rename here is a 404 a reader finds and nobody else does", contributingGuide, err)
	}
	raw, err := os.ReadFile(filepath.Join(siteRoot, contributingPage))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), contributingURL) {
		t.Errorf("docs-site/%s does not link %s; the page is the short form and has to send a reader to the long one", filepath.ToSlash(contributingPage), contributingURL)
	}
}
