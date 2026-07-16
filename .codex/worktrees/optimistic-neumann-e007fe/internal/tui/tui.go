// Package tui gathers the basic configuration for local agent scaffolds.
package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/scaffold"
)

const (
	otherLLM      = "other (type route)"
	actionCreate  = "create"
	actionQuit    = "quit"
	actionBack    = ":back"
	agentNameHelp = "Choose a unique name for your agent. This name is also used for its local directory."
)

// Agent is the editable basic configuration loaded by the CLI.
type Agent struct {
	Path   string
	Data   scaffold.Data
	Prompt string
}

// Result is one confirmed create/edit action. A zero Result means Quit.
type Result struct {
	Agent     Agent
	Original  Agent
	Create    bool
	Compile   bool
	Targets   []string
	Confirmed bool
}

// Run displays the home and agent menus without reading or writing files.
func Run(in io.Reader, out io.Writer, accessible bool, targets []string, agents []Agent) (Result, error) {
	runner := newRunner(in, out, accessible)
	for {
		choice := actionCreate
		options := []huh.Option[string]{huh.NewOption("Create a new agent", actionCreate)}
		for index, agent := range agents {
			label := fmt.Sprintf("%s  (%s)", agent.Data.Name, filepath.Base(agent.Path))
			options = append(options, huh.NewOption(label, "edit:"+strconv.Itoa(index)))
		}
		options = append(options, huh.NewOption("Quit", actionQuit))
		if _, err := runner.run(huh.NewSelect[string]().
			Title("What would you like to do?").
			Options(options...).
			Value(&choice), false); err != nil {
			return Result{}, err
		}

		switch {
		case choice == actionQuit:
			return Result{}, nil
		case choice == actionCreate:
			path := ""
			back, err := runner.input("Agent name", agentNameHelp, &path, validateName)
			if err != nil {
				return Result{}, err
			}
			if back {
				continue
			}
			agent := Agent{
				Path: path,
				Data: scaffold.Data{
					Name:     filepath.Base(filepath.Clean(path)),
					Greeting: scaffold.DefaultGreeting,
					Language: scaffold.DefaultLanguage,
					LLMModel: scaffold.DefaultLLMModel,
					STTModel: scaffold.DefaultSTTModel,
					TTSModel: scaffold.DefaultTTSModel,
					TTSVoice: scaffold.DefaultTTSVoice,
				},
			}
			result, back, err := editAgent(runner, agent, true, targets)
			if err != nil {
				return Result{}, err
			}
			if !back {
				return result, nil
			}
		case strings.HasPrefix(choice, "edit:"):
			index, _ := strconv.Atoi(strings.TrimPrefix(choice, "edit:"))
			result, back, err := editAgent(runner, agents[index], false, targets)
			if err != nil {
				return Result{}, err
			}
			if !back {
				return result, nil
			}
		}
	}
}

