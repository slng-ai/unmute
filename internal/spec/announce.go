package spec

import "strings"

// Announce is what an author may write for `announce:`: one fixed sentence as a
// bare scalar, or several alternatives as a list, one of which is spoken each
// time the line fires. Only the authoring surface is one-or-many; the resolved
// IR always holds a list.
//
// Alternatives exist because the line is spoken by code rather than written by
// the model, so it is identical every time. A live call on 2026-09-16 entered
// the booking flow twice in forty-four seconds and played "Sure, let me get
// that sorted for you." both times, which is what a recording sounds like.
//
// A handoff's `announce:` is deliberately not this type. A handoff moves the
// conversation once and never comes back, so its line cannot repeat, and its
// empty-versus-unset distinction is load-bearing where this one's is not.
type Announce []string

// UnmarshalYAML accepts both shapes, the way Regions does, and for the same
// reason: goccy's InterfaceUnmarshaler re-enters the decoder, so a value that is
// neither shape fails with goccy's own line and column rather than a sentence of
// ours with no position.
func (a *Announce) UnmarshalYAML(unmarshal func(any) error) error {
	var list []string
	listErr := unmarshal(&list)
	if listErr == nil {
		*a = list
		return nil
	}
	var one string
	if err := unmarshal(&one); err == nil {
		*a = Announce{one}
		return nil
	}
	return listErr
}

// MarshalYAML writes a single line back as the bare scalar the author wrote, so
// a TUI round-trip does not reshape their file.
func (a Announce) MarshalYAML() (any, error) {
	if len(a) == 1 {
		return a[0], nil
	}
	return []string(a), nil
}

// Settled trims every line and drops the blanks, which is the whole default
// resolution: a blank or whitespace-only entry reads as nothing to say, so every
// driver sees a settled list and none has to decide what " " means.
func (a Announce) Settled() []string {
	var out []string
	for _, line := range a {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
