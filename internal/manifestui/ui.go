// Package manifestui edits company contracts independently of the agent console.
package manifestui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/goccy/go-yaml"
	"github.com/slng-ai/unmute/internal/manifest"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/style"
)

// Options supplies storage operations; the editor owns only the unsaved draft.
type Options struct {
	Name                string
	Rules               *spec.Manifest
	Editing, HasDefault bool
	Destination         func(string) string
	ValidateName        func(string) error
	Save                func(name string, rules spec.Manifest, makeDefault bool) error
}

type item struct {
	label string
	run   func()
}
type field struct {
	label    string
	input    textinput.Model
	validate func(string) error
	err      string
}

const formHelp = "Use Tab to move between fields, Apply and Back. Shift+Tab moves to the previous field or action. Arrow keys do not move between fields. Press Enter on Apply to keep this entry in the draft. Esc cancels this entry. F1 shows help; F2 switches sections."

type form struct {
	after   func()
	help    string
	fields  []field
	initial []string
	focus   int
	apply   func([]string) error
}

// menuSection separates data rows from the actions that operate on them.
type menuSection struct {
	start    int
	title    string
	numbered int
}

type page struct {
	help           string
	sections       []menuSection
	controls       int // Number of trailing menu rows in the separate control group.
	title          string
	build          func() ([]item, string)
	form           *form
	pending        func() bool
	cursor, offset int
	review         bool
	exitPrompt     bool
}
type clipboardText struct {
	text string
	err  error
}

type model struct {
	options          Options
	draft            spec.Manifest
	pages            []*page
	width, height    int
	err              string
	done             bool
	makeDefault      bool
	quitting         bool
	sectionPositions []struct {
		cursor, offset int
		visited        bool
	}
}

// Run starts one terminal session. Plain mode uses the same actions and forms.
func Run(in io.Reader, out io.Writer, plain bool, options Options) error {
	m, err := newModel(options)
	if err != nil {
		return err
	}
	if plain {
		return m.runPlain(in, out)
	}
	_, err = tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out), tea.WithAltScreen()).Run()
	return err
}

func newModel(options Options) (*model, error) {
	m := &model{options: options, draft: spec.Manifest{Name: options.Name, Version: 1}, sectionPositions: make([]struct {
		cursor, offset int
		visited        bool
	}, len(sectionNames))}
	if options.Rules != nil {
		data, err := yaml.Marshal(options.Rules)
		if err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(data, &m.draft); err != nil {
			return nil, err
		}
	}
	m.push(m.home())
	if options.Name == "" {
		m.openForm("Manifest name", []string{"Local name"}, []string{""}, []func(string) error{m.validateName}, func(values []string) error {
			m.options.Name, m.draft.Name = values[0], values[0]
			return nil
		})
	}
	return m, nil
}
func (m *model) Init() tea.Cmd  { return textinput.Blink }
func (m *model) current() *page { return m.pages[len(m.pages)-1] }
func (m *model) push(p *page)   { m.pages = append(m.pages, p); m.err = "" }
func (m *model) back() {
	if m.current().exitPrompt {
		m.quitting = false
	}
	if len(m.pages) == 2 && m.options.Name == "" && !m.quitting {
		m.exit()
		return
	}
	if len(m.pages) > 1 {
		m.pages = m.pages[:len(m.pages)-1]
		m.err = ""
		return
	}
	m.exit()
}
func (m *model) dirty() bool {
	return !m.options.Editing || m.options.Rules == nil || !reflect.DeepEqual(m.options.Rules, &m.draft)
}
func (m *model) exit() {
	if m.quitting {
		return
	}
	if !m.dirty() && len(m.pages) == 1 {
		m.done = true
		return
	}
	m.quitting = true
	m.push(&page{title: "Discard unsaved changes?", exitPrompt: true, build: func() ([]item, string) {
		return []item{
			{"Keep editing", func() { m.quitting = false; m.back() }},
			{"Discard changes and exit", func() { m.done = true }},
		}, "Unsaved edits will be discarded."
	}})
}
func (m *model) confirm(title, description string, action func()) {
	m.push(&page{title: title, build: func() ([]item, string) {
		return []item{{"Keep editing", m.back}, {"Confirm", func() { m.back(); action() }}}, description
	}})
}
func (m *model) help() {
	if m.current().title == "Help" {
		return
	}
	context := m.current().help
	if context != "" {
		context = "About this screen\n" + context + "\n\n"
	}
	m.push(&page{title: "Help", review: true, build: func() ([]item, string) {
		return []item{{"Back", m.back}}, context + `Navigation
Menus: use ↑/↓ to choose a row, then Enter to open it.
Forms: use Tab to move between fields, Apply and Back. Shift+Tab moves to the previous field or action. Arrow keys do not move between fields. Type or paste your value, then move to Apply and press Enter.
Esc returns to the previous screen. F2 opens the section chooser; Back resumes your current screen. F1 opens this help without changing your draft. Page Up/Down scrolls this page and the review.

Several providers and models
Choose Models, then listen (STT), speak (TTS), or think.
Use Add provider for each service you connect to. SLNG is a service provider; it can serve models from multiple model makers.
Use Add model ID inside a provider for every allowed model. Apply each completed entry once. Back returns to the provider list, where Add provider adds another service. Completed changes stay in the draft as you navigate.

Keeping your work
Apply keeps a completed form in the draft. Back cancels unfinished form input; on lists it navigates back and keeps completed edits. Only Review and save → Save manifest writes to disk.
Ctrl+C offers Keep editing or Discard changes. Existing saved files stay untouched until you save.

Plain prompts
Enter the number shown beside an action. Type values one line at a time, then choose Apply. :back returns, :sections opens the section chooser, :help opens help, and :quit asks to exit.`
	}})
}

