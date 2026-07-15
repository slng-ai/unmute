package ir

import (
	"reflect"
	"testing"
)

func TestLLMCatalogModels(t *testing.T) { // V4
	want := []string{
		"openai/gpt-4.1",
		"openai/gpt-4.1-mini",
		"openai/gpt-4o",
		"openai/gpt-4o-mini",
	}
	if got := LLMCatalogModels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("LLMCatalogModels() = %v, want %v", got, want)
	}
}

func TestTranslateLLMRoute(t *testing.T) {
	cases := []struct {
		name      string
		canonical string
		framework string
		want      string
	}{
		{"slng known", "openai/gpt-4.1", "slng", "openai/gpt-4.1:latest"},
		{"pipecat known", "openai/gpt-4.1", "pipecat", "gpt-4.1"},
		{"slng unknown adds :latest", "openai/gpt-future", "slng", "openai/gpt-future:latest"},
		{"slng unknown keeps existing tag", "openai/gpt-future:beta", "slng", "openai/gpt-future:beta"},
		{"pipecat unknown strips prefix+tag", "openai/gpt-future:beta", "pipecat", "gpt-future"},
		{"empty stays empty", "", "slng", ""},
		{"unknown framework passes through", "openai/gpt-4.1", "ferrari", "openai/gpt-4.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TranslateLLMRoute(c.canonical, c.framework)
			if got != c.want {
				t.Errorf("TranslateLLMRoute(%q, %q) = %q, want %q", c.canonical, c.framework, got, c.want)
			}
		})
	}
}

func TestTranslateLLMConfigTranslatesFallbacks(t *testing.T) {
	in := LLMModelConfig{
		Model:     "openai/gpt-4.1",
		Fallbacks: []string{"openai/gpt-4o-mini"},
	}
	got := TranslateLLMConfig(in, "pipecat")
	if got.Model != "gpt-4.1" {
		t.Errorf("Model: got %q, want %q", got.Model, "gpt-4.1")
	}
	if len(got.Fallbacks) != 1 || got.Fallbacks[0] != "gpt-4o-mini" {
		t.Errorf("Fallbacks: got %v, want [gpt-4o-mini]", got.Fallbacks)
	}
	// Input untouched.
	if in.Model != "openai/gpt-4.1" || in.Fallbacks[0] != "openai/gpt-4o-mini" {
		t.Errorf("input mutated: %+v", in)
	}
}
