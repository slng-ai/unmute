package spec

// RealtimeDef is one entry of `models.realtime`, the model an
// `architecture: realtime` package runs on: one model on a vendor's realtime
// API, hearing the caller's audio and answering without a transcriber or a
// synthesizer in between.
//
// A list for the reason LiveDef is one, and a struct of exactly the legal
// fields so the strict decoder refuses everything else by name, with its line
// and its column.
//
// It is a separate section from `models.live` rather than a flag on one,
// because the two APIs differ in the one way that decides what a package may
// carry. A realtime session is mutable while the call runs:
// livekit-plugins-openai 1.8.1 declares `mutable_instructions=True,
// mutable_chat_context=True, mutable_tools=True` on this model, and the
// framework has an `_update_session(instructions=, chat_ctx=, tools=)` to use
// them. A live session declares all three False. So the restrictions a live
// package carries are not this one's, and folding the two together would mean
// writing the stricter list for both and never being able to lift it for one.
type RealtimeDef struct {
	Name     string `json:"name" yaml:"name"`
	Provider string `json:"provider" yaml:"provider"`
	Model    string `json:"model" yaml:"model"`
	// Voice is the voice the model speaks in, passed through as written. Write
	// it, or give the agent a `speak:` binding for the half cascade, where the
	// model listens and thinks and a synthesizer of your choosing speaks. Both
	// at once is refused: they answer the same question two ways, and the
	// framework only reads one of them.
	Voice string `json:"voice,omitempty" yaml:"voice,omitempty"`
	// TurnDetection is who decides the caller has finished. See
	// TurnDetectionValues for the three answers and what each lowers to.
	//
	// Omitted leaves the vendor's own default in place, and nothing is written
	// here in its stead, because writing one would send a setting the author did
	// not ask for. On livekit-plugins-openai 1.8.1 that default is *semantic*
	// VAD, not server VAD: utils.py:36-41 builds
	// `SemanticVad(eagerness="medium", create_response=True,
	// interrupt_response=True)` and uses it whenever the argument is not given.
	// This comment said server VAD until it was read off the wheel.
	TurnDetection string `json:"turn_detection,omitempty" yaml:"turn_detection,omitempty"`
	Description   string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Turn detection values for a realtime entry. The model can decide the turn two
// ways, or hand the job back to the framework's own detector, which is the same
// one a cascaded package uses.
const (
	// TurnDetectionServerVAD is the vendor's voice activity detector: the turn
	// ends on a silence window. Cheapest and the least aware of what was said.
	TurnDetectionServerVAD = "server_vad"
	// TurnDetectionSemantic is the vendor's semantic detector: the model judges
	// whether the caller has finished a thought rather than only gone quiet.
	TurnDetectionSemantic = "semantic"
	// TurnDetectionLocal hands the turn back to the framework, so the package's
	// own turn settings decide it exactly as they do for a cascaded pipeline.
	// The vendors support this explicitly rather than by accident: LiveKit
	// publishes `can_disable_turn_detection` and logs "client-side turn-taking
	// is configured, disabling realtime server-side turn detection", and Pipecat
	// takes `turn_detection=False` with a `user_audio_preroll_secs` for it.
	TurnDetectionLocal = "local"
)

// TurnDetectionValues lists the three in the order refusals and docs name them.
// One ordered list rather than a switch per site, so a value cannot reach the
// grammar and go missing from a refusal or from the published schema.
func TurnDetectionValues() []string {
	return []string{TurnDetectionServerVAD, TurnDetectionSemantic, TurnDetectionLocal}
}
