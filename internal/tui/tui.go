// Package tui gathers the basic configuration for a new v1 agent scaffold.
package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/slng/unmute/internal/scaffold"
)

const (
	actionCreate  = "create"
	actionQuit    = "quit"
	actionBack    = ":back"
	agentNameHelp = "Choose a unique name for your agent. This name is also used for its local directory."
)

// Agent is the basic configuration for a scaffold.
type Agent struct {
	Path string
	Data scaffold.Data
}

// Result is one confirmed create action. A zero Result means Quit.
type Result struct {
	Agent     Agent
	Create    bool
	Compile   bool
	Confirmed bool
}

// Run displays the create wizard without reading or writing files.
func Run(in io.Reader, out io.Writer, accessible bool) (Result, error) {
	runner := newRunner(in, out, accessible)
	for {
		choice := actionCreate
		if _, err := runner.run(huh.NewSelect[string]().
			Title("What would you like to do?").
			Options(
				huh.NewOption("Create a new agent", actionCreate),
				huh.NewOption("Quit", actionQuit),
			).
			Value(&choice), false); err != nil {
			return Result{}, err
		}
		if choice == actionQuit {
			return Result{}, nil
		}
		path := ""
		back, err := runner.input("Agent name", agentNameHelp, &path, validateName)
		if err != nil {
			return Result{}, err
		}
		if back {
			continue
		}
		agent := Agent{Path: path, Data: scaffold.Data{
			Name:         filepath.Base(filepath.Clean(path)),
			Greeting:     scaffold.DefaultGreeting,
			Instructions: scaffold.DefaultInstructions,
		}}
		result, back, err := editAgent(runner, agent)
		if err != nil {
			return Result{}, err
		}
		if !back {
			return result, nil
		}
	}
}

func editAgent(runner *fieldRunner, agent Agent) (Result, bool, error) {
	result := Result{Agent: agent, Create: true}
	for {
		choice := "prompt"
		compile := "off"
		if result.Compile {
			compile = "on"
		}
		back, err := runner.run(huh.NewSelect[string]().
			Title(result.Agent.Data.Name).
			Description("Choose a section; changes stay in memory until Create agent.").
			Options(
				huh.NewOption("Instructions (prompt)", "prompt"),
				huh.NewOption("Greeting  ·  "+result.Agent.Data.Greeting, "greeting"),
				huh.NewOption("Compile after create  ·  "+compile, "compile"),
				huh.NewOption("Create agent", "save"),
				huh.NewOption("← Back", actionBack),
			).
			Value(&choice), true)
		if err != nil {
			return Result{}, false, err
		}
		if back || choice == actionBack {
			return Result{}, true, nil
		}
		switch choice {
		case "prompt":
			if _, err := runner.text("Instructions", "Blank keeps the generated default.", &result.Agent.Data.Instructions); err != nil {
				return Result{}, false, err
			}
		case "greeting":
			if _, err := runner.input("Greeting", "", &result.Agent.Data.Greeting, validateBasic); err != nil {
				return Result{}, false, err
			}
		case "compile":
			result.Compile = !result.Compile
		case "save":
			confirmed := true
			back, err := runner.run(huh.NewConfirm().Title(summary(result)).Value(&confirmed), true)
			if err != nil {
				return Result{}, false, err
			}
			if !back && confirmed {
				result.Confirmed = true
				return result, false, nil
			}
		}
	}
}

func summary(result Result) string {
	compile := "no"
	if result.Compile {
		compile = "yes (all targets)"
	}
	return fmt.Sprintf("Create %s?\nGreeting: %s\nCompile: %s", result.Agent.Data.Name, result.Agent.Data.Greeting, compile)
}

