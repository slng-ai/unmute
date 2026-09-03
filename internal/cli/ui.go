package cli

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/slng-ai/unmute/internal/style"
)

// printHeader writes the run header only when w is a terminal, so scripts and
// pipes keep byte-identical machine-readable output (C18).
//
// Every command calls it, and calls it before the work rather than after: a
// package that fails to load is still a run of unmute, and `init` printing a
// bare file list while `compile` printed a badge was the whole complaint.
//
// The colours themselves come from internal/style, which is the one owner. This
// file used to carry a second renderer with its own copy of the badge; they
// agreed by hand until they did not.
func printHeader(w io.Writer, title string) {
	if isTTY(w) {
		fmt.Fprintln(w, style.For(w).Header(title))
		fmt.Fprintln(w)
	}
}

// dimPath renders a path whose directory is dimmed, so a list of ten files that
// share nine tenths of their text reads as ten filenames rather than ten paths.
func dimPath(u style.Writer, path string) string {
	dir, file := filepath.Split(path)
	if dir == "" {
		return path
	}
	return u.Dim(dir) + file
}

// warnf and notef write the two advisory prefixes this CLI uses, so the word
// itself carries the severity instead of the reader scanning for it. Both take
// the trailing newline in the format, because some of these lines wrap.
func warnf(w io.Writer, format string, args ...any) {
	fmt.Fprint(w, style.For(w).Warned("warning:")+" "+fmt.Sprintf(format, args...))
}

func notef(w io.Writer, format string, args ...any) {
	fmt.Fprint(w, style.For(w).Dim("note:")+" "+fmt.Sprintf(format, args...))
}
