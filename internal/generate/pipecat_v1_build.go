package generate

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/slng-ai/unmute/internal/devmetrics"
	"github.com/slng-ai/unmute/internal/ir"
	targetcap "github.com/slng-ai/unmute/internal/target"
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
		// Tracing is on for either provider now, and TracingProvider says which.
		Tracing:         agent.Tracing != nil,
		TracingProvider: tracingProviderOf(agent),
		ResultsHint:     pipecatResultsHint,
		// The VAD's stop_secs is this driver's silence window: the same knob
		// LiveKit calls endpointing min_delay. A transcriber slower than the
		// default answers half a sentence (B: fragmented STT, 2026-08-20).
		EndpointingDelay: pipecatEndpointingDelay(target.Models.Turn),
	}
	// Read through the same door validate uses, so the command and the emitted
	// project cannot disagree about what the account has to be allowed to do.
	data.Prerequisites = ir.RoutePrerequisites(agent, target, targetcap.Pipecat)
	// Rendered into the agent, deliberately never added to the env set: it is
	// the plane's to set, never the author's (gate C4).
	if target.Telephony != nil {
		data.CarrierSteps = slices.Clone(target.Telephony.ManualSteps)
	}
	env := newEnvSet()
	for _, name := range tracingEnv(data.TracingProvider) {
		env.add(name)
	}

	stt, err := sttService(target.Models.Listen, env)
	if err != nil {
		return pipecatData{}, err
	}
	data.STT = stt

	// The router's module-level helpers, collected before any LLM resolves so
	// every construction site can name its profile's configuration function.
	// Empty on a package with no router binding, and then nothing is emitted.
	data.Slng, err = slngHelpersFor(agent, target)
	if data.Slng.Any() {
		// This target's only seam for a response hook is a service that overrides
		// how it builds its client, so a router service is that subclass. It names
		// httpx's limits, so the import is needed whether or not this package has
		// a webhook tool.
		data.Slng.RouterClass = slngRouterClass
		data.NeedsHTTPX = true
	}
	if err != nil {
		return pipecatData{}, err
	}

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
	data.HandoffControls = handoffControls(agent)
	data.DevOptionalEnv = []string{"UNMUTE_LOG_LEVEL", devmetrics.Env}
	if len(data.CallStartVars) > 0 {
		data.DevOptionalEnv = append(data.DevOptionalEnv, "UNMUTE_CALL_START")
	}
	slices.Sort(data.DevOptionalEnv)
	// Declared secrets join the startup check; a required one missing fails the
	// bot before it answers a call (V12).
	for _, name := range requiredSecretEnv(agent, target, env) {
		env.add(name)
	}
	// Snapshot the provider creds before telephony env is added: the web dev
	// image (compose.dev.yaml) runs bot.py over WebRTC and needs no telephony
	// or coordination env.
	//
	// The route's own names are removed rather than merely not added, because
	// `secrets:` now declares them (SCHEMA N41) and the loop above just put them
	// in. Without this, giving a package a `secrets:` block would make its
	// browser session demand carrier credentials, which is the workflow FR-018
	// exists to protect.
	data.DevEnv = withoutRouteEnv(env.sorted(), agent, target, env)
	data.DailyCarrier, err = buildPipecatDailyCarrier(agent, target, env)
	if err != nil {
		return pipecatData{}, err
	}
	data.CloudWebsocket, err = buildPipecatCloudWebsocket(agent, target, env)
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
	knowledge, err := loweredKnowledge(agent, env)
	if err != nil {
		return pipecatData{}, err
	}
	data.Knowledge = knowledge
	data.Imports, data.Extras, data.Deps = collectImportsExtras(data)
	data.Deps = append(data.Deps, knowledgeDeps(data.Knowledge)...)
	if data.DailyCarrier != nil {
		// The operator-run ingress helper verifies Twilio's signature before its
		// platform-keyed start request. Twilio's SDK is the one carrier SDK this
		// driver still declares.
		data.Deps = append(data.Deps, "twilio>=9,<10")
	}
	slices.Sort(data.Deps)
	data.RequiredEnv = env.sorted()
	var supplied []string
	if target.Telephony != nil {
		supplied = slices.Clone(target.Telephony.LocalEnvironment)
		data.SuppliedForYou = slices.Clone(supplied)
	}
	data.AuthorEnv = authorEnv(data.RequiredEnv, supplied)
	if data.DailyCarrier != nil {
		// Whatever the helper reads is not the deployed agent's business, so the
		// two groups are split here, once, and every surface reads the split
		// (contracts/environment.md).
		for _, name := range data.RequiredEnv {
			if slices.Contains(data.DailyCarrier.HelperEnv, name) {
				continue
			}
			data.DailyCarrier.AgentEnv = append(data.DailyCarrier.AgentEnv, name)
			// DevEnv is the provider-credential snapshot the process already checks
			// at startup, so the phone call's own check is the remainder: exactly
			// what a call adds, listed once.
			if !slices.Contains(data.DevEnv, name) {
				data.DailyCarrier.CallEnv = append(data.DailyCarrier.CallEnv, name)
			}
		}
	}
	if data.CloudWebsocket != nil {
		// What a phone call adds, and nothing more. DevEnv is the provider-credential
		// snapshot the process already checks at startup, so the remainder is exactly
		// the carrier names plus the organization. A pure-inbound package's remainder
		// is empty, which is the shape the route exists to produce (spec FR-005/FR-008).
		for _, name := range data.RequiredEnv {
			if !slices.Contains(data.DevEnv, name) {
				data.CloudWebsocket.CallEnv = append(data.CloudWebsocket.CallEnv, name)
			}
		}
	}
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

