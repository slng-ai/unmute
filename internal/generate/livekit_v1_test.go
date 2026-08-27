package generate

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

var updateLiveKitV1 = flag.Bool("update-livekit", false, "rewrite the livekit v1 golden")

// TestLiveKitV1RemyGolden emits the Remy example (agent handoff + task groups +
// the SLNG plugin) to LiveKit and compares the full file set byte-for-byte
// (driver-livekit T8/T9/T10, V11/V12). Zero Python.
// TestLiveKitTaskRetryDoesNotRestartTheScript holds a repetition seen on a live
// call.
//
// When a task's response comes back empty the emitted code retries, and the retry
// instruction used to say only "follow the current task instructions and produce
// its next valid response". Pointed at the script rather than the conversation, the
// model restarted it: the caller had already said "tomorrow if possible" and was
// asked "what day would you like?" a second time.
//
// The caller's turns are in the copied context throughout, so this is purely a
// wording defect, and wording is exactly what no other gate here reads.
func TestLiveKitTaskRetryDoesNotRestartTheScript(t *testing.T) {
	agent := salonAgent(t)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	py := artifactFile(t, artifact, "agent.py")
	if !strings.Contains(py, "task response was empty; retrying") {
		t.Fatal("the empty-response retry must exist: without it a task turn goes silent")
	}
	// Each of the three recovery strings by its own distinctive phrase: the first
	// attempt, the second, and the post-tool case. Counting a common word like
	// "already" would pass on unrelated prose elsewhere in the file, which is a
	// gate that looks like coverage and is not.
	for _, want := range []string{
		"never ask again for something they",
		"rather than asking again",
		"using what the caller has already told you",
	} {
		if !strings.Contains(py, want) {
			t.Errorf("a retry instruction must forbid re-asking: missing %q", want)
		}
	}
}

func TestLiveKitV1RemyGolden(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	enableLangfuse(agent)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	var out strings.Builder
	for _, file := range artifact.Files {
		out.WriteString("=== " + file.Path + " ===\n")
		out.Write(file.Content)
		if !strings.HasSuffix(string(file.Content), "\n") {
			out.WriteByte('\n')
		}
	}

	path := filepath.Join("testdata", "golden", "livekit_v1_remy.txt")
	if *updateLiveKitV1 {
		if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Fatalf("livekit v1 golden differs; run: go test ./internal/generate -run TestLiveKitV1RemyGolden -update-livekit")
	}
}

// TestLiveKitV1EmitsSlngPlugin asserts the scaffold example (Remy, all-SLNG
// bindings) emits the SLNG plugin and LiveKit Inference: the first generation
// stays a real SLNG agent (driver-livekit V12). Since the C8 amendment SLNG is
// the default, not the only route; TestLiveKitV1MultiVendor covers the rest.
func TestLiveKitV1EmitsSlngPlugin(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"from livekit.plugins import openai, silero, slng",
		"slng.STT(",
		"slng.TTS(",
		"openai.LLM(", // native plugin, not Inference: console runs on OPENAI_API_KEY alone (B6/V19)
		"from livekit.agents.beta.workflows import TaskCompletedEvent, TaskGroup",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
}

func TestV22LiveKitSpeechTracingWiring(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	enableLangfuse(agent)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatal(err)
	}

	bot := artifactFile(t, artifact, "agent.py")
	tracing := artifactFile(t, artifact, "tracing.py")
	readme := artifactFile(t, artifact, "README.md")
	if !strings.Contains(readme, "`greeter-livekit`") {
		t.Error("README trace name must match the emitted Langfuse trace name")
	}
	for _, want := range []string{
		"from tracing import setup_langfuse",
		`"langfuse.session.id": ctx.room.name`,
		`"langfuse.trace.name": "greeter" + "-" + "livekit"`,
		"await session.start(agent=Greeter(initial=True), room=ctx.room)",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	for _, want := range []string{
		"def setup_langfuse(",
		"def trace_speech_metrics(",
		"set_tracer_provider(trace_provider, metadata=metadata)",
		"should_export_span=lambda span: True",
		"ctx.add_shutdown_callback(flush_trace)",
		`@session.on("conversation_item_added")`,
	} {
		if !strings.Contains(tracing, want) {
			t.Errorf("tracing.py missing %q", want)
		}
	}
	if strings.Contains(tracing, "if not any(values)") || !strings.Contains(tracing, "if not all(values)") {
		t.Error("configured tracing must reject missing credentials, including all three")
	}
	setupAt := strings.Index(bot, "    setup_langfuse(")
	startAt := strings.Index(bot, "await session.start(")
	if setupAt < 0 || startAt < 0 || setupAt > startAt {
		t.Error("Langfuse tracing must be configured before AgentSession.start")
	}

	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if !strings.Contains(pyproject, `name = "unmute-livekit"`) {
		t.Error("pyproject.toml distribution name shadows the livekit dependency")
	}
	for _, dep := range []string{`"langfuse>=3"`, `"opentelemetry-sdk>=1.33,<2"`} {
		if !strings.Contains(pyproject, dep) {
			t.Errorf("pyproject.toml missing %s", dep)
		}
	}
	env := artifactFile(t, artifact, ".env.example")
	for _, name := range []string{"LANGFUSE_SECRET_KEY=", "LANGFUSE_PUBLIC_KEY=", "LANGFUSE_BASE_URL="} {
		if !strings.Contains(env, name) {
			t.Errorf(".env.example missing %s", name)
		}
	}
}

func TestV31LiveKitTracingIsIsolated(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	enableLangfuse(agent)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatal(err)
	}

	bot := artifactFile(t, artifact, "agent.py")
	if !strings.Contains(bot, "from tracing import setup_langfuse") {
		t.Fatal("agent.py missing tracing import")
	}
	for _, forbidden := range []string{"def setup_langfuse", "def trace_speech_metrics", "Langfuse("} {
		if strings.Contains(bot, forbidden) {
			t.Errorf("agent.py contains tracing implementation %q", forbidden)
		}
	}
	_ = artifactFile(t, artifact, "tracing.py")
}

func TestV23LiveKitSpeechObservationsAreUtteranceScoped(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	enableLangfuse(agent)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatal(err)
	}

	tracing := artifactFile(t, artifact, "tracing.py")
	for _, want := range []string{
		"from livekit.agents.voice import ConversationItemAddedEvent, MetricsCollectedEvent",
		"def trace_speech_metrics(",
		`"langfuse.observation.input": input_value`,
		`"langfuse.observation.output": output_value`,
		`attributes["langfuse.trace.input"] = output_value`,
		`attributes["langfuse.trace.output"] = input_value`,
		"pending_stt_metrics: list[STTMetrics] = []",
		"pending_tts_metrics: list[TTSMetrics] = []",
		`@session.on("conversation_item_added")`,
	} {
		if !strings.Contains(tracing, want) {
			t.Errorf("tracing.py missing %q", want)
		}
	}
	if strings.Contains(tracing, "trace_speech_metric(trace_provider, ev.metrics)") {
		t.Error("speech generations must not be emitted for each metrics event")
	}
}

func TestLiveKitV1UnconfiguredGolden(t *testing.T) { // V24
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatal(err)
	}
	bot := artifactFile(t, artifact, "agent.py")
	path := filepath.Join("testdata", "golden", "livekit_v1_remy_unconfigured_agent.py")
	if *updateLiveKitV1 {
		if err := os.WriteFile(path, []byte(bot), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bot != string(want) {
		t.Fatal("unconfigured livekit agent.py golden differs; run: go test ./internal/generate -run TestLiveKitV1UnconfiguredGolden -update-livekit")
	}
	if artifactHasFile(artifact, "tracing.py") {
		t.Fatal("unconfigured artifact emitted tracing.py")
	}
	for path, forbidden := range map[string][]string{
		"agent.py":       {"Langfuse", "LANGFUSE_", "trace_speech_metrics", "set_tracer_provider"},
		"pyproject.toml": {"langfuse", "opentelemetry"},
		".env.example":   {"LANGFUSE_"},
		"README.md":      {"Trace with Langfuse"},
	} {
		content := artifactFile(t, artifact, path)
		for _, token := range forbidden {
			if strings.Contains(content, token) {
				t.Errorf("%s contains unconfigured tracing token %q", path, token)
			}
		}
	}
}

func TestV26LiveKitStaticCheckSurface(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "simple-prompt"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)

	withTools, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	withToolsAgent := artifactFile(t, withTools, "agent.py")
	for _, want := range []string{"    RunContext,", "    function_tool,"} {
		if !strings.Contains(withToolsAgent, want) {
			t.Errorf("agent.py with tools missing %q", want)
		}
	}

	entry := agent.Agents[agent.EntryAgent]
	entry.Tools = nil
	agent.Agents[agent.EntryAgent] = entry
	agent.Tracing = nil
	toolFree, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	toolFreeAgent := artifactFile(t, toolFree, "agent.py")
	for _, forbidden := range []string{"    RunContext,", "    function_tool,", "from collections.abc import Sequence"} {
		if strings.Contains(toolFreeAgent, forbidden) {
			t.Errorf("unconfigured tool-free agent.py contains unused import %q", forbidden)
		}
	}
	if !strings.Contains(toolFreeAgent, `api_key=os.environ["OPENAI_API_KEY"]`) {
		t.Error("agent.py missing non-optional provider key lookup")
	}
	if strings.Contains(toolFreeAgent, `api_key=os.environ.get("OPENAI_API_KEY")`) {
		t.Error("required provider key must not be typed as optional")
	}
	// dl§V26 requires the checkers to be declared. The ruff version is pinned on
	// purpose: unpinned, `uv` resolves whatever ruff shipped today, and 0.16
	// widened its default rule selection enough to fail an unchanged generator.
	for _, want := range []string{"[dependency-groups]", `"ruff==`, `"ty"`} {
		if !strings.Contains(artifactFile(t, toolFree, "pyproject.toml"), want) {
			t.Errorf("pyproject.toml missing %q", want)
		}
	}

	enableLangfuse(agent)
	configured, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	configuredAgent := artifactFile(t, configured, "agent.py")
	configuredTracing := artifactFile(t, configured, "tracing.py")
	for _, forbidden := range []string{"    RunContext,", "    function_tool,"} {
		if strings.Contains(configuredAgent, forbidden) {
			t.Errorf("configured tool-free agent.py contains unused import %q", forbidden)
		}
	}
	for _, want := range []string{
		"from collections.abc import Sequence",
		") -> TracerProvider:",
		"trace_provider: TracerProvider,",
		"speech_metrics: Sequence[STTMetrics | TTSMetrics]",
	} {
		if !strings.Contains(configuredTracing, want) {
			t.Errorf("configured tracing.py missing static-check-safe form %q", want)
		}
	}
	if strings.Contains(configuredTracing, "TracerProvider | None") {
		t.Error("configured tracing provider must not be typed as optional")
	}
}

// TestV26_LiveKitAgentWebhookImportsHTTPX guards B12: an agent that owns a
// webhook tool must emit `import httpx` even when no task owns one. safe_core is
// exactly this shape (agents own lookup_customer/get_invoice, no tasks), so its
// agent.py called httpx.AsyncClient with no import (ruff F821, a V26 violation)
// until the import need was computed over agent tools too, not just task tools.
func TestV26_LiveKitAgentWebhookImportsHTTPX(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	agentPy := artifactFile(t, artifact, "agent.py")
	if !strings.Contains(agentPy, "httpx.AsyncClient(") {
		t.Fatal("fixture no longer lowers an agent webhook tool to httpx; pick one that does")
	}
	if !strings.Contains(agentPy, "import httpx") {
		t.Error("agent.py calls httpx.AsyncClient but omits `import httpx` (ruff F821)")
	}
}

// authFixtures are the two schemes plus what each must emit at the call site and
// which env its token adds. Shared by the livekit and pipecat auth tests.
var authFixtures = []struct {
	Name     string
	Auth     *ir.ToolAuth
	CallSite string
	Helper   string
	Env      string
}{
	{
		Name:     "bearer",
		Auth:     &ir.ToolAuth{Type: ir.ToolAuthBearer, TokenEnv: "LOOKUP_CUSTOMER_TOKEN"},
		CallSite: `headers=_bearer("LOOKUP_CUSTOMER_TOKEN"),`,
		Helper:   `return {"Authorization": "Bearer " + os.environ[env]}`,
		Env:      "LOOKUP_CUSTOMER_TOKEN",
	},
	{
		Name:     "api_key",
		Auth:     &ir.ToolAuth{Type: ir.ToolAuthAPIKey, TokenEnv: "LOOKUP_CUSTOMER_KEY", Header: "X-API-Key"},
		CallSite: `headers=_api_key("X-API-Key", "LOOKUP_CUSTOMER_KEY"),`,
		Helper:   `return {header: os.environ[env]}`,
		Env:      "LOOKUP_CUSTOMER_KEY",
	},
}

// authAgent loads safe_core and puts one auth block on its webhook tool.
func authAgent(t *testing.T, auth *ir.ToolAuth) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	tool := agent.Tools["lookup_customer"]
	if tool.Execution != ir.ToolWebhook {
		t.Fatal("fixture no longer has a webhook lookup_customer; pick one that does")
	}
	tool.Auth = auth
	agent.Tools["lookup_customer"] = tool
	return agent
}

// TestLiveKitV1WebhookAuth covers both schemes (SCHEMA §5.3): the call site
// reads the right helper and the token env joins .env.example by name only.
func TestLiveKitV1WebhookAuth(t *testing.T) {
	for _, fixture := range authFixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			agent := authAgent(t, fixture.Auth)
			artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			agentPy := artifactFile(t, artifact, "agent.py")
			for _, want := range []string{fixture.CallSite, fixture.Helper} {
				if !strings.Contains(agentPy, want) {
					t.Errorf("agent.py missing %q:\n%s", want, agentPy)
				}
			}
			if env := artifactFile(t, artifact, ".env.example"); !strings.Contains(env, fixture.Env) {
				t.Errorf(".env.example missing %s", fixture.Env)
			}
			// Only the authenticated tool sends headers; get_invoice shares the file.
			if strings.Count(agentPy, "headers=") != 1 {
				t.Errorf("exactly one tool must send headers:\n%s", agentPy)
			}
		})
	}
}

// TestLiveKitV1NoAuthHelpersWithoutAuth keeps every helper and its imports
// conditional (V8/V26: no dead code) — safe_core declares no auth.
func TestLiveKitV1NoAuthHelpersWithoutAuth(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	agentPy := artifactFile(t, artifact, "agent.py")
	for _, unwanted := range []string{"_bearer", "_api_key"} {
		if strings.Contains(agentPy, unwanted) {
			t.Errorf("agent.py emits %q with no auth tool", unwanted)
		}
	}
}

