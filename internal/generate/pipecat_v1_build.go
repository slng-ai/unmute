package generate

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/slng/unmute/internal/ir"
	targetcap "github.com/slng/unmute/internal/target"
)

// firstRegion is the one region a Pipecat target may declare, or "" for none.
func firstRegion(regions []string) string {
	if len(regions) == 0 {
		return ""
	}
	return regions[0]
}

// regionList puts a single region back into the list shape the compile report
// uses on both drivers, so one key means one thing everywhere.
func regionList(region string) []string {
	if region == "" {
		return nil
	}
	return []string{region}
}

// buildPipecatData lowers the resolved IR + target into the template model.
// Bindings (model/voice/params) are forwarded verbatim; only their provider
// selects the Pipecat service class and api-key env (C11).
func buildPipecatData(agent *ir.Agent, target ir.Target) (pipecatData, error) {
	data := pipecatData{
		Project: target.Name,
		Version: target.Version,
		// At most one region reaches this driver: a list of several is a gated
		// validation error (FieldDeploymentMultiRegion), which runs before any
		// artifact exists.
		DeploymentRegion: firstRegion(target.DeploymentRegions),
		MainName:         "main",
		EntryAgent:       agent.EntryAgent,
		EntryClass:       pyName(agent.EntryAgent),
		Transport:        target.Transport,
		Tracing:          agent.Tracing != nil && agent.Tracing.Provider == "langfuse",
	}
	env := newEnvSet()
	if data.Tracing {
		for _, name := range []string{"LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY", "LANGFUSE_BASE_URL"} {
			env.add(name)
		}
	}

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

	data.NeedsLanguage = serviceUsesLanguage(data.STT)
	for _, a := range data.Agents {
		if serviceUsesLanguage(a.LLM) || serviceUsesLanguage(a.TTS) {
			data.NeedsLanguage = true
		}
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
		data.Variables = append(data.Variables, pipecatVariable{
			Name: name, PyType: pt, Default: def, Source: string(v.Source), Description: v.Description,
		})
		// Dispatched input variables hydrate before the greeting on every
		// channel, not just telephony: the web and console dev paths read the
		// same payload out of UNMUTE_CALL_START (I.dispatch).
		if v.Source == ir.VariableSourceCallStart || v.Source == "" {
			data.CallStartVars = append(data.CallStartVars, pipecatCallStartVar{
				Name: name, Type: string(v.Type), Required: v.Default == nil && v.Source == ir.VariableSourceCallStart,
			})
		}
	}
	data.Capture = buildPipecatCapture(agent)
	if len(data.CallStartVars) > 0 {
		data.DevOptionalEnv = []string{"UNMUTE_CALL_START"}
	}
	// Declared secrets join the startup check; a required one missing fails the
	// bot before it answers a call (V12).
	for _, name := range requiredSecretEnv(agent) {
		env.add(name)
	}
	// Snapshot the provider creds before telephony env is added: the web dev
	// image (compose.dev.yaml) runs bot.py over WebRTC and needs no telephony
	// or coordination env.
	data.DevEnv = env.sorted()
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
	data.NeedsRender = renderNeeds(agent)
	for _, tool := range data.FlowTools {
		// A task tool reading call state needs it bound onto its module-level
		// flows handler; agent @tool methods already have self.state.
		if tool.NeedsState {
			data.NeedsStateBind = true
		}
	}
	for _, tools := range append([][]pipecatTool{data.FlowTools}, agentToolLists(data.Agents)...) {
		for _, tool := range tools {
			if len(tool.Needed) > 0 {
				data.NeedsRefusal = true
			}
		}
	}
	data.Inline = inlineEligible(&data)
	data.Imports, data.Extras, data.Deps = collectImportsExtras(data)
	if data.Telephony != nil {
		switch data.Telephony.Carrier {
		case "twilio":
			data.Deps = append(data.Deps, "twilio>=9,<10")
		case "telnyx":
			data.Deps = append(data.Deps, "cryptography>=45,<47")
		case "plivo":
			data.Deps = append(data.Deps, "plivo>=4,<5")
		}
		slices.Sort(data.Deps)
	}
	data.RequiredEnv = env.sorted()
	data.Secrets, data.ExtraEnv = secretEnvDocs(agent, data.RequiredEnv)
	// The platform's own naming convention (my-agent-secrets). Keyed on the whole
	// required-env list, not the declared `secrets:` block: .env.example lists
	// required env either way, and a deployed agent with no provider keys looks
	// healthy and fails on its first call. A package needing no environment at
	// all gets no set and deploys without one.
	if len(data.RequiredEnv) > 0 {
		data.SecretSet = data.Project + "-secrets"
	}
	return data, nil
}

