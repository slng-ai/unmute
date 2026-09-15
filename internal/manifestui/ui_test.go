package manifestui

import (
	"bytes"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
)

func editor(t *testing.T, raw string) *model {
	t.Helper()
	var rules *spec.Manifest
	if raw != "" {
		var err error
		rules, err = spec.ParseManifest([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
	}
	m, err := newModel(Options{Name: "acme", Rules: rules, Editing: rules != nil, Save: func(string, spec.Manifest, bool) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}
func key(m *model, k tea.KeyType) { m.Update(tea.KeyMsg{Type: k}) }
func choose(t *testing.T, m *model, label string) {
	t.Helper()
	choices, _ := m.current().build()
	for i, item := range choices {
		if item.label == label || strings.HasPrefix(item.label, label+" · ") {
			m.current().cursor = i
			key(m, tea.KeyEnter)
			return
		}
	}
	t.Fatalf("missing %q on %s: %v", label, m.current().title, choices)
}
func fill(t *testing.T, m *model, values ...string) {
	t.Helper()
	f := m.current().form
	if f == nil || len(f.fields) != len(values) {
		t.Fatalf("wrong form: %+v", f)
	}
	for _, value := range values {
		key(m, tea.KeyCtrlU)
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value), Paste: true})
		key(m, tea.KeyEnter)
	}
	key(m, tea.KeyEnter)
	if m.current().form != nil {
		t.Fatalf("form not applied: %s\n%s", m.err, m.View())
	}
}

func TestDraftNavigationAndSave(t *testing.T) {
	m := editor(t, "manifest: acme\nversion: 4\n")
	saved := 0
	m.options.Save = func(_ string, d spec.Manifest, _ bool) error {
		saved++
		if d.Languages == nil || d.Languages.Allow[0] != "es" || d.Version != 4 {
			t.Fatalf("wrong save: %+v", d)
		}
		return nil
	}
	choose(t, m, "Languages")
	choose(t, m, "[ ] Allow selected values")
	choose(t, m, "Add value")
	fill(t, m, "es")
	choose(t, m, "Back")
	if m.draft.Languages == nil {
		t.Fatal("applied rule missing")
	}
	choose(t, m, "Languages")
	choose(t, m, "Add value")
	fill(t, m, "en")
	key(m, tea.KeyEsc)
	if !reflect.DeepEqual(m.draft.Languages.Allow, []string{"es", "en"}) {
		t.Fatal("Back lost completed list edits")
	}
	if m.current().cursor != 2 {
		t.Fatal("home selection lost")
	}
	key(m, tea.KeyCtrlC)
	choose(t, m, "Keep editing")
	if m.done || saved != 0 {
		t.Fatal("exit prompt saved or discarded")
	}
	choose(t, m, "Review and save")
	if !strings.Contains(m.summary(), "Changes from saved rules:") {
		t.Fatal("review lost previous restrictions")
	}
	m.options.Save = func(string, spec.Manifest, bool) error { return errors.New("disk full") }
	choose(t, m, "Save manifest")
	if m.done || m.err != "disk full" || m.draft.Languages.Allow[0] != "es" {
		t.Fatal("failed save lost draft")
	}
	m.options.Save = func(string, spec.Manifest, bool) error { saved++; return nil }
	choose(t, m, "Save manifest")
	if !m.done || saved != 1 {
		t.Fatal("save did not finish")
	}
}
func TestFormsPasteAndFocus(t *testing.T) {
	m := editor(t, "manifest: acme\nversion: 4\n")
	choose(t, m, "Identity")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bad\nname"), Paste: true})
	f := m.current().form
	if f.fields[0].input.Value() != "acme" || f.fields[0].err == "" {
		t.Fatal("multiline paste was silently altered")
	}
	key(m, tea.KeyCtrlU)
	m.Update(clipboardText{text: "Café 日本"})
	if f.focus != 0 || f.fields[0].input.Value() != "Café 日本" {
		t.Fatal("paste changed focus or text")
	}
	key(m, tea.KeyHome)
	key(m, tea.KeyDelete)
	key(m, tea.KeyEnd)
	key(m, tea.KeyBackspace)
	if f.fields[0].input.Value() != "afé 日" {
		t.Fatal(f.fields[0].input.Value())
	}
	key(m, tea.KeyTab)
	key(m, tea.KeyCtrlU)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("0")})
	key(m, tea.KeyTab)
	key(m, tea.KeyEnter)
	if f.focus != 1 || f.fields[1].err == "" {
		t.Fatal("invalid revision did not focus error")
	}
	key(m, tea.KeyCtrlU)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	key(m, tea.KeyEnter)
	key(m, tea.KeyEnter)
	if m.draft.Version != 5 || m.draft.Name != "afé 日" {
		t.Fatal("correction was not saved to draft")
	}
	choose(t, m, "Identity")
	key(m, tea.KeyCtrlU)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("discard")})
	key(m, tea.KeyEsc)
	if m.draft.Name != "afé 日" {
		t.Fatal("Back applied field")
	}
}

