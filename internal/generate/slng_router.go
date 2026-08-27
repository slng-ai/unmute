package generate

import (
	"encoding/json"
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
	// ClientExpr is the expression for the router client this construction is
	// given, empty on a target that lets the framework build its own.
	//
	// One client per call, held on the call's own state object, because that is
	// where a construction inside an agent method can still reach it. The same
	// split SessionExpr makes, and for the same reason.
	ClientExpr string
	// BodyPerRequest says the body extension travels with each request too,
	// which is what makes the variable snapshot current rather than frozen at
	// the moment the model object was built.
	//
	// True on LiveKit, and the mechanism is the same trap as the header one
	// field over: the plugin copies the per-request extra_kwargs first and then
	// overwrites extra_body from its own options (livekit-plugins-openai
	// llm.py:956-959, read at the pinned version). So a constructor body does
	// not merely win, it wins in silence while the emitted per-request source
	// still reads correctly.
	//
	// False on Pipecat, which keeps the body on the service and refreshes it
	// with a settings delta when the call writes a variable. A delta merges the
	// `extra` dict key by key (pipecat services/settings.py apply_update), so a
	// body-only delta leaves the site's scope header alone.
	//
	// False for the LiveKit summarizer, which needs nothing per request: it is
	// built inside an agent method at handoff time, so its construction is the
	// moment of its request and its snapshot is current already.
	BodyPerRequest bool
}

// slngConfigFunc names the emitted configuration helper for one think profile.
// Profile names are already snake_case, so the name needs no transform.
func slngConfigFunc(profile string) string { return "_slng_config_" + profile }

