package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// TestEmittedPipecatBotAvoidsCurrentDeprecations holds every Pipecat
// deprecation the emitted code could take the shape of. Each would print a
// framework warning the author cannot fix, on every call.
//
// Two arrived in 1.9.0 and one in 1.10.0. They stay together in one gate rather
// than one per release, so a reader sees the whole list and a fourth has an
// obvious place to go.
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
//
// The third, from 1.10.0, is Speechmatics' `operating_point`, replaced by
// `model`. Passing it still selects the model and warns, and 2.0.0 removes it.
// The compiler refuses it as an authored `params:` key, so no package can reach
// this by writing one; this asserts the emitter does not produce it by some
// other route, which is a different question and the one a gate answers.
func TestEmittedPipecatBotAvoidsCurrentDeprecations(t *testing.T) {
	// Two shipped examples, plus the two fixtures that bind the one vendor a
	// 1.10.0 deprecation names: an example binding none of them would pass this
	// while saying nothing about the shape it is meant to catch.
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
			if strings.Contains(content, "operating_point") {
				t.Errorf("%s/%s writes operating_point, deprecated since Pipecat 1.10.0 and removed in 2.0.0; write model instead", pkg, file.Path)
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

// TestEmittedSpeechmaticsAvoidsTheRetiredConstructor is the vendor-specific half
// of the gate above, on the two fixtures that actually bind Speechmatics.
//
// 1.10.0 moved this service onto Agent STT and left a deprecated path behind:
// `params=` on the constructor, replaced by `settings=`. The emitted call has to
// take the current one, and it has to reach the new Settings class rather than
// the InputParams shape that still exists for compatibility.
func TestEmittedSpeechmaticsAvoidsTheRetiredConstructor(t *testing.T) {
	for _, fixture := range []string{"speechmatics_local", "speechmatics_listen"} {
		bot := artifactFile(t, generateFor(t, fixture, ir.ProviderPipecat), "bot.py")
		call := bot[strings.Index(bot, "return SpeechmaticsSTTService("):]
		if end := strings.Index(call, "\n    )"); end > 0 {
			call = call[:end]
		}
		if !strings.Contains(call, "settings=SpeechmaticsSTTService.Settings(") {
			t.Errorf("%s: the service is not constructed with settings=:\n%s", fixture, call)
		}
		for _, absent := range []string{"params=SpeechmaticsSTTService.InputParams(", "params=", "operating_point"} {
			if strings.Contains(call, absent) {
				t.Errorf("%s: the service call carries %q, which pipecat 1.10.0 deprecates:\n%s", fixture, absent, call)
			}
		}
	}
}
