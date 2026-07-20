package generate

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/slng/unmute/internal/ir"
	targetcap "github.com/slng/unmute/internal/target"
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
		Tracing:    agent.Tracing != nil && agent.Tracing.Provider == "langfuse",
	}
	env := newEnvSet()
	if data.Tracing {
		for _, name := range []string{"LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY", "LANGFUSE_BASE_URL"} {
			env.add(name)
		}
	}

	stt, err := sttService(target.Models.Listen, agent.Language, env)
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

	// Task tools appear inside Flow nodes as module-level flows handlers; emit
	// each webhook handler once no matter how many steps reference it.
	seenFlowTools := map[string]bool{}
	for _, a := range data.Agents {
		for _, d := range a.Delegates {
			for _, step := range d.StepTasks {
				for _, tool := range step.Tools {
					if !seenFlowTools[tool.Name] {
						seenFlowTools[tool.Name] = true
						data.FlowTools = append(data.FlowTools, tool)
					}
				}
			}
		}
	}
	sort.Slice(data.FlowTools, func(i, j int) bool { return data.FlowTools[i].Name < data.FlowTools[j].Name })

	for _, name := range sortedVarNames(agent) {
		v := agent.Variables[name]
		pt, def := pyType(v.Type), pyLiteral(v.Default)
		if v.Default == nil {
			pt, def = pt+" | None", "None"
		}
		data.Variables = append(data.Variables, pipecatVariable{Name: name, PyType: pt, Default: def, Source: string(v.Source)})
	}
	data.Telephony, err = buildPipecatTelephony(agent, target, env)
	if err != nil {
		return pipecatData{}, err
	}

	applyConversation(agent.Conversation, &data)
	data.Notes = append(data.Notes, serviceNotes(data)...)
	if target.Models.Turn != nil {
		data.Notes = append(data.Notes, "turn role lowers to on-device VAD (Silero); its binding is advisory")
	}
	setImportNeeds(&data)
	data.Imports, data.Extras, data.Deps = collectImportsExtras(data)
	if data.Telephony != nil {
		switch data.Telephony.Carrier {
		case "twilio":
			data.Deps = append(data.Deps, "twilio>=9,<10")
		case "telnyx":
			data.Deps = append(data.Deps, "cryptography>=45,<47")
		}
		slices.Sort(data.Deps)
	}
	data.RequiredEnv = env.sorted()
	return data, nil
}

func buildPipecatTelephony(agent *ir.Agent, resolved ir.Target, env *envSet) (*pipecatTelephony, error) {
	plan := resolved.Telephony
	if plan == nil {
		return nil, nil
	}
	if plan.Key.Provider != ir.ProviderPipecat || plan.Key.Transport != "carrier-websocket" {
		return nil, fmt.Errorf("pipecat telephony route (%s, %s, %s) has no emitted adapter", plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier)
	}
	var required []string
	switch plan.Key.Carrier {
	case "twilio":
		required = []string{"account_sid", "auth_token", "from_number"}
	case "telnyx":
		required = []string{"api_key", "public_key", "connection_id", "from_number"}
	default:
		return nil, fmt.Errorf("pipecat telephony route (%s, %s, %s) has no emitted adapter", plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier)
	}
	allowed := map[string]bool{}
	for _, key := range required {
		allowed[key] = true
		name := plan.Environment[key]
		if name == "" {
			return nil, fmt.Errorf("pipecat telephony route %s requires connection environment key %q", plan.Key.Carrier, key)
		}
		env.add(name)
	}
	for key := range plan.Environment {
		if !allowed[key] {
			return nil, fmt.Errorf("pipecat telephony route %s does not accept connection environment key %q", plan.Key.Carrier, key)
		}
	}
	env.add("UNMUTE_PUBLIC_URL")

	telephony := &pipecatTelephony{
		Carrier: plan.Key.Carrier, Connection: plan.Connection,
		AccountSIDEnv: plan.Environment["account_sid"], AuthTokenEnv: plan.Environment["auth_token"],
		APIKeyEnv: plan.Environment["api_key"], PublicKeyEnv: plan.Environment["public_key"],
		ConnectionEnv: plan.Environment["connection_id"], FromNumberEnv: plan.Environment["from_number"],
	}
	for _, evidence := range plan.Evidence {
		switch evidence.Feature {
		case "inbound":
			telephony.HasInbound = true
		case "outbound":
			telephony.HasOutbound = true
		}
	}
	if telephony.HasOutbound {
		env.add("UNMUTE_OUTBOUND_TOKEN")
	}
	for _, variable := range sortedVarNames(agent) {
		def := agent.Variables[variable]
		if def.Source == ir.VariableSourceCallStart {
			telephony.CallStart = append(telephony.CallStart, pipecatCallStart{Name: variable, Type: string(def.Type), Required: def.Default == nil})
			continue
		}
		if def.Source != "" {
			telephony.SystemSources = append(telephony.SystemSources, pipecatSystemSource{Variable: variable, Source: string(def.Source)})
		}
	}
	return telephony, nil
}

