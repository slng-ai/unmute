package tui

import (
	"cmp"
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

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/scaffold"
	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
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
	slices.Sort(found)
	return found, nil
}

func openExisting(runner *fieldRunner) error {
	for {
		paths, err := discoverPackages(".")
		if err != nil {
			return fmt.Errorf("discover packages: %w", err)
		}
		options := make([]menuChoice, 0, len(paths)+2)
		for _, path := range paths {
			options = append(options, newChoice(filepath.Base(path), path))
		}
		options = append(options,
			newChoice("Enter a path manually", "manual"),
			newChoice("← Back", actionBack),
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
		Name:              filepath.Base(pkg.Root),
		AgentName:         pkg.Agent.Name,
		Target:            tgt.Provider,
		EntryAgent:        pkg.Agent.EntryAgent,
		TargetVersion:     tgt.Version,
		SDKLanguage:       tgt.SDKLanguage,
		DeploymentRegions: tgt.DeploymentRegion,
		WarmInstances:     tgt.WarmInstances,
		// The route is read from the connection the target names, because that
		// is where a route is declared (spec FR-001).
		Connection: tgt.Connection,
		Transport:  pkg.Connections[tgt.Connection].Transport,
		Carrier:    pkg.Connections[tgt.Connection].Carrier,
	}
	data.Pins = jsonText(tgt.Pins)
	// Read so it survives the rewrite. The console offers no editor for it: the
	// folder is a path on disk the console cannot check and the author already
	// knows. Carrying it is not optional though, because maintain rewrites
	// agent.yaml from this struct and a section absent here is a section deleted.
	for _, name := range slices.Sorted(maps.Keys(pkg.Agent.Knowledge)) {
		base := pkg.Agent.Knowledge[name]
		data.Knowledge = append(data.Knowledge, scaffold.KnowledgeBase{
			Name: name, Documents: base.Documents, Embed: base.Embed,
		})
	}
	if def, ok := effectiveModelDef(pkg, tgt, "listen"); ok {
		data.Listen = scaffoldBinding(def)
	}

	agentNames := slices.Sorted(maps.Keys(pkg.Agent.Agents))
	owners := map[string]string{}
	for _, name := range agentNames {
		agent := pkg.Agent.Agents[name]
		// Every list, because an owner is whoever attached the thing, and that
		// question does not care which kind it is.
		for _, list := range [][]string{agent.Tools, agent.TaskGroups, agent.Handoffs, agent.Escalations} {
			for _, attached := range list {
				if owners[attached] == "" {
					owners[attached] = name
				}
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
		if def, ok := effectiveModelDef(pkg, tgt, definition.Think); ok {
			agent.Reason = scaffoldBinding(def)
		}
		if def, ok := effectiveModelDef(pkg, tgt, definition.Speak); ok {
			agent.Speak = scaffoldBinding(def)
		}
		if name == "assistant" {
			data.Instructions, data.Reason, data.Speak = agent.Instructions, agent.Reason, agent.Speak
			continue
		}
		data.Agents = append(data.Agents, agent)
	}

	for _, name := range slices.Sorted(maps.Keys(pkg.Tools)) {
		tool := pkg.Tools[name]
		// The execution block decides which fields exist; maintenance carries
		// every one of them through, auth included, so a rewrite never drops a
		// tool's credentials (compiler.md V36).
		value := scaffold.Tool{
			Name: name, Description: tool.Description, Execution: tool.ExecutionKind(),
			Input: jsonText(tool.Input), Output: jsonText(tool.Output),
		}
		switch {
		case tool.Webhook != nil:
			value.URLEnv, value.Auth = tool.Webhook.URLEnv, tool.Webhook.Auth
		case tool.Local != nil:
			handlerPath := tool.Local.Handler
			if handlerPath == "" {
				handlerPath = filepath.Join("tools", name+".py")
			}
			value.Handler, value.HandlerSource = tool.Local.Handler, pkg.Handlers[handlerPath]
		case tool.MCP != nil:
			value.URLEnv, value.Auth = tool.MCP.URLEnv, tool.MCP.Auth
			value.MCPTransport, value.MCPTools = tool.MCP.Transport, tool.MCP.Tools
		case tool.Builtin != nil:
			value.Builtin, value.Instructions = tool.Builtin.ID, tool.Builtin.Instructions
		case tool.Knowledge != nil:
			value.KnowledgeBase = tool.Knowledge.Base
		case tool.Slng != nil:
			value.SlngHash = tool.Slng.Hash
		}
		for agentName, definition := range pkg.Agent.Agents {
			if slices.Contains(definition.Tools, name) {
				value.AttachTo = append(value.AttachTo, agentName)
			}
		}
		for taskName, task := range pkg.Tasks {
			if slices.Contains(task.Tools, name) {
				value.AttachTasks = append(value.AttachTasks, taskName)
			}
		}
		slices.Sort(value.AttachTo)
		slices.Sort(value.AttachTasks)
		data.Tools = append(data.Tools, value)
	}

	// A task's agent is where the task is written, so the pairing is a read rather
	// than a lookup through a naming convention.
	definers := map[string]string{}
	for _, agentName := range agentNames {
		for _, item := range pkg.Agent.Agents[agentName].Tasks {
			if item.Task != nil {
				definers[item.Task.Name] = agentName
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(pkg.Tasks)) {
		task := pkg.Tasks[name]
		data.Tasks = append(data.Tasks, scaffold.Task{
			Name: name, Instructions: pkg.Markdown[task.Instructions], Tools: append([]string(nil), task.Tools...),
			Handoffs: append([]string(nil), task.Handoffs...),
			Model:    task.Think, Result: jsonText(task.Result), History: task.Context.History,
			MaxMessages: task.Context.MaxMessages, Summarizer: task.Context.Summarizer,
			IncludeToolCalls: task.Context.IncludeToolCalls,
			Agent:            cmp.Or(definers[name], "assistant"),
			When:             task.When, Announce: task.Announce,
			Requires: append([]string(nil), task.Requires...), Assign: pairsText(task.Assign),
		})
	}
	for _, name := range slices.Sorted(maps.Keys(pkg.Agent.TaskGroups)) {
		group := pkg.Agent.TaskGroups[name]
		data.TaskGroups = append(data.TaskGroups, scaffold.TaskGroup{
			Name: name, Steps: append([]string(nil), group.Steps...), ContextScope: group.ContextScope,
			Then: group.Then, ThenTarget: group.ThenTarget, When: group.When, Announce: group.Announce,
			Agent: cmp.Or(owners[name], "assistant"),
		})
	}

	for _, name := range slices.Sorted(maps.Keys(pkg.Agent.Handoffs)) {
		handoff := pkg.Agent.Handoffs[name]
		value := scaffold.Handoff{Name: name, Source: cmp.Or(owners[name], "assistant"), When: handoff.When, To: handoff.To}
		if handoff.Announce != nil {
			value.Announce = *handoff.Announce
		}
		value.Requires = append([]string(nil), handoff.Requires...)
		if handoff.Context != nil {
			value.History, value.MaxMessages, value.Summarizer = handoff.Context.History, handoff.Context.MaxMessages, handoff.Context.Summarizer
			value.IncludeToolCalls = handoff.Context.IncludeToolCalls
			switch variables := handoff.Context.Variables.(type) {
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
	}
	for _, name := range slices.Sorted(maps.Keys(pkg.Agent.Escalations)) {
		escalation := pkg.Agent.Escalations[name]
		value := scaffold.HumanTransfer{Name: name, Agent: cmp.Or(owners[name], "assistant"), When: escalation.When}
		if destination := escalation.TransferDestination(); destination != "" {
			value.Destination = destination
			// Destinations are declared once for the package, in agent.yaml.
			value.Value = pkg.Agent.Destinations[destination]
		}
		value.Mode = escalation.TransferShape()
		switch {
		case escalation.Cold != nil:
			value.RingTimeout, value.OnUnavailable = escalation.Cold.RingTimeout, escalation.Cold.OnUnavailable
		case escalation.Warm != nil:
			value.Briefing = escalation.Warm.Briefing
			value.RingTimeout, value.OnUnavailable = escalation.Warm.RingTimeout, escalation.Warm.OnUnavailable
		}
		data.HumanTransfers = append(data.HumanTransfers, value)
	}

	for profile, model := range pkg.Agent.Models.Think {
		for _, fallback := range model.Fallback {
			def, _ := effectiveModelDef(pkg, tgt, fallback)
			data.Fallbacks = append(data.Fallbacks, scaffold.ModelFallback{Name: fallback, Profile: profile, Binding: scaffoldBinding(def)})
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

// effectiveModelDef resolves a model name to its per-target definition (N15): a
// target override wins, else the agent.yaml section entry. The pseudo names
// "listen"/"turn" resolve the package's role selection (pointer or sole entry).
func effectiveModelDef(pkg *packagespec.Package, tgt packagespec.Target, name string) (packagespec.ModelDef, bool) {
	switch name {
	case "listen":
		name = selectedRoleName(pkg.Agent.Models.Listen, pkg.Agent.Listen)
	case "turn":
		name = selectedRoleName(pkg.Agent.Models.Turn, pkg.Agent.Turn)
	}
	if name == "" {
		return packagespec.ModelDef{}, false
	}
	if def, ok := tgt.Models[name]; ok {
		return def, true
	}
	for _, section := range []map[string]packagespec.ModelDef{
		pkg.Agent.Models.Think, pkg.Agent.Models.Speak, pkg.Agent.Models.Listen, pkg.Agent.Models.Turn,
	} {
		if def, ok := section[name]; ok {
			return def, true
		}
	}
	return packagespec.ModelDef{}, false
}

// selectedRoleName mirrors ir.Build's selection rule: pointer wins, else the
// sole entry, else nothing (ambiguity is Build's error to raise, not ours).
func selectedRoleName(section map[string]packagespec.ModelDef, pointer string) string {
	if pointer != "" {
		return pointer
	}
	if len(section) == 1 {
		for name := range section {
			return name
		}
	}
	return ""
}

func scaffoldBinding(def packagespec.ModelDef) scaffold.Binding {
	// Carry the typed generation fields through the maintain round-trip as
	// params, so re-saving a package never silently drops temperature et al.
	params := def.Params
	if def.Temperature != nil || def.TopP != nil || def.TopK != nil || def.Speed != nil {
		params = maps.Clone(params)
		if params == nil {
			params = map[string]any{}
		}
		setIfAbsent := func(key string, value any) {
			if _, ok := params[key]; !ok {
				params[key] = value
			}
		}
		if def.Temperature != nil {
			setIfAbsent("temperature", *def.Temperature)
		}
		if def.TopP != nil {
			setIfAbsent("top_p", *def.TopP)
		}
		if def.TopK != nil {
			setIfAbsent("top_k", *def.TopK)
		}
		if def.Speed != nil {
			setIfAbsent("speed", *def.Speed)
		}
	}
	return scaffold.Binding{Provider: def.Provider, Model: def.Model, Voice: def.Voice, Language: def.Language, Params: jsonText(params)}
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

// pairsText flattens an authored pair list into the JSON object the console
// carries it as. Order is the author's, which the console does not preserve
// anyway: it writes the pairs back sorted by key.
func pairsText(pairs []packagespec.Pair) string {
	if len(pairs) == 0 {
		return ""
	}
	out := make(map[string]any, len(pairs))
	for _, pair := range pairs {
		out[pair.Key] = pair.Value
	}
	return jsonText(out)
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
	for path, content := range original.Handlers {
		renderedContent, ok := rendered.Handlers[path]
		if !ok || renderedContent != content {
			losses = append(losses, path+": handler content")
		}
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
		options := editorSectionOptions(agent.data)
		options = append(options,
			newChoice("Validate", "validate"),
			newChoice("Compile", "compile"),
			newChoice("Save", "save"),
			newChoice("← Back", actionBack),
		)
		choice, _, err := runner.selectOne(agent.data.Name, "Maintain existing package; Save regenerates confirmed package files.", options, true)
		if err != nil {
			return err
		}
		if choice == actionBack {
			return nil
		}
		if strings.HasPrefix(choice, "section:") {
			section := strings.TrimPrefix(choice, "section:")
			if section == "models" {
				if err := editModels(runner, &agent.data); err != nil {
					return err
				}
				continue
			}
			choice, err = chooseEditorSection(runner, &agent.data, section)
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
	selected, back, err := runner.selectOne("Target / orchestrator", "", maintainTargetOptions(data.Target), true)
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
	slices.Sort(affected)
	slices.Sort(removals)
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
	slices.Sort(files)
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
	choice, back, err := runner.selectOne("Save regenerated package?", runner.describe(strings.Join(lines, "\n")), []menuChoice{
		newChoice("Rewrite listed files", "confirm"), newChoice("← Back", actionBack),
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
