package generate

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// covalArtifact renders the shared fixture with Coval tracing on, for one target.
func covalArtifact(t *testing.T, provider ir.Provider) Artifact {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	enableCoval(agent)
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

// The provider decides which tracing module is emitted, and each provider's
// module must not carry the other's vocabulary. Both land on tracing.py so the
// entry point imports one module name either way.
func TestCovalTracingEmitsItsOwnModule(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		artifact := covalArtifact(t, provider)
		tracing := artifactFile(t, artifact, "tracing.py")
		for _, want := range []string{
			"https://api.coval.dev/v1/traces",
			`"x-api-key"`,
			"X-Simulation-Id",
			"COVAL_API_KEY",
			"resolve_simulation_id",
			"MAX_PREACTIVATION_SPANS",
			"timeout=EXPORT_TIMEOUT_SECONDS",
		} {
			if !strings.Contains(tracing, want) {
				t.Errorf("%s tracing.py missing %q", provider, want)
			}
		}
		// Coval's ingest correlates on a header, so the timeout its docs require
		// has to survive as a real value, not just a named constant.
		if !strings.Contains(tracing, "EXPORT_TIMEOUT_SECONDS = 30") {
			t.Errorf("%s tracing.py must keep Coval's required 30s export timeout", provider)
		}
		// Forbid Langfuse's machinery, not the word: a comment contrasting the
		// two providers' behaviour on a missing key is worth keeping.
		for _, forbidden := range []string{
			"LANGFUSE_", "langfuse.observation", "langfuse.trace",
			"import langfuse", "setup_langfuse",
		} {
			if strings.Contains(tracing, forbidden) {
				t.Errorf("%s Coval tracing.py carries Langfuse machinery: %q", provider, forbidden)
			}
		}
	}
}

// A missing Coval key disables tracing rather than failing the call. This is the
// one place Coval deliberately differs from Langfuse, which raises, so it is
// pinned rather than left to drift back.
func TestCovalTracingSurvivesAMissingKey(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		tracing := artifactFile(t, covalArtifact(t, provider), "tracing.py")
		if !strings.Contains(tracing, "COVAL_API_KEY is not set") {
			t.Errorf("%s tracing.py must warn on a missing key", provider)
		}
		if strings.Contains(tracing, `raise ValueError`) {
			t.Errorf("%s tracing.py must not raise on a missing Coval key", provider)
		}
	}
}

// Spans must be held until the simulation ID arrives, and the hold must be
// bounded so a call nobody claims cannot grow the process without limit.
// Every traced call reaches Coval, whether or not a simulation placed it. A
// call nothing claimed is registered as a Coval conversation once it ends, and
// its spans are exported against that ID, which is what puts a local run in
// Trace Search. Without this the spans are built, held, and then dropped.
func TestCovalTracingRegistersCallsNoSimulationClaimed(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		tracing := artifactFile(t, covalArtifact(t, provider), "tracing.py")
		for _, want := range []string{
			// The two routes are separate and Coval accepts exactly one header.
			`COVAL_CONVERSATIONS_ENDPOINT = "https://api.coval.dev/v1/conversations:submit"`,
			`CONVERSATION_EXPORT_HEADER = "X-Conversation-Id"`,
			`SIMULATION_EXPORT_HEADER = "X-Simulation-Id"`,
			`headers={"x-api-key": self._api_key, header: correlation_id}`,
			// Submit first, then export against what it returned.
			"def submit_conversation(",
			"_router.activate(conversation_id, CONVERSATION_EXPORT_HEADER)",
			// A missing key or an empty call must not invent a conversation,
			// and neither may pass without saying so.
			`logger.warning("COVAL_API_KEY is not set, so this call is not filed with Coval")`,
			"if not transcript:",
			// The call is over by now, so a failed submit is a warning.
			"Could not register this call with Coval",
		} {
			if !strings.Contains(tracing, want) {
				t.Errorf("%s tracing.py missing %q", provider, want)
			}
		}
		// The two headers must never travel together on one export.
		if strings.Contains(tracing, "SIMULATION_EXPORT_HEADER: simulation_id,") {
			t.Errorf("%s tracing.py still hardcodes the simulation header on export", provider)
		}
		// Where the transcript comes from differs, because who owns the spans
		// differs: Pipecat's own spans carry the text, while the LiveKit module
		// builds the tree itself and so records the transcript as it goes.
		source := "def _transcript_from("
		if provider == ir.ProviderLiveKit {
			source = "def transcript(self)"
		}
		if !strings.Contains(tracing, source) {
			t.Errorf("%s tracing.py missing its transcript source %q", provider, source)
		}
	}
}

