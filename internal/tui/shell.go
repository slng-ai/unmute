package tui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// The interactive console is a custom Bubble Tea MVU program. The screen flow
// runs as ordinary blocking Go code in a goroutine
// (fieldRunner); it hands the model one field at a time over a channel and waits
// for the answer. The model owns the alt-screen, the SLNG chrome, and the field
// components. huh renders only the accessible/headless path (run, in tui.go).

// ---- flow <-> model protocol ----

// ponytail: an alias, not a defined type. It was `interface{}`, which is the
// hand-spelled `any`; the name is kept because `chan shellRequest` reads better
// at its six use sites than `chan any` does.
type shellRequest = any

type requestMsg struct {
	request shellRequest
	ok      bool
}

func waitRequest(requests <-chan shellRequest) tea.Cmd {
	return func() tea.Msg {
		request, ok := <-requests
		return requestMsg{request: request, ok: ok}
	}
}

// fieldKind is the interactive field the model renders. confirms and notices are
// selects; the flow never sends anything else (C4).
type fieldKind int

const (
	kindSelect fieldKind = iota
	kindInput
	kindText
)

type choice struct{ label, value string }

// sideItem is one row of the section sidebar.
type sideItem struct {
	label  string
	active bool
	child  bool
}

// viewCtx is the persistent chrome the flow supplies with each field: the
// breadcrumb, the current target, and the sidebar rows (populated in T21).
type viewCtx struct {
	breadcrumb string
	target     string
	sidebar    []sideItem
	hero       bool // Home draws the wordmark hero instead of header + sidebar
}

// fieldReq asks the model to render one field and reply with the answer. It
// carries plain data, never a huh type, so the interactive path imports no huh.
type fieldReq struct {
	kind     fieldKind
	title    string
	desc     string
	choices  []choice
	initial  string
	backable bool
	validate func(string) error
	ctx      viewCtx
	reply    chan fieldReply
}

type fieldReply struct {
	value string
	back  bool
}

// noticeRequest streams a command's combined output into a scrollable panel.
type noticeRequest struct {
	title string
	run   func(io.Writer) error
	done  chan error
}

type noticeState struct {
	title  string
	lines  []string
	offset int
	height int
	done   chan error
}

type noticeDoneMsg struct{ text string }

// ---- the model ----

type console struct {
	requests <-chan shellRequest
	width    int
	height   int

	req     *fieldReq
	cursor  int
	input   textinput.Model
	area    textarea.Model
	errMsg  string
	palette *paletteState

	notice *noticeState
}

// paletteState is the fuzzy command palette overlaid on a menu (V45). It filters
// the current menu's rows; from the editor hub that includes every section jump.
type paletteState struct {
	query  string
	cursor int
}

func newConsole(requests <-chan shellRequest) console {
	in := textinput.New()
	in.Prompt = "› "
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	return console{requests: requests, input: in, area: ta}
}

func (m console) Init() tea.Cmd { return waitRequest(m.requests) }

func (m console) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.area.SetWidth(max(4, m.editorInner()-2))
		m.area.SetHeight(8)
		m.input.Width = max(4, m.editorInner()-4)
		if m.notice != nil {
			m.notice.height = max(3, msg.Height-6)
		}
		return m, nil
	case requestMsg:
		if !msg.ok {
			return m, tea.Quit
		}
		switch r := msg.request.(type) {
		case fieldReq:
			return m.startField(r)
		case noticeRequest:
			m.notice = &noticeState{title: r.title, height: max(3, m.height-6), done: r.done}
			run := r.run
			return m, func() tea.Msg {
				var report bytes.Buffer
				if err := run(&report); err != nil {
					fmt.Fprintf(&report, "error: %v\n", err)
				}
				return noticeDoneMsg{text: report.String()}
			}
		}
	case noticeDoneMsg:
		if m.notice != nil {
			m.notice.lines = strings.Split(strings.TrimRight(msg.text, "\n"), "\n")
		}
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.notice != nil {
			return m.updateNotice(msg)
		}
		if m.req != nil {
			return m.updateField(msg)
		}
	}
	return m, nil
}

func (m console) startField(r fieldReq) (tea.Model, tea.Cmd) {
	req := r
	m.req = &req
	m.errMsg = ""
	switch r.kind {
	case kindSelect:
		m.cursor = 0
		for i, c := range r.choices {
			if c.value == r.initial {
				m.cursor = i
				break
			}
		}
		return m, nil
	case kindInput:
		m.input.SetValue(r.initial)
		m.input.CursorEnd()
		return m, m.input.Focus()
	case kindText:
		m.area.SetValue(r.initial)
		return m, m.area.Focus()
	}
	return m, nil
}

