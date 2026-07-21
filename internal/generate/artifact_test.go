package generate

import (
	"path/filepath"
	"slices"
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
	// LiveKit, Pipecat, and ElevenLabs are real drivers now; two stubs remain.
	for _, provider := range []ir.Provider{
		ir.ProviderVapi, ir.ProviderDeepgram,
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

func TestTelephonyRuntimePlanAndCompileReportUseResolvedFacts(t *testing.T) { // telephony V7, V19
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	configured := pkg.Targets["pipecat"]
	configured.Transport = "carrier-websocket"
	configured.Carrier = "twilio"
	configured.Connection = "primary_phone"
	pkg.Targets["pipecat"] = configured
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	plan := TelephonyRuntimePlanFor(agent.Targets["pipecat"])
	if plan == nil || len(plan.Processes) != 1 || len(plan.PublicEndpoints) != 3 {
		t.Fatalf("runtime plan = %#v", plan)
	}
	if strings.Join(plan.RequiredEnv, ",") != "REDIS_URL,TWILIO_ACCOUNT_SID,TWILIO_AUTH_TOKEN,TWILIO_PHONE_NUMBER,UNMUTE_PUBLIC_URL" {
		t.Fatalf("required env = %v", plan.RequiredEnv)
	}
	if got := strings.Join(plan.Processes[0].Command, " "); got != "uv run uvicorn telephony:app --host 0.0.0.0 --port 7860" {
		t.Fatalf("process command = %q", got)
	}
	files, err := withTelephonyReport([]File{{Path: "compile-report.json", Content: []byte(`{"target":"pipecat","required_env":["OPENAI_API_KEY"]}`)}}, plan)
	if err != nil {
		t.Fatal(err)
	}
	report := string(files[0].Content)
	for _, want := range []string{`"telephony"`, `"carrier": "twilio"`, `"coordination": "shared"`, `"smoke": false`} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %s:\n%s", want, report)
		}
	}
	if !slices.Contains(plan.RequiredEnv, "OPENAI_API_KEY") {
		t.Fatalf("runtime plan did not merge generated environment: %v", plan.RequiredEnv)
	}
}

func loadCompilerAgent(t *testing.T) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
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

func enableLangfuse(agent *ir.Agent) {
	agent.Tracing = &ir.Tracing{Provider: "langfuse"}
}
