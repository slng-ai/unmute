package generate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/target"
)

// The Pipecat artifact has to be deployable with `pipecat cloud deploy` as the
// generated README prints it. Contract:
// specs/001-livekit-cloud-deploy/contracts/artifacts.md.

func pipecatArtifact(t *testing.T, regions []string) Artifact {
	t.Helper()
	agent := loadCompilerAgent(t)
	tgt := targetByProvider(t, agent, ir.ProviderPipecat)
	tgt.DeploymentRegions = regions
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func TestPipecatManifestIsDeployable(t *testing.T) { // FR-012, FR-013, FR-027
	manifest := artifactFile(t, pipecatArtifact(t, []string{"eu-west"}), "pcc-deploy.toml")
	// `image` switches off the cloud build the documented deploy depends on, and
	// the value we used to emit was not a resolvable image URL either.
	if strings.Contains(manifest, "image") {
		t.Errorf("manifest names an image, which switches cloud builds off:\n%s", manifest)
	}
	// A replica count nobody declared and nothing derived, which also bills for
	// a warm instance the platform would not keep by default.
	for _, unwanted := range []string{"min_agents", "[scaling]"} {
		if strings.Contains(manifest, unwanted) {
			t.Errorf("manifest still declares %q:\n%s", unwanted, manifest)
		}
	}
	for _, want := range []string{`agent_name = "pipecat"`, `region = "eu-west"`, `secret_set = "pipecat-secrets"`} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest missing %q:\n%s", want, manifest)
		}
	}
	if got := strings.Count(manifest, "region ="); got != 1 {
		t.Errorf("manifest has %d region keys, want exactly one:\n%s", got, manifest)
	}
	if none := artifactFile(t, pipecatArtifact(t, nil), "pcc-deploy.toml"); strings.Contains(none, "region =") {
		t.Errorf("manifest names a region with none declared:\n%s", none)
	}
}

func TestPipecatReadmeDeploySection(t *testing.T) { // FR-003, FR-004, FR-014, FR-018
	withRegion := artifactFile(t, pipecatArtifact(t, []string{"eu-west"}), "README.md")
	for _, want := range []string{
		"## Deploy to Pipecat Cloud",
		"pipecat cloud secrets set pipecat-secrets --file .env --region eu-west",
		"pipecat cloud deploy",
		"pipecat cloud agent status pipecat",
		"pipecat cloud regions list",
		"updates the existing agent",
		"--secrets <other-name>",
		"--min-agents",
		"globally unique",
	} {
		if !strings.Contains(withRegion, want) {
			t.Errorf("README missing %q", want)
		}
	}
	// The set has to exist before the deploy that references it.
	if secrets, deploy := strings.Index(withRegion, "secrets set"), strings.Index(withRegion, "pipecat cloud deploy\n"); secrets < 0 || deploy < 0 || secrets > deploy {
		t.Error("README does not order the secret set before the deploy")
	}

	noRegion := artifactFile(t, pipecatArtifact(t, nil), "README.md")
	for _, want := range []string{"organisation's default region", "`us-west`"} {
		if !strings.Contains(noRegion, want) {
			t.Errorf("README does not say what an undeclared region does: missing %q", want)
		}
	}
	if strings.Contains(noRegion, "--region eu-west") {
		t.Error("README carries a region with none declared")
	}
}

// The `pcc` CLI form is retired; nothing the compiler emits may still use it.
func TestPipecatArtifactUsesCurrentCLIName(t *testing.T) { // FR-007
	for _, file := range pipecatArtifact(t, []string{"eu-west"}).Files {
		for _, retired := range []string{"pcc deploy", "pcc secrets", "pcc auth", "pcc agent"} {
			if strings.Contains(string(file.Content), retired) {
				t.Errorf("%s uses the retired CLI form %q", file.Path, retired)
			}
		}
	}
}

func TestPipecatReportCarriesRegion(t *testing.T) { // FR-020
	var withRegion map[string]any
	if err := json.Unmarshal([]byte(artifactFile(t, pipecatArtifact(t, []string{"eu-west"}), "compile-report.json")), &withRegion); err != nil {
		t.Fatal(err)
	}
	got, ok := withRegion["deployment_regions"].([]any)
	if !ok || len(got) != 1 || got[0] != "eu-west" {
		t.Fatalf("deployment_regions = %v, want a one-element list", withRegion["deployment_regions"])
	}
	var none map[string]any
	if err := json.Unmarshal([]byte(artifactFile(t, pipecatArtifact(t, nil), "compile-report.json")), &none); err != nil {
		t.Fatal(err)
	}
	if _, present := none["deployment_regions"]; present {
		t.Error("deployment_regions is in the report with no region declared")
	}
}

// No emitted file carries a secret value on either driver: the artifact holds
// UPPER_SNAKE names, and the operator's own .env holds the values.
func TestNeitherDriverEmitsSecretValues(t *testing.T) { // FR-002, SC-007
	agent := loadCompilerAgent(t)
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		tgt := targetByProvider(t, agent, provider)
		artifact, err := Generate(agent, tgt, target.Default())
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range artifact.Files {
			if file.Path != ".env.example" {
				continue
			}
			for _, line := range strings.Split(string(file.Content), "\n") {
				if name, value, found := strings.Cut(line, "="); found && strings.TrimSpace(value) != "" {
					t.Errorf("%s: %s carries a value in .env.example", provider, name)
				}
			}
		}
	}
}
