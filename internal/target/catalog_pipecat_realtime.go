package target

// Pipecat realtime entries: one model that listens, thinks and speaks over the
// OpenAI Realtime API, standing where the transcriber, the model and the
// synthesizer stood. Same contract as catalog_pipecat.go (the pipecat_v1 driver
// templates, pipecat-ai ==1.10.0).
//
// Two facts this table cannot express, so the driver supplies both as Python
// expressions rather than FieldSpecs, the way the live entry's `delegation` is:
//
//   - the nested session properties, which carry the voice, who decides the
//     turn, whether the caller is transcribed and whether the model speaks at
//     all;
//   - the voice itself, which is why Voice stays a zero FieldSpec below. On
//     every other Pipecat entry the voice is a flat Settings field; here it
//     lives three levels down at session_properties.audio.output.voice, and
//     OpenAIRealtimeLLMService.Settings has no `voice` field at all
//     (services/settings.py LLMSettings + services/openai/realtime/llm.py:99-112,
//     read 2026-09-13), so a `voice=` kwarg is a TypeError rather than a
//     setting that lands somewhere harmless.
var pipecatRealtimeCatalog = []Entry{
	{
		// pipecat-ai 1.10.0 services/openai/realtime/llm.py:236-247:
		// OpenAIRealtimeLLMService takes api_key, base_url, settings and
		// user_audio_preroll_secs. `model=` and `session_properties=` are still
		// accepted as top-level kwargs and are deprecated for removal in 2.0.0
		// (llm.py:255-270); each prints a DeprecationWarning on every worker
		// start (llm.py:308, llm.py:312). Both belong inside `settings=`, which
		// is what ParamsSettings gives this entry: Settings(model=...) here, and
		// Settings(session_properties=...) written by the driver.
		//
		// The service's own package __init__ is empty in 1.10.0, so both this
		// import and the driver's events import name the full module path.
		Framework: Pipecat, Role: Realtime, Vendor: "openai",
		Verified: "2026-09-13", Docs: pipecatServicesDocs,
		Install: InstallSpec{Extra: "openai"},
		Import:  "from pipecat.services.openai.realtime.llm import OpenAIRealtimeLLMService",
		Call: &CallSpec{
			Class: "OpenAIRealtimeLLMService", APIKeyArg: "api_key", APIKeyEnv: "OPENAI_API_KEY",
			// Required, and not defaulted here on purpose: the service's own
			// default is "gpt-realtime-2.1" (llm.py:290), and the model id is a
			// URL query parameter rather than a session field (llm.py:334-336),
			// so a package that does not name one would be pinned by whichever
			// framework release the image happened to install.
			Model:  FieldSpec{Arg: "model", Required: true},
			Params: ParamsSettings,
		},
		Notes: []string{"OpenAI Realtime (gpt-realtime-2.1): speech to speech over one websocket session; the author picks who decides the turn, and a bound speak model may synthesize the reply instead of the model (the half cascade)"},
	},
}
