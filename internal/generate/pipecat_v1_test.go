package generate

import (
	"encoding/json"
	"flag"
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
		"(await client.register_tools(self.llm)).standard_tools",
		"return super().build_tools() + self._mcp_tools",
		"await agents[1].start_mcp()", // intake is the entry agent; billing sorts first
		"await agents[1].close_mcp()",
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
		"enable_tracing=tracing_enabled",
		`additional_span_attributes={"langfuse.trace.name": TRACE_NAME}`,
		"enable_agent_tracing(main, agents)",
		"flush_tracing()",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q", want)
		}
	}
	for _, want := range []string{
		"def setup_langfuse_tracing() -> bool:",
		`f"{base_url.rstrip('/')}/api/public/otel"`,
		"setup_tracing(service_name=TRACE_NAME, exporter=OTLPSpanExporter())",
		"def enable_agent_tracing(main: PipelineWorker, agents: Sequence[LLMWorker]) -> None:",
		"agent._tracing_context = main._tracing_context",
		"trace_provider.force_flush()",
	} {
		if !strings.Contains(tracing, want) {
			t.Errorf("tracing.py missing %q", want)
		}
	}
	if !strings.Contains(tracing, "if not public_key or not secret_key or not base_url:") {
		t.Error("configured tracing must reject missing credentials, including all three")
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
		"TTSService.append_to_audio_context",
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
	if !strings.Contains(bot, "PipelineParams(enable_metrics=True, enable_usage_metrics=True)") {
		t.Error("bot.py missing tracing metrics configuration")
	}
}

func TestV24PipecatStaticCheckSurface(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "simple-prompt"))
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
		`setattr(patched, "__langfuse_patch__", True)`,
		`setattr(service_decorators, "add_llm_span_attributes", patched_llm)`,
		`setattr(TTSService, "append_to_audio_context", patched_append_to_audio_context)`,
		"if not public_key or not secret_key or not base_url:",
		"def enable_agent_tracing(main: PipelineWorker, agents: Sequence[LLMWorker]) -> None:",
		"context = self._tracing_context",
		"if not self._enable_tracing or context is None:",
		"if isinstance(trace_provider, TracerProvider):",
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
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "simple-prompt"))
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
		`role_message="Ask for the caller's email, look them up, and confirm their account tier."`,
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
	if got := strings.Count(bot, "await self.queue_frame(LLMUpdateSettingsFrame("); got != 2 {
		t.Errorf("bot.py restores the owner role %d times, want 2", got)
	}
	if got := strings.Count(bot, "await self.flush_pipeline()"); got != 2 {
		t.Errorf("bot.py drains the owner role update %d times, want 2", got)
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

	// The restore is shared by return and transfer, while end needs neither the
	// restore nor its imports (V12, V28).
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
	for _, forbidden := range []string{"LLMUpdateSettingsFrame", "LLMSettings"} {
		if strings.Contains(endBot, forbidden) {
			t.Errorf("end-only task group has unused restoration import %q", forbidden)
		}
	}
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
	if !strings.Contains(endFinishBody, "self._run_triage_end_node()") {
		t.Error("then: end finish handler must return the terminal end node")
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
	requiresAt := strings.Index(body, "missing =")
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
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "simple-prompt"))
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

