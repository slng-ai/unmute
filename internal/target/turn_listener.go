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

// TurnSwitch says how a listening vendor's own turn detection is switched on.
// There are three, and the first one was the only one for as long as the table
// held two vendors, which is why it used to be assumed rather than recorded.
type TurnSwitch string

const (
	// SwitchClass is a different class entirely: DeepgramFluxSTTService where
	// DeepgramSTTService stood. The class, its import and its verification date
	// are swapped; everything else still comes from the ordinary catalogue entry.
	SwitchClass TurnSwitch = "class"
	// SwitchArg keeps the vendor's ordinary class and adds one flat constructor
	// argument, the way Gradium takes enable_turn_detection=True.
	SwitchArg TurnSwitch = "argument"
	// SwitchSetting keeps the ordinary class and writes one Settings field, the
	// way Speechmatics takes turn_detection_mode. A setting is the only switch
	// that also has something to say on the LOCAL path, because a setting has a
	// default and a default can change underneath a package (LocalValue).
	SwitchSetting TurnSwitch = "setting"
)

// ListenTurnDetector is how a listening vendor's service is asked to end the
// caller's turn. Selecting it is what `turn: provider: listen` does on Pipecat;
// the ordinary listen catalogue entry still supplies the extra, the key and the
// language slot.
//
// This is the second place the compiler reads a model id (the first is the
// LiveKit turn detector, LiveKitTurnVersion), and for the same reason: the
// driver loads a class by it. A Flux model on the ordinary Deepgram service, or
// a Nova model on the Flux service, is a connection the vendor refuses on the
// first packet, so the family is checked here rather than on a live call.
//
// Three of these fields are answers a row may decline to give. A vendor that
// exposes no end-of-turn timeout leaves CeilingArg empty and `pace` is refused
// on it; a vendor that reports only turns that have ended leaves Eager false and
// `eager: true` is refused. Both refusals read the row, so a vendor added later
// gets them without anyone remembering to write them.
type ListenTurnDetector struct {
	Vendor string
	// Switch says which of the three shapes below applies. The fields that do
	// not belong to a row's switch are empty, and a test holds that.
	Switch TurnSwitch
	// Class and Import are the turn-detecting class, on a SwitchClass row only.
	Class  string
	Import string
	// EnableArg is the constructor argument (SwitchArg) or Settings field
	// (SwitchSetting) that turns the vendor's own detection on, and EnableValue
	// is what it takes when the transcriber decides the turn.
	EnableArg   string
	EnableValue string
	// LocalValue is what that same key takes when the LOCAL detector decides.
	// Empty for every row but one: it exists because Speechmatics' default
	// flipped from "the caller drives turns" to "the service does" in pipecat
	// 1.10.0, so a package that says nothing about turns changes behaviour on a
	// bump unless the old answer is pinned back. Legal only on SwitchSetting.
	LocalValue string
	// ModelPrefixes are the model families the turn path serves, matched on the
	// front of the id; ExcludedModels are ids sharing a prefix that belong to
	// the ordinary transcriber instead. ExampleModel is what a refusal names.
	// An empty ModelPrefixes means every model the vendor serves can decide.
	ModelPrefixes  []string
	ExcludedModels []string
	ExampleModel   string
	// CeilingArg is the Settings field that takes the pace ceiling, in
	// milliseconds: how long after the caller stops the service closes the turn
	// whatever its own confidence says. It is the same thing the local
	// analyzer's stop_secs is, spelled by the vendor. Empty where the vendor
	// exposes no such field, and `pace` is then refused rather than landing on
	// the nearest-looking setting, which would make one authored word mean two
	// different things per vendor.
	CeilingArg string
	// Eager says the service can predict an end of turn before it commits one,
	// which is what `eager: true` answers early. The two class-swap rows can;
	// the two added in 1.10.0 report a turn that has already ended.
	Eager bool
	// Verified is when the class, its constructor and its Settings were read in
	// the pinned framework's source.
	Verified string
}

// SwapsClass reports whether this vendor is reached through a different class.
func (d ListenTurnDetector) SwapsClass() bool { return d.Switch == SwitchClass }

// HasCeiling reports whether a pace ceiling has somewhere to land on this
// vendor. A row without one refuses `pace`.
func (d ListenTurnDetector) HasCeiling() bool { return d.CeilingArg != "" }

