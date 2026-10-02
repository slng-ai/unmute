package target

import (
	"fmt"
	"slices"
	"strings"
)

// SlngSpeechGateway is the SLNG speech gateway a listen or speak binding
// names. WorldPart is "" when the author left it out, which only LiveKit
// allows: livekit-plugins-slng 1.8.4 still has a default host, and
// pipecat-slng 0.6.0 has none and raises TypeError at construction.
type SlngSpeechGateway struct {
	WorldPart string
}

// ParseSlngSpeechGateway resolves the author's gateway choice for one
// framework. A legacy code is never mapped to a guessed site, and an omitted
// choice on Pipecat is refused rather than filled with one.
func ParseSlngSpeechGateway(fw Provider, params map[string]any) (SlngSpeechGateway, error) {
	for _, key := range []string{"region_override", "world_part_override"} {
		if _, set := params[key]; set {
			return SlngSpeechGateway{}, fmt.Errorf("params.%s is no longer supported for SLNG speech: replace it with params.world_part (for example, eu-north)", key)
		}
	}
	value, set := params["world_part"]
	if !set {
		if fw == Pipecat {
			return SlngSpeechGateway{}, fmt.Errorf("params.world_part is required for SLNG speech on pipecat, which has no default gateway: choose one of %s", strings.Join(SlngRegions, ", "))
		}
		return SlngSpeechGateway{}, nil
	}
	part, ok := value.(string)
	if !ok || !slices.Contains(SlngRegions, part) {
		return SlngSpeechGateway{}, fmt.Errorf("params.world_part %v is not an SLNG speech gateway: choose one of %s; replace legacy na, eu or ap with a specific gateway", value, strings.Join(SlngRegions, ", "))
	}
	for _, key := range []string{"base_url", "slng_base_url"} {
		if _, set := params[key]; set {
			return SlngSpeechGateway{}, fmt.Errorf("params.world_part already selects the SLNG speech gateway: remove params.%s or world_part", key)
		}
	}
	return SlngSpeechGateway{WorldPart: part}, nil
}

// Host is the gateway host without scheme, or "" when no world part was set.
func (g SlngSpeechGateway) Host() string {
	if g.WorldPart == "" {
		return ""
	}
	return g.WorldPart + ".api.slng.ai"
}