// TestPipecatV1MCPInline is the shape examples/mcp-example compiles to: one
// agent, no bus, so the source's tools join the LLMContext directly and the
// client's lifecycle rides run_bot rather than a worker class (N40).
func TestPipecatV1MCPInline(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "simple-prompt"))
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

	bot := artifactFile(t, mustGeneratePipecatInline(t, agent), "bot.py")
	for _, want := range []string{
		"    llm = build_appointment_desk_llm()",
		"    web_search_mcp = MCPClient(",
		"    await web_search_mcp.start()",
		"    web_search_mcp_tools = await web_search_mcp.register_tools(llm)",
		"+ web_search_mcp_tools.standard_tools)",
		"        await web_search_mcp.close()",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("inline bot.py missing %q:\n%s", want, bot)
		}
	}
	// The LLM is built once and used in the pipeline, never built twice.
	if strings.Contains(bot, "build_appointment_desk_llm(),") {
		t.Error("the inline pipeline must reuse the llm the MCP client registered on")
	}
	if _, err := exec.LookPath("python3"); err == nil {
		f := filepath.Join(t.TempDir(), "bot.py")
		if err := os.WriteFile(f, []byte(bot), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("python3", "-m", "py_compile", f).CombinedOutput(); err != nil {
			t.Fatalf("inline bot.py with an MCP source is not valid Python:\n%s", out)
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

func TestPipecatTwilioTelephonyEmitsOnlySelectedAuthenticatedAdapter(t *testing.T) { // telephony V7-V14, V20
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	enablePackageTelephony(pkg)
	dropHumanTransfer(pkg)
	configured := pkg.Targets["pipecat"]
	configured.Connection = "primary_phone"
	setConnectionRoute(pkg, "primary_phone", "carrier-websocket", "twilio")
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	outbound := true
	phone := pkg.Agent.Channels["phone"]
	phone.Outbound = &outbound
	phone.OnVoicemail = "hangup"
	pkg.Agent.Channels["phone"] = phone
	pkg.Agent.Variables["campaign_id"] = spec.Variable{Type: "string", Source: "call_start"}
	pkg.Agent.Variables["provider_call"] = spec.Variable{Type: "string", Source: "call_id"}

	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	resolved := agent.Targets["pipecat"]
	artifact, err := GeneratePipecat(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	adapter := artifactFile(t, artifact, "telephony.py")
	for _, want := range []string{
		`RequestValidator(_env(AUTH_TOKEN_ENV))`,
		`TwilioHttpClient(timeout=CONTROL_TIMEOUT_SECS)`,
		`_validator().validate(_http_url(request.url.path), form, signature)`,
		`_validator().validate(`,
		`STATE.mark_once("inbound", call_id, SESSION_TTL_SECS)`,
		`STATE.mark_once("status", event_id, SESSION_TTL_SECS)`,
		`status_callback_event=["initiated", "ringing", "answered", "completed"]`,
		`destination, call_start = await _outbound_request(request)`,
		`except TwilioRestException as exc:`,
		`Twilio rejected the outbound call:`,
		`await handle_media(websocket, token, _validate_websocket)`,
	} {
		if !strings.Contains(adapter, want) {
			t.Errorf("telephony.py missing %q", want)
		}
	}
	shared := artifactFile(t, artifact, "telephony_shared.py")
	for _, want := range []string{
		`CONTROL_TIMEOUT_SECS = 10`,
		`SESSION_TTL_SECS = 1260`,
		`DRAIN_TIMEOUT_SECS = max(1, SESSION_TTL_SECS - 30)`,
		`app = FastAPI(lifespan=lifespan)`,
		`signal.signal(signal.SIGTERM, begin_drain)`,
		`STATE.begin_drain()`,
		`telephony forced termination after drain timeout; active_sessions={}`,
		`websocket.close(code=1012)`,
		`def _env(name: str) -> str:`,
		`def _public_url() -> str:`,
		`async def _remember(`,
		`async def _outbound_request(request: Request)`,
		`hmac.compare_digest(request.headers.get("Authorization", ""), expected)`,
		`pending = await STATE.pending(token, consume=True)`,
		`await STATE.admit(pending["session_id"])`,
		`async with asyncio.TaskGroup() as tasks:`,
		`_refresh_admission(pending["session_id"])`,
		`await asyncio.sleep(max(1, SESSION_TTL_SECS // 3))`,
		`raise RuntimeError("active telephony admission lease was lost")`,
		`if STATE.draining:`,
		`await websocket.close(code=1013)`,
		`"campaign_id": "string"`,
		`CALL_START_REQUIRED = frozenset((`,
	} {
		if !strings.Contains(shared, want) {
			t.Errorf("telephony_shared.py missing %q", want)
		}
	}
	state := artifactFile(t, artifact, "telephony_state.py")
	for _, want := range []string{"hashlib.sha256", "setex", "getdel", "ZREMRANGEBYSCORE", "ZADD", "EXPIRE", "zrem"} {
		if !strings.Contains(state, want) {
			t.Errorf("telephony_state.py missing %q", want)
		}
	}
	if strings.Index(state, "redis.call('ZADD'") > strings.Index(state, "redis.call('EXPIRE'") {
		t.Error("admission refresh must extend the Redis key lifetime after re-scoring the active session")
	}
	for _, forbidden := range []string{"from_number", "to_number", "call_start", "credential", "transcript", "audio"} {
		if strings.Contains(state, forbidden) {
			t.Errorf("telephony_state.py leaks disallowed state field %q", forbidden)
		}
	}
	for _, forbidden := range []string{"Telnyx", "Plivo", "Exotel", "audio/x-mulaw", "media.payload"} {
		if strings.Contains(adapter, forbidden) {
			t.Errorf("telephony.py contains unselected or media implementation %q", forbidden)
		}
	}
	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		`"twilio": lambda: FastAPIWebsocketParams`,
		`telephony.configure_pipecat_env()`,
		`call_context = telephony.normalized_context(runner_args)`,
		`agents = [BillingAgent(state=state, context=context, call_context=call_context), IntakeAgent(state=state, context=context, call_context=call_context)]`,
		`logger.add(sys.stderr, level=os.getenv("UNMUTE_LOG_LEVEL", "INFO").upper())`,
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q", want)
		}
	}
	if strings.Contains(bot, "STATE = State()") || strings.Contains(bot, "AGENTS = [") {
		t.Error("per-call state or workers escaped into module globals")
	}
	if strings.Contains(shared, `@app.on_event("shutdown")`) {
		t.Error("telephony_shared.py retains deprecated post-stop shutdown draining")
	}
	beginDrainAt := strings.Index(shared, "STATE.begin_drain()")
	stopServerAt := strings.Index(shared, "previous_sigterm(signum, frame)")
	if beginDrainAt < 0 || stopServerAt < 0 || beginDrainAt > stopServerAt {
		t.Error("SIGTERM must flip readiness before asking Uvicorn to stop")
	}
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if !strings.Contains(pyproject, `"twilio>=9,<10"`) {
		t.Error("pyproject.toml missing selected Twilio SDK")
	}
	report := artifactFile(t, artifact, "compile-report.json")
	for _, path := range []string{`"telephony.py"`, `"telephony_shared.py"`} {
		if !strings.Contains(report, path) {
			t.Errorf("compile report missing generated file %s", path)
		}
	}
	if strings.Contains(report, `"pcc-deploy.toml"`) {
		t.Error("direct carrier server must not advertise the incompatible Pipecat Cloud runner manifest")
	}
	if docker := artifactFile(t, artifact, "Dockerfile"); !strings.Contains(docker, `CMD ["uvicorn", "telephony:app"`) {
		t.Error("Dockerfile does not start the selected telephony server")
	}
	readme := artifactFile(t, artifact, "README.md")
	for _, want := range []string{
		"pipecat/carrier-websocket/twilio",
		"Twilio phone-number voice",
		"UNMUTE_LOG_LEVEL=DEBUG",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md missing Twilio setup %q", want)
		}
	}
	compose := artifactFile(t, artifact, "compose.telephony.yaml")
	assertValidYAML(t, compose)
	assertComposeLocalEnvironment(t, compose, TelephonyRuntimePlanFor(resolved))
	assertGoldenFile(t, filepath.Join("testdata", "golden", "pipecat_v1_telephony_compose.yaml"), compose, *updatePipecatV1)
	for _, want := range []string{
		"build:\n      context: .", "image: valkey/valkey:9.1.1-alpine", "condition: service_healthy",
		"REDIS_URL=redis://redis:6379/0", "redis_data:/data", "UNMUTE_TELEPHONY_PORT:-7860",
		`stop_grace_period: "1260s"`,
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("compose.telephony.yaml missing %q:\n%s", want, compose)
		}
	}
	for _, forbidden := range []string{"image: redis:latest", "secret-value", "+14155550123"} {
		if strings.Contains(compose, forbidden) {
			t.Errorf("compose.telephony.yaml contains %q", forbidden)
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

func TestPipecatTwilioTelephonyRejectsConnectionVocabularyDrift(t *testing.T) { // telephony V3, V7
	agent := loadCompilerAgent(t)
	// The carrier routes carry no transfers (SPEC C1, V1); this fixture is
	// about connection vocabulary, so the control goes.
	delete(agent.Controls, "to_human")
	billing := agent.Agents["billing"]
	billing.Tools = []string{"get_invoice"}
	agent.Agents["billing"] = billing
	resolved := targetByProvider(t, agent, ir.ProviderPipecat)
	resolved.Transport = "carrier-websocket"
	resolved.Carrier = "twilio"
	resolved.Connection = "primary_phone"
	resolved.Telephony = &ir.TelephonyPlan{
		Connection:  "primary_phone",
		Key:         ir.TelephonyKey{Provider: ir.ProviderPipecat, Transport: "carrier-websocket", Carrier: "twilio"},
		Environment: map[string]string{"account_sid": "TWILIO_ACCOUNT_SID", "auth_token": "TWILIO_AUTH_TOKEN", "from_number": "TWILIO_PHONE_NUMBER", "api_key": "WRONG"},
	}
	_, err := GeneratePipecat(agent, resolved, nil, nil)
	if err == nil || !strings.Contains(err.Error(), `does not accept connection environment key "api_key"`) {
		t.Fatalf("got %v", err)
	}
	delete(resolved.Telephony.Environment, "api_key")
	delete(resolved.Telephony.Environment, "auth_token")
	_, err = GeneratePipecat(agent, resolved, nil, nil)
	if err == nil || !strings.Contains(err.Error(), `requires connection environment key "auth_token"`) {
		t.Fatalf("got %v", err)
	}
}

func TestPipecatTelnyxTelephonyEmitsOnlySelectedAuthenticatedAdapter(t *testing.T) { // telephony T8, V7-V14
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	enablePackageTelephony(pkg)
	dropHumanTransfer(pkg)
	configured := pkg.Targets["pipecat"]
	configured.Connection = "primary_phone"
	setConnectionRoute(pkg, "primary_phone", "carrier-websocket", "telnyx")
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	connection := pkg.Connections["primary_phone"]
	connection.Environment = map[string]string{
		"api_key":       "TELNYX_API_KEY",
		"public_key":    "TELNYX_PUBLIC_KEY",
		"connection_id": "TELNYX_CONNECTION_ID",
		"from_number":   "TELNYX_PHONE_NUMBER",
	}
	pkg.Connections["primary_phone"] = connection
	outbound := true
	phone := pkg.Agent.Channels["phone"]
	phone.Outbound = &outbound
	pkg.Agent.Channels["phone"] = phone
	pkg.Agent.Variables["campaign_id"] = spec.Variable{Type: "string", Source: "call_start"}

	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := GeneratePipecat(agent, agent.Targets["pipecat"], nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	adapter := artifactFile(t, artifact, "telephony.py")
	for _, want := range []string{
		`Ed25519PublicKey.from_public_bytes`,
		`aiohttp.ClientTimeout(total=CONTROL_TIMEOUT_SECS)`,
		`aiohttp.ClientSession(headers=headers, timeout=timeout)`,
		`abs(int(time.time()) - signed_at) > WEBHOOK_TOLERANCE_SECS`,
		`timestamp.encode() + b"|" + raw`,
		`STATE.mark_once("event", data["id"], SESSION_TTL_SECS)`,
		`"stream_bidirectional_mode": "rtp"`,
		`"stream_bidirectional_codec": "PCMU"`,
		`"command_id": secrets.token_urlsafe(24)`,
		`task = asyncio.create_task(`,
		`await STATE.forget("event", event_id)`,
		`destination, call_start = await _outbound_request(request)`,
		`await handle_media(websocket, token)`,
		`"carrier": "telnyx"`,
	} {
		if !strings.Contains(adapter, want) {
			t.Errorf("telephony.py missing %q", want)
		}
	}
	statusCheck := strings.Index(adapter, `if response.status < 200 or response.status >= 300:`)
	jsonDecode := strings.Index(adapter, `return await response.json()`)
	if statusCheck < 0 || jsonDecode < 0 || statusCheck > jsonDecode {
		t.Error("Telnyx adapter must check response status before decoding JSON")
	}
	artifactFile(t, artifact, "telephony_shared.py")
	for _, forbidden := range []string{"RequestValidator", "Twilio", "Plivo", "Exotel", "media.payload", "audio/x-mulaw"} {
		if strings.Contains(adapter, forbidden) {
			t.Errorf("telephony.py contains unselected or media implementation %q", forbidden)
		}
	}
	bot := artifactFile(t, artifact, "bot.py")
	if !strings.Contains(bot, `"telnyx": lambda: FastAPIWebsocketParams`) {
		t.Error("bot.py does not select the Telnyx Pipecat serializer")
	}
	if strings.Contains(bot, `"twilio": lambda: FastAPIWebsocketParams`) {
		t.Error("bot.py contains the unselected Twilio telephony transport")
	}
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if !strings.Contains(pyproject, `"cryptography>=45,<47"`) || strings.Contains(pyproject, `"twilio>=9,<10"`) {
		t.Errorf("pyproject.toml has the wrong carrier dependencies:\n%s", pyproject)
	}
	report := artifactFile(t, artifact, "compile-report.json")
	for _, want := range []string{"TELNYX_API_KEY", "TELNYX_PUBLIC_KEY", "TELNYX_CONNECTION_ID", "TELNYX_PHONE_NUMBER"} {
		if !strings.Contains(report, want) {
			t.Errorf("compile report missing %s", want)
		}
	}
	readme := artifactFile(t, artifact, "README.md")
	for _, want := range []string{
		"pipecat/carrier-websocket/telnyx",
		"Telnyx Voice API",
		"API version 2",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md missing Telnyx setup %q", want)
		}
	}
}

func TestPipecatPlivoTelephonyEmitsOnlySelectedAuthenticatedAdapter(t *testing.T) { // telephony T9, V7-V14
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	enablePackageTelephony(pkg)
	dropHumanTransfer(pkg)
	configured := pkg.Targets["pipecat"]
	configured.Connection = "primary_phone"
	setConnectionRoute(pkg, "primary_phone", "carrier-websocket", "plivo")
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	connection := pkg.Connections["primary_phone"]
	connection.Environment = map[string]string{
		"auth_id": "PLIVO_AUTH_ID", "auth_token": "PLIVO_AUTH_TOKEN", "from_number": "PLIVO_PHONE_NUMBER",
	}
	pkg.Connections["primary_phone"] = connection
	outbound := true
	phone := pkg.Agent.Channels["phone"]
	phone.Outbound = &outbound
	pkg.Agent.Channels["phone"] = phone
	pkg.Agent.Variables["campaign_id"] = spec.Variable{Type: "string", Source: "call_start"}

	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := GeneratePipecat(agent, agent.Targets["pipecat"], nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	adapter := artifactFile(t, artifact, "telephony.py")
	for _, want := range []string{
		`utils.validate_v3_signature(`,
		`STATE.mark_once("nonce", nonce, SESSION_TTL_SECS)`,
		`timeout=CONTROL_TIMEOUT_SECS`,
		`bidirectional="true"`,
		`keepCallAlive="true"`,
		`contentType="audio/x-mulaw;rate=8000"`,
		`answer_url=_http_url(f"/telephony/answer/{token}")`,
		`destination, call_start = await _outbound_request(request)`,
		`await handle_media(websocket, token)`,
		`"carrier": "plivo"`,
	} {
		if !strings.Contains(adapter, want) {
			t.Errorf("telephony.py missing %q", want)
		}
	}
	artifactFile(t, artifact, "telephony_shared.py")
	for _, forbidden := range []string{"RequestValidator", "Twilio", "Telnyx", "Exotel", "media.payload"} {
		if strings.Contains(adapter, forbidden) {
			t.Errorf("telephony.py contains unselected or media implementation %q", forbidden)
		}
	}
	bot := artifactFile(t, artifact, "bot.py")
	if !strings.Contains(bot, `"plivo": lambda: FastAPIWebsocketParams`) {
		t.Error("bot.py does not select the Plivo Pipecat serializer")
	}
	for _, carrier := range []string{"twilio", "telnyx", "exotel"} {
		if strings.Contains(bot, `"`+carrier+`": lambda: FastAPIWebsocketParams`) {
			t.Errorf("bot.py contains the unselected %s telephony transport", carrier)
		}
	}
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if !strings.Contains(pyproject, `"plivo>=4,<5"`) || strings.Contains(pyproject, `"twilio>=9,<10"`) || strings.Contains(pyproject, `"cryptography>=45,<47"`) {
		t.Errorf("pyproject.toml has the wrong carrier dependencies:\n%s", pyproject)
	}
	report := artifactFile(t, artifact, "compile-report.json")
	for _, want := range []string{"PLIVO_AUTH_ID", "PLIVO_AUTH_TOKEN", "PLIVO_PHONE_NUMBER"} {
		if !strings.Contains(report, want) {
			t.Errorf("compile report missing %s", want)
		}
	}
	readme := artifactFile(t, artifact, "README.md")
	for _, want := range []string{
		"pipecat/carrier-websocket/plivo",
		"Plivo Voice XML",
		"Hangup URL",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md missing Plivo setup %q", want)
		}
	}
	runtime := TelephonyRuntimePlanFor(agent.Targets["pipecat"])
	var endpoints []string
	for _, endpoint := range runtime.PublicEndpoints {
		endpoints = append(endpoints, endpoint.Path)
	}
	if !slices.Contains(endpoints, "/telephony/answer/{token}") {
		t.Errorf("runtime plan missing endpoint /telephony/answer/{token}: %v", endpoints)
	}
	if slices.Contains(endpoints, "/telephony/transfer/{token}") {
		t.Errorf("runtime plan still exposes the deleted transfer endpoint: %v", endpoints)
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
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	human := agent.Controls["to_human"].(*ir.HumanTransfer)
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

// TestCheckPipecatVersion pins the template-compatible range. The workers model
// + Flows-in-core landed in 1.5.0 (the first 1.x release), so anything below is
// rejected at compile time — this is the guard that the bogus `1.0.3` pin
// (which never existed on PyPI) slipped past before.
func TestCheckPipecatVersion(t *testing.T) {
	for _, tc := range []struct {
		version string
		ok      bool
	}{
		{"1.5.0", true},
		{"1.5.3", true},
		{"1.6.0", true},
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
	agent := loadCompilerAgent(t)
	resolved := targetByProvider(t, agent, ir.ProviderPipecat)
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
	dropHumanTransfer(pkg)
	configured := pkg.Targets["pipecat"]
	configured.Connection = "primary_phone"
	setConnectionRoute(pkg, "primary_phone", "carrier-websocket", "twilio")
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

// carrierWebsocketArtifact builds the other Pipecat route shape, so the
// cross-route invariants can compare the two rather than assert one in
// isolation.
func carrierWebsocketArtifact(t *testing.T, carrier string) Artifact {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	enablePackageTelephony(pkg)
	dropHumanTransfer(pkg)
	configured := pkg.Targets["pipecat"]
	configured.Connection = "primary_phone"
	setConnectionRoute(pkg, "primary_phone", "carrier-websocket", carrier)
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	// Each carrier names its own credentials; the fixture ships Twilio's.
	if env := carrierEnvironment[carrier]; env != nil {
		connection := pkg.Connections["primary_phone"]
		connection.Environment = env
		pkg.Connections["primary_phone"] = connection
	}
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

var carrierEnvironment = map[string]map[string]string{
	"telnyx": {
		"api_key":       "TELNYX_API_KEY",
		"public_key":    "TELNYX_PUBLIC_KEY",
		"connection_id": "TELNYX_CONNECTION_ID",
		"from_number":   "TELNYX_PHONE_NUMBER",
	},
	"plivo": {
		"auth_id":     "PLIVO_AUTH_ID",
		"auth_token":  "PLIVO_AUTH_TOKEN",
		"from_number": "PLIVO_PHONE_NUMBER",
	},
}

func dailyArtifact(t *testing.T) Artifact {
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

// FR-008 / contracts invariant 8: a second transfer request in the same call
// produces no second attempt.
//
// The property is identical on every route; only the mechanism differs. The
// carrier routes hold it in the shared control store. The Daily route has no
// such store, must not gain one (contracts/artifacts.md forbids the service here
// and the constitution forbids an idle one), and needs no such store, because one
// process serves one call. So the assertion is written against the property.
//
// Before this feature there was no guard at all on this route: two requests in
// one call fired two platform transfers.
func TestUS2_DailyTransferAttemptsOnce(t *testing.T) {
	bot := artifactFile(t, dailyArtifact(t), "bot.py")
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
		`await params.result_callback(_TRANSFER_RESULT)`,
		`_TRANSFER_RESULT = {"transferred": True}`,
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

// Invariant 1: a Daily project declares no service and no public endpoint of its
// own. Asserted against the artifact's runtime description and file list, not
// against README prose, because prose is not what gets deployed.
func TestUS2_DailyProjectDeclaresNoServiceOrEndpoint(t *testing.T) {
	artifact := dailyArtifact(t)
	if artifact.Telephony != nil {
		t.Errorf("Daily artifact carries a telephony runtime plan (%d services, %d endpoints); "+
			"the platform's managed dial-in is the ingress",
			len(artifact.Telephony.Services), len(artifact.Telephony.PublicEndpoints))
	}
	for _, forbidden := range []string{
		"telephony.py", "telephony_shared.py", "telephony_state.py", "compose.telephony.yaml",
	} {
		if slices.Contains(artifactPaths(artifact), forbidden) {
			t.Errorf("Daily artifact emits %s: nothing on this route serves carrier traffic", forbidden)
		}
	}
	if !slices.Contains(artifactPaths(artifact), "pcc-deploy.toml") {
		t.Error("Daily artifact emits no deploy manifest")
	}
	report := artifactFile(t, artifact, "compile-report.json")
	for _, forbidden := range []string{"REDIS_URL", "UNMUTE_PUBLIC_URL", "UNMUTE_OUTBOUND_TOKEN"} {
		if strings.Contains(report, forbidden) {
			t.Errorf("Daily project requires %s, which nothing on this route reads", forbidden)
		}
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
	// Two, not three: the helper answers incoming calls and reports its health.
	// Placing a call is started against the platform, so there is no endpoint here
	// that spends money and no token guarding one.
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
	for _, forbidden := range []string{"REDIS_URL", "UNMUTE_PUBLIC_URL", "redis"} {
		if strings.Contains(report, forbidden) {
			t.Errorf("the carrier build requires %q, which nothing on this route reads", forbidden)
		}
	}
	for _, file := range artifact.Files {
		if strings.Contains(string(file.Content), "UNMUTE_PUBLIC_URL") {
			t.Errorf("%s references UNMUTE_PUBLIC_URL; the helper's public URL is the operator's and never compiled in", file.Path)
		}
	}
}

// Invariant 2, a regression guard: this feature must not shrink the route it is
// not touching.
func TestUS2_CarrierWebsocketKeepsItsRuntime(t *testing.T) {
	artifact := carrierWebsocketArtifact(t, "twilio")
	plan := artifact.Telephony
	if plan == nil {
		t.Fatal("carrier-websocket artifact lost its telephony runtime plan")
	}
	if len(plan.Processes) == 0 || len(plan.PublicEndpoints) == 0 {
		t.Errorf("carrier-websocket plan = %d processes, %d endpoints, want both non-empty",
			len(plan.Processes), len(plan.PublicEndpoints))
	}
	if plan.Coordination == "" {
		t.Error("carrier-websocket plan lost its coordination service")
	}
	for _, want := range []string{"telephony.py", "telephony_shared.py", "compose.telephony.yaml"} {
		if !slices.Contains(artifactPaths(artifact), want) {
			t.Errorf("carrier-websocket artifact no longer emits %s", want)
		}
	}
}

// Invariant 3: neither shape's credentials appear in the other. By set
// comparison, so a credential added later to one route cannot quietly show up in
// both without this failing.
func TestUS2_RouteShapesDoNotShareCredentials(t *testing.T) {
	daily := requiredEnvOf(t, dailyArtifact(t))
	carrier := requiredEnvOf(t, carrierWebsocketArtifact(t, "twilio"))
	carrierOnly := []string{"REDIS_URL", "UNMUTE_PUBLIC_URL", "TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN"}
	for _, name := range carrierOnly {
		if slices.Contains(daily, name) {
			t.Errorf("the Daily route asks for %q, which only the self-hosted media path needs", name)
		}
	}
	// The Daily route's own transfer destination must not leak the other way.
	if slices.Contains(carrier, "BILLING_PHONE_NUMBER") {
		t.Error("the carrier-websocket route asks for a transfer destination but emits no transfer")
	}
	if len(daily) == 0 || len(carrier) == 0 {
		t.Fatalf("required env sets = daily %v, carrier %v; an empty set proves nothing", daily, carrier)
	}
}

// Invariant 4: no transfer tool on any carrier websocket route. The base branch
// deleted these deliberately, because those transports carry no transfer
// primitive. This guards the deletion against a later change to shared emitter
// code putting it back.
func TestUS2_NoTransferToolOnCarrierWebsocketRoutes(t *testing.T) {
	for _, carrier := range []string{"twilio", "telnyx", "plivo"} {
		bot := artifactFile(t, carrierWebsocketArtifact(t, carrier), "bot.py")
		for _, forbidden := range []string{"sip_call_transfer", "sip_refer", "_TRANSFER_RESULT"} {
			if strings.Contains(bot, forbidden) {
				t.Errorf("%s bot.py emits %q: this transport has no transfer primitive", carrier, forbidden)
			}
		}
	}
}

// The prerequisite has one home, in the rulebook. It has to reach both places an
// author reads: the instructions they follow and the report they can inspect.
func TestUS3_PrerequisiteReachesReadmeAndReport(t *testing.T) {
	artifact := dailyArtifact(t)
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
	dropHumanTransferControl(pkg)
	// The transfer was the only thing using the phone route, so the connection
	// goes with it. On the Daily-provisioned route those two are now the same
	// question: the route has no carrier row in the capability table, so it
	// never carries a telephony channel (research R10), which leaves a dialing
	// control as the only way to use it. Dropping the control therefore drops
	// the route, and what remains under test is that a project with no dial-out
	// carries no prerequisite text anywhere.
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

// FR-011: every credential the route needs, the transfer destination included,
// joins the startup check, so a missing value fails the bot by name at boot
// instead of arriving as a call nobody answers.
func TestUS3_TransferDestinationIsInTheStartupCheck(t *testing.T) {
	// The example, not the fixture: a destination may be a literal or the name of
	// an env var read at call time, and only the env-name form has a credential to
	// check. A committed package uses the env form, because any number a
	// repository ships is a number nobody answers.
	pkg, err := spec.Load(filepath.Join("..", "..", "examples", "pipecat-human-transfer-daily"))
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
	for _, name := range requiredEnvOf(t, artifact) {
		if !strings.Contains(bot, `"`+name+`"`) {
			t.Errorf("required env %q is not in bot.py's startup check", name)
		}
		if !strings.Contains(env, name) {
			t.Errorf("required env %q is not in .env.example", name)
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

// dailyArtifactWithRegion compiles the Daily fixture for one declared region, or
// for none when region is empty.
func dailyArtifactWithRegion(t *testing.T, region string) Artifact {
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
	artifact := dailyArtifactWithRegion(t, region)
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
	artifact := dailyArtifactWithRegion(t, "")
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
	report := artifactFile(t, dailyArtifactWithRegion(t, "ap-south"), "compile-report.json")
	if !strings.Contains(report, `"deployment_regions"`) || !strings.Contains(report, "ap-south") {
		t.Errorf("compile-report.json does not record the forwarded region:\n%s", report)
	}
	absent := artifactFile(t, dailyArtifactWithRegion(t, ""), "compile-report.json")
	if strings.Contains(absent, "deployment_regions") {
		t.Errorf("compile-report.json reports a region the package never declared:\n%s", absent)
	}
}

// US5: the emitted instructions describe how an outbound call is started and say
// what identity the recipient sees, given the package cannot choose one.
//
// Documentation only on the *no-carrier* form, and deliberately so: there Daily
// owns the number, outbound is started by the platform against the deployed
// agent, and there is nothing for the package to declare.
//
// This comment used to say a phone channel was impossible on the Daily route
// full stop, which SCHEMA N37 superseded: the carrier form declares one, because
// naming a carrier gives the route a connection to dial with. That form is
// covered by TestCarrierOutboundTrigger. The no-carrier form is unchanged, and
// this test is its half.
func TestUS5_OutboundInstructionsNameTheIdentityAndThePermission(t *testing.T) {
	readme := artifactFile(t, dailyArtifact(t), "README.md")
	for _, want := range []string{
		"Place an outbound call",
		"https://api.pipecat.daily.co/v1/public/",
		`"enable_dialout": true`,
		"dialout_settings",
		// The recipient's identity is the provider's choice, and saying so is the
		// point: an author who assumes otherwise learns it from a stranger's
		// caller display.
		"recipient sees a number you did not choose",
		"picks one of your purchased numbers at random",
		"international dial-out is",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md missing %q", want)
		}
	}
}

func requiredEnvOf(t *testing.T, artifact Artifact) []string {
	t.Helper()
	var report struct {
		RequiredEnv []string `json:"required_env"`
	}
	if err := json.Unmarshal([]byte(artifactFile(t, artifact, "compile-report.json")), &report); err != nil {
		t.Fatal(err)
	}
	return report.RequiredEnv
}

func targetByProvider(t *testing.T, agent *ir.Agent, provider ir.Provider) ir.Target {
	t.Helper()
	for _, resolved := range agent.Targets {
		if resolved.Provider == provider {
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
		RequiredControls: []string{"cold_transfer", "hangup"},
	}
}

// dropHumanTransfer removes safe_core's to_human control, its tool wiring, and
// the channel's cold_transfer requirement, for fixtures on routes that carry
// no transfer primitive (SPEC C1, V1): the carrier-websocket routes.
func dropHumanTransfer(pkg *spec.Package) {
	dropHumanTransferControl(pkg)
	phone := pkg.Agent.Channels["phone"]
	phone.RequiredControls = []string{"hangup"}
	pkg.Agent.Channels["phone"] = phone
}

// dropHumanTransferControl removes the transfer completely: the control, its
// attachment, and the destination it resolved to. The destination has to go with
// it — a destination no control resolves to still put its environment name into
// .env.example and the generated startup check, so the agent refused to start
// over a phantom secret. That is now an error at build time (FR-002).
func dropHumanTransferControl(pkg *spec.Package) {
	delete(pkg.Agent.Controls, "to_human")
	delete(pkg.Agent.Destinations, "billing_line")
	billing := pkg.Agent.Agents["billing"]
	billing.Tools = slices.DeleteFunc(slices.Clone(billing.Tools), func(name string) bool { return name == "to_human" })
	pkg.Agent.Agents["billing"] = billing
}

// TestV14_ActivationGatedOnPipelineStart (driver-pipecat V14, B8): the entry
// agent's activation must not race main's StartFrame — frames pushed into
// BusBridge before it are dropped (greeting lost, tools never registered).
// The generated Daily SIP bot gates on_client_connected on an asyncio.Event set
// by main's on_pipeline_started handler.
func TestV14_ActivationGatedOnPipelineStart(t *testing.T) {
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
	for _, want := range []string{
		"pipeline_started = asyncio.Event()",
		"entry_started = False",
		"nonlocal entry_started",
		"if entry_started:",
		`@main.event_handler("on_pipeline_started")`,
		"pipeline_started.set()",
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

// A carrier-websocket telephony build (Twilio/Telnyx/Plivo) has no RTVI client:
// the carrier opens a raw media WebSocket, so the bot must activate and greet
// from the transport's on_client_connected event. Gating on RTVI on_client_ready
// (as the web client does) leaves a phone caller in silence — the greeting never
// fires. Verified against the Pipecat Twilio dial-in docs.
func TestPipecatCarrierWebsocketGreetsOnClientConnected(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	enablePackageTelephony(pkg)
	dropHumanTransfer(pkg)
	configured := pkg.Targets["pipecat"]
	configured.Connection = "primary_phone"
	setConnectionRoute(pkg, "primary_phone", "carrier-websocket", "twilio")
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
	if !strings.Contains(bot, `@transport.event_handler("on_client_connected")`) {
		t.Error("carrier-websocket bot must greet from on_client_connected (a phone call has no RTVI client)")
	}
	if strings.Contains(bot, `@main.rtvi.event_handler("on_client_ready")`) {
		t.Error("carrier-websocket bot must not wait for RTVI on_client_ready; a Twilio media stream never sends it")
	}
}

// TestV8_OutboundEmptyCallStartRendersSet (SPEC V8, B1): with no required
// call_start variables, CALL_START_REQUIRED must still be a set. An empty `{}`
// is a dict, so `CALL_START_REQUIRED - set(call_start)` in the dial-out handler
// would raise TypeError and 500 the outbound call.
func TestV8_OutboundEmptyCallStartRendersSet(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	enablePackageTelephony(pkg)
	dropHumanTransfer(pkg)
	configured := pkg.Targets["pipecat"]
	configured.Connection = "primary_phone"
	setConnectionRoute(pkg, "primary_phone", "carrier-websocket", "twilio")
	pkg.Targets = map[string]spec.Target{"pipecat": configured}
	outbound := true
	phone := pkg.Agent.Channels["phone"]
	phone.Outbound = &outbound
	pkg.Agent.Channels["phone"] = phone

	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := GeneratePipecat(agent, agent.Targets["pipecat"], nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	shared := artifactFile(t, artifact, "telephony_shared.py")
	if !strings.Contains(shared, "CALL_START_REQUIRED = frozenset((") {
		t.Errorf("CALL_START_REQUIRED must be a set even when empty:\n%s", shared)
	}
	if strings.Contains(shared, "CALL_START_REQUIRED = {") {
		t.Error("CALL_START_REQUIRED must not render as a dict literal ({} is a dict, breaks set difference)")
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

// TestPipecatWebDevNeedsNoTelephonyEnv is V10/B3: a telephony package must
// still boot in the browser, where Redis, the carrier keys and the public URL
// do not exist. bot.py checks provider credentials; telephony.py checks the
// route's environment on top.
//
// The fixture is the carrier-websocket route, because that is the route with a
// telephony.py to check the rest. It used to be examples/twilio-telephony-hello, whose
// Pipecat target moved to the platform-terminated route in feature 007; that
// route's own version of this invariant is
// TestCloudWebsocketPureInboundAsksForNothing.
func TestPipecatWebDevNeedsNoTelephonyEnv(t *testing.T) {
	artifact := carrierWebsocketArtifact(t, "twilio")
	botpy := artifactFile(t, artifact, "bot.py")
	start := strings.Index(botpy, "REQUIRED_ENV = [")
	block := botpy[start : start+strings.Index(botpy[start:], "]")]
	for _, telephonyOnly := range []string{"REDIS_URL", "UNMUTE_PUBLIC_URL", "TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN"} {
		if strings.Contains(block, telephonyOnly) {
			t.Errorf("bot.py REQUIRED_ENV must not demand %q: it blocks the web dev run (V10)", telephonyOnly)
		}
	}
	if !strings.Contains(block, "OPENAI_API_KEY") {
		t.Error("bot.py REQUIRED_ENV lost the provider credentials it does need")
	}
	// The telephony app still requires the full set, so nothing is unchecked.
	shared := artifactFile(t, artifact, "telephony_shared.py")
	for _, want := range []string{"REDIS_URL", "UNMUTE_PUBLIC_URL", "TWILIO_ACCOUNT_SID"} {
		if !strings.Contains(shared, want) {
			t.Errorf("telephony_shared.py must still require %q", want)
		}
	}
}

// T5: Daily's sip_call_transfer reports failure as a return value, never an
// exception, so the generated cold tool must read it — an ignored error tells
// the model the caller was transferred while they are still on the line. The
// on_unavailable policy picks the branch: return_to_caller hands the model a
// failure string; hangup says a goodbye and ends the call.
func TestV1_DailyColdTransferHandlesTheReturnedError(t *testing.T) {
	build := func(onUnavailable ir.OnUnavailable) string {
		pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
		if err != nil {
			t.Fatal(err)
		}
		agent, err := ir.Build(pkg)
		if err != nil {
			t.Fatal(err)
		}
		human := agent.Controls["to_human"].(*ir.HumanTransfer)
		human.OnUnavailable = onUnavailable
		artifact, err := GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return artifactFile(t, artifact, "bot.py")
	}

	returned := build(ir.OnUnavailableReturn)
	for _, want := range []string{
		// The primitive is a Daily transport method, so the tool narrows to that
		// transport first; a browser or console session gets a named failure
		// rather than an AttributeError.
		"if isinstance(_TRANSPORT, DailyTransport):",
		"this session is not a phone call, so it cannot be transferred",
		"await _TRANSPORT.sip_call_transfer(",
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

	hangup := build(ir.OnUnavailableHangup)
	for _, want := range []string{
		"if error is not None:",
		"nobody can take the call right now",
		"The transfer could not be completed; the call is ending.",
		"await params.llm.push_frame(EndFrame())",
	} {
		if !strings.Contains(hangup, want) {
			t.Errorf("hangup bot.py missing %q", want)
		}
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
