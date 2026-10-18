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
	Names     []string
	NamesExpr string // active scope lookup, when the site changes per request
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
	// llm.py:961-968, read at the pinned version). So a constructor body does
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
	// VariablesPerRequest says the template variables are read from live state
	// on every request rather than snapshotted into the construction.
	//
	// Narrower than BodyPerRequest on purpose. The rest of the body never
	// changes during a call, so snapshotting it costs nothing and keeps the
	// construction readable; the variables are the one part that does change,
	// and a snapshot of them is a value written part way through a step that
	// reaches the model one turn late. Set on both targets, reached two ways:
	// LiveKit renders the whole body inside its llm node, Pipecat overrides the
	// framework's own per-request parameter seam.
	VariablesPerRequest bool
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
	// Omitted when the variables travel per request: two spellings of the same
	// dict is how one of them comes to send a stale snapshot, and a snapshot
	// beside the live read is a reader's question with no answer.
	if (len(site.Names) > 0 || site.NamesExpr != "") && !site.VariablesPerRequest {
		names := site.NamesExpr
		if names == "" {
			names = "(" + pyTuple(site.Names) + ")"
		}
		scope := pyQuote(site.Scope)
		if site.HeadersPerRequest {
			scope = "agent._slng_scope"
		}
		body["template_variables"] = pyExpr(fmt.Sprintf("_slng_template_variables(%s, %s, scope=%s)",
			slngStateExpr(site.StateExpr), names, scope))
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
// forwards. world_part becomes the base URL and slng_pure_proxy rides the
// request body, so neither must also reach the client as a kwarg the SDK never
// heard of (D2).
func slngConsumedParams(params map[string]any) map[string]any {
	if len(params) == 0 {
		return params
	}
	out := make(map[string]any, len(params))
	for key, value := range params {
		if key == "world_part" || key == slngPureProxyParam {
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

// slngWorldPart reads the authored world part off a binding, before it is
// consumed.
func slngWorldPart(binding ir.Binding) string {
	part, _ := binding.Params["world_part"].(string)
	return part
}
