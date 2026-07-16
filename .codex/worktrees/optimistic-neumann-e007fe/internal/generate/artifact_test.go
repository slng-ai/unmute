package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

func TestGenerateValidatesBeforeProviderDispatch(t *testing.T) { // V17
	agent := loadCompilerAgent(t)
	agent.Conversation.ThinkingAudio = ir.ThinkingSubtle
	_, err := Generate(agent, compilerTarget(agent, ir.ProviderVapi), target.Default())
	if err == nil || !strings.Contains(err.Error(), "validation failed") || strings.Contains(err.Error(), "driver is not implemented") {
		t.Fatalf("got %v", err)
	}
}

func TestGenerateWarnOnlyReachesEveryProviderStub(t *testing.T) { // V17
	agent := loadCompilerAgent(t)
	for _, provider := range []ir.Provider{
		ir.ProviderLiveKit, ir.ProviderPipecat, ir.ProviderVapi, ir.ProviderElevenLabs, ir.ProviderDeepgram,
	} {
		t.Run(string(provider), func(t *testing.T) {
			artifact, err := Generate(agent, compilerTarget(agent, provider), target.Default())
			if err == nil || !strings.Contains(err.Error(), string(provider)+" driver is not implemented") {
				t.Fatalf("got %v", err)
			}
			if artifact.Kind == "" || len(artifact.Notes.Warnings) == 0 || len(artifact.Notes.ForwardedBindings) == 0 || len(artifact.Notes.Sizing) == 0 {
				t.Fatalf("warn-only validation was discarded: %#v", artifact)
			}
		})
	}
}

func TestApplyPlanIsOrderedAndBranchAware(t *testing.T) {
	plan := ApplyPlan{Steps: []ApplyStep{
		{Method: "POST", Endpoint: "/assistants", CaptureID: "assistant"},
		{Method: "POST", Endpoint: "/squads", Branch: "preview"},
	}}
	if plan.Steps[0].CaptureID != "assistant" || plan.Steps[1].Branch != "preview" {
		t.Fatalf("plan = %#v", plan)
	}
}

func loadCompilerAgent(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func compilerTarget(agent *ir.Agent, provider ir.Provider) ir.Target {
	for _, resolved := range agent.Targets {
		if resolved.Provider == provider {
			return resolved
		}
	}
	panic("target not found: " + provider)
}
