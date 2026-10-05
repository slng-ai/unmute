package generate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// One collector, one backend. Every provider on every code target emits the
// shared telemetry module with its own backend class and no other, the same
// entry point, and plain OTLP with no backend SDK.
func TestTracingEmitsOneBackendBehindOneEntryPoint(t *testing.T) {
	backends := map[string]string{"langfuse": "Langfuse", "logfire": "Logfire", "coval": "Coval"}
	enable := map[string]func(*ir.Agent){"langfuse": enableLangfuse, "logfire": enableLogfire, "coval": enableCoval}
	for provider, class := range backends {
		for _, driver := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
			pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := buildWithState(t, pkg)
			if err != nil {
				t.Fatal(err)
			}
			enable[provider](agent)
			artifact, err := Generate(agent, targetByProvider(t, agent, driver), target.Default())
			if err != nil {
				t.Fatal(err)
			}
			name := provider + " on " + string(driver)
			telemetry := artifactFile(t, artifact, "utils/telemetry.py")
			if !strings.Contains(telemetry, "class "+class+"(Backend):") || !strings.Contains(telemetry, "BACKEND: type[Backend] = "+class) {
				t.Errorf("%s: telemetry.py does not emit the %s backend", name, class)
			}
			for other, otherClass := range backends {
				if other != provider && strings.Contains(telemetry, "class "+otherClass+"(Backend):") {
					t.Errorf("%s: telemetry.py also emits the %s backend", name, otherClass)
				}
			}
			if !strings.Contains(telemetry, "class Telemetry:") {
				t.Errorf("%s: telemetry.py has no collector", name)
			}
			// Only telemetry.py builds an exporter or a provider.
			tracing := artifactFile(t, artifact, "utils/tracing.py")
			for _, forbidden := range []string{"OTLPSpanExporter(", "TracerProvider("} {
				if strings.Contains(tracing, forbidden) {
					t.Errorf("%s: tracing.py builds its own %s", name, forbidden)
				}
			}
			if !strings.Contains(tracing, "def setup_tracing(") {
				t.Errorf("%s: tracing.py has no setup_tracing entry point", name)
			}
			pyproject := artifactFile(t, artifact, "pyproject.toml")
			if !strings.Contains(pyproject, "opentelemetry-exporter-otlp-proto-http") {
				t.Errorf("%s: pyproject.toml does not declare the OTLP HTTP exporter", name)
			}
			if strings.Contains(pyproject, "langfuse") || strings.Contains(pyproject, "logfire") {
				t.Errorf("%s: pyproject.toml declares a backend SDK", name)
			}
		}
	}
}

// A package with no tracing emits neither module.
func TestTracingOffEmitsNoTelemetry(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := buildWithState(t, pkg)
	if err != nil {
		t.Fatal(err)
	}
	agent.Tracing = nil
	for _, driver := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		artifact, err := Generate(agent, targetByProvider(t, agent, driver), target.Default())
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{"utils/telemetry.py", "utils/tracing.py"} {
			if artifactHasFile(artifact, path) {
				t.Errorf("%s emits %s with tracing off", driver, path)
			}
		}
	}
}

// Logfire groups a call into agent runs and counts a model call toward one only
// when it sits directly under the run's span. The Logfire build reads through
// LiveKit's llm_node, makes Pipecat's turn a run, and labels every row; the
// Langfuse build does none of it, so a Langfuse trace keeps its proven shape.
func TestLogfireShapesTheAgentBreakdown(t *testing.T) {
	for _, tc := range []struct {
		provider string
		enable   func(*ir.Agent)
		want     []string
		forbid   []string
	}{
		{"logfire", enableLogfire, []string{
			"class LogfireExporter(SpanExporter):",
			`transparent_spans: ClassVar[frozenset[str]] = frozenset({"llm_node"})`,
			`return {"gen_ai.operation.name": "invoke_agent", "gen_ai.agent.name": agent_name}`,
			`attributes["gen_ai.conversation.id"] = session_id`,
			`attributes["logfire.msg"] = message`,
			"def genai_messages(",
		}, nil},
		{"langfuse", enableLangfuse, []string{
			"transparent_spans: ClassVar[frozenset[str]] = frozenset()",
		}, []string{"LogfireExporter", "logfire.msg", `frozenset({"llm_node"})`}},
	} {
		for _, driver := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
			pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := buildWithState(t, pkg)
			if err != nil {
				t.Fatal(err)
			}
			tc.enable(agent)
			artifact, err := Generate(agent, targetByProvider(t, agent, driver), target.Default())
			if err != nil {
				t.Fatal(err)
			}
			source := artifactFile(t, artifact, tracingSource)
			for _, want := range tc.want {
				if !strings.Contains(source, want) {
					t.Errorf("%s on %s missing %q", tc.provider, driver, want)
				}
			}
			for _, forbid := range tc.forbid {
				if strings.Contains(source, forbid) {
					t.Errorf("%s on %s carries Logfire shaping %q", tc.provider, driver, forbid)
				}
			}
			// The two seams each target uses to give Logfire its agent runs.
			seam := map[ir.Provider]string{
				ir.ProviderLiveKit: "return self._lifted.get(parent, context)",
				ir.ProviderPipecat: "span.set_attributes(self._backend.agent_run(TRACE_NAME))",
			}[driver]
			if !strings.Contains(source, seam) {
				t.Errorf("%s on %s missing %q", tc.provider, driver, seam)
			}
		}
	}
}
