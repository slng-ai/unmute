package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// V1: the watcher extracts the quick tunnel origin from realistic cloudflared
// log output, including when the URL splits across writes.
func TestTunnelURLWatcherExtractsOriginAcrossSplitWrites(t *testing.T) {
	var sink bytes.Buffer
	got := ""
	watcher := &tunnelURLWatcher{w: &sink, fire: func(origin string) { got = origin }}
	lines := []string{
		"2026-07-22T09:00:00Z INF Thank you for trying Cloudflare Tunnel...\n",
		"failed to request quick Tunnel: Post \"https://api.trycloudflare.com/tunnel\": context deadline exceeded\n",
		"2026-07-22T09:00:01Z INF +--------------------------------------------+\n",
		"2026-07-22T09:00:01Z INF |  Your quick Tunnel has been created! Visit it at (it may take some time to be reachable):  |\n",
		"2026-07-22T09:00:01Z INF |  https://drums-inputs-guam", "-tulsa.trycloudflare.com  |\n",
		"2026-07-22T09:00:01Z INF +--------------------------------------------+\n",
	}
	for _, line := range lines {
		if _, err := watcher.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if got != "https://drums-inputs-guam-tulsa.trycloudflare.com" {
		t.Fatalf("extracted origin = %q", got)
	}
	if !strings.Contains(sink.String(), "quick Tunnel has been created") {
		t.Fatalf("watcher did not tee output: %q", sink.String())
	}
}

// V2: a missing cloudflared binary fails with install instructions for both
// platforms and names --public-url as the bring-your-own-tunnel alternative.
func TestStartQuickTunnelMissingBinaryNamesInstallAndPublicURL(t *testing.T) {
	restore := tunnelLookPath
	tunnelLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { tunnelLookPath = restore })

	_, err := startQuickTunnel(context.Background(), "7860", &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"brew install cloudflared", "cloudflare/cloudflared/releases", "--public-url"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

// V1: the tunnel child is spawned with the URL parsed from its output, and
// Stop kills the whole process group.
func TestStartQuickTunnelParsesURLAndStopKillsGroup(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "cloudflared")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"INF |  https://fake-quick.trycloudflare.com  |\" 1>&2\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreLook, restoreCmd := tunnelLookPath, tunnelCommand
	tunnelLookPath = func(string) (string, error) { return script, nil }
	tunnelCommand = exec.CommandContext
	t.Cleanup(func() { tunnelLookPath, tunnelCommand = restoreLook, restoreCmd })

	var sink bytes.Buffer
	tunnel, err := startQuickTunnel(context.Background(), "7860", &sink)
	if err != nil {
		t.Fatal(err)
	}
	if tunnel.URL != "https://fake-quick.trycloudflare.com" {
		t.Fatalf("tunnel URL = %q", tunnel.URL)
	}
	pid := tunnel.cmd.Process.Pid
	tunnel.Stop()
	deadline := time.After(5 * time.Second)
	for {
		if err := syscall.Kill(-pid, 0); err != nil {
			break // group is gone
		}
		select {
		case <-deadline:
			t.Fatal("tunnel process group still alive after Stop")
		case <-time.After(50 * time.Millisecond):
		}
	}
	tunnel.Stop() // second Stop must be safe
}

// V1: a child that dies before assigning a URL surfaces a clear error
// instead of hanging until the startup timeout.
func TestStartQuickTunnelReportsEarlyExit(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "cloudflared")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'failed to request quick Tunnel: Post \"https://api.trycloudflare.com/tunnel\": context deadline exceeded' 1>&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) { return script, nil }
	t.Cleanup(func() { tunnelLookPath = restoreLook })

	_, err := startQuickTunnel(context.Background(), "7860", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "exited before assigning a quick tunnel URL") {
		t.Fatalf("early exit error = %v", err)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("early exit should wrap the child error: %v", err)
	}
}
