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
		"def _enable_agent_tracing(main: PipelineWorker, agents: Sequence[LLMWorker]) -> None:",
		"agent._tracing_context = main._tracing_context",
		"_enable_agent_tracing(main, AGENTS)",
		"trace_provider.force_flush()",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q", want)
		}
	}
	if !strings.Contains(bot, "if not public_key or not secret_key or not base_url:") {
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
	} {
		if strings.Contains(bot, forbidden) {
			t.Errorf("bot.py contains custom tracing hook %q", forbidden)
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
		"PipelineParams(enable_metrics=True, enable_usage_metrics=True)",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q", want)
		}
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
	artifact, err := Generate(agent, targetByProvider(t, agent, ir.ProviderPipecat), target.Default())
	if err != nil {
		t.Fatal(err)
	}

	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"from collections.abc import Sequence",
		"from opentelemetry.sdk.trace import TracerProvider",
		"from pipecat.transcriptions.language import Language",
		`Language("en")`,
		`setattr(patched, "__langfuse_patch__", True)`,
		`setattr(service_decorators, "add_llm_span_attributes", patched_llm)`,
		`setattr(TTSService, "append_to_audio_context", patched_append_to_audio_context)`,
		"if not public_key or not secret_key or not base_url:",
		"def _enable_agent_tracing(main: PipelineWorker, agents: Sequence[LLMWorker]) -> None:",
		"context = self._tracing_context",
		"if not self._enable_tracing or context is None:",
		"if isinstance(trace_provider, TracerProvider):",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing static-check-safe form %q", want)
		}
	}
	for _, forbidden := range []string{
		"patched.__langfuse_patch__",
		"append_to_audio_context.__langfuse_patch__",
		"TTSService.append_to_audio_context =",
		"trace.get_tracer_provider().force_flush()",
	} {
		if strings.Contains(bot, forbidden) {
			t.Errorf("bot.py contains ty-unsafe form %q", forbidden)
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

	bot := artifactFile(t, artifact, "bot.py")
	for _, want := range []string{
		"service_decorators.add_llm_span_attributes",
		`kwargs.get("system_instructions")`,
		`{"role": "system", "content": system_instruction}`,
		`span.set_attribute("langfuse.observation.input", json.dumps(messages, default=str))`,
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing system-instruction tracing form %q", want)
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
	for _, want := range []string{
		"class _TracedLLMWorker(LLMWorker):",
		"start_as_current_span(",
		`f"tool:{name}", context=parent`,
		`"langfuse.observation.input"`,
		`"langfuse.observation.output"`,
		`"tool.function_name"`,
		`"tool.call_id"`,
		"class IntakeAgent(_TracedLLMWorker):",
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q", want)
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

func TestPipecatTwilioTelephonyEmitsOnlySelectedAuthenticatedAdapter(t *testing.T) { // telephony V7-V14, V20
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	configured := pkg.Targets["pipecat"]
	configured.Transport = "carrier-websocket"
	configured.Carrier = "twilio"
	configured.Connection = "primary_phone"
	pkg.Targets["pipecat"] = configured
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
		`_validator().validate(_http_url(request.url.path), form, signature)`,
		`_validator().validate(`,
		`pending = _pending.pop(token, None)`,
		`hmac.compare_digest(request.headers.get("Authorization", ""), expected)`,
		`status_callback_event=["initiated", "ringing", "answered", "completed"]`,
		`await asyncio.to_thread(client.calls(call_id).update, twiml=_dial_twiml(destination))`,
		`"campaign_id": "string"`,
		`CALL_START_REQUIRED = {`,
	} {
		if !strings.Contains(adapter, want) {
			t.Errorf("telephony.py missing %q", want)
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
	} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q", want)
		}
	}
	if strings.Contains(bot, "STATE = State()") || strings.Contains(bot, "AGENTS = [") {
		t.Error("per-call state or workers escaped into module globals")
	}
	pyproject := artifactFile(t, artifact, "pyproject.toml")
	if !strings.Contains(pyproject, `"twilio>=9,<10"`) {
		t.Error("pyproject.toml missing selected Twilio SDK")
	}
	report := artifactFile(t, artifact, "compile-report.json")
	if !strings.Contains(report, `"telephony.py"`) {
		t.Error("compile report missing selected adapter")
	}
	if strings.Contains(report, `"pcc-deploy.toml"`) {
		t.Error("direct carrier server must not advertise the incompatible Pipecat Cloud runner manifest")
	}
	if docker := artifactFile(t, artifact, "Dockerfile"); !strings.Contains(docker, `CMD ["uvicorn", "telephony:app"`) {
		t.Error("Dockerfile does not start the selected telephony server")
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
