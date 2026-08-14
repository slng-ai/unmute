// Package tui gathers the basic configuration for a new v1 agent scaffold.
package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/slng/unmute/internal/scaffold"
	targetcap "github.com/slng/unmute/internal/target"
)

const (
	actionCreate  = "create"
	actionOpen    = "open"
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

// ActionHandler runs an existing CLI action and writes its report.
type ActionHandler func(action, path string, out io.Writer) error

// Run displays the console without writing files.
func Run(in io.Reader, out io.Writer, accessible bool) (Result, error) {
	return RunConsole(in, out, accessible, nil)
}

// RunConsole displays Home with optional in-process maintenance actions.
func RunConsole(in io.Reader, out io.Writer, accessible bool, actions ActionHandler) (Result, error) {
	return runWithStart(in, out, accessible, false, actions)
}

// RunCreate enters the create flow directly for `unmute init`.
func RunCreate(in io.Reader, out io.Writer, accessible bool) (Result, error) {
	return runWithStart(in, out, accessible, true, nil)
}

func runWithStart(in io.Reader, out io.Writer, accessible, createOnly bool, actions ActionHandler) (Result, error) {
	runner := newRunner(in, out, accessible)
	runner.actions = actions
	flow := func() (Result, error) {
		if createOnly {
			result, _, err := runCreate(runner)
			return result, err
		}
		return runHome(runner)
	}
	if accessible {
		return flow()
	}
	return runner.runProgram(flow)
}

func runHome(runner *fieldRunner) (Result, error) {
	for {
		runner.ctx = viewCtx{hero: true}
		choice, _, err := runner.selectOne(homeTitle(), "What would you like to do?", []huh.Option[string]{
			huh.NewOption("Create a new agent", actionCreate),
			huh.NewOption("Open an existing agent", actionOpen),
			huh.NewOption("Quit", actionQuit),
		}, false)
		if err != nil {
			return Result{}, err
		}
		if choice == actionQuit {
			return Result{}, nil
		}
		runner.ctx = viewCtx{}
		if choice == actionOpen {
			if err := openExisting(runner); err != nil {
				return Result{}, err
			}
			continue
		}
		result, back, err := runCreate(runner)
		if err != nil {
			return Result{}, err
		}
		if back {
			continue
		}
		return result, nil
	}
}

func runCreate(runner *fieldRunner) (Result, bool, error) {
	path := ""
	back, err := runner.input("Agent name", agentNameHelp, &path, validateName)
	if err != nil || back {
		return Result{}, back, err
	}
	data := scaffold.Data{
		Name:         filepath.Base(filepath.Clean(path)),
		Channel:      scaffold.DefaultChannel,
		Greeting:     scaffold.DefaultGreeting,
		Instructions: scaffold.DefaultInstructions,
		Tools:        scaffold.DefaultTools(), // seeded, editable/removable in the Tools section
	}
	data.SetTarget(scaffold.DefaultTarget)
	return editAgent(runner, Agent{Path: path, Data: data})
}

// homeTitle is the Home select title used by the accessible renderer; the
// interactive renderer draws the wordmark hero instead (view.go).
func homeTitle() string { return "SLNG//" }

// sidebarSections is the fixed five-section tree shown in the console sidebar
// (docs/spec/tui.md C10, C16, V44). Order matches editorSectionOptions.
var sidebarSections = []struct{ id, label string }{
	{"identity", "Identity"},
	{"models", "Models"},
	{"behavior", "Behavior"},
	{"integrations", "Integrations"},
	{"lifecycle", "Lifecycle"},
}

func sectionChildren(id string) []string {
	switch id {
	case "identity":
		return []string{"Target", "Language"}
	case "models":
		return []string{"Listen", "Reason", "Speak"}
	case "behavior":
		return []string{"Instructions", "Greeting", "Variables", "Advanced"}
	case "integrations":
		return []string{"Tools", "Channels", "Human transfers"}
	case "lifecycle":
		return []string{"Agents", "Handoffs", "Tasks", "Task groups"}
	}
	return nil
}

// sectionOf maps an editor choice to its owning sidebar section.
func sectionOf(choice string) string {
	if section, ok := strings.CutPrefix(choice, "section:"); ok {
		return section
	}
	switch choice {
	case "target", "language":
		return "identity"
	case "models":
		return "models"
	case "prompt", "greeting", "variables", "customize":
		return "behavior"
	case "tools", "channels", "humans":
		return "integrations"
	case "agents", "handoffs", "tasks", "groups":
		return "lifecycle"
	}
	return ""
}

// agentCtx builds the console chrome for the create/maintain editor: breadcrumb,
// target status, and the sidebar tree with the active section expanded.
func agentCtx(data scaffold.Data, active string) viewCtx {
	var items []sideItem
	breadcrumb := data.Name
	for _, s := range sidebarSections {
		items = append(items, sideItem{label: s.label, active: s.id == active})
		if s.id == active {
			breadcrumb = data.Name + " › " + s.label
			for _, child := range sectionChildren(s.id) {
				items = append(items, sideItem{label: child, child: true})
			}
		}
	}
	return viewCtx{breadcrumb: breadcrumb, target: targetLabel(data.Target), sidebar: items}
}

func editAgent(runner *fieldRunner, agent Agent) (Result, bool, error) {
	result := Result{Agent: agent, Create: true}
	for {
		compile := "off"
		if result.Compile {
			compile = "on"
		}
		options := editorSectionOptions(result.Agent.Data)
		options = append(options,
			huh.NewOption("Compile after create  ·  "+compile, "compile"),
			huh.NewOption("Create agent", "save"),
			huh.NewOption("← Back", actionBack),
		)
		runner.ctx = agentCtx(result.Agent.Data, "")
		choice, back, err := runner.selectOne(result.Agent.Data.Name, "Choose a section; changes stay in memory until Create agent.", options, true)
		if err != nil {
			return Result{}, false, err
		}
		if back || choice == actionBack {
			return Result{}, true, nil
		}
		runner.ctx = agentCtx(result.Agent.Data, sectionOf(choice))
		if strings.HasPrefix(choice, "section:") {
			section := strings.TrimPrefix(choice, "section:")
			if section == "models" {
				if err := editModels(runner, &result.Agent.Data); err != nil {
					return Result{}, false, err
				}
				continue
			}
			choice, err = chooseEditorSection(runner, &result.Agent.Data, section)
			if err != nil {
				return Result{}, false, err
			}
			if choice == actionBack {
				continue
			}
		}
		switch choice {
		case "target":
			selected, back, err := runner.selectOne("Target / orchestrator", runner.describe("Vapi and Deepgram are unavailable: their generators are not implemented yet."), []huh.Option[string]{
				huh.NewOption("Pipecat  ·  generated code project", string(targetcap.Pipecat)),
				huh.NewOption("LiveKit  ·  generated code project", string(targetcap.LiveKit)),
				huh.NewOption("← Back", actionBack),
			}, true)
			if err != nil {
				return Result{}, false, err
			}
			if !back && selected != actionBack {
				result.Agent.Data.SetTarget(selected)
				dropUnsupportedBuiltins(&result.Agent.Data)
				for i := range result.Agent.Data.Agents {
					result.Agent.Data.Agents[i].Reason = result.Agent.Data.Reason
					result.Agent.Data.Agents[i].Speak = result.Agent.Data.Speak
				}
				for i := range result.Agent.Data.Fallbacks {
					result.Agent.Data.Fallbacks[i].Binding = result.Agent.Data.Reason
				}
			}
		case "models":
			if err := editModels(runner, &result.Agent.Data); err != nil {
				return Result{}, false, err
			}
		case "prompt":
			if _, err := runner.text("Instructions", "Blank keeps the generated default.", &result.Agent.Data.Instructions); err != nil {
				return Result{}, false, err
			}
		case "greeting":
			if _, err := runner.input("Greeting", "", &result.Agent.Data.Greeting, validateBasic); err != nil {
				return Result{}, false, err
			}
		case "variables":
			if err := editVariables(runner, &result.Agent.Data); err != nil {
				return Result{}, false, err
			}
		case "tools":
			if err := editTools(runner, &result.Agent.Data); err != nil {
				return Result{}, false, err
			}
		case "agents":
			if err := editAgents(runner, &result.Agent.Data); err != nil {
				return Result{}, false, err
			}
		case "handoffs":
			if err := editHandoffs(runner, &result.Agent.Data); err != nil {
				return Result{}, false, err
			}
		case "tasks":
			if err := editTasks(runner, &result.Agent.Data); err != nil {
				return Result{}, false, err
			}
		case "groups":
			if err := editTaskGroups(runner, &result.Agent.Data); err != nil {
				return Result{}, false, err
			}
		case "channels":
			if err := editChannels(runner, &result.Agent.Data); err != nil {
				return Result{}, false, err
			}
		case "humans":
			if err := editHumanTransfers(runner, &result.Agent.Data); err != nil {
				return Result{}, false, err
			}
		case "customize":
			if err := editCustomize(runner, &result.Agent.Data); err != nil {
				return Result{}, false, err
			}
		case "compile":
			result.Compile = !result.Compile
		case "save":
			review, err := scaffold.Preflight(result.Agent.Data)
			if err != nil {
				if err := repairPreflight(runner, &result.Agent.Data, err); err != nil {
					return Result{}, false, err
				}
				continue
			}
			confirmed, err := confirmChoice(runner, summary(result, review), "Create agent")
			if err != nil {
				return Result{}, false, err
			}
			if confirmed {
				result.Confirmed = true
				return result, false, nil
			}
		}
	}
}

func editorSectionOptions(data scaffold.Data) []huh.Option[string] {
	return []huh.Option[string]{
		huh.NewOption("Identity  ·  target", "section:identity"),
		huh.NewOption("Models  ·  "+modelsLabel(data), "section:models"),
		huh.NewOption("Behavior  ·  instructions, greeting, variables, advanced", "section:behavior"),
		huh.NewOption("Integrations  ·  tools, channels, human transfers", "section:integrations"),
		huh.NewOption("Lifecycle  ·  agents, handoffs, tasks, groups", "section:lifecycle"),
	}
}

func chooseEditorSection(runner *fieldRunner, data *scaffold.Data, section string) (string, error) {
	var options []huh.Option[string]
	switch section {
	case "identity":
		options = []huh.Option[string]{
			huh.NewOption("Target  ·  "+targetLabel(data.Target), "target"),
		}
	case "behavior":
		options = []huh.Option[string]{
			huh.NewOption("Instructions (prompt)", "prompt"),
			huh.NewOption("Greeting  ·  "+data.Greeting, "greeting"),
			huh.NewOption(fmt.Sprintf("Variables  ·  %d", len(data.Variables)), "variables"),
			huh.NewOption("Advanced  ·  conversation, fallback, capacity, target", "customize"),
		}
	case "integrations":
		options = []huh.Option[string]{
			huh.NewOption(fmt.Sprintf("Tools  ·  %d", len(data.Tools)), "tools"),
			huh.NewOption("Caller channels  ·  "+channelsLabel(*data), "channels"),
			huh.NewOption(fmt.Sprintf("Human transfers  ·  %d", len(data.HumanTransfers)), "humans"),
		}
	case "lifecycle":
		options = []huh.Option[string]{
			huh.NewOption(fmt.Sprintf("Agents  ·  %d", len(data.AllAgents())), "agents"),
			huh.NewOption(fmt.Sprintf("Handoffs  ·  %d", len(data.Handoffs)), "handoffs"),
			huh.NewOption(fmt.Sprintf("Tasks  ·  %d", len(data.Tasks)), "tasks"),
			huh.NewOption(fmt.Sprintf("Task groups  ·  %d", len(data.TaskGroups)), "groups"),
		}
	default:
		return "", fmt.Errorf("unknown editor section %q", section)
	}
	options = append(options, huh.NewOption("← Back", actionBack))
	choice, _, err := runner.selectOne(strings.ToUpper(section[:1])+section[1:], "", options, true)
	return choice, err
}

func repairPreflight(runner *fieldRunner, data *scaffold.Data, preflightErr error) error {
	for {
		choice, _, err := runner.selectOne("Cannot create agent", runner.describe("Fix the configuration, then go Back to continue editing.\n\n"+preflightErr.Error()), []huh.Option[string]{
			huh.NewOption(fmt.Sprintf("Review / delete model fallbacks  ·  %d", len(data.Fallbacks)), "fallbacks"),
			huh.NewOption("Models  ·  "+modelsLabel(*data), "models"),
			huh.NewOption(fmt.Sprintf("Agents  ·  %d", len(data.AllAgents())), "agents"),
			huh.NewOption(fmt.Sprintf("Handoffs  ·  %d", len(data.Handoffs)), "handoffs"),
			huh.NewOption(fmt.Sprintf("Tasks  ·  %d", len(data.Tasks)), "tasks"),
			huh.NewOption(fmt.Sprintf("Task groups  ·  %d", len(data.TaskGroups)), "groups"),
			huh.NewOption(fmt.Sprintf("Tools  ·  %d", len(data.Tools)), "tools"),
			huh.NewOption(fmt.Sprintf("Variables  ·  %d", len(data.Variables)), "variables"),
			huh.NewOption(fmt.Sprintf("Human transfers  ·  %d", len(data.HumanTransfers)), "humans"),
			huh.NewOption("← Back", actionBack),
		}, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "fallbacks":
			err = editFallbacks(runner, data)
		case "models":
			err = editModels(runner, data)
		case "agents":
			err = editAgents(runner, data)
		case "handoffs":
			err = editHandoffs(runner, data)
		case "tasks":
			err = editTasks(runner, data)
		case "groups":
			err = editTaskGroups(runner, data)
		case "tools":
			err = editTools(runner, data)
		case "variables":
			err = editVariables(runner, data)
		case "humans":
			err = editHumanTransfers(runner, data)
		}
		if err != nil {
			return err
		}
	}
}

func showNotice(runner *fieldRunner, title, message string) error {
	if !runner.accessible {
		runner.sendField(fieldReq{
			kind:     kindSelect,
			title:    title,
			desc:     message,
			choices:  []choice{{label: "← Back", value: actionBack}},
			initial:  actionBack,
			backable: true,
			ctx:      runner.ctx,
		})
		return nil
	}
	choice := actionBack
	_, err := runner.run(huh.NewSelect[string]().
		Title(title).
		Description(runner.describe(message)).
		Options(huh.NewOption("← Back", actionBack)).
		Value(&choice), true)
	return err
}

func summary(result Result, review scaffold.PreflightReport) string {
	compile := "no"
	if result.Compile {
		compile = "yes (selected target)"
	}
	var text strings.Builder
	fmt.Fprintf(&text, "Create %s?\nTarget: %s (%s)\nCaller channels: %s\nGreeting: %s\nRequired env: %s\nForwarded bindings:",
		result.Agent.Data.Name, targetLabel(result.Agent.Data.Target), review.TargetName,
		channelsLabel(result.Agent.Data), result.Agent.Data.Greeting, strings.Join(review.RequiredEnv, ", "))
	for _, binding := range review.Bindings {
		profile := ""
		if binding.Profile != "" {
			profile = "." + binding.Profile
		}
		identity := binding.Binding.Provider
		if identity == "" {
			identity = "integrated"
		}
		if binding.Binding.Model != "" {
			identity += "/" + binding.Binding.Model
		}
		if voice := firstNonempty(binding.Binding.Voice, binding.Binding.VoiceID); voice != "" {
			identity += " voice=" + voice
		}
		fmt.Fprintf(&text, "\n- %s%s: %s", binding.Role, profile, identity)
	}
	if len(review.Warnings) > 0 {
		text.WriteString("\nWarnings:")
		for _, warning := range review.Warnings {
			fmt.Fprintf(&text, "\n- %s", warning)
		}
	}
	if hasTelephony(&result.Agent.Data) {
		fmt.Fprintf(&text, "\nExternal phone setup required: transport=%s carrier=%s; provision numbers/trunks outside Unmute.",
			firstNonempty(result.Agent.Data.Transport, "provider default"), firstNonempty(result.Agent.Data.Carrier, "provider default"))
	}
	fmt.Fprintf(&text, "\nCompile: %s", compile)
	return text.String()
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func targetLabel(provider string) string {
	switch targetcap.Provider(provider) {
	case targetcap.LiveKit:
		return "LiveKit"
	default:
		return "Pipecat"
	}
}

func modelsLabel(data scaffold.Data) string {
	framework := targetcap.Provider(data.Target)
	label := func(role targetcap.Role, binding scaffold.Binding) string {
		if binding.Provider == "" {
			if binding.Model != "" {
				return "forwarded"
			}
			return "integrated"
		}
		return targetcap.DefaultCatalog().Brand(framework, role, binding.Provider, binding.Model)
	}
	labels := []string{
		label(targetcap.Listen, data.Listen),
		label(targetcap.Reason, data.Reason),
		label(targetcap.Speak, data.Speak),
	}
	unique := labels[:0]
	for _, value := range labels {
		if !slices.Contains(unique, value) {
			unique = append(unique, value)
		}
	}
	return strings.Join(unique, " / ")
}

func channelsLabel(data scaffold.Data) string {
	channels := data.AllChannels()
	web, phone := false, false
	for _, channel := range channels {
		web = web || channel.Kind == "realtime_audio"
		phone = phone || channel.Kind == "telephony"
	}
	if web && phone {
		return "web + phone"
	}
	if phone {
		return "phone"
	}
	return "web"
}

func editModels(runner *fieldRunner, data *scaffold.Data) error {
	for {
		choice, _, err := runner.selectOne("STT / LLM / TTS", runner.describe("Providers come from the selected target's catalogue. Model ids, voices, and params are forwarded as entered."), []huh.Option[string]{
			huh.NewOption("Listen (STT)  ·  "+modelsLabelPart(data.Target, targetcap.Listen, data.Listen), string(targetcap.Listen)),
			huh.NewOption("Reason (LLM)  ·  "+modelsLabelPart(data.Target, targetcap.Reason, data.Reason), string(targetcap.Reason)),
			huh.NewOption("Speak (TTS)  ·  "+modelsLabelPart(data.Target, targetcap.Speak, data.Speak), string(targetcap.Speak)),
			huh.NewOption("Reset default models", "reset"),
			huh.NewOption("← Back", actionBack),
		}, true)
		if err != nil {
			return err
		}
		if choice == actionBack {
			return nil
		}
		if choice == "reset" {
			confirmed, err := confirmAction(runner, "Reset default models?", "Reset models")
			if err != nil {
				return err
			}
			if confirmed {
				defaults := scaffold.Data{}
				defaults.SetTarget(data.Target)
				data.Listen, data.Reason, data.Speak = defaults.Listen, defaults.Reason, defaults.Speak
			}
			continue
		}
		if err := editBinding(runner, data, targetcap.Role(choice)); err != nil {
			return err
		}
	}
}

func modelsLabelPart(target string, role targetcap.Role, binding scaffold.Binding) string {
	if binding.Provider != "" {
		brand := targetcap.DefaultCatalog().Brand(targetcap.Provider(target), role, binding.Provider, binding.Model)
		if brand != binding.Provider {
			return brand + " via " + binding.Provider
		}
		return brand
	}
	return "integrated / forwarded"
}

func editBinding(runner *fieldRunner, data *scaffold.Data, role targetcap.Role) error {
	binding := bindingForRole(data, role)
	return editBindingFor(runner, data.Target, role, binding)
}

func editBindingFor(runner *fieldRunner, target string, role targetcap.Role, binding *scaffold.Binding) error {
	framework := targetcap.Provider(target)
	catalog := targetcap.DefaultCatalog()
	for {
		brand := catalog.Brand(framework, role, binding.Provider, binding.Model)
		if brand == "" {
			brand = "not set"
		}
		distributors := catalog.Distributors(framework, role, brand)
		entry, _ := catalog.Lookup(framework, role, binding.Provider)
		entryHint := catalogueEntryHint(entry)
		modelApplicable := entry.Call == nil || entry.Call.Model.Arg != ""
		voiceApplicable := role == targetcap.Speak && (entry.Call == nil || entry.Call.Voice.Arg != "")
		// Language is per-model and only STT/TTS integrations with a real slot
		// carry it (N16). Offering it on a slotless entry would author a spec
		// that validates green but fails compile, so gate it like model/voice.
		languageApplicable := (role == targetcap.Listen || role == targetcap.Speak) &&
			entry.Call != nil && entry.Call.Language.Arg != "" && !entry.Call.NoLanguage

		options := []huh.Option[string]{huh.NewOption("Provider  ·  "+brand, "provider")}
		if len(distributors) > 1 {
			options = append(options, huh.NewOption("Distributor  ·  "+firstNonempty(binding.Provider, distributors[0]), "distributor"))
		}
		if modelApplicable {
			options = append(options, huh.NewOption("Model  ·  "+firstNonempty(binding.Model, "not set"), "model"))
		}
		if voiceApplicable {
			options = append(options, huh.NewOption("Voice  ·  "+firstNonempty(binding.Voice, "not set"), "voice"))
		}
		if languageApplicable {
			options = append(options, huh.NewOption("Language  ·  "+firstNonempty(binding.Language, "provider default"), "language"))
		}
		options = append(options,
			huh.NewOption("Additional config  ·  "+firstNonempty(binding.Params, "none"), "params"),
			huh.NewOption("← Back", actionBack),
		)
		choice, _, err := runner.selectOne(strings.ToUpper(string(role)[:1])+string(role)[1:], runner.describe(entryHint), options, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "provider":
			providerChoices := providerOptions(framework, role)
			if len(providerChoices) == 0 {
				if _, err := runner.input("Provider (optional)", "This role has no fixed provider catalogue; the identity is forwarded.", &binding.Provider, validateBasic); err != nil {
					return err
				}
				continue
			}
			providerChoices = append(providerChoices, huh.NewOption("← Back", actionBack))
			selected, back, err := runner.selectOne(string(role)+" provider", runner.describe("Choose the provider brand. Model and voice identities are forwarded without an allowlist."), providerChoices, true)
			if err != nil {
				return err
			}
			if !back {
				routes := catalog.Distributors(framework, role, selected)
				if len(routes) > 0 {
					binding.Provider = routes[0]
				}
			}
		case "distributor":
			routeOptions := make([]huh.Option[string], 0, len(distributors)+1)
			for _, route := range distributors {
				routeOptions = append(routeOptions, huh.NewOption(route, route))
			}
			routeOptions = append(routeOptions, huh.NewOption("← Back", actionBack))
			selected, back, err := runner.selectOne("Distributor for "+brand, runner.describe("Choose the integration that will deliver this provider."), routeOptions, true)
			if err != nil {
				return err
			}
			if !back {
				binding.Provider = selected
			}
		case "model":
			description := "Forwarded model id."
			validate := validateBasic
			if entry.ModelRequired() || role == targetcap.Reason || role == targetcap.Listen {
				description = "Required by this provider integration; forwarded without an allowlist."
				validate = validateRequiredBasic
			}
			if _, err := runner.input("Model", description+" "+entryHint, &binding.Model, validate); err != nil {
				return err
			}
		case "voice":
			description := "Optional voice name or id; forwarded without an allowlist."
			validate := validateBasic
			if entry.VoiceRequired() {
				description = "Required by this provider integration; enter a voice name or id."
				validate = validateRequiredBasic
			}
			if _, err := runner.input("Voice", description+" "+entryHint, &binding.Voice, validate); err != nil {
				return err
			}
		case "language":
			if _, err := runner.input("Language", "Per-model BCP-47 tag, for example en or es-MX. Blank uses the provider default. "+entryHint, &binding.Language, validateLanguage); err != nil {
				return err
			}
		case "params":
			if _, err := runner.input("Additional config (optional JSON object)", "Provider-specific request knobs, for example {\"temperature\":0.2}. "+entryHint, &binding.Params, validateParams); err != nil {
				return err
			}
		}
	}
}

func catalogueEntryHint(entry targetcap.Entry) string {
	required := func(value bool) string {
		if value {
			return "required"
		}
		return "optional"
	}
	if entry.Call == nil {
		return fmt.Sprintf("Catalogue arity: model %s; voice %s; language target-managed.",
			required(entry.ModelRequired()), required(entry.VoiceRequired()))
	}
	slot := func(name string, field targetcap.FieldSpec) string {
		if field.Arg == "" {
			return name + " unavailable"
		}
		return fmt.Sprintf("%s → %s (%s)", name, field.Arg, required(field.Required))
	}
	return "Catalogue arity: " + strings.Join([]string{
		slot("model", entry.Call.Model),
		slot("voice", entry.Call.Voice),
		slot("language", entry.Call.Language),
	}, "; ") + "."
}

func bindingForRole(data *scaffold.Data, role targetcap.Role) *scaffold.Binding {
	switch role {
	case targetcap.Listen:
		return &data.Listen
	case targetcap.Speak:
		return &data.Speak
	default:
		return &data.Reason
	}
}

func providerOptions(framework targetcap.Provider, role targetcap.Role) []huh.Option[string] {
	brands := targetcap.DefaultCatalog().Brands(framework, role)
	options := make([]huh.Option[string], 0, len(brands))
	for _, brand := range brands {
		options = append(options, huh.NewOption(brand, brand))
	}
	return options
}

func editVariables(runner *fieldRunner, data *scaffold.Data) error {
	for {
		options := make([]huh.Option[string], 0, len(data.Variables)+2)
		for _, variable := range data.Variables {
			options = append(options, huh.NewOption(variableLabel(variable), "edit:"+variable.Name))
		}
		options = append(options, huh.NewOption("Add variable", "add"), huh.NewOption("← Back", actionBack))
		choice, _, err := runner.selectOne("Variables", "", options, true)
		if err != nil || choice == actionBack {
			return err
		}
		if strings.HasPrefix(choice, "edit:") {
			if err := editVariable(runner, data, strings.TrimPrefix(choice, "edit:")); err != nil {
				return err
			}
			continue
		}
		variable := scaffold.Variable{Type: "string"}
		back, err := runner.input("Variable name", "Lowercase snake_case.", &variable.Name, func(value string) error {
			if err := validateIdentifier(value); err != nil {
				return err
			}
			for _, existing := range data.Variables {
				if existing.Name == value {
					return errors.New("variable already exists")
				}
			}
			return nil
		})
		if err != nil || back {
			continue
		}
		data.Variables = append(data.Variables, variable)
		if err := editVariable(runner, data, variable.Name); err != nil {
			return err
		}
	}
}

func variableLabel(variable scaffold.Variable) string {
	source := firstNonempty(variable.Source, "session state")
	return fmt.Sprintf("%s  ·  %s  ·  %s", variable.Name, variable.Type, strings.ReplaceAll(source, "_", " "))
}

func editVariable(runner *fieldRunner, data *scaffold.Data, name string) error {
	for {
		var variable *scaffold.Variable
		for i := range data.Variables {
			if data.Variables[i].Name == name {
				variable = &data.Variables[i]
				break
			}
		}
		if variable == nil {
			return nil
		}
		source := firstNonempty(variable.Source, "session state")
		choice, _, err := runner.selectOne(variable.Name, "Edit this saved variable. Its name stays stable so existing references do not break.", []huh.Option[string]{
			huh.NewOption("Type  ·  "+variable.Type, "type"),
			huh.NewOption("Default  ·  "+firstNonempty(variable.Default, "none"), "default"),
			huh.NewOption("Source  ·  "+strings.ReplaceAll(source, "_", " "), "source"),
			huh.NewOption("Delete variable", "delete"),
			huh.NewOption("← Back", actionBack),
		}, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "type":
			selected, back, err := runner.selectOne("Variable type", "", []huh.Option[string]{
				huh.NewOption("string", "string"), huh.NewOption("number", "number"),
				huh.NewOption("boolean", "boolean"), huh.NewOption("integer", "integer"),
				huh.NewOption("← Back", actionBack),
			}, true)
			if err != nil {
				return err
			}
			if !back && selected != actionBack {
				if err := validateVariableDefault(selected, variable.Default); err != nil {
					if err := showNotice(runner, "Type cannot change", "Keep the current type until its default is compatible: "+err.Error()); err != nil {
						return err
					}
				} else {
					variable.Type = selected
				}
			}
		case "default":
			if _, err := runner.input("Default (optional JSON value)", `Examples: "guest", false, 42. Blank means no default.`, &variable.Default, func(value string) error {
				return validateVariableDefault(variable.Type, value)
			}); err != nil {
				return err
			}
		case "source":
			selected, back, err := runner.selectOne("Value source", "", []huh.Option[string]{
				huh.NewOption("No external source", "none"),
				huh.NewOption("Required at call start", "call_start"),
				huh.NewOption("← Back", actionBack),
			}, true)
			if err != nil {
				return err
			}
			if !back && selected != actionBack {
				variable.Source = selected
				if selected == "none" {
					variable.Source = ""
				}
			}
		case "delete":
			confirmed, err := confirmDelete(runner, "variable", name)
			if err != nil {
				return err
			}
			if confirmed {
				return deleteResource(data, "variable", name)
			}
		}
	}
}

func editTools(runner *fieldRunner, data *scaffold.Data) error {
	for {
		options := make([]huh.Option[string], 0, len(data.Tools)+2)
		for _, tool := range data.Tools {
			options = append(options, huh.NewOption(toolLabel(data, tool), "edit:"+tool.Name))
		}
		options = append(options, huh.NewOption("Add tool", "add"), huh.NewOption("← Back", actionBack))
		choice, _, err := runner.selectOne("Tools", runner.describe("Tools may call a webhook or run local Python when the selected target driver supports it."), options, true)
		if err != nil || choice == actionBack {
			return err
		}
		if strings.HasPrefix(choice, "edit:") {
			for i := range data.Tools {
				if data.Tools[i].Name == strings.TrimPrefix(choice, "edit:") {
					if err := editTool(runner, data, &data.Tools[i]); err != nil {
						return err
					}
					break
				}
			}
			continue
		}
		tool := scaffold.Tool{Execution: "webhook", Input: `{"type":"object","properties":{}}`}
		back, err := runner.input("Tool name", "Lowercase snake_case.", &tool.Name, func(value string) error {
			if err := validateIdentifier(value); err != nil {
				return err
			}
			for _, existing := range data.Tools {
				if existing.Name == value {
					return errors.New("tool already exists")
				}
			}
			return nil
		})
		if err != nil || back {
			continue
		}
		tool.AttachTo = []string{firstNonempty(data.EntryAgent, "assistant")}
		data.Tools = append(data.Tools, tool)
		if err := editTool(runner, data, &data.Tools[len(data.Tools)-1]); err != nil {
			return err
		}
	}
}

// dropUnsupportedBuiltins removes prebuilt tools the current target cannot
// host. Switching to a managed target would otherwise leave the seeded
// end_call default as a guaranteed validation failure. Dropped tools are not
// re-added on switching back; the user re-adds them if wanted.
func dropUnsupportedBuiltins(data *scaffold.Data) {
	provider := targetcap.Provider(data.Target)
	if targetcap.Default().Capability(targetcap.FieldToolBuiltin, provider).Tag != targetcap.Gated {
		return
	}
	kept := data.Tools[:0]
	for _, tool := range data.Tools {
		if tool.ExecutionKind() == "builtin" {
			continue
		}
		kept = append(kept, tool)
	}
	data.Tools = kept
}

func toolLabel(data *scaffold.Data, tool scaffold.Tool) string {
	return fmt.Sprintf("%s  ·  %s  ·  %s", tool.Name, tool.ExecutionKind(), toolAttachmentLabel(data, tool))
}

// toolExecutionKinds are the execution kinds the console can express, each
// gated by its capability-table field (a zero field means core everywhere,
// SCHEMA §5: webhook is the safe choice). The picker derives its options and
// notices from this list plus the table — never a hardcoded kind list (V42).
type toolExecutionKind struct {
	Value string
	Name  string
	Field targetcap.Field // "" = ungated
}

var toolExecutionKinds = []toolExecutionKind{
	{Value: "webhook", Name: "Webhook"},
	{Value: "local", Name: "Local Python", Field: targetcap.FieldToolLocal},
	{Value: "mcp", Name: "MCP server", Field: targetcap.FieldToolMCP},
	{Value: "builtin", Name: "Prebuilt", Field: targetcap.FieldToolBuiltin},
}

// toolExecutionGate returns the capability row gating kind on target; ok is
// false when the kind is gated there (V42: offered iff non-gated in the table).
func toolExecutionGate(kind toolExecutionKind, target string) (targetcap.Capability, bool) {
	if kind.Field == "" {
		return targetcap.Capability{}, true
	}
	capability := targetcap.Default().Capability(kind.Field, targetcap.Provider(target))
	return capability, capability.Tag != targetcap.Gated
}

func chooseToolExecution(runner *fieldRunner, target string, tool *scaffold.Tool) (bool, error) {
	for {
		handler := filepath.ToSlash(filepath.Join("tools", tool.Name+".py"))
		detail := map[string]string{
			"webhook": "HTTP endpoint from an environment variable",
			"local":   "creates " + handler + " when saved",
			"mcp":     "server address from an environment variable",
			"builtin": "provider prebuilt tool (end_call)",
		}
		options := make([]huh.Option[string], 0, len(toolExecutionKinds)+1)
		for _, kind := range toolExecutionKinds {
			label := kind.Name + "  ·  " + detail[kind.Value]
			if _, ok := toolExecutionGate(kind, target); !ok {
				label = kind.Name + "  ·  unavailable on " + targetLabel(target)
			}
			options = append(options, huh.NewOption(label, kind.Value))
		}
		options = append(options, huh.NewOption("← Back", actionBack))
		selected, back, err := runner.selectOne("Tool execution", "Choose where this tool runs. Local Python creates an empty handler file when supported.", options, true)
		if err != nil || back || selected == actionBack {
			return back || selected == actionBack, err
		}
		for _, kind := range toolExecutionKinds {
			if kind.Value != selected {
				continue
			}
			if capability, ok := toolExecutionGate(kind, target); !ok {
				message := fmt.Sprintf("%s: %s. Change Target under Identity → Target to use %s.", targetLabel(target), capability.Note, kind.Name)
				if err := showNotice(runner, kind.Name+" unavailable", message); err != nil {
					return false, err
				}
				selected = ""
			}
			break
		}
		if selected == "" {
			continue
		}
		tool.Execution = selected
		tool.Handler = ""
		if selected == "local" {
			tool.Handler = handler
			tool.URLEnv = ""
		}
		if selected == "mcp" {
			// The server describes its own tools, so an mcp file is the block
			// and nothing else: no description, input, or output (N40).
			tool.Description, tool.Input, tool.Output = "", "", ""
		}
		if selected == "builtin" {
			// The registry has one id today; default it and drop the
			// webhook/local fields a prebuilt tool never carries.
			tool.URLEnv, tool.Input, tool.Output = "", "", ""
			if tool.Builtin == "" {
				tool.Builtin = "end_call"
			}
		} else {
			tool.Builtin, tool.Instructions = "", ""
		}
		return false, nil
	}
}

func editTool(runner *fieldRunner, data *scaffold.Data, tool *scaffold.Tool) error {
	for {
		var options []huh.Option[string]
		switch {
		case tool.ExecutionKind() == "builtin":
			// A prebuilt tool carries no input/output/url; the registry owns its
			// schema. Description and the goodbye message are the only knobs.
			options = []huh.Option[string]{
				huh.NewOption("Description  ·  "+oneLine(tool.Description), "description"),
				huh.NewOption("Prebuilt  ·  "+firstNonempty(tool.Builtin, "end_call"), "execution"),
				huh.NewOption("Goodbye message  ·  "+firstNonempty(oneLine(tool.Instructions), "provider default"), "instructions"),
				huh.NewOption("Attached to  ·  "+toolAttachmentLabel(data, *tool), "attach"),
				huh.NewOption("Delete tool", "delete"),
				huh.NewOption("← Back", actionBack),
			}
		case tool.ExecutionKind() == "mcp":
			// The server announces its own tools, so the file has no
			// description, input, or output to edit (N40). Transport, auth, and
			// a tool selection are written by hand and carried through here
			// untouched.
			options = []huh.Option[string]{
				huh.NewOption("Execution  ·  mcp", "execution"),
				huh.NewOption("MCP server URL env  ·  "+firstNonempty(tool.URLEnv, "none"), "url"),
				huh.NewOption("Attached to  ·  "+toolAttachmentLabel(data, *tool), "attach"),
				huh.NewOption("Delete tool", "delete"),
				huh.NewOption("← Back", actionBack),
			}
		default:
			executionField := huh.NewOption("Webhook URL env  ·  "+firstNonempty(tool.URLEnv, "none"), "url")
			if tool.ExecutionKind() == "local" {
				executionField = huh.NewOption("Python handler  ·  "+firstNonempty(tool.Handler, "none"), "handler")
			}
			options = []huh.Option[string]{
				huh.NewOption("Description  ·  "+oneLine(tool.Description), "description"),
				huh.NewOption("Execution  ·  "+tool.ExecutionKind(), "execution"),
				executionField,
				huh.NewOption("Input schema  ·  "+oneLine(tool.Input), "input"),
				huh.NewOption("Output schema  ·  "+firstNonempty(oneLine(tool.Output), "unconstrained"), "output"),
				huh.NewOption("Attached to  ·  "+toolAttachmentLabel(data, *tool), "attach"),
				huh.NewOption("Delete tool", "delete"),
				huh.NewOption("← Back", actionBack),
			}
		}
		choice, _, err := runner.selectOne(tool.Name, "", options, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "description":
			// Optional for a builtin (the registry supplies a default).
			validate := validateRequiredText
			if tool.ExecutionKind() == "builtin" {
				validate = validateBasic
			}
			if _, err := runner.input("Description", "What the model sees.", &tool.Description, validate); err != nil {
				return err
			}
		case "instructions":
			if _, err := runner.input("Goodbye message", "Optional. What the agent says as it ends the call.", &tool.Instructions, validateBasic); err != nil {
				return err
			}
		case "execution":
			if _, err := chooseToolExecution(runner, data.Target, tool); err != nil {
				return err
			}
		case "url":
			title, hint := "Webhook URL env", "Environment variable containing the URL; never the URL itself."
			if tool.ExecutionKind() == "mcp" {
				title, hint = "MCP server URL env", "Environment variable containing the MCP server address; never the URL itself."
			}
			if _, err := runner.input(title, hint, &tool.URLEnv, validateEnvName); err != nil {
				return err
			}
		case "handler":
			if err := showNotice(runner, "Python handler", tool.Handler+" is created empty when you save. Add the implementation in that file."); err != nil {
				return err
			}
		case "input":
			if _, err := runner.input("Input JSON Schema", "JSON object.", &tool.Input, validateRequiredObject); err != nil {
				return err
			}
		case "output":
			if _, err := runner.input("Output JSON Schema (optional)", "Blank leaves provider output unconstrained.", &tool.Output, validateParams); err != nil {
				return err
			}
		case "attach":
			available, selected := toolAttachmentChoices(data, *tool)
			selected, back, err := pickReferences(runner, "Attach tool", "Select every place that can call this tool.", available, selected, false)
			if err != nil {
				return err
			}
			if !back {
				tool.AttachTo, tool.AttachTasks = nil, nil
				for _, attachment := range selected {
					if name, ok := strings.CutPrefix(attachment, "Agent · "); ok {
						tool.AttachTo = append(tool.AttachTo, name)
					}
					if name, ok := strings.CutPrefix(attachment, "Task · "); ok {
						tool.AttachTasks = append(tool.AttachTasks, name)
					}
				}
			}
		case "delete":
			name := tool.Name
			confirmed, err := confirmDelete(runner, "tool", name)
			if err != nil {
				return err
			}
			if confirmed {
				return deleteResource(data, "tool", name)
			}
		}
	}
}

func toolAttachmentChoices(data *scaffold.Data, tool scaffold.Tool) ([]string, []string) {
	available := make([]string, 0, len(data.AllAgents())+len(data.Tasks))
	for _, name := range agentNames(data) {
		available = append(available, "Agent · "+name)
	}
	for _, name := range taskNames(data) {
		available = append(available, "Task · "+name)
	}
	selected := make([]string, 0, len(tool.AttachTo)+len(tool.AttachTasks))
	for _, name := range tool.AttachTo {
		selected = append(selected, "Agent · "+name)
	}
	if tool.AttachTo == nil {
		selected = append(selected, "Agent · "+firstNonempty(data.EntryAgent, "assistant"))
	}
	for _, name := range tool.AttachTasks {
		selected = append(selected, "Task · "+name)
	}
	return available, selected
}

func toolAttachmentLabel(data *scaffold.Data, tool scaffold.Tool) string {
	_, selected := toolAttachmentChoices(data, tool)
	return firstNonempty(strings.Join(selected, ", "), "not attached")
}

func editAgents(runner *fieldRunner, data *scaffold.Data) error {
	for {
		options := make([]huh.Option[string], 0, len(data.AllAgents())+3)
		for _, agent := range data.AllAgents() {
			options = append(options, huh.NewOption(agentLabel(data, agent), "edit:"+agent.Name))
		}
		options = append(options,
			huh.NewOption("Add agent", "add"),
			huh.NewOption("Choose entry agent  ·  "+firstNonempty(data.EntryAgent, "assistant"), "entry"),
			huh.NewOption("← Back", actionBack),
		)
		choice, _, err := runner.selectOne("Agents", "Select any agent to edit its prompt, LLM, TTS, and tools from one screen.", options, true)
		if err != nil || choice == actionBack {
			return err
		}
		if strings.HasPrefix(choice, "edit:") {
			if err := editAgentDetails(runner, data, strings.TrimPrefix(choice, "edit:")); err != nil {
				return err
			}
			continue
		}
		if choice == "entry" {
			options := agentOptions(data.AllAgents(), "")
			options = append(options, huh.NewOption("← Back", actionBack))
			selected, back, err := runner.selectOne("Entry agent", "", options, true)
			if err != nil {
				return err
			}
			if !back && selected != actionBack {
				data.EntryAgent = selected
			}
			continue
		}

		agent := scaffold.Agent{
			Instructions: "You are a helpful specialist. Keep every answer to one or two short sentences.",
			Reason:       data.Reason,
			Speak:        data.Speak,
		}
		back, err := runner.input("Agent name", "Lowercase snake_case.", &agent.Name, func(value string) error {
			if err := validateIdentifier(value); err != nil {
				return err
			}
			for _, existing := range data.AllAgents() {
				if existing.Name == value {
					return errors.New("agent already exists")
				}
			}
			return nil
		})
		if err != nil || back {
			continue
		}
		data.Agents = append(data.Agents, agent)
		if err := editAgentDetails(runner, data, agent.Name); err != nil {
			return err
		}
	}
}

func agentLabel(data *scaffold.Data, agent scaffold.Agent) string {
	entry := ""
	if agent.Name == firstNonempty(data.EntryAgent, "assistant") {
		entry = "  ·  entry"
	}
	return fmt.Sprintf("%s  ·  %s  ·  %s  ·  %d tools%s",
		agent.Name, bindingLabel(agent.Reason), bindingLabel(agent.Speak), len(agentToolNames(data, agent.Name)), entry)
}

func bindingLabel(binding scaffold.Binding) string {
	identity := firstNonempty(binding.Provider, "integrated")
	if binding.Model != "" {
		identity += "/" + binding.Model
	}
	if binding.Voice != "" {
		identity += " voice=" + binding.Voice
	}
	return identity
}

func editAgentDetails(runner *fieldRunner, data *scaffold.Data, name string) error {
	for {
		agent := agentByName(data, name)
		lifecycle := huh.NewOption("Delete agent", "delete")
		if name == "assistant" {
			lifecycle = huh.NewOption("Reset starter agent", "reset")
		}
		choice, _, err := runner.selectOne(name, "All agent-specific settings are visible here; select a row to change it.", []huh.Option[string]{
			huh.NewOption("Prompt  ·  "+oneLine(agent.Instructions), "prompt"),
			huh.NewOption("LLM model  ·  "+bindingLabel(agent.Reason), "reason"),
			huh.NewOption("TTS voice  ·  "+bindingLabel(agent.Speak), "speak"),
			huh.NewOption("Tools  ·  "+firstNonempty(strings.Join(agentToolNames(data, name), ", "), "none"), "tools"),
			huh.NewOption("Entry agent  ·  "+yesNo(name == firstNonempty(data.EntryAgent, "assistant")), "entry"),
			lifecycle,
			huh.NewOption("← Back", actionBack),
		}, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "prompt":
			prompt := agent.Instructions
			back, err := runner.text("Agent instructions", "Prompt for this agent.", &prompt)
			if err != nil {
				return err
			}
			if !back {
				setAgentPrompt(data, name, prompt)
			}
		case "reason":
			binding := agent.Reason
			if err := editBindingFor(runner, data.Target, targetcap.Reason, &binding); err != nil {
				return err
			}
			setAgentBinding(data, name, targetcap.Reason, binding)
		case "speak":
			binding := agent.Speak
			if err := editBindingFor(runner, data.Target, targetcap.Speak, &binding); err != nil {
				return err
			}
			setAgentBinding(data, name, targetcap.Speak, binding)
		case "tools":
			if len(data.Tools) == 0 {
				if err := showNotice(runner, "No tools yet", "Add a tool from Tools, then return here to attach it."); err != nil {
					return err
				}
				continue
			}
			selected, back, err := pickReferences(runner, "Tools for "+name, "Toggle the tools this agent can call.", toolNames(data), agentToolNames(data, name), true)
			if err != nil {
				return err
			}
			if !back {
				setAgentTools(data, name, selected)
			}
		case "entry":
			data.EntryAgent = name
		case "reset":
			confirmed, err := confirmAction(runner, "Reset starter agent?", "Reset agent")
			if err != nil {
				return err
			}
			if confirmed {
				resetStarterAgent(data)
			}
		case "delete":
			confirmed, err := confirmDelete(runner, "agent", name)
			if err != nil {
				return err
			}
			if confirmed {
				return deleteResource(data, "agent", name)
			}
		}
	}
}

func resetStarterAgent(data *scaffold.Data) {
	defaults := scaffold.Data{}
	defaults.SetTarget(data.Target)
	data.Instructions = scaffold.DefaultInstructions
	data.Reason = defaults.Reason
	data.Speak = defaults.Speak
	data.EntryAgent = "assistant"
}

func agentByName(data *scaffold.Data, name string) scaffold.Agent {
	if name == "assistant" {
		return scaffold.Agent{Name: name, Instructions: data.Instructions, Reason: data.Reason, Speak: data.Speak}
	}
	for _, agent := range data.Agents {
		if agent.Name == name {
			return agent
		}
	}
	return scaffold.Agent{Name: name}
}

func setAgentPrompt(data *scaffold.Data, name, prompt string) {
	if name == "assistant" {
		data.Instructions = prompt
		return
	}
	for i := range data.Agents {
		if data.Agents[i].Name == name {
			data.Agents[i].Instructions = prompt
			return
		}
	}
}

func setAgentBinding(data *scaffold.Data, name string, role targetcap.Role, binding scaffold.Binding) {
	if name == "assistant" {
		if role == targetcap.Reason {
			data.Reason = binding
		} else {
			data.Speak = binding
		}
		return
	}
	for i := range data.Agents {
		if data.Agents[i].Name != name {
			continue
		}
		if role == targetcap.Reason {
			data.Agents[i].Reason = binding
		} else {
			data.Agents[i].Speak = binding
		}
		return
	}
}

func agentToolNames(data *scaffold.Data, name string) []string {
	var names []string
	for _, tool := range data.Tools {
		attach := tool.AttachTo
		if attach == nil {
			attach = []string{"assistant"}
		}
		if containsName(attach, name) {
			names = append(names, tool.Name)
		}
	}
	return names
}

func setAgentTools(data *scaffold.Data, name string, selected []string) {
	for i := range data.Tools {
		attach := append([]string(nil), data.Tools[i].AttachTo...)
		if data.Tools[i].AttachTo == nil {
			attach = []string{"assistant"}
		}
		attach = removeName(attach, name)
		if containsName(selected, data.Tools[i].Name) {
			attach = append(attach, name)
		}
		data.Tools[i].AttachTo = append([]string{}, attach...)
	}
}

func editHandoffs(runner *fieldRunner, data *scaffold.Data) error {
	if len(data.AllAgents()) < 2 && len(data.Handoffs) == 0 {
		return showNotice(runner, "Handoffs unavailable", "Create a second agent first. Handoffs always select a source and target from the saved agents.")
	}
	for {
		options := make([]huh.Option[string], 0, len(data.Handoffs)+2)
		for _, handoff := range data.Handoffs {
			options = append(options, huh.NewOption(fmt.Sprintf("%s  ·  %s → %s", handoff.Name, handoff.Source, handoff.To), "view:"+handoff.Name))
		}
		options = append(options, huh.NewOption("Add handoff", "add"), huh.NewOption("← Back", actionBack))
		choice, _, err := runner.selectOne("Directional handoffs", "", options, true)
		if err != nil || choice == actionBack {
			return err
		}
		if strings.HasPrefix(choice, "view:") {
			if err := editHandoffDetails(runner, data, strings.TrimPrefix(choice, "view:")); err != nil {
				return err
			}
			continue
		}
		if len(data.AllAgents()) < 2 {
			if err := showNotice(runner, "Cannot add handoff", "Create a second agent first. Existing handoffs remain available above for repair or deletion."); err != nil {
				return err
			}
			continue
		}

		agents := data.AllAgents()
		handoff := scaffold.Handoff{Source: agents[0].Name, To: agents[1].Name, History: "full", AllVariables: true}
		handoff.Name = "to_" + handoff.To
		back, err := runner.input("Handoff name", "Lowercase snake_case; controls and tools share one namespace.", &handoff.Name, func(value string) error {
			if err := validateIdentifier(value); err != nil {
				return err
			}
			for _, tool := range data.Tools {
				if tool.Name == value {
					return errors.New("name already used by a tool")
				}
			}
			for _, existing := range data.Handoffs {
				if existing.Name == value {
					return errors.New("handoff already exists")
				}
			}
			return nil
		})
		if err != nil || back {
			continue
		}
		data.Handoffs = append(data.Handoffs, handoff)
		if err := editHandoffDetails(runner, data, handoff.Name); err != nil {
			return err
		}
	}
}

func handoffVariablesLabel(handoff scaffold.Handoff) string {
	if handoff.AllVariables {
		return "all"
	}
	return firstNonempty(strings.Join(handoff.Variables, ", "), "none")
}

func editHandoffDetails(runner *fieldRunner, data *scaffold.Data, name string) error {
	for {
		var handoff *scaffold.Handoff
		for i := range data.Handoffs {
			if data.Handoffs[i].Name == name {
				handoff = &data.Handoffs[i]
				break
			}
		}
		if handoff == nil {
			return nil
		}
		choice, _, err := runner.selectOne(name, "Edit this saved handoff or remove it.", []huh.Option[string]{
			huh.NewOption("Source agent  ·  "+handoff.Source, "source"),
			huh.NewOption("Target agent  ·  "+handoff.To, "target"),
			huh.NewOption("Trigger  ·  "+oneLine(handoff.When), "trigger"),
			huh.NewOption("Required variables  ·  "+firstNonempty(strings.Join(handoff.Requires, ", "), "none"), "requires"),
			huh.NewOption("Context  ·  "+firstNonempty(handoff.History, "full")+" · variables "+handoffVariablesLabel(*handoff), "context"),
			huh.NewOption("Delete handoff", "delete"),
			huh.NewOption("← Back", actionBack),
		}, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "source":
			options := agentOptions(data.AllAgents(), "")
			options = append(options, huh.NewOption("← Back", actionBack))
			source, back, err := runner.selectOne("Source agent", "", options, true)
			if err != nil {
				return err
			}
			if !back && source != handoff.To {
				handoff.Source = source
			}
		case "target":
			options := agentOptions(data.AllAgents(), handoff.Source)
			options = append(options, huh.NewOption("← Back", actionBack))
			targetName, back, err := runner.selectOne("Target agent", "", options, true)
			if err != nil {
				return err
			}
			if !back && targetName != actionBack {
				handoff.To = targetName
			}
		case "trigger":
			if _, err := runner.input("When to hand off", "Plain-language trigger shown to the model.", &handoff.When, validateRequiredText); err != nil {
				return err
			}
		case "requires":
			selected, back, err := pickReferences(runner, "Required variables (optional)", "The handoff is available only after every selected variable has a value.", variableNames(data), handoff.Requires, true)
			if err != nil {
				return err
			}
			if !back {
				handoff.Requires = selected
			}
		case "context":
			if err := editHandoffContextDetails(runner, data, handoff); err != nil {
				return err
			}
		case "delete":
			confirmed, err := confirmDelete(runner, "handoff", name)
			if err != nil {
				return err
			}
			if confirmed {
				return deleteResource(data, "handoff", name)
			}
		}
	}
}

func editHandoffContextDetails(runner *fieldRunner, data *scaffold.Data, handoff *scaffold.Handoff) error {
	for {
		history := firstNonempty(handoff.History, "full")
		tools := "provider default"
		if handoff.IncludeToolCalls != nil {
			tools = map[bool]string{true: "include", false: "exclude"}[*handoff.IncludeToolCalls]
		}
		scope := "none"
		if handoff.AllVariables {
			scope = "all"
		} else if len(handoff.Variables) > 0 {
			scope = "selected"
		}
		options := []huh.Option[string]{huh.NewOption("History  ·  "+history, "history")}
		if history == "last_n" {
			options = append(options, huh.NewOption(fmt.Sprintf("Maximum messages  ·  %d", handoff.MaxMessages), "maximum"))
		}
		if history == "summary" {
			options = append(options, huh.NewOption("Summarizer model  ·  "+firstNonempty(handoff.Summarizer, data.AllAgents()[0].ModelProfile()), "summarizer"))
		}
		options = append(options, huh.NewOption("Tool calls  ·  "+tools, "tools"))
		if len(data.Variables) > 0 {
			options = append(options, huh.NewOption("Variable scope  ·  "+scope, "scope"))
			if scope == "selected" {
				options = append(options, huh.NewOption("Selected variables  ·  "+strings.Join(handoff.Variables, ", "), "variables"))
			}
		}
		options = append(options, huh.NewOption("← Back", actionBack))
		choice, _, err := runner.selectOne("Handoff context", "Edit one field, then return here.", options, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "history":
			choices := []huh.Option[string]{
				huh.NewOption("Full history (portable)", "full"),
				huh.NewOption("Messages", "messages"),
				huh.NewOption("Last N messages", "last_n"),
				huh.NewOption("Summary", "summary"),
				huh.NewOption("Reset", "reset"),
				huh.NewOption("← Back", actionBack),
			}
			selected, back, err := runner.selectOne("Conversation history", "", choices, true)
			if err != nil {
				return err
			}
			if !back {
				handoff.History, handoff.MaxMessages, handoff.Summarizer = selected, 0, ""
				if selected == "last_n" {
					handoff.MaxMessages = 10
				}
				if selected == "summary" {
					handoff.Summarizer = data.AllAgents()[0].ModelProfile()
				}
			}
		case "maximum":
			value := strconv.Itoa(handoff.MaxMessages)
			back, err := runner.input("Maximum messages", "Positive integer.", &value, validatePositiveInteger)
			if err != nil {
				return err
			}
			if !back {
				handoff.MaxMessages, _ = strconv.Atoi(value)
			}
		case "summarizer":
			profiles := make([]huh.Option[string], 0, len(data.AllAgents())+1)
			for _, agent := range data.AllAgents() {
				profiles = append(profiles, huh.NewOption(agent.ModelProfile(), agent.ModelProfile()))
			}
			profiles = append(profiles, huh.NewOption("← Back", actionBack))
			selected, back, err := runner.selectOne("Summarizer model profile", "", profiles, true)
			if err != nil {
				return err
			}
			if !back {
				handoff.Summarizer = selected
			}
		case "tools":
			selected, back, err := runner.selectOne("Tool calls in context", "", []huh.Option[string]{huh.NewOption("Provider default", "default"), huh.NewOption("Include", "yes"), huh.NewOption("Exclude", "no"), huh.NewOption("← Back", actionBack)}, true)
			if err != nil {
				return err
			}
			if !back {
				handoff.IncludeToolCalls = nil
				if selected != "default" {
					include := selected == "yes"
					handoff.IncludeToolCalls = &include
				}
			}
		case "scope":
			selected, back, err := runner.selectOne("Variables in context", "Available variables: "+strings.Join(variableNames(data), ", "), []huh.Option[string]{huh.NewOption("All variables", "all"), huh.NewOption("Selected variables", "selected"), huh.NewOption("No variables", "none"), huh.NewOption("← Back", actionBack)}, true)
			if err != nil {
				return err
			}
			if !back {
				handoff.AllVariables = selected == "all"
				if selected == "selected" && len(handoff.Variables) == 0 {
					handoff.Variables = []string{data.Variables[0].Name}
				} else if selected != "selected" {
					handoff.Variables = nil
				}
			}
		case "variables":
			selected, back, err := pickReferences(runner, "Variables to include", "Choose which saved variables enter the target agent's context.", variableNames(data), handoff.Variables, false)
			if err != nil {
				return err
			}
			if !back {
				handoff.Variables = selected
			}
		}
	}
}

func editTasks(runner *fieldRunner, data *scaffold.Data) error {
	for {
		options := make([]huh.Option[string], 0, len(data.Tasks)+2)
		for _, task := range data.Tasks {
			options = append(options, huh.NewOption(taskLabel(task), "view:"+task.Name))
		}
		options = append(options, huh.NewOption("Add task", "add"), huh.NewOption("← Back", actionBack))
		choice, _, err := runner.selectOne("Tasks", "", options, true)
		if err != nil || choice == actionBack {
			return err
		}
		if strings.HasPrefix(choice, "view:") {
			if err := editTaskDetails(runner, data, strings.TrimPrefix(choice, "view:")); err != nil {
				return err
			}
			continue
		}
		task := scaffold.Task{
			Instructions: "Complete this focused task and return only the structured result.",
			Result:       `{"result":"string"}`,
			History:      "full",
			Agent:        firstNonempty(data.EntryAgent, "assistant"),
		}
		back, err := runner.input("Task name", "Lowercase snake_case; its delegate is named run_<task>.", &task.Name, func(value string) error {
			if err := validateIdentifier(value); err != nil {
				return err
			}
			for _, existing := range data.Tasks {
				if existing.Name == value {
					return errors.New("task already exists")
				}
			}
			return validateControlName(data, "run_"+value)
		})
		if err != nil || back {
			continue
		}
		data.Tasks = append(data.Tasks, task)
		if err := editTaskDetails(runner, data, task.Name); err != nil {
			return err
		}
	}
}

func taskLabel(task scaffold.Task) string {
	return fmt.Sprintf("%s  ·  %s  ·  %s", task.Name, firstNonempty(task.Agent, "entry agent"), task.Result)
}

func editTaskDetails(runner *fieldRunner, data *scaffold.Data, name string) error {
	for {
		var task *scaffold.Task
		for i := range data.Tasks {
			if data.Tasks[i].Name == name {
				task = &data.Tasks[i]
				break
			}
		}
		if task == nil {
			return nil
		}
		choice, _, err := runner.selectOne(name, "Edit this saved task or remove it.", []huh.Option[string]{
			huh.NewOption("Prompt  ·  "+oneLine(task.Instructions), "prompt"),
			huh.NewOption("Tools  ·  "+firstNonempty(strings.Join(task.Tools, ", "), "none"), "tools"),
			huh.NewOption("Model  ·  "+firstNonempty(task.Model, "entry agent model"), "model"),
			huh.NewOption("Typed result  ·  "+task.Result, "result"),
			huh.NewOption("Context  ·  "+firstNonempty(task.History, "full"), "context"),
			huh.NewOption("Delegating agent  ·  "+firstNonempty(task.Agent, "assistant"), "agent"),
			huh.NewOption("Trigger  ·  "+oneLine(task.When), "trigger"),
			huh.NewOption("Result assignments  ·  "+firstNonempty(strings.Join(assignmentVariables(task.Assign), ", "), "none"), "assign"),
			huh.NewOption("Delete task", "delete"),
			huh.NewOption("← Back", actionBack),
		}, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "prompt":
			if _, err := runner.text("Task instructions", "Prompt for this delegated task.", &task.Instructions); err != nil {
				return err
			}
		case "tools":
			selected, back, err := pickReferences(runner, "Task tools (optional)", "Choose from every saved tool.", toolNames(data), task.Tools, true)
			if err != nil {
				return err
			}
			if !back {
				task.Tools = selected
			}
		case "model":
			options := []huh.Option[string]{huh.NewOption("Entry agent model", "default")}
			for _, agent := range data.AllAgents() {
				options = append(options, huh.NewOption(agent.ModelProfile(), agent.ModelProfile()))
			}
			options = append(options, huh.NewOption("← Back", actionBack))
			selected, back, err := runner.selectOne("Task model", "", options, true)
			if err != nil {
				return err
			}
			if !back && selected != actionBack {
				task.Model = selected
				if selected == "default" {
					task.Model = ""
				}
			}
		case "result":
			if _, err := runner.input("Typed result", `Prefilled default: {"result":"string"}. Each key becomes one returned field. Use string, number, boolean, integer, or a nonempty enum.`, &task.Result, validateTaskResult); err != nil {
				return err
			}
		case "context":
			if _, err := editTaskContext(runner, data, task); err != nil {
				return err
			}
		case "agent":
			options := agentOptions(data.AllAgents(), "")
			options = append(options, huh.NewOption("← Back", actionBack))
			selected, back, err := runner.selectOne("Agent allowed to delegate", "", options, true)
			if err != nil {
				return err
			}
			if !back && selected != actionBack {
				task.Agent = selected
			}
		case "trigger":
			if _, err := runner.input("When to run", "Plain-language trigger shown to the agent.", &task.When, validateRequiredText); err != nil {
				return err
			}
		case "assign":
			if len(data.Variables) == 0 {
				if err := showNotice(runner, "No variables yet", "Add a variable first, then return here to save task results into it."); err != nil {
					return err
				}
				continue
			}
			if _, err := editTaskAssignments(runner, data, task); err != nil {
				return err
			}
		case "delete":
			confirmed, err := confirmDelete(runner, "task", name)
			if err != nil {
				return err
			}
			if confirmed {
				return deleteResource(data, "task", name)
			}
		}
	}
}

func editTaskContext(runner *fieldRunner, data *scaffold.Data, task *scaffold.Task) (bool, error) {
	for {
		history := firstNonempty(task.History, "full")
		tools := "provider default"
		if task.IncludeToolCalls != nil {
			tools = map[bool]string{true: "include", false: "exclude"}[*task.IncludeToolCalls]
		}
		options := []huh.Option[string]{huh.NewOption("History  ·  "+history, "history")}
		if history == "last_n" {
			options = append(options, huh.NewOption(fmt.Sprintf("Maximum messages  ·  %d", task.MaxMessages), "maximum"))
		}
		if history == "summary" {
			options = append(options, huh.NewOption("Summarizer model  ·  "+firstNonempty(task.Summarizer, data.AllAgents()[0].ModelProfile()), "summarizer"))
		}
		options = append(options, huh.NewOption("Tool calls  ·  "+tools, "tools"), huh.NewOption("← Back", actionBack))
		choice, _, err := runner.selectOne("Task context", "Edit one field, then return here.", options, true)
		if err != nil || choice == actionBack {
			return true, err
		}
		switch choice {
		case "history":
			choices := []huh.Option[string]{
				huh.NewOption("Full history (portable)", "full"),
				huh.NewOption("Messages", "messages"),
				huh.NewOption("Last N messages", "last_n"),
				huh.NewOption("Summary", "summary"),
				huh.NewOption("Reset", "reset"),
				huh.NewOption("← Back", actionBack),
			}
			selected, back, err := runner.selectOne("Task history", "", choices, true)
			if err != nil {
				return false, err
			}
			if !back {
				task.History, task.MaxMessages, task.Summarizer = selected, 0, ""
				if selected == "last_n" {
					task.MaxMessages = 10
				}
				if selected == "summary" {
					task.Summarizer = data.AllAgents()[0].ModelProfile()
				}
			}
		case "maximum":
			value := strconv.Itoa(task.MaxMessages)
			back, err := runner.input("Maximum messages", "Positive integer.", &value, validatePositiveInteger)
			if err != nil {
				return false, err
			}
			if !back {
				task.MaxMessages, _ = strconv.Atoi(value)
			}
		case "summarizer":
			var profiles []huh.Option[string]
			for _, agent := range data.AllAgents() {
				profiles = append(profiles, huh.NewOption(agent.ModelProfile(), agent.ModelProfile()))
			}
			profiles = append(profiles, huh.NewOption("← Back", actionBack))
			selected, back, err := runner.selectOne("Summarizer model profile", "", profiles, true)
			if err != nil {
				return false, err
			}
			if !back {
				task.Summarizer = selected
			}
		case "tools":
			selected, back, err := runner.selectOne("Tool calls in task context", "", []huh.Option[string]{huh.NewOption("Provider default", "default"), huh.NewOption("Include", "yes"), huh.NewOption("Exclude", "no"), huh.NewOption("← Back", actionBack)}, true)
			if err != nil {
				return false, err
			}
			if !back {
				task.IncludeToolCalls = nil
				if selected != "default" {
					include := selected == "yes"
					task.IncludeToolCalls = &include
				}
			}
		}
	}
}

func editTaskAssignments(runner *fieldRunner, data *scaffold.Data, task *scaffold.Task) (bool, error) {
	current := assignmentVariables(task.Assign)
	selected, back, err := pickReferences(runner, "Save result to variables (optional)", "Choose which saved variables receive a field from this task result.", variableNames(data), current, true)
	if err != nil || back {
		return back, err
	}
	if len(selected) == 0 {
		task.Assign = ""
		return false, nil
	}
	fields := taskResultNames(task.Result)
	assignments := make(map[string]string, len(selected))
	for _, variable := range selected {
		field := assignmentField(task.Assign, variable)
		if !containsName(fields, field) {
			field = fields[0]
		}
		assignments[variable] = "result." + field
	}
	for {
		options := make([]huh.Option[string], 0, len(selected)+2)
		for _, variable := range selected {
			options = append(options, huh.NewOption(variable+"  ·  "+strings.TrimPrefix(assignments[variable], "result."), variable))
		}
		options = append(options, huh.NewOption("Done", "done"), huh.NewOption("← Back", actionBack))
		choice, _, err := runner.selectOne("Task result assignments", "Every selected variable shows its current result field.", options, true)
		if err != nil {
			return false, err
		}
		if choice == actionBack {
			return true, nil
		}
		if choice == "done" {
			if len(assignments) == 0 {
				task.Assign = ""
				return false, nil
			}
			raw, err := json.Marshal(assignments)
			if err != nil {
				return false, err
			}
			task.Assign = string(raw)
			return false, nil
		}
		fieldOptions := make([]huh.Option[string], 0, len(fields)+2)
		for _, name := range fields {
			fieldOptions = append(fieldOptions, huh.NewOption(name, name))
		}
		fieldOptions = append(fieldOptions, huh.NewOption("Remove assignment", "remove"))
		fieldOptions = append(fieldOptions, huh.NewOption("← Back", actionBack))
		field, back, err := runner.selectOne("Result field for "+choice, "", fieldOptions, true)
		if err != nil {
			return false, err
		}
		if !back {
			if field == "remove" {
				delete(assignments, choice)
				selected = removeName(selected, choice)
				continue
			}
			assignments[choice] = "result." + field
		}
	}
}

func taskResultNames(result string) []string {
	var fields map[string]any
	_ = json.Unmarshal([]byte(result), &fields)
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func assignmentVariables(raw string) []string {
	var assignments map[string]string
	_ = json.Unmarshal([]byte(raw), &assignments)
	names := make([]string, 0, len(assignments))
	for name := range assignments {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func assignmentField(raw, variable string) string {
	var assignments map[string]string
	_ = json.Unmarshal([]byte(raw), &assignments)
	return strings.TrimPrefix(assignments[variable], "result.")
}

func editTaskGroups(runner *fieldRunner, data *scaffold.Data) error {
	if len(data.Tasks) == 0 && len(data.TaskGroups) == 0 {
		return showNotice(runner, "Task groups unavailable", "Create at least one task first. Every saved task will then appear in the ordered-step picker.")
	}
	for {
		options := make([]huh.Option[string], 0, len(data.TaskGroups)+2)
		for _, group := range data.TaskGroups {
			options = append(options, huh.NewOption(group.Name+"  ·  "+strings.Join(group.Steps, " → "), "view:"+group.Name))
		}
		options = append(options, huh.NewOption("Add task group", "add"), huh.NewOption("← Back", actionBack))
		choice, _, err := runner.selectOne("Ordered task groups", "", options, true)
		if err != nil || choice == actionBack {
			return err
		}
		if strings.HasPrefix(choice, "view:") {
			if err := editTaskGroupDetails(runner, data, strings.TrimPrefix(choice, "view:")); err != nil {
				return err
			}
			continue
		}
		if len(data.Tasks) == 0 {
			if err := showNotice(runner, "Cannot add task group", "Create a task first. Existing groups remain available above for repair or deletion."); err != nil {
				return err
			}
			continue
		}
		group := scaffold.TaskGroup{ContextScope: "shared", Then: "return", Agent: firstNonempty(data.EntryAgent, "assistant")}
		back, err := runner.input("Task group name", "Lowercase snake_case; its delegate is named run_<group>.", &group.Name, func(value string) error {
			if err := validateIdentifier(value); err != nil {
				return err
			}
			for _, existing := range data.TaskGroups {
				if existing.Name == value {
					return errors.New("task group already exists")
				}
			}
			return validateControlName(data, "run_"+value)
		})
		if err != nil || back {
			continue
		}
		group.Steps = []string{data.Tasks[0].Name}
		data.TaskGroups = append(data.TaskGroups, group)
		if err := editTaskGroupDetails(runner, data, group.Name); err != nil {
			return err
		}
	}
}

func editTaskGroupDetails(runner *fieldRunner, data *scaffold.Data, name string) error {
	for {
		var group *scaffold.TaskGroup
		for i := range data.TaskGroups {
			if data.TaskGroups[i].Name == name {
				group = &data.TaskGroups[i]
				break
			}
		}
		if group == nil {
			return nil
		}
		completion := group.Then
		if group.Then == "transfer" {
			completion += " to " + group.ThenTarget
		}
		choice, _, err := runner.selectOne(name, "Edit this saved task group or remove it.", []huh.Option[string]{
			huh.NewOption("Ordered steps  ·  "+strings.Join(group.Steps, " → "), "steps"),
			huh.NewOption("Context between steps  ·  "+group.ContextScope, "context"),
			huh.NewOption("Completion  ·  "+completion, "completion"),
			huh.NewOption("Transfer target  ·  "+firstNonempty(group.ThenTarget, "not applicable"), "target"),
			huh.NewOption("Delegating agent  ·  "+group.Agent, "agent"),
			huh.NewOption("Trigger  ·  "+oneLine(group.When), "trigger"),
			huh.NewOption("Delete task group", "delete"),
			huh.NewOption("← Back", actionBack),
		}, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "steps":
			selected, back, err := pickReferences(runner, "Ordered steps", "Toggle tasks in the order they should run.", taskNames(data), group.Steps, false)
			if err != nil {
				return err
			}
			if !back {
				group.Steps = selected
			}
		case "context":
			options := []huh.Option[string]{
				huh.NewOption("Shared context", "shared"),
				huh.NewOption("Isolated context", "isolated"),
				huh.NewOption("← Back", actionBack),
			}
			selected, back, err := runner.selectOne("Context between steps", "", options, true)
			if err != nil {
				return err
			}
			if !back && selected != actionBack {
				group.ContextScope = selected
			}
		case "completion":
			selected, back, err := runner.selectOne("After the final step", "", []huh.Option[string]{
				huh.NewOption("Return to caller agent", "return"), huh.NewOption("Transfer to agent", "transfer"), huh.NewOption("End conversation", "end"), huh.NewOption("← Back", actionBack),
			}, true)
			if err != nil {
				return err
			}
			if back || selected == actionBack {
				continue
			}
			group.Then, group.ThenTarget = selected, ""
			if selected == "transfer" {
				group.ThenTarget = data.AllAgents()[0].Name
			}
		case "target":
			if group.Then != "transfer" {
				if err := showNotice(runner, "Transfer target unavailable", "Set Completion to transfer first."); err != nil {
					return err
				}
				continue
			}
			options := agentOptions(data.AllAgents(), "")
			options = append(options, huh.NewOption("← Back", actionBack))
			selected, back, err := runner.selectOne("Transfer target", "", options, true)
			if err != nil {
				return err
			}
			if !back {
				group.ThenTarget = selected
			}
		case "agent":
			options := agentOptions(data.AllAgents(), "")
			options = append(options, huh.NewOption("← Back", actionBack))
			selected, back, err := runner.selectOne("Agent allowed to delegate", "", options, true)
			if err != nil {
				return err
			}
			if !back && selected != actionBack {
				group.Agent = selected
			}
		case "trigger":
			if _, err := runner.input("When to run", "Plain-language trigger shown to the agent.", &group.When, validateRequiredText); err != nil {
				return err
			}
		case "delete":
			confirmed, err := confirmDelete(runner, "task group", name)
			if err != nil {
				return err
			}
			if confirmed {
				return deleteResource(data, "group", name)
			}
		}
	}
}

func editChannels(runner *fieldRunner, data *scaffold.Data) error {
	for {
		mode := "web"
		phone := scaffold.Channel{Name: "phone", Kind: "telephony"}
		for _, channel := range data.AllChannels() {
			if channel.Kind != "telephony" {
				continue
			}
			phone = channel
			switch {
			case channel.Inbound && channel.Outbound:
				mode = "web_phone_both"
			case channel.Outbound:
				mode = "web_phone_outbound"
			default:
				mode = "web_phone_inbound"
			}
		}
		options := []huh.Option[string]{huh.NewOption("Channels  ·  "+strings.ReplaceAll(mode, "_", " "), "mode")}
		if mode != "web" {
			options = append(options, huh.NewOption("Required phone controls  ·  "+firstNonempty(strings.Join(phone.RequiredControls, ", "), "none"), "controls"))
			if phone.Outbound {
				options = append(options, huh.NewOption("When voicemail answers  ·  "+firstNonempty(phone.OnVoicemail, "hangup"), "voicemail"))
			}
			options = append(options,
				huh.NewOption("Target transport  ·  "+firstNonempty(data.Transport, "not set"), "transport"),
				huh.NewOption("Carrier  ·  "+firstNonempty(data.Carrier, "not set"), "carrier"),
			)
		}
		options = append(options, huh.NewOption("← Back", actionBack))
		choice, _, err := runner.selectOne("Caller channels", runner.describe("This declares web/phone behavior only. Phone numbers, SIP trunks, carriers, and rooms remain external setup."), options, true)
		if err != nil || choice == actionBack {
			return err
		}
		savePhone := func() {
			data.Channels = []scaffold.Channel{{Name: "web", Kind: "realtime_audio"}, phone}
		}
		switch choice {
		case "mode":
			selected, back, err := runner.selectOne("Caller channels", "", []huh.Option[string]{
				huh.NewOption("Web browser audio", "web"), huh.NewOption("Web + inbound phone", "web_phone_inbound"),
				huh.NewOption("Web + outbound phone", "web_phone_outbound"), huh.NewOption("Web + inbound/outbound phone", "web_phone_both"),
				huh.NewOption("← Back", actionBack),
			}, true)
			if err != nil {
				return err
			}
			if back {
				continue
			}
			if selected == "web" {
				data.Channels = []scaffold.Channel{{Name: "web", Kind: "realtime_audio"}}
				continue
			}
			phone.Inbound = strings.Contains(selected, "inbound") || selected == "web_phone_both"
			phone.Outbound = strings.Contains(selected, "outbound") || selected == "web_phone_both"
			if phone.Outbound {
				phone.OnVoicemail = firstNonempty(phone.OnVoicemail, "hangup")
			} else {
				phone.OnVoicemail = ""
			}
			if data.Target == string(targetcap.Pipecat) && data.Transport == "" {
				data.Transport = "daily-sip"
			}
			savePhone()
		case "controls":
			selected, back, err := pickReferences(runner, "Required phone controls (optional)", "Choose every runtime capability this phone channel requires.", []string{"cold_transfer", "warm_transfer", "dtmf_send", "dtmf_receive", "hold", "hangup", "voicemail_detection", "ivr_navigation"}, phone.RequiredControls, true)
			if err != nil {
				return err
			}
			if !back {
				phone.RequiredControls = selected
				savePhone()
			}
		case "voicemail":
			selected, back, err := runner.selectOne("When voicemail answers", "", []huh.Option[string]{huh.NewOption("Hang up", "hangup"), huh.NewOption("Leave a message", "leave_message"), huh.NewOption("← Back", actionBack)}, true)
			if err != nil {
				return err
			}
			if !back {
				phone.OnVoicemail = selected
				savePhone()
			}
		case "transport":
			if _, err := runner.input("Target transport (optional)", "Driver vocabulary; Pipecat cold transfer uses daily-sip.", &data.Transport, validateBasic); err != nil {
				return err
			}
		case "carrier":
			if _, err := runner.input("Carrier (optional)", "Driver vocabulary such as twilio; never provisions the carrier.", &data.Carrier, validateBasic); err != nil {
				return err
			}
		}
	}
}

func editHumanTransfers(runner *fieldRunner, data *scaffold.Data) error {
	if !hasTelephony(data) && len(data.HumanTransfers) == 0 {
		return showNotice(runner, "Human transfers unavailable", "Add a telephony caller channel first.")
	}
	for {
		options := make([]huh.Option[string], 0, len(data.HumanTransfers)+2)
		for _, transfer := range data.HumanTransfers {
			options = append(options, huh.NewOption(fmt.Sprintf("%s  ·  %s  ·  %s", transfer.Name, transfer.Agent, transfer.Destination), "view:"+transfer.Name))
		}
		options = append(options, huh.NewOption("Add human transfer", "add"), huh.NewOption("← Back", actionBack))
		choice, _, err := runner.selectOne("Human transfers", runner.describe(
			"Destinations live in agent.yaml and name environment variables, never numbers. Unmute does not buy numbers, create trunks, or configure carriers.",
		), options, true)
		if err != nil || choice == actionBack {
			return err
		}
		if strings.HasPrefix(choice, "view:") {
			if err := editHumanTransferDetails(runner, data, strings.TrimPrefix(choice, "view:")); err != nil {
				return err
			}
			continue
		}
		if !hasTelephony(data) {
			if err := showNotice(runner, "Cannot add human transfer", "Add a telephony caller channel first. Existing transfers remain available above for repair or deletion."); err != nil {
				return err
			}
			continue
		}
		transfer := scaffold.HumanTransfer{Agent: firstNonempty(data.EntryAgent, "assistant"), Mode: "cold"}
		back, err := runner.input("Control name", "Lowercase snake_case; controls and tools share one namespace.", &transfer.Name, func(value string) error {
			if err := validateIdentifier(value); err != nil {
				return err
			}
			return validateControlName(data, value)
		})
		if err != nil || back {
			continue
		}
		data.HumanTransfers = append(data.HumanTransfers, transfer)
		if err := editHumanTransferDetails(runner, data, transfer.Name); err != nil {
			return err
		}
	}
}

func editHumanTransferDetails(runner *fieldRunner, data *scaffold.Data, name string) error {
	for {
		var transfer *scaffold.HumanTransfer
		for i := range data.HumanTransfers {
			if data.HumanTransfers[i].Name == name {
				transfer = &data.HumanTransfers[i]
				break
			}
		}
		if transfer == nil {
			return nil
		}
		choice, _, err := runner.selectOne(name, "Edit this saved human transfer or remove it.", []huh.Option[string]{
			huh.NewOption("Agent  ·  "+transfer.Agent, "agent"),
			huh.NewOption("Trigger  ·  "+oneLine(transfer.When), "trigger"),
			huh.NewOption("Destination  ·  "+transfer.Destination, "destination"),
			huh.NewOption("Destination value  ·  "+transfer.Value, "value"),
			huh.NewOption("Mode  ·  "+transfer.Mode, "mode"),
			huh.NewOption("Briefing  ·  "+firstNonempty(transfer.Briefing, "none"), "briefing"),
			huh.NewOption("Delete human transfer", "delete"),
			huh.NewOption("← Back", actionBack),
		}, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "agent":
			options := agentOptions(data.AllAgents(), "")
			options = append(options, huh.NewOption("← Back", actionBack))
			selected, back, err := runner.selectOne("Agent allowed to transfer", "", options, true)
			if err != nil {
				return err
			}
			if !back && selected != actionBack {
				transfer.Agent = selected
			}
		case "trigger":
			if _, err := runner.input("When to transfer", "Plain-language trigger shown to the agent.", &transfer.When, validateRequiredText); err != nil {
				return err
			}
		case "destination":
			if _, err := runner.input("Destination name", "Symbolic lowercase snake_case name stored in the portable agent spec.", &transfer.Destination, func(value string) error {
				if err := validateIdentifier(value); err != nil {
					return err
				}
				for _, existing := range data.HumanTransfers {
					if existing.Name != name && existing.Destination == value {
						return errors.New("destination already exists")
					}
				}
				return nil
			}); err != nil {
				return err
			}
		case "value":
			if _, err := runner.input("Destination variable", "Name of an environment variable holding the number, such as SUPPORT_PHONE_NUMBER. The number itself is never written into the package.", &transfer.Value, validateDestination); err != nil {
				return err
			}
		case "mode":
			options := []huh.Option[string]{huh.NewOption("Cold transfer", "cold")}
			if data.Target != string(targetcap.Pipecat) {
				options = append(options, huh.NewOption("Warm transfer", "warm"))
			}
			options = append(options, huh.NewOption("← Back", actionBack))
			selected, back, err := runner.selectOne("Transfer mode", "", options, true)
			if err != nil {
				return err
			}
			if !back && selected != actionBack {
				transfer.Mode = selected
				if selected == "cold" {
					transfer.Briefing = ""
				}
			}
		case "briefing":
			if transfer.Mode != "warm" {
				if err := showNotice(runner, "No operator briefing", "Briefing applies to warm transfers. Change the transfer mode first."); err != nil {
					return err
				}
				continue
			}
			options := []huh.Option[string]{huh.NewOption("Summary", "summary"), huh.NewOption("← Back", actionBack)}
			selected, back, err := runner.selectOne("Operator briefing", "", options, true)
			if err != nil {
				return err
			}
			if !back && selected != actionBack {
				transfer.Briefing = selected
			}
		case "delete":
			confirmed, err := confirmDelete(runner, "human transfer", name)
			if err != nil {
				return err
			}
			if confirmed {
				return deleteResource(data, "human", name)
			}
		}
	}
}

func editCustomize(runner *fieldRunner, data *scaffold.Data) error {
	for {
		choice, _, err := runner.selectOne("Customize", runner.describe("Optional settings stay collapsed here; starter defaults remain valid."), []huh.Option[string]{
			huh.NewOption("Conversation behavior", "conversation"),
			huh.NewOption(fmt.Sprintf("Model fallbacks  ·  %d", len(data.Fallbacks)), "fallbacks"),
			huh.NewOption("Capacity", "capacity"),
			huh.NewOption("Advanced target settings", "target"),
			huh.NewOption("← Back", actionBack),
		}, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "conversation":
			if err := editConversation(runner, data); err != nil {
				return err
			}
		case "fallbacks":
			if err := editFallbacks(runner, data); err != nil {
				return err
			}
		case "capacity":
			if err := editCapacity(runner, data); err != nil {
				return err
			}
		case "target":
			if err := editAdvancedTarget(runner, data); err != nil {
				return err
			}
		}
	}
}

func editConversation(runner *fieldRunner, data *scaffold.Data) error {
	for {
		speaksFirst := firstNonempty(data.SpeaksFirst, "agent")
		opening := "fixed"
		if data.ModelGreeting {
			opening = "model-written"
		}
		interruption := "provider default"
		if data.Interruption != nil {
			interruption = map[bool]string{true: "enabled", false: "disabled"}[*data.Interruption]
		}
		options := []huh.Option[string]{
			huh.NewOption("Who speaks first  ·  "+speaksFirst, "speaker"),
			huh.NewOption("Opening mode  ·  "+opening, "opening"),
		}
		if !data.ModelGreeting {
			options = append(options, huh.NewOption("Fixed greeting  ·  "+oneLine(firstNonempty(data.Greeting, scaffold.DefaultGreeting)), "greeting"))
		}
		options = append(options,
			huh.NewOption("Interruption  ·  "+interruption, "interruption"),
			huh.NewOption(fmt.Sprintf("Minimum interruption words  ·  %d", data.MinimumWords), "minimum"),
			huh.NewOption("Ignored interruption phrases  ·  "+firstNonempty(strings.Join(data.IgnorePhrases, ", "), "none"), "phrases"),
			huh.NewOption("Inactivity nudge after  ·  "+firstNonempty(data.NudgeAfter, "not set"), "nudge"),
			huh.NewOption("Inactivity end after  ·  "+firstNonempty(data.EndAfter, "not set"), "end"),
			huh.NewOption("Maximum call duration  ·  "+firstNonempty(data.MaxDuration, "not set"), "duration"),
			huh.NewOption("Thinking audio  ·  "+firstNonempty(data.ThinkingAudio, "none"), "thinking"),
			huh.NewOption("← Back", actionBack),
		)
		choice, _, err := runner.selectOne("Conversation behavior", "Edit one field, then return here.", options, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "speaker":
			selected, back, err := runner.selectOne("Who speaks first", "", []huh.Option[string]{huh.NewOption("Agent", "agent"), huh.NewOption("Caller", "user"), huh.NewOption("← Back", actionBack)}, true)
			if err != nil {
				return err
			}
			if !back {
				data.SpeaksFirst = selected
			}
		case "opening":
			selected, back, err := runner.selectOne("Opening", "", []huh.Option[string]{huh.NewOption("Fixed greeting", "fixed"), huh.NewOption("Model-written greeting", "model"), huh.NewOption("← Back", actionBack)}, true)
			if err != nil {
				return err
			}
			if !back {
				data.ModelGreeting = selected == "model"
				if data.ModelGreeting {
					data.Greeting = ""
				} else {
					data.Greeting = firstNonempty(data.Greeting, scaffold.DefaultGreeting)
				}
			}
		case "greeting":
			if _, err := runner.input("Fixed greeting", "Spoken verbatim.", &data.Greeting, validateRequiredText); err != nil {
				return err
			}
		case "interruption":
			selected, back, err := runner.selectOne("Interruption", "", []huh.Option[string]{huh.NewOption("Provider default", "default"), huh.NewOption("Enabled", "enabled"), huh.NewOption("Disabled", "disabled"), huh.NewOption("← Back", actionBack)}, true)
			if err != nil {
				return err
			}
			if !back {
				data.Interruption = nil
				if selected != "default" {
					enabled := selected == "enabled"
					data.Interruption = &enabled
				}
			}
		case "minimum":
			value := strconv.Itoa(data.MinimumWords)
			if data.MinimumWords == 0 {
				value = ""
			}
			back, err := runner.input("Minimum interruption words (optional)", "Non-negative integer; provider support varies.", &value, validateOptionalNonNegativeInteger)
			if err != nil {
				return err
			}
			if !back {
				data.MinimumWords = 0
				if value != "" {
					data.MinimumWords, _ = strconv.Atoi(value)
				}
			}
		case "phrases":
			value := strings.Join(data.IgnorePhrases, ", ")
			back, err := runner.input("Ignored interruption phrases (optional)", "Comma-separated phrases.", &value, func(string) error { return nil })
			if err != nil {
				return err
			}
			if !back {
				data.IgnorePhrases = parsePhrases(value)
			}
		case "nudge", "end", "duration":
			field := map[string]*string{"nudge": &data.NudgeAfter, "end": &data.EndAfter, "duration": &data.MaxDuration}[choice]
			title := map[string]string{"nudge": "Inactivity nudge after (optional)", "end": "Inactivity end after (optional)", "duration": "Maximum call duration (optional)"}[choice]
			if _, err := runner.input(title, "Go duration such as 30s or 20m.", field, validateOptionalDuration); err != nil {
				return err
			}
		case "thinking":
			selected, back, err := runner.selectOne("Thinking audio", "", []huh.Option[string]{huh.NewOption("None", "none"), huh.NewOption("Subtle", "subtle"), huh.NewOption("← Back", actionBack)}, true)
			if err != nil {
				return err
			}
			if !back {
				data.ThinkingAudio = strings.TrimPrefix(selected, "none")
			}
		}
	}
}

func editFallbacks(runner *fieldRunner, data *scaffold.Data) error {
	for {
		options := make([]huh.Option[string], 0, len(data.Fallbacks)+2)
		for _, fallback := range data.Fallbacks {
			options = append(options, huh.NewOption(fallback.Name+"  ·  protects "+fallback.Profile, "view:"+fallback.Name))
		}
		options = append(options, huh.NewOption("Add fallback", "add"), huh.NewOption("← Back", actionBack))
		choice, _, err := runner.selectOne("Model fallbacks", runner.describe("Fallback support is target-gated and checked by preflight."), options, true)
		if err != nil || choice == actionBack {
			return err
		}
		if strings.HasPrefix(choice, "view:") {
			if err := editFallbackDetails(runner, data, strings.TrimPrefix(choice, "view:")); err != nil {
				return err
			}
			continue
		}
		fallback := scaffold.ModelFallback{Profile: data.AllAgents()[0].ModelProfile(), Binding: data.Reason}
		back, err := runner.input("Fallback profile name", "Lowercase snake_case.", &fallback.Name, func(value string) error {
			if err := validateIdentifier(value); err != nil {
				return err
			}
			for _, agent := range data.AllAgents() {
				if agent.ModelProfile() == value {
					return errors.New("model profile already exists")
				}
			}
			for _, existing := range data.Fallbacks {
				if existing.Name == value {
					return errors.New("fallback profile already exists")
				}
			}
			return nil
		})
		if err != nil || back {
			continue
		}
		data.Fallbacks = append(data.Fallbacks, fallback)
		if err := editFallbackDetails(runner, data, fallback.Name); err != nil {
			return err
		}
	}
}

func editFallbackDetails(runner *fieldRunner, data *scaffold.Data, name string) error {
	for {
		var fallback *scaffold.ModelFallback
		for i := range data.Fallbacks {
			if data.Fallbacks[i].Name == name {
				fallback = &data.Fallbacks[i]
				break
			}
		}
		if fallback == nil {
			return nil
		}
		choice, _, err := runner.selectOne(name, "Edit this saved fallback or remove it before retrying preflight.", []huh.Option[string]{
			huh.NewOption("Protected model  ·  "+fallback.Profile, "profile"),
			huh.NewOption("Fallback model  ·  "+bindingLabel(fallback.Binding), "binding"),
			huh.NewOption("Delete fallback", "delete"),
			huh.NewOption("← Back", actionBack),
		}, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "profile":
			profiles := make([]huh.Option[string], 0, len(data.AllAgents())+1)
			for _, agent := range data.AllAgents() {
				profiles = append(profiles, huh.NewOption(agent.ModelProfile(), agent.ModelProfile()))
			}
			profiles = append(profiles, huh.NewOption("← Back", actionBack))
			selected, back, err := runner.selectOne("Model profile to protect", "", profiles, true)
			if err != nil {
				return err
			}
			if !back && selected != actionBack {
				fallback.Profile = selected
			}
		case "binding":
			if err := editBindingFor(runner, data.Target, targetcap.Reason, &fallback.Binding); err != nil {
				return err
			}
		case "delete":
			confirmed, err := confirmDelete(runner, "fallback", name)
			if err != nil {
				return err
			}
			if confirmed {
				return deleteResource(data, "fallback", name)
			}
		}
	}
}

func editCapacity(runner *fieldRunner, data *scaffold.Data) error {
	for {
		capacity := data.EffectiveCapacity()
		choice, _, err := runner.selectOne("Capacity", "Edit one field, then return here.", []huh.Option[string]{
			huh.NewOption(fmt.Sprintf("Peak sessions  ·  %d", capacity.PeakSessions), "peak"),
			huh.NewOption(fmt.Sprintf("Maximum sessions  ·  %d", capacity.MaxSessions), "max"),
			huh.NewOption("Average session duration  ·  "+capacity.AvgSessionDuration, "duration"),
			huh.NewOption("← Back", actionBack),
		}, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "peak":
			value := strconv.Itoa(capacity.PeakSessions)
			back, err := runner.input("Peak sessions", "Positive concurrent conversations at busy hour.", &value, func(value string) error {
				if err := validatePositiveInteger(value); err != nil {
					return err
				}
				number, _ := strconv.Atoi(value)
				if number > capacity.MaxSessions {
					return errors.New("peak sessions must not exceed maximum sessions")
				}
				return nil
			})
			if err != nil {
				return err
			}
			if !back {
				capacity.PeakSessions, _ = strconv.Atoi(value)
				data.Capacity = capacity
			}
		case "max":
			value := strconv.Itoa(capacity.MaxSessions)
			back, err := runner.input("Maximum sessions", "Positive hard admission limit; must be at least peak.", &value, func(value string) error {
				if err := validatePositiveInteger(value); err != nil {
					return err
				}
				number, _ := strconv.Atoi(value)
				if number < capacity.PeakSessions {
					return errors.New("maximum sessions must be at least peak sessions")
				}
				return nil
			})
			if err != nil {
				return err
			}
			if !back {
				capacity.MaxSessions, _ = strconv.Atoi(value)
				data.Capacity = capacity
			}
		case "duration":
			value := capacity.AvgSessionDuration
			back, err := runner.input("Average session duration", "Positive Go duration such as 5m.", &value, validateDuration)
			if err != nil {
				return err
			}
			if !back {
				capacity.AvgSessionDuration = value
				data.Capacity = capacity
			}
		}
	}
}

func editAdvancedTarget(runner *fieldRunner, data *scaffold.Data) error {
	for {
		// deployment_region holds one region or several (N32), so it is one
		// comma-separated field: joined for display, split on save. Without the
		// save hook a multi-region package would lose every region but the
		// first the moment someone opened this form to edit something else.
		regions := strings.Join(data.DeploymentRegions, ", ")
		fields := []struct {
			title, help string
			value       *string
			validate    func(string) error
			save        func(string)
		}{
			{"Target version", "Driver/framework version pin.", &data.TargetVersion, validateBasic, nil},
			{"SDK language (optional)", "For example python on LiveKit.", &data.SDKLanguage, validateBasic, nil},
			{"Deployment region (optional)", "Where the platform deploys the agent; forwarded as declared. One region, or several separated by commas for one deployment per region (LiveKit only).", &regions, validateBasic, func(value string) {
				data.DeploymentRegions = parsePhrases(value)
			}},
			{"Pins (optional JSON object)", "Independently versioned target packages.", &data.Pins, validateParams, nil},
		}
		options := make([]huh.Option[string], 0, len(fields)+1)
		for i, field := range fields {
			options = append(options, huh.NewOption(field.title+"  ·  "+firstNonempty(*field.value, "not set"), strconv.Itoa(i)))
		}
		options = append(options, huh.NewOption("← Back", actionBack))
		choice, _, err := runner.selectOne("Advanced target settings", "Edit one field, then return here.", options, true)
		if err != nil || choice == actionBack {
			return err
		}
		index, _ := strconv.Atoi(choice)
		field := fields[index]
		if _, err := runner.input(field.title, field.help, field.value, field.validate); err != nil {
			return err
		}
		if field.save != nil {
			field.save(*field.value)
		}
	}
}

func parsePhrases(value string) []string {
	var phrases []string
	for _, phrase := range strings.Split(value, ",") {
		if phrase = strings.TrimSpace(phrase); phrase != "" {
			phrases = append(phrases, phrase)
		}
	}
	return phrases
}

func validateOptionalNonNegativeInteger(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 {
		return errors.New("value must be a non-negative integer")
	}
	return nil
}

func validateOptionalDuration(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return validateDuration(value)
}

func validateDuration(value string) error {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return errors.New("value must be a positive Go duration")
	}
	return nil
}

func hasTelephony(data *scaffold.Data) bool {
	for _, channel := range data.AllChannels() {
		if channel.Kind == "telephony" {
			return true
		}
	}
	return false
}

// A destination names an environment variable holding the number, never the
// number itself: agent.yaml is the portable half of a package, and a literal is
// refused at compile time (spec FR-004d).
var destinationPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func validateDestination(value string) error {
	if !destinationPattern.MatchString(value) {
		return errors.New("destination must be the UPPER_SNAKE name of an environment variable holding the number, such as SUPPORT_PHONE_NUMBER")
	}
	return nil
}

func validateControlName(data *scaffold.Data, name string) error {
	for _, tool := range data.Tools {
		if tool.Name == name {
			return errors.New("control name already used by a tool")
		}
	}
	for _, handoff := range data.Handoffs {
		if handoff.Name == name {
			return errors.New("control name already used by a handoff")
		}
	}
	for _, task := range data.Tasks {
		if task.RunName() == name {
			return errors.New("control name already used by a task")
		}
	}
	for _, group := range data.TaskGroups {
		if group.RunName() == name {
			return errors.New("control name already used by a task group")
		}
	}
	for _, transfer := range data.HumanTransfers {
		if transfer.Name == name {
			return errors.New("control name already used by a human transfer")
		}
	}
	return nil
}

func toolNames(data *scaffold.Data) []string {
	names := make([]string, 0, len(data.Tools))
	for _, tool := range data.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func taskNames(data *scaffold.Data) []string {
	names := make([]string, 0, len(data.Tasks))
	for _, task := range data.Tasks {
		names = append(names, task.Name)
	}
	return names
}

func validateTaskResult(value string) error {
	var result map[string]any
	if err := json.Unmarshal([]byte(value), &result); err != nil || len(result) == 0 {
		return errors.New("result must be a nonempty JSON object")
	}
	for name, field := range result {
		if err := validateIdentifier(name); err != nil {
			return fmt.Errorf("result field %q: %w", name, err)
		}
		switch typed := field.(type) {
		case string:
			if typed != "string" && typed != "number" && typed != "boolean" && typed != "integer" {
				return fmt.Errorf("result field %q has unknown type %q", name, typed)
			}
		case map[string]any:
			values, ok := typed["enum"].([]any)
			if !ok || len(typed) != 1 || len(values) == 0 {
				return fmt.Errorf("result field %q must be a primitive type or nonempty enum", name)
			}
			for _, value := range values {
				if _, ok := value.(string); !ok {
					return fmt.Errorf("result field %q enum values must be strings", name)
				}
			}
		default:
			return fmt.Errorf("result field %q must be a primitive type or enum", name)
		}
	}
	return nil
}

func agentOptions(agents []scaffold.Agent, except string) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(agents))
	for _, agent := range agents {
		if agent.Name != except {
			options = append(options, huh.NewOption(agent.Name, agent.Name))
		}
	}
	return options
}

func agentNames(data *scaffold.Data) []string {
	names := make([]string, 0, len(data.AllAgents()))
	for _, agent := range data.AllAgents() {
		names = append(names, agent.Name)
	}
	return names
}

func pickReferences(runner *fieldRunner, title, description string, available, current []string, allowEmpty bool) ([]string, bool, error) {
	original := append([]string(nil), current...)
	selected := append([]string(nil), current...)
	for {
		options := make([]huh.Option[string], 0, len(available)+2)
		for _, name := range available {
			mark := "[ ]"
			if containsName(selected, name) {
				mark = "[x]"
			}
			options = append(options, huh.NewOption(mark+" "+name, "toggle:"+name))
		}
		options = append(options, huh.NewOption("Done", "done"), huh.NewOption("← Back", actionBack))
		choice, back, err := runner.selectOne(title, runner.describe(description+" Toggle choices, then select Done."), options, true)
		if err != nil {
			return original, false, err
		}
		if back || choice == actionBack {
			return original, true, nil
		}
		if choice == "done" {
			if !allowEmpty && len(selected) == 0 {
				if err := showNotice(runner, "Selection required", "Select at least one item, or choose Back."); err != nil {
					return original, false, err
				}
				continue
			}
			return selected, false, nil
		}
		name := strings.TrimPrefix(choice, "toggle:")
		if containsName(selected, name) {
			selected = removeName(selected, name)
		} else {
			selected = append(selected, name)
		}
	}
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func removeName(names []string, remove string) []string {
	kept := names[:0]
	for _, name := range names {
		if name != remove {
			kept = append(kept, name)
		}
	}
	return kept
}

func confirmDelete(runner *fieldRunner, kind, name string) (bool, error) {
	return confirmAction(runner, "Delete "+kind+" "+name+"?", "Delete")
}

func confirmAction(runner *fieldRunner, title, action string) (bool, error) {
	return confirmChoice(runner, title, action)
}

func confirmChoice(runner *fieldRunner, title, action string) (bool, error) {
	choice, back, err := runner.selectOne(title, "Choose Back to return without applying this action.", []huh.Option[string]{
		huh.NewOption(action, "confirm"),
		huh.NewOption("← Back", actionBack),
	}, true)
	return err == nil && !back && choice == "confirm", err
}

// deleteResource removes one saved resource and every reference that would
// otherwise dangle from the generated spec.
func deleteResource(data *scaffold.Data, kind, name string) error {
	switch kind {
	case "variable":
		data.Variables = slices.DeleteFunc(data.Variables, func(item scaffold.Variable) bool { return item.Name == name })
		for i := range data.Handoffs {
			data.Handoffs[i].Requires = removeName(data.Handoffs[i].Requires, name)
			data.Handoffs[i].Variables = removeName(data.Handoffs[i].Variables, name)
		}
		for i := range data.Tasks {
			removeAssignment(&data.Tasks[i], name)
		}
	case "tool":
		data.Tools = slices.DeleteFunc(data.Tools, func(item scaffold.Tool) bool { return item.Name == name })
		for i := range data.Tasks {
			data.Tasks[i].Tools = removeName(data.Tasks[i].Tools, name)
		}
	case "agent":
		if name == "assistant" {
			return errors.New("the starter agent cannot be deleted; reset it instead")
		}
		data.Agents = slices.DeleteFunc(data.Agents, func(item scaffold.Agent) bool { return item.Name == name })
		if data.EntryAgent == name {
			data.EntryAgent = "assistant"
		}
		data.Handoffs = slices.DeleteFunc(data.Handoffs, func(item scaffold.Handoff) bool {
			return item.Source == name || item.To == name
		})
		profile := name + "_model"
		data.Fallbacks = slices.DeleteFunc(data.Fallbacks, func(item scaffold.ModelFallback) bool { return item.Profile == profile })
		for i := range data.Tools {
			if containsName(data.Tools[i].AttachTo, name) {
				data.Tools[i].AttachTo = removeName(data.Tools[i].AttachTo, name)
				if !containsName(data.Tools[i].AttachTo, "assistant") {
					data.Tools[i].AttachTo = append(data.Tools[i].AttachTo, "assistant")
				}
			}
		}
		for i := range data.Tasks {
			if data.Tasks[i].Agent == name {
				data.Tasks[i].Agent = "assistant"
			}
			if data.Tasks[i].Model == profile {
				data.Tasks[i].Model = ""
			}
			if data.Tasks[i].Summarizer == profile {
				data.Tasks[i].Summarizer = ""
			}
		}
		for i := range data.Handoffs {
			if data.Handoffs[i].Summarizer == profile {
				data.Handoffs[i].History = "full"
				data.Handoffs[i].Summarizer = ""
			}
		}
		for i := range data.TaskGroups {
			if data.TaskGroups[i].Agent == name {
				data.TaskGroups[i].Agent = "assistant"
			}
			if data.TaskGroups[i].ThenTarget == name {
				data.TaskGroups[i].Then = "return"
				data.TaskGroups[i].ThenTarget = ""
			}
		}
		for i := range data.HumanTransfers {
			if data.HumanTransfers[i].Agent == name {
				data.HumanTransfers[i].Agent = "assistant"
			}
		}
	case "handoff":
		data.Handoffs = slices.DeleteFunc(data.Handoffs, func(item scaffold.Handoff) bool { return item.Name == name })
	case "task":
		data.Tasks = slices.DeleteFunc(data.Tasks, func(item scaffold.Task) bool { return item.Name == name })
		for i := range data.Tools {
			data.Tools[i].AttachTasks = removeName(data.Tools[i].AttachTasks, name)
		}
		for i := range data.TaskGroups {
			data.TaskGroups[i].Steps = removeName(data.TaskGroups[i].Steps, name)
		}
		data.TaskGroups = slices.DeleteFunc(data.TaskGroups, func(item scaffold.TaskGroup) bool { return len(item.Steps) == 0 })
	case "group":
		data.TaskGroups = slices.DeleteFunc(data.TaskGroups, func(item scaffold.TaskGroup) bool { return item.Name == name })
	case "human":
		data.HumanTransfers = slices.DeleteFunc(data.HumanTransfers, func(item scaffold.HumanTransfer) bool { return item.Name == name })
	case "fallback":
		data.Fallbacks = slices.DeleteFunc(data.Fallbacks, func(item scaffold.ModelFallback) bool { return item.Name == name })
	default:
		return fmt.Errorf("unknown resource kind %q", kind)
	}
	return nil
}

func removeAssignment(task *scaffold.Task, variable string) {
	if task.Assign == "" {
		return
	}
	var assignments map[string]any
	if json.Unmarshal([]byte(task.Assign), &assignments) != nil {
		return
	}
	delete(assignments, variable)
	if len(assignments) == 0 {
		task.Assign = ""
		return
	}
	encoded, err := json.Marshal(assignments)
	if err == nil {
		task.Assign = string(encoded)
	}
}

func oneLine(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const limit = 56
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return value
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func variableNames(data *scaffold.Data) []string {
	names := make([]string, 0, len(data.Variables))
	for _, variable := range data.Variables {
		names = append(names, variable.Name)
	}
	return names
}

func validatePositiveInteger(value string) error {
	number, err := strconv.Atoi(value)
	if err != nil || number <= 0 {
		return errors.New("value must be a positive integer")
	}
	return nil
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

func validateRequiredBasic(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("value is required")
	}
	return validateBasic(value)
}

var (
	languagePattern   = regexp.MustCompile(`^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{1,8})*$`)
	identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
	envNamePattern    = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

func validateLanguage(value string) error {
	if !languagePattern.MatchString(value) {
		return errors.New("language must be a BCP-47 tag such as en or en-US")
	}
	return nil
}

func validateIdentifier(value string) error {
	if !identifierPattern.MatchString(value) {
		return errors.New("name must be lowercase snake_case")
	}
	return nil
}

func validateRequiredText(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("value is required")
	}
	return nil
}

func validateEnvName(value string) error {
	if !envNamePattern.MatchString(value) {
		return errors.New("value must be an environment variable name")
	}
	return nil
}

func validateRequiredObject(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("JSON object is required")
	}
	return validateParams(value)
}

func validateVariableDefault(variableType, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return errors.New("default must be a JSON value")
	}
	valid := false
	switch variableType {
	case "string":
		_, valid = decoded.(string)
	case "boolean":
		_, valid = decoded.(bool)
	case "number":
		_, valid = decoded.(float64)
	case "integer":
		number, ok := decoded.(float64)
		valid = ok && number == float64(int64(number))
	}
	if !valid {
		return fmt.Errorf("default must match type %s", variableType)
	}
	return nil
}

func validateParams(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(value), &object); err != nil || object == nil {
		return errors.New("params must be a JSON object")
	}
	return nil
}

type fieldRunner struct {
	in         io.Reader
	out        io.Writer
	accessible bool
	tracked    *eofReader
	requests   chan shellRequest
	actions    ActionHandler
	ctx        viewCtx // interactive chrome context (breadcrumb, target, sidebar)
}

func newRunner(in io.Reader, out io.Writer, accessible bool) *fieldRunner {
	runner := &fieldRunner{in: in, out: out, accessible: accessible}
	if accessible {
		runner.tracked = &eofReader{Reader: in}
		runner.in = runner.tracked
	}
	return runner
}

// run drives one huh field for the accessible/headless path only (C5). The
// interactive path never calls it; it uses sendField (shell.go) instead.
func (r *fieldRunner) run(field huh.Field, backable bool) (bool, error) {
	form := huh.NewForm(huh.NewGroup(field)).WithAccessible(true)
	err := r.runAccessible(form)
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

func (r *fieldRunner) runAccessible(form *huh.Form) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if r.tracked != nil && r.tracked.eof {
				err = huh.ErrUserAborted
				return
			}
			panic(recovered)
		}
	}()
	return form.WithInput(r.in).WithOutput(r.out).Run()
}

