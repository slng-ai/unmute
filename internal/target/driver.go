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

// driverVersions is the framework version window each shipped driver's
// templates compile against. Both windows are the same today; they are separate
// entries so a divergence is a data change rather than a second checker.
var driverVersions = map[Provider][2]int{
	LiveKit: {1, 5}, // beta.workflows TaskGroup + AgentTask + inference, from 1.5.x
	Pipecat: {1, 5},
}

// CheckVersion rejects a framework version outside a driver's
// template-compatible range. A provider with no shipped driver has no template
// range to be outside of, so it is not checked here.
func CheckVersion(provider Provider, version string) error {
	bounds, ok := driverVersions[provider]
	if !ok {
		return nil
	}
	major, minMinor := bounds[0], bounds[1]
	if version == "" {
		return fmt.Errorf("%s target requires a framework version", provider)
	}
	match := versionPattern.FindStringSubmatch(version)
	if match == nil {
		return fmt.Errorf("%s version %q is not a semantic version", provider, version)
	}
	gotMajor, _ := strconv.Atoi(match[1])
	gotMinor, _ := strconv.Atoi(match[2])
	if gotMajor != major || gotMinor < minMinor {
		return fmt.Errorf("%s version %q is outside the driver's template-compatible range (>=%d.%d, <%d.0)", provider, version, major, minMinor, major+1)
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

// PinFloors is the pinnable package set for a driver, with each one's catalogue
// floor. Empty for a driver that pins nothing.
func PinFloors(provider Provider) map[string]string {
	if provider != LiveKit {
		return nil
	}
	floors := DefaultCatalog().Packages(LiveKit)
	floors["livekit-plugins-silero"] = ">=1.6.1" // always emitted (session VAD)
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
