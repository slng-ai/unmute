package docsite

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skillRefs is the shipped skill's reference set. It quotes the same CLI output
// the docs site does, and a coding agent reads it the way a person reads a page,
// so it is held to the same rule.
const skillRefs = "../../internal/skill/assets/references"

// TestNoVerifiedDateInQuotedCLIOutput keeps a maintainer's provenance out of the
// surfaces a reader lands on.
//
// `unmute compile` prints a telephony evidence line per feature. It used to
// carry verified=<date>, the day someone last read the vendor's documentation.
// That is our record of our own diligence: it tells a reader nothing they can
// act on, and next to their route it reads as a claim about their deployment.
// The tag already says how firm the support is, and docs= is where to check.
//
// The field itself is not the problem and is still there — ir.Validate requires
// one before a feature may claim support, and compile-report.json carries it for
// tools. This is only about what a person is shown.
//
// The transcripts on these pages were hand-copied and nothing held them to the
// real format, which is how the date outlived the decision to drop it.
func TestNoVerifiedDateInQuotedCLIOutput(t *testing.T) {
	for _, root := range []string{siteRoot, skillRefs} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if skipSnippets(path, entry) {
				return filepath.SkipDir
			}
			if entry.IsDir() {
				return nil
			}
			switch filepath.Ext(path) {
			case ".mdx", ".md":
			default:
				return nil
			}
			// The changelog is generated from GitHub Releases and its entries
			// were accurate when written; rewriting history to match a later
			// decision would make it a worse record, not a better one.
			if filepath.Base(path) == "changelog.mdx" {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(raw), "\n") {
				if !strings.Contains(line, "telephony evidence ") {
					continue
				}
				if strings.Contains(line, "verified=") {
					t.Errorf("%s:%d quotes a telephony evidence line carrying verified=; "+
						"the compile output does not print one:\n\t%s",
						path, i+1, strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
