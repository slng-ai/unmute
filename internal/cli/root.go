package cli

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	targetcap "github.com/slng-ai/unmute/internal/target"
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

// supportedFrameworks is the one line that tells an author what their installed
// unmute supports, read from the recorded window rather than restated here. The
// version is a property of the binary, so `--version` is where it belongs: a
// package that fails on one unmute and passes on another differs by this line.
func supportedFrameworks() string {
	windows := targetcap.Windows()
	if len(windows) == 0 {
		return ""
	}
	providers := slices.Sorted(maps.Keys(windows))
	// Ceilings are normally verified together, so one trailing date reads best.
	// If they ever diverge, each framework carries its own rather than letting
	// one framework's date stand in for another's.
	dates := map[string]bool{}
	for _, win := range windows {
		dates[win.Verified] = true
	}
	shared := len(dates) == 1
	parts := make([]string, 0, len(windows))
	for _, provider := range providers {
		win := windows[provider]
		part := fmt.Sprintf("%s %s", targetcap.FrameworkPackage(provider), win.Ceiling)
		if !shared {
			part += fmt.Sprintf(" (verified %s)", win.Verified)
		}
		parts = append(parts, part)
	}
	line := "supported frameworks: " + strings.Join(parts, ", ")
	if shared {
		line += fmt.Sprintf(" (verified %s)", windows[providers[0]].Verified)
	}
	return line
}

// Execute builds the root, runs it, prints any error once to stderr, and
// returns the process exit code. os.Exit lives only in main.go.
func Execute(version string) int {
	root := newRootCmd()
	root.Version = version
	// The first line is a public CLI contract, so the frameworks line is appended
	// after it rather than folded into it.
	if line := supportedFrameworks(); line != "" {
		root.SetVersionTemplate("{{.Name}} version {{.Version}}\n" + line + "\n")
	}
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "unmute:", err)
		return 1
	}
	return 0
}
