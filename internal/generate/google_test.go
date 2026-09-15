package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

func TestGoogleUsesNativePluginsAndForwardsLocation(t *testing.T) {
	for _, fw := range []target.Provider{target.LiveKit, target.Pipecat} {
		for _, vendor := range []string{"google", "gemini"} {
			for _, location := range []string{"", "us", "eu", "global", "us-central1", "europe-west4", "unsupported-location"} {
				t.Run(string(fw)+"/"+vendor+"/"+location, func(t *testing.T) {
					binding := ir.Binding{Provider: vendor, Model: "gemini-3.5-flash-lite", Params: map[string]any{
						"thinking_config": map[string]any{"thinking_level": "minimal"}, "temperature": 0.2,
					}}
					if location != "" {
						binding.Params["vertexai"] = true
						binding.Params["location"] = location
					}
					call, entry, err := resolveService(fw, target.Reason, binding, newEnvSet(), slngSite{})
					if err != nil {
						t.Fatal(err)
					}
					if entry.Vendor != "google" || entry.Install.Extra != "google" {
						t.Fatalf("Gemini did not resolve to the native plugin: %+v", entry)
					}
					if location == "" {
						if call.Class != entry.Call.Class {
							t.Fatalf("Developer API constructor changed: %+v", call)
						}
					} else if call.Class != "_GoogleVertexLLM" || !strings.Contains(joinKVs(call.Args), "location="+pyQuote(location)) {
						t.Fatalf("missing Vertex constructor location: %+v", call)
					}
					if strings.Contains(joinKVs(call.SettingsArgs), "location=") {
						t.Fatalf("location became a generation setting: %+v", call)
					}
					agent := loadCompilerAgent(t)
					tgt := targetByProvider(t, agent, ir.Provider(fw))
					for name := range tgt.Models.Reason {
						tgt.Models.Reason[name] = binding
					}
					report, err := ir.Validate(agent, []ir.Target{tgt}, target.Default())
					if err != nil {
						t.Fatalf("%v: %+v", err, report)
					}
					artifact, err := Generate(agent, tgt, target.Default())
					if err != nil {
						t.Fatal(err)
					}
					file := "agent.py"
					if fw == target.Pipecat {
						file = "bot.py"
					}
					src := artifactFile(t, artifact, file)
					for _, want := range []string{"gemini-3.5-flash-lite", "GOOGLE_API_KEY", "minimal", "temperature=0.2"} {
						if !strings.Contains(src, want) {
							t.Errorf("missing %s", want)
						}
					}
					if strings.Contains(src, "inference.LLM(") {
						t.Fatal("native Gemini became LiveKit Inference")
					}
					if location == "" && strings.Contains(src, "_google_vertex_client") {
						t.Fatal("Developer API emits the Vertex adapter")
					}
					if location != "" && binding.Params["location"] != location {
						t.Fatal("generation mutated the authored params")
					}
				})
			}
		}
	}
}

func TestGoogleRefusesAmbiguousOrOverriddenRouting(t *testing.T) {
	for _, fw := range []target.Provider{target.LiveKit, target.Pipecat} {
		for _, params := range []map[string]any{
			{"location": "eu"}, {"vertexai": false}, {"vertexai": "true"}, {"vertexai": true},
			{"vertexai": true, "location": ""}, {"vertexai": true, "location": 42},
			{"vertexai": true, "location": []string{"us"}},
			{"vertexai": true, "location": "US"}, {"vertexai": true, "location": " us "},
			{"vertexai": true, "location": "us/other"}, {"vertexai": true, "location": "us.example.com"},
			{"vertexai": true, "location": "-us"}, {"vertexai": true, "location": "us-"},
			{"vertexai": true, "location": strings.Repeat("a", 64)},
			{"vertexai": true, "location": "us", "http_options": map[string]any{"base_url": "https://example.com"}},
			{"vertexai": true, "location": "eu", "project": "other"},
			{"vertexai": true, "location": "us", "credentials": "other"},
		} {
			binding := ir.Binding{Provider: "google", Model: "gemini-3.5-flash-lite", Params: params}
			if _, _, err := resolveService(fw, target.Reason, binding, newEnvSet(), slngSite{}); err == nil {
				t.Errorf("%s accepted ambiguous or overridden routing: %v", fw, params)
			}
			agent := loadCompilerAgent(t)
			tgt := targetByProvider(t, agent, ir.Provider(fw))
			for name := range tgt.Models.Reason {
				tgt.Models.Reason[name] = binding
			}
			if report, err := ir.Validate(agent, []ir.Target{tgt}, target.Default()); err == nil || !strings.Contains(strings.Join(report.PerTarget[0].Errors, " "), "google") {
				t.Errorf("%s validation did not name Google's bad routing: %+v", fw, report)
			}
		}
	}
}
