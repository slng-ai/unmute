//go:build smoke

package web

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"
)

// This runs the shipped page in Chromium. Only network/media boundaries are
// replaced; the renderer, browser layout and native controls stay real.
func TestSmokeDevUIStreaming(t *testing.T) {
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
	script, err := filepath.Abs("../testdata/web/streaming-replay.js")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), node, script, server.URL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("shipped-page replay: %v\n%s", err, out)
	}
	t.Log(string(out))
}
