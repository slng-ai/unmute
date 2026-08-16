package tui

import (
	"bytes"
	"errors"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/scaffold"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/style"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// renderField drives the interactive console model through one field at a fixed
// terminal size and returns the rendered frame, for asserting on the visible UI
// without a TTY.
func renderField(t *testing.T, w, h int, req fieldReq) string {
	t.Helper()
	m := newConsole(nil)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	shown, _ := sized.(console).Update(requestMsg{ok: true, request: req})
	return shown.(console).View()
}

func TestRunCreateDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	// 1=create, name=agent, 7=Create agent, ""=confirm default (yes).
	got, err := RunConsole(strings.NewReader("1\nagent\n7\n\n"), &output, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := scaffold.Data{Name: "agent", Channel: scaffold.DefaultChannel, Greeting: scaffold.DefaultGreeting, Instructions: scaffold.DefaultInstructions, Tools: scaffold.DefaultTools()}
	data.SetTarget(scaffold.DefaultTarget)
	agent := Agent{Path: "agent", Data: data}
	want := Result{Agent: agent, Create: true, Confirmed: true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Run() = %#v, want %#v", got, want)
	}
	if _, err := os.Stat("agent"); !os.IsNotExist(err) {
		t.Fatalf("TUI wrote agent directory: %v", err)
	}
	for _, label := range []string{"Identity", "Models", "Behavior", "Integrations", "Lifecycle", "Compile after create", "Create agent", "← Back"} {
		if !strings.Contains(output.String(), label) {
			t.Errorf("menu missing %q:\n%s", label, output.String())
		}
	}
	if !strings.Contains(output.String(), "Agent name") || !strings.Contains(output.String(), agentNameHelp) {
		t.Fatalf("name field missing guidance:\n%s", output.String())
	}
	for _, label := range []string{"Required env:", "Forwarded bindings:"} {
		if !strings.Contains(output.String(), label) {
			t.Errorf("review missing %q:\n%s", label, output.String())
		}
	}
}

func TestRunQuit(t *testing.T) {
	got, err := RunConsole(strings.NewReader("3\n"), &bytes.Buffer{}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Confirmed {
		t.Fatalf("quit returned a confirmed result: %#v", got)
	}
}

func TestV23HomeHeroShowsWordmark(t *testing.T) { // docs/spec/tui.md V23
	t.Setenv("NO_COLOR", "1")
	view := renderField(t, 90, 24, fieldReq{kind: kindSelect, ctx: viewCtx{hero: true}, choices: []choice{{"Create a new agent", actionCreate}, {"Quit", actionQuit}}})
	if !strings.Contains(view, "SLNG//") {
		t.Fatalf("home hero omits SLNG wordmark:\n%s", view)
	}
}

func TestV23HeaderBadgeShownOnEditorScreens(t *testing.T) { // docs/spec/tui.md V23
	view := renderField(t, 90, 24, fieldReq{kind: kindSelect, title: "Models", backable: true, ctx: viewCtx{breadcrumb: "Create › Models"}, choices: []choice{{"Listen", "listen"}, {"← Back", actionBack}}})
	if !strings.Contains(view, "SLNG//") {
		t.Fatalf("editor header omits SLNG badge:\n%s", view)
	}
}

func TestV26LogoRendersInsideProgram(t *testing.T) { // docs/spec/tui.md V26
	t.Setenv("NO_COLOR", "1")
	screens := map[string]string{
		"home":   renderField(t, 90, 24, fieldReq{kind: kindSelect, ctx: viewCtx{hero: true}, choices: []choice{{"Quit", actionQuit}}}),
		"editor": renderField(t, 90, 24, fieldReq{kind: kindSelect, title: "Models", backable: true, choices: []choice{{"Listen", "l"}, {"← Back", actionBack}}}),
	}
	for name, view := range screens {
		if !strings.Contains(view, "SLNG//") {
			t.Fatalf("%s screen omits logo:\n%s", name, view)
		}
	}
}

func TestV27RedrawsInPlaceNoScrollback(t *testing.T) { // docs/spec/tui.md V27
	var output bytes.Buffer
	runner := newRunner(strings.NewReader(""), &output, false)
	if _, err := runner.runProgram(func() (Result, error) { return Result{}, nil }); err != nil {
		t.Fatal(err)
	}
	for _, sequence := range []string{"\x1b[?1049h", "\x1b[?1049l"} {
		if got := strings.Count(output.String(), sequence); got != 1 {
			t.Fatalf("alt-screen sequence %q count = %d, want 1", sequence, got)
		}
	}
}

func TestInteractivePathImportsNoHuh(t *testing.T) { // docs/spec/tui.md C4, T28
	for _, file := range []string{"shell.go", "view.go"} {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, imp := range parsed.Imports {
			if strings.Contains(imp.Path.Value, "charmbracelet/huh") {
				t.Errorf("%s imports huh; the interactive path must stay huh-free", file)
			}
		}
	}
}

func TestV33NoticeRendersScrollableOutput(t *testing.T) { // docs/spec/tui.md V33, V21
	m := newConsole(nil)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	shown, _ := sized.(console).Update(requestMsg{ok: true, request: noticeRequest{
		title: "validate",
		run:   func(w io.Writer) error { return nil },
		done:  make(chan error, 1),
	}})
	loaded, _ := shown.(console).Update(noticeDoneMsg{text: "line one\nline two"})
	view := loaded.(console).View()
	for _, want := range []string{"validate", "line one", "line two", "scroll"} {
		if !strings.Contains(view, want) {
			t.Errorf("notice missing %q:\n%s", want, view)
		}
	}
}

func TestV35InteractiveMenuDefaultsToFirstRow(t *testing.T) { // docs/spec/tui.md V35, V38
	req := fieldReq{
		kind: kindSelect, title: "Variables", backable: true, initial: "type",
		choices: []choice{{"Type  ·  string", "type"}, {"Default  ·  —", "default"}, {"← Back", actionBack}},
	}
	view := renderField(t, 90, 24, req)
	if !strings.Contains(view, "› Type") {
		t.Errorf("overview did not default to the first actionable row:\n%s", view)
	}
	if !strings.Contains(view, "← Back") {
		t.Errorf("overview omits the Back row:\n%s", view)
	}
	if !strings.Contains(view, "·") {
		t.Errorf("overview rows should carry current values:\n%s", view)
	}
}

func TestV49AccessibleModeDrivesEveryScreen(t *testing.T) { // docs/spec/tui.md V49, C5
	t.Chdir(t.TempDir())
	// Home -> create -> name -> Create agent -> confirm, all by numbered input,
	// zero TTY and zero Python.
	got, err := RunConsole(strings.NewReader("1\nagent\n7\n\n"), &bytes.Buffer{}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Confirmed || !got.Create || got.Agent.Data.Name != "agent" {
		t.Fatalf("accessible create did not complete: %#v", got)
	}
}

func TestV45PaletteOpensAndFuzzyFilters(t *testing.T) { // docs/spec/tui.md V45
	m := newConsole(nil)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	withReq, _ := sized.(console).Update(requestMsg{ok: true, request: fieldReq{
		kind: kindSelect, title: "Menu", backable: true,
		choices: []choice{{"Identity", "i"}, {"Models", "m"}, {"Behavior", "b"}, {"← Back", actionBack}},
	}})
	opened, _ := withReq.(console).Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	typed := opened.(console)
	for _, r := range "beh" {
		next, _ := typed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		typed = next.(console)
	}
	view := typed.View()
	if !strings.Contains(view, "Command palette") {
		t.Fatalf("palette did not open:\n%s", view)
	}
	if !strings.Contains(view, "Behavior") {
		t.Fatalf("fuzzy query 'beh' should match Behavior:\n%s", view)
	}
	if strings.Contains(view, "Identity") {
		t.Errorf("fuzzy query 'beh' should filter out Identity:\n%s", view)
	}
}

func TestV45EveryActionIsInPalette(t *testing.T) { // docs/spec/tui.md V45
	m := newConsole(nil)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	withReq, _ := sized.(console).Update(requestMsg{ok: true, request: fieldReq{
		kind: kindSelect, title: "Menu", backable: true,
		choices: []choice{{"Identity", "i"}, {"Models", "m"}, {"Lifecycle", "l"}, {"← Back", actionBack}},
	}})
	opened, _ := withReq.(console).Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	view := opened.(console).View()
	for _, want := range []string{"Identity", "Models", "Lifecycle"} {
		if !strings.Contains(view, want) {
			t.Errorf("empty palette query should list every row, missing %q:\n%s", want, view)
		}
	}
}

func TestV46BreakpointsPickLayout(t *testing.T) { // docs/spec/tui.md V46
	req := fieldReq{
		kind: kindSelect, title: "Models", backable: true,
		ctx:     viewCtx{sidebar: []sideItem{{label: "Identity", active: true}}},
		choices: []choice{{"Listen", "l"}, {"← Back", actionBack}},
	}
	if wide := renderField(t, 100, 24, req); !strings.Contains(wide, "SECTIONS") {
		t.Errorf("wide layout should show the sidebar:\n%s", wide)
	}
	if narrow := renderField(t, 72, 24, req); strings.Contains(narrow, "SECTIONS") {
		t.Errorf("narrow layout should collapse to a single pane:\n%s", narrow)
	}
}

func TestV46TooSmallShowsMessage(t *testing.T) { // docs/spec/tui.md V46
	view := renderField(t, 40, 12, fieldReq{kind: kindSelect, title: "X", choices: []choice{{"a", "a"}}})
	if !strings.Contains(strings.ToLower(view), "too small") {
		t.Fatalf("a tiny terminal should show the too-small message:\n%s", view)
	}
}

func TestV46ResizeRelayouts(t *testing.T) { // docs/spec/tui.md V46
	m := newConsole(nil)
	req := requestMsg{ok: true, request: fieldReq{
		kind: kindSelect, title: "Models", backable: true,
		ctx:     viewCtx{sidebar: []sideItem{{label: "Identity", active: true}}},
		choices: []choice{{"a", "a"}, {"← Back", actionBack}},
	}}
	big, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	shown, _ := big.(console).Update(req)
	if !strings.Contains(shown.(console).View(), "SECTIONS") {
		t.Fatal("expected the sidebar at a wide size")
	}
	small, _ := shown.(console).Update(tea.WindowSizeMsg{Width: 70, Height: 24})
	if strings.Contains(small.(console).View(), "SECTIONS") {
		t.Fatal("resizing narrow should drop the sidebar")
	}
}

func TestV44LayoutHasHeaderSidebarEditorFooter(t *testing.T) { // docs/spec/tui.md V44
	req := fieldReq{
		kind: kindSelect, title: "Models", backable: true,
		ctx: viewCtx{
			breadcrumb: "agent › Models", target: "pipecat",
			sidebar: []sideItem{{label: "Identity"}, {label: "Models", active: true}, {label: "Listen", child: true}, {label: "Behavior"}, {label: "Integrations"}, {label: "Lifecycle"}},
		},
		choices: []choice{{"Listen", "listen"}, {"← Back", actionBack}},
	}
	view := renderField(t, 100, 24, req)
	for _, want := range []string{"SLNG//", "SECTIONS", "Identity", "Lifecycle", "Models", "Listen", "back"} {
		if !strings.Contains(strings.ToLower(view), strings.ToLower(want)) {
			t.Errorf("layout missing %q:\n%s", want, view)
		}
	}
}

func TestV44OnlyFocusedPanelHasAccentBorder(t *testing.T) { // docs/spec/tui.md V44
	t.Setenv("NO_COLOR", "")
	if got := panel(20, 10, true).GetBorderTopForeground(); got != lipgloss.Color(style.Accent) {
		t.Errorf("focused panel border = %v, want accent %s", got, style.Accent)
	}
	if got := panel(20, 10, false).GetBorderTopForeground(); got == lipgloss.Color(style.Accent) {
		t.Errorf("unfocused panel border must not be accent")
	}
}

func TestV44SidebarShowsActiveSection(t *testing.T) { // docs/spec/tui.md V44
	req := fieldReq{
		kind: kindSelect, title: "Models", backable: true,
		ctx:     viewCtx{sidebar: []sideItem{{label: "Identity"}, {label: "Models", active: true}, {label: "Listen", child: true}}},
		choices: []choice{{"Listen", "listen"}, {"← Back", actionBack}},
	}
	view := renderField(t, 100, 24, req)
	if !strings.Contains(view, "› Models") {
		t.Errorf("active section not marked:\n%s", view)
	}
	if !strings.Contains(view, "Listen") {
		t.Errorf("active section child not shown:\n%s", view)
	}
}

func TestV35MenusDefaultToFirstActionAndBackLast(t *testing.T) { // docs/spec/tui.md V35
	runner := newRunner(strings.NewReader("\n"), &bytes.Buffer{}, true)
	choice, back, err := runner.selectOne("Actions", "", []huh.Option[string]{
		huh.NewOption("First action", "first"),
		huh.NewOption("Second action", "second"),
		huh.NewOption("← Back", actionBack),
	}, true)
	if err != nil || back || choice != "first" {
		t.Fatalf("default choice = %q, back = %v, error = %v; want first action", choice, back, err)
	}

	_, _, err = runner.selectOne("Broken", "", []huh.Option[string]{
		huh.NewOption("← Back", actionBack),
		huh.NewOption("Action", "action"),
	}, true)
	if err == nil || !strings.Contains(err.Error(), "Back action must be last") {
		t.Fatalf("misordered Back error = %v", err)
	}
}

func TestV34EditorSectionsStayGrouped(t *testing.T) { // docs/spec/tui.md V34
	data := scaffold.Data{}
	data.SetTarget(scaffold.DefaultTarget)
	options := editorSectionOptions(data)
	want := []struct {
		value, label string
	}{
		{"section:identity", "Identity  ·  target"},
		{"section:models", "Models  ·  "},
		{"section:behavior", "Behavior  ·  instructions, greeting, variables, advanced"},
		{"section:integrations", "Integrations  ·  tools, channels, human transfers"},
		{"section:lifecycle", "Lifecycle  ·  agents, handoffs, tasks, groups"},
	}
	if len(options) != len(want) {
		t.Fatalf("section count = %d, want %d", len(options), len(want))
	}
	for i := range want {
		if options[i].Value != want[i].value || !strings.HasPrefix(options[i].Key, want[i].label) {
			t.Errorf("section %d = (%q, %q), want (%q, prefix %q)", i, options[i].Value, options[i].Key, want[i].value, want[i].label)
		}
	}
}

func TestV36SectionsHaveNoPassThroughMenus(t *testing.T) { // docs/spec/tui.md V36
	data := scaffold.Data{}
	data.SetTarget(scaffold.DefaultTarget)
	var output bytes.Buffer
	if err := editModels(newRunner(strings.NewReader("5\n"), &output, true), &data); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Listen (STT)", "Reason (LLM)", "Speak (TTS)"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("Models section missing direct child %q:\n%s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "Listen, reason, speak") {
		t.Fatalf("Models still contains a pass-through row:\n%s", output.String())
	}
}

func TestV37EveryScreenShowsBackAffordance(t *testing.T) { // docs/spec/tui.md V37
	for _, tc := range []struct {
		name string
		req  fieldReq
	}{
		{"select", fieldReq{kind: kindSelect, title: "Select", choices: []choice{{"Act", "act"}, {"← Back", actionBack}}, backable: true}},
		{"input", fieldReq{kind: kindInput, title: "Input", backable: true}},
		{"text", fieldReq{kind: kindText, title: "Text", backable: true}},
	} {
		if view := renderField(t, 90, 24, tc.req); !strings.Contains(strings.ToLower(view), "back") {
			t.Errorf("%s interactive screen omits Back affordance:\n%s", tc.name, view)
		}
	}

	for name, run := range map[string]func(*fieldRunner) error{
		"input": func(runner *fieldRunner) error {
			value := "saved"
			_, err := runner.input("Input", "", &value, validateBasic)
			return err
		},
		"text": func(runner *fieldRunner) error {
			value := "saved"
			_, err := runner.text("Text", "", &value)
			return err
		},
	} {
		var output bytes.Buffer
		if err := run(newRunner(strings.NewReader(":back\n"), &output, true)); err != nil {
			t.Fatalf("%s screen: %v", name, err)
		}
		if !strings.Contains(output.String(), "Type :back to return.") {
			t.Errorf("%s accessible screen omits Back affordance:\n%s", name, output.String())
		}
	}

	for _, screen := range []struct {
		name, input string
		run         func(*fieldRunner) error
	}{
		{"select", "2\n", func(runner *fieldRunner) error {
			_, _, err := runner.selectOne("Select", "", []huh.Option[string]{huh.NewOption("Act", "act"), huh.NewOption("← Back", actionBack)}, true)
			return err
		}},
		{"confirm", "2\n", func(runner *fieldRunner) error {
			_, err := confirmChoice(runner, "Confirm?", "Confirm")
			return err
		}},
		{"notice", "1\n", func(runner *fieldRunner) error {
			return showNotice(runner, "Notice", "Something happened.")
		}},
	} {
		var output bytes.Buffer
		if err := screen.run(newRunner(strings.NewReader(screen.input), &output, true)); err != nil {
			t.Fatalf("%s screen: %v", screen.name, err)
		}
		if !strings.Contains(output.String(), "Back") {
			t.Errorf("%s screen omits Back affordance:\n%s", screen.name, output.String())
		}
	}

	m := newConsole(nil)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	shown, _ := sized.(console).Update(requestMsg{ok: true, request: noticeRequest{
		title: "Report",
		run:   func(w io.Writer) error { _, _ = io.WriteString(w, "done"); return nil },
		done:  make(chan error, 1),
	}})
	if view := shown.(console).View(); !strings.Contains(strings.ToLower(view), "back") {
		t.Errorf("interactive notice footer omits Back: %q", view)
	}
}

func TestV37ConstrainedMenuPinsBackInFooter(t *testing.T) { // docs/spec/tui.md V37
	req := fieldReq{
		kind:  kindSelect,
		title: "user_verified",
		choices: []choice{
			{"Description", "description"}, {"Execution", "execution"},
			{"Webhook URL env", "url"}, {"Input schema", "input"},
			{"Output schema", "output"}, {"Attached to", "attach"},
			{"Delete tool", "delete"}, {"← Back", actionBack},
		},
		backable: true,
	}
	// At the minimum viewport the footer must still keep a Back affordance: it
	// is a fixed region, separate from the scrolling option list (V37, was B9).
	if view := renderField(t, 80, 20, req); !strings.Contains(strings.ToLower(view), "back") {
		t.Fatalf("constrained menu omitted Back affordance:\n%s", view)
	}
}

func TestV38MultiFieldFlowsUseOverviewMenus(t *testing.T) { // docs/spec/tui.md V38
	data := scaffold.Data{}
	var output bytes.Buffer
	err := editVariables(newRunner(strings.NewReader("1\ncustomer_id\n"), &output, true), &data)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("variable overview error = %v, want ErrUserAborted", err)
	}
	for _, field := range []string{"Type  ·", "Default  ·", "Source  ·", "← Back"} {
		if !strings.Contains(output.String(), field) {
			t.Errorf("variable overview missing %q:\n%s", field, output.String())
		}
	}
}

func TestV38BindingOverviewShowsAllFieldsAndReturns(t *testing.T) { // docs/spec/tui.md V38
	data := scaffold.Data{}
	data.SetTarget("pipecat")
	var output bytes.Buffer
	err := editBinding(newRunner(strings.NewReader("1\n1\n"), &output, true), &data, targetcap.Speak)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("binding overview error = %v, want ErrUserAborted", err)
	}
	for _, field := range []string{"Provider  ·", "Distributor  ·", "Model  ·", "Voice  ·", "Language  ·", "Additional config  ·", "← Back"} {
		if !strings.Contains(output.String(), field) {
			t.Errorf("binding overview missing %q:\n%s", field, output.String())
		}
	}
	if strings.Count(output.String(), "Speak \n") < 2 {
		t.Fatalf("provider edit did not return to binding overview:\n%s", output.String())
	}
}

