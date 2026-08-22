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
	// Scope is the cache scope this one prompt site sends as its agent id
	// header value, derived by target.SlngScope from the single authored id and
	// the site's own name.
	//
	// Per site, not per profile, and that is the whole point of this field. Two
	// sites can share a think profile and still be two prompts: four agents and
	// two tasks shared one profile in examples/salon-concierge, and therefore
	// one cache, which is how the booking specialist's opening turn was served
	// the concierge's line.
	Scope string
	// HeadersPerRequest says the identity headers travel with each request
	// rather than being fixed at construction.
	//
	// True on LiveKit, where one model object serves every site in the session,
	// so the scope cannot be a constructor value: a constructor extra_headers
	// overwrites the per-request one wholesale rather than merging with it
	// (research R5, and the note in target/catalog_livekit.go). False on
	// Pipecat, which builds one service per agent and can carry the scope where
	// it already carries everything else.
	HeadersPerRequest bool
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
	// The two header names, and the field the per-call identifier occupies on a
	// LiveKit user data object. A template that builds the header dict itself
	// still reads the names from internal/target, so a rename there reaches the
	// emitted Python rather than only the Go-rendered half.
	AgentIDHeader   string
	SessionIDHeader string
	SessionIDField  string
	// Scopes is every cache scope this package sends, in agent order then task
	// order. The emitted runbook prints the list rather than describing the rule,
	// because a reader checking a log line wants to recognise the value they are
	// looking at.
	Scopes []string
}

// Any reports whether the package needs the router machinery at all.
func (h slngHelpers) Any() bool { return len(h.Configs) > 0 }

// slngHelpersFor collects the module-level helpers one target needs. It reads
// the resolved bindings rather than agent.Models, because a per-target override
// can turn a binding into a router binding or away from one.
func slngHelpersFor(agent *ir.Agent, tgt ir.Target) (slngHelpers, error) {
	helpers := slngHelpers{
		Limit:           slngVariableLimit,
		AgentIDHeader:   targetcap.SlngAgentIDHeader,
		SessionIDHeader: targetcap.SlngSessionIDHeader,
		SessionIDField:  livekitSessionIDField,
	}
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
	helpers.Scopes = slngPackageScopes(agent, tgt)
	return helpers, nil
}

// slngPackageScopes lists every cache scope this package sends on this target, in
// agent order then task order. A site whose think profile is not a router binding
// contributes nothing, the same way the drivers give it no scope.
func slngPackageScopes(agent *ir.Agent, tgt ir.Target) []string {
	var scopes []string
	add := func(model string, site targetcap.SlngSite) {
		profile, router := slngRouterBinding(agent, tgt, model)
		if !router {
			return
		}
		scopes = append(scopes, targetcap.SlngScope(tgt.Models.Reason[profile].AgentID, site))
	}
	for _, name := range slices.Sorted(maps.Keys(agent.Agents)) {
		add(agent.Agents[name].Model, targetcap.SlngSite{Kind: targetcap.SlngSiteAgent, Name: name})
	}
	for _, name := range slices.Sorted(maps.Keys(agent.Tasks)) {
		add(agent.Tasks[name].Model, targetcap.SlngSite{Kind: targetcap.SlngSiteTask, Name: name})
	}
	return scopes
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

// slngRequestHeaders is the identity header dict one prompt site sends: its own
// cache scope, and the call's session id which scopes nothing and only groups one
// conversation's requests together.
//
// One owner, two readers. The construction path below puts it on the service that
// carries it for its whole life, and the Pipecat task handlers swap it on the way
// into a task and back on the way out. A second spelling of this dict is how a
// task would come to send its owner's scope.
func slngRequestHeaders(site slngSite) map[string]any {
	return map[string]any{
		targetcap.SlngAgentIDHeader:   site.Scope,
		targetcap.SlngSessionIDHeader: pyExpr(site.SessionExpr),
	}
}

// slngRequestExtras is what a router think request carries beyond the prompt: the
// body extension holding the inline configuration and the variable snapshot, and
// on a target that fixes them at construction, the identity headers too.
//
// A site whose headers travel per request gets no header dict here. On LiveKit
// that is not an optimisation but a requirement: a constructor extra_headers
// replaces the per-request one instead of merging, so leaving it in place would
// silently win over the scope and the emitted source would still look right.
func slngRequestExtras(site slngSite, pureProxy bool) map[string]any {
	body := map[string]any{"slng_config": pyExpr(site.ConfigFunc + "()")}
	if pureProxy {
		body[slngPureProxyParam] = true
	}
	if len(site.Names) > 0 {
		body["template_variables"] = pyExpr(fmt.Sprintf("_slng_template_variables(%s, (%s))",
			slngStateExpr(site.StateExpr), pyTuple(site.Names)))
	}
	extras := map[string]any{"extra_body": body}
	if !site.HeadersPerRequest {
		extras["extra_headers"] = slngRequestHeaders(site)
	}
	return extras
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
// forwards. world_part_override becomes the base URL and slng_pure_proxy rides
// the request body, so neither must also reach the client as a kwarg the SDK
// never heard of (D2).
func slngConsumedParams(params map[string]any) map[string]any {
	if len(params) == 0 {
		return params
	}
	out := make(map[string]any, len(params))
	for key, value := range params {
		if key == "world_part_override" || key == slngPureProxyParam {
			continue
		}
		out[key] = value
	}
	return out
}

// slngPureProxyParam is the authored switch for the router's pure-proxy mode:
// the upstream still answers every request, the cache still records what it
// would have served, and nothing is ever replayed to the caller. It is the
// router's own documented shadow-trial mode (docs/pure_proxy.md, read
// 2026-08-21), and the reason it exists here is measured: the cache key is the
// (assistant speech, user speech) pair and carries no system prompt, so two
// unmute agents whose last exchange matches collide. Observed 2026-08-21 on
// three live calls: the booking specialist's opening turn was served the
// concierge's "what phone number should I use", cache_layer l2_exact, 1.27ms,
// no model call.
//
// The measurement stays on the record because it is why this switch was reached
// for. What has changed is that it is no longer the answer. The real fix landed:
// every prompt site sends its own cache scope, derived from the one authored id,
// so two agents cannot share a cache and the collision above cannot happen. The
// shipped example carried this switch and no longer does.
//
// So the switch is now an author's own tool rather than a guard anything needs: a
// shadow trial, where you want to see what the cache would have served without
// serving it. Authored and never on by default, because suppressing the serve
// gives up the speed the router exists for.
const slngPureProxyParam = "slng_pure_proxy"

// slngPureProxy reads the authored switch off a binding, before it is consumed.
func slngPureProxy(binding ir.Binding) bool {
	on, _ := binding.Params[slngPureProxyParam].(bool)
	return on
}

// slngRegion reads the authored region off a binding, before it is consumed.
func slngRegion(binding ir.Binding) string {
	region, _ := binding.Params["world_part_override"].(string)
	return region
}
