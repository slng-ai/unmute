package ir

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/stateschema"
)

// handlerTypes gives a version 2 local tool the types its handler declares.
// Input is what the model fills in, so an injected parameter is left out of
// it, which is what Input means on version 1 too. Every injected key has to
// be a parameter, because that is where unmute-core's locked value goes.
func handlerTypes(pkg *packagespec.Package, name string, tool *Tool) error {
	if pkg.Agent.Version != packagespec.Version2 || tool.Execution != ToolLocal {
		return nil
	}
	file := filepath.Join("tools", name+".yaml")
	types, ok := pkg.ToolTypes[name]
	if !ok {
		return fmt.Errorf("%s: the handler's types were never read; call Package.ReadState before ir.Build", file)
	}
	properties, _ := types.Input["properties"].(map[string]any)
	for _, key := range slices.Sorted(maps.Keys(tool.Inject)) {
		if _, ok := properties[key]; !ok {
			return fmt.Errorf("%s: with: %q names no parameter of %s; add %s to the handler's signature, or remove it here",
				pkg.Location(file, key), key, tool.HandlerFunction, key)
		}
	}
	tool.Input, tool.Output = modelInput(types.Input, tool.Inject), types.Output
	if tool.Description == "" {
		tool.Description = types.Description
	}
	return nil
}

// modelInput is a handler's input with the injected parameters removed.
func modelInput(input map[string]any, inject map[string]any) map[string]any {
	if len(inject) == 0 {
		return input
	}
	out := maps.Clone(input)
	properties, _ := input["properties"].(map[string]any)
	shown := maps.Clone(properties)
	for key := range inject {
		delete(shown, key)
	}
	out["properties"] = shown
	if required, err := stringSlice(input["required"]); err == nil && required != nil {
		kept := []any{}
		for _, name := range required {
			if _, injected := inject[name]; !injected {
				kept = append(kept, name)
			}
		}
		out["required"] = kept
	}
	return out
}

// callStart resolves a variable's `call_start:` rule. A value dispatched with
// the call has source call_start, and no step confirms it.
func callStart(entry packagespec.Variable, variable *Variable) error {
	switch rule := CallStartRule(entry.CallStart); rule {
	case "":
		return nil
	case CallStartRequired, CallStartOptional:
		if entry.Confirm != "" {
			return fmt.Errorf("%q has call_start: and confirm:, and a value dispatched with the call is not confirmed by a step; remove one", entry.Name)
		}
		if entry.Source != "" && VariableSource(entry.Source) != VariableSourceCallStart {
			return fmt.Errorf("%q has call_start: and source: %s; call_start: is the source, so remove the source: line", entry.Name, entry.Source)
		}
		variable.Source, variable.CallStart = VariableSourceCallStart, rule
		return nil
	default:
		return fmt.Errorf("%q has call_start: %s; write required or optional", entry.Name, entry.CallStart)
	}
}

