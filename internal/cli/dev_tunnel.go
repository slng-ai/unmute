package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

// Quick-tunnel management for `dev --telephony` carrier WebSocket routes.
// cloudflared is the only supported tunnel client (SPEC C4): Apache 2.0, no
// account or token needed, so one client means one output parser and one
// failure mode. `--public-url` bypasses all of this for any other tunnel.
//
// Output contract verified 2026-07-22 against cloudflare/cloudflared
// cmd/cloudflared/tunnel/quick_tunnel.go: the assigned origin is printed to
// the child's log output as an https://<random>.trycloudflare.com line.
var (
	tunnelLookPath = exec.LookPath
	tunnelCommand  = exec.CommandContext
)

const tunnelStartTimeout = 45 * time.Second

var tunnelURLPattern = regexp.MustCompile(`\|\s+(https://[a-z0-9-]+\.trycloudflare\.com)\s+\|`)

const tunnelInstallHint = "cloudflared not found on PATH; the managed tunnel for carrier webhook routes needs it. " +
	"Install it (macOS: `brew install cloudflared`; Linux: distribution package or a binary from " +
	"https://github.com/cloudflare/cloudflared/releases) or pass --public-url to bring your own tunnel (ngrok included)"

// managedTunnel is a running cloudflared quick tunnel child process.
type managedTunnel struct {
	URL  string
	cmd  *exec.Cmd
	done chan error
}

// startQuickTunnel spawns cloudflared against the local application port and
// blocks until the public origin is known, the child exits, the context is
// cancelled, or the startup timeout elapses. Child output is teed to sink
// (the telephony log). The caller must Stop the tunnel on every exit path.
func startQuickTunnel(ctx context.Context, port string, sink io.Writer) (*managedTunnel, error) {
	path, err := tunnelLookPath("cloudflared")
	if err != nil {
		return nil, errors.New(tunnelInstallHint)
	}
	urlCh := make(chan string, 1)
	watcher := &tunnelURLWatcher{w: sink, fire: func(origin string) { urlCh <- origin }}
	cmd := tunnelCommand(ctx, path, "tunnel", "--no-autoupdate", "--url", "http://127.0.0.1:"+port)
	cmd.Stdout = watcher
	cmd.Stderr = watcher
	ownProcessGroup(cmd) // own group so we can kill children
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start cloudflared: %w", err)
	}
	tunnel := &managedTunnel{cmd: cmd, done: make(chan error, 1)}
	go func() { tunnel.done <- cmd.Wait() }()
	select {
	case origin := <-urlCh:
		tunnel.URL = origin
		return tunnel, nil
	case err := <-tunnel.done:
		if err != nil {
			return nil, fmt.Errorf("cloudflared exited before assigning a quick tunnel URL: %w", err)
		}
		return nil, errors.New("cloudflared exited before assigning a quick tunnel URL")
	case <-ctx.Done():
		tunnel.Stop()
		return nil, ctx.Err()
	case <-time.After(tunnelStartTimeout):
		tunnel.Stop()
		return nil, fmt.Errorf("cloudflared did not assign a quick tunnel URL within %s; check network access or pass --public-url", tunnelStartTimeout)
	}
}

// Stop ends the tunnel child's whole process group; safe to call more than
// once and on a tunnel that never fully started.
func (t *managedTunnel) Stop() {
	if t == nil || t.cmd == nil || t.cmd.Process == nil {
		return
	}
	stopBot(t.cmd, t.done)
}

// tunnelURLWatcher tees child output to w while scanning for the quick
// tunnel origin; it fires once with the first match. stdout and stderr share
// one watcher, so Write is guarded.
type tunnelURLWatcher struct {
	mu   sync.Mutex
	w    io.Writer
	buf  []byte
	done bool
	fire func(origin string)
}

func (tw *tunnelURLWatcher) Write(p []byte) (int, error) {
	tw.mu.Lock()
	if !tw.done {
		tw.buf = append(tw.buf, p...)
		if match := tunnelURLPattern.FindSubmatch(tw.buf); match != nil {
			tw.done = true
			tw.buf = nil
			origin := string(match[1])
			fire := tw.fire
			tw.mu.Unlock()
			fire(origin)
			return tw.w.Write(p)
		}
		if len(tw.buf) > 8192 { // bound memory; keep a tail in case the URL splits a write
			tw.buf = tw.buf[len(tw.buf)-256:]
		}
	}
	tw.mu.Unlock()
	return tw.w.Write(p)
}