// slngRouterClass is the emitted service subclass Pipecat needs: its only seam
// for a response hook is overriding how the service builds its client. Here
// rather than in a template because the driver assigns it as a call's class.
//
// The provenance line's own text lives in the two templates that write it, as
// literal Python. It was briefly a table here, which bought nothing: no Go code
// reads the field names, both templates already carry the hook verbatim, and
// TestSlngRouterProvenanceLineHasOneOwner holds the field set and its order by
// reading what the drivers wrote. The contract is in
// specs/016-router-template-variables/contracts/provenance-log.md.
const slngRouterClass = "_SlngRouterLLMService"

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
	// BodyParams is every forwardable param this package's router bindings send,
	// as "profile: key=json" lines. The runbook names them rather than describing
	// the rule, because a reader checking why a request went where it went wants to
	// see the value they authored, in the form it leaves in.
	//
	// The compiler knows nothing about what any of them mean. That is the point of
	// a passthrough param, and it is why the runbook can only say a param reached
	// the upstream, never what the upstream will do about it.
	BodyParams []string
	// PromptSuffix is the authored directive appended to every system prompt, per
	// profile, as "profile: value" lines. Empty on a package that authors none.
	PromptSuffix []string
	// Scopes is every cache scope this package sends, in agent order then task
	// order. The emitted runbook prints the list rather than describing the rule,
	// because a reader checking a log line wants to recognise the value they are
	// looking at.
	Scopes []string
	// ClientBaseURL is the endpoint the emitted client is built with, and the
	// flag that a target builds its own client at all, which is the only way to
	// attach a response hook. Set on LiveKit, empty on Pipecat, which overrides
	// how its service builds one instead.
	ClientBaseURL string
	// ClientKeyEnv is the credential's environment variable name. Threaded from
	// internal/target rather than spelled in the template, because that package
	// owns provider facts and a rename there has to reach emitted Python. The
	// emitted helper and class names above are ours, not a provider's, so they
	// are written literally.
	ClientKeyEnv string
	// RouterClass names the emitted service subclass, on the target that needs
	// one. Set on Pipecat, empty on LiveKit, which takes a client instead.
	RouterClass string
	// RequestBody is the body extension as a Python dict literal, set only on a
	// target that sends it per request. The template writes it into its own
	// request path, where the variable snapshot is read again for every turn
	// instead of once for the call.
	//
	// One value per package rather than one per profile, and validation is what
	// makes that safe: on livekit every router profile has to be the entry
	// agent's, so a package has exactly one.
	RequestBody string
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
		_, params := splitParams(slngConsumedParams(binding.Params), targetcap.SlngRequestBodyArg)
		for _, key := range slices.Sorted(maps.Keys(params)) {
			encoded, err := json.Marshal(params[key])
			if err != nil {
				return slngHelpers{}, fmt.Errorf("think.%s param %s: %w", profile, key, err)
			}
			helpers.BodyParams = append(helpers.BodyParams,
				fmt.Sprintf("%s: %s=%s", profile, key, encoded))
		}
		if binding.PromptSuffix != "" {
			helpers.PromptSuffix = append(helpers.PromptSuffix,
				fmt.Sprintf("%s: %s", profile, binding.PromptSuffix))
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

// slngClientArgs replaces a router construction's credential and endpoint
// arguments with the client the package builds itself.
//
// Both values move into that helper rather than being passed twice. The plugin
// ignores api_key and base_url entirely once it is given a client, so leaving
// them here would emit two arguments that do nothing and a reader would have to
// work out which pair the request actually used.
//
// Only the two named arguments go. Everything else the catalogue put on the call,
// the model above all, stays where it is.
func slngClientArgs(args []pyKV, expr string) []pyKV {
	if expr == "" {
		return args
	}
	rest := slices.DeleteFunc(args, func(arg pyKV) bool {
		return arg.Key == "api_key" || arg.Key == "base_url"
	})
	return append([]pyKV{{Key: "client", Value: expr}}, rest...)
}

// slngRequestBody is the body extension one router think request carries beyond
// the prompt: the inline model configuration, the variable snapshot, the author's
// pure-proxy switch when it is on, and the author's forwardable params.
//
// One owner, two readers, the same arrangement as the header dict below it. A
// target that fixes the body at construction reads it through
// slngRequestExtras; a target that sends it per request reads it through
// slngHelpers.RequestBody and writes it into its own request path. A second
// spelling of this dict is how one of the two would come to send a stale
// snapshot.
//
// The params join what this dict owns rather than getting an assembly path of
// their own, because target.SlngRequestBodyArg says the body is where a router
// binding's params go on every target. It reads them off the binding instead of
// taking them as an argument for the same reason it reads the pure-proxy switch
// off it: a caller that had to remember to pass them is a caller that can forget.
func slngRequestBody(site slngSite, binding ir.Binding) map[string]any {
	body := map[string]any{"slng_config": pyExpr(site.ConfigFunc + "()")}
	if slngPureProxy(binding) {
		body[slngPureProxyParam] = true
	}
	if len(site.Names) > 0 {
		body["template_variables"] = pyExpr(fmt.Sprintf("_slng_template_variables(%s, (%s))",
			slngStateExpr(site.StateExpr), pyTuple(site.Names)))
	}
	// The folded fields stay out: they are unmute's own typed model fields, they
	// name a real slot on every entry that has them, and moving them would change
	// where a temperature travels on every router package for no reason. What
	// lands here is what splitParams calls overflow, which is the author's
	// provider-specific passthrough and the only kind the upstream, not the
	// compiler, is meant to read.
	_, params := splitParams(slngConsumedParams(binding.Params), targetcap.SlngRequestBodyArg)
	maps.Copy(body, params)
	return body
}

// slngRequestExtras is what a router think construction carries: whichever of
// the two dicts this target does not send per request.
//
// A site whose headers travel per request gets no header dict here, and a site
// whose body travels per request gets no body. On LiveKit both are absent, and
// that is not an optimisation but a requirement: the plugin applies its own
// constructor options after the per-request ones, so either dict left here would
// silently win while the emitted per-request source still looked right. A
// LiveKit router construction therefore carries no router extras at all, which
// is what the gate in livekit_v1_test.go holds.
func slngRequestExtras(site slngSite, binding ir.Binding) map[string]any {
	extras := map[string]any{}
	if !site.BodyPerRequest {
		extras[targetcap.SlngRequestBodyArg] = slngRequestBody(site, binding)
	}
	if !site.HeadersPerRequest {
		extras[targetcap.SlngRequestHeadersArg] = slngRequestHeaders(site)
	}
	return extras
}

// summaryPromptSuffix is the directive the emitted summarizer prompt carries: the
// authored prompt_suffix on whichever think binding does the summarizing.
//
// The summarizer's prompt is a literal in the driver's own template rather than
// an authored file, so ir.Build cannot reach it the way it reaches every agent and
// task prompt. This is the one site that needs the value carried through instead.
//
// One value, because the emitted helper holds one prompt. A package whose
// summarizer profiles authored different suffixes has asked for something that
// shape cannot express, so it is refused with the reason named rather than served
// whichever one happened to be walked last. No package in the repository can
// reach it: a target is held to a single router think profile, and it takes two
// summarizer profiles with two different directives to get here.
func summaryPromptSuffix(agent *ir.Agent) (string, error) {
	found := map[string]string{} // suffix -> the profile that authored it
	consider := func(context ir.TaskContext) {
		if context.History != ir.HistorySummary || context.Summarizer == "" {
			return
		}
		if suffix := agent.Models[context.Summarizer].PromptSuffix; suffix != "" {
			found[suffix] = context.Summarizer
		}
	}
	for _, name := range slices.Sorted(maps.Keys(agent.Controls)) {
		if transfer, ok := agent.Controls[name].(*ir.AgentTransfer); ok {
			consider(transfer.Context.TaskContext)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(agent.Tasks)) {
		consider(agent.Tasks[name].Context)
	}
	switch len(found) {
	case 0:
		return "", nil
	case 1:
		for suffix := range found {
			return suffix, nil
		}
	}
	profiles := make([]string, 0, len(found))
	for suffix, profile := range found {
		profiles = append(profiles, fmt.Sprintf("%s authors %q", profile, suffix))
	}
	slices.Sort(profiles)
	return "", fmt.Errorf("two summarizer think profiles author different prompt_suffix values (%s): the emitted summarizer prompt is one literal shared by every summarizer site, so it can carry one directive. Give them the same value, or summarize both with one profile",
		strings.Join(profiles, "; "))
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
