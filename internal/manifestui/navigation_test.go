package manifestui

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSectionChooserKeepsInputAndCompletedDraft(t *testing.T) {
	m := editor(t, "manifest: acme\nversion: 4\n")
	choose(t, m, "Identity")
	formPage := m.current()
	key(m, tea.KeyCtrlU)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("café"), Paste: true})
	key(m, tea.KeyF2)
	if m.current().title != "Sections" {
		t.Fatal("F2 did not open sections")
	}
	choose(t, m, "Back")
	if m.current() != formPage || formPage.form.fields[0].input.Value() != "café" {
		t.Fatal("section chooser Back lost form input")
	}
	key(m, tea.KeyF2)
	choose(t, m, "Languages")
	if m.current().title != "Discard unfinished entry?" || m.current().cursor != 0 {
		t.Fatal("switching away from changed input must default to keeping it")
	}
	choose(t, m, "Keep editing")
	if m.current() != formPage {
		t.Fatal("Keep editing did not resume the form")
	}
	key(m, tea.KeyF2)
	choose(t, m, "Languages")
	choose(t, m, "Discard entry and switch")
	if m.current().title != "Languages" || m.draft.Name != "acme" || len(m.pages) != 2 {
		t.Fatal("section switch committed unfinished input or kept old pages")
	}
	key(m, tea.KeyF2)
	choose(t, m, "Identity")
	fill(t, m, "café", "5")
	key(m, tea.KeyF2)
	choose(t, m, "Models")
	if m.draft.Name != "café" || m.draft.Version != 5 {
		t.Fatal("switching section lost completed edits")
	}
}

func TestSectionChooserRestoresPositionWithFreshPages(t *testing.T) {
	m := editor(t, "manifest: acme\nversion: 1\n")
	choose(t, m, "Models")
	oldPage := m.current()
	key(m, tea.KeyDown)
	key(m, tea.KeyDown)
	oldPage.offset = 3
	key(m, tea.KeyF2)
	choose(t, m, "Languages")
	key(m, tea.KeyF2)
	choose(t, m, "Models")
	if m.current() == oldPage || m.current().cursor != 2 || m.current().offset != 3 {
		t.Fatal("section switch must rebuild data and retain selection/scroll")
	}
	key(m, tea.KeyF2)
	choose(t, m, "Identity")
	key(m, tea.KeyF2)
	choose(t, m, "Languages")
	if m.current().title != "Languages" {
		t.Fatal("unchanged form unexpectedly asked for confirmation")
	}
}

func TestPlainSectionChooser(t *testing.T) {
	m := editor(t, "manifest: acme\nversion: 1\n")
	var out bytes.Buffer
	err := m.runPlain(strings.NewReader(":sections\n1\n:sections\n3\n:quit\n2\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Sections", "Company name", "Languages", ":sections"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("plain section flow missing %q: %s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "Discard unfinished entry?") {
		t.Fatal("unchanged form prompted before plain section switch")
	}
}

func TestSectionChooserDoesNotBypassRequiredName(t *testing.T) {
	m, err := newModel(Options{})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	key(m, tea.KeyF2)
	if m.current().title != "Manifest name" || !strings.Contains(m.err, "name") {
		t.Fatal("section switch bypassed the required local name")
	}
}

func TestSectionChooserProtectsStagedEntries(t *testing.T) {
	for _, kind := range []string{"provider", "model region"} {
		t.Run(kind, func(t *testing.T) {
			m := editor(t, "manifest: acme\nversion: 1\n")
			if kind == "provider" {
				choose(t, m, "Models")
				choose(t, m, "listen")
				choose(t, m, "Add provider")
				choose(t, m, "slng")
			} else {
				choose(t, m, "Advanced")
				choose(t, m, "Model regions")
				choose(t, m, "Add region rule")
				choose(t, m, "Regions")
				choose(t, m, "Add value")
				fill(t, m, "eu")
			}
			stagedPage := m.current()
			key(m, tea.KeyF2)
			choose(t, m, "Languages")
			if m.current().title != "Discard unfinished entry?" || m.current().cursor != 0 {
				t.Fatal("F2 did not protect staged entry")
			}
			choose(t, m, "Keep editing")
			if m.current() != stagedPage {
				t.Fatal("Keep editing did not return to staged data")
			}
			if kind == "provider" {
				if !strings.Contains(m.current().title, "slng") {
					t.Fatal("staged provider lost")
				}
			} else {
				rows, _ := m.current().build()
				found := false
				for _, row := range rows {
					found = found || row.label == "eu"
				}
				if !found {
					t.Fatal("applied region value lost")
				}
			}
			key(m, tea.KeyF2)
			choose(t, m, "Languages")
			choose(t, m, "Discard entry and switch")
			if m.current().title != "Languages" || m.dirty() {
				t.Fatal("discarded staged entry was committed")
			}
		})
	}
}

func TestReviewSeparatesOptionsAndDefaultsToBack(t *testing.T) {
	m := editor(t, "")
	m.options.HasDefault = true
	key(m, tea.KeyF2)
	choose(t, m, "Review and save")
	rows, _ := m.current().build()
	if rows[m.current().cursor].label != "Back to editing" {
		t.Fatal("opening review must start on Back")
	}
	view := m.View()
	before, after, ok := strings.Cut(view, "CONTROLS")
	if !ok || !strings.Contains(before, "Use as default") || !strings.Contains(after, "Back to editing") || !strings.Contains(after, "Save manifest") {
		t.Fatalf("review mixes default option and controls:\n%s", view)
	}
}
