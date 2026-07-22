package style

import (
	"strings"
	"testing"
)

func TestV43ColorsComeFromTokenTable(t *testing.T) { // docs/spec/tui.md V43
	if Accent != "#FBE566" {
		t.Fatalf("accent = %q, want #FBE566", Accent)
	}
	if Ink != "#000000" {
		t.Fatalf("ink = %q, want #000000", Ink)
	}
}

func TestV43WarnIsNotBrandYellow(t *testing.T) { // docs/spec/tui.md V43
	if Warn == Accent {
		t.Fatalf("warn %q must differ from brand accent %q", Warn, Accent)
	}
}

func TestV43NoColorEmitsNoEscapes(t *testing.T) { // docs/spec/tui.md V43
	t.Setenv("NO_COLOR", "1")
	for _, s := range []string{Badge("SLNG//"), Accented("hero"), Dim("meta")} {
		if strings.ContainsRune(s, '\x1b') {
			t.Fatalf("NO_COLOR output contains escape: %q", s)
		}
	}
	if got := Badge("SLNG//"); got != "SLNG//" {
		t.Fatalf("NO_COLOR badge = %q, want plain SLNG//", got)
	}
}
