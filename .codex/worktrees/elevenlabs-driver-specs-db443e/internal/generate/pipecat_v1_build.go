package generate

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/slng/unmute/internal/ir"
)

// buildPipecatData lowers the resolved IR + target into the template model.
// Bindings (model/voice/params) are forwarded verbatim; only their provider
// selects the Pipecat service class and api-key env (C11).
func buildPipecatData(agent *ir.Agent, target ir.Target) (pipecatData, error) {
	data := pipecatData{
		Project:    target.Name,
		Version:    target.Version,
		MainName:   "main",
		EntryAgent: agent.EntryAgent,
		EntryClass: pyName(agent.EntryAgent),
		Transport:  target.Transport,
	}
	env := newEnvSet()

	stt, err := sttService(target.Models.Listen, env)
	if err != nil {
		return pipecatData{}, err
	}
	data.STT = stt

	for _, name := range sortedAgentNames(agent) {
		def := agent.Agents[name]
		built, err := buildPipecatAgent(agent, target, name, def, env)
		if err != nil {
			return pipecatData{}, err
		}
		data.Agents = append(data.Agents, built)
	}

	// Build a task-worker for every task any delegate reaches (single or group step).
	usedTasks := map[string]bool{}
	for _, a := range data.Agents {
		for _, d := range a.Delegates {
			if d.Task != "" {
				usedTasks[d.Task] = true
			}
			for _, step := range d.Steps {
				usedTasks[step] = true
			}
		}
	}
	for _, name := range sortedKeys(usedTasks) {
		task, err := buildTask(agent, target, name, agent.Tasks[name], env)
		if err != nil {
			return pipecatData{}, err
		}
		data.Tasks = append(data.Tasks, task)
	}

	for _, name := range sortedVarNames(agent) {
		v := agent.Variables[name]
		pt, def := pyType(v.Type), pyLiteral(v.Default)
		if v.Default == nil {
			pt, def = pt+" | None", "None"
		}
		data.Variables = append(data.Variables, pipecatVariable{Name: name, PyType: pt, Default: def})
	}

	applyConversation(agent.Conversation, &data)
	if agent.Pipeline.Turn != nil {
		data.Notes = append(data.Notes, "turn role lowers to on-device VAD (Silero); its binding is advisory")
	}
	setImportNeeds(&data)
	data.Imports, data.Extras, data.Deps = collectImportsExtras(data)
	data.RequiredEnv = env.sorted()
	return data, nil
}

// setImportNeeds inspects the built model so bot.py imports only what this spec
// exercises (no dead imports in the emitted pipeline).
func setImportNeeds(data *pipecatData) {
	data.NeedsAsyncio = data.MaxDurationSecs > 0
	data.NeedsTurnStrategies = data.Interrupt != nil && data.Interrupt.MinWords > 0
	data.NeedsAppendFrame = data.Inactivity != nil
	data.NeedsEndFrame = data.MaxDurationSecs > 0
	for _, a := range data.Agents {
		if len(a.Tools)+len(a.Transfers)+len(a.Delegates) > 0 {
			data.NeedsFunctionCalls = true
		}
		for _, t := range a.Tools {
			if t.ColdDestination != "" {
				data.HasColdTransfer = true
				data.NeedsAppendFrame = true // cold transfer prompts the caller
				data.NeedsEndFrame = true    // on_dialout_answered ends the call
				continue
			}
			data.NeedsHTTPX = true // webhook tool POSTs with httpx
			if t.EndsCall {
				data.NeedsEndFrame = true
			}
		}
		for _, d := range a.Delegates {
			if d.Then == "end" {
				data.NeedsEndFrame = true
			}
		}
	}
}