func TestV24DistributorRowAppearsOnlyForMultipleRoutes(t *testing.T) { // docs/spec/tui.md V24
	catalog := targetcap.DefaultCatalog()
	framework, role := targetcap.Pipecat, targetcap.Speak
	for _, wantMultiple := range []bool{false, true} {
		var brand string
		for _, candidate := range catalog.Brands(framework, role) {
			if (len(catalog.Distributors(framework, role, candidate)) > 1) == wantMultiple {
				brand = candidate
				break
			}
		}
		if brand == "" {
			t.Fatalf("catalogue has no speak brand with multiple=%v", wantMultiple)
		}
		routes := catalog.Distributors(framework, role, brand)
		binding := scaffold.Binding{Provider: routes[0], Model: brand + "/model"}
		var output bytes.Buffer
		err := editBindingFor(newRunner(strings.NewReader(""), &output, true), string(framework), role, &binding)
		if !errors.Is(err, huh.ErrUserAborted) {
			t.Fatalf("binding overview error = %v, want ErrUserAborted", err)
		}
		if got := strings.Contains(output.String(), "Distributor  ·"); got != wantMultiple {
			t.Errorf("brand %q distributor row = %v, want %v:\n%s", brand, got, wantMultiple, output.String())
		}
	}
}

func TestRunCompileToggle(t *testing.T) {
	t.Chdir(t.TempDir())
	// 1=create, name, 6=toggle compile on, 7=Create agent, confirm.
	got, err := RunConsole(strings.NewReader("1\nagent\n6\n7\n\n"), &bytes.Buffer{}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Confirmed || !got.Compile {
		t.Fatalf("compile toggle result = %#v", got)
	}
}

func TestRunSelectTarget(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	// Create, name, Target, Pipecat, Create agent, confirm.
	//
	// The accessible renderer takes ordinals, so this input selects a menu
	// entry by position. The target menu leads with scaffold.DefaultTarget
	// (createTargetOptions), which is livekit, so `2` is now Pipecat — and
	// picking the non-default is what actually exercises the menu. Any change
	// to that ordering has to be reflected here; the ordinal is not a constant.
	got, err := RunConsole(strings.NewReader("1\nagent\n1\n1\n2\n7\n\n"), &output, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Data.Target != "pipecat" {
		t.Fatalf("target = %q", got.Agent.Data.Target)
	}
	if !strings.Contains(output.String(), "Vapi and Deepgram are unavailable") {
		t.Fatalf("missing unavailable-driver explanation:\n%s", output.String())
	}
}

func TestRunEditModels(t *testing.T) {
	t.Chdir(t.TempDir())
	// Create, name, Models, Speak, edit model/voice/config rows, Back, Create, confirm.
	got, err := RunConsole(strings.NewReader("1\nagent\n2\n3\n1\n1\n2\n1\n3\nsonic-3\n4\nvoice-id\n6\n{\"speed\":1}\n7\n5\n7\n\n"), &bytes.Buffer{}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Data.Speak != (scaffold.Binding{Provider: "cartesia", Model: "sonic-3", Voice: "voice-id", Params: `{"speed":1}`}) {
		t.Fatalf("speak binding = %#v", got.Agent.Data.Speak)
	}
}

func TestEditLanguageIsPerModel(t *testing.T) { // per-model language (N16)
	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent"}
	data.SetTarget(scaffold.DefaultTarget)
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	agent.data.Speak.Language = "es-MX"
	if err := saveMaintained(newRunner(strings.NewReader("1\n1\n"), &bytes.Buffer{}, true), &agent); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "language: es-MX") {
		t.Fatalf("per-model language not emitted:\n%s", manifest)
	}
	reloaded, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.data.Speak.Language != "es-MX" {
		t.Fatalf("speak language round-trip = %q", reloaded.data.Speak.Language)
	}
}

func TestRunAddVariableAndTool(t *testing.T) {
	t.Chdir(t.TempDir())
	input := "1\nagent\n" +
		"3\n3\n1\ncustomer_id\n3\n2\n5\n3\n" +
		// Tools list now starts with the seeded end_call: "Add tool" is 2, and
		// after the new tool the list has 2 tools so its "Back" is 4.
		"4\n1\n2\nlookup_customer\n1\nLook up the caller\n3\nLOOKUP_URL\n8\n4\n" +
		"7\n\n"
	var output bytes.Buffer
	got, err := RunConsole(strings.NewReader(input), &output, true, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, output.String())
	}
	if len(got.Agent.Data.Variables) != 1 || got.Agent.Data.Variables[0].Source != "call_start" {
		t.Fatalf("variables = %#v", got.Agent.Data.Variables)
	}
	// The default end_call is seeded, so the added webhook tool is the second.
	var lookup *scaffold.Tool
	for i := range got.Agent.Data.Tools {
		if got.Agent.Data.Tools[i].Name == "lookup_customer" {
			lookup = &got.Agent.Data.Tools[i]
		}
	}
	if lookup == nil || lookup.URLEnv != "LOOKUP_URL" {
		t.Fatalf("tools = %#v", got.Agent.Data.Tools)
	}
}

func TestV18VariablesMenuShowsSavedItems(t *testing.T) { // docs/spec/tui.md V18
	data := scaffold.Data{Variables: []scaffold.Variable{{Name: "customer_id", Type: "string", Source: "call_start"}}}
	var output bytes.Buffer
	err := editVariables(newRunner(strings.NewReader(""), &output, true), &data)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("editVariables() error = %v, want ErrUserAborted", err)
	}
	if !strings.Contains(output.String(), "customer_id") {
		t.Fatalf("saved variable is not visible:\n%s", output.String())
	}
}