func (m console) updateField(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.palette != nil {
		return m.updatePalette(msg)
	}
	switch m.req.kind {
	case kindSelect:
		switch msg.String() {
		case "ctrl+p", ":":
			if !m.req.ctx.hero {
				m.palette = &paletteState{}
			}
			return m, nil
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.req.choices)-1 {
				m.cursor++
			}
		case "home", "g":
			m.cursor = 0
		case "end", "G":
			m.cursor = len(m.req.choices) - 1
		case "enter":
			return m.answer(fieldReply{value: m.req.choices[m.cursor].value})
		case "esc":
			if m.req.backable {
				return m.answer(fieldReply{value: actionBack, back: true})
			}
		}
		return m, nil
	case kindInput:
		switch msg.String() {
		case "enter":
			if m.req.validate != nil {
				if err := m.req.validate(m.input.Value()); err != nil {
					m.errMsg = err.Error()
					return m, nil
				}
			}
			return m.answer(fieldReply{value: m.input.Value()})
		case "esc":
			if m.req.backable {
				return m.answer(fieldReply{value: actionBack, back: true})
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	case kindText:
		switch msg.String() {
		case "ctrl+d":
			return m.answer(fieldReply{value: m.area.Value()})
		case "esc":
			if m.req.backable {
				return m.answer(fieldReply{value: actionBack, back: true})
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.area, cmd = m.area.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m console) updateNotice(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	maxOff := max(0, len(m.notice.lines)-m.notice.height)
	switch msg.String() {
	case "up", "k":
		m.notice.offset = max(0, m.notice.offset-1)
	case "down", "j":
		m.notice.offset = min(maxOff, m.notice.offset+1)
	case "pgup":
		m.notice.offset = max(0, m.notice.offset-m.notice.height)
	case "pgdown":
		m.notice.offset = min(maxOff, m.notice.offset+m.notice.height)
	case "enter", "esc":
		m.notice.done <- nil
		m.notice = nil
		return m, waitRequest(m.requests)
	}
	return m, nil
}

func (m console) updatePalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	matches := m.paletteMatches()
	switch msg.String() {
	case "esc":
		m.palette = nil
	case "up", "ctrl+k":
		if m.palette.cursor > 0 {
			m.palette.cursor--
		}
	case "down", "ctrl+j":
		if m.palette.cursor < len(matches)-1 {
			m.palette.cursor++
		}
	case "enter":
		if len(matches) > 0 {
			idx := matches[m.palette.cursor]
			m.palette = nil
			return m.answer(fieldReply{value: m.req.choices[idx].value})
		}
	case "backspace":
		if m.palette.query != "" {
			m.palette.query = m.palette.query[:len(m.palette.query)-1]
			m.palette.cursor = 0
		}
	default:
		if len(msg.Runes) == 1 {
			m.palette.query += string(msg.Runes)
			m.palette.cursor = 0
		}
	}
	return m, nil
}

// paletteMatches returns the indices of the current menu's rows that fuzzy-match
// the palette query.
func (m console) paletteMatches() []int {
	var out []int
	for i, c := range m.req.choices {
		if fuzzyMatch(m.palette.query, c.label) {
			out = append(out, i)
		}
	}
	return out
}

// fuzzyMatch reports whether query is a case-insensitive subsequence of s.
func fuzzyMatch(query, s string) bool {
	query, s = strings.ToLower(query), strings.ToLower(s)
	j := 0
	for i := 0; i < len(s) && j < len(query); i++ {
		if s[i] == query[j] {
			j++
		}
	}
	return j == len(query)
}

// answer replies to the blocked flow goroutine and waits for the next request.
func (m console) answer(r fieldReply) (tea.Model, tea.Cmd) {
	if m.req != nil && m.req.reply != nil {
		m.req.reply <- r
	}
	m.req = nil
	m.input.Blur()
	m.area.Blur()
	return m, waitRequest(m.requests)
}

// editorInner is the usable inner width of the editor panel at the current size.
func (m console) editorInner() int {
	w := m.width - m.sidebarWidth() - 1
	if w < 10 {
		w = m.width
	}
	return max(10, w-4) // minus border + padding
}

// sidebarWidth reserves the sidebar column only when the terminal is wide
// enough; below 80 cols the console drops to a single editor pane (C17, V46).
func (m console) sidebarWidth() int {
	if m.width < 80 || m.req == nil || len(m.req.ctx.sidebar) == 0 {
		return 0
	}
	return 22
}

// minWidth/minHeight are the floor below which the console shows a
// "terminal too small" message instead of a layout (C17, V46).
const (
	minWidth  = 60
	minHeight = 20
)

func (r *fieldRunner) runProgram(flow func() (Result, error)) (Result, error) {
	r.requests = make(chan shellRequest)
	type flowResult struct {
		result Result
		err    error
	}
	resultCh := make(chan flowResult, 1)
	go func() {
		res, err := flow()
		resultCh <- flowResult{res, err}
		close(r.requests)
	}()
	_, programErr := tea.NewProgram(newConsole(r.requests),
		tea.WithInput(r.in), tea.WithOutput(r.out), tea.WithAltScreen()).Run()
	if programErr != nil && !errors.Is(programErr, tea.ErrInterrupted) {
		return Result{}, programErr
	}
	select {
	case fr := <-resultCh:
		return fr.result, fr.err
	default:
		return Result{}, nil // quit before the flow finished
	}
}

// sendField hands one field to the model and blocks until the user answers.
func (r *fieldRunner) sendField(spec fieldReq) fieldReply {
	spec.reply = make(chan fieldReply, 1)
	r.requests <- spec
	return <-spec.reply
}

func (r *fieldRunner) runNotice(title string, run func(io.Writer) error) error {
	if r.accessible {
		var report bytes.Buffer
		if err := run(&report); err != nil {
			fmt.Fprintf(&report, "error: %v\n", err)
		}
		return showNotice(r, title, report.String())
	}
	done := make(chan error, 1)
	r.requests <- noticeRequest{title: title, run: run, done: done}
	return <-done
}
