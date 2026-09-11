package target

import (
	"maps"
	"slices"
	"strings"
)

// Who ends the caller's turn. A turn binding's `provider:` names one of these
// two; every other spelling on Pipecat is refused by the driver, and LiveKit
// has its own vocabulary (turn-detector-mini, turn-detector).
const (
	// TurnDeciderLocal is the on-device pair Unmute has always emitted: Silero
	// voice activity plus the framework's own end-of-turn analyzer.
	TurnDeciderLocal = "local"
	// TurnDeciderListen hands the decision to the listening model's own turn
	// detection. Only a transcriber that detects turns itself can take it, and
	// the table below is the list of those.
	TurnDeciderListen = "listen"
)

// ListenTurnDetector is the turn-detecting service a listening vendor ships,
// which is a different class from the vendor's ordinary transcriber. Selecting
// it is what `turn: provider: listen` does on Pipecat; the ordinary listen
// catalogue entry still supplies the extra, the key and the language slot.
//
// This is the second place the compiler reads a model id (the first is the
// LiveKit turn detector, LiveKitTurnVersion), and for the same reason: the
// driver loads a class by it. A Flux model on the ordinary Deepgram service, or
// a Nova model on the Flux service, is a connection the vendor refuses on the
// first packet, so the family is checked here rather than on a live call.
type ListenTurnDetector struct {
	Vendor string
	Class  string
	Import string
	// ModelPrefixes are the model families the class serves, matched on the
	// front of the id; ExcludedModels are ids sharing a prefix that belong to
	// the ordinary transcriber instead. ExampleModel is what a refusal names.
	ModelPrefixes  []string
	ExcludedModels []string
	ExampleModel   string
	// CeilingArg is the Settings field that takes the pace ceiling, in
	// milliseconds: how long after the caller stops the service closes the turn
	// whatever its own confidence says. It is the same thing the local
	// analyzer's stop_secs is, spelled by the vendor.
	CeilingArg string
	// Eager says the service can predict an end of turn before it commits one,
	// which is what `eager: true` answers early. Both rows can; the field exists
	// so a vendor that detects turns without predicting them can join the table
	// without pretending.
	Eager bool
	// Verified is when the class, its constructor and its Settings were read in
	// the pinned framework's source.
	Verified string
}

// listenTurnDetectors is the one recorded home for these facts (Principle III).
// Validation refuses by it, the driver constructs from it, and the docs-site
// table is held to it.
var listenTurnDetectors = map[Provider]map[string]ListenTurnDetector{
	Pipecat: {
		// pipecat-ai 1.9.0 services/deepgram/flux/stt.py: DeepgramFluxSTTService
		// takes enable_eager_end_of_turn and recommends EagerUserTurnStrategies
		// when it is on; Settings carries eot_timeout_ms ("time in ms after
		// speech to finish a turn regardless of EOT confidence", default 5000).
		"deepgram": {
			Vendor: "deepgram", Class: "DeepgramFluxSTTService",
			Import:        "from pipecat.services.deepgram.flux.stt import DeepgramFluxSTTService",
			ModelPrefixes: []string{"flux-"}, ExampleModel: "flux-general-en",
			CeilingArg: "eot_timeout_ms", Eager: true, Verified: "2026-09-11",
		},
		// pipecat-ai 1.9.0 services/cartesia/turns/stt.py: CartesiaTurnsSTTService
		// takes the same flag; Settings carries turn_end_timeout_ms
		// ("milliseconds to wait after the user stops speaking"). Its models are
		// the ink family, and ink-whisper is the ordinary transcriber's.
		"cartesia": {
			Vendor: "cartesia", Class: "CartesiaTurnsSTTService",
			Import:         "from pipecat.services.cartesia.turns.stt import CartesiaTurnsSTTService",
			ModelPrefixes:  []string{"ink-"},
			ExcludedModels: []string{"ink-whisper"}, ExampleModel: "ink-2",
			CeilingArg: "turn_end_timeout_ms", Eager: true, Verified: "2026-09-11",
		},
	},
}

// LookupListenTurnDetector returns the turn-detecting service for a listening
// vendor on a target, and false where the vendor has none or the target lets no
// transcriber decide.
func LookupListenTurnDetector(provider Provider, vendor string) (ListenTurnDetector, bool) {
	detector, ok := listenTurnDetectors[provider][vendor]
	return detector, ok
}

// ListenTurnDetectorVendors lists the vendors whose transcriber can decide the
// turn on a target, sorted, so a refusal and a docs table read one list.
func ListenTurnDetectorVendors(provider Provider) []string {
	return slices.Sorted(maps.Keys(listenTurnDetectors[provider]))
}

// ListenTurnDetectors returns every row for a target, by vendor, for the tests
// and the docs-site gate.
func ListenTurnDetectors(provider Provider) map[string]ListenTurnDetector {
	return maps.Clone(listenTurnDetectors[provider])
}

// ServesModel reports whether the turn-detecting class serves a model id: it
// starts with one of the family prefixes and is not one of the ids the ordinary
// transcriber owns.
func (d ListenTurnDetector) ServesModel(model string) bool {
	if slices.Contains(d.ExcludedModels, model) {
		return false
	}
	for _, prefix := range d.ModelPrefixes {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}
