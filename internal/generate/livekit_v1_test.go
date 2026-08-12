package generate

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

var updateLiveKitV1 = flag.Bool("update-livekit", false, "rewrite the livekit v1 golden")

// TestLiveKitV1RemyGolden emits the Remy example (agent handoff + task groups +
// the SLNG plugin) to LiveKit and compares the full file set byte-for-byte
// (driver-livekit T8/T9/T10, V11/V12). Zero Python.
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
		"from livekit.agents.beta.workflows import TaskGroup",
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
	for _, want := range []string{
		"from tracing import setup_langfuse",
		`"langfuse.session.id": ctx.room.name`,
		`"langfuse.trace.name": "greeter" + "-" + "livekit"`,
		"await session.start(agent=Greeter(), room=ctx.room)",
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
	if !strings.Contains(pyproject, `"livekit-agents[cartesia,deepgram,elevenlabs,openai]>=1.5"`) {
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

func artifactHasFile(artifact Artifact, path string) bool {
	for _, file := range artifact.Files {
		if file.Path == path {
			return true
		}
	}
	return false
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
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
		}
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
	if !strings.Contains(botpy, "async def finish(self, ctx: RunContext, sent: bool) -> None:") {
		t.Error("finish must be typed -> None (complete() is the sole resolution)")
	}
	if strings.Contains(botpy, `return "Done."`) {
		t.Error(`finish must not return a value after self.complete() (stray post-completion output)`)
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
		"from livekit.agents.beta.workflows import TaskGroup",
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
}

// TestV4_LiveKitInferenceFact (parity V4/C2): the artifact flags exactly the
// bindings that route through LiveKit Inference, so console mode knows when it
// needs LiveKit creds. safe_core's default (native deepgram/elevenlabs/openai +
// local turn-detector-mini) flags nothing; the Inference wildcard reason
// (provider: livekit) and the cloud turn detector each flag.
func TestV4_LiveKitInferenceFact(t *testing.T) {
	load := func() *ir.Agent {
		t.Helper()
		pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
		if err != nil {
			t.Fatal(err)
		}
		agent, err := ir.Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		return agent
	}

	agent := load()
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate default: %v", err)
	}
	if len(artifact.LiveKitInference) != 0 {
		t.Errorf("scaffold-default livekit must not route through Inference; got %v", artifact.LiveKitInference)
	}

	agent = load()
	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
	for profile := range tgt.Models.Reason {
		tgt.Models.Reason[profile] = ir.Binding{Provider: "livekit", Model: "openai/gpt-4o-mini"}
	}
	artifact, err = Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate wildcard reason: %v", err)
	}
	if !strings.Contains(strings.Join(artifact.LiveKitInference, " "), "reason") {
		t.Errorf("provider: livekit reason must flag Inference; got %v", artifact.LiveKitInference)
	}

	agent = load()
	tgt = targetByProvider(t, agent, ir.ProviderLiveKit)
	tgt.Models.Turn = &ir.Binding{Provider: "livekit", Model: "turn-detector"}
	artifact, err = Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate cloud turn: %v", err)
	}
	if !strings.Contains(strings.Join(artifact.LiveKitInference, " "), "turn") {
		t.Errorf("cloud turn-detector must flag Inference; got %v", artifact.LiveKitInference)
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

// TestLiveKitV1BuiltinEndCallTool covers the prebuilt end_call lowering:
// the beta EndCallTool import, its construction with the mapped params in the
// agent's super().__init__(tools=...), and that it is NOT a @function_tool
// method (docs/spec/prebuilt-tools.md V7).
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
		"class FindSlot(IgnorePhrasesMixin, AgentTask[dict]):",
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
		t.Fatalf("generate cold: %v", err)
	}
	botpy := artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"async def to_human(self, ctx: RunContext) -> str:",
		"job_ctx = get_job_context()",
		"rtc.ParticipantKind.PARTICIPANT_KIND_SIP",
		// The tool speaks its own announcement (SPEC V4/B4).
		`"Putting you through now, one moment.", allow_interruptions=False`,
		// A REFER destination is a URI, not a bare number: this asserted
		// `transfer_to="+14155550123"` until 2026-08-12, which is a shape no
		// LiveKit example uses and which Plivo's Refer-To rules forbid outright.
		`transfer_to="tel:+14155550123",`,
		"await job_ctx.api.sip.transfer_sip_participant(request)",
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("cold agent.py missing %q", want)
		}
	}

	human := agent.Controls["to_human"].(*ir.HumanTransfer)
	human.Mode = ir.TransferWarm
	human.Briefing = "Say who is calling and why."
	human.RingTimeout = "30s"
	human.OnUnavailable = ir.OnUnavailableReturn
	artifact, err = Generate(agent, targetByProvider(t, agent, ir.ProviderLiveKit), target.Default())
	if err != nil {
		t.Fatalf("generate warm: %v", err)
	}
	botpy = artifactFile(t, artifact, "agent.py")
	for _, want := range []string{
		"from livekit.agents.beta.workflows import WarmTransferTask",
		`sip_call_to="+14155550123"`,
		"chat_ctx=self.chat_ctx,",
		// extra_instructions is the 1.6-series briefing surface, verified in
		// the reference checkout (SPEC C3, V10). WorkflowInstructions never
		// existed there; emitting it was an ImportError at boot (T4).
		`extra_instructions="Say who is calling and why.",`,
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
	if strings.Contains(botpy, "WorkflowInstructions") {
		t.Error("agent.py imports WorkflowInstructions, which does not exist in the pinned livekit-agents (V10)")
	}
	// V10: a warm package pins the verified beta minor series, not <2.0.
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if !strings.Contains(pyproject, ">=1.6,<1.7") {
		t.Errorf("warm pyproject.toml must pin the verified livekit-agents minor series (V10):\n%s", pyproject)
	}
	envExample := artifactFile(t, artifact, ".env.example")
	if !strings.Contains(envExample, "LIVEKIT_SIP_OUTBOUND_TRUNK") {
		t.Error(".env.example missing LIVEKIT_SIP_OUTBOUND_TRUNK for warm transfer")
	}
}

