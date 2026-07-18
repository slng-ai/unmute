package target

// LiveKit entries. Contract: the livekit_v1 driver templates, livekit-agents
// >=1.5 <2.0, Python. Per-vendor plugins install as livekit-agents extras
// (each wraps a livekit-plugins-<name> package); the SLNG plugin is pinned as
// its own package to match the shipped driver. reason lowers to LiveKit
// Inference (no provider key; billed through LiveKit Cloud), so its row is
// the role's wildcard. Verified against docs.livekit.io 2026-07-15.
//
// SLNG stays the init-scaffold default (driver-livekit V12); since the C8
// amendment it is the default, not the only route — any entry here binds.

var livekitCatalog = []Entry{
	// --- listen ---------------------------------------------------------
	{
		Framework: LiveKit, Role: Listen, Vendor: "slng",
		Distributes: []string{"deepgram"},
		Verified:    "2026-07-15", Docs: "https://docs.livekit.io/agents/models/stt/slng/",
		Install: InstallSpec{Package: "livekit-plugins-slng", Constraint: ">=1.6.1"},
		Import:  "from livekit.plugins import slng",
		Call: &CallSpec{
			Class: "slng.STT", APIKeyArg: "api_key", APIKeyEnv: "SLNG_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsKwargs,
		},
		Notes: []string{"the route passes to model= verbatim — the slng/ prefix names the SLNG-hosted route family and is part of the URL path (driver-livekit C8/V17, B4)"},
	},
	{
		Framework: LiveKit, Role: Listen, Vendor: "deepgram",
		Verified: "2026-07-15", Docs: "https://docs.livekit.io/agents/models/stt/deepgram/",
		Install: InstallSpec{Extra: "deepgram"},
		Import:  "from livekit.plugins import deepgram",
		Call: &CallSpec{
			Class: "deepgram.STT", APIKeyArg: "api_key", APIKeyEnv: "DEEPGRAM_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsKwargs,
		},
		Notes: []string{"flux models use deepgram.STTv2; add a separate entry when a spec needs one"},
	},

	{
		Framework: LiveKit, Role: Listen, Vendor: "cartesia",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/stt/cartesia/",
		Install: InstallSpec{Extra: "cartesia"},
		Import:  "from livekit.plugins import cartesia",
		Call: &CallSpec{
			Class: "cartesia.STT", APIKeyArg: "api_key", APIKeyEnv: "CARTESIA_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsKwargs,
		},
	},
	{
		Framework: LiveKit, Role: Listen, Vendor: "elevenlabs", Aliases: []string{"eleven_labs"},
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/stt/elevenlabs/",
		Install: InstallSpec{Extra: "elevenlabs"},
		Import:  "from livekit.plugins import elevenlabs",
		Call: &CallSpec{
			Class: "elevenlabs.STT", APIKeyArg: "api_key", APIKeyEnv: "ELEVEN_API_KEY",
			Model:    FieldSpec{Arg: "model_id", Required: true},
			Language: FieldSpec{Arg: "language_code"},
			Params:   ParamsKwargs,
		},
		Notes: []string{"Python-only plugin; the model kwarg is model_id and language is language_code (per-entry facts, F3/F4)"},
	},
	{
		Framework: LiveKit, Role: Listen, Vendor: "gradium",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/stt/gradium/",
		Install: InstallSpec{Extra: "gradium"},
		Import:  "from livekit.plugins import gradium",
		Call: &CallSpec{
			Class: "gradium.STT", APIKeyArg: "api_key", APIKeyEnv: "GRADIUM_API_KEY",
			Model:    FieldSpec{Arg: "model_name", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsKwargs,
		},
	},
	{
		Framework: LiveKit, Role: Listen, Vendor: "sarvam",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/stt/sarvam/",
		Install: InstallSpec{Extra: "sarvam"},
		Import:  "from livekit.plugins import sarvam",
		Call: &CallSpec{
			Class: "sarvam.STT", APIKeyArg: "api_key", APIKeyEnv: "SARVAM_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsKwargs,
		},
	},
	{
		Framework: LiveKit, Role: Listen, Vendor: "soniox",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/stt/soniox/",
		Install: InstallSpec{Extra: "soniox"},
		Import:  "from livekit.plugins import soniox",
		Call: &CallSpec{
			Class: "soniox.STT", APIKeyArg: "api_key", APIKeyEnv: "SONIOX_API_KEY",
			Model:      FieldSpec{Arg: "model", Required: true},
			NoLanguage: true, // language identification is on by default; language_hints ride params
			Params:     ParamsSettings, SettingsArg: "params", SettingsClass: "soniox.STTOptions",
		},
		Notes: []string{"model and params nest in params=soniox.STTOptions(...); language is auto-identified (hints via params.language_hints)"},
	},
	{
		Framework: LiveKit, Role: Listen, Vendor: "assemblyai",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/stt/assemblyai/",
		Install: InstallSpec{Extra: "assemblyai"},
		Import:  "from livekit.plugins import assemblyai",
		Call: &CallSpec{
			Class: "assemblyai.STT", APIKeyArg: "api_key", APIKeyEnv: "ASSEMBLYAI_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Language: FieldSpec{Arg: "language_code"},
			Params:   ParamsKwargs,
		},
	},
	{
		Framework: LiveKit, Role: Listen, Vendor: "speechmatics",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/stt/speechmatics/",
		Install: InstallSpec{Extra: "speechmatics"},
		Import:  "from livekit.plugins import speechmatics",
		Call: &CallSpec{
			Class: "speechmatics.STT", APIKeyArg: "api_key", APIKeyEnv: "SPEECHMATICS_API_KEY",
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsKwargs,
		},
		Notes: []string{"no model kwarg (a model value is a hard error); accuracy is chosen via operating_point in params"},
	},

	// --- speak ----------------------------------------------------------
	{
		Framework: LiveKit, Role: Speak, Vendor: "slng",
		Distributes: []string{"cartesia", "deepgram"},
		Verified:    "2026-07-15", Docs: "https://docs.livekit.io/agents/models/tts/slng/",
		Install: InstallSpec{Package: "livekit-plugins-slng", Constraint: ">=1.6.1"},
		Import:  "from livekit.plugins import slng",
		Call: &CallSpec{
			Class: "slng.TTS", APIKeyArg: "api_key", APIKeyEnv: "SLNG_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Voice:    FieldSpec{Arg: "voice"},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsKwargs,
		},
		Notes: []string{"the route passes to model= verbatim — the slng/ prefix names the SLNG-hosted route family and is part of the URL path (driver-livekit C8/V17, B4)"},
	},
	{
		Framework: LiveKit, Role: Speak, Vendor: "elevenlabs", Aliases: []string{"eleven_labs"},
		Verified: "2026-07-15", Docs: "https://docs.livekit.io/agents/models/tts/elevenlabs/",
		Install: InstallSpec{Extra: "elevenlabs"},
		Import:  "from livekit.plugins import elevenlabs",
		Call: &CallSpec{
			// The LiveKit plugin spells the voice kwarg voice_id and documents
			// ELEVEN_API_KEY (not ELEVENLABS_API_KEY, which the managed
			// elevenlabs driver uses) — per-entry facts, not conventions.
			Class: "elevenlabs.TTS", APIKeyArg: "api_key", APIKeyEnv: "ELEVEN_API_KEY",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "voice_id", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsKwargs,
		},
	},
	{
		Framework: LiveKit, Role: Speak, Vendor: "cartesia",
		Verified: "2026-07-15", Docs: "https://docs.livekit.io/agents/models/tts/cartesia/",
		Install: InstallSpec{Extra: "cartesia"},
		Import:  "from livekit.plugins import cartesia",
		Call: &CallSpec{
			Class: "cartesia.TTS", APIKeyArg: "api_key", APIKeyEnv: "CARTESIA_API_KEY",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "voice", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsKwargs,
		},
	},

	{
		Framework: LiveKit, Role: Speak, Vendor: "deepgram",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/tts/deepgram/",
		Install: InstallSpec{Extra: "deepgram"},
		Import:  "from livekit.plugins import deepgram",
		Call: &CallSpec{
			Class: "deepgram.TTS", APIKeyArg: "api_key", APIKeyEnv: "DEEPGRAM_API_KEY",
			Model:      FieldSpec{Arg: "model", Required: true},
			NoLanguage: true, // voice and language both ride the aura model id (aura-2-andromeda-en)
			Params:     ParamsKwargs,
		},
		Notes: []string{"no voice or language kwarg: both ride the aura model id (a bound voice is a hard error)"},
	},
	{
		Framework: LiveKit, Role: Speak, Vendor: "inworld",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/tts/inworld/",
		Install: InstallSpec{Extra: "inworld"},
		Import:  "from livekit.plugins import inworld",
		Call: &CallSpec{
			Class: "inworld.TTS", APIKeyArg: "api_key", APIKeyEnv: "INWORLD_API_KEY",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "voice", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsKwargs,
		},
	},
	{
		Framework: LiveKit, Role: Speak, Vendor: "rime",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/tts/rime/",
		Install: InstallSpec{Extra: "rime"},
		Import:  "from livekit.plugins import rime",
		Call: &CallSpec{
			Class: "rime.TTS", APIKeyArg: "api_key", APIKeyEnv: "RIME_API_KEY",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "speaker", Required: true},
			Language: FieldSpec{Arg: "lang"},
			Params:   ParamsKwargs,
		},
		Notes: []string{"voice kwarg is speaker, language kwarg is lang (per-entry facts, F3/F4)"},
	},
	{
		Framework: LiveKit, Role: Speak, Vendor: "gemini",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/tts/gemini/",
		Install: InstallSpec{Extra: "google"},
		Import:  "from livekit.plugins import google",
		Call: &CallSpec{
			Class: "google.beta.GeminiTTS", APIKeyArg: "api_key", APIKeyEnv: "GOOGLE_API_KEY",
			Model:      FieldSpec{Arg: "model"},
			Voice:      FieldSpec{Arg: "voice_name", Required: true},
			NoLanguage: true, // Gemini TTS voices are multilingual; no language kwarg exists
			Params:     ParamsKwargs,
		},
		Notes: []string{"beta namespace constructor (google.beta.GeminiTTS); rides the google plugin extra"},
	},
	{
		Framework: LiveKit, Role: Speak, Vendor: "gradium",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/tts/gradium/",
		Install: InstallSpec{Extra: "gradium"},
		Import:  "from livekit.plugins import gradium",
		Call: &CallSpec{
			Class: "gradium.TTS", APIKeyArg: "api_key", APIKeyEnv: "GRADIUM_API_KEY",
			Model:      FieldSpec{Arg: "model_name"},
			Voice:      FieldSpec{Arg: "voice_id", Required: true},
			NoLanguage: true, // no language kwarg on the TTS constructor
			Params:     ParamsKwargs,
		},
	},
	{
		Framework: LiveKit, Role: Speak, Vendor: "sarvam",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/tts/sarvam/",
		Install: InstallSpec{Extra: "sarvam"},
		Import:  "from livekit.plugins import sarvam",
		Call: &CallSpec{
			Class: "sarvam.TTS", APIKeyArg: "api_key", APIKeyEnv: "SARVAM_API_KEY",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "speaker", Required: true},
			Language: FieldSpec{Arg: "target_language_code"},
			Params:   ParamsKwargs,
		},
		Notes: []string{"voice kwarg is speaker, language kwarg is target_language_code (per-entry facts, F3/F4)"},
	},
	{
		Framework: LiveKit, Role: Speak, Vendor: "soniox",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/tts/soniox/",
		Install: InstallSpec{Extra: "soniox"},
		Import:  "from livekit.plugins import soniox",
		Call: &CallSpec{
			Class: "soniox.TTS", APIKeyArg: "api_key", APIKeyEnv: "SONIOX_API_KEY",
			Model:    FieldSpec{Arg: "model"},
			Voice:    FieldSpec{Arg: "voice", Required: true},
			Language: FieldSpec{Arg: "language"},
			Params:   ParamsKwargs,
		},
	},

	// --- reason ---------------------------------------------------------
	// Native per-vendor LLM plugins where they exist (PR #10 restore + openai,
	// B6): an explicit vendor with a native entry binds it and its own key env
	// (V19). Vendors without one (gemini, deepseek, kimi, ...) fall to the
	// Inference wildcard below; provider: livekit is the deliberate Inference
	// spelling (model verbatim, "openai/gpt-4o-mini" form).
	{
		Framework: LiveKit, Role: Reason, Vendor: "openai",
		Verified: "2026-07-18", Docs: "https://docs.livekit.io/agents/models/llm/openai/",
		Install: InstallSpec{Extra: "openai"},
		Import:  "from livekit.plugins import openai",
		Call: &CallSpec{
			Class: "openai.LLM", APIKeyArg: "api_key", APIKeyEnv: "OPENAI_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsKwargs,
		},
		Notes: []string{"native plugin, not LiveKit Inference — a console run needs OPENAI_API_KEY only (B6); route through Inference deliberately with provider: livekit"},
	},
	{
		Framework: LiveKit, Role: Reason, Vendor: "anthropic",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/llm/anthropic/",
		Install: InstallSpec{Extra: "anthropic"},
		Import:  "from livekit.plugins import anthropic",
		Call: &CallSpec{
			Class: "anthropic.LLM", APIKeyArg: "api_key", APIKeyEnv: "ANTHROPIC_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsKwargs,
		},
	},
	{
		Framework: LiveKit, Role: Reason, Vendor: "aws",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/llm/aws/",
		Install: InstallSpec{Extra: "aws"},
		Import:  "from livekit.plugins import aws",
		Call: &CallSpec{
			Class:     "aws.LLM",
			ExtraEnvs: []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}, // boto3 default chain; no key kwarg emitted
			Model:     FieldSpec{Arg: "model", Required: true},
			Params:    ParamsKwargs,
		},
		Notes: []string{"Bedrock via the AWS SDK credential chain; region defaults to us-east-1 (override with a region param or AWS_REGION)"},
	},
	{
		Framework: LiveKit, Role: Reason, Vendor: "groq",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/llm/groq/",
		Install: InstallSpec{Extra: "groq"},
		Import:  "from livekit.plugins import groq",
		Call: &CallSpec{
			Class: "groq.LLM", APIKeyArg: "api_key", APIKeyEnv: "GROQ_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsKwargs,
		},
	},
	{
		Framework: LiveKit, Role: Reason, Vendor: "mistralai", Aliases: []string{"mistral"},
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/llm/mistralai/",
		Install: InstallSpec{Extra: "mistralai"},
		Import:  "from livekit.plugins import mistralai",
		Call: &CallSpec{
			Class: "mistralai.LLM", APIKeyArg: "api_key", APIKeyEnv: "MISTRAL_API_KEY",
			Model:  FieldSpec{Arg: "model", Required: true},
			Params: ParamsKwargs,
		},
		Notes: []string{"the plugin module is mistralai; mistral is accepted as an alias (matches the pipecat spelling)"},
	},
	{
		Framework: LiveKit, Role: Reason, Vendor: "openrouter",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/llm/openrouter/",
		Install: InstallSpec{Extra: "openai"},
		Import:  "from livekit.plugins import openai",
		Call: &CallSpec{
			Class: "openai.LLM.with_openrouter", APIKeyArg: "api_key", APIKeyEnv: "OPENROUTER_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsKwargs,
		},
		Notes: []string{"classmethod constructor on the openai plugin (openai.LLM.with_openrouter)"},
	},
	{
		Framework: LiveKit, Role: Reason, Vendor: "sarvam",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/llm/sarvam/",
		Install: InstallSpec{Extra: "sarvam"},
		Import:  "from livekit.plugins import sarvam",
		Call: &CallSpec{
			Class: "sarvam.LLM", APIKeyArg: "api_key", APIKeyEnv: "SARVAM_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Endpoint: FieldSpec{Arg: "base_url"},
			Params:   ParamsKwargs,
		},
	},
	{
		Framework: LiveKit, Role: Reason, Vendor: "azure",
		Verified: "2026-07-17", Docs: "https://docs.livekit.io/agents/models/llm/azure-openai/",
		Install: InstallSpec{Extra: "openai"},
		Import:  "from livekit.plugins import openai",
		Call: &CallSpec{
			Class: "openai.LLM.with_azure", APIKeyArg: "api_key", APIKeyEnv: "AZURE_OPENAI_API_KEY",
			Model:    FieldSpec{Arg: "model", Required: true},
			Endpoint: FieldSpec{Arg: "azure_endpoint"},
			Params:   ParamsKwargs,
		},
		Notes: []string{"set the deployment endpoint via endpoint_env (azure_endpoint) or AZURE_OPENAI_ENDPOINT; api_version via params when required"},
	},
	{
		Framework: LiveKit, Role: Reason, Vendor: "*",
		Verified: "2026-07-15", Docs: "https://docs.livekit.io/agents/models/llm/",
		Install: InstallSpec{}, // LiveKit Inference ships with livekit-agents
		Import:  "",            // inference is in the driver's core import block
		Call: &CallSpec{
			Class:  "inference.LLM",
			Model:  FieldSpec{Arg: "model", Required: true, Form: FormProviderSlashModel},
			Params: ParamsExtraKwargs,
		},
		Notes: []string{"LiveKit Inference: managed models, billed through LiveKit Cloud, no provider key — needs LIVEKIT_API_KEY/LIVEKIT_API_SECRET even in console mode; provider: livekit passes the model verbatim (V19)"},
	},
}
