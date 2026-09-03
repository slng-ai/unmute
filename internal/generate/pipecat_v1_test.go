package generate

import (
	"bytes"
	"flag"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

var updatePipecatV1 = flag.Bool("update-pipecat", false, "rewrite the pipecat v1 golden")

func TestPipecatV1LoggingIsConfiguredAtFirstBot(t *testing.T) {
	bot := artifactFile(t, exampleArtifact(t, "simple-prompt", ir.ProviderPipecat), "bot.py")
	for _, want := range []string{
		"import sys",
		"from loguru import logger",
		`logger.add(sys.stderr, level=os.getenv("UNMUTE_LOG_LEVEL", "INFO").upper())`,
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing logging structure %q", want)
		}
	}
	if got := strings.Count(bot, "logger.remove()"); got != 1 {
		t.Errorf("bot.py removes Loguru sinks %d times, want one guarded call", got)
	}
	if !strings.Contains(bot, "_LOGGING_CONFIGURED = False\n\n\ndef _configure_logging() -> None:\n    global _LOGGING_CONFIGURED\n    if _LOGGING_CONFIGURED:\n        return") {
		t.Error("bot.py does not guard process-wide logging configuration")
	}
	const entry = "async def bot(runner_args: RunnerArguments) -> None:"
	entryAt := strings.Index(bot, entry)
	if entryAt < 0 || !strings.HasPrefix(strings.TrimSpace(bot[entryAt+len(entry):]), "_configure_logging()") {
		t.Error("bot.py does not configure logging at the first bot entry")
	}
}

// TestPipecatV1BuiltinEndCallTool covers the prebuilt end_call lowering: a
// bodyless @tool that speaks the goodbye then ends via EndFrame, with no
// url_env, handler, or httpx POST.
func TestPipecatV1BuiltinEndCallTool(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
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
	def := agent.Agents[agent.EntryAgent]
	def.Tools = append(def.Tools, "end_call")
	agent.Agents[agent.EntryAgent] = def

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"async def end_call(self, params: FunctionCallParams):",
		`"content": "Thank the caller and say goodbye."`,
		"await params.llm.push_frame(EndFrame())",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q", want)
		}
	}
	if strings.Contains(bot, `os.environ[""]`) {
		t.Error("builtin tool must not emit a webhook POST")
	}
}

// mcpPipecatAgent puts one mcp tool source on the safe core's entry agent, so
// each case below changes only the block's own fields (N40).
func mcpPipecatAgent(t *testing.T, tool ir.Tool) *ir.Agent {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	tool.Execution = ir.ToolMCP
	tool.Interruption, tool.Effect = ir.ToolProviderDefault, ir.ToolReturnsData
	agent.Tools["web_search"] = tool
	def := agent.Agents[agent.EntryAgent]
	def.Tools = append(def.Tools, "web_search")
	agent.Agents[agent.EntryAgent] = def
	return agent
}

// TestPipecatV1MCPToolSource is the Pipecat side of N40: one MCPClient per
// source, connected before the agent is activated, its tools advertised only
// while that agent is active, and closed on shutdown. The gate this driver
// carried until now is what made the emission necessary rather than optional.
func TestPipecatV1MCPToolSource(t *testing.T) {
	// The compiler never reads a secret's value, so a value in the environment
	// must reach no emitted byte (SC-005).
	const secret = "fc-live-pretend-key"
	t.Setenv("FIRECRAWL_API_KEY", secret)
	agent := mcpPipecatAgent(t, ir.Tool{
		URLEnv: "FIRECRAWL_MCP_URL", MCPTransport: ir.MCPTransportStreamableHTTP,
		MCPTools: []string{"firecrawl_search"},
		Auth:     &ir.ToolAuth{Type: ir.ToolAuthBearer, TokenEnv: "FIRECRAWL_API_KEY"},
	})
	// A second source at the same address with its own selection: two clients,
	// not one merged mount (the spec's same-address edge case).
	agent.Tools["web_crawl"] = ir.Tool{
		Execution: ir.ToolMCP, URLEnv: "FIRECRAWL_MCP_URL",
		MCPTransport: ir.MCPTransportStreamableHTTP, MCPTools: []string{"firecrawl_scrape"},
		Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
	}
	entry := agent.Agents[agent.EntryAgent]
	entry.Tools = append(entry.Tools, "web_crawl")
	agent.Agents[agent.EntryAgent] = entry
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"from mcp.client.session_group import StreamableHttpParameters",
		"from pipecat.services.mcp_service import MCPClient",
		"self._mcp_clients = [",
		`server_params=StreamableHttpParameters(url=os.environ["FIRECRAWL_MCP_URL"], headers=_bearer("FIRECRAWL_API_KEY"), timeout=30),`,
		`tools_filter=["firecrawl_search"],`,
		"await client.start()",
		"tools = await client.get_tools_schema()",
		"await client.register_tools_schema(tools, llm)",
		"return super().build_tools() + self._mcp_tools",
		"await agent.start_mcp()",
		"(agent.close_mcp() for agent in mcp_agents)",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q:\n%s", want, bot)
		}
	}
	// The gate used to be the only thing keeping an mcp tool out of the webhook
	// lowering, so the server address may appear in exactly two places: the
	// client's own parameters and the startup check. Anywhere else means the
	// address became a request the driver builds itself.
	for _, line := range strings.Split(bot, "\n") {
		if !strings.Contains(line, "FIRECRAWL_MCP_URL") {
			continue
		}
		if strings.Contains(line, "server_params=") || strings.TrimSpace(line) == `"FIRECRAWL_MCP_URL",` {
			continue
		}
		t.Errorf("the MCP server address is read outside its client: %q", strings.TrimSpace(line))
	}
	// A stated transport wins, so the other parameter class is never imported.
	if strings.Contains(bot, "SseServerParameters") {
		t.Error("bot.py imports a parameter class it never constructs")
	}
	// The extra is what makes the import work at all (research R4).
	if pyproject := artifactFile(t, artifact, "pyproject.toml"); !strings.Contains(pyproject, "mcp,") {
		t.Errorf("pyproject must carry the mcp extra:\n%s", pyproject)
	}
	// Both env names are named before anything dials (FR-009).
	for _, env := range []string{"FIRECRAWL_MCP_URL", "FIRECRAWL_API_KEY"} {
		for _, file := range []string{".env.example", "bot.py"} {
			if !strings.Contains(artifactFile(t, artifact, file), env) {
				t.Errorf("%s missing %s", file, env)
			}
		}
	}
	for _, file := range artifact.Files {
		if strings.Contains(string(file.Content), secret) {
			t.Errorf("%s carries a secret value", file.Path)
		}
	}
	// Two sources, one address: each keeps its own selection.
	if strings.Count(bot, "MCPClient(") != 2 {
		t.Errorf("two sources at one address must build two clients:\n%s", bot)
	}
	if !strings.Contains(bot, `tools_filter=["firecrawl_scrape"],`) {
		t.Errorf("the second source lost its own selection:\n%s", bot)
	}
}

// TestPipecatV1MCPTransportChooser covers the source that states no transport:
// the bot picks the parameter class from the URL at startup, with the same rule
// livekit-agents auto-detects by (research R5).
func TestPipecatV1MCPTransportChooser(t *testing.T) {
	agent := mcpPipecatAgent(t, ir.Tool{URLEnv: "NOTES_MCP_URL"})
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"from mcp.client.session_group import SseServerParameters, StreamableHttpParameters",
		"def _mcp_params(url: str, headers: dict[str, str] | None = None):",
		`params_cls = StreamableHttpParameters if url.rstrip("/").endswith("/mcp") else SseServerParameters`,
		`server_params=_mcp_params(os.environ["NOTES_MCP_URL"]),`,
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q:\n%s", want, bot)
		}
	}
	// No selection and no auth means neither argument is written at all: an
	// empty filter would be a claim the author never made (SC-004).
	if strings.Contains(bot, "tools_filter") || strings.Contains(bot, "_bearer(") || strings.Contains(bot, "_api_key(") {
		t.Errorf("an unfiltered, unauthenticated source must emit neither argument:\n%s", bot)
	}
}

// TestPipecatV1WebhookAuth covers both schemes (SCHEMA §5.3): the @tool POST
// reads the right helper and the token env joins .env.example and REQUIRED_ENV
// by name.
func TestPipecatV1WebhookAuth(t *testing.T) {
	for _, fixture := range authFixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			agent := authAgent(t, fixture.Auth)
			artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			bot := artifactFile(t, artifact, "bot.py")
			for _, want := range []string{fixture.CallSite, fixture.Helper} {
				if !strings.Contains(bot, want) {
					t.Errorf("bot.py missing %q:\n%s", want, bot)
				}
			}
			if strings.Count(bot, "headers=") != 1 {
				t.Errorf("exactly one tool must send headers:\n%s", bot)
			}
			for _, file := range []string{".env.example", "bot.py"} {
				// bot.py carries REQUIRED_ENV, so a missing secret fails at startup.
				if !strings.Contains(artifactFile(t, artifact, file), fixture.Env) {
					t.Errorf("%s missing %s", file, fixture.Env)
				}
			}
		})
	}
}

// TestPipecatV1NoAuthHelpersWithoutAuth keeps helpers and imports conditional
// (V8, driver-pipecat V12) — safe_core declares no auth.
func TestPipecatV1NoAuthHelpersWithoutAuth(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, unwanted := range []string{"_bearer", "_api_key"} {
		if strings.Contains(bot, unwanted) {
			t.Errorf("bot.py emits %q with no auth tool", unwanted)
		}
	}
}

// TestPipecatV1Golden emits the safe_core project to pipecat and compares the
// full file set byte-for-byte (driver-pipecat T8, V10). Zero Python.
func TestPipecatV1Golden(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
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

	path := filepath.Join("testdata", "golden", "pipecat_v1.txt")
	if *updatePipecatV1 {
		if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Fatalf("pipecat v1 golden differs; run: go test ./internal/generate -run TestPipecatV1Golden -update-pipecat")
	}
}

// TestPipecatGreetingModes covers the three SCHEMA.md 4.8 combinations plus the
// no-block default. Fixed text bypasses the LLM; an omitted text still asks the
// model; user-first stays silent; an absent block takes the model-written
// opening, same as LiveKit (SPEC V1, V4, V5, V6).
func TestV32PipecatGreetingModes(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	tgt := targetByProvider(t, agent, ir.ProviderPipecat)

	for _, tc := range []struct {
		name      string
		greeting  *ir.Greeting
		want      []string
		forbidden []string
	}{
		{
			name: "fixed text",
			greeting: &ir.Greeting{
				SpeaksFirst: ir.SpeaksFirstAgent,
				Text:        "Hi, this is Sage and Stone Salon.",
			},
			want: []string{
				"from pipecat.frames.frames import EndFrame, LLMMessagesAppendFrame, TTSSpeakFrame",
				`TTSSpeakFrame("Hi, this is Sage and Stone Salon.")`,
				"next(agent for agent in agents",
				"args=LLMWorkerActivationArgs(run_llm=False)",
			},
			forbidden: []string{"Begin the conversation by saying, word for word:", "agent in AGENTS"},
		},
		{
			name:     "model written",
			greeting: &ir.Greeting{SpeaksFirst: ir.SpeaksFirstAgent},
			want: []string{
				`"content": "Greet the caller and offer to help."`,
				"run_llm=True",
			},
			forbidden: []string{"TTSSpeakFrame"},
		},
		{
			name:     "user first",
			greeting: &ir.Greeting{SpeaksFirst: ir.SpeaksFirstUser},
			want:     []string{"args=LLMWorkerActivationArgs(run_llm=False)"},
			forbidden: []string{
				"TTSSpeakFrame",
				"Greet the caller and offer to help.",
			},
		},
		{
			// No greeting block is not silence: it takes the model-written
			// opening, the same default the LiveKit driver picks.
			name:     "no greeting block",
			greeting: nil,
			want: []string{
				`"content": "Greet the caller and offer to help."`,
				"run_llm=True",
			},
			forbidden: []string{"TTSSpeakFrame"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent.Conversation.Greeting = tc.greeting
			artifact, err := Generate(agent, tgt, target.Default())
			if err != nil {
				t.Fatal(err)
			}
			bot := artifactFile(t, artifact, "bot.py")
			for _, want := range tc.want {
				if !strings.Contains(bot, want) {
					t.Errorf("bot.py missing %q", want)
				}
			}
			for _, forbidden := range tc.forbidden {
				if strings.Contains(bot, forbidden) {
					t.Errorf("bot.py contains %q", forbidden)
				}
			}
		})
	}
}

func TestV16PipecatRequestTracingWiring(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	enableLangfuse(agent)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatal(err)
	}

	bot := artifactFile(t, artifact, "bot.py")
	tracing := artifactFile(t, artifact, "tracing.py")
	for _, want := range []string{
		"from tracing import (",
		"trace_provider = setup_langfuse_tracing()",
		`trace_attributes = {"langfuse.trace.name": TRACE_NAME}`,
		"if runner_args.session_id is not None:",
		`trace_attributes["langfuse.session.id"] = runner_args.session_id`,
		"conversation_id=runner_args.session_id",
		"enable_tracing=True",
		"additional_span_attributes=trace_attributes",
		"enable_agent_tracing(main, agents)",
		"await asyncio.to_thread(flush_tracing, trace_provider)",
		"primary_error = sys.exception()",
		"Tracing flush failed while preserving the primary error ({})",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q", want)
		}
	}
	for _, want := range []string{
		"_TRACE_PROVIDER: TracerProvider | None = None",
		"def setup_langfuse_tracing() -> TracerProvider:",
		"if _TRACE_PROVIDER is not None:",
		"existing_provider = trace.get_tracer_provider()",
		"if isinstance(existing_provider, TracerProvider):",
		"OpenTelemetry already has a TracerProvider",
		`f"{base_url.rstrip('/')}/api/public/otel"`,
		"setup_tracing(service_name=TRACE_NAME, exporter=OTLPSpanExporter())",
		"def enable_agent_tracing(main: PipelineWorker, agents: Sequence[LLMWorker]) -> None:",
		"agent._tracing_context = main._tracing_context",
		"def flush_tracing(provider: TracerProvider) -> None:",
		"provider.force_flush()",
	} {
		if !strings.Contains(tracing, want) {
			t.Errorf("tracing.py missing %q", want)
		}
	}
	if !strings.Contains(tracing, "if not public_key or not secret_key or not base_url:") {
		t.Error("configured tracing must reject missing credentials, including all three")
	}
	guardAt := strings.Index(tracing, "if isinstance(existing_provider, TracerProvider):")
	setupAt := strings.Index(tracing, "setup_tracing(service_name=TRACE_NAME")
	if guardAt < 0 || setupAt < 0 || guardAt > setupAt {
		t.Error("preinstalled OpenTelemetry provider must fail before Pipecat setup")
	}
	if strings.Contains(bot, "tracing_enabled") {
		t.Error("configured tracing must not keep an impossible disabled branch")
	}
	if strings.Contains(bot, "\n        flush_tracing(trace_provider)\n") {
		t.Error("provider flush must not block Pipecat's event loop")
	}
	addAt := strings.Index(bot, "await runner.add_workers(main)")
	tryAt := -1
	if addAt >= 0 {
		tryAt = strings.LastIndex(bot[:addAt], "\n    try:\n")
	}
	if tryAt < 0 || addAt < tryAt {
		t.Error("traced main registration must share tracing cleanup ownership")
	}

	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if !strings.Contains(pyproject, ",tracing,") {
		t.Error("pyproject.toml missing the official pipecat-ai tracing extra")
	}
	if !strings.Contains(pyproject, `"opentelemetry-exporter-otlp-proto-http>=1.33,<2"`) {
		t.Error("pyproject.toml missing the OpenTelemetry HTTP exporter")
	}
	env := artifactFile(t, artifact, ".env.example")
	for _, name := range []string{"LANGFUSE_SECRET_KEY=", "LANGFUSE_PUBLIC_KEY=", "LANGFUSE_BASE_URL="} {
		if !strings.Contains(env, name) {
			t.Errorf(".env.example missing %s", name)
		}
	}
}

