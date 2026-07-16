package target

// Pipecat entries. Contract: the pipecat_v1 driver templates, pipecat-ai
// >=1.5.0 <2.0. Official services ship as pipecat-ai extras and take model/
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
			Model:  FieldSpec{Arg: "model", Required: true},
			Params: ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Listen, Vendor: "assemblyai",
		Verified: "2026-07-15", Docs: "https://docs.pipecat.ai/api-reference/server/services/stt/assemblyai",
		Install: InstallSpec{Extra: "assemblyai"},
		Import:  "from pipecat.services.assemblyai.stt import AssemblyAISTTService",
		Call: &CallSpec{
			Class: "AssemblyAISTTService", APIKeyArg: "api_key", APIKeyEnv: "ASSEMBLYAI_API_KEY",
			Model:  FieldSpec{Arg: "model", Required: true},
			Params: ParamsSettings,
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
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Listen, Vendor: "slng",
		Verified: "2026-07-15", Docs: pipecatServicesDocs,
		Install: InstallSpec{Package: "pipecat-slng", Constraint: ">=0.4.0"},
		Import:  "from pipecat_slng import SlngSTTService",
		Call: &CallSpec{
			Class: "SlngSTTService", APIKeyArg: "api_key", APIKeyEnv: "SLNG_API_KEY",
			Model:  FieldSpec{Arg: "model", Required: true}, // keeps the slng/<vendor>/<model> route form
			Params: ParamsKwargs,
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
			Model:  FieldSpec{Arg: "model"},
			Voice:  FieldSpec{Arg: "voice", Required: true}, // Settings(voice=...); the flat voice_id kwarg is the deprecated pre-0.0.105 form
			Params: ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Speak, Vendor: "cartesia",
		Verified: "2026-07-15", Docs: "https://docs.pipecat.ai/api-reference/server/services/tts/cartesia",
		Install: InstallSpec{Extra: "cartesia"},
		Import:  "from pipecat.services.cartesia.tts import CartesiaTTSService",
		Call: &CallSpec{
			Class: "CartesiaTTSService", APIKeyArg: "api_key", APIKeyEnv: "CARTESIA_API_KEY",
			Model:  FieldSpec{Arg: "model"},
			Voice:  FieldSpec{Arg: "voice", Required: true},
			Params: ParamsSettings,
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
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsSettings,
		},
	},
	{
		Framework: Pipecat, Role: Speak, Vendor: "slng",
		Verified: "2026-07-15", Docs: pipecatServicesDocs,
		Install: InstallSpec{Package: "pipecat-slng", Constraint: ">=0.4.0"},
		Import:  "from pipecat_slng import SlngTTSService",
		Call: &CallSpec{
			Class: "SlngTTSService", APIKeyArg: "api_key", APIKeyEnv: "SLNG_API_KEY",
			Model:  FieldSpec{Arg: "model", Required: true},
			Voice:  FieldSpec{Arg: "voice"},
			Params: ParamsKwargs,
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
		},
	},
	{
		Framework: Pipecat, Role: Reason, Vendor: "*",
		Verified: "2026-07-15", Docs: "https://docs.pipecat.ai/api-reference/server/services/llm/openai",
		Install: InstallSpec{Extra: "openai"},
		Import:  "from pipecat.services.openai.llm import OpenAILLMService",
		Call: &CallSpec{
			Class: "OpenAILLMService", APIKeyArg: "api_key",
			Model:    FieldSpec{Arg: "model", Required: true},
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsSettings,
		},
		RequiresEndpoint: true,
		Notes:            []string{"OpenAI-compatible custom endpoint (the documented SLNG-as-reason path); api-key env follows the <PROVIDER>_API_KEY convention"},
	},
}
