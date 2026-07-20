package generate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/slng/unmute/internal/ir"
	targetcap "github.com/slng/unmute/internal/target"
)

// buildLiveKitData lowers the resolved IR + target into the template model.
// listen/speak/reason resolve through the provider catalogue (SLNG default,
// any entry binds); a reason vendor without a native entry falls to the
// LiveKit Inference wildcard row (C8/V19).
func buildLiveKitData(agent *ir.Agent, tgt ir.Target) (livekitData, error) {
	// The driver's templates are Python; a node project would be silently
	// wrong, so fail loud until node templates exist (C1).
	if tgt.SDKLanguage != "" && tgt.SDKLanguage != "python" {
		return livekitData{}, fmt.Errorf("livekit driver emits python projects only; sdk_language %q has no templates yet", tgt.SDKLanguage)
	}
	if err := checkLiveKitPins(tgt.Pins); err != nil {
		return livekitData{}, err
	}
	env := newEnvSet()
	// LiveKit Cloud creds run the worker against a real room (dev/start) and
	// any Inference-routed role; console mode needs only the bound providers'
	// keys (B5/B6). SLNG and any native per-vendor plugin add their own
	// api-key env as bindings are lowered.
	for _, e := range []string{"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"} {
		env.add(e)
	}
	turnVersion, err := livekitTurnVersion(tgt.Models.Turn)
	if err != nil {
		return livekitData{}, err
	}
	data := livekitData{
		Project:     tgt.Name,
		Version:     tgt.Version,
		AgentName:   tgt.Name,
		EntryAgent:  agent.EntryAgent,
		EntryClass:  pyName(agent.EntryAgent),
		TurnVersion: turnVersion,
		Pins:        tgt.Pins,
	}

	entry := agent.Agents[agent.EntryAgent]
	stt, err := livekitSTTService(tgt.Models.Listen, agent.Language, env)
	if err != nil {
		return livekitData{}, err
	}
	data.STT = livekitChain{Primary: stt}
	// The selected listen model's fallback chain lowers to stt.FallbackAdapter
	// (T16); each entry resolves through the same catalogue path as the primary.
	for _, fallback := range tgt.Models.ListenFallbacks {
		binding := fallback.Binding
		svc, err := livekitSTTService(&binding, agent.Language, env)
		if err != nil {
			return livekitData{}, fmt.Errorf("listen fallback %q: %w", fallback.Name, err)
		}
		data.STT.Chain = append(data.STT.Chain, svc)
	}
	data.SessionLLM, err = livekitReasonLLM(agent, tgt, entry.Model, env)
	if err != nil {
		return livekitData{}, fmt.Errorf("entry agent %q: %w", agent.EntryAgent, err)
	}
	data.SessionTTS, err = livekitTTSService(tgt.Models.Speak[entry.Voice], agent.Language, env)
	if err != nil {
		return livekitData{}, fmt.Errorf("entry agent %q: %w", agent.EntryAgent, err)
	}

	for _, name := range sortedAgentNames(agent) {
		built, err := buildLiveKitAgent(agent, tgt, name, agent.Agents[name], entry, env)
		if err != nil {
			return livekitData{}, err
		}
		data.Agents = append(data.Agents, built)
	}

	// Tasks reached by any delegate: single tasks and each group step.
	used := map[string]bool{}
	for _, a := range data.Agents {
		for _, d := range a.Delegates {
			if d.Task != nil {
				used[d.Task.ID] = true
			}
			for _, s := range d.Steps {
				used[s.ID] = true
			}
			if len(d.Steps) > 0 && !d.Isolated {
				data.NeedsTaskGroups = true
			}
		}
	}
	for _, name := range sortedKeys(used) {
		task, ok := agent.Tasks[name]
		if !ok {
			return livekitData{}, fmt.Errorf("task group step %q is not a task", name)
		}
		built, err := buildLiveKitTask(agent, tgt, name, task, env)
		if err != nil {
			return livekitData{}, err
		}
		data.Tasks = append(data.Tasks, built)
	}
	data.NeedsTasks = len(data.Tasks) > 0

	// Local handler files ride the artifact (tools/<name>.py); mcp mounts and
	// local wrappers pull their imports.
	seenLocal := map[string]bool{}
	collectTools := func(tools []livekitTool, servers []livekitMCPServer) {
		data.NeedsMCP = data.NeedsMCP || len(servers) > 0
		for _, tool := range tools {
			if !tool.Local || seenLocal[tool.Method] {
				continue
			}
			seenLocal[tool.Method] = true
			data.NeedsInspect = true
			data.LocalTools = append(data.LocalTools, livekitLocalTool{
				Name: tool.Method, Source: agent.Tools[tool.Method].HandlerSource,
			})
		}
	}
	for _, a := range data.Agents {
		collectTools(a.Tools, a.MCPServers)
	}
	for _, t := range data.Tasks {
		collectTools(t.Tools, t.MCPServers)
	}
	sort.Slice(data.LocalTools, func(i, j int) bool { return data.LocalTools[i].Name < data.LocalTools[j].Name })

	// History-shaping helpers emit only when a transfer or task uses them (V5).
	for _, a := range data.Agents {
		for _, tr := range a.Transfers {
			data.NeedsLastN = data.NeedsLastN || strings.HasPrefix(tr.CtxExpr, "_last_n(")
			data.NeedsSummarize = data.NeedsSummarize || tr.Summary != nil
		}
		for _, d := range a.Delegates {
			if d.Task != nil {
				data.NeedsLastN = data.NeedsLastN || strings.HasPrefix(d.Task.CtxExpr, "_last_n(")
				data.NeedsSummarize = data.NeedsSummarize || d.Task.Summary != nil
			}
		}
	}

	// Typed shared state (SCHEMA 4.4): variables lower to a Userdata dataclass
	// on the session; `assign` and `requires` read and write its fields.
	for _, name := range sortedVarNames(agent) {
		v := agent.Variables[name]
		def := "None"
		if v.Default != nil {
			def = pyLiteral(v.Default)
		}
		data.Vars = append(data.Vars, livekitVar{Name: name, PyType: pyType(v.Type), Default: def})
	}
	data.HasVars = len(data.Vars) > 0

	// Prompt constants, ordered agents-then-tasks for a stable file.
	for _, a := range data.Agents {
		data.Prompts = append(data.Prompts, livekitPrompt{Const: a.PromptConst, Body: agent.Agents[a.Name].Instructions})
	}
	for _, t := range data.Tasks {
		data.Prompts = append(data.Prompts, livekitPrompt{Const: t.PromptConst, Body: livekitTaskPrompt(agent.Tasks[t.Name], t.Result)})
		for _, tool := range t.Tools {
			if tool.URLEnv != "" {
				data.NeedsHTTPX = true
			}
		}
	}

	applyLiveKitConversation(agent.Conversation, &data)

	// Telephony: outbound + AMD voicemail (V8/N6); cold/warm transfer imports.
	channelNames := make([]string, 0, len(agent.Channels))
	for name := range agent.Channels {
		channelNames = append(channelNames, name)
	}
	sort.Strings(channelNames)
	for _, name := range channelNames {
		ch := agent.Channels[name]
		if ch.Kind == ir.ChannelTelephony && ch.Outbound != nil && *ch.Outbound {
			data.Outbound = &livekitOutbound{LeaveMessage: ch.OnVoicemail == ir.VoicemailLeaveMessage}
			env.add("LIVEKIT_SIP_OUTBOUND_TRUNK")
			break
		}
	}
	for _, a := range data.Agents {
		for _, ht := range a.HumanTransfers {
			data.HasColdTransfer = data.HasColdTransfer || !ht.Warm
			data.HasWarmTransfer = data.HasWarmTransfer || ht.Warm
		}
	}

	data.Notes = append(data.Notes, livekitServiceNotes(data)...)
	if data.HasWarmTransfer {
		data.Notes = append(data.Notes, "human_transfer warm uses livekit-agents beta.workflows on Python (Beta)")
	}
	if tgt.Models.Turn != nil {
		data.Notes = append(data.Notes, "turn role lowers to LiveKit Inference turn detection; its binding placement is advisory")
	}

	data.InferenceUses = livekitInferenceUses(data)

	data.PluginModules = collectLiveKitPlugins(data)
	data.Deps = livekitDeps(data)
	data.RequiredEnv = env.sorted()
	return data, nil
}

