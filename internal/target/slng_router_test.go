package target

import (
	"strings"
	"testing"
)

func TestSlngRouterBaseURL(t *testing.T) {
	for _, tc := range []struct {
		region string
		want   string
	}{
		{"eu", "https://eu.context-router.slng.ai/v1"},
		{"us", "https://us.context-router.slng.ai/v1"},
		{"india", "https://india.context-router.slng.ai/v1"},
		{"indonesia", "https://indonesia.context-router.slng.ai/v1"},
	} {
		got, ok := SlngRouterBaseURL(tc.region)
		if !ok || got != tc.want {
			t.Errorf("SlngRouterBaseURL(%q) = %q, %v; want %q, true", tc.region, got, ok, tc.want)
		}
	}
	// The speech world parts share the params key and not the accepted set, so
	// each has to be refused here rather than silently produce a host that does
	// not exist (D2).
	for _, region := range []string{"", "na", "ap", "EU", "eu ", "europe"} {
		if got, ok := SlngRouterBaseURL(region); ok {
			t.Errorf("SlngRouterBaseURL(%q) = %q, true; want refused", region, got)
		}
	}
}

func TestValidateSlngAgentID(t *testing.T) {
	for _, id := range []string{"salon-concierge-v1", "a", "pkg.name_v12", strings.Repeat("x", SlngAgentIDMaxLen)} {
		if err := ValidateSlngAgentID(id); err != nil {
			t.Errorf("ValidateSlngAgentID(%q) = %v; want accepted", id, err)
		}
	}
	for _, tc := range []struct {
		name, id, wants string
	}{
		{"empty", "", "required"},
		{"space", "salon concierge v1", "printable ASCII"},
		{"tab", "salon\tv1", "printable ASCII"},
		{"newline", "salon-v1\n", "printable ASCII"},
		{"control", "salon-v1\x01", "printable ASCII"},
		{"non ascii", "salón-v1", "printable ASCII"},
		{"over long", strings.Repeat("x", SlngAgentIDMaxLen+1), "the bound is"},
	} {
		err := ValidateSlngAgentID(tc.id)
		if err == nil {
			t.Errorf("%s: ValidateSlngAgentID(%q) = nil; want a refusal", tc.name, tc.id)
			continue
		}
		if !strings.Contains(err.Error(), tc.wants) {
			t.Errorf("%s: refusal %q does not say %q", tc.name, err, tc.wants)
		}
		// The header is why the rule exists, so the message names it rather than
		// leaving the author to guess where the value goes.
		if tc.id != "" && !strings.Contains(err.Error(), SlngAgentIDHeader) {
			t.Errorf("%s: refusal %q does not name the header", tc.name, err)
		}
	}
}

// validAuthored fills every field a provider requires, taking the value shape
// from the table so a new row is covered without editing this helper.
func validAuthored(u SlngUpstream) map[string]string {
	out := map[string]string{}
	for _, field := range u.Fields {
		if !field.Required {
			continue
		}
		if field.Credential {
			out[field.Authored] = "UPSTREAM_VALUE_ENV"
			continue
		}
		out[field.Authored] = "value"
	}
	return out
}

func TestSlngRouterProviderTableAcceptsItsOwnRequiredFields(t *testing.T) {
	for _, upstream := range SlngUpstreams() {
		if errs := ValidateSlngUpstream(upstream.Provider, validAuthored(upstream)); len(errs) > 0 {
			t.Errorf("%s: a fully populated block was refused: %v", upstream.Provider, errs)
		}
	}
}

