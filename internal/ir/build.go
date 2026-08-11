package ir

import (
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	packagespec "github.com/slng/unmute/internal/spec"
	targetcap "github.com/slng/unmute/internal/target"
)

var (
	namePattern        = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
	e164Pattern        = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)
	sipDestinationPath = regexp.MustCompile(`^sips?:[^@\s]+@[^@\s]+$`)
)

// Build resolves a decoded package into the target-independent IR.
func Build(pkg *packagespec.Package) (*Agent, error) {
	if pkg == nil {
		return nil, fmt.Errorf("build: nil package")
	}
	if err := checkNames(pkg); err != nil {
		return nil, err
	}
	if err := checkSecrets(pkg); err != nil {
		return nil, err
	}
	if _, ok := pkg.Agent.Agents[pkg.Agent.EntryAgent]; !ok {
		return nil, missing(pkg, "agent.yaml", "entry_agent", pkg.Agent.EntryAgent)
	}
	if pkg.Agent.Tracing != nil && pkg.Agent.Tracing.Provider != "langfuse" {
		return nil, fmt.Errorf("%s: unsupported tracing provider %q; supported provider: langfuse", pkg.Location("agent.yaml", "tracing:"), pkg.Agent.Tracing.Provider)
	}

	models, err := buildModels(pkg)
	if err != nil {
		return nil, err
	}
	if err := checkModelReferences(pkg, models); err != nil {
		return nil, err
	}
	listenName, err := selectRoleModel(pkg, pkg.Agent.Models.Listen, pkg.Agent.Listen, "listen")
	if err != nil {
		return nil, err
	}
	turnName, err := selectRoleModel(pkg, pkg.Agent.Models.Turn, pkg.Agent.Turn, "turn")
	if err != nil {
		return nil, err
	}
	out := &Agent{
		Version:      pkg.Agent.Version,
		EntryAgent:   pkg.Agent.EntryAgent,
		Models:       models,
		Listen:       listenName,
		Turn:         turnName,
		Variables:    make(map[string]Variable, len(pkg.Agent.Variables)),
		Secrets:      slices.Sorted(slices.Values(pkg.Agent.Secrets)),
		Agents:       make(map[string]AgentDef, len(pkg.Agent.Agents)),
		Tasks:        make(map[string]Task, len(pkg.Agent.Tasks)),
		TaskGroups:   make(map[string]TaskGroup, len(pkg.Agent.TaskGroups)),
		Controls:     make(map[string]Control, len(pkg.Agent.Controls)),
		Tools:        make(map[string]Tool, len(pkg.Tools)),
		Conversation: buildConversation(pkg.Agent.Conversation),
		Channels:     make(map[string]Channel, len(pkg.Agent.Channels)),
		Connections:  make(map[string]Connection, len(pkg.Connections)),
		Targets:      make(map[string]Target, len(pkg.Targets)),
	}
	if pkg.Agent.Tracing != nil {
		out.Tracing = &Tracing{Provider: pkg.Agent.Tracing.Provider}
	}
	for name, variable := range pkg.Agent.Variables {
		out.Variables[name] = Variable{
			Type: PrimitiveType(variable.Type), Default: variable.Default,
			Source: VariableSource(variable.Source), Description: variable.Description,
		}
	}
	for name, tool := range pkg.Tools {
		built := buildTool(name, tool)
		built.HandlerSource = pkg.Handlers[built.Handler]
		out.Tools[name] = built
	}
	for name, channel := range pkg.Agent.Channels {
		out.Channels[name] = Channel{
			Kind: ChannelKind(channel.Kind), Inbound: channel.Inbound, Outbound: channel.Outbound,
			RequiredControls: channel.RequiredControls, OnVoicemail: VoicemailAction(channel.OnVoicemail),
		}
	}
	for _, name := range sortedKeys(pkg.Connections) {
		raw := pkg.Connections[name]
		path := filepath.Join("connections", name+".yaml")
		if raw.Kind != "telephony" {
			return nil, fmt.Errorf("%s: connection %q kind must be telephony", pkg.Location(path, "kind:"), name)
		}
		if len(raw.Environment) == 0 {
			return nil, fmt.Errorf("%s: connection %q environment must not be empty", pkg.Location(path, "environment:"), name)
		}
		for _, key := range sortedKeys(raw.Environment) {
			value := raw.Environment[key]
			if !namePattern.MatchString(key) {
				return nil, fmt.Errorf("%s: connection %q environment key %q must be lowercase snake_case", pkg.Location(path, key), name, key)
			}
			if !envNamePattern.MatchString(value) {
				return nil, fmt.Errorf("%s: connection %q environment value for %q must be an environment variable name", pkg.Location(path, value), name, key)
			}
		}
		out.Connections[name] = Connection{Kind: raw.Kind, Environment: maps.Clone(raw.Environment)}
	}
	if pkg.Agent.Capacity != nil {
		out.Capacity = &Capacity{
			PeakSessions: pkg.Agent.Capacity.PeakSessions, MaxSessions: pkg.Agent.Capacity.MaxSessions,
			PeakStartsPerSecond: pkg.Agent.Capacity.PeakStartsPerSecond,
			AvgSessionDuration:  Duration(pkg.Agent.Capacity.AvgSessionDuration),
		}
	}

	for _, name := range sortedKeys(pkg.Agent.Agents) {
		raw := pkg.Agent.Agents[name]
		// model/voice references and their kinds are resolved in resolveModelKinds.
		if err := checkToolRefs(pkg, raw.Tools); err != nil {
			return nil, err
		}
		instructions, ok := pkg.Markdown[raw.Instructions]
		if !ok {
			return nil, missing(pkg, "agent.yaml", "instructions", raw.Instructions)
		}
		out.Agents[name] = AgentDef{Instructions: instructions, Model: raw.Model, Voice: raw.Voice, Tools: raw.Tools}
	}

	for _, name := range sortedKeys(pkg.Agent.Tasks) {
		raw := pkg.Agent.Tasks[name]
		if raw.Model != "" {
			if _, ok := out.Models[raw.Model]; !ok {
				return nil, missing(pkg, "agent.yaml", "model", raw.Model)
			}
		}
		if err := checkToolRefs(pkg, raw.Tools); err != nil {
			return nil, err
		}
		if raw.Context.Summarizer != "" {
			if _, ok := out.Models[raw.Context.Summarizer]; !ok {
				return nil, missing(pkg, "agent.yaml", "summarizer", raw.Context.Summarizer)
			}
		}
		instructions, ok := pkg.Markdown[raw.Instructions]
		if !ok {
			return nil, missing(pkg, "agent.yaml", "instructions", raw.Instructions)
		}
		result, err := buildResult(raw.Result)
		if err != nil {
			return nil, fmt.Errorf("%s: task %q: %w", pkg.Location("agent.yaml", name), name, err)
		}
		out.Tasks[name] = Task{
			Instructions: instructions, Tools: raw.Tools, Model: raw.Model, Result: result,
			Context: buildTaskContext(raw.Context),
		}
	}

	for _, name := range sortedKeys(pkg.Agent.TaskGroups) {
		raw := pkg.Agent.TaskGroups[name]
		for _, step := range raw.Steps {
			if _, ok := out.Tasks[step]; !ok {
				return nil, missing(pkg, "agent.yaml", "task", step)
			}
		}
		if raw.ThenTarget != "" {
			if _, ok := out.Agents[raw.ThenTarget]; !ok {
				return nil, missing(pkg, "agent.yaml", "then_target", raw.ThenTarget)
			}
		}
		merge := GroupMerge(raw.Merge)
		if merge == "" {
			merge = GroupMergeResults
		}
		out.TaskGroups[name] = TaskGroup{
			Steps: raw.Steps, ContextScope: ContextScope(raw.ContextScope), Then: GroupThen(raw.Then),
			ThenTarget: raw.ThenTarget, Merge: merge,
		}
	}

	used := usedModelNames(pkg, models)
	for _, name := range sortedKeys(pkg.Targets) {
		target, err := buildTarget(pkg, name, pkg.Targets[name], out, used)
		if err != nil {
			return nil, err
		}
		out.Targets[name] = target
	}
	for _, name := range sortedKeys(pkg.Agent.Controls) {
		control, err := buildControl(pkg, pkg.Agent.Controls[name], out)
		if err != nil {
			return nil, fmt.Errorf("%s: control %q: %w", pkg.Location("agent.yaml", name), name, err)
		}
		out.Controls[name] = control
	}
	if err := checkInject(pkg); err != nil {
		return nil, err
	}
	if err := checkTemplates(pkg, out); err != nil {
		return nil, err
	}
	return out, nil
}

