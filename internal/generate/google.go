package generate

import (
	"github.com/slng-ai/unmute/internal/target"
)

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

func googleReason(fw target.Provider, role target.Role, vendor string) bool {
	return (fw == target.LiveKit || fw == target.Pipecat) && role == target.Reason && (vendor == "google" || vendor == "gemini")
}
