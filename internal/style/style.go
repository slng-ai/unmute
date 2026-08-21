// Package style holds the Unmute brand theme: the single source of color for the
// interactive console and the CLI. No other file
// defines a color literal. NO_COLOR (no-color.org) drops all color, so helpers
// then return plain text. Light and dark terminals both stay readable through
// adaptive colors.
package style

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Brand and semantic color tokens (hex). accent is Unmute yellow, ink Unmute black.
// warn is amber, kept apart from brand yellow so a warning never reads as
// branding (C14, V43).
const (
	Accent  = "#FBE566"
	Ink     = "#000000"
	Warn    = "#F5A623"
	Success = "#3FB950"
	Error   = "#F85149"
)

// Adaptive tokens keep contrast on both light and dark backgrounds.
var (
	Text        = lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#E6E6E6"}
	Muted       = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	Border      = lipgloss.AdaptiveColor{Light: "#D4D4D4", Dark: "#3A3A3A"}
	BorderFocus = lipgloss.Color(Accent)
)

// NoColor reports whether NO_COLOR is set. When true, every helper returns plain
// text with no escape sequences (C14, V43).
func NoColor() bool { return os.Getenv("NO_COLOR") != "" }

// Badge renders the Unmute logo chip: ink text on an accent fill, bounded to its
// own width and never a full row (C15, V23). NO_COLOR returns the plain text.
func Badge(s string) string {
	if NoColor() {
		return s
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(Ink)).
		Background(lipgloss.Color(Accent)).
		Bold(true).
		Padding(0, 1).
		Render(s)
}

// Accented renders text in the Unmute accent color, foreground only. Used for the
// Home hero wordmark and highlights.
func Accented(s string) string {
	if NoColor() {
		return s
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(Accent)).Bold(true).Render(s)
}

// Dim renders muted secondary text (metadata, current values, disabled rows).
func Dim(s string) string {
	if NoColor() {
		return s
	}
	return lipgloss.NewStyle().Foreground(Muted).Render(s)
}

// Errored renders text in the error color.
func Errored(s string) string { return fg(s, Error) }

func fg(s, hex string) string {
	if NoColor() {
		return s
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render(s)
}