func TestV31PipecatTracingIsIsolated(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	enableLangfuse(agent)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatal(err)
	}

	bot := artifactFile(t, artifact, "bot.py")
	if !strings.Contains(bot, "from tracing import (") {
		t.Fatal("bot.py missing tracing import")
	}
	for _, forbidden := range []string{"def setup_langfuse_tracing", "def _patch_pipecat_tracing", "class TracedLLMWorker"} {
		if strings.Contains(bot, forbidden) {
			t.Errorf("bot.py contains tracing implementation %q", forbidden)
		}
	}
	_ = artifactFile(t, artifact, "tracing.py")
}

func TestV21PipecatUsesNativeTracing(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	enableLangfuse(agent)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatal(err)
	}

	tracing := artifactFile(t, artifact, "tracing.py")
	for _, forbidden := range []string{
		"SpanProcessor",
		"LangfuseAttributeProcessor",
	} {
		if strings.Contains(tracing, forbidden) {
			t.Errorf("tracing.py contains custom tracing hook %q", forbidden)
		}
	}
}

func TestV23PipecatSpeechObservationsAreRich(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	enableLangfuse(agent)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatal(err)
	}

	bot := artifactFile(t, artifact, "bot.py")
	tracing := artifactFile(t, artifact, "tracing.py")
	for _, want := range []string{
		"def _patch_pipecat_tracing() -> None:",
		"service_decorators.add_stt_span_attributes",
		"service_decorators.add_tts_span_attributes",
		`"langfuse.observation.input"`,
		`"langfuse.observation.output"`,
		`"langfuse.trace.input"`,
		`"langfuse.trace.output"`,
		`"langfuse.observation.completion_start_time"`,
		`"langfuse.observation.usage_details"`,
		`"langfuse.observation.metadata.ttfb_seconds"`,
		`"langfuse.observation.metadata.character_count"`,
		"_patch_pipecat_tracing()",
	} {
		if !strings.Contains(tracing, want) {
			t.Errorf("tracing.py missing %q", want)
		}
	}
	for _, forbidden := range []string{"TTSAudioRawFrame", "TTSService", "append_to_audio_context", "Pipecat 1.5"} {
		if strings.Contains(tracing, forbidden) {
			t.Errorf("tracing.py contains obsolete Pipecat tracing workaround %q", forbidden)
		}
	}
	if !strings.Contains(bot, "PipelineParams(enable_metrics=True, enable_usage_metrics=True)") {
		t.Error("bot.py missing tracing metrics configuration")
	}
}

func TestV24PipecatStaticCheckSurface(t *testing.T) {
	pkg, err := spec.Load(examplePackagePath("simple-prompt"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	tgt := targetByProvider(t, agent, ir.ProviderPipecat)
	if tgt.Models.Listen != nil {
		// Exercise the per-model language path (N16): the slng STT slot emits a
		// language kwarg, which resolvePipecatService wraps in the Language(...)
		// enum. Top-level language no longer defaults, so set it here explicitly.
		tgt.Models.Listen.Language = "en"
	}
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}

	bot := artifactFile(t, artifact, "bot.py")
	tracing := artifactFile(t, artifact, "tracing.py")
	for _, want := range []string{
		"from tracing import (",
		"from pipecat.transcriptions.language import Language",
		`Language("en")`,
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing static-check-safe form %q", want)
		}
	}
	for _, want := range []string{
		"from collections.abc import Sequence",
		"from opentelemetry.sdk.trace import TracerProvider",
		"_TRACE_PROVIDER: TracerProvider | None = None",
		`setattr(patched, "__langfuse_patch__", True)`,
		`setattr(service_decorators, "add_llm_span_attributes", patched_llm)`,
		"if not public_key or not secret_key or not base_url:",
		"def setup_langfuse_tracing() -> TracerProvider:",
		"global _TRACE_PROVIDER",
		"if _TRACE_PROVIDER is not None:",
		"if not isinstance(provider, TracerProvider):",
		"def enable_agent_tracing(main: PipelineWorker, agents: Sequence[LLMWorker]) -> None:",
		"context = self._tracing_context",
		"if not self._enable_tracing or context is None:",
		"def flush_tracing(provider: TracerProvider) -> None:",
	} {
		if !strings.Contains(tracing, want) {
			t.Errorf("tracing.py missing static-check-safe form %q", want)
		}
	}
	for _, forbidden := range []string{
		"patched.__langfuse_patch__",
		"append_to_audio_context.__langfuse_patch__",
		"TTSService.append_to_audio_context =",
		"trace.get_tracer_provider().force_flush()",
		"TTSAudioRawFrame",
		"patched_append_to_audio_context",
	} {
		if strings.Contains(tracing, forbidden) {
			t.Errorf("tracing.py contains ty-unsafe form %q", forbidden)
		}
	}
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	for _, want := range []string{"[dependency-groups]", `"ty"`} {
		if !strings.Contains(pyproject, want) {
			t.Errorf("pyproject.toml missing %q", want)
		}
	}
}

func TestV25PipecatTracesConfiguredSystemInstruction(t *testing.T) {
	pkg, err := spec.Load(examplePackagePath("simple-prompt"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatal(err)
	}

	tracing := artifactFile(t, artifact, "tracing.py")
	for _, want := range []string{
		"service_decorators.add_llm_span_attributes",
		`kwargs.get("system_instructions")`,
		`{"role": "system", "content": system_instruction}`,
		`span.set_attribute("input", encoded)`,
		`span.set_attribute("langfuse.observation.input", encoded)`,
	} {
		if !strings.Contains(tracing, want) {
			t.Errorf("tracing.py missing system-instruction tracing form %q", want)
		}
	}
}

func TestV22PipecatToolCallsAreTraced(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	enableLangfuse(agent)
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatal(err)
	}

	bot := artifactFile(t, artifact, "bot.py")
	tracing := artifactFile(t, artifact, "tracing.py")
	for _, want := range []string{
		"class TracedLLMWorker(LLMWorker):",
		"start_as_current_span(",
		`f"tool:{name}", context=parent`,
		`"langfuse.observation.input"`,
		`"langfuse.observation.output"`,
		`"tool.function_name"`,
		`"tool.call_id"`,
	} {
		if !strings.Contains(tracing, want) {
			t.Errorf("tracing.py missing %q", want)
		}
	}
	if !strings.Contains(bot, "class IntakeAgent(TracedLLMWorker):") {
		t.Error("bot.py missing traced agent base")
	}
}

func TestV22PipecatMCPToolCallsAreTraced(t *testing.T) {
	pkg, err := spec.Load(examplePackagePath("simple-prompt"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Tools = map[string]ir.Tool{
		"web_search": {
			Execution: ir.ToolMCP, URLEnv: "FIRECRAWL_MCP_URL",
			MCPTransport: ir.MCPTransportStreamableHTTP, MCPTools: []string{"firecrawl_search"},
			Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
		},
	}
	entry := agent.Agents[agent.EntryAgent]
	entry.Tools = []string{"web_search"}
	agent.Agents[agent.EntryAgent] = entry

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatal(err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	tracing := artifactFile(t, artifact, "tracing.py")
	for _, want := range []string{
		"class TracedLLMWorker(LLMWorker):",
		"import functools",
	} {
		if !strings.Contains(tracing, want) {
			t.Errorf("tracing.py missing MCP-only tracing form %q", want)
		}
	}
	for _, want := range []string{
		"    TracedLLMWorker,",
		"class AppointmentDeskAgent(TracedLLMWorker):",
		"tools = await client.get_tools_schema()",
		"await client.register_tools_schema(tools, llm)",
		"llm.register_function(",
		"trace_handler(client._tool_wrapper)",
		"self._mcp_tools = await _register_mcp_tools(",
		"self._track_tool_call,",
		"primary_error = sys.exception()",
		"if primary_error is not None:",
		"except BaseException as cleanup_error:",
		"Tracing flush failed while preserving the primary error ({})",
		"suppress=True",
		"suppress=sys.exception() is not None",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing traced MCP form %q:\n%s", want, bot)
		}
	}
	discoverAt := strings.Index(bot, "tools = await client.get_tools_schema()")
	registerAt := strings.Index(bot, "await client.register_tools_schema(tools, llm)")
	traceAt := strings.Index(bot, "trace_handler(client._tool_wrapper)")
	if discoverAt < 0 || registerAt < discoverAt || traceAt < registerAt {
		t.Error("MCP discovery, traced re-registration, and advertisement must stay ordered")
	}
}

func TestPipecatV1MCPLifecycleAndCollisionsFailClosed(t *testing.T) {
	agent := mcpPipecatAgent(t, ir.Tool{
		URLEnv: "FIRECRAWL_MCP_URL", MCPTools: []string{"firecrawl_search"},
	})
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatal(err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"async def _close_mcp(awaitables, *, suppress: bool = False) -> None:",
		"for awaitable in awaitables:",
		"await awaitable",
		"except BaseException as failure:",
		"async def _register_mcp_tools(clients, llm, reserved_names",
		"tools = await client.get_tools_schema()",
		"if schema.name in names:",
		"MCP tool name collision",
		"await client.register_tools_schema(tools, llm)",
		"{tool.__name__ for tool in super().build_tools()}",
		"import sys",
		"for agent in mcp_agents:",
		"await runner.add_workers(main)",
		"await runner.add_workers(*agents)",
		"suppress=sys.exception() is not None",
		"(agent.close_mcp() for agent in mcp_agents)",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing MCP transaction form %q:\n%s", want, bot)
		}
	}
	discoveryAt := strings.Index(bot, "tools = await client.get_tools_schema()")
	collisionAt := strings.Index(bot, "if schema.name in names:")
	registrationAt := strings.Index(bot, "await client.register_tools_schema(tools, llm)")
	if discoveryAt < 0 || collisionAt < discoveryAt || registrationAt < collisionAt {
		t.Error("all MCP names must be discovered and checked before any registration")
	}
	constructAt := strings.Index(bot, "main = PipelineWorker(")
	startAt := strings.Index(bot, "for agent in mcp_agents:")
	tryAt := -1
	if startAt >= 0 {
		tryAt = strings.LastIndex(bot[:startAt], "\n    try:\n")
	}
	addAt := strings.Index(bot, "await runner.add_workers(main)")
	runAt := strings.Index(bot, "await runner.run()")
	closeAt := strings.LastIndex(bot, "(agent.close_mcp() for agent in mcp_agents)")
	if constructAt < 0 || tryAt < constructAt || startAt < tryAt || addAt < startAt || runAt < addAt || closeAt < runAt {
		t.Error("MCP start, worker registration, and run must share cleanup ownership after construction")
	}
	if strings.Contains(bot, "await runner.add_workers(main, *agents)") {
		t.Error("MCP specialists must not be registered before the main pipeline starts")
	}
}

func TestPipecatV1MCPReservesFlowFunctionNames(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	addPipecatTaskTransferFixture(agent)
	verify := agent.Tasks["verify"]
	verify.Tools = append(verify.Tools, "get_invoice")
	agent.Tasks["verify"] = verify
	agent.Tools["web_search"] = ir.Tool{
		Execution: ir.ToolMCP, URLEnv: "FIRECRAWL_MCP_URL",
		MCPTransport: ir.MCPTransportStreamableHTTP, MCPTools: []string{"firecrawl_search"},
		Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
	}
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "web_search")
	agent.Agents["intake"] = intake

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatal(err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		`{tool.__name__ for tool in super().build_tools()} | {`,
		`"finish_run_verify_complete",`,
		`"finish_run_verify_verify",`,
		`"get_invoice",`,
		`"to_billing",`,
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py does not reserve Flow function %q against MCP collisions:\n%s", want, bot)
		}
	}
	initAt := strings.Index(bot, "self._mcp_clients = [")
	activationAt := strings.Index(bot, "async def on_activated(self, args) -> None:")
	if initAt < 0 || activationAt < 0 || initAt > activationAt {
		t.Error("a Flow-owning MCP worker must construct its clients before on_activated")
	}
}

func addPipecatTaskTransferFixture(agent *ir.Agent) {
	agent.Tasks["verify"] = ir.Task{
		Instructions: "Verify the caller, unless they need billing help.",
		Tools:        []string{"to_billing"},
		Result: map[string]ir.ResultField{
			"verified": {Type: ir.PrimitiveBoolean},
			"label":    {Type: ir.PrimitiveString},
		},
		Context: ir.TaskContext{History: ir.HistoryFull},
	}
	agent.Tasks["complete"] = ir.Task{
		Instructions: "Complete verification.",
		Result:       map[string]ir.ResultField{"complete": {Type: ir.PrimitiveBoolean}},
		Context:      ir.TaskContext{History: ir.HistoryFull},
	}
	agent.TaskGroups["verification"] = ir.TaskGroup{
		Steps: []string{"verify", "complete"}, ContextScope: ir.ContextShared,
		Then: ir.GroupReturn, Merge: ir.GroupMergeResults,
	}
	agent.Controls["run_verify"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Group: "verification", When: "Verify the caller.",
	}
	transfer := agent.Controls["to_billing"].(*ir.AgentTransfer)
	transfer.Requires = []string{"customer_id"}
	transfer.Announce = "I’ll connect you with billing now."
	intake := agent.Agents["intake"]
	intake.Instructions += "\nCurrent customer: {{customer_id}}."
	intake.Tools = append(intake.Tools, "run_verify")
	agent.Agents["intake"] = intake
}

