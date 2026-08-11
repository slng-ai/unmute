package ir

import "regexp"

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