// setImportNeeds inspects the built model so bot.py imports only what this spec
// exercises (no dead imports in the emitted pipeline).
func setImportNeeds(data *pipecatData) {
	// asyncio is unconditional: every bot gates entry-agent activation on an
	// asyncio.Event (B8/V14), so it is not an import-need flag anymore.
	data.NeedsTurnStrategies = data.Interrupt != nil && data.Interrupt.MinWords > 0
	data.NeedsAppendFrame = data.Inactivity != nil
	data.NeedsEndFrame = data.MaxDurationSecs > 0
	for _, a := range data.Agents {
		if len(a.Tools)+len(a.Transfers)+len(a.Delegates) > 0 {
			data.NeedsFunctionCalls = true
		}
		for _, t := range a.Tools {
			if t.ColdDestination != "" {
				data.HasColdTransfer = data.HasColdTransfer || !t.CarrierTransfer
				data.NeedsAppendFrame = true // cold transfer prompts the caller
				data.NeedsEndFrame = data.NeedsEndFrame || !t.CarrierTransfer
				continue
			}
			if t.Local {
				data.NeedsInspect = true // isawaitable on the user handler (V13)
			} else {
				data.NeedsHTTPX = true // webhook tool POSTs with httpx
			}
			if t.EndsCall {
				data.NeedsEndFrame = true
			}
		}
		for _, d := range a.Delegates {
			data.HasFlows = true // tasks run as Flows on the owning worker (C8)
			if d.Then == "end" {
				data.NeedsEndFrame = true
			}
			if d.Isolated {
				data.HasIsolated = true
			}
			for _, step := range d.StepTasks {
				for _, t := range step.Tools {
					if t.Local {
						data.NeedsInspect = true
					} else {
						data.NeedsHTTPX = true // flows tool handlers POST with httpx
					}
				}
			}
		}
	}
	// Local handler files ride the artifact (tools/<name>.py, V13); dedupe
	// across agent @tool methods and flows handlers, mirroring livekit.
	seenLocal := map[string]bool{}
	collectLocal := func(tools []pipecatTool) {
		for _, t := range tools {
			if !t.Local || seenLocal[t.Name] {
				continue
			}
			seenLocal[t.Name] = true
			data.LocalTools = append(data.LocalTools, pipecatLocalTool{Name: t.Name, Source: t.HandlerSource})
		}
	}
	for _, a := range data.Agents {
		collectLocal(a.Tools)
	}
	collectLocal(data.FlowTools)
	sort.Slice(data.LocalTools, func(i, j int) bool { return data.LocalTools[i].Name < data.LocalTools[j].Name })
}

// collectImportsExtras returns the deduped, sorted service imports for bot.py,
// the pipecat-ai extras, and the standalone pip deps for plugin services (e.g.
// pipecat-slng), so only used services are pulled in. All three come off the
// used services' catalogue entries, so an emitted class can never lose its
// import or its install (the structural form of driver-pipecat V11).
func collectImportsExtras(data pipecatData) (imports, extras, deps []string) {
	importSet := map[string]bool{}
	// Always-on extras: every bot.py uses the runner (create_transport,
	// RunnerArguments, pipecat.runner.run.main → needs fastapi/uvicorn via
	// [runner]), a local VAD (silero), and the WebRTC dev transport (webrtc).
	extraSet := map[string]bool{"runner": true, "silero": true, "webrtc": true}
	if data.Tracing {
		extraSet["tracing"] = true
	}
	depSet := map[string]bool{}
	if data.Transport == "daily-sip" {
		extraSet["daily"] = true
	}
	note := func(entry targetcap.Entry) {
		if entry.Import != "" {
			importSet[entry.Import] = true
		}
		if entry.Install.Extra != "" {
			extraSet[entry.Install.Extra] = true
		}
		if entry.Install.Package != "" {
			depSet[entry.Install.Package+entry.Install.Constraint] = true
		}
	}
	note(data.STT.Entry)
	for _, a := range data.Agents {
		note(a.LLM.Entry)
		note(a.TTS.Entry)
	}
	return sortedKeys(importSet), sortedKeys(extraSet), sortedKeys(depSet)
}