// TestLiveKitV1MultiVendor proves the catalogue path end to end: the safe_core
// livekit target binds Deepgram listen and ElevenLabs speak (per-vendor
// plugins), one voice is rebound to Cartesia in-code, and the emitted project
// carries the right constructors, merged plugin import, extras dep, and env.
func TestLiveKitV1MultiVendor(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	tgt.Models.Speak["specialist"] = ir.Binding{
		Provider: "cartesia", Model: "sonic-3", Voice: "f786b574-daa5-4673-aa0c-cbe3e8534c02",
	}
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"from livekit.plugins import cartesia, deepgram, elevenlabs, openai, silero",
		`stt=deepgram.STT(api_key=os.environ["DEEPGRAM_API_KEY"], model="nova-3")`,
		`tts=elevenlabs.TTS(api_key=os.environ["ELEVEN_API_KEY"], voice_id="cgSgspJ2msm6clMCkdW9")`,
		`tts=cartesia.TTS(api_key=os.environ["CARTESIA_API_KEY"], voice="f786b574-daa5-4673-aa0c-cbe3e8534c02", model="sonic-3")`,
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if !strings.Contains(pyproject, `"livekit-agents[cartesia,deepgram,elevenlabs,openai]==1.6.10"`) {
		t.Errorf("pyproject.toml missing merged extras dep:\n%s", pyproject)
	}
	if strings.Contains(pyproject, "livekit-plugins-slng") {
		t.Error("pyproject.toml pulls the slng plugin without an slng binding")
	}
}

// TestT16_LiveKitEmitsListenFallbackAdapter proves the listen chain lowers to
// the native stt.FallbackAdapter (verified in livekit-agents source
// 2026-07-19), with both services resolved through the catalogue and the
// stt module imported.
func TestT16_LiveKitEmitsListenFallbackAdapter(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	pkg.Agent.Models.Listen["backup_stt"] = spec.ModelDef{Provider: "deepgram", Model: "nova-3"}
	primary := pkg.Agent.Models.Listen["transcriber"]
	primary.Fallback = []string{"backup_stt"}
	pkg.Agent.Models.Listen["transcriber"] = primary
	pkg.Agent.Listen = "transcriber"
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"    stt,",
		`stt=stt.FallbackAdapter(stt=[slng.STT(`,
		`deepgram.STT(api_key=os.environ["DEEPGRAM_API_KEY"], model="nova-3")])`,
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	env := artifactFile(t, artifact, ".env.example")
	if !strings.Contains(env, "DEEPGRAM_API_KEY=") {
		t.Errorf(".env.example missing the fallback vendor key:\n%s", env)
	}
}

// TestLiveKitV1UnknownVendorFailsWithMatrix asserts the no-slot diagnostic
// quotes the support matrix instead of guessing a substitute service.
func TestLiveKitV1UnknownVendorFailsWithMatrix(t *testing.T) {
	env := newEnvSet()
	_, err := livekitSTTService(&ir.Binding{Provider: "acme", Model: "m"}, env)
	if err == nil || !strings.Contains(err.Error(), "listen providers on livekit: assemblyai, cartesia, deepgram, elevenlabs, gradium, sarvam, slng, soniox, speechmatics") {
		t.Fatalf("want a matrix-quoting error, got %v", err)
	}
}

func artifactFile(t *testing.T, artifact Artifact, path string) string {
	t.Helper()
	for _, file := range artifact.Files {
		if file.Path == path {
			return string(file.Content)
		}
	}
	t.Fatalf("%s not emitted", path)
	return ""
}

// requiredEnvBlock returns the emitted REQUIRED_ENV list, so a test asserts on
// the startup check itself rather than on the name appearing anywhere in the
// file.
func requiredEnvBlock(t *testing.T, agentpy string) string {
	t.Helper()
	start := strings.Index(agentpy, "REQUIRED_ENV = [")
	if start < 0 {
		t.Fatal("agent.py has no REQUIRED_ENV")
	}
	end := strings.Index(agentpy[start:], "]")
	if end < 0 {
		t.Fatal("agent.py REQUIRED_ENV is unterminated")
	}
	return agentpy[start : start+end]
}

func artifactHasFile(artifact Artifact, path string) bool {
	for _, file := range artifact.Files {
		if file.Path == path {
			return true
		}
	}
	return false
}

// TestLiveKitV1EmptyTaskResponseContract keeps empty-response recovery scoped
// to generated tasks. Normal agents and packages without tasks retain the SDK
// default, and only task-bearing runbooks describe the retry.
func TestLiveKitV1EmptyTaskResponseContract(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate task package: %v", err)
	}

	const mixin = "_RetryEmptyTaskResponseMixin"
	taskAgent := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"class " + mixin + ":",
		"class FindSlot(" + mixin + ", AgentTask[dict]):",
		"class QualifyEvent(" + mixin + ", AgentTask[dict]):",
		"class ConfirmBooking(" + mixin + ", AgentTask[dict]):",
		"self._response_tool_call_ids.difference_update(",
	} {
		if !strings.Contains(taskAgent, want) {
			t.Errorf("task-bearing agent.py missing %q", want)
		}
	}
	for _, normalAgent := range []string{"Events", "Greeter", "Reservations"} {
		if strings.Contains(taskAgent, "class "+normalAgent+"("+mixin) {
			t.Errorf("normal agent %s must not inherit %s", normalAgent, mixin)
		}
	}

	const runbookHeading = "## Empty task responses"
	if !strings.Contains(artifactFile(t, artifact, "README.md"), runbookHeading) {
		t.Errorf("task-bearing README.md missing %q", runbookHeading)
	}

	minimalPkg, err := spec.Load(filepath.Join("..", "..", "examples", "simple-prompt"))
	if err != nil {
		t.Fatal(err)
	}
	minimalAgent, err := ir.Build(minimalPkg)
	if err != nil {
		t.Fatal(err)
	}
	minimalArtifact, err := Generate(minimalAgent, targetByProvider(t, minimalAgent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate task-free package: %v", err)
	}
	if minimal := artifactFile(t, minimalArtifact, "agent.py"); strings.Contains(minimal, mixin) {
		t.Errorf("task-free agent.py must not emit %s", mixin)
	}
	if minimalREADME := artifactFile(t, minimalArtifact, "README.md"); strings.Contains(minimalREADME, runbookHeading) {
		t.Errorf("task-free README.md must not emit %q", runbookHeading)
	}
}

// TestLiveKitV1DelegateThenTransferAndEnd covers the two non-return `then`
// lowerings (SCHEMA §4.7, N13): the delegate must not return to the owner, so it
// emits a handoff (transfer) or session shutdown (end) instead of the typed
// results, and its tool description must say control does not come back. Reuses
// the Remy package and rewrites its two groups' `then` in-memory.
func TestLiveKitV1DelegateThenTransferAndEnd(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	// do_reserve -> reserve_group (transfer to the greeter); do_event -> events_group (end).
	reserve := agent.TaskGroups["reserve_group"]
	reserve.Then, reserve.ThenTarget = ir.GroupTransfer, "greeter"
	agent.TaskGroups["reserve_group"] = reserve
	events := agent.TaskGroups["events_group"]
	events.Then, events.ThenTarget = ir.GroupEnd, ""
	agent.TaskGroups["events_group"] = events
	confirm := agent.Tasks["confirm_booking"]
	confirm.Tools = append(confirm.Tools, "back_to_greeter")
	agent.Tasks["confirm_booking"] = confirm

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var botpy string
	for _, file := range artifact.Files {
		if file.Path == "agent.py" {
			botpy = string(file.Content)
		}
	}
	if botpy == "" {
		t.Fatal("agent.py not emitted")
	}

	for _, want := range []string{
		// transfer: hands off to the target, does not return; no typed-result return.
		"async def do_reserve(self, ctx: RunContext):",
		"return Greeter(chat_ctx=owner_ctx)",
		"when it finishes the caller is handed to the greeter.",
		// end: shuts the session down, does not return.
		"self.session.shutdown()",
		"when it finishes the call ends.",
		"does not return to you",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	// Neither non-return path may hand back the typed results (N13/§4.7), and a
	// transfer/end delegate is not typed `-> dict`.
	for _, forbidden := range []string{"return result.task_results", "async def do_reserve(self, ctx: RunContext) -> dict:"} {
		if strings.Contains(botpy, forbidden) {
			t.Errorf("agent.py must not contain %q for a non-return delegate", forbidden)
		}
	}
	if got := strings.Count(botpy, "except _TaskTransfer as transfer:\n            return transfer.agent"); got != 2 {
		t.Errorf("transfer/end groups catch task handoffs %d times, want 2", got)
	}
}

// TestLiveKitV1SingleTaskDelegate covers the T12 lowering (V1/V3): a delegate
// with `task:` awaits the AgentTask directly, applies `assign` into the typed
// userdata, and returns the typed result to the owner — no TaskGroup involved.
func TestLiveKitV1SingleTaskDelegate(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Controls["do_find"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Task: "find_slot",
		When:   "The caller only wants to check for a slot, not book yet.",
		Assign: map[string]string{"caller_phone": "result.date"},
	}
	def := agent.Agents["reservations"]
	def.Tools = append(def.Tools, "do_find")
	agent.Agents["reservations"] = def

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"async def do_find(self, ctx: RunContext) -> dict:",
		"result = await FindSlot(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True))",
		`ctx.userdata.caller_phone = result["date"]`,
		"@dataclass\nclass Userdata:",
		"caller_phone: str | None = None",
		"session = AgentSession[Userdata](",
		"userdata=Userdata(),",
		// V1/B1: the single-task delegate docstring carries the finality guidance
		// so the owner LLM does not re-run the finished flow.
		"Do not run this flow again for the same request.",
		// N13: an awaited AgentTask merges its own turns into the owner on
		// return, so the owner's context is snapshotted before and restored
		// after. Without it the owner's next prompt ends on the task's last
		// assistant line with no tool record of the work, reads as unfinished,
		// and the model runs the whole flow a second time. The docstring above
		// asks for that; only these two lines enforce it.
		"owner_ctx = self.chat_ctx.copy()",
		"await self.update_chat_ctx(owner_ctx)",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	// The restore must land between the task and the assignment, or the owner
	// keeps the merged turns.
	restore := strings.Index(botpy, "await self.update_chat_ctx(owner_ctx)")
	assign := strings.Index(botpy, `ctx.userdata.caller_phone = result["date"]`)
	if restore < 0 || assign < 0 || restore > assign {
		t.Errorf("the owner context is restored at %d, after the assignment at %d", restore, assign)
	}
	if strings.Contains(botpy, "_terminal_claimed") {
		t.Error("a task without transfers must not emit terminal-claim state")
	}
}

// A task-scoped agent_transfer completes the task with a private exception so
// the delegate returns the target agent instead of treating it as task data.
func TestLiveKitV1SingleTaskAgentTransfer(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	transfer := agent.Controls["back_to_greeter"].(*ir.AgentTransfer)
	transfer.Announce = "I will take you back to Remy."
	transfer.Requires = []string{"caller_phone"}
	task := agent.Tasks["find_slot"]
	task.Tools = append(task.Tools, "back_to_greeter")
	agent.Tasks["find_slot"] = task
	agent.Controls["do_find"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Task: "find_slot", When: "Find one slot.",
	}
	reservations := agent.Agents["reservations"]
	reservations.Tools = append(reservations.Tools, "do_find")
	agent.Agents["reservations"] = reservations

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"class _TaskTransfer(Exception):",
		"class FindSlot(_RetryEmptyTaskResponseMixin, AgentTask[dict]):",
		"self._terminal_claimed = False",
		"def _claim_terminal(self) -> bool:",
		"async def back_to_greeter(self, ctx: RunContext):",
		"if not self._claim_terminal():\n            return",
		`_unmet = _unmet_prerequisites(ctx.userdata, ["caller_phone"])`,
		"if _unmet:\n            self._terminal_claimed = False",
		`await ctx.session.say("I will take you back to Remy.", allow_interruptions=False)`,
		"self.complete(_TaskTransfer(Greeter(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True))))",
		"except BaseException:\n            self._terminal_claimed = False\n            raise",
		"except _TaskTransfer as transfer:",
		"return transfer.agent",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	taskStart := strings.Index(botpy, "class FindSlot(_RetryEmptyTaskResponseMixin, AgentTask[dict]):")
	if taskStart < 0 {
		t.Fatal("FindSlot task not emitted")
	}
	taskEnd := strings.Index(botpy[taskStart:], "# --- session")
	if taskEnd < 0 {
		t.Fatal("could not bound FindSlot task")
	}
	taskBlock := botpy[taskStart : taskStart+taskEnd]
	if got := strings.Count(taskBlock, "if not self._claim_terminal():"); got != 2 {
		t.Errorf("transfer and finish claim the terminal %d times, want 2", got)
	}
	transferMethod := strings.Index(taskBlock, "async def back_to_greeter")
	claim := -1
	if transferMethod >= 0 {
		if relative := strings.Index(taskBlock[transferMethod:], "if not self._claim_terminal():"); relative >= 0 {
			claim = transferMethod + relative
		}
	}
	announce := strings.Index(taskBlock, "await ctx.session.say")
	if claim < 0 || announce < 0 || claim > announce {
		t.Errorf("transfer terminal claim at %d must precede announcement await at %d", claim, announce)
	}
	delegate := strings.Index(botpy, "async def do_find(")
	taskAwait := strings.Index(botpy[delegate:], "result = await FindSlot(")
	catch := strings.Index(botpy[delegate:], "except _TaskTransfer as transfer:")
	restore := strings.Index(botpy[delegate:], "await self.update_chat_ctx(owner_ctx)")
	if delegate < 0 || taskAwait < 0 || catch < taskAwait || (restore >= 0 && catch > restore) {
		t.Fatalf("task transfer must be caught around the task await; delegate=%d await=%d catch=%d restore=%d", delegate, taskAwait, catch, restore)
	}

	// The task path reuses the complete agent-transfer contract, including
	// summary history and variable selection.
	transfer.Context.History = ir.HistorySummary
	transfer.Context.Summarizer = "reasoning"
	agent.Variables["visit_count"] = ir.Variable{Type: ir.PrimitiveInteger}
	transfer.Context.Variables = ir.VariableSelection{Names: []string{"caller_phone"}}
	summaryArtifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate summary transfer: %v", err)
	}
	summaryBot := artifactFile(t, summaryArtifact, "agent.py")
	for _, want := range []string{
		"async def _summarize(source: llm.ChatContext",
		"ctx.userdata.visit_count = None  # context.variables: not carried on this transfer",
		"self.complete(_TaskTransfer(Greeter(chat_ctx=summary_ctx)))",
	} {
		if !strings.Contains(summaryBot, want) {
			t.Errorf("summary task transfer missing %q", want)
		}
	}
}

