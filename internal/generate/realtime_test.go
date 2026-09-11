package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// TestRealtimeEmitsOneLiveServiceAndNothingItReplaces reads the emitted bot for
// the realtime_live fixture: one live service configured with the prompt, the
// voice and the backend, the agent's tools on the context, the greeting as the
// session's opening instruction, the idle nudge as spoken commentary, and none
// of the transcriber, synthesizer or local turn machinery.
func TestRealtimeEmitsOneLiveServiceAndNothingItReplaces(t *testing.T) {
	artifact := generateFor(t, "realtime_live", ir.ProviderPipecat)
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"from pipecat.services.openai.live.llm import OpenAILiveLLMService",
		"from pipecat.services.openai.responses.llm import OpenAIResponsesLLMService",
		"from pipecat.services.openai.live import events as live_events",
		"def build_desk_live():",
		"return OpenAILiveLLMService(",
		`api_key=os.environ["OPENAI_API_KEY"],`,
		`delegation=OpenAILiveLLMService.ResponsesDelegation(settings=OpenAIResponsesLLMService.Settings(model="gpt-5.6-terra")),`,
		"settings=OpenAILiveLLMService.Settings(",
		`voice="marin",`,
		`model="gpt-live-1",`,
		"system_instruction=DESK_PROMPT,",
		"context = LLMContext(tools=[lookup_customer, check_availability])",
		"vad_analyzer=SileroVADAnalyzer(),",
		"live = dev.observe_live(build_desk_live())",
		"            user_aggregator,\n            live,\n            transport.output(),",
		"live_events.SessionCommentaryAppendEvent(",
		`LLMMessagesAppendFrame([{"role": "developer", "content": "Open the call with this greeting, in these words as far as you can: " + "Hi, this is Sage and Stone Salon. How can I help?"}]),`,
		"LLMRunFrame(),",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py is missing %q", want)
		}
	}
	for _, absent := range []string{
		"def build_stt", "_tts(", "TTSSpeakFrame", "VADParams", "LocalSmartTurnAnalyzerV3",
		"user_turn_strategies=", "from pipecat.turns", "LLMWorker", "BusBridgeProcessor",
		"LLMUpdateSettingsFrame", "user_mute_strategies",
	} {
		if strings.Contains(bot, absent) {
			t.Errorf("bot.py still carries %q, which a live model replaces", absent)
		}
	}
	for _, tool := range []string{"async def lookup_customer(params: FunctionCallParams", "async def check_availability(params: FunctionCallParams"} {
		if !strings.Contains(bot, tool) {
			t.Errorf("bot.py does not register %q the way the inline shape does", tool)
		}
	}
	if pyproject := artifactFile(t, artifact, "pyproject.toml"); !strings.Contains(pyproject, "openai,") {
		t.Errorf("pyproject.toml must carry the openai extra:\n%s", pyproject)
	}
	if env := artifactFile(t, artifact, ".env.example"); !strings.Contains(env, "OPENAI_API_KEY") {
		t.Errorf(".env.example must name OPENAI_API_KEY:\n%s", env)
	}
	readme := artifactFile(t, artifact, "README.md")
	for _, want := range []string{
		"## The live model", "spoken in the model's own words", "`fast`",
		"Responses API inside the same live session", "Fixed for the session", "After 20s of silence",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md is missing %q", want)
		}
	}
	if strings.Contains(readme, "## Turn taking") {
		t.Error("README.md explains turn taking, which no setting of a realtime package decides")
	}
	report := artifactFile(t, artifact, "compile-report.json")
	if !strings.Contains(report, "realtime: openai via OpenAILiveLLMService") {
		t.Errorf("compile-report.json does not name the live service:\n%s", report)
	}
}

// TestRealtimeWithoutABackendRunsAlone: no think entry means no delegation and
// no Responses import, and the runbook says the model declines tool work.
func TestRealtimeWithoutABackendRunsAlone(t *testing.T) {
	agent := agentFor(t, "realtime_live")
	live := agent.Models["live"]
	live.Think = ""
	agent.Models["live"] = live
	desk := agent.Agents["desk"]
	desk.Tools = nil
	agent.Agents["desk"] = desk
	tgt := targetByProvider(t, agent, ir.ProviderPipecat)
	binding := tgt.Models.Realtime["live"]
	binding.Think = ""
	tgt.Models.Realtime["live"] = binding
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, absent := range []string{"delegation=", "OpenAIResponsesLLMService"} {
		if strings.Contains(bot, absent) {
			t.Errorf("bot.py carries %q with no backend named", absent)
		}
	}
	if readme := artifactFile(t, artifact, "README.md"); !strings.Contains(readme, "No think entry is named") {
		t.Error("README.md does not say the live model runs alone")
	}
}

// TestCascadedPackagesGainNothingFromRealtime: a package binding think, listen
// and speak emits no live-model line.
func TestCascadedPackagesGainNothingFromRealtime(t *testing.T) {
	bot := artifactFile(t, generateFor(t, "simple-prompt", ir.ProviderPipecat), "bot.py")
	for _, absent := range []string{"OpenAILiveLLMService", "live_events", "_live()", "ResponsesDelegation"} {
		if strings.Contains(bot, absent) {
			t.Errorf("a cascaded package gained %q", absent)
		}
	}
}
