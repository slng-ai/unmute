package docsite

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A page that shows an author a YAML block and never says which keys the block
// takes makes them read the whole page to find out what else is legal, and
// guess at the values. The site settled on one answer to that: a run of
// <ParamField>, one per key, with the allowed values in the type. It was on 20
// pages and absent from 25, and which side a page landed on was an accident of
// when it was written.
//
// So this gate is the ratchet. A page that shows an authored YAML block carries
// at least one ParamField, and every ParamField says what the key accepts. The
// exemptions below are pages that deliberately do not state keys, each with the
// reason inline. That list may shrink. It must never grow.

// authoredBlock matches a fence holding YAML an author writes: an `agent.yaml`,
// `targets.yaml` or `connections.yaml` fence, or a bare ```yaml fence.
var authoredBlock = regexp.MustCompile("(?m)^```yaml(?: [a-z_./-]+\\.yaml)?\\s*$")

// paramField matches one key statement, and captures its declared type.
var paramField = regexp.MustCompile(`<ParamField\s+path="([^"]*)"(?:\s+type="([^"]*)")?`)

// keyExempt is every page that shows authored YAML and states no keys on
// purpose. The reason is the point: without one, a page lands here because
// somebody wanted the gate quiet.
var keyExempt = map[string]string{
	// The reference section leads with the complete list instead, which
	// docs-site/README.md already names as the exception to the page shape.
	"reference/agent-yaml.mdx":       "is the complete list",
	"reference/targets-yaml.mdx":     "is the complete list",
	"reference/connections-yaml.mdx": "is the complete list",
	"reference/variables.mdx":        "is the complete list",
	"reference/secrets.mdx":          "is the complete list",
	"reference/cli/init.mdx":         "documents a command, and its flags are the list",

	// Tutorials. Each walks one key at a time in the order an author writes
	// them, and points at the page that owns the full set. Stating the set
	// twice is how the two copies drift.
	"build/your-first-agent.mdx":                   "walks each key in order and defers to the reference",
	"build/orchestration/first-task.mdx":           "walks each key in order and defers to build/orchestration/tasks",
	"build/orchestration/choosing-a-structure.mdx": "compares two shapes rather than documenting either",

	// Advice pages. They argue for a choice and link to the mechanics; their
	// "Where the mechanics live" sections are the pointer.
	"best-practices/context-scope.mdx":  "advises on a choice, links to the mechanics",
	"best-practices/prompt-writing.mdx": "advises on a choice, links to the mechanics",
	"best-practices/state-design.mdx":   "advises on a choice, links to the mechanics",
	"best-practices/step-scoping.mdx":   "advises on a choice, links to the mechanics",
	"optimization/prefetch.mdx":         "argues when to pre-fetch; build/prefetch owns the keys",
	"optimization/execution-layer.mdx":  "restates a binding models/stt and models/tts own",
}

func TestPagesThatShowAKeyStateIt(t *testing.T) {
	shown := 0
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
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		body := string(raw)

		fields := paramField.FindAllStringSubmatch(body, -1)
		for _, f := range fields {
			if strings.TrimSpace(f[2]) == "" {
				t.Errorf("%s states key %q with no type; the type is where a reader learns the allowed values, so write one, such as type=\"cold | warm\"",
					page, f[1])
			}
		}

		if !authoredBlock.MatchString(body) {
			return nil
		}
		shown++
		if _, ok := keyExempt[page]; ok {
			if len(fields) > 0 {
				t.Errorf("%s is on the exempt list and now states its keys; delete its entry from keyExempt, because this list only shrinks", page)
			}
			return nil
		}
		if len(fields) == 0 {
			t.Errorf("%s shows an authored YAML block and states none of its keys; add a \"Every key a <thing> takes\" section of <ParamField> blocks, the way docs-site/models/stt.mdx does, or add the page to keyExempt with the reason it states none",
				page)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if shown == 0 {
		t.Fatal("found no page showing authored YAML, so this test would pass for the wrong reason")
	}
}