// agentToolLists is every agent's @tool list, for whole-project scans.
func agentToolLists(agents []pipecatAgent) [][]pipecatTool {
	lists := make([][]pipecatTool, 0, len(agents))
	for _, a := range agents {
		lists = append(lists, a.Tools)
	}
	return lists
}

// buildPipecatCapture builds the generated update_variables tool: one optional
// argument per conversation variable, each carrying its declared type and
// description so the model knows what it is saving (V6).
func buildPipecatCapture(agent *ir.Agent) *pipecatCapture {
	fields := captureFields(agent)
	if len(fields) == 0 {
		return nil
	}
	capture := &pipecatCapture{
		Name: ir.CaptureToolName, Description: captureDescription(agent, fields), Fields: fields,
	}
	for _, name := range fields {
		variable := agent.Variables[name]
		// Every field is optional: the model saves what it has learned so far,
		// one call or several, never all of them at once.
		capture.Args = append(capture.Args, pipecatArg{
			Name: name, PyType: pyType(variable.Type) + " | None", PyDefault: "None",
			Description: variable.Description,
		})
	}
	return capture
}

func buildPipecatTelephony(agent *ir.Agent, resolved ir.Target, env *envSet) (*pipecatTelephony, error) {
	plan := resolved.Telephony
	if plan == nil {
		return nil, nil
	}
	if plan.Key.Provider != ir.ProviderPipecat || plan.Key.Transport != "carrier-websocket" {
		return nil, fmt.Errorf("pipecat telephony route (%s, %s, %s) has no emitted adapter", plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier)
	}
	if agent.Capacity == nil || agent.Capacity.MaxSessions <= 0 {
		return nil, fmt.Errorf("pipecat telephony requires positive capacity.max_sessions")
	}
	switch plan.Key.Carrier {
	case "twilio", "telnyx", "plivo":
	default:
		return nil, fmt.Errorf("pipecat telephony route (%s, %s, %s) has no emitted adapter", plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier)
	}
	required, optional, ok := targetcap.TelephonyEnvironment(targetcap.TelephonyKey{
		Provider: targetcap.Pipecat, Transport: plan.Key.Transport, Carrier: plan.Key.Carrier,
	})
	if !ok {
		return nil, fmt.Errorf("pipecat telephony route (%s, %s, %s) has no environment vocabulary", plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier)
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range append(required, optional...) {
		allowed[key] = true
	}
	for _, key := range required {
		name := plan.Environment[key]
		if name == "" {
			return nil, fmt.Errorf("pipecat telephony route %s requires connection environment key %q", plan.Key.Carrier, key)
		}
		env.add(name)
	}
	for _, key := range slices.Sorted(maps.Keys(plan.Environment)) {
		if !allowed[key] {
			return nil, fmt.Errorf("pipecat telephony route %s does not accept connection environment key %q", plan.Key.Carrier, key)
		}
	}
	env.add("UNMUTE_PUBLIC_URL")
	env.add("REDIS_URL")
	sessionTTL := 360
	if agent.Conversation != nil {
		if configured := durationSecs(agent.Conversation.MaxDuration) + 60; configured > sessionTTL {
			sessionTTL = configured
		}
	}

	telephony := &pipecatTelephony{
		Carrier: plan.Key.Carrier, Connection: plan.Connection,
		MaxSessions: agent.Capacity.MaxSessions, SessionTTL: sessionTTL,
		AccountSIDEnv: plan.Environment["account_sid"], AuthIDEnv: plan.Environment["auth_id"],
		AuthTokenEnv: plan.Environment["auth_token"],
		APIKeyEnv:    plan.Environment["api_key"], PublicKeyEnv: plan.Environment["public_key"],
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
		// Only runtime-owned sources come from the call context (B3). A
		// conversation variable listed here would make every call fail at
		// startup on a context field that never exists.
		if ir.IsSystemSource(def.Source) {
			telephony.SystemSources = append(telephony.SystemSources, pipecatSystemSource{Variable: variable, Source: string(def.Source)})
		}
	}
	return telephony, nil
}

// inlineEligible reports whether the bot collapses to the inline single-agent
// shape (F3): the LLM sits directly in the main pipeline with its tools as
// module-level direct functions in LLMContext, no bus / BusBridge / LLMWorker /
// activate_worker. Scoped to the simplest case — one agent, no handoffs, no Flow
// delegates, no tracing (the tracing helper is worker-bound, V22), no telephony
// or cold transfer. Everything else keeps the workers/bus path (dp§C8).
func inlineEligible(data *pipecatData) bool {
	// State (Variables) and model-written greeting both need machinery the inline
	// shape lacks (module-level tools can't reach self.state; the greeting has no
	// activate_worker to carry a developer message), so they keep the bus path.
	if len(data.Agents) != 1 || data.Tracing || data.Telephony != nil || data.HasColdTransfer {
		return false
	}
	if len(data.Variables) > 0 || data.GreetingInstruction != "" {
		return false
	}
	a := data.Agents[0]
	return len(a.Transfers) == 0 && len(a.Delegates) == 0
}

// setImportNeeds inspects the built model so bot.py imports only what this spec
// exercises (no dead imports in the emitted pipeline).
func setImportNeeds(data *pipecatData) {
	// asyncio is unconditional: every bot gates entry-agent activation on an
	// asyncio.Event (B8/V14), so it is not an import-need flag anymore.
	data.NeedsTurnStrategies = data.Interrupt != nil && data.Interrupt.MinWords > 0
	data.NeedsAppendFrame = data.Inactivity != nil
	data.NeedsEndFrame = data.MaxDurationSecs > 0
	if data.Capture != nil {
		data.NeedsFunctionCalls = true // the generated capture tool is a @tool too
	}
	for _, a := range data.Agents {
		if len(a.Tools)+len(a.Transfers)+len(a.Delegates) > 0 {
			data.NeedsFunctionCalls = true
		}
		for _, t := range a.Tools {
			if t.ColdDestination != "" {
				// Daily SIP cold: the bot announces via an LLM append (the REFER
				// keeps it streaming, B14) and always needs EndFrame — the
				// on_dialout_answered handler ends the bot's leg once the human
				// answers, and the hangup failure branch pushes it too (T5).
				data.HasColdTransfer = true
				data.NeedsAppendFrame = true
				data.NeedsEndFrame = true
				continue
			}
			if t.Builtin != "" {
				// prebuilt end_call: bodyless, speaks the goodbye then EndFrame.
				data.NeedsEndFrame = true
				if t.Instructions != "" {
					data.NeedsAppendFrame = true
				}
				continue
			}
			if t.Local {
				data.NeedsInspect = true // isawaitable on the user handler (V13)
			} else {
				data.NeedsHTTPX = true // webhook tool POSTs with httpx
			}
			if t.Auth != nil {
				data.AuthKinds.add(t.Auth.Kind) // one helper per scheme in use (V8)
			}
			if t.EndsCall {
				data.NeedsEndFrame = true
			}
		}
		for _, d := range a.Delegates {
			data.HasFlows = true // tasks run as Flows on the owning worker (C8)
			// then: end ends via the Flows end_conversation post-action, not a
			// raw EndFrame (V4/B2); only a non-ending delegate restores the role.
			if d.Then != "end" {
				data.NeedsRoleRestore = true
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
					if t.Auth != nil {
						data.AuthKinds.add(t.Auth.Kind)
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

	// pipecat.frames.frames names ride one merged import (V2), sorted at the end
	// so the merged import matches isort whatever order the flags are read in.
	if data.NeedsEndFrame {
		data.FrameImports = append(data.FrameImports, "EndFrame")
	}
	if data.HasFlows {
		// delegate @tool resolves its call with run_llm=False (V7)
		data.FrameImports = append(data.FrameImports, "FunctionCallResultProperties")
	}
	if data.NeedsAppendFrame {
		data.FrameImports = append(data.FrameImports, "LLMMessagesAppendFrame")
	}
	if data.NeedsRoleRestore {
		data.FrameImports = append(data.FrameImports, "LLMUpdateSettingsFrame")
	}
	if data.GreetingText != "" {
		data.FrameImports = append(data.FrameImports, "TTSSpeakFrame")
	}
	slices.Sort(data.FrameImports)
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
	promptConst := promptConstName(name)
	// A templated prompt is rendered per session from the call state; an untouched
	// one stays the bare module constant it always was.
	prompt := promptExpr(promptConst, def.Instructions, pipecatStateExpr)
	llm, err := agentLLMService(target.Models.Reason[def.Model], prompt, env)
	if err != nil {
		return pipecatAgent{}, fmt.Errorf("agent %q: %w", name, err)
	}
	tts, err := ttsService(target.Models.Speak[def.Voice], env)
	if err != nil {
		return pipecatAgent{}, fmt.Errorf("agent %q: %w", name, err)
	}
	built := pipecatAgent{
		Name: name, Class: pyName(name) + "Agent", Prompt: def.Instructions,
		PromptConst: promptConst, PromptExpr: prompt, LLM: llm, TTS: tts,
	}

	for _, ref := range def.Tools {
		if tool, ok := agent.Tools[ref]; ok {
			built.Tools = append(built.Tools, buildTool(ref, tool, agent.Variables, env))
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
			tool, err := humanTransferTool(ref, name, c, target, env)
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
	if strings.TrimSpace(task.Instructions) == "" {
		return pipecatTask{}, fmt.Errorf("task %q instructions must not be empty", name)
	}
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
		built.Tools = append(built.Tools, buildTool(ref, tool, agent.Variables, env))
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

// pipecatStateExpr is how emitted Pipecat code reaches the call state: an agent
// @tool method has it on self, a flows handler receives it as a bound kwarg.
const pipecatStateExpr = "state"

func buildTool(name string, tool ir.Tool, variables map[string]ir.Variable, env *envSet) pipecatTool {
	if tool.URLEnv != "" {
		env.add(tool.URLEnv)
	}
	// The token rides its own env var, never the spec (SCHEMA §5.3).
	if tool.Auth != nil {
		env.add(tool.Auth.TokenEnv)
	}
	inject, needed := loweredInject(tool, variables, pipecatStateExpr)
	built := pipecatTool{
		Name: name, MethodName: name, Description: tool.Description, URLEnv: tool.URLEnv,
		URLExpr: urlExpr(tool, pipecatStateExpr), Inject: inject, Needed: needed,
		NeedsState: len(inject) > 0 || ir.HasTemplate(tool.Path),
		Auth:       loweredAuth(tool.Auth),
		Local:      tool.Execution == ir.ToolLocal, HandlerSource: tool.HandlerSource,
		Builtin: tool.Builtin, Instructions: tool.Instructions,
		EndsCall: tool.Effect == ir.ToolEndsConversation, Interruption: interruptionValue(tool.Interruption),
	}
	built.Args = append(built.Args, inputFields(tool.Input)...)
	argNames := make([]string, 0, len(built.Args))
	for _, arg := range built.Args {
		argNames = append(argNames, arg.Name)
	}
	built.JSONBody = requestBody(argNames, inject)
	built.CallKwargs = callKwargs(argNames, inject)
	built.NeededLiteral = neededLiteral(needed)
	// Flow nodes advertise the tool via a FlowsFunctionSchema, which takes the
	// input schema verbatim rather than a Python signature.
	props, _ := tool.Input["properties"].(map[string]any)
	built.InputProps = pyLiteral(props)
	requiredList, _ := tool.Input["required"].([]any)
	built.InputRequired = pyLiteral(requiredList)
	return built
}

// pipecatDestinationExpr renders a resolved destination as Python: a quoted
// literal, or an os.environ lookup when the target defers it to an env var
// (N26). The env name is registered so it reaches .env.example and REQUIRED_ENV.
func pipecatDestinationExpr(destination string, env *envSet) string {
	if name := ir.DestinationEnv(destination); name != "" {
		env.add(name)
		return "os.environ[" + pyLiteral(name) + "]"
	}
	return pyLiteral(destination)
}

// humanTransferTool lowers a human_transfer to a @tool. Only cold exists on
// Pipecat, and only via Daily's native `sip_call_transfer` (SPEC C4, V1):
// the bot announces, Daily reroutes the leg, the bot drops out. Warm has no
// Pipecat primitive; the gate rejects it before this runs, and the error here
// is the defense in depth for a gate bug.
func humanTransferTool(name, agent string, c *ir.HumanTransfer, target ir.Target, env *envSet) (pipecatTool, error) {
	_ = agent
	destination, ok := target.Destinations[c.Destination]
	if !ok {
		return pipecatTool{}, fmt.Errorf("human transfer %q destination %q missing on target %q", name, c.Destination, target.Name)
	}
	if target.Telephony != nil {
		return pipecatTool{}, fmt.Errorf("human transfer %q: the (%s, %s) route has no transfer primitive; Pipecat cold transfer rides the Daily route (transport daily-sip)", name, target.Telephony.Key.Transport, target.Telephony.Key.Carrier)
	}
	env.add("DAILY_API_KEY")
	desc := c.When
	if desc == "" {
		desc = "Transfer the caller to a human."
	}
	tool := pipecatTool{Name: name, MethodName: name, Description: desc}
	switch c.Mode {
	case ir.TransferCold:
		tool.EndsCall = true
		tool.ColdDestination = pipecatDestinationExpr(destination, env)
		tool.HangupOnUnavailable = c.OnUnavailable == ir.OnUnavailableHangup
		return tool, nil
	case ir.TransferWarm:
		return pipecatTool{}, fmt.Errorf("human transfer %q: Pipecat has no native warm transfer; warm compiles on (livekit, sip) only", name)
	}
	return pipecatTool{}, fmt.Errorf("human transfer %q mode %q has no Pipecat lowering", name, c.Mode)
}

// inputFields flattens a tool input JSON Schema object into ordered args,
// carrying each property's type, description, and enum so the agent-level @tool
// signature + docstring present the LLM the schema the tool YAML declares (V1).
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
	// Required params first (Python forbids a non-default arg after a defaulted
	// one), alphabetical within each group so the signature is stable (B3/V5).
	sort.Slice(names, func(i, j int) bool {
		if required[names[i]] != required[names[j]] {
			return required[names[i]]
		}
		return names[i] < names[j]
	})
	args := make([]pipecatArg, 0, len(names))
	for _, n := range names {
		prop, _ := props[n].(map[string]any)
		jsonType, _ := prop["type"].(string)
		pyType, pyDefault := pyArgTypeDefault(jsonType, required[n])
		arg := pipecatArg{Name: n, PyType: pyType, PyDefault: pyDefault, Required: required[n]}
		if desc, ok := prop["description"].(string); ok {
			arg.Description = desc
		}
		if enum, ok := prop["enum"].([]any); ok {
			for _, v := range enum {
				arg.Enum = append(arg.Enum, fmt.Sprintf("%v", v))
			}
		}
		args = append(args, arg)
	}
	return args
}

// pyArgTypeDefault maps a JSON-Schema primitive type to a Python signature
// annotation and, for an optional arg, a type-appropriate default literal. A
// None default on a complex type widens the annotation to keep it type-clean.
func pyArgTypeDefault(jsonType string, required bool) (pyType, pyDefault string) {
	switch jsonType {
	case "integer":
		pyType, pyDefault = "int", "0"
	case "number":
		pyType, pyDefault = "float", "0.0"
	case "boolean":
		pyType, pyDefault = "bool", "False"
	case "array":
		pyType, pyDefault = "list", "None"
	case "object":
		pyType, pyDefault = "dict", "None"
	default:
		pyType, pyDefault = "str", `""`
	}
	if !required && pyDefault == "None" {
		pyType += " | None"
	}
	return pyType, pyDefault
}

// defaultGreetingInstruction is the developer message behind a model-written
// opening. The LiveKit driver hands the same words to generate_reply
// (templates/livekit_v1/agent.py.tmpl), so both drivers open a call the same way.
const defaultGreetingInstruction = "Greet the caller and offer to help."

// applyConversation lowers the conversation block into the template model:
// greeting activation, interruption turn-strategies, idle timeout, max duration.
func applyConversation(c *ir.Conversation, data *pipecatData) {
	var greeting *ir.Greeting
	if c != nil {
		greeting = c.Greeting
	}
	applyGreeting(greeting, data)
	if c == nil {
		return
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

// applyGreeting lowers greeting activation. With no greeting block the agent
// opens with a model-written line, the same default livekitGreetingFor picks
// (SCHEMA N20). Silence is never the default, because on a call it reads as a
// dead line (docs/user/reference/conversation.md).
func applyGreeting(g *ir.Greeting, data *pipecatData) {
	if g == nil || (g.SpeaksFirst == ir.SpeaksFirstAgent && g.Text == "") {
		data.GreetingInstruction = defaultGreetingInstruction
		data.GreetingRunLLM = "True"
		return
	}
	if g.SpeaksFirst == ir.SpeaksFirstAgent {
		data.GreetingText = g.Text
		// The fixed line may name variables known at call start (C11); it is
		// rendered once, when the session opens.
		data.GreetingExpr = pyQuote(g.Text)
		if ir.HasTemplate(g.Text) {
			data.GreetingExpr = fmt.Sprintf("_render(%s, %s)", pyQuote(g.Text), pipecatStateExpr)
		}
		data.GreetingRunLLM = "False"
		return
	}
	data.GreetingRunLLM = "False" // speaks_first: user — stay silent until the caller talks
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
func resolvePipecatService(role targetcap.Role, binding ir.Binding, env *envSet, extraSettings ...pyKV) (pipecatService, error) {
	call, entry, err := resolveService(defaultCatalog, targetcap.Pipecat, role, binding, pipecatEnvRef, env, extraSettings...)
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

func sttService(binding *ir.Binding, env *envSet) (pipecatService, error) {
	if binding == nil {
		return pipecatService{}, fmt.Errorf("pipecat listen binding is missing a model")
	}
	return resolvePipecatService(targetcap.Listen, *binding, env)
}

// agentLLMService builds an agent's LLM; the prompt nests into Settings as
// system_instruction (the workers-model shape, driver-pipecat C2), referenced
// through its module constant so builder and restore share one copy (V2).
func agentLLMService(binding ir.Binding, promptRef string, env *envSet) (pipecatService, error) {
	return resolvePipecatService(targetcap.Reason, binding, env,
		pyKV{Key: "system_instruction", Value: promptRef})
}

// serviceUsesLanguage reports whether a service emits a language kwarg, which
// resolvePipecatService wraps in the Language(...) enum — so bot.py imports
// Language only then (N16: language is per-model and often unset).
func serviceUsesLanguage(s pipecatService) bool {
	for _, args := range [][]pyKV{s.Call.Args, s.Call.SettingsArgs} {
		for _, kv := range args {
			if kv.Key == "language" {
				return true
			}
		}
	}
	return false
}

func ttsService(binding ir.Binding, env *envSet) (pipecatService, error) {
	return resolvePipecatService(targetcap.Speak, binding, env)
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
