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
type SupportWindow struct {
	Floor    string // oldest supported, inclusive
	Ceiling  string // newest verified, inclusive
	Verified string // ISO date the ceiling was verified by live call
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
	LiveKit: {Floor: "1.6.10", Ceiling: "1.6.10", Verified: "2026-08-16"},
	Pipecat: {Floor: "1.7.0", Ceiling: "1.7.0", Verified: "2026-08-16"},
}

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
const SileroFloor = ">=1.6.1"

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
