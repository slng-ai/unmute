package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

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

func compileFixture(t *testing.T, fixture string, provider ir.Provider) (*ir.Agent, ir.Target, map[string]string) {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := buildWithState(t, pkg)
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
