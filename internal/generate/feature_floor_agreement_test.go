package generate

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// Two places answer "does this package use a warm transfer / an MCP source".
//
// ir.UsedFrameworkFeatures answers it as a version question, so validation can
// gate the declared version against each feature's floor. The LiveKit driver
// answers it again for its own reasons: which import to write, which extra to
// install. Both are legitimate, and they are allowed to be computed differently
// — but they must not disagree about a real package, because a divergence puts
// back exactly the bug this feature removed: emitted code that needs a newer
// framework than validation ever asked for.
//
// So this is the agreement test Principle III requires wherever one fact is
// stated twice. It runs over every shipped example and fixture rather than a
// hand-written case, so a package shape nobody thought of is still covered.
func TestFeatureUseAgreesWithDriver(t *testing.T) {
	packages := packagesUnder(t, filepath.Join("..", "..", "examples"))
	packages = append(packages, packagesUnder(t, filepath.Join("..", "testdata"))...)
	if len(packages) == 0 {
		t.Fatal("no packages found; the comparison below would be vacuous")
	}

	var sawWarm, sawMCP bool
	for _, dir := range packages {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			pkg, err := spec.Load(dir)
			if err != nil {
				t.Skipf("load: %v", err) // not every fixture is a loadable package
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Skipf("build: %v", err)
			}
			used := ir.UsedFrameworkFeatures(agent)
			wantWarm := slices.Contains(used, target.FeatureWarmTransfer)
			wantMCP := slices.Contains(used, target.FeatureMCPTools)

			for _, name := range slices.Sorted(maps.Keys(agent.Targets)) {
				resolved := agent.Targets[name]
				if target.Provider(resolved.Provider) != target.LiveKit {
					continue
				}
				data, err := buildLiveKitData(agent, resolved)
				if err != nil {
					t.Fatalf("build livekit data for %q: %v", name, err)
				}
				if data.HasWarmTransfer != wantWarm {
					t.Errorf("target %q: driver says warm transfer=%v, ir.UsedFrameworkFeatures says %v",
						name, data.HasWarmTransfer, wantWarm)
				}
				if data.NeedsMCP != wantMCP {
					t.Errorf("target %q: driver says mcp=%v, ir.UsedFrameworkFeatures says %v",
						name, data.NeedsMCP, wantMCP)
				}
				sawWarm = sawWarm || data.HasWarmTransfer
				sawMCP = sawMCP || data.NeedsMCP
			}
		})
	}

	// Agreeing on "neither feature is present" everywhere would pass while
	// proving nothing, so at least one package must exercise each side.
	if !sawWarm {
		t.Error("no example or fixture uses a warm transfer; the warm half of this test is vacuous")
	}
	if !sawMCP {
		t.Error("no example or fixture uses an MCP source; the mcp half of this test is vacuous")
	}
}

func packagesUnder(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read %s: %v", root, err)
	}
	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(dir, "agent.yaml")); err != nil {
			continue
		}
		dirs = append(dirs, dir)
	}
	return dirs
}