// A task-scoped transfer is terminal for the current Flow. The pinned Pipecat
// SDK treats NO_RESPONSE as stay-on-node without another LLM turn; None would
// re-run the source node and allow the remaining group steps to continue.
func TestPipecatV1TaskTransferStopsFlowAndPreservesFullHistory(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	addPipecatTaskTransferFixture(agent)
	verify := agent.Tasks["verify"]
	verify.Result["details"] = ir.ResultField{Schema: map[string]any{"type": "object"}}
	agent.Tasks["verify"] = verify

	artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate task transfer: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"from pipecat.flows import FlowManager, FlowsFunctionSchema, NodeConfig, NO_RESPONSE",
		`name="to_billing"`,
		"handler=self._run_verify_transfer_verify_to_billing",
		"async def _run_verify_transfer_verify_to_billing(self, args, flow_manager):",
		`self._run_verify_active_step = "verify"`,
		`_unmet = _unmet_prerequisites(self.state, ["customer_id"])`,
		`if self._run_verify_active_step != "verify":`,
		`return {"status": "already handled"}, NO_RESPONSE`,
		`async def on_activated(self, args) -> None:`,
		`delta=LLMSettings(system_instruction=_render(INTAKE_PROMPT, self.state))`,
		`task_start = len(messages) if flow_messages[:len(messages)] == messages else 0`,
		`if message.get("role") in {"user", "assistant", "tool"}`,
		`return {"transferred": True}, NO_RESPONSE`,
		`return {"status": "ok", "result": self._run_verify_results["verify"]}, self._run_verify_node_complete()`,
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing task-transfer invariant %q", want)
		}
	}

	transferAt := strings.Index(bot, "async def _run_verify_transfer_verify_to_billing")
	finishAt := strings.Index(bot, "async def _run_verify_finish_verify")
	nextFinishAt := strings.Index(bot, "async def _run_verify_finish_complete")
	if transferAt < 0 || finishAt < transferAt || nextFinishAt < finishAt {
		t.Fatalf("task transfer/finish chain missing: transfer=%d finish=%d next=%d", transferAt, finishAt, nextFinishAt)
	}
	transferBody := bot[transferAt:finishAt]
	requiresAt := strings.Index(transferBody, "_unmet = _unmet_prerequisites(")
	stepClaimAt := strings.Index(transferBody, "self._run_verify_active_step = None")
	tryAt := strings.Index(transferBody, "try:")
	announceAt := strings.Index(transferBody, "await self._announce_handoff")
	activateAt := strings.Index(transferBody, "await self.activate_worker")
	releaseAt := strings.Index(transferBody, `self._run_verify_active_step = "verify"`)
	restoreMessagesAt, restoreToolsAt := -1, -1
	if releaseAt >= 0 {
		restoreMessagesAt = strings.Index(transferBody[releaseAt:], "self.context.set_messages(flow_messages)")
		restoreToolsAt = strings.Index(transferBody[releaseAt:], "self.context.set_tools(flow_tools)")
	}
	if requiresAt < 0 || stepClaimAt < requiresAt || tryAt < stepClaimAt || announceAt < tryAt || activateAt < announceAt || releaseAt < activateAt || restoreMessagesAt < 0 || restoreToolsAt < restoreMessagesAt {
		t.Fatalf("task transfer must refuse, claim, attempt, then restore and release on failure: requires=%d step_claim=%d try=%d announce=%d activate=%d release=%d restore_messages=%d restore_tools=%d\n%s", requiresAt, stepClaimAt, tryAt, announceAt, activateAt, releaseAt, restoreMessagesAt, restoreToolsAt, transferBody)
	}
	if !strings.Contains(transferBody[:stepClaimAt], `return {"refused": _prerequisite_refusal(_unmet, False)}, None`) {
		t.Error("a recoverable requires refusal must stay on the task and let its LLM respond")
	}
	finishBody := bot[finishAt:nextFinishAt]
	stepGuardAt := strings.Index(finishBody, `if self._run_verify_active_step != "verify":`)
	advanceAt := strings.Index(finishBody, `self._run_verify_active_step = "complete"`)
	nextAt := strings.Index(finishBody, "self._run_verify_node_complete()")
	if stepGuardAt < 0 || advanceAt < stepGuardAt || nextAt < advanceAt {
		t.Fatalf("finish must reject stale calls and claim the next step before building its transition:\n%s", finishBody)
	}
	if !strings.Contains(finishBody[stepGuardAt:nextAt], `return {"status": "already handled"}, NO_RESPONSE`) {
		t.Error("a stale finish call can still advance to the next task")
	}

	finalBody := bot[nextFinishAt:]
	for _, want := range []string{
		`return await self._run_verify_complete_complete()`,
		`async def _run_verify_complete_complete(self):`,
		`self._run_verify_active_step = "complete"`,
		`self._run_verify_results.pop("complete", None)`,
		// The prompt continues with the compiler's finish contract, so match its
		// opening rather than the whole literal.
		`delta=LLMSettings(system_instruction="Complete verification.`,
	} {
		if !strings.Contains(finalBody, want) {
			t.Errorf("final task rollback missing %q", want)
		}
	}

	if _, err := exec.LookPath("python3"); err == nil {
		path := filepath.Join(t.TempDir(), "bot.py")
		if err := os.WriteFile(path, []byte(bot), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("python3", "-m", "py_compile", path).CombinedOutput(); err != nil {
			t.Fatalf("task-transfer bot.py is not valid Python:\n%s", out)
		}
	}
}

// TestPipecatV1TasksGolden exercises tasks, task groups, and delegates that
// safe_core omits, including the task-only role boundary (V26).
func TestPipecatV1TasksGolden(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	enableLangfuse(agent)

	agent.Tasks["collect"] = ir.Task{
		// A guided conversational step: it talks to the caller and uses a tool
		// (B7 — the old @job lowering could do neither). Per-task model stays
		// out: it is gated on Pipecat (no LLMSwitcher inside an LLMWorker).
		Instructions: "Ask for the caller's email, look them up, and confirm their account tier.",
		Tools:        []string{"lookup_customer"},
		Result: map[string]ir.ResultField{
			"verified_flag": {Type: ir.PrimitiveBoolean},
			"tier":          {Type: ir.PrimitiveString, Enum: []string{"free", "pro"}},
		},
		Context: ir.TaskContext{History: ir.HistoryFull},
	}
	agent.TaskGroups["triage"] = ir.TaskGroup{
		Steps: []string{"collect"}, ContextScope: ir.ContextIsolated, Then: ir.GroupReturn, Merge: ir.GroupMergeResults,
	}
	agent.Controls["run_collect"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Task: "collect", When: "Collect the caller's account details.",
		Assign: map[string]string{"verified": "result.verified_flag"},
	}
	agent.Controls["run_triage"] = &ir.Delegate{Kind: ir.ControlDelegate, Group: "triage", When: "Run the triage group."}
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "run_collect", "run_triage")
	agent.Agents["intake"] = intake

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var bot string
	for _, file := range artifact.Files {
		if file.Path == "bot.py" {
			bot = string(file.Content)
		}
	}
	if bot == "" {
		t.Fatal("bot.py not emitted")
	}
	for _, want := range []string{
		"from pipecat.frames.frames import EndFrame, FunctionCallResultProperties, LLMMessagesAppendFrame, LLMUpdateSettingsFrame, TTSSpeakFrame",
		"from pipecat.services.settings import LLMSettings",
		// The compiler appends its finish contract, so this matches the
		// authored opening only.
		`role_message="Ask for the caller's email, look them up, and confirm their account tier.`,
		`task_messages=[{"role": "developer", "content": "Begin this step."}]`,
		// The delegate resolves its call with run_llm=False so only the flow node
		// responds — no double assistant turn (V7/B4).
		`properties=FunctionCallResultProperties(run_llm=False),`,
		// The agent prompt is one module constant, referenced by builder + restore (V2).
		`INTAKE_PROMPT = """# Intake agent`,
		`delta=LLMSettings(system_instruction=INTAKE_PROMPT)`,
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing task role boundary %q", want)
		}
	}
	if strings.Contains(bot, `task_messages=[{"role": "system"`) {
		t.Error("bot.py still sends task instructions as a second system message")
	}
	activation := strings.Index(bot, "async def on_activated(self, args) -> None:")
	if activation < 0 {
		t.Fatal("bot.py missing the flow owner's activation hook")
	}
	activationBody := bot[activation:]
	restoreOnEntry := strings.Index(activationBody, "await self.queue_frame(LLMUpdateSettingsFrame(")
	activateBase := strings.Index(activationBody, "await super().on_activated(args)")
	if restoreOnEntry < 0 || activateBase < restoreOnEntry {
		t.Error("flow owner must restore its agent role before base activation installs tools and messages")
	}
	if got := strings.Count(bot, "await self.flush_pipeline()"); got != 4 {
		t.Errorf("bot.py drains delegate results and owner role updates %d times, want 4", got)
	}

	path := filepath.Join("testdata", "golden", "pipecat_v1_tasks_bot.py")
	if *updatePipecatV1 {
		if err := os.WriteFile(path, []byte(bot), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bot != string(want) {
		t.Fatalf("pipecat v1 tasks golden differs; run: go test ./internal/generate -run TestPipecatV1TasksGolden -update-pipecat")
	}

	// Return and transfer restore the owner's role before continuing. End goes
	// straight to its terminal node; its only role restore is the re-entry hook.
	delete(agent.Controls, "run_collect")
	intake = agent.Agents["intake"]
	intake.Tools = slices.DeleteFunc(intake.Tools, func(name string) bool { return name == "run_collect" })
	agent.Agents["intake"] = intake
	triage := agent.TaskGroups["triage"]
	triage.Then = ir.GroupTransfer
	triage.ThenTarget = "billing"
	agent.TaskGroups["triage"] = triage
	transferArtifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate transfer task group: %v", err)
	}
	transferBot := artifactFile(t, transferArtifact, "bot.py")
	finish := strings.Index(transferBot, "async def _run_triage_finish_collect")
	if finish < 0 {
		t.Fatal("transfer task group missing final handler")
	}
	finishBody := transferBot[finish:]
	restore := strings.Index(finishBody, "await self.queue_frame(LLMUpdateSettingsFrame(")
	flush := strings.Index(finishBody, "await self.flush_pipeline()")
	activate := strings.Index(finishBody, "await self.activate_worker(")
	if restore < 0 || flush < restore || activate < flush {
		t.Fatalf("transfer must drain owner-role restoration before activation: finish=%d restore=%d flush=%d activate=%d", finish, restore, flush, activate)
	}

	triage.Then = ir.GroupEnd
	triage.ThenTarget = ""
	agent.TaskGroups["triage"] = triage
	endArtifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate end-only task group: %v", err)
	}
	endBot := artifactFile(t, endArtifact, "bot.py")
	// then: end lowers to the Flows end_conversation post-action on a terminal
	// node, not a raw EndFrame in the finish handler (V4/B2, dp§V2 doc-wins).
	if !strings.Contains(endBot, `post_actions=[{"type": "end_conversation"}]`) {
		t.Error("then: end must end via the Flows end_conversation post-action")
	}
	if !strings.Contains(endBot, "def _run_triage_end_node(self) -> NodeConfig:") {
		t.Error("then: end must transition to a terminal end node")
	}
	endFinish := strings.Index(endBot, "async def _run_triage_finish_collect")
	if endFinish < 0 {
		t.Fatal("end task group missing final handler")
	}
	endFinishBody := endBot[endFinish:]
	if next := strings.Index(endFinishBody[1:], "\n    def "); next >= 0 {
		endFinishBody = endFinishBody[:next]
	}
	if strings.Contains(endFinishBody, "queue_frame(EndFrame())") {
		t.Error("then: end finish handler still queues a raw EndFrame (B2)")
	}
	if strings.Contains(endFinishBody, "LLMUpdateSettingsFrame") {
		t.Error("then: end finish handler restores a role that cannot run again")
	}
	if !strings.Contains(endFinishBody, "self._run_triage_end_node()") {
		t.Error("then: end finish handler must return the terminal end node")
	}
}

func TestV2_PipecatDelegateSnapshotsCompletedOwnerCall(t *testing.T) {
	bot, err := os.ReadFile(filepath.Join("testdata", "golden", "pipecat_v1_tasks_bot.py"))
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"run_collect", "run_triage"} {
		start := bytes.Index(bot, []byte("    async def "+name+"(self, params: FunctionCallParams):"))
		if start < 0 {
			t.Fatalf("missing generated delegate %q", name)
		}
		end := bytes.Index(bot[start:], []byte("\n    def _"+name+"_node_"))
		if end < 0 {
			t.Fatalf("missing first node after generated delegate %q", name)
		}
		body := bot[start : start+end]
		callback := bytes.Index(body, []byte("await params.result_callback("))
		flush := bytes.Index(body, []byte("await self.flush_pipeline()"))
		snapshot := bytes.Index(body, []byte("_snapshot ="))
		initialize := bytes.Index(body, []byte("await flow.initialize("))
		if callback < 0 || flush < callback || snapshot < flush || initialize < snapshot {
			t.Errorf("delegate %q must resolve, drain, snapshot, then initialize: callback=%d flush=%d snapshot=%d initialize=%d", name, callback, flush, snapshot, initialize)
		}
		// The snapshot must deep-copy history. The old shallow form,
		// [dict(m) for m in self.context.get_messages()], silently drops any
		// entry that is not a plain dict (pipecat's LLMSpecificMessage), so a
		// delegate that returns to its caller comes back with degraded history.
		if !bytes.Contains(body, []byte("copy.deepcopy(self.context.get_messages())")) {
			t.Errorf("delegate %q must snapshot history with copy.deepcopy, not a shallow dict() copy", name)
		}
	}
	if !bytes.Contains(bot, []byte("\nimport copy\n")) {
		t.Error("a bot with flows must import copy for the delegate snapshot")
	}
}

// A worker that fails to start says why, at the point where why is still known.
// The failure is recorded and the runner cancelled, which makes run() raise
// CancelledError; that reaches the caller first and takes the recorded error's
// place, so the re-raise below it never runs. Measured on a real call
// 2026-08-21: the session died half a second in and the only line it left
// behind was a failed trace flush, which says nothing about the cause.
func TestPipecatWorkerStartFailureSaysWhy(t *testing.T) {
	bot, err := os.ReadFile(filepath.Join("testdata", "golden", "pipecat_v1_tasks_bot.py"))
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(bot, []byte("await runner.add_workers(*agents)"))
	if start < 0 {
		t.Fatal("this bot starts no agent workers")
	}
	cancel := bytes.Index(bot[start:], []byte(`await runner.cancel(reason="agent worker startup failed")`))
	if cancel < 0 {
		t.Fatal("nothing cancels the runner when a worker fails to start")
	}
	logged := bytes.Index(bot[start:start+cancel], []byte("logger.exception("))
	if logged < 0 {
		t.Error("the worker start failure is not logged before the runner is cancelled, so the " +
			"cancellation replaces it and the session dies without naming a cause")
	}
}

