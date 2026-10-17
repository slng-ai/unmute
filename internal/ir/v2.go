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
			return fmt.Errorf("%s: inject %q names no parameter of %s; add %s to the handler's signature, or remove it here",
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
	if out.Mode != "" && out.Mode != TasksStatus && out.Mode != TasksRecord {
		return fmt.Errorf("%s: mode: %s; write record or status", pkg.KeyLocation("agent.yaml", "mode"), out.Mode)
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
			return fmt.Errorf("%s: task group %q: %w", pkg.Location("agent.yaml", name), name, err)
		}
		for _, step := range raw.NonBlocking {
			if !slices.ContainsFunc(raw.Steps, func(item packagespec.StepItem) bool { return item.Task == step }) {
				return fmt.Errorf("%s: task group %q: non_blocking names %q, which is not one of its steps",
					pkg.Location("agent.yaml", "non_blocking:"), name, step)
			}
		}
		group.Arguments, group.NonBlocking = raw.Arguments, raw.NonBlocking
		out.TaskGroups[name] = group
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
	}
	return nil
}

// buildV2Tool resolves one tool file's assign, replies, precheck and hidden.
func buildV2Tool(pkg *packagespec.Package, out *Agent, name string) error {
	raw, tool := pkg.Tools[name], out.Tools[name]
	file := filepath.Join("tools", name+".yaml")
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
				pkg.Location(file, "hidden:"), field, name, modelFields(output))
		}
	}
	assign, err := assignments(raw.Assign, false)
	if err != nil {
		return fmt.Errorf("%s: %w", pkg.Location(file, "assign:"), err)
	}
	for _, entry := range assign {
		source, err := modelPath(output, entry.Field)
		if err != nil {
			return fmt.Errorf("%s: assign result.%s: %s %w", pkg.Location(file, entry.Field), entry.Field, name, err)
		}
		if err := fitsAssign(entry, source, out); err != nil {
			return fmt.Errorf("%s: %w", pkg.Location(file, entry.Field), err)
		}
	}
	hint := lineHint{assign: "assign: in " + file}
	replies, err := buildReplies(pkg, out, file, raw.Replies, func(reply packagespec.Reply) (string, error) {
		if reply.Tool != "" {
			return "", fmt.Errorf("a reply in %s names tool: %s; a tool's own replies speak for it, so remove the tool: line", file, reply.Tool)
		}
		return name, nil
	}, func(string) lineHint { return hint })
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
	taskAssign := fmt.Sprintf("assign: of task %q", name)
	for i, entry := range raw.Finish {
		site := fmt.Sprintf("finish on %q", entry.Tool)
		for _, line := range entry.Confirm {
			if err := checkLine(pkg, out, "agent.yaml", line, site+" confirm", line, lineHint{assign: taskAssign, beforeTool: true}); err != nil {
				return err
			}
		}
		for _, line := range entry.Say {
			if err := checkLine(pkg, out, "agent.yaml", line, site+" say", line, lineHint{assign: taskAssign}); err != nil {
				return err
			}
		}
		task.Finish[i].Confirm, task.Finish[i].Say = flattenLines(entry.Confirm), flattenLines(entry.Say)
	}
	if err := checkInputAssigns(pkg, out, raw, task.Assign); err != nil {
		return err
	}
	replies, err := buildReplies(pkg, out, "agent.yaml", raw.Replies, func(reply packagespec.Reply) (string, error) {
		if !slices.Contains(raw.Tools, reply.Tool) {
			return "", fmt.Errorf("a reply names tool %q, which the step does not list under tools:; name one of its tools", reply.Tool)
		}
		return reply.Tool, nil
	}, func(tool string) lineHint {
		return lineHint{assign: "assign: in " + filepath.Join("tools", tool+".yaml")}
	})
	if err != nil {
		return err
	}
	opens, err := buildOpensWith(pkg, out, raw)
	if err != nil {
		return err
	}
	inputs, err := buildTaskInputs(pkg, out, raw.Inputs)
	if err != nil {
		return err
	}
	if err := stateNames(out, "arguments", raw.Arguments); err != nil {
		return err
	}
	for _, tool := range raw.Via {
		if !slices.Contains(raw.Tools, tool) {
			return fmt.Errorf("via names %q, which the step does not list under tools:; a via tool is one of the step's own", tool)
		}
	}
	task.Replies, task.OpensWith, task.Inputs, task.Arguments, task.Via = replies, opens, inputs, raw.Arguments, raw.Via
	out.Tasks[name] = task
	return nil
}

