package generate

import (
	"fmt"
	"testing"

	"github.com/slng/unmute/internal/ir"
	targetcap "github.com/slng/unmute/internal/target"
)

// The generation-slot smoke contract (N20, V36). The catalogue declares ~59
// kwarg names for the four typed generation params, hand-read off vendor docs
// and plugin sources. A wrong name passes validate, passes compile and passes
// ruff, then raises TypeError on the first live call — so the only real check
// is constructing the class in a venv, which is what the L4 smoke tests in
// generation_slots_smoke_test.go do with the probe lists below.
//
// Each probe becomes one throwaway agent in the smoke package, so a single venv
// instantiates many entries at once instead of one venv per entry. Its params
// are not written here: they come from the entry's own ParamSlots(), so adding
// a slot to an already-probed entry puts it under smoke with no test edit.
//
// This file carries no build tag on purpose: TestGenerationSlotSmokeCoverage
// runs in the PR gate, and fails when a new slot-bearing entry has neither a
// probe nor an exemption. Smoke is opt-in, so without that guard the lists
// would rot silently.

// generationProbe binds one speak entry and one reason entry. Either half may
// be zero, in which case the probe agent keeps the entry agent's own binding
// for that role (roles with no slots, listen and turn, are never probed).
type generationProbe struct {
	speak  ir.Binding
	reason ir.Binding
}

// generationProbes lists what each framework's smoke run must construct.
// Model, voice and endpoint identities are never validated (D10), so these are
// plausible strings rather than a claim that the id exists.
var generationProbes = map[targetcap.Provider][]generationProbe{
	targetcap.Pipecat: {
		{
			speak:  ir.Binding{Provider: "rime", Model: "mistv2", Voice: "cove"},
			reason: ir.Binding{Provider: "anthropic", Model: "claude-sonnet-4-6"},
		},
		{
			speak:  ir.Binding{Provider: "sarvam", Model: "bulbul:v2", Voice: "anushka"},
			reason: ir.Binding{Provider: "openai", Model: "gpt-4o-mini"},
		},
		{
			speak:  ir.Binding{Provider: "inworld", Model: "inworld-tts-2", Voice: "Ashley"},
			reason: ir.Binding{Provider: "google", Model: "gemini-2.0-flash"},
		},
		{
			speak:  ir.Binding{Provider: "slng", Model: "slng/deepgram/aura:2-en", Voice: "aura-2-thalia-en"},
			reason: ir.Binding{Provider: "groq", Model: "llama-3.3-70b-versatile"},
		},
		{
			speak:  ir.Binding{Provider: "soniox", Model: "soniox-tts-v1", Voice: "sara"},
			reason: ir.Binding{Provider: "mistral", Model: "mistral-small-latest"},
		},
		{
			speak:  ir.Binding{Provider: "elevenlabs", Model: "eleven_multilingual_v2", Voice: "21m00Tcm4TlvDq8ikWAM"},
			reason: ir.Binding{Provider: "deepseek", Model: "deepseek-chat"},
		},
		{
			speak:  ir.Binding{Provider: "openai", Model: "gpt-4o-mini-tts", Voice: "alloy"},
			reason: ir.Binding{Provider: "openrouter", Model: "openai/gpt-4o-mini"},
		},
		{reason: ir.Binding{Provider: "qwen", Model: "qwen-plus"}},
	},
	targetcap.LiveKit: {
		{
			speak:  ir.Binding{Provider: "rime", Model: "mistv2", Voice: "cove"},
			reason: ir.Binding{Provider: "anthropic", Model: "claude-sonnet-4-6"},
		},
		{
			speak:  ir.Binding{Provider: "sarvam", Model: "bulbul:v2", Voice: "anushka"},
			reason: ir.Binding{Provider: "openai", Model: "gpt-4o-mini"},
		},
		{
			speak:  ir.Binding{Provider: "inworld", Model: "inworld-tts-2", Voice: "Ashley"},
			reason: ir.Binding{Provider: "groq", Model: "llama-3.3-70b-versatile"},
		},
		{
			speak:  ir.Binding{Provider: "slng", Model: "slng/deepgram/aura:2-en", Voice: "aura-2-thalia-en"},
			reason: ir.Binding{Provider: "mistralai", Model: "mistral-small-latest"},
		},
		{
			speak:  ir.Binding{Provider: "cartesia", Model: "sonic-3", Voice: "f786b574-daa5-4673-aa0c-cbe3e8534c02"},
			reason: ir.Binding{Provider: "openrouter", Model: "openai/gpt-4o-mini"},
		},
		{
			speak:  ir.Binding{Provider: "soniox", Model: "soniox-tts-v1", Voice: "sara"},
			reason: ir.Binding{Provider: "aws", Model: "anthropic.claude-3-5-sonnet-20241022-v2:0"},
		},
		{reason: ir.Binding{Provider: "sarvam", Model: "sarvam-m"}},
		// provider "livekit" is the deliberate Inference spelling (V19), so this
		// probe resolves through the reason wildcard row.
		{reason: ir.Binding{Provider: "livekit", Model: "openai/gpt-4o-mini"}},
		{reason: ir.Binding{
			Provider:    "azure",
			Model:       "gpt-4o",
			EndpointEnv: "AZURE_OPENAI_ENDPOINT",
			// with_azure refuses to construct without an api version; it is the
			// author's own params key, forwarded verbatim and unrenamed (D2).
			Params: map[string]any{"api_version": "2024-10-21"},
		}},
	},
}