// Rebuild section pages from the current draft rather than keeping closures over
// entries that may have been replaced or removed in another section.
func (m *model) sections() {
	if m.quitting || m.current().title == "Sections" {
		return
	}
	if m.options.Name == "" {
		m.err = "Enter the manifest name before switching sections."
		return
	}
	source := append([]*page(nil), m.pages...)
	resume := func() { m.pages = source; m.err = "" }
	m.push(&page{title: "Sections", controls: 1, build: func() ([]item, string) {
		rows := make([]item, 0, len(sectionNames)+1)
		for index, name := range sectionNames {
			rows = append(rows, item{name, func() {
				if len(source) > 1 && source[1].title == name {
					resume()
					return
				}
				switchSection := func() {
					if len(source) > 1 {
						for i, label := range sectionNames {
							if source[1].title == label {
								m.sectionPositions[i].cursor = source[1].cursor
								m.sectionPositions[i].offset = source[1].offset
								m.sectionPositions[i].visited = true
							}
						}
					}
					m.pages = source[:1]
					choices, _ := m.pages[0].build()
					m.pages[0].cursor = index
					choices[index].run()
					if position := m.sectionPositions[index]; position.visited {
						m.current().cursor = position.cursor
						m.current().offset = position.offset
					}
					m.err = ""
				}
				for _, p := range source {
					if p.hasPending() {
						m.push(&page{title: "Discard unfinished entry?", build: func() ([]item, string) {
							return []item{{"Keep editing", resume}, {"Discard entry and switch", switchSection}}, "Only this unfinished entry will be discarded. Completed entries stay in your draft."
						}})
						return
					}
				}
				switchSection()
			}})
		}
		return append(rows, item{"Back", resume}), "Choose a section. Completed edits stay in your draft."
	}})
}

func (p *page) hasPending() bool {
	if p.pending != nil && p.pending() {
		return true
	}
	if f := p.form; f != nil {
		for i, field := range f.fields {
			if field.input.Value() != f.initial[i] {
				return true
			}
		}
	}
	return false
}

