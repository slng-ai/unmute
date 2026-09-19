package generate

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// The LiveKit artifact has to be deployable on LiveKit Cloud with the commands
// its own README prints. These assertions live outside the golden so a
// regeneration cannot quietly accept a broken artifact.

// livekitArtifact compiles the build for one declared region, splitting the
// target first the way the CLI does. A target naming several regions is several
// builds, so `which` says which one to compile.
func livekitArtifact(t *testing.T, regions []string, which ...int) Artifact {
	t.Helper()
	agent := loadCompilerAgent(t)
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	tgt.DeploymentRegions = regions
	split := tgt.PerRegion()
	index := 0
	if len(which) > 0 {
		index = which[0]
	}
	artifact, err := Generate(agent, split[index], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func TestLiveKitEmitsNoDeploymentConfigFile(t *testing.T) { // FR-008
	for _, regions := range [][]string{nil, {"us-east"}, {"us-east", "eu-central"}} {
		for _, file := range livekitArtifact(t, regions).Files {
			if strings.HasPrefix(file.Path, "livekit") && strings.HasSuffix(file.Path, ".toml") {
				t.Errorf("regions %v: emitted %s, which makes `lk agent create` refuse", regions, file.Path)
			}
		}
	}
}

func TestLiveKitReadmeDeploySection(t *testing.T) { // FR-003, FR-004, FR-005, FR-010, FR-019
	one := artifactFile(t, livekitArtifact(t, []string{"us-east"}), "README.md")
	for _, want := range []string{
		"lk agent create --region us-east --secrets-file .env", // first deploy, with the region
		"\nlk agent deploy\n", // redeploy, separately
		"lk agent update-secrets --secrets-file .env",
		"--overwrite",
		"lk agent config --id",
		"lk agent status",
		"lk project set-default",
		"LIVEKIT_API_SECRET` are supplied by the platform",
		"immutable",
		"self-hosted",
	} {
		if !strings.Contains(one, want) {
			t.Errorf("README missing %q", want)
		}
	}
	// The redeploy command takes no region, and the single-region flow must not
	// teach a config file name nobody needs to type.
	if strings.Contains(one, "lk agent deploy --region") {
		t.Error("README puts a region on the redeploy command; region is fixed at create")
	}
	if strings.Contains(one, "livekit.us-east.toml") {
		t.Error("README names a per-region config file for a single region")
	}
	for _, unwanted := range []string{"not the supported path", "unsupported"} {
		if strings.Contains(one, unwanted) {
			t.Errorf("README still disclaims a working path: %q", unwanted)
		}
	}

	// A one-element list is the scalar form, file names included.
	if scalar := artifactFile(t, livekitArtifact(t, []string{"us-east"}), "README.md"); scalar != one {
		t.Error("a one-element region list changed the README")
	}

	// Several regions are several builds. Each README describes its own region
	// and nothing else, and says where the siblings are.
	regions := []string{"us-east", "eu-central"}
	for index, region := range regions {
		build := artifactFile(t, livekitArtifact(t, regions, index), "README.md")
		for _, want := range []string{
			"lk agent create --region " + region + " --secrets-file .env",
			"This directory is **" + region + "**",
			"its own directory under `build/",
			"deploy each sibling directory separately",
			"not because the platform\nchose the nearest worker",
		} {
			if !strings.Contains(build, want) {
				t.Errorf("the %s README is missing %q", region, want)
			}
		}
		other := regions[(index+1)%len(regions)]
		if strings.Contains(build, "--region "+other) {
			t.Errorf("the %s README deploys %s too; one directory is one region", region, other)
		}
		if strings.Contains(build, "livekit."+region+".toml") {
			t.Errorf("the %s README names a per-region config file; one directory holds one plain livekit.toml", region)
		}
	}

	none := artifactFile(t, livekitArtifact(t, nil), "README.md")
	if strings.Contains(none, "--region") {
		t.Error("README carries a region flag with no region declared")
	}
	if !strings.Contains(none, "the first deploy asks which one to use") {
		t.Error("README does not say the platform prompts when no region is declared")
	}
}

// Two regions are two agent names. Sharing one would be one agent whose
// behaviour depended on which worker answered, which is exactly what per-region
// models make wrong.
func TestLiveKitRegionsGetTheirOwnAgentNames(t *testing.T) {
	regions := []string{"us-east", "eu-central"}
	agent := loadCompilerAgent(t)
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	tgt.DeploymentRegions = regions
	split := tgt.PerRegion()
	if len(split) != len(regions) {
		t.Fatalf("two regions split into %d builds", len(split))
	}
	first, second := agent.DeployName(split[0]), agent.DeployName(split[1])
	if first == second {
		t.Fatalf("both regions deploy as %q", first)
	}
	for index, region := range regions {
		if !strings.HasSuffix(agent.DeployName(split[index]), "-"+region) {
			t.Errorf("%s deploys as %q, which does not name its region", region, agent.DeployName(split[index]))
		}
		if got := split[index].BuildDir("pkg"); !strings.HasSuffix(got, region) {
			t.Errorf("%s builds into %q, which does not name its region", region, got)
		}
	}
}

func TestLiveKitContainerRequirements(t *testing.T) { // FR-011
	artifact := livekitArtifact(t, []string{"us-east"})
	dockerfile := artifactFile(t, artifact, "Dockerfile")
	if !strings.Contains(dockerfile, "\nUSER ") {
		t.Error("Dockerfile has no USER switch; LiveKit Cloud requires a non-root run user")
	}
	for _, injected := range []string{"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"} {
		if strings.Contains(dockerfile, "ENV "+injected) || strings.Contains(dockerfile, injected+"=") {
			t.Errorf("Dockerfile sets %s, which the platform injects and the image must not", injected)
		}
	}
	if !strings.Contains(dockerfile, "WORKDIR /app") {
		t.Error("Dockerfile has no explicit WORKDIR")
	}
	if !strings.Contains(dockerfile, `CMD ["python", "-m", "livekit.agents", "start", "agent.py"]`) {
		t.Error("Dockerfile no longer launches the worker through the supported module CLI")
	}
	ignore := artifactFile(t, artifact, ".dockerignore")
	for _, want := range []string{".env\n", ".env.*", ".venv/", "__pycache__/"} {
		if !strings.Contains(ignore, want) {
			t.Errorf(".dockerignore missing %q, so a local run inflates the upload context", want)
		}
	}
}

func TestLiveKitReportCarriesRegions(t *testing.T) { // FR-020
	report := func(t *testing.T, regions []string) map[string]any {
		t.Helper()
		var decoded map[string]any
		if err := json.Unmarshal([]byte(artifactFile(t, livekitArtifact(t, regions), "compile-report.json")), &decoded); err != nil {
			t.Fatal(err)
		}
		return decoded
	}
	// One report per build, each naming its own region: the report describes
	// the directory it sits in, and a directory holds one region.
	for index, region := range []string{"us-east", "eu-central"} {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(artifactFile(t, livekitArtifact(t, []string{"us-east", "eu-central"}, index), "compile-report.json")), &decoded); err != nil {
			t.Fatal(err)
		}
		got, ok := decoded["deployment_regions"].([]any)
		if !ok || len(got) != 1 || got[0] != region {
			t.Fatalf("the %s build reports deployment_regions = %v", region, decoded["deployment_regions"])
		}
	}
	one := report(t, []string{"us-east"})
	if got, ok := one["deployment_regions"].([]any); !ok || len(got) != 1 || got[0] != "us-east" {
		t.Fatalf("deployment_regions = %v, want the one declared region", one["deployment_regions"])
	}
	if _, present := report(t, nil)["deployment_regions"]; present {
		t.Error("deployment_regions is in the report with no region declared")
	}
}

// A LiveKit package with no webhook tools and no tracing pulls httpx from
// nowhere, and livekit-agents needs it: `inference/llm.py` imports httpx, the
// package declares none, and openai 3.0 replaced its httpx dependency with
// httpx2. Since `livekit.agents.__init__` imports `inference` eagerly, such a
// project cannot import the SDK at all. This is the exact shape of
// examples/salon-concierge, which is the package the transfer rig deploys.
func TestLiveKitDeclaresHTTPXWithoutWebhooksOrTracing(t *testing.T) {
	agent := loadCompilerAgent(t)
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	agent.Tracing = nil
	for name, tool := range agent.Tools {
		if tool.Execution == ir.ToolWebhook {
			delete(agent.Tools, name)
			for agentName, def := range agent.Agents {
				def.Tools = slices.DeleteFunc(def.Tools, func(t string) bool { return t == name })
				agent.Agents[agentName] = def
			}
			for taskName, task := range agent.Tasks {
				task.Tools = slices.DeleteFunc(task.Tools, func(t string) bool { return t == name })
				agent.Tasks[taskName] = task
			}
		}
	}
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if strings.Contains(pyproject, "langfuse") {
		t.Fatal("fixture still enables tracing, so httpx could arrive through langfuse")
	}
	if !strings.Contains(pyproject, `"httpx"`) {
		t.Errorf("pyproject omits httpx, so the deployed agent cannot import livekit.agents:\n%s", pyproject)
	}
}
