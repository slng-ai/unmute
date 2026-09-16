package generate

import (
	"fmt"
	"strings"
)

// announceExpr lowers a resolved announce list to the Python expression that
// produces the line to speak. Both targets use it, because both face the same
// two cases and the choice must not drift between them.
//
// One entry emits the string literal it always did, so a package writing a
// scalar `announce:` emits the bytes it emitted before alternatives existed.
// That is not politeness, it is what keeps `compat_bytes.txt` still: `announce`
// is not in the new-key exemption in compat_test.go, so a changed byte there is
// a failing gate rather than a note in a changelog.
//
// Several entries go through the emitted `_announce` helper, which never
// returns the line that key used last. The key is what separates one site's
// history from another's, so it has to be unique per call site and stable
// across compiles; callers build it from the thing that owns the line.
func announceExpr(key string, lines []string) string {
	// Blanks drop here as well as in ir.Build. A one-entry list holding the
	// empty string would otherwise lower to the two-character expression `""`,
	// which every `{{if .Announce}}` in both templates reads as something to
	// say, and the caller hears a frame carrying nothing.
	var kept []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	switch len(kept) {
	case 0:
		return ""
	case 1:
		return pyQuote(kept[0])
	}
	quoted := make([]string, len(kept))
	for i, line := range kept {
		quoted[i] = pyQuote(line)
	}
	return fmt.Sprintf("_announce(%s, [%s])", pyQuote(key), strings.Join(quoted, ", "))
}

// announceChosen reports whether a lowered expression goes through the emitted
// `_announce` helper rather than being a bare literal. It is what decides if the
// helper and its `random` import are emitted at all, the same "only emit
// machinery a package actually uses" rule the announcement flags follow.
func announceChosen(exprs ...string) bool {
	for _, expr := range exprs {
		if strings.HasPrefix(expr, "_announce(") {
			return true
		}
	}
	return false
}
