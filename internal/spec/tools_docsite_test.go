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

// TestMCPDocsiteMatchesContractKeys binds the MCP page's illegal-fields table to
// contractToolKeys. The page states which contract fields an `mcp:` file cannot
// carry, and each row quotes that key's own reason, so both are facts the map
// already owns.
//
// This exists because the page also states the count in prose ("Seven fields
// that describe one tool..."), and a key added to the map without touching the
// page leaves the reader a wrong number that nothing else catches. Adding
// `announce` did exactly that.
func TestMCPDocsiteMatchesContractKeys(t *testing.T) {
	path := filepath.Join("..", "..", "docs-site", "build", "tools", "mcp.mdx")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	row := regexp.MustCompile("^\\| `([a-z_]+)` \\| (.+?) \\|$")
	documented := map[string]string{}
	for _, line := range strings.Split(page, "\n") {
		if m := row.FindStringSubmatch(line); m != nil {
			if _, isContract := contractToolKeys[m[1]]; isContract {
				documented[m[1]] = m[2]
			}
		}
	}
	if len(documented) == 0 {
		t.Fatalf("parsed no contract-key rows from %s: table format changed? update this parser", path)
	}

	for name, reason := range contractToolKeys {
		got, ok := documented[name]
		if !ok {
			t.Errorf("an `mcp:` file refuses %q, which %s does not list", name, path)
			continue
		}
		if got != reason {
			t.Errorf("%s gives %q the reason %q, the compiler says %q", path, name, got, reason)
		}
	}
	for name := range documented {
		if _, ok := contractToolKeys[name]; !ok {
			t.Errorf("%s lists %q as illegal on an `mcp:` file, which the compiler allows", path, name)
		}
	}

	// The page counts the fields in prose too, so the number has to move with
	// the map or the reader is told something false.
	count := map[int]string{6: "Six", 7: "Seven", 8: "Eight", 9: "Nine"}[len(contractToolKeys)]
	if count == "" {
		t.Fatalf("contractToolKeys has %d entries: teach this test that number word", len(contractToolKeys))
	}
	if want := count + " fields that describe one tool to the model are illegal"; !strings.Contains(page, want) {
		t.Errorf("%s must say %q, since contractToolKeys has %d entries", path, want, len(contractToolKeys))
	}
}