// livekitInferenceUses lists the bindings that route through LiveKit Inference,
// which needs LIVEKIT_API_KEY/SECRET even in credless console mode (C2/C7): any
// resolved service on the `inference.*` classes (the reason wildcard, provider:
// livekit) and the cloud turn detector (turn-detector → v1; the mini runs
// local, an absent binding auto-selects local, V18). Empty means console runs
// on the bound providers' keys alone (the scaffold default).
func livekitInferenceUses(data livekitData) []string {
	set := map[string]bool{}
	for _, svc := range livekitServices(data) {
		if svc.Entry.Call != nil && strings.HasPrefix(svc.Entry.Call.Class, "inference.") {
			set[fmt.Sprintf("%s provider %q", svc.Entry.Role, svc.Vendor)] = true
		}
	}
	uses := sortedKeys(set)
	if data.TurnVersion == "v1" {
		uses = append(uses, "turn detection (cloud turn-detector)")
	}
	if len(uses) == 0 {
		return nil
	}
	return uses
}

// livekitServices lists every resolved service in the template model.
func livekitServices(data livekitData) []livekitService {
	services := append(data.STT.services(), data.SessionTTS)
	services = append(services, data.SessionLLM.services()...)
	for _, a := range data.Agents {
		if a.LLM != nil {
			services = append(services, a.LLM.services()...)
		}
		if a.TTS != nil {
			services = append(services, *a.TTS)
		}
		for _, tr := range a.Transfers {
			if tr.Summary != nil {
				services = append(services, tr.Summary.services()...)
			}
		}
		for _, d := range a.Delegates {
			if d.Task != nil && d.Task.Summary != nil {
				services = append(services, d.Task.Summary.services()...)
			}
		}
	}
	for _, t := range data.Tasks {
		if t.LLM != nil {
			services = append(services, t.LLM.services()...)
		}
	}
	return services
}

