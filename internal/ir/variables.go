package ir

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	packagespec "github.com/slng-ai/unmute/internal/spec"
)

// UnservedResultField is the one result field the drivers add themselves. Every
// generated task finish takes it, optional and empty by default, so a step can
// name the request it could not serve on its way out instead of refusing in
// place. The owning agent reads it off the returned result and routes. The name
// is reserved: a task result claiming it would collide with the generated
// argument (B: salon compound request, 2026-08-20).
const UnservedResultField = "unserved_request"

// UnservedResultDescription is what the model reads when it decides whether to
// fill the field, so it says both what goes in it and when to leave it out.
const UnservedResultDescription = "Leave empty unless the caller asked for something this step cannot serve. Then put that request here in one short plain sentence, in the caller's own terms, so the agent that owns this step can take it."

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

// checkSecrets enforces the secrets block's shape: an entry IS the environment
// variable name, so a lower-case or punctuated one is a typo that would
// otherwise become a lookup failing at call time (V8). A repeat is a typo too,
// and a list, unlike the map this used to be, cannot catch one on its own.
func checkSecrets(pkg *packagespec.Package) error {
	seen := make(map[string]bool, len(pkg.Agent.Secrets))
	for _, name := range pkg.Agent.Secrets {
		if !envNamePattern.MatchString(name) {
			// The offending text is never repeated back. What lands in this slot
			// when it is wrong is usually a pasted credential, and a refusal that
			// quotes it puts the value in a terminal, a CI log, and a bug report.
			// The location is enough to find the line (Wave B, 2026-08-15).
			return fmt.Errorf("%s: this secret is not an UPPER_SNAKE environment variable name. A secret is a name, never a value: "+
				"put the value in .env and list only the name here", pkg.Location("agent.yaml", name))
		}
		if seen[name] {
			return fmt.Errorf("%s: secret %q is declared twice", pkg.Location("agent.yaml", name), name)
		}
		seen[name] = true
	}
	return nil
}

