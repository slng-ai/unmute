package target

import (
	"fmt"
	"slices"
	"strings"
)

// SlngWorldParts are SLNG's world parts: the speech gateways and, since the
// Context Router was widened to the same set, the router endpoints too. One
// list because one vocabulary: an author who learns a world part for listening
// writes the same word for thinking.
//
// Worker deployment regions are still a separate question, and a separate set.
// If SLNG ever serves a world part for one role and not the other, this splits
// back into two lists rather than growing a per-role filter here.
var SlngWorldParts = []string{
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
	if !ok || !slices.Contains(SlngWorldParts, part) {
		return "", fmt.Errorf("params.world_part %v is not an SLNG speech gateway: choose one of %s; replace legacy na, eu or ap with a specific gateway", value, strings.Join(SlngWorldParts, ", "))
	}
	for _, key := range []string{"base_url", "slng_base_url"} {
		if _, set := params[key]; set {
			return "", fmt.Errorf("params.world_part already selects the SLNG speech gateway: remove params.%s or world_part", key)
		}
	}
	return part + ".api.slng.ai", nil
}
