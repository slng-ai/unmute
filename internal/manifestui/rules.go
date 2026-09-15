package manifestui

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/slng-ai/unmute/internal/spec"
	target "github.com/slng-ai/unmute/internal/target"
)

var sectionNames = []string{"Identity", "Models", "Languages", "Targets", "Tools", "Tracing", "Advanced", "Review and save"}

func allowLabel(values []string) string {
	if values == nil {
		return "No restriction"
	}
	if len(values) == 0 {
		return "Allow nothing"
	}
	return "Allow: " + strings.Join(values, ", ")
}
func providers() []string {
	values := make([]string, len(target.Providers))
	for i, p := range target.Providers {
		values[i] = string(p)
	}
	return values
}

// A manifest stores the service provider, not the brands it distributes.
// Brands would hide SLNG and offer its model makers in its place.
func modelProviders(role string) []string {
	r := target.Role(role)
	if role == "think" {
		r = target.Reason
	}
	var values []string
	for _, p := range target.Providers {
		values = append(values, target.DefaultCatalog().Vendors(p, r)...)
	}
	slices.Sort(values)
	return slices.Compact(values)
}
func (m *model) home() *page {
	return &page{title: "Manifest", build: func() ([]item, string) {
		rows := []item{
			{"Identity · " + m.draft.Name + " · revision " + strconv.Itoa(m.draft.Version), m.identity},
			{"Models · " + modelSummary(m.draft.Models), m.models},
		}
		for _, r := range m.simpleRules()[:4] {
			rows = append(rows, item{r.title + " · " + allowLabel(allowValues(r.get(m.draft))), func() { m.editRule(r) }})
		}
		advanced := "No restriction"
		count := 0
		for _, r := range m.simpleRules()[4:] {
			if r.get(m.draft) != nil {
				count++
			}
		}
		if m.draft.Regions != nil {
			count += len(m.draft.Regions.Models) + len(m.draft.Regions.Deployments)
		}
		if count > 0 {
			advanced = fmt.Sprintf("%d rules", count)
		}
		rows = append(rows, item{"Advanced · " + advanced, m.advanced}, item{"Review and save", m.review}, item{"Cancel", m.exit})
		return rows, "Choose the company rules to set. Nothing is saved until you review and save."
	}}
}
func modelSummary(models spec.ManifestModels) string {
	count := 0
	for _, rows := range [][]spec.ManifestModel{models.Listen, models.Think, models.Speak} {
		if rows != nil {
			count++
		}
	}
	if count == 0 {
		return "No restriction"
	}
	return fmt.Sprintf("%d roles restricted", count)
}
func (m *model) identity() {
	m.openForm("Identity", []string{"Company name", "Revision"}, []string{m.draft.Name, strconv.Itoa(m.draft.Version)}, []func(string) error{validateText, func(s string) error {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return fmt.Errorf("enter a positive whole number")
		}
		return nil
	}}, func(values []string) error {
		version, err := strconv.Atoi(values[1])
		if err != nil {
			return err
		}
		m.draft.Name, m.draft.Version = values[0], version
		return nil
	})
}

type rule struct {
	title  string
	get    func(spec.Manifest) *spec.ManifestAllow
	set    func(*spec.Manifest, *spec.ManifestAllow)
	finite []string
}