// collectLiveKitPlugins merges the used entries' plugin imports into one
// sorted `from livekit.plugins import ...` module list. Silero is always
// present (the session VAD).
func collectLiveKitPlugins(data livekitData) []string {
	const prefix = "from livekit.plugins import "
	set := map[string]bool{"silero": true}
	for _, svc := range livekitServices(data) {
		if strings.HasPrefix(svc.Entry.Import, prefix) {
			set[strings.TrimPrefix(svc.Entry.Import, prefix)] = true
		}
	}
	return sortedKeys(set)
}

// livekitServiceNotes lists every used catalogue entry in the compile report.
func livekitServiceNotes(data livekitData) []string {
	set := map[string]bool{}
	for _, svc := range livekitServices(data) {
		if svc.Entry.Call == nil {
			continue
		}
		set[fmt.Sprintf("%s: %s via %s (%s, verified %s)",
			svc.Entry.Role, svc.Vendor, svc.Entry.Call.Class, installLabel(svc.Entry), svc.Entry.Verified)] = true
	}
	return sortedKeys(set)
}

// applyLiveKitConversation lowers the conversation block (V16): interruption
// options, the generated ignore-phrase filter, thinking audio, inactivity
// timeouts, and the max-duration timer.
func applyLiveKitConversation(c *ir.Conversation, data *livekitData) {
	if c == nil {
		return
	}
	if c.Interruption != nil {
		data.Interruption = &livekitInterruption{
			Enabled:  c.Interruption.Enabled == nil || *c.Interruption.Enabled,
			MinWords: c.Interruption.MinimumWords,
		}
		for _, p := range c.Interruption.IgnorePhrases {
			data.IgnorePhrases = append(data.IgnorePhrases, strings.ToLower(p))
		}
	}
	data.ThinkingAudio = c.ThinkingAudio == ir.ThinkingSubtle
	if c.Inactivity != nil {
		data.InactivityNudgeSecs = durationSecs(c.Inactivity.NudgeAfter)
		data.InactivityEndSecs = durationSecs(c.Inactivity.EndAfter)
		if delta := data.InactivityEndSecs - data.InactivityNudgeSecs; delta > 0 {
			data.InactivityEndDeltaSecs = delta
		} else if data.InactivityEndSecs > 0 {
			data.InactivityEndDeltaSecs = 1
		}
	}
	data.MaxDurationSecs = durationSecs(c.MaxDuration)
	data.NeedsAsyncio = data.MaxDurationSecs > 0 ||
		(data.InactivityEndSecs > 0 && data.InactivityNudgeSecs > 0)
}

