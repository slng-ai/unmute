package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/style"
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
	out := cmd.OutOrStdout()
	printHeader(out, "compile "+displayDir(dir))
	agent, targets, err := loadPackage(dir, names)
	if err != nil {
		return fmt.Errorf("compile %s: %w", dir, err)
	}
	if len(targets) == 0 {
		return fmt.Errorf("compile %s: no targets selected", dir)
	}
	caps := target.Default()
	u := style.For(out)
	for _, resolved := range targets {
		artifact, err := generate.Generate(agent, resolved, caps)
		if err != nil {
			return fmt.Errorf("compile %s: %w", dir, err)
		}
		// Every warning left is a package problem with a fix in it, so these are
		// worth the two lines they cost. What used to print here as well - each
		// forwarded binding, each derived worker count, the resolved telephony
		// route - is written to compile-report.json in the same directory as the
		// rest of the artifact, which is where it belongs: it described the
		// output, and it is now next to the output.
		for _, warning := range artifact.Notes.Warnings {
			warnf(cmd.ErrOrStderr(), "%s: %s\n", resolved.Name, warning)
		}
		// Both kinds write the same way. They are still two cases, because the
		// switch had no default arm: an artifact kind nobody handled produced a
		// complete file list in memory, wrote none of it, and reported success.
		// The default arm is the point of this switch, not the cases.
		switch artifact.Kind {
		case generate.CodeTarget, generate.BodyTarget:
			outDir := filepath.Join(dir, "build", resolved.Name)
			copied, err := writeTargetBuild(cmd.ErrOrStderr(), dir, resolved.Name, artifact.Files)
			if err != nil {
				return fmt.Errorf("compile %s: %w", dir, err)
			}
			for _, file := range artifact.Files {
				fmt.Fprintln(out, u.Dim("generated"), dimPath(u, filepath.Join(outDir, file.Path)))
			}
			for _, path := range copied {
				fmt.Fprintln(out, u.Dim("copied"), dimPath(u, path))
			}
		default:
			return fmt.Errorf("compile %s: target %q produced artifact kind %q, which this command does not know how to write",
				dir, resolved.Name, artifact.Kind)
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

// preservedPatterns names the files a rewrite of a build directory must not
// destroy. `build/<target>/` is disposable by design (constitution: artifacts
// are regenerated, never edited), and these are the two deliberate exceptions,
// both written there by somebody other than the compiler:
//
//   - `.env` holds the operator's real values. Long-standing behaviour.
//   - `samples/*.json` are the tool samples `unmute deploy --run-samples` needs.
//     The push tool reads them from `<compiled>/samples/`, which is inside this
//     directory, so without this row writing one and re-running deploy would
//     delete it and report the same `sample_missing` blocker forever.
//   - `livekit*.toml` is written by LiveKit Cloud on the first deploy and names
//     the project subdomain and the assigned agent ID. Losing it breaks
//     `lk agent deploy` and sends people back to `lk agent create`, which
//     registers a *second* billable agent and splits dispatch between two
//     versions. The glob covers the platform's per-region naming
//     (`livekit.us-east.toml`) and is safe precisely because the emitter never
//     produces a file matching it.
var preservedPatterns = []string{".env", "livekit*.toml", filepath.Join("samples", "*.json")}

// writeTargetBuild writes one target's generated files to build/<target>/,
// then copies the package's hosting/<target>/ folder over them. It returns the
// paths it copied.
func writeTargetBuild(warn io.Writer, pkgDir, targetName string, files []generate.File) ([]string, error) {
	outDir := filepath.Join(pkgDir, "build", targetName)
	hosting, err := readHosting(pkgDir, targetName, files)
	if err != nil {
		return nil, err
	}
	if err := writeArtifactFiles(warn, outDir, files); err != nil {
		return nil, err
	}
	if err := restorePreserved(outDir, hosting); err != nil {
		return nil, err
	}
	copied := make([]string, 0, len(hosting))
	for _, file := range hosting {
		copied = append(copied, file.path)
	}
	return copied, nil
}

// readHosting reads <package>/hosting/<target>/, the files an author keeps
// next to the generated ones: a host's config such as render.yaml or fly.toml.
// build/<target>/ is deleted on every compile, so a file put there by hand is
// lost, and this folder is where such a file lives instead. It is read before
// anything is deleted, so a refusal leaves the old build as it was.
//
// A hosting file never replaces a generated file or a kept one (.env and the
// rest of preservedPatterns): that would change what the build runs without
// the compiler knowing. It is copied byte for byte, never formatted, and it is
// not part of the artifact id, because the app never reads it.
func readHosting(pkgDir, targetName string, generated []generate.File) ([]preservedFile, error) {
	root := filepath.Join(pkgDir, "hosting", targetName)
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	owned := make(map[string]bool, len(generated))
	for _, file := range generated {
		owned[filepath.Clean(file.Path)] = true
	}
	outDir := filepath.Join(pkgDir, "build", targetName)
	var hosting []preservedFile
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		shown := filepath.Join("hosting", targetName, rel)
		if !entry.Type().IsRegular() {
			return fmt.Errorf("%s: not a regular file; copy the file itself into hosting/", shown)
		}
		kept := slices.ContainsFunc(preservedPatterns, func(pattern string) bool {
			matched, _ := filepath.Match(pattern, rel)
			return matched
		})
		if owned[rel] || kept {
			return fmt.Errorf("%s: build/%s/%s is a file unmute writes or keeps; rename the hosting file", shown, targetName, rel)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hosting = append(hosting, preservedFile{path: filepath.Join(outDir, rel), content: content, mode: info.Mode().Perm()})
		return nil
	})
	return hosting, err
}

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
		if strings.HasSuffix(file.Path, ".py") && !file.Verbatim {
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
		warnf(warn, "ruff not found on PATH; emitted Python left unformatted (install ruff for formatted output)\n")
	}
	// ruff ran and could not format, but not because the Python was bad. That is
	// an environment problem, so it is reported and compile carries on with the
	// unformatted source: failing here would reject valid output for a reason
	// that has nothing to do with it.
	if len(ruffTrouble) > 0 && warn != nil {
		warnf(warn, "ruff could not format %s; emitted Python left unformatted\n", strings.Join(ruffTrouble, "; "))
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
		// A preserved file can sit in a subdirectory (samples/), and RemoveAll took
		// that directory with it.
		if err := os.MkdirAll(filepath.Dir(file.path), 0o755); err != nil {
			errs = append(errs, fmt.Errorf("restore %s: %w", filepath.Dir(file.path), err))
			continue
		}
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