// buildV2 resolves and checks the version 2 keys, after every tool, task,
// group and prefetch entry is built. Each check names the file and the key to
// fix. The library checks the same things again when the emitted spec.py is
// imported; these exist so the author hears it at validate, with a line.
func buildV2(pkg *packagespec.Package, out *Agent) error {
	if pkg.Agent.Version != packagespec.Version2 {
		return nil
	}
	if strings.TrimSpace(pkg.Agent.Mode) != "" {
		return fmt.Errorf("%s: mode: is removed. Every task runs in record mode: it hands its agent the values "+
			"the caller confirmed, keeps them on a hand-back, and lets the caller go back a step. Delete the line",
			pkg.KeyLocation("agent.yaml", "mode"))
	}
	if err := checkV2Variables(pkg, out); err != nil {
		return err
	}
	for _, name := range sortedKeys(pkg.Tools) {
		if err := buildV2Tool(pkg, out, name); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(pkg.Tasks) {
		if err := buildV2Task(pkg, out, name); err != nil {
			return fmt.Errorf("%s: task %q: %w", pkg.Location("agent.yaml", name), name, err)
		}
	}
	for _, name := range sortedKeys(pkg.Agent.TaskGroups) {
		raw, group := pkg.Agent.TaskGroups[name], out.TaskGroups[name]
		if err := stateNames(out, "arguments", raw.Arguments); err != nil {
			return fmt.Errorf("%s: flow %q: %w", pkg.Location("agent.yaml", name), name, err)
		}
		for _, step := range raw.NonBlocking {
			if !slices.ContainsFunc(raw.Steps, func(item packagespec.StepItem) bool { return item.Task == step }) {
				return fmt.Errorf("%s: flow %q: non_blocking names %q, which is not one of its steps",
					pkg.Location("agent.yaml", "non_blocking:"), name, step)
			}
		}
		group.Arguments, group.NonBlocking = raw.Arguments, raw.NonBlocking
		out.TaskGroups[name] = group
	}
	if entry := pkg.Agent.EntryBy; entry != nil {
		if _, ok := out.Variables[entry.Value]; !ok {
			return fmt.Errorf("%s: entry_by reads %q, which State in state.py does not declare", pkg.KeyLocation("agent.yaml", "entry_by"), entry.Value)
		}
		agents := map[string]string{}
		for _, pair := range entry.Agents {
			agent, _ := pair.Value.(string)
			if _, ok := out.Agents[agent]; !ok {
				return fmt.Errorf("%s: entry_by starts %s on %q, which is not an agent of this package", pkg.KeyLocation("agent.yaml", "entry_by"), pair.Key, agent)
			}
			agents[pair.Key] = agent
		}
		out.EntryBy = &EntryBy{Value: entry.Value, Agents: agents}
	}
	for _, name := range sortedKeys(pkg.Agent.Handoffs) {
		if err := buildV2Handoff(pkg, out, name); err != nil {
			return fmt.Errorf("%s: handoff %q: %w", pkg.Location("agent.yaml", name), name, err)
		}
	}
	if inactivity := pkg.Agent.Conversation; inactivity != nil && inactivity.Inactivity != nil {
		for _, line := range []struct{ key, text string }{
			{"nudge_text", inactivity.Inactivity.NudgeText}, {"end_text", inactivity.Inactivity.EndText},
		} {
			if err := checkLine(pkg, out, "agent.yaml", line.key+":", "conversation.inactivity."+line.key, line.text, lineHint{}); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkV2Variables holds the names a variables: or prefetch: entry lists to
// State.
func checkV2Variables(pkg *packagespec.Package, out *Agent) error {
	for _, entry := range pkg.Agent.Variables {
		if err := stateNames(out, "dependencies", entry.Dependencies); err != nil {
			return fmt.Errorf("%s: %w", pkg.Location("agent.yaml", entry.Name), err)
		}
	}
	for _, entry := range pkg.Agent.Prefetch {
		if err := stateNames(out, "needs_confirmed", entry.NeedsConfirmed); err != nil {
			return fmt.Errorf("%s: prefetch %q: %w", pkg.Location("agent.yaml", "needs_confirmed:"), entry.Name, err)
		}
		if entry.RefuseCall == "" {
			continue
		}
		if entry.Tool == "" {
			return fmt.Errorf("%s: prefetch %q: refuse_call refuses a call whose lookup finds nothing, so it goes on a tool: entry", pkg.Location("agent.yaml", "refuse_call:"), entry.Name)
		}
		if err := checkLine(pkg, out, "agent.yaml", "refuse_call:", "prefetch refuse_call", entry.RefuseCall, lineHint{}); err != nil {
			return err
		}
	}
	return nil
}

// buildV2Tool resolves one tool file's assign, replies, precheck and hidden.
func buildV2Tool(pkg *packagespec.Package, out *Agent, name string) error {
	raw, tool := pkg.Tools[name], out.Tools[name]
	file := filepath.Join("tools", name+".yaml")
	if raw.AnnounceWait && len(raw.Announce) == 0 {
		return fmt.Errorf("%s: start: wait: true waits for the start: say: line, and %s has none; add a say: line or remove wait: true",
			pkg.Location("agent.yaml", "wait:"), name)
	}
	tool.AnnounceWait = raw.AnnounceWait
	if len(raw.Assign) == 0 && len(raw.Replies) == 0 && len(raw.Hidden) == 0 {
		tool.Precheck = raw.Precheck
		out.Tools[name] = tool
		return nil
	}
	output, err := outputModel(name, tool.Output)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	for _, field := range raw.Hidden {
		if _, ok := output.Field(field); !ok {
			return fmt.Errorf("%s: hidden names %q, which %s never returns; it returns %s",
				pkg.Location("agent.yaml", "hidden:"), field, name, modelFields(output))
		}
	}
	where := pkg.Location("agent.yaml", "name: "+name)
	assign, err := assignments(raw.Assign, false)
	if err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	for _, entry := range assign {
		source, err := modelPath(output, entry.Field)
		if err != nil {
			return fmt.Errorf("%s: save result.%s: %s %w", where, entry.Field, name, err)
		}
		if err := fitsAssign(entry, source, out); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
	}
	replies, err := buildReplies(pkg, out, "agent.yaml", raw.Replies, func(packagespec.Reply) (string, error) {
		return name, nil
	})
	if err != nil {
		return err
	}
	tool.Assign, tool.Replies, tool.Precheck, tool.Hidden = assign, replies, raw.Precheck, raw.Hidden
	out.Tools[name] = tool
	return nil
}

// buildV2Task resolves one step's version 2 keys.
func buildV2Task(pkg *packagespec.Package, out *Agent, name string) error {
	raw, task := pkg.Tasks[name], out.Tasks[name]
	for i, entry := range raw.Finish {
		site := fmt.Sprintf("an outcome of %q", entry.Tool)
		for _, line := range entry.Confirm {
			if err := checkLine(pkg, out, "agent.yaml", line, "confirm of "+entry.Tool, line, lineHint{tool: entry.Tool, beforeTool: true}); err != nil {
				return err
			}
		}
		for _, line := range entry.Say {
			if err := checkLine(pkg, out, "agent.yaml", line, site, line, lineHint{tool: entry.Tool}); err != nil {
				return err
			}
		}
		task.Finish[i].Confirm, task.Finish[i].Say = flattenLines(entry.Confirm), flattenLines(entry.Say)
	}
	if err := checkInputAssigns(pkg, out, raw, task.Assign); err != nil {
		return err
	}
	replies, err := buildReplies(pkg, out, "agent.yaml", raw.Replies, func(reply packagespec.Reply) (string, error) {
		return reply.Tool, nil
	})
	if err != nil {
		return err
	}
	opens, err := buildOpensWith(pkg, out, raw.OpensWith, raw.Tools, "the step")
	if err != nil {
		return err
	}
	if err := stateNames(out, "arguments", raw.Arguments); err != nil {
		return err
	}
	task.Replies, task.OpensWith, task.Arguments = replies, opens, raw.Arguments
	out.Tasks[name] = task
	return nil
}

// checkInputAssigns holds each `- x: input.<path>` to the input of the tools the
// step finishes on. One of them has to take it. The row is saved when a tool
// that took it ends the step, as unmute-core reads it, so a step may close on a
// callback with a day or on a note without one.
func checkInputAssigns(pkg *packagespec.Package, out *Agent, raw packagespec.Task, assign []AssignTo) error {
	for _, entry := range assign {
		if !entry.FromInput {
			continue
		}
		if len(raw.Finish) == 0 {
			return fmt.Errorf("save input.%s reads the input of a tool the step ends on, and no outcome ends the step; add one with then: done, or save from result instead", entry.Field)
		}
		var tools []string
		taken := false
		for _, finish := range raw.Finish {
			tools = append(tools, finish.Tool)
			input, err := inputModel(pkg, out, finish.Tool)
			if err != nil {
				return err
			}
			source, err := modelPath(input, entry.Field)
			if err != nil {
				continue
			}
			if err := fitsAssign(entry, source, out); err != nil {
				return err
			}
			taken = true
		}
		if !taken {
			return fmt.Errorf("save input.%s: no tool the step ends on takes that input (%s)", entry.Field, strings.Join(tools, ", "))
		}
	}
	return nil
}

// buildReplies resolves the outcomes that say a line and do not end the step.
// toolOf says which tool each one belongs to.
func buildReplies(pkg *packagespec.Package, out *Agent, file string, raw []packagespec.Reply,
	toolOf func(packagespec.Reply) (string, error)) ([]Reply, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	replies := make([]Reply, 0, len(raw))
	for _, reply := range raw {
		tool, err := toolOf(reply)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", pkg.Location(file, "outcomes:"), err)
		}
		properties, _ := out.Tools[tool].Output["properties"].(map[string]any)
		when := map[string][]string{}
		for _, pair := range reply.When {
			declared, err := declaredEnum(tool, properties, pair.Field)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", pkg.Location(file, "if:"), err)
			}
			for _, value := range pair.Values {
				if !slices.Contains(declared, value) {
					return nil, fmt.Errorf("%s: %s never returns %s: %s; it declares %s",
						pkg.Location(file, value), tool, pair.Field, value, strings.Join(declared, ", "))
				}
			}
			when[pair.Field] = pair.Values
		}
		for _, line := range reply.Say {
			if err := checkLine(pkg, out, file, line, fmt.Sprintf("an outcome of %s", tool), line, lineHint{tool: tool}); err != nil {
				return nil, err
			}
		}
		saves, err := assignments(reply.Save, false)
		if err != nil {
			return nil, fmt.Errorf("%s: an outcome of %s: %w", pkg.Location(file, "save:"), tool, err)
		}
		if err := ownSaveFits(pkg, out, tool, saves); err != nil {
			return nil, fmt.Errorf("%s: an outcome of %s: %w", pkg.Location(file, "save:"), tool, err)
		}
		built := Reply{When: when, Say: flattenLines(reply.Say), Assign: saves}
		if reply.Tool != "" {
			built.Tool = tool
		}
		replies = append(replies, built)
	}
	return replies, nil
}

// buildOpensWith resolves the start: call: of a step or a handoff. tools are
// the tools of whoever runs it, named by owner in a refusal. A with: value
// marked ? is one the call runs without, and the mark has to agree with the
// tool's input schema, so the page says which values the call needs.
func buildOpensWith(pkg *packagespec.Package, out *Agent, opens *packagespec.OpensWith, tools []string, owner string) (*OpensWith, error) {
	if opens == nil {
		return nil, nil
	}
	if !slices.Contains(tools, opens.Tool) {
		return nil, fmt.Errorf("start: calls %q, which %s does not list under tools:; add it there", opens.Tool, owner)
	}
	input, err := inputModel(pkg, out, opens.Tool)
	if err != nil {
		return nil, err
	}
	required := map[string]bool{}
	schema := out.Tools[opens.Tool].Input
	if types, ok := pkg.ToolTypes[opens.Tool]; ok {
		schema = types.Input
	}
	if names, err := stringSlice(schema["required"]); err == nil {
		for _, name := range names {
			required[name] = true
		}
	}
	arguments := map[string]string{}
	for _, pair := range opens.Arguments {
		variable, _ := pair.Value.(string)
		if _, ok := input.Field(pair.Key); !ok {
			return nil, fmt.Errorf("with: passes %q, which %s does not take; it takes %s", pair.Key, opens.Tool, modelFields(input))
		}
		if _, ok := out.Variables[variable]; !ok {
			return nil, fmt.Errorf("with: passes %s from state.%s, which State in state.py does not declare; name a State value", pair.Key, variable)
		}
		marked := slices.Contains(opens.Optional, pair.Key)
		switch {
		case marked && required[pair.Key]:
			return nil, fmt.Errorf("with: marks %s with ?, and %s requires it, so the call cannot run without it. Remove the ?", pair.Key, opens.Tool)
		case !marked && !required[pair.Key]:
			return nil, fmt.Errorf("with: passes %s with no ?, and %s runs without it. Write state.%s? so the page says the call runs when it is empty", pair.Key, opens.Tool, variable)
		}
		arguments[pair.Key] = variable
	}
	return &OpensWith{Tool: opens.Tool, Arguments: arguments, Say: flattenLines(opens.Say)}, nil
}

// lineHint says what a spoken line may read besides State: the tool whose
// input or result it reads, and whether it is a read-back, which runs before
// the tool and so reads the input, not the result.
type lineHint struct {
	tool       string
	beforeTool bool
}

// checkLine holds one spoken line to what it can read. {{state.x}} reads State.
// A line of a tool reads {{result.x}}, or {{input.x}} when it is a read-back,
// and the path has to be one the tool takes or returns. Every State reference
// is checked like any other template, so a name State lacks is refused too.
func checkLine(pkg *packagespec.Package, out *Agent, file, token, site, line string, hint lineHint) error {
	where := pkg.Location(file, firstNonBlank(token, "{{"))
	stateOnly := line
	for _, match := range templatePattern.FindAllStringSubmatch(line, -1) {
		ref := match[1]
		kind, path, found := strings.Cut(ref, ".")
		if !found || (kind != "input" && kind != "result") {
			if !strings.HasPrefix(ref, StatePrefix) && !strings.HasPrefix(ref, "$") {
				return fmt.Errorf("%s: %s reads {{%s}}; a spoken line reads State as {{%s%s}}", where, site, ref, StatePrefix, ref)
			}
			continue
		}
		switch {
		case hint.tool == "":
			return fmt.Errorf("%s: %s reads {{%s}}, and this line belongs to no tool, so it has no input or result. Say a State value as {{state.<name>}}",
				where, site, ref)
		case kind == "result" && hint.beforeTool:
			return fmt.Errorf("%s: %s reads {{%s}}, and a read-back runs before the tool, so there is no result yet. Read the input instead, as {{input.%s}}",
				where, site, ref, path)
		case kind == "input" && !hint.beforeTool:
			return fmt.Errorf("%s: %s reads {{%s}}. A line said after the tool reads its result: say {{result.<field>}}, "+
				"or save the input in the step's save: and say {{state.<name>}}", where, site, ref)
		}
		model, err := outputModel(hint.tool, out.Tools[hint.tool].Output)
		if kind == "input" {
			model, err = inputModel(pkg, out, hint.tool)
		}
		if err == nil {
			_, err = modelPath(model, path)
		}
		if err != nil {
			return fmt.Errorf("%s: %s reads {{%s}}: %s %w", where, site, ref, hint.tool, err)
		}
		stateOnly = strings.Replace(stateOnly, match[0], "", 1)
	}
	return checkTemplateSite(pkg, out, file, token, site, stateOnly, false, false)
}

// flattenLines is FlattenPaths over a list of lines, the emitted form.
func flattenLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = FlattenPaths(line)
	}
	return out
}

