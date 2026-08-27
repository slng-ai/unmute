package generate

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/slng-ai/unmute/internal/devmetrics"
	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

// buildLiveKitData lowers the resolved IR + target into the template model.
// listen/speak/reason resolve through the provider catalogue (SLNG default,
// any entry binds); a reason vendor without a native entry falls to the
// LiveKit Inference wildcard row (C8/V19).
func buildLiveKitData(agent *ir.Agent, tgt ir.Target) (livekitData, error) {
	// The driver's templates are Python; a node project would be silently
	// wrong, so fail loud until node templates exist (C1). ir.Validate asks the
	// same question now, so this is the backstop rather than the only guard.
	if err := targetcap.CheckSDKLanguage(targetcap.LiveKit, tgt.SDKLanguage); err != nil {
		return livekitData{}, err
	}
	if err := checkLiveKitPins(tgt.Pins); err != nil {
		return livekitData{}, err
	}
	env := newEnvSet()
	platformEnv := []string{"LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"}
	// LiveKit Cloud creds run the worker against a real room (dev/start) and
	// any Inference-routed role; console mode needs only the bound providers'
	// keys (B5/B6). SLNG and any native per-vendor plugin add their own
	// api-key env as bindings are lowered.
	for _, e := range platformEnv {
		env.add(e)
	}
	turnVersion, err := livekitTurnVersion(tgt.Models.Turn)
	if err != nil {
		return livekitData{}, err
	}
	data := livekitData{
		Project:           tgt.Name,
		Version:           tgt.Version,
		DeploymentRegions: tgt.DeploymentRegions,
		Deploys:           livekitDeploys(tgt.DeploymentRegions),
		AgentName:         tgt.Name,
		EntryAgent:        agent.EntryAgent,
		EntryClass:        pyName(agent.EntryAgent),
		TurnVersion:       turnVersion,
		Pace:              resolvePaceView(targetcap.LiveKit, tgt.Models.Turn),
		SemanticOff:       semanticEndpointingOff(tgt.Models.Turn),
		Pins:              tgt.Pins,
		Tracing:           agent.Tracing != nil,
		TracingProvider:   tracingProviderOf(agent),
	}
	if tgt.Telephony != nil {
		data.CarrierSteps = slices.Clone(tgt.Telephony.ManualSteps)
	}
	for _, name := range tracingEnv(data.TracingProvider) {
		env.add(name)
	}

	entry := agent.Agents[agent.EntryAgent]
	stt, err := livekitSTTService(tgt.Models.Listen, env)
	if err != nil {
		return livekitData{}, err
	}
	data.STT = livekitChain{Primary: stt}
	// The selected listen model's fallback chain lowers to stt.FallbackAdapter
	// (T16); each entry resolves through the same catalogue path as the primary.
	for _, fallback := range tgt.Models.ListenFallbacks {
		binding := fallback.Binding
		svc, err := livekitSTTService(&binding, env)
		if err != nil {
			return livekitData{}, fmt.Errorf("listen fallback %q: %w", fallback.Name, err)
		}
		data.STT.Chain = append(data.STT.Chain, svc)
	}
	data.SessionLLM, err = livekitReasonLLM(agent, tgt, entry.Model, env)
	if err != nil {
		return livekitData{}, fmt.Errorf("entry agent %q: %w", agent.EntryAgent, err)
	}
	data.SessionTTS, err = livekitTTSService(tgt.Models.Speak[entry.Voice], env)
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
	data.Unserved = livekitArg{
		Name: ir.UnservedResultField, PyType: "str",
		Desc: ir.UnservedResultDescription,
		Anno: pyAnno("str", nil, ir.UnservedResultDescription),
	}
	for _, task := range data.Tasks {
		data.HasTaskTransfers = data.HasTaskTransfers || len(task.Transfers) > 0
	}
	data.NeedsFunctionTools = data.NeedsTasks
	for _, a := range data.Agents {
		if len(a.Tools) > 0 || len(a.Transfers) > 0 || len(a.HumanTransfers) > 0 || len(a.Delegates) > 0 {
			data.NeedsFunctionTools = true
			break
		}
	}
	for _, a := range data.Agents {
		if len(a.Prebuilt) > 0 {
			data.NeedsEndCallTool = true
		}
	}
	for _, tk := range data.Tasks {
		if len(tk.Prebuilt) > 0 {
			data.NeedsEndCallTool = true
		}
	}

	// V2: tool and finish args that carry an enum need typing.Literal; args that
	// carry a description need typing.Annotated + pydantic.Field. Import only what
	// is used (no unused-import F-violation, dl§V26).
	needLiteral, needAnnotated := false, false
	scanArgs := func(args []livekitArg) {
		for _, a := range args {
			needLiteral = needLiteral || len(a.Enum) > 0
			needAnnotated = needAnnotated || a.Desc != ""
		}
	}
	for _, a := range data.Agents {
		for _, tool := range a.Tools {
			scanArgs(tool.Args)
		}
	}
	for _, t := range data.Tasks {
		for _, tool := range t.Tools {
			scanArgs(tool.Args)
		}
		scanArgs(t.Result)
	}
	if data.NeedsTasks {
		// Every task's finish takes the reserved arg, and it carries a
		// description, so a package whose own args carry none still imports both.
		scanArgs([]livekitArg{data.Unserved})
	}
	data.NeedsField = needAnnotated
	var typingNames []string
	if needAnnotated {
		typingNames = append(typingNames, "Annotated")
	}
	if needLiteral {
		typingNames = append(typingNames, "Literal")
	}
	data.TypingImports = strings.Join(typingNames, ", ")

	// Local handler files ride the artifact (tools/<name>.py); mcp mounts and
	// local wrappers pull their imports.
	seenLocal := map[string]bool{}
	seenMCP := map[string]bool{}
	collectTools := func(tools []livekitTool, servers []livekitMCPServer) {
		data.NeedsMCP = data.NeedsMCP || len(servers) > 0
		for _, server := range servers {
			if !seenMCP[server.Name] {
				seenMCP[server.Name] = true
				data.MCPServers = append(data.MCPServers, server)
			}
			if server.Auth != nil {
				data.AuthKinds.add(server.Auth.Kind) // the same helper webhook auth uses (V8)
			}
		}
		for _, tool := range tools {
			if tool.URLEnv != "" {
				data.NeedsHTTPX = true // webhook tool POSTs with httpx (agents + tasks own them)
			}
			if tool.Auth != nil {
				data.AuthKinds.add(tool.Auth.Kind) // one helper per scheme in use (V8)
			}
			if tool.Announce != "" {
				data.HasToolAnnouncements = true
			}
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
	for _, a := range data.Agents {
		for _, tool := range a.Tools {
			if len(tool.Needed) > 0 {
				data.NeedsRefusal = true
			}
		}
	}
	for _, t := range data.Tasks {
		for _, tool := range t.Tools {
			if len(tool.Needed) > 0 {
				data.NeedsRefusal = true
			}
		}
		for _, transfer := range t.Transfers {
			data.NeedsLastN = data.NeedsLastN || strings.HasPrefix(transfer.CtxExpr, "_last_n(")
			data.NeedsSummarize = data.NeedsSummarize || transfer.Summary != nil
		}
	}

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

	if data.NeedsSummarize {
		suffix, err := summaryPromptSuffix(agent)
		if err != nil {
			return livekitData{}, err
		}
		data.SummaryPromptSuffix = suffix
	}

	// F3: a lone agent that is never a handoff target needs no chat_ctx plumbing
	// (the canonical single-agent shape is Agent(instructions=...)). Drop the
	// ctor param plus the NOT_GIVEN/NotGivenOr imports that only feed it. The llm
	// module import survives only if something still references it (a fallback
	// chain or a history helper); otherwise it too would be an unused import
	// (dl§V26). Multi-agent output is unchanged.
	data.SingleAgentMinimal = len(data.Agents) == 1 && !data.NeedsTasks &&
		len(data.Agents[0].Transfers) == 0 && len(data.Agents[0].HumanTransfers) == 0 &&
		len(data.Agents[0].Delegates) == 0
	anyChain := len(data.SessionLLM.Chain) > 0
	for _, a := range data.Agents {
		anyChain = anyChain || (a.LLM != nil && len(a.LLM.Chain) > 0)
	}
	for _, t := range data.Tasks {
		anyChain = anyChain || (t.LLM != nil && len(t.LLM.Chain) > 0)
	}
	data.NeedsLLM = !data.SingleAgentMinimal || anyChain || data.NeedsLastN || data.NeedsSummarize

	// Typed shared state (SCHEMA 4.4): variables lower to a Userdata dataclass
	// on the session; `assign` and `requires` read and write its fields.
	for _, name := range sortedVarNames(agent) {
		v := agent.Variables[name]
		def := "None"
		if v.Default != nil {
			def = pyLiteral(v.Default)
		}
		data.Vars = append(data.Vars, livekitVar{Name: name, PyType: pyType(v.Type), Default: def, Description: v.Description})
		if v.Source == ir.VariableSourceCallStart || v.Source == "" {
			data.CallStartVars = append(data.CallStartVars, livekitCallStartVar{
				Name: name, Type: string(v.Type), TypeCheck: livekitTypeCheck(v.Type),
				Required: v.Default == nil && v.Source == ir.VariableSourceCallStart,
			})
		}
	}
	data.HasVars = len(data.Vars) > 0
	data.Capture = buildLiveKitCapture(agent)
	// A bare name in a compose environment block is forwarded when the host sets
	// it and absent otherwise, which is exactly what the dev loop's measurement
	// switch needs: the worker runs in a container, so inheriting the parent's
	// environment is not enough, and a deployed artifact still declares nothing.
	data.HandoffControls = handoffControls(agent)
	data.DevOptionalEnv = []string{devmetrics.Env}
	if len(data.CallStartVars) > 0 {
		data.DevOptionalEnv = append(data.DevOptionalEnv, "UNMUTE_CALL_START")
	}
	slices.Sort(data.DevOptionalEnv)
	// Every mcp source's env names join the startup check as well. Declared
	// secrets cover most packages, but the address and token a tool source
	// names are required whether or not the package also declared them, and a
	// missing one has to be named before anything dials (FR-009/N40).
	for _, name := range appendMCPEnv(requiredSecretEnv(agent, tgt, env), data.Agents, data.Tasks) {
		env.add(name)
	}
	data.NeedsRender = renderNeeds(agent)
	data.PrerequisiteGuard, data.NeedsPrerequisiteGuard = PrerequisiteGuard(agent)
	if data.Capture != nil {
		data.NeedsFunctionTools = true // the generated capture tool is a @function_tool too
	}

	// Prompt constants, ordered agents-then-tasks for a stable file.
	for _, a := range data.Agents {
		data.Prompts = append(data.Prompts, livekitPrompt{Const: a.PromptConst, Body: agent.Agents[a.Name].Instructions})
	}
	for _, t := range data.Tasks {
		data.Prompts = append(data.Prompts, livekitPrompt{Const: t.PromptConst, Body: livekitTaskPrompt(agent.Tasks[t.Name], t.Result)})
	}
	entryInstructions := agent.Agents[agent.EntryAgent].Instructions
	if _, router := slngRouterBinding(agent, tgt, agent.Agents[agent.EntryAgent].Model); ir.HasTemplate(entryInstructions) && !router {
		data.EntryPromptExpr = promptExpr(promptConst(agent.EntryAgent), entryInstructions, "session.userdata", false)
	}

	applyLiveKitConversation(agent.Conversation, &data)

	// Telephony: outbound + AMD voicemail (V8/N6); cold/warm transfer imports.
	channelNames := make([]string, 0, len(agent.Channels))
	for name := range agent.Channels {
		channelNames = append(channelNames, name)
	}
	slices.Sort(channelNames)
	for _, name := range channelNames {
		ch := agent.Channels[name]
		// The connector route places outbound calls in the bridge (Twilio call +
		// room join), so the agent never runs the SIP dial-out/AMD flow. Only SIP
		// (and non-telephony targets) drive data.Outbound.
		if ch.Kind == ir.ChannelTelephony && ch.Outbound != nil && *ch.Outbound && tgt.Transport != "connector" {
			data.Outbound = &livekitOutbound{LeaveMessage: ch.OnVoicemail == ir.VoicemailLeaveMessage}
			break
		}
	}
	for _, a := range data.Agents {
		for _, ht := range a.HumanTransfers {
			data.HasColdTransfer = data.HasColdTransfer || !ht.Warm
			data.HasWarmTransfer = data.HasWarmTransfer || ht.Warm
		}
	}
	data.Telephony, err = buildLiveKitTelephony(agent, tgt, env)
	if err != nil {
		return livekitData{}, err
	}
	// Warm transfer dials from any session, including WebRTC, through the
	// selected SIP connection. Cold transfer acts only on an existing phone leg.
	if data.HasWarmTransfer && tgt.Telephony != nil {
		for _, name := range tgt.Telephony.Environment {
			env.addRead(name)
		}
	}
	// The web dev image needs every always-read value, but not route values or a
	// cold destination read only by a phone call.
	data.DevEnv = withoutRouteEnv(env.sorted(), agent, tgt, env)
	if data.Telephony != nil {
		if agent.Capacity == nil || agent.Capacity.MaxSessions <= 0 {
			return livekitData{}, fmt.Errorf("livekit telephony requires positive capacity.max_sessions")
		}
		data.MaxSessions = agent.Capacity.MaxSessions
		data.DrainTimeoutSecs = data.MaxDurationSecs
		if data.DrainTimeoutSecs <= 0 {
			data.DrainTimeoutSecs = 1800
		}
		for i := range data.Agents {
			if !data.Agents[i].IsEntry {
				continue
			}
			data.Telephony.Greeting = data.Agents[i].Greeting
			data.Agents[i].Greeting = &livekitGreeting{Silent: true}
			break
		}
	}

	data.Notes = append(data.Notes, livekitServiceNotes(data)...)
	if data.HasWarmTransfer {
		data.Notes = append(data.Notes, "human_transfer warm uses livekit-agents beta.workflows on Python (Beta)")
	}
	if tgt.Models.Turn != nil {
		data.Notes = append(data.Notes, "turn role lowers to LiveKit Inference turn detection; its binding placement is advisory")
		data.Notes = append(data.Notes, data.Pace.note())
	}

	for _, svc := range livekitServices(data) {
		if svc.Call.Class != "openai.responses.LLM" {
			continue
		}
		data.OpenAIResponses = true
		for _, arg := range svc.Call.Args {
			data.NeedsOpenAIReasoning = data.NeedsOpenAIReasoning || arg.Key == "reasoning" && strings.HasPrefix(arg.Value, "openai_types.Reasoning(")
		}
	}
	data.PluginModules = collectLiveKitPlugins(data)
	slng, err := slngHelpersFor(agent, tgt)
	if err != nil {
		return livekitData{}, err
	}
	data.Slng = slng
	// The body this target sends with every request, rendered from the entry
	// agent's site because validation holds every router profile in a livekit
	// package to being that one. Empty when the package has no router binding,
	// which is what keeps a non-router package byte-identical.
	if profile, router := slngRouterBinding(agent, tgt, agent.Agents[agent.EntryAgent].Model); router {
		binding := tgt.Models.Reason[profile]
		site := livekitSlngSite(agent, tgt, profile)
		data.Slng.RequestBody = pyLiteral(slngRequestBody(site, binding))
		// This target builds its own client, because attaching a response hook is
		// the only way to see the router's provenance headers and the plugin gives
		// no other seam. So the two values it would have passed to the client it
		// builds itself come here instead.
		if url, ok := targetcap.SlngRouterBaseURL(slngRegion(binding)); ok {
			data.Slng.ClientBaseURL = url
			data.Slng.ClientKeyEnv = targetcap.SlngRouterKeyEnv
			// That client is built with httpx, so the import is needed whether or
			// not this package has a webhook tool.
			data.NeedsHTTPX = true
		}
	}
	// A router package gets a Userdata object whether or not it declares
	// variables: the per-call session id lives there now, and a class reaching
	// it from a method body is the only way the header set can travel per
	// request.
	data.HasUserdata = data.HasVars || slng.Any()
	// The emitted mixin names llm.LLM to tell a per-class model override from
	// the session default, the way the framework's own activity does.
	data.NeedsLLM = data.NeedsLLM || slng.Any()
	knowledge, err := loweredKnowledge(agent, env)
	if err != nil {
		return livekitData{}, err
	}
	data.Knowledge = knowledge
	data.Deps = livekitDeps(data)
	data.RequiredEnv = env.sorted()
	// The startup check is derived from what the compiler knows it requires, not
	// from what the author remembered to declare. It used to read `secrets:`
	// alone, so a package with no block emitted no REQUIRED_ENV and no
	// require_env() at all, and a name left undeclared dropped out of the very
	// check meant to catch it — while docs-site/reference/secrets.mdx promised
	// "the generated agent refuses to start without them" (research D3).
	//
	// All names reach the environment, but the browser check skips values read
	// only by a phone call. Values a warm transfer reads from WebRTC stay because
	// envSet records them as always-read. The full set remains in .env.example,
	// the compile report, and the runbook. This is the same derivation the
	// Pipecat driver already uses for DevEnv.
	data.RequiredSecrets = withoutRouteEnv(data.RequiredEnv, agent, tgt, env)
	data.CallRequiredEnv = humanTransferEnv(agent, tgt, env)
	supplied := platformEnv
	if tgt.Telephony != nil {
		supplied = append(slices.Clone(supplied), tgt.Telephony.LocalEnvironment...)
		data.SuppliedForYou = slices.Clone(tgt.Telephony.LocalEnvironment)
	}
	data.AuthorEnv = authorEnv(data.RequiredEnv, supplied)
	return data, nil
}

// buildLiveKitCapture builds the generated update_variables tool: one optional
// argument per conversation variable, writing the session userdata (V6).
func buildLiveKitCapture(agent *ir.Agent) *livekitCapture {
	fields := captureFields(agent)
	if len(fields) == 0 {
		return nil
	}
	capture := &livekitCapture{
		Name: ir.CaptureToolName, Description: captureDescription(agent, fields), Fields: fields,
	}
	for _, name := range fields {
		variable := agent.Variables[name]
		// Every field is optional: the model saves what it has learned so far,
		// one call or several, never all of them at once.
		anno := pyType(variable.Type) + " | None"
		capture.Args = append(capture.Args, livekitArg{
			Name: name, PyType: pyType(variable.Type), Desc: variable.Description, Anno: anno,
		})
	}
	return capture
}

func buildLiveKitTelephony(agent *ir.Agent, tgt ir.Target, env *envSet) (*livekitTelephony, error) {
	plan := tgt.Telephony
	if plan == nil {
		return nil, nil
	}
	if plan.Key.Provider != ir.ProviderLiveKit {
		return nil, fmt.Errorf("livekit telephony route (%s, %s, %s) has no emitted adapter", plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier)
	}
	switch plan.Key.Transport {
	case "sip":
		return buildLiveKitSIPTelephony(agent, tgt, env)
	case "connector":
		return buildLiveKitConnectorTelephony(agent, tgt, env)
	default:
		return nil, fmt.Errorf("livekit telephony route (%s, %s, %s) has no emitted adapter", plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier)
	}
}

// fillLiveKitTelephonyCommon populates the direction, call_start, and system
// source facts shared by every LiveKit telephony transport.
func fillLiveKitTelephonyCommon(telephony *livekitTelephony, agent *ir.Agent, plan *ir.TelephonyPlan) {
	for _, evidence := range plan.Evidence {
		switch evidence.Feature {
		case "inbound":
			telephony.HasInbound = true
		case "outbound":
			telephony.HasOutbound = true
		case "warm_transfer":
			telephony.HasWarm = true
		}
	}
	for _, variable := range sortedVarNames(agent) {
		def := agent.Variables[variable]
		if def.Source == ir.VariableSourceCallStart {
			telephony.CallStart = append(telephony.CallStart, livekitCallStart{
				Name: variable, Type: string(def.Type), TypeCheck: livekitTypeCheck(def.Type), Required: def.Default == nil,
			})
		}
	}
	sourceVariables := make([]string, 0, len(plan.SystemSources))
	for variable := range plan.SystemSources {
		sourceVariables = append(sourceVariables, variable)
	}
	slices.Sort(sourceVariables)
	for _, variable := range sourceVariables {
		telephony.SystemSources = append(telephony.SystemSources, livekitSystemSource{
			Variable: variable, Source: string(plan.SystemSources[variable]),
		})
	}
}

// buildLiveKitConnectorTelephony lowers the LiveKit Twilio connector route: the
// generated bridge speaks Twilio Media Streams and joins a local LiveKit room,
// so the env vocabulary is the Twilio account trio (like Pipecat), not SIP
// trunk fields. No SIP trunk env and no Redis.
func buildLiveKitConnectorTelephony(agent *ir.Agent, tgt ir.Target, env *envSet) (*livekitTelephony, error) {
	plan := tgt.Telephony
	if plan.Key.Carrier != "twilio" {
		return nil, fmt.Errorf("livekit connector carrier %q has no emitted setup", plan.Key.Carrier)
	}
	docs := ""
	for _, evidence := range plan.Evidence {
		if evidence.Docs != "" {
			docs = evidence.Docs
			break
		}
	}
	required, optional, ok := targetcap.TelephonyEnvironment(targetcap.TelephonyKey{
		Provider: targetcap.LiveKit, Transport: plan.Key.Transport, Carrier: plan.Key.Carrier,
	})
	if !ok {
		return nil, fmt.Errorf("livekit connector route (%s, %s, %s) has no environment vocabulary", plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier)
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range append(required, optional...) {
		allowed[key] = true
	}
	for _, key := range required {
		if plan.Environment[key] == "" {
			return nil, fmt.Errorf("livekit connector connection requires environment key %q", key)
		}
		env.add(plan.Environment[key])
	}
	for key := range plan.Environment {
		if !allowed[key] {
			return nil, fmt.Errorf("livekit connector route does not accept connection environment key %q", key)
		}
	}
	telephony := &livekitTelephony{
		Transport: "connector", Carrier: plan.Key.Carrier, Connection: plan.Connection,
		ProviderDocs:  docs,
		AccountSIDEnv: plan.Environment["account_sid"], AuthTokenEnv: plan.Environment["auth_token"],
		FromNumberEnv: plan.Environment["from_number"],
	}
	fillLiveKitTelephonyCommon(telephony, agent, plan)
	// The bridge and worker connect out to the local LiveKit Server; the carrier
	// reaches the bridge over the public HTTPS origin, and dial-out mints a token.
	env.add("UNMUTE_PUBLIC_URL")
	if telephony.HasOutbound {
		env.add("UNMUTE_OUTBOUND_TOKEN")
	}
	return telephony, nil
}

func buildLiveKitSIPTelephony(agent *ir.Agent, tgt ir.Target, env *envSet) (*livekitTelephony, error) {
	plan := tgt.Telephony
	switch plan.Key.Carrier {
	case "twilio", "telnyx", "plivo":
	default:
		return nil, fmt.Errorf("livekit SIP carrier %q has no emitted setup", plan.Key.Carrier)
	}
	docs := ""
	for _, evidence := range plan.Evidence {
		if evidence.Docs != "" {
			docs = evidence.Docs
			break
		}
	}
	if docs == "" {
		return nil, fmt.Errorf("livekit SIP carrier %q has no setup documentation", plan.Key.Carrier)
	}
	required, optional, ok := targetcap.TelephonyEnvironment(targetcap.TelephonyKey{
		Provider: targetcap.LiveKit, Transport: plan.Key.Transport, Carrier: plan.Key.Carrier,
	})
	if !ok {
		return nil, fmt.Errorf("livekit SIP route (%s, %s, %s) has no environment vocabulary", plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier)
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range append(required, optional...) {
		allowed[key] = true
	}
	for _, key := range required {
		allowed[key] = true
		if plan.Environment[key] == "" {
			return nil, fmt.Errorf("livekit SIP connection requires environment key %q", key)
		}
		env.add(plan.Environment[key])
	}
	environmentKeys := make([]string, 0, len(plan.Environment))
	for key := range plan.Environment {
		environmentKeys = append(environmentKeys, key)
	}
	slices.Sort(environmentKeys)
	for _, key := range environmentKeys {
		if !allowed[key] {
			return nil, fmt.Errorf("livekit SIP route does not accept connection environment key %q", key)
		}
	}
	telephony := &livekitTelephony{
		Transport: "sip", Carrier: plan.Key.Carrier, Connection: plan.Connection,
		ProviderDocs: docs,
		// Short on purpose: the README's runbook now dictates which console tab
		// each value comes from, so this says where to be, not what to do.
		CredentialHint: "your carrier's SIP trunking console",
		SIPAddressEnv:  plan.Environment["sip_address"], SIPUsernameEnv: plan.Environment["sip_username"],
		SIPPasswordEnv: plan.Environment["sip_password"], FromNumberEnv: plan.Environment["from_number"],
	}
	fillLiveKitTelephonyCommon(telephony, agent, plan)
	env.add("REDIS_URL")
	// No trunk name of either direction. Both dial-out paths carry the carrier's
	// trunk settings inline, from the four names the Connection already declares
	// (SCHEMA N33, 2026-08-12). Inbound still needs its two platform records, but
	// the emitted telephony-setup.sh resolves them by phone number, so no
	// environment name carries the ID (SCHEMA N36, 2026-08-12).
	return telephony, nil
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
		for _, transfer := range t.Transfers {
			if transfer.Summary != nil {
				services = append(services, transfer.Summary.services()...)
			}
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
		verified := svc.Entry.Verified
		if svc.Call.Class == "openai.responses.LLM" {
			verified = "2026-08-18"
		}
		set[fmt.Sprintf("%s: %s via %s (%s, verified %s)",
			svc.Entry.Role, svc.Vendor, svc.Call.Class, installLabel(svc.Entry), verified)] = true
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
	// A templated prompt is re-rendered from the session state on entry; an
	// untouched one keeps the bare module constant.
	// A router-bound prompt keeps its placeholders, so there is nothing to
	// re-render on entry and the update_instructions call goes away with it.
	profile, router := slngRouterBinding(agent, tgt, def.Model)
	if router {
		built.SlngScope = targetcap.SlngScope(tgt.Models.Reason[profile].AgentID,
			targetcap.SlngSite{Kind: targetcap.SlngSiteAgent, Name: name})
	}
	if ir.HasTemplate(def.Instructions) && !router {
		built.PromptExpr = promptExpr(promptConst(name), def.Instructions, "self.session.userdata", false)
	}
	if def.Model != entry.Model {
		llm, err := livekitReasonLLM(agent, tgt, def.Model, env)
		if err != nil {
			return livekitAgent{}, fmt.Errorf("agent %q: %w", name, err)
		}
		built.LLM = &llm
	}
	if def.Voice != entry.Voice {
		tts, err := livekitTTSService(tgt.Models.Speak[def.Voice], env)
		if err != nil {
			return livekitAgent{}, fmt.Errorf("agent %q: %w", name, err)
		}
		built.TTS = &tts
	}
	if built.IsEntry {
		built.Greeting = livekitGreetingFor(agent.Conversation)
	}
	for _, ref := range def.Tools {
		if tool, ok := agent.Tools[ref]; ok {
			if tool.Execution == ir.ToolMCP {
				built.MCPServers = append(built.MCPServers, livekitMCPSource(ref, tool, env))
				continue
			}
			lowered, err := buildLiveKitTool(ref, tool, agent.Variables, env)
			if err != nil {
				return livekitAgent{}, fmt.Errorf("agent %q: %w", name, err)
			}
			if lowered.Builtin != "" {
				built.Prebuilt = append(built.Prebuilt, lowered)
			} else {
				built.Tools = append(built.Tools, lowered)
			}
			continue
		}
		control, ok := agent.Controls[ref]
		if !ok {
			return livekitAgent{}, fmt.Errorf("agent %q references unknown tool/control %q", name, ref)
		}
		switch c := control.(type) {
		case *ir.AgentTransfer:
			transfer, err := buildLiveKitTransfer(agent, tgt, ref, c, env)
			if err != nil {
				return livekitAgent{}, fmt.Errorf("agent %q: %w", name, err)
			}
			built.Transfers = append(built.Transfers, transfer)
		case *ir.HumanTransfer:
			dest, ok := tgt.Destinations[c.Destination]
			if !ok {
				return livekitAgent{}, fmt.Errorf("agent %q human_transfer %q: destination %q is not in the target's destinations map", name, ref, c.Destination)
			}
			ht := livekitHumanTransfer{
				Method: ref, When: humanTransferWhen(c),
				Warm:        c.Mode == ir.TransferWarm,
				Briefing:    c.Briefing,
				RingTimeout: durationSeconds(c.RingTimeout),
				Hangup:      c.OnUnavailable == ir.OnUnavailableHangup,
			}
			if ht.Warm {
				ht.DialExpr = destinationExpr(dest, env)
				if name := ir.DestinationEnv(dest); name != "" {
					env.addRead(name)
				}
			} else {
				ht.ToExpr = referURIExpr(dest, env)
			}
			// A warm transfer needs no trunk environment name: the emitted
			// _sip_trunk() passes the carrier's own settings inline, and the
			// prebuilt then ignores LIVEKIT_SIP_OUTBOUND_TRUNK by its own
			// documented precedence (verified in warm_transfer.py, 2026-08-12).
			built.HumanTransfers = append(built.HumanTransfers, ht)
		case *ir.Delegate:
			delegate, err := buildLiveKitDelegate(agent, tgt, ref, c, env)
			if err != nil {
				return livekitAgent{}, fmt.Errorf("agent %q: %w", name, err)
			}
			built.Delegates = append(built.Delegates, delegate)
		}
	}
	return built, nil
}

// buildLiveKitTransfer is shared by agent and task tool surfaces. Both carry
// the same conversation shape and shared session userdata into the target.
func buildLiveKitTransfer(agent *ir.Agent, tgt ir.Target, ref string, control *ir.AgentTransfer, env *envSet) (livekitTransfer, error) {
	transfer := livekitTransfer{
		Method: ref, When: transferWhen(control), TargetClass: pyName(control.To),
		Announce: control.Announce, Requires: control.Requires,
	}
	if control.Context.History == ir.HistorySummary {
		summarizer, err := livekitSummaryLLM(agent, tgt, control.Context.Summarizer, env)
		if err != nil {
			return livekitTransfer{}, fmt.Errorf("transfer %q summarizer: %w", ref, err)
		}
		transfer.Summary = &summarizer
	} else {
		transfer.CtxExpr, _ = livekitCtxExpr(control.Context.TaskContext)
	}
	// context.variables (D7): a subset resets the fields the transfer does not
	// carry; `all` leaves the shared session userdata untouched.
	if !control.Context.Variables.All {
		carried := map[string]bool{}
		for _, name := range control.Context.Variables.Names {
			carried[name] = true
		}
		for _, name := range sortedVarNames(agent) {
			if carried[name] {
				continue
			}
			variable := agent.Variables[name]
			def := "None"
			if variable.Default != nil {
				def = pyLiteral(variable.Default)
			}
			transfer.ResetVars = append(transfer.ResetVars, livekitVar{
				Name: name, PyType: pyType(variable.Type), Default: def,
			})
		}
	}
	return transfer, nil
}

// appendMCPEnv adds the env names every mcp mount reads, in mount order,
// skipping the ones already required. Order is the author's, so the emitted
// list reads like the package.
func appendMCPEnv(required []string, agents []livekitAgent, tasks []livekitTask) []string {
	add := func(name string) {
		if name != "" && !slices.Contains(required, name) {
			required = append(required, name)
		}
	}
	mounts := func(servers []livekitMCPServer) {
		for _, server := range servers {
			add(server.URLEnv)
			add(server.AuthEnv)
		}
	}
	for _, a := range agents {
		mounts(a.MCPServers)
	}
	for _, t := range tasks {
		mounts(t.MCPServers)
	}
	return required
}

// livekitMCPSource lowers one mcp tool source to its mount, in the order the
// author listed it. Two sources naming the same url_env are two mounts: the
// selection lives on the source now, not on a file-name convention (N40).
func livekitMCPSource(name string, tool ir.Tool, env *envSet) livekitMCPServer {
	env.addRead(tool.URLEnv)
	source := livekitMCPServer{
		Name: name, URLEnv: tool.URLEnv, Transport: tool.MCPTransport,
		Tools: tool.MCPTools, Auth: loweredAuth(tool.Auth),
	}
	if tool.Auth != nil {
		env.addRead(tool.Auth.TokenEnv)
		source.AuthEnv = tool.Auth.TokenEnv
	}
	return source
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
			summarizer, err := livekitSummaryLLM(agent, tgt, task.Context.Summarizer, env)
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
		// hands back the typed result only (C4/N13). The finality guidance stops
		// the owner LLM re-running the finished flow (B1/V1).
		return livekitDelegate{
			Method: ref, When: delegateWhen(c) + delegateReturnFinality + delegateForwardDeclaration(agent, c),
			Task:            single,
			Then:            "return",
			Requires:        c.Requires,
			CanTaskTransfer: livekitTaskCanTransfer(agent, task),
		}, nil
	}
	group, ok := agent.TaskGroups[c.Group]
	if !ok {
		return livekitDelegate{}, fmt.Errorf("delegate %q references unknown task_group %q", ref, c.Group)
	}
	// C3: TaskGroup always shares context, so `isolated` lowers to a generated
	// sequence of standalone AgentTasks (each starts fresh, C4) instead.
	delegate := livekitDelegate{
		Method: ref, When: delegateWhen(c) + delegateForwardDeclaration(agent, c), Then: string(group.Then),
		Isolated: group.ContextScope == ir.ContextIsolated,
		Requires: c.Requires,
	}
	// N13/§4.7: return hands the owner the typed results; transfer and end do not
	// return, so the tool description must say so (the model must not wait for a
	// result that never comes). The lowerings themselves live in the template.
	switch group.Then {
	case ir.GroupReturn:
		// The flow returns its typed results; tell the owner they are final so it
		// relays them and does not re-run the finished flow (B1/V1).
		delegate.When += delegateReturnFinality
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
		delegate.CanTaskTransfer = delegate.CanTaskTransfer || livekitTaskCanTransfer(agent, agent.Tasks[step])
	}
	return delegate, nil
}

func livekitTaskCanTransfer(agent *ir.Agent, task ir.Task) bool {
	for _, ref := range task.Tools {
		if _, ok := agent.Controls[ref].(*ir.AgentTransfer); ok {
			return true
		}
	}
	return false
}

func buildLiveKitTask(agent *ir.Agent, tgt ir.Target, name string, task ir.Task, env *envSet) (livekitTask, error) {
	built := livekitTask{Name: name, Class: pyName(name), PromptConst: promptConst(name)}
	profile, router := slngRouterBinding(agent, tgt, task.Model)
	if router {
		built.SlngScope = targetcap.SlngScope(tgt.Models.Reason[profile].AgentID,
			targetcap.SlngSite{Kind: targetcap.SlngSiteTask, Name: name})
	}
	if ir.HasTemplate(task.Instructions) && !router {
		built.PromptExpr = promptExpr(promptConst(name), task.Instructions, "self.session.userdata", false)
	}
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
		rf := task.Result[fname]
		base := resultPyType(rf)
		// V2: a result field's enum reaches the finish() arg as Literal[...] too
		// (resultPyType collapses it to str otherwise); result fields carry no
		// description in the schema.
		built.Result = append(built.Result, livekitArg{
			Name: fname, PyType: base, Required: true, Enum: rf.Enum,
			Anno: pyAnno(base, rf.Enum, ""),
		})
	}
	for _, ref := range task.Tools {
		tool, ok := agent.Tools[ref]
		if !ok {
			control, exists := agent.Controls[ref]
			if !exists {
				return livekitTask{}, fmt.Errorf("task %q references unknown tool/control %q", name, ref)
			}
			transfer, supported := control.(*ir.AgentTransfer)
			if !supported {
				return livekitTask{}, fmt.Errorf("task %q references control %q: tasks support agent_transfer controls only", name, ref)
			}
			lowered, err := buildLiveKitTransfer(agent, tgt, ref, transfer, env)
			if err != nil {
				return livekitTask{}, fmt.Errorf("task %q: %w", name, err)
			}
			built.Transfers = append(built.Transfers, lowered)
			continue
		}
		if tool.Execution == ir.ToolMCP {
			built.MCPServers = append(built.MCPServers, livekitMCPSource(ref, tool, env))
			continue
		}
		lowered, err := buildLiveKitTool(ref, tool, agent.Variables, env)
		if err != nil {
			return livekitTask{}, fmt.Errorf("task %q: %w", name, err)
		}
		if lowered.Builtin != "" {
			built.Prebuilt = append(built.Prebuilt, lowered)
		} else {
			built.Tools = append(built.Tools, lowered)
		}
	}
	return built, nil
}

// buildLiveKitTool lowers a webhook or local tool to a @function_tool method
// (agents and tasks share the shape); mcp tools mount as servers upstream. A
// builtin tool becomes a prebuilt (EndCallTool) rendered into tools=, not a
// method. client/provider_hosted stay table-denied.
// livekitStateExpr is how an emitted @function_tool reaches the call state.
const livekitStateExpr = "ctx.userdata"

func buildLiveKitTool(name string, tool ir.Tool, variables map[string]ir.Variable, env *envSet) (livekitTool, error) {
	inject, needed := loweredInject(tool, variables, livekitStateExpr)
	args := livekitToolArgs(tool.Input)
	argNames := make([]string, 0, len(args))
	for _, arg := range args {
		argNames = append(argNames, arg.Name)
	}
	switch tool.Execution {
	case ir.ToolWebhook:
		env.addRead(tool.URLEnv)
		// The token rides its own env var, never the spec (SCHEMA §5.3).
		if tool.Auth != nil {
			env.addRead(tool.Auth.TokenEnv)
		}
		return livekitTool{
			Method: name, Description: tool.Description, URLEnv: tool.URLEnv,
			URLExpr: urlExpr(tool, livekitStateExpr), Inject: inject, Needed: needed,
			NeededLiteral:    neededLiteral(needed),
			JSONBody:         requestBody(argNames, inject),
			Auth:             loweredAuth(tool.Auth),
			Args:             args,
			EndsConversation: tool.Effect == ir.ToolEndsConversation,
			Announce:         tool.Announce,
		}, nil
	case ir.ToolLocal:
		return livekitTool{
			Method: name, Description: tool.Description, Local: true,
			Inject: inject, Needed: needed, NeededLiteral: neededLiteral(needed),
			CallKwargs:       callKwargs(argNames, inject),
			Args:             args,
			EndsConversation: tool.Effect == ir.ToolEndsConversation,
			Announce:         tool.Announce,
		}, nil
	case ir.ToolKnowledge:
		// One string parameter, always, and the tool owns it: the author writes
		// no input schema, so the args list is fixed here rather than derived.
		return livekitTool{
			Method: name, Description: knowledgeDescription(tool.Description),
			KnowledgeBase: tool.KnowledgeBase,
			Args:          []livekitArg{knowledgeQueryArg()},
			Announce:      tool.Announce,
		}, nil
	case ir.ToolBuiltin:
		// Prebuilt: no method, no args; the registry id picks the SDK helper.
		return livekitTool{
			Method: name, Description: tool.Description,
			Builtin: tool.Builtin, Instructions: tool.Instructions,
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

func resolveLiveKitService(role targetcap.Role, binding ir.Binding, env *envSet, site slngSite) (livekitService, error) {
	call, entry, err := resolveService(targetcap.LiveKit, role, binding, env, site)
	if err != nil {
		return livekitService{}, err
	}
	return livekitService{Call: call, Entry: entry, Vendor: cmp.Or(binding.Provider, "openai")}, nil
}

func livekitSTTService(binding *ir.Binding, env *envSet) (livekitService, error) {
	if binding == nil {
		return livekitService{}, fmt.Errorf("livekit listen binding is missing a model")
	}
	return resolveLiveKitService(targetcap.Listen, *binding, env, slngSite{})
}

func livekitTTSService(binding ir.Binding, env *envSet) (livekitService, error) {
	return resolveLiveKitService(targetcap.Speak, binding, env, slngSite{})
}

func livekitChainService(binding ir.Binding, env *envSet, site slngSite) (livekitService, error) {
	svc, err := resolveLiveKitService(targetcap.Reason, binding, env, site)
	if err != nil {
		return livekitService{}, err
	}
	svc.Call.Args = slngClientArgs(svc.Call.Args, site.ClientExpr)
	return svc, nil
}

// livekitTurnVersion maps the target's turn: binding to the
// inference.TurnDetector version (V18, B5). turn-detector-mini runs fully
// local — no LiveKit Cloud creds; an absent binding emits no version kwarg,
// so the SDK auto-selects and falls back to the mini model with a warning
// instead of raising (C5).
func livekitTurnVersion(binding *ir.Binding) (string, error) {
	if binding == nil {
		return "", nil
	}
	// The recognised set lives in internal/target, because ir.Validate asks the
	// same question and one fact gets one home (D5). This stays as a backstop:
	// the driver still refuses rather than emitting an unrecognised version.
	return targetcap.LiveKitTurnVersion(binding.Model)
}

// livekitReasonLLM resolves a reason profile plus its fallback chain (V4).
// Every profile in the chain must carry its own reason binding; validation
// has already checked slot kind, placement, and cycles.
func livekitReasonLLM(agent *ir.Agent, tgt ir.Target, profile string, env *envSet) (livekitChain, error) {
	site := livekitSlngSite(agent, tgt, profile)
	primary, err := livekitChainService(tgt.Models.Reason[profile], env, site)
	if err != nil {
		return livekitChain{}, fmt.Errorf("model %q: %w", profile, err)
	}
	out := livekitChain{Primary: primary}
	for _, fb := range agent.Models[profile].Fallback {
		svc, err := livekitChainService(tgt.Models.Reason[fb], env, livekitSlngSite(agent, tgt, fb))
		if err != nil {
			return livekitChain{}, fmt.Errorf("model %q fallback %q: %w", profile, fb, err)
		}
		out.Chain = append(out.Chain, svc)
	}
	return out, nil
}

// livekitSummaryLLM is livekitReasonLLM for a summarizer: the same chain, built
// from the one site that still carries its headers at construction. Separate
// because the site differs, not the resolution.
func livekitSummaryLLM(agent *ir.Agent, tgt ir.Target, profile string, env *envSet) (livekitChain, error) {
	primary, err := livekitChainService(tgt.Models.Reason[profile], env, livekitSummarySite(agent, tgt, profile))
	if err != nil {
		return livekitChain{}, fmt.Errorf("model %q: %w", profile, err)
	}
	out := livekitChain{Primary: primary}
	for _, fb := range agent.Models[profile].Fallback {
		svc, err := livekitChainService(tgt.Models.Reason[fb], env, livekitSummarySite(agent, tgt, fb))
		if err != nil {
			return livekitChain{}, fmt.Errorf("model %q fallback %q: %w", profile, fb, err)
		}
		out.Chain = append(out.Chain, svc)
	}
	return out, nil
}

// livekitSlngSite is where the router's per-call values live on this target: the
// call state hoisted into an entrypoint local, which is why ir.Validate holds
// every router profile to being the entry agent's. Zero for a profile that is
// not a router binding.
//
// The identity headers do not live here. One model object serves every agent and
// task in the session on this target, so a constructor header set would send one
// scope for all of them, which is the defect. They travel per request instead,
// built by the emitted mixin from the speaking class's own scope constant. And
// this is not a preference: a constructor extra_headers overwrites the
// per-request one wholesale rather than merging, so leaving it would silently win
// while the emitted source still looked right (research R5).
func livekitSlngSite(agent *ir.Agent, tgt ir.Target, profile string) slngSite {
	if binding, ok := tgt.Models.Reason[profile]; !ok || !binding.Router() {
		return slngSite{}
	}
	site := slngSite{
		SessionExpr:       livekitSessionIDExpr,
		Names:             slngTemplateNames(agent, tgt, profile),
		ConfigFunc:        slngConfigFunc(profile),
		HeadersPerRequest: true,
		BodyPerRequest:    true,
		ClientExpr:        livekitEntryClientExpr,
	}
	if len(agent.Variables) > 0 {
		// The node's own name for the state object, not the entrypoint local:
		// the body is rendered where the request is made, and _slng_llm_node is
		// a module function whose only handle on the call is the session.
		site.StateExpr = livekitNodeStateExpr
	}
	return site
}

// livekitSummarySite is the one router site on this target that keeps its headers
// at construction. The summarizer is built inline where a handoff shapes history
// and its request never passes through the overridden request path, so there is
// nothing per-request to carry them.
//
// It is also built inside an agent method, where the entrypoint's locals are out
// of scope, so both values are read off the session's user data object instead.
// Before the session id moved there, this expression named a local that did not
// exist in that scope: a NameError waiting for the first package to combine a
// router binding with history: summary.
func livekitSummarySite(agent *ir.Agent, tgt ir.Target, profile string) slngSite {
	site := livekitSlngSite(agent, tgt, profile)
	if site.ConfigFunc == "" {
		return site
	}
	site.HeadersPerRequest = false
	// The body stays at construction here, and that is already current rather
	// than frozen: this site is built inside an agent method at handoff time, so
	// its construction is the moment of its request.
	site.BodyPerRequest = false
	site.SessionExpr = livekitRuntimeSessionIDExpr
	site.ClientExpr = livekitRuntimeClientExpr
	if site.StateExpr != "" {
		site.StateExpr = livekitRuntimeStateExpr
	}
	site.Scope = targetcap.SlngScope(tgt.Models.Reason[profile].AgentID, targetcap.SlngSite{Kind: targetcap.SlngSiteSummary})
	return site
}

// The entrypoint's own names for the router's per-call values, and the same two
// values as an agent method reaches them. The session id is not called
// `session_id`, because this template already writes "session_id": room_name into
// the telephony call context and that is a different thing.
const (
	livekitSessionIDExpr        = "slng_state.slng_session_id"
	livekitRuntimeSessionIDExpr = "self.session.userdata.slng_session_id"
	livekitRuntimeStateExpr     = "self.session.userdata"
	// livekitNodeStateExpr is the third of the three, and the one the per-request
	// body is rendered with: _slng_llm_node is a module function, so it has
	// neither the entrypoint's locals nor a `self`, only the session it reads off
	// the asking agent.
	livekitNodeStateExpr = "session.userdata"
	// The router client, in the two scopes that build a router model: the
	// entrypoint local it was just assigned to, and an agent method reaching the
	// same object through the session.
	livekitEntryClientExpr   = "slng_state.slng_client"
	livekitRuntimeClientExpr = "self.session.userdata.slng_client"
)

// livekitSessionIDField is the field the per-call session id occupies on the user
// data object, and the name both expressions above are built from.
const livekitSessionIDField = "slng_session_id"

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
		// exclude_handoff drops stale AgentHandoff markers from the carried
		// history (upstream recipe idiom).
		if excludeCalls {
			return "self.chat_ctx.copy(exclude_instructions=True, exclude_function_call=True, exclude_handoff=True)", false
		}
		return "self.chat_ctx.copy(exclude_instructions=True, exclude_handoff=True)", false
	}
}

// --- small helpers ---------------------------------------------------------

func promptConst(name string) string {
	return strings.ToUpper(strings.NewReplacer("-", "_").Replace(name)) + "_PROMPT"
}

func transferWhen(c *ir.AgentTransfer) string {
	return orDefault(c.When, "Transfer the caller to the "+c.To+".")
}

func humanTransferWhen(c *ir.HumanTransfer) string {
	return orDefault(c.When, "Transfer the caller to a human agent.")
}

// destinationExpr renders a resolved destination as Python: a quoted literal,
// or an os.environ lookup when the target defers it to an env var (N26). The
// env name is registered so it reaches .env.example and the required-env list.
// destinationExpr renders a destination for the warm path's `sip_call_to`,
// which takes a phone number or a SIP user and refuses a full URI (measured
// 2026-08-20; the live call that found it is in the emitted helper's comment).
// The value is only known on the call when it defers to an environment name, so
// the normalising happens at runtime there and at build time for a literal.
func destinationExpr(destination string, env *envSet) string {
	if name := ir.DestinationEnv(destination); name != "" {
		env.add(name)
		return "_sip_user(" + envRef(name) + ")"
	}
	return pyQuote(sipUser(destination))
}

// sipUser is the Go side of the same rule, for a destination authored as a
// literal rather than deferred to the environment.
func sipUser(destination string) string {
	if at := strings.LastIndex(destination, "@"); at >= 0 {
		destination = destination[:at]
	}
	for _, scheme := range []string{"sip:", "sips:", "tel:"} {
		if trimmed, ok := strings.CutPrefix(destination, scheme); ok {
			return trimmed
		}
	}
	return destination
}

// referURIExpr renders a cold-transfer destination for the `transfer_to` field
// of TransferSIPParticipant, which takes a **URI** rather than a bare number:
// `tel:+15105550100`, or `sip:+15105550100@<host>` for a provider that needs the
// trunk host in the Refer-To (Plivo documents that form as mandatory). Verified
// 2026-08-12 against the call-forwarding guide, whose own Python example writes
// `transfer_to=f"tel:{transfer_to}"`. A bare E.164 appears in no documented
// example, so it is normalised rather than forwarded.
//
// A literal is normalised here, at compile time. An env-var destination cannot
// be, because its value is only known on the call, so it goes through the
// emitted `_refer_uri` helper instead.
func referURIExpr(destination string, env *envSet) string {
	if name := ir.DestinationEnv(destination); name != "" {
		env.add(name)
		return "_refer_uri(" + envRef(name) + ")"
	}
	return pyQuote(referURI(destination))
}

// referURI adds the `tel:` scheme to a bare number and leaves an authored URI
// alone. A destination may already be a `sip:` URI (SCHEMA N26), and double
// prefixing one would break the transfer it was written for.
func referURI(destination string) string {
	for _, scheme := range []string{"tel:", "sip:", "sips:"} {
		if strings.HasPrefix(destination, scheme) {
			return destination
		}
	}
	return "tel:" + destination
}

// durationSeconds renders a validated authored duration as the float literal the
// LiveKit APIs take. Empty stays empty so the emitted call omits the argument and
// the platform default applies (SCHEMA N25).
func durationSeconds(value ir.Duration) string {
	if value == "" {
		return ""
	}
	duration, err := time.ParseDuration(string(value))
	if err != nil {
		return "" // ir.Validate already rejected it; never emit a broken literal
	}
	return strconv.FormatFloat(duration.Seconds(), 'f', -1, 64)
}

func delegateWhen(c *ir.Delegate) string {
	return orDefault(c.When, "Run this flow.")
}

// delegateReturnFinality is appended to a then:return delegate docstring so the
// owner LLM treats the returned result as the final outcome and does not re-run
// the finished flow (B1/V1; mirrors the upstream flow-entry docstring idiom).
const delegateReturnFinality = " When this flow finishes it returns its result to you. That result is the final outcome for this request: relay it to the caller and continue. Do not run this flow again for the same request. " + unservedOwnerRule

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
	slices.Sort(names)
	args := make([]livekitArg, 0, len(names))
	for _, n := range names {
		pt := "str"
		var enum []string
		var desc string
		if prop, ok := props[n].(map[string]any); ok {
			if t, ok := prop["type"].(string); ok {
				pt = pyTypeForJSON(t)
			}
			// V2: carry the declared enum and per-property description across so
			// the LLM sees the schema the tool YAML wrote (C4). Non-string enum
			// values are left off Literal (falls back to the base type).
			if e, ok := prop["enum"].([]any); ok {
				for _, v := range e {
					if s, ok := v.(string); ok {
						enum = append(enum, s)
					}
				}
			}
			if d, ok := prop["description"].(string); ok {
				desc = d
			}
		}
		args = append(args, livekitArg{
			Name: n, PyType: pt, Required: required[n], Enum: enum, Desc: desc,
			Anno: pyAnno(pt, enum, desc),
		})
	}
	return args
}

// pyAnno renders a tool arg's Python annotation from its base type, enum, and
// description (V2). An enum becomes Literal[...]; a description wraps the type in
// Annotated[..., Field(description=...)]. LiveKit derives the JSON-schema the LLM
// sees from these via pydantic (build_legacy_openai_schema): Literal → enum,
// Field(description=...) → the property description.
func pyAnno(base string, enum []string, desc string) string {
	typeExpr := base
	if len(enum) > 0 {
		quoted := make([]string, len(enum))
		for i, e := range enum {
			quoted[i] = pyQuote(e)
		}
		typeExpr = "Literal[" + strings.Join(quoted, ", ") + "]"
	}
	if desc != "" {
		return "Annotated[" + typeExpr + ", Field(description=" + pyQuote(desc) + ")]"
	}
	return typeExpr
}

func livekitTaskPrompt(task ir.Task, result []livekitArg) string {
	names := make([]string, len(result))
	for i, r := range result {
		names[i] = r.Name
	}
	return task.Instructions + taskFinishContract("finish", names)
}

func livekitGreetingFor(c *ir.Conversation) *livekitGreeting {
	if c == nil || c.Greeting == nil {
		return &livekitGreeting{RunLLM: true}
	}
	g := c.Greeting
	switch {
	case g.SpeaksFirst == ir.SpeaksFirstAgent && g.Text != "":
		// The fixed line may name variables known at call start (C11); it is
		// rendered once, when the session opens.
		return &livekitGreeting{Say: g.Text, Templated: ir.HasTemplate(g.Text)}
	case g.SpeaksFirst == ir.SpeaksFirstAgent:
		return &livekitGreeting{RunLLM: true}
	default:
		return &livekitGreeting{Silent: true}
	}
}

// knowledgeQueryArg is the one parameter a knowledge tool takes. The tool owns
// it, so the author writes no input schema and this is not derived from one.
func knowledgeQueryArg() livekitArg {
	return livekitArg{
		Name: "query", PyType: "str", Required: true,
		Desc: knowledgeQueryDescription,
		Anno: pyAnno("str", nil, knowledgeQueryDescription),
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
	if data.NeedsMCP {
		extras["mcp"] = true // without the extra the emitted import fails (N40)
	}
	// Knowledge bases only. These are the whole runtime cost of the feature, and a
	// package with no knowledge: section pays none of it: the set measures about
	// 178 MB installed (measured 2026-08-26, down from 433 MB before the Chroma
	// store came out), and Pipecat Cloud's warm-up time varies with image size.
	for _, pkg := range knowledgeDeps(data.Knowledge) {
		packages[pkg] = true
	}
	// The author's declared version is the pin, exactly as it is on Pipecat.
	constraint := "==" + data.Version
	pkg := targetcap.FrameworkPackage(targetcap.LiveKit)
	base := pkg + constraint
	if len(extras) > 0 {
		base = fmt.Sprintf("%s[%s]%s", pkg, strings.Join(sortedKeys(extras), ","), constraint)
	}
	deps := append([]string{
		base,
		pinned("livekit-plugins-silero", targetcap.SileroFloor),
		"python-dotenv",
		// httpx is unconditional, and not only for our own webhook tools.
		// `livekit/agents/inference/llm.py` imports it while livekit-agents
		// declares no httpx dependency: it used to arrive transitively through
		// `openai`, and openai 3.0 switched to the `httpx2` distribution. Since
		// `livekit/agents/__init__.py` imports `inference` eagerly, a package
		// that pulls httpx from nowhere else cannot import livekit.agents at
		// all. Verified 2026-08-12 against livekit-agents 1.6.9 and openai
		// 3.0.0 in a clean container: undeclared by the former, absent from the
		// latter. Drop this when livekit-agents declares its own.
		"httpx",
	}, sortedKeys(packages)...)
	switch data.TracingProvider {
	case "langfuse":
		deps = append(deps, "langfuse>=3", "opentelemetry-sdk>=1.33,<2")
	case "coval":
		// livekit-agents brings the OTel API and SDK in through its own
		// telemetry module, but nothing in the LiveKit stack pulls the OTLP HTTP
		// exporter, which is the one Coval ingests. Declare both so a clean
		// install cannot import tracing.py and fail on the exporter.
		deps = append(deps, "opentelemetry-sdk>=1.33,<2", "opentelemetry-exporter-otlp-proto-http>=1.33,<2")
	}
	// The connector bridge is a standalone aiohttp server that places outbound
	// Twilio calls and validates inbound webhook signatures. livekit-agents
	// already pulls in livekit rtc and aiohttp, but pin them so the bridge does
	// not depend on that staying true.
	if data.Telephony != nil && data.Telephony.Transport == "connector" {
		deps = append(deps, "aiohttp", "twilio")
	}
	slices.Sort(deps)
	return deps
}
