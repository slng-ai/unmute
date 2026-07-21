package ir

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	targetcap "github.com/slng/unmute/internal/target"
)

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
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
	global := validateStructure(agent)
	global = append(global, validateConfiguredTargets(agent, caps)...)
	report := ValidateReport{PerTarget: make([]TargetValidation, 0, len(targets))}
	failed := 0
	for _, resolved := range targets {
		row := TargetValidation{Name: resolved.Name, Provider: resolved.Provider, Errors: append([]string(nil), global...)}
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

func validateStructure(agent *Agent) []string {
	var errors []string
	if agent.Version != 1 {
		errors = add(errors, "version must be 1")
	}
	if agent.Language != "" && !languagePattern.MatchString(agent.Language) {
		errors = add(errors, "language must be a BCP-47 language tag such as en or en-US")
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
		if tool.Description == "" {
			errors = add(errors, fmt.Sprintf("tool %q description is required", name))
		}
		if tool.Input["type"] != "object" {
			errors = add(errors, fmt.Sprintf("tool %q input must be a JSON Schema object", name))
		}
		if tool.Output != nil && tool.Output["type"] != "object" {
			errors = add(errors, fmt.Sprintf("tool %q output must be a JSON Schema object", name))
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
				errors = add(errors, fmt.Sprintf("tool %q url_env must be an environment variable name", name))
			}
			if tool.Handler != "" {
				errors = add(errors, fmt.Sprintf("tool %q handler is legal for local execution only", name))
			}
		case ToolMCP:
			// B3 (SCHEMA §5, 2026-07-16): url_env names the MCP server address.
			if tool.URLEnv == "" {
				errors = add(errors, fmt.Sprintf("tool %q url_env is required for mcp execution (the MCP server address env)", name))
			} else if !envNamePattern.MatchString(tool.URLEnv) {
				errors = add(errors, fmt.Sprintf("tool %q url_env must be an environment variable name", name))
			}
			if tool.Handler != "" {
				errors = add(errors, fmt.Sprintf("tool %q handler is legal for local execution only", name))
			}
		case ToolClient, ToolProviderHosted, ToolBuiltin:
			if tool.Handler != "" || tool.URLEnv != "" {
				errors = add(errors, fmt.Sprintf("tool %q handler/url_env does not match execution %q", name, tool.Execution))
			}
		default:
			errors = add(errors, fmt.Sprintf("tool %q has invalid execution %q", name, tool.Execution))
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
	return errors
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
	if agent.Tracing != nil {
		applyCapability(caps, targetcap.FieldTracingLangfuse, provider, row)
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
	validateConversation(agent.Conversation, provider, caps, row)
	validateTelephonyPlan(resolved.Telephony, row)
	validateCapacity(agent, resolved, provider, row)
	validateChannels(agent, resolved, provider, caps, row)
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
	validateRoleBinding("listen", caps.Role(targetcap.Listen, provider), resolved.Models.Listen, row)
	checkVendor(targetcap.Listen, resolved.Models.Listen)
	for _, fallback := range resolved.Models.ListenFallbacks {
		binding := fallback.Binding
		validatePlacement("listen."+fallback.Name, &binding, row)
		checkVendor(targetcap.Listen, &binding)
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
	if entry.VoiceRequired() && binding.Voice == "" && binding.VoiceID == "" {
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
			if slot == targetcap.FallbackModelID && len(binding.Params) > 0 {
				row.Warnings = add(row.Warnings, "ElevenLabs fallback entries accept model IDs only; per-entry params are not forwarded")
			}
		}
	}
}

func validateHumanTransfer(control *HumanTransfer, resolved Target, provider targetcap.Provider, caps targetcap.Table, row *TargetValidation) {
	if resolved.Telephony != nil {
		switch control.Briefing {
		case "", BriefingSummary, BriefingMessage, BriefingWait:
		default:
			row.Errors = add(row.Errors, fmt.Sprintf("unknown warm-transfer briefing %q", control.Briefing))
		}
		return
	}
	required := targetcap.ColdTransfer
	if control.Mode == TransferWarm {
		required = targetcap.WarmTransfer
	}
	applyResolvedCapability(caps.Control(required, provider, resolved.Transport, resolved.Carrier), required, provider, row)
	switch control.Briefing {
	case "":
	case BriefingSummary:
		applyCapability(caps, targetcap.FieldBriefingSummary, provider, row)
	case BriefingMessage:
		// V8: the message-briefing gate is carrier-conditional on ElevenLabs
		// (native Twilio only); CapabilityForValue is a no-op where no condition exists.
		applyCapabilityValue(caps.CapabilityForValue(targetcap.FieldBriefingMessage, provider, resolved.Carrier), string(targetcap.FieldBriefingMessage), provider, row)
	case BriefingWait:
		applyCapability(caps, targetcap.FieldBriefingWait, provider, row)
	default:
		row.Errors = add(row.Errors, fmt.Sprintf("unknown warm-transfer briefing %q", control.Briefing))
	}
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
		if tool.Interruption != ToolProviderDefault {
			applyCapability(caps, targetcap.FieldToolInterruption, provider, row)
		}
	}
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

func validateChannels(agent *Agent, resolved Target, provider targetcap.Provider, caps targetcap.Table, row *TargetValidation) {
	for _, channel := range agent.Channels {
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
			continue
		}
		if channel.OnVoicemail == "" {
			row.Errors = add(row.Errors, "outbound telephony requires on_voicemail")
		}
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
	services := make(map[string]bool, len(plan.Services))
	allowedServices := map[string]bool{"application": true, "redis": true}
	if plan.Key.Provider == ProviderLiveKit && plan.Key.Transport == "sip" {
		allowedServices["livekit_server"] = true
		allowedServices["livekit_sip"] = true
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
	for _, required := range []string{"application", "redis"} {
		if !services[required] {
			row.Errors = add(row.Errors, fmt.Sprintf("telephony service %s is required", required))
		}
	}
	if plan.Key.Provider == ProviderLiveKit && plan.Key.Transport == "sip" {
		for _, required := range []string{"livekit_server", "livekit_sip"} {
			if !services[required] {
				row.Errors = add(row.Errors, fmt.Sprintf("livekit SIP service %s is required", required))
			}
		}
	} else if services["livekit_server"] || services["livekit_sip"] {
		row.Errors = add(row.Errors, "LiveKit Server and SIP services require the livekit/sip route")
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
		for _, required := range []string{"admission", "call_correlation", "callback_idempotency"} {
			if !seenReasons[required] {
				row.Errors = add(row.Errors, fmt.Sprintf("Pipecat coordination reason %q is required", required))
			}
		}
		needsHumanTransfer := false
		for _, evidence := range plan.Evidence {
			needsHumanTransfer = needsHumanTransfer || evidence.Feature == string(targetcap.ColdTransfer) || evidence.Feature == string(targetcap.WarmTransfer)
		}
		if needsHumanTransfer != seenReasons["human_transfer"] {
			row.Errors = add(row.Errors, "Pipecat human_transfer coordination reason must match the selected transfer features")
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
		case targetcap.Gated, targetcap.Provisional:
			row.Errors = add(row.Errors, fmt.Sprintf("telephony %s: %s", evidence.Feature, evidence.Note))
		default:
			row.Errors = add(row.Errors, fmt.Sprintf("telephony feature %s has no capability tag", evidence.Feature))
		}
	}
}

func applyResolvedCapability(capability targetcap.Capability, control targetcap.TelephonyControl, provider targetcap.Provider, row *TargetValidation) {
	applyCapabilityValue(capability, string(control), provider, row)
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
		VariableSourceDirection, VariableSourceFromNumber, VariableSourceToNumber:
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
