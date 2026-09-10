package generate

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

var updateCompat = flag.Bool("update-compat", false, "rewrite the compatibility byte digests")

const compatDigests = "testdata/compat_bytes.txt"

// compatAllowlist names a package whose emitted bytes may differ from the
// recorded digest, with the reason. It is the one door out of this test, and it
// is meant to stay nearly shut: a package that writes none of the three new
// keys and changes anyway is a defect, not a regeneration.
//
// The group-code changes are the exception the spec named: a group now stops
// when a step ends unserved, and a handoff called beside a terminal tool in one
// response waits for that tool to settle. Both reach every package that
// declares `task_groups:`, whether or not it writes a new key.
var compatAllowlist = map[string]string{
	// The only shipped, fixture or test package that declares a task group and
	// writes none of the three keys. Both allowed changes reach it:
	//
	//  1. a group now stops when a step ends unserved, rather than running the
	//     later steps and handing the owner a status for a request nobody
	//     answered;
	//  2. a handoff called beside a tool that ends the step waits for that tool
	//     to settle, so the caller is not moved before the step's own work is
	//     recorded.
	//
	// Both are named in the pull request that ships them. Nothing else may join
	// this list without the same treatment.
	"testdata/remy": "a group stops on unserved, and a handoff waits for a terminal call to settle",
}

// newAuthoringKey matches `finish:`, `opening:` or `skip_when_confirmed:` where
// a package writes one: at the start of a line, after an indent, or as the
// first key of a list item. A package that writes one of them is expected to
// change, so it is not held to a digest here; its own gate holds it instead.
var newAuthoringKey = regexp.MustCompile(`(?m)^\s*(?:-\s+)?(finish|opening|skip_when_confirmed):`)

// TestPackagesWritingNoNewKeyEmitTheSameBytes is the compatibility guard for
// spec 010. Every shipped, fixture and test package that writes none of the
// three new keys must compile to exactly the bytes it compiled to before, on
// every code target it declares.
//
// The existing goldens cover two packages. This covers all of them, which is
// the point: the three keys change the shared emitted Python, the task prompt
// tail and the group runtime, and each of those is inserted into files nobody
// reads on a package that asked for none of it.
func TestPackagesWritingNoNewKeyEmitTheSameBytes(t *testing.T) {
	packages := compatPackages(t)
	if len(packages) == 0 {
		t.Fatal("found no package to hold; the roots moved or the scan broke")
	}
	got := map[string]string{}
	for _, dir := range packages {
		name := compatName(dir)
		if writesANewKey(t, dir) {
			continue
		}
		pkg, err := spec.Load(dir)
		if err != nil {
			t.Fatalf("%s: load: %v", name, err)
		}
		agent, err := ir.Build(pkg)
		if err != nil {
			t.Fatalf("%s: build: %v", name, err)
		}
		for _, targetName := range slices.Sorted(maps.Keys(agent.Targets)) {
			resolved := agent.Targets[targetName]
			if !target.EmitsProject(target.Provider(resolved.Provider)) {
				continue
			}
			artifact, err := Generate(agent, resolved, target.Default())
			if err != nil {
				t.Fatalf("%s on %s: generate: %v", name, targetName, err)
			}
			got[name+" "+targetName] = digestFiles(artifact.Files)
		}
	}
	if *updateCompat {
		writeCompatDigests(t, got)
		return
	}
	want := readCompatDigests(t)
	for key, digest := range got {
		name, _, _ := strings.Cut(key, " ")
		recorded, ok := want[key]
		if !ok {
			t.Errorf("%s emits output nothing recorded; run go test ./internal/generate -run TestPackagesWritingNoNewKeyEmitTheSameBytes -update-compat and read the diff", key)
			continue
		}
		if recorded == digest {
			continue
		}
		if reason, allowed := compatAllowlist[name]; allowed {
			t.Logf("%s changed, allowed: %s", key, reason)
			continue
		}
		t.Errorf("%s writes none of finish:, skip_when_confirmed: or opening: and its emitted bytes changed; that is a regression for every package that asked for none of this feature. Read the diff before touching this test", key)
	}
	for key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("%s is recorded but no longer produced; if the package now writes a new key, re-record with -update-compat", key)
		}
	}
}

// compatPackages finds every directory holding an agent.yaml under the three
// roots that hold a package, for the three reasons CLAUDE.md gives for them
// being three roots.
func compatPackages(t *testing.T) []string {
	t.Helper()
	var found []string
	for _, root := range []string{
		filepath.Join("..", "..", "examples"),
		filepath.Join("..", "testdata"),
		filepath.Join("..", voiceAgentTestsDir),
	} {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("read %s: %v", root, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.Join(root, entry.Name())
			if _, err := os.Stat(filepath.Join(dir, "agent.yaml")); err != nil {
				continue
			}
			found = append(found, dir)
		}
	}
	sort.Strings(found)
	return found
}

// compatName is the package's own directory name prefixed by its root, so
// examples/remy and testdata/remy cannot collide in the digest file.
func compatName(dir string) string {
	parent := filepath.Base(filepath.Dir(dir))
	return parent + "/" + filepath.Base(dir)
}

func writesANewKey(t *testing.T, dir string) bool {
	t.Helper()
	written := false
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if newAuthoringKey.Match(content) {
			written = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", dir, err)
	}
	return written
}

// digestFiles hashes every emitted path and its content, so a renamed file, a
// dropped file and a changed byte all read as a difference.
func digestFiles(files []File) string {
	sorted := slices.Clone(files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	sum := sha256.New()
	for _, file := range sorted {
		fmt.Fprintf(sum, "%s %d\n", file.Path, len(file.Content))
		sum.Write(file.Content)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func readCompatDigests(t *testing.T) map[string]string {
	t.Helper()
	content, err := os.ReadFile(compatDigests)
	if err != nil {
		t.Fatalf("read %s: %v; record it with -update-compat", compatDigests, err)
	}
	digests := map[string]string{}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, digest, ok := strings.Cut(line, " = ")
		if !ok {
			t.Fatalf("%s: cannot read line %q", compatDigests, line)
		}
		digests[key] = digest
	}
	return digests
}

func writeCompatDigests(t *testing.T, digests map[string]string) {
	t.Helper()
	var out strings.Builder
	out.WriteString("# Emitted bytes, per package and target, for every package writing none of\n")
	out.WriteString("# finish:, skip_when_confirmed: or opening:. Written by\n")
	out.WriteString("# go test ./internal/generate -run TestPackagesWritingNoNewKeyEmitTheSameBytes -update-compat\n")
	for _, key := range slices.Sorted(maps.Keys(digests)) {
		fmt.Fprintf(&out, "%s = %s\n", key, digests[key])
	}
	if err := os.WriteFile(compatDigests, []byte(out.String()), 0o644); err != nil {
		t.Fatalf("write %s: %v", compatDigests, err)
	}
	t.Logf("recorded %d digests in %s", len(digests), compatDigests)
}
