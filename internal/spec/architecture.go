package spec

// Architecture is the pipeline shape a package compiles to. It is the one key
// that says how the caller's audio reaches a model and how the reply gets back,
// and every other model section is legal or refused according to it.
//
// It is written rather than derived, even though the bindings imply it. Three
// things the derived form cannot do:
//
//   - A refusal can name the shape the author chose ("architecture: live has no
//     place for tasks") instead of re-deriving it from whichever binding
//     happened to be present, which is the difference between one sentence and
//     a run of unrelated missing-binding errors.
//   - A section belonging to another architecture is caught at the key, with its
//     line, rather than downstream as several failures that never name the cause.
//   - It is the line a reader scans for. Which pipeline a package builds is the
//     first question anybody asks of it, and a package that only implies the
//     answer makes every reader work it out again.
//
// The key is authoritative: it decides, and a binding that disagrees with it is
// refused naming the key. So the two cannot drift apart the way a second source
// of truth would.
type Architecture string

const (
	// ArchitectureCascade is the shape every package had before this key: a
	// transcriber hears the caller, a model reads the words, a synthesizer
	// speaks the reply, and a turn detector decides when the caller stopped.
	// Four services, each one swappable, each one measurable on its own.
	ArchitectureCascade Architecture = "cascade"
	// ArchitectureRealtime is one model on a vendor's realtime API, hearing the
	// caller's audio and answering in its own voice. Its instructions, context
	// and tools stay changeable while the call runs, and it will hand turn
	// taking back to the framework if asked.
	ArchitectureRealtime Architecture = "realtime"
	// ArchitectureLive is one model on a vendor's live API. It listens, thinks
	// and speaks full duplex, deciding for itself when to yield, and hands work
	// that needs a tool or harder reasoning to a backend model. Everything about
	// the session is fixed when it starts, which is what the refusals are about.
	ArchitectureLive Architecture = "live"
)

// Architectures lists every value in the order the docs and the refusals name
// them, cascade first because it is the default. One ordered list rather than a
// switch in each place that needs the set: a value missing from one hand-written
// list reads as a typo in a refusal, or vanishes from the published schema.
func Architectures() []Architecture {
	return []Architecture{ArchitectureCascade, ArchitectureRealtime, ArchitectureLive}
}

// Valid reports whether a is one of the three.
func (a Architecture) Valid() bool {
	for _, known := range Architectures() {
		if a == known {
			return true
		}
	}
	return false
}

// String renders the value as an author writes it.
func (a Architecture) String() string { return string(a) }

// Or returns a when it is set and fallback otherwise. An omitted key means
// cascade, so every package written before this key existed keeps its shape.
func (a Architecture) Or(fallback Architecture) Architecture {
	if a == "" {
		return fallback
	}
	return a
}
