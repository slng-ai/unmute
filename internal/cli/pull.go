package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/style"
	"github.com/slng-ai/unmute/internal/target"
	"github.com/spf13/cobra"
)

// `unmute pull` is the only command that contacts the SLNG platform for a
// package's benefit, and that is the point rather than a limitation: it fetches
// once, writes into the package, and every later `validate`, `compile` and
// `make test` reads the committed copy with no credential and no network.
//
// It shells out to `voiceai tool get` like `deploy` and `resources` do, so
// nothing here opens a socket either.
//
// The shape is `skill install`'s, and for its stated reason: plan every file,
// refuse naming all the offending ones at once, then apply and report one line
// per file with its own outcome. A pull into a package that already has mirrors
// is exactly the situation that comment describes, where a silent no-op and a
// silent overwrite look identical from the outside.

func newPullCmd() *cobra.Command {
	var (
		force bool
		check bool
	)
	cmd := &cobra.Command{
		Use:   "pull [package-dir]",
		Short: "Fetch each SLNG-hosted tool's definition into the package.",
		Long: "Fetch the definition of every tool this package references with `slng:`, and\n" +
			"write it beside the tool file. Commit what it writes: the mirror is how a\n" +
			"hosted tool reaches livekit and pipecat, and the pin is how a later compile\n" +
			"knows the mirror is still the right one.\n\n" +
			"This is the only command that needs an SLNG credential. `validate` and\n" +
			"`compile` work offline, which is what lets CI build a package that names a\n" +
			"hosted tool.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPull(cmd, args, force, check)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Discard hand edits to a mirrored file")
	cmd.Flags().BoolVar(&check, "check", false, "Verify every pin against the organisation without writing; exit 1 on drift")
	return cmd
}

// pullAction is what happened to one file, in the words the report prints.
type pullAction string

const (
	pullWritten     pullAction = "written"
	pullOverwritten pullAction = "overwritten"
	pullUnchanged   pullAction = "unchanged"
	pullPinned      pullAction = "pinned"
)

// pullFile is one planned write.
type pullFile struct {
	Path    string
	Content []byte
	Action  pullAction
	// Edited marks a mirrored file that changed after it was written, which is a
	// refusal unless --force. The tool file is never marked: it is authored, and
	// a pull only ever touches its `hash:` line.
	Edited bool
}

