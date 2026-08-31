// Package tui gathers the basic configuration for a new v1 agent scaffold.
package tui

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/slng-ai/unmute/internal/scaffold"
	targetcap "github.com/slng-ai/unmute/internal/target"
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
		choice, _, err := runner.selectOne(homeTitle(), "What would you like to do?", []menuChoice{
			newChoice("Create a new agent", actionCreate),
			newChoice("Open an existing agent", actionOpen),
			newChoice("Quit", actionQuit),
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
func homeTitle() string { return "UNMUTE//" }

// sidebarSections is the fixed five-section tree shown in the console sidebar.
// Order matches editorSectionOptions.
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
		return []string{"Tools", "Channels", "Escalations"}
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
			newChoice("Compile after create  ·  "+compile, "compile"),
			newChoice("Create agent", "save"),
			newChoice("← Back", actionBack),
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
			selected, back, err := runner.selectOne("Target / orchestrator", runner.describe("LiveKit and Pipecat both emit a runnable project."), createTargetOptions(), true)
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

func editorSectionOptions(data scaffold.Data) []menuChoice {
	return []menuChoice{
		newChoice("Identity  ·  target", "section:identity"),
		newChoice("Models  ·  "+modelsLabel(data), "section:models"),
		newChoice("Behavior  ·  instructions, greeting, variables, advanced", "section:behavior"),
		newChoice("Integrations  ·  tools, channels, escalations", "section:integrations"),
		newChoice("Lifecycle  ·  agents, handoffs, tasks, groups", "section:lifecycle"),
	}
}

func chooseEditorSection(runner *fieldRunner, data *scaffold.Data, section string) (string, error) {
	var options []menuChoice
	switch section {
	case "identity":
		options = []menuChoice{
			newChoice("Target  ·  "+targetLabel(data.Target), "target"),
		}
	case "behavior":
		options = []menuChoice{
			newChoice("Instructions (prompt)", "prompt"),
			newChoice("Greeting  ·  "+data.Greeting, "greeting"),
			newChoice(fmt.Sprintf("Variables  ·  %d", len(data.Variables)), "variables"),
			newChoice("Advanced  ·  conversation, fallback, capacity, target", "customize"),
		}
	case "integrations":
		options = []menuChoice{
			newChoice(fmt.Sprintf("Tools  ·  %d", len(data.Tools)), "tools"),
			newChoice("Caller channels  ·  "+channelsLabel(*data), "channels"),
			newChoice(fmt.Sprintf("Escalations  ·  %d", len(data.HumanTransfers)), "humans"),
		}
	case "lifecycle":
		options = []menuChoice{
			newChoice(fmt.Sprintf("Agents  ·  %d", len(data.AllAgents())), "agents"),
			newChoice(fmt.Sprintf("Handoffs  ·  %d", len(data.Handoffs)), "handoffs"),
			newChoice(fmt.Sprintf("Tasks  ·  %d", len(data.Tasks)), "tasks"),
			newChoice(fmt.Sprintf("Task groups  ·  %d", len(data.TaskGroups)), "groups"),
		}
	default:
		return "", fmt.Errorf("unknown editor section %q", section)
	}
	options = append(options, newChoice("← Back", actionBack))
	choice, _, err := runner.selectOne(strings.ToUpper(section[:1])+section[1:], "", options, true)
	return choice, err
}

func repairPreflight(runner *fieldRunner, data *scaffold.Data, preflightErr error) error {
	for {
		choice, _, err := runner.selectOne("Cannot create agent", runner.describe("Fix the configuration, then go Back to continue editing.\n\n"+preflightErr.Error()), []menuChoice{
			newChoice(fmt.Sprintf("Review / delete model fallbacks  ·  %d", len(data.Fallbacks)), "fallbacks"),
			newChoice("Models  ·  "+modelsLabel(*data), "models"),
			newChoice(fmt.Sprintf("Agents  ·  %d", len(data.AllAgents())), "agents"),
			newChoice(fmt.Sprintf("Handoffs  ·  %d", len(data.Handoffs)), "handoffs"),
			newChoice(fmt.Sprintf("Tasks  ·  %d", len(data.Tasks)), "tasks"),
			newChoice(fmt.Sprintf("Task groups  ·  %d", len(data.TaskGroups)), "groups"),
			newChoice(fmt.Sprintf("Tools  ·  %d", len(data.Tools)), "tools"),
			newChoice(fmt.Sprintf("Variables  ·  %d", len(data.Variables)), "variables"),
			newChoice(fmt.Sprintf("Escalations  ·  %d", len(data.HumanTransfers)), "humans"),
			newChoice("← Back", actionBack),
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
			choices:  []menuChoice{{label: "← Back", value: actionBack}},
			initial:  actionBack,
			backable: true,
			ctx:      runner.ctx,
		})
		return nil
	}
	_, _, err := runner.selectOne(title, runner.describe(message),
		[]menuChoice{newChoice("← Back", actionBack)}, false)
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
		if voice := cmp.Or(binding.Binding.Voice, binding.Binding.VoiceID); voice != "" {
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
			cmp.Or(result.Agent.Data.Transport, "provider default"), cmp.Or(result.Agent.Data.Carrier, "provider default"))
	}
	fmt.Fprintf(&text, "\nCompile: %s", compile)
	return text.String()
}

// orderTargets lists every shipped target with `first` at the head.
// selectOne preselects options[0] positionally and never reads the current
// value, so head position *is* the preselect. One home for that ordering rule:
// the create flow passes the scaffold default, the maintain flow passes the
// package's own target, and neither restates the other's choice.
//
// It reads targetcap.Providers rather than naming the targets, because it used
// to name them: an if/else returned [livekit, pipecat] for LiveKit and
// [pipecat, livekit] for everything else, which was correct while "everything
// else" meant Pipecat. The day a third target shipped, that else branch would
// have dropped it from both menus *and* preselected Pipecat for a package that
// had chosen it, so one Enter rewrote the author's target — the exact failure
// the comment below this one warns about.
func orderTargets(first string) []string {
	ordered := make([]string, 0, len(targetcap.Providers))
	known := false
	for _, provider := range targetcap.Providers {
		value := string(provider)
		if value == first {
			known = true
			continue
		}
		ordered = append(ordered, value)
	}
	if !known {
		// An unrecognised value preselects nothing rather than promoting itself
		// into a menu of real targets.
		return ordered
	}
	return append([]string{first}, ordered...)
}

// createTargetOptions is the new-package menu: a fresh package should start on
// whatever the scaffold writes by default.
func createTargetOptions() []menuChoice {
	return targetMenu(orderTargets(scaffold.DefaultTarget), map[string]string{
		string(targetcap.Pipecat): "Pipecat  ·  generated code project",
		string(targetcap.LiveKit): "LiveKit  ·  generated code project",
		string(targetcap.Slng):    "SLNG  ·  hosted, deployment body only",
	})
}

// maintainTargetOptions is the existing-package menu. It leads with the
// package's own target, never the scaffold default: the author already chose,
// and offering to switch them by default is how you silently rewrite a
// Pipecat package into a LiveKit one.
func maintainTargetOptions(current string) []menuChoice {
	return targetMenu(orderTargets(current), map[string]string{
		string(targetcap.Pipecat): "Pipecat",
		string(targetcap.LiveKit): "LiveKit",
		string(targetcap.Slng):    "SLNG",
	})
}

// targetMenu falls back to the short label when a menu forgets a target, so a
// missing row reads as its own name rather than as another target's.
func targetMenu(ordered []string, labels map[string]string) []menuChoice {
	options := make([]menuChoice, 0, len(ordered)+1)
	for _, value := range ordered {
		options = append(options, newChoice(cmp.Or(labels[value], targetLabel(value)), value))
	}
	return append(options, newChoice("← Back", actionBack))
}

// targetLabel names a target on every screen: the header, the Identity row, the
// completion summary and both tool-gating messages. It used to end in
// `default: return "Pipecat"`, which meant a slng package was labelled Pipecat
// everywhere at once, on a green test run. An unknown value now reads as itself,
// because showing a raw provider string is a smaller lie than showing a
// different target's name.
func targetLabel(provider string) string {
	switch targetcap.Provider(provider) {
	case targetcap.LiveKit:
		return "LiveKit"
	case targetcap.Pipecat:
		return "Pipecat"
	case targetcap.Slng:
		return "SLNG"
	}
	return provider
}

// offersWarmTransfer asks the capability table the question the transfer-mode
// menu needs answered. The table is the one owner of it, and the console reading
// the table is what keeps the menu and `validate` from disagreeing.
//
// Transport and carrier are empty because the console has not chosen a
// connection at this point. That costs nothing today: no warm row conditions on
// a route, so every one of them answers before the condition is reached.
func offersWarmTransfer(provider string) bool {
	capability := targetcap.Default().Control(targetcap.WarmTransfer, targetcap.Provider(provider), "", "")
	return capability.Tag != targetcap.Gated && capability.Tag != targetcap.Provisional
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
		choice, _, err := runner.selectOne("STT / LLM / TTS", runner.describe("Providers come from the selected target's catalogue. Model ids, voices, and params are forwarded as entered."), []menuChoice{
			newChoice("Listen (STT)  ·  "+modelsLabelPart(data.Target, targetcap.Listen, data.Listen), string(targetcap.Listen)),
			newChoice("Reason (LLM)  ·  "+modelsLabelPart(data.Target, targetcap.Reason, data.Reason), string(targetcap.Reason)),
			newChoice("Speak (TTS)  ·  "+modelsLabelPart(data.Target, targetcap.Speak, data.Speak), string(targetcap.Speak)),
			newChoice("Reset default models", "reset"),
			newChoice("← Back", actionBack),
		}, true)
		if err != nil {
			return err
		}
		if choice == actionBack {
			return nil
		}
		if choice == "reset" {
			confirmed, err := confirmChoice(runner, "Reset default models?", "Reset models")
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

		options := []menuChoice{newChoice("Provider  ·  "+brand, "provider")}
		if len(distributors) > 1 {
			options = append(options, newChoice("Distributor  ·  "+cmp.Or(binding.Provider, distributors[0]), "distributor"))
		}
		if modelApplicable {
			options = append(options, newChoice("Model  ·  "+cmp.Or(binding.Model, "not set"), "model"))
		}
		if voiceApplicable {
			options = append(options, newChoice("Voice  ·  "+cmp.Or(binding.Voice, "not set"), "voice"))
		}
		if languageApplicable {
			options = append(options, newChoice("Language  ·  "+cmp.Or(binding.Language, "provider default"), "language"))
		}
		options = append(options,
			newChoice("Additional config  ·  "+cmp.Or(binding.Params, "none"), "params"),
			newChoice("← Back", actionBack),
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
			providerChoices = append(providerChoices, newChoice("← Back", actionBack))
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
			routeOptions := make([]menuChoice, 0, len(distributors)+1)
			for _, route := range distributors {
				routeOptions = append(routeOptions, newChoice(route, route))
			}
			routeOptions = append(routeOptions, newChoice("← Back", actionBack))
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

func providerOptions(framework targetcap.Provider, role targetcap.Role) []menuChoice {
	brands := targetcap.DefaultCatalog().Brands(framework, role)
	options := make([]menuChoice, 0, len(brands))
	for _, brand := range brands {
		options = append(options, newChoice(brand, brand))
	}
	return options
}

func editVariables(runner *fieldRunner, data *scaffold.Data) error {
	for {
		options := make([]menuChoice, 0, len(data.Variables)+2)
		for _, variable := range data.Variables {
			options = append(options, newChoice(variableLabel(variable), "edit:"+variable.Name))
		}
		options = append(options, newChoice("Add variable", "add"), newChoice("← Back", actionBack))
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
	source := cmp.Or(variable.Source, "session state")
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
		source := cmp.Or(variable.Source, "session state")
		choice, _, err := runner.selectOne(variable.Name, "Edit this saved variable. Its name stays stable so existing references do not break.", []menuChoice{
			newChoice("Type  ·  "+variable.Type, "type"),
			newChoice("Default  ·  "+cmp.Or(variable.Default, "none"), "default"),
			newChoice("Source  ·  "+strings.ReplaceAll(source, "_", " "), "source"),
			newChoice("Delete variable", "delete"),
			newChoice("← Back", actionBack),
		}, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "type":
			selected, back, err := runner.selectOne("Variable type", "", []menuChoice{
				newChoice("string", "string"), newChoice("number", "number"),
				newChoice("boolean", "boolean"), newChoice("integer", "integer"),
				newChoice("← Back", actionBack),
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
			selected, back, err := runner.selectOne("Value source", "", []menuChoice{
				newChoice("No external source", "none"),
				newChoice("Required at call start", "call_start"),
				newChoice("← Back", actionBack),
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
		options := make([]menuChoice, 0, len(data.Tools)+2)
		for _, tool := range data.Tools {
			options = append(options, newChoice(toolLabel(data, tool), "edit:"+tool.Name))
		}
		options = append(options, newChoice("Add tool", "add"), newChoice("← Back", actionBack))
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
		tool.AttachTo = []string{cmp.Or(data.EntryAgent, "assistant")}
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
	{Value: "knowledge", Name: "Knowledge base", Field: targetcap.FieldToolKnowledge},
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

func chooseToolExecution(runner *fieldRunner, data *scaffold.Data, tool *scaffold.Tool) (bool, error) {
	target := data.Target
	for {
		handler := filepath.ToSlash(filepath.Join("tools", tool.Name+".py"))
		detail := map[string]string{
			"webhook":   "HTTP endpoint from an environment variable",
			"local":     "creates " + handler + " when saved",
			"mcp":       "server address from an environment variable",
			"builtin":   "provider prebuilt tool (end_call)",
			"knowledge": knowledgeDetail(data),
		}
		options := make([]menuChoice, 0, len(toolExecutionKinds)+1)
		for _, kind := range toolExecutionKinds {
			label := kind.Name + "  ·  " + detail[kind.Value]
			if _, ok := toolExecutionGate(kind, target); !ok {
				label = kind.Name + "  ·  unavailable on " + targetLabel(target)
			}
			options = append(options, newChoice(label, kind.Value))
		}
		options = append(options, newChoice("← Back", actionBack))
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
		if selected == "knowledge" {
			// The tool owns its own schema (one string, the caller's question)
			// and its own result, so input and output have nowhere to go.
			tool.URLEnv, tool.Input, tool.Output = "", "", ""
			if len(data.Knowledge) == 0 {
				// Nothing to point at. Refusing here beats writing a tool that
				// names a base agent.yaml never declares, which compiles to an
				// error the author then has to trace back to this menu.
				if err := showNotice(runner, "Knowledge base",
					"No knowledge: section is declared in agent.yaml. Add one there, naming a folder "+
						"of .txt, .md or .pdf files, then choose this again."); err != nil {
					return false, err
				}
				continue
			}
			if back, err := chooseKnowledgeBase(runner, data, tool); err != nil || back {
				return back, err
			}
		} else {
			tool.KnowledgeBase = ""
		}
		return false, nil
	}
}

// knowledgeDetail is the picker's one-line hint for the knowledge kind: how many
// bases are declared, because the answer decides whether the choice leads anywhere.
func knowledgeDetail(data *scaffold.Data) string {
	switch len(data.Knowledge) {
	case 0:
		return "needs a knowledge: section in agent.yaml first"
	case 1:
		return "searches " + data.Knowledge[0].Name
	default:
		return fmt.Sprintf("searches one of %d declared knowledge bases", len(data.Knowledge))
	}
}

// chooseKnowledgeBase picks which declared base the tool searches. A menu rather
// than a text field: the legal values are known, and a typo here becomes a
// compile error the author has to trace back to this screen.
func chooseKnowledgeBase(runner *fieldRunner, data *scaffold.Data, tool *scaffold.Tool) (bool, error) {
	options := make([]menuChoice, 0, len(data.Knowledge)+1)
	for _, base := range data.Knowledge {
		options = append(options, newChoice(base.Name+"  ·  "+base.Documents, base.Name))
	}
	options = append(options, newChoice("← Back", actionBack))
	choice, _, err := runner.selectOne("Knowledge base", "Which documents this tool searches.", options, true)
	if err != nil || choice == actionBack {
		return choice == actionBack, err
	}
	tool.KnowledgeBase = choice
	return false, nil
}

func editTool(runner *fieldRunner, data *scaffold.Data, tool *scaffold.Tool) error {
	for {
		var options []menuChoice
		switch {
		case tool.ExecutionKind() == "builtin":
			// A prebuilt tool carries no input/output/url; the registry owns its
			// schema. Description and the goodbye message are the only knobs.
			options = []menuChoice{
				newChoice("Description  ·  "+oneLine(tool.Description), "description"),
				newChoice("Prebuilt  ·  "+cmp.Or(tool.Builtin, "end_call"), "execution"),
				newChoice("Goodbye message  ·  "+cmp.Or(oneLine(tool.Instructions), "provider default"), "instructions"),
				newChoice("Attached to  ·  "+toolAttachmentLabel(data, *tool), "attach"),
				newChoice("Delete tool", "delete"),
				newChoice("← Back", actionBack),
			}
		case tool.ExecutionKind() == "knowledge":
			// No input, output or URL: the tool owns its schema and its result.
			// The description is the one thing worth editing, because it is what
			// tells the model when to look something up at all.
			options = []menuChoice{
				newChoice("Description  ·  "+oneLine(tool.Description), "description"),
				newChoice("Execution  ·  knowledge", "execution"),
				newChoice("Knowledge base  ·  "+cmp.Or(tool.KnowledgeBase, "none"), "base"),
				newChoice("Attached to  ·  "+toolAttachmentLabel(data, *tool), "attach"),
				newChoice("Delete tool", "delete"),
				newChoice("← Back", actionBack),
			}
		case tool.ExecutionKind() == "mcp":
			// The server announces its own tools, so the file has no
			// description, input, or output to edit (N40). Transport, auth, and
			// a tool selection are written by hand and carried through here
			// untouched.
			options = []menuChoice{
				newChoice("Execution  ·  mcp", "execution"),
				newChoice("MCP server URL env  ·  "+cmp.Or(tool.URLEnv, "none"), "url"),
				newChoice("Attached to  ·  "+toolAttachmentLabel(data, *tool), "attach"),
				newChoice("Delete tool", "delete"),
				newChoice("← Back", actionBack),
			}
		default:
			executionField := newChoice("Webhook URL env  ·  "+cmp.Or(tool.URLEnv, "none"), "url")
			if tool.ExecutionKind() == "local" {
				executionField = newChoice("Python handler  ·  "+cmp.Or(tool.Handler, "none"), "handler")
			}
			options = []menuChoice{
				newChoice("Description  ·  "+oneLine(tool.Description), "description"),
				newChoice("Execution  ·  "+tool.ExecutionKind(), "execution"),
				executionField,
				newChoice("Input schema  ·  "+oneLine(tool.Input), "input"),
				newChoice("Output schema  ·  "+cmp.Or(oneLine(tool.Output), "unconstrained"), "output"),
				newChoice("Attached to  ·  "+toolAttachmentLabel(data, *tool), "attach"),
				newChoice("Delete tool", "delete"),
				newChoice("← Back", actionBack),
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
			if _, err := chooseToolExecution(runner, data, tool); err != nil {
				return err
			}
		case "base":
			if _, err := chooseKnowledgeBase(runner, data, tool); err != nil {
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
		selected = append(selected, "Agent · "+cmp.Or(data.EntryAgent, "assistant"))
	}
	for _, name := range tool.AttachTasks {
		selected = append(selected, "Task · "+name)
	}
	return available, selected
}

func toolAttachmentLabel(data *scaffold.Data, tool scaffold.Tool) string {
	_, selected := toolAttachmentChoices(data, tool)
	return cmp.Or(strings.Join(selected, ", "), "not attached")
}

func editAgents(runner *fieldRunner, data *scaffold.Data) error {
	for {
		options := make([]menuChoice, 0, len(data.AllAgents())+3)
		for _, agent := range data.AllAgents() {
			options = append(options, newChoice(agentLabel(data, agent), "edit:"+agent.Name))
		}
		options = append(options,
			newChoice("Add agent", "add"),
			newChoice("Choose entry agent  ·  "+cmp.Or(data.EntryAgent, "assistant"), "entry"),
			newChoice("← Back", actionBack),
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
			options = append(options, newChoice("← Back", actionBack))
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
	if agent.Name == cmp.Or(data.EntryAgent, "assistant") {
		entry = "  ·  entry"
	}
	return fmt.Sprintf("%s  ·  %s  ·  %s  ·  %d tools%s",
		agent.Name, bindingLabel(agent.Reason), bindingLabel(agent.Speak), len(agentToolNames(data, agent.Name)), entry)
}

func bindingLabel(binding scaffold.Binding) string {
	identity := cmp.Or(binding.Provider, "integrated")
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
		lifecycle := newChoice("Delete agent", "delete")
		if name == "assistant" {
			lifecycle = newChoice("Reset starter agent", "reset")
		}
		choice, _, err := runner.selectOne(name, "All agent-specific settings are visible here; select a row to change it.", []menuChoice{
			newChoice("Prompt  ·  "+oneLine(agent.Instructions), "prompt"),
			newChoice("LLM model  ·  "+bindingLabel(agent.Reason), "reason"),
			newChoice("TTS voice  ·  "+bindingLabel(agent.Speak), "speak"),
			newChoice("Tools  ·  "+cmp.Or(strings.Join(agentToolNames(data, name), ", "), "none"), "tools"),
			newChoice("Entry agent  ·  "+yesNo(name == cmp.Or(data.EntryAgent, "assistant")), "entry"),
			lifecycle,
			newChoice("← Back", actionBack),
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
			confirmed, err := confirmChoice(runner, "Reset starter agent?", "Reset agent")
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
		if slices.Contains(attach, name) {
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
		attach = slices.DeleteFunc(attach, func(n string) bool { return n == name })
		if slices.Contains(selected, data.Tools[i].Name) {
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
		options := make([]menuChoice, 0, len(data.Handoffs)+2)
		for _, handoff := range data.Handoffs {
			options = append(options, newChoice(fmt.Sprintf("%s  ·  %s → %s", handoff.Name, handoff.Source, handoff.To), "view:"+handoff.Name))
		}
		options = append(options, newChoice("Add handoff", "add"), newChoice("← Back", actionBack))
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
		back, err := runner.input("Handoff name", "Lowercase snake_case; tools, delegates, handoffs and escalations share one namespace.", &handoff.Name, func(value string) error {
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
	return cmp.Or(strings.Join(handoff.Variables, ", "), "none")
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
		choice, _, err := runner.selectOne(name, "Edit this saved handoff or remove it.", []menuChoice{
			newChoice("Source agent  ·  "+handoff.Source, "source"),
			newChoice("Target agent  ·  "+handoff.To, "target"),
			newChoice("Trigger  ·  "+oneLine(handoff.When), "trigger"),
			newChoice("Required variables  ·  "+cmp.Or(strings.Join(handoff.Requires, ", "), "none"), "requires"),
			newChoice("Context  ·  "+cmp.Or(handoff.History, "full")+" · variables "+handoffVariablesLabel(*handoff), "context"),
			newChoice("Announcement  ·  "+cmp.Or(oneLine(handoff.Announce), "silent"), "announce"),
			newChoice("Delete handoff", "delete"),
			newChoice("← Back", actionBack),
		}, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "source":
			options := agentOptions(data.AllAgents(), "")
			options = append(options, newChoice("← Back", actionBack))
			source, back, err := runner.selectOne("Source agent", "", options, true)
			if err != nil {
				return err
			}
			if !back && source != handoff.To {
				handoff.Source = source
			}
		case "target":
			options := agentOptions(data.AllAgents(), handoff.Source)
			options = append(options, newChoice("← Back", actionBack))
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
		case "announce":
			if _, err := runner.input("Announcement (optional)", "Exact sentence spoken before the handoff. Blank keeps it silent.", &handoff.Announce, func(string) error { return nil }); err != nil {
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
		history := cmp.Or(handoff.History, "full")
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
		options := []menuChoice{newChoice("History  ·  "+history, "history")}
		if history == "last_n" {
			options = append(options, newChoice(fmt.Sprintf("Maximum messages  ·  %d", handoff.MaxMessages), "maximum"))
		}
		if history == "summary" {
			options = append(options, newChoice("Summarizer model  ·  "+cmp.Or(handoff.Summarizer, data.AllAgents()[0].ModelProfile()), "summarizer"))
		}
		options = append(options, newChoice("Tool calls  ·  "+tools, "tools"))
		if len(data.Variables) > 0 {
			options = append(options, newChoice("Variable scope  ·  "+scope, "scope"))
			if scope == "selected" {
				options = append(options, newChoice("Selected variables  ·  "+strings.Join(handoff.Variables, ", "), "variables"))
			}
		}
		options = append(options, newChoice("← Back", actionBack))
		choice, _, err := runner.selectOne("Handoff context", "Edit one field, then return here.", options, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "history":
			choices := []menuChoice{
				newChoice("Full history (portable)", "full"),
				newChoice("Messages", "messages"),
				newChoice("Last N messages", "last_n"),
				newChoice("Summary", "summary"),
				newChoice("Reset", "reset"),
				newChoice("← Back", actionBack),
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
			profiles := make([]menuChoice, 0, len(data.AllAgents())+1)
			for _, agent := range data.AllAgents() {
				profiles = append(profiles, newChoice(agent.ModelProfile(), agent.ModelProfile()))
			}
			profiles = append(profiles, newChoice("← Back", actionBack))
			selected, back, err := runner.selectOne("Summarizer model profile", "", profiles, true)
			if err != nil {
				return err
			}
			if !back {
				handoff.Summarizer = selected
			}
		case "tools":
			selected, back, err := runner.selectOne("Tool calls in context", "", []menuChoice{newChoice("Provider default", "default"), newChoice("Include", "yes"), newChoice("Exclude", "no"), newChoice("← Back", actionBack)}, true)
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
			selected, back, err := runner.selectOne("Variables in context", "Available variables: "+strings.Join(variableNames(data), ", "), []menuChoice{newChoice("All variables", "all"), newChoice("Selected variables", "selected"), newChoice("No variables", "none"), newChoice("← Back", actionBack)}, true)
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
		options := make([]menuChoice, 0, len(data.Tasks)+2)
		for _, task := range data.Tasks {
			options = append(options, newChoice(taskLabel(task), "view:"+task.Name))
		}
		options = append(options, newChoice("Add task", "add"), newChoice("← Back", actionBack))
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
			Agent:        cmp.Or(data.EntryAgent, "assistant"),
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
	return fmt.Sprintf("%s  ·  %s  ·  %s", task.Name, cmp.Or(task.Agent, "entry agent"), task.Result)
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
		choice, _, err := runner.selectOne(name, "Edit this saved task or remove it.", []menuChoice{
			newChoice("Prompt  ·  "+oneLine(task.Instructions), "prompt"),
			newChoice("Tools  ·  "+cmp.Or(strings.Join(task.Tools, ", "), "none"), "tools"),
			newChoice("Model  ·  "+cmp.Or(task.Model, "entry agent model"), "model"),
			newChoice("Typed result  ·  "+task.Result, "result"),
			newChoice("Context  ·  "+cmp.Or(task.History, "full"), "context"),
			newChoice("Delegating agent  ·  "+cmp.Or(task.Agent, "assistant"), "agent"),
			newChoice("Trigger  ·  "+oneLine(task.When), "trigger"),
			newChoice("Result assignments  ·  "+cmp.Or(strings.Join(assignmentVariables(task.Assign), ", "), "none"), "assign"),
			newChoice("Delete task", "delete"),
			newChoice("← Back", actionBack),
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
			options := []menuChoice{newChoice("Entry agent model", "default")}
			for _, agent := range data.AllAgents() {
				options = append(options, newChoice(agent.ModelProfile(), agent.ModelProfile()))
			}
			options = append(options, newChoice("← Back", actionBack))
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
			options = append(options, newChoice("← Back", actionBack))
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
		history := cmp.Or(task.History, "full")
		tools := "provider default"
		if task.IncludeToolCalls != nil {
			tools = map[bool]string{true: "include", false: "exclude"}[*task.IncludeToolCalls]
		}
		options := []menuChoice{newChoice("History  ·  "+history, "history")}
		if history == "last_n" {
			options = append(options, newChoice(fmt.Sprintf("Maximum messages  ·  %d", task.MaxMessages), "maximum"))
		}
		if history == "summary" {
			options = append(options, newChoice("Summarizer model  ·  "+cmp.Or(task.Summarizer, data.AllAgents()[0].ModelProfile()), "summarizer"))
		}
		options = append(options, newChoice("Tool calls  ·  "+tools, "tools"), newChoice("← Back", actionBack))
		choice, _, err := runner.selectOne("Task context", "Edit one field, then return here.", options, true)
		if err != nil || choice == actionBack {
			return true, err
		}
		switch choice {
		case "history":
			choices := []menuChoice{
				newChoice("Full history (portable)", "full"),
				newChoice("Messages", "messages"),
				newChoice("Last N messages", "last_n"),
				newChoice("Summary", "summary"),
				newChoice("Reset", "reset"),
				newChoice("← Back", actionBack),
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
			var profiles []menuChoice
			for _, agent := range data.AllAgents() {
				profiles = append(profiles, newChoice(agent.ModelProfile(), agent.ModelProfile()))
			}
			profiles = append(profiles, newChoice("← Back", actionBack))
			selected, back, err := runner.selectOne("Summarizer model profile", "", profiles, true)
			if err != nil {
				return false, err
			}
			if !back {
				task.Summarizer = selected
			}
		case "tools":
			selected, back, err := runner.selectOne("Tool calls in task context", "", []menuChoice{newChoice("Provider default", "default"), newChoice("Include", "yes"), newChoice("Exclude", "no"), newChoice("← Back", actionBack)}, true)
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
		if !slices.Contains(fields, field) {
			field = fields[0]
		}
		assignments[variable] = "result." + field
	}
	for {
		options := make([]menuChoice, 0, len(selected)+2)
		for _, variable := range selected {
			options = append(options, newChoice(variable+"  ·  "+strings.TrimPrefix(assignments[variable], "result."), variable))
		}
		options = append(options, newChoice("Done", "done"), newChoice("← Back", actionBack))
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
		fieldOptions := make([]menuChoice, 0, len(fields)+2)
		for _, name := range fields {
			fieldOptions = append(fieldOptions, newChoice(name, name))
		}
		fieldOptions = append(fieldOptions, newChoice("Remove assignment", "remove"))
		fieldOptions = append(fieldOptions, newChoice("← Back", actionBack))
		field, back, err := runner.selectOne("Result field for "+choice, "", fieldOptions, true)
		if err != nil {
			return false, err
		}
		if !back {
			if field == "remove" {
				delete(assignments, choice)
				selected = slices.DeleteFunc(selected, func(n string) bool { return n == choice })
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
	slices.Sort(names)
	return names
}

func assignmentVariables(raw string) []string {
	var assignments map[string]string
	_ = json.Unmarshal([]byte(raw), &assignments)
	names := make([]string, 0, len(assignments))
	for name := range assignments {
		names = append(names, name)
	}
	slices.Sort(names)
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
		options := make([]menuChoice, 0, len(data.TaskGroups)+2)
		for _, group := range data.TaskGroups {
			options = append(options, newChoice(group.Name+"  ·  "+strings.Join(group.Steps, " → "), "view:"+group.Name))
		}
		options = append(options, newChoice("Add task group", "add"), newChoice("← Back", actionBack))
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
		group := scaffold.TaskGroup{ContextScope: "shared", Then: "return", Agent: cmp.Or(data.EntryAgent, "assistant")}
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
		choice, _, err := runner.selectOne(name, "Edit this saved task group or remove it.", []menuChoice{
			newChoice("Ordered steps  ·  "+strings.Join(group.Steps, " → "), "steps"),
			newChoice("Context between steps  ·  "+group.ContextScope, "context"),
			newChoice("Completion  ·  "+completion, "completion"),
			newChoice("Transfer target  ·  "+cmp.Or(group.ThenTarget, "not applicable"), "target"),
			newChoice("Delegating agent  ·  "+group.Agent, "agent"),
			newChoice("Trigger  ·  "+oneLine(group.When), "trigger"),
			newChoice("Delete task group", "delete"),
			newChoice("← Back", actionBack),
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
			options := []menuChoice{
				newChoice("Shared context", "shared"),
				newChoice("Isolated context", "isolated"),
				newChoice("← Back", actionBack),
			}
			selected, back, err := runner.selectOne("Context between steps", "", options, true)
			if err != nil {
				return err
			}
			if !back && selected != actionBack {
				group.ContextScope = selected
			}
		case "completion":
			selected, back, err := runner.selectOne("After the final step", "", []menuChoice{
				newChoice("Return to caller agent", "return"), newChoice("Transfer to agent", "transfer"), newChoice("End conversation", "end"), newChoice("← Back", actionBack),
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
			options = append(options, newChoice("← Back", actionBack))
			selected, back, err := runner.selectOne("Transfer target", "", options, true)
			if err != nil {
				return err
			}
			if !back {
				group.ThenTarget = selected
			}
		case "agent":
			options := agentOptions(data.AllAgents(), "")
			options = append(options, newChoice("← Back", actionBack))
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
		options := []menuChoice{newChoice("Channels  ·  "+strings.ReplaceAll(mode, "_", " "), "mode")}
		if mode != "web" {
			options = append(options, newChoice("Required phone controls  ·  "+cmp.Or(strings.Join(phone.RequiredControls, ", "), "none"), "controls"))
			if phone.Outbound {
				options = append(options, newChoice("When voicemail answers  ·  "+cmp.Or(phone.OnVoicemail, "hangup"), "voicemail"))
			}
			options = append(options,
				newChoice("Target transport  ·  "+cmp.Or(data.Transport, "not set"), "transport"),
				newChoice("Carrier  ·  "+cmp.Or(data.Carrier, "not set"), "carrier"),
			)
		}
		options = append(options, newChoice("← Back", actionBack))
		choice, _, err := runner.selectOne("Caller channels", runner.describe("This declares web/phone behavior only. Phone numbers, SIP trunks, carriers, and rooms remain external setup."), options, true)
		if err != nil || choice == actionBack {
			return err
		}
		savePhone := func() {
			data.Channels = []scaffold.Channel{{Name: "web", Kind: "realtime_audio"}, phone}
		}
		switch choice {
		case "mode":
			selected, back, err := runner.selectOne("Caller channels", "", []menuChoice{
				newChoice("Web browser audio", "web"), newChoice("Web + inbound phone", "web_phone_inbound"),
				newChoice("Web + outbound phone", "web_phone_outbound"), newChoice("Web + inbound/outbound phone", "web_phone_both"),
				newChoice("← Back", actionBack),
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
				phone.OnVoicemail = cmp.Or(phone.OnVoicemail, "hangup")
			} else {
				phone.OnVoicemail = ""
			}
			if data.Transport == "" {
				data.Transport = scaffold.DefaultTransport(data.Target)
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
			selected, back, err := runner.selectOne("When voicemail answers", "", []menuChoice{newChoice("Hang up", "hangup"), newChoice("Leave a message", "leave_message"), newChoice("← Back", actionBack)}, true)
			if err != nil {
				return err
			}
			if !back {
				phone.OnVoicemail = selected
				savePhone()
			}
		case "transport":
			if _, err := runner.input("Target transport (optional)", "Pipecat cold transfer needs carrier-backed daily-sip or cloud-websocket with Twilio.", &data.Transport, validateBasic); err != nil {
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
		return showNotice(runner, "Escalations unavailable", "Add a telephony caller channel first.")
	}
	for {
		options := make([]menuChoice, 0, len(data.HumanTransfers)+2)
		for _, transfer := range data.HumanTransfers {
			options = append(options, newChoice(fmt.Sprintf("%s  ·  %s  ·  %s", transfer.Name, transfer.Agent, transfer.Destination), "view:"+transfer.Name))
		}
		options = append(options, newChoice("Add escalation", "add"), newChoice("← Back", actionBack))
		choice, _, err := runner.selectOne("Escalations", runner.describe(
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
			if err := showNotice(runner, "Cannot add escalation", "Add a telephony caller channel first. Existing escalations remain available above for repair or deletion."); err != nil {
				return err
			}
			continue
		}
		transfer := scaffold.HumanTransfer{Agent: cmp.Or(data.EntryAgent, "assistant"), Mode: "cold"}
		back, err := runner.input("Escalation name", "Lowercase snake_case; tools, delegates, handoffs and escalations share one namespace.", &transfer.Name, func(value string) error {
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
		choice, _, err := runner.selectOne(name, "Edit this saved escalation or remove it.", []menuChoice{
			newChoice("Agent  ·  "+transfer.Agent, "agent"),
			newChoice("Trigger  ·  "+oneLine(transfer.When), "trigger"),
			newChoice("Destination  ·  "+transfer.Destination, "destination"),
			newChoice("Destination value  ·  "+transfer.Value, "value"),
			newChoice("Mode  ·  "+transfer.Mode, "mode"),
			newChoice("Briefing  ·  "+cmp.Or(transfer.Briefing, "none"), "briefing"),
			newChoice("Delete escalation", "delete"),
			newChoice("← Back", actionBack),
		}, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "agent":
			options := agentOptions(data.AllAgents(), "")
			options = append(options, newChoice("← Back", actionBack))
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
			// Asks the table rather than naming a provider. `!= pipecat` meant
			// "LiveKit" only while there were two targets; on a third it offered a
			// warm transfer the same table refuses at validate, so the console
			// wrote a package it already knew would fail.
			options := []menuChoice{newChoice("Cold transfer", "cold")}
			if offersWarmTransfer(data.Target) {
				options = append(options, newChoice("Warm transfer", "warm"))
			}
			options = append(options, newChoice("← Back", actionBack))
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
			options := []menuChoice{newChoice("Summary", "summary"), newChoice("← Back", actionBack)}
			selected, back, err := runner.selectOne("Operator briefing", "", options, true)
			if err != nil {
				return err
			}
			if !back && selected != actionBack {
				transfer.Briefing = selected
			}
		case "delete":
			confirmed, err := confirmDelete(runner, "escalation", name)
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
		choice, _, err := runner.selectOne("Customize", runner.describe("Optional settings stay collapsed here; starter defaults remain valid."), []menuChoice{
			newChoice("Conversation behavior", "conversation"),
			newChoice(fmt.Sprintf("Model fallbacks  ·  %d", len(data.Fallbacks)), "fallbacks"),
			newChoice("Capacity", "capacity"),
			newChoice("Advanced target settings", "target"),
			newChoice("← Back", actionBack),
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
		speaksFirst := cmp.Or(data.SpeaksFirst, "agent")
		opening := "fixed"
		if data.ModelGreeting {
			opening = "model-written"
		}
		interruption := "provider default"
		if data.Interruption != nil {
			interruption = map[bool]string{true: "enabled", false: "disabled"}[*data.Interruption]
		}
		options := []menuChoice{
			newChoice("Who speaks first  ·  "+speaksFirst, "speaker"),
			newChoice("Opening mode  ·  "+opening, "opening"),
		}
		if !data.ModelGreeting {
			options = append(options, newChoice("Fixed greeting  ·  "+oneLine(cmp.Or(data.Greeting, scaffold.DefaultGreeting)), "greeting"))
		}
		options = append(options,
			newChoice("Interruption  ·  "+interruption, "interruption"),
			newChoice(fmt.Sprintf("Minimum interruption words  ·  %d", data.MinimumWords), "minimum"),
			newChoice("Ignored interruption phrases  ·  "+cmp.Or(strings.Join(data.IgnorePhrases, ", "), "none"), "phrases"),
			newChoice("Inactivity nudge after  ·  "+cmp.Or(data.NudgeAfter, "not set"), "nudge"),
			newChoice("Inactivity end after  ·  "+cmp.Or(data.EndAfter, "not set"), "end"),
			newChoice("Maximum call duration  ·  "+cmp.Or(data.MaxDuration, "not set"), "duration"),
			newChoice("Thinking audio  ·  "+cmp.Or(data.ThinkingAudio, "none"), "thinking"),
			newChoice("← Back", actionBack),
		)
		choice, _, err := runner.selectOne("Conversation behavior", "Edit one field, then return here.", options, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "speaker":
			selected, back, err := runner.selectOne("Who speaks first", "", []menuChoice{newChoice("Agent", "agent"), newChoice("Caller", "user"), newChoice("← Back", actionBack)}, true)
			if err != nil {
				return err
			}
			if !back {
				data.SpeaksFirst = selected
			}
		case "opening":
			selected, back, err := runner.selectOne("Opening", "", []menuChoice{newChoice("Fixed greeting", "fixed"), newChoice("Model-written greeting", "model"), newChoice("← Back", actionBack)}, true)
			if err != nil {
				return err
			}
			if !back {
				data.ModelGreeting = selected == "model"
				if data.ModelGreeting {
					data.Greeting = ""
				} else {
					data.Greeting = cmp.Or(data.Greeting, scaffold.DefaultGreeting)
				}
			}
		case "greeting":
			if _, err := runner.input("Fixed greeting", "Spoken verbatim.", &data.Greeting, validateRequiredText); err != nil {
				return err
			}
		case "interruption":
			selected, back, err := runner.selectOne("Interruption", "", []menuChoice{newChoice("Provider default", "default"), newChoice("Enabled", "enabled"), newChoice("Disabled", "disabled"), newChoice("← Back", actionBack)}, true)
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
			selected, back, err := runner.selectOne("Thinking audio", "", []menuChoice{newChoice("None", "none"), newChoice("Subtle", "subtle"), newChoice("← Back", actionBack)}, true)
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
		options := make([]menuChoice, 0, len(data.Fallbacks)+2)
		for _, fallback := range data.Fallbacks {
			options = append(options, newChoice(fallback.Name+"  ·  protects "+fallback.Profile, "view:"+fallback.Name))
		}
		options = append(options, newChoice("Add fallback", "add"), newChoice("← Back", actionBack))
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
		choice, _, err := runner.selectOne(name, "Edit this saved fallback or remove it before retrying preflight.", []menuChoice{
			newChoice("Protected model  ·  "+fallback.Profile, "profile"),
			newChoice("Fallback model  ·  "+bindingLabel(fallback.Binding), "binding"),
			newChoice("Delete fallback", "delete"),
			newChoice("← Back", actionBack),
		}, true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "profile":
			profiles := make([]menuChoice, 0, len(data.AllAgents())+1)
			for _, agent := range data.AllAgents() {
				profiles = append(profiles, newChoice(agent.ModelProfile(), agent.ModelProfile()))
			}
			profiles = append(profiles, newChoice("← Back", actionBack))
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
		choice, _, err := runner.selectOne("Capacity", "Edit one field, then return here.", []menuChoice{
			newChoice(fmt.Sprintf("Peak sessions  ·  %d", capacity.PeakSessions), "peak"),
			newChoice(fmt.Sprintf("Maximum sessions  ·  %d", capacity.MaxSessions), "max"),
			newChoice("Average session duration  ·  "+capacity.AvgSessionDuration, "duration"),
			newChoice("← Back", actionBack),
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

type advancedField struct {
	title, help string
	value       *string
	validate    func(string) error
	save        func(string)
}

// advancedTargetFields is the list the Advanced target settings form offers, and
// it is a function rather than a literal inside the form so a test can read it.
//
// Three of the four describe a project: a framework version to pin, an SDK to
// generate, and packages to hold still. A target that emits no project has none
// of the three, and ir.validateDriverValues refuses all three by name, so
// offering them is the console filling in a package it knows will fail. The
// region stays, because every target deploys somewhere.
func advancedTargetFields(data *scaffold.Data, regions *string) []advancedField {
	region := advancedField{"Deployment region (optional)", "Where the platform deploys the agent; forwarded as declared. One region, or several separated by commas for one deployment per region (LiveKit only).", regions, validateBasic, func(value string) {
		data.DeploymentRegions = parsePhrases(value)
	}}
	if !targetcap.EmitsProject(targetcap.Provider(data.Target)) {
		return []advancedField{region}
	}
	return []advancedField{
		{"Target version", "Driver/framework version pin.", &data.TargetVersion, validateBasic, nil},
		{"SDK language (optional)", "For example python on LiveKit.", &data.SDKLanguage, validateBasic, nil},
		region,
		{"Pins (optional JSON object)", "Independently versioned target packages.", &data.Pins, validateParams, nil},
	}
}

func editAdvancedTarget(runner *fieldRunner, data *scaffold.Data) error {
	for {
		// deployment_region holds one region or several (N32), so it is one
		// comma-separated field: joined for display, split on save. Without the
		// save hook a multi-region package would lose every region but the
		// first the moment someone opened this form to edit something else.
		regions := strings.Join(data.DeploymentRegions, ", ")
		fields := advancedTargetFields(data, &regions)
		options := make([]menuChoice, 0, len(fields)+1)
		for i, field := range fields {
			options = append(options, newChoice(field.title+"  ·  "+cmp.Or(*field.value, "not set"), strconv.Itoa(i)))
		}
		options = append(options, newChoice("← Back", actionBack))
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

// optional accepts a blank answer and applies check to anything else.
// required rejects a blank answer with its own message and otherwise applies
// check. Between them they cover every "and it may be empty" / "and it must not
// be empty" rule here, so a new rule is a composition rather than a
// thirteenth near-identical wrapper.
func optional(check func(string) error) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return check(value)
	}
}

func required(message string, check func(string) error) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return errors.New(message)
		}
		if check == nil {
			return nil
		}
		return check(value)
	}
}

func validateNonNegativeInteger(value string) error {
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 {
		return errors.New("value must be a non-negative integer")
	}
	return nil
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

// validateControlName refuses a name any of the four kinds already uses. They
// all become callable function names at runtime, so they share one namespace and
// the check has to see all of them.
func validateControlName(data *scaffold.Data, name string) error {
	for _, tool := range data.Tools {
		if tool.Name == name {
			return errors.New("name already used by a tool")
		}
	}
	for _, handoff := range data.Handoffs {
		if handoff.Name == name {
			return errors.New("name already used by a handoff")
		}
	}
	for _, task := range data.Tasks {
		if task.RunName() == name {
			return errors.New("name already used by a delegate that runs a task")
		}
	}
	for _, group := range data.TaskGroups {
		if group.RunName() == name {
			return errors.New("name already used by a delegate that runs a task group")
		}
	}
	for _, transfer := range data.HumanTransfers {
		if transfer.Name == name {
			return errors.New("name already used by an escalation")
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

func agentOptions(agents []scaffold.Agent, except string) []menuChoice {
	options := make([]menuChoice, 0, len(agents))
	for _, agent := range agents {
		if agent.Name != except {
			options = append(options, newChoice(agent.Name, agent.Name))
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
		options := make([]menuChoice, 0, len(available)+2)
		for _, name := range available {
			mark := "[ ]"
			if slices.Contains(selected, name) {
				mark = "[x]"
			}
			options = append(options, newChoice(mark+" "+name, "toggle:"+name))
		}
		options = append(options, newChoice("Done", "done"), newChoice("← Back", actionBack))
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
		if slices.Contains(selected, name) {
			selected = slices.DeleteFunc(selected, func(n string) bool { return n == name })
		} else {
			selected = append(selected, name)
		}
	}
}

func confirmDelete(runner *fieldRunner, kind, name string) (bool, error) {
	return confirmChoice(runner, "Delete "+kind+" "+name+"?", "Delete")
}

func confirmChoice(runner *fieldRunner, title, action string) (bool, error) {
	choice, back, err := runner.selectOne(title, "Choose Back to return without applying this action.", []menuChoice{
		newChoice(action, "confirm"),
		newChoice("← Back", actionBack),
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
			data.Handoffs[i].Requires = slices.DeleteFunc(data.Handoffs[i].Requires, func(n string) bool { return n == name })
			data.Handoffs[i].Variables = slices.DeleteFunc(data.Handoffs[i].Variables, func(n string) bool { return n == name })
		}
		for i := range data.Tasks {
			removeAssignment(&data.Tasks[i], name)
		}
	case "tool":
		data.Tools = slices.DeleteFunc(data.Tools, func(item scaffold.Tool) bool { return item.Name == name })
		for i := range data.Tasks {
			data.Tasks[i].Tools = slices.DeleteFunc(data.Tasks[i].Tools, func(n string) bool { return n == name })
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
			if slices.Contains(data.Tools[i].AttachTo, name) {
				data.Tools[i].AttachTo = slices.DeleteFunc(data.Tools[i].AttachTo, func(n string) bool { return n == name })
				if !slices.Contains(data.Tools[i].AttachTo, "assistant") {
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
			data.Tools[i].AttachTasks = slices.DeleteFunc(data.Tools[i].AttachTasks, func(n string) bool { return n == name })
		}
		for i := range data.TaskGroups {
			data.TaskGroups[i].Steps = slices.DeleteFunc(data.TaskGroups[i].Steps, func(n string) bool { return n == name })
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

// The composed rules. Each message is the one the field printed before, because
// an author reads it and a changed message is a changed interface.
var (
	validateOptionalDuration           = optional(validateDuration)
	validateOptionalNonNegativeInteger = optional(validateNonNegativeInteger)
	validateRequiredBasic              = required("value is required", validateBasic)
	validateRequiredText               = required("value is required", nil)
	validateRequiredObject             = required("JSON object is required", validateParams)
)

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

func validateEnvName(value string) error {
	if !envNamePattern.MatchString(value) {
		return errors.New("value must be an environment variable name")
	}
	return nil
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
	// One scanner for the whole accessible session. Per-prompt scanners are what
	// forced the old one-byte reader: a second scanner over the same stream
	// inherits none of the first one's buffer, so a read-ahead swallowed the
	// next answer. One scanner has no second reader to lose a buffer to.
	scanner  *bufio.Scanner
	requests chan shellRequest
	actions  ActionHandler
	ctx      viewCtx // interactive chrome context (breadcrumb, target, sidebar)
}

func newRunner(in io.Reader, out io.Writer, accessible bool) *fieldRunner {
	runner := &fieldRunner{in: in, out: out, accessible: accessible}
	if accessible {
		runner.scanner = bufio.NewScanner(in)
	}
	return runner
}

// errAborted ends an accessible session: input stopped in the middle of a
// prompt, so there is no answer coming and every caller unwinds.
var errAborted = errors.New("user aborted")

// ask writes one prompt and returns the answer. ok is false at end of input,
// which is an abort rather than an empty answer: a script that stops mid-flow
// must not be read as someone pressing enter.
func (r *fieldRunner) ask(prompt string) (answer string, ok bool) {
	fmt.Fprint(r.out, prompt)
	if r.scanner == nil || !r.scanner.Scan() {
		return "", false
	}
	fmt.Fprintln(r.out)
	return r.scanner.Text(), true
}

// Option is one menu entry for callers outside this package, and SelectOne
// renders it. `unmute dev` prompts for a target before any console exists, so
// it has no interactive model to send a field to and uses this directly.
type Option struct{ Label, Value string }

func SelectOne(in io.Reader, out io.Writer, title string, options []Option) (string, error) {
	choices := make([]menuChoice, len(options))
	for i, o := range options {
		choices[i] = newChoice(o.Label, o.Value)
	}
	value, _, err := newRunner(in, out, true).selectOne(title, "", choices, false)
	return value, err
}

func (r *fieldRunner) selectOne(title, description string, options []menuChoice, backable bool) (string, bool, error) {
	if len(options) == 0 {
		return "", false, errors.New("menu has no actions")
	}
	if backable {
		if len(options) < 2 {
			return "", false, errors.New("menu requires an action before Back")
		}
		if options[len(options)-1].value != actionBack {
			return "", false, errors.New("menu Back action must be last")
		}
		for _, option := range options[:len(options)-1] {
			if option.value == actionBack {
				return "", false, errors.New("menu Back action may appear only once")
			}
		}
	}
	if !r.accessible {
		reply := r.sendField(fieldReq{
			kind:     kindSelect,
			title:    title,
			desc:     description,
			choices:  options,
			initial:  options[0].value,
			backable: backable,
			ctx:      r.ctx,
		})
		if reply.back {
			return actionBack, true, nil
		}
		return reply.value, false, nil
	}
	// Accessible: an ordinal list. The number is the whole interface, so every
	// option carries one and the range is repeated with the prompt. A reader who
	// cannot see the screen has to be able to name the option they want.
	fmt.Fprintf(r.out, "%s \n", title)
	for i, option := range options {
		fmt.Fprintf(r.out, "%d. %s\n", i+1, option.label)
	}
	for {
		answer, ok := r.ask(fmt.Sprintf("Enter a number between 1 and %d: ", len(options)))
		if !ok {
			return "", false, fmt.Errorf("menu: %w", errAborted)
		}
		// An empty answer takes the first option. The field used to be
		// pre-filled with it, so pressing enter accepted it; scripted runs rely
		// on that to mean "the default is fine".
		picked := 1
		if trimmed := strings.TrimSpace(answer); trimmed != "" {
			n, err := strconv.Atoi(trimmed)
			if err != nil || n < 1 || n > len(options) {
				fmt.Fprintf(r.out, "Enter a number between 1 and %d.\n", len(options))
				continue
			}
			picked = n
		}
		value := options[picked-1].value
		if backable && value == actionBack {
			return actionBack, true, nil
		}
		return value, false, nil
	}
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
	r.describe(description)
	for {
		answer, ok := r.ask(title + " ")
		if !ok {
			return false, fmt.Errorf("menu: %w", errAborted)
		}
		if strings.TrimSpace(answer) == actionBack {
			return true, nil
		}
		// An empty answer keeps what is already there, the way submitting a
		// pre-filled field did. It still has to validate: an empty current
		// value is not made acceptable by arriving this way.
		if answer == "" {
			answer = *value
		}
		if validate != nil {
			if err := validate(answer); err != nil {
				fmt.Fprintf(r.out, "%v\n", err)
				continue
			}
		}
		*value = answer
		return false, nil
	}
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
	r.describe(description)
	answer, ok := r.ask(title + " ")
	if !ok {
		return false, fmt.Errorf("menu: %w", errAborted)
	}
	if strings.TrimSpace(answer) == actionBack {
		return true, nil
	}
	if answer == "" {
		return false, nil // keep what is already there
	}
	*value = answer
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