func checkNames(pkg *packagespec.Package) error {
	modelNames := slices.Concat(
		sortedKeys(pkg.Agent.Models.Think), sortedKeys(pkg.Agent.Models.Speak),
		sortedKeys(pkg.Agent.Models.Listen), sortedKeys(pkg.Agent.Models.Turn),
	)
	sets := []struct {
		kind  string
		names []string
	}{
		{"model", modelNames},
		{"variable", sortedKeys(pkg.Agent.Variables)}, {"agent", sortedKeys(pkg.Agent.Agents)},
		{"task", sortedKeys(pkg.Agent.Tasks)}, {"task group", sortedKeys(pkg.Agent.TaskGroups)},
		{"control", sortedKeys(pkg.Agent.Controls)}, {"tool", sortedKeys(pkg.Tools)},
	}
	for _, set := range sets {
		for _, name := range set.names {
			if !namePattern.MatchString(name) {
				return fmt.Errorf("%s: %s name %q must be lowercase snake_case and cannot start with underscore", pkg.Location("agent.yaml", name), set.kind, name)
			}
		}
	}
	for name := range pkg.Agent.Controls {
		if _, ok := pkg.Tools[name]; ok {
			return fmt.Errorf("%s: tool and control name %q collide", pkg.Location("agent.yaml", name), name)
		}
	}
	// The capture tool is generated whenever a conversation variable exists, so
	// its name cannot also be a package tool or control (V7).
	if _, ok := pkg.Tools[CaptureToolName]; ok {
		return fmt.Errorf("%s: tool name %q is reserved: unmute generates %s for source: conversation variables", pkg.Location("agent.yaml", CaptureToolName), CaptureToolName, CaptureToolName)
	}
	if _, ok := pkg.Agent.Controls[CaptureToolName]; ok {
		return fmt.Errorf("%s: control name %q is reserved: unmute generates %s for source: conversation variables", pkg.Location("agent.yaml", CaptureToolName), CaptureToolName, CaptureToolName)
	}
	for _, name := range sortedKeys(pkg.Connections) {
		if !namePattern.MatchString(name) {
			path := filepath.Join("connections", name+".yaml")
			return fmt.Errorf("%s: connection name %q must be lowercase snake_case and cannot start with underscore", path, name)
		}
	}
	for _, raw := range pkg.Targets {
		for name := range raw.Destinations {
			if !namePattern.MatchString(name) {
				return fmt.Errorf("%s: destination name %q must be lowercase snake_case and cannot start with underscore", pkg.Location("targets.yaml", name), name)
			}
		}
	}
	return nil
}

