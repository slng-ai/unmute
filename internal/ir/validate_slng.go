package ir

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	targetcap "github.com/slng-ai/unmute/internal/target"
)

// The checks only a slng target makes. They sit beside validate.go rather than
// inside it for the reason internal/target/slng_target.go sits beside table.go:
// the platform-specific facts and the platform-specific refusals have one home,
// and validate.go stays about the rules every target shares.
//
// Every message here goes through target.SlngDiagnostic, which prefixes it with
// "slng target". That prefix is not decoration: `slng` is already a model vendor
// and a router provider in this repository, so an unprefixed message about the
// target sends the reader to the wrong file (research R13, spec FR-004).

var templateToken = regexp.MustCompile(`\{\{[^}]*\}\}`)

// validateSlngTarget is the whole slng-specific pass, called from validateTarget
// once the shared checks have run.
func validateSlngTarget(agent *Agent, resolved Target, row *TargetValidation) {
	validateSlngRegions(resolved, row)
	validateSlngFallbacks(agent, row)
	validateSlngVariables(agent, row)
	for _, name := range slices.Sorted(maps.Keys(agent.Tools)) {
		validateSlngTool(name, agent.Tools[name], row)
		validateSlngInject(agent, name, agent.Tools[name], row)
	}
	row.Scope = slngDeferredScope(agent)
}

// slngDeferredScope names what an offline slng validate could not reach, so a
// clean row does not read as a promise this compiler never made.
//
// Scoped to `slng:` hosted tools, which is what FR-003/FR-004 are about: a
// hosted reference's existence, its published description and its argument
// contract are the organisation's, and unmute compiles a package with no
// network and no credential, so none of that is checked here. Not extended to
// MCP servers or builtin capabilities, whose own deferred-check stories belong
// to their own stories (US4 and earlier) rather than to this one. A package
// with no hosted reference has nothing deferred: its slng row is already the
// whole check, and stays silent about scope exactly as it always has.
func slngDeferredScope(agent *Agent) string {
	for _, tool := range agent.Tools {
		if tool.Execution == ToolSlngHosted {
			return "local checks only: a hosted tool's existence, its published description and argument contract, " +
				"and every vault entry it needs, are confirmed by `unmute deploy`, not by this command"
		}
	}
	return ""
}

// validateSlngInject holds an injected value to SLNG's attachment expression
// contract, which is narrower than the pair list the code targets accept.
//
// An `argument_overrides` entry is a fixed scalar or one whole variable token,
// and that is the platform's rule rather than ours: the value is stored on the
// attachment and substituted when a call starts, so there is nothing to
// concatenate a template into. A code target assembles the request itself and
// can interpolate, so this check is per target and the same file keeps working
// on livekit and pipecat.
//
// The refusals below are the shapes that would otherwise reach the platform and
// be rejected at attach time, or worse, be stored and send the literal braces:
//
//   - `- greeting: "Order {{order_number}}"` is embedded interpolation. There is
//     no expression to evaluate, so the model's argument would be the text.
//   - `- limit: null` is the absence of a value written as a value.
//   - `- flags: [a, b]` is a collection, and an override holds one scalar.
//
// A fixed `false` and a fixed `0` are values and are deliberately not on that
// list: refusing them is the bug this check has to avoid, because both read as
// empty to a careless predicate and both are legitimate arguments.
func validateSlngInject(agent *Agent, name string, tool Tool, row *TargetValidation) {
	for _, key := range slices.Sorted(maps.Keys(tool.Inject)) {
		value := tool.Inject[key]
		if value == nil {
			row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
				"tool %q injects %q with no value, and an argument override holds one fixed scalar or one whole variable token: write the value, or drop the entry and let the model supply the argument", name, key))
			continue
		}
		switch typed := value.(type) {
		case string:
			validateSlngInjectText(agent, name, key, typed, row)
		case bool, int, int64, uint64, float64:
			// A fixed scalar. A non-finite float cannot arrive here from YAML
			// as a float64 that JSON can carry, and if it ever does the
			// marshal is what refuses it, with the value named.
			if number, ok := typed.(float64); ok && (number != number || number > 1e308 || number < -1e308) {
				row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
					"tool %q injects %q with a number JSON cannot carry: write a finite number", name, key))
			}
		default:
			row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
				"tool %q injects %q with a %T, and an argument override holds one fixed scalar or one whole variable token: write a string, a number or a boolean, or record the value you need into its own variable and name that variable here",
				name, key, value))
		}
	}
}