func TestV18ToolsMenuUsesNeutralNameAndShowsExecution(t *testing.T) { // docs/spec/tui.md V18
	data := scaffold.Data{Target: "pipecat"}
	var output bytes.Buffer
	err := editTools(newRunner(strings.NewReader("1\nlookup_customer\n2\n"), &output, true), &data)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("editTools() error = %v, want ErrUserAborted", err)
	}
	for _, want := range []string{"Add tool", "Webhook", "Local Python"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("tools flow missing %q:\n%s", want, output.String())
		}
	}
}

func TestV41NewToolDefaultsToEntryAgent(t *testing.T) { // docs/spec/tui.md V41
	data := scaffold.Data{Name: "agent", EntryAgent: "billing", Instructions: scaffold.DefaultInstructions}
	data.SetTarget(string(targetcap.LiveKit))
	data.Agents = []scaffold.Agent{{Name: "billing", Instructions: "Handle billing.", Reason: data.Reason, Speak: data.Speak}}
	var output bytes.Buffer
	if err := editTools(newRunner(strings.NewReader("1\nlookup_customer\n8\n3\n"), &output, true), &data); err != nil {
		t.Fatalf("editTools: %v\n%s", err, output.String())
	}
	if len(data.Tools) != 1 {
		t.Fatalf("tools = %#v", data.Tools)
	}
	if !slices.Equal(data.Tools[0].AttachTo, []string{"billing"}) || len(data.Tools[0].AttachTasks) != 0 {
		t.Fatalf("attachments = agents %v, tasks %v", data.Tools[0].AttachTo, data.Tools[0].AttachTasks)
	}
}

func TestV41ToolAttachmentsAreTargetIndependent(t *testing.T) { // docs/spec/tui.md V41
	data := scaffold.Data{Name: "agent", Instructions: scaffold.DefaultInstructions}
	data.Agents = []scaffold.Agent{{Name: "billing", Instructions: "Handle billing.", Reason: data.Reason, Speak: data.Speak}}
	data.Tasks = []scaffold.Task{{Name: "collect"}}
	tool := scaffold.Tool{Name: "lookup_customer", AttachTo: []string{"assistant"}}
	for _, provider := range targetcap.Providers {
		data.SetTarget(string(provider))
		available, _ := toolAttachmentChoices(&data, tool)
		want := []string{"Agent · assistant", "Agent · billing", "Task · collect"}
		if !slices.Equal(available, want) {
			t.Errorf("%s attachments = %v, want %v", provider, available, want)
		}
	}
	data.SetTarget(string(targetcap.LiveKit))
	var output bytes.Buffer
	if err := editTool(newRunner(strings.NewReader("6\n2\n3\n4\n8\n"), &output, true), &data, &tool); err != nil {
		t.Fatalf("editTool: %v\n%s", err, output.String())
	}
	for _, want := range []string{"Agent · assistant", "Agent · billing", "Task · collect"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("attachment choices omit %q:\n%s", want, output.String())
		}
	}
	if !slices.Equal(tool.AttachTo, []string{"assistant", "billing"}) || !slices.Equal(tool.AttachTasks, []string{"collect"}) {
		t.Fatalf("attachments = agents %v, tasks %v", tool.AttachTo, tool.AttachTasks)
	}
}

