package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/slng/unmute/internal/style"
)

// View composes the four SLNG regions: header (logo badge + breadcrumb +
// target), body (sidebar + editor, or editor alone), and footer hint bar
// (docs/spec/tui.md C16, V44). The logo badge shows on every interactive screen
// (C15, V23).
func (m console) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	header := m.renderHeader()
	footer := m.renderFooter()
	bodyH := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyH < 3 {
		bodyH = 3
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, m.renderBody(bodyH), footer)
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
	hint := "↑/↓ move · ↵ select · esc back · q quit"
	switch {
	case m.notice != nil:
		hint = "↑/↓ scroll · enter/esc back"
	case m.req != nil && m.req.kind == kindInput:
		hint = "↵ confirm · esc back"
	case m.req != nil && m.req.kind == kindText:
		hint = "ctrl+d save · esc back"
	}
	return style.Dim(hint)
}

func (m console) renderBody(h int) string {
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
