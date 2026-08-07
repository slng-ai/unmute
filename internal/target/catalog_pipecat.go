package target

// Pipecat entries. Contract: the pipecat_v1 driver templates, pipecat-ai
// >=1.5.0 <2.0. Official services ship as pipecat-ai extras and take model/
// voice/params nested in Class.Settings(...) (flat forms deprecated since
// v0.0.105; verified against the per-service docs 2026-07-15). The SLNG
// plugin is a standalone package with flat kwargs (verified against
// github.com/slng-ai/pipecat-slng source 2026-07-15).
//
// Generation-param slots (Temperature/TopP/TopK/Speed) verified 2026-08-07
// against each service's documented Settings table in the pipecat-ai/docs
// source, and the slng rows against the pipecat-slng package source. A missing
// slot is a checked fact, not an omission: see the note on each such entry.
//
// Every LLM service here takes all three sampling params, because temperature,
// top_p and top_k are fields of the shared LLMSettings base class. Worth knowing
// that on the OpenAI-compatible services top_k is accepted and then ignored (the
// pipecat source says the OpenAI client library has no such argument). A slot
// declares the kwarg exists, never that the provider acts on it — SCHEMA.md 6.2
// rule 6: some providers keep fields that do nothing, so run the agent to be sure.

const pipecatServicesDocs = "https://docs.pipecat.ai/api-reference/server/services/supported-services"