func buildLiveKitAgent(agent *ir.Agent, tgt ir.Target, name string, def, entry ir.AgentDef, env *envSet) (livekitAgent, error) {
	built := livekitAgent{
		Name: name, Class: pyName(name), PromptConst: promptConst(name),
		IsEntry: name == agent.EntryAgent,
	}
	if def.Model != entry.Model {
		llm, err := livekitReasonLLM(agent, tgt, def.Model, env)
		if err != nil {
			return livekitAgent{}, fmt.Errorf("agent %q: %w", name, err)
		}
		built.LLM = &llm
	}
	if def.Voice != entry.Voice {
		tts, err := livekitTTSService(tgt.Models.Speak[def.Voice], agent.Language, env)
		if err != nil {
			return livekitAgent{}, fmt.Errorf("agent %q: %w", name, err)
		}
		built.TTS = &tts
	}
	if built.IsEntry {
		built.Greeting = livekitGreetingFor(agent.Conversation)
	}
	mcpByEnv := map[string][]string{}
	for _, ref := range def.Tools {
		if tool, ok := agent.Tools[ref]; ok {
			if tool.Execution == ir.ToolMCP {
				env.add(tool.URLEnv)
				mcpByEnv[tool.URLEnv] = append(mcpByEnv[tool.URLEnv], ref)
				continue
			}
			lowered, err := buildLiveKitTool(ref, tool, env)
			if err != nil {
				return livekitAgent{}, fmt.Errorf("agent %q: %w", name, err)
			}
			built.Tools = append(built.Tools, lowered)
			continue
		}
		control, ok := agent.Controls[ref]
		if !ok {
			return livekitAgent{}, fmt.Errorf("agent %q references unknown tool/control %q", name, ref)
		}
		switch c := control.(type) {
		case *ir.AgentTransfer:
			transfer := livekitTransfer{
				Method: ref, When: transferWhen(c), TargetClass: pyName(c.To),
				Requires: c.Requires,
			}
			if c.Context.History == ir.HistorySummary {
				summarizer, err := livekitReasonLLM(agent, tgt, c.Context.Summarizer, env)
				if err != nil {
					return livekitAgent{}, fmt.Errorf("agent %q transfer %q summarizer: %w", name, ref, err)
				}
				transfer.Summary = &summarizer
			} else {
				transfer.CtxExpr, _ = livekitCtxExpr(c.Context.TaskContext)
			}
			// context.variables (D7): a subset resets the fields the transfer
			// does not carry; `all` carries the userdata untouched.
			if !c.Context.Variables.All {
				carried := map[string]bool{}
				for _, n := range c.Context.Variables.Names {
					carried[n] = true
				}
				for _, vname := range sortedVarNames(agent) {
					if carried[vname] {
						continue
					}
					v := agent.Variables[vname]
					def := "None"
					if v.Default != nil {
						def = pyLiteral(v.Default)
					}
					transfer.ResetVars = append(transfer.ResetVars, livekitVar{Name: vname, PyType: pyType(v.Type), Default: def})
				}
			}
			built.Transfers = append(built.Transfers, transfer)
		case *ir.HumanTransfer:
			dest, ok := tgt.Destinations[c.Destination]
			if !ok {
				return livekitAgent{}, fmt.Errorf("agent %q human_transfer %q: destination %q is not in the target's destinations map", name, ref, c.Destination)
			}
			ht := livekitHumanTransfer{
				Method: ref, When: humanTransferWhen(c), To: dest,
				Warm: c.Mode == ir.TransferWarm,
			}
			if ht.Warm {
				// WarmTransferTask reads LIVEKIT_SIP_OUTBOUND_TRUNK itself.
				env.add("LIVEKIT_SIP_OUTBOUND_TRUNK")
			}
			built.HumanTransfers = append(built.HumanTransfers, ht)
		case *ir.Delegate:
			delegate, err := buildLiveKitDelegate(agent, tgt, ref, c, env)
			if err != nil {
				return livekitAgent{}, fmt.Errorf("agent %q: %w", name, err)
			}
			built.Delegates = append(built.Delegates, delegate)
		}
	}
	built.MCPServers = livekitMCPServers(mcpByEnv)
	return built, nil
}