// buildModels flattens the four kind sections into one name-keyed map. Kind is
// the section; names share one namespace across sections (N15).
func buildModels(pkg *packagespec.Package) (map[string]ModelDef, error) {
	sections := []struct {
		kind    ModelKind
		entries map[string]packagespec.ModelDef
	}{
		{KindThink, pkg.Agent.Models.Think},
		{KindSpeak, pkg.Agent.Models.Speak},
		{KindListen, pkg.Agent.Models.Listen},
		{KindTurn, pkg.Agent.Models.Turn},
	}
	result := make(map[string]ModelDef)
	for _, section := range sections {
		memo := make(map[string][]string) // chains never cross sections
		for _, name := range sortedKeys(section.entries) {
			if prev, ok := result[name]; ok {
				return nil, fmt.Errorf("%s: model name %q appears in both %s and %s; names share one namespace", pkg.Location("agent.yaml", name), name, prev.Kind, section.kind)
			}
			var fallback []string
			switch section.kind {
			case KindThink, KindListen:
				var err error
				fallback, err = flattenFallback(pkg, section.entries, name, nil, memo)
				if err != nil {
					return nil, err
				}
			default:
				if len(section.entries[name].Fallback) > 0 {
					return nil, fmt.Errorf("%s: model %q has fallback but is a %s model (fallback is legal on think and listen models)", pkg.Location("agent.yaml", name), name, section.kind)
				}
			}
			result[name] = convertModelDef(section.entries[name], section.kind, fallback)
		}
	}
	return result, nil
}

// checkModelReferences enforces that every reference lands in the right section
// (V22). Unreferenced entries are legal palette alternates, never an error.
func checkModelReferences(pkg *packagespec.Package, models map[string]ModelDef) error {
	check := func(name string, kind ModelKind, ref string) error {
		if name == "" {
			return nil
		}
		def, ok := models[name]
		if !ok {
			return missing(pkg, "agent.yaml", ref, name)
		}
		if def.Kind != kind {
			return fmt.Errorf("%s: %s %q is a %s model, not a %s model", pkg.Location("agent.yaml", name), ref, name, def.Kind, kind)
		}
		return nil
	}
	for _, agentName := range sortedKeys(pkg.Agent.Agents) {
		agent := pkg.Agent.Agents[agentName]
		if err := check(agent.Model, KindThink, "model"); err != nil {
			return err
		}
		if err := check(agent.Voice, KindSpeak, "voice"); err != nil {
			return err
		}
	}
	for _, taskName := range sortedKeys(pkg.Agent.Tasks) {
		task := pkg.Agent.Tasks[taskName]
		if err := check(task.Model, KindThink, "model"); err != nil {
			return err
		}
		if err := check(task.Context.Summarizer, KindThink, "summarizer"); err != nil {
			return err
		}
	}
	for _, controlName := range sortedKeys(pkg.Agent.Controls) {
		control := pkg.Agent.Controls[controlName]
		if control.Context != nil {
			if err := check(control.Context.Summarizer, KindThink, "summarizer"); err != nil {
				return err
			}
		}
	}
	return nil
}

// selectRoleModel resolves the listen/turn selection (N15 palette): an explicit
// pointer must name a section entry; with no pointer the sole chain head
// selects itself (an entry named only in another entry's fallback list is part
// of that chain, not a selection candidate — T16); 2+ heads with no pointer
// fail loud naming the candidates.
func selectRoleModel(pkg *packagespec.Package, section map[string]packagespec.ModelDef, pointer, role string) (string, error) {
	if pointer != "" {
		if _, ok := section[pointer]; !ok {
			return "", fmt.Errorf("%s: %s %q does not name a models.%s entry", pkg.Location("agent.yaml", pointer), role, pointer, role)
		}
		return pointer, nil
	}
	inChain := make(map[string]bool)
	for _, def := range section {
		for _, name := range def.Fallback {
			inChain[name] = true
		}
	}
	var heads []string
	for _, name := range sortedKeys(section) {
		if !inChain[name] {
			heads = append(heads, name)
		}
	}
	switch len(heads) {
	case 0:
		return "", nil
	case 1:
		return heads[0], nil
	default:
		return "", fmt.Errorf("agent.yaml: %d %s models defined (%s); select one with %s: <name>", len(heads), role, strings.Join(heads, ", "), role)
	}
}

// usedModelNames collects every model the package actually uses: agent and task
// references, summarizers, fallback chains of used think models, and the
// listen/turn selections. Palette alternates stay out (never compiled).
func usedModelNames(pkg *packagespec.Package, models map[string]ModelDef) map[string]bool {
	used := make(map[string]bool)
	add := func(name string) {
		if name != "" {
			used[name] = true
		}
	}
	for _, agent := range pkg.Agent.Agents {
		add(agent.Model)
		add(agent.Voice)
	}
	for _, task := range pkg.Agent.Tasks {
		add(task.Model)
		add(task.Context.Summarizer)
	}
	for _, control := range pkg.Agent.Controls {
		if control.Context != nil {
			add(control.Context.Summarizer)
		}
	}
	for name := range maps.Clone(used) {
		for _, fallback := range models[name].Fallback {
			add(fallback)
		}
	}
	return used
}

// convertModelDef types a raw model definition and derives its placement (N15).
func convertModelDef(raw packagespec.ModelDef, kind ModelKind, fallback []string) ModelDef {
	return ModelDef{
		Kind: kind, Provider: raw.Provider, Model: raw.Model, Voice: raw.Voice,
		Speed: raw.Speed, Language: raw.Language, Temperature: raw.Temperature,
		TopP: raw.TopP, TopK: raw.TopK, EndpointEnv: raw.EndpointEnv,
		Placement: derivePlacement(raw), SemanticEndpointing: SemanticEndpointing(raw.SemanticEndpointing),
		Params: raw.Params, Fallback: fallback, Description: raw.Description,
	}
}