func TestSlngRouterProviderTableMissingRequiredField(t *testing.T) {
	for _, upstream := range SlngUpstreams() {
		for _, field := range upstream.Fields {
			if !field.Required {
				continue
			}
			authored := validAuthored(upstream)
			delete(authored, field.Authored)
			errs := ValidateSlngUpstream(upstream.Provider, authored)
			if len(errs) == 0 {
				t.Errorf("%s without %s was accepted; want a refusal", upstream.Provider, field.Authored)
				continue
			}
			joined := strings.Join(errs, "\n")
			if !strings.Contains(joined, field.Authored) {
				t.Errorf("%s without %s: refusal does not name the field: %s", upstream.Provider, field.Authored, joined)
			}
			// FR-034c: the missing name alone leaves the author guessing at the
			// rest of the row, so the refusal carries what the provider accepts.
			if !strings.Contains(joined, upstream.Accepts()) {
				t.Errorf("%s without %s: refusal does not list what the provider accepts: %s", upstream.Provider, field.Authored, joined)
			}
		}
	}
}

func TestSlngRouterProviderTableUnknownProvider(t *testing.T) {
	for _, provider := range []string{"", "anthropic", "openai_compat", "OpenAI"} {
		errs := ValidateSlngUpstream(provider, map[string]string{})
		if len(errs) == 0 {
			t.Fatalf("provider %q was accepted; want a refusal", provider)
		}
		joined := strings.Join(errs, "\n")
		for _, accepted := range SlngUpstreamProviders() {
			if !strings.Contains(joined, accepted) {
				t.Errorf("provider %q: refusal does not name %q: %s", provider, accepted, joined)
			}
		}
	}
}

func TestSlngRouterProviderTableForeignField(t *testing.T) {
	for _, tc := range []struct {
		provider, key, owner string
	}{
		{"azure", "location", "vertex"},
		{"vertex", "deployment", "azure"},
		{"bedrock", "api_version", "azure"},
		{"openai", "deployment", "azure"},
		{"vertex", "key_env", "openai"},
	} {
		authored := validAuthored(mustUpstream(t, tc.provider))
		authored[tc.key] = "value"
		errs := ValidateSlngUpstream(tc.provider, authored)
		if len(errs) == 0 {
			t.Errorf("%s carrying %s was accepted; want a refusal", tc.provider, tc.key)
			continue
		}
		joined := strings.Join(errs, "\n")
		if !strings.Contains(joined, tc.key) || !strings.Contains(joined, tc.owner) {
			t.Errorf("%s carrying %s: refusal names neither the key nor %q: %s", tc.provider, tc.key, tc.owner, joined)
		}
	}
	// A key belonging to nobody still gets the accepted list rather than silence.
	errs := ValidateSlngUpstream("openai", map[string]string{"endpoint": "value"})
	if len(errs) == 0 || !strings.Contains(strings.Join(errs, "\n"), "endpoint") {
		t.Errorf("an unknown key was not refused by name: %v", errs)
	}
}

// TestSlngRouterProviderTableCredentialsAreEnvNames is the structural half of
// FR-034a and FR-034d: a field the table treats as a credential is always named
// *_env, so a future row cannot introduce a field that takes a literal secret,
// and a field named *_env is always treated as one.
func TestSlngRouterProviderTableCredentialsAreEnvNames(t *testing.T) {
	for _, upstream := range SlngUpstreams() {
		for _, field := range upstream.Fields {
			suffixed := strings.HasSuffix(field.Authored, "_env")
			if field.Credential != suffixed {
				t.Errorf("%s.%s: credential=%v but the name %s say _env; the two have to agree or a package can hold a secret",
					upstream.Provider, field.Authored, field.Credential, map[bool]string{true: "does", false: "does not"}[suffixed])
			}
		}
	}
}

