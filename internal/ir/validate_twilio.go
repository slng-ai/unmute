package ir

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	targetcap "github.com/slng-ai/unmute/internal/target"
)

// The checks only a twilio target makes, beside validate.go for the reason
// validate_slng.go sits there: the platform's refusals have one home.
//
// Most unsupported behaviour is already refused by a capability row
// (twilio_target.go). What is here is what no row can say: shapes of the
// package as a whole, and values a row cannot see.
//
// Every message opens with "twilio target", because twilio is also a carrier
// on the livekit and pipecat routes and an unprefixed message would send the
// reader to the wrong file.

// elevenLabsVoiceID is a bare ElevenLabs voice id. ConversationRelay takes the
// voice as "<id>-<model>", and the model comes from the binding's own model
// field, so an id that already carries a suffix would be ambiguous.
var elevenLabsVoiceID = regexp.MustCompile(`^[A-Za-z0-9]{8,64}$`)

// twilioThinkOwned are request fields the app sets itself. A param of the same
// name would fight the app for the request.
var twilioThinkOwned = map[string][]string{
	"openai": {"messages", "model", "n", "stream", "stream_options", "tool_choice", "tools"},
	"google": {"automatic_function_calling", "config", "contents", "model", "system_instruction", "tool_config", "tools"},
}

// twilioThinkForeign are params that belong to the other vendor. They would
// reach a request that has no such field.
var twilioThinkForeign = map[string][]string{
	"openai": {"location", "thinking_config", "vertexai"},
	"google": {"parallel_tool_calls", "reasoning_effort"},
}

// validateTwilioTarget is the whole twilio-specific pass, called from
// validateTarget once the shared checks have run.
func validateTwilioTarget(agent *Agent, resolved Target, row *TargetValidation) {
	refuse := func(format string, args ...any) {
		row.Errors = add(row.Errors, "twilio target "+fmt.Sprintf(format, args...))
	}
	validateTwilioTargetValues(resolved, refuse)
	if agent.Architecture != ArchitectureCascade {
		return // the capability row already refused the architecture
	}
	if len(agent.Agents) != 1 {
		refuse("runs one agent, and this package declares %d (%s): keep the entry agent and fold the others into its instructions, or compile to livekit or pipecat",
			len(agent.Agents), strings.Join(sortedKeys(agent.Agents), ", "))
	}
	if len(agent.Controls) > 0 {
		refuse("emits no handoff, transfer or delegate control, and this package declares %s: remove them, or compile to livekit or pipecat",
			strings.Join(sortedKeys(agent.Controls), ", "))
	}
	if len(agent.Variables) > 0 {
		refuse("has no session state, and this package declares variables (%s): remove them and say the facts in the instructions, or compile to livekit or pipecat",
			strings.Join(sortedKeys(agent.Variables), ", "))
	}
	if len(agent.Shapes) > 0 {
		refuse("has no session state, so shapes (%s) have nothing to describe: remove them, or compile to livekit or pipecat",
			strings.Join(sortedKeys(agent.Shapes), ", "))
	}
	validateTwilioChannels(agent, refuse)
	validateTwilioConversation(agent, refuse)
	validateTwilioSpeech(agent, resolved, refuse)
	validateTwilioThink(resolved, refuse)
	for _, name := range sortedKeys(agent.Tools) {
		validateTwilioTool(name, agent.Tools[name], refuse)
	}
}

// validateTwilioTargetValues refuses the target settings that describe a
// framework or a platform deployment. The shared checks accept them silently
// here, because there is no support window and no pin floor to check against.
func validateTwilioTargetValues(resolved Target, refuse func(string, ...any)) {
	if resolved.Version != "" {
		refuse("has no framework version, because the app runs on no framework: remove version: %s", resolved.Version)
	}
	if len(resolved.Pins) > 0 {
		refuse("pins its own dependencies inside the generated project, so pins (%s) reach nothing: remove pins",
			strings.Join(sortedKeys(resolved.Pins), ", "))
	}
	if len(resolved.DeploymentRegions) == 1 {
		refuse("is one process you host, with no deployment region: remove deployment_region %s and run it where you choose", resolved.DeploymentRegions[0])
	}
}

