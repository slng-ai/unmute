package ir

// STTModelConfig is the shape of agent/models/stt.yaml.
//
// Config params are intentionally typed and finite: local authoring does not
// validate live catalog membership, but it also does not promise arbitrary
// passthrough keys.
type STTModelConfig struct {
	Model         string           `json:"model" yaml:"model"`
	Config        *STTBridgeConfig `json:"config,omitempty" yaml:"config,omitempty"`
	Fallbacks     []string         `json:"fallbacks,omitempty" yaml:"fallbacks,omitempty"`
	FinalTimeoutS *float64         `json:"final_timeout_s,omitempty" yaml:"final_timeout_s,omitempty"`
}

// LLMModelConfig is the shape of agent/models/llm.yaml.
type LLMModelConfig struct {
	Model              string         `json:"model" yaml:"model"`
	Config             map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	Fallbacks          []string       `json:"fallbacks,omitempty" yaml:"fallbacks,omitempty"`
	FirstTokenTimeoutS *float64       `json:"first_token_timeout_s,omitempty" yaml:"first_token_timeout_s,omitempty"`
}

// TTSModelConfig is the shape of agent/models/tts.yaml.
type TTSModelConfig struct {
	Model               string           `json:"model" yaml:"model"`
	Voice               string           `json:"voice" yaml:"voice"`
	Config              *TTSBridgeConfig `json:"config,omitempty" yaml:"config,omitempty"`
	Fallbacks           []TTSFallback    `json:"fallbacks,omitempty" yaml:"fallbacks,omitempty"`
	FirstAudioTimeoutS  *float64         `json:"first_audio_timeout_s,omitempty" yaml:"first_audio_timeout_s,omitempty"`
	FailureAudioEnabled *bool            `json:"failure_audio_enabled,omitempty" yaml:"failure_audio_enabled,omitempty"`
}

// STTBridgeConfig maps STT bridge-specific fields into models.stt_kwargs.
type STTBridgeConfig struct {
	Language       string `json:"language,omitempty" yaml:"language,omitempty"`
	SampleRate     *int   `json:"sample_rate,omitempty" yaml:"sample_rate,omitempty"`
	Encoding       string `json:"encoding,omitempty" yaml:"encoding,omitempty"`
	EnableVAD      *bool  `json:"enable_vad,omitempty" yaml:"enable_vad,omitempty"`
	EnablePartials *bool  `json:"enable_partials,omitempty" yaml:"enable_partials,omitempty"`
}

// TTSBridgeConfig maps TTS bridge-specific fields into models.tts_kwargs.
type TTSBridgeConfig struct {
	SampleRate *int     `json:"sample_rate,omitempty" yaml:"sample_rate,omitempty"`
	Encoding   string   `json:"encoding,omitempty" yaml:"encoding,omitempty"`
	Language   string   `json:"language,omitempty" yaml:"language,omitempty"`
	Speed      *float64 `json:"speed,omitempty" yaml:"speed,omitempty"`
}

// TTSFallback is one TTS fallback route. TTS fallbacks need a voice as well as
// a model route because the Voice Agents API keeps those fields separate.
type TTSFallback struct {
	Model string `json:"model" yaml:"model"`
	Voice string `json:"voice" yaml:"voice"`
}

// VoiceAgentsModels is the Voice Agents API models object produced from the
// split model files. Field names match the API contract, not the authoring
// filenames.
type VoiceAgentsModels struct {
	STT                   string                     `json:"stt,omitempty" yaml:"stt,omitempty"`
	STTKwargs             *STTBridgeConfig           `json:"stt_kwargs,omitempty" yaml:"stt_kwargs,omitempty"`
	STTFinalTimeoutS      *float64                   `json:"stt_final_timeout_s,omitempty" yaml:"stt_final_timeout_s,omitempty"`
	LLM                   string                     `json:"llm,omitempty" yaml:"llm,omitempty"`
	LLMKwargs             map[string]any             `json:"llm_kwargs,omitempty" yaml:"llm_kwargs,omitempty"`
	LLMFirstTokenTimeoutS *float64                   `json:"llm_first_token_timeout_s,omitempty" yaml:"llm_first_token_timeout_s,omitempty"`
	TTS                   string                     `json:"tts,omitempty" yaml:"tts,omitempty"`
	TTSVoice              string                     `json:"tts_voice,omitempty" yaml:"tts_voice,omitempty"`
	TTSKwargs             *TTSBridgeConfig           `json:"tts_kwargs,omitempty" yaml:"tts_kwargs,omitempty"`
	TTSFirstAudioTimeoutS *float64                   `json:"tts_first_audio_timeout_s,omitempty" yaml:"tts_first_audio_timeout_s,omitempty"`
	FailureAudioEnabled   *bool                      `json:"failure_audio_enabled,omitempty" yaml:"failure_audio_enabled,omitempty"`
	Fallbacks             *VoiceAgentsModelFallbacks `json:"fallbacks,omitempty" yaml:"fallbacks,omitempty"`
}