// TestV3PipecatToolsResolveCallback: every emitted agent-level @tool method
// resolves its function call via params.result_callback. Pipecat drops a @tool's
// return value, so a bare `return {...}` leaves the call unresolved (V3, B1). The
// delegate @tool that starts a FlowManager is the one B1 caught.
func TestV3PipecatToolsResolveCallback(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Tasks["collect"] = ir.Task{
		Instructions: "Ask for the caller's email and confirm their tier.",
		Tools:        []string{"lookup_customer"},
		Result:       map[string]ir.ResultField{"tier": {Type: ir.PrimitiveString}},
		Context:      ir.TaskContext{History: ir.HistoryFull},
	}
	agent.Controls["run_collect"] = &ir.Delegate{Kind: ir.ControlDelegate, Task: "collect", When: "Collect account details."}
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "run_collect")
	agent.Agents["intake"] = intake

	artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")

	// A @tool method has the signature `(self, params: FunctionCallParams …)`.
	// Flow-internal handlers take `(args, flow_manager)` / `(self, args, …)` and
	// return per the Flows contract instead, so this regex skips them.
	sig := regexp.MustCompile(`(?m)^    async def (\w+)\(self, params: FunctionCallParams`)
	methods := sig.FindAllStringSubmatchIndex(bot, -1)
	if len(methods) == 0 {
		t.Fatal("no @tool methods emitted")
	}
	for i, loc := range methods {
		name := bot[loc[2]:loc[3]]
		end := len(bot)
		if i+1 < len(methods) {
			end = methods[i+1][0]
		}
		if !strings.Contains(bot[loc[0]:end], "params.result_callback") {
			t.Errorf("@tool %q never calls params.result_callback; its function call is left unresolved", name)
		}
	}
	if strings.Contains(bot, `return {"status": "running`) {
		t.Error("delegate @tool still returns a status dict instead of resolving the call (B1)")
	}
}

// A provider may send an argument outside the advertised direct-function
// schema. Pipecat expands that mapping into keyword arguments before calling
// the generated handler, so every direct tool needs one shared boundary that
// turns both bad keywords and handler failures into exactly one safe result.
// functools.wraps keeps the declared signature visible to Pipecat; Flow
// handlers use their own callback contract and do not pass through this guard.
func TestPipecatV1DirectToolGuard(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Tasks["collect"] = ir.Task{
		Instructions: "Collect the caller's email.",
		Tools:        []string{"lookup_customer"},
		Result:       map[string]ir.ResultField{"email": {Type: ir.PrimitiveString}},
		Context:      ir.TaskContext{History: ir.HistoryFull},
	}
	agent.Controls["run_collect"] = &ir.Delegate{
		Kind: ir.ControlDelegate, Task: "collect", When: "Collect account details.",
	}
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "run_collect")
	agent.Agents["intake"] = intake
	artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")

	for _, want := range []string{
		"def _direct_tool(fn=None, *, cancel_on_interruption=True, timeout_secs=None):",
		"@functools.wraps(handler)",
		"unexpected = sorted(set(kwargs) - declared - {\"params\"})",
		"original_result_callback = params.result_callback",
		"if not resolved:",
		"await original_result_callback({",
		"@_direct_tool(cancel_on_interruption=False)",
		"@_direct_tool\n    async def lookup_customer(",
		"@_direct_tool\n    async def run_collect(",
		"async def _flow_tool_lookup_customer(",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing direct-tool guard %q", want)
		}
	}
	if strings.Contains(bot, "@_direct_tool\n    async def _run_") {
		t.Error("Flow handlers must keep Pipecat Flows' own result contract")
	}
	if regexp.MustCompile(`(?m)^\s*@tool(?:\(|$)`).MatchString(bot) {
		t.Error("a generated direct tool bypasses the shared guard")
	}
	if !strings.Contains(bot, `async def lookup_customer(self, params: FunctionCallParams, email: str = "", phone: str = ""):`) {
		t.Error("guard changed the generated tool's declared signature")
	}
}

func TestV2PipecatV1AgentTransferAnnouncementWaitsForSourcePlayout(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	transfer := agent.Controls["to_billing"].(*ir.AgentTransfer)
	transfer.Requires = []string{"customer_id"}
	transfer.Announce = "I’ll connect you with billing now."
	agent.Conversation.Greeting = &ir.Greeting{SpeaksFirst: ir.SpeaksFirstUser}

	artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	start := strings.Index(bot, "    async def to_billing(")
	if start < 0 {
		t.Fatal("to_billing transfer method not emitted")
	}
	body := bot[start:]
	if end := strings.Index(body[1:], "\n    async def "); end >= 0 {
		body = body[:end+1]
	}
	requiresAt := strings.Index(body, "_unmet = _unmet_prerequisites(")
	announcementAt := strings.Index(body, `await self._announce_handoff("I’ll connect you with billing now.")`)
	activateAt := strings.Index(body, "await self.activate_worker(")
	if requiresAt < 0 || activateAt < 0 || announcementAt < 0 {
		t.Fatalf("transfer method missing requires / exact source announcement / target activation:\n%s", body)
	}
	if requiresAt >= announcementAt || announcementAt >= activateAt {
		t.Errorf("transfer must guard, finish the exact source announcement, then activate the receiver:\n%s", body)
	}
	if strings.Contains(body[:activateAt], "messages=[") {
		t.Errorf("the source announcement must not start a second LLM turn:\n%s", body)
	}
	for _, want := range []string{
		"class _HandoffWorker(",
		"async def _announce_handoff(self, announcement: str) -> None:",
		"await PipelineWorker.queue_frame(self, TTSSpeakFrame(announcement))",
		"await asyncio.wait_for(self._handoff_finished.wait(), timeout=30.0)",
		"isinstance(frame, BotStartedSpeakingFrame)",
		"isinstance(frame, BotStoppedSpeakingFrame)",
		"async def process_deferred_tool_frames(self, frames):",
		"if not self.active:",
		"return []",
		"class IntakeAgent(_HandoffWorker):",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("announced handoff is missing source playout step %q", want)
		}
	}
	if !regexp.MustCompile(`(?m)^from pipecat\.frames\.frames import .*BotStartedSpeakingFrame.*BotStoppedSpeakingFrame.*TTSSpeakFrame`).MatchString(bot) {
		t.Error("an announced handoff must import playout and speech frames even without a text greeting")
	}
	if !strings.Contains(body, "run_llm=True") {
		t.Error("receiver must answer normally after source playout completes")
	}
	if !strings.Contains(body, `"content": "Caller asks about billing, an invoice, or a refund."`) {
		t.Error("target activation lost its existing transfer reason")
	}

	transfer.Announce = ""
	silent, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate silent transfer: %v", err)
	}
	silentBot := artifactFile(t, silent, "bot.py")
	silentStart := strings.Index(silentBot, "    async def to_billing(")
	silentBody := silentBot[silentStart:]
	if end := strings.Index(silentBody[1:], "\n    async def "); end >= 0 {
		silentBody = silentBody[:end+1]
	}
	if strings.Contains(silentBody, "_announce_handoff(") || strings.Contains(silentBot, "class _HandoffWorker(") {
		t.Error("omitted announce must preserve the ordinary silent handoff")
	}
	if !strings.Contains(silentBody, "run_llm=True") {
		t.Error("a silent handoff must let the receiver answer on activation")
	}
}

// TestF3PipecatSingleAgentInline: a single agent with no handoffs, tasks,
// variables, tracing, or telephony collapses to the inline shape (F3) — the LLM
// sits directly in the pipeline, tools are module-level direct functions in
// LLMContext, and there is no bus / BusBridge / LLMWorker / activate_worker.
func TestF3PipecatSingleAgentInline(t *testing.T) {
	pkg, err := spec.Load(examplePackagePath("simple-prompt"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Tracing = nil // simple-prompt ships tracing; the inline path is scoped to no-tracing

	bot := artifactFile(t, mustGeneratePipecatInline(t, agent), "bot.py")

	for _, absent := range []string{
		"BusBridgeProcessor",
		"activate_worker",
		"LLMWorkerActivationArgs",
		"class AppointmentDeskAgent",
	} {
		if strings.Contains(bot, absent) {
			t.Errorf("inline bot.py should not contain %q (bus scaffolding)", absent)
		}
	}
	for _, want := range []string{
		"async def lookup_customer(params: FunctionCallParams", // tool as a module-level direct function
		"context = LLMContext(tools=[",                         // tools registered on the context
		"build_appointment_desk_llm(),",                        // LLM inline in the pipeline
		"worker = PipelineWorker(",                             // a plain PipelineWorker, no bus
		`await worker.queue_frame(TTSSpeakFrame(`,              // text greeting queued directly
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("inline bot.py missing %q", want)
		}
	}

	if _, err := exec.LookPath("python3"); err == nil {
		f := filepath.Join(t.TempDir(), "bot.py")
		if err := os.WriteFile(f, []byte(bot), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("python3", "-m", "py_compile", f).CombinedOutput(); err != nil {
			t.Fatalf("inline bot.py is not valid Python:\n%s", out)
		}
	}
}

// MCP always uses the worker topology, even with one untraced agent. Keeping one
// lifecycle path prevents the inline optimization from growing a second MCP
// transaction and collision implementation.
func TestPipecatV1MCPUsesWorkerTopology(t *testing.T) {
	pkg, err := spec.Load(examplePackagePath("simple-prompt"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Tracing = nil // the inline path is scoped to no-tracing
	agent.Tools["web_search"] = ir.Tool{
		Execution: ir.ToolMCP, URLEnv: "FIRECRAWL_MCP_URL",
		MCPTransport: ir.MCPTransportStreamableHTTP, MCPTools: []string{"firecrawl_search"},
		Auth:         &ir.ToolAuth{Type: ir.ToolAuthBearer, TokenEnv: "FIRECRAWL_API_KEY"},
		Interruption: ir.ToolProviderDefault, Effect: ir.ToolReturnsData,
	}
	def := agent.Agents[agent.EntryAgent]
	def.Tools = append(def.Tools, "web_search")
	agent.Agents[agent.EntryAgent] = def

	artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"from pipecat.bus import BusBridgeProcessor",
		"class AppointmentDeskAgent(LLMWorker):",
		"self._mcp_clients = [",
		"self._mcp_tools = await _register_mcp_tools(",
		"await agent.start_mcp()",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("MCP worker bot.py missing %q:\n%s", want, bot)
		}
	}
	for _, absent := range []string{
		"One agent, no handoffs: the LLM runs inline",
		"web_search_mcp_tools.standard_tools",
	} {
		if strings.Contains(bot, absent) {
			t.Errorf("MCP package must not use the retired inline path %q", absent)
		}
	}
	if _, err := exec.LookPath("python3"); err == nil {
		f := filepath.Join(t.TempDir(), "bot.py")
		if err := os.WriteFile(f, []byte(bot), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("python3", "-m", "py_compile", f).CombinedOutput(); err != nil {
			t.Fatalf("MCP worker bot.py is not valid Python:\n%s", out)
		}
	}
}

func mustGeneratePipecatInline(t *testing.T, agent *ir.Agent) Artifact {
	t.Helper()
	artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return artifact
}

// TestV1PipecatAgentToolCarriesSchema: the same tool YAML reaches the LLM with
// its declared schema on BOTH emission paths (V1) — the agent-level @tool as a
// typed signature + Google `Args:` docstring (per-property description + enum
// prose, since Pipecat's direct-function generator does not map Literal→enum),
// and the Flow-node FlowsFunctionSchema as verbatim `properties`. Also proves
// required params precede optional so the signature is valid Python (V5/B3).
func TestV1PipecatAgentToolCarriesSchema(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Tools["book_service"] = ir.Tool{
		Description: "Book a service slot.",
		Execution:   ir.ToolWebhook,
		URLEnv:      "BOOK_SERVICE_URL",
		Input: map[string]any{
			"type": "object",
			"properties": map[string]any{
				// `notes`/`party_size` are optional and sort alphabetically before
				// the required `service`; the emitter must still list `service` first.
				"service":    map[string]any{"type": "string", "enum": []any{"haircut", "hair-color", "blowout"}, "description": "Which service"},
				"party_size": map[string]any{"type": "integer", "description": "Guests"},
				"notes":      map[string]any{"type": "string", "description": "Extra notes"},
			},
			"required": []any{"service"},
		},
	}
	// Reference it as an agent-level tool AND inside a task (the Flow path).
	agent.Tasks["book"] = ir.Task{
		Instructions: "Book the caller's chosen service.",
		Tools:        []string{"book_service"},
		Result:       map[string]ir.ResultField{"ok": {Type: ir.PrimitiveBoolean}},
		Context:      ir.TaskContext{History: ir.HistoryFull},
	}
	agent.Controls["run_book"] = &ir.Delegate{Kind: ir.ControlDelegate, Task: "book", When: "Book a service."}
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "book_service", "run_book")
	agent.Agents["intake"] = intake

	artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")

	for _, want := range []string{
		// Agent-level @tool: required first, typed, then optional with defaults.
		"async def book_service(self, params: FunctionCallParams, service: str, notes: str = \"\", party_size: int = 0):",
		// Google Args docstring carries descriptions + enum prose + real types.
		"            service (str): Which service One of: haircut, hair-color, blowout.",
		"            notes (str): Extra notes",
		"            party_size (int): Guests",
		// Flow-node path: the same schema verbatim as FlowsFunctionSchema properties.
		`properties={"notes": {"description": "Extra notes", "type": "string"}, "party_size": {"description": "Guests", "type": "integer"}, "service": {"description": "Which service", "enum": ["haircut", "hair-color", "blowout"], "type": "string"}}`,
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing schema fidelity %q", want)
		}
	}

	// The emitted signature must be valid Python (required-before-optional, B3).
	if _, err := exec.LookPath("python3"); err == nil {
		f := filepath.Join(t.TempDir(), "bot.py")
		if err := os.WriteFile(f, []byte(bot), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("python3", "-m", "py_compile", f).CombinedOutput(); err != nil {
			t.Fatalf("emitted bot.py is not valid Python:\n%s", out)
		}
	}
}

// TestPipecatRuffCheckClean: the raw generator output (template-only, before the
// write-path ruff format pass) passes `ruff check --select F` — only used
// imports, no undefined names (V2). Uses a feature-rich fixture (tools + a Flow
// delegate + tracing + variables). Skips when ruff is absent.
func TestPipecatRuffCheckClean(t *testing.T) {
	if _, err := exec.LookPath("ruff"); err != nil {
		t.Skip("ruff not installed")
	}
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	enableLangfuse(agent)
	agent.Tasks["collect"] = ir.Task{
		Instructions: "Ask for the caller's email and confirm their tier.",
		Tools:        []string{"lookup_customer"},
		Result:       map[string]ir.ResultField{"tier": {Type: ir.PrimitiveString, Enum: []string{"free", "pro"}}},
		Context:      ir.TaskContext{History: ir.HistoryFull},
	}
	agent.Controls["run_collect"] = &ir.Delegate{Kind: ir.ControlDelegate, Task: "collect", When: "Collect account details."}
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "run_collect")
	agent.Agents["intake"] = intake

	artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cmd := exec.Command("ruff", "check", "--select", "F", "-")
	cmd.Stdin = strings.NewReader(artifactFile(t, artifact, "bot.py"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("raw generated bot.py is not `ruff check --select F` clean:\n%s", out)
	}
}

func TestPipecatRejectsBlankTaskInstructions(t *testing.T) { // V27
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Tasks["blank"] = ir.Task{
		Instructions: " \n\t",
		Result:       map[string]ir.ResultField{"done": {Type: ir.PrimitiveBoolean}},
		Context:      ir.TaskContext{History: ir.HistoryFull},
	}
	agent.Controls["run_blank"] = &ir.Delegate{Kind: ir.ControlDelegate, Task: "blank", When: "Run the blank task."}
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "run_blank")
	agent.Agents["intake"] = intake

	_, err = GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err == nil || !strings.Contains(err.Error(), `task "blank" instructions must not be empty`) {
		t.Fatalf("blank task instructions must fail closed, got %v", err)
	}
}

func TestPipecatV1OmitsTracingUnlessConfigured(t *testing.T) { // V19, V31
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatal(err)
	}
	if artifactHasFile(artifact, "tracing.py") {
		t.Fatal("unconfigured artifact emitted tracing.py")
	}
	for path, forbidden := range map[string][]string{
		"bot.py":         {"Langfuse", "LANGFUSE_", "setup_tracing", "enable_tracing"},
		"pyproject.toml": {"opentelemetry"},
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

func assertValidYAML(t *testing.T, content string) {
	t.Helper()
	var decoded map[string]any
	if err := yaml.Unmarshal([]byte(content), &decoded); err != nil {
		t.Fatalf("invalid generated YAML: %v\n%s", err, content)
	}
}

func assertGoldenFile(t *testing.T, path, got string, update bool) {
	t.Helper()
	if update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden differs; rerun the test with its update flag")
	}
}

// TestPipecatV1LocalTool covers execution: local (T14, V13): the same @tool
// shape as webhook whose body imports + awaits the user handler from
// tools/<name>.py, at both sites — agent @tool method and flows handler. The
// handler file rides the artifact verbatim, mirroring the LiveKit driver.
func TestPipecatV1LocalTool(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
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
	agent.Tasks["collect"] = ir.Task{
		Instructions: "Ask what the caller needs, then pull their notes.",
		Tools:        []string{"fetch_notes"},
		Result:       map[string]ir.ResultField{"summary": {Type: ir.PrimitiveString}},
		Context:      ir.TaskContext{History: ir.HistoryFull},
	}
	agent.Controls["run_collect"] = &ir.Delegate{Kind: ir.ControlDelegate, Task: "collect", When: "Collect the caller's request."}
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "fetch_notes", "run_collect")
	agent.Agents["intake"] = intake

	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"import inspect",
		"import tools.fetch_notes",
		"async def fetch_notes(self, params: FunctionCallParams, topic: str):",
		"result = tools.fetch_notes.fetch_notes(topic=topic)",
		"if inspect.isawaitable(result):",
		"await params.result_callback(result)",
		"result = tools.fetch_notes.fetch_notes(**dict(args))", // flows handler site
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q", want)
		}
	}
	if handler := artifactFile(t, artifact, "tools/fetch_notes.py"); !strings.Contains(handler, "def fetch_notes(topic):") {
		t.Errorf("handler not copied verbatim:\n%s", handler)
	}
	artifactFile(t, artifact, "tools/__init__.py")
}

