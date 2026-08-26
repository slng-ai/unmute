package ir

import (
	"regexp"
	"strings"
)

// templatePattern matches one {{ token }}. The token is captured raw (not
// name-shaped) so validation can name what was actually written — a typo, or a
// secret someone tried to route through a template (V1, C4).
var templatePattern = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

// TemplateSegment is one piece of a parsed template: either literal Text or a
// Var reference, never both. Both drivers lower templates by walking these, so
// the parse lives here once rather than in each generator.
type TemplateSegment struct {
	Text string
	Var  string
}

// ParseTemplate splits a template into literal and variable segments, in order.
func ParseTemplate(value string) []TemplateSegment {
	var segments []TemplateSegment
	last := 0
	for _, match := range templatePattern.FindAllStringSubmatchIndex(value, -1) {
		if match[0] > last {
			segments = append(segments, TemplateSegment{Text: value[last:match[0]]})
		}
		segments = append(segments, TemplateSegment{Var: value[match[2]:match[3]]})
		last = match[1]
	}
	if last < len(value) {
		segments = append(segments, TemplateSegment{Text: value[last:]})
	}
	return segments
}

// TemplateRefs returns the tokens a template references, in order, deduped.
func TemplateRefs(value string) []string {
	var refs []string
	seen := make(map[string]bool)
	for _, segment := range ParseTemplate(value) {
		if segment.Var == "" || seen[segment.Var] {
			continue
		}
		seen[segment.Var] = true
		refs = append(refs, segment.Var)
	}
	return refs
}

// HasTemplate reports whether a value carries any token at all.
func HasTemplate(value string) bool { return templatePattern.MatchString(value) }

// TemplateVar returns the referenced name when the whole value is exactly one
// token and nothing else, else "". That is the case where the injected value
// keeps the variable's declared type instead of rendering to a string (V14).
func TemplateVar(value string) string {
	segments := ParseTemplate(value)
	if len(segments) == 1 && segments[0].Var != "" {
		return segments[0].Var
	}
	return ""
}

// VaultToken reports whether a template reference is a SLNG Vault variable
// rather than a package variable, and returns its name without the marker.
//
// The two share the {{ }} spelling and mean entirely different things: a package
// variable is declared in the package and rendered by unmute or by the target,
// while a Vault variable names a value in SLNG's own secret store that only a
// SLNG agent can resolve. The `$` is the whole difference, and telling an author
// their Vault token "is not a declared variable" sends them to the wrong file.
func VaultToken(ref string) (string, bool) {
	name, found := strings.CutPrefix(ref, "$")
	if !found || name == "" {
		return "", false
	}
	return name, true
}

// VaultNamePattern is the shape SLNG requires of a name in its secret store. The
// same pattern is written seven times across the SLNG backend and agrees
// everywhere, read 2026-08-25.
var VaultNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// ValidVaultName reports whether a name is one SLNG will accept.
func ValidVaultName(name string) bool {
	return len(name) <= 64 && VaultNamePattern.MatchString(name)
}