func TestV41LiveKitAgentEditorAttachesTool(t *testing.T) { // docs/spec/tui.md V41
	data := scaffold.Data{Name: "agent", Instructions: scaffold.DefaultInstructions}
	data.SetTarget(string(targetcap.LiveKit))
	data.Tools = []scaffold.Tool{{Name: "lookup_customer", AttachTo: []string{}}}
	var output bytes.Buffer
	if err := editAgentDetails(newRunner(strings.NewReader("4\n1\n2\n7\n"), &output, true), &data, "assistant"); err != nil {
		t.Fatalf("editAgentDetails: %v\n%s", err, output.String())
	}
	if !slices.Equal(data.Tools[0].AttachTo, []string{"assistant"}) {
		t.Fatalf("agent attachments = %v", data.Tools[0].AttachTo)
	}
}

func TestV41LiveKitInitWebhookCompilesOnAgent(t *testing.T) { // docs/spec/tui.md V41
	data := scaffold.Data{
		Name: "agent", Instructions: scaffold.DefaultInstructions,
		Tools: []scaffold.Tool{{
			Name: "lookup_customer", Description: "Look up the customer.",
			Execution: "webhook", URLEnv: "LOOKUP_URL", Input: `{"type":"object"}`,
			AttachTo: []string{"assistant"},
		}},
	}
	data.SetTarget(string(targetcap.LiveKit))
	dir := filepath.Join(t.TempDir(), "agent")
	if _, err := scaffold.Write(dir, data); err != nil {
		t.Fatal(err)
	}
	pkg, err := spec.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	var livekit ir.Target
	for _, target := range agent.Targets {
		livekit = target
	}
	artifact, err := generate.Generate(agent, livekit, targetcap.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range artifact.Files {
		if file.Path == "agent.py" && strings.Contains(string(file.Content), "@function_tool") && strings.Contains(string(file.Content), "async def lookup_customer(") {
			return
		}
	}
	t.Fatal("generated agent.py omitted the entry agent's @function_tool method")
}

func TestV42LiveKitInitMCPCompilesOnAgent(t *testing.T) { // docs/spec/tui.md V42
	data := scaffold.Data{
		Name: "agent", Instructions: scaffold.DefaultInstructions,
		Tools: []scaffold.Tool{{
			Name: "book_table", Description: "Book through the bookings MCP server.",
			Execution: "mcp", URLEnv: "BOOKINGS_MCP_URL", Input: `{"type":"object"}`,
			AttachTo: []string{"assistant"},
		}},
	}
	data.SetTarget(string(targetcap.LiveKit))
	dir := filepath.Join(t.TempDir(), "agent")
	if _, err := scaffold.Write(dir, data); err != nil {
		t.Fatal(err)
	}
	pkg, err := spec.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	var livekit ir.Target
	for _, target := range agent.Targets {
		livekit = target
	}
	artifact, err := generate.Generate(agent, livekit, targetcap.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range artifact.Files {
		if file.Path == "agent.py" && strings.Contains(string(file.Content), `mcp.MCPServerHTTP(url=os.environ["BOOKINGS_MCP_URL"]`) {
			return
		}
	}
	t.Fatal("generated agent.py omitted the MCP server mount")
}

func TestV18AgentMenuShowsAndEditsSavedAgent(t *testing.T) { // docs/spec/tui.md V18
	data := scaffold.Data{Instructions: scaffold.DefaultInstructions}
	data.SetTarget("pipecat")
	data.Agents = []scaffold.Agent{{
		Name: "billing", Instructions: "Handle billing.", Reason: data.Reason, Speak: data.Speak,
	}}
	data.Tools = []scaffold.Tool{{Name: "lookup_customer", AttachTo: []string{"billing"}}}
	var output bytes.Buffer
	err := editAgents(newRunner(strings.NewReader("2\n1\nUpdated billing prompt.\n7\n5\n"), &output, true), &data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "billing") {
		t.Fatalf("saved agent is not visible:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "lookup_customer") {
		t.Fatalf("saved agent tools are not visible:\n%s", output.String())
	}
	if got := data.Agents[0].Instructions; got != "Updated billing prompt." {
		t.Fatalf("billing prompt = %q", got)
	}
}

func TestV19HandoffShowsExistingVariablesAsChoices(t *testing.T) { // docs/spec/tui.md V19
	data := scaffold.Data{Instructions: scaffold.DefaultInstructions, Variables: []scaffold.Variable{{Name: "customer_id", Type: "string"}}}
	data.SetTarget("pipecat")
	data.Agents = []scaffold.Agent{{Name: "billing", Instructions: "Handle billing.", Reason: data.Reason, Speak: data.Speak}}
	var output bytes.Buffer
	err := editHandoffs(newRunner(strings.NewReader("1\nto_billing\n4\n"), &output, true), &data)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("editHandoffs() error = %v, want ErrUserAborted", err)
	}
	for _, want := range []string{"assistant", "billing", "customer_id"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("handoff choices missing %q:\n%s", want, output.String())
		}
	}
}

func TestV19TaskShowsExistingToolsAsChoices(t *testing.T) { // docs/spec/tui.md V19
	data := scaffold.Data{Tools: []scaffold.Tool{{Name: "lookup_customer"}}}
	data.SetTarget("pipecat")
	var output bytes.Buffer
	err := editTasks(newRunner(strings.NewReader("1\ncollect\n2\n"), &output, true), &data)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("editTasks() error = %v, want ErrUserAborted", err)
	}
	if !strings.Contains(output.String(), "lookup_customer") {
		t.Fatalf("task tool choices are not visible:\n%s", output.String())
	}
}

func TestV22TaskResultExplainsPrefilledShape(t *testing.T) { // docs/spec/tui.md V22
	data := scaffold.Data{}
	data.SetTarget("pipecat")
	var output bytes.Buffer
	err := editTasks(newRunner(strings.NewReader("1\ncollect\n4\n"), &output, true), &data)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("editTasks() error = %v, want ErrUserAborted", err)
	}
	for _, want := range []string{`{"result":"string"}`, "Each key becomes one returned field"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("task result help missing %q:\n%s", want, output.String())
		}
	}
}

func TestV19TaskAssignmentPicksSavedVariableAndResultField(t *testing.T) { // docs/spec/tui.md V19
	data := scaffold.Data{Variables: []scaffold.Variable{{Name: "verified", Type: "boolean"}, {Name: "tier", Type: "string"}}}
	task := scaffold.Task{Result: `{"verified":"boolean","tier":{"enum":["free","pro"]}}`}
	var output bytes.Buffer
	back, err := editTaskAssignments(newRunner(strings.NewReader("1\n3\n1\n2\n2\n"), &output, true), &data, &task)
	if err != nil {
		t.Fatal(err)
	}
	if back {
		t.Fatal("editTaskAssignments() unexpectedly went back")
	}
	if task.Assign != `{"verified":"result.verified"}` {
		t.Fatalf("task assignment = %s", task.Assign)
	}
	for _, want := range []string{"verified", "tier"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("task assignment choices missing %q:\n%s", want, output.String())
		}
	}
}

func TestV39TaskResultAssignmentCanBeRemoved(t *testing.T) { // docs/spec/tui.md V39
	for _, test := range []struct {
		name, result, assign, input, want string
		variables                         []scaffold.Variable
	}{
		{
			name: "remaining map", result: `{"company":"string","customer_name":"string"}`,
			assign: `{"company":"result.company","customer_name":"result.customer_name"}`,
			input:  "3\n1\n3\n2\n", want: `{"customer_name":"result.customer_name"}`,
			variables: []scaffold.Variable{{Name: "company", Type: "string"}, {Name: "customer_name", Type: "string"}},
		},
		{
			name: "empty map", result: `{"customer_name":"string"}`,
			assign: `{"customer_name":"result.customer_name"}`, input: "2\n1\n2\n1\n", want: "",
			variables: []scaffold.Variable{{Name: "customer_name", Type: "string"}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := scaffold.Data{Variables: test.variables}
			task := scaffold.Task{Result: test.result, Assign: test.assign}
			var output bytes.Buffer
			back, err := editTaskAssignments(newRunner(strings.NewReader(test.input), &output, true), &data, &task)
			if err != nil {
				t.Fatal(err)
			}
			if back {
				t.Fatal("removing an assignment unexpectedly went back")
			}
			if task.Assign != test.want {
				t.Fatalf("task assignment = %q, want %q", task.Assign, test.want)
			}
			if !strings.Contains(output.String(), "Remove assignment") {
				t.Fatalf("field editor omitted Remove assignment:\n%s", output.String())
			}
		})
	}
}