// derivePlacement: explicit wins, else provider local means local. A hosted
// entry is api even when the provider is left implicit (a managed model named by
// id alone, like an ElevenLabs supported-list LLM). Only a pure settings-only
// entry (no provider, no model, no voice) has no placement, so an integrated
// listen binding stays identity-free (N15).
func derivePlacement(raw packagespec.ModelDef) Placement {
	if raw.Placement != "" {
		return Placement(raw.Placement)
	}
	if raw.Provider == "local" {
		return PlacementLocal
	}
	if raw.Provider == "" && raw.Model == "" && raw.Voice == "" {
		return ""
	}
	return PlacementAPI
}

// flattenFallback flattens a chain within one section: a fallback names an
// entry of the same kind (think falls back to think, listen to listen, T16).
func flattenFallback(pkg *packagespec.Package, section map[string]packagespec.ModelDef, name string, stack []string, memo map[string][]string) ([]string, error) {
	if value, ok := memo[name]; ok {
		return value, nil
	}
	if slices.Contains(stack, name) {
		return nil, fmt.Errorf("%s: fallback cycle: %s", pkg.Location("agent.yaml", name), strings.Join(append(stack, name), " -> "))
	}
	raw, ok := section[name]
	if !ok {
		return nil, missing(pkg, "agent.yaml", "fallback", name)
	}
	stack = append(stack, name)
	seen := make(map[string]bool)
	var flat []string
	for _, next := range raw.Fallback {
		if _, ok := section[next]; !ok {
			return nil, missing(pkg, "agent.yaml", "fallback", next)
		}
		if !seen[next] {
			flat = append(flat, next)
			seen[next] = true
		}
		children, err := flattenFallback(pkg, section, next, stack, memo)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			if !seen[child] {
				flat = append(flat, child)
				seen[child] = true
			}
		}
	}
	memo[name] = flat
	return flat, nil
}

// buildTool folds the authoring block (spec) into the flat resolved tool the
// generators read (compiler.md V36). spec.Load has already rejected zero and
// two-or-more blocks, so at most one arm sets the execution kind.
func buildTool(name string, raw packagespec.Tool) Tool {
	tool := Tool{
		Description: raw.Description, Input: raw.Input, Output: raw.Output,
		Execution: ToolExecution(raw.ExecutionKind()), Inject: raw.Inject,
	}
	switch {
	case raw.Webhook != nil:
		tool.URLEnv = raw.Webhook.URLEnv
		tool.Path = raw.Webhook.Path
		tool.Auth = buildToolAuth(raw.Webhook.Auth)
	case raw.Local != nil:
		tool.Handler = raw.Local.Handler
		if tool.Handler == "" {
			tool.Handler = filepath.Join("tools", name+".py")
		}
	case raw.MCP != nil:
		tool.URLEnv = raw.MCP.URLEnv
	case raw.Builtin != nil:
		tool.Builtin = raw.Builtin.ID
		tool.Instructions = raw.Builtin.Instructions
	}
	tool.Interruption = ToolInterruption(raw.Interruption)
	if tool.Interruption == "" {
		tool.Interruption = ToolProviderDefault
	}
	tool.Effect = ToolEffect(raw.Effect)
	// A builtin tool takes its default effect and description from the prebuilt
	// registry; Validate rejects an unknown id or a conflicting effect (V1, V5).
	if tool.Execution == ToolBuiltin {
		if prebuilt, ok := targetcap.LookupPrebuilt(tool.Builtin); ok {
			if tool.Effect == "" {
				tool.Effect = ToolEffect(prebuilt.Effect)
			}
			if tool.Description == "" {
				tool.Description = prebuilt.DefaultDescription
			}
		}
	}
	if tool.Effect == "" {
		tool.Effect = ToolReturnsData
	}
	return tool
}

// buildToolAuth resolves an auth block: the scheme's own default lands here so
// every generator reads settled values. An unknown type passes through
// unchanged for Validate to reject by name.
func buildToolAuth(raw *packagespec.ToolAuth) *ToolAuth {
	if raw == nil {
		return nil
	}
	auth := &ToolAuth{Type: ToolAuthType(raw.Type), TokenEnv: raw.TokenEnv, Header: raw.Header}
	if auth.Type == ToolAuthAPIKey && auth.Header == "" {
		auth.Header = DefaultAPIKeyHeader
	}
	return auth
}

func buildResult(raw map[string]any) (map[string]ResultField, error) {
	result := make(map[string]ResultField, len(raw))
	for name, value := range raw {
		switch value := value.(type) {
		case string:
			result[name] = ResultField{Type: PrimitiveType(value)}
		case map[string]any:
			if enumValue, ok := value["enum"]; ok && len(value) == 1 {
				values, err := stringSlice(enumValue)
				if err != nil {
					return nil, fmt.Errorf("result %q enum: %w", name, err)
				}
				result[name] = ResultField{Type: PrimitiveString, Enum: values}
			} else {
				result[name] = ResultField{Schema: value}
			}
		default:
			return nil, fmt.Errorf("result %q must be a primitive type, enum, or JSON Schema object", name)
		}
	}
	return result, nil
}

func buildTaskContext(raw packagespec.TaskContext) TaskContext {
	return TaskContext{
		History: History(raw.History), MaxMessages: raw.MaxMessages, Summarizer: raw.Summarizer,
		IncludeToolCalls: raw.IncludeToolCalls,
	}
}

