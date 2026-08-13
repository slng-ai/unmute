package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// preservedPatterns names the files a rewrite of a build directory must not
// destroy. `build/<target>/` is disposable by design (constitution: artifacts
// are regenerated, never edited), and these are the two deliberate exceptions,
// both written there by somebody other than the compiler:
//
//   - `.env` holds the operator's real values. Long-standing behaviour.
//   - `livekit*.toml` is written by LiveKit Cloud on the first deploy and names
//     the project subdomain and the assigned agent ID. Losing it breaks
//     `lk agent deploy` and sends people back to `lk agent create`, which
//     registers a *second* billable agent and splits dispatch between two
//     versions. The glob covers the platform's per-region naming
//     (`livekit.us-east.toml`) and is safe precisely because the emitter never
//     produces a file matching it.
var preservedPatterns = []string{".env", "livekit*.toml"}

type preservedFile struct {
	path    string
	content []byte
	mode    os.FileMode
}

// writeArtifactFiles writes a code-target project into a clean build dir,
// applying a best-effort `ruff format` pass to emitted Python (SPEC C1/V2): the
// generator stays template-only, the write path polishes layout. ruff is
// optional — absent, the (already valid, F-clean) source is written unformatted
// and a single warning goes to warn.
func writeArtifactFiles(warn io.Writer, outDir string, files []generate.File) (err error) {
	preserved, err := readPreserved(outDir)
	if err != nil {
		return err
	}
	if len(preserved) > 0 {
		defer func() { err = errors.Join(err, restorePreserved(outDir, preserved)) }()
	}
	if err := os.RemoveAll(outDir); err != nil {
		return err
	}
	ruffMissing := false
	var invalidPython, ruffTrouble []string
	for _, file := range files {
		path := filepath.Join(outDir, file.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		content := file.Content
		if strings.HasSuffix(file.Path, ".py") {
			formatted, found, unparseable, failure := formatPython(content)
			content, ruffMissing = formatted, ruffMissing || !found
			switch {
			case unparseable:
				invalidPython = append(invalidPython, fmt.Sprintf("%s: %v", file.Path, failure))
			case failure != nil:
				ruffTrouble = append(ruffTrouble, fmt.Sprintf("%s: %v", file.Path, failure))
			}
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return err
		}
	}
	if ruffMissing && warn != nil {
		fmt.Fprintln(warn, "warning: ruff not found on PATH; emitted Python left unformatted (install ruff for formatted output)")
	}
	// ruff ran and could not format, but not because the Python was bad. That is
	// an environment problem, so it is reported and compile carries on with the
	// unformatted source: failing here would reject valid output for a reason
	// that has nothing to do with it.
	if len(ruffTrouble) > 0 && warn != nil {
		fmt.Fprintf(warn, "warning: ruff could not format %s; emitted Python left unformatted\n", strings.Join(ruffTrouble, "; "))
	}
	// The emitted Python does not parse. compile's whole job is to produce a
	// runnable project, so reporting success here would be the silent downgrade
	// D3 forbids. The files stay on disk deliberately, so the broken output can
	// be read.
	if len(invalidPython) > 0 {
		return fmt.Errorf("emitted Python is not valid: %s", strings.Join(invalidPython, "; "))
	}
	return nil
}

// readPreserved snapshots the preservedPatterns matches in a build directory
// before it is rewritten. A match that is not a regular file fails loud rather
// than being silently replaced.
func readPreserved(outDir string) ([]preservedFile, error) {
	var preserved []preservedFile
	for _, pattern := range preservedPatterns {
		matches, err := filepath.Glob(filepath.Join(outDir, pattern))
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", pattern, err)
		}
		for _, path := range matches {
			info, err := os.Lstat(path)
			if err != nil {
				return nil, fmt.Errorf("inspect %s: %w", path, err)
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("preserve %s: not a regular file", path)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("preserve %s: %w", path, err)
			}
			preserved = append(preserved, preservedFile{path: path, content: content, mode: info.Mode().Perm()})
		}
	}
	return preserved, nil
}

func restorePreserved(outDir string, preserved []preservedFile) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("restore %s: %w", outDir, err)
	}
	var errs []error
	for _, file := range preserved {
		if err := os.WriteFile(file.path, file.content, file.mode); err != nil {
			errs = append(errs, fmt.Errorf("restore %s: %w", file.path, err))
			continue
		}
		if err := os.Chmod(file.path, file.mode); err != nil {
			errs = append(errs, fmt.Errorf("restore mode for %s: %w", file.path, err))
		}
	}
	return errors.Join(errs...)
}

// unparseableMarker is how ruff reports source it could not parse:
// "error: Failed to parse at 1:12: Expected a parameter ...".
//
// It is matched with the "error: " prefix on purpose. A broken ruff config
// reports "ruff failed" followed by "  Cause: Failed to parse /path/ruff.toml",
// which contains the same three words about a TOML file rather than about the
// emitted Python. Both exit 2, so the exit code cannot tell them apart.
const unparseableMarker = "error: Failed to parse"

// ansiEscape matches the SGR sequences ruff wraps its diagnostics in when
// colour is on. They land in the middle of the marker
// ("\x1b[1;31merror\x1b[0m\x1b[1m:\x1b[0m \x1b[1mFailed to parse at \x1b[0m"),
// so a literal match would miss and invalid Python would be waved through as a
// mere environment problem.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// formatPython runs `ruff format` on Python source, best-effort.
//
// Three outcomes, and the distinction is the point. found=false means ruff is
// not installed, so the source is written unformatted. unparseable=true means
// ruff parsed nothing because the generator emitted Python that is not valid,
// which is a real defect the caller turns into a failed compile. Any other error
// (a broken binary, an OOM, a future ruff whose CLI moved) returns a non-nil
// failure with unparseable=false: it is reported, but it must never be blamed on
// the generated code, because valid output would then fail to compile for a
// reason that has nothing to do with it.
//
// --isolated stops ruff walking up the filesystem for configuration. That
// removes the whole class of failures caused by an unrelated pyproject.toml or
// ruff.toml above the working directory, and it makes the emitted formatting
// depend only on the compiler rather than on where the user happened to run it.
func formatPython(src []byte) (out []byte, found, unparseable bool, failure error) {
	ruff, err := exec.LookPath("ruff")
	if err != nil {
		return src, false, false, nil
	}
	// --color never because the classifier reads this text. ruff colours its
	// diagnostics when FORCE_COLOR or CLICOLOR_FORCE is set even with stderr
	// piped, and NO_COLOR does not override those, so without this a developer
	// or CI job that exports FORCE_COLOR would turn every parse failure into an
	// unrecognised one and compile would report success on Python that cannot
	// even be read.
	cmd := exec.Command(ruff, "format", "--isolated", "--color", "never", "-")
	cmd.Stdin = bytes.NewReader(src)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	formatted, err := cmd.Output()
	if err != nil {
		// Stripped as well as suppressed: --color handles the ruff we know, and
		// stripping keeps the classifier honest if some future version or
		// another mechanism colours the output anyway.
		detail := strings.TrimSpace(ansiEscape.ReplaceAllString(stderr.String(), ""))
		if detail == "" {
			detail = err.Error()
		}
		return src, true, strings.Contains(detail, unparseableMarker), errors.New(detail)
	}
	return formatted, true, false, nil
}
