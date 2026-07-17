package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/scaffold"
	packagespec "github.com/slng/unmute/internal/spec"
	targetcap "github.com/slng/unmute/internal/target"
)

type maintainedAgent struct {
	path    string
	data    scaffold.Data
	initial scaffold.Data
	losses  []string
}

func discoverPackages(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if info, err := os.Stat(filepath.Join(path, "agent.yaml")); err == nil && !info.IsDir() {
			found = append(found, path)
		}
	}
	sort.Strings(found)
	return found, nil
}

func openExisting(runner *fieldRunner) error {
	for {
		paths, err := discoverPackages(".")
		if err != nil {
			return fmt.Errorf("discover packages: %w", err)
		}
		options := make([]huh.Option[string], 0, len(paths)+2)
		for _, path := range paths {
			options = append(options, huh.NewOption(filepath.Base(path), path))
		}
		options = append(options,
			huh.NewOption("Enter a path manually", "manual"),
			huh.NewOption("← Back", actionBack),
		)
		choice, _, err := runner.selectOne("Open an existing agent", "", options, true)
		if err != nil {
			return err
		}
		if choice == actionBack {
			return nil
		}
		if choice == "manual" {
			choice = ""
			back, err := runner.input("Agent package path", "Directory containing agent.yaml.", &choice, func(value string) error {
				if strings.TrimSpace(value) == "" {
					return errors.New("path is required")
				}
				_, err := packagespec.Load(value)
				return err
			})
			if err != nil {
				return err
			}
			if back {
				continue
			}
		}
		agent, err := loadMaintained(choice)
		if err != nil {
			if err := showNotice(runner, "Cannot open agent", err.Error()); err != nil {
				return err
			}
			continue
		}
		if len(agent.losses) > 0 {
			if err := showNotice(runner, "Changed Save will regenerate package files", "Fields the editor cannot preserve:\n\n"+strings.Join(agent.losses, "\n")); err != nil {
				return err
			}
		}
		if err := editMaintained(runner, &agent); err != nil {
			return err
		}
	}
}

func loadMaintained(path string) (maintainedAgent, error) {
	pkg, err := packagespec.Load(path)
	if err != nil {
		return maintainedAgent{}, err
	}
	data, err := packageData(pkg)
	if err != nil {
		return maintainedAgent{}, err
	}
	losses, err := roundTripLosses(pkg, data)
	if err != nil {
		return maintainedAgent{}, err
	}
	return maintainedAgent{path: pkg.Root, data: data, initial: data, losses: losses}, nil
}

