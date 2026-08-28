package generate

import "github.com/slng-ai/unmute/internal/ir"

// LocalRunEnv marks a run as a local `unmute dev` one, so the emitted tracing
// module can label its traces apart from the same build running in the cloud.
// No deploy path sets it.
//
// This constant is the single owner of the name. It lives here, next to the
// templates that read it, because `unmute dev` sets it and both tracing
// templates hardcode the same string; `TestCovalTracingOwnsTheLocalRunMarker`
// fails if any of the three ever disagrees.
const LocalRunEnv = "UNMUTE_LOCAL_RUN"

// tracingTemplate names the per-provider tracing template. Both code drivers
// emit the result as tracing.py, so bot.py and agent.py import from one module
// name whichever provider the package named.
//
// ponytail: a lookup, not a registry. A third provider adds one line here and
// one template file; if that ever stops being true, the shape can change then.
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
