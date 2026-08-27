package ir

import (
	"cmp"
	"fmt"
	"maps"
	// net/url parses; it opens nothing. A webhook base URL has to be shape-checked
	// before it reaches a platform that would reject it with a 422.
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	packagespec "github.com/slng-ai/unmute/internal/spec"
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
	global = append(global, slngOneAgentIDPerPackage(agent)...)
	global = append(global, slngScopeErrors(agent)...)
	global = append(global, knowledgeErrors(agent)...)
	globalWarnings = add(globalWarnings, undeclaredSecretWarning(agent))
	globalWarnings = add(globalWarnings, unusedConnectionWarning(agent))
	globalWarnings = add(globalWarnings, unusedKnowledgeWarning(agent))
	globalWarnings = add(globalWarnings, knowledgeBudgetWarning(agent))
	globalWarnings = add(globalWarnings, knowledgeCutoffWarning(agent))
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
	if agent.Tracing != nil && !validTracingProvider(agent.Tracing.Provider) {
		errors = add(errors, fmt.Sprintf("tracing provider must be one of %s", strings.Join(TracingProviders, ", ")))
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
		errors = append(errors, paceErrors(name, model)...)
		for _, warning := range turnDeadFieldWarnings(name, model) {
			warnings = add(warnings, warning)
		}
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
		for _, ref := range task.Tools {
			var kind ControlKind
			switch agent.Controls[ref].(type) {
			case *Delegate:
				kind = ControlDelegate
			case *HumanTransfer:
				kind = ControlHumanTransfer
			}
			if kind != "" {
				errors = add(errors, fmt.Sprintf("task %q references control %q with kind %q; tasks may reference agent_transfer controls only", name, ref, kind))
			}
		}
		for fieldName, field := range task.Result {
			if fieldName == UnservedResultField {
				errors = add(errors, fmt.Sprintf("task %q result %q is reserved: every generated task finish already takes %s for a request the step cannot serve", name, fieldName, UnservedResultField))
			}
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
		// announce needs a body to speak before, so it follows inject's rule.
		// Blank already resolved to empty in Build, so reaching here means the
		// author wrote a real sentence.
		if tool.Announce != "" {
			switch tool.Execution {
			// A knowledge lookup is a body to speak before, the same as a
			// webhook call, so FR-029 reuses this field rather than adding one.
			case ToolWebhook, ToolLocal, ToolKnowledge:
			default:
				errors = add(errors, fmt.Sprintf("tool %q announce is legal for webhook, local and knowledge execution only", name))
			}
			// Fixed sentence, same rule as the transfer announcement: a
			// rendered line would need the variable set to be in scope at the
			// moment the tool fires, which is not a promise this field makes.
			if HasTemplate(tool.Announce) {
				errors = add(errors, fmt.Sprintf("tool %q announce does not support templates", name))
			}
		}
		if tool.Execution == ToolBuiltin {
			validateBuiltinTool(name, tool, &errors)
			continue
		}
		// An mcp file carries no per-tool contract at all: the server announces
		// each tool at run time (N40). spec.Load rejects the fields with a line
		// number, so reaching here means the IR was built in code.
		// A knowledge tool owns both sides of its contract too, but keeps its
		// description: unlike an mcp server, nothing else can say what is in the
		// folder. spec.Load rejects input and output with a line number, so the
		// arm below covers only an IR built in code.
		if tool.Execution == ToolKnowledge {
			if tool.Input != nil || tool.Output != nil {
				errors = add(errors, fmt.Sprintf("tool %q knowledge execution takes no input or output: the tool owns its own schema", name))
			}
			if tool.KnowledgeBase == "" {
				errors = add(errors, fmt.Sprintf("tool %q base is required for knowledge execution", name))
			} else if _, ok := agent.Knowledge[tool.KnowledgeBase]; !ok {
				declared := sortedKeys(agent.Knowledge)
				if len(declared) == 0 {
					errors = add(errors, fmt.Sprintf("tool %q names knowledge base %q, and no knowledge: section is declared in agent.yaml", name, tool.KnowledgeBase))
				} else {
					errors = add(errors, fmt.Sprintf("tool %q names knowledge base %q which is not declared in knowledge: (declared: %s)",
						name, tool.KnowledgeBase, strings.Join(declared, ", ")))
				}
			}
		} else if tool.KnowledgeBase != "" {
			errors = add(errors, fmt.Sprintf("tool %q base is legal for knowledge execution only", name))
		}
		if tool.Execution != ToolMCP {
			if tool.Description == "" {
				errors = add(errors, fmt.Sprintf("tool %q description is required", name))
			}
			if tool.Execution != ToolKnowledge && tool.Input["type"] != "object" {
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
			// The pin shape is checked wherever it is declared, not per target: a
			// range or a URL is not a dependency on any platform. Which targets can
			// install one is a separate question, answered by FieldToolDependencies.
			if _, err := targetcap.CanonicalSlngPins(tool.Dependencies); err != nil {
				errors = add(errors, fmt.Sprintf("tool %q dependency: %v", name, err))
			}
			if tool.URLEnv != "" {
				errors = add(errors, fmt.Sprintf("tool %q url_env is legal for webhook execution only", name))
			}
		case ToolWebhook:
			// A webhook needs a base from somewhere, and there are two shapes it
			// can take. Which one a target needs is a per-target question, asked in
			// validateTarget; this is the shared floor: at least one, and each one
			// well formed if written.
			if tool.URLEnv == "" && tool.BaseURL == "" {
				errors = add(errors, fmt.Sprintf("tool %q needs url_env or base_url for webhook execution", name))
			}
			if tool.URLEnv != "" && !envNamePattern.MatchString(tool.URLEnv) {
				errors = add(errors, fmt.Sprintf("tool %q url_env must be an UPPER_SNAKE environment variable name", name))
			}
			errors = append(errors, validateWebhookBaseURL(name, tool.BaseURL)...)
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
		case ToolKnowledge, ToolClient, ToolProviderHosted:
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
			} else if !*channel.Inbound && !*channel.Outbound {
				errors = add(errors, fmt.Sprintf("channel %q must enable inbound or outbound", name))
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
		if interruption := agent.Conversation.Interruption; interruption != nil {
			if interruption.Enabled == nil {
				errors = add(errors, "conversation.interruption.enabled is required")
			}
			for _, protect := range interruption.Protect {
				switch protect {
				case ProtectGreeting, ProtectToolCalls:
				default:
					errors = add(errors, fmt.Sprintf("conversation.interruption.protect has unknown entry %q: use %q or %q", protect, ProtectGreeting, ProtectToolCalls))
				}
			}
			// enabled: false already mutes the caller for every word the agent
			// speaks, so naming a stretch to protect on top of it is a
			// contradiction rather than a narrowing, and silently ignoring one of
			// the two would be worse than saying so.
			if interruption.Enabled != nil && !*interruption.Enabled && len(interruption.Protect) > 0 {
				errors = add(errors, "conversation.interruption.protect has no meaning when enabled is false, which already stops the caller talking over the whole call: drop protect, or set enabled: true to keep barge-in outside the protected stretches")
			}
		}
	}
	// schemas.notes and warnings are both structural warnings. Notes come first
	// because they came first historically and the report's order is golden-stable.
	// Before 2026-08-27 the named `warnings` return was dead: everything added to it
	// was collected and then discarded here, so a warning could look wired up and
	// reach nobody.
	return errors, append(schemas.notes, warnings...)
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
	// The three checks above all return early for a provider with no support
	// window, no pin floor and no SDK, which is every provider whose driver emits
	// no project. So a bodiless target accepted version, pins and sdk_language in
	// silence, and connection was never asked about at all (research R5). Say no
	// by name instead: the point of validate is that the answer arrives before
	// anything is written.
	if !targetcap.EmitsProject(provider) {
		refuseProjectOnlyValues(resolved, provider, row)
	}
}

// refuseProjectOnlyValues names the four target settings that describe a
// generated project, on a target that generates none.
func refuseProjectOnlyValues(resolved Target, provider targetcap.Provider, row *TargetValidation) {
	if provider == targetcap.Slng {
		refuseSlngProjectValues(resolved, row)
		return
	}
	// No other bodiless target exists yet. When one does, it says no in its own
	// words rather than inheriting SLNG's, which is what "a gated error uses that
	// target's vocabulary" means.
	row.Errors = add(row.Errors, fmt.Sprintf("%s emits no project, and this repository has no wording for refusing version, pins, sdk_language and connection on it", provider))
}

// livekitVADSilenceFloor is the shortest VAD silence window livekit-agents will
// accept with a streaming turn detector bound. Below it,
// _check_vad_silence_requirement raises ValueError rather than degrading
// (voice/audio_recognition.py:885-903, guarding inference/eot/base.py's
// MIN_SILENCE_DURATION_MS = 200 plus a 50ms margin).
const (
	livekitVADSilenceFloor     = 250 * time.Millisecond
	livekitVADSilenceFloorText = "250ms"
)

func validateTarget(agent *Agent, resolved Target, caps targetcap.Table, row *TargetValidation) {
	provider := targetcap.Provider(resolved.Provider)
	if !slices.Contains(targetcap.Providers, provider) {
		// A retired provider used to work, so "unknown" is true and useless.
		// Say what happened and what the supported values are (Principle II).
		supported := make([]string, 0, len(targetcap.Providers))
		for _, p := range targetcap.Providers {
			supported = append(supported, string(p))
		}
		if note, retired := targetcap.Retired[provider]; retired {
			row.Errors = add(row.Errors, fmt.Sprintf("provider %q was retired: %s. Supported providers: %s",
				resolved.Provider, note, strings.Join(supported, ", ")))
			return
		}
		row.Errors = add(row.Errors, fmt.Sprintf("unknown provider %q. Supported providers: %s",
			resolved.Provider, strings.Join(supported, ", ")))
		return
	}
	if targetcap.IsCode(provider) && resolved.Version == "" {
		row.Errors = add(row.Errors, fmt.Sprintf("%s code target requires version", resolved.Provider))
	}
	validateDriverValues(resolved, provider, row)
	validateVaultTokens(agent, provider, row)
	if provider == targetcap.Slng {
		validateSlngTarget(agent, resolved, row)
	}
	if agent.Tracing != nil {
		applyCapability(caps, tracingCapability(agent.Tracing.Provider), provider, row)
	}
	row.Errors = append(row.Errors, validateRegions(resolved.DeploymentRegions)...)
	// Only a list of more than one is gated: one region works everywhere the
	// field works, and the scalar form has since N18.
	if len(resolved.DeploymentRegions) > 1 {
		applyCapability(caps, targetcap.FieldDeploymentMultiRegion, provider, row)
	}
	// Absent and zero are the same declaration: no instance held ready, which is
	// every platform's default. So only a stated pool is gated, and a negative
	// one is refused here rather than reaching a manifest the platform rejects.
	if resolved.WarmInstances < 0 {
		row.Errors = add(row.Errors, fmt.Sprintf("warm_instances is %d; it counts instances held ready, so it cannot be negative", resolved.WarmInstances))
	}
	if resolved.WarmInstances > 0 {
		applyCapability(caps, targetcap.FieldWarmInstances, provider, row)
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
		// No warning for a binding that sets both pace and endpointing_delay:
		// both together is the recommended configuration, and the compile report
		// names which value won which half on every compile. Two versions of
		// that warning were written and both were wrong; the test named
		// TestValidatePaceWithDelayCompilesAndRefusesNothing records why.
		if b.Pace != "" {
			applyCapability(caps, targetcap.FieldPace, provider, row)
			// A per-target pace is refused rather than resolved to whichever won.
			// One word working on both targets is the whole reason the field
			// exists; a value that differed per target would be a duration in
			// disguise, and endpointing_delay already is one.
			//
			// agent.Turn is non-empty whenever this binding exists: resolveBindings
			// only builds bindings.Turn from a named turn model.
			if base := agent.Models[agent.Turn].Pace; b.Pace != base {
				authored := fmt.Sprintf("the package authors %q", base)
				if base == "" {
					authored = "the package authors none"
				}
				row.Errors = add(row.Errors, fmt.Sprintf(
					"turn binding overrides pace with %q where %s: pace takes no per-target override. Put one value on the models.turn binding, or use endpointing_delay for a window that really is per-target",
					b.Pace, authored))
			}
		}
		if b.EndpointingDelay != "" {
			applyCapability(caps, targetcap.FieldEndpointingDelay, provider, row)
			// The window is what the runtime actually waits, so the runtime's own
			// floor is a compile error rather than a first-call crash: with a
			// streaming turn detector bound, livekit-agents raises ValueError
			// below 250ms (voice/audio_recognition.py:885-903, guarding
			// MIN_SILENCE_DURATION_MS = 200 plus 50). LiveKit always has that
			// detector, because a target with no turn binding is already refused.
			if provider == targetcap.LiveKit {
				if d, err := time.ParseDuration(string(b.EndpointingDelay)); err == nil && d < livekitVADSilenceFloor {
					row.Errors = append(row.Errors, fmt.Sprintf(
						"endpointing_delay %s is below the %s floor livekit-agents enforces on the VAD silence window; it would raise on the first call",
						b.EndpointingDelay, livekitVADSilenceFloorText))
				}
			}
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
			if len(control.Requires) > 0 {
				applyCapability(caps, targetcap.FieldDelegateRequires, provider, row)
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
// Keyed on the route triple so validation and emitters read the same route
// contract without re-deriving it from the runtime plan.
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
		checkWarmStandby(provider, name, binding, row)
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
	validateSlngRouter(agent, resolved, row)
}

// validateSlngRouter holds every rule a SLNG Context Router think binding
// carries: the region, the agent id, one id per compiled package, the upstream
// block and its credential shape.
//
// It runs per target rather than over agent.Models because "one agent id per
// compiled package" is a property of what a target actually builds, and a
// per-target override can turn a binding into a router binding or away from one.
func validateSlngRouter(agent *Agent, resolved Target, row *TargetValidation) {
	var routers []string
	for _, name := range slices.Sorted(maps.Keys(resolved.Models.Reason)) {
		binding := resolved.Models.Reason[name]
		// An instructions file is package-level, so the directive appended to it is
		// too: Build applies the base binding's suffix once, to prompts every target
		// shares. A target override naming a different one is asking for two
		// different prompts out of one markdown file, which nothing downstream can
		// deliver, so it is refused rather than resolved to whichever won.
		if base := agent.Models[name].PromptSuffix; binding.PromptSuffix != base {
			row.Errors = add(row.Errors, fmt.Sprintf(
				"think.%s overrides prompt_suffix with %q where the package authors %q: the directive is appended to instructions files every target shares, so it cannot differ per target. Put one value on the base binding, or split the package",
				name, binding.PromptSuffix, base))
		}
		if !binding.Router() {
			// It works here — it is only prompt text — but the reason the field
			// exists is a router-served model whose thinking no parameter turns
			// off, so an author who set it elsewhere may have meant something else.
			if binding.PromptSuffix != "" {
				row.Warnings = add(row.Warnings, fmt.Sprintf(
					"think.%s sets prompt_suffix on a provider %q binding: it will be appended to every prompt as written, which works, but the field exists for a model whose own prompt directive is the only way to reach it. Check you meant this binding",
					name, binding.Provider))
			}
			// Both fields are the router's. A field with no slot is refused
			// rather than dropped, the same as every other slotless field.
			if binding.AgentID != "" {
				row.Errors = add(row.Errors, fmt.Sprintf("think.%s sets agent_id, which belongs to provider %q; this binding is provider %q", name, ProviderSlngRouter, binding.Provider))
			}
			if binding.Upstream != nil {
				row.Errors = add(row.Errors, fmt.Sprintf("think.%s sets upstream, which belongs to provider %q; this binding is provider %q", name, ProviderSlngRouter, binding.Provider))
			}
			continue
		}
		routers = append(routers, name)
		if binding.EndpointEnv != "" {
			row.Errors = add(row.Errors, fmt.Sprintf("think.%s sets endpoint_env, but params.world_part_override already selects the router endpoint and upstream owns the upstream one", name))
		}
		row.Errors = append(row.Errors, slngRouterRegionErrors(name, binding)...)
		if err := targetcap.ValidateSlngAgentID(binding.AgentID); err != nil {
			row.Errors = add(row.Errors, fmt.Sprintf("think.%s %s", name, err))
		}
		row.Errors = append(row.Errors, slngUpstreamErrors(agent, name, binding)...)
		if warning := slngReasoningEffortWarning(agent, name, binding); warning != "" {
			row.Warnings = add(row.Warnings, warning)
		}
	}
	// One target-shaped limit, named as such. LiveKit builds an agent's or a
	// task's own LLM inside its constructor, where neither the call's session id
	// nor the call state is in scope, and it constructs agents from ten places.
	// The session LLM is built in the entrypoint, where both exist. So a router
	// profile that is not the entry agent's has nowhere to put its per-call
	// values on this target, and saying so before generation beats emitting an
	// agent whose every think request is missing them (Principle II). Pipecat
	// builds one LLM per agent from a single place and has no such limit.
	if resolved.Provider == ProviderLiveKit && len(routers) > 0 {
		entry := agent.Agents[agent.EntryAgent].Model
		for _, profile := range routers {
			if profile != entry {
				row.Errors = add(row.Errors, fmt.Sprintf(
					"livekit: think.%s is a %s router profile but not the entry agent's (%s uses think.%s). On livekit the router's per-call session id and variable values are only in scope where the session model is built, so every router profile in a package has to be the entry agent's. Point the agents and tasks that use think.%s at the entry profile, or keep this profile on a direct provider",
					profile, ProviderSlngRouter, agent.EntryAgent, entry, profile))
			}
		}
	}
	// FR-001 and FR-014. A managed target forwards provider names into an API
	// body and emits no client, so it cannot carry a regional base URL, two
	// identity headers, or an inline configuration. Without this the binding
	// passes the vendor rulebook (a managed row forwards any provider name) and
	// the package compiles into an agent that never reaches the router.
	if len(routers) > 0 {
		fw := targetcap.Provider(resolved.Provider)
		if entry, ok := targetcap.DefaultCatalog().Lookup(fw, targetcap.Reason, ProviderSlngRouter); !ok || entry.Wildcard() {
			row.Errors = add(row.Errors, fmt.Sprintf(
				"%s has no think slot for provider %q: the SLNG Context Router needs a regional base URL, the two identity headers and an inline configuration on every request, and this target emits no client to put them on. The targets that serve it are %s",
				resolved.Provider, ProviderSlngRouter, strings.Join(slngRouterTargets(), " and ")))
		}
	}
}

// slngOneAgentIDPerPackage holds FR-010, the rule with the worst silent cost. A
// second id splits one package's cache into two namespaces and hit rates never
// build: nothing fails, the agent is simply never fast. So the refusal names both
// profiles and both values, because the author has to know which line to change.
//
// The composing half of FR-010 is gone: an agent id is no longer carried verbatim
// to every site, each site sends its own scope derived from it. What survives is
// this, that a package authors one id, because that is what makes the derivation
// mean anything.
func slngOneAgentIDPerPackage(agent *Agent) []string {
	ids := slngAuthoredIDs(agent)
	var errors []string
	for _, other := range ids[min(1, len(ids)):] {
		if other[1] != ids[0][1] {
			errors = add(errors, fmt.Sprintf(
				"think.%s agent_id %q and think.%s agent_id %q differ; one package sends one agent id, because the id is what scopes the router's cache",
				ids[0][0], ids[0][1], other[0], other[1]))
		}
	}
	return errors
}

// slngAuthoredIDs collects every agent id this package authors, as
// (profile, id) pairs in profile-name order, first sighting of a profile
// winning.
//
// It runs over the whole models palette rather than only the profiles a target
// compiles, and over every target's resolved bindings as well. An unused second
// profile emits nothing today, but pointing one agent at it is a one-word edit.
//
// One owner, two readers: the one-id-per-package rule and the derived-scope
// rules below both need the same answer to "what did the author write", and two
// walks of the same maps could disagree about it.
func slngAuthoredIDs(agent *Agent) [][2]string {
	var ids [][2]string
	seen := map[string]bool{}
	note := func(profile, id string) {
		if id == "" || seen[profile] {
			return
		}
		seen[profile] = true
		ids = append(ids, [2]string{profile, id})
	}
	for _, name := range sortedKeys(agent.Models) {
		if model := agent.Models[name]; model.Kind == KindThink && model.Provider == ProviderSlngRouter {
			note(name, model.AgentID)
		}
	}
	for _, target := range sortedKeys(agent.Targets) {
		reason := agent.Targets[target].Models.Reason
		for _, name := range slices.Sorted(maps.Keys(reason)) {
			if binding := reason[name]; binding.Router() {
				note(name, binding.AgentID)
			}
		}
	}
	return ids
}

// slngPromptSites lists every place in this package that will send a system
// prompt to the router: one site per agent, one per task, plus the summarizer,
// whose prompt is fixed and which therefore needs only one value.
//
// The summarizer is included whether or not this package's conversation setting
// emits one. Its suffix is the fixed and short `summary`, so including it can
// only refuse a package whose every other site is already refused, and leaving
// it out would mean a package that starts summarising later fails then instead
// of now.
func slngPromptSites(agent *Agent) []targetcap.SlngSite {
	sites := make([]targetcap.SlngSite, 0, len(agent.Agents)+len(agent.Tasks)+1)
	for _, name := range sortedKeys(agent.Agents) {
		sites = append(sites, targetcap.SlngSite{Kind: targetcap.SlngSiteAgent, Name: name})
	}
	for _, name := range sortedKeys(agent.Tasks) {
		sites = append(sites, targetcap.SlngSite{Kind: targetcap.SlngSiteTask, Name: name})
	}
	return append(sites, targetcap.SlngSite{Kind: targetcap.SlngSiteSummary})
}

// slngScopeErrors holds the one refusal the derived cache scope brings with it:
// the bound moves from the authored value to the value that actually leaves.
//
// It does not also refuse a name holding the separator, which the plan for this
// change expected to be necessary. It is not: checkNames in build.go already
// holds every agent and task name to `^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`, before
// Validate ever runs, so a name cannot hold a colon. The overlap is gated from
// the other side instead, by TestNamePatternExcludesTheScopeSeparator, because a
// derived scope's uniqueness now rests on a pattern that was written for Python
// identifiers and knows nothing about cache scopes.
//
// Nothing fires on a package with no router binding: a package that derives no
// scope has no scope to refuse.
func slngScopeErrors(agent *Agent) []string {
	ids := slngAuthoredIDs(agent)
	if len(ids) == 0 {
		return nil
	}
	sites := slngPromptSites(agent)
	// Every authored id is checked, not only the first. The one-id rule refuses
	// a second id separately, and reporting both problems in one pass beats
	// making an author fix one to discover the other.
	var errors []string
	for _, pair := range ids {
		for _, site := range sites {
			if err := targetcap.ValidateSlngScope(pair[1], site); err != nil {
				errors = add(errors, fmt.Sprintf("think.%s: %s", pair[0], err))
			}
		}
	}
	return errors
}

// slngRouterTargets names the targets whose catalogue carries a router think
// row, read from the catalogue so the refusal cannot drift from the code.
func slngRouterTargets() []string {
	var out []string
	for _, entry := range targetcap.DefaultCatalog().Entries() {
		if entry.Role == targetcap.Reason && entry.Vendor == ProviderSlngRouter {
			out = append(out, string(entry.Framework))
		}
	}
	slices.Sort(out)
	return out
}

// slngRouterRegionErrors holds FR-003 and FR-005. The refusal names the four
// router regions and says they are the router's own set, because `na` copied
// from the regional infrastructure page is the likely mistake.
func slngRouterRegionErrors(profile string, binding Binding) []string {
	regions := strings.Join(targetcap.SlngRouterRegions, ", ")
	value, ok := binding.Params["world_part_override"]
	if !ok {
		return []string{fmt.Sprintf("think.%s needs params.world_part_override to pick a router region: one of %s", profile, regions)}
	}
	region, _ := value.(string)
	if _, ok := targetcap.SlngRouterBaseURL(region); !ok {
		return []string{fmt.Sprintf(
			"think.%s params.world_part_override %q is not a router region: one of %s. These four are the router's own set; na, eu and ap are the SLNG *speech* world parts, which share this key and not its accepted values",
			profile, region, regions)}
	}
	return nil
}

// slngUpstreamErrors holds FR-034 and its lettered siblings: the block is
// present with a known provider, every field that provider requires is set, no
// field belongs to another provider, and every credential names an environment
// variable rather than holding one.
func slngUpstreamErrors(agent *Agent, profile string, binding Binding) []string {
	if binding.Upstream == nil {
		return []string{fmt.Sprintf(
			"think.%s needs an upstream block saying which upstream serves the model: one of %s. Nothing is assumed for you, because the configuration travels inline on every request and the credentials are yours",
			profile, strings.Join(targetcap.SlngUpstreamProviders(), ", "))}
	}
	fields := binding.Upstream.Fields()
	var errors []string
	for _, text := range targetcap.ValidateSlngUpstream(binding.Upstream.Provider, fields) {
		errors = add(errors, fmt.Sprintf("think.%s %s", profile, text))
	}
	upstream, known := targetcap.SlngUpstreamByName(binding.Upstream.Provider)
	if !known {
		return errors
	}
	for _, field := range upstream.Fields {
		value, written := fields[field.Authored]
		if !field.Credential || !written {
			continue
		}
		if !envNamePattern.MatchString(value) {
			// The offending text is never repeated back: what lands here is
			// usually a pasted credential, and a refusal that quotes it puts the
			// value in a terminal, a CI log and a bug report.
			errors = add(errors, fmt.Sprintf(
				"think.%s upstream %s is not an environment variable name: use upper case letters, digits and underscores, and do not start with a digit. This field names a variable and a package never holds a secret value",
				profile, field.Authored))
			continue
		}
		if !slices.Contains(agent.Secrets, value) {
			// An error rather than the usual warning: this name is the one the
			// upstream is paid with, it reaches the generated startup check
			// through secrets:, and a package that forgets it fails on the first
			// turn of a live call instead of at compile time (FR-034b).
			errors = add(errors, fmt.Sprintf(
				"think.%s upstream %s names %s, which secrets: does not declare; add it there so the generated agent checks for it at startup",
				profile, field.Authored, value))
		}
	}
	return errors
}

// slngReasoningEffortWarning holds FR-037: a warning, never a refusal, because
// the compiler cannot know the upstream model family for certain. Warnings go to
// stderr and keep exit 0, so it never hides a downgrade.
//
// Scoped to upstreams that serve OpenAI's own models, because the advice is only
// true there. On an openai-compat upstream it fired on every compile telling the
// author to set a parameter the host answers with a 400 (qwen/qwen3-32b on
// Nebius, measured 2026-08-27), and a warning nobody can act on is how the ones
// they should act on come to be scrolled past.
func slngReasoningEffortWarning(agent *Agent, profile string, binding Binding) string {
	if _, set := binding.Params["reasoning_effort"]; set {
		return ""
	}
	if !slngProfileHasTools(agent, profile) {
		return ""
	}
	// An absent upstream block is the openai default, which is the row this
	// advice was measured on, so an unnamed provider still warns.
	provider := "openai"
	if binding.Upstream != nil && binding.Upstream.Provider != "" {
		provider = binding.Upstream.Provider
	}
	if upstream, ok := targetcap.SlngUpstreamByName(provider); ok && !upstream.OpenAIModels {
		return ""
	}
	return fmt.Sprintf(
		"think.%s has tools and no params.reasoning_effort: measured 2026-08-19, every tool turn on the gpt-5.6 family then returns 400 through the router, \"Function tools with reasoning_effort are not supported\". Set reasoning_effort: \"none\" unless the upstream model is not one of those",
		profile)
}

// slngProfileHasTools reports whether any agent or task bound to this think
// profile carries tools. A task with no model of its own runs on the entry
// agent's profile, which is where the trap usually hides.
func slngProfileHasTools(agent *Agent, profile string) bool {
	entry := agent.Agents[agent.EntryAgent].Model
	for _, def := range agent.Agents {
		if def.Model == profile && len(def.Tools) > 0 {
			return true
		}
	}
	for _, task := range agent.Tasks {
		if cmp.Or(task.Model, entry) == profile && len(task.Tools) > 0 {
			return true
		}
	}
	return false
}

// upstreamEnvNames lists the environment names a router upstream block names,
// with the site that wrote each. Only author-written names: a compiler-supplied
// default is never demanded of the author, the same rule
// TestSecretsCrossCheckNeverAsksForDriverSuppliedNames already holds for
// provider keys.
func upstreamEnvNames(agent *Agent) map[string]string {
	out := map[string]string{}
	for _, name := range sortedKeys(agent.Models) {
		model := agent.Models[name]
		if model.Provider != ProviderSlngRouter || model.Upstream == nil {
			continue
		}
		upstream, known := targetcap.SlngUpstreamByName(model.Upstream.Provider)
		if !known {
			continue
		}
		fields := model.Upstream.Fields()
		for _, field := range upstream.Fields {
			if value, written := fields[field.Authored]; field.Credential && written && envNamePattern.MatchString(value) {
				out[value] = fmt.Sprintf("model %q upstream.%s", name, field.Authored)
			}
		}
	}
	return out
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

// checkWarmStandby warns that warm_standby_enabled reaches nothing on Pipecat.
//
// It is a real setting on the LiveKit side, where the SLNG plugin pre-opens the
// synthesis socket and reports standby_used per segment. pipecat-slng has no
// such kwarg through 0.5.0, which deferred it: the value lands in **kwargs,
// travels up to FrameProcessor and is discarded with no error. Nothing else in
// the compiler would catch that, because a params: block is forwarded verbatim
// by design, so a package can promise a held-open socket and get none.
//
// A warning rather than a refusal on purpose. Packages that ship to both targets
// author one speak binding for both, and salon-concierge is one of them, so
// refusing here would break a working package to report a param that is merely
// inert.
func checkWarmStandby(provider targetcap.Provider, profile string, binding Binding, row *TargetValidation) {
	if provider != targetcap.Pipecat {
		return
	}
	if _, set := binding.Params["warm_standby_enabled"]; !set {
		return
	}
	row.Warnings = add(row.Warnings, fmt.Sprintf(
		"pipecat speak.%s sets params.warm_standby_enabled, which pipecat-slng 0.5.0 does not implement: the kwarg is absorbed and no socket is held open. It works on livekit, so move it to a livekit target override if that is where you meant it",
		profile,
	))
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
// target the route table already resolved the control. Route-specific fallback
// limits are checked here after Build has resolved the default policy.
func validateHumanTransfer(control *HumanTransfer, resolved Target, provider targetcap.Provider, caps targetcap.Table, row *TargetValidation) {
	for _, err := range checkTransferBlock(control) {
		row.Errors = add(row.Errors, err)
	}
	if control.Briefing != "" {
		applyCapability(caps, targetcap.FieldTransferBriefing, provider, row)
	}
	cloudTwilio := resolved.Provider == ProviderPipecat && resolved.Transport == "cloud-websocket" && resolved.Carrier == "twilio"
	if control.Mode == TransferCold && cloudTwilio && resolved.Telephony == nil {
		row.Errors = add(row.Errors, "human transfer on (pipecat, cloud-websocket, twilio) requires channels.phone: a web session has no live Twilio CallSid or media stream to transfer")
		return
	}
	if control.Mode == TransferCold && cloudTwilio && control.OnUnavailable == OnUnavailableReturn {
		row.Errors = add(row.Errors, "human transfer on_unavailable: return_to_caller is not supported on (pipecat, cloud-websocket, twilio): this route cannot reconnect the original media stream; use on_unavailable: hangup")
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
	for _, name := range slices.Sorted(maps.Keys(agent.Tools)) {
		tool := agent.Tools[name]
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
		case ToolKnowledge:
			applyCapability(caps, targetcap.FieldToolKnowledge, provider, row)
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
		// A code driver emits `os.environ[...]` for the base URL, so it needs the
		// env var form and reads base_url not at all. The slng target asks the
		// mirror question in validateSlngTool. A package targeting both carries
		// both fields, and neither target is worse off for the other one existing.
		if tool.Execution == ToolWebhook && targetcap.EmitsProject(provider) && tool.URLEnv == "" {
			row.Errors = add(row.Errors, fmt.Sprintf("%s target reads a webhook base URL from the environment: tool %q needs url_env, keeping base_url for a hosted target", provider, name))
		}
		if tool.Interruption != ToolProviderDefault {
			applyCapability(caps, targetcap.FieldToolInterruption, provider, row)
		}
		if len(tool.Dependencies) > 0 {
			applyCapability(caps, targetcap.FieldToolDependencies, provider, row)
		}
		if tool.Announce != "" {
			applyCapability(caps, targetcap.FieldToolAnnounce, provider, row)
		}
	}
	// Scoping an MCP source to a task is its own capability: the source is
	// legal, the scope is what a driver may not be able to hold (N40). A tool
	// announcement splits the same way, so both scope checks share this loop.
	for _, task := range agent.Tasks {
		for _, ref := range task.Tools {
			if agent.Tools[ref].Execution == ToolMCP {
				applyCapability(caps, targetcap.FieldToolMCPTask, provider, row)
			}
			if agent.Tools[ref].Execution == ToolKnowledge {
				applyCapability(caps, targetcap.FieldToolKnowledgeTask, provider, row)
			}
			if agent.Tools[ref].Announce != "" {
				applyCapability(caps, targetcap.FieldToolAnnounceTask, provider, row)
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
	// A router upstream's credentials are *_env fields like any other, so they
	// belong in the same reference set: it is what the compile report reads to
	// say which site names each variable.
	upstreams := upstreamEnvNames(agent)
	for _, name := range slices.Sorted(maps.Keys(upstreams)) {
		note(name, upstreams[name])
	}
	if agent.Tracing != nil {
		for _, name := range TracingSecrets[agent.Tracing.Provider] {
			note(name, "tracing.provider: "+agent.Tracing.Provider)
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
	// A router upstream credential is the one group whose absence is an error
	// rather than a warning (FR-034b, slngUpstreamErrors). Reporting it here as
	// well would print the same missing name twice at two severities.
	errored := upstreamEnvNames(agent)
	var missing []string
	for _, name := range slices.Sorted(maps.Keys(refs)) {
		if errored[name] != "" {
			continue
		}
		if !slices.Contains(agent.Secrets, name) {
			missing = append(missing, fmt.Sprintf("%s (%s)", name, refs[name]))
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return "environment variables referenced but not declared in secrets: " + strings.Join(missing, ", ")
}

// knowledgeNamePattern is what a knowledge base name may be.
//
// Both bounds are ours, and readability is the whole reason for them. The name is
// a folder under knowledge/, a key in the emitted module's settings, and a word in
// every report and log line about the base, so one or two characters is too short
// to identify anything and 512 is not a thing an author meant to type.
//
// The lower bound used to be Chroma's: it refused a collection name under three
// characters, and this pattern inherited the number. That store is gone and the
// bound stayed, because three is still a sensible floor for a name a human reads.
var knowledgeNamePattern = regexp.MustCompile(`^[a-z0-9_]{3,64}$`)

// knowledgeErrors holds the compile-time half of the knowledge base rules:
// FR-009 (the folder), FR-011 (the service), FR-016 (its credential) and the
// name legality that keeps a runtime InvalidArgumentError from being the first
// the author hears of it.
//
// What is deliberately absent: any check on document content. Deciding whether a
// PDF yields text needs a parser the compiler does not have, so that is a startup
// failure instead (FR-010).
func knowledgeErrors(agent *Agent) []string {
	var errors []string
	for _, name := range sortedKeys(agent.Knowledge) {
		base := agent.Knowledge[name]
		if !knowledgeNamePattern.MatchString(name) {
			errors = add(errors, fmt.Sprintf("knowledge base %q: name must be 3 to 64 characters of [a-z0-9_]", name))
		}
		switch {
		case base.Documents == "":
			errors = add(errors, fmt.Sprintf("knowledge base %q: documents is required: name the folder holding its documents", name))
		case len(base.Files) == 0:
			// One message for both "the folder is not there" and "the folder
			// holds nothing we can read", because the author's next action is
			// the same either way and the compiler cannot always tell them
			// apart without another stat.
			errors = add(errors, fmt.Sprintf("knowledge base %q: documents folder %q holds no file of a supported type (looked for %s)",
				name, base.Documents, strings.Join(packagespec.KnowledgeExtensions, ", ")))
		}
		// The retrieval settings. Each bound is a thing that actually breaks, not a
		// taste: SentenceSplitter refuses chunk_size <= 0 outright, and refuses an
		// overlap larger than the size with "Got a larger chunk overlap (25) than
		// chunk size (20)". Measured against llama-index 2026-08-26.
		switch {
		case base.ChunkSize < 1:
			errors = add(errors, fmt.Sprintf("knowledge base %q: chunk_size must be at least 1 token, got %d", name, base.ChunkSize))
		case base.ChunkSize > MaxChunkSize:
			errors = add(errors, fmt.Sprintf("knowledge base %q: chunk_size %d is above the %d token limit: a passage that long is an essay, and %d of them is the model's whole context",
				name, base.ChunkSize, MaxChunkSize, base.TopK))
		}
		if base.ChunkOverlap < 0 {
			errors = add(errors, fmt.Sprintf("knowledge base %q: chunk_overlap cannot be negative, got %d", name, base.ChunkOverlap))
		}
		// Equal is legal; larger is what the splitter refuses.
		if base.ChunkSize >= 1 && base.ChunkOverlap > base.ChunkSize {
			errors = add(errors, fmt.Sprintf("knowledge base %q: chunk_overlap %d is larger than chunk_size %d, which the splitter refuses: overlap is how much two neighbouring passages share, so it has to fit inside one",
				name, base.ChunkOverlap, base.ChunkSize))
		}
		// The scores are similarities, not probabilities: measured 0.207 to 0.446 on
		// the example corpus. So the legal range is 0 to 1 but the *useful* range is
		// far narrower, which the warning below is for.
		if base.MinScore < 0 || base.MinScore > 1 {
			errors = add(errors, fmt.Sprintf("knowledge base %q: min_score must be between 0 and 1, got %g", name, base.MinScore))
		}
		switch {
		case base.TopK < 1:
			errors = add(errors, fmt.Sprintf("knowledge base %q: top_k must be at least 1, got %d: a lookup that returns nothing has no reason to exist", name, base.TopK))
		case base.TopK > MaxTopK:
			errors = add(errors, fmt.Sprintf("knowledge base %q: top_k %d is above the limit of %d", name, base.TopK, MaxTopK))
		}
		if !slices.Contains(KnowledgeModes, base.Mode) {
			legal := make([]string, 0, len(KnowledgeModes))
			for _, mode := range KnowledgeModes {
				legal = append(legal, string(mode))
			}
			errors = add(errors, fmt.Sprintf("knowledge base %q has unknown mode %q (supported: %s)",
				name, base.Mode, strings.Join(legal, ", ")))
		}
		// A cutoff only filters something if something is scored, and only the
		// meaning-based half scores. Silently accepting it on a keyword-only base
		// would let an author believe they had filtered when they had not.
		if base.MinScore > 0 && !base.Mode.Scores() {
			errors = add(errors, fmt.Sprintf("knowledge base %q sets min_score with mode %q, which produces no scores to filter: use mode meaning or hybrid, or drop min_score",
				name, base.Mode))
		}
		service, ok := targetcap.LookupEmbeddingService(base.Embed)
		if !ok {
			errors = add(errors, fmt.Sprintf("knowledge base %q has unknown embed %q (supported: %s)",
				name, base.Embed, strings.Join(targetcap.EmbeddingServiceNames(), ", ")))
			continue
		}
		// Named, never carried: the check is that the name is declared, and the
		// message prints the name, which is not a secret.
		//
		// Only when the mode actually embeds. A keyword-only base makes no
		// embedding call at all, so requiring its credential would refuse a
		// package that is complete.
		if base.Mode.Embeds() && service.CredentialEnv != "" && !slices.Contains(agent.Secrets, service.CredentialEnv) {
			errors = add(errors, fmt.Sprintf("knowledge base %q uses embed %q, which needs %s in secrets:",
				name, base.Embed, service.CredentialEnv))
		}
	}
	return errors
}

// knowledgeBudgetWarning reports a knowledge base that sends the model a lot of
// retrieved text per lookup.
//
// A warning, never an error: a large budget is a legitimate choice for a dense
// reference document. It is worth saying because the cost is invisible in the
// authoring file — top_k and chunk_size are two small numbers that multiply into
// the model's context on every single lookup, during a phone call.
func knowledgeBudgetWarning(agent *Agent) string {
	var loud []string
	for _, name := range sortedKeys(agent.Knowledge) {
		base := agent.Knowledge[name]
		if budget := base.TopK * base.ChunkSize; budget > TokenBudgetWarn {
			loud = append(loud, fmt.Sprintf("%s (top_k %d x chunk_size %d = about %d tokens)",
				name, base.TopK, base.ChunkSize, budget))
		}
	}
	if len(loud) == 0 {
		return ""
	}
	return "knowledge base sends the model a lot of retrieved text per lookup, which costs latency on every call: " + strings.Join(loud, ", ")
}

// knowledgeCutoffWarning reports a relevance cutoff high enough to start dropping
// real answers.
//
// This warns rather than refusing because the useful band is a property of the
// author's own corpus. It warns at all because the failure is silent: a cutoff set
// too high does not error, it just makes the agent say it does not know, on every
// question, and the score scale is not what most people expect. Measured on the
// example corpus 2026-08-26, 0.25 costs nothing, 0.30 loses two real answers of
// eighteen, 0.40 loses four, 0.50 loses seven, and nothing scores above 0.63.
func knowledgeCutoffWarning(agent *Agent) string {
	var loud []string
	for _, name := range sortedKeys(agent.Knowledge) {
		if score := agent.Knowledge[name].MinScore; score > MinScoreWarn {
			loud = append(loud, fmt.Sprintf("%s (min_score %g)", name, score))
		}
	}
	if len(loud) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"knowledge base sets min_score above %g, which starts dropping real answers: %s. "+
			"These are similarity scores, not probabilities: in practice they land well below 1, "+
			"so a cutoff near 1 returns nothing at all. Check it against your own documents before shipping",
		MinScoreWarn, strings.Join(loud, ", "))
}

// unusedKnowledgeWarning reports a declared knowledge base no tool searches. A
// warning rather than an error, because the package still compiles and runs; but
// worth saying, because every process start reads and embeds it, which costs
// startup time and embedding calls for something nothing will ever query.
func unusedKnowledgeWarning(agent *Agent) string {
	if len(agent.Knowledge) == 0 {
		return ""
	}
	used := make(map[string]bool, len(agent.Knowledge))
	for _, tool := range agent.Tools {
		if tool.Execution == ToolKnowledge {
			used[tool.KnowledgeBase] = true
		}
	}
	var unused []string
	for _, name := range sortedKeys(agent.Knowledge) {
		if !used[name] {
			unused = append(unused, name)
		}
	}
	if len(unused) == 0 {
		return ""
	}
	return "knowledge base declared but no tool uses it, so it is embedded at every start and never read: " + strings.Join(unused, ", ")
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
		if len(interruption.Protect) > 0 {
			applyCapability(caps, targetcap.FieldInterruptionProtect, provider, row)
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
	// Each route has an exact service set. A LiveKit SIP route runs a LiveKit
	// Server and a SIP service beside the agent, coordinating through a store; the
	// LiveKit connector runs the app plus a LiveKit Server only (no store, no SIP
	// bridge); a Pipecat route runs the agent, with a store only where it keeps a
	// record that outlives a call.
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
		if plan.Key.Provider == ProviderPipecat &&
			(len(reason.Consumers) != 1 || reason.Consumers[0] != "application") {
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
	// The one thing the store is for on this route is the server and the SIP
	// service finding each other.
	if isLiveKitSIP {
		if len(seenReasons) != 1 || !seenReasons["livekit_control_plane"] {
			row.Errors = add(row.Errors, "the LiveKit SIP route's only coordination reason is livekit_control_plane")
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
// validateWebhookBaseURL checks the shape every target agrees on, whether or not
// it reads the field. A literal base URL exists because SLNG's URL validator
// requires a literal hostname; writing one that is not https, has no host, or
// carries a template token would be a 422 at push, so it is refused here.
func validateWebhookBaseURL(name, base string) []string {
	if base == "" {
		return nil
	}
	var errors []string
	if HasTemplate(base) {
		errors = add(errors, fmt.Sprintf("tool %q base_url carries a template token: the scheme and host must be literal, so put the token in path instead", name))
	}
	parsed, err := url.Parse(base)
	switch {
	case err != nil:
		errors = add(errors, fmt.Sprintf("tool %q base_url is not a URL: %v", name, err))
	case parsed.Scheme != "https":
		errors = add(errors, fmt.Sprintf("tool %q base_url must be https, not %q", name, parsed.Scheme))
	case parsed.Host == "":
		errors = add(errors, fmt.Sprintf("tool %q base_url has no host", name))
	case parsed.User != nil:
		errors = add(errors, fmt.Sprintf("tool %q base_url carries userinfo: send credentials through auth: instead, which reaches the platform's own secret store", name))
	case parsed.Fragment != "":
		errors = add(errors, fmt.Sprintf("tool %q base_url carries a fragment, which never reaches a server: remove it", name))
	}
	return errors
}

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

// PromptSuffixMaxLen bounds the authored directive. It is a suffix, not a second
// prompt: a value this long is not something anyone meant to type, and past it
// the field stops being legible as the one line it is.
const PromptSuffixMaxLen = 512

// promptSuffixErrors holds the field's own rules. Each one exists because of a
// specific way the field goes wrong, and every refusal names what to do instead.
func promptSuffixErrors(name string, model ModelDef) []string {
	if model.PromptSuffix == "" {
		return nil
	}
	var errors []string
	// It appends to a system prompt. A listen, speak or turn binding has none, so
	// an authored value there does nothing at all, and a field that silently does
	// nothing is what Principle II exists to prevent.
	if model.Kind != KindThink {
		return add(errors, fmt.Sprintf(
			"model %q is a %s model; prompt_suffix appends to a system prompt, which only a think model sends. Move it to the think binding those prompts run on",
			name, model.Kind))
	}
	if strings.TrimSpace(model.PromptSuffix) == "" {
		errors = add(errors, fmt.Sprintf(
			"model %q prompt_suffix is empty or whitespace: leave the key out to mean off, so a typo cannot read as a deliberate blank",
			name))
	}
	if len(model.PromptSuffix) > PromptSuffixMaxLen {
		errors = add(errors, fmt.Sprintf(
			"model %q prompt_suffix is %d characters and the bound is %d: it is a directive appended to every prompt this binding sends, not a second prompt. Put the wording in the instructions file",
			name, len(model.PromptSuffix), PromptSuffixMaxLen))
	}
	// The router substitutes placeholders from a variable snapshot it is handed.
	// One arriving from a suffix rather than from an authored prompt would be sent
	// with no value, and the router answers that with a 422 that ends the call.
	if templatePattern.MatchString(model.PromptSuffix) {
		errors = add(errors, fmt.Sprintf(
			"model %q prompt_suffix contains a {{...}} placeholder: the router is given a snapshot of the names the prompts reference, this name would not be in it, and the request comes back 422 mid-call. Use literal text",
			name))
	}
	return errors
}

// turnDeadFieldWarnings names a field authored on a turn binding that no target
// reads. Three of them were accepted in silence until 2026-08-27: an author could
// write `params: {alpha: 0.5}` on a turn binding, compile clean, and get an agent
// that ignored it — with nothing in the report to say so.
//
// The neighbours already had this covered: `prompt_suffix` is refused on a turn
// binding and `endpoint_env` warns. These three just missed it.
//
// A warning rather than a refusal, deliberately. A package carrying one of these
// today is not broken, it is carrying a value that was never doing anything, and
// refusing would fail a compile that used to pass over a field that changed no
// behaviour either way. Each message names what to use instead, because "this
// does nothing" tells an author they are wrong and not what to write.
//
// If turn params ever do get forwarded, the `params` line here is what to delete.
func turnDeadFieldWarnings(name string, model ModelDef) []string {
	if model.Kind != KindTurn {
		return nil
	}
	var warnings []string
	if len(model.Params) > 0 {
		warnings = add(warnings, fmt.Sprintf(
			"model %q is a turn model and sets params (%s), which no target reads: turn params are not forwarded to either framework. Use pace for the turn window and endpointing_delay for the silence window; there is no way to reach an individual framework parameter from a package today",
			name, strings.Join(sortedKeys(model.Params), ", ")))
	}
	if model.AgentID != "" {
		warnings = add(warnings, fmt.Sprintf(
			"model %q is a turn model and sets agent_id, which no target reads: agent_id scopes the SLNG Context Router's cache and belongs on the think binding that names the router",
			name))
	}
	if len(model.Fallback) > 0 {
		warnings = add(warnings, fmt.Sprintf(
			"model %q is a turn model and sets fallback, which no target reads: fallback is a think and listen field. A turn detector has no fallback chain on either code target",
			name))
	}
	return warnings
}

// paceErrors refuses a pace that no target can map and a pace on a role that has
// no turn to time. The legal values come from internal/target, which owns the
// table that gives each one its numbers, so a value added there reaches this
// message without a second list to keep in step.
func paceErrors(name string, model ModelDef) []string {
	if model.Pace == "" {
		return nil
	}
	if model.Kind != KindTurn {
		return []string{fmt.Sprintf(
			"model %q pace is a turn-model field: pace belongs on a turn binding, not a %s binding",
			name, model.Kind)}
	}
	if !slices.Contains(targetcap.PaceValues(), string(model.Pace)) {
		return []string{fmt.Sprintf(
			"model %q pace: %q is not a pace; use %s",
			name, model.Pace, strings.Join(targetcap.PaceValues(), ", "))}
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
	if model.EndpointingDelay != "" {
		if model.Kind != KindTurn {
			errors = add(errors, fmt.Sprintf("model %q endpointing_delay is a turn-model field", name))
		} else {
			errors = append(errors, validateDuration(fmt.Sprintf("model %q endpointing_delay", name), model.EndpointingDelay)...)
		}
	}
	errors = append(errors, promptSuffixErrors(name, model)...)
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
