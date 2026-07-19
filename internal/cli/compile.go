package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	if len(targets) == 0 {
		return fmt.Errorf("compile %s: no targets selected", dir)
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
		printContract(cmd.OutOrStdout(), resolved.Name, artifact.Notes)
	}
	return nil
}

// printContract prints every forwarded binding/param and derived sizing line.
// SCHEMA.md §6.2 rule 6 and §5.1 call this report "the contract": what was sent
// is always inspectable, and no value here is validated (relayed verbatim).
func printContract(out io.Writer, name string, notes generate.GenerateReport) {
	for _, fb := range notes.ForwardedBindings {
		role := fb.Role
		if fb.Profile != "" {
			role += "." + fb.Profile
		}
		fmt.Fprintf(out, "%s: binding %s %s (forwarded as-is, not validated)\n", name, role, bindingSummary(fb.Binding))
		for _, p := range fb.Params {
			fmt.Fprintf(out, "%s:   param %s=%v\n", name, p.Name, p.Value)
		}
	}
	for _, s := range notes.Sizing {
		fmt.Fprintf(out, "%s: sizing %s=%s [%s] (%s)\n", name, s.Metric, s.Value, s.Status, s.Basis)
	}
}

// bindingSummary renders the forwarded identity fields of a binding for stdout.
// Placement is a derived routing fact, not a forwarded value, so it stays out of
// the "forwarded as-is" line (N15).
func bindingSummary(b ir.Binding) string {
	var parts []string
	for _, kv := range []struct{ k, v string }{
		{"provider", b.Provider}, {"model", b.Model}, {"voice", b.Voice},
		{"voice_id", b.VoiceID}, {"endpoint_env", b.EndpointEnv},
	} {
		if kv.v != "" {
			parts = append(parts, kv.k+"="+kv.v)
		}
	}
	return strings.Join(parts, " ")
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
