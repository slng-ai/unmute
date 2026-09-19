package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"

	"github.com/goccy/go-yaml"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/scaffold"
	"github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// ManifestChoice is one saved manifest offered during initialization.
type ManifestChoice struct {
	Name string
	Data []byte
}

// RunCreateWithManifests keeps the picker and editor in one console session.
func RunCreateWithManifests(in io.Reader, out io.Writer, accessible bool, path string, choices []ManifestChoice, defaultName string, pick bool) (Result, error) {
	runner := newRunner(in, out, accessible)
	flow := func() (Result, error) {
		if len(choices) == 0 {
			return Result{}, fmt.Errorf("no saved manifests; run `unmute manifest create` first")
		}
		raw := choices[0].Data
		if pick {
			var options []menuChoice
			for _, choice := range choices {
				if choice.Name == defaultName {
					options = append(options, newChoice(choice.Name, choice.Name))
				}
			}
			for _, choice := range choices {
				if choice.Name != defaultName {
					options = append(options, newChoice(choice.Name, choice.Name))
				}
			}
			name, _, err := runner.selectOne("Organization manifest", "Choose the contract for this agent.", options, false)
			if err != nil {
				return Result{}, err
			}
			for _, choice := range choices {
				if choice.Name == name {
					raw = choice.Data
					break
				}
			}
		}
		rules, err := spec.ParseManifest(raw)
		if err != nil {
			return Result{}, err
		}
		runner.manifest = rules
		if path == "" {
			back, err := runner.input("Agent name", agentNameHelp, &path, validateName)
			if err != nil || back {
				return Result{}, err
			}
		}
		data := scaffold.Data{Name: filepath.Base(filepath.Clean(path)), Channel: scaffold.DefaultChannel, Greeting: scaffold.DefaultGreeting, Instructions: scaffold.DefaultInstructions, Tools: scaffold.DefaultTools(), Manifest: raw}
		data.SetTarget(scaffold.DefaultTarget)
		if err := guideManifest(runner, &data); err != nil {
			return Result{}, err
		}
		result, _, err := editAgent(runner, Agent{Path: path, Data: data})
		return result, err
	}
	if accessible {
		return flow()
	}
	return runner.runProgram(flow)
}

func manifestPermits(rule *spec.ManifestAllow, value string) bool {
	return rule == nil || slices.Contains(rule.Allow, value)
}

func manifestChoose(runner *fieldRunner, title string, options []menuChoice) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("manifest: no permitted %s; edit the manifest before creating an agent (initialization cannot supply endpoint_env or SLNG think upstream/agent_id; author those packages manually)", title)
	}
	if len(options) == 1 {
		return options[0].value, nil
	}
	value, _, err := runner.selectOne(title, "Choose a value allowed by the organization manifest.", options, false)
	return value, err
}

func manifestTargetOptions(runner *fieldRunner, options []menuChoice) []menuChoice {
	if runner.manifest == nil {
		return options
	}
	return slices.DeleteFunc(options, func(c menuChoice) bool {
		if !manifestPermits(runner.manifest.Targets, c.value) {
			return true
		}
		for _, role := range []targetcap.Role{targetcap.Listen, targetcap.Reason, targetcap.Speak} {
			rows := manifestModelRules(runner.manifest, role)
			if rows == nil {
				continue
			}
			found := false
			for _, row := range rows {
				if _, ok := manifestScaffoldEntry(c.value, role, row.Provider); ok && (row.Allow == nil || len(row.Allow) > 0) {
					found = true
					break
				}
			}
			if !found {
				return true
			}
		}
		return false
	})
}

func manifestModelRules(rules *spec.Manifest, role targetcap.Role) []spec.ManifestModel {
	switch role {
	case targetcap.Listen:
		return rules.Models.Listen
	case targetcap.Speak:
		return rules.Models.Speak
	default:
		return rules.Models.Think
	}
}

