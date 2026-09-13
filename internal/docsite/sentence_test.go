package docsite

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// docs-site/README.md rule 4 says "plain language, short sentences". The dash
// half of that rule has been held since 2026-09-02; the sentence half was not
// held by anything, and drifted: a sweep on 2026-09-11 found 61 prose sentences
// over 40 words, one of them 62, chaining four ideas through two colons and a
// semicolon. Every one of them was a sentence a reader had to hold in their
// head to the end before any of it meant anything.
//
// 40 words is deliberately generous. The site's mean is 16, so this fails only
// on the tail, and a sentence that trips it is nearly always two sentences.
const maxSentenceWords = 40

func TestProseSentencesStayShort(t *testing.T) {
	exempt := map[string]bool{
		// Derived from GitHub Releases, so its sentences are not ours to split.
		"changelog.mdx": true,
		versionSnippet:  true,
	}

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
		page := filepath.ToSlash(rel)
		if exempt[page] {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		for _, par := range proseParagraphs(string(raw)) {
			for _, sentence := range splitSentences(stripComponents(par)) {
				if n := len(strings.Fields(sentence)); n > maxSentenceWords {
					t.Errorf("%s has a %d-word sentence; split it:\n    %s", page, n, sentence)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("checked no pages, so this test would pass for the wrong reason")
	}
}

// componentTag matches an inline JSX component tag, opening or closing. Only a
// capitalised name, so `tools/<name>.yaml` written in prose is left alone.
var componentTag = regexp.MustCompile(`</?[A-Z][^>]*>`)

// stripComponents removes inline component tags and keeps the text they wrap.
// A <Tooltip tip="..."> carries a whole sentence of hover text that the reader
// never reads inline, and counting it made the gate fire on sentences that are
// fine on the page.
func stripComponents(par string) string {
	return componentTag.ReplaceAllString(par, "")
}

// proseParagraphs returns the running-prose paragraphs of a page: no frontmatter,
// no fenced code, no tables, headings, list items, block quotes or JSX lines.
// Those carry long lines that are not sentences, and counting them would make
// the gate fire for the wrong reason.
func proseParagraphs(page string) []string {
	lines := strings.Split(page, "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				lines = lines[i+1:]
				break
			}
		}
	}
	var out []string
	var buf []string
	flush := func() {
		if len(buf) > 0 {
			out = append(out, strings.Join(buf, " "))
			buf = nil
		}
	}
	inFence := false
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "```") || strings.HasPrefix(s, "~~~") {
			inFence = !inFence
			flush()
			continue
		}
		if inFence {
			continue
		}
		if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "|") ||
			strings.HasPrefix(s, ">") || strings.HasPrefix(s, "<") ||
			strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ") ||
			listItem.MatchString(s) {
			flush()
			continue
		}
		buf = append(buf, s)
	}
	flush()
	return out
}

var listItem = regexp.MustCompile(`^\d+\.\s`)

// splitSentences cuts a paragraph at terminal punctuation that is followed by
// whitespace and then something that starts a new sentence here: a capital, a
// link, a code span, or bold.
//
// Written as a walk rather than a regexp because Go's RE2 has no lookahead, and
// because a period inside a code span is not a sentence end: `agent.yaml` and
// `result.date` would otherwise each read as two.
func splitSentences(par string) []string {
	var out []string
	start, inCode := 0, false
	runes := []rune(par)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '`' {
			inCode = !inCode
			continue
		}
		if inCode || (runes[i] != '.' && runes[i] != '!' && runes[i] != '?') {
			continue
		}
		j := i + 1
		for j < len(runes) && (runes[j] == ' ' || runes[j] == '\t') {
			j++
		}
		if j == i+1 || j >= len(runes) || !startsSentence(runes[j]) {
			continue
		}
		out = append(out, string(runes[start:i+1]))
		start = j
		i = j - 1
	}
	return append(out, string(runes[start:]))
}

// startsSentence reports whether a rune can begin the next sentence on this
// site: a capital, a Markdown link, a code span, or bold.
func startsSentence(r rune) bool {
	return (r >= 'A' && r <= 'Z') || r == '[' || r == '`' || r == '*' || r == '"' || r == '\''
}

// A guide page long enough to need a map has one.
//
// docs-site/README.md, "The shape of a guide page", puts an "On this page"
// bullet list second, right after the definition. It is the slot a reader uses
// to decide whether they are on the right page at all, and it is the first one
// to get skipped when a page grows a section at a time.
//
// Only the guide sections, and only past a length where scrolling to find the
// shape costs something. A short page is its own map, and the reference
// sections under Configuration files and CLI lead with the complete list
// instead, which README.md calls out as the exception.
const mapNeededAbove = 150

var guideSections = []string{"start", "build", "best-practices", "optimization", "dev"}

func TestLongGuidePagesCarryTheirOwnMap(t *testing.T) {
	checked, mapped := 0, 0
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
		page := filepath.ToSlash(rel)
		section, _, _ := strings.Cut(page, "/")
		if !slices.Contains(guideSections, section) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Count(string(raw), "\n")
		if lines <= mapNeededAbove {
			return nil
		}
		checked++
		if !strings.Contains(string(raw), "On this page:") {
			t.Errorf("%s is %d lines and has no \"On this page:\" list; a reader cannot see its shape without scrolling it", page, lines)
			return nil
		}
		mapped++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("found no long guide pages, so this test would pass for the wrong reason")
	}
	t.Logf("%d of %d guide pages over %d lines carry a map", mapped, checked, mapNeededAbove)
}
