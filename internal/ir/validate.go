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
	local := agent.Pipeline.Listen.Placement == PlacementLocal || agent.Pipeline.Speak.Placement == PlacementLocal
	for _, profile := range agent.Models {
		local = local || profile.Placement == PlacementLocal
	}
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
	return result
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
	if len(agent.Models) == 0 {
		errors = add(errors, "models must contain at least one profile")
	}
	if len(agent.Voices) == 0 {
		errors = add(errors, "voices must contain at least one profile")
	}
	if len(agent.Agents) == 0 {
		errors = add(errors, "agents must contain the entry agent")
	}
	if len(agent.Channels) == 0 {
		errors = add(errors, "channels must contain at least one channel")
	}
	if !validPlacement(agent.Pipeline.Listen.Placement) {
		errors = add(errors, "pipeline.listen.placement must be api or local")
	}
	if !validPlacement(agent.Pipeline.Speak.Placement) {
		errors = add(errors, "pipeline.speak.placement must be api or local")
	}
	if agent.Pipeline.Turn != nil {
		if !validPlacement(agent.Pipeline.Turn.Placement) {
			errors = add(errors, "pipeline.turn.placement must be api or local")
		}
		switch agent.Pipeline.Turn.SemanticEndpointing {
		case "", SemanticEndpointingRequired, SemanticEndpointingPreferred, SemanticEndpointingOff:
		default:
			errors = add(errors, "pipeline.turn.semantic_endpointing must be required, preferred, or off")
		}
	}
	for name, profile := range agent.Models {
		if !validPlacement(profile.Placement) {
			errors = add(errors, fmt.Sprintf("model %q placement must be api or local", name))
		}
	}
	for name, variable := range agent.Variables {
		if !validPrimitive(variable.Type) {
			errors = add(errors, fmt.Sprintf("variable %q has invalid type %q", name, variable.Type))
		}
		if variable.Source != "" && variable.Source != VariableSourceCallStart {
			errors = add(errors, fmt.Sprintf("variable %q source must be call_start", name))
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
		case ToolClient, ToolProviderHosted, ToolBuiltin, ToolMCP:
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
	if agent.Pipeline.Listen.Placement == PlacementLocal {
		applyCapability(caps, targetcap.FieldListenLocal, provider, row)
	}
	if agent.Pipeline.Speak.Placement == PlacementLocal {
		applyCapability(caps, targetcap.FieldSpeakLocal, provider, row)
	}
	if agent.Pipeline.Turn != nil {
		applyCapability(caps, targetcap.FieldTurnPlacement, provider, row)
		if agent.Pipeline.Turn.SemanticEndpointing != "" {
			applyCapability(caps, targetcap.FieldSemanticEndpointing, provider, row)
		}
	}
	for name, profile := range agent.Models {
		if profile.Placement == PlacementLocal {
			binding := resolved.Models.Reason[name]
			applyCapabilityValue(caps.CapabilityForValue(targetcap.FieldReasonLocal, provider, binding.EndpointEnv), string(targetcap.FieldReasonLocal), provider, row)
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
	validateCapacity(agent, provider, row)
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
	validateRoleBinding("listen", caps.Role(targetcap.Listen, provider), resolved.Models.Listen, agent.Pipeline.Listen.Placement, row)
	checkVendor(targetcap.Listen, resolved.Models.Listen)
	turnPlacement := Placement("")
	if agent.Pipeline.Turn != nil {
		turnPlacement = agent.Pipeline.Turn.Placement
	}
	validateRoleBinding("turn", caps.Role(targetcap.Turn, provider), resolved.Models.Turn, turnPlacement, row)
	checkVendor(targetcap.Turn, resolved.Models.Turn)

	models, voices := usedProfiles(agent)
	for _, name := range slices.Sorted(maps.Keys(voices)) {
		binding, ok := resolved.Models.Speak[name]
		if !ok || !bindingHasVoice(&binding) {
			row.Errors = add(row.Errors, fmt.Sprintf("%s target %q is missing speak binding for voice %q", resolved.Provider, resolved.Name, name))
			continue
		}
		validatePlacement("speak."+name, &binding, agent.Pipeline.Speak.Placement, row)
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
		validatePlacement("reason."+name, &binding, agent.Models[name].Placement, row)
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
	if !ok || entry.Call == nil {
		return
	}
	if entry.Call.Voice.Required && binding.Voice == "" && binding.VoiceID == "" {
		row.Errors = add(row.Errors, fmt.Sprintf("%s speak.%s binding provider %q is missing a voice", provider, profile, binding.Provider))
	}
	if entry.Call.Model.Required && binding.Model == "" {
		row.Errors = add(row.Errors, fmt.Sprintf("%s speak.%s binding provider %q is missing a model", provider, profile, binding.Provider))
	}
}

func validateRoleBinding(role string, kind targetcap.RoleKind, binding *Binding, placement Placement, row *TargetValidation) {
	if kind == targetcap.Open {
		if binding == nil || binding.Model == "" {
			row.Errors = add(row.Errors, fmt.Sprintf("%s target %q is missing open %s binding", row.Provider, row.Name, role))
			return
		}
		validatePlacement(role, binding, placement, row)
		return
	}
	if bindingHasIdentity(binding) {
		row.Errors = add(row.Errors, fmt.Sprintf("%s integrated %s binding may carry settings only", row.Provider, role))
	}
}

func validatePlacement(role string, binding *Binding, placement Placement, row *TargetValidation) {
	if placement != "" && binding.Placement != "" && binding.Placement != placement {
		row.Errors = add(row.Errors, fmt.Sprintf("%s binding placement %q disagrees with %q", role, binding.Placement, placement))
	}
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

func validateCapacity(agent *Agent, provider targetcap.Provider, row *TargetValidation) {
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
	if agent.Capacity.PeakSessions > agent.Capacity.MaxSessions {
		row.Errors = add(row.Errors, "capacity.peak_sessions must not exceed max_sessions")
	}
	if duration, err := time.ParseDuration(string(agent.Capacity.AvgSessionDuration)); err != nil || duration <= 0 {
		row.Errors = add(row.Errors, "capacity.avg_session_duration must be a positive Go duration")
	}
}

func validateChannels(agent *Agent, resolved Target, provider targetcap.Provider, caps targetcap.Table, row *TargetValidation) {
	for _, channel := range agent.Channels {
		for _, control := range channel.RequiredControls {
			if !validRequiredControl(control) {
				continue
			}
			name := targetcap.TelephonyControl(control)
			applyResolvedCapability(caps.Control(name, provider, resolved.Transport, resolved.Carrier), name, provider, row)
		}
		if channel.Kind != ChannelTelephony || channel.Outbound == nil || !*channel.Outbound {
			continue
		}
		if channel.OnVoicemail == "" {
			row.Errors = add(row.Errors, "outbound telephony requires on_voicemail")
		}
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

func validPlacement(value Placement) bool {
	return value == PlacementAPI || value == PlacementLocal
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