func allowValues(rule *spec.ManifestAllow) []string {
	if rule == nil {
		return nil
	}
	return rule.Allow
}
func toolRule(title string, index int) rule {
	return rule{title: title, get: func(d spec.Manifest) *spec.ManifestAllow {
		if d.Tools == nil {
			return nil
		}
		return []*spec.ManifestAllow{d.Tools.Kinds, d.Tools.Names, d.Tools.Builtin, d.Tools.Slng}[index]
	}, set: func(d *spec.Manifest, a *spec.ManifestAllow) {
		var tools spec.ManifestTools
		if d.Tools != nil {
			tools = *d.Tools
		}
		switch index {
		case 0:
			tools.Kinds = a
		case 1:
			tools.Names = a
		case 2:
			tools.Builtin = a
		case 3:
			tools.Slng = a
		}
		d.Tools = &tools
		if tools == (spec.ManifestTools{}) {
			d.Tools = nil
		}
	}}
}
func (m *model) simpleRules() []rule {
	kinds := toolRule("Tools", 0)
	kinds.finite = spec.ManifestToolKinds()
	return []rule{
		{"Languages", func(d spec.Manifest) *spec.ManifestAllow { return d.Languages }, func(d *spec.Manifest, a *spec.ManifestAllow) { d.Languages = a }, nil},
		{"Targets", func(d spec.Manifest) *spec.ManifestAllow { return d.Targets }, func(d *spec.Manifest, a *spec.ManifestAllow) { d.Targets = a }, providers()},
		kinds,
		{"Tracing", func(d spec.Manifest) *spec.ManifestAllow { return d.Tracing }, func(d *spec.Manifest, a *spec.ManifestAllow) { d.Tracing = a }, spec.ManifestTracingProviders()},
		toolRule("Tool names", 1), toolRule("Builtin names", 2), toolRule("SLNG tool names", 3),
	}
}
func (m *model) editRule(r rule) {
	m.editAllow(r.title, "value", "", allowValues(r.get(m.draft)), r.finite, true, func(values []string) error {
		var allowed *spec.ManifestAllow
		if values != nil {
			allowed = &spec.ManifestAllow{Allow: values}
		}
		trial := m.draft
		r.set(&trial, allowed)
		if err := validateDraft(trial); err != nil {
			return err
		}
		m.draft = trial
		return nil
	})
}

// Completed list edits reach the draft immediately; only an open input is temporary.
func (m *model) editAllow(title, noun, help string, initial, finite []string, unrestricted bool, commit func([]string) error) {
	chooseValue := func(title, current string, commit func(string) error) {
		m.chooseValue(title, current, finite, commit)
		if f := m.current().form; f != nil {
			f.fields[0].label = strings.ToUpper(noun[:1]) + noun[1:]
			f.help = strings.TrimSpace(help + "\n\n" + formHelp)
			m.current().help = help
		}
	}
	models := noun == "model ID"
	values := slices.Clone(initial)
	selected := len(values) > 0 || models
	p := &page{title: title, help: help, controls: 1}
	save := func(next []string) error {
		if models && next != nil && len(next) == 0 {
			return fmt.Errorf("keep at least one model; remove this provider or choose No restriction on the role instead")
		}
		if err := commit(next); err != nil {
			return err
		}
		values = slices.Clone(next)
		selected = len(values) > 0 || models
		return nil
	}
	add := func() {
		chooseValue("Add "+noun, "", func(value string) error {
			if slices.Contains(values, value) {
				return fmt.Errorf("this value is already in the list")
			}
			if err := save(append(slices.Clone(values), value)); err != nil {
				return err
			}
			rows, _ := p.build()
			p.cursor = len(rows) - 2 // Keep Add focused for the next value.
			return nil
		})
	}
	p.build = func() ([]item, string) {
		var rows []item
		p.sections = nil
		if models {
			p.sections = append(p.sections, menuSection{start: 0, title: "RULE"})
			rows = append(rows, item{mark(values == nil, "Allow all models"), func() {
				change := func() {
					if err := save(nil); err != nil {
						m.err = err.Error()
					}
				}
				if len(values) > 0 {
					m.confirm("Allow all models?", "This removes the model ID restriction for this provider, including future models.", change)
				} else {
					change()
				}
			}})
		}
		if !models {
			setMode := func(next []string) {
				change := func() {
					if err := save(next); err != nil {
						m.err = err.Error()
					}
				}
				if len(values) > 0 {
					text := "This replaces the selected list with Allow nothing."
					if next == nil {
						text = "This removes the restriction and allows any value."
					}
					m.confirm("Change rule?", text, change)
				} else {
					change()
				}
			}
			p.sections = append(p.sections, menuSection{start: 0, title: "RULE"})
			if unrestricted {
				rows = append(rows, item{mark(values == nil && !selected, "No restriction"), func() { setMode(nil) }})
			}
			rows = append(rows,
				item{mark(selected, "Allow selected values"), func() { selected = true }},
				item{mark(values != nil && len(values) == 0 && !selected, "Allow nothing"), func() { setMode([]string{}) }})
		}
		if selected {
			group := "ALLOWED VALUES"
			if models {
				group = "ALLOWED MODELS"
			}
			if len(values) > 0 {
				p.sections = append(p.sections, menuSection{start: len(rows), title: fmt.Sprintf("%s (%d)", group, len(values)), numbered: len(values)})
			}
			for i, value := range values {
				rows = append(rows, item{value, func() {
					m.push(&page{title: value, controls: 1, build: func() ([]item, string) {
						return []item{
							{"Edit " + noun, func() {
								chooseValue("Edit "+noun, values[i], func(value string) error {
									if j := slices.Index(values, value); j >= 0 && j != i {
										return fmt.Errorf("this value is already in the list")
									}
									next := slices.Clone(values)
									next[i] = value
									if err := save(next); err != nil {
										return err
									}
									m.back() // The picker then closes the entry menu too.
									return nil
								})
							}},
							{"Remove " + noun, func() {
								if models && len(values) == 1 {
									m.err = "Keep at least one model; remove this provider or choose No restriction on the role instead."
									return
								}
								text := "This removes " + values[i] + " from the allowed values."
								if len(values) == 1 {
									text += " The rule will Allow nothing."
								}
								m.confirm("Remove "+noun+"?", text, func() {
									next := slices.Delete(slices.Clone(values), i, i+1)
									if err := save(next); err != nil {
										m.err = err.Error()
										return
									}
									m.back()
								})
							}},
							{"Back", m.back},
						}, "Completed edits stay in your draft."
					}})
				}})
			}
		}
		p.sections = append(p.sections, menuSection{start: len(rows), title: "ACTIONS"})
		rows = append(rows, item{"Add " + noun, add}, item{"Back", m.back})
		description := "Changes stay in your draft. Select a value to edit or remove it. F1: help."
		if models {
			description = "Changes stay in your draft. Select a model to edit or remove it. F1: help."
			if len(values) == 0 {
				description = "Allow all models from this provider, or add a model ID to allow only selected models."
			}
		}
		return rows, description
	}
	if models && len(values) > 0 {
		p.cursor = 1
	}
	if selected && len(values) > 0 && !models {
		p.cursor = 2
		if unrestricted {
			p.cursor++
		}
	}
	m.push(p)
}

