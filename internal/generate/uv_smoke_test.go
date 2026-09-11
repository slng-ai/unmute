//go:build smoke

package generate

import (
	"os"
	"os/exec"
)

// uvCommand is `uv ...` with the environment every smoke run needs.
//
// UV_LINK_MODE=copy, because uv's default is to hard-link packages out of its
// cache, and nltk (pulled in by llama-index for the knowledge module) refuses to
// open a corpus file with more than one link: its path-hardening treats a
// multiply-linked file as a possible escape from the install root (CWE-59) and
// raises PermissionError at import. That failed the salon package's startup
// check on a clean cache on 2026-09-11 with an error naming neither uv nor the
// framework. The emitted Dockerfile already installs with --link-mode=copy for
// its own reasons; the smoke venvs now match it.
func uvCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("uv", args...)
	cmd.Env = append(os.Environ(), "UV_LINK_MODE=copy")
	return cmd
}
