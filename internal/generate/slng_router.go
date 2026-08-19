package generate

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// What a SLNG Context Router think binding adds to an emitted agent, shared by
// both drivers so the request shape has one owner: the regional base URL, the
// two identity headers, the inline model configuration, and the template
// variable snapshot that travels beside a prompt whose placeholders the router
// fills in.
//
// internal/target owns the provider facts. This file owns their Python.

// slngVariableLimit is the router's per-value ceiling. An over-long value is
// truncated with a warning rather than dropped, because FR-018 forbids ending a
// live call over a variable.
const slngVariableLimit = 4000

// slngSite is what one router LLM construction needs that the binding cannot
// carry: two expressions whose spelling belongs to the target, and the names the
// profile's prompts reference.
type slngSite struct {
	// SessionExpr is the Python expression for this call's session id. One value
	// per call, identical across every turn, retry, tool follow-up, handoff and
	// task of that call (FR-012).
	SessionExpr string
	// StateExpr is the expression for the variable state object, empty when the
	// package declares no variables.
	StateExpr string
	// Names is the union of template variable names the router-bound prompts on
	// this think profile reference, in order.
	//
	// Per profile rather than per prompt site, because that is the granularity
	// the two targets actually give: on Pipecat a task's prompt reaches the
	// router through the owning agent's LLM object as a flow role_message, so a
	// per-site snapshot could not reach it. A union can only ever carry a name
	// some sibling prompt on the same profile references, never a name no prompt
	// references at all, and FR-016's rule is that a referenced name is never
	// missing.
	Names []string
	// ConfigFunc is the emitted helper returning this profile's inline
	// configuration.
	ConfigFunc string
}

// slngConfigFunc names the emitted configuration helper for one think profile.
// Profile names are already snake_case, so the name needs no transform.
func slngConfigFunc(profile string) string { return "_slng_config_" + profile }

// slngConfigHelper is one emitted configuration function.
type slngConfigHelper struct {
	Func string // the function name
	Body string // the dict literal it returns
}

// slngHelpers is everything a package with a router binding emits at module
// level. Empty on a package with no router binding, which is FR-019 by
// construction: a driver with nothing here writes nothing.
type slngHelpers struct {
	// Configs is one entry per router think profile, in profile order.
	Configs []slngConfigHelper
	// Variables is set when any router prompt references a template variable, so
	// the snapshot helper is worth emitting.
	Variables bool
	// Vertex is set when any router upstream is vertex, so the credential helper
	// is worth emitting.
	Vertex bool
	// Limit is the truncation ceiling, so the emitted helper and the runbook
	// cannot drift from each other.
	Limit int
}

// Any reports whether the package needs the router machinery at all.
func (h slngHelpers) Any() bool { return len(h.Configs) > 0 }

// slngHelpersFor collects the module-level helpers one target needs. It reads
// the resolved bindings rather than agent.Models, because a per-target override
// can turn a binding into a router binding or away from one.
func slngHelpersFor(agent *ir.Agent, tgt ir.Target) (slngHelpers, error) {
	helpers := slngHelpers{Limit: slngVariableLimit}
	for _, profile := range slices.Sorted(maps.Keys(tgt.Models.Reason)) {
		binding := tgt.Models.Reason[profile]
		if !binding.Router() {
			continue
		}
		body, err := slngConfigBody(binding)
		if err != nil {
			return slngHelpers{}, fmt.Errorf("think.%s: %w", profile, err)
		}
		helpers.Configs = append(helpers.Configs, slngConfigHelper{Func: slngConfigFunc(profile), Body: body})
		if len(slngTemplateNames(agent, tgt, profile)) > 0 {
			helpers.Variables = true
		}
		if binding.Upstream != nil && binding.Upstream.Provider == "vertex" {
			helpers.Vertex = true
		}
	}
	return helpers, nil
}

// slngConfigBody renders the inline slng_config literal: one tier, one entry,
// weight 100, and an endpoint holding exactly the resolved upstream's fields.
//
// The author never writes a tier. cache_enabled is never sent, having been
// measured to change nothing, and sending it would imply control the author does
// not have (FR-035).
func slngConfigBody(binding ir.Binding) (string, error) {
	if binding.Upstream == nil {
		return "", fmt.Errorf("a router binding needs an upstream block")
	}
	fields, ok := targetcap.SlngResolveUpstream(binding.Upstream.Provider, binding.Upstream.Fields())
	if !ok {
		return "", fmt.Errorf("upstream provider %q is not one the router accepts", binding.Upstream.Provider)
	}
	endpoint := make(map[string]any, len(fields))
	for _, field := range fields {
		switch {
		case field.JSONObject:
			// A GCP key is an object on the wire, and the variable may hold it in
			// any of three shapes, so the helper decides at startup.
			endpoint[field.Wire] = pyExpr(fmt.Sprintf("_slng_vertex_credentials(%s)", pyQuote(field.Value)))
		case field.Env:
			// Named, never written: no generated file holds a credential value.
			endpoint[field.Wire] = pyExpr(envRef(field.Value))
		default:
			endpoint[field.Wire] = field.Value
		}
	}
	tier := map[string]any{"model": binding.Model, "weight": 100, "endpoint": endpoint}
	return pyLiteral(map[string]any{"tiers": map[string]any{"1": []any{tier}}}), nil
}