func validateName(name string) error {
	if filepath.IsAbs(name) {
		return errors.New("agent directory must be relative")
	}
	base := filepath.Base(filepath.Clean(name))
	if strings.TrimSpace(name) == "" || strings.TrimSpace(base) == "" || strings.ContainsAny(base, `/\`) {
		return errors.New("agent name must be nonempty and contain no path separators")
	}
	entries, err := os.ReadDir(name)
	if err == nil && len(entries) > 0 {
		return fmt.Errorf("%s: %w", name, scaffold.ErrExists)
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("check %s: %w", name, err)
	}
	return nil
}

func validateBasic(value string) error {
	if strings.ContainsAny(value, "\"\r\n") {
		return errors.New(`value must not contain quotes or newlines`)
	}
	return nil
}

type fieldRunner struct {
	in         io.Reader
	out        io.Writer
	accessible bool
	tracked    *eofReader
}

func newRunner(in io.Reader, out io.Writer, accessible bool) *fieldRunner {
	runner := &fieldRunner{in: in, out: out, accessible: accessible}
	if accessible {
		runner.tracked = &eofReader{Reader: in}
		runner.in = runner.tracked
	}
	return runner
}

func (r *fieldRunner) run(field huh.Field, backable bool) (bool, error) {
	form := huh.NewForm(huh.NewGroup(field)).
		WithInput(r.in).
		WithOutput(r.out).
		WithAccessible(r.accessible)
	if backable && !r.accessible {
		form.WithKeyMap(backKeyMap())
	}
	err := form.Run()
	if r.tracked != nil && r.tracked.eof {
		return false, fmt.Errorf("menu: %w", huh.ErrUserAborted)
	}
	if err != nil {
		if backable && errors.Is(err, huh.ErrUserAborted) {
			return true, nil
		}
		return false, fmt.Errorf("menu: %w", err)
	}
	return false, nil
}

func backKeyMap() *huh.KeyMap {
	keymap := huh.NewDefaultKeyMap()
	keymap.Quit.SetKeys("esc", "ctrl+c")
	// ponytail: Huh omits form-level Quit from field help, so include Esc in
	// submit help until Huh exposes custom footer bindings.
	keymap.Input.Submit.SetHelp("esc back • enter", "submit")
	keymap.Text.Submit.SetHelp("esc back • enter", "submit")
	keymap.Select.Submit.SetHelp("esc back • enter", "select")
	keymap.Confirm.Submit.SetHelp("esc back • enter", "submit")
	return keymap
}

func (r *fieldRunner) input(title, description string, value *string, validate func(string) error) (bool, error) {
	temporary := *value
	back, err := r.run(huh.NewInput().
		Title(title).
		Description(r.describe(description)).
		Value(&temporary).
		Validate(allowBack(validate)), true)
	if err != nil || back || strings.TrimSpace(temporary) == actionBack {
		return back || strings.TrimSpace(temporary) == actionBack, err
	}
	*value = temporary
	return false, nil
}

func (r *fieldRunner) text(title, description string, value *string) (bool, error) {
	temporary := *value
	back, err := r.run(huh.NewText().
		Title(title).
		Description(r.describe(description)).
		Lines(10).
		ExternalEditor(false).
		Value(&temporary).
		Validate(allowBack(func(string) error { return nil })), true)
	if err != nil || back || strings.TrimSpace(temporary) == actionBack {
		return back || strings.TrimSpace(temporary) == actionBack, err
	}
	*value = temporary
	return false, nil
}

func (r *fieldRunner) describe(description string) string {
	if r.accessible {
		if description != "" {
			fmt.Fprintln(r.out, description)
		}
		fmt.Fprintln(r.out, "Type :back to return.")
	}
	return description
}

func allowBack(validate func(string) error) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == actionBack {
			return nil
		}
		return validate(value)
	}
}

// Huh v1 creates a new scanner for each accessible field. Limiting reads to one
// byte prevents one field from buffering later answers and lets us observe EOF.
type eofReader struct {
	io.Reader
	eof bool
}

func (r *eofReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	n, err := r.Reader.Read(p)
	if errors.Is(err, io.EOF) {
		r.eof = true
	}
	return n, err
}
