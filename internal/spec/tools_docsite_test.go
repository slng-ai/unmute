package spec

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// TestToolsDocsiteMatchesExecutionBlocks binds the public tools overview page to
// the Tool struct. The page states which execution blocks exist, which is a fact
// the struct already owns, so it gets an agreement test: a block added or removed
// here fails until the page is updated, and a block the page invents fails too.
//
// The page's routing table is the source of the documented set. Every block is a
// pointer field on Tool, so the set is derived rather than listed twice.
func TestToolsDocsiteMatchesExecutionBlocks(t *testing.T) {
	path := filepath.Join("..", "..", "docs-site", "build", "tools", "overview.mdx")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	row := regexp.MustCompile("^\\| `([a-z_]+):` \\|")
	documented := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if m := row.FindStringSubmatch(line); m != nil {
			documented[m[1]] = true
		}
	}
	if len(documented) == 0 {
		t.Fatalf("parsed no execution-block rows from %s: table format changed? update this parser", path)
	}

	blocks := map[string]bool{}
	tool := reflect.TypeOf(Tool{})
	for i := range tool.NumField() {
		field := tool.Field(i)
		if field.Type.Kind() != reflect.Pointer {
			continue // the contract fields, which every block shares
		}
		name := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		blocks[name] = true
	}
	if len(blocks) == 0 {
		t.Fatal("no execution blocks found on Tool: the struct shape changed, so this test needs rewriting")
	}

	for name := range documented {
		if !blocks[name] {
			t.Errorf("%s documents execution block %q, which Tool does not have", path, name)
		}
	}
	for name := range blocks {
		if !documented[name] {
			t.Errorf("Tool has execution block %q, which %s does not document", name, path)
		}
	}
}
