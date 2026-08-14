package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/generate"
)

// TestFormatPythonSeparatesBadCodeFromBadEnvironment: `ruff format` exits 2 both
// when the source will not parse and when ruff itself cannot run, so the exit
// code alone cannot tell a generator defect from an environment problem. Only
// the first may fail a compile; treating the second the same way would reject
// perfectly valid emitted Python and blame the generator for it.
func TestFormatPythonSeparatesBadCodeFromBadEnvironment(t *testing.T) {
	if _, err := exec.LookPath("ruff"); err != nil {
		t.Skip("ruff not installed")
	}

	t.Run("unparseable source is a generator defect", func(t *testing.T) {
		_, found, unparseable, failure := formatPython([]byte("def broken(:\n"))
		if !found || !unparseable || failure == nil {
			t.Fatalf("found=%v unparseable=%v failure=%v", found, unparseable, failure)
		}
	})

	t.Run("valid source formats cleanly", func(t *testing.T) {
		formatted, found, unparseable, failure := formatPython([]byte("x  =  1\n"))
		if !found || unparseable || failure != nil {
			t.Fatalf("found=%v unparseable=%v failure=%v", found, unparseable, failure)
		}
		if string(formatted) != "x = 1\n" {
			t.Fatalf("formatted = %q", formatted)
		}
	})

	// ruff colours its diagnostics when FORCE_COLOR or CLICOLOR_FORCE is set,
	// even with stderr piped, and NO_COLOR does not override them. The escapes
	// land inside the marker ("\x1b[1;31merror\x1b[0m\x1b[1m:\x1b[0m ..."), so a
	// literal match would miss and compile would report success on Python that
	// cannot be parsed. Anyone exporting FORCE_COLOR in their shell or CI would
	// have hit it.
	for _, forced := range []string{"FORCE_COLOR", "CLICOLOR_FORCE"} {
		t.Run("classification survives "+forced, func(t *testing.T) {
			t.Setenv(forced, "1")
			_, _, unparseable, failure := formatPython([]byte("def broken(:\n"))
			if !unparseable {
				t.Fatalf("%s must not hide a parse failure: %v", forced, failure)
			}
			if strings.Contains(failure.Error(), "\x1b[") {
				t.Fatalf("the reported detail still carries ANSI escapes: %q", failure)
			}
		})
	}

	// The regression this guards: a stray ruff config anywhere above the working
	// directory used to make ruff exit 2 on valid input. Its message even
	// contains "Failed to parse", about the TOML rather than the Python, so a
	// looser match would call valid output a generator defect. --isolated stops
	// ruff looking for that config at all.
	t.Run("a broken ruff config above the cwd must not condemn valid source", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "ruff.toml"), []byte("line-length = \"nope\"\n[lint\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		sub := filepath.Join(root, "sub")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		restore, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(sub); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(restore) })

		formatted, _, unparseable, failure := formatPython([]byte("x  =  1\n"))
		if unparseable {
			t.Fatal("a bad ruff config must never be reported as invalid emitted Python")
		}
		if failure != nil {
			t.Fatalf("config discovery should be isolated away entirely: %v", failure)
		}
		if string(formatted) != "x = 1\n" {
			t.Fatalf("formatted = %q", formatted)
		}
	})
}

// The write path fails only on the generator defect, and still writes the files
// either way so the output can be inspected.
func TestWriteArtifactFailsOnlyOnInvalidPython(t *testing.T) {
	if _, err := exec.LookPath("ruff"); err != nil {
		t.Skip("ruff not installed")
	}
	dir := t.TempDir()
	var warn strings.Builder
	out := filepath.Join(dir, "build")
	err := writeArtifactFiles(&warn, out, []generate.File{{Path: "bot.py", Content: []byte("def broken(:\n")}})
	if err == nil {
		t.Fatal("unparseable emitted Python must fail the compile, not warn")
	}
	if !strings.Contains(err.Error(), "bot.py") || !strings.Contains(err.Error(), "not valid") {
		t.Fatalf("the error must name the file and the reason, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(out, "bot.py")); statErr != nil {
		t.Fatalf("the file must still be written: %v", statErr)
	}

	var okWarn strings.Builder
	okOut := filepath.Join(dir, "good")
	if err := writeArtifactFiles(&okWarn, okOut, []generate.File{{Path: "bot.py", Content: []byte("x  =  1\n")}}); err != nil {
		t.Fatalf("valid Python must compile: %v", err)
	}
	written, readErr := os.ReadFile(filepath.Join(okOut, "bot.py"))
	if readErr != nil || string(written) != "x = 1\n" {
		t.Fatalf("valid Python must be written formatted: %q %v", written, readErr)
	}
}
