package ir

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/slng-ai/unmute/internal/stateschema"
)

// Schema derives the resolved-IR schema at runtime.
func Schema() (*jsonschema.Schema, error) {
	controls, err := controlSchema()
	if err != nil {
		return nil, err
	}
	options := enumOptions()
	options.TypeSchemas[reflect.TypeFor[Control]()] = controls
	// Every state type publishes a reference to one definition, because a
	// type's items and fields hold types and reflection cannot follow a type
	// into itself: deriving it directly is a cycle the library refuses.
	options.TypeSchemas[reflect.TypeFor[stateschema.Type]()] = &jsonschema.Schema{Ref: stateTypePointer}
	schema, err := jsonschema.For[Agent](options)
	if err != nil {
		return nil, err
	}
	if schema.Defs == nil {
		schema.Defs = map[string]*jsonschema.Schema{}
	}
	schema.Defs[stateTypeDef] = stateTypeSchema()
	return schema, nil
}

// stateTypeDef names the one definition a state type lives in, and
// stateTypePointer is how every field referring to it points there.
const (
	stateTypeDef     = "StateType"
	stateTypePointer = "#/$defs/" + stateTypeDef
)

// stateTypeSchema publishes stateschema.Type. Hand-wired because it recurses.
// TestStateTypeSchemaNamesEveryField fails if the struct grows a field this
// misses, so the two cannot drift in silence.
func stateTypeSchema() *jsonschema.Schema {
	field := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{
		"name": {Type: "string"}, "description": {Type: "string"}, "type": {Ref: stateTypePointer},
		"required": {Type: "boolean"}, "default": {},
	}}
	return &jsonschema.Schema{
		Type:        "object",
		Description: "One state type, as Pydantic described it in state.py's JSON Schema.",
		Properties: map[string]*jsonschema.Schema{
			"kind":     {Type: "string", Enum: []any{"string", "integer", "number", "boolean", "object", "array"}},
			"nullable": {Type: "boolean"},
			"enum":     {Type: "array", Items: &jsonschema.Schema{Type: "string"}},
			"format":   {Type: "string"},
			"pattern":  {Type: "string"},
			"model":    {Type: "string"},
			"fields":   {Type: "array", Items: field},
			"items":    {Ref: stateTypePointer},
		},
	}
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
		reflect.TypeFor[Pace]():                enum(PaceSnappy, PaceBalanced, PacePatient),
		reflect.TypeFor[PrimitiveType]():       enum(PrimitiveString, PrimitiveNumber, PrimitiveBoolean, PrimitiveInteger),
		reflect.TypeFor[VariableSource](): enum(
			VariableSourceCallStart, VariableSourceSessionID, VariableSourceCarrier,
			VariableSourceConnection, VariableSourceCallID, VariableSourceStreamID,
			VariableSourceDirection, VariableSourceFromNumber, VariableSourceToNumber,
		),
		reflect.TypeFor[ControlKind]():   enum(ControlDelegate, ControlAgentTransfer, ControlHumanTransfer),
		reflect.TypeFor[History]():       enum(HistoryFull, HistoryMessages, HistoryLastN, HistorySummary, HistoryReset),
		reflect.TypeFor[ContextScope]():  enum(ContextShared, ContextIsolated),
		reflect.TypeFor[GroupThen]():     enum(GroupReturn, GroupTransfer, GroupEnd),
		reflect.TypeFor[GroupMerge]():    enum(GroupMergeResults),
		reflect.TypeFor[TransferMode]():  enum(TransferCold, TransferWarm),
		reflect.TypeFor[OnUnavailable](): enum(OnUnavailableReturn, OnUnavailableHangup),
		// All eight kinds. ToolKnowledge was missing here for as long as it has
		// existed, so the derived debug schema described a value the compiler
		// produces as illegal. Adding an eighth beside a missing seventh would
		// have read as deliberate, so both went in at once.
		reflect.TypeFor[ToolExecution]():    enum(ToolLocal, ToolClient, ToolWebhook, ToolProviderHosted, ToolBuiltin, ToolMCP, ToolKnowledge, ToolSlngHosted),
		reflect.TypeFor[ToolInterruption](): enum(ToolContinue, ToolCancel, ToolProviderDefault),
		reflect.TypeFor[ToolEffect]():       enum(ToolReturnsData, ToolEndsConversation),
		reflect.TypeFor[ToolAuthType]():     enum(ToolAuthBearer, ToolAuthAPIKey),
		reflect.TypeFor[SpeaksFirst]():      enum(SpeaksFirstAgent, SpeaksFirstUser),
		reflect.TypeFor[ThinkingAudio]():    enum(ThinkingNone, ThinkingSubtle),
		reflect.TypeFor[ChannelKind]():      enum(ChannelRealtimeAudio, ChannelTelephony),
		reflect.TypeFor[VoicemailAction]():  enum(VoicemailHangup, VoicemailLeaveMessage),
		reflect.TypeFor[Provider]():         enum(ProviderLiveKit, ProviderPipecat, ProviderSlng, ProviderTwilio),
	}}
}

func enum[T ~string](values ...T) *jsonschema.Schema {
	items := make([]any, len(values))
	for i, value := range values {
		items[i] = value
	}
	return &jsonschema.Schema{Type: "string", Enum: items}
}
