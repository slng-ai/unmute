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
	// 1.8.7 is the line unmute-core needs: it brings OpenTelemetry 1.45 and
	// openai 3, which the library builds on. GPT-Live (architecture: live)
	// first shipped in 1.8.1, so no live package loses anything here.
	//
	// Verified on 2026-10-17 by the offline suite and the opt-in smoke suite
	// running every emitted LiveKit module against the real 1.8.7 wheels, with
	// the same smoke tests run on main at 1.8.1 first so a failure could be
	// attributed. What 1.8.7 changed in the emitted code, and why:
	//
	//  - livekit-agents and livekit-plugins-openai moved to openai 3, which is
	//    built on httpx2 and no longer pulls httpx. The SLNG router client is an
	//    httpx2 client now, like the plugin's own (llm.py:22).
	//  - Startup spans sit under a new session_start span, and a handoff under
	//    update_agent, both children of agent_session (agent_session.py:1015,
	//    1888). The smoke suite now holds that shape.
	//  - livekit-plugins-speechmatics 1.8.2 moved to Agent STT and drops ten
	//    settings with only a log warning. retired_params.go refuses them, and
	//    the catalogue row takes model: in place of operating_point.
	//
	// Checks OWED rather than made:
	//
	//  1. A browser call on salon-concierge. The machine that did this reaches
	//     no provider. Replace this item with the call's counts.
	//  2. Tools still running when the interruption timeout fires are no longer
	//     cancelled: they keep running and deliver a deferred reply
	//     (agent_activity.py:3959-3990). The terminal-tool guard was written for
	//     a cancellation. Interrupt a booking mid-call and read the trace.
	//  3. PII redaction, unchanged from 1.8.1: set_tracer_provider still calls
	//     _install_pii_redaction (telemetry/traces.py:695) on the provider WE
	//     build. LIVEKIT_TELEMETRY_ALLOW_PII=0 would strip the conversation out
	//     of our Langfuse and Coval spans with no error, and nothing asserts the
	//     default.
	//  4. Worker memory on Linux. The worker now preloads the local turn
	//     detector's weights (ipc/_preload.py) unless
	//     LIVEKIT_AGENTS_PRELOAD_LOCAL_INFERENCE=0. Check the deployed worker's
	//     memory against its plan before the first busy day.
	//
	// `metrics_collected` is still deprecated and still prints a warning per
	// registration. It is not owed by this bump: moving off it changes what
	// every traced span carries, so it is its own change. The eleven
	// AgentSession keywords marked for removal in v2.0.0 are not owed either:
	// the emitted module passes TurnHandlingOptions and none of the old ones.
	LiveKit: {Floor: "1.8.7", Ceiling: "1.8.7", Verified: "2026-10-17"},
	// 1.12.0 is the line unmute-core needs. Verified on 2026-10-17 by the
	// offline suite and the opt-in smoke suite running every emitted Pipecat
	// module against the real 1.12.0 wheel, pipecat-slng 0.7.0 beside it, with
	// the same smoke tests run on main at 1.10.0 first. What 1.11 and 1.12
	// changed in the emitted code, and why:
	//
	//  - UserBotLatencyObserver sees only a frame's first push now
	//    (user_bot_latency_observer.py:580). The dev page's playback observer
	//    watches every push itself, because the transport's TTSStartedFrame is
	//    a repeat, and hands the framework only first pushes.
	//  - An empty user turn now adds a "please repeat" developer message and
	//    runs the model by default (llm_response_universal.py:185). It is
	//    switched off, so the model sees the same conversation as on LiveKit.
	//  - Pipecat splits sentences with sentencex and no longer imports nltk, so
	//    the Dockerfile no longer bakes NLTK's tokenizer.
	//  - Speechmatics' default turn mode went back to EXTERNAL in 1.11.0. The
	//    compiler writes the mode either way (turn_listener.go).
	//
	// Checks OWED rather than made:
	//
	//  1. A browser call on salon-concierge. The machine that did this reaches
	//     no provider. Replace this item with the call's counts.
	//  2. MCP moved to the 2.x SDK. A failed MCP call now returns the real error,
	//     cut to 200 characters, instead of a fixed apology line, so the model
	//     sees different text. Read one failed MCP call in a trace.
	//  3. The base image's certificate set, carried from 1.10.0: openai 3 checks
	//     TLS against the operating system's trust store. The first real
	//     deployment settles it.
	Pipecat: {Floor: "1.12.0", Ceiling: "1.12.0", Verified: "2026-10-17"},
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
// with the framework and declare it: livekit-plugins-silero 1.8.7 requires
// livekit-agents>=1.8.7. A floor left at an older release still resolved, since
// the newest plugin wins and an old one cannot pair with new agents anyway, but
// it stated a requirement that was no longer true.
const SileroFloor = ">=1.8.7"

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
