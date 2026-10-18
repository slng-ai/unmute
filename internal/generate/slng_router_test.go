package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
)

// The gates the SLNG Context Router's emitted shape needs. Two of them hold the
// rules with the worst silent cost: a split agent id splits one package's cache
// into that many namespaces, and a split session id ungroups one call's requests.
// Neither fails anything at run time. The agent is just never fast, or its
// support trail is unreadable, so only a check on what a driver actually wrote
// can catch a later refactor reintroducing composition.

const (
	routerAgentID = "safe-core-router-v3"
	routerModel   = "gpt-5.6-luna"
)

// FR-035, one case per upstream, asserted key by key. The emitted configuration
// is the smallest shape that works: one tier, one entry, weight 100, and an
// endpoint holding exactly the resolved provider's fields. `provider` stays out
// when it resolves to the router's own default, `model_id` stays out unless the
// upstream requires it, and `cache_enabled` is never sent at all.
func TestSlngRouterUpstreamEndpointObject(t *testing.T) {
	for _, tc := range []struct {
		name     string
		upstream *ir.Upstream
		want     string
	}{
		{
			name: "openai supplies both defaults", upstream: &ir.Upstream{Provider: "openai"},
			want: `{"tiers": {"1": [{"endpoint": {"api_key": os.environ["OPENAI_API_KEY"], "url": "https://api.openai.com/v1"}, "model": "gpt-5.6-luna", "weight": 100}]}}`,
		},
		{
			name: "openai-compat sends no provider key",
			upstream: &ir.Upstream{
				Provider: "openai-compat", URL: "https://host/v1", KeyEnv: "HOST_KEY", AuthHeader: "x-goog-api-key",
			},
			want: `{"tiers": {"1": [{"endpoint": {"api_key": os.environ["HOST_KEY"], "auth_header": "x-goog-api-key", "url": "https://host/v1"}, "model": "gpt-5.6-luna", "weight": 100}]}}`,
		},
		{
			name: "azure names itself and its deployment",
			upstream: &ir.Upstream{
				Provider: "azure", URL: "https://r.cognitiveservices.azure.com/", KeyEnv: "AZURE_OPENAI_API_KEY",
				Deployment: "gpt-4o-deploy", APIVersion: "2024-12-01-preview",
			},
			want: `{"tiers": {"1": [{"endpoint": {"api_key": os.environ["AZURE_OPENAI_API_KEY"], "api_version": "2024-12-01-preview", "azure_deployment": "gpt-4o-deploy", "provider": "azure", "url": "https://r.cognitiveservices.azure.com/"}, "model": "gpt-5.6-luna", "weight": 100}]}}`,
		},
		{
			name: "vertex resolves its key through the helper",
			upstream: &ir.Upstream{
				Provider: "vertex", CredentialsEnv: "GCP_SERVICE_ACCOUNT", Location: "europe-west4",
			},
			want: `{"tiers": {"1": [{"endpoint": {"provider": "vertex", "vertex_credentials": _slng_vertex_credentials("GCP_SERVICE_ACCOUNT"), "vertex_location": "europe-west4"}, "model": "gpt-5.6-luna", "weight": 100}]}}`,
		},
		{
			name: "bedrock is the only row carrying model_id",
			upstream: &ir.Upstream{
				Provider: "bedrock", AccessKeyIDEnv: "AWS_ACCESS_KEY_ID",
				SecretAccessKeyEnv: "AWS_SECRET_ACCESS_KEY", SessionTokenEnv: "AWS_SESSION_TOKEN",
				Region: "eu-central-1", ModelID: "anthropic.claude-3-5-sonnet-20241022-v2:0",
			},
			want: `{"tiers": {"1": [{"endpoint": {"aws_access_key_id": os.environ["AWS_ACCESS_KEY_ID"], "aws_region": "eu-central-1", "aws_secret_access_key": os.environ["AWS_SECRET_ACCESS_KEY"], "aws_session_token": os.environ["AWS_SESSION_TOKEN"], "model_id": "anthropic.claude-3-5-sonnet-20241022-v2:0", "provider": "bedrock"}, "model": "gpt-5.6-luna", "weight": 100}]}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := slngConfigBody(ir.Binding{
				Provider: ir.ProviderSlngRouter, Model: routerModel, AgentID: routerAgentID,
				Upstream: tc.upstream,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("endpoint object drifted\n got: %s\nwant: %s", got, tc.want)
			}
			if strings.Contains(got, "cache_enabled") {
				t.Error("cache_enabled is never sent: it was measured to change nothing")
			}
			// No credential value is ever written, only named.
			for _, value := range []string{"sk-", "AKIA", "-----BEGIN"} {
				if strings.Contains(got, value) {
					t.Errorf("the configuration holds something that looks like a secret value: %q", value)
				}
			}
		})
	}
}

// slngProvenanceKeys is the line's field set and its order, from
// contracts/provenance-log.md. One list, read by two gates, because the claim is
// that both drivers write the same line.
var slngProvenanceKeys = []string{"scope=", "source=", "layer=", "model=", "request_id="}