func (r *fieldRunner) selectOne(title, description string, options []huh.Option[string], backable bool) (string, bool, error) {
	if len(options) == 0 {
		return "", false, errors.New("menu has no actions")
	}
	if backable {
		if len(options) < 2 {
			return "", false, errors.New("menu requires an action before Back")
		}
		if options[len(options)-1].Value != actionBack {
			return "", false, errors.New("menu Back action must be last")
		}
		for _, option := range options[:len(options)-1] {
			if option.Value == actionBack {
				return "", false, errors.New("menu Back action may appear only once")
			}
		}
	}
	if !r.accessible {
		reply := r.sendField(fieldReq{
			kind:     kindSelect,
			title:    title,
			desc:     description,
			choices:  toChoices(options),
			initial:  options[0].Value,
			backable: backable,
			ctx:      r.ctx,
		})
		if reply.back {
			return actionBack, true, nil
		}
		return reply.value, false, nil
	}
	choice := options[0].Value
	field := huh.NewSelect[string]().Title(title).Description(description).Options(options...).Value(&choice)
	back, err := r.run(field, backable)
	if err != nil || back {
		return actionBack, back, err
	}
	return choice, false, nil
}

// toChoices converts huh options (built by the flow) into the neutral choice
// pairs the interactive model renders.
func toChoices(options []huh.Option[string]) []choice {
	out := make([]choice, len(options))
	for i, o := range options {
		out[i] = choice{label: o.Key, value: o.Value}
	}
	return out
}

func (r *fieldRunner) input(title, description string, value *string, validate func(string) error) (bool, error) {
	if !r.accessible {
		reply := r.sendField(fieldReq{kind: kindInput, title: title, desc: description, initial: *value, backable: true, validate: validate, ctx: r.ctx})
		if reply.back {
			return true, nil
		}
		*value = reply.value
		return false, nil
	}
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
	if !r.accessible {
		reply := r.sendField(fieldReq{kind: kindText, title: title, desc: description, initial: *value, backable: true, ctx: r.ctx})
		if reply.back {
			return true, nil
		}
		*value = reply.value
		return false, nil
	}
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