// TestPipecatEmitterMatchesCapabilityTable is the table↔emitter agreement test
// (compiler V19 / T12): the emitter's declared code paths must equal the table's
// non-gated Pipecat rows, so no field is validate-green yet silently unemitted.
func TestPipecatEmitterMatchesCapabilityTable(t *testing.T) {
	table := target.Default()
	for field := range table.Fields {
		capability := table.Capability(field, target.Pipecat)
		supported := capability.Tag != target.Gated && capability.Tag != target.Provisional
		if pipecatEmittedFields[field] != supported {
			t.Errorf("field %q: emitter emits=%v, table supported=%v (tag %q) — implement or gate to reconcile",
				field, pipecatEmittedFields[field], supported, capability.Tag)
		}
	}
}

// A package without a warm transfer emits none of the machinery (SPEC V4).
func TestPipecatWithoutWarmTransferEmitsNoBridge(t *testing.T) {
	agent := loadCompilerAgent(t)
	artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, forbidden := range []string{"_HoldMixer", "_AudioBridge", "_warm_transfer", "MixerEnableFrame", "ContextVar"} {
		if strings.Contains(bot, forbidden) {
			t.Errorf("bot.py emits warm-transfer machinery %q without a warm transfer", forbidden)
		}
	}
}

// This driver does not emit warm transfer yet (SPEC C4, V1, N33). The gate
// rejects warm before generation; this is the emitter's defense in depth, and it
// must refuse rather than quietly lower the shape as cold.
//
// The refusal's wording is part of the contract. Daily documents warm, so a
// message saying the platform lacks it would send an author to look for a
// different platform when what they actually need is feature 005.
func TestPipecatWarmHumanTransferFailsEverywhere(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "daily_carrier"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	human := agent.Controls["send_to_billing"].(*ir.HumanTransfer)
	human.Mode = ir.TransferWarm
	human.Briefing = "Say who is calling and why."
	_, err = GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "does not emit warm transfer yet") {
		t.Fatalf("warm transfer must fail instead of lowering cold, got %v", err)
	}
	if strings.Contains(err.Error(), "no native warm transfer") {
		t.Errorf("the refusal claims a platform limitation Daily's own docs contradict: %v", err)
	}
}

func TestPipecatCarrierlessHumanTransferFailsBeforeLowering(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Controls["to_human"] = &ir.HumanTransfer{
		Kind: ir.ControlHumanTransfer, Destination: "billing_line", Mode: ir.TransferCold,
		OnUnavailable: ir.OnUnavailableReturn,
	}
	billing := agent.Agents["billing"]
	billing.Tools = append(billing.Tools, "to_human")
	agent.Agents["billing"] = billing
	resolved := agent.Targets["pipecat"]
	resolved.Transport = "daily-sip"
	resolved.Connection = "removed_carrierless_route"
	resolved.Destinations = map[string]string{"billing_line": "BILLING_PHONE_NUMBER"}

	_, err = GeneratePipecat(agent, resolved, nil, nil)
	if err == nil {
		t.Fatal("direct generation lowered a Daily transfer with no phone session")
	}
	for _, want := range []string{"active channels.phone Connection", "sessionId"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("generation error missing %q: %v", want, err)
		}
	}
}

// The serviceInfo coverage test (driver-pipecat V11) is superseded: class,
// import, and install now travel together on one catalogue entry, so an
// emitted class structurally cannot lose its import (TestCatalogInvariants in
// internal/target). TestPipecatUnknownProviderFailsClosed keeps B1's failure
// mode (a silent OpenAI-compatible substitution) explicitly covered.
func TestPipecatUnknownProviderFailsClosed(t *testing.T) {
	env := newEnvSet()
	_, err := ttsService(ir.Binding{Provider: "acme", Model: "m", Voice: "v"}, env)
	if err == nil || !strings.Contains(err.Error(), "endpoint_env") {
		t.Fatalf("unknown provider without endpoint_env must fail closed, got %v", err)
	}
	svc, err := ttsService(ir.Binding{Provider: "acme", Model: "m", Voice: "v", EndpointEnv: "ACME_URL"}, env)
	if err != nil {
		t.Fatalf("OpenAI-compatible endpoint path: %v", err)
	}
	if svc.Call.Class != "OpenAITTSService" || svc.APIKeyEnv != "ACME_API_KEY" {
		t.Fatalf("wildcard resolution = %+v", svc)
	}
}

// TestPipecatListenAssemblyAI is the new-provider recipe, proven end to end:
// the assemblyai catalogue entry (added in the catalogue pilot) turns a
// listen binding into the Settings-style constructor, its import, its extra,
// and its env — with no driver or template change.
func TestPipecatListenAssemblyAI(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	tgt := targetByProvider(t, agent, ir.ProviderPipecat)
	tgt.Models.Listen = &ir.Binding{Provider: "assemblyai", Model: "universal-3-5-pro"}
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"from pipecat.services.assemblyai.stt import AssemblyAISTTService",
		"settings=AssemblyAISTTService.Settings(\n            model=\"universal-3-5-pro\",",
		"api_key=os.environ[\"ASSEMBLYAI_API_KEY\"],",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q", want)
		}
	}
	if pyproject := artifactFile(t, artifact, "pyproject.toml"); !strings.Contains(pyproject, "assemblyai") {
		t.Error("pyproject.toml missing the assemblyai extra")
	}
}

// TestCheckPipecatVersion pins the one SDK version exercised by the release
// matrix; compatibility with untested versions is not implied.
func TestCheckPipecatVersion(t *testing.T) {
	for _, tc := range []struct {
		version string
		ok      bool
	}{
		{"1.5.0", false},
		{"1.5.3", false},
		{"1.6.0", false},
		{"1.6.9", false},
		{"1.8.0", true},
		{"1.7.0", false}, // the previous pin; one version is supported, not a range
		{"1.0.3", false}, // never existed on PyPI; workers API not present
		{"1.4.9", false},
		{"1", false}, // too vague / pre-1.5
		{"0.0.108", false},
		{"2.0.0", false},
		{"", false},
		{"latest", false},
	} {
		err := checkPipecatVersion(tc.version)
		if (err == nil) != tc.ok {
			t.Errorf("checkPipecatVersion(%q): ok=%v, err=%v", tc.version, tc.ok, err)
		}
	}
}

// dailyTransportParamsClass returns the class the emitted transport_params map
// constructs for one key, so a test can compare keys instead of pattern-matching
// a whole file.
func transportParamsClass(t *testing.T, bot, key string) string {
	t.Helper()
	pattern := regexp.MustCompile(`(?m)^\s*"` + regexp.QuoteMeta(key) + `":\s*lambda:\s*([A-Za-z_][A-Za-z0-9_]*)\(`)
	match := pattern.FindStringSubmatch(bot)
	if match == nil {
		t.Fatalf("transport_params has no %q entry", key)
	}
	return match[1]
}

