package generate

import (
	"cmp"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// coreSources maps an authored prefetch source onto the unmute-core CallInfo
// field a CallValue reads.
var coreSources = map[ir.VariableSource]string{
	ir.VariableSourceFromNumber: "calling_number",
	ir.VariableSourceToNumber:   "called_number",
	ir.VariableSourceDirection:  "direction",
	ir.VariableSourceCallID:     "carrier_call_id",
	ir.VariableSourceSessionID:  "session_id",
}

// linePlaceholder is one value a spoken line reads, as the IR writes it:
// {{root}} or {{root__field__field}}.
var linePlaceholder = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*?)((?:__[A-Za-z_][A-Za-z0-9_]*)*)\s*\}\}`)

// coreSpec lowers a version 2 agent to agent.json: the data the emitted
// spec.py turns into unmute-core's objects. Each key is named as the library
// names it, so spec.py passes most of it through unchanged.
func coreSpec(agent *ir.Agent) (map[string]any, error) {
	tools, err := coreTools(agent)
	if err != nil {
		return nil, err
	}
	agents := map[string]any{}
	for _, name := range sortedKeys(agent.Agents) {
		built, err := coreAgent(agent, name)
		if err != nil {
			return nil, fmt.Errorf("agent %q: %w", name, err)
		}
		agents[name] = built
	}
	prefetch, err := corePrefetch(agent)
	if err != nil {
		return nil, err
	}
	spec := map[string]any{
		"agent_id": agent.Name,
		"entry":    agent.EntryAgent,
		"tools":    tools,
		"agents":   agents,
		"state":    coreState(agent),
		"prefetch": prefetch,
		"handoffs": coreHandoffs(agent),
	}
	if agent.EntryBy != nil {
		spec["entry_by"] = agent.EntryBy
	}
	if agent.Tracing != nil && agent.Tracing.Provider != "langfuse" {
		return nil, fmt.Errorf("tracing: version 2 traces to langfuse only so far, and this is %s", agent.Tracing.Provider)
	}
	if len(agent.Knowledge) > 0 {
		knowledge, err := coreKnowledge(agent)
		if err != nil {
			return nil, err
		}
		spec["knowledge"] = knowledge
	}
	if conversation := agent.Conversation; conversation != nil {
		if greeting := conversation.Greeting; greeting != nil {
			built := map[string]any{"speaks_first": string(greeting.SpeaksFirst)}
			putString(built, "text", stateLine(greeting.Text))
			putString(built, "instruction", greeting.Instruction)
			if greeting.Interruptible != nil {
				built["interruptible"] = *greeting.Interruptible
			}
			spec["greeting"] = built
		}
		if idle, err := coreIdle(conversation.Inactivity); err != nil {
			return nil, err
		} else if idle != nil {
			spec["idle"] = idle
		}
		if conversation.MaxDuration != "" {
			seconds, err := coreSeconds(conversation.MaxDuration)
			if err != nil {
				return nil, fmt.Errorf("conversation.max_duration: %w", err)
			}
			spec["max_duration_s"] = seconds
		}
	}
	return spec, nil
}

// coreTools lowers every local tool to its local_tool options and its own
// assign rows. Only local tools reach unmute-core this way so far.
func coreTools(agent *ir.Agent) ([]any, error) {
	var out []any
	for _, name := range sortedKeys(agent.Tools) {
		tool := agent.Tools[name]
		switch tool.Execution {
		case ir.ToolBuiltin:
			if tool.Builtin != "end_call" {
				return nil, fmt.Errorf("tool %q: version 2 compiles the end_call builtin only so far, and this one is %s", name, tool.Builtin)
			}
			continue // the framework's own end-call tool, given to each agent that lists it
		case ir.ToolKnowledge:
			out = append(out, map[string]any{
				"knowledge": tool.KnowledgeBase,
				"options":   map[string]any{"name": name, "description": tool.Description},
			})
			continue
		}
		if tool.Execution != ir.ToolLocal && tool.Execution != ir.ToolWebhook {
			return nil, fmt.Errorf("tool %q: version 2 compiles local and webhook tools only so far, and this one is %s", name, tool.Execution)
		}
		if tool.Execution == ir.ToolWebhook && (len(tool.Replies) > 0 || tool.Precheck != "") {
			return nil, fmt.Errorf("tool %q: replies and precheck run on a local tool only; unmute-core's webhook tool takes neither", name)
		}
		options := map[string]any{"name": name}
		putString(options, "description", tool.Description)
		if len(tool.Announce) > 0 {
			options["announce"] = stateLines(tool.Announce)
		}
		if len(tool.Inject) > 0 {
			locked := map[string]any{}
			for key, value := range tool.Inject {
				if text, ok := value.(string); ok {
					locked[key] = stateLine(text)
					continue
				}
				locked[key] = value
			}
			options["locked"] = locked
		}
		if len(tool.Hidden) > 0 {
			options["hidden"] = tool.Hidden
		}
		if tool.AnnounceWait {
			options["announce_wait"] = true
		}
		putString(options, "effect", string(tool.Effect))
		putString(options, "interruption", string(tool.Interruption))
		own := resultPaths(tool.Assign)
		var replies []any
		for _, reply := range tool.Replies {
			when, err := typedMatch(tool.Output, reply.When)
			if err != nil {
				return nil, fmt.Errorf("tool %q: %w", name, err)
			}
			replies = append(replies, map[string]any{"when": when, "say": rewriteLines(reply.Say, nil, own)})
		}
		if len(replies) > 0 {
			options["replies"] = replies
		}
		built := map[string]any{"options": options}
		if tool.Execution == ir.ToolWebhook {
			built["webhook"] = coreWebhook(tool)
			built["input"], built["output"] = tool.Input, tool.Output
		} else {
			built["handler"] = tool.Handler + ":" + tool.HandlerFunction
		}
		putString(built, "precheck", tool.Precheck)
		if len(tool.Assign) > 0 {
			built["assign"] = coreAssigns(tool.Assign)
		}
		out = append(out, built)
	}
	return out, nil
}

// coreAgent lowers one agent: its prompt, its own tools, the tasks and groups
// it offers as one TasksSpec, and the handoffs it offers.
func coreAgent(agent *ir.Agent, name string) (map[string]any, error) {
	def := agent.Agents[name]
	var own, handoffs, transfers []string
	var endCall map[string]any
	tasks, groups := []any{}, []any{}
	steps := map[string]bool{}
	for _, item := range def.Tools {
		if tool, ok := agent.Tools[item]; ok {
			if tool.Execution == ir.ToolBuiltin {
				endCall = map[string]any{"name": item, "description": tool.Description, "instructions": tool.Instructions}
				continue
			}
			own = append(own, item)
			continue
		}
		switch control := agent.Controls[item].(type) {
		case *ir.HumanTransfer:
			transfers = append(transfers, item)
		case *ir.AgentTransfer:
			handoffs = append(handoffs, item)
		case *ir.Delegate:
			if control.Task != "" {
				built, err := coreTask(agent, control.Task, control.When, control.Announce)
				if err != nil {
					return nil, err
				}
				tasks = append(tasks, built)
				continue
			}
			group := agent.TaskGroups[control.Group]
			built := map[string]any{
				"name": control.Group, "when": control.When, "context_scope": string(group.ContextScope),
				"then": string(group.Then),
			}
			putString(built, "then_target", group.ThenTarget)
			if len(control.Announce) > 0 {
				built["announce"] = stateLines(control.Announce)
			}
			var names []string
			skips := map[string]string{}
			for _, step := range group.Steps {
				names = append(names, step.Task)
				steps[step.Task] = true
				if step.SkipWhenConfirmed != "" {
					skips[step.Task] = step.SkipWhenConfirmed
				}
			}
			built["steps"] = names
			if len(skips) > 0 {
				built["skip_when_confirmed"] = skips
			}
			if len(group.Arguments) > 0 {
				built["arguments"] = group.Arguments
			}
			if len(group.NonBlocking) > 0 {
				built["non_blocking"] = group.NonBlocking
			}
			groups = append(groups, built)
		}
	}
	for _, task := range tasks {
		delete(steps, task.(map[string]any)["name"].(string)) // offered directly: it keeps its when
	}
	for _, step := range sortedKeys(steps) {
		built, err := coreTask(agent, step, "", nil)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, built)
	}
	if own == nil {
		own = []string{}
	}
	out := map[string]any{"instructions": def.Instructions, "tools": own, "model": def.Model, "voice": def.Voice}
	putString(out, "realtime", cmp.Or(def.Realtime, def.Live))
	if len(handoffs) > 0 {
		out["handoffs"] = handoffs
	}
	if len(transfers) > 0 {
		out["transfers"] = transfers
	}
	if endCall != nil {
		out["end_call"] = endCall
	}
	if len(tasks) == 0 && len(groups) == 0 {
		return out, nil
	}
	var taskOnly []string
	for _, task := range tasks {
		for _, tool := range task.(map[string]any)["tools"].([]string) {
			if !slices.Contains(own, tool) && !slices.Contains(taskOnly, tool) {
				taskOnly = append(taskOnly, tool)
			}
		}
	}
	slices.Sort(taskOnly)
	out["tasks"] = map[string]any{
		"version": 1, "mode": cmp.Or(string(agent.Mode), "status"), "tasks": tasks, "groups": groups, "task_only_tools": taskOnly,
	}
	return out, nil
}

// coreTask lowers one task to a TaskSpec. when is empty for a step only a
// group starts. Every spoken line is rewritten to read what the line's tool
// gives it: see rewriteLines.
func coreTask(agent *ir.Agent, name, when string, announce []string) (map[string]any, error) {
	task := agent.Tasks[name]
	tools, handoffs := []string{}, []string{}
	for _, item := range task.Tools {
		if _, ok := agent.Controls[item].(*ir.AgentTransfer); ok {
			handoffs = append(handoffs, item) // a handoff tool the step offers, as TaskSpec.handoffs
			continue
		}
		tools = append(tools, item)
	}
	out := map[string]any{
		"name": name, "instructions": task.Instructions, "tools": tools,
		"history": string(task.Context.History), "assign": coreAssigns(task.Assign),
	}
	if len(handoffs) > 0 {
		out["handoffs"] = handoffs
	}
	putString(out, "when", when)
	putString(out, "opening", string(task.Opening))
	if task.Context.MaxMessages > 0 {
		out["max_messages"] = task.Context.MaxMessages
	}
	if task.Context.IncludeToolCalls != nil {
		out["include_tool_calls"] = *task.Context.IncludeToolCalls
	}
	if lines := append(slices.Clone(announce), task.Announce...); len(lines) > 0 {
		out["announce"] = stateLines(lines)
	}
	fromResult := resultPaths(task.Assign)
	var finish []any
	for _, rule := range task.Finish {
		tool := agent.Tools[rule.Tool]
		success, err := typedMatch(tool.Output, rule.Success)
		if err != nil {
			return nil, fmt.Errorf("task %q: %w", name, err)
		}
		built := map[string]any{"tool": rule.Tool, "success": success}
		if len(rule.Confirm) > 0 {
			built["confirm"] = rewriteLines(rule.Confirm, inputPaths(task.Assign, tool), nil)
		}
		if len(rule.Say) > 0 {
			built["say"] = rewriteLines(rule.Say, nil, mergePaths(resultPaths(tool.Assign), fromResult))
		}
		finish = append(finish, built)
	}
	if len(finish) > 0 {
		out["finish"] = finish
	}
	var replies []any
	for _, reply := range task.Replies {
		tool := agent.Tools[reply.Tool]
		when, err := typedMatch(tool.Output, reply.When)
		if err != nil {
			return nil, fmt.Errorf("task %q: %w", name, err)
		}
		reads := mergePaths(resultPaths(tool.Assign), fromResult)
		replies = append(replies, map[string]any{"tool": reply.Tool, "when": when, "say": rewriteLines(reply.Say, nil, reads)})
	}
	if len(replies) > 0 {
		out["replies"] = replies
	}
	if opens := task.OpensWith; opens != nil {
		built := map[string]any{"tool": opens.Tool}
		if len(opens.Arguments) > 0 {
			built["arguments"] = opens.Arguments
		}
		if len(opens.Say) > 0 {
			reads := mergePaths(resultPaths(agent.Tools[opens.Tool].Assign), fromResult)
			built["say"] = rewriteLines(opens.Say, nil, reads)
		}
		out["opens_with"] = built
	}
	if len(task.Inputs) > 0 {
		var inputs []any
		for _, input := range task.Inputs {
			built := map[string]any{"kind": string(input.Kind), "name": input.Name}
			putString(built, "value", input.Value)
			inputs = append(inputs, built)
		}
		out["inputs"] = inputs
	}
	if len(task.Arguments) > 0 {
		out["arguments"] = task.Arguments
	}
	if len(task.Via) > 0 {
		out["via"] = task.Via
	}
	return out, nil
}

// coreAssigns lowers assign rows to unmute-core Assign rows.
func coreAssigns(rows []ir.AssignTo) []any {
	out := []any{}
	for _, row := range rows {
		built := map[string]any{"variable": row.Var, "path": fieldPath(row.Field)}
		if row.Append {
			built["append"] = true
		}
		if row.Optional {
			built["optional"] = true
		}
		if row.FromInput {
			built["source"] = "input"
		}
		out = append(out, built)
	}
	return out
}

// corePrefetch lowers the prefetch list, in the author's order.
func corePrefetch(agent *ir.Agent) ([]any, error) {
	out := []any{}
	for _, entry := range agent.Prefetch {
		switch {
		case entry.Clock != "":
			assign := map[string]string{}
			for _, pair := range entry.Assign {
				assign[pair.Key] = strings.TrimPrefix(fmt.Sprint(pair.Value), "result.")
			}
			out = append(out, map[string]any{"kind": "clock", "name": entry.Name, "timezone": entry.Timezone, "assign": assign})
		case entry.Source != "":
			source, ok := coreSources[entry.Source]
			if !ok {
				return nil, fmt.Errorf("prefetch %q: source %s has no unmute-core call value", entry.Name, entry.Source)
			}
			var values []string
			for _, pair := range entry.Assign {
				values = append(values, pair.Key)
			}
			out = append(out, map[string]any{"kind": "call_value", "name": entry.Name, "source": source, "assign": values})
		default:
			assign := map[string]string{}
			for _, pair := range entry.Assign {
				assign[pair.Key] = strings.TrimPrefix(fmt.Sprint(pair.Value), "result.")
			}
			built := map[string]any{"kind": "lookup", "name": entry.Name, "tool": entry.Tool, "assign": assign}
			if len(entry.Inputs) > 0 {
				built["inputs"] = entry.Inputs
			}
			if len(entry.NeedsConfirmed) > 0 {
				built["needs_confirmed"] = entry.NeedsConfirmed
			}
			if entry.Background {
				built["background"] = true
			}
			putString(built, "refuse_call", stateLine(entry.RefuseCall))
			out = append(out, built)
		}
	}
	return out, nil
}

// coreState lowers the variables: rules onto CallState's arguments.
func coreState(agent *ir.Agent) map[string]any {
	confirm, callStart, dependencies := map[string]string{}, map[string]string{}, map[string][]string{}
	for _, name := range sortedKeys(agent.Variables) {
		variable := agent.Variables[name]
		if variable.Confirm != "" {
			confirm[name] = variable.Confirm
		}
		if variable.CallStart != "" {
			callStart[name] = string(variable.CallStart)
		}
		if len(variable.Dependencies) > 0 {
			dependencies[name] = variable.Dependencies
		}
	}
	return map[string]any{"confirm": confirm, "call_start": callStart, "dependencies": dependencies}
}

// coreHandoffs lowers each agent transfer to what a handoff tool needs.
func coreHandoffs(agent *ir.Agent) map[string]any {
	out := map[string]any{}
	for _, name := range sortedKeys(agent.Controls) {
		transfer, ok := agent.Controls[name].(*ir.AgentTransfer)
		if !ok {
			continue
		}
		built := map[string]any{"to": transfer.To, "description": transfer.When, "history": string(transfer.Context.History)}
		putString(built, "announce", transfer.Announce)
		if transfer.Context.MaxMessages > 0 {
			built["max_messages"] = transfer.Context.MaxMessages
		}
		if transfer.Context.IncludeToolCalls != nil {
			built["include_tool_calls"] = *transfer.Context.IncludeToolCalls
		}
		if len(transfer.Saves) > 0 {
			built["saves"] = transfer.Saves
		}
		if opens := transfer.OpensWith; opens != nil {
			opening := map[string]any{"tool": opens.Tool}
			if len(opens.Arguments) > 0 {
				opening["arguments"] = opens.Arguments
			}
			if len(opens.Say) > 0 {
				opening["say"] = rewriteLines(opens.Say, nil, resultPaths(agent.Tools[opens.Tool].Assign))
			}
			built["opens_with"] = opening
		}
		out[name] = built
	}
	return out
}

// coreIdle lowers inactivity: onto IdleNudges. One nudge, then the goodbye:
// end_after counts from the start of the silence, and unmute-core's hang-up
// delay from the nudge.
func coreIdle(inactivity *ir.Inactivity) (map[string]any, error) {
	if inactivity == nil || inactivity.NudgeAfter == "" {
		return nil, nil
	}
	nudge, err := coreSeconds(inactivity.NudgeAfter)
	if err != nil {
		return nil, fmt.Errorf("inactivity.nudge_after: %w", err)
	}
	out := map[string]any{"first_nudge_delay_seconds": nudge, "second_nudge_delay_seconds": nil}
	putString(out, "first_nudge_text", stateLine(inactivity.NudgeText))
	putString(out, "final_hangup_text", stateLine(inactivity.EndText))
	if inactivity.EndAfter != "" {
		end, err := coreSeconds(inactivity.EndAfter)
		if err != nil {
			return nil, fmt.Errorf("inactivity.end_after: %w", err)
		}
		out["hangup_delay_seconds"] = max(end-nudge, 1)
	}
	return out, nil
}

// rewriteLines rewrites each line's placeholders. A value the line's tool
// fills from its input reads {{input.<path>}}, one it fills from its result
// reads {{result.<path>}}, so the caller hears the value the tool holds, said
// by its schema format. Any other value is read from State. The author wrote
// every one of them as {{state.x}}.
func rewriteLines(lines []string, input, result map[string][]string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = linePlaceholder.ReplaceAllStringFunc(line, func(token string) string {
			match := linePlaceholder.FindStringSubmatch(token)
			root, rest := match[1], strings.ReplaceAll(match[2], "__", ".")
			if path, ok := input[root]; ok {
				return "{{" + strings.Join(append([]string{"input"}, path...), ".") + rest + "}}"
			}
			if path, ok := result[root]; ok {
				return "{{" + strings.Join(append([]string{"result"}, path...), ".") + rest + "}}"
			}
			return "{{" + root + rest + "}}"
		})
	}
	return out
}

// stateLine reads every placeholder of a line from State, with dotted paths.
func stateLine(line string) string { return rewriteLines([]string{line}, nil, nil)[0] }

func stateLines(lines []string) []string { return rewriteLines(lines, nil, nil) }

// inputPaths maps each variable a task fills from its input to the input path,
// for one closing tool: only the inputs that tool takes.
func inputPaths(rows []ir.AssignTo, tool ir.Tool) map[string][]string {
	properties, _ := tool.Input["properties"].(map[string]any)
	out := map[string][]string{}
	for _, row := range rows {
		path := fieldPath(row.Field)
		if !row.FromInput || len(path) == 0 {
			continue
		}
		if _, ok := properties[path[0]]; ok {
			out[row.Var] = path
		}
	}
	return out
}

// resultPaths maps each variable a list of rows fills from a result to its path.
func resultPaths(rows []ir.AssignTo) map[string][]string {
	out := map[string][]string{}
	for _, row := range rows {
		if !row.FromInput {
			out[row.Var] = fieldPath(row.Field)
		}
	}
	return out
}

func mergePaths(first, second map[string][]string) map[string][]string {
	out := map[string][]string{}
	for key, value := range first {
		out[key] = value
	}
	for key, value := range second {
		out[key] = value
	}
	return out
}

func fieldPath(field string) []string {
	if field == "" {
		return []string{}
	}
	return strings.Split(field, ".")
}

// typedMatch turns a match's text values into the JSON types the tool's output
// declares, so unmute-core compares a boolean result with true, not "true".
func typedMatch(output map[string]any, match map[string][]string) (map[string][]any, error) {
	properties, _ := output["properties"].(map[string]any)
	out := map[string][]any{}
	for field, values := range match {
		property, _ := properties[field].(map[string]any)
		kind, _ := property["type"].(string)
		for _, value := range values {
			typed, err := typedValue(kind, value)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", field, err)
			}
			out[field] = append(out[field], typed)
		}
	}
	return out, nil
}

func typedValue(kind, value string) (any, error) {
	switch kind {
	case "boolean":
		return strconv.ParseBool(value)
	case "integer":
		return strconv.Atoi(value)
	}
	return value, nil
}

func coreSeconds(value ir.Duration) (float64, error) {
	parsed, err := time.ParseDuration(string(value))
	if err != nil {
		return 0, err
	}
	return parsed.Seconds(), nil
}

func putString(out map[string]any, key, value string) {
	if value != "" {
		out[key] = value
	}
}

// coreModels lowers the target's model bindings for agent.py, and refuses a
// provider agent.py does not build yet. Each agent names its think and speak
// bindings by the names agent.yaml gives them.
func coreModels(resolved ir.Target) (map[string]any, error) {
	models := resolved.Models
	think := map[string]any{}
	for name, binding := range models.Reason {
		switch {
		case binding.Provider == "google", binding.Provider == "openai":
			think[name] = binding
		case binding.Router():
			router, err := coreRouter(binding)
			if err != nil {
				return nil, fmt.Errorf("think %q: %w", name, err)
			}
			think[name] = router
		default:
			return nil, fmt.Errorf("think %q: version 2 compiles a google or openai model, or the slng router, only so far, and this one is %s", name, binding.Provider)
		}
	}
	// A live model runs where a realtime one does: one model listens, thinks and
	// speaks. Its backend is an argument of its constructor, in speech.py.
	realtime := map[string]ir.Binding{}
	maps.Copy(realtime, models.Realtime)
	maps.Copy(realtime, models.Live)
	if models.Listen == nil && len(realtime) == 0 {
		return nil, fmt.Errorf("listen: version 2 needs a speech-to-text model")
	}
	if models.Turn == nil && len(realtime) == 0 {
		return nil, fmt.Errorf("turn: version 2 needs a turn model")
	}
	return map[string]any{"think": think, "speak": models.Speak, "listen": models.Listen, "turn": models.Turn, "realtime": realtime}, nil
}

// coreRouter lowers a SLNG Context Router think binding to what spec.py builds
// a RouterModelCall from: the model, the world part, the slng_config with the
// upstream as one tier, and the params the router forwards. A credential is
// {"env": NAME}, read when the call starts: no generated file holds its value.
func coreRouter(binding ir.Binding) (map[string]any, error) {
	if binding.Upstream == nil {
		return nil, fmt.Errorf("a router binding needs an upstream block")
	}
	fields, ok := targetcap.SlngResolveUpstream(binding.Upstream.Provider, binding.Upstream.Fields())
	if !ok {
		return nil, fmt.Errorf("upstream provider %q is not one the router accepts", binding.Upstream.Provider)
	}
	endpoint := map[string]any{}
	for _, field := range fields {
		if field.Env {
			endpoint[field.Wire] = map[string]any{"env": field.Value}
			continue
		}
		endpoint[field.Wire] = field.Value
	}
	tier := map[string]any{"model": binding.Model, "weight": 100, "endpoint": endpoint}
	router := map[string]any{
		"provider": binding.Provider,
		"model":    binding.Model,
		"config":   map[string]any{"tiers": map[string]any{"1": []any{tier}}},
	}
	putString(router, "world_part", slngWorldPart(binding))
	if slngPureProxy(binding) {
		router["pure_proxy"] = true
	}
	if _, body := splitParams(slngConsumedParams(binding.Params), targetcap.SlngRequestBodyArg); len(body) > 0 {
		router["body"] = body
	}
	return router, nil
}

// coreKnowledge lowers each knowledge base to KnowledgeBase's arguments, and
// the embedder spec.py imports to build its index: a module, a class and the
// model it pins, from the same table version 1 reads.
func coreKnowledge(agent *ir.Agent) (map[string]any, error) {
	out := map[string]any{}
	for _, name := range sortedKeys(agent.Knowledge) {
		base := agent.Knowledge[name]
		embed, ok := targetcap.LookupEmbeddingService(base.Embed)
		if !ok {
			return nil, fmt.Errorf("knowledge %q: no embedding service %q", name, base.Embed)
		}
		out[name] = map[string]any{
			"documents":     "knowledge/" + name,
			"mode":          string(base.Mode),
			"chunk_size":    base.ChunkSize,
			"chunk_overlap": base.ChunkOverlap,
			"top_k":         base.TopK,
			"min_score":     base.MinScore,
			"embed": map[string]any{
				"module": embed.PythonModule, "class": embed.PythonClass,
				"kwarg": embed.ModelKwarg, "model": embed.Model, "credential_env": embed.CredentialEnv,
			},
		}
	}
	return out, nil
}

// coreTransfers lowers each escalation to core's Transfer: when the model
// calls it, the variable that holds the number, and what happens when nobody
// answers. The number stays in the environment, as version 1 keeps it.
func coreTransfers(agent *ir.Agent, resolved ir.Target) (map[string]any, error) {
	out := map[string]any{}
	for _, name := range sortedKeys(agent.Controls) {
		transfer, ok := agent.Controls[name].(*ir.HumanTransfer)
		if !ok {
			continue
		}
		env, ok := resolved.Destinations[transfer.Destination]
		if !ok {
			return nil, fmt.Errorf("escalation %q: destination %q is missing from target %s", name, transfer.Destination, resolved.Name)
		}
		built := map[string]any{
			"description":     transfer.When,
			"destination_env": env,
			"mode":            string(transfer.Mode),
			"on_unavailable":  string(transfer.OnUnavailable),
		}
		putString(built, "briefing", transfer.Briefing)
		if transfer.RingTimeout != "" {
			seconds, err := coreSeconds(transfer.RingTimeout)
			if err != nil {
				return nil, fmt.Errorf("escalation %q: ring_timeout: %w", name, err)
			}
			built["ring_timeout_s"] = seconds
		}
		out[name] = built
	}
	return out, nil
}

// coreWebhook lowers a webhook tool's endpoint: the variable that holds its
// base URL, the path after it with its placeholders in the library's spelling,
// and its credential, by variable name only.
func coreWebhook(tool ir.Tool) map[string]any {
	hook := map[string]any{"url_env": tool.URLEnv, "path": stateLine(tool.Path)}
	if auth := tool.Auth; auth != nil {
		built := map[string]any{"kind": string(auth.Type), "token_env": auth.TokenEnv}
		putString(built, "header", auth.Header)
		hook["auth"] = built
	}
	return hook
}
