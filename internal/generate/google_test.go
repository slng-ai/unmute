package generate

import (
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

func TestGoogleUsesNativePluginsAndPinsVertexToEU(t *testing.T) {
	for _, fw := range []target.Provider{target.LiveKit, target.Pipecat} {
		for _, vendor := range []string{"google", "gemini"} {
			t.Run(string(fw)+"/"+vendor, func(t *testing.T) {
				binding := ir.Binding{Provider: vendor, Model: "gemini-3.5-flash-lite"}
				call, entry, err := resolveService(fw, target.Reason, binding, newEnvSet(), slngSite{})
				if err != nil {
					t.Fatal(err)
				}
				if entry.Vendor != "google" || entry.Install.Extra != "google" || strings.Contains(call.Class, "inference") {
					t.Fatalf("Gemini did not resolve to the native plugin: %+v", entry)
				}
				binding.Params = map[string]any{"vertexai": true, "location": "eu", "thinking_config": map[string]any{"thinking_level": "minimal"}}
				call, _, err = resolveService(fw, target.Reason, binding, newEnvSet(), slngSite{})
				if err != nil {
					t.Fatal(err)
				}
				if call.Class != "_GoogleVertexLLM" || strings.Contains(joinKVs(call.SettingsArgs), "location=") {
					t.Fatalf("wrong Vertex constructor: %+v", call)
				}
				agent := loadCompilerAgent(t)
				tgt := targetByProvider(t, agent, ir.Provider(fw))
				for name := range tgt.Models.Reason {
					tgt.Models.Reason[name] = binding
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
				for _, want := range []string{"https://aiplatform.eu.rep.googleapis.com", "gemini-3.5-flash-lite", "GOOGLE_API_KEY", "minimal"} {
					if !strings.Contains(src, want) {
						t.Errorf("missing %s", want)
					}
				}
				if strings.Contains(src, "inference.LLM(") || strings.Contains(src, "https://aiplatform.googleapis.com") {
					t.Fatal("Gemini can escape EU routing")
				}
				for _, params := range []map[string]any{
					{"location": "eu"}, {"vertexai": true}, {"vertexai": true, "location": "global"},
					{"vertexai": true, "location": "eu", "http_options": map[string]any{"base_url": "https://example.com"}},
				} {
					binding.Params = params
					if _, _, err := resolveService(fw, target.Reason, binding, newEnvSet(), slngSite{}); err == nil {
						t.Errorf("accepted ambiguous or overridden Vertex routing: %v", params)
					}
				}
			})
		}
	}
}