func buildControl(pkg *packagespec.Package, raw packagespec.Control, agent *Agent) (Control, error) {
	kind := ControlKind(raw.Kind)
	if field := unexpectedControlField(raw, kind); field != "" {
		return nil, fmt.Errorf("field %q is illegal with control kind %q", field, raw.Kind)
	}
	task, group := stringValue(raw.Task), stringValue(raw.Group)
	to := stringValue(raw.To)
	switch kind {
	case ControlDelegate:
		if (task == "") == (group == "") {
			return nil, fmt.Errorf("delegate needs exactly one of task or group")
		}
		if task != "" {
			if _, ok := agent.Tasks[task]; !ok {
				return nil, missing(pkg, "agent.yaml", "task", task)
			}
		} else {
			if _, ok := agent.TaskGroups[group]; !ok {
				return nil, missing(pkg, "agent.yaml", "group", group)
			}
			if len(raw.Assign) > 0 {
				return nil, fmt.Errorf("assign is legal on task delegates only")
			}
		}
		if err := checkAssignments(raw, agent); err != nil {
			return nil, err
		}
		return &Delegate{Kind: ControlDelegate, When: raw.When, Task: task, Group: group, Assign: raw.Assign}, nil
	case ControlAgentTransfer:
		if _, ok := agent.Agents[to]; !ok {
			return nil, missing(pkg, "agent.yaml", "to", to)
		}
		for _, name := range raw.Requires {
			if _, ok := agent.Variables[name]; !ok {
				return nil, missing(pkg, "agent.yaml", "requires", name)
			}
		}
		context, err := buildTransferContext(pkg, raw.Context, agent)
		if err != nil {
			return nil, err
		}
		return &AgentTransfer{Kind: ControlAgentTransfer, When: raw.When, To: to, Requires: raw.Requires, Context: context}, nil
	case ControlHumanTransfer:
		transfer, err := buildHumanTransfer(raw)
		if err != nil {
			return nil, err
		}
		for name, target := range agent.Targets {
			if _, ok := target.Destinations[transfer.(*HumanTransfer).Destination]; !ok {
				return nil, fmt.Errorf("destination %q is missing from target %q", transfer.(*HumanTransfer).Destination, name)
			}
		}
		return transfer, nil
	default:
		return nil, fmt.Errorf("unknown control kind %q", raw.Kind)
	}
}

// buildHumanTransfer resolves the `cold:`/`warm:` block into the IR control
// (SCHEMA N25). The shape is the block name, so zero blocks and two blocks are
// both errors here; `on_unavailable` resolves to its default so no driver reads
// an empty value.
func buildHumanTransfer(raw packagespec.Control) (Control, error) {
	transfer := &HumanTransfer{Kind: ControlHumanTransfer, When: raw.When}
	switch {
	case raw.Cold != nil && raw.Warm != nil:
		return nil, fmt.Errorf("human_transfer declares both `cold:` and `warm:`: a transfer has exactly one shape")
	case raw.Cold != nil:
		transfer.Mode = TransferCold
		transfer.Destination = raw.Cold.Destination
		transfer.RingTimeout = Duration(raw.Cold.RingTimeout)
		transfer.OnUnavailable = OnUnavailable(raw.Cold.OnUnavailable)
	case raw.Warm != nil:
		transfer.Mode = TransferWarm
		transfer.Destination = raw.Warm.Destination
		transfer.Briefing = raw.Warm.Briefing
		transfer.RingTimeout = Duration(raw.Warm.RingTimeout)
		transfer.OnUnavailable = OnUnavailable(raw.Warm.OnUnavailable)
	default:
		// A block written with no body decodes to nothing, so it lands here too:
		// name the field the block must carry rather than only the missing key.
		return nil, fmt.Errorf("human_transfer has no shape block: add a `cold:` or `warm:` block with a `destination:`")
	}
	if transfer.Destination == "" {
		return nil, fmt.Errorf("human_transfer `%s:` block requires a `destination:`", transfer.Mode)
	}
	if transfer.OnUnavailable == "" {
		transfer.OnUnavailable = OnUnavailableReturn
	}
	return transfer, nil
}

func unexpectedControlField(raw packagespec.Control, kind ControlKind) string {
	fields := map[string]bool{
		"task": raw.Task != nil, "group": raw.Group != nil, "assign": raw.Assign != nil,
		"to": raw.To != nil, "requires": raw.Requires != nil, "context": raw.Context != nil,
		"cold": raw.Cold != nil, "warm": raw.Warm != nil,
	}
	allowed := map[ControlKind]map[string]bool{
		ControlDelegate:      {"task": true, "group": true, "assign": true},
		ControlAgentTransfer: {"to": true, "requires": true, "context": true},
		ControlHumanTransfer: {"cold": true, "warm": true},
	}[kind]
	for _, field := range slices.Sorted(maps.Keys(fields)) {
		if fields[field] && !allowed[field] {
			return field
		}
	}
	return ""
}

