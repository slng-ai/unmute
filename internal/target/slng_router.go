package target

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The SLNG Context Router's own facts: the regions it serves, the base URL each
// one takes, what an agent id may be, and which fields each upstream provider
// needs inside the inline endpoint object every think request carries.
//
// Both drivers and ir.Validate read this file, so the region set, the URL form
// and the provider table have one owner instead of a literal per driver.
//
// The OpenAI-compatible upstream kind was exercised against the live router on
// 2026-08-19, which covers the openai and openai-compat rows. The azure, vertex
// and bedrock rows come from the router team's published field list and have no
// live measurement behind them; every shipped surface that lists them says so
// (FR-034e).

// SlngRouterDocs is the public page the two catalogue reason entries point at,
// read 2026-08-19.
const SlngRouterDocs = "https://docs.slng.ai/context-router/"

// SlngRouterVerified is when the region set, the base URL form and the two
// identity headers were last read off that page.
const SlngRouterVerified = "2026-08-19"

// SlngRouterKeyEnv is the router's own key. One SLNG key serves all three SLNG
// roles, so this is the name the listen and speak entries already use.
const SlngRouterKeyEnv = "SLNG_API_KEY"

// The two identity headers, required on every router request. The agent id
// scopes the cache and is one value per package; the session id is one value
// per call and scopes nothing.
const (
	SlngAgentIDHeader   = "X-Slng-Agent-Id"
	SlngSessionIDHeader = "X-Slng-Session-Id"
)

// The two per-request extensions a router think request carries, named the way
// the OpenAI SDK names them, because that is the vocabulary both targets speak:
// LiveKit passes them through extra_kwargs and Pipecat merges them out of
// Settings.extra, and either way they reach the same request.
//
// SlngRequestBodyArg is also where a router binding's forwardable params go, and
// that is a fact about the router rather than about a target. It lives here and
// not as a field on the two catalogue reason rows, because those rows declare
// how their *class* of entry carries params — LiveKit's as constructor kwargs,
// Pipecat's through a Settings overflow field — and that stays true for the
// non-router entries sharing each class. The router is the one exception, so it
// gets one owner instead of two rows quietly agreeing with each other.
//
// The body and not a kwarg because of what the alternative costs. A param on a
// router binding is passthrough: the compiler does not know what it means and
// the upstream does. On LiveKit a constructor kwarg the plugin never heard of is
// a TypeError when the agent boots, which is a live call that never answers
// instead of a compile that refuses. The body takes any key, and the router
// forwards keys it does not recognise to the upstream (measured 2026-08-27,
// research R5).
const (
	SlngRequestBodyArg    = "extra_body"
	SlngRequestHeadersArg = "extra_headers"
)

// SlngRouterRegions are the router's own regions, in the order the public docs
// list them. SLNG *speech* world parts are na, eu and ap: the same
// params.world_part_override key, a different accepted set. A refusal names
// these four rather than saying "unknown", because `na` copied off the regional
// infrastructure page is the likely mistake (D2).
var SlngRouterRegions = []string{"eu", "us", "india", "indonesia"}

// SlngRouterBaseURL maps a router region onto its regional Chat Completions
// base URL. False for anything outside the set, a speech world part included.
func SlngRouterBaseURL(region string) (string, bool) {
	if !slices.Contains(SlngRouterRegions, region) {
		return "", false
	}
	return "https://" + region + ".context-router.slng.ai/v1", true
}

// SlngAgentIDMaxLen bounds the agent id. The value leaves as an HTTP header
// value rather than a payload, and 128 characters is far under any server's
// header limit while still holding a package name and a version suffix.
const SlngAgentIDMaxLen = 128

// ValidateSlngAgentID holds FR-007. The text is the reason alone; the caller
// names the profile it came from. The value is echoed because an agent id is
// authored rather than secret, and an author cannot fix whitespace they cannot
// see.
func ValidateSlngAgentID(id string) error {
	if id == "" {
		return fmt.Errorf("agent_id is required: it scopes the router's cache, and the author owns the value including the version suffix they bump after a prompt change they judge meaningful")
	}
	if len(id) > SlngAgentIDMaxLen {
		return fmt.Errorf("agent_id is %d characters and the bound is %d: the value becomes the %s header value, and a header is not a payload", len(id), SlngAgentIDMaxLen, SlngAgentIDHeader)
	}
	for _, r := range id {
		if r < 0x21 || r > 0x7e {
			return fmt.Errorf("agent_id %q must be printable ASCII with no whitespace: the value becomes the %s header value", id, SlngAgentIDHeader)
		}
	}
	return nil
}