func TestSlngRouterProviderTableEndpointObject(t *testing.T) {
	for _, tc := range []struct {
		provider string
		authored map[string]string
		want     []SlngEndpointField
	}{
		{
			// The compiler supplies both defaults, so the smallest legal block
			// still produces a complete endpoint (FR-035).
			provider: "openai", authored: map[string]string{},
			want: []SlngEndpointField{
				{Wire: "url", Value: "https://api.openai.com/v1"},
				{Wire: "api_key", Value: "OPENAI_API_KEY", Env: true},
			},
		},
		{
			provider: "openai", authored: map[string]string{"url": "https://proxy.internal/v1", "key_env": "PROXY_KEY"},
			want: []SlngEndpointField{
				{Wire: "url", Value: "https://proxy.internal/v1"},
				{Wire: "api_key", Value: "PROXY_KEY", Env: true},
			},
		},
		{
			// No provider key: openai-compat is the router's own default, and
			// sending a default says nothing.
			provider: "openai-compat", authored: map[string]string{"url": "https://host/v1", "key_env": "HOST_KEY", "auth_header": "x-goog-api-key"},
			want: []SlngEndpointField{
				{Wire: "url", Value: "https://host/v1"},
				{Wire: "api_key", Value: "HOST_KEY", Env: true},
				{Wire: "auth_header", Value: "x-goog-api-key"},
			},
		},
		{
			provider: "azure", authored: map[string]string{"url": "https://r.cognitiveservices.azure.com/", "key_env": "AZURE_OPENAI_API_KEY", "deployment": "gpt-4o-deploy", "api_version": "2024-12-01-preview"},
			want: []SlngEndpointField{
				{Wire: "provider", Value: "azure"},
				{Wire: "url", Value: "https://r.cognitiveservices.azure.com/"},
				{Wire: "api_key", Value: "AZURE_OPENAI_API_KEY", Env: true},
				{Wire: "azure_deployment", Value: "gpt-4o-deploy"},
				{Wire: "api_version", Value: "2024-12-01-preview"},
			},
		},
		{
			// project is optional and omitted, because the key carries its own.
			provider: "vertex", authored: map[string]string{"credentials_env": "GCP_KEY", "location": "europe-west4"},
			want: []SlngEndpointField{
				{Wire: "provider", Value: "vertex"},
				{Wire: "vertex_credentials", Value: "GCP_KEY", Env: true, JSONObject: true},
				{Wire: "vertex_location", Value: "europe-west4"},
			},
		},
		{
			// The widest row: three credentials and a model id of its own.
			provider: "bedrock", authored: map[string]string{
				"access_key_id_env": "AWS_ID", "secret_access_key_env": "AWS_SECRET",
				"session_token_env": "AWS_TOKEN", "region": "eu-central-1",
				"model_id": "anthropic.claude-3-5-sonnet-20241022-v2:0",
			},
			want: []SlngEndpointField{
				{Wire: "provider", Value: "bedrock"},
				{Wire: "aws_access_key_id", Value: "AWS_ID", Env: true},
				{Wire: "aws_secret_access_key", Value: "AWS_SECRET", Env: true},
				{Wire: "aws_region", Value: "eu-central-1"},
				{Wire: "model_id", Value: "anthropic.claude-3-5-sonnet-20241022-v2:0"},
				{Wire: "aws_session_token", Value: "AWS_TOKEN", Env: true},
			},
		},
	} {
		got, ok := SlngResolveUpstream(tc.provider, tc.authored)
		if !ok {
			t.Errorf("%s: SlngResolveUpstream reported no such provider", tc.provider)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("%s: endpoint has %d fields, want %d: %+v", tc.provider, len(got), len(tc.want), got)
			continue
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("%s: endpoint field %d = %+v, want %+v", tc.provider, i, got[i], tc.want[i])
			}
		}
		// cache_enabled was measured to change nothing, so shipping it would
		// imply control the author does not have (FR-035).
		for _, field := range got {
			if field.Wire == "cache_enabled" || field.Wire == "model_id" && tc.provider != "bedrock" {
				t.Errorf("%s: endpoint carries %s, which stays out unless the upstream forces it", tc.provider, field.Wire)
			}
		}
	}
}

func mustUpstream(t *testing.T, provider string) SlngUpstream {
	t.Helper()
	upstream, ok := SlngUpstreamByName(provider)
	if !ok {
		t.Fatalf("no upstream row for %q", provider)
	}
	return upstream
}