func TestEveryRuleRoundTripsAndDoesNotMutateOriginal(t *testing.T) {
	const raw = `manifest: acme
version: 4
models:
  listen:
    - provider: custom
      allow: [one, two]
  think:
    - provider: google
      allow: [gemini]
languages:
  allow: [en, es]
targets:
  allow: [livekit, pipecat]
tracing:
  allow: []
tools:
  kinds:
    allow: [webhook, local, mcp, builtin, client, provider_hosted, knowledge, slng]
  names:
    allow: [lookup]
  builtin:
    allow: []
  slng:
    allow: [hosted]
regions:
  models:
    - role: listen
      provider: custom
      allow: [eu, us]
    - role: speak
      provider: custom
      allow: []
  deployments:
    - provider: livekit
      allow: [eu-central]
    - provider: pipecat
      allow: []
`
	m := editor(t, raw)
	for _, r := range m.simpleRules() {
		m.editRule(r)
		choose(t, m, "Back")
	}
	choose(t, m, "Models")
	for _, role := range []string{"listen", "think", "speak"} {
		choose(t, m, role)
		choose(t, m, "Back")
	}
	choose(t, m, "Back")
	choose(t, m, "Advanced")
	for _, section := range []string{"Model regions", "Deployment regions"} {
		choose(t, m, section)
		choose(t, m, "Back")
	}
	choose(t, m, "Back")
	if !reflect.DeepEqual(m.options.Rules, &m.draft) {
		t.Fatalf("round trip changed rules: %+v", m.draft)
	}
	data, err := Encode(m.draft)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := spec.ParseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, m.options.Rules) {
		t.Fatal("encoding changed rules")
	}
	choose(t, m, "Review and save")
	m.options.Save = func(string, spec.Manifest, bool) error { t.Fatal("unchanged edit wrote file"); return nil }
	choose(t, m, "Save manifest")
	if !m.done {
		t.Fatal("unchanged edit did not finish")
	}
}

func TestModelAndRegionEditsReachDraftWithoutAncestorApply(t *testing.T) {
	m := editor(t, "")
	choose(t, m, "Models")
	choose(t, m, "listen")
	choose(t, m, "Add provider")
	choose(t, m, "Enter another provider")
	fill(t, m, "my-provider")
	if m.draft.Models.Listen != nil {
		t.Fatal("incomplete provider attached")
	}
	choose(t, m, "Add model ID")
	fill(t, m, "model/one:v1")
	if m.draft.Models.Listen[0].Provider != "my-provider" {
		t.Fatal("completed provider did not reach draft immediately")
	}
	for range 4 {
		choose(t, m, "Back")
	}
	choose(t, m, "Advanced")
	choose(t, m, "Model regions")
	choose(t, m, "Add region rule")
	choose(t, m, "Role")
	choose(t, m, "listen")
	choose(t, m, "Provider")
	choose(t, m, "Enter another provider")
	fill(t, m, "my-provider")
	if m.draft.Regions != nil {
		t.Fatal("incomplete region attached")
	}
	choose(t, m, "Regions")
	choose(t, m, "[ ] Allow selected values")
	choose(t, m, "Add value")
	fill(t, m, "eu-west")
	if m.draft.Regions.Models[0].Allow[0] != "eu-west" {
		t.Fatal("completed region did not reach draft immediately")
	}
	choose(t, m, "Back")
	choose(t, m, "Back")
	choose(t, m, "listen my-provider")
	choose(t, m, "Remove rule")
	choose(t, m, "Keep editing")
	if len(m.draft.Regions.Models) != 1 {
		t.Fatal("cancelled removal reached draft")
	}
	choose(t, m, "Remove rule")
	choose(t, m, "Confirm")
	choose(t, m, "Back")
	if m.draft.Regions != nil {
		t.Fatal("Back lost completed removal")
	}
}

