package target

// LiveKit live entries: one model that listens, thinks and speaks, standing
// where the transcriber, the model and the synthesizer stood. Same contract as
// catalog_livekit.go (the livekit_v1 driver templates, livekit-agents ==1.8.1).
//
// 1.8.1 is the floor for this row and the reason the support window moved: the
// module did not exist in 1.7.0, 1.7.1 or 1.8.0, checked by downloading each
// wheel.
//
// What "Verified: 2026-09-13" on the row below covers, and what it does not.
// It covers the offline suite and the opt-in smoke suite driving the real
// GPTLiveModel with only its socket replaced, which is the bar the Pipecat live
// row set for itself. It does NOT cover a browser call: the machine this was
// written on reaches no provider, so real audio, real latency and real turn
// taking on this target are OWED. Nothing in a green suite substitutes for it,
// because the one thing a live model takes over is the turn, and the turn is
// exactly what no offline check can exercise. Whoever has a key should run
// `unmute dev --target livekit` on internal/testdata/live_model, talk to it
// through a greeting, a tool call, an interruption and a silence past the nudge
// window, and replace this paragraph with what happened.
//
// The driver adds the one argument this table cannot express: where the model's
// delegated work runs. That is `delegation=` plus a `responses_options` dict
// built from the think entry the live entry names, which is an expression over
// a second binding rather than a field of this one, so it is a driver fact the
// way the Pipecat delegation is.
var livekitLiveCatalog = []Entry{
	{
		// livekit-plugins-openai 1.8.1
		// livekit/plugins/openai/realtime/gpt_live_model.py: GPTLiveModel takes
		// model, voice, delegation, responses_options and api_key. It is an
		// llm.DuplexModel, and AgentSession wraps one in DuplexRealtimeAdapter
		// itself (agent_session.py:621), so it is passed as llm= with no adapter
		// of ours.
		//
		// Its declared capabilities are mutable_chat_context=False and
		// mutable_instructions=False, which is what holds an architecture: live
		// package to one agent with no tasks: there is no moment at which the
		// instructions could change. Read 2026-09-13 off the installed wheel.
		Framework: LiveKit, Role: Live, Vendor: "openai",
		Verified: "2026-09-13", Docs: "https://docs.livekit.io/agents/models/realtime/plugins/gpt-live/",
		Install: InstallSpec{Extra: "openai"},
		Import:  "from livekit.plugins.openai.realtime import GPTLiveModel",
		Call: &CallSpec{
			Class: "GPTLiveModel", APIKeyArg: "api_key", APIKeyEnv: "OPENAI_API_KEY",
			Model:  FieldSpec{Arg: "model", Required: true},
			Voice:  FieldSpec{Arg: "voice"},
			Params: ParamsKwargs,
		},
		Notes: []string{"OpenAI Live (gpt-live-1): full-duplex speech to speech; tools and reasoning run on the think entry the live entry names as its backend, on OpenAI's Responses API inside the same live session"},
	},
}