func TestCovalTracingHoldsSpansUntilTheSimulationArrives(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		tracing := artifactFile(t, covalArtifact(t, provider), "tracing.py")
		for _, want := range []string{
			"deque(maxlen=MAX_PREACTIVATION_SPANS)",
			"def activate(",
			"Dropped %d span(s) before the correlation ID arrived",
		} {
			if !strings.Contains(tracing, want) {
				t.Errorf("%s tracing.py missing %q", provider, want)
			}
		}
	}
}

// Every documented route for the target has to be in the emitted resolver, and
// the route it used has to end up on the trace. A trace against the wrong
// simulation is otherwise undiagnosable.
func TestCovalTracingResolvesEveryDocumentedRoute(t *testing.T) {
	routes := map[ir.Provider][]string{
		ir.ProviderPipecat: {
			`return found, "websocket_header"`,
			`return found, "sip_header"`,
			`return found, "carrier_parameter"`,
			`return found, "environment"`,
		},
		ir.ProviderLiveKit: {
			`return found, "dispatch_metadata"`,
			`return found, "sip_participant_attribute"`,
			`return found, "environment"`,
		},
	}
	for provider, want := range routes {
		tracing := artifactFile(t, covalArtifact(t, provider), "tracing.py")
		for _, route := range want {
			if !strings.Contains(tracing, route) {
				t.Errorf("%s resolver missing route %s", provider, route)
			}
		}
		if !strings.Contains(tracing, "coval.correlation.method") {
			t.Errorf("%s must record which route supplied the simulation ID", provider)
		}
	}
}

// Coval keys its viewer and its metrics off span names, and both runtimes emit
// their own. Renaming those is what keeps one set of spans instead of two.
// Coval's Trace Search filters on service.name, and one package compiles to both
// targets. The name is built from the target, so the two never collide; build it
// from the package instead and a LiveKit run and a Pipecat run of the same
// package land in one undifferentiated pile.
// The identity is AGENT_NAME rather than TRACE_NAME since the local suffix was
// added: TRACE_NAME is now derived from it and reads identically on both
// targets, so matching that would compare the derivation instead of the name.
func TestCovalTracingNamesEachTargetsService(t *testing.T) {
	seen := map[string]ir.Provider{}
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		tracing := artifactFile(t, covalArtifact(t, provider), "tracing.py")
		found := regexp.MustCompile(`AGENT_NAME = (.+)`).FindStringSubmatch(tracing)
		if found == nil {
			t.Fatalf("%s Coval tracing declares no AGENT_NAME", provider)
		}
		if other, clash := seen[found[1]]; clash {
			t.Errorf("%s and %s both report service.name %s, so Trace Search cannot tell them apart", provider, other, found[1])
		}
		seen[found[1]] = provider
	}
}