func (m *model) validateName(value string) error {
	if err := manifest.ValidateName(value); err != nil {
		return err
	}
	if m.options.ValidateName != nil {
		return m.options.ValidateName(value)
	}
	return nil
}
func validateText(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("enter a value")
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("enter one value on one line; line breaks and control characters are not allowed")
	}
	return nil
}
func validateDraft(draft spec.Manifest) error {
	data, err := yaml.Marshal(draft)
	if err != nil {
		return err
	}
	_, err = spec.ParseManifest(data)
	return err
}
func (m *model) openForm(title string, labels, values []string, validators []func(string) error, apply func([]string) error) {
	f := &form{apply: apply, help: formHelp, initial: append([]string(nil), values...)}
	for i, label := range labels {
		input := textinput.New()
		input.Prompt = "› "
		input.CharLimit = 0
		input.SetValue(values[i])
		input.Width = max(5, m.contentWidth()-8)
		f.fields = append(f.fields, field{label: label, input: input, validate: validators[i]})
	}
	f.fields[0].input.Focus()
	m.push(&page{title: title, form: f})
}
func (m *model) focus(focus int) tea.Cmd {
	f := m.current().form
	for i := range f.fields {
		f.fields[i].input.Blur()
	}
	f.focus = (focus + len(f.fields) + 2) % (len(f.fields) + 2)
	if f.focus < len(f.fields) {
		return f.fields[f.focus].input.Focus()
	}
	return nil
}
func (m *model) applyForm() {
	f := m.current().form
	values := make([]string, len(f.fields))
	for i := range f.fields {
		value := f.fields[i].input.Value()
		f.fields[i].err = ""
		if err := f.fields[i].validate(value); err != nil {
			f.fields[i].err = err.Error()
			m.focus(i)
			return
		}
		values[i] = value
	}
	if err := f.apply(values); err != nil {
		m.err = err.Error()
		m.focus(0)
		return
	}
	m.back()
	if f.after != nil {
		f.after()
	}
}
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.done {
		return m, tea.Quit
	}
	switch msg := msg.(type) {
	case clipboardText:
		if msg.err != nil {
			m.err = "Could not paste: " + msg.err.Error()
			return m, nil
		}
		return m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(msg.text), Paste: true})
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		for _, p := range m.pages {
			if p.form != nil {
				for i := range p.form.fields {
					p.form.fields[i].input.Width = max(5, m.contentWidth()-8)
				}
			}
		}
		return m, nil
	case tea.KeyMsg:
		if msg.Paste && (m.width < 60 || m.height < 18) {
			return m, nil
		}
		// Handle bracketed paste before any navigation. Bubbles strips newlines, so
		// reject them here instead of silently changing an authored identifier.
		if msg.Paste {
			if f := m.current().form; f != nil && f.focus < len(f.fields) && strings.IndexFunc(string(msg.Runes), unicode.IsControl) >= 0 {
				f.fields[f.focus].err = "Paste one value on one line."
				return m, nil
			}
		} else {
			if msg.String() == "ctrl+c" {
				m.exit()
				if m.done {
					return m, tea.Quit
				}
				return m, nil
			}
			if m.width < 60 || m.height < 18 {
				if m.quitting && msg.String() == "enter" {
					m.quitting = false
					m.back()
				}
				if m.quitting && msg.String() == "d" {
					m.done = true
					return m, tea.Quit
				}
				return m, nil
			}
			if msg.String() == "f2" {
				m.sections()
				return m, nil
			}
			if msg.String() == "f1" {
				m.help()
				return m, nil
			}
			if msg.String() == "esc" {

				m.back()
				if m.done {
					return m, tea.Quit
				}
				return m, textinput.Blink
			}
		}
		p := m.current()
		if f := p.form; f != nil {
			if !msg.Paste {
				switch msg.String() {
				case "ctrl+v":
					return m, func() tea.Msg {
						pasted := textinput.Paste()
						if err, ok := pasted.(error); ok {
							return clipboardText{err: err}
						}
						return clipboardText{text: fmt.Sprint(pasted)}
					}
				case "tab":
					return m, m.focus(f.focus + 1)
				case "shift+tab":
					return m, m.focus(f.focus - 1)
				case "enter":
					if f.focus < len(f.fields) {
						return m, m.focus(f.focus + 1)
					}
					if f.focus == len(f.fields) {
						m.applyForm()
					} else {
						m.back()
					}
					return m, textinput.Blink
				}
			}
		} else if !msg.Paste {
			choices, _ := p.build()
			switch msg.String() {
			case "up", "shift+tab":
				p.cursor = max(0, p.cursor-1)
			case "down", "tab":
				p.cursor = min(len(choices)-1, p.cursor+1)
			case "home":
				p.cursor = 0
			case "end":
				p.cursor = len(choices) - 1
			case "pgup":
				p.offset = max(0, p.offset-max(1, m.height-12))
				if !p.review {
					p.cursor = max(0, p.cursor-max(1, m.height-12))
				}
			case "pgdown":
				p.offset += max(1, m.height-12)
				if !p.review {
					p.cursor = min(len(choices)-1, p.cursor+max(1, m.height-12))
				}
			case "enter":
				if p.cursor >= 0 && p.cursor < len(choices) {
					choices[p.cursor].run()
				}
			}
			if m.done {
				return m, tea.Quit
			}
			return m, textinput.Blink
		}
	}
	// Blink and clipboard messages must reach the active input too.
	if f := m.current().form; f != nil && f.focus < len(f.fields) {
		var cmd tea.Cmd
		f.fields[f.focus].input, cmd = f.fields[f.focus].input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) contentWidth() int {
	if m.width >= 100 {
		return m.width - 25
	}
	return m.width
}
func wrap(text string, width int) []string {
	if text == "" {
		return nil
	}
	return strings.Split(lipgloss.NewStyle().Width(max(1, width)).Render(text), "\n")
}
func (m *model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.width < 60 || m.height < 18 {
		text := "Resize to at least 60×18. Your draft is kept.\nCtrl+C to exit."
		if m.quitting {
			text = "Unsaved changes.\nEnter: keep editing · d: discard and exit"
		}
		return strings.Join(wrap(text, max(1, m.width)), "\n")
	}
	p := m.current()
	width, height := m.contentWidth(), m.height-4
	inner, room := width-6, height-2
	title := "Manifests"
	for _, page := range m.pages {
		title += " › " + page.title
	}
	header := style.Badge(" UNMUTE// ") + "  " + style.Dim(title)
	headerLines := wrap(header, m.width)
	header = headerLines[0]
	lines := []string{style.Accented(p.title), ""}
	selectedLine := 0
	footer := "↑/↓ move · enter select · esc back · ctrl+c exit · F1 help · F2 sections"
	if f := p.form; f != nil {
		footer = "Tab next · Shift+Tab previous · Enter next/action · Esc back · F1 help · F2 sections"
		lines = append(lines, wrap(style.Dim(f.help), inner)...)
		lines = append(lines, "")
		for i, field := range f.fields {
			if f.focus == i {
				selectedLine = len(lines)
			}
			lines = append(lines, field.label)
			lines = append(lines, field.input.View())
			if field.err != "" {
				lines = append(lines, wrap(style.Errored("Error: "+field.err), inner)...)
			}
			lines = append(lines, "")
		}
		lines = append(lines, style.Dim("CONTROLS"))
		for i, label := range []string{"Apply", "Back"} {
			if f.focus == len(f.fields)+i {
				selectedLine = len(lines)
				label = style.Accented("› " + label)
			} else {
				label = "  " + label
			}
			lines = append(lines, label)
		}
	} else {
		choices, description := p.build()
		p.cursor = max(0, min(p.cursor, len(choices)-1))
		descriptionLines := wrap(description, inner)
		if p.review {
			footer = "↑/↓ actions · pgup/pgdown scroll · enter select · esc back · F1 help · F2 sections"
			groupLines := 2 * len(p.sections)
			if p.controls > 0 {
				groupLines += 2
			}
			available := max(1, room-len(choices)-4-groupLines-len(wrap(m.err, inner)))
			p.offset = min(max(0, p.offset), max(0, len(descriptionLines)-available))
			lines = append(lines, descriptionLines[p.offset:min(len(descriptionLines), p.offset+available)]...)
			lines = append(lines, style.Dim(fmt.Sprintf("Review %d–%d of %d lines", min(len(descriptionLines), p.offset+1), min(len(descriptionLines), p.offset+available), len(descriptionLines))))
		} else {
			lines = append(lines, descriptionLines...)
		}
		for i, choice := range choices {
			if p.controls > 0 && i == len(choices)-p.controls {
				lines = append(lines, "", style.Dim("CONTROLS"))
			}
			text := choice.label
			for _, section := range p.sections {
				if i == section.start {
					lines = append(lines, "", style.Dim(section.title))
				}
				if i >= section.start && i < section.start+section.numbered {
					text = fmt.Sprintf("%02d  %s", i-section.start+1, text)
				}
			}
			label := "  " + text
			if i == p.cursor {
				selectedLine = len(lines)
				label = style.Accented("› " + text)
			}
			lines = append(lines, wrap(label, inner)...)
		}
	}
	if m.err != "" {
		lines = append(lines, wrap(style.Errored("Error: "+m.err), inner)...)
		if !p.review {
			selectedLine = len(lines) - 1
		}
	}
	if !p.review {
		if selectedLine < p.offset {
			p.offset = selectedLine
		}
		if selectedLine >= p.offset+room {
			p.offset = selectedLine - room + 1
		}
		p.offset = min(max(0, p.offset), max(0, len(lines)-room))
		lines = lines[p.offset:min(len(lines), p.offset+room)]
	}
	if len(lines) > room {
		lines = lines[:room]
	}
	body := style.Panel(width, height, true).Render(strings.Join(lines, "\n"))
	if m.width >= 100 {
		side := style.Dim("SECTIONS") + "\n\n"
		for _, label := range sectionNames {
			if len(m.pages) > 1 && m.pages[1].title == label {
				side += style.Accented("› "+label) + "\n"
			} else {
				side += "  " + label + "\n"
			}
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top, style.Panel(24, height, false).Render(side), " ", body)
	}
	return header + "\n" + body + "\n" + strings.Join(wrap(style.Dim(footer), m.width), "\n")
}

