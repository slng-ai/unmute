package cli

import (
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"

	"github.com/slng/unmute/internal/style"
)

// ui themes CLI output through a per-writer lipgloss renderer, so the same code
// is colored on a terminal and plain on a pipe or a test buffer (docs/spec/tui.md
// C18, V48). Color also drops under NO_COLOR. All color comes from internal/style.
type ui struct{ r *lipgloss.Renderer }

func newUI(w io.Writer) ui { return ui{r: lipgloss.NewRenderer(w)} }

func (u ui) fg(s, hex string) string {
	return u.r.NewStyle().Foreground(lipgloss.Color(hex)).Render(s)
}

func (u ui) ok(s string) string     { return u.fg(s, style.Success) }
func (u ui) fail(s string) string   { return u.fg(s, style.Error) }
func (u ui) muted(s string) string  { return u.r.NewStyle().Foreground(style.Muted).Render(s) }
func (u ui) accent(s string) string {
	return u.r.NewStyle().Foreground(lipgloss.Color(style.Accent)).Bold(true).Render(s)
}

// header is the SLNG run header: the bounded logo badge plus a title.
func (u ui) header(title string) string {
	badge := u.r.NewStyle().
		Foreground(lipgloss.Color(style.Ink)).
		Background(lipgloss.Color(style.Accent)).
		Bold(true).
		Padding(0, 1).
		Render("SLNG//")
	return badge + " " + u.muted(title)
}

// printHeader writes the run header only when w is a terminal, so scripts and
// pipes keep byte-identical machine-readable output (C18).
func printHeader(w io.Writer, title string) {
	if isTTY(w) {
		fmt.Fprintln(w, newUI(w).header(title))
	}
}