func editAgent(runner *fieldRunner, agent Agent, create bool, targets []string) (Result, bool, error) {
	result := Result{Agent: agent, Original: agent, Create: create}
	for {
		choice := "prompt"
		options := []huh.Option[string]{
			huh.NewOption("Agent prompt", "prompt"),
			huh.NewOption("LLM  ·  "+result.Agent.Data.LLMModel, "llm"),
			huh.NewOption("TTS  ·  "+result.Agent.Data.TTSModel+" / "+result.Agent.Data.TTSVoice, "tts"),
			huh.NewOption("STT  ·  "+result.Agent.Data.STTModel, "stt"),
			huh.NewOption("Greeting  ·  "+result.Agent.Data.Greeting, "greeting"),
			huh.NewOption("Language  ·  "+result.Agent.Data.Language, "language"),
		}
		if create {
			compile := "off"
			if result.Compile {
				compile = strings.Join(result.Targets, ", ")
			}
			options = append(options, huh.NewOption("Compile after create  ·  "+compile, "compile"))
		}
		action := "Save changes"
		if create {
			action = "Create agent"
		}
		options = append(options,
			huh.NewOption(action, "save"),
			huh.NewOption("← Back", actionBack),
		)

		back, err := runner.run(huh.NewSelect[string]().
			Title(result.Agent.Data.Name).
			Description("Choose a section; changes stay in memory until "+action+".").
			Options(options...).
			Value(&choice), true)
		if err != nil {
			return Result{}, false, err
		}
		if back || choice == actionBack {
			return Result{}, true, nil
		}

		switch choice {
		case "prompt":
			description := "Edit agent/prompt/identity.md."
			if create {
				description = "Blank keeps the generated identity prompt."
			}
			if _, err := runner.text("Agent prompt", description, &result.Agent.Prompt); err != nil {
				return Result{}, false, err
			}
		case "llm":
			if err := editLLM(runner, &result.Agent.Data.LLMModel); err != nil {
				return Result{}, false, err
			}
		case "tts":
			if err := editTTS(runner, &result.Agent.Data); err != nil {
				return Result{}, false, err
			}
		case "stt":
			if _, err := runner.input("STT model route", "", &result.Agent.Data.STTModel, validateBasic); err != nil {
				return Result{}, false, err
			}
		case "greeting":
			if _, err := runner.input("Greeting", "", &result.Agent.Data.Greeting, validateBasic); err != nil {
				return Result{}, false, err
			}
		case "language":
			if _, err := runner.input("Language", "", &result.Agent.Data.Language, validateBasic); err != nil {
				return Result{}, false, err
			}
		case "compile":
			if err := editCompile(runner, &result, targets); err != nil {
				return Result{}, false, err
			}
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

func editLLM(runner *fieldRunner, model *string) error {
	for {
		selected := *model
		if !slices.Contains(ir.LLMCatalogModels(), selected) {
			selected = otherLLM
		}
		options := append(llmOptions(), huh.NewOption("← Back", actionBack))
		back, err := runner.run(huh.NewSelect[string]().
			Title("LLM model").
			Options(options...).
			Value(&selected), true)
		if err != nil {
			return err
		}
		if back || selected == actionBack {
			return nil
		}
		if selected != otherLLM {
			*model = selected
			return nil
		}
		route := *model
		back, err = runner.input("LLM route", "", &route, validateRequired)
		if err != nil {
			return err
		}
		if !back {
			*model = route
			return nil
		}
	}
}

func editTTS(runner *fieldRunner, data *scaffold.Data) error {
	for {
		choice := "model"
		back, err := runner.run(huh.NewSelect[string]().
			Title("TTS").
			Options(
				huh.NewOption("Model  ·  "+data.TTSModel, "model"),
				huh.NewOption("Voice  ·  "+data.TTSVoice, "voice"),
				huh.NewOption("← Back", actionBack),
			).
			Value(&choice), true)
		if err != nil {
			return err
		}
		if back || choice == actionBack {
			return nil
		}
		if choice == "model" {
			if _, err := runner.input("TTS model route", "", &data.TTSModel, validateBasic); err != nil {
				return err
			}
		} else if _, err := runner.input("TTS voice", "", &data.TTSVoice, validateBasic); err != nil {
			return err
		}
	}
}

func editCompile(runner *fieldRunner, result *Result, targets []string) error {
	for {
		choice := "disabled"
		if result.Compile {
			choice = "enabled"
		}
		back, err := runner.run(huh.NewSelect[string]().
			Title("Compile after create?").
			Options(
				huh.NewOption("No", "disabled"),
				huh.NewOption("Yes", "enabled"),
				huh.NewOption("← Back", actionBack),
			).
			Value(&choice), true)
		if err != nil {
			return err
		}
		if back || choice == actionBack {
			return nil
		}
		if choice == "disabled" {
			result.Compile = false
			result.Targets = nil
			return nil
		}

		selected := append([]string(nil), result.Targets...)
		if len(selected) == 0 {
			selected = append(selected, targets...)
		}
		options := make([]huh.Option[string], 0, len(targets)+1)
		for _, target := range targets {
			options = append(options, huh.NewOption(target, target).Selected(slices.Contains(selected, target)))
		}
		options = append(options, huh.NewOption("← Back", actionBack))
		back, err = runner.run(huh.NewMultiSelect[string]().
			Title("Compile targets").
			Options(options...).
			Value(&selected).
			Validate(func(selected []string) error {
				if slices.Contains(selected, actionBack) {
					return nil
				}
				if len(selected) == 0 {
					return errors.New("select at least one target")
				}
				return nil
			}), true)
		if err != nil {
			return err
		}
		if back || slices.Contains(selected, actionBack) {
			continue
		}
		result.Compile = true
		result.Targets = selected
		return nil
	}
}

func llmModels() []string {
	return append(ir.LLMCatalogModels(), otherLLM)
}

func llmOptions() []huh.Option[string] {
	models := llmModels()
	options := make([]huh.Option[string], 0, len(models))
	for _, model := range models {
		options = append(options, huh.NewOption(model, model))
	}
	return options
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

func validateRequired(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("value must not be empty")
	}
	return validateBasic(value)
}

func summary(result Result) string {
	action := "Save changes to"
	if result.Create {
		action = "Create"
	}
	compile := "no"
	if result.Compile {
		compile = strings.Join(result.Targets, ", ")
	}
	prompt := "current"
	if result.Create && result.Agent.Prompt == "" {
		prompt = "generated default"
	} else if result.Agent.Prompt != result.Original.Prompt {
		prompt = "edited"
	}
	return fmt.Sprintf(
		"%s %s?\nPrompt: %s\nGreeting: %s\nLanguage: %s\nLLM: %s\nSTT: %s\nTTS: %s / %s\nCompile: %s",
		action,
		result.Agent.Data.Name,
		prompt,
		result.Agent.Data.Greeting,
		result.Agent.Data.Language,
		result.Agent.Data.LLMModel,
		result.Agent.Data.STTModel,
		result.Agent.Data.TTSModel,
		result.Agent.Data.TTSVoice,
		compile,
	)
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
	keymap.MultiSelect.Submit.SetHelp("esc back • enter", "confirm")
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