func packageData(pkg *packagespec.Package) (scaffold.Data, error) {
	targetNames := slices.Sorted(maps.Keys(pkg.Targets))
	if len(targetNames) == 0 {
		return scaffold.Data{}, errors.New("targets.yaml: no targets declared")
	}
	tgt := pkg.Targets[targetNames[0]]
	data := scaffold.Data{
		Name:          filepath.Base(pkg.Root),
		Target:        tgt.Provider,
		Language:      pkg.Agent.Language,
		EntryAgent:    pkg.Agent.EntryAgent,
		TargetVersion: tgt.Version,
		SDKLanguage:   tgt.SDKLanguage,
		Transport:     tgt.Transport,
		Carrier:       tgt.Carrier,
		Region:        tgt.Region,
		Edition:       tgt.Edition,
	}
	data.Pins = jsonText(tgt.Pins)
	if tgt.Models.Listen != nil {
		data.Listen = scaffoldBinding(*tgt.Models.Listen)
	}

	agentNames := slices.Sorted(maps.Keys(pkg.Agent.Agents))
	owners := map[string]string{}
	for _, name := range agentNames {
		agent := pkg.Agent.Agents[name]
		for _, tool := range agent.Tools {
			if owners[tool] == "" {
				owners[tool] = name
			}
		}
	}
	for name, variable := range pkg.Agent.Variables {
		data.Variables = append(data.Variables, scaffold.Variable{Name: name, Type: variable.Type, Default: jsonText(variable.Default), Source: variable.Source})
	}
	sort.Slice(data.Variables, func(i, j int) bool { return data.Variables[i].Name < data.Variables[j].Name })

	for _, name := range agentNames {
		definition := pkg.Agent.Agents[name]
		agent := scaffold.Agent{Name: name, Instructions: pkg.Markdown[definition.Instructions]}
		agent.Reason = scaffoldBinding(tgt.Models.Reason[definition.Model])
		agent.Speak = scaffoldBinding(tgt.Models.Speak[definition.Voice])
		if name == "assistant" {
			data.Instructions, data.Reason, data.Speak = agent.Instructions, agent.Reason, agent.Speak
			continue
		}
		data.Agents = append(data.Agents, agent)
	}

	for _, name := range slices.Sorted(maps.Keys(pkg.Tools)) {
		tool := pkg.Tools[name]
		value := scaffold.Tool{
			Name: name, Description: tool.Description, Execution: tool.Execution,
			Handler: tool.Handler, URLEnv: tool.URLEnv,
			Input: jsonText(tool.Input), Output: jsonText(tool.Output),
		}
		for agentName, definition := range pkg.Agent.Agents {
			if slices.Contains(definition.Tools, name) {
				value.AttachTo = append(value.AttachTo, agentName)
			}
		}
		for taskName, task := range pkg.Agent.Tasks {
			if slices.Contains(task.Tools, name) {
				value.AttachTasks = append(value.AttachTasks, taskName)
			}
		}
		sort.Strings(value.AttachTo)
		sort.Strings(value.AttachTasks)
		data.Tools = append(data.Tools, value)
	}

	for _, name := range slices.Sorted(maps.Keys(pkg.Agent.Tasks)) {
		task := pkg.Agent.Tasks[name]
		value := scaffold.Task{
			Name: name, Instructions: pkg.Markdown[task.Instructions], Tools: append([]string(nil), task.Tools...),
			Model: task.Model, Result: jsonText(task.Result), History: task.Context.History,
			MaxMessages: task.Context.MaxMessages, Summarizer: task.Context.Summarizer,
			IncludeToolCalls: task.Context.IncludeToolCalls, Agent: "assistant",
		}
		if control, ok := pkg.Agent.Controls["run_"+name]; ok {
			value.When, value.Assign = control.When, jsonText(control.Assign)
			value.Agent = firstNonempty(owners["run_"+name], "assistant")
		}
		data.Tasks = append(data.Tasks, value)
	}
	for _, name := range slices.Sorted(maps.Keys(pkg.Agent.TaskGroups)) {
		group := pkg.Agent.TaskGroups[name]
		value := scaffold.TaskGroup{Name: name, Steps: append([]string(nil), group.Steps...), ContextScope: group.ContextScope, Then: group.Then, ThenTarget: group.ThenTarget, Agent: "assistant"}
		if control, ok := pkg.Agent.Controls["run_"+name]; ok {
			value.When = control.When
			value.Agent = firstNonempty(owners["run_"+name], "assistant")
		}
		data.TaskGroups = append(data.TaskGroups, value)
	}

	for _, name := range slices.Sorted(maps.Keys(pkg.Agent.Controls)) {
		control := pkg.Agent.Controls[name]
		switch control.Kind {
		case "agent_transfer":
			value := scaffold.Handoff{Name: name, Source: firstNonempty(owners[name], "assistant"), When: control.When}
			if control.To != nil {
				value.To = *control.To
			}
			value.Requires = append([]string(nil), control.Requires...)
			if control.Context != nil {
				value.History, value.MaxMessages, value.Summarizer = control.Context.History, control.Context.MaxMessages, control.Context.Summarizer
				value.IncludeToolCalls = control.Context.IncludeToolCalls
				switch variables := control.Context.Variables.(type) {
				case string:
					value.AllVariables = variables == "all"
				case []any:
					for _, item := range variables {
						if text, ok := item.(string); ok {
							value.Variables = append(value.Variables, text)
						}
					}
				}
			}
			data.Handoffs = append(data.Handoffs, value)
		case "human_transfer":
			value := scaffold.HumanTransfer{Name: name, Agent: firstNonempty(owners[name], "assistant"), When: control.When}
			if control.Destination != nil {
				value.Destination = *control.Destination
				value.Value = tgt.Destinations[value.Destination]
			}
			if control.Mode != nil {
				value.Mode = *control.Mode
			}
			if control.Briefing != nil {
				value.Briefing = *control.Briefing
			}
			data.HumanTransfers = append(data.HumanTransfers, value)
		}
	}

	for profile, model := range pkg.Agent.Models {
		for _, fallback := range model.Fallback {
			data.Fallbacks = append(data.Fallbacks, scaffold.ModelFallback{Name: fallback, Profile: profile, Binding: scaffoldBinding(tgt.Models.Reason[fallback])})
		}
	}
	sort.Slice(data.Fallbacks, func(i, j int) bool { return data.Fallbacks[i].Name < data.Fallbacks[j].Name })

	if conversation := pkg.Agent.Conversation; conversation != nil {
		if conversation.Greeting != nil {
			data.SpeaksFirst = conversation.Greeting.SpeaksFirst
			data.Greeting = conversation.Greeting.Text
			data.ModelGreeting = conversation.Greeting.Text == "" && conversation.Greeting.SpeaksFirst == "agent"
		}
		if conversation.Interruption != nil {
			data.Interruption = conversation.Interruption.Enabled
			data.MinimumWords = conversation.Interruption.MinimumWords
			data.IgnorePhrases = append([]string(nil), conversation.Interruption.IgnorePhrases...)
		}
		if conversation.Inactivity != nil {
			data.NudgeAfter, data.EndAfter = conversation.Inactivity.NudgeAfter, conversation.Inactivity.EndAfter
		}
		data.MaxDuration, data.ThinkingAudio = conversation.MaxDuration, conversation.ThinkingAudio
	}
	for _, name := range slices.Sorted(maps.Keys(pkg.Agent.Channels)) {
		channel := pkg.Agent.Channels[name]
		data.Channels = append(data.Channels, scaffold.Channel{Name: name, Kind: channel.Kind, Inbound: boolValue(channel.Inbound), Outbound: boolValue(channel.Outbound), RequiredControls: append([]string(nil), channel.RequiredControls...), OnVoicemail: channel.OnVoicemail})
	}
	if pkg.Agent.Capacity != nil {
		data.Capacity = scaffold.Capacity{PeakSessions: pkg.Agent.Capacity.PeakSessions, MaxSessions: pkg.Agent.Capacity.MaxSessions, AvgSessionDuration: pkg.Agent.Capacity.AvgSessionDuration}
	}
	return data, nil
}