// livekitMCPServers collapses mcp tools by server env into sorted mounts.
func livekitMCPServers(byEnv map[string][]string) []livekitMCPServer {
	envs := make([]string, 0, len(byEnv))
	for e := range byEnv {
		envs = append(envs, e)
	}
	sort.Strings(envs)
	servers := make([]livekitMCPServer, 0, len(envs))
	for _, e := range envs {
		tools := byEnv[e]
		sort.Strings(tools)
		servers = append(servers, livekitMCPServer{URLEnv: e, Tools: tools})
	}
	return servers
}

func buildLiveKitDelegate(agent *ir.Agent, tgt ir.Target, ref string, c *ir.Delegate, env *envSet) (livekitDelegate, error) {
	if c.Task != "" {
		task, ok := agent.Tasks[c.Task]
		if !ok {
			return livekitDelegate{}, fmt.Errorf("delegate %q references unknown task %q", ref, c.Task)
		}
		single := &livekitSingleTask{Class: pyName(c.Task), ID: c.Task}
		// The task's own context (N12) shapes its entry; group steps instead
		// take the group's scope (SCHEMA 4.6), handled in the group path.
		if task.Context.History == ir.HistorySummary {
			summarizer, err := livekitReasonLLM(agent, tgt, task.Context.Summarizer, env)
			if err != nil {
				return livekitDelegate{}, fmt.Errorf("delegate %q task %q summarizer: %w", ref, c.Task, err)
			}
			single.Summary = &summarizer
		} else {
			single.CtxExpr, _ = livekitCtxExpr(task.Context)
		}
		for variable, path := range c.Assign {
			single.Assign = append(single.Assign, livekitAssign{Var: variable, Field: strings.TrimPrefix(path, "result.")})
		}
		sort.Slice(single.Assign, func(i, j int) bool { return single.Assign[i].Var < single.Assign[j].Var })
		// A single task always returns to the owner (SCHEMA 4.7); the AgentTask
		// hands back the typed result only (C4/N13).
		return livekitDelegate{Method: ref, When: delegateWhen(c), Task: single, Then: "return"}, nil
	}
	group, ok := agent.TaskGroups[c.Group]
	if !ok {
		return livekitDelegate{}, fmt.Errorf("delegate %q references unknown task_group %q", ref, c.Group)
	}
	// C3: TaskGroup always shares context, so `isolated` lowers to a generated
	// sequence of standalone AgentTasks (each starts fresh, C4) instead.
	delegate := livekitDelegate{
		Method: ref, When: delegateWhen(c), Then: string(group.Then),
		Isolated: group.ContextScope == ir.ContextIsolated,
	}
	// N13/§4.7: return hands the owner the typed results; transfer and end do not
	// return, so the tool description must say so (the model must not wait for a
	// result that never comes). The lowerings themselves live in the template.
	switch group.Then {
	case ir.GroupReturn:
	case ir.GroupTransfer:
		delegate.ThenClass = pyName(group.ThenTarget)
		delegate.When += " This flow does not return to you: when it finishes the caller is handed to the " + group.ThenTarget + "."
	case ir.GroupEnd:
		delegate.When += " This flow does not return to you: when it finishes the call ends."
	default:
		return livekitDelegate{}, fmt.Errorf("delegate %q group %q: livekit driver cannot lower then %q", ref, c.Group, group.Then)
	}
	for _, step := range group.Steps {
		delegate.Steps = append(delegate.Steps, livekitStep{Class: pyName(step), ID: step, Desc: humanize(step)})
	}
	return delegate, nil
}