func (m *model) runPlain(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for !m.done {
		p := m.current()
		fmt.Fprintf(out, "\n%s\n", p.title)
		if m.err != "" {
			fmt.Fprintln(out, "Error:", m.err)
		}
		if f := p.form; f != nil {
			fmt.Fprintln(out, strings.ReplaceAll(f.help, formHelp, "Enter a value on each line. At the action menu, choose 1 to Apply or 2 to go Back. Type :help for help or :sections to switch sections."))
			if f.focus < len(f.fields) {
				field := &f.fields[f.focus]
				fmt.Fprintf(out, "%s [%s] (:back to cancel): ", field.label, field.input.Value())
				if field.err != "" {
					fmt.Fprintln(out, "Error:", field.err)
				}
			} else {
				fmt.Fprint(out, "\nCONTROLS\n1. Apply\n2. Back\nChoose: ")
			}
		} else {
			choices, description := p.build()
			if description != "" {
				fmt.Fprintln(out, description)
			}
			for i, choice := range choices {
				if p.controls > 0 && i == len(choices)-p.controls {
					fmt.Fprintln(out, "\nCONTROLS")
				}
				for _, section := range p.sections {
					if i == section.start {
						fmt.Fprintln(out, "\n"+section.title)
					}
				}
				fmt.Fprintf(out, "%d. %s\n", i+1, choice.label)
			}
			fmt.Fprint(out, "Choose (:back to return, :sections to switch, :help for help, :quit to exit): ")
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("read manifest input: %w", err)
			}
			return errors.New("manifest editing cancelled: input ended; no unsaved changes were written")
		}
		answer := scanner.Text()
		if answer == ":sections" {
			m.sections()
			continue
		}
		if answer == ":help" {
			m.help()
			continue
		}
		if answer == ":quit" {
			m.exit()
			continue
		}
		if answer == ":back" {

			m.back()
			continue
		}
		if f := p.form; f != nil {
			if f.focus < len(f.fields) {
				if answer != "" {
					f.fields[f.focus].input.SetValue(answer)
				}
				f.focus++
			} else if answer == "1" {
				m.applyForm()
			} else if answer == "2" {
				m.back()
			} else {
				m.err = "Choose 1 or 2."
			}
			continue
		}
		choices, _ := p.build()
		n, err := strconv.Atoi(answer)
		if err != nil || n < 1 || n > len(choices) {
			m.err = "Choose a number from the list."
			continue
		}
		m.err = ""
		choices[n-1].run()
	}
	return nil
}