// FR-005 / contracts invariant 5: a route that can receive a phone call must
// build its transport with a parameter object that accepts inbound call fields.
//
// Asserted as a property, not as the string "DailyParams". Pipecat's
// create_transport assigns dialin_settings, api_key, and api_url straight onto
// whatever the factory returns, and the generic TransportParams is a Pydantic
// model that declares none of them and allows no extras — so every inbound Daily
// call died while the transport was being built. The offline half of the proof is
// that the Daily key uses a different, imported class from the browser key; the
// L4 smoke instantiates it against the real package, which is the only layer
// that can prove the fields actually land.
func TestUS1_DailyTransportAcceptsInboundCallFields(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "daily_carrier"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	resolved := agent.Targets["pipecat"]
	if resolved.Transport != "daily-sip" {
		t.Fatalf("fixture transport = %q, want daily-sip", resolved.Transport)
	}
	artifact, err := GeneratePipecat(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	bot := artifactFile(t, artifact, "bot.py")

	daily := transportParamsClass(t, bot, "daily")
	generic := transportParamsClass(t, bot, "webrtc")
	if daily == generic {
		t.Errorf("the daily key builds %s, the same class as the browser key: "+
			"create_transport assigns dialin_settings, api_key, and api_url onto it, "+
			"and the generic class declares none of them, so every inbound call fails", daily)
	}
	// Class and import travel as one unit, so an emitted class structurally
	// cannot lose its import.
	if !regexp.MustCompile(`(?m)^from [\w.]+ import (?:[\w, ]*\b)?` + regexp.QuoteMeta(daily) + `\b`).MatchString(bot) {
		t.Errorf("bot.py builds %s for the daily key but never imports it", daily)
	}
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if !regexp.MustCompile(`pipecat-ai\[[^]]*\bdaily\b[^]]*\]`).MatchString(pyproject) {
		t.Error("the Daily route imports its transport without installing the pipecat-ai daily extra")
	}
}

// Research D2: the fix is scoped to the Daily route. A package that is not on it
// keeps the generic class for every key and emits no Daily import, because
// bot.py imports only what the package exercises and an unconditional change
// would rewrite every Pipecat golden.
func TestUS1_NonDailyRouteKeepsGenericTransportParams(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	enablePackageTelephony(pkg)
	configured := pkg.Targets["pipecat"]
	configured.Connection = "primary_phone"
	setConnectionRoute(pkg, "primary_phone", "cloud-websocket", "twilio")
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := GeneratePipecat(agent, agent.Targets["pipecat"], nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	generic := transportParamsClass(t, bot, "webrtc")
	if got := transportParamsClass(t, bot, "daily"); got != generic {
		t.Errorf("carrier-websocket route builds %s for the daily key, want the generic %s: "+
			"this route never receives a Daily call, and a Daily import here is a dead import", got, generic)
	}
	if strings.Contains(bot, "pipecat.transports.daily") {
		t.Error("bot.py imports a Daily transport module on a carrier-websocket package")
	}
}

func plainPipecatArtifact(t *testing.T) Artifact {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	pkg.Targets = map[string]spec.Target{"pipecat": pkg.Targets["pipecat"]}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, agent.Targets["pipecat"], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func artifactPaths(artifact Artifact) []string {
	paths := make([]string, 0, len(artifact.Files))
	for _, file := range artifact.Files {
		paths = append(paths, file.Path)
	}
	return paths
}

// Mutable call facts belong to one run_bot invocation. A module-level latch is
// shared by concurrent sessions in the same process and can replay another
// caller's transfer result or phone identity.
func TestPipecatMutableCallStateIsRunLocal(t *testing.T) {
	cases := []struct {
		name string
		bot  string
		want []string
	}{
		{
			name: "daily transfer",
			bot:  artifactFile(t, dailyCarrierArtifact(t, "twilio", false), "bot.py"),
			want: []string{
				"call_context = {}",
				`call_context["_transport"] = transport`,
				`self.call_context.get("_transfer_result")`,
				`self.call_context.get("_transport")`,
			},
		},
		{
			name: "cloud websocket phone call",
			bot: artifactFile(t, cloudWebsocketArtifact(t, cloudWebsocketOptions{
				inbound: true, transfer: true, connection: true,
			}), "bot.py"),
			want: []string{
				"call_context = {}",
				`call_context["_phone_call"] = phone_call`,
				"_pipeline_audio_rates(phone_call)",
				`self.call_context.get("_phone_call")`,
				`self.call_context.get("_transfer_result")`,
			},
		},
		{
			name: "daily carrier forward",
			bot:  artifactFile(t, dailyCarrierArtifact(t, "twilio", false), "bot.py"),
			want: []string{
				"call_context = {}",
				"call_forwarded = False",
				"nonlocal call_forwarded",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Count(tc.bot, "    call_context = {}") != 1 {
				t.Error("run_bot must create exactly one fresh call context")
			}
			if !strings.Contains(tc.bot, "call_context=call_context") {
				t.Error("run_bot does not share its call context with the run's workers")
			}
			for _, want := range tc.want {
				if !strings.Contains(tc.bot, want) {
					t.Errorf("bot.py missing run-local state use %q", want)
				}
			}
			for _, forbidden := range []string{"_TRANSPORT", "_TRANSFER_RESULT", "_PHONE_CALL", "_CALL_FORWARDED"} {
				if strings.Contains(tc.bot, forbidden) {
					t.Errorf("bot.py still carries per-call module global %s", forbidden)
				}
			}
		})
	}
}

// FR-008 / contracts invariant 8: a second transfer request in the same call
// produces no second attempt.
//
// The property is identical on every route; only the mechanism differs. The
// carrier routes hold it in the shared control store. The Daily route keeps it
// in the call context shared by one run's workers and must not gain Redis just
// to remember a fact that never outlives the call.
//
// Before this feature there was no guard at all on this route: two requests in
// one call fired two platform transfers.
func TestUS2_DailyTransferAttemptsOnce(t *testing.T) {
	bot := artifactFile(t, dailyCarrierArtifact(t, "twilio", false), "bot.py")
	primitive := strings.Index(bot, "sip_call_transfer(")
	if primitive < 0 {
		t.Fatal("fixture emits no Daily transfer")
	}
	// Named structurally rather than by symbol, so the assertion survives the
	// guard being renamed or reshaped: somewhere between the tool's docstring
	// and the primitive there must be a condition that returns early.
	body := bot[:primitive]
	tool := body[strings.LastIndex(body, "    async def "):]
	if !strings.Contains(tool, "if ") || !strings.Contains(tool, "return") {
		t.Errorf("nothing between the transfer tool's start and sip_call_transfer can stop a "+
			"second attempt; a caller who asks twice is transferred twice:\n%s", tool)
	}
	// A repeat request must not be told the transfer succeeded when it failed, so
	// the failure paths have to record what they return.
	for _, want := range []string{
		`await params.result_callback(transfer_result)`,
		`self.call_context["_transfer_result"] = {"transferred": True}`,
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q: a replayed answer must be the answer the attempt produced", want)
		}
	}
	// It must not reach for the shared store, which this route does not have.
	if strings.Contains(bot, "STATE.mark_once") || strings.Contains(bot, "REDIS_URL") {
		t.Error("bot.py guards the Daily transfer through the shared control store, " +
			"which this route does not declare and must not gain")
	}
}

// The carrier form of the same route (SCHEMA N37) has a plan, and whose it is
// matters. Processes, endpoints, and services on this route describe the
// *operator-run helper*; the deployed agent still declares nothing of its own.
func TestUS2_DailyCarrierPlanBelongsToTheHelperNotTheAgent(t *testing.T) {
	artifact := dailyCarrierArtifact(t, "twilio", true)
	plan := artifact.Telephony
	if plan == nil {
		t.Fatal("the carrier form resolves no telephony plan")
	}
	if len(plan.Processes) != 1 || plan.Processes[0].Name != "telephony-helper" {
		t.Fatalf("processes = %#v, want the one operator-run helper", plan.Processes)
	}
	if strings.Join(plan.Processes[0].Command, " ") != "uv run telephony_helper.py" {
		t.Errorf("the helper's command = %v", plan.Processes[0].Command)
	}
	names := make([]string, 0, len(plan.PublicEndpoints))
	for _, endpoint := range plan.PublicEndpoints {
		names = append(names, endpoint.Name)
	}
	// Two, not three: the helper answers authenticated incoming calls and reports
	// its health. Placing a call is started against the platform directly.
	if strings.Join(names, ",") != "inbound,health" {
		t.Errorf("endpoints = %v, want the helper's two", names)
	}
	if strings.Join(plan.Services, ",") != "application" {
		t.Errorf("services = %v, want the helper alone: this route keeps no shared control record", plan.Services)
	}
	// Nothing this project wrote serves anything on the agent side. The platform's
	// base image answers the platform's own calls, which is why the Dockerfile
	// starts no server of ours: a uvicorn line here would be the carrier-websocket
	// route's shape on a route that has no carrier websocket.
	docker := artifactFile(t, artifact, "Dockerfile")
	if strings.Contains(docker, "uvicorn") {
		t.Error("the deployed agent's Dockerfile runs a web server of its own; on this route the platform's base image serves the platform")
	}
	// And none of the carrier-websocket route's environment or credentials.
	report := artifactFile(t, artifact, "compile-report.json")
	for _, forbidden := range []string{"REDIS_URL", "redis"} {
		if strings.Contains(report, forbidden) {
			t.Errorf("the carrier build requires %q, which nothing on this route reads", forbidden)
		}
	}
	if !strings.Contains(report, "UNMUTE_PUBLIC_URL") {
		t.Error("the carrier build does not require the exact public helper origin used for signature validation")
	}
	if bot := artifactFile(t, artifact, "bot.py"); strings.Contains(bot, "UNMUTE_PUBLIC_URL") {
		t.Error("the deployed agent reads the helper-only public origin")
	}
}

// The prerequisite has one home, in the rulebook. It has to reach both places an
// author reads: the instructions they follow and the report they can inspect.
func TestUS3_PrerequisiteReachesReadmeAndReport(t *testing.T) {
	artifact := dailyCarrierArtifact(t, "twilio", true)
	readme := artifactFile(t, artifact, "README.md")
	for _, want := range []string{
		"Account prerequisites", "daily_dialout", "dial-out",
		"https://docs.pipecat.ai/pipecat-cloud/guides/telephony/daily-dial-out", "2026-08-12",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md missing %q", want)
		}
	}
	// International dial-out is granted separately per domain, which is the part
	// an author discovers on the first cross-border call otherwise.
	if !strings.Contains(readme, "international") {
		t.Error("README.md does not mention that international dial-out is enabled separately")
	}
	report := artifactFile(t, artifact, "compile-report.json")
	if !strings.Contains(report, `"route_prerequisites"`) || !strings.Contains(report, "daily_dialout") {
		t.Errorf("compile-report.json does not record the route prerequisite:\n%s", report)
	}
}

// A prerequisite that always prints is a banner. Compiled without anything that
// dials out, the project must carry no prerequisite text at all.
func TestUS3_NoPrerequisiteWithoutTheCapability(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	// The plain web package places no calls, so it must carry no dial-out
	// prerequisite text.
	configured := pkg.Targets["pipecat"]
	configured.Connection = ""
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, agent.Targets["pipecat"], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"README.md", "compile-report.json"} {
		content := artifactFile(t, artifact, path)
		for _, forbidden := range []string{"daily_dialout", "Account prerequisites", "route_prerequisites"} {
			if strings.Contains(content, forbidden) {
				t.Errorf("%s names %q for a package that never dials out", path, forbidden)
			}
		}
	}
}

// FR-011: the transfer destination joins the bot's startup check, so a missing
// value fails by name at boot instead of arriving as a call nobody answers.
// Helper-only deployment credentials have their own startup check.
func TestUS3_TransferDestinationIsInTheStartupCheck(t *testing.T) {
	// The fixture uses an environment name because a committed phone number would
	// be a number nobody answers.
	pkg, err := spec.Load(filepath.Join("..", "testdata", "daily_carrier"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, agent.Targets["pipecat"], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	env := artifactFile(t, artifact, ".env.example")
	for _, content := range []string{bot, env} {
		if !strings.Contains(content, "BILLING_PHONE_NUMBER") {
			t.Error("transfer destination is missing from a runtime environment surface")
		}
	}
	// The destination reaches the emitted code as an env read, never as a value.
	if !strings.Contains(bot, "BILLING_PHONE_NUMBER") {
		t.Error("bot.py does not read the transfer destination from the environment")
	}
	if strings.Contains(bot, "+1415") {
		t.Error("bot.py contains a dialable literal; destinations are env names only")
	}
}

func pipecatArtifactWithRegion(t *testing.T, region string) Artifact {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	configured := pkg.Targets["pipecat"]
	if region != "" {
		configured.DeploymentRegion = spec.Regions{region}
	}
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, agent.Targets["pipecat"], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

// FR-013 / contracts invariant 6: one declared region reaches every emitted
// reference. The agent and its credential store have to land in the same region,
// because a store in the wrong one cannot be read by the agent and the failure
// names neither side.
func TestUS4_OneRegionReachesEveryReference(t *testing.T) {
	const region = "eu-central"
	artifact := pipecatArtifactWithRegion(t, region)
	manifest := artifactFile(t, artifact, "pcc-deploy.toml")
	if !strings.Contains(manifest, `region = "`+region+`"`) {
		t.Errorf("pcc-deploy.toml does not carry the declared region:\n%s", manifest)
	}
	readme := artifactFile(t, artifact, "README.md")
	if !strings.Contains(readme, "secrets set") || !strings.Contains(readme, "--region "+region) {
		t.Errorf("the credential-store command does not carry the declared region:\n%s", readme)
	}
	// No other region may appear anywhere: a second one would mean a second
	// authored value, which is the failure this invariant exists to prevent.
	for _, other := range []string{"us-west", "us-east", "ap-south"} {
		if strings.Contains(manifest, other) {
			t.Errorf("pcc-deploy.toml names %q as well as the declared region", other)
		}
	}
}

// The absent case, which is the one that silently misconfigures. Two facts, not
// one, because they fail independently: which region the agent goes to, and that
// the credential store follows the same default.
func TestUS4_NoRegionStatesTheDefaultForBothSides(t *testing.T) {
	artifact := pipecatArtifactWithRegion(t, "")
	if manifest := artifactFile(t, artifact, "pcc-deploy.toml"); strings.Contains(manifest, "region") {
		t.Errorf("pcc-deploy.toml invents a region when the package declares none:\n%s", manifest)
	}
	readme := artifactFile(t, artifact, "README.md")
	if !strings.Contains(readme, "us-west") {
		t.Error("the instructions do not say which region applies by default")
	}
	if !strings.Contains(readme, "secret set") || !strings.Contains(readme, "wrong region cannot be read") {
		t.Errorf("the instructions do not say the credential store follows the same default:\n%s", readme)
	}
}

// Principle II: a value unmute forwards without checking has to be readable back
// out, so the region appears in the report whether or not the platform likes it.
func TestUS4_ForwardedRegionIsInTheReport(t *testing.T) {
	report := artifactFile(t, pipecatArtifactWithRegion(t, "ap-south"), "compile-report.json")
	if !strings.Contains(report, `"deployment_regions"`) || !strings.Contains(report, "ap-south") {
		t.Errorf("compile-report.json does not record the forwarded region:\n%s", report)
	}
	absent := artifactFile(t, pipecatArtifactWithRegion(t, ""), "compile-report.json")
	if strings.Contains(absent, "deployment_regions") {
		t.Errorf("compile-report.json reports a region the package never declared:\n%s", absent)
	}
}

// By name, in order, because a package may declare two targets on one provider:
// salon-concierge puts Pipecat on the media stream and on a trunk. Ranging over
// the map returned whichever one Go felt like, so a caller asking for "the
// Pipecat target" of such a package was a coin flip that would land the day the
// two stopped agreeing.
func targetByProvider(t *testing.T, agent *ir.Agent, provider ir.Provider) ir.Target {
	t.Helper()
	for _, name := range slices.Sorted(maps.Keys(agent.Targets)) {
		if resolved := agent.Targets[name]; resolved.Provider == provider {
			return resolved
		}
	}
	t.Fatalf("no target for provider %q", provider)
	return ir.Target{}
}

// setConnectionRoute declares a route in a connection, which is where a route
// lives: a target names one connection and says nothing else about how a call
// reaches it (spec FR-001). Tests that used to set target.Transport and
// target.Carrier call this instead, and keep setting target.Connection.
//
// The connection need not already exist. A connection with a route and no
// `environment:` block is legal, for the routes that need no credentials.
func setConnectionRoute(pkg *spec.Package, connection, transport, carrier string) {
	if pkg.Connections == nil {
		pkg.Connections = map[string]spec.Connection{}
	}
	conn := pkg.Connections[connection]
	conn.Transport, conn.Carrier = transport, carrier
	pkg.Connections[connection] = conn
}

func enablePackageTelephony(pkg *spec.Package) {
	inbound, outbound := true, false
	pkg.Agent.Channels["phone"] = spec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
		RequiredControls: []string{"hangup"},
	}
}

func addColdHumanTransfer(pkg *spec.Package) {
	if pkg.Agent.Escalations == nil {
		pkg.Agent.Escalations = map[string]spec.Escalation{}
	}
	pkg.Agent.Escalations["to_human"] = spec.Escalation{
		Cold: &spec.ColdTransfer{Destination: "billing_line"},
	}
	if pkg.Agent.Destinations == nil {
		pkg.Agent.Destinations = map[string]string{}
	}
	pkg.Agent.Destinations["billing_line"] = "BILLING_PHONE_NUMBER"
	billing := pkg.Agent.Agents["billing"]
	billing.Escalations = append(billing.Escalations, "to_human")
	pkg.Agent.Agents["billing"] = billing
}

// TestV14_ActivationGatedOnPipelineStart (driver-pipecat V14, B8): the entry
// agent's activation must not race main's StartFrame — frames pushed into
// BusBridge before it are dropped (greeting lost, tools never registered).
// The generated Daily SIP bot gates on_client_connected on an asyncio.Event set
// by main's on_pipeline_started handler.
func TestV14_ActivationGatedOnPipelineStart(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "daily_carrier"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(agent, agent.Targets["pipecat"], target.Default())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"runner_ready = asyncio.Event()",
		"pipeline_started = asyncio.Event()",
		"worker_start_error = None",
		"entry_started = False",
		"nonlocal entry_started",
		"if entry_started or worker_start_error is not None:",
		`@runner.event_handler("on_ready")`,
		"runner_ready.set()",
		`@main.event_handler("on_pipeline_started")`,
		"pipeline_started.set()",
		"if worker_start_error is not None:",
		"raise worker_start_error",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q", want)
		}
	}
	// Entry activation is centralised in the activate_entry helper: the web path
	// awaits pipeline_started before calling it, the console path calls it from
	// on_pipeline_started (post-start by definition). Assert the client-connected
	// handler waits before invoking the helper — refactor-stable, unlike an
	// absolute source order (agent-transfer handoffs also call activate_worker).
	if !strings.Contains(bot, "async def activate_entry():") {
		t.Errorf("bot.py missing activate_entry helper (entry activation must be centralised)")
	}
	started := strings.Index(bot, `@main.event_handler("on_pipeline_started")`)
	if started < 0 {
		t.Fatal("bot.py missing on_pipeline_started handler")
	}
	startedBlock := bot[started:]
	wait := strings.Index(startedBlock, "await runner_ready.wait()")
	register := strings.Index(startedBlock, "await runner.add_workers(*agents)")
	failure := strings.Index(startedBlock, "except Exception as error:")
	store := strings.Index(startedBlock, "worker_start_error = error")
	cancel := strings.Index(startedBlock, `await runner.cancel(reason="agent worker startup failed")`)
	ready := strings.LastIndex(startedBlock, "pipeline_started.set()")
	if wait < 0 || register < wait || ready < register {
		t.Errorf("on_pipeline_started must wait for the runner, register specialists, then open the activation gate (wait=%d register=%d ready=%d)", wait, register, ready)
	}
	if failure < register || store < failure || cancel < store || strings.Count(startedBlock, "pipeline_started.set()") < 2 {
		t.Errorf("specialist startup failure must be stored, release waiters, and cancel the runner (register=%d failure=%d store=%d cancel=%d)", register, failure, store, cancel)
	}
	if !strings.Contains(bot, "await runner.add_workers(main)") {
		t.Error("runner must initially register only the main pipeline worker")
	}
	if strings.Contains(bot, "await runner.add_workers(main, *agents)") {
		t.Error("specialists must not be registered before the main pipeline starts")
	}
	conn := strings.Index(bot, "async def on_client_connected(")
	if conn == -1 {
		t.Fatalf("bot.py missing on_client_connected handler")
	}
	block := bot[conn:]
	gate := strings.Index(block, "await pipeline_started.wait()")
	call := strings.Index(block, "await activate_entry()")
	if gate == -1 || call == -1 || gate > call {
		t.Errorf("on_client_connected must await pipeline_started before activate_entry (gate=%d, call=%d)", gate, call)
	}
}

