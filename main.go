package main

import (
	"os"

	"github.com/slng-ai/unmute/internal/cli"
)

// Stamped at link time by both build paths (Makefile LDFLAGS and
// .goreleaser.yaml); never hardcoded. A bare `go build` leaves commit and date
// empty, which is why the version string below has two shapes.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	v := version
	if commit != "" {
		v = version + " (" + commit + " " + date + ")"
	}
	os.Exit(cli.Execute(v))
}