func buildLiveKitTask(agent *ir.Agent, tgt ir.Target, name string, task ir.Task, env *envSet) (livekitTask, error) {
	built := livekitTask{Name: name, Class: pyName(name), PromptConst: promptConst(name)}
	// Per-task model (B1): AgentTask takes its own llm=, resolved through the
	// catalogue like any per-agent override. Same profile as the entry agent =
	// the session default, no kwarg.
	if task.Model != "" && task.Model != agent.Agents[agent.EntryAgent].Model {
		taskLLM, err := livekitReasonLLM(agent, tgt, task.Model, env)
		if err != nil {
			return livekitTask{}, fmt.Errorf("task %q model %q: %w", name, task.Model, err)
		}
		built.LLM = &taskLLM
	}
	for _, fname := range sortedResultNames(task.Result) {
		built.Result = append(built.Result, livekitArg{Name: fname, PyType: resultPyType(task.Result[fname]), Required: true})
	}
	mcpByEnv := map[string][]string{}
	for _, ref := range task.Tools {
		tool, ok := agent.Tools[ref]
		if !ok {
			return livekitTask{}, fmt.Errorf("task %q references unknown tool %q", name, ref)
		}
		if tool.Execution == ir.ToolMCP {
			env.add(tool.URLEnv)
			mcpByEnv[tool.URLEnv] = append(mcpByEnv[tool.URLEnv], ref)
			continue
		}
		lowered, err := buildLiveKitTool(ref, tool, env)
		if err != nil {
			return livekitTask{}, fmt.Errorf("task %q: %w", name, err)
		}
		built.Tools = append(built.Tools, lowered)
	}
	built.MCPServers = livekitMCPServers(mcpByEnv)
	return built, nil
}

// buildLiveKitTool lowers a webhook or local tool to a @function_tool method
// (agents and tasks share the shape); mcp tools mount as servers upstream.
// client/provider_hosted/builtin are table-denied and cannot validate green.
func buildLiveKitTool(name string, tool ir.Tool, env *envSet) (livekitTool, error) {
	switch tool.Execution {
	case ir.ToolWebhook:
		env.add(tool.URLEnv)
		return livekitTool{
			Method: name, Description: tool.Description, URLEnv: tool.URLEnv,
			Args:             livekitToolArgs(tool.Input),
			EndsConversation: tool.Effect == ir.ToolEndsConversation,
		}, nil
	case ir.ToolLocal:
		return livekitTool{
			Method: name, Description: tool.Description, Local: true,
			Args:             livekitToolArgs(tool.Input),
			EndsConversation: tool.Effect == ir.ToolEndsConversation,
		}, nil
	default:
		return livekitTool{}, fmt.Errorf("tool %q: execution %q has no livekit lowering", name, tool.Execution)
	}
}

// --- binding → call mapping ------------------------------------------------
// The catalogue (internal/target/catalog_livekit.go) picks the class, plugin
// module, install path, and constructor shape; model routes keep their entry's
// named form (the SLNG plugin strips the slng/ prefix, Inference joins
// provider/model).

// livekitEnvRef renders the driver's environment-lookup idiom.
func livekitEnvRef(name string) string { return "os.environ.get(" + pyQuote(name) + ")" }