func TestLiveKitV1SharedGroupTaskTransferAndResults(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	task := agent.Tasks["find_slot"]
	task.Tools = append(task.Tools, "back_to_greeter")
	agent.Tasks["find_slot"] = task

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	start := strings.Index(botpy, "async def do_reserve(")
	if start < 0 {
		t.Fatal("do_reserve delegate not emitted")
	}
	end := strings.Index(botpy[start:], "# --- tasks")
	if end < 0 {
		t.Fatal("could not bound do_reserve delegate before task classes")
	}
	block := botpy[start : start+end]
	for _, want := range []string{
		"group = TaskGroup(",
		"summarize_chat_ctx=False,",
		"on_task_completed=lambda event: _share_task_result(group, event),",
		"try:\n            result = await group",
		"except _TaskTransfer as transfer:\n            return transfer.agent",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("do_reserve missing %q", want)
		}
	}
	for _, privileged := range []string{
		`role="developer"`, `role="system"`, "add_message(",
	} {
		if strings.Contains(block, privileged) {
			t.Errorf("shared group contains injected prompt context %q", privileged)
		}
	}
	if strings.Contains(block, "return_exceptions=True") {
		t.Error("TaskGroup must propagate the transfer sentinel and stop remaining tasks")
	}
}

func TestLiveKitV1IsolatedGroupTaskAgentTransfer(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	task := agent.Tasks["find_slot"]
	task.Tools = append(task.Tools, "back_to_greeter")
	agent.Tasks["find_slot"] = task
	group := agent.TaskGroups["reserve_group"]
	group.ContextScope = ir.ContextIsolated
	agent.TaskGroups["reserve_group"] = group

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	start := strings.Index(botpy, "async def do_reserve(")
	if start < 0 {
		t.Fatal("do_reserve delegate not emitted")
	}
	block := botpy[start:]
	for _, want := range []string{
		"try:\n            task_results[\"find_slot\"] = await FindSlot()\n            task_results[\"confirm_booking\"] = await ConfirmBooking()",
		"except _TaskTransfer as transfer:\n            return transfer.agent",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("do_reserve missing %q", want)
		}
	}
	catch := strings.Index(block, "except _TaskTransfer as transfer:")
	second := strings.Index(block, `task_results["confirm_booking"] = await ConfirmBooking()`)
	if catch < second {
		t.Fatalf("catch at %d must wrap the later step at %d", catch, second)
	}
}

func TestLiveKitV1TaskRejectsOtherControlKinds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ref     string
		control ir.Control
	}{
		{name: "delegate", ref: "do_reserve"},
		{
			name: "human transfer", ref: "to_manager",
			control: &ir.HumanTransfer{
				Kind: ir.ControlHumanTransfer, Destination: "manager", Mode: ir.TransferCold,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := ir.Build(pkg)
			if err != nil {
				t.Fatal(err)
			}
			if tc.control != nil {
				agent.Controls[tc.ref] = tc.control
			}
			task := agent.Tasks["find_slot"]
			task.Tools = append(task.Tools, tc.ref)
			agent.Tasks["find_slot"] = task

			_, err = buildLiveKitData(agent, targetByProvider(t, agent, ir.ProviderLiveKit))
			want := `task "find_slot" references control "` + tc.ref + `": tasks support agent_transfer controls only`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("want %q, got %v", want, err)
			}
		})
	}
}

// TestV1LiveKitCompletedFlowEndsOnce guards F0/B1: a completed task/delegate
// returns control once and the owner never re-runs a finished flow. Deterministic
// proxies: finish() resolves via self.complete() only (`-> None`, no trailing
// return that would emit a stray output after the task closes), and every
// then:return delegate docstring tells the LLM the result is final and the flow
// must not re-run.
func TestV1LiveKitCompletedFlowEndsOnce(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")

	// finish() is the sole resolution: -> None, no value returned after complete().
	// The reserved unserved-request arg trails the task's own result args, so this
	// reads the whole signature line rather than a fixed string.
	finishLine := ""
	for _, line := range strings.Split(botpy, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "async def finish(self, ctx: RunContext, sent: bool, unserved_request: ") {
			finishLine = line
			break
		}
	}
	if finishLine == "" || !strings.HasSuffix(finishLine, ") -> None:") {
		t.Error("finish must be typed -> None (complete() is the sole resolution)")
	}
	if strings.Contains(botpy, `return "Done."`) {
		t.Error(`finish must not return a value after self.complete() (stray post-completion output)`)
	}
	// LiveKit 1.6.10 resolves AgentTask before recording finish's tool output.
	// Shared TaskGroups repair that exact output through the SDK callback, without
	// returning from finish() and triggering another child-model turn.
	for _, want := range []string{
		"async def _share_task_result(group: TaskGroup, event: TaskCompletedEvent) -> None:",
		`finish_call_id = getattr(event.agent_task, "_finish_call_id", None)`,
		"and not item.is_error",
		"and item.call_id == finish_call_id",
		"shared_ctx.remove(shared_output)",
		"exclude_invalid_function_calls=False",
		"on_task_completed=lambda event: _share_task_result(group, event)",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("shared task result repair missing %q", want)
		}
	}
	// Every then:return delegate (do_reserve, do_event here) states the result is
	// final and the flow must not re-run.
	for _, delegate := range []string{"async def do_reserve", "async def do_event"} {
		idx := strings.Index(botpy, delegate)
		if idx < 0 {
			t.Fatalf("delegate %q not emitted", delegate)
		}
		doc := botpy[idx : idx+400]
		if !strings.Contains(doc, "Do not run this flow again for the same request.") {
			t.Errorf("then:return delegate %q missing finality guidance in its docstring", delegate)
		}
	}
}

