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
		Verified: "2026-07-15", Docs: "https://docs.livekit.io/agents/models/stt/slng/",
		Install: InstallSpec{Package: "livekit-plugins-slng", Constraint: ">=1.6.1"},
		Import:  "from livekit.plugins import slng",
		Call: &CallSpec{
			Class: "slng.STT", APIKeyArg: "api_key", APIKeyEnv: "SLNG_API_KEY",
			Model:  FieldSpec{Arg: "model", Required: true, Form: FormSlngRoute},
			Params: ParamsKwargs,
		},
		Notes: []string{"the plugin takes the bare vendor/model route; the slng/ prefix is stripped (driver-livekit C8)"},
	},
	{
		Framework: LiveKit, Role: Listen, Vendor: "deepgram",
		Verified: "2026-07-15", Docs: "https://docs.livekit.io/agents/models/stt/deepgram/",
		Install: InstallSpec{Extra: "deepgram"},
		Import:  "from livekit.plugins import deepgram",
		Call: &CallSpec{
			Class: "deepgram.STT", APIKeyArg: "api_key", APIKeyEnv: "DEEPGRAM_API_KEY",
			Model:  FieldSpec{Arg: "model", Required: true},
			Params: ParamsKwargs,
		},
		Notes: []string{"flux models use deepgram.STTv2; add a separate entry when a spec needs one"},
	},

	// --- speak ----------------------------------------------------------
	{
		Framework: LiveKit, Role: Speak, Vendor: "slng",
		Verified: "2026-07-15", Docs: "https://docs.livekit.io/agents/models/tts/slng/",
		Install: InstallSpec{Package: "livekit-plugins-slng", Constraint: ">=1.6.1"},
		Import:  "from livekit.plugins import slng",
		Call: &CallSpec{
			Class: "slng.TTS", APIKeyArg: "api_key", APIKeyEnv: "SLNG_API_KEY",
			Model:  FieldSpec{Arg: "model", Required: true, Form: FormSlngRoute},
			Voice:  FieldSpec{Arg: "voice"},
			Params: ParamsKwargs,
		},
		Notes: []string{"the plugin takes the bare vendor/model route; the slng/ prefix is stripped (driver-livekit C8)"},
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
			Model:  FieldSpec{Arg: "model"},
			Voice:  FieldSpec{Arg: "voice_id", Required: true},
			Params: ParamsKwargs,
		},
	},
	{
		Framework: LiveKit, Role: Speak, Vendor: "cartesia",
		Verified: "2026-07-15", Docs: "https://docs.livekit.io/agents/models/tts/cartesia/",
		Install: InstallSpec{Extra: "cartesia"},
		Import:  "from livekit.plugins import cartesia",
		Call: &CallSpec{
			Class: "cartesia.TTS", APIKeyArg: "api_key", APIKeyEnv: "CARTESIA_API_KEY",
			Model:  FieldSpec{Arg: "model"},
			Voice:  FieldSpec{Arg: "voice", Required: true},
			Params: ParamsKwargs,
		},
	},

	// --- reason ---------------------------------------------------------
	{
		Framework: LiveKit, Role: Reason, Vendor: "*",
		Verified: "2026-07-15", Docs: "https://docs.livekit.io/agents/models/llm/",
		Install: InstallSpec{}, // LiveKit Inference ships with livekit-agents
		Import:  "",            // inference is in the driver's core import block
		Call: &CallSpec{
			Class: "inference.LLM",
			Model: FieldSpec{Arg: "model", Required: true, Form: FormProviderSlashModel},
			Params: ParamsExtraKwargs,
		},
		Notes: []string{"LiveKit Inference: managed models, billed through LiveKit Cloud, no provider key"},
	},
}