var pipecatCatalog = []Entry{
	// --- listen ---------------------------------------------------------
	{
		Framework: Pipecat, Role: Listen, Vendor: "deepgram",
		Verified: "2026-07-15", Docs: "https://docs.pipecat.ai/api-reference/server/services/stt/deepgram",
		Install: InstallSpec{Extra: "deepgram"},
		Import:  "from pipecat.services.deepgram.stt import DeepgramSTTService",
		Call: &CallSpec{
			Class: "DeepgramSTTService", APIKeyArg: "api_key", APIKeyEnv: "DEEPGRAM_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Listen, Vendor: "assemblyai",
		Verified: "2026-07-15", Docs: "https://docs.pipecat.ai/api-reference/server/services/stt/assemblyai",
		Install: InstallSpec{Extra: "assemblyai"},
		Import:  "from pipecat.services.assemblyai.stt import AssemblyAISTTService",
		Call: &CallSpec{
			Class: "AssemblyAISTTService", APIKeyArg: "api_key", APIKeyEnv: "ASSEMBLYAI_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Listen, Vendor: "openai",
		Verified: "2026-07-15", Docs: "https://docs.pipecat.ai/api-reference/server/services/stt/openai",
		Install: InstallSpec{Extra: "openai"},
		Import:  "from pipecat.services.openai.stt import OpenAISTTService",
		Call: &CallSpec{
			Class: "OpenAISTTService", APIKeyArg: "api_key", APIKeyEnv: "OPENAI_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Language: FieldSpec{Arg: "language"},
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Listen, Vendor: "cartesia",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/stt/cartesia",
		Install: InstallSpec{Extra: "cartesia"},
		Import:  "from pipecat.services.cartesia.stt import CartesiaSTTService",
		Call: &CallSpec{
			Class: "CartesiaSTTService", APIKeyArg: "api_key", APIKeyEnv: "CARTESIA_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Listen, Vendor: "elevenlabs", Aliases: []string{"eleven_labs"},
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/stt/elevenlabs",
		Install: InstallSpec{Extra: "elevenlabs"},
		Import:  "from pipecat.services.elevenlabs.stt import ElevenLabsRealtimeSTTService",
		Call: &CallSpec{
			Class: "ElevenLabsRealtimeSTTService", APIKeyArg: "api_key", APIKeyEnv: "ELEVENLABS_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsSettings,
		},
		Notes: []string{"the WebSocket realtime service; the HTTP ElevenLabsSTTService needs a caller-supplied aiohttp session, which the generic builder has no slot for"},
	},
	{
		Framework: Pipecat, Role: Listen, Vendor: "gradium",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/stt/gradium",
		Install: InstallSpec{Extra: "gradium"},
		Import:  "from pipecat.services.gradium.stt import GradiumSTTService",
		Call: &CallSpec{
			Class: "GradiumSTTService", APIKeyArg: "api_key", APIKeyEnv: "GRADIUM_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Listen, Vendor: "soniox",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/stt/soniox",
		Install: InstallSpec{Extra: "soniox"},
		Import:  "from pipecat.services.soniox.stt import SonioxSTTService",
		Call: &CallSpec{
			Class: "SonioxSTTService", APIKeyArg: "api_key", APIKeyEnv: "SONIOX_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Listen, Vendor: "speechmatics",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/stt/speechmatics",
		Install: InstallSpec{Extra: "speechmatics"},
		Import:  "from pipecat.services.speechmatics.stt import SpeechmaticsSTTService",
		Call: &CallSpec{
			Class: "SpeechmaticsSTTService", APIKeyArg: "api_key", APIKeyEnv: "SPEECHMATICS_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Listen, Vendor: "slng",
		Distributes: []string{"deepgram"},
		Verified:    "2026-07-15", Docs: pipecatServicesDocs,
		Install: InstallSpec{Package: "pipecat-slng", Constraint: ">=0.4.0"},
		Import:  "from pipecat_slng import SlngSTTService",
		Call: &CallSpec{
			Class: "SlngSTTService", APIKeyArg: "api_key", APIKeyEnv: "SLNG_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true}, // keeps the slng/<vendor>/<model> route form
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsKwargs,
		},
		Notes: []string{"routes by api_key + region params; endpoint_env has no slot (driver-pipecat B1/C10)"},
	},
	{
		Framework: Pipecat, Role: Listen, Vendor: "*",
		Verified: "2026-07-15", Docs: "https://docs.pipecat.ai/api-reference/server/services/stt/openai",
		Install: InstallSpec{Extra: "openai"},
		Import:  "from pipecat.services.openai.stt import OpenAISTTService",
		Call: &CallSpec{
			Class: "OpenAISTTService", APIKeyArg: "api_key",
			Model:    FieldSpec{Arg: "model", Required: true},
			Language: FieldSpec{Arg: "language"},
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsSettings,
		},
		RequiresEndpoint: true,
		Notes:            []string{"OpenAI-compatible custom endpoint; api-key env follows the <PROVIDER>_API_KEY convention"},
	},

	// --- speak ----------------------------------------------------------
	{
		Framework: Pipecat, Role: Speak, Vendor: "elevenlabs", Aliases: []string{"eleven_labs"},
		Verified: "2026-07-15", Docs: "https://docs.pipecat.ai/api-reference/server/services/tts/elevenlabs",
		Install: InstallSpec{Extra: "elevenlabs"},
		Import:  "from pipecat.services.elevenlabs.tts import ElevenLabsTTSService",
		Call: &CallSpec{
			Class: "ElevenLabsTTSService", APIKeyArg: "api_key", APIKeyEnv: "ELEVENLABS_API_KEY",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "voice", Required: true}, // Settings(voice=...); the flat voice_id kwarg is the deprecated pre-0.0.105 form
			Language: FieldSpec{Arg: "language"},
			Speed:    FieldSpec{Arg: "speed"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Speak, Vendor: "cartesia",
		Verified: "2026-07-15", Docs: "https://docs.pipecat.ai/api-reference/server/services/tts/cartesia",
		Install: InstallSpec{Extra: "cartesia"},
		Import:  "from pipecat.services.cartesia.tts import CartesiaTTSService",
		Call: &CallSpec{
			Class: "CartesiaTTSService", APIKeyArg: "api_key", APIKeyEnv: "CARTESIA_API_KEY",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "voice", Required: true},
			Language: FieldSpec{Arg: "language"},
			// No Speed slot: Cartesia puts speed one level down, in
			// Settings(generation_config=GenerationConfig(speed=...)), and a
			// nested constructor is not a kwarg this builder can fill.
			Params: ParamsSettings,
		},
		Notes: []string{"speed has no flat slot: it nests in Settings(generation_config=GenerationConfig(speed=...))"},
	},
	{
		Framework: Pipecat, Role: Speak, Vendor: "openai",
		Verified: "2026-07-15", Docs: "https://docs.pipecat.ai/api-reference/server/services/tts/openai",
		Install: InstallSpec{Extra: "openai"},
		Import:  "from pipecat.services.openai.tts import OpenAITTSService",
		Call: &CallSpec{
			Class: "OpenAITTSService", APIKeyArg: "api_key", APIKeyEnv: "OPENAI_API_KEY",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "voice"},
			Language: FieldSpec{Arg: "language"},
			Endpoint: FieldSpec{Arg: "base_url"},
			Speed:    FieldSpec{Arg: "speed"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Speak, Vendor: "deepgram",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/tts/deepgram",
		Install: InstallSpec{Extra: "deepgram"},
		Import:  "from pipecat.services.deepgram.tts import DeepgramTTSService",
		Call: &CallSpec{
			Class: "DeepgramTTSService", APIKeyArg: "api_key", APIKeyEnv: "DEEPGRAM_API_KEY",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "voice", Required: true}, // aura voice ids (e.g. aura-2-helena-en)
			Language: FieldSpec{Arg: "language"},
			// No Speed slot: Settings is model/voice/language only, Aura has no
			// rate control.
			Params: ParamsSettings,
		},
		Notes: []string{"no speed slot: Aura exposes no rate control, so Settings is model/voice/language only"},
	},
	{
		Framework: Pipecat, Role: Speak, Vendor: "gradium",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/tts/gradium",
		Install: InstallSpec{Extra: "gradium"},
		Import:  "from pipecat.services.gradium.tts import GradiumTTSService",
		Call: &CallSpec{
			Class: "GradiumTTSService", APIKeyArg: "api_key", APIKeyEnv: "GRADIUM_API_KEY",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "voice", Required: true},
			Language: FieldSpec{Arg: "language"},
			// No Speed slot: Settings is model/voice/language plus connection
			// event handlers.
			Params: ParamsSettings,
		},
		Notes: []string{"no speed slot: Settings carries model, voice, language and connection handlers only"},
	},
	{
		Framework: Pipecat, Role: Speak, Vendor: "inworld",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/tts/inworld",
		Install: InstallSpec{Extra: "inworld"},
		Import:  "from pipecat.services.inworld.tts import InworldTTSService",
		Call: &CallSpec{
			Class: "InworldTTSService", APIKeyArg: "api_key", APIKeyEnv: "INWORLD_API_KEY",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "voice", Required: true},
			Language: FieldSpec{Arg: "language"},
			Speed:    FieldSpec{Arg: "speaking_rate"},
			Params:   ParamsSettings,
		},
		Notes: []string{"speed lowers to speaking_rate (per-entry fact, F3/F4)"},
	},
	{
		Framework: Pipecat, Role: Speak, Vendor: "rime",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/tts/rime",
		Install: InstallSpec{Extra: "rime"},
		Import:  "from pipecat.services.rime.tts import RimeTTSService",
		Call: &CallSpec{
			Class: "RimeTTSService", APIKeyArg: "api_key", APIKeyEnv: "RIME_API_KEY",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "voice", Required: true},
			Language: FieldSpec{Arg: "language"},
			Speed:    FieldSpec{Arg: "speedAlpha"}, // Rime's own camelCase spelling
			Params:   ParamsSettings,
		},
		Notes: []string{"speed lowers to speedAlpha, Rime's own camelCase spelling (per-entry fact, F3/F4)"},
	},
	{
		Framework: Pipecat, Role: Speak, Vendor: "sarvam",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/tts/sarvam",
		Install: InstallSpec{Extra: "sarvam"},
		Import:  "from pipecat.services.sarvam.tts import SarvamTTSService",
		Call: &CallSpec{
			Class: "SarvamTTSService", APIKeyArg: "api_key", APIKeyEnv: "SARVAM_API_KEY",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "voice", Required: true},
			Language: FieldSpec{Arg: "language"},
			Speed:    FieldSpec{Arg: "pace"},
			Params:   ParamsSettings,
		},
		Notes: []string{
			"the WebSocket service; SarvamHttpTTSService is the HTTP variant (PR #9 shipped that one)",
			"speed lowers to pace (per-entry fact, F3/F4)",
		},
	},
	{
		Framework: Pipecat, Role: Speak, Vendor: "soniox",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/tts/soniox",
		Install: InstallSpec{Extra: "soniox"},
		Import:  "from pipecat.services.soniox.tts import SonioxTTSService",
		Call: &CallSpec{
			Class: "SonioxTTSService", APIKeyArg: "api_key", APIKeyEnv: "SONIOX_API_KEY",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "voice", Required: true},
			Language: FieldSpec{Arg: "language"},
			Speed:    FieldSpec{Arg: "speed"},
			Params:   ParamsSettings,
		},
	},
	// speechmatics speak is deliberately absent: SpeechmaticsTTSService requires
	// a caller-supplied aiohttp_session (required keyword-only argument), and
	// this driver constructs its workers at module import where no event loop
	// runs — smoke-proven incompatible 2026-07-17 (driver-pipecat T13).
	{
		Framework: Pipecat, Role: Speak, Vendor: "slng",
		Distributes: []string{"cartesia", "deepgram"},
		Verified:    "2026-07-15", Docs: pipecatServicesDocs,
		Install: InstallSpec{Package: "pipecat-slng", Constraint: ">=0.4.0"},
		Import:  "from pipecat_slng import SlngTTSService",
		Call: &CallSpec{
			Class: "SlngTTSService", APIKeyArg: "api_key", APIKeyEnv: "SLNG_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Voice:    FieldSpec{Arg: "voice"},
			Language: FieldSpec{Arg: "language"},
			Speed:    FieldSpec{Arg: "speed"},
			Params:   ParamsKwargs,
		},
		Notes: []string{"routes by api_key + region params; endpoint_env has no slot (driver-pipecat B1/C10)"},
	},
	{
		Framework: Pipecat, Role: Speak, Vendor: "*",
		Verified: "2026-07-15", Docs: "https://docs.pipecat.ai/api-reference/server/services/tts/openai",
		Install: InstallSpec{Extra: "openai"},
		Import:  "from pipecat.services.openai.tts import OpenAITTSService",
		Call: &CallSpec{
			Class: "OpenAITTSService", APIKeyArg: "api_key",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "voice"},
			Language: FieldSpec{Arg: "language"},
			Endpoint: FieldSpec{Arg: "base_url"},
			Speed:    FieldSpec{Arg: "speed"},
			Params:   ParamsSettings,
		},
		RequiresEndpoint: true,
		Notes:            []string{"OpenAI-compatible custom endpoint; api-key env follows the <PROVIDER>_API_KEY convention"},
	},

	// --- reason ---------------------------------------------------------
	// The workers-model driver injects system_instruction into Settings; the
	// entry carries the shape, the driver carries the prompt.
	{
		Framework: Pipecat, Role: Reason, Vendor: "openai",
		Verified: "2026-07-15", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/openai",
		Install: InstallSpec{Extra: "openai"},
		Import:  "from pipecat.services.openai.llm import OpenAILLMService",
		Call: &CallSpec{
			Class: "OpenAILLMService", APIKeyArg: "api_key", APIKeyEnv: "OPENAI_API_KEY",
			Model:       FieldSpec{Arg: "model", Required: true},
			Endpoint:    FieldSpec{Arg: "base_url"},
			Temperature: FieldSpec{Arg: "temperature"},
			TopP:        FieldSpec{Arg: "top_p"},
			TopK:        FieldSpec{Arg: "top_k"},
			Params:      ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "anthropic",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/anthropic",
		Install: InstallSpec{Extra: "anthropic"},
		Import:  "from pipecat.services.anthropic.llm import AnthropicLLMService",
		Call: &CallSpec{
			Class: "AnthropicLLMService", APIKeyArg: "api_key", APIKeyEnv: "ANTHROPIC_API_KEY",
			Model:       FieldSpec{Arg: "model", Required: true},
			Temperature: FieldSpec{Arg: "temperature"},
			TopP:        FieldSpec{Arg: "top_p"},
			TopK:        FieldSpec{Arg: "top_k"},
			Params:      ParamsSettings, // Settings accepts system_instruction (workers driver injects it)
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "deepseek",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/deepseek",
		Install: InstallSpec{Extra: "deepseek"},
		Import:  "from pipecat.services.deepseek.llm import DeepSeekLLMService",
		Call: &CallSpec{
			Class: "DeepSeekLLMService", APIKeyArg: "api_key", APIKeyEnv: "DEEPSEEK_API_KEY",
			Model:       FieldSpec{Arg: "model", Required: true},
			Endpoint:    FieldSpec{Arg: "base_url"},
			Temperature: FieldSpec{Arg: "temperature"},
			TopP:        FieldSpec{Arg: "top_p"},
			TopK:        FieldSpec{Arg: "top_k"},
			Params:      ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "google",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/google",
		Install: InstallSpec{Extra: "google"},
		Import:  "from pipecat.services.google.llm import GoogleLLMService",
		Call: &CallSpec{
			Class: "GoogleLLMService", APIKeyArg: "api_key", APIKeyEnv: "GOOGLE_API_KEY",
			Model:       FieldSpec{Arg: "model", Required: true},
			Temperature: FieldSpec{Arg: "temperature"},
			TopP:        FieldSpec{Arg: "top_p"},
			TopK:        FieldSpec{Arg: "top_k"},
			Params:      ParamsSettings, // Gemini GenAI backend; Settings accepts system_instruction
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "groq",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/groq",
		Install: InstallSpec{Extra: "groq"},
		Import:  "from pipecat.services.groq.llm import GroqLLMService",
		Call: &CallSpec{
			Class: "GroqLLMService", APIKeyArg: "api_key", APIKeyEnv: "GROQ_API_KEY",
			Model:       FieldSpec{Arg: "model", Required: true},
			Endpoint:    FieldSpec{Arg: "base_url"},
			Temperature: FieldSpec{Arg: "temperature"},
			TopP:        FieldSpec{Arg: "top_p"},
			TopK:        FieldSpec{Arg: "top_k"},
			Params:      ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "mistral",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/mistral",
		Install: InstallSpec{Extra: "mistral"},
		Import:  "from pipecat.services.mistral.llm import MistralLLMService",
		Call: &CallSpec{
			Class: "MistralLLMService", APIKeyArg: "api_key", APIKeyEnv: "MISTRAL_API_KEY",
			Model:       FieldSpec{Arg: "model", Required: true},
			Endpoint:    FieldSpec{Arg: "base_url"},
			Temperature: FieldSpec{Arg: "temperature"},
			TopP:        FieldSpec{Arg: "top_p"},
			TopK:        FieldSpec{Arg: "top_k"},
			Params:      ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "openrouter",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/openrouter",
		Install: InstallSpec{Extra: "openrouter"},
		Import:  "from pipecat.services.openrouter.llm import OpenRouterLLMService",
		Call: &CallSpec{
			Class: "OpenRouterLLMService", APIKeyArg: "api_key", APIKeyEnv: "OPENROUTER_API_KEY",
			Model:       FieldSpec{Arg: "model", Required: true},
			Endpoint:    FieldSpec{Arg: "base_url"},
			Temperature: FieldSpec{Arg: "temperature"},
			TopP:        FieldSpec{Arg: "top_p"},
			TopK:        FieldSpec{Arg: "top_k"},
			Params:      ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "qwen",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/qwen",
		Install: InstallSpec{Extra: "qwen"},
		Import:  "from pipecat.services.qwen.llm import QwenLLMService",
		Call: &CallSpec{
			Class: "QwenLLMService", APIKeyArg: "api_key", APIKeyEnv: "QWEN_API_KEY",
			Model:       FieldSpec{Arg: "model", Required: true},
			Endpoint:    FieldSpec{Arg: "base_url"},
			Temperature: FieldSpec{Arg: "temperature"},
			TopP:        FieldSpec{Arg: "top_p"},
			TopK:        FieldSpec{Arg: "top_k"},
			Params:      ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "*",
		Verified: "2026-07-15", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/openai",
		Install: InstallSpec{Extra: "openai"},
		Import:  "from pipecat.services.openai.llm import OpenAILLMService",
		Call: &CallSpec{
			Class: "OpenAILLMService", APIKeyArg: "api_key",
			Model:       FieldSpec{Arg: "model", Required: true},
			Endpoint:    FieldSpec{Arg: "base_url"},
			Temperature: FieldSpec{Arg: "temperature"},
			TopP:        FieldSpec{Arg: "top_p"},
			TopK:        FieldSpec{Arg: "top_k"},
			Params:      ParamsSettings,
		},
		RequiresEndpoint: true,
		Notes:            []string{"OpenAI-compatible custom endpoint (the documented SLNG-as-reason path); api-key env follows the <PROVIDER>_API_KEY convention"},
	},
}
