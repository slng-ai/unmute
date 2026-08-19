package target

// Pipecat entries. Contract: the pipecat_v1 driver templates, pipecat-ai
// ==1.7.0. Official services ship as pipecat-ai extras and take model/
// voice/params nested in Class.Settings(...) (flat forms deprecated since
// v0.0.105; verified against the per-service docs 2026-07-15). The SLNG
// plugin is a standalone package with flat kwargs (verified against
// github.com/slng-ai/pipecat-slng source 2026-07-15).

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
			Params:   ParamsSettings,
		},
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
			Params:   ParamsSettings,
		},
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
			Params:   ParamsSettings,
		},
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
			Params:   ParamsSettings,
		},
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
			Params:   ParamsSettings,
		},
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
			Params:   ParamsSettings,
		},
		Notes: []string{"the WebSocket service; SarvamHttpTTSService is the HTTP variant (PR #9 shipped that one)"},
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
			Model:    FieldSpec{Arg: "model", Required: true},
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsSettings,
			// `reasoning_effort` is the param that needs this: OpenAI rejects
			// function tools on /v1/chat/completions for a reasoning model
			// unless the request carries reasoning_effort="none", and
			// OpenAILLMSettings has no field for it (verified against
			// pipecat-ai 1.5.0, 2026-08-15).
			SettingsOverflow: "extra",
		},
	},
	{
		// The SLNG Context Router. Same class and same install as the openai row
		// above, because the router speaks Chat Completions: what changes is the
		// base URL, the key, and two identity headers.
		//
		// No Distributes: the router picks the model itself per turn, so naming
		// a brand here would mislabel it in the interactive console (FR-021).
		Framework: Pipecat, Role: Reason, Vendor: "slng",
		Verified: SlngRouterVerified, Docs: SlngRouterDocs,
		Install: InstallSpec{Extra: "openai"},
		Import:  "from pipecat.services.openai.llm import OpenAILLMService",
		Call: &CallSpec{
			Class: "OpenAILLMService", APIKeyArg: "api_key", APIKeyEnv: SlngRouterKeyEnv,
			Model:    FieldSpec{Arg: "model", Required: true},
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsSettings,
			// Settings.extra is merged verbatim into the request params
			// (services/openai/base_llm.py:353, pipecat 1.7.0 source read
			// 2026-08-19), so extra_headers and extra_body reach the SDK call
			// without a new Python class.
			SettingsOverflow: "extra",
		},
		Notes: []string{"SLNG Context Router over Chat Completions; params.world_part_override becomes base_url and the identity headers plus the inline slng_config ride Settings.extra"},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "anthropic",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/anthropic",
		Install: InstallSpec{Extra: "anthropic"},
		Import:  "from pipecat.services.anthropic.llm import AnthropicLLMService",
		Call: &CallSpec{
			Class: "AnthropicLLMService", APIKeyArg: "api_key", APIKeyEnv: "ANTHROPIC_API_KEY",
			Model:  FieldSpec{Arg: "model", Required: true},
			Params: ParamsSettings, // Settings accepts system_instruction (workers driver injects it)
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "deepseek",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/deepseek",
		Install: InstallSpec{Extra: "deepseek"},
		Import:  "from pipecat.services.deepseek.llm import DeepSeekLLMService",
		Call: &CallSpec{
			Class: "DeepSeekLLMService", APIKeyArg: "api_key", APIKeyEnv: "DEEPSEEK_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "google",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/google",
		Install: InstallSpec{Extra: "google"},
		Import:  "from pipecat.services.google.llm import GoogleLLMService",
		Call: &CallSpec{
			Class: "GoogleLLMService", APIKeyArg: "api_key", APIKeyEnv: "GOOGLE_API_KEY",
			Model:  FieldSpec{Arg: "model", Required: true},
			Params: ParamsSettings, // Gemini GenAI backend; Settings accepts system_instruction
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "groq",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/groq",
		Install: InstallSpec{Extra: "groq"},
		Import:  "from pipecat.services.groq.llm import GroqLLMService",
		Call: &CallSpec{
			Class: "GroqLLMService", APIKeyArg: "api_key", APIKeyEnv: "GROQ_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "mistral",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/mistral",
		Install: InstallSpec{Extra: "mistral"},
		Import:  "from pipecat.services.mistral.llm import MistralLLMService",
		Call: &CallSpec{
			Class: "MistralLLMService", APIKeyArg: "api_key", APIKeyEnv: "MISTRAL_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "openrouter",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/openrouter",
		Install: InstallSpec{Extra: "openrouter"},
		Import:  "from pipecat.services.openrouter.llm import OpenRouterLLMService",
		Call: &CallSpec{
			Class: "OpenRouterLLMService", APIKeyArg: "api_key", APIKeyEnv: "OPENROUTER_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "qwen",
		Verified: "2026-07-17", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/qwen",
		Install: InstallSpec{Extra: "qwen"},
		Import:  "from pipecat.services.qwen.llm import QwenLLMService",
		Call: &CallSpec{
			Class: "QwenLLMService", APIKeyArg: "api_key", APIKeyEnv: "QWEN_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "*",
		Verified: "2026-07-15", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/openai",
		Install: InstallSpec{Extra: "openai"},
		Import:  "from pipecat.services.openai.llm import OpenAILLMService",
		Call: &CallSpec{
			Class: "OpenAILLMService", APIKeyArg: "api_key",
			Model:            FieldSpec{Arg: "model", Required: true},
			Endpoint:         FieldSpec{Arg: "base_url"},
			Params:           ParamsSettings,
			SettingsOverflow: "extra", // same class as the openai row above
		},
		RequiresEndpoint: true,
		Notes:            []string{"OpenAI-compatible custom endpoint (the documented SLNG-as-reason path); api-key env follows the <PROVIDER>_API_KEY convention"},
	},
}
