package cli

import (
	"fmt"
	"io"

	"github.com/slng-ai/unmute/internal/style"
)

// printHeader writes the run header only when w is a terminal, so scripts and
// pipes keep byte-identical machine-readable output (C18).
//
// The colours themselves come from internal/style, which is the one owner. This
// file used to carry a second renderer with its own copy of the badge; they
// agreed by hand until they did not.
func printHeader(w io.Writer, title string) {
	if isTTY(w) {
		fmt.Fprintln(w, style.For(w).Header(title))
	}
}