func checkAssignments(raw packagespec.Control, agent *Agent) error {
	taskName := stringValue(raw.Task)
	if taskName == "" {
		return nil
	}
	task := agent.Tasks[taskName]
	for variable, path := range raw.Assign {
		want, ok := agent.Variables[variable]
		if !ok {
			return fmt.Errorf("assign variable %q does not resolve", variable)
		}
		fieldName, ok := strings.CutPrefix(path, "result.")
		if !ok {
			return fmt.Errorf("assign %q must use result.<field>", path)
		}
		field, ok := task.Result[fieldName]
		if !ok {
			return fmt.Errorf("assign result field %q does not resolve", fieldName)
		}
		if field.Schema != nil || field.Type != want.Type {
			return fmt.Errorf("assign result %q type does not match variable %q", fieldName, variable)
		}
	}
	return nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func buildTransferContext(pkg *packagespec.Package, raw *packagespec.TransferContext, agent *Agent) (TransferContext, error) {
	if raw == nil {
		return TransferContext{}, nil
	}
	if raw.Summarizer != "" {
		if _, ok := agent.Models[raw.Summarizer]; !ok {
			return TransferContext{}, missing(pkg, "agent.yaml", "summarizer", raw.Summarizer)
		}
	}
	selection, err := buildVariableSelection(raw.Variables)
	if err != nil {
		return TransferContext{}, err
	}
	for _, name := range selection.Names {
		if _, ok := agent.Variables[name]; !ok {
			return TransferContext{}, missing(pkg, "agent.yaml", "variable", name)
		}
	}
	return TransferContext{
		TaskContext: TaskContext{
			History: History(raw.History), MaxMessages: raw.MaxMessages, Summarizer: raw.Summarizer,
			IncludeToolCalls: raw.IncludeToolCalls,
		},
		Variables: selection,
	}, nil
}

func buildVariableSelection(value any) (VariableSelection, error) {
	if value == "all" {
		return VariableSelection{All: true}, nil
	}
	values, err := stringSlice(value)
	if err != nil {
		return VariableSelection{}, fmt.Errorf("context variables must be all or a list of names")
	}
	return VariableSelection{Names: values}, nil
}

func stringSlice(value any) ([]string, error) {
	switch values := value.(type) {
	case []string:
		return values, nil
	case []any:
		result := make([]string, len(values))
		for i, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("item %d is not a string", i)
			}
			result[i] = text
		}
		return result, nil
	default:
		return nil, fmt.Errorf("not a string list")
	}
}

// buildTarget resolves a target's effective models (per-target override else the
// agent definition, N15) into the derived Bindings view the generators consume.
// Only used models and the listen/turn selections resolve; palette alternates
// stay inert.
func buildTarget(pkg *packagespec.Package, name string, raw packagespec.Target, agent *Agent, used map[string]bool) (Target, error) {
	for _, key := range sortedKeys(raw.Models) {
		if _, ok := agent.Models[key]; !ok {
			return Target{}, fmt.Errorf("%s: target %q overrides %q, which is not a defined model", pkg.Location("targets.yaml", key), name, key)
		}
	}
	for _, destination := range sortedKeys(raw.Destinations) {
		value := raw.Destinations[destination]
		if !validDestination(value) {
			return Target{}, fmt.Errorf("%s: destination %q must be an E.164 phone number or SIP URI", pkg.Location("targets.yaml", destination), destination)
		}
	}
	if raw.Connection != "" {
		if _, ok := agent.Connections[raw.Connection]; !ok {
			return Target{}, missing(pkg, "targets.yaml", "connection", raw.Connection)
		}
	}
	telephony := hasTelephonyChannel(agent)
	if telephony && (raw.Provider == string(ProviderLiveKit) || raw.Provider == string(ProviderPipecat)) && raw.Connection == "" {
		return Target{}, fmt.Errorf("%s: target %q requires connection for telephony", pkg.Location("targets.yaml", name+":"), name)
	}
	if !telephony && raw.Connection != "" {
		return Target{}, fmt.Errorf("%s: target %q sets connection but has no telephony channel", pkg.Location("targets.yaml", "connection:"), name)
	}
	built := Target{
		Name: name, Provider: Provider(raw.Provider), Version: raw.Version, Pins: raw.Pins,
		SDKLanguage: raw.SDKLanguage, Transport: raw.Transport, Carrier: raw.Carrier, Connection: raw.Connection,
		DeploymentRegion: raw.DeploymentRegion,
		Models:           resolveBindings(agent, used, raw.Models),
		Destinations:     raw.Destinations,
	}
	if raw.Connection != "" && telephony {
		built.Telephony = buildTelephonyPlan(pkg, agent, built)
		if err := validateTelephonyEnvironment(pkg, built.Telephony); err != nil {
			return Target{}, err
		}
	}
	return built, nil
}

func validateTelephonyEnvironment(pkg *packagespec.Package, plan *TelephonyPlan) error {
	key := targetcap.TelephonyKey{Provider: targetcap.Provider(plan.Key.Provider), Transport: plan.Key.Transport, Carrier: plan.Key.Carrier}
	required, optional, ok := targetcap.TelephonyEnvironment(key)
	if !ok || len(required)+len(optional) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	path := filepath.Join("connections", plan.Connection+".yaml")
	for _, name := range append(required, optional...) {
		allowed[name] = true
	}
	for _, name := range required {
		if plan.Environment[name] == "" {
			return fmt.Errorf("%s: connection %q requires environment key %q for route (%s, %s, %s)", pkg.Location(path, "environment:"), plan.Connection, name, plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier)
		}
	}
	for _, name := range sortedKeys(plan.Environment) {
		if !allowed[name] {
			return fmt.Errorf("%s: connection %q environment key %q is not accepted by route (%s, %s, %s)", pkg.Location(path, name), plan.Connection, name, plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier)
		}
	}
	return nil
}

func hasTelephonyChannel(agent *Agent) bool {
	for _, channel := range agent.Channels {
		if channel.Kind == ChannelTelephony {
			return true
		}
	}
	return false
}