func TestV21PreflightFailureUsesDedicatedScreen(t *testing.T) { // docs/spec/tui.md V21
	// Pipecat still gates fallback (driver-pipecat C9/B7); livekit emits it
	// natively since driver-livekit T5, so the failure fixture lives here.
	data := scaffold.Data{Name: "agent", Channel: "web"}
	data.SetTarget("pipecat")
	data.Fallbacks = []scaffold.ModelFallback{{Name: "backup", Profile: "assistant_model", Binding: data.Reason}}
	var output bytes.Buffer
	_, back, err := editAgent(newRunner(strings.NewReader("7\n10\n8\n"), &output, true), Agent{Path: "agent", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if !back {
		t.Fatal("editAgent() did not return through Back")
	}
	for _, want := range []string{"Cannot create agent", "Fix the configuration, then go Back", "does not emit generated fallback", "Review / delete model fallbacks"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("preflight screen missing %q:\n%s", want, output.String())
		}
	}
}

func TestRunHandoffsRequireTwoAgents(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	got, err := RunConsole(strings.NewReader("1\nagent\n5\n2\n1\n7\n\n"), &output, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Agent.Data.Handoffs) != 0 || !strings.Contains(output.String(), "Create a second agent first") {
		t.Fatalf("handoff gate missing: result=%#v\n%s", got, output.String())
	}
}

func TestRunAddAgentAndHandoff(t *testing.T) {
	t.Chdir(t.TempDir())
	input := "1\nagent\n" +
		"5\n1\n2\nbilling\n1\nYou handle billing questions.\n7\n5\n" +
		"5\n2\n1\nto_billing\n3\nCaller asks about billing.\n8\n3\n" +
		"7\n\n"
	var output bytes.Buffer
	got, err := RunConsole(strings.NewReader(input), &output, true, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, output.String())
	}
	if len(got.Agent.Data.Agents) != 1 || got.Agent.Data.Agents[0].Name != "billing" {
		t.Fatalf("agents = %#v", got.Agent.Data.Agents)
	}
	if len(got.Agent.Data.Handoffs) != 1 || got.Agent.Data.Handoffs[0].Source != "assistant" || got.Agent.Data.Handoffs[0].To != "billing" {
		t.Fatalf("handoffs = %#v", got.Agent.Data.Handoffs)
	}
}

func TestHandoffAnnouncementEditsAndRoundTrips(t *testing.T) {
	data := scaffold.Data{Instructions: scaffold.DefaultInstructions}
	data.SetTarget("pipecat")
	data.Agents = []scaffold.Agent{{Name: "billing", Instructions: "Handle billing.", Reason: data.Reason, Speak: data.Speak}}
	data.Handoffs = []scaffold.Handoff{{Name: "to_billing", Source: "assistant", To: "billing", When: "Billing", History: "full", AllVariables: true}}

	var output bytes.Buffer
	if err := editHandoffDetails(newRunner(strings.NewReader("6\nI’ll connect you to billing now.\n8\n"), &output, true), &data, "to_billing"); err != nil {
		t.Fatal(err)
	}
	if got := data.Handoffs[0].Announce; got != "I’ll connect you to billing now." {
		t.Fatalf("announcement = %q", got)
	}

	root := filepath.Join(t.TempDir(), "agent")
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.data.Handoffs[0].Announce; got != data.Handoffs[0].Announce {
		t.Fatalf("round-trip announcement = %q", got)
	}
}

func TestRunTaskGroupsRequireTasks(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	if _, err := RunConsole(strings.NewReader("1\nagent\n5\n4\n1\n7\n\n"), &output, true, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Create at least one task first") {
		t.Fatalf("task-group gate missing:\n%s", output.String())
	}
}

func TestRunAddTaskAndOrderedGroup(t *testing.T) {
	t.Chdir(t.TempDir())
	input := "1\nagent\n" +
		"5\n3\n1\ncollect\n7\nClassify the caller.\n10\n3\n" +
		"5\n4\n1\ntriage\n6\nRun triage.\n8\n3\n" +
		"7\n\n"
	got, err := RunConsole(strings.NewReader(input), &bytes.Buffer{}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Agent.Data.Tasks) != 1 || got.Agent.Data.Tasks[0].Name != "collect" {
		t.Fatalf("tasks = %#v", got.Agent.Data.Tasks)
	}
	if len(got.Agent.Data.TaskGroups) != 1 || !reflect.DeepEqual(got.Agent.Data.TaskGroups[0].Steps, []string{"collect"}) {
		t.Fatalf("task groups = %#v", got.Agent.Data.TaskGroups)
	}
}

func TestRunHumanTransfersRequireTelephony(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	if _, err := RunConsole(strings.NewReader("1\nagent\n4\n3\n1\n7\n\n"), &output, true, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Add a telephony caller channel first") {
		t.Fatalf("human-transfer gate missing:\n%s", output.String())
	}
}

func TestRunTelephonyCreateGatedOnConnection(t *testing.T) {
	t.Chdir(t.TempDir())
	// The create wizard offers only code targets (Pipecat/LiveKit), and it
	// cannot supply everything a phone route needs on either of them. Create is
	// correctly gated, and which field is missing depends on the target:
	// Pipecat defaults the Daily-provisioned transport and is short a carrier;
	// LiveKit defaults no transport at all and is short that. Both refusals name
	// connections/phone.yaml and the field to add, so this asserts the gate
	// itself plus the shape of the guidance, not one target's wording.
	// (Managed targets like ElevenLabs, which slipped past this gate, no longer
	// exist — and never had a real telephony route anyway, SCHEMA N17.)
	// Drive: create → add telephony channel → add human transfer → Create (blocked)
	// → Back out of the repair menu → Back out of the editor → Quit.
	input := "1\nagent\n" +
		"4\n2\n1\n2\n5\n" +
		"4\n3\n1\nto_human\n2\nCaller requests a person.\n3\nsupport_line\n4\nSUPPORT_PHONE_NUMBER\n8\n3\n" +
		"7\n10\n8\n3\n"
	var output bytes.Buffer
	got, _ := RunConsole(strings.NewReader(input), &output, true, nil)
	if got.Confirmed {
		t.Fatalf("telephony agent must not be created on a route that cannot receive calls: %#v", got.Agent.Data)
	}
	// The blocking assertion above is the gate and must not be weakened. This
	// one holds it to a useful refusal: name the file, and name the field.
	refusal := output.String()
	if !strings.Contains(refusal, "connections/phone.yaml") {
		t.Fatalf("the refusal must name the file to fix:\n%s", refusal)
	}
	if !strings.Contains(refusal, "carrier") && !strings.Contains(refusal, "transport") {
		t.Fatalf("the refusal must name the missing field:\n%s", refusal)
	}
}