// serviceNotes lists every used catalogue entry in the compile report, so
// what was selected (and from which install path) is always inspectable.
func serviceNotes(data pipecatData) []string {
	set := map[string]bool{}
	note := func(svc pipecatService) {
		if svc.Entry.Call == nil {
			return
		}
		set[fmt.Sprintf("%s: %s via %s (%s, verified %s)",
			svc.Entry.Role, svc.Vendor, svc.Entry.Call.Class, installLabel(svc.Entry), svc.Entry.Verified)] = true
	}
	note(data.STT)
	for _, a := range data.Agents {
		note(a.LLM)
		note(a.TTS)
	}
	return sortedKeys(set)
}

func installLabel(entry targetcap.Entry) string {
	host := "pipecat-ai"
	if entry.Framework == targetcap.LiveKit {
		host = "livekit-agents"
	}
	switch {
	case entry.Install.Extra != "":
		return host + "[" + entry.Install.Extra + "]"
	case entry.Install.Package != "":
		return entry.Install.Package + entry.Install.Constraint
	default:
		return "ships with the framework"
	}
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
	llm, err := agentLLMService(target.Models.Reason[def.Model], def.Instructions, env)
	if err != nil {
		return pipecatAgent{}, fmt.Errorf("agent %q: %w", name, err)
	}
	tts, err := ttsService(target.Models.Speak[def.Voice], agent.Language, env)
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
			delegate, err := buildDelegate(agent, ref, c, env)
			if err != nil {
				return pipecatAgent{}, fmt.Errorf("agent %q: %w", name, err)
			}
			built.Delegates = append(built.Delegates, delegate)
		}
	}
	return built, nil
}

// buildDelegate lowers a delegate control to a Flow run on the owning worker
// (C8): a single task is a one-node flow, a group a linear chain. Each step is
// resolved here so the template emits its node inline.
func buildDelegate(agent *ir.Agent, ref string, c *ir.Delegate, env *envSet) (pipecatDelegate, error) {
	delegate := pipecatDelegate{MethodName: ref, When: c.When}
	steps := []string{c.Task}
	if c.Task != "" {
		delegate.Task = c.Task
		delegate.Then = "return" // a single task always returns (SCHEMA 4.7)
		for variable, path := range c.Assign {
			delegate.Assign = append(delegate.Assign, pipecatAssign{Var: variable, Field: strings.TrimPrefix(path, "result.")})
		}
		sort.Slice(delegate.Assign, func(i, j int) bool { return delegate.Assign[i].Var < delegate.Assign[j].Var })
	} else {
		group := agent.TaskGroups[c.Group]
		delegate.Group = c.Group
		delegate.Then = string(group.Then)
		delegate.ThenTarget = group.ThenTarget
		delegate.Isolated = group.ContextScope == ir.ContextIsolated
		steps = group.Steps
	}
	for _, step := range steps {
		task, err := buildTask(agent, step, agent.Tasks[step], env)
		if err != nil {
			return pipecatDelegate{}, err
		}
		task.FinishName = "finish_" + ref + "_" + step
		delegate.StepTasks = append(delegate.StepTasks, task)
	}
	for i := range delegate.StepTasks[:len(delegate.StepTasks)-1] {
		delegate.StepTasks[i].NextName = delegate.StepTasks[i+1].Name
	}
	return delegate, nil
}

// buildTask lowers a task to a Flow-node model: instructions, tools, and the
// finish-function schema derived from the typed result (V1). The node runs on
// the owning agent's LLM; per-task model is gated (no LLMSwitcher, B7).
func buildTask(agent *ir.Agent, name string, task ir.Task, env *envSet) (pipecatTask, error) {
	built := pipecatTask{
		Name: name, Prompt: task.Instructions,
		ResultProps:    pyLiteral(resultProperties(task.Result)),
		ResultRequired: pyLiteral(anyStrings(sortedResultNames(task.Result))),
	}
	for _, ref := range task.Tools {
		tool, ok := agent.Tools[ref]
		if !ok {
			return pipecatTask{}, fmt.Errorf("task %q references unknown tool %q", name, ref)
		}
		built.Tools = append(built.Tools, buildTool(ref, tool, env))
	}
	return built, nil
}

// resultProperties builds the finish function's JSON-schema properties from a
// task's typed result. Forwarded verbatim for nested schemas (C11).
func resultProperties(result map[string]ir.ResultField) map[string]any {
	properties := map[string]any{}
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
	}
	return properties
}

