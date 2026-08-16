package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// packageDir resolves the package directory from an optional positional
// argument. With no argument it is the current directory, so an author can
// `cd` into a scaffolded package and work there.
//
// The current directory must itself hold agent.yaml; no parent is searched.
// That is deliberate and load-bearing: `compile` writes build/<target>/ inside
// the package, so an upward search would let a run from inside build/livekit/
// rewrite the directory the author is standing in.
//
// cmd supplies the command's own name for the failure message, which is why
// this takes a *cobra.Command it otherwise would not need.
func packageDir(cmd *cobra.Command, args []string) (string, error) {
	// An explicit argument is returned untouched, so its errors stay exactly
	// as they were before this feature (contract C9).
	if len(args) == 1 {
		return args[0], nil
	}
	if _, err := os.Stat(filepath.Join(".", "agent.yaml")); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%s: reading agent.yaml: %w", cmd.Name(), err)
		}
		where, absErr := filepath.Abs(".")
		if absErr != nil {
			// Never promise an absolute path and then print a relative one.
			return "", fmt.Errorf("%s: resolving the current directory: %w", cmd.Name(), absErr)
		}
		// CommandPath is already "unmute validate"; UseLine would repeat the
		// binary name and append "[flags]", which is noise here.
		return "", fmt.Errorf(
			"%s: no agent.yaml in %s\n"+
				"  run `%s` from inside an agent package, or name one: `%s <package-dir>`",
			cmd.Name(), where, cmd.CommandPath(), cmd.CommandPath())
	}
	return ".", nil
}

// displayDir is what the header shows. Without it a zero-argument run prints
// `validate .`, which reads like a mistake on the one screen this feature
// exists to improve. printHeader only writes to a TTY, so no captured-output
// test sees this; its own test calls it directly.
func displayDir(dir string) string {
	if dir != "." {
		return dir
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return filepath.Base(abs)
}