func TestCovalTracingBuildsOneSpanPerCovalName(t *testing.T) {
	pipecat := artifactFile(t, covalArtifact(t, ir.ProviderPipecat), "tracing.py")
	// Pipecat already emits conversation/turn/llm/stt/tts itself, so the Coval
	// module must not create a second llm or tts span of its own.
	for _, forbidden := range []string{`start_span("llm")`, `start_span("tts")`, `start_span("stt")`} {
		if strings.Contains(pipecat, forbidden) {
			t.Errorf("pipecat Coval tracing duplicates a span Pipecat already emits: %s", forbidden)
		}
	}
	if !strings.Contains(pipecat, `"llm_tool_call"`) {
		t.Error("pipecat Coval tracing must add the tool-call span Pipecat lacks")
	}

	livekit := artifactFile(t, covalArtifact(t, ir.ProviderLiveKit), "tracing.py")
	// LiveKit is the opposite case. Its own spans are shaped for LiveKit:
	// `user_turn` and `agent_turn` are siblings, so the caller's speech can never
	// sit inside the reply it caused, and one exchange opens several `agent_turn`s
	// once tools run. Renaming cannot reparent, so the module builds Coval's tree
	// itself and leaves LiveKit's telemetry off. Two ways that goes wrong, and
	// both are silent: handing the provider to LiveKit puts a hundred framework
	// spans and a duplicate of every canonical one back into the trace, and
	// creating a canonical name twice doubles whatever metric counts it.
	// Matched at statement position, so the comment explaining why it is not
	// called does not trip the gate.
	installsLiveKitTelemetry := regexp.MustCompile(`(?m)^\s*(from livekit\.agents\.telemetry import|import livekit\.agents\.telemetry|set_tracer_provider\()`)
	if installsLiveKitTelemetry.MatchString(livekit) {
		t.Error("livekit Coval tracing must not install LiveKit's own telemetry, which duplicates every canonical span")
	}
	if strings.Contains(livekit, "update_name(") {
		t.Error("livekit Coval tracing renames a framework span; a rename cannot reparent one, which is why the tree was wrong")
	}
	started := livekitSpanNames(t, livekit)
	for _, coval := range []string{"conversation", "turn", "stt", "vad", "llm", "tts", "llm_tool_call"} {
		switch started[coval] {
		case 1:
		case 0:
			t.Errorf("livekit Coval tracing never starts the canonical %q span", coval)
		default:
			t.Errorf("livekit Coval tracing starts %q in %d places, so Coval sees it more than once per turn", coval, started[coval])
		}
	}
	// Coval's schema nests one span per provider attempt under `stt`. The name
	// carries the provider, so it is built rather than written out.
	if !strings.Contains(livekit, `"stt.provider." + turn.stt_provider.lower()`) {
		t.Error("livekit Coval tracing must nest a stt.provider.<name> span under stt")
	}
}

// Coval's built-in metrics read attributes by name. LiveKit reports the same
// measurements on its session events under its own names, so the emitted module
// has to write Coval's names onto the spans it builds. Without this the spans
// look right in the viewer and every latency, token and tool metric reads
// nothing.
func TestCovalLiveKitCarriesTheAttributesCovalReads(t *testing.T) {
	tracing := artifactFile(t, covalArtifact(t, ir.ProviderLiveKit), "tracing.py")
	for _, want := range []string{
		// stt
		`"transcript": heard.text`,
		`"turn.user_transcript": _clip(turn.said())`,
		`attributes["stt.confidence"]`,
		`attributes["metrics.ttfb"] = float(delay)`,
		`"stt.providerName": turn.stt_provider`,
		// llm
		`"llm.finish_reason": "tool_calls" if round_.tools else "stop"`,
		`"gen_ai.usage.input_tokens": round_.input_tokens`,
		`"gen_ai.usage.output_tokens": round_.output_tokens`,
		`attributes["metrics.ttfb"] = float(round_.ttft)`,
		// the prompt each round ran on
		`"gen_ai.system_instructions": round_.instructions`,
		`"input": round_.prompt`,
		`"tools": round_.offered_tools`,
		`"agent.label": round_.agent_label`,
		`"gen_ai.response.id": round_.request_id`,
		`if round_.total_tokens or round_.input_tokens or round_.output_tokens:`,
		`PLACEHOLDER_INSTRUCTIONS`,
		`attributes["output"] = turn.agent_transcript`,
		// tts
		`attributes["metrics.ttfb"] = float(speech.ttfb)`,
		// tool calls, including the result the agent acted on
		`"function.name": tool["name"]`,
		`"tool_call_id": tool["call_id"]`,
		`"function.arguments": tool["arguments"]`,
		`"tool.result": tool["output"]`,
		`"tool.error": 1 if tool["failed"] else 0`,
		`"tool.latency_ms"`,
		// conversation aggregates
		`"tool.call.count": self._tool_calls`,
		`"tool.failure.count": self._tool_failures`,
		`"transcript.turn.count": self._turns`,
		`"call.duration_seconds"`,
	} {
		if !strings.Contains(tracing, want) {
			t.Errorf("livekit Coval tracing does not write %s", want)
		}
	}
	// Free text goes out bounded. Coval rejects an oversized batch outright, and
	// a chat context grows all call long while riding along on every model call,
	// so an unbounded prompt is the one attribute that can get a whole trace
	// rejected.
	for _, bound := range []string{"MAX_TEXT_CHARS", "MAX_PROMPT_CHARS", "def _clip(", "def _clip_to("} {
		if !strings.Contains(tracing, bound) {
			t.Errorf("livekit Coval tracing must bound the free text it exports, missing %s", bound)
		}
	}
}