func TestRunCustomizeCapacity(t *testing.T) {
	t.Chdir(t.TempDir())
	input := "1\nagent\n3\n4\n3\n2\n10\n1\n5\n3\n4m\n4\n5\n7\n\n"
	got, err := RunConsole(strings.NewReader(input), &bytes.Buffer{}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Data.Capacity != (scaffold.Capacity{PeakSessions: 5, MaxSessions: 10, AvgSessionDuration: "4m"}) {
		t.Fatalf("capacity = %#v", got.Agent.Data.Capacity)
	}
}

func TestV20BackPreservesPriorEdits(t *testing.T) { // docs/spec/tui.md V20
	t.Chdir(t.TempDir())
	// Edit greeting (Behavior), then open Instructions and Back out unchanged;
	// the greeting edit must survive.
	input := "1\nagent\n3\n2\nEdited greeting\n3\n1\n:back\n7\n\n"
	got, err := RunConsole(strings.NewReader(input), &bytes.Buffer{}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.Data.Greeting != "Edited greeting" || got.Agent.Data.Instructions != scaffold.DefaultInstructions {
		t.Fatalf("back lost edits: %#v", got.Agent.Data)
	}
}

func TestV31ProviderOptionsMirrorCatalog(t *testing.T) {
	type pair struct {
		framework targetcap.Provider
		role      targetcap.Role
	}
	catalog := targetcap.DefaultCatalog()
	expected := map[pair][]string{}
	for _, entry := range catalog.Entries() {
		key := pair{entry.Framework, entry.Role}
		if entry.Wildcard() {
			if _, ok := expected[key]; !ok {
				expected[key] = nil
			}
			continue
		}
		brands := entry.Distributes
		if len(brands) == 0 {
			brands = []string{entry.Vendor}
		}
		expected[key] = append(expected[key], brands...)
	}
	for key, brands := range expected {
		slices.Sort(brands)
		brands = slices.Compact(brands)
		options := providerOptions(key.framework, key.role)
		got := make([]string, len(options))
		for i := range options {
			got[i] = options[i].Value
		}
		if !slices.Equal(got, brands) {
			t.Errorf("%s/%s options = %v, catalogue brands = %v", key.framework, key.role, got, brands)
		}
	}
}

func TestV31CatalogueHintUsesEntryArityAndLanguageSlot(t *testing.T) {
	entry, ok := targetcap.DefaultCatalog().Lookup(targetcap.Pipecat, targetcap.Listen, "deepgram")
	if !ok {
		t.Fatal("missing pipecat/listen/deepgram catalogue entry")
	}
	hint := catalogueEntryHint(entry)
	for _, want := range []string{"model → model (required)", "voice unavailable", "language → language (optional)"} {
		if !strings.Contains(hint, want) {
			t.Errorf("catalogue hint %q omits %q", hint, want)
		}
	}
}

func TestV29UnavailableChoiceNamesGateAndOffersBack(t *testing.T) {
	// Vapi still gates local tools; pipecat's gate lifted 2026-07-17 (T14).
	var output bytes.Buffer
	tool := scaffold.Tool{}
	back, err := chooseToolExecution(newRunner(strings.NewReader("2\n1\n5\n"), &output, true), string(targetcap.Vapi), &tool)
	if err != nil {
		t.Fatal(err)
	}
	if !back {
		t.Fatal("unavailable choice did not permit Back")
	}
	want := targetcap.Default().Capability(targetcap.FieldToolLocal, targetcap.Vapi).Note
	if !strings.Contains(output.String(), want) || !strings.Contains(output.String(), "Identity → Target") || !strings.Contains(output.String(), "← Back") {
		t.Fatalf("unavailable choice omitted exact gate, target guidance, or Back:\n%s", output.String())
	}
}

func TestTUIMatchesCapabilityTable(t *testing.T) { // docs/spec/tui.md V42
	table := targetcap.Default()
	providers := []targetcap.Provider{targetcap.LiveKit, targetcap.Pipecat, targetcap.Deepgram, targetcap.Vapi}
	kindFields := map[targetcap.Field]bool{}
	for _, kind := range toolExecutionKinds {
		if kind.Field != "" {
			kindFields[kind.Field] = true
		}
		for _, provider := range providers {
			_, offered := toolExecutionGate(kind, string(provider))
			supported := kind.Field == "" || table.Capability(kind.Field, provider).Tag != targetcap.Gated
			if offered != supported {
				t.Errorf("%s on %s: console offers=%v, table supports=%v — wizard gating contradicts internal/target", kind.Value, provider, offered, supported)
			}
		}
	}
	// Every tool-execution capability the console never offers must be gated on
	// every target; otherwise the wizard hides a kind some driver emits.
	for field := range table.Fields {
		if !strings.HasPrefix(string(field), "tools.execution.") || kindFields[field] {
			continue
		}
		for _, provider := range providers {
			if capability := table.Capability(field, provider); capability.Tag != targetcap.Gated {
				t.Errorf("capability %q is emittable on %s (tag %q) but the console never offers it", field, provider, capability.Tag)
			}
		}
	}
}

func TestV42ExecutionPickerDerivesFromTable(t *testing.T) { // docs/spec/tui.md V42
	// Gated: mcp on Deepgram surfaces the table row's own note, then Back.
	// (Pipecat emits MCP sources since N40, so its row is no longer a gate.)
	var output bytes.Buffer
	tool := scaffold.Tool{Name: "book_table"}
	back, err := chooseToolExecution(newRunner(strings.NewReader("3\n1\n5\n"), &output, true), string(targetcap.Deepgram), &tool)
	if err != nil {
		t.Fatal(err)
	}
	if !back {
		t.Fatal("gated mcp choice did not permit Back")
	}
	note := targetcap.Default().Capability(targetcap.FieldToolMCP, targetcap.Deepgram).Note
	if !strings.Contains(output.String(), note) || !strings.Contains(output.String(), "Identity → Target") {
		t.Fatalf("gated mcp choice omitted the table note or target guidance:\n%s", output.String())
	}

	// Available: mcp on LiveKit selects, clearing any local handler state.
	output.Reset()
	tool = scaffold.Tool{Name: "book_table", Execution: "local", Handler: "tools/book_table.py"}
	back, err = chooseToolExecution(newRunner(strings.NewReader("3\n"), &output, true), string(targetcap.LiveKit), &tool)
	if err != nil {
		t.Fatal(err)
	}
	if back {
		t.Fatal("available mcp selection returned Back")
	}
	if tool.Execution != "mcp" || tool.Handler != "" {
		t.Fatalf("mcp selection left tool = %+v", tool)
	}
}

func TestV40LocalPythonSelectionScaffoldsHandler(t *testing.T) { // docs/spec/tui.md V40
	tool := scaffold.Tool{Name: "lookup_customer", Execution: "webhook", URLEnv: "LOOKUP_URL", Input: `{"type":"object"}`}
	var output bytes.Buffer
	back, err := chooseToolExecution(newRunner(strings.NewReader("2\n"), &output, true), string(targetcap.LiveKit), &tool)
	if err != nil {
		t.Fatal(err)
	}
	if back {
		t.Fatal("Local Python selection unexpectedly went back")
	}
	if tool.Execution != "local" || tool.Handler != "tools/lookup_customer.py" || tool.URLEnv != "" {
		t.Fatalf("local tool = %#v", tool)
	}
	if !strings.Contains(output.String(), "creates tools/lookup_customer.py when saved") {
		t.Fatalf("Local Python option omitted file guidance:\n%s", output.String())
	}

	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent", Tools: []scaffold.Tool{tool}}
	data.SetTarget(string(targetcap.LiveKit))
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, "tools", "lookup_customer.py"))
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 0 {
		t.Fatalf("new handler content = %q, want empty", content)
	}
}

func TestV40MaintainPreservesLocalHandler(t *testing.T) { // docs/spec/tui.md V40
	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent", Tools: []scaffold.Tool{{
		Name: "lookup_customer", Description: "Look up the customer.", Execution: "local",
		Handler: "tools/lookup_customer.py", Input: `{"type":"object"}`,
	}}}
	data.SetTarget(string(targetcap.LiveKit))
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	handler := filepath.Join(root, "tools", "lookup_customer.py")
	want := "def lookup_customer():\n    return {}\n"
	if err := os.WriteFile(handler, []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	agent.data.Speak.Language = "es"
	if err := saveMaintained(newRunner(strings.NewReader("1\n1\n"), &bytes.Buffer{}, true), &agent); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(handler)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("handler changed:\n%s", got)
	}
}

func TestV24ModelsLabelDeduplicatesProviderBrands(t *testing.T) { // docs/spec/tui.md V24
	data := scaffold.Data{}
	data.SetTarget("livekit")
	if got := modelsLabel(data); got != "deepgram / openai" {
		t.Fatalf("models label = %q", got)
	}
}

func TestV24ProviderThenDistributorFlow(t *testing.T) { // docs/spec/tui.md V24
	binding := scaffold.Binding{}
	input := "1\n1\n2\n2\n3\nslng/cartesia/sonic-3\n4\nvoice-id\n7\n"
	if err := editBindingFor(newRunner(strings.NewReader(input), &bytes.Buffer{}, true), "pipecat", targetcap.Speak, &binding); err != nil {
		t.Fatal(err)
	}
	want := scaffold.Binding{Provider: "slng", Model: "slng/cartesia/sonic-3", Voice: "voice-id"}
	if binding != want {
		t.Fatalf("binding = %#v, want %#v", binding, want)
	}
}

func TestV24ProviderBrandsAreUniqueAndExposeDistributors(t *testing.T) { // docs/spec/tui.md V24
	type pair struct {
		framework targetcap.Provider
		role      targetcap.Role
	}
	catalog := targetcap.DefaultCatalog()
	pairs := map[pair]bool{}
	for _, entry := range catalog.Entries() {
		pairs[pair{entry.Framework, entry.Role}] = true
	}
	for key := range pairs {
		seen := map[string]bool{}
		for _, option := range providerOptions(key.framework, key.role) {
			if seen[option.Value] {
				t.Errorf("%s/%s repeats provider brand %q", key.framework, key.role, option.Value)
			}
			seen[option.Value] = true
			if routes := catalog.Distributors(key.framework, key.role, option.Value); len(routes) == 0 {
				t.Errorf("%s/%s provider brand %q has no distributor", key.framework, key.role, option.Value)
			}
		}
	}
}

func TestV25SavedResourcesOfferDelete(t *testing.T) { // docs/spec/tui.md V25
	base := func() scaffold.Data {
		data := scaffold.Data{Name: "agent", Instructions: scaffold.DefaultInstructions}
		data.SetTarget("pipecat")
		data.Variables = []scaffold.Variable{{Name: "customer_id", Type: "string"}}
		data.Tools = []scaffold.Tool{{Name: "lookup_customer", Description: "Lookup", Input: `{}`}}
		data.Agents = []scaffold.Agent{{Name: "billing", Instructions: "Billing", Reason: data.Reason, Speak: data.Speak}}
		data.Handoffs = []scaffold.Handoff{{Name: "to_billing", Source: "assistant", To: "billing", When: "Billing", History: "full", AllVariables: true}}
		data.Tasks = []scaffold.Task{{Name: "collect", Instructions: "Collect", Result: `{"result":"string"}`, History: "full", Agent: "assistant", When: "Collect"}}
		data.TaskGroups = []scaffold.TaskGroup{{Name: "flow", Steps: []string{"collect"}, ContextScope: "shared", Then: "return", Agent: "assistant", When: "Flow"}}
		data.Channels = []scaffold.Channel{{Name: "phone", Kind: "telephony", Inbound: true}}
		data.HumanTransfers = []scaffold.HumanTransfer{{Name: "to_human", Agent: "assistant", When: "Human", Destination: "support", Value: "SUPPORT_PHONE_NUMBER", Mode: "cold"}}
		data.Fallbacks = []scaffold.ModelFallback{{Name: "backup", Profile: "assistant_model", Binding: data.Reason}}
		return data
	}

	tests := []struct {
		name  string
		input string
		open  func(*fieldRunner, *scaffold.Data) error
	}{
		{"variable", "1\n", editVariables},
		{"tool", "1\n", editTools},
		{"agent", "2\n", editAgents},
		{"handoff", "1\n", editHandoffs},
		{"task", "1\n", editTasks},
		{"task group", "1\n", editTaskGroups},
		{"human transfer", "1\n", editHumanTransfers},
		{"fallback", "1\n", editFallbacks},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := base()
			var output bytes.Buffer
			err := tc.open(newRunner(strings.NewReader(tc.input), &output, true), &data)
			if !errors.Is(err, huh.ErrUserAborted) {
				t.Fatalf("editor error = %v, want ErrUserAborted", err)
			}
			if !strings.Contains(output.String(), "Delete") {
				t.Fatalf("saved %s has no Delete action:\n%s", tc.name, output.String())
			}
		})
	}
}