func runPull(cmd *cobra.Command, args []string, force, check bool) error {
	dir, err := packageDir(cmd, args)
	if err != nil {
		return err
	}
	pkg, err := spec.Load(dir)
	if err != nil {
		return fmt.Errorf("pull %s: %w", displayDir(dir), err)
	}
	hosted := hostedToolNames(pkg)
	if len(hosted) == 0 {
		return fmt.Errorf("pull %s: no tool in this package has an `slng:` block, so there is nothing to fetch: "+
			"write `slng: {}` in a tools/<name>.yaml naming a tool your organisation hosts, then run this again", displayDir(dir))
	}

	bin, lookErr := exec.LookPath(deployPushBinary)
	if lookErr != nil {
		return fmt.Errorf("pull %s: %s", displayDir(dir), missingPushToolGuidance())
	}
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	env := packageEnv(dir, errOut)
	key, _ := deployCredential(env)
	if key == "" && os.Getenv(target.SlngPushCredentialEnv) == "" {
		// The one command that needs a credential says so plainly, and says
		// which commands do not, because that is the question an author asks
		// next.
		return fmt.Errorf("pull %s: no SLNG credential found: set %s, or run `%s`. "+
			"This is the only command that needs one; `validate` and `compile` work offline",
			displayDir(dir), target.SlngRouterKeyEnv, target.SlngLoginCommand)
	}
	readEnv := env
	if key != "" {
		readEnv = append(append([]string(nil), env...), target.SlngPushCredentialEnv+"="+key)
	}
	runner := newVoiceaiRunner(bin, readEnv, "")

	// The organisation before any finding. Two are reachable from one checkout
	// and are provisioned differently, so a listing from one says nothing about
	// the other and a reader who does not know which was read cannot act on any
	// of the rest.
	var account slngAccount
	if err := runner.read(target.SlngWhoami, &account); err != nil {
		return fmt.Errorf("pull %s: cannot tell which SLNG organisation this would read: %w", displayDir(dir), err)
	}
	printHeader(out, "pull "+displayDir(dir))
	fmt.Fprintf(out, "  slng: organisation %s\n\n", account)

	// Every tool is fetched before anything is written. A package whose second
	// tool cannot be fetched must not be left holding a mirror of its first:
	// `unmute init` refuses rather than half-writing, and the same reasoning
	// applies to a fetch that touches several files at once.
	mirrors := make(map[string]spec.Mirror, len(hosted))
	var listing []slngAccountTool
	listErr := runner.read(target.SlngToolList, &listing)
	for _, name := range hosted {
		mirror, err := readTool(runner, name)
		if err != nil || mirror.Name == "" {
			return fmt.Errorf("pull %s: %s", displayDir(dir), missingToolGuidance(name, listing, listErr, account, err))
		}
		if mirror.Source == "curated" {
			return fmt.Errorf("pull %s: `%s` is a capability SLNG curates, not a tool with a definition to mirror: "+
				"attach it with `builtin: %s` instead, which needs no pull", displayDir(dir), name, name)
		}
		mirror.Fetched = time.Now().UTC().Format(time.DateOnly)
		mirrors[name] = mirror
	}

	files, err := planPull(dir, hosted, mirrors)
	if err != nil {
		return fmt.Errorf("pull %s: %w", displayDir(dir), err)
	}
	secrets, secretFile, err := planSecrets(pkg, dir, mirrors)
	if err != nil {
		return fmt.Errorf("pull %s: %w", displayDir(dir), err)
	}

	if check {
		return reportPullCheck(cmd, displayDir(dir), files)
	}
	if edited := editedPaths(files); len(edited) > 0 && !force {
		return fmt.Errorf("pull %s: these mirrored files changed after they were written:\n  %s\n"+
			"a mirror is the platform's copy, so an edit here reaches nothing: run `unmute pull --force` to discard the edits, "+
			"or make the change in the SLNG dashboard and pull again", displayDir(dir), strings.Join(edited, "\n  "))
	}
	if secretFile != nil {
		files = append(files, *secretFile)
	}
	for _, file := range files {
		if file.Action == pullUnchanged {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, file.Path), file.Content, 0o644); err != nil {
			return fmt.Errorf("pull %s: writing %s: %w", displayDir(dir), file.Path, err)
		}
	}
	u := style.For(out)
	for _, file := range files {
		fmt.Fprintf(out, "  %-40s %s\n", dimPath(u, file.Path), u.Dim(string(file.Action)))
	}
	if len(secrets) > 0 {
		fmt.Fprintf(out, "\n  %s declares %s. Create %s with `unmute deploy`, which prompts for the value.\n",
			"agent.yaml", pluralSecrets(secrets), oneOrThem(secrets))
	}
	return nil
}

