package generate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/slng/unmute/internal/ir"
	targetcap "github.com/slng/unmute/internal/target"
)

// buildLiveKitData lowers the resolved IR + target into the template model.
// listen/speak resolve through the provider catalogue (SLNG default, any
// entry binds); reason lowers to LiveKit Inference (the role's wildcard row).
// Features the driver does not emit yet fail loud here rather than emitting
// broken code (no validate-green / generate-broken drift, compiler V19 in spirit).
func buildLiveKitData(agent *ir.Agent, tgt ir.Target) (livekitData, error) {
	if err := livekitGuards(agent); err != nil {
		return livekitData{}, err
	}
	env := newEnvSet()
	// LiveKit Cloud creds run the worker + Inference (reason/turn). SLNG and any
	// native per-vendor plugin add their own api-key env as bindings are lowered.
	for _, e := range []string{"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"} {
		env.add(e)
	}
	data := livekitData{
		Project:     tgt.Name,
		Version:     tgt.Version,
		AgentName:   tgt.Name,
		EntryClass:  pyName(agent.EntryAgent),
		TurnVersion: "v1",
	}

	entry := agent.Agents[agent.EntryAgent]
	stt, err := livekitSTTService(tgt.Models.Listen, agent.Language, env)
	if err != nil {
		return livekitData{}, err
	}
	data.STT = stt
	data.SessionLLM, err = livekitLLMService(tgt.Models.Reason[entry.Model], env)
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

	// Tasks reached by any delegate (each group step).
	used := map[string]bool{}
	for _, a := range data.Agents {
		for _, d := range a.Delegates {
			for _, s := range d.Steps {
				used[s.ID] = true
			}
		}
	}
	for _, name := range sortedKeys(used) {
		task, ok := agent.Tasks[name]
		if !ok {
			return livekitData{}, fmt.Errorf("task group step %q is not a task", name)
		}
		built, err := buildLiveKitTask(agent, name, task, env)
		if err != nil {
			return livekitData{}, err
		}
		data.Tasks = append(data.Tasks, built)
	}
	data.NeedsTasks = len(data.Tasks) > 0

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

	data.Notes = append(data.Notes, livekitServiceNotes(data)...)
	if agent.Pipeline.Turn != nil {
		data.Notes = append(data.Notes, "turn role lowers to LiveKit Inference turn detection; its binding placement is advisory")
	}
	if c := agent.Conversation; c != nil {
		if c.Inactivity != nil {
			data.Notes = append(data.Notes, "conversation.inactivity is not emitted yet")
		}
		if c.MaxDuration != "" {
			data.Notes = append(data.Notes, "conversation.max_duration is not emitted yet")
		}
	}

	data.PluginModules = collectLiveKitPlugins(data)
	data.Deps = livekitDeps(data)
	data.RequiredEnv = env.sorted()
	return data, nil
}

