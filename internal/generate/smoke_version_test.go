package generate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A smoke script that asserts a framework version as a LITERAL fails the whole
// opt-in suite on the next bump, and fails it for a reason that has nothing to
// do with what the script tests.
//
// That is not hypothetical. Six of these were in the tree, and moving the pin to
// pipecat 1.10.0 broke the salon journeys suite on line 21 of a generated
// smoke_check, long before any behaviour ran. The version is already held in one
// place (internal/target/driver.go) and checked on every compile; what a smoke
// script wants to know is narrower and does not need a literal: did the venv
// install what this project's own compile report asked for.
//
// So the shape is `_report["version"]`, read from the compile report the script
// already opens, and this gate refuses the literal.
//
// It lives in an UNTAGGED file on purpose. The scripts it reads are inside
// `//go:build smoke` files, so a gate carrying that tag would only run in the
// suite it is meant to protect, which is the suite nobody runs on a pull
// request. Reading them as text is what makes the check reachable from
// `make test`.

// frameworkVersionLiteral matches an assertion comparing an installed
// distribution's version against a quoted semver.
var frameworkVersionLiteral = regexp.MustCompile(`version\(\s*"(?:pipecat-ai|livekit-agents)"\s*\)\s*==\s*"(\d+\.\d+\.\d+)"`)

func TestNoSmokeScriptPinsAFrameworkVersionAsALiteral(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("read no files, so this gate would pass for the wrong reason")
	}
	checked, found := 0, 0
	for _, path := range files {
		// This file writes out the shapes it refuses, twice: once as the pattern
		// and once as the fixtures below that prove the pattern works. It also
		// contains the very string the filter greps for, so without this it
		// matches its own filter and reports itself. Same exemption, same
		// reason, as internal/style/literal_test.go.
		if filepath.Base(path) == "smoke_version_test.go" {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		content := string(raw)
		// Only the files holding embedded Python, which is where the literal
		// can hide from every Go tool.
		if !strings.Contains(content, "importlib.metadata import version") {
			continue
		}
		checked++
		for _, line := range strings.Split(content, "\n") {
			if hit := frameworkVersionLiteral.FindStringSubmatch(line); hit != nil {
				found++
				t.Errorf("%s pins the framework version as the literal %q:\n\t%s\nRead it from the compile report the script already opens instead: `_report[\"version\"]`. A literal here fails the whole opt-in suite on the next bump, for a reason that has nothing to do with what this script tests",
					path, hit[1], strings.TrimSpace(line))
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no file carrying an embedded smoke script, so this gate would pass for the wrong reason")
	}
	if found > 0 {
		t.Logf("%d smoke files carry an embedded script", checked)
	}
}

// TestTheSmokeVersionGateCatchesEachSpellingThatShipped proves the matcher
// works on the forms that were actually in the tree, module level and inside a
// coroutine, and that it leaves the replacement alone.
func TestTheSmokeVersionGateCatchesEachSpellingThatShipped(t *testing.T) {
	for _, shipped := range []string{
		`assert version("pipecat-ai") == "1.9.0"`,
		`    assert version("livekit-agents") == "1.6.10"`,
		`assert version( "pipecat-ai" ) == "1.10.0"`,
	} {
		if !frameworkVersionLiteral.MatchString(shipped) {
			t.Errorf("the matcher misses %q, which is a spelling that shipped", shipped)
		}
	}
	for _, fine := range []string{
		`assert version("pipecat-ai") == _report["version"], (version("pipecat-ai"), _report["version"])`,
		`# 1.9.0 changed how the breakdown reports a reply`,
		`assert version("some-other-package") == "1.2.3"`,
	} {
		if frameworkVersionLiteral.MatchString(fine) {
			t.Errorf("the matcher fires on %q, which is not a pinned framework version", fine)
		}
	}
}
