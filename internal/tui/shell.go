package tui

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

type formRequest struct {
	form *huh.Form
	done chan error
}

type requestMsg struct {
	request formRequest
	ok      bool
}

type programShell struct {
	requests <-chan formRequest
	current  *huh.Form
	done     chan error
}

func waitRequest(requests <-chan formRequest) tea.Cmd {
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
		m.current = msg.request.form
		m.done = msg.request.done
		return m, m.current.Init()
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
	if m.current == nil {
		return ""
	}
	return m.current.View()
}

func (r *fieldRunner) runProgram(flow func() (Result, error)) (Result, error) {
	r.requests = make(chan formRequest)
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