// speechmaticsTurnMode spells one of the modes the way the emitted bot writes
// it. The enum is re-exported on the service class (1.10.0 stt.py,
// `TurnDetectionMode = TurnDetectionMode` inside SpeechmaticsSTTService), so
// this needs no second import in the generated module.
func speechmaticsTurnMode(mode string) string {
	return "SpeechmaticsSTTService.TurnDetectionMode." + mode
}

// listenTurnDetectors is the one recorded home for these facts (Principle III).
// Validation refuses by it, the driver constructs from it, and the docs-site
// table is held to it.
var listenTurnDetectors = map[Provider]map[string]ListenTurnDetector{
	Pipecat: {
		// pipecat-ai 1.10.0 services/deepgram/flux/stt.py: DeepgramFluxSTTService
		// takes enable_eager_end_of_turn and recommends EagerUserTurnStrategies
		// when it is on; Settings carries eot_timeout_ms ("time in ms after
		// speech to finish a turn regardless of EOT confidence", default 5000).
		"deepgram": {
			Vendor: "deepgram", Switch: SwitchClass, Class: "DeepgramFluxSTTService",
			Import:        "from pipecat.services.deepgram.flux.stt import DeepgramFluxSTTService",
			ModelPrefixes: []string{"flux-"}, ExampleModel: "flux-general-en",
			CeilingArg: "eot_timeout_ms", Eager: true, Verified: "2026-09-12",
		},
		// pipecat-ai 1.10.0 services/cartesia/turns/stt.py: CartesiaTurnsSTTService
		// takes the same flag; Settings carries turn_end_timeout_ms
		// ("milliseconds to wait after the user stops speaking"). Its models are
		// the ink family, and ink-whisper is the ordinary transcriber's.
		"cartesia": {
			Vendor: "cartesia", Switch: SwitchClass, Class: "CartesiaTurnsSTTService",
			Import:         "from pipecat.services.cartesia.turns.stt import CartesiaTurnsSTTService",
			ModelPrefixes:  []string{"ink-"},
			ExcludedModels: []string{"ink-whisper"}, ExampleModel: "ink-2",
			CeilingArg: "turn_end_timeout_ms", Eager: true, Verified: "2026-09-12",
		},
		// pipecat-ai 1.10.0 services/gradium/stt.py: GradiumSTTService gained
		// enable_turn_detection, a flat constructor argument. With it on, the
		// server's end-pointing signal drives turns and the service recommends
		// ExternalUserTurnStrategies. Its three tuning settings (eot_horizon_s,
		// eot_threshold, post_flush_cooldown_frames) are left as ordinary
		// params: none of them is a timeout, so none of them is the ceiling.
		// eot_horizon_s is which prediction horizon to READ, in seconds.
		"gradium": {
			Vendor: "gradium", Switch: SwitchArg,
			EnableArg: "enable_turn_detection", EnableValue: "True",
			ExampleModel: "default", Eager: false, Verified: "2026-09-12",
		},
		// pipecat-ai 1.10.0 services/speechmatics/stt.py: the service moved to
		// Agent STT and turn_detection_mode's DEFAULT FLIPPED, from EXTERNAL
		// (the caller drives turns, which is Pipecat's own detector) to VAD (the
		// service closes turns itself). In EXTERNAL it emits no turn frames and
		// subscribes to no turn events; in VAD it does both.
		//
		// So this row is written in both directions. Under a local decider the
		// old answer is pinned back, or a package that says nothing about turns
		// silently changes who ends them on a version bump. Under a listening
		// decider the new default is what the author asked for, and it is still
		// written rather than left implicit, because a default that moved once
		// can move again.
		"speechmatics": {
			Vendor: "speechmatics", Switch: SwitchSetting,
			EnableArg:    "turn_detection_mode",
			EnableValue:  speechmaticsTurnMode("VAD"),
			LocalValue:   speechmaticsTurnMode("EXTERNAL"),
			ExampleModel: "linden-1", Eager: false, Verified: "2026-09-12",
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

// ServesModel reports whether the turn path serves a model id: it starts with
// one of the family prefixes and is not one of the ids the ordinary transcriber
// owns.
//
// A row with no prefixes serves every model the vendor serves. That is the
// honest answer for a vendor switched on by an argument or a setting rather than
// by a class: there is no second service with its own model list to get wrong,
// so there is nothing here to check. Excluded ids are still honoured, so a row
// can carve one out later without gaining a prefix it does not have.
func (d ListenTurnDetector) ServesModel(model string) bool {
	if slices.Contains(d.ExcludedModels, model) {
		return false
	}
	if len(d.ModelPrefixes) == 0 {
		return true
	}
	for _, prefix := range d.ModelPrefixes {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}
