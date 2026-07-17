package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// newRootCmd builds a fresh command tree. No package-level rootCmd: a new tree
// per call keeps flag state isolated between tests (and between Execute calls).
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "unmute",
		Short:         "Author-once, portable voice agents.",
		SilenceUsage:  true, // a failed command must not reprint --help
		SilenceErrors: true, // Execute owns error printing
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if shouldRunConsole(cmd.InOrStdin(), cmd.OutOrStdout()) {
				return runConsole(cmd, false)
			}
			return cmd.Help()
		},
	}
	root.AddCommand(newInitCmd(), newCompileCmd(), newApplyCmd(), newDevCmd(), newValidateCmd())
	return root
}

func shouldRunConsole(in, out any) bool {
	return isCharDevice(in) && isCharDevice(out)
}

// Execute builds the root, runs it, prints any error once to stderr, and
// returns the process exit code. os.Exit lives only in main.go.
func Execute(version string) int {
	root := newRootCmd()
	root.Version = version
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "unmute:", err)
		return 1
	}
	return 0
}
