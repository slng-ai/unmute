package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/style"
)

func TestV48HeaderShowsBrand(t *testing.T) {
	if h := style.For(&bytes.Buffer{}).Header("validate"); !strings.Contains(h, "UNMUTE//") {
		t.Fatalf("run header omits the Unmute brand: %q", h)
	}
}

// A command writing to a pipe or a buffer must emit no escapes, even when
// os.Stdout is still a terminal. That is the whole reason colour is bound to a
// writer rather than taken from the global renderer.
func TestV48NoColorOutputIsPlain(t *testing.T) {
	u := style.For(&bytes.Buffer{})
	for _, s := range []string{u.Ok("✓"), u.Failed("✗"), u.Accent("livekit"), u.Header("x")} {
		if strings.ContainsRune(s, '\x1b') {
			t.Fatalf("non-terminal output must be plain, got escape in %q", s)
		}
	}
}
