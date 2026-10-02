package generate

import (
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
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

func TestSlngSpeechGatewayKeepsDeployment(t *testing.T) {
	for _, provider := range []ir.Provider{ir.ProviderLiveKit, ir.ProviderPipecat} {
		t.Run(string(provider), func(t *testing.T) {
			agent := loadCompilerAgent(t)
			tgt := targetByProvider(t, agent, provider)
			tgt.DeploymentRegions = []string{"eu-central"}
			first := map[string]any{"world_part": "eu-north"}
			tgt.Models.Listen = &ir.Binding{Provider: "slng", Model: "sarvam/saaras:v3", Placement: ir.PlacementAPI, Params: first}
			for name := range tgt.Models.Speak {
				tgt.Models.Speak[name] = ir.Binding{Provider: "slng", Model: "sarvam/bulbul:v3", Voice: "shubh", Placement: ir.PlacementAPI, Params: first}
			}
			before, err := Generate(agent, tgt, target.Default())
			if err != nil {
				t.Fatal(err)
			}
			tgt.Models.Listen.Params = map[string]any{"world_part": "in"}
			for name, binding := range tgt.Models.Speak {
				binding.Params = map[string]any{"world_part": "au"}
				tgt.Models.Speak[name] = binding
			}
			after, err := Generate(agent, tgt, target.Default())
			if err != nil {
				t.Fatal(err)
			}
			file, gateway, deploy := agentSource, `world_part="%s"`, "pcc-deploy.toml"
			if provider == ir.ProviderLiveKit {
				file, gateway, deploy = agentSource, `slng_base_url="%s.api.slng.ai"`, "README.md"
			}
			src := artifactFile(t, after, file)
			for _, part := range []string{"in", "au"} {
				if !strings.Contains(src, fmt.Sprintf(gateway, part)) {
					t.Errorf("%s missing %s gateway", file, part)
				}
			}
			if got, want := artifactFile(t, after, deploy), artifactFile(t, before, deploy); got != want {
				t.Errorf("speech gateway changed %s", deploy)
			}
			if provider == ir.ProviderLiveKit {
				tgt.Models.ListenFallbacks = []ir.ListenFallback{{Name: "backup", Binding: ir.Binding{
					Provider: "slng", Model: "sarvam/saaras:v3", Placement: ir.PlacementAPI,
					Params: map[string]any{"world_part": "sg"},
				}}}
				fallback, err := Generate(agent, tgt, target.Default())
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(artifactFile(t, fallback, file), `slng_base_url="sg.api.slng.ai"`) {
					t.Error("fallback did not use its own gateway")
				}
			}
		})
	}
}

func TestSlngSpeechGatewayUsesTargetOverride(t *testing.T) {
	pkg, err := spec.Load(filepath.Join("..", "testdata", "safe_core"))
	if err != nil {
		t.Fatal(err)
	}
	model := pkg.Agent.Models.Listen["transcriber"]
	model.Provider, model.Model = "slng", "sarvam/saaras:v3"
	model.Params = map[string]any{"world_part": "in"}
	pkg.Agent.Models.Listen["transcriber"] = model
	override := pkg.Targets["livekit"]
	model.Params = map[string]any{"world_part": "jp"}
	override.Models["transcriber"] = model
	pkg.Targets["livekit"] = override
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		provider ir.Provider
		file     string
		want     string
	}{
		{ir.ProviderLiveKit, agentSource, `slng_base_url="jp.api.slng.ai"`},
		{ir.ProviderPipecat, agentSource, `world_part="in"`},
	} {
		artifact, err := Generate(agent, targetByProvider(t, agent, tc.provider), target.Default())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(artifactFile(t, artifact, tc.file), tc.want) {
			t.Errorf("%s did not use its resolved model gateway %s", tc.provider, tc.want)
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
