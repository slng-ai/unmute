package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// allowedDirectDependencies is the list CLAUDE.md states, kept here because a
// rule with no gate is a wish.
//
// Everything else is the standard library. The bar for adding a row is in
// CLAUDE.md and the constitution: a new dependency needs a concrete reason, and
// standard library code wins when it is enough. Adding a row is the deliberate
// act; this test only makes sure it is deliberate.
var allowedDirectDependencies = map[string]string{
	"github.com/spf13/cobra":               "command tree",
	"github.com/goccy/go-yaml":             "YAML with line/col on parse errors",
	"github.com/google/jsonschema-go":      "schema derivation (0.x: pin exactly, bump deliberately)",
	"github.com/charmbracelet/bubbletea":   "interactive console runtime",
	"github.com/charmbracelet/bubbles":     "console text input",
	"github.com/charmbracelet/lipgloss":    "console styling, via internal/style",
	"golang.org/x/term":                    "terminal size and TTY detection",
	"golang.org/x/sys":                     "terminal syscalls",
	"golang.org/x/text":                    "unicode width",
	"github.com/mattn/go-isatty":           "TTY detection",
	"github.com/mattn/go-runewidth":        "console column widths",
	"github.com/muesli/termenv":            "colour profile detection",
	"github.com/lucasb-eyer/go-colorful":   "adaptive colour",
	"github.com/aymanbagabas/go-osc52/v2":  "clipboard escape",
	"github.com/rivo/uniseg":               "grapheme clustering",
	"github.com/spf13/pflag":               "cobra flags",
	"github.com/inconshreveable/mousetrap": "cobra windows guard",
}

var requireLine = regexp.MustCompile(`^\s*([\w.\-]+(?:\.[\w\-]+)*/[^\s]+)\s+v`)

// TestDirectDependenciesAreOnTheAllowlist fails when go.mod grows a direct
// dependency nobody signed off on.
//
// Only the direct block is checked. Indirect modules arrive because a direct
// one asked for them, so the place to refuse them is the direct row that pulls
// them in — which is exactly what removing charmbracelet/huh did: one direct
// row out, nine modules gone.
func TestDirectDependenciesAreOnTheAllowlist(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	inBlock, direct := false, false
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(line, "require ("):
			inBlock, direct = true, true
			continue
		case line == ")":
			inBlock = false
			continue
		}
		if !inBlock {
			continue
		}
		if strings.Contains(line, "// indirect") {
			direct = false
			continue
		}
		match := requireLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if !direct {
			continue
		}
		if _, ok := allowedDirectDependencies[match[1]]; !ok {
			t.Errorf("go.mod adds direct dependency %q, which is not on the allowlist.\n"+
				"The standard library wins when it is enough (constitution, Technology and Boundaries). "+
				"If this one earns its place, add it to allowedDirectDependencies in this file with the reason, "+
				"and to the dependency list in CLAUDE.md.", match[1])
		}
	}
}

// TestRemovedFormLibraryStaysGone is narrower and blunter: charmbracelet/huh
// was removed in feature 018 because 373 of its 380 uses were a two-field pair
// the console already had. It is easy to reach for again.
func TestRemovedFormLibraryStaysGone(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "charmbracelet/huh") {
		t.Error("charmbracelet/huh is back in go.mod. The console renders its own menus: " +
			"menuChoice in internal/tui/shell.go is the option type, and fieldRunner.selectOne " +
			"is the accessible renderer.")
	}
}