func resolveLiveKitService(role targetcap.Role, binding ir.Binding, language string, env *envSet) (livekitService, error) {
	call, entry, err := resolveService(defaultCatalog, targetcap.LiveKit, role, binding, language, livekitEnvRef, env)
	if err != nil {
		return livekitService{}, err
	}
	return livekitService{Call: call, Entry: entry, Vendor: firstNonEmpty(binding.Provider, "openai")}, nil
}

func livekitSTTService(binding *ir.Binding, language string, env *envSet) (livekitService, error) {
	if binding == nil {
		return livekitService{}, fmt.Errorf("livekit listen binding is missing a model")
	}
	return resolveLiveKitService(targetcap.Listen, *binding, language, env)
}

func livekitTTSService(binding ir.Binding, language string, env *envSet) (livekitService, error) {
	return resolveLiveKitService(targetcap.Speak, binding, language, env)
}

func livekitChainService(binding ir.Binding, env *envSet) (livekitService, error) {
	return resolveLiveKitService(targetcap.Reason, binding, "", env)
}

// livekitTurnVersion maps the target's turn: binding to the
// inference.TurnDetector version (V18, B5). turn-detector-mini runs fully
// local — no LiveKit Cloud creds; an absent binding emits no version kwarg,
// so the SDK auto-selects and falls back to the mini model with a warning
// instead of raising (C5).
func livekitTurnVersion(binding *ir.Binding) (string, error) {
	if binding == nil || binding.Model == "" {
		return "", nil
	}
	switch binding.Model {
	case "turn-detector-mini":
		return "v1-mini", nil
	case "turn-detector":
		return "v1", nil
	default:
		return "", fmt.Errorf("livekit turn model %q is not recognized; use turn-detector-mini (local) or turn-detector (LiveKit Cloud)", binding.Model)
	}
}

// livekitReasonLLM resolves a reason profile plus its fallback chain (V4).
// Every profile in the chain must carry its own reason binding; validation
// has already checked slot kind, placement, and cycles.
func livekitReasonLLM(agent *ir.Agent, tgt ir.Target, profile string, env *envSet) (livekitChain, error) {
	primary, err := livekitChainService(tgt.Models.Reason[profile], env)
	if err != nil {
		return livekitChain{}, fmt.Errorf("model %q: %w", profile, err)
	}
	out := livekitChain{Primary: primary}
	for _, fb := range agent.Models[profile].Fallback {
		svc, err := livekitChainService(tgt.Models.Reason[fb], env)
		if err != nil {
			return livekitChain{}, fmt.Errorf("model %q fallback %q: %w", profile, fb, err)
		}
		out.Chain = append(out.Chain, svc)
	}
	return out, nil
}

// livekitCtxExpr lowers a context block's history shaping (V5) to a Python
// expression for the handed-over ChatContext. "" means history: reset — the
// receiver starts fresh. history: summary is handled by the caller (it needs
// an await, not an expression). needsLastN reports use of the _last_n helper.
func livekitCtxExpr(c ir.TaskContext) (expr string, needsLastN bool) {
	excludeCalls := c.IncludeToolCalls != nil && !*c.IncludeToolCalls
	switch c.History {
	case ir.HistoryReset:
		return "", false
	case ir.HistoryMessages:
		return `llm.ChatContext(items=[m for m in self.chat_ctx.messages() if m.role in ("user", "assistant")])`, false
	case ir.HistoryLastN:
		if excludeCalls {
			return fmt.Sprintf("_last_n(self.chat_ctx, %d, exclude_function_call=True)", c.MaxMessages), true
		}
		return fmt.Sprintf("_last_n(self.chat_ctx, %d)", c.MaxMessages), true
	default: // full
		if excludeCalls {
			return "self.chat_ctx.copy(exclude_instructions=True, exclude_function_call=True)", false
		}
		return "self.chat_ctx.copy(exclude_instructions=True)", false
	}
}

// --- small helpers ---------------------------------------------------------

