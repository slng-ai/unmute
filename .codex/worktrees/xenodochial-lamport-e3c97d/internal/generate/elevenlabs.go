package generate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/slng/unmute/internal/ir"
)

// The ElevenLabs driver lowers a validated agent + elevenlabs target into a
// managed artifact: standalone workspace tool resources plus one
// Conversational-AI Agent resource per Unmute agent (conversation_config),
// wired by the transfer_to_agent / transfer_to_number built-in tools. Tasks,
// task groups, and a model-written opening lower to a linear Workflow graph on
// the entry agent. No code is generated (C1); only the provider API surface is
// used (D4).
//
// Validate() runs before this (Generate calls it first, V17), so every gate and
// warning is already recorded — this reads agent + target only.
//
// API surface verified against live ElevenLabs docs 2026-07-15 (context7
// /websites/elevenlabs_io, OpenAPI): agents create at POST /v1/convai/agents/create;
// webhook tools are standalone POST /v1/convai/tools resources referenced by
// prompt.tool_ids; system tools live in prompt.built_in_tools; turn-level fields
// (silence_end_call_timeout, interruption_ignore_terms) sit under conversation_config.turn.

const (
	elCredentialEnv   = "ELEVENLABS_API_KEY"
	elAgentsPath      = "/v1/convai/agents"        // GET / PATCH / DELETE by id
	elAgentCreatePath = "/v1/convai/agents/create" // POST create (not /agents)
	elToolsPath       = "/v1/convai/tools"         // POST create a workspace tool
	// cascade_timeout_seconds: default 8, range 2-15s (SCHEMA.md fallback row).
	elCascadeTimeoutDefault = 8
	elCascadeTimeoutMin     = 2
	elCascadeTimeoutMax     = 15
)

// GenerateElevenLabs is the elevenlabs case of generate.Artifact.
func GenerateElevenLabs(agent *ir.Agent, tgt ir.Target) (Artifact, error) {
	// Branch/draft targeting is a whole separate resource surface
	// (POST /v1/convai/agents/{id}/branches/... plus versioning enabled on the
	// agent), not a query param on create. Until that is wired, refuse a branch
	// pin loudly rather than silently ignore it (V10). Upgrade path: emit the
	// branches API when a branch-using spec ships.
	if branch := tgt.Pins["branch_id"]; branch != "" {
		return Artifact{}, fmt.Errorf("elevenlabs: branch_id pin %q: branch/draft-targeted apply is not supported (needs the drafts/branches API with versioning enabled); apply targets the agent's live config — drop the pin or manage branches in the dashboard", branch)
	}

	b := &elBuilder{agent: agent, tgt: tgt}

	order, err := b.agentOrder()
	if err != nil {
		return Artifact{}, err
	}

	tools := b.referencedWebhookTools()
	steps := make([]ApplyStep, 0, len(tools)+len(order))

	// Workspace tools are created first so agents can reference their captured
	// ids via prompt.tool_ids / node.additional_tool_ids.
	for _, name := range tools {
		cfg, err := b.webhookToolConfig(name, b.agent.Tools[name])
		if err != nil {
			return Artifact{}, err
		}
		raw, err := json.MarshalIndent(map[string]any{"tool_config": cfg}, "", "  ")
		if err != nil {
			return Artifact{}, fmt.Errorf("elevenlabs: marshal tool %q: %w", name, err)
		}
		steps = append(steps, ApplyStep{
			Method: "POST", Endpoint: elToolsPath, Body: raw, CaptureID: toolCaptureID(name),
		})
	}

	for _, name := range order {
		body, err := b.agentBody(name)
		if err != nil {
			return Artifact{}, err
		}
		raw, err := json.MarshalIndent(body, "", "  ")
		if err != nil {
			return Artifact{}, fmt.Errorf("elevenlabs: marshal agent %q: %w", name, err)
		}
		method, endpoint := "POST", elAgentCreatePath
		if id := tgt.Pins["agent_id."+name]; id != "" {
			method, endpoint = "PATCH", elAgentsPath+"/"+id
		}
		steps = append(steps, ApplyStep{
			Method: method, Endpoint: endpoint, Body: raw, CaptureID: name,
		})
	}

	return Artifact{
		Kind:  ManagedTarget,
		Apply: &ApplyPlan{CredentialEnv: elCredentialEnv, Steps: steps},
		Notes: GenerateReport{Notes: b.notes(order, tools)},
	}, nil
}