// collectImportsExtras returns the deduped, sorted service imports for bot.py,
// the pipecat-ai extras, and the standalone pip deps for plugin services (e.g.
// pipecat-slng), so only used services are pulled in.
func collectImportsExtras(data pipecatData) (imports, extras, deps []string) {
	importSet := map[string]bool{}
	// Always-on extras: every bot.py uses the runner (create_transport,
	// RunnerArguments, pipecat.runner.run.main → needs fastapi/uvicorn via
	// [runner]), a local VAD (silero), and the WebRTC dev transport (webrtc).
	extraSet := map[string]bool{"runner": true, "silero": true, "webrtc": true}
	depSet := map[string]bool{}
	if data.Transport == "daily-sip" {
		extraSet["daily"] = true
	}
	note := func(class string) {
		if info, ok := serviceInfo[class]; ok {
			importSet[info.Import] = true
			if info.Extra != "" {
				extraSet[info.Extra] = true
			}
			if info.Dep != "" {
				depSet[info.Dep] = true
			}
		}
	}
	note(data.STT.Class)
	for _, a := range data.Agents {
		note(a.LLM.Class)
		note(a.TTS.Class)
	}
	if len(data.Tasks) > 0 {
		extraSet["openai"] = true // task job-workers use the OpenAI SDK directly
	}
	return sortedKeys(importSet), sortedKeys(extraSet), sortedKeys(depSet)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func buildPipecatAgent(agent *ir.Agent, target ir.Target, name string, def ir.AgentDef, env *envSet) (pipecatAgent, error) {
	llm, err := llmService(target.Models.Reason[def.Model], agent.Models[def.Model], env)
	if err != nil {
		return pipecatAgent{}, fmt.Errorf("agent %q: %w", name, err)
	}
	tts, err := ttsService(target.Models.Speak[def.Voice], env)
	if err != nil {
		return pipecatAgent{}, fmt.Errorf("agent %q: %w", name, err)
	}
	built := pipecatAgent{Name: name, Class: pyName(name) + "Agent", Prompt: def.Instructions, LLM: llm, TTS: tts}

	for _, ref := range def.Tools {
		if tool, ok := agent.Tools[ref]; ok {
			built.Tools = append(built.Tools, buildTool(ref, tool, env))
			continue
		}
		control, ok := agent.Controls[ref]
		if !ok {
			return pipecatAgent{}, fmt.Errorf("agent %q references unknown tool/control %q", name, ref)
		}
		switch c := control.(type) {
		case *ir.AgentTransfer:
			// The method name is the control name (tools/controls share one
			// namespace, D8), so the LLM invokes the tool by its spec name.
			built.Transfers = append(built.Transfers, pipecatTransfer{
				MethodName: ref, To: c.To, When: c.When,
				Reason: transferReason(c), Requires: c.Requires,
			})
		case *ir.HumanTransfer:
			tool, err := humanTransferTool(ref, c, target, env)
			if err != nil {
				return pipecatAgent{}, fmt.Errorf("agent %q: %w", name, err)
			}
			built.Tools = append(built.Tools, tool)
		case *ir.Delegate:
			built.Delegates = append(built.Delegates, buildDelegate(agent, ref, c))
		}
	}
	return built, nil
}

// buildDelegate lowers a delegate control to a single-task or group dispatch.
func buildDelegate(agent *ir.Agent, ref string, c *ir.Delegate) pipecatDelegate {
	delegate := pipecatDelegate{MethodName: ref, When: c.When}
	if c.Task != "" {
		delegate.Task = c.Task
		for variable, path := range c.Assign {
			delegate.Assign = append(delegate.Assign, pipecatAssign{Var: variable, Field: strings.TrimPrefix(path, "result.")})
		}
		sort.Slice(delegate.Assign, func(i, j int) bool { return delegate.Assign[i].Var < delegate.Assign[j].Var })
		return delegate
	}
	group := agent.TaskGroups[c.Group]
	delegate.Group = c.Group
	delegate.Steps = group.Steps
	delegate.Then = string(group.Then)
	delegate.ThenTarget = group.ThenTarget
	delegate.Isolated = group.ContextScope == ir.ContextIsolated
	return delegate
}

// buildTask lowers a task to a job-worker. The per-task model (or the entry
// agent's model when omitted) picks the OpenAI-compatible route the @job uses.
func buildTask(agent *ir.Agent, target ir.Target, name string, task ir.Task, env *envSet) (pipecatTask, error) {
	profile := task.Model
	if profile == "" {
		profile = agent.Agents[agent.EntryAgent].Model
	}
	llm, err := llmService(target.Models.Reason[profile], agent.Models[profile], env)
	if err != nil {
		return pipecatTask{}, fmt.Errorf("task %q: %w", name, err)
	}
	return pipecatTask{
		Name: name, Class: pyName(name) + "Task", Prompt: task.Instructions,
		LLM: llm, ResultSchema: pyLiteral(resultResponseFormat(task.Result)),
	}, nil
}

// resultResponseFormat builds an OpenAI structured-output response_format from a
// task's typed result. Forwarded verbatim for nested schemas (C11).
func resultResponseFormat(result map[string]ir.ResultField) map[string]any {
	properties := map[string]any{}
	required := make([]any, 0, len(result))
	for _, name := range sortedResultNames(result) {
		field := result[name]
		switch {
		case field.Schema != nil:
			properties[name] = field.Schema
		case len(field.Enum) > 0:
			enum := make([]any, len(field.Enum))
			for i, value := range field.Enum {
				enum[i] = value
			}
			properties[name] = map[string]any{"type": "string", "enum": enum}
		default:
			properties[name] = map[string]any{"type": jsonType(field.Type)}
		}
		required = append(required, name)
	}
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name": "result", "strict": true,
			"schema": map[string]any{
				"type": "object", "properties": properties,
				"required": required, "additionalProperties": false,
			},
		},
	}
}

