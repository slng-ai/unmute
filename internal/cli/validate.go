package cli

import (
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/style"
	"github.com/slng-ai/unmute/internal/target"
	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	var names []string
	cmd := &cobra.Command{
		Use:   "validate [package-dir]",
		Short: "Validate a v1 agent package against its targets.",
		Long: "Validate a v1 agent package against its targets.\n\n" +
			"With no package-dir, the package is the current directory, so you can " +
			"cd into an agent and run this with no arguments.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := packageDir(cmd, args)
			if err != nil {
				return err
			}
			return runValidate(cmd, dir, names)
		},
	}
	cmd.Flags().StringSliceVar(&names, "target", nil, "target instance name (repeatable)")
	return cmd
}

func runValidate(cmd *cobra.Command, dir string, names []string) error {
	pkg, err := spec.Load(dir)
	if err != nil {
		return fmt.Errorf("validate %s: load: %w", dir, err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		return fmt.Errorf("validate %s: build: %w", dir, err)
	}
	targets, err := validationTargets(agent, names)
	if err != nil {
		return fmt.Errorf("validate %s: %w", dir, err)
	}
	report, validateErr := ir.Validate(agent, targets, target.Default())
	out := cmd.OutOrStdout()
	printHeader(out, "validate "+displayDir(dir))
	printValidationReport(out, cmd.ErrOrStderr(), report)
	if validateErr != nil {
		return fmt.Errorf("validate %s: %w", dir, validateErr)
	}
	return nil
}

// printValidationReport writes the per-target result rows to out and every
// prerequisite, warning and error to errOut.
//
// `deploy` validates before it pushes and prints the same report, so the format
// has one owner rather than two renderers that agree by hand until they do not.
func printValidationReport(out, errOut io.Writer, report ir.ValidateReport) {
	u := style.For(out)
	for _, row := range report.PerTarget {
		status := u.Ok("✓")
		if len(row.Errors) > 0 {
			status = u.Failed("✗")
		}
		fmt.Fprintf(out, "%s %s (%s)\n", status, u.Accent(row.Name), row.Provider)
	}
	// Prerequisites first, because they are the thing an author has to go and ask
	// someone else for, and the lead time is theirs, not ours. Exit code stays 0:
	// the package is correct, the account may not be provisioned yet.
	wrotePrerequisites := false
	for _, row := range report.PerTarget {
		for _, prerequisite := range row.Prerequisites {
			if !wrotePrerequisites {
				fmt.Fprintln(errOut, "\nSetup prerequisites:")
				wrotePrerequisites = true
			}
			fmt.Fprintf(errOut, "  %s: %s: %s\n    %s (verified %s)\n",
				row.Name, prerequisite.Name, prerequisite.Summary,
				prerequisite.Docs, prerequisite.Verified)
		}
	}
	wroteWarnings := false
	for _, row := range report.PerTarget {
		for _, warning := range row.Warnings {
			if !wroteWarnings {
				fmt.Fprintln(errOut, "\nWarnings:")
				wroteWarnings = true
			}
			fmt.Fprintf(errOut, "  %s: %s\n", row.Name, warning)
		}
	}
	wroteErrors := false
	for _, row := range report.PerTarget {
		for _, validationError := range row.Errors {
			if !wroteErrors {
				fmt.Fprintln(errOut, "\nErrors:")
				wroteErrors = true
			}
			fmt.Fprintf(errOut, "  %s: %s\n", row.Name, validationError)
		}
	}
}

func validationTargets(agent *ir.Agent, names []string) ([]ir.Target, error) {
	if len(names) == 0 {
		names = slices.Sorted(maps.Keys(agent.Targets))
	}
	seen := make(map[string]bool)
	resolved := make([]ir.Target, 0, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		value, ok := agent.Targets[name]
		if !ok {
			return nil, fmt.Errorf("target instance %q is not declared", name)
		}
		seen[name] = true
		resolved = append(resolved, value)
	}
	return resolved, nil
}
