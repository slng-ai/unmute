package ir

import (
	"cmp"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	targetcap "github.com/slng-ai/unmute/internal/target"
)

// envNamePattern is the UPPER_SNAKE shape every env var name in a package
// takes. Requiring upper case also catches a pasted secret or URL where a
// name belongs, which a mixed-case pattern would accept (compiler.md V36).
var envNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
var languagePattern = regexp.MustCompile(`^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{1,8})*$`)

type ValidateReport struct {
	PerTarget         []TargetValidation
	ForwardedBindings []ForwardedBinding
	Sizing            []Sizing
}

type TargetValidation struct {
	Name     string
	Provider Provider
	Errors   []string
	Warnings []string
	// Prerequisites are account features the provider grants on request that this
	// package's route needs. Neither errors nor warnings: unmute compiles the
	// package correctly and cannot know what the author's account is allowed to
	// do, so this states a fact about the route and never claims a failure
	// (research D3). Reported at exit 0.
	Prerequisites []targetcap.RouteAccountPrerequisite
}

type ForwardedBinding struct {
	Target  string           `json:"target"`
	Role    string           `json:"role"`
	Profile string           `json:"profile,omitempty"`
	Binding Binding          `json:"binding"`
	Params  []ForwardedParam `json:"-"` // sorted view for stdout; binding.params carries the same data in the report
}

type ForwardedParam struct {
	Name  string
	Value any
}

type Sizing struct {
	Target string `json:"target"`
	Metric string `json:"metric"`
	Value  string `json:"value"`
	Status string `json:"status"`
	Basis  string `json:"basis"`
}

// Validate checks structure and every selected target without short-circuiting.
func Validate(agent *Agent, targets []Target, caps targetcap.Table) (ValidateReport, error) {
	if agent == nil {
		return ValidateReport{}, fmt.Errorf("validate: nil agent")
	}
	if len(targets) == 0 {
		return ValidateReport{}, fmt.Errorf("validate: no targets selected")
	}
	global, globalWarnings := validateStructure(agent)
	global = append(global, validateConfiguredTargets(agent, caps)...)
	globalWarnings = add(globalWarnings, undeclaredSecretWarning(agent))
	globalWarnings = add(globalWarnings, unusedConnectionWarning(agent))
	report := ValidateReport{PerTarget: make([]TargetValidation, 0, len(targets))}
	failed := 0
	for _, resolved := range targets {
		row := TargetValidation{
			Name: resolved.Name, Provider: resolved.Provider,
			Errors:   append([]string(nil), global...),
			Warnings: append([]string(nil), globalWarnings...),
		}
		validateTarget(agent, resolved, caps, &row)
		report.ForwardedBindings = append(report.ForwardedBindings, forwardedBindings(resolved)...)
		report.Sizing = append(report.Sizing, sizing(agent, resolved)...)
		if len(row.Errors) > 0 {
			failed++
		}
		report.PerTarget = append(report.PerTarget, row)
	}
	if failed > 0 {
		return report, fmt.Errorf("validation failed for %d target(s)", failed)
	}
	return report, nil
}

func forwardedBindings(resolved Target) []ForwardedBinding {
	var result []ForwardedBinding
	appendBinding := func(role, profile string, binding *Binding) {
		if binding == nil {
			return
		}
		params := make([]ForwardedParam, 0, len(binding.Params))
		for _, name := range slices.Sorted(maps.Keys(binding.Params)) {
			params = append(params, ForwardedParam{Name: name, Value: binding.Params[name]})
		}
		result = append(result, ForwardedBinding{
			Target: resolved.Name, Role: role, Profile: profile, Binding: *binding, Params: params,
		})
	}
	appendBinding("listen", "", resolved.Models.Listen)
	for _, fallback := range resolved.Models.ListenFallbacks {
		binding := fallback.Binding
		appendBinding("listen", fallback.Name, &binding)
	}
	appendBinding("turn", "", resolved.Models.Turn)
	for _, name := range slices.Sorted(maps.Keys(resolved.Models.Speak)) {
		binding := resolved.Models.Speak[name]
		appendBinding("speak", name, &binding)
	}
	for _, name := range slices.Sorted(maps.Keys(resolved.Models.Reason)) {
		binding := resolved.Models.Reason[name]
		appendBinding("reason", name, &binding)
	}
	return result
}

func sizing(agent *Agent, resolved Target) []Sizing {
	if agent.Capacity == nil {
		return nil
	}
	// ponytail: conservative 1 session per worker/GPU; replace with dated benchmark coefficients when measured.
	workers := 0
	if targetcap.IsCode(targetcap.Provider(resolved.Provider)) {
		workers = agent.Capacity.MaxSessions
	}
	local := resolvedHasLocal(resolved)
	gpus := 0
	if local && targetcap.IsCode(targetcap.Provider(resolved.Provider)) {
		gpus = agent.Capacity.MaxSessions
	}
	channelKinds := make(map[string]bool)
	for _, channel := range agent.Channels {
		channelKinds[string(channel.Kind)] = true
	}
	basis := "2026-07-15 conservative 1 session per worker/GPU; channels=" + strings.Join(slices.Sorted(maps.Keys(channelKinds)), ",")
	result := []Sizing{
		{Target: resolved.Name, Metric: "workers", Value: fmt.Sprint(workers), Status: "unbenchmarked", Basis: basis},
		{Target: resolved.Name, Metric: "gpus", Value: fmt.Sprint(gpus), Status: "unbenchmarked", Basis: basis},
	}
	average, averageErr := time.ParseDuration(string(agent.Capacity.AvgSessionDuration))
	for _, kind := range slices.Sorted(maps.Keys(channelKinds)) {
		result = append(result, Sizing{
			Target: resolved.Name, Metric: "provider_concurrency_quota." + kind,
			Value: fmt.Sprint(agent.Capacity.PeakSessions), Status: "unbenchmarked", Basis: basis,
		})
		if averageErr == nil && average > 0 {
			result = append(result, Sizing{
				Target: resolved.Name, Metric: "provider_session_time_quota." + kind,
				Value: (time.Duration(agent.Capacity.PeakSessions) * average).String(), Status: "unbenchmarked", Basis: basis,
			})
		}
	}
	if channelKinds[string(ChannelTelephony)] {
		result = append(result, Sizing{
			Target: resolved.Name, Metric: "provider_call_start_rate.telephony",
			Value: fmt.Sprint(agent.Capacity.PeakStartsPerSecond), Status: "unbenchmarked", Basis: basis,
		})
	}
	return result
}

// resolvedHasLocal reports whether any GPU-bearing effective binding runs on
// local hardware, the GPU-sizing input (N15: sizing reads each target's
// effective models). Turn is excluded: a local turn model is CPU-side VAD, not
// a GPU workload, matching the pre-N15 sizing.
func resolvedHasLocal(resolved Target) bool {
	if b := resolved.Models.Listen; b != nil && b.Placement == PlacementLocal {
		return true
	}
	for _, fallback := range resolved.Models.ListenFallbacks {
		if fallback.Binding.Placement == PlacementLocal {
			return true
		}
	}
	for _, b := range resolved.Models.Speak {
		if b.Placement == PlacementLocal {
			return true
		}
	}
	for _, b := range resolved.Models.Reason {
		if b.Placement == PlacementLocal {
			return true
		}
	}
	return false
}

func validateConfiguredTargets(agent *Agent, caps targetcap.Table) []string {
	hasNestedResult := false
	for _, task := range agent.Tasks {
		for _, field := range task.Result {
			hasNestedResult = hasNestedResult || field.Schema != nil
		}
	}
	if !hasNestedResult {
		return nil
	}
	var errors []string
	for _, name := range slices.Sorted(maps.Keys(agent.Targets)) {
		resolved := agent.Targets[name]
		provider := targetcap.Provider(resolved.Provider)
		if !slices.Contains(targetcap.Providers, provider) {
			errors = add(errors, fmt.Sprintf("configured target %q has unknown provider %q", name, resolved.Provider))
			continue
		}
		capability := caps.Capability(targetcap.FieldTaskNestedResult, provider)
		if capability.Tag == targetcap.Gated || capability.Tag == targetcap.Provisional {
			errors = add(errors, fmt.Sprintf("configured target %q: %s", name, capability.Note))
		}
	}
	return errors
}

