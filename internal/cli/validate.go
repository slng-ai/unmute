package cli

import (
	"fmt"
	"maps"
	"slices"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	var names []string
	cmd := &cobra.Command{
		Use:   "validate <package-dir>",
		Short: "Validate a v1 agent package against its targets.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidate(cmd, args[0], names)
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
	printHeader(out, "validate "+dir)
	u := newUI(out)
	for _, row := range report.PerTarget {
		status := u.ok("✓")
		if len(row.Errors) > 0 {
			status = u.fail("✗")
		}
		fmt.Fprintf(out, "%s %s (%s)\n", status, u.accent(row.Name), row.Provider)
	}
	wroteWarnings := false
	for _, row := range report.PerTarget {
		for _, warning := range row.Warnings {
			if !wroteWarnings {
				fmt.Fprintln(cmd.ErrOrStderr(), "\nWarnings:")
				wroteWarnings = true
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "  %s: %s\n", row.Name, warning)
		}
	}
	wroteErrors := false
	for _, row := range report.PerTarget {
		for _, validationError := range row.Errors {
			if !wroteErrors {
				fmt.Fprintln(cmd.ErrOrStderr(), "\nErrors:")
				wroteErrors = true
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "  %s: %s\n", row.Name, validationError)
		}
	}
	if validateErr != nil {
		return fmt.Errorf("validate %s: %w", dir, validateErr)
	}
	return nil
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