func mark(selected bool, label string) string {
	if selected {
		return "[x] " + label
	}
	return "[ ] " + label
}
func (m *model) chooseValue(title, current string, finite []string, commit func(string) error) {
	if len(finite) == 0 {
		m.openForm(title, []string{"Value"}, []string{current}, []func(string) error{validateText}, func(values []string) error { return commit(values[0]) })
		return
	}
	m.push(&page{title: title, controls: 1, build: func() ([]item, string) {
		var rows []item
		for _, value := range finite {
			rows = append(rows, item{value, func() {
				if err := commit(value); err != nil {
					m.err = err.Error()
					return
				}
				m.back()
			}})
		}
		rows = append(rows, item{"Back", m.back})
		return rows, "Choose an allowed value."
	}})
}
func (m *model) chooseProvider(title, current, role string, commit func(string) error, after ...func()) {
	advance := func() {
		for _, next := range after {
			next()
		}
	}
	m.push(&page{title: title, controls: 1, build: func() ([]item, string) {
		var rows []item
		for _, value := range modelProviders(role) {
			label := value
			if value == "slng" {
				label += " · gateway for multiple model makers"
				if role == "think" {
					label = "slng · context router"
				}
			}
			rows = append(rows, item{label, func() {
				if err := commit(value); err != nil {
					m.err = err.Error()
					return
				}
				m.back()
				advance()
			}})
		}
		rows = append(rows, item{"Enter another provider", func() {
			m.openForm("Custom provider", []string{"Provider"}, []string{current}, []func(string) error{validateText}, func(values []string) error {
				if err := commit(values[0]); err != nil {
					return err
				}
				m.back()
				return nil
			})
			m.current().form.after = advance
		}}, item{"Back", m.back})
		return rows, "Choose the service you connect to, such as SLNG or Deepgram. SLNG can serve models from several makers. Choose the service here; enter its model IDs next. ↑/↓ selects a row; Enter opens it. F1 shows help."
	}})
}
func roleRows(models *spec.ManifestModels, role string) *[]spec.ManifestModel {
	switch role {
	case "listen":
		return &models.Listen
	case "speak":
		return &models.Speak
	default:
		return &models.Think
	}
}
func (m *model) models() {
	m.push(&page{title: "Models", controls: 1, build: func() ([]item, string) {
		var rows []item
		for _, role := range []string{"listen", "think", "speak"} {
			entries := *roleRows(&m.draft.Models, role)
			label := "No restriction"
			if entries != nil {
				label = "Allow nothing"
				if len(entries) > 0 {
					label = fmt.Sprintf("%d providers", len(entries))
				}
			}
			rows = append(rows, item{role + " · " + label, func() { m.editModels(role) }})
		}
		return append(rows, item{"Back", m.back}), "listen = speech recognition (STT); speak = speech generation (TTS); think = language models. Each role can allow several providers, with several models per provider."
	}})
}
func (m *model) editModels(role string) {
	entries := slices.Clone(*roleRows(&m.draft.Models, role))
	p := &page{title: role, controls: 1}
	save := func(next []spec.ManifestModel) error {
		trial := m.draft
		*roleRows(&trial.Models, role) = next
		if err := validateDraft(trial); err != nil {
			return err
		}
		m.draft = trial
		entries = slices.Clone(next)
		return nil
	}
	edit := func(index int) {
		entry := spec.ManifestModel{}
		if index >= 0 {
			entry = entries[index]
		}
		remove := func() {
			if index >= 0 {
				next := slices.Delete(slices.Clone(entries), index, index+1)
				if len(next) == 0 {
					next = nil
				}
				if err := save(next); err != nil {
					m.err = err.Error()
				}
			}
		}
		m.editModelEntry(role, entry, func(row spec.ManifestModel) error {
			for j, e := range entries {
				if j != index && e.Provider == row.Provider {
					return fmt.Errorf("provider already exists")
				}
			}
			next := slices.Clone(entries)
			if index < 0 {
				next = append(next, row)
			} else {
				next[index] = row
			}
			if err := save(next); err != nil {
				return err
			}
			if index < 0 {
				index = len(next) - 1
			}
			return nil
		}, remove)
	}
	p.build = func() ([]item, string) {
		rows := []item{
			{mark(entries == nil, "No restriction"), func() {
				if entries == nil {
					return
				}
				m.confirm("Remove model restriction?", "Any provider and model will be allowed for "+role+".", func() {
					if err := save(nil); err != nil {
						m.err = err.Error()
					}
				})
			}},
			{mark(len(entries) > 0, "Allow selected values"), func() { edit(-1) }},
		}
		p.sections = []menuSection{{start: 0, title: "RULE"}}
		if len(entries) > 0 {
			p.sections = append(p.sections, menuSection{start: len(rows), title: fmt.Sprintf("ALLOWED PROVIDERS (%d)", len(entries)), numbered: len(entries)})
		}
		for i, entry := range entries {
			label := fmt.Sprintf("%s · %d models", entry.Provider, len(entry.Allow))
			if entry.Allow == nil {
				label = entry.Provider + " · All models"
			} else if len(entry.Allow) == 0 {
				label += " · needs repair"
			}
			rows = append(rows, item{label, func() { edit(i) }})
		}
		p.sections = append(p.sections, menuSection{start: len(rows), title: "ACTIONS"})
		rows = append(rows, item{"Add provider", func() { edit(-1) }}, item{"Back", m.back})
		description := "Allow several services, with several model IDs per service. Changes stay in your draft."
		if entries != nil && len(entries) == 0 {
			description = "This saved rule blocks every model. Add a provider and model, or choose No restriction, before saving."
		}
		return rows, description
	}
	m.push(p)
}

