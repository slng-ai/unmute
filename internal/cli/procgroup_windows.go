//go:build windows

package cli

import (
	"os/exec"
	"syscall"
)

// Windows has no Setpgid and no syscall.Kill, so this file is what makes the
// package cross-compile for the Windows release binary.
//
// ponytail: the graceful half is a no-op and the forceful half kills only the
// child we started. Every caller is a `dev` command that already needs POSIX
// tools (uv, cloudflared, docker compose), so a grandchild leak here is
// theoretical. Upgrade path if Windows dev ever ships: a job object, which is
// the only real way to reap a tree there.
func ownProcessGroup(c *exec.Cmd) {}

func signalGroup(c *exec.Cmd, sig syscall.Signal) {
	if sig == syscall.SIGKILL {
		_ = c.Process.Kill()
	}
}
