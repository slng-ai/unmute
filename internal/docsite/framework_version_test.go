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

// A framework version is written in two different moods, and only one of them
// can rot.
//
// A PIN is a value a reader copies: `version: "1.10.0"` in a targets.yaml
// sample, or `pipecat-ai[...]==1.10.0` in a dependency line. Copy a stale one
// and the compiler refuses the package, and the reader cannot tell a stale page
// from a broken install.
//
// A HISTORY is a sentence about when something changed: "since Pipecat 1.9.0
// the profanity filter is off unless you ask for it". That stays true forever
// and moving it would make the page wrong.
//
// So this gate holds pins to the support window and leaves history alone.
// Before it existed, fourteen pages named a version and nothing checked any of
// them; the 1.9.0 adoption left every one of them behind for a day.
//
// The support window is the one recorded home for the supported version
// (Principle III). This is the test that makes "every surface reads it" true
// rather than aspirational.

// versionPins are the two shapes a reader copies. Both capture the version.
var versionPins = []struct {
	name string
	re   *regexp.Regexp
}{
	// A targets.yaml sample. Quoted, because the authoring surface requires it.
	{"a targets.yaml version", regexp.MustCompile(`(?m)^\s*version:\s*"(\d+\.\d+\.\d+)"`)},
	// A dependency line, with or without extras in brackets.
	{"a pipecat-ai dependency", regexp.MustCompile(`pipecat-ai(?:\[[^\]]*\])?==(\d+\.\d+\.\d+)`)},
	{"a livekit-agents dependency", regexp.MustCompile(`livekit-agents(?:\[[^\]]*\])?==(\d+\.\d+\.\d+)`)},
	// Prose that states the pin outright, which reads as a pin even though it is
	// a sentence: "installs exactly `pipecat-ai` 1.10.0".
	{"a stated pin", regexp.MustCompile("`(?:pipecat-ai|livekit-agents)`" + `\s+(\d+\.\d+\.\d+)`)},
}

// frameworkVersionSurfaces are the reader-facing files a stale pin reaches.
// README.md is here for the same reason the pages are: it carries a copyable
// sample.
var frameworkVersionSurfaces = []string{siteRoot, skillRefs, "../../README.md"}

// TestFrameworkVersionPinsReadTheSupportWindow refuses a pin that names a
// version the compiler would not accept.
func TestFrameworkVersionPinsReadTheSupportWindow(t *testing.T) {
	supported := map[string]bool{}
	for provider, window := range target.Windows() {
		supported[window.Ceiling] = true
		if window.Floor != window.Ceiling {
			t.Fatalf("%s supports a range (%s to %s); this gate assumes one supported version per framework and has to learn the range before the range ships",
				provider, window.Floor, window.Ceiling)
		}
	}
	if len(supported) == 0 {
		t.Fatal("no support window, so this gate would pass for the wrong reason")
	}

	checked, found := 0, 0
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
			for _, pin := range versionPins {
				for _, hit := range pin.re.FindAllStringSubmatch(content, -1) {
					found++
					if supported[hit[1]] {
						continue
					}
					t.Errorf("%s writes %s as %s; the supported versions are %s. A reader who copies this gets a package the compiler refuses, and cannot tell a stale page from a broken install. The support window in internal/target/driver.go is the one place this is recorded",
						shortPath(path), pin.name, hit[1], strings.Join(sortedKeys(supported), ", "))
				}
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
	if found == 0 {
		t.Fatal("found no version pin on any reader-facing surface; either the shapes above stopped matching how a pin is written, or the pages stopped showing one")
	}
}

// TestFrameworkVersionGateLeavesHistoryAlone is the other half, and it is the
// one that keeps the gate usable. A page saying what changed in an older release
// is telling the truth, and a gate that refused those sentences would be fixed
// by deleting them, which loses the reader the answer.
func TestFrameworkVersionGateLeavesHistoryAlone(t *testing.T) {
	for _, history := range []string{
		"Since Pipecat 1.9.0 the default model is `sonic-3.6`.",
		"since Pipecat 1.9.0 the profanity filter is off unless you ask for it",
		"the framework's default since 1.9.0",
		"Two speaking defaults moved in 1.9.0: `cartesia` now speaks with `sonic-3.6`",
	} {
		for _, pin := range versionPins {
			if pin.re.MatchString(history) {
				t.Errorf("%q reads as %s, and it is history: a sentence about an older release stays true and must not be rewritten on a bump", history, pin.name)
			}
		}
	}
	// And the inverse: each shape it does have to catch.
	for _, want := range []string{
		`    version: "9.9.9"`,
		"`pipecat-ai[openai,runner,silero,tracing,webrtc]==9.9.9`",
		"`livekit-agents[openai]==9.9.9`",
		"installs exactly `pipecat-ai` 9.9.9. Any other version is refused.",
	} {
		matched := false
		for _, pin := range versionPins {
			if pin.re.MatchString(want) {
				matched = true
			}
		}
		if !matched {
			t.Errorf("%q is a pin a reader would copy and no shape catches it", want)
		}
	}
}

func shortPath(path string) string {
	return filepath.ToSlash(strings.TrimPrefix(filepath.Clean(path), filepath.Clean("../../")+string(filepath.Separator)))
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	// Small and fixed; insertion order is not stable across runs, so sort.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
