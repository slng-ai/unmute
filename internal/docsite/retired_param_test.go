package docsite

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/target"
)

// A page that teaches a `params:` key the vendor removed hands the reader a
// package the compiler refuses. That is a worse failure than an out-of-date
// sentence, because the reader cannot tell a stale page from a broken install:
// they copied what the documentation told them to write.
//
// This gate exists because the 1.9.0 adoption shipped exactly that. Two surfaces
// written in that release offered `params: {include_results: true}` on a
// Speechmatics binding, and pipecat 1.10.0 removed the setting the next month.
// Without a gate, the correction made in this release rots back the first time
// somebody copies an old example.
//
// It reads the same table the refusal reads (internal/target/retired_params.go),
// so a key added there is caught on every page with no second list.

// paramsUse finds a key written as a `params:` entry, in either spelling the
// pages use: the inline `params: {key: value}` form the provider tables use, and
// the block form an authored sample shows. A key merely NAMED in prose, such as
// a sentence explaining that it was removed, is not a use and is left alone:
// removing those sentences would lose the reader the answer.
func paramsUse(key string) *regexp.Regexp {
	return regexp.MustCompile(`params:\s*\{[^}]*\b` + regexp.QuoteMeta(key) + `\s*:|(?m)^\s*` + regexp.QuoteMeta(key) + `\s*:\s*\S`)
}

func TestNoSurfaceTeachesARetiredVendorParam(t *testing.T) {
	params := target.RetiredParams()
	if len(params) == 0 {
		t.Fatal("no retired params, so this gate would pass for the wrong reason")
	}
	checked := 0
	for _, root := range frameworkVersionSurfaces {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			switch filepath.Ext(path) {
			case ".mdx", ".md":
			default:
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			checked++
			content := string(raw)
			for _, param := range params {
				// Only pages that could be about this vendor at all. A page
				// never naming it cannot be teaching one of its settings, and
				// checking every page for a generic-looking key such as
				// `max_delay` would fire on somebody else's field.
				if !strings.Contains(content, param.Vendor) {
					continue
				}
				if !paramsUse(param.Key).MatchString(content) {
					continue
				}
				t.Errorf("%s writes params.%s on a page that covers %s, and that setting %s. %s. A reader who copies this gets a package the compiler refuses",
					shortPath(path), param.Key, param.Vendor, param.Fate(), upperFirstForTest(param.Advice()))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if checked == 0 {
		t.Fatal("read no files, so this gate would pass for the wrong reason")
	}
}

// TestRetiredParamGateCatchesTheShapeThatShipped is the proof the matcher works.
// The line below is the one the speech-to-text page carried until this release,
// verbatim; a gate that did not catch it would have caught nothing.
func TestRetiredParamGateCatchesTheShapeThatShipped(t *testing.T) {
	shipped := "| `speechmatics` | `params: {include_results: true}` asks for word-level results in each transcript message. |"
	if !paramsUse("include_results").MatchString(shipped) {
		t.Error("the matcher does not catch the inline params form the provider tables use, which is the form that shipped")
	}
	block := "    transcriber:\n      provider: speechmatics\n      params:\n        include_results: true\n"
	if !paramsUse("include_results").MatchString(block) {
		t.Error("the matcher does not catch the block form an authored sample shows")
	}
	// And the sentence that explains the removal is not a use. A gate that
	// refused these would be satisfied by deleting the explanation, which is the
	// opposite of what the reader needs.
	for _, prose := range []string{
		"Eleven settings this service had before Pipecat 1.10.0 are gone, `include_results` among them.",
		"`include_results` was removed upstream; nothing replaced it.",
	} {
		if paramsUse("include_results").MatchString(prose) {
			t.Errorf("the matcher calls %q a use; it is an explanation, and deleting it would lose the reader the answer", prose)
		}
	}
}

// upperFirstForTest keeps the message in this file readable without exporting a
// helper from the compiler for one sentence.
func upperFirstForTest(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}