func TestRestrictionStatesAndValidation(t *testing.T) {
	for _, state := range []string{"No restriction", "Allow selected values", "Allow nothing"} {
		t.Run(state, func(t *testing.T) {
			m := editor(t, "")
			choose(t, m, "Languages")
			prefix := "[ ] "
			if state == "No restriction" {
				prefix = "[x] "
			}
			choose(t, m, prefix+state)
			if state == "Allow selected values" {
				if m.draft.Languages != nil {
					t.Fatal("empty selected list reached draft")
				}
				choose(t, m, "Add value")
				m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("not_a_tag"), Paste: true})
				key(m, tea.KeyTab)
				key(m, tea.KeyEnter)
				if m.current().form == nil || m.err == "" || m.draft.Languages != nil {
					t.Fatal("invalid language did not stay in form without changing draft")
				}
				if m.current().form.focus != 0 {
					t.Fatal("validation error did not focus the input")
				}
				fill(t, m, "en-US")
			}
			choose(t, m, "Back")
			if m.current().title != "Manifest" {
				t.Fatal(m.err)
			}
			switch state {
			case "No restriction":
				if m.draft.Languages != nil {
					t.Fatal("nil lost")
				}
			case "Allow nothing":
				if m.draft.Languages == nil || m.draft.Languages.Allow == nil || len(m.draft.Languages.Allow) != 0 {
					t.Fatal("empty list lost")
				}
			default:
				if m.draft.Languages.Allow[0] != "en-US" {
					t.Fatal("wrong value")
				}
			}
		})
	}
}

func TestLayoutResizeAndExit(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := editor(t, "")
	var values []string
	for i := range 50 {
		values = append(values, fmt.Sprintf("model-%02d-%s", i, strings.Repeat("long", 30)))
	}
	m.editAllow("Long values", "value", "", values, nil, true, func([]string) error { return nil })
	for _, size := range [][2]int{{120, 40}, {100, 30}, {80, 24}, {59, 17}, {80, 24}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		key(m, tea.KeyEnd)
		view := m.View()
		if strings.Contains(view, "\x1b[") {
			t.Fatal("NO_COLOR leaked escapes")
		}
		if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
			t.Fatalf("overflow at %v: %dx%d\n%s", size, lipgloss.Width(view), lipgloss.Height(view), view)
		}
		if size[0] >= 60 && !strings.Contains(view, "Back") {
			t.Fatal("selected last action is invisible")
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	key(m, tea.KeyCtrlC)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if !m.done {
		t.Fatal("small terminal cannot exit")
	}
}
func TestReviewScrollKeepsActionsVisible(t *testing.T) {
	m := editor(t, "")
	m.draft.Languages = &spec.ManifestAllow{Allow: []string{"en"}}
	m.review()
	for _, keyType := range []tea.KeyType{tea.KeyPgDown, tea.KeyPgDown, tea.KeyPgUp} {
		key(m, keyType)
		view := m.View()
		if !strings.Contains(view, "Save manifest") || !strings.Contains(view, "Back to editing") || lipgloss.Height(view) > 24 {
			t.Fatal(view)
		}
	}
}
func TestPlainModeAndEndOfInput(t *testing.T) {
	var out bytes.Buffer
	saved := 0
	options := Options{Name: "acme", Save: func(string, spec.Manifest, bool) error { saved++; return nil }}
	if err := Run(strings.NewReader("8\n2\n"), &out, true, options); err != nil {
		t.Fatal(err)
	}
	if saved != 1 {
		t.Fatal("not saved")
	}
	if err := Run(strings.NewReader("1\n"), &out, true, options); err == nil {
		t.Fatal("EOF accepted as save")
	}
	if saved != 1 {
		t.Fatal("EOF saved draft")
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatal("plain mode leaked styling")
	}
}
func TestManifestUIIsIndependent(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if strings.Contains(imp.Path.Value, "internal/tui") {
				t.Fatalf("%s imports the agent console", name)
			}
		}
	}
	// One local visual fixture keeps the shared brand and navigation reviewable.
	m := editor(t, "")
	t.Setenv("NO_COLOR", "1")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if os.Getenv("UPDATE_MANIFEST_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("testdata/home.txt", []byte(m.View()+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/home.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != m.View()+"\n" {
		t.Fatal("manifest home changed; inspect and regenerate with UPDATE_MANIFEST_GOLDEN=1")
	}
}