// checkInputAssigns holds each `- x: input.<path>` to the input of every tool
// the step finishes on, because the step may close on any of them and the row
// reads the input of whichever one it was.
func checkInputAssigns(pkg *packagespec.Package, out *Agent, raw packagespec.Task, assign []AssignTo) error {
	for _, entry := range assign {
		if !entry.FromInput {
			continue
		}
		if len(raw.Finish) == 0 {
			return fmt.Errorf("assign input.%s reads the input of the tool the step finishes on, and the step has no finish:; add one, or save from result instead", entry.Field)
		}
		for _, finish := range raw.Finish {
			input, err := inputModel(pkg, out, finish.Tool)
			if err != nil {
				return err
			}
			source, err := modelPath(input, entry.Field)
			if err != nil {
				return fmt.Errorf("assign input.%s: %s takes no such input: %w", entry.Field, finish.Tool, err)
			}
			if err := fitsAssign(entry, source, out); err != nil {
				return err
			}
		}
	}
	return nil
}

// buildReplies resolves a replies: list. toolOf says which tool a reply speaks
// for, and hintOf where a value it needs is saved.
func buildReplies(pkg *packagespec.Package, out *Agent, file string, raw []packagespec.Reply,
	toolOf func(packagespec.Reply) (string, error), hintOf func(string) lineHint) ([]Reply, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	replies := make([]Reply, 0, len(raw))
	for _, reply := range raw {
		tool, err := toolOf(reply)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", pkg.Location(file, "replies:"), err)
		}
		if len(reply.Say) == 0 {
			return nil, fmt.Errorf("%s: a reply for %s has no say:; give it at least one line", pkg.Location(file, "replies:"), tool)
		}
		properties, _ := out.Tools[tool].Output["properties"].(map[string]any)
		when := map[string][]string{}
		for _, pair := range reply.When {
			declared, err := declaredEnum(tool, properties, pair.Field)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", pkg.Location(file, "when:"), err)
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
			if err := checkLine(pkg, out, file, line, fmt.Sprintf("a reply for %s", tool), line, hintOf(tool)); err != nil {
				return nil, err
			}
		}
		built := Reply{When: when, Say: flattenLines(reply.Say)}
		if reply.Tool != "" {
			built.Tool = tool
		}
		replies = append(replies, built)
	}
	return replies, nil
}

// buildOpensWith resolves a step's opens_with:.
func buildOpensWith(pkg *packagespec.Package, out *Agent, raw packagespec.Task) (*OpensWith, error) {
	if raw.OpensWith == nil {
		return nil, nil
	}
	opens := raw.OpensWith
	if !slices.Contains(raw.Tools, opens.Tool) {
		return nil, fmt.Errorf("opens_with names %q, which the step does not list under tools; add it there", opens.Tool)
	}
	input, err := inputModel(pkg, out, opens.Tool)
	if err != nil {
		return nil, err
	}
	arguments := map[string]string{}
	for _, pair := range opens.Arguments {
		variable, _ := pair.Value.(string)
		if _, ok := input.Field(pair.Key); !ok {
			return nil, fmt.Errorf("opens_with passes %q, which %s does not take; it takes %s", pair.Key, opens.Tool, modelFields(input))
		}
		if _, ok := out.Variables[variable]; !ok {
			return nil, fmt.Errorf("opens_with passes %s from %q, which State in state.py does not declare; name a State value", pair.Key, variable)
		}
		arguments[pair.Key] = variable
	}
	hint := lineHint{assign: "assign: in " + filepath.Join("tools", opens.Tool+".yaml")}
	for _, line := range opens.Say {
		if err := checkLine(pkg, out, "agent.yaml", line, "opens_with say", line, hint); err != nil {
			return nil, err
		}
	}
	return &OpensWith{Tool: opens.Tool, Arguments: arguments, Say: flattenLines(opens.Say)}, nil
}