// livekit.agents.telemetry.set_tracer_provider installs the provider on
// LiveKit's own tracer and leaves the process-global provider untouched. Emitted
// code that reaches for the global gets a no-op provider, and every span it
// creates disappears without an error. So the LiveKit module must take its
// tracers off the provider it built.
func TestCovalLiveKitTracingNeverReadsTheGlobalProvider(t *testing.T) {
	tracing := artifactFile(t, covalArtifact(t, ir.ProviderLiveKit), "tracing.py")
	if strings.Contains(tracing, "trace.get_tracer(") {
		t.Error("livekit Coval tracing reads the global tracer provider, which LiveKit does not set")
	}
	if !strings.Contains(tracing, "provider.get_tracer(") {
		t.Error("livekit Coval tracing must take its tracer off the provider it built")
	}
}

// LiveKit only surfaces a SIP header it was told about in advance, so the trunk
// the operator registers has to carry the mapping. Without this the participant
// attribute the agent reads never exists.
func TestCovalLiveKitTrunkMapsTheSimulationHeader(t *testing.T) {
	agent, resolved := configuredLiveKitSIP(t)
	enableCoval(agent)
	artifact, err := GenerateLiveKit(agent, resolved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	trunk := artifactFile(t, artifact, "sip-inbound-trunk.json")
	for _, want := range []string{"headers_to_attributes", "X-Coval-Simulation-Id", "coval.simulation_id"} {
		if !strings.Contains(trunk, want) {
			t.Errorf("sip-inbound-trunk.json missing %q", want)
		}
	}
	// The agent reads the attribute this file names, so the two must agree.
	if !strings.Contains(artifactFile(t, artifact, "tracing.py"), `"coval.simulation_id"`) {
		t.Fatal("the agent does not read the attribute the trunk maps")
	}

	// Langfuse must not acquire a Coval header mapping.
	langfuseAgent, langfuseTarget := configuredLiveKitSIP(t)
	enableLangfuse(langfuseAgent)
	langfuse, err := GenerateLiveKit(langfuseAgent, langfuseTarget, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(artifactFile(t, langfuse, "sip-inbound-trunk.json"), "Coval") {
		t.Fatal("Langfuse tracing added a Coval header mapping")
	}
}

// The exporter Coval ingests is not the one either runtime pulls in on its own.
func TestCovalTracingDeclaresItsExporter(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		pyproject := artifactFile(t, covalArtifact(t, provider), "pyproject.toml")
		if !strings.Contains(pyproject, "opentelemetry-exporter-otlp-proto-http") {
			t.Errorf("%s pyproject.toml does not declare the OTLP HTTP exporter", provider)
		}
		if strings.Contains(pyproject, "langfuse") {
			t.Errorf("%s pyproject.toml declares langfuse under Coval tracing", provider)
		}
	}
}

// The entry point must call its provider's setup, and only its provider's.
func TestCovalTracingWiresTheEntryPoint(t *testing.T) {
	bot := artifactFile(t, covalArtifact(t, ir.ProviderPipecat), "bot.py")
	for _, want := range []string{"setup_coval_tracing()", "activate_simulation(runner_args)", "enable_tracing=True"} {
		if !strings.Contains(bot, want) {
			t.Errorf("bot.py missing %q", want)
		}
	}
	if strings.Contains(bot, "setup_langfuse_tracing") {
		t.Error("bot.py calls the Langfuse setup under Coval tracing")
	}

	agentPy := artifactFile(t, covalArtifact(t, ir.ProviderLiveKit), "agent.py")
	if !strings.Contains(agentPy, "from tracing import setup_coval") || !strings.Contains(agentPy, "setup_coval(") {
		t.Error("agent.py does not wire the Coval setup")
	}
	if strings.Contains(agentPy, "setup_langfuse") {
		t.Error("agent.py calls the Langfuse setup under Coval tracing")
	}
	// The implementation stays in tracing.py (V31), same as Langfuse.
	for _, forbidden := range []string{"def setup_coval", "OTLPSpanExporter("} {
		if strings.Contains(agentPy, forbidden) {
			t.Errorf("agent.py contains tracing implementation %q", forbidden)
		}
	}
}

// Choosing coval must require the Coval key and none of the Langfuse ones.
func TestCovalTracingRequiresOnlyItsOwnSecret(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		env := artifactFile(t, covalArtifact(t, provider), ".env.example")
		if !strings.Contains(env, "COVAL_API_KEY") {
			t.Errorf("%s .env.example does not require COVAL_API_KEY", provider)
		}
		if strings.Contains(env, "LANGFUSE_") {
			t.Errorf("%s .env.example requires a Langfuse key under Coval tracing", provider)
		}
	}
}