func TestV25DeleteResourceCleansReferences(t *testing.T) { // docs/spec/tui.md V25
	data := scaffold.Data{EntryAgent: "billing"}
	data.Agents = []scaffold.Agent{{Name: "billing"}}
	data.Variables = []scaffold.Variable{{Name: "customer_id", Type: "string"}}
	data.Tools = []scaffold.Tool{{Name: "lookup", AttachTo: []string{"billing"}, AttachTasks: []string{"collect"}}}
	data.Handoffs = []scaffold.Handoff{{Name: "to_billing", Source: "assistant", To: "billing", Requires: []string{"customer_id"}, Variables: []string{"customer_id"}}}
	data.Tasks = []scaffold.Task{{Name: "collect", Tools: []string{"lookup"}, Model: "billing_model", Assign: `{"customer_id":"result.result"}`, Agent: "billing"}}
	data.TaskGroups = []scaffold.TaskGroup{{Name: "flow", Steps: []string{"collect"}, Agent: "billing"}}
	data.HumanTransfers = []scaffold.HumanTransfer{{Name: "human", Agent: "billing"}}
	data.Fallbacks = []scaffold.ModelFallback{{Name: "backup", Profile: "billing_model"}}

	for _, item := range [][2]string{{"variable", "customer_id"}, {"tool", "lookup"}, {"agent", "billing"}, {"task", "collect"}} {
		if err := deleteResource(&data, item[0], item[1]); err != nil {
			t.Fatalf("delete %s: %v", item[0], err)
		}
	}
	if len(data.Variables)+len(data.Tools)+len(data.Agents)+len(data.Handoffs)+len(data.Tasks)+len(data.TaskGroups)+len(data.Fallbacks) != 0 {
		t.Fatalf("dangling resources after delete: %#v", data)
	}
	if data.EntryAgent != "assistant" || data.HumanTransfers[0].Agent != "assistant" {
		t.Fatalf("agent references were not reset: %#v", data)
	}
}

func TestV25InvalidSavedResourcesRemainAvailableForRepair(t *testing.T) { // docs/spec/tui.md V25
	tests := []struct {
		name string
		data scaffold.Data
		open func(*fieldRunner, *scaffold.Data) error
	}{
		{"handoff without second agent", scaffold.Data{Handoffs: []scaffold.Handoff{{Name: "broken", Source: "assistant", To: "missing"}}}, editHandoffs},
		{"group without tasks", scaffold.Data{TaskGroups: []scaffold.TaskGroup{{Name: "broken", Steps: []string{"missing"}}}}, editTaskGroups},
		{"transfer without phone", scaffold.Data{HumanTransfers: []scaffold.HumanTransfer{{Name: "broken", Agent: "assistant"}}}, editHumanTransfers},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			err := tc.open(newRunner(strings.NewReader("1\n"), &output, true), &tc.data)
			if !errors.Is(err, huh.ErrUserAborted) {
				t.Fatalf("editor error = %v, want ErrUserAborted", err)
			}
			if !strings.Contains(output.String(), "Delete") {
				t.Fatalf("invalid saved resource cannot be repaired or deleted:\n%s", output.String())
			}
		})
	}
}