// slngBindingCredentialEnvs lists the environment variables one router binding's
// request body reads, so every one joins the generated startup check and a
// missing value fails at boot rather than on the first turn of a live call
// (FR-034b). Names only: no value is read here.
func slngBindingCredentialEnvs(binding ir.Binding) []string {
	if !binding.Router() || binding.Upstream == nil {
		return nil
	}
	fields, ok := targetcap.SlngResolveUpstream(binding.Upstream.Provider, binding.Upstream.Fields())
	if !ok {
		return nil
	}
	var names []string
	for _, field := range fields {
		if field.Env && !slices.Contains(names, field.Value) {
			names = append(names, field.Value)
		}
	}
	return names
}

// slngTemplateNames is the union of template variable names every router-bound
// prompt resolving to this think profile references, deduped and ordered.
//
// The name list comes from ir.TemplateRefs, the same function the validator
// uses, so the request can never reference a name it does not supply and the
// router's 422 is unreachable from emitted output (FR-016).
func slngTemplateNames(agent *ir.Agent, tgt ir.Target, profile string) []string {
	var names []string
	add := func(body string) {
		for _, name := range ir.TemplateRefs(body) {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	entry := agent.Agents[agent.EntryAgent].Model
	for _, name := range slices.Sorted(maps.Keys(agent.Agents)) {
		if def := agent.Agents[name]; slngProfileOf(tgt, def.Model, entry) == profile {
			add(def.Instructions)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(agent.Tasks)) {
		if task := agent.Tasks[name]; slngProfileOf(tgt, task.Model, entry) == profile {
			add(task.Instructions)
		}
	}
	slices.Sort(names)
	return names
}

// slngProfileOf resolves which think profile a prompt site runs on: its own if
// it names one, otherwise the entry agent's. It answers "" when that profile is
// not a router binding on this target, so a package holding a router profile and
// a direct one comes out right.
func slngProfileOf(tgt ir.Target, model, entry string) string {
	profile := model
	if profile == "" {
		profile = entry
	}
	if binding, ok := tgt.Models.Reason[profile]; ok && binding.Router() {
		return profile
	}
	return ""
}

// slngRouterBinding reports whether a prompt site's think profile is a router
// binding, which is the one question the prompt lowering seam asks.
func slngRouterBinding(agent *ir.Agent, tgt ir.Target, model string) (string, bool) {
	profile := slngProfileOf(tgt, model, agent.Agents[agent.EntryAgent].Model)
	return profile, profile != ""
}

// slngRequestExtras is the pair of dicts every router think request carries:
// the identity headers, and the body extension holding the inline configuration
// and the variable snapshot.
func slngRequestExtras(binding ir.Binding, site slngSite) map[string]any {
	headers := map[string]any{
		targetcap.SlngAgentIDHeader:   binding.AgentID,
		targetcap.SlngSessionIDHeader: pyExpr(site.SessionExpr),
	}
	body := map[string]any{"slng_config": pyExpr(site.ConfigFunc + "()")}
	if len(site.Names) > 0 {
		body["template_variables"] = pyExpr(fmt.Sprintf("_slng_template_variables(%s, (%s))",
			slngStateExpr(site.StateExpr), pyTuple(site.Names)))
	}
	return map[string]any{"extra_headers": headers, "extra_body": body}
}

// slngStateExpr is the state object the snapshot reads. A package with names to
// send always has variables, so the empty case is unreachable from a real
// package; None keeps the emitted call valid if it ever is not.
func slngStateExpr(expr string) string {
	if expr == "" {
		return "None"
	}
	return expr
}

// pyTuple renders a Python tuple of strings, with the trailing comma a
// one-element tuple needs.
func pyTuple(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = pyQuote(value)
	}
	if len(quoted) == 1 {
		return quoted[0] + ","
	}
	return strings.Join(quoted, ", ")
}

// slngConsumedParams drops the params the compiler consumes rather than
// forwards. world_part_override becomes the base URL, so it must not also reach
// the client as a kwarg the SDK never heard of (D2).
func slngConsumedParams(params map[string]any) map[string]any {
	if len(params) == 0 {
		return params
	}
	out := make(map[string]any, len(params))
	for key, value := range params {
		if key == "world_part_override" {
			continue
		}
		out[key] = value
	}
	return out
}

// slngRegion reads the authored region off a binding, before it is consumed.
func slngRegion(binding ir.Binding) string {
	region, _ := binding.Params["world_part_override"].(string)
	return region
}
