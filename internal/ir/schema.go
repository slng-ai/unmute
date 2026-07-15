package ir

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

// Schema derives the resolved-IR schema at runtime.
func Schema() (*jsonschema.Schema, error) {
	controls, err := controlSchema()
	if err != nil {
		return nil, err
	}
	options := enumOptions()
	options.TypeSchemas[reflect.TypeFor[Control]()] = controls
	return jsonschema.For[Agent](options)
}

func controlSchema() (*jsonschema.Schema, error) {
	options := enumOptions()
	delegate, err := jsonschema.For[Delegate](options)
	if err != nil {
		return nil, err
	}
	agent, err := jsonschema.For[AgentTransfer](options)
	if err != nil {
		return nil, err
	}
	human, err := jsonschema.For[HumanTransfer](options)
	if err != nil {
		return nil, err
	}
	setKind(delegate, ControlDelegate)
	setKind(agent, ControlAgentTransfer)
	setKind(human, ControlHumanTransfer)
	return &jsonschema.Schema{OneOf: []*jsonschema.Schema{delegate, agent, human}}, nil
}

func setKind(schema *jsonschema.Schema, kind ControlKind) {
	value := any(kind)
	schema.Properties["kind"] = &jsonschema.Schema{Type: "string", Const: &value}
}

func enumOptions() *jsonschema.ForOptions {
	return &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[Placement]():           enum(PlacementAPI, PlacementLocal),
		reflect.TypeFor[SemanticEndpointing](): enum(SemanticEndpointingRequired, SemanticEndpointingPreferred, SemanticEndpointingOff),
		reflect.TypeFor[PrimitiveType]():       enum(PrimitiveString, PrimitiveNumber, PrimitiveBoolean, PrimitiveInteger),
		reflect.TypeFor[VariableSource]():      enum(VariableSourceCallStart),
		reflect.TypeFor[ControlKind]():         enum(ControlDelegate, ControlAgentTransfer, ControlHumanTransfer),
		reflect.TypeFor[History]():             enum(HistoryFull, HistoryMessages, HistoryLastN, HistorySummary, HistoryReset),
		reflect.TypeFor[ContextScope]():        enum(ContextShared, ContextIsolated),
		reflect.TypeFor[GroupThen]():           enum(GroupReturn, GroupTransfer, GroupEnd),
		reflect.TypeFor[GroupMerge]():          enum(GroupMergeResults),
		reflect.TypeFor[TransferMode]():        enum(TransferCold, TransferWarm),
		reflect.TypeFor[Briefing]():            enum(BriefingSummary, BriefingMessage, BriefingWait),
		reflect.TypeFor[ToolExecution]():       enum(ToolLocal, ToolClient, ToolWebhook, ToolProviderHosted, ToolBuiltin, ToolMCP),
		reflect.TypeFor[ToolInterruption]():    enum(ToolContinue, ToolCancel, ToolProviderDefault),
		reflect.TypeFor[ToolEffect]():          enum(ToolReturnsData, ToolEndsConversation),
		reflect.TypeFor[SpeaksFirst]():         enum(SpeaksFirstAgent, SpeaksFirstUser),
		reflect.TypeFor[ThinkingAudio]():       enum(ThinkingNone, ThinkingSubtle),
		reflect.TypeFor[ChannelKind]():         enum(ChannelRealtimeAudio, ChannelTelephony),
		reflect.TypeFor[VoicemailAction]():     enum(VoicemailHangup, VoicemailLeaveMessage),
		reflect.TypeFor[Provider]():            enum(ProviderLiveKit, ProviderPipecat, ProviderVapi, ProviderElevenLabs, ProviderDeepgram),
	}}
}

func enum[T ~string](values ...T) *jsonschema.Schema {
	items := make([]any, len(values))
	for i, value := range values {
		items[i] = value
	}
	return &jsonschema.Schema{Type: "string", Enum: items}
}