// hostedToolNames are the package's `slng:` tools, sorted, so the report and
// every refusal are in a stable order.
func hostedToolNames(pkg *spec.Package) []string {
	var names []string
	for name, tool := range pkg.Tools {
		if tool.Slng != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// planPull turns each fetched mirror into the files it writes, without writing
// any of them.
//
// Three per hosted tool at most: the sidecar, the module for a code tool, and
// the pin stamped into the authored tool file. The tool file is rewritten one
// line at a time rather than re-rendered, because every other line in it is the
// author's.
func planPull(dir string, hosted []string, mirrors map[string]spec.Mirror) ([]pullFile, error) {
	var files []pullFile
	for _, name := range hosted {
		mirror := mirrors[name]
		sidecarPath, modulePath := spec.MirrorPaths(name)

		sidecar, err := mirror.MirrorJSON()
		if err != nil {
			return nil, err
		}
		pinned := sidecar
		planned := []pullFile{fileAction(dir, sidecarPath, sidecar)}
		if mirror.Code != "" {
			module := []byte(spec.MirrorHeaderLines + mirror.Code)
			planned = append(planned, fileAction(dir, modulePath, module))
			pinned = append(append([]byte{}, sidecar...), module...)
		} else if _, err := os.Stat(filepath.Join(dir, modulePath)); err == nil {
			// The tool stopped being a code tool. Leaving the old module behind
			// would leave a file the pin does not cover and the code targets
			// would still copy.
			return nil, fmt.Errorf("%s is no longer a code tool on the platform, and %s is still committed: "+
				"delete it, then run this again", name, modulePath)
		}

		// The pin covers the sidecar and the module together, so one field pins
		// the whole mirror and there is no way for half of it to be right.
		hash := ir.MirrorDigest(pinned)
		toolPath := filepath.ToSlash(filepath.Join("tools", name+".yaml"))
		stamped, changed, err := stampPin(filepath.Join(dir, toolPath), hash)
		if err != nil {
			return nil, err
		}
		if changed {
			planned = append(planned, pullFile{Path: toolPath, Content: stamped, Action: pullPinned})
		} else {
			planned = append(planned, pullFile{Path: toolPath, Content: stamped, Action: pullUnchanged})
		}
		files = append(files, planned...)
	}
	return files, nil
}

// fileAction decides what writing content to path would do, which is what makes
// `unchanged` a reportable outcome rather than a silent skip.
//
// A mirrored file that is present and differs is `Edited`: nobody wrote it by
// hand on purpose, and an edit there reaches nothing, so the refusal names it
// unless --force. That is the same rule `skill install` applies to the files it
// writes, and for the same reason.
func fileAction(dir, path string, content []byte) pullFile {
	existing, err := os.ReadFile(filepath.Join(dir, path))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return pullFile{Path: path, Content: content, Action: pullWritten}
	case err != nil, string(existing) == string(content):
		return pullFile{Path: path, Content: content, Action: pullUnchanged}
	}
	return pullFile{Path: path, Content: content, Action: pullOverwritten, Edited: true}
}

// stampPin rewrites the `hash:` line under a tool file's `slng:` block and
// leaves every other byte alone.
//
// A line edit rather than a YAML round trip, deliberately: the file is the
// author's, and a re-render would lose their comments, their key order and
// their blank lines. Nothing else in this tree rewrites an authored file, so
// the one that does touches as little as it can.
func stampPin(path, hash string) (content []byte, changed bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		if strings.TrimRight(line, " \t") != "slng:" && strings.TrimSpace(line) != "slng: {}" {
			continue
		}
		want := "  hash: " + hash
		// `slng: {}` is the pre-pull shape; it becomes a block with one field.
		if strings.TrimSpace(line) == "slng: {}" {
			lines[i] = "slng:"
			lines = slices.Insert(lines, i+1, want)
			return []byte(strings.Join(lines, "\n")), true, nil
		}
		if i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "hash:") {
			if lines[i+1] == want {
				return raw, false, nil
			}
			lines[i+1] = want
			return []byte(strings.Join(lines, "\n")), true, nil
		}
		lines = slices.Insert(lines, i+1, want)
		return []byte(strings.Join(lines, "\n")), true, nil
	}
	return nil, false, fmt.Errorf("%s has no `slng:` line to pin: it is the block a hosted reference is written with", filepath.Base(path))
}

// planSecrets adds each mirrored tool's declared secret NAMES to the package's
// `secrets:` list. Names only, never a value: this is the command most able to
// break "secret values appear in no package, generated file, or report", so it
// handles no value at all.
//
// The names also have to reach the slng vault requirements, which is a separate
// change in the generator: the slng driver reads the package's `secrets:` list
// nowhere and derives its own list, so writing only here would be visible in
// the diff and checked by nothing.
func planSecrets(pkg *spec.Package, dir string, mirrors map[string]spec.Mirror) ([]string, *pullFile, error) {
	declared := map[string]bool{}
	for _, name := range pkg.Agent.Secrets {
		declared[name] = true
	}
	seen := map[string]bool{}
	var missing []string
	for _, mirror := range mirrors {
		for _, secret := range mirror.Secrets() {
			if declared[secret] || seen[secret] {
				continue
			}
			seen[secret] = true
			missing = append(missing, secret)
		}
	}
	if len(missing) == 0 {
		return nil, nil, nil
	}
	sort.Strings(missing)
	content, err := appendSecrets(filepath.Join(dir, "agent.yaml"), missing)
	if err != nil {
		return nil, nil, err
	}
	return missing, &pullFile{
		Path: "agent.yaml", Content: content,
		Action: pullAction(fmt.Sprintf("%s added", pluralSecrets(missing))),
	}, nil
}

