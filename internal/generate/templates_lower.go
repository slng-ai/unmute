package generate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/slng/unmute/internal/ir"
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
// start without, in name order (V12).
func requiredSecretEnv(agent *ir.Agent) []string {
	var names []string
	for name, secret := range agent.Secrets {
		if secret.Required {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
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
	Required     bool     `json:"required"`
	Description  string   `json:"description,omitempty"`
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
	names := make([]string, 0, len(agent.Secrets))
	for name := range agent.Secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	sites := ir.EnvReferenceSites(agent)
	out := make([]reportSecret, 0, len(names))
	for _, name := range names {
		secret := agent.Secrets[name]
		out = append(out, reportSecret{
			Name: name, Required: secret.Required, Description: secret.Description,
			ReferencedBy: sites[name],
		})
	}
	return out
}

// secretDoc is one declared secret as the .env.example renders it (V11).
type secretDoc struct {
	Name        string
	Description string
	Optional    bool
	// Source names where a non-secret env name comes from (a connection file),
	// so a reader can tell declared secrets from route-supplied ones.
	Source string
}

// secretDocs builds the .env.example model: declared secrets first, then env
// names the package references without declaring, each labeled.
func secretDocs(agent *ir.Agent, required []string) []secretDoc {
	// A package that declares nothing keeps the plain name list it always had,
	// so nothing about its output changes (V16).
	if len(agent.Secrets) == 0 {
		return nil
	}
	docs := make([]secretDoc, 0, len(required))
	declared := make(map[string]bool, len(agent.Secrets))
	names := make([]string, 0, len(agent.Secrets))
	for name := range agent.Secrets {
		names = append(names, name)
		declared[name] = true
	}
	sort.Strings(names)
	for _, name := range names {
		secret := agent.Secrets[name]
		docs = append(docs, secretDoc{Name: name, Description: secret.Description, Optional: !secret.Required})
	}
	for _, name := range required {
		if !declared[name] {
			docs = append(docs, secretDoc{Name: name, Source: "required by the target or a connection"})
		}
	}
	return docs
}