func modelIDHelp(provider, role string) string {
	if provider == "slng" && (role == "listen" || role == "speak") {
		example := "deepgram/nova:3"
		if role == "speak" {
			example = "cartesia/sonic:3.5"
		}
		return "Keep slng as the provider for models served through SLNG. Enter the full SLNG model ID, including its maker, for example " + example + ". Different model makers can share this list."
	}
	return "Enter the exact model ID accepted by " + provider + ". Add each allowed model separately; this list can contain several models."
}

func (m *model) editModelEntry(role string, entry spec.ManifestModel, commit func(spec.ManifestModel) error, remove func()) {
	entry.Allow = slices.Clone(entry.Allow)
	attached := entry.Provider != ""
	modelsChosen := attached
	p := &page{title: "Provider models", controls: 1}
	p.pending = func() bool { return !attached && entry.Provider != "" }
	save := func(next spec.ManifestModel) error {
		if next.Provider != "" && modelsChosen && (next.Allow == nil || len(next.Allow) > 0) {
			if err := commit(next); err != nil {
				return err
			}
			attached = true
		}
		entry = next
		return nil
	}
	models := func() {
		initial := entry.Allow
		if !modelsChosen {
			initial = []string{}
		}
		m.editAllow("Model IDs · "+entry.Provider, "model ID", modelIDHelp(entry.Provider, role), initial, nil, false, func(values []string) error {
			next := entry
			next.Allow = values
			previous := modelsChosen
			modelsChosen = true
			if err := save(next); err != nil {
				modelsChosen = previous
				return err
			}
			return nil
		})
	}
	provider := func(advance bool) {
		m.chooseProvider("Provider", entry.Provider, role, func(value string) error {
			next := entry
			next.Provider = value
			return save(next)
		}, func() {
			if advance {
				m.push(p)
				models()
			}
		})
	}
	p.build = func() ([]item, string) {
		label := fmt.Sprintf("Models · %d model IDs", len(entry.Allow))
		if modelsChosen && entry.Allow == nil {
			label = "Models · All models"
		}
		rows := []item{
			{"Provider · " + entry.Provider, func() { provider(false) }},
			{label, models},
		}
		if remove != nil && attached {
			rows = append(rows, item{"Remove provider", func() {
				m.confirm("Remove provider?", "This removes this provider's rule. If it is the last provider, the role becomes unrestricted.", func() { remove(); m.back() })
			}})
		}
		return append(rows, item{"Back", m.back}), "Provider is the service you connect to; model IDs are the models it serves. Changes stay in your draft."
	}
	if attached {
		m.push(p)
	} else {
		provider(true)
	}
}

