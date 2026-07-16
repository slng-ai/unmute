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

func TestGenerateWarnOnlyReachesRemainingStubs(t *testing.T) { // V17
	agent := loadCompilerAgent(t)
	// LiveKit, Pipecat, and ElevenLabs are real drivers now; Vapi and Deepgram are still stubs.
	for _, provider := range []ir.Provider{ir.ProviderVapi, ir.ProviderDeepgram} {
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

func TestGeneratePipecatEmitsProject(t *testing.T) { // driver-pipecat T2, V17
	agent := loadCompilerAgent(t)
	artifact, err := Generate(agent, compilerTarget(agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatalf("pipecat generate: %v", err)
	}
	if artifact.Kind != CodeTarget {
		t.Fatalf("kind = %q", artifact.Kind)
	}
	want := map[string]bool{"bot.py": false, "pyproject.toml": false, "compile-report.json": false}
	for _, file := range artifact.Files {
		if _, ok := want[file.Path]; ok {
			want[file.Path] = true
		}
	}
	for path, found := range want {
		if !found {
			t.Errorf("missing generated file %q", path)
		}
	}
	if len(artifact.Notes.ForwardedBindings) == 0 || len(artifact.Notes.Sizing) == 0 {
		t.Fatalf("validate-derived notes were discarded: %#v", artifact.Notes)
	}
}

func TestApplyPlanIsOrdered(t *testing.T) {
	plan := ApplyPlan{Steps: []ApplyStep{
		{Method: "POST", Endpoint: "/assistants", CaptureID: "assistant"},
		{Method: "POST", Endpoint: "/squads"},
	}}
	if plan.Steps[0].CaptureID != "assistant" || plan.Steps[1].Endpoint != "/squads" {
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
