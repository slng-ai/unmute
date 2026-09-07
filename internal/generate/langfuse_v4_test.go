package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// TestLangfuseSelectsTheV4IngestionPathOnBothTargets holds the one thing that
// decides which data model a call lands in, and it is a header rather than
// anything visible in the spans.
//
// Langfuse reads `x-langfuse-ingestion-version: 4` to route an export to the
// observations-first path. The two targets build their exporter differently and
// each can lose the header on its own: Pipecat sets OTEL_EXPORTER_OTLP_HEADERS
// by hand, while LiveKit lets the Langfuse SDK build the exporter, and that
// exporter's own default headers carry auth and the SDK version and nothing
// else. Without the header the spans still arrive, so nothing fails and nothing
// is logged; they just arrive on the legacy path, which is the failure this
// gate exists to catch.
func TestLangfuseSelectsTheV4IngestionPathOnBothTargets(t *testing.T) {
	const header = "x-langfuse-ingestion-version"
	for _, tc := range []struct {
		provider ir.Provider
		want     string
	}{
		{ir.ProviderLiveKit, `additional_headers={"x-langfuse-ingestion-version": "4"}`},
		{ir.ProviderPipecat, `x-langfuse-ingestion-version=4`},
	} {
		tracing := langfuseTracingModule(t, tc.provider)
		if !strings.Contains(tracing, tc.want) {
			t.Errorf("%s tracing.py does not select the v4 ingestion path: missing %q", tc.provider, tc.want)
		}
		if got := strings.Count(tracing, header); got != 1 {
			t.Errorf("%s tracing.py names %s %d times, want 1", tc.provider, header, got)
		}
	}
}

// TestLangfuseWritesNoDeprecatedTraceIO covers the other half of the v4 data
// model. A trace is no longer an entity, only the observations sharing a trace
// ID, so `langfuse.trace.input` and `langfuse.trace.output` reach nothing an
// observation-level evaluator can read. What was said belongs on observations,
// and both targets have to agree on that or one of them silently stops
// recording what the call was about.
func TestLangfuseWritesNoDeprecatedTraceIO(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		tracing := langfuseTracingModule(t, provider)
		for _, retired := range []string{"langfuse.trace.input", "langfuse.trace.output"} {
			if strings.Contains(tracing, retired) {
				t.Errorf("%s tracing.py still writes the deprecated %q", provider, retired)
			}
		}
		if !strings.Contains(tracing, `def said(self, role: str, text: str) -> None:`) {
			t.Errorf("%s tracing.py records nothing that was said", provider)
		}
	}
}

// TestLangfuseCorrelatingAttributesReachEverySpan is the reason a span
// processor is emitted at all. Langfuse v4 answers questions over observations,
// so a session ID that sits only on the top span leaves its own generations
// unfilterable and its cost unaddable. Neither framework does this for us:
// Pipecat's additional_span_attributes reach the conversation span alone, and
// livekit-agents stopped honouring the metadata argument inside a job in 1.8.0.
func TestLangfuseCorrelatingAttributesReachEverySpan(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		tracing := langfuseTracingModule(t, provider)
		for _, want := range []string{
			"class CallTrace(SpanProcessor):",
			"def on_start(self, span: Span, parent_context: Context | None = None) -> None:",
			"span.set_attributes(self._attributes)",
			"add_span_processor(",
		} {
			if !strings.Contains(tracing, want) {
				t.Errorf("%s tracing.py missing %q", provider, want)
			}
		}
	}
}

// TestLangfuseKeepsTheWholeCallInOneTrace holds the trace shape, and it is the
// shape a reader actually uses.
//
// One trace per turn was tried and reverted on 2026-09-07 after looking at real
// calls: the turns themselves read well, but the top of the call was left an
// empty envelope with no input, no output and nothing but lifecycle spans under
// it, so there was nowhere to see the conversation whole or add it up. A trace
// is the unit you aggregate over, so the call is the trace.
//
// Inside it each exchange gets a `turn` span to open. LiveKit has no span
// covering one, since `user_turn` closes when the caller stops speaking and
// `agent_turn` is its sibling, so that target emits its own and patches the
// provider to put livekit's turn spans inside it. Pipecat already nests `turn`
// under `conversation` and is left alone.
func TestLangfuseKeepsTheWholeCallInOneTrace(t *testing.T) {
	for _, tc := range []struct {
		provider ir.Provider
		want     []string
		forbid   []string
	}{
		{ir.ProviderLiveKit, []string{
			`TURN_SPANS = ("user_turn", "agent_turn")`,
			// Inside the call's trace, not a new one.
			`self._tracer.start_span("turn", context=self._call_context)`,
			"def install_turn_spans(provider: TracerProvider, call: CallTrace) -> None:",
			"install_turn_spans(trace_provider, call)",
			// The provider, because livekit decorates llm_node at import time
			// and a tracer patch would miss it silently.
			"provider.get_tracer = lambda",
		}, []string{
			// A turn rooted on an empty Context starts its own trace, which is
			// exactly the shape this reverted.
			`start_span("turn", context=Context())`,
		}},
		{ir.ProviderPipecat, []string{
			`CALL_SPAN = "conversation"`,
			`TURN_SPAN = "turn"`,
		}, []string{
			// Pipecat's tree is already right, so nothing may re-parent it.
			"provider.get_tracer = lambda",
			"class TurnRootTracer:",
		}},
	} {
		tracing := langfuseTracingModule(t, tc.provider)
		for _, want := range tc.want {
			if !strings.Contains(tracing, want) {
				t.Errorf("%s tracing.py missing %q", tc.provider, want)
			}
		}
		for _, forbid := range tc.forbid {
			if strings.Contains(tracing, forbid) {
				t.Errorf("%s tracing.py splits the call into one trace per turn: %q", tc.provider, forbid)
			}
		}
		// The root observation is where a reader opens the call, so it carries
		// the conversation rather than being an envelope of lifecycle spans.
		if !strings.Contains(tracing, `"langfuse.observation.input", json.dumps(self._transcript)`) {
			t.Errorf("%s tracing.py leaves the call's root observation empty", tc.provider)
		}
	}
}

func langfuseTracingModule(t *testing.T, provider ir.Provider) string {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	enableLangfuse(agent)
	artifact, err := Generate(agent, targetByProvider(t, agent, provider), target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifactFile(t, artifact, "tracing.py")
}
