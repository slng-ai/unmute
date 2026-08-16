// Package skill holds the coding-agent skill bundle and the logic that puts it
// into a user's project.
//
// The bundle is embedded at build time, so `unmute skill install` makes no
// network call and can never disagree with the binary that carries it. Two
// destinations exist because no single directory serves every assistant: the
// canonical bundle lands where Codex, Cursor, and GitHub Copilot read, and a
// pointer lands where Claude Code reads. Verified against each vendor's own
// documentation on 2026-08-15.
//
// This package holds no cobra. The command in internal/cli/skill.go parses
// flags and prints; everything decided here is a pure function of the embedded
// bundle and what is already on disk.
package skill

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed assets
var assets embed.FS

// versionToken is the only thing the install substitutes. Everything else in
// the bundle is written exactly as it was authored.
const versionToken = "{{unmute_version}}"

// Bundle is the embedded skill plus the CLI version that carries it. The
// version is stamped at link time and reaches here from the root command, never
// hardcoded.
type Bundle struct {
	FS      fs.FS
	Version string
}

// New returns the bundle this binary carries.
func New(version string) Bundle {
	return Bundle{FS: assets, Version: version}
}

// Destination is one directory a bundle lands in. There are two and they are
// fixed: an install that writes anywhere else is a bug, not a feature.
type Destination struct {
	Name string // "canonical" or "pointer", used in errors and tests

	dir  []string // path elements under the project root
	root string   // subtree of the embedded FS this destination takes
	skip string   // embedded subtree that belongs to another destination
}

// The two destinations. Path elements rather than a literal string, because
// Windows is a supported platform and filepath.Join owns the separator.
var (
	Canonical = Destination{
		Name: "canonical",
		dir:  []string{".agents", "skills", "unmute"},
		root: "assets",
		skip: "assets/pointer",
	}
	Pointer = Destination{
		Name: "pointer",
		dir:  []string{".claude", "skills", "unmute"},
		root: "assets/pointer",
	}
)

// Dir returns this destination's directory under the given project root.
func (d Destination) Dir(project string) string {
	return filepath.Join(append([]string{project}, d.dir...)...)
}

// Rel returns this destination's directory as the user sees it, with forward
// slashes, for printing. Never used to touch the filesystem.
func (d Destination) Rel() string { return path.Join(d.dir...) }

// Assistants maps the --agent names onto destinations. Several names share a
// destination, which is why the resolver deduplicates rather than writing twice.
var assistants = map[string][]Destination{
	"claude":  {Pointer},
	"codex":   {Canonical},
	"cursor":  {Canonical},
	"copilot": {Canonical},
	"all":     {Canonical, Pointer},
}

