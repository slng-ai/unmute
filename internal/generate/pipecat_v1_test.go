package generate

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/spec"
	"github.com/slng/unmute/internal/target"
)

var updatePipecatV1 = flag.Bool("update-pipecat", false, "rewrite the pipecat v1 golden")

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
	for _, want := range []string{
		"def setup_langfuse_tracing() -> bool:",
		`f"{base_url.rstrip('/')}/api/public/otel"`,
		"setup_tracing(service_name=TRACE_NAME, exporter=OTLPSpanExporter())",
		"enable_tracing=tracing_enabled",
		`additional_span_attributes={"langfuse.trace.name": TRACE_NAME}`,
		"def _enable_agent_tracing(main: PipelineWorker, agents: list[LLMWorker]) -> None:",
		"agent._tracing_context = main._tracing_context",
		"_enable_agent_tracing(main, AGENTS)",
		"trace.get_tracer_provider().force_flush()",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q", want)
		}
	}
	if strings.Contains(bot, "if not any(values)") || !strings.Contains(bot, "if not all(values)") {
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

	bot := artifactFile(t, artifact, "bot.py")
	for _, forbidden := range []string{
		"SpanProcessor",
		"LangfuseAttributeProcessor",
		"span.set_attribute =",
		"service_decorators",
	} {
		if strings.Contains(bot, forbidden) {
			t.Errorf("bot.py contains custom tracing hook %q", forbidden)
		}
	}
}

// TestPipecatV1TasksGolden exercises the T4 agency level (tasks, task_group,
// delegates) that safe_core omits, by building the IR in-code (driver-pipecat T4).
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
}

func TestPipecatV1OmitsTracingUnlessConfigured(t *testing.T) { // V19
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

// The serviceInfo coverage test (driver-pipecat V11) is superseded: class,
// import, and install now travel together on one catalogue entry, so an
// emitted class structurally cannot lose its import (TestCatalogInvariants in
// internal/target). TestPipecatUnknownProviderFailsClosed keeps B1's failure
// mode (a silent OpenAI-compatible substitution) explicitly covered.
func TestPipecatUnknownProviderFailsClosed(t *testing.T) {
	env := newEnvSet()
	_, err := ttsService(ir.Binding{Provider: "acme", Model: "m", Voice: "v"}, "en", env)
	if err == nil || !strings.Contains(err.Error(), "endpoint_env") {
		t.Fatalf("unknown provider without endpoint_env must fail closed, got %v", err)
	}
	svc, err := ttsService(ir.Binding{Provider: "acme", Model: "m", Voice: "v", EndpointEnv: "ACME_URL"}, "en", env)
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

// TestV14_ActivationGatedOnPipelineStart (driver-pipecat V14, B8): the entry
// agent's activation must not race main's StartFrame — frames pushed into
// BusBridge before it are dropped (greeting lost, tools never registered).
// The generated bot gates on_client_connected on an asyncio.Event set by
// main's on_pipeline_started handler.
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
