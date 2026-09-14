package generate

import (
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// The pinned native plugins require OAuth for Vertex. This bridge changes only
// authentication; their own streaming, tool conversion and thought signatures
// remain in charge. Keep the client replacement covered against both SDK pins.
// ponytail: EU API-key auth only; add another location when a package needs it.
func googleVertexHelpers(tgt ir.Target) string {
	for _, binding := range tgt.Models.Reason {
		if (binding.Provider == "google" || binding.Provider == "gemini") && binding.Params["vertexai"] == true {
			if tgt.Provider == ir.ProviderLiveKit {
				return googleVertexClient + googleVertexLiveKit
			}
			return googleVertexClient + googleVertexPipecat
		}
	}
	return ""
}

const googleVertexClient = `
def _google_vertex_client(api_key):
    from google import genai as _google_genai

    # Pin the host as well as the location: API-key defaults can route globally.
    return _google_genai.Client(
        vertexai=True,
        api_key=api_key,
        http_options={
            "base_url": "https://aiplatform.eu.rep.googleapis.com",
            "api_version": "v1beta1",
        },
    )

`

const googleVertexLiveKit = `
class _GoogleVertexLLM(google.LLM):
    def __init__(self, *, api_key, **kwargs):
        super().__init__(api_key=api_key, vertexai=False, **kwargs)
        # No request has run. Replace the plugin's unauthenticated Vertex path
        # with the official GenAI API-key client before even prewarming it.
        self._client.close()
        self._client = _google_vertex_client(api_key)

`

const googleVertexPipecat = `
class _GoogleVertexLLM(GoogleLLMService):
    def create_client(self):
        self._client = _google_vertex_client(self._api_key)

`

func googleReason(fw target.Provider, role target.Role, vendor string) bool {
	return (fw == target.LiveKit || fw == target.Pipecat) && role == target.Reason && (vendor == "google" || vendor == "gemini")
}