// SlngScopeSeparator joins the authored agent id to the site that is speaking.
// A colon: printable ASCII, legal in a header value, and it cannot appear in a
// Python identifier, so no agent or task name can hold one by accident. A name
// that does hold one is refused in ir.Validate, because two different sites
// could otherwise derive the same scope and quietly bring back the collision
// this whole shape exists to prevent.
const SlngScopeSeparator = ":"

// SlngSiteKind is what kind of prompt site is speaking.
type SlngSiteKind string

const (
	SlngSiteAgent   SlngSiteKind = "agent"
	SlngSiteTask    SlngSiteKind = "task"
	SlngSiteSummary SlngSiteKind = "summary"
)

// SlngSite is one place in an emitted project that sends a system prompt to the
// router. Not a model profile: two sites can share a profile and still be two
// sites, which is exactly how four agents and two tasks came to share one cache.
type SlngSite struct {
	Kind SlngSiteKind
	// Name is the site's own name from the package. Unused by a summarizer,
	// whose prompt is fixed and which therefore needs only one value.
	Name string
}

// String names the site the way an author wrote it, for a refusal.
func (s SlngSite) String() string {
	if s.Kind == SlngSiteSummary {
		return "the summarizer"
	}
	return string(s.Kind) + " " + s.Name
}

// suffix is what the scope carries after the authored id.
//
// An agent's name is the bare suffix, because an agent is the ordinary case and
// a reader of the router's own logs should see the name they wrote. Every other
// kind namespaces itself with its own kind, so a task called `concierge` and an
// agent called `concierge` cannot derive the same value. That is also why the
// default arm namespaces instead of falling back to the bare name: a kind added
// later is collision-free without anyone having to remember this rule.
func (s SlngSite) suffix() string {
	switch s.Kind {
	case SlngSiteAgent:
		return s.Name
	case SlngSiteSummary:
		return string(SlngSiteSummary)
	default:
		return string(s.Kind) + "." + s.Name
	}
}

// SlngScope is the cache scope one site sends as its agent id header value.
//
// Derived from names, never from prompt content. A wording change must not move
// the scope, or every edit would silently retire a cache the author meant to
// keep. Retiring one is the author's own act, done by bumping the version suffix
// on the authored id (FR-041, FR-043).
func SlngScope(authoredID string, site SlngSite) string {
	return authoredID + SlngScopeSeparator + site.suffix()
}

// ValidateSlngScope bounds the value that actually leaves, which is the derived
// one and not the authored one. A package that fits today can fail after this
// change, and that refusal is the point: a truncated scope would still be sent,
// two sites could truncate to the same string, and the collision would come back
// silently. A value the router rejects outright would instead fail on the first
// turn of a live call, which is the worst place to learn a compile-time fact.
func ValidateSlngScope(authoredID string, site SlngSite) error {
	scope := SlngScope(authoredID, site)
	if len(scope) <= SlngAgentIDMaxLen {
		return nil
	}
	return fmt.Errorf(
		"the cache scope for %s is %d characters and the bound is %d: agent_id %q is %d and the site adds %d, and the two add up because the whole value becomes the %s header value. Shorten the agent_id or the name",
		site, len(scope), SlngAgentIDMaxLen,
		authoredID, len(authoredID), len(scope)-len(authoredID),
		SlngAgentIDHeader,
	)
}

// SlngUpstreamField is one field on an upstream block, and the key it takes
// inside the router's endpoint object.
type SlngUpstreamField struct {
	Authored string // the YAML key an author writes
	Wire     string // the key inside the endpoint object
	Required bool
	// Credential says the value is an environment variable name whose contents
	// the emitted agent reads at run time. Every one is named *_env, which
	// TestSlngUpstreamCredentialsAreEnvNames holds, so a future row cannot
	// introduce a field that takes a literal secret (FR-034a, FR-034d).
	Credential bool
	// Default is what the compiler supplies when the author writes nothing.
	// Only the openai row has any: it is the one upstream whose URL and key
	// name are not the author's to choose.
	Default string
	// JSONObject says the credential's contents are a JSON object rather than a
	// string, so the emitted endpoint carries a parsed object (vertex).
	JSONObject bool
}

