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
	for _, s := range []string{u.Ok("✓"), u.Failed("✗"), u.Accent("livekit"), u.Header("x"),
		u.Warned("warning:"), u.Bold("Errors:"), u.Dim("created")} {
		if strings.ContainsRune(s, '\x1b') {
			t.Fatalf("non-terminal output must be plain, got escape in %q", s)
		}
	}
}

// Branding every command is only safe because none of it survives a pipe. These
// are the exact bytes a script reads, so this test is what says the run header,
// the dimmed directory and the coloured advisory prefixes cost a caller nothing.
func TestBrandingLeavesPipedOutputByteIdentical(t *testing.T) {
	var buf bytes.Buffer
	printHeader(&buf, "init my-test")
	if buf.Len() != 0 {
		t.Errorf("the run header reached a non-terminal writer: %q", buf.String())
	}

	u := style.For(&buf)
	for _, c := range []struct{ path, want string }{
		{"my-test/tools/end_call.yaml", "my-test/tools/end_call.yaml"},
		{"agent.yaml", "agent.yaml"},
	} {
		if got := dimPath(u, c.path); got != c.want {
			t.Errorf("dimPath(%q) = %q, want %q", c.path, got, c.want)
		}
	}

	buf.Reset()
	warnf(&buf, "%s: %s\n", "livekit", "turn placement is a preference")
	notef(&buf, "%s\n", "read from the last stored probe")
	want := "warning: livekit: turn placement is a preference\n" +
		"note: read from the last stored probe\n"
	if got := buf.String(); got != want {
		t.Errorf("advisory prefixes changed piped bytes:\n got %q\nwant %q", got, want)
	}
}
