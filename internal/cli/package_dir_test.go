package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The package directory is now an optional argument: with none, it is the
// directory you are standing in, so `unmute init x && cd x && unmute validate`
// works. These tests pin that rule and, just as importantly, pin the parts of
// the old behaviour that must not move (contracts C1-C10).
//
// None of them may call t.Parallel: t.Chdir forbids it.

// copyPackage puts a fixture somewhere writable and returns the absolute path.
// Absolute matters — a test that chdirs cannot reach a relative fixture path.
func copyPackage(t *testing.T, fixture string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "agent")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", fixture))); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// C1: the whole point of the feature.
func TestValidateWithNoArgumentUsesTheCurrentDirectory(t *testing.T) {
	t.Chdir(copyPackage(t, "remy"))
	stdout, _, err := runValidateCommand(t)
	if err != nil {
		t.Fatalf("validate in a package must succeed: %v", err)
	}
	if !strings.Contains(stdout, "✓ livekit (livekit)") {
		t.Fatalf("stdout = %q", stdout)
	}
}

// C6: compile writes under the directory it resolved, not under the process's
// idea of somewhere else.
func TestCompileWithNoArgumentWritesUnderTheCurrentDirectory(t *testing.T) {
	dir := copyPackage(t, "remy")
	t.Chdir(dir)
	if _, _, err := runCompileCommand(t); err != nil {
		t.Fatalf("compile in a package must succeed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "build", "livekit", "agent.py")); err != nil {
		t.Fatalf("compile did not write build/livekit under the cwd: %v", err)
	}
}

// C8a: FR-004 covers every flag on all three commands, not just dev's.
func TestFlagsBehaveTheSameWithNoArgument(t *testing.T) {
	t.Chdir(copyPackage(t, "remy"))
	stdout, _, err := runValidateCommand(t, "--target", "livekit")
	if err != nil {
		t.Fatalf("validate --target with no directory: %v", err)
	}
	if !strings.Contains(stdout, "✓ livekit (livekit)") {
		t.Fatalf("validate stdout = %q", stdout)
	}
	if _, _, err := runCompileCommand(t, "--target", "livekit"); err != nil {
		t.Fatalf("compile --target with no directory: %v", err)
	}
}

// C4: the reported bug was `accepts 1 arg(s), received 0`, which says nothing
// about what went wrong or how to fix it. FR-002 requires the file, the
// directory, and both ways to run the command.
func TestNoPackageInTheCurrentDirectoryExplainsBothForms(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = runValidateCommand(t)
	if err == nil {
		t.Fatal("validate outside a package must fail")
	}
	got := err.Error()
	for _, want := range []string{"agent.yaml", abs, "unmute validate"} {
		if !strings.Contains(got, want) {
			t.Errorf("error must name %q; got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "accepts 1 arg") {
		t.Errorf("the cobra arity error is the bug being fixed; got:\n%s", got)
	}
}

// C5: optional is not unlimited.
func TestMoreThanOneArgumentIsStillAUsageError(t *testing.T) {
	if _, _, err := runValidateCommand(t, "a", "b"); err == nil {
		t.Fatal("two positional arguments must be rejected")
	}
}

// C9: the explicit form keeps today's wording, so nothing that already works
// has to be relearned.
func TestExplicitMissingDirectoryKeepsItsExistingError(t *testing.T) {
	_, _, err := runValidateCommand(t, filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("an explicit missing directory must fail")
	}
	if !strings.Contains(err.Error(), "agent.yaml") {
		t.Fatalf("err = %v", err)
	}
}

// C10: the safety case. An upward search would let `unmute compile` run from
// inside build/<target>/ and rewrite the directory the author is standing in.
func TestNoParentSearchFromInsideABuildDirectory(t *testing.T) {
	dir := copyPackage(t, "remy")
	inside := filepath.Join(dir, "build", "livekit")
	if err := os.MkdirAll(inside, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Chdir(inside)
	_, _, err := runCompileCommand(t)
	if err == nil {
		t.Fatal("compile from inside build/<target>/ must not compile the parent")
	}
	if !strings.Contains(err.Error(), "agent.yaml") {
		t.Fatalf("err = %v", err)
	}
	// The parent must be untouched: no artifact appeared from a silent recompile.
	if _, err := os.Stat(filepath.Join(inside, "agent.py")); err == nil {
		t.Fatal("compile wrote into the directory it was standing in")
	}
}

// The header would otherwise read `validate .`, which looks like something
// went wrong on the one screen this feature exists to improve. printHeader is
// TTY-gated, so no captured-output test can see it; this is the only check.
func TestDisplayDirNamesTheDirectoryRatherThanADot(t *testing.T) {
	dir := copyPackage(t, "remy")
	t.Chdir(dir)
	if got := displayDir("."); got != filepath.Base(dir) {
		t.Errorf("displayDir(%q) = %q, want %q", ".", got, filepath.Base(dir))
	}
	if got := displayDir("some/path"); got != "some/path" {
		t.Errorf("an explicit path is shown as written: got %q", got)
	}
}