func TestSlngScope(t *testing.T) {
	const authored = "optimized-salon-concierge-v3"
	for _, tc := range []struct {
		site SlngSite
		want string
	}{
		{SlngSite{Kind: SlngSiteAgent, Name: "concierge"}, "optimized-salon-concierge-v3:concierge"},
		{SlngSite{Kind: SlngSiteAgent, Name: "booking_specialist"}, "optimized-salon-concierge-v3:booking_specialist"},
		{SlngSite{Kind: SlngSiteTask, Name: "customer_verification"}, "optimized-salon-concierge-v3:task.customer_verification"},
		{SlngSite{Kind: SlngSiteTask, Name: "booking"}, "optimized-salon-concierge-v3:task.booking"},
		{SlngSite{Kind: SlngSiteSummary}, "optimized-salon-concierge-v3:summary"},
	} {
		if got := SlngScope(authored, tc.site); got != tc.want {
			t.Errorf("SlngScope(%q, %v) = %q; want %q", authored, tc.site, got, tc.want)
		}
	}
}

// An agent and a task may share a name. They are still two sites with two
// prompts, so they must not share a scope: that sharing is the whole defect.
func TestSlngScopeSeparatesKindsSharingAName(t *testing.T) {
	agent := SlngScope("pkg-v1", SlngSite{Kind: SlngSiteAgent, Name: "booking"})
	task := SlngScope("pkg-v1", SlngSite{Kind: SlngSiteTask, Name: "booking"})
	if agent == task {
		t.Fatalf("agent and task named booking both derived %q", agent)
	}
	prefix := "pkg-v1" + SlngScopeSeparator
	if !strings.HasPrefix(agent, prefix) || !strings.HasPrefix(task, prefix) {
		t.Errorf("both scopes must carry the authored prefix: %q and %q", agent, task)
	}
}

// The scope is derived from names only. A prompt cannot move it, because a
// prompt is not a parameter: the signature is (authoredID, site) and a site
// holds a kind and a name. This asserts the consequence an author depends on,
// which is that the same package compiles to the same scope every time, so only
// their own version suffix retires a cache.
func TestSlngScopeIsStableAcrossCalls(t *testing.T) {
	site := SlngSite{Kind: SlngSiteAgent, Name: "concierge"}
	first := SlngScope("pkg-v1", site)
	for range 100 {
		if got := SlngScope("pkg-v1", site); got != first {
			t.Fatalf("SlngScope is not stable: %q then %q", first, got)
		}
	}
}

func TestValidateSlngScope(t *testing.T) {
	// The bound is on the derived value, so an authored id that passes
	// ValidateSlngAgentID on its own can still fail here. That is the point.
	longEnoughToFail := strings.Repeat("x", SlngAgentIDMaxLen)
	if err := ValidateSlngAgentID(longEnoughToFail); err != nil {
		t.Fatalf("fixture must pass the authored-value rule first: %v", err)
	}
	site := SlngSite{Kind: SlngSiteAgent, Name: "concierge"}
	err := ValidateSlngScope(longEnoughToFail, site)
	if err == nil {
		t.Fatalf("ValidateSlngScope accepted a %d-character scope; want refused", len(SlngScope(longEnoughToFail, site)))
	}
	// The author has two names to choose between, so the message has to say
	// which site produced the value and how the two lengths add up.
	for _, want := range []string{"agent concierge", "128", SlngAgentIDHeader, "Shorten"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not mention %q", err, want)
		}
	}
	// A scope that lands exactly on the bound is accepted: the bound is a
	// header-value limit, not a margin.
	exact := strings.Repeat("y", SlngAgentIDMaxLen-len(SlngScopeSeparator)-len("concierge"))
	if err := ValidateSlngScope(exact, site); err != nil {
		t.Errorf("ValidateSlngScope refused a scope of exactly %d characters: %v", SlngAgentIDMaxLen, err)
	}
}