// TestPipecatWebWaitsForRTVIClientReady (SPEC V2): web agents activate and
// greet from the PipelineWorker's RTVI readiness event, never the earlier
// transport connection event.
func TestPipecatWebWaitsForRTVIClientReady(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	delete(agent.Channels, "phone")
	delete(agent.Controls, "to_human")
	billing := agent.Agents["billing"]
	billing.Tools = []string{"get_invoice"}
	agent.Agents["billing"] = billing
	tgt := targetByProvider(t, agent, ir.ProviderPipecat)
	tgt.Transport = ""
	artifact, err := Generate(agent, tgt, target.Default())
	if err != nil {
		t.Fatal(err)
	}
	bot := artifactFile(t, artifact, "bot.py")

	if strings.Contains(bot, `@transport.event_handler("on_client_connected")`) {
		t.Error("web bot must not greet from on_client_connected")
	}
	ready := strings.Index(bot, `@main.rtvi.event_handler("on_client_ready")`)
	if ready == -1 {
		t.Fatal("web bot missing main.rtvi on_client_ready handler")
	}
	block := bot[ready:]
	gate := strings.Index(block, "await pipeline_started.wait()")
	call := strings.Index(block, "await activate_entry()")
	if gate == -1 || call == -1 || gate > call {
		t.Errorf("on_client_ready must await pipeline_started before activate_entry (gate=%d, call=%d)", gate, call)
	}
}

// T5: Daily's sip_call_transfer can report failure as a return value or raise.
// Both paths must resolve the per-call claim before applying on_unavailable;
// otherwise every later request replays an in_progress result forever.
func TestV1_DailyColdTransferHandlesPrimitiveFailures(t *testing.T) {
	build := func(onUnavailable ir.OnUnavailable) string {
		pkg, err := spec.Load(filepath.Join("..", "testdata", "daily_carrier"))
		if err != nil {
			t.Fatal(err)
		}
		agent, err := ir.Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		human := agent.Controls["send_to_billing"].(*ir.HumanTransfer)
		human.OnUnavailable = onUnavailable
		artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return artifactFile(t, artifact, "bot.py")
	}

	returned := build(ir.OnUnavailableReturn)
	for _, want := range []string{
		`@_direct_tool(cancel_on_interruption=False)
    async def send_to_billing`,
		// The primitive is a Daily transport method, so the tool narrows to that
		// transport first; a browser or console session gets a named failure
		// rather than an AttributeError.
		`if not isinstance(transport, DailyTransport) or not self.call_context.get("_daily_sip_session"):`,
		`call_context["_daily_sip_session"] = carrier_call is not None`,
		"this session is not a phone call, so it cannot be transferred",
		`self.call_context.pop("_transfer_result", None)`,
		"try:\n            error = await transport.sip_call_transfer(",
		"except Exception as exc:\n            error = exc",
		"if error is not None:",
		"Tell the caller and keep helping them.",
	} {
		if !strings.Contains(returned, want) {
			t.Errorf("return_to_caller bot.py missing %q", want)
		}
	}
	if strings.Contains(returned, "the call is ending") {
		t.Error("return_to_caller bot.py must not end the call on a failed transfer")
	}
	noPhoneAt := strings.Index(returned, `if not isinstance(transport, DailyTransport) or not self.call_context.get("_daily_sip_session"):`)
	claimAt := strings.Index(returned, `self.call_context["_transfer_result"] = {"in_progress":`)
	announceAt := strings.Index(returned, "Tell the caller you are transferring them to a colleague now")
	primitiveAt := strings.Index(returned, "await transport.sip_call_transfer(")
	if noPhoneAt < 0 || claimAt < noPhoneAt || announceAt < claimAt || primitiveAt < announceAt {
		t.Errorf("Daily transfer order = preflight %d, claim %d, announce %d, primitive %d", noPhoneAt, claimAt, announceAt, primitiveAt)
	}

	hangup := build(ir.OnUnavailableHangup)
	for _, want := range []string{
		"if error is not None:",
		"nobody can take the call right now",
		"The transfer could not be completed; the call is ending.",
		"failure_error = None",
		`logger.exception("failed to end call after transfer failure")`,
		"await params.llm.push_frame(EndFrame())",
	} {
		if !strings.Contains(hangup, want) {
			t.Errorf("hangup bot.py missing %q", want)
		}
	}
	terminalAt := strings.Index(hangup, `self.call_context["_transfer_result"] = {"failed": "The transfer could not be completed; the call is ending."}`)
	if terminalAt < 0 {
		t.Fatal("hangup bot.py has no terminal failure assignment")
	}
	failureBody := hangup[terminalAt:]
	goodbyeAt := strings.Index(failureBody, "Tell the caller nobody can take the call right now")
	endAt := strings.Index(failureBody, "await params.llm.push_frame(EndFrame())")
	if goodbyeAt < 0 || endAt < goodbyeAt {
		t.Error("hangup failure does not record terminal state, announce, then attempt EndFrame")
	}
}

// N32: several regions is a Pipecat gate, not a Pipecat feature. The agreement
// test alone would also pass if both sides were wrong together, so name the row.
func TestPipecatDoesNotClaimMultiRegion(t *testing.T) {
	if pipecatEmittedFields[target.FieldDeploymentMultiRegion] {
		t.Error("pipecatEmittedFields claims deployment_region.multiple, but one Pipecat agent deploys to one region")
	}
	if tag := target.Default().Capability(target.FieldDeploymentMultiRegion, target.Pipecat).Tag; tag != target.Gated {
		t.Errorf("deployment_region.multiple on pipecat = %q, want gated", tag)
	}
}

// TestPipecatV1TaskToolAnnounceQueuesFrameFromFlowManager is the gate on the
// scope half of tool announcements. An agent tool reaches the pipeline through
// FunctionCallParams; a task tool is a flows handler and reaches it through
// FlowManager.worker, which is the documented seam for queueing a frame from
// inside a handler (verified against pipecat-ai 1.8.0, the pinned version, where
// flows ships as pipecat.flows rather than the standalone pipecat_flows).
//
// This case exists because the capability table used to deny Pipecat here with
// "cannot announce a tool listed on a task", which was a gap in this compiler
// written as a limit of the provider. It blocked a working feature, and nothing
// failed when the claim was wrong. This is what fails now.
func TestPipecatV1TaskToolAnnounceQueuesFrameFromFlowManager(t *testing.T) {
	load := func(t *testing.T, announce string) string {
		t.Helper()
		pkg, err := spec.Load(filepath.Join("..", "..", "examples", "salon-concierge"))
		if err != nil {
			t.Fatal(err)
		}
		agent, err := ir.Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		// check_availability is listed on the booking task, never on an agent,
		// so it can only be reached through the flows-handler path.
		tool := agent.Tools["check_availability"]
		tool.Announce = announce
		agent.Tools["check_availability"] = tool
		artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
		if err != nil {
			t.Fatalf("generate with announce %q: %v", announce, err)
		}
		return artifactFile(t, artifact, "bot.py")
	}

	bot := load(t, "Let me check the calendar.")
	want := `await flow_manager.worker.queue_frame(TTSSpeakFrame("Let me check the calendar."))`
	if !strings.Contains(bot, want) {
		t.Errorf("a task tool must announce through FlowManager.worker: want %s", want)
	}
	if !regexp.MustCompile(`(?m)^from pipecat\.frames\.frames import .*TTSSpeakFrame`).MatchString(bot) {
		t.Error("an announcing task tool must import TTSSpeakFrame")
	}
	// The line is queued, never awaited for playout: the whole point is that
	// speech starts while the handler body runs.
	handler := bot[strings.Index(bot, "async def _flow_tool_check_availability"):]
	handler = handler[:strings.Index(handler, "\n\n\nasync def ")+1]
	for _, absent := range []string{"BotStoppedSpeakingFrame", "asyncio.wait_for", "_handoff_finished"} {
		if strings.Contains(handler, absent) {
			t.Errorf("the flows-handler announcement must not wait for playout, found %q:\n%s", absent, handler)
		}
	}

	// Unset must leave this handler exactly as it was, so no existing package
	// gains a frame it never asked for. Scoped to this tool's own handler:
	// salon-concierge announces on several other task tools, so the package as a
	// whole still queues frames.
	silent := load(t, "")
	quiet := silent[strings.Index(silent, "async def _flow_tool_check_availability"):]
	quiet = quiet[:strings.Index(quiet, "\n\n\nasync def ")+1]
	if strings.Contains(quiet, "queue_frame(TTSSpeakFrame") {
		t.Errorf("a task tool that announces nothing must queue no frame:\n%s", quiet)
	}
}

// TestPipecatV1ToolAnnounceQueuesFrameWithoutWaiting: an announcing tool carries
// the line on the decorator that already wraps every direct tool, the wrapper
// queues it as a TTSSpeakFrame before the handler body runs, and nothing waits
// for playout (FR-008, FR-009). The interruption argument still composes with it
// in one list, and a package that announces nothing keeps today's output exactly
// (FR-010).
func TestPipecatV1ToolAnnounceQueuesFrameWithoutWaiting(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	// A user-first greeting so the only reason to import TTSSpeakFrame is the
	// announcement itself.
	agent.Conversation.Greeting = &ir.Greeting{SpeaksFirst: ir.SpeaksFirstUser}
	silent, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate without announce: %v", err)
	}
	before := artifactFile(t, silent, "bot.py")
	if strings.Contains(before, "announce") {
		t.Errorf("a package that announces nothing must not mention announce at all")
	}
	if strings.Contains(before, "TTSSpeakFrame") {
		t.Errorf("a package that announces nothing must not import TTSSpeakFrame")
	}

	webhook := agent.Tools["get_invoice"]
	webhook.Announce = "Let me pull that invoice up."
	agent.Tools["get_invoice"] = webhook
	lookup := agent.Tools["lookup_customer"]
	lookup.Announce = "Give me one second to find you."
	lookup.Interruption = ir.ToolContinue
	agent.Tools["lookup_customer"] = lookup

	artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"def _direct_tool(fn=None, *, cancel_on_interruption=True, timeout_secs=None, announce=None):",
		"await params.llm.push_frame(TTSSpeakFrame(announce))",
		`    @_direct_tool(announce="Let me pull that invoice up.")`,
		`    @_direct_tool(cancel_on_interruption=False, announce="Give me one second to find you.")`,
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("announcing package missing %q", want)
		}
	}
	if !regexp.MustCompile(`(?m)^from pipecat\.frames\.frames import .*TTSSpeakFrame`).MatchString(bot) {
		t.Error("an announcing tool must import TTSSpeakFrame")
	}
	// The announcement is queued, never waited on: no handle await, and none of
	// the transfer announcement's playout-wait shape.
	wrapper := pipecatDirectToolBody(t, bot)
	pushAt := strings.Index(wrapper, "TTSSpeakFrame(announce)")
	guardAt := strings.Index(wrapper, "unexpected = sorted(")
	if pushAt < 0 || guardAt < 0 {
		t.Fatalf("_direct_tool lost its announcement or its argument guard:\n%s", wrapper)
	}
	if guardAt >= pushAt {
		t.Errorf("the announcement must come after the argument check, so a rejected call stays silent:\n%s", wrapper)
	}
	for _, absent := range []string{
		"_announce_handoff",
		"_handoff_finished",
		"asyncio.wait_for",
		"BotStoppedSpeakingFrame",
	} {
		if strings.Contains(wrapper, absent) {
			t.Errorf("_direct_tool must not wait for playout, found %q:\n%s", absent, wrapper)
		}
	}
}

