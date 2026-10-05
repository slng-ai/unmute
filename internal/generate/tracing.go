package generate

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"

	"github.com/slng-ai/unmute/internal/ir"
)

// LocalRunEnv marks a run as a local `unmute dev` one, so the emitted tracing
// module can label its traces apart from the same build running in the cloud.
// No deploy path sets it.
//
// This constant is the single owner of the name. It lives here, next to the
// template that reads it, because `unmute dev` sets it and the telemetry
// template hardcodes the same string; `TestCovalTracingOwnsTheLocalRunMarker`
// fails if the two ever disagree.
const LocalRunEnv = "UNMUTE_LOCAL_RUN"

// The emitted telemetry module, shared by both drivers.
//
// It owns the one tracer provider, its resource, and the backend a call's
// spans export to, and imports no framework. What differs per driver is which
// spans a call produces, and that stays in each driver's own tracing.py.
//
//go:embed templates/telemetry/*.tmpl
var telemetryTemplates embed.FS

// telemetryData is what the shared module needs: which backend to emit, which
// driver it runs under, and the identity every trace carries.
type telemetryData struct {
	Project  string
	Provider string
	// Target is "livekit" or "pipecat". Only Coval's span processor reads it.
	Target string
	// AgentName is the trace identity, entry agent then worker name, so the
	// two targets of one package never report the same service.
	AgentName string
}

// renderTelemetry renders the shared utils/telemetry.py for either driver.
func renderTelemetry(data telemetryData) ([]byte, error) {
	raw, err := telemetryTemplates.ReadFile("templates/telemetry/telemetry.py.tmpl")
	if err != nil {
		return nil, fmt.Errorf("telemetry template: %w", err)
	}
	tmpl, err := template.New("telemetry.py").Funcs(template.FuncMap{"pyq": pyQuote}).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("telemetry template: %w", err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("telemetry template: %w", err)
	}
	return wrapLongImports(out.Bytes()), nil
}

// telemetryFile is utils/telemetry.py for one driver, or nothing when the
// package configures no tracing.
func telemetryFile(data telemetryData) ([]File, error) {
	if data.Provider == "" {
		return nil, nil
	}
	content, err := renderTelemetry(data)
	if err != nil {
		return nil, err
	}
	return []File{{Path: "utils/telemetry.py", Content: content}}, nil
}

// tracingTemplate names the driver's tracing template. Both drivers emit the
// result as tracing.py, so bot.py and agent.py import from one module name
// whichever provider the package named.
//
// LiveKit's Coval module is its own file because it builds Coval's span tree
// from session events instead of reusing LiveKit's spans. Pipecat's spans
// already fit every backend, so it has one tracing template.
func tracingTemplate(provider string) string {
	if provider == "coval" {
		return "tracing_coval.py"
	}
	return "tracing.py"
}

// tracingEnv is the env a provider needs at run time, read straight off the IR
// so the drivers and the compile report cannot disagree about it. Empty when
// tracing is off, which is what makes the caller a plain range with no branch.
func tracingEnv(provider string) []string {
	return ir.TracingSecrets[provider]
}

// tracingProviderOf is "" when the package configures no tracing, which is what
// every provider comparison downstream relies on.
func tracingProviderOf(agent *ir.Agent) string {
	if agent.Tracing == nil {
		return ""
	}
	return agent.Tracing.Provider
}
