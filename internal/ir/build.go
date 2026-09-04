package ir

import (
	"cmp"
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	packagespec "github.com/slng-ai/unmute/internal/spec"
	targetcap "github.com/slng-ai/unmute/internal/target"
)

var (
	namePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
	// The E.164 and sip: URI patterns that stood here went with the literal
	// destination forms they matched (spec FR-004d): a destination now names an
	// environment variable and nothing else, which envNamePattern covers.
)

// Build resolves a decoded package into the target-independent IR.
func Build(pkg *packagespec.Package) (*Agent, error) {
	if pkg == nil {
		return nil, fmt.Errorf("build: nil package")
	}
	if err := checkPackageName(pkg); err != nil {
		return nil, err
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
	if pkg.Agent.Tracing != nil && !validTracingProvider(pkg.Agent.Tracing.Provider) {
		return nil, fmt.Errorf("%s: unsupported tracing provider %q; supported providers: %s", pkg.Location("agent.yaml", "tracing:"), pkg.Agent.Tracing.Provider, strings.Join(TracingProviders, ", "))
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
	shapes, err := buildShapes(pkg)
	if err != nil {
		return nil, err
	}
	declared := shapeNames(shapes)
	out := &Agent{
		Version:      pkg.Agent.Version,
		Name:         strings.TrimSpace(pkg.Agent.Name),
		EntryAgent:   pkg.Agent.EntryAgent,
		Models:       models,
		Listen:       listenName,
		Turn:         turnName,
		Shapes:       shapes,
		Variables:    make(map[string]Variable, len(pkg.Agent.Variables)),
		Secrets:      slices.Sorted(slices.Values(pkg.Agent.Secrets)),
		Agents:       make(map[string]AgentDef, len(pkg.Agent.Agents)),
		Tasks:        make(map[string]Task, len(pkg.Tasks)),
		TaskGroups:   make(map[string]TaskGroup, len(pkg.Agent.TaskGroups)),
		Controls:     make(map[string]Control, len(pkg.Callables)+len(pkg.Agent.Handoffs)+len(pkg.Agent.Escalations)),
		Tools:        make(map[string]Tool, len(pkg.Tools)),
		Conversation: buildConversation(pkg.Agent.Conversation),
		Channels:     make(map[string]Channel, len(pkg.Agent.Channels)),
		Connections:  make(map[string]Connection, len(pkg.Connections)),
		Targets:      make(map[string]Target, len(pkg.Targets)),
	}
	if pkg.Agent.Tracing != nil {
		out.Tracing = &Tracing{Provider: pkg.Agent.Tracing.Provider}
	}
	out.Knowledge = buildKnowledge(pkg)
	out.Documents = pkg.Documents
	out.VariableOrder = variableOrder(pkg)
	for _, name := range out.VariableOrder {
		variable := pkg.Agent.Variables[name]
		resolved := Variable{
			Type: PrimitiveType(variable.Type), Default: variable.Default,
			Source: VariableSource(variable.Source), Confirm: variable.Confirm,
			Description: variable.Description,
		}
		// An empty type: is left exactly as it was, because Validate already
		// refuses it by name and its message is the one an author has read
		// before. Everything else resolves, and a bare primitive keeps Shape nil
		// so a package declaring nothing structured resolves byte-identically.
		if strings.TrimSpace(variable.Type) != "" {
			ref, err := resolveType(variable.Type, declared)
			if err != nil {
				return nil, fmt.Errorf("%s: variable %q: %w",
					locateType(pkg, variable.Type, name+":"), name, err)
			}
			if ref.Structured() {
				// A structured value reaches a prompt as text, which is the one
				// thing every existing reader of Type does with it. The truth is
				// in Shape.
				resolved.Shape, resolved.Type = ref, PrimitiveString
			} else {
				resolved.Type = ref.Primitive
			}
		}
		out.Variables[name] = resolved
	}
	for name, tool := range pkg.Tools {
		built := buildTool(name, tool)
		built.HandlerSource = pkg.Handlers[built.Handler]
		// A hosted tool's definition is the mirror, and it is folded in here
		// rather than in buildTool because buildTool sees one tool file and the
		// mirror was read from the package. An absent mirror stays absent:
		// Validate owns that refusal, and it names the pull.
		if built.Execution == ToolSlngHosted {
			if mirror, ok := pkg.Mirrors[name]; ok {
				built.Mirror = &mirror
				built.MirrorBytes = pkg.MirrorBytes[name]
				built.Input = mirror.ArgSchema
				built.Dependencies = mirror.Dependencies
				if built.Description == "" {
					built.Description = mirror.Description
				}
			}
		}
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
		if raw.Kind != "" {
			return nil, fmt.Errorf("%s: kind is no longer written in a connection. Every transport in the catalog "+
				"is telephony, so transport: %s already says it", pkg.Location(path, "kind:"), orPlaceholder(raw.Transport))
		}
		if raw.Transport == "" {
			return nil, fmt.Errorf("%s: connection %q declares no transport. A connection is a phone route, and the "+
				"transport is the mechanism that carries the call", pkg.Location(path, "environment:"), name)
		}
		// An empty environment is legal for receive-only
		// (pipecat, cloud-websocket), where the platform terminates the carrier's
		// stream itself (spec FR-009a).
		for _, key := range sortedKeys(raw.Environment) {
			value := raw.Environment[key]
			if !namePattern.MatchString(key) {
				return nil, fmt.Errorf("%s: connection %q environment key %q must be lowercase snake_case", pkg.Location(path, key), name, key)
			}
			if !envNamePattern.MatchString(value) {
				// The offending text is never repeated back: what lands here when
				// it is wrong is usually a pasted credential, and a refusal that
				// quotes it puts the value in a terminal, a CI log, and a bug
				// report. The file, the line, and the key name locate it without
				// printing it (Wave B, 2026-08-15).
				return nil, fmt.Errorf("%s: connection %q environment %s is not a valid environment variable name: "+
					"use upper case letters, digits, and underscores, and do not start with a digit. This field takes a "+
					"name and never a value. A deployment platform exports secrets through a shell, so a bad name would "+
					"be missing at runtime with no error of its own",
					pkg.Location(path, value), name, key)
			}
		}
		// Kind is a resolved-surface field with no author to read it from: every
		// connection is telephony, so it is set here rather than deleted, which
		// keeps the resolved schema and its goldens still (data-model §2).
		out.Connections[name] = Connection{Kind: "telephony", Environment: maps.Clone(raw.Environment)}
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
		// think/speak references and their kinds are resolved in resolveModelKinds.
		for _, list := range []struct {
			key   string
			names []string
		}{
			{"tools", raw.Tools}, {"tasks", taskRefs(raw)}, {"task_groups", raw.TaskGroups},
			{"handoffs", raw.Handoffs}, {"escalations", raw.Escalations},
		} {
			if err := checkAttachments(pkg, list.key, list.names); err != nil {
				return nil, err
			}
		}
		instructions, ok := pkg.Markdown[raw.Instructions]
		if !ok {
			return nil, missing(pkg, "agent.yaml", "instructions", raw.Instructions)
		}
		instructions = appendPromptSuffix(instructions, thinkPromptSuffix(pkg, raw.Think))
		// The composed state block, after the authored suffix, because the suffix
		// is a directive about how to answer and this is the record it answers
		// from. Composed per site: an unconfirmed value belongs in one prompt
		// only, and no agent prompt is that prompt.
		instructions = appendPromptSuffix(instructions, out.StateBlock(AgentPromptSite(name)))
		out.Agents[name] = AgentDef{
			Instructions: instructions, Model: raw.Think, Voice: raw.Speak,
			Tools: attached(raw.Tools, callables(raw, pkg), raw.Handoffs, raw.Escalations),
		}
	}

	for _, name := range sortedKeys(pkg.Tasks) {
		raw := pkg.Tasks[name]
		if raw.Think != "" {
			if _, ok := out.Models[raw.Think]; !ok {
				return nil, missing(pkg, "agent.yaml", "think", raw.Think)
			}
		}
		if err := checkAttachments(pkg, "tools", raw.Tools); err != nil {
			return nil, err
		}
		if err := checkAttachments(pkg, "handoffs", raw.Handoffs); err != nil {
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
		// A task with no think profile of its own runs on the entry agent's, which
		// is the same rule slngProfileHasTools applies. One rule, read twice,
		// rather than two spellings that can drift. Deliberately the entry agent's
		// and not its defining agent's: a task is reachable from any agent that
		// names it, so the profile cannot come from where it happens to be written.
		instructions = appendPromptSuffix(instructions, thinkPromptSuffix(pkg,
			cmp.Or(raw.Think, pkg.Agent.Agents[pkg.Agent.EntryAgent].Think)))
		instructions = appendPromptSuffix(instructions, out.StateBlock(TaskPromptSite(name)))
		result, err := buildResult(raw.Result, declared)
		if err != nil {
			return nil, fmt.Errorf("%s: task %q: %w", pkg.Location("agent.yaml", name), name, err)
		}
		out.Tasks[name] = Task{
			Instructions: instructions, Tools: attached(raw.Tools, raw.Handoffs), Model: raw.Think, Result: result,
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
	// The callables and the two catalogs merge into one map keyed by name. Names
	// are one namespace across all five kinds (checkNames refuses a collision), so
	// the merge is order-free and the intermediate representation is unchanged.
	for _, name := range sortedKeys(pkg.Callables) {
		raw := pkg.Callables[name]
		control, err := buildCallable(pkg, raw, out)
		if err != nil {
			kind := "task"
			if raw.Group != "" {
				kind = "task group"
			}
			return nil, fmt.Errorf("%s: %s %q: %w", pkg.Location("agent.yaml", name), kind, name, err)
		}
		out.Controls[name] = control
	}
	for _, name := range sortedKeys(pkg.Agent.Handoffs) {
		control, err := buildHandoff(pkg, pkg.Agent.Handoffs[name], out)
		if err != nil {
			return nil, fmt.Errorf("%s: handoff %q: %w", pkg.Location("agent.yaml", name), name, err)
		}
		out.Controls[name] = control
	}
	for _, name := range sortedKeys(pkg.Agent.Escalations) {
		control, err := buildEscalation(pkg.Agent.Escalations[name], out)
		if err != nil {
			return nil, fmt.Errorf("%s: escalation %q: %w", pkg.Location("agent.yaml", name), name, err)
		}
		out.Controls[name] = control
	}
	if err := checkInject(pkg); err != nil {
		return nil, err
	}
	// After the controls, because a confirm: names a task a delegate runs, and
	// before checkTemplates, because a prefetch-assigned variable is one that has
	// a value when a session-start prompt is built and the template check has to
	// know that.
	if err := buildPrefetch(pkg, out); err != nil {
		return nil, err
	}
	if err := checkTemplates(pkg, out); err != nil {
		return nil, err
	}
	// Last, so a name that does not resolve is reported as unresolved rather than
	// as unreachable.
	if err := checkReachability(pkg); err != nil {
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
		{"task", sortedKeys(pkg.Tasks)}, {"task group", sortedKeys(pkg.Agent.TaskGroups)},
		{"handoff", sortedKeys(pkg.Agent.Handoffs)},
		{"escalation", sortedKeys(pkg.Agent.Escalations)}, {"tool", sortedKeys(pkg.Tools)},
	}
	for _, set := range sets {
		for _, name := range set.names {
			if !namePattern.MatchString(name) {
				return fmt.Errorf("%s: %s name %q must be lowercase snake_case and cannot start with underscore", pkg.Location("agent.yaml", name), set.kind, name)
			}
		}
	}
	// All five kinds become callable function names at runtime, so they share one
	// flat namespace: a name may sit in exactly one catalog, and never on a tool
	// as well. The capture tool is generated whenever a conversation variable
	// exists, so its name is reserved across all of them (V7).
	//
	// Task groups are the easy half to miss. They were never checked while a
	// delegate pointed at them, because the delegate carried the name into the
	// emitted project. An agent names a group directly now, so the group name IS
	// the emitted method name, and a group colliding with a tool would define one
	// Python function twice and silently drop the tool.
	if _, ok := pkg.Tools[CaptureToolName]; ok {
		return fmt.Errorf("%s: tool name %q is reserved: unmute generates %s for source: conversation variables", pkg.Location("agent.yaml", CaptureToolName), CaptureToolName, CaptureToolName)
	}
	declared := map[string]string{}
	for _, catalog := range []struct {
		kind  string
		names []string
	}{
		{"task", sortedKeys(pkg.Tasks)}, {"task group", sortedKeys(pkg.Agent.TaskGroups)},
		{"handoff", sortedKeys(pkg.Agent.Handoffs)},
		{"escalation", sortedKeys(pkg.Agent.Escalations)},
	} {
		for _, name := range catalog.names {
			if prior, ok := declared[name]; ok {
				return fmt.Errorf("%s: %s and %s name %q collide: all five kinds share one namespace, so a name belongs to exactly one of them", pkg.Location("agent.yaml", name), prior, catalog.kind, name)
			}
			declared[name] = catalog.kind
			if _, ok := pkg.Tools[name]; ok {
				return fmt.Errorf("%s: tool and %s name %q collide", pkg.Location("agent.yaml", name), catalog.kind, name)
			}
			if name == CaptureToolName {
				return fmt.Errorf("%s: %s name %q is reserved: unmute generates %s for source: conversation variables", pkg.Location("agent.yaml", name), catalog.kind, name, CaptureToolName)
			}
		}
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
		if err := check(agent.Think, KindThink, "think"); err != nil {
			return err
		}
		if err := check(agent.Speak, KindSpeak, "speak"); err != nil {
			return err
		}
	}
	for _, taskName := range sortedKeys(pkg.Tasks) {
		task := pkg.Tasks[taskName]
		if err := check(task.Think, KindThink, "think"); err != nil {
			return err
		}
		if err := check(task.Context.Summarizer, KindThink, "summarizer"); err != nil {
			return err
		}
	}
	// A handoff is the only kind carrying a `context:`, so it is the only one with
	// a summarizer to resolve.
	for _, name := range sortedKeys(pkg.Agent.Handoffs) {
		handoff := pkg.Agent.Handoffs[name]
		if handoff.Context != nil {
			if err := check(handoff.Context.Summarizer, KindThink, "summarizer"); err != nil {
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
		add(agent.Think)
		add(agent.Speak)
	}
	for _, task := range pkg.Tasks {
		add(task.Think)
		add(task.Context.Summarizer)
	}
	for _, handoff := range pkg.Agent.Handoffs {
		if handoff.Context != nil {
			add(handoff.Context.Summarizer)
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
		Pace:             Pace(raw.Pace),
		EndpointingDelay: Duration(raw.EndpointingDelay),
		AgentID:          raw.AgentID, Upstream: convertUpstream(raw.Upstream),
		PromptSuffix: raw.PromptSuffix,
		Params:       raw.Params, Fallback: fallback, Description: raw.Description,
	}
}

// thinkPromptSuffix is the directive one prompt site appends: the authored
// suffix on the think binding that site uses, trimmed, or empty for a site whose
// binding sets none.
//
// Read off the unresolved package rather than a resolved per-target binding,
// because an instructions file is package-level and so is this. One markdown file
// cannot become two different prompts for two targets, so a per-target override
// that names a different suffix is refused in Validate rather than silently
// resolved to one of them.
func thinkPromptSuffix(pkg *packagespec.Package, profile string) string {
	return strings.TrimSpace(pkg.Agent.Models.Think[profile].PromptSuffix)
}

// appendPromptSuffix puts the directive after the authored prompt, separated by a
// blank line so it reads as its own instruction rather than as the tail of the
// author's last sentence.
//
// An empty suffix returns the prompt byte for byte, which is what keeps every
// package that does not use the field emitting exactly what it emitted before.
func appendPromptSuffix(instructions, suffix string) string {
	if suffix == "" {
		return instructions
	}
	return strings.TrimRight(instructions, "\n") + "\n\n" + suffix
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
		tool.BaseURL = raw.Webhook.BaseURL
		tool.Path = raw.Webhook.Path
		tool.Auth = buildToolAuth(raw.Webhook.Auth)
	case raw.Local != nil:
		tool.Handler = raw.Local.Handler
		tool.Dependencies = raw.Local.Dependencies
		if tool.Handler == "" {
			tool.Handler = filepath.Join("tools", name+".py")
		}
	case raw.MCP != nil:
		tool.URLEnv = raw.MCP.URLEnv
		tool.MCPTransport = raw.MCP.Transport
		tool.MCPTools = raw.MCP.Tools
		tool.MCPServer = raw.MCP.Server
		tool.Auth = buildToolAuth(raw.MCP.Auth)
	case raw.Builtin != nil:
		tool.Builtin = raw.Builtin.ID
		tool.Instructions = raw.Builtin.Instructions
	case raw.Knowledge != nil:
		tool.KnowledgeBase = raw.Knowledge.Base
	case raw.Slng != nil:
		// The pin is all the file carries. Everything else about a hosted tool
		// comes from the mirror, which Build folds in at the call site because
		// it belongs to the package rather than to this file.
		tool.MirrorPin = raw.Slng.Hash
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
	// ponytail: one TrimSpace is the whole default resolution. A blank or
	// whitespace-only line reads as no announcement, so every driver sees a
	// settled value and none has to decide what " " means.
	tool.Announce = strings.TrimSpace(raw.Announce)
	return tool
}

// buildKnowledge resolves the knowledge: section: the embedding-service default
// lands here so every generator and the report read a settled value, and the file
// list comes from what spec.Load actually read rather than from a second listing
// of the folder.
//
// A base whose folder is missing arrives here with no files, and Validate refuses
// it by name (FR-009). Build does not second-guess that: its job is to resolve,
// not to judge.
func buildKnowledge(pkg *packagespec.Package) map[string]KnowledgeBase {
	if len(pkg.Agent.Knowledge) == 0 {
		return nil
	}
	bases := make(map[string]KnowledgeBase, len(pkg.Agent.Knowledge))
	for name, raw := range pkg.Agent.Knowledge {
		embed := raw.Embed
		if embed == "" {
			embed = targetcap.DefaultEmbeddingService
		}
		prefix := path.Join("knowledge", name) + "/"
		var files []string
		for key := range pkg.Documents {
			if rest, ok := strings.CutPrefix(key, prefix); ok {
				files = append(files, rest)
			}
		}
		slices.Sort(files)
		bases[name] = KnowledgeBase{
			Name: name, Documents: raw.Documents, Embed: embed, Files: files,
			// Pointers, so `chunk_overlap: 0` survives as a choice rather than
			// reading as unset.
			ChunkSize:    intOr(raw.ChunkSize, DefaultChunkSize),
			ChunkOverlap: intOr(raw.ChunkOverlap, DefaultChunkOverlap),
			TopK:         intOr(raw.TopK, DefaultTopK),
			Mode:         KnowledgeMode(cmp.Or(raw.Mode, string(DefaultKnowledgeMode))),
			MinScore:     floatOr(raw.MinScore, 0),
		}
	}
	return bases
}

// intOr resolves an optional authored int to a settled value, so every generator
// and the compile report read a number rather than deciding what absent means.
func intOr(authored *int, fallback int) int {
	if authored == nil {
		return fallback
	}
	return *authored
}

// floatOr is intOr for a float. Absent and zero mean the same thing for the one
// field that uses it, so there is no pointer to preserve past this point.
func floatOr(authored *float64, fallback float64) float64 {
	if authored == nil {
		return fallback
	}
	return *authored
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

func buildResult(raw map[string]any, declared map[string]bool) (map[string]ResultField, error) {
	result := make(map[string]ResultField, len(raw))
	for name, value := range raw {
		switch value := value.(type) {
		case string:
			// A result field's type is a type expression, the same grammar a
			// variable's is, so a step can hand back a whole shape and the
			// assignment can match it against the variable it writes to. The
			// four JSON Schema words every package written before this uses
			// resolve to the same primitives they always did.
			ref, err := resolveType(value, declared)
			if err != nil {
				return nil, fmt.Errorf("result %q: %w", name, err)
			}
			if ref.Structured() {
				result[name] = ResultField{Type: PrimitiveString, Shape: ref}
			} else {
				result[name] = ResultField{Type: ref.Primitive}
			}
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

// checkRequires resolves a guard's names against the declared variables. Both
// kinds that accept `requires:` share this, so the two cannot drift, and a name
// that does not resolve is a typo the author must see at compile rather than a
// guard that can never pass at runtime.
//
// Resolving the name is the whole check, and a stronger one was tried and
// deleted: "refuse a name no route can ever fill". There is no such name. A
// variable with no `source:` at all is seeded from the dispatch payload on both
// code targets (livekit_v1_build.go and pipecat_v1_build.go both read
// `v.Source == ir.VariableSourceCallStart || v.Source == ""`), optional rather
// than required, so every declared variable has at least one route to a value
// and the refusal could never fire. TestBuildAcceptsEveryRequiresARouteCanFill
// holds that, so the next person to reach for the check finds the answer before
// writing it.
func checkRequires(pkg *packagespec.Package, requires []string, agent *Agent) error {
	for _, name := range requires {
		root, path, _ := strings.Cut(name, ".")
		variable, ok := agent.Variables[root]
		if !ok {
			// Name what the field takes, because the common mistake is writing a
			// tool or another catalog entry here and expecting an ordering rule.
			return fmt.Errorf("%s: requires names variables, and %q is not declared under the variables: block",
				pkg.Location("agent.yaml", root), root)
		}
		if path == "" {
			continue
		}
		// A path resolves against the declared shape here, so a typo is refused
		// at compile rather than becoming a guard that can never pass.
		if _, err := FieldPath(agent.Shapes, variable.Shape, strings.Split(path, ".")); err != nil {
			return fmt.Errorf("%s: requires %q does not resolve: %w",
				pkg.Location("agent.yaml", name), name, err)
		}
	}
	return nil
}

// buildCallable resolves one thing an agent can decide to run.
//
// Two refusals its predecessor carried are gone rather than moved. "Exactly one
// of task or group" is unwritable now that a callable is derived from either a
// nested task or a `task_groups:` entry, and "assign is legal on task delegates
// only" is unwritable because `assign:` is a key on a task and a group has none.
func buildCallable(pkg *packagespec.Package, raw packagespec.Callable, agent *Agent) (Control, error) {
	if err := checkRequires(pkg, raw.Requires, agent); err != nil {
		return nil, err
	}
	if raw.Task != "" {
		if _, ok := agent.Tasks[raw.Task]; !ok {
			return nil, missing(pkg, "agent.yaml", "task", raw.Task)
		}
	} else if _, ok := agent.TaskGroups[raw.Group]; !ok {
		return nil, missing(pkg, "agent.yaml", "group", raw.Group)
	}
	assign, err := assignments(raw.Assign)
	if err != nil {
		return nil, err
	}
	if err := checkAssignments(raw.Task, assign, agent); err != nil {
		return nil, err
	}
	// ponytail: one TrimSpace, matching buildTool. A blank line reads as no
	// announcement, so no driver has to decide what " " means.
	return &Delegate{
		Kind: ControlDelegate, When: raw.When, Task: raw.Task, Group: raw.Group,
		Requires: raw.Requires, Assign: assign,
		Announce: strings.TrimSpace(raw.Announce),
	}, nil
}

// assignments turns the authored pair list into the name-keyed map the resolved
// representation holds. A pair value is one scalar by the time it decodes, and
// an assignment is always a `result.<field>` string, so anything else is refused
// here rather than reaching checkAssignments as something no path can be.
func assignments(pairs []packagespec.Pair) ([]AssignTo, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make([]AssignTo, 0, len(pairs))
	for _, pair := range pairs {
		text, ok := pair.Value.(string)
		if !ok {
			return nil, fmt.Errorf("assign %q must use result.<field>, and %v is not a name", pair.Key, pair.Value)
		}
		// A `+` on the key is an append. Stripped here, where the authored
		// spelling stops and the resolved shape starts, so no driver has to
		// know that the mark was ever part of a name.
		name, appends := strings.CutSuffix(pair.Key, "+")
		field, ok := strings.CutPrefix(text, "result.")
		if !ok {
			return nil, fmt.Errorf("assign %q must use result.<field>", text)
		}
		out = append(out, AssignTo{Var: name, Field: field, Append: appends})
	}
	return out, nil
}

func buildHandoff(pkg *packagespec.Package, raw packagespec.Handoff, agent *Agent) (Control, error) {
	if err := checkRequires(pkg, raw.Requires, agent); err != nil {
		return nil, err
	}
	if _, ok := agent.Agents[raw.To]; !ok {
		return nil, missing(pkg, "agent.yaml", "to", raw.To)
	}
	announce := stringValue(raw.Announce)
	if raw.Announce != nil && strings.TrimSpace(announce) == "" {
		return nil, fmt.Errorf("announce must not be blank")
	}
	if HasTemplate(announce) {
		return nil, fmt.Errorf("announce does not support templates")
	}
	context, err := buildTransferContext(pkg, raw.Context, agent)
	if err != nil {
		return nil, err
	}
	return &AgentTransfer{Kind: ControlAgentTransfer, When: raw.When, To: raw.To, Announce: announce, Requires: raw.Requires, Context: context}, nil
}

func buildEscalation(raw packagespec.Escalation, agent *Agent) (Control, error) {
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
}

// buildHumanTransfer resolves the `cold:`/`warm:` block into the IR control
// (SCHEMA N25). The shape is the block name, so zero blocks and two blocks are
// both errors here; `on_unavailable` resolves to its default so no driver reads
// an empty value.
func buildHumanTransfer(raw packagespec.Escalation) (Control, error) {
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

// unexpectedControlField is gone, and nothing replaced it. It was a nine-field
// by three-kind allow-matrix that existed only because one superset struct held
// every kind's fields. Three precise structs make every cell of it unwritable,
// so strict decoding refuses the same inputs, and it names file, line and column
// where the matrix named only the field.

func checkAssignments(taskName string, assign []AssignTo, agent *Agent) error {
	if taskName == "" {
		return nil
	}
	task := agent.Tasks[taskName]
	for _, entry := range assign {
		want, ok := agent.Variables[entry.Var]
		if !ok {
			// Same sentence as checkRequires, for the same mistake made in the
			// other key: name the block the author has to edit, because "does
			// not resolve" says neither where to look nor what is wrong.
			return fmt.Errorf("assign writes to %q, and it is not declared under the variables: block", entry.Var)
		}
		field, ok := task.Result[entry.Field]
		if !ok {
			return fmt.Errorf("assign result field %q does not resolve", entry.Field)
		}
		target := want.Shape
		source := field
		if entry.Append {
			if !target.IsList() {
				return fmt.Errorf("assign appends to %q with %q, and %q is declared %s rather than a list. "+
					"Drop the %q to replace the value, or declare it list[...] so an entry can be added to it",
					entry.Var, entry.Var+"+:", entry.Var, declaredAs(want), "+")
			}
			target = target.List
			// An entry that may be absent is legal to append: it means "add one
			// if there is one this time", which is the shape a step that
			// concludes nothing needs. The emitted append skips it, so the list
			// grows only when the step produced something. Without this the step
			// would have to invent an entry on the turn the caller changed their
			// mind.
			source.Shape = withoutOptional(source.Shape)
		}
		if err := assignableInto(target, want.Type, source); err != nil {
			return fmt.Errorf("assign result %q does not fit variable %q: %w", entry.Field, entry.Var, err)
		}
	}
	return nil
}

// declaredAs names a variable's type the way its author wrote it, for a refusal
// that has to say what the value is as well as what it is not.
func declaredAs(variable Variable) string {
	if variable.Shape != nil {
		return variable.Shape.String()
	}
	return pythonSpelling(variable.Type)
}

// assignableInto is the one predicate deciding whether a step's result field can
// be written into a declared value. It replaced a single condition,
// `field.Schema != nil || field.Type != want.Type`, whose first half is what
// made a structured result unassignable at all.
//
// The three refusals that condition's siblings carry are untouched: an unknown
// variable, an unresolved result field, and a path not written `result.<field>`.
func assignableInto(target *TypeRef, targetPrimitive PrimitiveType, field ResultField) error {
	// A raw JSON Schema result field stays unassignable, and deliberately: it is
	// provider passthrough with no declared shape to match, so nothing can say
	// whether it fits. Declaring a shape is what makes it assignable.
	if field.Schema != nil {
		return fmt.Errorf("the result field is a raw JSON Schema object, which has no declared shape to match. "+
			"Declare the fields under %q and name that shape as the result field's type", "shapes:")
	}
	if target == nil {
		// Both sides plain: the check that existed before this feature, byte for
		// byte, so every package that compiles today keeps compiling.
		if field.Shape != nil {
			return fmt.Errorf("the result field is %s and the variable is %s. Declare the variable with the same "+
				"type as the result field", field.Shape.String(), pythonSpelling(targetPrimitive))
		}
		if field.Type != targetPrimitive {
			return fmt.Errorf("the result field is %s and the variable is %s",
				pythonSpelling(field.Type), pythonSpelling(targetPrimitive))
		}
		return nil
	}
	source := field.Shape
	if source == nil {
		// A text type with a validated shape accepts plain text, because the
		// shape is checked where the value enters the state and not in the
		// schema the model is sent (FR-013). Everything else has to be declared.
		if target.Shaped != "" && field.Type == PrimitiveString {
			return nil
		}
		// An `enum:` result field and a Literal are the same closed set written
		// two ways, so the same set assigns. A different set, or a bare string
		// into a closed set, does not: the model would never be told the set and
		// the value would be refused on every call.
		if len(target.Literal) > 0 && len(field.Enum) > 0 {
			if sameSet(target.Literal, field.Enum) {
				return nil
			}
			return fmt.Errorf("the result field allows %s and the variable allows %s. One set, written once",
				strings.Join(field.Enum, ", "), strings.Join(target.Literal, ", "))
		}
		return fmt.Errorf("the result field is %s and the variable is %s. Declare the result field with the "+
			"same type, so the model is told what to produce", pythonSpelling(field.Type), target.String())
	}
	// Optional on the target and not on the source is fine: a value that may be
	// absent accepts one that is present.
	if source.Equal(target) || (target.Optional && source.Equal(withoutOptional(target))) {
		return nil
	}
	return fmt.Errorf("the result field is %s and the variable is %s", source.String(), target.String())
}

// withoutOptional is the target type with its nullability dropped, for the one
// comparison that has to ignore it.
func withoutOptional(ref *TypeRef) *TypeRef {
	if ref == nil {
		return nil
	}
	bare := *ref
	bare.Optional = false
	return &bare
}

// sameSet reports whether two closed sets hold the same entries. Order is how a
// Literal renders and is not what makes two sets equal.
func sameSet(left, right []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(left)), slices.Sorted(slices.Values(right)))
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
	// Three fields moved out of a target. Each is refused by name, quoting the
	// line and saying where it now lives, because a bare "unknown field" leaves
	// the author to find that out themselves (Principle II). Keyed on the field,
	// not on the provider: `carrier` is refused on every target, vapi and
	// deepgram included (spec FR-007).
	if raw.Transport != "" {
		return Target{}, fmt.Errorf("%s: target %q declares transport: %s, which now belongs in %s. "+
			"A target names one connection and the connection declares the route",
			pkg.Location("targets.yaml", "transport:"), name, raw.Transport, connectionFileFor(raw.Connection))
	}
	if raw.Carrier != "" {
		return Target{}, fmt.Errorf("%s: target %q declares carrier: %s, which now belongs in %s alongside its transport",
			pkg.Location("targets.yaml", "carrier:"), name, raw.Carrier, connectionFileFor(raw.Connection))
	}
	if len(raw.Destinations) > 0 {
		return Target{}, fmt.Errorf("%s: target %q declares destinations, which now belong at the top level of "+
			"agent.yaml. A destination is who this agent escalates to, which is the same desk whichever carrier "+
			"reaches it", pkg.Location("targets.yaml", "destinations:"), name)
	}
	destinations := pkg.Agent.Destinations
	for _, destination := range sortedKeys(destinations) {
		value := destinations[destination]
		if !envNamePattern.MatchString(value) {
			// The value is not repeated back. A destination that is not a name is
			// a phone number, and the bundle's hardest rule is that no phone
			// number appears in any output (Wave B, 2026-08-15).
			return Target{}, fmt.Errorf("%s: destination %q is a literal. agent.yaml is the portable half of a "+
				"package, so a destination names an environment variable holding the number: %s: BILLING_PHONE_NUMBER",
				pkg.Location("agent.yaml", destination), destination, destination)
		}
	}
	if raw.Connection != "" {
		if _, ok := agent.Connections[raw.Connection]; !ok {
			// The defined set rides the message. Without it the author learns the
			// name is wrong and has to go looking for which names are right, and
			// the usual cause is a typo they cannot see by staring at it.
			defined := "This package defines no connections."
			if names := sortedKeys(agent.Connections); len(names) > 0 {
				defined = "It defines: " + strings.Join(names, ", ") + "."
			}
			return Target{}, fmt.Errorf("%s: target %q names connection %q, which this package does not define. %s",
				pkg.Location("targets.yaml", raw.Connection), name, raw.Connection, defined)
		}
	}
	// The connection owns the route: the target says which connection, and the
	// connection file says everything about how a call reaches it.
	var transport, carrier string
	if conn, ok := pkg.Connections[raw.Connection]; ok && raw.Connection != "" {
		transport, carrier = conn.Transport, conn.Carrier
		if err := validateRoute(pkg, raw.Connection, raw.Provider, transport, carrier); err != nil {
			return Target{}, err
		}
	}
	telephony := hasTelephonyChannel(agent)
	cloudWebsocket := raw.Provider == string(ProviderPipecat) && transport == "cloud-websocket"
	// Every telephony target names a connection now, because the connection is
	// where the route is written: without one there is no transport to reason
	// about at all. The carve-out that used to stand here — receive-only on
	// (pipecat, cloud-websocket) needs no carrier credentials — did not go away,
	// it moved into the connection file, which is legal with a route and no
	// `environment:` block (spec FR-009a).
	if telephony && (raw.Provider == string(ProviderLiveKit) || raw.Provider == string(ProviderPipecat)) && raw.Connection == "" {
		return Target{}, fmt.Errorf("%s: target %q has a telephony channel and names no connection. "+
			"Add connection: <name> and a connections/<name>.yaml declaring the route",
			pkg.Location("targets.yaml", name+":"), name)
	}
	// A connection is used by a telephony channel or by a control that dials.
	if !telephony && raw.Connection != "" && !packagePlacesCalls(pkg, agent) {
		return Target{}, fmt.Errorf("%s: target %q names connection %q, but nothing in this package uses a phone "+
			"route: declare a channels.phone entry, or a control that dials a person",
			pkg.Location("targets.yaml", "connection:"), name, raw.Connection)
	}
	built := Target{
		Name: name, Provider: Provider(raw.Provider), Version: raw.Version, Pins: raw.Pins,
		SDKLanguage: raw.SDKLanguage, Transport: transport, Carrier: carrier, Connection: raw.Connection,
		// Declared order, no deduplication, no region invented when none is
		// declared: validate rejects a duplicate and each README states what
		// the platform does with an empty list.
		DeploymentRegions: raw.DeploymentRegion,
		WarmInstances:     raw.WarmInstances,
		Models:            resolveBindings(agent, used, raw.Models),
		Destinations:      destinations,
	}
	// The plan is what tells the emitter to emit the Bin, the transport entry,
	// and the runbook. Without it a package would compile with telephony declared
	// and no telephony emitted, which is the silent downgrade Principle II
	// forbids.
	if telephony && (raw.Connection != "" || cloudWebsocket) {
		built.Telephony = buildTelephonyPlan(pkg, agent, built)
		if raw.Connection != "" {
			if err := validateTelephonyEnvironment(pkg, built.Telephony, packagePlacesCalls(pkg, agent)); err != nil {
				return Target{}, err
			}
		}
	}
	return built, nil
}

// packagePlacesCalls reports whether the package dials anybody: an outbound phone
// direction, or a human transfer, which dials its destination whatever the
// channel says. Both need the carrier credentials; receiving a call does not.
//
// The escalations are read from the raw package rather than the built agent
// because controls are built *after* targets (they resolve destinations against
// a target), so agent.Controls is still empty here. buildTelephonyPlan reads them
// the same way, for the same reason.
func packagePlacesCalls(pkg *packagespec.Package, agent *Agent) bool {
	for _, channel := range agent.Channels {
		if channel.Kind == ChannelTelephony && channel.Outbound != nil && *channel.Outbound {
			return true
		}
	}
	// Every escalation dials a person, so one is enough.
	return len(pkg.Agent.Escalations) > 0
}

// connectionFileFor names the file a moved route field belongs in. A target that
// names no connection has no file to point at yet, so it gets the instruction
// instead of a path.
func connectionFileFor(connection string) string {
	if connection == "" {
		return "the connection file this target should name"
	}
	return filepath.Join("connections", connection+".yaml")
}

func orPlaceholder(transport string) string {
	if transport == "" {
		return "<transport>"
	}
	return transport
}

// validateRoute checks that a connection declares a route the provider actually
// has, and refuses with the routes it does have.
func validateRoute(pkg *packagespec.Package, connection, provider, transport, carrier string) error {
	path := filepath.Join("connections", connection+".yaml")
	if carrier == "" {
		return fmt.Errorf("%s: transport %q declares no carrier. %s",
			pkg.Location(path, "transport:"), transport, supportedRoutes(provider))
	}
	key := targetcap.TelephonyKey{Provider: targetcap.Provider(provider), Transport: transport, Carrier: carrier}
	if _, ok := targetcap.SelectableTelephonyRoutes()[key]; ok {
		return nil
	}
	return fmt.Errorf("%s: transport %q with carrier %q is not a route for provider %s. %s",
		pkg.Location(path, "transport:"), transport, carrier, provider, supportedRoutes(provider))
}

// supportedRoutes lists what the provider does support, reading only selectable
// routes so a suggestion never leads to a second refusal (spec FR-011a).
func supportedRoutes(provider string) string {
	byTransport := make(map[string][]string)
	for key := range targetcap.SelectableTelephonyRoutes() {
		if string(key.Provider) != provider {
			continue
		}
		byTransport[key.Transport] = append(byTransport[key.Transport], key.Carrier)
	}
	if len(byTransport) == 0 {
		return fmt.Sprintf("Provider %s has no phone routes.", provider)
	}
	parts := make([]string, 0, len(byTransport))
	for _, transport := range sortedKeys(byTransport) {
		carriers := byTransport[transport]
		slices.Sort(carriers)
		parts = append(parts, fmt.Sprintf("%s with %s", transport, strings.Join(carriers, ", ")))
	}
	return fmt.Sprintf("%s supports: %s.", provider, strings.Join(parts, "; "))
}

func validateTelephonyEnvironment(pkg *packagespec.Package, plan *TelephonyPlan, placesCalls bool) error {
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
	// On one route the credentials are conditional, and it is the point of the
	// route: on (pipecat, cloud-websocket) the platform terminates the carrier's
	// stream itself, so receiving a call needs nothing from your account.
	// Placing one, or redirecting one to a person, still does (SCHEMA N38).
	conditional := plan.Key.Provider == ProviderPipecat && plan.Key.Transport == "cloud-websocket"
	if conditional && !placesCalls && len(plan.Environment) == 0 {
		return nil
	}
	for _, name := range required {
		if plan.Environment[name] == "" {
			because := ""
			if conditional {
				because = ", because this package places or redirects calls. A package that only receives calls " +
					"on this route needs no connection environment at all"
			}
			return fmt.Errorf("%s: connection %q requires environment key %q for route (%s, %s, %s)%s", pkg.Location(path, "environment:"), plan.Connection, name, plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier, because)
		}
	}
	accepted := append(slices.Clone(required), optional...)
	slices.Sort(accepted)
	for _, name := range sortedKeys(plan.Environment) {
		if !allowed[name] {
			// The accepted set rides the message. Without it the author learns which
			// key is wrong and has to go looking for which keys are right, and on the
			// cloud-websocket route the three SIP keys are the exact mistake somebody
			// moving from another route will make (spec FR-009).
			return fmt.Errorf("%s: connection %q environment key %q is not accepted by route (%s, %s, %s); it accepts %s",
				pkg.Location(path, name), plan.Connection, name, plan.Key.Provider, plan.Key.Transport, plan.Key.Carrier,
				strings.Join(accepted, ", "))
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
	for _, escalation := range pkg.Agent.Escalations {
		// The shape block is the feature: SCHEMA N25 removed the briefing mode
		// enum, so free-text briefing rides the warm row and resolves nothing.
		if shape := escalation.TransferShape(); shape != "" {
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
	if resolved.Provider == ProviderPipecat && resolved.Transport == "cloud-websocket" {
		// One service, and it is the agent: the platform hosts it. No Redis,
		// because nothing here outlives a call, and none of the Pipecat correlation
		// reasons, because each names a Redis-backed record this route never keeps.
		// The route row's empty Processes says the operator runs nothing; this says
		// what the thing they do not run is (data-model section 1).
		services = []string{"application"}
	} else if resolved.Provider == ProviderPipecat && resolved.Transport == "daily-sip" {
		// The Daily carrier route keeps no shared control record: the transfer guard
		// is in-process because one process serves one
		// call, and the room, not a store, correlates the legs. So no redis, and
		// none of the Pipecat reasons below, which each describe a Redis-backed
		// record this route does not keep. Same Redis-free shape the LiveKit
		// connector route already has.
		services = []string{"application"}
	}
	// A LiveKit SIP route's topology is a LiveKit Server and a SIP service beside
	// the agent, coordinating through a store. On LiveKit Cloud the platform runs
	// all three; a self-hosted deployment runs them itself, which is why REDIS_URL
	// is a name this route asks for.
	if resolved.Provider == ProviderLiveKit && resolved.Transport == "sip" {
		services = append(services, "livekit_server", "livekit_sip")
		reasons = []TelephonyCoordinationReason{
			{Name: "livekit_control_plane", Consumers: []string{"livekit_server", "livekit_sip"}},
		}
	}
	// Admission is still the provider's own business, and it does not follow from
	// the plane. A LiveKit worker is admitted by the dispatch that starts it; the
	// Pipecat bot on this same plane is admitted by the emitted application, which
	// is what decides one session per room.
	if resolved.Provider == ProviderLiveKit && resolved.Transport == "sip" {
		admissionOwner = "livekit_dispatch"
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
		// Scoped to what this package's route actually requires. The route
		// declares its locally-supplied names statically, but some of them are
		// feature-gated — UNMUTE_OUTBOUND_TOKEN only exists on a package that
		// dials out — and a plan naming a supplied value the runtime never asks
		// for is the disagreement validateTelephonyPlan refuses.
		LocalEnvironment:    intersect(route.LocallySuppliedEnvironment, requiredEnvironment),
		AutoWebhookEndpoint: autoWebhook, ManualSteps: slices.Clone(route.ManualSteps),
		Services: services, Coordination: coordination,
		CoordinationReasons: reasons, AdmissionOwner: admissionOwner,
	}
}

// A destination used to accept three forms (SCHEMA N26): an E.164 literal, a
// SIP URI, or an environment variable name. It now accepts only the last of the
// three, checked with envNamePattern where destinations are read. agent.yaml is
// the portable half of a package, and a phone number is a deployment fact
// (spec FR-004d).

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
			replaced := convertModelDef(override, def.Kind, def.Fallback)
			// A target override replaces the vendor selection, but the silence
			// budget is not part of it: every target that runs this package wants
			// the same wait for the same transcriber. Dropping it silently undoes
			// the one fix for a slow STT (B: fragmented STT, 2026-08-20).
			if replaced.EndpointingDelay == "" {
				replaced.EndpointingDelay = def.EndpointingDelay
			}
			// Same reasoning for the prompt directive, and a harder version of it:
			// Build has already appended the base suffix to the package's shared
			// instructions, so an override that dropped it would report a binding
			// with no directive while every prompt beside it still carried one.
			// Validate refuses an override that names a *different* suffix, which
			// is the case this carry-forward cannot rescue.
			if replaced.PromptSuffix == "" {
				replaced.PromptSuffix = def.PromptSuffix
			}
			// Neither is part of the vendor selection either, and
			// examples/salon-concierge overrides its turn binding on both
			// targets, so without these a base `semantic_endpointing` or `pace`
			// reaches neither of them.
			//
			// pace is carried even though it takes no per-target override: an
			// override that names one has to stay visible in the resolved binding
			// for Validate to refuse it rather than Build dropping it in silence.
			if replaced.SemanticEndpointing == "" {
				replaced.SemanticEndpointing = def.SemanticEndpointing
			}
			if replaced.Pace == "" {
				replaced.Pace = def.Pace
			}
			def = replaced
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
		SemanticEndpointing: def.SemanticEndpointing, Pace: def.Pace,
		EndpointingDelay: def.EndpointingDelay,
		AgentID:          def.AgentID, Upstream: def.Upstream, PromptSuffix: def.PromptSuffix,
		Params: foldParams(def),
	}
}

// convertUpstream carries the authored upstream block through unfolded. It is
// not merged into Params: params reach the SDK verbatim, and every field here is
// consumed by the compiler into the inline endpoint object instead.
func convertUpstream(raw *packagespec.Upstream) *Upstream {
	if raw == nil {
		return nil
	}
	return &Upstream{
		Provider: raw.Provider, URL: raw.URL, KeyEnv: raw.KeyEnv, AuthHeader: raw.AuthHeader,
		Deployment: raw.Deployment, APIVersion: raw.APIVersion,
		CredentialsEnv: raw.CredentialsEnv, Location: raw.Location, Project: raw.Project,
		AccessKeyIDEnv: raw.AccessKeyIDEnv, SecretAccessKeyEnv: raw.SecretAccessKeyEnv,
		SessionTokenEnv: raw.SessionTokenEnv, Region: raw.Region, ModelID: raw.ModelID,
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
		// nil stays nil and empty stays empty: the difference is what tells a
		// target whether the author asked for no protection or said nothing at
		// all, so this cannot go through a `len(...) > 0` guard.
		if raw.Interruption.Protect != nil {
			protect := make([]InterruptionProtect, 0, len(raw.Interruption.Protect))
			for _, p := range raw.Interruption.Protect {
				protect = append(protect, InterruptionProtect(p))
			}
			conversation.Interruption.Protect = protect
		}
	}
	if raw.Inactivity != nil {
		conversation.Inactivity = &Inactivity{
			NudgeAfter: Duration(raw.Inactivity.NudgeAfter), EndAfter: Duration(raw.Inactivity.EndAfter),
		}
	}
	return conversation
}

// checkReachability walks the package forwards from the entry agent and refuses
// anything declared the walk never arrives at.
//
// checkToolRefs above proves every name an agent lists resolves. This is its
// mirror image, and it is the half that was missing: a control nobody attached
// was not filtered out of the generated project, it was simply never visited,
// so it compiled at exit 0 and vanished. An unreferenced destination was worse
// than dead — its environment name still reached .env.example and the generated
// startup check, so the agent refused to start over a phantom secret nothing
// would ever read.
//
// It runs here rather than in ir.Validate because reachability is a property of
// the package graph alone: no target can change the answer, so it fires once for
// the package rather than once per target, and this is the only stage that can
// carry the file and line (research D1, D7).
//
// One carve-out, and it is the models map: it is a palette, so entries that
// nothing currently references are legal. That rule is scoped to models and to
// nothing else.
func checkReachability(pkg *packagespec.Package) error {
	agents, controls, tools := map[string]bool{}, map[string]bool{}, map[string]bool{}
	tasks, groups, destinations := map[string]bool{}, map[string]bool{}, map[string]bool{}

	var visitAgent, visitTask, visitGroup func(string)
	// An agent and a task attach the same way: by name, from typed lists. Which
	// list a name came out of is no longer guessed, it is the catalog the name
	// resolves in, and a name that resolves in none is checkAttachments' error to
	// report, not this walk's.
	attach := func(lists ...[]string) {
		for _, names := range lists {
			for _, name := range names {
				switch catalogOf(pkg, name) {
				case "tools":
					tools[name] = true
				case "tasks":
					if controls[name] {
						continue
					}
					controls[name] = true
					visitTask(name)
				case "task_groups":
					if controls[name] {
						continue
					}
					controls[name] = true
					visitGroup(name)
				case "handoffs":
					if controls[name] {
						continue
					}
					controls[name] = true
					visitAgent(pkg.Agent.Handoffs[name].To)
				case "escalations":
					controls[name] = true
					destinations[pkg.Agent.Escalations[name].TransferDestination()] = true
				}
			}
		}
	}
	visitAgent = func(name string) {
		if agents[name] {
			return
		}
		agents[name] = true
		def := pkg.Agent.Agents[name]
		// callables rather than every task the agent writes: defining a step-only
		// task does not run it, so a task nothing else reaches is still unreachable
		// and is still refused by name below.
		attach(def.Tools, callables(def, pkg), def.Handoffs, def.Escalations)
	}
	visitTask = func(name string) {
		if tasks[name] {
			return
		}
		tasks[name] = true
		def := pkg.Tasks[name]
		attach(def.Tools, def.Handoffs)
	}
	visitGroup = func(name string) {
		if groups[name] {
			return
		}
		groups[name] = true
		raw := pkg.Agent.TaskGroups[name]
		for _, step := range raw.Steps {
			visitTask(step)
		}
		if raw.ThenTarget != "" {
			visitAgent(raw.ThenTarget)
		}
	}
	visitAgent(pkg.Agent.EntryAgent)
	// A prefetch entry reaches its tool. This is the whole point of FR-003: a tool
	// that exists only to be pre-fetched is never advertised to any model, so it
	// appears in no agent's tools: list, and without this it would read as
	// unreachable and be refused. The refusal it replaces is a real one though, so
	// the name still has to resolve: an entry naming a tool the package does not
	// declare is refused by name in buildPrefetch.
	for _, entry := range pkg.Agent.Prefetch {
		if entry.Tool != "" {
			tools[entry.Tool] = true
		}
	}

	// The fix rides the message, and never suggests an impossible one: with no
	// reachable agent to attach to, the hint is omitted rather than invented.
	attachable := func(list string) string {
		var names []string
		if agents[pkg.Agent.EntryAgent] {
			names = append(names, pkg.Agent.EntryAgent)
		}
		for _, name := range sortedKeys(pkg.Agent.Agents) {
			if agents[name] && name != pkg.Agent.EntryAgent {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			return ""
		}
		return fmt.Sprintf("; add it to the %s: of one of these agents: %s", list, strings.Join(names, ", "))
	}

	// Reported in graph order, nearest declaration first: an unattached handoff
	// makes the agent behind it unreachable too, and naming the handoff is the one
	// edit that fixes both.
	//
	// ponytail: the first one, not all of them. Build returns a single error and
	// every other check in this file does the same, so a package with three
	// unreachable declarations takes three runs to clear. Collect them into one
	// message when somebody hits that in practice; the graph order above is what
	// makes the single error the useful one.
	for _, catalog := range []struct {
		list  string
		names []string
	}{
		{"handoffs", sortedKeys(pkg.Agent.Handoffs)},
		{"escalations", sortedKeys(pkg.Agent.Escalations)},
	} {
		for _, name := range catalog.names {
			if !controls[name] {
				return fmt.Errorf("%s: %s %q is declared but no agent reaches it%s", pkg.Location("agent.yaml", name), singular(catalog.list), name, attachable(catalog.list))
			}
		}
	}
	for _, name := range sortedKeys(pkg.Agent.Destinations) {
		if !destinations[name] {
			return fmt.Errorf("%s: destination %q is declared but no escalation resolves to it", pkg.Location("agent.yaml", name), name)
		}
	}
	for _, name := range sortedKeys(pkg.Tools) {
		if !tools[name] {
			return fmt.Errorf("%s: tool %q is declared but no agent reaches it%s", pkg.Location("agent.yaml", name), name, attachable("tools"))
		}
	}
	for _, name := range sortedKeys(pkg.Agent.TaskGroups) {
		if !groups[name] {
			return fmt.Errorf("%s: task group %q is declared but no agent reaches it%s", pkg.Location("agent.yaml", name), name, attachable("task_groups"))
		}
	}
	for _, name := range sortedKeys(pkg.Tasks) {
		if !tasks[name] {
			return fmt.Errorf("%s: task %q has no when: and no task group lists it in steps:, so nothing runs it. Give it a when: so its agent can decide to run it, or list it as a step of a task group that is reached", pkg.Location("agent.yaml", name), name)
		}
	}
	for _, name := range sortedKeys(pkg.Agent.Agents) {
		if !agents[name] {
			return fmt.Errorf("%s: agent %q is declared but the entry agent %q cannot reach it, directly or through a handoff", pkg.Location("agent.yaml", name), name, pkg.Agent.EntryAgent)
		}
	}
	return nil
}

// attached flattens the kind-named lists an agent or a task writes back into the
// single ordered list the intermediate representation holds.
//
// The order is load-bearing and is the reading order: tools, delegates,
// handoffs, escalations. Both drivers walk this list in order. LiveKit
// re-partitions it into four buckets as it walks, so only within-bucket order
// survives there and concatenating by kind preserves it exactly. Pipecat does
// NOT fully partition: it appends a human transfer into its tool list beside
// real tools, so on Pipecat the relative order of tools and escalations is
// visible in the emitted project. No package in this repository interleaves them,
// which is why every package still compiles byte for byte.
func attached(lists ...[]string) []string {
	var out []string
	for _, list := range lists {
		out = append(out, list...)
	}
	return out
}

// singular turns a catalog key into the noun an error message uses for one of
// its entries. A lookup rather than a suffix trim, because `task_groups`
// singularises to "task group" and no rule about the letter s gets there.
func singular(list string) string {
	switch list {
	case "task_groups":
		return "task group"
	default:
		return strings.TrimSuffix(list, "s")
	}
}

// article is the indefinite article for a kind name, so the one kind that
// begins with a vowel reads as "an escalation" rather than "a escalation".
func article(kind string) string {
	if strings.ContainsRune("aeiou", rune(kind[0])) {
		return "an"
	}
	return "a"
}

// catalogOf reports which of the five lists declares name, or "" when nothing
// does. All five kinds share one flat namespace, so at most one answers.
func catalogOf(pkg *packagespec.Package, name string) string {
	switch {
	case has(pkg.Tools, name):
		return "tools"
	case has(pkg.Tasks, name):
		return "tasks"
	case has(pkg.Agent.TaskGroups, name):
		return "task_groups"
	case has(pkg.Agent.Handoffs, name):
		return "handoffs"
	case has(pkg.Agent.Escalations, name):
		return "escalations"
	}
	return ""
}

// taskNames is the names an agent's `tasks:` list holds, whether each item
// defines a task or names one another agent defines.
func taskNames(raw packagespec.AgentDef) []string {
	out := make([]string, 0, len(raw.Tasks))
	for _, item := range raw.Tasks {
		if item.Task != nil {
			out = append(out, item.Task.Name)
			continue
		}
		out = append(out, item.Ref)
	}
	return out
}

// taskRefs is only the bare names, the items that have to resolve to something
// written elsewhere. A definition resolves to itself, and it is where a step-only
// task is legitimately written, so putting one through checkAttachments would
// refuse the very shape task groups are authored in.
func taskRefs(raw packagespec.AgentDef) []string {
	var out []string
	for _, item := range raw.Tasks {
		if item.Task == nil {
			out = append(out, item.Ref)
		}
	}
	return out
}

// callables is what an agent can decide to run beyond its tools, handoffs and
// escalations: the tasks it runs, in authored order, then the task groups.
//
// A task with no `when:` contributes nothing, because there is no trigger for
// the agent to act on. checkAttachments refuses one written in an agent's list.
func callables(raw packagespec.AgentDef, pkg *packagespec.Package) []string {
	out := make([]string, 0, len(raw.Tasks)+len(raw.TaskGroups))
	for _, name := range taskNames(raw) {
		if _, ok := pkg.Callables[name]; ok {
			out = append(out, name)
		}
	}
	return append(out, raw.TaskGroups...)
}

func has[V any](m map[string]V, name string) bool {
	_, ok := m[name]
	return ok
}

// checkAttachments resolves the names an agent or a task lists under one key
// against the catalog of the same name.
//
// Kind-aware, which the single mixed list could not be. With five lists the
// mistake worth naming is a name written in the wrong one, so when the name does
// resolve, just not here, the message says where it IS declared and which list
// it belongs on.
//
// A task is the one kind that can resolve in the right list and still be wrong:
// a task with no `when:` is a definition only, so an agent has no trigger to act
// on and naming it does nothing. That is refused here rather than ignored.
func checkAttachments(pkg *packagespec.Package, list string, names []string) error {
	for _, name := range names {
		switch found := catalogOf(pkg, name); found {
		case list:
			if list != "tasks" {
				continue
			}
			if _, ok := pkg.Callables[name]; !ok {
				return fmt.Errorf("%s: task %q has no when:, so it is a task group's step rather than something an agent can decide to run. Give it a when: to make it a task this agent runs, or name the group that runs it",
					pkg.Location("agent.yaml", name), name)
			}
		case "":
			return missing(pkg, "agent.yaml", singular(list), name)
		default:
			kind := singular(found)
			return fmt.Errorf("%s: %q is %s %s, so move it out of the %s: list and into the %s: list",
				pkg.Location("agent.yaml", name), name, article(kind), kind, list, found)
		}
	}
	return nil
}

func missing(pkg *packagespec.Package, file, kind, name string) error {
	return fmt.Errorf("%s: %s %q does not resolve", pkg.Location(file, name), kind, name)
}

// shapeNames is the membership set resolveType needs, which is all it needs.
func shapeNames(shapes map[string]Shape) map[string]bool {
	out := make(map[string]bool, len(shapes))
	for name := range shapes {
		out[name] = true
	}
	return out
}

// variableOrder is every declared variable, in the order agent.yaml declared
// them. The authored order is read off the file, so a name it somehow missed is
// appended sorted rather than dropped: the composed state block covers every
// variable or it is not the state.
func variableOrder(pkg *packagespec.Package) []string {
	out := make([]string, 0, len(pkg.Agent.Variables))
	for _, name := range pkg.VariableOrder() {
		if _, ok := pkg.Agent.Variables[name]; ok && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	for _, name := range sortedKeys(pkg.Agent.Variables) {
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

func sortedKeys[V any](values map[string]V) []string {
	return slices.Sorted(maps.Keys(values))
}

// intersect keeps the members of names that also appear in keep, in names' own
// order.
func intersect(names, keep []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if slices.Contains(keep, name) {
			out = append(out, name)
		}
	}
	return out
}