// stateNames refuses a name State does not declare.
func stateNames(out *Agent, key string, names []string) error {
	for _, name := range names {
		if _, ok := out.Variables[name]; !ok {
			return fmt.Errorf("%s names %q, which State in state.py does not declare", key, name)
		}
	}
	return nil
}

// outputModel reads a tool's output JSON Schema as a model, so a path into it
// resolves the way a path into State does.
func outputModel(tool string, output map[string]any) (*stateschema.Model, error) {
	if output == nil {
		return nil, fmt.Errorf("%s declares no output, so nothing can read its result", tool)
	}
	return schemaModel(output)
}

// inputModel reads a tool's whole input, injected parameters included, as a
// model: a handler's from its signature, any other tool's from input:.
func inputModel(pkg *packagespec.Package, out *Agent, tool string) (*stateschema.Model, error) {
	input := out.Tools[tool].Input
	if types, ok := pkg.ToolTypes[tool]; ok {
		input = types.Input
	}
	if input == nil {
		return nil, fmt.Errorf("%s declares no input", tool)
	}
	return schemaModel(input)
}

func schemaModel(schema map[string]any) (*stateschema.Model, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	return stateschema.Parse(data)
}

// modelPath resolves a dotted path inside a model.
func modelPath(model *stateschema.Model, path string) (*stateschema.Type, error) {
	root, rest, _ := strings.Cut(path, ".")
	field, ok := model.Field(root)
	if !ok {
		return nil, fmt.Errorf("has no field %q; it has %s", root, modelFields(model))
	}
	if rest == "" {
		return field.Type, nil
	}
	return field.Type.Path(strings.Split(rest, "."))
}

