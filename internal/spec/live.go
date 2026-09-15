package spec

// LiveDef is one entry of `models.live`, the model an `architecture: live`
// package runs on: it hears the caller's audio, decides what to say and when,
// and speaks in its own voice, full duplex, deciding for itself when to yield.
//
// A list rather than a name-keyed map like the cascade's four sections, because
// a new map-typed authored field is refused (no_dictionaries_test.go), and
// because a struct of exactly the legal fields lets the strict decoder do the
// refusing: `temperature`, `language`, `speed`, `params` and the rest are
// refused with the file, the line and the column, and no validate rule has to
// list them.
//
// Everything about a live session is fixed when it starts. The vendor says so
// itself: livekit-plugins-openai 1.8.1 declares the model's capabilities as
// `mutable_instructions=False, mutable_chat_context=False`, and pipecat-ai
// 1.10.0 says model, voice and system_instruction "are fixed once the session
// has started". That is the whole reason an `architecture: live` package is held
// to one agent with no tasks and no handoffs: there is no moment at which the
// instructions could change. `architecture: realtime` is the shape for a package
// that needs them to.
type LiveDef struct {
	Name     string `json:"name" yaml:"name"`
	Provider string `json:"provider" yaml:"provider"`
	Model    string `json:"model" yaml:"model"`
	// Voice is the voice the model speaks in, passed through as written. A live
	// model always speaks for itself: there is no half cascade here the way
	// `architecture: realtime` has one, because a live session has no text-only
	// output mode to put a synthesizer behind.
	Voice string `json:"voice,omitempty" yaml:"voice,omitempty"`
	// Backend names a `models.think` entry the live model hands its tools and
	// its harder reasoning to, while it keeps talking. It names an existing
	// think entry rather than carrying its own model and instructions inline,
	// because a think entry already carries provider, model, `prompt_suffix` and
	// per-target overrides, and a second inline model shape would drift from it.
	//
	// Omitted means the vendor's client delegation, where the work comes back to
	// the application instead of running on a backend model. The two frameworks
	// differ on what that is worth: Pipecat's ClientDelegation wraps a worker
	// that does run this project's tools, while LiveKit's sets
	// `mutable_tools=False` and its own docstring says "no framework tool can
	// answer" it.
	//
	// An agent with tools and no backend is nonetheless refused on BOTH, and
	// deliberately: one authored package compiling to two different answers
	// about whether its tools run is worse than a refusal an author can act on
	// in one line. internal/ir/validate_live_test.go records the decision.
	Backend     string `json:"backend,omitempty" yaml:"backend,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}
