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
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/slng/unmute/internal/scaffold"
	targetcap "github.com/slng/unmute/internal/target"
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
		data := scaffold.Data{
			Name:         filepath.Base(filepath.Clean(path)),
			Language:     scaffold.DefaultLanguage,
			Channel:      scaffold.DefaultChannel,
			Greeting:     scaffold.DefaultGreeting,
			Instructions: scaffold.DefaultInstructions,
		}
		data.SetTarget(scaffold.DefaultTarget)
		agent := Agent{Path: path, Data: data}
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
				huh.NewOption("Target  ·  "+targetLabel(result.Agent.Data.Target), "target"),
				huh.NewOption("Language  ·  "+result.Agent.Data.Language, "language"),
				huh.NewOption("Models  ·  "+modelsLabel(result.Agent.Data), "models"),
				huh.NewOption("Instructions (prompt)", "prompt"),
				huh.NewOption("Greeting  ·  "+result.Agent.Data.Greeting, "greeting"),
				huh.NewOption(fmt.Sprintf("Variables  ·  %d", len(result.Agent.Data.Variables)), "variables"),
				huh.NewOption(fmt.Sprintf("Webhook tools  ·  %d", len(result.Agent.Data.Tools)), "tools"),
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
		case "compile":
			result.Compile = !result.Compile
		case "save":
			review, err := scaffold.Preflight(result.Agent.Data)
			if err != nil {
				fmt.Fprintf(runner.out, "\nPreflight failed:\n%s\n", err)
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

func summary(result Result, review scaffold.PreflightReport) string {
	compile := "no"
	if result.Compile {
		compile = "yes (selected target)"
	}
	var text strings.Builder
	fmt.Fprintf(&text, "Create %s?\nTarget: %s (%s)\nLanguage: %s\nGreeting: %s\nRequired env: %s\nForwarded bindings:",
		result.Agent.Data.Name, targetLabel(result.Agent.Data.Target), review.TargetName,
		result.Agent.Data.Language, result.Agent.Data.Greeting, strings.Join(review.RequiredEnv, ", "))
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
	label := func(binding scaffold.Binding) string {
		if binding.Provider == "" {
			if binding.Model != "" {
				return "forwarded"
			}
			return "integrated"
		}
		return binding.Provider
	}
	return label(data.Listen) + " / " + label(data.Reason) + " / " + label(data.Speak)
}

func editModels(runner *fieldRunner, data *scaffold.Data) error {
	for {
		choice := actionBack
		_, err := runner.run(huh.NewSelect[string]().
			Title("STT / LLM / TTS").
			Description(runner.describe("Providers come from the selected target's catalogue. Model ids, voices, and params are forwarded as entered.")).
			Options(
				huh.NewOption("Listen (STT)  ·  "+modelsLabelPart(data.Listen), string(targetcap.Listen)),
				huh.NewOption("Reason (LLM)  ·  "+modelsLabelPart(data.Reason), string(targetcap.Reason)),
				huh.NewOption("Speak (TTS)  ·  "+modelsLabelPart(data.Speak), string(targetcap.Speak)),
				huh.NewOption("← Back", actionBack),
			).
			Value(&choice), true)
		if err != nil {
			return err
		}
		if choice == actionBack {
			return nil
		}
		if err := editBinding(runner, data, targetcap.Role(choice)); err != nil {
			return err
		}
	}
}

func modelsLabelPart(binding scaffold.Binding) string {
	if binding.Provider != "" {
		return binding.Provider
	}
	return "integrated / forwarded"
}

func editBinding(runner *fieldRunner, data *scaffold.Data, role targetcap.Role) error {
	binding := bindingForRole(data, role)
	framework := targetcap.Provider(data.Target)
	options := providerOptions(framework, role)
	integratedListen := framework == targetcap.ElevenLabs && role == targetcap.Listen

	if integratedListen {
		runner.describe("ElevenLabs STT is integrated; only its optional params are configurable.")
	} else if len(options) > 0 {
		selected := binding.Provider
		options = append(options, huh.NewOption("← Back", actionBack))
		back, err := runner.run(huh.NewSelect[string]().
			Title(string(role)+" provider").
			Description(runner.describe("Provider integrations listed by internal/target; model and voice identities are not catalogued.")).
			Options(options...).
			Value(&selected), true)
		if err != nil {
			return err
		}
		if back || selected == actionBack {
			return nil
		}
		binding.Provider = selected
	} else {
		back, err := runner.input("Provider (optional)", "This role has no fixed provider catalogue; the identity is forwarded.", &binding.Provider, validateBasic)
		if err != nil || back {
			return err
		}
	}

	entry, _ := targetcap.DefaultCatalog().Lookup(framework, role, binding.Provider)
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
	vendors := targetcap.DefaultCatalog().Vendors(framework, role)
	options := make([]huh.Option[string], 0, len(vendors))
	for _, vendor := range vendors {
		options = append(options, huh.NewOption(vendor, vendor))
	}
	return options
}

func editVariables(runner *fieldRunner, data *scaffold.Data) error {
	for {
		choice := actionBack
		_, err := runner.run(huh.NewSelect[string]().Title("Variables").Options(
			huh.NewOption("Add variable", "add"),
			huh.NewOption("← Back", actionBack),
		).Value(&choice), true)
		if err != nil || choice == actionBack {
			return err
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

func editTools(runner *fieldRunner, data *scaffold.Data) error {
	if data.Target == string(targetcap.LiveKit) {
		fmt.Fprintln(runner.out, "Webhook tools on agents are unavailable for LiveKit: its current driver emits webhook tools on tasks only.")
		return nil
	}
	for {
		choice := actionBack
		_, err := runner.run(huh.NewSelect[string]().
			Title("Webhook tools").
			Description(runner.describe("New tools attach to the current entry agent.")).
			Options(huh.NewOption("Add webhook tool", "add"), huh.NewOption("← Back", actionBack)).
			Value(&choice), true)
		if err != nil || choice == actionBack {
			return err
		}
		tool := scaffold.Tool{Input: `{"type":"object","properties":{}}`}
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
		if back, err = runner.input("Webhook URL env", "Environment variable containing the URL; never the URL itself.", &tool.URLEnv, validateEnvName); err != nil || back {
			continue
		}
		if back, err = runner.input("Input JSON Schema", "JSON object.", &tool.Input, validateRequiredObject); err != nil || back {
			continue
		}
		if back, err = runner.input("Output JSON Schema (optional)", "Blank leaves provider output unconstrained.", &tool.Output, validateParams); err != nil || back {
			continue
		}
		data.Tools = append(data.Tools, tool)
	}
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