func buildTelephonyPlan(pkg *packagespec.Package, agent *Agent, resolved Target) *TelephonyPlan {
	features := map[targetcap.TelephonyFeature]bool{targetcap.TelephonyRouteSelected: true}
	var channels []string
	for _, name := range slices.Sorted(maps.Keys(agent.Channels)) {
		channel := agent.Channels[name]
		if channel.Kind != ChannelTelephony {
			continue
		}
		channels = append(channels, name)
		if channel.Inbound != nil && *channel.Inbound {
			features[targetcap.TelephonyInbound] = true
		}
		if channel.Outbound != nil && *channel.Outbound {
			features[targetcap.TelephonyOutbound] = true
			if channel.OnVoicemail != "" {
				features[targetcap.TelephonyFeature(targetcap.VoicemailDetection)] = true
			}
		}
		for _, control := range channel.RequiredControls {
			features[targetcap.TelephonyFeature(control)] = true
		}
	}
	for _, control := range pkg.Agent.Controls {
		if control.Kind != string(ControlHumanTransfer) {
			continue
		}
		// The shape block is the feature: SCHEMA N25 removed the briefing mode
		// enum, so free-text briefing rides the warm row and resolves nothing.
		if shape := control.TransferShape(); shape != "" {
			features[targetcap.TelephonyFeature(shape+"_transfer")] = true
		}
	}
	sources := make(map[string]VariableSource)
	for name, variable := range agent.Variables {
		// Only runtime-owned sources resolve against the route: a dispatched or
		// model-captured value has nothing to do with the carrier.
		if !IsSystemSource(variable.Source) {
			continue
		}
		sources[name] = variable.Source
		features[targetcap.TelephonyFeature(targetcap.TelephonySourcePrefix+string(variable.Source))] = true
	}
	key := targetcap.TelephonyKey{
		Provider: targetcap.Provider(resolved.Provider), Transport: resolved.Transport, Carrier: resolved.Carrier,
	}
	route := targetcap.TelephonyRoutes()[key]
	evidence := make([]TelephonyFeatureEvidence, 0, len(features))
	for _, feature := range slices.Sorted(maps.Keys(features)) {
		row := targetcap.ResolveTelephonyFeature(key, feature)
		evidence = append(evidence, TelephonyFeatureEvidence{
			Feature: string(row.Feature), Tag: string(row.Tag), Note: row.Note,
			Docs: row.Docs, Verified: row.Verified, Smoke: row.Smoke,
		})
	}
	connection := agent.Connections[resolved.Connection]
	hasAnyFeature := func(required []targetcap.TelephonyFeature) bool {
		if len(required) == 0 {
			return true
		}
		for _, feature := range required {
			if features[feature] {
				return true
			}
		}
		return false
	}
	processes := make([]TelephonyProcess, 0, len(route.Processes))
	for _, process := range route.Processes {
		processes = append(processes, TelephonyProcess{
			Name: process.Name, Command: slices.Clone(process.Command), Health: process.Health, Readiness: process.Readiness,
		})
	}
	endpoints := make([]TelephonyEndpoint, 0, len(route.PublicEndpoints))
	for _, endpoint := range route.PublicEndpoints {
		if hasAnyFeature(endpoint.AnyFeatures) {
			endpoints = append(endpoints, TelephonyEndpoint{Name: endpoint.Name, Method: endpoint.Method, Path: endpoint.Path})
		}
	}
	requiredEnvironment := make([]string, 0, len(connection.Environment)+len(route.RuntimeEnvironment))
	for _, name := range connection.Environment {
		if name != "" {
			requiredEnvironment = append(requiredEnvironment, name)
		}
	}
	for _, requirement := range route.RuntimeEnvironment {
		if hasAnyFeature(requirement.AnyFeatures) {
			requiredEnvironment = append(requiredEnvironment, requirement.Name)
		}
	}
	slices.Sort(requiredEnvironment)
	requiredEnvironment = slices.Compact(requiredEnvironment)
	// Dev-supplied names only count when the requested features actually
	// require them (e.g. no outbound trunk ID for an inbound-only channel).
	devEnvironment := make([]string, 0, len(route.DevSuppliedEnvironment))
	for _, name := range route.DevSuppliedEnvironment {
		if slices.Contains(requiredEnvironment, name) {
			devEnvironment = append(devEnvironment, name)
		}
	}
	// The auto-webhook fact only survives when the named endpoint is emitted
	// (an outbound-only channel has no inbound endpoint to point Twilio at).
	autoWebhook := route.AutoWebhookEndpoint
	if !slices.ContainsFunc(endpoints, func(e TelephonyEndpoint) bool { return e.Name == autoWebhook }) {
		autoWebhook = ""
	}
	coordination, admissionOwner := "shared", "generated_runtime"
	services := []string{"application", "redis"}
	reasons := []TelephonyCoordinationReason{
		{Name: "admission", Consumers: []string{"application"}},
	}
	if resolved.Provider == ProviderPipecat {
		reasons = append(reasons,
			TelephonyCoordinationReason{Name: "call_correlation", Consumers: []string{"application"}},
			TelephonyCoordinationReason{Name: "callback_idempotency", Consumers: []string{"application"}},
		)
	}
	if resolved.Provider == ProviderLiveKit && resolved.Transport == "sip" {
		admissionOwner = "livekit_dispatch"
		services = append(services, "livekit_server", "livekit_sip")
		reasons = []TelephonyCoordinationReason{
			{Name: "livekit_control_plane", Consumers: []string{"livekit_server", "livekit_sip"}},
		}
	}
	if resolved.Provider == ProviderLiveKit && resolved.Transport == "connector" {
		// The bridge and worker share a room on a local LiveKit Server. No SIP
		// bridge and no Redis: the bridge keeps its own in-process call state.
		admissionOwner = "livekit_dispatch"
		services = []string{"application", "livekit_server"}
		reasons = []TelephonyCoordinationReason{
			{Name: "livekit_control_plane", Consumers: []string{"livekit_server"}},
		}
	}
	slices.Sort(services)
	slices.SortFunc(reasons, func(a, b TelephonyCoordinationReason) int { return strings.Compare(a.Name, b.Name) })
	return &TelephonyPlan{
		Channels: channels, Connection: resolved.Connection,
		Key:         TelephonyKey{Provider: resolved.Provider, Transport: resolved.Transport, Carrier: resolved.Carrier},
		Environment: maps.Clone(connection.Environment), Destinations: maps.Clone(resolved.Destinations),
		SystemSources: sources, Evidence: evidence,
		Processes: processes, PublicEndpoints: endpoints, RequiredEnvironment: requiredEnvironment,
		LocalEnvironment: slices.Clone(route.LocallySuppliedEnvironment), DevEnvironment: devEnvironment,
		AutoWebhookEndpoint: autoWebhook, ManualSteps: slices.Clone(route.ManualSteps),
		Services: services, Coordination: coordination,
		CoordinationReasons: reasons, AdmissionOwner: admissionOwner,
	}
}