// TestV2LiveKitToolCarriesSchema guards F1/V2: both @function_tool paths present
// the LLM the schema the tool YAML declares. An enum lowers to Literal[...] and a
// per-property description to Annotated[..., Field(description=...)], on the
// task-level tool (find_slot's check_availability) and, with the same tool added
// to an agent, on the agent-level path too.
func TestV2LiveKitToolCarriesSchema(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	// Inject an enum property into check_availability (Input is a shared map).
	props := agent.Tools["check_availability"].Input["properties"].(map[string]any)
	props["service"] = map[string]any{
		"type":        "string",
		"enum":        []any{"haircut", "hair-color", "blowout"},
		"description": "The service requested",
	}
	// Also expose the tool agent-level on the greeter so both paths emit it.
	g := agent.Agents["greeter"]
	g.Tools = append(g.Tools, "check_availability")
	agent.Agents["greeter"] = g

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")

	for _, want := range []string{
		"from typing import Annotated, Literal",
		"from pydantic import Field",
		// enum → Literal, description → Annotated[..., Field(...)]
		`service: Annotated[Literal["haircut", "hair-color", "blowout"], Field(description="The service requested")]`,
		// non-enum described args still carry the description
		`party_size: Annotated[int, Field(description="Number of people")]`,
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	// The same schema must reach the LLM on BOTH the agent-level and task-level
	// emissions of the tool (two def sites, both carrying the Literal).
	if got := strings.Count(botpy, `Literal["haircut", "hair-color", "blowout"]`); got != 2 {
		t.Errorf("expected the enum Literal on both tool paths (2 sites), got %d", got)
	}
}

// TestF3LiveKitSingleAgentMinimalShape guards F3: a lone agent that is never a
// handoff target emits the canonical Agent(instructions=...) shape with no
// chat_ctx ctor plumbing and no NOT_GIVEN/NotGivenOr/llm imports that only feed
// it. Multi-agent output is unchanged.
func TestF3LiveKitSingleAgentMinimalShape(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "simple-prompt"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	if !strings.Contains(botpy, "    def __init__(self) -> None:") {
		t.Error("minimal single agent must have a plain __init__(self) with no chat_ctx param")
	}
	for _, forbidden := range []string{
		"chat_ctx: NotGivenOr[llm.ChatContext]",
		"chat_ctx=chat_ctx",
		"    NOT_GIVEN,",
		"    NotGivenOr,",
		"    llm,",
	} {
		if strings.Contains(botpy, forbidden) {
			t.Errorf("minimal single agent must not emit %q", forbidden)
		}
	}

	// Guard against over-application: multi-agent Remy keeps the plumbing.
	rpkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	ragent, err := ir.Build(rpkg)
	if err != nil {
		t.Fatal(err)
	}
	rart, err := Generate(ragent, targetByProvider(t, ragent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate remy: %v", err)
	}
	remypy := artifactFile(t, rart, "agent.py")
	if !strings.Contains(remypy, "def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN) -> None:") {
		t.Error("multi-agent Remy must keep the chat_ctx handoff plumbing")
	}
}

// TestLiveKitV1IsolatedGroup covers the T13 lowering (V2/C3): an isolated
// task_group compiles to a sequence of standalone AgentTasks, each starting
// with a fresh context — never a TaskGroup, which always shares context.
func TestLiveKitV1IsolatedGroup(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	reserve := agent.TaskGroups["reserve_group"]
	reserve.ContextScope = ir.ContextIsolated
	agent.TaskGroups["reserve_group"] = reserve

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		// the isolated flow: fresh AgentTasks, results dict, typed return
		"async def do_reserve(self, ctx: RunContext) -> dict:",
		`task_results["find_slot"] = await FindSlot()`,
		`task_results["confirm_booking"] = await ConfirmBooking()`,
		"return task_results",
		// events_group stays shared, so TaskGroup is still imported and used
		"from livekit.agents.beta.workflows import TaskCompletedEvent, TaskGroup",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	if strings.Contains(botpy, `group.add(\n            lambda: FindSlot`) {
		t.Error("isolated group must not lower to TaskGroup.add")
	}

	// With every group isolated, the TaskGroup import must disappear.
	events := agent.TaskGroups["events_group"]
	events.ContextScope = ir.ContextIsolated
	agent.TaskGroups["events_group"] = events
	artifact, err = Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate all-isolated: %v", err)
	}
	botpy = artifactFile(t, artifact, "agent.py")
	if strings.Contains(botpy, "TaskGroup") {
		t.Error("all-isolated project must not import or use TaskGroup")
	}
	if strings.Contains(botpy, "_finish_call_id") {
		t.Error("all-isolated project must not emit shared TaskGroup result state")
	}
}

// TestLiveKitV1PerTaskModel covers the T14 lowering (B1/V1/V15): a task with
// its own model profile gets llm= on the AgentTask, resolved through the
// catalogue; a task on the entry agent's profile stays on the session LLM.
func TestLiveKitV1PerTaskModel(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Models["fast"] = ir.ModelDef{Kind: ir.KindThink, Placement: ir.PlacementAPI}
	task := agent.Tasks["find_slot"]
	task.Model = "fast"
	agent.Tasks["find_slot"] = task
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	tgt.Models.Reason["fast"] = ir.Binding{Provider: "openai", Model: "gpt-4o-mini"}

	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	want := `super().__init__(instructions=FIND_SLOT_PROMPT, chat_ctx=chat_ctx, llm=openai.LLM(api_key=os.environ["OPENAI_API_KEY"], model="gpt-4o-mini"))`
	if !strings.Contains(botpy, want) {
		t.Errorf("agent.py missing per-task llm override %q", want)
	}
	// The other tasks keep the session LLM: no stray llm= kwarg.
	if !strings.Contains(botpy, "super().__init__(instructions=CONFIRM_BOOKING_PROMPT, chat_ctx=chat_ctx)") {
		t.Error("confirm_booking must stay on the session LLM")
	}
}

// TestLiveKitV1HistoryShapingAndFallback covers the T5 lowerings (V4/V5):
// every history value compiles, include_tool_calls and variables subsets
// shape the handoff, and a fallback chain lowers to llm.FallbackAdapter.
func TestLiveKitV1HistoryShapingAndFallback(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	// Fallback chain on the session profile.
	profile := agent.Models["reasoning"]
	profile.Fallback = []string{"backup"}
	agent.Models["reasoning"] = profile
	agent.Models["backup"] = ir.ModelDef{Kind: ir.KindThink, Placement: ir.PlacementAPI}
	// Shape each transfer differently.
	agent.Variables["visit_count"] = ir.Variable{Type: ir.PrimitiveInteger}
	toRes := agent.Controls["to_reservations"].(*ir.AgentTransfer)
	toRes.Context.History = ir.HistoryMessages
	toRes.Context.Variables = ir.VariableSelection{Names: []string{"caller_phone"}} // visit_count not carried
	toEvents := agent.Controls["to_events"].(*ir.AgentTransfer)
	toEvents.Context.History = ir.HistoryLastN
	toEvents.Context.MaxMessages = 6
	back := agent.Controls["back_to_greeter"].(*ir.AgentTransfer)
	back.Context.History = ir.HistorySummary
	back.Context.Summarizer = "backup"

	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	tgt.Models.Reason["backup"] = ir.Binding{Provider: "openai", Model: "gpt-4o"}

	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		// V4: native adapter around the chain, everywhere the profile binds.
		`llm=llm.FallbackAdapter(llm=[openai.LLM(api_key=os.environ["OPENAI_API_KEY"], model="gpt-4o-mini", temperature=0.4), openai.LLM(api_key=os.environ["OPENAI_API_KEY"], model="gpt-4o")])`,
		// V5: messages / last_n / summary shaping.
		`return Reservations(chat_ctx=llm.ChatContext(items=[m for m in self.chat_ctx.messages() if m.role in ("user", "assistant")]))`,
		`return Events(chat_ctx=_last_n(self.chat_ctx, 6))`,
		"def _last_n(source: llm.ChatContext",
		`summary_ctx = await _summarize(self.chat_ctx, openai.LLM(api_key=os.environ["OPENAI_API_KEY"], model="gpt-4o"))`,
		"return Greeter(chat_ctx=summary_ctx)",
		"async def _summarize(source: llm.ChatContext",
		// D7: an uncarried variable resets on the transfer.
		"ctx.userdata.visit_count = None  # context.variables: not carried on this transfer",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
}

// TestLiveKitV1HistoryResetAndToolCallShaping covers reset transfers and
// include_tool_calls: false (V5): reset hands the target a fresh context;
// exclude_function_call strips tool traffic from a full handoff.
func TestLiveKitV1HistoryResetAndToolCallShaping(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	noCalls := false
	toRes := agent.Controls["to_reservations"].(*ir.AgentTransfer)
	toRes.Context.History = ir.HistoryFull
	toRes.Context.IncludeToolCalls = &noCalls
	back := agent.Controls["back_to_greeter"].(*ir.AgentTransfer)
	back.Context.History = ir.HistoryReset

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		`return Reservations(chat_ctx=self.chat_ctx.copy(exclude_instructions=True, exclude_function_call=True, exclude_handoff=True))`,
		"# history: reset — the target starts fresh (a handoff marker still lands).",
		"return Greeter()",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
}

func TestLiveKitV1TransferAnnounceAndEntryGreeting(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	toRes := agent.Controls["to_reservations"].(*ir.AgentTransfer)
	toRes.Announce = "I’ll connect you to reservations now."
	toRes.Requires = []string{"caller_phone"}
	agent.Variables["visit_count"] = ir.Variable{Type: ir.PrimitiveInteger}
	toRes.Context.Variables = ir.VariableSelection{Names: []string{"caller_phone"}}
	back := agent.Controls["back_to_greeter"].(*ir.AgentTransfer)
	back.Context.History = ir.HistoryReset

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	start := strings.Index(botpy, "    async def to_reservations(self, ctx: RunContext):")
	if start < 0 {
		t.Fatal("agent.py missing to_reservations")
	}
	end := strings.Index(botpy[start+1:], "\n    @function_tool")
	if end < 0 {
		t.Fatal("agent.py missing the end of to_reservations")
	}
	method := botpy[start : start+1+end]
	guardAt := strings.Index(method, "        if _unmet:")
	announceAt := strings.Index(method, `        await ctx.session.say("I’ll connect you to reservations now.", allow_interruptions=False)`)
	resetAt := strings.Index(method, "        ctx.userdata.visit_count = None")
	returnAt := strings.Index(method, "        return Reservations(")
	if guardAt < 0 || announceAt < 0 || resetAt < 0 || returnAt < 0 ||
		guardAt >= announceAt || announceAt >= resetAt || resetAt >= returnAt {
		t.Errorf("transfer must guard, finish its announcement, shape context, then hand off:\n%s", method)
	}
	if strings.Contains(method, "generate_reply(instructions=") {
		t.Errorf("the exact announcement must not start another LLM turn:\n%s", method)
	}

	for _, want := range []string{
		"def __init__(self, chat_ctx: NotGivenOr[llm.ChatContext] = NOT_GIVEN, initial: bool = False) -> None:",
		"        self._initial = initial",
		// Re-entry is a handoff arrival too, so its opening turn withholds the
		// agent's own handoffs (B3) and keeps everything else.
		"        if not self._initial:\n",
		"            self.session.generate_reply(tools=[t.id for t in self.tools if t.id not in {\"to_reservations\", \"to_events\"}])\n            return",
		"await session.start(agent=Greeter(initial=True), room=ctx.room)",
		"return Greeter()",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	if strings.Contains(botpy, "await session.start(agent=Greeter(), room=ctx.room)") {
		t.Error("the startup agent must be marked initial; transfer-created agents keep the false default")
	}
	backStart := strings.Index(botpy, "    async def back_to_greeter(self, ctx: RunContext):")
	if backStart < 0 {
		t.Fatal("agent.py missing back_to_greeter")
	}
	backEnd := strings.Index(botpy[backStart+1:], "\n    @function_tool")
	if backEnd < 0 {
		t.Fatal("agent.py missing the end of back_to_greeter")
	}
	if block := botpy[backStart : backStart+1+backEnd]; strings.Contains(block, "session.say(") {
		t.Errorf("an omitted announce must stay silent:\n%s", block)
	}
}

// TestV3LiveKitAgentTransfersHiddenOnlyOnEnter covers the B3/V3 loop guard and
// the bug the first version of it caused: a receiving agent's automatic opening
// turn must not be able to hand the call back, and every turn after it must have
// the handoffs again.
//
// The guard used to be the framework's IGNORE_ON_ENTER tool flag. That filter is
// scoped to a context variable, and the opening reply's own tool calls inherit
// it, so an agent whose opening reply ran a delegate never saw its handoffs
// again: a live call offered the booking specialist only manage_booking for ten
// straight turns while the caller asked for customer care (B: salon handoffs,
// 2026-08-20). The opening reply now names the tools it may use instead, which
// scopes the guard to the one turn it is meant for.
func TestV3LiveKitAgentTransfersHiddenOnlyOnEnter(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	greeter := agent.Agents["greeter"]
	greeter.Tools = append(greeter.Tools, "check_availability")
	agent.Agents["greeter"] = greeter

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	if strings.Contains(botpy, "IGNORE_ON_ENTER") {
		t.Error("the on-enter tool flag hides a handoff for the rest of the call; the opening reply must name its tools instead")
	}
	for _, method := range []string{"to_reservations", "to_events", "back_to_greeter"} {
		want := "    @function_tool\n    async def " + method + "("
		if !strings.Contains(botpy, want) {
			t.Errorf("agent transfer %q must be an ordinary function tool", method)
		}
	}
	if !strings.Contains(botpy, "    @function_tool\n    async def check_availability(") {
		t.Error("ordinary tools must remain available during on_enter")
	}
	// Every agent that can be handed the call opens with an allowlist, and that
	// allowlist excludes exactly its own handoffs.
	openings := strings.Count(botpy, "self.session.generate_reply(tools=[t.id for t in self.tools if t.id not in {")
	if openings == 0 {
		t.Fatal("no agent opening reply names the tools it may use")
	}
	for _, want := range []string{
		`if t.id not in {"to_reservations", "to_events"}`,
		`if t.id not in {"back_to_greeter"}`,
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("an opening reply does not withhold its handoffs: want %s", want)
		}
	}
}

// TestLiveKitV1BuiltinEndCallTool covers the prebuilt end_call lowering:
// the beta EndCallTool import, its construction with the mapped params in the
// agent's super().__init__(tools=...), and that it is NOT a @function_tool
// method.
func TestLiveKitV1BuiltinEndCallTool(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Tools["end_call"] = ir.Tool{
		Execution:    ir.ToolBuiltin,
		Builtin:      "end_call",
		Description:  "End the call when the caller is finished.",
		Instructions: "Thank the caller and say goodbye.",
		Effect:       ir.ToolEndsConversation,
		Interruption: ir.ToolProviderDefault,
	}
	def := agent.Agents["greeter"]
	def.Tools = append(def.Tools, "end_call")
	agent.Agents["greeter"] = def

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"from livekit.agents.beta.tools import EndCallTool",
		`tools=[*EndCallTool(extra_description="End the call when the caller is finished.", end_instructions="Thank the caller and say goodbye.").tools],`,
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	if strings.Contains(botpy, "async def end_call(") {
		t.Error("a builtin tool must not render as a @function_tool method")
	}
}

// TestLiveKitV1ConversationShapingAndAgentTools covers the T15 lowerings
// (V16): agent-level webhook tools, interruption enabled/min_words via
// TurnHandlingOptions, the generated ignore-phrase stt_node filter, thinking
// audio via BackgroundAudioPlayer, and effect: ends_conversation.
func TestLiveKitV1ConversationShapingAndAgentTools(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	def := agent.Agents["greeter"]
	def.Tools = append(def.Tools, "check_availability")
	agent.Agents["greeter"] = def
	enabled := true
	agent.Conversation.Interruption = &ir.Interruption{
		Enabled: &enabled, MinimumWords: 2, IgnorePhrases: []string{"uh-huh", "OK"},
	}
	agent.Conversation.ThinkingAudio = ir.ThinkingSubtle
	tool := agent.Tools["send_confirmation"]
	tool.Effect = ir.ToolEndsConversation
	agent.Tools["send_confirmation"] = tool

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		// Agent-level webhook tool on the greeter class, carrying the declared
		// per-property schema (V2): descriptions via Annotated[..., Field(...)].
		"class Greeter(IgnorePhrasesMixin, Agent):",
		`async def check_availability(self, ctx: RunContext, date: Annotated[str, Field(description="The requested date, e.g. 2026-08-14")], party_size: Annotated[int, Field(description="Number of people")]) -> dict:`,
		// Interruption options ride turn_handling.
		`interruption={"enabled": True, "min_words": 2},`,
		// Generated ignore-phrase filter (lowercased phrases).
		`IGNORE_PHRASES = ["uh-huh", "ok"]`,
		"stt.SpeechEventType.FINAL_TRANSCRIPT",
		"class FindSlot(_RetryEmptyTaskResponseMixin, IgnorePhrasesMixin, AgentTask[dict]):",
		// Thinking audio.
		"background_audio = BackgroundAudioPlayer(",
		"thinking_sound=BuiltinAudioClip.KEYBOARD_TYPING,  # thinking_audio: subtle",
		"await background_audio.start(room=ctx.room, agent_session=session)",
		// effect: ends_conversation on a webhook tool.
		"self.session.shutdown()  # effect: ends_conversation",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
}

// TestLiveKitV1HumanTransferColdAndWarm covers the T6 control lowerings (V6):
// cold is a SIP REFER through the job context with the resolved destination;
// warm awaits the prebuilt WarmTransferTask and registers the trunk env.
func TestLiveKitV1HumanTransferColdAndWarm(t *testing.T) {
	agent, resolved := configuredLiveKitSIPCold(t)
	artifact, err := Generate(agent, resolved, target.Default())
	if err != nil {
		t.Fatalf("generate cold: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		// LiveKit may execute parallel tool calls. Reject a duplicate while the
		// first transfer is in flight so parallel requests cannot trigger two REFERs.
		`@function_tool(on_duplicate="reject")
    async def to_human(self, ctx: RunContext) -> str | None:`,
		// `str | None` since 2026-08-12: a function tool's return value is fed
		// back to the LLM, which then takes another turn. That is right while
		// the caller is still listening and wrong once the session is over, so
		// the session-ending paths return nothing.
		"async def to_human(self, ctx: RunContext) -> str | None:",
		"job_ctx = get_job_context()",
		"rtc.ParticipantKind.PARTICIPANT_KIND_SIP",
		// The tool speaks its own announcement (SPEC V4/B4).
		`"Putting you through now, one moment.", allow_interruptions=False`,
		// A REFER destination is a URI, not a bare number: this asserted
		// `transfer_to="+14155550123"` until 2026-08-12, which is a shape no
		// LiveKit example uses and which Plivo's Refer-To rules forbid outright.
		// The number itself now arrives from the environment, because a
		// destination in agent.yaml names a variable rather than a number
		// (spec FR-004d); _refer_uri adds the scheme at call time.
		`transfer_to=_refer_uri(os.environ["BILLING_PHONE_NUMBER"]),`,
		"await job_ctx.api.sip.transfer_sip_participant(request)",
		// The pinned SDK can also surface transport exceptions such as
		// aiohttp.ClientError and asyncio.TimeoutError. The try covers only the
		// provider await, and Exception deliberately leaves cancellation alone.
		"except Exception as error:",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("cold agent.py missing %q", want)
		}
	}
	if strings.Contains(botpy, "except (api.SipCallError, api.ServerError) as error:") {
		t.Error("cold agent.py lets generic transport failures bypass on_unavailable")
	}
	// A completed REFER takes the caller out of the room, so the session ends on
	// its own and the tool returns nothing. A return value here would buy one
	// LLM turn spoken to nobody, racing the teardown.
	if strings.Contains(botpy, `return "The caller was transferred."`) {
		t.Error("cold agent.py returns to the LLM after the caller has already left the room")
	}
	if !strings.Contains(botpy, "return None") {
		t.Error("cold agent.py does not end its transfer tool without a value")
	}

	// The warm half needs a telephony Connection: a warm transfer dials the
	// destination itself, and since 2026-08-12 (SCHEMA N33) it does that with
	// the carrier's own SIP credentials, so a target with no Connection is a
	// gated validation error rather than a package that reads a trunk ID it
	// never declared. This used to mutate the non-telephony target in place,
	// which exercised a shape nobody can author.
	warmAgent, warmTarget := configuredLiveKitSIP(t)
	human := warmAgent.Controls["to_human"].(*ir.HumanTransfer)
	human.RingTimeout = "30s"
	human.OnUnavailable = ir.OnUnavailableReturn
	artifact, err = Generate(warmAgent, warmTarget, target.Default())
	if err != nil {
		t.Fatalf("generate warm: %v", err)
	}
	botpy = artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		`@function_tool(on_duplicate="reject")
    async def to_human(self, ctx: RunContext) -> str | None:`,
		"from livekit.agents.beta.workflows import WarmTransferTask, WorkflowInstructions",
		`sip_call_to=_sip_user(os.environ["BILLING_PHONE_NUMBER"])`,
		// The conversation is read into one local and that local is what is
		// handed over, so the count logged beside it cannot describe something
		// else (003 contract C4).
		"chat_ctx=briefing_ctx,",
		// instructions=WorkflowInstructions(...) is the supported briefing
		// surface. extra_instructions is deprecated and the prebuilt warns and
		// ignores it whenever instructions is given. WorkflowInstructions is the
		// 1.6.9 name for what 1.6.4 called InstructionParts: the rename carries
		// no alias, and it was verified by reading the installed package on
		// 2026-08-12 rather than the older reference checkout.
		"instructions=WorkflowInstructions(",
		"persona=_BRIEFING_PERSONA,",
		`extra="Say who is calling and why.",`,
		"ringing_timeout=30,",
		// The tool speaks before the hold starts (SPEC V4/B4).
		`"One moment while I bring a colleague on the line.", allow_interruptions=False`,
		"except ToolError as error:",
		"room_options=room_io.RoomOptions(delete_room_on_close=False)",
		"result.human_agent_identity",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("warm agent.py missing %q", want)
		}
	}
	if strings.Contains(botpy, "extra_instructions") {
		t.Error("agent.py still passes the deprecated extra_instructions, which the prebuilt ignores once instructions is given")
	}
	// A warm package installs exactly the one supported version its target
	// declares; it is never quietly widened or narrowed on the author's behalf.
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if !strings.Contains(pyproject, "==1.6.10") {
		t.Errorf("warm pyproject.toml must pin the declared livekit-agents version exactly:\n%s", pyproject)
	}
	if strings.Contains(pyproject, ">=1.6,<1.7") {
		t.Errorf("warm pyproject.toml still derives a version window instead of pinning:\n%s", pyproject)
	}
	// A warm transfer needs no platform-assigned trunk identity: it dials with
	// the carrier's own trunk settings, passed inline (SCHEMA N33, 2026-08-12).
	// What it does need is the four Connection values, and the inline form makes
	// the from-number mandatory because there is no trunk number to default to.
	envExample := artifactFile(t, artifact, ".env.example")
	if strings.Contains(envExample, "LIVEKIT_SIP_OUTBOUND_TRUNK") {
		t.Error(".env.example still lists LIVEKIT_SIP_OUTBOUND_TRUNK for a warm transfer")
	}
	for _, want := range []string{
		"sip_connection=_sip_trunk(),",
		"sip_number=_sip_number(),",
		"def _sip_trunk() -> api.SIPOutboundConfig:",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("warm agent.py missing %q", want)
		}
	}
	if strings.Contains(botpy, "sip_trunk_id") {
		t.Error("warm agent.py still passes a stored trunk id")
	}
	// After the merge the session is over, so the tool must hand the LLM
	// nothing. Returning a value here bought one more LLM turn, spoken into a
	// room that by then held the caller and the person we had just handed them
	// to, immediately after saying goodbye. Found on a live call 2026-08-12;
	// upstream's own warm-transfer example returns nothing here too.
	for _, want := range []string{
		`"warm transfer merged after %ds: %s",`,
		"result.human_agent_identity,",
		"ctx.session.shutdown()",
		"return None",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("warm agent.py missing %q", want)
		}
	}
	if strings.Contains(botpy, "The caller is now connected to ") {
		t.Error("warm agent.py returns the merge result to the LLM, which then speaks to the caller and the person again")
	}
	// FR-017: exactly the values needed to dial. Region pinning and transport
	// are optional on the platform and nobody declared them.
	for _, forbidden := range []string{"destination_country", "transport="} {
		if strings.Contains(botpy, forbidden) {
			t.Errorf("warm agent.py emits %q, which no Connection declared", forbidden)
		}
	}
}

