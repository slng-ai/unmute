package style

import (
	"strings"
	"testing"
)

func TestV43ColorsComeFromTokenTable(t *testing.T) {
	if Accent != "#FBE566" {
		t.Fatalf("accent = %q, want #FBE566", Accent)
	}
	if Ink != "#000000" {
		t.Fatalf("ink = %q, want #000000", Ink)
	}
}

func TestV43WarnIsNotBrandYellow(t *testing.T) {
	if Warn == Accent {
		t.Fatalf("warn %q must differ from brand accent %q", Warn, Accent)
	}
}

func TestV43NoColorEmitsNoEscapes(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, s := range []string{Badge("UNMUTE//"), Accented("hero"), Dim("meta")} {
		if strings.ContainsRune(s, '\x1b') {
			t.Fatalf("NO_COLOR output contains escape: %q", s)
		}
	}
	if got := Badge("UNMUTE//"); got != "UNMUTE//" {
		t.Fatalf("NO_COLOR badge = %q, want plain UNMUTE//", got)
	}
}
