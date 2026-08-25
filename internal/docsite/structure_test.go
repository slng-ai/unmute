package docsite

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const siteRoot = "../../docs-site"

// snippetsDir holds reusable fragments, not pages. Mintlify never renders a
// file under it as a standalone page, so the two walks below skip it: demanding
// a navigation entry for a snippet would fail for the wrong reason, and a
// snippet has no frontmatter to check.
const snippetsDir = "snippets"

// skipSnippets is the WalkDir clause both page walks share.
func skipSnippets(path string, entry fs.DirEntry) bool {
	return entry.IsDir() && entry.Name() == snippetsDir && filepath.Dir(path) == siteRoot
}

type navigation struct {
	Groups []group `json:"groups"`
}

type group struct {
	Name  string            `json:"group"`
	Root  string            `json:"root"`
	Pages []json.RawMessage `json:"pages"`
}

func TestNavigationMatchesPages(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(siteRoot, "docs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Navigation navigation `json:"navigation"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}

	listed := map[string]int{}
	for _, top := range config.Navigation.Groups {
		if top.Root != "" {
			t.Errorf("top-level group %q uses root %q; it will look like a nested link", top.Name, top.Root)
		}
		collectPages(t, top, listed)
	}

	err = filepath.WalkDir(siteRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipSnippets(path, entry) {
			return fs.SkipDir
		}
		if entry.IsDir() || filepath.Ext(path) != ".mdx" {
			return nil
		}
		rel, err := filepath.Rel(siteRoot, path)
		if err != nil {
			return err
		}
		page := strings.TrimSuffix(filepath.ToSlash(rel), ".mdx")
		if listed[page] != 1 {
			t.Errorf("%s appears %d times in navigation, want exactly once", page, listed[page])
		}
		delete(listed, page)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for page, count := range listed {
		t.Errorf("navigation references missing page %s %d times", page, count)
	}
}

func collectPages(t *testing.T, current group, listed map[string]int) {
	t.Helper()
	if current.Root != "" {
		listed[current.Root]++
	}
	seenGroup := false
	pageAfterGroup := false
	for _, raw := range current.Pages {
		var page string
		if err := json.Unmarshal(raw, &page); err == nil {
			listed[page]++
			pageAfterGroup = seenGroup
			continue
		}
		var child group
		if err := json.Unmarshal(raw, &child); err != nil {
			t.Fatalf("decode child of %q: %v", current.Name, err)
		}
		if pageAfterGroup {
			t.Errorf("group %q puts nested group %q after a page that follows another nested group", current.Name, child.Name)
		}
		seenGroup = true
		collectPages(t, child, listed)
	}
}

func TestPagesHaveCleanStructure(t *testing.T) {
	titles := map[string]string{}
	err := filepath.WalkDir(siteRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipSnippets(path, entry) {
			return fs.SkipDir
		}
		if entry.IsDir() || filepath.Ext(path) != ".mdx" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(raw), "\n")
		if len(lines) < 3 || lines[0] != "---" {
			t.Errorf("%s has no frontmatter", path)
			return nil
		}
		end := 1
		for end < len(lines) && lines[end] != "---" {
			end++
		}
		if end == len(lines) {
			t.Errorf("%s has unclosed frontmatter", path)
			return nil
		}
		title := frontmatterValue(lines[1:end], "title")
		if title == "" {
			t.Errorf("%s has no title", path)
		} else if previous := titles[title]; previous != "" {
			t.Errorf("%s and %s share the title %q", previous, path, title)
		} else {
			titles[title] = path
		}
		if frontmatterValue(lines[1:end], "description") == "" {
			t.Errorf("%s has no description", path)
		}

		previousLevel := 1
		inFence := false
		for number, line := range lines[end+1:] {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				inFence = !inFence
				continue
			}
			if inFence || !strings.HasPrefix(line, "#") {
				continue
			}
			level := strings.IndexByte(line, ' ')
			if level < 1 || strings.Trim(line[:level], "#") != "" {
				continue
			}
			lineNumber := end + number + 2
			if level == 1 {
				t.Errorf("%s:%d has an H1; frontmatter already renders the page title", path, lineNumber)
			}
			if level > previousLevel+1 {
				t.Errorf("%s:%d jumps from H%d to H%d", path, lineNumber, previousLevel, level)
			}
			previousLevel = level
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func frontmatterValue(lines []string, key string) string {
	prefix := key + ":"
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, prefix)), `"'`)
		}
	}
	return ""
}