func promptConst(name string) string {
	return strings.ToUpper(strings.NewReplacer("-", "_").Replace(name)) + "_PROMPT"
}

func transferWhen(c *ir.AgentTransfer) string {
	if c.When != "" {
		return c.When
	}
	return "Transfer the caller to the " + c.To + "."
}

func humanTransferWhen(c *ir.HumanTransfer) string {
	if c.When != "" {
		return c.When
	}
	return "Transfer the caller to a human agent."
}

func delegateWhen(c *ir.Delegate) string {
	if c.When != "" {
		return c.When
	}
	return "Run this flow."
}

func humanize(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(name, "_", " "), "-", " ")
}

func resultPyType(field ir.ResultField) string {
	if field.Schema != nil {
		return "dict" // nested result schema (code targets only): a JSON object arg
	}
	if len(field.Enum) > 0 {
		return "str"
	}
	return pyType(field.Type)
}

func jsonPyType(t string) string {
	switch t {
	case "integer":
		return "int"
	case "number":
		return "float"
	case "boolean":
		return "bool"
	default:
		return "str"
	}
}

func livekitToolArgs(input map[string]any) []livekitArg {
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
	args := make([]livekitArg, 0, len(names))
	for _, n := range names {
		pt := "str"
		if prop, ok := props[n].(map[string]any); ok {
			if t, ok := prop["type"].(string); ok {
				pt = jsonPyType(t)
			}
		}
		args = append(args, livekitArg{Name: n, PyType: pt, Required: required[n]})
	}
	return args
}

func livekitTaskPrompt(task ir.Task, result []livekitArg) string {
	body := task.Instructions
	if len(result) > 0 {
		names := make([]string, len(result))
		for i, r := range result {
			names[i] = r.Name
		}
		body += "\n\nWhen this step is complete, call `finish` with: " + strings.Join(names, ", ") + "."
	}
	return body
}

func livekitGreetingFor(c *ir.Conversation) *livekitGreeting {
	if c == nil || c.Greeting == nil {
		return &livekitGreeting{RunLLM: true}
	}
	g := c.Greeting
	switch {
	case g.SpeaksFirst == ir.SpeaksFirstAgent && g.Text != "":
		return &livekitGreeting{Say: g.Text}
	case g.SpeaksFirst == ir.SpeaksFirstAgent:
		return &livekitGreeting{RunLLM: true}
	default:
		return &livekitGreeting{Silent: true}
	}
}

// livekitDeps builds the dependency list from the used entries: extras merge
// onto the livekit-agents pin, standalone plugin packages keep their own
// floors (user pins: override them per SCHEMA.md 6.1).
func livekitDeps(data livekitData) []string {
	// A user pin raises a plugin's floor (C6, checked by checkLiveKitPins).
	pinned := func(pkg, constraint string) string {
		if v := data.Pins[pkg]; v != "" {
			return pkg + ">=" + v
		}
		return pkg + constraint
	}
	extras := map[string]bool{}
	packages := map[string]bool{}
	for _, svc := range livekitServices(data) {
		if svc.Entry.Install.Extra != "" {
			extras[svc.Entry.Install.Extra] = true
		}
		if svc.Entry.Install.Package != "" {
			packages[pinned(svc.Entry.Install.Package, svc.Entry.Install.Constraint)] = true
		}
	}
	base := fmt.Sprintf("livekit-agents>=%d.%d", livekitVersionMajor, livekitVersionMinMinor)
	if len(extras) > 0 {
		base = fmt.Sprintf("livekit-agents[%s]>=%d.%d",
			strings.Join(sortedKeys(extras), ","), livekitVersionMajor, livekitVersionMinMinor)
	}
	deps := append([]string{
		base,
		"langfuse>=3",
		"opentelemetry-sdk>=1.33,<2",
		pinned("livekit-plugins-silero", ">=1.6.1"),
		"python-dotenv",
	}, sortedKeys(packages)...)
	if data.NeedsHTTPX {
		deps = append(deps, "httpx")
	}
	sort.Strings(deps)
	return deps
}
