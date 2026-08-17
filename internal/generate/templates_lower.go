package generate

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// This file lowers the variable-template surface for both code drivers
// (variable_secrets_specs.md T6/T7). Each driver reads its call state through a
// different expression — Pipecat a `state` dataclass, LiveKit `ctx.userdata` —
// so every helper takes that accessor rather than hard-coding one.

// injectedValue is one hidden request value: Key is the request key, Expr the
// Python expression producing it at call time.
type injectedValue struct {
	Key  string
	Expr string
}

// neededVar is a variable an injected value reads. The generated tool refuses
// the call when one is unset, so no half-formed request is ever sent (V4).
type neededVar struct {
	Name        string
	Description string
}

// injectExpr renders one inject value as a Python expression. A value that is
// exactly one token reads the state attribute directly, so an integer variable
// stays an integer in the JSON body; a mixed string renders through the helper;
// a non-string value is forwarded as a literal (V14).
func injectExpr(value any, stateExpr string) string {
	text, ok := value.(string)
	if !ok {
		return pyLiteral(value)
	}
	if name := ir.TemplateVar(text); name != "" {
		return stateExpr + "." + name
	}
	if !ir.HasTemplate(text) {
		return pyLiteral(text)
	}
	return fmt.Sprintf("_render(%s, %s)", pyQuote(text), stateExpr)
}

// loweredInject builds the sorted inject expressions for a tool plus the
// variables they read, so the emitter can both send the values and refuse the
// call when one is missing.
func loweredInject(tool ir.Tool, variables map[string]ir.Variable, stateExpr string) ([]injectedValue, []neededVar) {
	keys := make([]string, 0, len(tool.Inject))
	for key := range tool.Inject {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]injectedValue, 0, len(keys))
	for _, key := range keys {
		values = append(values, injectedValue{Key: key, Expr: injectExpr(tool.Inject[key], stateExpr)})
	}
	return values, neededVars(tool, variables)
}

// neededVars collects the variables a tool's inject values and path read that
// could still be unset when the model calls it, in name order. Two kinds are
// left out: one carrying a default (it always has a value), and a system one
// (B2 — a refusal tells the model to go and ask, and nobody can be asked for a
// value the runtime owns; a route that owns it fails at session start instead).
func neededVars(tool ir.Tool, variables map[string]ir.Variable) []neededVar {
	seen := make(map[string]bool)
	var needed []neededVar
	collect := func(text string) {
		for _, ref := range ir.TemplateRefs(text) {
			variable, ok := variables[ref]
			if !ok || seen[ref] || variable.Default != nil || ir.IsSystemSource(variable.Source) {
				continue
			}
			seen[ref] = true
			needed = append(needed, neededVar{Name: ref, Description: variable.Description})
		}
	}
	keys := make([]string, 0, len(tool.Inject))
	for key := range tool.Inject {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if text, ok := tool.Inject[key].(string); ok {
			collect(text)
		}
	}
	collect(tool.Path)
	sort.Slice(needed, func(i, j int) bool { return needed[i].Name < needed[j].Name })
	return needed
}

// neededLiteral renders the refusal check's (name, hint) pairs as a Python list.
func neededLiteral(needed []neededVar) string {
	pairs := make([]string, 0, len(needed))
	for _, variable := range needed {
		pairs = append(pairs, "("+pyQuote(variable.Name)+", "+pyQuote(variable.Description)+")")
	}
	return "[" + strings.Join(pairs, ", ") + "]"
}

