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
		Project:           tgt.Name,
		Version:           tgt.Version,
		DeploymentRegions: tgt.DeploymentRegions,
		Deploys:           livekitDeploys(tgt.DeploymentRegions),
		AgentName:         tgt.Name,
		EntryAgent:        agent.EntryAgent,
		EntryClass:        pyName(agent.EntryAgent),
		TurnVersion:       turnVersion,
		Pins:              tgt.Pins,
		Tracing:           agent.Tracing != nil && agent.Tracing.Provider == "langfuse",
	}
	if data.Tracing {
		for _, name := range []string{"LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY", "LANGFUSE_BASE_URL"} {
			env.add(name)
		}
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
	collectTools := func(tools []livekitTool, servers []livekitMCPServer) {
		data.NeedsMCP = data.NeedsMCP || len(servers) > 0
		for _, tool := range tools {
			if tool.URLEnv != "" {
				data.NeedsHTTPX = true // webhook tool POSTs with httpx (agents + tasks own them)
			}
			if tool.Auth != nil {
				data.AuthKinds.add(tool.Auth.Kind) // one helper per scheme in use (V8)
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
	if len(data.CallStartVars) > 0 {
		data.DevOptionalEnv = []string{"UNMUTE_CALL_START"}
	}
	// Every declared secret reaches the environment, but the *startup check* skips
	// the route's own names. One file serves every channel on this driver, so a
	// check that demanded carrier credentials would refuse a browser or console
	// session on a phone package — the workflow FR-018 protects. This restores
	// exactly the behaviour these packages had before their telephony names were
	// declared in `secrets:` (SCHEMA N40); the route's values are still listed in
	// .env.example, the compile report, and the runbook.
	for _, name := range requiredSecretEnv(agent) {
		env.add(name)
	}
	data.RequiredSecrets = withoutRouteEnv(requiredSecretEnv(agent), agent, tgt)
	data.NeedsRender = renderNeeds(agent)
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

	applyLiveKitConversation(agent.Conversation, &data)

	// Telephony: outbound + AMD voicemail (V8/N6); cold/warm transfer imports.
	channelNames := make([]string, 0, len(agent.Channels))
	for name := range agent.Channels {
		channelNames = append(channelNames, name)
	}
	sort.Strings(channelNames)
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
	// Snapshot the provider creds before telephony env is added: the web dev
	// image (compose.dev.yaml) runs `agent.py dev` against a single-node dev
	// livekit-server and needs no telephony env. LIVEKIT_URL/KEY/SECRET are in
	// this set but the template hardcodes their dev values.
	//
	// The route's own names are removed rather than merely not added: `secrets:`
	// now declares them (SCHEMA N40), so a package with a secrets block would
	// otherwise demand carrier credentials for a browser session (FR-018).
	data.DevEnv = withoutRouteEnv(env.sorted(), agent, tgt)
	data.Telephony, err = buildLiveKitTelephony(agent, tgt, env)
	if err != nil {
		return livekitData{}, err
	}
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
	}

	data.InferenceUses = livekitInferenceUses(data)

	data.PluginModules = collectLiveKitPlugins(data)
	data.Deps = livekitDeps(data)
	data.RequiredEnv = env.sorted()
	data.PlatformEnv, data.OperatorEnv = splitPlatformEnv(data.RequiredEnv, tgt.Telephony)
	// The undeclared-name list is built from the operator's half, not from every
	// required name. A platform-supplied name belongs in exactly one section: it
	// has its own, which explains that you do not set it. Listing it in both
	// tells the reader to set a value and then not to.
	data.Secrets, data.ExtraEnv = secretEnvDocs(agent, data.OperatorEnv)
	return data, nil
}

// splitPlatformEnv separates the names the operator must supply from the names
// something else supplies. The route already declares the second set
// (LocallySuppliedEnvironment): on LiveKit Cloud the platform injects
// LIVEKIT_URL, LIVEKIT_API_KEY, and LIVEKIT_API_SECRET into a deployed agent and
// drops them from any secrets file, and its managed SIP service owns Redis,
// which no emitted Python reads on this driver. Listing those beside the
// operator's own keys made a deployed agent look as though it needed a Redis it
// can never use, so the emitted env file labels them instead.
func splitPlatformEnv(required []string, plan *ir.TelephonyPlan) (platform, operator []string) {
	if plan == nil {
		return nil, required
	}
	for _, name := range required {
		if slices.Contains(plan.LocalEnvironment, name) {
			platform = append(platform, name)
			continue
		}
		operator = append(operator, name)
	}
	return platform, operator
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
	sort.Strings(sourceVariables)
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
	sort.Strings(environmentKeys)
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

func livekitTypeCheck(t ir.PrimitiveType) string {
	switch t {
	case ir.PrimitiveBoolean:
		return "isinstance(value, bool)"
	case ir.PrimitiveInteger:
		return "isinstance(value, int) and not isinstance(value, bool)"
	case ir.PrimitiveNumber:
		return "isinstance(value, (int, float)) and not isinstance(value, bool)"
	default:
		return "isinstance(value, str)"
	}
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
	// A templated prompt is re-rendered from the session state on entry; an
	// untouched one keeps the bare module constant.
	if ir.HasTemplate(def.Instructions) {
		built.PromptExpr = promptExpr(promptConst(name), def.Instructions, "self.session.userdata")
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
	mcpByEnv := map[string][]string{}
	for _, ref := range def.Tools {
		if tool, ok := agent.Tools[ref]; ok {
			if tool.Execution == ir.ToolMCP {
				env.add(tool.URLEnv)
				mcpByEnv[tool.URLEnv] = append(mcpByEnv[tool.URLEnv], ref)
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
				Method: ref, When: humanTransferWhen(c),
				// Cold is a SIP REFER, whose destination must be a URI; warm
				// dials a number. Two positions, two shapes, one destination.
				ToExpr:      referURIExpr(dest, env),
				DialExpr:    destinationExpr(dest, env),
				Warm:        c.Mode == ir.TransferWarm,
				Briefing:    c.Briefing,
				RingTimeout: ringTimeoutSeconds(c.RingTimeout),
				Hangup:      c.OnUnavailable == ir.OnUnavailableHangup,
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
		// hands back the typed result only (C4/N13). The finality guidance stops
		// the owner LLM re-running the finished flow (B1/V1).
		return livekitDelegate{Method: ref, When: delegateWhen(c) + delegateReturnFinality, Task: single, Then: "return"}, nil
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
	}
	return delegate, nil
}

func buildLiveKitTask(agent *ir.Agent, tgt ir.Target, name string, task ir.Task, env *envSet) (livekitTask, error) {
	built := livekitTask{Name: name, Class: pyName(name), PromptConst: promptConst(name)}
	if ir.HasTemplate(task.Instructions) {
		built.PromptExpr = promptExpr(promptConst(name), task.Instructions, "self.session.userdata")
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
	built.MCPServers = livekitMCPServers(mcpByEnv)
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
		env.add(tool.URLEnv)
		// The token rides its own env var, never the spec (SCHEMA §5.3).
		if tool.Auth != nil {
			env.add(tool.Auth.TokenEnv)
		}
		return livekitTool{
			Method: name, Description: tool.Description, URLEnv: tool.URLEnv,
			URLExpr: urlExpr(tool, livekitStateExpr), Inject: inject, Needed: needed,
			NeededLiteral:    neededLiteral(needed),
			JSONBody:         requestBody(argNames, inject),
			Auth:             loweredAuth(tool.Auth),
			Args:             args,
			EndsConversation: tool.Effect == ir.ToolEndsConversation,
		}, nil
	case ir.ToolLocal:
		return livekitTool{
			Method: name, Description: tool.Description, Local: true,
			Inject: inject, Needed: needed, NeededLiteral: neededLiteral(needed),
			CallKwargs:       callKwargs(argNames, inject),
			Args:             args,
			EndsConversation: tool.Effect == ir.ToolEndsConversation,
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

// livekitEnvRef renders the driver's environment-lookup idiom.
func livekitEnvRef(name string) string { return "os.environ[" + pyQuote(name) + "]" }

func resolveLiveKitService(role targetcap.Role, binding ir.Binding, env *envSet) (livekitService, error) {
	call, entry, err := resolveService(defaultCatalog, targetcap.LiveKit, role, binding, livekitEnvRef, env)
	if err != nil {
		return livekitService{}, err
	}
	return livekitService{Call: call, Entry: entry, Vendor: firstNonEmpty(binding.Provider, "openai")}, nil
}

func livekitSTTService(binding *ir.Binding, env *envSet) (livekitService, error) {
	if binding == nil {
		return livekitService{}, fmt.Errorf("livekit listen binding is missing a model")
	}
	return resolveLiveKitService(targetcap.Listen, *binding, env)
}

func livekitTTSService(binding ir.Binding, env *envSet) (livekitService, error) {
	return resolveLiveKitService(targetcap.Speak, binding, env)
}

func livekitChainService(binding ir.Binding, env *envSet) (livekitService, error) {
	return resolveLiveKitService(targetcap.Reason, binding, env)
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
		// exclude_handoff drops stale AgentHandoff markers from the carried
		// history (upstream recipe idiom); exclude_config_update is intentionally
		// omitted to stay within the >=1.5 floor (dl§C6).
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

// destinationExpr renders a resolved destination as Python: a quoted literal,
// or an os.environ lookup when the target defers it to an env var (N26). The
// env name is registered so it reaches .env.example and the required-env list.
func destinationExpr(destination string, env *envSet) string {
	if name := ir.DestinationEnv(destination); name != "" {
		env.add(name)
		return livekitEnvRef(name)
	}
	return pyQuote(destination)
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
		return "_refer_uri(" + livekitEnvRef(name) + ")"
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

// ringTimeoutSeconds renders a validated ring_timeout as the float literal both
// LiveKit APIs take. Empty stays empty so the emitted call omits the argument
// and the platform default applies (SCHEMA N25).
func ringTimeoutSeconds(value ir.Duration) string {
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
	if c.When != "" {
		return c.When
	}
	return "Run this flow."
}

// delegateReturnFinality is appended to a then:return delegate docstring so the
// owner LLM treats the returned result as the final outcome and does not re-run
// the finished flow (B1/V1; mirrors the upstream flow-entry docstring idiom).
const delegateReturnFinality = " When this flow finishes it returns its result to you. That result is the final outcome for this request: relay it to the caller and continue. Do not run this flow again for the same request."

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
		var enum []string
		var desc string
		if prop, ok := props[n].(map[string]any); ok {
			if t, ok := prop["type"].(string); ok {
				pt = jsonPyType(t)
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
		// The fixed line may name variables known at call start (C11); it is
		// rendered once, when the session opens.
		return &livekitGreeting{Say: g.Text, Templated: ir.HasTemplate(g.Text)}
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
	// A warm transfer imports the beta WarmTransferTask, and a beta API is
	// allowed to move between minor releases (it already renamed its
	// instructions type once). So a warm package pins the minor series the
	// import was verified against instead of floating to <2.0 (SPEC V10, C3).
	constraint := fmt.Sprintf(">=%d.%d", livekitVersionMajor, livekitVersionMinMinor)
	if data.HasWarmTransfer {
		constraint = fmt.Sprintf(">=%d.%d,<%d.%d", livekitVersionMajor, livekitWarmVerifiedMinor,
			livekitVersionMajor, livekitWarmVerifiedMinor+1)
	}
	base := "livekit-agents" + constraint
	if len(extras) > 0 {
		base = fmt.Sprintf("livekit-agents[%s]%s", strings.Join(sortedKeys(extras), ","), constraint)
	}
	deps := append([]string{
		base,
		pinned("livekit-plugins-silero", ">=1.6.1"),
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
	if data.Tracing {
		deps = append(deps, "langfuse>=3", "opentelemetry-sdk>=1.33,<2")
	}
	// The connector bridge is a standalone aiohttp server that places outbound
	// Twilio calls and validates inbound webhook signatures. livekit-agents
	// already pulls in livekit rtc and aiohttp, but pin them so the bridge does
	// not depend on that staying true.
	if data.Telephony != nil && data.Telephony.Transport == "connector" {
		deps = append(deps, "aiohttp", "twilio")
	}
	sort.Strings(deps)
	return deps
}
