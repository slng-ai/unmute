package cli

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// deadcodeVersion pins the analysis tool. Bump it deliberately.
const deadcodeVersion = "golang.org/x/tools/cmd/deadcode@v0.49.0"

// TestNothingIsUnreachable fails when a declaration is reachable only from its
// own definition.
//
// -test is not optional. Without it the analysis roots at main alone, calls
// every test-only helper dead, and reports ten false positives on this tree —
// which is how a gate gets switched off within a month.
//
// The tool comes from golang.org/x/tools, already in the module graph, so this
// adds no dependency. It is shelled out to rather than imported because the
// analysis wants a built package graph, not this test's process.
//
// The version is pinned. `@latest` would let a new release change the output
// format, or the finding set, without anything in this repository changing —
// a gate that can start failing on its own teaches people to ignore it.
//
// Its blind spot is the whole reason internal/scaffold/template_symbols_test.go
// exists: a call graph cannot see through text/template. The two gates cover
// different things and neither replaces the other.
func TestNothingIsUnreachable(t *testing.T) {
	if testing.Short() {
		t.Skip("deadcode builds the whole package graph")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}

	cmd := exec.Command("go", "run", deadcodeVersion, "-test", "./...")
	cmd.Dir = "../.."
	// stdout and stderr are kept apart on purpose. `go run` writes its
	// "go: downloading ..." progress to stderr, so a run on a cold module cache
	// — every CI run — would otherwise be read as a page of findings. The
	// report is on stdout; stderr is only ever context for a failure.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// A network-less or module-cache-less environment cannot fetch the
		// tool. That is an environment problem, not a finding, and failing here
		// would train people to ignore this test.
		noise := stderr.String()
		if strings.Contains(noise, "dial tcp") ||
			strings.Contains(noise, "no such host") ||
			strings.Contains(noise, "module lookup disabled") {
			t.Skipf("cannot fetch deadcode in this environment: %s", noise)
		}
		t.Fatalf("deadcode failed: %v\n%s", err, noise)
	}

	if report := strings.TrimSpace(string(out)); report != "" {
		t.Errorf("unreachable declarations:\n%s\n\n"+
			"Delete them, or give them a caller. If a template calls it, this tool cannot see that — "+
			"add the caller in Go or confirm against internal/scaffold/template_symbols_test.go.", report)
	}
}
