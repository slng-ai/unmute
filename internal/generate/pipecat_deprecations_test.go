package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// TestEmittedPipecatBotAvoidsThe190Deprecations holds the two Pipecat 1.9.0
// deprecations that would fire on every call if the emitted code ever took the
// shape they name.
//
// The first is the system prompt as a leading "system" message in LLMContext
// (adapters/base_llm_adapter.py, `_warn_context_system_message`): the framework
// warns once per adapter, under an `always` filter so the default DeprecationWarning
// suppression does not hide it, and 2.0.0 stops reading the message. The emitted
// bot has always put the prompt on the service as `system_instruction` and
// changed it through `LLMUpdateSettingsFrame`, so nothing here needs to move;
// this gate is what keeps a future template from moving it back. The one place
// the tree spells that role is tracing.py, which re-inserts the instruction into
// a copy of the messages for the trace viewer and never into the context.
//
// The second is `LatencyBreakdown.chronological_events()`, replaced by
// `turn_contribution_lines()` and the `contributions` list the dev page reads.
func TestEmittedPipecatBotAvoidsThe190Deprecations(t *testing.T) {
	for _, pkg := range []string{"salon-concierge", "customer-intake"} {
		agent := loadExample(t, pkg)
		artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range artifact.Files {
			if !strings.HasSuffix(file.Path, ".py") {
				continue
			}
			content := string(file.Content)
			if strings.Contains(content, "chronological_events(") {
				t.Errorf("%s/%s calls LatencyBreakdown.chronological_events, deprecated since Pipecat 1.9.0; read .contributions instead", pkg, file.Path)
			}
			for _, line := range strings.Split(content, "\n") {
				if !strings.Contains(line, `"role": "system"`) {
					continue
				}
				if strings.HasSuffix(file.Path, "tracing.py") && strings.Contains(line, "messages.insert(0,") {
					continue // the trace viewer's copy, never the context
				}
				t.Errorf("%s/%s seeds a system message the framework has deprecated as a prompt carrier; set system_instruction on the service:\n%s", pkg, file.Path, line)
			}
		}
	}
}