// validateSlngInjectText splits a string override into the two shapes SLNG
// takes: text with no template in it at all, or exactly one whole token.
func validateSlngInjectText(agent *Agent, name, key, value string, row *TargetValidation) {
	refs := TemplateRefs(value)
	if len(refs) == 0 {
		return
	}
	// TemplateVar returns a name only when the whole value is one token, which
	// is exactly the distinction this check needs and is already the predicate
	// the code targets use to preserve an injected value's type.
	if TemplateVar(value) != "" {
		return
	}
	if len(refs) > 1 {
		row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
			"tool %q injects %q with %d template references in one value, and SLNG substitutes a whole override or nothing: name one variable on its own, or record the sentence you want into a variable and name that",
			name, key, len(refs)))
		return
	}
	row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
		"tool %q injects %q as text with {{%s}} inside it, and SLNG stores an override and substitutes it whole: the braces would reach the tool as characters. Write `%s` on its own to send the value, or record the text you want into a variable and name that variable here",
		name, key, refs[0], "{{"+refs[0]+"}}"))
}

// validateSlngRegions is the only region *value* check in the tree. Every other
// target forwards whatever region string it is given, because its platform owns
// the names; SLNG publishes a closed set of four, so an author can be told
// before the push instead of after it.
func validateSlngRegions(resolved Target, row *TargetValidation) {
	// One region is FieldDeploymentMultiRegion's job to enforce, and it already
	// refuses more than one. Checking every entry anyway means a package that
	// wrote two wrong regions hears about both problems, not just the count.
	if len(resolved.DeploymentRegions) == 0 {
		row.Errors = add(row.Errors, targetcap.CheckSlngRegion("").Error())
		return
	}
	for _, region := range resolved.DeploymentRegions {
		if err := targetcap.CheckSlngRegion(region); err != nil {
			row.Errors = add(row.Errors, err.Error())
		}
	}
}

// validateSlngFallbacks refuses the two fallback shapes SLNG rejects at push:
// a model listed in its own fallback chain, and a chain with a duplicate
// (voice_agent.py:194-225). Both are checkable here, so neither has to be
// discovered from a 422.
func validateSlngFallbacks(agent *Agent, row *TargetValidation) {
	for _, name := range slices.Sorted(maps.Keys(agent.Models)) {
		seen := map[string]bool{}
		for _, fallback := range agent.Models[name].Fallback {
			switch {
			case fallback == name:
				row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
					"model %q lists itself in its own fallback chain, which SLNG rejects: remove %q from its fallback list", name, name))
			case seen[fallback]:
				row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
					"model %q lists %q twice in its fallback chain, which SLNG rejects: name each fallback once", name, fallback))
			}
			seen[fallback] = true
		}
	}
}

// validateSlngVariables enforces the two rules SLNG's template_defaults imposes:
// the map is dict[str, str] (voice_agent.py:968), and a default may not itself
// carry a {{ }} reference (:985-994).
func validateSlngVariables(agent *Agent, row *TargetValidation) {
	for _, name := range slices.Sorted(maps.Keys(agent.Variables)) {
		variable := agent.Variables[name]
		if variable.Default == nil {
			continue
		}
		text, ok := variable.Default.(string)
		if !ok {
			row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
				"variable %q has a %s default, and SLNG stores every default as a string: quote the default, or drop it and supply the value when the call is dispatched",
				name, variable.Type))
			continue
		}
		if templateToken.MatchString(text) {
			row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
				"variable %q has a default containing a template reference, which SLNG rejects: write a literal default, or drop it and supply the value when the call is dispatched", name))
		}
	}
}

func validateSlngTool(name string, tool Tool, row *TargetValidation) {
	// A reserved name is reserved against a tool unmute would create. A builtin
	// reference *is* the curated capability that owns the name — `builtin: end_call`
	// is written as the tool `end_call` and resolves to SLNG's own end_call — so
	// refusing it would refuse the correct spelling and name itself as the fix.
	isOwningBuiltin := tool.Execution == ToolBuiltin && tool.Builtin == name
	if instead, reserved := targetcap.SlngReservedToolNames[name]; reserved && !isOwningBuiltin {
		row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
			"tool %q uses a name SLNG keeps for one of its own capabilities: use %s, or rename this tool", name, instead))
	}
	// `webhook:` has no execution Field constant of its own, so its slng
	// refusal lives here rather than in the capability table: webhook is the
	// ungated default there and only FieldWebhookPath and FieldToolAuth gate
	// parts of it. Adding a constant would mean adding a row for it, which
	// TestEveryFieldConstantHasARow requires, to say one slng-only thing.
	//
	// Same second clause as the `local:` refusal, different first: a webhook's
	// URL and credential are the part SLNG stores.
	if tool.Execution == ToolWebhook {
		row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
			"does not create tools: tool %q is a `webhook:` block, which would have to write the URL and its credential into a tool body, and SLNG owns those: create the tool in the SLNG dashboard and reference it with `slng:`, or compile to livekit or pipecat which call your endpoint themselves", name))
	}
	// An MCP server on SLNG becomes one reference per tool: mcp_refs is a list of
	// attachments, and there is no "the whole server" attachment to write. unmute
	// compiles offline and so cannot expand "everything" into that list. The push
	// does know the server's tools, but by then the package has already had to say
	// which ones it wanted, so the list is authored rather than inferred.
	if tool.Execution == ToolMCP && len(tool.MCPTools) == 0 {
		row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
			"tool %q exposes every tool on its MCP server, and SLNG attaches one reference per tool: list the tools you want under mcp.tools", name))
	}
}