func TestLongInputFitsAndCannotChangeWhileTooSmall(t *testing.T) {
	m := editor(t, "")
	for _, width := range []int{120, 100, 80} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		choose(t, m, "Identity")
		key(m, tea.KeyCtrlU)
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("日本", 100)), Paste: true})
		view := m.View()
		if lipgloss.Width(view) > width || lipgloss.Height(view) > 24 {
			t.Fatalf("long input overflows at %d: %dx%d", width, lipgloss.Width(view), lipgloss.Height(view))
		}
		previous := m.current().form.fields[0].input.Value()
		m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ignore"), Paste: true})
		key(m, tea.KeyEnter)
		if m.current().form.fields[0].input.Value() != previous || m.current().form.focus != 0 {
			t.Fatal("small terminal accepted input")
		}
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		key(m, tea.KeyEsc)
	}
}

func TestSpeechProviderPickerIncludesSLNGAndDirectServices(t *testing.T) {
	for _, role := range []string{"listen", "speak", "think"} {
		t.Run(role, func(t *testing.T) {
			m := editor(t, "")
			got := ""
			m.chooseProvider("Provider", "", role, func(provider string) error { got = provider; return nil })
			choose(t, m, "slng")
			if got != "slng" {
				t.Fatal("gateway was replaced with a model maker")
			}
		})
	}
}

func TestMultipleSpeechProvidersAndModelsSurviveUIAndValidation(t *testing.T) {
	m := editor(t, "")
	want := map[string][]spec.ManifestModel{
		"listen": {{Provider: "slng", Allow: []string{"deepgram/nova:3", "soniox/speech-ai:rt-v5"}}, {Provider: "deepgram", Allow: []string{"direct-one", "direct-two"}}},
		"speak":  {{Provider: "slng", Allow: []string{"cartesia/sonic:3.5", "deepgram/aura:2"}}, {Provider: "cartesia", Allow: []string{"direct-one", "direct-two"}}},
	}
	choose(t, m, "Models")
	for _, role := range []string{"listen", "speak"} {
		choose(t, m, role)
		for _, entry := range want[role] {
			choose(t, m, "Add provider")
			choose(t, m, entry.Provider)
			for _, id := range entry.Allow {
				choose(t, m, "Add model ID")
				fill(t, m, id)
			}
			choose(t, m, "Back")
			choose(t, m, "Back")
		}
		choose(t, m, "Back")
	}
	choose(t, m, "Back")
	data, err := Encode(m.draft)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := spec.ParseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"listen", "speak"} {
		if got := *roleRows(&restored.Models, role); !reflect.DeepEqual(got, want[role]) {
			t.Fatalf("%s lost models/providers: %+v", role, got)
		}
		for _, entry := range want[role] {
			for _, id := range entry.Allow {
				agent := &ir.Agent{Manifest: restored, Models: map[string]ir.ModelDef{"test": {Kind: ir.ModelKind(role), Provider: entry.Provider, Model: id}}}
				if errs, _ := ir.ValidateManifest(agent); len(errs) > 0 {
					t.Fatalf("%s/%s refused: %v", entry.Provider, id, errs)
				}
				agent.Models["test"] = ir.ModelDef{Kind: ir.ModelKind(role), Provider: entry.Provider, Model: "not-allowed"}
				if errs, _ := ir.ValidateManifest(agent); len(errs) == 0 {
					t.Fatal("unlisted model accepted")
				}
			}
		}
		// A maker name inside an SLNG ID is not a separate allowed service provider.
		maker, id := "soniox", "soniox/speech-ai:rt-v5"
		if role == "speak" {
			maker, id = "deepgram", "deepgram/aura:2"
		}
		agent := &ir.Agent{Manifest: restored, Models: map[string]ir.ModelDef{"test": {Kind: ir.ModelKind(role), Provider: maker, Model: id}}}
		if errs, _ := ir.ValidateManifest(agent); len(errs) == 0 {
			t.Fatal("SLNG model accidentally granted a direct provider")
		}
	}
	// Reopening the draft in the editor keeps both providers and their lists.
	options := m.options
	options.Rules = restored
	options.Editing = true
	edited, err := newModel(options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(edited.draft.Models, restored.Models) {
		t.Fatal("reopening lost speech choices")
	}
}