func validateTwilioChannels(agent *Agent, refuse func(string, ...any)) {
	if len(agent.Channels) != 1 {
		refuse("answers one inbound phone channel, and this package declares %d channels: keep one channels entry with kind: telephony, inbound: true, outbound: false", len(agent.Channels))
		return
	}
	for name, channel := range agent.Channels {
		if channel.Kind != ChannelTelephony {
			refuse("answers phone calls only, so channel %q of kind %s has no transport: use kind: telephony, or compile to livekit or pipecat for a browser channel", name, channel.Kind)
			continue
		}
		if channel.Inbound == nil || !*channel.Inbound {
			refuse("answers inbound calls only: set inbound: true on channel %q", name)
		}
		if channel.Outbound == nil || *channel.Outbound {
			refuse("answers inbound calls only: set outbound: false on channel %q", name)
		}
	}
}

func validateTwilioConversation(agent *Agent, refuse func(string, ...any)) {
	conversation := agent.Conversation
	if conversation == nil || conversation.Greeting == nil {
		return // FieldGreetingAbsent refuses it with the target's wording
	}
	if greeting := conversation.Greeting; greeting.SpeaksFirst == SpeaksFirstUser && greeting.Text != "" {
		refuse("waits silently when the caller speaks first, so greeting text %q would never be said: remove the text, or set speaks_first: agent", greeting.Text)
	}
	if interruption := conversation.Interruption; interruption != nil {
		for _, stretch := range interruption.Protect {
			if stretch != ProtectGreeting {
				refuse("can protect only the greeting from barge-in (welcomeGreetingInterruptible), not %s: remove it from conversation.interruption.protect", stretch)
			}
		}
	}
}

// validateTwilioSpeech checks the listen, speak and turn bindings, which all
// become attributes on one <ConversationRelay> element.
func validateTwilioSpeech(agent *Agent, resolved Target, refuse func(string, ...any)) {
	if binding := resolved.Models.Listen; binding != nil && len(binding.Params) > 0 {
		refuse("forwards no listen params to ConversationRelay, and this binding sets %s: remove them", strings.Join(sortedKeys(binding.Params), ", "))
	}
	for _, name := range sortedKeys(resolved.Models.Speak) {
		binding := resolved.Models.Speak[name]
		if len(binding.Params) > 0 {
			refuse("forwards no speak params to ConversationRelay, and speak.%s sets %s: remove them", name, strings.Join(sortedKeys(binding.Params), ", "))
		}
		voice := binding.Voice
		if voice == "" {
			voice = binding.VoiceID
		}
		if binding.Provider == "elevenlabs" && voice != "" && !elevenLabsVoiceID.MatchString(voice) {
			refuse("speak.%s voice %q is not a bare ElevenLabs voice id: write the id alone, for example UgBBYS2sOqTuMpoF3BR0, and put the model (for example flash_v2_5) in model:; the voice attribute is built as <id>-<model>", name, voice)
		}
	}
	turn := resolved.Models.Turn
	if turn == nil {
		return
	}
	if turn.Provider != "" || turn.Model != "" || turn.Placement != "" || turn.EndpointEnv != "" {
		refuse("has ConversationRelay decide the turn, so the turn entry carries settings only: remove provider, model, placement and endpoint_env from models.turn")
	}
	for _, key := range sortedKeys(turn.Params) {
		if err := targetcap.CheckTwilioTurnParam(key, turn.Params[key]); err != nil {
			refuse("%s", strings.TrimPrefix(err.Error(), "twilio target "))
		}
	}
}

// validateTwilioThink holds a think binding to the two request shapes the app
// sends. Everything not refused here is forwarded to the request verbatim, the
// catalogue's rule for params.
func validateTwilioThink(resolved Target, refuse func(string, ...any)) {
	for _, name := range sortedKeys(resolved.Models.Reason) {
		binding := resolved.Models.Reason[name]
		vendor := binding.Provider
		if vendor == "gemini" {
			vendor = "google"
		}
		for _, key := range sortedKeys(binding.Params) {
			switch {
			case slices.Contains(twilioThinkOwned[vendor], key):
				refuse("think.%s sets params.%s, which the app sets itself on every request: remove it", name, key)
			case slices.Contains(twilioThinkForeign[vendor], key):
				refuse("think.%s sets params.%s, which %s requests do not take: remove it; it belongs to the other think provider", name, key, vendor)
			}
		}
	}
}

