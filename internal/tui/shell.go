package tui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

type formRequest struct {
	form *huh.Form
	done chan error
}

type noticeRequest struct {
	title string
	run   func(io.Writer) error
	done  chan error
}

type shellRequest interface{}

type requestMsg struct {
	request shellRequest
	ok      bool
}

type noticeDoneMsg struct {
	text string
}

type noticeState struct {
	title  string
	lines  []string
	offset int
	height int
	done   chan error
}

type programShell struct {
	requests <-chan shellRequest
	current  *huh.Form
	done     chan error
	notice   *noticeState
}

func waitRequest(requests <-chan shellRequest) tea.Cmd {
	return func() tea.Msg {
		request, ok := <-requests
		return requestMsg{request: request, ok: ok}
	}
}

func (m programShell) Init() tea.Cmd { return waitRequest(m.requests) }

func (m programShell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case requestMsg:
		if !msg.ok {
			return m, tea.Quit
		}
		switch request := msg.request.(type) {
		case formRequest:
			m.current = request.form
			m.done = request.done
			return m, m.current.Init()
		case noticeRequest:
			m.notice = &noticeState{title: request.title, height: 20, done: request.done}
			return m, func() tea.Msg {
				var report bytes.Buffer
				if err := request.run(&report); err != nil {
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
	case tea.WindowSizeMsg:
		if m.notice != nil {
			m.notice.height = max(3, msg.Height-4)
			return m, nil
		}
	case tea.KeyMsg:
		if m.notice != nil {
			switch msg.String() {
			case "up", "k":
				m.notice.offset = max(0, m.notice.offset-1)
			case "down", "j":
				m.notice.offset = min(max(0, len(m.notice.lines)-m.notice.height), m.notice.offset+1)
			case "pgup":
				m.notice.offset = max(0, m.notice.offset-m.notice.height)
			case "pgdown":
				m.notice.offset = min(max(0, len(m.notice.lines)-m.notice.height), m.notice.offset+m.notice.height)
			case "enter", "esc":
				m.notice.done <- nil
				m.notice = nil
				return m, waitRequest(m.requests)
			}
			return m, nil
		}
	}
	if m.notice != nil {
		return m, nil
	}
	if m.current == nil {
		return m, nil
	}
	model, cmd := m.current.Update(msg)
	m.current = model.(*huh.Form)
	if m.current.State == huh.StateNormal {
		return m, cmd
	}
	err := error(nil)
	if m.current.State == huh.StateAborted {
		err = huh.ErrUserAborted
	}
	m.done <- err
	m.current, m.done = nil, nil
	return m, waitRequest(m.requests)
}

func (m programShell) View() string {
	if m.notice != nil {
		end := min(len(m.notice.lines), m.notice.offset+m.notice.height)
		lines := m.notice.lines[m.notice.offset:end]
		return m.notice.title + "\n\n" + strings.Join(lines, "\n") + "\n\n↑/↓ scroll • enter/esc back"
	}
	if m.current == nil {
		return ""
	}
	return m.current.View()
}

func (r *fieldRunner) runProgram(flow func() (Result, error)) (Result, error) {
	r.requests = make(chan shellRequest)
	var result Result
	var flowErr error
	go func() {
		result, flowErr = flow()
		close(r.requests)
	}()
	_, programErr := tea.NewProgram(programShell{requests: r.requests},
		tea.WithInput(r.in), tea.WithOutput(r.out), tea.WithAltScreen()).Run()
	if programErr != nil && !errors.Is(programErr, tea.ErrInterrupted) {
		return Result{}, programErr
	}
	return result, flowErr
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