func TestNavigationHelpPreservesInputAndFits(t *testing.T) {
	m := editor(t, "")
	choose(t, m, "Models")
	choose(t, m, "listen")
	choose(t, m, "Add provider")
	choose(t, m, "slng")
	choose(t, m, "Add model ID")
	f := m.current().form
	if f.fields[0].label != "Model ID" || !strings.Contains(f.help, "Keep slng as the provider") || !strings.Contains(f.help, "Use Tab to move between fields, Apply and Back") || !strings.Contains(f.help, "Arrow keys do not move between fields") {
		t.Fatal("model input lacks context or navigation")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("deepgram/nova:3"), Paste: true})
	inputPage := m.current()
	key(m, tea.KeyF1)
	_, help := m.current().build()
	for _, text := range []string{"Shift+Tab", "Add model ID", "Add provider", "Save manifest"} {
		if !strings.Contains(help, text) {
			t.Fatalf("missing help: %s", text)
		}
	}
	for _, width := range []int{100, 80, 60} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		key(m, tea.KeyPgDown)
		view := m.View()
		if lipgloss.Width(view) > width || lipgloss.Height(view) > 24 || !strings.Contains(view, "Back") {
			t.Fatalf("help overflow at %d: %s", width, view)
		}
	}
	key(m, tea.KeyEsc)
	if m.current() != inputPage || f.fields[0].input.Value() != "deepgram/nova:3" {
		t.Fatal("help changed input")
	}
	key(m, tea.KeyEnter)
	key(m, tea.KeyEnter)
	choose(t, m, "Back")
	choose(t, m, "Back")
	choose(t, m, "Back")
	choose(t, m, "Back")
	key(m, tea.KeyF1)
	key(m, tea.KeyF1)
	if len(m.pages) != 2 {
		t.Fatal("help recursively opened")
	}
}

func TestHelpFromExitPromptKeepsExitState(t *testing.T) {
	m := editor(t, "")
	key(m, tea.KeyCtrlC)
	key(m, tea.KeyF1)
	key(m, tea.KeyEsc)
	if !m.quitting || !m.current().exitPrompt {
		t.Fatal("returning from help lost exit confirmation")
	}
	choose(t, m, "Keep editing")
	if m.quitting || len(m.pages) != 1 {
		t.Fatal("exit confirmation did not close")
	}
}

func TestModelListSeparatesRowsFromActions(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := editor(t, "")
	values := []string{"cartesia/sonic:3", "cartesia/sonic:3.5"}
	var saved []string
	m.editAllow("Model IDs · slng", "model ID", modelIDHelp("slng", "speak"), values, nil, false, func(values []string) error { saved = values; return nil })
	view := m.View()
	for _, label := range []string{"ALLOWED MODELS (2)", "› 01  cartesia/sonic:3", "02  cartesia/sonic:3.5", "ACTIONS", "Add model ID", "CONTROLS", "Back"} {
		if !strings.Contains(view, label) {
			t.Fatalf("missing %q:\n%s", label, view)
		}
	}
	if strings.Contains(view, "including its maker") {
		t.Fatal("long model-ID instructions still crowd the list")
	}
	if os.Getenv("UPDATE_MANIFEST_GOLDEN") == "1" {
		if err := os.WriteFile("testdata/models_80x24.txt", []byte(view+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/models_80x24.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != view+"\n" {
		t.Fatal("model list layout changed; inspect and regenerate with UPDATE_MANIFEST_GOLDEN=1")
	}
	// Presentation numbers never become part of a model ID or its menu action.
	key(m, tea.KeyEnter)
	choose(t, m, "Edit model ID")
	fill(t, m, "cartesia/sonic:updated")
	if m.current().title != "Model IDs · slng" || !strings.Contains(m.View(), "› 01  cartesia/sonic:updated") {
		t.Fatalf("Apply did not return to the updated model list:\n%s", m.View())
	}
	choose(t, m, "Back")
	if !reflect.DeepEqual(saved, []string{"cartesia/sonic:updated", "cartesia/sonic:3.5"}) {
		t.Fatalf("display changed IDs/order: %v", saved)
	}
}

func TestModelListScrollsAndEmptyStateIsClear(t *testing.T) {
	m := editor(t, "")
	var models []string
	for i := range 40 {
		models = append(models, fmt.Sprintf("maker/model-%02d:version", i))
	}
	m.editAllow("Model IDs · slng", "model ID", "", models, nil, false, func([]string) error { return nil })
	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 18}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		key(m, tea.KeyEnd)
		view := m.View()
		if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] || !strings.Contains(view, "› Back") {
			t.Fatalf("actions clipped at %v:\n%s", size, view)
		}
		key(m, tea.KeyHome)
	}
	key(m, tea.KeyEsc)
	m.editAllow("Model IDs · slng", "model ID", "", []string{}, nil, false, func([]string) error { return nil })
	_, description := m.current().build()
	if !strings.Contains(description, "Allow all models") {
		t.Fatal(description)
	}
}

