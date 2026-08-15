package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// reasonParamsArtifact compiles safe_core with one provider-specific param on
// its openai think model (fast_reasoning: provider openai, gpt-4o-mini,
// temperature 0.4). That is the shape both rules below are about: a param
// unmute never folds itself, sitting next to one it does.
func reasonParamsArtifact(t *testing.T, provider ir.Provider) Artifact {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	def := pkg.Agent.Models.Think["fast_reasoning"]
	def.Params = map[string]any{"reasoning_effort": "none"}
	pkg.Agent.Models.Think["fast_reasoning"] = def

	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return artifact
}

// TestReasonParamsOverflowIntoPipecatSettingsExtra pins the two live failures
// this split prevents. OpenAI returns 400 on /v1/chat/completions when a
// reasoning model is sent function tools without reasoning_effort="none", so
// the param has to reach the request; but pipecat 1.5.0's OpenAILLMSettings is
// a fixed dataclass with no reasoning_effort field, so passing it as a Settings
// kwarg raises TypeError at construction. Params unmute folds itself
// (temperature and friends) stay plain Settings fields; everything else rides
// the entry's overflow field, which OpenAILLMService merges into the body.
func TestReasonParamsOverflowIntoPipecatSettingsExtra(t *testing.T) {
	bot := artifactFile(t, reasonParamsArtifact(t, ir.ProviderPipecat), "bot.py")

	start := strings.Index(bot, "def build_intake_llm(")
	if start < 0 {
		t.Fatal("bot.py has no build_intake_llm; fixture drifted")
	}
	block := bot[start:]
	if end := strings.Index(block, "\n\n"); end > 0 {
		block = block[:end]
	}

	for _, want := range []struct {
		text string
		why  string
	}{
		{"settings=OpenAILLMService.Settings(", "pipecat's openai reason entry carries its params in Settings"},
		{"temperature=0.4,", "a folded param stays a plain Settings field"},
		{`extra={"reasoning_effort": "none"},`, "a non-folded param rides the entry's overflow field, or OpenAI 400s on tools"},
	} {
		if !strings.Contains(block, want.text) {
			t.Errorf("build_intake_llm is missing %q (%s); got:\n%s", want.text, want.why, block)
		}
	}
	if strings.Contains(bot, "reasoning_effort=") {
		t.Error("bot.py passes reasoning_effort= as a Settings kwarg; OpenAILLMSettings has no such field and raises TypeError")
	}
}

// TestReasonParamsStayFlatKwargsOnLiveKit is the other half of the same rule:
// only entries that declare an overflow field get one. LiveKit's openai reason
// row forwards params as plain constructor kwargs, so reasoning_effort must
// land flat on openai.LLM(...) and no extra={...} may appear.
func TestReasonParamsStayFlatKwargsOnLiveKit(t *testing.T) {
	agentpy := artifactFile(t, reasonParamsArtifact(t, ir.ProviderLiveKit), "agent.py")

	var call string
	for _, line := range strings.Split(agentpy, "\n") {
		if strings.Contains(line, "openai.LLM(") && strings.Contains(line, `model="gpt-4o-mini"`) {
			call = line
			break
		}
	}
	if call == "" {
		t.Fatal(`agent.py has no openai.LLM( call for model="gpt-4o-mini"; fixture drifted`)
	}
	for _, want := range []string{`reasoning_effort="none"`, "temperature=0.4"} {
		if !strings.Contains(call, want) {
			t.Errorf("openai.LLM call is missing flat kwarg %s; got:\n%s", want, strings.TrimSpace(call))
		}
	}
	if strings.Contains(agentpy, "extra={") {
		t.Error("agent.py wraps params in extra={...}; the LiveKit openai row declares no overflow field, so kwargs stay flat")
	}
}