// requestBody renders the webhook JSON body: the model's own arguments plus the
// injected values. Joining here keeps the comma handling out of the templates.
func requestBody(args []string, inject []injectedValue) string {
	pairs := make([]string, 0, len(args)+len(inject))
	for _, name := range args {
		pairs = append(pairs, pyQuote(name)+": "+name)
	}
	for _, value := range inject {
		pairs = append(pairs, pyQuote(value.Key)+": "+value.Expr)
	}
	if len(pairs) == 0 {
		return "{}"
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}

// callKwargs renders a local handler call's keyword arguments, model arguments
// first, then the injected values.
func callKwargs(args []string, inject []injectedValue) string {
	pairs := make([]string, 0, len(args)+len(inject))
	for _, name := range args {
		pairs = append(pairs, name+"="+name)
	}
	for _, value := range inject {
		pairs = append(pairs, value.Key+"="+value.Expr)
	}
	return strings.Join(pairs, ", ")
}

// urlExpr renders the request URL. Without a path it is the plain env lookup
// both drivers already emit; with one the rendered path is appended to the
// trimmed base URL and every substituted value is URL-encoded.
func urlExpr(tool ir.Tool, stateExpr string) string {
	base := "os.environ[" + pyQuote(tool.URLEnv) + "]"
	if tool.Path == "" {
		return base
	}
	if !ir.HasTemplate(tool.Path) {
		return base + `.rstrip("/") + ` + pyQuote(tool.Path)
	}
	return fmt.Sprintf(`%s.rstrip("/") + _render(%s, %s, quote_values=True)`, base, pyQuote(tool.Path), stateExpr)
}

// promptExpr renders an agent's or task's system prompt: the module constant as
// it always was, wrapped in a render call only when it carries a template.
func promptExpr(constName, body, stateExpr string) string {
	if !ir.HasTemplate(body) {
		return constName
	}
	return fmt.Sprintf("_render(%s, %s)", constName, stateExpr)
}

// renderNeeds reports whether the emitted runtime needs the render helper at
// all. Leaving it out when nothing is templated keeps a plain project free of the
// machinery. The URL-encoding import rides with the helper rather than with the
// templated path that uses it: the helper always references it (B4).
func renderNeeds(agent *ir.Agent) bool {
	if agent.HasSessionStartTemplate() {
		return true
	}
	for _, tool := range agent.Tools {
		if ir.HasTemplate(tool.Path) {
			return true
		}
		for _, value := range tool.Inject {
			text, ok := value.(string)
			// A single-token value reads the state attribute directly, so only a
			// mixed string reaches the helper.
			if ok && ir.HasTemplate(text) && ir.TemplateVar(text) == "" {
				return true
			}
		}
	}
	return false
}

// captureFields returns the conversation variables, in name order: the schema of
// the generated update_variables tool (V6).
func captureFields(agent *ir.Agent) []string {
	var names []string
	for name, variable := range agent.Variables {
		if variable.Source == ir.VariableSourceConversation {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// captureDescription is the generated tool's description: one fixed line plus
// each variable's own description, so the model knows what to listen for.
func captureDescription(agent *ir.Agent, names []string) string {
	text := "Save details the caller gives you, as soon as you learn them."
	for _, name := range names {
		if description := agent.Variables[name].Description; description != "" {
			text += " " + name + ": " + description
		}
	}
	return text
}

// requiredSecretEnv lists the declared secrets a generated runtime refuses to
// start without, in name order (V12). Every declared secret is required.
// requiredSecretEnv is the declared list, minus the names that belong to a
// connection **this** target does not name.
//
// `secrets:` is package-wide by design (SCHEMA N42), so a two-target package
// declares both routes' carrier credentials. Feeding the whole list into one
// target's startup check made the Pipecat build refuse phone calls until the
// operator supplied four Elastic SIP trunk values that only the LiveKit build
// has any use for — names its own emitted code never reads once (Wave C,
// 2026-08-15). They reached `CALL_REQUIRED_ENV`, `.env.example`, the README, and
// the compile report, and they survived `--target`.
//
// A name that also does something else here — a model key, a tool credential —
// stays, because the exclusion is about the other route's names and not about
// the other route's file.
func requiredSecretEnv(agent *ir.Agent, target ir.Target, env *envSet) []string {
	foreign := map[string]bool{}
	for name, connection := range agent.Connections {
		if name == target.Connection {
			continue
		}
		for _, value := range connection.Environment {
			foreign[value] = true
		}
	}
	if len(foreign) == 0 {
		return slices.Clone(agent.Secrets)
	}
	// Anything this target's own route needs is not foreign, whatever another
	// connection also maps.
	if plan := target.Telephony; plan != nil {
		for _, name := range plan.Environment {
			delete(foreign, name)
		}
		for _, name := range slices.Concat(plan.RequiredEnvironment, plan.LocalEnvironment) {
			delete(foreign, name)
		}
	}
	out := make([]string, 0, len(agent.Secrets))
	for _, name := range agent.Secrets {
		if foreign[name] && !env.alwaysRead(name) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// orDefault returns text, or fallback when text is blank.
//
// Blank means empty **or whitespace**, and the difference is not academic: a
// control's `when:` becomes the docstring of the tool the model decides from, so
// `when: "   "` used to emit `""" """` and leave the model nothing to go on,
// while omitting the field entirely got the sensible default. Writing spaces was
// strictly worse than writing nothing, and nothing said so (Wave C, 2026-08-15).
func orDefault(text, fallback string) string {
	if strings.TrimSpace(text) == "" {
		return fallback
	}
	return text
}

// authorEnv is the half of a target's required environment the author actually
// supplies. The driver passes the other half: `unmute dev` sets those locally,
// while the platform or operator sets them on deploy.
//
// Both drivers read this one function, which is the fix rather than a new
// concept: the classification already existed, the LiveKit template labelled it
// instead of excluding it, and the Pipecat template ignored it entirely, so the
// same REDIS_URL was explained on one target and silently demanded on the other
// from one piece of data (research D11).
//
// `.env.example` is a to-do list and every line of it is a to-do, so a name
// nobody sets is absent rather than relabelled. It survives in
// compile-report.json's required_env, in the Compose interpolation defaults, and
// in the emitted README's carrier setup, which says where each value comes from
// rather than only naming it.
func authorEnv(required, supplied []string) []string {
	if len(supplied) == 0 {
		return slices.Clone(required)
	}
	out := make([]string, 0, len(required))
	for _, name := range required {
		if !slices.Contains(supplied, name) {
			out = append(out, name)
		}
	}
	return out
}

// withoutRouteEnv drops the names only a phone call reads, leaving the set a
// browser session actually needs.
//
// A phone package must run in the browser with nothing but model keys
// (spec FR-018). That held for free while connection environment names were
// exempt from `secrets:`; now that they are declared there (SCHEMA N41) they
// reach the startup check like any other secret, and a package with a secrets
// block would demand carrier credentials before opening a browser session.
//
// The route is the authority on which names are its own, and the question is
// asked of **every** connection in the package rather than only this target's.
// `secrets:` is package-wide, so a two-target package declares both targets'
// carrier credentials, and neither target's own plan knows about the other's.
// A connection environment value is a phone-route name whichever target names
// the file. Nothing here is re-derived from a name's spelling.
//
// **One name can be two things**, and matching by name alone got that wrong in
// both directions (Wave C, 2026-08-15). A connection maps `auth_token` onto a
// variable; a webhook tool's `auth.token_env` may name the same variable,
// because one gateway token really does serve both. Stripping it left the
// emitted code raising KeyError on the first tool call, after a startup check
// that passed and a README that said the key was covered. So the env set records
// which names the code reads on every session, and those are never stripped
// however a connection happens to map them.
func withoutRouteEnv(names []string, agent *ir.Agent, target ir.Target, env *envSet) []string {
	route := map[string]bool{}
	for _, connection := range agent.Connections {
		for _, name := range connection.Environment {
			route[name] = true
		}
	}
	for name := range route {
		if env.alwaysRead(name) {
			delete(route, name)
		}
	}
	if plan := target.Telephony; plan != nil {
		for _, name := range plan.Environment {
			route[name] = true
		}
		for _, name := range plan.RequiredEnvironment {
			route[name] = true
		}
		for _, name := range plan.LocalEnvironment {
			route[name] = true
		}
	}
	if len(route) == 0 {
		return names
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if !route[name] {
			out = append(out, name)
		}
	}
	return out
}

// reportSupported is the framework range this unmute supports, recorded next to
// the version the target declared. The report already has to show what was sent
// (constitution, Principle II); showing what was possible is what lets a reader
// tell a deliberate older pin from a stale one, without opening a document.
type reportSupported struct {
	Floor    string `json:"floor"`
	Ceiling  string `json:"ceiling"`
	Verified string `json:"verified"`
}

// supportedRange reads the window from its one home. A provider with no shipped
// driver has none, and the field is omitted rather than invented.
func supportedRange(provider targetcap.Provider) *reportSupported {
	win, ok := targetcap.Window(provider)
	if !ok {
		return nil
	}
	return &reportSupported{Floor: win.Floor, Ceiling: win.Ceiling, Verified: win.Verified}
}

// reportVariable is one variable as the compile report lists it: what it is,
// where its value comes from, and whether it has a fallback (I.artifacts).
type reportVariable struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Source      string `json:"source,omitempty"`
	HasDefault  bool   `json:"has_default"`
	Description string `json:"description,omitempty"`
}

// reportSecret is one declared secret as the compile report lists it, with the
// sites that reference it so an unused declaration is visible too.
type reportSecret struct {
	Name         string   `json:"name"`
	ReferencedBy []string `json:"referenced_by,omitempty"`
}

// reportVariables lists every declared variable, in name order.
func reportVariables(agent *ir.Agent) []reportVariable {
	names := make([]string, 0, len(agent.Variables))
	for name := range agent.Variables {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]reportVariable, 0, len(names))
	for _, name := range names {
		variable := agent.Variables[name]
		out = append(out, reportVariable{
			Name: name, Type: string(variable.Type), Source: string(variable.Source),
			HasDefault: variable.Default != nil, Description: variable.Description,
		})
	}
	return out
}

// reportSecrets lists every declared secret with the sites naming it.
func reportSecrets(agent *ir.Agent) []reportSecret {
	sites := ir.EnvReferenceSites(agent)
	out := make([]reportSecret, 0, len(agent.Secrets))
	for _, name := range agent.Secrets {
		out = append(out, reportSecret{Name: name, ReferencedBy: sites[name]})
	}
	return out
}

// secretEnvDocs builds the .env.example model in two lists: the declared
// secrets, then the env names the route needs that the package never declared,
// which the template labels once as a group (V11). A secret carries no fields, so
// a name is the whole entry.
func secretEnvDocs(agent *ir.Agent, required []string) (declared, extra []string) {
	// A package that declares nothing keeps the plain name list it always had,
	// so nothing about its output changes (V16).
	if len(agent.Secrets) == 0 {
		return nil, nil
	}
	for _, name := range required {
		if !slices.Contains(agent.Secrets, name) {
			extra = append(extra, name)
		}
	}
	return slices.Clone(agent.Secrets), extra
}