// buildTaskInputs resolves a step's inputs:, each with exactly one key.
func buildTaskInputs(pkg *packagespec.Package, out *Agent, raw []packagespec.TaskInput) ([]TaskInput, error) {
	var inputs []TaskInput
	for _, item := range raw {
		var built []TaskInput
		if item.Memory != "" {
			if _, ok := out.Variables[item.Memory]; !ok {
				return nil, fmt.Errorf("inputs reads memory: %s, which State in state.py does not declare", item.Memory)
			}
			built = append(built, TaskInput{Kind: TaskInputMemory, Name: item.Memory})
		}
		if item.Tool != "" {
			if !slices.ContainsFunc(pkg.Agent.Prefetch, func(entry packagespec.Prefetch) bool { return entry.Name == item.Tool }) {
				return nil, fmt.Errorf("inputs reads tool: %s, which is not a prefetch entry; name an entry under prefetch", item.Tool)
			}
			built = append(built, TaskInput{Kind: TaskInputTool, Name: item.Tool})
		}
		if item.Input != "" {
			if strings.TrimSpace(item.Value) == "" {
				return nil, fmt.Errorf("inputs names input: %s with no value:; write the text it reads", item.Input)
			}
			built = append(built, TaskInput{Kind: TaskInputText, Name: item.Input, Value: item.Value})
		} else if item.Value != "" {
			return nil, fmt.Errorf("inputs has value: with no input:; value: is the text of an input: entry")
		}
		if len(built) != 1 {
			return nil, fmt.Errorf("an inputs entry takes exactly one of memory:, tool: and input:, and this one has %d", len(built))
		}
		inputs = append(inputs, built[0])
	}
	return inputs, nil
}

// lineHint says where a spoken line's value is saved: assign names the list to
// add the save to, and beforeTool marks a read-back, which runs before the tool
// and so has no result.
type lineHint struct {
	assign     string
	beforeTool bool
}

// checkLine holds one spoken line to state only. A `{{input.x}}` or
// `{{result.x}}` is refused with the save to write; every other reference is
// checked like any other template, so a name State lacks is refused too.
func checkLine(pkg *packagespec.Package, out *Agent, file, token, site, line string, hint lineHint) error {
	where := pkg.Location(file, firstNonBlank(token, "{{"))
	for _, match := range templatePattern.FindAllStringSubmatch(line, -1) {
		ref := match[1]
		kind, path, found := strings.Cut(ref, ".")
		if !found || (kind != "input" && kind != "result") {
			if !strings.HasPrefix(ref, StatePrefix) && !strings.HasPrefix(ref, "$") {
				return fmt.Errorf("%s: %s reads {{%s}}; a spoken line reads State as {{%s%s}}", where, site, ref, StatePrefix, ref)
			}
			continue
		}
		root, _, _ := strings.Cut(path, ".")
		switch {
		case kind == "result" && hint.beforeTool:
			return fmt.Errorf("%s: %s reads {{%s}}, and a read-back runs before the tool, so there is no result yet. "+
				"Read back the input instead: add \"- %s: input.%s\" under %s, then write {{state.%s}}",
				where, site, ref, root, root, hint.assign, path)
		case hint.assign == "":
			return fmt.Errorf("%s: %s reads {{%s}}, and this line has no tool. A spoken line reads only state: write {{state.<name>}}",
				where, site, ref)
		default:
			return fmt.Errorf("%s: %s reads {{%s}}. A spoken line reads only state: add \"- %s: %s.%s\" under %s, then write {{state.%s}}",
				where, site, ref, root, kind, root, hint.assign, path)
		}
	}
	return checkTemplateSite(pkg, out, file, token, site, line, false, false)
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