// Pipecat Cloud serves many calls from one warm container. The emitted module
// keeps one provider for the process, because Pipecat installs one and refuses a
// second, but everything about *which* call is being traced has to reset on
// every call. It did not, and the result was silent data corruption: the second
// and later calls in a warm process were appended to the first call's Coval
// conversation and registered nothing of their own.
//
// The smoke gate is what actually catches a regression here. This one keeps the
// shape of the fix in place, and runs in the default suite where the smoke
// suite does not.
func TestCovalTracingResetsCorrelationPerCall(t *testing.T) {
	tracing := artifactFile(t, covalArtifact(t, ir.ProviderPipecat), "tracing.py")
	for _, want := range []string{
		// The reset itself, and a token that says which call a span belongs to.
		"def begin_call(self) -> int:",
		"self.call_token += 1",
		"self.simulation_id = None",
		`_CALL_TOKEN_ATTR = "coval.internal.call_token"`,
		"span.set_attribute(_CALL_TOKEN_ATTR, router.call_token)",
		// The export is scoped by that token, so a span that ended after its own
		// call cannot ride out under the next call's correlation ID.
		"mine = [s for s in spans if (s.attributes or {}).get(_CALL_TOKEN_ATTR) == token]",
		"return exporter.export(mine)",
		"Discarded %d span(s) from a finished call",
	} {
		if !strings.Contains(tracing, want) {
			t.Errorf("pipecat tracing.py missing per-call correlation: %q", want)
		}
	}
	// The memoized path is the whole bug: returning the process's provider
	// without resetting the call is what filed every later call under the first.
	memoized := regexp.MustCompile(`(?s)if _provider is not None:.*?return _provider`).FindString(tracing)
	if memoized == "" {
		t.Fatal("pipecat tracing.py no longer memoizes its provider; re-read this gate before deleting it")
	}
	if !strings.Contains(memoized, "begin_call()") {
		t.Error("pipecat setup_coval_tracing returns a warm provider without beginning a new call, so a second call in one process is filed under the first")
	}
	// A processor that has shut down drops every later span in silence, and the
	// next call in a warm process still needs it.
	if strings.Contains(tracing, "provider.shutdown()") {
		t.Error("pipecat tracing.py shuts down the provider, which silently drops every span of the next call in a warm process")
	}
}