// SlngUpstream is one upstream the router accepts.
type SlngUpstream struct {
	Provider string
	// WireProvider is what the endpoint object's own "provider" key carries.
	// Empty omits the key, because the router's default is openai-compat and
	// sending a default says nothing (FR-035).
	WireProvider string
	// Fields is in endpoint-object order, after the provider key.
	Fields []SlngUpstreamField
	// OpenAIModels says this upstream serves OpenAI's own model families, which
	// is what decides whether the reasoning_effort advice in ir.Validate
	// applies. True for openai and azure; false for the three that serve
	// somebody else's models, and false for openai-compat, which speaks the
	// OpenAI wire format on behalf of whoever the author points it at.
	//
	// The distinction earns a column because the alternative is an unactionable
	// warning. On an openai-compat upstream the advice was to set a parameter the
	// pinned host answers with a 400, on every compile, and an unactionable
	// warning is how the actionable ones come to be ignored.
	OpenAIModels bool
}

// slngUpstreams is the provider table: which fields each upstream requires,
// which it accepts, and the name each takes on the wire. Five literals over
// four provider kinds.
var slngUpstreams = []SlngUpstream{
	{
		Provider: "openai", OpenAIModels: true,
		Fields: []SlngUpstreamField{
			{Authored: "url", Wire: "url", Default: "https://api.openai.com/v1"},
			{Authored: "key_env", Wire: "api_key", Credential: true, Default: "OPENAI_API_KEY"},
		},
	},
	{
		Provider: "openai-compat",
		Fields: []SlngUpstreamField{
			{Authored: "url", Wire: "url", Required: true},
			{Authored: "key_env", Wire: "api_key", Required: true, Credential: true},
			// A header name, never a secret: the value still comes from
			// key_env, which is what keeps a package free of literals.
			{Authored: "auth_header", Wire: "auth_header"},
		},
	},
	{
		Provider: "azure", WireProvider: "azure", OpenAIModels: true,
		Fields: []SlngUpstreamField{
			{Authored: "url", Wire: "url", Required: true},
			{Authored: "key_env", Wire: "api_key", Required: true, Credential: true},
			{Authored: "deployment", Wire: "azure_deployment", Required: true},
			{Authored: "api_version", Wire: "api_version", Required: true},
		},
	},
	{
		Provider: "vertex", WireProvider: "vertex",
		Fields: []SlngUpstreamField{
			{Authored: "credentials_env", Wire: "vertex_credentials", Required: true, Credential: true, JSONObject: true},
			{Authored: "location", Wire: "vertex_location", Required: true},
			{Authored: "project", Wire: "vertex_project"},
		},
	},
	{
		Provider: "bedrock", WireProvider: "bedrock",
		Fields: []SlngUpstreamField{
			{Authored: "access_key_id_env", Wire: "aws_access_key_id", Required: true, Credential: true},
			{Authored: "secret_access_key_env", Wire: "aws_secret_access_key", Required: true, Credential: true},
			{Authored: "region", Wire: "aws_region", Required: true},
			// The Bedrock model id differs from the entry label, which is the
			// one upstream where model_id earns its place on the wire.
			{Authored: "model_id", Wire: "model_id", Required: true},
			{Authored: "session_token_env", Wire: "aws_session_token", Credential: true},
		},
	},
}

// SlngUpstreams lists the accepted upstreams in table order.
func SlngUpstreams() []SlngUpstream { return slices.Clone(slngUpstreams) }

// SlngUpstreamProviders names the accepted providers, so a refusal can list
// them instead of saying "unknown".
func SlngUpstreamProviders() []string {
	out := make([]string, 0, len(slngUpstreams))
	for _, upstream := range slngUpstreams {
		out = append(out, upstream.Provider)
	}
	return out
}

// SlngUpstreamByName finds one row.
func SlngUpstreamByName(provider string) (SlngUpstream, bool) {
	for _, upstream := range slngUpstreams {
		if upstream.Provider == provider {
			return upstream, true
		}
	}
	return SlngUpstream{}, false
}

