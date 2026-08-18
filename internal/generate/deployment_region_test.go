package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
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

func TestRegionalInfrastructureExample(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "regional-infrastructure"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}

	livekit, err := Generate(agent, agent.Targets["livekit"], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	livekitPython := artifactFile(t, livekit, "agent.py")
	for _, want := range []string{
		"slng.STT(",
		`world_part_override="eu"`,
		"slng.TTS(",
		`region_override="eu-north-1"`,
	} {
		if !strings.Contains(livekitPython, want) {
			t.Errorf("LiveKit agent.py missing %q", want)
		}
	}
	if got := strings.Count(livekitPython, `world_part_override="eu"`); got != 2 {
		t.Errorf("LiveKit agent.py has %d world-part overrides, want one for STT and one for TTS", got)
	}
	if got := artifactFile(t, livekit, "README.md"); !strings.Contains(got, "lk agent create --region eu-central") {
		t.Errorf("LiveKit README missing eu-central deploy command:\n%s", got)
	}
	if got := artifactFile(t, livekit, "pyproject.toml"); !strings.Contains(got, `"livekit-plugins-slng>=1.6.7"`) {
		t.Errorf("LiveKit pyproject.toml does not require regional override support:\n%s", got)
	}

	pipecat, err := Generate(agent, agent.Targets["pipecat"], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	pipecatPython := artifactFile(t, pipecat, "bot.py")
	for _, want := range []string{
		"SlngSTTService(",
		`world_part_override="eu"`,
		"SlngTTSService(",
		`region_override="eu-north-1"`,
	} {
		if !strings.Contains(pipecatPython, want) {
			t.Errorf("Pipecat bot.py missing %q", want)
		}
	}
	if got := strings.Count(pipecatPython, `world_part_override="eu"`); got != 2 {
		t.Errorf("Pipecat bot.py has %d world-part overrides, want one for STT and one for TTS", got)
	}
	if got := artifactFile(t, pipecat, "pcc-deploy.toml"); !strings.Contains(got, `region = "eu-central"`) {
		t.Errorf("Pipecat pcc-deploy.toml missing eu-central:\n%s", got)
	}
}