// pipecatConnectionVocabulary checks the Connection's key set against the route
// row in both directions — a missing required key and an unaccepted key each
// fail naming the route — and registers every required name so it reaches
// .env.example, REQUIRED_ENV, and the compile report. One home for the check,
// shared by the carrier-websocket routes and the Daily carrier leg.
func pipecatConnectionVocabulary(plan *ir.TelephonyPlan, env *envSet) error {
	required, optional, ok := targetcap.TelephonyEnvironment(targetcap.TelephonyKey{
		Provider: targetcap.Pipecat, Transport: plan.Key.Transport, Carrier: plan.Key.Carrier,
	})
	if !ok {
		return fmt.Errorf("pipecat telephony route (%s, %s, %s) has no environment vocabulary", plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier)
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range append(required, optional...) {
		allowed[key] = true
	}
	for _, key := range required {
		name := plan.Environment[key]
		if name == "" {
			return fmt.Errorf("pipecat telephony route %s requires connection environment key %q", plan.Key.Carrier, key)
		}
		env.add(name)
	}
	for _, key := range slices.Sorted(maps.Keys(plan.Environment)) {
		if !allowed[key] {
			return fmt.Errorf("pipecat telephony route %s does not accept connection environment key %q", plan.Key.Carrier, key)
		}
	}
	return nil
}

// buildPipecatDailyCarrier lowers the (pipecat, daily-sip, <carrier>) route: the
// operator's own carrier forwards the call over SIP into a per-call Daily room.
//
// No REDIS_URL is added: this route keeps no shared control record. The helper
// does require its configured public origin so Twilio's signed URL is stable
// behind tunnels and reverse proxies.
func buildPipecatDailyCarrier(agent *ir.Agent, resolved ir.Target, env *envSet) (*pipecatDailyCarrier, error) {
	plan := resolved.Telephony
	if plan == nil || plan.Key.Provider != ir.ProviderPipecat || plan.Key.Transport != "daily-sip" {
		return nil, nil
	}
	if plan.Key.Carrier != "twilio" {
		// The route row is the real gate; this is the emitter's half of it, so a
		// carrier whose forwarding action and runbook text nobody has written
		// cannot compile to a build that looks complete.
		return nil, fmt.Errorf("pipecat telephony route (%s, %s, %s) has no emitted carrier forwarding action", plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier)
	}
	if agent.Capacity == nil || agent.Capacity.PeakStartsPerSecond <= 0 {
		return nil, fmt.Errorf("pipecat telephony requires positive capacity.peak_starts_per_second")
	}
	if err := pipecatConnectionVocabulary(plan, env); err != nil {
		return nil, err
	}
	carrier := &pipecatDailyCarrier{
		Carrier: plan.Key.Carrier, Connection: plan.Connection,
		AccountSIDEnv: plan.Environment["account_sid"], AuthTokenEnv: plan.Environment["auth_token"],
		SIPAddressEnv: plan.Environment["sip_address"], FromNumberEnv: plan.Environment["from_number"],
	}
	for _, evidence := range plan.Evidence {
		switch evidence.Feature {
		case "inbound":
			carrier.HasInbound = true
		case "outbound":
			carrier.HasOutbound = true
		}
	}
	// The helper's own names. DAILY_API_KEY is not one of them: the room
	// is the platform's to create, so the helper never calls Daily. It stays on
	// the agent side, where a local `uv run bot.py` mints its own room with it.
	//
	// No outbound trigger token, because the helper has no endpoint that places a
	// call: outbound is started against the platform with the same public key.
	// The Twilio auth token stays agent-side too because the deployed bot uses it
	// to forward the existing call; the helper reads that shared route value for
	// signature validation rather than classifying it as helper-only here.
	carrier.HelperEnv = []string{"PIPECAT_CLOUD_API_KEY", "UNMUTE_PUBLIC_URL"}
	for _, name := range carrier.HelperEnv {
		env.add(name)
	}
	env.add("DAILY_API_KEY")
	// Optional, so never required and never in the startup check: hold audio the
	// operator hosts, and the Daily room geography. Absent means the emitted
	// default (a spoken hold line) and Daily's own default region.
	carrier.OptionalEnv = []string{"DAILY_ROOM_GEO", "DAILY_HOLD_AUDIO_URL"}
	return carrier, nil
}

// pipecatCloudStreamURL is the platform's carrier stream endpoint, regional when
// the target declares a region (research F1). One function, called once, so the
// Bin the README dictates, the outbound command, and the transfer markup all
// render the same host: three copies of a URL is three chances to point a call at
// the wrong region (data-model section 3).
func pipecatCloudStreamURL(carrier, region string) string {
	host := "api.pipecat.daily.co"
	if region != "" {
		host = region + "." + host
	}
	return "wss://" + host + "/ws/" + carrier
}

// buildPipecatCloudWebsocket lowers the (pipecat, cloud-websocket, <carrier>)
// route: the carrier streams the call's audio to the platform, which starts this
// agent. Nothing of the operator's runs anywhere (SCHEMA N38).
//
// No REDIS_URL and no UNMUTE_PUBLIC_URL, and the absence is the whole point:
// there is no coordination record to keep and no endpoint of ours to have a
// public address.
func buildPipecatCloudWebsocket(agent *ir.Agent, resolved ir.Target, env *envSet) (*pipecatCloudWebsocket, error) {
	plan := resolved.Telephony
	if plan == nil || plan.Key.Provider != ir.ProviderPipecat || plan.Key.Transport != "cloud-websocket" {
		return nil, nil
	}
	if plan.Key.Carrier != "twilio" {
		// The platform terminates Telnyx, Plivo, and Exotel streams too (research
		// F9), and each needs its own dictated console markup and its own call
		// control. The route row is the real gate; this is the emitter's half of it,
		// so a carrier whose runbook nobody has written cannot compile to a build
		// that looks finished.
		return nil, fmt.Errorf("pipecat telephony route (%s, %s, %s) has no dictated carrier markup yet; the platform terminates other carriers' streams, but this project has only written Twilio's", plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier)
	}
	carrier := &pipecatCloudWebsocket{
		Carrier:   plan.Key.Carrier,
		StreamURL: pipecatCloudStreamURL(plan.Key.Carrier, firstRegion(resolved.DeploymentRegions)),
	}
	// A pure-inbound package names a connection like every other telephony
	// target — that is where the route is written — but declares no environment
	// in it, because receiving a call on this route needs nothing from your
	// account. So the test is whether there is a key set to check, not whether
	// there is a connection. Whether an empty set is *allowed* was already
	// decided in ir.validateTelephonyEnvironment, against the same table.
	if len(plan.Environment) > 0 {
		if err := pipecatConnectionVocabulary(plan, env); err != nil {
			return nil, err
		}
		carrier.Connection = plan.Connection
		carrier.AccountSIDEnv = plan.Environment["account_sid"]
		carrier.AuthTokenEnv = plan.Environment["auth_token"]
		carrier.FromNumberEnv = plan.Environment["from_number"]
	}
	for _, evidence := range plan.Evidence {
		switch evidence.Feature {
		case "inbound":
			carrier.HasInbound = true
		case "outbound":
			carrier.HasOutbound = true
		}
	}
	// The organization is needed exactly when outbound TwiML has to name the
	// service host. The
	// plan already resolved that condition against the route row, so read it there
	// rather than re-deriving it (Principle III).
	if slices.Contains(plan.RequiredEnvironment, "PIPECAT_CLOUD_ORGANIZATION") {
		carrier.OrganizationEnv = "PIPECAT_CLOUD_ORGANIZATION"
		env.add(carrier.OrganizationEnv)
	}
	return carrier, nil
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
	// activate_worker to carry a developer message). MCP also stays on the bus so
	// startup, collision checks, and cleanup have one implementation.
	if len(data.Agents) != 1 || data.Tracing || data.NeedsMCP || data.HasColdTransfer {
		return false
	}
	// The carrier leg registers transport event handlers (the forward-once
	// dial-in-ready handler, the dial-out observers), which the inline shape has
	// no place for. The platform-terminated route reads the parsed handshake and
	// checks the call's environment before the conversation starts, which the
	// inline shape also has nowhere to put.
	if data.DailyCarrier != nil || data.CloudWebsocket != nil {
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
	data.NeedsEndFrame = data.NeedsEndAfter
	if data.Capture != nil {
		data.NeedsFunctionCalls = true // the generated capture tool is a @tool too
	}
	paramsClasses := map[string]bool{}
	for _, a := range data.Agents {
		if len(a.Tools)+len(a.Transfers)+len(a.Delegates) > 0 {
			data.NeedsFunctionCalls = true
		}
		for _, source := range a.MCPSources {
			data.NeedsMCP = true
			if source.Auth != nil {
				data.AuthKinds.add(source.Auth.Kind) // the same helper webhook auth uses (V8)
			}
			if class := source.ParamsClass(); class != "" {
				paramsClasses[class] = true
				continue
			}
			// No transport stated: the bot picks between the two at startup, so
			// it imports both (research R5).
			data.NeedsMCPChooser = true
			paramsClasses["SseServerParameters"] = true
			paramsClasses["StreamableHttpParameters"] = true
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
				if data.CloudWebsocket != nil {
					// The platform-terminated route needs EndFrame only when a failed
					// transfer is asked to hang up. A completed transfer ends the stream at
					// the carrier, so there is nothing here to end, and an EndFrame import
					// that nothing pushes fails the emitted project's own lint gate.
					data.NeedsEndFrame = t.HangupOnUnavailable || data.MaxDurationSecs > 0
				}
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
	data.MCPParamsImports = sortedKeys(paramsClasses)

	// pipecat.frames.frames names ride one merged import (V2), sorted at the end
	// so the merged import matches isort whatever order the flags are read in.
	if data.NeedsEndFrame {
		data.FrameImports = append(data.FrameImports, "EndFrame")
	}
	if data.HasFlows {
		// The delegate call resolves with run_llm=False (V7), and every
		// activation resets an agent that owns Flows to its owner prompt.
		data.FrameImports = append(data.FrameImports, "FunctionCallResultProperties", "LLMUpdateSettingsFrame")
	}
	if data.NeedsAppendFrame {
		data.FrameImports = append(data.FrameImports, "LLMMessagesAppendFrame")
	}
	needsTTSSpeakFrame := data.GreetingText != ""
	for _, agent := range data.Agents {
		for _, transfer := range agent.Transfers {
			if transfer.Announce != "" {
				data.HasTransferAnnouncements = true
				needsTTSSpeakFrame = true
			}
		}
		for _, delegate := range agent.Delegates {
			for _, task := range delegate.StepTasks {
				data.HasTaskTransfers = data.HasTaskTransfers || len(task.Transfers) > 0
				for _, transfer := range task.Transfers {
					if transfer.Announce != "" {
						data.HasTransferAnnouncements = true
						needsTTSSpeakFrame = true
					}
				}
			}
		}
	}
	if data.HasTransferAnnouncements {
		data.FrameImports = append(data.FrameImports, "BotStartedSpeakingFrame", "BotStoppedSpeakingFrame")
	}
	// An announcing tool queues the same frame from inside _direct_tool, so the
	// import has to survive a package whose only speech is a tool line. Both
	// tool lists are scanned: FlowTools is table-denied on Pipecat today, but the
	// scan costs one loop and keeps the import honest if that ever changes.
	for _, tools := range append([][]pipecatTool{data.FlowTools}, agentToolLists(data.Agents)...) {
		for _, tool := range tools {
			if tool.Announce != "" {
				data.HasToolAnnouncements = true
				needsTTSSpeakFrame = true
			}
		}
	}
	if needsTTSSpeakFrame {
		data.FrameImports = append(data.FrameImports, "TTSSpeakFrame")
	}
	slices.Sort(data.FrameImports)
	setDailyParams(data)
}

// setDailyParams picks the Daily route's transport classes and the one import
// that carries them. It runs last because the import depends on HasColdTransfer,
// and bot.py must import only what the package exercises.
func setDailyParams(data *pipecatData) {
	if data.Transport != "daily-sip" {
		return
	}
	// Pipecat's create_transport assigns an inbound call's dialin_settings,
	// api_key, and api_url straight onto whatever the params factory returns. The
	// generic TransportParams is a Pydantic model that declares none of the three
	// and allows no extras, so it raises and the call never connects. That was the
	// whole reason an inbound Daily call could not be answered. DailyParams
	// subclasses TransportParams and declares all three, so the kwargs already
	// passed keep working. Verified by instantiating both against pipecat-ai
	// 1.5.0 on 2026-08-12: the generic class raises ValueError, DailyParams
	// accepts.
	//
	// The import is the API reference's spelling, not the shorter one in the
	// telephony guides: 1.5.0 does not re-export the class from
	// pipecat.transports.daily, so the guides' spelling is an ImportError.
	// Confirmed by importing both paths from the installed package.
	params := &pipecatTransportParams{
		Class:  "DailyParams",
		Import: "from pipecat.transports.daily.transport import DailyParams",
	}
	// The transport class itself is needed for one reason: a transfer primitive is
	// a Daily transport method rather than a BaseTransport one, so the tool has to
	// narrow before calling it. Both classes come from the same module, so they
	// ride one import.
	if data.HasColdTransfer {
		params.Transport = "DailyTransport"
		params.Import = "from pipecat.transports.daily.transport import DailyParams, DailyTransport"
	}
	data.DailyParams = params
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
	if data.CloudWebsocket != nil {
		// This project terminates a carrier's WebSocket, so it declares the extra
		// that carries the machinery rather than inheriting fastapi from `runner`
		// (research D12/F10).
		extraSet["websocket"] = true
	}
	if data.NeedsMCP {
		// pipecat.services.mcp_service raises ImportError without it (N40).
		extraSet["mcp"] = true
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

// sortedKeys is the stdlib one-liner. internal/ir already writes it this way;
// this file used to disagree with it.
func sortedKeys(set map[string]bool) []string { return slices.Sorted(maps.Keys(set)) }

func buildPipecatAgent(agent *ir.Agent, target ir.Target, name string, def ir.AgentDef, env *envSet) (pipecatAgent, error) {
	promptConst := promptConstName(name)
	// A templated prompt is rendered per session from the call state; an untouched
	// one stays the bare module constant it always was.
	profile, router := slngRouterBinding(agent, target, def.Model)
	prompt := promptExpr(promptConst, def.Instructions, pipecatStateExpr, router)
	llm, err := resolvePipecatService(targetcap.Reason, target.Models.Reason[def.Model], env,
		pipecatSlngSite(agent, target, profile, targetcap.SlngSite{Kind: targetcap.SlngSiteAgent, Name: name}),
		pyKV{Key: "system_instruction", Value: prompt})
	if err != nil {
		return pipecatAgent{}, fmt.Errorf("agent %q: %w", name, err)
	}
	tts, err := ttsService(target.Models.Speak[def.Voice], env)
	if err != nil {
		return pipecatAgent{}, fmt.Errorf("agent %q: %w", name, err)
	}
	built := pipecatAgent{
		Name: name, Class: pyName(name) + "Agent", Prompt: def.Instructions,
		PromptConst: promptConst, PromptExpr: prompt,
		RuntimePromptExpr: promptExpr(promptConst, def.Instructions, "self.state", router),
		SlngHeaders:       pipecatRuntimeHeaders(target, profile, targetcap.SlngSite{Kind: targetcap.SlngSiteAgent, Name: name}),
		SlngBody:          pipecatRuntimeBody(agent, target, profile),
		LLM:               llm, TTS: tts,
	}

	for _, ref := range def.Tools {
		if tool, ok := agent.Tools[ref]; ok {
			// An mcp source is a server connection, not a @tool method: it must
			// never reach buildTool, whose fallback would POST to the MCP
			// address as if it were a webhook (N40).
			if tool.Execution == ir.ToolMCP {
				built.MCPSources = append(built.MCPSources, buildMCPSource(ref, tool, env))
				continue
			}
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
				MethodName: ref, To: c.To, When: transferReason(c),
				Announce: c.Announce, Reason: transferReason(c), Requires: c.Requires,
			})
		case *ir.HumanTransfer:
			tool, err := humanTransferTool(ref, name, c, target, env)
			if err != nil {
				return pipecatAgent{}, fmt.Errorf("agent %q: %w", name, err)
			}
			built.Tools = append(built.Tools, tool)
		case *ir.Delegate:
			delegate, err := buildDelegate(agent, target, ref, c, env)
			if err != nil {
				return pipecatAgent{}, fmt.Errorf("agent %q: %w", name, err)
			}
			built.Delegates = append(built.Delegates, delegate)
		}
	}
	flowNames := map[string]bool{}
	for _, delegate := range built.Delegates {
		for _, step := range delegate.StepTasks {
			flowNames[step.FinishName] = true
			for _, tool := range step.Tools {
				flowNames[tool.Name] = true
			}
			for _, transfer := range step.Transfers {
				flowNames[transfer.MethodName] = true
			}
		}
	}
	built.FlowFunctionNames = sortedKeys(flowNames)
	return built, nil
}

// buildDelegate lowers a delegate control to a Flow run on the owning worker
// (C8): a single task is a one-node flow, a group a linear chain. Each step is
// resolved here so the template emits its node inline.
func buildDelegate(agent *ir.Agent, tgt ir.Target, ref string, c *ir.Delegate, env *envSet) (pipecatDelegate, error) {
	delegate := pipecatDelegate{MethodName: ref, When: delegateReason(c)}
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
		task, err := buildTask(agent, tgt, step, agent.Tasks[step], env, "finish_"+ref+"_"+step)
		if err != nil {
			return pipecatDelegate{}, err
		}
		delegate.HasTransfers = delegate.HasTransfers || len(task.Transfers) > 0
		delegate.StepTasks = append(delegate.StepTasks, task)
	}
	for i := range delegate.StepTasks[:len(delegate.StepTasks)-1] {
		delegate.StepTasks[i].NextName = delegate.StepTasks[i+1].Name
		delegate.StepTasks[i].SlngNextHeaders = delegate.StepTasks[i+1].SlngHeaders
	}
	return delegate, nil
}

// buildTask lowers a task to a Flow-node model: instructions, tools, and the
// finish-function schema derived from the typed result (V1). The node runs on
// the owning agent's LLM; per-task model is gated (no LLMSwitcher, B7).
func buildTask(agent *ir.Agent, tgt ir.Target, name string, task ir.Task, env *envSet, finishName string) (pipecatTask, error) {
	if strings.TrimSpace(task.Instructions) == "" {
		return pipecatTask{}, fmt.Errorf("task %q instructions must not be empty", name)
	}
	taskProfile, taskRouter := slngRouterBinding(agent, tgt, task.Model)
	// The node's finish function is the only way out of the step, and its name is
	// per-step here, so the prompt has to name it (livekitTaskPrompt does the
	// same for a plain `finish`).
	prompt := task.Instructions + taskFinishContract(finishName, sortedResultNames(task.Result))
	built := pipecatTask{
		Name: name, FinishName: finishName, Prompt: prompt,
		// The node is built when the step is entered, not at session start, so a
		// prompt naming a variable an earlier task assigned renders with that
		// value. Left as a literal, the model would read "{{customer_id}}" and
		// have nothing to book with (B: multi-task, 2026-08-15).
		// A router-bound task ships its placeholders intact like any other router
		// prompt site: the flow node's role_message goes to the router as the
		// system message, through the owning agent's LLM.
		PromptExpr:     promptExpr(pyQuote(prompt), prompt, "self.state", taskRouter),
		ResultProps:    pyLiteral(resultProperties(task.Result)),
		ResultRequired: pyLiteral(anyStrings(sortedResultNames(task.Result))),
		// A task's prompt is not its owner's, so its cache scope is not its
		// owner's either. This is the value the emitted handlers swap in on the
		// way into the node and out of on every way back.
		SlngHeaders: pipecatRuntimeHeaders(tgt, taskProfile, targetcap.SlngSite{Kind: targetcap.SlngSiteTask, Name: name}),
	}
	for _, ref := range task.Tools {
		tool, ok := agent.Tools[ref]
		if !ok {
			control, exists := agent.Controls[ref]
			if !exists {
				return pipecatTask{}, fmt.Errorf("task %q references unknown tool/control %q", name, ref)
			}
			transfer, supported := control.(*ir.AgentTransfer)
			if !supported {
				return pipecatTask{}, fmt.Errorf("task %q references unsupported control %q: tasks support agent_transfer controls only", name, ref)
			}
			built.Transfers = append(built.Transfers, pipecatTransfer{
				MethodName: ref, To: transfer.To, When: transferReason(transfer),
				Announce: transfer.Announce, Reason: transferReason(transfer), Requires: transfer.Requires,
			})
			continue
		}
		// The capability table denies this combination, so a package never gets
		// here; an IR built in code still must not fall through to the webhook
		// lowering (N40).
		if tool.Execution == ir.ToolMCP {
			return pipecatTask{}, fmt.Errorf("task %q lists the MCP tool source %q: a Flows node advertises only its own function schemas, so list the source on the agent instead", name, ref)
		}
		built.Tools = append(built.Tools, buildTool(ref, tool, agent.Variables, env))
	}
	return built, nil
}

// resultProperties builds the finish function's JSON-schema properties from a
// task's typed result. Forwarded verbatim for nested schemas (C11).
//
// The reserved unserved-request property rides along, optional (it is absent
// from ResultRequired), so a step can hand back a request it cannot serve
// instead of refusing in place. ir.Validate rejects a task result that claims
// the name.
func resultProperties(result map[string]ir.ResultField) map[string]any {
	properties := map[string]any{
		ir.UnservedResultField: map[string]any{
			"type":        "string",
			"description": ir.UnservedResultDescription,
		},
	}
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
	slices.Sort(names)
	return names
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
	return orDefault(c.When, "Transfer the caller to the "+c.To+" agent.")
}

// delegateReason is the delegate's equivalent, and it exists for the same reason
// transferReason does: the docstring is the only thing the model reads when it
// decides whether to call the tool, so it can never be empty. It used to render
// straight from `when:`, which is optional, so every Pipecat delegate written
// without one emitted `""""""` — a control present in the file and unreachable
// at run time (Wave C, 2026-08-15).
// pipecatEndpointingDelay renders the turn binding's silence window as seconds
// for VADParams(stop_secs=...). Empty leaves SileroVADAnalyzer on its default.
func pipecatEndpointingDelay(binding *ir.Binding) string {
	if binding == nil || binding.EndpointingDelay == "" {
		return ""
	}
	duration, err := time.ParseDuration(string(binding.EndpointingDelay))
	if err != nil {
		return "" // ir.Validate already rejected it; never emit a broken literal
	}
	return strconv.FormatFloat(duration.Seconds(), 'f', -1, 64)
}

func delegateReason(c *ir.Delegate) string {
	return orDefault(c.When, "Run this flow. It returns its result to you when it finishes.")
}

// pipecatResultsHint is the developer message that hands a delegate's results
// back to the owning agent. LiveKit says the same thing in the delegate tool's
// docstring; here there is no tool to describe, so it rides the handback.
const pipecatResultsHint = " Continue with the caller in one short line. " + unservedOwnerRule

// pipecatStateExpr is how emitted Pipecat code reaches the call state: an agent
// @tool method has it on self, a flows handler receives it as a bound kwarg.
const pipecatStateExpr = "state"

// pipecatSessionIDExpr is the router session id inside a build_<agent>_llm call:
// a parameter, never shared state. The name is not `session_id`, because this
// template already reads runner_args.session_id for its Langfuse trace and that
// is a different thing.
const pipecatSessionIDExpr = "slng_session_id"

// pipecatRuntimeSessionIDExpr is the same value inside an agent method, where
// the constructor's parameter is out of scope, so the worker keeps it on self.
// Exactly the split RuntimePromptExpr already makes for a prompt: one value,
// two spellings, because a task's headers are swapped in from a method body and
// a whole header dict has to carry the session id along with the scope.
const pipecatRuntimeSessionIDExpr = "self._slng_session_id"

// buildMCPSource lowers one mcp tool source to its client. Both env names are
// registered, so the address and the token reach .env.example and the bot's
// REQUIRED_ENV startup check (FR-009).
func buildMCPSource(name string, tool ir.Tool, env *envSet) pipecatMCPSource {
	env.addRead(tool.URLEnv)
	source := pipecatMCPSource{
		Name: name, Var: name + "_mcp", URLEnv: tool.URLEnv,
		Transport: tool.MCPTransport, Tools: tool.MCPTools, Auth: loweredAuth(tool.Auth),
	}
	if tool.Auth != nil {
		env.addRead(tool.Auth.TokenEnv)
		source.AuthEnv = tool.Auth.TokenEnv
	}
	return source
}

func buildTool(name string, tool ir.Tool, variables map[string]ir.Variable, env *envSet) pipecatTool {
	if tool.URLEnv != "" {
		env.addRead(tool.URLEnv)
	}
	// The token rides its own env var, never the spec (SCHEMA §5.3).
	if tool.Auth != nil {
		env.addRead(tool.Auth.TokenEnv)
	}
	inject, needed := loweredInject(tool, variables, pipecatStateExpr)
	built := pipecatTool{
		Name: name, MethodName: name, Description: tool.Description, URLEnv: tool.URLEnv,
		URLExpr: urlExpr(tool, pipecatStateExpr), Inject: inject, Needed: needed,
		NeedsState: len(inject) > 0 || ir.HasTemplate(tool.Path),
		Auth:       loweredAuth(tool.Auth),
		Local:      tool.Execution == ir.ToolLocal, HandlerSource: tool.HandlerSource,
		Builtin: tool.Builtin, Instructions: tool.Instructions,
		KnowledgeBase: tool.KnowledgeBase,
		EndsCall:      tool.Effect == ir.ToolEndsConversation, Interruption: interruptionValue(tool.Interruption),
		Announce: tool.Announce,
	}
	if tool.Execution == ir.ToolKnowledge {
		// The tool owns its own schema, so the one parameter is fixed here rather
		// than derived from an input the author never wrote. Same shape as
		// LiveKit's, which is FR-026.
		built.Description = knowledgeDescription(tool.Description)
		built.Args = []pipecatArg{{
			Name: "query", PyType: "str", Required: true,
			Description: knowledgeQueryDescription,
		}}
		built.InputProps = pyLiteral(map[string]any{
			"query": map[string]any{"type": "string", "description": knowledgeQueryDescription},
		})
		built.InputRequired = pyLiteral([]any{"query"})
		return built
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
// Pipecat. Daily uses `sip_call_transfer`; cloud-websocket replaces the live
// carrier call's markup. Warm is rejected before this runs, and the error here
// is defense in depth for a gate bug.
func humanTransferTool(name, agent string, c *ir.HumanTransfer, target ir.Target, env *envSet) (pipecatTool, error) {
	_ = agent
	destination, ok := target.Destinations[c.Destination]
	if !ok {
		return pipecatTool{}, fmt.Errorf("human transfer %q destination %q missing on target %q", name, c.Destination, target.Name)
	}
	if c.Mode == ir.TransferWarm {
		if target.Telephony != nil && target.Telephony.Key.Transport == "sip" {
			// The self-hosted trunk route. The stack underneath it can do a warm
			// handoff, so the honest refusal says this project has not built it,
			// rather than implying a platform limit that does not exist.
			return pipecatTool{}, fmt.Errorf("human transfer %q: this project has not built warm transfer "+
				"on Pipecat yet. The stack this route runs on can do it: a warm handoff needs the agent to "+
				"hold both legs and merge them, and that is not built here. Warm transfer compiles on "+
				"(livekit, sip) trunks today. Change the transfer to cold, or run this package on a "+
				"LiveKit target", name)
		}
		return pipecatTool{}, fmt.Errorf("human transfer %q: this driver does not emit warm transfer yet (Daily documents the pattern; it needs the bot to own the call audio, tracked as feature 005); warm compiles on (livekit, sip) today", name)
	}
	if target.Telephony == nil {
		return pipecatTool{}, fmt.Errorf("human transfer %q requires an active channels.phone Connection: a web-only session has no existing SIP sessionId or phone leg to transfer", name)
	}
	// The Daily route carries the transfer after the operator's carrier call
	// arrives as a SIP dial-in participant in the same room. Daily documents
	// sipCallTransfer as working for dial-in legs with
	// SIP-to-SIP and SIP-to-PSTN both supported (research F4, 2026-08-12). Every
	// other Pipecat route still has no primitive at all.
	carrierLeg := target.Telephony.Key.Transport == "daily-sip"
	// The platform-terminated carrier route transfers by updating the live call's
	// TwiML at the carrier, keyed on its CallSid (research F5, D7). Different
	// primitive, same promise: the caller reaches a person and is never stranded
	// silently.
	cloudWebsocket := target.Telephony.Key.Transport == "cloud-websocket"
	if !carrierLeg && !cloudWebsocket {
		return pipecatTool{}, fmt.Errorf("human transfer %q: the (%s, %s) route has no transfer primitive; Pipecat cold transfer rides the Daily route (transport daily-sip) or the platform's carrier stream (transport cloud-websocket)", name, target.Telephony.Key.Transport, target.Telephony.Key.Carrier)
	}
	if !cloudWebsocket {
		// The carrier-backed Daily route mints or joins a room with this key. The
		// platform-terminated route touches no Daily API at all, so demanding the key
		// would make a working package fail its startup check on a value nothing reads.
		env.add("DAILY_API_KEY")
	}
	tool := pipecatTool{
		Name: name, MethodName: name,
		Description: orDefault(c.When, "Transfer the caller to a human."),
		// The carrier operation is claimed synchronously and cannot be safely
		// restarted if caller speech interrupts the tool while it is in flight.
		Interruption: "continue",
		// The author's ring_timeout, or the 25 seconds this template used to
		// hardcode. It was hardcoded past a declared value: writing
		// `ring_timeout: 7s` compiled green, emitted `timeout="25"`, and produced
		// output byte-identical to a package that declared nothing at all
		// (Wave C, 2026-08-15).
		RingTimeoutSecs: 25,
	}
	if secs := durationSecs(c.RingTimeout); secs > 0 {
		tool.RingTimeoutSecs = secs
	}
	switch c.Mode {
	case ir.TransferCold:
		tool.EndsCall = true
		tool.ColdDestination = pipecatDestinationExpr(destination, env)
		if carrierLeg {
			// On a carrier target every outbound leg goes through the operator's own
			// trunk, so the destination becomes a SIP URI at the trunk's termination
			// address (research F2). Composed at call time by _carrier_sip, because a
			// destination deferred to an environment variable has no value here.
			tool.ColdDestination = "_carrier_sip(" + tool.ColdDestination + ")"
		}
		tool.HangupOnUnavailable = c.OnUnavailable == ir.OnUnavailableHangup
		return tool, nil
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
		// interruption.enabled: false lowers to the aggregator's always-mute
		// strategy, which is Pipecat 1.5's mechanism for it (verified in the
		// built image: AlwaysUserMuteStrategy, "always mutes the user while the
		// bot is speaking"). The field used to be computed here and never
		// rendered, so an author who declared "the caller cannot barge in" got a
		// fully interruptible Pipecat agent while the LiveKit build from the same
		// source honoured it (Wave C, 2026-08-15).
		data.NeedsUserMute = !interrupt.Enabled
		if len(interrupt.IgnorePhrase) > 0 {
			data.Notes = append(data.Notes, "interruption ignore_phrases emitted as IGNORE_PHRASES; short phrases are also suppressed by the min-words turn-start strategy")
		}
	}
	if c.Inactivity != nil {
		data.Inactivity = &pipecatInactivity{NudgeSecs: durationSecs(c.Inactivity.NudgeAfter), EndSecs: durationSecs(c.Inactivity.EndAfter)}
	}
	data.MaxDurationSecs = durationSecs(c.MaxDuration)
	// _end_after is shared by the max-duration cap and the inactivity hangup, so
	// it is emitted when either asks for it. It used to be emitted for the cap
	// alone, and `inactivity.end_after` was computed and never rendered at all:
	// an idle call was nudged forever and never hung up (Wave C, 2026-08-15).
	data.NeedsEndAfter = data.MaxDurationSecs > 0 || (data.Inactivity != nil && data.Inactivity.EndSecs > 0)
}

// applyGreeting lowers greeting activation. With no greeting block the agent
// opens with a model-written line, the same default livekitGreetingFor picks
// (SCHEMA N20). Silence is never the default, because on a call it reads as a
// dead line (docs-site/reference/agent-yaml.mdx).
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

// resolvePipecatService resolves one binding through the catalogue.
// extraSettings are nested Settings args the driver injects (the agents'
// system_instruction); the task job-workers use the raw identity fields.
func resolvePipecatService(role targetcap.Role, binding ir.Binding, env *envSet, site slngSite, extraSettings ...pyKV) (pipecatService, error) {
	call, entry, err := resolveService(targetcap.Pipecat, role, binding, env, site, extraSettings...)
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
	// A router think service is the subclass that attaches the provenance hook to
	// the client it builds. Same constructor, same arguments; the only difference
	// is which create_client runs.
	if binding.Router() && role == targetcap.Reason {
		call.Class = slngRouterClass
	}
	svc := pipecatService{Call: call, Entry: entry, Model: binding.Model, BaseURL: binding.EndpointEnv,
		Vendor: cmp.Or(binding.Provider, "openai")}
	if spec := entry.Call; spec.APIKeyArg != "" {
		svc.APIKeyEnv = spec.APIKeyEnv
		if svc.APIKeyEnv == "" {
			svc.APIKeyEnv = apiKeyEnv(cmp.Or(binding.Provider, "openai"))
		}
	}
	return svc, nil
}

func sttService(binding *ir.Binding, env *envSet) (pipecatService, error) {
	if binding == nil {
		return pipecatService{}, fmt.Errorf("pipecat listen binding is missing a model")
	}
	return resolvePipecatService(targetcap.Listen, *binding, env, slngSite{})
}

// pipecatSlngSite is where the router's per-call values live on this target: a
// uuid created once in run_bot and passed as an argument, and the call state the
// agent already receives. Zero for a profile that is not a router binding.
//
// site names which prompt is speaking, and it is what makes the cache scope this
// site's own rather than the whole package's. This target builds one service per
// agent, so an agent's scope can sit on its own service at construction and never
// move; a task borrows its owner's service, so a task's scope is swapped in on
// the way in and back out on the way out.
func pipecatSlngSite(agent *ir.Agent, tgt ir.Target, profile string, site targetcap.SlngSite) slngSite {
	if profile == "" {
		return slngSite{}
	}
	return slngSite{
		SessionExpr: pipecatSessionIDExpr,
		StateExpr:   pipecatStateExpr,
		Names:       slngTemplateNames(agent, tgt, profile),
		ConfigFunc:  slngConfigFunc(profile),
		Scope:       targetcap.SlngScope(tgt.Models.Reason[profile].AgentID, site),
	}
}

// pipecatRuntimeBody is one prompt site's router body extension as a Python
// literal, spelled for an agent method body, so a settings delta can refresh the
// variable snapshot where the call writes a variable.
//
// Empty for a site that references no variable: there would be nothing in the
// dict but the model configuration, which never changes, and a delta that
// rewrites an unchanged value is a frame for nothing.
//
// The body only, never the headers. A settings update merges `extra` key by key,
// so `extra_headers` is replaced wholesale, and a refresh that carried it would
// hand whichever site is speaking the owner's scope.
func pipecatRuntimeBody(agent *ir.Agent, tgt ir.Target, profile string) string {
	if profile == "" {
		return ""
	}
	names := slngTemplateNames(agent, tgt, profile)
	if len(names) == 0 {
		return ""
	}
	site := slngSite{
		StateExpr:  pipecatRuntimeStateExpr,
		Names:      names,
		ConfigFunc: slngConfigFunc(profile),
	}
	return pyLiteral(slngRequestBody(site, slngPureProxy(tgt.Models.Reason[profile])))
}

// pipecatRuntimeStateExpr is the call state inside an agent method, where the
// constructor's parameter is out of scope. The same split RuntimePromptExpr and
// pipecatRuntimeSessionIDExpr already make: one value, two spellings.
const pipecatRuntimeStateExpr = "self.state"

// pipecatRuntimeHeaders is one prompt site's identity header dict as a Python
// literal, spelled for an agent method body rather than for a builder call.
// Empty for a profile that is not a router binding, so a non-router package
// emits nothing.
//
// The whole dict, not just the scope: a settings update merges the `extra` dict
// key by key, so `extra_headers` is replaced wholesale and a delta carrying only
// the scope would drop the session id from every request after it.
func pipecatRuntimeHeaders(tgt ir.Target, profile string, site targetcap.SlngSite) string {
	if profile == "" {
		return ""
	}
	return pyLiteral(slngRequestHeaders(slngSite{
		SessionExpr: pipecatRuntimeSessionIDExpr,
		Scope:       targetcap.SlngScope(tgt.Models.Reason[profile].AgentID, site),
	}))
}

// agentLLMService builds an agent's LLM; the prompt nests into Settings as
// system_instruction (the workers-model shape, driver-pipecat C2), referenced
// through its module constant so builder and restore share one copy (V2).
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
	return resolvePipecatService(targetcap.Speak, binding, env, slngSite{})
}

func forwardParams(params map[string]any) []pyKV {
	if len(params) == 0 {
		return nil
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]pyKV, 0, len(keys))
	for _, k := range keys {
		out = append(out, pyKV{Key: k, Value: pyLiteral(params[k])})
	}
	return out
}

// --- small helpers ---------------------------------------------------------

// pyLiteral renders a decoded YAML value as a Python literal.
// pyExpr is a value already written as a Python expression, so pyLiteral emits
// it verbatim instead of quoting it. It exists for the router's per-call values,
// which are variable names and helper calls rather than constants.
type pyExpr string

func pyLiteral(v any) string {
	switch value := v.(type) {
	case nil:
		return "None"
	case pyExpr:
		return string(value)
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
		slices.Sort(keys)
		pairs := make([]string, len(keys))
		for i, k := range keys {
			pairs[i] = strconv.Quote(k) + ": " + pyLiteral(value[k])
		}
		return "{" + strings.Join(pairs, ", ") + "}"
	default:
		return strconv.Quote(fmt.Sprintf("%v", value))
	}
}

func sortedAgentNames(agent *ir.Agent) []string {
	names := make([]string, 0, len(agent.Agents))
	for name := range agent.Agents {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func sortedVarNames(agent *ir.Agent) []string {
	names := make([]string, 0, len(agent.Variables))
	for name := range agent.Variables {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// envSet collects the environment names a generated project needs, and records
// which of them the emitted code reads on **every** session.
//
// The distinction exists because a name can be two things at once. A connection
// maps `auth_token` onto a variable; a webhook tool's `auth.token_env` may name
// the same variable, because one gateway token really does serve both. Deciding
// what a browser session needs by matching names against the connection then
// strips a credential the browser session genuinely reads, and the emitted code
// raises KeyError on the first tool call — after a startup check that passed
// (Wave C, 2026-08-15).
//
// So provenance is recorded rather than re-derived: `read` holds the names a
// model binding or a tool registered, and those are never treated as route
// names however a connection happens to map them.
type envSet struct {
	seen map[string]bool
	read map[string]bool
}

func newEnvSet() *envSet {
	return &envSet{seen: map[string]bool{}, read: map[string]bool{}}
}

func (e *envSet) add(name string) {
	if name != "" {
		e.seen[name] = true
	}
}

// addRead registers a name the emitted code reads directly, whatever channel the
// session arrived on.
func (e *envSet) addRead(name string) {
	if name != "" {
		e.seen[name] = true
		e.read[name] = true
	}
}

// alwaysRead reports whether the emitted code reads this name on every session.
func (e *envSet) alwaysRead(name string) bool { return e.read[name] }

func (e *envSet) sorted() []string {
	names := make([]string, 0, len(e.seen))
	for name := range e.seen {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