func chooseManifestBinding(runner *fieldRunner, target string, role targetcap.Role, binding *scaffold.Binding) error {
	changed := false
	rows := manifestModelRules(runner.manifest, role)
	if rows != nil {
		var options []menuChoice
		var bindings []scaffold.Binding
		for _, row := range rows {
			if _, ok := manifestScaffoldEntry(target, role, row.Provider); !ok {
				continue
			}
			if row.Allow == nil {
				options = append(options, newChoice(row.Provider+" / Enter model ID", fmt.Sprint(len(bindings))))
				bindings = append(bindings, scaffold.Binding{Provider: row.Provider})
			}
			for _, model := range row.Allow {
				options = append(options, newChoice(row.Provider+" / "+model, fmt.Sprint(len(bindings))))
				bindings = append(bindings, scaffold.Binding{Provider: row.Provider, Model: model})
			}
		}
		selected, err := manifestChoose(runner, string(role)+" model", options)
		if err != nil {
			return err
		}
		for i, option := range options {
			if option.value == selected {
				next := bindings[i]
				if next.Model == "" {
					if next.Provider == binding.Provider {
						next.Model = binding.Model
					}
					if _, err := runner.input("Model ID", "All models from this provider are allowed. Enter the exact model ID.", &next.Model, validateRequiredBasic); err != nil {
						return err
					}
				}
				changed = next.Provider != binding.Provider || next.Model != binding.Model
				if next.Provider == binding.Provider && next.Model == binding.Model {
					next.Voice = binding.Voice
					next.Params = binding.Params
					next.Language = binding.Language
				}
				*binding = next
				break
			}
		}
	}
	entry, _ := targetcap.DefaultCatalog().Lookup(targetcap.Provider(target), role, binding.Provider)
	if role == targetcap.Speak && binding.Voice == "" && (entry.VoiceRequired() || changed && entry.Call != nil && entry.Call.Voice.Arg != "") {
		description := "Enter a voice ID for the selected model. Blank uses the provider default."
		validate := validateBasic
		if entry.VoiceRequired() {
			description = "Enter a voice ID for the selected model."
			validate = validateRequiredBasic
		}
		if _, err := runner.input("Voice", description, &binding.Voice, validate); err != nil {
			return err
		}
	}
	if role != targetcap.Reason && runner.manifest.Languages != nil && (entry.Call == nil || entry.Call.Language.Arg != "" && !entry.Call.NoLanguage) {
		var options []menuChoice
		for _, language := range runner.manifest.Languages.Allow {
			options = append(options, newChoice(language, language))
		}
		selected, err := manifestChoose(runner, string(role)+" language", options)
		if err != nil {
			return err
		}
		binding.Language = selected
	}
	if runner.manifest.Regions != nil {
		kind := ir.ModelKind(role)
		if role == targetcap.Reason {
			kind = ir.KindThink
		}
		key := ir.ManifestModelRegionParam(binding.Provider, kind, ir.Provider(target))
		if key != "" {
			for _, row := range runner.manifest.Regions.Models {
				if row.Role == string(kind) && row.Provider == binding.Provider {
					var options []menuChoice
					for _, region := range row.Allow {
						options = append(options, newChoice(region, region))
					}
					region, err := manifestChoose(runner, string(role)+" region", options)
					if err != nil {
						return err
					}
					params := map[string]any{}
					if binding.Params != "" {
						if err := yaml.Unmarshal([]byte(binding.Params), &params); err != nil {
							return fmt.Errorf("model params: %w", err)
						}
					}
					if params == nil {
						params = map[string]any{}
					}
					params[key] = region
					encoded, err := json.Marshal(params)
					if err != nil {
						return err
					}
					binding.Params = string(encoded)
				}
			}
		}
	}
	return nil
}

func guideManifest(runner *fieldRunner, data *scaffold.Data) error {
	selected, err := manifestChoose(runner, "deployment target", manifestTargetOptions(runner, createTargetOptions()))
	if err != nil {
		return err
	}
	data.SetTarget(selected)
	return guideManifestTarget(runner, data)
}

func guideManifestTarget(runner *fieldRunner, data *scaffold.Data) error {
	for _, role := range []targetcap.Role{targetcap.Listen, targetcap.Reason, targetcap.Speak} {
		if err := chooseManifestBinding(runner, data.Target, role, bindingForRole(data, role)); err != nil {
			return err
		}
	}
	if runner.manifest.Regions != nil {
		for _, row := range runner.manifest.Regions.Deployments {
			if row.Provider == data.Target {
				var options []menuChoice
				for _, region := range row.Allow {
					options = append(options, newChoice(region, region))
				}
				region, err := manifestChoose(runner, "deployment region", options)
				if err != nil {
					return err
				}
				data.DeploymentRegions = spec.Regions{{Name: region}}
			}
		}
	}
	if rules := runner.manifest.Tools; rules != nil {
		data.Tools = slices.DeleteFunc(data.Tools, func(tool scaffold.Tool) bool {
			return !manifestPermits(rules.Kinds, tool.Execution) || !manifestPermits(rules.Names, tool.Name) || (tool.Execution == "builtin" && !manifestPermits(rules.Builtin, tool.Builtin))
		})
	}
	dropUnsupportedBuiltins(data)
	if rules := runner.manifest.Tracing; rules != nil {
		options := []menuChoice{newChoice("Disabled", "")}
		for _, provider := range rules.Allow {
			field := targetcap.FieldTracingLangfuse
			if provider == "coval" {
				field = targetcap.FieldTracingCoval
			}
			if targetcap.Default().Capability(field, targetcap.Provider(data.Target)).Tag == targetcap.Gated {
				continue
			}
			options = append(options, newChoice(provider, provider))
		}
		provider, err := manifestChoose(runner, "tracing", options)
		if err != nil {
			return err
		}
		data.Tracing = nil
		if provider != "" {
			data.Tracing = &spec.Tracing{Provider: provider}
		}
	}
	return nil
}

// Initialization cannot collect endpoint or router credentials in a model binding.
func manifestScaffoldEntry(target string, role targetcap.Role, provider string) (targetcap.Entry, bool) {
	entry, ok := targetcap.DefaultCatalog().Lookup(targetcap.Provider(target), role, provider)
	return entry, ok && !entry.RequiresEndpoint && (role != targetcap.Reason || provider != "slng")
}