// generationProbeValues is the value each generation param is probed with, in
// every vendor's accepted range so a rejection means a wrong kwarg name and not
// a wrong number. top_k is an int because the plugins type it as one.
var generationProbeValues = map[string]any{
	"speed":       1.1,
	"temperature": 0.4,
	"top_p":       0.8,
	"top_k":       40,
}

// generationSlotSmokeExempt names slot-bearing entries no probe reaches, and
// why. Keyed like slotKey; keep the reason specific, it is the whole point.
var generationSlotSmokeExempt = map[string]string{
	slotKey(targetcap.Pipecat, targetcap.Speak, "*"):  "same OpenAITTSService class as the probed pipecat openai speak row, reached through base_url",
	slotKey(targetcap.Pipecat, targetcap.Reason, "*"): "same OpenAILLMService class as the probed pipecat openai reason row, reached through base_url",
}

func slotKey(framework targetcap.Provider, role targetcap.Role, vendor string) string {
	return fmt.Sprintf("%s %s %s", framework, role, vendor)
}

// TestGenerationSlotSmokeCoverage keeps the probe lists honest: every catalogue
// entry that declares a generation slot is either constructed by an L4 smoke
// run or carries a written reason why it cannot be. Adding a slot to a row
// nobody probes fails here, in the PR gate, rather than in a user's first call.
func TestGenerationSlotSmokeCoverage(t *testing.T) {
	for _, name := range targetcap.GenerationParams {
		if _, ok := generationProbeValues[name]; !ok {
			t.Errorf("generation param %q has no probe value", name)
		}
	}

	covered := map[string]bool{}
	for framework, probes := range generationProbes {
		for i, probe := range probes {
			for role, binding := range map[targetcap.Role]ir.Binding{
				targetcap.Speak: probe.speak, targetcap.Reason: probe.reason,
			} {
				if binding.Provider == "" {
					continue
				}
				entry, ok := defaultCatalog.Lookup(framework, role, binding.Provider)
				if !ok {
					t.Errorf("%s probe %d: %s provider %q is not catalogued", framework, i, role, binding.Provider)
					continue
				}
				if len(entry.Call.ParamSlots()) == 0 {
					t.Errorf("%s probe %d: %s provider %q declares no generation slot, so probing it proves nothing",
						framework, i, role, binding.Provider)
					continue
				}
				covered[slotKey(framework, role, entry.Vendor)] = true
			}
		}
	}

	for _, entry := range defaultCatalog.Entries() {
		key := slotKey(entry.Framework, entry.Role, entry.Vendor)
		if len(entry.Call.ParamSlots()) == 0 {
			if reason, ok := generationSlotSmokeExempt[key]; ok {
				t.Errorf("%s is exempted (%q) but declares no generation slot; drop the exemption", key, reason)
			}
			continue
		}
		if covered[key] || generationSlotSmokeExempt[key] != "" {
			continue
		}
		t.Errorf("%s declares generation slots %v that no smoke probe constructs; "+
			"add it to generationProbes, or to generationSlotSmokeExempt with a reason",
			key, entry.Call.ParamSlots())
	}
	for key, reason := range generationSlotSmokeExempt {
		if covered[key] {
			t.Errorf("%s is both probed and exempted (%q); drop the exemption", key, reason)
		}
	}
}
