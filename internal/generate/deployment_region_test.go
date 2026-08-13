package generate

import (
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/target"
)

// deployment_region (SCHEMA N18) is forwarded as declared: Pipecat lowers it to
// the pcc-deploy.toml region key, LiveKit to the README lk-agent-create flag.
// Both omit it entirely when unset — no default region is invented.

func TestDeploymentRegionPipecat(t *testing.T) {
	agent := loadCompilerAgent(t)
	tgt := targetByProvider(t, agent, ir.ProviderPipecat)

	tgt.DeploymentRegions = []string{"eu-west1"}
	set, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	if got := artifactFile(t, set, "pcc-deploy.toml"); !strings.Contains(got, `region = "eu-west1"`) {
		t.Errorf("set: pcc-deploy.toml missing region:\n%s", got)
	}

	tgt.DeploymentRegions = nil
	unset, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	if got := artifactFile(t, unset, "pcc-deploy.toml"); strings.Contains(got, "region =") {
		t.Errorf("unset: pcc-deploy.toml must not emit a region:\n%s", got)
	}
}

func TestDeploymentRegionLiveKit(t *testing.T) {
	agent := loadCompilerAgent(t)
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)

	tgt.DeploymentRegions = []string{"eu-central"}
	set, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	setREADME := artifactFile(t, set, "README.md")
	if !strings.Contains(setREADME, "lk agent create --region eu-central") {
		t.Errorf("set: README missing lk-agent-create region flag:\n%s", setREADME)
	}
	if strings.Contains(setREADME, "\n\n\n") {
		t.Errorf("set: README has a doubled blank line:\n%s", setREADME)
	}

	tgt.DeploymentRegions = nil
	unset, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	got := artifactFile(t, unset, "README.md")
	if strings.Contains(got, "lk agent create --region") {
		t.Errorf("unset: README must not pin a region on the create command:\n%s", got)
	}
	if !strings.Contains(got, "lk agent create") {
		t.Errorf("unset: README still documents the deploy command:\n%s", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("unset: README has a doubled blank line:\n%s", got)
	}
}
