package generate

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// TestGenerateValidatesBeforeProviderDispatch pins the ordering: a package is
// validated before anything looks at which driver to run. An author with two
// problems should hear about the one in their package, not about the provider.
//
// It used to prove this with `vapi`, whose dispatch arm returned "driver is not
// implemented" — seeing "validation failed" instead showed validation ran
// first. That provider is retired, so the same ordering is now shown with a
// retired provider name, which is what an author upgrading would actually have
// written.
func TestGenerateValidatesBeforeProviderDispatch(t *testing.T) { // V17
	agent := loadCompilerAgent(t)
	agent.Conversation.ThinkingAudio = ir.ThinkingSubtle
	_, err := Generate(agent, retiredTarget(agent, ir.Provider("vapi")), target.Default())
	if err == nil || !strings.Contains(err.Error(), "validation failed") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("dispatch ran before validation: %v", err)
	}
}

// TestGenerateRefusesRetiredProviderWithAMigrationMessage is FR-022: "unknown"
// is true and useless for a word that used to work.
func TestGenerateRefusesRetiredProviderWithAMigrationMessage(t *testing.T) {
	agent := loadCompilerAgent(t)
	_, err := Generate(agent, retiredTarget(agent, ir.Provider("deepgram")), target.Default())
	if err == nil {
		t.Fatal("a retired provider must be refused")
	}
	for _, want := range []string{"retired", "livekit", "pipecat"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q must mention %q", err, want)
		}
	}
	// The model vendor of the same name is untouched, and the message has to
	// say so, or an author reads this as losing their STT provider too.
	if !strings.Contains(err.Error(), "model vendor") {
		t.Errorf("refusal must distinguish the retired target from the live model vendor: %v", err)
	}
}

// V17's warn-only-validation-is-kept check used to run against the two stub
// drivers, because a stub returned an error while still handing back the
// artifact notes. Both stubs are retired, so there is nothing left that both
// fails and produces notes; the property is covered by the real drivers, which
// return their notes on success.

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
	setConnectionRoute(pkg, "primary_phone", "cloud-websocket", "twilio")
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	// The platform-terminated route hosts nothing, so the plan it resolves is the
	// empty one. This used to be asserted against carrier-websocket, which ran a
	// uvicorn process behind three endpoints and no longer exists; what the
	// compile report has to prove is that the plan comes from the resolved facts,
	// and an empty plan proves that just as well as a full one.
	plan := TelephonyRuntimePlanFor(agent.Targets["pipecat"])
	if plan == nil || len(plan.Processes) != 0 || len(plan.PublicEndpoints) != 0 {
		t.Fatalf("runtime plan = %#v", plan)
	}
	if strings.Join(plan.RequiredEnv, ",") != "TWILIO_ACCOUNT_SID,TWILIO_AUTH_TOKEN,TWILIO_PHONE_NUMBER" {
		t.Fatalf("required env = %v", plan.RequiredEnv)
	}
	// One service and no coordination store: this route keeps no shared control
	// record, which is the other half of hosting nothing.
	if got := strings.Join(plan.Services, ","); got != "application" || len(plan.Reasons) == 0 {
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

// retiredTarget builds a target naming a provider this repository no longer
// accepts. It cannot come from the fixture, which only carries live providers —
// which is the point: this is what an author's package looks like after the
// provider they were using was retired.
func retiredTarget(agent *ir.Agent, provider ir.Provider) ir.Target {
	resolved := compilerTarget(agent, ir.ProviderPipecat)
	resolved.Provider = provider
	resolved.Name = string(provider)
	return resolved
}

func enableLangfuse(agent *ir.Agent) {
	agent.Tracing = &ir.Tracing{Provider: "langfuse"}
}

func enableCoval(agent *ir.Agent) {
	agent.Tracing = &ir.Tracing{Provider: "coval"}
}