func TestControlsAreSeparateFromContent(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, screen := range []string{"provider", "region", "models", "form"} {
		t.Run(screen, func(t *testing.T) {
			m := editor(t, "")
			content := ""
			switch screen {
			case "provider":
				m.editModelEntry("listen", spec.ManifestModel{Provider: "slng", Allow: []string{"soniox/speech-ai:rt-v5"}}, func(spec.ManifestModel) error { return nil }, func() {})
				content = "Remove provider"
			case "region":
				m.regionEntry(regionEntry{provider: "livekit", allow: []string{"eu"}}, false, func(regionEntry) error { return nil }, func() {})
				content = "Remove rule"
			case "models":
				// IDs that match control labels must remain ordinary data rows.
				m.editAllow("Model IDs · slng", "model ID", "", []string{"Apply", "Back"}, nil, false, func([]string) error { return nil })
				content = "Add model ID"
			case "form":
				m.identity()
				content = "Revision"
			}
			for _, size := range [][2]int{{120, 40}, {100, 30}, {80, 24}} {
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				view := m.View()
				before, after, found := strings.Cut(view, "CONTROLS")
				if !found || !strings.Contains(before, content) || !strings.Contains(after, "Back") || (screen == "form" && !strings.Contains(after, "Apply")) {
					t.Fatalf("controls mixed with content at %v:\n%s", size, view)
				}
				if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
					t.Fatalf("controls clipped at %v:\n%s", size, view)
				}
			}
			if screen != "form" {
				rows, _ := m.current().build()
				if rows[len(rows)-1].label != "Back" {
					t.Fatal("Back must follow content actions")
				}
				if m.current().controls != 1 {
					t.Fatal("list still requires ancestor Apply")
				}
				var plain bytes.Buffer
				_ = m.runPlain(strings.NewReader(":quit\n2\n"), &plain)
				if !strings.Contains(plain.String(), "\nCONTROLS\n") {
					t.Fatal("plain mode lost the control group")
				}
			}
		})
	}
}

func TestEditedValueReturnsToListOnlyOnSuccess(t *testing.T) {
	for _, finite := range [][]string{nil, {"first", "second", "replacement"}} {
		m := editor(t, "")
		original := []string{"first", "second"}
		var saved []string
		m.editAllow("Allowed values", "value", "", original, finite, false, func(values []string) error { saved = values; return nil })
		list := m.current()
		choose(t, m, "first")
		entry := m.current()
		choose(t, m, "Edit value")
		picker := m.current()
		if finite == nil {
			key(m, tea.KeyCtrlU)
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("second"), Paste: true})
			key(m, tea.KeyTab)
			key(m, tea.KeyEnter)
		} else {
			choose(t, m, "second")
		}
		if m.current() != picker || m.err == "" {
			t.Fatal("duplicate must keep the editor open with an error")
		}
		key(m, tea.KeyEsc)
		if m.current() != entry {
			t.Fatal("Back from the editor must return without applying")
		}
		choose(t, m, "Edit value")
		if finite == nil {
			fill(t, m, "replacement")
		} else {
			choose(t, m, "replacement")
		}
		if m.current() != list || !strings.Contains(m.View(), "replacement") || !reflect.DeepEqual(saved, []string{"replacement", "second"}) {
			t.Fatal("successful edit must return to the list and update its parent")
		}
		choose(t, m, "Back")
		if !reflect.DeepEqual(saved, []string{"replacement", "second"}) || original[0] != "first" {
			t.Fatal("Back must retain completed changes without mutating original bytes")
		}
	}
}

