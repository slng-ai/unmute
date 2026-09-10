package target

import (
	"fmt"
	"slices"
	"strings"
)

// SlngSpeechWorldParts selects speech gateways, independently of worker
// deployment regions and the Context Router's own region set.
var SlngSpeechWorldParts = []string{
	"us-east", "us-west", "br", "eu-west", "eu-north", "gb", "za",
	"il", "jp", "sg", "id", "in", "au",
}

// SlngSpeechBaseURL resolves the author's gateway choice. An omitted choice
// leaves the SDK default alone; a legacy code is never mapped to a guessed site.
func SlngSpeechBaseURL(params map[string]any) (string, error) {
	for _, key := range []string{"region_override", "world_part_override"} {
		if _, set := params[key]; set {
			return "", fmt.Errorf("params.%s is no longer supported for SLNG speech: replace it with params.world_part (for example, eu-north)", key)
		}
	}
	value, set := params["world_part"]
	if !set {
		return "", nil
	}
	part, ok := value.(string)
	if !ok || !slices.Contains(SlngSpeechWorldParts, part) {
		return "", fmt.Errorf("params.world_part %v is not an SLNG speech gateway: choose one of %s; replace legacy na, eu or ap with a specific gateway", value, strings.Join(SlngSpeechWorldParts, ", "))
	}
	for _, key := range []string{"base_url", "slng_base_url"} {
		if _, set := params[key]; set {
			return "", fmt.Errorf("params.world_part already selects the SLNG speech gateway: remove params.%s or world_part", key)
		}
	}
	return part + ".api.slng.ai", nil
}
