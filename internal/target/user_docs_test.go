package target

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestUserDocsCodeTargetParity(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "user")
	checks := []struct {
		path  string
		terms []string
	}{
		{"README.md", []string{"targets/livekit.md", "targets/pipecat.md", "agent.py", "bot.py"}},
		{"start/first-agent.md", []string{"provider: livekit", "provider: pipecat"}},
		{"concepts/how-targets-run-your-agent.md", []string{"AgentTask", "TaskGroup", "worker", "Flow"}},
		{"learn/08-going-live.md", []string{"LiveKit and Pipecat have shipped code", "derived sizing line"}},
	}
	for _, check := range checks {
		raw, err := os.ReadFile(filepath.Join(root, check.path))
		if err != nil {
			t.Fatal(err)
		}
		for _, term := range check.terms {
			if !strings.Contains(string(raw), term) {
				t.Errorf("%s does not document %q", check.path, term)
			}
		}
	}

	stale := []string{
		"only the Pipecat driver ships today",
		"compile the one whose driver is ready (Pipecat today)",
		"Pipecat is the target these docs build toward",
		"This documentation focuses on Pipecat",
		"most complete one today",
	}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, phrase := range stale {
			if strings.Contains(string(raw), phrase) {
				t.Errorf("%s contains stale claim %q", path, phrase)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUserDocsRelativeLinksResolve(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "user")
	link := regexp.MustCompile(`\]\(([^)]+)\)`)
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range link.FindAllStringSubmatch(string(raw), -1) {
			target := strings.Trim(strings.Fields(match[1])[0], "<>")
			if strings.HasPrefix(target, "#") || strings.Contains(target, "://") ||
				strings.HasPrefix(target, "mailto:") {
				continue
			}
			target, _, _ = strings.Cut(target, "#")
			if target == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(path), filepath.FromSlash(target))); err != nil {
				t.Errorf("%s links to missing %s", path, target)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
