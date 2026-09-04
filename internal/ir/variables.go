package ir

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	packagespec "github.com/slng-ai/unmute/internal/spec"
)

// CaptureToolName is the generated tool the drivers emit when a package declares
// any source: conversation variable. The name is reserved: a package tool or
// control claiming it would shadow the generated one (V7).
const CaptureToolName = "update_variables"

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
	// An agent prompt is a session-start site, with one exception: a router-bound
	// one is never rendered here at all. It travels to the SLNG Context Router
	// with its placeholders intact and the router substitutes them, once per
	// request, from the values sent beside it. So "no value when the prompt is
	// built" does not describe that site: there is no build, and the value the
	// request carries is the one the call holds at that moment.
	//
	// Which makes a value the call learns later exactly the case worth authoring
	// there: a name the caller offers mid-conversation, or a field a task
	// assigns. Before this exception the only way to write either was to render
	// the value into the prompt text, which is the thing that stops the answer
	// being cached at all.
	//
	// So the allowance is every declared variable rather than a chosen subset.
	// The restriction exists because a session-start render of an unset variable
	// produces a prompt with a hole in it; a router-bound prompt has no
	// session-start render to produce one.
	allVariables := sortedKeys(agent.Variables)
	for _, name := range sortedKeys(pkg.Agent.Agents) {
		raw := pkg.Agent.Agents[name]
		site := AgentPromptSite(name)
		var lateBound []string
		if routerPrompt(pkg, agent, name) {
			lateBound = allVariables
		}
		if err := checkTemplateSite(pkg, agent, raw.Instructions, "", site, pkg.Markdown[raw.Instructions], true, lateBound...); err != nil {
			return err
		}
	}
	// A task prompt is not a session-start site. Both drivers render it when the
	// task is entered, which is mid-call and after any earlier task has written
	// its result into the variables (livekit_v1_build.go promptExpr on the task,
	// pipecat's per-worker render). So a task may name a variable a task assigns:
	// that is the whole point of `assign:`, and forbidding it left the mapping
	// with no reader anywhere in the package. A variable still unset at that
	// moment renders empty, never the word "None", so the prompt can say what
	// empty means (B: multi-task booked nothing because the appointment task
	// could not see the customer id, 2026-08-15).
	//
	// The allowance is the step's own `requires:` list, not every variable any
	// step assigns anywhere in the package. The global version let a later
	// prompt name a value with nothing checking that the step which produces it
	// had run, and the symptom is not a failure: _render substitutes an empty
	// string, so the model reads a prompt with a hole in it mid-call. Narrowing
	// it needed no package edit, because every prompt in the tree names only
	// pre-fetched values, which are session-start values and were never using
	// the allowance.
	suppliers := assigners(agent)
	for _, name := range sortedKeys(pkg.Tasks) {
		raw := pkg.Tasks[name]
		site := TaskPromptSite(name)
		if err := checkTaskPromptReads(pkg, agent, raw, name, site, suppliers); err != nil {
			return err
		}
		if err := checkTemplateSite(pkg, agent, raw.Instructions, "", site, pkg.Markdown[raw.Instructions], true, raw.Requires...); err != nil {
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

// routerPrompt reports whether this agent's think profile sends its prompt to the
// SLNG Context Router, which is what makes the prompt a late-bound site rather
// than a session-start one.
//
// Read from the package's own models block rather than from a resolved target,
// because this check runs once for the package. A per-target override that turns
// the profile into a direct provider makes the prompt render locally again, and
// an unset variable there renders empty rather than failing, the same way a task
// prompt has always behaved.
func routerPrompt(pkg *packagespec.Package, agent *Agent, name string) bool {
	profile := pkg.Agent.Agents[name].Think
	if profile == "" {
		profile = pkg.Agent.Agents[agent.EntryAgent].Think
	}
	return pkg.Agent.Models.Think[profile].Provider == ProviderSlngRouter
}

// assigners pairs each variable a step writes from a task result with the steps
// that write it. They hold no value at session start and a value later in the
// call, so what matters about one is which step produces it.
//
// The list, rather than one name, is what lets a check tell "some other step
// fills this" from "only this one does", which are different mistakes with
// different fixes. Keyed on the control name because that is what the model
// calls and what the emitted guard's _PREREQUISITE_SUPPLIER map carries
// (internal/generate/guard.go), so a compile refusal and the run-time refusal
// name the same thing.
//
// generate.SupplierIndex builds the same pairing and cannot be shared with this:
// generate imports ir and not the reverse. It keeps the sorted-first supplier
// per variable, which is this map's first element.
func assigners(agent *Agent) map[string][]string {
	suppliers := map[string][]string{}
	for _, name := range sortedKeys(agent.Controls) {
		delegate, ok := agent.Controls[name].(*Delegate)
		if !ok {
			continue
		}
		for _, variable := range AssignedVars(delegate.Assign) {
			suppliers[variable] = append(suppliers[variable], name)
		}
	}
	return suppliers
}

// checkTaskPromptReads refuses a task prompt naming a value only some step
// produces, unless the step that reads it declares the need.
//
// The declaration is `requires:`, which already holds the step back until the
// value exists. One key, so the prompt and the guard cannot disagree: a prompt
// that reads a value the guard does not wait for renders an empty string in the
// middle of a call, and nothing fails.
//
// Its own walk rather than another argument to checkTemplateSite, because the
// message has to name the step that supplies the value and the step that reads
// it, and the shared function knows neither: it has the reader only as a
// formatted site string. Leaving that function alone also keeps refusal 16
// provably where it was, which matters because the two decide the same call.
//
// Anything not recognised here falls through to checkTemplateSite, which owns
// the vault token, the undeclared name, and the plain "no value yet".
func checkTaskPromptReads(pkg *packagespec.Package, agent *Agent, raw packagespec.Task, task, site string, suppliers map[string][]string) error {
	for _, ref := range TemplateRefs(pkg.Markdown[raw.Instructions]) {
		if _, vault := VaultToken(ref); vault {
			continue
		}
		variable, declared := agent.Variables[ref]
		if !declared || hasSessionStartValue(agent, ref, variable) || slices.Contains(raw.Requires, ref) {
			continue
		}
		steps := suppliers[ref]
		if len(steps) == 0 {
			continue
		}
		where := pkg.Location(raw.Instructions, "{{")
		others := slices.DeleteFunc(slices.Clone(steps), func(step string) bool { return step == task })
		if len(others) == 0 {
			// Adding it to this step's own requires: would be a guard waiting on
			// the step's own output, so the advice is the opposite one.
			return fmt.Errorf("%s: %s references {{%s}}, and %q is the only step that assigns it, so the value "+
				"does not exist while this prompt is being built. Give the variable a default or a source:, or "+
				"assign it from an earlier step", where, site, ref, task)
		}
		return fmt.Errorf("%s: %s references {{%s}}, which only task %q assigns. Add %s to this task's requires: "+
			"list, so the step waits for the value and its prompt can read it", where, site, ref, others[0], ref)
	}
	return nil
}

// checkTemplateSite resolves one site's tokens. sessionStart marks a site
// rendered once before the call begins; alsoAllowed names variables that are
// legal at this site even though they hold no value at session start.
func checkTemplateSite(pkg *packagespec.Package, agent *Agent, file, token, site, value string, sessionStart bool, alsoAllowed ...string) error {
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
			if slices.Contains(agent.Secrets, ref) || envNamePattern.MatchString(ref) {
				return fmt.Errorf("%s: %s references {{%s}}, but secrets never flow through templates; a secret reaches a tool through its own *_env field", where, site, ref)
			}
			return fmt.Errorf("%s: %s references {{%s}}, which is not a declared variable", where, site, ref)
		}
		if sessionStart && !hasSessionStartValue(agent, ref, variable) && !slices.Contains(alsoAllowed, ref) {
			return fmt.Errorf("%s: %s references {{%s}}, which has no value when the prompt is built; give it source: call_start, a system source, or a default", where, site, ref)
		}
		// Refusal 16. A value awaiting confirmation renders in exactly one prompt:
		// the one belonging to the step that confirms it. Everywhere else is a
		// place the model would read a number nobody has agreed to, and the worst
		// version is not a wrong booking, it is greeting a stranger by the account
		// holder's name.
		//
		// Scoped to prompt sites, which is what sessionStart marks: the greeting,
		// an agent's instructions and a task's instructions all pass true here,
		// while `inject:` and a webhook path pass false. That is not a coincidence
		// worth relying on silently, so: a prompt is a thing the model reads, and
		// this rule is about what the model may read.
		//
		// An `inject:` value is never read by the model at all, it goes straight
		// into a request, so the risk there is different: the request would reach
		// somebody else's record. That one is held at run time by the emitted
		// refusal helper, which treats an unconfirmed name as unset wherever the
		// tool is attached. Refusing it here instead would have made the value
		// unusable by any tool, which is most of what a confirmed number is for.
		if step := variable.Confirm; sessionStart && step != "" && site != confirmingSite(step) {
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