// AssistantNames lists every accepted --agent value, sorted, for help text and
// for the error an unknown value produces. "all" is one of them.
func AssistantNames() []string {
	out := make([]string, 0, len(assistants))
	for name := range assistants {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Assistants lists the real assistants, without the "all" shorthand. This is
// what the install reports back, because "installed for all" answers a
// different question than "installed for the editor I use".
func Assistants() []string {
	out := make([]string, 0, len(assistants)-1)
	for name := range assistants {
		if name == "all" {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Destinations resolves --agent values to the destinations to write. An empty
// list means all. An unknown name is an error that lists what is supported: it
// never falls back to all, because silently installing something other than
// what was asked for is the averaging Principle II forbids.
func Destinations(agents []string) ([]Destination, error) {
	if len(agents) == 0 {
		agents = []string{"all"}
	}
	var out []Destination
	seen := map[string]bool{}
	for _, agent := range agents {
		dests, ok := assistants[strings.ToLower(strings.TrimSpace(agent))]
		if !ok {
			return nil, fmt.Errorf("unknown assistant %q: supported names are %s", agent, strings.Join(AssistantNames(), ", "))
		}
		for _, dest := range dests {
			if seen[dest.Name] {
				continue // two names, one directory: write it once
			}
			seen[dest.Name] = true
			out = append(out, dest)
		}
	}
	return out, nil
}

// Action is what the install did to one file. Every file gets one, including
// the ones nothing happened to: a silent no-op and a silent overwrite look
// identical to a user.
type Action string

const (
	ActionWritten  Action = "written"
	ActionCurrent  Action = "left alone"
	ActionUpdated  Action = "updated"
	ActionUpgraded Action = "upgraded"
	ActionRestored Action = "restored"
	ActionRemoved  Action = "removed"
	ActionRefused  Action = "refused"
)

// FileDecision is what will happen to one file, decided before anything is
// written.
type FileDecision struct {
	Path   string // relative to the destination directory, forward slashes
	Action Action

	content []byte // nil when the decision writes nothing
}

// DestinationPlan is the whole decision for one destination.
type DestinationPlan struct {
	Destination Destination
	Dir         string
	Files       []FileDecision

	// FromVersion is the version the manifest carried when it differs from the
	// bundle's. Empty when there is nothing to upgrade from.
	FromVersion string
}

// Refused lists the files this plan will not touch. A non-empty list means the
// plan must not be applied without --force.
func (p DestinationPlan) Refused() []string {
	var out []string
	for _, file := range p.Files {
		if file.Action == ActionRefused {
			out = append(out, path.Join(p.Destination.Rel(), file.Path))
		}
	}
	return out
}

// Changed reports whether applying this plan would change anything on disk.
func (p DestinationPlan) Changed() bool {
	for _, file := range p.Files {
		if file.Action != ActionCurrent {
			return true
		}
	}
	return false
}

// Plan computes every file decision for one destination against what is on
// disk, before anything is written. The whole set is computed up front so a
// refusal can name every offending file at once rather than stopping at the
// first one.
func (b Bundle) Plan(project string, dest Destination, force bool) (DestinationPlan, error) {
	dir := dest.Dir(project)
	plan := DestinationPlan{Destination: dest, Dir: dir}

	embedded, err := b.Files(dest)
	if err != nil {
		return plan, err
	}

	manifest, hasManifest, err := readManifest(dir)
	if err != nil {
		return plan, err
	}
	if hasManifest && manifest.Version != b.Version {
		plan.FromVersion = manifest.Version
	}

	names := make([]string, 0, len(embedded))
	for name := range embedded {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		want := embedded[name]
		plan.Files = append(plan.Files, decide(dir, name, want, manifest, hasManifest, plan.FromVersion != "", force))
	}

	// A file the manifest lists and this bundle no longer carries is a stale
	// reference the assistant would still read. Remove it, and say so.
	for name := range manifest.Files {
		if _, still := embedded[name]; still {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			continue // already gone
		}
		plan.Files = append(plan.Files, FileDecision{Path: name, Action: ActionRemoved})
	}
	sort.Slice(plan.Files, func(i, j int) bool { return plan.Files[i].Path < plan.Files[j].Path })
	return plan, nil
}

// decide is the install decision table from data-model.md, one file at a time.
func decide(dir, name string, want []byte, manifest Manifest, hasManifest, upgrading, force bool) FileDecision {
	write := FileDecision{Path: name, content: want}
	recorded, tracked := manifest.Files[name]

	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	switch {
	case errors.Is(err, os.ErrNotExist):
		// Absent on disk. Restored if we wrote it once, new otherwise.
		write.Action = ActionWritten
		if tracked {
			write.Action = ActionRestored
		}
		return write
	case err != nil:
		// Unreadable is not the same as absent. Refuse rather than clobber.
		return FileDecision{Path: name, Action: ActionRefused}
	}

	if !hasManifest || !tracked || Hash(got) != recorded {
		// Someone else's file, or ours after a hand edit. Either way it is not
		// safe to overwrite without being told to.
		if !force {
			return FileDecision{Path: name, Action: ActionRefused}
		}
		write.Action = ActionUpdated
		return write
	}

	switch {
	case upgrading:
		write.Action = ActionUpgraded
	case Hash(got) == Hash(want):
		return FileDecision{Path: name, Action: ActionCurrent}
	default:
		write.Action = ActionUpdated
	}
	return write
}

// Apply writes one destination whole or not at all. A failure part way through
// puts back what was there, so a broken install never resembles a good one.
func (b Bundle) Apply(plan DestinationPlan) (err error) {
	if refused := plan.Refused(); len(refused) > 0 {
		return fmt.Errorf("%s: refusing to overwrite locally changed files:\n  %s\nrun with --force to overwrite them", plan.Destination.Rel(), strings.Join(refused, "\n  "))
	}
	if !plan.Changed() {
		return nil
	}

	backup := map[string][]byte{} // path on disk -> what was there, nil if absent
	defer func() {
		if err == nil {
			return
		}
		for file, was := range backup {
			if was == nil {
				_ = os.Remove(file)
				continue
			}
			_ = os.WriteFile(file, was, 0o644)
		}
	}()

	manifest := Manifest{Version: b.Version, Files: map[string]string{}}
	for _, file := range plan.Files {
		target := filepath.Join(plan.Dir, filepath.FromSlash(file.Path))
		if file.Action == ActionRemoved {
			was, readErr := os.ReadFile(target)
			if readErr != nil {
				continue
			}
			backup[target] = was
			if err = os.Remove(target); err != nil {
				return fmt.Errorf("removing %s: %w", target, err)
			}
			continue
		}
		if file.Action == ActionCurrent {
			manifest.Files[file.Path] = Hash(file.content)
			continue
		}
		if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(target), err)
		}
		was, readErr := os.ReadFile(target)
		if readErr != nil {
			was = nil
		}
		backup[target] = was
		if err = os.WriteFile(target, file.content, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", target, err)
		}
		manifest.Files[file.Path] = Hash(file.content)
	}

	// The manifest lands last: a destination without one reads as not installed
	// by Unmute, which is the honest state if anything above failed.
	manifestPath := filepath.Join(plan.Dir, manifestName)
	was, readErr := os.ReadFile(manifestPath)
	if readErr != nil {
		was = nil
	}
	backup[manifestPath] = was
	if err = writeManifest(plan.Dir, manifest); err != nil {
		return err
	}
	return nil
}

// Files reads every file this destination takes from the embedded bundle, keyed
// by its path inside the destination. Exported for the agreement tests, which
// read the bundle's content rather than installing it.
func (b Bundle) Files(dest Destination) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := fs.WalkDir(b.FS, dest.root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if dest.skip != "" && name == dest.skip {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".gitkeep") {
			return nil // the directory exists because of its contents
		}
		content, err := fs.ReadFile(b.FS, name)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dest.root, name)
		if err != nil {
			return err
		}
		// The one substitution the bundle carries. The version is stamped at
		// link time, so it cannot be a literal in an embedded file, and an
		// assistant reading SKILL.md needs to know which version it is holding.
		content = []byte(strings.ReplaceAll(string(content), versionToken, b.Version))
		out[filepath.ToSlash(rel)] = content
		return nil
	})
	return out, err
}

// Hash is the lowercase hex SHA-256 the manifest records.
func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
