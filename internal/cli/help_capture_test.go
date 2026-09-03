package cli

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateHelpCapture = flag.Bool("update", false, "rewrite the captured CLI help text")

// helpCapture is the one place the user docs quote command help from. The
// docs-site CLI pages are written against this file, never against a
// remembered flag list, so a flag rename that nobody re-documents has to fail
// a test rather than reach a reader (constitution III: a fact stated twice
// gets an agreement test).
var helpCapture = filepath.Join("testdata", "help.txt")

// helpCommands are the command paths the docs document, in the order the
// capture file lists them.
var helpCommands = [][]string{{}, {"init"}, {"validate"}, {"compile"}, {"deploy"}, {"dev"}, {"pull"}, {"resources"}, {"skill"}, {"skill", "install"}, {"completion"}}

func renderHelp(t *testing.T) string {
	t.Helper()
	var out strings.Builder
	for _, path := range helpCommands {
		root := newRootCmd()
		// --version only appears in root help once a version is set, and
		// Execute sets it at run time. Any non-empty value renders the same
		// line, so the capture does not go stale on every release.
		root.Version = "test"
		buf := &bytes.Buffer{}
		root.SetOut(buf)
		root.SetErr(buf)
		root.SetArgs(append(append([]string{}, path...), "--help"))
		if err := root.Execute(); err != nil {
			t.Fatalf("unmute %s --help: %v", strings.Join(path, " "), err)
		}
		fmt.Fprintf(&out, "$ unmute %s--help\n", commandPrefix(path))
		out.WriteString(buf.String())
		out.WriteString("\n")
	}
	return out.String()
}

func commandPrefix(path []string) string {
	if len(path) == 0 {
		return ""
	}
	return strings.Join(path, " ") + " "
}

func TestHelpCaptureMatchesBinary(t *testing.T) {
	got := renderHelp(t)
	if *updateHelpCapture {
		if err := os.WriteFile(helpCapture, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(helpCapture)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("command help no longer matches %s.\nRe-capture with:\n\tgo test ./internal/cli -run TestHelpCaptureMatchesBinary -update\nand update the docs-site CLI pages that quote it.\n\ngot:\n%s\nwant:\n%s", helpCapture, got, want)
	}
}

// TestDocsSiteCLIPagesQuoteHelp closes the loop: the capture is only useful if
// the pages actually carry the same flags. Every flag line in the capture must
// appear on the page that documents that command.
func TestDocsSiteCLIPagesQuoteHelp(t *testing.T) {
	pages := map[string]string{
		"":              "overview",
		"init":          "init",
		"validate":      "validate",
		"compile":       "compile",
		"deploy":        "deploy",
		"dev":           "dev",
		"pull":          "pull",
		"resources":     "resources",
		"skill":         "skill",
		"skill install": "skill",
		"completion":    "overview",
	}
	for _, path := range helpCommands {
		name := strings.Join(path, " ")
		page := filepath.Join("..", "..", "docs-site", "reference", "cli", pages[name]+".mdx")
		raw, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		root := newRootCmd()
		root.Version = "test"
		buf := &bytes.Buffer{}
		root.SetOut(buf)
		root.SetErr(buf)
		root.SetArgs(append(append([]string{}, path...), "--help"))
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		// The usage line, which says what the command's arguments are. Checking
		// only flag lines left this ungated, so a page could keep advertising
		// `validate <package-dir>` long after the argument became optional.
		usage := usageLine(buf.String())
		if usage == "" {
			t.Fatalf("no Usage: line in `unmute %s--help`", commandPrefix(path))
		}
		if !strings.Contains(string(raw), usage) {
			t.Errorf("%s does not quote the usage line %q from `unmute %s--help`", page, usage, commandPrefix(path))
		}
		for _, line := range strings.Split(buf.String(), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "-h, --help") {
				continue // -h is on every command and is documented once
			}
			if !strings.Contains(string(raw), trimmed) {
				t.Errorf("%s does not quote %q from `unmute %s--help`", page, trimmed, commandPrefix(path))
			}
		}
	}
}

// usageLine returns the first line under cobra's "Usage:" heading, trimmed.
// That is the line naming the command's arguments, which is exactly the claim
// a docs page goes stale on when an argument becomes optional.
func usageLine(help string) string {
	lines := strings.Split(help, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "Usage:" || i+1 >= len(lines) {
			continue
		}
		return strings.TrimSpace(lines[i+1])
	}
	return ""
}