// What used to live here, and why none of it does any more.
//
// validateSlngToolSchema held a tool's declared input against the limits in the
// published policy manifest, so an oversized schema failed at validate rather
// than at push. It only ever applied to a schema unmute *sent*, and unmute now
// sends none: a hosted reference carries a name, and the platform already
// accepted the schema when the tool was created there. Keeping the check would
// mean refusing a schema the platform holds and runs.
//
// The local-handler checks went with it. One refused a network import, because
// custom code on SLNG has no internet access; the other refused an
// `async def` entry point, because SLNG calls a handler synchronously. Both
// described a handler unmute uploaded. It uploads none, and `local:` is refused
// on this target by its capability row before either check could run.
//
// The base_url requirement went too. It existed because SLNG stored a webhook
// tool's URL in the body unmute wrote, so url_env alone left the body with no
// URL. There is no body.
//
// All four facts are still true about the platform. They are simply no longer
// unmute's to enforce, which is what reference-only means.

// refuseSlngProjectValues names the four target settings a bodiless target has
// no use for. All four passed in silence before this existed, because every
// driver-side check returns early for a provider with no support window, no pin
// floor and no SDK (research R5). Silence is the worst answer available: the
// author writes a version, watches validate pass, and the field reaches no
// artifact at all.
func refuseSlngProjectValues(resolved Target, row *TargetValidation) {
	for _, refusal := range []struct {
		set   bool
		field string
		why   string
	}{
		{resolved.Version != "", "version", "SLNG owns the runtime version its agents run on: remove the field"},
		{len(resolved.Pins) > 0, "pins", "there is no generated project whose packages could be pinned: remove the field"},
		{resolved.SDKLanguage != "", "sdk_language", "no SDK is generated for this target: remove the field"},
		{resolved.Connection != "", "connection", "a package declares no carrier state on SLNG: which number reaches an agent belongs to one deployment, not to a portable package, so buy the number and configure the trunk in the SLNG dashboard and remove the field. `unmute deploy` offers to attach a free trunk after a successful push"},
	} {
		if refusal.set {
			row.Errors = add(row.Errors, targetcap.SlngDiagnostic("does not take %s. %s", refusal.field, refusal.why))
		}
	}
}

// validateVaultTokens answers the per-target half of the {{$NAME}} question.
//
// Build lets a Vault token through on every package, because it runs once and
// does not know which targets are named; refusing there would refuse a legal
// slng package for having also named a livekit target. So the answer arrives
// here, per target, which is where "each target validates on its own terms"
// already puts every other question of this shape.
//
// The message is the whole point of the story. Before it existed, a Vault token
// produced "references {{$ACME_KEY}}, which is not a declared variable", which
// is true and sends the author to declare a variable that must not exist.
func validateVaultTokens(agent *Agent, provider targetcap.Provider, row *TargetValidation) {
	if provider == targetcap.Slng {
		return // slng resolves them; the token reaches the emitted body unchanged
	}
	report := func(site, value string) {
		for _, ref := range TemplateRefs(value) {
			name, vault := VaultToken(ref)
			if !vault {
				continue
			}
			row.Errors = add(row.Errors, fmt.Sprintf(
				"%s: {{$%s}} is a SLNG Vault variable, which only a slng target resolves: declare %s as a package variable and use {{%s}}, or compile this package to a slng target as well",
				site, name, strings.ToLower(name), strings.ToLower(name)))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(agent.Agents)) {
		report("agent "+name+" instructions", agent.Agents[name].Instructions)
	}
	for _, name := range slices.Sorted(maps.Keys(agent.Tasks)) {
		report("task "+name+" instructions", agent.Tasks[name].Instructions)
	}
	if agent.Conversation != nil && agent.Conversation.Greeting != nil {
		report("conversation.greeting.text", agent.Conversation.Greeting.Text)
	}
	for _, name := range slices.Sorted(maps.Keys(agent.Tools)) {
		tool := agent.Tools[name]
		report("tool "+name+" description", tool.Description)
		report("tool "+name+" webhook path", tool.Path)
		report("tool "+name+" webhook base_url", tool.BaseURL)
		for _, key := range slices.Sorted(maps.Keys(tool.Inject)) {
			if text, ok := tool.Inject[key].(string); ok {
				report("tool "+name+" inject."+key, text)
			}
		}
	}
}
