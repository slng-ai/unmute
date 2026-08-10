package ir

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	packagespec "github.com/slng/unmute/internal/spec"
)

// CaptureToolName is the generated tool the drivers emit when a package declares
// any source: conversation variable. The name is reserved: a package tool or
// control claiming it would shadow the generated one (V7).
const CaptureToolName = "update_variables"

// systemSources are the runtime-owned variable sources: their value exists
// before the greeting, so a session-start template may reference them (V2).
var systemSources = []VariableSource{
	VariableSourceSessionID, VariableSourceCarrier, VariableSourceConnection,
	VariableSourceCallID, VariableSourceStreamID, VariableSourceDirection,
	VariableSourceFromNumber, VariableSourceToNumber,
}

// IsSystemSource reports whether a source is runtime-owned, meaning the value
// arrives from the telephony route rather than from a dispatch payload or the
// conversation. Both drivers and the telephony plan key off this.
func IsSystemSource(source VariableSource) bool { return slices.Contains(systemSources, source) }

// checkSecrets enforces the secrets block's shape: the key IS the environment
// variable name, so a lower-case or punctuated key is a typo that would
// otherwise become a lookup failing at call time (V8).
func checkSecrets(pkg *packagespec.Package) error {
	for _, name := range sortedKeys(pkg.Agent.Secrets) {
		if !envNamePattern.MatchString(name) {
			return fmt.Errorf("%s: secret %q must be an UPPER_SNAKE environment variable name", pkg.Location("agent.yaml", name), name)
		}
	}
	return nil
}

// checkTemplates walks every template site and resolves each token against the
// declared variables (V1). Greeting and instructions render once at session
// start, so they may only name a variable that has a value by then (V2, C11);
// inject and path render per call, so a conversation variable is fine there.
func checkTemplates(pkg *packagespec.Package, agent *Agent) error {
	if pkg.Agent.Conversation != nil && pkg.Agent.Conversation.Greeting != nil {
		text := pkg.Agent.Conversation.Greeting.Text
		if err := checkTemplateSite(pkg, agent, "agent.yaml", "text:", "conversation.greeting.text", text, true); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(pkg.Agent.Agents) {
		raw := pkg.Agent.Agents[name]
		site := fmt.Sprintf("agent %q instructions", name)
		if err := checkTemplateSite(pkg, agent, raw.Instructions, "", site, pkg.Markdown[raw.Instructions], true); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(pkg.Agent.Tasks) {
		raw := pkg.Agent.Tasks[name]
		site := fmt.Sprintf("task %q instructions", name)
		if err := checkTemplateSite(pkg, agent, raw.Instructions, "", site, pkg.Markdown[raw.Instructions], true); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(pkg.Tools) {
		raw := pkg.Tools[name]
		file := filepath.Join("tools", name+".yaml")
		for _, key := range sortedKeys(raw.Inject) {
			value, ok := raw.Inject[key].(string)
			if !ok {
				continue
			}
			site := fmt.Sprintf("tool %q inject %q", name, key)
			if err := checkTemplateSite(pkg, agent, file, key, site, value, false); err != nil {
				return err
			}
		}
		if raw.Webhook != nil && raw.Webhook.Path != "" {
			site := fmt.Sprintf("tool %q webhook.path", name)
			if err := checkTemplateSite(pkg, agent, file, "path:", site, raw.Webhook.Path, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkTemplateSite resolves one site's tokens. sessionStart marks a site
// rendered once before the call begins.
func checkTemplateSite(pkg *packagespec.Package, agent *Agent, file, token, site, value string, sessionStart bool) error {
	for _, ref := range TemplateRefs(value) {
		where := pkg.Location(file, firstNonBlank(token, "{{"))
		variable, ok := agent.Variables[ref]
		if !ok {
			if _, isSecret := agent.Secrets[ref]; isSecret || envNamePattern.MatchString(ref) {
				return fmt.Errorf("%s: %s references {{%s}}, but secrets never flow through templates; a secret reaches a tool through its own *_env field", where, site, ref)
			}
			return fmt.Errorf("%s: %s references {{%s}}, which is not a declared variable", where, site, ref)
		}
		if sessionStart && !hasSessionStartValue(variable) {
			return fmt.Errorf("%s: %s references {{%s}}, which has no value when the prompt is built; give it source: call_start, a system source, or a default", where, site, ref)
		}
	}
	return nil
}

// hasSessionStartValue reports whether a variable holds a value before the first
// spoken word: dispatched at call start, owned by the runtime, or defaulted.
func hasSessionStartValue(variable Variable) bool {
	if variable.Default != nil {
		return true
	}
	return variable.Source == VariableSourceCallStart || slices.Contains(systemSources, variable.Source)
}

// checkInject enforces where hidden request values are legal: an execution kind
// with no request to merge them into has nowhere to put them, and a key that
// also names a model-visible parameter would let the model overwrite it (V3).
func checkInject(pkg *packagespec.Package) error {
	for _, name := range sortedKeys(pkg.Tools) {
		raw := pkg.Tools[name]
		file := filepath.Join("tools", name+".yaml")
		if len(raw.Inject) > 0 {
			switch raw.ExecutionKind() {
			case "webhook", "local":
			default:
				// An mcp tool's arguments are assembled by the MCP client from the
				// server's schema; neither SDK exposes a per-call hook, so an
				// injected value would be dropped rather than sent.
				return fmt.Errorf("%s: tool %q is a %s tool; inject is legal on webhook and local tools, the two kinds whose request unmute builds itself",
					pkg.Location(file, "inject:"), name, raw.ExecutionKind())
			}
		}
		properties, _ := raw.Input["properties"].(map[string]any)
		for _, key := range sortedKeys(raw.Inject) {
			if _, ok := properties[key]; ok {
				return fmt.Errorf("%s: tool %q injects %q, which is also an input property; an injected value is hidden from the model, so it cannot double as a parameter the model fills in",
					pkg.Location(file, key), name, key)
			}
			if value, ok := raw.Inject[key].(map[string]any); ok && value != nil {
				return fmt.Errorf("%s: tool %q inject %q must be a scalar", pkg.Location(file, key), name, key)
			}
			if value, ok := raw.Inject[key].([]any); ok && value != nil {
				return fmt.Errorf("%s: tool %q inject %q must be a scalar", pkg.Location(file, key), name, key)
			}
		}
		if raw.Webhook != nil && raw.Webhook.Path != "" && !strings.HasPrefix(raw.Webhook.Path, "/") {
			return fmt.Errorf("%s: tool %q webhook.path must start with /", pkg.Location(file, "path:"), name)
		}
	}
	return nil
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