func TestModelRestrictionsCannotBeChangedToBlockAll(t *testing.T) {
	m := editor(t, "manifest: acme\nversion: 1\nmodels:\n  listen:\n    - provider: slng\n      allow: [first, second]\n")
	choose(t, m, "Models")
	choose(t, m, "listen")
	rows, _ := m.current().build()
	for _, row := range rows {
		if strings.Contains(row.label, "Allow nothing") || row.label == "Apply" {
			t.Fatal("role offers block-all or redundant Apply")
		}
	}
	choose(t, m, "slng")
	choose(t, m, "Models")
	rows, _ = m.current().build()
	for _, row := range rows {
		if strings.Contains(row.label, "Allow nothing") || row.label == "Apply" {
			t.Fatal("model list offers block-all or redundant Apply")
		}
	}
	choose(t, m, "first")
	choose(t, m, "Remove model ID")
	choose(t, m, "Confirm")
	if !reflect.DeepEqual(m.draft.Models.Listen[0].Allow, []string{"second"}) {
		t.Fatal("removal did not reach draft")
	}
	choose(t, m, "second")
	choose(t, m, "Remove model ID")
	if m.err == "" {
		t.Fatal("last model removal needs an explanation")
	}
	if !reflect.DeepEqual(m.draft.Models.Listen[0].Allow, []string{"second"}) {
		t.Fatal("last model removal blocked the provider")
	}
	choose(t, m, "Back")
	choose(t, m, "Back")
	choose(t, m, "Remove provider")
	choose(t, m, "Keep editing")
	if len(m.draft.Models.Listen) != 1 {
		t.Fatal("cancelled provider removal changed draft")
	}
	choose(t, m, "Remove provider")
	choose(t, m, "Confirm")
	if m.draft.Models.Listen != nil {
		t.Fatal("removing last provider did not restore No restriction")
	}
}

func TestGuidedSaveRefusesLegacyEmptyModelRules(t *testing.T) {
	for _, models := range []string{"  listen: []\n", "  speak:\n    - provider: slng\n      allow: []\n"} {
		m := editor(t, "manifest: acme\nversion: 1\nmodels:\n"+models)
		data, err := Encode(m.draft)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := spec.ParseManifest(data)
		if err != nil || !reflect.DeepEqual(restored, m.options.Rules) {
			t.Fatal("legacy empty model restriction changed before repair")
		}
		m.options.Save = func(string, spec.Manifest, bool) error { t.Fatal("empty model rule reached storage"); return nil }
		choose(t, m, "Review and save")
		choose(t, m, "Save manifest")
		if m.done || m.err == "" {
			t.Fatal("legacy empty model rule was accepted without repair")
		}
	}
}

func TestAllAllowListsUseTheSameGroups(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, noun := range []string{"value", "model ID"} {
		m := editor(t, "")
		m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		m.editAllow("Allowed values", noun, "", []string{"first", "second"}, nil, noun != "model ID", func([]string) error { return nil })
		view := m.View()
		for _, label := range []string{"01  first", "02  second", "ACTIONS", "CONTROLS"} {
			if !strings.Contains(view, label) {
				t.Fatalf("%s missing %s:\n%s", noun, label, view)
			}
		}
	}
}

func TestIncompleteEntriesStayLocalAndExplicitEmptyRegionIsKept(t *testing.T) {
	m := editor(t, "")
	choose(t, m, "Models")
	choose(t, m, "listen")
	choose(t, m, "Add provider")
	choose(t, m, "slng")
	choose(t, m, "Back")
	if m.draft.Models.Listen != nil {
		t.Fatal("Back attached a provider with no models")
	}
	choose(t, m, "Back")
	choose(t, m, "Back")
	choose(t, m, "Back")
	choose(t, m, "Advanced")
	choose(t, m, "Deployment regions")
	choose(t, m, "Add region rule")
	choose(t, m, "Provider")
	choose(t, m, "livekit")
	choose(t, m, "Back")
	if m.draft.Regions != nil {
		t.Fatal("Back attached a region before a restriction was chosen")
	}
	choose(t, m, "Add region rule")
	choose(t, m, "Provider")
	choose(t, m, "livekit")
	choose(t, m, "Regions")
	choose(t, m, "[ ] Allow nothing")
	choose(t, m, "Back")
	choose(t, m, "Back")
	if m.draft.Regions == nil || len(m.draft.Regions.Deployments) != 1 || m.draft.Regions.Deployments[0].Allow == nil || len(m.draft.Regions.Deployments[0].Allow) != 0 {
		t.Fatal("explicitly empty region restriction was lost")
	}
}