// validateStructure returns target-independent errors plus target-independent
// warnings. The warnings seed every target row, because a schema key is a
// property of the package, not of the target that compiles it.
func validateStructure(agent *Agent) (errors, warnings []string) {
	var schemas schemaReport
	if agent.Version != 1 {
		errors = add(errors, "version must be 1")
	}
	for _, name := range sortedKeys(agent.Models) {
		m := agent.Models[name]
		if m.Language == "" {
			continue
		}
		// language is a speak/listen field only (N16); on a think/turn model it
		// would be silently dropped at generate, so reject it here (fail loud).
		if m.Kind == KindThink || m.Kind == KindTurn {
			errors = add(errors, fmt.Sprintf("model %q is a %s model; language applies only to speak and listen models", name, m.Kind))
			continue
		}
		if !languagePattern.MatchString(m.Language) {
			errors = add(errors, fmt.Sprintf("model %q language must be a BCP-47 language tag such as en or en-US", name))
		}
	}
	if agent.Tracing != nil && agent.Tracing.Provider != "langfuse" {
		errors = add(errors, "tracing provider must be langfuse")
	}
	if len(agent.Models) == 0 {
		errors = add(errors, "models must contain at least one model")
	}
	if len(agent.Agents) == 0 {
		errors = add(errors, "agents must contain the entry agent")
	}
	if len(agent.Channels) == 0 {
		errors = add(errors, "channels must contain at least one channel")
	}
	for _, name := range slices.Sorted(maps.Keys(agent.Models)) {
		model := agent.Models[name]
		// A settings-only entry (integrated-role slot) has no derived placement.
		if model.Placement != "" && !validPlacement(model.Placement) {
			errors = add(errors, fmt.Sprintf("model %q placement must be api or local", name))
		}
		errors = append(errors, validateModelKind(name, model)...)
	}
	for name, variable := range agent.Variables {
		if !validPrimitive(variable.Type) {
			errors = add(errors, fmt.Sprintf("variable %q has invalid type %q", name, variable.Type))
		}
		if variable.Source != "" && !validVariableSource(variable.Source) {
			errors = add(errors, fmt.Sprintf("variable %q has invalid source %q", name, variable.Source))
		}
		if variable.Default != nil && !defaultMatches(variable.Type, variable.Default) {
			errors = add(errors, fmt.Sprintf("variable %q default does not match type %q", name, variable.Type))
		}
	}
	for name, task := range agent.Tasks {
		if len(task.Result) == 0 {
			errors = add(errors, fmt.Sprintf("task %q result must not be empty", name))
		}
		for fieldName, field := range task.Result {
			if field.Schema == nil && !validPrimitive(field.Type) {
				errors = add(errors, fmt.Sprintf("task %q result %q has invalid type %q", name, fieldName, field.Type))
			}
			if field.Enum != nil && len(field.Enum) == 0 {
				errors = add(errors, fmt.Sprintf("task %q result %q enum must not be empty", name, fieldName))
			}
			// A nested result field carries a raw schema (build.go stashes any
			// unrecognised map as ResultField.Schema), which the Pipecat driver
			// serialises through resultProperties/pyLiteral exactly the way it
			// serialises tool properties. Same unvalidated surface, so the same
			// walk applies.
			validateSchemaKeys(fmt.Sprintf("task %q result %q", name, fieldName), "schema", field.Schema, &schemas)
		}
		errors = append(errors, validateContextShape(name, task.Context)...)
	}
	for name, group := range agent.TaskGroups {
		if len(group.Steps) == 0 {
			errors = add(errors, fmt.Sprintf("task group %q steps must not be empty", name))
		}
		if group.ContextScope != ContextShared && group.ContextScope != ContextIsolated {
			errors = add(errors, fmt.Sprintf("task group %q context_scope must be shared or isolated", name))
		}
		switch group.Then {
		case GroupReturn, GroupEnd:
			if group.ThenTarget != "" {
				errors = add(errors, fmt.Sprintf("task group %q then_target is legal with transfer only", name))
			}
		case GroupTransfer:
			if group.ThenTarget == "" {
				errors = add(errors, fmt.Sprintf("task group %q then_target is required with transfer", name))
			}
		default:
			errors = add(errors, fmt.Sprintf("task group %q then must be return, transfer, or end", name))
		}
		if group.Merge != GroupMergeResults {
			errors = add(errors, fmt.Sprintf("task group %q merge must be results", name))
		}
	}
	for name, control := range agent.Controls {
		switch control := control.(type) {
		case *AgentTransfer:
			errors = append(errors, validateContextShape(name, control.Context.TaskContext)...)
			if !control.Context.Variables.All && len(control.Context.Variables.Names) == 0 {
				errors = add(errors, fmt.Sprintf("control %q context.variables is required", name))
			}
		case *HumanTransfer:
			if control.Mode != TransferCold && control.Mode != TransferWarm {
				errors = add(errors, fmt.Sprintf("control %q mode must be cold or warm", name))
			}
			if control.Briefing != "" && control.Mode != TransferWarm {
				errors = add(errors, fmt.Sprintf("control %q briefing is legal with warm transfer only", name))
			}
		case *Delegate:
		default:
			errors = add(errors, fmt.Sprintf("control %q has unknown union member %T", name, control))
		}
	}
	for name, tool := range agent.Tools {
		// builtin/instructions are legal only on a builtin tool (V2).
		if tool.Execution != ToolBuiltin {
			if tool.Builtin != "" {
				errors = add(errors, fmt.Sprintf("tool %q builtin is legal for builtin execution only", name))
			}
			if tool.Instructions != "" {
				errors = add(errors, fmt.Sprintf("tool %q instructions is legal for builtin execution only", name))
			}
		}
		// auth lives in the webhook or mcp block, so any other tool can only
		// carry one if the IR was built in code (tests, future drivers).
		if tool.Auth != nil && tool.Execution != ToolWebhook && tool.Execution != ToolMCP {
			errors = add(errors, fmt.Sprintf("tool %q auth is legal for webhook and mcp execution only", name))
		}
		if tool.Path != "" && tool.Execution != ToolWebhook {
			errors = add(errors, fmt.Sprintf("tool %q path is legal for webhook execution only", name))
		}
		if len(tool.Inject) > 0 {
			switch tool.Execution {
			case ToolWebhook, ToolLocal:
			default:
				errors = add(errors, fmt.Sprintf("tool %q inject is legal for webhook and local execution only", name))
			}
		}
		if tool.Execution == ToolBuiltin {
			validateBuiltinTool(name, tool, &errors)
			continue
		}
		// An mcp file carries no per-tool contract at all: the server announces
		// each tool at run time (N40). spec.Load rejects the fields with a line
		// number, so reaching here means the IR was built in code.
		if tool.Execution != ToolMCP {
			if tool.Description == "" {
				errors = add(errors, fmt.Sprintf("tool %q description is required", name))
			}
			if tool.Input["type"] != "object" {
				errors = add(errors, fmt.Sprintf("tool %q input must be a JSON Schema object", name))
			}
			validateSchemaKeys(fmt.Sprintf("tool %q", name), "input", tool.Input, &schemas)
			if tool.Output != nil && tool.Output["type"] != "object" {
				errors = add(errors, fmt.Sprintf("tool %q output must be a JSON Schema object", name))
			}
			validateSchemaKeys(fmt.Sprintf("tool %q", name), "output", tool.Output, &schemas)
		}
		if tool.Execution != ToolMCP && (tool.MCPTransport != "" || len(tool.MCPTools) > 0) {
			errors = add(errors, fmt.Sprintf("tool %q transport and tools are legal for mcp execution only", name))
		}
		switch tool.Execution {
		case ToolLocal:
			if tool.Handler == "" {
				errors = add(errors, fmt.Sprintf("tool %q handler is required for local execution", name))
			}
			if tool.URLEnv != "" {
				errors = add(errors, fmt.Sprintf("tool %q url_env is legal for webhook execution only", name))
			}
		case ToolWebhook:
			if tool.URLEnv == "" {
				errors = add(errors, fmt.Sprintf("tool %q url_env is required for webhook execution", name))
			} else if !envNamePattern.MatchString(tool.URLEnv) {
				errors = add(errors, fmt.Sprintf("tool %q url_env must be an UPPER_SNAKE environment variable name", name))
			}
			if tool.Handler != "" {
				errors = add(errors, fmt.Sprintf("tool %q handler is legal for local execution only", name))
			}
			validateToolAuth(name, tool.Auth, &errors)
		case ToolMCP:
			// N40: the server owns every tool's contract. spec.Load rejects
			// these with a line number, so reaching here means the IR was built
			// in code.
			if tool.Description != "" || tool.Input != nil || tool.Output != nil {
				errors = add(errors, fmt.Sprintf("tool %q mcp execution takes no description, input, or output: the server describes its own tools", name))
			}
			// B3 (SCHEMA §5, 2026-07-16): url_env names the MCP server address.
			if tool.URLEnv == "" {
				errors = add(errors, fmt.Sprintf("tool %q url_env is required for mcp execution (the MCP server address env)", name))
			} else if !envNamePattern.MatchString(tool.URLEnv) {
				errors = add(errors, fmt.Sprintf("tool %q url_env must be an UPPER_SNAKE environment variable name", name))
			}
			if tool.Handler != "" {
				errors = add(errors, fmt.Sprintf("tool %q handler is legal for local execution only", name))
			}
			// N40: the two remote transports, and only when the author states
			// one. Absent means the platform's own rule for the URL.
			switch tool.MCPTransport {
			case "", MCPTransportSSE, MCPTransportStreamableHTTP:
			default:
				errors = add(errors, fmt.Sprintf("tool %q transport must be %s or %s, not %q",
					name, MCPTransportSSE, MCPTransportStreamableHTTP, tool.MCPTransport))
			}
			// The server's tool list only exists at run time, so a name is
			// checked for shape here and for existence never: an empty or
			// repeated entry is an authoring slip worth naming, a name the
			// server does not expose is simply never offered.
			seen := make(map[string]bool, len(tool.MCPTools))
			for _, selected := range tool.MCPTools {
				switch {
				case strings.TrimSpace(selected) == "":
					errors = add(errors, fmt.Sprintf("tool %q tools has an empty entry: name a tool the server exposes, or drop the list to take them all", name))
				case seen[selected]:
					errors = add(errors, fmt.Sprintf("tool %q tools names %q twice", name, selected))
				}
				seen[selected] = true
			}
			validateToolAuth(name, tool.Auth, &errors)
		case ToolClient, ToolProviderHosted:
			if tool.Handler != "" || tool.URLEnv != "" {
				errors = add(errors, fmt.Sprintf("tool %q handler/url_env does not match execution %q", name, tool.Execution))
			}
		default:
			errors = add(errors, fmt.Sprintf("tool %q has invalid execution %q", name, tool.Execution))
		}
		// The two conversation scalars are not part of an mcp file either
		// (N40), so an mcp source is not asked to carry a value for them.
		if tool.Execution == ToolMCP {
			continue
		}
		switch tool.Interruption {
		case ToolContinue, ToolCancel, ToolProviderDefault:
		default:
			errors = add(errors, fmt.Sprintf("tool %q has invalid interruption %q", name, tool.Interruption))
		}
		switch tool.Effect {
		case ToolReturnsData, ToolEndsConversation:
		default:
			errors = add(errors, fmt.Sprintf("tool %q has invalid effect %q", name, tool.Effect))
		}
	}
	for name, channel := range agent.Channels {
		for _, control := range channel.RequiredControls {
			if !validRequiredControl(control) {
				errors = add(errors, fmt.Sprintf("channel %q has unknown required_control %q", name, control))
			}
		}
		switch channel.Kind {
		case ChannelRealtimeAudio:
			if channel.Inbound != nil || channel.Outbound != nil || len(channel.RequiredControls) > 0 || channel.OnVoicemail != "" {
				errors = add(errors, fmt.Sprintf("channel %q telephony fields require kind telephony", name))
			}
		case ChannelTelephony:
			if channel.Inbound == nil || channel.Outbound == nil {
				errors = add(errors, fmt.Sprintf("channel %q inbound and outbound are required", name))
			}
			if channel.OnVoicemail != "" && (channel.Outbound == nil || !*channel.Outbound) {
				errors = add(errors, fmt.Sprintf("channel %q on_voicemail requires outbound: true", name))
			}
			if channel.OnVoicemail != "" && channel.OnVoicemail != VoicemailHangup && channel.OnVoicemail != VoicemailLeaveMessage {
				errors = add(errors, fmt.Sprintf("channel %q on_voicemail must be hangup or leave_message", name))
			}
		default:
			errors = add(errors, fmt.Sprintf("channel %q kind must be realtime_audio or telephony", name))
		}
	}
	if agent.Conversation != nil {
		if greeting := agent.Conversation.Greeting; greeting != nil {
			if greeting.SpeaksFirst != SpeaksFirstAgent && greeting.SpeaksFirst != SpeaksFirstUser {
				errors = add(errors, "conversation.greeting.speaks_first must be agent or user")
			}
			if greeting.SpeaksFirst == SpeaksFirstUser && greeting.Text != "" {
				errors = add(errors, "conversation.greeting.text requires speaks_first: agent")
			}
		}
		switch agent.Conversation.ThinkingAudio {
		case "", ThinkingNone, ThinkingSubtle:
		default:
			errors = add(errors, "conversation.thinking_audio must be none or subtle")
		}
		if value := agent.Conversation.MaxDuration; value != "" {
			errors = append(errors, validateDuration("conversation.max_duration", value)...)
		}
		if inactivity := agent.Conversation.Inactivity; inactivity != nil {
			if inactivity.NudgeAfter != "" {
				errors = append(errors, validateDuration("conversation.inactivity.nudge_after", inactivity.NudgeAfter)...)
			}
			if inactivity.EndAfter != "" {
				errors = append(errors, validateDuration("conversation.inactivity.end_after", inactivity.EndAfter)...)
			}
		}
		if interruption := agent.Conversation.Interruption; interruption != nil && interruption.Enabled == nil {
			errors = add(errors, "conversation.interruption.enabled is required")
		}
	}
	return errors, schemas.notes
}