func TestLiveKitV1HumanTransferHangupAlwaysShutsDown(t *testing.T) {
	coldAgent, coldTarget := configuredLiveKitSIPCold(t)
	coldAgent.Controls["to_human"].(*ir.HumanTransfer).OnUnavailable = ir.OnUnavailableHangup
	coldArtifact, err := Generate(
		coldAgent,
		coldTarget,
		target.Default(),
	)
	if err != nil {
		t.Fatal(err)
	}

	warmAgent, warmTarget := configuredLiveKitSIP(t)
	warmAgent.Controls["to_human"].(*ir.HumanTransfer).OnUnavailable = ir.OnUnavailableHangup
	warmArtifact, err := GenerateLiveKit(warmAgent, warmTarget, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	for name, test := range map[string]struct {
		py          string
		instruction string
	}{
		"cold": {
			py:          artifactFile(t, coldArtifact, "agent.py"),
			instruction: "Tell the caller the transfer failed and say goodbye.",
		},
		"warm": {
			py:          artifactFile(t, warmArtifact, "agent.py"),
			instruction: "Tell the caller nobody is available and say goodbye.",
		},
	} {
		t.Run(name, func(t *testing.T) {
			want := fmt.Sprintf(`try:
                await ctx.session.generate_reply(
                    instructions=%q
                )
            finally:
                ctx.session.shutdown()`, test.instruction)
			if !strings.Contains(test.py, want) {
				t.Errorf("%s hangup can skip shutdown when goodbye generation fails; missing:\n%s", name, want)
			}
		})
	}
}

// TestLiveKitV1RequiresGuard covers V7: a transfer with requires: emits a
// machine-checked guard that refuses unset and empty values.
func TestLiveKitV1RequiresGuard(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	toRes := agent.Controls["to_reservations"].(*ir.AgentTransfer)
	toRes.Requires = []string{"caller_phone"}

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		`_unmet = _unmet_prerequisites(ctx.userdata, ["caller_phone"])`,
		`return {"refused": _prerequisite_refusal(_unmet, False)}`,
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
}

// TestLiveKitV1OutboundVoicemail covers V8/N6: an outbound telephony channel
// emits the AMD dial-out flow; on_voicemail picks the machine-vm branch.
func TestLiveKitV1OutboundVoicemail(t *testing.T) {
	for _, tc := range []struct {
		action ir.VoicemailAction
		want   string
	}{
		{ir.VoicemailLeaveMessage, `ctx.shutdown("voicemail: left a message")  # on_voicemail: leave_message`},
		{ir.VoicemailHangup, `ctx.shutdown("voicemail detected")  # on_voicemail: hangup`},
	} {
		agent, resolved := configuredLiveKitSIP(t)
		phone := agent.Channels["phone"]
		phone.OnVoicemail = tc.action
		agent.Channels["phone"] = phone

		artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
		if err != nil {
			t.Fatalf("generate %s: %v", tc.action, err)
		}
		botpy := artifactFile(t, artifact, "agent.py")
		for _, want := range []string{
			"async with AMD(session, participant_identity=\"phone_user\") as detector:",
			"api.CreateSIPParticipantRequest(",
			// Inline trunk configuration, not a stored trunk id (SCHEMA N33,
			// 2026-08-12). CreateSIPParticipant takes the settings in `trunk`
			// and requires `sip_number` with them, because inline configuration
			// carries no number list to default from.
			"trunk=_sip_trunk(),",
			"sip_number=_sip_number(),",
			"wait_until_answered=True,",
			"result = await detector.execute()",
			tc.want,
		} {
			if !strings.Contains(botpy, want) {
				t.Errorf("%s agent.py missing %q", tc.action, want)
			}
		}
		if strings.Contains(botpy, "sip_trunk_id") {
			t.Errorf("%s agent.py still passes a stored trunk id", tc.action)
		}
	}
}

func TestLiveKitSIPEmitsTopologyAndHydratesContextBeforeGreeting(t *testing.T) { // telephony T10, V7, V9-V10, V13, V17-V20
	agent, resolved := configuredLiveKitSIP(t)
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"sip-inbound-trunk.json", "sip-dispatch-rule.json"} {
		content := artifactFile(t, artifact, path)
		if !json.Valid([]byte(content)) {
			t.Errorf("%s is not valid JSON:\n%s", path, content)
		}
		// Pinned as goldens on 2026-08-12, before the inline dial-out change,
		// so "inbound is untouched" is a byte comparison inside the repository
		// rather than a diff against a copy somebody made in /tmp.
		assertGoldenFile(t, filepath.Join("testdata", "golden", "livekit_v1_"+path), content, *updateLiveKitV1)
	}
	// No outbound-trunk input is emitted any more: nothing reads a stored
	// outbound trunk, so an input file for creating one would be a step whose
	// output nothing consumes (SCHEMA N33, 2026-08-12).
	for _, file := range artifact.Files {
		if file.Path == "sip-outbound-trunk.json" {
			t.Error("sip-outbound-trunk.json must not be emitted")
		}
	}
	for path, wants := range map[string][]string{
		"sip-inbound-trunk.json": {`${TWILIO_PHONE_NUMBER}`, `twilio inbound`},
		"sip-dispatch-rule.json": {`${UNMUTE_SIP_TRUNK_ID}`, `"agentName": "livekit"`, `\"direction\":\"inbound\"`},
	} {
		content := artifactFile(t, artifact, path)
		for _, want := range wants {
			if !strings.Contains(content, want) {
				t.Errorf("%s missing %q:\n%s", path, want, content)
			}
		}
		if strings.Contains(content, "secret-value") {
			t.Errorf("%s contains a secret value", path)
		}
	}
	for _, file := range artifact.Files {
		for _, forbidden := range []string{"TELNYX_SIP_", "PLIVO_SIP_", "EXOTEL_SIP_"} {
			if strings.Contains(string(file.Content), forbidden) {
				t.Errorf("%s contains unselected carrier environment %s", file.Path, forbidden)
			}
		}
	}

	agentPy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		`MAX_SESSIONS = 60`,
		`len(agent_server.active_jobs) / MAX_SESSIONS`,
		`drain_timeout=1200`,
		`load_threshold=1.0`,
		`"call_id": attributes.get("sip.callID")`,
		`"from_number": trunk_number if direction == "outbound" else remote_number`,
		`raise RuntimeError("phone_number must be an E.164 number")`,
		`raise RuntimeError("call_start.campaign_id must be string")`,
		`userdata.provider_call_id = value`,
		`userdata.call_direction = value`,
		// System sources and dispatched input variables hydrate through their own
		// call, so one path serves telephony and a plain `dev --var` session alike.
		`_hydrate_livekit_context(session.userdata, call_context)`,
		`_hydrate_call_start(session.userdata, call_start)`,
		`await session.start(agent=Intake(initial=True), room=ctx.room`,
		`await _livekit_entry_greeting(session)`,
		"result = await WarmTransferTask(",
		`sip_call_to=_sip_user(os.environ["BILLING_PHONE_NUMBER"]),`,
	} {
		if !strings.Contains(agentPy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	if strings.Contains(agentPy, "await session.start(agent=Intake(), room=ctx.room") {
		t.Error("every telephony startup agent must be marked initial")
	}
	hydrateAt := strings.Index(agentPy, "_hydrate_livekit_context(session.userdata")
	callStartAt := strings.Index(agentPy, "_hydrate_call_start(session.userdata")
	greetAt := strings.Index(agentPy, "await _livekit_entry_greeting(session)")
	if hydrateAt < 0 || greetAt < 0 || hydrateAt > greetAt {
		t.Error("LiveKit SIP context must hydrate before the first greeting")
	}
	// A greeting may name an input variable, so those must land first too.
	if callStartAt < 0 || callStartAt > greetAt {
		t.Error("dispatched input variables must hydrate before the first greeting")
	}
	env := artifactFile(t, artifact, ".env.example")
	// This fixture keeps the carrier-prefixed names on purpose. The compiler
	// carries whatever a Connection declares, so a package written before the
	// shipped example moved to the plain SIP names keeps working unchanged.
	for _, name := range []string{
		"TWILIO_SIP_ADDRESS", "TWILIO_SIP_USERNAME", "TWILIO_SIP_PASSWORD", "TWILIO_PHONE_NUMBER",
	} {
		if !strings.Contains(env, name+"=") {
			t.Errorf(".env.example missing %s", name)
		}
	}
	// The route's own values are absent: LiveKit Cloud injects its connection
	// trio into a deployed agent, and its managed SIP service owns the Redis this
	// agent never reads (FR-018).
	for _, name := range []string{"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "REDIS_URL"} {
		if strings.Contains(env, name) {
			t.Errorf(".env.example still asks for %s, which is not the reader's to set", name)
		}
	}
	// Dialling out registers nothing on the platform, so the stored outbound
	// trunk is not a name this package asks anybody for (SCHEMA N33).
	if strings.Contains(env, "LIVEKIT_SIP_OUTBOUND_TRUNK") {
		t.Error(".env.example still lists LIVEKIT_SIP_OUTBOUND_TRUNK")
	}
	readme := artifactFile(t, artifact, "README.md")
	for _, want := range []string{
		"## Telephony setup", "SIP trunking console", "twilio provider guide", "REDIS_URL", "not an audio hop",
		"no LiveKit outbound trunk is registered",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md missing %q", want)
		}
	}
	// The README still names the retired command and variable, on purpose, to
	// tell an operator who set up an earlier build that both are gone. What it
	// must not do is instruct anybody to create the trunk.
	for _, forbidden := range []string{"envsubst < sip-outbound-trunk.json", "Set LIVEKIT_SIP_OUTBOUND_TRUNK"} {
		if strings.Contains(readme, forbidden) {
			t.Errorf("README.md still tells the reader to create the outbound trunk: %q", forbidden)
		}
	}
	// No local Compose topology: this route is verified on a deployed agent, so
	// nothing here starts LiveKit Server, LiveKit SIP or a Redis for you.
	for _, path := range []string{"compose.telephony.yaml", "endpoint.Dockerfile", "baresip.conf"} {
		for _, file := range artifact.Files {
			if file.Path == path {
				t.Errorf("SIP route still emits the local plane file %q", path)
			}
		}
	}

	runtime := TelephonyRuntimePlanFor(resolved)
	if runtime.Coordination != "shared" || runtime.AdmissionOwner != "livekit_dispatch" {
		t.Fatalf("runtime coordination = %#v", runtime)
	}
	if len(runtime.Processes) != 1 || runtime.Processes[0].Readiness != "/" {
		t.Fatalf("runtime process = %#v", runtime.Processes)
	}
	required := strings.Join(runtime.RequiredEnv, ",")
	for _, want := range []string{"REDIS_URL", "TWILIO_SIP_PASSWORD"} {
		if !strings.Contains(required, want) {
			t.Errorf("runtime required env missing %s: %s", want, required)
		}
	}
	// No environment name carries a trunk ID in either direction (SCHEMA N36).
	if strings.Contains(required, "LIVEKIT_SIP_INBOUND_TRUNK") {
		t.Errorf("runtime still requires the retired inbound trunk name: %s", required)
	}
	if strings.Contains(required, "LIVEKIT_SIP_URI") {
		t.Errorf("runtime still requires the unused LIVEKIT_SIP_URI: %s", required)
	}
	// Dialling out needs no stored trunk, so the runtime asks for no trunk id
	// it cannot get from the carrier (SCHEMA N33, 2026-08-12).
	if strings.Contains(required, "LIVEKIT_SIP_OUTBOUND_TRUNK") {
		t.Errorf("runtime still requires the retired outbound trunk id: %s", required)
	}
	if strings.Contains(required, "UNMUTE_OUTBOUND_TOKEN") {
		t.Errorf("LiveKit SIP runtime requires unused outbound token: %s", required)
	}
	if len(runtime.PublicEndpoints) != 0 {
		t.Fatalf("LiveKit SIP must not report HTTP callback endpoints: %#v", runtime.PublicEndpoints)
	}
}

func TestLiveKitSIPCapacityIsExactAtOneAndN(t *testing.T) { // telephony V22, V30
	for _, maxSessions := range []int{1, 60} {
		t.Run(fmt.Sprintf("max_%d", maxSessions), func(t *testing.T) {
			agent, resolved := configuredLiveKitSIP(t)
			agent.Capacity.MaxSessions = maxSessions
			artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			agentPy := artifactFile(t, artifact, "agent.py")
			for _, want := range []string{
				fmt.Sprintf("MAX_SESSIONS = %d", maxSessions),
				`return min(len(agent_server.active_jobs) / MAX_SESSIONS, 1.0)`,
				`load_threshold=1.0`,
			} {
				if !strings.Contains(agentPy, want) {
					t.Errorf("agent.py missing exact-capacity expression %q", want)
				}
			}
			if strings.Contains(agentPy, `load_threshold=(MAX_SESSIONS - 1) / MAX_SESSIONS`) {
				t.Error("agent.py retains the capacity-shrinking threshold")
			}

			load := func(active int) float64 {
				return min(float64(active)/float64(maxSessions), 1)
			}
			if load(maxSessions-1) >= 1 {
				t.Fatalf("worker becomes full before session %d", maxSessions)
			}
			if load(maxSessions) != 1 {
				t.Fatalf("worker is not full at session %d", maxSessions)
			}
		})
	}
}

func configuredLiveKitSIP(t *testing.T) (*ir.Agent, ir.Target) {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	addColdHumanTransfer(pkg)
	enablePackageTelephony(pkg)
	configured := pkg.Targets["livekit"]
	configured.Connection = "primary_phone"
	setConnectionRoute(pkg, "primary_phone", "sip", "twilio")
	pkg.Targets = map[string]spec.Target{"livekit": configured}
	connection := pkg.Connections["primary_phone"]
	connection.Environment = map[string]string{
		"sip_address": "TWILIO_SIP_ADDRESS", "sip_username": "TWILIO_SIP_USERNAME",
		"sip_password": "TWILIO_SIP_PASSWORD", "from_number": "TWILIO_PHONE_NUMBER",
	}
	pkg.Connections["primary_phone"] = connection
	outbound := true
	phone := pkg.Agent.Channels["phone"]
	phone.Outbound, phone.OnVoicemail = &outbound, "hangup"
	pkg.Agent.Channels["phone"] = phone
	pkg.Agent.Variables["campaign_id"] = spec.Variable{Type: "string", Source: "call_start", Default: "manual"}
	pkg.Agent.Variables["provider_call_id"] = spec.Variable{Type: "string", Source: "call_id"}
	pkg.Agent.Variables["call_direction"] = spec.Variable{Type: "string", Source: "direction"}
	human := pkg.Agent.Controls["to_human"]
	human.Cold = nil
	human.Warm = &spec.WarmTransfer{Destination: "billing_line", Briefing: "Say who is calling and why."}
	pkg.Agent.Controls["to_human"] = human

	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent, agent.Targets["livekit"]
}

