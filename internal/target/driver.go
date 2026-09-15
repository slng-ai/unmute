package target

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Driver facts that two stages ask about.
//
// Each check below used to live only inside a generator, which meant a package
// could pass `unmute validate` at exit 0 and fail `unmute compile` at exit 1 on
// a value the author wrote — after validate had already said the package was
// fine. Eight of those were measured (reproduction.md section E) and all eight
// are mirrored into ir.Validate. The generators keep their own errors as a
// backstop; what moved here is the **fact** each one checks against, so there is
// one copy of it rather than two (Principle III).
//
// The wording is unchanged from what the generators printed, so an author who
// hit one before recognises it now.

// versionPattern accepts a leading semantic triple, with minor and patch
// optional, and ignores any suffix.
var versionPattern = regexp.MustCompile(`^(\d+)(?:\.(\d+))?(?:\.(\d+))?`)

// exactVersionPattern is the whole of what a target may declare: three numbers,
// nothing else. A declared version is an exact install pin, so half a version
// (`1.6`) or a prerelease suffix (`1.6.11rc1`) is rejected rather than resolved
// into something the author did not write.
var exactVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// SupportWindow records the one framework version an unmute release supports
// for a shipped driver, plus the date it was verified end to end. Floor and
// Ceiling stay separate for compile-report schema compatibility and must match.
//
// The ceiling is a claim about verification, not about upstream. A newer
// release upstream is unsupported until a human proves it and a new unmute
// ships, which is what ties the supported version to the unmute version.
//
// Verified is the date of the newest verification, and the comment beside each
// row says what that verification was: a browser call on a shipped example is
// the whole answer, and anything less (the offline suite, the smoke suite on
// the real wheel, a source diff) is named as what it is, with the call still
// owed. A date with no comment would let a suite run read as a call.
type SupportWindow struct {
	Floor    string // oldest supported, inclusive
	Ceiling  string // newest verified, inclusive
	Verified string // ISO date of the verification the row's comment describes
}