type elBuilder struct {
	agent *ir.Agent
	tgt   ir.Target
}

// agentOrder returns agents in create order: a transfer target is created before
// the agent that references it, so its captured id is available. Acyclic only
// (ponytail: mutual handoffs would need a two-pass create+wire; gated with a
// named upgrade path until a spec needs them).
func (b *elBuilder) agentOrder() ([]string, error) {
	const (
		unvisited = 0
		active    = 1
		done      = 2
	)
	state := make(map[string]int, len(b.agent.Agents))
	order := make([]string, 0, len(b.agent.Agents))
	var visit func(name string, stack []string) error
	visit = func(name string, stack []string) error {
		switch state[name] {
		case done:
			return nil
		case active:
			return fmt.Errorf("elevenlabs: transfer_to_agent cycle %s: id capture needs acyclic handoffs (two-pass wiring not emitted yet)", strings.Join(append(stack, name), " -> "))
		}
		state[name] = active
		for _, target := range b.transferTargets(name) {
			if _, ok := b.agent.Agents[target]; !ok {
				continue
			}
			if err := visit(target, append(stack, name)); err != nil {
				return err
			}
		}
		state[name] = done
		order = append(order, name)
		return nil
	}
	for _, name := range sortedAgentNames(b.agent) {
		if err := visit(name, nil); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// transferTargets lists the agents this agent hands off to via agent_transfer
// controls it exposes (from its tools list).
func (b *elBuilder) transferTargets(name string) []string {
	var targets []string
	for _, ctrl := range b.agentControls(name) {
		if t, ok := ctrl.(*ir.AgentTransfer); ok {
			targets = append(targets, t.To)
		}
	}
	return targets
}

// agentControls returns the controls an agent exposes, resolved from its tools
// list against the shared control namespace (ORCHESTRATOR rule 4).
func (b *elBuilder) agentControls(name string) []ir.Control {
	var out []ir.Control
	for _, tool := range b.agent.Agents[name].Tools {
		if ctrl, ok := b.agent.Controls[tool]; ok {
			out = append(out, ctrl)
		}
	}
	return out
}

// agentBody builds the create/update payload for one Unmute agent.
func (b *elBuilder) agentBody(name string) (map[string]any, error) {
	def := b.agent.Agents[name]
	prompt := map[string]any{"prompt": def.Instructions}

	if err := b.applyReason(prompt, def.Model); err != nil {
		return nil, err
	}
	b.applyFallback(prompt, def.Model)

	toolIDs, builtIn, err := b.agentToolRefs(name)
	if err != nil {
		return nil, err
	}
	if len(toolIDs) > 0 {
		prompt["tool_ids"] = toolIDs
	}
	if len(builtIn) > 0 {
		prompt["built_in_tools"] = builtIn
	}

	// ElevenAgents exposes one conversation language on the agent block; its
	// integrated ASR and TTS both follow this value.
	agentBlock := map[string]any{"prompt": prompt, "language": firstNonEmpty(b.agent.Language, "en")}
	conv := map[string]any{"agent": agentBlock}
	if asr := b.asr(); asr != nil {
		conv["asr"] = asr
	}
	if tts, err := b.tts(name); err != nil {
		return nil, err
	} else if tts != nil {
		conv["tts"] = tts
	}
	if turn := b.turn(); turn != nil {
		conv["turn"] = turn
	}
	if c := b.conversationBlock(); c != nil {
		conv["conversation"] = c
	}

	body := map[string]any{
		"name":                fmt.Sprintf("%s-%s", b.tgt.Name, name),
		"conversation_config": conv,
	}

	// The greeting's first_message lives on the entry agent; a model-written
	// opening needs the workflow entry node instead (V6).
	modelWritten := false
	if name == b.agent.EntryAgent {
		modelWritten = b.applyFirstMessage(agentBlock)
	}
	// Tasks and task groups (delegates) and a model-written opening lower to a
	// linear Workflow graph on this agent (V1, V2, V6).
	if delegates := b.agentDelegates(name); modelWritten || len(delegates) > 0 {
		workflow, err := b.workflow(name, modelWritten, delegates)
		if err != nil {
			return nil, err
		}
		body["workflow"] = workflow
	}
	return body, nil
}

// applyReason sets the LLM for the prompt. Open reason is a supported model id;
// a local-placement reason profile lowers to the documented custom LLM endpoint
// (the one placement exception on this managed target, C1/V9).
func (b *elBuilder) applyReason(prompt map[string]any, model string) error {
	binding := b.tgt.Models.Reason[model]
	profile := b.agent.Models[model]
	// temperature default 0; forward temperature/max_tokens and any extra params.
	prompt["temperature"] = 0
	for k, v := range binding.Params {
		prompt[k] = v
	}
	if profile.Placement == ir.PlacementLocal {
		if binding.EndpointEnv == "" {
			return fmt.Errorf("elevenlabs: local reason %q needs a custom-LLM endpoint_env", model)
		}
		// C9: no secret value in the plan; reference the env by name.
		prompt["custom_llm"] = map[string]any{"url": envPlaceholder(binding.EndpointEnv)}
		return nil
	}
	prompt["llm"] = binding.Model
	return nil
}

// applyFallback lowers models.fallback to backup_llm_config (preference override,
// ordered model-id list) plus the sibling cascade_timeout_seconds (V5). Entries
// are model ids only; per-entry params already warned in Validate.
func (b *elBuilder) applyFallback(prompt map[string]any, model string) {
	profile := b.agent.Models[model]
	if len(profile.Fallback) == 0 {
		return
	}
	order := make([]string, 0, len(profile.Fallback))
	for _, name := range profile.Fallback {
		order = append(order, b.tgt.Models.Reason[name].Model)
	}
	prompt["backup_llm_config"] = map[string]any{"preference": "override", "order": order}
	prompt["cascade_timeout_seconds"] = elCascadeTimeoutDefault
}

// asr is the integrated listen block: settings only, never an outside STT model
// (C2/V9). The listen binding's params tune the built-in ASR.
func (b *elBuilder) asr() map[string]any {
	if b.tgt.Models.Listen == nil || len(b.tgt.Models.Listen.Params) == 0 {
		return nil
	}
	asr := map[string]any{}
	for k, v := range b.tgt.Models.Listen.Params {
		asr[k] = v
	}
	return asr
}

// tts is the speak block: an ElevenLabs voice id only (C2). Params forwarded.
func (b *elBuilder) tts(agentName string) (map[string]any, error) {
	voice := b.agent.Agents[agentName].Voice
	binding, ok := b.tgt.Models.Speak[voice]
	if !ok {
		return nil, fmt.Errorf("elevenlabs: agent %q voice %q has no speak binding", agentName, voice)
	}
	tts := map[string]any{}
	for k, v := range binding.Params {
		tts[k] = v
	}
	switch {
	case binding.VoiceID != "":
		tts["voice_id"] = binding.VoiceID
	case binding.Voice != "":
		tts["voice_id"] = binding.Voice
	default:
		return nil, fmt.Errorf("elevenlabs: agent %q voice %q binding has no voice_id", agentName, voice)
	}
	return tts, nil
}

// turn carries the turn-level knobs: interruption ignore-phrases and the
// silence-before-hangup timeout. Both live under conversation_config.turn per
// the live TurnConfig schema (interruption_ignore_terms, silence_end_call_timeout);
// minimum_words has no knob (warned in Validate) and turn placement is integrated.
func (b *elBuilder) turn() map[string]any {
	c := b.agent.Conversation
	if c == nil {
		return nil
	}
	block := map[string]any{}
	if c.Interruption != nil && len(c.Interruption.IgnorePhrases) > 0 {
		block["interruption_ignore_terms"] = c.Interruption.IgnorePhrases
	}
	if c.Inactivity != nil {
		if secs := durationSeconds(c.Inactivity.EndAfter); secs > 0 {
			block["silence_end_call_timeout"] = secs
		}
	}
	if len(block) == 0 {
		return nil
	}
	return block
}

// conversationBlock carries max_duration (V, range-checked in Validate).
func (b *elBuilder) conversationBlock() map[string]any {
	c := b.agent.Conversation
	if c == nil {
		return nil
	}
	block := map[string]any{}
	if secs := durationSeconds(c.MaxDuration); secs > 0 {
		block["max_duration_seconds"] = secs
	}
	if len(block) == 0 {
		return nil
	}
	return block
}

// applyFirstMessage sets the entry agent's opening. Fixed text -> first_message;
// speaks_first: user -> empty first_message (native wait). Returns true for a
// model-written opening, which the caller lowers to a workflow entry node (V6).
func (b *elBuilder) applyFirstMessage(agentBlock map[string]any) (modelWritten bool) {
	c := b.agent.Conversation
	if c == nil || c.Greeting == nil {
		return false
	}
	g := c.Greeting
	switch {
	case g.SpeaksFirst == ir.SpeaksFirstUser:
		agentBlock["first_message"] = "" // native "agent waits for the user"
	case g.Text != "":
		agentBlock["first_message"] = g.Text
	default:
		return true
	}
	return false
}

// agentDelegates returns the delegate controls (tasks / task groups) an agent
// exposes via its tools, in declaration order (sorted by control name).
func (b *elBuilder) agentDelegates(name string) []*ir.Delegate {
	var out []*ir.Delegate
	for _, tool := range b.agent.Agents[name].Tools {
		if d, ok := b.agent.Controls[tool].(*ir.Delegate); ok {
			out = append(out, d)
		}
	}
	return out
}

// workflow lowers an agent's delegates (and a model-written opening) into a
// linear ElevenLabs Workflow: a start node, an override_agent entry node, and
// one override_agent subagent node per task/step, chained by edges whose
// forward_condition is the delegate's `when` (llm) or unconditional. A task
// returns to entry; a task group ends with then: return / transfer / end (V1,V2).
//
// ponytail: node/edge shapes follow the documented workflow schema (source /
// target / forward_condition; override_agent, start, end, agent-transfer, phone
// nodes); tool wiring on subagent nodes uses additional_tool_ids by name pending
// live tool-id resolution — verify the graph against the live API when a
// workflow-using spec ships. The golden locks exactly what is emitted.
func (b *elBuilder) workflow(agentName string, modelWritten bool, delegates []*ir.Delegate) (map[string]any, error) {
	nodes := map[string]any{}
	edges := map[string]any{}
	entry := map[string]any{"type": "override_agent", "label": "Entry"}
	if modelWritten {
		entry["entry_behavior"] = "generate_immediately" // shipped 2026-06-15 (V6)
	}
	var entryEdges []string
	addEdge := func(id, source, target string, cond map[string]any) {
		edges[id] = map[string]any{"source": source, "target": target, "forward_condition": cond}
	}

	nodes["start_node"] = map[string]any{"edge_order": []string{"start_to_entry"}}
	addEdge("start_to_entry", "start_node", "entry_node", map[string]any{})

	for _, d := range delegates {
		if err := b.checkAssign(d); err != nil {
			return nil, err
		}
		if d.Task != "" {
			node := "task_" + d.Task
			nodes[node] = b.subagentNode(d.Task)
			enter := "entry_to_" + node
			addEdge(enter, "entry_node", node, llmCondition(d.When))
			entryEdges = append(entryEdges, enter)
			back := node + "_to_entry" // then: return
			addEdge(back, node, "entry_node", map[string]any{})
			nodes[node].(map[string]any)["edge_order"] = []string{back}
			continue
		}
		group := b.agent.TaskGroups[d.Group]
		prev, cond := "entry_node", llmCondition(d.When)
		for i, step := range group.Steps {
			node := fmt.Sprintf("group_%s_%d_%s", d.Group, i, step)
			nodes[node] = b.subagentNode(step)
			edge := prev + "_to_" + node
			addEdge(edge, prev, node, cond)
			if prev == "entry_node" {
				entryEdges = append(entryEdges, edge)
			} else {
				nodes[prev].(map[string]any)["edge_order"] = []string{edge}
			}
			prev, cond = node, map[string]any{}
		}
		b.groupThen(nodes, edges, d.Group, prev, group)
	}

	entry["edge_order"] = entryEdges
	nodes["entry_node"] = entry
	return map[string]any{"nodes": nodes, "edges": edges}, nil
}

// groupThen wires the tail of a task group: return -> back to entry; transfer ->
// an agent-transfer node holding the target's captured id; end -> an end node (V2).
func (b *elBuilder) groupThen(nodes, edges map[string]any, group, last string, g ir.TaskGroup) {
	switch g.Then {
	case ir.GroupReturn:
		edge := last + "_to_entry"
		edges[edge] = map[string]any{"source": last, "target": "entry_node", "forward_condition": map[string]any{}}
		nodes[last].(map[string]any)["edge_order"] = []string{edge}
	case ir.GroupTransfer:
		node := "group_" + group + "_transfer"
		nodes[node] = map[string]any{"agent_id": capturePlaceholder(g.ThenTarget)}
		edge := last + "_to_" + node
		edges[edge] = map[string]any{"source": last, "target": node, "forward_condition": map[string]any{}}
		nodes[last].(map[string]any)["edge_order"] = []string{edge}
	case ir.GroupEnd:
		node := "group_" + group + "_end"
		nodes[node] = map[string]any{}
		edge := last + "_to_" + node
		edges[edge] = map[string]any{"source": last, "target": node, "forward_condition": map[string]any{}}
		nodes[last].(map[string]any)["edge_order"] = []string{edge}
	}
}

// subagentNode is an override_agent node for a task: it appends the task prompt
// and references the task's webhook tools by their apply-time captured ids
// (additional_tool_ids holds real tool ids, not Unmute tool names).
func (b *elBuilder) subagentNode(taskName string) map[string]any {
	task := b.agent.Tasks[taskName]
	node := map[string]any{
		"type":              "override_agent",
		"label":             taskName,
		"additional_prompt": task.Instructions,
	}
	var ids []any
	for _, name := range task.Tools {
		if tool, ok := b.agent.Tools[name]; ok && tool.Execution == ir.ToolWebhook {
			ids = append(ids, toolCapturePlaceholder(name))
		}
	}
	if len(ids) > 0 {
		node["additional_tool_ids"] = ids
	}
	return node
}

// checkAssign enforces C3/V1: a delegate whose assign: writes owner variables
// only compiles when the task routes its result through a tool returning JSON
// (a tool with an output schema). Otherwise there is no mid-call write path.
func (b *elBuilder) checkAssign(d *ir.Delegate) error {
	if len(d.Assign) == 0 || d.Task == "" {
		return nil
	}
	for _, toolName := range b.agent.Tasks[d.Task].Tools {
		if tool, ok := b.agent.Tools[toolName]; ok && tool.Output != nil {
			return nil
		}
	}
	return fmt.Errorf("elevenlabs: task %q assign requires a tool returning JSON — the only mid-call variable write path (C3)", d.Task)
}

func llmCondition(when string) map[string]any {
	if when == "" {
		return map[string]any{}
	}
	return map[string]any{"type": "llm", "condition": when}
}

// referencedWebhookTools returns the sorted, unique names of the webhook tools
// referenced by any agent or task — these become standalone workspace tool
// resources (POST /v1/convai/tools). Unreferenced or non-webhook entries are
// skipped so apply never creates orphan workspace tools.
func (b *elBuilder) referencedWebhookTools() []string {
	seen := map[string]bool{}
	add := func(name string) {
		if seen[name] {
			return
		}
		if tool, ok := b.agent.Tools[name]; ok && tool.Execution == ir.ToolWebhook {
			seen[name] = true
		}
	}
	for _, a := range b.agent.Agents {
		for _, t := range a.Tools {
			add(t)
		}
	}
	for _, task := range b.agent.Tasks {
		for _, t := range task.Tools {
			add(t)
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// agentToolRefs lowers the agent's declared tools into the current API surface:
// webhook tools by their apply-time captured ids (prompt.tool_ids) and the
// control system tools (transfer_to_agent, transfer_to_number,
// voicemail_detection) into prompt.built_in_tools.
func (b *elBuilder) agentToolRefs(name string) (toolIDs []any, builtIn map[string]any, err error) {
	builtIn = map[string]any{}
	var agentTransfers, numberTransfers []any

	for _, toolName := range b.agent.Agents[name].Tools {
		if ctrl, ok := b.agent.Controls[toolName]; ok {
			switch c := ctrl.(type) {
			case *ir.AgentTransfer:
				agentTransfers = append(agentTransfers, b.agentTransferRule(c))
			case *ir.HumanTransfer:
				rule, err := b.humanTransferRule(c)
				if err != nil {
					return nil, nil, err
				}
				numberTransfers = append(numberTransfers, rule)
			case *ir.Delegate:
				// Delegates lower to Workflow subagent nodes, not prompt tools.
			}
			continue
		}
		tool, ok := b.agent.Tools[toolName]
		if !ok {
			continue
		}
		if tool.Execution != ir.ToolWebhook {
			return nil, nil, fmt.Errorf("elevenlabs: tool %q execution %q is not emitted by this driver", toolName, tool.Execution)
		}
		toolIDs = append(toolIDs, toolCapturePlaceholder(toolName))
	}

	if len(agentTransfers) > 0 {
		builtIn["transfer_to_agent"] = builtInTool("transfer_to_agent", map[string]any{"transfers": agentTransfers})
	}
	if len(numberTransfers) > 0 {
		builtIn["transfer_to_number"] = builtInTool("transfer_to_number", map[string]any{"transfers": numberTransfers})
	}
	// Voicemail detection rides the entry agent, which answers the outbound call.
	if name == b.agent.EntryAgent {
		if vm, ok := b.voicemailParams(); ok {
			builtIn["voicemail_detection"] = builtInTool("voicemail_detection", vm)
		}
	}
	return toolIDs, builtIn, nil
}

func (b *elBuilder) agentTransferRule(c *ir.AgentTransfer) map[string]any {
	// The destination supplies its own config; the parent references its
	// (apply-time) captured id (V3).
	return map[string]any{
		"agent_id":  capturePlaceholder(c.To),
		"condition": c.When,
	}
}

// humanTransferRule lowers human_transfer: cold -> blind (phone) or sip_refer
// (SIP URI); warm -> conference. briefing: message -> agent_message, native
// Twilio only (V8, gated in Validate for SIP). briefing summary/wait already gated.
func (b *elBuilder) humanTransferRule(c *ir.HumanTransfer) (map[string]any, error) {
	dest, ok := b.tgt.Destinations[c.Destination]
	if !ok {
		return nil, fmt.Errorf("elevenlabs: human_transfer destination %q is not in the target's destinations map", c.Destination)
	}
	isSIP := strings.HasPrefix(strings.ToLower(dest), "sip:")
	rule := map[string]any{"condition": c.When}
	if isSIP {
		rule["transfer_destination"] = map[string]any{"type": "sip_uri", "sip_uri": dest}
		rule["transfer_type"] = "sip_refer"
	} else {
		rule["transfer_destination"] = map[string]any{"type": "phone", "phone_number": dest}
		if c.Mode == ir.TransferWarm {
			rule["transfer_type"] = "conference"
		} else {
			rule["transfer_type"] = "blind"
		}
	}
	if c.Briefing == ir.BriefingMessage {
		rule["agent_message"] = c.When // read to the operator (Twilio native)
	}
	return rule, nil
}

// webhookToolConfig builds the tool_config for a standalone webhook tool
// (POST /v1/convai/tools). The request input schema rides api_schema, not a
// tool-level field.
// ponytail: the spec models no HTTP verb, so a tool that takes input is sent as
// POST with a request_body_schema; wire method through the spec if a GET/query
// webhook tool is ever needed.
func (b *elBuilder) webhookToolConfig(name string, tool ir.Tool) (map[string]any, error) {
	if tool.Execution != ir.ToolWebhook {
		return nil, fmt.Errorf("elevenlabs: tool %q execution %q is not emitted by this driver", name, tool.Execution)
	}
	api := map[string]any{"url": envPlaceholder(tool.URLEnv)}
	if tool.Input != nil {
		api["method"] = "POST"
		api["request_body_schema"] = tool.Input
	}
	return map[string]any{
		"type":        "webhook",
		"name":        name,
		"description": tool.Description,
		"api_schema":  api,
	}, nil
}

// voicemailParams returns the voicemail_detection params for an outbound
// telephony channel with on_voicemail; leave_message carries a voicemail_message,
// hangup omits it (absent = hang up) (V7). The bool reports whether the tool applies.
func (b *elBuilder) voicemailParams() (map[string]any, bool) {
	for _, ch := range b.agent.Channels {
		if ch.Kind != ir.ChannelTelephony || ch.Outbound == nil || !*ch.Outbound || ch.OnVoicemail == "" {
			continue
		}
		params := map[string]any{}
		if ch.OnVoicemail == ir.VoicemailLeaveMessage {
			params["voicemail_message"] = ""
		}
		return params, true
	}
	return nil, false
}

// notes records the driver's documented reconcile choices (C5/V10): create
// order, what apply targets, the comparison rule, and that forwarded/inert
// fields are reported but never judged (the full forwarded list rides in
// Notes.ForwardedBindings).
func (b *elBuilder) notes(order, tools []string) []string {
	return []string{
		fmt.Sprintf("elevenlabs: %d tool(s) then %d agent(s) created in order %s (entry: %s)", len(tools), len(order), strings.Join(order, ", "), b.agent.EntryAgent),
		"apply targets the agent's live config (partial PATCH preserves dashboard-authored fields Unmute does not model); branch/draft targeting is not supported",
		"reconcile comparison: modeled fields diffed normally; forwarded params compared byte-equal after JSON normalization; inert provider fields are reported, never judged",
	}
}

// --- small helpers ---

// builtInTool wraps a system tool for prompt.built_in_tools: the value is a
// SystemToolConfig keyed by tool name, with system_tool_type in its params.
func builtInTool(name string, params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{}
	}
	params["system_tool_type"] = name
	return map[string]any{"type": "system", "name": name, "params": params}
}

func capturePlaceholder(agentName string) string { return "{{capture:" + agentName + "}}" }

// tool captures are namespaced so a tool and an agent can share a name without
// colliding in the apply-time capture map.
func toolCaptureID(name string) string          { return "tool:" + name }
func toolCapturePlaceholder(name string) string { return capturePlaceholder(toolCaptureID(name)) }

func envPlaceholder(env string) string { return "{{env:" + env + "}}" }

func durationSeconds(d ir.Duration) int {
	if d == "" {
		return 0
	}
	parsed, err := time.ParseDuration(string(d))
	if err != nil {
		return 0
	}
	return int(parsed.Seconds())
}