// TestPipecatV1ToolAnnounceOnInlinePath: the module-level emission site a minimal
// single-agent package uses carries the same decorator, so both call sites are
// held by a test rather than by one of them happening to be exercised.
func TestPipecatV1ToolAnnounceOnInlinePath(t *testing.T) {
	pkg, err := spec.Load(examplePackagePath("simple-prompt"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Tracing = nil // the inline path is scoped to no-tracing
	tool := agent.Tools["lookup_customer"]
	tool.Announce = "Let me look that up."
	agent.Tools["lookup_customer"] = tool

	bot := artifactFile(t, mustGeneratePipecatInline(t, agent), "bot.py")
	if !strings.Contains(bot, `@_direct_tool(announce="Let me look that up.")`) {
		t.Error("the inline emission site lost the announcement")
	}
	if !strings.Contains(bot, "async def lookup_customer(params: FunctionCallParams") {
		t.Error("expected the module-level inline tool, not the method form")
	}
}

// pipecatDirectToolBody returns the generated _direct_tool wrapper, so an
// assertion about it cannot be satisfied by a line elsewhere in bot.py.
func pipecatDirectToolBody(t *testing.T, bot string) string {
	t.Helper()
	start := strings.Index(bot, "def _direct_tool(")
	if start < 0 {
		t.Fatal("_direct_tool wrapper not emitted")
	}
	body := bot[start:]
	if end := strings.Index(body[1:], "\n\ndef "); end >= 0 {
		body = body[:end+1]
	}
	return body
}

// TestPipecatV1RefusedDelegateGivesTheModelItsTurnBack exists for one line, and
// nothing else.
//
// The success path resolves the delegate's tool call with run_llm=False on
// purpose: the flow's first node is the sole responder, and a second completion
// makes the caller hear the opening line twice. A refused delegate starts no
// flow, so there is no first node and nothing else in the process is going to
// speak. Copying run_llm=False onto the refusal leaves a live call in silence,
// with no exception, no error, and nothing in the trace to find it by.
//
// That failure is invisible to every other test in this file, which is why this
// one is separate from them and is not allowed to ride on a golden diff.
func TestPipecatV1RefusedDelegateGivesTheModelItsTurnBack(t *testing.T) {
	bot := emitFor(t, guardedFixture(t), ir.ProviderPipecat, "bot.py")

	start := strings.Index(bot, "    async def manage_booking(self, params: FunctionCallParams):")
	if start < 0 {
		t.Fatal("bot.py has no manage_booking method")
	}
	method := bot[start:]
	if end := strings.Index(method[1:], "\n    async def "); end >= 0 {
		method = method[:end+1]
	}

	refusalAt := strings.Index(method, `{"refused": _prerequisite_refusal(`)
	flowStartAt := strings.Index(method, `properties=FunctionCallResultProperties(run_llm=False)`)
	if refusalAt < 0 || flowStartAt < 0 {
		t.Fatalf("expected both a refusal path and a flow-start path:\n%s", method)
	}
	if refusalAt >= flowStartAt {
		t.Fatalf("the refusal must come before the flow-start resolution:\n%s", method)
	}

	// Read the refusal branch itself: from `if _unmet:` to the `return` that
	// leaves it. Comparing against the flow-start offset instead would sweep in
	// the comment that explains run_llm=False, and the test would fail on prose.
	branchAt := strings.Index(method, "        if _unmet:")
	if branchAt < 0 {
		t.Fatalf("no refusal branch:\n%s", method)
	}
	branch := method[branchAt:]
	if end := strings.Index(branch, "\n            return\n"); end >= 0 {
		branch = branch[:end]
	} else {
		t.Fatalf("the refusal branch must return:\n%s", branch)
	}

	// The refusal must resolve its call, or the model never gets its turn back.
	if !strings.Contains(branch, "await params.result_callback(") {
		t.Errorf("the refusal must resolve the tool call:\n%s", branch)
	}

	// And it must resolve it WITHOUT run_llm=False.
	if strings.Contains(branch, "run_llm=False") && !strings.Contains(branch, "# ") {
		t.Error("the refusal path resolves with run_llm=False. A refused delegate starts no flow, so nothing else will speak and the call goes silent with nothing in the trace")
	}
	for _, line := range strings.Split(branch, "\n") {
		code := line
		if hash := strings.Index(code, "#"); hash >= 0 {
			code = code[:hash]
		}
		if strings.Contains(code, "run_llm=False") {
			t.Errorf("the refusal path resolves with run_llm=False, which leaves a live call silent with nothing in the trace: %s", line)
		}
	}
	_ = refusalAt

	// The flow-start path must keep it, or the caller hears the opening line
	// twice. Both halves of the asymmetry, held together.
	if !strings.Contains(method[flowStartAt-400:flowStartAt], "run_llm=False: the flow's first node") {
		t.Error("the flow-start path must keep run_llm=False and say why")
	}
}

// TestPipecatV1DelegateRequiresGuard is the Pipecat side of the same list the
// LiveKit test holds: guard before any work, counter, bound, reset, both log
// lines with names only, and the forward declaration on the description.
func TestPipecatV1DelegateRequiresGuard(t *testing.T) {
	bot := emitFor(t, guardedFixture(t), ir.ProviderPipecat, "bot.py")

	start := strings.Index(bot, "    async def manage_booking(self, params: FunctionCallParams):")
	if start < 0 {
		t.Fatal("bot.py has no manage_booking method")
	}
	method := bot[start:]
	if end := strings.Index(method[1:], "\n    async def "); end >= 0 {
		method = method[:end+1]
	}

	guardAt := strings.Index(method, `_unmet = _unmet_prerequisites(self.state, ["customer_id"])`)
	resultsAt := strings.Index(method, "_results = {}")
	flowAt := strings.Index(method, "flow = FlowManager(")
	if guardAt < 0 || resultsAt < 0 || flowAt < 0 || guardAt >= resultsAt || resultsAt >= flowAt {
		t.Fatalf("the guard must run before the results dict and before the flow is built:\n%s", method)
	}

	for _, want := range []string{
		`_tries = _prerequisite_refusals.get("manage_booking", 0) + 1`,
		"_at_limit = _tries >= _PREREQUISITE_LIMIT",
		`_prerequisite_refusals["manage_booking"] = 0`,
	} {
		if !strings.Contains(method, want) {
			t.Errorf("emitted guard missing %q:\n%s", want, method)
		}
	}

	resetAt := strings.Index(method, `_prerequisite_refusals["manage_booking"] = 0`)
	if resetAt < guardAt || resetAt > resultsAt {
		t.Errorf("the counter must reset where the step starts, not in the refusal branch:\n%s", method)
	}

	doc := method[:guardAt]
	for _, want := range []string{"customer_id", "verify_customer"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the function description must name %q so the model collects it earlier:\n%s", want, doc)
		}
	}

	assertGuardLogsNamesOnly(t, method, "pipecat")
}

// pipecatHistoryBot compiles the history fixture and returns its bot.py.
//
// The fixture exists because no other package in the tree authors a non-`full`
// history on Pipecat, so before it there was nothing to compile as proof that
// the driver reads ir.TaskContext.History at all.
func pipecatHistoryBot(t *testing.T) string {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "history_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate history fixture: %v", err)
	}
	return artifactFile(t, artifact, "bot.py")
}

// Each `context.history` value lowers to its own message list at the task
// entry, and `full` lowers to nothing at all (FR-007, FR-008, FR-009, FR-013).
//
// Pipecat keeps one LLMContext for the whole call and hands every worker the
// same object, so shaping means replacing that object's message list rather
// than handing over a copy. LLMContext at pipecat-ai 1.8.0 has no copy(), no
// truncate() and no exclusion filter, so this is plain list work and the
// framework provides no safety.
func TestPipecatLowersEveryTaskHistoryValue(t *testing.T) {
	bot := pipecatHistoryBot(t)

	// reset drives the ContextStrategyConfig line the template already has for
	// context_scope: isolated. On a node transition RESET replaces the message
	// list with that node's own task_messages, which is what reset means, so
	// there is no new emitted code path.
	if !strings.Contains(bot, "context_strategy=ContextStrategyConfig(strategy=ContextStrategy.RESET)") {
		t.Error("history: reset emits no ContextStrategy.RESET on the task node")
	}
	// messages keeps what the caller and the agent said out loud. Tool records
	// go, which is what LiveKit's `messages` does: .messages() returns message
	// items only, so a tool result carrying a value is not in it either.
	if !containsCollapsed(bot, `self.context.set_messages([m for m in self.context.get_messages() if m.get("role") in ("user", "assistant")])`) {
		t.Error("history: messages emits no user/assistant filter at the task entry")
	}
	// last_n bounds the window by the authored max_messages, through the helper
	// that drops a leading orphan.
	if !containsCollapsed(bot, "self.context.set_messages(_last_n(self.context.get_messages(), 6))") {
		t.Error("history: last_n does not bound the task entry by the authored max_messages")
	}
	// full is the control, and it is in this same package: a value that shapes
	// nothing must emit nothing even where its neighbours emit something. The
	// entry is the span from the owner snapshot to the node initialize; the
	// finish path further down restores and legitimately calls set_messages.
	entry := pipecatMethodBody(t, bot, "self._take_message_snapshot = (", "await flow.initialize(")
	if strings.Contains(entry, "set_messages(") {
		t.Errorf("history: full shapes the context, and the shared LLMContext is already the whole history:\n%s", entry)
	}

	// The emitted module has to parse. The shaping sites add a helper, two call
	// sites and an import gated on two conditions, and a missed import is a
	// NameError at worker start rather than anything a string assertion sees.
	// Skipped where python3 is absent, so the default suite still needs none.
	if _, err := exec.LookPath("python3"); err == nil {
		path := filepath.Join(t.TempDir(), "bot.py")
		if err := os.WriteFile(path, []byte(bot), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("python3", "-m", "py_compile", path).CombinedOutput(); err != nil {
			t.Fatalf("the shaped bot.py is not valid Python:\n%s", out)
		}
	}
}

// The last_n helper cuts at the front and then drops what the cut orphaned
// (FR-009, SC-005).
//
// This is the load-bearing assertion of the story. A `tool` message with no
// matching `tool_call_id` is not a cosmetic problem: it is a request the
// provider rejects, mid-call, on a step that worked in every test that did not
// happen to leave one. The front is the only cut being made, so the tail is
// intact and the sole orphan possible is a leading tool result whose assistant
// call fell outside the window. LiveKit gets this free from truncate().
func TestPipecatLastNLeavesNoOrphanedToolResult(t *testing.T) {
	bot := pipecatHistoryBot(t)
	if !strings.Contains(bot, "def _last_n(") {
		t.Fatal("no _last_n helper is emitted, so nothing bounds a last_n window")
	}
	helper := pipecatMethodBody(t, bot, "def _last_n(", "\n\n\n")
	for _, want := range []string{
		`messages[-limit:]`,
		`while window and window[0].get("role") == "tool":`,
	} {
		if !containsCollapsed(helper, want) {
			t.Errorf("the _last_n helper is missing %q:\n%s", want, helper)
		}
	}
	sliceAt := strings.Index(helper, "messages[-limit:]")
	dropAt := strings.Index(helper, `window[0].get("role") == "tool"`)
	if sliceAt < 0 || dropAt < sliceAt {
		t.Errorf("the orphan drop must follow the cut that can create one:\n%s", helper)
	}
}

// A non-`full` value on a handoff shapes the receiving worker's context, and
// does it before the worker is activated (FR-019, FR-020).
//
// Not an extra: the capability lookup takes a value and a provider and nothing
// else, and the same validateContext runs for a task and for a transfer. So
// the moment `reset` stops being refused on Pipecat it is legal on a Pipecat
// handoff too. A handoff hands the receiving worker the same shared
// LLMContext plus one developer message, so leaving this path alone would mean
// accepting a value and ignoring it, which is the silent downgrade Principle
// II forbids by name.
func TestPipecatShapesAHandoffByTheSameValues(t *testing.T) {
	bot := pipecatHistoryBot(t)
	handoff := pipecatMethodBody(t, bot, "async def to_billing(self, params: FunctionCallParams):", "\n    @_direct_tool")
	shapeAt := strings.Index(handoff, "self.context.set_messages([])")
	activateAt := strings.Index(handoff, "await self.activate_worker(")
	if shapeAt < 0 {
		t.Fatalf("history: reset on a handoff shapes nothing, so the receiver gets the whole call:\n%s", handoff)
	}
	if activateAt < 0 || shapeAt > activateAt {
		t.Errorf("the handoff must shape the context before it activates the receiver:\n%s", handoff)
	}
}

// The owner's context comes back whatever the step saw (FR-010).
//
// The snapshot is taken before the shaping, so the finish path restores the
// full owner context even from a step that ran on an empty one. Nothing about
// the finish path changes, and that is the claim: only the typed result crosses
// back.
func TestPipecatRestoresTheOwnerContextAtEveryHistoryValue(t *testing.T) {
	bot := pipecatHistoryBot(t)
	for _, method := range []string{"_confirm_number", "_read_back", "_sort_invoice", "_take_message"} {
		snapshot := method + "_snapshot = (copy.deepcopy(self.context.get_messages()), self.context.tools)"
		if !strings.Contains(bot, "self."+snapshot) {
			t.Errorf("%s takes no owner snapshot, so its finish has nothing to restore", method)
		}
		if !strings.Contains(bot, "messages, tools = self."+method+"_snapshot") {
			t.Errorf("%s never restores the owner's messages and tools", method)
		}
	}
	// The snapshot has to precede the shaping, or the owner's context is
	// restored from whatever the step was given rather than from what it had.
	// read_back is the case that can get this wrong: it is the one whose entry
	// both snapshots and shapes.
	entry := pipecatMethodBody(t, bot, "async def read_back(self, params: FunctionCallParams):", "def _read_back_node_read_back")
	snapshotAt := strings.Index(entry, "self._read_back_snapshot = (")
	shapeAt := strings.Index(entry, "self.context.set_messages(")
	if snapshotAt < 0 || shapeAt < 0 || snapshotAt > shapeAt {
		t.Errorf("the owner snapshot must be taken before the step's context is shaped: snapshot=%d shape=%d\n%s", snapshotAt, shapeAt, entry)
	}
}

// containsCollapsed asks whether the emitted module contains a fragment, with
// every run of whitespace treated as one space.
//
// The emitted bot.py goes through a Python formatter, so a one-line template
// expression can arrive wrapped over five lines. Matching the collapsed form
// keeps an assertion about behaviour from failing on a change of line width.
func containsCollapsed(text, want string) bool {
	return strings.Contains(strings.Join(strings.Fields(text), " "), strings.Join(strings.Fields(want), " "))
}

// pipecatMethodBody returns the emitted text from one marker up to the next, so
// an assertion can say "inside this method" rather than "somewhere in the file".
func pipecatMethodBody(t *testing.T, bot, from, to string) string {
	t.Helper()
	start := strings.Index(bot, from)
	if start < 0 {
		t.Fatalf("emitted bot.py has no %q", from)
	}
	rest := bot[start:]
	if end := strings.Index(rest[len(from):], to); end >= 0 {
		return rest[:len(from)+end]
	}
	return rest
}

// A task group's `context_scope` keeps governing its member steps, and a
// member's own `history:` is not consulted (FR-014).
//
// This is behaviour at HEAD that the change must not disturb, and the way it
// holds is structural: the driver reads a task's context only on a single-task
// delegate, which is the same split LiveKit makes. A group step reaching for
// its own `history:` would mean two settings deciding one thing, with the
// group's the one the author wrote down.
func TestPipecatTaskGroupStillGovernsItsMembersContext(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	addPipecatTaskTransferFixture(agent)
	// A member asking for the shortest context there is. The group says shared,
	// so the member is ignored.
	verify := agent.Tasks["verify"]
	verify.Context = ir.TaskContext{History: ir.HistoryReset}
	agent.Tasks["verify"] = verify
	complete := agent.Tasks["complete"]
	complete.Context = ir.TaskContext{History: ir.HistoryLastN, MaxMessages: 2}
	agent.Tasks["complete"] = complete

	artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate task group: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	entry := pipecatMethodBody(t, bot, "self._run_verify_snapshot = (", "await flow.initialize(")
	if strings.Contains(entry, "set_messages(") {
		t.Errorf("a group step's own history: shaped the group's entry:\n%s", entry)
	}
	if strings.Contains(bot, "_last_n(") {
		t.Error("a group step's last_n emitted the window helper, so the member's history: was consulted")
	}
	if strings.Contains(bot, "ContextStrategy.RESET") {
		t.Error("a group step's reset emitted the RESET strategy; context_scope: shared has to win")
	}
}

// A Pipecat package where every task and handoff authors `history: full` emits
// nothing new at all (FR-013).
//
// This is what makes the change safe to land: `full` on this target is not a
// setting the driver implements, it is what one shared LLMContext per call
// already does. So the whole of `full` is the absence of a shaping call, and
// every existing Pipecat golden in the tree stays where it is. The goldens hold
// that too; this says it in one place with the reason attached.
func TestPipecatFullOnlyPackageEmitsNoShaping(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	addPipecatTaskTransferFixture(agent)
	single := &ir.Delegate{Kind: ir.ControlDelegate, Task: "complete", When: "Finish up."}
	agent.Controls["run_complete"] = single
	intake := agent.Agents["intake"]
	intake.Tools = append(intake.Tools, "run_complete")
	agent.Agents["intake"] = intake

	artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err != nil {
		t.Fatalf("generate full-only package: %v", err)
	}
	bot := artifactFile(t, artifact, "bot.py")
	if strings.Contains(bot, "def _last_n(") {
		t.Error("a full-only package emits the _last_n helper, so the gate on it is not working")
	}
	if strings.Contains(bot, "ContextStrategy") {
		t.Error("a full-only package imports or names ContextStrategy")
	}
	// The single-task delegate is the site history: full would have shaped.
	entry := pipecatMethodBody(t, bot, "self._run_complete_snapshot = (", "await flow.initialize(")
	if strings.Contains(entry, "set_messages(") {
		t.Errorf("history: full shaped a single-task delegate's entry:\n%s", entry)
	}
	handoff := pipecatMethodBody(t, bot, "async def to_billing(self, params: FunctionCallParams):", "\n    @_direct_tool")
	if strings.Contains(handoff, "set_messages(") {
		t.Errorf("history: full shaped a handoff:\n%s", handoff)
	}
}
