package ir

import (
	"path/filepath"
	"strings"
	"testing"

	packagespec "github.com/slng-ai/unmute/internal/spec"
)

// A target may name several regions, and a region may swap model names, so one
// package runs one agent and one prompt in several places with its own models
// in each. These are the tests for what that resolves to and what it refuses.

func regionsFixture(t *testing.T) *packagespec.Package {
	t.Helper()
	pkg, err := packagespec.Load(filepath.Join("..", "testdata", "livekit_regions"))
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

// withRegions replaces the fixture's declared regions, so a test states the
// shape it is about rather than depending on the fixture's own.
func withRegions(t *testing.T, pkg *packagespec.Package, regions packagespec.Regions) (*Agent, error) {
	t.Helper()
	target := pkg.Targets["livekit"]
	target.DeploymentRegion = regions
	pkg.Targets = map[string]packagespec.Target{"livekit": target}
	return Build(pkg)
}

func TestRegionSwapsResolvePerRegion(t *testing.T) {
	agent, err := withRegions(t, regionsFixture(t), packagespec.Regions{
		{Name: "us-east"},
		{Name: "eu-central", Swaps: []packagespec.Pair{
			{Key: "transcriber", Value: "transcriber_fr"},
			{Key: "front_desk", Value: "front_desk_fr"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	split := agent.Targets["livekit"].PerRegion()
	if len(split) != 2 {
		t.Fatalf("two regions split into %d builds", len(split))
	}
	east, central := split[0], split[1]
	// The swap reaches the binding the agents already name. Nothing in
	// agent.yaml changed: `speak: front_desk` still selects the voice, and in
	// eu-central that name reads the French entry.
	if east.Models.Listen.Language != "en" {
		t.Errorf("us-east listens in %q, want en", east.Models.Listen.Language)
	}
	if central.Models.Listen.Language != "fr" {
		t.Errorf("eu-central listens in %q, want fr", central.Models.Listen.Language)
	}
	if east.Models.Speak["front_desk"].Voice == central.Models.Speak["front_desk"].Voice {
		t.Errorf("both regions speak with %q; the swap did not reach the voice", east.Models.Speak["front_desk"].Voice)
	}
	// A model no region swapped is the same in both, which is what makes this
	// one package rather than two.
	if east.Models.Speak["specialist"].Voice != central.Models.Speak["specialist"].Voice {
		t.Error("an unswapped voice differs between regions")
	}
	if east.Region != "us-east" || central.Region != "eu-central" {
		t.Errorf("regions = %q, %q", east.Region, central.Region)
	}
	if agent.DeployName(east) == agent.DeployName(central) {
		t.Errorf("both regions deploy as %q", agent.DeployName(east))
	}
	if got := central.BuildDir("pkg"); got != filepath.Join("pkg", "build", "livekit", "eu-central") {
		t.Errorf("eu-central builds into %q", got)
	}
	if got := central.Label(); got != "livekit (eu-central)" {
		t.Errorf("label = %q", got)
	}
}

// One region is the shape every package had before this feature, and it must
// still compile to the same directory under the same name. A swap on it is
// still honoured: there is one build, and it reads the named entries.
func TestOneRegionKeepsItsPathsAndName(t *testing.T) {
	agent, err := withRegions(t, regionsFixture(t), packagespec.Regions{
		{Name: "eu-central", Swaps: []packagespec.Pair{{Key: "transcriber", Value: "transcriber_fr"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	split := agent.Targets["livekit"].PerRegion()
	if len(split) != 1 {
		t.Fatalf("one region split into %d builds", len(split))
	}
	only := split[0]
	if only.Region != "" {
		t.Errorf("one region set Region = %q, which would change its paths and its deployed name", only.Region)
	}
	if got := only.BuildDir("pkg"); got != filepath.Join("pkg", "build", "livekit") {
		t.Errorf("one region builds into %q", got)
	}
	if got := only.Label(); got != "livekit" {
		t.Errorf("label = %q", got)
	}
	if only.Models.Listen.Language != "fr" {
		t.Errorf("the single region's swap did not resolve: listens in %q", only.Models.Listen.Language)
	}
}

// Every way a swap can be wrong, refused at build with the file named. A swap
// is two bare words, so the message has to say which word and why.
func TestRegionSwapRefusals(t *testing.T) {
	for _, tc := range []struct {
		name    string
		regions packagespec.Regions
		want    string
	}{
		{
			"unknown default",
			packagespec.Regions{{Name: "us-east"}, {Name: "eu-central", Swaps: []packagespec.Pair{{Key: "transcribr", Value: "transcriber_fr"}}}},
			`swaps "transcribr" in region "eu-central", which is not a defined model`,
		},
		{
			"unknown replacement",
			packagespec.Regions{{Name: "us-east"}, {Name: "eu-central", Swaps: []packagespec.Pair{{Key: "transcriber", Value: "transcriber_de"}}}},
			`"transcriber_de" is not a defined model`,
		},
		{
			"different kind",
			packagespec.Regions{{Name: "us-east"}, {Name: "eu-central", Swaps: []packagespec.Pair{{Key: "transcriber", Value: "front_desk_fr"}}}},
			"one model for another of the same kind",
		},
		{
			"nothing uses the name",
			packagespec.Regions{{Name: "us-east"}, {Name: "eu-central", Swaps: []packagespec.Pair{{Key: "transcriber_fr", Value: "transcriber"}}}},
			"so the swap would change nothing",
		},
		{
			"the same model twice",
			packagespec.Regions{{Name: "us-east"}, {Name: "eu-central", Swaps: []packagespec.Pair{
				{Key: "transcriber", Value: "transcriber_fr"},
				{Key: "transcriber", Value: "transcriber_fr"},
			}}},
			`swaps "transcriber" twice in region "eu-central"`,
		},
		{
			"a region listed twice",
			packagespec.Regions{{Name: "us-east"}, {Name: "us-east"}},
			`lists deployment_region "us-east" twice`,
		},
		{
			"an empty region",
			packagespec.Regions{{Name: "us-east"}, {Name: ""}},
			"empty deployment_region entry",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := withRegions(t, regionsFixture(t), tc.regions)
			if err == nil {
				t.Fatal("want a refusal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not say %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "targets.yaml") {
				t.Fatalf("error %v does not name the file", err)
			}
		})
	}
}
