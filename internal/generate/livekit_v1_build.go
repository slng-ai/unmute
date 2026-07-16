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
	// LiveKit Cloud creds run the worker + Inference (reason/turn). SLNG and any
	// native per-vendor plugin add their own api-key env as bindings are lowered.
	for _, e := range []string{"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"} {
		env.add(e)
	}
	// silero (VAD) is always emitted; other plugin modules/deps/env accrue as
	// listen/speak/reason bindings are lowered through the catalogue (C8/V11/V13).
	emit := &lkEmit{
		env:     env,
		modules: map[string]bool{"silero": true},
		deps:    map[string]string{"livekit-plugins-silero": "1.6.1"},
		pins:    tgt.Pins,
	}

	data := livekitData{
		Project:     tgt.Name,
		Version:     tgt.Version,
		AgentName:   tgt.Name,
		EntryClass:  pyName(agent.EntryAgent),
		TurnVersion: "v1",
	}

	entry := agent.Agents[agent.EntryAgent]
	stt, err := emit.stt(tgt.Models.Listen)
	if err != nil {
		return livekitData{}, err
	}
	data.STT = stt
	data.SessionLLM, err = emit.llm(tgt.Models.Reason[entry.Model])
	if err != nil {
		return livekitData{}, fmt.Errorf("entry agent %q: %w", agent.EntryAgent, err)
	}
	data.SessionTTS, err = emit.tts(tgt.Models.Speak[entry.Voice])
	if err != nil {
		return livekitData{}, fmt.Errorf("entry agent %q: %w", agent.EntryAgent, err)
	}

	for _, name := range sortedAgentNames(agent) {
		built, err := buildLiveKitAgent(agent, tgt, name, agent.Agents[name], entry, emit)
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

	data.PluginModules = sortedKeys(emit.modules)
	data.Deps = livekitDeps(emit.deps, data.NeedsHTTPX)
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

func buildLiveKitAgent(agent *ir.Agent, tgt ir.Target, name string, def, entry ir.AgentDef, emit *lkEmit) (livekitAgent, error) {
	built := livekitAgent{
		Name: name, Class: pyName(name), PromptConst: promptConst(name),
		IsEntry: name == agent.EntryAgent,
	}
	if def.Model != entry.Model {
		llm, err := emit.llm(tgt.Models.Reason[def.Model])
		if err != nil {
			return livekitAgent{}, fmt.Errorf("agent %q: %w", name, err)
		}
		built.LLM = &llm
	}
	if def.Voice != entry.Voice {
		tts, err := emit.tts(tgt.Models.Speak[def.Voice])
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

// --- provider → plugin catalogue + call mapping ----------------------------
//
// listen/speak/reason lower to Python constructor expressions. `slng` is the
// default Execution-Layer route (slng.STT/slng.TTS); any other listen/speak
// provider names a native livekit.plugins.<vendor> plugin; reason defaults to
// LiveKit Inference (inference.LLM("provider/model")) and lowers to a native
// <vendor>.LLM when the provider is catalogued (C8/V11/V13, amended 2026-07-16).
// Per-provider facts verified against docs.livekit.io/agents/models/{stt,tts,llm}/*
// on 2026-07-16.

// slngRoute strips the Execution-Layer `slng/` prefix: the livekit-plugins-slng
// plugin takes the bare provider/model form (verified: model="deepgram/aura:2").
func slngRoute(model string) string { return strings.TrimPrefix(model, "slng/") }

// lkPlugin is one native-plugin catalogue entry. Zero-value fields normalise
// (norm): Callable defaults to Module.<class>, api-key env to <MODULE>_API_KEY,
// model kwarg to `model`, version floor to 1.5. The pip package is always
// livekit-plugins-<Module>.
type lkPlugin struct {
	Module     string   // livekit.plugins submodule to import
	Callable   string   // constructor callable ("" → Module + "." + class)
	Env        []string // api-key env var(s) the plugin auto-reads ("" → <MODULE>_API_KEY)
	ModelKwarg string   // model kwarg name ("" → "model"); ignored when NoModel
	VoiceKwarg string   // TTS voice kwarg ("" → no voice arg)
	OptionsCls string   // nested-options class (soniox): call(params=OptionsCls(...))
	MinVersion string   // dep floor ("" → "1.5")
	NoModel    bool     // provider takes no model arg (speechmatics)
	Compat     bool     // openai-compatible: add base_url from the binding endpoint_env
}

func (p lkPlugin) norm(class string) lkPlugin {
	if p.Callable == "" {
		p.Callable = p.Module + "." + class
	}
	if len(p.Env) == 0 {
		p.Env = []string{strings.ToUpper(p.Module) + "_API_KEY"}
	}
	if p.ModelKwarg == "" {
		p.ModelKwarg = "model"
	}
	if p.MinVersion == "" {
		p.MinVersion = "1.5"
	}
	return p
}

// call renders the constructor. Native plugins auto-read their api key from the
// environment (load_dotenv), so no api_key= is passed; model/voice ride the
// binding, params are forwarded verbatim (D10).
func (p lkPlugin) call(model, voice string, params map[string]any, endpointEnv string) string {
	if p.OptionsCls != "" { // soniox: params=soniox.STTOptions(model=..., <params>)
		var inner []pyKV
		if !p.NoModel && model != "" {
			inner = append(inner, pyKV{Key: p.ModelKwarg, Value: pyQuote(model)})
		}
		inner = append(inner, forwardParams(params)...)
		return pyCall(p.Callable, []pyKV{{Key: "params", Value: pyCall(p.OptionsCls, inner)}})
	}
	var args []pyKV
	if !p.NoModel && model != "" {
		args = append(args, pyKV{Key: p.ModelKwarg, Value: pyQuote(model)})
	}
	if p.VoiceKwarg != "" && voice != "" {
		args = append(args, pyKV{Key: p.VoiceKwarg, Value: pyQuote(voice)})
	}
	if p.Compat && endpointEnv != "" {
		args = append(args, pyKV{Key: "base_url", Value: pyEnv(endpointEnv)})
	}
	args = append(args, forwardParams(params)...)
	return pyCall(p.Callable, args)
}

// The native catalogues. `slng` is handled outside these tables (the default
// Execution-Layer route); a reason provider absent from livekitLLMPlugins lowers
// to LiveKit Inference. Coverage-tested by TestLiveKitPluginCatalogCoverage (V13).
var livekitSTTPlugins = map[string]lkPlugin{
	"assemblyai":   {Module: "assemblyai", MinVersion: "1.6"},
	"cartesia":     {Module: "cartesia"},
	"deepgram":     {Module: "deepgram"},
	"elevenlabs":   {Module: "elevenlabs", Env: []string{"ELEVEN_API_KEY"}, ModelKwarg: "model_id"},
	"gradium":      {Module: "gradium", ModelKwarg: "model_name"},
	"sarvam":       {Module: "sarvam"},
	"soniox":       {Module: "soniox", OptionsCls: "soniox.STTOptions"},
	"speechmatics": {Module: "speechmatics", NoModel: true},
}

var livekitTTSPlugins = map[string]lkPlugin{
	"cartesia":     {Module: "cartesia", VoiceKwarg: "voice"},
	"deepgram":     {Module: "deepgram"}, // voice is baked into the aura model id
	"elevenlabs":   {Module: "elevenlabs", Env: []string{"ELEVEN_API_KEY"}, VoiceKwarg: "voice_id"},
	"gemini":       {Module: "google", Callable: "google.beta.GeminiTTS", VoiceKwarg: "voice_name"},
	"google":       {Module: "google", Callable: "google.beta.GeminiTTS", VoiceKwarg: "voice_name"},
	"gradium":      {Module: "gradium", ModelKwarg: "model_name", VoiceKwarg: "voice_id"},
	"inworld":      {Module: "inworld", VoiceKwarg: "voice"},
	"rime":         {Module: "rime", VoiceKwarg: "speaker"},
	"sarvam":       {Module: "sarvam", VoiceKwarg: "speaker"},
	"soniox":       {Module: "soniox", VoiceKwarg: "voice"},
	"speechmatics": {Module: "speechmatics", NoModel: true, VoiceKwarg: "voice"},
}

var livekitLLMPlugins = map[string]lkPlugin{
	"anthropic":         {Module: "anthropic"},
	"aws":               {Module: "aws", Env: []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}},
	"azure":             {Module: "openai", Callable: "openai.LLM.with_azure", Env: []string{"AZURE_OPENAI_API_KEY"}},
	"groq":              {Module: "groq"},
	"mistralai":         {Module: "mistralai", Env: []string{"MISTRAL_API_KEY"}},
	"openai-compatible": {Module: "openai", Callable: "openai.LLM", Env: []string{"OPENAI_API_KEY"}, Compat: true},
	"openrouter":        {Module: "openai", Callable: "openai.LLM.with_openrouter", Env: []string{"OPENROUTER_API_KEY"}},
	"sarvam":            {Module: "sarvam"},
}

// lkEmit accumulates the plugin modules to import, their pip deps (pinned via
// Target.Pins, else the catalogue floor), and api-key env vars, as bindings lower.
type lkEmit struct {
	env     *envSet
	modules map[string]bool   // livekit.plugins submodules to import
	deps    map[string]string // pip package → resolved version number
	pins    map[string]string // Target.Pins (package → version)
}

func (e *lkEmit) use(module, minVersion string, envs []string) {
	e.modules[module] = true
	pkg := "livekit-plugins-" + module
	version := minVersion
	if v, ok := e.pins[pkg]; ok && v != "" {
		version = v
	}
	e.deps[pkg] = version
	for _, name := range envs {
		e.env.add(name)
	}
}

func (e *lkEmit) stt(binding *ir.Binding) (livekitSTT, error) {
	if binding == nil {
		return livekitSTT{}, fmt.Errorf("livekit listen binding is missing")
	}
	if binding.Provider == "slng" {
		if binding.Model == "" {
			return livekitSTT{}, fmt.Errorf("livekit listen binding is missing a model")
		}
		e.use("slng", "1.6.1", []string{"SLNG_API_KEY"})
		args := append([]pyKV{{Key: "api_key", Value: pyEnv("SLNG_API_KEY")}, {Key: "model", Value: pyQuote(slngRoute(binding.Model))}}, forwardParams(binding.Params)...)
		return livekitSTT{Ctor: pyCall("slng.STT", args)}, nil
	}
	p, ok := livekitSTTPlugins[binding.Provider]
	if !ok {
		return livekitSTT{}, fmt.Errorf("livekit listen provider %q is not supported (expected slng or one of: %s)", binding.Provider, sortedPluginKeys(livekitSTTPlugins))
	}
	if binding.Model == "" && !p.NoModel {
		return livekitSTT{}, fmt.Errorf("livekit listen binding is missing a model")
	}
	p = p.norm("STT")
	e.use(p.Module, p.MinVersion, p.Env)
	return livekitSTT{Ctor: p.call(binding.Model, "", binding.Params, binding.EndpointEnv)}, nil
}

func (e *lkEmit) tts(binding ir.Binding) (livekitTTS, error) {
	if binding.Provider == "slng" {
		if binding.Model == "" {
			return livekitTTS{}, fmt.Errorf("livekit speak binding is missing a model")
		}
		e.use("slng", "1.6.1", []string{"SLNG_API_KEY"})
		args := append([]pyKV{{Key: "api_key", Value: pyEnv("SLNG_API_KEY")}, {Key: "model", Value: pyQuote(slngRoute(binding.Model))}, {Key: "voice", Value: pyQuote(firstNonEmpty(binding.Voice, binding.VoiceID))}}, forwardParams(binding.Params)...)
		return livekitTTS{Ctor: pyCall("slng.TTS", args)}, nil
	}
	p, ok := livekitTTSPlugins[binding.Provider]
	if !ok {
		return livekitTTS{}, fmt.Errorf("livekit speak provider %q is not supported (expected slng or one of: %s)", binding.Provider, sortedPluginKeys(livekitTTSPlugins))
	}
	if binding.Model == "" && !p.NoModel {
		return livekitTTS{}, fmt.Errorf("livekit speak binding is missing a model")
	}
	p = p.norm("TTS")
	e.use(p.Module, p.MinVersion, p.Env)
	return livekitTTS{Ctor: p.call(binding.Model, firstNonEmpty(binding.Voice, binding.VoiceID), binding.Params, binding.EndpointEnv)}, nil
}

func (e *lkEmit) llm(binding ir.Binding) (livekitLLM, error) {
	if binding.Model == "" {
		return livekitLLM{}, fmt.Errorf("reason binding is missing a model")
	}
	if p, ok := livekitLLMPlugins[binding.Provider]; ok {
		p = p.norm("LLM")
		e.use(p.Module, p.MinVersion, p.Env)
		return livekitLLM{Ctor: p.call(binding.Model, "", binding.Params, binding.EndpointEnv)}, nil
	}
	// default: LiveKit Inference — model is provider/model, params ride extra_kwargs (C8).
	model := binding.Model
	if binding.Provider != "" {
		model = binding.Provider + "/" + binding.Model
	}
	args := []pyKV{{Key: "model", Value: pyQuote(model)}}
	if len(binding.Params) > 0 {
		args = append(args, pyKV{Key: "extra_kwargs", Value: pyDict(binding.Params)})
	}
	return livekitLLM{Ctor: pyCall("inference.LLM", args)}, nil
}

// --- Python expression helpers ---------------------------------------------

func pyCall(callable string, args []pyKV) string {
	var b strings.Builder
	b.WriteString(callable)
	b.WriteByte('(')
	for i, a := range args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(a.Key)
		b.WriteByte('=')
		b.WriteString(a.Value)
	}
	b.WriteByte(')')
	return b.String()
}

func pyEnv(name string) string { return "os.environ.get(" + pyQuote(name) + ")" }

// pyDict renders a params map as a Python dict literal with sorted keys, for the
// inference.LLM extra_kwargs bag.
func pyDict(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, pyQuote(k)+": "+pyLiteral(m[k]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func sortedPluginKeys(m map[string]lkPlugin) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
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

// livekitDeps builds the pyproject dependency list: livekit-agents + dotenv
// always, plus each used plugin (livekit-plugins-<module>) at its resolved
// version, plus httpx when a webhook tool is emitted. Sorted for a stable file.
func livekitDeps(plugins map[string]string, needsHTTPX bool) []string {
	deps := []string{
		fmt.Sprintf("livekit-agents>=%d.%d", livekitVersionMajor, livekitVersionMinMinor),
		"python-dotenv",
	}
	for pkg, version := range plugins {
		deps = append(deps, pkg+">="+version)
	}
	if needsHTTPX {
		deps = append(deps, "httpx")
	}
	sort.Strings(deps)
	return deps
}