// appendSecrets adds names under an existing `secrets:` list, or writes the
// whole block when the package has none. A line edit for the same reason
// stampPin is one: agent.yaml is the author's file.
func appendSecrets(path string, names []string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(raw), "\n")
	added := make([]string, 0, len(names))
	for _, name := range names {
		added = append(added, "  - "+name)
	}
	for i, line := range lines {
		if strings.TrimRight(line, " \t") != "secrets:" {
			continue
		}
		end := i + 1
		for end < len(lines) && strings.HasPrefix(lines[end], "  - ") {
			end++
		}
		return []byte(strings.Join(slices.Insert(lines, end, added...), "\n")), nil
	}
	// No `secrets:` at all. It goes after `entry_agent:`, which every package
	// has and which is where the scaffold puts it.
	for i, line := range lines {
		if !strings.HasPrefix(line, "entry_agent:") {
			continue
		}
		block := append([]string{"", "secrets:"}, added...)
		return []byte(strings.Join(slices.Insert(lines, i+1, block...), "\n")), nil
	}
	return nil, fmt.Errorf("agent.yaml has neither a `secrets:` list nor an `entry_agent:` line, so there is nowhere to add %s: add `secrets:` yourself", strings.Join(names, ", "))
}

// reportPullCheck is --check: compare, report, write nothing. Exit 1 on drift,
// which is the shape a CI job would use.
func reportPullCheck(cmd *cobra.Command, dir string, files []pullFile) error {
	out := cmd.OutOrStdout()
	var stale []string
	for _, file := range files {
		if file.Action == pullUnchanged {
			continue
		}
		stale = append(stale, file.Path)
	}
	if len(stale) == 0 {
		fmt.Fprintf(out, "  every hosted tool's mirror matches the organisation\n")
		return nil
	}
	u := style.For(out)
	for _, path := range stale {
		fmt.Fprintf(out, "  %-40s %s\n", dimPath(u, path), u.Dim("stale"))
	}
	return fmt.Errorf("pull %s --check: %d file(s) no longer match the organisation: run `unmute pull` to update them", dir, len(stale))
}

func editedPaths(files []pullFile) []string {
	var edited []string
	for _, file := range files {
		if file.Edited {
			edited = append(edited, file.Path)
		}
	}
	return edited
}

// missingToolGuidance says what to do about a name the organisation does not
// hold, and names the organisation, because the answer depends on it.
//
// unmute creates no tool, so this is the end of the road until somebody makes
// one. The message says that rather than implying a flag would fix it.
func missingToolGuidance(name string, listing []slngAccountTool, listErr error, account slngAccount, readErr error) string {
	var missed *unchecked
	if errors.As(readErr, &missed) && listErr != nil {
		// Both reads failed, so this is not evidence the tool is absent.
		return fmt.Sprintf("could not read `%s` from %s: %v", name, account, readErr)
	}
	var names []string
	for _, tool := range listing {
		if !slices.Contains(names, tool.Name) {
			names = append(names, tool.Name)
		}
	}
	sort.Strings(names)
	held := "it holds none"
	if len(names) > 0 {
		held = "it has `" + strings.Join(names, "`, `") + "`"
	}
	return fmt.Sprintf("this organisation has no tool called `%s` (%s). A hosted reference is the tool file's own name, "+
		"so either rename tools/%s.yaml to a tool the organisation has, or create the tool in the SLNG dashboard: unmute creates none",
		name, held, name)
}

func pluralSecrets(names []string) string {
	if len(names) == 1 {
		return "1 secret"
	}
	return fmt.Sprintf("%d secrets", len(names))
}

func oneOrThem(names []string) string {
	if len(names) == 1 {
		return "it"
	}
	return "them"
}
