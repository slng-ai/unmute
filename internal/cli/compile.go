package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/slng/unmute/internal/generate"
	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
	"github.com/spf13/cobra"
)

func newCompileCmd() *cobra.Command {
	var names []string
	cmd := &cobra.Command{
		Use:   "compile <package-dir>",
		Short: "Compile a v1 agent package to its resolved target artifacts.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompile(cmd, args[0], names)
		},
	}
	cmd.Flags().StringSliceVar(&names, "target", nil, "target instance name (repeatable; default: all)")
	return cmd
}

func runCompile(cmd *cobra.Command, dir string, names []string) error {
	agent, targets, err := loadPackage(dir, names)
	if err != nil {
		return fmt.Errorf("compile %s: %w", dir, err)
	}
	caps := target.Default()
	for _, resolved := range targets {
		artifact, err := generate.Generate(agent, resolved, caps)
		if err != nil {
			return fmt.Errorf("compile %s: %w", dir, err)
		}
		for _, warning := range artifact.Notes.Warnings {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: %s\n", resolved.Name, warning)
		}
		switch artifact.Kind {
		case generate.CodeTarget:
			outDir := filepath.Join(dir, "build", resolved.Name)
			if err := writeArtifactFiles(outDir, artifact.Files); err != nil {
				return fmt.Errorf("compile %s: %w", dir, err)
			}
			for _, file := range artifact.Files {
				fmt.Fprintln(cmd.OutOrStdout(), "generated", filepath.Join(outDir, file.Path))
			}
		case generate.ManagedTarget:
			fmt.Fprintf(cmd.OutOrStdout(), "%s: managed target — run `unmute apply %s --target %s`\n", resolved.Name, dir, resolved.Name)
		}
	}
	return nil
}

// loadPackage loads, builds, and selects target instances — the shared front of
// compile / apply / dev. With no names it selects every declared target.
func loadPackage(dir string, names []string) (*ir.Agent, []ir.Target, error) {
	pkg, err := spec.Load(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("load: %w", err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		return nil, nil, fmt.Errorf("build: %w", err)
	}
	targets, err := validationTargets(agent, names)
	if err != nil {
		return nil, nil, err
	}
	return agent, targets, nil
}

// writeArtifactFiles writes a code-target project into a clean build dir.
func writeArtifactFiles(outDir string, files []generate.File) error {
	if err := os.RemoveAll(outDir); err != nil {
		return err
	}
	for _, file := range files {
		path := filepath.Join(outDir, file.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, file.Content, 0o644); err != nil {
			return err
		}
	}
	return nil
}
