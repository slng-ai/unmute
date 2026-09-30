package generate

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// fromImport matches one `from module import ...` statement, on one line or
// wrapped in parentheses over several, the way ruff's isort writes it.
var fromImport = regexp.MustCompile(`(?m)^from ([\w.]+) import (?:\(([^)]*)\)|([^\n]*))`)

// pyImportedNames lists the names a Python source imports from one module,
// wherever its import statements are and however they wrap. `a as b` counts as
// `a`.
func pyImportedNames(src, module string) []string {
	var names []string
	for _, match := range fromImport.FindAllStringSubmatch(src, -1) {
		if match[1] != module {
			continue
		}
		for _, line := range strings.Split(match[2]+match[3], "\n") {
			if hash := strings.Index(line, "#"); hash >= 0 {
				line = line[:hash]
			}
			for _, name := range strings.Split(line, ",") {
				if name, _, _ = strings.Cut(strings.TrimSpace(name), " as "); name != "" {
					names = append(names, name)
				}
			}
		}
	}
	return names
}

// pyImports reports whether a Python source imports name from module.
func pyImports(src, module, name string) bool {
	return slices.Contains(pyImportedNames(src, module), name)
}

func TestPyImportedNamesReadsWrappedAndSingleLineImports(t *testing.T) {
	src := `from __future__ import annotations

from pipecat.frames.frames import (
    EndFrame,
    TTSSpeakFrame,  # spoken announcements
)
from pipecat.turns.user_mute import FunctionCallUserMuteStrategy as Mute
from pipecat.turns.user_mute_extra import Other
`
	if got := pyImportedNames(src, "pipecat.frames.frames"); !slices.Equal(got, []string{"EndFrame", "TTSSpeakFrame"}) {
		t.Errorf("wrapped import read as %v", got)
	}
	if !pyImports(src, "pipecat.turns.user_mute", "FunctionCallUserMuteStrategy") {
		t.Error("single-line import with an alias not found")
	}
	if pyImports(src, "pipecat.turns.user_mute", "Other") {
		t.Error("a name from a different module was attributed to this one")
	}
}
