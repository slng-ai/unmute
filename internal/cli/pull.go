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
	refs := hostedToolRefs(pkg)
	if len(refs) == 0 {
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
	mirrors := make(map[string]spec.Mirror, len(refs))
	var listing []slngAccountTool
	listErr := runner.read(target.SlngToolList, &listing)
	for _, ref := range refs {
		mirror, err := readTool(runner, ref.Hosted)
		if err != nil || mirror.Name == "" {
			return fmt.Errorf("pull %s: %s", displayDir(dir), missingToolGuidance(ref, listing, listErr, account, err))
		}
		if mirror.Source == "curated" {
			return fmt.Errorf("pull %s: `%s` is a capability SLNG curates, not a tool with a definition to mirror: "+
				"attach it with `builtin: %s` instead, which needs no pull", displayDir(dir), ref.Hosted, ref.Hosted)
		}
		mirror.Fetched = time.Now().UTC().Format(time.DateOnly)
		mirrors[ref.Local] = mirror
	}

	files, err := planPull(dir, refs, mirrors)
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

// hostedRef pairs a package tool's LOCAL file name, the one every other
// package file and diagnostic uses, with the HOSTED name pull fetches it by.
// They differ only for a scalar reference whose `slng:` value is not the tool
// file's own name (tools/order_status.yaml holding `slng: check_order`); a
// legacy block always resolves by the file name, so Local and Hosted are the
// same string for one.
type hostedRef struct {
	Local  string
	Hosted string
	// Scalar is false for a legacy `slng:\n  hash:` block, whose pin is
	// stamped into the tool file itself. True for a scalar `slng: name`
	// reference, whose pin goes into generated metadata instead, because a
	// scalar reference authors none for stampPin to update.
	Scalar bool
}

// hostedToolRefs are the package's `slng:` tools, sorted by local name, so the
// report and every refusal are in a stable order.
func hostedToolRefs(pkg *spec.Package) []hostedRef {
	var refs []hostedRef
	for name, tool := range pkg.Tools {
		if tool.Slng == nil {
			continue
		}
		hosted := tool.Slng.Name
		scalar := hosted != ""
		if hosted == "" {
			hosted = name // the legacy block resolves by the file's own name
		}
		refs = append(refs, hostedRef{Local: name, Hosted: hosted, Scalar: scalar})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Local < refs[j].Local })
	return refs
}

