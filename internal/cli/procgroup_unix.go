//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup puts a child in its own process group, which is what lets
// signalGroup reach the grandchildren it spawns (uv's python, cloudflared's
// helpers) instead of only the process we started.
func ownProcessGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalGroup sends sig to the child's whole group. The negative pid is the
// group, not the process.
func signalGroup(c *exec.Cmd, sig syscall.Signal) {
	_ = syscall.Kill(-c.Process.Pid, sig)
}
