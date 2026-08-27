package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// What a deployed thing is called comes from the package, never from the target
// instance alone.
//
// Every package on a target calls it after the platform, because that is what
// the docs, the examples and the console all name it. Naming a deployment after
// it meant, on SLNG and Pipecat Cloud, that two packages in one organisation
// claimed one live agent and the second deploy replaced the first; and on
// LiveKit, that two packages in one project registered one worker name and
// fought over which answered a call.
//
// The three drivers now read ir.Agent.DeployName. This holds each emitted
// identity to it, and holds the labels to the package name, so the split cannot
// quietly collapse back into one field.
func TestDeployedIdentitiesComeFromThePackage(t *testing.T) {
	for _, test := range []struct {
		fixture  string
		provider ir.Provider
		// identity is every emitted place a platform resolves this deployment by
		// name; label is every place the emitted project names itself.
		identity func(files map[string]string) map[string]string
		label    func(files map[string]string) map[string]string
	}{
		{
			fixture: "slng_core", provider: ir.ProviderSlng,
			identity: func(files map[string]string) map[string]string {
				name, _ := slngBodyOf(t, files)["name"].(string)
				return map[string]string{"agent.json name": name}
			},
		},
		{
			fixture: "safe_core", provider: ir.ProviderPipecat,
			identity: func(files map[string]string) map[string]string {
				return map[string]string{
					"pcc-deploy.toml agent_name": tomlString(files["pcc-deploy.toml"], "agent_name"),
					"pcc-deploy.toml secret_set": strings.TrimSuffix(tomlString(files["pcc-deploy.toml"], "secret_set"), "-secrets"),
				}
			},
			label: func(files map[string]string) map[string]string {
				return map[string]string{"pyproject name": tomlString(files["pyproject.toml"], "name")}
			},
		},
		{
			fixture: "safe_core", provider: ir.ProviderLiveKit,
			identity: func(files map[string]string) map[string]string {
				// Worker registration, which is what a dispatch rule matches. The
				// dispatch rule itself is only emitted for a phone route, and
				// TestLiveKitSIPEmitsTopologyAndHydratesContextBeforeGreeting holds
				// that one to the same string.
				_, registered, _ := strings.Cut(files["agent.py"], `@server.rtc_session(agent_name="`)
				registered, _, _ = strings.Cut(registered, `"`)
				return map[string]string{"rtc_session agent_name": registered}
			},
			label: func(files map[string]string) map[string]string {
				return map[string]string{"pyproject name": strings.TrimPrefix(tomlString(files["pyproject.toml"], "name"), "unmute-")}
			},
		},
	} {
		t.Run(test.fixture+"/"+string(test.provider), func(t *testing.T) {
			agent, tgt, files := compileFixture(t, test.fixture, test.provider)
			want := agent.DeployName(tgt)
			if want == tgt.Name {
				t.Fatalf("this test needs the two to differ; both are %q", want)
			}
			for where, got := range test.identity(files) {
				if got == "" {
					t.Fatalf("%s is empty: the assertion below would pass on nothing", where)
				}
				if got != want {
					t.Errorf("%s = %q, want the package's deploy name %q", where, got, want)
				}
			}
			if test.label == nil {
				return
			}
			for where, got := range test.label(files) {
				if got != agent.Name {
					t.Errorf("%s = %q, want the package's own name %q", where, got, agent.Name)
				}
			}
		})
	}
}

// Two packages with the conventional target name are the collision itself. The
// slng fixtures are the pair that used to produce it.
func TestTwoPackagesOnOneTargetNameDeployTwoAgents(t *testing.T) {
	seen := map[string]string{}
	for _, fixture := range []string{"slng_core", "slng_tools"} {
		agent, tgt, files := compileFixture(t, fixture, ir.ProviderSlng)
		if tgt.Name != "slng" {
			t.Fatalf("%s: this test needs both fixtures to use the conventional target name; got %q", fixture, tgt.Name)
		}
		name, _ := slngBodyOf(t, files)["name"].(string)
		if previous, clash := seen[name]; clash {
			t.Errorf("%s and %s both push an agent called %q, so the second replaces the first", previous, fixture, name)
		}
		seen[name] = fixture
		// The runbook is where an author reads which agent they are about to
		// replace, so it names the same one.
		if !strings.Contains(files["README.md"], "pushes\n**"+name+"**") {
			t.Errorf("%s: the runbook does not name the agent it creates", fixture)
		}
		_ = agent
	}
}

// The emitted command an author copies has to name the target instance, because
// that is what `--target` takes. It reads .Target, and reading .AgentName there
// was a real bug: the two were the same string until they were not.
func TestEmittedTargetCommandsNameTheTarget(t *testing.T) {
	_, tgt, files := compileFixture(t, "safe_core", ir.ProviderLiveKit)
	if !strings.Contains(files["README.md"], "--target "+tgt.Name+"\n") {
		t.Errorf("the livekit runbook does not tell the author to run --target %q", tgt.Name)
	}
}

func compileFixture(t *testing.T, fixture string, provider ir.Provider) (*ir.Agent, ir.Target, map[string]string) {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	tgt := targetByProvider(t, agent, provider)
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	files := map[string]string{}
	for _, file := range artifact.Files {
		files[file.Path] = string(file.Content)
	}
	return agent, tgt, files
}

func tomlString(content, key string) string {
	for line := range strings.SplitSeq(content, "\n") {
		name, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(name) != key {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"`)
	}
	return ""
}