// validateTwilioTool refuses the tool shapes the app does not run. Kinds a
// capability row covers are left to that row; webhook has no row and is named
// here.
func validateTwilioTool(name string, tool Tool, refuse func(string, ...any)) {
	switch tool.Execution {
	case ToolWebhook:
		refuse("runs only local handlers and the builtin end_call, so webhook tool %q is not emitted: move the request into a local: handler, or compile to livekit or pipecat", name)
		return
	case ToolBuiltin:
		if tool.Instructions != "" {
			refuse("ends the call at once on end_call and speaks no closing line, so tool %q instructions reach nothing: remove instructions and let the agent say goodbye before it calls end_call", name)
		}
		return
	case ToolLocal:
	default:
		return
	}
	if tool.Effect == ToolEndsConversation {
		refuse("ends a call only through the builtin end_call, so local tool %q with effect: ends_conversation is not honoured: set effect: returns_data, and add the builtin end_call tool", name)
	}
	for _, part := range []struct {
		label  string
		schema map[string]any
	}{{"input", tool.Input}, {"output", tool.Output}} {
		for _, problem := range flatSchemaProblems(part.schema) {
			refuse("tool %q %s: %s", name, part.label, problem)
		}
	}
}

// flatPrimitiveTypes are the JSON Schema types the app validates with a
// generated Pydantic model.
var flatPrimitiveTypes = []string{"boolean", "integer", "number", "string"}

// flatSchemaProblems names what keeps a schema from being a flat object of
// primitive fields. Empty means the schema is supported. A nil schema is
// supported too: no input means the tool takes no arguments, and no output
// means the result is passed through unchecked.
func flatSchemaProblems(schema map[string]any) []string {
	if schema == nil {
		return nil
	}
	fix := "the twilio target validates a flat object of string, integer, number and boolean fields; simplify the schema, or compile to livekit or pipecat"
	var problems []string
	for _, key := range sortedKeys(schema) {
		switch key {
		case "type", "properties", "required", "description", "title":
		case "additionalProperties":
			if schema[key] != false {
				problems = append(problems, "additionalProperties must be false or absent: "+fix)
			}
		default:
			problems = append(problems, fmt.Sprintf("keyword %q is not represented: %s", key, fix))
		}
	}
	if schema["type"] != "object" {
		problems = append(problems, fmt.Sprintf("type is %v, not object: %s", schema["type"], fix))
	}
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range slices.Sorted(maps.Keys(properties)) {
		property, ok := properties[field].(map[string]any)
		if !ok {
			problems = append(problems, fmt.Sprintf("field %q is not a schema object: %s", field, fix))
			continue
		}
		kind, _ := property["type"].(string)
		if !slices.Contains(flatPrimitiveTypes, kind) {
			problems = append(problems, fmt.Sprintf("field %q has type %v: %s", field, property["type"], fix))
			continue
		}
		for _, key := range sortedKeys(property) {
			switch key {
			case "type", "description", "title":
			case "enum":
				values, _ := property["enum"].([]any)
				if len(values) == 0 {
					problems = append(problems, fmt.Sprintf("field %q enum is empty: %s", field, fix))
				}
				for _, value := range values {
					if !primitiveMatches(kind, value) {
						problems = append(problems, fmt.Sprintf("field %q enum value %v is not a %s: %s", field, value, kind, fix))
					}
				}
			default:
				problems = append(problems, fmt.Sprintf("field %q keyword %q is not represented: %s", field, key, fix))
			}
		}
	}
	required, _ := schema["required"].([]any)
	for _, entry := range required {
		name, _ := entry.(string)
		if _, ok := properties[name]; !ok {
			problems = append(problems, fmt.Sprintf("required field %v is not in properties", entry))
		}
	}
	return problems
}

func primitiveMatches(kind string, value any) bool {
	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		_, ok := wholeNumberValue(value)
		return ok
	case "number":
		switch value.(type) {
		case int, int64, uint64, float64:
			return true
		}
	}
	return false
}

func wholeNumberValue(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case uint64:
		return int64(v), true
	case float64:
		if v == float64(int64(v)) {
			return int64(v), true
		}
	}
	return 0, false
}