// validateDriverValues asks the value questions a driver used to ask alone.
//
// Each of these let a package exit 0 from `unmute validate` and 1 from
// `unmute compile`, on a value the author wrote, with a bare message carrying no
// target prefix and no position — after validate had already said the package
// was fine. Eight were measured (reproduction.md section E) and the constitution
// does not license the gap: Principle V's "validate is wider than generation" is
// about the **provider set**, not about depth, and the repository had already
// made this exact decision for a sibling field at the language-slot check above.
//
// Moving a check from generate to validate buys "before any artifact is
// written". It does not buy a file and line: only spec.Load and ir.Build carry a
// position (research D7). The generators keep their own errors as a backstop.
func validateDriverValues(resolved Target, provider targetcap.Provider, row *TargetValidation) {
	if err := targetcap.CheckSDKLanguage(provider, resolved.SDKLanguage); err != nil {
		row.Errors = add(row.Errors, err.Error())
	}
	if err := targetcap.CheckPins(provider, resolved.Pins); err != nil {
		row.Errors = add(row.Errors, err.Error())
	}
	// An absent version is already reported above, with its own wording.
	if resolved.Version != "" {
		if err := targetcap.CheckVersion(provider, resolved.Version); err != nil {
			row.Errors = add(row.Errors, err.Error())
		}
	}
	if provider == targetcap.LiveKit && resolved.Models.Turn != nil {
		if _, err := targetcap.LiveKitTurnVersion(resolved.Models.Turn.Model); err != nil {
			row.Errors = add(row.Errors, err.Error())
		}
	}
}

