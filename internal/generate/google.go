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
def _google_vertex_client(api_key: str, location: str):  # noqa: ANN202 - genai.Client is only imported inside
    """Build a GenAI client that reaches Vertex with an API key.

    Args:
        api_key: The Vertex API key.
        location: A Vertex region, or us, eu or global for a multi-region host.

    Returns:
        A google.genai Client pinned to that location's Vertex host.
    """
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
    """The Google LLM plugin, with its client swapped for a Vertex API-key one."""

    def __init__(self, *, api_key: str, location: str, **kwargs: object) -> None:
        """Build the plugin, then replace its client before any request runs.

        Args:
            api_key: The Vertex API key.
            location: A Vertex region, or us, eu or global for a multi-region host.
            **kwargs: The plugin's own constructor arguments.
        """
        # The kwargs are the plugin's own, typed there; naming them here would need Any.
        super().__init__(api_key=api_key, vertexai=False, **kwargs)  # ty: ignore[invalid-argument-type]
        # No request has run. Replace the plugin's unauthenticated Vertex path
        # with the official GenAI API-key client before even prewarming it.
        self._client.close()
        self._client = _google_vertex_client(api_key, location)

`

const googleVertexPipecat = `
class _GoogleVertexLLM(GoogleLLMService):
    """The Google LLM service, with its client swapped for a Vertex API-key one."""

    def __init__(self, *, location: str, **kwargs: object) -> None:
        """Remember the Vertex location, then build the service.

        Args:
            location: A Vertex region, or us, eu or global for a multi-region host.
            **kwargs: The service's own constructor arguments.
        """
        self._vertex_location = location
        # The kwargs are the service's own, typed there; naming them here would need Any.
        super().__init__(**kwargs)  # ty: ignore[invalid-argument-type]

    def create_client(self) -> None:
        """Build the Vertex client the service calls through."""
        self._client = _google_vertex_client(self._api_key, self._vertex_location)

`

func googleReason(fw target.Provider, role target.Role, vendor string) bool {
	return (fw == target.LiveKit || fw == target.Pipecat) && role == target.Reason && (vendor == "google" || vendor == "gemini")
}