func sortedResultNames(result map[string]ir.ResultField) []string {
	names := make([]string, 0, len(result))
	for name := range result {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func jsonType(t ir.PrimitiveType) string {
	switch t {
	case ir.PrimitiveBoolean:
		return "boolean"
	case ir.PrimitiveInteger:
		return "integer"
	case ir.PrimitiveNumber:
		return "number"
	default:
		return "string"
	}
}

// interruptionValue maps a tool's interruption policy to the @tool argument;
// provider_default emits nothing (Pipecat's default).
func interruptionValue(i ir.ToolInterruption) string {
	switch i {
	case ir.ToolCancel:
		return "cancel"
	case ir.ToolContinue:
		return "continue"
	default:
		return ""
	}
}

func transferReason(c *ir.AgentTransfer) string {
	if c.When != "" {
		return c.When
	}
	return "Transfer the caller to the " + c.To + " agent."
}

func buildTool(name string, tool ir.Tool, env *envSet) pipecatTool {
	if tool.URLEnv != "" {
		env.add(tool.URLEnv)
	}
	built := pipecatTool{
		Name: name, MethodName: name, Description: tool.Description, URLEnv: tool.URLEnv,
		EndsCall: tool.Effect == ir.ToolEndsConversation, Interruption: interruptionValue(tool.Interruption),
	}
	built.Args = append(built.Args, inputFields(tool.Input)...)
	return built
}

// humanTransferTool lowers a cold human_transfer to a @tool that dials the
// resolved destination over the Daily SIP transport (V5, cold only).
func humanTransferTool(name string, c *ir.HumanTransfer, target ir.Target, env *envSet) (pipecatTool, error) {
	destination, ok := target.Destinations[c.Destination]
	if !ok {
		return pipecatTool{}, fmt.Errorf("human transfer %q destination %q missing on target %q", name, c.Destination, target.Name)
	}
	env.add("DAILY_API_KEY")
	desc := c.When
	if desc == "" {
		desc = "Transfer the caller to a human."
	}
	return pipecatTool{
		Name: name, MethodName: name, Description: desc,
		URLEnv: "", Args: nil, EndsCall: true,
		// The destination rides through as the tool's fixed target; rendered by the template.
		ColdDestination: destination,
	}, nil
}

// inputFields flattens a tool input JSON Schema object into ordered arg names.
func inputFields(input map[string]any) []pipecatArg {
	props, _ := input["properties"].(map[string]any)
	requiredList, _ := input["required"].([]any)
	required := map[string]bool{}
	for _, r := range requiredList {
		if s, ok := r.(string); ok {
			required[s] = true
		}
	}
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	sort.Strings(names)
	args := make([]pipecatArg, 0, len(names))
	for _, n := range names {
		args = append(args, pipecatArg{Name: n, Required: required[n]})
	}
	return args
}

// applyConversation lowers the conversation block into the template model:
// greeting activation, interruption turn-strategies, idle timeout, max duration.
func applyConversation(c *ir.Conversation, data *pipecatData) {
	data.GreetingRunLLM = "False" // no greeting → activate silently, wait for the caller
	if c == nil {
		return
	}
	if c.Greeting != nil {
		switch {
		case c.Greeting.SpeaksFirst == ir.SpeaksFirstAgent && c.Greeting.Text != "":
			data.GreetingInstruction = "Begin the conversation by saying, word for word: " + c.Greeting.Text
			data.GreetingRunLLM = "True"
		case c.Greeting.SpeaksFirst == ir.SpeaksFirstAgent:
			data.GreetingInstruction = "Greet the caller and offer to help."
			data.GreetingRunLLM = "True"
		default: // speaks_first: user — stay silent until the caller talks
			data.GreetingRunLLM = "False"
		}
	}
	if c.Interruption != nil {
		interrupt := &pipecatInterrupt{MinWords: c.Interruption.MinimumWords, IgnorePhrase: c.Interruption.IgnorePhrases}
		if c.Interruption.Enabled != nil {
			interrupt.Enabled = *c.Interruption.Enabled
		}
		data.Interrupt = interrupt
		if len(interrupt.IgnorePhrase) > 0 {
			data.Notes = append(data.Notes, "interruption ignore_phrases emitted as IGNORE_PHRASES; short phrases are also suppressed by the min-words turn-start strategy")
		}
	}
	if c.Inactivity != nil {
		data.Inactivity = &pipecatInactivity{NudgeSecs: durationSecs(c.Inactivity.NudgeAfter), EndSecs: durationSecs(c.Inactivity.EndAfter)}
	}
	data.MaxDurationSecs = durationSecs(c.MaxDuration)
}

// durationSecs parses a Go-syntax IR duration to whole seconds; 0 if empty/invalid.
func durationSecs(d ir.Duration) int {
	if d == "" {
		return 0
	}
	parsed, err := time.ParseDuration(string(d))
	if err != nil {
		return 0
	}
	return int(parsed.Seconds())
}

// --- provider → service mapping -------------------------------------------

func apiKeyEnv(provider string) string {
	return strings.ToUpper(strings.ReplaceAll(provider, "-", "_")) + "_API_KEY"
}

func sttService(binding *ir.Binding, env *envSet) (pipecatService, error) {
	if binding == nil || binding.Model == "" {
		return pipecatService{}, fmt.Errorf("pipecat listen binding is missing a model")
	}
	svc := pipecatService{Model: binding.Model, Params: forwardParams(binding.Params)}
	switch binding.Provider {
	case "deepgram":
		svc.Class = "DeepgramSTTService"
	case "slng":
		svc.Class = "SlngSTTService" // pipecat-slng plugin: api_key + slng-format model
	case "openai", "":
		svc.Class = "OpenAISTTService"
	default:
		svc.Class = "OpenAISTTService" // OpenAI-compatible fallthrough (custom endpoint)
	}
	// Slng routes by api_key + region params, not a base_url; only the
	// OpenAI-compatible services take an endpoint override.
	if binding.EndpointEnv != "" && binding.Provider != "slng" {
		svc.BaseURL = binding.EndpointEnv
		env.add(binding.EndpointEnv)
	}
	svc.APIKeyEnv = apiKeyEnv(defaultProvider(binding.Provider))
	env.add(svc.APIKeyEnv)
	return svc, nil
}

func llmService(binding ir.Binding, profile ir.ModelProfile, env *envSet) (pipecatService, error) {
	if binding.Model == "" {
		return pipecatService{}, fmt.Errorf("reason binding is missing a model")
	}
	svc := pipecatService{Class: "OpenAILLMService", Model: binding.Model, Params: forwardParams(binding.Params)}
	if binding.EndpointEnv != "" {
		svc.BaseURL = binding.EndpointEnv
		env.add(binding.EndpointEnv)
	}
	svc.APIKeyEnv = apiKeyEnv(defaultProvider(binding.Provider))
	env.add(svc.APIKeyEnv)
	return svc, nil
}

func ttsService(binding ir.Binding, env *envSet) (pipecatService, error) {
	svc := pipecatService{Model: binding.Model, Voice: firstNonEmpty(binding.Voice, binding.VoiceID), Params: forwardParams(binding.Params)}
	switch binding.Provider {
	case "elevenlabs", "eleven_labs":
		svc.Class = "ElevenLabsTTSService"
	case "cartesia":
		svc.Class = "CartesiaTTSService"
	case "slng":
		svc.Class = "SlngTTSService" // pipecat-slng plugin: api_key + slng-format model + voice
	case "openai", "":
		svc.Class = "OpenAITTSService"
	default:
		svc.Class = "OpenAITTSService" // OpenAI-compatible custom endpoint
	}
	// Slng routes by api_key + region params, not a base_url (see sttService).
	if binding.EndpointEnv != "" && binding.Provider != "slng" {
		svc.BaseURL = binding.EndpointEnv
		env.add(binding.EndpointEnv)
	}
	svc.APIKeyEnv = apiKeyEnv(defaultProvider(binding.Provider))
	env.add(svc.APIKeyEnv)
	return svc, nil
}

func defaultProvider(provider string) string {
	if provider == "" {
		return "openai"
	}
	return provider
}

func forwardParams(params map[string]any) []pyKV {
	if len(params) == 0 {
		return nil
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]pyKV, 0, len(keys))
	for _, k := range keys {
		out = append(out, pyKV{Key: k, Value: pyLiteral(params[k])})
	}
	return out
}

// --- small helpers ---------------------------------------------------------

func pyType(t ir.PrimitiveType) string {
	switch t {
	case ir.PrimitiveBoolean:
		return "bool"
	case ir.PrimitiveInteger:
		return "int"
	case ir.PrimitiveNumber:
		return "float"
	default:
		return "str"
	}
}

// pyLiteral renders a decoded YAML value as a Python literal.
func pyLiteral(v any) string {
	switch value := v.(type) {
	case nil:
		return "None"
	case bool:
		if value {
			return "True"
		}
		return "False"
	case string:
		return strconv.Quote(value)
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case uint64:
		return strconv.FormatUint(value, 10)
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64)
	case []any:
		items := make([]string, len(value))
		for i, item := range value {
			items[i] = pyLiteral(item)
		}
		return "[" + strings.Join(items, ", ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(value))
		for k := range value {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]string, len(keys))
		for i, k := range keys {
			pairs[i] = strconv.Quote(k) + ": " + pyLiteral(value[k])
		}
		return "{" + strings.Join(pairs, ", ") + "}"
	default:
		return strconv.Quote(fmt.Sprintf("%v", value))
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func sortedAgentNames(agent *ir.Agent) []string {
	names := make([]string, 0, len(agent.Agents))
	for name := range agent.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedVarNames(agent *ir.Agent) []string {
	names := make([]string, 0, len(agent.Variables))
	for name := range agent.Variables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type envSet struct{ seen map[string]bool }

func newEnvSet() *envSet { return &envSet{seen: map[string]bool{}} }

func (e *envSet) add(name string) {
	if name != "" {
		e.seen[name] = true
	}
}

func (e *envSet) sorted() []string {
	names := make([]string, 0, len(e.seen))
	for name := range e.seen {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