// VoiceAgentsModelFallbacks groups fallback routes by modality.
type VoiceAgentsModelFallbacks struct {
	STT []string      `json:"stt,omitempty" yaml:"stt,omitempty"`
	LLM []string      `json:"llm,omitempty" yaml:"llm,omitempty"`
	TTS []TTSFallback `json:"tts,omitempty" yaml:"tts,omitempty"`
}

// ToVoiceAgentsModels maps split model config files to the Voice Agents API
// models object. It copies routes verbatim; live catalog lookup belongs to
// resolve/deploy, not local IR construction.
func ToVoiceAgentsModels(stt STTModelConfig, llm LLMModelConfig, tts TTSModelConfig) VoiceAgentsModels {
	var out VoiceAgentsModels
	stt.ApplyToVoiceAgentsModels(&out)
	llm.ApplyToVoiceAgentsModels(&out)
	tts.ApplyToVoiceAgentsModels(&out)
	return out
}

// ApplyToVoiceAgentsModels maps STT authoring fields into models.stt*.
func (c STTModelConfig) ApplyToVoiceAgentsModels(out *VoiceAgentsModels) {
	out.STT = c.Model
	if c.Config != nil && !c.Config.empty() {
		config := *c.Config
		out.STTKwargs = &config
	}
	out.STTFinalTimeoutS = c.FinalTimeoutS
	if len(c.Fallbacks) > 0 {
		out.ensureFallbacks().STT = append([]string(nil), c.Fallbacks...)
	}
}

// ApplyToVoiceAgentsModels maps LLM authoring fields into models.llm*.
func (c LLMModelConfig) ApplyToVoiceAgentsModels(out *VoiceAgentsModels) {
	out.LLM = c.Model
	if len(c.Config) > 0 {
		out.LLMKwargs = c.Config
	}
	out.LLMFirstTokenTimeoutS = c.FirstTokenTimeoutS
	if len(c.Fallbacks) > 0 {
		out.ensureFallbacks().LLM = append([]string(nil), c.Fallbacks...)
	}
}

// ApplyToVoiceAgentsModels maps TTS authoring fields into models.tts*.
func (c TTSModelConfig) ApplyToVoiceAgentsModels(out *VoiceAgentsModels) {
	out.TTS = c.Model
	out.TTSVoice = c.Voice
	if c.Config != nil && !c.Config.empty() {
		config := *c.Config
		out.TTSKwargs = &config
	}
	out.TTSFirstAudioTimeoutS = c.FirstAudioTimeoutS
	out.FailureAudioEnabled = c.FailureAudioEnabled
	if len(c.Fallbacks) > 0 {
		out.ensureFallbacks().TTS = append([]TTSFallback(nil), c.Fallbacks...)
	}
}

func (m *VoiceAgentsModels) ensureFallbacks() *VoiceAgentsModelFallbacks {
	if m.Fallbacks == nil {
		m.Fallbacks = &VoiceAgentsModelFallbacks{}
	}
	return m.Fallbacks
}

func (c STTBridgeConfig) empty() bool {
	return c.Language == "" &&
		c.SampleRate == nil &&
		c.Encoding == "" &&
		c.EnableVAD == nil &&
		c.EnablePartials == nil
}

func (c TTSBridgeConfig) empty() bool {
	return c.SampleRate == nil &&
		c.Encoding == "" &&
		c.Language == "" &&
		c.Speed == nil
}
