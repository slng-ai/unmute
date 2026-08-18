package generate

import (
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

var composeInjectedEnvironment = regexp.MustCompile(`(?m)^      - ([A-Z][A-Z0-9_]*)=.*$`)

func assertComposeLocalEnvironment(t *testing.T, compose string, plan *TelephonyRuntimePlan) {
	t.Helper()
	seen := make(map[string]bool)
	for _, match := range composeInjectedEnvironment.FindAllStringSubmatch(compose, -1) {
		seen[match[1]] = true
	}
	if strings.Contains(compose, "redis:6379") {
		seen["REDIS_URL"] = true
	}
	got := slices.Sorted(maps.Keys(seen))
	// Every name the Compose graph injects must be one the route says is
	// supplied rather than authored, or the generated file is quietly setting
	// something .env.example told the reader to set. The reverse is not required:
	// `unmute dev` mints the public URL and the outbound token itself, so they
	// are supplied without appearing here.
	for _, name := range got {
		if !slices.Contains(plan.LocalEnvironment, name) {
			t.Fatalf("Compose injects %q, which the telephony plan does not list as supplied: %v", name, plan.LocalEnvironment)
		}
	}
}

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
	// LiveKit and Pipecat are real drivers now; two stubs remain.
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
	enablePackageTelephony(pkg)
	configured := pkg.Targets["pipecat"]
	configured.Connection = "primary_phone"
	setConnectionRoute(pkg, "primary_phone", "carrier-websocket", "twilio")
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
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
	if got := strings.Join(plan.Services, ","); got != "application,redis" || len(plan.Reasons) == 0 {
		t.Fatalf("coordination graph = %#v", plan)
	}
	files, err := withTelephonyReport([]File{{Path: "compile-report.json", Content: []byte(`{"target":"pipecat","required_env":["OPENAI_API_KEY"]}`)}}, plan)
	if err != nil {
		t.Fatal(err)
	}
	report := string(files[0].Content)
	for _, want := range []string{`"telephony"`, `"carrier": "twilio"`, `"services"`, `"coordination_reasons"`, `"coordination": "shared"`, `"smoke": false`} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %s:\n%s", want, report)
		}
	}
	if !slices.Contains(plan.RequiredEnv, "OPENAI_API_KEY") {
		t.Fatalf("runtime plan did not merge generated environment: %v", plan.RequiredEnv)
	}
}

func TestTelephonyRuntimePlanForIsAThinCopy(t *testing.T) { // telephony I.plan, B11
	resolved := ir.Target{Telephony: &ir.TelephonyPlan{
		Key:                 ir.TelephonyKey{Provider: ir.ProviderPipecat, Transport: "custom", Carrier: "custom"},
		Processes:           []ir.TelephonyProcess{{Name: "custom", Command: []string{"custom-command"}}},
		PublicEndpoints:     []ir.TelephonyEndpoint{{Name: "custom", Method: "PATCH", Path: "/custom"}},
		RequiredEnvironment: []string{"CUSTOM_REQUIRED"}, LocalEnvironment: []string{"CUSTOM_REQUIRED"},
		AutoWebhookEndpoint: "custom",
		Environment:         map[string]string{"custom_key": "CUSTOM_REQUIRED"},
		ManualSteps:         []string{"custom setup"}, Services: []string{"custom-service"},
	}}
	runtime := TelephonyRuntimePlanFor(resolved)
	if runtime == nil || runtime.Processes[0].Command[0] != "custom-command" || runtime.PublicEndpoints[0].Path != "/custom" {
		t.Fatalf("runtime plan re-derived resolved facts: %#v", runtime)
	}
	if strings.Join(runtime.RequiredEnv, ",") != "CUSTOM_REQUIRED" || strings.Join(runtime.LocalEnvironment, ",") != "CUSTOM_REQUIRED" || strings.Join(runtime.ManualSteps, ",") != "custom setup" {
		t.Fatalf("runtime plan did not copy resolved facts: %#v", runtime)
	}
	if runtime.AutoWebhookEndpoint != "custom" || runtime.Environment["custom_key"] != "CUSTOM_REQUIRED" {
		t.Fatalf("runtime plan did not copy dev facts: %#v", runtime)
	}
	runtime.Processes[0].Command[0] = "mutated"
	if resolved.Telephony.Processes[0].Command[0] != "custom-command" {
		t.Fatal("runtime plan aliases the IR process command slice")
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
