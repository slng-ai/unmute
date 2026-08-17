package target

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestPrebuiltDocsMatchRegistry(t *testing.T) {
	ids := make([]string, 0, len(prebuilts))
	for id := range prebuilts {
		ids = append(ids, "`"+id+"`")
	}
	sort.Strings(ids)
	want := "Builtin ids: " + strings.Join(ids, ", ") + "."
	for _, path := range []string{
		filepath.Join("..", "skill", "assets", "references", "tools.md"),
		filepath.Join("..", "..", "docs-site", "reference", "agent-yaml.mdx"),
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), want) {
			t.Errorf("%s does not state %q", path, want)
		}
	}
}