func configuredLiveKitSIPCold(t *testing.T) (*ir.Agent, ir.Target) {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	addColdHumanTransfer(pkg)
	enablePackageTelephony(pkg)
	configured := pkg.Targets["livekit"]
	configured.Connection = "primary_phone"
	setConnectionRoute(pkg, "primary_phone", "sip", "twilio")
	pkg.Targets = map[string]spec.Target{"livekit": configured}
	connection := pkg.Connections["primary_phone"]
	connection.Environment = map[string]string{
		"sip_address": "TWILIO_SIP_ADDRESS", "sip_username": "TWILIO_SIP_USERNAME",
		"sip_password": "TWILIO_SIP_PASSWORD", "from_number": "TWILIO_PHONE_NUMBER",
	}
	pkg.Connections["primary_phone"] = connection

	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent, agent.Targets["livekit"]
}

func configuredLiveKitConnector(t *testing.T) (*ir.Agent, ir.Target) {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	// The connector supports no transfers yet, so drop the human transfer
	// control, every reference to it, and the destination it resolved to.
	// primary_phone already carries the Twilio account trio (account_sid,
	// auth_token, from_number).
	inbound, outbound := true, true
	pkg.Agent.Channels["phone"] = spec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
		RequiredControls: []string{"hangup"},
	}
	// Exercise the metadata/hydrate path: the bridge writes these into the
	// dispatch metadata and the connector agent branch reads them back.
	pkg.Agent.Variables["campaign_id"] = spec.Variable{Type: "string", Source: "call_start", Default: "manual"}
	pkg.Agent.Variables["provider_call_id"] = spec.Variable{Type: "string", Source: "call_id"}
	pkg.Agent.Variables["call_direction"] = spec.Variable{Type: "string", Source: "direction"}
	configured := pkg.Targets["livekit"]
	configured.Connection = "primary_phone"
	setConnectionRoute(pkg, "primary_phone", "connector", "twilio")
	pkg.Targets = map[string]spec.Target{"livekit": configured}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return agent, agent.Targets["livekit"]
}

// The LiveKit Twilio connector generates its own open-source Media Streams
// bridge (SPEC T2, V1-V4): the bridge and connector agent branch are emitted,
// there is no SIP trunk or Redis, no LiveKit Cloud reference, and the env is
// the Twilio account trio the same as Pipecat.
func TestLiveKitConnectorGeneratesBridgeWithoutCloudOrSIP(t *testing.T) {
	agent, resolved := configuredLiveKitConnector(t)
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	bridge := artifactFile(t, artifact, "telephony_bridge.py")
	for _, want := range []string{
		"_mulaw_selfcheck()",
		"api.RoomAgentDispatch(agent_name=AGENT_NAME",
		"rtc.AudioSource(SAMPLE_RATE, NUM_CHANNELS)",
		"RequestValidator",
		`app.router.add_post("/telephony/inbound", inbound)`,
		`app.router.add_post("/telephony/outbound", outbound)`,
		`app.router.add_get("/telephony/ws/{token}", media_ws)`,
	} {
		if !strings.Contains(bridge, want) {
			t.Errorf("telephony_bridge.py missing %q", want)
		}
	}

	agentPy := artifactFile(t, artifact, "agent.py")
	if !strings.Contains(agentPy, "_livekit_call_context(ctx.room.name, metadata)") {
		t.Error("agent.py connector branch must build context from metadata")
	}
	if !strings.Contains(agentPy, "await session.start(agent=Intake(initial=True), room=ctx.room") {
		t.Error("connector startup agent must be marked initial")
	}
	if strings.Contains(agentPy, "await session.start(agent=Intake(), room=ctx.room") {
		t.Error("connector startup agent must not use the return-handoff default")
	}
	if strings.Contains(agentPy, "create_sip_participant") {
		t.Error("connector agent.py must not create SIP participants")
	}

	// V1: no LiveKit Cloud dependency anywhere in the generated project.
	for _, file := range artifact.Files {
		content := string(file.Content)
		for _, forbidden := range []string{"livekit.cloud", "ConnectTwilioCall", "connect_twilio_call", "LIVEKIT_SIP_URI"} {
			if strings.Contains(content, forbidden) {
				t.Errorf("%s references %q (connector must be self-hosted only)", file.Path, forbidden)
			}
		}
	}
	// No SIP trunk inputs on the connector route.
	for _, file := range artifact.Files {
		if strings.HasPrefix(file.Path, "sip-") {
			t.Errorf("connector emitted a SIP input file %q", file.Path)
		}
	}

	// The bridge is the deployed webhook server. Its local Compose file is gone
	// with every other local phone topology.
	for _, file := range artifact.Files {
		if file.Path == "compose.telephony.yaml" {
			t.Error("connector still emits a local telephony Compose file")
		}
	}
	if !strings.Contains(artifactFile(t, artifact, "telephony_bridge.py"), "aiohttp") {
		t.Error("connector must still emit its bridge")
	}

	env := artifactFile(t, artifact, ".env.example")
	for _, want := range []string{"TWILIO_ACCOUNT_SID=", "TWILIO_AUTH_TOKEN=", "TWILIO_PHONE_NUMBER="} {
		if !strings.Contains(env, want) {
			t.Errorf(".env.example missing %q", want)
		}
	}
	// `unmute dev` mints both of these and a deployment's platform supplies
	// them, so they are not the reader's to fill in (FR-018c).
	for _, forbidden := range []string{"UNMUTE_PUBLIC_URL", "UNMUTE_OUTBOUND_TOKEN", "TWILIO_SIP_", "LIVEKIT_SIP_INBOUND_TRUNK", "REDIS_URL"} {
		if strings.Contains(env, forbidden) {
			t.Errorf(".env.example contains SIP-only %q", forbidden)
		}
	}
	if pyproject := artifactFile(t, artifact, "pyproject.toml"); !strings.Contains(pyproject, `"aiohttp"`) || !strings.Contains(pyproject, `"twilio"`) {
		t.Errorf("pyproject missing bridge deps:\n%s", pyproject)
	}

	plan := TelephonyRuntimePlanFor(resolved)
	services := strings.Join(plan.Services, ",")
	if services != "application,livekit_server" {
		t.Errorf("connector services = %q, want application,livekit_server", services)
	}
	if len(plan.PublicEndpoints) != 4 || plan.AutoWebhookEndpoint != "inbound" {
		t.Errorf("connector runtime facts = %#v", plan)
	}
}

// TestLiveKitV1PinsAndSDKLanguage covers the T7 remainders (C6/C1): plugin
// pins raise dep floors and are range-checked; a non-python sdk_language
// fails loud instead of emitting a silently-wrong Python project.
func TestLiveKitV1PinsAndSDKLanguage(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	tgt.Pins = map[string]string{"livekit-plugins-slng": "1.7.0"}
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate with pin: %v", err)
	}
	if pyproject := artifactFile(t, artifact, "pyproject.toml"); !strings.Contains(pyproject, `"livekit-plugins-slng>=1.7.0"`) {
		t.Errorf("pin did not raise the plugin floor:\n%s", pyproject)
	}
	// No deployment config file is emitted: the platform assigns both of its
	// values, and its presence makes `lk agent create` refuse (FR-008).
	for _, file := range artifact.Files {
		if strings.HasPrefix(file.Path, "livekit") && strings.HasSuffix(file.Path, ".toml") {
			t.Errorf("emitted %s; LiveKit Cloud writes that file itself", file.Path)
		}
	}

	tgt.Pins = map[string]string{"livekit-plugins-slng": "1.0.0"}
	if _, err := Generate(agent, tgt, target.Default()); err == nil || !strings.Contains(err.Error(), "below the catalogue floor") {
		t.Fatalf("below-floor pin must fail, got %v", err)
	}
	tgt.Pins = map[string]string{"left-pad": "1.0.0"}
	if _, err := Generate(agent, tgt, target.Default()); err == nil || !strings.Contains(err.Error(), "not a pinnable package") {
		t.Fatalf("unknown pin must fail, got %v", err)
	}
	tgt.Pins = nil
	tgt.SDKLanguage = "node"
	if _, err := Generate(agent, tgt, target.Default()); err == nil || !strings.Contains(err.Error(), "python projects only") {
		t.Fatalf("sdk_language node must fail loud, got %v", err)
	}
}

// TestLiveKitV1MCPSelectionTransportAndScope covers the three things a source
// declares beyond its address (N40): which tools it offers, which transport it
// speaks, and which scope it belongs to. Two sources share one `url_env` on
// purpose — that is the case the old collapse-by-env code merged into a single
// mount and can no longer.
func TestLiveKitV1MCPSelectionTransportAndScope(t *testing.T) {
	const secret = "bk-live-pretend-key"
	t.Setenv("BOOKINGS_MCP_TOKEN", secret)
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Tools["book_table"] = ir.Tool{
		Execution: ir.ToolMCP, URLEnv: "BOOKINGS_MCP_URL", MCPTransport: ir.MCPTransportSSE,
		MCPTools: []string{"reserve", "cancel"},
		Auth:     &ir.ToolAuth{Type: ir.ToolAuthBearer, TokenEnv: "BOOKINGS_MCP_TOKEN"},
	}
	// Same address, nothing else stated: every tool, platform-chosen transport,
	// no authentication, and a scope of its own.
	agent.Tools["browse_tables"] = ir.Tool{Execution: ir.ToolMCP, URLEnv: "BOOKINGS_MCP_URL"}
	greeter := agent.Agents["greeter"]
	greeter.Tools = append(greeter.Tools, "book_table")
	agent.Agents["greeter"] = greeter
	task := agent.Tasks["find_slot"]
	task.Tools = append(task.Tools, "browse_tables")
	agent.Tasks["find_slot"] = task

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	agentpy := artifactFile(t, artifact, "agent.py")
	greeterBody := pyClassBody(t, agentpy, "class Greeter(")
	taskBody := pyClassBody(t, agentpy, "class FindSlot(")

	// The stated transport and the selection ride the agent's mount, and only
	// the agent's: a task-scoped source is offered inside that task alone.
	if !strings.Contains(agentpy, `return mcp.MCPToolset(id="book_table", mcp_server=mcp.MCPServerHTTP(url=os.environ["BOOKINGS_MCP_URL"], transport_type="sse", allowed_tools=["reserve", "cancel"], headers=_bearer("BOOKINGS_MCP_TOKEN"), timeout=30, client_session_timeout_seconds=30))`) {
		t.Errorf("the agent's source is not constructed as declared:\n%s", agentpy)
	}
	if !strings.Contains(greeterBody, `tools=[_mcp_toolset("book_table")]`) {
		t.Errorf("the agent does not mount its source:\n%s", greeterBody)
	}
	if strings.Contains(greeterBody, "browse_tables") {
		t.Errorf("a task-scoped source must not reach the agent:\n%s", greeterBody)
	}
	// Nothing stated means nothing emitted: an empty allowed_tools would claim
	// a selection the author never made (SC-004).
	if !strings.Contains(agentpy, `return mcp.MCPToolset(id="browse_tables", mcp_server=mcp.MCPServerHTTP(url=os.environ["BOOKINGS_MCP_URL"], timeout=30, client_session_timeout_seconds=30))`) {
		t.Errorf("the task's source emits an argument the source never declared:\n%s", agentpy)
	}
	if !strings.Contains(taskBody, `tools=[_mcp_toolset("browse_tables")]`) {
		t.Errorf("the task does not mount its source:\n%s", taskBody)
	}
	if strings.Contains(taskBody, "book_table") {
		t.Errorf("an agent-scoped source must not reach the task:\n%s", taskBody)
	}
	// One helper per scheme in use, however many sources read it (V8).
	if got := strings.Count(agentpy, "def _bearer("); got != 1 {
		t.Errorf("_bearer defined %d times, want 1", got)
	}
	if strings.Count(agentpy, "def _api_key(") != 0 {
		t.Error("a scheme no tool declares must not emit its helper")
	}
	// Two sources at one address stay two mounts: the selection lives on the
	// source, so merging them would offer each scope the other's tools.
	if got := strings.Count(agentpy, "mcp.MCPToolset("); got != 2 {
		t.Errorf("%d mounts emitted, want one per source", got)
	}
	if !strings.Contains(agentpy, `sources = ("book_table", "browse_tables",)`) {
		t.Errorf("the two distinct sources are not both preflighted:\n%s", agentpy)
	}
	// The compiler never reads a secret's value, so nothing it writes can carry
	// one (SC-005).
	for _, file := range artifact.Files {
		if strings.Contains(string(file.Content), secret) {
			t.Errorf("%s carries a secret value", file.Path)
		}
	}
}

// TestLiveKitV1MCPPreflightIsRequired protects the startup contract LiveKit's
// own AgentActivity does not provide: every mounted MCP source must connect
// before AgentSession.start can greet the caller. The factory is shared with
// runtime mounts so the check cannot drift from the tool the agent later uses.
func TestLiveKitV1MCPPreflightIsRequired(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Tools["book_table"] = ir.Tool{
		Execution: ir.ToolMCP, URLEnv: "BOOKINGS_MCP_URL",
		MCPTransport: ir.MCPTransportStreamableHTTP,
	}
	greeter := agent.Agents["greeter"]
	greeter.Tools = append(greeter.Tools, "book_table")
	agent.Agents["greeter"] = greeter
	// Mounting one source in two scopes must still preflight it only once.
	task := agent.Tasks["find_slot"]
	task.Tools = append(task.Tools, "book_table")
	agent.Tasks["find_slot"] = task

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	agentpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		`def _mcp_toolset(source: str) -> mcp.MCPToolset:`,
		`if source == "book_table":`,
		`tools=[_mcp_toolset("book_table")]`,
		`async def _preflight_mcp() -> None:`,
		`async def probe(source: str):`,
		`sources = ("book_table",)`,
		`*(probe(source) for source in sources)`,
		`return_exceptions=True`,
		`*(("setup", source, error) for source, error in setup_failures)`,
		`*(("cleanup", source, error) for source, error in close_failures)`,
		`"MCP preflight %s failed for %s"`,
	} {
		if !strings.Contains(agentpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	if got := strings.Count(agentpy, "mcp.MCPToolset("); got != 1 {
		t.Errorf("MCP constructor emitted %d times, want one shared factory branch", got)
	}
	connect := strings.Index(agentpy, "    await ctx.connect()")
	preflight := strings.Index(agentpy, "    await _preflight_mcp()")
	start := strings.Index(agentpy, "    await session.start(")
	if connect < 0 || preflight < connect || start < preflight {
		t.Errorf("MCP preflight order must be connect, preflight, start: connect=%d preflight=%d start=%d", connect, preflight, start)
	}
}

func TestLiveKitV1MCPPreflightConnectsOnceOnPhoneRoutes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(*testing.T) (*ir.Agent, ir.Target)
	}{
		{name: "sip", build: configuredLiveKitSIP},
		{name: "connector", build: configuredLiveKitConnector},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent, resolved := tc.build(t)
			agent.Tools["book_table"] = ir.Tool{
				Execution: ir.ToolMCP, URLEnv: "BOOKINGS_MCP_URL",
			}
			entry := agent.Agents[agent.EntryAgent]
			entry.Tools = append(entry.Tools, "book_table")
			agent.Agents[agent.EntryAgent] = entry

			artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			agentpy := artifactFile(t, artifact, "agent.py")
			if got := strings.Count(agentpy, "    await ctx.connect()"); got != 1 {
				t.Fatalf("ctx.connect() emitted %d times, want 1", got)
			}
			connect := strings.Index(agentpy, "    await ctx.connect()")
			preflight := strings.Index(agentpy, "    await _preflight_mcp()")
			start := strings.Index(agentpy, "    await session.start(")
			if connect < 0 || preflight < connect || start < preflight {
				t.Errorf("MCP preflight order must be connect, preflight, start: connect=%d preflight=%d start=%d", connect, preflight, start)
			}
		})
	}
}

