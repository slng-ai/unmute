package ir

import (
	"maps"
	"slices"
	"strings"
)

// LLM route translation table. Authoring uses a canonical `provider/model`
// name (no tag); each framework gets the native string it expects.
//
// To add a model: append a row. To add a framework: append a column and
// extend translateUnknownLLM with its heuristic.
//
// Canonical name: lowercase, `provider/model`, no `:tag`, no `:latest`.
var llmCatalog = map[string]map[string]string{
	"openai/gpt-4.1": {
		"slng":    "openai/gpt-4.1:latest",
		"pipecat": "gpt-4.1",
	},
	"openai/gpt-4.1-mini": {
		"slng":    "openai/gpt-4.1-mini:latest",
		"pipecat": "gpt-4.1-mini",
	},
	"openai/gpt-4o": {
		"slng":    "openai/gpt-4o:latest",
		"pipecat": "gpt-4o",
	},
	"openai/gpt-4o-mini": {
		"slng":    "openai/gpt-4o-mini:latest",
		"pipecat": "gpt-4o-mini",
	},
}

// LLMCatalogModels returns the canonical model names in lexical order.
func LLMCatalogModels() []string {
	return slices.Sorted(maps.Keys(llmCatalog))
}

// TranslateLLMRoute maps a canonical model name to its framework-native form.
// Unknown models fall through to a per-framework heuristic so authoring isn't
// blocked by an out-of-date catalog. Empty string in → empty string out.
func TranslateLLMRoute(canonical, framework string) string {
	if canonical == "" {
		return ""
	}
	if entry, ok := llmCatalog[canonical]; ok {
		if mapped, ok := entry[framework]; ok {
			return mapped
		}
	}
	return translateUnknownLLM(canonical, framework)
}

// TranslateLLMConfig returns a copy of cfg with Model and Fallbacks translated
// to the framework's native names.
func TranslateLLMConfig(cfg LLMModelConfig, framework string) LLMModelConfig {
	out := cfg
	out.Model = TranslateLLMRoute(cfg.Model, framework)
	if len(cfg.Fallbacks) > 0 {
		out.Fallbacks = make([]string, len(cfg.Fallbacks))
		for i, fb := range cfg.Fallbacks {
			out.Fallbacks[i] = TranslateLLMRoute(fb, framework)
		}
	}
	return out
}

func translateUnknownLLM(canonical, framework string) string {
	switch framework {
	case "slng":
		// slng requires a `:tag` — append `:latest` if author didn't pick one.
		if strings.Contains(canonical, ":") {
			return canonical
		}
		return canonical + ":latest"
	case "pipecat":
		// pipecat takes the bare model name. Strip provider prefix and any tag.
		bare := canonical
		if i := strings.Index(bare, "/"); i >= 0 {
			bare = bare[i+1:]
		}
		if i := strings.Index(bare, ":"); i >= 0 {
			bare = bare[:i]
		}
		return bare
	default:
		return canonical
	}
}