// checkTemplates walks every template site and resolves each token against the
// declared variables (V1).
//
// The greeting is the one site rendered before the call begins, so it may only
// name a variable that already has a value by then (V2, C11): a hole in a
// prompt nobody built yet is silent, and the greeting is the only render with
// no later turn to catch up on. Every other site renders mid-call: an agent
// prompt on entry (and again after one of its own steps writes state), a task
// prompt when the step is entered, inject and a webhook path on every call.
// So each of those may name any declared variable, including one only a later
// step ever assigns — that is the whole point of assign: reading it back
// somewhere with no message history. A variable with nothing in it yet renders
// as words, never as a hole (_state_text and _render's plain fallback, both in
// generate), so naming one early is never silent.
func checkTemplates(pkg *packagespec.Package, agent *Agent) error {
	if pkg.Agent.Conversation != nil && pkg.Agent.Conversation.Greeting != nil {
		text := pkg.Agent.Conversation.Greeting.Text
		if err := checkTemplateSite(pkg, agent, "agent.yaml", "text:", "conversation.greeting.text", text, true, true); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(pkg.Agent.Agents) {
		raw := pkg.Agent.Agents[name]
		site := AgentPromptSite(name)
		if err := checkTemplateSite(pkg, agent, raw.Instructions, "", site, pkg.Markdown[raw.Instructions], true, false); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(pkg.Tasks) {
		raw := pkg.Tasks[name]
		site := TaskPromptSite(name)
		if err := checkTemplateSite(pkg, agent, raw.Instructions, "", site, pkg.Markdown[raw.Instructions], true, false); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(pkg.Tools) {
		raw := pkg.Tools[name]
		file := filepath.Join("tools", name+".yaml")
		for _, pair := range raw.Inject {
			key := pair.Key
			value, ok := pair.Value.(string)
			if !ok {
				continue
			}
			site := fmt.Sprintf("tool %q inject %q", name, key)
			if err := checkTemplateSite(pkg, agent, file, key, site, value, false, false); err != nil {
				return err
			}
		}
		if raw.Webhook != nil && raw.Webhook.Path != "" {
			site := fmt.Sprintf("tool %q webhook.path", name)
			if err := checkTemplateSite(pkg, agent, file, "path:", site, raw.Webhook.Path, false, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkTemplateSite resolves one site's tokens. prompt marks a site the model
// reads, which scopes refusal 16 to the sites it exists to protect. requireNow
// marks the one site rendered before the call begins, where a variable with no
// value yet would leave a hole in the text rather than a later turn to fill it
// in; alsoAllowed names variables that are legal there even so.
func checkTemplateSite(pkg *packagespec.Package, agent *Agent, file, token, site, value string, prompt, requireNow bool, alsoAllowed ...string) error {
	for _, ref := range TemplateRefs(value) {
		where := pkg.Location(file, firstNonBlank(token, "{{"))
		// A {{$NAME}} token is a SLNG Vault variable, not a package variable, and
		// it is not declared anywhere in the package by design: SLNG holds the
		// value and substitutes it at run time.
		//
		// It passes here on every target, because Build runs once for the whole
		// package and does not know which targets are named. Which targets can
		// resolve one is a per-target question, answered by validateVaultTokens.
		// Refusing it here would refuse a legal slng package for having also named
		// a livekit target.
		if name, vault := VaultToken(ref); vault {
			if !ValidVaultName(name) {
				return fmt.Errorf("%s: %s references the SLNG Vault variable {{$%s}}, and %q is not a name SLNG's secret store accepts: a Vault name is uppercase, starts with a letter, and is at most 64 characters, like ACME_API_KEY", where, site, name, name)
			}
			continue
		}
		// A reference is a root and, when it carries a path, the fields the path
		// walks: {{customer.status}} is the variable customer read at its field
		// status. Every rule here is about the root, because the root is the
		// value the site reads; the path is resolved last, against the root's
		// declared shape, and only decides which part is rendered.
		root, fields := PathRoot(ref), PathFields(ref)
		variable, ok := agent.Variables[root]
		if !ok {
			if slices.Contains(agent.Secrets, root) || envNamePattern.MatchString(root) {
				return fmt.Errorf("%s: %s references {{%s}}, but secrets never flow through templates; a secret reaches a tool through its own *_env field", where, site, ref)
			}
			return fmt.Errorf("%s: %s references {{%s}}, which is not a declared variable", where, site, ref)
		}
		if requireNow && !hasSessionStartValue(agent, root, variable) && !slices.Contains(alsoAllowed, root) {
			return fmt.Errorf("%s: %s references {{%s}}, which has no value when the prompt is built; give it source: call_start, a system source, or a default", where, site, ref)
		}
		if len(fields) > 0 {
			if err := checkPathFields(agent.Shapes, root, string(variable.Type), variable.Shape, fields); err != nil {
				return fmt.Errorf("%s: %s references {{%s}}: %w", where, site, ref, err)
			}
		}
	}
	return nil
}

// checkPathFields resolves the fields a placeholder walks after its root. The
// root's own type decides whether there is anything to walk: a plain type, a
// text type such as Phone, a literal set and a list have no fields, and each is
// refused naming the type and the whole name to write instead. The list case is
// caught here rather than left to FieldPath so the message can name the root,
// which FieldPath never sees. Past the root, FieldPath's own messages apply: an
// unknown field lists the fields the shape declares, a list partway down says
// nothing names its entry, and a plain field partway down says it has no fields.
// A token carrying anything but names and dots ends up here too, and is refused
// as an unknown field with the text as written: a placeholder carries no logic.
func checkPathFields(shapes map[string]Shape, root, plain string, typ *TypeRef, fields []string) error {
	switch {
	case typ == nil:
		return fmt.Errorf("%s is a plain %s with no fields to name; write {{%s}}", root, plain, root)
	case typ.IsList():
		return fmt.Errorf("%s is %s, and a path cannot name a field inside a list: nothing says which entry it "+
			"means. Record the entry you need into its own variable with assign: on the step that records it, "+
			"and name that variable here", root, typ.String())
	}
	if _, ok := shapes[typ.Shape]; !ok {
		return fmt.Errorf("%s is %s, which has no fields to name; write {{%s}}", root, typ.String(), root)
	}
	_, err := FieldPath(shapes, typ, fields)
	return err
}

// hasSessionStartValue reports whether a variable holds a value before the first
// spoken word: dispatched at call start, owned by the runtime, defaulted, or
// resolved by a prefetch entry.
//
// The prefetch case is what lets a pre-fetched value render in a session-start
// prompt at all. It is a *may hold* rather than a *does hold*: an entry whose
// inputs are empty is skipped and the variable keeps its default, which is why
// FR-013 also requires every prompt to read as a whole sentence when the value
// renders empty. The alternative, refusing the render, would make the whole
// feature unusable on any route that supplies no caller ID.
func hasSessionStartValue(agent *Agent, name string, variable Variable) bool {
	if variable.Default != nil {
		return true
	}
	if prefetchAssigns(agent, name) {
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
			// A hosted tool joins these two, because all three have a request
			// unmute assembles: the code targets build the call from the
			// mirror's schema, and the slng reference carries the values as
			// argument_overrides on the attachment.
			case "webhook", "local", "slng":
			default:
				// An mcp tool's arguments are assembled by the MCP client from the
				// server's schema; neither SDK exposes a per-call hook, so an
				// injected value would be dropped rather than sent.
				return fmt.Errorf("%s: tool %q is a %s tool; inject is legal on webhook, local and slng tools, the kinds whose call unmute assembles itself",
					pkg.Location(file, "inject:"), name, raw.ExecutionKind())
			}
		}
		properties, _ := raw.Input["properties"].(map[string]any)
		seen := make(map[string]bool)
		required, _ := stringSlice(raw.Input["required"])
		for _, pair := range raw.Inject {
			key := pair.Key
			if seen[key] {
				return fmt.Errorf("%s: inject names %q twice", pkg.Location(file, "inject:"), key)
			}
			seen[key] = true
			if slices.Contains(required, key) {
				return fmt.Errorf("%s: injected key %q must be absent from input.required", pkg.Location(file, "inject:"), key)
			}
			if _, ok := properties[key]; ok {
				return fmt.Errorf("%s: tool %q injects %q, which is also an input property; an injected value is hidden from the model, so it cannot double as a parameter the model fills in",
					pkg.Location(file, key), name, key)
			}
			if value, ok := pair.Value.(map[string]any); ok && value != nil {
				return fmt.Errorf("%s: tool %q inject %q must be a scalar", pkg.Location(file, key), name, key)
			}
			if value, ok := pair.Value.([]any); ok && value != nil {
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
