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
	}
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
