package ir

import (
	"reflect"
	"testing"
)

func TestToVoiceAgentsModels(t *testing.T) { // V17, V18, V19
	sttSampleRate := 16_000
	enableVAD := true
	enablePartials := false
	finalTimeout := 1.2
	llmFirstToken := 4.5
	ttsSampleRate := 24_000
	ttsSpeed := 1.05
	ttsFirstAudio := 2.1
	failureAudio := true

	got := ToVoiceAgentsModels(
		STTModelConfig{
			Model: "slng/deepgram/nova:3-en",
			Config: &STTBridgeConfig{
				Language:       "en",
				SampleRate:     &sttSampleRate,
				Encoding:       "linear16",
				EnableVAD:      &enableVAD,
				EnablePartials: &enablePartials,
			},
			Fallbacks:     []string{"deepgram/nova-2:en"},
			FinalTimeoutS: &finalTimeout,
		},
		LLMModelConfig{
			Model:              "openai/gpt-4.1",
			Config:             map[string]any{"temperature": 0.2},
			Fallbacks:          []string{"openai/gpt-4o-mini"},
			FirstTokenTimeoutS: &llmFirstToken,
		},
		TTSModelConfig{
			Model: "slng/rime/arcana:3-en",
			Voice: "luna",
			Config: &TTSBridgeConfig{
				SampleRate: &ttsSampleRate,
				Encoding:   "pcm",
				Language:   "en",
				Speed:      &ttsSpeed,
			},
			Fallbacks: []TTSFallback{
				{Model: "rime/arcana:3-en", Voice: "luna"},
			},
			FirstAudioTimeoutS:  &ttsFirstAudio,
			FailureAudioEnabled: &failureAudio,
		},
	)

	want := VoiceAgentsModels{
		STT: "slng/deepgram/nova:3-en",
		STTKwargs: &STTBridgeConfig{
			Language:       "en",
			SampleRate:     &sttSampleRate,
			Encoding:       "linear16",
			EnableVAD:      &enableVAD,
			EnablePartials: &enablePartials,
		},
		STTFinalTimeoutS:      &finalTimeout,
		LLM:                   "openai/gpt-4.1",
		LLMKwargs:             map[string]any{"temperature": 0.2},
		LLMFirstTokenTimeoutS: &llmFirstToken,
		TTS:                   "slng/rime/arcana:3-en",
		TTSVoice:              "luna",
		TTSKwargs: &TTSBridgeConfig{
			SampleRate: &ttsSampleRate,
			Encoding:   "pcm",
			Language:   "en",
			Speed:      &ttsSpeed,
		},
		TTSFirstAudioTimeoutS: &ttsFirstAudio,
		FailureAudioEnabled:   &failureAudio,
		Fallbacks: &VoiceAgentsModelFallbacks{
			STT: []string{"deepgram/nova-2:en"},
			LLM: []string{"openai/gpt-4o-mini"},
			TTS: []TTSFallback{{Model: "rime/arcana:3-en", Voice: "luna"}},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToVoiceAgentsModels() mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestModelRoutesPassThroughUnvalidated(t *testing.T) { // V14
	got := ToVoiceAgentsModels(
		STTModelConfig{Model: "future-stt/new-model:next"},
		LLMModelConfig{Model: "future-llm/new-model"},
		TTSModelConfig{Model: "future-tts/new-model:next", Voice: "new-voice"},
	)

	if got.STT != "future-stt/new-model:next" {
		t.Errorf("STT route was changed or rejected: %q", got.STT)
	}
	if got.LLM != "future-llm/new-model" {
		t.Errorf("LLM route was changed or rejected: %q", got.LLM)
	}
	if got.TTS != "future-tts/new-model:next" || got.TTSVoice != "new-voice" {
		t.Errorf("TTS route/voice was changed or rejected: %q %q", got.TTS, got.TTSVoice)
	}
}
