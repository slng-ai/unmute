package spec

// RealtimeDef is one entry of `models.realtime`: a model that listens, thinks
// and speaks as one, in place of a listen, a think and a speak binding.
//
// A list rather than a name-keyed map like its four siblings, because a new
// map-typed authored field is refused (no_dictionaries_test.go), and because a
// struct of exactly the legal fields lets the strict decoder do the refusing:
// `temperature`, `language`, `speed`, `params` and the rest are refused with the
// file, the line and the column, and no validate rule has to list them.
//
// `think` names a `models.think` entry that runs the model's tools and hard
// reasoning while the live model keeps talking. Without one the live model
// declines every request that would need a tool, so an agent with tools has to
// name one. Both the live model and its backend are fixed when the session
// starts, which is why the compiler holds a realtime package to one agent.
type RealtimeDef struct {
	Name        string `json:"name" yaml:"name"`
	Provider    string `json:"provider" yaml:"provider"`
	Model       string `json:"model" yaml:"model"`
	Voice       string `json:"voice,omitempty" yaml:"voice,omitempty"`
	Think       string `json:"think,omitempty" yaml:"think,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}
