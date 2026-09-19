package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A target that names several regions is several builds. These hold the parts a
// reader sees: the directories on disk, the rows validate prints, and the fact
// that the two builds really do carry different models.

func TestCompileWritesOneBuildPerRegion(t *testing.T) {
	dir := copyPackage(t, filepath.Join("..", "testdata", "livekit_regions"))
	stdout, stderr, err := runCompileCommand(t, dir)
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, stderr)
	}
	for _, region := range []string{"us-east", "eu-central"} {
		agent := filepath.Join(dir, "build", "livekit", region, "agent.py")
		if _, err := os.Stat(agent); err != nil {
			t.Fatalf("no build for %s: %v", region, err)
		}
		// Every written path is named on stdout, because that is what `compile`
		// says it did.
		if !strings.Contains(stdout, filepath.Join("build", "livekit", region, "agent.py")) {
			t.Errorf("stdout does not name the %s build:\n%s", region, stdout)
		}
	}
	// The target's own directory holds the regions and nothing else: a build
	// there would be a fourth thing to deploy that nobody asked for.
	if _, err := os.Stat(filepath.Join(dir, "build", "livekit", "agent.py")); err == nil {
		t.Error("compile also wrote a region-less build next to the regional ones")
	}
	// The two builds differ where the swap says they differ, and nowhere else
	// that matters: same agent, same prompt, different models.
	east := readBuild(t, dir, "us-east")
	central := readBuild(t, dir, "eu-central")
	if east == central {
		t.Fatal("both regions compiled to the same agent; the swap reached nothing")
	}
	if !strings.Contains(east, `language="en"`) || !strings.Contains(central, `language="fr"`) {
		t.Error("the transcriber swap did not reach the emitted agents")
	}
	if !strings.Contains(east, "aura-2-thalia-en") || !strings.Contains(central, "aura-2-pandora-fr") {
		t.Error("the voice swap did not reach the emitted agents")
	}
	// The worker registers under a name that carries its region, so two
	// deployments of one package cannot answer for each other.
	if !strings.Contains(east, "safe-core-fixture-livekit-us-east") {
		t.Error("the us-east agent does not register under its own name")
	}
	if !strings.Contains(central, "safe-core-fixture-livekit-eu-central") {
		t.Error("the eu-central agent does not register under its own name")
	}
}

func readBuild(t *testing.T, dir, region string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, "build", "livekit", region, "agent.py"))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// Each region gets its own validation row, so a package that is wrong in one
// place and right in another says which.
func TestValidateLabelsEachRegionRow(t *testing.T) {
	dir := copyPackage(t, filepath.Join("..", "testdata", "livekit_regions"))
	stdout, stderr, err := runValidateCommand(t, dir)
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, stderr)
	}
	for _, want := range []string{"livekit (us-east)", "livekit (eu-central)"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout has no row for %q:\n%s", want, stdout)
		}
	}
}

// A target naming one region keeps the directory and the row it always had.
// This is the promise every existing package depends on.
func TestOneRegionKeepsTheOldLayout(t *testing.T) {
	dir := copyPackage(t, filepath.Join("..", "testdata", "livekit_regions"))
	path := filepath.Join(dir, "targets.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.SplitN(string(content), "    deployment_region:", 2)[0] + "    deployment_region: us-east\n"
	if err := os.WriteFile(path, []byte(trimmed), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runCompileCommand(t, dir)
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "build", "livekit", "agent.py")); err != nil {
		t.Fatalf("one region did not compile to build/livekit/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "build", "livekit", "us-east")); err == nil {
		t.Error("one region compiled into a region directory, changing every existing package's layout")
	}
	if strings.Contains(stdout, "livekit (us-east)") {
		t.Errorf("one region labelled its row with the region:\n%s", stdout)
	}
}
