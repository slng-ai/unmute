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
	slngWordmark  = "\x1b[1;38;2;245;201;110m" +
		"  ____  _     _   _  ____       //  // \n" +
		" / ___|| |   | \\ | |/ ___|     //  //  \n" +
		" \\___ \\| |   |  \\| | |  _     //  //   \n" +
		"  ___) | |___| |\\  | |_| |   //  //    \n" +
		" |____/|_____|_| \\_|\\____|  //  //     \x1b[0m"
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

// Run displays the console without writing files.
func Run(in io.Reader, out io.Writer, accessible bool) (Result, error) {
	return runWithStart(in, out, accessible, false)
}

// RunCreate enters the create flow directly for `unmute init`.
func RunCreate(in io.Reader, out io.Writer, accessible bool) (Result, error) {
	return runWithStart(in, out, accessible, true)
}

func runWithStart(in io.Reader, out io.Writer, accessible, createOnly bool) (Result, error) {
	runner := newRunner(in, out, accessible)
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
		choice := actionCreate
		if _, err := runner.run(huh.NewSelect[string]().
			Title(homeTitle()).
			Description("What would you like to do?").
			Options(
				huh.NewOption("Create a new agent", actionCreate),
				huh.NewOption("Open an existing agent", actionOpen),
				huh.NewOption("Quit", actionQuit),
			).
			Value(&choice), false); err != nil {
			return Result{}, err
		}
		if choice == actionQuit {
			return Result{}, nil
		}
		if choice == actionOpen {
			if err := showNotice(runner, "Open existing agent unavailable", "Package maintenance lands in T2. Choose Back to return Home."); err != nil {
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
		Language:     scaffold.DefaultLanguage,
		Channel:      scaffold.DefaultChannel,
		Greeting:     scaffold.DefaultGreeting,
		Instructions: scaffold.DefaultInstructions,
	}
	data.SetTarget(scaffold.DefaultTarget)
	return editAgent(runner, Agent{Path: path, Data: data})
}

func homeTitle() string {
	if os.Getenv("NO_COLOR") != "" {
		return "Unmute"
	}
	return slngWordmark
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
				huh.NewOption("Target  ·  "+targetLabel(result.Agent.Data.Target), "target"),
				huh.NewOption("Language  ·  "+result.Agent.Data.Language, "language"),
				huh.NewOption("Models  ·  "+modelsLabel(result.Agent.Data), "models"),
				huh.NewOption("Instructions (prompt)", "prompt"),
				huh.NewOption("Greeting  ·  "+result.Agent.Data.Greeting, "greeting"),
				huh.NewOption(fmt.Sprintf("Variables  ·  %d", len(result.Agent.Data.Variables)), "variables"),
				huh.NewOption(fmt.Sprintf("Tools  ·  %d", len(result.Agent.Data.Tools)), "tools"),
				huh.NewOption(fmt.Sprintf("Agents  ·  %d", len(result.Agent.Data.AllAgents())), "agents"),
				huh.NewOption(fmt.Sprintf("Handoffs  ·  %d", len(result.Agent.Data.Handoffs)), "handoffs"),
				huh.NewOption(fmt.Sprintf("Tasks  ·  %d", len(result.Agent.Data.Tasks)), "tasks"),
				huh.NewOption(fmt.Sprintf("Task groups  ·  %d", len(result.Agent.Data.TaskGroups)), "groups"),
				huh.NewOption("Caller channels  ·  "+channelsLabel(result.Agent.Data), "channels"),
				huh.NewOption(fmt.Sprintf("Human transfers  ·  %d", len(result.Agent.Data.HumanTransfers)), "humans"),
				huh.NewOption("Customize  ·  conversation, fallback, capacity, target", "customize"),
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
		case "target":
			selected := result.Agent.Data.Target
			back, err := runner.run(huh.NewSelect[string]().
				Title("Target / orchestrator").
				Description(runner.describe("Vapi and Deepgram are unavailable: their generators are not implemented yet.")).
				Options(
					huh.NewOption("Pipecat  ·  generated code project", string(targetcap.Pipecat)),
					huh.NewOption("LiveKit  ·  generated code project", string(targetcap.LiveKit)),
					huh.NewOption("ElevenLabs  ·  managed API plan", string(targetcap.ElevenLabs)),
					huh.NewOption("← Back", actionBack),
				).
				Value(&selected), true)
			if err != nil {
				return Result{}, false, err
			}
			if !back && selected != actionBack {
				result.Agent.Data.SetTarget(selected)
				for i := range result.Agent.Data.Agents {
					result.Agent.Data.Agents[i].Reason = result.Agent.Data.Reason
					result.Agent.Data.Agents[i].Speak = result.Agent.Data.Speak
				}
				for i := range result.Agent.Data.Fallbacks {
					result.Agent.Data.Fallbacks[i].Binding = result.Agent.Data.Reason
				}
			}
		case "language":
			if _, err := runner.input("Language", "Primary spoken BCP-47 language tag, for example en or es-MX.", &result.Agent.Data.Language, validateLanguage); err != nil {
				return Result{}, false, err
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
			confirmed := true
			back, err := runner.run(huh.NewConfirm().Title(summary(result, review)).Value(&confirmed), true)
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

func repairPreflight(runner *fieldRunner, data *scaffold.Data, preflightErr error) error {
	for {
		choice := actionBack
		_, err := runner.run(huh.NewSelect[string]().
			Title("Cannot create agent").
			Description(runner.describe("Fix the configuration, then go Back to continue editing.\n\n"+preflightErr.Error())).
			Options(
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
			).
			Value(&choice), true)
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
	fmt.Fprintf(&text, "Create %s?\nTarget: %s (%s)\nLanguage: %s\nCaller channels: %s\nGreeting: %s\nRequired env: %s\nForwarded bindings:",
		result.Agent.Data.Name, targetLabel(result.Agent.Data.Target), review.TargetName,
		result.Agent.Data.Language, channelsLabel(result.Agent.Data), result.Agent.Data.Greeting, strings.Join(review.RequiredEnv, ", "))
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
	case targetcap.ElevenLabs:
		return "ElevenLabs"
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
		choice := actionBack
		_, err := runner.run(huh.NewSelect[string]().
			Title("STT / LLM / TTS").
			Description(runner.describe("Providers come from the selected target's catalogue. Model ids, voices, and params are forwarded as entered.")).
			Options(
				huh.NewOption("Listen (STT)  ·  "+modelsLabelPart(data.Target, targetcap.Listen, data.Listen), string(targetcap.Listen)),
				huh.NewOption("Reason (LLM)  ·  "+modelsLabelPart(data.Target, targetcap.Reason, data.Reason), string(targetcap.Reason)),
				huh.NewOption("Speak (TTS)  ·  "+modelsLabelPart(data.Target, targetcap.Speak, data.Speak), string(targetcap.Speak)),
				huh.NewOption("Reset default models", "reset"),
				huh.NewOption("← Back", actionBack),
			).
			Value(&choice), true)
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
	options := providerOptions(framework, role)
	integratedListen := framework == targetcap.ElevenLabs && role == targetcap.Listen

	if integratedListen {
		runner.describe("ElevenLabs STT is integrated; only its optional params are configurable.")
	} else if len(options) > 0 {
		selected := catalog.Brand(framework, role, binding.Provider, binding.Model)
		options = append(options, huh.NewOption("← Back", actionBack))
		back, err := runner.run(huh.NewSelect[string]().
			Title(string(role)+" provider").
			Description(runner.describe("Choose the provider brand. Model and voice identities are forwarded without an allowlist.")).
			Options(options...).
			Value(&selected), true)
		if err != nil {
			return err
		}
		if back || selected == actionBack {
			return nil
		}
		distributors := catalog.Distributors(framework, role, selected)
		distributor := binding.Provider
		if !slices.Contains(distributors, distributor) {
			distributor = distributors[0]
		}
		if len(distributors) > 1 {
			distributorOptions := make([]huh.Option[string], 0, len(distributors)+1)
			for _, name := range distributors {
				distributorOptions = append(distributorOptions, huh.NewOption(name, name))
			}
			distributorOptions = append(distributorOptions, huh.NewOption("← Back", actionBack))
			back, err = runner.run(huh.NewSelect[string]().
				Title("Distributor for "+selected).
				Description(runner.describe("Choose the integration that will deliver this provider.")).
				Options(distributorOptions...).
				Value(&distributor), true)
			if err != nil {
				return err
			}
			if back || distributor == actionBack {
				return nil
			}
		}
		binding.Provider = distributor
	} else {
		back, err := runner.input("Provider (optional)", "This role has no fixed provider catalogue; the identity is forwarded.", &binding.Provider, validateBasic)
		if err != nil || back {
			return err
		}
	}

	entry, _ := catalog.Lookup(framework, role, binding.Provider)
	if !integratedListen {
		description := "Forwarded model id."
		validate := validateBasic
		if entry.ModelRequired() || role == targetcap.Reason || role == targetcap.Listen {
			description = "Required by this provider integration; forwarded without an allowlist."
			validate = validateRequiredBasic
		}
		back, err := runner.input("Model", description, &binding.Model, validate)
		if err != nil || back {
			return err
		}
	}

	if role == targetcap.Speak {
		description := "Optional voice name or id; forwarded without an allowlist."
		validate := validateBasic
		if entry.VoiceRequired() {
			description = "Required by this provider integration; enter a voice name or id."
			validate = validateRequiredBasic
		}
		back, err := runner.input("Voice", description, &binding.Voice, validate)
		if err != nil || back {
			return err
		}
	}

	back, err := runner.input("Params (optional JSON object)", "Provider-specific request knobs, for example {\"temperature\":0.2}.", &binding.Params, validateParams)
	if err != nil || back {
		return err
	}
	return nil
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
		choice := actionBack
		options := make([]huh.Option[string], 0, len(data.Variables)+2)
		for _, variable := range data.Variables {
			options = append(options, huh.NewOption(variableLabel(variable), "edit:"+variable.Name))
		}
		options = append(options, huh.NewOption("Add variable", "add"), huh.NewOption("← Back", actionBack))
		_, err := runner.run(huh.NewSelect[string]().Title("Variables").Options(options...).Value(&choice), true)
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
		back, err = runner.run(huh.NewSelect[string]().Title("Variable type").Options(
			huh.NewOption("string", "string"), huh.NewOption("number", "number"),
			huh.NewOption("boolean", "boolean"), huh.NewOption("integer", "integer"),
			huh.NewOption("← Back", actionBack),
		).Value(&variable.Type), true)
		if err != nil {
			return err
		}
		if back || variable.Type == actionBack {
			continue
		}
		back, err = runner.input("Default (optional JSON value)", `Examples: "guest", false, 42. Blank means no default.`, &variable.Default, func(value string) error {
			return validateVariableDefault(variable.Type, value)
		})
		if err != nil || back {
			continue
		}
		source := "none"
		back, err = runner.run(huh.NewSelect[string]().Title("Value source").Options(
			huh.NewOption("No external source", "none"),
			huh.NewOption("Required at call start", "call_start"),
			huh.NewOption("← Back", actionBack),
		).Value(&source), true)
		if err != nil {
			return err
		}
		if back || source == actionBack {
			continue
		}
		if source != "none" {
			variable.Source = source
		}
		data.Variables = append(data.Variables, variable)
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
		choice := actionBack
		source := firstNonempty(variable.Source, "session state")
		_, err := runner.run(huh.NewSelect[string]().
			Title(variable.Name).
			Description("Edit this saved variable. Its name stays stable so existing references do not break.").
			Options(
				huh.NewOption("Type  ·  "+variable.Type, "type"),
				huh.NewOption("Default  ·  "+firstNonempty(variable.Default, "none"), "default"),
				huh.NewOption("Source  ·  "+strings.ReplaceAll(source, "_", " "), "source"),
				huh.NewOption("Delete variable", "delete"),
				huh.NewOption("← Back", actionBack),
			).
			Value(&choice), true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "type":
			selected := variable.Type
			back, err := runner.run(huh.NewSelect[string]().Title("Variable type").Options(
				huh.NewOption("string", "string"), huh.NewOption("number", "number"),
				huh.NewOption("boolean", "boolean"), huh.NewOption("integer", "integer"),
				huh.NewOption("← Back", actionBack),
			).Value(&selected), true)
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
			selected := firstNonempty(variable.Source, "none")
			back, err := runner.run(huh.NewSelect[string]().Title("Value source").Options(
				huh.NewOption("No external source", "none"),
				huh.NewOption("Required at call start", "call_start"),
				huh.NewOption("← Back", actionBack),
			).Value(&selected), true)
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
		choice := actionBack
		options := make([]huh.Option[string], 0, len(data.Tools)+2)
		for _, tool := range data.Tools {
			options = append(options, huh.NewOption(toolLabel(data, tool), "edit:"+tool.Name))
		}
		options = append(options, huh.NewOption("Add tool", "add"), huh.NewOption("← Back", actionBack))
		_, err := runner.run(huh.NewSelect[string]().
			Title("Tools").
			Description(runner.describe("Tools may call a webhook or run local Python when the selected target driver supports it.")).
			Options(options...).
			Value(&choice), true)
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
		if data.Target == string(targetcap.LiveKit) && len(data.Tasks) == 0 {
			if err := showNotice(runner, "Tools unavailable", "LiveKit tools attach to tasks. Add a task first."); err != nil {
				return err
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
		if back, err = runner.input("Description", "What the model sees.", &tool.Description, validateRequiredText); err != nil || back {
			continue
		}
		if back, err = chooseToolExecution(runner, data.Target, &tool); err != nil {
			return err
		} else if back {
			continue
		}
		if tool.ExecutionKind() == "webhook" {
			if back, err = runner.input("Webhook URL env", "Environment variable containing the URL; never the URL itself.", &tool.URLEnv, validateEnvName); err != nil || back {
				continue
			}
		}
		if back, err = runner.input("Input JSON Schema", "JSON object.", &tool.Input, validateRequiredObject); err != nil || back {
			continue
		}
		if back, err = runner.input("Output JSON Schema (optional)", "Blank leaves provider output unconstrained.", &tool.Output, validateParams); err != nil || back {
			continue
		}
		attached := []string{firstNonempty(data.EntryAgent, "assistant")}
		var available []string
		if data.Target == string(targetcap.LiveKit) {
			attached = []string{data.Tasks[0].Name}
			for _, task := range data.Tasks {
				available = append(available, task.Name)
			}
		} else {
			for _, agent := range data.AllAgents() {
				available = append(available, agent.Name)
			}
		}
		attached, back, err = pickReferences(runner, "Attach tool", "Select every place that can call this tool.", available, attached, false)
		if err != nil {
			return err
		}
		if back {
			continue
		}
		if data.Target == string(targetcap.LiveKit) {
			tool.AttachTasks = attached
		} else {
			tool.AttachTo = attached
		}
		data.Tools = append(data.Tools, tool)
	}
}

func toolLabel(data *scaffold.Data, tool scaffold.Tool) string {
	attached := tool.AttachTo
	if data.Target == string(targetcap.LiveKit) {
		attached = tool.AttachTasks
	} else if attached == nil {
		attached = []string{"assistant"}
	}
	return fmt.Sprintf("%s  ·  %s  ·  %s", tool.Name, tool.ExecutionKind(), firstNonempty(strings.Join(attached, ", "), "not attached"))
}

func chooseToolExecution(runner *fieldRunner, target string, tool *scaffold.Tool) (bool, error) {
	for {
		selected := tool.ExecutionKind()
		localNote := localToolNote(target)
		back, err := runner.run(huh.NewSelect[string]().
			Title("Tool execution").
			Description("Choose how this tool runs. Unsupported choices include the exact driver limitation.").
			Options(
				huh.NewOption("Webhook  ·  HTTP endpoint from an environment variable", "webhook"),
				huh.NewOption("Local Python  ·  unavailable: "+localNote, "local"),
				huh.NewOption("← Back", actionBack),
			).
			Value(&selected), true)
		if err != nil || back || selected == actionBack {
			return back || selected == actionBack, err
		}
		if selected == "local" {
			if err := showNotice(runner, "Local Python unavailable", fmt.Sprintf("%s: %s.", targetLabel(target), localNote)); err != nil {
				return false, err
			}
			continue
		}
		tool.Execution = selected
		tool.Handler = ""
		return false, nil
	}
}

func localToolNote(target string) string {
	switch targetcap.Provider(target) {
	case targetcap.LiveKit:
		return "the LiveKit driver currently emits webhook task tools only"
	case targetcap.ElevenLabs:
		return "ElevenLabs cannot host local tool code"
	default:
		return "the Pipecat driver does not emit local tool handlers yet"
	}
}

func editTool(runner *fieldRunner, data *scaffold.Data, tool *scaffold.Tool) error {
	for {
		choice := actionBack
		_, err := runner.run(huh.NewSelect[string]().Title(tool.Name).Options(
			huh.NewOption("Description  ·  "+oneLine(tool.Description), "description"),
			huh.NewOption("Execution  ·  "+tool.ExecutionKind(), "execution"),
			huh.NewOption("Webhook URL env  ·  "+firstNonempty(tool.URLEnv, "none"), "url"),
			huh.NewOption("Input schema  ·  "+oneLine(tool.Input), "input"),
			huh.NewOption("Output schema  ·  "+firstNonempty(oneLine(tool.Output), "unconstrained"), "output"),
			huh.NewOption("Attached to  ·  "+toolAttachmentLabel(data, *tool), "attach"),
			huh.NewOption("Delete tool", "delete"),
			huh.NewOption("← Back", actionBack),
		).Value(&choice), true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "description":
			if _, err := runner.input("Description", "What the model sees.", &tool.Description, validateRequiredText); err != nil {
				return err
			}
		case "execution":
			if _, err := chooseToolExecution(runner, data.Target, tool); err != nil {
				return err
			}
		case "url":
			if tool.ExecutionKind() != "webhook" {
				if err := showNotice(runner, "No webhook URL", "Only webhook tools use a URL environment variable."); err != nil {
					return err
				}
				continue
			}
			if _, err := runner.input("Webhook URL env", "Environment variable containing the URL; never the URL itself.", &tool.URLEnv, validateEnvName); err != nil {
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
			if len(available) == 0 {
				if err := showNotice(runner, "No attachment targets", "Add a task before attaching LiveKit tools."); err != nil {
					return err
				}
				continue
			}
			selected, back, err := pickReferences(runner, "Attach tool", "Select every place that can call this tool.", available, selected, false)
			if err != nil {
				return err
			}
			if !back {
				if data.Target == string(targetcap.LiveKit) {
					tool.AttachTasks, tool.AttachTo = selected, nil
				} else {
					tool.AttachTo, tool.AttachTasks = selected, nil
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
	if data.Target == string(targetcap.LiveKit) {
		return taskNames(data), append([]string(nil), tool.AttachTasks...)
	}
	selected := append([]string(nil), tool.AttachTo...)
	if tool.AttachTo == nil {
		selected = []string{"assistant"}
	}
	return agentNames(data), selected
}

func toolAttachmentLabel(data *scaffold.Data, tool scaffold.Tool) string {
	_, selected := toolAttachmentChoices(data, tool)
	return firstNonempty(strings.Join(selected, ", "), "not attached")
}

func editAgents(runner *fieldRunner, data *scaffold.Data) error {
	for {
		choice := actionBack
		options := make([]huh.Option[string], 0, len(data.AllAgents())+3)
		for _, agent := range data.AllAgents() {
			options = append(options, huh.NewOption(agentLabel(data, agent), "edit:"+agent.Name))
		}
		options = append(options,
			huh.NewOption("Add agent", "add"),
			huh.NewOption("Choose entry agent  ·  "+firstNonempty(data.EntryAgent, "assistant"), "entry"),
			huh.NewOption("← Back", actionBack),
		)
		_, err := runner.run(huh.NewSelect[string]().Title("Agents").
			Description("Select any agent to edit its prompt, LLM, TTS, and tools from one screen.").
			Options(options...).Value(&choice), true)
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
			selected := firstNonempty(data.EntryAgent, "assistant")
			options := agentOptions(data.AllAgents(), "")
			options = append(options, huh.NewOption("← Back", actionBack))
			back, err := runner.run(huh.NewSelect[string]().Title("Entry agent").Options(options...).Value(&selected), true)
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
		if back, err = runner.text("Agent instructions", "Prompt for this specialist.", &agent.Instructions); err != nil || back {
			continue
		}
		if err := editBindingFor(runner, data.Target, targetcap.Reason, &agent.Reason); err != nil {
			return err
		}
		if err := editBindingFor(runner, data.Target, targetcap.Speak, &agent.Speak); err != nil {
			return err
		}
		data.Agents = append(data.Agents, agent)
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
		choice := actionBack
		lifecycle := huh.NewOption("Delete agent", "delete")
		if name == "assistant" {
			lifecycle = huh.NewOption("Reset starter agent", "reset")
		}
		_, err := runner.run(huh.NewSelect[string]().
			Title(name).
			Description("All agent-specific settings are visible here; select a row to change it.").
			Options(
				huh.NewOption("Prompt  ·  "+oneLine(agent.Instructions), "prompt"),
				huh.NewOption("LLM model  ·  "+bindingLabel(agent.Reason), "reason"),
				huh.NewOption("TTS voice  ·  "+bindingLabel(agent.Speak), "speak"),
				huh.NewOption("Tools  ·  "+firstNonempty(strings.Join(agentToolNames(data, name), ", "), "none"), "tools"),
				huh.NewOption("Entry agent  ·  "+yesNo(name == firstNonempty(data.EntryAgent, "assistant")), "entry"),
				lifecycle,
				huh.NewOption("← Back", actionBack),
			).
			Value(&choice), true)
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
			if data.Target == string(targetcap.LiveKit) {
				if err := showNotice(runner, "Tools are task-bound", "LiveKit tools are attached to tasks; edit them from Tasks or Tools."); err != nil {
					return err
				}
				continue
			}
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
		choice := actionBack
		options := make([]huh.Option[string], 0, len(data.Handoffs)+2)
		for _, handoff := range data.Handoffs {
			options = append(options, huh.NewOption(fmt.Sprintf("%s  ·  %s → %s", handoff.Name, handoff.Source, handoff.To), "view:"+handoff.Name))
		}
		options = append(options, huh.NewOption("Add handoff", "add"), huh.NewOption("← Back", actionBack))
		_, err := runner.run(huh.NewSelect[string]().Title("Directional handoffs").Options(options...).Value(&choice), true)
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

		handoff := scaffold.Handoff{History: "full", AllVariables: true}
		handoff.Source = firstNonempty(data.EntryAgent, "assistant")
		sources := agentOptions(data.AllAgents(), "")
		sources = append(sources, huh.NewOption("← Back", actionBack))
		back, err := runner.run(huh.NewSelect[string]().Title("Source agent").Options(sources...).Value(&handoff.Source), true)
		if err != nil {
			return err
		}
		if back || handoff.Source == actionBack {
			continue
		}
		targets := agentOptions(data.AllAgents(), handoff.Source)
		handoff.To = targets[0].Value
		targets = append(targets, huh.NewOption("← Back", actionBack))
		back, err = runner.run(huh.NewSelect[string]().Title("Target agent").Options(targets...).Value(&handoff.To), true)
		if err != nil {
			return err
		}
		if back || handoff.To == actionBack {
			continue
		}
		handoff.Name = "to_" + handoff.To
		back, err = runner.input("Handoff name", "Lowercase snake_case; controls and tools share one namespace.", &handoff.Name, func(value string) error {
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
		if back, err = runner.input("When to hand off", "Plain-language trigger shown to the model.", &handoff.When, validateRequiredText); err != nil || back {
			continue
		}
		if len(data.Variables) > 0 {
			handoff.Requires, back, err = pickReferences(runner, "Required variables (optional)", "The handoff is available only after every selected variable has a value.", variableNames(data), nil, true)
			if err != nil {
				return err
			}
			if back {
				continue
			}
		}
		historyOptions := []huh.Option[string]{huh.NewOption("Full history (portable)", "full")}
		if data.Target != string(targetcap.ElevenLabs) {
			historyOptions = append(historyOptions,
				huh.NewOption("Messages", "messages"), huh.NewOption("Last N messages", "last_n"),
				huh.NewOption("Summary", "summary"), huh.NewOption("Reset", "reset"))
		}
		historyOptions = append(historyOptions, huh.NewOption("← Back", actionBack))
		back, err = runner.run(huh.NewSelect[string]().Title("Conversation history").Options(historyOptions...).Value(&handoff.History), true)
		if err != nil {
			return err
		}
		if back || handoff.History == actionBack {
			continue
		}
		if handoff.History == "last_n" {
			max := "10"
			if back, err = runner.input("Maximum messages", "Positive integer.", &max, validatePositiveInteger); err != nil || back {
				continue
			}
			handoff.MaxMessages, _ = strconv.Atoi(max)
		}
		if handoff.History == "summary" {
			summarizerAgent := data.AllAgents()[0]
			selected := summarizerAgent.ModelProfile()
			options := make([]huh.Option[string], 0, len(data.AllAgents()))
			for _, agent := range data.AllAgents() {
				options = append(options, huh.NewOption(agent.ModelProfile(), agent.ModelProfile()))
			}
			options = append(options, huh.NewOption("← Back", actionBack))
			back, err = runner.run(huh.NewSelect[string]().Title("Summarizer model profile").Options(options...).Value(&selected), true)
			if err != nil {
				return err
			}
			if back || selected == actionBack {
				continue
			}
			handoff.Summarizer = selected
		}
		includeTools := "default"
		back, err = runner.run(huh.NewSelect[string]().Title("Tool calls in context").Options(
			huh.NewOption("Provider default", "default"), huh.NewOption("Include", "yes"), huh.NewOption("Exclude", "no"),
			huh.NewOption("← Back", actionBack),
		).Value(&includeTools), true)
		if err != nil {
			return err
		}
		if back || includeTools == actionBack {
			continue
		}
		if includeTools != "default" {
			include := includeTools == "yes"
			handoff.IncludeToolCalls = &include
		}
		if len(data.Variables) > 0 {
			variableScope := "all"
			back, err = runner.run(huh.NewSelect[string]().Title("Variables in context").
				Description("Available variables: "+strings.Join(variableNames(data), ", ")).Options(
				huh.NewOption("All variables (portable)", "all"), huh.NewOption("Selected variables", "selected"),
				huh.NewOption("← Back", actionBack),
			).Value(&variableScope), true)
			if err != nil {
				return err
			}
			if back || variableScope == actionBack {
				continue
			}
			if variableScope == "selected" {
				handoff.Variables, back, err = pickReferences(runner, "Variables to include", "Choose which saved variables enter the target agent's context.", variableNames(data), nil, false)
				if err != nil {
					return err
				}
				if back {
					continue
				}
				handoff.AllVariables = false
			}
		}
		data.Handoffs = append(data.Handoffs, handoff)
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
		choice := actionBack
		_, err := runner.run(huh.NewSelect[string]().Title(name).Description("Edit this saved handoff or remove it.").Options(
			huh.NewOption("Route  ·  "+handoff.Source+" → "+handoff.To, "route"),
			huh.NewOption("Trigger  ·  "+oneLine(handoff.When), "trigger"),
			huh.NewOption("Required variables  ·  "+firstNonempty(strings.Join(handoff.Requires, ", "), "none"), "requires"),
			huh.NewOption("Context  ·  "+firstNonempty(handoff.History, "full")+" · variables "+handoffVariablesLabel(*handoff), "context"),
			huh.NewOption("Delete handoff", "delete"),
			huh.NewOption("← Back", actionBack),
		).Value(&choice), true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "route":
			source := handoff.Source
			options := agentOptions(data.AllAgents(), "")
			options = append(options, huh.NewOption("← Back", actionBack))
			back, err := runner.run(huh.NewSelect[string]().Title("Source agent").Options(options...).Value(&source), true)
			if err != nil {
				return err
			}
			if back || source == actionBack {
				continue
			}
			targetName := handoff.To
			if targetName == source {
				targetName = ""
			}
			options = agentOptions(data.AllAgents(), source)
			if targetName == "" {
				targetName = options[0].Value
			}
			options = append(options, huh.NewOption("← Back", actionBack))
			back, err = runner.run(huh.NewSelect[string]().Title("Target agent").Options(options...).Value(&targetName), true)
			if err != nil {
				return err
			}
			if !back && targetName != actionBack {
				handoff.Source, handoff.To = source, targetName
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
	history := firstNonempty(handoff.History, "full")
	options := []huh.Option[string]{huh.NewOption("Full history (portable)", "full")}
	if data.Target != string(targetcap.ElevenLabs) {
		options = append(options, huh.NewOption("Messages", "messages"), huh.NewOption("Last N messages", "last_n"), huh.NewOption("Summary", "summary"), huh.NewOption("Reset", "reset"))
	}
	options = append(options, huh.NewOption("← Back", actionBack))
	back, err := runner.run(huh.NewSelect[string]().Title("Conversation history").Options(options...).Value(&history), true)
	if err != nil || back || history == actionBack {
		return err
	}
	handoff.History, handoff.MaxMessages, handoff.Summarizer = history, 0, ""
	if history == "last_n" {
		max := "10"
		back, err = runner.input("Maximum messages", "Positive integer.", &max, validatePositiveInteger)
		if err != nil || back {
			return err
		}
		handoff.MaxMessages, _ = strconv.Atoi(max)
	}
	if history == "summary" {
		profiles := make([]huh.Option[string], 0, len(data.AllAgents())+1)
		for _, agent := range data.AllAgents() {
			profiles = append(profiles, huh.NewOption(agent.ModelProfile(), agent.ModelProfile()))
		}
		handoff.Summarizer = profiles[0].Value
		profiles = append(profiles, huh.NewOption("← Back", actionBack))
		back, err = runner.run(huh.NewSelect[string]().Title("Summarizer model profile").Options(profiles...).Value(&handoff.Summarizer), true)
		if err != nil || back || handoff.Summarizer == actionBack {
			return err
		}
	}
	includeTools := "default"
	if handoff.IncludeToolCalls != nil {
		if *handoff.IncludeToolCalls {
			includeTools = "yes"
		} else {
			includeTools = "no"
		}
	}
	back, err = runner.run(huh.NewSelect[string]().Title("Tool calls in context").Options(
		huh.NewOption("Provider default", "default"), huh.NewOption("Include", "yes"), huh.NewOption("Exclude", "no"), huh.NewOption("← Back", actionBack),
	).Value(&includeTools), true)
	if err != nil || back || includeTools == actionBack {
		return err
	}
	handoff.IncludeToolCalls = nil
	if includeTools != "default" {
		include := includeTools == "yes"
		handoff.IncludeToolCalls = &include
	}
	if len(data.Variables) == 0 {
		handoff.AllVariables, handoff.Variables = false, nil
		return nil
	}
	scope := "selected"
	if handoff.AllVariables {
		scope = "all"
	} else if len(handoff.Variables) == 0 {
		scope = "none"
	}
	back, err = runner.run(huh.NewSelect[string]().Title("Variables in context").Description("Available variables: "+strings.Join(variableNames(data), ", ")).Options(
		huh.NewOption("All variables", "all"), huh.NewOption("Selected variables", "selected"), huh.NewOption("No variables", "none"), huh.NewOption("← Back", actionBack),
	).Value(&scope), true)
	if err != nil || back || scope == actionBack {
		return err
	}
	currentVariables := append([]string(nil), handoff.Variables...)
	handoff.AllVariables, handoff.Variables = scope == "all", nil
	if scope == "selected" {
		handoff.Variables, _, err = pickReferences(runner, "Variables to include", "Choose which saved variables enter the target agent's context.", variableNames(data), currentVariables, false)
	}
	return err
}

func editTasks(runner *fieldRunner, data *scaffold.Data) error {
	for {
		choice := actionBack
		options := make([]huh.Option[string], 0, len(data.Tasks)+2)
		for _, task := range data.Tasks {
			options = append(options, huh.NewOption(taskLabel(task), "view:"+task.Name))
		}
		options = append(options, huh.NewOption("Add task", "add"), huh.NewOption("← Back", actionBack))
		_, err := runner.run(huh.NewSelect[string]().Title("Tasks").Options(options...).Value(&choice), true)
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
		if back, err = runner.text("Task instructions", "Prompt for this delegated task.", &task.Instructions); err != nil || back {
			continue
		}
		if len(data.Tools) > 0 {
			task.Tools, back, err = pickReferences(runner, "Task tools (optional)", "Choose from every saved tool.", toolNames(data), nil, true)
			if err != nil {
				return err
			}
			if back {
				continue
			}
		}
		model := "default"
		modelOptions := []huh.Option[string]{huh.NewOption("Entry agent model", "default")}
		for _, agent := range data.AllAgents() {
			modelOptions = append(modelOptions, huh.NewOption(agent.ModelProfile(), agent.ModelProfile()))
		}
		modelOptions = append(modelOptions, huh.NewOption("← Back", actionBack))
		back, err = runner.run(huh.NewSelect[string]().Title("Task model").Options(modelOptions...).Value(&model), true)
		if err != nil {
			return err
		}
		if back || model == actionBack {
			continue
		}
		if model != "default" {
			task.Model = model
		}
		if back, err = runner.input("Typed result", `Prefilled default: {"result":"string"}. Each key becomes one returned field. Use string, number, boolean, integer, or an enum; for example {"verified":"boolean","tier":{"enum":["free","pro"]}}.`, &task.Result, validateTaskResult); err != nil || back {
			continue
		}
		if back, err = editTaskContext(runner, data, &task); err != nil {
			return err
		} else if back {
			continue
		}
		agentChoices := agentOptions(data.AllAgents(), "")
		agentChoices = append(agentChoices, huh.NewOption("← Back", actionBack))
		back, err = runner.run(huh.NewSelect[string]().Title("Agent allowed to delegate").Options(agentChoices...).Value(&task.Agent), true)
		if err != nil {
			return err
		}
		if back || task.Agent == actionBack {
			continue
		}
		if back, err = runner.input("When to run", "Plain-language trigger shown to the agent.", &task.When, validateRequiredText); err != nil || back {
			continue
		}
		if len(data.Variables) > 0 {
			if back, err = editTaskAssignments(runner, data, &task); err != nil {
				return err
			} else if back {
				continue
			}
		}
		data.Tasks = append(data.Tasks, task)
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
		choice := actionBack
		_, err := runner.run(huh.NewSelect[string]().Title(name).Description("Edit this saved task or remove it.").Options(
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
		).Value(&choice), true)
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
			selected := firstNonempty(task.Model, "default")
			options := []huh.Option[string]{huh.NewOption("Entry agent model", "default")}
			for _, agent := range data.AllAgents() {
				options = append(options, huh.NewOption(agent.ModelProfile(), agent.ModelProfile()))
			}
			options = append(options, huh.NewOption("← Back", actionBack))
			back, err := runner.run(huh.NewSelect[string]().Title("Task model").Options(options...).Value(&selected), true)
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
			if _, err := runner.input("Typed result", `Prefilled default: {"result":"string"}. Use string, number, boolean, integer, or a nonempty enum.`, &task.Result, validateTaskResult); err != nil {
				return err
			}
		case "context":
			if _, err := editTaskContext(runner, data, task); err != nil {
				return err
			}
		case "agent":
			selected := task.Agent
			options := agentOptions(data.AllAgents(), "")
			options = append(options, huh.NewOption("← Back", actionBack))
			back, err := runner.run(huh.NewSelect[string]().Title("Agent allowed to delegate").Options(options...).Value(&selected), true)
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
	history := firstNonempty(task.History, "full")
	options := []huh.Option[string]{huh.NewOption("Full history (portable)", "full")}
	if data.Target != string(targetcap.ElevenLabs) {
		options = append(options,
			huh.NewOption("Messages", "messages"), huh.NewOption("Last N messages", "last_n"),
			huh.NewOption("Summary", "summary"), huh.NewOption("Reset", "reset"))
	}
	options = append(options, huh.NewOption("← Back", actionBack))
	back, err := runner.run(huh.NewSelect[string]().Title("Task history").Options(options...).Value(&history), true)
	if err != nil {
		return false, err
	}
	if back || history == actionBack {
		return true, nil
	}
	task.History, task.MaxMessages, task.Summarizer = history, 0, ""
	if history == "last_n" {
		max := "10"
		back, err = runner.input("Maximum messages", "Positive integer.", &max, validatePositiveInteger)
		if err != nil || back {
			return back, err
		}
		task.MaxMessages, _ = strconv.Atoi(max)
	}
	if history == "summary" {
		task.Summarizer = data.AllAgents()[0].ModelProfile()
		var modelOptions []huh.Option[string]
		for _, agent := range data.AllAgents() {
			modelOptions = append(modelOptions, huh.NewOption(agent.ModelProfile(), agent.ModelProfile()))
		}
		modelOptions = append(modelOptions, huh.NewOption("← Back", actionBack))
		back, err = runner.run(huh.NewSelect[string]().Title("Summarizer model profile").Options(modelOptions...).Value(&task.Summarizer), true)
		if err != nil {
			return false, err
		}
		if back || task.Summarizer == actionBack {
			return true, nil
		}
	}
	includeTools := "default"
	if task.IncludeToolCalls != nil {
		if *task.IncludeToolCalls {
			includeTools = "yes"
		} else {
			includeTools = "no"
		}
	}
	back, err = runner.run(huh.NewSelect[string]().Title("Tool calls in task context").Options(
		huh.NewOption("Provider default", "default"), huh.NewOption("Include", "yes"), huh.NewOption("Exclude", "no"),
		huh.NewOption("← Back", actionBack),
	).Value(&includeTools), true)
	if err != nil {
		return false, err
	}
	if back || includeTools == actionBack {
		return true, nil
	}
	task.IncludeToolCalls = nil
	if includeTools != "default" {
		include := includeTools == "yes"
		task.IncludeToolCalls = &include
	}
	return false, nil
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
		field := fields[0]
		if existing := assignmentField(task.Assign, variable); containsName(fields, existing) {
			field = existing
		}
		options := make([]huh.Option[string], 0, len(fields)+1)
		for _, name := range fields {
			options = append(options, huh.NewOption(name, name))
		}
		options = append(options, huh.NewOption("← Back", actionBack))
		back, err := runner.run(huh.NewSelect[string]().Title("Result field for "+variable).Options(options...).Value(&field), true)
		if err != nil {
			return false, err
		}
		if back || field == actionBack {
			return true, nil
		}
		assignments[variable] = "result." + field
	}
	raw, err := json.Marshal(assignments)
	if err != nil {
		return false, err
	}
	task.Assign = string(raw)
	return false, nil
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
		choice := actionBack
		options := make([]huh.Option[string], 0, len(data.TaskGroups)+2)
		for _, group := range data.TaskGroups {
			options = append(options, huh.NewOption(group.Name+"  ·  "+strings.Join(group.Steps, " → "), "view:"+group.Name))
		}
		options = append(options, huh.NewOption("Add task group", "add"), huh.NewOption("← Back", actionBack))
		_, err := runner.run(huh.NewSelect[string]().Title("Ordered task groups").Options(options...).Value(&choice), true)
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
		group.Steps, back, err = pickReferences(runner, "Ordered steps", "Toggle tasks in the order they should run. Selecting a saved task adds it to the end.", taskNames(data), nil, false)
		if err != nil {
			return err
		}
		if back {
			continue
		}
		scopeOptions := []huh.Option[string]{huh.NewOption("Shared context", "shared")}
		if data.Target != string(targetcap.ElevenLabs) {
			scopeOptions = append(scopeOptions, huh.NewOption("Isolated context", "isolated"))
		}
		scopeOptions = append(scopeOptions, huh.NewOption("← Back", actionBack))
		back, err = runner.run(huh.NewSelect[string]().Title("Context between steps").Options(scopeOptions...).Value(&group.ContextScope), true)
		if err != nil {
			return err
		}
		if back || group.ContextScope == actionBack {
			continue
		}
		back, err = runner.run(huh.NewSelect[string]().Title("After the final step").Options(
			huh.NewOption("Return to caller agent", "return"), huh.NewOption("Transfer to agent", "transfer"), huh.NewOption("End conversation", "end"),
			huh.NewOption("← Back", actionBack),
		).Value(&group.Then), true)
		if err != nil {
			return err
		}
		if back || group.Then == actionBack {
			continue
		}
		if group.Then == "transfer" {
			options := agentOptions(data.AllAgents(), "")
			group.ThenTarget = options[0].Value
			options = append(options, huh.NewOption("← Back", actionBack))
			back, err = runner.run(huh.NewSelect[string]().Title("Transfer target").Options(options...).Value(&group.ThenTarget), true)
			if err != nil {
				return err
			}
			if back || group.ThenTarget == actionBack {
				continue
			}
		}
		agentChoices := agentOptions(data.AllAgents(), "")
		agentChoices = append(agentChoices, huh.NewOption("← Back", actionBack))
		back, err = runner.run(huh.NewSelect[string]().Title("Agent allowed to delegate").Options(agentChoices...).Value(&group.Agent), true)
		if err != nil {
			return err
		}
		if back || group.Agent == actionBack {
			continue
		}
		if back, err = runner.input("When to run", "Plain-language trigger shown to the agent.", &group.When, validateRequiredText); err != nil || back {
			continue
		}
		data.TaskGroups = append(data.TaskGroups, group)
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
		choice := actionBack
		completion := group.Then
		if group.Then == "transfer" {
			completion += " to " + group.ThenTarget
		}
		_, err := runner.run(huh.NewSelect[string]().Title(name).Description("Edit this saved task group or remove it.").Options(
			huh.NewOption("Ordered steps  ·  "+strings.Join(group.Steps, " → "), "steps"),
			huh.NewOption("Context between steps  ·  "+group.ContextScope, "context"),
			huh.NewOption("Completion  ·  "+completion, "completion"),
			huh.NewOption("Delegating agent  ·  "+group.Agent, "agent"),
			huh.NewOption("Trigger  ·  "+oneLine(group.When), "trigger"),
			huh.NewOption("Delete task group", "delete"),
			huh.NewOption("← Back", actionBack),
		).Value(&choice), true)
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
			selected := group.ContextScope
			options := []huh.Option[string]{huh.NewOption("Shared context", "shared")}
			if data.Target != string(targetcap.ElevenLabs) {
				options = append(options, huh.NewOption("Isolated context", "isolated"))
			}
			options = append(options, huh.NewOption("← Back", actionBack))
			back, err := runner.run(huh.NewSelect[string]().Title("Context between steps").Options(options...).Value(&selected), true)
			if err != nil {
				return err
			}
			if !back && selected != actionBack {
				group.ContextScope = selected
			}
		case "completion":
			selected := group.Then
			previousTarget := group.ThenTarget
			back, err := runner.run(huh.NewSelect[string]().Title("After the final step").Options(
				huh.NewOption("Return to caller agent", "return"), huh.NewOption("Transfer to agent", "transfer"), huh.NewOption("End conversation", "end"), huh.NewOption("← Back", actionBack),
			).Value(&selected), true)
			if err != nil {
				return err
			}
			if back || selected == actionBack {
				continue
			}
			group.Then, group.ThenTarget = selected, ""
			if selected == "transfer" {
				targetName := firstNonempty(previousTarget, "assistant")
				options := agentOptions(data.AllAgents(), "")
				options = append(options, huh.NewOption("← Back", actionBack))
				back, err = runner.run(huh.NewSelect[string]().Title("Transfer target").Options(options...).Value(&targetName), true)
				if err != nil {
					return err
				}
				if !back && targetName != actionBack {
					group.ThenTarget = targetName
				}
			}
		case "agent":
			selected := group.Agent
			options := agentOptions(data.AllAgents(), "")
			options = append(options, huh.NewOption("← Back", actionBack))
			back, err := runner.run(huh.NewSelect[string]().Title("Agent allowed to delegate").Options(options...).Value(&selected), true)
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
	mode := "web"
	for _, channel := range data.AllChannels() {
		if channel.Kind != "telephony" {
			continue
		}
		switch {
		case channel.Inbound && channel.Outbound:
			mode = "web_phone_both"
		case channel.Outbound:
			mode = "web_phone_outbound"
		default:
			mode = "web_phone_inbound"
		}
	}
	back, err := runner.run(huh.NewSelect[string]().Title("Caller channels").Description(runner.describe(
		"This declares web/phone behavior only. Phone numbers, SIP trunks, carriers, and rooms remain external setup.",
	)).Options(
		huh.NewOption("Web browser audio", "web"),
		huh.NewOption("Web + inbound phone", "web_phone_inbound"),
		huh.NewOption("Web + outbound phone", "web_phone_outbound"),
		huh.NewOption("Web + inbound/outbound phone", "web_phone_both"),
		huh.NewOption("← Back", actionBack),
	).Value(&mode), true)
	if err != nil || back || mode == actionBack {
		return err
	}
	data.Channels = []scaffold.Channel{{Name: "web", Kind: "realtime_audio"}}
	if mode == "web" {
		return nil
	}
	phone := scaffold.Channel{Name: "phone", Kind: "telephony", Inbound: strings.Contains(mode, "inbound") || mode == "web_phone_both", Outbound: strings.Contains(mode, "outbound") || mode == "web_phone_both"}
	phone.RequiredControls, back, err = pickReferences(runner, "Required phone controls (optional)", "Choose every runtime capability this phone channel requires.", []string{
		"cold_transfer", "warm_transfer", "dtmf_send", "dtmf_receive", "hold", "hangup", "voicemail_detection", "ivr_navigation",
	}, nil, true)
	if err != nil || back {
		return err
	}
	if phone.Outbound {
		phone.OnVoicemail = "hangup"
		back, err = runner.run(huh.NewSelect[string]().Title("When voicemail answers").Options(
			huh.NewOption("Hang up", "hangup"), huh.NewOption("Leave a message", "leave_message"),
			huh.NewOption("← Back", actionBack),
		).Value(&phone.OnVoicemail), true)
		if err != nil || back || phone.OnVoicemail == actionBack {
			return err
		}
	}
	if data.Target == string(targetcap.Pipecat) && data.Transport == "" {
		data.Transport = "daily-sip"
	}
	if back, err = runner.input("Target transport (optional)", "Driver vocabulary; Pipecat cold transfer uses daily-sip.", &data.Transport, validateBasic); err != nil || back {
		return err
	}
	if back, err = runner.input("Carrier (optional)", "Driver vocabulary such as twilio; never provisions the carrier.", &data.Carrier, validateBasic); err != nil || back {
		return err
	}
	data.Channels = append(data.Channels, phone)
	return nil
}

func editHumanTransfers(runner *fieldRunner, data *scaffold.Data) error {
	if !hasTelephony(data) && len(data.HumanTransfers) == 0 {
		return showNotice(runner, "Human transfers unavailable", "Add a telephony caller channel first.")
	}
	for {
		choice := actionBack
		options := make([]huh.Option[string], 0, len(data.HumanTransfers)+2)
		for _, transfer := range data.HumanTransfers {
			options = append(options, huh.NewOption(fmt.Sprintf("%s  ·  %s  ·  %s", transfer.Name, transfer.Agent, transfer.Destination), "view:"+transfer.Name))
		}
		options = append(options, huh.NewOption("Add human transfer", "add"), huh.NewOption("← Back", actionBack))
		_, err := runner.run(huh.NewSelect[string]().Title("Human transfers").Description(runner.describe(
			"Destinations are references in targets.yaml. Unmute does not buy numbers, create trunks, or configure carriers.",
		)).Options(options...).Value(&choice), true)
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
		options = agentOptions(data.AllAgents(), "")
		options = append(options, huh.NewOption("← Back", actionBack))
		back, err = runner.run(huh.NewSelect[string]().Title("Agent allowed to transfer").Options(options...).Value(&transfer.Agent), true)
		if err != nil {
			return err
		}
		if back || transfer.Agent == actionBack {
			continue
		}
		if back, err = runner.input("When to transfer", "Plain-language trigger shown to the agent.", &transfer.When, validateRequiredText); err != nil || back {
			continue
		}
		if back, err = runner.input("Destination name", "Symbolic lowercase snake_case name stored in the portable agent spec.", &transfer.Destination, func(value string) error {
			if err := validateIdentifier(value); err != nil {
				return err
			}
			for _, existing := range data.HumanTransfers {
				if existing.Destination == value {
					return errors.New("destination already exists")
				}
			}
			return nil
		}); err != nil || back {
			continue
		}
		if back, err = runner.input("Destination value", "E.164 number such as +14155550123, or a SIP URI.", &transfer.Value, validateDestination); err != nil || back {
			continue
		}
		modeOptions := []huh.Option[string]{huh.NewOption("Cold transfer", "cold")}
		if data.Target != string(targetcap.Pipecat) {
			modeOptions = append(modeOptions, huh.NewOption("Warm transfer", "warm"))
		}
		modeOptions = append(modeOptions, huh.NewOption("← Back", actionBack))
		back, err = runner.run(huh.NewSelect[string]().Title("Transfer mode").Options(modeOptions...).Value(&transfer.Mode), true)
		if err != nil {
			return err
		}
		if back || transfer.Mode == actionBack {
			continue
		}
		if transfer.Mode == "warm" {
			briefings := []huh.Option[string]{huh.NewOption("Summary", "summary")}
			if data.Target == string(targetcap.ElevenLabs) {
				briefings = []huh.Option[string]{huh.NewOption("Message", "message")}
			}
			transfer.Briefing = briefings[0].Value
			briefings = append(briefings, huh.NewOption("← Back", actionBack))
			back, err = runner.run(huh.NewSelect[string]().Title("Operator briefing").Options(briefings...).Value(&transfer.Briefing), true)
			if err != nil {
				return err
			}
			if back || transfer.Briefing == actionBack {
				continue
			}
		}
		data.HumanTransfers = append(data.HumanTransfers, transfer)
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
		choice := actionBack
		_, err := runner.run(huh.NewSelect[string]().Title(name).Description("Edit this saved human transfer or remove it.").Options(
			huh.NewOption("Agent  ·  "+transfer.Agent, "agent"),
			huh.NewOption("Trigger  ·  "+oneLine(transfer.When), "trigger"),
			huh.NewOption("Destination  ·  "+transfer.Destination, "destination"),
			huh.NewOption("Destination value  ·  "+transfer.Value, "value"),
			huh.NewOption("Mode  ·  "+transfer.Mode, "mode"),
			huh.NewOption("Briefing  ·  "+firstNonempty(transfer.Briefing, "none"), "briefing"),
			huh.NewOption("Delete human transfer", "delete"),
			huh.NewOption("← Back", actionBack),
		).Value(&choice), true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "agent":
			selected := transfer.Agent
			options := agentOptions(data.AllAgents(), "")
			options = append(options, huh.NewOption("← Back", actionBack))
			back, err := runner.run(huh.NewSelect[string]().Title("Agent allowed to transfer").Options(options...).Value(&selected), true)
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
			if _, err := runner.input("Destination value", "E.164 number such as +14155550123, or a SIP URI.", &transfer.Value, validateDestination); err != nil {
				return err
			}
		case "mode":
			selected := transfer.Mode
			options := []huh.Option[string]{huh.NewOption("Cold transfer", "cold")}
			if data.Target != string(targetcap.Pipecat) {
				options = append(options, huh.NewOption("Warm transfer", "warm"))
			}
			options = append(options, huh.NewOption("← Back", actionBack))
			back, err := runner.run(huh.NewSelect[string]().Title("Transfer mode").Options(options...).Value(&selected), true)
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
			selected := firstNonempty(transfer.Briefing, "summary")
			options := []huh.Option[string]{huh.NewOption("Summary", "summary")}
			if data.Target == string(targetcap.ElevenLabs) {
				options = []huh.Option[string]{huh.NewOption("Message", "message")}
			}
			options = append(options, huh.NewOption("← Back", actionBack))
			back, err := runner.run(huh.NewSelect[string]().Title("Operator briefing").Options(options...).Value(&selected), true)
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
		choice := actionBack
		_, err := runner.run(huh.NewSelect[string]().Title("Customize").Description(runner.describe("Optional settings stay collapsed here; starter defaults remain valid.")).Options(
			huh.NewOption("Conversation behavior", "conversation"),
			huh.NewOption(fmt.Sprintf("Model fallbacks  ·  %d", len(data.Fallbacks)), "fallbacks"),
			huh.NewOption("Capacity", "capacity"),
			huh.NewOption("Advanced target settings", "target"),
			huh.NewOption("← Back", actionBack),
		).Value(&choice), true)
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
	data.SpeaksFirst = firstNonempty(data.SpeaksFirst, "agent")
	back, err := runner.run(huh.NewSelect[string]().Title("Who speaks first").Options(
		huh.NewOption("Agent", "agent"), huh.NewOption("Caller", "user"),
		huh.NewOption("← Back", actionBack),
	).Value(&data.SpeaksFirst), true)
	if err != nil || back || data.SpeaksFirst == actionBack {
		return err
	}
	opening := "fixed"
	if data.ModelGreeting {
		opening = "model"
	}
	back, err = runner.run(huh.NewSelect[string]().Title("Opening").Options(
		huh.NewOption("Fixed greeting", "fixed"), huh.NewOption("Model-written greeting", "model"),
		huh.NewOption("← Back", actionBack),
	).Value(&opening), true)
	if err != nil || back || opening == actionBack {
		return err
	}
	if opening == "model" {
		data.Greeting = ""
		data.ModelGreeting = true
	} else {
		data.ModelGreeting = false
		data.Greeting = firstNonempty(data.Greeting, scaffold.DefaultGreeting)
		if back, err = runner.input("Fixed greeting", "Spoken verbatim.", &data.Greeting, validateRequiredText); err != nil || back {
			return err
		}
	}
	interruption := "default"
	if data.Interruption != nil {
		if *data.Interruption {
			interruption = "enabled"
		} else {
			interruption = "disabled"
		}
	}
	back, err = runner.run(huh.NewSelect[string]().Title("Interruption").Options(
		huh.NewOption("Provider default", "default"), huh.NewOption("Enabled", "enabled"), huh.NewOption("Disabled", "disabled"),
		huh.NewOption("← Back", actionBack),
	).Value(&interruption), true)
	if err != nil || back || interruption == actionBack {
		return err
	}
	data.Interruption = nil
	if interruption != "default" {
		enabled := interruption == "enabled"
		data.Interruption = &enabled
		minimum := ""
		if data.MinimumWords > 0 {
			minimum = strconv.Itoa(data.MinimumWords)
		}
		if back, err = runner.input("Minimum interruption words (optional)", "Non-negative integer; provider support varies.", &minimum, validateOptionalNonNegativeInteger); err != nil || back {
			return err
		}
		data.MinimumWords = 0
		if minimum != "" {
			data.MinimumWords, _ = strconv.Atoi(minimum)
		}
		phrases := strings.Join(data.IgnorePhrases, ",")
		if back, err = runner.input("Ignored interruption phrases (optional)", "Comma-separated phrases.", &phrases, func(string) error { return nil }); err != nil || back {
			return err
		}
		data.IgnorePhrases = parsePhrases(phrases)
	}
	for _, field := range []struct {
		title string
		value *string
	}{{"Inactivity nudge after (optional)", &data.NudgeAfter}, {"Inactivity end after (optional)", &data.EndAfter}, {"Maximum call duration (optional)", &data.MaxDuration}} {
		if back, err = runner.input(field.title, "Go duration such as 30s or 20m.", field.value, validateOptionalDuration); err != nil || back {
			return err
		}
	}
	thinking := firstNonempty(data.ThinkingAudio, "none")
	back, err = runner.run(huh.NewSelect[string]().Title("Thinking audio").Options(
		huh.NewOption("None", "none"), huh.NewOption("Subtle", "subtle"),
		huh.NewOption("← Back", actionBack),
	).Value(&thinking), true)
	if err != nil || back || thinking == actionBack {
		return err
	}
	data.ThinkingAudio = ""
	if thinking != "none" {
		data.ThinkingAudio = thinking
	}
	return nil
}

func editFallbacks(runner *fieldRunner, data *scaffold.Data) error {
	for {
		choice := actionBack
		options := make([]huh.Option[string], 0, len(data.Fallbacks)+2)
		for _, fallback := range data.Fallbacks {
			options = append(options, huh.NewOption(fallback.Name+"  ·  protects "+fallback.Profile, "view:"+fallback.Name))
		}
		options = append(options, huh.NewOption("Add fallback", "add"), huh.NewOption("← Back", actionBack))
		_, err := runner.run(huh.NewSelect[string]().Title("Model fallbacks").Description(runner.describe("Fallback support is target-gated and checked by preflight.")).Options(options...).Value(&choice), true)
		if err != nil || choice == actionBack {
			return err
		}
		if strings.HasPrefix(choice, "view:") {
			if err := editFallbackDetails(runner, data, strings.TrimPrefix(choice, "view:")); err != nil {
				return err
			}
			continue
		}
		fallback := scaffold.ModelFallback{Binding: data.Reason}
		profiles := make([]huh.Option[string], 0, len(data.AllAgents()))
		for _, agent := range data.AllAgents() {
			profiles = append(profiles, huh.NewOption(agent.ModelProfile(), agent.ModelProfile()))
		}
		fallback.Profile = profiles[0].Value
		profiles = append(profiles, huh.NewOption("← Back", actionBack))
		back, err := runner.run(huh.NewSelect[string]().Title("Model profile to protect").Options(profiles...).Value(&fallback.Profile), true)
		if err != nil {
			return err
		}
		if back || fallback.Profile == actionBack {
			continue
		}
		for _, agent := range data.AllAgents() {
			if agent.ModelProfile() == fallback.Profile {
				fallback.Binding = agent.Reason
			}
		}
		back, err = runner.input("Fallback profile name", "Lowercase snake_case.", &fallback.Name, func(value string) error {
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
		if err := editBindingFor(runner, data.Target, targetcap.Reason, &fallback.Binding); err != nil {
			return err
		}
		data.Fallbacks = append(data.Fallbacks, fallback)
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
		choice := actionBack
		_, err := runner.run(huh.NewSelect[string]().Title(name).Description("Edit this saved fallback or remove it before retrying preflight.").Options(
			huh.NewOption("Protected model  ·  "+fallback.Profile, "profile"),
			huh.NewOption("Fallback model  ·  "+bindingLabel(fallback.Binding), "binding"),
			huh.NewOption("Delete fallback", "delete"),
			huh.NewOption("← Back", actionBack),
		).Value(&choice), true)
		if err != nil || choice == actionBack {
			return err
		}
		switch choice {
		case "profile":
			selected := fallback.Profile
			profiles := make([]huh.Option[string], 0, len(data.AllAgents())+1)
			for _, agent := range data.AllAgents() {
				profiles = append(profiles, huh.NewOption(agent.ModelProfile(), agent.ModelProfile()))
			}
			profiles = append(profiles, huh.NewOption("← Back", actionBack))
			back, err := runner.run(huh.NewSelect[string]().Title("Model profile to protect").Options(profiles...).Value(&selected), true)
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
	capacity := data.EffectiveCapacity()
	peak, max := strconv.Itoa(capacity.PeakSessions), strconv.Itoa(capacity.MaxSessions)
	back, err := runner.input("Peak sessions", "Positive concurrent conversations at busy hour.", &peak, validatePositiveInteger)
	if err != nil || back {
		return err
	}
	peakValue, _ := strconv.Atoi(peak)
	back, err = runner.input("Maximum sessions", "Positive hard admission limit; must be at least peak.", &max, func(value string) error {
		if err := validatePositiveInteger(value); err != nil {
			return err
		}
		number, _ := strconv.Atoi(value)
		if number < peakValue {
			return errors.New("maximum sessions must be at least peak sessions")
		}
		return nil
	})
	if err != nil || back {
		return err
	}
	maxValue, _ := strconv.Atoi(max)
	back, err = runner.input("Average session duration", "Positive Go duration such as 5m.", &capacity.AvgSessionDuration, validateDuration)
	if err != nil || back {
		return err
	}
	capacity.PeakSessions, capacity.MaxSessions = peakValue, maxValue
	data.Capacity = capacity
	return nil
}

func editAdvancedTarget(runner *fieldRunner, data *scaffold.Data) error {
	for _, field := range []struct {
		title, help string
		value       *string
		validate    func(string) error
	}{
		{"Target version", "Driver/framework version pin.", &data.TargetVersion, validateBasic},
		{"SDK language (optional)", "For example python on LiveKit.", &data.SDKLanguage, validateBasic},
		{"Region (optional)", "Provider vocabulary; forwarded as declared.", &data.Region, validateBasic},
		{"Edition (optional)", "Provider vocabulary; forwarded as declared.", &data.Edition, validateBasic},
		{"Pins (optional JSON object)", "Independently versioned target packages.", &data.Pins, validateParams},
	} {
		back, err := runner.input(field.title, field.help, field.value, field.validate)
		if err != nil || back {
			return err
		}
	}
	return nil
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

var destinationPattern = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$|^sips?:[^@\s]+@[^@\s]+$`)

func validateDestination(value string) error {
	if !destinationPattern.MatchString(value) {
		return errors.New("destination must be an E.164 number or SIP URI")
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
		choice := "done"
		if !allowEmpty && len(selected) == 0 && len(available) > 0 {
			choice = "toggle:" + available[0]
		}
		back, err := runner.run(huh.NewSelect[string]().
			Title(title).
			Description(runner.describe(description+" Toggle choices, then select Done.")).
			Options(options...).
			Value(&choice), true)
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
	choice := actionBack
	back, err := runner.run(huh.NewSelect[string]().Title(title).Description("References are updated so the generated spec stays valid.").Options(
		huh.NewOption(action, "confirm"),
		huh.NewOption("← Back", actionBack),
	).Value(&choice), true)
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
	envNamePattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
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
	requests   chan formRequest
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
		WithAccessible(r.accessible)
	if backable && !r.accessible {
		form.WithKeyMap(backKeyMap())
	}
	var err error
	if r.accessible {
		err = form.WithInput(r.in).WithOutput(r.out).Run()
	} else {
		done := make(chan error, 1)
		r.requests <- formRequest{form: form, done: done}
		err = <-done
	}
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
