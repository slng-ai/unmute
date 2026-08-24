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
	resultField, err := resultFieldSchema()
	if err != nil {
		return nil, err
	}
	options := enumOptions()
	options.TypeSchemas[reflect.TypeFor[Control]()] = controls
	options.TypeSchemas[reflect.TypeFor[ResultField]()] = resultField
	return jsonschema.For[Agent](options)
}

type primitiveResultFieldSchema struct {
	Type PrimitiveType `json:"type"`
}

type enumResultFieldSchema struct {
	Type PrimitiveType `json:"type"`
	Enum []string      `json:"enum"`
}

type nestedResultFieldSchema struct {
	Schema map[string]any `json:"schema"`
}

func resultFieldSchema() (*jsonschema.Schema, error) {
	options := enumOptions()
	primitive, err := jsonschema.For[primitiveResultFieldSchema](options)
	if err != nil {
		return nil, err
	}
	enumResult, err := jsonschema.For[enumResultFieldSchema](options)
	if err != nil {
		return nil, err
	}
	nested, err := jsonschema.For[nestedResultFieldSchema](options)
	if err != nil {
		return nil, err
	}
	value := any(PrimitiveString)
	enumResult.Properties["type"] = &jsonschema.Schema{Type: "string", Const: &value}
	return &jsonschema.Schema{OneOf: []*jsonschema.Schema{primitive, enumResult, nested}}, nil
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
		reflect.TypeFor[ModelKind]():           enum(KindThink, KindSpeak, KindListen, KindTurn),
		reflect.TypeFor[SemanticEndpointing](): enum(SemanticEndpointingRequired, SemanticEndpointingPreferred, SemanticEndpointingOff),
		reflect.TypeFor[PrimitiveType]():       enum(PrimitiveString, PrimitiveNumber, PrimitiveBoolean, PrimitiveInteger),
		reflect.TypeFor[VariableSource](): enum(
			VariableSourceCallStart, VariableSourceSessionID, VariableSourceCarrier,
			VariableSourceConnection, VariableSourceCallID, VariableSourceStreamID,
			VariableSourceDirection, VariableSourceFromNumber, VariableSourceToNumber,
		),
		reflect.TypeFor[ControlKind]():      enum(ControlDelegate, ControlAgentTransfer, ControlHumanTransfer),
		reflect.TypeFor[History]():          enum(HistoryFull, HistoryMessages, HistoryLastN, HistorySummary, HistoryReset),
		reflect.TypeFor[ContextScope]():     enum(ContextShared, ContextIsolated),
		reflect.TypeFor[GroupThen]():        enum(GroupReturn, GroupTransfer, GroupEnd),
		reflect.TypeFor[GroupMerge]():       enum(GroupMergeResults),
		reflect.TypeFor[TransferMode]():     enum(TransferCold, TransferWarm),
		reflect.TypeFor[OnUnavailable]():    enum(OnUnavailableReturn, OnUnavailableHangup),
		reflect.TypeFor[ToolExecution]():    enum(ToolLocal, ToolClient, ToolWebhook, ToolProviderHosted, ToolBuiltin, ToolMCP),
		reflect.TypeFor[ToolInterruption](): enum(ToolContinue, ToolCancel, ToolProviderDefault),
		reflect.TypeFor[ToolEffect]():       enum(ToolReturnsData, ToolEndsConversation),
		reflect.TypeFor[ToolAuthType]():     enum(ToolAuthBearer, ToolAuthAPIKey),
		reflect.TypeFor[SpeaksFirst]():      enum(SpeaksFirstAgent, SpeaksFirstUser),
		reflect.TypeFor[ThinkingAudio]():    enum(ThinkingNone, ThinkingSubtle),
		reflect.TypeFor[ChannelKind]():      enum(ChannelRealtimeAudio, ChannelTelephony),
		reflect.TypeFor[VoicemailAction]():  enum(VoicemailHangup, VoicemailLeaveMessage),
		reflect.TypeFor[Provider]():         enum(ProviderLiveKit, ProviderPipecat),
	}}
}

func enum[T ~string](values ...T) *jsonschema.Schema {
	items := make([]any, len(values))
	for i, value := range values {
		items[i] = value
	}
	return &jsonschema.Schema{Type: "string", Enum: items}
}
