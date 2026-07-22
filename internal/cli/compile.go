package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	printHeader(cmd.OutOrStdout(), "compile "+dir)
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
			if err := writeArtifactFiles(cmd.ErrOrStderr(), outDir, artifact.Files); err != nil {
				return fmt.Errorf("compile %s: %w", dir, err)
			}
			for _, file := range artifact.Files {
				fmt.Fprintln(cmd.OutOrStdout(), "generated", filepath.Join(outDir, file.Path))
			}
		case generate.ManagedTarget:
			fmt.Fprintf(cmd.OutOrStdout(), "%s: managed target — run `unmute apply %s --target %s`\n", resolved.Name, dir, resolved.Name)
		}
		printContract(cmd.OutOrStdout(), resolved.Name, artifact.Notes)
		printTelephonyPlan(cmd.OutOrStdout(), resolved.Name, artifact.Telephony)
	}
	return nil
}

func printTelephonyPlan(out io.Writer, name string, plan *generate.TelephonyRuntimePlan) {
	if plan == nil {
		return
	}
	fmt.Fprintf(out, "%s: telephony route provider=%s transport=%s carrier=%s coordination=%s admission=%s\n",
		name, plan.Route.Provider, plan.Route.Transport, plan.Route.Carrier, plan.Coordination, plan.AdmissionOwner)
	for _, endpoint := range plan.PublicEndpoints {
		fmt.Fprintf(out, "%s: telephony endpoint %s %s %s\n", name, endpoint.Name, endpoint.Method, endpoint.Path)
	}
	for _, env := range plan.RequiredEnv {
		fmt.Fprintf(out, "%s: telephony required env %s\n", name, env)
	}
	for _, evidence := range plan.Evidence {
		fmt.Fprintf(out, "%s: telephony evidence %s=%s docs=%s verified=%s smoke=%t\n",
			name, evidence.Feature, evidence.Tag, evidence.Docs, evidence.Verified, evidence.Smoke)
	}
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

// writeArtifactFiles writes a code-target project into a clean build dir,
// applying a best-effort `ruff format` pass to emitted Python (SPEC C1/V2): the
// generator stays template-only, the write path polishes layout. ruff is
// optional — absent, the (already valid, F-clean) source is written unformatted
// and a single warning goes to warn.
func writeArtifactFiles(warn io.Writer, outDir string, files []generate.File) (err error) {
	dotenvPath := filepath.Join(outDir, ".env")
	var dotenv []byte
	var dotenvMode os.FileMode
	if info, statErr := os.Lstat(dotenvPath); statErr == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("preserve %s: not a regular file", dotenvPath)
		}
		dotenv, err = os.ReadFile(dotenvPath)
		if err != nil {
			return fmt.Errorf("preserve %s: %w", dotenvPath, err)
		}
		dotenvMode = info.Mode().Perm()
		defer func() {
			if mkdirErr := os.MkdirAll(outDir, 0o755); mkdirErr != nil {
				err = errors.Join(err, fmt.Errorf("restore %s: %w", dotenvPath, mkdirErr))
				return
			}
			if writeErr := os.WriteFile(dotenvPath, dotenv, dotenvMode); writeErr != nil {
				err = errors.Join(err, fmt.Errorf("restore %s: %w", dotenvPath, writeErr))
				return
			}
			if chmodErr := os.Chmod(dotenvPath, dotenvMode); chmodErr != nil {
				err = errors.Join(err, fmt.Errorf("restore mode for %s: %w", dotenvPath, chmodErr))
			}
		}()
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("inspect %s: %w", dotenvPath, statErr)
	}
	if err := os.RemoveAll(outDir); err != nil {
		return err
	}
	ruffMissing := false
	for _, file := range files {
		path := filepath.Join(outDir, file.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		content := file.Content
		if strings.HasSuffix(file.Path, ".py") {
			formatted, found := formatPython(content)
			content, ruffMissing = formatted, ruffMissing || !found
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return err
		}
	}
	if ruffMissing && warn != nil {
		fmt.Fprintln(warn, "warning: ruff not found on PATH; emitted Python left unformatted (install ruff for formatted output)")
	}
	return nil
}

// formatPython runs `ruff format` on Python source, best-effort. Returns the
// input unchanged with found=false when ruff is not installed, and unchanged
// (found=true) if ruff errors — formatting never fails generation.
func formatPython(src []byte) (out []byte, found bool) {
	ruff, err := exec.LookPath("ruff")
	if err != nil {
		return src, false
	}
	cmd := exec.Command(ruff, "format", "-")
	cmd.Stdin = bytes.NewReader(src)
	formatted, err := cmd.Output()
	if err != nil {
		return src, true
	}
	return formatted, true
}