func scaffoldBinding(binding packagespec.Binding) scaffold.Binding {
	voice := binding.Voice
	if voice == "" {
		voice = binding.VoiceID
	}
	return scaffold.Binding{Provider: binding.Provider, Model: binding.Model, Voice: voice, Params: jsonText(binding.Params)}
}

func jsonText(value any) string {
	if value == nil || reflect.ValueOf(value).Kind() == reflect.Map && reflect.ValueOf(value).Len() == 0 {
		return ""
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func boolValue(value *bool) bool { return value != nil && *value }

func roundTripLosses(original *packagespec.Package, data scaffold.Data) ([]string, error) {
	root, err := os.MkdirTemp("", "unmute-maintain-loss-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(root) }()
	candidate := filepath.Join(root, "agent")
	if _, err := scaffold.Write(candidate, data); err != nil {
		return nil, err
	}
	rendered, err := packagespec.Load(candidate)
	if err != nil {
		return nil, err
	}
	var losses []string
	diffJSON("agent.yaml", normalized(original.Agent), normalized(rendered.Agent), &losses)
	diffJSON("targets.yaml", normalized(original.Targets), normalized(rendered.Targets), &losses)
	toolNames := map[string]bool{}
	for name := range original.Tools {
		toolNames[name] = true
	}
	for name := range rendered.Tools {
		toolNames[name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(toolNames)) {
		diffJSON(filepath.Join("tools", name+".yaml"), normalized(original.Tools[name]), normalized(rendered.Tools[name]), &losses)
	}
	for path, content := range original.Markdown {
		if rendered.Markdown[path] != content {
			losses = append(losses, path+": content")
		}
	}
	for path := range original.Handlers {
		losses = append(losses, path+": handler content")
	}
	slices.Sort(losses)
	return slices.Compact(losses), nil
}

func normalized(value any) any {
	encoded, _ := json.Marshal(value)
	var result any
	_ = json.Unmarshal(encoded, &result)
	return result
}

func diffJSON(path string, left, right any, losses *[]string) {
	if reflect.DeepEqual(left, right) {
		return
	}
	lm, lok := left.(map[string]any)
	rm, rok := right.(map[string]any)
	if lok && rok {
		keys := map[string]bool{}
		for key := range lm {
			keys[key] = true
		}
		for key := range rm {
			keys[key] = true
		}
		for _, key := range slices.Sorted(maps.Keys(keys)) {
			next := key
			if path != "" {
				next = path + "." + key
			}
			diffJSON(next, lm[key], rm[key], losses)
		}
		return
	}
	*losses = append(*losses, path)
}

func editMaintained(runner *fieldRunner, agent *maintainedAgent) error {
	for {
		choice, _, err := runner.selectOne(agent.data.Name, "Maintain existing package; Save regenerates confirmed package files.", []huh.Option[string]{
			huh.NewOption("Identity  ·  target, language", "section:identity"),
			huh.NewOption("Models  ·  "+modelsLabel(agent.data), "section:models"),
			huh.NewOption("Behavior  ·  instructions, greeting, variables, advanced", "section:behavior"),
			huh.NewOption("Integrations  ·  tools, channels, human transfers", "section:integrations"),
			huh.NewOption("Lifecycle  ·  agents, handoffs, tasks, groups", "section:lifecycle"),
			huh.NewOption("Validate", "validate"),
			huh.NewOption("Compile", "compile"),
			huh.NewOption("Save", "save"),
			huh.NewOption("← Back", actionBack),
		}, true)
		if err != nil {
			return err
		}
		if choice == actionBack {
			return nil
		}
		if strings.HasPrefix(choice, "section:") {
			var err error
			choice, err = chooseEditorSection(runner, &agent.data, strings.TrimPrefix(choice, "section:"))
			if err != nil {
				return err
			}
			if choice == actionBack {
				continue
			}
		}
		switch choice {
		case "target":
			err = editTarget(runner, &agent.data)
		case "language":
			_, err = runner.input("Language", "Primary spoken BCP-47 language tag, for example en or es-MX.", &agent.data.Language, validateLanguage)
		case "models":
			err = editModels(runner, &agent.data)
		case "prompt":
			_, err = runner.text("Instructions", "Blank keeps the generated default.", &agent.data.Instructions)
		case "greeting":
			_, err = runner.input("Greeting", "", &agent.data.Greeting, validateBasic)
		case "variables":
			err = editVariables(runner, &agent.data)
		case "tools":
			err = editTools(runner, &agent.data)
		case "agents":
			err = editAgents(runner, &agent.data)
		case "handoffs":
			err = editHandoffs(runner, &agent.data)
		case "tasks":
			err = editTasks(runner, &agent.data)
		case "groups":
			err = editTaskGroups(runner, &agent.data)
		case "channels":
			err = editChannels(runner, &agent.data)
		case "humans":
			err = editHumanTransfers(runner, &agent.data)
		case "customize":
			err = editCustomize(runner, &agent.data)
		case "validate", "compile":
			if runner.actions == nil {
				err = showNotice(runner, "Action unavailable", "This console was started without a Validate/Compile handler. Choose Back to continue.")
			} else {
				err = runner.runNotice(strings.ToUpper(choice[:1])+choice[1:], func(out io.Writer) error {
					return runner.actions(choice, agent.path, out)
				})
			}
		case "save":
			err = saveMaintained(runner, agent)
		}
		if err != nil {
			return err
		}
	}
}

func editTarget(runner *fieldRunner, data *scaffold.Data) error {
	selected, back, err := runner.selectOne("Target / orchestrator", "", []huh.Option[string]{
		huh.NewOption("Pipecat", string(targetcap.Pipecat)),
		huh.NewOption("LiveKit", string(targetcap.LiveKit)),
		huh.NewOption("ElevenLabs", string(targetcap.ElevenLabs)),
		huh.NewOption("← Back", actionBack),
	}, true)
	if err == nil && !back && selected != actionBack && selected != data.Target {
		data.SetTarget(selected)
	}
	return err
}

func validateMaintained(path string) error {
	pkg, err := packagespec.Load(path)
	if err != nil {
		return err
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		return err
	}
	names := slices.Sorted(maps.Keys(agent.Targets))
	targets := make([]ir.Target, 0, len(names))
	for _, name := range names {
		targets = append(targets, agent.Targets[name])
	}
	_, err = ir.Validate(agent, targets, targetcap.Default())
	return err
}

func saveMaintained(runner *fieldRunner, agent *maintainedAgent) error {
	if reflect.DeepEqual(agent.data, agent.initial) {
		if err := validateMaintained(agent.path); err != nil {
			return repairPreflight(runner, &agent.data, err)
		}
		return showNotice(runner, "No changes to save", "The package validates and every byte is unchanged.")
	}
	root, err := os.MkdirTemp("", "unmute-maintain-save-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(root) }()
	candidate := filepath.Join(root, "agent")
	created, err := scaffold.Write(candidate, agent.data)
	if err != nil {
		return err
	}
	if err := validateMaintained(candidate); err != nil {
		return repairPreflight(runner, &agent.data, err)
	}
	affected, removals, err := affectedFiles(agent.path, candidate, created)
	if err != nil {
		return err
	}
	if len(affected)+len(removals) == 0 {
		agent.initial = agent.data
		return showNotice(runner, "No bytes changed", "The edited model renders the existing package exactly.")
	}
	confirmed, err := confirmRewrite(runner, affected, removals, agent.losses)
	if err != nil || !confirmed {
		return err
	}
	if err := applyCandidate(agent.path, candidate, affected, removals); err != nil {
		return err
	}
	reloaded, err := loadMaintained(agent.path)
	if err != nil {
		return err
	}
	*agent = reloaded
	return showNotice(runner, "Agent saved", strings.Join(append(affected, removals...), "\n"))
}

func affectedFiles(root, candidate string, created []string) ([]string, []string, error) {
	var affected []string
	candidateSet := map[string]bool{}
	for _, path := range created {
		rel, _ := filepath.Rel(candidate, path)
		candidateSet[rel] = true
		newContent, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		oldContent, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || !slices.Equal(oldContent, newContent) {
			affected = append(affected, rel)
		}
	}
	known, err := knownPackageFiles(root)
	if err != nil {
		return nil, nil, err
	}
	var removals []string
	for _, rel := range known {
		if !candidateSet[rel] {
			removals = append(removals, rel)
		}
	}
	sort.Strings(affected)
	sort.Strings(removals)
	return affected, removals, nil
}

func knownPackageFiles(root string) ([]string, error) {
	var files []string
	for _, rel := range []string{"agent.yaml", "targets.yaml", "instructions.md", ".env.example"} {
		if info, err := os.Stat(filepath.Join(root, rel)); err == nil && !info.IsDir() {
			files = append(files, rel)
		}
	}
	for _, dir := range []string{"agents", "tasks", "tools"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return fs.SkipDir
			}
			if err != nil || entry.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			files = append(files, rel)
			return nil
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

func confirmRewrite(runner *fieldRunner, affected, removals, losses []string) (bool, error) {
	lines := []string{"Rewrite:"}
	for _, path := range affected {
		lines = append(lines, "- "+path)
	}
	for _, path := range removals {
		lines = append(lines, "- "+path+" (delete)")
	}
	if len(losses) > 0 {
		lines = append(lines, "", "Fields that will be lost:")
		for _, loss := range losses {
			lines = append(lines, "- "+loss)
		}
	}
	choice, back, err := runner.selectOne("Save regenerated package?", runner.describe(strings.Join(lines, "\n")), []huh.Option[string]{
		huh.NewOption("Rewrite listed files", "confirm"), huh.NewOption("← Back", actionBack),
	}, true)
	return err == nil && !back && choice == "confirm", err
}

func applyCandidate(root, candidate string, affected, removals []string) error {
	backup, err := os.MkdirTemp("", "unmute-maintain-backup-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(backup) }()
	all := append(append([]string(nil), affected...), removals...)
	existed := map[string]bool{}
	for _, rel := range all {
		content, err := os.ReadFile(filepath.Join(root, rel))
		if err == nil {
			existed[rel] = true
			path := filepath.Join(backup, rel)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, content, 0o644); err != nil {
				return err
			}
		}
	}
	rollback := func() {
		for _, rel := range all {
			path := filepath.Join(root, rel)
			if !existed[rel] {
				_ = os.Remove(path)
				continue
			}
			content, readErr := os.ReadFile(filepath.Join(backup, rel))
			if readErr == nil {
				_ = os.MkdirAll(filepath.Dir(path), 0o755)
				_ = os.WriteFile(path, content, 0o644)
			}
		}
	}
	for _, rel := range affected {
		content, err := os.ReadFile(filepath.Join(candidate, rel))
		if err != nil {
			rollback()
			return err
		}
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			rollback()
			return err
		}
		temporary := path + ".unmute-save"
		if err := os.WriteFile(temporary, content, 0o644); err != nil {
			rollback()
			return err
		}
		if err := os.Rename(temporary, path); err != nil {
			rollback()
			return err
		}
	}
	for _, rel := range removals {
		if err := os.Remove(filepath.Join(root, rel)); err != nil && !errors.Is(err, os.ErrNotExist) {
			rollback()
			return err
		}
	}
	return nil
}