// supportWindows is the one recorded home for these facts (Principle III).
// Validation, both drivers, the scaffold default, the compile report, and the
// version output all read it; none of them keeps a copy. Raising a ceiling is
// the whole of a routine framework upgrade: change it here, regenerate the
// derived output, verify by live call, ship.
//
// Each runtime intentionally supports one tested SDK version rather than
// claiming a compatibility range the release matrix does not exercise.
var supportWindows = map[Provider]SupportWindow{
	// 1.8.1 is the first release carrying the GPT-Live model
	// (livekit/plugins/openai/realtime/gpt_live_model.py), which is why this
	// moved: architecture: live cannot compile on this target below it. Checked
	// by downloading the 1.7.0, 1.7.1 and 1.8.0 wheels, none of which contains
	// the module.
	//
	// Verified on 2026-09-13 by two things short of a call: the offline suite,
	// and the opt-in smoke suite running every emitted LiveKit module against
	// the real 1.8.1 wheel, livekit-plugins-slng 1.8.1 beside it, with the same
	// suite green on 1.6.10 first so a failure could be attributed.
	//
	// Two checks are OWED rather than made:
	//
	//  1. A browser call. The machine that did this reaches no provider. The
	//     1.6.10 row carried a bare date and no comment, so what closed it is
	//     not recorded; make the call on salon-concierge and replace this
	//     paragraph with its counts.
	//  2. PII redaction, which is new here. 1.8.1's set_tracer_provider also
	//     calls _install_pii_redaction (telemetry/traces.py:581), which prepends
	//     a filtering span processor to the provider WE build. The default
	//     allows, so nothing changes today, but LIVEKIT_TELEMETRY_ALLOW_PII=0
	//     would now strip the conversation out of our Langfuse and Coval spans
	//     with no error anywhere. Absent at 1.6.10. Nothing asserts the default,
	//     and a run with that variable set would pass every gate we have.
	//
	// `metrics_collected` is deprecated and the emitted module subscribes to it
	// in four places (agent.py, tracing.py, tracing_coval.py, dev_metrics.py),
	// which prints a warning per registration: two on a deployed traced run,
	// three under `unmute dev`. It is NOT owed by this bump and this row is not
	// where it gets fixed: the deprecation block is byte-identical in 1.6.10
	// (agent_session.py:717-723), so it has been printing all along. Moving off
	// it changes what every traced span carries, so it is its own change with
	// its own verification. Pipecat has a gate refusing deprecated shapes in
	// emitted code (pipecat_deprecations_test.go); LiveKit has none, and this is
	// the first thing it would catch.
	//
	// The deprecated AgentSession keywords are NOT owed: 1.8.1 marks eleven for
	// removal in v2.0.0 (min_endpointing_delay, allow_interruptions,
	// turn_detection and the rest) in favour of TurnHandlingOptions and
	// STTContextOptions, and the emitted module already passes the new shapes
	// and none of the old ones.
	LiveKit: {Floor: "1.8.1", Ceiling: "1.8.1", Verified: "2026-09-13"},
	// 1.10.0 verified on 2026-09-12 by three things short of a call: the offline
	// suite; the opt-in smoke suite running every emitted Pipecat module against
	// the real 1.10.0 wheel, pipecat-slng 0.5.2 beside it; and a diff of every
	// module the emitted bot imports between 1.9.0 and 1.10.0, all 24 of them
	// byte-identical (research R1 of spec 022). Only two services a package can
	// bind changed at all, and both are handled: speechmatics, whose turn mode
	// default flipped and is now pinned from the turn binding, and gradium,
	// which gained turn detection and joined the decider table.
	//
	// Two checks are OWED rather than made, and neither is a formality:
	//
	//  1. A browser call. The machine that did this reaches no provider, so the
	//     call on salon-concierge that closed 1.8.0 (13 turns, 20 interruptions,
	//     3 handoffs, 12 function calls settled, no errors) is still owed, for
	//     1.9.0 and now for this. Make it, and replace this paragraph with its
	//     counts.
	//  2. The base image's certificate set. 1.10.0 widens the OpenAI SDK pin to
	//     the 3.x line, which builds on httpx2 and verifies TLS against the
	//     operating system trust store rather than a bundled set. An image with
	//     no system certificates fails every provider call. The emitted project
	//     builds on the framework vendor's own image, which could not be
	//     inspected here: no container runtime, and the registry's blob CDN is
	//     refused by the egress proxy. The runbook names the variable that
	//     points at a bundle. The first real deployment settles it.
	Pipecat: {Floor: "1.10.0", Ceiling: "1.10.0", Verified: "2026-09-12"},
}

// LiveKitDeploymentRegions are the agent compute regions, not media region groups.
// Verified 2026-09-14: https://docs.livekit.io/deploy/admin/regions/endpoints/#agent-deployment-regions
var LiveKitDeploymentRegions = []string{"us-east", "eu-central", "ap-south"}

// frameworkPackages is the distribution each driver installs, so an error
// message and an emitted dependency name it the same way.
var frameworkPackages = map[Provider]string{
	LiveKit: "livekit-agents",
	Pipecat: "pipecat-ai",
}

// Window returns the supported framework version for a provider. A provider with
// no shipped driver has no window, and reports false.
func Window(provider Provider) (SupportWindow, bool) {
	win, ok := supportWindows[provider]
	return win, ok
}

// Windows returns every supported framework version, keyed by provider.
func Windows() map[Provider]SupportWindow {
	return maps.Clone(supportWindows)
}

// FrameworkPackage is the distribution name a driver installs ("" if none).
func FrameworkPackage(provider Provider) string { return frameworkPackages[provider] }

