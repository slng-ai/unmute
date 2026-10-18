package generate

import (
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

func TestSlngSpeechGateways(t *testing.T) {
	for _, fw := range []target.Provider{target.LiveKit, target.Pipecat} {
		for _, role := range []target.Role{target.Listen, target.Speak} {
			for _, part := range []string{"us-east", "us-west", "br", "eu-west", "eu-north", "gb", "za", "il", "jp", "sg", "id", "in", "au"} {
				t.Run(string(fw)+"/"+string(role)+"/"+part, func(t *testing.T) {
					binding := ir.Binding{Provider: "slng", Model: "sarvam/saaras:v3", Language: "en-IN",
						Params: map[string]any{"world_part": part}}
					if role == target.Speak {
						binding.Model, binding.Voice = "sarvam/bulbul:v3", "shubh"
						binding.Params["warm_standby_enabled"] = true
					}
					original := maps.Clone(binding.Params)
					call, _, err := resolveService(fw, role, binding, newEnvSet(), slngSite{})
					if err != nil {
						t.Fatal(err)
					}
					// pipecat-slng 0.6.0 takes world_part itself; the LiveKit
					// plugin only knows a host.
					gateway, gone := `world_part="`+part+`"`, "base_url="
					if fw == target.LiveKit {
						gateway, gone = `slng_base_url="`+part+`.api.slng.ai"`, "world_part"
					}
					args := joinKVs(call.Args)
					for _, want := range []string{gateway, `model="` + binding.Model + `"`, `language="en-IN"`} {
						if !strings.Contains(args, want) {
							t.Errorf("missing %s in %s", want, args)
						}
					}
					if strings.Contains(args, gone) || strings.Count(args, gateway) != 1 {
						t.Errorf("want exactly one %s and no %s: %s", gateway, gone, args)
					}
					if role == target.Speak && (!strings.Contains(args, `voice="shubh"`) || !strings.Contains(args, "warm_standby_enabled=True")) {
						t.Errorf("lost speak params: %s", args)
					}
					if !reflect.DeepEqual(binding.Params, original) {
						t.Error("consuming the gateway mutated the source binding")
					}
				})
			}
		}
	}
}

// pipecat-slng 0.6.0 raises TypeError without world_part, so the generator
// refuses the package itself rather than emit a bot that crashes at import.
func TestSlngSpeechGatewayRequiredOnPipecat(t *testing.T) {
	binding := ir.Binding{Provider: "slng", Model: "deepgram/aura:2", Voice: "aura-2-thalia-en"}
	if _, _, err := resolveService(target.Pipecat, target.Speak, binding, newEnvSet(), slngSite{}); err == nil || !strings.Contains(err.Error(), "params.world_part is required") {
		t.Fatalf("pipecat SLNG speak without world_part: err %v", err)
	}
	if _, _, err := resolveService(target.LiveKit, target.Speak, binding, newEnvSet(), slngSite{}); err != nil {
		t.Fatalf("livekit keeps its plugin default: %v", err)
	}
}
