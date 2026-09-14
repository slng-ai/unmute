package target

// Pipecat live entries: one model that listens, thinks and speaks, standing
// where the transcriber, the model and the synthesizer stood. Same contract as
// catalog_pipecat.go (the pipecat_v1 driver templates, pipecat-ai ==1.9.0).
//
// What "Verified" on the row below covers: the offline suite reading the emitted
// module, and the opt-in smoke driving the real service with only its socket
// replaced. It does NOT cover a browser call. Real audio, real latency and real
// turn taking on this target are OWED, and this is the shape where the model
// takes the turn decision over entirely, so no offline check stands in for it.
// Whoever has a key should run `unmute dev --target pipecat` on
// examples/takeaway-orders and replace this paragraph with what happened.
//
// The driver adds the one argument this table cannot express: the backend the
// live model hands its tools and reasoning to, built from the think entry the
// live entry names. That is a Python expression over a second class, so it
// is a driver fact rather than a FieldSpec.
var pipecatLiveCatalog = []Entry{
	{
		// pipecat-ai 1.9.0 services/openai/live/llm.py: OpenAILiveLLMService takes
		// api_key, settings=Settings(model, voice, system_instruction, ...) and
		// delegation=. model, voice and system_instruction are fixed once the
		// session starts (docstring, read 2026-09-11), which is what holds a
		// live package to one agent: a handoff would have to restart the
		// session to change any of them.
		Framework: Pipecat, Role: Live, Vendor: "openai",
		Verified: "2026-09-11", Docs: pipecatServicesDocs,
		Install: InstallSpec{Extra: "openai"},
		Import:  "from pipecat.services.openai.live.llm import OpenAILiveLLMService",
		Call: &CallSpec{
			Class: "OpenAILiveLLMService", APIKeyArg: "api_key", APIKeyEnv: "OPENAI_API_KEY",
			Model:  FieldSpec{Arg: "model", Required: true},
			Voice:  FieldSpec{Arg: "voice"},
			Params: ParamsSettings,
		},
		Notes: []string{"OpenAI Live (gpt-live-1): full-duplex speech to speech; tools and reasoning run on the think entry the live entry names, on OpenAI's Responses API inside the same live session"},
	},
}