// pyClassBody returns one emitted class, from its header to the next
// top-level class, so a test can ask what a single agent or task carries.
func pyClassBody(t *testing.T, source, header string) string {
	t.Helper()
	start := strings.Index(source, header)
	if start < 0 {
		t.Fatalf("%q not emitted", header)
	}
	rest := source[start+len(header):]
	if end := strings.Index(rest, "\nclass "); end >= 0 {
		return source[start : start+len(header)+end]
	}
	return source[start:]
}

// TestLiveKitV1LocalAndMCPTools covers the tool executions beyond webhook:
// local copies the package handler into tools/<name>.py and wraps it (SCHEMA
// §5, code targets); mcp mounts one MCPToolset per source on the agent's tools
// surface, with only the arguments the source actually declares (N40). The
// local handler rides spec.Load like instructions do.
func TestLiveKitV1LocalAndMCPTools(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Tools["fetch_notes"] = ir.Tool{
		Description: "Fetch the caller's saved notes.",
		Input:       map[string]any{"type": "object", "properties": map[string]any{"topic": map[string]any{"type": "string"}}, "required": []any{"topic"}},
		Execution:   ir.ToolLocal, Handler: "tools/fetch_notes.py",
		HandlerSource: "def fetch_notes(topic):\n    return {\"notes\": []}\n",
		Interruption:  ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
	}
	agent.Tools["book_table"] = ir.Tool{
		Execution: ir.ToolMCP, URLEnv: "BOOKINGS_MCP_URL",
		MCPTransport: ir.MCPTransportStreamableHTTP, MCPTools: []string{"reserve", "cancel"},
		Auth:         &ir.ToolAuth{Type: ir.ToolAuthBearer, TokenEnv: "BOOKINGS_MCP_TOKEN"},
		Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
	}
	def := agent.Agents["greeter"]
	def.Tools = append(def.Tools, "fetch_notes", "book_table")
	agent.Agents["greeter"] = def

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"import inspect",
		"import tools.fetch_notes",
		"async def fetch_notes(self, ctx: RunContext, topic: str) -> dict:",
		"result = tools.fetch_notes.fetch_notes(topic=topic)",
		"if inspect.isawaitable(result):",
		`return mcp.MCPToolset(id="book_table", mcp_server=mcp.MCPServerHTTP(url=os.environ["BOOKINGS_MCP_URL"], transport_type="streamable_http", allowed_tools=["reserve", "cancel"], headers=_bearer("BOOKINGS_MCP_TOKEN"), timeout=30, client_session_timeout_seconds=30))`,
		`tools=[_mcp_toolset("book_table")],`,
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
	}
	// The deprecated parameter is gone for good: it logs a warning on every
	// start since 1.5.11, so a generated project must never emit it (N40).
	if strings.Contains(botpy, "mcp_servers=") {
		t.Error("agent.py still emits the deprecated mcp_servers= parameter")
	}
	// The extra is what makes the import work at all; it is separate from the
	// globally supported framework version.
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if !strings.Contains(pyproject, "livekit-agents[mcp,") {
		t.Errorf("pyproject must carry the mcp extra:\n%s", pyproject)
	}
	// A missing address or token is named before the agent dials (FR-009).
	for _, want := range []string{`"BOOKINGS_MCP_URL"`, `"BOOKINGS_MCP_TOKEN"`} {
		if !strings.Contains(requiredEnvBlock(t, botpy), want) {
			t.Errorf("REQUIRED_ENV missing %s", want)
		}
	}
	if handler := artifactFile(t, artifact, "tools/fetch_notes.py"); !strings.Contains(handler, "def fetch_notes(topic):") {
		t.Errorf("handler not copied verbatim:\n%s", handler)
	}
	artifactFile(t, artifact, "tools/__init__.py")
	if env := artifactFile(t, artifact, ".env.example"); !strings.Contains(env, "BOOKINGS_MCP_URL") {
		t.Error(".env.example missing the MCP server env")
	}
}

// TestLiveKitEmitterMatchesCapabilityTable is the table↔emitter agreement test
// (V15, mirroring pipecat's): the emitter's declared code paths must equal the
// table's non-gated LiveKit rows, so no field is validate-green yet silently
// unemitted (B1's class).
func TestLiveKitEmitterMatchesCapabilityTable(t *testing.T) {
	table := target.Default()
	for field := range table.Fields {
		capability := table.Capability(field, target.LiveKit)
		supported := capability.Tag != target.Gated && capability.Tag != target.Provisional
		if livekitEmittedFields[field] != supported {
			t.Errorf("field %q: emitter emits=%v, table supported=%v (tag %q) — implement or gate to reconcile",
				field, livekitEmittedFields[field], supported, capability.Tag)
		}
	}
}

// TestLiveKitV1ParityFixture is the V14 fixture: one package loaded with every
// SCHEMA §7 livekit-ok feature at once must validate green AND generate — no
// validate-green/generate-fail is representable (B2's class).
func TestLiveKitV1ParityFixture(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	enabled, noCalls := true, false
	agent.Conversation.Interruption = &ir.Interruption{Enabled: &enabled, MinimumWords: 2, IgnorePhrases: []string{"uh-huh"}}
	agent.Conversation.ThinkingAudio = ir.ThinkingSubtle
	agent.Variables["visit_count"] = ir.Variable{Type: ir.PrimitiveInteger}
	agent.Models["backup"] = ir.ModelDef{Kind: ir.KindThink, Placement: ir.PlacementAPI}
	profile := agent.Models["reasoning"]
	profile.Fallback = []string{"backup"}
	agent.Models["reasoning"] = profile
	toRes := agent.Controls["to_reservations"].(*ir.AgentTransfer)
	toRes.Requires = []string{"caller_phone"}
	toRes.Context.History = ir.HistoryLastN
	toRes.Context.MaxMessages = 6
	toRes.Context.IncludeToolCalls = &noCalls
	toRes.Context.Variables = ir.VariableSelection{Names: []string{"caller_phone"}}
	back := agent.Controls["back_to_greeter"].(*ir.AgentTransfer)
	back.Context.History = ir.HistorySummary
	back.Context.Summarizer = "backup"
	// Cold, not warm. A warm transfer dials the destination itself and needs the
	// carrier credentials a telephony Connection carries (SCHEMA N33,
	// 2026-08-12). Warm's lowering is covered on a SIP target by
	// TestLiveKitV1HumanTransferColdAndWarm and
	// TestLiveKitSIPEmitsTopologyAndHydratesContextBeforeGreeting.
	//
	// Cold needs a route too, since 2026-08-15: it hands over the caller's own
	// leg, and a target that names no route has none (FR-003). The fixture names
	// one below rather than dropping the transfer, because a parity fixture whose
	// job is "every livekit-ok feature at once" has to keep the feature.
	agent.Controls["to_human"] = &ir.HumanTransfer{Kind: ir.ControlHumanTransfer, Destination: "line", Mode: ir.TransferCold, OnUnavailable: ir.OnUnavailableReturn}
	agent.Controls["to_human_cold"] = &ir.HumanTransfer{Kind: ir.ControlHumanTransfer, Destination: "line", Mode: ir.TransferCold, RingTimeout: "20s", OnUnavailable: ir.OnUnavailableHangup}
	agent.Tools["fetch_notes"] = ir.Tool{
		Description: "Fetch the caller's saved notes.",
		Input:       map[string]any{"type": "object", "properties": map[string]any{"topic": map[string]any{"type": "string"}}},
		Execution:   ir.ToolLocal, Handler: "tools/fetch_notes.py", HandlerSource: "def fetch_notes(topic):\n    return {}\n",
		Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
	}
	// Two mcp sources on one server address: agent-scoped, fully specified,
	// and task-scoped with nothing but the address, so the fixture pins both
	// the full mount and the one that emits no optional argument at all (N40).
	agent.Tools["book_table"] = ir.Tool{
		Execution: ir.ToolMCP, URLEnv: "BOOKINGS_MCP_URL",
		MCPTransport: ir.MCPTransportStreamableHTTP, MCPTools: []string{"reserve"},
		Auth:         &ir.ToolAuth{Type: ir.ToolAuthAPIKey, TokenEnv: "BOOKINGS_MCP_TOKEN", Header: ir.DefaultAPIKeyHeader},
		Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
	}
	agent.Tools["browse_tables"] = ir.Tool{
		Execution: ir.ToolMCP, URLEnv: "BOOKINGS_MCP_URL",
		Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
	}
	def := agent.Agents["greeter"]
	def.Tools = append(def.Tools, "to_human", "to_human_cold", "fetch_notes", "book_table")
	agent.Agents["greeter"] = def
	reserve := agent.TaskGroups["reserve_group"]
	reserve.ContextScope = ir.ContextIsolated
	agent.TaskGroups["reserve_group"] = reserve
	task := agent.Tasks["find_slot"]
	task.Model = "backup"
	task.Result["details"] = ir.ResultField{Schema: map[string]any{"type": "object"}}
	task.Tools = append(task.Tools, "browse_tables")
	agent.Tasks["find_slot"] = task
	agent.Controls["do_find"] = &ir.Delegate{Kind: ir.ControlDelegate, Task: "find_slot", Assign: map[string]string{"caller_phone": "result.date"}}
	resDef := agent.Agents["reservations"]
	resDef.Tools = append(resDef.Tools, "do_find")
	agent.Agents["reservations"] = resDef

	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	tgt.Connection = "twilio_sip"
	tgt.Models.Reason["backup"] = ir.Binding{Model: "openai/gpt-4o"}
	tgt.Destinations = map[string]string{"line": "+14155550123"}
	tgt.Pins = map[string]string{"livekit-plugins-slng": "1.7.0"}

	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("the fully-loaded fixture must generate: %v", err)
	}
	if len(artifact.Files) == 0 {
		t.Fatal("no files emitted")
	}
}

// TestCheckLiveKitVersion pins the supported version against the recorded window
// in internal/target. The driver keeps its own check as a backstop; the fact it
// checks against lives in one place.
func TestCheckLiveKitVersion(t *testing.T) {
	for _, tc := range []struct {
		version string
		ok      bool
	}{
		{"1.6.10", true},
		// A declared version is an exact install pin, so half a version is no
		// longer accepted and resolved on the author's behalf.
		{"1.5", false},
		{"1.5.2", false},
		{"1.6.0", false},
		{"1.6.9", false},
		{"1.2", false},
		{"1.4.9", false},
		{"0.0.108", false},
		{"1.6.11", false}, // above the ceiling: unverified is unsupported
		{"2.0.0", false},
		{"", false},
		{"latest", false},
	} {
		err := checkLiveKitVersion(tc.version)
		if (err == nil) != tc.ok {
			t.Errorf("checkLiveKitVersion(%q): ok=%v, err=%v", tc.version, tc.ok, err)
		}
	}
}

// TestV17_SlngRouteVerbatim (driver-livekit V17, B4): slng routes reach the
// plugin's model= argument verbatim — the slng/ prefix names the SLNG-hosted
// route family and is part of the URL path, so stripping it 404s.
func TestV17_SlngRouteVerbatim(t *testing.T) {
	for _, tc := range []struct {
		role  string
		route string
	}{
		{"listen", "slng/deepgram/nova:3-en"},
		{"speak", "slng/deepgram/aura:2-en"},
	} {
		binding := ir.Binding{Provider: "slng", Model: tc.route}
		var svc livekitService
		var err error
		if tc.role == "listen" {
			svc, err = livekitSTTService(&binding, newEnvSet())
		} else {
			binding.Voice = "aura-2-thalia-en"
			svc, err = livekitTTSService(binding, newEnvSet())
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.role, err)
		}
		found := false
		for _, kv := range svc.Call.Args {
			if kv.Key == "model" {
				found = true
				if kv.Value != pyQuote(tc.route) {
					t.Errorf("%s model = %s, want %s (route must pass verbatim)", tc.role, kv.Value, pyQuote(tc.route))
				}
			}
		}
		if !found {
			t.Errorf("%s call has no model kwarg", tc.role)
		}
	}
}

