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
	fmt.Fprintln(cmd.OutOrStdout(), "TARGET\tPROVIDER\tRESULT")
	for _, row := range report.PerTarget {
		status := "pass"
		if len(row.Errors) > 0 {
			status = "fail"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", row.Name, row.Provider, status)
		for _, warning := range row.Warnings {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: %s\n", row.Name, warning)
		}
		for _, validationError := range row.Errors {
			fmt.Fprintf(cmd.ErrOrStderr(), "error: %s: %s\n", row.Name, validationError)
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
