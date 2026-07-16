package main

import (
	"os"

	"github.com/slng/unmute/internal/cli"
)

// version is stamped at link time (see Makefile LDFLAGS); never hardcoded.
var version = "dev"

func main() { os.Exit(cli.Execute(version)) }
