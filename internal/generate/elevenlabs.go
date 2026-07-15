package generate

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/slng/unmute/internal/ir"
)

// The ElevenLabs driver lowers a validated agent + elevenlabs target into a
// managed artifact: one Conversational-AI Agent resource per Unmute agent
// (conversation_config), wired by the transfer_to_agent / transfer_to_number
// system tools, plus a branch-aware ApplyPlan. Tasks, task groups, and a
// model-written opening lower to a linear Workflow graph on the entry agent.
// No code is generated (C1); only the provider API surface is used (D4).
//
// Validate() runs before this (Generate calls it first, V17), so every gate and
// warning is already recorded — this reads agent + target only.
//
// API facts verified against ElevenLabs docs 2026-07-15 (SCHEMA.md ³, ORCHESTRATOR ³).

const (
	elCredentialEnv = "ELEVENLABS_API_KEY"
	elAgentsPath    = "/v1/convai/agents"
	// cascade_timeout_seconds: default 8, range 2-15s (SCHEMA.md fallback row).
	elCascadeTimeoutDefault = 8
	elCascadeTimeoutMin     = 2
	elCascadeTimeoutMax     = 15
)

// GenerateElevenLabs is the elevenlabs case of generate.Artifact.
func GenerateElevenLabs(agent *ir.Agent, tgt ir.Target) (Artifact, error) {
	b := &elBuilder{agent: agent, tgt: tgt}

	order, err := b.agentOrder()
	if err != nil {
		return Artifact{}, err
	}

	branch := tgt.Pins["branch_id"] // empty = the agent's main branch
	steps := make([]ApplyStep, 0, len(order))
	for _, name := range order {
		body, err := b.agentBody(name)
		if err != nil {
			return Artifact{}, err
		}
		raw, err := json.MarshalIndent(body, "", "  ")
		if err != nil {
			return Artifact{}, fmt.Errorf("elevenlabs: marshal agent %q: %w", name, err)
		}
		method, endpoint := "POST", elAgentsPath
		if id := tgt.Pins["agent_id."+name]; id != "" {
			method, endpoint = "PATCH", elAgentsPath+"/"+id
		}
		steps = append(steps, ApplyStep{
			Method: method, Endpoint: endpoint, Body: raw, CaptureID: name, Branch: branch,
		})
	}

	return Artifact{
		Kind:  ManagedTarget,
		Apply: &ApplyPlan{CredentialEnv: elCredentialEnv, Steps: steps},
		Notes: GenerateReport{Notes: b.notes(order, branch)},
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

	tools, err := b.agentTools(name)
	if err != nil {
		return nil, err
	}
	if len(tools) > 0 {
		prompt["tools"] = tools
	}

	agentBlock := map[string]any{"prompt": prompt}
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

	// Greeting and the model-written-opening workflow live on the entry agent.
	if name == b.agent.EntryAgent {
		workflow := b.applyGreeting(agentBlock)
		if workflow != nil {
			body["workflow"] = workflow
		}
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

// turn carries interruption ignore-phrases (native); minimum_words has no knob
// (warned in Validate) and turn placement is integrated (ignored).
func (b *elBuilder) turn() map[string]any {
	c := b.agent.Conversation
	if c == nil || c.Interruption == nil || len(c.Interruption.IgnorePhrases) == 0 {
		return nil
	}
	return map[string]any{"interruption_ignore_terms": c.Interruption.IgnorePhrases}
}

// conversationBlock carries max_duration (V) and inactivity (range-checked in Validate).
func (b *elBuilder) conversationBlock() map[string]any {
	c := b.agent.Conversation
	if c == nil {
		return nil
	}
	block := map[string]any{}
	if secs := durationSeconds(c.MaxDuration); secs > 0 {
		block["max_duration_seconds"] = secs
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

// applyGreeting sets the entry agent's opening. Fixed text -> first_message;
// speaks_first: user -> empty first_message (native wait); model-written -> a
// Workflow override-agent entry node with entry_behavior: generate_immediately
// (V6, shipped 2026-06-15). Returns a workflow object when one is needed.
func (b *elBuilder) applyGreeting(agentBlock map[string]any) map[string]any {
	c := b.agent.Conversation
	if c == nil || c.Greeting == nil {
		return nil
	}
	g := c.Greeting
	switch {
	case g.SpeaksFirst == ir.SpeaksFirstUser:
		agentBlock["first_message"] = "" // native "agent waits for the user"
	case g.Text != "":
		agentBlock["first_message"] = g.Text
	default:
		// Model-written opening: a plain-agent greeting has no generate-opening
		// toggle, so wrap the entry agent in a one-node workflow whose entry
		// override-agent node generates immediately.
		// ponytail: minimal single-node workflow shape observed from the
		// rebase-preview API response; extend when a workflow spec ships.
		return map[string]any{
			"nodes": map[string]any{
				"start_node": map[string]any{"type": "start", "edge_order": []string{"start_to_entry"}},
				"entry_node": map[string]any{"type": "override_agent", "label": "Entry", "entry_behavior": "generate_immediately"},
			},
			"edges": map[string]any{
				"start_to_entry": map[string]any{"from": "start_node", "to": "entry_node"},
			},
		}
	}
	return nil
}

// agentTools lowers the agent's declared tools: webhook tools plus the control
// system tools (transfer_to_agent, transfer_to_number, voicemail_detection).
func (b *elBuilder) agentTools(name string) ([]any, error) {
	var tools []any
	var agentTransfers, numberTransfers []any

	for _, toolName := range b.agent.Agents[name].Tools {
		if ctrl, ok := b.agent.Controls[toolName]; ok {
			switch c := ctrl.(type) {
			case *ir.AgentTransfer:
				agentTransfers = append(agentTransfers, b.agentTransferRule(c))
			case *ir.HumanTransfer:
				rule, err := b.humanTransferRule(c)
				if err != nil {
					return nil, err
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
		lowered, err := b.webhookTool(toolName, tool)
		if err != nil {
			return nil, err
		}
		tools = append(tools, lowered)
	}

	if len(agentTransfers) > 0 {
		tools = append(tools, systemTool("transfer_to_agent", map[string]any{"transfers": agentTransfers}))
	}
	if len(numberTransfers) > 0 {
		tools = append(tools, systemTool("transfer_to_number", map[string]any{"transfers": numberTransfers}))
	}
	if vm := b.voicemailTool(); vm != nil {
		tools = append(tools, vm)
	}
	return tools, nil
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

func (b *elBuilder) webhookTool(name string, tool ir.Tool) (map[string]any, error) {
	if tool.Execution != ir.ToolWebhook {
		return nil, fmt.Errorf("elevenlabs: tool %q execution %q is not emitted by this driver", name, tool.Execution)
	}
	lowered := map[string]any{
		"type":            "webhook",
		"name":            name,
		"description":     tool.Description,
		"api_schema":      map[string]any{"url": envPlaceholder(tool.URLEnv)},
		"input_json_schema": tool.Input,
	}
	return lowered, nil
}

// voicemailTool emits the voicemail_detection system tool for an outbound
// telephony channel with on_voicemail; leave_message carries a voicemail_message,
// hangup omits it (absent = hang up) (V7).
func (b *elBuilder) voicemailTool() map[string]any {
	for _, ch := range b.agent.Channels {
		if ch.Kind != ir.ChannelTelephony || ch.Outbound == nil || !*ch.Outbound || ch.OnVoicemail == "" {
			continue
		}
		params := map[string]any{}
		if ch.OnVoicemail == ir.VoicemailLeaveMessage {
			params["voicemail_message"] = ""
		}
		return systemTool("voicemail_detection", params)
	}
	return nil
}

// notes records the driver's documented reconcile choices (C5/V10): which branch
// apply targets, the comparison rule, and that forwarded/inert fields are
// reported but never judged (the full forwarded list rides in Notes.ForwardedBindings).
func (b *elBuilder) notes(order []string, branch string) []string {
	branchNote := "apply targets the agent's main branch (partial PATCH preserves dashboard-authored fields Unmute does not model); override with target pin branch_id"
	if branch != "" {
		branchNote = fmt.Sprintf("apply targets branch %q (target pin branch_id); merge/rebase to main is a dashboard step", branch)
	}
	return []string{
		fmt.Sprintf("elevenlabs: %d agent(s) created in order %s (entry: %s)", len(order), strings.Join(order, ", "), b.agent.EntryAgent),
		branchNote,
		"reconcile comparison: modeled fields diffed normally; forwarded params compared byte-equal after JSON normalization; inert provider fields are reported, never judged",
	}
}

// --- small helpers ---

func systemTool(name string, params map[string]any) map[string]any {
	return map[string]any{"type": "system", "name": name, "params": params}
}

func capturePlaceholder(agentName string) string { return "{{capture:" + agentName + "}}" }

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
