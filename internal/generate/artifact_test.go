package generate

import (
	"path/filepath"
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
	agent, err := buildWithState(t, pkg)
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