// Two budgets, deliberately different. The exporter keeps the 30 seconds Coval's
// docs require; registering the call runs inside the host's shutdown window, and
// LiveKit force-kills a job process ten seconds after shutdown begins. Collapsing
// them back into one literal is the regression this gate exists for.
func TestCovalTracingSplitsTheSubmitBudgetFromTheExportTimeout(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		tracing := artifactFile(t, covalArtifact(t, provider), "tracing.py")
		export := regexp.MustCompile(`EXPORT_TIMEOUT_SECONDS = (\d+)`).FindStringSubmatch(tracing)
		submit := regexp.MustCompile(`SUBMIT_TIMEOUT_SECONDS = (\d+)`).FindStringSubmatch(tracing)
		if export == nil || submit == nil {
			t.Fatalf("%s tracing.py must declare both timeout budgets", provider)
		}
		if export[1] != "30" {
			t.Errorf("%s tracing.py must keep Coval's required 30s export timeout, got %s", provider, export[1])
		}
		if submit[1] == export[1] {
			t.Errorf("%s tracing.py gives the shutdown-path submit the exporter's %ss budget, which does not fit LiveKit's 10s kill window", provider, submit[1])
		}
		if submit[1] != "5" {
			t.Errorf("%s tracing.py submit budget is %ss; the contract pins 5s", provider, submit[1])
		}
		// The submit is the call that has to fit the window.
		if !strings.Contains(tracing, "timeout=SUBMIT_TIMEOUT_SECONDS") {
			t.Errorf("%s tracing.py does not spend the shutdown budget on the conversation submit", provider)
		}
		if !strings.Contains(tracing, "timeout=EXPORT_TIMEOUT_SECONDS") {
			t.Errorf("%s tracing.py does not give the OTLP exporter Coval's required timeout", provider)
		}
		// Losing a trace to a slow Coval must be visible, not silent.
		if !strings.Contains(tracing, "took longer than its %ss budget") {
			t.Errorf("%s tracing.py does not log a submit cut short by its budget", provider)
		}
	}
}

// One build has to serve both a local `unmute dev` run and the same package
// deployed, so the local mark is decided when the agent starts rather than when
// it is compiled. The two targets of one package must still be told apart, which
// is what the deployed identity underneath the suffix is for.
func TestCovalTracingMarksLocalRuns(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		tracing := artifactFile(t, covalArtifact(t, provider), "tracing.py")
		if !strings.Contains(tracing, `TRACE_NAME = AGENT_NAME + "-local" if os.environ.get(LOCAL_RUN_ENV) else AGENT_NAME`) {
			t.Errorf("%s tracing.py does not derive the local label from the marker at run time", provider)
		}
		// Baked in at compile time, one build could not serve both.
		identity := regexp.MustCompile(`AGENT_NAME = (.+)`).FindStringSubmatch(tracing)
		if identity == nil {
			t.Fatalf("%s tracing.py declares no AGENT_NAME", provider)
		}
		if strings.Contains(identity[1], "-local") {
			t.Errorf("%s tracing.py bakes the local mark into the deployed identity: %s", provider, identity[1])
		}
	}
	// FR-004, restated against the new derivation because this is the expression
	// TestCovalTracingNamesEachTargetsService used to read.
	pipecat := artifactFile(t, covalArtifact(t, ir.ProviderPipecat), "tracing.py")
	livekit := artifactFile(t, covalArtifact(t, ir.ProviderLiveKit), "tracing.py")
	pipecatName := regexp.MustCompile(`AGENT_NAME = (.+)`).FindStringSubmatch(pipecat)[1]
	livekitName := regexp.MustCompile(`AGENT_NAME = (.+)`).FindStringSubmatch(livekit)[1]
	if pipecatName == livekitName {
		t.Errorf("both targets now derive the same identity %s, so Coval cannot tell them apart", pipecatName)
	}
}

// Three places name the local-run marker: the Go that sets it and the two
// templates that read it. Without this gate that is three owners of one string,
// and a rename in one of them makes every local run label itself as deployed.
func TestCovalTracingOwnsTheLocalRunMarker(t *testing.T) {
	if LocalRunEnv == "" {
		t.Fatal("generate.LocalRunEnv is empty")
	}
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		tracing := artifactFile(t, covalArtifact(t, provider), "tracing.py")
		declared := regexp.MustCompile(`LOCAL_RUN_ENV = "([^"]+)"`).FindStringSubmatch(tracing)
		if declared == nil {
			t.Fatalf("%s tracing.py declares no LOCAL_RUN_ENV", provider)
		}
		if declared[1] != LocalRunEnv {
			t.Errorf("%s tracing.py reads %q but generate.LocalRunEnv is %q, so a local run would label itself as deployed", provider, declared[1], LocalRunEnv)
		}
	}
}

