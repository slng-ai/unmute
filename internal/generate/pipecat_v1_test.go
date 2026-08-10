package generate

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

var updatePipecatV1 = flag.Bool("update-pipecat", false, "rewrite the pipecat v1 golden")

// TestPipecatV1BuiltinEndCallTool covers the prebuilt end_call lowering: a
// bodyless @tool that speaks the goodbye then ends via EndFrame, with no
// url_env, handler, or httpx POST (docs/spec/prebuilt-tools.md V7).
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

// TestPipecatGreetingModes covers the three SCHEMA.md 4.8 combinations. Fixed
// text bypasses the LLM; an omitted text still asks the model; user-first stays
// silent (SPEC V1, V4, V5).
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
		"@tool",
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
	configured := pkg.Targets["pipecat"]
	configured.Transport = "carrier-websocket"
	configured.Carrier = "twilio"
	configured.Connection = "primary_phone"
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
		`STATE.mark_once("transfer", call_id, SESSION_TTL_SECS)`,
		`STATE.mark_once("status", event_id, SESSION_TTL_SECS)`,
		`status_callback_event=["initiated", "ringing", "answered", "completed"]`,
		`await asyncio.to_thread(client.calls(call_id).update, twiml=_dial_twiml(destination))`,
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
		`await telephony.cold_transfer(self.call_context["call_id"], "+14155550123")`,
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
		"Cold transfer is destructive on this generated\nTwilio\nroute",
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
	configured := pkg.Targets["pipecat"]
	configured.Transport = "carrier-websocket"
	configured.Carrier = "telnyx"
	configured.Connection = "primary_phone"
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
		"Cold transfer is destructive on this generated\nTelnyx\nroute",
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
	configured := pkg.Targets["pipecat"]
	configured.Transport = "carrier-websocket"
	configured.Carrier = "plivo"
	configured.Connection = "primary_phone"
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
		`_client().calls.transfer`,
		`aleg_url=_http_url(f"/telephony/transfer/{token}")`,
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
		"Cold transfer is destructive on this generated\nPlivo\nroute",
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
	for _, want := range []string{"/telephony/answer/{token}", "/telephony/transfer/{token}"} {
		if !slices.Contains(endpoints, want) {
			t.Errorf("runtime plan missing endpoint %s: %v", want, endpoints)
		}
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

func TestPipecatWarmHumanTransferFailsGeneration(t *testing.T) {
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
	human.Briefing = ir.BriefingSummary
	_, err = GeneratePipecat(agent, targetByProvider(t, agent, ir.ProviderPipecat), nil, nil)
	if err == nil || !strings.Contains(err.Error(), `mode "warm" has no Pipecat lowering`) {
		t.Fatalf("warm transfer must fail instead of lowering cold, got %v", err)
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

func enablePackageTelephony(pkg *spec.Package) {
	inbound, outbound := true, false
	pkg.Agent.Channels["phone"] = spec.Channel{
		Kind: "telephony", Inbound: &inbound, Outbound: &outbound,
		RequiredControls: []string{"cold_transfer", "hangup"},
	}
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
	configured := pkg.Targets["pipecat"]
	configured.Transport = "carrier-websocket"
	configured.Carrier = "twilio"
	configured.Connection = "primary_phone"
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
	configured := pkg.Targets["pipecat"]
	configured.Transport, configured.Carrier, configured.Connection = "carrier-websocket", "twilio", "primary_phone"
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
