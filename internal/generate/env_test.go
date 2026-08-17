package generate

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// exampleArtifact compiles one shipped example for one provider.
func exampleArtifact(t *testing.T, example string, provider ir.Provider) Artifact {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", example))
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

// envFileNames lists the names an env file asks the reader for. A commented-out
// name is still the file mentioning it, and this file's whole job is to be a
// to-do list whose every line is a to-do, so both forms count.
func envFileNames(content string) []string {
	name := regexp.MustCompile(`(?m)^#?\s*([A-Z][A-Z0-9_]*)=`)
	var names []string
	for _, hit := range name.FindAllStringSubmatch(content, -1) {
		names = append(names, hit[1])
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// readmeEnvNames lists the names the emitted README asks the reader to set.
func readmeEnvNames(content string) []string {
	item := regexp.MustCompile("(?m)^- `([A-Z][A-Z0-9_]*)`")
	var names []string
	for _, hit := range item.FindAllStringSubmatch(content, -1) {
		names = append(names, hit[1])
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func TestSLNGRouterUsesEnvironmentNamesWithoutValues(t *testing.T) {
	const routerValue = "router-secret-must-not-leak"
	const upstreamValue = "upstream-secret-must-not-leak"
	t.Setenv("SLNG_API_KEY", routerValue)
	t.Setenv("OPENAI_API_KEY", upstreamValue)

	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "slng-context-router"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		provider ir.Provider
		file     string
	}{
		{provider: ir.ProviderPipecat, file: "bot.py"},
		{provider: ir.ProviderLiveKit, file: "agent.py"},
	} {
		t.Run(string(test.provider), func(t *testing.T) {
			artifact, err := Generate(agent, targetByProvider(t, agent, test.provider), target.Default())
			if err != nil {
				t.Fatal(err)
			}
			envExample := artifactFile(t, artifact, ".env.example")
			readmeNames := readmeEnvNames(artifactFile(t, artifact, "README.md"))
			for _, name := range []string{"OPENAI_API_KEY", "SLNG_API_KEY"} {
				if !slices.Contains(envFileNames(envExample), name) {
					t.Errorf(".env.example does not name %s", name)
				}
				if !slices.Contains(readmeNames, name) {
					t.Errorf("README does not name %s", name)
				}
			}
			runtime := artifactFile(t, artifact, test.file)
			for _, name := range []string{"OPENAI_API_KEY", "SLNG_API_KEY"} {
				if !strings.Contains(runtime, `os.environ["`+name+`"]`) {
					t.Errorf("%s does not read %s through the environment", test.file, name)
				}
			}
			for _, file := range artifact.Files {
				for _, value := range []string{routerValue, upstreamValue} {
					if strings.Contains(string(file.Content), value) {
						t.Errorf("%s contains an environment value", file.Path)
					}
				}
			}
		})
	}
}

// TestEnvExampleListsOnlyAuthorNames holds FR-018: `build/<target>/.env.example`
// contains only names the author supplies. Everything else is absent, not
// relabelled and not commented out.
//
// The classification already exists. internal/target/telephony.go carries
// LocallySuppliedEnvironment per route, cloned into the IR as LocalEnvironment,
// and three things ignore it: the LiveKit template labels rather than excludes,
// the Pipecat template does not read it at all, and UNMUTE_PUBLIC_URL and
// UNMUTE_OUTBOUND_TOKEN are missing from the data even though `unmute dev` mints
// both. So the same REDIS_URL is explained on one target and silently demanded
// on the other, from one piece of data (research D11).
func TestEnvExampleListsOnlyAuthorNames(t *testing.T) {
	authorSet := []string{
		"BILLING_PHONE_NUMBER", "OPENAI_API_KEY", "SIP_AUTH_PASSWORD", "SIP_AUTH_USERNAME",
		"SIP_FROM_NUMBER", "SIP_TRUNK_HOSTNAME", "SLNG_API_KEY", "SUPERVISOR_PHONE_NUMBER",
	}
	t.Run("livekit sip names exactly the eight the author sets", func(t *testing.T) {
		env := artifactFile(t, exampleArtifact(t, "livekit-human-transfer", ir.ProviderLiveKit), ".env.example")
		if got := envFileNames(env); !slices.Equal(got, authorSet) {
			t.Errorf(".env.example names %v, want exactly %v", got, authorSet)
		}
	})

	// Both drivers read one piece of data, so both must reach the same answer.
	// outbound-reminder is the sharpest fixture: it declares both targets, and
	// on Pipecat it is the carrier-websocket route whose REDIS_URL is supplied by
	// the Compose graph and by nothing the author writes.
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		t.Run("no locally supplied name survives on "+string(provider), func(t *testing.T) {
			env := artifactFile(t, exampleArtifact(t, "outbound-reminder", provider), ".env.example")
			for _, hidden := range []string{"REDIS_URL", "UNMUTE_PUBLIC_URL", "UNMUTE_OUTBOUND_TOKEN", "LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"} {
				if strings.Contains(env, hidden) {
					t.Errorf("%s asks for %s, which `unmute dev` sets locally and the platform sets at deploy time:\n%s", provider, hidden, env)
				}
			}
			if len(envFileNames(env)) == 0 {
				t.Error("the file names nothing at all, so this test would pass for the wrong reason")
			}
		})
	}

	// T013b: the README list and the env file are two views of one fact.
	t.Run("the README set-these list matches the env file", func(t *testing.T) {
		artifact := exampleArtifact(t, "outbound-reminder", ir.ProviderPipecat)
		env := envFileNames(artifactFile(t, artifact, ".env.example"))
		readme := readmeEnvNames(artifactFile(t, artifact, "README.md"))
		if !slices.Equal(env, readme) {
			t.Errorf("the env file asks for %v and the README asks for %v; they are two views of one fact", env, readme)
		}
	})

	// T013c: hiding is not deleting. The machine-readable form stays complete, so
	// an operator deploying by hand can still recover every name (FR-018e).
	t.Run("compile-report keeps every hidden name", func(t *testing.T) {
		artifact := exampleArtifact(t, "outbound-reminder", ir.ProviderPipecat)
		var report struct {
			RequiredEnv []string `json:"required_env"`
		}
		if err := json.Unmarshal([]byte(artifactFile(t, artifact, "compile-report.json")), &report); err != nil {
			t.Fatal(err)
		}
		for _, hidden := range []string{"REDIS_URL", "UNMUTE_PUBLIC_URL", "UNMUTE_OUTBOUND_TOKEN"} {
			if !slices.Contains(report.RequiredEnv, hidden) {
				t.Errorf("compile-report.json dropped %s; hiding a name from the env file must not delete it: %v", hidden, report.RequiredEnv)
			}
		}
	})
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

// TestRingTimeoutIsNotHardcoded holds one more of the same class, separately
// because it needs a telephony route. Writing `ring_timeout: 7s` used to emit
// `timeout="25"` and produce output byte-identical to a package that declared
// nothing at all.
func TestRingTimeoutIsNotHardcoded(t *testing.T) {
	build := func(t *testing.T, ring ir.Duration) string {
		t.Helper()
		pkg, err := spec.Load(filepath.Join("..", "..", "examples", "pipecat-human-transfer-twilio"))
		if err != nil {
			t.Fatal(err)
		}
		agent, err := ir.Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		for name, control := range agent.Controls {
			if transfer, ok := control.(*ir.HumanTransfer); ok {
				transfer.RingTimeout = ring
				agent.Controls[name] = transfer
			}
		}
		artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
		if err != nil {
			t.Fatal(err)
		}
		return artifactFile(t, artifact, "bot.py")
	}
	if got := build(t, "7s"); !strings.Contains(got, ", 7)") {
		t.Errorf("bot.py does not carry the declared 7s ring_timeout")
	}
	if got := build(t, ""); !strings.Contains(got, ", 25)") {
		t.Errorf("bot.py lost the 25s default when nothing was declared")
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
