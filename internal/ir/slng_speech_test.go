package ir

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/target"
)

func TestValidateSlngSpeechGateway(t *testing.T) {
	for _, provider := range []Provider{ProviderLiveKit, ProviderPipecat} {
		for _, site := range []string{"listen", "speak", "fallback"} {
			for _, tc := range []struct {
				name   string
				params map[string]any
				want   string
			}{
				{"unset", nil, ""},
				{"gateway", map[string]any{"world_part_override": "in"}, ""},
				{"retired region", map[string]any{"region_override": "ap-south-1"}, "region_override is no longer supported"},
				{"retired region with gateway", map[string]any{"world_part_override": "in", "region_override": "ap-south-1"}, "region_override is no longer supported"},
				{"retired null region", map[string]any{"region_override": nil}, "region_override is no longer supported"},
				{"empty", map[string]any{"world_part_override": ""}, "choose one of"},
				{"null", map[string]any{"world_part_override": nil}, "choose one of"},
				{"number", map[string]any{"world_part_override": 1}, "choose one of"},
				{"list", map[string]any{"world_part_override": []string{"in"}}, "choose one of"},
				{"unknown", map[string]any{"world_part_override": "atlantis"}, "choose one of"},
				{"legacy", map[string]any{"world_part_override": "eu"}, "replace legacy"},
				{"explicit URL", map[string]any{"world_part_override": "in", "base_url": "custom.test"}, "remove params.base_url"},
				{"explicit LiveKit URL", map[string]any{"world_part_override": "in", "slng_base_url": "custom.test"}, "remove params.slng_base_url"},
			} {
				t.Run(string(provider)+"/"+site+"/"+tc.name, func(t *testing.T) {
					if site == "fallback" && provider == ProviderPipecat {
						t.Skip("Pipecat has no listen fallback lowering")
					}
					agent := safeAgent(t)
					tgt := targetFor(agent, provider)
					binding := Binding{Provider: "slng", Model: "sarvam/saaras:v3", Placement: PlacementAPI, Params: tc.params}
					switch site {
					case "listen":
						tgt.Models.Listen = &binding
					case "speak":
						binding.Model, binding.Voice = "sarvam/bulbul:v3", "shubh"
						tgt.Models.Speak["front_desk"] = binding
					case "fallback":
						tgt.Models.ListenFallbacks = []ListenFallback{{Name: "backup", Binding: binding}}
					}
					report, err := Validate(agent, []Target{tgt}, target.Default())
					got := strings.Join(report.PerTarget[0].Errors, "\n")
					if tc.want == "" {
						if err != nil {
							t.Fatal(got)
						}
					} else if err == nil || !strings.Contains(got, tc.want) || !strings.Contains(got, "world_part_override") || !strings.Contains(got, binding.Model) {
						t.Fatalf("want %q and model name in gateway refusal; got %s", tc.want, got)
					}
				})
			}
		}
	}
}
