package target

// LiveKit realtime entries: one model on OpenAI's Realtime API, hearing the
// caller's audio and answering it. Same contract as catalog_livekit.go (the
// livekit_v1 driver templates, livekit-agents ==1.8.1).
//
// This is a different class and a different section from `models.live`, and the
// difference is the one that decides what a package may carry: this model
// declares mutable_instructions, mutable_chat_context and mutable_tools, where
// the live model declares all three False.
// livekit-plugins-openai 1.8.1 realtime_model.py:488-492, read off the
// installed wheel 2026-09-13.
//
// What "Verified" on the row below covers: the offline suite reading the
// emitted module. It does NOT cover a browser call. Real audio, real latency
// and real turn taking on this target are OWED, and the turn is exactly the
// thing this architecture lets an author move, so no offline check stands in
// for it. Whoever has a key should run `unmute dev --target livekit` on
// internal/testdata/realtime_model, at each of the three turn_detection values,
// and replace this paragraph with what happened.
//
// Two arguments this table cannot express are attached by the driver, because
// each is an expression over more than one field of the package:
//
//   - `turn_detection`, which is resolved through ResolveTurnDetection and is
//     emitted as *nothing at all* for `local`. See turn_detection.go.
//   - `modalities`, which is decided by whether the agent binds `speak:`: the
//     half cascade asks the model for text and lets a synthesizer speak it.
var livekitRealtimeCatalog = []Entry{
	{
		// livekit-plugins-openai 1.8.1
		// livekit/plugins/openai/realtime/realtime_model.py:385-414: every
		// argument is keyword-only and nothing is required except an api_key
		// that resolves (realtime_model.py:501-506). model defaults to
		// "gpt-realtime" and voice to "marin" (DEFAULT_VOICE,
		// realtime_model.py:123); both are written out here because a vendor
		// default is not an answer this package ever states.
		//
		// It is an llm.RealtimeModel, which AgentSession takes as llm= with no
		// adapter of ours.
		//
		// Two arguments are deliberately absent from this row and must never be
		// emitted:
		//   - temperature, which the constructor accepts and silently discards
		//     ("deprecated, unused in v1", realtime_model.py:382).
		//   - api_version, which flips the client into Azure mode
		//     (realtime_model.py:466-476) and then demands an Azure endpoint.
		//
		// Neither is reachable from a package today: spec.RealtimeDef carries no
		// Params field, so `params:` on a realtime entry is refused at decode
		// with its line and column. They are named here because that is the only
		// thing standing between an author and a setting that reaches nothing —
		// temperature is accepted by the constructor and thrown away, and a
		// stray OPENAI_API_VERSION in a container is enough for the second — so
		// a future row that opens params up needs to know before it does.
		//
		// The Params value below is ParamsKwargs for the same reason every other
		// LiveKit row carries one: it describes where a param WOULD land, and
		// the golden renders it. It is unreachable, not wrong.
		Framework: LiveKit, Role: Realtime, Vendor: "openai",
		Verified: "2026-09-13", Docs: "https://docs.livekit.io/agents/models/realtime/plugins/openai/",
		Install: InstallSpec{Extra: "openai"},
		Import:  "from livekit.plugins.openai.realtime import RealtimeModel",
		Call: &CallSpec{
			Class: "RealtimeModel", APIKeyArg: "api_key", APIKeyEnv: "OPENAI_API_KEY",
			Model:  FieldSpec{Arg: "model", Required: true},
			Voice:  FieldSpec{Arg: "voice"},
			Params: ParamsKwargs,
		},
		Notes: []string{"OpenAI Realtime (gpt-realtime): one model listens, thinks and speaks; tools run natively over the same socket, and the author chooses who decides the turn"},
	},
}
