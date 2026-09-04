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
	// Every TypeRef publishes a reference to one definition, because a TypeRef's
	// `list` holds a TypeRef and reflection cannot follow a type into itself:
	// deriving it directly is a cycle the library refuses.
	options.TypeSchemas[reflect.TypeFor[TypeRef]()] = &jsonschema.Schema{Ref: typeRefPointer}
	schema, err := jsonschema.For[Agent](options)
	if err != nil {
		return nil, err
	}
	if schema.Defs == nil {
		schema.Defs = map[string]*jsonschema.Schema{}
	}
	schema.Defs[typeRefDef] = typeRefSchema()
	return schema, nil
}

// typeRefDef names the one definition the resolved type tree lives in, and
// typeRefPointer is how every field referring to it points there.
const (
	typeRefDef     = "TypeRef"
	typeRefPointer = "#/$defs/" + typeRefDef
)

// typeRefSchema publishes the resolved type tree.
//
// Hand-wired rather than derived, for the same reason resultFieldSchema is: the
// shape reflection would produce is not the shape the type means. Here the
// recursion is the reason. Every field of TypeRef appears below, and
// shape_test.go fails if the struct grows one this misses, so the two cannot
// drift in silence.
func typeRefSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Description: "One resolved type expression. Exactly one of primitive, shaped, literal, " +
			"list and shape is set; optional rides on whichever it is.",
		Properties: map[string]*jsonschema.Schema{
			"primitive": enum(PrimitiveString, PrimitiveNumber, PrimitiveBoolean, PrimitiveInteger),
			"shaped":    enum(ShapedPhone, ShapedDate, ShapedTime, ShapedID),
			"literal":   {Type: "array", Items: &jsonschema.Schema{Type: "string"}},
			"list":      {Ref: typeRefPointer},
			"shape":     {Type: "string"},
			"optional":  {Type: "boolean"},
		},
	}
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
		reflect.TypeFor[Pace]():                enum(PaceSnappy, PaceBalanced, PacePatient),
		reflect.TypeFor[PrimitiveType]():       enum(PrimitiveString, PrimitiveNumber, PrimitiveBoolean, PrimitiveInteger),
		reflect.TypeFor[ShapedText]():          enum(ShapedPhone, ShapedDate, ShapedTime, ShapedID),
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
		reflect.TypeFor[Provider]():         enum(ProviderLiveKit, ProviderPipecat, ProviderSlng),
	}}
}

func enum[T ~string](values ...T) *jsonschema.Schema {
	items := make([]any, len(values))
	for i, value := range values {
		items[i] = value
	}
	return &jsonschema.Schema{Type: "string", Enum: items}
}
