package target

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// The code-tool dependency rule, which is SLNG's and is reproduced here so
// ir.Validate and the driver cannot disagree about it.
//
// SLNG's normalize_exact_dependencies (app/schemas/tool.py:270, read 2026-08-25)
// rejects URLs and markers, requires exactly one `==` specifier with no extras
// and no wildcard, parses the version as PEP 440, canonicalises the name, refuses
// a duplicate after canonicalisation, and returns the list sorted by canonical
// name. The server stores what it returns, so unmute has to emit that exact
// string or the body it wrote is not the body that exists.

var (
	// pep508Name is the package-name shape. Canonicalisation collapses runs of
	// -, _ and . to a single - and lowercases, which is PEP 503 and is cheap to
	// reproduce exactly.
	pep508Name = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)
	nameRuns   = regexp.MustCompile(`[-_.]+`)

	// canonicalVersion is a deliberate subset of PEP 440, not the whole grammar.
	//
	// The full normaliser is a real parser: epochs, local versions, implicit
	// post-releases, `v` prefixes, and a dozen spellings that all mean rc1. This
	// repository would have to reproduce it exactly for the emitted pin to match
	// what the server stores, and a near-miss is worse than a refusal because it
	// fails at push with a message about a version nobody typed.
	//
	// So unmute accepts versions already in normal form and refuses the rest by
	// name, telling the author what to write. That covers every pin anyone
	// actually writes and keeps the round-trip exact. If a real package ever
	// needs an epoch or a local version, this is the line to widen, deliberately.
	canonicalVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*((a|b|rc)[0-9]+)?(\.post[0-9]+)?(\.dev[0-9]+)?$`)
)

// CanonicalSlngPin checks one authored dependency and returns the exact string
// SLNG will store for it.
func CanonicalSlngPin(raw string) (string, error) {
	pin := strings.TrimSpace(raw)
	for _, token := range []string{"@", ";", "/", "\\"} {
		if strings.Contains(pin, token) {
			return "", fmt.Errorf("%q contains %q: a dependency is an exact registry pin, with no URL, no environment marker and no path", raw, token)
		}
	}
	if strings.Contains(pin, "[") {
		return "", fmt.Errorf("%q requests extras: SLNG installs the package alone, so name the extra's own distribution instead", raw)
	}
	name, version, found := strings.Cut(pin, "==")
	if !found {
		return "", fmt.Errorf("%q is not an exact pin: write one `name==version`, because a range is not reproducible and SLNG refuses it", raw)
	}
	name, version = strings.TrimSpace(name), strings.TrimSpace(version)
	// `!` is deliberately not in this set. Inside a version it means an epoch
	// (`1!2.0`), which is a version form this repository does not normalise, and
	// the message for that belongs to the version check below. A second specifier
	// written with != still carries a comma, which is caught here.
	if strings.ContainsAny(version, "=<>~,*") {
		return "", fmt.Errorf("%q is not a single exact pin: write one `name==version` with no second specifier and no wildcard", raw)
	}
	if !pep508Name.MatchString(name) {
		return "", fmt.Errorf("%q is not a package name: use letters, digits, and single -, _ or . separators", name)
	}
	if !canonicalVersion.MatchString(version) {
		return "", fmt.Errorf("%q is not a version in the form SLNG stores: write it like 2.12.5, or 1.4.0rc1, or 1.2.3.post1", version)
	}
	return strings.ToLower(nameRuns.ReplaceAllString(name, "-")) + "==" + version, nil
}

// CanonicalSlngPins checks a whole list and returns it in the order SLNG stores
// it: canonical, deduplicated by canonical name, sorted.
//
// The duplicate check is by canonical name, because `Foo_Bar` and `foo-bar` are
// one package and pinning both is a contradiction rather than a repetition.
func CanonicalSlngPins(raw []string) ([]string, error) {
	byName := make(map[string]string, len(raw))
	for _, entry := range raw {
		canonical, err := CanonicalSlngPin(entry)
		if err != nil {
			return nil, err
		}
		name, _, _ := strings.Cut(canonical, "==")
		if existing, clash := byName[name]; clash {
			return nil, fmt.Errorf("%q and %q are the same package: pin it once", existing, canonical)
		}
		byName[name] = canonical
	}
	pins := make([]string, 0, len(byName))
	for _, canonical := range byName {
		pins = append(pins, canonical)
	}
	slices.Sort(pins)
	return pins, nil
}
