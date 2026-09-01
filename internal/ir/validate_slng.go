package ir

import (
	"encoding/json"
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

// pythonImport matches an import statement's first module path. Python's own
// grammar allows more than this, but a network client is imported by one of these
// two forms in every handler anyone writes, and a parser for the rest would be a
// dependency and a maintenance surface for no extra catch.
var pythonImport = regexp.MustCompile(`(?m)^[ \t]*(?:from|import)[ \t]+([A-Za-z_][\w.]*)`)

// templateToken matches any {{ ... }} token. Used to refuse one where SLNG takes
// a literal.
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
	validateSlngToolSchema(name, tool, row)
	if tool.Execution == ToolWebhook && tool.BaseURL == "" {
		row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
			"tool %q names only url_env, and SLNG stores the URL in the tool body rather than reading it from an environment: add base_url with the literal https host, keeping url_env for the code targets", name))
	}
	// An MCP server on SLNG becomes one reference per tool, and unmute cannot ask
	// the server what it offers: mcp_refs carries a name pair per tool and the
	// schema hash is read from the live server at push time. So "expose
	// everything" has nothing to expand into here.
	if tool.Execution == ToolMCP && len(tool.MCPTools) == 0 {
		row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
			"tool %q exposes every tool on its MCP server, and SLNG attaches one reference per tool: list the tools you want under mcp.tools", name))
	}
	if tool.Execution != ToolLocal || tool.HandlerSource == "" {
		return
	}
	// Custom code on SLNG has no internet access at all. CodeConfig.egress reads
	// like a knob and validates nothing: it is kept for historical compatibility
	// (tool.py:191). So a handler that imports a network client fails here rather
	// than raising a connection error inside the sandbox on a live call.
	for _, match := range pythonImport.FindAllStringSubmatch(tool.HandlerSource, -1) {
		module := match[1]
		root := strings.SplitN(module, ".", 2)[0]
		if !slices.Contains(targetcap.SlngNetworkModules, module) && !slices.Contains(targetcap.SlngNetworkModules, root) {
			continue
		}
		row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
			"tool %q imports %s, and custom code on SLNG has no internet access: move the request into a webhook: tool, which is the shape that reaches the network", name, module))
	}
	// SLNG's handler is synchronous (app/services/tools.py:43-83) while LiveKit
	// awaits an awaitable result, so the same file cannot be both. Only the entry
	// point matters: a sync handler that runs its own event loop internally is
	// fine, which is why this looks for the tool's own function and not for the
	// word async.
	entryPoint := regexp.MustCompile(`(?m)^[ \t]*async[ \t]+def[ \t]+` + regexp.QuoteMeta(name) + `[ \t]*\(`)
	if entryPoint.MatchString(tool.HandlerSource) {
		row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
			"tool %q has an `async def %s` handler and SLNG calls a handler synchronously: make %s a plain `def` and run any awaitable inside it with asyncio.run", name, name, name))
	}
}

// validateSlngToolSchema checks a tool's declared input against the limits in
// the published policy manifest, so an oversized schema fails at validate rather
// than at push. The limits are one owner in internal/target.
func validateSlngToolSchema(name string, tool Tool, row *TargetValidation) {
	if len(tool.Input) == 0 {
		return
	}
	limits := targetcap.SlngSchemaLimits
	encoded, err := json.Marshal(tool.Input)
	if err != nil {
		// A schema that will not encode is a bug elsewhere; say so rather than
		// silently passing it.
		row.Errors = add(row.Errors, targetcap.SlngDiagnostic("tool %q has an input schema that will not encode as JSON: %v", name, err))
		return
	}
	if len(encoded) > limits.Bytes {
		row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
			"tool %q has a %d byte input schema and SLNG accepts %d: describe fewer fields, or take an identifier and look the rest up inside the tool", name, len(encoded), limits.Bytes))
	}
	counts := schemaCounts{}
	countSchema(tool.Input, 1, &counts)
	for _, check := range []struct {
		measured int
		limit    int
		what     string
		instead  string
	}{
		{counts.depth, limits.Depth, "levels deep", "flatten the nested objects"},
		{counts.nodes, limits.Nodes, "schema nodes", "describe fewer fields"},
		{counts.properties, limits.Properties, "properties on one object", "split the object, or take an identifier instead"},
		{counts.branches, limits.Branches, "branches in one union", "narrow the union to the cases the model actually picks"},
	} {
		if check.measured > check.limit {
			row.Errors = add(row.Errors, targetcap.SlngDiagnostic(
				"tool %q has an input schema %d %s and SLNG accepts %d: %s", name, check.measured, check.what, check.limit, check.instead))
		}
	}
}

type schemaCounts struct {
	depth      int
	nodes      int
	properties int
	branches   int
}

// countSchema walks a decoded JSON Schema and records the four shape measures
// the policy manifest caps. It counts the worst case per measure rather than a
// total, because that is what the manifest caps: 256 properties on one object,
// not 256 in the document.
func countSchema(node any, depth int, counts *schemaCounts) {
	counts.nodes++
	if depth > counts.depth {
		counts.depth = depth
	}
	switch value := node.(type) {
	case map[string]any:
		for _, key := range []string{"properties", "patternProperties", "$defs", "definitions"} {
			if properties, ok := value[key].(map[string]any); ok && len(properties) > counts.properties {
				counts.properties = len(properties)
			}
		}
		for _, key := range []string{"oneOf", "anyOf", "allOf"} {
			if branches, ok := value[key].([]any); ok && len(branches) > counts.branches {
				counts.branches = len(branches)
			}
		}
		for _, key := range slices.Sorted(maps.Keys(value)) {
			countSchema(value[key], depth+1, counts)
		}
	case []any:
		for _, item := range value {
			countSchema(item, depth+1, counts)
		}
	}
}

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
