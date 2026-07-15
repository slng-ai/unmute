package cli

import (
	"fmt"

	"github.com/slng/unmute/internal/generate"
	"github.com/slng/unmute/internal/target"
	"github.com/spf13/cobra"
)

func newApplyCmd() *cobra.Command {
	var names []string
	cmd := &cobra.Command{
		Use:   "apply <package-dir>",
		Short: "Apply a v1 package to its managed (config-plane) targets.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApply(cmd, args[0], names)
		},
	}
	cmd.Flags().StringSliceVar(&names, "target", nil, "target instance name (repeatable; default: all)")
	return cmd
}

func runApply(cmd *cobra.Command, dir string, names []string) error {
	agent, targets, err := loadPackage(dir, names)
	if err != nil {
		return fmt.Errorf("apply %s: %w", dir, err)
	}
	caps := target.Default()
	for _, resolved := range targets {
		artifact, err := generate.Generate(agent, resolved, caps)
		if err != nil {
			return fmt.Errorf("apply %s: %w", dir, err)
		}
		if artifact.Kind != generate.ManagedTarget || artifact.Apply == nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s: code target — use `unmute compile`\n", resolved.Name)
			continue
		}
		// The ordered, branch-aware ApplyPlan executor lands with the first
		// managed driver (Vapi/ElevenLabs); until then, surface the plan.
		for i, step := range artifact.Apply.Steps {
			fmt.Fprintf(cmd.OutOrStdout(), "%s step %d: %s %s\n", resolved.Name, i+1, step.Method, step.Endpoint)
		}
	}
	return nil
}