// TestLiveKitV1RequiresGuard covers V7: a transfer with requires: emits a
// machine-checked guard that refuses and names the unmet variables.
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
		`missing = [n for n, v in (("caller_phone", ctx.userdata.caller_phone), ) if v is None]`,
		`return "Cannot transfer yet; missing required information: " + ", ".join(missing)`,
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
			`sip_trunk_id=os.environ["LIVEKIT_SIP_OUTBOUND_TRUNK"],`,
			"wait_until_answered=True,",
			"result = await detector.execute()",
			tc.want,
		} {
			if !strings.Contains(botpy, want) {
				t.Errorf("%s agent.py missing %q", tc.action, want)
			}
		}
	}
}

func TestLiveKitSIPEmitsTopologyAndHydratesContextBeforeGreeting(t *testing.T) { // telephony T10, V7, V9-V10, V13, V17-V20
	agent, resolved := configuredLiveKitSIP(t)
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"sip-inbound-trunk.json", "sip-outbound-trunk.json", "sip-dispatch-rule.json"} {
		content := artifactFile(t, artifact, path)
		if !json.Valid([]byte(content)) {
			t.Errorf("%s is not valid JSON:\n%s", path, content)
		}
	}
	for path, wants := range map[string][]string{
		"sip-inbound-trunk.json":  {`${TWILIO_PHONE_NUMBER}`, `twilio inbound`},
		"sip-outbound-trunk.json": {`${TWILIO_SIP_ADDRESS}`, `${TWILIO_PHONE_NUMBER}`},
		"sip-dispatch-rule.json":  {`${LIVEKIT_SIP_INBOUND_TRUNK}`, `"agentName": "livekit"`, `\"direction\":\"inbound\"`},
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
		`await _livekit_entry_greeting(session)`,
		"result = await WarmTransferTask(",
		`sip_call_to="+14155550123",`,
	} {
		if !strings.Contains(agentPy, want) {
			t.Errorf("agent.py missing %q", want)
		}
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
	for _, name := range []string{
		"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "REDIS_URL",
		"LIVEKIT_SIP_INBOUND_TRUNK", "LIVEKIT_SIP_OUTBOUND_TRUNK",
		"TWILIO_SIP_ADDRESS", "TWILIO_SIP_USERNAME", "TWILIO_SIP_PASSWORD", "TWILIO_PHONE_NUMBER",
	} {
		if !strings.Contains(env, name+"=") {
			t.Errorf(".env.example missing %s", name)
		}
	}
	readme := artifactFile(t, artifact, "README.md")
	for _, want := range []string{
		"Configure self-hosted LiveKit SIP", "SIP trunking console", "twilio provider guide", "REDIS_URL", "not an audio hop",
		`--auth-user "$TWILIO_SIP_USERNAME"`, `--auth-pass "$TWILIO_SIP_PASSWORD"`,
		"UNMUTE_LIVEKIT_SIP_PORT", "UNMUTE_LIVEKIT_RTP_PORT_RANGE", "UNMUTE_LIVEKIT_PORT",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md missing %q", want)
		}
	}
	compose := artifactFile(t, artifact, "compose.telephony.yaml")
	assertValidYAML(t, compose)
	assertComposeLocalEnvironment(t, compose, TelephonyRuntimePlanFor(resolved))
	assertGoldenFile(t, filepath.Join("testdata", "golden", "livekit_v1_telephony_compose.yaml"), compose, *updateLiveKitV1)
	for _, want := range []string{
		"image: valkey/valkey:9.1.1-alpine", "image: livekit/livekit-server:v1.13.4", "image: livekit/sip:v1.7.0",
		"LIVEKIT_API_SECRET=secret", "address: redis:6379",
		`stop_grace_period: "1200s"`, `"${UNMUTE_LIVEKIT_PORT:-7880}:7880"`,
		`"${UNMUTE_LIVEKIT_SIP_PORT:-5060}:5060/udp"`,
		`rtp_port: ${UNMUTE_LIVEKIT_RTP_PORT_RANGE:-10000-10100}`,
		`"${UNMUTE_LIVEKIT_RTP_PORT_RANGE:-10000-10100}:${UNMUTE_LIVEKIT_RTP_PORT_RANGE:-10000-10100}/udp"`,
		"condition: service_healthy", "redis_data:/data",
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("compose.telephony.yaml missing %q:\n%s", want, compose)
		}
	}
	for _, forbidden := range []string{"image: livekit/livekit-server:latest", "image: livekit/sip:latest", "secret-value", "TWILIO_SIP_PASSWORD=", "REDIS_URL=redis"} {
		if strings.Contains(compose, forbidden) {
			t.Errorf("compose.telephony.yaml contains %q", forbidden)
		}
	}
	// V10/B2: the local stack must use the documented `livekit-server --dev`
	// pair. A secret the dev server will not accept 401s worker registration
	// and every SIP Twirp admin call.
	if strings.Contains(compose, "devsecret-local-only") {
		t.Error("compose.telephony.yaml uses a secret livekit-server --dev does not accept")
	}
	if !strings.Contains(compose, "LIVEKIT_API_SECRET=secret") || !strings.Contains(compose, "api_secret: secret") {
		t.Errorf("compose.telephony.yaml must sign the app and SIP with the --dev secret:\n%s", compose)
	}

	runtime := TelephonyRuntimePlanFor(resolved)
	if runtime.Coordination != "shared" || runtime.AdmissionOwner != "livekit_dispatch" {
		t.Fatalf("runtime coordination = %#v", runtime)
	}
	if len(runtime.Processes) != 1 || runtime.Processes[0].Readiness != "/" {
		t.Fatalf("runtime process = %#v", runtime.Processes)
	}
	required := strings.Join(runtime.RequiredEnv, ",")
	for _, want := range []string{"REDIS_URL", "LIVEKIT_SIP_INBOUND_TRUNK", "LIVEKIT_SIP_OUTBOUND_TRUNK", "TWILIO_SIP_PASSWORD"} {
		if !strings.Contains(required, want) {
			t.Errorf("runtime required env missing %s: %s", want, required)
		}
	}
	if strings.Contains(required, "LIVEKIT_SIP_URI") {
		t.Errorf("runtime still requires the unused LIVEKIT_SIP_URI: %s", required)
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
	enablePackageTelephony(pkg)
	configured := pkg.Targets["livekit"]
	configured.Transport, configured.Carrier, configured.Connection = "sip", "twilio", "primary_phone"
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

func configuredLiveKitConnector(t *testing.T) (*ir.Agent, ir.Target) {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	// The connector supports no transfers yet, so drop the human transfer
	// control and every reference to it. primary_phone already carries the
	// Twilio account trio (account_sid, auth_token, from_number).
	delete(pkg.Agent.Controls, "to_human")
	for name, a := range pkg.Agent.Agents {
		a.Tools = slices.DeleteFunc(a.Tools, func(s string) bool { return s == "to_human" })
		pkg.Agent.Agents[name] = a
	}
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
	configured.Transport, configured.Carrier, configured.Connection = "connector", "twilio", "primary_phone"
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

	compose := artifactFile(t, artifact, "compose.telephony.yaml")
	for _, want := range []string{"livekit_server:", "LIVEKIT_API_SECRET=secret", "python telephony_bridge.py"} {
		if !strings.Contains(compose, want) {
			t.Errorf("connector compose missing %q:\n%s", want, compose)
		}
	}
	for _, forbidden := range []string{"redis", "livekit_sip", "livekit/sip:", "devsecret-local-only"} {
		if strings.Contains(compose, forbidden) {
			t.Errorf("connector compose contains %q", forbidden)
		}
	}

	env := artifactFile(t, artifact, ".env.example")
	for _, want := range []string{"TWILIO_ACCOUNT_SID=", "TWILIO_AUTH_TOKEN=", "TWILIO_PHONE_NUMBER=", "UNMUTE_PUBLIC_URL=", "UNMUTE_OUTBOUND_TOKEN="} {
		if !strings.Contains(env, want) {
			t.Errorf(".env.example missing %q", want)
		}
	}
	for _, forbidden := range []string{"TWILIO_SIP_", "LIVEKIT_SIP_INBOUND_TRUNK", "REDIS_URL"} {
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

// TestLiveKitV1LocalAndMCPTools covers the tool executions beyond webhook:
// local copies the package handler into tools/<name>.py and wraps it (SCHEMA
// §5, code targets); mcp mounts MCPServerHTTP off url_env with allowed_tools
// (B3/D8). The local handler rides spec.Load like instructions do.
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
		Description: "Book the table through the bookings MCP server.",
		Input:       map[string]any{"type": "object"},
		Execution:   ir.ToolMCP, URLEnv: "BOOKINGS_MCP_URL",
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
		`mcp_servers=[mcp.MCPServerHTTP(url=os.environ["BOOKINGS_MCP_URL"], allowed_tools=["book_table"])],`,
	} {
		if !strings.Contains(botpy, want) {
			t.Errorf("agent.py missing %q", want)
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
	agent.Controls["to_human"] = &ir.HumanTransfer{Kind: ir.ControlHumanTransfer, Destination: "line", Mode: ir.TransferWarm, Briefing: "Say who is calling and why.", OnUnavailable: ir.OnUnavailableReturn}
	agent.Controls["to_human_cold"] = &ir.HumanTransfer{Kind: ir.ControlHumanTransfer, Destination: "line", Mode: ir.TransferCold, RingTimeout: "20s", OnUnavailable: ir.OnUnavailableHangup}
	agent.Tools["fetch_notes"] = ir.Tool{
		Description: "Fetch the caller's saved notes.",
		Input:       map[string]any{"type": "object", "properties": map[string]any{"topic": map[string]any{"type": "string"}}},
		Execution:   ir.ToolLocal, Handler: "tools/fetch_notes.py", HandlerSource: "def fetch_notes(topic):\n    return {}\n",
		Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
	}
	agent.Tools["book_table"] = ir.Tool{
		Description: "Book through the bookings MCP server.", Input: map[string]any{"type": "object"},
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
	agent.Tasks["find_slot"] = task
	agent.Controls["do_find"] = &ir.Delegate{Kind: ir.ControlDelegate, Task: "find_slot", Assign: map[string]string{"caller_phone": "result.date"}}
	resDef := agent.Agents["reservations"]
	resDef.Tools = append(resDef.Tools, "do_find")
	agent.Agents["reservations"] = resDef

	tgt := targetByProvider(t, agent, ir.ProviderLiveKit)
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

// TestCheckLiveKitVersion pins the template-compatible range (>=1.5, <2.0):
// beta.workflows TaskGroup + AgentTask + inference are present from 1.5.x.
func TestCheckLiveKitVersion(t *testing.T) {
	for _, tc := range []struct {
		version string
		ok      bool
	}{
		{"1.5.2", true},
		{"1.5", true},
		{"1.6.0", true},
		{"1.2", false},
		{"1.4.9", false},
		{"0.0.108", false},
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
	svc, err := livekitChainService(ir.Binding{Provider: "openai", Model: "gpt-4.1-mini"}, env)
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

	svc, err = livekitChainService(ir.Binding{Provider: "livekit", Model: "openai/gpt-4o-mini"}, newEnvSet())
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