func TestNewEntriesCanBeRemovedWithoutReopening(t *testing.T) {
	for _, section := range []string{"provider", "region"} {
		t.Run(section, func(t *testing.T) {
			m := editor(t, "")
			remove := "Remove provider"
			if section == "provider" {
				choose(t, m, "Models")
				choose(t, m, "listen")
				choose(t, m, "Add provider")
				choose(t, m, "slng")
			} else {
				remove = "Remove rule"
				choose(t, m, "Advanced")
				choose(t, m, "Deployment regions")
				choose(t, m, "Add region rule")
				choose(t, m, "Provider")
				choose(t, m, "livekit")
			}
			detail := m.current()
			if section == "provider" {
				detail = m.pages[len(m.pages)-2]
				choose(t, m, "Add model ID")
				fill(t, m, "soniox/speech-ai:rt-v5")
				if len(m.draft.Models.Listen) != 1 {
					t.Fatal("new provider did not reach draft")
				}
			} else {
				choose(t, m, "Regions")
				choose(t, m, "Add value")
				fill(t, m, "eu-central")
				if m.draft.Regions == nil || len(m.draft.Regions.Deployments) != 1 {
					t.Fatal("new region did not reach draft")
				}
			}
			choose(t, m, "Back")
			if m.current() != detail {
				t.Fatal("detail page was replaced")
			}
			choose(t, m, remove)
			choose(t, m, "Confirm")
			if m.draft.Models.Listen != nil || m.draft.Regions != nil {
				t.Fatal("new entry's Remove did not update draft")
			}
		})
	}
}

func TestAddingValuesKeepsAddFocused(t *testing.T) {
	for _, noun := range []string{"value", "model ID"} {
		m := editor(t, "")
		var saved []string
		m.editAllow("Allowed", noun, "", nil, nil, noun != "model ID", func(values []string) error { saved = values; return nil })
		choose(t, m, "Add "+noun)
		fill(t, m, "first")
		rows, _ := m.current().build()
		if rows[m.current().cursor].label != "Add "+noun {
			t.Fatal("Add must stay focused so Enter adds the next value")
		}
		key(m, tea.KeyEnter)
		fill(t, m, "second")
		if !reflect.DeepEqual(saved, []string{"first", "second"}) {
			t.Fatal("repeated entry did not reach the draft")
		}
	}
}

func TestProviderShortcutAndAllModels(t *testing.T) {
	for _, role := range []string{"listen", "speak", "think"} {
		t.Run(role, func(t *testing.T) {
			m := editor(t, "")
			choose(t, m, "Models")
			choose(t, m, role)
			choose(t, m, "Add provider")
			if m.current().title != "Provider" {
				t.Fatal("Add provider did not open picker")
			}
			key(m, tea.KeyEsc)
			if m.current().title != role {
				t.Fatal("cancel provider picker left a blank detail page")
			}
			choose(t, m, "Add provider")
			choose(t, m, "slng")
			if m.current().title != "Model IDs · slng" {
				t.Fatal("provider did not open model choices")
			}
			if *roleRows(&m.draft.Models, role) != nil {
				t.Fatal("provider selection implicitly allowed all models")
			}
			choose(t, m, "[ ] Allow all models")
			entries := *roleRows(&m.draft.Models, role)
			if len(entries) != 1 || entries[0].Allow != nil {
				t.Fatal("all models not committed")
			}
			if err := validateGuidedModels(m.draft); err != nil {
				t.Fatal(err)
			}
			data, err := Encode(m.draft)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := spec.ParseManifest(data)
			if err != nil || (*roleRows(&restored.Models, role))[0].Allow != nil {
				t.Fatalf("all models lost: %s, %v", data, err)
			}
			choose(t, m, "Add model ID")
			key(m, tea.KeyEsc)
			if (*roleRows(&m.draft.Models, role))[0].Allow != nil {
				t.Fatal("cancel narrowed provider")
			}
			choose(t, m, "Add model ID")
			fill(t, m, "maker/first")
			key(m, tea.KeyEnter) // Add remains focused.
			fill(t, m, "maker/second")
			if !reflect.DeepEqual((*roleRows(&m.draft.Models, role))[0].Allow, []string{"maker/first", "maker/second"}) {
				t.Fatal("repeat add lost models")
			}
			choose(t, m, "[ ] Allow all models")
			choose(t, m, "Keep editing")
			if len((*roleRows(&m.draft.Models, role))[0].Allow) != 2 {
				t.Fatal("cancel lifted restriction")
			}
			choose(t, m, "[ ] Allow all models")
			choose(t, m, "Confirm")
			key(m, tea.KeyF2)
			choose(t, m, "Review and save")
			if !strings.Contains(m.View(), "slng: All models") {
				t.Fatal("review omitted all models")
			}
		})
	}
}
