package cli

import (
	"fmt"
	"os"

	"github.com/slng-ai/unmute/internal/style"
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
	root.AddCommand(newInitCmd(), newCompileCmd(), newDeployCmd(), newDevCmd(), newValidateCmd(), newResourcesCmd(), newSkillCmd())
	return root
}

func shouldRunConsole(in, out any) bool {
	return isTTY(in) && isTTY(out)
}

// Execute builds the root, runs it, prints any error once to stderr, and
// returns the process exit code. os.Exit lives only in main.go.
func Execute(version string) int {
	root := newRootCmd()
	root.Version = version
	if err := root.Execute(); err != nil {
		// The prefix carries the colour, not the message: a wall of red is
		// harder to read than plain text with a red marker on it, and the
		// message is the part somebody has to act on.
		//
		// The prefix itself stays, even though a run header two lines up has
		// already said `unmute` and named the command. This is the one print
		// site that cannot know whether that header ran: it never does for a
		// pipe or a CI log, and there the program name and the wrapped
		// `<command> <package>:` chain are the only things identifying whose
		// failure this is. Saying it twice on a terminal is the cheaper half of
		// that trade. Bound to os.Stderr, so a redirected stream stays plain
		// even while os.Stdout is still a terminal.
		fmt.Fprintln(os.Stderr, style.For(os.Stderr).Failed("unmute:"), err)
		return 1
	}
	return 0
}