func modelFields(model *stateschema.Model) string {
	names := make([]string, 0, len(model.Fields))
	for _, field := range model.Fields {
		names = append(names, field.Name)
	}
	return strings.Join(names, ", ")
}

// buildV2Handoff resolves a handoff's saves: and opens_with:. A saved value is
// a State value, and the opening tool is one the next agent has.
func buildV2Handoff(pkg *packagespec.Package, out *Agent, name string) error {
	raw := pkg.Agent.Handoffs[name]
	transfer, ok := out.Controls[name].(*AgentTransfer)
	if !ok || (len(raw.Saves) == 0 && raw.OpensWith == nil) {
		return nil
	}
	if len(raw.Saves) > 0 {
		transfer.Saves = make(map[string]any, len(raw.Saves))
	}
	for _, pair := range raw.Saves {
		if _, ok := out.Variables[pair.Key]; !ok {
			return fmt.Errorf("saves %q, which State in state.py does not declare", pair.Key)
		}
		transfer.Saves[pair.Key] = pair.Value
	}
	opens, err := buildOpensWith(pkg, out, raw.OpensWith, out.Agents[raw.To].Tools, fmt.Sprintf("agent %q", raw.To))
	if err != nil {
		return err
	}
	transfer.OpensWith = opens
	return nil
}