func TestValidateParams(t *testing.T) {
	for _, value := range []string{"", `{}`, `{"temperature":0.2}`} {
		if err := validateParams(value); err != nil {
			t.Errorf("validateParams(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"[]", "nope", "null"} {
		if err := validateParams(value); err == nil {
			t.Errorf("validateParams(%q) accepted", value)
		}
	}
}

func TestValidateLanguage(t *testing.T) {
	for _, value := range []string{"en", "es-MX", "zh-Hans-CN"} {
		if err := validateLanguage(value); err != nil {
			t.Errorf("validateLanguage(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"", "not_a_language"} {
		if err := validateLanguage(value); err == nil {
			t.Errorf("validateLanguage(%q) accepted", value)
		}
	}
}

func TestValidateVariableDefault(t *testing.T) {
	for _, tc := range []struct{ kind, value string }{
		{"string", `""`}, {"string", `"hello"`}, {"boolean", "false"},
		{"number", "1.5"}, {"integer", "2"},
	} {
		if err := validateVariableDefault(tc.kind, tc.value); err != nil {
			t.Errorf("%s %s: %v", tc.kind, tc.value, err)
		}
	}
	for _, tc := range []struct{ kind, value string }{{"string", "1"}, {"integer", "1.5"}, {"boolean", `"false"`}} {
		if err := validateVariableDefault(tc.kind, tc.value); err == nil {
			t.Errorf("%s %s accepted", tc.kind, tc.value)
		}
	}
}

func TestValidateTaskResult(t *testing.T) {
	for _, value := range []string{`{"ok":"boolean"}`, `{"tier":{"enum":["free","pro"]}}`} {
		if err := validateTaskResult(value); err != nil {
			t.Errorf("validateTaskResult(%s) = %v", value, err)
		}
	}
	for _, value := range []string{`{}`, `{"nested":{"type":"object"}}`, `{"score":"float"}`} {
		if err := validateTaskResult(value); err == nil {
			t.Errorf("validateTaskResult(%s) accepted", value)
		}
	}
}

// A destination names an environment variable and nothing else. The literal
// forms the wizard used to accept are refused, because the value it collects
// lands in agent.yaml, the portable half of a package (spec FR-004d).
func TestValidateDestination(t *testing.T) {
	for _, value := range []string{"SUPPORT_PHONE_NUMBER", "BILLING_LINE", "L2"} {
		if err := validateDestination(value); err != nil {
			t.Errorf("validateDestination(%q) = %v", value, err)
		}
	}
	for _, value := range []string{
		"+14155550123", "sip:agent@example.com", "sips:agent@example.com",
		"14155550123", "https://example.com", "support_line", "2_LINES", "",
	} {
		if err := validateDestination(value); err == nil {
			t.Errorf("validateDestination(%q) accepted", value)
		}
	}
}

func TestValidateDuration(t *testing.T) {
	for _, value := range []string{"30s", "5m", "1h30m"} {
		if err := validateDuration(value); err != nil {
			t.Errorf("validateDuration(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"", "5 minutes", "-1s", "0s"} {
		if err := validateDuration(value); err == nil {
			t.Errorf("validateDuration(%q) accepted", value)
		}
	}
}

func TestRunEOFAborts(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := RunConsole(strings.NewReader("1\nagent\n"), &bytes.Buffer{}, true, nil)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("Run() error = %v, want ErrUserAborted", err)
	}
}

func TestConsoleFooterShowsBackHint(t *testing.T) {
	if view := renderField(t, 80, 24, fieldReq{kind: kindInput, title: "Name", backable: true}); !strings.Contains(strings.ToLower(view), "back") {
		t.Fatalf("interactive input footer omits Back hint:\n%s", view)
	}
}

func TestValidateNameRefusesExistingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("taken", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("taken", "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateName("taken"); !errors.Is(err, scaffold.ErrExists) {
		t.Fatalf("validateName() = %v, want ErrExists", err)
	}
}

func TestValidateBasicRejectsTemplateBreakers(t *testing.T) {
	for _, value := range []string{`bad"value`, "bad\nvalue", "bad\rvalue"} {
		if err := validateBasic(value); err == nil {
			t.Errorf("validateBasic(%q) accepted", value)
		}
	}
}

func TestOpenDiscoversOnlyImmediateAgentPackages(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"b", "a", filepath.Join("nested", "agent")} {
		path := filepath.Join(root, dir)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "agent.yaml"), []byte("version: 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := discoverPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "a"), filepath.Join(root, "b")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverPackages() = %v, want %v", got, want)
	}
}

func TestV32MaintainNoOpSaveIsByteIdentical(t *testing.T) {
	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent"}
	data.SetTarget(scaffold.DefaultTarget)
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	before := packageBytes(t, root)
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := saveMaintained(newRunner(strings.NewReader("1\n"), &output, true), &agent); err != nil {
		t.Fatal(err)
	}
	after := packageBytes(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("no-op Save changed package bytes")
	}
	if !strings.Contains(output.String(), "No changes to save") {
		t.Fatalf("missing no-op notice:\n%s", output.String())
	}
}

func TestV32DestructiveSaveNamesFilesAndConfirms(t *testing.T) {
	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent"}
	data.SetTarget(scaffold.DefaultTarget)
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	agent.data.Speak.Language = "es-MX"
	var output bytes.Buffer
	// Confirm rewrite, then Back from the saved notice.
	if err := saveMaintained(newRunner(strings.NewReader("1\n1\n"), &output, true), &agent); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "language: es-MX") {
		t.Fatalf("saved manifest omitted edit:\n%s", manifest)
	}
	if !strings.Contains(output.String(), "agent.yaml") || !strings.Contains(output.String(), "Rewrite listed files") {
		t.Fatalf("confirmation omitted affected file:\n%s", output.String())
	}
}

func TestV20MaintainBackPreservesPriorEdits(t *testing.T) { // docs/spec/tui.md V20
	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent"}
	data.SetTarget(scaffold.DefaultTarget)
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	// Behavior → Greeting, new value, Back from the maintain menu.
	if err := editMaintained(newRunner(strings.NewReader("3\n2\nEdited greeting\n9\n"), &bytes.Buffer{}, true), &agent); err != nil {
		t.Fatal(err)
	}
	if agent.data.Greeting != "Edited greeting" {
		t.Fatalf("Back lost maintain edit: %#v", agent.data)
	}
}

func TestV33ValidateStaysInConsole(t *testing.T) {
	agent := testMaintainedAgent(t)
	var output bytes.Buffer
	runner := newRunner(strings.NewReader("6\n1\n9\n"), &output, true)
	runner.actions = func(action, path string, out io.Writer) error {
		if action != "validate" || path != agent.path {
			t.Fatalf("action = %q, path = %q", action, path)
		}
		_, _ = io.WriteString(out, "✓ pipecat (pipecat)\n")
		return nil
	}
	if err := editMaintained(runner, &agent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "pipecat") || strings.Count(output.String(), "Enter a number between 1 and 9") < 2 {
		t.Fatalf("Validate report did not return to maintain menu:\n%s", output.String())
	}
}

func TestV33CompileStaysInConsole(t *testing.T) {
	agent := testMaintainedAgent(t)
	var output bytes.Buffer
	runner := newRunner(strings.NewReader("7\n1\n9\n"), &output, true)
	runner.actions = func(action, path string, out io.Writer) error {
		if action != "compile" || path != agent.path {
			t.Fatalf("action = %q, path = %q", action, path)
		}
		_, _ = io.WriteString(out, "generated build/pipecat/agent.py\n")
		return nil
	}
	if err := editMaintained(runner, &agent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "generated build/pipecat/agent.py") || strings.Count(output.String(), "Enter a number between 1 and 9") < 2 {
		t.Fatalf("Compile report did not return to maintain menu:\n%s", output.String())
	}
}

func testMaintainedAgent(t *testing.T) maintainedAgent {
	t.Helper()
	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent"}
	data.SetTarget(scaffold.DefaultTarget)
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func TestOpenReportsUnrepresentableFieldPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent"}
	data.SetTarget(scaffold.DefaultTarget)
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	// Model definitions live in agent.yaml now (N15); endpoint_env is a field the
	// scaffold editor cannot represent, so opening the package must report it lost.
	path := filepath.Join(root, "agent.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Anchored on the constant rather than a literal, so a model bump is one edit
	// in internal/scaffold and this test follows it.
	anchor := []byte(`model: "` + scaffold.DefaultReasonModel + `"`)
	if !bytes.Contains(content, anchor) {
		t.Fatalf("fixture anchor %q not found; the scaffold moved under this test", anchor)
	}
	content = bytes.Replace(content, anchor, append(anchor, "\n      endpoint_env: CUSTOM_LLM_URL"...), 1)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(agent.losses, "\n"); !strings.Contains(got, "endpoint_env") || !strings.Contains(got, "agent.yaml") {
		t.Fatalf("loss report = %q", got)
	}
}

func packageBytes(t *testing.T, root string) map[string]string {
	t.Helper()
	paths, err := knownPackageFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]string, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		result[path] = string(content)
	}
	return result
}

// TestMaintainToolBlocksRoundTrip is the shape gate for maintenance (SCHEMA §5.2):
// every execution block a package can carry must survive a load → save → load
// cycle. The webhook block also carries its auth, so a rewrite never drops a
// tool's credentials, and the fieldless kinds must re-emit as `{}` — writing a
// bare `client:` or a blank `url_env:` makes the package unloadable.
func TestMaintainToolBlocksRoundTrip(t *testing.T) {
	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent"}
	// livekit: the only target that hosts every block this test writes (mcp is
	// still a Pipecat driver gate).
	data.SetTarget("livekit")
	input := `{"type":"object","properties":{}}`
	data.Tools = append(data.Tools,
		scaffold.Tool{
			Name: "lookup", Description: "Look up a customer.", Execution: "webhook",
			URLEnv: "LOOKUP_URL", Input: input, AttachTo: []string{"assistant"},
			Auth: &spec.ToolAuth{Type: "api_key", TokenEnv: "LOOKUP_KEY", Header: "X-API-Key"},
		},
		scaffold.Tool{
			Name: "notes", Description: "Load notes.", Execution: "local",
			Handler: "tools/notes.py", HandlerSource: "def notes():\n    return {}\n",
			Input: input, AttachTo: []string{"assistant"},
		},
		scaffold.Tool{
			Name: "search", Description: "Search the KB.", Execution: "mcp",
			URLEnv: "SEARCH_MCP_URL", Input: input, AttachTo: []string{"assistant"},
		},
	)
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	// Save only rewrites when something changed; edit one field so the real
	// write path runs rather than the no-change notice.
	agent.data.Instructions += "\n\nBe brief."
	if err := saveMaintained(newRunner(strings.NewReader("1\n1\n1\n1\n"), &bytes.Buffer{}, true), &agent); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadMaintained(root)
	if err != nil {
		t.Fatalf("saved package no longer loads: %v", err)
	}
	byName := map[string]scaffold.Tool{}
	for _, tool := range reloaded.data.Tools {
		byName[tool.Name] = tool
	}
	if got := byName["lookup"]; got.Execution != "webhook" || got.URLEnv != "LOOKUP_URL" ||
		got.Auth == nil || got.Auth.Type != "api_key" || got.Auth.TokenEnv != "LOOKUP_KEY" || got.Auth.Header != "X-API-Key" {
		t.Errorf("webhook tool round-trip lost fields: %#v (auth %#v)", got, got.Auth)
	}
	if got := byName["notes"]; got.Execution != "local" || got.Handler != "tools/notes.py" {
		t.Errorf("local tool round-trip = %#v", got)
	}
	if got := byName["search"]; got.Execution != "mcp" || got.URLEnv != "SEARCH_MCP_URL" {
		t.Errorf("mcp tool round-trip = %#v", got)
	}
}

// TestScaffoldFieldlessToolBlocks covers the two execution kinds with no fields:
// they must render as an explicit empty mapping, since a bare `client:` decodes
// to nothing and would fail as an empty execution kind (SCHEMA §5.2).
func TestScaffoldFieldlessToolBlocks(t *testing.T) {
	for _, kind := range []string{"client", "provider_hosted"} {
		t.Run(kind, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "agent")
			data := scaffold.Data{Name: "agent"}
			data.SetTarget(scaffold.DefaultTarget)
			data.Tools = append(data.Tools, scaffold.Tool{
				Name: "hosted", Description: "Hosted elsewhere.", Execution: kind,
				Input: `{"type":"object","properties":{}}`,
			})
			if _, err := scaffold.Write(root, data); err != nil {
				t.Fatal(err)
			}
			written, err := os.ReadFile(filepath.Join(root, "tools", "hosted.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(written), kind+": {}") {
				t.Errorf("%s block must render as an explicit empty mapping:\n%s", kind, written)
			}
			if strings.Contains(string(written), "url_env:") {
				t.Errorf("%s block must not carry url_env:\n%s", kind, written)
			}
		})
	}
}

// N32: deployment_region can hold several regions, and the maintain flow rewrites
// targets.yaml from scaffold.Data. A field that could hold only one region would
// silently drop the rest of an author's list on any unrelated edit, so both the
// editor field and the round-trip are checked here.
func TestMaintainKeepsEveryDeploymentRegion(t *testing.T) {
	root := filepath.Join(t.TempDir(), "agent")
	data := scaffold.Data{Name: "agent"}
	data.SetTarget("livekit")
	data.DeploymentRegions = []string{"us-east", "eu-central"}
	if _, err := scaffold.Write(root, data); err != nil {
		t.Fatal(err)
	}
	agent, err := loadMaintained(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(agent.data.DeploymentRegions, ","); got != "us-east,eu-central" {
		t.Fatalf("regions after load = %q, want both in order", got)
	}
	agent.data.Instructions += "\n\nBe brief."
	if err := saveMaintained(newRunner(strings.NewReader("1\n1\n1\n1\n"), &bytes.Buffer{}, true), &agent); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "targets.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "deployment_region:\n      - us-east\n      - eu-central\n") {
		t.Fatalf("several regions did not round-trip as a list:\n%s", manifest)
	}
	reloaded, err := loadMaintained(root)
	if err != nil {
		t.Fatalf("saved package no longer loads: %v", err)
	}
	if got := strings.Join(reloaded.data.DeploymentRegions, ","); got != "us-east,eu-central" {
		t.Fatalf("regions after save = %q, want both in order", got)
	}
}

// The advanced-target form edits the list as one comma-separated field, so the
// split on save is what keeps a second region.
func TestAdvancedTargetFormSplitsRegions(t *testing.T) { // N32
	data := scaffold.Data{Name: "agent"}
	data.SetTarget("livekit")
	// Pick the third field (deployment region), type both regions, then Back.
	runner := newRunner(strings.NewReader("3\nus-east, eu-central\n5\n"), &bytes.Buffer{}, true)
	if err := editAdvancedTarget(runner, &data); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(data.DeploymentRegions, ","); got != "us-east,eu-central" {
		t.Fatalf("regions = %q, want both split and trimmed", got)
	}
}