// anyStrings widens a string slice for pyLiteral rendering.
func anyStrings(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
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
		Local: tool.Execution == ir.ToolLocal, HandlerSource: tool.HandlerSource,
		EndsCall: tool.Effect == ir.ToolEndsConversation, Interruption: interruptionValue(tool.Interruption),
	}
	built.Args = append(built.Args, inputFields(tool.Input)...)
	// Flow nodes advertise the tool via a FlowsFunctionSchema, which takes the
	// input schema verbatim rather than a Python signature.
	props, _ := tool.Input["properties"].(map[string]any)
	built.InputProps = pyLiteral(props)
	requiredList, _ := tool.Input["required"].([]any)
	built.InputRequired = pyLiteral(requiredList)
	return built
}

// humanTransferTool lowers a cold human_transfer to a @tool that dials the
// resolved destination over the Daily SIP transport (V5, cold only).
func humanTransferTool(name string, c *ir.HumanTransfer, target ir.Target, env *envSet) (pipecatTool, error) {
	destination, ok := target.Destinations[c.Destination]
	if !ok {
		return pipecatTool{}, fmt.Errorf("human transfer %q destination %q missing on target %q", name, c.Destination, target.Name)
	}
	carrierTransfer := target.Telephony != nil && target.Telephony.Key.Provider == ir.ProviderPipecat &&
		target.Telephony.Key.Transport == "carrier-websocket"
	if !carrierTransfer {
		env.add("DAILY_API_KEY")
	}
	desc := c.When
	if desc == "" {
		desc = "Transfer the caller to a human."
	}
	return pipecatTool{
		Name: name, MethodName: name, Description: desc,
		URLEnv: "", Args: nil, EndsCall: true,
		// The destination rides through as the tool's fixed target; rendered by the template.
		ColdDestination: destination, CarrierTransfer: carrierTransfer,
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
// The catalogue (internal/target) picks the class, import, install path, and
// constructor shape; this file only adapts its output to the driver's structs.

func apiKeyEnv(provider string) string {
	return strings.ToUpper(strings.ReplaceAll(provider, "-", "_")) + "_API_KEY"
}

// pipecatEnvRef renders the driver's environment-lookup idiom.
func pipecatEnvRef(name string) string { return "os.environ[" + pyQuote(name) + "]" }

// resolvePipecatService resolves one binding through the catalogue.
// extraSettings are nested Settings args the driver injects (the agents'
// system_instruction); the task job-workers use the raw identity fields.
func resolvePipecatService(role targetcap.Role, binding ir.Binding, language string, env *envSet, extraSettings ...pyKV) (pipecatService, error) {
	call, entry, err := resolveService(defaultCatalog, targetcap.Pipecat, role, binding, language, pipecatEnvRef, env, extraSettings...)
	if err != nil {
		return pipecatService{}, err
	}
	for _, args := range [][]pyKV{call.Args, call.SettingsArgs} {
		for i := range args {
			if args[i].Key == "language" {
				args[i].Value = "Language(" + args[i].Value + ")"
			}
		}
	}
	svc := pipecatService{Call: call, Entry: entry, Model: binding.Model, BaseURL: binding.EndpointEnv,
		Vendor: firstNonEmpty(binding.Provider, "openai")}
	if spec := entry.Call; spec.APIKeyArg != "" {
		svc.APIKeyEnv = spec.APIKeyEnv
		if svc.APIKeyEnv == "" {
			svc.APIKeyEnv = apiKeyEnv(firstNonEmpty(binding.Provider, "openai"))
		}
	}
	return svc, nil
}

func sttService(binding *ir.Binding, language string, env *envSet) (pipecatService, error) {
	if binding == nil {
		return pipecatService{}, fmt.Errorf("pipecat listen binding is missing a model")
	}
	return resolvePipecatService(targetcap.Listen, *binding, language, env)
}

// agentLLMService builds an agent's LLM; the prompt nests into Settings as
// system_instruction (the workers-model shape, driver-pipecat C2).
func agentLLMService(binding ir.Binding, prompt string, env *envSet) (pipecatService, error) {
	return resolvePipecatService(targetcap.Reason, binding, "", env,
		pyKV{Key: "system_instruction", Value: pyQuote(prompt)})
}

func ttsService(binding ir.Binding, language string, env *envSet) (pipecatService, error) {
	return resolvePipecatService(targetcap.Speak, binding, language, env)
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