func (m *model) advanced() {
	m.push(&page{title: "Advanced", controls: 1, build: func() ([]item, string) {
		rows := []item{{"Model regions", func() { m.regions(true) }}, {"Deployment regions", func() { m.regions(false) }}}
		for _, r := range m.simpleRules()[4:] {
			rows = append(rows, item{r.title + " · " + allowLabel(allowValues(r.get(m.draft))), func() { m.editRule(r) }})
		}
		return append(rows, item{"Back", m.back}), "Optional region and tool-name restrictions."
	}})
}

// Both region surfaces use the same local entry form. The typed Go lists remain
// the source of truth; conversion happens only at this UI boundary.
type regionEntry struct {
	role, provider string
	allow          []string
}

func (m *model) regions(models bool) {
	title := "Deployment regions"
	if models {
		title = "Model regions"
	}
	var entries []regionEntry
	if r := m.draft.Regions; r != nil {
		if models {
			for _, row := range r.Models {
				entries = append(entries, regionEntry{row.Role, row.Provider, row.Allow})
			}
		} else {
			for _, row := range r.Deployments {
				entries = append(entries, regionEntry{"", row.Provider, row.Allow})
			}
		}
	}
	save := func(next []regionEntry) error {
		r := spec.ManifestRegions{}
		if m.draft.Regions != nil {
			r = *m.draft.Regions
		}
		if models {
			r.Models = nil
			for _, e := range next {
				r.Models = append(r.Models, spec.ManifestModelRegion{Role: e.role, Provider: e.provider, Allow: e.allow})
			}
		} else {
			r.Deployments = nil
			for _, e := range next {
				r.Deployments = append(r.Deployments, spec.ManifestDeploymentRegion{Provider: e.provider, Allow: e.allow})
			}
		}
		trial := m.draft
		trial.Regions = &r
		if len(r.Models) == 0 && len(r.Deployments) == 0 {
			trial.Regions = nil
		}
		if err := validateDraft(trial); err != nil {
			return err
		}
		m.draft = trial
		entries = slices.Clone(next)
		return nil
	}
	edit := func(index int) {
		entry := regionEntry{}
		if index >= 0 {
			entry = entries[index]
		}
		remove := func() {
			if index >= 0 {
				if err := save(slices.Delete(slices.Clone(entries), index, index+1)); err != nil {
					m.err = err.Error()
				}
			}
		}
		m.regionEntry(entry, models, func(row regionEntry) error {
			for j, e := range entries {
				if j != index && e.role == row.role && e.provider == row.provider {
					return fmt.Errorf("this region rule already exists")
				}
			}
			next := slices.Clone(entries)
			if index < 0 {
				next = append(next, row)
			} else {
				next[index] = row
			}
			if err := save(next); err != nil {
				return err
			}
			if index < 0 {
				index = len(next) - 1
			}
			return nil
		}, remove)
	}
	p := &page{title: title, controls: 1}
	p.build = func() ([]item, string) {
		var rows []item
		p.sections = nil
		if len(entries) > 0 {
			p.sections = append(p.sections, menuSection{start: 0, title: fmt.Sprintf("REGION RULES (%d)", len(entries)), numbered: len(entries)})
		}
		for i, entry := range entries {
			rows = append(rows, item{strings.TrimSpace(entry.role+" "+entry.provider) + " · " + allowLabel(entry.allow), func() { edit(i) }})
		}
		p.sections = append(p.sections, menuSection{start: len(rows), title: "ACTIONS"})
		rows = append(rows, item{"Add region rule", func() { edit(-1) }}, item{"Back", m.back})
		return rows, "Unlisted providers have no region restriction. Changes stay in your draft."
	}
	m.push(p)
}
func (m *model) regionEntry(entry regionEntry, models bool, commit func(regionEntry) error, remove func()) {
	entry.allow = slices.Clone(entry.allow)
	regionsChosen := entry.allow != nil
	attached := entry.provider != ""
	save := func(next regionEntry, chosen bool) error {
		if next.provider != "" && (!models || next.role != "") && chosen {
			if err := commit(next); err != nil {
				return err
			}
			attached = true
		}
		entry, regionsChosen = next, chosen
		return nil
	}
	p := &page{title: "Region rule", controls: 1}
	p.pending = func() bool { return !attached && (entry.role != "" || entry.provider != "" || regionsChosen) }
	p.build = func() ([]item, string) {
		var rows []item
		if models {
			rows = append(rows, item{"Role · " + entry.role, func() {
				m.chooseValue("Role", entry.role, []string{"listen", "think", "speak"}, func(value string) error {
					next := entry
					next.role = value
					return save(next, regionsChosen)
				})
			}})
		}
		rows = append(rows, item{"Provider · " + entry.provider, func() {
			commit := func(value string) error {
				next := entry
				next.provider = value
				return save(next, regionsChosen)
			}
			if models {
				m.chooseProvider("Provider", entry.provider, entry.role, commit)
			} else {
				m.chooseValue("Provider", entry.provider, providers(), commit)
			}
		}}, item{"Regions · " + allowLabel(entry.allow), func() {
			m.editAllow("Allowed regions", "value", "Enter each allowed region separately.", entry.allow, nil, false, func(values []string) error {
				next := entry
				next.allow = values
				return save(next, true)
			})
		}})
		if remove != nil && attached {
			rows = append(rows, item{"Remove rule", func() {
				m.confirm("Remove region restriction?", "This provider will have no restriction from this region rule.", func() { remove(); m.back() })
			}})
		}
		description := "Changes stay in your draft."
		if entry.provider == "" || (models && entry.role == "") || !regionsChosen {
			description = "Complete the provider, role (for model regions), and regions to keep this entry."
		}
		return append(rows, item{"Back", m.back}), description
	}
	m.push(p)
}

