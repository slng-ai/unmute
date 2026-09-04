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
// generate), so naming one early is never silent. requires: is a different
// question: it holds a step back until a value it needs exists, and stays the
// only place that guard lives.
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
		// The inputs this tool's hidden values may read: required, and handed to
		// every site the tool is attached to. Decided once per tool, because the
		// message has to name the site that is not handed the input, and the
		// shared check below knows the tool only as a formatted site string.
		inputs, err := checkToolInputReads(pkg, agent, name, raw)
		if err != nil {
			return err
		}
		for _, key := range sortedKeys(raw.Inject) {
			value, ok := raw.Inject[key].(string)
			if !ok {
				continue
			}
			site := fmt.Sprintf("tool %q inject %q", name, key)
			if err := checkTemplateSite(pkg, agent, file, key, site, value, false, false, inputs...); err != nil {
				return err
			}
		}
		if raw.Webhook != nil && raw.Webhook.Path != "" {
			site := fmt.Sprintf("tool %q webhook.path", name)
			if err := checkTemplateSite(pkg, agent, file, "path:", site, raw.Webhook.Path, false, false, inputs...); err != nil {
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
		variable, ok := agent.Variables[ref]
		if !ok {
			// An expected value is read by the prompt it was handed to and nowhere else.
			// At run time the value sits on the shared call state for its visit,
			// so this refusal is the only thing keeping a step's request out of
			// its parent's prompt. A tool's inject may read one when
			// checkToolInputReads has allowed it.
			if sites := inputSites(agent, ref); len(sites) > 0 {
				if slices.Contains(sites, site) || slices.Contains(alsoAllowed, ref) {
					continue
				}
				return fmt.Errorf("%s: %s references {{%s}}, which is a value %s expects to be handed. Only the prompt that "+
					"expects it may read it: name it there, and here ask the caller or read a declared variable",
					where, site, ref, strings.Join(sites, " and "))
			}
			if slices.Contains(agent.Secrets, ref) || envNamePattern.MatchString(ref) {
				return fmt.Errorf("%s: %s references {{%s}}, but secrets never flow through templates; a secret reaches a tool through its own *_env field", where, site, ref)
			}
			return fmt.Errorf("%s: %s references {{%s}}, which is not a declared variable", where, site, ref)
		}
		if requireNow && !hasSessionStartValue(agent, ref, variable) && !slices.Contains(alsoAllowed, ref) {
			return fmt.Errorf("%s: %s references {{%s}}, which has no value when the prompt is built; give it source: call_start, a system source, or a default", where, site, ref)
		}
		// Refusal 16. A value awaiting confirmation renders in exactly one prompt:
		// the one belonging to the step that confirms it. Everywhere else is a
		// place the model would read a number nobody has agreed to, and the worst
		// version is not a wrong booking, it is greeting a stranger by the account
		// holder's name.
		//
		// Scoped to prompt sites, which is what prompt marks: the greeting, an
		// agent's instructions and a task's instructions all pass true here, while
		// `inject:` and a webhook path pass false. That is not a coincidence worth
		// relying on silently, so: a prompt is a thing the model reads, and this
		// rule is about what the model may read.
		//
		// An `inject:` value is never read by the model at all, it goes straight
		// into a request, so the risk there is different: the request would reach
		// somebody else's record. That one is held at run time by the emitted
		// refusal helper, which treats an unconfirmed name as unset wherever the
		// tool is attached. Refusing it here instead would have made the value
		// unusable by any tool, which is most of what a confirmed number is for.
		if step := variable.Confirm; prompt && step != "" && site != confirmingSite(step) {
			return fmt.Errorf("%s: %s references {{%s}}, which the caller has not confirmed yet. It renders only in "+
				"task %q, the step that confirms it. Read it back there, and name it here only after that step has "+
				"assigned it", where, site, ref, step)
		}
	}
	return nil
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
