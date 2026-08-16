package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/slng-ai/unmute/internal/style"
)

// View composes the four SLNG regions: header (logo badge + breadcrumb +
// target), body (sidebar + editor, or editor alone), and footer hint bar. The
// logo badge shows on every interactive screen.
func (m console) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		msg := fmt.Sprintf("terminal too small — need at least %dx%d", minWidth, minHeight)
		return lipgloss.Place(max(m.width, 1), max(m.height, 1), lipgloss.Center, lipgloss.Center, style.Dim(msg))
	}
	footer := m.renderFooter()
	// Home is the logo hero: no header badge, the wordmark is the whole screen.
	if m.req != nil && m.req.ctx.hero {
		bodyH := m.height - lipgloss.Height(footer)
		if bodyH < 3 {
			bodyH = 3
		}
		return lipgloss.JoinVertical(lipgloss.Left, m.renderHome(bodyH), footer)
	}
	header := m.renderHeader()
	bodyH := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyH < 3 {
		bodyH = 3
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, m.renderBody(bodyH), footer)
}

// heroArt is the SLNG wordmark drawn large for the Home screen. Rendered in the
// accent color; NO_COLOR falls back to plain "SLNG//" text (C15, V23).
const heroArt = `███████╗██╗     ███╗   ██╗ ██████╗    ██╗██╗
██╔════╝██║     ████╗  ██║██╔════╝   ██╔╝██║
███████╗██║     ██╔██╗ ██║██║  ███╗ ██╔╝██╔╝
╚════██║██║     ██║╚██╗██║██║   ██║██╔╝██╔╝
███████║███████╗██║ ╚████║╚██████╔╝██╔╝██╔╝
╚══════╝╚══════╝╚═╝  ╚═══╝ ╚═════╝ ╚═╝ ╚═╝`

func heroLogo() string {
	if style.NoColor() {
		return "SLNG//"
	}
	return style.Accented(heroArt)
}

func (m console) renderHome(h int) string {
	block := lipgloss.JoinVertical(lipgloss.Center,
		heroLogo(),
		"",
		style.Dim("Author-once, portable voice agents"),
		"",
		m.renderChoices(),
	)
	return lipgloss.Place(m.width, h, lipgloss.Center, lipgloss.Center, block)
}

func (m console) renderChoices() string {
	var b strings.Builder
	for i, c := range m.req.choices {
		if i == m.cursor {
			b.WriteString(style.Accented("› " + c.label))
		} else {
			b.WriteString("  " + c.label)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m console) renderHeader() string {
	left := style.Badge(" SLNG// ")
	if m.req != nil && m.req.ctx.breadcrumb != "" {
		left += "  " + style.Dim(m.req.ctx.breadcrumb)
	}
	right := ""
	if m.req != nil && m.req.ctx.target != "" {
		right = style.Accented("●") + " " + m.req.ctx.target
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m console) renderFooter() string {
	hint := "↑/↓ move · ↵ select · ctrl+p palette · esc back · q quit"
	switch {
	case m.palette != nil:
		hint = "type to filter · ↑/↓ move · ↵ jump · esc close"
	case m.notice != nil:
		hint = "↑/↓ scroll · enter/esc back"
	case m.req != nil && m.req.kind == kindInput:
		hint = "↵ confirm · esc back"
	case m.req != nil && m.req.kind == kindText:
		hint = "ctrl+d save · esc back"
	}
	return style.Dim(hint)
}

func (m console) renderPalette(w, h int) string {
	var b strings.Builder
	b.WriteString(style.Accented("Command palette") + "\n")
	b.WriteString("> " + m.palette.query + "\n\n")
	matches := m.paletteMatches()
	if len(matches) == 0 {
		b.WriteString(style.Dim("no matches"))
	}
	for n, idx := range matches {
		label := m.req.choices[idx].label
		if n == m.palette.cursor {
			b.WriteString(style.Accented("› " + label))
		} else {
			b.WriteString("  " + label)
		}
		b.WriteString("\n")
	}
	boxW := min(w-4, 60)
	if boxW < 12 {
		boxW = max(w, 12)
	}
	box := panel(boxW, min(h, len(matches)+6), true).Render(strings.TrimRight(b.String(), "\n"))
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
}

func (m console) renderBody(h int) string {
	if m.palette != nil {
		return m.renderPalette(m.width, h)
	}
	if sw := m.sidebarWidth(); sw > 0 {
		side := m.renderSidebar(sw, h)
		ed := m.renderEditor(m.width-sw-1, h)
		return lipgloss.JoinHorizontal(lipgloss.Top, side, " ", ed)
	}
	return m.renderEditor(m.width, h)
}

func (m console) renderSidebar(w, h int) string {
	var b strings.Builder
	b.WriteString(style.Dim("SECTIONS") + "\n\n")
	for _, it := range m.req.ctx.sidebar {
		switch {
		case it.active:
			b.WriteString(style.Accented("› " + it.label))
		case it.child:
			b.WriteString("    " + style.Dim(it.label))
		default:
			b.WriteString("  " + it.label)
		}
		b.WriteString("\n")
	}
	return panel(w, h, false).Render(b.String())
}

func (m console) renderEditor(w, h int) string {
	var content string
	switch {
	case m.notice != nil:
		content = m.renderNotice()
	case m.req == nil:
		content = ""
	case m.req.kind == kindSelect:
		content = m.renderSelect()
	case m.req.kind == kindInput:
		content = m.renderInput()
	case m.req.kind == kindText:
		content = m.renderText()
	}
	return panel(w, h, true).Render(content)
}

func (m console) renderSelect() string {
	var b strings.Builder
	b.WriteString(style.Accented(m.req.title) + "\n")
	if m.req.desc != "" {
		b.WriteString(style.Dim(m.req.desc) + "\n")
	}
	b.WriteString("\n")
	for i, c := range m.req.choices {
		if i == m.cursor {
			b.WriteString(style.Accented("› " + c.label))
		} else {
			b.WriteString("  " + c.label)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m console) renderInput() string {
	var b strings.Builder
	b.WriteString(style.Accented(m.req.title) + "\n")
	if m.req.desc != "" {
		b.WriteString(style.Dim(m.req.desc) + "\n")
	}
	b.WriteString("\n" + m.input.View())
	if m.errMsg != "" {
		b.WriteString("\n\n" + style.Errored(m.errMsg))
	}
	return b.String()
}

func (m console) renderText() string {
	var b strings.Builder
	b.WriteString(style.Accented(m.req.title) + "\n")
	if m.req.desc != "" {
		b.WriteString(style.Dim(m.req.desc) + "\n")
	}
	b.WriteString("\n" + m.area.View())
	return b.String()
}

func (m console) renderNotice() string {
	end := min(len(m.notice.lines), m.notice.offset+m.notice.height)
	var lines []string
	if m.notice.offset < end {
		lines = m.notice.lines[m.notice.offset:end]
	}
	return style.Accented(m.notice.title) + "\n\n" + strings.Join(lines, "\n")
}

// panel is a rounded box; the focused panel carries the accent border, the other
// the muted border (V44). NO_COLOR draws the border with no color.
func panel(w, h int, focus bool) lipgloss.Style {
	s := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	if w > 2 {
		s = s.Width(w - 2)
	}
	if h > 2 {
		s = s.Height(h - 2)
	}
	if !style.NoColor() {
		if focus {
			s = s.BorderForeground(lipgloss.Color(style.Accent))
		} else {
			s = s.BorderForeground(style.Border)
		}
	}
	return s
}