// Old YAML can express blocking model policies. Keep it readable without silently
// widening it, but require an explicit repair before the guided editor saves.
func validateGuidedModels(draft spec.Manifest) error {
	for _, role := range []string{"listen", "think", "speak"} {
		entries := *roleRows(&draft.Models, role)
		if entries != nil && len(entries) == 0 {
			return fmt.Errorf("models / %s blocks every model; add a provider and model or choose No restriction", role)
		}
		for _, entry := range entries {
			if entry.Allow != nil && len(entry.Allow) == 0 {
				return fmt.Errorf("models / %s / %s has no allowed models; add a model or remove the provider rule", role, entry.Provider)
			}
		}
	}
	return nil
}

func (m *model) review() {
	p := &page{title: "Review and save", review: true, controls: 2}
	if !m.options.Editing && m.options.HasDefault {
		p.sections = []menuSection{{start: 0, title: "OPTIONS"}}
		p.cursor = 1 // Start on Back, rather than toggling the default or saving.
	}
	p.build = func() ([]item, string) {
		var rows []item
		if !m.options.Editing && m.options.HasDefault {
			rows = append(rows, item{mark(m.makeDefault, "Use as default for new agents"), func() { m.makeDefault = !m.makeDefault }})
		}
		rows = append(rows, item{"Back to editing", m.back}, item{"Save manifest", func() {
			if err := m.validateNameForSave(); err != nil {
				m.err = err.Error()
				return
			}
			if err := validateDraft(m.draft); err != nil {
				m.err = err.Error()
				return
			}
			if err := validateGuidedModels(m.draft); err != nil {
				m.err = err.Error()
				return
			}
			if !m.dirty() {
				m.done = true
				return
			}
			if err := m.options.Save(m.options.Name, m.draft, m.makeDefault); err != nil {
				m.err = err.Error()
				return
			}
			m.done = true
		}})
		description := m.summary()
		if err := validateGuidedModels(m.draft); err != nil {
			description += "\n\nNeeds repair: " + err.Error()
		}
		if m.options.Destination != nil {
			description += "\n\nSave to: " + m.options.Destination(m.options.Name)
		}
		if m.options.Editing {
			description += "\n\nSaving writes clean YAML and replaces comments and formatting. An exact backup is kept beside the file. Existing agent packages stay unchanged."
		}
		return rows, description
	}
	m.push(p)
}
func (m *model) validateNameForSave() error {
	if m.options.Editing {
		return nil
	}
	return m.validateName(m.options.Name)
}
func (m *model) summaryRows(d spec.Manifest) [][2]string {
	rows := [][2]string{{"Company", d.Name}, {"Revision", strconv.Itoa(d.Version)}}
	for _, role := range []string{"listen", "think", "speak"} {
		entries := *roleRows(&d.Models, role)
		value := "No restriction"
		if entries != nil {
			value = "Allow nothing"
			if len(entries) > 0 {
				var parts []string
				for _, entry := range entries {
					label := allowLabel(entry.Allow)
					if entry.Allow == nil {
						label = "All models"
					}
					parts = append(parts, entry.Provider+": "+label)
				}
				value = strings.Join(parts, "; ")
			}
		}
		rows = append(rows, [2]string{"Models / " + role, value})
	}
	for _, r := range m.simpleRules() {
		rows = append(rows, [2]string{r.title, allowLabel(allowValues(r.get(d)))})
	}
	var modelRegions, deploymentRegions []string
	if d.Regions != nil {
		for _, r := range d.Regions.Models {
			modelRegions = append(modelRegions, r.Role+" / "+r.Provider+": "+allowLabel(r.Allow))
		}
		for _, r := range d.Regions.Deployments {
			deploymentRegions = append(deploymentRegions, r.Provider+": "+allowLabel(r.Allow))
		}
	}
	for _, group := range []struct {
		label  string
		values []string
	}{{"Model regions", modelRegions}, {"Deployment regions", deploymentRegions}} {
		value := "No restriction"
		if len(group.values) > 0 {
			value = strings.Join(group.values, "; ")
		}
		rows = append(rows, [2]string{group.label, value})
	}
	return rows
}
func (m *model) summary() string {
	current := m.summaryRows(m.draft)
	var lines []string
	if m.options.Editing && m.options.Rules != nil {
		if reflect.DeepEqual(m.options.Rules, &m.draft) {
			lines = append(lines, "No changes. The saved file will be kept as it is.")
		} else {
			lines = append(lines, "Changes from saved rules:")
			previous := m.summaryRows(*m.options.Rules)
			for i, row := range current {
				if row[1] != previous[i][1] {
					suffix := ""
					if row[1] == "No restriction" {
						suffix = " (restriction removed)"
					}
					lines = append(lines, row[0]+": "+previous[i][1]+" → "+row[1]+suffix)
				}
			}
		}
		lines = append(lines, "", "Resulting rules:")
	}
	for _, row := range current {
		lines = append(lines, row[0]+": "+row[1])
	}
	return strings.Join(lines, "\n")
}

// Encode writes only the rules an author chose; no commented starter is inserted.
func Encode(rules spec.Manifest) ([]byte, error) {
	data, err := yaml.Marshal(rules)
	if err != nil {
		return nil, err
	}
	if _, err := spec.ParseManifest(data); err != nil {
		return nil, err
	}
	return data, nil
}
