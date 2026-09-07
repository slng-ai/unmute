package generate

import (
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
	"strings"
	"testing"
)

// emittedText returns every text file every declared target of a package
// writes, keyed by "<target>/<path>", so one gate reads a whole compile.
func emittedText(t *testing.T, agent *ir.Agent) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, tgt := range agent.Targets {
		artifact, err := Generate(agent, tgt, target.Default())
		if err != nil {
			t.Fatalf("generate %s: %v", name, err)
		}
		for _, file := range artifact.Files {
			if strings.HasSuffix(file.Path, ".py") || strings.HasSuffix(file.Path, ".md") {
				out[name+"/"+file.Path] = string(file.Content)
			}
		}
	}
	return out
}

func TestRetiredInputsEmitNothing(t *testing.T) {
	for name, body := range emittedText(t, loadTypedState(t)) {
		for _, retired := range []string{"_INPUT_TYPES", "_typed_inputs", "Request block", "Conversation info:"} {
			if strings.Contains(body, retired) {
				t.Errorf("%s still emits %q", name, retired)
			}
		}
	}
}
