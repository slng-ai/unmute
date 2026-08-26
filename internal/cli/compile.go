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

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
	"github.com/spf13/cobra"
)

func newCompileCmd() *cobra.Command {
	var names []string
	cmd := &cobra.Command{
		Use:   "compile [package-dir]",
		Short: "Compile a v1 agent package to its resolved target artifacts.",
		Long: "Compile a v1 agent package to its resolved target artifacts.\n\n" +
			"With no package-dir, the package is the current directory, so you can " +
			"cd into an agent and run this with no arguments.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := packageDir(cmd, args)
			if err != nil {
				return err
			}
			return runCompile(cmd, dir, names)
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
	printHeader(cmd.OutOrStdout(), "compile "+displayDir(dir))
	caps := target.Default()
	for _, resolved := range targets {
		artifact, err := generate.Generate(agent, resolved, caps)
		if err != nil {
			return fmt.Errorf("compile %s: %w", dir, err)
		}
		for _, warning := range artifact.Notes.Warnings {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: %s\n", resolved.Name, warning)
		}
		// Both kinds write the same way. They are still two cases, because the
		// switch had no default arm: an artifact kind nobody handled produced a
		// complete file list in memory, wrote none of it, and reported success.
		// The default arm is the point of this switch, not the cases.
		switch artifact.Kind {
		case generate.CodeTarget, generate.BodyTarget:
			outDir := filepath.Join(dir, "build", resolved.Name)
			if err := writeArtifactFiles(cmd.ErrOrStderr(), outDir, artifact.Files); err != nil {
				return fmt.Errorf("compile %s: %w", dir, err)
			}
			for _, file := range artifact.Files {
				fmt.Fprintln(cmd.OutOrStdout(), "generated", filepath.Join(outDir, file.Path))
			}
		default:
			return fmt.Errorf("compile %s: target %q produced artifact kind %q, which this command does not know how to write",
				dir, resolved.Name, artifact.Kind)
		}
		printContract(cmd.OutOrStdout(), resolved.Name, resolved.Provider, artifact.Notes)
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

// printContract prints every resolved binding/param and derived sizing line.
// Most provider values are relayed verbatim. The narrow LiveKit Responses mode
// is a compiler directive, so its consumed/transformed params are named here.
func printContract(out io.Writer, name string, provider ir.Provider, notes generate.GenerateReport) {
	for _, fb := range notes.ForwardedBindings {
		role := fb.Role
		if fb.Profile != "" {
			role += "." + fb.Profile
		}
		responses := provider == ir.ProviderLiveKit && fb.Role == "reason" &&
			(fb.Binding.Provider == "" || fb.Binding.Provider == "openai") &&
			fb.Binding.Params["api"] == "responses"
		if responses {
			fmt.Fprintf(out, "%s: binding %s %s (OpenAI Responses mode; api is consumed and reasoning_effort is lowered when present)\n", name, role, bindingSummary(fb.Binding))
			for _, p := range fb.Params {
				suffix := " (forwarded as-is, not validated)"
				switch p.Name {
				case "api":
					suffix = " (compiler directive)"
				case "reasoning_effort":
					suffix = " (lowered to reasoning.effort)"
				}
				fmt.Fprintf(out, "%s:   param %s=%v%s\n", name, p.Name, p.Value, suffix)
			}
			continue
		}
		// The SLNG Context Router consumes two authored things rather than
		// forwarding them, so neither is silent: the region becomes the base URL,
		// and the upstream block becomes the inline configuration the request
		// body carries. The upstream line names the URL and the *name* of each
		// credential variable, never a value (FR-004, FR-036).
		if fb.Role == "reason" && fb.Binding.Router() {
			if base, ok := target.SlngRouterBaseURL(routerRegion(fb.Binding)); ok {
				fmt.Fprintf(out, "%s: binding %s %s (SLNG Context Router; world_part_override is consumed into base_url=%s)\n",
					name, role, bindingSummary(fb.Binding), base)
				for _, p := range fb.Params {
					suffix := ""
					if p.Name == "world_part_override" {
						suffix = " (consumed as the router base URL)"
					}
					fmt.Fprintf(out, "%s:   param %s=%v%s\n", name, p.Name, p.Value, suffix)
				}
				fmt.Fprintf(out, "%s:   upstream %s (sent inline in the request body)\n", name, upstreamSummary(fb.Binding))
				continue
			}
		}
		fmt.Fprintf(out, "%s: binding %s %s (forwarded as-is, not validated)\n", name, role, bindingSummary(fb.Binding))
		for _, p := range fb.Params {
			fmt.Fprintf(out, "%s:   param %s=%v\n", name, p.Name, p.Value)
		}
	}
	for _, s := range notes.Sizing {
		fmt.Fprintf(out, "%s: sizing %s=%s [%s] (%s)\n", name, s.Metric, s.Value, s.Status, s.Basis)
	}
	// Driver notes are facts about what the artifact will do that no binding or
	// sizing line covers: today, what each knowledge base carries (FR-015). The
	// target name prefixes them like every other line here, because two targets
	// each print their own and an unprefixed pair would be indistinguishable.
	for _, note := range notes.Notes {
		fmt.Fprintf(out, "%s: %s\n", name, note)
	}
}

// routerRegion reads the authored router region off a binding's params.
func routerRegion(b ir.Binding) string {
	region, _ := b.Params["world_part_override"].(string)
	return region
}

// upstreamSummary names the resolved upstream, its URL, and each credential
// variable by name. A value never appears: the compiler checks that a credential
// field is present, not that it is right, and the report is one of the surfaces
// the constitution keeps free of secret values.
func upstreamSummary(b ir.Binding) string {
	if b.Upstream == nil {
		return "unresolved"
	}
	fields, ok := target.SlngResolveUpstream(b.Upstream.Provider, b.Upstream.Fields())
	if !ok {
		return b.Upstream.Provider
	}
	parts := []string{b.Upstream.Provider}
	for _, field := range fields {
		switch {
		case field.Wire == "provider":
			continue
		case field.Env:
			parts = append(parts, field.Wire+"="+field.Value+" (env)")
		default:
			parts = append(parts, field.Wire+"="+field.Value)
		}
	}
	return strings.Join(parts, " ")
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
