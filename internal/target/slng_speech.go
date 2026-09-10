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
	if _, set := params["region_override"]; set {
		return "", fmt.Errorf("params.region_override is no longer supported for SLNG speech: remove it and choose params.world_part_override, for example eu-north")
	}
	value, set := params["world_part_override"]
	if !set {
		return "", nil
	}
	part, ok := value.(string)
	if !ok || !slices.Contains(SlngSpeechWorldParts, part) {
		return "", fmt.Errorf("params.world_part_override %v is not an SLNG speech gateway: choose one of %s; replace legacy na, eu or ap with a specific gateway", value, strings.Join(SlngSpeechWorldParts, ", "))
	}
	for _, key := range []string{"base_url", "slng_base_url"} {
		if _, set := params[key]; set {
			return "", fmt.Errorf("params.world_part_override already selects the SLNG speech gateway: remove params.%s or world_part_override", key)
		}
	}
	return part + ".api.slng.ai", nil
}
