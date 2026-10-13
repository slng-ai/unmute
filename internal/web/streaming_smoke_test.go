//go:build smoke

package web

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"
)

// playwrightScript starts the shipped page on a local server and runs one
// Playwright script against it, skipping when Node or Chromium is missing.
func playwrightScript(t *testing.T, name string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	preflight := exec.CommandContext(t.Context(), node, "-e", `const {chromium}=require(process.env.UNMUTE_SWEEP_PLAYWRIGHT || 'playwright'); if(!require('node:fs').existsSync(chromium.executablePath())) throw new Error('Chromium is not installed');`)
	if out, err := preflight.CombinedOutput(); err != nil {
		t.Skipf("optional Playwright unavailable (set UNMUTE_SWEEP_PLAYWRIGHT): %s", out)
	}
	server := httptest.NewServer(http.FileServer(http.FS(FS)))
	defer server.Close()
	script, err := filepath.Abs(filepath.Join("../testdata/web", name))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(t.Context(), node, script, server.URL).CombinedOutput()
	if err != nil {
		t.Fatalf("shipped-page %s: %v\n%s", name, err, out)
	}
	t.Log(string(out))
}

// TestSmokeDevUITextChat types lines and starts a text-only call on both
// transports, with the media and network boundaries replaced.
func TestSmokeDevUITextChat(t *testing.T) {
	playwrightScript(t, "text-chat.js")
}

// This runs the shipped page in Chromium. Only network/media boundaries are
// replaced; the renderer, browser layout and native controls stay real.
func TestSmokeDevUIStreaming(t *testing.T) {
	playwrightScript(t, "streaming-replay.js")
}
