package cli

import (
	"os/exec"
	"strings"
	"testing"
)

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

	cmd := exec.Command("go", "run", "golang.org/x/tools/cmd/deadcode@latest", "-test", "./...")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		// A network-less or module-cache-less environment cannot fetch the
		// tool. That is an environment problem, not a finding, and failing here
		// would train people to ignore this test.
		if strings.Contains(string(out), "dial tcp") ||
			strings.Contains(string(out), "no such host") ||
			strings.Contains(string(out), "module lookup disabled") {
			t.Skipf("cannot fetch deadcode in this environment: %s", out)
		}
		t.Fatalf("deadcode failed: %v\n%s", err, out)
	}

	if report := strings.TrimSpace(string(out)); report != "" {
		t.Errorf("unreachable declarations:\n%s\n\n"+
			"Delete them, or give them a caller. If a template calls it, this tool cannot see that — "+
			"add the caller in Go or confirm against internal/scaffold/template_symbols_test.go.", report)
	}
}