// livekitServices lists every resolved service in the template model.
func livekitServices(data livekitData) []livekitService {
	services := []livekitService{data.STT, data.SessionLLM, data.SessionTTS}
	for _, a := range data.Agents {
		if a.LLM != nil {
			services = append(services, *a.LLM)
		}
		if a.TTS != nil {
			services = append(services, *a.TTS)
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

// livekitGuards fails loud on features the driver does not emit yet, so a spec
// that validates green never silently loses behavior on LiveKit.
func livekitGuards(agent *ir.Agent) error {
	for name, mp := range agent.Models {
		if len(mp.Fallback) > 0 {
			return fmt.Errorf("livekit driver does not emit model fallback yet (model %q)", name)
		}
	}
	if c := agent.Conversation; c != nil {
		if c.ThinkingAudio != "" {
			return fmt.Errorf("livekit driver does not emit thinking_audio yet")
		}
		if c.Interruption != nil && (c.Interruption.MinimumWords > 0 || len(c.Interruption.IgnorePhrases) > 0) {
			return fmt.Errorf("livekit driver does not shape interruption minimum_words/ignore_phrases yet")
		}
	}
	for name, ch := range agent.Channels {
		if ch.Outbound != nil && *ch.Outbound {
			return fmt.Errorf("livekit driver does not emit outbound calling yet (channel %q)", name)
		}
	}
	return nil
}

func buildLiveKitAgent(agent *ir.Agent, tgt ir.Target, name string, def, entry ir.AgentDef, env *envSet) (livekitAgent, error) {
	built := livekitAgent{
		Name: name, Class: pyName(name), PromptConst: promptConst(name),
		IsEntry: name == agent.EntryAgent,
	}
	if def.Model != entry.Model {
		llm, err := livekitLLMService(tgt.Models.Reason[def.Model], env)
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
	for _, ref := range def.Tools {
		if _, ok := agent.Tools[ref]; ok {
			return livekitAgent{}, fmt.Errorf("agent %q references tool %q: livekit driver emits tools on tasks only for now", name, ref)
		}
		control, ok := agent.Controls[ref]
		if !ok {
			return livekitAgent{}, fmt.Errorf("agent %q references unknown tool/control %q", name, ref)
		}
		switch c := control.(type) {
		case *ir.AgentTransfer:
			if len(c.Requires) > 0 {
				return livekitAgent{}, fmt.Errorf("agent %q transfer %q: livekit driver does not emit transfer requires yet", name, ref)
			}
			if c.Context.History != ir.HistoryFull {
				return livekitAgent{}, fmt.Errorf("agent %q transfer %q: livekit driver emits transfer history: full only for now", name, ref)
			}
			built.Transfers = append(built.Transfers, livekitTransfer{
				Method: ref, When: transferWhen(c), TargetClass: pyName(c.To),
			})
		case *ir.HumanTransfer:
			return livekitAgent{}, fmt.Errorf("agent %q: livekit driver does not emit human_transfer yet (%q)", name, ref)
		case *ir.Delegate:
			delegate, err := buildLiveKitDelegate(agent, ref, c)
			if err != nil {
				return livekitAgent{}, fmt.Errorf("agent %q: %w", name, err)
			}
			built.Delegates = append(built.Delegates, delegate)
		}
	}
	return built, nil
}

func buildLiveKitDelegate(agent *ir.Agent, ref string, c *ir.Delegate) (livekitDelegate, error) {
	if c.Task != "" {
		return livekitDelegate{}, fmt.Errorf("delegate %q: livekit driver emits task-group delegates only for now", ref)
	}
	group, ok := agent.TaskGroups[c.Group]
	if !ok {
		return livekitDelegate{}, fmt.Errorf("delegate %q references unknown task_group %q", ref, c.Group)
	}
	if group.ContextScope == ir.ContextIsolated {
		return livekitDelegate{}, fmt.Errorf("delegate %q group %q: livekit driver emits shared task groups only; isolated not emitted yet", ref, c.Group)
	}
	delegate := livekitDelegate{Method: ref, When: delegateWhen(c), Then: string(group.Then)}
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

func buildLiveKitTask(agent *ir.Agent, name string, task ir.Task, env *envSet) (livekitTask, error) {
	if task.Context.History != ir.HistoryFull {
		return livekitTask{}, fmt.Errorf("task %q: livekit driver emits history: full only for now", name)
	}
	built := livekitTask{Name: name, Class: pyName(name), PromptConst: promptConst(name)}
	for _, fname := range sortedResultNames(task.Result) {
		field := task.Result[fname]
		if field.Schema != nil {
			return livekitTask{}, fmt.Errorf("task %q result %q: livekit driver does not emit nested result schemas yet", name, fname)
		}
		built.Result = append(built.Result, livekitArg{Name: fname, PyType: resultPyType(field), Required: true})
	}
	for _, ref := range task.Tools {
		tool, ok := agent.Tools[ref]
		if !ok {
			return livekitTask{}, fmt.Errorf("task %q references unknown tool %q", name, ref)
		}
		if tool.Execution != ir.ToolWebhook {
			return livekitTask{}, fmt.Errorf("task %q tool %q: livekit driver emits webhook tools only for now (got %q)", name, ref, tool.Execution)
		}
		if tool.URLEnv != "" {
			env.add(tool.URLEnv)
		}
		built.Tools = append(built.Tools, livekitTool{
			Method: ref, Description: tool.Description, URLEnv: tool.URLEnv, Args: livekitToolArgs(tool.Input),
		})
	}
	return built, nil
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

func livekitLLMService(binding ir.Binding, env *envSet) (livekitService, error) {
	return resolveLiveKitService(targetcap.Reason, binding, "", env)
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
	extras := map[string]bool{}
	packages := map[string]bool{}
	for _, svc := range livekitServices(data) {
		if svc.Entry.Install.Extra != "" {
			extras[svc.Entry.Install.Extra] = true
		}
		if svc.Entry.Install.Package != "" {
			packages[svc.Entry.Install.Package+svc.Entry.Install.Constraint] = true
		}
	}
	base := fmt.Sprintf("livekit-agents>=%d.%d", livekitVersionMajor, livekitVersionMinMinor)
	if len(extras) > 0 {
		base = fmt.Sprintf("livekit-agents[%s]>=%d.%d",
			strings.Join(sortedKeys(extras), ","), livekitVersionMajor, livekitVersionMinMinor)
	}
	deps := append([]string{base, "livekit-plugins-silero>=1.6.1", "python-dotenv"}, sortedKeys(packages)...)
	if data.NeedsHTTPX {
		deps = append(deps, "httpx")
	}
	sort.Strings(deps)
	return deps
}