// TestV18_TurnBindingLowersToDetectorVersion (driver-livekit V18, B5): the
// target's turn: binding maps to the inference.TurnDetector version — mini is
// fully local (no LiveKit Cloud creds), absent means SDK auto-select (C5),
// and an unrecognized model fails loud instead of being silently dropped.
func TestV18_TurnBindingLowersToDetectorVersion(t *testing.T) {
	for _, tc := range []struct {
		binding *ir.Binding
		want    string
		wantErr bool
	}{
		{nil, "", false},
		{&ir.Binding{Provider: "livekit"}, "", false},
		{&ir.Binding{Provider: "livekit", Model: "turn-detector-mini"}, "v1-mini", false},
		{&ir.Binding{Provider: "livekit", Model: "turn-detector"}, "v1", false},
		{&ir.Binding{Provider: "livekit", Model: "eou-9000"}, "", true},
	} {
		got, err := livekitTurnVersion(tc.binding)
		if (err != nil) != tc.wantErr {
			t.Errorf("livekitTurnVersion(%+v): err=%v, wantErr=%v", tc.binding, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("livekitTurnVersion(%+v) = %q, want %q", tc.binding, got, tc.want)
		}
	}
}

// TestV19_NativeReasonBeatsInferenceWildcard (driver-livekit V19, B6): a
// reason vendor with a native catalogue entry binds that plugin with its own
// key env — never the Inference wildcard — so the scaffold default runs
// console with no LiveKit Cloud creds. provider: livekit stays the deliberate
// Inference spelling with the model passed verbatim.
func TestV19_NativeReasonBeatsInferenceWildcard(t *testing.T) {
	env := newEnvSet()
	svc, err := livekitChainService(ir.Binding{Provider: "openai", Model: "gpt-4.1-mini"}, env, slngSite{})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Call.Class != "openai.LLM" {
		t.Errorf("openai reason class = %q, want openai.LLM", svc.Call.Class)
	}
	if !env.seen["OPENAI_API_KEY"] {
		t.Error("openai reason binding did not register OPENAI_API_KEY")
	}
	if env.seen["LIVEKIT_API_KEY"] {
		t.Error("openai reason binding registered LIVEKIT_API_KEY (wildcard leak)")
	}

	svc, err = livekitChainService(ir.Binding{Provider: "livekit", Model: "openai/gpt-4o-mini"}, newEnvSet(), slngSite{})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Call.Class != "inference.LLM" {
		t.Errorf("provider livekit class = %q, want inference.LLM", svc.Call.Class)
	}
	for _, kv := range svc.Call.Args {
		if kv.Key == "model" && kv.Value != pyQuote("openai/gpt-4o-mini") {
			t.Errorf("inference model = %s, want %s (verbatim, no livekit/ join)", kv.Value, pyQuote("openai/gpt-4o-mini"))
		}
	}
}

func TestLiveKitV1OpenAIResponsesMode(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	binding := tgt.Models.Reason["reasoning"]
	binding.Model = "gpt-5.6-terra"
	binding.Params = map[string]any{
		"api":              "responses",
		"reasoning_effort": "low",
		"use_websocket":    false,
	}
	tgt.Models.Reason["reasoning"] = binding

	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	agentPy := artifactFile(t, artifact, "agent.py")
	want := `openai.responses.LLM(api_key=os.environ["OPENAI_API_KEY"], model="gpt-5.6-terra", reasoning=openai_types.Reasoning(effort="low"), use_websocket=False)`
	if !strings.Contains(agentPy, want) {
		t.Errorf("agent.py missing Responses API constructor %q", want)
	}
	if !strings.Contains(agentPy, "from openai import types as openai_types") {
		t.Error("agent.py does not import the public OpenAI Reasoning type")
	}
	readme := artifactFile(t, artifact, "README.md")
	if !strings.Contains(readme, "OpenAI Responses API") {
		t.Error("README.md omits the Responses API runbook note")
	}
	report := artifactFile(t, artifact, "compile-report.json")
	if !strings.Contains(report, "reason: openai via openai.responses.LLM (livekit-agents[openai], verified 2026-08-18)") {
		t.Error("compile report names the wrong OpenAI API class")
	}

	for _, tc := range []struct {
		name   string
		params map[string]any
		want   string
	}{
		{
			name:   "provider default reasoning",
			params: map[string]any{"api": "responses"},
			want:   `openai.responses.LLM(api_key=os.environ["OPENAI_API_KEY"], model="gpt-5.6-terra")`,
		},
		{
			name: "reasoning-like user value",
			params: map[string]any{
				"api":  "responses",
				"user": "openai_types.Reasoning(",
			},
			want: `user="openai_types.Reasoning("`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binding.Params = tc.params
			tgt.Models.Reason["reasoning"] = binding
			artifact, err := Generate(agent, tgt, target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			agentPy := artifactFile(t, artifact, "agent.py")
			if !strings.Contains(agentPy, tc.want) {
				t.Errorf("agent.py missing Responses API constructor %q", tc.want)
			}
			if strings.Contains(agentPy, "from openai import types as openai_types") {
				t.Error("agent.py imports OpenAI Reasoning without using it")
			}
		})
	}
}

func TestLiveKitV1OpenAIResponsesRejectsConflictingParams(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params map[string]any
		want   string
	}{
		{
			name:   "unsupported api",
			params: map[string]any{"api": "chat"},
			want:   `unsupported api chat; want "responses"`,
		},
		{
			name:   "non-string api",
			params: map[string]any{"api": true},
			want:   `unsupported api true; want "responses"`,
		},
		{
			name: "two reasoning forms",
			params: map[string]any{
				"api":              "responses",
				"reasoning":        map[string]any{"effort": "low"},
				"reasoning_effort": "low",
			},
			want: "does not accept raw reasoning; use reasoning_effort",
		},
		{
			name: "raw reasoning",
			params: map[string]any{
				"api":       "responses",
				"reasoning": map[string]any{"effort": "low"},
			},
			want: "does not accept raw reasoning; use reasoning_effort",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := livekitChainService(ir.Binding{
				Provider: "openai",
				Model:    "gpt-5.6-terra",
				Params:   tc.params,
			}, newEnvSet(), slngSite{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// TestLiveKitV1ToolAnnounceSpeaksBeforeTheWork: an announcing tool speaks the
// exact authored line before it does its work, on both tool bodies, and never
// awaits the handle. Awaiting would hold the request until playout finished,
// which is the delay the field exists to hide (FR-008, FR-009). A tool with no
// announcement keeps a body with no speech in it at all (FR-010).
func TestLiveKitV1ToolAnnounceSpeaksBeforeTheWork(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	webhook := agent.Tools["get_invoice"]
	webhook.Announce = "Let me pull that invoice up."
	agent.Tools["get_invoice"] = webhook
	agent.Tools["fetch_notes"] = ir.Tool{
		Description: "Fetch the caller's saved notes.",
		Input:       map[string]any{"type": "object", "properties": map[string]any{"topic": map[string]any{"type": "string"}}, "required": []any{"topic"}},
		Execution:   ir.ToolLocal, Handler: "tools/fetch_notes.py",
		HandlerSource: "def fetch_notes(topic):\n    return {\"notes\": []}\n",
		Interruption:  ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
		Announce: "One moment while I find your notes.",
	}
	def := agent.Agents["billing"]
	def.Tools = append(def.Tools, "fetch_notes")
	agent.Agents["billing"] = def

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	agentpy := artifactFile(t, artifact, "agent.py")

	for _, tc := range []struct{ method, line, work string }{
		{"get_invoice", `self.session.say("Let me pull that invoice up.")`, "async with httpx.AsyncClient()"},
		{"fetch_notes", `self.session.say("One moment while I find your notes.")`, "result = tools.fetch_notes.fetch_notes("},
	} {
		body := livekitMethodBody(t, agentpy, tc.method)
		sayAt := strings.Index(body, tc.line)
		workAt := strings.Index(body, tc.work)
		if sayAt < 0 || workAt < 0 {
			t.Fatalf("%s: missing exact announcement or the work it covers:\n%s", tc.method, body)
		}
		if sayAt >= workAt {
			t.Errorf("%s: the announcement must start before the work:\n%s", tc.method, body)
		}
		if strings.Contains(body, "await self.session.say(") {
			t.Errorf("%s: the announcement must not be awaited, it would block until playout:\n%s", tc.method, body)
		}
	}

	// lookup_customer announces nothing, so its body stays speechless.
	if silent := livekitMethodBody(t, agentpy, "lookup_customer"); strings.Contains(silent, ".say(") {
		t.Errorf("a tool without announce must emit no speech:\n%s", silent)
	}
}

// livekitMethodBody returns one emitted agent method, from its def line to the
// next one, so an assertion about a tool body cannot be satisfied by a line
// somewhere else in the file.
func livekitMethodBody(t *testing.T, agentpy, method string) string {
	t.Helper()
	start := strings.Index(agentpy, "    async def "+method+"(")
	if start < 0 {
		t.Fatalf("method %s not emitted", method)
	}
	body := agentpy[start:]
	if end := strings.Index(body[1:], "\n    async def "); end >= 0 {
		body = body[:end+1]
	}
	return body
}

// A delegated task must never be asked to answer a tool call it did not make.
//
// The framework injects the parent's in-flight delegate call, plus a placeholder
// output reading "The tool call is still in progress.", into the context the task
// generates from (voice/generation.py, _inject_running_tool_calls). For the agent
// that made the call that is right: it stops the model re-issuing a call already
// running. A task is a different agent with a different prompt, and its opening
// turn reads an unfinished tool call it never made. It then answers with nothing,
// or apologises for a failure that did not happen, and the caller hears the agent
// break: 3 of 3 scripted salon calls opened with either the empty-response
// fallback or "I'm sorry, but I can't complete your booking right now"
// (B: task opened silent after delegate, 2026-08-21).
//
// The strip is keyed on the SDK's own marker rather than the placeholder wording,
// because the wording is prose and the marker is the flag the SDK added for
// exactly this. That makes the private name load-bearing, so this test names it:
// if upstream renames it, this fails here rather than in a live call.
func TestLiveKitV1TaskDropsParentInFlightCall(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	agentpy := artifactFile(t, artifact, "agent.py")

	// The strip lives in the task mixin's llm_node, so it covers the opening turn
	// and every retry, not just the construction of the context.
	mixin := "class _RetryEmptyTaskResponseMixin:"
	start := strings.Index(agentpy, mixin)
	if start < 0 {
		t.Fatal("a package with tasks emits no task mixin")
	}
	body := agentpy[start:]
	if end := strings.Index(body[len(mixin):], "\nclass "); end >= 0 {
		body = body[:len(mixin)+end]
	}
	for _, want := range []string{
		`item.extra.get("__lk_running_placeholder__")`,
		"running_placeholders = {",
		`if getattr(item, "call_id", None) not in running_placeholders`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the task mixin does not drop the parent's in-flight call: want %q", want)
		}
	}
	if !strings.Contains(body, "isinstance(item, llm.FunctionCall)") {
		t.Error("the strip must key on the flagged FunctionCall, not on the placeholder text")
	}
	// Matching the prose would break silently the next time upstream rewords it.
	if strings.Contains(body, "The tool call is still in progress") {
		t.Error("the strip matches the placeholder wording; key on the SDK marker instead")
	}

	// A package with no tasks has no mixin, so it must not carry the strip.
	bare, err := spec.Load(filepath.Join("..", "testdata", "remy"))
	if err != nil {
		t.Fatal(err)
	}
	bareAgent, err := ir.Build(bare)
	if err != nil {
		t.Fatal(err)
	}
	bareAgent.Tasks = nil
	bareAgent.TaskGroups = nil
	for name, control := range bareAgent.Controls {
		if _, isDelegate := control.(*ir.Delegate); isDelegate {
			delete(bareAgent.Controls, name)
		}
	}
	for name, held := range bareAgent.Agents {
		kept := held.Tools[:0:0]
		for _, tool := range held.Tools {
			_, isControl := bareAgent.Controls[tool]
			_, isTool := bareAgent.Tools[tool]
			if isControl || isTool {
				kept = append(kept, tool)
			}
		}
		held.Tools = kept
		bareAgent.Agents[name] = held
	}
	bareArtifact, err := Generate(bareAgent, targetByProvider(t, bareAgent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate task-free: %v", err)
	}
	got := artifactFile(t, bareArtifact, "agent.py")
	if strings.Contains(got, "_RetryEmptyTaskResponseMixin") {
		t.Fatal("a package with no tasks still emits the task mixin; the negative case proves nothing")
	}
	if strings.Contains(got, "running_placeholders") {
		t.Error("a package with no tasks emits the in-flight strip anyway")
	}
}

// TestLiveKitV1DelegateRequiresGuard covers the emitted guard on a returning
// step: it runs before the step starts, refuses while a value is unset, resets
// on a successful start, speaks at the bound, logs both paths, and declares its
// requirement on the tool description so the model rarely reaches the guard.
func TestLiveKitV1DelegateRequiresGuard(t *testing.T) {
	agent := guardedFixture(t)
	py := emitFor(t, agent, ir.ProviderLiveKit, "agent.py")

	start := strings.Index(py, "    async def manage_booking(self, ctx: RunContext)")
	if start < 0 {
		t.Fatal("agent.py has no manage_booking method")
	}
	method := py[start:]
	if end := strings.Index(method[1:], "\n    @function_tool"); end >= 0 {
		method = method[:end+1]
	}

	guardAt := strings.Index(method, `_unmet = _unmet_prerequisites(ctx.userdata, ["customer_id"])`)
	workAt := strings.Index(method, "owner_ctx = self.chat_ctx.copy()")
	if guardAt < 0 || workAt < 0 || guardAt >= workAt {
		t.Fatalf("the guard must run before the step does any work:\n%s", method)
	}

	for _, want := range []string{
		`_tries = _prerequisite_refusals.get("manage_booking", 0) + 1`,
		"_at_limit = _tries >= _PREREQUISITE_LIMIT",
		`return {"refused": _prerequisite_refusal(_unmet, _at_limit)}`,
		`_prerequisite_refusals["manage_booking"] = 0`,
	} {
		if !strings.Contains(method, want) {
			t.Errorf("emitted guard missing %q:\n%s", want, method)
		}
	}

	// The reset must be on the path that actually starts the step, not inside
	// the refusal branch, or the counter never advances and the bound never
	// fires.
	resetAt := strings.Index(method, `_prerequisite_refusals["manage_booking"] = 0`)
	if resetAt < guardAt || resetAt > workAt {
		t.Errorf("the counter must reset where the step starts, not in the refusal branch:\n%s", method)
	}

	// FR-003b: the requirement is declared where the model sees it before it
	// gets here.
	docEnd := strings.Index(method, `_unmet = _unmet_prerequisites`)
	doc := method[:docEnd]
	for _, want := range []string{"customer_id", "verify_customer"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the tool description must name %q so the model collects it earlier:\n%s", want, doc)
		}
	}

	assertGuardLogsNamesOnly(t, method, "livekit")
}

// assertGuardLogsNamesOnly holds FR-003f and SC-005d.
//
// The identifier in the release example is a caller's phone number. A log line
// that formats a variable's value writes real phone numbers into an operator's
// log, which is a privacy defect and not a formatting slip. Every argument to
// the guard's log calls must be a name or a step, never a lookup of a value.
func assertGuardLogsNamesOnly(t *testing.T, method, label string) {
	t.Helper()
	logged := 0
	for _, line := range strings.Split(method, "\n") {
		if !strings.Contains(line, "prerequisite guard:") {
			continue
		}
		logged++
		if !strings.Contains(line, "refused") {
			t.Errorf("%s: a refusal log line must say the step was refused: %s", label, line)
		}
	}
	if logged < 2 {
		t.Errorf("%s: both the ordinary refusal and the one that reaches the bound must be logged, found %d", label, logged)
	}

	// The only values that may reach a log argument. ctx.userdata.<name> and
	// self.state.<name> are the two ways a value could get there.
	for _, forbidden := range []string{"ctx.userdata.", "self.state.", "getattr(ctx.userdata", "getattr(self.state"} {
		for _, line := range strings.Split(method, "\n") {
			if strings.Contains(line, "logger.") && strings.Contains(line, forbidden) {
				t.Errorf("%s: a log line reads a variable's value (%q); the identifier is a phone number, so only names may be logged: %s", label, forbidden, line)
			}
		}
	}
}