func validateTarget(agent *Agent, resolved Target, caps targetcap.Table, row *TargetValidation) {
	provider := targetcap.Provider(resolved.Provider)
	if !slices.Contains(targetcap.Providers, provider) {
		row.Errors = add(row.Errors, fmt.Sprintf("unknown provider %q", resolved.Provider))
		return
	}
	if targetcap.IsCode(provider) && resolved.Version == "" {
		row.Errors = add(row.Errors, fmt.Sprintf("%s code target requires version", resolved.Provider))
	}
	validateDriverValues(resolved, provider, row)
	// A feature whose emitted code needs a newer framework than the target
	// declares. This used to be applied by quietly raising the emitted
	// constraint, so the package installed a version it never named; now the
	// declared version is the pin and the mismatch is a gated error instead.
	if err := targetcap.CheckFeatureFloors(provider, resolved.Version, UsedFrameworkFeatures(agent)); err != nil {
		row.Errors = add(row.Errors, err.Error())
	}
	if agent.Tracing != nil {
		applyCapability(caps, targetcap.FieldTracingLangfuse, provider, row)
	}
	row.Errors = append(row.Errors, validateRegions(resolved.DeploymentRegions)...)
	// Only a list of more than one is gated: one region works everywhere the
	// field works, and the scalar form has since N18.
	if len(resolved.DeploymentRegions) > 1 {
		applyCapability(caps, targetcap.FieldDeploymentMultiRegion, provider, row)
	}
	// Placement gates read the resolved per-target bindings (N15): a per-target
	// override can change where a model runs, so the effective binding decides.
	if b := resolved.Models.Listen; b != nil && b.Placement == PlacementLocal {
		applyCapability(caps, targetcap.FieldListenLocal, provider, row)
	}
	for _, fallback := range resolved.Models.ListenFallbacks {
		if fallback.Binding.Placement == PlacementLocal {
			applyCapability(caps, targetcap.FieldListenLocal, provider, row)
		}
	}
	if len(resolved.Models.ListenFallbacks) > 0 {
		applyCapability(caps, targetcap.FieldListenFallback, provider, row)
	}
	if b := resolved.Models.Turn; b != nil {
		applyCapability(caps, targetcap.FieldTurnPlacement, provider, row)
		if b.SemanticEndpointing != "" {
			applyCapability(caps, targetcap.FieldSemanticEndpointing, provider, row)
		}
	}
	for _, b := range resolved.Models.Speak {
		if b.Placement == PlacementLocal {
			applyCapability(caps, targetcap.FieldSpeakLocal, provider, row)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(resolved.Models.Reason)) {
		b := resolved.Models.Reason[name]
		if b.Placement == PlacementLocal {
			applyCapabilityValue(caps.CapabilityForValue(targetcap.FieldReasonLocal, provider, b.EndpointEnv), string(targetcap.FieldReasonLocal), provider, row)
		}
	}
	validateBindings(agent, resolved, caps, row)
	validateFallbacks(agent, resolved, caps, row)

	taskContexts := taskContextUsage(agent)
	for name, task := range agent.Tasks {
		if task.Model != "" {
			applyCapability(caps, targetcap.FieldTaskModel, provider, row)
		}
		if taskContexts[name] {
			validateContext(task.Context, provider, caps, row)
		}
	}
	if len(agent.TaskGroups) > 0 {
		applyCapability(caps, targetcap.FieldTaskGroup, provider, row)
	}
	for _, group := range agent.TaskGroups {
		if group.Then == GroupReturn {
			applyCapability(caps, targetcap.FieldTaskGroupReturn, provider, row)
		}
		if group.ContextScope == ContextIsolated {
			applyCapability(caps, targetcap.FieldContextIsolated, provider, row)
		}
	}
	for _, control := range agent.Controls {
		switch control := control.(type) {
		case *Delegate:
			if control.Task != "" {
				applyCapability(caps, targetcap.FieldTask, provider, row)
			}
		case *AgentTransfer:
			if control.Announce != "" {
				applyCapability(caps, targetcap.FieldTransferAnnounce, provider, row)
			}
			if len(control.Requires) > 0 {
				applyCapability(caps, targetcap.FieldTransferRequires, provider, row)
			}
			validateContext(control.Context.TaskContext, provider, caps, row)
			if !control.Context.Variables.All {
				applyCapability(caps, targetcap.FieldContextVariableSubset, provider, row)
			}
		case *HumanTransfer:
			validateHumanTransfer(control, resolved, provider, caps, row)
		}
	}
	validateTools(agent, resolved, provider, caps, row)
	validateVariables(agent, provider, caps, row)
	validateConversation(agent.Conversation, provider, caps, row)
	validateTelephonyPlan(resolved.Telephony, row)
	validateCapacity(agent, resolved, provider, row)
	validateChannels(agent, resolved, provider, caps, row)
	validateRoutePrerequisites(agent, resolved, provider, row)
}

func validateRoutePrerequisites(agent *Agent, resolved Target, provider targetcap.Provider, row *TargetValidation) {
	row.Prerequisites = RoutePrerequisites(agent, resolved, provider)
}

// RoutePrerequisites returns the account features this package's route needs that
// the provider grants on request.
//
// One function, two callers: `validate` reports these and the emitters carry them
// into the generated project. Going through the same door is what stops the
// command and the artifact disagreeing about what an account has to be allowed to
// do (Principle III).
//
// Keyed on the route triple rather than on the resolved telephony plan, because
// the Daily route has no plan: it declares no connection and no telephony
// channel, so every other route fact is unreachable there.
func RoutePrerequisites(agent *Agent, resolved Target, provider targetcap.Provider) []targetcap.RouteAccountPrerequisite {
	candidates := targetcap.RouteAccountPrerequisites(provider, resolved.Transport, resolved.Carrier)
	if len(candidates) == 0 {
		return nil
	}
	used := routeCapabilitiesUsed(agent)
	var applies []targetcap.RouteAccountPrerequisite
	for _, prerequisite := range candidates {
		if prerequisite.Needs(used) {
			applies = append(applies, prerequisite)
		}
	}
	return applies
}

// routeCapabilitiesUsed names the route capabilities this package exercises. It
// is what keeps a prerequisite from becoming a standing banner: a package that
// needs none of them must not be told about one.
func routeCapabilitiesUsed(agent *Agent) []targetcap.TelephonyFeature {
	var used []targetcap.TelephonyFeature
	note := func(feature targetcap.TelephonyFeature) {
		if !slices.Contains(used, feature) {
			used = append(used, feature)
		}
	}
	for _, control := range agent.Controls {
		transfer, ok := control.(*HumanTransfer)
		if !ok {
			continue
		}
		if transfer.Mode == TransferWarm {
			note(targetcap.TelephonyFeature(targetcap.WarmTransfer))
			continue
		}
		note(targetcap.TelephonyFeature(targetcap.ColdTransfer))
	}
	for _, channel := range agent.Channels {
		if channel.Kind != ChannelTelephony {
			continue
		}
		if channel.Inbound != nil && *channel.Inbound {
			note(targetcap.TelephonyInbound)
		}
		if channel.Outbound != nil && *channel.Outbound {
			note(targetcap.TelephonyOutbound)
		}
	}
	return used
}

func validateContext(context TaskContext, provider targetcap.Provider, caps targetcap.Table, row *TargetValidation) {
	if context.History == "" {
		return
	}
	support := caps.HistorySupport(targetcap.History(context.History), provider)
	switch support.Kind {
	case "":
		row.Errors = add(row.Errors, fmt.Sprintf("unknown context history %q", context.History))
	case targetcap.HistoryFail:
		row.Errors = add(row.Errors, support.Note)
	}
	if context.IncludeToolCalls != nil && !*context.IncludeToolCalls {
		applyCapability(caps, targetcap.FieldContextNoToolCalls, provider, row)
	}
	if context.History == HistorySummary && context.Summarizer == "" {
		row.Errors = add(row.Errors, "context history summary requires a summarizer profile")
	}
}

// validateBuiltinTool checks a prebuilt tool: known id (V1), no webhook/local
// fields (V3), and an effect that matches the registry (V5). The provider gate
// (FieldToolBuiltin) is applied per target in validateTools (V4).
// knownSchemaKeywords is what unmute recognises in a tool schema. It decides
// whether an unrecognised key is worth mentioning; it never decides validity.
//
// JSON Schema is deliberately open: an implementation must ignore keywords it
// does not know, and vendors rely on that (OpenAPI's `discriminator`, `x-`
// extensions, whatever a provider adds next). SCHEMA.md N10 inherits that
// openness by calling tool `input` "a JSON Schema object" without narrowing it,
// and D10 says forwarded values are never validated because the provider is the
// real validator. So a closed allow-list would reject schemas this repo's own
// contract permits, and would rot every time a draft or a vendor adds a
// keyword. Unrecognised keys therefore warn rather than fail.
//
// The list is generous on purpose, spanning draft-07 through 2020-12 plus the
// OpenAPI dialect, so that using a real keyword stays quiet.
var knownSchemaKeywords = map[string]struct{}{
	// core and identifiers, draft-07 through 2020-12
	"$anchor": {}, "$comment": {}, "$defs": {}, "$dynamicAnchor": {}, "$dynamicRef": {},
	"$id": {}, "$recursiveAnchor": {}, "$recursiveRef": {}, "$ref": {}, "$schema": {},
	"$vocabulary": {}, "definitions": {}, "id": {},
	// applicators
	"additionalItems": {}, "additionalProperties": {}, "allOf": {}, "anyOf": {},
	"contains": {}, "dependencies": {}, "dependentSchemas": {}, "else": {}, "if": {},
	"items": {}, "not": {}, "oneOf": {}, "patternProperties": {}, "prefixItems": {},
	"properties": {}, "propertyNames": {}, "then": {}, "unevaluatedItems": {},
	"unevaluatedProperties": {},
	// validation
	"const": {}, "dependentRequired": {}, "enum": {}, "exclusiveMaximum": {},
	"exclusiveMinimum": {}, "maxContains": {}, "maxItems": {}, "maxLength": {},
	"maxProperties": {}, "maximum": {}, "minContains": {}, "minItems": {},
	"minLength": {}, "minProperties": {}, "minimum": {}, "multipleOf": {},
	"pattern": {}, "required": {}, "type": {}, "uniqueItems": {},
	// annotations and content
	"contentEncoding": {}, "contentMediaType": {}, "contentSchema": {},
	"default": {}, "deprecated": {}, "description": {}, "example": {}, "examples": {},
	"format": {}, "readOnly": {}, "title": {}, "writeOnly": {},
	// OpenAPI dialect, which several provider tool APIs accept verbatim
	"discriminator": {}, "externalDocs": {}, "nullable": {}, "xml": {},
}

// schemaKeyLooksLikeYAMLAccident reports a valueless key containing whitespace,
// which is what an unquoted comma inside a YAML flow mapping leaves behind: the
// parser ends the previous entry at the comma and reads the remaining prose as
// a bare key. `description: The requested date, e.g. 2026-08-14` becomes a
// truncated description plus a null-valued key `e.g. 2026-08-14`, the shape
// that shipped in a fixture and reached generated Python.
//
// This only selects a better warning message. It is deliberately not a failure
// condition, because the shape is legal: a JSON Schema member name is an
// unrestricted string and unknown keywords must be ignored whatever their value,
// so `{"e.g. 2026-08-14": null}` is a valid schema. No key-shape rule can catch
// this accident without also rejecting something valid, and N10 plus D10 say
// unmute does not get to reject it.
func schemaKeyLooksLikeYAMLAccident(key string, value any) bool {
	return value == nil && strings.ContainsFunc(key, unicode.IsSpace)
}

// schemaKeyIsExtension reports a vendor extension, which is valid, expected,
// and not worth a warning: `x-`/`X-` per OpenAPI, and `$`-prefixed per JSON
// Schema's own reserved space.
func schemaKeyIsExtension(key string) bool {
	return strings.HasPrefix(key, "x-") || strings.HasPrefix(key, "X-") || strings.HasPrefix(key, "$")
}

// schemaMapKeywords hold a map of author-chosen name to subschema. Only the
// values are schemas; the keys are the author's own property names and must
// never be checked against the vocabulary.
// draft-07's `dependencies` belongs here too: its values are either a subschema
// or a plain string list, and validateSchemaKeysValue handles both.
var schemaMapKeywords = []string{
	"$defs", "definitions", "dependencies", "dependentSchemas", "patternProperties", "properties",
}

// schemaValueKeywords hold a subschema directly, or a list of them. Anything
// not listed here is left alone on purpose: `default`, `const`, `enum`, and
// `examples` carry author data that may itself be an object, and recursing into
// those would report a data key as an unknown schema keyword.
var schemaValueKeywords = []string{
	"additionalItems", "additionalProperties", "allOf", "anyOf", "contains",
	"contentSchema", "else", "if", "items", "not", "oneOf", "prefixItems",
	"propertyNames", "then", "unevaluatedItems", "unevaluatedProperties",
}

// schemaReport collects what a schema walk found. It carries notes only, and
// that is the point: every note becomes a warning on stderr with exit 0, never
// a failure. JSON Schema requires unknown keywords to be ignored, N10 inherits
// that openness by calling tool input "a JSON Schema object" without narrowing
// it, and D10 says forwarded values are never validated because the provider is
// the real validator. Any key-shape rule strong enough to reject the accident
// below would also reject a schema those three permit, so unmute reports rather
// than refusing.
//
// The note says what unmute knows ("we do not read this") and stops there. It
// deliberately makes no claim about whether the key survives into the emitted
// project, because that answer needs three axes, not one: LiveKit reads five
// named keys per property and drops the rest everywhere; Pipecat drops them too
// except on the Flow-node path, where buildTool hands the whole `properties` map
// to pyLiteral and it lands in bot.py verbatim; and a key at the top level of
// `input` is dropped by both. "Depends on the driver" would be wrong often
// enough to mislead, so the matrix lives in SCHEMA.md N21 where it fits.
type schemaReport struct {
	notes []string
}

// validateSchemaKeys walks a raw schema and describes what it cannot vouch
// for. Nothing here fails a build. subject names the owner for the message
// (`tool "x"`, `task "y" result "z"`), since tool input/output and task result
// schemas share this unvalidated surface.
//
// path starts at "input" or "output" and grows into the offending location, so
// each message is unique: add() de-duplicates, and a bare key name would
// collapse the same typo made in two different properties into one line.
func validateSchemaKeys(subject, path string, schema map[string]any, report *schemaReport) {
	for _, key := range slices.Sorted(maps.Keys(schema)) {
		switch {
		case schemaKeyIsExtension(key):
			// vendor space, forwarded untouched
		case schemaKeyLooksLikeYAMLAccident(key, schema[key]):
			report.notes = add(report.notes, fmt.Sprintf(
				"%s has an empty schema key %q at %s; an unquoted comma in a YAML flow mapping splits the entry, so quote the value if that text belongs to it",
				subject, key, path))
		default:
			if _, ok := knownSchemaKeywords[key]; !ok {
				report.notes = add(report.notes, fmt.Sprintf(
					"%s has unrecognised schema key %q at %s; unmute does not read it",
					subject, key, path))
			}
		}
		switch {
		case slices.Contains(schemaMapKeywords, key):
			named, ok := schema[key].(map[string]any)
			if !ok {
				continue
			}
			for _, name := range slices.Sorted(maps.Keys(named)) {
				validateSchemaKeysValue(subject, path+"."+key+"."+name, named[name], report)
			}
		case slices.Contains(schemaValueKeywords, key):
			validateSchemaKeysValue(subject, path+"."+key, schema[key], report)
		}
	}
}

// validateSchemaKeysValue descends one subschema position, which JSON Schema
// allows to be a schema, a list of schemas, or a bare bool (`items: false`).
func validateSchemaKeysValue(subject, path string, value any, report *schemaReport) {
	switch typed := value.(type) {
	case map[string]any:
		validateSchemaKeys(subject, path, typed, report)
	case []any:
		for i, item := range typed {
			validateSchemaKeysValue(subject, fmt.Sprintf("%s[%d]", path, i), item, report)
		}
	}
}

func validateBuiltinTool(name string, tool Tool, errors *[]string) {
	if tool.Input != nil || tool.Output != nil || tool.Handler != "" || tool.URLEnv != "" {
		*errors = add(*errors, fmt.Sprintf("tool %q builtin execution takes no input, output, handler, or url_env", name))
	}
	if tool.Builtin == "" {
		*errors = add(*errors, fmt.Sprintf("tool %q builtin execution requires a builtin id", name))
		return
	}
	prebuilt, ok := targetcap.LookupPrebuilt(tool.Builtin)
	if !ok {
		*errors = add(*errors, fmt.Sprintf("tool %q has unknown builtin %q", name, tool.Builtin))
		return
	}
	if tool.Effect != ToolEffect(prebuilt.Effect) {
		*errors = add(*errors, fmt.Sprintf("tool %q builtin %q fixes effect to %s, cannot be %q", name, tool.Builtin, prebuilt.Effect, tool.Effect))
	}
}

// validateToolAuth checks a webhook auth block: a known scheme, exactly its own
// fields, and an env name rather than a literal token (SCHEMA §5.3).
func validateToolAuth(name string, auth *ToolAuth, errors *[]string) {
	if auth == nil {
		return
	}
	fail := func(format string, args ...any) {
		*errors = add(*errors, fmt.Sprintf("tool %q auth "+format, append([]any{name}, args...)...))
	}
	switch auth.Type {
	case ToolAuthBearer:
		// header belongs to api_key, so a copy-paste between the two schemes
		// fails instead of being silently ignored.
		if auth.Header != "" {
			fail("header is not a bearer field: bearer always sends Authorization")
		}
	case ToolAuthAPIKey:
		if auth.Header == "" {
			fail("header is required for api_key")
		}
	case "":
		fail("type is required: bearer or api_key")
		return
	default:
		fail("type must be bearer or api_key, not %q", auth.Type)
		return
	}
	switch {
	case auth.TokenEnv == "":
		fail("token_env is required for %s", auth.Type)
	case !envNamePattern.MatchString(auth.TokenEnv):
		fail("token_env must be an UPPER_SNAKE environment variable name, never a secret value")
	}
}

func validateContextShape(owner string, context TaskContext) []string {
	var errors []string
	switch context.History {
	case HistoryFull, HistoryMessages, HistoryLastN, HistorySummary, HistoryReset:
	case "":
		errors = add(errors, fmt.Sprintf("%q context.history is required", owner))
	default:
		errors = add(errors, fmt.Sprintf("%q context.history must be full, messages, last_n, summary, or reset", owner))
	}
	if context.History == HistoryLastN {
		if context.MaxMessages <= 0 {
			errors = add(errors, fmt.Sprintf("%q context.max_messages must be positive with last_n", owner))
		}
	} else if context.MaxMessages != 0 {
		errors = add(errors, fmt.Sprintf("%q context.max_messages is legal with last_n only", owner))
	}
	if context.History != HistorySummary && context.Summarizer != "" {
		errors = add(errors, fmt.Sprintf("%q context.summarizer is legal with summary only", owner))
	}
	return errors
}

func validateBindings(agent *Agent, resolved Target, caps targetcap.Table, row *TargetValidation) {
	provider := targetcap.Provider(resolved.Provider)
	// The provider catalogue is the vendor/endpoint matrix; the same
	// CheckVendor rulebook backs driver resolution, so a binding that
	// validates green cannot fail provider selection at generate time.
	// (Becomes a parameter when the providers.yaml overlay loader lands.)
	catalog := targetcap.DefaultCatalog()
	checkVendor := func(role targetcap.Role, binding *Binding) {
		if binding == nil {
			return
		}
		if err := catalog.CheckVendor(provider, role, binding.Provider, binding.EndpointEnv != ""); err != nil {
			row.Errors = add(row.Errors, err.Error())
		}
	}
	// A per-model language must have a slot on the resolved target's integration
	// (N16). The generator errors on a slotless entry; mirror it here so a
	// slotless language fails validate, not just generate (C6: gate before any
	// artifact).
	checkLanguageSlot := func(role targetcap.Role, label string, binding *Binding) {
		if binding == nil || binding.Language == "" || binding.Provider == "" {
			return
		}
		entry, ok := catalog.Lookup(provider, role, binding.Provider)
		if !ok {
			return // an unknown vendor is already reported by checkVendor
		}
		if entry.Call == nil || entry.Call.NoLanguage || entry.Call.Language.Arg == "" {
			row.Errors = add(row.Errors, fmt.Sprintf("%s %s binding provider %q has no language slot; remove the language field", provider, label, binding.Provider))
		}
	}
	validateRoleBinding("listen", caps.Role(targetcap.Listen, provider), resolved.Models.Listen, row)
	checkVendor(targetcap.Listen, resolved.Models.Listen)
	checkLanguageSlot(targetcap.Listen, "listen", resolved.Models.Listen)
	for _, fallback := range resolved.Models.ListenFallbacks {
		binding := fallback.Binding
		validatePlacement("listen."+fallback.Name, &binding, row)
		checkVendor(targetcap.Listen, &binding)
		checkLanguageSlot(targetcap.Listen, "listen."+fallback.Name, &binding)
	}
	validateRoleBinding("turn", caps.Role(targetcap.Turn, provider), resolved.Models.Turn, row)
	checkVendor(targetcap.Turn, resolved.Models.Turn)

	models, voices := usedProfiles(agent)
	for _, name := range slices.Sorted(maps.Keys(voices)) {
		binding, ok := resolved.Models.Speak[name]
		if !ok || !bindingHasVoice(&binding) {
			row.Errors = add(row.Errors, fmt.Sprintf("%s target %q is missing speak binding for voice %q", resolved.Provider, resolved.Name, name))
			continue
		}
		validatePlacement("speak."+name, &binding, row)
		if binding.EndpointEnv != "" {
			applyCapability(caps, targetcap.FieldSpeakEndpoint, provider, row)
		}
		checkVendor(targetcap.Speak, &binding)
		checkLanguageSlot(targetcap.Speak, "speak."+name, &binding)
		checkSpeakRequiredFields(catalog, provider, name, binding, row)
	}
	for _, name := range slices.Sorted(maps.Keys(models)) {
		binding, ok := resolved.Models.Reason[name]
		if !ok || binding.Model == "" {
			row.Errors = add(row.Errors, fmt.Sprintf("%s target %q is missing reason binding for model %q", resolved.Provider, resolved.Name, name))
			continue
		}
		validatePlacement("reason."+name, &binding, row)
		checkVendor(targetcap.Reason, &binding)
	}
}

// checkSpeakRequiredFields enforces entry-declared field arity at validate
// time (a voice-less ElevenLabs binding, a model-less SLNG one), so a spec
// that validates green cannot fail speak resolution at generate. Listen and
// reason model presence is already covered by the open-role rules above;
// speak's has-voice-or-model floor alone lets these slip through.
func checkSpeakRequiredFields(catalog targetcap.Catalog, provider targetcap.Provider, profile string, binding Binding, row *TargetValidation) {
	if binding.Provider == "" {
		return
	}
	entry, ok := catalog.Lookup(provider, targetcap.Speak, binding.Provider)
	if !ok {
		return
	}
	voice := cmp.Or(binding.Voice, binding.VoiceID)
	// The sharpest of the eight former generator-only checks: `voice` is required
	// on every speak entry, so authoring a deepgram speak
	// model necessarily produced a package that validated green and could not
	// compile. On such an entry the voice rides the model id instead.
	if voice != "" && entry.Call != nil && entry.Call.Voice.Arg == "" {
		row.Errors = add(row.Errors, fmt.Sprintf("%s speak binding provider %q: voice has no slot here", provider, binding.Provider))
	}
	if entry.VoiceRequired() && voice == "" {
		row.Errors = add(row.Errors, fmt.Sprintf("%s speak.%s binding provider %q is missing a voice", provider, profile, binding.Provider))
	}
	if entry.ModelRequired() && binding.Model == "" {
		row.Errors = add(row.Errors, fmt.Sprintf("%s speak.%s binding provider %q is missing a model", provider, profile, binding.Provider))
	}
}

func validateRoleBinding(role string, kind targetcap.RoleKind, binding *Binding, row *TargetValidation) {
	if kind == targetcap.Open {
		if binding == nil || binding.Model == "" {
			row.Errors = add(row.Errors, fmt.Sprintf("%s target %q is missing open %s binding", row.Provider, row.Name, role))
			return
		}
		validatePlacement(role, binding, row)
		return
	}
	// turn is a preference everywhere (N15): an integrated target may carry an
	// advisory turn model; FieldTurnPlacement warns, it does not fail.
	if role != "turn" && bindingHasIdentity(binding) {
		row.Errors = add(row.Errors, fmt.Sprintf("%s integrated %s binding may carry settings only", row.Provider, role))
	}
}

func validatePlacement(role string, binding *Binding, row *TargetValidation) {
	// Placement is derived from a single source now (N15), so it cannot disagree
	// with itself; only the endpoint env name still needs a shape check.
	if binding.EndpointEnv != "" && !envNamePattern.MatchString(binding.EndpointEnv) {
		row.Errors = add(row.Errors, fmt.Sprintf("%s endpoint_env must be an environment variable name", role))
	}
}

func bindingHasIdentity(binding *Binding) bool {
	return binding != nil && (binding.Provider != "" || binding.Model != "" || binding.Voice != "" || binding.VoiceID != "" || binding.EndpointEnv != "" || binding.Placement != "")
}

func bindingHasVoice(binding *Binding) bool {
	return binding != nil && (binding.Model != "" || binding.Voice != "" || binding.VoiceID != "")
}

func usedProfiles(agent *Agent) (map[string]bool, map[string]bool) {
	models := make(map[string]bool)
	voices := make(map[string]bool)
	taskContexts := taskContextUsage(agent)
	for _, definition := range agent.Agents {
		models[definition.Model] = true
		voices[definition.Voice] = true
	}
	for name, task := range agent.Tasks {
		if task.Model != "" {
			models[task.Model] = true
		}
		if taskContexts[name] && task.Context.Summarizer != "" {
			models[task.Context.Summarizer] = true
		}
	}
	for _, control := range agent.Controls {
		if transfer, ok := control.(*AgentTransfer); ok && transfer.Context.Summarizer != "" {
			models[transfer.Context.Summarizer] = true
		}
	}
	for name := range maps.Clone(models) {
		for _, fallback := range agent.Models[name].Fallback {
			models[fallback] = true
		}
	}
	return models, voices
}

func taskContextUsage(agent *Agent) map[string]bool {
	usesOwnContext := make(map[string]bool, len(agent.Tasks))
	for name := range agent.Tasks {
		usesOwnContext[name] = true
	}
	for _, group := range agent.TaskGroups {
		for _, task := range group.Steps {
			usesOwnContext[task] = false
		}
	}
	for _, control := range agent.Controls {
		if delegate, ok := control.(*Delegate); ok && delegate.Task != "" {
			usesOwnContext[delegate.Task] = true
		}
	}
	return usesOwnContext
}

func validateFallbacks(agent *Agent, resolved Target, caps targetcap.Table, row *TargetValidation) {
	models, _ := usedProfiles(agent)
	slot := caps.FallbackSlots[targetcap.Provider(resolved.Provider)]
	if slot == "" {
		row.Errors = add(row.Errors, fmt.Sprintf("%s target has no fallback slot kind", resolved.Provider))
		return
	}
	// The listen chain mirrors the think rule: same section, same placement (T16).
	if agent.Listen != "" {
		listen := agent.Models[agent.Listen]
		for _, fallbackName := range listen.Fallback {
			if agent.Models[fallbackName].Placement != listen.Placement {
				row.Errors = add(row.Errors, fmt.Sprintf("fallback %q placement differs from %q", fallbackName, agent.Listen))
			}
		}
	}
	for name := range models {
		profile := agent.Models[name]
		primary := resolved.Models.Reason[name]
		if len(profile.Fallback) > 0 {
			applyCapability(caps, targetcap.FieldFallback, targetcap.Provider(resolved.Provider), row)
		}
		for _, fallbackName := range profile.Fallback {
			fallback := agent.Models[fallbackName]
			if fallback.Placement != profile.Placement {
				row.Errors = add(row.Errors, fmt.Sprintf("fallback %q placement differs from %q", fallbackName, name))
			}
			binding := resolved.Models.Reason[fallbackName]
			if slot == targetcap.FallbackSameProvider && primary.Provider != "" && binding.Provider != "" && primary.Provider != binding.Provider {
				row.Errors = add(row.Errors, "Vapi fallbackModels must stay within one provider")
			}
		}
	}
}

// validateHumanTransfer checks the resolved shape against the route (SCHEMA
// N25). A free-text briefing resolves nothing on its own: it rides the warm
// control row, so there is no briefing capability left to apply. On a telephony
// target the route table already resolved the control, so only the block's own
// values are checked here.
func validateHumanTransfer(control *HumanTransfer, resolved Target, provider targetcap.Provider, caps targetcap.Table, row *TargetValidation) {
	for _, err := range checkTransferBlock(control) {
		row.Errors = add(row.Errors, err)
	}
	if control.Briefing != "" {
		applyCapability(caps, targetcap.FieldTransferBriefing, provider, row)
	}
	if resolved.Telephony != nil {
		return
	}
	required := targetcap.ColdTransfer
	if control.Mode == TransferWarm {
		required = targetcap.WarmTransfer
	}
	before := len(row.Errors)
	applyResolvedCapability(caps.Control(required, provider, resolved.Transport, resolved.Carrier), required, provider, row)
	// Both modes need a Connection, for different reasons, and the difference is
	// why cold was missed.
	//
	// Warm dials the person itself, so it needs the carrier's SIP credentials.
	// The emitted agent passes the trunk settings inline (SCHEMA N33), so a
	// target with no telephony Connection has nothing to dial with, whatever
	// route it names.
	//
	// Cold dials nobody, and this guard's comment used to conclude from that
	// "cold is unaffected". It reasoned about credentials and missed the leg:
	// cold hands the caller's own call to the destination, so it needs a route
	// that can bridge one. A package that names a route has one — Daily's
	// dial-out reaches a PSTN number from a browser session. A package that names **no
	// route at all** has nothing, and the capability table cannot say so because
	// it describes route support, not route existence (research D2). That is the
	// browser-only case LiveKit used to compile, then explain away in a generated
	// `caller is None` comment (reproduction.md B).
	//
	// Only when the provider itself has not already refused, so one that denies
	// it keeps failing in its own words (Principle II). Pipecat's table row has
	// already added "Pipecat cold transfer requires Daily SIP transport", so this
	// generic message never appends on top of it.
	if len(row.Errors) != before {
		return
	}
	if control.Mode == TransferWarm {
		row.Errors = add(row.Errors, "warm transfer needs a telephony Connection: it dials the destination itself, using the connection's sip_address, sip_username, sip_password and from_number")
		return
	}
	// Only where a project is actually emitted. On a managed provider the
	// platform owns the call leg and configures the transfer on its own side, so
	// there is no route for the package to name and nothing for this to check.
	if resolved.Connection == "" && targetcap.EmitsProject(provider) {
		row.Errors = add(row.Errors, "cold transfer needs a telephony Connection: it hands the caller's own phone leg to the destination, and a session that did not arrive by phone has no leg to hand over")
	}
}

// checkTransferBlock validates the block's own values, independent of target.
func checkTransferBlock(control *HumanTransfer) []string {
	var errs []string
	if control.RingTimeout != "" {
		errs = append(errs, validateDuration("ring_timeout", control.RingTimeout)...)
	}
	switch control.OnUnavailable {
	case OnUnavailableReturn, OnUnavailableHangup:
	default:
		errs = append(errs, fmt.Sprintf("unknown on_unavailable %q: use %q or %q",
			control.OnUnavailable, OnUnavailableReturn, OnUnavailableHangup))
	}
	if control.Briefing != "" && control.Mode != TransferWarm {
		errs = append(errs, "briefing is legal in a `warm:` block only")
	}
	return errs
}

func validateTools(agent *Agent, resolved Target, provider targetcap.Provider, caps targetcap.Table, row *TargetValidation) {
	for _, tool := range agent.Tools {
		if tool.Output != nil {
			applyCapability(caps, targetcap.FieldToolOutput, provider, row)
		}
		switch tool.Execution {
		case ToolLocal:
			applyCapability(caps, targetcap.FieldToolLocal, provider, row)
		case ToolMCP:
			applyCapabilityValue(caps.CapabilityForValue(targetcap.FieldToolMCP, provider, resolved.SDKLanguage), string(targetcap.FieldToolMCP), provider, row)
		case ToolClient:
			applyCapability(caps, targetcap.FieldToolClient, provider, row)
		case ToolProviderHosted:
			applyCapability(caps, targetcap.FieldToolProviderHosted, provider, row)
		case ToolBuiltin:
			applyCapability(caps, targetcap.FieldToolBuiltin, provider, row)
		}
		if tool.Auth != nil {
			applyCapability(caps, targetcap.FieldToolAuth, provider, row)
		}
		if len(tool.Inject) > 0 {
			applyCapability(caps, targetcap.FieldToolInject, provider, row)
		}
		if tool.Path != "" {
			applyCapability(caps, targetcap.FieldWebhookPath, provider, row)
		}
		if tool.Interruption != ToolProviderDefault {
			applyCapability(caps, targetcap.FieldToolInterruption, provider, row)
		}
	}
	// Scoping an MCP source to a task is its own capability: the source is
	// legal, the scope is what a driver may not be able to hold (N40).
	for _, task := range agent.Tasks {
		for _, ref := range task.Tools {
			if agent.Tools[ref].Execution == ToolMCP {
				applyCapability(caps, targetcap.FieldToolMCPTask, provider, row)
			}
		}
	}
}

// validateVariables gates the two per-target variable features: capturing a
// value mid-call, and rendering a template into a prompt or greeting before the
// call starts (V5).
func validateVariables(agent *Agent, provider targetcap.Provider, caps targetcap.Table, row *TargetValidation) {
	for _, variable := range agent.Variables {
		if variable.Source == VariableSourceConversation {
			applyCapability(caps, targetcap.FieldVariableConversation, provider, row)
			break
		}
	}
	if agent.HasSessionStartTemplate() {
		applyCapability(caps, targetcap.FieldTemplates, provider, row)
	}
}

// HasSessionStartTemplate reports whether the greeting or any prompt carries a
// template, which is what needs render support on the target. The generators
// read it too, to decide whether to emit the render helper at all.
func (a *Agent) HasSessionStartTemplate() bool {
	if a.Conversation != nil && a.Conversation.Greeting != nil && HasTemplate(a.Conversation.Greeting.Text) {
		return true
	}
	for _, def := range a.Agents {
		if HasTemplate(def.Instructions) {
			return true
		}
	}
	for _, task := range a.Tasks {
		if HasTemplate(task.Instructions) {
			return true
		}
	}
	return false
}

// EnvReferenceSites groups the package sites naming each environment variable,
// so the compile report can show what a declared secret is actually used for.
func EnvReferenceSites(agent *Agent) map[string][]string {
	sites := make(map[string][]string)
	for name, site := range referencedEnvNames(agent) {
		sites[name] = append(sites[name], site)
	}
	for name := range sites {
		slices.Sort(sites[name])
	}
	return sites
}

// handlerEnvRead matches the three ways a local Python handler reads an
// environment variable: os.environ["X"], os.environ.get("X"), and os.getenv("X").
// Only UPPER_SNAKE names are collected, the same convention every *_env field
// enforces, so a lowercase lookup is never mistaken for a credential.
var handlerEnvRead = regexp.MustCompile(`os\.(?:environ\.get\(|getenv\(|environ\[)\s*["']([A-Z][A-Z0-9_]*)["']`)

// referencedEnvNames lists every environment variable the package points at,
// with the site that names it, so an undeclared one can be reported (V10).
//
// Connection environment values and destinations are ordinary members of this
// set. They used to be exempt, on the grounds that they are declared in their
// own file; that left no single list of what a package needs to run, so
// `secrets:` now carries every name the author wrote (spec FR-005a).
//
// Names the driver or the platform supplies are still absent, because no author
// writes them: REDIS_URL, UNMUTE_PUBLIC_URL, LIVEKIT_*, DAILY_API_KEY and the
// rest reach a package from its runtime, not from its source (FR-005c).
func referencedEnvNames(agent *Agent) map[string]string {
	refs := make(map[string]string)
	note := func(name, site string) {
		// Only names. A value in a name's slot — a pasted token, a URL — has its
		// own refusal that says what the field takes, and repeating it here would
		// print the secret back at the author in a warning (SC-005). This guard
		// used to be unreachable because the whole check returned early on an
		// empty secrets: block.
		if name == "" || !envNamePattern.MatchString(name) || refs[name] != "" {
			return
		}
		refs[name] = site
	}
	for _, name := range sortedKeys(agent.Tools) {
		tool := agent.Tools[name]
		switch tool.Execution {
		case ToolWebhook:
			note(tool.URLEnv, fmt.Sprintf("tools/%s.yaml webhook.url_env", name))
		case ToolMCP:
			note(tool.URLEnv, fmt.Sprintf("tools/%s.yaml mcp.url_env", name))
		case ToolLocal:
			// A local handler owns its own request, so its credential is read in
			// Python rather than named in YAML. Scanning the source is what keeps
			// that path inside the same cross-check as every *_env field, instead
			// of failing on the first tool call (V10).
			site := fmt.Sprintf("%s os.environ", tool.Handler)
			for _, match := range handlerEnvRead.FindAllStringSubmatch(tool.HandlerSource, -1) {
				note(match[1], site)
			}
		}
		if tool.Auth != nil {
			block := "webhook"
			if tool.Execution == ToolMCP {
				block = "mcp"
			}
			note(tool.Auth.TokenEnv, fmt.Sprintf("tools/%s.yaml %s.auth.token_env", name, block))
		}
	}
	for _, name := range sortedKeys(agent.Models) {
		note(agent.Models[name].EndpointEnv, fmt.Sprintf("model %q endpoint_env", name))
	}
	if agent.Tracing != nil {
		for _, name := range []string{"LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY", "LANGFUSE_BASE_URL"} {
			note(name, "tracing.provider: langfuse")
		}
	}
	for _, name := range sortedKeys(agent.Connections) {
		connection := agent.Connections[name]
		for _, key := range sortedKeys(connection.Environment) {
			note(connection.Environment[key], fmt.Sprintf("connections/%s.yaml environment %s", name, key))
		}
	}
	// Destinations are declared once in agent.yaml and resolved onto every
	// target, so any target carries the whole set (research R1).
	for _, target := range sortedKeys(agent.Targets) {
		for _, name := range sortedKeys(agent.Targets[target].Destinations) {
			note(agent.Targets[target].Destinations[name], fmt.Sprintf("agent.yaml destinations %s", name))
		}
	}
	for _, name := range sortedKeys(agent.Targets) {
		for _, key := range providerKeyEnvNames(agent, agent.Targets[name]) {
			note(key.name, key.site)
		}
	}
	return refs
}

// providerKeyEnvNames lists the API key each of a target's resolved model
// bindings reads, with the models entry that chose it.
//
// Without these the check is vacuous exactly where it matters most: a fresh
// `unmute init` package references no *_env field, no connection, and no
// destination, so its reference set was **empty** and removing the guard alone
// would have changed nothing there (FR-005a). The name has one home already —
// the catalogue Entry's key-env field — so it is read from there rather than
// listed a second time.
func providerKeyEnvNames(agent *Agent, resolved Target) []struct{ name, site string } {
	catalog := targetcap.DefaultCatalog()
	provider := targetcap.Provider(resolved.Provider)
	var out []struct{ name, site string }
	add := func(role targetcap.Role, section, profile string, binding *Binding) {
		if binding == nil || binding.Provider == "" {
			return
		}
		entry, ok := catalog.Lookup(provider, role, binding.Provider)
		if !ok || entry.Call == nil || entry.Call.APIKeyArg == "" {
			return // a managed target holds the key itself, or the call takes none
		}
		name := entry.Call.APIKeyEnv
		if name == "" {
			// The wildcard-row convention, the same one the drivers apply.
			name = strings.ToUpper(strings.ReplaceAll(binding.Provider, "-", "_")) + "_API_KEY"
		}
		out = append(out, struct{ name, site string }{name, fmt.Sprintf("agent.yaml models %s %s", section, profile)})
	}
	add(targetcap.Listen, "listen", agent.Listen, resolved.Models.Listen)
	for _, fallback := range resolved.Models.ListenFallbacks {
		binding := fallback.Binding
		add(targetcap.Listen, "listen", fallback.Name, &binding)
	}
	add(targetcap.Turn, "turn", agent.Turn, resolved.Models.Turn)
	for _, name := range sortedKeys(resolved.Models.Speak) {
		binding := resolved.Models.Speak[name]
		add(targetcap.Speak, "speak", name, &binding)
	}
	// `reason` is the internal role identifier; `think` is the authoring word,
	// and the site has to name the section the author can actually find (N15).
	for _, name := range sortedKeys(resolved.Models.Reason) {
		binding := resolved.Models.Reason[name]
		add(targetcap.Reason, "think", name, &binding)
	}
	return out
}

// unusedConnectionWarning reports connection files no target names.
//
// A warning rather than an error, and deliberately: an unused route file costs
// nothing at runtime, and an author part-way through wiring up a second carrier
// should not be stopped by the half they have not reached yet. It is checked
// across **every declared target**, not the `--target` selection, so validating
// one target never reports a file another target uses (spec FR-015).
func unusedConnectionWarning(agent *Agent) string {
	if len(agent.Connections) == 0 {
		return ""
	}
	named := make(map[string]bool, len(agent.Targets))
	for _, target := range agent.Targets {
		named[target.Connection] = true
	}
	var unused []string
	for _, name := range slices.Sorted(maps.Keys(agent.Connections)) {
		if !named[name] {
			unused = append(unused, fmt.Sprintf("connections/%s.yaml", name))
		}
	}
	if len(unused) == 0 {
		return ""
	}
	return "declares a route no target names, so nothing uses it: " + strings.Join(unused, ", ")
}

// undeclaredSecretWarning reports env names the package references but never
// declares. A warning, never an error: declaring secrets is opt-in, and a
// package written before the block existed still compiles (C7, V10).
//
// It used to return early on an empty `secrets:` block, which tested the
// **declaration** list: the package with the most to report — declares nothing,
// references eight names — took the same early return as the package with
// nothing to report, and lost the generated startup check as well. Opt-in
// controls the warning severity, not whether the check runs. Its sibling
// unusedConnectionWarning guards on the **subject** set, which is the correct
// shape and was already in this file.
func undeclaredSecretWarning(agent *Agent) string {
	refs := referencedEnvNames(agent)
	var missing []string
	for _, name := range slices.Sorted(maps.Keys(refs)) {
		if !slices.Contains(agent.Secrets, name) {
			missing = append(missing, fmt.Sprintf("%s (%s)", name, refs[name]))
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return "environment variables referenced but not declared in secrets: " + strings.Join(missing, ", ")
}

func validateConversation(conversation *Conversation, provider targetcap.Provider, caps targetcap.Table, row *TargetValidation) {
	if conversation == nil || conversation.Greeting == nil {
		applyCapability(caps, targetcap.FieldGreetingAbsent, provider, row)
	} else if conversation.Greeting.SpeaksFirst == SpeaksFirstUser {
		applyCapability(caps, targetcap.FieldGreetingUserFirst, provider, row)
	} else if conversation.Greeting.SpeaksFirst == SpeaksFirstAgent && conversation.Greeting.Text == "" {
		applyCapability(caps, targetcap.FieldGreetingModelWritten, provider, row)
	}
	if conversation == nil {
		return
	}
	if interruption := conversation.Interruption; interruption != nil {
		if interruption.MinimumWords > 0 {
			applyCapability(caps, targetcap.FieldInterruptionMinWords, provider, row)
		}
		if len(interruption.IgnorePhrases) > 0 {
			applyCapability(caps, targetcap.FieldInterruptionIgnore, provider, row)
		}
	}
	if conversation.Inactivity != nil {
		applyCapability(caps, targetcap.FieldInactivity, provider, row)
	}
	if conversation.MaxDuration != "" {
		applyCapability(caps, targetcap.FieldMaxDuration, provider, row)
	}
	if conversation.ThinkingAudio == ThinkingSubtle {
		applyCapability(caps, targetcap.FieldThinkingAudio, provider, row)
	}
}

func validateCapacity(agent *Agent, resolved Target, provider targetcap.Provider, row *TargetValidation) {
	required := targetcap.IsCode(provider)
	for _, channel := range agent.Channels {
		required = required || channel.Kind == ChannelTelephony
	}
	if agent.Capacity == nil {
		if required {
			row.Errors = add(row.Errors, "capacity is required for telephony or code targets")
		}
		return
	}
	if agent.Capacity.PeakSessions <= 0 || agent.Capacity.MaxSessions <= 0 {
		row.Errors = add(row.Errors, "capacity peak_sessions and max_sessions must be positive")
	}
	if hasTelephonyChannel(agent) && agent.Capacity.PeakStartsPerSecond <= 0 {
		row.Errors = add(row.Errors, "capacity.peak_starts_per_second must be positive for telephony")
	}
	if agent.Capacity.PeakSessions > agent.Capacity.MaxSessions {
		row.Errors = add(row.Errors, "capacity.peak_sessions must not exceed max_sessions")
	}
	if duration, err := time.ParseDuration(string(agent.Capacity.AvgSessionDuration)); err != nil || duration <= 0 {
		row.Errors = add(row.Errors, "capacity.avg_session_duration must be a positive Go duration")
	}
}

// hasWarmTransfer reports whether any control dials a destination itself.
func hasWarmTransfer(agent *Agent) bool {
	for _, control := range agent.Controls {
		if transfer, ok := control.(*HumanTransfer); ok && transfer.Mode == TransferWarm {
			return true
		}
	}
	return false
}

func validateChannels(agent *Agent, resolved Target, provider targetcap.Provider, caps targetcap.Table, row *TargetValidation) {
	for channelName, channel := range agent.Channels {
		if channel.Kind == ChannelTelephony && (provider == targetcap.LiveKit || provider == targetcap.Pipecat) && resolved.Telephony == nil {
			row.Errors = add(row.Errors, "telephony channel requires a resolved Connection plan")
			continue
		}
		for _, control := range channel.RequiredControls {
			if !validRequiredControl(control) {
				continue
			}
			if resolved.Telephony == nil {
				name := targetcap.TelephonyControl(control)
				applyResolvedCapability(caps.Control(name, provider, resolved.Transport, resolved.Carrier), name, provider, row)
			}
		}
		if channel.Kind != ChannelTelephony {
			continue
		}
		if resolved.Telephony != nil && channel.Inbound != nil && *channel.Inbound {
			for name, variable := range agent.Variables {
				if variable.Source == VariableSourceCallStart && variable.Default == nil {
					row.Errors = add(row.Errors, fmt.Sprintf("inbound call_start variable %q requires a default", name))
				}
			}
		}
		if channel.Outbound == nil || !*channel.Outbound {
			// A warm transfer dials the destination, so the agent places calls
			// whatever the channel says. Declaring one direction and using the
			// other is the spec lying about the emitted shape (V12/B5). One
			// error per channel, not per control: the remedy is the same line.
			if hasWarmTransfer(agent) {
				row.Errors = add(row.Errors, fmt.Sprintf("channel %q needs outbound: true; a warm transfer places a call to its destination", channelName))
			}
			continue
		}
		// Voicemail handling is optional for outbound (T1): the carrier-websocket
		// dial-out flow never requires it. on_voicemail stays route-gated below
		// and in the resolved plan's evidence, so a route that cannot detect
		// voicemail (Pipecat) still errors when on_voicemail is explicitly set.
		if resolved.Telephony == nil {
			for name, variable := range agent.Variables {
				if variable.Source == VariableSourceCallStart && variable.Default == nil {
					row.Errors = add(row.Errors, fmt.Sprintf("outbound call_start variable %q is not satisfiable", name))
				}
			}
			applyCapability(caps, targetcap.FieldOutbound, provider, row)
			if channel.OnVoicemail != "" {
				applyCapability(caps, targetcap.FieldVoicemail, provider, row)
				applyResolvedCapability(caps.Control(targetcap.VoicemailDetection, provider, resolved.Transport, resolved.Carrier), targetcap.VoicemailDetection, provider, row)
			}
		}
	}
}

func validateTelephonyPlan(plan *TelephonyPlan, row *TargetValidation) {
	if plan == nil {
		return
	}
	if plan.Coordination != "shared" {
		row.Errors = add(row.Errors, "telephony coordination must be shared")
	}
	// One route runs nothing of the operator's, so an empty process list is the
	// expected shape there and a mistake everywhere else (SCHEMA N38).
	hostsNothing := plan.Key.Provider == ProviderPipecat && plan.Key.Transport == "cloud-websocket"
	if len(plan.Processes) == 0 && !hostsNothing {
		row.Errors = add(row.Errors, "telephony plan has no runtime process")
	}
	if len(plan.Processes) > 0 && hostsNothing {
		row.Errors = add(row.Errors, "the Pipecat Cloud websocket route runs no process of yours; a process here contradicts the route")
	}
	if len(plan.PublicEndpoints) > 0 && hostsNothing {
		row.Errors = add(row.Errors, "the Pipecat Cloud websocket route hosts no endpoint of yours; an endpoint here contradicts the route")
	}
	seenProcesses := make(map[string]bool, len(plan.Processes))
	for _, process := range plan.Processes {
		if process.Name == "" || len(process.Command) == 0 || seenProcesses[process.Name] {
			row.Errors = add(row.Errors, "telephony runtime processes must have unique names and non-empty commands")
		}
		seenProcesses[process.Name] = true
	}
	seenEndpoints := make(map[string]bool, len(plan.PublicEndpoints))
	for _, endpoint := range plan.PublicEndpoints {
		if endpoint.Name == "" || endpoint.Method == "" || endpoint.Path == "" || seenEndpoints[endpoint.Name] {
			row.Errors = add(row.Errors, "telephony public endpoints must have unique names, methods, and paths")
		}
		seenEndpoints[endpoint.Name] = true
	}
	requiredEnvironment := make(map[string]bool, len(plan.RequiredEnvironment))
	for _, name := range plan.RequiredEnvironment {
		if name == "" || requiredEnvironment[name] {
			row.Errors = add(row.Errors, "telephony required environment must be non-empty and unique")
		}
		requiredEnvironment[name] = true
	}
	localEnvironment := make(map[string]bool, len(plan.LocalEnvironment))
	for _, name := range plan.LocalEnvironment {
		if name == "" || localEnvironment[name] || !requiredEnvironment[name] {
			row.Errors = add(row.Errors, "telephony locally supplied environment must be unique and required by the runtime")
		}
		localEnvironment[name] = true
	}
	if plan.AutoWebhookEndpoint != "" {
		if !slices.ContainsFunc(plan.PublicEndpoints, func(e TelephonyEndpoint) bool { return e.Name == plan.AutoWebhookEndpoint }) {
			row.Errors = add(row.Errors, "telephony auto-webhook endpoint must name an emitted public endpoint")
		}
	}
	if len(plan.ManualSteps) == 0 {
		row.Errors = add(row.Errors, "telephony plan has no route setup instructions")
	}
	services := make(map[string]bool, len(plan.Services))
	// Each route has an exact service set: Pipecat and LiveKit SIP coordinate
	// through Redis; the LiveKit connector runs the app plus a local LiveKit
	// Server only (no Redis, no SIP bridge).
	isLiveKitSIP := plan.Key.Provider == ProviderLiveKit && plan.Key.Transport == "sip"
	isLiveKitConnector := plan.Key.Provider == ProviderLiveKit && plan.Key.Transport == "connector"
	// The Pipecat Daily carrier route runs the operator's helper and nothing
	// else: no Redis, because it keeps no shared control record (SCHEMA N37).
	isPipecatDailyCarrier := plan.Key.Provider == ProviderPipecat && plan.Key.Transport == "daily-sip"
	allowedServices := map[string]bool{"application": true}
	requiredServices := []string{"application"}
	switch {
	case isPipecatDailyCarrier, hostsNothing:
		// application only, already in both sets. On the cloud-websocket route the
		// application is the deployed agent: the platform hosts it, and dev runs the
		// same one locally, which is why an empty process list and one application
		// service are the same route rather than a contradiction.
	case isLiveKitSIP:
		allowedServices["redis"] = true
		allowedServices["livekit_server"] = true
		allowedServices["livekit_sip"] = true
		requiredServices = append(requiredServices, "redis", "livekit_server", "livekit_sip")
	case isLiveKitConnector:
		allowedServices["livekit_server"] = true
		requiredServices = append(requiredServices, "livekit_server")
	default:
		allowedServices["redis"] = true
		requiredServices = append(requiredServices, "redis")
	}
	for _, service := range plan.Services {
		if service == "" || services[service] {
			row.Errors = add(row.Errors, "telephony services must be non-empty and unique")
			continue
		}
		if !allowedServices[service] {
			row.Errors = add(row.Errors, fmt.Sprintf("telephony route declares unexpected service %q", service))
		}
		services[service] = true
	}
	for _, required := range requiredServices {
		if !services[required] {
			row.Errors = add(row.Errors, fmt.Sprintf("telephony service %s is required", required))
		}
	}
	closedReasons := map[string]bool{
		"livekit_control_plane": true, "call_correlation": true, "callback_idempotency": true,
		"human_transfer": true, "admission": true,
	}
	if len(plan.CoordinationReasons) == 0 {
		row.Errors = add(row.Errors, "telephony Redis service has no coordination consumer")
	}
	seenReasons := make(map[string]bool, len(plan.CoordinationReasons))
	for _, reason := range plan.CoordinationReasons {
		if !closedReasons[reason.Name] {
			row.Errors = add(row.Errors, fmt.Sprintf("unknown telephony coordination reason %q", reason.Name))
		}
		if seenReasons[reason.Name] {
			row.Errors = add(row.Errors, fmt.Sprintf("duplicate telephony coordination reason %q", reason.Name))
		}
		seenReasons[reason.Name] = true
		if len(reason.Consumers) == 0 {
			row.Errors = add(row.Errors, fmt.Sprintf("telephony coordination reason %q has no consumers", reason.Name))
		}
		for _, consumer := range reason.Consumers {
			if consumer == "redis" || !services[consumer] {
				row.Errors = add(row.Errors, fmt.Sprintf("telephony coordination reason %q has undeclared consumer %q", reason.Name, consumer))
			}
		}
		if plan.Key.Provider == ProviderPipecat && (len(reason.Consumers) != 1 || reason.Consumers[0] != "application") {
			row.Errors = add(row.Errors, fmt.Sprintf("Pipecat coordination reason %q must be consumed by application", reason.Name))
		}
	}
	if plan.Key.Provider == ProviderPipecat {
		// The two correlation reasons describe Redis-backed records, so they are
		// required exactly where Redis is: the carrier-websocket routes. The Daily
		// carrier leg keeps no such record and admits calls through the room
		// (SCHEMA N37), so requiring them there would demand a reason for a service
		// the same validation forbids.
		required := []string{"admission", "call_correlation", "callback_idempotency"}
		if isPipecatDailyCarrier || hostsNothing {
			required = []string{"admission"}
		}
		for _, name := range required {
			if !seenReasons[name] {
				row.Errors = add(row.Errors, fmt.Sprintf("Pipecat coordination reason %q is required", name))
			}
		}
		if isPipecatDailyCarrier && len(seenReasons) != 1 {
			row.Errors = add(row.Errors, "the Pipecat Daily carrier route coordinates only admission")
		}
		if hostsNothing && len(seenReasons) != 1 {
			row.Errors = add(row.Errors, "the Pipecat Cloud websocket route coordinates only admission")
		}
	}
	if plan.Key.Provider == ProviderLiveKit && plan.Key.Transport == "sip" {
		if len(seenReasons) != 1 || !seenReasons["livekit_control_plane"] {
			row.Errors = add(row.Errors, "LiveKit SIP coordination reason must be livekit_control_plane")
		}
		for _, reason := range plan.CoordinationReasons {
			consumers := slices.Clone(reason.Consumers)
			slices.Sort(consumers)
			if reason.Name == "livekit_control_plane" && !slices.Equal(consumers, []string{"livekit_server", "livekit_sip"}) {
				row.Errors = add(row.Errors, "LiveKit control-plane coordination consumers must be livekit_server and livekit_sip")
			}
		}
	}
	for _, evidence := range plan.Evidence {
		switch targetcap.Tag(evidence.Tag) {
		case targetcap.Core:
			if evidence.Docs == "" || evidence.Verified == "" || !evidence.Smoke {
				row.Errors = add(row.Errors, fmt.Sprintf("telephony feature %s lacks complete route evidence", evidence.Feature))
			}
		case targetcap.Warn:
			row.Warnings = add(row.Warnings, evidence.Note)
		case targetcap.Provisional:
			// A provisional route has a real adapter but no automated end-to-end
			// test yet. It is usable and validates silently; the provisional
			// status stays in compile-report.json for the team to track and
			// promote. Only Gated (no adapter exists) is a hard error.
		case targetcap.Gated:
			// Name the connection and the transport it declares, not just the
			// feature. The author's next move is to open a file and change a
			// line, and until the route moved into the connection this message
			// could not say which file that was (spec FR-016a).
			row.Errors = add(row.Errors, fmt.Sprintf("telephony %s: %s. Connection %q declares transport: %s",
				evidence.Feature, strings.TrimSuffix(evidence.Note, "."), plan.Connection, plan.Key.Transport))
		default:
			row.Errors = add(row.Errors, fmt.Sprintf("telephony feature %s has no capability tag", evidence.Feature))
		}
	}
}

func applyResolvedCapability(capability targetcap.Capability, control targetcap.TelephonyControl, provider targetcap.Provider, row *TargetValidation) {
	applyCapabilityValue(capability, string(control), provider, row)
}

// validateRegions rejects the two authoring mistakes a region list can hold. A
// duplicate is never deduplicated silently: two first deploys against one config
// file name is a confusing thing to debug.
func validateRegions(regions []string) []string {
	var errors []string
	seen := make(map[string]bool, len(regions))
	for _, region := range regions {
		switch {
		case region == "":
			errors = add(errors, "deployment_region has an empty entry")
		case seen[region]:
			errors = add(errors, fmt.Sprintf("deployment_region lists %q twice", region))
		}
		seen[region] = true
	}
	return errors
}

func applyCapabilityValue(capability targetcap.Capability, name string, provider targetcap.Provider, row *TargetValidation) {
	switch capability.Tag {
	case targetcap.Core:
	case targetcap.Warn:
		row.Warnings = add(row.Warnings, capability.Note)
	case targetcap.Gated, targetcap.Provisional:
		row.Errors = add(row.Errors, capability.Note)
	default:
		row.Errors = add(row.Errors, fmt.Sprintf("capability %q has no %s tag", name, provider))
	}
}

func applyCapability(caps targetcap.Table, field targetcap.Field, provider targetcap.Provider, row *TargetValidation) {
	applyCapabilityValue(caps.Capability(field, provider), string(field), provider, row)
}

func validateDuration(name string, value Duration) []string {
	duration, err := time.ParseDuration(string(value))
	if err != nil || duration <= 0 {
		return []string{fmt.Sprintf("%s must be a positive Go duration", name)}
	}
	return nil
}

// validateModelKind field-checks a model against its section kind (V22).
func validateModelKind(name string, model ModelDef) []string {
	var errors []string
	switch model.Kind {
	case KindSpeak:
		if model.Temperature != nil || model.TopP != nil || model.TopK != nil {
			errors = add(errors, fmt.Sprintf("model %q is a speak model; temperature, top_p, and top_k are think-model fields", name))
		}
	case KindThink:
		if model.Voice != "" || model.Speed != nil {
			errors = add(errors, fmt.Sprintf("model %q is a think model; voice and speed are speak-model fields", name))
		}
	case KindListen:
		if model.Voice != "" || model.Speed != nil || model.Temperature != nil || model.TopP != nil || model.TopK != nil {
			errors = add(errors, fmt.Sprintf("model %q is a listen model; voice, speed, and sampling fields do not apply", name))
		}
	case KindTurn:
		if model.Voice != "" || model.Speed != nil || model.Temperature != nil || model.TopP != nil || model.TopK != nil {
			errors = add(errors, fmt.Sprintf("model %q is a turn model; voice, speed, and sampling fields do not apply", name))
		}
	}
	if model.SemanticEndpointing != "" {
		if model.Kind != KindTurn {
			errors = add(errors, fmt.Sprintf("model %q semantic_endpointing is a turn-model field", name))
		} else {
			switch model.SemanticEndpointing {
			case SemanticEndpointingRequired, SemanticEndpointingPreferred, SemanticEndpointingOff:
			default:
				errors = add(errors, fmt.Sprintf("model %q semantic_endpointing must be required, preferred, or off", name))
			}
		}
	}
	return errors
}

func validPlacement(value Placement) bool {
	return value == PlacementAPI || value == PlacementLocal
}

func validVariableSource(value VariableSource) bool {
	switch value {
	case VariableSourceCallStart, VariableSourceSessionID, VariableSourceCarrier,
		VariableSourceConnection, VariableSourceCallID, VariableSourceStreamID,
		VariableSourceDirection, VariableSourceFromNumber, VariableSourceToNumber,
		VariableSourceConversation:
		return true
	default:
		return false
	}
}

func validPrimitive(value PrimitiveType) bool {
	return value == PrimitiveString || value == PrimitiveNumber || value == PrimitiveBoolean || value == PrimitiveInteger
}

func validRequiredControl(value string) bool {
	switch value {
	case "cold_transfer", "warm_transfer", "dtmf_send", "dtmf_receive", "hold", "hangup", "voicemail_detection", "ivr_navigation":
		return true
	default:
		return false
	}
}

func defaultMatches(kind PrimitiveType, value any) bool {
	switch kind {
	case PrimitiveString:
		_, ok := value.(string)
		return ok
	case PrimitiveBoolean:
		_, ok := value.(bool)
		return ok
	case PrimitiveInteger:
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			return true
		}
	case PrimitiveNumber:
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return true
		}
	}
	return false
}

func add(values []string, value string) []string {
	if value == "" || slices.Contains(values, value) {
		return values
	}
	return append(values, strings.TrimSpace(value))
}
