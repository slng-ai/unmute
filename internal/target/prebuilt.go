package target

// Prebuilt is a provider-shipped tool a user selects by id with
// `execution: builtin` + `builtin: <id>`, rather than authoring a handler.
// The registry carries only what is portable across drivers: the default
// description and the implied effect. Which providers host it is the
// FieldToolBuiltin capability gate; which params it accepts is the closed set
// of typed Tool fields (docs/spec/prebuilt-tools.md C5).
type Prebuilt struct {
	ID                 string
	DefaultDescription string
	Effect             string // "ends_conversation" | "returns_data"
}

// prebuilts is the closed registry. Adding a prebuilt is one row here plus a
// per-driver lowering; no new authoring surface.
var prebuilts = map[string]Prebuilt{
	"end_call": {
		ID:                 "end_call",
		DefaultDescription: "End the call when the caller is finished or says goodbye.",
		Effect:             "ends_conversation",
	},
}

// LookupPrebuilt returns the registry entry for id, or ok=false if unknown.
func LookupPrebuilt(id string) (Prebuilt, bool) {
	p, ok := prebuilts[id]
	return p, ok
}