// Accepts describes the row for a refusal: what the author must write and what
// they may. An author who wrote the wrong key needs the right list, not a
// verdict.
func (u SlngUpstream) Accepts() string {
	var required, optional []string
	for _, field := range u.Fields {
		if field.Required {
			required = append(required, field.Authored)
		} else {
			optional = append(optional, field.Authored)
		}
	}
	must := "nothing else"
	if len(required) > 0 {
		must = strings.Join(required, ", ")
	}
	if len(optional) == 0 {
		return fmt.Sprintf("%s requires %s", u.Provider, must)
	}
	return fmt.Sprintf("%s requires %s and accepts %s", u.Provider, must, strings.Join(optional, ", "))
}

// ValidateSlngUpstream holds FR-034 and FR-034c: the provider is one of the
// five, every field that provider requires is present, and no field belonging
// to another provider is set. authored maps an authored key to its value, with
// empty values already dropped.
//
// A key the table does not expect is a refusal rather than a pass-through,
// because the router answers an unknown endpoint key with a 400 on every think
// request: a compile-time fact discovered on the first turn of a call.
func ValidateSlngUpstream(provider string, authored map[string]string) []string {
	upstream, ok := SlngUpstreamByName(provider)
	if !ok {
		accepted := strings.Join(SlngUpstreamProviders(), ", ")
		if provider == "" {
			return []string{fmt.Sprintf("upstream needs a provider: the router has to be told which upstream serves the model, and nothing is assumed for you because the credentials are yours. One of %s", accepted)}
		}
		return []string{fmt.Sprintf("upstream provider %q is not one the router accepts. One of %s", provider, accepted)}
	}
	var errors []string
	accepted := make(map[string]bool, len(upstream.Fields))
	for _, field := range upstream.Fields {
		accepted[field.Authored] = true
		if field.Required && authored[field.Authored] == "" {
			errors = append(errors, fmt.Sprintf("upstream provider %q is missing %s: %s", provider, field.Authored, upstream.Accepts()))
		}
	}
	for _, key := range slices.Sorted(maps.Keys(authored)) {
		if accepted[key] {
			continue
		}
		if owners := slngUpstreamFieldOwners(key); len(owners) > 0 {
			errors = append(errors, fmt.Sprintf("upstream %s belongs to provider %s, not %q: %s", key, strings.Join(owners, " and "), provider, upstream.Accepts()))
			continue
		}
		errors = append(errors, fmt.Sprintf("upstream has no field %s: %s", key, upstream.Accepts()))
	}
	return errors
}

// slngUpstreamFieldOwners names the providers that do accept a field, so a
// misplaced key points at the row it came from.
func slngUpstreamFieldOwners(authored string) []string {
	var owners []string
	for _, upstream := range slngUpstreams {
		for _, field := range upstream.Fields {
			if field.Authored == authored {
				owners = append(owners, upstream.Provider)
				break
			}
		}
	}
	return owners
}

// SlngEndpointField is one resolved key inside the emitted endpoint object.
type SlngEndpointField struct {
	Wire string
	// Value is a literal, or an environment variable name when Env is set. A
	// credential value is never read here, only named (FR-036).
	Value      string
	Env        bool
	JSONObject bool
}

// SlngResolveUpstream turns a validated upstream block into the ordered
// endpoint object the request carries. A default fills in only where the table
// carries one, and a field with neither a value nor a default is omitted, which
// is what keeps the emitted config to the smallest shape that works (FR-035).
func SlngResolveUpstream(provider string, authored map[string]string) ([]SlngEndpointField, bool) {
	upstream, ok := SlngUpstreamByName(provider)
	if !ok {
		return nil, false
	}
	var out []SlngEndpointField
	if upstream.WireProvider != "" {
		out = append(out, SlngEndpointField{Wire: "provider", Value: upstream.WireProvider})
	}
	for _, field := range upstream.Fields {
		value := cmp.Or(authored[field.Authored], field.Default)
		if value == "" {
			continue
		}
		out = append(out, SlngEndpointField{
			Wire: field.Wire, Value: value,
			Env: field.Credential, JSONObject: field.JSONObject,
		})
	}
	return out, true
}
