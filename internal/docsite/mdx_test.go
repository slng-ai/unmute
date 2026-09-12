package docsite

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Nothing in the Go suite parses MDX, so a page that the site cannot render
// reached main looking exactly like a page that can. Three did: a `cols=2`
// written without braces, and two closing `</Step>` tags that a paragraph
// rewrap pulled up onto the end of a sentence. `mint dev` refused all three
// and then dropped the page out of the navigation, so the three pages the
// reshape spent the most words on were the three a reader could not open.
//
// The real check is the MDX compiler, which needs node and a network install,
// so it cannot run here. These two patterns are what actually broke, they are
// cheap to read off the text, and each was watched failing on the page that
// broke.

// openingTag matches the start of a JSX component tag: a capitalised name, so
// a `<name>` placeholder written in prose is left alone.
var openingTag = regexp.MustCompile(`<([A-Z][A-Za-z0-9]*)\b`)

// closingTag matches a JSX closing tag and what sits in front of it on the line.
var closingTag = regexp.MustCompile(`(?m)^(.*?)</([A-Z][A-Za-z0-9]*)>`)

// attribute matches an attribute name and the first character of its value.
var attribute = regexp.MustCompile(`\s([a-zA-Z][a-zA-Z0-9-]*)=(.)`)

func TestPagesHoldValidMDX(t *testing.T) {
	checked := 0
	err := filepath.WalkDir(siteRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".mdx" {
			return nil
		}
		rel, err := filepath.Rel(siteRoot, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		page := filepath.ToSlash(rel)
		lines := markupLines(string(raw))
		checkAttributeValues(t, page, lines)
		checkClosingTagsStartTheirLine(t, page, lines)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("checked no pages, so this test would pass for the wrong reason")
	}
}

// checkAttributeValues refuses an attribute value that is neither quoted nor
// braced. `<CardGroup cols=2>` reads fine and renders nothing: MDX stops at the
// `2` and refuses the whole page.
func checkAttributeValues(t *testing.T, page string, lines []string) {
	t.Helper()
	for i, line := range lines {
		if !openingTag.MatchString(line) {
			continue
		}
		for _, m := range attribute.FindAllStringSubmatch(line, -1) {
			switch m[2] {
			case `"`, `'`, `{`:
				continue
			}
			t.Errorf("%s:%d writes %s=%s, and an MDX attribute value is quoted or braced; write %s=\"...\" or %s={...}\n    %s",
				page, i+1, m[1], m[2], m[1], m[1], strings.TrimSpace(line))
		}
	}
}

// checkClosingTagsStartTheirLine refuses a closing tag sitting at the end of a
// line of prose, when the element it closes holds more than one block. MDX
// reads `<Step>` followed by a blank line as block content, and then wants its
// closing tag to start a line of its own; glued to the end of a sentence it is
// a parse error naming a paragraph, which does not obviously mean "move the
// tag". An element with no blank line inside it is inline content and is fine
// closed on the same line, which is how several <Note> and <Warning> blocks on
// the site are written.
func checkClosingTagsStartTheirLine(t *testing.T, page string, lines []string) {
	t.Helper()
	for i, line := range lines {
		for _, m := range closingTag.FindAllStringSubmatch(line, -1) {
			before, name := m[1], m[2]
			if strings.TrimSpace(before) == "" {
				continue
			}
			open := openingLine(lines, i, name)
			if open < 0 || !holdsABlankLine(lines, open, i) {
				continue
			}
			t.Errorf("%s:%d closes </%s> at the end of a sentence, and the element spans a blank line, so MDX refuses the page; put the closing tag on its own line\n    %s",
				page, i+1, name, strings.TrimSpace(line))
		}
	}
}

// openingLine returns the index of the nearest `<Name` above line, or -1.
func openingLine(lines []string, line int, name string) int {
	want := "<" + name
	for i := line; i >= 0; i-- {
		rest := lines[i]
		if i == line {
			rest = rest[:strings.Index(rest, "</"+name+">")]
		}
		at := strings.LastIndex(rest, want)
		if at < 0 {
			continue
		}
		after := rest[at+len(want):]
		if after == "" || after[0] == ' ' || after[0] == '>' || after[0] == '\t' || after[0] == '/' {
			return i
		}
	}
	return -1
}

func holdsABlankLine(lines []string, from, to int) bool {
	for i := from + 1; i < to; i++ {
		if strings.TrimSpace(lines[i]) == "" {
			return true
		}
	}
	return false
}

// markupLines blanks out frontmatter and fenced code, keeping every line so a
// reported number is the number the editor shows. A closing tag quoted inside a
// fence is documentation, not markup.
func markupLines(page string) []string {
	lines := strings.Split(page, "\n")
	out := make([]string, len(lines))
	start := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				start = i + 1
				break
			}
		}
	}
	fence := ""
	for i := start; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		switch {
		case fence != "":
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
		case strings.HasPrefix(trimmed, "```"):
			fence = "```"
		case strings.HasPrefix(trimmed, "~~~"):
			fence = "~~~"
		default:
			out[i] = lines[i]
		}
	}
	return out
}