// validDestination accepts the three forms a transfer destination can take
// (SCHEMA N26): an E.164 literal, a SIP URI, or the UPPER_SNAKE name of an
// environment variable holding one of those. The three are unambiguous by
// shape, so no extra key or suffix is needed to tell them apart: a literal
// starts with `+`, a URI with `sip:`/`sips:`, and neither can be UPPER_SNAKE.
func validDestination(value string) bool {
	return e164Pattern.MatchString(value) || sipDestinationPath.MatchString(value) ||
		envNamePattern.MatchString(value)
}

// DestinationEnv reports the environment variable name a destination defers to,
// or "" when it carries a literal. Drivers use it to decide between emitting the
// value and emitting a lookup, and to register the name in `.env.example`.
func DestinationEnv(value string) string {
	if envNamePattern.MatchString(value) {
		return value
	}
	return ""
}

// resolveBindings converts each used effective model into a Binding: think
// models land in Reason, speak models in Speak, the listen/turn selections in
// their role slots.
func resolveBindings(agent *Agent, used map[string]bool, overrides map[string]packagespec.ModelDef) Bindings {
	effective := func(name string) ModelDef {
		def := agent.Models[name]
		if override, ok := overrides[name]; ok {
			def = convertModelDef(override, def.Kind, def.Fallback)
		}
		return def
	}
	bindings := Bindings{Speak: make(map[string]Binding), Reason: make(map[string]Binding)}
	if agent.Listen != "" {
		binding := toBinding(effective(agent.Listen))
		bindings.Listen = &binding
		for _, name := range agent.Models[agent.Listen].Fallback {
			bindings.ListenFallbacks = append(bindings.ListenFallbacks, ListenFallback{Name: name, Binding: toBinding(effective(name))})
		}
	}
	if agent.Turn != "" {
		binding := toBinding(effective(agent.Turn))
		bindings.Turn = &binding
	}
	for name := range used {
		def := effective(name)
		switch def.Kind {
		case KindSpeak:
			bindings.Speak[name] = toBinding(def)
		case KindThink:
			bindings.Reason[name] = toBinding(def)
		}
	}
	return bindings
}

// toBinding flattens a model into the resolved binding, folding the typed
// generation fields into the forwarded params (they lower through the same
// per-vendor param path the old params map used).
func toBinding(def ModelDef) Binding {
	return Binding{
		Provider: def.Provider, Model: def.Model, Voice: def.Voice, Language: def.Language,
		EndpointEnv: def.EndpointEnv, Placement: def.Placement,
		SemanticEndpointing: def.SemanticEndpointing, Params: foldParams(def),
	}
}

func foldParams(def ModelDef) map[string]any {
	if def.Temperature == nil && def.TopP == nil && def.TopK == nil && def.Speed == nil && len(def.Params) == 0 {
		return nil
	}
	out := make(map[string]any, len(def.Params)+5)
	maps.Copy(out, def.Params)
	setIfAbsent := func(key string, value any) {
		if _, ok := out[key]; !ok {
			out[key] = value
		}
	}
	if def.Temperature != nil {
		setIfAbsent("temperature", *def.Temperature)
	}
	if def.TopP != nil {
		setIfAbsent("top_p", *def.TopP)
	}
	if def.TopK != nil {
		setIfAbsent("top_k", *def.TopK)
	}
	if def.Speed != nil {
		setIfAbsent("speed", *def.Speed)
	}
	return out
}

func buildConversation(raw *packagespec.Conversation) *Conversation {
	if raw == nil {
		return nil
	}
	conversation := &Conversation{MaxDuration: Duration(raw.MaxDuration), ThinkingAudio: ThinkingAudio(raw.ThinkingAudio)}
	if raw.Greeting != nil {
		conversation.Greeting = &Greeting{SpeaksFirst: SpeaksFirst(raw.Greeting.SpeaksFirst), Text: raw.Greeting.Text}
	}
	if raw.Interruption != nil {
		conversation.Interruption = &Interruption{
			Enabled: raw.Interruption.Enabled, MinimumWords: raw.Interruption.MinimumWords,
			IgnorePhrases: raw.Interruption.IgnorePhrases,
		}
	}
	if raw.Inactivity != nil {
		conversation.Inactivity = &Inactivity{
			NudgeAfter: Duration(raw.Inactivity.NudgeAfter), EndAfter: Duration(raw.Inactivity.EndAfter),
		}
	}
	return conversation
}

func checkToolRefs(pkg *packagespec.Package, names []string) error {
	for _, name := range names {
		if _, tool := pkg.Tools[name]; tool {
			continue
		}
		if _, control := pkg.Agent.Controls[name]; control {
			continue
		}
		return missing(pkg, "agent.yaml", "tool or control", name)
	}
	return nil
}

func missing(pkg *packagespec.Package, file, kind, name string) error {
	return fmt.Errorf("%s: %s %q does not resolve", pkg.Location(file, name), kind, name)
}

func sortedKeys[V any](values map[string]V) []string {
	return slices.Sorted(maps.Keys(values))
}