// Coval's Trace Search filters on the agent attribute and on tags. It has no
// service.name filter, so a label that reached only the service name would be
// visible in a trace and unfilterable across them, which is the whole point of
// marking local runs.
func TestCovalTracingLabelsEveryPlaceCovalFilters(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		tracing := artifactFile(t, covalArtifact(t, provider), "tracing.py")
		want := []string{
			`"agent": TRACE_NAME`,  // the conversation's own metadata
			`"tags": [TRACE_NAME]`, // what Trace Search filters on
			`"agent.name": TRACE_NAME`,
		}
		// The service name is set differently per target, by each framework.
		if provider == ir.ProviderPipecat {
			want = append(want, "setup_tracing(service_name=TRACE_NAME")
		} else {
			want = append(want, "SERVICE_NAME: TRACE_NAME")
		}
		for _, w := range want {
			if !strings.Contains(tracing, w) {
				t.Errorf("%s tracing.py does not label %s", provider, w)
			}
		}
	}
}

// An operator looking at a call in Coval should be able to tell how it arrived
// without decoding a session id. The values are fixed so the four documentation
// surfaces and both targets can agree on them.
func TestCovalTracingRecordsHowTheCallArrived(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		tracing := artifactFile(t, covalArtifact(t, provider), "tracing.py")
		for _, want := range []string{
			`CALL_ORIGIN_ATTR = "coval.call.origin"`,
			"def resolve_call_origin(",
			`return "browser"`,
			`"origin"`, // carried on the conversation metadata too
		} {
			if !strings.Contains(tracing, want) {
				t.Errorf("%s tracing.py does not record the call origin: %q", provider, want)
			}
		}
		if !strings.Contains(tracing, `"phone"`) {
			t.Errorf("%s tracing.py never reports a carrier call as phone", provider)
		}
	}
	// Only Pipecat can receive a non-carrier websocket session; a LiveKit job
	// always arrives over the room, so it has no websocket origin to report.
	if !strings.Contains(artifactFile(t, covalArtifact(t, ir.ProviderPipecat), "tracing.py"), `"websocket"`) {
		t.Error("pipecat tracing.py never reports a plain websocket session")
	}
}

// The line this replaces said `No Coval simulation ID on this call, so no trace
// is exported` on every real call. It is false: the conversation route exports
// the trace at the call's end. That one sentence is what confirmed the wrong
// conclusion during the investigation that produced this feature, so it is
// forbidden by shape rather than by string.
func TestCovalTracingLogsWhatActuallyHappened(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderPipecat, ir.ProviderLiveKit} {
		tracing := artifactFile(t, covalArtifact(t, provider), "tracing.py")
		if strings.Contains(tracing, "so no trace is exported") {
			t.Errorf("%s tracing.py still claims no trace is exported on a call the conversation route files", provider)
		}
		for _, want := range []string{
			// FR-005: one startup line, naming provider, destination and key.
			`"Coval tracing on: agent %s, traces to %s, COVAL_API_KEY %s"`,
			`"present" if api_key else "MISSING"`,
			// FR-006: one line per terminal state of the call.
			"No Coval simulation placed this",
			"so its trace is filed as a ",
			"Filed this call as Coval conversation %s with %d span(s)",
			"so its spans are on their way",
			"This call produced no transcript",
			"Could not register this call with Coval",
			"took longer than its %ss budget",
		} {
			if !strings.Contains(tracing, want) {
				t.Errorf("%s tracing.py is missing the log line %q", provider, want)
			}
		}
	}
}

// livekitSpanNames counts, per span name, how many places in the emitted module
// start a span with that name, so the gate above checks what the agent will
// actually create rather than a list restated in the test.
func livekitSpanNames(t *testing.T, tracingPy string) map[string]int {
	t.Helper()
	names := map[string]int{}
	for _, m := range regexp.MustCompile(`(?:start_span|_child)\(\s*"([a-z_.]+)"`).FindAllStringSubmatch(tracingPy, -1) {
		names[m[1]]++
	}
	if len(names) == 0 {
		t.Fatal("livekit tracing.py starts no spans at all")
	}
	return names
}
