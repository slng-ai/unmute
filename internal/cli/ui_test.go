package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestV48HeaderShowsBrand(t *testing.T) { // docs/spec/tui.md V48
	if h := newUI(&bytes.Buffer{}).header("validate"); !strings.Contains(h, "SLNG//") {
		t.Fatalf("run header omits the SLNG brand: %q", h)
	}
}

func TestV48NoColorOutputIsPlain(t *testing.T) { // docs/spec/tui.md V48
	u := newUI(&bytes.Buffer{})
	for _, s := range []string{u.ok("✓"), u.fail("✗"), u.accent("livekit"), u.header("x")} {
		if strings.ContainsRune(s, '\x1b') {
			t.Fatalf("non-terminal output must be plain, got escape in %q", s)
		}
	}
}