// CheckVersion rejects any framework version other than the one this unmute
// supports. A provider with no shipped driver has no version contract,
// so it is not checked here.
//
// The whole triple is compared, not just the minor: a ceiling of 1.6.10 is
// meaningless without the patch, and string order would get it backwards, since
// "1.6.10" sorts below "1.6.4".
func CheckVersion(provider Provider, version string) error {
	win, ok := supportWindows[provider]
	if !ok {
		return nil
	}
	if version == "" {
		return fmt.Errorf("%s target requires a framework version", provider)
	}
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("%s version %q is not a semantic version", provider, version)
	}
	if !exactVersionPattern.MatchString(version) {
		return fmt.Errorf("%s version %q must be three numbers, for example %q", provider, version, win.Ceiling)
	}
	got, _ := ParseVersion(version)
	supported, _ := ParseVersion(win.Ceiling)
	if slices.Compare(got[:], supported[:]) > 0 {
		// Named separately from the floor case: the fix is upgrading unmute, not
		// editing the package, and an author cannot guess that from a range alone.
		return fmt.Errorf("%s version %q is newer than this unmute supports (exactly %s); a newer unmute may support it",
			provider, version, win.Ceiling)
	}
	if slices.Compare(got[:], supported[:]) < 0 {
		return fmt.Errorf("%s version %q is outside the supported range (exactly %s)", provider, version, win.Ceiling)
	}
	return nil
}

// ParseVersion reads a leading semantic triple, reporting whether one is there.
func ParseVersion(v string) ([3]int, bool) {
	match := versionPattern.FindStringSubmatch(v)
	if match == nil {
		return [3]int{}, false
	}
	var out [3]int
	for i, part := range match[1:] {
		out[i], _ = strconv.Atoi(part)
	}
	return out, true
}

// SileroFloor is the constraint the always-emitted session VAD plugin carries.
// It is here rather than in the emitter so the floor validation checks and the
// floor the driver emits cannot drift apart (FR-003).
//
// It tracks the support window because livekit's plugins version in lockstep
// with the framework and declare it: livekit-plugins-silero 1.8.1 requires
// livekit-agents>=1.8.1. A floor left at an older release still resolved, since
// the newest plugin wins and an old one cannot pair with new agents anyway, but
// it stated a requirement that was no longer true.
const SileroFloor = ">=1.8.1"

// PinFloors is the pinnable package set for a driver, with each one's catalogue
// floor. Empty for a driver that pins nothing.
func PinFloors(provider Provider) map[string]string {
	if provider != LiveKit {
		return nil
	}
	floors := DefaultCatalog().Packages(LiveKit)
	floors["livekit-plugins-silero"] = SileroFloor // always emitted (session VAD)
	return floors
}

// CheckPins validates plugin pins: a pin key must be a pinnable package for this
// driver, and its value a semantic version at or above the catalogue floor. An
// unknown key fails loud, because a typo must not silently drop a pin.
func CheckPins(provider Provider, pins map[string]string) error {
	floors := PinFloors(provider)
	if len(floors) == 0 {
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(pins)) {
		floor, ok := floors[name]
		if !ok {
			return fmt.Errorf("%s pin %q is not a pinnable package; known: %s", provider, name, strings.Join(slices.Sorted(maps.Keys(floors)), ", "))
		}
		pinned, ok := ParseVersion(pins[name])
		if !ok {
			return fmt.Errorf("%s pin %s: %q is not a semantic version", provider, name, pins[name])
		}
		min, ok := ParseVersion(strings.TrimPrefix(floor, ">="))
		// Lexicographic over major/minor/patch, which is exactly semver order
		// for a parsed triple.
		if ok && slices.Compare(pinned[:], min[:]) < 0 {
			return fmt.Errorf("%s pin %s %q is below the catalogue floor %s", provider, name, pins[name], floor)
		}
	}
	return nil
}

// CheckSDKLanguage rejects an SDK language a driver has no templates for.
func CheckSDKLanguage(provider Provider, language string) error {
	if language == "" || language == "python" {
		return nil
	}
	if !EmitsProject(provider) {
		return nil
	}
	return fmt.Errorf("%s driver emits python projects only; sdk_language %q has no templates yet", provider, language)
}
