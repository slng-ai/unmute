package generate

import (
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// The pinned native plugins require OAuth for Vertex. This bridge changes only
// authentication; their own streaming, tool conversion and thought signatures
// remain in charge. Keep the client replacement covered against both SDK pins.
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
def _google_vertex_client(api_key, location):
    from google import genai as _google_genai

    # Google's multi-region host differs from its standard regional host:
    # https://cloud.google.com/vertex-ai/generative-ai/docs/learn/locations
    if location in ("us", "eu"):
        host = f"aiplatform.{location}.rep.googleapis.com"
    elif location == "global":
        host = "aiplatform.googleapis.com"
    else:
        host = f"{location}-aiplatform.googleapis.com"
    # Pin the host too: API-key defaults and environment settings must not
    # reroute an explicit location. Collection scope keeps the API-key path
    # project-free even when location is set; it also omits the API version.
    return _google_genai.Client(
        vertexai=True,
        api_key=api_key,
        location=location,
        http_options=_google_genai.types.HttpOptions(
            base_url=f"https://{host}/v1beta1",
            base_url_resource_scope=_google_genai.types.ResourceScope.COLLECTION,
            api_version="v1beta1",
        ),
    )

`

const googleVertexLiveKit = `
class _GoogleVertexLLM(google.LLM):
    def __init__(self, *, api_key, location, **kwargs):
        super().__init__(api_key=api_key, vertexai=False, **kwargs)
        # No request has run. Replace the plugin's unauthenticated Vertex path
        # with the official GenAI API-key client before even prewarming it.
        self._client.close()
        self._client = _google_vertex_client(api_key, location)

`

const googleVertexPipecat = `
class _GoogleVertexLLM(GoogleLLMService):
    def __init__(self, *, location, **kwargs):
        self._vertex_location = location
        super().__init__(**kwargs)

    def create_client(self):
        self._client = _google_vertex_client(self._api_key, self._vertex_location)

`

func googleReason(fw target.Provider, role target.Role, vendor string) bool {
	return (fw == target.LiveKit || fw == target.Pipecat) && role == target.Reason && (vendor == "google" || vendor == "gemini")
}
