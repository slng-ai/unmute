// Package brand holds no code. It exists for one test: the brand marks live in
// images/ but three renderers each need their own copy, because none of them
// can read outside its own root. GitHub resolves README paths from the
// repository root, Mintlify from docs-site/, and go:embed refuses any path
// outside its package. Duplication is forced, so this test is what keeps the
// copies honest.
package brand

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// repo walks up from this package to the repository root.
func repo(parts ...string) string {
	return filepath.Join(append([]string{"..", ".."}, parts...)...)
}

// TestMarkCopiesMatchCanonical fails when a copy of a brand mark stops matching
// the canonical file in images/. Update images/ first, then re-copy.
func TestMarkCopiesMatchCanonical(t *testing.T) {
	for _, tc := range []struct {
		canonical string
		copy      string
	}{
		{repo("images", "Isotype_UNMUTE.svg"), repo("docs-site", "logo", "Isotype_UNMUTE.svg")},
		{repo("images", "Isotype_UNMUTE_wb.svg"), repo("docs-site", "logo", "Isotype_UNMUTE_wb.svg")},
		{repo("images", "Isotype_UNMUTE_wb.svg"), repo("internal", "web", "logo.svg")},
	} {
		want, err := os.ReadFile(tc.canonical)
		if err != nil {
			t.Errorf("read canonical %s: %v", tc.canonical, err)
			continue
		}
		got, err := os.ReadFile(tc.copy)
		if err != nil {
			t.Errorf("read copy %s: %v", tc.copy, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s no longer matches %s; re-copy the canonical file over it", tc.copy, tc.canonical)
		}
	}
}