// planPull turns each fetched mirror into the files it writes, without writing
// any of them.
//
// Fetched by ref.Hosted, written under ref.Local: three files per hosted code
// tool, two per hosted request tool. A legacy reference's third file is the
// pin stamped into the authored tool file, rewritten one line at a time rather
// than re-rendered because every other line in it is the author's. A scalar
// reference's third file is generated metadata instead: stampPin is never
// called for one, so a scalar reference's YAML is untouched by a pull.
func planPull(dir string, refs []hostedRef, mirrors map[string]spec.Mirror) ([]pullFile, error) {
	var files []pullFile
	for _, ref := range refs {
		mirror := mirrors[ref.Local]
		sidecarPath, modulePath := spec.MirrorPaths(ref.Local)

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
				"delete it, then run this again", ref.Local, modulePath)
		}

		// The pin covers the sidecar and the module together, so one field pins
		// the whole mirror and there is no way for half of it to be right.
		hash := ir.MirrorDigest(pinned)
		if ref.Scalar {
			metaContent, err := spec.MirrorMeta{Hash: hash}.MirrorMetaJSON()
			if err != nil {
				return nil, err
			}
			// The same overwrite protection every other mirrored file gets: a
			// hand-edited metadata file is Edited, not silently replaced.
			planned = append(planned, fileAction(dir, spec.MirrorMetaPath(ref.Local), metaContent))
		} else {
			toolPath := filepath.ToSlash(filepath.Join("tools", ref.Local+".yaml"))
			stamped, changed, err := stampPin(filepath.Join(dir, toolPath), hash)
			if err != nil {
				return nil, err
			}
			if changed {
				planned = append(planned, pullFile{Path: toolPath, Content: stamped, Action: pullPinned})
			} else {
				planned = append(planned, pullFile{Path: toolPath, Content: stamped, Action: pullUnchanged})
			}
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
//
// Called for a legacy reference only. A scalar reference authors no `slng:`
// block for this to find, and its pin goes into generated metadata instead
// (planPull), so a pull never reaches into a scalar reference's YAML at all.
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
	// Only where a target actually builds and runs the tool.
	//
	// A code target emits a project that calls the tool itself, so the
	// credential has to reach that project and the package's `secrets:` is how
	// it gets there. On slng the platform holds the tool and reads the
	// credential from its own vault, and the slng driver reads the package's
	// `secrets:` nowhere at all: adding a name there edits the author's file to
	// declare something nothing consumes, and `unmute deploy` discovers the same
	// requirement from the published contract anyway.
	//
	// This is why it is conditional rather than removed. Take it away outright
	// and a package pulled for livekit or pipecat loses the one line that gets
	// its credential into the emitted project.
	if !buildsToolsLocally(pkg) {
		return nil, nil, nil
	}
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

// buildsToolsLocally reports whether any selected target emits a project that
// runs the package's tools itself.
func buildsToolsLocally(pkg *spec.Package) bool {
	for _, tgt := range pkg.Targets {
		if target.EmitsProject(target.Provider(tgt.Provider)) {
			return true
		}
	}
	return false
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
// one. The message says that rather than implying a flag would fix it. The fix
// itself differs by reference form: a legacy block resolves by the tool
// file's own name, so renaming the file is the fix; a scalar reference names
// the hosted tool on its own line, so the fix is changing that line instead.
func missingToolGuidance(ref hostedRef, listing []slngAccountTool, listErr error, account slngAccount, readErr error) string {
	var names []string
	for _, tool := range listing {
		if !slices.Contains(names, tool.Name) {
			names = append(names, tool.Name)
		}
	}
	sort.Strings(names)

	// `voiceai tool get` exits non-zero for an absent tool and for a read that
	// did not work, so the error alone cannot tell them apart. The listing can,
	// and the distinction matters: an absent tool is created in the dashboard
	// and a failed read is retried.
	var missed *unchecked
	if errors.As(readErr, &missed) {
		switch {
		case listErr != nil:
			// Neither read worked, so nothing here is evidence either way.
			return fmt.Sprintf("could not read `%s` from %s: %v", ref.Hosted, account, readErr)
		case slices.Contains(names, ref.Hosted):
			// The listing names it and the read failed. Reporting this as an
			// absence produced a self-contradicting sentence, seen for real:
			// "this organisation has no tool called `check_order` (it has
			// `check_order`)". A truncated response or a partial outage is
			// exactly this shape.
			return fmt.Sprintf("`%s` is listed in %s and its definition could not be read: %v. "+
				"That is a failed read rather than a missing tool, so nothing was written: run this again",
				ref.Hosted, account, readErr)
		}
		// The listing worked and does not name it, so it really is absent and
		// the guidance below is right.
	}
	held := "it holds none"
	if len(names) > 0 {
		held = "it has `" + strings.Join(names, "`, `") + "`"
	}
	if ref.Scalar {
		return fmt.Sprintf("this organisation has no tool called `%s` (%s). Change the `slng: %s` line in tools/%s.yaml "+
			"to a tool the organisation has, or create the tool in the SLNG dashboard: unmute creates none",
			ref.Hosted, held, ref.Hosted, ref.Local)
	}
	return fmt.Sprintf("this organisation has no tool called `%s` (%s). A hosted reference is the tool file's own name, "+
		"so either rename tools/%s.yaml to a tool the organisation has, or create the tool in the SLNG dashboard: unmute creates none",
		ref.Hosted, held, ref.Local)
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
