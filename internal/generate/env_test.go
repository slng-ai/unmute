package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// exampleArtifact compiles one shipped example for one provider.
func exampleArtifact(t *testing.T, example string, provider ir.Provider) Artifact {
	t.Helper()
	pkg, err := spec.Load(examplePackagePath(example))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatalf("%s on %s: %v", example, provider, err)
	}
	return artifact
}

func TestPipecatLogLevelIsOptionalDevEnvironment(t *testing.T) {
	artifact := exampleArtifact(t, "simple-prompt", ir.ProviderPipecat)
	if compose := artifactFile(t, artifact, "compose.dev.yaml"); !strings.Contains(compose, "- UNMUTE_LOG_LEVEL") {
		t.Errorf("compose.dev.yaml does not pass through UNMUTE_LOG_LEVEL:\n%s", compose)
	}
	for _, path := range []string{".env.example", "compile-report.json"} {
		if content := artifactFile(t, artifact, path); strings.Contains(content, "UNMUTE_LOG_LEVEL") {
			t.Errorf("%s treats optional UNMUTE_LOG_LEVEL as required:\n%s", path, content)
		}
	}
}

// TestDeclaredFieldsReachTheGeneratedProject holds the class of defect this
// whole feature exists to remove, on the five instances Wave C's adversarial
// agents found after the first fixes had landed: a field the author declares,
// both commands at exit 0, and nothing in the generated project that carries it.
//
// Each row is a package shape and the string the emitted Python must contain.
// A row that fails means an author is being told their declaration took effect
// when it did not.
func TestDeclaredFieldsReachTheGeneratedProject(t *testing.T) {
	off := false
	for _, test := range []struct {
		name     string
		mutate   func(*ir.Agent)
		provider ir.Provider
		file     string
		want     string
	}{
		{
			// The author says the caller cannot talk over the agent. Pipecat
			// computed the field and rendered it nowhere, so the emitted agent
			// was fully interruptible while the LiveKit build honoured it.
			name:     "interruption disabled reaches pipecat",
			mutate:   func(a *ir.Agent) { a.Conversation.Interruption = &ir.Interruption{Enabled: &off} },
			provider: ir.ProviderPipecat,
			file:     "bot.py",
			want:     "AlwaysUserMuteStrategy()",
		},
		{
			// The nudge was emitted and the hangup was not, so an idle call was
			// prompted forever. On a phone line that is a billed open call.
			name: "inactivity end_after reaches pipecat",
			mutate: func(a *ir.Agent) {
				a.Conversation.Inactivity = &ir.Inactivity{NudgeAfter: "15s", EndAfter: "45s"}
			},
			provider: ir.ProviderPipecat,
			file:     "bot.py",
			want:     "_end_after(",
		},
		{
			// The docstring is the only thing the model reads when it decides
			// whether to call the tool, so an empty one is a dead control.
			name:     "a transfer with no when: still describes itself on pipecat",
			mutate:   func(a *ir.Agent) { setTransferWhen(a, "to_billing", "") },
			provider: ir.ProviderPipecat,
			file:     "bot.py",
			want:     "Transfer the caller to the billing agent.",
		},
		{
			// Whitespace was strictly worse than nothing, on both drivers.
			name:     "a whitespace when: does not defeat the default",
			mutate:   func(a *ir.Agent) { setTransferWhen(a, "to_billing", "   ") },
			provider: ir.ProviderLiveKit,
			file:     "agent.py",
			want:     "Transfer the caller to the billing.",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			agent := loadCompilerAgent(t)
			test.mutate(agent)
			artifact, err := Generate(agent, targetByProvider(t, agent, test.provider), target.Default())
			if err != nil {
				t.Fatalf("the package must still compile: %v", err)
			}
			if got := artifactFile(t, artifact, test.file); !strings.Contains(got, test.want) {
				t.Errorf("%s does not carry %q, so the declaration compiled green and did nothing", test.file, test.want)
			}
		})
	}
}

// setTransferWhen rewrites one agent_transfer's `when:` in place, keeping every
// other field the fixture set. Replacing the whole control would drop its
// context, which is required and is not what these rows are about.
func setTransferWhen(agent *ir.Agent, name, when string) {
	transfer, ok := agent.Controls[name].(*ir.AgentTransfer)
	if !ok {
		return
	}
	transfer.When = when
	agent.Controls[name] = transfer
}
