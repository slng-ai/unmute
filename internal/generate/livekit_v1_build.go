package generate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/slng/unmute/internal/ir"
)

// buildLiveKitData lowers the resolved IR + target into the template model.
// listen/speak MUST route through SLNG (V11); reason lowers to LiveKit Inference.
// Features the driver does not emit yet fail loud here rather than emitting
// broken code (no validate-green / generate-broken drift, compiler V19 in spirit).
func buildLiveKitData(agent *ir.Agent, tgt ir.Target) (livekitData, error) {
	if err := livekitGuards(agent); err != nil {
		return livekitData{}, err
	}
	env := newEnvSet()
	// LiveKit Cloud creds run the worker + Inference (reason/turn); SLNG_API_KEY
	// runs the Execution Layer STT/TTS.
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
	stt, err := livekitSlngSTT(tgt.Models.Listen, env)
	if err != nil {
		return livekitData{}, err
	}
	data.STT = stt
	data.SessionLLM, err = livekitInferenceLLM(tgt.Models.Reason[entry.Model])
	if err != nil {
		return livekitData{}, fmt.Errorf("entry agent %q: %w", agent.EntryAgent, err)
	}
	data.SessionTTS, err = livekitSlngTTS(tgt.Models.Speak[entry.Voice], env)
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

	data.Deps = livekitDeps(data)
	data.RequiredEnv = env.sorted()
	return data, nil
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
		llm, err := livekitInferenceLLM(tgt.Models.Reason[def.Model])
		if err != nil {
			return livekitAgent{}, fmt.Errorf("agent %q: %w", name, err)
		}
		built.LLM = &llm
	}
	if def.Voice != entry.Voice {
		tts, err := livekitSlngTTS(tgt.Models.Speak[def.Voice], env)
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
	delegate := livekitDelegate{
		Method: ref, When: delegateWhen(c),
		Cue:              "This flow is complete. Move to the closing and ask if there is anything else.",
		SummarizeChatCtx: group.Merge != ir.GroupMergeResults,
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

// slngRoute strips the Execution-Layer `slng/` prefix: the livekit-plugins-slng
// plugin takes the bare provider/model form (verified: model="deepgram/aura:2").
func slngRoute(model string) string { return strings.TrimPrefix(model, "slng/") }

func livekitSlngSTT(binding *ir.Binding, env *envSet) (livekitSTT, error) {
	if binding == nil || binding.Model == "" {
		return livekitSTT{}, fmt.Errorf("livekit listen binding is missing a model")
	}
	if binding.Provider != "slng" {
		return livekitSTT{}, fmt.Errorf("livekit routes listen through SLNG only; bind provider: slng (got %q)", binding.Provider)
	}
	key := apiKeyEnv("slng")
	env.add(key)
	return livekitSTT{Model: slngRoute(binding.Model), APIKeyEnv: key, Params: forwardParams(binding.Params)}, nil
}

func livekitSlngTTS(binding ir.Binding, env *envSet) (livekitTTS, error) {
	if binding.Model == "" {
		return livekitTTS{}, fmt.Errorf("livekit speak binding is missing a model")
	}
	if binding.Provider != "slng" {
		return livekitTTS{}, fmt.Errorf("livekit routes speak through SLNG only; bind provider: slng (got %q)", binding.Provider)
	}
	key := apiKeyEnv("slng")
	env.add(key)
	return livekitTTS{
		Model: slngRoute(binding.Model), Voice: firstNonEmpty(binding.Voice, binding.VoiceID),
		APIKeyEnv: key, Params: forwardParams(binding.Params),
	}, nil
}

func livekitInferenceLLM(binding ir.Binding) (livekitLLM, error) {
	if binding.Model == "" {
		return livekitLLM{}, fmt.Errorf("reason binding is missing a model")
	}
	model := binding.Model
	if binding.Provider != "" {
		model = binding.Provider + "/" + binding.Model
	}
	return livekitLLM{Model: model, Params: forwardParams(binding.Params)}, nil
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

func livekitDeps(data livekitData) []string {
	deps := []string{
		fmt.Sprintf("livekit-agents>=%d.%d", livekitVersionMajor, livekitVersionMinMinor),
		"livekit-plugins-silero>=1.6.1",
		"livekit-plugins-slng>=1.6.1",
		"python-dotenv",
	}
	if data.NeedsHTTPX {
		deps = append(deps, "httpx")
	}
	sort.Strings(deps)
	return deps
}
